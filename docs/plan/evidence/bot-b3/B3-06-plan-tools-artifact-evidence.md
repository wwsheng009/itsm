# B3-06 实施证据（计划类工具与 artifact）

> 文档类型：实施证据（任务 B3-06）
> Status: draft
> 编制日期：2026-09-27
> 任务：B3-06（`draft_ticket_fields`(plan) / `analyze_ci_impact_plan`(analysis) / `draft_kb_article`(draft) 落地，产出 `bot_artifacts`）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.4 B3-06、§5.2 AB3-06）
> 核查方式：Go 集成测试（ent/sqlite 真库 + 注册表执行面）+ 产物隔离断言

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B3-06 | **`integration_verified`** | 三件 plan/analysis/draft 工具注册并落地产物；归属/租户隔离与**零业务写入**由测试锁定 |
| AB3-06 | ✅ | UT + 集成（artifact 归属、租户隔离、零业务写入、证据引用、能力未注入时报错） |
| 预算/超时 | ✅ | 三件工具均标注 `TimeoutMs`/`MaxOutputBytes`/`RedactionProfile`/`Risk=plan`（B0-01 元数据单一来源） |

## 2. 交付物

| # | 文件 | 内容 |
| --- | --- | --- |
| 1 | `itsm-backend/ent/schema/bot_artifact.go`（新建） | `bot_artifacts` 表：`tenant_id`/`owner_user_id`/`conversation_id`/`run_id`/`kind`(plan\|analysis\|draft)/`tool_name`/`title`/`content_json`/`evidence_json`/`created_at`；四个租户前置索引 |
| 2 | `itsm-backend/service/bot/artifact.go`（新建） | `ArtifactStore`：`Create`（类型校验 + JSON 序列化 + 归属必填）/`Get`（租户+归属）/`List`（租户+归属+类型+倒序，limit 收敛） |
| 3 | `itsm-backend/service/tool_registry_plan.go`（新建） | 三件工具实现：`executeDraftTicketFields`（描述→字段草案，含保守优先级建议）/`executeCIImpactPlan`（**只读**本租户 CI + 检查清单 + 证据引用；CI 不可用即拒绝且不产生产物）/`executeDraftKBArticle`（草稿，**不发布**） |
| 4 | `itsm-backend/service/tool_registry.go` | 三件工具定义（`ReadOnly=true`、`Risk=plan`、资源 ticket/cmdb/knowledge）+ `SetArtifactStore` + 执行分支 + 调用者上下文（`WithToolActor`/`WithToolConversation`） |
| 5 | `itsm-backend/handlers/ai/service.go` | 只读执行路径注入发起人/会话（`service.WithToolActor`/`WithToolConversation`；`run_id` 由 B1-03 既有注入） |
| 6 | `itsm-backend/internal/bootstrap/app.go` | 装配 `toolRegistry.SetArtifactStore(bot.NewArtifactStore(client))` |

## 3. 行为契约

| 场景 | 行为 |
| --- | --- |
| 调用 `draft_ticket_fields{description}` | 返回 `{artifactId, kind:"plan", title, content, evidence}`；产物归属 = 发起人 + 会话 + 运行（有则记） |
| 调用 `analyze_ci_impact_plan{ciId}`（本租户 CI） | 返回 `kind:"analysis"`；`evidence_json` 含 CI 名称（证据引用） |
| 调用 `analyze_ci_impact_plan`（跨租户/不存在 CI） | 明确失败「配置项不可用」，**不产生**产物（fail-closed） |
| 调用 `draft_kb_article{title,content}` | 返回 `kind:"draft"`，`status:"draft"` 写入内容；**不创建知识文章实体** |
| 缺少发起人上下文（服务端未注入） | 明确失败「产物缺少发起人上下文」，不产生产物 |
| 未注入产物存储 | 明确失败「能力未启用」（不静默成功） |
| 读取产物 | 一律按「租户 + 归属」双条件；同租户他人不可见、跨租户不可见 |

## 4. 测试与运行记录

| # | 用例 | 覆盖 | 结果 |
| --- | --- | --- | --- |
| 1 | `service/bot/artifact_test.go::TestArtifactStore_CreateGetList` | 写入/读取/列表/类型过滤/证据缺省 | ✅ |
| 2 | `service/bot/artifact_test.go::TestArtifactStore_TenantAndOwnerIsolation` | 同租户他人 / 跨租户不可见 | ✅ |
| 3 | `service/bot/artifact_test.go::TestArtifactStore_Validation` | 归属必填 / 类型校验 / nil store | ✅ |
| 4 | `service/bot/artifact_test.go::TestArtifactStore_ListLimitClamp` | limit 收敛 + 表级形态断言 | ✅ |
| 5 | `service/tool_registry_plan_test.go::TestPlanTools_ArtifactsAndZeroBusinessWrites` | 三件工具产物 + 归属 + 跨租户拒绝 + **零业务写入** + 缺上下文拒绝 + 未注入报错 | ✅ |

## 5. 遗留与登记

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | 产物读取端点（`GET /ai/artifacts`）与工作区证据面板消费 | **B4-01**（API 契约 + 前端展示）；本任务只落存储与产出 |
| 2 | `create_kb_draft`（真正创建知识草稿实体，act_low 写工具） | **B3-05**（依赖本任务产物：草稿先落产物，再由写工具创建实体）；当前 S3 仅用 `draft_kb_article` |
| 3 | 影响分析的**图遍历**深化（复用 `get_ci_impact` 的多跳结果） | B4 增强候选；当前分析草案含检查清单 + 证据引用，未做多跳合成 |
| 4 | `bot_artifacts` 保留策略（与 runs/steps 180 天对齐） | 运维归档任务（BQ4 口径）；表已就绪 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B3-06 交付：`bot_artifacts` 表 + `ArtifactStore` + 三件 plan/analysis/draft 工具（只读业务库、产物归属/隔离、证据引用）；测试 5 组全绿；判定 `integration_verified`（读取端点与图遍历深化归 B4） |
