package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
)

// 结项清单服务（PE-15）。
//
// 职责：
//   - 清单读（自动项实时派生 + 人工/产物项读落库记录）；
//   - 动作：生成复盘 KB 草稿（幂等）/ 资产登记确认或豁免 / 成员回收 / CSAT 触发（幂等）；
//   - 门禁：complete 前要求 manual 项齐备（requireClosureChecklist，由 Transition 调用）；
//   - 结项副作用：complete 成功后尽力触发 CSAT（失败不阻塞结项——契约 §4）。

// ErrClosurePortUnavailable 结项动作依赖的服务未注入（bootstrap 未装配 → 503）。
var ErrClosurePortUnavailable = errors.New("project closure service port not configured")

// retroDraftCategory 复盘草稿知识库分类（须在 knowledge 分类白名单内）。
const retroDraftCategory = "项目复盘"

// GetClosure 结项清单（可见性谓词；无记录时返回零态视图，不产生写副作用）。
func (s *Service) GetClosure(
	ctx context.Context, tenantID, projectID, viewerID int, admin bool,
) (*ProjectClosureResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.GetClosure(ctx, tenantID, projectID)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if ent.IsNotFound(err) {
		record = nil
	}
	return s.buildClosureView(ctx, tenantID, project, record)
}

// GenerateRetroDraft 生成复盘 KB 草稿（幂等：已生成直接回显；不自动发布）。
func (s *Service) GenerateRetroDraft(
	ctx context.Context, tenantID, projectID, actorID int, admin bool,
) (*ProjectClosureResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可生成复盘草稿", ErrValidation)
	}
	record, err := s.ensureClosureRecord(ctx, tenantID, projectID, actorID)
	if err != nil {
		return nil, err
	}
	if record.RetroKBArticleID > 0 {
		// 幂等：已生成直接回显当前清单。
		return s.buildClosureView(ctx, tenantID, project, record)
	}
	if s.closureKnowledge == nil {
		return nil, ErrClosurePortUnavailable
	}
	content, err := s.buildRetroDraftContent(ctx, tenantID, project)
	if err != nil {
		return nil, err
	}
	articleID, err := s.closureKnowledge.CreateRetroDraft(ctx, tenantID, actorID, RetroDraft{
		Title:    fmt.Sprintf("项目复盘：%s %s", project.Code, project.Name),
		Content:  content,
		Category: retroDraftCategory,
		Tags:     []string{"项目复盘", project.Code},
	})
	if err != nil {
		return nil, err
	}
	now := s.now()
	updated, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, ClosurePatch{
		RetroKbArticleID: &articleID,
		RetroGeneratedBy: &actorID,
		RetroGeneratedAt: &now,
	})
	if err != nil {
		return nil, err
	}
	return s.buildClosureView(ctx, tenantID, project, updated)
}

// ConfirmAssets 资产/许可登记确认（confirm）或豁免（waive，必填原因）。
func (s *Service) ConfirmAssets(
	ctx context.Context, tenantID, projectID, actorID int, req *ClosureAssetsRequest, admin bool,
) (*ProjectClosureResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可登记结项资产", ErrValidation)
	}
	req.Normalize()
	if !req.Valid() {
		return nil, fmt.Errorf("%w: action 必须为 confirm|waive，waive 需填写原因", ErrValidation)
	}
	record, err := s.ensureClosureRecord(ctx, tenantID, projectID, actorID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	patch := ClosurePatch{}
	if req.Action == "confirm" {
		patch.AssetsConfirmedBy = &actorID
		patch.AssetsConfirmedAt = &now
		patch.AssetsNote = &req.Note
		patch.ClearAssetsWaived = true
	} else {
		patch.AssetsWaivedBy = &actorID
		patch.AssetsWaivedAt = &now
		patch.AssetsWaiveReason = &req.Reason
		patch.ClearAssetsConfirmed = true
	}
	updated, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, patch)
	if err != nil {
		return nil, err
	}
	return s.buildClosureView(ctx, tenantID, project, updated)
}

// RecycleMembers 成员回收：回收 member 角色活跃成员（pm/stakeholder 保留，见 D-19）。
// 可重复执行以回收结项前新加入的成员；回收数量为最近一次执行结果。
func (s *Service) RecycleMembers(
	ctx context.Context, tenantID, projectID, actorID int, admin bool,
) (*ProjectClosureResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可执行成员回收", ErrValidation)
	}
	record, err := s.ensureClosureRecord(ctx, tenantID, projectID, actorID)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.RecycleProjectMembers(ctx, tenantID, projectID, project.ManagerID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	updated, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, ClosurePatch{
		MembersRecycledBy: &actorID,
		MembersRecycledAt: &now,
		MembersCount:      &count,
	})
	if err != nil {
		return nil, err
	}
	return s.buildClosureView(ctx, tenantID, project, updated)
}

// TriggerCSAT 触发结项 CSAT 调研（幂等：已触发直接回显）。
func (s *Service) TriggerCSAT(
	ctx context.Context, tenantID, projectID, actorID int, admin bool,
) (*ProjectClosureResponse, error) {
	project, err := s.GetForViewer(ctx, tenantID, projectID, actorID, admin)
	if err != nil {
		return nil, err
	}
	if project.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: 已取消项目不可触发 CSAT", ErrValidation)
	}
	record, err := s.ensureClosureRecord(ctx, tenantID, projectID, actorID)
	if err != nil {
		return nil, err
	}
	if record.CsatSurveyID > 0 {
		return s.buildClosureView(ctx, tenantID, project, record)
	}
	if s.closureSurvey == nil {
		return nil, ErrClosurePortUnavailable
	}
	surveyID, err := s.closureSurvey.CreateCSATSurvey(ctx, tenantID, CSATSurveyInput{
		Title: fmt.Sprintf("项目结项满意度调研：%s %s", project.Code, project.Name),
		Description: fmt.Sprintf(
			"请对项目 %s（%s）的交付过程与结果进行满意度评价（CSAT）。", project.Name, project.Code),
	})
	if err != nil {
		return nil, err
	}
	now := s.now()
	updated, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, ClosurePatch{
		CSATSurveyID:    &surveyID,
		CSATTriggeredAt: &now,
	})
	if err != nil {
		return nil, err
	}
	return s.buildClosureView(ctx, tenantID, project, updated)
}

// requireClosureChecklist 结项清单门禁（complete 前）：manual 项齐备（retro/assets/members）。
// 自动项（任务/单据/门）由既有 closureBlockers + requireGatePassed 单独暴露，避免重复。
func (s *Service) requireClosureChecklist(ctx context.Context, tenantID, projectID int) error {
	record, err := s.repo.GetClosure(ctx, tenantID, projectID)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if ent.IsNotFound(err) {
		record = nil
	}
	blockers := make([]Blocker, 0, 3)
	if record == nil || record.RetroKBArticleID <= 0 {
		blockers = append(blockers, Blocker{
			Type:   ClosureItemRetro,
			Title:  "复盘 KB 草稿未生成（结项清单动作）",
			Status: ClosureItemPending,
		})
	}
	if record == nil || (record.AssetsConfirmedAt == nil && record.AssetsWaivedAt == nil) {
		blockers = append(blockers, Blocker{
			Type:   ClosureItemAssets,
			Title:  "资产/许可登记未确认（可豁免并说明原因）",
			Status: ClosureItemPending,
		})
	}
	if record == nil || record.MembersRecycledAt == nil {
		blockers = append(blockers, Blocker{
			Type:   ClosureItemMembers,
			Title:  "成员回收未执行",
			Status: ClosureItemPending,
		})
	}
	if len(blockers) > 0 {
		return &ClosureBlockedError{Blockers: blockers}
	}
	return nil
}

// autoTriggerCSAT 结项成功后的 CSAT 触发（尽力而为：失败仅记日志，不阻塞结项）。
func (s *Service) autoTriggerCSAT(ctx context.Context, tenantID, projectID int) {
	if s.closureSurvey == nil {
		return
	}
	project, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		s.logger.Warnw("结项 CSAT：项目读取失败", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	record, err := s.repo.GetClosure(ctx, tenantID, projectID)
	if err != nil && !ent.IsNotFound(err) {
		s.logger.Warnw("结项 CSAT：清单读取失败", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	if ent.IsNotFound(err) {
		record, err = s.repo.CreateClosure(ctx, tenantID, projectID, 0)
		if err != nil {
			if reread, rereadErr := s.repo.GetClosure(ctx, tenantID, projectID); rereadErr == nil {
				record = reread
			} else {
				s.logger.Warnw("结项 CSAT：清单创建失败", "err", err, "projectId", projectID, "tenantId", tenantID)
				return
			}
		}
	}
	if record.CsatSurveyID > 0 {
		return
	}
	surveyID, err := s.closureSurvey.CreateCSATSurvey(ctx, tenantID, CSATSurveyInput{
		Title: fmt.Sprintf("项目结项满意度调研：%s %s", project.Code, project.Name),
		Description: fmt.Sprintf(
			"请对项目 %s（%s）的交付过程与结果进行满意度评价（CSAT）。", project.Name, project.Code),
	})
	if err != nil {
		s.logger.Warnw("结项 CSAT 触发失败（不阻塞结项）", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	now := s.now()
	if _, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, ClosurePatch{
		CSATSurveyID:    &surveyID,
		CSATTriggeredAt: &now,
	}); err != nil {
		s.logger.Warnw("结项 CSAT 回写失败（不阻塞结项）", "err", err, "projectId", projectID, "tenantId", tenantID)
	}
}

// autoGenerateRetroDraft 结项门提交时的复盘草稿自动生成（尽力而为：失败仅记日志，
// 不阻塞门提交；清单项保持 pending，可由结项清单手动生成重试）。
func (s *Service) autoGenerateRetroDraft(ctx context.Context, tenantID, projectID, actorID int) {
	if s.closureKnowledge == nil {
		return
	}
	project, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		s.logger.Warnw("结项复盘草稿：项目读取失败", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	record, err := s.ensureClosureRecord(ctx, tenantID, projectID, actorID)
	if err != nil {
		s.logger.Warnw("结项复盘草稿：清单读取失败", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	if record.RetroKBArticleID > 0 {
		return
	}
	content, err := s.buildRetroDraftContent(ctx, tenantID, project)
	if err != nil {
		s.logger.Warnw("结项复盘草稿：正文生成失败", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	articleID, err := s.closureKnowledge.CreateRetroDraft(ctx, tenantID, actorID, RetroDraft{
		Title:    fmt.Sprintf("项目复盘：%s %s", project.Code, project.Name),
		Content:  content,
		Category: retroDraftCategory,
		Tags:     []string{"项目复盘", project.Code},
	})
	if err != nil {
		s.logger.Warnw("结项复盘草稿自动生成失败（不阻塞门提交）", "err", err, "projectId", projectID, "tenantId", tenantID)
		return
	}
	now := s.now()
	if _, err := s.repo.UpdateClosureCAS(ctx, tenantID, record.ID, record.Version, ClosurePatch{
		RetroKbArticleID: &articleID,
		RetroGeneratedBy: &actorID,
		RetroGeneratedAt: &now,
	}); err != nil {
		s.logger.Warnw("结项复盘草稿回写失败", "err", err, "projectId", projectID, "tenantId", tenantID)
	}
}

// ensureClosureRecord 取清单记录；不存在则创建（并发创建冲突时回读）。
func (s *Service) ensureClosureRecord(
	ctx context.Context, tenantID, projectID, actorID int,
) (*ent.ProjectClosure, error) {
	record, err := s.repo.GetClosure(ctx, tenantID, projectID)
	if err == nil {
		return record, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	created, createErr := s.repo.CreateClosure(ctx, tenantID, projectID, actorID)
	if createErr != nil {
		if reread, rereadErr := s.repo.GetClosure(ctx, tenantID, projectID); rereadErr == nil {
			return reread, nil
		}
		return nil, createErr
	}
	return created, nil
}

// buildClosureView 组装清单视图：自动项实时派生 + 落库项合并 + 阻塞明细。
func (s *Service) buildClosureView(
	ctx context.Context, tenantID int, project *ent.Project, record *ent.ProjectClosure,
) (*ProjectClosureResponse, error) {
	items := make([]ClosureItem, 0, len(closureChecklistOrder))
	blockers := make([]Blocker, 0, 8)

	openTasks, err := s.repo.CountOpenTasks(ctx, tenantID, project.ID)
	if err != nil {
		return nil, err
	}
	tasksItem := ClosureItem{Key: ClosureItemTasks, Title: closureItemTitles[ClosureItemTasks], Blocking: true}
	if openTasks == 0 {
		tasksItem.Status = ClosureItemDone
	} else {
		tasksItem.Status = ClosureItemPending
		tasksItem.Detail = fmt.Sprintf("%d 个未终结任务", openTasks)
		blockers = append(blockers, Blocker{
			Type: ClosureItemTasks, Title: tasksItem.Detail, Status: "open",
		})
	}
	items = append(items, tasksItem)

	records, err := s.repo.NonTerminalRecords(ctx, tenantID, project.ID)
	if err != nil {
		return nil, err
	}
	recordsItem := ClosureItem{Key: ClosureItemRecords, Title: closureItemTitles[ClosureItemRecords], Blocking: true}
	if len(records) == 0 {
		recordsItem.Status = ClosureItemDone
	} else {
		recordsItem.Status = ClosureItemPending
		recordsItem.Detail = fmt.Sprintf("%d 个关联单据未终结", len(records))
		blockers = append(blockers, records...)
	}
	items = append(items, recordsItem)

	gateItem := ClosureItem{Key: ClosureItemGate, Title: closureItemTitles[ClosureItemGate], Blocking: true}
	gateStatus := GateStatusPending
	gateRecord, err := s.repo.GetGate(ctx, tenantID, project.ID, GateClosure)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if gateRecord != nil {
		gateStatus = gateRecord.Status
	}
	if gateStatus == GateStatusPassed {
		gateItem.Status = ClosureItemDone
	} else {
		gateItem.Status = ClosureItemPending
		gateItem.Detail = fmt.Sprintf("结项门当前状态：%s", gateStatus)
		blockers = append(blockers, Blocker{
			Type: ClosureItemGate, Title: "结项阶段门未通过", Status: gateStatus,
		})
	}
	items = append(items, gateItem)

	retroItem := ClosureItem{
		Key: ClosureItemRetro, Title: closureItemTitles[ClosureItemRetro],
		Blocking: true, Action: ClosureActionGenerateRetro,
	}
	if record != nil && record.RetroKBArticleID > 0 {
		retroItem.Status = ClosureItemDone
		retroItem.Detail = fmt.Sprintf("草稿已生成（文章 #%d，未发布）", record.RetroKBArticleID)
	} else {
		retroItem.Status = ClosureItemPending
		retroItem.Detail = "生成复盘草稿（不自动发布）"
		blockers = append(blockers, Blocker{
			Type: ClosureItemRetro, Title: "复盘 KB 草稿未生成", Status: ClosureItemPending,
		})
	}
	items = append(items, retroItem)

	// PE-26：采购/资产结项登记明细（读时富化；失败降级为空列表，不阻断清单读）。
	linkList, linkErr := s.buildAssetLinkList(ctx, tenantID, project.ID)
	if linkErr != nil {
		s.logger.Warnw("结项清单：资产登记明细读取失败（降级为空）",
			"err", linkErr, "projectId", project.ID, "tenantId", tenantID)
		linkList = &AssetLinkListResponse{Items: []AssetLinkItem{}, Summary: newAssetLinkSummary()}
	}
	linkSummaryText := assetLinkSummaryText(linkList.Summary)

	assetsItem := ClosureItem{
		Key: ClosureItemAssets, Title: closureItemTitles[ClosureItemAssets],
		Blocking: true, Action: ClosureActionConfirmAssets,
	}
	switch {
	case record != nil && record.AssetsConfirmedAt != nil:
		assetsItem.Status = ClosureItemDone
		assetsItem.Detail = "已完成资产/许可登记确认"
		if linkSummaryText != "" {
			assetsItem.Detail += "（" + linkSummaryText + "）"
		}
	case record != nil && record.AssetsWaivedAt != nil:
		assetsItem.Status = ClosureItemDone
		assetsItem.Detail = "已豁免：" + record.AssetsWaiveReason
	default:
		assetsItem.Status = ClosureItemPending
		assetsItem.Detail = "确认资产/许可登记，或豁免并说明原因"
		if linkSummaryText != "" {
			assetsItem.Detail = linkSummaryText + "；请确认或豁免"
		}
		blockers = append(blockers, Blocker{
			Type: ClosureItemAssets, Title: "资产/许可登记未确认", Status: ClosureItemPending,
		})
	}
	items = append(items, assetsItem)

	membersItem := ClosureItem{
		Key: ClosureItemMembers, Title: closureItemTitles[ClosureItemMembers],
		Blocking: true, Action: ClosureActionRecycleMembers,
	}
	if record != nil && record.MembersRecycledAt != nil {
		membersItem.Status = ClosureItemDone
		membersItem.Detail = fmt.Sprintf("已回收 %d 人（保留 PM 与干系人）", record.MembersRecycledCount)
	} else {
		membersItem.Status = ClosureItemPending
		membersItem.Detail = "回收 member 角色成员（保留 PM 与干系人）"
		blockers = append(blockers, Blocker{
			Type: ClosureItemMembers, Title: "成员回收未执行", Status: ClosureItemPending,
		})
	}
	items = append(items, membersItem)

	csatItem := ClosureItem{
		Key: ClosureItemCSAT, Title: closureItemTitles[ClosureItemCSAT],
		Blocking: false, Action: ClosureActionTriggerCSAT,
	}
	if record != nil && record.CsatSurveyID > 0 {
		csatItem.Status = ClosureItemDone
		csatItem.Detail = fmt.Sprintf("调研已触发（survey #%d）", record.CsatSurveyID)
	} else {
		csatItem.Status = ClosureItemPending
		csatItem.Detail = "结项时自动触发；缺失不阻塞结项"
	}
	items = append(items, csatItem)

	resp := ToClosureResponse(project.ID, record, items, blockers, project.Status)
	resp.AssetsLinks = linkList.Items
	resp.AssetsLinkSummary = linkList.Summary
	resp.AssetsLinksTruncated = linkList.Truncated

	// 阻塞项截断保护：明细最多 10 条 + 汇总（records 可能很多）。
	resp.BlockerTotal = len(resp.Blockers)
	if len(resp.Blockers) > 10 {
		total := len(resp.Blockers)
		resp.Blockers = append(resp.Blockers[:10], Blocker{
			Type: "more", Title: fmt.Sprintf("另有 %d 项阻塞未展示", total-10), Status: "more",
		})
	}
	return &resp, nil
}

// buildRetroDraftContent 由执行事实生成复盘草稿正文（Markdown；人工补充复盘要点后发布）。
func (s *Service) buildRetroDraftContent(ctx context.Context, tenantID int, project *ent.Project) (string, error) {
	tasks, err := s.repo.ListTasks(ctx, tenantID, project.ID, "", 0)
	if err != nil {
		return "", err
	}
	milestones, err := s.repo.ListMilestones(ctx, tenantID, project.ID, "")
	if err != nil {
		return "", err
	}
	gates, err := s.repo.ListGates(ctx, tenantID, project.ID)
	if err != nil {
		return "", err
	}
	_, recordTotal, err := s.repo.ListRecords(ctx, tenantID, project.ID, "", "", 0, 1)
	if err != nil {
		return "", err
	}

	doneTasks, cancelledTasks := 0, 0
	for _, t := range tasks {
		switch t.Status {
		case "done":
			doneTasks++
		case "cancelled":
			cancelledTasks++
		}
	}
	reachedMilestones := 0
	for _, m := range milestones {
		if m.Status == "reached" {
			reachedMilestones++
		}
	}
	gateStatus := make(map[string]string, len(gates))
	for _, g := range gates {
		gateStatus[g.Gate] = g.Status
	}
	gateNames := map[string]string{
		GateInitiation: "立项", GatePlan: "计划", GateAcceptance: "验收", GateClosure: "结项",
	}
	gateLine := make([]string, 0, len(gateSequence))
	for _, g := range gateSequence {
		status := gateStatus[g]
		if status == "" {
			status = GateStatusPending
		}
		gateLine = append(gateLine, fmt.Sprintf("%s=%s", gateNames[g], status))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# 项目复盘：%s %s\n\n", project.Code, project.Name)
	b.WriteString("> 本草稿由结项清单自动生成（PE-15），请补充「复盘要点」后人工发布；系统不会自动发布。\n\n")
	if desc := strings.TrimSpace(project.Description); desc != "" {
		fmt.Fprintf(&b, "## 项目概述\n\n%s\n\n", desc)
	}
	b.WriteString("## 执行事实\n\n")
	fmt.Fprintf(&b, "- 项目类型：%s\n", project.Type)
	fmt.Fprintf(&b, "- 计划周期：%s ~ %s\n", formatDateOrDash(project.StartDate), formatDateOrDash(project.EndDate))
	fmt.Fprintf(&b, "- 实际起止：%s ~ %s\n", formatDateOrDash(project.ActualStart), formatDateOrDash(project.ActualEnd))
	fmt.Fprintf(&b, "- 任务：%d 个（完成 %d / 取消 %d / 未完成 %d）\n",
		len(tasks), doneTasks, cancelledTasks, len(tasks)-doneTasks-cancelledTasks)
	fmt.Fprintf(&b, "- 里程碑：%d 个（达成 %d）\n", len(milestones), reachedMilestones)
	fmt.Fprintf(&b, "- 关联单据：%d 个\n", recordTotal)
	fmt.Fprintf(&b, "- 阶段门：%s\n", strings.Join(gateLine, " / "))
	b.WriteString("\n## 复盘要点\n\n")
	b.WriteString("- 交付结果：\n- 偏差与原因（对照计划基线）：\n- 改进项：\n- 可沉淀的知识/模板：\n")
	return b.String(), nil
}

// formatDateOrDash 日期格式化（零值输出 "-"）。
func formatDateOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}
