package incident

import (
	"context"
	"database/sql"
	"fmt"

	"itsm-backend/handlers/common/datascope"
)

// incidentStatsRepository 负责 incidents 表的聚合统计。
//
// 说明：当前聚合同时依赖 PostgreSQL 专有语法：
//   - COUNT(*) FILTER (WHERE ...) — 聚合过滤
//   - EXTRACT(EPOCH FROM (resolved_at - created_at)) — 时间差转秒
//
// 这些语法没有 Ent 等价的"跨方言"表达。封装到 repository 的目的：
//  1. 把 SQL 字符串集中到一处，未来加方言兼容只需改这里；
//  2. 强制 tenant_id 谓词、deleted_at 守卫（与 incident schema 软删语义一致）；
//  3. 调用方只看到面向对象方法 GetStats(ctx, tenantID)。
//
// 注意：若需要 SQLite 兼容，应提供 SQLite 后端实现（独立文件或 build tag），
// 当前后端默认 PostgreSQL，SQLite 仅用于测试。
type incidentStatsRepository struct {
	db *sql.DB
}

func newIncidentStatsRepository(db *sql.DB) *incidentStatsRepository {
	if db == nil {
		return nil
	}
	return &incidentStatsRepository{db: db}
}

// GetStats 一次性返回 IncidentStats 所需的全部聚合指标。
//   - total/open/critical/major/resolved: COUNT(*) FILTER 一次完成
//   - avgResolutionTime: 已解决/已关闭事件的平均 (resolved_at - created_at) 分钟数；
//     未解决事件排除；空集通过 COALESCE 返回 0 而非 NULL。
func (r *incidentStatsRepository) GetStats(ctx context.Context, tenantID int) (*IncidentStats, error) {
	return r.GetStatsScoped(ctx, tenantID, datascope.DataScopeAll, 0)
}

// GetStatsScoped 与列表同 DataScope 口径：all/department 统计租户全量，
// owner 仅统计报告人或受理人为当前用户的事件。
func (r *incidentStatsRepository) GetStatsScoped(ctx context.Context, tenantID int, dataScope datascope.DataScope, currentUserID int) (*IncidentStats, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("incident stats repository requires tenantID")
	}
	scopeClause := ""
	args := []any{tenantID}
	if !datascope.IsTenantWide(dataScope) {
		if currentUserID <= 0 {
			scopeClause = " AND FALSE"
		} else {
			scopeClause = " AND (reporter_id = $2 OR assignee_id = $2)"
			args = append(args, currentUserID)
		}
	}
	query := fmt.Sprintf(`
		SELECT
		  COUNT(*) FILTER (WHERE TRUE) AS total,
		  COUNT(*) FILTER (WHERE status IN ('open','in_progress')) AS open,
		  COUNT(*) FILTER (WHERE priority = 'critical') AS critical,
		  COUNT(*) FILTER (WHERE priority = 'high') AS major,
		  COUNT(*) FILTER (WHERE status IN ('resolved','closed')) AS resolved,
		  COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 60.0)
		           FILTER (WHERE status IN ('resolved','closed') AND resolved_at IS NOT NULL),
		           0)::int AS avg_minutes
		FROM incidents
		WHERE tenant_id = $1 AND deleted_at IS NULL%s
	`, scopeClause)

	stats := &IncidentStats{}
	row := r.db.QueryRowContext(ctx, query, args...)
	if err := row.Scan(
		&stats.TotalIncidents,
		&stats.OpenIncidents,
		&stats.CriticalIncidents,
		&stats.MajorIncidents,
		&stats.ResolvedIncidents,
		&stats.AvgResolutionTime,
	); err != nil {
		return nil, fmt.Errorf("scan incident stats: %w", err)
	}
	return stats, nil
}
