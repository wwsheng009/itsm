// Package provider 将 MCP 工具聚合为 ToolProvider 并接入既有 ToolRegistry。
//
// 契约（D5）：MCP 工具与内置工具**同源**——同一 Gate1（身份）/ Gate2（域 RBAC）/ Gate3（审批）链路、
// 同一 ToolQueue 与同一 tool_invocations 审计；不扩展 Connector 语义。
//
// 落地任务：M0-09；当前为 M0-01 骨架：仅包声明与文档，无可执行逻辑。
package provider
