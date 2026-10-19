package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent/enttest"
	"itsm-backend/service/bot"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestListHelpDrafts_TenantQueueWithDiff(t *testing.T) {
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "help-drafts.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	store := bot.NewArtifactStore(client)
	ctx := context.Background()

	tenantA := client.Tenant.Create().SetCode("help-drafts-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("help-drafts-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("help-drafts-a").SetEmail("help-drafts-a@example.com").SetName("A").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)
	userB := client.User.Create().
		SetUsername("help-drafts-b").SetEmail("help-drafts-b@example.com").SetName("B").
		SetPasswordHash("x").SetTenantID(tenantB.ID).SaveX(ctx)

	content := helpDraftReviewContent{
		PageKey:  "ticket.detail",
		Title:    "工单详情帮助草稿",
		Markdown: "## 新步骤\n- 保留行\n- 新增行",
		Status:   "draft",
		Revision: 2,
	}
	row, err := store.Create(ctx, bot.ArtifactInput{
		TenantID: tenantA.ID, OwnerUserID: userA.ID,
		Kind: bot.ArtifactKindDraft, ToolName: "help_update_draft",
		Title: "工单详情帮助草稿", Content: content,
		Evidence: map[string]interface{}{"sourceDocs": []string{"docs/help/tickets/detail.md"}},
	})
	require.NoError(t, err)
	_, err = store.Create(ctx, bot.ArtifactInput{
		TenantID: tenantB.ID, OwnerUserID: userB.ID,
		Kind: bot.ArtifactKindDraft, ToolName: "help_create_draft",
		Title: "B tenant draft", Content: helpDraftReviewContent{PageKey: "x", Markdown: "b"},
	})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := NewHandler(&Service{artifacts: store})
	engine.GET("/api/v1/ai/help/drafts", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		handler.ListHelpDrafts(c)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/drafts", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Data struct {
			Items []helpDraftReviewItem `json:"items"`
			Total int                   `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, 1, payload.Data.Total)
	require.Len(t, payload.Data.Items, 1)
	item := payload.Data.Items[0]
	require.Equal(t, row.ID, item.ArtifactID)
	require.Equal(t, userA.ID, item.OwnerUserID)
	require.Equal(t, "draft", item.Status)
	require.NotEmpty(t, item.BaselineMarkdown, "未保存 baseline 时从内嵌帮助索引派生")
	require.NotEmpty(t, item.Diff)

	// 服务未装配 → 503。
	engine = gin.New()
	engine.GET("/api/v1/ai/help/drafts", func(c *gin.Context) {
		c.Set("tenant_id", tenantA.ID)
		NewHandler(&Service{}).ListHelpDrafts(c)
	})
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/drafts", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestHelpDraftDiff(t *testing.T) {
	diff := helpDraftDiff("a\nb\nc", "a\nc\nd")
	require.Contains(t, diff, helpDraftReviewLine{Type: "removed", Content: "b"})
	require.Contains(t, diff, helpDraftReviewLine{Type: "added", Content: "d"})

	allNew := helpDraftDiff("", "x\ny")
	require.Equal(t, []helpDraftReviewLine{
		{Type: "added", Content: "x"},
		{Type: "added", Content: "y"},
	}, allNew)

	oneRemoved := helpDraftDiff("a\na", "a")
	require.Equal(t, []helpDraftReviewLine{{Type: "removed", Content: "a"}}, oneRemoved)
}
