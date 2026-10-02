# 07 · 已确认产品缺口（Known Gaps）

> **状态**：当前
> **更新日期**：2026-09-28
> **来源**：`saas_msp` 生产环境端到端初始化实测（1 服务商 + 2 客户，见 [06 文档 §7](./06-verification-and-troubleshooting.md)）
> **用途**：记录实测中确认的**产品缺口**、当前运维规避手段与建议修复方向；缺口关闭后在本页更新状态，不删除历史结论。
> **配套方案**：用户创建 / 登录选租户 / 租户切换相关缺口（G1/G2/G3/G9）的深入分析与分期方案见 [plan/msp-user-lifecycle-and-tenant-switching-plan.md](./plan/msp-user-lifecycle-and-tenant-switching-plan.md)（缺口编号 F1–F15）。

> **复核（2026-09-30）**：G1–G10 的承接工作流见[实施方案 §3.0-F](./plan/msp-implementation-plan.md)。G8 优先级上调为 **P0**（并入 `IP-P0-2` 缓存审查）；G1/G3/G8 在 P0 关闭；**G9 的 P0 关闭（IP-P0-6）经 2026-09-30 复核未生效（JWT 已锁定时 Header 被跳过），已由 IP-P1-8 真正关闭**（401 + `tenant.probe_denied` 落库审计，见 §10 实现复核）；G2/G4/G5/G6 在 P1 关闭，G7 接受现状（P2 提升为通用约定），G10 文档/脚本已规避（P0 收尾）。

> **编号（2026-09-29 一致性整改）**：本页 `G1–G10` 是**生产实测缺口**的唯一 G 空间；canon §7.3 的能力供给缺口已更名 `K1–K5`；工作台规则已更名 `WB1–WB6`。跨文档引用本页写作 `07:G4`（注册表见 canon 附录 C）。

## 1. 缺口清单（按影响排序）

| # | 缺口 | 影响 | 当前规避 | 建议修复 | 优先级 |
|---|---|---|---|---|---|
| G1 | 跨租户创建用户被 tenant guard 拦截 | 无法为新租户创建首个用户 | SQL + pgcrypto 直写 | 提供受控的跨租户用户创建路径（见 §2）；**→ ✅ 已关闭（2026-09-30，IP-P0-5）：`UserProvisioningService` 三通道 + `tenantctx.WithProvisioningBypass(actor,channel,target)`；平台/MSP/租户内均可经 API 建号（`07:G2` 仍归 IP-P1-5）** | P0 |
| G2 | bootstrap 无法为多租户建首个管理员 | 第 2 个及以后租户无法走官方初始化 | 同上 | bootstrap 支持 `-tenant-id` + 用户名/邮箱策略（§3）；**→ ✅ 已关闭（2026-09-30，IP-P1-5）：账号策略 `admin-<tenantCode>`（username/email 不再全局冲突）；`cmd/initialize` 支持 `-tenant-id`/`-tenant-code`/`-admin-username`/`-admin-email`；`provision_tenant -create-admin` 无 token 通道（幂等）；`must_change_password` 首登强制改密 + `POST /api/v1/auth/change-password`；连续 2 租户 bootstrap 用例通过** | P0 |
| G3 | MSP 管理员有效角色解析为 `msp_manager` | 同租户 API 建号不可用（无 `user:write`，且过不了角色高攀校验） | 首个用户/技术员均走 SQL | JWT claims 取主角色或按 rank 取最大（§4）；**→ ✅ 已关闭（2026-09-30，IP-P0-9）：登录 role=max(主角色, MSP 映射角色)（`admin`+`provider_admin`→`admin`）；`middleware.RoleRank` 单源覆盖 `msp_*`；同租户 API 可建 `agent`** | P0 |
| G4 | 源租户 `default` 缺内置审批组 | `provision_tenant` readiness 失败（groups=0） | 脚本回填 6 个内置组 | migration 回填存量库 + readiness 降级策略（§5） | P1 |
| G5 | id 序列落后于 `max(id)` | 供给时 `roles_pkey` duplicate key | 脚本 `setval(...)` 校正 | 初始化/迁移统一校正（§6） | P1 |
| G6 | `role_permissions` 无唯一约束 | 无法 `ON CONFLICT`，供给脚本只能 `where not exists` | 脚本幂等插入 | 增加 `(role_id, permission_id, tenant_id)` 唯一索引（§7） | P2 |
| G7 | CLI 工具 stdout 混入状态行 | `psql -tAc` 的 `INSERT 0 1` 污染 `returning id` 输出，自动化解析失败 | `last_number()` 过滤 | 工具输出规范（§8） | P2 |
| G8 | 缓存 key 无租户维度 | 多客户场景存在串数据风险 | **✅ 已审查并关闭（2026-09-30）** | 逐 key 审查完成：租户分区缓存/序列均含 `tenant_id`；全局编号序列登记豁免（见 §9）；承接 `IP-P0-2` 步骤 6 | **P0** |
| G9 | `X-Tenant-Code` 与 JWT 冲突被静默忽略 | 调用方误以为切换了租户；无冲突告警，排障困难（不越权） | 依赖 JWT 租户；探针按实测标注 | Header 与 JWT 冲突时返回 401/400 并记录告警（§10）；**→ ✅ 真正关闭（2026-09-30 复核 + IP-P1-8 v1.29）**：IP-P0-6 声明的关闭未生效（JWT 已锁定时 Header 被跳过，冲突分支不可达）；现为 401 + `reasonCode=TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 落库审计（一致时正常放行） | P2 |
| G10 | snap 版 docker 下宿主 `/tmp` 对守护进程不可见 | `docker cp` 静默复制旧文件；`docker run -v /tmp/...` 产物"写丢"，构建/供给莫名失败 | 产物与暂存目录放 `$HOME` 非隐藏目录 + sha256 校验（§11） | 文档/脚本固化路径约定；容器化部署优先用 bind 到 `$HOME` 或非 snap docker | P1 |

## 2. G1 · 跨租户创建用户被 tenant guard 拦截

**现象**：以 `super_admin`（default 租户）通过 `POST /api/v1/users` 为 `msp_provider` 租户（`tenantId=3`）创建用户，返回 `create User with tenant_id=3 but request tenant=1`。

**证据**：tenant guard 中间件按请求上下文（JWT 的 `tenant_id`）校验写入实体归属，`super_admin` 不豁免；当前无 HTTP 头/参数可切换请求租户上下文（`X-Customer-Tenant-ID` 仅对 MSP 分配用户生效，且面向客户业务数据）。

**影响**：新租户（尤其 `msp_provider` / `msp_customer`）的**首个用户**无法通过产品 API 创建，初始化流程必须回落数据库直写。

**当前规避**：`scripts/msp/setup-msp-tenants.sh` §6 用 `pgcrypto` 的 `crypt(..., gen_salt('bf',10))` 生成 bcrypt 口令直写 `users` + `user_roles`（与 Go `bcrypt` 校验兼容，已实测登录成功）。

**建议修复**：为平台管理员提供受审计的跨租户用户开通能力，二选一：

1. `POST /api/v1/tenants/{id}/users`（平台级路由，`tenant:write` + 审计日志，显式指定目标租户）；
2. 允许 `super_admin` 携带受控头（如 `X-Target-Tenant-ID`）覆盖请求租户上下文，并纳入 tenant guard 白名单与审计。

**验收标准**：`super_admin` 可经 HTTP 为任意租户创建首个管理员；tenant guard 仍拒绝普通跨租户写入；审计日志含操作者/目标租户/被创建用户。

## 3. G2 · bootstrap 无法为多租户建首个管理员

**现象**：

- `bootstrap_tokens` 表为空，且数据库未启用 `pgcrypto`；
- `cmd/initialize generate-bootstrap-token` 生成的流程固定创建 `username=admin` / `email=admin@example.com`；
- `users.username` / `users.email` 为**全局唯一**，第 2 个租户必然冲突。

**关闭（2026-09-30，IP-P1-5）**：账号策略改为 `admin-<tenantCode>`（邮箱 `admin-<tenantCode>@bootstrap.local`，可用 `-admin-username/-admin-email` 显式覆盖）；`cmd/initialize` 支持 `-tenant-id`/`-tenant-code` 定位目标租户（不再写死 `default`）；`provision_tenant -create-admin` 提供无 token 通道（`CreateFirstAdmin`，幂等保护：租户已有 bootstrap 管理员则拒绝）；bootstrap 管理员默认 `must_change_password=true`，登录响应携带 `mustChangePassword`，经 `POST /api/v1/auth/change-password`（持旧密码 + 密码策略）清除。回归：`pkg/bootstrap` 连续 2 租户用例 + 身份/幂等用例。

**影响**：`saas_msp` / `saas` 模式无法用官方 bootstrap 流程开通第二个租户的首个管理员。

**当前规避**：同 G1（SQL 直写）。

**建议修复**：

1. `users.username`/`email` 唯一性改为**租户内唯一**（`unique(tenant_id, username)`），或
2. bootstrap 令牌携带 `tenant_id`，并允许自定义 `username`/`email`（默认值带租户后缀，如 `admin@msp001.local`）；
3. 初始化 CLI 增加 `-tenant-id` 参数并在文档中明确多租户开通顺序。

**验收标准**：连续为 2 个以上租户执行 bootstrap 均成功，且互不冲突。

## 4. G3 · MSP 管理员有效角色解析为 `msp_manager`

**现象**：`mspadmin`（主角色 `admin`，`msp_role=provider_admin`，角色边含 `admin` + `msp_manager`）登录后 JWT claims 的 `role` 为 `msp_manager`：

- `roleRank("msp_manager") == 0`（`handlers/user/handler.go:516`），低于 `admin`；
- 其有效权限集仅 10 条 `msp_*`，不含 `user:write`；
- 即便补权限，创建用户时仍会触发"不得分配高于自身角色的用户角色"校验（`admin` 目标角色 > `msp_manager`）。

**影响**：服务商管理员无法在同租户内通过 API 创建用户/技术员（实测 `mspagent` 只能 SQL 创建）。

**当前规避**：`scripts/msp/setup-msp-tenants.sh` §6b 用 SQL 创建 `mspagent`。

**建议修复**：

1. JWT `role` claims 取**主角色**（`users.role`）或按 `roleRank` 取最大者，MSP 角色只作为附加维度（`msp_role`）；
2. `roleRank` 为 `msp_*` 角色定义合理秩（不应低于其绑定的主角色能力）；
3. 明确"角色高攀校验"在 MSP 场景的语义（应比较实际权限集而非角色名）。

**验收标准**：`mspadmin` 登录后 `role=admin`（或 rank ≥ admin），可经 `POST /api/v1/users` 创建 `agent` 用户。

## 5. G4 · 源租户 default 缺内置审批组

**现象**：`provision_tenant` 的 readiness 校验要求源模板租户 `groups > 0`；2026-09-15 新增的 `BuiltinGroups`（`pkg/seeder/seeder.go`：approvers-l1/l2/l3/managers/security/change）未在存量 `default` 租户生效 → 供给直接失败。

**影响**：存量库升级后新租户供给不可用（新建库不受影响）。

**当前规避**：脚本 §1 按 `BuiltinGroups` 回填 6 个组（`where not exists` 幂等）。

**建议修复**：随版本提供一次性回填（migration 或 `initialize` 子命令），并让 readiness 失败信息明确指出缺项与修复命令。

## 6. G5 · id 序列落后于 max(id)

**现象**：`roles` 表 `max(id)=112` 而序列 `last_value=100`，`provision_tenant` 插入时 `roles_pkey` duplicate key。

**影响**：任何依赖序列的自增写入（模板供给、种子数据）在存量库上随机失败。

**当前规避**：脚本 §2 对 `roles/permissions/menus/groups/role_permissions/sla_definitions/ci_types/approval_workflows/system_configs` 执行 `setval(seq, greatest(max(id),1))`。

**建议修复**：初始化/迁移脚本统一执行序列校正（覆盖全部自增表），并在启动自检中告警。

## 7. G6 · role_permissions 无唯一约束

**现象**：`role_permissions` 无 `(role_id, permission_id, tenant_id)` 唯一约束，`ON CONFLICT` 不可用，重复插入会产生重复行（影响权限并集计算与审计）。

**当前规避**：脚本用 `insert ... where not exists (...)`。

**建议修复**：增加唯一索引（先清理历史重复行），并让授权接口使用 upsert。

## 8. G7 · CLI 工具 stdout 混入状态行

**现象**：`psql -tAc "insert ... returning id"` 的输出包含 `INSERT 0 1` 状态行，导致脚本取到的 ID 为多行（实测：`-tenant-id` 解析失败）。

**当前规避**：脚本统一用 `last_number()`（`sed -n 's/^\([0-9][0-9]*\)$/\1/p' | tail -n 1`）过滤；函数内部状态信息一律走 stderr。

**建议修复**：工具/脚本规范：结构化输出仅走 stdout，状态与日志走 stderr；文档中给出示例。

## 9. G8 · 缓存 key 无租户维度

**审查结论（2026-09-30，`IP-P0-2` 步骤 6）**：**关闭**。逐 key 清单：

| 键/缓存（代码位置） | 格式 | 判定 |
|---|---|---|
| RBAC 权限缓存（`middleware/rbac.go`） | `{role}_{tenantID}` | ✅ 含租户维度 |
| HTTP 响应缓存（`middleware/cache_middleware.go`） | `api:tenant:{id}:user:{id}:path:{path}:query:{hash}` | ✅ 含租户+用户维度 |
| 影响面解释缓存（`service/impact_explanation_service.go`） | `cacheKey(tenantID, ciID, hops)` | ✅ 含租户维度 |
| 工单号序列（`repository/ticket`、`service/ticket_core_service.go`） | `sequence:ticket:{tenantID}:{YYYYMM}` | ✅ 含租户维度 |
| Token 黑名单（`service/token_blacklist_service.go`） | `jwt:blacklist:{token}` / `refresh:blacklist:{token}` | ➖ 凭据键，无租户业务数据 |
| 事件号/CI 号序列（`incident_service`、`configuration_item_service`） | `sequence:incident:{YYYYMM}` / `sequence:ci:{YYYYMM}` | ⚪ **豁免**：编号为全局唯一（唯一约束不含 `tenant_id`），序列用于跨租户协调 + 存在性跳号，不含租户数据；若后续要求编号按租户隔离，需先改唯一约束（登记 P2） |
| `cache/redis.go` 包装器 / `query_optimizer.GenerateCacheKey` | — | ➖ 当前无业务调用方（无生效键） |

**评审检查项**：新增缓存/序列键必须含 `tenant_id`；全局协调键须在代码注释中声明豁免理由，并在本表登记。

## 10. G9 · `X-Tenant-Code` 与 JWT 冲突被静默忽略

**现象**：客户 A 的管理员 token 携带 `X-Tenant-Code: MSPCUSTB` 请求 `GET /api/v1/users`，实测返回 **200** 且仅返回客户 A 用户（数据不越界），但也没有任何"租户不匹配"拒绝或告警。

**根因**（`middleware/tenant.go:31-83`）：租户来源优先级为 `JWT claims.tenant_id > X-Tenant-Code > Subdomain > Path`；步骤 2 的 Header 解析条件是 `tenantEntity == nil`，即 **JWT 已解析出实体时 Header 根本不会被读取**，因此步骤 5 的"JWT 与最终结果不一致 → 拒绝"（`tenant.go:144-157`）也不会触发。文档旧表述（"冲突返回 401"）与实际不符。

**影响**：安全上 fail-safe（不会跨租户取数），但：

- 客户端/网关若依赖该 Header 切租户，会得到"看似成功"的响应，实际仍在原租户 → 误操作与排障成本；
- 冲突请求无审计/告警信号，不符合 fail-closed 的可观测性要求。

**当前规避**：文档与探针按实测行为描述（06 文档 §2 用例 2、§7.3）；客户端一律以 JWT 为准（登录时用 `tenantCode` 换取正确 token）。

**建议修复**：在步骤 2 之前增加"冲突检测"：若请求同时携带 JWT `tenant_id` 与 `X-Tenant-Code`，且二者解析结果不一致 → 返回 401（或 400 参数错误）并记录 `tenant mismatch rejected` 告警。

**验收标准**：`X-Tenant-Code` 与 JWT 冲突时返回 401/400；一致时正常放行；日志含冲突双方租户 ID。

**实现复核（2026-09-30，IP-P1-8）**：IP-P0-6 曾声明本缺口已在 P0 关闭，但复核确认该路径仍被跳过（根因同上）——冲突分支仅在"JWT 解析不到实体"时才可能触发。IP-P1-8 将 Header 解析改为**无论 JWT 是否锁定都执行**，锁定时做一致性校验：冲突 → 401 + `TENANT_MISMATCH_REJECTED` + `tenant.probe_denied` 审计（source=header）；一致 → 正常放行。回归：`middleware/tenant_test.go` 冲突/一致两态 + 头通道拒绝审计。

## 11. G10 · snap 版 docker 下宿主 `/tmp` 对守护进程不可见

**现象（2026-09-28 实测）**：

- 宿主 `/tmp/provision_tenant_linux_amd64`（123,248,392 字节，sha256 `e7f518…`）经 `docker cp` 注入容器后，容器内 `/tmp/provision_tenant` 仍是**旧文件**（119,537,895 字节，sha256 `7c14a7…`），且 `docker cp` 返回码为 0、无任何报错；
- `docker run -v /tmp:/out ...` 的构建产物在宿主 `/tmp` 不可见（模块缓存目录同样为空），表现为"构建成功但找不到二进制"；
- 将源文件换到 `/home/<user>/...` 后 `docker cp` 立即恢复正常（sha256 一致）。

**根因**：docker 由 snap 安装（`/snap/bin/docker` → `/usr/bin/snap`，`snap list docker` = 29.8.0）。snap 沙箱为守护进程提供**独立的 `/tmp`**，宿主 `/tmp` 不在其可见范围内；snap 的 `home` 接口同时**不放行 `$HOME` 下的隐藏目录**（实测 `$HOME/.itsm-provision` 报 `permission denied`，`$HOME/itsm-artifacts` 正常）。

**影响**：所有依赖 `docker cp` / `docker run -v` 且路径在 `/tmp` 的运维动作（二进制注入、构建产物导出、缓存挂载）都可能静默失败或使用陈旧文件；`docker cp` 不报错使问题极难定位。

**当前规避**：

- `scripts/msp/setup-msp-tenants.sh` 注入前先把二进制复制到 `$HOME/itsm-artifacts`（snap 可见）并**校验容器内 sha256**，不一致立即失败退出；
- `scripts/msp/build-provision-tenant.sh` 的产物与模块缓存默认落在 `$HOME/itsm-artifacts`；
- 本次实际采用的构建路径为本机交叉编译 + `scp` 到 `$HOME/itsm-artifacts`。

**建议修复（环境/工具侧）**：

1. 部署文档与运维脚本统一约定"docker 可见路径"（`$HOME` 下非隐藏目录，或 `/var/lib/...` 等），禁止把 docker 输入/输出放 `/tmp`；
2. 或改用非 snap 安装的 docker（apt/deb），消除沙箱路径差异；
3. 运维脚本对 `docker cp` / `-v` 结果做内容校验（本次已加 sha256 校验，可作为通用范式）。

**验收标准**：在 snap docker 环境下按文档路径执行构建与注入，容器内文件与宿主 sha256 一致；文档不再出现"把二进制/构建产物放 `/tmp` 再 docker cp/-v"的指引。

## 12. 与 ADR-004 行动项的关系

| 本页缺口 | ADR-004 行动项 | 状态 |
|---|---|---|
| G1/G2 | A2–A3（租户开通与首个管理员） | 实测确认；G1→`IP-P0-5`（P0）→ **✅ 关闭（2026-09-30）**；G2→`IP-P1-5`（P1）→ **✅ 关闭（2026-09-30）** |
| G3 | A4–A5（MSP 授权与权限矩阵） | 实测确认；承接 `IP-P0-9`（P0）→ **✅ 关闭（2026-09-30）** |
| G4/G5/G6 | A2（供给可复现性） | 实测确认；脚本已规避，承接 `IP-P1-5`（P1） |
| G7 | 工具规范 | 实测确认；脚本已规避，P2 提升为通用约定（接受现状） |
| G8 | A8（缓存租户维度） | **P0**；承接 `IP-P0-2` 步骤 6（2026-09-30 上调） |
| G9 | A10（隔离回归） | ✅ 已关闭（IP-P1-8 v1.29；IP-P0-6 漏覆盖 header 冲突路径，复核后修复并落审计） |
| G10 | 部署/工具链（非 ADR 行动项） | 已文档化 + sha256 校验；P0 收尾（随 `IP-P0-1` 自检复核） |

## 13. 证据索引

- 脚本与复现步骤：[02 文档 §10](./02-deployment-and-configuration.md)、[06 文档 §7](./06-verification-and-troubleshooting.md)；
- 代码位置：`middleware/tenant.go`、`middleware/msp_middleware.go`、`handlers/user/handler.go:516`、`pkg/seeder/tenant_provisioner.go:27-43`、`cmd/initialize`、`cmd/provision_tenant/main.go`；
- 生产实测数据：租户 3/4/5 供给计数、`msp_allocations` 三条分配、隔离性探针输出（06 文档 §7）。
