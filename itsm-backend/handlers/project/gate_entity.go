package project

import (
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
)

// 阶段门（stage-gate）常量与 DTO（PE-12）。
//
// 权威定义：docs/project-management/01-module-blueprint.md §3「阶段门」+
// docs/project-management/plan/p1-gate-design.md（详细设计：规则/流程/端点/状态机）。

// 门标识（business_sub_type 取值；process_binding.business_type=project）。
const (
	GateInitiation = "initiation" // 立项门：planning → active 前
	GatePlan       = "plan"       // 计划门：进入执行前（通过时冻结基线）
	GateAcceptance = "acceptance" // 验收门：交付完成前
	GateClosure    = "closure"    // 结项门：→ completed 前
)

// 门状态。
const (
	GateStatusPending  = "pending"   // 未提交
	GateStatusInReview = "in_review" // 审批中
	GateStatusPassed   = "passed"    // 已通过
	GateStatusRejected = "rejected"  // 已拒绝（可重新提交）
)

// gateSequence 四道门的固定顺序：后门的前置是前门 passed。
var gateSequence = []string{GateInitiation, GatePlan, GateAcceptance, GateClosure}

// GateOrder 返回阶段门顺序（外发用，避免调用方自造顺序）。
func GateOrder() []string {
	out := make([]string, len(gateSequence))
	copy(out, gateSequence)
	return out
}

// IsValidGate 门标识合法性。
func IsValidGate(gate string) bool {
	for _, g := range gateSequence {
		if g == gate {
			return true
		}
	}
	return false
}

// previousGate 返回某门的前置门；首门返回空串。
func previousGate(gate string) string {
	for i, g := range gateSequence {
		if g == gate && i > 0 {
			return gateSequence[i-1]
		}
	}
	return ""
}

// GateBlocker 门禁阻塞项（响应体 blockers 元素）。
type GateBlocker struct {
	Type   string `json:"type"`             // gate_not_passed|manager_missing|member_missing|task_missing|milestone_missing|task_open|milestone_open|record_open
	Detail string `json:"detail,omitempty"` // 可读说明
	RefID  int    `json:"refId,omitempty"`  // 关联对象ID（如未终态单据/任务）
	Number string `json:"number,omitempty"` // 关联对象编号（单据）
	Status string `json:"status,omitempty"` // 关联对象状态
	Total  int    `json:"total,omitempty"`  // 该类阻塞总数（明细截断时给出总量）
}

// GateBlockedError 门禁阻塞（409 + blockers 明细）。
type GateBlockedError struct {
	Gate     string
	Blockers []GateBlocker
}

func (e *GateBlockedError) Error() string {
	return fmt.Sprintf("stage gate %q blocked by %d item(s)", e.Gate, len(e.Blockers))
}

// GateDecisionRequest 阶段门裁决请求。
type GateDecisionRequest struct {
	// Decision approve|reject。
	Decision string `json:"decision" binding:"required"`
	// Comment 裁决意见；reject 必填（拒绝含明确原因）。
	Comment string `json:"comment"`
}

// Normalize 规范化（去空格 + 小写）。
func (r *GateDecisionRequest) Normalize() {
	r.Decision = strings.ToLower(strings.TrimSpace(r.Decision))
	r.Comment = strings.TrimSpace(r.Comment)
}

// Valid 校验裁决动作。
func (r *GateDecisionRequest) Valid() bool {
	return r.Decision == "approve" || r.Decision == "reject"
}

// GateSubmitRequest 阶段门提交请求（当前无必填字段，保留扩展位：备注）。
type GateSubmitRequest struct {
	Comment string `json:"comment"`
}

// ProjectGateResponse 阶段门响应（类型名供领域别名 projectGates）。
type ProjectGateResponse struct {
	ID               int            `json:"id"`
	ProjectID        int            `json:"projectId"`
	Gate             string         `json:"gate"`
	Status           string         `json:"status"`
	ProcessInstanceID int           `json:"processInstanceId"`
	ApprovalChainID  string         `json:"approvalChainId,omitempty"`
	SubmittedBy      int            `json:"submittedBy"`
	SubmittedAt      *time.Time     `json:"submittedAt,omitempty"`
	DecidedBy        int            `json:"decidedBy"`
	DecidedAt        *time.Time     `json:"decidedAt,omitempty"`
	DecisionComment  string         `json:"decisionComment,omitempty"`
	Blockers         []GateBlocker  `json:"blockers"`
	// PE-24：累计被拒绝次数（一次通过率度量；passed 且 0 = 一次通过）。
	RejectCount      int            `json:"rejectCount"`
	Version          int            `json:"version"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

// ToGateResponse 实体 → 响应。
func ToGateResponse(g *ent.ProjectGate) ProjectGateResponse {
	resp := ProjectGateResponse{
		ID:                g.ID,
		ProjectID:         g.ProjectID,
		Gate:              g.Gate,
		Status:            g.Status,
		ProcessInstanceID: g.ProcessInstanceID,
		ApprovalChainID:   g.ApprovalChainID,
		SubmittedBy:       g.SubmittedBy,
		DecidedBy:         g.DecidedBy,
		DecisionComment:   g.DecisionComment,
		Blockers:          gateBlockersFromJSON(g.Blockers),
		RejectCount:       g.RejectCount,
		Version:           g.Version,
		CreatedAt:         g.CreatedAt,
		UpdatedAt:         g.UpdatedAt,
	}
	if g.SubmittedAt != nil {
		resp.SubmittedAt = g.SubmittedAt
	}
	if g.DecidedAt != nil {
		resp.DecidedAt = g.DecidedAt
	}
	return resp
}

// ToGateResponseList 列表转换（空切片而非 null）。
func ToGateResponseList(items []*ent.ProjectGate) []ProjectGateResponse {
	out := make([]ProjectGateResponse, 0, len(items))
	for _, item := range items {
		out = append(out, ToGateResponse(item))
	}
	return out
}

// gateBlockersFromJSON JSONB → 响应类型。
func gateBlockersFromJSON(raw []map[string]any) []GateBlocker {
	out := make([]GateBlocker, 0, len(raw))
	for _, m := range raw {
		b := GateBlocker{}
		if v, ok := m["type"].(string); ok {
			b.Type = v
		}
		if v, ok := m["detail"].(string); ok {
			b.Detail = v
		}
		if v, ok := m["refId"].(float64); ok {
			b.RefID = int(v)
		}
		if v, ok := m["number"].(string); ok {
			b.Number = v
		}
		if v, ok := m["status"].(string); ok {
			b.Status = v
		}
		if v, ok := m["total"].(float64); ok {
			b.Total = int(v)
		}
		out = append(out, b)
	}
	return out
}

// gateBlockersToJSON 响应类型 → JSONB。
func gateBlockersToJSON(items []GateBlocker) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, b := range items {
		m := map[string]any{"type": b.Type}
		if b.Detail != "" {
			m["detail"] = b.Detail
		}
		if b.RefID != 0 {
			m["refId"] = b.RefID
		}
		if b.Number != "" {
			m["number"] = b.Number
		}
		if b.Status != "" {
			m["status"] = b.Status
		}
		if b.Total != 0 {
			m["total"] = b.Total
		}
		out = append(out, m)
	}
	return out
}
