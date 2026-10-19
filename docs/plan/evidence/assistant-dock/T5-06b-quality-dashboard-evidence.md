# T5-06b 质量层看板接线证据（quality / models）

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-06b**（T5-06 子任务；数据源见 T5-06a）
- **范围**：观测看板 UI 接线 + 前端 API 类型 + i18n + 测试；**不含**后端聚合口径变更
- **关联文档**：方案 `ai-assistant-dock-plan-2026-10-18.md` §11.5 / §13.3 / §14.2 / §14.4-③④；交付台账 T5 段
- **提交**：随本文件同一提交（`feat(ai): T5-06b 质量层看板接线`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| summary `quality` 接入看板 | ✅ 新增 `QualityMetricsCard`，展示引用覆盖率 / 无引用回答率 / 点踩率 | `QualityMetricsCard.tsx`；页面回归 |
| summary `models` 接入看板 | ✅ 成本与时延卡新增模型分布 Top 20（标签：模型 · 计数） | `observability/index.tsx`；页面回归 |
| §11.5 三指标标注可测 | ✅ 卡片标题含「§11.5 可测」+ 绿色「可测」Tag | `quality-measurable`；i18n zh/en |
| 空分母不伪造 0 | ✅ 后端省略比率时显示「—」；真实 0 显示 `0.0%` | `QualityMetricsCard.test.tsx` 3 例 |
| 分母可解释 | ✅ 三指标分别展示 `有引用/可统计`、`无引用/可统计`、`点踩/反馈` | `quality-*-sample` testid |
| 门槛可执行 | ✅ 三指标已可读、可展示，可作为 §11.5 评审事实输入；门禁判定逻辑不变（V0-01~03 + 负向指标） | 本文 §3、§5 |

---

## 2. 实现清单

### 2.1 前端 API 类型

`src/lib/api/ai-api.ts`：

- 新增 `AssistantTelemetryQuality`：
  - `answerCount` / `citationAnswerCount` / `feedbackCount` / `downvoteCount`
  - `citationCoverage?` / `noCitationRate?` / `downvoteRate?`（空分母时后端省略）
- `AssistantTelemetrySummary` 新增 `quality?` 与 `models?`（后向兼容旧后端/旧 mock）。

### 2.2 质量层指标卡

新增 `src/components/ai/observability/QualityMetricsCard.tsx`：

- 标题：`质量层三指标（§11.5 可测）`；右上角绿色 `可测` Tag。
- 三列指标：引用覆盖率、无引用回答率、点踩率。
- 每列下方展示分母样本：
  - `有引用 {cited} / 可统计 {total} 回答`
  - `无引用 {uncited} / 可统计 {total} 回答`
  - `点踩 {downvotes} / 反馈 {total}`
- 口径说明：`sources_count` 为 `NULL` 的旧客户端事件不计入引用分母；分母为 0 显示「—」。
- 空样本：无回答且无反馈时额外显示「窗口内暂无可统计的回答或反馈」。
- `fmtQualityRate` 仅接受有限数值；`NaN` / `undefined` / `null` 一律「—」。

### 2.3 页面接线

`src/pages/(main)/ai/observability/index.tsx`：

- 在「试点对照报告卡」后挂载 `<QualityMetricsCard quality={data?.quality} />`。
- 成本与时延卡内新增模型分布：
  - 数据源 `data?.models ?? []`；
  - 标签渲染 `模型 · 计数`；
  - 空数组显示「窗口内暂无模型上报」；
  - testid：`model-distribution` / `model-distribution-empty`。

### 2.4 i18n

`src/lib/i18n/translations.ts` 的 `aiObservability` 命名空间新增 zh/en 各 12 键：

- `qualityTitle` / `qualityMeasurable` / `qualityDescription`
- `qualityCitationCoverage` / `qualityNoCitationRate` / `qualityDownvoteRate`
- `qualityCitationSample` / `qualityNoCitationSample` / `qualityDownvoteSample`
- `qualityEmpty`
- `modelDistributionTitle` / `modelDistributionEmpty`

---

## 3. 口径与边界

| 指标 | 后端分子 / 分母 | 前端展示 |
|---|---|---|
| 引用覆盖率 | `citationAnswerCount / answerCount`；分母 = `stream_end AND sources_count IS NOT NULL` | 百分比；空分母「—」 |
| 无引用回答率 | `(answerCount - citationAnswerCount) / answerCount` | 百分比；空分母「—」 |
| 点踩率 | `downvoteCount / feedbackCount`；分母 = `message_feedback AND feedback <> ''` | 百分比；空分母「—」 |
| 模型分布 | `stream_end AND model <> ''`，按 model 计数 Top 20 | `模型 · 计数` 标签 |

边界：

- 比率由后端以 **0..1 小数**返回，前端统一乘 100 保留 1 位。
- `answerCount` / `feedbackCount` 为 0 时，后端不返回对应比率；前端不得用计数自行补 0。
- 模型分布只覆盖上报了 `model` 的 `stream_end`；静态回退未识别模型时不伪造默认值。
- §11.5 的通过条件仍由试点对照报告（V0-01~03 + 负向指标）决定；本任务只保证质量三指标可见、可解释、可作为评审事实输入。

---

## 4. 验证

### 4.1 前端

| 命令（workdir `itsm-frontend`） | 结果 |
|---|---|
| `npm test -- --runTestsByPath src/components/ai/observability/__tests__/QualityMetricsCard.test.tsx "src/pages/(main)/ai/observability/__tests__/index.test.tsx" --coverage=false --runInBand --silent` | ✅ **2 suites / 20 tests passed** |
| `npm test -- --runTestsByPath <观测看板 4 个组件套件 + 页面套件> --coverage=false --runInBand --silent` | ✅ **5 suites / 33 tests passed** |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

> 全量回归套件：`QualityMetricsCard`、`PilotReportCard`、`SegmentBreakdownCard`、`DocMetricsCard`、`observability/index`。

关键断言：

- `QualityMetricsCard.test.tsx`
  - 三率与分母样本正确渲染；
  - 空分母三率均为「—」且显示空样本说明；
  - 真实 0 值保留 `0.0%`，与空分母区分。
- `observability/index.test.tsx`
  - `quality` fixture 渲染 `60.0% / 40.0% / 25.0%` 与 `6/10`、`2/8` 样本；
  - `models` fixture 渲染 `gpt-4o-mini · 30`、`claude-sonnet-4 · 5`。

### 4.2 未运行项

- 未跑真实浏览器截图：本轮为组件/页面级 jsdom 回归。
- 未跑真实 PG：后端聚合口径已在 T5-06a 验证，本任务未改后端。

---

## 5. 局限与后续

1. **模型分布为标签列表**：Top 20 内可读；若后续需要占比图/导出，归 T5-07（成本口径与看板）继续演进。
2. **质量指标不参与门禁算法**：§11.5 通过条件仍为试点达成 + 负向指标不恶化；三指标当前用于评审解释与风险判断。
3. **旧客户端样本偏少**：`sources_count = NULL` 被排除，引用覆盖率只覆盖新客户端；看板已通过分母文案显式说明。
4. **无历史回填**：质量指标从 T5-06a 上线后开始累积，窗口内样本不足时以「—」呈现。

---

## 6. 变更文件

**新增**

- `itsm-frontend/src/components/ai/observability/QualityMetricsCard.tsx`
- `itsm-frontend/src/components/ai/observability/__tests__/QualityMetricsCard.test.tsx`
- `docs/plan/evidence/assistant-dock/T5-06b-quality-dashboard-evidence.md`

**修改**

- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/lib/i18n/translations.ts`
- `itsm-frontend/src/pages/(main)/ai/observability/index.tsx`
- `itsm-frontend/src/pages/(main)/ai/observability/__tests__/index.test.tsx`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
- `docs/plan/evidence/assistant-dock/T5-06a-quality-data-source-evidence.md`（仅回写 T5-06b 已完成状态）
