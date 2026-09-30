# B3-01 实施证据（entrypoint 枚举与上下文协议）

> 文档类型：实施证据（任务 B3-01）
> Status: draft
> 编制日期：2026-09-27
> 任务：B3-01（entrypoint 枚举与上下文协议 + `ScopeResolver` + 权限预检 G9）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B3-01、§5.2 AB3-01）
> 核查方式：Go 单元 + 集成测试（ent/sqlite 真库 + 探针 provider）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B3-01 | **`integration_verified`** | 入口枚举 + 上下文协议落地；`ScopeResolver` 归一化与**目标对象预检 fail-closed**；入口值贯通下发面/执行面/运行记录 |
| AB3-01 | ✅ | UT（伪造 target 被拒/覆写并审计）+ 集成（入口上下文进入 run 记录）双覆盖 |
| 兼容默认 | ✅ | 未携带入口/目标的既有请求：入口缺省 `chat`、参数形状不变、关闭态行为零差异（单测锁定） |

## 2. 交付物

| # | 文件 | 内容 |
| --- | --- | --- |
| 1 | `itsm-backend/service/bot/scope.go`（新建） | 入口枚举（`chat`/`ticket_detail`/`ticket_list`/`incident_detail`/`incident_create`/`ci_detail`）+ 目标类型（`ticket`/`incident`/`ci`）+ `Scope`/`ScopeInput` + `ScopeResolver.Resolve` + 五个哨兵错误 + `TargetChecker` 端口 |
| 2 | `itsm-backend/service/bot/run.go` | `StartRunInput` 增 `TargetType`/`TargetID`（成对才落库） |
| 3 | `itsm-backend/ent/schema/bot_run.go` | 新增 `target_type`（默认 ""）/`target_id`（Optional）两列（ent 重新生成） |
| 4 | `itsm-backend/handlers/ai/scope.go`（新建） | 上下文传递（`WithScope`/`ScopeFromContext`/`EntrypointFromContext`）+ `entTargetChecker`（租户内存在性 + 角色读权限）+ `resolveRequestScope`（关闭态 fail-closed）+ `scopeHTTPStatus` 映射 |
| 5 | `itsm-backend/handlers/ai/args_guard.go` | 新增 `reservedScopeArgKeys`（协议键剥离）+ `scopeProtocolKeysStripped` + `injectScopeArgs`（仅当模型尝试使用协议键时以服务端解析值覆写） |
| 6 | `itsm-backend/handlers/ai/service.go` | Gate 2.5 与下发面判定改用 `EntrypointFromContext`；执行入口注入 scope 覆写；运行记录/`run_started` 事件携带 `entrypoint` + `targetType/targetId`；`ScopeResolver()`/`SetScopeResolver()` |
| 7 | `itsm-backend/handlers/ai/handler.go` | `Chat`/`ChatStream` 接受 `entrypoint/targetType/targetId/summary`；解析失败：HTTP 400/404/503（SSE 走统一 `error` 事件 + `errorCode`） |

## 3. 行为契约

### 3.1 入口归一化

| 入参 | 结果 |
| --- | --- |
| 空白 / 缺省 | `chat`（既有行为） |
| 已知枚举（大小写归一） | 原值 |
| 未知值 | `ErrScopeEntrypointUnknown` → HTTP 400 `AI_SCOPE_INVALID`（**不静默降级**） |

### 3.2 目标对象预检（G9，fail-closed）

| 入参 | 结果 |
| --- | --- |
| `targetType`/`targetId` 皆空 | 无目标上下文（正常） |
| 只给一半 | `ErrScopeTargetPairIncomplete` → 400 |
| 未知类型 | `ErrScopeTargetTypeUnknown` → 400 |
| 校验器未注入（关闭态）但携带目标 | `ErrScopeCheckerUnavailable` → 503（**不静默丢弃**） |
| 类型存在但对象不在本租户 / 无读权限 / 不存在 | `ErrScopeTargetDenied` → **404** `AI_SCOPE_TARGET_UNAVAILABLE`（统一表现为"不可用"，不泄露存在性） |

### 3.3 参数覆写与留痕（与 B2-05 参数守卫衔接）

- 模型在工具参数里携带 `target_type`/`target_id`/`entrypoint` 等协议键 → **一律剥离**（`args_stripped:<keys>` 留痕不变）；
- 若模型确实尝试使用协议键**且**请求有服务端解析通过的目标 → 以解析值覆写（`injectScopeArgs`）；
- 模型未使用协议键 → 入参形状不变（对既有工具零影响）。

### 3.4 入口贯通点（单一来源）

| 位置 | 用途 |
| --- | --- |
| `chatToolDecision`（下发面） | 工具可见性按入口过滤 |
| `botPolicy.CheckTool`（执行面 Gate 2.5） | 独立复判（不可只靠下发） |
| `bot_runs.entrypoint` + `run_started` 事件 | 审计回溯「从哪个页面发起」 |
| `bot_runs.target_type/target_id` + `run_started.targetType/targetId` | 审计回溯「针对哪个对象」 |

## 4. 测试与运行记录

| # | 用例 | 覆盖 | 结果 |
| --- | --- | --- | --- |
| 1 | `service/bot/scope_test.go::TestScopeResolver_EntrypointNormalization` | 归一化/未知入口/摘要截断 | ✅ |
| 2 | `service/bot/scope_test.go::TestScopeResolver_TargetPairing` | 成对校验/类型/校验器缺失/透传/拒绝 | ✅ |
| 3 | `service/bot/scope_test.go::TestScopeResolver_ErrorSentinelWrapping` | 哨兵包装（HTTP 映射依赖） | ✅ |
| 4 | `handlers/ai/scope_test.go::TestResolveRequestScope_NilResolverFailClosed` | 关闭态三态 | ✅ |
| 5 | `handlers/ai/scope_test.go::TestScopeHTTPStatusMapping` | 400/404/503/500 映射 | ✅ |
| 6 | `handlers/ai/scope_test.go::TestSanitizeReservedArgs_StripsScopeProtocolKeys` | 协议键剥离 + 业务键保留 | ✅ |
| 7 | `handlers/ai/scope_test.go::TestInjectScopeArgs_ServerResolvedOverwrite` | 覆写三态 | ✅ |
| 8 | `handlers/ai/scope_test.go::TestEntTargetChecker_TenantIsolationAndPermission` | 跨租户/无权限/未知类型（真库） | ✅ |
| 9 | `tests/botintegration/b3_entrypoint_test.go::TestB3Entrypoint_RunRecordPersistsTarget` | `bot_runs` 目标落库 | ✅ |
| 10 | `tests/botintegration/b3_entrypoint_test.go::TestB3Entrypoint_ExecutionFaceFiltering` | 执行面入口过滤 + 审计留痕 | ✅ |
| 11 | 回归 `go test ./ent/schema/ ./handlers/ai/ ./tests/botintegration/` | 迁移/受控方 | 见提交记录 |

> 既有用例适配 1 处：`handlers/ai/bot_security_negative_test.go::TestSanitizeReservedArgs` 移除 `target_id` 业务键断言（该键自 B3-01 起属入口协议键，语义变更已在用例注释说明）；`target_id` 的覆写语义由新增用例覆盖。

## 5. 遗留与边界

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | 入口枚举的前端下拉/路由映射与 launcher 组件 | **B3-02**（前端） |
| 2 | 目标对象的**内容级**权限（如工单内部分类可见性）仍由各工具执行面自行校验；入口预检只保证"对象存在 + 资源可读" | 边界已注释；深化评估列入 B4 |
| 3 | `summary` 仅作提示（截断 300 rune），不参与判定 | 设计口径 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B3-01 交付：入口枚举 + 上下文协议 + `ScopeResolver`（目标预检 fail-closed）+ 参数覆写/留痕 + 运行记录落库；UT/集成 10 组全绿，判定 `integration_verified` |
