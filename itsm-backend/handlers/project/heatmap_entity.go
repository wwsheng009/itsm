package project

import "encoding/json"

// ---------------------------------------------------------------------------
// P2 PE-23 资源负荷只读热图 DTO
// 权威设计：docs/project-management/plan/p2-resource-heatmap-design.md（D-37~D-39）
// ---------------------------------------------------------------------------

const (
	// DefaultWeeklyCapacityHours 内置周容量兜底（小时/人/周；D-37）。
	DefaultWeeklyCapacityHours = 40.0
	// MaxWeeklyCapacityHours 周容量上限（7×24）。
	MaxWeeklyCapacityHours = 168.0
	// HeatmapMinWeeks / HeatmapMaxWeeks 热图窗口周数边界。
	HeatmapMinWeeks = 1
	HeatmapMaxWeeks = 12
	// HeatmapDefaultWeeks 缺省窗口周数。
	HeatmapDefaultWeeks = 6
)

// OptionalFloat 三态浮点：缺省（Set=false，不修改）/ null（Set=true, Value=nil，清空）/ 数值。
//
// 说明：JSON 中 null 与字段缺省在 *float64 上无法区分，容量配置需要"清空=回继承"语义，
// 故用显式三态（与 D-37 的部分更新语义配套）。
type OptionalFloat struct {
	Set   bool
	Value *float64
}

// UnmarshalJSON 实现 json.Unmarshaler（null → Set 且 Value=nil）。
func (o *OptionalFloat) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	o.Value = &f
	return nil
}

// CapacityMemberInput 成员容量项（weeklyCapacityHours 缺省=不修改；null/0=清空回继承）。
type CapacityMemberInput struct {
	UserID              int           `json:"userId" binding:"required"`
	WeeklyCapacityHours OptionalFloat `json:"weeklyCapacityHours"`
}

// CapacityUpdateRequest 容量配置请求（字段缺省=不修改；null/0=清空）。
type CapacityUpdateRequest struct {
	DefaultWeeklyCapacityHours OptionalFloat        `json:"defaultWeeklyCapacityHours"`
	Members                    []CapacityMemberInput `json:"members"`
}

// CapacityMemberView 成员容量视图（仅存活成员）。
type CapacityMemberView struct {
	UserID              int      `json:"userId"`
	WeeklyCapacityHours *float64 `json:"weeklyCapacityHours"`
}

// CapacityResponse 容量配置响应（members=存活成员全量，按加入顺序）。
type CapacityResponse struct {
	DefaultWeeklyCapacityHours *float64             `json:"defaultWeeklyCapacityHours"`
	Members                    []CapacityMemberView `json:"members"`
}

// HeatmapCalendar 日历口径标注（B7：v1 自然日 + 明确标注，不接租户工作日历）。
type HeatmapCalendar struct {
	Mode             string `json:"mode"`
	BusinessDaysOnly bool   `json:"businessDaysOnly"`
	Note             string `json:"note"`
}

// HeatmapDefaults 容量默认解析结果（source=project|fallback）。
type HeatmapDefaults struct {
	DefaultWeeklyCapacityHours float64 `json:"defaultWeeklyCapacityHours"`
	Source                     string  `json:"source"`
}

// HeatmapRange 窗口区间（含首尾，均为 ISO 周边界）。
type HeatmapRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// HeatmapUnplanned 未排期提示（有估算但无计划日期，无法摊分）。
type HeatmapUnplanned struct {
	TaskCount     int     `json:"taskCount"`
	EstimateHours float64 `json:"estimateHours"`
}

// HeatmapMemberWeek 单元格：某成员某周的计划/剩余负荷 vs 容量。
type HeatmapMemberWeek struct {
	UserID         int     `json:"userId"`
	Name           string  `json:"name"`
	Username       string  `json:"username"`
	ProjectRole    string  `json:"projectRole"`
	MemberStatus   string  `json:"memberStatus"`
	CapacityHours  float64 `json:"capacityHours"`
	PlannedHours   float64 `json:"plannedHours"`
	RemainingHours float64 `json:"remainingHours"`
	PlannedRatio   float64 `json:"plannedRatio"`
	RemainingRatio float64 `json:"remainingRatio"`
}

// HeatmapWeek 一周（周一~周日）的全部成员行。
type HeatmapWeek struct {
	WeekStart string              `json:"weekStart"`
	WeekEnd   string              `json:"weekEnd"`
	Members   []HeatmapMemberWeek `json:"members"`
}

// ResourceHeatmap 资源负荷热图响应（只读；实时计算不落库，D-39）。
type ResourceHeatmap struct {
	ProjectID int             `json:"projectId"`
	Range     HeatmapRange    `json:"range"`
	Calendar  HeatmapCalendar `json:"calendar"`
	Defaults  HeatmapDefaults `json:"defaults"`
	Weeks     []HeatmapWeek   `json:"weeks"`
	Unplanned HeatmapUnplanned `json:"unplanned"`
}