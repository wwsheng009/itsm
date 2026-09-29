package bot

import (
	"context"
	"path/filepath"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B2-02 策略门禁测试：兼容默认（迁移自 chatWritableTools）、严格四重交集、边界与快照解析。

func readTool(name string) ToolMeta {
	return ToolMeta{Name: name, Provider: "builtin", ReadOnly: true, Resource: "ticket", Action: "read", Risk: RiskRead}
}

func writeTool(name, risk string) ToolMeta {
	return ToolMeta{Name: name, Provider: "builtin", ReadOnly: false, Resource: "ticket", Action: "write", Risk: risk}
}

func strictSnapshot(status, riskLimit, entrypoints string, grants ...*ent.BotToolGrant) *Snapshot {
	tpl := &ent.BotTemplate{ID: 1, Status: status, RiskLimit: riskLimit, EntrypointsJSON: entrypoints}
	byName := make(map[string]*ent.BotToolGrant, len(grants))
	for _, grant := range grants {
		byName[grant.ToolName] = grant
	}
	return &Snapshot{Template: tpl, Grants: byName}
}

func grant(name, riskLimit string) *ent.BotToolGrant {
	return &ent.BotToolGrant{BotID: 1, ToolName: name, RiskLimit: riskLimit}
}

// TestDecide_LegacyCompatPinsOldRule 兼容默认必须与迁移前规则**逐项等价**：
//
//	允许 = ReadOnly || chatWritableTools[name]（迁移前 handlers/ai/service.go 的判定）。
func TestDecide_LegacyCompatPinsOldRule(t *testing.T) {
	cases := []struct {
		name   string
		tool   ToolMeta
		legacy bool
	}{
		{"只读工具放行", readTool("list_tickets"), true},
		{"遗留白名单写工具放行", writeTool("create_ticket", RiskActLow), true},
		{"遗留白名单写工具放行(update_ticket)", writeTool("update_ticket", RiskActLow), true},
		{"遗留白名单写工具放行(create_ticket_type)", writeTool("create_ticket_type", RiskActLow), true},
		{"遗留白名单写工具放行(link_ticket_ci)", writeTool("link_ticket_ci", RiskActLow), true},
		{"遗留白名单写工具放行(create_ci_relationship)", writeTool("create_ci_relationship", RiskActLow), true},
		{"遗留白名单写工具放行(delete_ci_relationship)", writeTool("delete_ci_relationship", RiskActLow), true},
		{"非白名单写工具拒绝", writeTool("delete_ticket", RiskActHigh), false},
		{"MCP 写工具拒绝(未授权)", writeTool("mcp__github__create_issue", RiskActLow), false},
		{"风险未标注仍放行(兼容默认不看风险)", writeTool("create_ticket", ""), true},
	}
	for _, tc := range cases {
		t.Run("nil快照/"+tc.name, func(t *testing.T) {
			decision := Decide(CheckInput{Tool: tc.tool, Entrypoint: EntrypointChat})
			assert.Equal(t, tc.legacy, decision.Allowed)
			assert.True(t, decision.Legacy, "无模板必须标记 Legacy")
			if !tc.legacy {
				assert.Equal(t, ReasonToolNotGranted, decision.Reason)
			}
		})
		t.Run("零授权快照/"+tc.name, func(t *testing.T) {
			// 模板存在但未配置任何授权 = 未上线策略 → 同样走兼容默认。
			snapshot := strictSnapshot(StatusGA, RiskActLow, `["chat"]`)
			decision := Decide(CheckInput{Snapshot: snapshot, Tool: tc.tool, Entrypoint: EntrypointChat})
			assert.Equal(t, tc.legacy, decision.Allowed)
			assert.True(t, decision.Legacy)
		})
	}
}

func TestDecide_StrictIntersection(t *testing.T) {
	chatGA := `["chat"]`

	t.Run("已授权且风险未超限放行", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.True(t, decision.Allowed)
		assert.False(t, decision.Legacy)
	})

	t.Run("只读工具也必须在授权内", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: readTool("list_tickets"), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonToolNotGranted, decision.Reason)
	})

	t.Run("未授权工具拒绝", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("delete_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonToolNotGranted, decision.Reason)
	})

	t.Run("风险超授权上限拒绝", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActHigh, chatGA, grant("mcp__ops__rotate", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("mcp__ops__rotate", RiskActMedium), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonRiskExceeded, decision.Reason)
	})

	t.Run("风险未标注fail-closed", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActHigh, chatGA, grant("mcp__ops__x", RiskActHigh))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("mcp__ops__x", ""), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonRiskUnknown, decision.Reason)
	})

	t.Run("授权上限超过模板上限拒绝(读侧双保险)", func(t *testing.T) {
		// 绕过写入侧校验直接构造的脏数据（例如直改库/历史迁移），读侧必须拦住。
		snapshot := strictSnapshot(StatusGA, RiskActLow, chatGA, grant("mcp__ops__rotate", RiskActHigh))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("mcp__ops__rotate", RiskActHigh), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonRiskExceeded, decision.Reason)
	})

	t.Run("入口不匹配拒绝", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, `["ticket"]`, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonEntrypointDenied, decision.Reason)
	})

	t.Run("入口为空列表fail-closed", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, `[]`, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonEntrypointDenied, decision.Reason)
	})

	t.Run("入口JSON损坏fail-closed", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, `not-json`, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonEntrypointDenied, decision.Reason)
	})

	t.Run("draft模板不下发", func(t *testing.T) {
		snapshot := strictSnapshot(StatusDraft, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonStatusDraft, decision.Reason)
	})

	t.Run("pilot模板视为已发布", func(t *testing.T) {
		snapshot := strictSnapshot(StatusPilot, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.True(t, decision.Allowed)
	})

	t.Run("RBAC拒绝优先于放行", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, chatGA, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{
			Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat,
			RBACAllowed: func(string, string) bool { return false },
		})
		assert.False(t, decision.Allowed)
		assert.Equal(t, ReasonRBACDenied, decision.Reason)
	})

	t.Run("风险等于上限边界放行", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActHigh, chatGA, grant("mcp__ops__rotate", RiskActMedium))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("mcp__ops__rotate", RiskActMedium), Entrypoint: EntrypointChat})
		assert.True(t, decision.Allowed, "上限=工具风险属合法边界")
	})

	t.Run("入口大小写不敏感", func(t *testing.T) {
		snapshot := strictSnapshot(StatusGA, RiskActMedium, `["CHAT"]`, grant("create_ticket", RiskActLow))
		decision := Decide(CheckInput{Snapshot: snapshot, Tool: writeTool("create_ticket", RiskActLow), Entrypoint: EntrypointChat})
		assert.True(t, decision.Allowed)
	})
}

func TestPolicy_SnapshotForBot(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "bot-policy.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	admin := NewTemplateAdmin(client)
	policy := NewPolicy(client)
	ctx := context.Background()

	// 无模板 → (nil, nil)：调用方按兼容默认处理。
	snapshot, err := policy.SnapshotForBot(ctx, 1, 0)
	require.NoError(t, err)
	require.Nil(t, snapshot, "未种子默认助手时不得报错，返回 nil 走兼容默认")

	// 显式 botID：模板 + 授权。
	tpl, err := admin.CreateTemplate(ctx, 1, TemplateInput{
		Slug: "ops", Name: "运维助手", RiskLimit: RiskActMedium, Status: StatusGA, Entrypoints: []string{EntrypointChat},
	})
	require.NoError(t, err)
	_, err = admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "create_ticket", RiskLimit: RiskActLow})
	require.NoError(t, err)

	snapshot, err = policy.SnapshotForBot(ctx, 1, tpl.ID)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, tpl.ID, snapshot.Template.ID)
	require.Contains(t, snapshot.Grants, "create_ticket")
	assert.Equal(t, RiskActLow, snapshot.Grants["create_ticket"].RiskLimit)

	// 租户隔离：租户 2 看不到租户 1 的模板。
	snapshot, err = policy.SnapshotForBot(ctx, 2, tpl.ID)
	require.NoError(t, err)
	assert.Nil(t, snapshot)

	// botID<=0：解析内置默认助手（首次列表已种入）。
	_, err = admin.ListTemplates(ctx, 3)
	require.NoError(t, err)
	snapshot, err = policy.SnapshotForBot(ctx, 3, 0)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, DefaultTemplateSlug, snapshot.Template.Slug)
	assert.Empty(t, snapshot.Grants, "默认助手无授权 → 兼容默认")

	// CheckTool：快照读取失败（client 置空）→ fail-closed。
	broken := &Policy{}
	decision := broken.CheckTool(ctx, 1, tpl.ID, EntrypointChat, writeTool("create_ticket", RiskActLow), nil)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonSnapshotError, decision.Reason)
}
