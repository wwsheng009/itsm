# O2-02 补全 · 工作区诊断入口复用 + 新增组件 axe 覆盖面（证据）

- 日期：2026-10-18
- 状态：`integration_verified`
- 关联：方案 §7 **O2-02**（诊断抽屉）、**I2-02**（合规与无障碍 axe 扫描）、**V1-03 条件项闭环**（确认卡入扫描面）；
  前置：`O2-02-diagnostics-drawer-evidence.md`、`O2-02-token-usage-evidence.md`
- 交付批次：O2-02 剩余两项（工作区入口复用 + 新增组件扫描面）+ V1-03 条件项（确认卡入扫描面）

## 1. 交付范围

### 1.1 工作区 `/ai/chat` 诊断入口（与 Dock 面板同一实现）

| 文件 | 变更 |
| --- | --- |
| `src/components/ai/diagnostics/AssistantDiagnosticsEntry.tsx` | **新增**：共享入口组件 = 「会话诊断」按钮（`aria-label="会话诊断"`、`title` 含轨迹/耗时/报障 ID）+ 按需挂载 `DiagnosticsDrawer`；`testId` 可覆写（默认 `assistant-diagnostics-open`） |
| `src/components/ai/dock/AssistantChatPanel.tsx` | 重构为复用入口组件：删除本地 `diagnosticsOpen` state、内联按钮与内联抽屉渲染；入口按钮 testid/文案/行为保持不变 |
| `src/components/ai/AIChat.tsx` | 工作区工具条接入同一入口（`testId=ai-chat-diagnostics-open`、`pageKey="ai.chat"`），透传工作区会话数据 `messages` / `convId` / `streaming` |

契约要点：

- **单一实现**：面板与工作区不再各写一份按钮 + 抽屉接线，入口逻辑（打开态、懒加载抽屉、关闭零请求）只有一处；
- **唯一差异**：工作区入口 testid 为 `ai-chat-diagnostics-open`。原因：Dock 悬浮球/面板与 `/ai/chat` 工作区可能同页共存，
  同 testid 会让 `getByTestId` 产生歧义；语义（aria-label/title/行为）与面板入口一致；
- 会话数据由各自会话内核（`useAIChatSession`）透传，抽屉内「会话 / 消息数 / 生成状态 / 最近运行 /
  步骤耗时 / token（估算）」在两处同源。

### 1.2 新增组件纳入 axe 扫描面（I2-02）

`src/components/ai/__tests__/a11y-surfaces.test.tsx` 新增三例（扫描对象由 3 → 6）：

| 用例 | 扫描对象 | 夹具 |
| --- | --- | --- |
| AssistantMemoryChips：记忆条 | V2-01 记忆条（常用语/模板 chips + 管理入口） | telemetry 档切 `on`、能力面默认开启、偏好面含 1 常用语 + 1 模板 |
| AssistantAsyncAnswerCard：异步回执卡 | V2-02 回执卡（快速参考 + 已完成精算 + 未读回执） | store 注入 `status=done` + `refined`（非 running → 无轮询） |
| IntentUnderstandingCard：确认卡 | V1-03 口语→术语确认卡（术语映射 + 证据片段 + 确认/修改控件） | 后端理解结果替身（`matched=true`、2 条术语证据、network/high） |
| （既有 3 例不变） | 面板 / 诊断抽屉（含轨迹视图）/ 内联建议条 | — |

其中第三例**闭环 V1-03 已登记条件项「新卡未入 axe 扫描」**（方案 §7 V1-03 块）。

口径不变：仅禁用 `color-contrast`（jsdom 无真实布局与级联样式，无法可靠计算对比度），其余规则全开。

## 2. 复跑命令与结果（本机，2026-10-18）

```powershell
cd itsm-frontend

# ① 新增工作区入口用例（2 例：打开/关闭零请求 + 非空会话按需拉轨迹/token 同源）
npx jest src/components/ai/__tests__/AIChat.diagnostics.test.tsx --coverage=false --watchAll=false --runInBand --silent

# ② 无障碍扫描面 + 面板回归（验证重构未破坏面板既有入口行为）
npx jest src/components/ai/__tests__/a11y-surfaces.test.tsx src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx --coverage=false --watchAll=false --runInBand --silent

# ③ 类型 / 静态检查
npx tsc --noEmit
npx eslint src/components/ai/AIChat.tsx src/components/ai/diagnostics/AssistantDiagnosticsEntry.tsx src/components/ai/dock/AssistantChatPanel.tsx src/components/ai/__tests__/AIChat.diagnostics.test.tsx src/components/ai/__tests__/a11y-surfaces.test.tsx

# ④ AI 表面全量回归
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
```

结果：

- ① 1 suite / 2 tests 通过；
- ② 2 suites / 13 tests 通过（a11y 当时 5 例；面板 8 例，含 O2-02 既有入口用例）；
- ②′ a11y 补第三例（`IntentUnderstandingCard`）后单跑 **6 例全绿**、`tsc`/`eslint` 复跑 exit 0；
- ③ `tsc --noEmit` exit 0；`eslint` exit 0；
- ④ **128 suites / 1668 tests 全绿**（上批 127/1663 → +1 suite/+5 tests：工作区 2 + axe 3）。

## 3. 关键断言与边界条件

工作区入口（`AIChat.diagnostics.test.tsx`）：

1. 工具条渲染入口，`aria-label="会话诊断"`；**关闭态**：抽屉不存在、`aiGetRunTrace` **零调用**（懒加载契约）；
2. 点击打开抽屉，读到工作区会话数据：会话 `#9`（`convId` 透传）、空会话显示 `diagnostics-no-run`、
   仍**不发**轨迹请求（无目标运行）；
3. 非空会话（消息含 `run.runId=7`）：打开即按需 `aiGetRunTrace(7)`，且 token 段
   （`diagnostics-usage`，来自同一消息的 `usage`）与工作区数据同源 → 「轨迹 / 耗时 / token」在
   `/ai/chat` 同样可见。

axe 扫描（新增 2 例）：断言 `axe(container).toHaveNoViolations()`；夹具覆盖 chips 渲染态与回执卡
完成态（antd Tag/Button 等均为可访问名齐全的控件）。

诚实登记（不假装）：

- 扫描面**不含** `AIChat` 整页（页面壳做 DOM 级布局锁、且渲染需完整会话内核桩，收益低）；
  工作区**入口按钮本身**为带 `aria-label` 的 text button，其可达性由 ③ 的类型/静态检查与 ② 的
  面板同构入口用例间接覆盖；若后续需要，可按本次新增范式补整页扫描（需完整会话内核桩）。
- 抽屉的「打开即拉轨迹」在**未打开**与**无最近运行**两种情况下均零请求（本批用例显式断言）。

## 4. 结论

O2-02 的两项遗留（工作区入口复用、新增组件 axe 覆盖面）已交付并接线验证；结合前两批
（诊断抽屉本体、token 段），O2-02 在方案 §7 中不再有未交付项。
同时闭环 **V1-03 条件项「新卡未入 axe 扫描」**（`IntentUnderstandingCard` 入扫描面，0 violations）。
遗留条件项（登记于方案文档）：真实 usage 解析归 B4-02、失败回合/非 SSE 回退路径不带 usage；
更多界面纳入 axe 批量与真实浏览器走查归后续。
