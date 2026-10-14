---
name: itsm-deploy
description: Deploy ITSM builds to the 172.18.3.238 (238) demo/prod Docker stacks via git bundle + .dev/deploy scripts. Use when the user asks to 部署/发布/更新 ITSM 到服务器/demo 环境/238, ship frontend or backend changes to the demo stack (http://172.18.3.238:8089/), verify a deployed version, or troubleshoot a failed ITSM deployment. Covers version tagging (1.8.0-dev plus short sha), image rebuild, container rollout, and post-deploy verification.
---

# ITSM 238 部署

把本仓库改动部署到 238 开发机的 Docker 栈（demo 8089 / prod 8088）。执行机是本 Windows 工作站，远端是 Ubuntu + Docker Compose。

## 连接（先读 references/environments.md）

统一用仓库自带 helper（凭据走环境变量，不落命令行）：

```powershell
$env:ITSM_DEPLOY_HOST='172.18.3.238'; $env:ITSM_DEPLOY_USER='n9e'; $env:ITSM_DEPLOY_PASSWORD='passw0rd'
python .dev/deploy/remote.py "echo ok"                  # 普通命令
python .dev/deploy/remote.py --sudo "docker ps"         # sudo 包装
python .dev/deploy/remote.py --put <local> <remote>     # 上传
```

首次接手或环境不符时，读 `references/environments.md`（238/189 两套环境、凭据、端口、容器清单）。

## 核心流程（demo 前端更新，最常见路径）

前提：本地改动已提交。以下 `<NEW>` = `1.8.0-dev.$(git rev-parse --short HEAD)`，`<OLD>` = 部署前 demo 正在跑的 tag（用 `docker ps` 查）。

1. **打包增量 bundle**（基线 = 远端当前 HEAD，不是本地 HEAD~1）：
   ```powershell
   git bundle create ".dev/deploy/itsm-<tag>.bundle" <remote-head-sha>..HEAD
   ```
2. **上传** bundle + 三段脚本（`deploy238_demo_fe_a/b/c.sh` 已在 `.dev/deploy/`，若无则按现状重建）。
3. **A 步**：`BUNDLE=<remote-bundle> NEW=<NEW> OLD=<OLD> bash /tmp/deploy238_demo_fe_a.sh` — fetch/checkout → 改 `.env.demo` VERSION → retag backend/ai（前端-only 改动）。
4. **B 步**：`NEW=<NEW> bash /tmp/deploy238_demo_fe_b.sh` — 用 prod compose 上下文构建 `itsm-frontend:<NEW>`（后台 setsid，3-6 分钟）。
5. **C 步**：`NEW=<NEW> bash /tmp/deploy238_demo_fe_c.sh` — `up -d itsm-frontend-demo` + 健康检查 + `curl 127.0.0.1:8089`。
6. **验证**：容器引用新 tag + A/B 镜像特征对比（见 pitfalls.md P8）+ 部署证据文档（`.dev/deploy/DEMO-*EVIDENCE.md` 模式）并提交。

后端改动：把 B 步换 `build itsm-backend`，C 步 `up -d itsm-backend-demo itsm-worker-demo`（init 会跑迁移）。全栈/生产栈（8088）改动同理替换 compose 服务名，流程不变。

## 决策要点

- **构建一律 `--env-file .env.prod -f docker-compose.prod.yml`**，无论目标栈是 demo 还是 prod（pitfalls P3/P4）。
- **bundle 基线永远取远端真实 HEAD**：先 `git log --oneline -1` 远端，再本地打包（pitfalls P1）。
- **只 `up -d` 受影响服务**，绝不 `down -v`（会删 itsm_demo 数据，pitfalls P6）。
- 磁盘 <10G 空闲时先清理旧镜像（`docker image prune -f`）再构建。
- 登录验证 401 ≠ 部署失败，看 API 是否通（pitfalls P9）。

## 排障

部署失败时按序查 `references/pitfalls.md`（P1 bundle 基线 → P2 CRLF → P3 compose 服务名 → P4 env 变量 → P5 轮询 → P6 容器操作 → P7 乱码 → P8 A/B 验证 → P9 401 → P10 remote.py 限制），每条含症状与修复命令。

## 证据留存（每次部署后）

写 `.dev/deploy/DEMO-<主题>-<date>-<sha>-EVIDENCE.md`：变更内容、步骤表（脚本/结果）、验证证据（镜像 A/B 对比数字、curl 状态码、容器 healthy 列表）、遗留项。随部署提交一起 commit。
