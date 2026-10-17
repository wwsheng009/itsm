package project

import (
	"strings"
	"time"
)

// 采购/资产结项登记（P2，PE-26）。
//
// 权威定义：docs/project-management/plan/p2-procurement-design.md（D-49~D-53）。
// 语义：结项清单「资产/许可登记」的结构化台账链接——项目 → 既有台账
// （asset|asset_license|vendor|contract，同租户弱引用 target_id，不做跨租户引用）。

// 链接类型词表（D-49；与蓝图「链接既有 asset/asset_license/vendor/contract」一致）。
const (
	AssetLinkTypeAsset        = "asset"
	AssetLinkTypeAssetLicense = "asset_license"
	AssetLinkTypeVendor       = "vendor"
	AssetLinkTypeContract     = "contract"
)

// assetLinkTypeOrder 摘要/展示顺序（summary 恒含四键，缺省 0，避免前端判空）。
var assetLinkTypeOrder = []string{
	AssetLinkTypeAsset, AssetLinkTypeAssetLicense, AssetLinkTypeVendor, AssetLinkTypeContract,
}

// validAssetLinkTypes 写入词表（D-49）。
var validAssetLinkTypes = map[string]bool{
	AssetLinkTypeAsset:        true,
	AssetLinkTypeAssetLicense: true,
	AssetLinkTypeVendor:       true,
	AssetLinkTypeContract:     true,
}

// assetLinkMaxItems 登记列表硬上限（不分页；超出截断并显式 truncated，D-49）。
const assetLinkMaxItems = 200

// assetLinkNoteMaxRunes 备注长度上限（与列宽 512 对齐）。
const assetLinkNoteMaxRunes = 512

// 候选选择器限额（D-52）。
const (
	assetLinkCandidatesDefaultLimit = 20
	assetLinkCandidatesMaxLimit     = 50
)

// AssetLinkCreateRequest 新增登记请求（D-49/D-50）。
type AssetLinkCreateRequest struct {
	// LinkType asset|asset_license|vendor|contract。
	LinkType string `json:"linkType" binding:"required"`
	// TargetID 同租户目标 ID（弱引用；写入时存在性校验）。
	TargetID int `json:"targetId" binding:"required"`
	// Note 登记备注（可选，≤512 字符）。
	Note string `json:"note"`
}

// Normalize 规范化。
func (r *AssetLinkCreateRequest) Normalize() {
	r.LinkType = strings.ToLower(strings.TrimSpace(r.LinkType))
	r.Note = strings.TrimSpace(r.Note)
}

// Valid 校验（类型词表 + 目标 ID + 备注长度）。
func (r *AssetLinkCreateRequest) Valid() bool {
	return validAssetLinkTypes[r.LinkType] && r.TargetID > 0 &&
		len([]rune(r.Note)) <= assetLinkNoteMaxRunes
}

// AssetLinkTargetInfo 目标标签（读时富化；label=编号/名称主键，sublabel=辅助说明）。
type AssetLinkTargetInfo struct {
	Label    string
	Sublabel string
}

// AssetLinkItem 登记行响应（结项清单 assetsLinks 与登记列表共用形状）。
type AssetLinkItem struct {
	ID       int    `json:"id"`
	LinkType string `json:"linkType"`
	TargetID int    `json:"targetId"`
	// Label/Sublabel 读时富化的目标标签；TargetMissing=true 时 Label 为空。
	Label         string    `json:"label"`
	Sublabel      string    `json:"sublabel,omitempty"`
	Note          string    `json:"note,omitempty"`
	TargetMissing bool      `json:"targetMissing"`
	CreatedBy     int       `json:"createdBy"`
	CreatedAt     time.Time `json:"createdAt"`
}

// AssetLinkListResponse 登记列表响应（GET / POST / DELETE 同构，便于一次刷新）。
type AssetLinkListResponse struct {
	Items   []AssetLinkItem `json:"items"`
	Summary map[string]int  `json:"summary"`
	Total   int             `json:"total"`
	// Truncated 超出硬上限（200）时为 true（不静默截断）。
	Truncated bool `json:"truncated"`
}

// AssetLinkCandidate 候选行（最小字段，D-52：不回传价格/密钥/联系方式/合同金额）。
type AssetLinkCandidate struct {
	ID       int    `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
}

// AssetLinkCandidateListResponse 候选响应。
type AssetLinkCandidateListResponse struct {
	Items     []AssetLinkCandidate `json:"items"`
	Truncated bool                 `json:"truncated"`
}

// newAssetLinkSummary 四键全量摘要（缺省 0）。
func newAssetLinkSummary() map[string]int {
	summary := make(map[string]int, len(assetLinkTypeOrder))
	for _, t := range assetLinkTypeOrder {
		summary[t] = 0
	}
	return summary
}