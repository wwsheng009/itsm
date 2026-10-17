package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent/auditlog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PE-22 预算与工时（D-33~D-36）；权威设计：docs/project-management/plan/p2-budget-hours-design.md。

func ptrF(t *testing.T, v float64) *float64 {
	t.Helper()
	return &v
}

func ptrS(t *testing.T, v string) *string {
	t.Helper()
	return &v
}

// TestBudgetWorkflow 轻量预算：创建/更新/清空 + 成对校验 + 读侧映射（PE-22/D-33）。
func TestBudgetWorkflow(t *testing.T) {
	svc, _, tid := newTestEnv(t)
	ctx := context.Background()

	// 创建带预算。
	p, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "预算项目",
		BudgetAmount: ptrF(t, 120000), BudgetCurrency: ptrS(t, "CNY"),
	})
	require.NoError(t, err)
	require.NotNil(t, p.BudgetAmount)
	assert.Equal(t, 120000.0, *p.BudgetAmount)
	require.NotNil(t, p.BudgetCurrency)
	assert.Equal(t, "CNY", *p.BudgetCurrency)
	resp := ToResponse(p)
	require.NotNil(t, resp.BudgetAmount)
	assert.Equal(t, 120000.0, *resp.BudgetAmount)
	require.NotNil(t, resp.BudgetCurrency)
	assert.Equal(t, "CNY", *resp.BudgetCurrency)

	// 成对校验：只给其一 → 400；负数/超上限 → 400；币种过长 → 400。
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "半预算", BudgetAmount: ptrF(t, 1)})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "负预算", BudgetAmount: ptrF(t, -1), BudgetCurrency: ptrS(t, "CNY")})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "超限预算", BudgetAmount: ptrF(t, MaxBudgetAmount+1), BudgetCurrency: ptrS(t, "CNY")})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "长币种", BudgetAmount: ptrF(t, 1), BudgetCurrency: ptrS(t, "CNY-TOOLONG")})
	assert.ErrorIs(t, err, ErrValidation)

	// 更新为新预算（版本 CAS）。
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{
		BudgetAmount: ptrF(t, 88000), BudgetCurrency: ptrS(t, " USD "),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.BudgetAmount)
	assert.Equal(t, 88000.0, *updated.BudgetAmount)
	require.NotNil(t, updated.BudgetCurrency)
	assert.Equal(t, "USD", *updated.BudgetCurrency, "币种应 trim")

	// 只给其一 → 400。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetCurrency: ptrS(t, "CNY")})
	assert.ErrorIs(t, err, ErrValidation)

	// 清空：amount=0（与空币种）→ NULL 语义（读侧省略）。
	cleared, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{
		BudgetAmount: ptrF(t, 0), BudgetCurrency: ptrS(t, ""),
	})
	require.NoError(t, err)
	assert.Nil(t, cleared.BudgetAmount)
	assert.Nil(t, cleared.BudgetCurrency)
	assert.Nil(t, ToResponse(cleared).BudgetAmount)

	// 纯预算更新不受"无可更新字段"拦截（清空后可再次写入）。
	again, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: ptrF(t, 5000), BudgetCurrency: ptrS(t, "CNY")})
	require.NoError(t, err)
	require.NotNil(t, again.BudgetAmount)
	assert.Equal(t, 5000.0, *again.BudgetAmount)

	// 完全空请求 → ErrNoUpdateFields。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{})
	assert.ErrorIs(t, err, ErrNoUpdateFields)
}

// TestTaskHoursSummary 任务级实际工时：校验 + 汇总口径 + 软删排除 + 可见性（PE-22/D-35）。
func TestTaskHoursSummary(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	member := seedUserWithMembership(t, client, tid, "pe22-member", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "工时项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	// 创建：未填工时 OK；负数 → 400。
	t1, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "需求梳理", EstimateHours: ptrF(t, 8)})
	require.NoError(t, err)
	assert.Equal(t, 8.0, t1.EstimateHours)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "坏任务", EstimateHours: ptrF(t, -1)})
	assert.ErrorIs(t, err, ErrValidation)
	_, err = svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "超限任务", EstimateHours: ptrF(t, MaxTaskHours+1)})
	assert.ErrorIs(t, err, ErrValidation)

	t2, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "开发", EstimateHours: ptrF(t, 2)})
	require.NoError(t, err)

	// 更新实际工时（负数拒绝）。
	_, err = svc.UpdateTask(ctx, tid, p.ID, t1.ID, &TaskUpdateRequest{ActualHours: ptrF(t, -2)})
	assert.ErrorIs(t, err, ErrValidation)
	now := time.Now()
	_, err = svc.UpdateTask(ctx, tid, p.ID, t1.ID, &TaskUpdateRequest{ActualHours: ptrF(t, 5.5), ActualEnd: &now})
	require.NoError(t, err)

	// 汇总：2 任务（8/2 估算，5.5 实际已填报）。
	summary, err := svc.HoursSummary(ctx, tid, p.ID, member.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.TaskCount)
	assert.Equal(t, 2, summary.EstimatedTaskCount)
	assert.Equal(t, 1, summary.ActualTaskCount)
	assert.Equal(t, 10.0, summary.EstimateHours)
	assert.Equal(t, 5.5, summary.ActualHours)

	// 软删任务不计入汇总。
	require.NoError(t, svc.DeleteTask(ctx, tid, p.ID, t2.ID))
	summary, err = svc.HoursSummary(ctx, tid, p.ID, member.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.TaskCount)
	assert.Equal(t, 8.0, summary.EstimateHours)

	// 可见性：非成员 → 404；管理角色直通。
	outsider := seedUserWithMembership(t, client, tid, "pe22-outsider", "home", nil)
	_, err = svc.HoursSummary(ctx, tid, p.ID, outsider.ID, false)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.HoursSummary(ctx, tid, p.ID, outsider.ID, true)
	assert.NoError(t, err)
}

// TestBaselineBudgetSnapshot 预算进基线快照（v2）+ 差异计算 + v1 旧快照容忍（PE-22/D-34）。
func TestBaselineBudgetSnapshot(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	pm := seedUserWithMembership(t, client, tid, "pe22-bl", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "基线预算项目",
		BudgetAmount: ptrF(t, 100000), BudgetCurrency: ptrS(t, "CNY"),
	})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, pm.ID, &MemberAddRequest{UserID: pm.ID})
	require.NoError(t, err)

	// 冻结基线：快照 v2 + 含预算。
	base, err := svc.CreateBaseline(ctx, tid, p.ID, pm.ID, &BaselineCreateRequest{Reason: "计划门"})
	require.NoError(t, err)
	snap := ToBaselineResponse(base).Snapshot
	require.NotNil(t, snap)
	assert.Equal(t, 2, snap.SchemaVersion)
	require.NotNil(t, snap.Project.BudgetAmount)
	assert.Equal(t, 100000.0, *snap.Project.BudgetAmount)
	require.NotNil(t, snap.Project.BudgetCurrency)
	assert.Equal(t, "CNY", *snap.Project.BudgetCurrency)

	// 调整预算 → 偏差 changed。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: ptrF(t, 50000), BudgetCurrency: ptrS(t, "USD")})
	require.NoError(t, err)
	diff, err := svc.BaselineDiff(ctx, tid, p.ID, pm.ID, false)
	require.NoError(t, err)
	require.NotNil(t, diff.Budget)
	assert.True(t, diff.Budget.Frozen)
	assert.True(t, diff.Budget.Changed)
	require.NotNil(t, diff.Budget.BaseAmount)
	assert.Equal(t, 100000.0, *diff.Budget.BaseAmount)
	require.NotNil(t, diff.Budget.CurrentAmount)
	assert.Equal(t, 50000.0, *diff.Budget.CurrentAmount)

	// 重冻结（v2 基线，reason 必填）→ 与当前一致 changed=false。
	_, err = svc.CreateBaseline(ctx, tid, p.ID, pm.ID, &BaselineCreateRequest{Reason: "预算调整后重冻结"})
	require.NoError(t, err)
	diff, err = svc.BaselineDiff(ctx, tid, p.ID, pm.ID, false)
	require.NoError(t, err)
	assert.True(t, diff.Budget.Frozen)
	assert.False(t, diff.Budget.Changed)
	require.NotNil(t, diff.Budget.BaseAmount)
	assert.Equal(t, 50000.0, *diff.Budget.BaseAmount)

	// v1 旧快照（无预算字段）容忍：frozen=false 且不参与变更判定。
	_, err = client.ProjectBaseline.Create().
		SetTenantID(tid).
		SetProjectID(p.ID).
		SetVersion(3).
		SetReason("旧版本快照").
		SetCreatedBy(pm.ID).
		SetSnapshot(map[string]any{
			"schemaVersion": 1,
			"frozenAt":      time.Now().UTC().Format(time.RFC3339Nano),
			"project":       map[string]any{"code": p.Code, "name": p.Name, "status": p.Status, "progress": 0},
			"tasks":         []any{},
			"milestones":    []any{},
		}).
		Save(ctx)
	require.NoError(t, err)
	diff, err = svc.BaselineDiff(ctx, tid, p.ID, pm.ID, false)
	require.NoError(t, err)
	require.NotNil(t, diff.Budget)
	assert.False(t, diff.Budget.Frozen)
	assert.False(t, diff.Budget.Changed, "v1 快照不参与预算变更判定")
}

// TestHandlerBudgetAudit 预算变更审计（budget_update）+ 工时汇总端点（PE-22）。
func TestHandlerBudgetAudit(t *testing.T) {
	r, client, _ := newHandlerEnv(t)
	ctx := context.Background()

	// 创建（带预算）。
	w, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{
		"name": "审计项目", "budgetAmount": 88000, "budgetCurrency": "CNY",
	})
	require.Equal(t, http.StatusOK, w.Code)
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))
	require.NotNil(t, created.BudgetAmount)
	assert.Equal(t, 88000.0, *created.BudgetAmount)

	// 只给金额 → 400。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d", created.ID), map[string]any{"budgetAmount": 1})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 更新预算 → 审计 budget_update（resource=project，含 old/new）。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d", created.ID), map[string]any{
		"budgetAmount": 99000, "budgetCurrency": "USD", "version": created.Version,
	})
	require.Equal(t, http.StatusOK, w.Code)
	audits, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("budget_update")).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, audits, "预算变更应写 auditlog(resource=project, action=budget_update)")
	require.NotNil(t, audits[0].RequestBody)
	assert.Contains(t, *audits[0].RequestBody, "oldAmount")
	assert.Contains(t, *audits[0].RequestBody, "newAmount")

	// 预算未变化 → 不写第二条。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d", created.ID), map[string]any{
		"budgetAmount": 99000, "budgetCurrency": "USD",
	})
	require.Equal(t, http.StatusOK, w.Code)
	count, err := client.AuditLog.Query().
		Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ("budget_update")).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "预算未变化不应重复留痕")

	// 工时汇总端点：任务工时落库并被汇总。
	var task ProjectTaskResponse
	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{"name": "实施", "estimateHours": 6})
	require.NoError(t, json.Unmarshal(env.Data, &task))
	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{"name": "验证", "estimateHours": 4})
	var task2 ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &task2))
	_, env = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/tasks/%d", created.ID, task.ID), map[string]any{"actualHours": 2.5})
	var updatedTask ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &updatedTask))
	require.NotNil(t, updatedTask.ActualHours)
	assert.Equal(t, 2.5, *updatedTask.ActualHours)

	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/hours-summary", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, common.SuccessCode, env.Code)
	var summary HoursSummaryResponse
	require.NoError(t, json.Unmarshal(env.Data, &summary))
	assert.Equal(t, 2, summary.TaskCount)
	assert.Equal(t, 10.0, summary.EstimateHours)
	assert.Equal(t, 1, summary.ActualTaskCount)
	assert.Equal(t, 2.5, summary.ActualHours)

	// 工时负数 → 400（HTTP 口径）。
	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d/tasks/%d", created.ID, task2.ID), map[string]any{"actualHours": -1})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}