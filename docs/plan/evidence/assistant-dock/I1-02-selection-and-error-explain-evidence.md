# I1-02 划词问 AI + 错误解释入口 · 交付证据

- 日期：2026-10-18
- 任务：I1-02（方案 I 系列：划词问 AI + 错误解释入口｜验收：选区→面板预填；错误旁解释可用）
- 状态：`integration_verified`（组件 + 两处页面级接线 + 用例；真实浏览器选区交互 E2E 归联调）
- 交付物：
  - 划词：`components/ai/SelectionAskAI.tsx`（选区 2–500 字符 → 悬浮「问 AI」→ 预填并打开面板）；
    挂载：`MainLayout`（`AssistantDock` 旁）
  - 错误解释：`components/ai/ExplainErrorButton.tsx`（错误值归一化 + 提示词构造）；
    接线：`ErrorBoundary`（页面级错误 fallback）与 `RouteError`（react-router `errorElement`）
  - 复用通道：`assistant-store.pendingPrompt`（I0-03；**只填不发送**）

## 1. 行为口径（验收映射）

| 要求 | 实现 | 证据 |
|---|---|---|
| 选区 → 面板预填 | 选区文本（trim 后 2–500 字符）→ `buildSelectionPrompt` → `pendingPrompt` + `openPanel()` | 组件 4 例（含短选区/滚动/ESC 收起） |
| 不打扰输入 | 忽略 `input/textarea/select/[contenteditable]/助手面板内部` 的选区；`mousedown` 其他区域/滚动/ESC 立即收起；按钮 `onMouseDown` 阻止隐藏竞态 | 输入框选区用例 |
| 错误旁解释可用 | `ErrorBoundary` fallback（页面级）+ `RouteError`（路由级 404/500/异常）均渲染「解释这个错误」 | 组件 4 例 + ErrorBoundary 回归通过 |
| 错误信息归一 | `Error` → message；`Response` 形状（react-router）→ `HTTP <status> <statusText>`；字符串/对象兜底；≤500 字符截断 | 提取/截断用例 |
| 埋点 | `handoff(from=selection)`、`handoff(from=error_explain)` | 两组件用例断言 |

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai src/lib/ai src/components/common/__tests__/ErrorBoundary.test.tsx src/routes --coverage=false --watchAll=false --runInBand
# → 27 suites / 224 tests 全绿（新增选区 4 例 + 错误解释 4 例）
npx tsc --noEmit ; npx eslint src/components/ai src/components/errors/RouteError.tsx src/layouts/MainLayout.tsx src/components/common/ErrorBoundary.tsx
# eslint：0 error（仅既有 tool-approval-card.test 的 unused-disable 警告，与本批无关）
```

关键断言：
- 选中文本 → 悬浮按钮出现 → 点击后 `pendingPrompt` 含选区文本与"解释"、面板打开、`handoff(from=selection)`、按钮收起；
- 短选区（<2）/输入框内选区 → 不浮现；滚动与 ESC 收起；
- 错误解释：`Error('ECONNREFUSED…')` + 位置 → 提示词含错误与"排查步骤"/位置；无错误不渲染；
  `{status,statusText}` 归一为 `HTTP 404 Not Found`；超长消息截到 500 字符。

## 3. 边界与条件项（不阻塞）

- 「划词」在移动端为长按选择（浏览器原生菜单与悬浮按钮可能重叠）——移动端形态（V1-02）批内评估；
- 选区在 iframe/富文本编辑器内部（RichTextEditor）可能触发编辑区自身的选区逻辑：当前按 `[contenteditable]` 忽略，
  如需"在描述编辑器内划词问 AI"再单独评估；
- 真实浏览器选区坐标/滚动行为与多行选区定位归联调 E2E；
- 更多错误面（表单校验、toast、组件内联错误）可直接复用 `<ExplainErrorButton error={...}/>`，按需接线。