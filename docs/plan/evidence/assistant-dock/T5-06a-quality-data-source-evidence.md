# T5-06a 质量层数据源证据（citation / 无引用 / 点踩 / model / query_hash）

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-06a**（由 T5-06 拆分；看板接线归 T5-06b）
- **范围**：事件契约 + 前端埋点 + 后端字段/落库/聚合；**不含**看板 UI
- **关联文档**：方案 `ai-assistant-dock-plan-2026-10-18.md` §13.2 / §13.3 / §14.2；评审 §7-F / §11.5；交付台账 T5 段
- **提交**：随本文件同一提交（`feat(ai): T5-06a 质量层数据源`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| 质量层事件契约可执行 | ✅ 方案 §13.2 事件字典新增 `message_feedback` 与质量字段；前端 SDK 白名单、后端服务端白名单同步 | 方案 §13.2；`telemetry.ts` / `handlers/ai/telemetry.go` |
| citation 埋点（sources 数 / 无引用回答） | ✅ `stream_end` 恒上报 `sourcesCount`；`0` = 无引用回答；`NULL` = 旧客户端未上报，不计入分母 | `chat-telemetry.ts`；`ai_assistant_telemetry.go`；单测 |
| 回答级点踩 | ✅ 回答下方「有用 / 没用」按钮，单条消息只允许反馈一次；上报 `message_feedback`（`helpful`/`unhelpful`） | `ChatMessageItem.tsx`；`ChatMessageItem.test.tsx` |
| `model` 上报 | ✅ done SSE 可选 `model?`，仅 provider resolution 有 Model 时下发；`stream_end` 落库并聚合 Top 20 | `handler.go`；`ai-api.ts`；`chat-telemetry.ts` |
| `query_hash` 补链 | ✅ 前端 FNV-1a 32 位哈希；`message_sent` / `stream_end` / `error_shown` / `message_feedback` 均可关联；不采集问题原文 | `telemetry.ts`；`chat-telemetry.ts`；`use-ai-chat-session.ts` |
| 指标可算 | ✅ `GET /api/v1/ai/telemetry/summary` 返回 `quality`（三率）与 `models` | `ai_assistant_telemetry.go`；handler/service 单测 |
| 看板接线 | ✅ 已由 **T5-06b**（2026-10-19）完成 | 方案 §14.2；证据 `assistant-dock/T5-06b-quality-dashboard-evidence.md` |

---

## 2. 事件与字段契约

### 2.1 字段语义

| 字段 | 类型 / 约束 | 产生位置 | 语义与分母口径 |
|---|---|---|---|
| `model` | `VARCHAR(128) NOT NULL DEFAULT ''` | done SSE `model?` → `stream_end` | 本回合实际生效模型。仅 `resolution.Model != ""` 时下发；静态回退不伪造。模型分布只统计 `stream_end AND model <> ''` |
| `query_hash` | `VARCHAR(64) NOT NULL DEFAULT ''` | `message_sent` / `stream_end` / `error_shown` / `message_feedback` | 问题文本 FNV-1a 32 位哈希（8 位十六进制）；仅用于同题分组/关联，不存原文 |
| `sources_count` | `INTEGER NULL` | `stream_end`（新客户端恒上报）；`error_shown` 条件性 | `> 0` = 有引用；`0` = **显式无引用回答**；`NULL` = 旧客户端未上报，**不计入引用覆盖率分母** |
| `feedback` | `VARCHAR(16) NOT NULL DEFAULT ''` | `message_feedback` | 仅接受 `helpful` / `unhelpful`；服务端 `trim + lower` 后白名单校验，未知值丢弃 |
| `conversation_id` | 既有字段 | 上述事件 | 回答级反馈与会话关联；历史消息由会话 id + 前置用户问题哈希补齐 |

### 2.2 向后兼容

- **SSE v1 只增不改**：done 载荷在既有 `{conversationId, provider?, providerSource?}` 后追加可选 `model?`；既有字段语义不变，旧客户端忽略未知字段。
- **旧客户端**：无 `sourcesCount` → `sources_count = NULL`，不进入引用覆盖率分母；无 `message_feedback` → 点踩率分母为 0，接口返回 `null`。
- **旧后端**：done 无 `model` → 前端不落该字段；既有 token/usage 行为不变。
- **历史数据**：迁移不回填；旧行 `sources_count = NULL`、`feedback = ''`、`model = ''`，按上述口径自然排除。

---

## 3. 实现清单

### 3.1 前端

| 文件 | 改动 |
|---|---|
| `src/lib/ai/telemetry.ts` | 事件名新增 `message_feedback`；`AssistantEventInput` 新增 `sourcesCount` / `hasSources` / `queryHash` / `feedback`，并加入 `COPYABLE_FIELDS`；新增 `hashTelemetryText`（FNV-1a 32 位） |
| `src/lib/ai/chat-telemetry.ts` | `onSend(queryLength)` → `onSend(query)`（内部算 `queryHash`，保留 `extra.queryLength`）；新增 `onSources(count)`；`stream_end` 恒上报 `sourcesCount` / `hasSources`（无 sources 事件按 `0`）；`error_shown` 条件性带 `queryHash` / `sourcesCount`；回合结束/失败后复位状态 |
| `src/lib/api/ai-api.ts` | `AIChatDoneInfo`、done SSE 类型与解析新增 `model?`；仅当存在 provider/providerSource/model/usage 时构造 info（旧行为不变） |
| `src/components/ai/chat-core/use-ai-chat-session.ts` | `onSources` 接线；done 回填 `model`；消息补 `conversationId` / `queryHash`；历史消息按“前置用户问题”补哈希（含工作区交接会话） |
| `src/components/ai/chat-core/ChatMessageItem.tsx` | 回答下方新增「有用 / 没用」反馈按钮；点击后上报 `message_feedback`（含 `conversationId` / `queryHash` / `feedback` / `extra.helpful`）并固化，防重复计数 |
| `src/components/ai/chat-core/types.ts` | `ChatMessage` 新增 `conversationId?` / `queryHash?` |

### 3.2 后端

| 文件 | 改动 |
|---|---|
| `migrations/20261019_ai_telemetry_quality_fields.sql` | `ai_telemetry_events` 新增 `model` / `query_hash` / `sources_count` / `feedback`；新增 `(tenant_id, model, occurred_at)` 与 `(tenant_id, query_hash)` 索引；`IF NOT EXISTS` 幂等 |
| `migrations/20261019_ai_telemetry_quality_fields_down.sql` | 对称回滚（删索引 + 删列） |
| `handlers/ai/telemetry.go` | 白名单新增 `message_feedback`；解析 `model` / `queryHash` / `sourcesCount` / `feedback`；新增 `telemetryNonNegativeInt`（缺失/负数 → nil）与 `telemetryFeedback`（仅 `helpful`/`unhelpful`） |
| `service/ai_assistant_telemetry.go` | `AssistantTelemetryEvent` 新增 4 字段；INSERT 14 → **18** 列；新增 `AssistantTelemetryQuality`；summary 新增 `quality` / `models`；新增质量聚合与模型 Top 20；分母为 0 时比率返回 `nil` |
| `handlers/ai/handler.go` | done 载荷在 `resolution.Model != ""` 时追加 `model` |
| `handlers/ai/sse_events.go` | done 载荷文档与注册表更新为 `{conversationId, provider?, providerSource?, model?}`；注明 v1 只允许追加可选字段 |

---

## 4. 聚合口径

`GET /api/v1/ai/telemetry/summary`（租户 + `days` + 可选 `pageKey` / `role`）新增：

| 指标 | 计算 | 空分母行为 |
|---|---|---|
| `answerCount` | `COUNT(*) FILTER (WHERE name='stream_end' AND sources_count IS NOT NULL)` | — |
| `citationAnswerCount` | `COUNT(*) FILTER (WHERE name='stream_end' AND sources_count > 0)` | — |
| `citationCoverage` | `citationAnswerCount / answerCount` | `null` |
| `noCitationRate` | `(answerCount - citationAnswerCount) / answerCount` | `null` |
| `feedbackCount` | `COUNT(*) FILTER (WHERE name='message_feedback' AND feedback <> '')` | — |
| `downvoteCount` | `COUNT(*) FILTER (WHERE name='message_feedback' AND feedback='unhelpful')` | — |
| `downvoteRate` | `downvoteCount / feedbackCount` | `null` |
| `models` | `stream_end AND model <> ''` 按 `model` 分组，`COUNT(*) DESC LIMIT 20` | 空数组 |

> 比率为 **0..1 小数**（非百分数），由消费方格式化；空分母返回 `null`，前端应显示“—”而不是伪造 0。

---

## 5. 验证

### 5.1 后端

| 命令（workdir `itsm-backend`） | 结果 |
|---|---|
| `go test -count=1 ./service -run 'TestIngestAssistantEvents\|TestGetAssistantTelemetrySummary'` | ✅ `ok itsm-backend/service 1.734s` |
| `go test -count=1 ./handlers/ai -run 'TestIngestTelemetryEvents\|TestGetTelemetrySummary'` | ✅ `ok itsm-backend/handlers/ai 1.664s` |
| `go test -count=1 ./migration` | ✅ `ok itsm-backend/migration 1.123s` |
| `gofmt -l <改动 Go 文件>` | ✅ 无输出 |

关键断言：

- `TestIngestAssistantEvents_Insert`：2 条事件 × **18 列**，质量字段逐列匹配。
- `TestGetAssistantTelemetrySummary_Aggregates`：`citationCoverage=0.6`、`noCitationRate=0.4`、`downvoteRate=0.25`；`models` 顺序与计数。
- `TestGetAssistantTelemetrySummary_ClampsDays`：空分母三率均为 `nil`。
- `TestIngestTelemetryEvents_PersistsQualityFields`：`message_feedback` + `sourcesCount=0` + `feedback=unhelpful` 正确落库。
- `TestGetTelemetrySummary_Success`：HTTP 响应含 `quality.citationCoverage=0.6` 与 `models`。
- 迁移测试：up/down 成对与幂等约束通过。

### 5.2 前端

| 命令（workdir `itsm-frontend`） | 结果 |
|---|---|
| `npx jest <4 个相关套件> --runInBand --coverage=false` | ✅ **4 suites / 42 tests passed**（5.086s） |
| `npx eslint <改动文件>` | ✅ exit 0 |
| `npx tsc --noEmit` | ✅ exit 0 |

> 说明：首次运行未加 `--coverage=false` 时 42 用例同样全部通过，但因“只跑子集”触发仓库全局覆盖率阈值而进程退出 1；这不是用例失败。随后以 `--coverage=false` 复跑取得干净退出码。

关键断言：

- `telemetry.test.ts`：事件名与方案 §13.2 字典逐项一致（含 `message_feedback`）。
- `chat-telemetry.test.ts`：完整回合 `stream_end` 携带 `model` / `sourcesCount` / `hasSources` / `queryHash`；**无 sources 事件时记 0**。
- `ChatMessageItem.test.tsx`：点踩上报一次后固化，重复点击不重复计数。
- `use-ai-chat-session.test.tsx`：历史消息补 `conversationId` 与前置问题 `queryHash`。

---

## 6. 局限与后续

1. **看板接线已由 T5-06b 完成**：`quality` / `models` 已在 summary API 可算可读，并接入观测看板与 §11.5 质量指标展示；详见 `assistant-dock/T5-06b-quality-dashboard-evidence.md`。
2. **未跑真实 PG 集成**：本轮为 sqlmock + 迁移测试；真实库回归仍属方案 §14.3 条件项。
3. **反馈去重仅前端**：单条消息只允许反馈一次；服务端未按消息去重（事件无 message id）。严格去重需后续补事件去重键。
4. **哈希碰撞**：FNV-1a 32 位仅用于分组关联，不作为身份标识；碰撞概率可接受但不为零。
5. **`model` 可选**：静态回退未配置模型时不上报，模型分布只覆盖可识别模型的回合，不伪造默认值。
6. **`sources_count` 取最新 sources 事件**：当前后端每回合发送一次 sources 事件；若未来改为多次，需明确“累计”还是“最终”口径。
7. **历史数据不回填**：旧行按 `NULL` / 空串排除，质量指标从新版本上线后开始累积。

---

## 7. 变更文件

**新增**

- `itsm-backend/migrations/20261019_ai_telemetry_quality_fields.sql`
- `itsm-backend/migrations/20261019_ai_telemetry_quality_fields_down.sql`
- `docs/plan/evidence/assistant-dock/T5-06a-quality-data-source-evidence.md`

**修改**

- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
- `itsm-backend/handlers/ai/handler.go`
- `itsm-backend/handlers/ai/sse_events.go`
- `itsm-backend/handlers/ai/telemetry.go`
- `itsm-backend/handlers/ai/telemetry_handler_test.go`
- `itsm-backend/service/ai_assistant_telemetry.go`
- `itsm-backend/service/ai_assistant_telemetry_test.go`
- `itsm-frontend/src/lib/ai/telemetry.ts`
- `itsm-frontend/src/lib/ai/chat-telemetry.ts`
- `itsm-frontend/src/lib/ai/__tests__/telemetry.test.ts`
- `itsm-frontend/src/lib/ai/__tests__/chat-telemetry.test.ts`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/components/ai/chat-core/types.ts`
- `itsm-frontend/src/components/ai/chat-core/use-ai-chat-session.ts`
- `itsm-frontend/src/components/ai/chat-core/ChatMessageItem.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/use-ai-chat-session.test.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/ChatMessageItem.test.tsx`
