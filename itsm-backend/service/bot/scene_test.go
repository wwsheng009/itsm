package bot

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
)

// D1-01：场景 Bot 可用性判定与 slug 快照解析。

func sceneTemplate(status, audience, entrypoints string) *ent.BotTemplate {
	return &ent.BotTemplate{ID: 1, Status: status, Audience: audience, EntrypointsJSON: entrypoints}
}

func TestSceneEligibility_Table(t *testing.T) {
	cases := []struct {
		name       string
		tpl        *ent.BotTemplate
		role       string
		entrypoint string
		eligible   bool
		reason     string
	}{
		{"缺失模板 fail-closed", nil, "admin", EntrypointChat, false, ReasonTemplateMissing},
		{"draft 不下发", sceneTemplate(StatusDraft, AudienceInternal, `["chat"]`), "admin", EntrypointChat, false, ReasonStatusDraft},
		{"audience 拒绝（终端用户）", sceneTemplate(StatusPilot, AudienceInternal, `["chat"]`), "end_user", EntrypointChat, false, ReasonAudienceDenied},
		{"入口拒绝", sceneTemplate(StatusPilot, AudienceInternal, `["ticket_detail"]`), "admin", EntrypointChat, false, ReasonEntrypointDenied},
		{"entrypoints 为空 = 全拒", sceneTemplate(StatusPilot, AudienceInternal, `[]`), "admin", EntrypointChat, false, ReasonEntrypointDenied},
		{"pilot 可见", sceneTemplate(StatusPilot, AudienceInternal, `["chat","ticket_detail"]`), "admin", "ticket_detail", true, ""},
		{"all 对终端用户可见", sceneTemplate(StatusGA, AudienceAll, `["chat"]`), "end_user", EntrypointChat, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eligible, reason := SceneEligibility(tc.tpl, tc.role, tc.entrypoint)
			assert.Equal(t, tc.eligible, eligible)
			assert.Equal(t, tc.reason, reason)
		})
	}
}

func TestPolicy_SnapshotForSlug(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "bot-scene.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	admin := NewTemplateAdmin(client)
	tpl, err := admin.CreateTemplate(ctx, 7, TemplateInput{
		Slug:        "s1-ticket-assistant",
		Name:        "S1 工单助手",
		Audience:    AudienceInternal,
		RiskLimit:   RiskActLow,
		Entrypoints: []string{EntrypointChat, "ticket_detail"},
		Status:      StatusPilot,
	})
	require.NoError(t, err)
	_, err = admin.UpsertGrant(ctx, 7, tpl.ID, GrantInput{ToolName: "list_tickets", RiskLimit: RiskRead})
	require.NoError(t, err)

	policy := NewPolicy(client)
	snap, err := policy.SnapshotForSlug(ctx, 7, "s1-ticket-assistant")
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, tpl.ID, snap.Template.ID)
	assert.Contains(t, snap.Grants, "list_tickets")

	// 未知 slug / 跨租户 → (nil, nil)，不视为错误。
	for _, tc := range []struct {
		tenant int
		slug   string
	}{
		{7, "no-such-bot"},
		{8, "s1-ticket-assistant"},
		{7, "  "},
	} {
		missing, err := policy.SnapshotForSlug(ctx, tc.tenant, tc.slug)
		require.NoError(t, err)
		assert.Nil(t, missing, "tenant=%d slug=%q", tc.tenant, tc.slug)
	}

	// 空 slug 直接返回 nil（不触库）。
	missing, err := policy.SnapshotForSlug(ctx, 7, "")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
