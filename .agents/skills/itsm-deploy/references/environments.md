# ITSM 部署环境清单（references/environments.md）

## 238 开发/演示机（主力部署目标）

| 项 | 值 |
|---|---|
| SSH | `n9e@172.18.3.238`，密码 `passw0rd` |
| 免密通道 | 本地 OpenSSH `ssh.exe` + `id_ed25519`（n9e authorized_keys 已装），优先用 |
| 密码兜底 | `C:\Program Files\PuTTY\plink.exe -pw`（238 本机可直连，无需代理） |
| SSH helper | `python .dev/deploy/remote.py "cmd"`（paramiko，读 env：`ITSM_DEPLOY_HOST/USER/PASSWORD`）；`--sudo` 自动包 `sudo -S`；`--put` 上传 |
| 仓库 | `/home/n9e/itsm/repo` |
| 部署模式 | Docker Compose（`/snap/docker`），生产栈 8088 + 演示栈 8089 |
| 磁盘 | 100G（历史约 86% 占用，构建前 `df -h /` 确认 >10G 空闲） |

### 生产栈（itsm-prod，端口 8088）
- 容器：`itsm-frontend-prod` / `itsm-backend-prod` / `itsm-worker-prod` / `itsm-nginx-prod` / `itsm-ai-service-prod` / `itsm-postgres-prod` / `itsm-redis-prod`
- env 文件：`.env.prod`
- compose：`docker-compose.prod.yml`（frontend/backend 构建入口在此）

### 演示栈（itsm-demo，端口 8089）
- 容器：`itsm-*-demo` 七件套（frontend/backend/worker/nginx/ai-service/redis + init 跑完即退）
- env 文件：`.env.demo`（关键字段 `VERSION`、`DEMO_DB_NAME=itsm_demo`、`DEMO_HTTP_PORT=8089`、`DEPLOYMENT_MODE=saas_msp`）
- compose：`docker-compose.demo.yml`（**无 build 段**——构建必须走 prod compose，见 pitfalls.md P3）
- 数据库 `itsm_demo` 复用生产 postgres 容器（`docker exec itsm-postgres-prod psql -U itsm -d itsm_demo`）
- 健康检查：`curl -fsS http://127.0.0.1:8089/`；后端 readyz 在宿主 `127.0.0.1:8091`
- demo 账号：库内 seeded（admin / demo.msp.* / demo.mfg.*，见 users 表）；口令以 `.env.demo` 的 `DEMO_ADMIN_PASSWORD` 与当次 init 播种为准，猜密码（Msp@2026Staff!、Demo@2026! 等均 401 过）
- 版本约定：`1.8.0-dev.<short-sha>`（例：`1.8.0-dev.b86819be`）

## 192.210.174.189（已评估，不适合 ITSM 栈）

- SSH：`root@192.210.174.189`，密码 `3RKH04x2qppJoy86SI`
- **仅可经 SOCKS5 代理 `127.0.0.1:10808` 访问**（用户口中的 10810 是 HTTP 口；10808 是 socks 口；clash-verge/mihomo 提供，10809 已废）
- 连接命令（plink + Git 的 connect.exe 做 proxycmd）：
  ```
  & "C:\Program Files\PuTTY\plink.exe" -batch -ssh -pw <pw> `
    -hostkey "SHA256:vgg1f7Vwd6EbtcURSZKu6I1i8Be66AnrodjgWH5IxKI" `
    -proxycmd "`"C:\Program Files\Git\mingw64\bin\connect.exe`" -S 127.0.0.1:10808 192.210.174.189 22" `
    root@192.210.174.189 "<cmd>"
  ```
- 资源：**1 核 CPU / 961MB 内存 / 无 Docker**，跑着 ai-gateway 等常驻服务（已占 25% 内存）
- **结论：不要在此机部署 ITSM**（全栈需要 2C/4G+）；此机只适合轻量 agent 类服务
- 连接工具沉淀：`.dev/deploy/remote189.py`（paramiko + PySocks SOCKS5，读同样 env 变量）
