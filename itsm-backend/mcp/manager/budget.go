package manager

import (
	"fmt"
	"strconv"
	"strings"

	"itsm-backend/mcp/budget"
	"itsm-backend/metrics"
)

// M2-03：工具面预算与告警。
//
// 触发信号（与 `mcp/budget` 的口径一致，分析报告 §9.1 第一行）：
//  1. 单租户**有效工具数**（healthy ∧ enabled ∧ ¬quarantined）> 预算（默认 40）；
//  2. 工具面估算 token 占上下文预算比例 > 上限（默认 30% / 128k）。
//
// 触发时机与去重：一次工具发现刷新后自动判定（`discoverWithSession`），
// 也可由管理面/指标层显式调用 `EvaluateToolBudget`；采用**边沿触发**——
// 仅在“进入超限”或“超限期间计数发生变化”时发事件，避免每轮发现都刷告警。
type budgetState struct {
	tools    int
	tokens   int
	exceeded bool
}

// budgetLimits 汇总平台预算阈值（Options 已补默认值）。
func (m *Manager) budgetLimits() budget.Limits {
	return budget.Limits{
		Tools:         m.opts.ToolBudget,
		ContextTokens: m.opts.ContextTokens,
		Share:         m.opts.ToolTokenShare,
	}
}

// tenantBudgetEntries 收集某租户有效工具的工具面估算输入（名称 + 描述 + schema）。
func tenantBudgetEntries(tools []ToolRecord, tenantID int) []budget.Entry {
	entries := make([]budget.Entry, 0, len(tools))
	for _, tool := range tools {
		if tool.TenantID != tenantID {
			continue
		}
		entries = append(entries, budget.Entry{
			Name:        tool.CallableName,
			Description: tool.Description,
			Schema:      tool.InputSchema,
		})
	}
	return entries
}

// ToolBudgetReport 计算某租户的当前工具面预算判定（只读，不发事件）。
func (m *Manager) ToolBudgetReport(tenantID int) budget.Report {
	entries := tenantBudgetEntries(m.EffectiveTools(), tenantID)
	return budget.Evaluate(budget.Measure(entries), m.budgetLimits())
}

// EvaluateToolBudget 判定并（必要时）发出 `mcp.tools.budget_exceeded` 事件，返回判定结果。
//
// 发事件的三种情形：首次超限、超限期间工具数变化、超限期间估算 token 变化（描述/schema 变更）。
// 未超限时仅复位内部状态，不发“恢复”事件（恢复可从 health/事件流的时间序推断）。
func (m *Manager) EvaluateToolBudget(tenantID int) budget.Report {
	report := m.ToolBudgetReport(tenantID)
	// M2-06：工具面规模指标（看板趋势用；超限事件仍走事件流）。
	tenantLabel := strconv.Itoa(tenantID)
	metrics.MCPToolFaceTools.WithLabelValues(tenantLabel).Set(float64(report.Surface.Tools))
	metrics.MCPToolFaceTokens.WithLabelValues(tenantLabel).Set(float64(report.Surface.Tokens))

	m.budgetMu.Lock()
	if m.budgetLast == nil {
		m.budgetLast = map[int]budgetState{}
	}
	previous, seen := m.budgetLast[tenantID]
	m.budgetLast[tenantID] = budgetState{
		tools:    report.Surface.Tools,
		tokens:   report.Surface.Tokens,
		exceeded: report.Exceeded,
	}
	shouldEmit := report.Exceeded &&
		(!seen || !previous.exceeded || previous.tools != report.Surface.Tools || previous.tokens != report.Surface.Tokens)
	m.budgetMu.Unlock()

	if !shouldEmit {
		return report
	}
	m.emit(Event{
		Type:     EventToolsBudgetExceeded,
		TenantID: tenantID,
		Detail: fmt.Sprintf("工具面预算超限（tools=%d tokens=%d，上限 tools=%d share=%.0f%%）：%s",
			report.Surface.Tools, report.Surface.Tokens, report.ToolLimit, report.ShareLimit*100,
			strings.Join(report.Reasons, "；")),
	})
	return report
}
