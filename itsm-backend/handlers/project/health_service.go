package project

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// PE-27 健康度派生与解释：读时实时计算、不落库（D-54）；列表/详情/组合/报表/AI 同源（D-58）。
// 权威设计：docs/project-management/plan/p3-health-design.md §3/§5。
// ---------------------------------------------------------------------------

// healthFacts 单个项目的事实源集合（批量装载后按 projectID 归并）。
type healthFacts struct {
	tasks      []*ent.ProjectTask
	milestones []*ent.ProjectMilestone
	gates      []*ent.ProjectGate
	risks      []*ent.ProjectRisk
	// baseline 最新基线快照（无基线/解码失败 = nil，仅跳过预算规则，D-59）。
	baseline *BaselineSnapshot
}

// HealthMap 批量派生健康度（按 project.ID → DerivedHealth；D-58 批量取数避免 N+1）。
func (s *Service) HealthMap(ctx context.Context, tenantID int, projects []*ent.Project) (map[int]DerivedHealth, error) {
	facts, err := s.loadHealthFacts(ctx, tenantID, projects)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make(map[int]DerivedHealth, len(projects))
	for _, p := range projects {
		if p == nil {
			continue
		}
		out[p.ID] = deriveHealth(p, facts[p.ID], now)
	}
	return out, nil
}

// loadHealthFacts 批量装载事实源（任务/里程碑/阶段门/风险/最新基线）。
// 错误不吞（D-59：取数失败 → 读失败，不展示假绿）。
func (s *Service) loadHealthFacts(ctx context.Context, tenantID int, projects []*ent.Project) (map[int]*healthFacts, error) {
	out := make(map[int]*healthFacts, len(projects))
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		if p == nil {
			continue
		}
		if _, exists := out[p.ID]; exists {
			continue
		}
		out[p.ID] = &healthFacts{}
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}

	tasks, err := s.repo.ListTasksByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if t != nil {
			if f := out[t.ProjectID]; f != nil {
				f.tasks = append(f.tasks, t)
			}
		}
	}

	milestones, err := s.repo.ListMilestonesByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range milestones {
		if m != nil {
			if f := out[m.ProjectID]; f != nil {
				f.milestones = append(f.milestones, m)
			}
		}
	}

	gates, err := s.repo.ListGatesByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, g := range gates {
		if g != nil {
			if f := out[g.ProjectID]; f != nil {
				f.gates = append(f.gates, g)
			}
		}
	}

	risks, err := s.repo.ListRisksByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range risks {
		if r != nil {
			if f := out[r.ProjectID]; f != nil {
				f.risks = append(f.risks, r)
			}
		}
	}

	baselines, err := s.repo.ListBaselinesByProjects(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	latest := make(map[int]*ent.ProjectBaseline, len(ids))
	for _, b := range baselines {
		if b == nil {
			continue
		}
		if cur := latest[b.ProjectID]; cur == nil || b.Version > cur.Version {
			latest[b.ProjectID] = b
		}
	}
	for projectID, b := range latest {
		snap, err := snapshotFromMap(b.Snapshot)
		if err != nil {
			s.logger.Warnw("project health: decode baseline snapshot failed",
				"project_id", projectID, "baseline_id", b.ID, "error", err)
			continue
		}
		if f := out[projectID]; f != nil {
			f.baseline = snap
		}
	}
	return out, nil
}

// deriveHealth 派生健康度与解释项（确定性；终态语义见 D-56）。
func deriveHealth(p *ent.Project, f *healthFacts, now time.Time) DerivedHealth {
	if p == nil {
		return DerivedHealth{Level: HealthLevelGreen}
	}
	status := normalizeStatus(p.Status)
	if status == StatusCancelled {
		return DerivedHealth{Level: HealthLevelGreen}
	}
	if f == nil {
		f = &healthFacts{}
	}
	terminal := status == StatusCompleted
	reasons := make([]HealthReason, 0, 4)

	if !terminal {
		// 1. 项目整体逾期（与 D-45 overdue_project 同判定）。
		if !p.EndDate.IsZero() && p.EndDate.Before(now) {
			reasons = append(reasons, HealthReason{
				Rule:  HealthRuleOverdueProject,
				Level: HealthLevelRed,
				Message: fmt.Sprintf("已超过计划结束日期 %s（当前状态「%s」，进度 %d%%）",
					p.EndDate.Format("2006-01-02"), projectStatusLabels[status], p.Progress),
			})
		}

		// 2. 进度偏差（仅 active/suspended；start/end 齐备且已过开始日）。
		if (status == StatusActive || status == StatusSuspended) &&
			!p.StartDate.IsZero() && !p.EndDate.IsZero() && p.EndDate.After(p.StartDate) {
			start, end, today := dayUTC(p.StartDate), dayUTC(p.EndDate), dayUTC(now)
			if today.After(start) {
				ratio := today.Sub(start).Hours() / end.Sub(start).Hours()
				if ratio > 1 {
					ratio = 1
				}
				expected := int(ratio*100 + 0.5)
				deviation := expected - p.Progress
				if deviation >= HealthDeviationYellowPP {
					level := HealthLevelYellow
					if deviation >= HealthDeviationRedPP {
						level = HealthLevelRed
					}
					reasons = append(reasons, HealthReason{
						Rule:  HealthRuleScheduleDeviation,
						Level: level,
						Message: fmt.Sprintf("进度落后时间轴 %d 个百分点（按自然日应完成约 %d%%，当前 %d%%）",
							deviation, expected, p.Progress),
					})
				}
			}
		}

		// 3. 逾期任务 / 阻塞任务（未终结任务口径）。
		overdueTasks, blockedTasks := 0, 0
		for _, t := range f.tasks {
			if t == nil || !taskIsOpen(t.Status) {
				continue
			}
			if t.Status == TaskStatusBlocked {
				blockedTasks++
			}
			if !t.PlanEnd.IsZero() && t.PlanEnd.Before(now) {
				overdueTasks++
			}
		}
		if overdueTasks > 0 {
			level := HealthLevelYellow
			if overdueTasks >= HealthOverdueTaskRedCount {
				level = HealthLevelRed
			}
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleOverdueTask, Level: level,
				Message: fmt.Sprintf("%d 个任务已逾期未完成", overdueTasks),
				Count:   overdueTasks,
			})
		}
		if blockedTasks > 0 {
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleBlockedTask, Level: HealthLevelYellow,
				Message: fmt.Sprintf("%d 个任务处于阻塞状态", blockedTasks),
				Count:   blockedTasks,
			})
		}

		// 4. 里程碑逾期（planned 且目标日期已过；与 D-45 overdue_milestone 同判定）。
		overdueMilestones := 0
		for _, m := range f.milestones {
			if m != nil && m.Status == MilestoneStatusPlanned && !m.DueDate.IsZero() && m.DueDate.Before(now) {
				overdueMilestones++
			}
		}
		if overdueMilestones > 0 {
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleOverdueMilestone, Level: HealthLevelYellow,
				Message: fmt.Sprintf("%d 个里程碑已逾期未达成", overdueMilestones),
				Count:   overdueMilestones,
			})
		}

		// 5. 未关闭高风险（kind=risk、level=high；issue 不计）。
		highRisks := 0
		for _, r := range f.risks {
			if r != nil && r.Kind == RiskKindRisk && r.Level == RiskLevelHigh && r.Status != RiskStatusClosed {
				highRisks++
			}
		}
		if highRisks > 0 {
			level := HealthLevelYellow
			if highRisks >= HealthHighRiskRedCount {
				level = HealthLevelRed
			}
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleOpenHighRisk, Level: level,
				Message: fmt.Sprintf("%d 个高风险未关闭", highRisks),
				Count:   highRisks,
			})
		}

		// 6. 阶段门被打回（可重新提交）。
		rejected := make([]string, 0, 2)
		for _, g := range f.gates {
			if g != nil && g.Status == GateStatusRejected {
				rejected = append(rejected, gateLabel(g.Gate))
			}
		}
		if len(rejected) > 0 {
			sort.Strings(rejected)
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleGateRejected, Level: HealthLevelYellow,
				Message: fmt.Sprintf("阶段门「%s」被打回（可重新提交）", strings.Join(rejected, "、")),
				Count:   len(rejected),
			})
		}

		// 7. 预算偏差（最新基线 v2 冻结预算 vs 当前预算）。
		if msg, ok := budgetDeviationMessage(p, f.baseline); ok {
			reasons = append(reasons, HealthReason{
				Rule: HealthRuleBudgetDeviation, Level: HealthLevelYellow, Message: msg,
			})
		}
	}

	// 8. 里程碑未达成（completed 项目同样保留，见 D-56）。
	missed := 0
	for _, m := range f.milestones {
		if m != nil && m.Status == MilestoneStatusMissed {
			missed++
		}
	}
	if missed > 0 {
		reasons = append(reasons, HealthReason{
			Rule: HealthRuleMissedMilestone, Level: HealthLevelYellow,
			Message: fmt.Sprintf("%d 个里程碑已判定未达成", missed),
			Count:   missed,
		})
	}

	level := HealthLevelGreen
	for _, r := range reasons {
		if healthLevelRank(r.Level) > healthLevelRank(level) {
			level = r.Level
		}
	}
	// 排序：red 优先，同级按规则名（前端解释列表稳定可读）。
	sort.SliceStable(reasons, func(i, j int) bool {
		ri, rj := healthLevelRank(reasons[i].Level), healthLevelRank(reasons[j].Level)
		if ri != rj {
			return ri > rj
		}
		return reasons[i].Rule < reasons[j].Rule
	})
	return DerivedHealth{Level: level, Reasons: reasons}
}

// budgetPair 预算归一（amount<=0/nil = 未设置；币种大小写不敏感比较）。
type budgetPair struct {
	amount   *float64
	currency string // 大写；空 = 未指定
}

func normalizeBudgetPair(amount *float64, currency *string) budgetPair {
	pair := budgetPair{}
	if amount != nil && *amount > 0 {
		v := *amount
		pair.amount = &v
	}
	if currency != nil {
		pair.currency = strings.ToUpper(strings.TrimSpace(*currency))
	}
	return pair
}

// budgetDeviationMessage 预算偏差文案（无基线 / schemaVersion<2 / 未变化 → 不判定）。
func budgetDeviationMessage(p *ent.Project, snap *BaselineSnapshot) (string, bool) {
	if snap == nil || snap.SchemaVersion < BaselineSnapshotSchemaVersion {
		return "", false
	}
	base := normalizeBudgetPair(snap.Project.BudgetAmount, snap.Project.BudgetCurrency)
	cur := normalizeBudgetPair(p.BudgetAmount, p.BudgetCurrency)
	same := true
	switch {
	case base.amount == nil && cur.amount == nil:
	case base.amount == nil || cur.amount == nil:
		same = false
	default:
		same = *base.amount == *cur.amount && base.currency == cur.currency
	}
	if same {
		return "", false
	}
	return fmt.Sprintf("预算相对已批准基线发生变化：%s → %s", formatBudgetPair(base), formatBudgetPair(cur)), true
}

// formatBudgetPair 预算展示（与前端列表「金额 币种」风格一致）。
func formatBudgetPair(pair budgetPair) string {
	if pair.amount == nil {
		return "未设置"
	}
	amount := fmt.Sprintf("%v", *pair.amount)
	if pair.currency != "" {
		return amount + " " + pair.currency
	}
	return amount + "（租户默认币种）"
}
