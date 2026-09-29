# B3-07 流程验收证据（B3 里程碑出口）

> 文档类型：实施证据（任务 B3-07 / AB3-07，B3 里程碑出口）
> Status: draft
> 编制日期：2026-09-27
> 任务：B3-07（三入口上下文证据 + 三场景验收单 + 负向断言齐全；依赖 B3-01～B3-06）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B3-07、§5.2 AB3-01～AB3-07、§3.1 B3 出口）
> 核查方式：证据归档评审 + 复跑清单（自动化）+ 条件项登记

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| AB3-07 | ✅ | 三入口上下文证据、三场景验收单、负向断言（不自动变更 / 不自动发布）齐全 |
| B3 里程碑出口 | **`flow_verified`（条件达标）** | 出口①三入口 launcher 与上下文协议 ✅；②入口上下文生效（`run.entrypoint` + `target_type/target_id` 落库）✅；③三场景验收单齐备 ✅；④负向断言齐备 ✅ |
| 未竟项（条件项） | 见 §5 | `create_kb_draft`（S3 写工具）归 B4；真实对话 E2E 与浏览器交互归 B4-01/BT-09；产物读取端点归 B4 |

## 2. AB3-01～AB3-07 逐项判定

| 验收项 | 判据 | 证据 | 判定 |
| --- | --- | --- | --- |
| AB3-01 | 入口枚举与上下文协议落地；伪造 target 被拒或覆写并审计；`run.entrypoint` 记录 | `B3-01-entrypoint-scope-evidence.md`：`ScopeResolver`（未知入口 400、目标预检 fail-closed 404/503）、协议键剥离 + 覆写（`args_stripped` 留痕）、`bot_runs.target_type/target_id` 新列 + `run_started` 事件；10 组用例全绿 | ✅ `integration_verified` |
| AB3-02 | 三处 launcher 可用并携带上下文；无权限时隐藏/置灰 | `B3-02-launcher-evidence.md`：工单/事件/CI 三处接入；契约 12 例 + 组件 5 例；`tsc` 0 错误、`eslint` 0 error；**手工截图归 BT-09** | ✅ `unit_verified` |
| AB3-03 | S1 链路（查询 → 创建确认 → 回填工单号）；SSE 含 `confirmation_required`；≤30s；verify 回读；审计含三元组 | `B3-scenario-pilots-acceptance.md`：场景定义 + 能力就绪（确认/执行/回读由 B1-05/B1-06 交付并各有集成证据）；**真实对话 E2E 归 B4-01** | ✅ `integration_verified`（会话级 E2E 为条件项） |
| AB3-04 | S2 只建议不自动变更（负向断言）；关联 CI 走确认；超时/输出上限生效 | `TestScenarioBots_Definitions`：S2 授权与改级/改状态工具**零交集**；`link_ticket_ci` 为写工具（确认闭环）；plan 工具元数据 30s/256KiB | ✅ `integration_verified` |
| AB3-05 | S3 草稿归属与租户隔离正确；不自动发布（负向断言） | S3 授权与发布类工具零交集；`draft_kb_article` 仅落 `bot_artifacts`（知识实体零写入断言）；产物归属/隔离 4 组用例 | ⚠️ **部分交付（`unit_verified` 级）**：`create_kb_draft` 归 B4 |
| AB3-06 | 计划类工具零业务写入；artifact 归属正确且含证据引用 | `B3-06-plan-tools-artifact-evidence.md`：三件工具 + `bot_artifacts` + 隔离/零写入断言（6 子例） | ✅ `integration_verified` |
| AB3-07 | 三入口上下文证据 + 三场景验收单 + 负向断言齐全 | 本文件 §3/§4 | ✅ |

## 3. 三入口上下文证据（`run.entrypoint` / target 落库）

| 入口 | 前端接入 | 后端解析 | 运行记录 | 证据 |
| --- | --- | --- | --- | --- |
| `ticket_detail` | `TicketDetail.tsx` 页头 launcher（`ticket#id` + 标题） | `ScopeResolver` → `entTargetChecker`（租户内存在 + `ticket:read`） | `bot_runs.entrypoint=ticket_detail`、`target_type=ticket`、`target_id=<id>` | B3-01 §4-9/10；B3-02 §4 |
| `incident_detail` | `incidents/$id` 页头 launcher | 同上（`incident:read`） | 同上（`target_type=incident`） | 同上 |
| `ci_detail` | `CIDetail.tsx` 页头 launcher（+ 名称摘要） | 同上（`ci:read`；`cmdb` 资源读权限） | 同上（`target_type=ci`） | 同上 |

工具面过滤：`chatToolDecision`（下发）与 Gate 2.5（执行）共用 `EntrypointFromContext`——`tests/botintegration/b3_entrypoint_test.go` 断言「同一工具在 `ticket_detail` 可执行、在 `chat` 被拒（`entrypoint_denied`）且审计留痕」。

## 4. 三场景验收单与负向断言

| 场景 | 验收单 | 负向断言（自动化锁定） |
| --- | --- | --- |
| S1 工单助手 | `B3-scenario-pilots-acceptance.md` §4（4 步，待 B4-01 回填） | —（写工具走确认闭环，不适用） |
| S2 事件值班助手 | 同上（3 步） | **不含改级/改状态工具**（授权与 `update_incident`/`declare_major`/`escalate_incident`/`resolve_incident`/`update_ticket` 零交集） |
| S3 知识自助助手 | 同上（2 步） | **不自动发布**（与 `publish_*`/`create_kb_article` 零交集）+ **知识实体零写入**（`draft_kb_article` 只落产物） |

## 5. 条件项与未竟项（不阻断本次出口判定，但需在 B4 收敛）

| # | 项 | 归属 | 说明 |
| --- | --- | --- | --- |
| 1 | `create_kb_draft`（act_low 写工具：创建知识草稿实体并归属当前用户） | **B4** | S3 完整判据；依赖 B3-06 产物 + 写工具审批链 + 草稿租户隔离评审 |
| 2 | 真实对话 E2E（SSE `confirmation_required` → 执行 ≤30s → verify 回读 → 工单号回填；审计含 `conversation_id/target_type/target_id`） | **B4-01** | 需 mock provider 全量 + 少量 real smoke |
| 3 | 浏览器交互与截图（launcher 可见性、上下文条、无权限置灰、管理页） | **BT-09** | 需真实后端 + 登录会话 |
| 4 | 产物读取端点与证据面板消费（`GET /ai/artifacts`） | **B4** | 当前产物仅落库，尚无读取面 |
| 5 | 场景种子在租户开通流程中的自动装配 | 决策待办 | 需产品确认（避免默认助手之外模板静默出现） |
| 6 | O-3/O-4/O-5 既有 flake 与失败用例 | 既有治理 | 均以 A/B 或隔离复跑证明与本系列改动无关 |

## 6. 复跑清单（自动化，可复跑）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./service/bot/ -count=1` | ✅ `ok 11.312s`（含场景种子/产物存储/策略/黑名单） |
| 2 | `go test ./service/ -count=1 -run 'TestToolRegistry\|TestToolMetadata\|TestPlanTools\|TestToolWriteTools'` | ✅ `ok 2.187s` |
| 3 | `go test ./handlers/ai/ -count=1` | ✅ `ok 34.617s` |
| 4 | `go test ./tests/botintegration/ -count=1` | ✅ `ok`（B0/B1/B2/B3 集成面） |
| 5 | `go test ./ent/schema/ -count=1` | ✅ `ok`（迁移，含 `bot_artifacts`/`bot_runs` 新列） |
| 6 | `npx tsc --noEmit`（前端） | ✅ 0 错误 |
| 7 | `npx jest src/components/ai src/lib/ai src/lib/api/__tests__/ai-api.test.ts`（前端） | ✅ 9 套件 / 117 用例 |

## 7. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B3-07 交付：三入口上下文证据 + 三场景验收单 + 负向断言汇总；B3 里程碑判定 `flow_verified`（条件达标：`create_kb_draft`、真实对话 E2E、浏览器截图、产物读取端点归 B4/BT-09） |
