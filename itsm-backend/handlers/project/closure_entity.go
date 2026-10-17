package project

import (
	"strings"
	"time"

	"itsm-backend/ent"
)

// 结项清单（closure checklist）常量与 DTO（PE-15）。
//
// 权威定义：docs/project-management/plan/p1-closure-design.md +
// docs/project-management/01-module-blueprint.md §3「结项门」。
//
// 清单项分两类：
//   - 自动派生（不入库，读时计算）：tasks（任务清零）、records（关联单据终态）、gate（结项门通过）；
//   - 人工/产物（落库 project_closures）：retro（复盘 KB 草稿）、assets（资产/许可登记）、
//     members（成员回收）、csat（CSAT 触发；非阻塞——契约 §4「满意度缺失不阻塞结项」）。

// 清单项标识。
const (
	ClosureItemTasks   = "tasks"   // 任务清零（todo/in_progress/blocked 清零）
	ClosureItemRecords = "records" // 关联单据全部终态（或已转出）
	ClosureItemGate    = "gate"    // 结项阶段门 passed
	ClosureItemRetro   = "retro"   // 复盘 KB 草稿已生成（不自动发布）
	ClosureItemAssets  = "assets"  // 资产/许可登记已确认（或豁免并说明原因）
	ClosureItemMembers = "members" // 成员回收已执行
	ClosureItemCSAT    = "csat"    // CSAT 调研已触发（非阻塞）
)

// 清单项前端动作标识。
const (
	ClosureActionGenerateRetro  = "generate_retro"
	ClosureActionConfirmAssets  = "confirm_assets"
	ClosureActionRecycleMembers = "recycle_members"
	ClosureActionTriggerCSAT    = "trigger_csat"
)

// 清单项状态。
const (
	ClosureItemDone    = "done"
	ClosureItemPending = "pending"
)

// closureChecklistOrder 清单展示顺序（自动项 → 人工/产物项 → CSAT）。
var closureChecklistOrder = []string{
	ClosureItemTasks, ClosureItemRecords, ClosureItemGate,
	ClosureItemRetro, ClosureItemAssets, ClosureItemMembers, ClosureItemCSAT,
}

// closureItemTitles 清单项标题（响应与阻塞文案共用）。
var closureItemTitles = map[string]string{
	ClosureItemTasks:   "任务清零",
	ClosureItemRecords: "关联单据终态",
	ClosureItemGate:    "结项阶段门通过",
	ClosureItemRetro:   "复盘 KB 草稿",
	ClosureItemAssets:  "资产/许可登记",
	ClosureItemMembers: "成员回收",
	ClosureItemCSAT:    "CSAT 调研",
}

// closureBlockingItems 阻塞结项的清单项（不含自动项——它们由既有门禁单独暴露；CSAT 不阻塞结项）。
var closureBlockingItems = []string{ClosureItemRetro, ClosureItemAssets, ClosureItemMembers}

// ClosureItem 清单项（自动派生与落库状态合并后的视图）。
type ClosureItem struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status"` // done|pending
	Blocking bool   `json:"blocking"`
	Detail   string `json:"detail,omitempty"`
	Action   string `json:"action,omitempty"` // 前端可用动作（无=只读/自动项）
}

// ProjectClosureResponse 结项清单响应。
type ProjectClosureResponse struct {
	ID        int    `json:"id"`
	ProjectID int    `json:"projectId"`
	Status    string `json:"status"` // pending=存在未通过项；ready=可结项；completed=项目已结项
	Items     []ClosureItem `json:"items"`
	// Blockers 未通过的阻塞项（含自动项，供前端一次展示全部缺口）。
	Blockers []Blocker `json:"blockers"`
	// 产物与动作留痕。
	RetroKbArticleID  int        `json:"retroKbArticleId"`
	RetroGeneratedAt  *time.Time `json:"retroGeneratedAt,omitempty"`
	AssetsNote        string     `json:"assetsNote,omitempty"`
	AssetsWaived      bool       `json:"assetsWaived"`
	AssetsWaiveReason string     `json:"assetsWaiveReason,omitempty"`
	AssetsConfirmedAt *time.Time `json:"assetsConfirmedAt,omitempty"`
	// AssetsLinks 采购/资产结项登记明细（PE-26；读时富化，上限 200）。
	AssetsLinks []AssetLinkItem `json:"assetsLinks"`
	// AssetsLinkSummary 按类型计数（恒含 asset/asset_license/vendor/contract 四键，缺省 0）。
	AssetsLinkSummary map[string]int `json:"assetsLinkSummary"`
	// AssetsLinksTruncated 登记明细超出上限（200）时为 true。
	AssetsLinksTruncated bool       `json:"assetsLinksTruncated"`
	MembersRecycledAt    *time.Time `json:"membersRecycledAt,omitempty"`
	MembersRecycledCount int        `json:"membersRecycledCount"`
	CSATSurveyID         int        `json:"csatSurveyId"`
	CSATTriggeredAt      *time.Time `json:"csatTriggeredAt,omitempty"`
	Version              int        `json:"version"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

// ClosureAssetsRequest 资产/许可登记确认请求（action=confirm|waive）。
type ClosureAssetsRequest struct {
	// Action confirm=确认已登记；waive=本项目无资产/许可登记（豁免，必填原因）。
	Action string `json:"action" binding:"required"`
	// Note 登记备注（confirm 可选；如登记位置/清单说明）。
	Note string `json:"note"`
	// Reason 豁免原因（waive 必填）。
	Reason string `json:"reason"`
}

// Normalize 规范化。
func (r *ClosureAssetsRequest) Normalize() {
	r.Action = strings.ToLower(strings.TrimSpace(r.Action))
	r.Note = strings.TrimSpace(r.Note)
	r.Reason = strings.TrimSpace(r.Reason)
}

// Valid 校验动作与必填项。
func (r *ClosureAssetsRequest) Valid() bool {
	switch r.Action {
	case "confirm":
		return true
	case "waive":
		return r.Reason != ""
	default:
		return false
	}
}

// closureStatus 由清单项推导整体状态（completed 语义由服务层结合项目状态覆盖）。
func closureStatus(items []ClosureItem) string {
	for _, item := range items {
		if item.Blocking && item.Status != ClosureItemDone {
			return "pending"
		}
	}
	return "ready"
}

// ToClosureResponse 实体 + 派生项 → 响应。
func ToClosureResponse(
	projectID int, record *ent.ProjectClosure, items []ClosureItem, blockers []Blocker, projectStatus string,
) ProjectClosureResponse {
	resp := ProjectClosureResponse{
		ProjectID: projectID,
		Status:    closureStatus(items),
		Items:     items,
		Blockers:  blockers,
	}
	if resp.Blockers == nil {
		resp.Blockers = []Blocker{}
	}
	if resp.Items == nil {
		resp.Items = []ClosureItem{}
	}
	if projectStatus == StatusCompleted || projectStatus == StatusCancelled {
		// 项目已终结：清单状态跟随项目终态（只读留痕，不再表达「可结项」）。
		resp.Status = projectStatus
	}
	if record == nil {
		return resp
	}
	resp.ID = record.ID
	resp.RetroKbArticleID = record.RetroKBArticleID
	resp.RetroGeneratedAt = record.RetroGeneratedAt
	resp.AssetsNote = record.AssetsNote
	resp.AssetsWaived = record.AssetsWaivedAt != nil
	resp.AssetsWaiveReason = record.AssetsWaiveReason
	resp.AssetsConfirmedAt = record.AssetsConfirmedAt
	resp.MembersRecycledAt = record.MembersRecycledAt
	resp.MembersRecycledCount = record.MembersRecycledCount
	resp.CSATSurveyID = record.CsatSurveyID
	resp.CSATTriggeredAt = record.CsatTriggeredAt
	resp.Version = record.Version
	resp.UpdatedAt = record.UpdatedAt
	return resp
}