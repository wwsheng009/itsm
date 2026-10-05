# 场景 02 · 平台新建客户租户并绑定服务商

> **状态**：当前（2026-10-05 实测）｜**定位**：平台管理员创建 `msp_customer` 租户，并绑定唯一的服务商
> **读者**：平台管理员、实施交付、测试
> **前置**：[场景 01](./01-platform-create-provider-tenant.md) 已完成（存在至少一家 `msp_provider`）；平台管理员登录

---

## 1. 场景目标与验收判据

| 判据 | 通过标准 |
|---|---|
| 租户存在 | `/admin/tenants` 列表出现新租户，类型 **MSP客户** |
| 归属正确 | 详情显示所属「MSP 服务商」= 场景 01 创建的服务商 |
| 服务商可见 | 服务商管理员登录后，在 MSP 客户目录/分配下拉中能看到该客户 |

## 2. 操作步骤（浏览器）

| 步骤 | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 平台账号登录，进入 **租户管理**（`/admin/tenants`）→ **「新建租户」** | 打开新建表单 |
| 2 | 填写：租户名称=`<客户名称>`；租户编码=`B2CCUST02` | 实时校验 |
| 3 | 租户类型选择 **「MSP客户」** | **出现**「MSP 服务商」下拉（必填） |
| 4 | 打开「MSP 服务商」下拉，选择场景 01 的服务商（显示为 `名称（编码）`） | 下拉可搜索；选中后回显 |
| 5 | 状态保持 **「活跃」**；其余留空；点击 **「保存」** | 列表出现新客户租户；类型列 `MSP客户` |

> 实测（2026-10-05）：`B2CCUST`（id=8）创建成功，`msp_provider_id=7`（MSPB2C）。

## 3. 验证

**界面**
- 列表行打开「查看」：字段包含「MSP 服务商」且值为目标服务商。
- 用**服务商管理员**登录：`分配管理 → 新建分配` 的「客户」下拉中可见该客户；MSP 客户目录接口同样返回该客户（provider_admin 视角）。

**接口**

```js
// 平台账号：确认客户租户归属
fetch('/api/v1/tenants?keyword=B2CCUST02', { credentials:'include' })
  .then(r => r.json()).then(d => console.log(d.data.items[0]));
// 预期字段：type: "msp_customer"；mspProviderId: <服务商租户id>
```

```js
// 服务商管理员账号：确认客户目录可见（provider_admin）
fetch('/api/v1/msp/customers', { credentials:'include' })
  .then(r => r.json()).then(d => console.log(d.data.customers.map(c => c.code)));
```

**数据库（可选）**

```sql
SELECT id, code, name, type, msp_provider_id
FROM tenants WHERE code = 'B2CCUST02';   -- msp_provider_id 应指向 MSPB2C02 的 id
```

## 4. 边界与反例

| 反例 | 操作 | 预期 |
|---|---|---|
| 未选服务商 | 类型选「MSP客户」但「MSP 服务商」留空 → 保存 | 表单校验拒绝（必填），无法提交 |
| 切走类型 | 先选「MSP客户」并选服务商，再切回「MSP服务商」 | 服务商字段被清空且不提交（避免脏字段） |
| 服务商是暂停/过期状态 | 选择非活跃服务商 | 允许保存但后续使用受限；运营上建议仅选择活跃服务商 |

## 5. 排错

| 现象 | 处置 |
|---|---|
| 下拉为空 | 尚无 `msp_provider` 租户（先做场景 01），或当前账号非平台管理员 |
| 服务商管理员看不到新客户 | 用 `provider_admin` 账号查看；`provider_agent` 只看到被分配的客户（设计如此） |
| 客户侧登录后 MSP 相关入口不可见 | 属预期：客户租户看不到 MSP 管理面 |

## 6. 下一步

→ [场景 03 · 开通向导：模板供给 → 首个管理员 → 完成](./03-tenant-provisioning-first-admin.md)
