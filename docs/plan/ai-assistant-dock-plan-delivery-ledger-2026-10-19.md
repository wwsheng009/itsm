# AI 助手 Dock 方案 · 交付台账（截至 2026-10-19）

- 用途：v5 起承接方案 §7 任务的**逐项交付状态**；方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`（§14 为评审收口说明），评审：`docs/plan/ai-assistant-dock-plan-review-2026-10-19.md`。
- 抽取基线：`29edea81` 快照（2026-10-19 13:16）；其后变更（批 7–10）见下方"G. 批次与提交"；批 7–10 证据补登记见 §E/§G/§H 与 T5 段（2026-10-19，T5-01）。
- 状态口径：✅ 已交付（进度块 + 证据实存）｜✅* 代码已交付、证据待补｜❌ 未交付。
- 提交为批次级映射（无逐任务溯源，评审 §4.2 已记录）；证据文件均在 `docs/plan/evidence/` 下。

## A. V 系列（用户价值）—— 9/9 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| V0-01 | 工单详情三件套（分诊 + 答复草稿 + 知识推荐，Inline 优先） | ✅ | assistant-dock/V0-01-ticket-inline-trio-evidence.md |
| V0-02 | 一句话建单 + 自助问答（员工侧） | ✅ | assistant-dock/V0-02-self-service-ai-create-evidence.md |
| V0-03 | 变更影响分析任务卡（plan 产物） | ✅ | assistant-dock/V0-03-change-impact-plan-card-evidence.md |
| V0-04 | 试点对照报告 + 放量门槛（§11.5） | ✅ | assistant-observability/V0-04-pilot-report-gate-evidence.md |
| V1-01 | 列表页批量 AI（分类 / 草稿 / 分派） | ✅ | assistant-dock/V1-01-batch-ai-evidence.md |
| V1-02 | 员工侧入口覆盖（移动端 / 门户 / 邮件） | ✅ | assistant-dock/V1-02-employee-entry-coverage-evidence.md |
| V1-03 | 口语↔术语确认 | ✅ | assistant-dock/V1-03-intent-interpretation-evidence.md |
| V2-01 | 个性化记忆（可关闭） | ✅ | assistant-dock/V2-01-personalized-memory-evidence.md |
| V2-02 | 异步任务体验（先答后精 + 通知） | ✅ | assistant-dock/V2-02-async-task-experience-evidence.md |

## B. P0（Dock 底座 + 帮助骨架）—— 9/9 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| D0-01 | chat-core 抽离 | ✅ | assistant-dock/D0-01-chat-core-extraction-evidence.md |
| D0-02 | 常驻 Dock 底座（含移动形态；宽度三档后补于 `5d87a831`） | ✅ | assistant-dock/D0-02-assistant-dock-evidence.md |
| D0-03 | 页面上下文注册表（page_key 白名单） | ✅ | assistant-dock/D0-03-page-context-registry-evidence.md |
| D0-04 | 页面上下文协议（/ai/assistant/context） | ✅ | assistant-dock/D0-04-page-context-protocol-evidence.md |
| D0-05 | 遥测接线 + 建议 / ContextBar（分两批） | ✅ | assistant-dock/D0-05-chat-telemetry-wiring-evidence.md；assistant-dock/D0-05-suggestions-context-bar-evidence.md |
| H0-01 | 帮助文档种子（docs/help 规范） | ✅ | help-agent/H0-01-help-docs-seed-evidence.md |
| H0-02 | Page Manifest + Doc Ledger | ✅（schema 与方案有偏差，评审 §5.1） | help-agent/H0-02-page-manifest-doc-ledger-evidence.md |
| H0-03 | Help Agent 工具集（读面） | ✅（写面 3 工具未实现，见方案 §14.2 T5-09） | help-agent/H0-03-help-agent-tools-evidence.md |
| H0-04 | 面板帮助视图 | ✅ | help-agent/H0-04-panel-help-view-evidence.md |

## C. P1（场景化 + 帮助闭环）—— 6/6 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| D1-01 | 助手上下文接口（/ai/assistant/context） | ✅ | assistant-dock/D1-01-assistant-context-evidence.md |
| D1-02 | 动作与页面上下文 | ✅ | assistant-dock/D1-02-actions-and-page-context-evidence.md |
| H1-01 | 文档待办队列 | ✅ | help-agent/H1-01-doc-todo-queue-evidence.md |
| H1-02 | 文档校验器 + CI | ✅ | help-agent/H1-02-docs-validator-ci-evidence.md |
| H1-03 | 夜间 stale 检测 | ✅ | help-agent/H1-03-nightly-stale-detection-evidence.md |
| H1-04 | 覆盖看板 + 待办持久化 + 草稿采纳度量（分三批） | ✅ | help-agent/H1-04-help-coverage-dashboard-evidence.md；help-agent/H1-04-todo-state-persistence-evidence.md；help-agent/H1-04-doc-draft-adoption-evidence.md |

## D. P2（智能化与运营；原方案无 ID，本表代号为 v5 台账所加）—— 4/4 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| P2-a | 动态建议 | ✅ | assistant-dock/P2-dynamic-suggestions-evidence.md |
| P2-b | L1 自动合并（默认 dry-run） | ✅（审计字段不全，评审 §5.1） | help-agent/P2-l1-auto-merge-evidence.md |
| P2-c | 文档运营报表 | ✅ | help-agent/P2-doc-metrics-report-evidence.md |
| P2-d | MSP 帮助空间 | ✅ | help-agent/P2-msp-help-space-evidence.md |

## E. I 系列（交互补强）—— 9/9 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| I0-01 | 交互状态机 + 错误字典（Dock 接入） | ✅（`4a1fd4f8`；33 例测试实跑通过） | assistant-dock/I0-01-interaction-state-evidence.md |
| I0-02 | 任务卡模型 | ✅ | assistant-dock/I0-02-task-card-model-evidence.md |
| I0-03 | 快捷键 + 命令面板 | ✅ | assistant-dock/I0-03-shortcuts-and-command-palette-evidence.md |
| I0-04 | Outcome Contract 回执 | ✅ | assistant-dock/I0-04-outcome-contract-evidence.md |
| I1-01 | Inline 一键应用（diff + undo） | ✅ | assistant-dock/I1-01-inline-apply-evidence.md |
| I1-02 | 划词问 AI + 错误解释 | ✅ | assistant-dock/I1-02-selection-and-error-explain-evidence.md |
| I1-03 | 主动协助（三形态 / 静默） | ✅ | assistant-dock/I1-03-proactive-assist-evidence.md |
| I2-01 | Autopilot 巡检 | ✅ | assistant-dock/I2-01-autopilot-supervision-evidence.md |
| I2-02 | 合规 + a11y（axe 扫描面） | ✅（流式 aria-live 行为验收已由 T5-08 补位，2026-10-19） | assistant-dock/I2-02-compliance-a11y-evidence.md；assistant-dock/O2-02-workspace-entry-a11y-evidence.md |

## F. O 系列（观测性与分析）—— 7/7 ✅

| 任务 | 内容 | 状态 | 证据（docs/plan/evidence/ 下） |
|---|---|---|---|
| O0-01 | 遥测 SDK | ✅ | assistant-observability/O0-01-telemetry-sdk-evidence.md |
| O0-02 | 遥测接收 + 保留清理 | ✅ | assistant-observability/O0-02-telemetry-ingest-evidence.md；assistant-observability/O0-02-retention-cleanup-evidence.md |
| O0-03 | 观测看板 | ✅（与 §13.3 指标口径差距，评审 §5.2 / T5-06） | assistant-observability/O0-03-observability-dashboard-evidence.md |
| O1-01 | 单任务轨迹 + 会话回放 | ✅ | assistant-observability/O1-01-run-trace-evidence.md；assistant-observability/O1-01-conversation-trace-evidence.md |
| O1-02 | 健康告警 + sweeper 订阅 | ✅ | assistant-observability/O1-02-health-alerts-evidence.md；assistant-observability/O1-02-sweeper-subscription-evidence.md |
| O2-01 | 分析增强 | ✅ | assistant-observability/O2-01-analysis-enhancements-evidence.md |
| O2-02 | 诊断抽屉 + token 用量 + 工作区入口 a11y（分三批） | ✅ | assistant-dock/O2-02-diagnostics-drawer-evidence.md；assistant-dock/O2-02-token-usage-evidence.md；assistant-dock/O2-02-workspace-entry-a11y-evidence.md |

## G. 批次与提交（`d51ed172` 起）

| # | 提交 | 说明 |
|---|---|---|
| 1 | `d51ed172` | 全量交付（V / P0 / P1 / P2 / I 主批） |
| 2 | `27ead101` | 全量交付部署证据与变更记录 |
| 3 | `2926ddc8` | 写路径审批闭环两处实测缺陷修复 |
| 4 | `00817fc6` | 写路径审批闭环浏览器实测证据 |
| 5 | `abd8705a` | 帮助面板首帧修复（"未注册页面"谎报） |
| 6 | `29edea81` | 帮助面板修复证据与回归走查结论 |
| 7 | `4a1fd4f8` | I0-01 交互状态机 + 错误字典（Dock 接入） |
| 8 | `cd61ce7f` | 流式会话回放历史消息 + 稳定前缀（后端多轮连续性） |
| 9 | `26512d69` | 过程证据面板默认折叠可展开 |
| 10 | `5d87a831` | Dock 宽度三档 + 持久化 + 键盘循环 + 推挤；会话历史下拉；Dock→工作区一次性交接 |

> 批 7–10 证据补登记（2026-10-19，T5-01）：#7 → `assistant-dock/I0-01-interaction-state-evidence.md`；#8–#10 → `assistant-dock/runtime-fixes-batch-2-evidence.md`。

## H. 例外与悬空（与方案 §14 同步）

- I0-01：已交付（`4a1fd4f8`）且证据补登记完成（`assistant-dock/I0-01-interaction-state-evidence.md`，T5-01）。
- 批次 7–10（`4a1fd4f8`/`cd61ce7f`/`26512d69`/`5d87a831`）：证据已补登记（T5-01，2026-10-19）；方案 §7 按 §14.5 惯例保留为历史增量、不回填。
- 悬空设计项（无任务映射）：`/` 与 `@`（T5-02，已关闭）、附件 / 截图（T5-03）、handoff"转人工"三项（T5-04）、灰度开关（T5-05）、质量层指标数据源（T5-06a，已关闭；看板接线 T5-06b）→ 方案 §14.2。
- 宽度交互"拖拽调宽"：已被 `5d87a831` 显式替代为离散三档 + 键盘循环（WCAG 2.1.1）；条款已回写（T5-11，2026-10-19）；"边缘收纳"显式不采纳（收起态即 FAB，与 D1 一致）。
- 双入口"在侧栏继续 / 全屏打开"：未实现；现为"抑制 + 一次性交接"（`5d87a831`）——已按 B 收口（T5-12，2026-10-19）：条款更新认可现状（方案 §3.7/§8）、§14.4-① 解除。
- 流式消息区 aria-live 验收缺口（评审 §12 第 12 项；UX B9/F2）：已由 T5-08 关闭（2026-10-19）——工作区/Dock 面板消息区 `role="log"` + `aria-live="polite"`，流式期间 `aria-busy` 抑制逐字播报、完成后一次性读出；行为断言 `streaming-a11y-behavior.test.tsx`；证据 `assistant-dock/T5-08-streaming-aria-live-evidence.md`。
- 输入坞 `/` 快捷指令 + `@` 引用当前页对象（评审 §3 第 1 项；UX B4 / 建议 10）：已由 T5-02 关闭（2026-10-19）——双表面接入共享辅助模块（解析/菜单/hook），菜单开启时 Enter 只做选择（不发送）；解析单测 20 例 + 交互 4 例；证据 `assistant-dock/T5-02-input-dock-slash-and-reference-evidence.md`。
- 文案 i18n 批次（V1-01 / V1-02 / 面板等；评审 §2/§3 悬空项）：已由 T5-13 关闭（2026-10-19）——12 个助手表面接入 `useI18n`；新增 `assistant` 命名空间（11 子命名空间、zh/en 各 213 键；键集/占位符对齐校验通过）；zh 值逐字不变；eslint/tsc 全绿；证据 `assistant-dock/T5-13-i18n-copy-batch-evidence.md`。
- 条件项（真实 Provider E2E、真库回归、Q1/Q2/Q4 口径等）：见方案 §14.3。

---

## T5. v5 补任务状态（方案 §14.2）

| ID | 状态 | 说明 |
|---|---|---|
| T5-01 | ✅ 2026-10-19 | 批 7–10 证据补登记：`assistant-dock/I0-01-interaction-state-evidence.md`（#7）、`assistant-dock/runtime-fixes-batch-2-evidence.md`（#8–10） |
| T5-02 | ✅ 2026-10-19 | 输入坞 `/` 快捷指令（`/工单` `/SLA` `/话术`）+ `@` 引用当前页对象：双表面接线共享模块；解析单测 20 例 + 交互 4 例；证据 `assistant-dock/T5-02-input-dock-slash-and-reference-evidence.md` |
| T5-06 | 拆分 | 质量层拆分为 T5-06a（数据源，已完成）/ T5-06b（看板接线，待排期） |
| T5-06a | ✅ 2026-10-19 | 质量层数据源：`message_feedback` + `sources_count` / `query_hash` / `model`；前端 SDK、回答级反馈、历史消息补链；后端迁移 4 列 + 2 索引、INSERT 18 列、质量三率 + 模型 Top 20；后端 3 包 + 前端 4 suites / 42 tests 全过，eslint/tsc/gofmt 全绿；证据 `assistant-dock/T5-06a-quality-data-source-evidence.md` |
| T5-06b | 待排期 | 质量层看板接线：summary `quality` / `models` 接入观测看板；§11.5 三指标标注可测 |
| T5-08 | ✅ 2026-10-19 | 流式 aria-live 行为验收：消息区 `role="log"` + `aria-live="polite"` + `aria-busy` 流式抑制（工作区/Dock 双表面）；行为断言 3 例；证据 `assistant-dock/T5-08-streaming-aria-live-evidence.md` |
| T5-10 | ✅ 2026-10-19 | i18n"文案批次"任务化：新设 T5-13 承接（方案 §14.2；§14.3 条件行改挂） |
| T5-11 | ✅ 2026-10-19 | 宽度交互回写：离散三档 + 键盘循环（显式替代自由拖拽）；"边缘收纳"不采纳（收起态即 FAB） |
| T5-12 | ✅ 2026-10-19 | 双入口按 B 收口：§3.7/§8 条款认可现状（抑制 + 一次性交接）；§14.4-① 解除；Q3（launcher/深链）连带回写 |
| T5-13 | ✅ 2026-10-19 | 文案 i18n 批次：12 个助手表面接入 `useI18n`；新增 `assistant` 命名空间（11 子命名空间、zh/en 各 213 键）；zh 值逐字不变；证据 `assistant-dock/T5-13-i18n-copy-batch-evidence.md` |
| T5-03 ~ T5-05、T5-06b、T5-07、T5-09 | 待排期 | 见方案 §14.2 |

> 维护记录（2026-10-19）：存量失败测试治理 6 套件 / 19 用例已修复（`4938945a`）；方案 `stock-test-failure-governance-plan-2026-10-19.md`，证据 `assistant-dock/T5-13-i18n-copy-batch-evidence.md` §5。

---

- 维护约定：本台账为 v5 起任务状态的**唯一入口**；方案 §7 `> 进度：` 块为历史增量。新增/变更任务须更新本表并附证据路径（可行时附提交哈希）。
