# 场景 11 · 离职编排（Offboarding：停用 + 全量会话失效 + 全部分配回收）

> **状态**：当前（2026-10-09，API 验收 10/10 PASS + 浏览器实操复核）｜**定位**：把 F-08「回收 / 离职 / 退租」落到可执行剧本——**一条「离职」动作**完成停用账号 + access/refresh 全量失效 + 撤销该员工全部分配 + 审计，幂等可重跑
> **读者**：平台管理员、服务商管理员、测试/QA
> **前置**：场景 05 完成（存在有效分配语义）；平台面需 TUM 写通道开关（见 §2）；先读 [离职编排方案 §3](../plan/msp-offboarding-orchestration-plan.md)
> **关联**：[F-08 用户交互流程](../plan/msp-user-interaction-flows.md)、[TUM 方案（平台面契约）](../plan/msp-tenant-user-management-enhancement-plan.md)、[场景 09 平台用户治理](./09-tenant-user-management.md)、[场景 05 分配](./05-msp-allocation-management.md)

## 1. 判定（一句话）

**「离职」= 停用账号（`active=false`）+ 全量会话失效（access+refresh）+ 撤销全部分配（逐条幂等）+ 审计留痕；重复执行安全（`alreadyInactive=true`、回收计数归零），残留可重跑收敛。**

## 2. 前置与开关

| 项 | 要求 | 说明 |
|---|---|---|
| 服务 | 后端 `:8090`（`GET /api/v1/health`=200）、前端 `:3000` | 隧道（DB 15433 / Redis 16380）必须先通，否则后端启动即失败 |
| 平台账号 | `admin`（super_admin，tenantId=1） | 平台治理面通道 |
| 服务商管理员 | `admin-demo01`（provider 租户 `demo01`） | 分配创建 + 反例通道 |
| **写通道开关** | `itsm-backend/.env`：`TENANT_USER_ADMIN_ENABLED=true` 后重启后端 | **默认关**（灰度设计）；未开启时平台面 offboard 一律 403 `TENANT_USER_ADMIN_DISABLED`（fail-closed，见 §5.2 实测） |

## 3. 通道 A：provider 自服务（`admin/users` 行操作）

| # | 操作 | 预期界面/接口反馈 | 验证手法 |
|---|---|---|---|
| 1 | 服务商管理员登录 → `/admin/users` | 列表可见服务商员工（含被离职对象） | 页面加载无 403 |
| 2 | 行操作「离职（停用并撤销全部分配）」 | 二次确认弹窗：说明将停用 + 吊销会话 + 撤销全部分配 | 弹窗文案 |
| 3 | 确认 | 成功提示「已撤销 N 个分配」（N=有效分配数）；列表刷新，状态=停用 | 提示文案 + 行状态 |
| 4 | 该员工旧会话继续访问 | 401（access 已按用户失效；refresh 续签被拒） | 旧 token 请求断言 |
| 5 | 再次点击「离职」（重跑） | 幂等：提示 0 回收；无新增副作用 | 提示 + 审计新增 1 行 |

API 等价：`POST /api/v1/users/:id/offboard`（`user:write`；provider 作用域）。

## 4. 通道 B：平台治理面（TUM 抽屉 + 端点）

| # | 操作 | 预期界面/接口反馈 | 验证手法 |
|---|---|---|---|
| 1 | 平台 `admin` 登录 → `/admin/tenants?tenantUsers=<providerId>` | 用户抽屉打开（深链可复现） | 抽屉标题「共 N 个用户」 |
| 2 | 搜索目标用户（如 `offprobe`）→ 行操作「离职」 | 二次确认：「将停用账号、立即吊销全部会话并撤销其全部分配（可安全重跑）」 | 弹窗文案 |
| 3 | 确认 | 成功提示「已离职「<用户名>」：撤销 N 个分配」（pending>0 时追加「M 个待重试」） | 提示文案 |
| 4 | 列表刷新 | 该行状态开关=停用；「强制下线」置灰（不可重复下线） | 行状态 |
| 5 | 反例：服务商管理员调用同一端点 | 403 `PLATFORM_SCOPE_REQUIRED`（平台面硬判定） | 接口断言 |
| 6 | 反例：目标为租户最后一个可用管理员 | 409 `LAST_ADMIN_PROTECTED`（与停用同口径护栏） | 接口断言 |

API 等价：`POST /api/v1/tenants/:id/users/:userId/offboard`（`tenant:write` 路由粗筛 + service `super_admin` 硬校验）。

## 5. 实测记录（2026-10-09）

### 5.1 API 验收（`scripts/msp/acceptance/run-offboarding-acceptance.ps1`，10/10 PASS）

```
[PASS] OFF-0 平台登录 — role=super_admin tenantId=1
[PASS] OFF-1 定位租户 — provider=demo01(id=10) customer=demo_customer(id=11)
[PASS] OFF-2 平台建号（探针用户） — username=offacpt_20261009-095122 id=76
[PASS] OFF-3 provider 分配 — active_allocations_before=1
[PASS] OFF-4 非平台面反例 — http=403 code=PLATFORM_SCOPE_REQUIRED msg=需要平台管理员权限
[PASS] OFF-5 平台 offboard — deactivated=True revoked=1 pending=0
[PASS] OFF-6a 停用生效 — active=false
[PASS] OFF-6b 分配清零 — active_allocations_after=0
[PASS] OFF-7a 幂等重跑 — alreadyInactive=True revoked=0
[PASS] OFF-7b 治理审计留痕 — rows=2 source=platform_selected targetTenantId=10
```

产物：[run-summary-20261009-095122.md](../evidence/offboarding-acceptance/run-summary-20261009-095122.md)。

### 5.2 开关 fail-closed 实测（先失败后放行）

首次实跑（`TENANT_USER_ADMIN_ENABLED` 未设置）：`[offboard_platform] http=403 msg=平台侧用户治理未启用`（错误码 `TENANT_USER_ADMIN_DISABLED`）——写通道默认关，平台面**不可**绕过；开启开关并重启后端后复跑全绿。此负向结果按预期保留为灰度回滚证据。

### 5.3 浏览器实操（Chrome DevTools MCP）

- 平台 `admin` 登录 → `/admin/tenants?tenantUsers=10` → 抽屉搜索 `offprobe` → 行「离职」→ 确认弹窗 → 成功提示 **「已离职「offprobe_1009094437」：撤销 1 个分配」** → 列表刷新：该行开关=停用、「强制下线」置灰。
- 实测中发现的 UX 告警已修复：`Modal.confirm` 静态调用不消费主题/语言上下文（antd 告警）→ 改为 `App.useApp().modal`；抽屉用例 6/6 通过、`tsc -b`/eslint 绿。

## 6. 反例与边界

| 场景 | 结果 | 说明 |
|---|---|---|
| 非平台面调用平台端点 | 403 `PLATFORM_SCOPE_REQUIRED` | service 层 super_admin 硬判定（路由权限仅粗筛） |
| 最后管理员离职 | 409 `LAST_ADMIN_PROTECTED` | 与停用同口径，防租户锁死 |
| 自操作（actor=目标） | 409 `SELF_OPERATION_FORBIDDEN` | 防自锁 |
| 幂等重跑 | 200，`alreadyInactive=true`、回收 0 | 仍留痕（审计各 1 行） |
| 分配回收部分失败 | `allocationsPending>0`，动作不阻断 | 巡检兜底 + 重跑收敛（方案 D2=A） |
| 跨租户双键 | 404 `TENANT_USER_NOT_FOUND` | (tenant_id,user_id) 双键，杜绝 IDOR |

## 7. 验证命令

```powershell
# 一键验收（隔离探针：新建 → 分配 → 离职 → 核验；可重复执行）
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/msp/acceptance/run-offboarding-acceptance.ps1

# 后端定向回归（服务/处理器/路由/HTTP 全链）
cd itsm-backend
go test -p 1 ./service ./handlers/tenant ./handlers/user ./router -count=1
```

## 8. 已知限制与未覆盖

1. **P2 未实现**：`expires_at` 到期自动回收（后台任务）另案立项，见[方案 §4](../plan/msp-offboarding-orchestration-plan.md)。
2. 验收脚本在指定 provider 租户内新建探针用户并停用（不触碰既有用户/分配）；重复执行会累积已停用探针用户，属预期。
3. 浏览器实操依赖 Chrome 调试端口可用；不可用时按 §5.1 API 验收 + 抽屉组件用例（jest）替代复核。
