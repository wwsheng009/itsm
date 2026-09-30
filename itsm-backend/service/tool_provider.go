package service

import "context"

// 稳定错误码：写入 tool_invocations.error_code，前端与审计按此展示（不解析错误文本）。
const (
	// ErrorCodeToolNotFound：目标工具当前不可解析（已被停用/隔离，或所属服务器已禁用/删除）。
	ErrorCodeToolNotFound = "tool_not_found"
	// ErrorCodeNotSupported：能力缺失（如 provider 未实现审批后写执行），fail-closed。
	ErrorCodeNotSupported = "not_supported"
	// ErrorCodeMCPDisabled：MCP 能力已在管理后台关闭（mcp.enabled=false）。
	// 与 mcp/provider 的同名常量保持一致；service 不反向依赖 provider。
	ErrorCodeMCPDisabled = "mcp_disabled"
	// ErrorCodeMCPWriteDisabled：外部写工具面已在管理后台关闭（mcp.write_enabled=false）。
	ErrorCodeMCPWriteDisabled = "mcp_write_disabled"
)

// 能力开关拒绝原因的稳定串（与 mcp/provider.ReasonMCP* 一致，用于审计可检索）。
const (
	// ReasonCapabilityMCPDisabled：MCP 总开关关闭。
	ReasonCapabilityMCPDisabled = "capability_disabled:mcp"
	// ReasonCapabilityMCPWriteDisabled：写工具面关闭。
	ReasonCapabilityMCPWriteDisabled = "capability_disabled:mcp_write"
)

// CapabilityGateError 由能力开关原因串构造稳定错误（未知原因回落为工具不可用）。
func CapabilityGateError(reason string) *ToolExecutionError {
	switch reason {
	case ReasonCapabilityMCPDisabled:
		return &ToolExecutionError{Code: ErrorCodeMCPDisabled, Message: "MCP 能力已在管理后台关闭，当前不可执行"}
	case ReasonCapabilityMCPWriteDisabled:
		return &ToolExecutionError{Code: ErrorCodeMCPWriteDisabled, Message: "外部写工具面已在管理后台关闭，当前不可执行"}
	default:
		return &ToolExecutionError{Code: ErrorCodeToolNotFound, Message: "外部工具当前不可用（服务器已禁用/删除，或工具已停用/隔离）"}
	}
}

// ToolExecutionError 是 service 层的稳定错误（实现 errorCoder，供 tool_queue.errorCodeOf 提取错误码）。
//
// 为什么需要：service 不能反向依赖 mcp/provider，但审批后的执行失败必须落**精确**错误码，
// 而不是让「工具已不可用」这类可预期失败退化成 internal_error。
type ToolExecutionError struct {
	Code    string
	Message string
}

func (e *ToolExecutionError) Error() string { return e.Message }

// ErrorCode 实现 tool_queue.errorCoder。
func (e *ToolExecutionError) ErrorCode() string { return e.Code }

// ToolExecution 是一次工具执行的**审计元数据 + 结果**（M0-11）。
//
// 内置工具与外部 provider 都返回本类型：Provider 字段区分来源（builtin|mcp），
// MCP 来源额外携带三元组（ServerName/RawToolName/CallableName）供 tool_invocations 落库；
// DurationMs/ErrorCode 用于审计与前端错误展示对齐；OutputSummary 是落库用的脱敏截断摘要。
//
// 注意：Value 仍按**不可信数据**处理，Summary 由 provider/registry 侧做脱敏与截断，
// 调用方不得把 Value 直接写入审计（审计只能写 OutputSummary）。
type ToolExecution struct {
	Value         interface{}
	Provider      string // builtin|mcp
	ServerName    string // provider=mcp 时：MCP 服务器名
	RawToolName   string // provider=mcp 时：原始工具名
	CallableName  string // provider=mcp 时：投影名 mcp__<server>__<tool>
	DurationMs    int64
	ErrorCode     string
	OutputSummary string
	// Risk/Category: 调用时快照的工具治理元数据（B0-01）——内置工具取自注册表，
	// MCP 工具取自治理标注；落 tool_invocations.risk/category，供审计与审批详情使用。
	Risk     string
	Category string
}

// WriteCapableProvider 由支持「审批通过后执行写工具」的 provider 实现（M1-02）。
//
// 调用方承诺：仅在 Gate3 审批通过（ToolQueue 消费已批准的 tool_invocations）后调用。
// provider 自身不感知审批状态，也**不得**在其它路径暴露写执行入口；写调用一律单次执行
// （不自动重试，避免重复副作用——失败即失败，由审批链路的审计留痕）。
type WriteCapableProvider interface {
	ExecuteApprovedWrite(ctx context.Context, tenantID int, name string, args map[string]interface{}) (*ToolExecution, error)
}

// ProviderNameBuiltin 内置工具的审计 provider 标识（与 tool_invocations.provider 默认值一致）。
const ProviderNameBuiltin = "builtin"

// ToolProvider 是内置工具之外的工具来源（D5：与内置工具**同源**接入 Gate1/Gate2/Gate3、
// ToolQueue 与审计链路；不扩展 Connector 语义）。
//
// 契约（M0-09 起，MCP 为第一个实现）：
//   - provider **不做** Gate2（域 RBAC）与 Gate3（审批）判定——那由 handlers/ai.Service 编排；
//     provider 只负责「按租户可见性组装工具面」「解析名字」「执行」；
//   - ListTools 与 Resolve 必须由**同一投影/解析函数**产出（防展示/执行口径漂移）；
//     解析失败一律 fail-closed（不可解析 = 工具不存在）；
//   - Execute 只服务只读工具（M0-09 一期）；写工具的审批编排在 M1-02 接入，
//     provider 自身不感知审批状态；
//   - provider 的工具必须以 `Resource/Action` 声明 Gate2 资源位（MCP：Resource=mcp，
//     Action=read|write，由治理分类决定）；
//   - 返回值与错误必须按**不可信数据**处理：不得回显内部地址/堆栈，错误信息只带稳定错误码与简短原因。
type ToolProvider interface {
	// ProviderName 返回 provider 标识（审计与展示口径，如 "mcp"）。
	ProviderName() string
	// ListTools 返回该租户当前可暴露的工具（已按租户/治理/健康/schema 过滤）。
	ListTools(ctx context.Context, tenantID int) []ToolDefinition
	// Resolve 解析工具名（含 canonical 与唯一短名）：返回定义与是否可解析。
	Resolve(ctx context.Context, tenantID int, name string) (*ToolDefinition, bool)
	// Execute 执行工具（前置 Gate1/Gate2 由调用方完成）；返回值携带审计元数据。
	Execute(ctx context.Context, tenantID int, name string, args map[string]interface{}) (*ToolExecution, error)
}
