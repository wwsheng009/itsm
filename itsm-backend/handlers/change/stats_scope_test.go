package change

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/common/datascope"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatsRepository_RespectsDataScope 锁定变更统计与列表同口径：
// owner 仅统计本人创建/受理，all 统计租户全量。
func TestStatsRepository_RespectsDataScope(t *testing.T) {
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:change_stats_scope_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	defer client.Close()
	ctx := context.Background()

	tenantEntity, err := client.Tenant.Create().
		SetName("Change Stats Tenant").
		SetCode("change-stats").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	owner, err := client.User.Create().
		SetUsername("change-owner").
		SetEmail("owner@example.com").
		SetName("Owner").
		SetPasswordHash("hash").
		SetTenantID(tenantEntity.ID).
		Save(ctx)
	require.NoError(t, err)
	other, err := client.User.Create().
		SetUsername("change-other").
		SetEmail("other@example.com").
		SetName("Other").
		SetPasswordHash("hash").
		SetTenantID(tenantEntity.ID).
		Save(ctx)
	require.NoError(t, err)

	createChange := func(title string, createdBy int, assignee *int) *ent.Change {
		builder := client.Change.Create().
			SetTitle(title).
			SetCreatedBy(createdBy).
			SetTenantID(tenantEntity.ID)
		if assignee != nil {
			builder = builder.SetAssigneeID(*assignee)
		}
		entity, err := builder.Save(ctx)
		require.NoError(t, err)
		return entity
	}
	createChange("owner-1", owner.ID, nil)
	createChange("owner-2", owner.ID, nil)
	createChange("other-1", other.ID, &owner.ID) // owner 受理

	repo := newStatsRepository(client)

	ownerStats, err := repo.GetStatsScoped(ctx, tenantEntity.ID, datascope.DataScopeOwnedOrAssigned, owner.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, ownerStats.Total, "owner 创建的 2 条 + 受理的 1 条")

	otherStats, err := repo.GetStatsScoped(ctx, tenantEntity.ID, datascope.DataScopeOwnedOrAssigned, other.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, otherStats.Total)

	allStats, err := repo.GetStatsScoped(ctx, tenantEntity.ID, datascope.DataScopeAll, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, allStats.Total)
}
