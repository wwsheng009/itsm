// Package manager 管理 MCP 服务器连接的生命周期与状态。
//
// 状态语义（D6 三态分离）：
//   - configured_enabled：管理位（管理员开关）；
//   - healthy：运行位（连接健康）；
//   - effective：派生位（configured ∧ healthy ∧ 未隔离）；
//   - quarantined：异常隔离（如 canonical 碰撞、schema_hash 变更复核）。
//
// 职责：断连退避重连、工具面塌缩与恢复、禁用/删除的 in-flight 宽限、异步启停（202 + 状态回读）。
//
// 落地任务：M0-07；当前为 M0-01 骨架：仅包声明与文档，无可执行逻辑。
package manager
