package project

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"itsm-backend/ent"
)

// 模板域（PE-11）：实体/DTO/校验。
//
// 权威定义：docs/project-management/plan/p1-template-design.md。
// 定位：模板 = 可复用交付方法论（任务/检查单/里程碑），一键生成到已有项目；
// 应用幂等由 `projects.template_id` CAS 认领 + 单事务生成保证（一项目一模板）。

// 模板状态词表（draft 可编不可用；active 可应用；archived 只读归档）。
const (
	TemplateStatusDraft    = "draft"
	TemplateStatusActive   = "active"
	TemplateStatusArchived = "archived"
)

var validTemplateStatuses = map[string]bool{
	TemplateStatusDraft:    true,
	TemplateStatusActive:   true,
	TemplateStatusArchived: true,
}

// 模板 payload 限额（防滥用；字段过长/条目过多直接 400）。
const (
	templateMilestoneMax      = 50
	templateTaskMax           = 200
	templateChecklistMax      = 20
	templateNameMaxRunes      = 200
	templateDescMaxRunes      = 2000
	templateStageNameMaxRunes = 100
	templateOffsetMaxDays     = 3650
	templateDurationMaxDays   = 3650
	templatePayloadMaxBytes   = 64 * 1024
)

// TemplateMilestoneSpec 模板里程碑定义（offsetDays 为相对锚点的自然日偏移）。
type TemplateMilestoneSpec struct {
	Name       string `json:"name"`
	OffsetDays int    `json:"offsetDays"`
}

// TemplateTaskSpec 模板任务定义。
//
// stage 相同（非空）的任务归入同一阶段（L1 分组任务）；无 stage 的任务直接挂项目根。
// 检查项生成为任务的三级子项（WBS ≤3 层：阶段 L1 → 任务 L2 → 检查项 L3）。
type TemplateTaskSpec struct {
	Stage           string   `json:"stage"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Priority        string   `json:"priority"`
	StartOffsetDays *int     `json:"startOffsetDays"`
	DurationDays    *int     `json:"durationDays"`
	EstimateHours   *float64 `json:"estimateHours"`
	Checklist       []string `json:"checklist"`
}

// TemplatePayload 模板定义根（jsonb 列结构）。
type TemplatePayload struct {
	Milestones []TemplateMilestoneSpec `json:"milestones"`
	Tasks      []TemplateTaskSpec      `json:"tasks"`
}

// Validate 校验模板定义（返回 ErrValidation 包装；nil 安全）。
func (p *TemplatePayload) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: payload is required", ErrValidation)
	}
	if len(p.Milestones) == 0 && len(p.Tasks) == 0 {
		return fmt.Errorf("%w: payload requires at least one task or milestone", ErrValidation)
	}
	if len(p.Milestones) > templateMilestoneMax {
		return fmt.Errorf("%w: too many milestones (max %d)", ErrValidation, templateMilestoneMax)
	}
	if len(p.Tasks) > templateTaskMax {
		return fmt.Errorf("%w: too many tasks (max %d)", ErrValidation, templateTaskMax)
	}
	for i, m := range p.Milestones {
		if err := validateSpecName(m.Name, "milestone", i); err != nil {
			return err
		}
		if m.OffsetDays < 0 || m.OffsetDays > templateOffsetMaxDays {
			return fmt.Errorf("%w: milestone[%d] offsetDays must be within 0..%d", ErrValidation, i, templateOffsetMaxDays)
		}
	}
	for i, t := range p.Tasks {
		if err := validateSpecName(t.Name, "task", i); err != nil {
			return err
		}
		if utf8.RuneCountInString(strings.TrimSpace(t.Stage)) > templateStageNameMaxRunes {
			return fmt.Errorf("%w: task[%d] stage too long (max %d)", ErrValidation, i, templateStageNameMaxRunes)
		}
		if utf8.RuneCountInString(t.Description) > templateDescMaxRunes {
			return fmt.Errorf("%w: task[%d] description too long (max %d)", ErrValidation, i, templateDescMaxRunes)
		}
		if t.Priority != "" && !validTaskPriorities[t.Priority] {
			return fmt.Errorf("%w: task[%d] invalid priority %q", ErrValidation, i, t.Priority)
		}
		if t.StartOffsetDays != nil && (*t.StartOffsetDays < 0 || *t.StartOffsetDays > templateOffsetMaxDays) {
			return fmt.Errorf("%w: task[%d] startOffsetDays must be within 0..%d", ErrValidation, i, templateOffsetMaxDays)
		}
		if t.DurationDays != nil && (*t.DurationDays < 0 || *t.DurationDays > templateDurationMaxDays) {
			return fmt.Errorf("%w: task[%d] durationDays must be within 0..%d", ErrValidation, i, templateDurationMaxDays)
		}
		if t.EstimateHours != nil && (*t.EstimateHours < 0 || *t.EstimateHours > 100000) {
			return fmt.Errorf("%w: task[%d] estimateHours out of range", ErrValidation, i)
		}
		if len(t.Checklist) > templateChecklistMax {
			return fmt.Errorf("%w: task[%d] too many checklist items (max %d)", ErrValidation, i, templateChecklistMax)
		}
		for j, item := range t.Checklist {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("%w: task[%d] checklist[%d] is empty", ErrValidation, i, j)
			}
			if utf8.RuneCountInString(strings.TrimSpace(item)) > templateNameMaxRunes {
				return fmt.Errorf("%w: task[%d] checklist[%d] too long (max %d)", ErrValidation, i, j, templateNameMaxRunes)
			}
		}
	}
	// JSON 体积上限（编码后校验，避免超大 payload 落库）。
	if raw, err := json.Marshal(p); err == nil && len(raw) > templatePayloadMaxBytes {
		return fmt.Errorf("%w: payload too large (max %d bytes)", ErrValidation, templatePayloadMaxBytes)
	}
	return nil
}

func validateSpecName(name, kind string, idx int) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: %s[%d] name is required", ErrValidation, kind, idx)
	}
	if utf8.RuneCountInString(trimmed) > templateNameMaxRunes {
		return fmt.Errorf("%w: %s[%d] name too long (max %d)", ErrValidation, kind, idx, templateNameMaxRunes)
	}
	return nil
}

// Priority 归一（空 = medium，与任务服务默认一致）。
func (t TemplateTaskSpec) PriorityOrDefault() string {
	if p := strings.TrimSpace(t.Priority); p != "" {
		return p
	}
	return "medium"
}

// Stage 归一（去空格；空 = 未分组）。
func (t TemplateTaskSpec) StageOrDefault() string {
	return strings.TrimSpace(t.Stage)
}

// ChecklistNormalized 检查项归一（去空格、丢弃空项）。
func (t TemplateTaskSpec) ChecklistNormalized() []string {
	out := make([]string, 0, len(t.Checklist))
	for _, item := range t.Checklist {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// templatePayloadToMap 模板定义 → JSONB map。
func templatePayloadToMap(p *TemplatePayload) (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// templatePayloadFromMap JSONB map → 模板定义（解码失败返回 ErrValidation 包装）。
func templatePayloadFromMap(m map[string]any) (*TemplatePayload, error) {
	if len(m) == 0 {
		return &TemplatePayload{}, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("%w: encode payload: %v", ErrValidation, err)
	}
	var out TemplatePayload
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: invalid payload: %v", ErrValidation, err)
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// 请求 DTO（PE-11）
// ---------------------------------------------------------------------------

// TemplateCreateRequest 创建模板（body）。
type TemplateCreateRequest struct {
	TenantID    int              `json:"-"`
	Name        string           `json:"name" binding:"required"`
	Description string           `json:"description"`
	Type        string           `json:"type"`
	Status      string           `json:"status"`
	Payload     *TemplatePayload `json:"payload" binding:"required"`
}

// TemplateUpdateRequest 更新模板（nil = 不修改；status 走本接口显式切换）。
type TemplateUpdateRequest struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Type        *string          `json:"type"`
	Status      *string          `json:"status"`
	Payload     *TemplatePayload `json:"payload"`
	Version     *int             `json:"version"`
}

// TemplateApplyRequest 一键应用（body）。
type TemplateApplyRequest struct {
	TemplateID int `json:"templateId" binding:"required"`
}

// ---------------------------------------------------------------------------
// 响应 DTO（PE-11；类型名 ProjectTemplateResponse 供领域别名 projectTemplates）
// ---------------------------------------------------------------------------

// ProjectTemplateResponse 模板响应（withPayload=false 时省略定义体，仅给计数）。
type ProjectTemplateResponse struct {
	ID             int              `json:"id"`
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	Type           string           `json:"type"`
	Status         string           `json:"status"`
	Payload        *TemplatePayload `json:"payload,omitempty"`
	StageCount     int              `json:"stageCount"`
	TaskCount      int              `json:"taskCount"`
	ChecklistCount int              `json:"checklistCount"`
	MilestoneCount int              `json:"milestoneCount"`
	Version        int              `json:"version"`
	CreatedBy      *int             `json:"createdBy,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// TemplateApplyResponse 一键生成结果（计数 + 锚点）。
type TemplateApplyResponse struct {
	ProjectID      int       `json:"projectId"`
	TemplateID     int       `json:"templateId"`
	TemplateName   string    `json:"templateName"`
	AppliedAt      time.Time `json:"appliedAt"`
	AnchorDate     time.Time `json:"anchorDate"`
	StageCount     int       `json:"stages"`
	TaskCount      int       `json:"tasks"`
	ChecklistCount int       `json:"checklists"`
	MilestoneCount int       `json:"milestones"`
}

// TemplateAlreadyAppliedError 项目已有模板（409；幂等：重复应用不产生重复数据）。
type TemplateAlreadyAppliedError struct {
	TemplateID   int
	TemplateName string
	AppliedAt    time.Time
}

func (e *TemplateAlreadyAppliedError) Error() string {
	return fmt.Sprintf("project already applied template %d", e.TemplateID)
}

// ToTemplateResponse ent → 响应。
func ToTemplateResponse(t *ent.ProjectTemplate, withPayload bool) *ProjectTemplateResponse {
	if t == nil {
		return nil
	}
	resp := &ProjectTemplateResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		Status:      t.Status,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if t.CreatedBy > 0 {
		id := t.CreatedBy
		resp.CreatedBy = &id
	}
	payload, err := templatePayloadFromMap(t.Payload)
	if err != nil {
		// 存量脏数据：响应给空定义体，避免整个列表 500（fail-open 仅限读展示）。
		payload = &TemplatePayload{}
	}
	resp.StageCount, resp.TaskCount, resp.ChecklistCount, resp.MilestoneCount = templatePayloadCounts(payload)
	if withPayload {
		resp.Payload = payload
	}
	return resp
}

// ToTemplateResponseList 批量映射（列表省略定义体）。
func ToTemplateResponseList(items []*ent.ProjectTemplate) []*ProjectTemplateResponse {
	out := make([]*ProjectTemplateResponse, 0, len(items))
	for _, it := range items {
		out = append(out, ToTemplateResponse(it, false))
	}
	return out
}

// templatePayloadCounts 计数（阶段 = 去重后的非空 stage 数）。
func templatePayloadCounts(p *TemplatePayload) (stages, tasks, checklists, milestones int) {
	if p == nil {
		return 0, 0, 0, 0
	}
	seen := map[string]bool{}
	for _, t := range p.Tasks {
		if s := t.StageOrDefault(); s != "" {
			seen[s] = true
		}
		checklists += len(t.ChecklistNormalized())
	}
	return len(seen), len(p.Tasks), checklists, len(p.Milestones)
}
