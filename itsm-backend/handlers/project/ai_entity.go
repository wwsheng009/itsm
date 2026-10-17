package project

import (
	"strings"
	"time"
)

// PE-25 项目 AI 能力（周报摘要 / 风险预警 / 纪要转任务）。
// 权威设计：docs/project-management/plan/p2-ai-design.md（D-44~D-48）。
//
// 本文件只定义词表 / DTO / 事实结构；服务编排见 ai_service.go，LLM 适配器见 ai_adapter.go，
// HTTP 端点见 ai_handler.go。三个端点均不落业务状态（AI 失败不影响主流程）。

// ---------------------------------------------------------------------------
// 词表（受控；前端同源）
// ---------------------------------------------------------------------------

const (
	// AIDegradeAIUnavailable 端口未装配（无 LLM Gateway；合法状态，降级不报错）。
	AIDegradeAIUnavailable = "ai_unavailable"
	// AIDegradeLLMError LLM 调用失败（网络/鉴权/限流等）。
	AIDegradeLLMError = "llm_error"
	// AIDegradeParseError LLM 输出不可解析为约定结构。
	AIDegradeParseError = "parse_error"

	// AIGeneratedByAI 结果来自 LLM。
	AIGeneratedByAI = "ai"
	// AIGeneratedByDeterministic 摘要降级为事实派生（同源数字，不臆造）。
	AIGeneratedByDeterministic = "deterministic"
	// AIGeneratedByRules 风险预警降级为纯规则信号。
	AIGeneratedByRules = "rules"
	// AIGeneratedByNone 纯 AI 能力（纪要转任务）降级为空结果。
	AIGeneratedByNone = "none"
)

const (
	// AIRiskSignalOverdueProject 项目结束日期已过且未终结。
	AIRiskSignalOverdueProject = "overdue_project"
	// AIRiskSignalOverdueTask 未终结任务计划结束已过。
	AIRiskSignalOverdueTask = "overdue_task"
	// AIRiskSignalBlockedTask 任务处于 blocked。
	AIRiskSignalBlockedTask = "blocked_task"
	// AIRiskSignalOverdueMilestone planned 里程碑目标日期已过。
	AIRiskSignalOverdueMilestone = "overdue_milestone"
	// AIRiskSignalHighRisk 高等级未关闭风险。
	AIRiskSignalHighRisk = "high_risk"
	// AIRiskSignalGateRejected 阶段门被拒绝。
	AIRiskSignalGateRejected = "gate_rejected"

	// AISeverityInfo / AISeverityWarning / AISeverityCritical 信号与 AI 预警严重度。
	AISeverityInfo     = "info"
	AISeverityWarning  = "warning"
	AISeverityCritical = "critical"
)

const (
	// AIMaxMinutesChars 纪要正文上限（字符；超限 400，不截断静默）。
	AIMaxMinutesChars = 20000
	// AIProposalLimitDefault 建议条数缺省。
	AIProposalLimitDefault = 10
	// AIProposalLimitMin / AIProposalLimitMax 建议条数收敛区间。
	AIProposalLimitMin = 2
	AIProposalLimitMax = 20

	// aiSignalsPerTypeCap 规则信号每个类型的明细上限（超出只计 total，不静默）。
	aiSignalsPerTypeCap = 5
	// aiWarningsCap AI 判断条目上限。
	aiWarningsCap = 10
	// aiFactsPromptCap 事实文本上限（字符；超出截断并在日志留痕）。
	aiFactsPromptCap = 6000
)

// validAISeverities AI 预警严重度归一（未知 → warning）。
var validAISeverities = map[string]bool{
	AISeverityInfo: true, AISeverityWarning: true, AISeverityCritical: true,
}

// ---------------------------------------------------------------------------
// 事实结构（与 PE-17 报告同源派生，不落库）
// ---------------------------------------------------------------------------

// AIRiskFact AI 输入中的风险摘要行（PE-21 台账；仅取提示词需要的字段）。
type AIRiskFact struct {
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Level  string `json:"level"`
	Status string `json:"status"`
}

// AIProjectFacts AI 输入事实（实时派生；PromptText 为紧凑事实文本）。
type AIProjectFacts struct {
	ProjectID   int
	ProjectName string
	ProjectCode string
	Status      string
	Health      string
	Progress    int
	EndDate     time.Time
	ManagerID   int
	OwnerTeam   string

	TaskStats      ReportTaskStats
	MilestoneStats ReportMilestoneStats
	Gates          []ReportGateItem
	Variance       *ReportVarianceSummary
	Risks          []AIRiskFact

	// Signals 规则信号（确定性口径，D-45）；SignalTotal=截断前全量，SignalsTruncated 不静默。
	Signals          []AIRiskSignal
	SignalTotal      int
	SignalsTruncated bool

	// PromptText 紧凑事实文本（构建时已按 aiFactsPromptCap 截断）。
	PromptText string
}

// ---------------------------------------------------------------------------
// 请求 DTO
// ---------------------------------------------------------------------------

// AISummaryRequest 摘要生成请求（body 可选；cadence 缺省 weekly）。
type AISummaryRequest struct {
	Cadence string `json:"cadence"`
}

// AIMinutesTasksRequest 纪要转任务请求（只出建议，不落库）。
type AIMinutesTasksRequest struct {
	Content string `json:"content"`
	Limit   int    `json:"limit"`
}

// ---------------------------------------------------------------------------
// 响应 DTO
// ---------------------------------------------------------------------------

// AIRiskSignal 规则信号（确定性派生；AI 不可用时同样返回）。
type AIRiskSignal struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	RefType  string `json:"refType,omitempty"`
	RefID    int    `json:"refId,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// AIRiskWarning AI 判断的风险预警条目。
type AIRiskWarning struct {
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion,omitempty"`
}

// AISummaryResponse 摘要响应（降级时 generatedBy=deterministic，summary=事实派生文本）。
type AISummaryResponse struct {
	ProjectID     int       `json:"projectId"`
	Summary       string    `json:"summary"`
	Cadence       string    `json:"cadence"`
	GeneratedBy   string    `json:"generatedBy"`
	Degraded      bool      `json:"degraded"`
	DegradeReason string    `json:"degradeReason,omitempty"`
	Model         string    `json:"model,omitempty"`
	PromptChars   int       `json:"promptChars"`
	GeneratedAt   time.Time `json:"generatedAt"`
}

// AIRiskWarningsResponse 风险预警响应（signals 恒为规则全量；warnings 为 AI 段）。
type AIRiskWarningsResponse struct {
	ProjectID     int             `json:"projectId"`
	Signals       []AIRiskSignal  `json:"signals"`
	SignalTotal   int             `json:"signalTotal"`
	Truncated     bool            `json:"truncated"`
	Warnings      []AIRiskWarning `json:"warnings"`
	GeneratedBy   string          `json:"generatedBy"`
	Degraded      bool            `json:"degraded"`
	DegradeReason string          `json:"degradeReason,omitempty"`
	Model         string          `json:"model,omitempty"`
	GeneratedAt   time.Time       `json:"generatedAt"`
}

// AITaskProposal 纪要转任务建议（不落库；用户勾选后走既有建任务端点）。
type AITaskProposal struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Priority      string   `json:"priority,omitempty"`
	EstimateHours *float64 `json:"estimateHours,omitempty"`
	OwnerHint     string   `json:"ownerHint,omitempty"`
}

// AIMinutesTasksResponse 纪要转任务响应（降级时 proposals 为空 + generatedBy=none）。
type AIMinutesTasksResponse struct {
	ProjectID     int              `json:"projectId"`
	Proposals     []AITaskProposal `json:"proposals"`
	GeneratedBy   string           `json:"generatedBy"`
	Degraded      bool             `json:"degraded"`
	DegradeReason string           `json:"degradeReason,omitempty"`
	Model         string           `json:"model,omitempty"`
	ContentChars  int              `json:"contentChars"`
	GeneratedAt   time.Time        `json:"generatedAt"`
}

// ---------------------------------------------------------------------------
// 归一化辅助（纯函数，便于单测）
// ---------------------------------------------------------------------------

// normalizeAIWarnings 归一 AI 预警条目：丢弃空标题；severity 未知 → warning；上限截断。
func normalizeAIWarnings(items []AIRiskWarning) []AIRiskWarning {
	out := make([]AIRiskWarning, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		severity := strings.TrimSpace(item.Severity)
		if !validAISeverities[severity] {
			severity = AISeverityWarning
		}
		out = append(out, AIRiskWarning{
			Severity:   severity,
			Title:      title,
			Detail:     strings.TrimSpace(item.Detail),
			Suggestion: strings.TrimSpace(item.Suggestion),
		})
		if len(out) >= aiWarningsCap {
			break
		}
	}
	return out
}

// normalizeAITaskProposals 归一任务建议：丢弃空名；priority 收敛到任务词表（未知丢弃）；
// estimateHours 仅保留 >0；上限截断。
func normalizeAITaskProposals(items []AITaskProposal, limit int) []AITaskProposal {
	if limit <= 0 || limit > AIProposalLimitMax {
		limit = AIProposalLimitDefault
	}
	out := make([]AITaskProposal, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		proposal := AITaskProposal{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			OwnerHint:   strings.TrimSpace(item.OwnerHint),
		}
		if priority := strings.TrimSpace(item.Priority); validTaskPriorities[priority] {
			proposal.Priority = priority
		}
		if item.EstimateHours != nil && *item.EstimateHours > 0 {
			hours := *item.EstimateHours
			proposal.EstimateHours = &hours
		}
		out = append(out, proposal)
		if len(out) >= limit {
			break
		}
	}
	return out
}