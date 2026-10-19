# D0-03 页面注册表与页面感知 · 交付证据

- 日期：2026-10-18
- 任务：D0-03（方案 §7 P0 / §4.1 L1 / §3.3 任务卡）
- 状态：`unit_verified`（建议卡渲染与协议回传归 D0-05/D0-04）
- 变更文件：
  - 新增 `src/lib/ai/page-context/types.ts`（L1 声明类型）
  - 新增 `src/lib/ai/page-context/registry.ts`（**21 个页面**声明，覆盖 Top 20 业务页）
  - 新增 `src/lib/ai/page-context/use-page-assistant-context.ts`（解析纯函数 + 路由联动 Hook）
  - 新增 `__tests__/registry.test.ts`（6 例契约）、`__tests__/use-page-assistant-context.test.tsx`（4 例）
  - 接入 `src/components/ai/dock/AssistantDock.tsx`（`data-page-key` + 记录 `store.lastRoute`）
  - 更新 `src/components/ai/dock/__tests__/AssistantDock.test.tsx`（MemoryRouter + 路由解析断言）

## 1. 交付内容

1. **注册表（21 页）**：工单（列表/创建/AI 创建/详情）、事件（列表/创建/详情）、变更（列表/新建/详情）、
   问题、CMDB（列表/创建/详情）、知识（列表/新建）、SLA 监控、服务目录、仪表盘、AI 工作区、AI 观测看板。
   每页 1–3 个动作：`task`（零输入任务卡，**全部 readOnly**——D4 不变量）与 `question`（了解型）。
2. **场景/入口映射**：`scene` 对齐 `service/bot/scenarios.go`（s1/s2/s3/s4）；`entrypoint` 对齐后端
   已知入口枚举（`chat/ticket_detail/ticket_list/incident_detail/incident_create/ci_detail`）。
3. **解析器与 Hook**：`resolvePageAssistantContext(pathname)` 纯函数（顺序即优先级）+ `usePageAssistantContext()`
   （路由联动；把当前路由写入 `assistant-store.lastRoute`，供上下文条/诊断使用）。
4. **容器接入**：AssistantDock 根节点携带 `data-page-key`，路由变化即时反映（`/dashboard` → `dashboard`，
   `/tickets/42` → `ticket.detail`）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/lib/ai/page-context --coverage=false --runInBand   # 2 suites / 10 tests passed
npx jest src/components/ai/dock --coverage=false --runInBand    # 2 suites / 6 tests passed
npx jest src/components/ai src/lib/ai --coverage=false --runInBand  # 15 suites / 110 tests passed
npx tsc --noEmit                                                # 0 错误
npx eslint src/lib/ai/page-context src/components/ai/dock src/components/ai/AIChat.tsx
```

契约门禁（registry.test.ts）：
- 规模 ≥20（Top 20 可用）；
- **每个注册项命中 `ROUTE_PATHS`**（静态直查；动态模式校验静态前缀），防注册表与路由漂移；
- pageKey 全局唯一、动作 1–3 个且 id 页内唯一；
- entrypoint 均在 `ASK_AI_ENTRYPOINTS` 内；
- **D4 不变量：零输入任务卡必须只读**（写动作只能走对话确认闭环，不能做成"点了就写"卡片）；
- 文案/提示词非空。

匹配优先级（hook 测试）：`/tickets/create` → `ticket.create`、`/tickets/ai-create` → `ticket.aiCreate`、
`/tickets/42` → `ticket.detail`、`/tickets` → `ticket.list`；`/incidents/create`、`/changes/new`、
`/knowledge/articles/new`、`/cmdb/cis/create` 同理；未注册路由 → `unknown`（不误匹配）。

## 3. 测试环境修复（踩坑记录）

`jest.setup.js` 为不挂 Router 的组件全局 mock 了 `useLocation`（固定 `/`），导致依赖真实路由的用例
全部解析为 `unknown`。按 setup 注释约定，在需要真实路由的两个测试文件中
`jest.mock('react-router', () => jest.requireActual('react-router'))` 还原真实实现；
临时探针测试已删除。

## 4. 边界与遗留

- **建议卡渲染**（把声明变成面板里的可点卡片）归 D0-05；
- **协议回传**（`pageContext` 随对话请求上报后端）归 D0-04；
- 文案为静态中文（与 `chat-core` SUGGESTIONS 同口径）；i18n 化归 D0-05 文案批次；
- `scene`/`entrypoint` 目前是前端提示值：服务端仍以 `ScopeResolver` 预检为准（D0-04 落地后复用）。