package service

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectmember"
	"itsm-backend/ent/projectmilestone"
	"itsm-backend/ent/team"

	"go.uber.org/zap"
)

// PE-14 收尾：里程碑到期通知（outbox 投递审计）。
//
// 上游契约：docs/project-management/plan/p0-integration-contracts.md §6
// —— 事件 `project.milestone.due`，通知偏好 event_type `project_milestone_due`，
// 收件人 = PM（`projects.manager_id` ∪ 项目内 pm 角色活跃成员）+ 干系人（stakeholder 角色活跃成员）。
//
// 投递语义：与 `bpmn_timeout_scanner` 一致——扫描仅入箱（operational_commands），
// 不做 fire-and-forget；重复扫描由幂等键（里程碑 + 目标日期 + 触发类型）收敛，
// 同一里程碑的同一触发在其生命周期内只投递一次。
const (
	// ProjectMilestoneDueNotificationType 通知偏好/双词表事件类型（长词表）。
	ProjectMilestoneDueNotificationType = "project_milestone_due"
	// ProjectMilestoneDueDefaultWindow 默认到期提醒窗口（提前 3 天，可配）。
	ProjectMilestoneDueDefaultWindow = 72 * time.Hour
	// projectMilestoneDueScanLimit 单租户单轮扫描上限（防止单轮无界内存）。
	projectMilestoneDueScanLimit = 500

	projectMilestoneEventDueSoon = "due_soon"
	projectMilestoneEventOverdue = "overdue"

	projectMilestoneStatusPlanned = "planned"
)

// projectMilestoneNonTerminalStatuses 仍在推进中的项目状态（终态项目不再提醒）。
var projectMilestoneNonTerminalStatuses = []string{"planning", "active", "suspended"}

// projectMilestoneStakeholderRoles 里程碑到期通知的项目内角色（PM 协同 + 干系人）；
// 项目经理另由 projects.manager_id 权威解析（与审批链/测试口径一致）。
var projectMilestoneStakeholderRoles = []string{"pm", "stakeholder"}

// ProjectMilestoneDueScanner 里程碑到期/逾期扫描器（每租户一轮）。
type ProjectMilestoneDueScanner struct {
	client    *ent.Client
	logger    *zap.SugaredLogger
	now       func() time.Time
	dueWindow time.Duration
}

func NewProjectMilestoneDueScanner(client *ent.Client, logger *zap.SugaredLogger) *ProjectMilestoneDueScanner {
	return &ProjectMilestoneDueScanner{
		client:    client,
		logger:    logger,
		now:       time.Now,
		dueWindow: ProjectMilestoneDueDefaultWindow,
	}
}

// SetDueWindow 覆盖到期提醒窗口（<=0 时忽略；用于配置注入与测试）。
func (s *ProjectMilestoneDueScanner) SetDueWindow(window time.Duration) {
	if window > 0 {
		s.dueWindow = window
	}
}

// ScanDueMilestones 扫描租户内 `planned` 且「已逾期或窗口内即将到期」的里程碑，
// 为 PM 与干系人入箱 in_app 通知。返回本轮实际入箱的通知条数（去重跳过不计）。
func (s *ProjectMilestoneDueScanner) ScanDueMilestones(ctx context.Context, tenantID int) (int, error) {
	if tenantID <= 0 {
		return 0, fmt.Errorf("project milestone due scanner: invalid tenant ID")
	}
	// IP-P0-11 / §5.2：执行前显式收窄租户，错误 ctx 不得跨租户执行。
	if err := tenantctx.EnsureJobTenant(ctx, tenantID); err != nil {
		return 0, fmt.Errorf("project milestone due scanner: %w", err)
	}
	ctx = tenantctx.WithTenantID(ctx, tenantID)

	now := s.now()
	horizon := now.Add(s.dueWindow)
	milestones, err := s.client.ProjectMilestone.Query().
		Where(
			projectmilestone.TenantID(tenantID),
			projectmilestone.StatusEQ(projectMilestoneStatusPlanned),
			projectmilestone.DueDateLTE(horizon),
		).
		Order(ent.Asc(projectmilestone.FieldDueDate)).
		Limit(projectMilestoneDueScanLimit).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("project milestone due scanner: query due milestones: %w", err)
	}
	if len(milestones) == 0 {
		return 0, nil
	}

	projects := make(map[int]*ent.Project, 4)
	recipientCache := make(map[int][]int, 4)
	enqueued := 0
	for _, milestone := range milestones {
		p, ok := projects[milestone.ProjectID]
		if !ok {
			p, err = s.client.Project.Query().
				Where(project.IDEQ(milestone.ProjectID), project.TenantID(tenantID)).
				Only(ctx)
			if err != nil {
				if !ent.IsNotFound(err) {
					s.logger.Warnw("project milestone due scanner: load project failed",
						"milestone_id", milestone.ID, "project_id", milestone.ProjectID,
						"tenant_id", tenantID, "error", err)
				}
				projects[milestone.ProjectID] = nil
				continue
			}
			projects[milestone.ProjectID] = p
		}
		if p == nil || !projectMilestoneStatusActive(p.Status) {
			continue
		}

		eventKind := projectMilestoneEventDueSoon
		if milestone.DueDate.Before(now) {
			eventKind = projectMilestoneEventOverdue
		}
		recipients, ok := recipientCache[milestone.ProjectID]
		if !ok {
			recipients = s.resolveRecipients(ctx, tenantID, milestone.ProjectID, p.ManagerID, p.OwnerTeamID)
			recipientCache[milestone.ProjectID] = recipients
		}
		if len(recipients) == 0 {
			continue
		}

		content := projectMilestoneDueContent(p.Name, milestone, eventKind)
		occurrenceKey := fmt.Sprintf("%s:%d:%s:%s",
			ProjectMilestoneDueNotificationType, milestone.ID,
			milestone.DueDate.Format("20060102"), eventKind)
		for _, recipientID := range recipients {
			if s.enqueueMilestoneDueNotification(ctx, tenantID, milestone, recipientID, occurrenceKey, eventKind, content) {
				enqueued++
			}
		}
	}

	if enqueued > 0 {
		RecordJobAudit(ctx, s.client, s.logger, JobAuditEntry{
			TenantID:   tenantID,
			Component:  "project-milestone-due-scanner",
			Action:     "project.milestone_due_scan",
			Resource:   "project_milestone",
			StatusCode: 200,
			Detail:     map[string]any{"scanned": len(milestones), "enqueued": enqueued},
		})
	}
	return enqueued, nil
}

// projectMilestoneStatusActive 项目是否仍在推进（planning/active/suspended）。
func projectMilestoneStatusActive(status string) bool {
	for _, s := range projectMilestoneNonTerminalStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// resolveRecipients 解析收件人：项目经理（权威）+ 项目内 pm/stakeholder 活跃成员 + 归口团队负责人（PE-17
// 「归口参与通知候选」；团队软删/停用自动降级不计入）；
// 去重、仅返回正数 ID（是否为目标租户活跃用户由入箱校验 fail-closed 兜底）。
func (s *ProjectMilestoneDueScanner) resolveRecipients(ctx context.Context, tenantID, projectID, managerID, ownerTeamID int) []int {
	seen := make(map[int]bool, 4)
	recipients := make([]int, 0, 4)
	if managerID > 0 {
		seen[managerID] = true
		recipients = append(recipients, managerID)
	}
	if ownerTeamID > 0 {
		ownerTeam, err := s.client.Team.Query().
			Where(team.IDEQ(ownerTeamID), team.TenantIDEQ(tenantID)).
			Only(ctx)
		switch {
		case err != nil:
			if !ent.IsNotFound(err) {
				s.logger.Warnw("project milestone due scanner: load owner team failed",
					"team_id", ownerTeamID, "tenant_id", tenantID, "error", err)
			}
		case ownerTeam.ManagerID > 0 &&
			(ownerTeam.DeletedAt == nil || ownerTeam.DeletedAt.IsZero()) &&
			ownerTeam.Status == "active":
			if !seen[ownerTeam.ManagerID] {
				seen[ownerTeam.ManagerID] = true
				recipients = append(recipients, ownerTeam.ManagerID)
			}
		}
	}
	members, err := s.client.ProjectMember.Query().
		Where(
			projectmember.TenantID(tenantID),
			projectmember.ProjectID(projectID),
			projectmember.StatusEQ("active"),
			projectmember.ProjectRoleIn(projectMilestoneStakeholderRoles...),
		).
		All(ctx)
	if err != nil {
		s.logger.Warnw("project milestone due scanner: query stakeholders failed",
			"project_id", projectID, "tenant_id", tenantID, "error", err)
		return recipients
	}
	for _, member := range members {
		if member.UserID <= 0 || seen[member.UserID] {
			continue
		}
		seen[member.UserID] = true
		recipients = append(recipients, member.UserID)
	}
	return recipients
}

// enqueueMilestoneDueNotification 单收件人入箱；幂等键冲突视为「已投递」跳过（返回 false）。
func (s *ProjectMilestoneDueScanner) enqueueMilestoneDueNotification(
	ctx context.Context, tenantID int, milestone *ent.ProjectMilestone,
	recipientID int, occurrenceKey, eventKind, content string,
) bool {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		s.logger.Warnw("project milestone due scanner: begin tx failed",
			"milestone_id", milestone.ID, "error", err)
		return false
	}
	err = EnqueueResourceNotificationTx(ctx, tx, ResourceNotificationCommand{
		TenantID:         tenantID,
		ResourceType:     "project_milestone",
		ResourceID:       milestone.ID,
		RecipientID:      recipientID,
		NotificationType: ProjectMilestoneDueNotificationType,
		Channel:          "in_app",
		Content:          content,
		OccurrenceKey:    occurrenceKey,
	})
	if err != nil {
		_ = tx.Rollback()
		if ent.IsConstraintError(err) {
			// 幂等收敛：同一里程碑同一触发已入箱（重复扫描/多实例并发）。
			return false
		}
		s.logger.Warnw("project milestone due scanner: enqueue notification failed",
			"milestone_id", milestone.ID, "recipient_id", recipientID,
			"event", eventKind, "error", err)
		return false
	}
	if err := tx.Commit(); err != nil {
		s.logger.Warnw("project milestone due scanner: commit notification failed",
			"milestone_id", milestone.ID, "recipient_id", recipientID, "error", err)
		return false
	}
	return true
}

// projectMilestoneDueContent 通知正文（in_app 通知中心展示；含逾期/临期语义）。
func projectMilestoneDueContent(projectName string, milestone *ent.ProjectMilestone, eventKind string) string {
	due := milestone.DueDate.Format("2006-01-02")
	if eventKind == projectMilestoneEventOverdue {
		return fmt.Sprintf("项目「%s」的里程碑「%s」已逾期（目标日期 %s）", projectName, milestone.Name, due)
	}
	return fmt.Sprintf("项目「%s」的里程碑「%s」将于 %s 到期", projectName, milestone.Name, due)
}
