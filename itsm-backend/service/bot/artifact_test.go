package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

// B3-06 产物存储：归属/租户隔离、类型校验、内容与证据序列化。

func newArtifactEnv(t *testing.T) (*ArtifactStore, context.Context, int, int, int) {
	t.Helper()
	ctx := context.Background()
	dsn := "file:bot-artifact-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("art-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("art-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("art-a-user").SetEmail("art-a@example.com").SetName("A User").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)
	userB := client.User.Create().
		SetUsername("art-b-user").SetEmail("art-b@example.com").SetName("B User").
		SetPasswordHash("x").SetTenantID(tenantB.ID).SaveX(ctx)

	return NewArtifactStore(client), ctx, tenantA.ID, userA.ID, userB.ID
}

func TestArtifactStore_CreateGetList(t *testing.T) {
	store, ctx, tenantID, ownerID, _ := newArtifactEnv(t)
	require.NotNil(t, store)

	created, err := store.Create(ctx, ArtifactInput{
		TenantID: tenantID, OwnerUserID: ownerID, ConversationID: 7, RunID: 9,
		Kind: ArtifactKindPlan, ToolName: "draft_ticket_fields", Title: "工单字段草案",
		Content:  map[string]interface{}{"fields": map[string]interface{}{"title": "打印机故障"}},
		Evidence: map[string]interface{}{"source": "user_description"},
	})
	require.NoError(t, err)
	assert.Equal(t, ArtifactKindPlan, created.Kind)
	assert.Equal(t, ownerID, created.OwnerUserID)
	assert.Contains(t, created.ContentJSON, "打印机故障")
	assert.Contains(t, created.EvidenceJSON, "user_description")

	// Get：租户 + 归属双条件。
	got, err := store.Get(ctx, tenantID, ownerID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)

	// 列表：按租户 + 归属；类型过滤生效。
	list, err := store.List(ctx, tenantID, ownerID, "", 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	filtered, err := store.List(ctx, tenantID, ownerID, ArtifactKindDraft, 10)
	require.NoError(t, err)
	assert.Empty(t, filtered)

	// 证据缺省为 {}（不写空字符串）。
	noEvidence, err := store.Create(ctx, ArtifactInput{
		TenantID: tenantID, OwnerUserID: ownerID, Kind: ArtifactKindDraft,
		ToolName: "draft_kb_article", Title: "草稿", Content: map[string]interface{}{"k": "v"},
	})
	require.NoError(t, err)
	assert.Equal(t, "{}", noEvidence.EvidenceJSON)
}

func TestArtifactStore_TenantAndOwnerIsolation(t *testing.T) {
	store, ctx, tenantID, ownerID, otherUserID := newArtifactEnv(t)
	created, err := store.Create(ctx, ArtifactInput{
		TenantID: tenantID, OwnerUserID: ownerID, Kind: ArtifactKindAnalysis,
		ToolName: "analyze_ci_impact_plan", Title: "影响分析", Content: map[string]interface{}{"ci": 1},
	})
	require.NoError(t, err)

	// 同租户他人不可见（会话隔离）。
	_, err = store.Get(ctx, tenantID, otherUserID, created.ID)
	assert.Error(t, err)
	// 跨租户不可见（即便 ID 正确）。
	_, err = store.Get(ctx, tenantID+1, ownerID, created.ID)
	assert.Error(t, err)
	// List 同样不含他人产物。
	list, err := store.List(ctx, tenantID, otherUserID, "", 10)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestArtifactStore_Validation(t *testing.T) {
	store, ctx, tenantID, ownerID, _ := newArtifactEnv(t)

	_, err := store.Create(ctx, ArtifactInput{TenantID: tenantID, OwnerUserID: 0, Kind: ArtifactKindPlan})
	assert.ErrorContains(t, err, "归属发起人")

	_, err = store.Create(ctx, ArtifactInput{TenantID: tenantID, OwnerUserID: ownerID, Kind: "unknown"})
	assert.ErrorContains(t, err, "未知产物类型")

	var nilStore *ArtifactStore
	_, err = nilStore.Create(ctx, ArtifactInput{TenantID: 1, OwnerUserID: 1, Kind: ArtifactKindPlan})
	assert.ErrorContains(t, err, "未初始化")
}

func TestArtifactStore_ListLimitClamp(t *testing.T) {
	store, ctx, tenantID, ownerID, _ := newArtifactEnv(t)
	for i := 0; i < 3; i++ {
		_, err := store.Create(ctx, ArtifactInput{
			TenantID: tenantID, OwnerUserID: ownerID, Kind: ArtifactKindPlan,
			ToolName: "draft_ticket_fields", Content: map[string]interface{}{"i": i},
		})
		require.NoError(t, err)
	}
	rows, err := store.List(ctx, tenantID, ownerID, "", 0) // 0 → 默认 20
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	// 表级断言：产物类型字段形态稳定（防 schema 漂移）。
	client := store.client
	count, err := client.BotArtifact.Query().Where(botartifact.KindEQ(ArtifactKindPlan)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.True(t, strings.HasPrefix(rows[0].ToolName, "draft"))
}
