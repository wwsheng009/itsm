# DEMO 验证证据：AI 助手常驻组件与 AI 驱动业务系统全量交付（dock-plan，2026-10-18）

- 版本：`1.8.0-dev.d51ed172`；实现提交 `d51ed172d12b7940c25bca05b44e3ac4061e81f8`（`feat(ai): AI 助手常驻组件与 AI 驱动业务系统全量交付（ai-assistant-dock-plan-2026-10-18）`，391 files / 51933 insertions）
- 演示环境：172.18.3.238，容器 `itsm-backend-demo` / `itsm-worker-demo` / `itsm-frontend-demo`（本批重建）+ `itsm-ai-service-demo`（本批无 sidecar 代码变更，沿用 F2 `c05cc49e`）
- 前置：F2（`1.8.0-dev.c05cc49e`）已部署；本批是 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` 的**全量交付首登**（此前一直处于未提交工作区）
- bundle：`itsm-dock-d51ed172.bundle`（907522 B，SHA256 `50AA617BFF1C3B319A2BC42789F4DDD842BB33005EBC74BE441EBCC48CB4FFA3`；`refs/heads/feat/msp-p0-hardening` → `d51ed172`，前置 `c05cc49e` 满足）
- 范围：后端（Dock/上下文协议/建议排序、遥测接收与聚合、帮助空间与帮助待办、运行轨迹/巡检、分析增强 O2）+ 前端（AI Dock / 建议卡 / 帮助覆盖页 / 观测页 / i18n）+ 迁移（20261018 批）+ 文档与 CI

## 1. 部署（238 demo，8089）

三步脚本 + 运行时验证 + 终态采集：`deploy238_dock_a.sh`（fetch/checkout + `.env.demo` VERSION）→ `_b.sh`（prod compose 上下文构建 backend+frontend 双镜像，后台轮询）→ `_c.sh`（up -d + init + 健康 + A/B 指纹）→ `_verify.sh`（运行时验证）→ `_evidence.sh`（终态采集）；均上传 `/tmp` 执行（CRLF 已清）。

| 检查 | 结果 |
|---|---|
| 部署前远端 HEAD | `c05cc49e`（F2，分支 `deploy-demo-f2-c05cc49e`），demo ledger=**96** |
| A 步（fetch/checkout） | ✅ 分支 `deploy-demo-dock-d51ed172`，HEAD=`d51ed172d12b…`；`.env.demo VERSION=1.8.0-dev.d51ed172` |
| B 步（构建） | ✅ `BUILD_EXIT=0`；`Image itsm-backend:1.8.0-dev.d51ed172 Built` + `Image itsm-frontend:1.8.0-dev.d51ed172 Built` |
| C 步（up -d + init） | ✅ `up -d itsm-init-demo itsm-backend-demo itsm-worker-demo itsm-frontend-demo`；`init_wait_exit=0`；ledger **96→103（+7）** |
| 容器终态 | backend/worker/frontend 均 `Up (healthy)` 且镜像 tag=`d51ed172`；`itsm-init-demo` `exited\|exit=0`；nginx/redis/ai-service 保持运行 |
| `GET /` / `GET /api/v1/version` | ✅ `root=200`；`{"build":"release","version":"1.8.0-dev.d51ed172"}` |
| A/B 指纹（镜像内 grep） | backend `assistant/context` **0→2**；frontend `assistant/context` **0→1**、`assistant-dock` **0→1**；backend `ai_telemetry_events` **0→15** |

本批新增应用迁移 7 条（`applied_at` = 2026-10-19 02:39 UTC）：`20261018_add_ai_help_coverage_menu`、`20261018_add_ai_observability_menu`、`20261018_ai_telemetry_events`、`20261018_ai_telemetry_events_filters`、`20261018_ai_telemetry_events_retention`、`20261018m_msp_help_space`、`20261018n_ai_doc_todo_states`。批内另有 2 条此前已应用：`20261018_project_management_p1`（2026-10-18 00:05）、`20261018_add_llm_provider_endpoint_full_path`（2026-10-18 05:02）。

部署期间修正 2 处脚本问题（已回写脚本）：① C 步前端指纹路径 `/usr/share/nginx/html/assets` → `/usr/share/nginx/html`（Vite 产物 `assetsDir=static`，assets 目录不存在）；② V3 遥测写入补 CSRF token（`GET /api/v1/csrf-token` → `X-CSRF-Token` 头）。

## 2. 运行时验证（`deploy238_dock_verify.sh`，全绿）

| # | 检查 | 结果 |
|---|---|---|
| V1 | `admin` 登录 | ✅ cookie_lines=6 |
| V2 | 5 个新端点冒烟（GET，期望 200） | ✅ 全 200：`/ai/assistant/context?pageKey=tickets.list`（`known=false` 属设计内降级）、`/ai/help/search?q=工单&limit=3`（命中 `docs/help/tickets/create.md`）、`/ai/help/coverage`（coverage=1）、`/ai/help/todos`（`states.available=true`、`telemetry.available=true`）、`/ai/telemetry/summary?days=7` |
| V3 | 遥测写入 | ✅ `csrf_len=24`；POST `/ai/telemetry/events` → `{"accepted":2,"dropped":1}`（1 个未知事件按设计丢弃） |
| V4 | 遥测落库（psql） | ✅ `page_key='verify.dock'` = **2**（`help_opened`=1、`assistant_opened`=1） |
| V4b | summary 聚合反映 | ✅ `totalEvents=2`、`byName=[assistant_opened=1, help_opened=1]`、`byPage=[verify.dock=2]` |
| V5 | 新表存在性 | ✅ `ai_telemetry_events` / `ai_doc_todo_states` / `msp_help_docs` / `msp_help_distributions` |
| V6 | 日志扫描（backend/worker 近 10m） | ✅ 无 panic / fatal / error |

## 3. 终态采集（`deploy238_dock_evidence.sh`，E1-E7，2026-10-19 02:45 UTC 前后复核）

E1 分支/HEAD：

```
branch=deploy-demo-dock-d51ed172
head=d51ed172d12b7940c25bca05b44e3ac4061e81f8
```

E3 demo 容器（采集时刻）：

```
itsm-ai-service-demo|itsm-ai-service:1.8.0-dev.c05cc49e|Up 11 hours (healthy)
itsm-backend-demo|itsm-backend:1.8.0-dev.d51ed172|Up 8 minutes (healthy)
itsm-frontend-demo|itsm-frontend:1.8.0-dev.d51ed172|Up 8 minutes (healthy)
itsm-nginx-demo|nginx:alpine|Up 5 days (healthy)
itsm-redis-demo|redis:7-alpine|Up 5 days (healthy)
itsm-worker-demo|itsm-backend:1.8.0-dev.d51ed172|Up 8 minutes (healthy)
init_demo=exited|exit=0
```

E4 版本/HTTP：`{"code":0,"message":"success","data":{"build":"release","version":"1.8.0-dev.d51ed172"}}`，`root=200`。

E5 ledger：`ledger_demo=103`；20261018 批次 9 条（7 条本批 + 2 条此前），详见 §1。

E6 本批迁移文件（`itsm-backend/migrations/`，含 `_down`）：

```
20261018_add_ai_help_coverage_menu.sql
20261018_add_ai_observability_menu.sql
20261018_add_llm_provider_endpoint_full_path_down.sql
20261018_add_llm_provider_endpoint_full_path.sql
20261018_ai_telemetry_events_filters.sql
20261018_ai_telemetry_events_retention.sql
20261018_ai_telemetry_events.sql
20261018m_msp_help_space.sql
20261018n_ai_doc_todo_states.sql
20261018_project_management_p1_down.sql
20261018_project_management_p1.sql
```

E7 A/B 指纹（镜像内 grep，以 `c05cc49e` 为旧基准）：

```
old-backend=0        new-backend=2        （assistant/context）
old-frontend=0       new-frontend=1       （assistant/context，路径 /usr/share/nginx/html）
old-frontend-dock=0  new-frontend-dock=1  （assistant-dock）
old-backend-tele=0   new-backend-tele=15  （ai_telemetry_events）
```

## 4. 结论与遗留

结论：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` 全量交付已在 238 demo（8089）上线——新端点（assistant/context、help/search、help/coverage、help/todos、telemetry/summary）全部可用；遥测链路（写入 → 落库 → 聚合）端到端验证通过；20261018 批迁移全部应用、4 张新表就位；backend/worker/frontend 三容器以新镜像健康运行、version 对齐；A/B 指纹确认新代码确实进入镜像。

遗留与跟进：

1. **浏览器级 UI 走查**未纳入本批部署证据（以镜像指纹 + 端点冒烟 + 任务级证据覆盖；dock 全交互走查可选补强）。
2. **ai-service 未重建**：本批无 sidecar 代码变更（F2 `c05cc49e` 镜像继续服役）；如后续动 sidecar 需按 F2 口径重建。
3. **移动端抽屉 / 真实 Provider E2E** 等方案既定条件项（plan §7 V1-02 等）归联调，不在本批范围。
4. **回滚**：`.env.demo` 改回 `VERSION=1.8.0-dev.c05cc49e` + `up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo`（旧镜像仍在）。
