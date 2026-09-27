package ai

import (
	"time"
)

// Conversation represents a chat session with AI
type Conversation struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	UserID    int       `json:"userId"`
	TenantID  int       `json:"tenantId"`
	CreatedAt time.Time `json:"createdAt"`
}

// Message represents a single message in a conversation
type Message struct {
	ID             int       `json:"id"`
	ConversationID int       `json:"conversationId"`
	Role           string    `json:"role"` // user, assistant, system
	Content        string    `json:"content"`
	RequestID      string    `json:"requestId"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ToolInvocation represents an AI tool execution
type ToolInvocation struct {
	ID             int        `json:"id"`
	TenantID       int        `json:"tenantId"`
	ConversationID int        `json:"conversationId"`
	ToolName       string     `json:"toolName"`
	Arguments      string     `json:"arguments"` // JSON string
	Status         string     `json:"status"`    // pending, running, completed, failed
	Result         *string    `json:"result"`
	Error          *string    `json:"error"`
	NeedsApproval  bool       `json:"needsApproval"`
	ApprovalState  string     `json:"approvalState"` // pending, approved, rejected
	ApprovedBy     int        `json:"approvedBy"`
	ApprovalReason string     `json:"approvalReason"`
	ApprovedAt     *time.Time `json:"approvedAt"`
	RequestID      string     `json:"requestId"`
	CreatedAt      time.Time  `json:"createdAt"`
	// P2-6 AI 工具 RBAC 校验审计字段
	UserID           int    `json:"userId"`
	PermissionCheck  string `json:"permissionCheck"`  // passed|denied|skipped
	PermissionReason string `json:"permissionReason"` // 校验/拒绝原因
	RoleSnapshot     string `json:"roleSnapshot"`     // 调用时角色快照
	// M0-11 外部工具审计字段（MCP 三元组 + 脱敏入参 + 摘要 + 耗时 + 稳定错误码）
	Provider        string `json:"provider"`        // builtin|mcp
	McpServerName   string `json:"mcpServerName"`   // provider=mcp 时：服务器名
	McpRawToolName  string `json:"mcpRawToolName"`  // provider=mcp 时：原始工具名
	McpCallableName string `json:"mcpCallableName"` // provider=mcp 时：投影名 mcp__<server>__<tool>
	ArgsRedacted    string `json:"argsRedacted"`    // 脱敏入参快照（展示/审计唯一来源）
	OutputSummary   string `json:"outputSummary"`   // 结果摘要（脱敏截断；不落原始 Value）
	DurationMs      int64  `json:"durationMs"`      // 执行耗时（毫秒）
	ErrorCode       string `json:"errorCode"`       // 稳定错误码（与前端展示对齐）
}

// RootCauseAnalysis represents an RCA record for a ticket
type RootCauseAnalysis struct {
	ID              int                      `json:"id"`
	TicketID        int                      `json:"ticketId"`
	TicketNumber    string                   `json:"ticketNumber"`
	TicketTitle     string                   `json:"ticketTitle"`
	AnalysisDate    string                   `json:"analysisDate"`
	RootCauses      []map[string]interface{} `json:"rootCauses"`
	AnalysisSummary string                   `json:"analysisSummary"`
	ConfidenceScore float64                  `json:"confidenceScore"`
	AnalysisMethod  string                   `json:"analysisMethod"`
	TenantID        int                      `json:"tenantId"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
}

// AIAnalysisResult stores AI analysis outputs for tickets/incidents.
type AIAnalysisResult struct {
	ID              int       `json:"id"`
	TenantID        int       `json:"tenantId"`
	UserID          int       `json:"userId"`
	AnalysisType    string    `json:"analysisType"` // triage|summary|rca|deep_analytics|trend_prediction|incident_impact
	TicketID        int       `json:"ticketId,omitempty"`
	IncidentID      int       `json:"incidentId,omitempty"`
	TicketNumber    string    `json:"ticketNumber,omitempty"`
	TicketTitle     string    `json:"ticketTitle,omitempty"`
	RequestPrompt   string    `json:"requestPrompt"`
	ResultJSON      string    `json:"resultJson"`
	Model           string    `json:"model,omitempty"`
	LatencyMs       int       `json:"latencyMs,omitempty"`
	TotalTokens     int       `json:"totalTokens,omitempty"`
	CostUSD         float64   `json:"costUsd,omitempty"`
	ConfidenceScore float64   `json:"confidenceScore,omitempty"`
	Degraded        bool      `json:"degraded"`
	CreatedAt       time.Time `json:"createdAt"`
}
