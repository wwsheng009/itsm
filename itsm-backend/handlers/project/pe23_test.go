package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PE-23 资源负荷（周容量可配 + 只读热图）；权威设计：docs/project-management/plan/p2-resource-heatmap-design.md。

// optF 三态浮点：显式数值。
func optF(t *testing.T, v float64) OptionalFloat {
	t.Helper()
	return OptionalFloat{Set: true, Value: ptrF(t, v)}
}

// optClear 三态浮点：显式 null（清空）。
func optClear() OptionalFloat {
	return OptionalFloat{Set: true}
}

func mustDay(t *testing.T, raw string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", raw)
	require.NoError(t, err)
	return d
}

// TestCapacityWorkflow 周容量配置：默认+覆盖写入/清空 + 校验 + 成员视图带出（PE-23/D-37）。
func TestCapacityWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	m1 := seedUserWithMembership(t, client, tid, "pe23-cap1", "home", nil)
	m2 := seedUserWithMembership(t, client, tid, "pe23-cap2", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "容量项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, m1.ID, &MemberAddRequest{UserID: m1.ID})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, m2.ID, &MemberAddRequest{UserID: m2.ID})
	require.NoError(t, err)

	// 项目默认 32 + m1 覆盖 24 + m2 未设置（缺省=不修改）。
	resp, err := svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		DefaultWeeklyCapacityHours: optF(t, 32),
		Members: []CapacityMemberInput{
			{UserID: m1.ID, WeeklyCapacityHours: optF(t, 24)},
			{UserID: m2.ID},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.DefaultWeeklyCapacityHours)
	assert.Equal(t, 32.0, *resp.DefaultWeeklyCapacityHours)
	require.Len(t, resp.Members, 2)
	assert.Equal(t, m1.ID, resp.Members[0].UserID)
	require.NotNil(t, resp.Members[0].WeeklyCapacityHours)
	assert.Equal(t, 24.0, *resp.Members[0].WeeklyCapacityHours)
	assert.Nil(t, resp.Members[1].WeeklyCapacityHours)

	// 成员视图携带覆盖值（成员 Tab 直接展示）。
	views, err := svc.ListMembers(ctx, tid, p.ID, m1.ID, false)
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.NotNil(t, views[0].WeeklyCapacityHours)
	assert.Equal(t, 24.0, *views[0].WeeklyCapacityHours)

	// Capacity 视图（GET 语义）与写入一致。
	view, err := svc.Capacity(ctx, tid, p.ID)
	require.NoError(t, err)
	require.NotNil(t, view.DefaultWeeklyCapacityHours)
	assert.Equal(t, 32.0, *view.DefaultWeeklyCapacityHours)

	// 清空：默认 null → 回内置；成员 0 → 回继承。
	resp, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		DefaultWeeklyCapacityHours: optClear(),
		Members: []CapacityMemberInput{
			{UserID: m1.ID, WeeklyCapacityHours: optF(t, 0)},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.DefaultWeeklyCapacityHours)
	assert.Nil(t, resp.Members[0].WeeklyCapacityHours)

	// 校验：越界 / 负数 / 非成员 / 重复 → 400。
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		DefaultWeeklyCapacityHours: optF(t, MaxWeeklyCapacityHours+1),
	})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		DefaultWeeklyCapacityHours: optF(t, -1),
	})
	assert.ErrorIs(t, err, ErrValidation)
	outsider := seedUserWithMembership(t, client, tid, "pe23-cap-out", "home", nil)
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		Members: []CapacityMemberInput{{UserID: outsider.ID, WeeklyCapacityHours: optF(t, 10)}},
	})
	assert.ErrorIs(t, err, ErrValidation, "非存活成员不可配置容量")
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		Members: []CapacityMemberInput{
			{UserID: m1.ID, WeeklyCapacityHours: optF(t, 10)},
			{UserID: m1.ID, WeeklyCapacityHours: optF(t, 12)},
		},
	})
	assert.ErrorIs(t, err, ErrValidation, "重复成员应拒绝")

	// 项目不存在 → 404。
	_, err = svc.UpdateCapacity(ctx, tid, 999999, &CapacityUpdateRequest{DefaultWeeklyCapacityHours: optF(t, 8)})
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestResourceHeatmapMath 热图口径：跨界摊分/完成归零/逾期落本周/未排期/覆盖容量/移除成员行（PE-23/D-38）。
func TestResourceHeatmapMath(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	m1 := seedUserWithMembership(t, client, tid, "pe23-hm1", "home", nil)
	m2 := seedUserWithMembership(t, client, tid, "pe23-hm2", "home", nil)

	fixed := time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC) // 周三
	svc.now = func() time.Time { return fixed }

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "热图项目"})
	require.NoError(t, err)
	for _, u := range []*ent.User{m1, m2} {
		_, err = svc.AddMember(ctx, tid, p.ID, u.ID, &MemberAddRequest{UserID: u.ID})
		require.NoError(t, err)
	}
	// m1 覆盖容量 20；m2 继承（回退 40）。
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{
		Members: []CapacityMemberInput{{UserID: m1.ID, WeeklyCapacityHours: optF(t, 20)}},
	})
	require.NoError(t, err)

	createTask := func(name string, assignee int, start, end string, est float64) *ent.ProjectTask {
		t.Helper()
		req := &TaskCreateRequest{Name: name, AssigneeID: assignee, EstimateHours: ptrF(t, est)}
		if start != "" {
			d := mustDay(t, start)
			req.PlanStart = &d
		}
		if end != "" {
			d := mustDay(t, end)
			req.PlanEnd = &d
		}
		task, err := svc.CreateTask(ctx, tid, p.ID, m1.ID, req)
		require.NoError(t, err)
		return task
	}

	// A：本周整周 14h（每日 2）→ week0 planned 14、remaining 14（10-21~10-25 均摊）。
	createTask("本周任务", m1.ID, "2026-10-19", "2026-10-25", 14)
	// B：下周 7h，且已完成 → week1 planned 7、remaining 0。
	taskB := createTask("下周已完成", m1.ID, "2026-10-26", "2026-11-01", 7)
	_, err = client.ProjectTask.UpdateOneID(taskB.ID).SetStatus(TaskStatusDone).Save(ctx)
	require.NoError(t, err)
	// C：上周（逾期），est 10 / actual 4 → planned 不计入窗口；remaining 6 落本周。
	taskC := createTask("逾期任务", m1.ID, "2026-10-12", "2026-10-18", 10)
	_, err = svc.UpdateTask(ctx, tid, p.ID, taskC.ID, &TaskUpdateRequest{ActualHours: ptrF(t, 4)})
	require.NoError(t, err)
	// D：无计划日期 → 未排期提示。
	createTask("未排期任务", m1.ID, "", "", 5)
	// E：m2 本周 7h（用于移除成员仍出行的断言）。
	createTask("m2 本周任务", m2.ID, "2026-10-19", "2026-10-25", 7)

	hm, err := svc.ResourceHeatmap(ctx, tid, p.ID, m1.ID, false, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, "natural_days", hm.Calendar.Mode)
	assert.False(t, hm.Calendar.BusinessDaysOnly)
	assert.NotEmpty(t, hm.Calendar.Note, "v1 必须明确标注自然日口径（B7）")
	assert.Equal(t, "fallback", hm.Defaults.Source)
	assert.Equal(t, DefaultWeeklyCapacityHours, hm.Defaults.DefaultWeeklyCapacityHours)
	assert.Equal(t, "2026-10-19", hm.Range.Start)
	assert.Equal(t, "2026-11-01", hm.Range.End)
	require.Len(t, hm.Weeks, 2)
	require.Len(t, hm.Weeks[0].Members, 2)

	cell := hm.Weeks[0].Members[0]
	assert.Equal(t, m1.ID, cell.UserID)
	assert.Equal(t, "active", cell.MemberStatus)
	assert.Equal(t, 20.0, cell.CapacityHours, "成员覆盖优先于项目默认/内置")
	assert.InDelta(t, 14.0, cell.PlannedHours, 1e-9)
	assert.InDelta(t, 20.0, cell.RemainingHours, 1e-9, "14（本周）+ 6（逾期剩余落本周）")
	assert.InDelta(t, 0.7, cell.PlannedRatio, 1e-9)
	assert.InDelta(t, 1.0, cell.RemainingRatio, 1e-9)

	w1 := hm.Weeks[1].Members[0]
	assert.InDelta(t, 7.0, w1.PlannedHours, 1e-9)
	assert.InDelta(t, 0.0, w1.RemainingHours, 1e-9, "已完成任务剩余归零")

	m2Cell := hm.Weeks[0].Members[1]
	assert.Equal(t, m2.ID, m2Cell.UserID)
	assert.Equal(t, 40.0, m2Cell.CapacityHours, "继承内置 40")
	assert.InDelta(t, 7.0, m2Cell.PlannedHours, 1e-9)

	assert.Equal(t, 1, hm.Unplanned.TaskCount)
	assert.InDelta(t, 5.0, hm.Unplanned.EstimateHours, 1e-9)

	// 项目默认配置后 source=project（成员覆盖仍优先）。
	_, err = svc.UpdateCapacity(ctx, tid, p.ID, &CapacityUpdateRequest{DefaultWeeklyCapacityHours: optF(t, 36)})
	require.NoError(t, err)
	hm, err = svc.ResourceHeatmap(ctx, tid, p.ID, m1.ID, false, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, "project", hm.Defaults.Source)
	assert.Equal(t, 36.0, hm.Defaults.DefaultWeeklyCapacityHours)
	assert.Equal(t, 20.0, hm.Weeks[0].Members[0].CapacityHours)
	assert.Equal(t, 36.0, hm.Weeks[0].Members[1].CapacityHours)

	// start 落周二 → snap 到周一。
	start := mustDay(t, "2026-10-20")
	hm3, err := svc.ResourceHeatmap(ctx, tid, p.ID, m1.ID, false, &start, 1)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-19", hm3.Range.Start)
	assert.Equal(t, "2026-10-25", hm3.Range.End)

	// 移除成员后仍出行（memberStatus=removed，防静默漏载）。
	require.NoError(t, svc.RemoveMember(ctx, tid, p.ID, m2.ID))
	hm4, err := svc.ResourceHeatmap(ctx, tid, p.ID, m1.ID, false, nil, 1)
	require.NoError(t, err)
	require.Len(t, hm4.Weeks[0].Members, 2)
	removed := hm4.Weeks[0].Members[1]
	assert.Equal(t, m2.ID, removed.UserID)
	assert.Equal(t, "removed", removed.MemberStatus)
	assert.InDelta(t, 7.0, removed.PlannedHours, 1e-9)

	// weeks 越界 → 400；可见性：外部人 404 / 管理角色直通。
	_, err = svc.ResourceHeatmap(ctx, tid, p.ID, m1.ID, false, nil, HeatmapMaxWeeks+1)
	assert.ErrorIs(t, err, ErrValidation)
	outsider := seedUserWithMembership(t, client, tid, "pe23-hm-out", "home", nil)
	_, err = svc.ResourceHeatmap(ctx, tid, p.ID, outsider.ID, false, nil, 2)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.ResourceHeatmap(ctx, tid, p.ID, outsider.ID, true, nil, 2)
	assert.NoError(t, err)
}

// TestHandlerCapacityAuditAndHeatmap 容量端点审计（capacity_update）+ 热图 HTTP 契约（PE-23）。
func TestHandlerCapacityAuditAndHeatmap(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	w, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "热图 HTTP 项目"})
	require.Equal(t, http.StatusOK, w.Code)
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	member := seedUserWithMembership(t, client, tid, "pe23-http", "home", nil)
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": member.ID})
	require.Equal(t, http.StatusOK, w.Code)

	// 容量：默认 30 + 成员 18 → 200 + 审计 capacity_update（old/new）。
	w, env = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/capacity", created.ID), map[string]any{
		"defaultWeeklyCapacityHours": 30,
		"members":                    []map[string]any{{"userId": member.ID, "weeklyCapacityHours": 18}},
	})
	require.Equal(t, http.StatusOK, w.Code)
	var capResp CapacityResponse
	require.NoError(t, json.Unmarshal(env.Data, &capResp))
	require.NotNil(t, capResp.DefaultWeeklyCapacityHours)
	assert.Equal(t, 30.0, *capResp.DefaultWeeklyCapacityHours)
	require.Len(t, capResp.Members, 1)
	require.NotNil(t, capResp.Members[0].WeeklyCapacityHours)
	assert.Equal(t, 18.0, *capResp.Members[0].WeeklyCapacityHours)

	audits, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("capacity_update")).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, audits, 1, "容量变更应写 auditlog(resource=project, action=capacity_update)")
	require.NotNil(t, audits[0].RequestBody)
	assert.Contains(t, *audits[0].RequestBody, "defaultWeeklyCapacityHours")
	assert.Contains(t, *audits[0].RequestBody, "old")
	assert.Contains(t, *audits[0].RequestBody, "new")

	// 未变化 → 不重复留痕。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/capacity", created.ID), map[string]any{"defaultWeeklyCapacityHours": 30})
	require.Equal(t, http.StatusOK, w.Code)
	count, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("capacity_update")).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "容量未变化不应重复留痕")

	// 校验：越界 400 / 未知成员 400。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/capacity", created.ID), map[string]any{"defaultWeeklyCapacityHours": 500})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/capacity", created.ID), map[string]any{
		"members": []map[string]any{{"userId": 999999, "weeklyCapacityHours": 10}},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 热图：weeks 非法 400；正常 200（含日历标注 + 容量来源）。
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/resource-heatmap?weeks=99", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/resource-heatmap?start=2026-13-99", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/resource-heatmap?weeks=2", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var hm ResourceHeatmap
	require.NoError(t, json.Unmarshal(env.Data, &hm))
	require.Len(t, hm.Weeks, 2)
	assert.Equal(t, "natural_days", hm.Calendar.Mode)
	assert.NotEmpty(t, hm.Calendar.Note)
	assert.Equal(t, "project", hm.Defaults.Source)
	assert.Equal(t, 30.0, hm.Defaults.DefaultWeeklyCapacityHours)

	// 项目不存在 → 404。
	w, _ = doJSON(t, r, http.MethodGet, "/projects/999999/resource-heatmap", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}