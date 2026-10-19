# P2 · MSP 多租户帮助空间与分发授权（§P2 / Q2）· 交付证据

- 日期：2026-10-18
- 任务：P2「智能化与运营」第 4 项：**MSP 多租户帮助空间与分发授权**（provider 帮助库 → 客户 copy-in 分发 → 客户侧助手消费）
- 状态：`integration_verified`（后端管理面 + 授权链 + 客户侧消费 + 前端标注；真实 PG 冒烟与 provider 管理 UI 归联调/迭代）
- 交付物：
  - **数据面**：`migrations/20261018m_msp_help_space.sql`（`msp_help_docs` provider 帮助库 + `msp_help_distributions` 客户侧 copy-in 追踪；
    active 行唯一 `(tenant_id, source_slug) WHERE status='active'` 部分唯一索引 + 页面检索索引）；
    `database/rls/migrations/016_msp_help_policies.sql` + rollback（两表 ENABLE RLS + `tenant_id = get_current_tenant_id()` fail-closed，只 ENABLE 不 FORCE，与 003–015 同范式）
  - **服务层**：`service/msp_help_space.go`（`MSPHelpSpaceService`）
    - provider 帮助库：`ListDocs / CreateDoc / UpdateDoc / PublishDoc / DeleteDoc`（draft→publish 门禁；**仅 published 可分发**；
      已发布文档编辑回 draft 并推进版本，需重新发布才可再分发）；
    - 分发授权：`Distribute / ListDistributions / Upgrade / Unbind`（copy-in 快照 + 来源追踪（source_doc_id/slug/version/updated_at）；
      升级提示"提示不强制"，显式 `upgrade` 原位刷新到最新已发布版本；`unbind` 转客户原生副本（保留可见，停止追踪）；
      授权唯一入口 = **mspguard（allocation + 客户 active）**；写入按客户租户重绑定 ctx（tenantctx，RLS 同源，零跨租户读））；
    - 客户侧消费：`CustomerHelpDocs / SearchCustomerHelpDocs`（本租户 active/unbound 行；检索 ILIKE + 片段截取）；
    - 审计：`msp_help_doc.{distribute,upgrade,unbind}`（best-effort，`source=workbench` + `target_tenant_id`）。
  - **DTO**：`dto/msp_help_space_dto.go`
  - **HTTP 层**：`handlers/msp/help_space.go`（9 端点）+ `handler.go` setter 装配（不破坏既有 9 参构造签名）+ `router/msp_routes.go` 路由
  - **助手消费**：`handlers/ai/help.go` 在 `/ai/help/page` 返回 `distributedDocs`、`/ai/help/search` 返回 `distributedResults`
    （来源标注 `source=msp_provider` + providerTenantId/sourceSlug/sourceVersion/status；查询失败/未装配 → 空数组，**不阻塞帮助视图**）；
    `handlers/ai/service.go` 注入 `SetMSPHelpSpace` + 容错包装
  - **装配**：`internal/bootstrap/app.go`（同一 `MSPHelpSpaceService` 实例注入 AI 助手与 MSP handler；`database.GetRawDB()` + `MSPAccessValidator`）
  - **前端**：`ai-api.ts` 类型（`HelpDistributedDoc`/`HelpDistributedSearchResult` + 响应字段）；`AssistantHelpPanel` 服务商分发文档区块
    （「服务商分发」标签；unbound 显示「已解绑·原生副本」）+ 分发检索结果分区 + 反馈区在"仅分发文档"时也可见；
    文档数埋点 `docCount` 计入分发数（`help_opened` / `doc_feedback` 口径一致）

## 1. 授权与语义口径（实现即契约）

| 面 | 授权链 | 语义 |
|---|---|---|
| provider 帮助库 `/msp/help/docs*` | `RequireMSPPermission("msp", read/write)`（msp_manager 持 write） | 自有资产；draft→publish；编辑已发布 → 回 draft + 版本 +1 |
| 分发 `/msp/customers/:id/help/**` | `RequireMSPPermission("msp_customer", read/write)` **∩** 服务端 mspguard（allocation + 客户 active） | copy-in 快照；active 唯一；重复分发 → 409；未发布 → 409 |
| 升级 / 解绑 | 同上（write） | 升级=显式刷新到 provider 最新已发布版本（同版本 → 409）；解绑=转客户原生（unbound 保留可见） |
| 客户侧消费（助手帮助） | 普通租户行（RLS `tenant_id = ctx`，无 MSP 例外通道） | active 跟随 provider + unbound 原生副本；查询失败降级为空 |

## 2. 验证证据（可复跑）

```powershell
# 后端
cd itsm-backend
go run ./cmd/authz-gen                                              # RBAC 预检再生
go test ./service/ -run "MSPHelpSpace|HelpMatchSnippet" -count=1    # 5 例全过（生命周期/分发/升级提示/升级解绑/客户消费+片段）
go test ./handlers/msp/ -run "HelpSpace" -count=1                   # 3 例全过（端点透传/错误映射/fail-closed）
go test ./handlers/ai/ -run "Distributed|Merges" -count=1           # 4 例全过（合并/检索/未装配降级/查询失败降级）
go test -p 2 ./service/ ./handlers/ai/ ./handlers/msp/ ./router/ ./middleware/ ./internal/bootstrap/ ./dto/ -count=1
#   → 7 包全绿（service 65.1s；bootstrap 含装配路径编译+自检）

# 前端
cd itsm-frontend
npx jest src/components/ai/dock/__tests__/AssistantHelpPanel.test.tsx --coverage=false --watchAll=false --runInBand
#   → 14 pass（新增：分发文档标注/unbound 标签/仅分发可反馈/分发检索分区）
npx tsc --noEmit && npx eslint src/lib/api/ai-api.ts src/components/ai/dock/AssistantHelpPanel.tsx
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils --coverage=false --watchAll=false --runInBand
#   → 57 suites / 489 tests 全绿
```

## 3. 关键断言

- **授权回退面**：非 MSP 上下文 → 403；服务未装配 → 503；客户无 allocation → mspguard 错误（403/404 语义，经 `failMSPAccess` 归因链）；
  handler 契约测试覆盖 404/409/400/403/503 全映射。
- **copy-in 与一致性**：分发落快照（title/pageKey/bodyMd + 来源版本/时间）；`active` 部分唯一索引防双份；未发布不可分发；
  升级仅在 provider 有更新版本时成功，且仅作用于 active 行。
- **零跨租户读**：provider 侧读写均以 `tenantctx.WithTenantID` 显式绑租户（provider 或 customer 作用域）；
  客户侧只读本租户行；RLS 策略为单一 `tenant_id = ctx`（无绕过通道）。
- **降级不阻塞帮助**：助手端分发查询失败 → `distributedDocs=[]`（200），内嵌帮助照常（单测覆盖 `db down` 场景）。

## 4. 条件项（不阻塞）

- **真实 PG 冒烟未执行**：本机 Docker daemon 未运行（`docker images` 连接失败），迁移 DDL（IDENTITY/部分唯一索引）与 RLS 策略仅经 sqlmock 契约验证。
  联调执行：`go run -tags rlsapply ./cmd/rls-apply -files 016_msp_help_policies.sql` + `-verify`；`make migrate` 侧执行 `20261018m_msp_help_space.sql`。
- **provider 管理 UI 未交付**：与 P2-①（模板分发）/ D-TT-1（工单类型下发）先例一致——先 API + 授权链 + 审计，MSP 工作台管理界面归后续迭代；
  客户侧消费（帮助面板）本批已交付。
- **来源维度未分离**：`help_opened`/`doc_feedback` 目前不区分内嵌/分发来源（报表按租户汇总）；如需按来源拆分统计，后续在事件 `extra` 增加 `source`。
- **unbound 副本只读**：解绑后副本转为客户原生但无编辑入口（帮助文档展示面只读）；客户自建帮助文档不在本批范围。
- 审计写入依赖 ent client（best-effort；失败仅日志，不影响主流程）。