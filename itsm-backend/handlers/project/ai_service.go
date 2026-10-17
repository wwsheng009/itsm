package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"itsm-backend/ent"

	"go.uber.org/zap"
)

// PE-25 项目 AI 服务编排（D-44~D-48）。
//
// 分层：事实派生（本文件，实时、不落库）→ 端口调用（ai_ports.go / ai_adapter.go）
// → 降级决策（本文件）→ 审计（ai_handler.go）。AI 失败不影响主流程：三端点均不写业务状态。

// SetAIPort 注入 AI 端口（bootstrap；未注入 = 降级模式，端点仍 200）。
func (s *Service) SetAIPort(port ProjectAIPort) {
	s.aiPort = port
}

// ---------------------------------------------------------------------------
// 服务方法（三端点）
// ---------------------------------------------------------------------------

// GenerateAISummary 生成项目周报/状态摘要（D-44）：
// 事实派生（PE-17 同源）→ LLM；失败/未装配 → 确定性事实摘要（generatedBy=deterministic）。
func (s *Service) GenerateAISummary(ctx context.Context, tenantID, projectID, viewerID int, admin bool, req *AISummaryRequest) (*AISummaryResponse, error) {
	cadence := ReportCadenceWeekly
	if req != nil && strings.TrimSpace(req.Cadence) != "" {
		cadence = strings.TrimSpace(req.Cadence)
		if !validReportCadences[cadence] {
			return nil, fmt.Errorf("%w: invalid cadence %q", ErrValidation, req.Cadence)
		}
	}

	facts, err := s.gatherAIFacts(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}

	resp := &AISummaryResponse{
		ProjectID:     projectID,
		Cadence:       cadence,
		GeneratedAt:   s.now(),
		GeneratedBy:   AIGeneratedByDeterministic,
		Degraded:      true,
		DegradeReason: AIDegradeAIUnavailable,
		PromptChars:   utf8.RuneCountInString(facts.PromptText),
	}
	if s.aiPort != nil {
		text, aiErr := s.aiPort.Summarize(ctx, tenantID, viewerID, projectID, cadence, facts)
		if aiErr != nil {
			s.logAIFailure("summary", projectID, aiErr)
			resp.DegradeReason = classifyAIDegrade(aiErr)
		} else {
			resp.Summary = strings.TrimSpace(text)
			resp.GeneratedBy = AIGeneratedByAI
			resp.Degraded = false
			resp.DegradeReason = ""
			resp.Model = aiModelName(s.aiPort)
		}
	}
	if resp.GeneratedBy != AIGeneratedByAI || strings.TrimSpace(resp.Summary) == "" {
		// 降级：事实派生摘要（同源数字，不臆造）。
		resp.Summary = buildDeterministicSummary(facts)
		resp.GeneratedBy = AIGeneratedByDeterministic
		resp.Degraded = true
		if resp.DegradeReason == "" {
			resp.DegradeReason = AIDegradeAIUnavailable
		}
	}
	return resp, nil
}

// GenerateAIRiskWarnings 风险预警（D-45）：规则信号恒返回；AI 判断为增强段（失败 → 空段 + 降级）。
func (s *Service) GenerateAIRiskWarnings(ctx context.Context, tenantID, projectID, viewerID int, admin bool) (*AIRiskWarningsResponse, error) {
	facts, err := s.gatherAIFacts(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}

	resp := &AIRiskWarningsResponse{
		ProjectID:     projectID,
		Signals:       facts.Signals,
		SignalTotal:   facts.SignalTotal,
		Truncated:     facts.SignalsTruncated,
		Warnings:      []AIRiskWarning{},
		GeneratedAt:   s.now(),
		GeneratedBy:   AIGeneratedByRules,
		Degraded:      true,
		DegradeReason: AIDegradeAIUnavailable,
	}
	if s.aiPort != nil {
		warnings, aiErr := s.aiPort.AssessRisks(ctx, tenantID, viewerID, projectID, facts)
		if aiErr != nil {
			s.logAIFailure("risk-warnings", projectID, aiErr)
			resp.DegradeReason = classifyAIDegrade(aiErr)
		} else {
			// 服务层再归一一次（防御：端口实现可能未归一）。
			resp.Warnings = normalizeAIWarnings(warnings)
			resp.GeneratedBy = AIGeneratedByAI
			resp.Degraded = false
			resp.DegradeReason = ""
			resp.Model = aiModelName(s.aiPort)
		}
	}
	return resp, nil
}

// ExtractTasksFromMinutes 纪要转任务（D-46）：只出建议，不落库；AI 失败 → 空提案 + none。
func (s *Service) ExtractTasksFromMinutes(ctx context.Context, tenantID, projectID, viewerID int, admin bool, req *AIMinutesTasksRequest) (*AIMinutesTasksResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: content is required", ErrValidation)
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("%w: content is required", ErrValidation)
	}
	contentChars := utf8.RuneCountInString(content)
	if contentChars > AIMaxMinutesChars {
		return nil, fmt.Errorf("%w: content exceeds %d characters", ErrValidation, AIMaxMinutesChars)
	}
	limit := req.Limit
	if limit == 0 {
		limit = AIProposalLimitDefault
	}
	if limit < AIProposalLimitMin || limit > AIProposalLimitMax {
		return nil, fmt.Errorf("%w: limit must be between %d and %d", ErrValidation, AIProposalLimitMin, AIProposalLimitMax)
	}

	// 可见性：同详情（不可见统一 404）。
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}

	resp := &AIMinutesTasksResponse{
		ProjectID:     projectID,
		Proposals:     []AITaskProposal{},
		GeneratedAt:   s.now(),
		GeneratedBy:   AIGeneratedByNone,
		Degraded:      true,
		DegradeReason: AIDegradeAIUnavailable,
		ContentChars:  contentChars,
	}
	if s.aiPort != nil {
		proposals, aiErr := s.aiPort.ExtractTasks(ctx, tenantID, viewerID, projectID, content, limit)
		if aiErr != nil {
			s.logAIFailure("minutes-extract", projectID, aiErr)
			resp.DegradeReason = classifyAIDegrade(aiErr)
		} else {
			// 服务层再归一一次（丢弃空名/未知优先级/非正工时）。
			resp.Proposals = normalizeAITaskProposals(proposals, limit)
			resp.GeneratedBy = AIGeneratedByAI
			resp.Degraded = false
			resp.DegradeReason = ""
			resp.Model = aiModelName(s.aiPort)
		}
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// 事实派生（与 PE-17 报告同源；实时计算不落库）
// ---------------------------------------------------------------------------

// gatherAIFacts 汇总 AI 输入事实：项目/任务/里程碑/阶段门/基线偏差/风险 + 规则信号 + 紧凑提示文本。
func (s *Service) gatherAIFacts(ctx context.Context, tenantID, projectID, viewerID int, admin bool) (*AIProjectFacts, error) {
	p, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	milestones, err := s.repo.ListMilestones(ctx, tenantID, projectID, "")
	if err != nil {
		return nil, err
	}
	gates, err := s.repo.ListGates(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	risks, err := s.ListRisks(ctx, tenantID, projectID, viewerID, admin, "", "", "")
	if err != nil {
		return nil, err
	}

	now := s.now()
	facts := &AIProjectFacts{
		ProjectID:      p.ID,
		ProjectName:    p.Name,
		ProjectCode:    p.Code,
		Status:         normalizeStatus(p.Status),
		Health:         p.Health,
		Progress:       p.Progress,
		EndDate:        p.EndDate,
		ManagerID:      p.ManagerID,
		TaskStats:      buildReportTaskStats(tasks, now),
		MilestoneStats: buildReportMilestoneStats(milestones, now),
		Gates:          buildReportGates(gates),
	}
	if ref, ok := s.OwnerTeamRefs(ctx, tenantID, []*ent.Project{p})[p.OwnerTeamID]; ok && p.OwnerTeamID > 0 {
		facts.OwnerTeam = ref.Name
	}

	// 基线偏差：无基线（ErrBaselineNotFound）跳过；其他错误不吞（与报告同口径）。
	diff, diffErr := s.BaselineDiff(ctx, tenantID, projectID, viewerID, admin)
	switch {
	case diffErr == nil && diff != nil:
		createdAt := diff.CreatedAt
		facts.Variance = &ReportVarianceSummary{
			BaselineVersion:   diff.Version,
			BaselineCreatedAt: &createdAt,
			Added:             diff.TaskSummary.Added + diff.MilestoneSummary.Added,
			Removed:           diff.TaskSummary.Removed + diff.MilestoneSummary.Removed,
			Changed:           diff.TaskSummary.Changed + diff.MilestoneSummary.Changed,
			Unchanged:         diff.TaskSummary.Unchanged + diff.MilestoneSummary.Unchanged,
		}
	case diffErr != nil && !errors.Is(diffErr, ErrBaselineNotFound):
		return nil, diffErr
	}

	for _, r := range risks {
		facts.Risks = append(facts.Risks, AIRiskFact{Title: r.Title, Kind: r.Kind, Level: r.Level, Status: r.Status})
	}

	facts.Signals, facts.SignalTotal, facts.SignalsTruncated = deriveAIRiskSignals(p, tasks, milestones, gates, risks, now)
	facts.PromptText = buildAIFactsPrompt(facts, tasks, milestones, risks)

	return facts, nil
}

// ---------------------------------------------------------------------------
// 规则信号（D-45；确定性口径，AI 不可用时同样返回）
// ---------------------------------------------------------------------------

// deriveAIRiskSignals 规则派生风险信号：每类明细上限 aiSignalsPerTypeCap；
// SignalTotal=截断前全量条数（不静默）。
func deriveAIRiskSignals(
	p *ent.Project,
	tasks []*ent.ProjectTask,
	milestones []*ent.ProjectMilestone,
	gates []*ent.ProjectGate,
	risks []*ent.ProjectRisk,
	now time.Time,
) (signals []AIRiskSignal, total int, truncated bool) {
	signals = make([]AIRiskSignal, 0, aiSignalsPerTypeCap*2)
	perType := make(map[string]int)

	appendSignal := func(sig AIRiskSignal) {
		total++
		if perType[sig.Type] >= aiSignalsPerTypeCap {
			truncated = true
			return
		}
		perType[sig.Type]++
		signals = append(signals, sig)
	}

	// 项目整体逾期：结束日期已过且未终结。
	if p != nil && !p.EndDate.IsZero() && p.EndDate.Before(now) &&
		p.Status != StatusCompleted && p.Status != StatusCancelled {
		appendSignal(AIRiskSignal{
			Type: AIRiskSignalOverdueProject, Severity: AISeverityCritical, RefType: "project", RefID: p.ID,
			Title:  "项目已超出计划结束日期",
			Detail: fmt.Sprintf("计划结束 %s 已过，当前状态 %s、进度 %d%%", p.EndDate.Format("2006-01-02"), p.Status, p.Progress),
		})
	}

	// 未终结任务逾期 / 阻塞。
	for _, task := range tasks {
		if task == nil || !taskIsOpen(task.Status) {
			continue
		}
		if !task.PlanEnd.IsZero() && task.PlanEnd.Before(now) {
			appendSignal(AIRiskSignal{
				Type: AIRiskSignalOverdueTask, Severity: AISeverityWarning, RefType: "task", RefID: task.ID,
				Title:  fmt.Sprintf("任务逾期：%s", task.Name),
				Detail: fmt.Sprintf("计划结束 %s 已过，当前状态 %s", task.PlanEnd.Format("2006-01-02"), task.Status),
			})
		}
		if task.Status == TaskStatusBlocked {
			appendSignal(AIRiskSignal{
				Type: AIRiskSignalBlockedTask, Severity: AISeverityWarning, RefType: "task", RefID: task.ID,
				Title:  fmt.Sprintf("任务阻塞：%s", task.Name),
				Detail: "任务处于 blocked，需解除阻塞或重新排期",
			})
		}
	}

	// planned 里程碑逾期。
	for _, milestone := range milestones {
		if milestone == nil || milestone.Status != MilestoneStatusPlanned {
			continue
		}
		if !milestone.DueDate.IsZero() && milestone.DueDate.Before(now) {
			appendSignal(AIRiskSignal{
				Type: AIRiskSignalOverdueMilestone, Severity: AISeverityWarning, RefType: "milestone", RefID: milestone.ID,
				Title:  fmt.Sprintf("里程碑逾期未达成：%s", milestone.Name),
				Detail: fmt.Sprintf("目标日期 %s 已过，状态仍为 planned", milestone.DueDate.Format("2006-01-02")),
			})
		}
	}

	// 高等级未关闭风险。
	for _, risk := range risks {
		if risk == nil || risk.Level != RiskLevelHigh {
			continue
		}
		if risk.Status != RiskStatusOpen && risk.Status != RiskStatusMitigating {
			continue
		}
		appendSignal(AIRiskSignal{
			Type: AIRiskSignalHighRisk, Severity: AISeverityCritical, RefType: "risk", RefID: risk.ID,
			Title:  fmt.Sprintf("高等级风险未关闭：%s", risk.Title),
			Detail: fmt.Sprintf("level=high，status=%s", risk.Status),
		})
	}

	// 阶段门被拒绝。
	for _, gate := range gates {
		if gate == nil || gate.Status != GateStatusRejected {
			continue
		}
		appendSignal(AIRiskSignal{
			Type: AIRiskSignalGateRejected, Severity: AISeverityWarning, RefType: "gate", RefID: gate.ID,
			Title:  fmt.Sprintf("阶段门被拒绝：%s", gate.Gate),
			Detail: "需整改后重新提交（reject_count=" + fmt.Sprintf("%d", gate.RejectCount) + "）",
		})
	}

	return signals, total, truncated
}

// ---------------------------------------------------------------------------
// 提示文本与确定性摘要（纯函数）
// ---------------------------------------------------------------------------

// buildAIFactsPrompt 构建紧凑事实文本（≤aiFactsPromptCap 字符；截断在末尾发生）。
func buildAIFactsPrompt(facts *AIProjectFacts, tasks []*ent.ProjectTask, milestones []*ent.ProjectMilestone, risks []*ent.ProjectRisk) string {
	if facts == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "项目：%s（%s）｜状态：%s｜健康度：%s｜进度：%d%%\n",
		facts.ProjectName, facts.ProjectCode, facts.Status, facts.Health, facts.Progress)
	if !facts.EndDate.IsZero() {
		fmt.Fprintf(&b, "计划结束：%s\n", facts.EndDate.Format("2006-01-02"))
	}
	if facts.OwnerTeam != "" {
		fmt.Fprintf(&b, "归口团队：%s\n", facts.OwnerTeam)
	}
	fmt.Fprintf(&b, "任务：总数 %d｜完成 %d｜未终结 %d｜取消 %d｜逾期 %d｜3天内到期 %d\n",
		facts.TaskStats.Total, facts.TaskStats.Done, facts.TaskStats.Open,
		facts.TaskStats.Cancelled, facts.TaskStats.Overdue, facts.TaskStats.DueSoon)
	fmt.Fprintf(&b, "里程碑：总数 %d｜达成 %d｜计划 %d｜错过 %d｜逾期未达成 %d｜按期达成 %d\n",
		facts.MilestoneStats.Total, facts.MilestoneStats.Reached, facts.MilestoneStats.Planned,
		facts.MilestoneStats.Missed, facts.MilestoneStats.Overdue, facts.MilestoneStats.OnTime)
	if len(facts.Gates) > 0 {
		parts := make([]string, 0, len(facts.Gates))
		for _, gate := range facts.Gates {
			parts = append(parts, fmt.Sprintf("%s=%s", gate.Gate, gate.Status))
		}
		fmt.Fprintf(&b, "阶段门：%s\n", strings.Join(parts, "｜"))
	}
	if facts.Variance != nil {
		fmt.Fprintf(&b, "基线偏差：v%d｜新增 %d｜移除 %d｜变更 %d｜未变 %d\n",
			facts.Variance.BaselineVersion, facts.Variance.Added, facts.Variance.Removed,
			facts.Variance.Changed, facts.Variance.Unchanged)
	} else {
		b.WriteString("基线偏差：未冻结基线\n")
	}
	highOpen := 0
	for _, r := range facts.Risks {
		if r.Level == RiskLevelHigh && (r.Status == RiskStatusOpen || r.Status == RiskStatusMitigating) {
			highOpen++
		}
	}
	fmt.Fprintf(&b, "风险台账：共 %d 条（高等级未关闭 %d 条）\n", len(facts.Risks), highOpen)

	if len(facts.Signals) > 0 {
		fmt.Fprintf(&b, "规则信号（%d/%d 条，截断=%t）：\n", len(facts.Signals), facts.SignalTotal, facts.SignalsTruncated)
		for _, sig := range facts.Signals {
			fmt.Fprintf(&b, "- [%s/%s] %s：%s\n", sig.Type, sig.Severity, sig.Title, sig.Detail)
		}
	}

	// 未终结任务明细（前 10 条）——给 LLM 可引用的具体条目。
	openTasks := make([]*ent.ProjectTask, 0, 10)
	for _, task := range tasks {
		if task == nil || !taskIsOpen(task.Status) {
			continue
		}
		openTasks = append(openTasks, task)
		if len(openTasks) >= 10 {
			break
		}
	}
	if len(openTasks) > 0 {
		b.WriteString("未终结任务（前 10 条）：\n")
		for _, task := range openTasks {
			due := "-"
			if !task.PlanEnd.IsZero() {
				due = task.PlanEnd.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "- [%s] %s（截止 %s，估 %.1fh）\n", task.Status, task.Name, due, task.EstimateHours)
		}
	}

	// 未达成里程碑（前 10 条）。
	pendingMilestones := make([]*ent.ProjectMilestone, 0, 10)
	for _, milestone := range milestones {
		if milestone == nil || milestone.Status == MilestoneStatusReached {
			continue
		}
		pendingMilestones = append(pendingMilestones, milestone)
		if len(pendingMilestones) >= 10 {
			break
		}
	}
	if len(pendingMilestones) > 0 {
		b.WriteString("未达成里程碑（前 10 条）：\n")
		for _, milestone := range pendingMilestones {
			due := "-"
			if !milestone.DueDate.IsZero() {
				due = milestone.DueDate.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "- [%s] %s（目标 %s）\n", milestone.Status, milestone.Name, due)
		}
	}

	// 未关闭风险（前 10 条）。
	openRisks := make([]*ent.ProjectRisk, 0, 10)
	for _, risk := range risks {
		if risk == nil || risk.Status == RiskStatusClosed {
			continue
		}
		openRisks = append(openRisks, risk)
		if len(openRisks) >= 10 {
			break
		}
	}
	if len(openRisks) > 0 {
		b.WriteString("未关闭风险/问题（前 10 条）：\n")
		for _, risk := range openRisks {
			fmt.Fprintf(&b, "- [%s/%s] %s（%s）\n", risk.Level, risk.Status, risk.Title, risk.Kind)
		}
	}

	return truncateRunes(b.String(), aiFactsPromptCap)
}

// buildDeterministicSummary 降级摘要：完全由事实派生（同源数字，不臆造）。
func buildDeterministicSummary(facts *AIProjectFacts) string {
	if facts == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 项目状态（事实派生）\n\n")
	fmt.Fprintf(&b, "- **%s（%s）**：状态 %s｜健康度 %s｜进度 %d%%\n",
		facts.ProjectName, facts.ProjectCode, facts.Status, facts.Health, facts.Progress)
	if !facts.EndDate.IsZero() {
		fmt.Fprintf(&b, "- 计划结束：%s\n", facts.EndDate.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- 任务：%d 项（完成 %d / 未终结 %d / 取消 %d）；逾期 %d；3 天内到期 %d\n",
		facts.TaskStats.Total, facts.TaskStats.Done, facts.TaskStats.Open,
		facts.TaskStats.Cancelled, facts.TaskStats.Overdue, facts.TaskStats.DueSoon)
	fmt.Fprintf(&b, "- 里程碑：%d 项（达成 %d / 计划 %d / 错过 %d）；逾期未达成 %d\n",
		facts.MilestoneStats.Total, facts.MilestoneStats.Reached,
		facts.MilestoneStats.Planned, facts.MilestoneStats.Missed, facts.MilestoneStats.Overdue)

	critical, warning := 0, 0
	for _, sig := range facts.Signals {
		switch sig.Severity {
		case AISeverityCritical:
			critical++
		case AISeverityWarning:
			warning++
		}
	}
	fmt.Fprintf(&b, "- 风险信号：%d 条（critical %d / warning %d）\n", facts.SignalTotal, critical, warning)

	b.WriteString("\n> AI 摘要不可用（" + AIDegradeAIUnavailable + "），以下为规则提示：\n")
	if len(facts.Signals) == 0 {
		b.WriteString("> 暂无逾期/阻塞/高风险信号。\n")
		return b.String()
	}
	for _, sig := range facts.Signals {
		fmt.Fprintf(&b, "> - [%s] %s（%s）\n", sig.Severity, sig.Title, sig.Detail)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 降级辅助
// ---------------------------------------------------------------------------

// classifyAIDegrade 错误 → 降级原因词表（D-44）。
func classifyAIDegrade(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAIParseOutput):
		return AIDegradeParseError
	case errors.Is(err, ErrAIPortUnavailable):
		return AIDegradeAIUnavailable
	default:
		return AIDegradeLLMError
	}
}

// logAIFailure 记录 AI 失败（仅日志 + 降级字段；不向上抛错）。
func (s *Service) logAIFailure(scene string, projectID int, err error) {
	logger := s.logger
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	logger.Warnw("project ai degraded", "scene", scene, "project_id", projectID, "error", err)
}