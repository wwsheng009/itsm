# B2-06 实施证据（B2 集成验收）

> 文档类型：实施证据（任务 B2-06 / AB2-06）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-06（B2 出口证据包：2 角色 × 2 入口下发矩阵 + 审计证据；依赖 B2-02～B2-05）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B2-06、§5.2 AB2-01～AB2-06、§3.1 B2 出口条件）
> 核查方式：Go 集成测试（ent/sqlite 真库 + 探针 provider + gin 路由）+ 逐项证据归档

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| AB2-06（B2 出口） | **`flow_verified`** | 2 角色 × 2 入口 × 7 工具类 = **28 格**逐格断言通过；审计证据（allow/deny 双态）与跨租户 404 HTTP 契约闭环 |
| AB2-01～AB2-05 | ✅ 全过 | 逐项证据见 §4 |
| 兼容默认回归 | ✅ | 未配置授权 = 只读 ∪ 遗留写白名单；黑名单先于兼容默认判定；关闭态（`bot.enabled=false`）零行为变化（B2-02/B2-05 已分别锁定） |
| B2 里程碑出口 | **达成 `flow_verified`** | 出口条件 ①～⑥ 见 §3；浏览器级交互与真实 provider 覆盖归 BT-09/B4-01（**条件项**，见 §6） |

## 2. 验收环境与用例组织

| 项 | 取值 |
| --- | --- |
| 用例文件 | `itsm-backend/tests/botintegration/b2_acceptance_test.go` |
| 数据层 | ent + sqlite（`_fk=1`，临时库），真实租户/用户/模板/授权行 |
| 工具面 | 探针 provider `probe`（7 个工具，名称避开内置注册表，执行不触达真实业务服务） |
| 角色 | `operator_full`（RBAC：`ticket:read` + `ticket:write`） / `viewer_readonly`（仅 `ticket:read`） |
| 入口 | `chat`（模板 entrypoints 内） / `ticket_detail`（不在 entrypoints 内） |
| 判定面 | `bot.Decide`（与生产下发面 `chatToolDecision`、执行面 Gate 2.5 **同源委托**；角色差异经 RBAC 回调表达，生产回调来源 `rbacAllowedFunc`） |
| 权限模式 | `HardcodeOnly`（与 handlers/ai 既有测试同口径） |

## 3. B2 里程碑出口条件核对（§3.1）

| # | 出口条件 | 证据 | 判定 |
| --- | --- | --- | --- |
| ① | 模板/授权两表与管理 API（AB2-01） | `B2-01-template-grants-evidence.md`（8 端点 + 路由门禁 + 级联修复 D-1） | ✅ |
| ② | `BotPolicy` 四重交集生效 + 兼容默认迁移（AB2-02） | `B2-02-policy-evidence.md`（执行面 Gate 2.5 独立复判 + 下发面同源 `Decide`） | ✅ |
| ③ | 管理页 CRUD + 影响面预览（AB2-03） | `B2-03-admin-page-evidence.md`（页面 7 例 + 纯函数 12 例；条件项：手工全流程与截图） | ✅ |
| ④ | 工作区 Bot 切换与会话归属（AB2-04） | `B2-04-bot-selector-evidence.md`（`conversation.bot_id` + 四级优先级 + audience 过滤） | ✅ |
| ⑤ | 授权负向测试全过（AB2-05） | `B2-05-security-negative-evidence.md`（黑名单/跨租户/参数守卫/判定优先级） | ✅ |
| ⑥ | 2 角色 × 2 入口下发矩阵验收（AB2-06） | 本文件 §5 矩阵 28 格 + §5.3 审计证据 + §5.4 HTTP 契约 | ✅ |

## 4. AB2-01～AB2-05 逐项核对

| 验收项 | 判据 | 证据 | 判定 |
| --- | --- | --- | --- |
| AB2-01 | 两表 + 8 端点 + 权限门禁 + 跨租户 404 | B2-01 证据（`admin_test.go`/`service_test.go`/`handler_test.go`） | ✅ |
| AB2-02 | 四重交集；执行面拒绝落 `permission_check=denied` + `permission_reason`；兼容默认逐项等价 | B2-02 证据 + 本文件 §5.3（审计双态） | ✅ |
| AB2-03 | 管理页 CRUD + 影响面预览 | B2-03 证据（页面/纯函数用例） | ✅ |
| AB2-04 | `conversation.bot_id` 归属 + 选择器过滤 + 四级优先级 | B2-04 证据 | ✅ |
| AB2-05 | 未授权不可见/不可调用、跨租户 fail-closed、黑名单、注入覆写 | B2-05 证据（7 组用例） | ✅ |
| AB2-06 | 矩阵 + 审计证据 | 本文件 §5 | ✅ |

## 5. 矩阵与证据（本轮新增，28 格）

### 5.1 授权配置（模板 `matrix-chat`，GA，`riskLimit=act_high`，entrypoints=`["chat"]`）

| 工具 | 类型 | 风险 | 授权上限 | 期望 |
| --- | --- | --- | --- | --- |
| `probe_list_tickets` | 读 | read | read | 授权内放行 |
| `probe_create_note` | 写 | act_low | act_low | 授权内放行（写经 Gate3 审批） |
| `probe_bulk_archive` | 写 | **act_high** | act_low | **超限拒绝**（`risk_exceeded`） |
| `probe_update_note` | 写 | act_low | 未授权 | `tool_not_granted` |
| `delete_probe_item` | 写 | act_low | 未授权 | **黑名单**（`tool_blacklisted:destructive`） |
| `admin_probe_users` | 读 | read | 未授权 | **黑名单**（`tool_blacklisted:admin_surface`） |
| `http_probe_fetch` | 读 | read | 未授权 | **黑名单**（`tool_blacklisted:arbitrary_io`） |

### 5.2 2 角色 × 2 入口矩阵（逐格判定）

| 工具 | operator_full / chat | viewer_readonly / chat | operator_full / ticket_detail | viewer_readonly / ticket_detail |
| --- | --- | --- | --- | --- |
| `probe_list_tickets` | ✅ 可见可调用 | ✅ 可见可调用 | ⛔ `entrypoint_denied` | ⛔ `entrypoint_denied` |
| `probe_create_note` | ✅ 可见可调用 | ⛔ `rbac_denied` | ⛔ `entrypoint_denied` | ⛔ `entrypoint_denied` |
| `probe_bulk_archive` | ⛔ `risk_exceeded` | ⛔ `risk_exceeded` | ⛔ `entrypoint_denied` | ⛔ `entrypoint_denied` |
| `probe_update_note` | ⛔ `tool_not_granted` | ⛔ `tool_not_granted` | ⛔ `entrypoint_denied` | ⛔ `entrypoint_denied` |
| `delete_probe_item` | ⛔ `tool_blacklisted:destructive` | ⛔ 同左 | ⛔ 同左（**黑名单先于入口判定**） | ⛔ 同左 |
| `admin_probe_users` | ⛔ `tool_blacklisted:admin_surface` | ⛔ 同左 | ⛔ 同左 | ⛔ 同左 |
| `http_probe_fetch` | ⛔ `tool_blacklisted:arbitrary_io` | ⛔ 同左 | ⛔ 同左 | ⛔ 同左 |

> 关键不变量：**黑名单在所有判定之前**（与入口/角色/状态无关）；`draft` 模板即使已配授权也一律不下发（`status_draft`，黑名单例外仍先行）；兼容默认（未配置授权）只放行只读 ∪ 遗留写白名单，且 `Legacy=true` 标记在非黑名单分支保持。

### 5.3 审计证据（执行面，真实落库）

| 场景 | 调用 | 审计断言（`tool_invocations`） |
| --- | --- | --- |
| 允许 | `ExecuteTool(probe_list_tickets)` | `status=executed`、`permission_check=passed` |
| 拒绝（未授权） | `ExecuteTool(probe_update_note)` | `permission_check=denied`、`permission_reason` 含 `tool_not_granted` |
| 拒绝（黑名单） | `ExecuteTool(admin_probe_users)` | `permission_check=denied`、`permission_reason` 含 `tool_blacklisted` + `admin_surface` |

### 5.4 HTTP 契约（B2-05 遗留项闭环）

| 场景 | 请求 | 结果 |
| --- | --- | --- |
| 跨租户 `botId` | `POST /api/v1/ai/chat` + 其他租户模板 ID | **404** + `BOT_NOT_FOUND`（不静默降级、不建会话） |

## 6. 条件项与遗留（不阻断 B2 出口）

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | 浏览器级交互（管理页截图、选择器切换、SSE 下的下发面实景） | **B3-02/BT-09**（需真实后端 + 登录会话）；B2 侧已以单测 + 集成矩阵覆盖判定面 |
| 2 | 真实 provider（OpenAI/…) 的工具调用覆盖（本验收全部使用探针 provider） | BT-09/B4-01；登记 U-B5（既有未核实项） |
| 3 | 目标参数（`target_type`/`target_id`）覆写由 `ScopeResolver` 承担 | **B3-01** |
| 4 | G-B2-03-1：路由生成器全量重生成漂移 | 工程债，建议单开对齐任务（B2-03 已登记） |
| 5 | O-3/O-4/O-5：既有不稳定/失败用例（前端 jsdom 渲染、`pkg/seeder` 流程定义、`service` 根包偶发） | 已分别登记；均以 A/B 或单独复跑证明与本系列改动无关 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-06 交付：新增 `tests/botintegration/b2_acceptance_test.go`（28 格矩阵 + draft/兼容默认回归 + 执行面审计双态 + 跨租户 404 契约）；AB2-01～AB2-06 逐项证据归档；B2 里程碑判定 `flow_verified`（条件项：浏览器级与真实 provider 覆盖归 BT-09/B4-01，目标参数覆写归 B3-01） |
