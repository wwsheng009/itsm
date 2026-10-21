package project

import (
	"math"
	"sort"
	"strconv"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// P2 PE-24 组合视图与度量 DTO（D-40~D-43）
// 权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md §4
// ---------------------------------------------------------------------------

const (
	// PortfolioMaxProjects 组合视图 projects 行上限（超出 truncated=true）。
	PortfolioMaxProjects = 500
	// PortfolioAggregateMaxProjects 聚合取数上限（组合与度量共用的项目集上限；超过 truncated=true，不静默）。
	PortfolioAggregateMaxProjects = 2000
	// MetricsDetailMaxRows 每组明细行上限（截断只影响展示；分子/分母恒为全量）。
	MetricsDetailMaxRows = 200
)

// PortfolioBucket 聚合桶（key 为维度值字符串；count 降序、key 升序稳定排序）。
type PortfolioBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	// Name 富化名称（manager=用户姓名，缺失/未设置为「未设置」；department 名称由前端映射，后端不跨域查询）。
	Name string `json:"name,omitempty"`
}

// PortfolioSummary 组合聚合摘要。
type PortfolioSummary struct {
	Total        int               `json:"total"`
	ByStatus     []PortfolioBucket `json:"byStatus"`
	ByHealth     []PortfolioBucket `json:"byHealth"`
	ByType       []PortfolioBucket `json:"byType"`
	ByManager    []PortfolioBucket `json:"byManager"`
	ByDepartment []PortfolioBucket `json:"byDepartment"`
}

// PortfolioResponse 组合视图响应（projects 行与 GET /projects 同结构同序，前端可直接对账）。
type PortfolioResponse struct {
	GeneratedAt time.Time          `json:"generatedAt"`
	Truncated   bool               `json:"truncated"`
	Summary     PortfolioSummary   `json:"summary"`
	Projects    []*ProjectResponse `json:"projects"`
}

// MetricsDateWindow from/to 时间窗回显（口径日期：交付=actual_end；里程碑=reached_at/due_date；门=decided_at）。
type MetricsDateWindow struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// MetricsFilters 项目范围过滤回显（与列表同参数）。
type MetricsFilters struct {
	Status      string `json:"status,omitempty"`
	Type        string `json:"type,omitempty"`
	ManagerID   int    `json:"managerId,omitempty"`
	OwnerTeamID int    `json:"ownerTeamId,omitempty"`
	Keyword     string `json:"q,omitempty"`
}

// MetricsProjectScope 度量范围（项目数 + 过滤回显 + 聚合上限截断标记）。
type MetricsProjectScope struct {
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
	Filters   MetricsFilters `json:"filters"`
}

// DeliveryDetail 交付明细行（dateUsed=actual_end；逐条与汇总勾稽）。
type DeliveryDetail struct {
	ProjectID int       `json:"projectId"`
	Code      string    `json:"code,omitempty"`
	Name      string    `json:"name"`
	PlanEnd   time.Time `json:"planEnd"`
	ActualEnd time.Time `json:"actualEnd"`
	OnTime    bool      `json:"onTime"`
}

// DeliveryMetrics 按时交付率（den=已完成且有计划结束日；noPlanDate 单列不掩盖）。
type DeliveryMetrics struct {
	Denominator  int              `json:"denominator"`
	Numerator    int              `json:"numerator"`
	Rate         *float64         `json:"rate"`
	NoPlanDate   int              `json:"noPlanDate"`
	DetailsTotal int              `json:"detailsTotal"`
	Details      []DeliveryDetail `json:"details"`
}

// MilestoneDetail 里程碑明细行（dateUsed：reached→reached_at；missed/逾期 planned→due_date）。
type MilestoneDetail struct {
	ProjectID   int        `json:"projectId"`
	MilestoneID int        `json:"milestoneId"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	DueDate     time.Time  `json:"dueDate"`
	ReachedAt   *time.Time `json:"reachedAt,omitempty"`
	Achieved    bool       `json:"achieved"`
	OnTime      bool       `json:"onTime"`
	DateUsed    time.Time  `json:"dateUsed"`
}

// MilestoneMetrics 里程碑达成率（judged=已判定；onTimeRate 以 reached 为分母）。
type MilestoneMetrics struct {
	Judged        int               `json:"judged"`
	Reached       int               `json:"reached"`
	Missed        int               `json:"missed"`
	OnTimeReached int               `json:"onTimeReached"`
	Rate          *float64          `json:"rate"`
	OnTimeRate    *float64          `json:"onTimeRate"`
	DetailsTotal  int               `json:"detailsTotal"`
	Details       []MilestoneDetail `json:"details"`
}

// GateDetail 阶段门明细行（firstPass 勾稽字段；rejectCount=0 且 passed 为一次通过）。
type GateDetail struct {
	ProjectID   int    `json:"projectId"`
	Gate        string `json:"gate"`
	Status      string `json:"status"`
	RejectCount int    `json:"rejectCount"`
	FirstPass   bool   `json:"firstPass"`
	// AutoApproved 未配置审批流程的自动放行（B12：剔除出一次通过率，单独计数）。
	AutoApproved bool       `json:"autoApproved"`
	DecidedAt    *time.Time `json:"decidedAt,omitempty"`
}

// GateMetrics 阶段门一次通过率（B12 口径）：
//   - 分母 passed 仅统计**人工审批**通过的门；
//   - 分子 = passed ∧ reject_count=0（一次通过）；
//   - 自动放行（auto_approved_no_flow）单独计 autoApproved，不进入分子/分母。
type GateMetrics struct {
	Passed       int          `json:"passed"`
	FirstPass    int          `json:"firstPass"`
	AutoApproved int          `json:"autoApproved"`
	Rate         *float64     `json:"rate"`
	DetailsTotal int          `json:"detailsTotal"`
	Details      []GateDetail `json:"details"`
}

// ResourceUnplanned 未排期提示（有负责人+估算但无计划日期；与热图一致，不进分母）。
type ResourceUnplanned struct {
	TaskCount     int     `json:"taskCount"`
	EstimateHours float64 `json:"estimateHours"`
}

// ResourceWindow 资源窗口（ISO 周对齐；周数 1..12）。
type ResourceWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Weeks int    `json:"weeks"`
}

// ResourceProjectRow 项目级资源行（planned=估算自然日均摊；capacity=Σ存活成员容量×weeks）。
type ResourceProjectRow struct {
	ProjectID     int      `json:"projectId"`
	Code          string   `json:"code,omitempty"`
	Name          string   `json:"name"`
	PlannedHours  float64  `json:"plannedHours"`
	CapacityHours float64  `json:"capacityHours"`
	Rate          *float64 `json:"rate"`
}

// ResourceUtilization 资源利用率（复用 PE-23 热图口径；D-39/D-41）。
type ResourceUtilization struct {
	PlannedHours  float64              `json:"plannedHours"`
	CapacityHours float64              `json:"capacityHours"`
	Rate          *float64             `json:"rate"`
	Unplanned     ResourceUnplanned    `json:"unplanned"`
	Calendar      HeatmapCalendar      `json:"calendar"`
	Window        ResourceWindow       `json:"window"`
	Projects      []ResourceProjectRow `json:"projects"`
}

// MetricsResponse 组合度量响应（四项指标 + 明细，支撑抽样对账）。
type MetricsResponse struct {
	GeneratedAt          time.Time           `json:"generatedAt"`
	ProjectScope         MetricsProjectScope `json:"projectScope"`
	Window               MetricsDateWindow   `json:"window"`
	OnTimeDelivery       DeliveryMetrics     `json:"onTimeDelivery"`
	MilestoneAchievement MilestoneMetrics    `json:"milestoneAchievement"`
	GateFirstPass        GateMetrics         `json:"gateFirstPass"`
	ResourceUtilization  ResourceUtilization `json:"resourceUtilization"`
}

// --- 计算辅助 ---

// rateOrNil 比率（分母 0 → nil，避免"全部不准时"误读）；保留 4 位小数。
func rateOrNil(numerator, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := round4(float64(numerator) / float64(denominator))
	return &v
}

// ratioOrNil 浮点比率（容量 0 → nil）。
func ratioOrNil(numerator, denominator float64) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := round4(numerator / denominator)
	return &v
}

// round4 四舍五入到 4 位小数。
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// portfolioBuckets 维度计数 → 稳定排序桶（count 降序、key 升序）。
func portfolioBuckets(rows []*ent.Project, pick func(*ent.Project) string) []PortfolioBucket {
	counts := make(map[string]int, len(rows))
	for _, p := range rows {
		counts[pick(p)]++
	}
	out := make([]PortfolioBucket, 0, len(counts))
	for k, c := range counts {
		out = append(out, PortfolioBucket{Key: k, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// intKey 整数维度键（0=未设置）。
func intKey(id int) string {
	return strconv.Itoa(id)
}

// inDateWindow 口径日期是否命中 [from,to]（nil 端点 = 不限；UTC 日粒度）。
func inDateWindow(d time.Time, from, to *time.Time) bool {
	if d.IsZero() {
		return false
	}
	day := dayUTC(d)
	if from != nil && day.Before(dayUTC(*from)) {
		return false
	}
	if to != nil && day.After(dayUTC(*to)) {
		return false
	}
	return true
}
