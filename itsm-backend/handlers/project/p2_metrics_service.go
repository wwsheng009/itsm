package project

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// P2 PE-24 组合视图与度量（D-40~D-43）
// 权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md §4/§5
// ---------------------------------------------------------------------------

// visibleProjectFilter 可见性收窄（与列表 ListForViewer 同源：非管理员=参与项目集合）。
// 返回 nil 表示"无可见项目"（调用方按空集短路；不产生查询）。
func (s *Service) visibleProjectFilter(
	ctx context.Context, tenantID, viewerID int, admin bool, f ListFilter,
) (*ListFilter, error) {
	if admin {
		return &f, nil
	}
	ids, err := s.repo.MemberProjectIDs(ctx, tenantID, viewerID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	f.ProjectIDs = ids
	return &f, nil
}

// Portfolio 组合视图（D-40：与列表同谓词同可见性；projects 行与列表同结构同序）。
func (s *Service) Portfolio(
	ctx context.Context, tenantID, viewerID int, admin bool, f ListFilter,
) (*PortfolioResponse, error) {
	scope, err := s.visibleProjectFilter(ctx, tenantID, viewerID, admin, f)
	if err != nil {
		return nil, err
	}
	resp := &PortfolioResponse{
		GeneratedAt: s.now(),
		Projects:    []*ProjectResponse{},
		Summary: PortfolioSummary{
			ByStatus: []PortfolioBucket{}, ByHealth: []PortfolioBucket{}, ByType: []PortfolioBucket{},
			ByManager: []PortfolioBucket{}, ByDepartment: []PortfolioBucket{},
		},
	}
	if scope == nil {
		return resp, nil
	}
	rows, err := s.repo.ListForAggregate(ctx, *scope, PortfolioAggregateMaxProjects)
	if err != nil {
		return nil, err
	}
	resp.Truncated = len(rows) >= PortfolioAggregateMaxProjects
	// PE-27（D-58）：组合行与 byHealth 桶使用派生健康度，与列表同源。
	healthMap, err := s.HealthMap(ctx, tenantID, rows)
	if err != nil {
		return nil, err
	}
	resp.Summary = s.buildPortfolioSummary(ctx, rows, healthMap)
	rowCap := rows
	if len(rowCap) > PortfolioMaxProjects {
		rowCap = rowCap[:PortfolioMaxProjects]
	}
	resp.Projects = ToResponseListWithOwnerTeams(rowCap, s.OwnerTeamRefs(ctx, tenantID, rowCap))
	applyHealthList(resp.Projects, healthMap)
	return resp, nil
}

// buildPortfolioSummary 五维聚合（count 降序、key 升序）；负责人姓名批量富化。
func (s *Service) buildPortfolioSummary(ctx context.Context, rows []*ent.Project, healthMap map[int]DerivedHealth) PortfolioSummary {
	summary := PortfolioSummary{
		Total:        len(rows),
		ByStatus:     portfolioBuckets(rows, func(p *ent.Project) string { return normalizeStatus(p.Status) }),
		ByHealth:     portfolioBuckets(rows, func(p *ent.Project) string { return healthLevelOf(healthMap, p.ID) }),
		ByType:       portfolioBuckets(rows, func(p *ent.Project) string { return p.Type }),
		ByManager:    portfolioBuckets(rows, func(p *ent.Project) string { return intKey(p.ManagerID) }),
		ByDepartment: portfolioBuckets(rows, func(p *ent.Project) string { return intKey(p.DepartmentID) }),
	}
	ids := make([]int, 0, len(summary.ByManager))
	for _, b := range summary.ByManager {
		if id, err := strconv.Atoi(b.Key); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	nameByID := map[int]string{}
	if len(ids) > 0 {
		if users, err := s.repo.UsersByIDs(ctx, ids); err == nil {
			for _, u := range users {
				nameByID[u.ID] = u.Name
			}
		}
	}
	for i := range summary.ByManager {
		id, _ := strconv.Atoi(summary.ByManager[i].Key)
		if id <= 0 {
			summary.ByManager[i].Name = "未设置"
			continue
		}
		summary.ByManager[i].Name = nameByID[id]
	}
	// 部门名称由前端用部门列表映射（后端不跨域查询，避免新耦合）。
	return summary
}

// healthLevelOf 组合桶健康度口径（PE-27 D-58：与行健康度同源；未命中兜底 green）。
func healthLevelOf(healthMap map[int]DerivedHealth, projectID int) string {
	if h, ok := healthMap[projectID]; ok && h.Level != "" {
		return h.Level
	}
	return HealthLevelGreen
}

// Metrics 组合度量（四项指标 + 明细；D-41 口径，明细供抽样对账）。
func (s *Service) Metrics(
	ctx context.Context, tenantID, viewerID int, admin bool, f ListFilter,
	from, to, start *time.Time, weeks int,
) (*MetricsResponse, error) {
	if weeks == 0 {
		weeks = HeatmapDefaultWeeks
	}
	if weeks < HeatmapMinWeeks || weeks > HeatmapMaxWeeks {
		return nil, fmt.Errorf("%w: weeks 应在 %d~%d", ErrValidation, HeatmapMinWeeks, HeatmapMaxWeeks)
	}
	scope, err := s.visibleProjectFilter(ctx, tenantID, viewerID, admin, f)
	if err != nil {
		return nil, err
	}
	resp := &MetricsResponse{
		GeneratedAt: s.now(),
		ProjectScope: MetricsProjectScope{
			Filters: MetricsFilters{
				Status: f.Status, Type: f.Type, ManagerID: f.ManagerID,
				OwnerTeamID: f.OwnerTeamID, Keyword: f.Keyword,
			},
		},
		Window: MetricsDateWindow{},
		OnTimeDelivery:      DeliveryMetrics{Details: []DeliveryDetail{}},
		MilestoneAchievement: MilestoneMetrics{Details: []MilestoneDetail{}},
		GateFirstPass:       GateMetrics{Details: []GateDetail{}},
		ResourceUtilization: ResourceUtilization{
			Calendar: HeatmapCalendar{Mode: "natural_days", BusinessDaysOnly: false, Note: heatmapCalendarNote},
			Projects: []ResourceProjectRow{},
		},
	}
	if from != nil {
		resp.Window.From = dayUTC(*from).Format("2006-01-02")
	}
	if to != nil {
		resp.Window.To = dayUTC(*to).Format("2006-01-02")
	}
	if scope == nil {
		return resp, nil
	}

	projects, err := s.repo.ListForAggregate(ctx, *scope, PortfolioAggregateMaxProjects)
	if err != nil {
		return nil, err
	}
	resp.ProjectScope.Total = len(projects)
	resp.ProjectScope.Truncated = len(projects) >= PortfolioAggregateMaxProjects
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	milestones, err := s.repo.ListMilestonesByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	gates, err := s.repo.ListGatesByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListTasksByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembersByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}

	today := dayUTC(s.now())
	resp.OnTimeDelivery = deliveryMetrics(projects, from, to)
	resp.MilestoneAchievement = milestoneMetrics(milestones, today, from, to)
	resp.GateFirstPass = gateMetrics(gates, from, to)
	resp.ResourceUtilization = resourceUtilization(projects, tasks, members, today, start, weeks)
	return resp, nil
}

// deliveryMetrics 按时交付率（§5.1）。
func deliveryMetrics(projects []*ent.Project, from, to *time.Time) DeliveryMetrics {
	out := DeliveryMetrics{Details: []DeliveryDetail{}}
	details := make([]DeliveryDetail, 0, len(projects))
	for _, p := range projects {
		if normalizeStatus(p.Status) != StatusCompleted || p.ActualEnd.IsZero() {
			continue
		}
		if !inDateWindow(p.ActualEnd, from, to) {
			continue
		}
		if p.EndDate.IsZero() {
			out.NoPlanDate++
			continue
		}
		onTime := !dayUTC(p.ActualEnd).After(dayUTC(p.EndDate))
		out.Denominator++
		if onTime {
			out.Numerator++
		}
		details = append(details, DeliveryDetail{
			ProjectID: p.ID, Code: p.Code, Name: p.Name,
			PlanEnd: dayUTC(p.EndDate), ActualEnd: dayUTC(p.ActualEnd), OnTime: onTime,
		})
	}
	sort.Slice(details, func(i, j int) bool { return details[i].ProjectID < details[j].ProjectID })
	out.DetailsTotal = len(details)
	if len(details) > MetricsDetailMaxRows {
		details = details[:MetricsDetailMaxRows]
	}
	out.Details = details
	out.Rate = rateOrNil(out.Numerator, out.Denominator)
	return out
}

// milestoneMetrics 里程碑达成率（§5.2）。
func milestoneMetrics(milestones []*ent.ProjectMilestone, today time.Time, from, to *time.Time) MilestoneMetrics {
	out := MilestoneMetrics{Details: []MilestoneDetail{}}
	details := make([]MilestoneDetail, 0, len(milestones))
	for _, m := range milestones {
		judged := false
		dateUsed := time.Time{}
		switch m.Status {
		case MilestoneStatusReached:
			judged = true
			if m.ReachedAt != nil {
				dateUsed = dayUTC(*m.ReachedAt)
			} else {
				dateUsed = dayUTC(m.DueDate)
			}
		case MilestoneStatusMissed:
			judged = true
			dateUsed = dayUTC(m.DueDate)
		default: // planned
			if !dayUTC(m.DueDate).After(today) {
				judged = true
				dateUsed = dayUTC(m.DueDate)
			}
		}
		if !judged || !inDateWindow(dateUsed, from, to) {
			continue
		}
		achieved := m.Status == MilestoneStatusReached
		onTime := achieved && m.ReachedAt != nil && !dayUTC(*m.ReachedAt).After(dayUTC(m.DueDate))
		out.Judged++
		if achieved {
			out.Reached++
		} else if m.Status == MilestoneStatusMissed {
			out.Missed++
		}
		if onTime {
			out.OnTimeReached++
		}
		row := MilestoneDetail{
			ProjectID: m.ProjectID, MilestoneID: m.ID, Name: m.Name, Status: m.Status,
			DueDate: dayUTC(m.DueDate), Achieved: achieved, OnTime: onTime, DateUsed: dateUsed,
		}
		if m.ReachedAt != nil {
			t := *m.ReachedAt
			row.ReachedAt = &t
		}
		details = append(details, row)
	}
	sort.Slice(details, func(i, j int) bool {
		if !details[i].DueDate.Equal(details[j].DueDate) {
			return details[i].DueDate.Before(details[j].DueDate)
		}
		return details[i].MilestoneID < details[j].MilestoneID
	})
	out.DetailsTotal = len(details)
	if len(details) > MetricsDetailMaxRows {
		details = details[:MetricsDetailMaxRows]
	}
	out.Details = details
	out.Rate = rateOrNil(out.Reached, out.Judged)
	out.OnTimeRate = rateOrNil(out.OnTimeReached, out.Reached)
	return out
}

// gateMetrics 阶段门一次通过率（§5.3；无时间窗时包含全部 passed，含历史无 decided_at 行）。
func gateMetrics(gates []*ent.ProjectGate, from, to *time.Time) GateMetrics {
	out := GateMetrics{Details: []GateDetail{}}
	details := make([]GateDetail, 0, len(gates))
	for _, g := range gates {
		if g.Status != GateStatusPassed {
			continue
		}
		if from != nil || to != nil {
			if g.DecidedAt == nil || !inDateWindow(*g.DecidedAt, from, to) {
				continue
			}
		}
		firstPass := g.RejectCount == 0
		out.Passed++
		if firstPass {
			out.FirstPass++
		}
		row := GateDetail{
			ProjectID: g.ProjectID, Gate: g.Gate, Status: g.Status,
			RejectCount: g.RejectCount, FirstPass: firstPass,
		}
		if g.DecidedAt != nil {
			t := *g.DecidedAt
			row.DecidedAt = &t
		}
		details = append(details, row)
	}
	sort.Slice(details, func(i, j int) bool {
		if details[i].ProjectID != details[j].ProjectID {
			return details[i].ProjectID < details[j].ProjectID
		}
		return details[i].Gate < details[j].Gate
	})
	out.DetailsTotal = len(details)
	if len(details) > MetricsDetailMaxRows {
		details = details[:MetricsDetailMaxRows]
	}
	out.Details = details
	out.Rate = rateOrNil(out.FirstPass, out.Passed)
	return out
}

// resourceUtilization 资源利用率（§5.4：复用 PE-23 热图摊分函数，口径同源）。
func resourceUtilization(
	projects []*ent.Project, tasks []*ent.ProjectTask, members []*ent.ProjectMember,
	today time.Time, start *time.Time, weeks int,
) ResourceUtilization {
	base := today
	if start != nil {
		base = dayUTC(*start)
	}
	weekStart := isoWeekStart(base)

	defaultCapByProject := make(map[int]float64, len(projects))
	for _, p := range projects {
		cap := DefaultWeeklyCapacityHours
		if p.DefaultWeeklyCapacityHours != nil && *p.DefaultWeeklyCapacityHours > 0 {
			cap = *p.DefaultWeeklyCapacityHours
		}
		defaultCapByProject[p.ID] = cap
	}
	capacityByProject := make(map[int]float64, len(projects))
	for _, m := range members {
		if m.Status != "active" {
			continue
		}
		cap := defaultCapByProject[m.ProjectID]
		if m.WeeklyCapacityHours != nil && *m.WeeklyCapacityHours > 0 {
			cap = *m.WeeklyCapacityHours
		}
		capacityByProject[m.ProjectID] += cap * float64(weeks)
	}

	plannedByProject := make(map[int]float64, len(projects))
	acc := make([]float64, weeks)
	unplanned := ResourceUnplanned{}
	for _, t := range tasks {
		if t.AssigneeID <= 0 {
			continue
		}
		estimate := t.EstimateHours
		if t.PlanStart.IsZero() && t.PlanEnd.IsZero() {
			if estimate > 0 {
				unplanned.TaskCount++
				unplanned.EstimateHours += estimate
			}
			continue
		}
		if estimate <= 0 {
			continue
		}
		ws, we := planWindow(t)
		for i := range acc {
			acc[i] = 0
		}
		distributeHours(acc, ws, we, weekStart, weeks, estimate)
		sum := 0.0
		for _, v := range acc {
			sum += v
		}
		plannedByProject[t.ProjectID] += sum
	}

	rows := make([]ResourceProjectRow, 0, len(projects))
	totalPlanned, totalCapacity := 0.0, 0.0
	for _, p := range projects {
		planned := round1(plannedByProject[p.ID])
		capacity := round1(capacityByProject[p.ID])
		totalPlanned += planned
		totalCapacity += capacity
		rows = append(rows, ResourceProjectRow{
			ProjectID: p.ID, Code: p.Code, Name: p.Name,
			PlannedHours: planned, CapacityHours: capacity, Rate: ratioOrNil(planned, capacity),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ProjectID < rows[j].ProjectID })

	return ResourceUtilization{
		PlannedHours:  round1(totalPlanned),
		CapacityHours: round1(totalCapacity),
		Rate:          ratioOrNil(totalPlanned, totalCapacity),
		Unplanned: ResourceUnplanned{
			TaskCount:     unplanned.TaskCount,
			EstimateHours: round1(unplanned.EstimateHours),
		},
		Calendar: HeatmapCalendar{Mode: "natural_days", BusinessDaysOnly: false, Note: heatmapCalendarNote},
		Window: ResourceWindow{
			Start: weekStart.Format("2006-01-02"),
			End:   weekStart.AddDate(0, 0, weeks*7-1).Format("2006-01-02"),
			Weeks: weeks,
		},
		Projects: rows,
	}
}

// round1 四舍五入到 1 位小数（小时展示口径）。
func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}