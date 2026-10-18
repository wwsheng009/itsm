# 238 demo 全栈部署证据（第二轮）：/assets 路由修复 + LLM 超时可配置（2026-10-18，1.8.0-dev.5a1f1b03）

## 变更内容

- 范围：`553bc28c..5a1f1b03` 共 2 提交：
  - `5171e53d` fix(frontend): Vite 产物目录改为 static，消除 /assets 与 SPA 路由冲突
  - `5a1f1b03` feat(llm): 后端 LLM 请求超时可配置（LLM_TIMEOUT / llm.timeout，默认 30s）
- 交付镜像：`itsm-backend:1.8.0-dev.5a1f1b03`（sha256:8c8fc17e…）、`itsm-frontend:1.8.0-dev.5a1f1b03`（sha256:325dde33…）；`itsm-ai-service` retag 对齐（代码未变）。
- 配置：`.env.demo` 增 `LLM_TIMEOUT`（先 15，后调 5；网关默认重试 3 次）。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-pm-p2b-full.bundle`（基线=远端 553bc28c，12,930B） | fetch/checkout OK（HEAD=5a1f1b03，分支 deploy-demo-pm-p2b） |
| A | checkout + `.env.demo VERSION` + `LLM_TIMEOUT=15` | `A-STATUS: DONE` |
| A2 | 磁盘清理 + retag ai + 后台顺序构建 | `retag-ai-ok`、`A2-STATUS: DONE` |
| W | backend → frontend 顺序构建 | `BUILD_BACKEND_EXIT=0`、`BUILD_FRONTEND_EXIT=0`、`BUILD_ALL_DONE` |
| C | `up -d backend/worker/frontend`（init 重跑）+ 健康 | 三容器切 5a1f1b03 healthy；version=5a1f1b03 |
| AB | 镜像 A/B 特征对比 | 见下 |
| Tune | `LLM_TIMEOUT 15→5` + 仅重启 backend/worker | `printenv LLM_TIMEOUT=5`、health=200 |

## 验证证据

### 1) `/assets` 路由冲突修复（核心）

| 项 | 旧（553bc28c） | 新（5a1f1b03） |
|---|---|---|
| `GET /assets`（宿主 8089） | 301 → `http://172.18.3.238:3000/assets/`（落 Grafana） | **200（无 redirect）** |
| `GET /assets/new` | 404 | **200** |
| `index.html` 中 `/static/` 引用数 | 0 | **5** |
| 镜像内产物目录 | `html/assets/` 存在 | `html/static/` 存在 |

Tabbit 实测（登录 admin）：
- `/assets?fresh=…` 渲染「资产管理」页（资产总数 1 / 列表行 `AST-P2-DEMO-001`）✅
- UI「新增资产」→ `/assets/new` 表单填写 → 保存 → 回到 `/assets` 且列表出现新资产 `AST-UI-843111` ✅
- 项目 `#1` → 结项清单 →「添加登记」→ 候选下拉命中 `AST-UI-843111 · UI 资产 843111 / available` → 登记成功，明细卡显示 2 条（新 + 历史）✅
- 截图：`…\Runtime\artifacts\task-1774b907-…\screenshot-shot-2dc4fbc6-….png`

> 注意：Chrome 会缓存 301。曾访问过旧 `/assets` 的浏览器需强刷/清缓存后再访问（曲线）：本次自动化用 `?fresh=` 绕过缓存验证；新用户无此问题。

### 2) LLM 超时可配置

| 配置 | ai/summary | ai/risk-warnings | ai/minutes/tasks |
|---|---|---|---|
| 未配置（553bc28c） | 60,480ms | 60,402ms | 60,404ms |
| `LLM_TIMEOUT=15` | 46,080ms | 46,012ms | 46,006ms |
| `LLM_TIMEOUT=5` | **16,076ms** | **16,011ms** | **16,000ms** |

三者均 200 + `degraded=true`（`deterministic` / `rules` / `none`），语义不变。
镜像 A/B：后端二进制 `LLM_TIMEOUT` 字符串 旧=0 → 新=1；容器 env `LLM_TIMEOUT` 生效。

## 观察项

1. AI 端到端延迟 = `LLM_TIMEOUT × 网关重试次数(3)`（46s ≈ 3×15，16s ≈ 3×5）；如需更快可继续调小或收敛网关重试。
2. 浏览器对历史 301 的缓存需强刷（见上）。
3. 工作区存在**并行工作流**未提交改动（`internal/authz/roles.go` + `service/msp_record_projection.go`：P2+ MSP 工作台 `recordType=project` 只读投影 + MSP 客户角色补 `project:read/write`），本批**未包含、未部署**。

## 复跑方式

```bash
NEW=1.8.0-dev.5a1f1b03 bash /tmp/deploy238_pm_p2b_a.sh        # n9e
NEW=1.8.0-dev.5a1f1b03 bash /tmp/deploy238_pm_p2b_a2.sh       # sudo
bash /tmp/deploy238_pm_p2b_w.sh                                # sudo（轮询）
NEW=1.8.0-dev.5a1f1b03 bash /tmp/deploy238_pm_p2b_c.sh        # sudo
bash /tmp/deploy238_pm_p2b_ab.sh                               # sudo
bash /tmp/deploy238_pm_p2b_tune.sh                             # sudo（LLM_TIMEOUT=5）
```