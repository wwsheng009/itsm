# 文档起草采纳率（H1-01 / P2 条件项闭环）证据

- 日期：2026-10-18
- 状态：`integration_verified`
- 对应方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`（H1-01 条件项「doc_draft_created 前端上报点待产物链路」、
  P2 条件项「起草采纳率（草稿→发布）无信号未进报表」）

## 1. 交付范围

把「AI 起草 → 落地成文」的信号接进遥测字典与 P2 运营报表，形成可复跑的**起草落地率**：

1. 起草动作信号 `doc_draft_created`：会话页两个入口（「补充为知识文章」单条回答、「保存成文章」整段会话）上报；
2. 终点信号 `doc_draft_published`：新建文章页仅在 **AI 预填驱动且创建成功** 后上报；
3. 报表 `draftPublishRate = published / drafts`（零分母 null，不虚报 0%）与 `published` 计数
   （总量 / 按页 / 按天三处聚合 + 前端卡 + Markdown 导出 + i18n zh/en）。

## 2. 信号接线

| 位置 | 事件 | 参数 | 说明 |
|---|---|---|---|
| `itsm-frontend/src/components/ai/chat-core/use-ai-chat-session.ts` | `doc_draft_created` | `source=chat_promote_answer`、`outcomeType=draft`、`extra.kind=answer` | 「补充为知识文章」点击，携带当前 `pageKey` |
| 同上（`handleSaveConversationAsArticle`） | `doc_draft_created` | `source=chat_promote_conversation`、`conversationId`、`extra.kind=conversation` | 标题栏「保存成文章」 |
| `itsm-frontend/src/pages/(main)/knowledge/articles/new/index.tsx` | `doc_draft_published` | `source=article_new_prefill`、`pageKey=knowledge.article.new`、`extra.kind=answer\|conversation` | 仅 `prefillRequest` 非空且 `createArticle` 成功后上报；普通空白新建不上报 |

字典同步：

- 前端 `src/lib/ai/telemetry.ts` `ASSISTANT_EVENT_NAMES` 增 `doc_draft_published`（冻结单测 `telemetry.test.ts` 同步）；
- 后端 `handlers/ai/telemetry.go` 白名单增 `doc_draft_published`（未知事件仍静默丢弃 + dropped 计数，语义不变）。

## 3. 报表口径

`itsm-backend/service/ai_doc_metrics.go`：

- 总量：`published` = `COUNT(name='doc_draft_published')`；比率 `rates.draftPublishRate = published/drafts`；
- 按页 / 按天：`doc_draft_published` 并入 `name IN (...)`，各自新增 `published` 列（顺序在 `drafts` 之后）；
- 分母为 0（无起草动作）→ `draftPublishRate` 省略 = null（前端渲染「—」）；
- 口径注记（报表内嵌）：窗口级漏斗（窗口内成文数 / 起草动作数），**非逐篇归因**；草稿在窗口末尾产生、
  成文落入下一窗口属已知近似。

前端消费：

- `src/lib/api/ai-api.ts`：`DocMetricsTotals.published`、`DocMetricsRates.draftPublishRate`、
  `DocMetricsPage.published`、`DocMetricsTrendPoint.published`；
- `DocMetricsCard`：新增 `doc-metrics-draft-publish-rate`、`doc-metrics-published` 统计与 `docMetricsPublishedCol` 列
  （`/ai/observability`），i18n zh/en（键 `docMetricsPublished/DraftPublishRate/DraftPublishHint/PublishedCol`）；
- `doc-metrics-markdown.ts`：概览新增「起草成文」「起草落地率」两行；按页/趋势表新增「成文」列；口径注记同步。

## 4. 复跑命令与实测结果（2026-10-18）

后端（workdir `itsm-backend`）：

```pwsh
go test ./service/ -run "TestGetAssistantDocMetrics|TestRatioOrNil" -count=1   # ok 1.9s
go test ./handlers/ai/ -run "DocMetrics|Telemetry" -count=1                     # ok 1.7s
go test ./service/ ./handlers/ai/ -count=1                                      # ok（service 52.9s / handlers/ai 7.4s）
```

前端（workdir `itsm-frontend`）：

```pwsh
npx jest --runTestsByPath src/lib/ai/__tests__/telemetry.test.ts `
  src/components/ai/observability/__tests__/DocMetricsCard.test.tsx `
  src/pages/__tests__/knowledge-new-article-prefill.test.tsx `
  src/components/ai/chat-core/__tests__/use-ai-chat-session.test.tsx `
  --coverage=false --watchAll=false --runInBand           # 4 suites / 43 tests 全绿（exit 0）
npx jest src/lib/ai src/components/ai --runInBand --silent --coverage=false  # 48 suites / 307 tests（exit 0）
npx jest src/lib/api --runInBand --silent --coverage=false                    # 81 suites / 1373 tests（exit 0）
npx tsc --noEmit                                                              # exit 0
npx eslint <变更文件 11 个>                                                    # exit 0
```

## 5. 关键断言（测试锁定）

- `use-ai-chat-session.test.tsx`：两个入口分别上报 `doc_draft_created`，参数含
  `source`、`outcomeType=draft`、`extra.kind`（会话入口另含 `conversationId`）；
- `knowledge-new-article-prefill.test.tsx`：AI 预填成文后 `doc_draft_published` 参数为
  `source=article_new_prefill` + `extra.kind=answer` 且导航到新文章；**空白新建不上报**（`not.toHaveBeenCalledWith`）；
- `DocMetricsCard.test.tsx`：`draftPublishRate=0.5 → 50.0%`、`published=1` 渲染；
  零分母与缺省 rates → 显示「—」；Markdown 导出含
  `| 起草落地率（成文 / 起草动作） | 50.0% |` 与按页「成文」列；
- `service/ai_doc_metrics_test.go`：三查询列序（`drafts,published,...`）与
  `published=2/drafts=3 → 0.6667` 断言；零起草 → `DraftPublishRate` nil；
- `handlers/ai/doc_metrics_handler_test.go`：端点 JSON `totals.published=2`、
  `rates.draftPublishRate≈0.6667`、`byPage[0].published=1`。

## 6. 边界与条件项

- **窗口级漏斗**：`draftPublishRate` 是「窗口内成文数 / 起草动作数」，不做逐篇归因——`doc_draft_created`
  发生在跳转前（无文章 ID），当前无跨页 durable 关联；已在报表与导出内显式注记，避免过度声称；
- **未覆盖路径**：从 AI 预填进入新建页但未提交（放弃成文）不计入分子——这正是转化率的语义（诚实计数）；
- 真实 PG 冒烟（jsonb 谓词 → COUNT、`date_trunc`、按页排序 + LIMIT 的列序）仍归联调（本批 sqlmock 三查询列序
  与聚合比率已被端点 + service 双层锁定；查询新增列与 `name IN` 已同步）；
- 匿名/关闭模式会话 id 缺失时的会话级比率「—」语义不受本批影响（新增比率仅依赖事件计数）。