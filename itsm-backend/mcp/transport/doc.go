// Package transport 承载 MCP 外部工具的远程传输实现（一期：Streamable HTTP 为主、SSE 兼容）。
//
// 依赖方向（见 ../README.md）：transport ← client ← manager ← admin / provider。
// 本包只负责协议传输与出站安全（SSRF 校验），不感知业务工具语义；
// 错误分层（connect_timeout / tls_error / protocol_mismatch / auth_required …）由本包与 client 共同定义。
//
// 落地任务：M0-04（传输）、M0-05（SSRF，上线硬门槛）。
// 当前为 M0-01 骨架：仅包声明与文档，无可执行逻辑。
package transport
