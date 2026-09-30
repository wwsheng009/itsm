# B4-04 S4/S5 场景评估结论（变更方案助手 / 员工自助与服务请求助手）

> 文档类型：产品评估结论
> Status: draft（待产品评审确认）
> 编制日期：2026-09-27
> 任务：B4-04（S4/S5 评估部分）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-04、§5.2 AB4-04）、`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`（§9 落地状态）
> 依据：工具面盘点（`itsm-backend/service/tool_registry.go:393-838`，17 个内置工具）+ B0–B4 已交付能力

## 1. 结论摘要

| 场景 | 结论 | 理由（一句话） |
| --- | --- | --- |
| **S4 变更方案助手** | **暂缓（下一期重估）** | 变更域**零工具**（无 `list_changes`/`get_change`/`draft_change_plan`），且变更涉及 CAB 审批语义与风险分级，需先做域工具接入 + 审批语义评审，非当前一期可闭环 |
| **S5 员工自助与服务请求助手** | **进入下一期（P1，编号 B5-01 起）** | 自助的两条主路径中「知识问答/自助」已由 S3 场景（`audience=all`）交付；缺口集中在「服务目录只读 + 服务请求创建（写工具）」，可复用现有确认闭环与产物设施，增量成本可控 |

## 2. 工具面盘点（现状，可复核）

| 域 | 已有工具 | 缺失（S4/S5 需要） |
| --- | --- | --- |
| 工单 | `list_tickets` / `create_ticket` / `update_ticket` / `create_ticket_type` / `draft_ticket_fields`（`tool_registry.go:530/633/658/694/444`） | — |
| 事件 | `get_incident_stats`（`:393`） | 事件详情/改级（**有意缺失**：S2 只建议不自动变更） |
| CMDB | `list_cis` / `get_ci` / `get_ci_relationships` / `create_ci_relationship` / `delete_ci_relationship` / `get_ci_impact` / `get_ci_tickets` / `link_ticket_ci` / `analyze_ci_impact_plan`（`:554/732/757/782/812/838/581/606/473`） | — |
| 知识 | `list_kb` / `draft_kb_article`（`:419/502`） | `create_kb_draft`（已登记归 B4/B5）、发布类（**有意不做**） |
| **变更** | **无** | `list_changes` / `get_change` / `draft_change_plan` / 风险分级辅助 |
| **服务请求/目录** | **无** | `list_service_catalog_items` / `get_service_request` / `create_service_request` |

## 3. S4 变更方案助手 — 暂缓依据

| # | 维度 | 评估 |
| --- | --- | --- |
| 1 | 能力前置 | 变更域零工具；`changes` API 与 CAB 审批链（`handlers/cab`）、标准变更模板（`standard_change`）均未暴露给 Bot 工具面 |
| 2 | 语义风险 | 「生成变更方案」天然贴近 `act_medium/high`（影响生产变更窗口），需明确：只产出草案（plan 类）→ 人工转正式变更；不得直接创建/提交变更 |
| 3 | 审批耦合 | 变更走 CAB 审批而非「对话内确认」；确认单闭环与 CAB 的关系需产品定义（一次确认 vs 双重审批） |
| 4 | 收益/成本 | 需 4～6 个新工具 + 变更域权限矩阵复评 + CAB 语义评审；相对 S5 的收益证据不足（无用户需求数据支撑） |
| 5 | 结论 | **暂缓**：列入下一期候选（B5 备选），前置条件 = 变更域工具接入 + CAB 语义评审 + 用户需求验证 |

## 4. S5 员工自助与服务请求助手 — 进入下一期依据

| # | 维度 | 评估 |
| --- | --- | --- |
| 1 | 已具备 | S3 场景（`s3-knowledge-assistant`，`audience=all`）已交付知识问答与草稿；`create_ticket`（`act_low`）可承担「报障」入口；确认闭环/产物/审计/指标全部就绪 |
| 2 | 缺口（增量清晰） | ①服务目录只读工具（`list_service_catalog_items`）；②服务请求只读（`get_service_request`）；③服务请求创建工具（`create_service_request`，写工具走确认闭环 + 幂等键） |
| 3 | 权限模型 | 自助面向 `end_user`：需把新工具标注为 `read`/`act_low` 并确认 `end_user` 角色在目标入口（`chat`/`service_request_*`）的授权矩阵（复用 B2-02 `BotPolicy` 四重交集，无需新机制） |
| 4 | 落点 | 场景 S5（`s5-self-service`）模板 + 授权：`list_kb`/`list_service_catalog_items`/`get_service_request`（读）+ `create_service_request`/`create_ticket`（写，确认闭环）+ `draft_kb_article`（plan） |
| 5 | 结论 | **进入下一期 P1**（B5-01 服务目录只读工具 → B5-02 服务请求创建（含确认/幂等/回读）→ B5-03 S5 场景 pilot 验收） |

## 5. 下一期建议拆解（B5 草案，供评审）

| ID | 任务 | 依赖 | 规模 |
| --- | --- | --- | --- |
| B5-01 | 服务目录与服务请求**只读**工具（`list_service_catalog_items`/`get_service_request`）+ 元数据标注 + 入口矩阵 | B4 出口 | M |
| B5-02 | 服务请求**创建**写工具（`create_service_request`：确认闭环 + 幂等键 + 回读校验 + 租户隔离） | B5-01 | L |
| B5-03 | S5 场景 pilot（模板 + 授权 + 验收单 + 负向断言「不得代填他人/跨租户」） | B5-02 | M |
| B5-04 | S4 复评输入（变更域工具可行性 spike + CAB 语义评审材料） | 产品评审 | S |
| B5-05 | token 计量接线（provider usage → `bot_runs`/`bot_steps`）与成本看板真实成本列 | B4-02 | M |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | S4 判定暂缓（变更域零工具 + CAB 语义未定），S5 判定进入下一期 P1；附工具面盘点与 B5 拆解草案（待产品评审确认） |
