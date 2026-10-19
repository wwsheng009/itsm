# T5-07a 成本口径修正证据（首 token / 整体 P50+P95）

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-07a**（由 T5-07 拆分；模型分级路由/成本上限归 T5-07b）
- **范围**：summary 时延口径 + 观测看板 + 健康 SLO；**不含**模型分级路由与预算
- **关联文档**：方案 §13.3 / §14.2；评审 §5.2；交付台账 T5 段
- **提交**：随本文件同一提交（`feat(ai): T5-07a 成本口径修正`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| 首 token P95 与整体 P95 区分 | ✅ summary 返回 `latencyMs.firstToken` / `latencyMs.overall`；顶层字段固定为整体口径 | `ai_assistant_telemetry.go`；service/handler 测试 |
| 补 P50 | ✅ 两套口径均返回 P50/P95/Max/Count | 同上；看板测试 |
| 健康 SLO 不再用混合值 | ✅ `ai_first_token_p95` 只取 `firstTokenP95Ms` | `ai_health.go`；`ai_health_test.go` |
| 看板可读 | ✅ 成本卡分「首 token」「整体」两段展示 P50/P95/样本/最大 | `observability/index.tsx`；页面测试 |
| 向后兼容 | ✅ 顶层 `avg/p50/p95/max/count` 保留，语义固定为 `stream_end` 整体口径 | 本文 §3 |

---

## 2. 实现清单

### 2.1 后端

| 文件 | 改动 |
|---|---|
| `service/ai_assistant_telemetry.go` | 新增 `AssistantTelemetryLatencyBreakdown`；`AssistantTelemetryLatency` 增加 `p50/count/firstToken/overall`；SQL 用 `FILTER` 分别聚合 `stream_end` 与 `first_token` |
| `service/ai_health.go` | `AIHealthQuality` 增加首 token / 整体 P50/P95/Count；SLO 改用 `FirstTokenP95Ms`，样本数取 `FirstTokenCount` |
| `service/ai_assistant_telemetry_test.go` | SQL mock 10 列；断言双口径 P50/P95/Count |
| `service/ai_health_test.go` | 首 token 独立于整体（整体慢但首 token 达标 → ok；反之 breach） |
| `handlers/ai/telemetry_handler_test.go` | HTTP 契约断言 `latencyMs.firstToken/overall` |

### 2.2 前端

| 文件 | 改动 |
|---|---|
| `lib/api/ai-api.ts` | 新增 `AssistantTelemetryLatencyBreakdown`；`AssistantTelemetryLatency` 增加 `p50/count/firstToken/overall`；`AIHealthQuality` 增加首 token/整体字段 |
| `pages/(main)/ai/observability/index.tsx` | 成本卡拆为首 token / 整体两段：P50、P95、样本/最大 |
| `pages/(main)/ai/observability/__tests__/index.test.tsx` | fixture 与断言更新为双口径 |
| `lib/i18n/translations.ts` | zh/en 新增 P50、样本数、首 token / 整体标题 4 键 |

---

## 3. 口径与兼容

| 字段 | 事件 | 含义 |
|---|---|---|
| `latencyMs.p50/p95/max/count` | `stream_end` | 兼容顶层字段；语义固定为整体时延 |
| `latencyMs.overall.*` | `stream_end` | 整体时延（回答完成） |
| `latencyMs.firstToken.*` | `first_token` | 首 token 时延 |
| `AIHealthQuality.latencyP95Ms` | `stream_end` | 兼容字段 = 整体 P95 |
| `AIHealthQuality.firstTokenP95Ms` | `first_token` | 健康 SLO 使用值 |
| `AIHealthQuality.overallP95Ms` | `stream_end` | 整体 P95 |

- 两套口径均使用 `percentile_cont`，P50 为 0.50、P95 为 0.95。
- 无样本时对应 breakdown 省略；仅有首 token 时 `latencyMs` 仍下发，但顶层整体字段为 0。
- 旧消费方继续读顶层 `p95` 时，得到的是整体 P95（修正后的正确语义），不再是混合事件 P95。

---

## 4. 验证

### 4.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `gofmt -w <改动 Go 文件>` | ✅ 无格式差异 |
| `go test -count=1 ./service ./handlers/ai` | ✅ `ok itsm-backend/service 51.329s`；`ok itsm-backend/handlers/ai 6.277s` |

关键断言：

- `TestGetAssistantTelemetrySummary_Aggregates`：`overall.p95=250`、`firstToken.p95=120`、顶层 `p50=100`/`count=10`。
- `TestGetAssistantTelemetrySummary_ClampsDays`：无样本 → `LatencyMs=nil`。
- `TestGetTelemetrySummary_Success`：HTTP 载荷含 `latencyMs.firstToken/overall`。
- `TestEvaluateAIHealthSLOs_FirstTokenIndependentFromOverall`：整体 9000ms + 首 token 2000ms → ok；首 token 3200ms + 整体 1000ms → breach。

### 4.2 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm test -- --runTestsByPath src/pages/(main)/ai/observability/__tests__/index.test.tsx --coverage=false --runInBand --silent` | ✅ 1 suite / 17 tests passed |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

关键断言：

- 首 token：P50 `280 ms`、P95 `450 ms`、样本 `35`。
- 整体：P50 `800 ms`、P95 `1500 ms`、最大 `3000 ms`。
- 两组数值独立渲染，证明未复用同一个混合字段。

---

## 5. 局限与后续

1. **模型分级路由与成本上限未做**：归 **T5-07b**（Q5）。
2. **未跑真实 PG**：`percentile_cont ... FILTER` 的 SQL 形态由 sqlmock 覆盖；真实库回归仍属联调条件项。
3. **首 token 事件缺失时 SLO 不判定**：按 `insufficient_data` 处理，不回退到整体时延，避免错误达标。
4. **看板不展示 avg**：保留 API 兼容字段，但看板聚焦 P50/P95/样本；avg 可作为后续排障补充。

---

## 6. 变更文件

**新增**

- `docs/plan/evidence/assistant-dock/T5-07a-cost-latency-evidence.md`

**修改**

- `itsm-backend/service/ai_assistant_telemetry.go`
- `itsm-backend/service/ai_assistant_telemetry_test.go`
- `itsm-backend/service/ai_health.go`
- `itsm-backend/service/ai_health_test.go`
- `itsm-backend/handlers/ai/telemetry_handler_test.go`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/pages/(main)/ai/observability/index.tsx`
- `itsm-frontend/src/pages/(main)/ai/observability/__tests__/index.test.tsx`
- `itsm-frontend/src/lib/i18n/translations.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
