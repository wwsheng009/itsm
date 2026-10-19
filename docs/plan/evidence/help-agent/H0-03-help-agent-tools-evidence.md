# H0-03 Help Agent 只读工具与帮助数据面 · 交付证据

- 日期：2026-10-18
- 任务：H0-03（方案 §5.4 工具集 / §13.7 数据面；P0）
- 状态：`unit_verified`（面板帮助视图渲染归 H0-04；LLM 对话内调用链路沿用既有工具循环）
- 交付物：
  - 生成器扩展：`itsm-frontend/scripts/gen-page-manifest.mjs` 追加输出后端内嵌副本
    （`itsm-backend/internal/helpindex/{help-index,page-manifest,doc-ledger}.json`）
  - 新包 `itsm-backend/internal/helpindex/`（go:embed 索引 + 检索/页面状态/gaps/覆盖率）
  - REST：`handlers/ai/help.go`（`GET /ai/help/search`、`GET /ai/help/page`）+ 路由 + RBAC 预检重生
  - 工具：`service/tool_registry_help.go`（4 个只读工具）+ 注册表接线 + 风险矩阵 21 项
  - 场景授权：`service/bot/scenarios.go` S3 授予 4 个 help 读工具
  - 测试：`internal/helpindex/index_test.go`（6 例）、`handlers/ai/help_handler_test.go`（5 例）、
    `service/tool_registry_help_test.go`（2 例）

## 1. 数据面与口径

| 项 | 口径 |
|---|---|
| 数据源 | `docs/help/**` 真源 → 生成器 → 三份 JSON（go:embed，零文件系统依赖） |
| 检索 | 标题/页面键命中权重 3、正文 1；整词未命中且为多字 CJK 词时 2-gram 回退（上限 4，**ASCII 词不回退**避免误命中）；limit 缺省 5、上限 20 |
| 页面帮助 | `status: ok/missing/stale/unknown` + 关联文档正文；未知页面正常返回 `unknown`（不 404，面板降级） |
| 工具 | `help_search` / `help_get_page` / `help_read_doc` / `help_list_gaps`：全部 `ToolRiskRead`、`ReadOnly=true`、无 DB 依赖、无需确认 |
| 失败语义 | 参数缺失/文档不存在 → 模型可读错误；空查询 → 空结果不报错 |

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
node scripts/gen-page-manifest.mjs --check            # exit 0（同时刷新后端三份内嵌副本）

cd ../itsm-backend
go test ./internal/helpindex/ -count=1                # ok（6 例：检索/过滤/页面/阅读/gaps 一致性）
go test ./handlers/ai/ -run 'Help' -count=1           # ok（5 例：租户缺失 401/空词 400/命中/过滤器/页面）
go test ./service/ -run 'BuiltinToolMetadata|HelpTools|NormalizeToolMetadata|WriteTools' -count=1  # ok
go test ./service/bot/ -count=1                       # ok（S3 授权变更后场景回归）
go test ./handlers/ai/ -count=1                       # ok（全包回归，含黑名单/授权矩阵）
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1  # ok（RBAC 预检已重生）
```

关键断言：
- 检索：`SLA` 命中 `sla/monitor.md`；`工单详情` 标题权重最高居首；`影响分析计划`（无空格中文）经 2-gram 命中；
  `zzz-nonexistent-xyz`（ASCII）零命中；
- 页面：`ticket.detail` → status=ok + 正文包含"分诊建议"；未知 pageKey → status=unknown + docs 空；
- 工具执行：四个工具经 `ToolRegistry.Execute` 走通（help_read_doc 读取 ai/chat.md 标题/正文）；
- 风险矩阵守卫：内置工具 17 → **21**（新工具均 read、category=help），`TestBuiltinToolMetadataComplete` 通过。

## 3. 观察记录（与本批无关，未处理）

`go test ./service/ -count=1` 全量时 `TestBiz_*`（BPMN 业务流）出现
`UNIQUE constraint failed: process_instances.process_instance_id` / `process_tasks.task_id`：
- 单测单独运行通过（`-run TestBiz_ListProcessInstancesByTenant` ok）；
- 批量运行时多用例共享 SQLite 状态、基于时间的实例/任务 ID 冲突；
- 相关文件（`service/bpmn_*`、`bpmn_business_flow_test.go`）**本批未改动** → 既有测试隔离问题，
  建议后续单独治理（为每个用例子库或注入固定时钟）；本批改动面的定向用例均通过。

## 4. 边界与遗留

- 对话内工具调用链（LLM 触发 help_*）沿用既有工具循环，未做真实 provider 的 E2E（归联调）；
- 写面工具（`help_submit_review`/`help_publish`）与 KB 草稿通道归 H1；
- 检索为词法匹配（无向量）；语义检索可选复用 `/ai/rag/search`（P2 分析增强时评估）。