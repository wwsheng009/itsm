# M1-10 证据（M1 流程验收）

> 验收项：A1-10（对应任务 M1-10，里程碑级）｜DoD：`flow_verified`
> 实际状态：**`flow_verified`**（写路径主链路 + 失败注入 + 前端审批/审计交互全绿；浏览器级 E2E 按计划归 M2-05）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，Node/jest/tsc；基线分支 `feat/bot-mcp-integration`
> 时间：2026-09-27

## 1. 验收范围与证据映射

| 验收内容（§4.2 M1-10） | 执行方式 | 证据 |
| --- | --- | --- |
| 写路径 E2E：对话入口 → 待审批 → 审批 → 执行 → 回填 → 审计可查 | 真实 ent + 真实 manager + 真实 mock MCP（HTTP/SSE）+ 真实 ToolQueue 的**服务端全链路**；入口 `ai.Service.ExecuteTool`（对话内工具调用的服务端入口，与 chat 路径同源） | `tests/mcpintegration/m1_write_approval_test.go:74`（M1-02 主链路，含来源三元组/脱敏快照/参数冻结/重复审批防护）+ `mcp_flow_test.go:67`（M0-14 基础流） |
| 失败注入：**超时** | 服务器级 `timeout_ms=1000`（管理 API，下限校验命中）+ 慢工具（2s） | `m1_flow_acceptance_test.go:63`：`failed` + `tool_timeout` + 恰好 1 次调用（写不重试）+ 失败记录仍脱敏 |
| 失败注入：**拒绝** | 审批 `approved=false` | `m1_flow_acceptance_test.go:141`：`rejected`、原因与决策人留痕、零执行 |
| 失败注入：**过期**（等价语义） | 记录不可得 + 终态保护（后端无过期状态机，前端卡片对 unknown/expired 只读，见 M1-05） | 同上：不存在记录审批失败；已终态记录再审批 → `ErrInvocationNotPending` |
| 失败注入：**服务器下线** | 提交审批后 `DisableServer` 再审批 | 同上 `TestM1Flow_FailureInjection_ServerOffline`：`failed` + `tool_not_found` + **零下游调用** + 工具面同步收缩 |
| 审批页 / 审计页交互用例 | jest 组件套件 | `src/pages/(main)/ai/approval/__tests__/index.test.tsx`、`src/pages/(main)/ai/audit/__tests__/index.test.tsx`、`src/components/ai/__tests__/{tool-approval-card,tool-call-timeline}.test.tsx`、`src/pages/(main)/admin/mcp-servers/__tests__/*` |
| 类型与构建 | tsc | `npx tsc --noEmit` exit 0 |

**范围声明**：Playwright 浏览器全链路（管理页流程 + 时间线 + 审批流）由实施方案 §4.3 **M2-05** 承担（依赖 M0-12、M1-04～06、R3），本任务的 `flow_verified` 以「服务端全链路 E2E + 前端组件交互用例 + 类型检查」为判据，浏览器级证据在 M2-05 归档。

## 2. 执行记录

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./tests/mcpintegration/ -count=1 -timeout 300s`（连跑 2 次） | **ok 13.2s / ok 19.3s**（11 个用例：M0-14 3 个 + M1 注释 4 个 + M1 写审批 2 个 + M1 流程验收 3 个） |
| 2 | `go test ./tests/mcpintegration/ -run TestM1Flow -v` | 3 个失败注入用例 **PASS**（超时 3.25s / 下线 0.90s / 拒绝+终态 0.78s） |
| 3 | `go test ./service/ ./handlers/ai/ -count=1` | **ok**（service 328s / handlers/ai 17s；覆盖本次 service 层错误码改动） |
| 4 | `npx jest --testPathPattern "(ai/approval\|ai/audit\|tool-approval-card\|tool-call-timeline\|admin/mcp-servers)"` | **6 suites / 37 tests 全 PASS**（含审批页 5、审计页 2、审批卡片、时间线、治理页） |
| 5 | `npx tsc --noEmit` | exit 0 |

## 3. 本次发现与修复

### D-2（已修复）审批后目标工具消失 → 审计错误码退化为 `internal_error`

- **现象**：提交审批后禁用服务器（或工具被停用/隔离），审批放行、执行 fail-closed，但 `tool_invocations.error_code` 落成 `internal_error`，运维无法据此定位「工具已不可用」。
- **根因**：① `ToolRegistry.ExecuteApprovedWrite` 的未知工具分支返回裸 `fmt.Errorf`；② 概率更高的路径：`ToolQueue` 在 `HasProviderTool=false` 时直接落入内置分支，最终以通用错误收尾；两者都不带 `ErrorCode()`，被 `tool_queue.errorCodeOf` 兜底成 `internal_error`。
- **修复**：`service/tool_provider.go` 新增稳定错误 `ToolExecutionError{Code,Message}`（实现 `ErrorCode()`）与常量 `ErrorCodeToolNotFound`/`ErrorCodeNotSupported`；`ToolRegistry.ExecuteApprovedWrite` 两个分支改用之；`ToolQueue` 在 `inv.Provider=="mcp"` 且工具已不可解析时**前置守卫**，直接落 `tool_not_found` 并保持零下游调用。
- **验证**：`TestM1Flow_FailureInjection_ServerOffline` 从 `internal_error` 转为断言 `tool_not_found` PASS；`go test ./service/` 全绿。

### O-1（观察项，未改产品行为）治理位翻转与异步工具重发现存在竞争窗口

- **现象**：`UpdateServer`（如改超时）触发重连/重发现为**异步**写入工具缓存；若随后立刻 `SetToolEnabled`，治理位可能被在途缓存写覆盖（观测到 `enabled` 回退 → 工具不可解析），表现为偶发「未知工具」。
- **测试处置**：用例改为「先改配置 → 等工具回到工具面 → 开治理位 → 有界重试直到解析可见」（幂等重放，20s 上限并给出诊断信息），两次全量连跑稳定。
- **产品影响**：管理页在「刚保存服务器配置后立即启用工具」的窄窗口内可能提示失败（刷新后可重试成功）；**不造成越权**（方向是更严格）。建议 M2 加固：工具缓存写入时保留既有治理位（`enabled/risk/quarantine`），或治理位变更后主动重载一次。

### O-2（观察项，测试基础设施）mock HTTP 关闭可能被长连接阻塞

- **现象**：某次配置下 `httptest.Server.Close` 打印 `blocked in Close ... waiting for connections`，测试挂到包超时。
- **处置**：本任务用例不再依赖「配置重载期间」的连接；用例通过 `provisionMockServer` 的 track 机制在收尾时先关会话。已登记的缓解：M2-05 浏览器 E2E 的夹具应显式「先禁服务器（关会话）→ 再关 mock」，避免定时器/长连接泄漏（jest 侧同类告警：mcp-servers 套件运行后有 worker 未优雅退出的既有告警）。

## 4. 里程碑判定（M1）

| 维度 | 结果 |
| --- | --- |
| 写路径闭环（对话入口→审批→执行→回填→审计） | 通过（服务端全链路，真实 mock MCP over HTTP/SSE） |
| 失败注入（超时/拒绝/过期等价/下线） | 4/4 通过，错误码精确、零副作用、审计可查 |
| 前端交互（审批页/审计页/卡片/时间线/治理页） | 6 suites / 37 tests 通过；tsc 干净 |
| 安全负向 | M1-09 12+2 项全通过（独立报告） |
| 运维收口 | M1-08 6 用例 + 策略回读通过（独立证据） |

**结论：M1 达 `flow_verified`**（A1-10）。浏览器级 E2E 与 accepted 复核分别由 M2-05、M2-07 承接。

## 5. 已知缺口

| # | 缺口 | 归属 |
| --- | --- | --- |
| 1 | 浏览器 E2E（截图/SSE 抓包/真实点击流） | M2-05（A2-05） |
| 2 | 治理位与重发现的竞争窗口（O-1） | M2 加固候选（建议并入 M2-03 或单列） |
| 3 | 后端无「审批过期」状态机（前端按 unknown/expired 只读降级） | 阶段一 G3 待评估；当前语义已由 M1-05 固化 |
| 4 | 审批/审计页筛选未写 URL query、invocation 无深链 | 建议单开小任务（前端体验项，不阻塞流程） |
