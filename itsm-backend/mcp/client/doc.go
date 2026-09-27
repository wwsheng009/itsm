// Package client 封装 MCP 客户端会话：协议版本协商、连接与调用超时、错误分层。
//
// 依赖方向（见 ../README.md）：本包依赖 transport，被 manager 依赖。
// 一期语义：Streamable HTTP 为主、SSE 兼容；协议版本不匹配时置 error 并记录原因，不降级。
//
// 落地任务：M0-04；SDK 版本握手回归见 sdk_handshake_test.go（M0-01 P2 spike）。
// 当前为 M0-01 骨架：仅包声明与文档，无可执行逻辑。
package client
