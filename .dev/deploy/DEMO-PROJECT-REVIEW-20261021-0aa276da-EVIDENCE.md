# DEMO 部署证据：项目管理功能审查修复（0aa276da，2026-10-21）

- 版本：`1.8.0-dev.0aa276da`；实现提交 `0aa276dad44fc6062f70499c18e4f37f8dd9e795`
  （`fix(project): 按 2026-10-21 功能审查报告修复 21 项缺陷`，35 files / +2007 −263）
- 演示环境：`172.18.3.238`，`docker-compose.demo.yml`（端口 8089，库 `itsm_demo`）
- 远端仓库：`/home/n9e/itsm/repo`，部署分支 `deploy-demo-project-review-0aa276da`，HEAD=`0aa276da`
- 前置版本：`1.8.0-dev.3cfc1dcc`（backend/worker/frontend）
- bundle：`.dev/deploy/itsm-project-review-0aa276da.bundle`（53,088 B，`3cfc1dcc..0aa276da`；`git bundle verify` okay）
- 迁移：本批无新增迁移，demo ledger 保持 **105**

## 1. 部署（A → B → C）

| 步骤 | 结果 |
|---|---|
| A. fetch/checkout | ✅ 分支 `deploy-demo-project-review-0aa276da`，`HEAD=0aa276da`；`.env.demo VERSION=1.8.0-dev.0aa276da`；新文件（business_time.go / display_service.go / review 测试与台账）全部落树 |
| B. 构建 | ✅ `BUILD_EXIT=0`；`Image itsm-backend:1.8.0-dev.0aa276da Built` + `Image itsm-frontend:1.8.0-dev.0aa276da Built`（磁盘 55G 可用） |
| C. up -d + init | ✅ init `exited/exit=0`；ledger 105→105（无新迁移）；seed 幂等（templates_created=0 / grants_skipped=39） |
| 容器终态 | backend/worker/frontend 均 `Up (healthy)` 且镜像 tag=`0aa276da`；nginx/redis/ai-service 保持运行 |
| 版本 | `GET /api/v1/version` → `1.8.0-dev.0aa276da`；`/` 200 |

A/B 指纹（镜像内 grep）：

| 指纹 | 旧 `3cfc1dcc` | 新 `0aa276da` |
|---|---|---|
| backend `healthDataQuality`（`/app/main`） | 0 | **1** |
| frontend `结项待补` | 0 | **1** |
| frontend `数据不足` | — | **1** |
| frontend `自动放行（未配置流程）` | — | **1** |

## 2. 运行时验证（`deploy238_project_review_verify.sh` + `_verify2.sh`）

| # | 检查 | 结果 |
|---|---|---|
| V1 | 版本 / admin 登录 / CSRF | `1.8.0-dev.0aa276da`；登录成功（cookie 6 行）；csrf_len=24 |
| V2 | **A1 严格必填**：仅 `name` 创建 | **400** `项目必填校验未通过：… managerId is required` |
| V3 | **A2/B3 名称富化**：列表 5 行 | 均有 `managerName=系统管理员`；项目 6 有 `departmentName=信息技术部`；无裸 ID |
| V4 | **A4 门审批方式** | 预检：initiation/closure=`manual`，plan/acceptance=`auto`（`configMissing=true`）；已通过门逐条 `approvalMode/autoApproved` 正确 |
| V5 | **A8 阻塞口径** | 项目 6 `status=completed`、`blockerTotal=0`、`blockers_len=0`（三处同源） |
| V6 | **B12 一次通过率** | `passed=5`、`firstPass=5`、**`autoApproved=6`**、`rate=1`、`detailsTotal=11`；明细自动放行 6 行、一次通过 5 行（分子与明细勾稽） |
| V7 | **A6 业务时区** | 报告正文「生成时间：2026-10-21 08:41」（UTC 传输 00:41Z → 本地 08:41）；「项目经理：系统管理员」「所属部门：信息技术部」 |
| W1 | **A2/A6 新建项目** | 200；`code=`**`PRJ-20261021-9992`**（业务时区日期）；`departmentId=1 → departmentName=信息技术部` |
| W2 | A1 再次拦截 | 400（同一错误语义） |
| W3 | 页面可达 | `/projects` `/projects/6` `/projects/templates` `/projects/reports` `/projects/portfolio` 全部 200 |
| W4 | 验证数据清理 | 软删项目 7 → 200 `{deleted:true}`；列表 `total=5`（无残留） |
| V8 | 日志扫描（近 10m） | backend/worker 无 panic / fatal / error |

## 3. 终态采集

```
branch=deploy-demo-project-review-0aa276da
head=0aa276dad44fc6062f70499c18e4f37f8dd9e795
VERSION=1.8.0-dev.0aa276da

itsm-ai-service-demo|itsm-ai-service:1.8.0-dev.5b070145|Up 24 hours (healthy)
itsm-backend-demo|itsm-backend:1.8.0-dev.0aa276da|Up 3 minutes (healthy)
itsm-frontend-demo|itsm-frontend:1.8.0-dev.0aa276da|Up 2 minutes (healthy)
itsm-nginx-demo|nginx:alpine|Up 7 days (healthy)
itsm-redis-demo|redis:7-alpine|Up 7 days (healthy)
itsm-worker-demo|itsm-backend:1.8.0-dev.0aa276da|Up 3 minutes (healthy)

init_demo=exited|exit=0
ledger_demo=105
version=1.8.0-dev.0aa276da
```

项目域抽样（存量为旧数据，经理/部门为空的项目在列表中会显示「未设置」+ 健康度「数据不足」）：

```
1|PRJ-20261018-4563|0|0|planning
3|PRJ-20261018-5583|1|0|planning
5|PRJ-20261018-9577|1|4|planning
6|PRJ-20261020-9637|1|1|completed
```

门审批留痕抽样：

```
3|plan|passed|auto_approved_no_flow
4|plan|passed|auto_approved_no_flow
6|acceptance|passed|auto_approved_no_flow
6|initiation|passed|-          ← 人工审批（无自动放行留痕）
```

> 与 V4/V6 一致：自动放行门被单独识别并从一次通过率剔除。

## 4. 部署脚本

- `.dev/deploy/deploy238_project_review_a.sh`（fetch/checkout + VERSION + 落树核对）
- `.dev/deploy/deploy238_project_review_b.sh`（镜像构建，后台日志）
- `.dev/deploy/deploy238_project_review_c.sh`（up -d + 健康 + A/B 指纹）
- `.dev/deploy/deploy238_project_review_verify.sh`（名单/门/结项/指标/报告）
- `.dev/deploy/deploy238_project_review_verify2.sh`（新建项目 + 页面可达 + 清理）
- `.dev/deploy/deploy238_project_review_evidence.sh`（终态采集）

原始输出：`.dev/deploy/project-review-evidence-0aa276da.log`

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
git checkout -f deploy-demo-ai-outcome-3cfc1dcc
sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.3cfc1dcc/' .env.demo
echo 'passw0rd' | sudo -S -p '' env VERSION=1.8.0-dev.3cfc1dcc \
  docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-backend-demo itsm-worker-demo itsm-frontend-demo
```

旧镜像 `itsm-backend:1.8.0-dev.3cfc1dcc` / `itsm-frontend:1.8.0-dev.3cfc1dcc` 仍在本地，无需重建。
