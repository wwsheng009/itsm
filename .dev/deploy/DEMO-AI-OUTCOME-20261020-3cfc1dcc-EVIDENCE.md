# DEMO 部署证据：AI 结果落点可跳转可验证（3cfc1dcc，2026-10-20）

- 版本：`1.8.0-dev.3cfc1dcc`；实现提交 `3cfc1dccba549b451ab9499177e6faceabdc0332`（`fix(ai): 结果落点可跳转可验证——产物详情/引用锚点/证据空态`，29 files / +1459 −97）
- 演示环境：172.18.3.238，`docker-compose.demo.yml`（端口 8089）
- 远端仓库：`/home/n9e/itsm/repo`，部署分支 `deploy-demo-ai-outcome-3cfc1dcc`，HEAD=`3cfc1dcc`
- 前置版本：`1.8.0-dev.58edc407`（backend/frontend/worker）
- bundle：`.dev/deploy/itsm-ai-outcome-3cfc1dcc.bundle`（30,942 B，`58edc407..3cfc1dcc`；`git bundle verify` okay）
- 迁移：本批无新增迁移，demo ledger 保持 **105**

## 1. 部署（A → B → C）

| 步骤 | 结果 |
|---|---|
| A. fetch/checkout | ✅ 分支 `deploy-demo-ai-outcome-3cfc1dcc`，`HEAD=3cfc1dcc`；`.env.demo VERSION=1.8.0-dev.3cfc1dcc`；新文件（artifact_detail.go / citations.ts / artifacts 页 / help-doc 页）全部落树 |
| B. 构建 | ✅ `BUILD_EXIT=0`；`Image itsm-backend:1.8.0-dev.3cfc1dcc Built` + `Image itsm-frontend:1.8.0-dev.3cfc1dcc Built`（磁盘 57G 可用） |
| C. up -d + init | ✅ `init_wait_exit=0`；ledger 105→105（无新迁移）；seed 幂等（templates_created=0 / grants_skipped=39） |
| 容器终态 | backend/worker/frontend 均 `Up (healthy)` 且镜像 tag=`3cfc1dcc`；`itsm-init-demo` `exited/exit=0`；nginx/redis/ai-service 保持运行 |
| 版本 | `GET /api/v1/version` → `1.8.0-dev.3cfc1dcc`；`/` 200 |

A/B 指纹（镜像内 grep）：

| 指纹 | 旧 `58edc407` | 新 `3cfc1dcc` |
|---|---|---|
| backend `artifactTitle`（`/app/main`） | 0 | **1** |
| frontend `/ai/help-doc` | — | **1** |
| frontend `/ai/artifacts/` | — | **1** |

## 2. 运行时验证（`deploy238_ai_outcome_verify.sh`，全绿）

| # | 检查 | 结果 |
|---|---|---|
| V1 | 版本 / admin 登录 | `1.8.0-dev.3cfc1dcc`；登录成功，cookie_lines=6 |
| V2 | 帮助文档详情 | `GET /ai/help/doc?path=docs/help/incidents/create.md` → 200，`pageKeys=["incident.create"]`，返回 Markdown 正文 |
| V3 | 路径穿越防护 | `path=../../etc/passwd` → **404**（不读任意文件） |
| V4 | 产物不存在 | `GET /ai/artifacts/99999999` → **404**，`code=4004`「产物不存在或无权访问」 |
| V5 | 真实产物详情 | `GET /ai/artifacts/2` → 200，`kind=plan`、`title=工单字段草案`、`toolName=draft_ticket_fields`、含 content/evidence |
| V6 | 新路由可达 | `/ai/chat` 200、`/ai/help-doc` 200、`/ai/artifacts/2` 200、`/` 200 |
| V7 | 日志扫描（近 10m） | backend/worker 无 panic / fatal / error |

### 关键复现点

V5 返回的 `artifact_id=2` 正是原问题报告里渲染成「产物 #2 / artifact:2」的那条产物。
当前接口返回 `title=工单字段草案`，前端据此渲染可读名称并提供 `/ai/artifacts/2` 打开入口，
`artifact:2` 仅在「引用」动作写入剪贴板，不再出现在正文。

## 3. 终态采集（`deploy238_ai_outcome_evidence.sh`）

```
branch=deploy-demo-ai-outcome-3cfc1dcc
head=3cfc1dccba549b451ab9499177e6faceabdc0332
VERSION=1.8.0-dev.3cfc1dcc

itsm-ai-service-demo|itsm-ai-service:1.8.0-dev.5b070145|Up 12 hours (healthy)
itsm-backend-demo|itsm-backend:1.8.0-dev.3cfc1dcc|Up 3 minutes (healthy)
itsm-frontend-demo|itsm-frontend:1.8.0-dev.3cfc1dcc|Up 2 minutes (healthy)
itsm-nginx-demo|nginx:alpine|Up 7 days (healthy)
itsm-redis-demo|redis:7-alpine|Up 7 days (healthy)
itsm-worker-demo|itsm-backend:1.8.0-dev.3cfc1dcc|Up 3 minutes (healthy)
init_demo=exited|exit=0

ledger_demo=105
pending_approvals=0
```

产物台账样例（title 非空，无 `#id` 拼接）：

```
2|plan|工单字段草案|draft_ticket_fields
1|plan|工单字段草案|draft_ticket_fields
```

## 4. 部署脚本

- `.dev/deploy/deploy238_ai_outcome_a.sh`（fetch/checkout + VERSION）
- `.dev/deploy/deploy238_ai_outcome_b.sh`（镜像构建）
- `.dev/deploy/deploy238_ai_outcome_c.sh`（up -d + 健康 + A/B 指纹）
- `.dev/deploy/deploy238_ai_outcome_verify.sh`（运行时验证）
- `.dev/deploy/deploy238_ai_outcome_evidence.sh`（终态采集）

原始输出：`.dev/deploy/ai-outcome-verify-3cfc1dcc.log`、`.dev/deploy/ai-outcome-evidence-3cfc1dcc.log`

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
git checkout -f deploy-demo-ai-sr-58edc407
sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.58edc407/' .env.demo
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-backend-demo itsm-worker-demo itsm-frontend-demo
```

旧镜像 `itsm-backend:1.8.0-dev.58edc407` / `itsm-frontend:1.8.0-dev.58edc407` 仍在本地，无需重建。
