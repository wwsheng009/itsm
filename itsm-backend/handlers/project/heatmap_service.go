package project

import (
	"context"
	"fmt"
	"sort"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// P2 PE-23 资源负荷（周容量配置 + 只读热图）
// 权威设计：docs/project-management/plan/p2-resource-heatmap-design.md（D-37~D-39）
// ---------------------------------------------------------------------------

// heatmapCalendarNote 日历限制标注（B7：v1 自然日 + 明确标注；热图响应携带）。
const heatmapCalendarNote = "v1 按自然日均摊（含周末），未接入租户工作日历；周容量为整周口径"

// Capacity 容量配置视图（GET 语义；项目不存在返回 ErrNotFound）。
func (s *Service) Capacity(ctx context.Context, tenantID, projectID int) (*CapacityResponse, error) {
	if _, err := s.Get(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	return s.capacityView(ctx, tenantID, projectID)
}

// UpdateCapacity 容量配置（D-37）：项目默认 + 成员覆盖；字段缺省=不修改，null/0=清空（回继承/回内置 40）。
func (s *Service) UpdateCapacity(ctx context.Context, tenantID, projectID int, req *CapacityUpdateRequest) (*CapacityResponse, error) {
	if _, err := s.Get(ctx, tenantID, projectID); err != nil {
		return nil, err
	}

	type memberPatch struct {
		userID int
		hours  *float64
	}
	patches := make([]memberPatch, 0, len(req.Members))
	if len(req.Members) > 0 {
		members, err := s.repo.ListMembers(ctx, tenantID, projectID)
		if err != nil {
			return nil, err
		}
		active := make(map[int]bool, len(members))
		for _, m := range members {
			if m.Status == "active" {
				active[m.UserID] = true
			}
		}
		seen := make(map[int]bool, len(req.Members))
		for _, item := range req.Members {
			if item.UserID <= 0 {
				return nil, fmt.Errorf("%w: userId 非法", ErrValidation)
			}
			if !active[item.UserID] {
				return nil, fmt.Errorf("%w: 用户 %d 不在项目存活成员中", ErrValidation, item.UserID)
			}
			if seen[item.UserID] {
				return nil, fmt.Errorf("%w: 成员 %d 重复", ErrValidation, item.UserID)
			}
			seen[item.UserID] = true
			if !item.WeeklyCapacityHours.Set {
				continue
			}
			hours, err := normalizeCapacityHours(item.WeeklyCapacityHours)
			if err != nil {
				return nil, err
			}
			patches = append(patches, memberPatch{userID: item.UserID, hours: hours})
		}
	}

	if req.DefaultWeeklyCapacityHours.Set {
		hours, err := normalizeCapacityHours(req.DefaultWeeklyCapacityHours)
		if err != nil {
			return nil, err
		}
		if err := s.repo.SetProjectDefaultCapacity(ctx, tenantID, projectID, hours); err != nil {
			return nil, err
		}
	}
	for _, p := range patches {
		if err := s.repo.SetMemberCapacity(ctx, tenantID, projectID, p.userID, p.hours); err != nil {
			return nil, err
		}
	}
	return s.capacityView(ctx, tenantID, projectID)
}

// normalizeCapacityHours 三态归一：未设置/null → nil（清空）；0 → nil（清空）；(0,168] → 值；其余 400。
func normalizeCapacityHours(o OptionalFloat) (*float64, error) {
	if !o.Set || o.Value == nil {
		return nil, nil
	}
	h := *o.Value
	if h == 0 {
		return nil, nil
	}
	if h < 0 || h > MaxWeeklyCapacityHours {
		return nil, fmt.Errorf("%w: 周容量应在 (0, %g] 小时，或置空回默认", ErrValidation, MaxWeeklyCapacityHours)
	}
	return &h, nil
}

// capacityView 组装容量视图（存活成员全量，按加入顺序）。
func (s *Service) capacityView(ctx context.Context, tenantID, projectID int) (*CapacityResponse, error) {
	p, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	resp := &CapacityResponse{
		DefaultWeeklyCapacityHours: p.DefaultWeeklyCapacityHours,
		Members:                    make([]CapacityMemberView, 0, len(members)),
	}
	for _, m := range members {
		if m.Status != "active" {
			continue
		}
		resp.Members = append(resp.Members, CapacityMemberView{
			UserID:              m.UserID,
			WeeklyCapacityHours: m.WeeklyCapacityHours,
		})
	}
	return resp, nil
}

// heatmapRow 热图行（成员或离册负责人）。
type heatmapRow struct {
	userID    int
	role      string
	status    string
	capacity  float64
	planned   []float64
	remaining []float64
}

// ResourceHeatmap 资源负荷只读热图（D-38 口径；实时计算不落库）。
//
// 周对齐：ISO 周（周一为首）；start 缺省=本周，服务端 snap 到所在周周一；weeks 1..12（缺省 6）。
func (s *Service) ResourceHeatmap(
	ctx context.Context, tenantID, projectID, viewerID int, admin bool,
	start *time.Time, weeks int,
) (*ResourceHeatmap, error) {
	if weeks == 0 {
		weeks = HeatmapDefaultWeeks
	}
	if weeks < HeatmapMinWeeks || weeks > HeatmapMaxWeeks {
		return nil, fmt.Errorf("%w: weeks 应在 %d~%d", ErrValidation, HeatmapMinWeeks, HeatmapMaxWeeks)
	}
	p, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}

	today := dayUTC(s.now())
	base := today
	if start != nil {
		base = dayUTC(*start)
	}
	weekStart := isoWeekStart(base)

	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	// 容量解析（D-37）：成员覆盖 ?? 项目默认 ?? 内置 40。
	defaultCap := DefaultWeeklyCapacityHours
	source := "fallback"
	if p.DefaultWeeklyCapacityHours != nil && *p.DefaultWeeklyCapacityHours > 0 {
		defaultCap = *p.DefaultWeeklyCapacityHours
		source = "project"
	}
	memberByUser := make(map[int]*ent.ProjectMember, len(members))
	for _, m := range members {
		memberByUser[m.UserID] = m
	}
	resolveCapacity := func(userID int) float64 {
		if m := memberByUser[userID]; m != nil && m.WeeklyCapacityHours != nil && *m.WeeklyCapacityHours > 0 {
			return *m.WeeklyCapacityHours
		}
		return defaultCap
	}

	rows := make([]*heatmapRow, 0, len(members)+4)
	rowIndex := make(map[int]int, len(members)+4)
	for _, m := range members {
		if m.Status != "active" {
			continue
		}
		rowIndex[m.UserID] = len(rows)
		rows = append(rows, &heatmapRow{
			userID:    m.UserID,
			role:      m.ProjectRole,
			status:    "active",
			capacity:  resolveCapacity(m.UserID),
			planned:   make([]float64, weeks),
			remaining: make([]float64, weeks),
		})
	}
	// 行覆盖：存活成员 ∪ 窗口内任务的负责人（后者标 removed，防静默漏载；D-38/§4）。
	extraIndex := make(map[int]int, 4)
	extra := make([]*heatmapRow, 0, 4)
	ensureRow := func(userID int) *heatmapRow {
		if i, ok := rowIndex[userID]; ok {
			return rows[i]
		}
		if i, ok := extraIndex[userID]; ok {
			return extra[i]
		}
		status := "removed"
		if _, exists := memberByUser[userID]; !exists {
			status = "removed" // 成员行缺失（历史数据兜底）：同样按离册处理
		}
		row := &heatmapRow{
			userID:    userID,
			role:      "member",
			status:    status,
			capacity:  resolveCapacity(userID),
			planned:   make([]float64, weeks),
			remaining: make([]float64, weeks),
		}
		if m := memberByUser[userID]; m != nil {
			row.role = m.ProjectRole
		}
		extraIndex[userID] = len(extra)
		extra = append(extra, row)
		return row
	}

	unplanned := HeatmapUnplanned{}
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
		if estimate > 0 {
			ws, we := planWindow(t)
			row := ensureRow(t.AssigneeID)
			distributeHours(row.planned, ws, we, weekStart, weeks, estimate)
		}
		if t.Status != TaskStatusDone && t.Status != TaskStatusCancelled {
			if rem := estimate - t.ActualHours; rem > 0 {
				ws, we := remainingWindow(t, today)
				row := ensureRow(t.AssigneeID)
				distributeHours(row.remaining, ws, we, weekStart, weeks, rem)
			}
		}
	}

	sort.Slice(extra, func(i, j int) bool { return extra[i].userID < extra[j].userID })
	for _, row := range extra {
		rows = append(rows, row)
	}

	// 姓名解析（批量；缺失降级空串，不阻塞展示）。
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.userID)
	}
	users, err := s.repo.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	userByID := make(map[int]*ent.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	resp := &ResourceHeatmap{
		ProjectID: projectID,
		Range: HeatmapRange{
			Start: weekStart.Format("2006-01-02"),
			End:   weekStart.AddDate(0, 0, weeks*7-1).Format("2006-01-02"),
		},
		Calendar: HeatmapCalendar{Mode: "natural_days", BusinessDaysOnly: false, Note: heatmapCalendarNote},
		Defaults: HeatmapDefaults{DefaultWeeklyCapacityHours: defaultCap, Source: source},
		Weeks:    make([]HeatmapWeek, 0, weeks),
		Unplanned: unplanned,
	}
	for i := 0; i < weeks; i++ {
		wS := weekStart.AddDate(0, 0, 7*i)
		week := HeatmapWeek{
			WeekStart: wS.Format("2006-01-02"),
			WeekEnd:   wS.AddDate(0, 0, 6).Format("2006-01-02"),
			Members:   make([]HeatmapMemberWeek, 0, len(rows)),
		}
		for _, row := range rows {
			planned := row.planned[i]
			remaining := row.remaining[i]
			cell := HeatmapMemberWeek{
				UserID:         row.userID,
				ProjectRole:    row.role,
				MemberStatus:   row.status,
				CapacityHours:  row.capacity,
				PlannedHours:   planned,
				RemainingHours: remaining,
				PlannedRatio:   planned / row.capacity,
				RemainingRatio: remaining / row.capacity,
			}
			if u := userByID[row.userID]; u != nil {
				cell.Name = u.Name
				cell.Username = u.Username
			}
			week.Members = append(week.Members, cell)
		}
		resp.Weeks = append(resp.Weeks, week)
	}
	return resp, nil
}

// --- 日期/窗口工具（全部以 UTC 日粒度计算；与存储 timestamptz 的 UTC 日对齐）---

func dayUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func isoWeekStart(d time.Time) time.Time {
	offset := (int(d.Weekday()) + 6) % 7 // Monday=0
	return d.AddDate(0, 0, -offset)
}

func maxDay(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func daysInclusive(a, b time.Time) int {
	return int(b.Sub(a).Hours()/24) + 1
}

// planWindow 计划摊分窗口（D-38 口径 1）：两端齐=区间；仅一端=该端单日。
func planWindow(t *ent.ProjectTask) (time.Time, time.Time) {
	switch {
	case !t.PlanStart.IsZero() && !t.PlanEnd.IsZero():
		ws, we := dayUTC(t.PlanStart), dayUTC(t.PlanEnd)
		if we.Before(ws) {
			return we, ws
		}
		return ws, we
	case !t.PlanStart.IsZero():
		d := dayUTC(t.PlanStart)
		return d, d
	default:
		d := dayUTC(t.PlanEnd)
		return d, d
	}
}

// remainingWindow 剩余摊分窗口（D-38 口径 2）：[max(plan_start, today), max(plan_end, today)]；
// 逾期任务 → 剩余全量落本周（today）。
func remainingWindow(t *ent.ProjectTask, today time.Time) (time.Time, time.Time) {
	ws := today
	if !t.PlanStart.IsZero() {
		if d := dayUTC(t.PlanStart); d.After(today) {
			ws = d
		}
	}
	we := today
	if !t.PlanEnd.IsZero() {
		if d := dayUTC(t.PlanEnd); d.After(today) {
			we = d
		}
	}
	if we.Before(ws) {
		we = ws
	}
	return ws, we
}

// distributeHours 把 hours 在 [ws, we] 自然日均摊后按周切片累加到 acc。
func distributeHours(acc []float64, ws, we, weekStart time.Time, weeks int, hours float64) {
	if we.Before(ws) {
		ws, we = we, ws
	}
	perDay := hours / float64(daysInclusive(ws, we))
	for i := 0; i < weeks; i++ {
		wS := weekStart.AddDate(0, 0, 7*i)
		wE := wS.AddDate(0, 0, 6)
		ovS := maxDay(ws, wS)
		ovE := minDay(we, wE)
		if ovE.Before(ovS) {
			continue
		}
		acc[i] += perDay * float64(daysInclusive(ovS, ovE))
	}
}