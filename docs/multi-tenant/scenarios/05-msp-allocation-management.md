# 场景 05 · MSP 分配管理（授权员工到客户）

> **状态**：当前（2026-10-05 实测；含分配列表可见性修复）｜**定位**：服务商管理员把员工授权到客户租户——跨客户访问的**唯一授权载体**
> **读者**：服务商管理员、测试
> **前置**：[场景 04 §2](./04-user-provisioning.md) 已创建服务商员工（`provider_agent` / `provider_admin`）；客户租户已存在

---

## 1. 场景目标与验收判据

| 判据 | 通过标准 |
|---|---|
| 分配可建 | 选择员工 + 客户 + 角色提交成功 |
| 列表可核 | **修复后**：`provider_admin` 能看到本服务商**全部**有效分配（此前只看到自己的，管理页会"永远为空"） |
| 授权生效 | 被分配员工可跨客户读取/处理该客户工单（工作台可见该客户数据） |
| 未分配拒绝 | 未分配客户在头/路径/请求体/切换 4 通道访问 → **403 `MSP_ALLOCATION_REQUIRED`** |
| 可解除 | 「解除」后该员工不再看到该客户（软删，保留历史） |

## 2. 操作步骤（浏览器）

| 步骤 | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 服务商管理员登录 → 左侧「分配管理」或直接打开 `/msp/management` | 页面标题 **MSP 分配管理**；顶部有「为客户建号」「新建分配」按钮；说明文案提示"分配后员工可通过 `X-Customer-Tenant-ID` 访问对应客户工单" |
| 2 | 点击 **「新建分配」** | 弹窗 **创建 MSP 分配**：**员工**（下拉）、**客户**（下拉）、**分配角色**（`主支持`/`备份`/`专家`，以界面为准） |
| 3 | 选择：员工=`b2c_tech`（技术员）；客户=`B2CCUST02`；角色=`主支持` | 下拉可搜索；客户列表来自该服务商名下客户 |
| 4 | 提交 | 提示「分配创建成功」；弹窗关闭；**列表出现新行**（员工/客户/角色/分配时间） |
| 5 | 再建一条：员工=`b2cprov_admin`（自己）→ 同客户 | 列表共 2 行（实测） |
| 6 | （回收）对某行点击 **「解除」** | 弹出确认框「确认解除分配」→ 确认后提示「分配已解除」，行消失（历史保留 `deassigned_at`） |

> 实测（2026-10-05）：创建 2 条 `primary` 分配（技术员 + 服务商管理员 → B2CCUST），管理页列表可见 2 行；此前版本因 `ListForCaller` 缺失恒为空，已修复。

## 3. 验证

**界面**
- 列表：2 行，角色显示「主支持」（`primary`）。
- 用 **agent 账号**（`b2c_tech`）登录并在顶栏客户过滤器中选择该客户 → 工作台/列表能看到该客户数据（见 [07](./07-msp-workbench-collaboration.md)）。
- 用未分配员工账号访问同一客户 → 界面提示无权限/空态；对应网络请求返回 403。

**接口**

```js
// 服务商管理员：分配列表（修复后返回团队全部有效分配）
fetch('/api/v1/msp/allocations', { credentials:'include' })
  .then(r => r.json()).then(d => console.log(d.data.allocations));
// 预期：包含 { mspUserId, customerTenantId, role: "primary", ... } 的目标行
```

```js
// agent：带目标客户头访问其被分配的客户 → 200
fetch('/api/v1/msp/workbench/tickets?limit=5', {
  credentials:'include', headers:{ 'X-Customer-Tenant-ID': '<客户租户id>' }
}).then(r => r.status);   // 200

// agent：访问未分配客户 → 403，响应体含 MSP_ALLOCATION_REQUIRED
fetch('/api/v1/msp/workbench/tickets?limit=5', {
  credentials:'include', headers:{ 'X-Customer-Tenant-ID': '<未分配客户id>' }
}).then(async r => console.log(r.status, await r.text()));
```

**数据库（可选）**

```sql
SELECT a.*, u.username FROM msp_allocations a
JOIN users u ON u.id = a.msp_user_id
WHERE a.customer_tenant_id = (SELECT id FROM tenants WHERE code='B2CCUST02')
ORDER BY a.id;
-- deassigned_at IS NULL 视为有效
```

## 4. 边界与反例

| 反例 | 操作 | 预期 |
|---|---|---|
| 重复分配 | 同一员工+同一客户再次创建 | 业务拒绝（已有有效分配）或幂等提示（以现网为准，建议重跑前先看列表） |
| 跨服务商分配 | 用服务商 A 的账号试图分配其员工到服务商 B 的客户 | 创建被拒绝（`allocation.provider_tenant_id` 必须匹配客户 `msp_provider_id`） |
| 无权限建分配 | `provider_agent` 打开 `新建分配` | 端点级 RBAC 拒绝（403）；agent 仅能查看**自己的**分配 |
| 解除后仍访问 | 解除后 agent 再带该客户头访问 | 403 `MSP_ALLOCATION_REQUIRED`（软删立即生效） |

## 5. 排错

| 现象 | 处置 |
|---|---|
| 「分配管理」页面空白/无入口 | 当前账号不是 `provider_admin`（需 `msp_allocation:read`）；或后端非 `saas_msp` |
| 员工下拉为空 | 该服务商租户下还没有员工账号 → 先做 [04](./04-user-provisioning.md) |
| 客户下拉为空 | 尚无「MSP客户」租户（做 [02](./02-platform-create-customer-tenant.md)）；或当前账号非 provider_admin |
| agent 工作台看不到客户 | 检查分配是否有效（`deassigned_at IS NULL`）、是否选对客户过滤器 |

## 6. 下一步

→ [场景 06 · 客户建工单类型与工单](./06-customer-ticket-type-and-ticket.md)
