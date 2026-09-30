# ITSM Bot 能力落地分析（借鉴 ai-gateway 用户侧 Bot 体系）

> 文档类型：分析报告（现状盘点 + 差距分析 + 目标设计与分期建议）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`（React）；`itsm-agent` / `itsm-cli` 作为后续消费面
> 目标读者：后端、前端、测试、产品
> 关联文档：`docs/plan/multi-llm-provider-plan.md`、`docs/plan/llm-protocol-adapter-plan.md`、`docs/articles/07-ai-native-architecture-guidance-harness-skill.md`、`AGENTS.md`、`ROADMAP.md`、`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（本报告的实施方案）
> 外部参考（只读参考，不引入代码依赖）：`E:\projects\ai-gateway`（`feat/shop-p1-payment-wallet`）——其 `docs/plan/user-side-bot-agent-system-design-implementation-plan-20260702.md`（设计）与 `docs/plan/user-side-bot-agent-feature-completeness-review-20260927.md`（完整度审查）
> 核查基线：`feat/vite-migration`，HEAD `7442fad5`；工作树含未提交改动（`itsm-backend/handlers/ai/service.go`、`repository_impl.go`、新增 `conversation_title*.go` 等，见 §3.5）
> 核查方式：静态代码与文档交叉核对；本轮未执行编译、单元测试与 E2E，凡未核实处均标注【未核实】

---

## 0. 结论先行（TL;DR）

**一句话**：ITSM 已经具备“能聊、能查、能发起写请求并走审批”的 AI 骨架（含 LLM 工具循环），缺的是把单助手升级为 **Bot 运行时** 的组织层：Bot 模板与工具授权、运行态（run/step/事件/工件）、写动作的对话内确认闭环、页面级入口协议、以及配套的脱敏与验收体系。

**从 ai-gateway 值得借鉴的 4 件事（其余为强业务耦合，不建议照搬）**：

1. **Bot 策略层**：Bot 模板（audience/risk 上限/entrypoint）＋工具授权（tool grants）＋工具元数据（risk/category/dry_run/idempotency/redaction/timeout）。ai-gateway 用 `PolicyEngine` 在工具下发前做“风险上限 × 入口 × 授权”三重校验（`internal/service/userbot/policy.go:24-41`）；ITSM 目前只有 RBAC（`itsm-backend/handlers/ai/service.go:126-156`）与硬编码白名单（`service.go:385-398`）。
2. **会话运行态与事件协议**：session/run/step/message/tool_call/artifact/event 分层建模（设计文档 §12.1）＋稳定 SSE 事件（可恢复、可审计）。ITSM 目前只有 conversation/message/tool_invocation，无 run/step/事件/工件。
3. **写动作“确认闭环”**：dry-run 预览 → 持久化待确认（参数冻结，审批通过后执行持久化参数、不允许模型重新生成）→ 幂等键/对象版本 → 过期与拒绝回填 → 执行后 verify。ai-gateway 的 `user_bot_confirmations` 状态机为 `pending/confirmed/rejected/expired/cancelled`（`internal/model/entity/user_bot_confirmation.go`）；ITSM 已有 `pending→approved→执行` 的审批管线与队列，但缺 dry-run、过期、幂等/版本、执行后校验与对话内确认 UX。
4. **页面级入口与上下文协议**：entrypoint + page context + launch 参数（记录/商品/资源详情页把“当前对象”带进 Bot 会话）。ai-gateway 有 `launchUserRecordBotForPlaygroundLog` 等 launcher 与 `/portal/bots` 工作区；ITSM 的 AI 仅有一个独立 `/ai/chat` 页面与若干静态分析面板。

**不可直接借鉴的部分**：Shop/商家/社区/号源/支付等领域工具与 Actor 模型、商家 surface、以及其分阶段实施记录——这些与 ai-gateway 业务域强绑定，ITSM 应换成工单/事件/知识/CMDB/变更等自身域。

**建议路线（B0→B4）**：B0 工具元数据与审计补全 → B1 run/事件与确认闭环 → B2 Bot 模板与工具授权（含管理配置） → B3 页面入口与首批场景 Bot（工单/知识/CMDB） → B4 E2E 验收与状态回写。详见 §6。

**可直接复用的 ITSM 存量资产**：LLM 网关（多 provider + 工具调用能力探测 + 降级）、`ToolRegistry`（8 个读工具 + 6 个写工具，域 RBAC）、写工具审批管线（`ExecuteTool` → `ToolInvocation` → `ApproveTool` → `ToolQueue`）、`AIChat` 全页（SSE/Markdown/引用/多 provider/保存文章）、AI 审批页与审计页、`SkillRegistry`（11 个内置技能，pilot→ga 生命周期）、`ai:read`/`ai:write` 权限与 AI 指标/评估设施。

**边界（非目标）**：不引入 ai-gateway 代码依赖；不自建通用多 Agent 运行时；不向模型暴露 shell/任意 HTTP/数据库/Admin API；`financial_sensitive` 类动作不进入通用 Bot；Bot 输出只是建议、计划、工件或受控工具结果，不替代业务 canonical state。

---

## 1. 背景与目标

- **产品方向要求**：`AGENTS.md:24` 明确“把 AI 建进服务管理生命周期，而不是在旁边加一个聊天机器人”，并点名“受控工具调用（controlled tool invocation）”；`AGENTS.md:25` 预留 Skill/连接器/插件市场；`AGENTS.md:107` 指出 `itsm-agent` / `itsm-skill` / `itsm-cli` 是面向未来的扩展面。
- **版本路线**：`ROADMAP.md:36`（v1.7：AI evaluator、Skill registry、连接器生产化）、`ROADMAP.md:37,146-159,194`（v2.0：AI auto-triage GA；v1.7 先做 human-in-the-loop triage）、`ROADMAP.md:57`（v1.0 已交付 Guidance-Harness-Skill 框架脚手架）。
- **已有方法论**：`docs/articles/07-ai-native-architecture-guidance-harness-skill.md` 定义了 Guidance（可控输出）/ Harness（可测）/ Skill（可扩展）三层体系；本报告是在其 Skill 层之上补齐 **Bot 运行时与写动作治理**。
- **本报告要回答的三个问题**：
  1. ai-gateway 用户侧 Bot 体系里，哪些是通用能力、值得移植？
  2. ITSM 现状与目标形态差距在哪、哪些已具备？
  3. 结合 ITSM 的域（工单/事件/知识/CMDB/变更）与现有权限/审批/审计设施，应该分几步落地、每步验收什么？

---

## 2. 参考体系：ai-gateway 用户侧 Bot 拆解与可借鉴性判定

> 本节事实来自 `E:\projects\ai-gateway` 的代码与文档（该仓库 HEAD `ca9b0373cc56` 的独立审查结论见其 `docs/plan/user-side-bot-agent-feature-completeness-review-20260927.md`、`docs/plan/user-side-bot-agent-system-design-implementation-plan-20260702.md`）。引用时以 `ai-gateway:` 前缀区分。

### 2.1 逻辑分层（参考设计文档 §7.1）

```text
Portal UI
   │
User Bot API → UserBot Runtime（Agent 层）
                 ├─ Bot Registry / Template Registry
                 ├─ Tool Registry（工具元数据 + surface 生成）
                 ├─ Policy Engine（风险上限 / entrypoint / 授权）
                 ├─ Tenant & Object Scope Resolver（后端解析，不信任模型参数）
                 ├─ Redaction Engine（输入/输出/工件/审计统一脱敏）
                 ├─ Audit / Artifact Store
                 └─ LLM Gateway + Tool Loop（step/token/timeout 预算）
                       │
                 Tool Executors → 各业务域 service
```

关键职责边界（设计文档 §7.1.1 表）：API handler 只做鉴权/绑定/SSE；Agent 层管状态机、策略、脱敏、工件、审计；Tool Executor 只做 typed input → domain service；Domain Service 仍是业务事实源。**Agent 层是“用户侧智能编排边界”，不是新的大模型运行时，也不复制 Admin Ops Agent。**

### 2.2 数据模型（设计文档 §12.1、§12.3）

| 表 | 作用 | 对本报告最关键的字段/约束 |
| --- | --- | --- |
| `user_bot_templates` | 可配置 Bot 模板（也可代码种子） | `tenant_scope`、`audience`、`status`；unique `slug` |
| `user_bot_tool_grants` | Bot→工具授权 | `bot_id + tool_name` 唯一；`risk_limit`、`args_policy` |
| `user_bot_sessions` | 会话 | `tenant_id/actor_type/actor_id/merchant_id/bot_id/entrypoint`；多组隔离索引 |
| `user_bot_messages` | 消息 | `session_id`、`role`、`content_redacted`、`artifact_refs`、`sequence` |
| `user_bot_runs` | 一次推理与工具循环 | `status`、`model`、`budget`、`error_code`；可恢复/取消 |
| `user_bot_steps` | run 内步骤 | unique `(run_id, step_index)` |
| `user_bot_tool_calls` | 工具调用审计 | `risk`、`status`、`target_type/id`、`input_redacted`、`output_summary`、`support_ref`、`idempotency_key_hash` |
| `user_bot_artifacts` | 结构化产物（草稿/对比/计划） | `artifact_type`、`summary`、`content_ref`、owner 隔离 |
| `user_bot_confirmations` | 写动作确认/审批 | 状态机 `pending/confirmed/rejected/expired/cancelled`；`expires_at`；unique 幂等键 |

配套规则：所有 `act_low/act_high` 工具必须带幂等键（只存 hash，作用域含 tenant/actor/tool/target）；高风险动作另需 `expected_version`；确认单必须保存“脱敏参数 + dry-run 结果 + 过期时间”，**审批通过后执行持久化参数**（设计文档 §12.3、§12.4）。该仓库的审查同时指出两个实现级教训：写动作未检索到显式行锁（现依赖幂等键 + 唯一索引 + 版本），并非所有 executor 都做了执行后 verify——ITSM 设计时应显式补齐这两点。

### 2.3 工具元数据与风险分级（设计文档 §7.2、§7.3、§12.4）

工具必须声明稳定元数据：`category`（read/analyze/draft/plan/act_low/act_high/financial_sensitive）、`risk`、`audience`、`permissions`、`tenant_scope`、`object_scope`、`supports_dry_run`、`approval_required`、`idempotency_required`、`timeout_ms`、`max_output_bytes`、`redaction_profile`。策略门禁按 `风险上限（Bot）≥ 工具风险` 且 `entrypoint 允许` 判定，需要确认的工具不在 tool loop 里直接执行，而是产出 `confirmation_required`（`ai-gateway: internal/service/userbot/policy.go:24-41`）。

风险分级参考（`policy.go:64-75` 排序 + 设计 §7.3 分类）：`read(0) < plan(1) < low(2) < medium(3) < high(4)`；`financial_sensitive` 首版不进入通用 Bot。

### 2.4 运行时服务面（`ai-gateway: internal/service/userbot/service.go`）

`CreateSession / ListSessions / GetSessionDetail / RunMessage / ListConfirmations / CreateConfirmation / ConfirmConfirmation / RejectConfirmation / ExecuteConfirmation / ListSessionEvents / expirePendingConfirmations / replayCompletedConfirmationExecution`。几个对 ITSM 特别有参考价值的语义：

- **执行与确认分离**：`RunMessage` 负责模型推理与读工具；写动作生成 confirmation；`ExecuteConfirmation` 才真正落库。
- **拒绝/过期回填**：拒绝原因作为 tool result 回填给模型重新规划；过期后不能再执行原调用。
- **幂等回放**：重复提交确认单返回首次执行结果（`replayCompletedConfirmationExecution`），不重复写业务状态。
- **写工具不下发给 LLM tool loop**（其单测 `TestServiceRunMessageDoesNotExposeWriteToolsToLLMToolLoop`）：由策略决定 surface。相较之下，ITSM 当前把 6 个写工具下发给 LLM，靠“审批挂起”兜底（`itsm-backend/handlers/ai/service.go:485-530`）。两种模式都可行；ITSM 若保留现模式，必须把“风险上限 + 授权 + 入口”补成策略门禁，否则写工具 surface 会随注册表膨胀而失控。

### 2.5 前端信息架构（审查报告 §3.4-3.5）

- 全页工作区 `frontend/src/pages/user/bots/`：`UserBotChatWorkspace`、`UserBotConfirmationDrawer`（确认抽屉）、`UserBotEvidencePanel`（证据面板）、`UserBotRunStatusBar`（运行状态条）、`UserBotSelectorSidebar`（Bot 切换）+ controller/model/hook。
- 页面入口 launcher：Playground 日志、商品详情、商家商品/资源详情分别通过 `launchUserRecordBotForPlaygroundLog` / `launchShopDiscoveryBotForProduct` / `launchMerchantProductBot` 携带上下文进入会话。
- 管理侧：Admin Bot 配置页（模板/授权/只读投影，读、写权限点分离）；E2E 目录 `e2e/user/bot-agent/{api,browser}/run.ps1` 与结论模板。

### 2.6 验收状态分级与其已知缺口

ai-gateway 采用 `implemented → unit_verified → integration_verified → flow_verified → accepted` 五级状态；其 2026-09-27 审查结论为 `partial_verified_pending_flow_acceptance`（功能与单测基本齐备，真实 provider 样本、部分浏览器主路径与写动作后验未闭环）。**这套“分级 + 证据要求”本身值得 ITSM 直接借用**（ITSM 文档治理已要求“计划 checkbox ≠ 已实现”，见 `plans/README.md:5`，两者精神一致）。

### 2.7 可借鉴性判定

| ai-gateway 能力 | 判定 | 理由 |
| --- | --- | --- |
| Bot 模板/工具授权/风险上限/entrypoint 策略 | ✅ 直接借鉴（换域） | 与业务域无关，是通用的能力治理 |
| session/run/step/tool_call/artifact/event 模型 | ✅ 直接借鉴 | 通用运行时形态；字段可按 ITSM 域裁剪 |
| 确认/审批矩阵（confirm vs approve 分离、dry-run、幂等、过期、回填） | ✅ 直接借鉴 | 与 ITSM 审批设施天然互补 |
| 稳定 SSE 事件与证据面板/确认抽屉 | ✅ 借鉴交互模式 | 前端技术栈不同（React vs 其栈），按 ITSM 组件重写 |
| 脱敏引擎与 `redaction_profile` | ✅ 直接借鉴 | 合规刚需；ITSM 工具参数目前原文落库 |
| E2E 五级验收与 run-summary 证据 | ✅ 直接借鉴 | 与 ITSM docs 治理兼容 |
| Shop/商家/社区/号源/支付工具与 Actor | ❌ 不借鉴 | 与 ai-gateway 业务域强绑定 |
| 商家 surface / merchant scope | ❌ 不借鉴 | ITSM 是多租户 MSP，但无商家模型 |
| 其分阶段实施记录与迁移编号 | ❌ 不借鉴 | 仅作节奏参考 |

---

## 3. ITSM 现状盘点（基线：2026-09-27 工作树）

### 3.1 LLM 网关与多 Provider（已就绪）

- 多 provider 方案与协议适配层均已交付：`docs/plan/multi-llm-provider-plan.md`（v1.25）、`docs/plan/llm-protocol-adapter-plan.md`（PA-1..PA-6 全部落地）。
- 工具调用是一等能力：`service.LLMGateway` 定义 `ToolCallingStreamProvider`（声明 tools + 流式 + `onToolCalls` 回调）与能力探测 `SupportsToolCalling()` / `SupportsToolCallingRequest()`，不支持的 provider 自动降级为普通流式（`itsm-backend/service/llm_gateway.go:81-89,357-408,512-565`）。
- 当前检索到 `OpenAIProvider` 实现了 `ChatStreamWithTools`（`itsm-backend/service/llm_providers.go:150`）；其他 provider 是否具备需按实现探测，缺失时聊天链路会**不注入工具**（`handlers/ai/service.go:477-484` 的 provider 能力闸门）。

### 3.2 聊天链路已含 LLM 工具循环与审批挂起（已就绪，易被低估）

`chatStream`（`itsm-backend/handlers/ai/service.go:447-616`）的实际行为：

1. 按租户拉取工具清单，过滤写工具白名单与域 RBAC 后注入 LLM（`service.go:485-504`）；
2. 工具执行回调复用 `ExecuteTool`（RBAC Gate + 审计），写工具返回 `approval_pending` 时**转译为结构化 tool result 回填给模型**，让模型明确告知用户“已提交待审批”（`service.go:506-530`）；
3. 经 `rag.AskWithLLMStreamWithTools` 走流式工具循环（`service.go:550`）；
4. 流结束后持久化 user/assistant 消息与引用来源（`service.go:554-613`）。

聊天写工具白名单（硬编码，待收敛为授权模型）：`create_ticket`、`update_ticket`、`create_ticket_type`、`link_ticket_ci`、`create_ci_relationship`、`delete_ci_relationship`（`service.go:385-398`）。

### 3.3 工具注册表（14 个工具，元数据偏薄）

`itsm-backend/service/tool_registry.go:126-380` 注册了 8 读 + 6 写：

| # | 工具 | 读/写 | 域权限（Resource:Action） |
| --- | --- | --- | --- |
| 1 | `get_incident_stats` | 读 | incident:read |
| 2 | `list_kb` | 读 | knowledge:read |
| 3 | `list_tickets` | 读 | ticket:read |
| 4 | `list_cis` | 读（ci_type 枚举按租户动态化） | cmdb:read |
| 5 | `get_ci_tickets` | 读 | cmdb:read |
| 6 | `get_ci` | 读 | cmdb:read |
| 7 | `get_ci_relationships` | 读 | cmdb:read |
| 8 | `get_ci_impact` | 读 | cmdb:read |
| 9 | `link_ticket_ci` | 写 | cmdb:write（绑定工单到 CI） |
| 10 | `create_ticket` | 写 | ticket:create |
| 11 | `update_ticket` | 写 | ticket:write |
| 12 | `create_ticket_type` | 写 | ticket_type:write |
| 13 | `create_ci_relationship` | 写 | ci_relationship:write |
| 14 | `delete_ci_relationship` | 写 | ci_relationship:write |

`ToolDefinition` 目前只有 `Name/Description/ReadOnly/Resource/Action/ArgsSchema/ResultSchema`（`tool_registry.go:14-22`）——**没有** risk、category、dry-run、幂等、脱敏、超时、输出上限等元数据。

### 3.4 写工具审批管线（已就绪，缺确认语义）

```text
LLM / API → ExecuteTool（Gate1 身份 + Gate2 域 RBAC + 审计）
  ├─ 读工具：同步执行 → 审计（status=executed, approval_state=auto）
  └─ 写工具：创建 pending ToolInvocation → 返回 invocationId（不落业务库）
       ↓ 人工
   POST /agent/tools/:id/approve（ai:write）
       ├─ 拒绝：approval_state=rejected（无回填/无终态回写给发起会话）
       └─ 通过：approved → ToolQueue.Enqueue
                    ↓ worker（30s 超时）
              ToolRegistry.Execute（持久化参数，模型不能改参）→ finalize
```

证据：`service.go:118-257`（ExecuteTool/recordToolAudit/ApproveTool）、`service/tool_queue.go:30-130`（队列与执行）、契约测试 `handlers/ai/tool_execution_contract_test.go:19-56`（`create_ticket` 请求只产生 pending，业务库零写入）。

**已有但未用/缺失**：`ToolInvocation` 有 `dry_run` 字段但 AI 链路未使用；有 `conversation_id` 字段但聊天路径未回填（§3.5）；无 `expires_at`、幂等键、对象版本、执行后 verify、行锁；`ToolQueue.Enqueue` 在队列满时**静默丢弃**（`tool_queue.go:45-50`），且为进程内队列，重启即丢。

### 3.5 数据模型与迁移基线

- 实体：`Conversation{ID,Title,UserID,TenantID,CreatedAt}`、`Message{ID,ConversationID,Role,Content,RequestID,CreatedAt}`、`ToolInvocation{...}`（`itsm-backend/handlers/ai/entity.go:7-48`）。
- Ent schema：`ent/schema/conversation.go`、`ent/schema/tool_invocation.go`；`ToolInvocation` 含 `needs_approval/approval_state/approval_reason/approved_by/approved_at/dry_run/error` 与 P2-6 RBAC 审计四件套（`user_id/permission_check/permission_reason/role_snapshot`），并有 `conversation`、`user` 两条边（`ent/schema/tool_invocation.go:14-43`）。
- **缺口**：`ToolInvocation.ConversationID` 在聊天执行路径未被写入（`service.go` 中 `ConversationID` 只出现在消息持久化处），因此**审计无法从工具调用回溯到会话/轮次**；也没有 risk、target_type/id、idempotency、input_redacted、output_summary、support_ref、expires_at。
- **基线提醒**：工作树存在未提交改动（`git status`：`M CHANGELOG.md`、`M itsm-backend/handlers/ai/repository_impl.go`、`M itsm-backend/handlers/ai/service.go`、新增 `conversation_title.go` / `conversation_title_test.go` / `repository_conversation_delete_test.go`）。本报告的现状结论以该工作树为准，与 HEAD `7442fad5` 可能有差异。

### 3.6 Skill Registry（已就绪，可作 Bot 工具授权的近亲）

- 11 个内置 Skill：Triage/Chat/KnowledgeSearch/Summarize/Analyze/Analytics/TrendPrediction/CreateTicket/AgentTool/Metrics/Feedback（`itsm-backend/handlers/ai/builtin_skills.go:25-37`）。
- 启动期注册（`internal/bootstrap/app.go:1015-1021`）；管理 API：`GET /api/v1/skills`、`GET /api/v1/skills/:code`、`POST/PUT/DELETE /admin/skills...`、`POST /admin/skills/:code/promote`（pilot→ga）、`POST /skills/:code/invoke`（`router/router.go:579-591`）。
- Skill 是“服务端能力包装 + manifest”；Bot 工具授权引用的是**工具**而非 Skill，两者应在设计上明确分工（Skill=可运营的 AI 能力单元；Tool=受控业务动作）。

### 3.7 前端现状

- 主入口：`/ai/chat`（菜单“智能助手 → AI 问答”，`itsm-frontend/src/lib/router/route-config.ts:415-438`），页面组件 `components/ai/AIChat.tsx`，功能包括：会话历史侧栏、SSE 流式、Markdown、引用来源（RagAnswer 得分/权威等级字段）、停止生成、provider 切换、单条回答→知识文章、整段会话→知识文章（`AIChat.tsx:3-20` 注释与实现）。
- SSE 客户端：`fetch` + `ReadableStream` + `text/event-stream`，含 CSRF 轮换重试、同源代理回退、失败降级为 `/ai/chat`（`lib/api/ai-api.ts:521-660,862-868`）。
- 审批与审计页：`pages/(main)/ai/approval/index.tsx`（待审批/已通过/已驳回/自动执行 + 权限校验列展示）、`pages/(main)/ai/audit/index.tsx`。
- 嵌入式 AI 组件：`AISuggestionPanel`、`AIMetrics`、`AIFeedback`、`AIWorkflowAssistant`（工作流设计器）、`AISuggestionPanel` 在工单详情（`components/ticket/TicketDetail.tsx`）、事件创建/详情（`pages/(main)/incidents/...`）等处使用（`ai-api` 触点清单）。
- **缺口**：无运行状态条、无确认抽屉、无证据/工件面板、无页面 launcher；`/ai/chat` 是一个“通用问答页”，不知道“从哪个业务对象来”。

### 3.8 权限与审计设施

- 权限位：`ai:read`（查看 AI 能力与审计）、`ai:write`（调用和管理 AI 能力）（`internal/authz/catalog.go:161-162`）；绝大多数业务角色持 `ai:read`，`ai:write` 集中在管理类角色（`internal/authz/roles.go:38-148,252-254,425-427`）。
- 工具级权限：走底层域 `resource:action`（如 `incident:read`、`ticket:create`），由 `middleware.HasResourcePermission` 校验（`handlers/ai/handler.go:50`、`handlers/ai/service.go:139`）。
- 已有审计/指标：`ai_feedbacks`（含 `item_type='ai_audit'` 的 GA 审计上报）、`/ai/audit-logs`、`/ai/evaluation`、`/ai/metrics`；工具调用审计落 `tool_invocations`（含 RBAC 快照）。
- 脱敏现状：仅有 LLM Provider 错误文本脱敏（`llm_provider_admin_service.go:818`）；**工具参数与审计结果原文落库**，无通用脱敏引擎。

### 3.9 测试与验收现状

- 单测/契约：`handlers/ai/` 下含 `handler_test.go`、`service_persist_test.go`、`service_rbac_test.go`、`llm_provider_*_test.go`、`tool_execution_contract_test.go`、`repository_conversation_delete_test.go`、`conversation_title_test.go`；前端 `lib/api/__tests__/ai-api.test.ts`、`components/ai/__tests__/MarkdownMessage.test.tsx`。
- 无 Bot 级 E2E（无 run-summary、无浏览器主路径、无“确认→执行→verify”流验收）。本轮未运行任何测试。

### 3.10 成熟度小结

| 维度 | 状态 | 说明 |
| --- | --- | --- |
| 对话体验（流式/历史/引用/Markdown/多 provider） | ✅ 已就绪 | 优于“加个聊天框”的行业平均 |
| LLM 工具循环 | ✅ 已就绪 | 读工具自动执行；写工具审批挂起并回填 |
| 工具元数据与策略 | ⚠️ 偏薄 | 仅 readOnly/resource/action；无风险/入口/授权/幂等/脱敏 |
| 写动作确认闭环 | ⚠️ 部分 | 有审批队列与持久化参数执行；缺 dry-run/过期/幂等/版本/verify/回填 |
| 运行态与事件 | ❌ 缺失 | 无 run/step/event/artifact |
| Bot 组织层（模板/授权/入口） | ❌ 缺失 | 单助手；写工具白名单硬编码 |
| 前端 Bot 工作区与页面入口 | ⚠️ 部分 | 有聊天页；无确认抽屉/证据面板/launcher |
| 审计与脱敏 | ⚠️ 部分 | 有 RBAC 审计；缺会话关联/风险/目标对象/脱敏 |
| 验收体系 | ⚠️ 部分 | 单测与契约测试有；无 Bot E2E 与状态分级 |

---

## 4. 差距矩阵（ITSM 现状 vs 目标形态）

优先级口径：**P0** = 影响安全或审计正确性，必须先做；**P1** = 决定 Bot 能否规模化落地；**P2** = 体验完善；**P3** = 明确暂缓。

| 编号 | 能力维度 | ai-gateway 参考做法 | ITSM 现状 | 差距与建议 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| G1 | 工具元数据与策略门禁 | `ToolDefinition` 全元数据 + `PolicyEngine` 三重校验（`policy.go:24-41`） | 仅 `ReadOnly/Resource/Action`（`tool_registry.go:14-22`） | 扩展元数据（risk/category/dry-run/幂等/超时/输出上限/脱敏档）；工具下发前做「Bot 授权 ∩ 风险上限 ∩ 入口」校验 | P0 |
| G2 | Bot 模板与工具授权 | `user_bot_templates` + `user_bot_tool_grants` | 无 Bot 概念；写工具白名单硬编码（`service.go:385-398`） | 新增模板/授权两表；把硬编码白名单迁移为「授权表 + 兼容默认」 | P1 |
| G3 | 运行态与事件协议 | runs/steps/events + 稳定 SSE 事件 | 仅 conversation/message；SSE 无事件契约文档 | 新增 runs/steps/events；定义并版本化 SSE 事件表（§5.4） | P0 |
| G4 | 写动作确认闭环 | confirmations 状态机 + dry-run + 持久化参数执行 | 审批管线可用；dry-run 字段闲置；无过期/回填/verify | 扩展 ToolInvocation（expires_at、拒绝原因回填、dry-run 快照、执行后 verify） | P0 |
| G5 | 幂等与并发控制 | 幂等键 hash + unique 索引 + `expected_version` | 无幂等键/版本/行锁 | 写工具统一幂等键（作用域：租户+发起人+工具+目标+参数哈希）；对象版本乐观锁 | P0（写路径） |
| G6 | 脱敏 | RedactionEngine + `redaction_profile` | 工具参数与审计原文落库 | 审计/消息/工件统一脱敏；落 `input_redacted` 与 `output_summary`；禁止密钥/token 类字段入库 | P1 |
| G7 | 审计可回溯 | tool_calls 带 session/run/target/support_ref | `conversation_id` 未回填、无目标对象 | 聊天路径回填 conversation/run；补 target_type/target_id/support_ref | P0 |
| G8 | 会话标题与历史管理 | sessions 列表 + 详情 | 工作树已新增会话标题能力（未提交） | 与 run 状态一并呈现；不单独造轮子 | P2 |
| G9 | 页面入口与上下文 | entrypoint + page context + launcher | 仅独立 `/ai/chat`，无来源上下文 | 定义入口枚举与上下文协议（type/id/summary/权限预检），首批接工单、事件、CI 详情页 | P1 |
| G10 | Bot 工作区 UX | 确认抽屉/证据面板/运行状态条/Selector | AIChat 已有流式/引用/会话侧栏 | 在现有 AIChat 上增补三组件，不另起新页面；保持单入口 | P1 |
| G11 | 记忆/经验沉淀 | 四类记忆工件，禁止材料化 | 无 | 首版不做写记忆；仅在 run/step 上预留字段 | P3（本期非目标） |
| G12 | E2E 与验收分级 | 五级状态 + api/browser 双通道 + run-summary | 单测/契约测试有，无 Bot E2E | 建立 Bot E2E 章程（`e2e/`）与 summary 模板；状态写回文档 | P1 |

**矩阵结论**：P0 集中在 G1/G3/G4/G5/G7——即「元数据 → 门禁 → run/事件 → 确认→执行→verify → 审计回填」这一条主干；它们相互依赖（事件需要 run，确认需要元数据与审计），应作为 B0/B1 两期捆绑交付。G2/G9/G10/G12 是规模化与体验层，放 B2/B3/B4。

---

## 5. 目标设计（结合 ITSM 实际情况）

### 5.1 设计原则

1. **业务事实源不变**：工单/CI/知识库仍由既有 `service` + ent 写入；Bot 只负责编排、建议、工件与受控写入，不新造第二套业务存储。
2. **作用域后端解析，不信任模型参数**：现有 `ExecuteTool` 已注入 `user_id/tenant_id` 等服务端事实（`service.go:118-137`）；新增 `ScopeResolver` 统一负责「当前用户/租户/入口上下文对象」的解析与校验，工具入参中任何身份/租户/目标字段一律以服务端解析为准。
3. **最小暴露面**：工具下发面 = `Bot 授权 ∩ 调用者域 RBAC ∩ 风险上限 ∩ 入口策略`；默认只读，写工具按模板显式授权。
4. **写动作三要素**：可预览（dry-run）、参数冻结（确认后执行持久化参数，不允许模型重新生成）、执行后校验（verify/版本回读）。
5. **全程可审计可回放**：run/step/tool_call/confirmation 事件化；拒绝与过期原因回填给模型并落审计。
6. **增量兼容**：`conversation` 即 session v1，不重命名、不双写；Skill 继续作为「可运营的 AI 能力单元」，Bot 只治理「工具与入口」。

### 5.2 逻辑架构（目标态）

```text
┌─ 入口层（前端）──────────────────────────────────────────────┐
│  /ai/chat（升级为 Bot 工作区）   页面 launcher（工单/事件/CI 详情）
│  确认抽屉 · 证据面板 · 运行状态条 · 会话侧栏                    │
└──────────────────────────┬───────────────────────────────────┘
                           │ REST + SSE（事件契约见 §5.4）
┌─ API 层（itsm-backend/handlers/ai）──────────────────────────┐
│  Chat/SSE · Tools · Invocations/Confirmations · Bots(模板/授权)
└──────────────────────────┬───────────────────────────────────┘
┌─ Bot 运行时（itsm-backend/service/bot，新建）─────────────────┐
│  BotPolicy（授权∩风险∩入口） · RunManager（run/step/事件）      │
│  ScopeResolver · Redactor · ConfirmationService · ArtifactStore
└───────┬──────────────────────────────┬───────────────────────┘
        │                              │
┌─ 既有 AI 资产 ────────────────┐  ┌─ 既有业务资产 ─────────────┐
│ LLM Gateway（多 provider/探测）│  │ ToolRegistry（14 工具）     │
│ RAG（AskWithLLMStreamWithTools）│  │ 业务 service / ent（事实源）│
│ SkillRegistry（11 技能）        │  │ Authz（RBAC catalog）      │
└───────────────────────────────┘  └───────────────────────────┘
```

### 5.3 数据模型增量（Ent 迁移建议）

**（a）扩展现有 `tool_invocations`（本期核心，避免拆表）**

| 字段 | 类型 | 用途 |
| --- | --- | --- |
| `conversation_id`（已有，补回填） | uuid | 会话回溯 |
| `run_id` / `step_id` | uuid | 关联运行与步骤 |
| `risk` / `category` | string | 快照工具元数据（防元数据漂移导致审计歧义） |
| `target_type` / `target_id` | string | 目标对象（ticket/ci/kb…），供前端跳转与审计 |
| `idempotency_key_hash` | string | 幂等（只存 hash） |
| `input_redacted` | json | 脱敏后参数（替代原文用于展示） |
| `output_summary` / `support_ref` | string | 结果摘要与支持单引用（合规追溯） |
| `expires_at` | time | 确认过期（默认 24h，可配） |
| `verify_state` / `verify_note` | string | 执行后校验结果 |
| `attempt_count` / `last_error_code` | int/string | 队列重试可观测（替代静默丢弃） |

**（b）新增表（B1/B2 期）**

| 表 | 关键列与约束 |
| --- | --- |
| `bot_runs` | `conversation_id`、`bot_id`、`entrypoint`、`status`、`model`、`budget_json`、`error_code`、`started_at/finished_at` |
| `bot_steps` | `run_id + step_index` 唯一；`type(llm/tool/confirm)`、`payload_ref`、`duration_ms` |
| `bot_events` | `run_id + seq` 唯一；`type`、`payload_json`（SSE 重放与审计共用） |
| `bot_artifacts` | `run_id`、`artifact_type`、`summary`、`content_ref`、`owner_user_id`（按会话隔离） |
| `bot_templates` | `slug` 唯一、`name`、`audience`、`risk_limit`、`entrypoints_json`、`system_prompt_ref`、`status(draft/pilot/ga)` |
| `bot_tool_grants` | `bot_id + tool_name` 唯一；`risk_limit`、`args_policy_json` |

说明：ai-gateway 把确认单独立成表；ITSM 已有成熟的 `ToolInvocation` 审批入口与页面，一期建议**扩展不拆分**（单一审批事实源），若 B2 出现「同一调用多次确认/多次 dry-run 快照」的真实需求再考虑拆 `bot_confirmations`。

### 5.4 运行链路改造点（对照现有代码）

| 现有代码锚点 | 改造 |
| --- | --- |
| `service.go:385-398` 写工具白名单硬编码 | 改为 `BotPolicy.FilterTools(bot, caller, entrypoint)`：授权表读取 + 风险上限 + 入口允许 + 域 RBAC；白名单仅作兼容默认 |
| `service.go:447-616` `chatStream` | 生成 `run_id`，写入 `bot_runs`；每轮 LLM/工具/确认步骤写 `bot_steps`；SSE 按事件表广播并落 `bot_events` |
| `service.go:118-257` `ExecuteTool` | 保留双重 Gate；增加 ScopeResolver、元数据门禁、幂等键生成、dry-run 分支（预览不落业务库） |
| `service.go:506-530` 写工具回填 | 回填内容从 `approval_pending` 升级为 `confirmation_required`（含 invocation_id/摘要/过期时间/dry-run 结果引用） |
| `service/tool_queue.go:30-130` | 队列持久化：pending 落库 + 启动扫描恢复；队列满返回 503 而非静默丢弃；执行后 verify + 失败重试上限 |
| 审批 API（`ApproveTool`） | 支持 approve/reject + 拒绝原因回填会话；过期校验；重复执行返回首次结果（幂等回放） |
| SSE 客户端（`ai-api.ts:521-660`） | 兼容旧 `delta/sources/done/error`，新增事件按 `event:` 名解析；未知事件忽略（前向兼容） |

**SSE 事件契约 v2（建议）**

| event | data 关键字段 | 时机 |
| --- | --- | --- |
| `run_started` | `run_id, bot_id, entrypoint` | 本轮开始 |
| `step` | `step_id, index, type(llm/tool/confirm)` | 每步开始/结束 |
| `tool_call` | `tool, status, risk`（不含敏感参数） | 工具执行前后 |
| `confirmation_required` | `invocation_id, summary, expires_at, dry_run_ref?` | 写工具挂起 |
| `delta` | `text` | 流式增量（兼容旧名） |
| `sources` | `items[]` | RAG 引用（兼容旧名） |
| `artifact` | `artifact_id, type, summary` | 生成工件 |
| `done` | `run_id, usage` | 本轮结束（兼容旧名） |
| `error` | `code, message` | 失败 |

---

### 5.5 写工具风险与确认矩阵（基于现有 6 个写工具）

| 工具 | 建议 category/risk | dry-run | 幂等/版本 | 确认方式与执行后校验 |
| --- | --- | --- | --- | --- |
| `create_ticket` | act_low | 是（返回将创建的字段预览） | 幂等键必需 | 对话内确认卡；执行后回读工单号并回填会话 |
| `update_ticket` | act_medium | 是（字段 diff 预览） | 幂等键 + `expected_version`（乐观锁） | 确认卡展示 diff；版本冲突时中止并提示刷新 |
| `link_ticket_ci` | act_low | 是（显示将建立的关联） | 幂等键 | 对话内确认；执行后回读关联存在性 |
| `create_ci_relationship` | act_high | 是（影响预览） | 幂等键 + `expected_version` | 默认不进入普通 Bot 授权上限；执行后回读关系 |
| `delete_ci_relationship` | act_high | 是（删除影响面） | 幂等键 + `expected_version` | 仅管理员 Bot/管理入口；执行后回读确认已删除 |
| `create_ticket_type` | act_high（配置类） | 是（schema 预览） | 幂等键 | 走管理审批（不复用普通用户确认） |

补充建议（非写库，先以 `plan/analysis` 类工具补齐体验）：

- `draft_ticket_fields`（plan）：根据自然语言生成工单字段草稿 → 只产出 artifact，用户一键带入创建表单；
- `analyze_ci_impact_plan`（analysis）：影响面分析报告，含置信度与证据引用；
- `draft_kb_article`（draft）：把回答/会话整理为知识草稿（复用现有“保存成文章”能力，落 artifact + 知识草稿）；
- `propose_change_plan`（plan）：变更方案骨架，B4 后按需。

### 5.6 首批场景 Bot

**S1 工单助手（Ticket Copilot）**

- 入口：`/ai/chat` 通用入口；工单详情/列表 launcher（entrypoint = `ticket_detail` / `ticket_list`）。
- 工具授权：读 `list_tickets` / `get_incident_stats` / `list_kb`；写 `create_ticket`（act_low）；plan `draft_ticket_fields`。
- 典型场景：自然语言查询（“本周我负责的 P1 工单”）；对话创建工单（确认抽屉 → 执行 → 回填工单号）；字段草稿带入表单。
- 验收要点：SSE 含 `confirmation_required`；确认后队列执行 ≤30s；执行后 verify 回读；审计含 `conversation_id/target_type/target_id`。

**S2 事件/值班助手（Incident Copilot）**

- 入口：事件创建/详情页、值班视图。
- 工具授权：`get_incident_stats`、`list_tickets`、`get_ci_impact`、`link_ticket_ci`（act_low）。
- 典型场景：新事件相似事件检索与影响面分析；定级建议（**只建议不自动变更**，与 `ROADMAP.md:146-159` v1.7 human-in-the-loop triage 一致）；一键关联 CI（确认后执行）。
- 验收要点：负向断言“不得自动修改事件等级/状态”；影响面工具超时与 `max_output_bytes` 生效。

**S3 知识/自助助手（Knowledge Bot）**

- 入口：知识库页、全局 launcher。
- 工具授权：读 `list_kb`；plan/draft `draft_kb_article`；首期写动作仅“保存为草稿”（需新增 `create_kb_draft`，act_low）。
- 典型场景：问答引用 → 一键保存草稿（artifact + 知识草稿，归属当前用户）；不直接发布（publish 属 act_high，后续再评估）。
- 验收要点：草稿归属与租户隔离；不得自动发布。

候选后续：S4 变更方案助手、S5 员工自助/服务请求助手（B4 之后按需增补）。

### 5.7 前端信息架构调整

- **保留** `/ai/chat` 作为唯一工作区（不新建 `/bots` 页面；ITSM 已有成熟聊天页，避免 ai-gateway 那种双页并存的历史包袱）。
- 新增三组件（均可作为 `components/ai/` 子组件复用）：
  - `ConfirmationDrawer`：展示 dry-run 摘要/差异、过期倒计时、通过/拒绝（拒绝原因必填并回填会话）；
  - `EvidencePanel`：本轮 step/tool_call 时间线，含 `target_type/id` 跳转与 `support_ref`；
  - `RunStatusBar`：run 状态、模型、预算消耗。
- **页面 launcher**：工单/事件/CI 详情页增加“问 AI”入口，携带 `{entrypoint, target_type, target_id, summary}` 打开工作区；这是把 AI 从“独立页”变成“嵌在生命周期里”的关键一步。
- **审批页增强**（`pages/(main)/ai/approval`）：补 risk 徽标、target 跳转、dry-run 快照、过期倒计时；操作与抽屉共用同一 API。
- **管理页**：Bot 模板与工具授权 CRUD（交互模式复用 Skill 管理页），保存前提供“影响面预览”（哪些角色/入口将获得该工具）。

### 5.8 权限与安全设计

- **权限位**：一期复用 `ai:read`（查看 Bot 模板/授权/审计）与 `ai:write`（管理 Bot 与调用写工具审批）；若后续需要与“AI 使用”解耦，再引入 `bot:read`/`bot:write`（避免一次引入过多权限位，保持 `internal/authz/catalog.go` 收敛）。
- **三重交集判定**：`Bot 授权 ∩ 调用者域 RBAC ∩ 服务端风险上限`，任一不满足即拒绝并审计拒绝原因（沿用现有 `permission_reason` 快照机制）。
- **Prompt Injection 边界**：模型输出只被当作“工具名 + 参数建议”；身份/租户/目标对象由 `ScopeResolver` 覆写；被覆盖的参数差异写入审计，便于发现注入尝试。
- **不可信输入**：RAG 文档与工具返回内容一律视为数据而非指令（与 `docs/articles/07-...md` 的 Guidance 体系一致），高风险写动作不得由工具返回内容触发（需用户显式确认）。
- **黑名单**：Admin API、权限管理、删除类（除已授权的 `delete_ci_relationship` 管理员场景）、任意 HTTP/shell/DB 工具永不进通用 Bot。
- **预算与速率**：每 run 限制 step 数、token、工具调用次数、单工具超时（默认 30s）与输出字节上限；超限以 `error{code=budget_exceeded}` 结束并落审计。

### 5.9 可观测与成本

- 指标扩展（复用 `/ai/metrics`）：run 成功率、确认通过/拒绝/过期率、工具错误率、verify 失败率、平均步数与时延、token 消耗（按 bot/entrypoint/tool 维度）。
- 审计联查：`tool_invocations`（业务动作/审批）× `bot_events`（对话过程）可通过 `run_id` 关联导出。
- 成本护栏：高风险/高频 Bot 单独限额；模型按 Bot 模板可配置（默认沿用租户主模型）。

---

## 6. 分期落地建议（B0→B4）

> 依赖关系：B0 → B1 → B2 → B3 → B4；B0 与 B1 的部分前端组件可并行。每期结束需提供「变更文件 + 测试输出 + 可复核证据（SSE 抓包/审计查询/截图）」并回写文档状态。

| 期 | 目标 | 主要交付 | 验收证据 | 主要风险 |
| --- | --- | --- | --- | --- |
| **B0** 元数据与审计补全（P0） | 让写动作“可判定、可回溯” | `ToolDefinition` 扩展元数据（14 个工具全量标注）；`ToolInvocation` 增加 risk/target/幂等/脱敏/过期字段；聊天路径回填 `conversation_id`；`create_ticket`/`update_ticket` dry-run 预览（只读不落库）；拒绝原因回填会话 | 单测 + 契约测试（dry-run 零业务写入；拒绝有回填）；审计可按会话查询 | 存量工具元数据一次标注工作量；dry-run 语义边界需写清 |
| **B1** 运行态与确认闭环（P0） | 把“审批”升级为“对话内确认闭环” | `bot_runs/steps/events` 三表；SSE 事件契约 v2（兼容旧事件）；确认抽屉/证据面板/运行状态条；幂等键 + `expected_version`；过期与幂等回放；ToolQueue 持久化（pending 落库 + 重启恢复 + 队列满返回 503） | E2E 一条“对话→确认→执行→回读”全链路 + run-summary；队列重启恢复用例 | 前端 SSE reducer 兼容改造；队列持久化选型 |
| **B2** Bot 模板与授权（P1） | 从“单助手”变成“可治理的多 Bot” | `bot_templates`/`bot_tool_grants`；`BotPolicy.FilterTools`（授权∩RBAC∩风险∩入口）；管理页 CRUD + 影响面预览；硬编码白名单迁移为兼容默认 | 不同角色/入口下发工具面不同；未授权调用被拒且审计可查 | 授权模型设计不当会放大权限面，需安全评审 |
| **B3** 页面入口与场景 Bot（P1） | 把 AI 嵌进业务生命周期 | 页面 launcher + 上下文协议（工单/事件/CI）；S1 工单、S2 事件、S3 知识三个 Bot 以 pilot 状态上线；`draft_*`/`analyze_*` 计划类工具 | 三处入口可用；上下文在工具调用中可验证生效；三场景验收单 | 入口扩张带来的权限与噪音治理 |
| **B4** E2E 验收与状态回写（P1） | 建立可持续的验收体系 | `e2e/` Bot 用例（api + browser 双通道）、run-summary 模板、五级状态（implemented→…→accepted）；ROADMAP/CHANGELOG 回写；S4/S5 评估 | E2E 脚本可复跑；文档状态与实际一致 | 真实 provider 成本/波动 → mock 为主 + 少量 smoke |

**本期明确不做**（非目标，避免范围膨胀）：记忆系统（G11）、多 Agent 协作运行时、任意 HTTP/shell/DB 工具、知识自动发布、事件等级自动变更、`financial_sensitive` 类动作。

---

## 7. 风险与开放问题（含阻塞项）

1. **写工具下发模式（阻塞 B1 设计定稿）**：保留 ITSM 现行“写工具进 tool loop + 审批挂起”，还是采用 ai-gateway“写工具不下发、由策略层决定 surface”？建议折中：写工具仍可被模型请求，但立即返回 `confirmation_required` 而**不进入任何执行分支**（维持现契约测试的“零写入”保证），由用户在确认抽屉显式确认。需产品与安全共同拍板。
2. **审批与发起人分离（四眼原则）**：ITSM 现行是否允许同一人审批自己的写请求【未核实】；若允许，Bot 高频使用场景下风险上升，建议至少对 `act_high` 强制分离。
3. **队列持久化选型**：DB 表轮询（无外部依赖、起步快）vs 既有消息设施【是否有 Redis/MQ 未核实】；B1 先 DB 表轮询，保留抽象接口。
4. **多租户红线**：`bot_runs/steps/events/artifacts` 所有查询必须带 `tenant_id`，索引设计从第一天就应包含租户维度；审计导出同样按租户隔离。
5. **模型能力差异**：当前仅 OpenAI 兼容 provider 实现 `ChatStreamWithTools`；其他 provider 下 Bot 退化为普通问答，需在 UI 明示“当前模型不支持工具调用”，避免用户误以为写动作可用。
6. **事件表膨胀**：`bot_events` 需要保留策略（如 90 天热存 + 归档），否则长会话/高频使用下存储增长失控。
7. **工作树未提交改动**：会话标题相关改动尚未提交，落地时先确认其是否并入（G8），避免重复建设。
8. **dry-run 语义边界**：部分业务约束（唯一性/配额/外部系统）无法在纯预览中完整校验；文档与 UI 必须写明“预览不保证最终成功”，执行失败仍走错误回填。
9. **前端改造兼容性**：SSE 新增事件对旧客户端必须“未知事件忽略”；`ai-api.ts` 的 CSRF 重试/降级逻辑需要在新增事件下复测。
10. **E2E 环境依赖**：真实 provider 不稳定且有成本，采用 mock provider 全量 + 少量 real smoke 的策略（与 `docs/plan/multi-llm-provider-plan.md` 既有测试策略一致）。
11. **文档治理**：本报告仅完成「分析」，不代表已排期；已另立 `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`，并按 `plans/README.md:5` 的原则维护「未实现/已实现」状态，杜绝 checkbox 冒充交付。

---

## 8. 附录

### 8.1 术语对照（ai-gateway ↔ ITSM）

| ai-gateway 术语 | ITSM 对应/建议 | 说明 |
| --- | --- | --- |
| session / run / step | `conversation`（v1 session）/ `bot_runs` / `bot_steps` | 一期不重命名 conversation |
| confirmation | `ToolInvocation`（扩展后） | 保留单一审批事实源 |
| tool grant | `bot_tool_grants` | Bot 维度的工具白名单 |
| entrypoint | 入口枚举（`chat`/`ticket_detail`/`incident_detail`/`ci_detail`…） | 页面 launcher 携带 |
| artifact | `bot_artifacts`（草稿/分析/计划） | 不落业务库的内容产物 |
| redaction profile | 脱敏档（default/strict） | 一期先实现 default + 密钥类强制 strict |
| risk: read/plan/low/medium/high | 同左（映射到 act_low/act_medium/act_high 命名） | 与 `policy.go` 排序一致 |

### 8.2 内置 Skill 清单（11 个，摘自 `builtin_skills.go:25-37`）

Triage、Chat、KnowledgeSearch、Summarize、Analyze、Analytics、TrendPrediction、CreateTicket、AgentTool、Metrics、Feedback。Bot 的工具授权对象是 Tool 而非 Skill；Skill 继续由 `/api/v1/skills` 与 `/admin/skills` 管理（promote 支持 pilot→ga，可复用于 Bot 模板的 `status` 生命周期）。

### 8.3 外部参考资料

- ai-gateway 用户侧 Bot 系统设计：`E:\projects\ai-gateway\docs\plan\user-side-bot-agent-system-design-implementation-plan-20260702.md`（§7 工具与策略、§12 数据模型与确认矩阵、§13 前端 IA）。
- ai-gateway 用户侧 Bot 完整度审查（2026-09-27）：`E:\projects\ai-gateway\docs\plan\user-side-bot-agent-feature-completeness-review-20260927.md`（五级验收状态、已知缺口与“确认执行后未全量 verify”教训）。
- ITSM 关联文档：`docs/plan/multi-llm-provider-plan.md`、`docs/plan/llm-protocol-adapter-plan.md`、`docs/articles/07-ai-native-architecture-guidance-harness-skill.md`、`AGENTS.md`、`ROADMAP.md`。

---

## 9. 落地状态回写（2026-09-27）

> 本报告为**设计态**分析（初稿未运行测试）。B0–B4 的实际交付状态按五级口径回写如下；每一行的证据均可在 `docs/plan/evidence/` 下复核，方案文档为 `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`。

| 阶段 | 交付状态 | 证据与说明 |
| --- | --- | --- |
| BP5 前置 | 实现完成 | `evidence/bot-b0/BP5-B0-01-unit-evidence.md`（`service/bot/` 骨架 + `bot.enabled` 开关，默认关闭零行为变化） |
| B0 元数据与一次迁移 | `integration_verified` | `evidence/bot-b0/B0-07-b0-acceptance-evidence.md`（14 个内置工具全量标注 + `tool_invocations` 联合迁移 + dry-run 零写入 + 统一脱敏入口）；条件项：Postgres 侧 CI job 首轮观察 |
| B1 运行档案与确认 | `flow_verified` | `evidence/bot-b1/B1-10-flow-acceptance-evidence.md`（run/step/event 三表 + 确认五态 + 持久队列重启恢复 + SSE v2 注册表 + 前端确认抽屉/证据面板/审批页）；条件项：浏览器 E2E 归 B4-01/BT-09（**B4-01 已交付 api 通道，browser 通道待 CI 首轮**） |
| B2 模板与授权治理 | `flow_verified` | `evidence/bot-b2/B2-06-acceptance-evidence.md`（模板/授权两表 + `BotPolicy` 四重交集（下发与执行同源）+ 越权负向安全集 + `/admin/bots` 管理页 + 工作区 Bot 选择器）；条件项：浏览器级交互归 BT-09、真实 provider 覆盖归 B4-01 |
| B3 入口上下文与场景 pilot | `flow_verified`（条件达标） | `evidence/bot-b3/B3-07-flow-acceptance-evidence.md`（六入口协议与目标预检 fail-closed + 三处 launcher + S1/S2/S3 验收单与负向断言）；**部分交付**：S3 的 `create_kb_draft`（写工具）归 B4；条件项：真实对话 E2E 归 B4-01 |
| B4 E2E 验收与状态回写 | 进行中（本文件即回写动作之一） | `evidence/bot-b4/B4-01-e2e-charter-evidence.md`（双通道章程 + run-summary，api 6/6 实跑通过）、`evidence/bot-b4/B4-02-metrics-evidence.md`（运行维度指标与看板）；**B4-03 本次完成**（本文件/ROADMAP/CHANGELOG 回写）；B4-04 `accepted` 评审待产品与测试签署 |

**与初稿设计的三处偏差（已在方案中登记）**：

1. **`/ai/metrics` 复用改为专用端点**：B4-02 新增 `GET /api/v1/ai/bot-metrics`，以保持既有 telemetry 载荷字节稳定并隔离数据源（方案 §4.5 B4-02 状态行）。
2. **S3 场景部分交付**：`draft_kb_article`（只读业务库、产物落 `bot_artifacts`）已交付；`create_kb_draft`（真正创建知识草稿实体）因依赖写工具审批链与草稿租户隔离评审，归入 B4。
3. **token 计量未接线**：成本维度暂以「步数 / 工具调用数 / 时延」为代理（`tokensRecorded=false` 显式暴露，页面与指标 `notes` 均提示）。

---

## 变更记录

| 日期 | 作者 | 变更 |
| --- | --- | --- |
| 2026-09-27 | AI 辅助分析 | 初稿：完成 ai-gateway 可借鉴性判定、ITSM 现状盘点、差距矩阵与 B0–B4 分期建议（基于 `feat/vite-migration` 工作树静态核对，未运行测试） |
| 2026-09-27 | AI 辅助执行 | 新增 §9 落地状态回写：B0–B3 交付状态（`integration_verified`/`flow_verified`）与 B4 进行中状态、三处设计偏差登记；与 ROADMAP / CHANGELOG 回写同步（B4-03） |
