# D0-04 pageContext 协议（提示层）· 交付证据

- 日期：2026-10-18
- 任务：D0-04（方案 §7 P0 / §4.2 协议扩展）
- 状态：`unit_verified`（SSE 载荷正向断言与端到端联调归后续）
- 变更文件：
  - 前端 `src/lib/api/ai-api.ts`：`AIPageContextHint` 类型 + `AIChatStreamRequest.pageContext` +
    `aiChatStream` / `AIApi.chat` 请求体**条件落字段**
  - 前端 `src/components/ai/chat-core/use-ai-chat-session.ts`：页面上下文接线（注册表解析 → 请求）
  - 前端测试：`lib/api/__tests__/ai-api.test.ts`（+2 例）、`chat-core/__tests__/use-ai-chat-session.test.tsx`（+2 例）
  - 后端 `handlers/ai/page_context.go`（归一化/忽略语义/ctx 注入）+ `page_context_test.go`（4 例）
  - 后端 `handlers/ai/handler.go`：Chat 与 ChatStream 请求体新增 `pageContext`（仅提示/审计、日志留痕）
  - 后端 `handlers/ai/service.go`：`run_started` 事件在携带时附加 `pageKey`
  - 后端 `handlers/ai/page_context_handler_test.go`（端点级 hint-only 契约）

## 1. 协议语义

| 维度 | 口径 |
|---|---|
| 前端字段 | `pageContext?: { pageKey: string; route?: string }`；**缺省不落字段**，请求体与改动前逐字节一致（QA-3 门禁） |
| 取值来源 | D0-03 页面注册表解析（未注册路由 → `unknown` → 不携带） |
| 后端校验 | `pageKey`：`^[a-z0-9][a-z0-9._-]{0,63}$`（小写化、trim）；`route`：必须以 `/` 开头、截断 255 |
| 未知/非法 | **静默忽略（不 400）**，容忍前端版本先行 |
| 授权 | **不参与**：不进入 `ScopeResolver` 的 fail-closed 链，不影响工具面/角色/目标校验 |
| 留痕 | handler 结构化日志（pageKey/route/tenantID）+ `run_started` 事件在存在时附加 `pageKey`（缺省不落字段，事件载荷与现状一致） |

## 2. 验证证据（可复跑）

```powershell
# 前端
cd itsm-frontend
npx jest src/lib/api/__tests__/ai-api.test.ts --coverage=false --runInBand    # 53 tests（+2）
npx jest src/components/ai/chat-core --coverage=false --runInBand             # 6 tests（+2）
npx tsc --noEmit                                                              # 0 错误
npx eslint src/lib/api/ai-api.ts src/components/ai/chat-core

# 后端
cd itsm-backend
go test ./handlers/ai/ -run 'PageContext|PageKey|Normalize|ChatPageContext' -count=1
go test ./handlers/ai/ -count=1                                               # 全包回归通过
```

关键断言：
- **字节一致**：既有 QA-3 用例（原始 body 字符串相等）保持通过；新增"未携带 pageContext 时字段不存在"
  与"携带时追加在末尾且内容一致"两例；
- **hint-only**：端点级用例对 合法/非法/超长/缺失 四种 hint × Chat/ChatStream 双通道断言
  既有 403 语义不变；super_admin + 幽灵实例仍 404 `AI_PROVIDER_NOT_FOUND`（提示层不改变状态码）；
- **单测**：pageKey 64/65 边界、大小写归一、非法字符、route 非相对路径与截断、nil/空键 ctx 安全。

## 3. 边界与遗留（条件项）

- **SSE 载荷正向断言**：`run_started` 携带 pageKey 需要 RAG/运行档案工装（真实 run），本批未覆盖，
  归后续联调；
- **持久化**：`bot_runs` 暂无 `page_key` 列（追加列需 ent 迁移）；当前留痕为日志 + 事件，
  如看板需要"按页面发起量"可复用 `O0` 埋点（`pageKey` 已在事件字典内）；
- **提示注入 LLM**：把 pageKey/route 注入系统提示归 D1（`/ai/assistant/context` 批次）；
- 页面→entrypoint 映射仍由 launcher scope（B3 协议）独立上报；注册表的 `entrypoint` 字段
  将在 D1 服务端建议过滤时启用（当前仅作为声明与测试口径）。