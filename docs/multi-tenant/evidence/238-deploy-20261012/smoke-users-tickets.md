# 238 新库建号 + 建单 + 跨租户协同冒烟运行摘要（2026-10-12）

> 状态：**PASS（14/14）**｜定位：全新生产库 `itsm_prod_v2` 上「建号（三账号）→ 客户建单 → MSP 分配 → 工作台回复/改状态 → 客户侧回看」全链路冒烟（经公网入口 `http://172.18.3.238:8088`）。
> 关联：[238 部署运行摘要](./run-summary.md)｜[建租户冒烟](./smoke-tenants.md)｜[backlog](../../backlog.md)

## 1. 结果（PASS=14 / FAIL=0）

| # | 用例 | 结果 | 证据 |
|---|---|---|---|
| 1 | 平台管理员登录 | PASS | `code=0` |
| 2 | 建号：服务商经理（`provider_admin`） | PASS | `prov.smoke.mgr1012232309` id=7（`role=end_user`+`mspRole=provider_admin`） |
| 3 | 建号：服务商技术员（`provider_agent`） | PASS | `prov.smoke.agt1012232309` id=8 |
| 4 | 建号：客户用户 | PASS | `custa.smoke1012232309` id=9（tenant 3） |
| 5 | 客户用户登录 | PASS | — |
| 6 | 客户建单 | PASS | ticket id=2 `TKT-202610-000003`（priority=high） |
| 7 | 服务商经理登录（MSP 面） | PASS | — |
| 8 | 分配：技术员 → 客户 3（primary） | PASS | `POST /api/v1/msp/allocations` 200 |
| 9 | 技术员登录（MSP 面） | PASS | — |
| 10 | 工作台可见本客户工单 | PASS | `GET /msp/workbench/tickets?customerTenantIds=3` 含 `SMOKE-238-…` |
| 11 | 工作台回复 | PASS | `POST /msp/tickets/2/reply` 200 |
| 12 | 工作台改状态 → `in_progress` | PASS | `POST /msp/tickets/2/status` 200 |
| 13 | 客户可见服务商回复（评论互通） | PASS | `GET /tickets/2/comments` 含回复内容 |
| 14 | 客户侧状态同步 `in_progress` | PASS | `GET /tickets/2` |

## 2. 观察点（脚本落地经验，非缺陷）

- **分配管理写权限归属**：`POST /msp/allocations` 需 `msp_allocation:write`；普通租户 `admin` 角色不含该权限，且 07:G3 角色提级取「rank 更高者」，`role=admin`（rank 4）会**遮蔽** `provider_admin→msp_manager`（rank 3）→ 403「权限不足」。
  - 冒烟修正：分配身份使用 **`role=end_user` + `mspRole=provider_admin`**（有效角色 `msp_manager`，含分配写权限）；或使用开通首管（`provider_tenant_admin`，admin ∪ msp_admin 合并）。
  - 建议（可登记跟进）：平台通道建 provider 员工时，若需要分配管理能力，应默认/提示选择 `provider_agent/provider_admin` 映射路径，或在文档中固化该口径。
- **CSRF**：与建租户冒烟一致，写接口需 `X-CSRF-Token`（每次写前重取）。
- 首轮（v1，未修正身份）遗留：用户 id=4/5/6 与工单 id=1（无分配）；本轮（v3）新增用户 id=7/8/9 与工单 id=2（链路完整）。

## 3. 资源清单（生产库 `itsm_prod_v2`，供后续联调）

| 资源 | 值 |
|---|---|
| 租户 | 服务商 `SMKMSP1012231808`（id=2）｜客户 `SMKCUST1012231808`（id=3） |
| 用户 | 经理 id=7（provider_admin）｜技术员 id=8（provider_agent）｜客户 id=9 |
| 分配 | id=8 → 客户 3（primary） |
| 工单 | id=2 `TKT-202610-000003`（状态 `in_progress`，含服务商回复） |
