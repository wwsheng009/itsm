package project

import (
	"context"
	"fmt"

	changeHandler "itsm-backend/handlers/change"
	problemHandler "itsm-backend/handlers/problem"
)

// 风险转换外呼端口（P2，PE-21 / D-28）：风险/问题 → ITSM 问题单 / 变更单。
//
// 端口定义在域内、适配器在本包（与 ClosureKnowledgeAdapter 同先例），
// bootstrap 负责装配；未装配时对应动作返回 503（ErrRiskPortUnavailable）。
// 转换语义：创建 + 回写关联（related_problem_id / related_change_id）；同目标二次转换 409。

// RiskConvertInput 转换入参（复用字段；适配器负责映射到目标域字段，D-29）。
type RiskConvertInput struct {
	RiskID      int
	Kind        string
	Title       string
	Description string
	Level       string
	Probability string
	Impact      string
	ProjectID   int
	ProjectCode string
	ProjectName string
}

// RiskProblemPort 风险→问题单端口。
type RiskProblemPort interface {
	CreateProblemFromRisk(ctx context.Context, tenantID, actorID int, in RiskConvertInput) (int, error)
}

// RiskChangePort 风险→变更单端口。
type RiskChangePort interface {
	CreateChangeFromRisk(ctx context.Context, tenantID, actorID int, in RiskConvertInput) (int, error)
}

// SetRiskPorts 注入风险转换端口（bootstrap；测试可注入假实现）。
func (s *Service) SetRiskPorts(problem RiskProblemPort, change RiskChangePort) {
	if problem != nil {
		s.riskProblem = problem
	}
	if change != nil {
		s.riskChange = change
	}
}

// riskKindLabel 中文标签（来源描述用）。
func riskKindLabel(kind string) string {
	if kind == RiskKindIssue {
		return "问题"
	}
	return "风险"
}

// riskConvertSource 来源描述（附加到目标单据描述尾部，保证可追溯）。
func riskConvertSource(in RiskConvertInput) string {
	project := in.ProjectName
	if in.ProjectCode != "" {
		project = fmt.Sprintf("%s（%s）", in.ProjectName, in.ProjectCode)
	}
	return fmt.Sprintf("来源：项目 %s %s #%d（等级 %s）", project, riskKindLabel(in.Kind), in.RiskID, in.Level)
}

// riskConvertDescription 目标单据描述 = 原描述 + 来源行。
func riskConvertDescription(in RiskConvertInput) string {
	desc := in.Description
	if desc != "" {
		desc += "\n\n"
	}
	return desc + riskConvertSource(in)
}

// riskLevelToPriority 等级映射（low→low / medium→medium / high→high；两域词表一致）。
func riskLevelToPriority(level string) string {
	switch level {
	case RiskLevelLow, RiskLevelMedium, RiskLevelHigh:
		return level
	default:
		return RiskLevelMedium
	}
}

// RiskProblemAdapter 风险→问题单适配器（复用问题域服务，走生产链路）。
type RiskProblemAdapter struct {
	svc *problemHandler.Service
}

// NewRiskProblemAdapter 构造适配器。
func NewRiskProblemAdapter(svc *problemHandler.Service) *RiskProblemAdapter {
	return &RiskProblemAdapter{svc: svc}
}

// CreateProblemFromRisk 创建问题单，返回问题 ID。
func (a *RiskProblemAdapter) CreateProblemFromRisk(
	ctx context.Context, tenantID, actorID int, in RiskConvertInput,
) (int, error) {
	p, err := a.svc.Create(ctx, tenantID, &problemHandler.Problem{
		Title:       in.Title,
		Description: riskConvertDescription(in),
		Priority:    riskLevelToPriority(in.Level),
		CreatedBy:   actorID,
	})
	if err != nil {
		return 0, err
	}
	return p.ID, nil
}

// RiskChangeAdapter 风险→变更单适配器（复用变更域服务，走生产链路）。
type RiskChangeAdapter struct {
	svc *changeHandler.Service
}

// NewRiskChangeAdapter 构造适配器。
func NewRiskChangeAdapter(svc *changeHandler.Service) *RiskChangeAdapter {
	return &RiskChangeAdapter{svc: svc}
}

// CreateChangeFromRisk 创建变更单（draft），返回变更 ID。
func (a *RiskChangeAdapter) CreateChangeFromRisk(
	ctx context.Context, tenantID, actorID int, in RiskConvertInput,
) (int, error) {
	c, err := a.svc.CreateChange(ctx, &changeHandler.Change{
		Title:         in.Title,
		Description:   riskConvertDescription(in),
		Justification: riskConvertSource(in),
		Type:          "normal",
		Status:        "draft",
		Priority:      riskLevelToPriority(in.Level),
		ImpactScope:   "medium",
		RiskLevel:     riskLevelToPriority(in.Level),
		CreatedBy:     actorID,
		TenantID:      tenantID,
	})
	if err != nil {
		return 0, err
	}
	return c.ID, nil
}