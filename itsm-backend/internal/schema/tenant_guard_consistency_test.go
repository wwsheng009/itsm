package schema

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestApplyConsistencyChecks_Silent(t *testing.T) {
	violations, err := ApplyConsistencyChecks(context.TODO(), nil, nil, PolicySilent)
	require.NoError(t, err)
	assert.Nil(t, violations)
}

func setupConsistencyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:guard_consistency?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// 单连接：共享内存库需在同一连接上保持存活，避免连接池换连接时丢失 schema。
	db.SetMaxOpenConns(1)
	stmts := []string{
		`CREATE TABLE users (id integer primary key, tenant_id int)`,
		`CREATE TABLE tenants (id integer primary key, msp_provider_id int)`,
		`CREATE TABLE user_tenant_memberships (id integer primary key, user_id int, tenant_id int)`,
		`CREATE TABLE user_tenant_membership_orgs (id integer primary key, membership_id int, tenant_id int, org_type text, org_id int, deleted_at timestamp null)`,
		`CREATE TABLE departments (id integer primary key, tenant_id int)`,
		`CREATE TABLE teams (id integer primary key, tenant_id int)`,
		`CREATE TABLE groups (id integer primary key, tenant_id int)`,
		`CREATE TABLE projects (id integer primary key, tenant_id int)`,
		`CREATE TABLE msp_allocations (id integer primary key, customer_tenant_id int, provider_tenant_id int, deassigned_at timestamp null)`,
		`INSERT INTO users (id, tenant_id) VALUES (1, 1)`,
		`INSERT INTO tenants (id, msp_provider_id) VALUES (1, 100)`,
	}
	for _, s := range stmts {
		_, err := db.Exec(s)
		require.NoError(t, err, s)
	}
	return db
}

// TestApplyConsistencyChecks_DetectsViolations（IP-P2-5）：三类违规均可检出；fatal 阻断、warn 放行。
func TestApplyConsistencyChecks_DetectsViolations(t *testing.T) {
	db := setupConsistencyDB(t)
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	// 干净样本：先通过。
	violations, err := ApplyConsistencyChecks(ctx, db, logger, PolicyWarn)
	require.NoError(t, err)
	assert.Empty(t, violations)

	// 注入违规：悬挂成员（user 999 不存在）。
	_, err = db.Exec(`INSERT INTO user_tenant_memberships (id, user_id, tenant_id) VALUES (1, 999, 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO user_tenant_memberships (id, user_id, tenant_id) VALUES (2, 1, 1)`)
	require.NoError(t, err)
	// 组织关联自身 tenant 跨租户（membership 2 属租户 1，关联行写租户 2）。
	_, err = db.Exec(`INSERT INTO user_tenant_membership_orgs (id, membership_id, tenant_id, org_type, org_id, deleted_at) VALUES (1, 2, 2, 'department', 10, NULL)`)
	require.NoError(t, err)
	// 目标组织跨租户（project 20 属租户 4）。
	_, err = db.Exec(`INSERT INTO user_tenant_membership_orgs (id, membership_id, tenant_id, org_type, org_id, deleted_at) VALUES (2, 2, 1, 'project', 20, NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO departments (id, tenant_id) VALUES (10, 3)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO projects (id, tenant_id) VALUES (20, 4)`)
	require.NoError(t, err)
	// allocation provider 错配（客户 1 的 provider=100，分配写 999）。
	_, err = db.Exec(`INSERT INTO msp_allocations (id, customer_tenant_id, provider_tenant_id) VALUES (1, 1, 999)`)
	require.NoError(t, err)

	violations, err = ApplyConsistencyChecks(ctx, db, logger, PolicyWarn)
	require.NoError(t, err)
	joined := strings.Join(violations, " ")
	assert.Contains(t, joined, "dangling_memberships")
	assert.Contains(t, joined, "membership_org_cross_tenant")
	assert.Contains(t, joined, "membership_org_target_cross_tenant")
	assert.Contains(t, joined, "allocation_provider_mismatch")

	// fatal 策略阻断启动。
	_, err = ApplyConsistencyChecks(ctx, db, logger, PolicyFatal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to start")
}

// TestApplyConsistencyChecks_SkipsWhenProviderColumnMissing：迁移未落库（列不存在）时跳过 provider 检查。
func TestApplyConsistencyChecks_SkipsWhenProviderColumnMissing(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:guard_consistency_nocol?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE msp_allocations (id integer primary key, customer_tenant_id int)`)
	require.NoError(t, err)

	// 其它表缺失 → 各检查跳过（warn 不阻断）；provider 列不存在 → 不纳入检查。
	violations, err := ApplyConsistencyChecks(context.Background(), db, zaptest.NewLogger(t).Sugar(), PolicyWarn)
	require.NoError(t, err)
	assert.Empty(t, violations)
}
