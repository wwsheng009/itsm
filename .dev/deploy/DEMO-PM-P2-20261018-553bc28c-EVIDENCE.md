# 238 demo 全栈部署证据：项目管理模块 P0–P2（2026-10-18，1.8.0-dev.553bc28c）

## 变更内容

- 范围：`586bb07a..553bc28c` 共 4 提交（数据层/后端/前端/文档；项目管理 PE-1~PE-26，含 PE-21~PE-26 P2 全部切片）。
- 交付镜像：
  - `itsm-backend:1.8.0-dev.553bc28c`（sha256:970bf6a7…）
  - `itsm-frontend:1.8.0-dev.553bc28c`（sha256:77447583…）
  - `itsm-ai-service` retag 对齐（代码未变，原 tag `22ee3603`）。
- 迁移：demo 侧 `itsm-init-demo` 随依赖重跑（ent 自动迁移创建 P2 表/列）；RLS `015` 经 psql 幂等重放（新增 `project_asset_links` 覆盖）。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-pm-p2-full.bundle`（基线=远端 586bb07a，795,523B） | fetch/checkout OK（HEAD=553bc28c，分支 deploy-demo-pm-p2） |
| A | `deploy238_pm_p2_a.sh`（n9e）：checkout + `.env.demo` VERSION | `A-STATUS: DONE`（VERSION=1.8.0-dev.553bc28c） |
| A2 | `deploy238_pm_p2_a2.sh`（sudo）：磁盘清理 + retag ai + 后台顺序构建 | `retag-ai-ok`、`A2-STATUS: DONE` |
| W | 构建轮询（backend → frontend） | `BUILD_BACKEND_EXIT=0`、`BUILD_FRONTEND_EXIT=0`、`BUILD_ALL_DONE` |
| C | `deploy238_pm_p2_c.sh`：`up -d backend/worker/frontend`（init 随依赖跑迁移） | 三容器切 553bc28c + healthy；`root=200 / health=200`；API version=1.8.0-dev.553bc28c |
| D | `deploy238_pm_p2_d.sh`：结构/RLS 验证 | `D-STATUS: DONE`（见下） |
| AB | `deploy238_pm_p2_ab.sh`：镜像 A/B 特征对比 | `AB-STATUS: DONE`（见下） |

执行机：本 Windows 工作站（`python .dev/deploy/remote.py [--sudo]`）；远端：`n9e@172.18.3.238:/home/n9e/itsm/repo`。

## 验证证据（238 侧）

- **结构（D1/D2/D3）**：`project*` 表 13 张在册（新增 `project_asset_links`/`project_risks`/`project_gates`/`project_closures`/`project_members`/`project_tasks`/`project_task_dependencies`/`project_milestones`/`project_baselines`/`project_templates`/`project_report_templates`）；关键列 `projects.budget_amount|budget_currency|default_weekly_capacity_hours|owner_team_id|template_id`、`project_gates.reject_count`、`project_members.weekly_capacity_hours`、`project_tasks.estimate_hours|actual_hours`、`survey_responses.project_id` 全部在位；PE-26 partial unique index `uq_project_asset_links_active_target ... WHERE deleted_at IS NULL` 就位。
- **RLS（D4/D5）**：015 幂等重放成功（`project_asset_links` 策略 `tenant_isolation_project_asset_links` 新建；`project_risks` 策略在位）。
- **A/B 特征（AB）**：前端资产 token `采购/资产登记明细` 旧镜像=**0** / 新镜像=**1**；后端二进制 token `asset-link-candidates` 旧=**0** / 新=**2**；容器映像：`itsm-backend-demo|itsm-worker-demo|itsm-frontend-demo` = `1.8.0-dev.553bc28c`（healthy），`itsm-ai-service-demo` = `22ee3603`（retag 到 553bc28c 已存在）。
- **API 版本**：`GET /api/v1/version` → `1.8.0-dev.553bc28c`。

## 浏览器实测（Tabbit，demo `http://172.18.3.238:8089/`）

任务：`ITSM PM P2 demo 验证`（tabbit-cli，Windows launcher）。登录账号：demo `admin`（super_admin）。

| 场景 | 结果 |
|---|---|
| 列表页 | 工具栏 `模板管理 / 报表中心 / 组合视图 / 新建项目` 可见（新 UI） |
| 新建项目（UI 表单） | 创建成功 `PM P2 验收 084270` → `/projects/1`；概览显示 **预算 120,000 CNY** |
| 工作台 12 Tab | 概览 / 阶段门 (0/4) / 结项清单 (2/6) / 任务 / 里程碑 / 计划基线 / 关联单据 / 成员 / 状态报告 / 风险/问题 / 资源负荷 / AI 助手 —— 全部可切换，无错误面板 |
| PE-26 采购·资产登记 | 结项清单内「采购/资产登记明细」卡在位；「添加登记」弹窗文案正确；候选下拉命中 `AST-P2-DEMO-001 · P2 验收资产 A / available`（最小字段）；登记成功后卡片出现该行（类型/台账目标/登记时间/删除）✅ |
| PE-21 风险 | 「风险/问题」Tab → 新建风险 → 列表命中 `P2 验收风险 525908` ✅ |
| PE-23 资源负荷 | Tab 渲染，含「自然日」口径标注与计划/剩余/容量列 ✅ |
| PE-24 组合视图 | `/projects/portfolio` 渲染度量卡片（按时交付率/资源利用率）与项目行 ✅ |
| PE-25 AI 摘要 | 生成摘要 → 200 降级：标签「事实派生」+ 提示「AI 调用失败，已降级为确定性输出」+ 事实派生 Markdown（项目/任务/里程碑/风险信号计数）✅ |
| PE-25 风险预警 | 生成预警 → 200 降级：标签「规则信号」、「AI 判断不可用，以下为规则信号（不影响主流程）」✅ |

截图产物（Tabbit artifacts，任务 id `task-51f710db-46e5-4a01-b7c2-1380dd19b946`）：结项清单登记后、AI 助手页等 3 张 PNG；另经 API 直连复跑 AI 三端点（`ai/summary` / `ai/risk-warnings` / `ai/minutes/tasks`）均 200 降级。

## 观察项（非阻断）

1. **demo AI 端点端到端 ~60.4s**：`ai/summary` 60,480ms、`ai/risk-warnings` 60,402ms、`ai/minutes/tasks` 60,404ms（LLM provider 调用超时后降级；返回语义正确 200 + `degraded=true`）。UI 在等待期间保持 loading；如需演示体验可缩短 demo 侧 LLM 超时或置空 provider。
2. **demo nginx `/assets*` 被 Grafana 规则截获**：`GET /assets` → 301 `http://wan1.e2yun.com:17000/login?redirect=%2Fgrafana%2Fassets%2F`，`GET /assets/new` → 404；即 demo 环境无法经 UI 打开 ITSM「资产管理」页（既有 nginx/Grafana 配置遗留，与本批改动无关）。本次 PE-26 登记的资产改由后端 API 预置（`POST /api/v1/assets`，id=1 / `AST-P2-DEMO-001`）。
3. `itsm-ai-service-demo` 容器 tag 仍显示 `22ee3603`（镜像已 retag 到 `553bc28c`；代码同源未重建）。

## 复跑方式

```bash
# 远端脚本（已上传 /tmp，可重复执行）
NEW=1.8.0-dev.553bc28c bash /tmp/deploy238_pm_p2_a.sh          # n9e
NEW=1.8.0-dev.553bc28c bash /tmp/deploy238_pm_p2_a2.sh         # sudo
bash /tmp/deploy238_pm_p2_w.sh                                  # sudo（轮询）
NEW=1.8.0-dev.553bc28c bash /tmp/deploy238_pm_p2_c.sh          # sudo
bash /tmp/deploy238_pm_p2_d.sh                                  # sudo
bash /tmp/deploy238_pm_p2_ab.sh                                 # sudo
```