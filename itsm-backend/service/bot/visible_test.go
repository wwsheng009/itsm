package bot

import (
	"context"
	"path/filepath"
	"testing"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// —— B2-04 选择器可见性（audience × status × entrypoint）——

func TestVisibleForRole(t *testing.T) {
	cases := []struct {
		audience string
		role     string
		want     bool
	}{
		{"", "technician", true}, // 空 = internal
		{"", "end_user", false},  // 空 = internal → 终端用户不可见
		{"internal", "technician", true},
		{"internal", "end_user", false},
		{"end_user", "end_user", true},
		{"end_user", "technician", true}, // 内部可见终端用户 Bot（无害）
		{"all", "end_user", true},
		{"all", "technician", true},
		{"Internal", "end_user", false}, // 大小写不敏感
		{"weird-value", "end_user", false},
		{"weird-value", "technician", true},
		{"  ", "end_user", false}, // 空白按空值 = internal
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, VisibleForRole(tc.audience, tc.role),
			"audience=%q role=%q", tc.audience, tc.role)
	}
}

func TestListVisibleForChat(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "bot-visible.db") + "?_fk=1&_busy_timeout=15000"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	admin := NewTemplateAdmin(client)
	ctx := context.Background()

	// 空租户先列表 → 种入默认助手（internal + chat 入口 + ga）。
	seeded, err := admin.ListVisibleForChat(ctx, 1, "technician")
	require.NoError(t, err)
	require.Len(t, seeded, 1)
	assert.Equal(t, DefaultTemplateSlug, seeded[0].Slug)

	// 终端用户视角：默认助手（internal）不可见 → 空列表（不失败）。
	got, err := admin.ListVisibleForChat(ctx, 1, "end_user")
	require.NoError(t, err)
	assert.Empty(t, got)

	mk := func(slug, audience, status string, entrypoints []string) {
		t.Helper()
		_, err := admin.CreateTemplate(ctx, 1, TemplateInput{
			Slug: slug, Name: slug, Audience: audience, RiskLimit: RiskActLow,
			Status: status, Entrypoints: entrypoints,
		})
		require.NoError(t, err)
	}
	mk("ops", "internal", StatusGA, []string{EntrypointChat})
	mk("user-bot", "end_user", StatusGA, []string{EntrypointChat})
	mk("everyone", "all", StatusPilot, []string{EntrypointChat})
	mk("draft-bot", "all", StatusDraft, []string{EntrypointChat})
	mk("ticket-only", "all", StatusGA, []string{"ticket_detail"})
	mk("no-entry", "all", StatusGA, nil)

	slugs := func(role string) []string {
		rows, err := admin.ListVisibleForChat(ctx, 1, role)
		require.NoError(t, err)
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.Slug)
		}
		return out
	}

	technician := slugs("technician")
	assert.ElementsMatch(t, []string{DefaultTemplateSlug, "ops", "user-bot", "everyone"}, technician,
		"技术员可见 internal/end_user/all；draft、入口不匹配、空入口不可见")

	endUser := slugs("end_user")
	assert.ElementsMatch(t, []string{"user-bot", "everyone"}, endUser,
		"终端用户只见 end_user/all")

	// 租户隔离：另一租户只有自己的默认助手。
	other, err := admin.ListVisibleForChat(ctx, 2, "technician")
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, DefaultTemplateSlug, other[0].Slug)
}
