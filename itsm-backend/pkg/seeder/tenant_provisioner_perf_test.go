package seeder

import (
	"context"
	"sync/atomic"
	"testing"

	"itsm-backend/config"
	"itsm-backend/ent"
	"itsm-backend/ent/tenant"
	"itsm-backend/pkg/tenantmode"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// countingDriver 统计底层 SQL 次数（含事务内），用于锁定「批量供给」的查询预算。
// 旧实现逐行查询（约 4–5k 次），在跨网/隧道环境下被往返延迟放大成分钟级；
// 本守护确保克隆与校验保持「每类资源常数次」批量查询。
type countingDriver struct {
	dialect.Driver
	queries atomic.Int64
}

func (d *countingDriver) Query(ctx context.Context, query string, args, v any) error {
	d.queries.Add(1)
	return d.Driver.Query(ctx, query, args, v)
}

func (d *countingDriver) Exec(ctx context.Context, query string, args, v any) error {
	d.queries.Add(1)
	return d.Driver.Exec(ctx, query, args, v)
}

func (d *countingDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return &countingTx{Tx: tx, counter: d}, nil
}

type countingTx struct {
	dialect.Tx
	counter *countingDriver
}

func (t *countingTx) Query(ctx context.Context, query string, args, v any) error {
	t.counter.queries.Add(1)
	return t.Tx.Query(ctx, query, args, v)
}

func (t *countingTx) Exec(ctx context.Context, query string, args, v any) error {
	t.counter.queries.Add(1)
	return t.Tx.Exec(ctx, query, args, v)
}

// TestProvisionTenantQueryBudgetStaysBatched 锁定租户供给的查询预算：
// 全新租户（含 ~1k 授权批量写入）与幂等复跑都必须保持在百次量级。
func TestProvisionTenantQueryBudgetStaysBatched(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("SEED_USER1_PASSWORD", "user123")
	t.Setenv("SEED_SECURITY1_PASSWORD", "sec123")

	drv, err := entsql.Open("sqlite3", "file:provision-budget?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	counter := &countingDriver{Driver: drv}
	client := ent.NewClient(ent.Driver(counter))
	t.Cleanup(func() {
		client.Close()
	})
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))

	cfg := &config.Config{
		Deployment: config.DeploymentConfig{
			Mode: tenantmode.DeploymentModeSaaS,
		},
	}
	seeder := NewSeeder(client, zap.NewNop().Sugar(), cfg)
	require.NoError(t, seeder.SeedProduction(ctx))

	target, err := client.Tenant.Create().
		SetName("Query Budget Target").
		SetCode("query-budget-target").
		SetType(tenant.TypeSaasCustomer).
		Save(ctx)
	require.NoError(t, err)

	before := counter.queries.Load()
	require.NoError(t, seeder.ProvisionTenant(ctx, target.ID, CurrentTenantTemplateVersion))
	fresh := counter.queries.Load() - before

	before = counter.queries.Load()
	require.NoError(t, seeder.ProvisionTenant(ctx, target.ID, CurrentTenantTemplateVersion))
	rerun := counter.queries.Load() - before

	t.Logf("provision queries: fresh=%d idempotent-rerun=%d", fresh, rerun)
	assert.Less(t, fresh, int64(300), "fresh provision query budget (N+1 regression?)")
	assert.Less(t, rerun, int64(300), "idempotent re-run query budget (N+1 regression?)")
}
