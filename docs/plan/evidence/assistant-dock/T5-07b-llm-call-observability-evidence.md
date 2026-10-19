# T5-07b LLM 调用报表与错误观测证据

- **状态**：调用观测已交付；预算、金额与模型分级路由按用户口径延后（由上游管理）
- **日期**：2026-10-19
- **范围**：租户级 LLM 调用账本、调用历史列表、汇总报表、错误分类与看板接线
- **非目标**：预算/金额/计费、自动降级、模型分级路由、配额拦截
- **关联**：`docs/plan/assistant-model-routing-and-budget-plan-2026-10-19.md`；主方案 §12 Q5 / §13.3 / §14.2

---

## 1. 交付结论

本轮把 T5-07b 收窄为“调用观测”纵向切片：

1. 每次 LLM 调用记录租户、Provider、模型、任务类型、耗时、成功/失败与错误分类；
2. 租户可查询自己的调用历史，支持时间窗口、Provider、模型、任务、错误分类、成功/失败与分页过滤；
3. 汇总接口输出调用总数、成功率、失败数、平均时延、Token 估算、Provider/模型/任务/错误分类分布；
4. `/ai/observability` 增加“LLM 调用历史与错误”卡片；
5. 不做预算、金额、配额或自动路由；Token 明确标注为 prompt 估算值。

---

## 2. 数据与 API

### 2.1 迁移

`itsm-backend/migrations/20261019_ai_llm_call_observability.sql` / `_down.sql`：

- `ai_llm_calls` 新增 `tenant_id`、`provider_source`、`task_type`、`tokens_estimated`、`error_class`、`http_status`、`error_message`；
- 新增租户 + 时间、成功状态、错误分类、Provider 索引；
- 历史行 `tenant_id = NULL` 不回填，不进入租户报表。

### 2.2 写入

`service/LLMCallObserver` 为可选扩展，网关优先调用；未实现该接口的旧 Observer 继续走原签名：

- 错误分类：`auth / bad_request / not_found / server_error / rate_limited / timeout / network / cancelled / provider_unavailable / unknown`；
- 错误摘要经 `SanitizeLLMCallError` 脱敏并按 rune 截断 512 字符；
- 不落 prompt、请求体或完整上游响应；
- 任务类型通过 `service.WithLLMTaskType` 注入：`chat`、`dynamic_suggestion`、`async_answer`、`batch`；未标注为 `unknown`。

### 2.3 查询

| 接口 | 说明 |
|---|---|
| `GET /api/v1/ai/llm-calls` | 租户级调用历史；`days/providerKey/model/taskType/errorClass/success/page/pageSize` |
| `GET /api/v1/ai/llm-calls/summary` | 租户级调用报表；同过滤条件，不含分页 |

权限复用 `ai:read`。所有查询强制 `tenant_id = 当前租户`。

---

## 3. 前端

- 新增 `components/ai/observability/LLMCallObservabilityCard.tsx`；
- 汇总卡：调用总数、成功率、失败数、平均时延；
- 错误分类 Tag、调用明细表（时间/Provider/模型/任务/状态/耗时/Token 估算/错误）；
- 过滤：状态、Provider key、任务类型、错误分类；分页；
- 空窗口、加载失败与重试可见；
- 接入 `/ai/observability`，使用现有 `days` 过滤。

---

## 4. 验证

### 4.1 后端

```text
gofmt -l <改动 Go 文件>  # 无输出
go test -count=1 -p 1 ./service ./handlers/ai ./migration ./router ./internal/bootstrap
```

结果：

- `itsm-backend/service` ✅
- `itsm-backend/handlers/ai` ✅
- `itsm-backend/migration` ✅
- `itsm-backend/router` ✅
- `itsm-backend/internal/bootstrap` ✅

关键断言：

- 租户隔离：租户 1 只看到自己的 2 条调用，不返回租户 2；
- 错误过滤：`success=false` 只返回失败行，错误分类 `rate_limited`、HTTP 429 正确；
- 汇总：总数 2 / 成功 1 / 失败 1 / 成功率 0.5 / 平均时延 350ms / Token 300；
- 网关明细观测：租户、任务 `chat`、静态 Provider 来源、协议枚举、错误分类 `server_error` 与 HTTP 503 正确；
- 脱敏：`sk-*`、`Bearer`、`api_key=...` 不进入错误摘要，超长错误截断。

### 4.2 前端

```text
npm run type-check                         # exit 0
npx eslint <改动文件>                      # exit 0
npm test -- --runTestsByPath <3 suites> --coverage=false --runInBand --silent
```

结果：3 suites / 22 tests passed（含 API contract 3 例）。

覆盖：

- 汇总与错误分类渲染；
- 调用明细 Provider/模型/状态/Token 估算/错误；
- 空窗口空态；
- 观测页既有回归全部通过。

---

## 5. 局限与后续

1. Token 仍是网关 prompt 估算（字符/4），不是 provider 账单用量；前端已标注“估算”。
2. 历史平台级行（`tenant_id IS NULL`）不进入租户报表，不做回填。
3. 未做预算/配额/金额/自动降级/模型分级路由；这些由用户口径明确延后。
4. 调用历史未携带 `request_id`/`conversation_id`/`run_id`；后续可与会话/运行轨迹关联增强。
5. 错误摘要只保留脱敏短文本，不保留原始 Provider 响应体。
