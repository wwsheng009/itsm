package project

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/processdefinition"
	"itsm-backend/ent/processinstance"
	"itsm-backend/service"
)

// GateProcessAdapter 阶段门审批端口实现（PE-12）：
// process_binding（business_type=project，business_sub_type=<gate>）路由 → BPMN 触发/审批桥接。
//
// 降级规则（对齐变更域的既有约定）：
//   - 无绑定（FindBestBinding 返回 nil）→ 提交即自动通过（processInstanceID=0）；
//   - 绑定指向的流程定义不存在 → 告警并降级自动通过，避免租户误配导致提交永久失败。
type GateProcessAdapter struct {
	client   *ent.Client
	trigger  service.ProcessTriggerServiceInterface
	bindings service.ProcessBindingServiceInterface
	bridge   *service.BPMNApprovalBridge
	logger   *zap.SugaredLogger
}

// ApprovalMode 实现 GateApprovalModeInspector：只读探测该门是否有可用审批流程（A4）。
// 判定与 SubmitGate 完全一致：端口缺失/无绑定/绑定定义缺失 → auto；否则 manual。
func (a *GateProcessAdapter) ApprovalMode(ctx context.Context, tenantID int, gate string) (string, error) {
	if a.trigger == nil || a.bindings == nil {
		return GateApprovalModeAuto, nil
	}
	binding, err := a.bindings.FindBestBinding(ctx, dto.BusinessTypeProject, gate, tenantID)
	if err != nil {
		return "", err
	}
	if binding == nil || binding.ProcessDefinitionKey == "" {
		return GateApprovalModeAuto, nil
	}
	exists, err := a.client.ProcessDefinition.Query().
		Where(
			processdefinition.Key(binding.ProcessDefinitionKey),
			processdefinition.TenantID(tenantID),
			processdefinition.IsLatest(true),
		).
		Exist(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return GateApprovalModeAuto, nil
	}
	return GateApprovalModeManual, nil
}

// NewGateProcessAdapter 构造适配器。
func NewGateProcessAdapter(
	client *ent.Client,
	trigger service.ProcessTriggerServiceInterface,
	bindings service.ProcessBindingServiceInterface,
	logger *zap.SugaredLogger,
) *GateProcessAdapter {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &GateProcessAdapter{
		client:   client,
		trigger:  trigger,
		bindings: bindings,
		bridge:   service.NewBPMNApprovalBridge(client, logger),
		logger:   logger,
	}
}

// SetGateProcess 注入阶段门审批适配器（bootstrap）。
func (s *Service) SetGateProcess(adapter *GateProcessAdapter) {
	if adapter != nil {
		s.gateApproval = adapter
	}
}

// SubmitGate 触发阶段门审批流程（无绑定/定义缺失 → 返回零值降级自动通过）。
func (a *GateProcessAdapter) SubmitGate(
	ctx context.Context, tenantID, projectID int, gate string, variables map[string]any, triggeredBy string,
) (GateApprovalStart, error) {
	if a.trigger == nil || a.bindings == nil {
		return GateApprovalStart{}, nil
	}
	binding, err := a.bindings.FindBestBinding(ctx, dto.BusinessTypeProject, gate, tenantID)
	if err != nil {
		return GateApprovalStart{}, err
	}
	if binding == nil || binding.ProcessDefinitionKey == "" {
		return GateApprovalStart{}, nil
	}

	// 存在性校验：租户误配/流程未部署时降级自动通过（避免门提交永久 409）。
	exists, err := a.client.ProcessDefinition.Query().
		Where(
			processdefinition.Key(binding.ProcessDefinitionKey),
			processdefinition.TenantID(tenantID),
			processdefinition.IsLatest(true),
		).
		Exist(ctx)
	if err != nil {
		return GateApprovalStart{}, err
	}
	if !exists {
		a.logger.Warnw("阶段门绑定指向的流程定义不存在，降级自动通过",
			"gate", gate, "processDefinitionKey", binding.ProcessDefinitionKey, "tenantId", tenantID)
		return GateApprovalStart{ApprovalChainID: binding.ApprovalChainID}, nil
	}

	resp, err := a.trigger.TriggerProcess(ctx, &dto.ProcessTriggerRequest{
		BusinessType:         dto.BusinessTypeProject,
		BusinessSubType:      gate,
		BusinessID:           projectID,
		ProcessDefinitionKey: binding.ProcessDefinitionKey,
		ProcessVersion:       binding.ProcessVersion,
		Variables:            variables,
		TriggeredBy:          triggeredBy,
		TriggeredAt:          time.Now(),
		TenantID:             tenantID,
	})
	if err != nil {
		return GateApprovalStart{}, fmt.Errorf("触发阶段门审批流程失败: %w", err)
	}
	return GateApprovalStart{
		ProcessInstanceID: resp.ProcessInstanceID,
		ApprovalChainID:   binding.ApprovalChainID,
	}, nil
}

// DecideGate 完成当前待办审批任务，并判断流程是否已结束。
func (a *GateProcessAdapter) DecideGate(
	ctx context.Context, tenantID, actorID, projectID int, decision, comment string,
) (GateApprovalDecision, error) {
	if a.bridge == nil {
		return GateApprovalDecision{}, nil
	}
	handled, err := a.bridge.CompleteBusinessApprovalTask(
		ctx, tenantID, actorID, string(dto.BusinessTypeProject), projectID, decision, comment,
	)
	if err != nil {
		return GateApprovalDecision{}, err
	}
	// 流程是否仍运行：按业务键（project:<id>）取最新运行实例。
	running, err := a.client.ProcessInstance.Query().
		Where(
			processinstance.BusinessKey(fmt.Sprintf("project:%d", projectID)),
			processinstance.TenantID(tenantID),
			processinstance.Status("running"),
		).
		Exist(ctx)
	if err != nil {
		return GateApprovalDecision{}, err
	}
	return GateApprovalDecision{Handled: handled, FlowFinished: !running}, nil
}
