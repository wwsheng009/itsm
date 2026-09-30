# B2-02 实施证据（BotPolicy 交集门禁）

> 文档类型：实施证据（任务 B2-02）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-02（工具下发/执行前的四重交集：授权 ∩ RBAC ∩ 风险上限 ∩ 入口；依赖 B2-01、B0-01）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.3 B2-02、§5.2 AB2-02）
> 核查方式：策略层表驱动单测 + 真实 ent/SQLite 快照解析 + 执行面（Gate 2.5）集成测试 + 两包全量回归 + `go build ./...`

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B2-02 | **`integration_verified`** | 策略层（`service/bot/policy.go`）四重交集定义完整；**执行面二次校验**接入 `ExecuteTool`（Gate 2.5，拒绝落审计 `permission_check=denied` + `permission_reason`）；**下发面**（ChatStream 工具面）改用同一 `Decide`；兼容默认经两包全量回归证明存量行为不变 |
| 兼容默认 | ✅ | 未配置授权的 Bot（无模板 / 零授权）等价迁移前规则 `只读 ∪ 遗留写白名单`；`bot.enabled=false`（Policy 未注入）时两条路径都保持原实现 |
| 白名单迁移 | ✅ | 单一来源迁至 `bot.LegacyChatWritableTools`，`handlers/ai` 的 `chatWritableTools` 改为其别名（无第二份拷贝） |
| 遗留 | 2 项 | G-B2-02-1：**下发面端到端**（含 SSE 层）断言归 B2-06；G-B2-02-2：会话→Bot 绑定解析归 B2-04（当前缺省 0 = 默认助手） |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/service/bot/policy.go`（新建） | `Policy`（`SnapshotForBot`/`CheckTool`）、纯函数 `Decide(CheckInput)`、`ToolMeta`、`Snapshot`、7 项 `LegacyChatWritableTools`、7 个稳定原因码常量 |
| 2 | `itsm-backend/handlers/ai/service.go`（修改） | ①执行面 Gate 2.5（Gate2 之后、审批之前）；②下发面 ChatStream 工具面改走 `Decide`（快照读取失败 → fail-closed 不下发）；③`SetBotPolicy`/`SetBotIDResolver`/`resolveBotID`/`rbacAllowedFunc`（RBAC 口径单一来源）；④`chatWritableTools` 改为别名 |
| 3 | `itsm-backend/internal/bootstrap/app.go`（修改） | `cfg.Bot.Enabled` 时 `SetBotPolicy(botService.NewPolicy(client))`（默认关闭 = 不注入） |
| 4 | `itsm-backend/service/bot/policy_test.go`（新建） | 兼容默认逐项钉旧规则（含 CMDB 三项）+ 严格交集 13 例 + 快照解析（租户隔离/默认助手/读失败 fail-closed） |
| 5 | `itsm-backend/handlers/ai/service_bot_policy_test.go`（新建） | 执行面 5 例：未授权拒绝 + 审计、已授权走审批、风险超限拒绝、入口/状态拒绝、零授权与关闭态兼容 |

## 3. 判定语义（可审计）

| 原因码 | 触发条件 | 处理 |
| --- | --- | --- |
| `legacy_default` | 无模板 / 模板零授权（未上线策略） | 放行（且 `Legacy=true`），规则=只读 ∪ 遗留写白名单 |
| `status_draft` | 模板 `status=draft` | 拒绝（`pilot`/`ga` 视为已发布） |
| `entrypoint_denied` | 入口不在 `entrypoints_json`；空列表或 JSON 损坏 | 拒绝（fail-closed） |
| `tool_not_granted` | 严格模式下无该工具授权 | 拒绝 |
| `risk_unknown` | 工具风险未标注 | 拒绝（与 B0-01 最保守兜底一致） |
| `risk_exceeded` | 工具风险 > 授权上限；或授权上限 > 模板上限（读侧双保险） | 拒绝 |
| `rbac_denied` | `rbacAllowedFunc` 返回 false（与 Gate 2 同源） | 拒绝 |
| `snapshot_error` | 策略快照读取失败 | 拒绝（执行面）；下发面不下发任何工具 |

**兼容默认的边界（现状等价）**：只读工具有无风险标注都放行（迁移前不看风险）；
非白名单写工具在兼容默认下同样被拒（原因码 `tool_not_granted`，语义等价迁移前的 `continue`）。

## 4. 运行记录（本机，pwsh）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./service/bot/ -run 'TestDecide\|TestPolicy'` | ok 0.68s |
| 2 | `go test ./handlers/ai/ -run 'TestExecuteTool_BotPolicy'` | ok 3.99s（5 例） |
| 3 | `go test ./service/bot/ ./handlers/ai/ -count=1 -timeout 25m`（全量） | **全绿** 6.2s / 9.0s（含存量 RBAC、审批、MCP 审计等回归） |
| 4 | `go build ./...` | exit 0 |

## 5. 本次发现与处置

### D-2（已处置）遗留白名单实际条目多于压缩摘要

- **现象**：实现时依早期摘要按 3 项白名单（`create_ticket`/`update_ticket`/`create_ticket_type`）落 `LegacyChatWritableTools`；补丁定位失败后回读源码，实际为 **7 项**（另有 `link_ticket_ci`、`create_ci_relationship`、`delete_ci_relationship`，见迁移前 `handlers/ai/service.go:827-837`）。
- **处置**：以源码为准补全 7 项；`handlers/ai` 改为别名引用；`policy_test.go` 逐项钉住 7 项 + 非白名单拒绝，后续再增删白名单必须同时过策略测试。
- **教训**：跨会话摘要不得作为迁移依据，涉及安全清单一律回源码核对。

### O-2（运维须知）默认助手加授权 = 全租户聊天工具面切严格模式

- 默认助手（`default-assistant`）零授权时走兼容默认；管理员对它配第一条授权后，**该租户聊天下发面立即切换为严格交集**（未授权工具不可见）。
- 回滚：删除该模板全部授权（回到兼容默认）或关 `bot.enabled`（Policy 不注入，走原实现）。

## 6. 遗留与下一步

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | G-B2-02-1：下发面端到端断言（2 角色 × 2 入口 × 风险矩阵 + SSE 事件） | **B2-06**（任务卡即「2 角色 × 2 入口矩阵 + 审计证据」） |
| 2 | G-B2-02-2：`conversation → bot` 归属落库与解析器替换（当前 `resolveBotID` 缺省 0 = 默认助手） | **B2-04** |
| 3 | 未授权调用的**拒绝持久化举证**（含 `permission_reason` 快照查询入口） | B2-05（负向安全测试集） |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-02 交付：策略层四重交集 + 执行面 Gate 2.5 + 下发面同源判定 + 白名单单一来源迁移（7 项）；策略单测/执行面集成/两包全量回归全绿、`go build ./...` 干净；判定 `integration_verified`（条件项见 §6） |
