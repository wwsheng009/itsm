# B1-05 实施证据（确认状态机：过期 / 幂等回放 / 乐观锁）

> 文档类型：实施证据（任务 B1-05）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-05（ToolInvocation 确认状态机；依赖 B0-05、BP4）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-05、§5.2）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（A1-02 写路径契约）
> 核查方式：真实 ent/SQLite + 真实 `ApproveTool`/扫描器（无 mock 状态机）；配置用例；工单参数映射与错误码用例

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-05 | **`integration_verified`** | 五态确认状态机落地（`pending/confirmed/rejected/expired/cancelled`）；TTL 过期（惰性 + 周期扫描）；同人同向重复确认 = **幂等回放**（零二次执行）；异人/改判 = **冲突**；`update_ticket` 支持 `expected_version` 乐观锁，冲突落稳定错误码 `tool_version_conflict` |
| 兼容 | ✅ | 持久化仍用既有 `approval_state` 取值（`approved` 等），新增 `confirmationState` 与 `expiresAt` 字段**附加返回**；MCP A1-02 的「不二次执行」不变量保持不变（仅把「同人同向重试」从 409 改为幂等回放） |
| 遗留 | 2 项 | ①`cancelled` 状态已定义并预留，尚无触发点（会话/运行取消接线归 B1-06/后续）；②四眼原则（`act_high` 强制分离）依赖 BQ2 的人员矩阵，本轮未启用 |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/service/bot/confirmation.go`（新建） | 状态常量与归一化 `NormalizeConfirmationState`（`approved`→`confirmed` 兼容映射）、`IsDecidable`/`IsTerminal`、`IsExpiredAt`、决策语义 `EvaluateDecision`（apply/replay/conflict） |
| 2 | `itsm-backend/service/bot/confirmation_sweeper.go`（新建） | 纯逻辑扫描引擎 `SweepOnce` + 周期运行器 `Sweeper`（含观测钩子），存储经 `ConfirmationStore` 接口解耦 |
| 3 | `itsm-backend/handlers/ai/confirmation_store.go`（新建） | ent 实现：`ListExpirable`（pending ∧ expires_at < now）与条件置位 `Expire`（仅 pending→expired，幂等）；`Service.SweepExpiredConfirmations` |
| 4 | `itsm-backend/handlers/ai/service.go`（修改） | `SetConfirmationTTL`/`newConfirmationExpiry`；pending 创建落 `expires_at`；`ApproveTool` 改为状态机驱动（惰性过期 → expired + 回填；replay → 返回既有决策；conflict → `ErrInvocationStateConflict`） |
| 5 | `itsm-backend/handlers/ai/handler.go`（修改） | 审批接口错误分层新增 `invocation_expired` / `invocation_state_conflict`（均 409）；列表/详情附加 `confirmationState`、`expiresAt` |
| 6 | `itsm-backend/handlers/ai/tool_events.go`（修改） | 错误码映射：过期/冲突/版本冲突（`tool_version_conflict`） |
| 7 | `itsm-backend/service/tool_queue.go`（修改） | 队列审计错误码同步识别版本冲突 |
| 8 | `itsm-backend/service/tool_registry.go`（修改） | `update_ticket` 入参 schema 补全（含 `expected_version`）；抽 `updateTicketRequestFromArgs` 纯映射函数并映射乐观锁版本号 |
| 9 | `itsm-backend/config/config.go`（修改） | `bot.confirmation_ttl_hours`（默认 24，环境变量 `BOT_CONFIRMATION_TTL_HOURS`，非正值回落默认） |
| 10 | `itsm-backend/internal/bootstrap/app.go`（修改） | bot 开启时注入 TTL 并启动过期扫描（10min 周期） |
| 11 | 测试 | `service/bot/confirmation_test.go`（状态矩阵/幂等/扫描 7 函数）、`service/tool_registry_args_test.go`（映射 + 错误码）、`tests/botintegration/b1_confirmation_test.go`（4 个端到端）、`config/config_bot_test.go`（+2）；`tests/mcpintegration/m1_write_approval_test.go` 更新重复审批语义 |

## 3. 状态机与判定口径

```text
pending ──confirm(同人)──► confirmed（执行由队列负责；可回放）
   │  └─reject(同人)─────► rejected（可回放）
   ├─TTL 到期（惰性/扫描）─► expired（终态；仅可重新发起）
   └─（预留）取消────────► cancelled（终态）

再次确认的语义（EvaluateDecision）：
  pending            → apply（正常落决策）
  同人 + 同向        → replay（返回既有决策；零二次执行、零改库）
  异人 / 改判 / 终态 → conflict（409，不静默回放）
```

**为何「同人同向」等价幂等键**：客户端重试必然是同一个人重复同一动作；此口径无需新增请求头即可覆盖网络抖动场景，同时把「换人补批」「改判」保留为可见冲突（安全语义不降级）。

## 4. 运行记录（本机，pwsh）

```text
go test ./service/bot/ -run 'TestNormalize|TestConfirmationState|TestEvaluateDecision|TestIsExpiredAt|TestSweepOnce|TestSweeper'
→ ok（7 个测试函数，含 10+9 表驱动子例）
go test ./service/ -run 'TestUpdateTicketRequestFromArgs|TestErrorCodeOf_VersionConflict' → ok
go test ./tests/botintegration/ -run 'TestB1Confirmation' -v
→ PASS ×4（过期拒批 / 扫描置位幂等 / 回放与冲突 / TTL 关闭不设期限）
go test ./tests/mcpintegration/ -run 'TestM1WriteApproval_EndToEnd' → PASS（重复审批=回放，仍 1 次执行）
go test ./config/ -run 'TestLoadConfig_Bot' → ok
go build ./... → exit 0；go vet（6 包）→ exit 0；gofumpt -l → 无输出
```

**回归观察（与本任务无关的既有抖动，登记备查）**：全量回归时 `itsm-backend/service` 包的
`TestBiz_ListProcessInstancesByTenant` 偶发失败（整包运行时），单独运行（`-run` 精确匹配）稳定通过；
该用例不涉及本任务改动的任何代码路径（流程实例列表），且在同日 B1-03 的同类整包回归中已出现过同一现象 → 判为**既有测试互相污染/时序抖动**，不属 B1-05 引入；建议后续单开任务排查（进程实例测试与同包其它用例的共享状态）。

## 5. 与既有契约的关系（重要变更登记）

| 契约点 | 变更 | 影响面 |
| --- | --- | --- |
| 重复审批（同人同向） | 由 `409 该审批已处理` → **200 + 回放既有决策** | MCP A1-02「重复审批被拒」用例已同步为「幂等回放 + 仍不二次执行」；前端审批页对非 pending 记录本就只读，无 UI 回归 |
| 重复审批（异人/改判） | 新增 `ErrInvocationStateConflict`（409）——**包装** `ErrInvocationNotPending` | 语义更显式：原先一律「已处理」，现区分「你无权覆盖他人决策」；老调用方用 `errors.Is(err, ErrInvocationNotPending)` 判定仍成立（MCP 终态防护用例零改动通过） |
| 过期单 | 新增惰性拒批 + 周期置位（原无任何过期语义） | 审批页展示 `expiresAt`；过期后需重新发起 |
| `update_ticket` | 新增可选 `expected_version`（乐观锁） | 未传参数时行为完全不变；传错版本 → `tool_version_conflict`，提示刷新 |

**关于生效开关（明确登记）**：TTL 与周期扫描仅在 `bot.enabled=true` 时注入（bootstrap 同一分支），
关闭态保持既有「不设期限」行为 —— 与 BP5/B0/B1 全线的「默认关闭 = 零行为变化」硬约束一致；
MCP 线共用同一确认表，开启 Bot 后 MCP 待审批记录同样受 TTL 保护（两条线共用一套过期语义）。

## 6. 遗留与归属

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | `cancelled` 触发点（会话删除/运行取消时级联取消 pending） | B1-06 / 后续 |
| 2 | 四眼原则（`act_high` 强制分离决策人） | 依赖 BQ2 人员矩阵；当前仅保留冲突可见性 |
| 3 | 前端确认抽屉（倒计时/差异摘要/拒绝原因） | B1-07 |
| 4 | 过期扫描的生产观测（指标而非仅日志） | 与 M2-06 指标面统一（`itsm_mcp_*` 同类） |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-05 交付：五态状态机 + TTL（惰性 + 扫描）+ 幂等回放 + 冲突语义 + `expected_version` 乐观锁；测试 4+2+7+1 组全绿，判定 `integration_verified` |
