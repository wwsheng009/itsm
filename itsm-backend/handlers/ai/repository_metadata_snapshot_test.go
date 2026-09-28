package ai_test

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B0-01：工具元数据快照（risk/category）随 ToolInvocation 落库并可往返读取——
// 审计与审批详情据此在工具治理变更后仍能还原「调用当时」的风险与分类。
func TestEntRepository_ToolMetadataSnapshotRoundTrip(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_b0_metadata?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	tenant, err := client.Tenant.Create().SetCode("bot-meta").SetName("Bot Meta").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("bot-meta-user").
		SetEmail("bot-meta@example.com").
		SetName("Bot Meta User").
		SetPasswordHash("x").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	created, err := repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:      tenant.ID,
		UserID:        user.ID,
		ToolName:      "delete_ci_relationship",
		Arguments:     `{"relationship_id":7}`,
		Status:        "pending",
		NeedsApproval: true,
		ApprovalState: "pending",
		Provider:      "builtin",
		Risk:          "act_high",
		Category:      "cmdb",
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "act_high", created.Risk, "写入路径应回读 risk 快照")
	assert.Equal(t, "cmdb", created.Category, "写入路径应回读 category 快照")

	// 读路径（审计/审批详情）同样带出快照。
	got, err := repo.GetToolInvocation(ctx, created.ID, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, "act_high", got.Risk)
	assert.Equal(t, "cmdb", got.Category)

	// 未标注（空值）不写列：保持既有行为与列默认值不变。
	unannotated, err := repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:      tenant.ID,
		UserID:        user.ID,
		ToolName:      "get_incident_stats",
		Status:        "executed",
		ApprovalState: "auto",
	})
	require.NoError(t, err)
	assert.Empty(t, unannotated.Risk)
	assert.Empty(t, unannotated.Category)
}
