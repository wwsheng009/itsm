// msp_records_summary.go：MSP dashboard 跨租户协助单据总览（只读计数投影）。
//
// 契约：GET /api/v1/msp/workbench/records-summary（路由声明 msp_ticket:read，
// 与 ListRecords 同授权面）。设计约束与 msp_record_projection.go 一致：
//   1. 允许集合一律来自服务端（actor.AllowedCustomers）；逐租户查询 + 内存合并（RLS 友好）；
//   2. 每类单据返回 total（全量）/ open（非终态）计数 + 最近更新条目（updated_at 降序）；
//   3. 不下发 allowedActions / 明细字段——本端点是 dashboard 汇总卡数据源，明细操作
//      引导至工作台（problems/changes/service-requests 列表）或客户侧页面；
//   4. 暂停/过期客户仍可读（镜像工单读面）；known_error 无软删字段，incident 同。
//
// 口径说明：
//   - incident open = status NOT IN (resolved, closed, cancelled)（与工单 openTicketPredicate 同语义，
//     事件域终态三值）；known_error open = status NOT IN (resolved, deprecated)。
//   - 各类查询为 3 条轻量聚合（total / open / recent limit+1），客户数 × 5 类 = 15 条 COUNT +
//     5 × N 条 recent 查询；MSP 客户规模（数十级）下成本可接受，且无状态不加缓存。
package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	entChange "itsm-backend/ent/change"
	entIncident "itsm-backend/ent/incident"
	entKE "itsm-backend/ent/knownerror"
	entProblem "itsm-backend/ent/problem"
	entSR "itsm-backend/ent/servicerequest"
)

// 各类单据的 open 口径（终态排除集；与域状态机终态语义一致）。
var (
	mspIncidentOpenExclude   = []string{"resolved", "closed", "cancelled"}
	mspKnownErrorOpenExclude = []string{"resolved", "deprecated"}
	mspChangeOpenExclude     = []string{"closed", "cancelled", "rejected", "rolled_back"} // = mspChangeTerminalStatuses
	mspSROpenExclude         = []string{"completed", "cancelled", "rejected", "delivered", "failed"}
	mspProblemOpenExclude    = []string{"resolved", "closed"}
)

// mspRecordsSummaryRecentLimit 每类最近条目上限（dashboard 摘要卡展示 5 条，limit+1 判更多无需：固定截断）。
const mspRecordsSummaryRecentLimit = 5

// RecordsSummary 跨租户协助单据总览（事件/问题/已知错误/变更/服务请求）。
// tenantIDs 为可选筛选（nil = 全部允许客户）；与列表同语义：筛选只能收窄，
// 授权集合仍以 actor.AllowedCustomers 为准（越界 → MSP_ALLOCATION_REQUIRED）。
func (s *MSPWorkbenchService) RecordsSummary(ctx context.Context, actor MSPWorkbenchActor, tenantIDs []int) (*dto.MSPRecordsSummaryResponse, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ent client not available for msp workbench")
	}
	tenantIDs, err := s.normalizeTenantSet(tenantIDs, actor.AllowedCustomers)
	if err != nil {
		return nil, err
	}
	meta, err := s.loadTenantMeta(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &dto.MSPRecordsSummaryResponse{GeneratedAt: now}

	// 逐类型汇总：每类在全部允许租户上聚合计数 + 合并最近条目。
	type typeAgg struct {
		total  int
		open   int
		recent []dto.MSPRecordsSummaryRecentItem
	}
	aggs := make(map[string]*typeAgg, 5)
	initAgg := func() *typeAgg {
		return &typeAgg{recent: make([]dto.MSPRecordsSummaryRecentItem, 0, 8)}
	}
	agg := func(t string) *typeAgg {
		if aggs[t] == nil {
			aggs[t] = initAgg()
		}
		return aggs[t]
	}

	for _, tid := range tenantIDs {
		// IP-P2-2：与列表/工单同口径，按目标客户租户重绑定 ctx。
		qctx := tenantctx.WithTenantID(ctx, tid)
		customerName := meta[tid].name

		// ---- incident ----
		incQuery := s.client.Incident.Query().Where(entIncident.TenantIDEQ(tid))
		if n, err := incQuery.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary incident total tenant %d: %w", tid, err)
		} else {
			agg("incident").total += n
		}
		incOpen := incQuery.Clone().Where(entIncident.StatusNotIn(mspIncidentOpenExclude...))
		if n, err := incOpen.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary incident open tenant %d: %w", tid, err)
		} else {
			agg("incident").open += n
		}
		incRows, err := s.client.Incident.Query().
			Where(entIncident.TenantIDEQ(tid)).
			Order(ent.Desc(entIncident.FieldUpdatedAt)).
			Limit(mspRecordsSummaryRecentLimit).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("records summary incident recent tenant %d: %w", tid, err)
		}
		for _, r := range incRows {
			agg("incident").recent = append(agg("incident").recent, dto.MSPRecordsSummaryRecentItem{
				RecordType: "incident", ID: r.ID, CustomerTenantID: tid, CustomerName: customerName,
				Number: r.IncidentNumber, Title: r.Title, Status: r.Status, Priority: r.Priority, UpdatedAt: r.UpdatedAt,
			})
		}

		// ---- problem ----
		prbQuery := s.client.Problem.Query().Where(entProblem.TenantIDEQ(tid))
		if n, err := prbQuery.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary problem total tenant %d: %w", tid, err)
		} else {
			agg("problem").total += n
		}
		if n, err := prbQuery.Clone().Where(entProblem.StatusNotIn(mspProblemOpenExclude...)).Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary problem open tenant %d: %w", tid, err)
		} else {
			agg("problem").open += n
		}
		prbRows, err := s.client.Problem.Query().
			Where(entProblem.TenantIDEQ(tid)).
			Order(ent.Desc(entProblem.FieldUpdatedAt)).
			Limit(mspRecordsSummaryRecentLimit).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("records summary problem recent tenant %d: %w", tid, err)
		}
		for _, r := range prbRows {
			agg("problem").recent = append(agg("problem").recent, dto.MSPRecordsSummaryRecentItem{
				RecordType: "problem", ID: r.ID, CustomerTenantID: tid, CustomerName: customerName,
				Number: r.ProblemNumber, Title: r.Title, Status: r.Status, Priority: r.Priority, UpdatedAt: r.UpdatedAt,
			})
		}

		// ---- known_error ----
		keQuery := s.client.KnownError.Query().Where(entKE.TenantIDEQ(tid))
		if n, err := keQuery.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary known_error total tenant %d: %w", tid, err)
		} else {
			agg("known_error").total += n
		}
		if n, err := keQuery.Clone().Where(entKE.StatusNotIn(mspKnownErrorOpenExclude...)).Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary known_error open tenant %d: %w", tid, err)
		} else {
			agg("known_error").open += n
		}
		keRows, err := s.client.KnownError.Query().
			Where(entKE.TenantIDEQ(tid)).
			Order(ent.Desc(entKE.FieldUpdatedAt)).
			Limit(mspRecordsSummaryRecentLimit).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("records summary known_error recent tenant %d: %w", tid, err)
		}
		for _, r := range keRows {
			agg("known_error").recent = append(agg("known_error").recent, dto.MSPRecordsSummaryRecentItem{
				RecordType: "known_error", ID: r.ID, CustomerTenantID: tid, CustomerName: customerName,
				Number: fmt.Sprintf("KE-%d", r.ID), Title: r.Title, Status: r.Status, UpdatedAt: r.UpdatedAt,
			})
		}

		// ---- change ----
		chgQuery := s.client.Change.Query().Where(entChange.TenantIDEQ(tid))
		if n, err := chgQuery.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary change total tenant %d: %w", tid, err)
		} else {
			agg("change").total += n
		}
		if n, err := chgQuery.Clone().Where(entChange.StatusNotIn(mspChangeOpenExclude...)).Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary change open tenant %d: %w", tid, err)
		} else {
			agg("change").open += n
		}
		chgRows, err := s.client.Change.Query().
			Where(entChange.TenantIDEQ(tid)).
			Order(ent.Desc(entChange.FieldUpdatedAt)).
			Limit(mspRecordsSummaryRecentLimit).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("records summary change recent tenant %d: %w", tid, err)
		}
		for _, r := range chgRows {
			agg("change").recent = append(agg("change").recent, dto.MSPRecordsSummaryRecentItem{
				RecordType: "change", ID: r.ID, CustomerTenantID: tid, CustomerName: customerName,
				Number: r.ChangeNumber, Title: r.Title, Status: r.Status, Priority: r.Priority, UpdatedAt: r.UpdatedAt,
			})
		}

		// ---- service_request ----
		srQuery := s.client.ServiceRequest.Query().Where(entSR.TenantIDEQ(tid), entSR.DeletedAtIsNil())
		if n, err := srQuery.Clone().Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary service_request total tenant %d: %w", tid, err)
		} else {
			agg("service_request").total += n
		}
		if n, err := srQuery.Clone().Where(entSR.StatusNotIn(mspSROpenExclude...)).Count(qctx); err != nil {
			return nil, fmt.Errorf("records summary service_request open tenant %d: %w", tid, err)
		} else {
			agg("service_request").open += n
		}
		srRows, err := s.client.ServiceRequest.Query().
			Where(entSR.TenantIDEQ(tid), entSR.DeletedAtIsNil()).
			Order(ent.Desc(entSR.FieldUpdatedAt)).
			Limit(mspRecordsSummaryRecentLimit).
			All(qctx)
		if err != nil {
			return nil, fmt.Errorf("records summary service_request recent tenant %d: %w", tid, err)
		}
		for _, r := range srRows {
			agg("service_request").recent = append(agg("service_request").recent, dto.MSPRecordsSummaryRecentItem{
				RecordType: "service_request", ID: r.ID, CustomerTenantID: tid, CustomerName: customerName,
				Number: dto.GenerateServiceRequestNumber(r.ID, r.CreatedAt), Title: r.Title, Status: r.Status, UpdatedAt: r.UpdatedAt,
			})
		}
	}

	// 每类合并后按 updated_at 降序截断到上限。
	trimRecent := func(items []dto.MSPRecordsSummaryRecentItem) []dto.MSPRecordsSummaryRecentItem {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
				return items[i].ID > items[j].ID
			}
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		})
		if len(items) > mspRecordsSummaryRecentLimit {
			items = items[:mspRecordsSummaryRecentLimit]
		}
		return items
	}
	toSummary := func(t string) dto.MSPRecordsSummaryTypeSummary {
		a := aggs[t]
		if a == nil {
			a = initAgg()
		}
		return dto.MSPRecordsSummaryTypeSummary{
			MSPRecordsSummaryTypeCounts: dto.MSPRecordsSummaryTypeCounts{Total: a.total, Open: a.open},
			Recent:                      trimRecent(a.recent),
		}
	}

	out.Incident = toSummary("incident")
	out.Problem = toSummary("problem")
	out.KnownError = toSummary("known_error")
	out.Change = toSummary("change")
	out.ServiceReq = toSummary("service_request")
	return out, nil
}
