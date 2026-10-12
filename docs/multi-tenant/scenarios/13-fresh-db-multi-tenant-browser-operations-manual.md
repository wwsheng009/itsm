# 13 · 全新库多租户浏览器操作手册

> **状态**：v0.4（2026-10-12，已评审 + 批次 FDB-B1 首跑实测 + 阶段 7 实测完成；证据见 [run-summary](../evidence/fresh-db-browser-B1/run-summary-20261012-1925.md)）｜**定位**：与[测试计划 12](./12-fresh-db-multi-tenant-browser-test-plan.md)配套的**逐步操作手册**——从新库准备（§0，运维）到阶段 1–7 的**浏览器操作**（模拟真实用户），每步给出操作路径、输入、预期界面反馈与验证手法。
> **读者**：执行测试的测试/QA、实施交付、平台/服务商/客户管理员
> **关联**：[12 测试计划](./12-fresh-db-multi-tenant-browser-test-plan.md)｜[00 环境与账号](./00-environment-and-accounts.md)｜[07 工作台协作](./07-msp-workbench-collaboration.md)｜[08 审计与隔离](./08-audit-and-isolation-verification.md)｜[10 切换边界](./10-membership-switch-and-workbench-boundary.md)｜[11 离职编排](./11-offboarding.md)
> **前置**：已读测试计划 §2–§4；本手册中的口令一律用占位符 `<口令>`，执行人自行按规则生成并本地保存，**不得写入本文件或提交到仓库**。

---

## 0. 环境准备（新库 + `saas_msp`，运维动作，非浏览器）

> 目标：准备一个**独立于 `itsm_prod` 的全新数据库**，完成迁移/播种/平台管理员创建，启动后端（`saas_msp`）与前端，并通过健康检查。全程不修改现有联调库数据。
>
> **方案选择**：
> - **方案 A（默认，推荐）**：复用现有 PostgreSQL 实例（经 SSH 隧道），新建独立数据库 `itsm_msp_fdb`；后端/前端本机运行并指向新库。适合本机现状（无需 Docker Desktop）。
> - **方案 B（备选，完全隔离）**：本地 Docker Compose 独立项目（独立卷/端口/库），见 §0.7。
> - 严禁：对 `itsm_prod` 执行任何 `DROP DATABASE` / `-fresh` / `down -v` 类操作；当前仓库根 `Makefile` **没有** `db-reset` target，文档中的旧命令不要使用。

### 0.0 批次与环境指纹

| 项 | 值（示例） |
|---|---|
| 批次号 | `FDB-B1`（重跑用 `FDB-B2`，租户编码同步换 `MSPT2/CUSTA2/CUSTB2`） |
| 新库名 | `itsm_msp_fdb` |
| 后端 | `http://127.0.0.1:8090`（`saas_msp`） |
| 前端 | `http://localhost:3000` |
| 隧道 | PG `127.0.0.1:15433` → 远程 `127.0.0.1:5433`；Redis `16380` → `6380` |
| 记录 | commit、迁移账本、执行人、时间窗、批次号 |

### 0.1 前置检查

```powershell
cd E:\projects\itsm\itsm
# 1) 隧道状态（未启动则启动；隧道窗口保持开启）
.\.dev\dev.ps1 status
.\.dev\dev.ps1 tunnel
# 2) 确认端口可达（15433 / 16380）
Test-NetConnection 127.0.0.1 -Port 15433 | Select-Object TcpTestSucceeded
Test-NetConnection 127.0.0.1 -Port 16380 | Select-Object TcpTestSucceeded
# 3) 方案 A 依赖：远程主机可达（不可达则隧道无法建立，需改用方案 B）
Test-NetConnection 172.18.3.238 -Port 22 | Select-Object TcpTestSucceeded   # 期望 True
# 4) 远程磁盘余量（新库预计 <100MB；建议空闲 ≥1G，低于 500M 先清理/扩容）
ssh -o BatchMode=yes n9e@172.18.3.238 'df -h / | tail -1'
```

> 说明：本机 `E:\bins\ssh-client.exe` 不存在，`.dev` 已改用 OpenSSH `ssh.exe`（密钥免密）或 `plink.exe` 兜底；详见 `.dev/connection-guide.md`「本机适配记录」。

### 0.2 创建新库（方案 A）

```powershell
# 在远程 PG 容器内执行（推荐：不依赖本机 psql / Docker Desktop）
ssh -o StrictHostKeyChecking=accept-new n9e@172.18.3.238 'echo passw0rd | sudo -S -p "" docker exec itsm-postgres-prod psql -U itsm -d postgres -c "CREATE DATABASE itsm_msp_fdb"'
# 预期输出：CREATE DATABASE
# 若提示权限不足：把 -U itsm 改为 -U postgres 重试
# 若提示 already exists：说明库名已被占用——换一个新库名（如 itsm_msp_fdb2），不要删除旧库
```

> `ssh` 免密不可用时，用 PuTTY `plink.exe` 兜底（hostkey 见 connection-guide）：
> `& 'C:\Program Files\PuTTY\plink.exe' -pw passw0rd -hostkey SHA256:xpNuX4MMX9Nuj0HcgHPVUOdFnwse0jwHC6hViPhedWQ n9e@172.18.3.238 'echo passw0rd | sudo -S -p "" docker exec itsm-postgres-prod psql -U itsm -d postgres -c "CREATE DATABASE itsm_msp_fdb"'`

### 0.3 初始化新库（迁移 + 播种 + 平台管理员，一次性）

```powershell
cd E:\projects\itsm\itsm\itsm-backend

# 从现有 .env 读取连接口令（不打印、不写入文档）
$env:DB_PASSWORD    = (Select-String -Path .env -Pattern '^DB_PASSWORD=').Line.Split('=',2)[1].Trim()
$env:REDIS_PASSWORD = (Select-String -Path .env -Pattern '^REDIS_PASSWORD=').Line.Split('=',2)[1].Trim()
$env:JWT_SECRET     = (Select-String -Path .env -Pattern '^JWT_SECRET=').Line.Split('=',2)[1].Trim()

# 指向新库 + 独立 Redis 逻辑库（避免与既有实例的键混用）
$env:DB_HOST='127.0.0.1'; $env:DB_PORT='15433'; $env:DB_USER='itsm'; $env:DB_NAME='itsm_msp_fdb'; $env:DB_SSLMODE='disable'
$env:REDIS_HOST='127.0.0.1'; $env:REDIS_PORT='16380'; $env:REDIS_DB='1'

# 部署模式与初始化开关
$env:DEPLOYMENT_MODE='saas_msp'
$env:ADMIN_PASSWORD='<设置平台管理员强口令，≥12 位>'
$env:ITSM_BOOTSTRAP_ONLY='true'; $env:ITSM_AUTO_MIGRATE='true'; $env:ITSM_AUTO_SEED='true'

.\main.exe
```

**预期**：日志出现 `database schema ensured`、`tenant_guard: pass`、初始化组件 succeeded、`deployment self-check`（初期无 provider 租户的告警属预期）；进程初始化完成后**自动退出**（`ITSM_BOOTSTRAP_ONLY=true` 为一次性模式）。

> 若迁移日志提示缺少 `vector` 扩展：在目标库补执行一次
> `ssh -o StrictHostKeyChecking=accept-new n9e@172.18.3.238 'echo passw0rd | sudo -S -p "" docker exec itsm-postgres-prod psql -U itsm -d itsm_msp_fdb -c "CREATE EXTENSION IF NOT EXISTS vector"'`。

**可选复核**（迁移账本）：

```powershell
# 保持上面同一 pwsh 会话的环境变量
go run -tags migrate cmd/migrate/main.go -status
# 预期：applied=N pending=0
```

> 注意：不要使用 `-fresh`（会 DROP+CREATE 数据库）；`-reset/-down` 同样危险，测试库上也不要随手用。

### 0.4 启动后端（指向新库）

```powershell
cd E:\projects\itsm\itsm\itsm-backend
# 若旧后端在运行，先停掉（保留隧道）：端口 8090 只能被一个进程占用
if (Test-Path ..\.dev\backend.pid) { Stop-Process -Id (Get-Content ..\.dev\backend.pid) -Force -ErrorAction SilentlyContinue }

# 保持 §0.3 的同一 pwsh 会话环境变量，但必须清掉一次性初始化开关（否则后端初始化后直接退出）
Remove-Item Env:ITSM_BOOTSTRAP_ONLY -ErrorAction SilentlyContinue
$env:ITSM_AUTO_MIGRATE='false'; $env:ITSM_AUTO_SEED='false'

# 后台启动
Start-Process -FilePath .\main.exe -WorkingDirectory (Get-Location) -WindowStyle Hidden `
  -RedirectStandardOutput E:\projects\itsm\itsm\.dev\backend-fdb.out.log `
  -RedirectStandardError  E:\projects\itsm\itsm\.dev\backend-fdb.err.log

# 健康检查
(Invoke-WebRequest http://127.0.0.1:8090/api/v1/healthz -UseBasicParsing).StatusCode   # 200
(Invoke-WebRequest http://127.0.0.1:8090/api/v1/readyz  -UseBasicParsing).StatusCode   # 200
```

> 若 `readyz` 返回 503：说明初始化组件未全部 succeeded 或迁移未到最新——回到 §0.3 复核日志/迁移账本。
> 后端日志（含 `deployment gate resolved` / `tenant_guard: pass`）见 `.dev\backend-fdb.out.log` 与 `itsm-backend\logs\`。

### 0.5 启动前端

```powershell
cd E:\projects\itsm\itsm\itsm-frontend
npm run dev        # 打开 http://localhost:3000
```

> Vite 已配置 `server.proxy['/api'] → ITSM_BACKEND_URL || http://127.0.0.1:8090`，默认即指向本机后端；无需额外配置。

### 0.6 阶段 0 校验清单（出口）

| # | 校验项 | 方法（浏览器/命令） | 通过标准 |
|---|---|---|---|
| 1 | 服务就绪 | `healthz` / `readyz` | 200 |
| 2 | 模式正确 | 登录后 DevTools Console：`fetch('/api/v1/msp/status',{credentials:'include'}).then(r=>r.json()).then(d=>console.log(d.data))` | `deploymentMode=saas_msp`、`mspRoutesEnabled=true` |
| 3 | 平台账号 | 浏览器打开 `/login`，`admin` + §0.3 设置的 `ADMIN_PASSWORD` | 登录成功，侧栏可见「租户管理」 |
| 4 | 自检日志 | `.dev\backend-fdb.out.log` | 门控 + `tenant_guard: pass` |
| 5 | 环境指纹 | 记录到证据 run-summary | 库名/端口/commit/批次/时间窗 |

### 0.7 方案 B（备选）：本地独立 Compose 栈

> 仅在 Docker Desktop 可用且需要完全隔离时使用。dev compose 的库名/端口是硬编码的，必须用 override 文件覆盖；以下为**参考骨架**，执行前逐服务核对 `DATABASE_URL` 与 `DB_*` 同步。

新建 `docker-compose.dev.fdb.yml`（仓库根，仅测试用）：

```yaml
services:
  postgres:
    ports: ["55433:5432"]
    environment:
      POSTGRES_DB: itsm_fdb
      POSTGRES_USER: itsm_fdb
      POSTGRES_PASSWORD: dev123
  itsm-init:
    environment:
      - DATABASE_URL=postgres://itsm_fdb:dev123@postgres:5432/itsm_fdb?sslmode=disable
      - DB_USER=itsm_fdb
      - DB_PASSWORD=dev123
      - DB_NAME=itsm_fdb
      - DEPLOYMENT_MODE=saas_msp
      - ADMIN_PASSWORD=<强口令>
  itsm-backend:
    environment:
      - DATABASE_URL=postgres://itsm_fdb:dev123@postgres:5432/itsm_fdb?sslmode=disable
      - DB_USER=itsm_fdb
      - DB_PASSWORD=dev123
      - DB_NAME=itsm_fdb
      - DEPLOYMENT_MODE=saas_msp
```

```powershell
cd E:\projects\itsm\itsm
$env:DEPLOYMENT_MODE='saas_msp'
docker compose -p itsm-fdb -f docker-compose.dev.yml -f docker-compose.dev.fdb.yml up -d postgres redis
docker compose -p itsm-fdb -f docker-compose.dev.yml -f docker-compose.dev.fdb.yml up itsm-init
docker compose -p itsm-fdb -f docker-compose.dev.yml -f docker-compose.dev.fdb.yml up -d itsm-backend
# 前端本机运行：$env:ITSM_BACKEND_URL='http://127.0.0.1:8090'; npm run dev
```

> 若端口冲突，可再覆盖 `itsm-backend.ports`（如 `8091:8090`）并相应调整前端 `ITSM_BACKEND_URL`。

---

## 1. 阶段 1 · 平台创建租户（1 个服务商 + 2 个客户）

> 角色：平台超级管理员（`admin`）。全部在浏览器完成。
> 参考：[场景 01](./01-platform-create-provider-tenant.md)、[场景 02](./02-platform-create-customer-tenant.md)、[场景 03](./03-tenant-provisioning-first-admin.md)。

### 1.1 创建服务商租户 `MSPT1`

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 打开 `http://localhost:3000/login`，用 `admin` 登录 | 进入平台主页；侧栏出现「租户管理」 |
| 2 | 进入 **租户管理**（`/admin/tenants`）→ 点「新建租户」 | 弹出表单：名称/编码/域名/类型/状态/到期/配额 |
| 3 | 名称=`测试服务商一`；编码=`MSPT1`；类型选 **「MSP服务商」**；状态「活跃」 | 类型选服务商后**不出现**「MSP 服务商」下拉 |
| 4 | 保存 | 弹窗关闭；列表出现 `MSPT1`，类型列 `MSP服务商` |

### 1.2 创建客户租户 `CUSTA1` / `CUSTB1`（均绑定 MSPT1）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 租户管理 → 新建租户：名称=`测试客户A`；编码=`CUSTA1`；类型选 **「MSP客户」** | 出现「MSP 服务商」下拉（必填） |
| 2 | 下拉选择 `测试服务商一（MSPT1）`；保存 | 列表出现 `CUSTA1`，类型 `MSP客户` |
| 3 | 重复上述步骤创建 `CUSTB1`（同样绑定 MSPT1） | 列表共 3 个新租户 |

### 1.3 开通三个租户（模板供给 → 首个管理员 → 完成）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 租户列表 `MSPT1` 行点「开通」 | 三步向导：模板供给 → 首个管理员 → 完成 |
| 2 | 第 1 步点「开通模板」 | 进入 loading；完成后 7 项基线显示计数、整体「已就绪」 |
| 3 | 第 2 步填写管理员（用户名默认 `admin-mspt1`，密码可留空自动生成） | 提交后显示「首个管理员已创建」；自动生成口令**仅显示一次** |
| 4 | 点「复制」保存口令 → 进入第 3 步 → 关闭 | 完成态提示 |
| 5 | 对 `CUSTA1`、`CUSTB1` 重复同样流程 | 各自生成首管 `admin-custa1` / `admin-custb1` |
| 6 | 用 `admin-mspt1` + 一次性口令登录 | 被强制跳转改密页；改密后进入服务商主页 |
| 7 | 同样完成 `admin-custa1` / `admin-custb1` 首登改密 | 客户主页可见客户业务菜单 |

> **幂等**：再次打开同一租户「开通」应显示「已就绪 / 已存在首个管理员」，不产生第二个首管。
> **反例（顺带）**：重复编码建租户 → 保存被拒；客户类型不选服务商 → 必填校验拒绝。

### 1.4 阶段 1 验证

**界面**：租户列表三行类型/状态正确；客户详情显示所属服务商。

**接口（浏览器 DevTools Console，平台登录态）**：

```js
fetch('/api/v1/tenants?page=1&pageSize=50', { credentials:'include' })
  .then(r => r.json())
  .then(d => console.log(d.data.items.filter(t => ['MSPT1','CUSTA1','CUSTB1'].includes(t.code))));
// 预期：MSPT1 type=msp_provider；CUSTA1/CUSTB1 type=msp_customer 且 mspProviderId=MSPT1.id
```

---

## 2. 阶段 2 · 超级管理员在目标租户建用户（平台通道）

> 角色：平台 `admin`。入口：`/admin/tenants` 行内「建号」（跨租户建号通道；开关 `USER_PROVISIONING_CHANNELS_ENABLED` 默认开启）。
> 参考：[场景 04 通道 A](./04-user-provisioning.md)。

### 2.1 在服务商租户建工作台员工 `prov.agent1`

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 平台登录 → `/admin/tenants` → `MSPT1` 行点「建号」 | 弹出建号表单，标题显示目标租户 |
| 2 | 填写：用户名 `prov.agent1`、姓名、邮箱、初始口令 | 逐项校验 |
| 3 | **「MSP 角色」下拉必填** → 选择「服务商技术员（provider_agent）」 | 只有 `msp_provider` 类型租户才出现该下拉；提示"必须带 MSP 角色，否则无法进入服务商工作台" |
| 4 | 提交 | 成功提示；账号出现在 MSPT1 用户列表 |

> 可选：同样方式创建 `prov.admin1`（`provider_admin`），用于对照"管理员 vs 技术员"的工作台能力差异。

### 2.2 （可选）平台通道为客户租户建号 `custa.plt1`

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | `CUSTA1` 行点「建号」 | 表单**不出现** MSP 角色字段 |
| 2 | 填写用户名 `custa.plt1` 等；如出现「角色」多选，选择默认普通用户（`end_user`） | 校验通过 |
| 3 | 提交 | 账号出现在 CUSTA1 用户列表 |

### 2.3 阶段 2 验证

| 验证 | 方法 | 预期 |
|---|---|---|
| 归属 | CUSTA1/MSPT1 用户列表 | `prov.agent1` 在 MSPT1；`custa.plt1` 在 CUSTA1 |
| 可登录 | 用 `prov.agent1` 登录 | 进入服务商作用域；侧栏出现 MSP 组（工作台/分配管理/审计看板等，具体以角色为准） |
| 身份 | DevTools：`fetch('/api/v1/auth/me',{credentials:'include'}).then(r=>r.json()).then(d=>console.log(d.data.user))` | `mspRole='provider_agent'`、`tenantId=MSPT1.id` |
| 反例 | 用重复用户名建号 | 明确报错（用户名已存在），列表不新增 |
| 反例 | 尝试给 provider 租户建号但不选 MSP 角色 | 表单必填校验拒绝 |

---

## 3. 阶段 3 · 租户管理员登录后创建普通用户

> 角色：`admin-mspt1`（服务商管理员）、`admin-custa1` / `admin-custb1`（客户管理员）。
> 参考：[场景 04 通道 B/C/D](./04-user-provisioning.md)。

### 3.1 服务商管理员在本租户建普通用户 `prov.staff1`

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-mspt1` 登录 → `/admin/users` | 用户列表加载（无 403） |
| 2 | 点「新建用户」→ 填写用户名 `prov.staff1`、姓名、邮箱、初始口令；**主角色选「最终用户（end_user）」（表单默认值）** | 表单校验通过 |
| 3 | 提交 | 列表出现 `prov.staff1` |
| 4 | 用 `prov.staff1` 登录 | 可进入服务商租户业务页 |

> ⚠️ **已知约束（执行时记录）**：租户通道（`/admin/users` 新建用户）**不含 MSP 角色选择**；这样创建的员工没有 `msp_role`，**无法进入 MSP 工作台**（属预期约束，不是缺陷）。需要工作台能力的员工必须走：① 平台通道（§2.1，可选 MSP 角色）；② 邀请通道（若界面提供 MSP 角色选择）。本阶段用 `prov.staff1` 验证"租户内建号"通道本身即可。

### 3.2 客户管理员建本租户普通用户

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-custa1` 登录 → `/admin/users` → 「新建用户」 | 表单出现（客户租户） |
| 2 | 用户名 `custa.user1`、姓名、邮箱、初始口令；**主角色选「最终用户（end_user）」（表单默认值）** | 校验通过 |
| 3 | 提交 | 列表出现 `custa.user1` |
| 4 | 对 `admin-custb1` 重复：创建 `custb.user1` | 列表出现 `custb.user1` |
| 5 | 分别用两个普通用户登录 | 均进入客户业务页；`tenantId` 分别为 CUSTA1/CUSTB1 |

### 3.3 服务商管理员通过 MSP 通道为客户建号 `custa.user2`

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-mspt1` 登录 → **分配管理**（`/msp/management`） | 页面加载；右上「为客户建号」「新建分配」 |
| 2 | 点「为客户建号」 | 弹窗带「客户」下拉（列出本服务商客户 CUSTA1/CUSTB1） |
| 3 | 客户选 `CUSTA1`；填写用户名 `custa.user2` 等 | 校验通过 |
| 4 | 提交 | 提示成功；账号创建在 CUSTA1 |
| 5 | 用 `custa.user2` 登录 | 进入 CUSTA1 业务页 |

### 3.4 反例（顺带验证）

| 反例 | 操作 | 预期 |
|---|---|---|
| 无权限员工建号 | 用 `prov.agent1` 打开 `/msp/management` 的「为客户建号」 | 无入口/按钮不可用；若直接调接口 → 403（`msp_customer:write` 缺失） |
| 未分配员工为客户建号 | 用未分配该客户的服务商员工尝试 | 服务端二次授权拒绝（403/业务错误），界面内联报错 |
| 重复用户名 | 重复提交同一用户名 | 明确报错，不产生脏数据 |

### 3.5 （可选）邀请通道

1. 客户管理员 → `/admin/users` →「邀请用户」：创建邀请（邮箱、角色）→ 列表可复制邀请链接；
2. 用无痕窗口打开 `/invite/<token>` → 设置密码并提交 → 按邀请角色入租户 → 登录；
3. 反例：过期/已用 token → 明确错误；刷新落地页不泄露租户列表。

---

## 4. 阶段 4 · 普通用户登录与业务操作（客户租户）

> 角色：客户管理员（建类型）+ 客户普通用户（建单）。参考：[场景 06](./06-customer-ticket-type-and-ticket.md)。

### 4.1 客户管理员建工单类型（普通用户无此权限）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-custa1` 登录 → `/tickets/types` | 页头「工单类型」；按钮「从预设安装」「新建类型」 |
| 2 | 点「新建类型」：编码 `TS_FDB_GENERAL`、名称 `通用支持`、默认优先级「中」 | 校验通过 |
| 3 | 提交 | 列表出现 `通用支持（TS_FDB_GENERAL）` |
| 4 | （替代路径）点「从预设安装」→ 对目标预设点「安装」 | 安装成功；已安装项置灰 |

> ⚠️ 新租户模板 7 项基线**不含工单类型**：不先建类型，建单页类型选择器为空（已知限制，不判失败）。

### 4.2 客户 A 普通用户建工单

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `custa.user1` 登录 → `/tickets/create` | 页头「新建工单」；顶部「选择工单类型」 |
| 2 | 选择类型 `通用支持` | 类型徽标回显；如类型带动态字段则出现专属字段区 |
| 3 | 标题 `[FDB-B1] VPN 无法连接`；描述 ≥10 字；优先级「中」 | 逐项校验 |
| 4 | 点「创建工单」 | 成功提示；列表出现新单，编号形如 `TKT-202610-000001` |

### 4.3 客户 B 普通用户建隔离样本单

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `custb.user1` 登录 → `/tickets/create` | 同前 |
| 2 | 若 CUSTB1 无类型：由 `admin-custb1` 先建一个类型（或从预设安装） | 类型可用 |
| 3 | 标题 `[FDB-B1] 客户 B 隔离样本`；描述 ≥10 字；提交 | 创建成功，编号生成 |

### 4.4 阶段 4 验证

| 验证 | 方法 | 预期 |
|---|---|---|
| 列表/详情 | 各租户 `/tickets` 与详情 | 标题/描述/类型/优先级与提交一致 |
| 编号 | 列表 `ticketNumber` | 形如 `TKT-<年月>-<序号>`，全局唯一 |
| 隔离 | 用 `custa.user1` 直访 B 单详情 URL | 404/403，不泄露标题 |
| 反例 | 标题 1 字符 / 描述 <10 字 | 前端校验拒绝 |
| 反例 | 未建类型直接建单 | 类型选择器为空、无法提交（已知限制） |

> 说明：`end_user` 权限含 `ticket:read/write/create/update`（`write` 经同义补齐到 `create/update`），可建单/评论并维护**本人**单据（改状态/解决按生命周期守卫：创建人/受理人/管理员）；无 `ticket:assign`。本手册"确认解决"主口径仍由客户管理员执行（§6），普通用户路径作为可选复核。

### 4.5 （可选）其它业务单据

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | `custa.user1` → `/service-requests/new`（或 `/service-catalog` → 选择目录项 → 提交请求） | 服务请求创建成功，可在列表中查看（`end_user` 具备 `service_request:read/write`） |
| 2 | 其它单据（事件/变更等）由具备对应权限的角色按实际界面执行 | 以目标版本实际可达性与权限为准；结果记入证据 |

---

## 5. 阶段 5 · 权限/角色正确性验证（三层证据 + 负向）

> 参考：[场景 08 负向矩阵](./08-audit-and-isolation-verification.md)。判定原则：**菜单/路由/接口三层一致；拒绝必须带稳定错误码；拒绝必须留痕；不得静默放行。**

### 5.1 环境基线（先做一次）

各角色登录后，在 DevTools Console 记录：

```js
fetch('/api/v1/auth/me',   { credentials:'include' }).then(r=>r.json()).then(d=>console.log('me', d.data.user));
fetch('/api/v1/auth/menus',{ credentials:'include' }).then(r=>r.json()).then(d=>console.log('menus', d.data));
```

| 角色 | 期望 `role`/`mspRole` | 期望关键菜单 |
|---|---|---|
| `admin`（平台） | `role=super_admin` | 租户管理、平台治理菜单 |
| `admin-mspt1` | `role=admin`、`mspRole=provider_admin` | 工作台、分配管理、审计看板 + provider 自身业务菜单 |
| `prov.agent1` | `mspRole=provider_agent` | MSP 面（工作台等，按权限）；**无**分配管理写入口 |
| `admin-custa1` | `role=admin` | 客户业务菜单 + `/admin/users` 等；**无** MSP/平台菜单 |
| `custa.user1` | `role=end_user` | 服务台/我的请求/知识库等；**无**管理入口 |

### 5.2 权限矩阵核对（逐格打勾）

| 能力 / 页面 | 平台 admin | 服务商管理员 | 服务商员工 | 客户管理员 | 客户普通用户 |
|---|---|---|---|---|---|
| `/admin/tenants` | ✅ | ❌ | ❌ | ❌ | ❌ |
| `/msp/workbench` | ❌（设计隔离） | ✅ | ✅（仅已分配） | ❌ | ❌ |
| `/msp/management` | ❌ | ✅ | 只读（无写） | ❌ | ❌ |
| 为客户建号 | ✅（平台通道） | ✅ | ❌ | ❌ | ❌ |
| `/admin/users` | ❌（走租户治理） | ✅（本租户） | ❌ | ✅（本租户） | ❌ |
| 工单类型管理 | ❌ | ❌ | ❌ | ✅ | ❌ |
| 建单/评论 | ❌ | 深度切换后按客户侧 | 深度切换后按客户侧 | ✅ | ✅ |
| 改状态/确认解决 | ❌ | 工作台/深度切换 | 工作台改状态；resolve 无 MSP 端点 | ✅ | ✅（本人单据） |

### 5.3 负向验证清单（浏览器 + DevTools；全部必须"按预期拒绝"）

| 场景 | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 客户用户访问 MSP 面 | `custa.user1` 直访 `/msp/workbench`；再 Console：`fetch('/api/v1/msp/workbench/tickets',{credentials:'include'})` | 页面重定向 /403；接口 **403**（"仅MSP员工可访问"） | 页面截图 + Network |
| 客户用户访问平台面 | 直访 `/admin/tenants`；Console 调 `/api/v1/tenants` | /403 页面；接口 **403**（"仅平台管理员可执行该操作"） | 截图 + Network |
| 未分配客户（头通道） | `prov.agent1` Console：`fetch('/api/v1/msp/workbench/tickets?limit=5',{credentials:'include',headers:{'X-Customer-Tenant-ID':'<CUSTB1 id>'}})` | **403**，`reasonCode=MSP_ALLOCATION_REQUIRED` | Network 响应体 |
| 未分配客户（路径通道） | Console：`fetch('/api/v1/msp/customers/<CUSTB1 id>/tickets',{credentials:'include'})` | **403** `MSP_ALLOCATION_REQUIRED`（客户不存在则 404 `CUSTOMER_TENANT_NOT_FOUND`） | Network |
| 跨租户头冲突 | `custa.user1` Console：`fetch('/api/v1/users?page=1&pageSize=5',{credentials:'include',headers:{'X-Tenant-Code':'CUSTB1'}})` | **401**，`reasonCode=TENANT_MISMATCH_REJECTED` | Network + 审计 |
| 平台管理员进 MSP 面 | `admin` 直访 `/msp/workbench` | **403/无权限页**（平台面与 MSP 面隔离；无 provider 身份） | 截图 |
| 未授权租户切换 | `prov.agent1` Console：`POST /api/v1/auth/switch-tenant` `{tenantId:<CUSTB1 id>}`（需 CSRF token） | **403** + 审计 `tenant.switch_denied`（`no_grant`） | Network + 审计 |
| 平台治理写通道（如关闭） | 平台 `admin` → 用户管理 → 重置普通用户密码 | 开关关闭时 **403** `TENANT_USER_ADMIN_DISABLED`；开关开启时正常（记录开关状态） | Network/界面提示 |
| 普通用户直访管理页 | `custa.user1` 直访 `/admin/users` | /403 页面（无权限守卫） | 截图 |

> 本表为执行清单，**不另设编号空间**；与[场景 08](./08-audit-and-isolation-verification.md) 的 N1–N6 矩阵同源（需要编号引用时以场景 08 为准）。
> 「未分配客户」「跨租户头冲突」「未授权租户切换」需要客户租户 id：可在平台租户列表或 Console `fetch('/api/v1/tenants?keyword=CUSTB1')` 获取（平台登录态）。

### 5.4 授权即时生效（可恢复性）

1. 在阶段 5 先记录「未分配客户」两项（头通道/路径通道）的 403；
2. 完成阶段 6.1 的分配后，用同一请求复测 → **200**（无需重启服务）；
3. 解除分配后（可选）再测 → 回到 403。

---

## 6. 阶段 6 · 跨租户协作与单据闭环

> 角色：服务商管理员/员工 + 客户用户/管理员。参考：[场景 07](./07-msp-workbench-collaboration.md)、[场景 10](./10-membership-switch-and-workbench-boundary.md)、[canon §7.2](../plan/msp-concept-model-and-architecture-canon.md)。
> 闭环口径：**客户建单 → 服务商工作台可见/回复/改状态/指派 → 客户回看 → 客户管理员确认解决（受保护终局）→ 服务商侧可见终态**。

### 6.1 建立分配（跨客户访问的唯一授权载体）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-mspt1` 登录 → **分配管理**（`/msp/management`）→ 「新建分配」 | 弹窗：员工 / 客户 / 分配角色（主支持/备份/专家） |
| 2 | 员工 `prov.agent1`；客户 `CUSTA1`；角色「主支持」；提交 | 提示「分配创建成功」；列表出现该行 |
| 3 | 再建一条：员工 `admin-mspt1` → `CUSTA1`（主支持） | 列表共 2 行 |
| 4 | 反例：同一员工+客户重复创建 | 409 `MSP_ALLOCATION_EXISTS`（幂等提示） |

### 6.2 工作台可见性

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `prov.agent1` 登录 → **跨客户工作台**（`/msp/workbench`） | 列表加载；顶栏有客户过滤器（全部客户/多选，带计数） |
| 2 | 过滤器勾选 `CUSTA1` | 列表收窄为该客户；URL 出现 `customerTenantIds=<id>`；刷新可复现 |
| 3 | 确认客户 B 的样本单**不可见**（未分配） | 过滤器中无 CUSTB1 或选中后无数据 |
| 4 | 反例：Console 带 `X-Customer-Tenant-ID: <CUSTB1 id>` 请求工作台 | **403** `MSP_ALLOCATION_REQUIRED`（见 §5.3「未分配客户（头通道）」） |

### 6.3 行内处理（服务商侧）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 目标工单（A-1）行点「回复」→ 输入 `已受理，请补充日志` → 发送 | 「回复已发送」 |
| 2 | 点「改状态」→ 选择 `处理中`（open→in_progress） | 提示成功；行内状态徽标更新为「处理中」 |
| 3 | 点「指派」→ 确认 | 「已指派给当前技术员」；处理人列=当前用户（`prov.agent1`） |
| 4 | 再「改状态」→ `等待中`（in_progress→pending） | 状态更新为「等待中」 |
| 5 | （观察项）状态下拉中若出现「已解决/已关闭」 | 服务商侧不应选择：`resolved` 需客户侧受保护解决流程，`closed` 非法迁移——如点击报错属**已知契约不一致**，记录为发现（不要判主线失败） |

> 批量（可选）：勾选 2 条（含跨客户）→「批量回复」→ 先显示**客户分布确认** → 提交 → 逐条结果（成功/失败分列）。

### 6.4 客户回看

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `custa.user1` 登录 → `/tickets` → 打开 A-1 详情 | 状态=「等待中」；评论区出现服务商回复 |
| 2 | 刷新/重新登录后再看 | 回复与状态保持（持久化） |

### 6.5 客户确认解决（闭环终局）

> 主口径由**客户管理员**执行（`ticket:update`）；普通用户对**本人**单据同样具备 `ticket:update`（生命周期守卫=创建人/受理人/管理员），可作为可选复核路径。

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 用 `admin-custa1` 登录 → 打开 A-1 详情 | 状态=「等待中」 |
| 2 | 点「编辑」→ 状态改为「已解决」→（若界面出现「解决方案」字段则填写）→ 保存 | 成功提示；状态徽标=「已解决」 |
| 3 | **记录实际请求**（Network） | ⚠️ 当前界面路径可能走 `PUT /tickets/:id`（直接改状态）；受保护口径为 `POST /tickets/:id/resolve`（需解决方案）。两者差异作为**发现登记**，不阻断闭环判定 |
| 4 | 接口复核（可选，DevTools Console，非主通道） | `POST /api/v1/tickets/<id>/resolve`（带 CSRF 与 `{resolution:'...'}`）→ 200；详情 `status=resolved` |
| 5 | （可选）客户侧「关闭」 | 状态=「已关闭」（终态） |

### 6.6 服务商侧终态核对与审计

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 回到 `prov.agent1` 工作台刷新 | 该单状态同步为「已解决/已关闭」；行内动作按 `allowedActions` 收敛（终态不可再操作） |
| 2 | `admin-mspt1` → **审计看板**（`/msp/audit`） | 统计窗口与分布可见；明细含本次 `workbench.action`（回复/改状态/指派）与拒绝事件 |
| 3 | 平台 `admin` → `/audit-logs` 筛 `action=workbench.action` | 可见对应记录（如需按目标租户精确筛选，用 Console：`/api/v1/audit-logs?targetTenantId=<CUSTA1 id>`） |

### 6.7 深度切换（对照路径，验证边界）

| # | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | `prov.agent1` → 顶栏「客户过滤器」→ `CUSTA1` 行「进入客户」→ 确认 | 跳转 `/dashboard`；顶栏出现「当前客户：CUSTA1（深度操作中）」+「返回工作台」 |
| 2 | 打开「工单管理」 | 显示 CUSTA1 的工单（数据限定目标租户） |
| 3 | 深度态下访问 `/msp/workbench` | 403/不可用（设计如此：工作台只在 provider 作用域） |
| 4 | 点「返回工作台」 | 回到 provider 作用域；`/auth/me` 的 `tenantId` 回到 MSPT1、`homeTenantId` 始终为 MSPT1 |
| 5 | 客户管理员 `admin-custa1` → `/admin/users` | 出现「服务方成员（只读）」区块（服务商员工带标签、无管理动作、不计入分页 total） |

### 6.8 阶段 6 判据（逐项打勾）

- [ ] 分配 2 条可见；重复分配 409；
- [ ] 工作台仅见已分配客户；未分配客户 4 通道拒绝（头/路径/请求体/切换）；
- [ ] 回复客户侧可见；状态 `open→in_progress→pending` 双向一致；
- [ ] 指派处理人正确（当前技术员）；
- [ ] 客户管理员确认解决后状态 `resolved`（记录实际端点与差异）；
- [ ] 服务商侧终态可见且动作收敛；
- [ ] 审计可查（工作台动作 + 拒绝事件）；
- [ ] 深度切换/返回语义与只读服务方成员符合预期。

---

## 7. 阶段 7 · 其它模拟操作（选做）

> **执行状态（2026-10-12）**：§7.1–§7.3 已实测（TUM 30 PASS/2 SKIP + 浏览器写路径；离职编排 10 PASS；邀请受理通道浏览器 E2E）。执行注记：① TUM 验收脚本在 `saas_msp` 下自动以 `msp_customer` + `mspProviderId` 建隔离租户；② 邀请链接取后端 `APP_BASE_URL`（默认已对齐本地 dev `http://localhost:3000`；生产按部署显式设置——2026-10-12 OB-2 收口，见部署文档 02 §3）；③ 受邀用户登录名为邮箱本地部分（如 `custa.invitee1`），非邮箱全称（落地页/邀请弹窗已补提示）；④ 平台审计视图不含租户域邀请审计行（行归属 actor 家租户），请在租户管理员视图查看。**追加（同日第二轮）**：§7.1 R4（写通道关闭回滚演练）单跑 PASS（10/10 + 恢复 6/6）；§7.3 批量护栏浏览器实测 PASS（勾选 3 → 生效 2 + 自动排除 1 → 逐条 成功 2/失败 0）；§7.4 跨租户读面已记录（待办任务 + 变更/问题/服务请求，空态渲染）。详见 [run-summary §R3/§R4](../evidence/fresh-db-browser-B1/run-summary-20261012-1925.md)。

### 7.1 平台侧租户用户治理（TUM；需 `TENANT_USER_ADMIN_ENABLED=true`）

| # | 操作 | 预期 |
|---|---|---|
| 1 | 平台 `admin` → `/admin/tenants` → `CUSTA1` 行「用户管理」 | 抽屉打开（深链 `?tenantUsers=<id>` 可复现） |
| 2 | 搜索 `custa.user1` →「重置密码」→ 一次性口令 → 复制并勾选"已保存" | 口令仅显示一次；该用户出现「待改密」徽标 |
| 3 | 用一次性口令登录 | 强制改密页 → 改密后进入业务页；旧口令/旧会话失效 |
| 4 | 行内「强制下线」 | 该用户旧会话 401 |
| 5 | 行内状态开关「停用」 | 该用户无法登录；审计 `user.admin_status` |
| 6 | 护栏反例：对租户最后一个可用管理员停用 | 开关禁用/409 `LAST_ADMIN_PROTECTED` |
| 7 | 审计：`/admin/audit` 或 Console 按 `targetTenantId`/`targetUserId` 查询 | 可见重置/启停/强下线记录 |

> R4 反例（写通道关闭）：以 `TENANT_USER_ADMIN_ENABLED=false` 重启实例后单跑
> `scripts/msp/acceptance/run-tenant-user-admin-r4-probe.ps1 -Mode disabled`（随后 `-Mode restore` 还原口令与写通道）——2026-10-12 实测 10/10 + 6/6 PASS。

### 7.2 离职编排

| # | 操作 | 预期 |
|---|---|---|
| 1 | `admin-mspt1` → `/admin/users` → 目标员工行「离职（停用并撤销全部分配）」→ 确认 | 提示「已撤销 N 个分配」；状态=停用；旧会话 401 |
| 2 | 再次点击「离职」 | 幂等：0 回收、无新增副作用（审计各 1 行） |
| 3 | （平台通道）`admin` → 用户管理 → 目标用户「离职」 | 成功提示；分配清零；平台审计留痕 |
| 4 | 反例：服务商管理员调用平台离职端点 | 403 `PLATFORM_SCOPE_REQUIRED` |

### 7.3 批量护栏

勾选 ≥2 条工单 →「批量回复」→ 客户分布确认 → 提交 → 逐条结果（成功/失败分列）；含不可操作条目时，失败条目给原因，成功条目正常生效。

> **执行状态（2026-10-12）**：PASS——勾选 3 条（2 新建 + 1 已解决）→ 客户分布确认「生效 2 条」+「1 条所选条目无此操作权限，已自动排除」→ 逐条结果 成功 2 / 失败 0；评论落库 + MSP 审计看板可见（详见 run-summary §R4）。

### 7.4 其它单据读面（按已落地范围）

工作台切换工单/待办任务/跨租户单据页签：查看问题、变更、服务请求的读面与状态推进能力（按目标版本实际渲染为准；审批类动作保留在客户治理面）。记录实际可见范围与操作结果。

> **执行状态（2026-10-12）**：读面已记录——「待办任务」与「跨租户单据（变更/问题/服务请求）」页签/列头渲染正常；本批次无问题/变更/SR 样本（空态）；审批类动作保留在客户治理面。

---

## 8. 证据与归档

### 8.1 目录与命名

```text
docs/multi-tenant/evidence/fresh-db-browser-<批次>/
├── run-summary-<ts>.md          # 每阶段 PASS/FAIL/SKIP + 证据指针 + 环境指纹
├── stage0/                      # 部署校验（健康检查、msp/status、日志摘录）
├── stage1/                      # 租户列表/详情截图
├── stage2/ … stage6/            # 各阶段关键截图
├── network/                     # 负向场景 Network 截图/导出（含状态码与 reasonCode）
└── sql/                         # 可选：只读抽查 SQL 与结果
```

命名建议：`<stage>-<seq>-<desc>.png`，例如 `stage5-03-n3-403-MSP_ALLOCATION_REQUIRED.png`。

> **截图约定（OB-9，2026-10-12 收口）**：证据以**文本快照 + Network/日志 + API 回放**为主通道（全部入库）；截图按仓库约定**本地留存、不入库**（`.gitignore:122` 忽略 `*.png`，仓库无受跟踪 PNG）。
> 自动化截图两条路径：① MCP 截图工具可用时优先（注意受工作区根限制）；② 受限时用 **CDP 直采**兜底（Edge `--remote-debugging-port` + `Page.captureScreenshot`），落盘目录自定（如 `%TEMP%\itsm-evidence\`），并在 run-summary 记录文件名与来源。

### 8.2 run-summary 模板（头部）

```markdown
# 全新库多租户浏览器验收运行摘要（批次 FDB-B1）
- 新库：itsm_msp_fdb ｜ 后端：http://127.0.0.1:8090（saas_msp）｜ 前端：http://localhost:3000
- commit：<hash> ｜ 迁移账本：applied=<N> pending=<N> ｜ 执行人：<name> ｜ 时间窗：<start>–<end>
- 结果：PASS=<n> FAIL=<n> SKIP=<n>
- 关键发现：<列表，含已知限制/差异>
```

---

## 9. 排错

| 现象 | 排查 |
|---|---|
| 后端 8090 拒绝连接 | 后端未启动/已退出；隧道是否在跑（`.\.dev\dev.ps1 status`）；查看 `.dev\backend-fdb.err.log` |
| `/api/v1/msp/*` 全部 404 | `DEPLOYMENT_MODE` 不是 `saas_msp`（检查启动会话环境变量与日志 `deployment gate resolved`） |
| `readyz` 503 | 初始化组件未全部 succeeded / 迁移未到最新；回 §0.3 看日志与 `migrate -status` |
| 登录 401 / 无 MSP 菜单 | provider 租户账号缺 `msp_role`（如经租户通道建号）——用平台通道重建或补角色 |
| 工作台为空 | 无有效分配（`deallocated_at` 非空）；或过滤器选中了无关客户 |
| 建单类型为空 | 该客户租户尚未建类型（模板 7 项不含工单类型）——先建类型/从预设安装 |
| 请求 429/403"过于频繁" | 登录限流；调高 `LOGIN_RATE_LIMIT_PER_MIN` 并重启后端（记录变更） |
| 页面/接口 403（负向场景） | 属预期；核对 `reasonCode`（`MSP_ALLOCATION_REQUIRED` / `TENANT_MISMATCH_REJECTED` / `PLATFORM_SCOPE_REQUIRED` / `TENANT_USER_ADMIN_DISABLED`） |
| 改了 `.env` 但后端仍连旧库 | 宿主环境变量优先于 godotenv；启动会话必须显式设置 `DB_*`/`REDIS_*`（本手册 §0.3/§0.4 方式） |
| 隧道断开 | `.dev\tunnel-window.ps1` 自动重连；必要时 `.\.dev\dev.ps1 restart`（会重启后端，注意后端指向） |
| 方案 B：Docker 拉镜像超时 | 配置 registry-mirrors 或换 `*_IMAGE` 变量；确认 Docker Desktop 已启动 |

---

## 10. 重置与重跑

1. **换批次重跑**：使用新编码（`MSPT2/CUSTA2/CUSTB2`、`prov.agent2` 等）与新的样本标题前缀 `[FDB-B2]`；不清理旧批次数据。
2. **重建新库**（仅当需要干净库时）：新建另一个库名（如 `itsm_msp_fdb2`），重复 §0.2–§0.4；**不要**删除 `itsm_prod`。
3. **回收测试库**（谨慎）：确认库名无误后再执行
   `ssh -o StrictHostKeyChecking=accept-new n9e@172.18.3.238 'echo passw0rd | sudo -S -p "" docker exec itsm-postgres-prod psql -U itsm -d postgres -c "DROP DATABASE itsm_msp_fdb"'`
   ——执行前必须二次核对库名（`itsm_msp_fdb`），并确认无业务仍在使用。
4. **恢复原环境**：停止测试后端进程，改用 `.\.dev\dev.ps1 backend` 启动（其导入 `itsm-backend\.env`，指向 `itsm_prod`）；本手册全程未修改 `.env`，无需回滚文件。

---

## 11. 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v0.1 | 2026-10-12 | 首版草案：新库准备（方案 A/B）、阶段 1–7 浏览器逐步操作、权限三层与负向清单、证据归档、排错与重置；配套计划文档（12） |
| v0.2 | 2026-10-12 | 评审修订：`end_user` 权限与解决路径口径修正（本人单据可改状态/解决）；界面文案对齐（最终用户 / 服务商技术员）；ssh 首次连接加 `StrictHostKeyChecking=accept-new`；新增远程主机可达性检查；补「其它业务单据」可选步骤（§4.5） |
| v0.3 | 2026-10-12 | **首跑执行回填（批次 FDB-B1）**：§0–§6 逐步可执行性实测；MSP 通道 B 实测 403（F1）；确认解决实际走 `PUT /tickets/:id`（F2）；G4/G5 规避命令有效性验证；详见 run-summary |
| v0.4 | 2026-10-12 | **阶段 7 执行回填**：§7.1–§7.3 实测记录（TUM/离职/邀请通道 + TUM 浏览器写路径）；补执行注记（saas_msp 租户类型自适应、邀请链接端口配置、受邀用户名形态、租户域审计视图）；观察项 O1–O3 见 run-summary §R3 |
| v0.5 | 2026-10-12 | **R4 + §7.3/§7.4 执行回填**：TUM R4（写通道关闭）单跑 PASS（探针脚本 `run-tenant-user-admin-r4-probe.ps1` disabled/restore 双模式）；§7.3 批量护栏浏览器实测（自动排除无权限条目 + 逐条结果）；§7.4 跨租户读面记录；O4 观察项（平台审计 API 与 MSP 看板口径差异） |
