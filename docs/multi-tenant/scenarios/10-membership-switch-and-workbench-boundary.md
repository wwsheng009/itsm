# 场景 10 · 切换 vs 工作台边界（Membership + Switch 收口实操）

> **状态**：当前（2026-10-07，API 实跑 + 前端组件用例验证；浏览器 MCP 不可用，见 §5）｜**定位**：把 MSW-6 边界决策表落到可执行剧本——何时用工作台（不切换）、何时深度切换、返回语义、客户侧只读成员视图
> **读者**：服务商管理员/技术员、测试
> **前置**：场景 01–05 完成（provider `MSPB2C` + 客户 `B2CCUST` + 分配 `b2cprov_admin`/`b2c_tech`）；先读 [canon §2.3 边界表](../plan/msp-concept-model-and-architecture-canon.md)
> **关联**：[收口方案 §3.4/§4 M3–M4](../plan/msp-membership-switch-consolidation-plan.md)、[场景 07 工作台](./07-msp-workbench-collaboration.md)、[场景 05 分配](./05-msp-allocation-management.md)

## 1. 判定（一句话）

**跨客户 → 工作台（不切换）；单客户深度 → 深度切换；"返回工作台" = 切回 provider 作用域。**

## 2. 步骤与预期

| # | 操作 | 预期界面/接口反馈 | 验证手法 |
|---|---|---|---|
| 1 | `b2cprov_admin` 登录，进入 `/msp/workbench` | 工作台可用；`GET /auth/me`：`tenantId=MSPB2C`、`homeTenantId=MSPB2C` | `/auth/me` 响应断言 |
| 2 | 顶栏客户选择器选 **B2CCUST**（跨客户场景 → 工作台筛选；单客户深度 → 进入深度态） | 深度态指示：**「当前客户：B2CCUST（深度操作中）」**；`GET /auth/me`：`tenantId=B2CCUST`、**`homeTenantId=MSPB2C` 不变**（归属租户契约，MSW-4） | `/auth/me` + 页面指示文案 |
| 3 | 在客户作用域做单客户操作（工单详情/评论/状态） | 走客户侧页面与权限（会话按目标租户 membership 解析）；此时 `/msp/workbench` 不可用（403，设计如此） | 页面操作 + `tenant.switch` 审计 |
| 4 | 点击 **「返回工作台」** | 先 `switch-tenant(homeTenantId)` **成功后再导航**；`/auth/me` 回到 `tenantId=MSPB2C`；工作台恢复可用（不再 403） | `/auth/me` 断言 + 工作台加载 |
| 5 | 客户管理员 `b2ccust_admin` 打开 `/admin/users` | 出现 **「服务方成员（只读）」** 区块：`b2cprov_admin`/`b2c_tech` 带「服务方」标签，**无任何管理动作**（Q2=B）；不计入分页 total | 列表响应 `providerMembers` + 页面只读断言 |
| 6 | 反例（撤销联动）：场景 05 解除分配后，provider 员工再次切换 B2CCUST | 403 拒绝 + `tenant.switch_denied` 审计（Q3=A：撤销即时生效，在途 refresh 同步吊销） | 切换接口 403 + 审计查询 |

## 3. API 复现（PowerShell 片段）

```powershell
$base = 'http://127.0.0.1:8090'
# 1) provider 管理员登录（home=provider）
$p = Invoke-RestMethod -Uri "$base/api/v1/auth/login" -Method Post -ContentType 'application/json' `
  -Body '{"username":"b2cprov_admin","password":"<pass>"}' -SessionVariable provSess
# 2) 深度切换（需 CSRF；响应 user.homeTenantId 必须保留 provider）
$csrf = (Invoke-RestMethod -Uri "$base/api/v1/csrf-token" -WebSession $provSess).data.csrf_token
$sw = Invoke-RestMethod -Uri "$base/api/v1/auth/switch-tenant" -Method Post -WebSession $provSess `
  -Headers @{ 'X-CSRF-Token' = $csrf } -ContentType 'application/json' -Body (@{ tenantId = $custId } | ConvertTo-Json)
$sw.data.user.homeTenantId   # 期望：provider 租户 ID
# 3) 客户侧只读成员视图（客户管理员会话）
$u = Invoke-RestMethod -Uri "$base/api/v1/users?page=1&pageSize=10" -WebSession $custSess
$u.data.providerMembers      # 期望：accountKind=provider 且 readOnly=true
```

## 4. 边界速查（与 canon §2.3 同源）

| 操作 | 路径 | 说明 |
|---|---|---|
| 多客户分诊/报表/批量 | `/msp` 工作台（不切换） | 跨租户只读 + 批量；唯一跨租户写面已收窄 + 审计 |
| 单客户深度操作 | 深度切换 | 会话按目标租户 membership；退出=切回 home |
| 平台治理 | 治理面（显式目标租户） | 与业务面隔离（D11） |
| provider 自身业务 | Home 作用域 | provider 同时是普通 ITSM 租户（I3） |

## 5. 已知限制

1. **浏览器 MCP 不可用（Chrome 调试端口 403）**：本剧本 2026-10-07 以 **API 实跑 + 前端组件用例**（CustomerFilter 深度态/auth-store 回滚/ProviderMembersCard）替代浏览器截图复核；环境恢复后可按 §2 表格逐条复跑。
2. `providerMembers` 为只读区块、独立于分页 `total`；跨页查看请改用筛选（status/search/department）。
3. 深度态指示当前展示目标租户与退出入口；"来源"（`tenant_source`）未在 UI 文案展示（需要时可在 tooltip 补充）。
