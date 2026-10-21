package project

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// PE-22 轻量预算与工时（D-33/D-35；币种口径归一 D-63/PE-28；权威定义：
// docs/project-management/plan/p2-budget-hours-design.md §3/§5）。
// ---------------------------------------------------------------------------

// MaxBudgetAmount 预算金额上限（防误录护栏，非财务口径）。
const MaxBudgetAmount = 1e12

// MaxTaskHours 单任务工时上限（防误录护栏）。
//
// 第三轮回归（B1）：原 100 万小时对「人月级」项目毫无约束力，且越界值被钳制后
// 仍计入项目汇总（实测 1000009h），等于把错误数字当事实。
// 收敛为 10000 小时（约 5.5 人年 @ 8h×250 天），覆盖单任务合理上限；
// 越界一律 400（不静默截断），由前端显式提示。
const MaxTaskHours = 10000

// budgetCurrencyPattern 币种口径（D-63）：2-8 位大写字母（如 CNY / USD）。
var budgetCurrencyPattern = regexp.MustCompile(`^[A-Z]{2,8}$`)

// normalizeBudget 校验并归一化预算入参（成对提供，D-33）：
//   - 两者都 nil → (nil, nil)：不修改 / 未设置；
//   - 只给其一 → 400（必须成对）；
//   - amount <0 或 > MaxBudgetAmount → 400；currency trim + 大写后非空须匹配 ^[A-Z]{2,8}$（D-63）→ 否则 400；
//   - amount <= 0 → 归一化为 0（仓储侧清空为 NULL，语义=未设置预算）。
func normalizeBudget(amount *float64, currency *string) (*float64, *string, error) {
	if amount == nil && currency == nil {
		return nil, nil, nil
	}
	if amount == nil || currency == nil {
		return nil, nil, fmt.Errorf("%w: budget amount and currency must be provided together", ErrValidation)
	}
	if *amount < 0 || *amount > MaxBudgetAmount {
		return nil, nil, fmt.Errorf("%w: budget amount must be within 0..%g", ErrValidation, float64(MaxBudgetAmount))
	}
	cur := strings.ToUpper(strings.TrimSpace(*currency))
	if cur != "" && !budgetCurrencyPattern.MatchString(cur) {
		return nil, nil, fmt.Errorf("%w: budget currency must be 2-8 letters (e.g. CNY)", ErrValidation)
	}
	if *amount <= 0 {
		zero := 0.0
		return &zero, &cur, nil
	}
	return amount, &cur, nil
}

// HoursSummaryResponse 项目任务工时汇总响应（GET /projects/:id/hours-summary）。
type HoursSummaryResponse struct {
	TaskCount          int     `json:"taskCount"`
	EstimatedTaskCount int     `json:"estimatedTaskCount"`
	ActualTaskCount    int     `json:"actualTaskCount"`
	EstimateHours      float64 `json:"estimateHours"`
	ActualHours        float64 `json:"actualHours"`
	// B1（第三轮）：越界工时不计入合计；此处回传被排除的任务数与工时，供 UI 显式告警。
	InvalidHoursTasks    int     `json:"invalidHoursTasks"`
	InvalidEstimateHours float64 `json:"invalidEstimateHours"`
	InvalidActualHours   float64 `json:"invalidActualHours"`
	// MaxTaskHours 当前口径上限（前端据此展示提示，避免与后端漂移）。
	MaxTaskHours float64 `json:"maxTaskHours"`
}

// ToHoursSummaryResponse 映射汇总（nil → nil）。
func ToHoursSummaryResponse(s *TaskHoursSummary) *HoursSummaryResponse {
	if s == nil {
		return nil
	}
	return &HoursSummaryResponse{
		TaskCount:            s.TaskCount,
		EstimatedTaskCount:   s.EstimatedTaskCount,
		ActualTaskCount:      s.ActualTaskCount,
		EstimateHours:        s.EstimateHours,
		ActualHours:          s.ActualHours,
		InvalidHoursTasks:    s.InvalidHoursTasks,
		InvalidEstimateHours: s.InvalidEstimateHours,
		InvalidActualHours:   s.InvalidActualHours,
		MaxTaskHours:         MaxTaskHours,
	}
}
