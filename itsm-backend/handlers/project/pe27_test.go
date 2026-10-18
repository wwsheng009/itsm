package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// PE-27 健康度派生与解释（D-54~D-59）
// 权威设计：docs/project-management/plan/p3-health-design.md §3/§6
// ---------------------------------------------------------------------------

func pe27Float(v float64) *float64 { return &v }
func pe27Str(v string) *string     { return &v }

func pe27Task(projectID int, status string, planEnd time.Time) *ent.ProjectTask {
	return &ent.ProjectTask{ProjectID: projectID, Status: status, PlanEnd: planEnd}
}

func pe27Milestone(projectID int, status string, due time.Time) *ent.ProjectMilestone {
	return &ent.ProjectMilestone{ProjectID: projectID, Status: status, DueDate: due}
}

func pe27Gate(projectID int, gate, status string) *ent.ProjectGate {
	return &ent.ProjectGate{ProjectID: projectID, Gate: gate, Status: status}
}

func pe27Risk(projectID int, kind, level, status string) *ent.ProjectRisk {
	return &ent.ProjectRisk{ProjectID: projectID, Kind: kind, Level: level, Status: status}
}

func healthRuleSet(reasons []HealthReason) map[string]HealthReason {
	out := make(map[string]HealthReason, len(reasons))
	for _, r := range reasons {
		out[r.Rule] = r
	}
	return out
}

// TestDeriveHealthRules 规则矩阵：触发/不触发、阈值边界、终态语义、排序。
func TestDeriveHealthRules(t *testing.T) {
	now := time.Now()
	today := dayUTC(now)

	t.Run("无事实 → green 且无解释", func(t *testing.T) {
		d := deriveHealth(&ent.Project{ID: 1, Status: StatusPlanning}, nil, now)
		assert.Equal(t, HealthLevelGreen, d.Level)
		assert.Empty(t, d.Reasons)
	})

	t.Run("项目逾期 → red（非终态）", func(t *testing.T) {
		p := &ent.Project{ID: 1, Status: StatusPlanning, EndDate: now.Add(-24 * time.Hour)}
		d := deriveHealth(p, nil, now)
		assert.Equal(t, HealthLevelRed, d.Level)
		require.Len(t, d.Reasons, 1)
		assert.Equal(t, HealthRuleOverdueProject, d.Reasons[0].Rule)
	})

	t.Run("completed/cancelled 不再判定项目逾期", func(t *testing.T) {
		for _, status := range []string{StatusCompleted, StatusCancelled} {
			p := &ent.Project{ID: 1, Status: status, EndDate: now.Add(-24 * time.Hour)}
			d := deriveHealth(p, nil, now)
			assert.Equal(t, HealthLevelGreen, d.Level, status)
		}
	})

	t.Run("进度偏差阈值 20/40 个百分点", func(t *testing.T) {
		base := func(progress int) *ent.Project {
			return &ent.Project{
				ID: 1, Status: StatusActive, Progress: progress,
				StartDate: today.AddDate(0, 0, -10), EndDate: today.AddDate(0, 0, 10),
			}
		}
		// 时间轴应完成 50%：落后 19pp → green；20pp → yellow；40pp → red。
		assert.Equal(t, HealthLevelGreen, deriveHealth(base(31), nil, now).Level)
		d := deriveHealth(base(30), nil, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		assert.Equal(t, HealthRuleScheduleDeviation, d.Reasons[0].Rule)
		assert.Equal(t, HealthLevelRed, deriveHealth(base(10), nil, now).Level)
	})

	t.Run("planning 不判定进度偏差", func(t *testing.T) {
		p := &ent.Project{
			ID: 1, Status: StatusPlanning, Progress: 0,
			StartDate: today.AddDate(0, 0, -10), EndDate: today.AddDate(0, 0, 10),
		}
		assert.Equal(t, HealthLevelGreen, deriveHealth(p, nil, now).Level)
	})

	t.Run("逾期任务 1→yellow / 3→red；done 不计", func(t *testing.T) {
		p := &ent.Project{ID: 1, Status: StatusActive}
		past := now.Add(-2 * time.Hour)
		facts := &healthFacts{tasks: []*ent.ProjectTask{
			pe27Task(1, "todo", past),
			pe27Task(1, "done", past),
		}}
		d := deriveHealth(p, facts, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		require.Len(t, d.Reasons, 1)
		assert.Equal(t, 1, d.Reasons[0].Count)

		facts.tasks = []*ent.ProjectTask{
			pe27Task(1, "todo", past), pe27Task(1, "in_progress", past), pe27Task(1, "blocked", past),
		}
		d = deriveHealth(p, facts, now)
		assert.Equal(t, HealthLevelRed, d.Level)
		rules := healthRuleSet(d.Reasons)
		assert.Equal(t, HealthOverdueTaskRedCount, rules[HealthRuleOverdueTask].Count)
		assert.Equal(t, 1, rules[HealthRuleBlockedTask].Count)
	})

	t.Run("里程碑：planned 逾期 yellow；missed 在终态保留/cancelled 忽略", func(t *testing.T) {
		past := now.Add(-24 * time.Hour)
		d := deriveHealth(&ent.Project{ID: 1, Status: StatusActive}, &healthFacts{
			milestones: []*ent.ProjectMilestone{pe27Milestone(1, MilestoneStatusPlanned, past)},
		}, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		assert.Equal(t, HealthRuleOverdueMilestone, d.Reasons[0].Rule)

		d = deriveHealth(&ent.Project{ID: 2, Status: StatusCompleted}, &healthFacts{
			milestones: []*ent.ProjectMilestone{pe27Milestone(2, MilestoneStatusMissed, past)},
		}, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		assert.Equal(t, HealthRuleMissedMilestone, d.Reasons[0].Rule)

		d = deriveHealth(&ent.Project{ID: 3, Status: StatusCancelled}, &healthFacts{
			milestones: []*ent.ProjectMilestone{pe27Milestone(3, MilestoneStatusMissed, past)},
		}, now)
		assert.Equal(t, HealthLevelGreen, d.Level)
	})

	t.Run("未关闭高风险 1→yellow / 3→red；issue/已关闭/非 high 不计", func(t *testing.T) {
		p := &ent.Project{ID: 1, Status: StatusActive}
		facts := &healthFacts{risks: []*ent.ProjectRisk{
			pe27Risk(1, RiskKindRisk, RiskLevelHigh, "open"),
			pe27Risk(1, RiskKindIssue, RiskLevelHigh, "open"),
			pe27Risk(1, RiskKindRisk, RiskLevelHigh, RiskStatusClosed),
			pe27Risk(1, RiskKindRisk, "low", "open"),
		}}
		d := deriveHealth(p, facts, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		require.Len(t, d.Reasons, 1)
		assert.Equal(t, 1, d.Reasons[0].Count)

		facts.risks = append(facts.risks,
			pe27Risk(1, RiskKindRisk, RiskLevelHigh, "mitigating"),
			pe27Risk(1, RiskKindRisk, RiskLevelHigh, "open"),
		)
		d = deriveHealth(p, facts, now)
		assert.Equal(t, HealthLevelRed, d.Level)
		assert.Equal(t, HealthHighRiskRedCount, d.Reasons[0].Count)
	})

	t.Run("阶段门打回 yellow（门名文案）", func(t *testing.T) {
		d := deriveHealth(&ent.Project{ID: 1, Status: StatusActive}, &healthFacts{gates: []*ent.ProjectGate{
			pe27Gate(1, GateAcceptance, GateStatusRejected),
			pe27Gate(1, GatePlan, GateStatusPassed),
		}}, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		require.Len(t, d.Reasons, 1)
		assert.Contains(t, d.Reasons[0].Message, "验收门")
	})

	t.Run("预算偏差：基线 v2 变化判定；v1/一致不判定；设置↔清空判定", func(t *testing.T) {
		p := &ent.Project{ID: 1, Status: StatusActive, BudgetAmount: pe27Float(120000), BudgetCurrency: pe27Str("CNY")}
		snap := func(v int, amount *float64, currency *string) *BaselineSnapshot {
			return &BaselineSnapshot{SchemaVersion: v, Project: BaselineProjectSnapshot{BudgetAmount: amount, BudgetCurrency: currency}}
		}
		// 币种大小写不敏感：一致 → 不判定。
		d := deriveHealth(p, &healthFacts{baseline: snap(2, pe27Float(120000), pe27Str("cny"))}, now)
		assert.Equal(t, HealthLevelGreen, d.Level)
		// 金额变化 → yellow。
		d = deriveHealth(p, &healthFacts{baseline: snap(2, pe27Float(100000), pe27Str("CNY"))}, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
		assert.Equal(t, HealthRuleBudgetDeviation, d.Reasons[0].Rule)
		// v1 旧快照不判定。
		d = deriveHealth(p, &healthFacts{baseline: snap(1, pe27Float(100000), pe27Str("CNY"))}, now)
		assert.Equal(t, HealthLevelGreen, d.Level)
		// 当前未设置 vs 基线已冻结 → 判定。
		unset := &ent.Project{ID: 2, Status: StatusActive}
		d = deriveHealth(unset, &healthFacts{baseline: snap(2, pe27Float(120000), pe27Str("CNY"))}, now)
		assert.Equal(t, HealthLevelYellow, d.Level)
	})

	t.Run("解释项排序：red 优先，同级按规则名", func(t *testing.T) {
		p := &ent.Project{ID: 1, Status: StatusActive, EndDate: now.Add(-48 * time.Hour)}
		facts := &healthFacts{tasks: []*ent.ProjectTask{pe27Task(1, TaskStatusBlocked, time.Time{})}}
		d := deriveHealth(p, facts, now)
		require.Len(t, d.Reasons, 2)
		assert.Equal(t, HealthLevelRed, d.Reasons[0].Level)
		assert.Equal(t, HealthRuleOverdueProject, d.Reasons[0].Rule)
		assert.Equal(t, HealthLevelYellow, d.Reasons[1].Level)
		assert.Equal(t, HealthRuleBlockedTask, d.Reasons[1].Rule)
	})
}

// TestHealthReadPathIgnoresStoredColumn 读路径派生 + 存量列忽略 + 报告同源（D-54/D-58）。
func TestHealthReadPathIgnoresStoredColumn(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "健康度读路径"})
	require.NoError(t, err)

	// 存量列写入被读路径忽略（单一事实来源）。
	require.NoError(t, client.Project.UpdateOneID(p.ID).SetHealth("yellow").Exec(ctx))

	// 事实：3 个逾期未完成任务 → red + count=3。
	past := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 3; i++ {
		_, err := client.ProjectTask.Create().SetTenantID(tid).SetProjectID(p.ID).
			SetName(fmt.Sprintf("逾期任务%d", i)).SetStatus("todo").SetPlanEnd(past).Save(ctx)
		require.NoError(t, err)
	}
	fresh, err := svc.Get(ctx, tid, p.ID)
	require.NoError(t, err)
	healthMap, err := svc.HealthMap(ctx, tid, []*ent.Project{fresh})
	require.NoError(t, err)
	derived := healthMap[p.ID]
	assert.Equal(t, HealthLevelRed, derived.Level)
	require.Len(t, derived.Reasons, 1)
	assert.Equal(t, HealthRuleOverdueTask, derived.Reasons[0].Rule)
	assert.Equal(t, 3, derived.Reasons[0].Count)

	// 预算偏差（基线 v2 冻结预算 vs 当前预算）+ 报告同源装配。
	amount, currency := 100000.0, "CNY"
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: &amount, BudgetCurrency: &currency})
	require.NoError(t, err)
	pm := seedUserWithMembership(t, client, tid, "pe27-pm", "home", nil)
	_, err = svc.AddMember(ctx, tid, p.ID, pm.ID, &MemberAddRequest{UserID: pm.ID, ProjectRole: "member"})
	require.NoError(t, err)
	_, err = svc.CreateBaseline(ctx, tid, p.ID, pm.ID, &BaselineCreateRequest{Reason: "首个基线"})
	require.NoError(t, err)
	newAmount := 120000.0
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: &newAmount, BudgetCurrency: &currency})
	require.NoError(t, err)

	fresh, err = svc.Get(ctx, tid, p.ID)
	require.NoError(t, err)
	healthMap, err = svc.HealthMap(ctx, tid, []*ent.Project{fresh})
	require.NoError(t, err)
	rules := healthRuleSet(healthMap[p.ID].Reasons)
	assert.Contains(t, rules, HealthRuleBudgetDeviation)
	assert.Equal(t, HealthLevelRed, healthMap[p.ID].Level, "red（逾期任务）优先于 yellow（预算偏差）")

	report, err := svc.GenerateProjectReport(ctx, tid, p.ID, pm.ID, false, 0)
	require.NoError(t, err)
	assert.Equal(t, HealthLevelRed, report.Data.Health)
	assert.Contains(t, report.Data.HealthReasons, rules[HealthRuleOverdueTask])
	assert.Contains(t, report.Markdown, "健康度信号")
}

// TestHealthEndpoints 端点响应：派生 health + healthReasons（列表/详情同源）；health 写入退役。
func TestHealthEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	create := apiJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "端点健康度"})
	require.Equal(t, common.SuccessCode, create.Code)
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(create.Data, &created))
	assert.Equal(t, HealthLevelGreen, created.Health)
	assert.Empty(t, created.HealthReasons)

	// 事实：项目逾期（red）+ 阻塞任务（yellow）。
	past := time.Now().Add(-48 * time.Hour)
	require.NoError(t, client.Project.UpdateOneID(created.ID).SetStatus(StatusActive).SetEndDate(past).Exec(ctx))
	_, err := client.ProjectTask.Create().SetTenantID(tid).SetProjectID(created.ID).
		SetName("阻塞任务").SetStatus(TaskStatusBlocked).Save(ctx)
	require.NoError(t, err)

	detail := apiJSON(t, r, http.MethodGet, pathInt("/projects/%d", created.ID), nil)
	require.Equal(t, common.SuccessCode, detail.Code)
	var got ProjectResponse
	require.NoError(t, json.Unmarshal(detail.Data, &got))
	assert.Equal(t, HealthLevelRed, got.Health)
	require.Len(t, got.HealthReasons, 2)
	assert.Equal(t, HealthRuleOverdueProject, got.HealthReasons[0].Rule)
	assert.Equal(t, HealthRuleBlockedTask, got.HealthReasons[1].Rule)

	list := apiJSON(t, r, http.MethodGet, "/projects", nil)
	require.Equal(t, common.SuccessCode, list.Code)
	var page struct {
		Items []ProjectResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(list.Data, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, got.Health, page.Items[0].Health, "列表与详情必须同源（D-58）")
	assert.Len(t, page.Items[0].HealthReasons, 2)

	// health 已退役：仅 health → 400（无可更新字段）；带其他字段 → 生效但不影响派生值（D-54）。
	onlyHealth := apiJSON(t, r, http.MethodPut, pathInt("/projects/%d", created.ID), map[string]any{"health": "yellow"})
	assert.Equal(t, common.ParamErrorCode, onlyHealth.Code)
	renamed := apiJSON(t, r, http.MethodPut, pathInt("/projects/%d", created.ID), map[string]any{"name": "改名后", "health": "yellow"})
	require.Equal(t, common.SuccessCode, renamed.Code)
	var renamedResp ProjectResponse
	require.NoError(t, json.Unmarshal(renamed.Data, &renamedResp))
	assert.Equal(t, "改名后", renamedResp.Name)
	assert.Equal(t, HealthLevelRed, renamedResp.Health, "health 入参被忽略（D-54）")
}
