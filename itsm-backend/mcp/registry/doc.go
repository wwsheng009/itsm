// Package registry 实现 MCP 工具的命名投影与解析（纯逻辑包，无 IO）。
//
// 契约（M0-02，最高优先级，先于一切执行链路上线）：
//   - 统一投影 mcp__<server>__<tool>（非法字符替换、FNV-1a 短哈希、超长截断）；
//   - canonical 完全碰撞时后者 quarantine（不暴露、不可执行、可诊断）；
//   - 解析顺序 canonical 精确 → 原始短名唯一 → 多候选 fail-closed（AmbiguousToolError）；
//   - 执行归一化强制以 (server, raw_name) 路由，杜绝串服务。
//
// 落地任务：M0-02。当前为 M0-01 骨架：仅包声明与文档，无可执行逻辑。
package registry
