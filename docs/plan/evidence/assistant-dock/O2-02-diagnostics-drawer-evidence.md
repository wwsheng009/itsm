# O2-02 面板内诊断抽屉（轨迹/耗时可见 + 报障导出轨迹 ID） · 交付证据

- 日期：2026-10-18
- 任务：O2-02（方案 §13.4 用户侧"诊断抽屉"｜验收：报障可导出轨迹 ID）
- 状态：`integration_verified`（面板入口 + 抽屉 + 轨迹懒加载 + 复制导出；工作区入口与 token 计量归后续）
- 交付物：
  - 纯逻辑：`components/ai/diagnostics/diagnostics-model.ts`（最近运行提取、步骤排序、报障引用、耗时可读化）
  - 抽屉：`components/ai/diagnostics/DiagnosticsDrawer.tsx`
  - 入口：`AssistantChatPanel` 页签行右侧「会话诊断」按钮（`data-testid=assistant-diagnostics-open`）

## 1. 内容与口径

| 区块 | 数据来源 | 展示 |
|---|---|---|
| 会话 | `convId` / `messages` / `streaming` | 会话 #、消息数（助手计数）、生成状态 |
| 最近轨迹 | `ChatMessage.run`（v2 `run_started/step/done/error` 增量，本地零请求） | run#、状态、步骤数、最近耗时、生效实例、预算（已知上限才显示） |
| 本地步骤 | `ChatMessage.steps`（stepIndex 升序） | #序号 · 类型 · 耗时 · payloadRef |
| 深度轨迹 | O1-01 `GET /ai/runs/:id/trace`（按需，打开且存在 runId 才请求） | 复用 `RunTraceView`（会话→run→步骤/事件→工具/确认→产物→业务对象） |
| 报障导出 | —— | 「复制轨迹 ID」（disabled 当无 run）/「复制会话 ID」兜底；埋点 `handoff(source=diagnostics_copy)` |
| token | —— | **诚实降级**：明示「token 计量未接线（B4-02），可查轨迹/步骤/耗时」 |

边界：复制走剪贴板 API，失败静默（隐私模式/非安全上下文）；纯问答无 run → 显示「无运行记录」并引导复制会话 ID；
工作区（`/ai/chat`）入口复用同一抽屉归后续条件项。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai/diagnostics "src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx" --coverage=false --watchAll=false --runInBand
# → 3 suites / 15 tests（新增：model 4 + drawer 4 + 面板入口 1）
npx jest src/components/ai src/lib/ai src/lib/store src/lib/api/__tests__/ai-api.test.ts --coverage=false --watchAll=false --runInBand
# → 37 suites / 302 tests 全绿
npx tsc --noEmit ; npx eslint src/components/ai/diagnostics src/components/ai/dock/AssistantChatPanel.tsx
```

关键断言：
- 摘要：取**最后一条带 run 的助手消息**（run#7），步骤按 stepIndex 排序，payloadRef 保留；
  空会话/无 run 不报错；报障引用优先 `run#7`，否则 `conversation#9`，皆无 → 空；
- 抽屉：会话/消息数/run#7/状态/步骤数/实例/预算渲染；本地步骤可见；
  存在 runId 时自动调用 `aiGetRunTrace(7)` 并渲染 `run-trace-view`；
- 导出：点击「复制轨迹 ID」→ 剪贴板写入 `7` + `handoff {source: diagnostics_copy, runId: '7'}`；
  无 run 时按钮 disabled、**不发轨迹请求**、复制会话 ID 写入 `9`；
- 失败降级：轨迹 404 → 「轨迹加载失败（可复制轨迹 ID 报障）」；
- 面板入口：点击打开抽屉，空会话显示「本次会话没有运行记录」。

## 3. 条件项（不阻塞）

- token 计量（B4-02 未接线）：抽屉显式说明；接线后在同位置补 token 段；
- 工作区 `/ai/chat` 的诊断入口（同一抽屉组件复用）归后续；会话列表级轨迹检索不做（隐私边界）；
- 轨迹响应在超大运行（数百步骤/事件）下的裁剪策略沿用 O1-01 后续上限参数；
- 浏览器 E2E（真实 run 下打开抽屉 → 复制 → 通知/工单报障引用）归联调。