# 238 三类单据「推进状态」UI 写面验证摘要（2026-10-12）

> 状态：**已执行 · 发现 F10（读面契约不一致 + 写面不可达路径）**｜定位：238 公网站「跨租户单据 → 推进状态」的 **UI 写面**验证（浏览器实测 + 授权矩阵核验），并做演示账号口令轮换。
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
