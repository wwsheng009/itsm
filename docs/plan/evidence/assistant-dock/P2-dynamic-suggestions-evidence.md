# P2 LLM 动态建议（缓存 + 审计） · 交付证据

- 日期：2026-10-18
- 任务：P2「智能化与运营」首项：**LLM 动态建议（缓存 + 审计）**
- 状态：`integration_verified`（后端生成/缓存/单飞/审计 + 端点 + 前端增强层接入；真实 LLM E2E 归联调）
- 交付物：
  - 后端：`handlers/ai/dynamic_suggestions.go`
    - `DynamicSuggestionService`：进程内 TTL 缓存（默认 5m，上限 200，淘汰最早过期）+ **同键单飞**
      （并发请求只触发一次 LLM 调用）+ 失败 fail-safe（`llm_unavailable` / `parse_failed`，**不缓存失败**）；
    - `parseDynamicSuggestions`（纯函数）：容忍 markdown 围栏/前后噪声；最多 3 条；
      `kind` 归一（非法 → task）、`readOnly` **强制 true**（§D4：建议区只读）、label/prompt 截断与必填；
    - `llmDynamicSuggestionGenerator`：经 LLMGateway §3.3 解析链（`ResolveRequest` → `ChatWithRequest`，20s 超时）
      下发「仅输出 JSON 数组」严格提示词（含页面 title/scene/entrypoint 与现有卡片文案，要求不重复、禁写动作）；
    - 审计：每次**真实生成**（成功/失败）写 `ai_feedbacks(item_type=ai_audit, scenario=assistant_dynamic_suggestion)`——
      与 `/ai/audit-logs`、`/ai/evaluation` 同一数据面；信心分为代理口径（成功 40+10×条数、失败 0，已在代码注释与本节登记）
  - 端点：`GET /api/v1/ai/assistant/suggestions/dynamic?pageKey=&route=`（ai:read；未装配 → 503；
    未知 pageKey → 200 `known=false`；生成失败 → 200 `generated=false`+`reason`）+ `authz-gen` 预检再生
  - 前端：`aiGetDynamicSuggestions` + `lib/ai/page-context/use-dynamic-suggestions.ts`
    （会话级缓存 5m、并发去重、失败/未启用 → 空数组零行为差异）+ `SuggestionCards` 增强层接入
    （动态卡置于各组首位、静态/服务端建议兜底、埋点 `feed=llm`）
  - bootstrap 装配：LLM 未装配时生成器为 nil → 端点 503、前端静默回退静态基线

## 1. 口径与边界

| 项 | 口径 |
|---|---|
| 缓存 | 键 = `tenant|pageKey|role|route`；TTL 5m；命中返回 `cached=true`，**不重复调用 LLM、不重复审计** |
| 并发 | 同键单飞（等待者读缓存）；无缓存（失败）→ 允许下次重试 |
| 只读约束 | 解析层强制 `readOnly=true`；提示词显式禁止写操作/自动提交（写动作仍走确认闭环） |
| fail-safe | 生成/解析失败 → `generated=false` + reason；前端保持静态/服务端基线（动态建议只是增强） |
| 审计 | 仅真实生成尝试落审计（缓存命中的"展示"由前端 `suggestion_shown(feed=llm)` 埋点承担） |
| 降级链 | 动态（LLM）→ 服务端过滤建议（D1-01）→ 静态注册表基线（D0-03/05） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l handlers/ai/dynamic_suggestions.go handlers/ai/dynamic_suggestions_test.go
go test ./handlers/ai/ -run 'TestParseDynamicSuggestions|TestDynamicSuggestionAuditScore|TestDynamicSuggestionService|TestGetDynamicSuggestions' -count=1
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1     # 5 包全绿
go run ./cmd/authz-gen   # 再生：/api/v1/ai/assistant/suggestions/dynamic → ai:read

cd ../itsm-frontend
npx jest src/lib/ai/page-context/__tests__/use-dynamic-suggestions.test.tsx "src/components/ai/chat-core/__tests__/SuggestionCards.test.tsx" --runInBand
# → 2 suites / 14 tests
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils --runInBand   # 56 suites / 477 tests
npx tsc --noEmit ; npx eslint src/lib/ai/page-context src/components/ai/chat-core/SuggestionCards.tsx …
```

关键断言：
- 解析：围栏/噪声容忍；最多 3 条；非法 kind→task；`readOnly` 强制 true、`source=llm`、ID `dyn:N`；
  空 label/prompt 跳过且全无效 → 错误；非 JSON → 错误；
- 编排：冷启动生成（audit 1 条 generated=true）；二次命中缓存（LLM 调用仍 1、audit 仍 1、`cached=true`）；
  角色不同 → 不同键；TTL 过期 → 重新生成并再审计；LLM 失败/解析失败 → 不缓存、可重试、reason 正确；
  **单飞**：并发两次 → LLM 调用 = 1，两个结果都成功（一个 `cached=false`、一个 `cached=true`）；
- 审计分代理：失败 0 / 1 条 50 / 3 条 70 / 封顶 100；
- 端点：200 生成 + `cached` 语义 + `model`；未知页 200 `known=false` 不触发 LLM；缺 pageKey 400；缺身份 401；未装配 503；
- 前端：动态建议置于任务组首位（静态卡仍在）、点击发送其提示词、`suggestion_shown/clicked{feed:llm}`；
  未生成/503/未注册页面 → 静默回退基线（零请求/零渲染差异）；会话级缓存不重复请求。

## 3. 条件项（不阻塞）

- 真实 LLM 生成质量与端到端（Provider 返回 JSON 稳定性）归联调：本批用 fake 生成器锁定编排契约，
  提示词为严格 JSON 约束且解析失败 fail-safe；
- 审计信心分为**代理口径**（LLM 未回带置信度），质量以 `suggestion_shown/clicked/ignored(feed=llm)` 闭环衡量；
- 动态建议的租户级开关（`assistant.dynamicSuggestions` 系统配置）沿用"LLM 网关是否装配"隐式门控，
  显式开关（含缓存 TTL/条数策略）归运营迭代；
- 缓存为进程内（多实例部署下各自缓存，可接受）；如需跨实例一致性，后续可迁移到 Redis/表存储。