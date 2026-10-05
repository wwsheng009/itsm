# 场景 08 · 审计看板 + 隔离负向验证（403 / 401 / 404）

> **状态**：当前（2026-10-05 实测；含审计空集崩溃修复）｜**定位**：证明"该拒绝的都拒绝、拒绝都有痕迹"
> **读者**：服务商管理员、测试/QA、安全评审
> **前置**：[场景 05](./05-msp-allocation-management.md) 已有分配数据；[场景 07](./07-msp-workbench-collaboration.md) 产生过协作操作

---

## 1. 审计看板（`/msp/audit`）

| 步骤 | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 服务商管理员登录 → **审计看板**（`/msp/audit`） | 页面加载：**统计窗口**（近 7 / 30 / 90 天）、三张统计卡（拒绝 / 跨租户 / 总数）、来源分布、动作分布、客户分布、**最近拒绝明细** |
| 2 | 切换「统计窗口」 | 触发重拉，数字随窗口变化 |
| 3 | 制造一条拒绝（见 §2），回到本页刷新 | 明细/分布中出现对应条目（动作如 `tenant.scope_denied` / `tenant.probe_denied` / `workbench.action`） |

> **回归修复（2026-10-05）**：当窗口内没有告警时，后端返回 `recentDenials: null`，旧版前端在 `null.length` 上抛异常导致**整页白屏**。现已兜底为空数组并加回归用例；空集时应显示"最近 0 条"与空态文案，页面不崩。

## 2. 负向验证矩阵（逐条可复现）

| # | 场景 | 操作（服务商/客户账号） | 预期结果 | 独立验证 |
|---|---|---|---|---|
| N1 | **未分配客户**（头通道） | agent 已登录，请求头带未分配客户的 `X-Customer-Tenant-ID` 调 `/msp/workbench/tickets` | **403**，错误码 `MSP_ALLOCATION_REQUIRED`；审计出现拒绝事件 | 见 §3-① |
| N2 | **未分配客户**（路径/请求体通道） | 调 `/msp/customers/<未分配客户id>/tickets`；或工作台批量请求体塞入该客户条目 | 路径：403；批量：该条目失败并给出原因，其余合法条目不受影响 | 见 §3-② |
| N3 | **头与登录租户冲突** | 用客户 A 的登录态请求头携带 `X-Tenant-Code: <客户B编码>` 调 `/users` 等业务接口 | **401**（`TENANT_MISMATCH_REJECTED`），并产生告警事件；不按任一租户静默放行 | 见 §3-③ |
| N4 | **private 模式不可达** | 以 `DEPLOYMENT_MODE=private` 重启后端，访问 `/api/v1/msp/status`（带平台登录态） | **404**（MSP 整族关闭；不带登录态遇到 401 亦属不可达，以带登录态复核 404 为准） | 见 §3-④ |
| N5 | **租户目录收敛** | provider_admin 调 `GET /tenants`；再调直属客户以外租户的 `GET /tenants/:id` | 列表=本租户+直属客户；跨作用域详情 **404**（隐藏存在性）；平台管理员仍全量 | 见 §3-⑤ |
| N6 | **客户与 MSP 面隔离** | 客户账号访问 `/msp/*` 页面/接口 | 页面 403/无入口；接口 403 | DevTools Network 复核 |

> 说明：N1–N3、N5 的修复与用例见实施方案修订记录 v1.69（对应提交 `ed4e1ab9`）；N4 为部署模式口径，gate 记录见 `02-deployment-and-configuration.md`。

## 3. 复现命令（PowerShell，独立于浏览器）

```powershell
$base = 'http://127.0.0.1:8090/api/v1'

function Login([string]$u,[string]$p){
  $s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  Invoke-RestMethod -Method Post -Uri "$base/auth/login" -WebSession $s `
    -ContentType 'application/json' -Body (@{username=$u;password=$p}|ConvertTo-Json) | Out-Null
  return $s
}
```

**① 未分配客户（头通道）→ 403**

```powershell
$s = Login 'b2c_tech' '<口令>'
try {
  Invoke-WebRequest -Uri "$base/msp/workbench/tickets?limit=5" -WebSession $s `
    -Headers @{ 'X-Customer-Tenant-ID' = '<未分配客户id>' } | Out-Null
  'UNEXPECTED-200'
} catch {
  $r = $_.Exception.Response; "status=$([int]$r.StatusCode)"
  (New-Object IO.StreamReader($r.GetResponseStream())).ReadToEnd()   # 期望含 MSP_ALLOCATION_REQUIRED
}
```

**② 未分配客户（路径通道）→ 403**

```powershell
Invoke-WebRequest -Uri "$base/msp/customers/<未分配客户id>/tickets" -WebSession $s | Out-Null  # 期望 403
```

**③ 头与登录租户冲突 → 401 + 告警**

```powershell
$c = Login 'b2ccust_admin' '<口令>'   # 客户A 登录态
try {
  Invoke-WebRequest -Uri "$base/users?page=1&pageSize=5" -WebSession $c `
    -Headers @{ 'X-Tenant-Code' = '<客户B编码>' } | Out-Null
  'UNEXPECTED-200'
} catch { "status=$([int]$_.Exception.Response.StatusCode)" }   # 期望 401
# 随后在 /msp/audit 或审计 API 中观察 tenant.probe_denied / TENANT_MISMATCH_REJECTED
```

**④ private 模式 → 404**

```powershell
# 重启后端时设置： $env:DEPLOYMENT_MODE='private'; 其余同上
Invoke-WebRequest -Uri "$base/msp/status" -WebSession (Login 'admin' 'admin123') | Out-Null  # 期望 404
# 复核完务必重启回 saas_msp（见 00 §2 / §7）
```

**⑤ 租户目录收敛**

```powershell
$p = Login 'b2cprov_admin' '<口令>'
(Invoke-RestMethod -Uri "$base/tenants?page=1&pageSize=50" -WebSession $p).data.items |
  Select-Object id, code, type        # 期望：本租户 + 直属客户（不含无关租户）
try {
  Invoke-WebRequest -Uri "$base/tenants/<无关租户id>" -WebSession $p | Out-Null
  'UNEXPECTED-200'
} catch { "status=$([int]$_.Exception.Response.StatusCode)" }   # 期望 404
```

## 4. 判定标准（把"预期"变"可验收"）

| 项 | 判定 |
|---|---|
| 拒绝码稳定 | 403 必须含 `MSP_ALLOCATION_REQUIRED`（未分配）；401 冲突场景含 `TENANT_MISMATCH_REJECTED` |
| 无静默放行 | 冲突头不得被忽略后按 JWT 租户返回 200 |
| 不泄露存在性 | 跨作用域详情返回 404 而非 403（避免暴露"存在但无权"） |
| 留痕 | 每次拒绝可在审计看板/审计接口找到对应事件（来源/动作/客户维度） |
| 可恢复 | 完成分配后同一请求由 403 变 200（无需重启服务） |

## 5. 排错

| 现象 | 处置 |
|---|---|
| 审计页空白/历史白屏 | 已修复；若复现请确认前端为最新版本，并检查 `recentDenials` 是否被非数组值污染 |
| 403 但审计无记录 | 确认后端 `LOG_LEVEL` 与审计写入开关；拒绝事件写入是强审计路径 |
| private 模式仍能打开 MSP 页面 | 后端未重启或 `DEPLOYMENT_MODE` 未生效；重启并复核 `/msp/status` 404 |

## 6. 收官检查单（全链路）

- [ ] 01–02：两类租户创建且类型/归属正确；
- [ ] 03：readiness 就绪 + 首管登录 + 强制改密；
- [ ] 04：至少一个服务商员工建号成功；
- [ ] 05：分配 2 条可见；agent 跨客户 200、未分配 403；
- [ ] 06：客户建成工单，编号生成；
- [ ] 07：回复客户可见、状态同步、批量逐条结果；
- [ ] 08：审计可见、上述负向 1–5 全通过。

> 完成后建议把本次结果追加到本目录一页"运行记录"（可复制本页表格），或直接引用 `plan/msp-multi-tenant-business-acceptance-design.md` 的脚本 `/ evidence` 机制留档。
