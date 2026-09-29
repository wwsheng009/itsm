# 06 · 验证与排障：MSP 场景

> **状态**：当前
> **更新日期**：2026-09-28
> **适用**：运维/测试/服务商管理员；前置阅读 [02](./02-deployment-and-configuration.md)、[04](./04-provider-dimension.md)

> **定位（as-is）**：本文为**现状验证/排障手册**（2026-09-28 实测），**不是目标口径**；其中 ADR-004 行动项引用（`A2–A10`）须写作 `ADR-004:A#`（见[一致性审计](./plan/msp-docs-consistency-audit.md) C15）。

## 1. 上线验收清单

**部署层**

- [ ] `DEPLOYMENT_MODE=saas_msp` 已生效：登录后 `GET /api/v1/msp/status` 返回 200；
- [ ] 对照组：`private` 模式下 `/api/v1/msp/status` 返回 404（门控生效）；
- [ ] `ITSM_TENANT_GUARD_POLICY=fatal` 下服务可正常启动（无未豁免缺 `tenant_id` 表）。

**租户层**

- [ ] 服务商租户（`msp_provider`）与客户租户（`msp_customer`）创建完成；
- [ ] `go run ./cmd/provision_tenant -tenant-id <ID>` 执行成功；
- [ ] readiness 7 项（roles/permissions/role permissions/menus/groups/SLA/CI types）全部非 0。

**权限层**

- [ ] 服务商角色已授予 `msp.read` / `msp_customer.read` / `msp_ticket.read` / `msp_report.read` / `msp_allocation.read/write`；
- [ ] 客户侧角色未授予任何 `msp_*` 权限；
- [ ] 服务商工程师均存在有效 `MSPAllocation`（`deassigned_at` 为空）。

**隔离层**（见 §2 回归用例，全部通过）。

## 2. 隔离回归用例（fail-closed）

| # | 操作 | 期望结果 |
|---|---|---|
| 1 | 不带租户上下文（无 JWT/`X-Tenant-Code`/域名）访问业务接口 | 401（`middleware/tenant.go:138-141`） |
| 2 | 客户 A 的 JWT + `X-Tenant-Code: <客户B>` | 数据不越界；**实测 200**：JWT 已解析出租户时 Header 被忽略（`tenant.go:60-83` 仅作补充来源，不会触发 `tenant.go:146-156` 的不匹配拒绝）→ 见 [07-known-gaps.md](./07-known-gaps.md) G9 |
| 3 | 服务商工程师带未分配的 `X-Customer-Tenant-ID` | 403（分配校验，`middleware/msp_middleware.go:133`） |
| 4 | 客户 A 的 token 读取客户 B 的工单/知识库 | 401/403，且无任何数据返回 |
| 5 | 租户 `suspended`/`expired` 后任意请求 | 403（`tenant.go:160-169`） |
| 6 | `private` 模式访问 `/api/v1/msp/*` | 404（`middleware/msp_gate.go:21-33`） |
| 7 | 租户切换后使用新 JWT 访问旧租户数据 | 被拒（JWT `tenant_id` 与数据不符） |

## 3. 常见问题排查

| 症状 | 可能原因 | 处理 |
|---|---|---|
| MSP 接口/页面 404 | 部署模式为 `private` | 改为 `saas_msp` 并重启（02 文档） |
| MSP 接口 403 "非 MSP 用户" | 当前用户不在 `msp_provider` 租户，或 `msp_role` 为空 | 检查 `users.msp_role`（`ent/schema/user.go:69-72`） |
| 客户列表为空 | 无 `MSPAllocation`，或已 `deassigned_at` | 新建/恢复分配（04 文档 §3） |
| 带 `X-Customer-Tenant-ID` 403 | 该客户不在当前用户的分配列表 | 补分配或核对客户租户 ID |
| 登录 401 "租户信息缺失/不匹配" | 未携带 `tenantCode`/域名不匹配/与 JWT 冲突 | 核对租户 `code`/`domain` 与登录参数 |
| 请求 403 "租户已被暂停或过期" | `status` 非 `active` 或 `expiresAt` 已过 | 恢复状态或调整到期时间（03 文档 §8） |
| provisioning 失败 | readiness 校验有 0 项（如菜单/权限未克隆完整） | 查看错误日志定位缺项；修复后重跑（幂等） |
| 服务启动失败，日志含 `tenant_guard ... refusing to start` | 存在未豁免且缺 `tenant_id` 的表 | 补 `tenant_id` 或在 `TenantExemptTables` 登记（含理由/owner/复核日期） |
| RLS `enforce` 下请求 500 | 请求上下文缺少租户（fail-closed） | 先回退 `shadow` 观察，补齐缺失点后再切 `enforce`（`config/config.go:107-114`） |
| 疑似跨客户数据串 | 缓存/外部系统 key 未带租户维度 | 核查缓存 key 约定（`itsm-backend/cache/` 无 tenant 逻辑，需人工确认）；AI 调用确认传 `tenantId` |

## 4. 排障工具

- 自检接口：`GET /api/v1/msp/status`、`GET /api/v1/msp/context`；
- 数据库核查（只读）：

```sql
-- 租户与 MSP 关联
SELECT id, name, code, type, status, parent_tenant_id, msp_provider_id, expires_at
FROM tenants ORDER BY id;

-- 分配与回收
SELECT msp_user_id, customer_tenant_id, role, assigned_at, deassigned_at
FROM msp_allocations ORDER BY msp_user_id;
```

- 日志关键词：`tenant lookup failed`、`msp`、`tenant_guard`、`rls`；
- 相关代码：`middleware/tenant.go`、`middleware/msp_middleware.go`、`middleware/msp_gate.go`、`internal/schema/tenant_guard.go`。

## 5. 与 ADR-004 行动项的对应

| ADR-004 行动项 | 本文档验证章节 |
|---|---|
| A1 部署模式 | §1 部署层、§3（404 排查） |
| A2–A3 服务商/客户租户与开通 | §1 租户层、§3（provisioning 排查） |
| A4–A5 分配与权限 | §1 权限层、§3（列表为空/403） |
| A8 缓存租户维度核查 | §3（数据串排查） |
| A9 共享表影响评估 | 01 文档 §4.3、03 文档 §6 |
| A10 隔离回归 | §2 全部用例 |
| A11 RLS 灰度 | §1、§3（RLS enforce 排查） |

## 6. 证据索引

- `middleware/tenant.go:138-169`、`middleware/msp_middleware.go:133`、`middleware/msp_gate.go:21-33`
- `internal/schema/tenant_guard.go:117-127, 219-239`、`config/config.go:107-114`
- `ent/schema/user.go:69-72`、`ent/schema/msp_allocation.go:19-51`
- `docs/acl-manifest.yaml:2593-2650`、`docs/articles/05-multi-tenant-msp-operations.md`

## 7. 生产实测记录（2026-09-28，saas_msp）

**环境**：`DEPLOYMENT_MODE=saas_msp`、`RLS_MODE=shadow`、版本 `1.8.0-dev.4ffde439`、PostgreSQL 17（库 `itsm_prod`）。
**执行**：`scripts/msp/setup-msp-tenants.sh`；随后以 `FORCE_PROVISION=1` 复跑一次，验证二进制可用性与脚本幂等性。

### 7.1 租户与模板供给

| 租户 | code | type | parent | provider | roles | permissions | role_permissions | menus | groups |
|---|---|---|---|---|---|---|---|---|---|
| 1 | default | internal | 0 | 0 | 33 | 203 | 913 | 78 | 6 |
| 3 | MSP001 | msp_provider | 0 | 0 | 33 | 203 | 941 | 78 | 6 |
| 4 | MSPCUSTA | msp_customer | 3 | 3 | 33 | 203 | 913 | 78 | 6 |
| 5 | MSPCUSTB | msp_customer | 3 | 3 | 33 | 203 | 913 | 78 | 6 |

- `provision_tenant -tenant-id <ID> -template-version 1.0.0` 对 3 个租户均输出 `tenant provisioning completed`；重复执行后各项计数不变（幂等，`pkg/seeder/tenant_provisioner.go:27-43`）；
- 二进制由**本机交叉编译**产出（`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath ./cmd/provision_tenant`，本机 Go 1.27.1，实测 68s），scp 到 `/tmp/provision_tenant_linux_amd64`（约 123 MB）；
- provider 租户 MSP 角色授权：`msp_manager=10`、`msp_tech=6`、`msp_viewer=5`、`msp_specialist=7`（合计 28；941 = 913 + 28）。

### 7.2 用户与分配

| 用户 | ID | 租户 | 主角色 | msp_role | 角色边 | 创建方式 |
|---|---|---|---|---|---|---|
| mspadmin | 7 | 3 MSP001 | admin | provider_admin | admin + msp_manager | SQL + pgcrypto |
| mspagent | 10 | 3 MSP001 | agent | provider_agent | msp_tech | SQL + pgcrypto |
| custa_admin | 8 | 4 MSPCUSTA | admin | customer_user | admin | SQL + pgcrypto |
| custb_user | 9 | 5 MSPCUSTB | end_user | customer_user | end_user | SQL + pgcrypto |

> 为什么不是 API 创建：见 [07-known-gaps.md](./07-known-gaps.md) G1/G2/G3。密码哈希用 `pgcrypto` 的 `crypt(..., gen_salt('bf',10))`，与 Go `bcrypt` 校验兼容（已实测 4 个账号均可登录）。

`msp_allocations`（经 `POST /api/v1/msp/allocations`，由 mspadmin 的 token 调用）：

| id | msp_user | customer_tenant | role |
|---|---|---|---|
| 1 | mspadmin | 4 MSPCUSTA | primary |
| 2 | mspadmin | 5 MSPCUSTB | primary |
| 3 | mspagent | 4 MSPCUSTA | primary |

### 7.3 隔离性与能力探针（实测输出）

| 探针 | 期望 | 实测 |
|---|---|---|
| mspadmin `GET /api/v1/msp/status` | 200 | 200 |
| mspadmin `GET /api/v1/msp/context` | 200 | 200 |
| mspadmin `GET /api/v1/msp/customers` | 200，A+B | 200，`MSPCUSTA(4)` + `MSPCUSTB(5)`，total=2 |
| mspagent `GET /api/v1/msp/allocations` | 200，仅客户 A | 200，仅 `MSPCUSTA(4)`，total=1 |
| custa_admin `GET /api/v1/msp/allocations` | 403 | 403 |
| custa_admin + `X-Tenant-Code: MSPCUSTB` | 数据不越界 | **200**（JWT 已定租户时 Header 被忽略，数据仍限客户 A；与旧预期 401 不符 → G9） |
| mspagent + `X-Customer-Tenant-ID: 5`（未分配） | 403 | 403 |
| custa_admin `GET /api/v1/users` | 仅本租户 | 200，`total=1`，仅 `custa_admin` |

### 7.4 复现步骤

```bash
# 1) 构建 provision_tenant（本机交叉编译；服务器侧容器构建见 02 文档 §10.1）
cd itsm-backend
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/provision_tenant_linux_amd64 ./cmd/provision_tenant
ssh <host> 'mkdir -p ~/itsm-artifacts'
scp /tmp/provision_tenant_linux_amd64 <host>:~/itsm-artifacts/

# 2) 一键初始化（幂等；FORCE_PROVISION=1 可强制重跑模板供给以验证二进制）
scp scripts/msp/setup-msp-tenants.sh <host>:~/msp-scripts/
ssh <host> 'cd ~/msp-scripts && PROVISION_BIN_HOST=~/itsm-artifacts/provision_tenant_linux_amd64 bash setup-msp-tenants.sh'
```

> 说明：实测中"服务器侧用 golang 容器构建"因容器内模块下载（goproxy.cn）长时间无进展而改用本机交叉编译；两种方式产物一致，容器脚本保留在仓库供有缓存/网络良好的主机使用。**snap 版 docker 下宿主 `/tmp` 对 `docker cp` / `-v` 不可见**（详见 02 文档 §10.1 与 07 文档 G10），故二进制与脚本放在 `$HOME` 下的非隐藏目录。

## 8. 缺口索引

实测确认的产品缺口、当前规避与建议修复统一记录在 [07-known-gaps.md](./07-known-gaps.md)（G1–G9）。
