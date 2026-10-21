// Package project 域切片：项目管理。
//
// PE-27 健康度派生与解释（D-54~D-59）；
// 权威设计：docs/project-management/plan/p3-health-design.md §2/§3。
package project

// 健康度级别词表（读时派生；存储列 health 已退役，见 D-54）。
const (
	HealthLevelGreen  = "green"
	HealthLevelYellow = "yellow"
	HealthLevelRed    = "red"
)

// 规则词表：与 D-45 AI 风险信号同名复用（overdue_project/overdue_task/blocked_task/
// overdue_milestone/gate_rejected），其余为健康度专属派生规则（D-55）。
const (
	HealthRuleOverdueProject    = "overdue_project"
	HealthRuleScheduleDeviation = "schedule_deviation"
	HealthRuleOverdueTask       = "overdue_task"
	HealthRuleBlockedTask       = "blocked_task"
	HealthRuleOverdueMilestone  = "overdue_milestone"
	HealthRuleMissedMilestone   = "missed_milestone"
	HealthRuleOpenHighRisk      = "open_high_risk"
	HealthRuleGateRejected      = "gate_rejected"
	HealthRuleBudgetDeviation   = "budget_deviation"
)

// 派生阈值（变更需同步 p3-health-design.md §3）。
const (
	HealthDeviationYellowPP   = 20 // 进度落后 ≥20 个百分点 → yellow
	HealthDeviationRedPP      = 40 // 进度落后 ≥40 个百分点 → red
	HealthOverdueTaskRedCount = 3  // 逾期任务 ≥3 → red
	HealthHighRiskRedCount    = 3  // 未关闭高风险 ≥3 → red
)

// HealthReason 健康度解释项（每条命中规则最多一项；count=明细数）。
type HealthReason struct {
	Rule    string `json:"rule"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
}

// DerivedHealth 派生结果（Level 为命中规则最高级；无命中 = green）。
type DerivedHealth struct {
	Level   string
	Reasons []HealthReason
	// DataQuality 健康度计算的数据完备度（A7）：sufficient=关键事实齐备；insufficient=存在缺口。
	// 与 Level 正交：无风险信号时仍可能因缺基线/缺归属而「无法判断」，UI 需显式标注「数据不足」。
	DataQuality string
	// DataGaps 数据缺口说明（≤4 条，稳定顺序）。
	DataGaps []string
}

// 数据完备度词表（A7）。
const (
	HealthDataSufficient   = "sufficient"
	HealthDataInsufficient = "insufficient"
)

// healthLevelRank 级别序（green<yellow<red）。
func healthLevelRank(level string) int {
	switch level {
	case HealthLevelRed:
		return 2
	case HealthLevelYellow:
		return 1
	default:
		return 0
	}
}

// projectStatusLabels 状态文案（解释文案用；与前端 STATUS_META 对齐）。
var projectStatusLabels = map[string]string{
	StatusPlanning:  "规划中",
	StatusActive:    "进行中",
	StatusSuspended: "已暂停",
	StatusCompleted: "已完成",
	StatusCancelled: "已取消",
}

// gateLabel 阶段门文案。
func gateLabel(gate string) string {
	switch gate {
	case GateInitiation:
		return "立项门"
	case GatePlan:
		return "计划门"
	case GateAcceptance:
		return "验收门"
	case GateClosure:
		return "结项门"
	default:
		return gate
	}
}
