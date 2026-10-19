# T5-02 输入坞 `/` 快捷指令 + `@` 当前页对象引用 · 交付证据

- 任务：T5-02（方案 §14.2 v5 补任务｜验收：解析 + 交互测试）
- 来源缺口：评审 §3 缺口表第 1 项（输入坞 `/` 快捷指令与 `@` 引用当前页对象，❌ 零命中）；UX B4 / 建议 10 / §3 映射表（斜杠命令 `/工单` `/SLA` `/话术`，设计内未交付）
- 交付日期：2026-10-19（代码 + 测试 + 文档同批提交）

## 1. 实现（双表面共享模块）

| 模块 | 文件 | 职责 |
|---|---|---|
| 解析层（纯函数） | `itsm-frontend/src/components/ai/chat-core/input-dock-commands.ts` | 触发解析（斜杠 / `@`）、命令注册表与过滤、替换（只填不发） |
| 菜单 UI | `itsm-frontend/src/components/ai/chat-core/InputDockAssistMenu.tsx` | listbox 形态菜单（标题 / 候选项 / 无匹配提示行），绝对定位于输入容器上方 |
| 交互 hook | `itsm-frontend/src/components/ai/chat-core/use-input-dock-assist.ts` | 组合 onChange / onKeyDown；键盘导航与选择；焦点回填 |

接线（两处输入坞）：

| 表面 | 文件 | 变更 |
|---|---|---|
| 工作区 | `itsm-frontend/src/components/ai/AIChat.tsx` | 输入容器 `position: relative`；TextArea 接 hook；渲染菜单；新增 `data-testid="assistant-chat-input"` |
| Dock 面板 | `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx` | 同上（复用既有 `inputRef`） |

口径：

- **触发**：斜杠 = 整串 trimmed 值匹配 `^\/\S*$`（`/` 后字符为过滤词，含空白/`/` 不触发）；`@` = 值末尾匹配 `(?:^|\s)@([^\s@]*)$`（前导须为行首或空白，邮箱式内嵌不误触发）。
- **命令集 v1**：`/工单`（总结我最近处理过的工单）、`/SLA`（当前 SLA 达成情况如何？）、`/话术`（帮我起草一段客户沟通话术）——来源为 UX 评审 B4 示例；注册表 `SLASH_COMMANDS` 可扩展，后续与 I0-03 命令面板统一时替换注册表即可（调用方不感知）。
- **`@` 引用**：候选 = L2 当前页对象（`useAssistantContextStore.pageData`，详情页显式注册）；选中插入 `@类型#编号 `（尾随空格脱离触发态，便于续写）；无对象 / 无匹配时为**提示行**（非静默、非空白）。引用插入是**消息文本锚点**：请求作用域仍由既有 L2 派生逻辑携带（D1-02），本批未新增请求协议字段。
- **键盘**：菜单开启时 ↑/↓ 循环高亮、Enter/Tab 选择、Esc 关闭；**Enter 只做选择，绝不发送**；Esc 关闭后按键交还会话内核（Enter 恢复发送路径）。菜单关闭时完全委托内核 `handleKeyDown`。
- **只填不发**：所有选择仅替换输入框文本（等价记忆 chip 的预填模式），用户可继续编辑或自行发送。
- **IME**：组合输入（`isComposing`）期间不拦截按键，不与中文输入法抢键。

## 2. 测试（解析 + 交互）

1. 解析层单测 `chat-core/__tests__/input-dock-commands.test.ts`（20 例）：触发边界（整串 / 前缀 / 空白 / 双斜杠 / 邮箱式 @）、过滤（大小写 / 中文前缀 / 无匹配）、替换（触发态替换 / 非触发态防御 / 引用尾随空格）、引用命中（编号 / 摘要 / 类型）。
2. 交互测试 `__tests__/input-dock-assist.test.tsx`（4 例，双表面；会话桩的 query/setQuery 走真实 React state）：
   - 工作区：`/` 打开菜单（3 项）→ `/SL` 过滤（1 项）→ Enter 选择只填不发（会话 Enter 路径 0 调用）；
   - 工作区：↑ 高亮移动 → 无匹配提示 → Esc 关闭 → Enter 交还会话路径（哨兵 1 调用）；
   - 工作区：`@` 无对象提示 → 注册 L2 对象后过滤并插入 `@工单#42 `；
   - Dock 面板：`/` 点击插入脚手架（只填不发）；`@` 无对象提示。

## 3. 验证命令与结果

- `npx --no-install jest --silent --coverage=false input-dock-commands input-dock-assist` → **2 suites / 24 tests 全过**（42.7s）；
- 回归：`npx --no-install jest --silent --coverage=false AssistantChatPanel AIChat.diagnostics a11y-surfaces streaming-a11y-behavior use-ai-chat-session ChatMessageItem SuggestionCards AssistantMemoryChips` → **8 suites / 61 tests 全过**（34.5s）；
- `npx --no-install eslint <本批 7 个文件>` → 全绿（28.0s）；
- `npx --no-install tsc --noEmit` → 全绿（13.7s）。

## 4. 局限 / 后续

- jsdom 无法覆盖真实键盘 / IME 与菜单视觉呈现；交互语义已按行为断言固定，真机走查可并入联调（不阻塞本任务验收口径 = 解析 + 交互测试）；
- 命令集 v1 为 3 条（UX B4 示例）；增改命令只需扩注册表（不改接线）；
- 本批新增文案（菜单标题 / 命令说明 / 提示行）未走 i18n——属 **T5-13**（文案 i18n 批次）范围；
- 附件 / 截图输入（评审 §7 建议 2 后半）仍挂 T5-03；"与 I0-03 命令面板统一"维持后续增强。
