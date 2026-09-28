// Package botintegration 是 B0-07 的 B0 出口集成验收：真实 ent + SQLite，
// 走通「对话内提交写工具 → pending（元数据/脱敏/幂等键一次落库）→ 重复提交幂等回放
// → 审批 → 队列执行 → 回填 → 按会话回溯审计」，叠加 dry-run 零写入与 strict 档负向断言。
//
// 与生产装配的差异（仅两处，均为测试必需）：
//  1. 工具来源：注册本地 stub provider（不起网络），用于可控地提供写工具/严格档工具；
//  2. 权限模式：切到 HardcodeOnly（无 DB 会话的 RBAC 判定路径），角色用 super_admin。
package botintegration

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"
	"itsm-backend/middleware"
	"itsm-backend/service"

	_ "github.com/mattn/go-sqlite3"
)

// stubProvider 是本地工具来源（不起网络）：提供可控的写工具与严格档工具。
type stubProvider struct {
	defs []service.ToolDefinition
	exec *service.ToolExecution

	mu    sync.Mutex
	calls []string
}

func (p *stubProvider) ProviderName() string { return "stub" }

func (p *stubProvider) ListTools(context.Context, int) []service.ToolDefinition { return p.defs }

func (p *stubProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	for index := range p.defs {
		if p.defs[index].Name == name {
			def := p.defs[index]
			return &def, true
		}
	}
	return nil, false
}

func (p *stubProvider) Execute(_ context.Context, _ int, name string, _ map[string]interface{}) (*service.ToolExecution, error) {
	p.mu.Lock()
	p.calls = append(p.calls, name)
	p.mu.Unlock()
	return p.exec, nil
}

// ExecuteApprovedWrite 声明本 provider 支持「审批通过后执行写工具」（M1-02 的 WriteCapableProvider）。
// 写调用一律单次执行（不自动重试）；审批状态由调用方（ToolQueue）保证。
func (p *stubProvider) ExecuteApprovedWrite(_ context.Context, _ int, name string, _ map[string]interface{}) (*service.ToolExecution, error) {
	return p.Execute(context.Background(), 0, name, nil)
}

func (p *stubProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

type b0Harness struct {
	client   *ent.Client
	registry *service.ToolRegistry
	queue    *service.ToolQueue
	svc      *ai.Service
	provider *stubProvider

	tenantID int
	userID   int
	convID   int
}

func newB0Harness(t *testing.T) *b0Harness {
	t.Helper()
	ctx := context.Background()

	dsn := filepath.Join(t.TempDir(), "bot-b0-07.db") + "?_fk=1&_busy_timeout=15000&_journal_mode=WAL"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	// tool_invocations.user_id / conversation_id 是外键，需真实行。
	tenant, err := client.Tenant.Create().SetCode("b0-07").SetName("B0-07").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("b0-07-user").
		SetEmail("b0-07@example.com").
		SetName("B0-07 User").
		SetPasswordHash("x").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	conv, err := client.Conversation.Create().
		SetTenantID(tenant.ID).
		SetUserID(user.ID).
		SetTitle("B0-07 验收会话").
		Save(ctx)
	require.NoError(t, err)

	// 权限模式：HardcodeOnly（与 handlers/ai 既有测试同口径）。
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeHardcodeOnly
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(func() {
		middleware.PermissionConfig.Mode = prevMode
		middleware.InvalidateAllPermissionCaches()
	})

	provider := &stubProvider{
		defs: []service.ToolDefinition{
			{
				Name: "stub__create_note", Description: "创建备注（B0-07 桩）",
				ReadOnly: false, Resource: "ticket", Action: "write",
				Provider: "stub", ServerName: "stub", RawToolName: "create_note",
				Risk: service.ToolRiskActMedium, Category: "ticket",
				RedactionProfile: service.ToolRedactionDefault, Idempotent: true,
				TimeoutMs: service.DefaultToolTimeoutMs, MaxOutputBytes: service.DefaultToolMaxOutputBytes,
				ArgsSchema: map[string]interface{}{"type": "object"},
			},
			{
				Name: "stub__rotate_secret", Description: "轮换密钥（严格档，桩）",
				ReadOnly: false, Resource: "ticket", Action: "write",
				Provider: "stub", ServerName: "stub", RawToolName: "rotate_secret",
				Risk: service.ToolRiskActHigh, Category: "ticket",
				RedactionProfile: service.ToolRedactionStrict, Idempotent: true,
				TimeoutMs: service.DefaultToolTimeoutMs, MaxOutputBytes: service.DefaultToolMaxOutputBytes,
				ArgsSchema: map[string]interface{}{"type": "object"},
			},
		},
		exec: &service.ToolExecution{
			Value:         map[string]interface{}{"noteId": 11},
			Provider:      "stub",
			ServerName:    "stub",
			RawToolName:   "create_note",
			CallableName:  "stub__create_note",
			DurationMs:    7,
			OutputSummary: `{"noteId":11}`,
		},
	}

	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(provider)
	queue := service.NewToolQueue(client, registry, 8, zap.NewNop().Sugar())
	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)

	return &b0Harness{
		client: client, registry: registry, queue: queue, svc: svc, provider: provider,
		tenantID: tenant.ID, userID: user.ID, convID: conv.ID,
	}
}

func waitStatus(t *testing.T, h *b0Harness, id int, want string) *ent.ToolInvocation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		inv, err := h.client.ToolInvocation.Get(context.Background(), id)
		require.NoError(t, err)
		if inv.Status == want {
			return inv
		}
		if time.Now().After(deadline) {
			msg := ""
			if inv.Error != nil {
				msg = *inv.Error
			}
			t.Fatalf("invocation %d 未在 15s 内进入 %s：status=%s errorCode=%s error=%s", id, want, inv.Status, inv.ErrorCode, msg)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestB0Flow_EndToEnd：AB0-01/03/05 的主链路（元数据快照、会话归属、幂等回放、审批执行、拒绝回填、按会话回溯）。
func TestB0Flow_EndToEnd(t *testing.T) {
	ctx := context.Background()
	h := newB0Harness(t)

	args := map[string]interface{}{"title": "打印机故障", "password": "P@ssw0rd-明文"}

	// 1) 对话内提交写工具 → pending；元数据快照 / 脱敏入参 / 幂等键 / 会话归属一次落库（AB0-01/03/05/06）。
	_, pendingID, err := h.svc.ExecuteToolWithConversation(ctx, h.userID, h.tenantID, "super_admin", "stub__create_note", args, h.convID)
	require.NoError(t, err)
	require.Positive(t, pendingID)

	pending, err := h.client.ToolInvocation.Get(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Status)
	assert.Equal(t, "pending", pending.ApprovalState)
	assert.Equal(t, h.convID, pending.ConversationID, "AB0-03：聊天路径 conversation_id 回填")
	assert.Equal(t, service.ToolRiskActMedium, pending.Risk, "AB0-01：风险快照随记录落库")
	assert.Equal(t, "ticket", pending.Category, "AB0-01：分类快照随记录落库")
	assert.Equal(t, "stub", pending.Provider, "AB0-01：来源快照")
	assert.Equal(t, "stub", pending.McpServerName)
	assert.Equal(t, "create_note", pending.McpRawToolName)
	assert.Equal(t, "stub__create_note", pending.McpCallableName)
	assert.NotEmpty(t, pending.IdempotencyKeyHash, "AB0-05：写工具幂等键 hash 落库")
	assert.NotEmpty(t, pending.Arguments, "执行真源仍在（审批重放依赖）")
	assert.Contains(t, pending.Arguments, "P@ssw0rd-明文", "执行真源保留原文（仅内存/审批内部使用）")
	assert.NotContains(t, pending.ArgsRedacted, "P@ssw0rd-明文", "AB0-06：审计快照不得含明文口令")
	assert.Contains(t, pending.ArgsRedacted, "打印机故障", "AB0-06：default 档常规字段保留")

	// 2) 重复提交（同参数）→ 幂等回放首次记录（AB0-05）。
	before, err := h.client.ToolInvocation.Query().Count(ctx)
	require.NoError(t, err)
	replay, replayID, err := h.svc.ExecuteToolWithConversation(ctx, h.userID, h.tenantID, "super_admin", "stub__create_note", args, h.convID)
	require.NoError(t, err)
	assert.Equal(t, pendingID, replayID, "重复提交必须命中首次记录")
	payload, ok := replay.(map[string]interface{})
	require.True(t, ok, "回放载荷应为结构化 map，实际 %T", replay)
	assert.Equal(t, true, payload["idempotentReplay"])
	after, err := h.client.ToolInvocation.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "幂等回放不得新增记录")

	// 3) 审批通过 → 队列执行 → 结果回填；写调用恰好一次（无自动重试）。
	decision, err := h.svc.ApproveTool(ctx, pendingID, h.tenantID, h.userID, true, "")
	require.NoError(t, err)
	assert.Equal(t, "approved", decision)
	executed := waitStatus(t, h, pendingID, "done")
	assert.Equal(t, 1, h.provider.callCount(), "写调用不得自动重试")
	require.NotNil(t, executed.Result)
	assert.Contains(t, *executed.Result, "noteId")

	// 4) 拒绝路径：原因结构化回填会话（AB0-03），且不触发执行。
	_, rejectID, err := h.svc.ExecuteToolWithConversation(ctx, h.userID, h.tenantID, "super_admin", "stub__create_note",
		map[string]interface{}{"title": "另一张工单"}, h.convID)
	require.NoError(t, err)
	decision, err = h.svc.ApproveTool(ctx, rejectID, h.tenantID, h.userID, false, "工单信息不完整")
	require.NoError(t, err)
	assert.Equal(t, "rejected", decision)
	rejected := waitStatus(t, h, rejectID, "rejected")
	assert.Equal(t, "rejected", rejected.ApprovalState)
	assert.Equal(t, 1, h.provider.callCount(), "拒绝路径不得触发执行")

	// 5) 审计按会话回溯（AB0-03）：两次提交都可在该会话下检索。
	rows, err := h.client.ToolInvocation.Query().
		Where(toolinvocation.TenantID(h.tenantID), toolinvocation.ConversationID(h.convID)).
		All(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2, "按会话应可回溯该会话内全部工具调用")

	// 6) 拒绝原因回填会话消息（模型可见），且消息内容不含明文密钥类内容。
	messages, err := h.client.Message.Query().All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	found := false
	for _, message := range messages {
		if message.ConversationID != h.convID {
			continue
		}
		if strings.Contains(message.Content, "工单信息不完整") {
			found = true
			var decoded map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(message.Content), &decoded), "回填消息必须是合法 JSON")
			assert.Equal(t, "tool_approval_decision", decoded["type"])
			assert.Equal(t, "rejected", decoded["decision"])
		}
	}
	assert.True(t, found, "拒绝原因必须回填到会话消息")
}

// TestB0Flow_DryRunZeroWrite：AB0-04——dry-run 预览零业务写入（真实 ent 计数断言）。
func TestB0Flow_DryRunZeroWrite(t *testing.T) {
	ctx := context.Background()
	h := newB0Harness(t)

	before, err := h.client.Ticket.Query().Count(ctx)
	require.NoError(t, err)

	result, previewID, err := h.svc.ExecuteToolWithOptions(ctx, h.userID, h.tenantID, "super_admin", "create_ticket",
		map[string]interface{}{"title": "预览工单", "priority": "high"}, ai.ExecuteToolOptions{DryRun: true})
	require.NoError(t, err)
	assert.Positive(t, previewID, "dry-run 预览记录落库（可审计）")

	preview, ok := result.(*service.ToolPreview)
	require.True(t, ok, "预览结果应为 *service.ToolPreview，实际 %T", result)
	assert.True(t, preview.DryRun)
	assert.Equal(t, "create_ticket", preview.Tool)
	assert.Equal(t, "create", preview.Mode)
	assert.NotEmpty(t, preview.Version, "预览必须带内容哈希（审批所见即所执行）")
	assert.Equal(t, "预览工单", preview.Fields["title"])

	after, err := h.client.Ticket.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "AB0-04：dry-run 必须零业务写入")

	// 预览记录落库（status=preview），可审计但不进审批。
	inv, err := h.client.ToolInvocation.Get(ctx, previewID)
	require.NoError(t, err)
	assert.True(t, inv.DryRun)
	assert.Equal(t, "preview", inv.Status, "预览记录不进审批队列")
	assert.NotEqual(t, "pending", inv.ApprovalState)
	assert.False(t, inv.NeedsApproval, "预览不产生待审批待办")
	assert.Equal(t, 0, inv.ConversationID, "未带会话上下文时不回填 conversation_id")
	assert.NotContains(t, inv.ArgsRedacted, "P@ssw0rd")
}

// TestB0Flow_StrictProfileAndMCPAnnotation：AB0-06——strict 档全掩码 + provider 标注生效（锁定 GetToolForTenant 修复）。
func TestB0Flow_StrictProfileAndMCPAnnotation(t *testing.T) {
	ctx := context.Background()
	h := newB0Harness(t)

	// strict 档：只留键名，任何值不落库。
	_, strictID, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "stub__rotate_secret",
		map[string]interface{}{"secret": "sk-live-abcdef", "note": "轮换说明原文"})
	require.NoError(t, err)
	strictInv, err := h.client.ToolInvocation.Get(ctx, strictID)
	require.NoError(t, err)
	assert.NotContains(t, strictInv.ArgsRedacted, "sk-live-abcdef")
	assert.NotContains(t, strictInv.ArgsRedacted, "轮换说明原文", "strict 档不得落任何值")
	assert.Contains(t, strictInv.ArgsRedacted, `"masked":true`)
	assert.Contains(t, strictInv.ArgsRedacted, "secret", "键名保留（审计可定位字段）")

	// provider 标注（default）生效：非敏感字段可读——若档位解析退化为 strict 该断言失败。
	def := h.registry.GetToolForTenant(ctx, h.tenantID, "stub__create_note")
	require.NotNil(t, def)
	assert.Equal(t, service.ToolRedactionDefault, def.RedactionProfile, "AB0-01：治理标注随 provider 定义解析")

	_, id, err := h.svc.ExecuteTool(ctx, h.userID, h.tenantID, "super_admin", "stub__create_note",
		map[string]interface{}{"title": "标注生效验证", "token": "t-123"})
	require.NoError(t, err)
	inv, err := h.client.ToolInvocation.Get(ctx, id)
	require.NoError(t, err)
	assert.Contains(t, inv.ArgsRedacted, "标注生效验证")
	assert.NotContains(t, inv.ArgsRedacted, "t-123")
}
