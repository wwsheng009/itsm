# V0-02 一句话建单 + 自助问答（员工侧） · 交付证据

- 日期：2026-10-18
- 任务：V0-02（方案 §7 V 系列｜验收：自助解决率、建单时长、误分类率）
- 状态：`integration_verified`（员工侧自助问答 + 一句话建单度量闭环 + 看板基线；真实 Provider/RAG E2E 归联调）
- 交付物：
  - 度量契约：事件字典 +4（`self_service_answered/resolved/ticket_created`、`ai_create_field_corrected`）
    —— 前端 SDK `lib/ai/telemetry.ts`、后端白名单 `handlers/ai/telemetry.go`、方案 §13.2 同步
  - 度量逻辑：`lib/ai/ai-create-metrics.ts`（`AICreateFlowTracker`：建单时长 / 字段纠正（误分类率代理）/ 漏斗）
  - 员工侧自助问答：`components/ai/self-service/SelfServiceAssistant.tsx`
    （知识库 Top3 + 匹配度 →「已解决（不用建单）」/「还没解决，去建单」；未命中直接给建单 CTA；**不做任何写操作**）
  - 一句话建单接线：`A2UIFormRenderer` 受控 `intent/onIntentChange/pageKey` + 度量埋点
    （生成即 `task_started`；提交成功 `task_completed(latencyMs=durationMs)`；失败 `task_failed`；
    提交前对比 AI 建议 `type/priority` → `ai_create_field_corrected`）；
    `/tickets/ai-create` 页组合「先自助 → 未解决带入建单意图（不自动生成/提交）」
  - 看板基线：`/ai/observability`「自助服务与建单（V0-02 基线）」卡（自助问答数/自助解决率/转建单/字段纠正）

## 1. 口径（实现即契约）

| 指标 | 口径 | 说明 |
|---|---|---|
| 自助解决率 | `self_service_resolved / self_service_answered` | 命中与未命中都计 answered（未命中不计 resolved） |
| 转建单 | `self_service_ticket_created` 计数 | 自助未解决 → 带入建单意图（不自动提交） |
| 误分类率代理 | `ai_create_field_corrected / task_completed(ai_create)` | **无人工标注真值**：以「用户修改 AI 建议的 type/priority」为代理 |
| 建单时长 | `task_completed.durationMs`（generate → 提交成功） | 同一数值写入 `latencyMs`，使 O0-03 时延桶包含建单样本（小样本，口径已记录） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npx jest src/components/ai/self-service src/components/a2ui src/lib/ai/__tests__/ai-create-metrics.test.ts src/lib/ai/__tests__/telemetry.test.ts --runInBand
# → 4 suites / 28 tests（self-service 6 + a2ui 2 + metrics 4 + 字典契约）
npx jest --testPathPattern "tickets/ai-create" --runInBand                      # 1 suite / 1 test（页面接线）
npx jest --testPathPattern "ai/observability" --runInBand                       # 1 suite / 11 tests（含自助服务卡）
npx jest src/components/ai src/components/a2ui src/lib/ai src/lib/store src/lib/utils --runInBand
# → 53 suites / 463 tests 全绿
npx tsc --noEmit ; npx eslint src/components/ai/self-service src/components/a2ui src/lib/ai/ai-create-metrics.ts "src/pages/*/tickets/ai-create/*" "src/pages/*/ai/observability/*"

cd ../itsm-backend
gofmt -l handlers/ai/telemetry.go      # 无输出（已格式化）
go test ./handlers/ai/ -run 'TestIngestTelemetryEvents' -count=1   # ok（白名单容忍未知事件；新增 4 项已登记）
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1   # 5 包全绿
```

关键断言：
- 自助问答：命中渲染标题/匹配度/摘要并埋点 `self_service_answered{results:1}`；**检索命中不触发建单**（负向）；
  「已解决」→ `self_service_resolved` + 不建单提示；「还没解决」→ `self_service_ticket_created` + 回传问题描述；
  未命中/检索失败 → 降级为「去建单」CTA（失败埋点 `errorCode=search_failed`）；空输入按钮禁用不检索；
- 建单度量：生成 → `task_started`；把 AI 建议 `hardware` 改为 `software` 后提交成功 →
  `ai_create_field_corrected{field:'type'}` + `task_completed{durationMs, latencyMs}`；生成失败 → `task_failed{generate_failed}` 且无 completed；
- 页面对接：自助「去建单」把描述写入表单意图（受控）且**不自动调用生成**；
- 看板：10 次自助 / 6 次解决 → 60.0%；转建单 3；字段纠正 2（口径文案随卡展示）。

## 3. 条件项（不阻塞）

- 误分类率**真值**需要人工标注/回访（当前为字段纠正代理）；如后续引入 AI 分类评测集，可在同事件上补 `correct` 标签；
- 自助解决率仅覆盖"走本页自助"的样本；面板/工作区内提问不进此指标（各自的 message_sent/task 漏斗已覆盖）；
- 命中率（`results>0` 比例）可从 `self_service_answered` 的 `extra.results` 分桶派生，运营报表归 O2-01；
- 移动端/门户入口与 5 分钟建单时长目标验证归 P2/联调；真实 Provider/RAG E2E 归联调。