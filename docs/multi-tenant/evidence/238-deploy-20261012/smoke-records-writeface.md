# 238 三类单据「推进状态」UI 写面验证摘要（2026-10-12）

> 状态：**已修复并回归（OB-14 关闭，提交 `b50e7c7a` + 238 v3 API/UI 双回归）**｜定位：238 公网站「跨租户单据 → 推进状态」的 **UI 写面**验证（浏览器实测 + 授权矩阵核验）、修复与回归，并做演示账号口令轮换。
> 关联：[238 部署运行摘要](./run-summary.md)｜[smoke-ui.md](./smoke-ui.md)｜[backlog OB-14](../../backlog.md)

## 1. 执行与结果

| # | 用例 | 结果 | 证据 |
|---|---|---|---|
| 1 | 普通 MSP 账号（`end_user + provider_admin`）打开「推进状态」 | **符合设计（fail-closed）**：按钮禁用；API reasonText：问题/变更为「仅创建人、受理人或管理员可执行状态流转」；服务请求为「当前状态不支持履约推进（审批类动作保留客户治理面）」 | 页面快照 + `GET /api/v1/msp/{problems,changes,service-requests}/1?customerTenantId=3` |
| 2 | 管理账号（`role=admin + mspRole=provider_admin`）打开「推进状态」 | 读面 `allowedActions.status=true`；UI 弹窗正常（「推进问题状态 · PRB-20261012-0001」，选项 调查中/已定位/已解决/已关闭） | 页面快照 |
| 3 | 同上账号提交「调查中」 | **FAIL（F10）**：`403 {"code":2003,"message":"权限不足"}`（通用预检拒绝）——与读面「允许」**不一致** | UI toast「权限不足」；页面内直调 `POST /api/v1/msp/problems/1/status` 原始响应 403 |
| 4 | 口令卫生 | 本轮演示账号（`prov.ui.demo1012235726`、`prov.ui.mgr1013000422`）**口令已轮换作废**（旧口令登录 → 2001） | 轮换脚本输出 |

## 2. F10 根因（双层门禁口径不一致 + 角色词表交集为空）

1. **路由级通用预检**（`middleware/rbac_precheck_gen.go`：`POST /msp/problems/*/status` → `msp_ticket:write`）按 **token 角色**（07:G3 取「主角色 ∪ MSP 映射角色」rank 更高者）计算：`role=admin`（rank 4）**遮蔽** `provider_admin→msp_manager`（rank 3）→ 租户 2 的 `admin` 角色无 `msp_ticket:write` → **403**。
2. **读面 allowedActions**（`service/msp_record_*.go`）用 **`actor.MSPRole`**（msp_manager）∩ 目标租户业务码（租户 3 `problem:write`，实测存在）→ 计算为 **true**。两处口径来源不同 → 不一致。
3. **`canWriteRecord` 管理白名单** = `{super_admin, admin, manager, sysadmin}`；而**预检可通过**的 token 角色需 ∈ 租户 2 持有 `msp_ticket:write` 的角色（实测：`it_director/msp_admin/msp_manager/msp_specialist/msp_tech/ops_director/provider_tenant_admin/sysadmin`）；平台通道**可授予**词表（`super_admin/admin/manager/it_admin/security_admin/sysadmin/agent/technician/security/end_user/user`）中**仅 `sysadmin` 同时满足**，而它被平台通道**禁授**（实测 `ROLE_NOT_GRANTABLE`）。
   → 对平台通道创建的 MSP 用户，**record 推进状态事实上不可达**（除 owner/assignee；跨租户场景 owner 恒为客户侧创建人，assignee 亦无 MSP 指派通道）。工单（tickets）写面在同矩阵下可用，问题集中在 record 写面这层组合门禁。

## 3. 建议修复方向（OB-14，待产品确认）

- ① 通用预检对 `/api/v1/msp/*` 改用 **MSP 映射角色**（与 `RequireMSPPermission` 同口径），或对已挂 `RequireMSPPermission` 的路由去重重算；
- ② `canWriteRecord` 接受 MSP 管理角色（`msp_manager`/`msp_admin`/`provider_tenant_admin`）；
- ③ 或为 record 写面单独定义明确的 MSP 授权矩阵（与工单写面统一），并补 parity 回归用例。

## 4. 资源与卫生

- 账号：`prov.ui.demo1012235726`（id=14）、`prov.ui.mgr1013000422`（id=15），均分配客户 3；用后口令已轮换（当前口令不可知，可按需重置）。
- 结论边界：本轮为**新库+平台通道建号**口径；`sysadmin` 若由运维直接授予（非平台通道）可绕开交集问题，但不应作为常规路径。

## 5. 修复与回归（v3 部署 · 版本 `1.8.0-dev.b50e7c7a`，2026-10-12 23:1x）

**修复（提交 `b50e7c7a`）**：`middleware/rbac.go` 为 `/api/v1/msp/*` 增加预检兜底——token 角色判定失败时，按用户 DB `msp_role` 的映射角色（`provider_admin→msp_manager`）复用四层判定再判一次（四层主体抽为 `smartCheckPermissionWithRoles`，供单/多角色复用）；非 MSP 路径不受影响；`customer_user` 映射回 `end_user` 且与 context 角色重合，不触发兜底。回归测试：`middleware/msp_precheck_fallback_test.go`（5/5 子用例：兜底放行 / 映射解析 / customer_user 仍拒 / token 已具备不受影响 / 非 MSP 路径不受影响）。

**238 回归（新版镜像 `1.8.0-dev.b50e7c7a` 上线后实测）**：

| # | 用例 | 结果 |
|---|---|---|
| R1 | `admin + provider_admin`（平台通道建号，已分配）提交 `POST /msp/problems/1/status → investigating` | **200**（响应含记录详情）；复读状态 = `investigating` ✅（修复前为 403 权限不足，读面/写路径口径一致） |
| R2 | 同账号读面 `allowedActions.status=true` | ✅ |
| R3 | 未分配客户的 `msp_manager` 提交写路径 | 403 `MSP_ALLOCATION_REQUIRED`（较早门拒绝，fail-closed）✅ |
| R4 | 平台通道建 `customer_user` MSP 角色账号 | 明确拒绝 `MSP_ROLE_NOT_ALLOWED`（白名单仅 `provider_admin/provider_agent`，设计内 fail-closed）✅ |

- 部署侧：后端重建为新镜像、7/7 容器 healthy、HTTP/版本验证通过（`/api/v1/version = 1.8.0-dev.b50e7c7a`）；回归用演示账号口令均轮换作废。

## 6. UI 全链路复验（v3 · `1.8.0-dev.b50e7c7a`，2026-10-13 07:19–07:21 CST）

账号：`prov.ui.op21013071836`（id=18，`role=admin` + `mspRole=provider_admin`，平台通道建号后**自分配**客户 3）。**自分配直接成功**——预检兜底对 `/msp/allocations` 同样生效，OB-13 所述手工绕行不再需要。

| # | 步骤（浏览器真实点击） | 结果 |
|---|---|---|
| U1 | 登录 `http://172.18.3.238:8088/login` | ✅ 进入仪表盘；MSP 菜单（服务台/客户管理/跨客户工作台/分配管理/审计看板）可见 |
| U2 | `/msp/workbench?board=records` → 「跨租户单据」看板 → 切「问题」子 tab | ✅ 行 `PRB-20261012-0001`（冒烟客户A）可见，状态「调查中」 |
| U3 | 行内点「推进状态」 | ✅ 弹窗「推进问题状态 · PRB-20261012-0001」，当前状态「调查中」 |
| U4 | 展开目标状态下拉 | ✅ 选项：已定位/已解决/已关闭（均可选，非禁用） |
| U5 | 选择「已定位」 | ✅ 选定；「推 进」按钮由 disabled 转为可点 |
| U6 | 点击「推 进」 | ✅ 弹窗关闭；行状态「调查中」→「**已定位**」；行更新时间 `2026/10/13 07:21:10` |

**API 读回佐证**（同浏览器会话内 fetch）：`GET /api/v1/msp/problems/1?customerTenantId=3` → 200，`status=identified`，`updatedAt=2026-10-12T23:21:10Z`，`allowedActions=[{view:true},{status:true}]`——读面与写面口径一致。

**结论**：修复前同操作返回「权限不足」toast（F10）；修复后 **UI 点击 → 选择 → 提交全链路成功**，行状态与 API 读回一致。演示账号 `prov.ui.op21013071836` 口令已轮换作废（旧口令登录 → `2001`）。
