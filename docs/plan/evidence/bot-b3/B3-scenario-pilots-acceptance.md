# B3-03 / B3-04 / B3-05 场景 pilot 验收单（S1 工单助手 / S2 事件值班助手 / S3 知识自助助手）

> 文档类型：场景验收单（实施证据）
> Status: draft
> 编制日期：2026-09-27
> 任务：B3-03（S1）/ B3-04（S2）/ B3-05（S3，部分交付）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B3-03～B3-05、§5.2 AB3-03～AB3-05）
> 核查方式：种子幂等测试 + 定义边界断言（负向）+ 策略矩阵（下发/执行同源判定）+ 工具注册表核对

## 1. 结论

| 任务 | 判定 | 说明 |
| --- | --- | --- |
| B3-03（S1 工单助手） | **`integration_verified`** | 场景定义落地（入口 3 个 + 5 项授权，含 `create_ticket`(act_low) 与 `draft_ticket_fields`(plan)）；种子幂等；典型链路所需的确认/执行/回读能力已由 B1 系列交付 |
| B3-04（S2 事件值班助手） | **`integration_verified`** | 场景定义落地（入口 3 个 + 5 项授权）；**负向边界**由断言锁定：不含任何改级/改状态工具（只建议，人工执行）；关联 CI 走写工具确认闭环 |
| B3-05（S3 知识自助助手） | **部分交付（`unit_verified`）** | 场景定义落地（`list_kb` + `draft_kb_article`）；**不自动发布**由断言锁定；`create_kb_draft`（写入知识草稿实体）经评审归入 B4（依赖 B3-06 产物 + 写工具审批链），见 §5-1 |
| 真实对话 E2E（SSE 确认→执行→回读、浏览器交互） | **条件项** | 归 **B4-01**（mock provider 全量 + 少量 real smoke）与 **BT-09**（浏览器）；本验收单不替代 E2E |

## 2. 种子定义（`service/bot/scenarios.go`）

| 场景 | Slug | 入口 | 风险上限 | 授权（工具 → 上限） |
| --- | --- | --- | --- | --- |
| S1 工单助手 | `s1-ticket-assistant` | `chat` / `ticket_detail` / `ticket_list` | act_low | `list_tickets`(read)、`get_incident_stats`(read)、`list_kb`(read)、`create_ticket`(act_low)、`draft_ticket_fields`(plan) |
| S2 事件值班助手 | `s2-incident-oncall` | `chat` / `incident_detail` / `incident_create` | act_low | `list_tickets`(read)、`get_incident_stats`(read)、`get_ci_impact`(plan)、`link_ticket_ci`(act_low)、`analyze_ci_impact_plan`(plan) |
| S3 知识自助助手 | `s3-knowledge-assistant` | `chat` / `ci_detail` | act_low | `list_kb`(read)、`draft_kb_article`(plan) |

- 状态：`pilot`（不参与 GA 严格交集，按兼容默认下发；上线由管理员显式改 `ga`）。
- 幂等：`SeedScenarioBots` **只增不改**——已存在模板不覆盖字段，仅补齐缺失授权（管理员修改优先）。

## 3. 验收要点核对

| # | 要点 | 证据 | 判定 |
| --- | --- | --- | --- |
| 1 | 三场景可装配（模板 + 授权）且复跑幂等 | `TestSeedScenarioBots_IdempotentAndNonOverwriting`：首跑建 3 模板 / 11 授权；复跑 0 新建 / 11 跳过；管理员改名与上限调整**不被覆盖** | ✅ |
| 2 | S1 典型链路能力就绪（自然语言查询 → 对话创建工单 → 确认 → 执行 → 回读） | 入口 `chat`+`ticket_detail`；`create_ticket`(act_low) 走 Gate3 确认闭环（B1-05/B1-06）；字段草案走 `draft_ticket_fields`（B3-06） | ✅（能力就绪；E2E 归 B4-01） |
| 3 | S2 **只建议不变更**（负向断言：不得含改级/改状态工具） | `TestScenarioBots_Definitions`：S2 授权与 `update_incident/declare_major/escalate_incident/resolve_incident/update_ticket` **无交集**；`link_ticket_ci` 为写工具（确认闭环） | ✅ |
| 4 | S2 影响面分析可用且超时/输出上限生效 | `get_ci_impact`、`analyze_ci_impact_plan`（均 plan；注册表元数据：30s / 256 KiB，执行侧统一生效） | ✅ |
| 5 | S3 **不自动发布**（负向断言） | `TestScenarioBots_Definitions`：S3 与 `publish_*`/`create_kb_article` 无交集；`draft_kb_article` 只落 `bot_artifacts`（`TestPlanTools_ArtifactsAndZeroBusinessWrites` 断言知识文章实体零写入） | ✅ |
| 6 | S3 草稿归属与隔离 | 产物按「租户 + 发起人」收敛（`TestArtifactStore_TenantAndOwnerIsolation`） | ✅ |
| 7 | 入口上下文生效（`ticket_detail`/`incident_detail`/`ci_detail` 工具面按入口过滤） | B3-01 证据（`TestB3Entrypoint_ExecutionFaceFiltering`）+ B3-02 前端 launcher | ✅ |

## 4. 场景验收单（人工复核用清单）

> 下列步骤为**真实环境**验收路径（需后端 `bot.enabled=true`、mock/真实 provider）；本轮以自动化证据替代，条目本身留给 B4-01/BT-09 执行并回填。

| 场景 | 步骤 | 期望 | 结果（待回填） |
| --- | --- | --- | --- |
| S1 | ① 工作区选择「S1 工单助手」→ 问「本租户高优先级工单有哪些」 | 返回列表工具结果，SSE 含 `tool_call_finished` | ⬜ |
| S1 | ② 说「帮我建一个打印机故障工单」 | SSE 含 `confirmation_required`；确认抽屉展示字段草案 | ⬜ |
| S1 | ③ 确认 → 执行 | ≤30s 内执行完成，回读校验通过（`verified`），工单号回填 | ⬜ |
| S2 | ① 事件详情页点「问 AI」→ 问「这次事件影响哪些 CI」 | 触发 `get_ci_impact`/`analyze_ci_impact_plan`，证据面板可回溯 | ⬜ |
| S2 | ② 说「把事件升级为重大事件」 | **不得**自动改级/改状态；仅给出建议（负向断言在自动化已锁定） | ⬜ |
| S2 | ③ 说「把工单和 CI 关联起来」 | 走 `link_ticket_ci` 确认闭环，确认后执行 | ⬜ |
| S3 | ① 问知识问题 → 说「保存成文章草稿」 | 产出 `draft` 产物；**不创建/不发布**知识文章 | ⬜ |
| S3 | ② 在产物面板查看草稿 | 仅本会话/本人可见 | ⬜ |

## 5. 遗留与登记

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | `create_kb_draft`（act_low 写工具，创建知识草稿实体并归属当前用户） | **B4**（写工具 + 审批链 + 草稿租户隔离评审）；当前 S3 以「产物草稿」满足"不自动发布"底线 |
| 2 | 场景 Bot 的用户可见性（audience：S1/S2 `internal`、S3 `all`）与角色×入口矩阵 | 已按 audience 过滤（`ListVisibleForChat`）；完整矩阵归 B2-06 已覆盖口径，场景级矩阵并入 B4-01 |
| 3 | 场景种子在 bootstrap 的自动装配（当前为显式调用：管理页/运维脚本触发） | 若产品要求新租户默认可见，需在租户开通流程显式调用（避免默认助手之外的模板静默出现）；已登记为决策待办 |
| 4 | 真实 provider 工具调用覆盖（S1/S2/S3 全量链路） | B4-01（U-B5 未核实项合并跟踪） |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | S1/S2 判定 `integration_verified`（场景定义 + 幂等种子 + 边界断言 + 能力就绪）；S3 部分交付（`unit_verified`，`create_kb_draft` 归 B4）；真实对话 E2E 与浏览器交互归 B4-01/BT-09 |
