package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"itsm-backend/ent"
)

// 模板域哨兵错误（PE-11；handler 统一映射 HTTP 语义）。
var (
	ErrTemplateNotFound       = errors.New("project template not found")
	ErrTemplateNameExists     = errors.New("project template name already exists")
	ErrTemplateNotActive      = errors.New("project template is not active")
	ErrTemplateInUse          = errors.New("project template is applied by projects")
	ErrTemplateAlreadyApplied = errors.New("project template already applied")
)

// TemplateVersionConflictError 模板乐观锁冲突（409）。
type TemplateVersionConflictError struct {
	CurrentVersion int
}

func (e *TemplateVersionConflictError) Error() string {
	return fmt.Sprintf("project template version conflict (current=%d)", e.CurrentVersion)
}

// ListTemplates 模板列表（分页归一：默认 20、上限 100）。
func (s *Service) ListTemplates(ctx context.Context, f TemplateListFilter) ([]*ent.ProjectTemplate, int64, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return s.repo.ListTemplates(ctx, f)
}

// GetTemplate 模板详情（不存在 → ErrTemplateNotFound，404 语义）。
func (s *Service) GetTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectTemplate, error) {
	item, err := s.repo.GetTemplate(ctx, tenantID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return item, nil
}

// CreateTemplate 创建模板：name 租户内唯一；status 缺省 draft（编好再激活）；payload 强校验。
func (s *Service) CreateTemplate(ctx context.Context, tenantID, actorID int, req *TemplateCreateRequest) (*ent.ProjectTemplate, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: payload is required", ErrValidation)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 128 {
		return nil, fmt.Errorf("%w: name too long (max 128)", ErrValidation)
	}
	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > templateDescMaxRunes {
		return nil, fmt.Errorf("%w: description too long (max %d)", ErrValidation, templateDescMaxRunes)
	}
	templateType := strings.TrimSpace(req.Type)
	if templateType == "" {
		templateType = "internal"
	}
	if !validTypes[templateType] {
		return nil, fmt.Errorf("%w: invalid type %q", ErrValidation, req.Type)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = TemplateStatusDraft
	}
	if !validTemplateStatuses[status] {
		return nil, fmt.Errorf("%w: invalid status %q", ErrValidation, req.Status)
	}
	if err := req.Payload.Validate(); err != nil {
		return nil, err
	}
	exists, err := s.repo.TemplateNameExists(ctx, tenantID, name, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrTemplateNameExists
	}
	payload, err := templatePayloadToMap(req.Payload)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateTemplate(ctx, TemplateCreateParams{
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		Type:        templateType,
		Status:      status,
		Payload:     payload,
		CreatedBy:   actorID,
	})
}

// UpdateTemplate 更新模板（version 可选乐观锁；nil 字段不改；空更新 400）。
//
// 状态切换：PUT 显式传 status；应用侧仅接受 active（draft/archived 一律 409）。
func (s *Service) UpdateTemplate(ctx context.Context, tenantID, id int, req *TemplateUpdateRequest) (*ent.ProjectTemplate, error) {
	if req == nil {
		return nil, ErrNoUpdateFields
	}
	if req.Name == nil && req.Description == nil && req.Type == nil && req.Status == nil && req.Payload == nil {
		return nil, ErrNoUpdateFields
	}
	params := TemplateUpdateParams{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrValidation)
		}
		if utf8.RuneCountInString(name) > 128 {
			return nil, fmt.Errorf("%w: name too long (max 128)", ErrValidation)
		}
		exists, err := s.repo.TemplateNameExists(ctx, tenantID, name, id)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrTemplateNameExists
		}
		params.Name = &name
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(description) > templateDescMaxRunes {
			return nil, fmt.Errorf("%w: description too long (max %d)", ErrValidation, templateDescMaxRunes)
		}
		params.Description = &description
	}
	if req.Type != nil {
		templateType := strings.TrimSpace(*req.Type)
		if !validTypes[templateType] {
			return nil, fmt.Errorf("%w: invalid type %q", ErrValidation, *req.Type)
		}
		params.Type = &templateType
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validTemplateStatuses[status] {
			return nil, fmt.Errorf("%w: invalid status %q", ErrValidation, *req.Status)
		}
		params.Status = &status
	}
	if req.Payload != nil {
		if err := req.Payload.Validate(); err != nil {
			return nil, err
		}
		payload, err := templatePayloadToMap(req.Payload)
		if err != nil {
			return nil, err
		}
		params.Payload = payload
	}

	version := derefInt(req.Version)
	updated, err := s.repo.UpdateTemplateCAS(ctx, tenantID, id, params, version)
	if err != nil {
		if ent.IsNotFound(err) {
			current, getErr := s.repo.GetTemplate(ctx, tenantID, id)
			if getErr != nil {
				return nil, ErrTemplateNotFound
			}
			return nil, &TemplateVersionConflictError{CurrentVersion: current.Version}
		}
		return nil, err
	}
	return updated, nil
}

// DeleteTemplate 软删除（被项目应用时拒绝，引导改为归档）。
func (s *Service) DeleteTemplate(ctx context.Context, tenantID, id int) error {
	if _, err := s.GetTemplate(ctx, tenantID, id); err != nil {
		return err
	}
	inUse, err := s.repo.TemplateAppliedProjectCount(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return ErrTemplateInUse
	}
	return s.repo.SoftDeleteTemplate(ctx, tenantID, id)
}

// ApplyTemplate 一键生成项目计划（阶段/任务/检查项/里程碑）。
//
// 规则：
//   - 可见性谓词（非成员 404）；终态项目拒绝；
//   - 模板须 active（draft/archived 409）；
//   - 一项目一模板：已应用再次调用返回 409（幂等：不产生重复数据）；
//   - 计划日期锚点 = 项目计划开始日期（缺失时 = 应用当日），任务/里程碑按 offsetDays 换算自然日。
func (s *Service) ApplyTemplate(ctx context.Context, tenantID, projectID, viewerID int, admin bool, req *TemplateApplyRequest) (*TemplateApplyResponse, error) {
	if req == nil || req.TemplateID <= 0 {
		return nil, fmt.Errorf("%w: templateId is required", ErrValidation)
	}
	current, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: project is closed", ErrValidation)
	}
	if current.TemplateID > 0 {
		return nil, s.templateAppliedConflict(ctx, tenantID, current)
	}
	tpl, err := s.GetTemplate(ctx, tenantID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	if tpl.Status != TemplateStatusActive {
		return nil, fmt.Errorf("%w: template %q is %s", ErrTemplateNotActive, tpl.Name, tpl.Status)
	}
	payload, err := templatePayloadFromMap(tpl.Payload)
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}

	anchor := dateOnlyUTC(s.now())
	if !current.StartDate.IsZero() {
		anchor = dateOnlyUTC(current.StartDate)
	}
	stages, milestones := buildTemplatePlans(payload, anchor)
	if len(stages) == 0 && len(milestones) == 0 {
		return nil, fmt.Errorf("%w: payload requires at least one task or milestone", ErrValidation)
	}
	appliedAt := s.now()
	outcome, err := s.repo.ApplyTemplateTx(ctx, TemplateApplyParams{
		TenantID:   tenantID,
		ProjectID:  projectID,
		TemplateID: tpl.ID,
		AppliedBy:  viewerID,
		AppliedAt:  appliedAt,
		Stages:     stages,
		Milestones: milestones,
	})
	if err != nil {
		if errors.Is(err, ErrTemplateAlreadyApplied) {
			refreshed, getErr := s.repo.Get(ctx, tenantID, projectID)
			if getErr != nil {
				return nil, &TemplateAlreadyAppliedError{TemplateID: tpl.ID, TemplateName: tpl.Name, AppliedAt: appliedAt}
			}
			return nil, s.templateAppliedConflict(ctx, tenantID, refreshed)
		}
		return nil, err
	}
	return &TemplateApplyResponse{
		ProjectID:      projectID,
		TemplateID:     tpl.ID,
		TemplateName:   tpl.Name,
		AppliedAt:      appliedAt,
		AnchorDate:     anchor,
		StageCount:     outcome.StageCount,
		TaskCount:      outcome.TaskCount,
		ChecklistCount: outcome.ChecklistCount,
		MilestoneCount: outcome.MilestoneCount,
	}, nil
}

// templateAppliedConflict 构造"已应用"冲突（含模板名快照；模板已删时仅给 ID）。
func (s *Service) templateAppliedConflict(ctx context.Context, tenantID int, p *ent.Project) error {
	out := &TemplateAlreadyAppliedError{TemplateID: p.TemplateID, AppliedAt: p.TemplateAppliedAt}
	if tpl, err := s.repo.GetTemplate(ctx, tenantID, p.TemplateID); err == nil {
		out.TemplateName = tpl.Name
	}
	return out
}

// buildTemplatePlans payload → 生成计划（阶段分组按任务出现顺序；日期按锚点换算）。
func buildTemplatePlans(p *TemplatePayload, anchor time.Time) ([]TemplateStagePlan, []TemplateMilestonePlan) {
	stageIndex := map[string]int{}
	stages := make([]TemplateStagePlan, 0)
	addStage := func(name string) *TemplateStagePlan {
		if idx, ok := stageIndex[name]; ok {
			return &stages[idx]
		}
		stages = append(stages, TemplateStagePlan{Name: name})
		stageIndex[name] = len(stages) - 1
		return &stages[len(stages)-1]
	}

	for _, t := range p.Tasks {
		plan := TemplateTaskPlan{
			Name:          strings.TrimSpace(t.Name),
			Description:   strings.TrimSpace(t.Description),
			Priority:      t.PriorityOrDefault(),
			EstimateHours: t.EstimateHours,
			Checklist:     t.ChecklistNormalized(),
		}
		if t.StartOffsetDays != nil || t.DurationDays != nil {
			start := anchor
			if t.StartOffsetDays != nil {
				start = anchor.AddDate(0, 0, *t.StartOffsetDays)
			}
			plan.PlanStart = &start
			if t.DurationDays != nil {
				end := start.AddDate(0, 0, *t.DurationDays)
				plan.PlanEnd = &end
			}
		}
		stage := addStage(t.StageOrDefault())
		stage.Tasks = append(stage.Tasks, plan)
	}

	milestones := make([]TemplateMilestonePlan, 0, len(p.Milestones))
	for _, m := range p.Milestones {
		milestones = append(milestones, TemplateMilestonePlan{
			Name:    strings.TrimSpace(m.Name),
			DueDate: anchor.AddDate(0, 0, m.OffsetDays),
		})
	}
	return stages, milestones
}

// dateOnlyUTC 截断到 UTC 自然日（计划日期与 SLA/基线口径一致，避免时刻噪声）。
func dateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
