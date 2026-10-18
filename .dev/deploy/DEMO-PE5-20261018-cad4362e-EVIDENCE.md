# 238 demo 全栈部署证据：单据挂接项目（创建时，五域）+ BPMN 遗留收口（2026-10-18，1.8.0-dev.cad4362e）

## 变更内容

- 范围：`12f383c2..cad4362e` 单提交 `cad4362e`（78 文件）：
  - PE-5 增补：五域（工单/事件/问题/变更/服务请求）创建请求支持可选 `projectId`；`handlers/common/projectlink.GuardCreate` 统一前置校验（`project:write` 动态复核 + 项目存在且非终态），`project_id` 随 INSERT 一次写入；未装配端口 fail-closed 503。
  - 新增 5 条 `GET /api/v1/{tickets|incidents|problems|changes|service-requests}/:id/project` 反查端点；前端 `ProjectSelect`（创建页）/`RecordProjectLink`（详情页「所属项目」+ 加入/移出）接入五域。
  - BPMN 遗留收口：`create_incident` / `create_change` / `create_request` 读取流程变量 `projectId`（兼容 `project_id`）并复用同一校验端口；顺带修复 `create_incident` 存量缺陷（必填 `incident_number` 缺失、空 `type/priority/severity` 触发枚举校验失败）。
- 交付镜像：
  - `itsm-backend:1.8.0-dev.cad4362e` = `sha256:56c7814d0b68a7123d9c88cb56593e0780ccf37ef628dc60834364ebf13c32e3`
  - `itsm-frontend:1.8.0-dev.cad4362e` = `sha256:4b4309dc789a5ebeaad710f22d2fd386c36d7599f84d1a2a5d351aaf132df171`
- 旧版本：backend `1.8.0-dev.12f383c2`、frontend `1.8.0-dev.003f17ed`。
- 配置：`.env.demo` `VERSION=1.8.0-dev.cad4362e`（`LLM_TIMEOUT=5`、`RLS_MODE=shadow`、`DEPLOYMENT_MODE=saas_msp` 保持不变）。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-pe5-cad4362e.bundle`（基线=远端 12f383c2，96,852B） | fetch/checkout OK（HEAD=cad4362e，分支 `deploy-demo-pe5-cad4362e`） |
| A | checkout + `.env.demo VERSION` | `A-STATUS: DONE` |
| B | prod compose 上下文构建 backend + frontend（后台 setsid） | `BUILD_EXIT=0`（两镜像均 Built） |
| C | `up -d backend/worker/frontend`（init 重跑）+ 健康 | 三容器切 cad4362e healthy；init ExitCode=0 |
| E2E | 登录 → 项目列表 → 携带 projectId 建单 → 反查 → 反例 | 全部通过（见下） |

## 验证证据

### 1) 运行态与健康

| 项 | 结果 |
|---|---|
| `itsm-backend-demo` | `itsm-backend:1.8.0-dev.cad4362e`，Up 10 minutes (healthy) |
| `itsm-worker-demo` | `itsm-backend:1.8.0-dev.cad4362e`，Up 10 minutes (healthy) |
| `itsm-frontend-demo` | `itsm-frontend:1.8.0-dev.cad4362e`，Up 10 minutes (healthy) |
| `itsm-init-demo` | Exited (0) |
| `GET http://127.0.0.1:8089/` | 200 |
| `GET /api/v1/health` | 200 |
| `GET /api/v1/version` | `{"version":"1.8.0-dev.cad4362e"}` |

### 2) 镜像 A/B（新代码确实进镜像）

| 特征 | 旧 | 新 |
|---|---|---|
| backend 二进制含 `创建时挂接项目校验失败`（BPMN 新校验文案） | 0 | **1** |
| frontend `static/` 产物含 `所属项目`（RecordProjectLink） | 1 | **13** |
| frontend `static/` 产物含 `attachRecord` | 1 | **2** |

### 3) API 端到端（demo 8089，admin cookie + CSRF）

| 步骤 | 结果 |
|---|---|
| `POST /api/v1/auth/login` | 200（HttpOnly cookies 生效） |
| `GET /api/v1/csrf-token` | 200（token 长度 24） |
| `GET /api/v1/projects?limit=20` | 命中非终态项目 `#3` |
| `POST /api/v1/tickets`（`projectId=3`） | 200，工单 `#65` 创建成功 |
| `GET /api/v1/tickets/65/project` | `project.id=3`（创建时挂接生效） |
| `POST /api/v1/tickets`（`projectId=999999`） | **404**，`code=4004`「项目不存在或不可见」（fail-closed，零落库） |

> 冒烟工单 `#65`（标题 `PE-5 冒烟-创建时挂接-<random>`）保留在 demo 作为部署证据；项目 `#3` 未修改。

## 观察项

1. 本次为 **API 级 + 镜像特征级** 验证；浏览器 UI 走查（ProjectSelect/RecordProjectLink 交互）未在本轮执行，留待后续 UI 回归。
2. BPMN `create_*` 路径的验证以单测覆盖（`service/bpmn/project_link_test.go` 12 用例）；demo 上未触发真实流程（无对应流程实例夹具）。
3. 失败分支：未装配端口携带 `projectId` 时 fail-closed（503）；不携带 `projectId` 的存量流程直通（单测覆盖）。

## 复跑方式

```bash
# 本地：git bundle create <file> 12f383c2..cad4362e
python .dev/deploy/remote.py --put .dev/deploy/itsm-pe5-cad4362e.bundle /tmp/itsm-pe5-cad4362e.bundle
python .dev/deploy/remote.py --put .dev/deploy/deploy238_pe5_{a,b,c,e2e,ab}.sh /tmp/   # 逐个上传
python .dev/deploy/remote.py "NEW=1.8.0-dev.cad4362e BUNDLE=/tmp/itsm-pe5-cad4362e.bundle bash /tmp/deploy238_pe5_a.sh"
python .dev/deploy/remote.py --sudo "NEW=1.8.0-dev.cad4362e bash /tmp/deploy238_pe5_b.sh"
python .dev/deploy/remote.py --sudo "grep -oE 'BUILD_EXIT=[0-9]+' /tmp/build-pe5-1.8.0-dev.cad4362e.log | tail -1"
python .dev/deploy/remote.py --sudo "NEW=1.8.0-dev.cad4362e bash /tmp/deploy238_pe5_c.sh"
python .dev/deploy/remote.py --sudo "bash /tmp/deploy238_pe5_e2e.sh"
python .dev/deploy/remote.py --sudo "bash /tmp/deploy238_pe5_ab.sh"
```