package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"itsm-backend/database"
)

// dashboardRepository 封装仪表盘复杂聚合 raw SQL。
//
// 之所以单独抽出这些方法而不是改用 Ent：
//  1. dashboard 的 AVG(...) FILTER (...) / EXTRACT(EPOCH FROM ...) / 跨月 CTE 聚合
//     在当前 Ent 生成器中没有等价的能力，硬迁会牺牲可读性并产生大量 generated 噪声。
//  2. 单一职责：service 负责业务编排和租户/权限校验；raw SQL 集中在 repository，
//     schema 变化只影响本文件，便于演进为可审计的复杂聚合出口。
//  3. 与本目录其他 repository（change/incident/ticket/problem_investigation）保持
//     同样的封装形态，避免 controller/service 中夹杂裸 SQL。
//
// 所有方法都强制 tenant_id 谓词（fail-closed），并在 db 为 nil 时返回明确错误，
// 避免生产环境误退化为 nil QueryRowContext panic。
type dashboardRepository struct {
	db *sql.DB
}

// newDashboardRepository 构造 dashboard 聚合仓储。
// 允许传入 nil（测试场景或尚未初始化的全局 rawDB），调用方在拿到 nil 仓储时
// 应当决定是否走降级路径；仓储自身方法在 nil 状态下会返回 error。
func newDashboardRepository(db *sql.DB) *dashboardRepository {
	return &dashboardRepository{db: db}
}

// requireDB 仓储方法内部前置校验：db 必须非空。
func (r *dashboardRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("dashboard repository: 数据库连接未初始化")
	}
	return nil
}

// AvgResponseAndResolutionHours 计算当前租户工单的平均首次响应时长和平均解决时长（小时）。
// 对应原 dashboard_service.go 中 GetDashboardOverviewStats 的 raw SQL。
//
// 入参：
//   - ctx：请求上下文（必须携带租户信息之外的取消/超时信号）
//   - tenantID：租户主键；缺失或非法将返回 0 值与 nil error（空集场景），不应跨租户查询
//
// 返回：avgResp（小时，可能为 0）、avgRes（小时，可能为 0）、error
func (r *dashboardRepository) AvgResponseAndResolutionHours(ctx context.Context, tenantID int) (avgResp, avgRes float64, err error) {
	if err := r.requireDB(); err != nil {
		return 0, 0, err
	}
	if tenantID <= 0 {
		return 0, 0, errors.New("dashboard repository: tenantID 必须为正整数")
	}

	const query = `
		SELECT
			COALESCE(AVG(EXTRACT(EPOCH FROM (first_response_at - created_at)) / 3600.0)
				FILTER (WHERE first_response_at IS NOT NULL), 0),
			COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 3600.0)
				FILTER (WHERE resolved_at IS NOT NULL), 0)
		FROM tickets
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`
	if _, err = database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query, tenantID).Scan(&avgResp, &avgRes)
	}); err != nil {
		return 0, 0, err
	}
	return avgResp, avgRes, nil
}

// KPIAvgAndSLAScope 计算本月/上月平均响应与解决时长 + 本月 SLA 达成 scope。
// 对应原 dashboard_service.go 中 getKPIMetrics 的 CTE raw SQL。
//
// 入参：
//   - ctx：请求上下文
//   - tenantID：租户主键
//   - thisMonthStart：本月窗口起点（含）
//   - lastMonthStart：上月窗口起点（含），用于本月/上月对比及 SLA scope 起点
//
// 返回：
//   - avgRespNow / avgResNow：本月平均首次响应 / 解决时长（小时）
//   - avgRespPrev / avgResPrev：上月平均首次响应 / 解决时长（小时）
//   - totalSLATickets / metSLATickets：本月参与 SLA 考核的工单数 / 达成数
//
// 时间窗口语义：current_month = [thisMonthStart, +∞)、prev_month =
// [lastMonthStart, thisMonthStart)、sla_scope = [lastMonthStart, +∞)。
// 与原 SQL 完全一致，迁移到本仓储后口径不变。
func (r *dashboardRepository) KPIAvgAndSLAScope(
	ctx context.Context,
	tenantID int,
	thisMonthStart, lastMonthStart time.Time,
) (
	avgRespNow, avgResNow, avgRespPrev, avgResPrev float64,
	totalSLATickets, metSLATickets int,
	err error,
) {
	if err := r.requireDB(); err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	if tenantID <= 0 {
		return 0, 0, 0, 0, 0, 0, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if thisMonthStart.IsZero() || lastMonthStart.IsZero() {
		return 0, 0, 0, 0, 0, 0, errors.New("dashboard repository: 时间窗口不能为零值")
	}
	if !lastMonthStart.Before(thisMonthStart) {
		return 0, 0, 0, 0, 0, 0, errors.New("dashboard repository: lastMonthStart 必须早于 thisMonthStart")
	}

	const query = `
		WITH current_month AS (
			SELECT id, first_response_at, resolved_at, created_at
			FROM tickets
			WHERE tenant_id = $1 AND deleted_at IS NULL AND created_at >= $2
		),
		prev_month AS (
			SELECT id, first_response_at, resolved_at, created_at
			FROM tickets
			WHERE tenant_id = $1 AND deleted_at IS NULL AND created_at >= $3 AND created_at < $2
		),
		sla_scope AS (
			SELECT id, first_response_at, resolved_at, created_at, sla_response_deadline, sla_resolution_deadline
			FROM tickets
			WHERE tenant_id = $1 AND deleted_at IS NULL AND created_at >= $3
		)
		SELECT
			COALESCE((SELECT AVG(EXTRACT(EPOCH FROM (first_response_at - created_at)) / 3600.0)
			           FROM current_month WHERE first_response_at IS NOT NULL), 0),
			COALESCE((SELECT AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 3600.0)
			           FROM current_month WHERE resolved_at IS NOT NULL), 0),
			COALESCE((SELECT AVG(EXTRACT(EPOCH FROM (first_response_at - created_at)) / 3600.0)
			           FROM prev_month WHERE first_response_at IS NOT NULL), 0),
			COALESCE((SELECT AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 3600.0)
			           FROM prev_month WHERE resolved_at IS NOT NULL), 0),
			(SELECT COUNT(*) FROM sla_scope),
			(SELECT COUNT(*) FROM sla_scope WHERE (
				sla_response_deadline IS NULL OR first_response_at <= sla_response_deadline
			) AND (
				sla_resolution_deadline IS NULL OR resolved_at <= sla_resolution_deadline
			))
	`
	if _, err = database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query, tenantID, thisMonthStart, lastMonthStart).Scan(
			&avgRespNow, &avgResNow, &avgRespPrev, &avgResPrev, &totalSLATickets, &metSLATickets,
		)
	}); err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	return avgRespNow, avgResNow, avgRespPrev, avgResPrev, totalSLATickets, metSLATickets, nil
}

// PreviousSLAScopeCount 计算上月 SLA 达成 scope，用于计算 SLA 达成率环比变化。
// 对应原 dashboard_service.go 中 getKPIMetrics 第二段 raw SQL。
//
// 入参：
//   - ctx：请求上下文
//   - tenantID：租户主键
//   - lastMonthStart：上月窗口起点（含）
//   - thisMonthStart：本月窗口起点（不含），用于圈定上月窗口上界
//
// 返回：上月参与 SLA 考核的工单数 / 达成数。当窗口为空时返回 0,0,nil。
func (r *dashboardRepository) PreviousSLAScopeCount(
	ctx context.Context,
	tenantID int,
	lastMonthStart, thisMonthStart time.Time,
) (totalSLATickets, metSLATickets int, err error) {
	if err := r.requireDB(); err != nil {
		return 0, 0, err
	}
	if tenantID <= 0 {
		return 0, 0, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if thisMonthStart.IsZero() || lastMonthStart.IsZero() {
		return 0, 0, errors.New("dashboard repository: 时间窗口不能为零值")
	}
	if !lastMonthStart.Before(thisMonthStart) {
		return 0, 0, errors.New("dashboard repository: lastMonthStart 必须早于 thisMonthStart")
	}

	const query = `
		SELECT COUNT(*), COUNT(*) FILTER (
			WHERE (sla_response_deadline IS NULL OR first_response_at <= sla_response_deadline)
			AND (sla_resolution_deadline IS NULL OR resolved_at <= sla_resolution_deadline)
		)
		FROM tickets
		WHERE tenant_id = $1 AND deleted_at IS NULL AND created_at >= $2 AND created_at < $3
	`
	if _, err = database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query, tenantID, lastMonthStart, thisMonthStart).Scan(&totalSLATickets, &metSLATickets)
	}); err != nil {
		return 0, 0, err
	}
	return totalSLATickets, metSLATickets, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 仪表盘批量聚合（2026-09-22 性能优化）
//
// 背景：/api/v1/dashboard/overview 原先在 service 层用 Ent 逐项 Count()/All()，
// 单次请求产生约 145 次数据库事务（7 天趋势 = 7×5 次 COUNT、事件分布 = 5 次、
// 团队负载逐人查用户 N+1、KPI 8 次 COUNT + 概览 4 次 COUNT…）。当数据库跨
// SSH 隧道访问（单次往返 4~6ms）时，往返次数被直接放大成 620ms~2s 的接口延迟。
//
// 以下方法把同一业务语义压缩为单条聚合 SQL（FILTER / GROUP BY / LEFT JOIN），
// 使 overview 的数据库往返从约 78 次降到约 10 次。每条 SQL 的口径都与被替换的
// Ent 查询逐字段对齐（含 deleted_at、时间窗口、状态集合、排序与截断规则），
// 调用方在 raw DB 不可用或聚合失败时回退到原 Ent 实现。
// ─────────────────────────────────────────────────────────────────────────────

// TicketTrendRow 单日工单趋势聚合结果，按 ord 与调用方传入的 dayStarts 一一对应。
type TicketTrendRow struct {
	Open       int
	InProgress int
	Resolved   int
	Closed     int
	NewTickets int
}

// TicketTrendDaily 一次查询返回多日工单趋势（原实现为每天 5 次 Count）。
//
// 口径与 Ent 版逐日统计完全一致：
//   - Open:       status ∈ pendingStatuses   且 created_at ∈ [dayStart, dayEnd]
//   - InProgress: status = inProgressStatus  且 created_at ∈ 窗口
//   - Resolved:   status ∈ completedStatuses 且 updated_at ∈ 窗口
//   - Closed:     status = closedStatus      且 updated_at ∈ 窗口
//   - NewTickets: created_at ∈ 窗口（不限状态，也不过滤 deleted_at，与 Ent 版一致）
//
// 每日边界由调用方用 time.Local 计算后传入，避免把日期分桶交给数据库 session 时区。
func (r *dashboardRepository) TicketTrendDaily(
	ctx context.Context,
	tenantID int,
	dayStarts []time.Time,
	pendingStatuses, completedStatuses []string,
	inProgressStatus, closedStatus string,
) ([]TicketTrendRow, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if len(dayStarts) == 0 {
		return nil, errors.New("dashboard repository: 趋势窗口不能为空")
	}

	const query = `
		WITH days AS (
			SELECT u.ord, u.ts AS day_start, u.ts + interval '24 hours' - interval '1 second' AS day_end
			FROM unnest($2::timestamptz[]) WITH ORDINALITY AS u(ts, ord)
		),
		created_agg AS (
			SELECT d.ord,
			       COUNT(*) FILTER (WHERE t.status = ANY($3::text[])) AS open_cnt,
			       COUNT(*) FILTER (WHERE t.status = $4)              AS in_progress_cnt,
			       COUNT(*)                                           AS new_cnt
			FROM days d
			JOIN tickets t ON t.tenant_id = $1
			  AND t.created_at >= d.day_start AND t.created_at <= d.day_end
			GROUP BY d.ord
		),
		updated_agg AS (
			SELECT d.ord,
			       COUNT(*) FILTER (WHERE t.status = ANY($5::text[])) AS resolved_cnt,
			       COUNT(*) FILTER (WHERE t.status = $6)              AS closed_cnt
			FROM days d
			JOIN tickets t ON t.tenant_id = $1
			  AND t.updated_at >= d.day_start AND t.updated_at <= d.day_end
			GROUP BY d.ord
		)
		SELECT d.ord,
		       COALESCE(c.open_cnt, 0), COALESCE(c.in_progress_cnt, 0),
		       COALESCE(u.resolved_cnt, 0), COALESCE(u.closed_cnt, 0),
		       COALESCE(c.new_cnt, 0)
		FROM days d
		LEFT JOIN created_agg c ON c.ord = d.ord
		LEFT JOIN updated_agg u ON u.ord = d.ord
		ORDER BY d.ord
	`

	rows := make([]TicketTrendRow, 0, len(dayStarts))
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		rs, qerr := q.QueryContext(ctx, query,
			tenantID,
			pq.Array(dayStarts),
			pq.Array(pendingStatuses),
			inProgressStatus,
			pq.Array(completedStatuses),
			closedStatus,
		)
		if qerr != nil {
			return struct{}{}, qerr
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var ord int
			var row TicketTrendRow
			if serr := rs.Scan(&ord, &row.Open, &row.InProgress, &row.Resolved, &row.Closed, &row.NewTickets); serr != nil {
				return struct{}{}, serr
			}
			rows = append(rows, row)
		}
		return struct{}{}, rs.Err()
	}); err != nil {
		return nil, err
	}
	if len(rows) != len(dayStarts) {
		return nil, fmt.Errorf("dashboard repository: 趋势返回 %d 行，与窗口 %d 天不一致", len(rows), len(dayStarts))
	}
	return rows, nil
}

// IncidentCountsByCategory 一次查询返回租户下按 category 分组的计数（原实现为每个分类 1 次 Count）。
// 未出现在结果中的分类由调用方补 0；结果中未在展示白名单内的分类由调用方忽略，与原逻辑一致。
func (r *dashboardRepository) IncidentCountsByCategory(ctx context.Context, tenantID int) (map[string]int, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}

	const query = `
		SELECT category, COUNT(*)
		FROM incidents
		WHERE tenant_id = $1
		GROUP BY category
	`
	counts := make(map[string]int)
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		rs, qerr := q.QueryContext(ctx, query, tenantID)
		if qerr != nil {
			return struct{}{}, qerr
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var category sql.NullString
			var count int
			if serr := rs.Scan(&category, &count); serr != nil {
				return struct{}{}, serr
			}
			counts[category.String] = count
		}
		return struct{}{}, rs.Err()
	}); err != nil {
		return nil, err
	}
	return counts, nil
}

// ResponseTimeBuckets 一次查询返回首次响应时长的分桶统计（原实现为全表加载后在内存分桶）。
//
// 返回字段与 getResponseTimeDistribution 的中间状态一一对应：
// Counts/Sums 下标 0..3 分别对应 0-1h / 1-4h / 4-8h / >8h；Total 为参与统计的工单总数
// （= 有 first_response_at 的工单数，与原实现 totalTickets 同口径）；Negative 为
// first_response_at < created_at 的异常记录数——原实现中这类记录计入分母但不落入任何桶，
// 因此这里单独返回以保持百分比口径不变。
type ResponseTimeBuckets struct {
	Total    int
	Negative int
	Counts   [4]int
	Sums     [4]float64
}

// ResponseTimeBuckets 见结构体注释。
func (r *dashboardRepository) ResponseTimeBuckets(ctx context.Context, tenantID int) (*ResponseTimeBuckets, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}

	const query = `
		SELECT
			COUNT(*)                                                                   AS total,
			COUNT(*) FILTER (WHERE diff < 0)                                           AS negative,
			COUNT(*) FILTER (WHERE diff >= 0 AND diff < 1)                             AS b0_1,
			COUNT(*) FILTER (WHERE diff >= 1 AND diff < 4)                             AS b1_4,
			COUNT(*) FILTER (WHERE diff >= 4 AND diff < 8)                             AS b4_8,
			COUNT(*) FILTER (WHERE diff >= 8)                                          AS b8p,
			COALESCE(SUM(diff) FILTER (WHERE diff >= 0 AND diff < 1), 0)               AS s0_1,
			COALESCE(SUM(diff) FILTER (WHERE diff >= 1 AND diff < 4), 0)               AS s1_4,
			COALESCE(SUM(diff) FILTER (WHERE diff >= 4 AND diff < 8), 0)               AS s4_8,
			COALESCE(SUM(diff) FILTER (WHERE diff >= 8), 0)                            AS s8p
		FROM (
			SELECT EXTRACT(EPOCH FROM (first_response_at - created_at)) / 3600.0 AS diff
			FROM tickets
			WHERE tenant_id = $1 AND first_response_at IS NOT NULL
		) x
	`
	out := &ResponseTimeBuckets{}
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query, tenantID).Scan(
			&out.Total, &out.Negative,
			&out.Counts[0], &out.Counts[1], &out.Counts[2], &out.Counts[3],
			&out.Sums[0], &out.Sums[1], &out.Sums[2], &out.Sums[3],
		)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// TeamWorkloadRow 团队负载单行聚合结果。
type TeamWorkloadRow struct {
	AssigneeID      int
	AssigneeName    string
	TicketCount     int
	CompletedCount  int
	ActiveCount     int
	AvgResponseTime float64
}

// TeamWorkloadTopN 一次查询返回团队负载 TopN（原实现为全量加载 + 每个处理人一次 User.Get 的 N+1）。
//
// 语义对齐：
//   - 只统计 assignee_id IS NOT NULL 且 <> 0 的工单（原实现过滤 NotNil 后在循环内跳过 0）；
//   - 处理人姓名优先级 name → username → "用户{id}"（原实现 User.Get 失败时同样回退占位名）；
//   - completedCount: status ∈ completedStatuses；activeCount: status = inProgress 或 ∈ pending；
//   - avgResponseTime: 仅统计 first_response_at 非空的记录（小时）；
//   - 按 ticketCount 降序截断 limit 条（原实现先全量分组再排序取前 10，此处下推到 SQL）。
func (r *dashboardRepository) TeamWorkloadTopN(
	ctx context.Context,
	tenantID int,
	limit int,
	pendingStatuses, completedStatuses []string,
	inProgressStatus string,
) ([]TeamWorkloadRow, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if limit <= 0 {
		return []TeamWorkloadRow{}, nil
	}

	const query = `
		SELECT
			t.assignee_id,
			COALESCE(NULLIF(u.name, ''), NULLIF(u.username, ''), '用户' || t.assignee_id::text) AS assignee_name,
			COUNT(*)                                                        AS ticket_count,
			COUNT(*) FILTER (WHERE t.status = ANY($3::text[]))              AS completed_count,
			COUNT(*) FILTER (WHERE t.status = $4 OR t.status = ANY($5::text[])) AS active_count,
			COALESCE(AVG(EXTRACT(EPOCH FROM (t.first_response_at - t.created_at)) / 3600.0)
			         FILTER (WHERE t.first_response_at IS NOT NULL), 0)     AS avg_response_hours
		FROM tickets t
		LEFT JOIN users u ON u.id = t.assignee_id
		WHERE t.tenant_id = $1 AND t.assignee_id IS NOT NULL AND t.assignee_id <> 0
		GROUP BY t.assignee_id, u.name, u.username
		ORDER BY ticket_count DESC, t.assignee_id ASC
		LIMIT $2
	`
	out := make([]TeamWorkloadRow, 0, limit)
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		rs, qerr := q.QueryContext(ctx, query, tenantID, limit, pq.Array(completedStatuses), inProgressStatus, pq.Array(pendingStatuses))
		if qerr != nil {
			return struct{}{}, qerr
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var row TeamWorkloadRow
			if serr := rs.Scan(&row.AssigneeID, &row.AssigneeName, &row.TicketCount, &row.CompletedCount, &row.ActiveCount, &row.AvgResponseTime); serr != nil {
				return struct{}{}, serr
			}
			out = append(out, row)
		}
		return struct{}{}, rs.Err()
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// OverviewStatsAgg 一次查询返回 /dashboard/overview 顶部扁平统计
// （原先为 4 次 Count + 1 次 AvgResponseAndResolutionHours）。
//
// 口径与 Ent 版逐项查询一致：均要求 deleted_at IS NULL；ResolvedToday 取
// updated_at >= todayStart 且状态属于 completedStatuses。
type OverviewStatsAgg struct {
	TotalTickets      int
	PendingTickets    int
	InProgressTickets int
	ResolvedToday     int
	AvgResponseHours  float64
	AvgResolveHours   float64
}

// OverviewStatsAgg 见结构体注释。
func (r *dashboardRepository) OverviewStatsAgg(
	ctx context.Context,
	tenantID int,
	todayStart time.Time,
	pendingStatuses, completedStatuses []string,
	inProgressStatus string,
) (*OverviewStatsAgg, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if todayStart.IsZero() {
		return nil, errors.New("dashboard repository: todayStart 不能为零值")
	}

	const query = `
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($2::text[])),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = $3),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($4::text[]) AND updated_at >= $5),
			COALESCE(AVG(EXTRACT(EPOCH FROM (first_response_at - created_at)) / 3600.0)
			         FILTER (WHERE deleted_at IS NULL AND first_response_at IS NOT NULL), 0),
			COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 3600.0)
			         FILTER (WHERE deleted_at IS NULL AND resolved_at IS NOT NULL), 0)
		FROM tickets
		WHERE tenant_id = $1
	`
	out := &OverviewStatsAgg{}
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query,
			tenantID, pq.Array(pendingStatuses), inProgressStatus, pq.Array(completedStatuses), todayStart,
		).Scan(
			&out.TotalTickets, &out.PendingTickets, &out.InProgressTickets, &out.ResolvedToday,
			&out.AvgResponseHours, &out.AvgResolveHours,
		)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// KPITicketCounts 一次查询返回 KPI 卡片所需的 8 个工单计数
// （原先为 8 次独立 Count）。口径与 Ent 版逐项查询严格一致：
//   - Total:            全部未删除工单（原实现未加时间窗口，保持原样）；
//   - LastMonthTotal/Pending/InProgress/Completed: created_at ∈ [lastMonthStart, lastMonthEnd] 且未删除；
//   - Pending/InProgress: 当前状态集合计数，未删除；
//   - ThisMonthCompleted: status ∈ completed 且 created_at >= thisMonthStart，未删除。
type KPITicketCounts struct {
	Total               int
	LastMonthTotal      int
	Pending             int
	LastMonthPending    int
	InProgress          int
	LastMonthInProgress int
	CompletedThisMonth  int
	CompletedLastMonth  int
}

// KPITicketCounts 见结构体注释。
func (r *dashboardRepository) KPITicketCounts(
	ctx context.Context,
	tenantID int,
	thisMonthStart, lastMonthStart, lastMonthEnd time.Time,
	pendingStatuses, completedStatuses []string,
	inProgressStatus string,
) (*KPITicketCounts, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if tenantID <= 0 {
		return nil, errors.New("dashboard repository: tenantID 必须为正整数")
	}
	if thisMonthStart.IsZero() || lastMonthStart.IsZero() || lastMonthEnd.IsZero() {
		return nil, errors.New("dashboard repository: 时间窗口不能为零值")
	}

	const query = `
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND created_at >= $2 AND created_at <= $3),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($4::text[])),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($4::text[]) AND created_at >= $2 AND created_at <= $3),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = $5),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = $5 AND created_at >= $2 AND created_at <= $3),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($6::text[]) AND created_at >= $7),
			COUNT(*) FILTER (WHERE deleted_at IS NULL AND status = ANY($6::text[]) AND created_at >= $2 AND created_at <= $3)
		FROM tickets
		WHERE tenant_id = $1
	`
	out := &KPITicketCounts{}
	if _, err := database.WithTenantSQL(ctx, r.db, tenantID, func(q database.SQLExecutor) (struct{}, error) {
		return struct{}{}, q.QueryRowContext(ctx, query,
			tenantID, lastMonthStart, lastMonthEnd,
			pq.Array(pendingStatuses), inProgressStatus, pq.Array(completedStatuses), thisMonthStart,
		).Scan(
			&out.Total, &out.LastMonthTotal, &out.Pending, &out.LastMonthPending,
			&out.InProgress, &out.LastMonthInProgress, &out.CompletedThisMonth, &out.CompletedLastMonth,
		)
	}); err != nil {
		return nil, err
	}
	return out, nil
}
