# B0-05 实施证据（幂等键生成、落库与幂等回放）

> 文档类型：实施证据（任务 B0-05）
> Status: draft
> 编制日期：2026-09-27
> 任务：B0-05（幂等键生成与落库）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.1 B0-05、§5.2 AB0-05）
> 核查方式：本机编译 + 定向/全量测试（含真实 SQLite 的唯一索引与回查）

## 1. 结论

| 任务 | 目标级别 | 本轮判定 | 依据 |
| --- | --- | --- | --- |
| B0-05 | `unit_verified` | **`unit_verified`** | 键生成模块（冻结口径 + 6 UT）；执行路径接线（生成键 + 顺序命中回放 + 并发唯一冲突回放，4 例）；真实 SQLite 下唯一索引与按 hash 回查（1 例）。**未达** `flow_verified`：端到端幂等回放属 B0-07 集成验收 |

## 2. 交付物

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 幂等键模块 | `itsm-backend/service/bot/idempotency.go` | `BuildKey`（作用域 = 租户 + 发起人 + 工具 + 目标 + 参数；`v1` 拼接顺序冻结；canonical JSON；sha256 hex）；`TargetFromArgs`；`IsUniqueViolation`；`ErrDuplicateKey`；`KeyVersion` |
| 仓储查询 | `handlers/ai/repository.go` / `repository_impl.go` | 新增 `GetToolInvocationByIdempotencyKey(ctx, tenantID, hash)`：**未命中返回 (nil, nil)**（正常分支），空键直接返回 nil（防误命中读工具记录） |
| 执行路径接线 | `handlers/ai/service.go` | 写工具（`Idempotent=true`）提交前生成键 → **顺序重复**直接回放既有记录（不新增、不重复进审批队列）；创建时 **唯一索引冲突** → 回查命中并回放（`ErrDuplicateKey` 兜底），**不再 500**；无法生成键时 fail-closed（不降级为普通提交） |
| 回放载荷 | 同上（`replayInvocation`） | `{idempotentReplay:true, invocationId, status, approvalState, toolName, result?}`；`result` 为 JSON 时解码回放，否则原样返回 |
| 落库字段 | `ToolInvocation.IdempotencyKeyHash` | 仅存 hash（B0-02 已建唯一索引 `(tenant_id, idempotency_key_hash)`；读工具空串不入列） |

## 3. 测试记录（本机）

| 用例 | 断言要点 | 结果 |
| --- | --- | --- |
| `TestBuildKey_DeterministicAndScoped` | 同输入同键（含 map 键序无关）；租户/用户/工具/参数/目标任一变化即变键 | PASS |
| `TestBuildKey_IsOpaqueHash` | 64 位 hex；不含明文参数与工具名 | PASS |
| `TestBuildKey_NilArgsEqualsEmpty` | nil 与空参等价；`KeyVersion="v1"` 冻结断言 | PASS |
| `TestTargetFromArgs` | ticket/ci/relationship/ticket_type；无目标返回空 | PASS |
| `TestIsUniqueViolation` | SQLite/Postgres 两种措辞命中；无关错误不误判 | PASS |
| `TestB0_05_SequentialDuplicateReplaysFirstRecord` | 命中既有记录：返回首次 ID、**不新增记录**、回放载荷含首次 `result` | PASS |
| `TestB0_05_ParamChangeCreatesNewRecord` | 参数变化 → 新记录、键不同（不误伤正常重提） | PASS |
| `TestB0_05_ScopeIsolationAcrossTenantAndUser` | 跨租户/跨发起人各自建记录（3 键互异） | PASS |
| `TestB0_05_UniqueViolationRaceReplaysExisting` | 并发窗口：预查未命中 → 创建冲突 → 回查命中回放；`creates=1`、记录数不变 | PASS |
| `TestEntRepository_IdempotencyKeyUniqueness`（扩展） | 真实 SQLite：按 hash 回查命中；未命中 (nil,nil)；空键不命中；同租户重复冲突 | PASS |

编译/格式：`gofumpt -l ./handlers/ai ./service/bot` 无输出；`go build ./...` exit 0。

**全量复跑与一次 flake 记录（如实登记）**

1. `go test ./handlers/ai/ ./service/ ./service/bot/ -count=1` 首次：`handlers/ai` ok 16.2s、`service/bot` ok 0.2s，`service` **FAIL** —— `TestBiz_MultipleInstancesIndependent`（`service/bpmn_business_flow_test.go:864`，BPMN 多实例独立性）。
2. 单测复跑该用例：**PASS**（0.45s）；随后整包 `go test ./service/ -count=1` 复跑：**ok 296.3s**。
3. 判定：与本任务无关的**偶发失败**（同一包内重型 BPMN 用例并发/时序抖动；本任务改动仅新增 `service/bot/` 与 `handlers/ai`，不触及 BPMN 与计时器路径）。本记录同时作为既有 flake 线索留档，供 B0-07 集成验收时观察是否复现。

## 4. 设计取舍（登记）

1. **只存 hash**：明文参数不进键、不进日志；键不可逆（UT 断言不含明文）。
2. **冲突即命中**：唯一索引冲突不是错误语义而是「已有同请求」，映射为回放；仅在极端情况（回查也失败）返回 `ErrDuplicateKey`，绝不重试写入。
3. **顺序优先于并发**：常见重复提交被预查拦下（零写入）；并发窗口由唯一索引兜底（一次失败写入 + 回查）。
4. **查询未命中不是错误**：`(nil, nil)` 约定避免调用方把「首次提交」当异常处理。
5. **回放语义为「返回首次状态」**：pending 阶段的重复提交返回同一 pending 记录 ID（不重复进审批队列）；已执行记录的重复提交返回其 `result`。执行结果层面的重放（不重复产生业务副作用）由 B1-05 的确认单冻结 + 执行幂等共同保证。

## 5. 未闭环

- 端到端幂等（HTTP → 审批 → 执行 → 重复提交回放首次结果）归 **B0-07**；
- 回放载荷的前端呈现（提示「该提交已受理/已执行」）归 B1-07；
- `expires_at` 与幂等键的保留策略（过期后是否允许同键重提）待 B1 评审（当前：不主动清理）。

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B0-05 交付证据：键生成模块 + 执行路径接线 + 真实 SQLite 回查；含并发冲突映射与设计取舍登记 |
