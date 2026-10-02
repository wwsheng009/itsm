package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// ApplyConsistencyChecks（IP-P2-5）启动扫描"关联表一致性"：
//  1. 悬挂成员：user_tenant_memberships 指向不存在的 users/tenants；
//  2. 组织关联跨租户：user_tenant_membership_orgs 自身 tenant_id 与所属 membership 不一致，
//     或 org_id 指向的 department/team/group/project 归属租户与 membership 不一致；
//  3. allocation provider 错配：msp_allocations.provider_tenant_id IS DISTINCT FROM 客户租户 msp_provider_id。
//
// 语义与 ApplyGuard 一致：fatal 阻断启动 / warn 记录 / silent 跳过。
// 只读；表或列尚未就绪（迁移未跑完/降级库）时跳过该项并告警，不阻断。
func ApplyConsistencyChecks(ctx context.Context, db *sql.DB, logger *zap.SugaredLogger, policy Policy) ([]string, error) {
	if policy == PolicySilent || db == nil {
		return nil, nil
	}

	checks := []struct {
		name  string
		query string
	}{
		{
			name: "dangling_memberships",
			query: `SELECT count(*) FROM user_tenant_memberships m
				LEFT JOIN users u ON u.id = m.user_id
				LEFT JOIN tenants t ON t.id = m.tenant_id
				WHERE u.id IS NULL OR t.id IS NULL`,
		},
		{
			name: "membership_org_cross_tenant",
			query: `SELECT count(*) FROM user_tenant_membership_orgs mo
				JOIN user_tenant_memberships m ON m.id = mo.membership_id
				WHERE mo.deleted_at IS NULL AND mo.tenant_id <> m.tenant_id`,
		},
		{
			name: "membership_org_target_cross_tenant",
			query: `SELECT count(*) FROM (
				SELECT mo.id FROM user_tenant_membership_orgs mo
					JOIN departments d ON d.id = mo.org_id
					WHERE mo.deleted_at IS NULL AND mo.org_type = 'department' AND d.tenant_id <> mo.tenant_id
				UNION ALL
				SELECT mo.id FROM user_tenant_membership_orgs mo
					JOIN teams t ON t.id = mo.org_id
					WHERE mo.deleted_at IS NULL AND mo.org_type = 'team' AND t.tenant_id <> mo.tenant_id
				UNION ALL
				SELECT mo.id FROM user_tenant_membership_orgs mo
					JOIN groups g ON g.id = mo.org_id
					WHERE mo.deleted_at IS NULL AND mo.org_type = 'group' AND g.tenant_id <> mo.tenant_id
				UNION ALL
				SELECT mo.id FROM user_tenant_membership_orgs mo
					JOIN projects p ON p.id = mo.org_id
					WHERE mo.deleted_at IS NULL AND mo.org_type = 'project' AND p.tenant_id <> mo.tenant_id
			) x`,
		},
	}

	// 迁移未落库（列不存在）时跳过 provider 检查（过渡期兼容）。
	if hasColumn(ctx, db, "msp_allocations", "provider_tenant_id") {
		checks = append(checks, struct {
			name  string
			query string
		}{
			name: "allocation_provider_mismatch",
			query: `SELECT count(*) FROM msp_allocations a
				JOIN tenants c ON c.id = a.customer_tenant_id
				WHERE a.provider_tenant_id IS DISTINCT FROM c.msp_provider_id`,
		})
	}

	var violations []string
	for _, ck := range checks {
		var n int
		if err := db.QueryRowContext(ctx, ck.query).Scan(&n); err != nil {
			if logger != nil {
				logger.Warnw("tenant_guard: consistency check skipped (table/column not ready)",
					"check", ck.name, "error", err)
			}
			continue
		}
		if n > 0 {
			violations = append(violations, fmt.Sprintf("%s: %d row(s)", ck.name, n))
		}
	}

	if len(violations) == 0 {
		if logger != nil {
			logger.Infow("tenant_guard: consistency pass", "checks", len(checks))
		}
		return nil, nil
	}

	msg := fmt.Sprintf("tenant_guard: %d consistency violation(s): %s",
		len(violations), strings.Join(violations, "; "))
	switch policy {
	case PolicyFatal:
		if logger != nil {
			logger.Errorw(msg, "violations", violations,
				"action", "fix cross-tenant links / backfill provider_tenant_id before start")
		}
		return violations, fmt.Errorf("%s; refusing to start (policy=fatal)", msg)
	case PolicyWarn:
		if logger != nil {
			logger.Warnw(msg, "violations", violations)
		}
		return violations, nil
	default:
		return nil, nil
	}
}

// hasColumn 以 SELECT ... LIMIT 0 探测列是否存在（Postgres/SQLite 通用，避免 information_schema 依赖）。
// table/column 均为代码内常量，不构成 SQL 注入面。
func hasColumn(ctx context.Context, db *sql.DB, table, column string) bool {
	if db == nil {
		return false
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s LIMIT 0", column, table))
	if err != nil {
		return false
	}
	// 必须显式关闭：否则连接被长期占用（单连接池下会造成后续查询死锁）。
	_ = rows.Close()
	return err == nil
}
