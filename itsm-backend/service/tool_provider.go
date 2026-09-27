package service

import "context"

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
	// Execute 执行工具（前置 Gate1/Gate2 由调用方完成）。
	Execute(ctx context.Context, tenantID int, name string, args map[string]interface{}) (interface{}, error)
}
