package project

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// PE-22 轻量预算与工时（D-33/D-35；权威定义：
// docs/project-management/plan/p2-budget-hours-design.md §3/§5）。
// ---------------------------------------------------------------------------

// MaxBudgetAmount 预算金额上限（防误录护栏，非财务口径）。
const MaxBudgetAmount = 1e12

// MaxTaskHours 单任务工时上限（防误录护栏；100 万小时）。
const MaxTaskHours = 1e6

// normalizeBudget 校验并归一化预算入参（成对提供，D-33）：
//   - 两者都 nil → (nil, nil)：不修改 / 未设置；
//   - 只给其一 → 400（必须成对）；
//   - amount <0 或 > MaxBudgetAmount → 400；currency trim 后 >8 → 400；
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
	cur := strings.TrimSpace(*currency)
	if len(cur) > 8 {
		return nil, nil, fmt.Errorf("%w: budget currency is too long (max 8)", ErrValidation)
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
}

// ToHoursSummaryResponse 映射汇总（nil → nil）。
func ToHoursSummaryResponse(s *TaskHoursSummary) *HoursSummaryResponse {
	if s == nil {
		return nil
	}
	return &HoursSummaryResponse{
		TaskCount:          s.TaskCount,
		EstimatedTaskCount: s.EstimatedTaskCount,
		ActualTaskCount:    s.ActualTaskCount,
		EstimateHours:      s.EstimateHours,
		ActualHours:        s.ActualHours,
	}
}
