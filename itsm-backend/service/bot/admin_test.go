package bot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"itsm-backend/ent/bottoolgrant"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B2-01 服务层测试：模板/授权的 CRUD、校验、租户隔离、级联删除与内置默认助手。

func newAdminHarness(t *testing.T) (*TemplateAdmin, context.Context) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "bot-admin.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return NewTemplateAdmin(client), context.Background()
}

func TestTemplateAdmin_CRUDDefaultsAndValidation(t *testing.T) {
	admin, ctx := newAdminHarness(t)

	created, err := admin.CreateTemplate(ctx, 1, TemplateInput{Slug: "ticket-helper", Name: "工单助手"})
	require.NoError(t, err)
	assert.Equal(t, StatusDraft, created.Status, "默认 status=draft")
	assert.Equal(t, RiskActLow, created.RiskLimit, "默认风险上限 act_low")
	assert.Equal(t, "internal", created.Audience)
	assert.Equal(t, "[]", created.EntrypointsJSON)

	// slug 租户内唯一；不同租户可同名。
	_, err = admin.CreateTemplate(ctx, 1, TemplateInput{Slug: "ticket-helper", Name: "重复"})
	require.ErrorIs(t, err, ErrTemplateSlugUsed)
	_, err = admin.CreateTemplate(ctx, 2, TemplateInput{Slug: "ticket-helper", Name: "租户2"})
	require.NoError(t, err)

	// 校验：status/risk/entrypoints 非法值全部拒绝。
	for _, in := range []TemplateInput{
		{Slug: "a", Name: "A", Status: "unknown"},
		{Slug: "b", Name: "B", RiskLimit: "act_nuclear"},
		{Slug: "c", Name: "C", Entrypoints: []string{"chat", " "}},
		{Slug: " ", Name: "D"},
		{Slug: "e", Name: " "},
	} {
		_, err := admin.CreateTemplate(ctx, 1, in)
		require.ErrorIs(t, err, ErrValidation, "入参 %+v 必须被拒绝", in)
	}

	// 更新：name/status/risk/entrypoints；slug 不改动（接口层拒绝）。
	updated, err := admin.UpdateTemplate(ctx, 1, created.ID, TemplateInput{
		Name: "工单助手 v2", Status: StatusPilot, RiskLimit: RiskActMedium,
		Entrypoints: []string{"chat", "ticket"},
	})
	require.NoError(t, err)
	assert.Equal(t, "工单助手 v2", updated.Name)
	assert.Equal(t, StatusPilot, updated.Status)
	assert.Equal(t, RiskActMedium, updated.RiskLimit)
	assert.Equal(t, `["chat","ticket"]`, updated.EntrypointsJSON)
	assert.Equal(t, "ticket-helper", updated.Slug, "slug 不可变")

	// 更新非法 status 被拒；不存在/跨租户更新返回 ErrTemplateNotFound。
	_, err = admin.UpdateTemplate(ctx, 1, created.ID, TemplateInput{Status: "nope"})
	require.ErrorIs(t, err, ErrValidation)
	_, err = admin.UpdateTemplate(ctx, 3, created.ID, TemplateInput{Name: "越权"})
	require.ErrorIs(t, err, ErrTemplateNotFound)
	_, err = admin.GetTemplate(ctx, 3, created.ID)
	require.ErrorIs(t, err, ErrTemplateNotFound, "跨租户读取必须表现为不存在")
}

func TestTemplateAdmin_GrantUpsertLimitsAndCascade(t *testing.T) {
	admin, ctx := newAdminHarness(t)

	tpl, err := admin.CreateTemplate(ctx, 1, TemplateInput{
		Slug: "ops-helper", Name: "运维助手", RiskLimit: RiskActMedium, Status: StatusGA,
	})
	require.NoError(t, err)

	// 新建授权 → 更新同键授权（不产生重复行）。
	grant, err := admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "stub__create_note", RiskLimit: RiskActLow})
	require.NoError(t, err)
	assert.Equal(t, RiskActLow, grant.RiskLimit)
	grant2, err := admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{
		ToolName: "stub__create_note", RiskLimit: RiskActMedium, ArgsPolicyJSON: `{"deny":["force"]}`,
	})
	require.NoError(t, err)
	assert.Equal(t, grant.ID, grant2.ID, "同模板同工具必须 upsert 到同一行")
	assert.Equal(t, RiskActMedium, grant2.RiskLimit)
	assert.Contains(t, grant2.ArgsPolicyJSON, "force")

	// 授权风险不得超过模板上限。
	_, err = admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "stub__rotate_secret", RiskLimit: RiskActHigh})
	require.ErrorIs(t, err, ErrValidation)
	// 空工具名/非法风险级别被拒。
	_, err = admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "  "})
	require.ErrorIs(t, err, ErrValidation)
	_, err = admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "x", RiskLimit: "wild"})
	require.ErrorIs(t, err, ErrValidation)

	// 跨租户不可见：租户 2 对同一 botID 的授权操作表现为模板不存在。
	_, err = admin.UpsertGrant(ctx, 2, tpl.ID, GrantInput{ToolName: "stub__create_note"})
	require.ErrorIs(t, err, ErrTemplateNotFound)

	// 删除授权（幂等边界：再次删除 → ErrGrantNotFound）。
	require.NoError(t, admin.DeleteGrant(ctx, 1, tpl.ID, grant2.ID))
	require.ErrorIs(t, admin.DeleteGrant(ctx, 1, tpl.ID, grant2.ID), ErrGrantNotFound)

	// 级联：删除模板 → 授权随删；再次删除模板 → ErrTemplateNotFound。
	_, err = admin.UpsertGrant(ctx, 1, tpl.ID, GrantInput{ToolName: "stub__list_notes"})
	require.NoError(t, err)
	require.NoError(t, admin.DeleteTemplate(ctx, 1, tpl.ID))
	require.ErrorIs(t, admin.DeleteTemplate(ctx, 1, tpl.ID), ErrTemplateNotFound)

	grants, err := admin.client.BotToolGrant.Query().
		Where(bottoolgrant.BotID(tpl.ID)).All(ctx)
	require.NoError(t, err)
	assert.Empty(t, grants, "删除模板必须级联删除其授权")
}

func TestTemplateAdmin_SeedDefaultAndAutoSeedOnList(t *testing.T) {
	admin, ctx := newAdminHarness(t)

	// 首次列表：空租户自动种入内置默认助手。
	rows, err := admin.ListTemplates(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, DefaultTemplateSlug, rows[0].Slug)
	assert.Equal(t, StatusGA, rows[0].Status)
	assert.Equal(t, `["chat"]`, rows[0].EntrypointsJSON)
	assert.Equal(t, RiskActLow, rows[0].RiskLimit)

	// 再次列表与显式种子：幂等（created=false），不产生重复行。
	rows, err = admin.ListTemplates(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, created, err := admin.SeedDefaultTemplate(ctx, 7)
	require.NoError(t, err)
	assert.False(t, created, "已存在时不得重复种入")

	// 另一个租户各自独立种子（租户隔离）。
	otherRows, err := admin.ListTemplates(ctx, 8)
	require.NoError(t, err)
	require.Len(t, otherRows, 1)
	assert.NotEqual(t, otherRows[0].ID, rows[0].ID, "不同租户的种子模板必须是各自的行")

	// 管理员的改名/改状态不被覆盖。
	updated, err := admin.UpdateTemplate(ctx, 7, rows[0].ID, TemplateInput{Name: "自定义默认"})
	require.NoError(t, err)
	assert.Equal(t, "自定义默认", updated.Name)
	rows, err = admin.ListTemplates(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "自定义默认", rows[0].Name)

	// 全部删光后再访问 → 保护性兜底重新种入（既定行为，见 ListTemplates 注释）。
	require.NoError(t, admin.DeleteTemplate(ctx, 7, rows[0].ID))
	rows, err = admin.ListTemplates(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, DefaultTemplateSlug, rows[0].Slug)
}

func TestTemplateAdmin_RiskRank(t *testing.T) {
	require.Equal(t, -1, RiskRank("unknown"))
	require.Less(t, RiskRank(RiskRead), RiskRank(RiskPlan))
	require.Less(t, RiskRank(RiskActLow), RiskRank(RiskActMedium))
	require.Less(t, RiskRank(RiskActMedium), RiskRank(RiskActHigh))
	require.True(t, errors.Is(ErrValidation, ErrValidation))
}
