# T5-13 文案 i18n 批次（助手各表面）· 交付证据

- 任务：T5-13（方案 §14.2 v5 补任务｜验收：文案走翻译键、zh/en 键齐全）
- 来源缺口：§3.7 L116 文案批次悬空（承接 T5-10 任务化）；范围含 V1-01 / V1-02 等新增文案
- 交付日期：2026-10-19（代码 + 测试 + 文档同批提交）

## 1. 实现

新增 `itsm-frontend/src/lib/i18n/translations-assistant.ts`（505 行）：`assistant` 命名空间，11 个子命名空间——`panel / banner / suggestions / memory / async / outcome / portal / help / dock / batch / email`，**zh-CN / en-US 各 213 键**；由 `translations.ts` import 后挂到两语言根（`assistant: assistantTranslations['zh-CN' | 'en-US']`）。

12 个助手表面接入 `useI18n`（每文件均含 import + `const { t } = useI18n()`；下表为 `t()` 调用点计数）：

| 表面 | 文件 | `t()` 调用点 |
|---|---|---:|
| Dock 面板 | `components/ai/dock/AssistantChatPanel.tsx` | 29 |
| 建议卡 | `components/ai/chat-core/SuggestionCards.tsx` | 3 |
| 主动建议条 | `components/ai/dock/ProactiveSuggestionBar.tsx` | 5 |
| Dock 容器 | `components/ai/dock/AssistantDock.tsx` | 20 |
| 交互状态条 | `components/ai/dock/AssistantInteractionBanner.tsx` | 11 |
| 异步精算卡 | `components/ai/dock/AssistantAsyncAnswerCard.tsx` | 6 |
| 回执卡 | `components/ai/OutcomeReceipt.tsx` | 5 |
| 门户入口 | `components/ai/PortalAssistantEntry.tsx` | 3 |
| 批量助手面板（V1-01） | `components/ticket/TicketBatchAIPanel.tsx` | 38 |
| 记忆条 | `components/ai/dock/AssistantMemoryChips.tsx` | 39 |
| 帮助视图 | `components/ai/dock/AssistantHelpPanel.tsx` | 20 |
| 邮件意图建议（V1-02） | `pages/(main)/email-intake/components/EmailIntentSuggest.tsx` | 9 |
| **合计** | 12 个文件 | **188** |

口径：

- **zh-CN 值与原字面量逐字一致**（默认语言 zh-CN；既有中文断言测试全部通过），en-US 为新增译文。
- **键集/占位符对齐**：临时脚本解析数据对象校验——zh 213 / en 213；`missingEn`、`extraEn`、`placeholderMismatch`、`emptyValues` 均为空。
- **动态键**：Dock 宽度档位 `assistant.dock.widthTier*`（400/560/720）、回执 kind `assistant.outcome.kind*`、批量面板类别/优先级 `assistant.batch.category*` / `priority*`、记忆条最近动作 `assistant.memory.recent.*`（兜底规则与 `lib/ai/memory/types.recentActionLabel` 同口径）。
- **边界（内容/协议数据，不进本命名空间）**：
  1. 注册表卡片标题/提示词（前后端同源再生）与帮助文档正文（服务端数据）；
  2. 发送给模型的提示词模板——`PortalAssistantEntry.tsx:22-23` 的 `DEFAULT_PORTAL_PROMPT`、`AssistantHelpPanel.tsx:159-168` 的 `buildDraftPrompt`，以及 `lib/ai/interaction-state` 的提示词/错误码字典。理由：属内容/协议数据，切换语言会改变模型输入语义；如需多语言提示词另行任务化。
- 回执卡 kind 标签已改走 `assistant.outcome.kind*` 键；`lib/ai` 规则表保留给合同测试（头注释已注记）。

## 2. 测试

- `components/ai/dock/__tests__/AssistantAsyncAnswerCard.test.tsx`：断言由字面量改为翻译键（`assistant.async.*`），并新增 `not_found` 与 `running → null` 用例。
- 回归覆盖面：`components/ai` + `components/ticket` + `email-intake` + `lib/i18n`，共 **41 suites / 288 tests**；**40 suites / 284 tests 通过**。
- 唯一失败套件 = `components/ticket/__tests__/TicketDetailAssignSearch.test.tsx`（4 例），根因：该测试对 `@/lib/store/auth-store` 的 mock 只提供 `useAuthStore()`，缺 `getState`，与 `use-permissions.ts:36` 现实现（`useAuthStore.getState()`）漂移；**与本批改动无关**（该测试及其依赖文件均未触碰）。
- **存量取证**：`git stash push -u -- itsm-frontend/src` 后在 HEAD 单跑该套件 → 同样 4 例失败（`JEST_EXIT_ON_HEAD=1`）；随后 `git stash pop` 恢复全部改动。结论：存量缺陷，建议另立测试修复任务（补 mock `getState` 或统一权限读取路径）。

## 3. 验证命令与结果

- `npx --no-install eslint <本批 15 个文件>` → **exit 0（零输出）**；
- `npx --no-install tsc --noEmit` → **exit 0**；
- `npx --no-install jest --silent --coverage=false --testPathPattern "(components/ai|components/ticket|email-intake|lib/i18n)"` → **1 failed / 40 passed（41 suites）；4 failed / 284 passed（288 tests）；45.078 s**（唯一失败为上述存量套件）；
- 键集校验（node 临时脚本）→ zh 213 / en 213，缺失/多余/占位符不一致/空值均为 0；
- 残留硬编码扫描（12 文件；字符串字面量与 JSX 文本，排除注释）→ 除 §1 列出的 2 处提示词模板边界外，无 UI 文案残留。

## 4. 局限 / 后续

- 提示词模板、注册表卡片与帮助文档正文不在本批（边界见 §1）；如需多语言提示词，另立任务并同步后端/模型侧口径。
- `TicketDetailAssignSearch` 存量失败建议单独立项修复（mock 补 `getState` 即可），不影响本批验收。
- en-US 为新增译文，建议在联调环境做一次英文走查（长度/换行/截断）；zh 默认路径已由既有测试覆盖。
