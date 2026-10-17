package project

import (
	"context"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PE-24 组合视图与度量；权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md（D-40~D-43）。

// responseIDs 提取项目行 ID（对账用）。
func responseIDs(rows []*ProjectResponse) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// entityIDs 提取实体 ID。
func entityIDs(rows []*ent.Project) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// TestPortfolioReconcilesWithList 组合 vs 列表对账（同过滤/同可见性/同行序；D-40）。
func TestPortfolioReconcilesWithList(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	mgr := seedUserWithMembership(t, client, tid, "pe24-mgr", "home", nil)
	viewer := seedUserWithMembership(t, client, tid, "pe24-viewer", "home", nil)
	dept, err := client.Department.Create().SetName("交付部").SetCode("PE24-DEPT").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)

	p1, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "组合A", Type: "delivery"})
	require.NoError(t, err)
	p2, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "组合B", Type: "internal",
		ManagerID: ptrInt(t, mgr.ID), DepartmentID: ptrInt(t, dept.ID),
	})
	require.NoError(t, err)
	p3, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "组合C"})
	require.NoError(t, err)
	require.NoError(t, client.Project.UpdateOneID(p1.ID).SetStatus(StatusActive).SetHealth("red").Exec(ctx))
	require.NoError(t, client.Project.UpdateOneID(p2.ID).SetStatus(StatusCompleted).SetHealth("yellow").Exec(ctx))
	_, err = svc.AddMember(ctx, tid, p3.ID, viewer.ID, &MemberAddRequest{UserID: viewer.ID})
	require.NoError(t, err)

	// 管理员：行集合/顺序与列表全量一致；summary.total == 列表 total。
	portfolio, err := svc.Portfolio(ctx, tid, 0, true, ListFilter{TenantID: tid})
	require.NoError(t, err)
	items, total, err := svc.ListForViewer(ctx, ListFilter{TenantID: tid, Limit: 100}, 0, true)
	require.NoError(t, err)
	assert.Equal(t, entityIDs(items), responseIDs(portfolio.Projects), "组合行必须与列表同集合同序（口径同源）")
	assert.Equal(t, int(total), portfolio.Summary.Total)
	assert.False(t, portfolio.Truncated)

	// 维度聚合不重不漏；状态桶与列表行逐键一致。
	statusCounts := map[string]int{}
	for _, it := range items {
		statusCounts[normalizeStatus(it.Status)]++
	}
	sum := 0
	for _, b := range portfolio.Summary.ByStatus {
		assert.Equal(t, statusCounts[b.Key], b.Count)
		sum += b.Count
	}
	assert.Equal(t, len(items), sum)
	for _, buckets := range [][]PortfolioBucket{
		portfolio.Summary.ByHealth, portfolio.Summary.ByType,
		portfolio.Summary.ByManager, portfolio.Summary.ByDepartment,
	} {
		bucketTotal := 0
		for _, b := range buckets {
			bucketTotal += b.Count
		}
		assert.Equal(t, len(items), bucketTotal, "各维度桶计数总和应等于项目总数")
	}
	foundManager := false
	for _, b := range portfolio.Summary.ByManager {
		if b.Key == intKey(mgr.ID) {
			assert.Equal(t, mgr.Name, b.Name)
			foundManager = true
		}
	}
	assert.True(t, foundManager, "负责人桶应携带姓名富化")

	// 过滤口径一致：status 过滤下组合行与列表一致。
	itemsActive, totalActive, err := svc.ListForViewer(ctx, ListFilter{TenantID: tid, Status: StatusActive, Limit: 100}, 0, true)
	require.NoError(t, err)
	portfolioActive, err := svc.Portfolio(ctx, tid, 0, true, ListFilter{TenantID: tid, Status: StatusActive})
	require.NoError(t, err)
	assert.Equal(t, entityIDs(itemsActive), responseIDs(portfolioActive.Projects))
	assert.Equal(t, int(totalActive), portfolioActive.Summary.Total)

	// 可见性：非管理员仅见参与项目（与 ListForViewer 同源）。
	pv, err := svc.Portfolio(ctx, tid, viewer.ID, false, ListFilter{TenantID: tid})
	require.NoError(t, err)
	require.Len(t, pv.Projects, 1)
	assert.Equal(t, p3.ID, pv.Projects[0].ID)
	assert.Equal(t, 1, pv.Summary.Total)
	viewerItems, viewerTotal, err := svc.ListForViewer(ctx, ListFilter{TenantID: tid, Limit: 100}, viewer.ID, false)
	require.NoError(t, err)
	assert.Equal(t, entityIDs(viewerItems), responseIDs(pv.Projects))
	assert.Equal(t, int(viewerTotal), pv.Summary.Total)

	// 无参与项目 → 空集（不产生查询）。
	outsider := seedUserWithMembership(t, client, tid, "pe24-out", "home", nil)
	po, err := svc.Portfolio(ctx, tid, outsider.ID, false, ListFilter{TenantID: tid})
	require.NoError(t, err)
	assert.Empty(t, po.Projects)
	assert.Equal(t, 0, po.Summary.Total)
}

// TestMetricsDeliveryAndMilestones 按时交付率 + 里程碑达成率（分子/分母与明细逐条勾稽；§5.1/§5.2）。
func TestMetricsDeliveryAndMilestones(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	today := dayUTC(time.Now())

	pOn, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "按期项目"})
	require.NoError(t, err)
	pLate, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "逾期项目"})
	require.NoError(t, err)
	pNoPlan, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "无计划日项目"})
	require.NoError(t, err)
	pActive, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "在途项目"})
	require.NoError(t, err)

	planEnd := today.AddDate(0, 0, -20)
	onTimeEnd := planEnd.AddDate(0, 0, -2)
	require.NoError(t, client.Project.UpdateOneID(pOn.ID).
		SetStatus(StatusCompleted).SetEndDate(planEnd).SetActualEnd(onTimeEnd).Exec(ctx))
	require.NoError(t, client.Project.UpdateOneID(pLate.ID).
		SetStatus(StatusCompleted).SetEndDate(planEnd).SetActualEnd(planEnd.AddDate(0, 0, 3)).Exec(ctx))
	require.NoError(t, client.Project.UpdateOneID(pNoPlan.ID).
		SetStatus(StatusCompleted).SetActualEnd(planEnd).Exec(ctx))
	require.NoError(t, client.Project.UpdateOneID(pActive.ID).
		SetStatus(StatusActive).SetEndDate(planEnd).SetActualEnd(planEnd).Exec(ctx))

	// 里程碑：按期达成 / 逾期达成 / missed / 逾期未判定两种。
	mk := func(projectID int, name string, due time.Time, status string, reachedAt *time.Time) {
		t.Helper()
		c := client.ProjectMilestone.Create().
			SetTenantID(tid).SetProjectID(projectID).SetName(name).SetDueDate(due).SetStatus(status)
		if reachedAt != nil {
			c = c.SetReachedAt(*reachedAt)
		}
		_, e := c.Save(ctx)
		require.NoError(t, e)
	}
	dueOverdue := today.AddDate(0, 0, -10)
	reachedEarly := today.AddDate(0, 0, -12)
	reachedLate := today.AddDate(0, 0, -8)
	mk(pOn.ID, "按期达成", dueOverdue, MilestoneStatusReached, &reachedEarly)
	mk(pOn.ID, "逾期达成", dueOverdue, MilestoneStatusReached, &reachedLate)
	mk(pLate.ID, "已判定未达成", dueOverdue, MilestoneStatusMissed, nil)
	mk(pLate.ID, "逾期未流转", dueOverdue, MilestoneStatusPlanned, nil)
	mk(pActive.ID, "未来计划中", today.AddDate(0, 0, 10), MilestoneStatusPlanned, nil)

	metrics, err := svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, nil, nil, nil, 0)
	require.NoError(t, err)

	// 交付：den=2（按期/逾期），num=1，noPlanDate=1，active 不计入。
	d := metrics.OnTimeDelivery
	assert.Equal(t, 2, d.Denominator)
	assert.Equal(t, 1, d.Numerator)
	assert.Equal(t, 1, d.NoPlanDate)
	require.NotNil(t, d.Rate)
	assert.InDelta(t, 0.5, *d.Rate, 0.0001)
	assert.Equal(t, 2, d.DetailsTotal)
	require.Len(t, d.Details, 2)
	onTimeCount := 0
	for _, row := range d.Details {
		if row.OnTime {
			onTimeCount++
		}
	}
	assert.Equal(t, d.Numerator, onTimeCount, "明细勾稽：onTime 明细数 == 分子")

	// 里程碑：judged=4（未来 planned 不计），reached=2，missed=1，onTimeReached=1。
	m := metrics.MilestoneAchievement
	assert.Equal(t, 4, m.Judged)
	assert.Equal(t, 2, m.Reached)
	assert.Equal(t, 1, m.Missed)
	assert.Equal(t, 1, m.OnTimeReached)
	require.NotNil(t, m.Rate)
	assert.InDelta(t, 0.5, *m.Rate, 0.0001)
	require.NotNil(t, m.OnTimeRate)
	assert.InDelta(t, 0.5, *m.OnTimeRate, 0.0001)
	assert.Equal(t, 4, m.DetailsTotal)
	require.Len(t, m.Details, 4)
	achieved, onTimeRows := 0, 0
	for _, row := range m.Details {
		if row.Achieved {
			achieved++
		}
		if row.OnTime {
			onTimeRows++
		}
		assert.False(t, row.DateUsed.IsZero(), "明细必须携带口径日期")
	}
	assert.Equal(t, m.Reached, achieved, "明细勾稽：achieved 明细数 == 分子")
	assert.Equal(t, m.OnTimeReached, onTimeRows)

	// 时间窗：仅命中该 actual_end 当日。
	from, to := dayUTC(onTimeEnd), dayUTC(onTimeEnd)
	windowed, err := svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, &from, &to, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, windowed.OnTimeDelivery.Denominator)
	assert.Equal(t, 1, windowed.OnTimeDelivery.Numerator)
	assert.Equal(t, 0, windowed.OnTimeDelivery.NoPlanDate)
}

// TestMetricsGateFirstPass 阶段门一次通过率 + reject_count 原子累加（D-42；§5.3）。
func TestMetricsGateFirstPass(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	pm := seedUserWithMembership(t, client, tid, "pe24-gate", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "门测试项目"})
	require.NoError(t, err)
	gate, err := client.ProjectGate.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetGate(GateAcceptance).
		SetStatus(GateStatusInReview).SetVersion(1).
		SetSubmittedBy(pm.ID).SetSubmittedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	// 打回两次 → reject_count 累加（服务 CAS 路径）。
	for expect := 1; expect <= 2; expect++ {
		dec, err := svc.DecideGate(ctx, tid, p.ID, pm.ID, GateAcceptance,
			&GateDecisionRequest{Decision: "reject", Comment: "材料不全"}, true)
		require.NoError(t, err)
		assert.Equal(t, GateStatusRejected, dec.Status)
		assert.Equal(t, expect, dec.RejectCount)
		require.NoError(t, client.ProjectGate.UpdateOneID(gate.ID).
			SetStatus(GateStatusInReview).Exec(ctx))
	}
	// 最终通过：reject_count 保留（非一次通过）。
	passed, err := svc.DecideGate(ctx, tid, p.ID, pm.ID, GateAcceptance,
		&GateDecisionRequest{Decision: "approve"}, true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, passed.Status)
	assert.Equal(t, 2, passed.RejectCount)

	// 另一项目：一次通过 + 打回后通过（直接造库，度量读取口径）。
	p2, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "门测试项目二"})
	require.NoError(t, err)
	_, err = client.ProjectGate.Create().
		SetTenantID(tid).SetProjectID(p2.ID).SetGate(GateInitiation).
		SetStatus(GateStatusPassed).SetRejectCount(0).SetDecidedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectGate.Create().
		SetTenantID(tid).SetProjectID(p2.ID).SetGate(GatePlan).
		SetStatus(GateStatusPassed).SetRejectCount(1).SetDecidedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	metrics, err := svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, nil, nil, nil, 0)
	require.NoError(t, err)
	g := metrics.GateFirstPass
	assert.Equal(t, 3, g.Passed)
	assert.Equal(t, 1, g.FirstPass)
	require.NotNil(t, g.Rate)
	assert.InDelta(t, 1.0/3.0, *g.Rate, 0.0001)
	assert.Equal(t, 3, g.DetailsTotal)
	firstPassRows := 0
	for _, row := range g.Details {
		if row.FirstPass {
			firstPassRows++
		}
	}
	assert.Equal(t, g.FirstPass, firstPassRows, "明细勾稽：firstPass 明细数 == 分子")

	// 时间窗：仅命中「今天」裁决的门（全部）。
	today := dayUTC(time.Now())
	windowed, err := svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, &today, &today, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, windowed.GateFirstPass.Passed)
}

// TestMetricsResourceMatchesHeatmap 资源利用率与 PE-23 热图同窗口对账（D-39/D-41；§5.4）。
func TestMetricsResourceMatchesHeatmap(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	m1 := seedUserWithMembership(t, client, tid, "pe24-res", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "资源项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, m1.ID, &MemberAddRequest{UserID: m1.ID})
	require.NoError(t, err)
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		Members: []CapacityMemberInput{{UserID: m1.ID, WeeklyCapacityHours: optF(t, 20)}},
	})
	require.NoError(t, err)

	createTask := func(name, planStart, planEnd string, estimate float64) {
		t.Helper()
		start, end := mustDay(t, planStart), mustDay(t, planEnd)
		_, e := svc.CreateTask(ctx, tid, p.ID, m1.ID, &TaskCreateRequest{
			Name: name, AssigneeID: m1.ID, PlanStart: &start, PlanEnd: &end, EstimateHours: ptrF(t, estimate),
		})
		require.NoError(t, e)
	}
	createTask("第一周 7h", "2026-10-26", "2026-11-01", 7)
	createTask("第二周 14h", "2026-11-02", "2026-11-08", 14)
	_, err = svc.CreateTask(ctx, tid, p.ID, m1.ID, &TaskCreateRequest{
		Name: "未排期 5h", AssigneeID: m1.ID, EstimateHours: ptrF(t, 5),
	})
	require.NoError(t, err)

	start := mustDay(t, "2026-10-26")
	metrics, err := svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, nil, nil, &start, 2)
	require.NoError(t, err)
	ru := metrics.ResourceUtilization
	assert.Equal(t, "2026-10-26", ru.Window.Start)
	assert.Equal(t, "2026-11-08", ru.Window.End)
	assert.Equal(t, 2, ru.Window.Weeks)
	assert.InDelta(t, 21.0, ru.PlannedHours, 0.001)
	assert.InDelta(t, 40.0, ru.CapacityHours, 0.001)
	require.NotNil(t, ru.Rate)
	assert.InDelta(t, 0.525, *ru.Rate, 0.0001)
	assert.Equal(t, 1, ru.Unplanned.TaskCount, "未排期只提示、不进分母")
	assert.InDelta(t, 5.0, ru.Unplanned.EstimateHours, 0.001)
	require.Len(t, ru.Projects, 1)
	require.NotNil(t, ru.Projects[0].Rate)
	assert.InDelta(t, 0.525, *ru.Projects[0].Rate, 0.0001)

	// 对账：与热图同窗口逐周求和一致（口径同源）。
	hm, err := svc.ResourceHeatmap(ctx, tid, p.ID, 0, true, &start, 2)
	require.NoError(t, err)
	sumPlanned, sumCapacity := 0.0, 0.0
	for _, week := range hm.Weeks {
		for _, cell := range week.Members {
			sumPlanned += cell.PlannedHours
			sumCapacity += cell.CapacityHours
		}
	}
	assert.InDelta(t, ru.PlannedHours, sumPlanned, 0.001, "组合资源与热图 planned 必须一致")
	assert.InDelta(t, ru.CapacityHours, sumCapacity, 0.001, "组合资源与热图 capacity 必须一致")

	// 窗口校验：weeks 越界 400。
	_, err = svc.Metrics(ctx, tid, 0, true, ListFilter{TenantID: tid}, nil, nil, nil, HeatmapMaxWeeks+1)
	assert.ErrorIs(t, err, ErrValidation)
}

// TestMetricsHandlerEndpoints 端点冒烟 + 参数校验（400）。
func TestMetricsHandlerEndpoints(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	w, env := doJSON(t, r, http.MethodGet, "/projects/portfolio", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, common.SuccessCode, env.Code)

	w, env = doJSON(t, r, http.MethodGet, "/projects/metrics", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, common.SuccessCode, env.Code)

	w, _ = doJSON(t, r, http.MethodGet, "/projects/metrics?weeks=99", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)

	w, _ = doJSON(t, r, http.MethodGet, "/projects/metrics?from=2026-13-01", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)

	w, _ = doJSON(t, r, http.MethodGet, "/projects/portfolio?status=active", nil)
	require.Equal(t, http.StatusOK, w.Code)
}