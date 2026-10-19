# AI 助手 UX 评审 × 代码现状差距分析

- 日期：2026-10-19
- 核验版本：仓库 HEAD `27ead101`（2026-10-19）；相关交付批次 `d51ed172`（`ai-assistant-dock-plan-2026-10-18`，1.8.0-dev）
- 输入：用户提供的两版《AI 助手页面 UX 评审》（内容高度重合，本文按合并口径处理）
- 方法：静态代码核验（文件级证据）+ 4 组并行只读核查 + 关键点人工复核；未运行前端
- 口径说明：评审稿描述的是"一个页面"，代码里实际是**两个界面**，评审把两者混在一起了：
  1. **工作区**：`/ai/chat` → `components/ai/AIChat.tsx`（左侧会话历史 + 中栏对话 + 底部输入坞）；
  2. **全局 Dock 面板**：`layouts/MainLayout.tsx:223` 挂载 → `components/ai/dock/AssistantDock.tsx` + `AssistantChatPanel.tsx`（右侧面板，含「对话 / 帮助」页签、任务卡、记忆条、后台精算）。
  二者共用 `chat-core`（`ChatMessageItem` / `use-ai-chat-session`），但**能力并不对称**，下文逐条区分。
- 状态标记：✅ 已实现｜🟡 部分实现｜❌ 缺失｜⚠️ 评审与现状不符（信息差）

---

## 0. 结论速览（先看这 8 条信息差）

1. ⚠️ **"缺少停止生成"——已实现**。工作区 header 与输入坞、Dock 输入区三处都有停止按钮（`AIChat.tsx:426-429,566-575`；`AssistantChatPanel.tsx:234-241`）；停止后落 `error:'已停止生成'` 以 Alert 呈现（`use-ai-chat-session.ts:521-524`），不是静默截断。评审描述与现状不符。
2. ⚠️ **"中栏几乎空白、引导落在右栏"——与现状不符**。工作区空态**中栏**已有图标 + 标题 + 说明 + 建议卡（`AIChat.tsx:470-498`）；Dock 空态也有「结合知识库回答当前页面的问题」+ 建议卡（`AssistantChatPanel.tsx:141-157`）。"引导位置不合理"的批评不成立，真正的问题是**两个入口同屏时引导重复**（见第 4 条）。
3. ⚠️ **"引用呈现方式没有任何说明"——部分成立**。已有「引用来源 · N」块：objectType 标签 + 标题 + 相关度百分比 + **可展开原文片段**（`ChatMessageItem.tsx:341-392`）。确实缺：编号角标/脚注锚点、点击跳转（标题是纯文本）、更新时间戳、低置信度声明。评审方向成立，但"没有任何说明"不准确。
4. ✅ **"两个输入框 + 两段相同提示"——成立**。Dock 全路由可用（`MainLayout.tsx:223`；唯一拦截是权限 `AssistantDock.tsx:113`），`/ai/chat` 页面上打开 Dock 即出现双输入框；两个 placeholder 完全相同（均为「请输入你的问题…」，`AIChat.tsx:561` / `AssistantChatPanel.tsx:219`），提示文案**近似但不完全相同**（「Enter 发送，Shift+Enter 换行 · 回答由 AI 生成，请自行甄别」vs「Enter 发送 · 回答由 AI 生成，请自行甄别」）。
5. ⚠️ **任务卡示例文案与仓库不符**。三张卡中只有「汇总我最近处理过的工单」真实存在（`registry.ts:500`）；「分析近期工单量变化趋势」「起草常见问题回复话术」在 `itsm-frontend/src` 全量搜索零命中——评审依据的卡片清单可能来自演示产物、旧版本或记忆偏差，需与截图/环境核对。
6. ✅ **「任务卡（零输入可执行；只读）」确实是可见组头**（`SuggestionCards.tsx:221`），"实现语言暴露给用户"的批评成立（卡片自身还带 `title="任务卡（只读）"`，`:195`）。
7. ⚠️ **"记忆无解释、不知道记了什么"——部分成立**。管理面已有：可查看（常用语/模板/最近处理方式）、逐条删除、清空、存储说明「记忆仅存本机，不上传」（`AssistantMemoryChips.tsx:178-280`）。确实缺：编辑、首次使用说明、以及**"它如何影响回答"**——且按代码现状，记忆只做输入框预填（`AssistantChatPanel.tsx:74-84`），**不注入回答**，这一点必须向用户说清楚（改文案或改能力）。
8. ❌ **真正缺失清单**（评审方向成立）：附件/截图上传、斜杠命令、重新生成/编辑重发、回答级「这条没用」反馈、低置信度声明、转人工出口（建单出口已有）、任务卡参数化/收藏/排序/自定义、Dock 随进度自动收起、消息区无障碍播报（`aria-live`）。

---
## 1. 逐条差距对照

### A. 首屏与信息架构

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| A1 | 右栏内容随对话进度变化：空态中栏展示任务卡/示例；首轮后右栏收缩为抽屉 | 内容按 `isEmpty` 切换（空态提示+卡 ↔ 消息列表）；开合纯手动（Ctrl/Cmd+I、ESC、关闭按钮、FAB） | 🟡 | `AssistantChatPanel.tsx:141,158`；`AssistantDock.tsx:85-92,377-384` | "随进度变化"需新增规则（如首轮后收起引导/建议区），当前都没有；但"空态把引导放中栏"在工作区已实现，评审此点与现状不符 |
| A2 | 两个输入框 + 两段相同提示 | 可同屏：Dock 无路由例外；placeholder 相同；提示近似 | ✅ 成立 | `MainLayout.tsx:223`；`AIChat.tsx:561,595`；`AssistantChatPanel.tsx:219,262` | 建议 `/ai/chat` 隐藏 Dock 入口（或隐藏工作区内重复提示），并区分两个 placeholder；"完全相同"需微调为"仅 Enter 提示近似" |
| A3 | 「任务卡（零输入可执行；只读）」是实现语言 | 是**可见组头**，卡片自身还有 title「任务卡（只读）」 | ✅ 成立 | `SuggestionCards.tsx:221,195` | 直接替换为用户语言，如「常用任务 · 点击即可开始」 |
| A4 | 「结合知识库回答当前页面的问题」是实现语言 | 真实存在，位于 Dock 空态（建议卡上方）；工作区同位置文案不同 | ✅ 成立 | `AssistantChatPanel.tsx:151`；`AIChat.tsx:490` | 补「点了会发生什么/能改哪些条件/结果能否带走」的价值说明 |

### B. 输入区与对话交互

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| B1 | 知识库/数据范围选择 | 入口上下文 chip 已实现（可清除、随请求回传）；**检索范围选择器没有** | 🟡 | chip：`AIChat.tsx:355-371`；`AssistantDock.tsx:282-297`；`use-ai-chat-session.ts:75-76,96,438`。无范围控件：工作区唯一 Select 是 Provider（`AIChat.tsx:375-394`） | chip ≠ 范围选择器，评审两条要拆开写 |
| B2 | 附件与截图上传 | 两界面输入坞均只有 TextArea | ❌ | `AIChat.tsx:557`；`AssistantChatPanel.tsx:214` | 方案 §3.5 已把"附件/截图"列为输入通道（未交付） |
| B3 | 模式切换（快速答 vs 深度检索） | Dock 有「后台精算」（先给快速参考+完成通知）；工作区无；无显式档位切换 | 🟡 | `AssistantChatPanel.tsx:87-96,224-233`；`AssistantAsyncAnswerCard.tsx`；`AssistantDock.tsx:215-232` | V2-02 只落在 Dock；若要做"模式"，建议复用异步口径并命名清楚 |
| B4 | 斜杠命令 `/工单` `/SLA` `/话术` | 无 | ❌ | 全范围无 slash/命令菜单命中；键盘交互仅 `Ctrl/Cmd+I`（`AssistantDock.tsx:85`） | 方案 §3.1 面板结构已列 `输入坞（/ 快捷指令、@ 引用）`，属设计内未交付 |
| B5 | 生成过程提示（检索…命中 3 篇…归纳） | 有：首字前「检索知识库并生成回答…」、运行状态条、工具时间线/证据面板、「生成中」Tag；**无"已命中 N 篇"阶段** | 🟡 | `ChatMessageItem.tsx:157-171,227-250`；`run-status-bar.tsx:63-67,96`；`tool-call-timeline.tsx:100-107`；`AIChat.tsx:420-424` | 命中数只在**结果后**的「引用来源 · N」出现（`ChatMessageItem.tsx:355`）；建议增加检索阶段事件 |
| B6 | 停止生成 | 两界面已实现 | ✅ | `use-ai-chat-session.ts:610-612,521-524`；`AIChat.tsx:426-429,566-575`；`AssistantChatPanel.tsx:234-241` | 停止后以 error Alert 呈现，文案「已停止生成」；可考虑"停止后可直接重发"的衔接 |
| B7 | 重新生成 / 编辑上一条重发 | 无 | ❌ | 用户气泡纯展示（`ChatMessageItem.tsx:116-136`）；无 regenerate/重发控件 | 对话产品底线能力，评审此点成立 |
| B8 | 每条回答下方：复制 / 引用跳转 / 「这条没用」 | 复制✅；引用跳转❌；回答级反馈❌ | 🟡 | 复制：`ChatMessageItem.tsx:290-298`；引用块无链接：`:342-392`（`RagAnswer` 无 url，`ai-api.ts:13-28`）；`EvidencePanel` 有 `targetUrl`/`onOpenTarget` 但生产未接线（`ChatMessageItem.tsx:242-247`；`evidence-panel.tsx:32-44,174-184`）；"无用"仅帮助 Tab 的已解决/未解决（`AssistantHelpPanel.tsx:256-285`，`doc_feedback`） | 高优先级：引用跳转是"证据链"闭环的关键断点 |
| B9 | 流式输出 + `aria-live` | SSE 流式✅；消息区无障碍播报❌ | 🟡 | 流式：`ai-api.ts:1388,1448,1473`；消息区无 `role="log"/aria-live`（`AIChat.tsx:465-468`；`AssistantChatPanel.tsx:140`）；全范围仅建议条有 `role="status" aria-live="polite"`（`ProactiveSuggestionBar.tsx:36-38`） | 屏幕阅读器用户体验目前是"无播报"，比评审预期的"重复朗读"更严重 |

### C. 任务卡

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| C1 | 点击后应"预填参数化模板"而非直接发送 | 现状=**直接发送**（无 handler/route 时立即 `sendPrompt`）；例外：页面注册 handler（如变更影响分析）或跳转路由 | ✅ 现状一致（即评审所述行为） | `SuggestionCards.tsx:172-175`；`assistant-actions.ts:77-82`；`ChangeDetail.tsx:95` | 唯一"预填不发"的链路是记忆 chip（`AssistantChatPanel.tsx:74-84`），可复用该模式做任务卡预填 |
| C2 | 参数化变量（时间范围/类型/渠道）与可修改高亮 | 无参数化；无"近 7/30/90 天"类提示 | ❌ | `registry.ts` 全量无数字天数；任务卡 prompt 为固定句式（如 `:500-501`） | 评审方向成立 |
| C3 | 使用频次排序、收藏置顶、「最近用过」、自定义卡片 | 无 | ❌ | 顺序=动态任务→静态任务→动态问题→静态问题（`SuggestionCards.tsx:91-97`）；`rank` 仅埋点序号（`:131-146`） | V2-01 记忆条与任务卡无耦合；需新增持久化+排序层 |
| C4 | 空态引导卡进入中栏、可一键运行 | 已实现（工作区空态中栏 + Dock 空态） | ✅ | `AIChat.tsx:470-498`；`AssistantChatPanel.tsx:141-157` | 评审"引导在右栏窄栏"描述不准确 |

### D. 引用与可信度

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| D1 | 编号角标 / 脚注 / 可点击跳转 / 展开原文片段 | 展开片段✅、相关度✅、标题✅；编号角标❌、点击跳转❌ | 🟡 | `ChatMessageItem.tsx:341-392`（`ellipsis expandable` :377-385） | 引用块是"块级列表"而非"结论句后角标"；跳转需接 `onOpenTarget` |
| D2 | 知识库版本日期 / 更新时间戳 | 标题✅；MSP 分发帮助显示 `v{sourceVersion}`；聊天引用无更新/复核日期 | 🟡 | `AssistantHelpPanel.tsx:246-248`；字段已声明未渲染：`ai-api.ts:21-27`（`lastReviewedAt/validUntil` 等） | 后端已下发字段，前端零渲染，属低成本补齐项 |
| D3 | 低置信度/检索为空主动声明 | 空检索有固定兜底话术；低置信度声明❌；后端 `degraded` 被丢弃 | 🟡 | `use-ai-chat-session.ts:544`；`aiSearchKB` 解构后未返回 degraded：`ai-api.ts:220-231`；自助场景有"知识库暂未命中"（`SelfServiceAssistant.tsx:164-170`） | 建议：sources 为空/低分 → 「未找到直接依据，以下为通用建议」 |
| D4 | 能力边界声明 + 转人工/创建工单出口 | 建单出口✅（回执"去创建工单（字段草案）"→ `/tickets/create`；`create_ticket` 走审批中心）；转人工 UI❌；统一边界声明❌ | 🟡 | `outcome.ts:37-42,54`；`SelfServiceAssistant.tsx:151-159,164-183`；`handoff` 事件语义=打开助手/导出报障（`ExplainErrorButton.tsx:71` 等） | 方案 §3.2 已定义 handoff 状态（未交付） |
| D5 | 「回答由 AI 生成，请自行甄别」出现两处 | 属实，且为硬编码、未走 i18n | ✅ 成立 | `AIChat.tsx:595`；`AssistantChatPanel.tsx:262`；`translations.ts` 零命中 | 建议换成具体边界声明（引用数/未覆盖范围）+ 一键转人工/建单 |

### E. 记忆面板

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| E1 | 可查看/可编辑/可逐条删除 + 首次说明 + 如何影响回答 | 查看✅、逐条删除✅、清空✅、存储说明✅（本机不上传）；编辑❌、首用说明❌、影响说明❌（现状仅预填输入框，不注入回答） | 🟡 | `AssistantMemoryChips.tsx:178-280,257-263,303-313`；预填链路 `AssistantChatPanel.tsx:74-84`；数据源 `lib/ai/memory/**` | 评审"不知道记了什么/能不能删"部分不成立；"如何影响回答"必须说明（或改名"助手记住的偏好（仅预填）"） |

### F. 视觉、可达性与响应式

| # | 评审条目 | 现状 | 状态 | 证据 | 修正说明 |
|---|---|---|---|---|---|
| F1 | 1280px 以下三栏挤压；右栏收起规则/断点；窄屏降级底部抽屉 | Dock：`<768px` 为 header 以下**全宽全高面板**（非带把手 Bottom Sheet，无遮罩）；桌面宽度默认 400 可持久化但无拖拽/钳制 UI；工作区左栏 `<768` 默认收起；chat-css 无断点 | 🟡 | `AssistantDock.tsx:39,74-82,247-264`；`assistant-store.ts:16,43,61`；`use-ai-chat-session.ts:67-69`；`chat-css.ts` 全 24 行 | "底部抽屉"结论需修正为"全屏面板"；若目标真是 Bottom Sheet，需补高度/遮罩/焦点管理 |
| F2 | 焦点环/Tab 顺序/aria-label/aria-live/快捷键可发现 | 输入框无 aria-label❌；消息区无 aria-live❌；Dock `role="dialog"` 但无 `aria-modal`/焦点陷阱❌；a11y 测试未覆盖 Tab/焦点且禁用对比度检查🟡；快捷键只在脚注/FAB Tooltip 出现🟡 | 🟡 | `AIChat.tsx:557-565`；`a11y-surfaces.test.tsx:88,113-316`；`AssistantDock.tsx:244-249`；`chat-css.ts:10-16`；`AssistantDock.tsx:173` | 焦点可见依赖 antd 默认，未验证；建议补 `:focus-visible`、`aria-modal`、快捷键帮助章节 |

### G. 文案对照表核验

| 评审"现在" | 是否真实存在 | 代码现状 |
|---|---|---|
| 任务卡（零输入可执行；只读） | ✅ 存在 | `SuggestionCards.tsx:221` 组头；卡片另有 title「任务卡（只读）」`:195` |
| 分析近期工单量变化趋势 | ❌ 不存在 | `itsm-frontend/src` 全量搜索零命中（"工单量"仅出现在分析类页面，非任务卡） |
| 起草常见问题回复话术 | ❌ 不存在 | 全量搜索"话术"零命中 |
| 汇总我最近处理过的工单 | ✅ 存在 | `registry.ts:500`（ai.chat 任务卡，prompt `:501`，`tools:['list_tickets']`） |
| 回答由 AI 生成，请自行甄别 | ✅ 存在 ×2 | `AIChat.tsx:595`；`AssistantChatPanel.tsx:262`（内容近似，工作区版含 Shift+Enter 说明） |
| 记忆 | ✅ 存在（按钮文本） | `AssistantMemoryChips.tsx:303-313`（aria-label「记忆管理」） |

> 说明：评审三张示例卡中两张不存在，**建议以环境截图核对**评审观察的是哪个部署版本；本文其余条目按合并口径核验。

### H. 度量口径核验（评审提出的衡量方式）

| 指标 | 现状 | 证据 |
|---|---|---|
| 首答延迟 | ✅ 已埋点 | `chat-telemetry.ts:105-109`（`first_token` + `latencyMs`） |
| 任务卡运行率 | ✅ 已有完成率口径 | `ai-create-metrics.ts:37,51-55,75-81`；看板 `pages/(main)/ai/observability/index.tsx:285-287` |
| 引用点击率 | ❌ 既无点击入口也无引类事件 | `telemetry.ts` 无 citation 类事件；`SourceList` 无链接（见 B8/D1） |
| 点踩率 | 🟡 仅帮助文档粒度 | `telemetry.ts:47-48`（`doc_feedback`）；`AssistantHelpPanel.tsx:130-142`；对话回答无反馈入口 |
| 追问率 / 会话放弃率 | ❌ 未找到事件口径 | 事件字典（方案 §13.2）未定义 |
| 引用覆盖率 / 无引用回答率 | ❌ 方案 §13.3 质量层要求，缺数据源 | `stream_end` 未记录 sources 数量（`chat-telemetry.ts:122-141`） |

---

## 2. 修正后的真实差距清单（按可执行优先级重排）

> 与评审优先级的差异：评审把「停止/重发/过程提示」打包为 P0——其中**停止已实现**；真正卡住"能否用于对外回复"的是**引用溯源未接线 + 无低置信度声明 + 质量指标缺数据源**。重排如下：

### P0 · 信任链与底线体验（决定工具能否被真正使用）
1. **引用可点击溯源**：`ChatMessageItem` 接线 `EvidencePanel` 的 `onOpenTarget`（现有能力被闲置，`evidence-panel.tsx:32-44,174-184`）；`SourceList` 对可跳转来源渲染链接/补编号，并在引用块展示 `lastReviewedAt`（字段已有）。
2. **低置信度 / 无依据声明**：`aiSearchKB` 上抛 `degraded`（`ai-api.ts:220-231` 现被丢弃）；sources 为空或低分时输出"未找到直接依据，以下为通用建议"。
3. **会话级可访问性**：消息区 `role="log" aria-live="polite"`；输入框补 `aria-label`（两界面）；`RunStatusBar` 补 `role="status"`；Dock 面板补 `aria-modal`/初始聚焦。
4. **回答反馈 + 引用覆盖率埋点**：对话回答补"这条没用"入口；`stream_end` 增 `sourceCount/hasCitations`（否则 §13.3 质量指标与评审建议的"引用点击率"都无数据）。

### P1 · 可控感（评审方向成立、按现状加速）
5. **双输入区去重**：`/ai/chat` 隐藏 Dock 入口或隐藏重复提示；区分 placeholder，明确唯一主输入区。
6. **检索过程提示**：增加"已命中 N 篇 → 正在归纳"阶段（当前只有首字前占位与工具时间线）。
7. **重发 / 重新生成 / 编辑上一条**（`ChatMessageItem` 操作区扩展）。
8. **任务卡预填参数化**：复用记忆 chip 的"只填不发"模式（`AssistantChatPanel.tsx:74-84`），变量高亮 + 确认后执行。
9. **附件/截图上传**（方案 §3.5 设计内未交付）。
10. **斜杠命令**（方案 §3.1 设计内未交付；可与 I0-03 命令面板一并做）。

### P2 · 长期与个性化
11. **记忆**：补编辑、首次说明、"如何影响回答"（或明确改文案为"仅预填"）。
12. **任务卡运营**：使用频次排序、收藏置顶、"最近用过"、自定义卡片。
13. **响应式细化**：Dock 桌面宽度拖拽/钳制；窄屏改为真正的 Bottom Sheet（高度限制 + 遮罩 + 焦点管理）；chat-css 补断点规则。

---

## 3. 与方案（`docs/plan/ai-assistant-dock-plan-2026-10-18.md`）的映射

| 差距项 | 方案出处 | 性质 |
|---|---|---|
| 附件/截图上传 | §3.5 输入通道 | 设计内未交付 |
| 斜杠命令（`/` 快捷指令） | §3.1 面板结构 | 设计内未交付 |
| 流式 `aria-live`、键盘可达 | §3.7 WCAG 2.1 AA | 设计内未达成 |
| 转人工（handoff 状态） | §3.2 交互状态机 | 设计内未交付 |
| 引用渲染（RagAnswer） | §5.8、§13.3 质量层指标 | 已交付一半，点击/覆盖率未接线 |
| 点踩率 / 引用覆盖率 / 无引用回答率 | §13.3 四层看板-质量层 | 指标口径已定，数据源缺失 |
| 任务卡预填参数化、收藏、自定义 | 方案未要求 | 评审新增需求（需产品确认） |
| Dock 随对话进度收起 | 方案未要求 | 评审新增需求（需产品确认） |

---

## 4. 不确定项与建议验证

1. **评审所看版本**：两张任务卡文案不在当前代码中，需对照截图/部署环境确认评审版本（可能为演示产物或旧版本）。
2. **后端 confidence / degraded 链路**：对话 SSE 事件结构（`ai-api.ts:1135-1147`）无 confidence 字段；`itsm-ai-service` 的 citation/confidence 字段未核实（前端范围内确实没有消费点）。
3. **运行时表现**：双输入框同屏的实际视觉效果（z-index/遮挡）、焦点可见性依赖 antd 默认样式，均建议浏览器实测（配合 axe）。
4. **a11y 测试实际通过性**：本次未运行测试，`a11y-surfaces.test.tsx` 覆盖 6 个面且禁用对比度规则。

## 5. 建议的下一步（供选择）

- 对 P0 第 1/2 条做一次小改造（接线 `onOpenTarget` + `degraded` 上抛 + 声明文案），可用现有测试面做回归；
- 或先补 B8/D1 的**运行时截图复现**，再决定 Dock 与工作区的收敛策略（双入口去重方案）。

---

### 附录 · 界面与关键文件索引

| 界面 | 入口 | 关键文件 |
|---|---|---|
| 工作区 | `/ai/chat`（`pages/(main)/ai/chat/index.tsx`） | `AIChat.tsx`、`chat-core/ChatMessageItem.tsx`、`chat-core/use-ai-chat-session.ts` |
| Dock 面板 | 全路由 FAB / `Ctrl+I`（`layouts/MainLayout.tsx:223`） | `dock/AssistantDock.tsx`、`dock/AssistantChatPanel.tsx`、`dock/AssistantMemoryChips.tsx`、`dock/AssistantHelpPanel.tsx` |
| 任务卡/建议 | 两界面共用 | `chat-core/SuggestionCards.tsx`、`lib/ai/page-context/registry.ts` |
| 引用/证据 | 消息内 | `chat-core/ChatMessageItem.tsx`（SourceList）、`evidence-panel.tsx`、`lib/ai/outcome.ts` |
| 埋点 | 两界面共用 | `lib/ai/telemetry.ts`、`lib/ai/chat-telemetry.ts`、`lib/ai/ai-create-metrics.ts` |

---

## 勘误与时效说明（2026-10-19 增补，评审时点后）

1. **"双入口"结论更新**：本分析中关于"两入口均无路由级抑制、唯一拦截是权限"的结论在评审快照时点（HEAD 未含抑制逻辑）仍成立；但工作区随后出现**未提交在途修复批**：`dock/AssistantDock.tsx:146` 已加入 `isWorkspaceRoute` 抑制（`/ai/chat` 下不渲染 Dock），并实现 Dock→工作区一次性会话交接（`handoffConversationId` + `AIChat.tsx:161` `consumeWorkspaceHandoff`）。该批**未提交**，且与方案 L119"双入口并存"口径冲突，需二选一决策（详见评审报告 §6.2-3）。
2. **时效**：本分析其余 Dock 相关条目请按"评审快照（2026-10-19 13:0x）"理解；引用行号可能随在途批提交而漂移。
3. **配套文档**：方案 v4 与交付台账的对照核验见 `docs/plan/ai-assistant-dock-plan-review-2026-10-19.md`。

4. **更新第 1 条（在途批已提交）**：上述修复批已于 `4a1fd4f8`/`cd61ce7f`/`26512d69`/`5d87a831` 提交；其中 `5d87a831` 落地 Dock 宽度三档 400/560/720 + 持久化 + 键盘循环 + 视口钳制（显式替代自由拖拽，理由：WCAG 2.1.1 无纯指针操作 + 可测试性），故 F1 行"无拖拽/钳制 UI"与建议 13 的"拖拽/钳制"表述请按此更新；"窄屏 Bottom Sheet"建议仍待办。双入口条款冲突保留待决策（详见评审报告 §6.2-3 与方案 §14.4-①）。

5. **收口（2026-10-19，T5-11 / T5-12）**：双入口冲突已按 B 收口——方案 §3.7/§8 条款更新认可现状（工作区抑制 Dock + 一次性交接；§14.4-① 解除）；宽度交互按"离散三档 + 键盘循环"回写方案 §3.1/§3.7（显式替代自由拖拽），"边缘收纳"不采纳（收起态即 FAB）。本文 F1 行与建议 13 的"拖拽/钳制"表述按第 4 条更新为准、至此关闭；"窄屏 Bottom Sheet"仍为待办项。

6. **消息区 aria-live 缺口关闭（2026-10-19，T5-08）**：第 24 条 / B9 / F2 中"消息区无 `role="log"/aria-live`、屏幕阅读器无播报"的缺口已关闭——工作区与 Dock 面板消息区均为 `role="log"` + `aria-live="polite"`，流式期间 `aria-busy` 抑制逐字播报、完成后一次性读出；行为断言 `streaming-a11y-behavior.test.tsx`（3 例，独立于 axe 扫描；证据 `docs/plan/evidence/assistant-dock/T5-08-streaming-aria-live-evidence.md`）。第 117 条建议中"输入框补 `aria-label`、`RunStatusBar` 补 `role="status"`、Dock 补 `aria-modal`/初始聚焦"等其余条目维持原口径（未变）。
