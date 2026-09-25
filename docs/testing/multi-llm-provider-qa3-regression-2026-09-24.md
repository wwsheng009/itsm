# 多 LLM Provider · QA-3 开关关闭全链路回归报告

> 任务：`docs/plan/multi-llm-provider-plan.md` §5.3 QA-3 ——「回归门禁：开关关闭时全链路行为与现状一致（diff 级）」
> 执行日期：2026-09-24（复跑 2026-09-25）；环境：Windows / pwsh，工作树（`itsm-backend` + `itsm-frontend`），无需真实 DB
> 结论：**✅ 通过**——开关关闭时：无新路由（404）、无新 UI、无新增请求、接口响应字段与现状一致、请求体逐字节一致、路由/菜单零 diff。
> 说明：浏览器级人工视觉复核（页签不渲染、选择器不出现）归入 QA-2（§6.2 场景 8），见 §4。

## 1. 门禁矩阵与证据

| # | 维度 | 门禁要求 | 证据（文件 : 行 / 用例） | 结果 |
|:--|:--|:--|:--|:--|
| 1 | 开关语义 | 未配置默认关；env 优先；`1/0` 兼容；非法值按关 | `service/llm_multi_provider_flag.go:29`；`TestMultiProviderEnabledDefaultsOff`、`TestMultiProviderEnabledReadsEnvOverride`（`service/llm_multi_provider_flag_test.go:11/27`） | ✅ PASS |
| 2 | 后端无新路由（装配门禁） | 开关关闭时 bootstrap 不构造管理面 handler ⇒ 整组路由不注册 | `internal/bootstrap/app.go:987-993`（仅 `service.MultiProviderEnabled()` 时注入）；`router/router.go:209-211`（nil handler = 端点不可达，§3.5 回滚语义） | ✅ PASS |
| 3 | 后端无新路由（运行时断言） | 开关关闭（handler=nil）⇒ `/api/v1/ai/providers` 为 404；开关开启 ⇒ 已注册（鉴权链 401，非 404） | **本轮新增** `router/llm_provider_routes_test.go`：`TestSetupRoutes_LLMProviderAdminRoutesAbsentWhenHandlerNil`（实测 `404`）、`TestSetupRoutes_LLMProviderAdminRoutesRegisteredWhenHandlerPresent`（实测 `401`） | ✅ PASS |
| 4 | chat 旧调用路径 | 开关关闭且未显式覆盖 ⇒ 走既有 `svc.Chat`，响应形状不变 | `handlers/ai/handler.go:170-172`（注释即 QA-3 零破坏门禁）；`handlers/ai/service.go:375-381`（无 resolver 时直接返回既有网关）；`go test ./handlers/ai/` 全绿（含 `TestResolveChatProviderDefaultChainFallsBackToStatic`、`TestResolveChatProviderWithoutGateway`） | ✅ PASS |
| 5 | 指标响应字段 | 开关关闭时 `GetMetrics` 不含 `byProvider`（`omitempty` 省略，与引入前逐字节一致） | `TestAIMetricsByProviderOmittedWhenNotAggregated`（`service/ai_telemetry_provider_key_test.go:128-134`，断言 raw JSON 无 `byProvider`）；聚合门禁 `service/ai_telemetry.go:159` | ✅ PASS |
| 6 | 构建分派回退 | 开关关闭 ⇒ 既有实现分支（旧行为） | `TestNewProviderFromConfigSwitchOffKeepsLegacyProviders`（`service/llm_registry_test.go:167`）；`TestLLMProviderRegistry*` 定向集全绿 | ✅ PASS |
| 7 | 启动硬约束 | 开关关闭时 DB 实例不参与解析链、启动探针不启用（保持现状语义） | `internal/bootstrap/app.go:619-625`；`go test ./internal/bootstrap/ -count=1` 全绿 | ✅ PASS |
| 8 | RBAC 预检与路由守卫 | 新端点权限声明与预检生成物对齐；既有守卫不回归 | `go test ./router/ -run 'TestSetupRoutes|TestWriteRoutesRequirePermission|TestRoutePermissionCodesAreDefined'`、`go test ./middleware/ -run 'TestRoutePrecheckAlignment|TestPrecheckMapIsFresh'` 全绿 | ✅ PASS |
| 9 | 前端无新 UI / 零请求 | 非 `system:write`：不渲染页签/选择器、**零新增请求**；开关关闭（404 无 `errorCode`）：fail-closed 且不抛异常 | `use-llm-provider-feature.test.ts`：`非系统管理员：不渲染探测、零请求（QA-3）`、`开关关闭（404 且无 errorCode）：fail-closed，不抛异常`；FE-3 页签用例 `should gate LLM provider tab by system:write (FE-3)`（`use-permissions.test.ts`） | ✅ PASS |
| 10 | 前端请求体字节一致 | 未选择 provider（开关关闭/单实例）时 SSE 与降级 `AIApi.chat` 请求体均不含 `provider`，与现状逐字节一致 | `ai-api.test.ts` → `omits provider from the SSE body when not selected (QA-3 byte-identity)`、`omits provider in one-shot fallback body unless selected, and forwards it otherwise` | ✅ PASS |
| 11 | 前端回调兼容 | `done` 无 `provider`/`providerSource` 字段时 `onDone` 仍按旧单参数签名调用 | `ai-api.test.ts` → `keeps done callback backward compatible when the switch is off (no provider fields)`；实现 `src/lib/api/ai-api.ts`（`if (info) onDone(id, info); else onDone(id);`） | ✅ PASS |
| 12 | 路由与菜单零变更 | 前端路由与 route-map CSV 零 diff | `git diff --stat -- src/routes/index.tsx ../docs/plan/_data/vite-route-map.csv` → 空输出 | ✅ PASS |
| 13 | 前端静态质量 | tsc / eslint 无错 | `npx tsc --noEmit` → `TSC_OK`；`npx eslint`（10 个改动/新增文件）→ `ESLINT_OK` | ✅ PASS |

## 2. 复现命令（本轮实测）

```powershell
# --- 后端（workdir: itsm-backend）---
go test ./service/ -run 'TestMultiProviderEnabled|TestAIMetricsByProviderOmittedWhenNotAggregated' -count=1 -v   # 3 PASS
go test ./service/ -run 'TestNewProviderFromConfig|TestLLMProviderRegistry|TestProtocolProvider|TestMultiProvider|TestLLMProtocolSlot' -count=1   # ok
go test ./handlers/ai/ -count=1                                                                                  # ok
go test ./router/ -run 'TestSetupRoutes|TestWriteRoutesRequirePermission|TestRoutePermissionCodesAreDefined' -count=1   # ok
go test ./router/ -run 'TestSetupRoutes_LLMProviderAdminRoutes' -count=1 -v                                      # 2 PASS（新增用例）
go test ./internal/bootstrap/ -count=1                                                                            # ok
go test ./middleware/ -run 'TestRoutePrecheckAlignment|TestPrecheckMapIsFresh' -count=1                           # ok

# --- 前端（workdir: itsm-frontend）---
npx tsc --noEmit                                                                                                  # exit 0
npx eslint src/components/ai/AIChat.tsx src/lib/api/ai-api.ts src/lib/api/http-client.ts src/lib/api/llm-provider-api.ts src/lib/api/__tests__/ai-api.test.ts src/lib/hooks/use-llm-provider-feature.ts src/lib/hooks/__tests__/use-llm-provider-feature.test.ts src/lib/hooks/__tests__/use-permissions.test.ts "src/pages/(main)/admin/system-config/index.tsx" "src/pages/(main)/admin/system-config/llm-provider-settings.tsx"   # 无输出
npx jest src/lib/api/__tests__/ai-api.test.ts src/lib/hooks/__tests__/use-llm-provider-feature.test.ts src/lib/hooks/__tests__/use-permissions.test.ts --silent --coverage=false   # 3 suites / 94 tests passed

# --- 零 diff 复核（workdir: itsm-frontend）---
git diff --stat -- src/routes/index.tsx ../docs/plan/_data/vite-route-map.csv                                      # 空输出
```

## 3. 本轮新增用例（「无新路由」的运行时断言）

`itsm-backend/router/llm_provider_routes_test.go`：

- `TestSetupRoutes_LLMProviderAdminRoutesAbsentWhenHandlerNil`——`RouterConfig.LLMProviderAdminHandler=nil`（等价开关关闭时的 bootstrap 注入结果）时：路由表中不含 `/ai/providers*`，且 `GET /api/v1/ai/providers` 实际返回 **404**。
- `TestSetupRoutes_LLMProviderAdminRoutesRegisteredWhenHandlerPresent`——注入 handler（等价开关开启）时同一路径返回 **401**（进入鉴权链），证明前一条的 404 由门禁产生而非路径拼写问题。

实测日志（节选）：

```
[GIN] 2026/09/25 - 12:57:10 | 404 | GET "/api/v1/ai/providers"    # nil handler（开关关闭）
[GIN] 2026/09/25 - 12:57:11 | 401 | GET "/api/v1/ai/providers"    # handler 注入（开关开启，未带令牌）
```

## 4. 覆盖边界

- 本报告覆盖**开关关闭路径**（QA-3 范围）；开关开启后的功能验收（创建→测试→设默认→切换→禁用降级等场景 1–11）属 QA-2。
- 「无新 UI」在本轮以组件/hook 单测 + 权限用例 + 路由零 diff 证明；**浏览器级人工视觉复核**（系统配置页无「LLM 模型」页签、会话页无选择器）需运行前后端后执行，随 QA-2 一并归档。
- `LLM_PROTOCOL_ADAPTER_ENABLED`（协议适配器开关，默认关）不在本报告范围，其等价对照与回退已在 `service/llm_registry_test.go` 锁定。

## 5. 复测记录

| 日期 | 范围 | 结果 | 备注 |
|:--|:--|:--|:--|
| 2026-09-24 | 后端门禁用例 + 前端三套件 + 路由零 diff | 通过 | 首次收敛 |
| 2026-09-25 | 全部门禁矩阵复跑 + 新增 router 门禁用例 | 通过（13/13 维度） | 报告定稿 |

---

**维护人**：后端 / 前端 / QA（滚动回归）
**适用版本**：多 LLM Provider 方案 v1.13+（开关默认关闭发布）
**下次回归**：任何改动 `LLM_MULTI_PROVIDER_ENABLED` 语义、`/api/v1/ai/providers*` 注册条件、AI 会话请求体或路由表时
