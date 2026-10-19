# D1-01 助手上下文服务端过滤 · 交付证据

- 日期：2026-10-18
- 任务：D1-01（方案 §4.1 L1 / §7 P1；`/ai/assistant/context`：按可见 Bot/授权过滤的建议与快捷动作）
- 状态：`integration_verified`（前端消费 + 后端契约 + 生成物门禁；浏览器级 E2E 归联调）
- 交付物：
  - 后端：`handlers/ai/assistant_context.go`、`router/ai_routes.go`（`GET /api/v1/ai/assistant/context`，权限 `ai:read`）、
    `internal/assistantcontext/`（新包：内嵌注册表 + 页面声明查询）、
    `service/bot/scene.go`（场景可用性判定）、`service/bot/policy.go`（`SnapshotForSlug`）
  - 生成器：`itsm-frontend/scripts/gen-page-manifest.mjs` 新增 `assistant-registry.json` 产物 + `--check` 漂移门禁
  - 前端：`lib/api/ai-api.ts`（`aiGetAssistantContext` + 契约类型）、
    `lib/ai/page-context/use-assistant-suggestions.ts`（服务端优先 / 静态基线回退 / 5 分钟缓存）、
    `chat-core/SuggestionCards.tsx`（消费过滤结果 + `extra.feed` 埋点）

## 1. 单一真源与数据流

```
前端 page-context/registry.ts（唯一真源：pageKey/pattern/scene/entrypoint/suggestions[+tools]）
  └─ gen-page-manifest.mjs ──► itsm-backend/internal/assistantcontext/assistant-registry.json（go:embed）
        └─ GET /ai/assistant/context?pageKey=&route= ──► 面板/工作区建议卡（失败回退静态基线）
```

`npm run docs:manifest -- --check`（CI 同命令）新增漂移门禁：内嵌注册表与前端注册表 `pages` 不一致或缺失 → FAIL
（实测：临时改动注册表后 FAIL，重跑生成后 PASS）。

## 2. 契约

`GET /api/v1/ai/assistant/context?pageKey=<key>&route=<path>`（`ai:read`；`pageKey` 必填，空 → 400）

```json
{
  "pageKey": "ticket.detail", "route": "/tickets/42", "known": true, "title": "工单详情",
  "entrypoint": "ticket_detail",
  "scene": { "slug": "s1-ticket-assistant", "botId": 3, "status": "pilot", "eligible": true, "reason": "" },
  "filtering": "enabled",
  "suggestions": [{ "id": "reply_draft", "kind": "task", "label": "生成答复草稿", "prompt": "…", "readOnly": true, "rank": 1 }],
  "filtered": [{ "id": "triage_plan", "reason": "rbac_denied" }]
}
```

## 3. 过滤口径（实现即契约）

1. **未知 pageKey** → `known=false` + `suggestions=[]`（不 404、不 400；前端回退静态基线，与 D0-04 提示层同口径）。
2. **场景 Bot 可见性**（`bot.SceneEligibility`，与运行时 `ListVisibleForChat`/`Decide` 同源，fail-closed）：
   - 模板缺失 → `template_missing`；draft → `status_draft`；`audience` 角色不可见 → `audience_denied`；
   - 入口不在模板 `entrypoints`（空=全拒）→ `entrypoint_denied`；
   - 命中任一 → 该页**全部**建议进入 `filtered`（`scene.eligible=false`）。
3. **工具门禁**（仅对声明了 `tools` 的建议）：逐工具复用 `bot.Decide`（授权 ∩ RBAC ∩ 风险上限 ∩ 入口）；
   任一工具不通过 → 过滤，`reason` 取判定原因码（`rbac_denied`/`tool_not_granted`/`risk_exceeded`…）；
   注册表里不存在的工具 → `unknown_tool`；工具注册表未装配 → `tools_unavailable`。
4. **兼容态**：Bot 策略层未装配（`bot.enabled=false`）→ `filtering=disabled`、`scene.reason=policy_disabled`、
   不做过滤（与静态基线行为逐字节一致）；无场景声明的页面 → `filtering=none`（建议不受工具门禁约束；
   执行面仍走既有 Gate2/Gate3，本接口不改变任何授权判定）。

注册表 `tools` 声明（14 条动作）：`draft_ticket_fields`、`list_tickets`、`list_kb`、`get_incident_stats`、
`analyze_ci_impact_plan`、`get_ci_tickets`、`list_cis`、`draft_kb_article`——仅声明可从提示词直接推断依赖的动作；
未声明 ≠ 无依赖，而是暂不纳入工具门禁（在注册表头注释与类型注释中固定）。

## 4. 前端消费（两段式，不破坏零延迟）

- 静态基线（D0-03/D0-05）**先行渲染**；`/ai/assistant/context` 返回后替换为服务端过滤结果；
- 请求失败 / `known=false` → 保持静态基线，并上报 `context_degraded`（`reasonCode=context_fetch_failed|context_unknown_page`）；
  成功 → `context_loaded`（含 `latencyMs`、`extra.suggestions`、`extra.filtering`）；
- 服务端返回**空建议**视为权威（全部被过滤时不下发，不回退静态）；
- 会话级缓存 5 分钟（pageKey 维度，切换页签/开关面板不重复请求；`clearAssistantSuggestionCache()` 供登出/测试）；
- `suggestion_shown` / `suggestion_clicked` 追加 `extra.feed = server|static`（观测面可区分数据源采纳）。

## 5. 验证证据（可复跑）

```powershell
cd itsm-frontend
node scripts/gen-page-manifest.mjs            # pages=21 coverage=100%；写入 …/assistantcontext/assistant-registry.json
node scripts/gen-page-manifest.mjs --check    # [docs-ledger] assistant registry in sync (D1-01) + ratchet OK
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand   # 17 suites / 124 tests
npx tsc --noEmit                              # 0 错误
npx eslint src/components/ai/chat-core/SuggestionCards.tsx src/lib/ai/page-context/use-assistant-suggestions.ts src/lib/api/ai-api.ts …

cd ../itsm-backend
gofmt -l -w internal/assistantcontext service/bot handlers/ai router
go test ./internal/assistantcontext/ ./service/bot/ ./handlers/ai/ -count=1   # 全 ok
go run ./cmd/authz-gen                        # 路由新增 → 预检重生成（middleware/rbac_precheck_gen.go）
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1   # ok
```

关键断言（后端 `assistant_context_test.go`，7 例）：
- 缺 `pageKey` → 400；未知 pageKey → 200 + `known=false`（无 `scene` 块）；
- 场景 Bot 缺失/draft/audience 拒绝 → `suggestions=[]` 且 `filtered` 覆盖全部建议（原因码正确）；
- 场景可见 + super_admin → `ticket.detail` 三条建议全下发（含 `triage_plan`/`reply_draft`/`kb_lookup`）；
- `limited_role` → 声明 `tools` 的两条被 `rbac_denied` 过滤，未声明的 `reply_draft` 保留；
- 策略未装配 → `filtering=disabled` + 三条建议全保留（兼容态）。

关键断言（前端 `SuggestionCards.test.tsx`，5 例）：服务端优先替换 + `extra.feed=server`；
失败回退静态 + `context_degraded`；`known=false` 回退静态；空建议权威；同页二次渲染不重复请求（缓存）。

## 6. 边界与条件项（不阻塞）

- `route` 仅回显/审计，服务端不做路由模式匹配（路由解析仍在前端注册表；注册表一致性由生成器门禁保证）；
- 场景无关页面的 `tools` 声明本轮不生效（无场景 Bot 快照可判）；D1-02 动作桥/服务端筛选增强时复用；
- ~~`suggestion_ignored` 采集与建议排序权重（O2-01）待后续~~ → **已闭环**（O2-01 排序权重 2026-10-18 已交付；ignored 采集 + 半衰期衰减见 D1-02 / O2-01 补全）；
- 前端缓存 TTL 5 分钟：管理端回收授权后面板最多滞后一个 TTL（登出/路由刷新即清空路径已留 `clearAssistantSuggestionCache`）；
- 面向真实登录态的浏览器 E2E（含 `scene` 字段与 `filtered` 的 UI 呈现）归联调批。