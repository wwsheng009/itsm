package bpmn

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/incident"
	"itsm-backend/handlers/common/projectlink"

	"go.uber.org/zap"
)

// IncidentServiceTaskHandler 事件服务任务处理器
type IncidentServiceTaskHandler struct {
	HandlerBase
	client      *ent.Client
	logger      *zap.SugaredLogger
	projectLink projectlink.Validator
}

// NewIncidentServiceTaskHandler 创建事件处理器
func NewIncidentServiceTaskHandler(client *ent.Client, logger *zap.SugaredLogger) *IncidentServiceTaskHandler {
	return &IncidentServiceTaskHandler{
		client: client,
		logger: logger,
	}
}

// SetProjectLink 注入项目挂接校验端口（bootstrap 装配）。
// 未装配时携带 projectId 的 create_incident fail-closed（503），与 HTTP 五域口径一致。
func (h *IncidentServiceTaskHandler) SetProjectLink(v projectlink.Validator) {
	h.projectLink = v
}

// GetTaskType 返回任务类型
func (h *IncidentServiceTaskHandler) GetTaskType() string {
	return "incident_task"
}

// GetHandlerID 返回处理器标识
func (h *IncidentServiceTaskHandler) GetHandlerID() string {
	return "incident_service_handler"
}

// Execute 执行事件服务任务
func (h *IncidentServiceTaskHandler) Execute(ctx context.Context, task *ent.ProcessTask, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	action, _ := variables["action"].(string)
	switch action {
	case "create_incident":
		return h.createIncident(ctx, variables)
	case "assign_incident":
		return h.assignIncident(ctx, variables)
	case "escalate_incident":
		return h.escalateIncident(ctx, variables)
	case "resolve_incident":
		return h.resolveIncident(ctx, variables)
	case "close_incident":
		return h.closeIncident(ctx, variables)
	case "update_incident":
		return h.updateIncident(ctx, variables)
	case "acknowledge_incident":
		return h.acknowledgeIncident(ctx, variables)
	case "categorize_incident":
		return h.categorizeIncident(ctx, variables)
	default:
		return &dto.ServiceTaskResult{Success: true, Message: "无操作执行"}, nil
	}
}

// Validate 验证配置
func (h *IncidentServiceTaskHandler) Validate(ctx context.Context, config map[string]interface{}) error {
	return nil
}

// getIncidentByIDWithTenant 按 ID + TenantID 查询 Incident，找不到或不属于该租户均返回错误。
// 这是所有操作的前置守卫，防止 IDOR。
func (h *IncidentServiceTaskHandler) getIncidentByIDWithTenant(ctx context.Context, incidentID, tenantID int) (*ent.Incident, error) {
	if incidentID <= 0 {
		return nil, fmt.Errorf("无效的事件ID: %d", incidentID)
	}
	i, err := h.client.Incident.Query().
		Where(
			incident.ID(incidentID),
			incident.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("事件不存在或不属于当前租户: %d (tenant=%d)", incidentID, tenantID)
	}
	return i, nil
}

// createIncident 创建事件
func (h *IncidentServiceTaskHandler) createIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	title, _ := variables["title"].(string)
	description, _ := variables["description"].(string)
	incidentType, _ := variables["type"].(string)
	priority, _ := variables["priority"].(string)
	severity, _ := variables["severity"].(string)
	// schema 默认值兜底：显式 Set 空串会覆盖 Default 并触发枚举校验失败。
	if incidentType == "" {
		incidentType = "incident"
	}
	if priority == "" {
		priority = "medium"
	}
	if severity == "" {
		severity = "medium"
	}
	incidentNumber := GetStringFromVars(variables, "incident_number")
	if incidentNumber == "" {
		incidentNumber = nextBPMNIncidentNumber()
	}

	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		h.logger.Errorw("BPMN createIncident 缺少租户上下文", "error", err)
		return nil, fmt.Errorf("创建事件失败: %w", err)
	}

	if title == "" {
		return nil, fmt.Errorf("事件标题不能为空")
	}

	// PE-5 遗留项：创建时挂接项目——前置校验与 HTTP 五域同一端口（未装配 → fail-closed）。
	projectID, err := GuardProjectAttach(ctx, h.client, h.projectLink, tenantID, variables)
	if err != nil {
		return nil, errProjectAttach(err)
	}

	incident, err := h.client.Incident.Create().
		SetTitle(title).
		SetDescription(description).
		SetType(incidentType).
		SetPriority(priority).
		SetSeverity(severity).
		SetStatus(common.IncidentStatusNew).
		SetIncidentNumber(incidentNumber).
		SetReporterID(GetIntFromVars(variables, "reporter_id")).
		SetTenantID(tenantID).
		SetNillableProjectID(projectID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建事件失败: %w", err)
	}

	h.logger.Infow("Incident created via BPMN", "incident_id", incident.ID, "title", title, "tenant_id", tenantID, "project_id", incident.ProjectID)

	return &dto.ServiceTaskResult{
		Success:    true,
		Message:    fmt.Sprintf("事件 %d 已创建", incident.ID),
		OutputVars: map[string]interface{}{"incident_id": incident.ID, "incident_number": incident.IncidentNumber, "tenant_id": tenantID},
	}, nil
}

// nextBPMNIncidentNumber 生成流程侧事件编号兜底值（crypto/rand 后缀，碰撞概率可忽略；
// incident_number 为全局唯一约束，真碰撞时 INSERT 会失败并由调用方重试）。
// 流程变量显式携带 incident_number 时以显式值为准。
func nextBPMNIncidentNumber() string {
	suffix := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	if n, err := rand.Int(rand.Reader, big.NewInt(1000000)); err == nil {
		suffix = fmt.Sprintf("%06d", n.Int64())
	}
	return fmt.Sprintf("INC-%04d%02d-%s", time.Now().Year(), int(time.Now().Month()), suffix)
}

// assignIncident 分配事件
func (h *IncidentServiceTaskHandler) assignIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	assigneeID := GetIntFromVars(variables, "assignee_id")
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("分配事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	_, err = h.client.Incident.UpdateOneID(incidentID).
		SetAssigneeID(assigneeID).
		SetStatus(common.IncidentStatusAssigned).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("分配事件失败: %w", err)
	}

	h.logger.Infow("Incident assigned via BPMN", "incident_id", incidentID, "assignee_id", assigneeID, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已分配给用户 %d", incidentID, assigneeID),
	}, nil
}

// escalateIncident 升级事件
func (h *IncidentServiceTaskHandler) escalateIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	escalationLevel := GetIntFromVars(variables, "escalation_level")
	reason, _ := variables["escalation_reason"].(string)
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("升级事件失败: %w", err)
	}

	inc, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID)
	if err != nil {
		return nil, err
	}

	newLevel := escalationLevel
	if newLevel <= 0 {
		newLevel = inc.EscalationLevel + 1
	}

	now := time.Now()
	_, err = h.client.Incident.UpdateOneID(incidentID).
		SetEscalationLevel(newLevel).
		SetEscalatedAt(now).
		SetStatus(common.IncidentStatusEscalated).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("升级事件失败: %w", err)
	}

	h.logger.Infow("Incident escalated via BPMN",
		"incident_id", incidentID, "escalation_level", newLevel, "reason", reason, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已升级到第 %d 级", incidentID, newLevel),
	}, nil
}

// resolveIncident 解决事件
func (h *IncidentServiceTaskHandler) resolveIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	resolution, _ := variables["resolution"].(string)
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("解决事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = h.client.Incident.UpdateOneID(incidentID).
		SetStatus(common.IncidentStatusResolved).
		SetResolvedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("解决事件失败: %w", err)
	}

	h.logger.Infow("Incident resolved via BPMN", "incident_id", incidentID, "resolution", resolution, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已解决: %s", incidentID, resolution),
	}, nil
}

// closeIncident 关闭事件
func (h *IncidentServiceTaskHandler) closeIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	feedback, _ := variables["feedback"].(string)
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("关闭事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = h.client.Incident.UpdateOneID(incidentID).
		SetStatus(common.IncidentStatusClosed).
		SetClosedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("关闭事件失败: %w", err)
	}

	h.logger.Infow("Incident closed via BPMN", "incident_id", incidentID, "feedback", feedback, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已关闭", incidentID),
	}, nil
}

// updateIncident 更新事件
func (h *IncidentServiceTaskHandler) updateIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("更新事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	updateQuery := h.client.Incident.UpdateOneID(incidentID)

	if title, ok := variables["title"].(string); ok && title != "" {
		updateQuery.SetTitle(title)
	}
	if description, ok := variables["description"].(string); ok && description != "" {
		updateQuery.SetDescription(description)
	}
	if priority, ok := variables["priority"].(string); ok && priority != "" {
		updateQuery.SetPriority(priority)
	}
	if severity, ok := variables["severity"].(string); ok && severity != "" {
		updateQuery.SetSeverity(severity)
	}
	if status, ok := variables["status"].(string); ok && status != "" {
		updateQuery.SetStatus(status)
	}

	_, err = updateQuery.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("更新事件失败: %w", err)
	}

	h.logger.Infow("Incident updated via BPMN", "incident_id", incidentID, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已更新", incidentID),
	}, nil
}

// acknowledgeIncident 确认事件
func (h *IncidentServiceTaskHandler) acknowledgeIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("确认事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	_, err = h.client.Incident.UpdateOneID(incidentID).
		SetStatus(common.IncidentStatusAcknowledged).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("确认事件失败: %w", err)
	}

	h.logger.Infow("Incident acknowledged via BPMN", "incident_id", incidentID, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已确认", incidentID),
	}, nil
}

// categorizeIncident 分类事件
func (h *IncidentServiceTaskHandler) categorizeIncident(ctx context.Context, variables map[string]interface{}) (*dto.ServiceTaskResult, error) {
	incidentID := GetIntFromVars(variables, "incident_id")
	category, _ := variables["category"].(string)
	subcategory, _ := variables["subcategory"].(string)
	tenantID, err := ResolveTenantID(ctx, variables)
	if err != nil {
		return nil, fmt.Errorf("分类事件失败: %w", err)
	}

	if _, err := h.getIncidentByIDWithTenant(ctx, incidentID, tenantID); err != nil {
		return nil, err
	}

	updateQuery := h.client.Incident.UpdateOneID(incidentID).SetStatus(common.IncidentStatusTriaged)
	if category != "" {
		updateQuery.SetCategory(category)
	}
	if subcategory != "" {
		updateQuery.SetSubcategory(subcategory)
	}

	_, err = updateQuery.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("分类事件失败: %w", err)
	}

	h.logger.Infow("Incident categorized via BPMN",
		"incident_id", incidentID, "category", category, "subcategory", subcategory, "tenant_id", tenantID)

	return &dto.ServiceTaskResult{
		Success: true,
		Message: fmt.Sprintf("事件 %d 已分类: %s/%s", incidentID, category, subcategory),
	}, nil
}

// Ensure IncidentServiceTaskHandler implements ServiceTaskHandlerInterface
var _ ServiceTaskHandlerInterface = (*IncidentServiceTaskHandler)(nil)
