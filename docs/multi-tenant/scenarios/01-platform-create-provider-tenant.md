# 场景 01 · 平台新建服务商（MSP）租户

> **状态**：当前（2026-10-05 实测）｜**定位**：平台管理员在浏览器创建 `msp_provider` 租户（一家服务商）
> **读者**：平台管理员、实施交付、测试
> **前置**：[00 环境与账号](./00-environment-and-accounts.md)；以**平台管理员**（`admin`）登录

---

## 1. 场景目标与验收判据

| 判据 | 通过标准 |
|---|---|
| 租户存在 | `/admin/tenants` 列表出现新租户，类型显示为 **MSP服务商** |
| 类型正确 | 详情/接口 `type = msp_provider`，状态 `active` |
| 服务商可用 | 在场景 02 中，该租户可作为「MSP 服务商」下拉选项被选择 |

## 2. 操作步骤（浏览器）

| 步骤 | 操作 | 预期界面反馈 |
|---|---|---|
| 1 | 打开 `http://localhost:3000/login`，输入平台账号，登录 | 进入管理端；顶栏可见「租户管理」入口 |
| 2 | 进入 **租户管理**（`/admin/tenants`） | 租户列表；右上角有「新建租户」按钮 |
| 3 | 点击 **「新建租户」** | 弹出表单：租户名称 / 租户编码 / 域名 / 租户类型 / 状态 / 到期时间 / 配额（用户上限、月工单上限、存储上限） |
| 4 | 填写：租户名称=`<你的服务商名>`；租户编码=`MSPB2C02`（大写字母/数字/下划线/连字符，字母或数字开头） | 输入即时校验，编码格式不符会红字提示 |
| 5 | 租户类型选择 **「MSP服务商」**；状态选 **「活跃」** | 类型选择后**不出现**「MSP 服务商」下拉（该下拉仅 `MSP客户` 类型需要） |
| 6 | 其余留空（配额 0/空 = 不限）；点击 **「保存」** | 弹窗关闭；列表刷新出现新租户；类型列显示 `MSP服务商` |

> 实测（2026-10-05）：编码 `MSPB2C` 创建成功，列表 id=7、type=`msp_provider`、status=`active`。

## 3. 验证

**界面**
- 列表中该行「类型」= `MSP服务商`、「状态」= `活跃`；「查看」可打开详情。

**接口**（浏览器已登录平台账号）

```js
fetch('/api/v1/tenants?page=1&pageSize=50', { credentials:'include' })
  .then(r => r.json()).then(d => console.log(d.data.items.filter(t => t.code === 'MSPB2C02')));
```

或 PowerShell（见 [00 §6](./00-environment-and-accounts.md)）：

```powershell
Invoke-RestMethod -Uri 'http://127.0.0.1:8090/api/v1/tenants?keyword=MSPB2C02' -WebSession $s |
  ConvertTo-Json -Depth 6
```

预期：返回对象含 `type: "msp_provider"`、`status: "active"`，`id` 为新分配值。

**数据库（可选）**

```sql
SELECT id, code, name, type, status FROM tenants WHERE code = 'MSPB2C02';
```

## 4. 边界与反例（建议顺带验证）

| 反例 | 操作 | 预期 |
|---|---|---|
| 编码重复 | 再次用同编码创建 | 保存失败并提示（唯一约束）；列表不新增 |
| 编码非法 | 输入 `2 code!` | 表单红字拒绝（仅字母/数字/下划线/连字符，字母或数字开头） |
| 非平台账号 | 用服务商管理员登录后访问 `/admin/tenants` | 403 页面（菜单也不展示该入口） |

## 5. 排错

| 现象 | 处置 |
|---|---|
| 页面没有「新建租户」 | 当前账号不是平台管理员（`super_admin`），或后端非 `saas_msp` 模式（见 [00 §2](./00-environment-and-accounts.md)） |
| 保存 500 | 查看后端日志；确认迁移已执行（租户表结构） |
| 保存后列表未见 | 刷新列表；检查是否因编码重复被拒（会有提示） |

## 6. 下一步

→ [场景 02 · 平台新建客户租户并绑定服务商](./02-platform-create-customer-tenant.md)
