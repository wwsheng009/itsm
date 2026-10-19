# O2-01 分析增强（建议排序权重 + 分群对比） · 交付证据

- 日期：2026-10-18
- 任务：O2-01（方案 §13.6 分析增强｜验收：§11 报告自动产出；建议采纳率提升可验证）
- 状态：`integration_verified`（规则排序闭环 + 忽略样本衰减 + 分群对比看板；模型化排序与真实 PG 冒烟归后续）
- 交付物：
  - **建议排序权重（先规则）**：
    `service/ai_suggestions.go`（`AggregateSuggestionWeights`：近 30 天 `suggestion_shown/clicked/ignored`
    按 `payload.cardId` 分组计数）+ `handlers/ai/suggestion_ranking.go`（拉普拉斯平滑打分 + 组内稳定重排）+
    `GetAssistantContext` 接线（响应新增 `ranking: weighted|registry`、每条建议 `score`/`feedback`）
  - **分群对比**：`AggregateAssistantSegments`（维度白名单 `role|pageKey`；单条 GROUP BY 聚合采用/建议/任务/
    Inline/错误子集）+ `GetAssistantSegments`（派生比率，分母 0 → 0）+ 端点
    `GET /api/v1/ai/observability/segments?dimension=&days=`（ai:read）+ `authz-gen` 预检再生
  - 前端：`aiGetAssistantSegments()` + `components/ai/observability/SegmentBreakdownCard.tsx`
    （维度切换 按角色/按页面、子集指标表、空态/降级）+ `/ai/observability` 挂载（跟随 `days`）
  - 报告面（§11）：V0-04 试点对照报告已交付；本批为其补上**分群对比**与**排序反馈闭环**，共用同一事件面
  - **忽略样本衰减（补全，2026-10-18）**：`AggregateSuggestionWeights` 改为「卡 × 事件 × 天龄」分桶 →
    半衰期 14 天指数衰减（`0.5^(age/14)`，桶内 age 取 `ageDays+0.5` 均值修正）＋ `suggestion_ranking.go`
    打分补 λ=0.5 忽略惩罚；D1-02 `suggestion_ignored` 采集接线后，忽略样本即时进入排序

## 1. 口径（实现即契约）

| 项 | 口径 |
|---|---|
| 排序打分 | `score = (clicked' + k·prior) / (shown' + λ·ignored' + k)`，`k=10`、`λ=0.5`；`prior` = 全局 CTR（无展示数据 → 经验先验 0.2）；`'` = 衰减有效计数 |
| 反馈衰减 | 按「卡 × 事件 × 天龄」分桶后 **14 天半衰期**指数衰减：`0.5^(age/14)`（桶内 age 取 `ageDays+0.5` 均值修正）；`decayedShown/decayedClicked/decayedIgnored` 随 `feedback` 透出（4 位小数） |
| 重排范围 | **仅组内**（任务卡/问题卡各自排序）→ 保持 I0-02「任务在前」分组契约；并列保持注册表顺序（稳定排序） |
| 冷启动 | 无反馈的卡取全局先验分（中性），不因缺数据被惩罚 |
| ignored | D1-02 会话级结算（展示后未点击）→ 作为**隐式负样本**按 λ=0.5 计入打分分母；原始计数与衰减计数随 `feedback` 透出供审计 |
| 降级 | 埋点服务未装配 → 不重排、`ranking=registry`（与既有静态基线逐字段一致，仅新增 ranking 字段） |
| 分群维度 | 白名单 `role` / `pageKey`（列名显式映射防注入）；非法维度 → 400；空值分组剔除；Top 20 |
| 分群比率 | 建议 CTR=`clicked/shown`；任务完成率=`completed/started`；Inline 采纳率=`applied/preview`；错误率=`error_shown/events`（分母 0 → 0，不产生 NaN） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l service/ai_suggestions.go handlers/ai/suggestion_ranking.go handlers/ai/assistant_context.go handlers/ai/segments.go
go test ./service/ -run 'TestGetSuggestionWeights|TestGetAssistantSegments|TestBuildAssistantSegments' -count=1
go test ./handlers/ai/ -run 'TestRankAssistantSuggestions|TestGlobalSuggestionCtr|TestGetAssistantSegments_Contract|TestGetAssistantContext_WeightedRanking' -count=1
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1     # 5 包全绿
go run ./cmd/authz-gen   # 再生：/api/v1/ai/observability/segments → ai:read

# —— 补全：忽略样本衰减 + 负样本打分（2026-10-18）——
gofmt -l service/ai_suggestions.go service/ai_suggestions_test.go handlers/ai/suggestion_ranking.go handlers/ai/suggestion_ranking_test.go handlers/ai/suggestion_ranking_handler_test.go handlers/ai/assistant_context.go   # 0
go test ./service/ -run 'TestGetSuggestionWeights|TestDecayedSuggestionCount' -count=1 -v   # Aggregates/HalfLife PASS；ok 1.6s
go test ./handlers/ai/ -run 'TestRankAssistantSuggestions|TestGetAssistantContext' -count=1 -v   # 5 例 PASS；ok 1.4s
go test -count=1 ./handlers/ai/      # 全包 ok 7.1s；./service/ 全包 ok 57.5s

cd ../itsm-frontend
npx jest "src/components/ai/observability/__tests__/SegmentBreakdownCard.test.tsx" --runInBand   # 4 例
npx jest --testPathPattern "ai/observability" --runInBand                                        # 3 suites / 21 tests
npx jest src/components/ai/chat-core/__tests__/SuggestionCards.test.tsx --runInBand --coverage=false   # 13 例（忽略结算 4 例）
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils src/lib/api/__tests__/ai-api.test.ts --runInBand
# → 66 suites / 621 tests 全绿（补全后复跑）
npx tsc --noEmit ; npx eslint src/components/ai/chat-core/SuggestionCards.tsx src/lib/api/ai-api.ts …
```

关键断言：
- 排序：任务卡内 CTR 高的 `reply_draft` 提前，问题卡仍在其后（分组契约保持）、rank 重编号 0..n；
  无反馈卡 `Feedback=nil` 且分数 = 全局先验（冷启动中性）；并列保持注册表顺序；
  埋点未装配 → `ranking=registry` 且顺序 = 注册表（兼容态，仅多一个字段）；
- 权重聚合：per-card shown/clicked/ignored 原始计数 + `decayed*` 值正确（`attention_today` 30/5/3 →
  `19.511/4.6421/2.5227`；`triage_plan` 20/1/9 → `19.511/0.9755/8.7799`；半衰期口径可复算，
  `TestDecayedSuggestionCount_HalfLife`）；未装配/非法租户 → 明确错误；
- 忽略惩罚：同一 shown/clicked、ignored 10 → 0 的排序翻转；衰减排序：近窗口高 CTR 卡优先于旧窗口同 CTR 卡；
- 分群：200 + 比率（30.0%/60.0%/45.0%/2.0%）+ 窗口 clamp（999→90）；`page_key` 别名归一；
  非法维度 400（**先于** 503 判定，错误码不被掩盖）；缺租户 401；未装配 503；空窗口 → 说明；
- 前端：四张卡（健康/SLO、自助服务、试点对照、分群对比）并挂看板；分群维度切换与 `days` 联动重取；503 降级。

## 3. 条件项（不阻塞）

- ~~忽略样本负样本衰减~~ → **已闭环**（2026-10-18 补全：半衰期 14 天 + λ=0.5 忽略惩罚 + D1-02 采集接线）；
- **模型化排序（"后模型"）** 归 P2；当前规则闭环已让 CTR 反馈直接作用于下发顺序；
- 聚合 SQL 为 PG 专有（`FILTER`/`jsonb->>`/`percentile_cont`）：真实 PG 冒烟归联调（与 V0-04 同一登记）；
- "租户维度"分群属平台/MSP 侧视图，不在本租户端点范围（MSP 帮助空间归 P2）；
- 建议排序的 A/B 对照（weighted vs registry）可用现有 `ranking` 字段在事件侧加维度，归后续运营迭代。