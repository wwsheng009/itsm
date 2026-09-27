package ai_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/handlers/ai"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// M0-11：外部工具（MCP）只读执行的审计落库断言——
// tool_invocations 必须落 provider/三元组/耗时/错误码/脱敏入参/输出摘要。

type stubMCPProvider struct {
	def      service.ToolDefinition
	exec     *service.ToolExecution
	execErr  error
	resolveN int
}

func (p *stubMCPProvider) ProviderName() string { return "mcp" }

func (p *stubMCPProvider) ListTools(context.Context, int) []service.ToolDefinition {
	return []service.ToolDefinition{p.def}
}

func (p *stubMCPProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	p.resolveN++
	if name != p.def.Name {
		return nil, false
	}
	def := p.def
	return &def, true
}

func (p *stubMCPProvider) Execute(context.Context, int, string, map[string]interface{}) (*service.ToolExecution, error) {
	return p.exec, p.execErr
}

type stubCodedError struct{ code, message string }

func (e *stubCodedError) Error() string     { return e.code + ": " + e.message }
func (e *stubCodedError) ErrorCode() string { return e.code }

func newMCPAuditEnv(t *testing.T, provider service.ToolProvider) (*ai.Service, *rbacMockRepo) {
	t.Helper()
	repo := &rbacMockRepo{}
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	tools.RegisterProvider(provider)

	svc := ai.NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(&ent.Client{})

	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	ai.ResetRBACFlagForTest()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
		ai.ResetRBACFlagForTest()
	})
	return svc, repo
}

func mcpReadTool(name string) service.ToolDefinition {
	return service.ToolDefinition{
		Name:        name,
		Description: "[MCP:gitlab] " + name,
		ReadOnly:    true,
		Resource:    "mcp",
		Action:      "read",
		ArgsSchema:  map[string]interface{}{"type": "object"},
	}
}

func TestExecuteTool_MCPReadOnlyAuditTriple(t *testing.T) {
	const callable = "mcp__gitlab__list_issues"
	provider := &stubMCPProvider{
		def: mcpReadTool(callable),
		exec: &service.ToolExecution{
			Value:         map[string]interface{}{"issues": []interface{}{}},
			Provider:      "mcp",
			ServerName:    "gitlab",
			RawToolName:   "list_issues",
			CallableName:  callable,
			DurationMs:    42,
			OutputSummary: `{"issues":[]}`,
		},
	}
	svc, repo := newMCPAuditEnv(t, provider)

	args := map[string]interface{}{"token": "glpat-super-secret-value", "project": "ops"}
	result, pendingID, err := svc.ExecuteTool(context.Background(), 7, 1, "super_admin", callable, args)
	require.NoError(t, err)
	require.Equal(t, 0, pendingID, "只读工具不应进入审批")
	require.NotNil(t, result)

	inv := repo.lastInvocation()
	require.NotNil(t, inv)
	assert.Equal(t, "executed", inv.Status)
	// 三元组 + provider 标识（审计查询按 provider/服务器筛选的依据）。
	assert.Equal(t, "mcp", inv.Provider)
	assert.Equal(t, "gitlab", inv.McpServerName)
	assert.Equal(t, "list_issues", inv.McpRawToolName)
	assert.Equal(t, callable, inv.McpCallableName)
	assert.Equal(t, int64(42), inv.DurationMs)
	assert.Empty(t, inv.ErrorCode)
	assert.Equal(t, `{"issues":[]}`, inv.OutputSummary)
	// 脱敏：敏感键掩码，明文不得出现在审计字段中。
	assert.True(t, strings.Contains(inv.ArgsRedacted, "****"), "敏感键应替换为掩码：%s", inv.ArgsRedacted)
	assert.NotContains(t, inv.ArgsRedacted, "glpat-super-secret-value")
	assert.Contains(t, inv.ArgsRedacted, "ops", "非敏感键保持可读")
}

func TestExecuteTool_MCPFailureRecordsErrorCode(t *testing.T) {
	const callable = "mcp__gitlab__list_issues"
	provider := &stubMCPProvider{
		def: mcpReadTool(callable),
		exec: &service.ToolExecution{
			Provider:     "mcp",
			ServerName:   "gitlab",
			RawToolName:  "list_issues",
			CallableName: callable,
			DurationMs:   900,
			ErrorCode:    "tool_timeout",
		},
		execErr: &stubCodedError{code: "tool_timeout", message: "调用超时"},
	}
	svc, repo := newMCPAuditEnv(t, provider)

	_, _, err := svc.ExecuteTool(context.Background(), 7, 1, "super_admin", callable, map[string]interface{}{})
	require.Error(t, err)

	inv := repo.lastInvocation()
	require.NotNil(t, inv)
	assert.Equal(t, "failed", inv.Status, "失败也要留痕（不得静默）")
	assert.Equal(t, "tool_timeout", inv.ErrorCode, "稳定错误码必须落库（与前端展示对齐）")
	assert.Equal(t, "gitlab", inv.McpServerName)
	assert.Equal(t, int64(900), inv.DurationMs)
}

func TestExecuteTool_PermissionDeniedKeepsBuiltinProvider(t *testing.T) {
	const callable = "mcp__gitlab__list_issues"
	provider := &stubMCPProvider{def: mcpReadTool(callable), exec: &service.ToolExecution{Value: "never"}}
	svc, repo := newMCPAuditEnv(t, provider)

	// technician 无 mcp:read（M0-10 默认拒绝）→ Gate2 拒绝且不得执行。
	_, _, err := svc.ExecuteTool(context.Background(), 7, 1, "technician", callable, map[string]interface{}{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ai.ErrToolPermissionDenied))

	inv := repo.lastInvocation()
	require.NotNil(t, inv)
	assert.Equal(t, "denied", inv.PermissionCheck)
	assert.Empty(t, inv.Provider, "被 Gate2 拒绝时不带外部 provider 元数据")
	assert.Empty(t, inv.McpServerName)
}
