package project

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"itsm-backend/ent"
)

// PE-17 归口与报告业务服务。权威定义：docs/project-management/plan/p1-report-design.md。

// 报告域哨兵错误。
var (
	ErrReportTemplateNotFound = errors.New("project report template not found")
	ErrReportTemplateExists   = errors.New("project report template name already exists")
	ErrReportTemplateArchived = errors.New("project report template is not active")
)

// ErrOwnerTeamUnavailable 归口团队不可用（不存在/跨租户/已软删/停用）。
var ErrOwnerTeamUnavailable = errors.New("owner team is not available")

// ReportTemplateCASError 报告模板乐观锁冲突（409）。
type ReportTemplateCASError struct {
	CurrentVersion int
}

func (e *ReportTemplateCASError) Error() string {
	return fmt.Sprintf("report template version conflict (current=%d)", e.CurrentVersion)
}

// ---------------------------------------------------------------------------
// 归口团队（PE-17）
// ---------------------------------------------------------------------------

// ensureOwnerTeam 写入校验：归口团队必须同租户、未软删且 active（fail-closed）。
func (s *Service) ensureOwnerTeam(ctx context.Context, tenantID, teamID int) error {
	if teamID <= 0 {
		return nil
	}
	ref, err := s.repo.GetTeamRef(ctx, tenantID, teamID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrOwnerTeamUnavailable
		}
		return err
	}
	if ref.Deleted || ref.Status != "active" {
		return ErrOwnerTeamUnavailable
	}
	return nil
}

// OwnerTeamRefs 批量取归口团队引用（读取侧富化；失败降级为空 map，不阻塞读）。
func (s *Service) OwnerTeamRefs(ctx context.Context, tenantID int, projects []*ent.Project) map[int]TeamRef {
	ids := make([]int, 0, len(projects))
	seen := make(map[int]bool, len(projects))
	for _, p := range projects {
		if p == nil || p.OwnerTeamID <= 0 || seen[p.OwnerTeamID] {
			continue
		}
		seen[p.OwnerTeamID] = true
		ids = append(ids, p.OwnerTeamID)
	}
	if len(ids) == 0 {
		return map[int]TeamRef{}
	}
	refs, err := s.repo.ListTeamRefs(ctx, tenantID, ids)
	if err != nil {
		s.logger.Warnw("load owner team refs failed", "tenant_id", tenantID, "error", err)
		return map[int]TeamRef{}
	}
	return refs
}

// ---------------------------------------------------------------------------
// 报告模板 CRUD
// ---------------------------------------------------------------------------

// ListReportTemplates 报告模板列表（分页收敛 ≤100；默认 20）。
func (s *Service) ListReportTemplates(ctx context.Context, f ReportTemplateListFilter) ([]*ent.ProjectReportTemplate, int64, error) {
	if f.Status != "" && !validReportTemplateStatuses[f.Status] {
		return nil, 0, fmt.Errorf("%w: invalid report template status %q", ErrValidation, f.Status)
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	return s.repo.ListReportTemplates(ctx, f)
}

// GetReportTemplate 报告模板详情（不存在 → ErrReportTemplateNotFound）。
func (s *Service) GetReportTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectReportTemplate, error) {
	tpl, err := s.repo.GetReportTemplate(ctx, tenantID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrReportTemplateNotFound
		}
		return nil, err
	}
	return tpl, nil
}

// CreateReportTemplate 创建报告模板（名称租户内唯一；cadence/sections/status 受控词表）。
func (s *Service) CreateReportTemplate(ctx context.Context, tenantID, actorID int, req *ReportTemplateCreateRequest) (*ent.ProjectReportTemplate, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: report template name is required", ErrValidation)
	}
	cadence, err := normalizeReportCadence(req.Cadence)
	if err != nil {
		return nil, err
	}
	status, err := normalizeReportTemplateStatus(req.Status, ReportTemplateStatusDraft)
	if err != nil {
		return nil, err
	}
	sections := normalizeReportSections(req.Sections)
	exists, err := s.repo.ReportTemplateNameExists(ctx, tenantID, name, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrReportTemplateExists
	}
	created, err := s.repo.CreateReportTemplate(ctx, ReportTemplateCreateParams{
		TenantID:    tenantID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Cadence:     cadence,
		Sections:    sections,
		Status:      status,
		CreatedBy:   actorID,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrReportTemplateExists
		}
		return nil, err
	}
	return created, nil
}

// UpdateReportTemplate 更新报告模板（version 乐观锁；name 唯一性重校验）。
func (s *Service) UpdateReportTemplate(ctx context.Context, tenantID, id int, req *ReportTemplateUpdateRequest) (*ent.ProjectReportTemplate, error) {
	current, err := s.repo.GetReportTemplate(ctx, tenantID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrReportTemplateNotFound
		}
		return nil, err
	}
	params := ReportTemplateUpdateParams{}
	changed := false
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: report template name is required", ErrValidation)
		}
		if name != current.Name {
			exists, err := s.repo.ReportTemplateNameExists(ctx, tenantID, name, id)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, ErrReportTemplateExists
			}
		}
		params.Name = &name
		changed = true
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		params.Description = &description
		changed = true
	}
	if req.Cadence != nil {
		cadence, err := normalizeReportCadence(*req.Cadence)
		if err != nil {
			return nil, err
		}
		params.Cadence = &cadence
		changed = true
	}
	if req.Sections != nil {
		sections := normalizeReportSections(*req.Sections)
		params.Sections = &sections
		changed = true
	}
	if req.Status != nil {
		status, err := normalizeReportTemplateStatus(*req.Status, "")
		if err != nil {
			return nil, err
		}
		params.Status = &status
		changed = true
	}
	if !changed {
		return nil, ErrNoUpdateFields
	}
	version := derefInt(req.Version)
	updated, err := s.repo.UpdateReportTemplateCAS(ctx, tenantID, id, params, version)
	if err != nil {
		if ent.IsNotFound(err) {
			latest, getErr := s.repo.GetReportTemplate(ctx, tenantID, id)
			if getErr != nil {
				return nil, ErrReportTemplateNotFound
			}
			return nil, &ReportTemplateCASError{CurrentVersion: latest.Version}
		}
		if ent.IsConstraintError(err) {
			return nil, ErrReportTemplateExists
		}
		return nil, err
	}
	return updated, nil
}

// DeleteReportTemplate 软删除报告模板。
func (s *Service) DeleteReportTemplate(ctx context.Context, tenantID, id int) error {
	if err := s.repo.SoftDeleteReportTemplate(ctx, tenantID, id); err != nil {
		if ent.IsNotFound(err) {
			return ErrReportTemplateNotFound
		}
		return err
	}
	return nil
}

// validReportTemplateStatuses 模板状态词表。
var validReportTemplateStatuses = map[string]bool{
	ReportTemplateStatusDraft:    true,
	ReportTemplateStatusActive:   true,
	ReportTemplateStatusArchived: true,
}

func normalizeReportCadence(cadence string) (string, error) {
	value := strings.TrimSpace(cadence)
	if value == "" {
		return ReportCadenceWeekly, nil
	}
	if !validReportCadences[value] {
		return "", fmt.Errorf("%w: invalid report cadence %q", ErrValidation, cadence)
	}
	return value, nil
}

func normalizeReportTemplateStatus(status, fallback string) (string, error) {
	value := strings.TrimSpace(status)
	if value == "" {
		return fallback, nil
	}
	if !validReportTemplateStatuses[value] {
		return "", fmt.Errorf("%w: invalid report template status %q", ErrValidation, status)
	}
	return value, nil
}

// ---------------------------------------------------------------------------
// 报告生成（数据派生；先导出后 AI）
// ---------------------------------------------------------------------------

// GenerateProjectReport 生成项目状态报告：事实数据实时派生 markdown + data，
// 不落报告快照（模板/周期数据驱动；templateId>0 时必须为 active 模板）。
func (s *Service) GenerateProjectReport(ctx context.Context, tenantID, projectID, viewerID int, admin bool, templateID int) (*ReportResponse, error) {
	p, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	sections := defaultReportSections()
	var templateRef *ReportTemplateRef
	if templateID > 0 {
		tpl, err := s.repo.GetReportTemplate(ctx, tenantID, templateID)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, ErrReportTemplateNotFound
			}
			return nil, err
		}
		if tpl.Status != ReportTemplateStatusActive {
			return nil, ErrReportTemplateArchived
		}
		if selected := normalizeReportSections(tpl.Sections); len(selected) > 0 {
			sections = selected
		}
		templateRef = &ReportTemplateRef{ID: tpl.ID, Name: tpl.Name, Cadence: tpl.Cadence}
	}

	now := s.now()
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

	// 偏差：无基线（ErrBaselineNotFound）跳过；其他错误不吞。
	var variance *ReportVarianceSummary
	diff, diffErr := s.BaselineDiff(ctx, tenantID, projectID, viewerID, admin)
	switch {
	case diffErr == nil && diff != nil:
		createdAt := diff.CreatedAt
		variance = &ReportVarianceSummary{
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

	data := ReportData{
		ProjectID:      p.ID,
		ProjectName:    p.Name,
		ProjectCode:    p.Code,
		Status:         normalizeStatus(p.Status),
		Health:         p.Health,
		Progress:       p.Progress,
		GeneratedAt:    now,
		Template:       templateRef,
		Sections:       sections,
		TaskStats:      buildReportTaskStats(tasks, now),
		MilestoneStats: buildReportMilestoneStats(milestones, now),
		Gates:          buildReportGates(gates),
		Variance:       variance,
	}
	if p.ManagerID > 0 {
		id := p.ManagerID
		data.ManagerID = &id
	}
	if ref, ok := s.OwnerTeamRefs(ctx, tenantID, []*ent.Project{p})[p.OwnerTeamID]; ok && p.OwnerTeamID > 0 {
		data.OwnerTeam = &ReportOwnerTeam{ID: ref.ID, Name: ref.Name, Deleted: ref.Deleted}
	} else if p.OwnerTeamID > 0 {
		// 全局软删读拦截器对软删团队透明过滤：名称不可回显 → 降级标记（UI 回退显示团队 #ID）。
		data.OwnerTeam = &ReportOwnerTeam{ID: p.OwnerTeamID, Name: "已删除团队", Deleted: true}
	}

	return &ReportResponse{Markdown: buildReportMarkdown(&data), Data: data}, nil
}

func buildReportTaskStats(tasks []*ent.ProjectTask, now time.Time) ReportTaskStats {
	stats := ReportTaskStats{Total: len(tasks)}
	dueSoonBefore := now.Add(72 * time.Hour)
	for _, task := range tasks {
		switch task.Status {
		case "done":
			stats.Done++
		case "cancelled":
			stats.Cancelled++
		}
		if !taskIsOpen(task.Status) {
			continue
		}
		stats.Open++
		if task.PlanEnd.IsZero() {
			continue
		}
		if task.PlanEnd.Before(now) {
			stats.Overdue++
		} else if task.PlanEnd.Before(dueSoonBefore) {
			stats.DueSoon++
		}
	}
	return stats
}

func buildReportMilestoneStats(milestones []*ent.ProjectMilestone, now time.Time) ReportMilestoneStats {
	stats := ReportMilestoneStats{Total: len(milestones)}
	for _, milestone := range milestones {
		switch milestone.Status {
		case "reached":
			stats.Reached++
			if milestone.ReachedAt != nil && !milestone.ReachedAt.After(milestone.DueDate) {
				stats.OnTime++
			}
		case "missed":
			stats.Missed++
		default:
			stats.Planned++
			if milestone.DueDate.Before(now) {
				stats.Overdue++
			}
		}
	}
	if stats.Total > 0 {
		stats.Rate = float64(stats.Reached) / float64(stats.Total)
		stats.OverdueRate = float64(stats.Overdue) / float64(stats.Total)
	}
	return stats
}

func buildReportGates(gates []*ent.ProjectGate) []ReportGateItem {
	out := make([]ReportGateItem, 0, len(gates))
	for _, gate := range gates {
		item := ReportGateItem{Gate: gate.Gate, Status: gate.Status}
		if !gate.UpdatedAt.IsZero() {
			updated := gate.UpdatedAt
			item.UpdatedAt = &updated
		}
		out = append(out, item)
	}
	return out
}

// ---------------------------------------------------------------------------
// 报表分组（按归口团队；PE-17）
// ---------------------------------------------------------------------------

// ReportGroupsByOwnerTeam 报表分组：按归口团队聚合项目数与健康度分布
// （可见性谓词与列表一致：管理角色直通；否则仅其为活跃成员的项目）。
func (s *Service) ReportGroupsByOwnerTeam(ctx context.Context, tenantID, viewerID int, admin bool) ([]ReportGroupResponse, error) {
	var projectIDs []int
	if !admin {
		ids, err := s.repo.MemberProjectIDs(ctx, tenantID, viewerID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []ReportGroupResponse{}, nil
		}
		projectIDs = ids
	}
	aggs, err := s.repo.ReportGroupsByOwnerTeam(ctx, tenantID, projectIDs)
	if err != nil {
		return nil, err
	}
	if len(aggs) == 0 {
		return []ReportGroupResponse{}, nil
	}
	teamIDs := make([]int, 0, len(aggs))
	for _, agg := range aggs {
		if agg.OwnerTeamID > 0 {
			teamIDs = append(teamIDs, agg.OwnerTeamID)
		}
	}
	refs := s.OwnerTeamRefs(ctx, tenantID, projectRefsFromTeamIDs(teamIDs))
	out := make([]ReportGroupResponse, 0, len(aggs))
	for _, agg := range aggs {
		row := ReportGroupResponse{
			ProjectCount: agg.ProjectCount,
			GreenCount:   agg.GreenCount,
			YellowCount:  agg.YellowCount,
			RedCount:     agg.RedCount,
		}
		if agg.OwnerTeamID > 0 {
			id := agg.OwnerTeamID
			row.OwnerTeamID = &id
			if ref, ok := refs[agg.OwnerTeamID]; ok {
				row.OwnerTeamName = ref.Name
				row.OwnerTeamDeleted = ref.Deleted
			} else {
				row.OwnerTeamDeleted = true
			}
		} else {
			row.OwnerTeamName = "未归口"
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return reportGroupKey(out[i]) < reportGroupKey(out[j])
	})
	return out, nil
}

func reportGroupKey(row ReportGroupResponse) int {
	if row.OwnerTeamID == nil {
		return 0
	}
	return *row.OwnerTeamID
}

// projectRefsFromTeamIDs 构造仅有归口 ID 的轻量项目引用（复用 OwnerTeamRefs 批量取团队）。
func projectRefsFromTeamIDs(teamIDs []int) []*ent.Project {
	refs := make([]*ent.Project, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		refs = append(refs, &ent.Project{OwnerTeamID: teamID})
	}
	return refs
}
