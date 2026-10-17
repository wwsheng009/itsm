package project

import (
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 风险/问题台账 DTO 与状态机（P2，PE-21）
//
// 权威定义：docs/project-management/plan/p2-risk-design.md（上游 02 §3.5）。
// 状态机：open →(mitigate) mitigating、open|mitigating →(close) closed、closed →(reopen) open；
// close 必填 resolution（D-27）。
// ---------------------------------------------------------------------------

const (
	RiskKindRisk  = "risk"
	RiskKindIssue = "issue"
)

const (
	RiskLevelLow    = "low"
	RiskLevelMedium = "medium"
	RiskLevelHigh   = "high"
)

const (
	RiskStatusOpen       = "open"
	RiskStatusMitigating = "mitigating"
	RiskStatusClosed     = "closed"
)

const (
	RiskActionMitigate = "mitigate"
	RiskActionClose    = "close"
	RiskActionReopen   = "reopen"
)

// 转换目标（D-28）。
const (
	ConvertTargetProblem = "problem"
	ConvertTargetChange  = "change"
)

// riskActionTransitions：动作 → 允许来源状态 → 目标状态。
var riskActionTransitions = map[string]map[string]string{
	RiskActionMitigate: {RiskStatusOpen: RiskStatusMitigating},
	RiskActionClose: {
		RiskStatusOpen:       RiskStatusClosed,
		RiskStatusMitigating: RiskStatusClosed,
	},
	RiskActionReopen: {RiskStatusClosed: RiskStatusOpen},
}

// 词表（写入/过滤校验）。
var (
	validRiskKinds    = map[string]bool{RiskKindRisk: true, RiskKindIssue: true}
	validRiskLevels   = map[string]bool{RiskLevelLow: true, RiskLevelMedium: true, RiskLevelHigh: true}
	validRiskStatuses = map[string]bool{RiskStatusOpen: true, RiskStatusMitigating: true, RiskStatusClosed: true}
)

// ---------------------------------------------------------------------------
// 请求 DTO（PE-21）
// ---------------------------------------------------------------------------

// RiskCreateRequest 创建风险/问题。
type RiskCreateRequest struct {
	Kind            string `json:"kind"`
	Title           string `json:"title" binding:"required"`
	Description     string `json:"description"`
	Level           string `json:"level"`
	Probability     string `json:"probability"`
	Impact          string `json:"impact"`
	OwnerID         int    `json:"ownerId"`
	RelatedTicketID int    `json:"relatedTicketId"`
}

// RiskUpdateRequest 更新风险/问题（nil = 不修改；ownerId/relatedTicketId 传 0 清空）。
type RiskUpdateRequest struct {
	Kind            *string `json:"kind"`
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	Level           *string `json:"level"`
	Probability     *string `json:"probability"`
	Impact          *string `json:"impact"`
	OwnerID         *int    `json:"ownerId"`
	RelatedTicketID *int    `json:"relatedTicketId"`
	Version         *int    `json:"version"`
}

// RiskStatusRequest 状态流转（mitigate|close|reopen；close 必填 resolution）。
type RiskStatusRequest struct {
	Action     string `json:"action" binding:"required"`
	Resolution string `json:"resolution"`
	Version    *int   `json:"version"`
}

// RiskConvertRequest 转问题单/变更（target=problem|change）。
type RiskConvertRequest struct {
	Target string `json:"target" binding:"required"`
}

// RiskConvertResponse 转换响应（产物ID + 更新后的风险）。
type RiskConvertResponse struct {
	Target    string               `json:"target"`
	CreatedID int                  `json:"createdId"`
	Risk      *ProjectRiskResponse `json:"risk"`
}

// ---------------------------------------------------------------------------
// 响应 DTO（类型名 ProjectRiskResponse 供 common.inferDomainAlias 映射到领域别名 projectRisks）
// ---------------------------------------------------------------------------

// ProjectRiskResponse 风险/问题响应（ownerName 由服务层批量填充）。
type ProjectRiskResponse struct {
	ID              int        `json:"id"`
	ProjectID       int        `json:"projectId"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Level           string     `json:"level"`
	Probability     string     `json:"probability,omitempty"`
	Impact          string     `json:"impact,omitempty"`
	OwnerID         *int       `json:"ownerId,omitempty"`
	OwnerName       string     `json:"ownerName,omitempty"`
	Status          string     `json:"status"`
	Resolution      string     `json:"resolution,omitempty"`
	RelatedTicketID *int       `json:"relatedTicketId,omitempty"`
	RelatedProblemID *int      `json:"relatedProblemId,omitempty"`
	RelatedChangeID *int       `json:"relatedChangeId,omitempty"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// ToRiskResponse ent → 响应（NULL 语义保留；ownerName 由调用方填充）。
func ToRiskResponse(r *ent.ProjectRisk) *ProjectRiskResponse {
	if r == nil {
		return nil
	}
	resp := &ProjectRiskResponse{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Kind:        r.Kind,
		Title:       r.Title,
		Description: r.Description,
		Level:       r.Level,
		Probability: r.Probability,
		Impact:      r.Impact,
		Status:      r.Status,
		Resolution:  r.Resolution,
		Version:     r.Version,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.OwnerID > 0 {
		id := r.OwnerID
		resp.OwnerID = &id
	}
	if r.RelatedTicketID > 0 {
		id := r.RelatedTicketID
		resp.RelatedTicketID = &id
	}
	if r.RelatedProblemID > 0 {
		id := r.RelatedProblemID
		resp.RelatedProblemID = &id
	}
	if r.RelatedChangeID > 0 {
		id := r.RelatedChangeID
		resp.RelatedChangeID = &id
	}
	return resp
}

// ToRiskResponseList 批量映射（ownerNames 按 ID 填充，缺失保持空）。
func ToRiskResponseList(items []*ent.ProjectRisk, ownerNames map[int]string) []*ProjectRiskResponse {
	out := make([]*ProjectRiskResponse, 0, len(items))
	for _, it := range items {
		resp := ToRiskResponse(it)
		if resp != nil && resp.OwnerID != nil {
			resp.OwnerName = ownerNames[*resp.OwnerID]
		}
		out = append(out, resp)
	}
	return out
}