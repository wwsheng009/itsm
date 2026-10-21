package project

import (
	"context"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 2026-10-21 功能审查报告回归用例（A1/A2/A4/A6/A8/B3/B12）
// 权威记录：docs/project-management/reviews/2026-10-21-功能审查修复台账.md
// ---------------------------------------------------------------------------

// TestStrictCreateRequiresOwnerAndPlan A1：严格模式下缺失项目经理/部门/计划区间必须 400。
func TestStrictCreateRequiresOwnerAndPlan(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	svc.SetStrictCreate(true)

	// 仅名称 → 拒绝（空壳项目）。
	_, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "空壳项目"})
	require.ErrorIs(t, err, ErrRequiredProjectFields)
	assert.Contains(t, err.Error(), "managerId")

	pm := seedUserWithMembership(t, client, tid, "strict-pm", "home", nil)
	dept, err := client.Department.Create().
		SetName("信息技术部").SetCode("STRICT-DEPT").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	start := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	// 缺部门。
	_, err = svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "缺部门", ManagerID: &pm.ID, StartDate: &start, EndDate: &end,
	})
	require.ErrorIs(t, err, ErrRequiredProjectFields)
	assert.Contains(t, err.Error(), "departmentId")

	// 缺计划区间。
	_, err = svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "缺计划", ManagerID: &pm.ID, DepartmentID: &dept.ID,
	})
	require.ErrorIs(t, err, ErrRequiredProjectFields)
	assert.Contains(t, err.Error(), "startDate")

	// 齐备 → 通过。
	created, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "合规项目", ManagerID: &pm.ID, DepartmentID: &dept.ID,
		StartDate: &start, EndDate: &end,
	})
	require.NoError(t, err)
	assert.Equal(t, pm.ID, created.ManagerID)
	assert.Equal(t, dept.ID, created.DepartmentID)

	// 关闭严格模式后保持历史兼容（导入/脚本/测试）。
	svc.SetStrictCreate(false)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "兼容模式项目"})
	require.NoError(t, err)
}

// TestProjectDisplayNamesEnriched A2/B3：列表/详情响应回填经理与部门名称，禁止裸 ID。
func TestProjectDisplayNamesEnriched(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	pm := seedUserWithMembership(t, client, tid, "display-pm", "home", nil)
	dept, err := client.Department.Create().
		SetName("交付部").SetCode("DISPLAY-DEPT").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)

	p, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "展示项目", ManagerID: &pm.ID, DepartmentID: &dept.ID,
	})
	require.NoError(t, err)

	resp := ToResponse(p)
	svc.EnrichProjectDisplayNames(ctx, tid, []*ProjectResponse{resp})
	assert.Equal(t, pm.Name, resp.ManagerName)
	assert.Equal(t, "交付部", resp.DepartmentName)

	// 部门软删后读路径降级为空名称（不阻塞列表，前端回退 ID 前缀）。
	require.NoError(t, client.Department.UpdateOneID(dept.ID).SetDeletedAt(time.Now()).Exec(ctx))
	resp2 := ToResponse(p)
	svc.EnrichProjectDisplayNames(ctx, tid, []*ProjectResponse{resp2})
	assert.Equal(t, pm.Name, resp2.ManagerName)
	assert.Empty(t, resp2.DepartmentName)
}

// TestGateApprovalModeExposed A4：门响应必须显式标注审批方式（auto/manual/pending）。
func TestGateApprovalModeExposed(t *testing.T) {
	auto := ToGateResponse(&ent.ProjectGate{
		ID: 1, Gate: GateAcceptance, Status: GateStatusPassed,
		DecisionComment: gateDecisionAutoApproved,
	})
	assert.Equal(t, GateApprovalModeAuto, auto.ApprovalMode)
	assert.True(t, auto.AutoApproved)

	manual := ToGateResponse(&ent.ProjectGate{
		ID: 2, Gate: GateInitiation, Status: GateStatusPassed, ProcessInstanceID: 65,
	})
	assert.Equal(t, GateApprovalModeManual, manual.ApprovalMode)
	assert.False(t, manual.AutoApproved)

	// 历史已通过但无流程实例/自动留痕：归为人工，避免污染自动放行口径。
	legacy := ToGateResponse(&ent.ProjectGate{ID: 3, Gate: GatePlan, Status: GateStatusPassed})
	assert.Equal(t, GateApprovalModeManual, legacy.ApprovalMode)
	assert.False(t, legacy.AutoApproved)

	pending := ToGateResponse(&ent.ProjectGate{ID: 4, Gate: GateClosure, Status: GateStatusPending})
	assert.Equal(t, GateApprovalModePending, pending.ApprovalMode)
	assert.False(t, pending.AutoApproved)

	review := ToGateResponse(&ent.ProjectGate{ID: 5, Gate: GateClosure, Status: GateStatusInReview, ProcessInstanceID: 66})
	assert.Equal(t, GateApprovalModeManual, review.ApprovalMode)
}

// previewApprovalPort 只读审批方式探测的假实现（未实现 Inspector 的端口按 auto 兜底）。
type previewApprovalPort struct {
	mode string
}

func (p *previewApprovalPort) SubmitGate(context.Context, int, int, string, map[string]any, string) (GateApprovalStart, error) {
	return GateApprovalStart{}, nil
}

func (p *previewApprovalPort) DecideGate(context.Context, int, int, int, string, string) (GateApprovalDecision, error) {
	return GateApprovalDecision{}, nil
}

func (p *previewApprovalPort) ApprovalMode(context.Context, int, string) (string, error) {
	return p.mode, nil
}

// TestPreviewGatesApprovalMode A4：预检返回审批方式与配置缺失标记（提交前即可识别自动放行）。
func TestPreviewGatesApprovalMode(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	pm := seedUserWithMembership(t, client, tid, "preview-pm", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "预检审批方式", ManagerID: &pm.ID})
	require.NoError(t, err)

	// 未装配审批端口：全部 auto + 配置缺失（与提交降级行为一致）。
	readiness, err := svc.PreviewGates(ctx, tid, p.ID, pm.ID, true)
	require.NoError(t, err)
	require.Len(t, readiness, 4)
	for _, row := range readiness {
		assert.Equal(t, GateApprovalModeAuto, row.ApprovalMode, row.Gate)
		assert.True(t, row.AutoApprovalConfigMissing, row.Gate)
	}

	// 装配人工审批端口：全部 manual（不再误报自动放行）。
	svc.SetGateApproval(&previewApprovalPort{mode: GateApprovalModeManual})
	readiness, err = svc.PreviewGates(ctx, tid, p.ID, pm.ID, true)
	require.NoError(t, err)
	for _, row := range readiness {
		assert.Equal(t, GateApprovalModeManual, row.ApprovalMode, row.Gate)
		assert.False(t, row.AutoApprovalConfigMissing, row.Gate)
	}
}

// TestGateMetricsExcludeAutoApproved B12：自动放行不计入一次通过率分子/分母，但有独立计数。
func TestGateMetricsExcludeAutoApproved(t *testing.T) {
	now := time.Now()
	gates := []*ent.ProjectGate{
		{ID: 1, ProjectID: 1, Gate: GateInitiation, Status: GateStatusPassed, RejectCount: 0, ProcessInstanceID: 65, DecidedAt: &now},
		{ID: 2, ProjectID: 1, Gate: GatePlan, Status: GateStatusPassed, DecisionComment: gateDecisionAutoApproved, DecidedAt: &now},
		{ID: 3, ProjectID: 1, Gate: GateClosure, Status: GateStatusPassed, DecisionComment: gateDecisionAutoApproved, DecidedAt: &now},
	}
	metrics := gateMetrics(gates, nil, nil)
	assert.Equal(t, 1, metrics.Passed, "仅人工审批通过的门进入分母")
	assert.Equal(t, 1, metrics.FirstPass)
	assert.Equal(t, 2, metrics.AutoApproved)
	require.NotNil(t, metrics.Rate)
	assert.InDelta(t, 1.0, *metrics.Rate, 0.0001)
	// 明细全量保留（含自动放行标记）供对账。
	assert.Equal(t, 3, metrics.DetailsTotal)
	autoRows := 0
	for _, row := range metrics.Details {
		if row.AutoApproved {
			autoRows++
			assert.False(t, row.FirstPass, "自动放行不应标记为一次通过")
		}
	}
	assert.Equal(t, metrics.AutoApproved, autoRows)
}

// TestProjectCodeUsesBusinessTimezone A6：编号日期按业务时区生成（UTC 与本地跨天不错位）。
func TestProjectCodeUsesBusinessTimezone(t *testing.T) {
	// UTC 2026-10-20 23:30 = 业务时区 2026-10-21 07:30（Asia/Shanghai）。
	utcLate := time.Date(2026, 10, 20, 23, 30, 0, 0, time.UTC)
	code := InBusinessTime(utcLate).Format("20060102")
	assert.Equal(t, "20261021", code)
	assert.True(t, strings.HasPrefix(FormatBusinessDateTime(utcLate), "2026-10-21 07:30"),
		"业务时间展示应与本地日期一致，实际 %s", FormatBusinessDateTime(utcLate))
}

// TestHealthDataQuality A7：健康度数据完备度提示（不改变既有 9 条规则的判级）。
func TestHealthDataQuality(t *testing.T) {
	now := time.Now()

	// 空壳项目：无经理/无部门/无基线/无任务 → 数据不足 + 缺口说明。
	empty := deriveHealth(&ent.Project{ID: 1, Status: StatusPlanning}, &healthFacts{}, now)
	assert.Equal(t, HealthLevelGreen, empty.Level, "判级口径保持 green（不回溯改判）")
	assert.Equal(t, HealthDataInsufficient, empty.DataQuality)
	assert.Contains(t, empty.DataGaps, "未任命项目经理")
	assert.Contains(t, empty.DataGaps, "未设置所属部门")
	assert.LessOrEqual(t, len(empty.DataGaps), 4, "缺口说明裁剪到 ≤4 条")

	// 事实齐备（经理/部门/计划/基线/任务）→ 数据充分（无缺口）。
	start := now.AddDate(0, 0, -3)
	end := now.AddDate(0, 0, 20)
	full := &ent.Project{
		ID: 2, Status: StatusActive, ManagerID: 9, DepartmentID: 3,
		StartDate: start, EndDate: end,
	}
	facts := &healthFacts{
		tasks:    []*ent.ProjectTask{{ProjectID: 2, Status: TaskStatusTodo}},
		baseline: &BaselineSnapshot{SchemaVersion: BaselineSnapshotSchemaVersion},
	}
	derived := deriveHealth(full, facts, now)
	assert.Equal(t, HealthDataSufficient, derived.DataQuality)
	assert.Empty(t, derived.DataGaps)

	// 响应装配：insufficient 时携带 healthDataQuality/healthDataGaps。
	resp := ToResponse(&ent.Project{ID: 3, Status: StatusPlanning})
	applyHealth(resp, empty)
	assert.Equal(t, HealthDataInsufficient, resp.HealthDataQuality)
	assert.NotEmpty(t, resp.HealthDataGaps)
}
