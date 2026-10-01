package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IP-P1-3b 组织唯一约束租户化回归：
//   - 同名团队/组、同码项目在**不同租户**可共存（原 project.code 全局唯一会误伤）；
//   - 同租户内重复被 DB 拒绝；
//   - team 软删后同名可重建（部分唯一索引 WHERE deleted_at IS NULL）。
//
// Postgres 侧同口径索引由迁移 20260506 落地。
// =============================================================================

func TestOrgScopedUniqueConstraints(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:org_unique_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()

	tenantA, err := client.Tenant.Create().SetName("UQ A").SetCode("uq-a").SetStatus("active").SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	tenantB, err := client.Tenant.Create().SetName("UQ B").SetCode("uq-b").SetStatus("active").SetType("msp_customer").Save(ctx)
	require.NoError(t, err)

	// ---- team：跨租户同名 OK；同租户重复拒绝；软删后可重建 ----
	teamA, err := client.Team.Create().SetName("Support").SetCode("support-a").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.Team.Create().SetName("Support").SetCode("support-b").SetTenantID(tenantB.ID).Save(ctx)
	require.NoError(t, err, "跨租户同名团队必须允许")

	_, err = client.Team.Create().SetName("Support").SetCode("support-a2").SetTenantID(tenantA.ID).Save(ctx)
	require.Error(t, err, "同租户同名团队必须被拒绝")

	require.NoError(t, client.Team.UpdateOneID(teamA.ID).SetDeletedAt(time.Now()).Exec(ctx))
	_, err = client.Team.Create().SetName("Support").SetCode("support-a3").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err, "软删团队名应可重用（部分唯一索引）")

	// ---- group：跨租户同名 OK；同租户重复拒绝 ----
	_, err = client.Group.Create().SetName("Ops").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.Group.Create().SetName("Ops").SetTenantID(tenantB.ID).Save(ctx)
	require.NoError(t, err, "跨租户同名组必须允许")

	_, err = client.Group.Create().SetName("Ops").SetTenantID(tenantA.ID).Save(ctx)
	require.Error(t, err, "同租户同名组必须被拒绝")

	// ---- project：跨租户同码 OK；同租户重复拒绝 ----
	_, err = client.Project.Create().SetName("Proj A").SetCode("PRJ-1").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.Project.Create().SetName("Proj B").SetCode("PRJ-1").SetTenantID(tenantB.ID).Save(ctx)
	require.NoError(t, err, "跨租户同码项目必须允许（替换全局唯一）")

	_, err = client.Project.Create().SetName("Proj A2").SetCode("PRJ-1").SetTenantID(tenantA.ID).Save(ctx)
	require.Error(t, err, "同租户同码项目必须被拒绝")
}
