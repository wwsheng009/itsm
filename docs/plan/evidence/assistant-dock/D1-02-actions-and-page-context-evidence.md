# D1-02 动作桥 + 详情页 L2 上下文 · 交付证据

- 日期：2026-10-18
- 任务：D1-02（方案 §3.3 任务卡 / §4.1 L2 / §6 前端；动作桥 + 工单/事件/变更/CI 详情页 context provider）
- 状态：`integration_verified`（机制 + 四类实体接线 + 后端 target 扩展 + 建议卡忽略结算；浏览器级 E2E 归联调）
- 交付物：
  - 动作桥：`src/lib/ai/assistant-actions.ts`（`actionId → handler`；未注册 → route 跳转 → 提示词回退）、
    `src/lib/ai/page-context/use-register-assistant-action.ts`（页面注册/卸载自动注销）
  - L2 运行时数据：`src/lib/store/assistant-context-store.ts`（非持久化 + owner 语义）、
    `src/lib/ai/page-context/use-register-assistant-page-data.ts`
  - 详情页接线：`TicketDetail`（ticket）、`IncidentDetail`（incident）、`CIDetail`（ci）、`ChangeDetail`（change）
  - 请求接线：`use-ai-chat-session.ts`（launcher 优先 → 页面 L2 派生入口上下文；遥测 entrypoint 同步）
  - 用户可见：`AssistantDock` 上下文条展示 `页面标题 · 工单 #42`（title 属性带摘要）
  - 后端扩展：`service/bot/scope.go` + `handlers/ai/scope.go` 新增 `TargetTypeChange`（变更详情目标预检）
  - 忽略采集（`suggestion_ignored`，补全 2026-10-18）：`chat-core/SuggestionCards.tsx` 会话级展示台账
    （`pageKey::cardId` → kind/rank/feed）+ 点击豁免 + 「卡消失即结算」+「面板卸载延迟一拍结算」
    （StrictMode 双挂载由下一轮 effect 取消规避，不产生虚假忽略）→ D0-05 登记的采集缺口闭环

## 1. 口径（实现即契约）

| 项 | 规则 |
|---|---|
| L2 来源 | **仅页面显式注册**（详情实体加载成功后）；不自动采集、不持久化（刷新即重置，由页面重新注册） |
| owner 语义 | 后挂载页面优先；旧页面卸载回调不得清掉新页面的数据（用例：两级详情互斥挂载） |
| 请求优先级 | launcher（路由 state，B3-02）**优先**；无 launcher 时由页面 L2 + 声明 entrypoint 派生；两者皆无 → 请求体不落字段（与 D0-04 兼容口径一致） |
| 派生规则 | `entrypoint` 取页面声明（`chat` 且无目标 → 不派生）；`targetType/targetId` 来自 L2；`summary` 透传（≤300 字符，与入口协议一致） |
| 事实源 | 服务端 `resolveRequestScope` + `entTargetChecker` 做存在性/租户/读权限预检，失败 → 404/503 fail-closed；前端值只是提示 |
| 动作桥 | 已注册 handler → 执行（异常吸收，不阻塞对话）；未注册 + 声明 `route` → 路由跳转；否则 → 发送提示词（D0-05 现状） |
| 写动作边界 | 桥不执行任何写动作；写操作仍走对话内确认闭环 / Gate3（§D4 不变量不变） |
| 埋点 | `suggestion_clicked.extra.dispatch = handler\|navigated\|prompt`（落地方式可观测）；`suggestion_ignored{pageKey,cardId,cardKind,rank,extra.feed}`（补全，2026-10-18） |
| 忽略结算 | 仅对**已展示且未点击**的卡结算；服务端结果替换基线时只结算**消失**的卡（保留卡不误报）；卸载时延迟一拍统一结算；同一卡单轮只结算一次 |

## 2. 上下文流

```
详情页加载实体成功
  └─ useRegisterAssistantPageData({ targetType, targetId, summary })   ← 显式、可卸载
       ├─ AssistantDock 上下文条：页面标题 · 工单 #42（用户可见；title=摘要）
       └─ useAIChatSession：effectiveScope = launcherScope ?? pageScope
            └─ POST /ai/chat：entrypoint + targetType/targetId/summary
                 └─ 服务端对象预检（change 为 D1-02 新增 target 类型）
```

## 3. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai/chat-core/__tests__/SuggestionCards.test.tsx --coverage=false --runInBand   # 13 例（忽略结算 4 例）
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand   # 48 suites / 311 tests
npx tsc --noEmit                              # 0 错误
npx eslint src/lib/ai src/lib/store/assistant-context-store.ts src/components/ai/chat-core/SuggestionCards.tsx  # 0 问题
node scripts/gen-page-manifest.mjs --check    # 生成物未漂移（D1-01 门禁复跑）

cd ../itsm-backend
gofmt -l -w handlers/ai service/bot
go test ./handlers/ai/ ./service/bot/ -count=1        # 全 ok（含 scope_test 的 change 目标隔离/权限用例）
go test ./service/ -run 'TestNormalizeToolMetadata|TestBuiltinToolMetadata' -count=1   # ok
```

关键断言：
- 动作桥 5 例：handler 优先/不触发回退；route → `navigate(route)`；无 handler 无 route → `sendPrompt(prompt)`；
  handler 抛错被吸收（不重复下发）；注销回退与「后注册者优先、旧注销不误删」。
- L2 store 3 例：挂载注册/卸载清理；数据更新重新注册、非法 id 不注册；两级详情互斥挂载 owner 语义。
- 会话内核新增 2 例：`/tickets/42` + L2 数据 → 请求携带 `entrypoint=ticket_detail, targetType=ticket, targetId=42, summary`；
  `/tickets` 列表页 → 仅 `entrypoint=ticket_list`（不携带 target 字段，请求体向后兼容）。
- 建议卡新增 2 例：动作桥 handler 分支（不发送提示词，`dispatch=handler`）与 prompt 回退分支（`dispatch=prompt`）。
- 忽略结算新增 4 例：① 卸载结算展示未点击卡（断言 `cardKind/rank/extra.feed`）；② 点击过的卡豁免、其余仍结算；
  ③ 服务端结果替换基线只结算消失卡（`triage_plan`），保留卡 `reply_draft` 不误报、卸载后仍结算且
  `triage_plan` 仅结算 1 次；④ StrictMode 双挂载零虚假 `ignored`、卸载后仍结算。
- Dock 新增 1 例：L2 数据在上下文条展示 `工单详情 · 工单 #42` 且 `title=无法登录`（用户可见）。
- 后端：`TargetTypeChange` 纳入 `KnownTargetTypes`；`entTargetChecker` 对 change 做「本租户存在 + `change:read`」
  双重收敛（跨租户/无权角色均拒绝）。

## 4. 边界与条件项（不阻塞）

- ~~忽略采集~~ → **已闭环**（2026-10-18 补全：会话级「展示后未点击」结算上线；衰减与参与打分见 O2-01 补全）；
- **首个页面级 handler 使用点**留给 I1-01 Inline Apply（如分诊/字段建议一键应用）；本轮交付机制 + 用例，
  注册表暂无 action 声明 `route`（回退=提示词，行为与 D0-05 一致）——机制已由单测与组件测试锁定；
- 未接线的详情域（problem/release/project 等）可复用同一 hook，按需登记；CI/工单**列表页筛选**（L2 列表数据）
  待后续批次与动作桥筛选建议联动；
- 前端请求经 `buildAskAIRequestScope` 仅落非空字段；`chat` 入口不落 `entrypoint`（兼容口径不变）；
- 真实浏览器 E2E（打开面板 → 请求体核对 → 对象预检 404 负向）归联调批。