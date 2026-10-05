# 场景 00 · 环境与账号准备（执行一切场景之前）

> **状态**：当前（2026-10-05 实测）｜**定位**：把本机环境、账号、验证工具准备好；后续场景都从这里出发
> **读者**：测试/QA、实施交付、联调研发
> **所属**：[scenarios/README.md](./README.md)

---

## 1. 服务与端口

| 组件 | 地址 | 说明 |
|---|---|---|
| 前端（Vite Dev） | `http://localhost:3000` | 浏览器操作入口；登录页 `/login` |
| 后端 API | `http://127.0.0.1:8090/api/v1` | 健康检查 `/healthz`、就绪 `/readyz` |
| PostgreSQL | `127.0.0.1:15433`，库 `itsm_prod` | 连接参数见 `itsm-backend/.env`（勿提交真实口令） |

## 2. 启动后端（`saas_msp` 模式）

```powershell
cd E:\projects\itsm\itsm-backend
go build -o main.exe .
$env:DEPLOYMENT_MODE = 'saas_msp'                 # 必须：否则 /api/v1/msp/* 整族 404
$env:USER_PROVISIONING_CHANNELS_ENABLED = 'true'  # 跨租户建号通道开关
$env:RLS_MODE = 'off'                             # 演练用；灰度期可为 shadow / enforce
$env:LOG_LEVEL = 'info'
.\main.exe
```

启动成功的判定：

```powershell
(Invoke-WebRequest http://127.0.0.1:8090/api/v1/healthz -UseBasicParsing).StatusCode   # 200
(Invoke-WebRequest http://127.0.0.1:8090/api/v1/readyz  -UseBasicParsing).StatusCode   # 200
```

> 日志里应出现租户守卫通过（`tenant_guard: pass`）与运行模式（`saas_msp`）。若 `/msp/status` 返回 404，先检查 `DEPLOYMENT_MODE`。

## 3. 启动前端

```powershell
cd E:\projects\itsm\itsm-frontend
npm install        # 首次
npm run dev        # 打开 http://localhost:3000
```

## 4. 账号矩阵与口令规则

| 账号 | 角色 | 口令来源 | 备注 |
|---|---|---|---|
| `admin` | 平台管理员（`super_admin`） | 本地开发缺省 `admin123`（`docs/install.md`） | 生产环境禁止使用缺省口令 |
| `admin-<租户编码>` | 新租户首个管理员 | 开通向导**一次性生成**、仅回显一次 | 首登强制改密；账号名可在向导中自定义 |
| 服务商员工 | `provider_admin` / `provider_agent` | 建号时人工设置初始口令 | provider 租户建号**必须**选择 MSP 角色 |
| 客户方用户 | 客户租户内角色 | 建号初始口令或邀请受理设置 | 邀请落地页 `/invite/<token>` |

> 首管口令策略：向导中「密码」留空 → 服务端生成 12–128 位强口令；**只显示一次**，请当场复制保存。

## 5. 数据库直查（可选但推荐）

```powershell
# 口令从 itsm-backend/.env 的 DB_PASSWORD 读取
$env:PGPASSWORD = (Select-String -Path itsm-backend\.env -Pattern '^DB_PASSWORD=').Line.Split('=')[1]
psql -h 127.0.0.1 -p 15433 -U itsm -d itsm_prod
```

常用 SQL（把 `MSPB2C`/`B2CCUST` 换成你的编码）：

```sql
-- 租户与类型
SELECT id, code, name, type, status, parent_tenant_id, msp_provider_id
FROM tenants WHERE code IN ('MSPB2C','B2CCUST');

-- 服务商员工身份
SELECT id, username, role, msp_role FROM users WHERE username IN ('b2cprov_admin','b2c_tech');

-- 有效分配
SELECT a.id, u.username, c.code AS customer_code, a.role,
       a.deassigned_at IS NULL AS active
FROM msp_allocations a
JOIN users u   ON u.id = a.msp_user_id
JOIN tenants c ON c.id = a.customer_tenant_id
ORDER BY a.id DESC LIMIT 20;

-- 工单
SELECT id, ticket_number, title, status, tenant_id
FROM tickets WHERE tenant_id = (SELECT id FROM tenants WHERE code = 'B2CCUST')
ORDER BY id DESC LIMIT 10;
```

## 6. 通用接口验证手法（PowerShell）

```powershell
$base = 'http://127.0.0.1:8090/api/v1'
$s = New-Object Microsoft.PowerShell.Commands.WebRequestSession

# 1) 登录（示例：服务商管理员）
Invoke-RestMethod -Method Post -Uri "$base/auth/login" -WebSession $s `
  -ContentType 'application/json' `
  -Body '{"username":"b2cprov_admin","password":"<口令>"}' | Out-Null

# 2) 已登录后调用业务接口（示例：分配列表）
Invoke-RestMethod -Uri "$base/msp/allocations" -WebSession $s | ConvertTo-Json -Depth 6

# 3) 跨客户访问（示例：带目标客户租户头）
Invoke-RestMethod -Uri "$base/msp/workbench/tickets" -WebSession $s `
  -Headers @{ 'X-Customer-Tenant-ID' = '<客户租户id>' } | ConvertTo-Json -Depth 4
```

**浏览器侧同源技巧**：已在页面登录时，直接在地址栏输入 `http://localhost:3000/api/v1/...` 不可用——请用 DevTools Console 执行 `fetch('/api/v1/...', {credentials:'include'})`，或查看 Network 面板核对请求头（如 `X-Customer-Tenant-ID`）与响应码。

## 7. 重置与重跑

- **推荐**：换新编码（`MSPB2C02`/`B2CCUST02`）重新走 01→08，不清理旧数据（保留证据）。
- 需要回收时：在「分配管理」逐条**解除**分配；租户/用户建议保留或置为暂停，**不要删除 `default` 平台根租户**。
- 重跑前确认后端环境变量仍为 `saas_msp`；切换过 `private` 演练后**必须重启**回 `saas_msp`。

## 8. 常见问题

| 现象 | 排查 |
|---|---|
| 页面 3000 打不开 | 前端 `npm run dev` 是否在跑；端口是否被占用 |
| 登录 401/无 MSP 菜单 | 后端是否为 `saas_msp`；账号是否为目标租户身份 |
| 开通向导停在"待供给" | 见 [03 §6](./03-tenant-provisioning-first-admin.md)（超时/重试/CLI 供给） |
| 客户侧建单"类型为空" | 模板不含工单类型，先按 [06](./06-customer-ticket-type-and-ticket.md) 建类型 |
| 新员工登录后权限为空 | 见 [README §6-1](./README.md)（建号通道 membership 限制） |
