package service

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/tenant"

	"go.uber.org/zap"
)

// MSPAuditService 审计看板服务（IP-P1-8）——provider 治理视角的跨租户审计聚合。
//
// 数据口径：
//   - 行归属 = actor home 租户（tenant_id=provider home）；
//   - 跨租户 = target_tenant_id 非空且 ≠ home；
//   - 拒绝类 = tenant.scope_denied（未分配客户访问尝试）+ tenant.probe_denied（冲突/探测）。
//
// 窗口内聚合为内存实现（审计窗口 ≤90 天、provider 面可用；扫描上限兜底防爆量）。
type MSPAuditService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

// NewMSPAuditService 创建审计看板服务。
func NewMSPAuditService(client *ent.Client, logger *zap.SugaredLogger) *MSPAuditService {
	return &MSPAuditService{client: client, logger: logger}
}

const (
	mspAuditDefaultWindowDays = 30
	mspAuditMaxWindowDays     = 90
	mspAuditRowScanLimit      = 5000
	mspAuditRecentLimit       = 50
)

// MSPAuditAggRow 聚合行（key 为枚举值/租户/成员 ID；label 为可读名）。
type MSPAuditAggRow struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

// MSPAuditDenialRow 拒绝事件行（看板"越权尝试/冲突告警"面板）。
type MSPAuditDenialRow struct {
	ID             int       `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	Action         string    `json:"action"`
	Source         string    `json:"source"`
	TargetTenantID int       `json:"targetTenantId"`
	TargetName     string    `json:"targetName,omitempty"`
	ActorAccount   string    `json:"actorAccount"`
	StatusCode     int       `json:"statusCode"`
	ReasonCode     string    `json:"reasonCode"`
	Path           string    `json:"path"`
}

// MSPAuditSummary 看板聚合响应。
type MSPAuditSummary struct {
	WindowDays        int                 `json:"windowDays"`
	GeneratedAt       time.Time           `json:"generatedAt"`
	TotalEvents       int                 `json:"totalEvents"`
	CrossTenantEvents int                 `json:"crossTenantEvents"`
	DeniedEvents      int                 `json:"deniedEvents"`
	BySource          []MSPAuditAggRow    `json:"bySource"`
	ByAction          []MSPAuditAggRow    `json:"byAction"`
	ByTargetTenant    []MSPAuditAggRow    `json:"byTargetTenant"`
	ByMembership      []MSPAuditAggRow    `json:"byMembership"`
	RecentDenials     []MSPAuditDenialRow `json:"recentDenials"`
}

// isDeniedAction 拒绝类事件（越权尝试/冲突告警）。
func isDeniedAction(action string) bool {
	return action == "tenant.scope_denied" || action == "tenant.probe_denied"
}

// Summary 聚合 home 租户窗口内的审计数据。days 越界时收敛到 [1,90]。
func (s *MSPAuditService) Summary(ctx context.Context, homeTenantID, days int) (*MSPAuditSummary, error) {
	if days <= 0 {
		days = mspAuditDefaultWindowDays
	}
	if days > mspAuditMaxWindowDays {
		days = mspAuditMaxWindowDays
	}
	since := time.Now().AddDate(0, 0, -days)

	rows, err := s.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(homeTenantID),
			auditlog.CreatedAtGTE(since),
		).
		Order(ent.Desc(auditlog.FieldCreatedAt)).
		Limit(mspAuditRowScanLimit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	summary := &MSPAuditSummary{
		WindowDays:  days,
		GeneratedAt: time.Now(),
	}
	sourceCounts := map[string]int{}
	actionCounts := map[string]int{}
	targetCounts := map[int]int{}
	membershipCounts := map[int]int{}

	for _, row := range rows {
		summary.TotalEvents++
		source := row.Source
		if source == "" {
			source = "legacy"
		}
		sourceCounts[source]++
		actionCounts[row.Action]++
		if row.TargetTenantID > 0 && row.TargetTenantID != homeTenantID {
			summary.CrossTenantEvents++
			targetCounts[row.TargetTenantID]++
		}
		if row.MembershipID > 0 {
			membershipCounts[row.MembershipID]++
		}
		if isDeniedAction(row.Action) {
			summary.DeniedEvents++
			if len(summary.RecentDenials) < mspAuditRecentLimit {
				body := ""
				if row.RequestBody != nil {
					body = *row.RequestBody
				}
				summary.RecentDenials = append(summary.RecentDenials, MSPAuditDenialRow{
					ID:             row.ID,
					CreatedAt:      row.CreatedAt,
					Action:         row.Action,
					Source:         source,
					TargetTenantID: row.TargetTenantID,
					ActorAccount:   row.ActorAccount,
					StatusCode:     row.StatusCode,
					ReasonCode:     denialReasonCode(body),
					Path:           row.Path,
				})
			}
		}
	}

	summary.BySource = sortAggRowsString(sourceCounts)
	summary.ByAction = sortAggRowsString(actionCounts)
	summary.ByTargetTenant = sortAggRows(targetCounts)
	summary.ByMembership = sortAggRows(membershipCounts)

	// 目标租户名回填（拒绝行 + 客户分布），供看板直接展示。
	if len(targetCounts) > 0 {
		ids := make([]int, 0, len(targetCounts))
		for id := range targetCounts {
			ids = append(ids, id)
		}
		names := s.tenantNames(ctx, ids)
		for i := range summary.ByTargetTenant {
			id, _ := strconv.Atoi(summary.ByTargetTenant[i].Key)
			summary.ByTargetTenant[i].Label = names[id]
		}
		for i := range summary.RecentDenials {
			summary.RecentDenials[i].TargetName = names[summary.RecentDenials[i].TargetTenantID]
		}
	}

	if s.logger != nil {
		s.logger.Infow("msp audit summary",
			"home_tenant_id", homeTenantID, "window_days", days,
			"total", summary.TotalEvents, "denied", summary.DeniedEvents)
	}
	return summary, nil
}

// tenantNames 批量解析租户名（缺失返回空串，不阻断聚合）。
func (s *MSPAuditService) tenantNames(ctx context.Context, ids []int) map[int]string {
	names := map[int]string{}
	if len(ids) == 0 {
		return names
	}
	entities, err := s.client.Tenant.Query().
		Where(tenant.IDIn(ids...)).
		Select(tenant.FieldID, tenant.FieldName).
		All(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warnw("msp audit tenant name lookup failed", "error", err)
		}
		return names
	}
	for _, entity := range entities {
		names[entity.ID] = entity.Name
	}
	return names
}

// denialReasonCode 从审计 request_body 提取 reasonCode（兼容 failureReason 旧载荷）。
func denialReasonCode(body string) string {
	if body == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"reasonCode", "failureReason"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

// sortAggRows 计数 map → 按 count 降序、key 升序的稳定输出。
func sortAggRows(counts map[int]int) []MSPAuditAggRow {
	if len(counts) == 0 {
		return []MSPAuditAggRow{}
	}
	rows := make([]MSPAuditAggRow, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, MSPAuditAggRow{Key: strconv.Itoa(key), Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// sortAggRowsString 字符串键版本（source/action 枚举）。
func sortAggRowsString(counts map[string]int) []MSPAuditAggRow {
	rows := make([]MSPAuditAggRow, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, MSPAuditAggRow{Key: key, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}
