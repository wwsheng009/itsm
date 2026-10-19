# AI 助手 Dock 方案评审报告

- 评审对象：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`（深化规划 v4 + §7 交付台账；评审时点 93427 B / 1049+ 行）
- 评审日期：2026-10-19（快照 13:0x；**静态核验，未运行前端/测试**）
- 代码基线：HEAD `00817fc6`（评审当天 12:56 的台账增量提交）；主交付 `d51ed172`（391 files，+51933/-1298）
- 评审方法：主审全量通读 + 3 个只读子代理交叉核验（§1.2）+ 抽查 `file:line` 证据
- 状态标记：✅ 已交付｜🟡 部分/偏差｜❌ 未找到｜⚠️ 口径/治理问题

---

## 0. 结论速览

1. **文档状态治理失效，这是评审要解决的第一问题。** 头部仍写"状态：规划定稿（待评审；未开工）"（L4），但 §7 已积累 40+ 条 `> 进度：**…已交付**` 台账（最近一条由评审当天 12:56 的 `00817fc6` 追加），代码早已全量落地。"待评审"的服务对象应是**收口决策**（拆分台账、收敛开放问题、确认口径变化），不再是开工审批。
2. **设计→任务映射有 8 项悬空/偏差**：`/` 快捷指令与 `@` 引用、附件/截图、I0-01 交互状态机+错误字典、灰度开关 `assistant.dock.enabled`、"在侧栏继续/全屏打开"双入口条款、拖拽调宽/边缘收纳、§13.3 质量层指标数据源、a11y 流式验收口径（§3 逐项表）。
3. **§13.3 质量层指标与 §11.5 放量门槛自相矛盾**：引用覆盖率/无引用回答率/点踩率在代码中无数据源（§5.2），而放量门槛的"负向指标不恶化"依赖这些指标。
4. **在途未提交修复批方向与 §3.7 L119 不一致**：工作区改为"抑制 Dock + 一次性交接"，不是方案写的"双入口并存 + 在侧栏继续/全屏打开"；该批尚未入任何台账（§6）。
5. **§5 帮助子系统存在系统性"写面缺口"**：8 个工具有 3 个（`help_update_draft`/`help_submit_review`/`help_publish`）零实现，diff 预览/一键审阅/批量草稿未落地，help-index 向量化与 KB"使用手册"合并未做（§5.1）。

---

## 1. 方法与范围

### 1.1 对象与快照

- 方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`。评审期间该文档处于并行编辑中（v4 时 91333 B → 评审时点 93427 B），行号以 2026-10-19 13:0x 快照为准。
- 代码：HEAD `00817fc6` + 工作区未提交批（§6，评审期间仍在变化）。
- 限制声明：本次为**静态核验**（阅读 + grep + git），未运行前端/后端测试与浏览器；"已交付"判定引用方案台账与证据文件，交叉验证上限为代码/产物 `file:line`。

### 1.2 子代理交叉核验

| 子代理会话 ID | 范围 | 核验方法 |
|---|---|---|
| `subagent_plan-task-inventory_36e6b56b679342bfa9a6b57bebc1ec79` | §7 全部任务（V/P0/P1/P2/I；O 系除外） | 进度块 + 证据文件存在性 + 代码 file:line + git 四重交叉 |
| `subagent_plan-help-subsystem_1f615d6a17d34e7c94201260a5e0154a` | §5.1–5.8（H 系列设计） | 设计项逐条对照代码/脚本/CI/迁移 |
| `subagent_plan-observability_7fa0feb7610147db944aaa248aaf6614` | §9 + §13（O 系列设计） | 事件字典 38 项逐字段、看板四层逐指标对照实现 |

### 1.3 口径说明

- 用户侧 UX 评审稿描述的是"一个页面"，系统实际有**两个界面**：AI 工作区 `/ai/chat`（`components/ai/AIChat.tsx`）与全局 Dock（`layouts/MainLayout.tsx:222` 挂载 `dock/AssistantDock.tsx` + `AssistantChatPanel.tsx`），二者共用 `components/ai/chat-core/`。
- 本报告只评审方案文档与其交付一致性，不含对 UI 观感的复核（观感与实现差距另见 `docs/plan/ai-assistant-ux-review-gap-analysis-2026-10-19.md`）。

---

## 2. 文本质量与状态治理

### 2.1 状态头与修订史过期

- L4 仍写 `状态：规划定稿（待评审；未开工）`，与 §7 台账直接矛盾：V/P0/P1/P2 全量、I 系 8/9、O 系 7/7 均有"已交付"块；主交付提交 `d51ed172`、部署记录 `27ead101`、运行时走查台账 `00817fc6` 均已入史。
- 修订史止于 v4（L6-8），未记录 2026-10-19 的台账增量（`00817fc6` 追加的"浏览器级运行时走查 + 写路径审批闭环缺陷修复"，含 `create_ticket` schema 缺陷与审批卡无限重拉两处修复）。
- §10 L930"本规划实施前需评审确认"存在**流程倒挂**：实施已全量完成，本次评审才发生。建议把该句改为"实施后复核 D2/D5 落地口径"类表述。

### 2.2 "设计 + 台账"混合体的可读性/可审计性问题

- §7 任务表**无状态列、无勾选框**（子代理全文件检索 `✅/☐/☑/[x]` 零命中）；执行状态散落于 40+ 条 `> 进度：` 引用块与表间注释（如 V1-01 在 L303-312），且仍在逐日追加。
- **任务无 ID 级提交溯源**：`git log --grep <任务ID>` 全部为空（提交信息不带任务 ID）；唯一总交付 `d51ed172`。审计只能依赖 `docs/plan/evidence/**` 证据文件，追溯强度弱于"任务↔提交"直连。
- 台账已混排 4 种时态：设计（未交付）、已交付、条件项（待后续）、追加补全（"条件项闭环"/"补全"）。对"哪些真的完成了"缺乏一眼可读视图。

### 2.3 §6 文件级方案 vs 实交付偏差

- 方案 §6（L254-276）点名的前端文件（`MessageList.tsx`/`Composer.tsx`/`ContextBar.tsx`/`DockHandle.tsx` 等）多数未按名落地；实际实现为 `chat-core/`（`ChatMessageItem.tsx`、`SuggestionCards.tsx`、`use-ai-chat-session.ts`、`chat-css.ts`、`constants.ts`、`types.ts`）与 `dock/`（`AssistantDock.tsx`、`AssistantChatPanel.tsx`、`AssistantHelpPanel.tsx`、`AssistantMemoryChips.tsx`、`ProactiveSuggestionBar.tsx`、`AssistantAsyncAnswerCard.tsx`）。
- 方案 §6 L270 的 `assistant.dock.enabled` / `help.agent.enabled` 平台开关未实现（见 §3 第 8 行；V0-01 证据自述"沿用既有平台配置，不在本批改动"）。

### 2.4 §12 开放问题全部过期待收敛

- Q1-Q5（L971-980）的收敛时点多已过期（P0 结束 / P1 评审 / P2 启动前 / H1 评审），表格未更新结论；其中 Q5"模型分级路由策略与成本上限"标注"P2 启动前"收敛，而 P2 已全部交付、该问题未见闭环证据。

### 2.5 条件项与"悬空依赖"

- **`文案批次` 悬空**：L425 唯一出现（"建议文案 i18n 归文案批次"），另被 L520（"页面 i18n 同批"）、L559（"新增文案 i18n 同批"）等条件项引用，但从未定义为任务或批次。
- D0-02 条件项（L401）"拖拽调宽与边缘收纳、移动端动效待后续批次"：无后续批次安排。
- 其余条件项（O0-03 的"真实 PG 删除语义回归与全栈 E2E 待补"L798-799、P2 MSP 的"真实 PG 冒烟归联调"L749、V 系各"真实 Provider E2E 归联调"等）散落各处，无归并视图，有长期滞留风险。

---

## 3. 设计→任务映射缺口表

> 判定依据：§7 台账无对应任务 + 代码检索（HEAD/工作区）。行号均为 2026-10-19 快照；§6 所述在途文件的行号可能已漂移。

| # | 设计条目（方案位置） | 判定 | 证据 |
|---|---|---|---|
| 1 | 输入坞 `/` 快捷指令与 `@` 引用当前页对象（§3.1 L47） | ❌ | §7 无对应任务；`chat-core`/`dock` 全范围检索 `slash/斜杠/命令菜单` 零命中（子代理 gap2-input 第 4 条） |
| 2 | 附件/截图输入（§3.5 L100） | ❌ | §7 无对应任务；两界面输入坞仅 `Input.TextArea`（`AIChat.tsx:557`、`AssistantChatPanel.tsx:214`），无上传控件（gap2-input 第 1 条） |
| 3 | 语音输入（§3.5 L101，"P2 可选"） | 未安排 | 方案自标可选，§7 无任务；可维持"不做"，但应在 §12 收口 |
| 4 | handoff"转人工：打开页面/预填/指派"（§3.2 L65） | 🟡 | 仅埋点与导航钩子存在（`use-ai-chat-session.ts:658`、`RunTraceView.tsx:45`、`DiagnosticsDrawer.tsx:134` 等 9 处），无专项任务/验收 |
| 5 | **I0-01 交互状态机 + 错误字典**（§3.2 L52-68；§7 L737） | ❌ | 无进度块、无证据文件；全仓检索 `I0-01` 仅命中方案自身；`错误字典` 代码零命中；opening 骨架屏零命中。部分状态能力散落他批（停止生成、`confirmation-drawer.tsx:5`、`approval/index.tsx:127`、I0-04 回执），无"8 类状态组件测试"证据（子代理存疑节） |
| 6 | 拖拽调宽 / 边缘收纳（§3.1 L45；D0-02 条件项 L401） | ❌ | "待后续批次"无后续；在途批（§6）新增的是"宽度三档循环"，非拖拽/边缘收纳 |
| 7 | 双入口一致性："在侧栏继续 / 全屏打开"（§3.7 L119；§8 L910） | ⚠️ | HEAD 无实现（`双入口/一致性` 在 `components/ai` 零命中）；在途批方向为"工作区抑制 + Dock→工作区一次性交接"（§6），**与方案文本口径不同，需确认后更新方案** |
| 8 | 灰度开关 `assistant.dock.enabled`（§3.7 L118；§6 L270） | ❌ | 代码零命中（前端仅本地存储键 `assistant.dock.v1`，`lib/store/assistant-store.ts:11`；后端无 dock 开关）；V0-01 证据自述"沿用既有平台配置，不在本批改动"（`evidence/assistant-dock/V0-01-ticket-inline-trio-evidence.md`） |
| 9 | 预算与限流状态可见（§3.7 L117） | 🟡 | 步数/工具预算可见（`components/ai/run-status-bar.tsx:24-25,110-113`；`DiagnosticsDrawer`），无 token/费用/限流"预算"面 |
| 10 | §13.3 质量层：引用覆盖率 / 无引用回答率 / 点踩率（L1019） | ❌ 无数据源 | `stream_end` 不记 sources 数、遥测无 citation 类事件；反馈仅文档级 `doc_feedback`（`AssistantHelpPanel.tsx:130-142`）；详见 §5.2 |
| 11 | i18n（§3.7 L114 + L425 条件项） | ⚠️ | "文案批次"仅 L425 一处、未定义，被多处条件项引用 → 悬空依赖 |
| 12 | 流式 `aria-live`（§3.7 L113） | ❌ 验收缺口 | 消息区无 `role="log"/aria-live`（`AIChat.tsx:465-468`、`AssistantChatPanel.tsx:140`；全范围仅 `ProactiveSuggestionBar.tsx:36-37`）；I2-02 以 axe 扫描（0 violations）验收，覆盖不了该项 |
| 13 | §6 文件级方案一致性 | ⚠️ | 见 §2.3 |

（附注：§3.3 任务优先 / §3.4 三种 AI 形态 / §3.6 主动协助三形态均有实现并入台账——I0-02/I0-04/I1-01/I1-03 等；§3.5 中"划词问 AI"（L97）、"命令面板整合"（L99）已交付 I1-02/I0-03。）

---

## 4. 交付台账核验（§7：V / P0 / P1 / P2 / I 系列）

> 子代理 `plan-task-inventory` 方法：以「§7 进度块声明 + 证据文件实测存在 + 代码/产物 file:line + git 交叉」四重核验。

### 4.1 汇总

| 系列 | 任务数 | ✅ 已交付 | 🟡 | ❌ 未找到独立实现 |
|---|---|---|---|---|
| V（价值试点/补齐） | 9 | 9 | 0 | 0 |
| P0（D0×5 + H0×4） | 9 | 9（D0-05 分两批） | 0 | 0 |
| P1（D1×2 + H1×4） | 6 | 6 | 0 | 0 |
| P2（无 ID，4 条目） | 4 | 4 | 0 | 0 |
| I（交互补强） | 9 | 8 | 0 | **1（I0-01）** |
| **合计** | **37** | **36** | 0 | 1 |

- 全部引用证据文件实测存在（assistant-dock 28 个 / assistant-observability 11 个 / help-agent 13 个）。
- **唯一未交付：I0-01**（证据见 §3 第 5 行）。
- O 系列（O0-01 … O2-02，7 项）不在本表：§7 L767-887 均有"已交付"进度块（含多轮"条件项闭环/补全"），证据 11 个；其与 §13.3 设计口径的差距见 §5.2。
- 结构备注：`B4-02`（真实 usage 解析）进度块位于 O 区块内（L840-851），证据 `assistant-observability/B4-02-real-usage-evidence.md` 存在，但 §7 任务表无 B 系列条目——建议归位。

### 4.2 可信度评估

- §7 台账**声明层可信度高**：抽查的证据文件、代码 file:line、脚本/CI/迁移均实；"两批交付""条件项闭环"等标注与提交史（`d51ed172` + `2926ddc8` + `00817fc6` + `abd8705a`）吻合。
- 结构性弱点：无状态列、无 ID 级提交溯源、条件项无归并（§2.2）——"全绿"结论需要靠人工再核验，成本高。建议改为"任务 ID 表格化台账 + 证据链接"（§7-A）。

---

## 5. 子系统核验

### 5.1 帮助子系统（§5.1–5.8，H 系列）

状态总表（子代理 `plan-help-subsystem`）：

| 设计项 | 判定 | 摘要 |
|---|---|---|
| 5.1 单真源双投影 | 🟡 | docs-as-code 真源/校验/mkdocs/ledger 已落地；KB"使用手册"合并、help-index→向量库导入未实现 |
| 5.2 Page Manifest | 🟡 | 生成器与产物已交付；schema 与方案不符（`route`→`pattern`、`actions/botTools`→`suggestions`） |
| 5.3 Doc Ledger 与覆盖率 | 🟡 | ledger/ratchet/运行时查询存在；stale 口径与 endpoint 形态偏离方案 |
| 5.4 Help Agent 工具集 | 🟡 | 8 个实现 5 个；写面 3 个（update/submit_review/publish）零实现 |
| 5.5 AI 起草闭环 | 🟡 | 检测/校验/草稿/度量在；diff 预览、一键审阅、批量生成草稿不在 |
| 5.6 触发源（四类） | 🟡 | 1 项完整、2 项部分（无任意页显式入口；夜间产出 Issue 而非 PR） |
| 5.7 最少人工分级 | 🟡 | L1 执行器完整（默认关）；L2 无 diff 审阅 UI；L3 仅 prompt 规则；审计字段不全 |
| 5.8 与现有资产映射 | ✅ | 全部资产确认存在 |

关键缺口（file:line 示例）：

- **写面工具零实现**：`help_update_draft` / `help_submit_review` / `help_publish` 全仓零命中；代码自述"写面…属后续批次"（`itsm-backend/service/tool_registry_help.go:18`）。
- **diff 预览 / 一键审阅 / 批量草稿零命中**（`components/ai/dock` 检索 `diff|预览` 为空）；"让 AI 生成初稿"仅组装 prompt 切回对话（`AssistantHelpPanel.tsx:151-160,288-307`）。
- **help-index 无向量化**：检索为词法匹配（`H0-03-help-agent-tools-evidence.md:61` 自述）；KB"使用手册"分类/合并零命中；面板"合并来源"实为 MSP 分发 `distributedDocs`（`itsm-backend/handlers/ai/help.go:79-93`、`AssistantHelpPanel.tsx:227-254`）。
- **契约偏差**：`GET /ai/help/coverage` 无 `pageKey` 参数、返回全量账本（`handlers/ai/help.go:110-129`；单页状态由 `/ai/help/page?pageKey=` 承担）；stale 口径为 `last_reviewed` 缺失/超 90 天，与方案"git 元数据联动"不同（代码变更联动另走 H1-03 夜批）。
- **Page Manifest 字段**：`botTools` 全仓零命中（除方案 L176 自身）；生成源未消费后端工具目录。
- L1 自动合并：默认 dry-run、`DOCS_AUTO_MERGE=1` 才写、fail-closed、审计 JSONL（`P2-l1-auto-merge-evidence.md:10-27`）；但方案要求的"模型/prompt_version/diff/审阅人"统一留痕未达成（`prompt_version` 在 help 域零命中）。

### 5.2 观测子系统（§9 + §13，O 系列）

**事件字典**：38 项事件名**前后端白名单逐字对齐 ✅**（前端 `lib/ai/telemetry.ts:20-62`；后端 `handlers/ai/telemetry.go:26-66`）。但后端只提列 14 个独立字段（`telemetry.go:106-120`），其余进 `payload` JSONB（8KB 截断，`:213-219`），聚合能力受限；字段级缺口（节选）：

| 事件 | 缺口 |
|---|---|
| `assistant_opened/closed` | Dock FAB/Ctrl+I 路径不上报 opened；`closed` 全仓零上报 |
| `message_sent/first_token/stream_end` | `model` 零上报；token 仅在 payload、无聚合 |
| `tool_call` / `confirmation_*` / `approval_pending` | risk 未带；confirmation 缺 tool/risk/reason_code；`approval_pending` 零 track 调用 |
| `help_*` | `query_hash` 全链缺失 |
| `error_shown/recovered/handoff` | `error_recovered` 零上报；`recovery_action` 未带（仅 `lib/ai/telemetry.ts:106,193` 字段定义） |

**四层看板**（`/ai/observability`，单页卡片、无图层切换）：

- 采用层：打开率无曝光分母（Dock 不上报 opened）；零输入占比无埋点；入口分布未聚合（`entrypoint` 已入库）；活跃用户无 distinct users 聚合。
- 效果层：任务完成率 ✅；转人工率以 `task_failure_rate` 代理（`itsm-backend/service/ai_pilot_report.go:257`）；"单任务点击/输入字符"无指标。
- **质量层：引用覆盖率 / 无引用回答率 / 点踩率均无数据源**（`stream_end` 不落 sources 数；无 citation 事件；反馈仅文档级 `doc_feedback`）。verify 通过率（后端有 `VerifyStats.FailRate`，`service/bot/metrics.go:93-100`，未接看板）、错误率按 error_code 分组、重试成功率（无埋点）同样缺失。
- 成本层：P50 缺失；看板 P95 为**混合事件**口径且 health 把它标为"首 token P95"（`service/ai_health.go:315-330` 注释自认口径）；token 成本/任务、模型分布、预算超限率无数据源。

**结论**："O0-03 已交付四层看板"与"§13.3 指标面"之间存在**系统口径差距**——建议 v5 明确哪些指标降级为后续批次、哪些补采集（§7-B）；其中质量层三项与 §11.5 放量门槛直接相关，优先级最高。

---

## 6. 在途改动快照（并行修复批，P0–P3）

> 评审期间仓库存在**未提交**的并行修复批（非本评审所产出，作者会话仍活跃）。以下为 2026-10-19 13:0x 快照：`git status --short` 19 个文件、`git diff --shortstat` **+829/-170**。撰写后该批仍可能变化，收口时请以最新状态复核。

### 6.1 改动清单与设计意图

| 主题 | 关键实现（file:line） | 说明 |
|---|---|---|
| P0 工作区路由抑制 | `dock/AssistantDock.tsx:86-88`（注释）、`:146` `if (!canReadAssistant \|\| isWorkspaceRoute) return null;` | `/ai/chat` 工作区内不再渲染浮动 Dock |
| P1 宽度三档循环 | `lib/store/assistant-store.ts:28`（`ASSISTANT_DOCK_PUSH_THRESHOLD=560`、`ASSISTANT_WIDTH_TIERS=[400,560,720]`）、`normalizeAssistantWidth/maxAssistantWidthForViewport/nextAssistantWidth/handleCycleWidth`（store:71 一带）；Dock 按钮 `assistant-width-toggle`（aria-pressed）+ `data-width`、maxWidth `calc(100vw-320px)` | 以"三档循环"替代方案 L45 的"拖拽调宽" |
| P2 主内容推挤 | `layouts/MainLayout.tsx:172-173` `paddingRight: 'var(--assistant-dock-push, 0px)'` + `--assistant-dock-push` 变量 | Dock 展开时推挤而非遮挡 |
| P3 Dock→工作区一次性交接 | store `handoffConversationId`（不持久化）+ `setHandoffConversationId/consumeHandoffConversationId`（store:87-137）；`dock/AssistantChatPanel.tsx:54,57-67,157-167`"在工作区打开"按钮（生成中禁用）；`AIChat.tsx:161` `useAIChatSession({ consumeWorkspaceHandoff: true })`；`chat-core/use-ai-chat-session.ts:55-60,309-329` 消费 + `getConversationMessages` 回放 | 会话从 Dock 带入工作区继续；双端已接线 |
| 证据/时间线折叠 | `chat-core/evidence-panel.tsx`（+38）、`chat-core/tool-call-timeline.tsx`（+257，aria-expanded） | 降低长回答视觉噪音 |
| 后端会话历史连续性 | 新增 `itsm-backend/handlers/ai/chat_history.go`（`loadChatHistory/buildChatHistory`；水位 24→16 条、8000→6000 字符、单条 2000；fail-closed 租户+用户校验）；`service/rag_service.go`（+94）、`handlers/ai/repository_impl.go`（+5）、`handlers/ai/service.go`（+10） | 为 provider 前缀缓存提供确定性历史回放 |
| 测试 | 前端：`AssistantDock.test.tsx`、`AssistantChatPanel.test.tsx`、`use-ai-chat-session.test.tsx`、`assistant-store.test.ts`、`evidence-panel.test.tsx`、`tool-call-timeline.test.tsx`、e2e `flow-bot-workspace.spec.ts` / `flow-mcp-chat-chain.spec.ts`；后端：`chat_history_test.go`、`rag_history_messages_test.go`、`chat_history_continuity_test.go` | 覆盖 P0–P3 主路径 |

### 6.2 风险与需决策项

1. **未提交**：19 文件 +829/-170 仍在增长；本报告所有 file:line 均以此快照为准。
2. **未入 §7 台账**：`00817fc6` 的台账追加（L7 +18 行）只记载"浏览器走查 + 写路径审批闭环修复"，未覆盖 P0–P3 全套；若不登记，v5 台账将再次失真。
3. **与方案文本口径冲突（需决策）**：方案 §3.7 L119 / §8 L910 设计为"双入口并存 + 在侧栏继续 / 全屏打开"；在途实现为"工作区抑制 Dock + 一次性会话交接"。**二选一**：改实现（回到并存），或改方案（认可现状并更新 L119/L910）。
4. **验收强度**：该批 e2e 仅约 +2 行断言级改动；P3 依赖双端一致性，建议补"生成中禁用 + 交接后消息完整回放"用例。
5. **快照漂移**：本次评审撰写期间 HEAD `00817fc6` → `abd8705a`（新增"帮助面板加载中不得谎报未注册页面"修复）；未提交批 16→19 文件。评审数字以各节注明快照为准。

---

## 7. v5 修订建议

### A. 文档治理（低风险、建议立即做）

1. L4 状态头改为：`状态：实施完成（v5 修订待评审；I0-01 及 v5 补任务除外）`。
2. 修订史补 v5 条目，记录 `00817fc6` 走查与在途批（提交后）。
3. §7 台账表格化为独立文件：每任务一行，含**状态列**（✅/🟡/❌）、证据链接、提交哈希；`B4-02` 从 O 区块归位 B 系列。
4. §12 Q1–Q5 逐条补"结论/关闭"（或迁移收敛时点）。
5. §10 L930"实施前需评审确认"流程倒挂表述更新。

### B. v5 补任务（缺口兑现）

1. **I0-01 复活**：交互状态机 + 错误字典 + 8 类状态组件测试（含 opening 骨架屏）。
2. 输入坞：`/` 快捷指令与 `@` 页面对象引用；附件/截图输入（§3.5 L100-101）。
3. handoff"转人工"三项（打开页面/预填/指派）专项任务 + 验收。
4. 灰度开关 `assistant.dock.enabled` / `help.agent.enabled`（§6 L270）。
5. **质量层数据源**：citation 埋点（sources 数 / 无引用回答）、回答级点踩反馈、`model` 上报、`query_hash` 补链。
6. 流式 `aria-live`：补行为断言（a11y 验收不止 axe 静态扫描）。
7. 成本口径修正：区分"首 token P95 / 整体 P95"、补 P50（`service/ai_health.go:315-330` 自认口径问题）。
8. 帮助子系统收口：写面 3 工具、diff 预览/一键审阅 UI、help-index 向量化、coverage `pageKey`（§5.1）。
9. i18n"文案批次"落为真实任务，或显式关闭。

### C. 验收口径修订

- 双入口条款按 §6.2-3 决策改写；
- "拖拽调宽"→"宽度三档"（或恢复拖拽，二选一后统一）；
- "O0-03 四层看板交付"与"§13.3 指标可算"分开验收（差距见 §5.2）。

### D. 条件项并轨总表

- 全库"条件项/待后续批次"集中为"条件项登记表"（含责任批、触发条件、目标时点），防止滞留（§2.5）。

### E. 在途批收口

- 提交 P0–P3 修复批 → §7 台账登记 → 按 §6.2-4 补强验收。

### F. 放量门槛复核

- §11.5 中依赖质量层三指标（引用覆盖率/无引用率/点踩率）的门槛，在数据源补齐前应标注"暂不可测"，否则门槛不可执行。

---

## 附录

### 附-1 证据索引

- 评审对象：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`（v4，约 93 KB）
- 本评审：`docs/plan/ai-assistant-dock-plan-review-2026-10-19.md`（本文件）
- 前置差距分析：`docs/plan/ai-assistant-ux-review-gap-analysis-2026-10-19.md`
- 证据目录：`docs/plan/evidence/assistant-dock/**`（28 个）、`assistant-observability/**`（11 个）、`help-agent/**`（13 个）
- 子代理核验（只读、结果可再抽取）：`plan-task-inventory`、`plan-help-subsystem`、`plan-observability`
- 快照命令：`git status --short` / `git diff --shortstat` / `git log --oneline`（2026-10-19 13:0x）

### 附-2 快照与漂移注记

- 提交史交叉：`d51ed172`（主交付）→ `2926ddc8`（写路径审批缺陷修复）→ `00817fc6`（走查台账与证据）→ `abd8705a`（帮助面板加载态修复）。
- 在途批（未提交、非本评审产出）撰写时 19 文件 +829/-170；含 `AIChat.tsx` 工作区接线与 3 个新后端测试文件。
- **本评审为静态核验：未运行任何构建/测试**（`go test` / `npm test` / Playwright 均未执行）。
- 所有 file:line 以 2026-10-19 13:0x 快照为准；在途批提交后行号可能漂移。

### 附-3 复核路径（可复现）

- 四重交叉：方案文本 ↔ §7 台账 ↔ 证据文件 ↔ 代码/提交。
- 关键静态检索词（命中/零命中均记录）：`slash/斜杠/命令菜单`、`附件/上传`、`aria-live / role="log"`、`assistant.dock.enabled`、`I0-01`、`错误字典`、`botTools`、`help_update_draft`、`query_hash`、`assistant_closed`。
- 结论均可由上述检索 + `git log/show` 静态复核。

---

### 附-4 快照后更新（2026-10-19 午后，评审结论修正）

评审快照（`29edea81`）之后，在途批与后续修复已全部提交；对本评审结论做如下更新：

1. **I0-01 已交付**（`4a1fd4f8`，"交互状态机 + 错误字典（Dock 接入）"）：`itsm-frontend/src/lib/ai/interaction-state.ts`（10 状态 + 错误字典）+ `AssistantInteractionBanner` + 247 行测试。本文"唯一未交付项"结论作废；遗留项为**证据文件未落盘**（v5 补任务 T5-01）。
2. **在途批已提交**（§6 快照对象）：`cd61ce7f`（流式会话回放历史消息 + 稳定前缀）、`26512d69`（过程证据面板默认折叠）、`5d87a831`（Dock 宽度三档 400/560/720 + 键盘循环 + 推挤 + Dock→工作区一次性交接）；`4a1fd4f8` 亦属该批次工作（I0-01 雏形 → 正式提交）。四者均未附证据、未更新方案 §7。
3. **§3 缺口表变化**："拖拽调宽"更新为"已实现为离散三档 + 键盘循环（显式放弃自由拖拽，理由：WCAG 2.1.1 无纯指针操作 + 可测试性；`5d87a831`）"，相应条款回写为方案 T5-11；"双入口'在侧栏继续 / 全屏打开'"仍未实现（源码零命中），保留为待决策（方案 §14.4-① / T5-12）。
4. **§7-E 收口进展**：原"提交 P0–P3 修复批 → §7 台账登记"中，"提交"已完成（`4a1fd4f8`/`cd61ce7f`/`26512d69`/`5d87a831`）；"§7 台账登记"转入 v5 补任务 T5-01。
5. v5 修订（方案 §14 + 独立交付台账 `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`）已按上述事实落盘；本评审文件保留快照口径，不以事后信息改写正文。

---

### 附-5 补登记落地（2026-10-19，T5-01 完成）

1. **T5-01 完成**：批 7–10（`4a1fd4f8`/`cd61ce7f`/`26512d69`/`5d87a831`）证据补登记落盘——`assistant-dock/I0-01-interaction-state-evidence.md`（I0-01）、`assistant-dock/runtime-fixes-batch-2-evidence.md`（运行时修复批 2）；登记见交付台账 §E/§G/§H 与 T5 段。
2. **补登记实跑（2026-10-19）**：前端相关表面（`src/lib/ai/` + `src/components/ai/` + `src/lib/store/`）**54 suites / 417 tests 全过**；后端定向 **13/13 全过**（含真实 ChatStream 三轮历史回放 / 前缀稳定集成用例）。
3. **缺口状态**：评审 §2.1–2.2 的证据缺口关闭；§7-E 的"§7 台账登记"事项完成（改记独立交付台账，依 §14.5 惯例）；"双入口一致性"（§14.4-① / T5-12）与浏览器级补强验收仍待决策 / 排期。


---

### 附-6 双任务收口与 Q3 回写（2026-10-19，T5-11 / T5-12）

1. **T5-12（双入口，按 B 收口）**：§14.4-① 决策取"更新方案条款认可现状"——方案 §3.7「双入口一致性」条、§8 风险行已更新为"工作区抑制 Dock + 一次性会话交接（convId + 页面 scope，`5d87a831`）"；原"在侧栏继续 / 全屏打开"表述作废、不再列为缺口；§14.4-① 标记解除。一致性用例=既有回归：`AssistantDock.test.tsx`（工作区不渲染 Dock/FAB）、`AssistantChatPanel.test.tsx`（交接写入 + 导航 + L2 上下文）、`use-ai-chat-session.test.tsx`（工作区消费 / Dock 不截胡）。
2. **T5-11（宽度交互）**：方案 §3.1 已回写"离散三档 400 / 560 / 720 + 键盘循环（显式替代自由拖拽）"、§3.7 无障碍条同步；"边缘收纳"显式不采纳（收起态即 FAB，与 D1 一致）。
3. **Q3 连带回写**：launcher / `/ai/chat` 深链口径按现状保留（工作区为持久入口），原"随后统一改为打开 Dock"作废；§14.3 对应行关闭（回写见方案 §12 收口注）。
4. **快照语义与登记**：本评审正文保留快照口径，本节仅为附注；§3 缺口表第 6、7 项（拖拽调宽 / 边缘收纳、双入口）随本批处置关闭；交付台账 §H 与 T5 段已同步。

### 附-7 流式 aria-live 行为验收落地（2026-10-19，T5-08）

1. **§12 第 12 项缺口关闭**：消息区 live region 双表面落地——工作区 `AIChat.tsx` 与 Dock 面板 `AssistantChatPanel.tsx` 消息滚动容器均为 `role="log"` + `aria-live="polite"` + 流式期间 `aria-busy`（抑制逐字播报、完成后一次性读出）。
2. **验收方式 = 行为断言**（不依赖 axe 静态扫描）：新增 `streaming-a11y-behavior.test.tsx` 3 例（空态持久 log / busy=true→false 状态机 / 双表面），实跑 4 suites / 26 tests 全过（含 I2-02 axe 回归）；证据 `docs/plan/evidence/assistant-dock/T5-08-streaming-aria-live-evidence.md`。
3. **快照语义**：本评审正文保留快照口径，本节仅为附注。

### 附-8 i18n"文案批次"任务化（2026-10-19，T5-10）

1. **§2/§3/§7 悬空依赖关闭**：L69（§2 悬空清单）、§3 第 11 项与 §7 建议 9 中"文案批次"不再悬空——新设 **T5-13**「文案 i18n 批次」（范围 = 助手各表面新增文案；验收 = `useI18n` zh/en 键齐全；证据路径 `assistant-dock/T5-13-i18n-copy-batch-evidence.md`），方案 §14.2/§14.3 与交付台账已同步。
2. **快照语义**：本评审正文保留快照口径，本节仅为附注。


### 附-9 输入坞 `/` 快捷指令 + `@` 引用落地（2026-10-19，T5-02）

1. **§3 缺口表第 1 项关闭**：输入坞 `/` 快捷指令与 `@` 引用当前页对象已双表面落地（工作区 `AIChat.tsx` / Dock 面板 `AssistantChatPanel.tsx`）——共享模块 `chat-core/input-dock-commands.ts`（触发解析/过滤/替换）、`InputDockAssistMenu.tsx`（菜单）、`use-input-dock-assist.ts`（键盘交互）；命令集 v1 = `/工单` `/SLA` `/话术`（UX B4 示例，注册表可扩展）；`@` 列出 L2 当前页对象（无对象/无匹配时给提示，非静默）。
2. **验收（解析 + 交互测试）**：解析单测 20 例（`input-dock-commands.test.ts`）+ 双表面交互 4 例（`input-dock-assist.test.tsx`）——`/` 触发/过滤/↑↓/Enter 选择（菜单开启时不发送）/Esc 交还；`@` 插入 `@类型#编号 `。实跑新测 2 suites / 24 tests + 回归 8 suites / 61 tests 全过；证据 `docs/plan/evidence/assistant-dock/T5-02-input-dock-slash-and-reference-evidence.md`。
3. **§7 建议 2 部分落地**："输入坞：`/` 快捷指令与 `@` 页面对象引用"完成；"附件/截图输入"仍挂 T5-03。
4. **快照语义**：本评审正文保留快照口径，本节仅为附注。
