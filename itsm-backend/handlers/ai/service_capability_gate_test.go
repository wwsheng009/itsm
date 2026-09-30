package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// M2 能力开关：执行入口 Gate 0 的契约（2026-09-30 方案 §2.3 G1）。
//
//  1. 工具不在面内但**存在**且被能力开关挡下 → 返回 ErrToolCapabilityDisabled（带精确原因），
//     而不是笼统的 ErrUnknownTool；
//  2. 审计落 denied + permission_reason（capability_disabled:*），且不得执行；
//  3. 无门禁的未知工具仍回落既有语义。

// stubGatedProvider 实现 service.ToolProvider + service.ToolGateReasoner：
// 工具不在面内（Resolve 失败），但 GateReason 能给出被挡原因。
type stubGatedProvider struct {
	reason    string
	executed  int
	execError error
}

func (p *stubGatedProvider) ProviderName() string { return "mcp" }

func (p *stubGatedProvider) ListTools(context.Context, int) []service.ToolDefinition { return nil }

func (p *stubGatedProvider) Resolve(context.Context, int, string) (*service.ToolDefinition, bool) {
	return nil, false
}

func (p *stubGatedProvider) Execute(context.Context, int, string, map[string]interface{}) (*service.ToolExecution, error) {
	p.executed++
	return nil, p.execError
}

func (p *stubGatedProvider) GateReason(context.Context, int, string) string { return p.reason }

func TestExecuteTool_CapabilityGateDeniesWithReason(t *testing.T) {
	provider := &stubGatedProvider{reason: service.ReasonCapabilityMCPWriteDisabled}
	svc, repo := newMCPAuditEnv(t, provider)

	_, _, err := svc.ExecuteToolWithOptions(context.Background(), 7, 1, "super_admin",
		"mcp__mock__create_issue", map[string]interface{}{"title": "x"}, ai.ExecuteToolOptions{})

	require.Error(t, err)
	require.ErrorIs(t, err, ai.ErrToolCapabilityDisabled)

	var capErr *ai.CapabilityDisabledError
	require.True(t, errors.As(err, &capErr))
	assert.Equal(t, service.ReasonCapabilityMCPWriteDisabled, capErr.Reason)
	assert.Contains(t, ai.CapabilityDisabledMessage(capErr.Reason), "管理后台")
	assert.Equal(t, 0, provider.executed, "被能力开关挡下时不得执行")

	require.Len(t, repo.toolInvocations, 1, "拒绝路径必须落审计")
	inv := repo.toolInvocations[0]
	assert.Equal(t, "denied", inv.PermissionCheck)
	assert.Contains(t, inv.PermissionReason, service.ReasonCapabilityMCPWriteDisabled)
	assert.Equal(t, "mcp__mock__create_issue", inv.ToolName)
}

func TestExecuteTool_CapabilityGateMCPDisabledReason(t *testing.T) {
	provider := &stubGatedProvider{reason: service.ReasonCapabilityMCPDisabled}
	svc, repo := newMCPAuditEnv(t, provider)

	_, _, err := svc.ExecuteToolWithOptions(context.Background(), 7, 1, "super_admin",
		"mcp__mock__list_issues", nil, ai.ExecuteToolOptions{})
	require.ErrorIs(t, err, ai.ErrToolCapabilityDisabled)
	require.Len(t, repo.toolInvocations, 1)
	assert.Contains(t, repo.toolInvocations[0].PermissionReason, service.ReasonCapabilityMCPDisabled)
	assert.Equal(t, 0, provider.executed)
}

func TestExecuteTool_UnknownToolStillUsesLegacyError(t *testing.T) {
	provider := &stubGatedProvider{reason: ""} // 无门禁：应回落「未知工具」
	svc, repo := newMCPAuditEnv(t, provider)

	_, _, err := svc.ExecuteToolWithOptions(context.Background(), 7, 1, "super_admin",
		"mcp__mock__nope", nil, ai.ExecuteToolOptions{})
	require.ErrorIs(t, err, ai.ErrUnknownTool)
	require.NotErrorIs(t, err, ai.ErrToolCapabilityDisabled)
	require.Len(t, repo.toolInvocations, 1)
	assert.Equal(t, "unknown tool", repo.toolInvocations[0].PermissionReason)
}
