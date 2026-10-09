# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.7.0] - 2026-09-24

### Changed

- **前端框架迁移：Next.js 15 App Router → Vite 7 + React Router 7 静态 SPA** — 168 条路由全量改为 `React.lazy` + `Suspense` 代码分割；`next/navigation` / `next/link` / `next/dynamic` 全部替换为 React Router 等价实现；`middleware.ts` 守卫与历史菜单重定向迁移为 `src/routes/guards.tsx` 与 `src/routes/legacy-redirects.tsx`；删除 `src/app/**` BFF（`/api/**`、`/health`、`/uploads` 处理）——dev 由 Vite 代理承接、生产由容器内 nginx 承接（`docs/plan/vite-migration-plan.md`）
- **构建与部署产物** — `itsm-frontend/Dockerfile` 改为 deps/builder/production/runner 四阶段：生产镜像为 `nginx:alpine + dist/`（无 Node 运行时，容器仍监听 3000），`nginx/default.conf` 提供 SPA `try_files`、`/api/` 反代、`/health` 探活与 `/uploads/` 静态别名；compose 构建参数由 `NEXT_PUBLIC_*` 切换为 `VITE_*`（构建期烘入，运行期不可变）
- **测试基建** — Jest 由 `next/jest` 切到 `babel-jest`（保留 `@/` 别名与 `import.meta.env` 桥接）；Playwright `webServer` 改指 Vite dev server；新增 168 条路由可达性冒烟用例 `tests/e2e/route-reachability.spec.ts`

### Added

- **服务请求新建页** — 新增 `/service-requests/new` 独立路由（选择服务目录项后跳转申请表单）；`/service-requests/:id` 对非法 ID 给出「未找到服务请求」兜底页与新建入口，不再以 `id="new"` 请求详情接口

### Removed

- **Next.js 依赖与残留** — 移除 `next`、`@ant-design/nextjs-registry` 与未使用的 `swr`；锁文件清理 Next 时代 webpack 依赖树；仓库内不再有可执行的 Next.js 引用（仅保留解释性注释）

### Fixed

- **E2E 兼容回退清理** — Playwright 用例移除 `NEXT_PUBLIC_API_URL` 兼容分支（统一 `VITE_API_URL`）

## [Unreleased]

### Added

- **IP-P2-10 模板分发 copy-in 落地（跨租户单据流转 P2-①，offboarding 后的流程集成首批）** — provider 设计、客户持有副本、运行时零跨租户引用：①**端点**：`POST /api/v1/msp/customers/:id/workflow-templates/:key/distribute`（在客户租户创建**草稿副本** + 来源追踪）、`GET .../workflow-templates`（分发状态 + 升级提示；`upgradeAvailable` = provider 最新已发布版本 ≠ 分发来源版本）；②**来源追踪**：新表 `workflow_template_distributions`（迁移 `20261009_workflow_template_distribution`；owner=客户租户 + provider 来源 + source/customer 版本 + 操作者）；③**授权与审计**：`msp_customer:read/write` 路由 + `mspguard`（allocation + 客户 active）唯一入口，写入按客户租户重绑定 ctx（RLS GUC 同源），审计 `workflow_template.distribute`（XF8：target_tenant + source=workbench）；④**决策定稿**：D5=A（草稿副本，发布/绑定由客户租户内既有流程完成）、D6=A（提示升级不强制覆盖；重复分发在客户无同 key 草稿时生成新版本，有草稿则 409）；⑤**顺带修复**：存量库 `workflow_templates.created_at/updated_at` 无列默认值导致模板建草稿 500（catalog 与分发 copy-in 均显式 `NOW()`）。验证：service 6 用例（sqlmock）+ handlers/msp 契约用例；实弹 9/9 PASS（创建→发布→分发→状态→反例：重复分发 409 / 未分配客户 403 / 跨 provider 403），零残留（保留审计行）。方案：[msp-cross-tenant-record-flow-architecture.md](docs/multi-tenant/plan/msp-cross-tenant-record-flow-architecture.md) v0.11。

- **IP-P2-9 membership 到期回收落地（offboarding P2 收口）** — `expires_at` 执行点缺失补齐：①**惰性判定**：`middleware.MembershipNotExpired()` 谓词 + 5 处读路径收口（`ResolvePermissions` 权限解析 / 切换邀请依据 / 工作台成员计数 / 组织挂靠 / 客户侧「服务方成员」展示）——到期即时 fail-closed，不依赖后台任务；②**后台回收**：`service/membership_expiry.go`（启动一轮 + 定时 10 分钟）复用 MSW-3 回收核心（`revokeMembershipCore` 抽出 + 条件软删守卫，多实例安全）：软删 + allocation 级联解除 + `membership.revoke` 审计（trigger=`membership.expired`）+ 立即生效（权限缓存失效 + (user×tenant) refresh 吊销）；home 豁免并告警；③开关 `MEMBERSHIP_EXPIRY_RECYCLE_ENABLED`（默认开）/`MEMBERSHIP_EXPIRY_RECYCLE_INTERVAL_MINUTES`（默认 10，部署文档登记）。验证：service 3 用例 + middleware 2 用例；`./service` 全量、`./middleware`、`./handlers/auth`、`./handlers/user`、`./handlers/rbac`、`./router` 全绿（`rbac_precheck_gen.go` 重新生成）；实弹两轮（`scanned=1 recycled=1`、审计 `trigger=membership.expired`、allocation 级联解除、惰性判定复算 `permissions=[] source=none`）。方案：[msp-membership-expiry-recycle-plan.md](docs/multi-tenant/plan/msp-membership-expiry-recycle-plan.md)。

- **TUM-5 联调实例补强收口（TUM 全项完成）** — `run-tenant-user-admin-acceptance.ps1` 联调实跑 **PASS=30 / FAIL=0 / SKIP=2**（A4d/R4 设计内 SKIP；run-summary 入档）+ 浏览器逐步走查（S1 一次性重置+「待改密」徽标 → S2 强制下线 → S3 停用+立即下线；首管行开关禁用=最后管理员护栏）。**实跑修复脚本两处缺口**：① PowerShell 变量名大小写不敏感——解析后的 `$tenantId` 覆盖参数 `$TenantId`，首管/护栏分支误判为「复用租户」（引入 `$requestedTenantId` 快照）；② 新租户缺「模板开通」前置，`bootstrap-admin` 解析模板角色失败（500）——补 `POST /tenants/:id/provision` 步骤。验收前置登记：`LOGIN_RATE_LIMIT_PER_MIN≥30`（默认 10/分钟会误伤脚本单轮 10+ 次登录）。场景 09 与 TUM 方案同步回填。

- **IP-P2-8 收口：F-08 剧本 + 实弹验证（P1 完成）** — 新增 `scenarios/11-offboarding.md`（provider 自服务 / 平台治理双通道步骤表 + 反例 + 开关前置 + 实测记录）与 `scripts/msp/acceptance/run-offboarding-acceptance.ps1`（OFF-0–OFF-7：建号→分配→非平台面 403→offboard→停用/清零→幂等→审计，隔离探针可重复执行，run-summary 入档）。实弹结果：API 10/10 PASS、浏览器实操（TUM 抽屉离职 → 「撤销 1 个分配」→ 行刷新停用）通过；写通道开关未开时 403 `TENANT_USER_ADMIN_DISABLED` fail-closed 留证。实弹发现并修复：TUM 抽屉 `Modal.confirm` 静态调用不消费主题上下文 → 改 `App.useApp().modal`（控制台告警消除；抽屉 jest 6/6、tsc/eslint 绿）。IP-P2-8 仅剩 P2（`expires_at` 自动回收，另案）。

- **IP-P2-8 P1 完成：平台 TUM 面离职编排** — `POST /api/v1/tenants/:id/users/:userId/offboard`（`tenant:write` + super_admin 硬校验 + 最后管理员护栏；复用 `OffboardingService`：停用 + 全量会话失效 + 撤销全部分配；`user.admin_offboard` 治理审计（`target_tenant_id` 标注）+ 租户通知）；TUM 抽屉行操作「离职」（二次确认 + 撤销计数提示）+ `TenantUserAdminApi.offboardUser`。验证：service（护栏/双审计/幂等）、handler 契约、路由契约、e2e（幂等 + 非平台面 403 + 审计 2 行）、抽屉 jest 6 通过、tsc/eslint 绿；`go test -p 1 ./service ./handlers/tenant ./handlers/user ./router` 全绿。剩余：F-08 剧本补充。

- **IP-P2-8 P1 部分：前端「离职」入口 + 停用/删除路径会话缺口补齐** — ① 前端：`admin/users` 用户行操作新增「离职（停用并撤销全部分配）」（仅服务商租户展示，二次确认 + 已撤销分配计数提示），`UserApi.offboardUser` + i18n 双语；② 后端：`DeleteUser`（软删）与 `BatchUpdateUsers(deactivate)` 补全存量 access+refresh 吊销（与单条停用口径一致，方案 §2 S2/S3 缺口）。验证：jest `user-api`（16 通过）+ i18n（8 通过）+ `tsc -b`/eslint 绿；`go test -p 1 ./service ./handlers/user ./router` 全绿。剩余：平台 TUM 面 + F-08 剧本。

- **IP-P2-8 离职编排 P0 落地（分配回收单一动作）** — 新增 `POST /api/v1/users/:id/offboard`（`user:write`）：停用账号 + 全量会话失效（access 下限 + refresh 用户级）+ 撤销全部分配（`MSPAllocationService.RevokeAllForUser` 批量加载 + 逐条复用撤销核心，幂等、失败计入 pending 由巡检兜底）+ `user.offboard` 审计（1+N）。防自离职 409；跨租户/不存在 404；幂等重跑 alreadyInactive+0 回收仍留痕。测试：service 编排全量（2 分配回收 + membership 软删 + 审计 + refresh 吊销 + 幂等 + 防自离职）、handler 映射（200/404/409/1001）、router 守卫；回归 `-p 1 ./service ./handlers/user ./router` 全绿。方案 [msp-offboarding-orchestration-plan.md](docs/multi-tenant/plan/msp-offboarding-orchestration-plan.md)（P1：前端入口 + 平台 TUM 面）。

- **MSP 离职编排方案落盘（IP-P2-8；P1 剩余项「分配回收编排」）** — 现状核对 S1–S7：单点能力齐备（停用+token 吊销、单对撤销+membership 联动+审计+立即生效），缺编排；发现批量停用/删除路径 token 缺口与 F-08「无审计」过期表述。设计：停用先行（安全优先）+ 全量分配回收（逐条幂等、失败可续）+ `user.offboard` 审计；`POST /api/v1/users/:id/offboard`（provider 自服务，`user:write`）；决策点 D1–D4；分期 P0（服务+路由+测试）→ P1（前端+TUM 面+剧本回填）。批次登记 §5 P2 总表（[方案](docs/multi-tenant/plan/msp-offboarding-orchestration-plan.md)）。

- **存量租户权限目录处置（D1）决策材料落盘（IP-P2-7 §5.2 P0 收口）** — 实测证据（dev 只读 SQL：11 租户；非根 10 租户 × 2 平台家族码 `system:read/write`；全库零通配符；非平台根**零用户持有**，仅平台根 t1 有 3 用户属正当）；A/B 对比（A=裁剪须连带改 `ProvisionTenant` 克隆链 + 守卫 + 迁移，B=现状三重防线闭环）；**D1=B 定案**，A 预置预案与触发条件入档（[决策材料](docs/multi-tenant/plan/platform-permission-shielding-d1-decision.md)）；设计文档 v0.8 回填（§5.1/§5.2/§6.1/R1）。

- **建号角色多选与目标租户角色列表（IP-P2-7）** — 平台/MSP 建号弹窗的角色选择由**硬编码常量改为数据驱动多选**：新增 `GET /api/v1/tenants/:id/roles` 与 `GET /api/v1/msp/customers/:id/roles`（与建号写入同源的通道/rank 过滤：排除 `super_admin`/`sysadmin`、rank 上限、客户类型租户排除 `provider_tenant_admin`/`msp_*`、仅 active；RLS 按目标租户读取）。写入沿用既有 `roleIds` 通道（`CanGrantRoles` 校验）；`roleIds` 非空且未显式指定 `role` 时，服务端按最高 rank 的**枚举兼容**角色派生 `users.role`，home membership 角色按所选最高 rank 解析（`msp_role` 映射优先不变）；补齐 `roleIds` 平台角色越权防护（`super_admin`/`sysadmin` → 422 `ROLE_NOT_GRANTABLE`）。前端 `ProvisionUserModal` 新增「角色（可多选）」异步候选（加载失败告警、不阻断基础字段），`/admin/tenants` 与 `/msp/management` 两处接线（移除 provider 常量硬编码，`msp_role` 白名单保留单一源注释）。验证：service/handlers/user/middleware 预检/router 定向回归全绿；弹窗 7/7、租户页 4 套件 26 用例、tsc/eslint 0；联调实测客户租户返回 27 个候选（含租户自定义角色）、服务商租户含 `msp_*` + `provider_tenant_admin`，客户弹窗仅「角色（可多选）」、服务商弹窗「角色（可多选）+ MSP 角色」。方案：`docs/multi-tenant/plan/msp-provision-user-roles-multiselect-plan.md`。

- **《08 · 服务商账号与客户授权操作手册（用户级）》（`docs/multi-tenant/08-msp-user-operations-manual.md`）** — 面向界面操作的逐步手册：平台建号（含 MSP 角色）→ 分配到客户（客户域 membership/C2 自动落地）→ 员工「进入客户」深度切换与菜单/边界验证 → 解除分配（fail-closed）；含联调账号表、菜单对照实测、审计自查命令、C1/C2 角色映射附录与 FAQ。INDEX §0/§2 已登记。

- **《映射式多租户切换收口方案》（Membership + Switch，L4，v0.2 独立审查修订完成）** — 把"账号≠成员身份 + 映射式切换"从既有目标架构（canon I3/I5、目标架构 §3.2/§5.3、主方案 F7/F14/F15）收口到落地：现状核对 S1–S6（membership 表/权限单源/switch 会话/allocation/前端深度切换/跨租户面）；差距 MSW-1–6（**P0：建号与分配通道不写/不同步 membership → 切换后权限空窗**；切换授权仅 allocation；**生产无 membership 撤销面**；"返回工作台"不切回作用域且客户作用域下工作台必 403；真实双源＝请求期 RBAC 口径差；切换面/跨租户面边界未成文）；切换授权矩阵与边界决策表；批次 M1–M4 与验收 MSW-A1–A6；待决 MSW-Q1–Q4（Q4 已决）。独立审查（只读子代理）：needs-revision → 1 blocker + 7 major + 5 minor/nit 全处置（F-01–F-13，见方案 §8），修订后判定 ready-to-implement。

### Fixed

- **前端测试环境收敛：2 个环境诱发存量红清零（前端全量 4118 通过 / 0 失败）** — 根因：本地 shell 预置 `NODE_ENV=development`（jest 不覆盖已设置值），经 `tools/babel-plugin-import-meta-env.cjs` 的 `import.meta.env.MODE → NODE_ENV` 映射错位为 `isTest=false`/`isDevelopment=true`，诱发 `unified-config`（isTest 断言）与 `env`（jsdom 无 `performance.mark`）两套件红；CI（NODE_ENV 未设置 → jest 默认 test）本不受影响。修复：`jest.setup.js` 统一收敛 `process.env.NODE_ENV='test'`（与 CI 一致；测试内动态改写分支仍可用）；`devTools.mark` 增加 `typeof performance.mark === 'function'` 防御。验证：全量 jest 4118 通过、`tsc -b` 0、eslint 0。

- **MSP 分配历史端点补齐：`GET /api/v1/msp/allocations/history`（契约漂移修复；target-architecture §8 P1）** — 前端 `/msp` 页分配历史此前调用后端不存在的端点（仓库 `api-contract` 测试报 1 real contract drift）。补齐：service `ListHistoryForCaller`（含已解除记录；作用域与 `ListForCaller` 同口径：provider_admin 全 provider、员工仅本人；支持 `mspUserId`/`customerTenantId`/assignedAt 日期区间过滤，endDate 含当日；展示名批量 system 作用域解析避免 N+1）+ handler `GetAllocationHistory` + 路由（`msp_allocation:read`）。测试：`TestGetAllocationHistory_ScopeAndFilters`（作用域/过滤/含已解除/400）；`api-contract` 转绿（0 mismatches；2 条动态 base 解析器缺口按约定登记 `KNOWN_UNMATCHED_FRONTEND_PATHS`）。前端全量 jest 4116 通过（剩余 2 个存量失败：env/unified-config 环境项）。

- **用户角色下拉 rank 收口：`GET /roles` `grantable` 标注（IP-P2-7 收口补丁 9；§5.2 P1 剩余项）** — ① 后端：角色列表每项新增 `grantable`（rank ≤ 调用者；与写面 `CanGrantRoles` 同口径，缺省调用者角色按空角色收敛 fail closed），handler 注入调用者角色、service 计算、`RoleDTO` 透传；新增 `TestListRoles_GrantableFlagByCallerRank`（service）与 `TestListRoles_GrantableReflectsCallerRank`（handler）。② 前端：用户页创建下拉隐藏不可授予项；编辑下拉保留"用户当前持有但不可授予"项（禁选，防裸 ID 标签、不允许追加）；邀请弹窗同源过滤。验证：`./service`、`./handlers/rbac` 全量绿、`tsc -b` 0、eslint 0、前端全量 jest 4115 通过（3 个存量失败套件与本次无关：env/unified-config 环境项、msp-api `allocations/history` 契约漂移）。

- **前端展示层联动：权限目录 `assignable` 标注 + 角色页隐藏不可分配项（IP-P2-7 收口补丁 8；R5 收口）** — ① 后端：`GET /permissions` 的 `items` 新增 `assignable`（与写面护栏 `ensureAssignablePermissionsForNonPlatformRoot` 同口径：非平台根租户的通配/平台治理家族码为 `false`；平台根含 `code=default` 兜底全为 `true`），护栏谓词抽为 `isWildcardPermission`/`isPlatformGovernedPermissionResource` 单一源；新增 `TestListPermissions_AssignableFlag`。② 前端：角色页权限矩阵按 `assignable` 过滤（不可分配项不进选择矩阵），编辑含锁定码的角色时展示警示（保存不保留）；`PermissionCatalogItem` 类型同步；i18n 中英双语。验证：`./service` 全量绿（跳过已登记偶发 bpmn 用例）、handlers/rbac 绿、`tsc -b` 0、eslint 0、i18n 用例 8/8。

- **角色码归类完整性守卫（IP-P2-7 收口补丁 7；§5.2 P2 收口）** — 新增 `pkg/seeder/role_code_classification_guard_test.go`：扫描五路角色码定义源（`domainrole.All`、`middleware.RolePermissions`、`authz.BuiltinRolePermissionCodes`、`authz.MSPCustomerScopeRolePermissionCodes`、`seeder.BuiltinRoles`）并锁四条契约——① `msp_`/`provider_` 前缀 ⊆ MSP 词表（防"漏登记词表 → 客户侧按租户本地角色默认放行"）；② 平台保留码 ⊆ 平台词表；③ 词表条目反防腐化（必须存在于定义源）；④ 兜底表键归类（`domainrole.All` ∪ 平台 ∪ MSP ∪ 显式遗留白名单；历史键 `security` 登记为待收敛项）。可失败性已用临时探针验证（注入陈旧白名单 → CI 红 → 移除）。回归：`./pkg/seeder` 全量绿。

- **平台面 L1 注册表守卫 + 平台家族码逐家族评估（IP-P2-7 收口补丁 6；R2/R3 收口）** — ① **R2**：新增 `router/platform_face_guard_test.go`——平台面路由 L1 注册表（LLM 实例管理/运维命令/RLS 指标/定时器/权限词表写面共 20 条，逐条断言 `RequirePlatformAdmin` 行内或分组级挂载）+ 反向前缀检查（`/admin/operations`、`/admin/rls`、`/timers`、`/ai/providers`、`/permissions` 直接注册漏挂 L1 即 CI 红）+ handler 级平台面登记（租户治理写面 6 条，存在性防腐化）；`route_permission_guard_test.go` 扫描器扩展（全方法 + L1 检测，写路由过滤下移）。② **R3**：平台治理资源由单常量改为显式集合 `platformGovernedPermissionResources`（行为不变）；逐家族评估结论：`system` 纳入（平台面独占）、`system_config`/`mcp`/`connector` 为租户管理面显式依赖不纳入（其平台面由 L1 承载）、`menu`/`federation` 码空间不存在；决策锁用例 `TestPlatformGovernedResources_FamilyEvaluationLock`。③ 新增《[新增角色/权限/路由评审清单](docs/multi-tenant/plan/role-permission-extension-checklist.md)》（扩展纪律/评估表/守卫矩阵）。回归：`./router`、`./service` 定向全绿（service 跳过已登记偶发 `TestBiz_ListProcessInstancesByTenant`）。

- **平台根判定叠加根租户标识（IP-P2-7 收口补丁 5；D4=B）** — `saas_msp` 模式根租户（`code=default`）被 seeder 固定为 `msp_provider` 类型，原平台根谓词仅认 `internal`/`standard` → 根租户丧失平台面（平台角色不可见、`system:*` 分配 400，补丁 4 登记 D4/R6）。修复：`service/tenant_scope.go` 谓词拆分——新增 `isPlatformRootTenant(kind, code)`（`code=default` 兜底叠加）、`tenantScopeOf`（单查返回类型+标识）、`hiddenRoleCodesForTenant`；`role_service`/`user_service` 调用点改用叠加口径，**provider 语义仍按类型判定（不放大）**。测试：新增 `platform_root_tenant_test.go`（谓词矩阵 + 根租户平台治理恢复差分）；回归 `./service` 全量绿（唯一失败为已登记偶发 `TestBiz_ListProcessInstancesByTenant`，独立重跑 PASS）。**实弹复测（8090 重启二进制，真实账号零残留）**：tenant 1（`code=default`/`msp_provider`）平台角色可见（`super_admin`/`sysadmin`）、`system:write` 分配 400→200、普通码 200、临时角色建删 200/200、残留 0。设计文档同步 [platform-permission-shielding-design.md](docs/multi-tenant/plan/platform-permission-shielding-design.md) §5/§6/§7.4。

- **平台面权限屏蔽（IP-P2-7 收口补丁 4；安全）** — 在权限码之上为平台治理面叠加**身份硬校验**：新增 `middleware.RequirePlatformAdmin()`（主角色 + m2m 附加角色并集判定 `super_admin`/`sysadmin`，与权限码无关），挂接 LLM 实例管理组、运维命令（`/admin/operations/commands*`）、RLS 指标（`/admin/rls/stats`）、定时器（`/timers*`）与权限词表写面（`POST /permissions`、`/permissions/init`）；`RoleService` 新增平台治理资源码护栏（`resource=system` 非平台根不可分配，`ErrPlatformPermissionForbidden`→400，覆盖 Create/Assign/Update 三路径）。设计文档：[platform-permission-shielding-design.md](docs/multi-tenant/plan/platform-permission-shielding-design.md)（L1/L2/L3 三层防线、平台治理面清单、D1–D4 决策、R1–R6 风险）。**实弹（8090 真实账号，零残留）**：客户 admin `POST /permissions`→403（L1 文案）、`/permissions/init`→403、`/timers`→403、`/admin/rls/stats`→403、`/admin/operations/commands`→403、分配 `system:write`→400；服务商 admin `POST /permissions`→403、`/timers`/`/admin/rls/stats`→403；平台超管 `/timers`、`/admin/rls/stats`、`/admin/operations/commands`、`/permissions/init` 全 200。**登记**：`saas_msp` 根租户（`code=default`，type=`msp_provider`）被类型判定当作服务商 → 平台角色不可见、`system:*` 分配 400（实测）→ 设计文档 D4/R6，补丁 5 候选。回归：`middleware`（`require_platform_admin_test.go` 6 子用例）、`service`（`role_config_bypass_guard_test.go` 步骤⑤）、`handlers/rbac`、`router` 定向全绿。

- **角色配置面越权绕过收口（IP-P2-7 收口补丁 3；安全）** — 针对 `/admin/roles`「创建角色/改角色/配权限」的绕过审计发现并修复：① **保留角色码**：`roles` 表无 `(tenant_id,code)` 唯一约束（`ent/schema/role.go` 无索引）且 `UpdateRole` 允许改码——客户管理员可把自有角色改名为 `super_admin`，持有该角色的用户随即命中 `AuthorizeResource` 的 super_admin 直通（平台级越权链路）。修复：`CreateRole`/`UpdateRole` 全局拒绝平台/MSP 保留码（`service.ErrReservedRoleCode` → HTTP 400）。② **通配权限**：`POST /api/v1/permissions` 可自造 `*:*`（resource/action=`*`）再挂到自有角色，`checkPermissionMatch` 对 `*:*` 无条件放行。修复：非平台根租户禁止创建/分配通配权限（`service.ErrWildcardPermissionForbidden` → HTTP 400），覆盖 CreatePermission / AssignPermissions / UpdateRole(permissions) 三条路径。③ **租户治理写面**：`POST/PUT/DELETE /api/v1/tenants`（含 status/provision/bootstrap-admin）此前仅 `tenant:write` 权限粗筛，而客户/服务商租户的权限目录克隆后其 admin 角色**实际持有** `tenant:write` → 可跨租户创建/改删/供给租户。修复：新增 `handlers/tenant.Handler.requirePlatformAdmin`（super_admin/sysadmin 硬校验，主角色 + m2m 附加角色并集）→ 403。④ **用户面纵深**：`UserService.CreateUser/UpdateUser` 直连路径补平台角色护栏（HTTP 层原有 rank 护栏已拦 403/422，此为内部调用兜底），`msp_role` 仅服务商租户可写。**端到端实弹（8090，真实账号）**：客户 admin（tenant 11）`POST /tenants`→403、保留码建角色→400、通配权限→400、`role=super_admin`→422，`GET /roles` 正常 200；服务商 admin（tenant 10）`POST /tenants`→403、`code=sysadmin`→400；平台超管 `POST /tenants`（空体）→400 参数错误（护栏未误伤）、切入客户作用域后 service 层护栏 400/422；探测零残留（0 个 probe 租户/角色/用户）。回归：`service` 新增 3 用例（保留码/通配权限/用户面）+ 全量 `./service`、`handlers/tenant`、`handlers/rbac`、`middleware` 全绿。

- **非平台租户角色面泄漏平台角色与 MSP 专属角色（IP-P2-7 收口补丁）** — 产品口径：**客户不该知道 MSP 相关功能；客户与服务商租户都不该有超级管理员/平台管理员**。此前客户/服务商租户的 `GET /api/v1/roles`（角色管理页、用户建号/编辑角色选择器、权限页等 6 处消费）会下发模板克隆进来的 `msp_viewer`/`msp_tech`/`msp_specialist`/`msp_manager`/`msp_admin`（客户租户）与 `super_admin`/`sysadmin`（客户 + 服务商租户）。修复：词表单一源 `middleware.PlatformRoleCodes()/IsPlatformRole()` 与 `MSPOnlyRoleCodes()/IsMSPOnlyRole()`；`RoleService` 列表/详情/改删/授权按租户类型隐藏——平台根（internal/standard）不过滤、服务商隐藏平台角色、客户隐藏平台角色 + MSP 专属角色（按"不存在"处理、`total` 同步收敛、不泄露存在性）；`UserService.CanGrantRoles` 同口径拒绝授予（覆盖用户建号/编辑与建号通道 `roleIds`，422 `ROLE_NOT_GRANTABLE`）；`UserProvisioningService` 平台角色防护改同源词表、`GrantableRoles` 过滤同源。实测（联调，super_admin 切换作用域）：客户租户 **35→28（`mspOnly=[]`、`platformLeak=[]`）**；服务商租户 **34→32（MSP 六角色保留、`platformLeak=[]`）**；平台根不变（本无这两个角色行）。回归：`service`（新增 2 用例 + role/user/provision 既有用例）、`handlers/rbac`、`handlers/user`、`middleware` 全绿。

- **`service` 包 BPMN 业务流夹具固定 DSN（测试基建）** — `bizTestClient` 使用固定 DSN（`file:biz_<name>?mode=memory&cache=shared`），同进程同名夹具/`-count>1` 复跑时命中残留内存库，偶发 `tenants.code` 冲突（`TestBiz_ListProcessInstancesByTenant` 等）。改为复用既有唯一 DSN 范式（`uniqueTestDSN`，与 `svc_test` 共用原子计数），消除跨用例/跨复跑的库共享；全量 `./service/` 通过。注：同用例内 `process_instances.process_instance_id` 偶发同 tick 撞号（纳秒 ID 生成）为独立问题，另行观察（`-count>1` 可复现）。

- **分配管理「客户」列/解除确认显示为空（字段错配）** — 前端读取 `allocation.customerName`，而后端分配列表 DTO 字段为 `customerTenantName`，导致客户列为空、解除确认显示"对客户 undefined 的分配"；改为 `customerTenantName`（保留 `customerName` 兼容回退），类型定义同步（`itsm-frontend/src/types/msp.ts`）。浏览器实测：客户列=测试-客户、解除确认文案正确（2026-10-08 用户级手册全流程演练附带修复）。

- **深度切换菜单不变化（客户租户 msp 角色基线 C2 未落库）** — 根因：实施计划 §3.0-C2「客户租户内 msp 角色基线 → 客户侧业务权限」未实现——客户租户的 `msp_*` 角色随 default 租户克隆落入的是 C1 跨租户码（`msp:*`），深度切换后菜单投影与 provider 作用域一致（admin-demo01 两侧均 5 个 MSP 项），表现为"切换后菜单没有变化"。修复：①`internal/authz` 新增 `MSPCustomerScopeRolePermissionCodes()`（C2 基线：`ticket:read/create/update/write、knowledge:read、cmdb:read、service_catalog:read` + 任务基线；manager +`user:write/report:read`；admin +`cmdb:write/change:write`；文档 "ticket:read/write" 按客户侧路由守卫展开为 create/update）；②`pkg/seeder.ProvisionTenant` 对客户类型租户（`msp_customer`/`saas_customer`）执行 C1→C2 对账（移除 C1 残留、补齐 C2、保留自定义；幂等），`validateTenantClone` 按 C2 验收。实跑：`provision_tenant -tenant-id 11` 后 admin-demo01 深度切换菜单由 5 个 MSP 项变为 15 个客户侧项（工单/知识库/CMDB/服务目录等），客户作用域下 `/msp/workbench` 403（符合 canon §2.3 边界）；测试 `internal/authz` + `tests/parity` + `pkg/seeder` 全绿。

- **深度切换体验（MSW-A5）回归修复** — ①后端 `/auth/me` 归属租户契约（scenarios/10 §2）：`tenantId` 改为当前 JWT 作用域（切换后=目标租户）、`homeTenantId` 恒为归属租户；此前两者均取 `users.tenant_id`，刷新/会话引导后作用域被回退到 home，表现为深度态指示消失、目标域菜单不出现；②前端「进入客户」切换成功后落点由公开落地页 `/` 修复为登录后主页 `/dashboard`（此前切换成功但界面看不到深度态）。测试：`handlers/auth`（MSW-4 契约断言：`tenantId=目标租户/homeTenantId=归属租户`）、`CustomerFilter`（进入客户导航回归）全绿；浏览器实测：进入客户→`/dashboard` 深度态指示 + 目标域菜单 + `/auth/me` = `tenantId:11 / homeTenantId:10`。

### Changed

- **建号通道开关 `USER_PROVISIONING_CHANNELS_ENABLED` 默认开启（2026-10-08）** — 由"默认关（灰度）"改为"未设置/空/无法识别即开启；显式 `false/0/no/off`（大小写/空白容忍）才关闭回滚"（`service/user_provisioning.go` 新增 `channelsEnabledFromEnv` + 单测 `TestChannelsEnabledFromEnv`；`SetChannelsEnabled` 覆盖语义不变）。**联调实测（用户级手册 §3 通道）**：`.env` 注释显式行（进程内无此变量）重启 → 平台建号通道开放（重名建号 `409 USERNAME_EXISTS`，非 404）；显式 `false` 重启 → `404 PROVISIONING_CHANNELS_DISABLED`（回滚有效）；恢复默认后 `409` 复绿。文档同步：02 §3.1 开关登记默认值与 §10.2 初始化步骤、scenarios 00/04 前置、08 用户手册 §3 前置、MSP 验收脚本注释。

- **MSW-FU-1 实施：存量 membership 回填修复器（重放 20260504 回填 1/2 语义）** — 新增 `service/membership_backfill.go` + `cmd/backfill_membership`：活跃账号缺 home / 活跃分配缺客户作用域 → 复用 `MembershipSyncService` 幂等创建（`source=home`/`allocation` + `membership.sync` 审计）；异源存活行不覆盖（记 conflicts）；默认 dry-run，`-apply` 写库，`-reconcile` 校正同源 `role_id`/`account_kind`/`status` 漂移，`-json` 报告，Errors>0 退出码 1。口径修正：`accountKindForHome` 对 super_admin 固定 platform（canon D5，与巡检 `expectedKind` 同口径）。测试：`service/membership_backfill_test.go` 3 例（dry-run 零写入 / apply 补齐 / reconcile 校正 / 二次执行幂等无重复审计 / 异源不覆盖 / super_admin 口径），含"构造缺失 + `role_id` 空漂移 → 回填 → `membershipcheck` 0 漂移"闭环。**联调库实跑（.dev SSH 隧道 → `itsm_prod`）**：dry-run 31 home + 5 allocation → `-apply`（memberships 38→74，+36 审计）→ `cmd/provision_tenant -tenant-id 2` 供给缺失角色字典（36 角色）→ `-reconcile` 校正 → 巡检 **FAIL 36→0**（WARN 3→2，tenant 1 E2E 遗留 invite 账号为接受观察项）；报告刷新 `.dev/membership-drift-report.{txt,json}`。方案 v0.8。

- **MSW-6 收口（M4）：边界表入档 + 巡检脚本 + 开关登记 + 注释清理** — ①**边界入档**：canon 新增 §2.3"切换面 vs 跨租户面"边界决策表（工作台不切换 / 单客户深度切换 / 平台治理面 / Home 作用域 + Q4=A 返回语义 `homeTenantId`），工作台方案 §2.2 增补指针，新增 `scenarios/10-membership-switch-and-workbench-boundary.md` 剧本（深度切换→返回=切回 provider→客户侧只读成员→撤销反例）+ README/INDEX 登记；②**巡检**：新增 `internal/membershipcheck`（纯函数：`missing_membership`/`role_id_null`/`status_mismatch`/`customer_scope_conflict` 为 FAIL，`account_kind_snapshot`/`jwt_role_mismatch` 为 WARN；与写入路径 `accountKindForHome`、D10 角色映射同口径）+ `cmd/membership-drift-check`（只读；`-json`/`-strict`；FAIL>0 退出码 1）；夹具=scenarios 种子投影（0 漂移）+ 人工构造 6 类漂移分类断言；**demo 实跑样本**：`users=68 memberships=38 active_allocations=9` → FAIL=36（全部 `missing_membership`，M1 前创建的验收用户/历史分配未回填）WARN=3（default 租户类型异常引发的快照告警）；巡检发现登记 **MSW-FU-1**（存量回填修复器）/ **MSW-FU-2**（default 租户类型数据治理）；③**开关登记**：02 §3.1 登记 6 组过渡开关（owner/默认/退出计划），声明"巡检 FAIL=0 是关闭 `AUTHZ_STATIC_FALLBACK` 前置"；④**IP-P1-1 遗留注释清理**：Ent schema / config / middleware 三处"读路径仍回退 home+allocation"改为 membership 单源口径。方案 v0.6。

- **MSW-4/Q2=B 落地：返回工作台=切回 provider 作用域 + 客户侧"服务方成员（只读）"** — ①后端归属租户契约：切换响应与 `/auth/me` 新增 `user.homeTenantId`（`LoginUserResponse` / `common.User`，MSW-4 依据）；②前端"返回工作台"改为 `switchTenant(homeTenantId)` **成功后再导航**（失败不导航、保留会话并提示；旧会话回退 `user.tenantId`），深度态指示与退出入口语义同步；③`switchTenant` 失败回滚用例（user/currentTenant/isAuthenticated 不变）；④**Q2=B**：`GET /api/v1/users` 对客户类型租户新增 `providerMembers`（`account_kind=provider ∧ status=active ∧ 未软删`；复用 status/search/department 过滤；独立字段不计入分页 total；读通道失败降级不阻断主列表），`/admin/users` 新增"服务方成员（只读）"卡片（服务方标签、零管理动作、空集合零占位）。测试：后端 `TestUserService_ListUsers_ProviderMembersReadOnly` + 切换响应 homeTenantId 断言；前端 CustomerFilter（深度态两例）/auth-store（回滚）/ProviderMembersCard 共 3 套件全绿；API 实跑：切换响应/`/auth/me` home=10 ✓、客户管理员列表 `providerMembers=[admin-demo01 readOnly=true role=msp_tech]` ✓。方案 v0.5。

- **MSW-2/MSW-3 落地：切换授权单源 + 撤销联动（Q1=C / Q3=A）** — ①**切换授权单一入口** `internal/authz.DecideTenantSwitch`（纯函数决策表）：native / super_admin / provider+msp_role+（active allocation 或**客户侧显式确认的 invite membership**：`source=invite ∧ active ∧ invited_by 非空`，Q1=C）→ 放行；停用账号拒绝换发令牌；普通客户用户跨租户一律拒绝。②**全分支审计**：allow→`tenant.switch`（payload basis=native/super_admin/allocation/invite）/ deny→`tenant.switch_denied`（reasonCode=user_inactive/no_grant/target_not_found/target_suspended/target_expired）；未授权者不进入目标存在性分支（防枚举）。③**撤销面**：新增 `POST /api/v1/tenants/:id/users/:userId/membership/revoke`（TUM：`tenant:write` + super_admin 硬判定 + 写通道开关；home 不可撤销 409；allocation 来源级联解除分配；软删 `deleted_at+deassigned_at+status=suspended`）。④**Q3=A 立即生效**：撤销即权限缓存单点失效（`InvalidateMembershipPermissionCache`）+ **(user×tenant) 在途 refresh 吊销**（新增租户级 refresh cutoff，refresh 校验与用户级并联 fail-closed、作用域隔离）；M1 的 allocation 撤销路径同步挂接。测试：authz 决策表 14 例 / middleware 租户级吊销 / service 撤销+级联+缓存+refresh / handlers/auth 全分支审计 / TUM service+handler+路由契约全绿；`rbac_precheck_gen.go` 随路由重新生成。方案 v0.4（Q1/Q3/Q5 决议回填）。

- **前端复用收敛 #2：工单列表壳（`TicketListShell`）** — 抽取共享列表壳：antd Table 默认项（rowKey/loading/scroll/size/弹层容器）、空态文案、错误态（Alert + 重试）、行点击守卫（自动忽略按钮/链接/下拉/勾选区域）、页码分页透传与游标「加载更多」页脚；工作台平铺/分组/空组视图与客户侧 `TicketList` 改接同一壳——列定义、选择态、筛选、行内操作与数据获取仍由调用方注入。行为差异：工作台平铺视图的「加载更多」页脚由壳渲染（分组视图保留页级按钮）。测试：新增 `TicketListShell` 用例 4 例 + 相关 4 套件 20 用例全绿；`tsc --noEmit`/eslint 0；浏览器实测 `/tickets` 与 `/msp/workbench`（平铺/分组）正常渲染、无新增控制台错误。

- **前端复用收敛 #1 收尾：评论条目渲染单源（`CommentItemBody`）** — 从 `TicketCommentTimeline` 抽取评论条目通用渲染件（身份行/侧标签/内部标签/时间/已编辑标记/正文 + 头像/附加标签/底部/操作四槽位），工作台只读时间线与客户侧 `CommentPanel`（工单/事件详情 Tab）共用同一渲染源；客户侧头像、@提及计数、附件列表、编辑/删除按钮与编辑态经槽位注入，交互与 i18n 不变（`Paragraph` 内联渲染收敛到共享件）。测试：新增 `CommentItemBody` 用例（槽位/缺省标签/未知用户回退）+ 相关 7 套件 35 用例全绿（工作台/时间线/detail-tabs/TicketDetail）；`tsc --noEmit`/eslint 0；方案 v0.8 与 §13.1 状态同步。

- **单据流转 P2 评论只读面（XFD8）+ 前端复用收敛首件** — ①后端新增 `GET /api/v1/msp/tickets/:id/comments?customerTenantId=NN`（`MSPWorkbenchService.ListTicketComments`）：授权链与详情同口径（声明租户绑定 RLS + 资源租户一致性 + allocation 二次校验），可见性遵循 XFD8——公开评论双向互通（客户/服务商作者同一评论流），客户内部备注（`is_internal=true`）一律不下发；作者名与归属按客户/provider 双作用域解析（`authorSide`）；时间正序 + `total` 全量计数 + limit（默认 200/上限 500）。②前端：工作台详情抽屉新增「评论（只读）」区（与详情解耦加载、失败仅降级区块）；首个共享渲染件 `components/business/TicketCommentTimeline.tsx`（纯展示、无写入口、缺省作者侧标签 → 客户侧可复用）。③测试：服务层 `TestWorkbenchTicketComments`（公开/内部隔离/正序/limit/负例三连）+ A11/A12 路由级 e2e 评论断言（内部备注不下发）+ 前端 3 套件 16 用例（API 契约/组件/页面）全绿；`tsc --noEmit`/eslint 0。④方案 §13.1 新增「前端复用收敛清单」（授权与数据获取层刻意不收敛，呈现层共享）。

- **单据流转 P2 首批：通知深链直达详情（含跨租户写入守卫修复）** — ①provider 侧双投递 `Notification.action_url` 由工作台首页 `/msp/workbench` 升级为条目深链 `/msp/workbench?ticket=<id>&customerTenantId=<tid>`（非事务/Tx 两路径同源，`actionText`「打开工单详情」）；②**修复存量缺陷**：双投递在客户请求上下文（tenant=客户）写 provider 行（tenant_id=provider）被写路径租户守卫 fail-closed 拦截（联调实测 `cross-tenant insert blocked`，provider 侧通知实际未落库）——现按 `tenantctx.WithTenantID(ctx, provider)` 重绑后再查询/写入（RLS GUC 与写守卫同口径）；③前端铃铛 `NotificationDrawer` 点击按 `actionUrl` 直达（仅站内路径、防开放重定向），工作台读取深链参数自动打开详情抽屉（参数不参与列表过滤键，可分享/可刷新）；客户侧既有 `/tickets/:id` 不变。测试：`TestMSPDualDelivery*`（注册生产同款写守卫 + 客户租户 ctx，含守卫在线校验与深链断言）+ `TestMSP_A12_ProviderScopedTicketFlow` 路由级 e2e 深链断言 + 工作台深链用例（jest 10/10）；**联调+浏览器实测**：客户评论 → provider 通知落库（`actionUrl=/msp/workbench?ticket=224&customerTenantId=11`）→ 浏览器打开深链自动弹出详情抽屉（TKT-202610-000177：字段 + 动作可用性完整）。

- **单据流转 P1 首批落地（v0.5）** — ①**`workflow:*` 权限缺口修复**：`admin` 角色补 `workflow:read/write`（此前仅有 `bpmn:*` 导致工单"流程"页 403），`provider_tenant_admin` 由 `admin ∪ msp_admin` 派生自动继承；新增 `pkg/seeder.SyncBuiltinRolePermissionsForTenant/AllTenants` + `cmd/sync_role_permissions`（幂等增缺/删词表管理范围内过期授予、保留词表外自定义；dry-run 默认），联调 11 租户补 **112 条**授权、复跑 **0/0**；实测客户/服务商管理员权限面含 `workflow:read,write` 且 `GET /tickets/224/workflow/state` **403→200**（返回 canAccept/availableActions）。②**跨租户工单详情**：新增 `GET /api/v1/msp/tickets/:id?customerTenantId=NN`（handler + `MSPWorkbenchService.GetTicketDetail`）——声明租户绑定 RLS GUC、资源租户一致性、allocation 二次校验（与列表同口径，未分配/不存在同码防枚举），暂停客户可读但 `allowedActions` 全只读；发起人（客户租户）与处理人（provider 租户）姓名分作用域解析；新增服务层回归用例（放行/只读/未分配/租户不一致/404）。③**前端工作台详情抽屉**：行点击打开（动作按钮点击不冒泡）、只读投影（编号/标题/描述/状态/优先级/类型/发起人/处理人/时间/SLA）+ `allowedActions` 只读展示；`getWorkbenchTicket` API 契约测试 + 页面抽屉用例（10/10 全绿）。

- **单据流转 P0 四项落地（方案 v0.4 实跑）** — ①**终态 reply 契约修复**：`workbenchAllowedActions` 与写路径 `authorizeTicketAction` 同口径（resolved/closed/cancelled 对全部动作含 reply 一律拒绝），新增回归用例；②**快照回填 Go 版**：`service.BackfillTicketMSPSnapshots` + `cmd/backfill_ticket_snapshot`（dry-run 默认/`-apply`/`-tenant-id`，与 SQL 脚本同判据），联调 dry-run 0 候选（此前指派已幂等回填）；③**provider 首管默认分配**：`EnsureBootstrapAllocationsFor{Customer,Provider}`（幂等、active 唯一索引、审计 `msp.allocation_auto_assigned`）+ `TenantProvisioningService.Provision/CreateBootstrapAdmin` 双钩子 + `cmd/backfill_msp_allocations`（联调创建 1 条、复跑 0）；④**类型级自动路由**：`ticket_types.msp_route_enabled` + `tickets.msp_routed_at`（ent schema 重生成 + 迁移 024 + DTO/服务/前端类型表单开关），建单命中类型开关且客户托管时标记路由，工作台新增 `routedOnly` 过滤。**联调实跑 S1**：客户 11 类型 14 `general_incident` 开启路由 → 工单 **#224**（`TKT-202610-000177`）→ provider(10) 工作台 `all=2 / routedOnly=1` 且精确命中 #224。证据：`go test -p 1 ./service ./migration ./repository/ticket ./handlers/msp ./router` 全绿（新增 5 组用例）；`tsc --noEmit`/eslint 0；`TicketTypeFormModal` 9/9；`gofmt` 干净。剩余：`workflow:*` 权限缺口顺延 P1（方案 §13）

- **《多租户单据流转交互架构》v0.3：新增路由与交接设计、信息互通矩阵、端到端模拟与业务适配评估** — ①**§8.8 路由与交接**：三模式（自动路由默认/手动交接兜底/流程驱动复杂场景）+ 服务范围三级模型（租户/类型/工单级；未托管不外送、工作台默认按范围过滤）+ 复用现有分配规则引擎（`TicketType.auto_assign_enabled` + `ticket_assignment_rules`，建单钩子 `ticket_service.go:473-484`）扩展 `provider_queue`/`provider_user` 动作（按 allocation `primary→backup→specialist` 解析）+ 幂等单归属。②**§8.9 信息互通矩阵**：评论可见性模型（公开评论双向互通/内部备注租户内隔离——已核实 `canManageInternalComments` 对跨租户 MSP 员工按设计禁写客户内部备注；provider 内部备注存 provider 侧）；字段/参与人/配置三档边界（SLA 期限等"生效结果"共享，配置与原始审计不共享）。③**§8.10 端到端模拟与业务适配评估**：S1 标准托管（自动路由→认领→公开评论→状态回写→关单）、S2 流程驱动（服务商处理节点）、S3 反例边界（未托管/未分配/内部备注/暂停/终态/幂等）；ITIL 对照评估结论——主链路已适配，差距集中在接单确认、转派、详情协作、关单验收、provider 内部协作五处（对应 P0/P1/P2）。新增决策 `XFD7`/`XFD8`，canon 附录 C.2 与 INDEX 已同步

- **《多租户单据流转交互架构》v0.2：新增 §8 端到端流转设计（customer 建单 → provider 处理 → 状态回写）** — 补齐四维对齐（用户/角色/单据/流程：账号与 membership、租户内 RBAC 按目标租户计算、MSP 角色与 Allocation 角色语义）、读路径（可见性派生四入口 + 时序图 + provider Home 面不可见的澄清）、写路径（代码级 7 步授权链 + 工单状态机迁移表 + 回写事件/审计）、角色-动作矩阵、客户流程中 provider 任务的节点级设计（任务投影 + allocation 角色解析 + 写回）、通知双投递矩阵、端到端差距承接表（P0 快照回填/默认分配/终态 reply 契约修复 → P1 详情 → P2 分发/投影）。同时记录新缺陷：`allowedActions` 对终态工单标记 `reply=true` 与写路径终态一律拒绝不一致（`service/msp_workbench.go:438` vs `:615-617`）。文件 `docs/multi-tenant/plan/msp-cross-tenant-record-flow-architecture.md`（章节号顺延：原 §8–§13 → §9–§14）

- **新增《多租户单据流转交互架构》设计文档（L3）** — 把「工单跨租户可见/可操作」泛化为**全部单据类型**（工单/变更/问题/服务请求/审批任务/CMDB）的跨租户流转交互模型：`XF1–XF10` 交互规则（单一归属/关系显式/读写通道分离/条目级授权/状态机归属/通知双投递/审计四元组/快照策略/接入五件套）、五通道模型（Home/工作台/过滤器/头通道/深度切换 + 独立治理面）与选择决策树、通用操作矩阵与 API 命名规范、单据类型接入五件套、流程集成三模式（模板分发 copy-in / 跨租户任务投影 / 深度切换代管）、审计与验收（`XFA1–XFA6`）、分期落地（P0 权限缺口修复+快照回填+首管分配 → P1 跨租户详情 → P2 流程集成）。依据 2026-10-07 联调实测（allocation 前后工作台对比、#223 快照为空、provider/customer 各自 BPMN、工单流程端点 403）与三份代码/文档追踪报告；文件 `docs/multi-tenant/plan/msp-cross-tenant-record-flow-architecture.md`，INDEX §2 与 canon 附录 C.2（XF 编号空间）已登记

- **provider 首管默认角色改为组合角色 `provider_tenant_admin`（= `admin` ∪ `msp_admin`，修复 07:G11：服务商租户只见 4+1 个 MSP 菜单）** — canon §2.1 的 provider 双重定位（自身普通 ITSM 租户 + 跨客户 MSP 运营方）此前被首管策略打破：`msp_provider` 首管只绑纯 MSP 角色 `msp_admin`（10 个 `msp_*` 码 + task 基线），租户内 84 个菜单按权限过滤后只剩「服务台 + 客户管理 + 跨客户工作台 + 分配管理 + 审计看板」。现新增租户内组合角色 `provider_tenant_admin`（权限 = `admin` ∪ `msp_admin` 并集，由 `internal/authz.BuiltinRolePermissionCodes()` 显式派生，无平台通配符），`pkg/bootstrap` 首管策略改绑该角色（账号仍为 `admin` + `msp_role=provider_admin`，membership 仍记 `msp_role=msp_admin`）；`middleware` 硬编码兜底（`init` 派生并集）、`RoleRank`（rank 4）、菜单提权名单（`isElevatedMenuRole`）同步。存量租户由新增 `cmd/backfill_provider_admin` 幂等回填（角色行 + 权限并集 + 首管 membership 重绑 + `user_roles` 边 + 审计 `rbac.provider_tenant_admin_backfill`；dry-run 默认开启），并内置「首管可见菜单」自证探针。实测：demo01 首管菜单 5 → **42 主菜单 + 35 管理菜单**（工单/事件/问题/变更/知识库/CMDB/SLA/AI 与 客户管理/跨客户工作台/分配管理/审计看板双面可见）；幂等复跑 `perms_added=0 / admins_rebound=0`。证据：`go test ./internal/authz ./pkg/bootstrap ./tests/parity ./middleware ./pkg/seeder ./service ./handlers/user ./handlers/tenant` 全绿（新增组合角色并集 / 三源 parity / 首管策略 / 菜单提权 / MCP 兜底面用例）；`gofmt` 干净

- **租户模板供给批量化（N+1 消除：隧道环境从 >300s 超时未完成到 10.7s 完成）** — `pkg/seeder` 的 `ProvisionTenant` 由「逐行查询 + 逐行写入」改为「批量加载 + 内存差集 + 批量写入」（`CreateBulk` 按 500 行分片）：角色/权限/角色权限绑定/菜单/审批组/SLA 定义/CI 类型/审批工作流/流程定义与部署/流程绑定/Bot 模板与工具授权，以及提交前 `validateTenantClone` 全量校验；幂等语义、字段集与校验错误顺序保持不变。实测查询数：全新租户 **75**、幂等复跑 **59**（此前约 4–5k，隧道单查询 ~150–250ms → 分钟级）；HTTP 通道端到端实测 **10.73s**（`POST /api/v1/tenants/8/provision | 200`，此前两次 2m0s/5m0s 被前端中止后 500）。新增回归守护 `TestProvisionTenantQueryBudgetStaysBatched`（预算 <300 次）。证据：`go test ./pkg/seeder/ ./handlers/tenant/ ./router/ -count=1` 全绿；`gofmt`/`go vet` 干净；浏览器实测「开通模板」11.1s 完成 + 成功 toast

- **租户用户管理抽屉布局优化（`/admin/tenants`，TUM-4 反馈：用户名字段过窄）** — 原 10 列挤在 920px 抽屉内（实测用户名仅 78px、姓名/部门 29px）；改为身份优先布局：**用户**列固定左列 250px（用户名加粗 + 「首管」标记随行，姓名/邮箱作次级行，超长省略 + Tooltip），**角色**合并 MSP 角色、**状态**合并「待改密」徽标，**操作**固定右列，创建时间紧凑到分钟，列数 10→6；抽屉宽 920→1160 并随断点收敛（1160/1080/960/100%），表格 `tableLayout="fixed"` + `scroll.x=1024`。证据：`tsc --noEmit`/eslint exit 0；`admin/tenants` 4 套件 24 用例全绿；浏览器实测（1486px 无横向滚动、用户列 254px；1101px 抽屉 960、用户名/操作固定列生效）

- **首管口令恢复默认可用（TUM-D3 修订）+ 服务器侧兜底 CLI** — 平台侧租户用户管理写通道灰度开关 `TENANT_USER_ADMIN_ENABLED` 保持默认关，但 **`is_bootstrap_admin` 首管的重置密码拆为兜底恢复通道、默认可用**（防「租户唯一管理员口令丢失 → 锁死」）；非首管重置与启停/强制下线仍受开关约束（403 `TENANT_USER_ADMIN_DISABLED` 语义不变），`super_admin` 硬校验不变；开关关闭下走窄通道的审计标注 `channel=bootstrap_recovery`。新增 `cmd/reset_password`（默认目标=租户首管；目标租户密码策略校验 + bcrypt + 会话双吊销尽力 + 审计 `user.admin_password_reset`/`cmd:reset_password`）。证据：`go test -p 1 -gcflags=all=-c=1 ./service -run TestTenantUserAdmin` 全绿（新增 `TestTenantUserAdmin_ResetPassword_BootstrapRecovery`）；CLI 构建通过；部署文档/场景 09/方案 v0.10 已回填

### Fixed

- **MSW-1 写路径闭环：建号/分配通道补写 membership（"切换后权限空窗"修复）** — 新增 `service/membership_sync.go` 收口账号↔租户关系写入：`UserProvisioningService.ProvisionUser` 建号即写 home membership（`source=home`、account_kind 按账号类型、角色按 msp_role/users.role 解析、解析失败 NULL 入巡检清单）；`MSPAllocationService.Create/Deactivate` 与 `EnsureBootstrapAllocationsFor*` 同步 allocation membership（`source=allocation`、`allocation_id`、`is_default=false`；specialist→`msp_specialist`、其余→`msp_tech`）；撤销软删（`deassigned_at`+`deleted_at`，回收即失效）；幂等 upsert + 冲突不覆盖异源行；同步审计 `membership.sync`/`membership.revoke`（含 actor/scope/role/allocation 载荷）。测试：service 5 例（home 幂等/分配生命周期/冲突不覆盖/写路径租户守卫兼容/建号通道）+ `handlers/auth` 端到端（建号→登录→分配→切换目标租户权限非空且与 home 独立→撤销后再次切换被拒）；回归 `./service ./middleware ./handlers/auth ./handlers/user ./router` 全绿；方案 v0.3 实施记录回填（含分配角色映射口径差登记 MSW-Q5）。

- **`/tickets/types` 预设安装对租户管理员 403「权限不足」修复 + 页面入口权限门控** — 根因：`ticket_type:install_preset` 权限点在迁移 016 授权名单与角色模板 `internal/authz/roles.go` 中均遗漏 `admin`，租户管理员点「从预设安装」→ `POST /api/v1/ticket-type-presets/:id/install` 被 `RequirePermission("ticket_type","install_preset")` 拒绝 403（前端未捕获 → 未处理 Promise 拒绝）。修复：①`admin` 模板补该码（`middleware` 硬编码兜底同步）；②新增迁移 `023_grant_ticket_type_install_preset_to_admin`（权限行 + 授权行幂等回填存量租户；`AUTO_MIGRATE` 部署随启动应用，手动 `migrate -up` 后需重启以清权限解析缓存）；③前端按权限渲染「从预设安装」入口并为安装补错误处理；④同页 antd v6 弃用告警清理（`Alert message`→`title`、`List`→普通列表渲染）。证据：`go test ./internal/authz/ ./pkg/seeder/ ./middleware/ ./tests/parity/ ./migration/` 全绿（含 MCP 白名单补登记 `provider_tenant_admin` 继承面）；迁移 023 实测 applied；浏览器实测（客户管理员 `admin_demo_customer`/tenant 11）：`install → 200`、类型「通用事件/general_incident」入表、控制台无 403 与弃用告警；新增前端回归 `tickets/types/__tests__/index.test.tsx` 3/3，tsc/eslint 0

- **预设重复安装误报 400「请求参数错误」修复（上一 403 修复的后续）** — `InstallPreset` handler 把**所有服务错误**统一压成 `400 请求参数错误`，重复安装（编码已存在，服务返回 Conflict）被误报为参数问题。修复：handler 改走 `common.RespondError` 按 AppError 分流（重复安装 **409** / 未知预设 404 / 校验失败 400）；前端预设库按租户现有类型编码（**含已归档**，与服务端 `checkCodeExists` 口径一致）标记「已安装」并置灰入口，安装成功即时更新状态。证据：`service` 新增 `TestTicketTypePresetInstall_DuplicateIsConflict`（409 + ErrCodeConflict + detail=general_incident）；API 实测重复 `install` → `409/4090`；浏览器实测 `account-request` 安装 → 200、重开预设库「通用事件」「账号权限申请」均「已安装」置灰；前端用例 3/3、tsc/eslint 0

- **租户头与 JWT 作用域冲突的整页 401「租户不匹配」自愈** — 现象：前端残留旧租户头（如 `X-Tenant-Code: demo_customer`）而 JWT 已回到 home 作用域时，工单编辑页等所有租户域请求被中间件按设计 fail-closed 401（审计 `tenant.probe_denied`，2026-10-07 实测 34 条：`admin-demo01` → target 测试-客户，路径 `/tickets/:id`、`/tickets/:id/sla`、`/tickets/:id/configuration-items`、`/notifications`），页面持续报「租户不匹配」且不自愈（仅整页刷新可恢复）。修复：①`http-client` 识别 401 包络 `reasonCode=TENANT_MISMATCH_REJECTED` → 清理租户上下文并**重试一次**（回到 JWT 权威作用域），`HttpClientError` 透出 `reasonCode`；②`auth-store.login` 在无租户信息时同步清空租户上下文与 localStorage 键，消除「store=null / 旧租户头残留」分叉。证据：新增 `http-client.test.ts` 用例（旧头请求→401→自动清理→重试 200，且重试不带 `X-Tenant-*`）；`jest http-client + auth-store` 65/65、tsc/eslint 0

- **通知 WebSocket 票据端点对非 super_admin 角色 403「权限不足」修复** — `POST /api/v1/ws/ticket` 属「身份自省面」路由（仅需已认证身份、无 `RequirePermission` 声明，见 `router/route_permission_guard_test.go` 白名单），因此不在 RBAC 预检映射 `ResourceActionMap` 中；`super_admin` 直通掩盖了缺陷，其余全部角色（provider_tenant_admin / admin / customer_user / end_user）在预检 nil 映射处被 fail-closed 403，浏览器 Header 的通知 WS 全量不可用（控制台 `POST /api/v1/ws/ticket 403` + `[NotificationWS] Failed to get WS ticket`）。修复：`middleware/rbac.go` 在既有 menus/capabilities 特例旁增加身份自省放行（授权依据=已认证身份，票据仅绑定调用者本人会话）。证据：新增 `middleware/ws_ticket_identity_test.go`（4 角色放行 + 未映射普通路径仍 fail-closed）；`go test ./middleware -count=1` 全绿；浏览器实测（客户管理员 `admin_demo_customer` / tenant 11）`POST /api/v1/ws/ticket → 200`（票据 64 字符），刷新后控制台无 403；同族 `switch-tenant` / `change-password` 实测 400（预检已放行）确认修复范围仅此端点

- **租户开通向导「登录提示」用户名口径修正** — 已存在首管但非本次向导创建时（后台/CLI/API 建号，或重开向导），完成页不再用 `admin-<租户编码>` 猜测用户名（避免与实际账号不符，如提示 `admin-TURECOV010055`、实际 `turecov_admin`），改为「沿用已存在账号（见『用户管理』列表）」；默认用户名预览统一镜像后端 `bootstrap.sanitizeTenantCodeForUsername` 口径（小写、`.`/空格 → `-`、非法字符丢弃、编码为空回退租户 ID）。证据：`tsc --noEmit`/eslint exit 0；`TenantOnboardingModal` 9/9 绿（新增预览口径用例 + 已完成态提示断言）

- **修复租户开通「点击无反应」：前端超时被静默吞掉（`http-client`）+ 向导反馈增强** — `http-client` 将所有 `AbortError` 一律当作页面导航中止并 `return null`，导致 `POST /tenants/:id/provision` 超时被中止后调用方收到 null：开通向导步骤内容被清空、无错误提示、无成功提示（后端日志实测两次点击分别以 `latency 2m0s`/`5m0s` + `context canceled` 500 结束）。现区分**内部超时**与**调用方主动中止**：超时抛 `CLIENT_TIMEOUT`（「请求超时（Ns），请稍后重试」），页面导航/切租户/登出仍静默；向导新增「长事务」提示、loading 文案「开通中…」、成功 toast「模板供给完成」、失败 Alert 保留清单可安全重试（幂等）；供给超时 120s→300s。证据：`tsc --noEmit`/eslint exit 0；`http-client` 两套件 + 开通向导用例 55/55 绿（新增「超时显式抛错」「失败保留清单」两条回归）；浏览器实测超时提示可见。注：供给在 SSH 隧道环境仍 >300s（逐条查询延迟放大，约 3.5–4.5k 次），后端批量化优化列为后续项，慢链路可先用服务器侧 `provision_tenant` CLI

- **全仓 `tsc --noEmit`（含 `__tests__`）归零：强制改密守卫用例类型断言修复 + 本地陈旧测试文件清理** — 根 `tsconfig` 覆盖测试目录时暴露 `guards-must-change-password.test.tsx` 的 TS2339（`jest.Mock` 类型不含 `getState` 属性），改用具名形状断言 `{ getState: jest.Mock }`；同时删除 3 个被 `.gitignore` `*password*` 规则忽略、未被跟踪且引用已删除 API 的本地陈旧文件（`itsm-backend/service/password_policy_test.go`、`itsm-frontend/src/lib/api/__tests__/password-policy-api.test.ts`、`itsm-frontend/src/lib/hooks/__tests__/usePasswordPolicy.test.ts`；已跟踪的 `password-policy-api.ts` 与 `usePasswordPolicy.ts` 为在用源码，保留）。证据：`npx tsc --noEmit` exit 0；守卫用例 4/4 绿；`go test -p 1 ./... -run '^$'` 全包编译 exit 0

- **重建 `(main)/change-password` 页与强制改密守卫用例（`.gitignore` 漏提交第三/四例）** — IP-P1-5 的强制改密页与 `guards-must-change-password.test.tsx` 因 `.gitignore` 的 `*password*` 规则从未入库：`routes/index.tsx` 的 lazy import 悬空导致干净检出 `tsc` 失败（本机也缺文件）。本批按契约重建页面（旧密码 + 租户密码策略 + 清除本地 `mustChangePassword` + 回 `/dashboard`）与 4 条守卫用例（收敛/自锁豁免/放行/未登录），`.gitignore` 增补源码例外防复发。证据：`tsc --noEmit` exit 0（含全部既有代码）、守卫用例 4/4 绿

- **修复 `/auth/change-password` 服务端实现缺失（分支无法编译）** — IP-P1-5（`1654e1ae`）提交的 `handlers/common/handler.go` 引用了 `Service.ChangePassword`/`ErrOldPasswordMismatch`，但实现文件因 `.gitignore` 的 `*password*` 规则被静默忽略、从未入库，导致 `handlers/common`/`router` 在干净检出上编译失败。本批在 `handlers/common/service.go` 补齐：旧密码校验（`ErrOldPasswordMismatch`）→ 目标租户密码策略（`PasswordPolicyViolationError` 透出规则文案）→ bcrypt 落库 + 清除 `must_change_password` → 显式审计 `auth.change_password`；`internal/bootstrap/app.go` 注入 `SystemConfigService`；新增 `credential_change_test.go`（旧密码/策略/跨租户/成功+审计）；`.gitignore` 增加 `handlers/common` 源码例外防复发。证据：`go build ./...` exit 0；`go test ./service -run 'TenantUser|UserAdmin'`、`./handlers/common -run TestServiceChangePassword`、`./handlers/tenant ./router -run 'TenantUser|UserAdmin'` 全绿

- **平台侧租户用户管理掩码补强（TUM-3 复核）** — `middleware/mask.go` 密码族掩码规则补齐 camelCase 键名（`newPassword`/`currentPassword`/`confirmPassword`/`oldPassword`，兼容 snake_case），修复 `mode=specified` 重置密码时审计 `request_body` 可能落明文的问题（既有 `/users/*` 重置路径同受益）；新增 `middleware/mask_test.go` 与审计中间件 camelCase 断言（`middleware/audit_test.go`）。方案见 `docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md` v0.4

- **AI 会话标题恢复按首条用户 prompt 生成（跨分支回归修复）** — `ensureConversation`（B2-04 单一创建入口）新建会话时标题由固定 `"AI 对话"` 改为 `deriveConversationTitle(query)`：空白（含换行 / 全角空格）折叠单行、Unicode 码点截断 30 字并追加省略号、空输入兜底「AI 会话」（`handlers/ai/conversation_title.go`）。背景：该能力此前只在 `feat/vite-migration` 分支（`d3471221`）落地、未合并进当前分支 `feat/bot-mcp-integration`，后端二进制从当前分支重建后标题退回固定占位（库中历史会话全部同名）。已有历史会话标题不变；`Chat` / `ChatStream` 共用同一创建入口，`done` 事件后前端 `loadConversations()` 刷新即得新标题。证据：`gofmt -l handlers/ai` 无输出、`go build ./...` exit 0、`go test ./handlers/ai/ -run 'TestDeriveConversationTitle|TestEnsureConversation' -count=1` 全绿（含 `ensureConversation` 标题推导断言）

### Changed

- **知识库发布 / 版本语义收敛：版本 = 发布历史** — 版本快照只在发布动作中产生：`POST /knowledge/articles/{id}/publish` 支持可选请求体 `changeLog`（缺省由服务端生成「首次发布」/「发布更新」，版本作者记为发布人），文章首次发布即 v1；`PUT /knowledge/articles/{id}` 保存不再产生版本，并一律把文章置回草稿——已发布文章编辑保存后自动下架，必须重新发布才对外生效（请求体 `status` 被忽略，发布态只能由 publish / unpublish 两个动作改变，避免保存动作绕过版本记录把未发布内容放出去）；`unpublish` 只切换可见性、幂等且不产生版本；内容与最近发布版本一致（title / content / category / tags 全等）时重复发布或原样重新上架不占用版本号，内容变化才追加新版本；`versions/{version}/restore` 回落为草稿而不是静默追加版本。向量 outbox：发布 / 下架与 `vector_index` 同步 / 移除命令同事务落库，草稿变更按移除处理，知识检索只承载已发布内容。前端：编辑已发布文章提供「保存并重新发布」（写回内容后立即发布，内容未变时不占用版本号）与「保存为草稿」（下架待发布，保存后提示重新发布）双动作，表单顶部说明两种保存语义，详情页草稿态告警，发布确认与版本控件（发布 / 恢复 / 版本说明）文案对齐新语义；`KnowledgeBaseApi.getCategories()` 把后端字符串数组归一化为 `{id, name}`（修复编辑 / 新建页按对象解析失败后把文章分类回写成 `"1"`、进而让下一次发布凭空多出内容差异与版本的问题）；`docs/api-reference.md` 同步改写（新增发布 / 下架接口，改写保存 / 版本历史 / 恢复口径），`PublishArticleRequest.scheduleAt` 调整为 `changeLog`。证据：`go build ./...` exit 0、`go test ./handlers/knowledge/ -count=1` ok（`version_test.go` 全链路：创建→保存无版本→发布 v1→重复发布幂等→编辑回草稿→发布 v2→下架→原样重发不产生 v3→恢复 v1 回草稿→发布 v3→版本对比→跨租户 404）与 `outbox_test.go` 全绿、`go vet ./handlers/knowledge/` 无输出；`npx tsc --noEmit` exit 0、`npx eslint`（6 个改动文件）无输出、`npx jest`（knowledge-base-api / useKnowledgeBase / knowledge-new-article-prefill）3 套件 84 用例全绿；运行时复验（重启后端二进制后）：临时文章「创建 → 发布 v1 → 原样保存回落草稿且版本仍为 1 → 原样重发仍为 1」，浏览器点击「保存并重新发布」后分类 / 正文原样、版本不新增

- **多 LLM Provider P1：选择器读端点与 `provider` 覆盖参数降为 `ai:read`（全员可切换）** — `GET /api/v1/ai/providers/available` 与 `GET /api/v1/ai/user-preference` 由 `system:write` 降为 `ai:read`（`router/llm_provider_routes.go` 拆读/写权限组；实例 CRUD、连通性测试、设默认、`import-static` 与 `PUT /user-preference` 仍在 `system:write` 组），`/ai/chat[/stream]` 的 `provider` 覆盖参数门禁由 `HasSystemWritePermission` 改为新增 `HasAIReadPermission`（`middleware/llm_provider_permission.go`，与读端点同源，403 文案改为「无 AI 使用权限，不能指定 provider 实例」），`go run ./cmd/authz-gen` 重生成 `middleware/rbac_precheck_gen.go`（两行预检 `system:write → ai:read`）。前端 `use-llm-provider-feature.ts` 去掉 `hasPermission('system','write')` 探测门控（全员探测、失败仍 fail-closed），`AIChat.tsx` 选择器改为「开关开启 + ≥2 可用实例」即渲染，「设为 / 清除我的默认」写操作与「LLM 模型」页签仍限管理员。网关与数据模型零改动。证据：`go build ./...` exit 0、`go test ./middleware/ ./router/ ./handlers/ai/ -count=1` 与预检新鲜度 / 对齐守卫全绿、改动文件 `gofmt -l` 无输出、`npm run type-check` exit 0、3 套件 53 用例全绿；实现见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑳ / §10 v1.24

- **部署默认值：LLM 多 Provider 与 AI 能力默认开启（新部署免手工改 env）** — `docker-compose.prod.yml` / `docker-compose.dev.yml` 的透传默认值由 `false` / 空改为 `LLM_MULTI_PROVIDER_ENABLED=${LLM_MULTI_PROVIDER_ENABLED:-true}`、`ITSM_AI_READY=${ITSM_AI_READY:-1}`，`.env.prod.example` 同步为 `true` / `1` 并注明回退方式（在 `.env.prod` 显式设 `false` / `0`，无需改 compose）。背景：线上 1.8.0-dev 排查确认这两个键未设置 / 未透传时，前端 AI 菜单与「系统配置 → LLM 模型」页签、AI Provider 选择器整体不可见，每次部署需人工检查修改。边界：**代码层默认不变**（`config.yaml` / `service.MultiProviderEnabled()` 默认关、非法值按关；灰度契约 D9 与 QA-3「开关关闭」门禁不受影响，非 compose 部署不受影响）；启动硬约束矩阵不新增失败模式——开关开启只会放宽（「静态密钥占位符 + 生产 + 有可用 DB 实例」由 Fatal 转 Warn，反向不成立；`service/llm_provider_startup.go:60-68` 与 `internal/bootstrap/app.go:620-642` 逐行核对）。证据：服务器 `docker compose config` 渲染核验（含 `.env.prod` 叠加结果）、后端容器 env 实测、`/api/v1/capabilities`（`ai.deploymentReady=true`）与 `/api/v1/ai/providers/available`（200，2 实例）回归通过；本地仓库产物 compose 渲染校验 `config --quiet` exit 0；实现见 `docs/plan/multi-llm-provider-plan.md` §4.3 ⑤ / §10 v1.25

### Added

- **TUM-5 收口：路由器级 API 通道 e2e（TUM-A1–A8 + 反例）** — 新增 `itsm-backend/router/tenant_user_admin_e2e_test.go`：在路由器级跑**完整中间件链**（Auth → RBAC → TenantMiddleware → handler → service，ent/sqlite 内存库、无外部环境、CI 可复跑），覆盖 A1 列表/检索/详情、A2 一次性重置（旧口令失效/新口令可验/强制改密/refresh 双吊销/二次生成前值失效）、A3 指定重置（400 策略拦截 + 审计明文掩码）、A4 启停与护栏（409 LAST_ADMIN_PROTECTED / SELF_OPERATION_FORBIDDEN）、S3 强制下线、A5 审计按 `target_tenant_id/target_user_id` 复核、A8 通知触达（不含口令）与列表 total ↔ 配额 users 对账，以及反例（403 PLATFORM_SCOPE_REQUIRED / 404 TENANT_USER_NOT_FOUND / 400 PASSWORD_POLICY_VIOLATION）。证据：`go test -p 1 ./router -run TestTUM -count=1` 全绿；`./router` 全包回归绿；gofmt 无差异。联调实例 run-summary 与浏览器逐步截图降级为可选补强（脚本已就绪）。方案 v0.8

- **TUM-6/TUM-7 收口（实施映射 + P2 评估）** — 实施方案新增 TUM 承接映射（`docs/multi-tenant/plan/msp-implementation-plan.md` §3.0-G / 修订 v1.71：读通道/治理/审计/前端/验收/灰度与 IP-P1-5、IP-P0-8/10、IP-P1-8 的对应关系）；TUM-7 P2 评估纪要（删除用户 / 跨租户角色调整 / 导出清单三项均不做实现，登记触发条件，见 TUM 方案 §10 / v0.7）；至此 TUM-1–TUM-7 全部收口（仅 TUM-5 live 部分待联调环境）

- **平台侧租户用户管理验收与闭环（TUM-5 + C5）** — 新增 C5 通知触达：治理动作（重置/启停/强制下线）向目标租户**启用中的管理员**发站内通知（类型 `user_admin_action`，排除被操作用户自身、不含口令；best-effort，失败仅告警不回滚治理动作），`TenantUserAdminService.SetNotifier` 端口 + bootstrap 注入 `NotificationService`；新增 S1–S3 闭环用例（`ClosureS1S2S3`：旧口令失效/新口令可验/强制改密/refresh 双吊销/通知/审计 5 行按 `target_user_id` 复核）与通知用例（`NotifiesTenantAdmins` / `NotifierFailureDoesNotBlock`）；浏览器实操剧本 `docs/multi-tenant/scenarios/09-tenant-user-management.md`（S1–S3 步骤/预期/API 验证/反例/对账/已知限制）；API 通道验收脚本 `scripts/msp/acceptance/run-tenant-user-admin-acceptance.ps1`（覆盖 TUM-A1–A8 + 反例 403/404/409×3/400，默认新建隔离租户可重跑，PowerShell 语法校验通过）。证据：`go build -p 2 ./...` exit 0；`go test -p 1 ./service -run TestTenantUserAdmin -count=1` 9/9 绿（含闭环/通知）；`./handlers/tenant ./router ./handlers/common` 定向绿。live API run-summary 与浏览器实测待联调环境（本机 Docker 守护进程不可用、PostgreSQL 未运行）

- **平台侧租户用户管理前端（TUM-4）** — 新增 `TenantUsersDrawer`（宽 920：搜索 300ms 防抖 + 状态/角色筛选 + 分页；深链 `?tenantUsers=<id>` 与 `tu*` 筛选参数 URL 同步；启停乐观更新 + 失败回滚；最后管理员前端禁用 + 后端 409 兜底；`default` 租户停用二次确认；强制下线；本页批量 ≤50 + 逐条结果 + 失败重试；空态三分；403 平台面说明）、共享组件 `UserStatusToggle`/`ResetPasswordModal`（`admin/users` 改为复用，行为不变）与 API 客户端 `tenant-user-admin-api.ts`（契约锁定 URL/载荷 + §3.5 错误码文案映射）。测试：抽屉 5 用例 + API 契约 9 用例 + 守卫 4 用例；`tsc --noEmit` exit 0、eslint 0 error、目标 jest 全绿（含 user-api/TenantUsageModal/TenantOnboardingModal/InvitationManagementModal 31 用例回归）。实现见 `docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md` v0.5

- **用户侧 Bot 能力落地（B0–B4，功能分支 `feat/bot-mcp-integration`）** — 用户侧 Bot 从工具元数据到运行维度看板的完整落地，`bot.enabled=false`（默认）时零行为变化、零路由：**B0** 工具元数据单一来源（14 个内置工具全量标注 + 调用时快照）与 `tool_invocations` 联合迁移；**B1** `bot_runs`/`bot_steps`/`bot_events` 运行档案、确认五态状态机与 TTL、持久队列与重启恢复（防双执行）、SSE v2 事件注册表（未知事件忽略契约）与前端确认抽屉/证据面板/审批页；**B2** `bot_templates`/`bot_tool_grants` 治理与 `BotPolicy` 四重交集（授权 ∩ RBAC ∩ 风险上限 ∩ 入口，下发与执行同源）、越权负向安全集、`/admin/bots` 管理页与工作区 Bot 选择器；**B3** 六入口枚举与上下文协议（目标预检 fail-closed、协议键覆写留痕、`bot_runs.entrypoint/target_type/target_id` 落库）、工单/事件/CI 三处 `AskAILauncher`、场景 pilot S1 工单助手 / S2 事件值班助手（只建议不自动变更，负向断言）/ S3 知识自助助手（不自动发布，负向断言）与 plan/analysis/draft 三类工具（`bot_artifacts` 产物表，零业务写入）；**B4** `e2e/` 双通道 E2E 章程与 run-summary（api 6/6 实跑通过）+ CI `e2e-bot.yml`、运行维度指标 `GET /api/v1/ai/bot-metrics` 与 `/ai/bot-metrics` 看板（成功率/确认率/回读失败率/工具错误率/时延/成本代理，`bot.enabled=false` 降级 503）。证据：`docs/plan/evidence/bot-b0/`～`bot-b4/`（含逐任务证据与 run-summary）；方案：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`
- **多 LLM Provider 支持（后端 BE-1..BE-7 落地，默认关闭）** — 新增租户级 `llm_provider_configs` / `llm_user_preferences` 两表（ent schema + expand/down 迁移；密钥 AES-GCM + `tenant_id:name` AAD 加密落库，读接口仅掩码回显）与 `LLMProviderRegistry`（TTL 30s 缓存 / 失效 / 解密失败降级不 panic）+ `LLMGateway` 运行期解析（显式 `provider` 失败不回退；`个人默认 → 租户默认 → 静态配置` 降级链并回带 `providerSource`；旧方法零改动）。管理 API 挂 `/api/v1/ai/providers*`（列表 / 新建 / 更新 / 软删 / 连通性测试 / 设默认 / 幂等 `import-static` / `available` / `user-preference`，统一 `system:write`）；`/api/v1/ai/chat[/stream]` 支持可选 `provider` 覆盖（非 `system:write` 返回 403 `AI_PROVIDER_FORBIDDEN`，SSE `done` 事件回带 `provider`/`providerSource`）；`ai_llm_calls.provider_key` 落库并按 provider 聚合统计（静态回退写 NULL，开关关闭时字段省略）。协议按 4 种 API 形态建模（P0：`openai_chat_completions` 适配器 + `anthropic_messages` 走既有 minimax 分支；`openai_responses` / `google_gemini` 预留 422）；启动硬约束矩阵扩展（生产 + 占位密钥 + 无可用 DB 实例 → 拒绝启动）。灰度开关 `LLM_MULTI_PROVIDER_ENABLED`（config `llm.multi_provider_enabled`，默认关闭、env 优先、非法值按关；关闭时零路由零装配、行为与静态单 provider 逐字节一致）。RBAC 预检生成物同步（`go run ./cmd/authz-gen`：`/api/v1/ai/providers*` 预检由 `ai:read/ai:write` 回退修正为声明的 `system:write`）。测试全绿：`go test ./internal/llm/protocol/... ./internal/bootstrap/ ./middleware/ ./router/ ./handlers/ai/ -count=1` + service 定向集（registry / gateway / 协议槽位 / 启动矩阵 / 遥测聚合）60+ 用例；实现与验收详见 `docs/plan/multi-llm-provider-plan.md` §10 v1.10
- **多 LLM Provider 前端（FE-1..FE-4，QA-3 零破坏门禁）** — 新增 `src/lib/api/llm-provider-api.ts`（10 端点 + 与后端 DTO 对齐的类型 + 错误码/HTTP 状态 → 可读中文提示，含 `AI_PROVIDER_DISABLED` / `AI_PROTOCOL_NOT_IMPLEMENTED` / `AI_ADAPTER_OPTIONS_INVALID` 等）与 `src/lib/hooks/use-llm-provider-feature.ts`（仅 `system:write` 用户探测 `available` 端点；404/403/503/网络错误一律 fail-closed，普通用户 **零新增请求**）。「系统管理 → 系统配置」新增「LLM 模型」页签：列表（掩码密钥 / 连通状态 / 默认标记）+ 新建 / 编辑 / 连通性测试 / 设默认 / 启停 / 软删 / `import-static` 导入，未实现协议选项置灰、422 错误码转可读提示、密钥单向写入不回显。AI 会话页新增 provider 选择器（仅开关开启且 ≥2 个可用实例时渲染）+「设为我的默认 / 清除默认」+ 回答气泡生效实例标签（`done` 事件 `provider`/`providerSource` → 实例名 + 来源：会话指定 / 个人默认 / 租户默认 / 静态配置）；`/ai/chat[/stream]` 仅在显式选择时携带 `provider`（未选择时请求体与现状**逐字节一致**），实例被禁用/下线时清选择、提示并回退默认。零破坏：`src/routes/index.tsx` 与 `docs/plan/_data/vite-route-map.csv` 零 diff。证据：`npx tsc --noEmit` exit 0；`npx eslint`（7 个改动文件）无输出；`npx jest src/lib/api/__tests__/ai-api.test.ts src/lib/hooks/__tests__/use-llm-provider-feature.test.ts src/lib/hooks/__tests__/use-permissions.test.ts --silent` 3 套件全绿（ai-api 41 用例，含 SSE/降级请求体字节一致与 `onDone` 回调向后兼容用例）；实现与验收详见 `docs/plan/multi-llm-provider-plan.md` §10 v1.13
- **多 LLM Provider QA-3 开关关闭全链路回归（零破坏门禁，证据归档）** — 新增 `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`（13 项门禁维度：开关语义 / 无新路由 / 旧响应形状 / 前端零新 UI 零请求 / 请求体逐字节一致与 `onDone` 回调兼容 / 路由与 route-map 零 diff）与 `itsm-backend/router/llm_provider_routes_test.go`（handler=nil ⇒ `/api/v1/ai/providers` 404；handler 注入 ⇒ 401 进入鉴权链），把「开关关闭无新路由」从注释约定升为测试断言。复跑证据：后端六组测试全绿（service 定向集 / `handlers/ai` / `router` / `internal/bootstrap` / `middleware` / 新增 router 用例）、`npx tsc --noEmit` exit 0、`npx eslint` 无输出、`npx jest`（ai-api / use-llm-provider-feature / use-permissions）3 套件 94 用例全绿。收口见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑪ 与 §10 v1.14
- **多 LLM Provider QA-2 端到端验收（API 级链路用例 + 验收记录）** — 新增 `itsm-backend/handlers/ai/llm_provider_qa2_flow_test.go`（`TestLLMProviderQA2RuntimeSwitchFlow`）：内存 SQLite + `httptest` 桩、**不连生产库 / 不连真实 LLM**，按生产同构接线（registry 兼作管理面 `Invalidator`、`gateway.WithResolver`）把「管理 API 写入 → 缓存失效 → 运行期解析 → 真实 HTTP 出站」串成单条链，十段断言锁定显式覆盖零回退、个人默认优先、禁用降级、删除后显式引用不可见、无实例租户静态回退（`providerSource=static`）。逐场景状态矩阵（含场景 7/8/9/10/11 由 QA-1 / QA-3 / 适配器对照测试承接的证据）与人工遗留（浏览器级 UI 复核 / 真实 Ollama·Anthropic 桩联调 / 运行期日志明文检索）见 `docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md`。证据：`go test ./handlers/ai/ ./router/ ./internal/bootstrap/ ./middleware/ -count=1` 四包全绿 + `go test ./service/ -run 'LLM|Provider|Protocol|Registry|Gateway' -count=1` + `go test ./internal/llm/protocol/... -count=1` ok、`gofmt -l` 无输出。收口见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑫ 与 §10 v1.15
- **LLM 协议适配层解冻与首适配器 PA-1 `anthropic_messages`（独立计划升 v1.0）** — `itsm-backend/internal/llm/protocol` 新增 Anthropic Messages 适配器（官方 + `minimax` 变体）：`x-api-key` + `anthropic-version: 2023-06-01` 鉴权、system 提示提升顶层、工具结果转 `user`/`tool_result` 块、工具声明用 `input_schema`、OpenAI 风格 `tool_choice` 归一为 `{type:tool,name}`、事件驱动 SSE 解析（`text_delta` / `thinking_delta` / `input_json_delta` 增量累积）与非流式 `content[]` 文本块拼接、带内 `error` 事件映射 `*ProtocolError`；minimax 变体沿用 `maxTokens` / `stopReason` camelCase 字段与缺省端点 `https://api.minimaxi.com/anthropic/v1`（新增 `protocol.DefaultEndpoint(protocol, variant)`，官方缺省 `https://api.anthropic.com`），service 层按变体解析缺省参数（`maxTokens` 4096；`temperature` minimax 1.0 / 官方 0.3）并把 `(anthropic_messages, "")` / `(anthropic_messages, minimax)` 两槽位注册进构建分派（构建分派泛化为「协议包注册表优先、未注册回退旧分支」，DB 实例路径的 anthropic 变体由此一并改由适配器承载，`TestLLMProviderRegistryProtocolVariantMapping` 同步更新期望）；`LLM_PROTOCOL_ADAPTER_ENABLED` 开关语义不变（默认关、关闭时构建路径不经过协议包），无 DB / API 契约改动。等价性对照测试 `service/llm_anthropic_equivalence_test.go` 锁定与既有 `MiniMaxProvider` 在请求路径 / 鉴权头 / 请求体字段 / 返回文本上一致，协议单测 `internal/llm/protocol/anthropic_messages_test.go` 覆盖非流式 / 流式 / 工具累积 / 带内错误。独立计划 `docs/plan/llm-protocol-adapter-plan.md` 由 Stub 升 v1.0（详细设计 §4 + PA-1..PA-6 拆解 + 差异清单 + 验收证据），门禁注记见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑭ 与 §10 v1.18

- **LLM 协议适配层 PA-2：适配器维度收敛「一协议一实现」+ `openai_chat_completions` 变体承载（独立计划升 v1.1）** — `itsm-backend/internal/llm/protocol/registry.go` 注册表键由 `(protocol, variant)` 改为每协议一个 `AdapterSpec{Variants, New}`（`Supports` / `NewAdapter` 按协议 spec + 变体白名单判定，变体差异在构造期固化；`azure` / `ollama` 不再新增独立适配器文件），默认注册表登记 `openai_chat_completions{"", azure, ollama}` 与 `anthropic_messages{"", minimax}`，`DefaultEndpoint` 增补 ollama 缺省地址 `http://localhost:11434`（与旧 `LocalProvider` 同值）。接线层 `service/llm_registry.go` 新增 `normalizeVariantEndpoint`（azure 仅主机名 endpoint 补 `/openai`，与旧 `AzureProvider` 同址；已带路径原样透传）与 `llmProviderModelForVariant`（azure `model` 缺省回退 `deployment`），静态配置与 DB 实例两条路径共用。等价性对照 `service/llm_openai_variant_equivalence_test.go` 锁定 azure 与既有 `AzureProvider` 在请求路径 / 鉴权头 / 请求体字段 / 返回文本上一致；ollama 为**有意差异（协议归一）**——旧 `LocalProvider` 走 Ollama 原生 `/api/chat`，适配器统一走 OpenAI 兼容 `/v1/chat/completions`（Ollama 官方支持；`max_tokens` 4096 / `temperature` 0.3 沿用 BE-9 默认值表），差异逐条登记于独立计划 §4.3（PA-2 六条）；`adapter_options.api_version` 仍只存储回显（**不引入** `api-version` 查询参数），`LLM_PROTOCOL_ADAPTER_ENABLED` 开关语义不变（默认关，关闭时构建路径不经过协议包、仍走旧分支，可回退）。证据：`go test ./internal/llm/protocol/... -count=1` ok、service 定向集（Anthropic / Azure / Ollama / ProviderVariant / ResolveProtocolURL / Registry / Protocol）全绿、`go build ./...` + `go vet ./internal/llm/protocol/... ./service/` exit 0、改动文件 `gofmt -l` 无输出。独立计划 `docs/plan/llm-protocol-adapter-plan.md` 升 v1.1，门禁注记见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑮ 与 §10 v1.19
- **LLM 协议适配层 PA-3：`openai_responses` 适配器 + 槽位放量（独立计划升 v1.2）** — `itsm-backend/internal/llm/protocol/openai_responses.go` 新增：以**官方 Responses API** 为载体（`POST /v1/responses` + `Authorization: Bearer`，缺省 endpoint 与 `openai_chat_completions` 同址回退 `https://api.openai.com`，资源路径由适配器给出；**不复刻** ChatGPT backend 形态），前导 system/developer 合并为顶层 `instructions`、`input` 项数组（message / function_call / function_call_output）、`store:false` 恒下发（无状态、每次重放完整历史，不实现 `previous_response_id`）、`max_output_tokens`（接线层缺省 4096）与推理模型抑制 `temperature`（`codex` / `gpt-5` / `o1`–`o5` 同源判定）、tools 扁平结构与 `tool_choice` 归一；流式为事件驱动 SSE（`response.output_text.delta` / `response.reasoning_summary_text.delta` / `response.function_call_arguments.delta`，`response.completed` / `incomplete` / `done` 先补齐快照再定 `finishReason`），`response.failed` 与 `error` 事件映射 `*ProtocolError`（带内、一律不重试）。`registry.go` 登记 `openai_responses{"", }`；`service/llm_protocol_slot.go` 槽位 `implemented=true`、变体白名单 `[""]`（**未实现协议 422 仅剩 `google_gemini`**，待 PA-4）。测试：新增 `internal/llm/protocol/openai_responses_test.go`（请求构建 / 工具与 `tool_choice` / 推理模型抑制采样 / 非流式成功与带内失败 / 流式事件与快照恢复 / 失败与坏帧 / 边缘项），同步 `internal/llm/protocol/openai_chat_test.go`、`service/llm_protocol_slot_test.go`、`service/llm_registry_test.go`、`handlers/ai/llm_provider_admin_test.go` 受影响断言（422 哨兵改用 `google_gemini`）。零破坏：无 DB / API 契约改动、`LLM_PROTOCOL_ADAPTER_ENABLED` 默认关。差异/决策 9 条登记于独立计划 `docs/plan/llm-protocol-adapter-plan.md` §4.3（Q1 载体决策见其 §8），门禁注记见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑯ 与 §10 v1.20
- **LLM 协议适配层 PA-4：`google_gemini` 适配器 + 4/4 协议适配器化收官（独立计划升 v1.3）** — `itsm-backend/internal/llm/protocol/google_gemini.go` 新增：以 **Gemini API v1beta** 为载体（`POST /v1beta/models/{model}:generateContent`；流式 `:streamGenerateContent?alt=sse`），鉴权固定 `x-goog-api-key`（**不复刻**参考实现的 `?key=` 查询参数形态，密钥不进 URL / 代理日志）；前导 system 提升为顶层 `systemInstruction`、`contents[]` 仅 `user` / `model`、工具声明 `functionDeclarations`、工具结果 `functionResponse`（函数名按会话历史反查、无 id → `call_N`、`args` 兼容对象/字符串、空结果回退 `{"result":""}`）、`generationConfig.maxOutputTokens`（>0 才下发，接线层缺省 4096）与 `temperature`（缺省 0.3；`IsReasoningModel` 恒 false）、`thought=true` parts → 推理链、`finishReason` 原生值透传、帧内 `error` / `promptFeedback.blockReason` 映射 `*ProtocolError`（带内、一律不重试）。协议包新增可选接口 `ModelPathAdapter`（`APIPathFor(model, stream)`），`service` 层请求期优先取动态资源路径、回退静态 `GetAPIPath()`；`registry.go` 登记 `google_gemini{"", }` + `DefaultEndpoint`（`https://generativelanguage.googleapis.com`）；`service/llm_protocol_slot.go` 槽位 `implemented=true`、变体白名单 `[""]`——**4 值协议全部实现**，`AI_PROTOCOL_NOT_IMPLEMENTED`(422) 仅剩协议枚举外 / 变体白名单外组合（能力位真实值收敛留待 PA-5）。测试：新增 `internal/llm/protocol/google_gemini_test.go`（元数据 / 请求构建 / 边缘项 / 工具配置 / 非流式 / 流式 / 带内错误 7 用例），同步 `service/llm_registry_test.go`、`service/llm_protocol_slot_test.go`、`internal/llm/protocol/openai_chat_test.go`、`internal/llm/protocol/anthropic_messages_test.go`、`handlers/ai/llm_provider_admin_test.go` 受影响断言。零破坏：无 DB / API 契约改动、`LLM_PROTOCOL_ADAPTER_ENABLED` 默认关。差异/决策 8 条登记于独立计划 `docs/plan/llm-protocol-adapter-plan.md` §4.3，门禁注记见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑰ 与 §10 v1.21
- **LLM 协议适配层 PA-5：能力位开关感知收敛（独立计划升 v1.4）** — `itsm-backend/service/llm_protocol_slot.go` 的槽位能力位改为按「承载实现 + 部署开关」计算：`LLM_PROTOCOL_ADAPTER_ENABLED` 开启且 `(协议, 变体)` 由协议包注册表承载时返回适配器事实值（PA-1..PA-4 四协议适配器均实现流式 / 工具 / 推理链，`supportsStream` / `supportsTools` / `supportsReasoning` 三项全真）；关闭（默认）时保持旧分支事实值（`azure` / `ollama` / `anthropic_messages` 两变体仅「已实现、非流式」），管理 API 与前端不提前放量（独立计划 R-3）；`Implemented` 为枚举级事实、不随开关变化（422 仍仅枚举外协议 / 变体白名单外）。实现：槽位字段改名 `legacyCapabilities`、新增包级 `llmAdapterCapabilities` 与 `effectiveCapabilities(协议)`，`LLMProtocolCapabilities` / `LLMProtocolOptions` / `defaultCapabilities` 读取期解析开关与注册表；`docs/api-reference.md` 更新能力位取值说明（字段 / 形状不变、非破坏）；前端零改动（协议 / 变体选项由 `implemented` + `variants` 白名单数据驱动，三项能力位无消费方）。测试：`service/llm_protocol_slot_test.go` 开关两侧取值矩阵（关闭侧逐值锁定旧事实值；开启侧 7 组合三项全真；校验语义不随开关变化），`handlers/ai/llm_provider_admin_test.go` 新增 `available` 与 `protocolOptions` 同口径端到端断言。证据：`go test ./service/ -run "ProtocolSlot|ProtocolOptions" -count=1`、`go test ./handlers/ai/ -run Provider -count=1` 全绿、改动文件 `gofmt -l` 无输出。独立计划 `docs/plan/llm-protocol-adapter-plan.md` 升 v1.4（PA-5 决策 4 条 + §4.5 落地口径），门禁注记见 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑱ 与 §10 v1.22
- **LLM 协议适配层 PA-6：4 份协议 parser 规格文档 + 文档同步（独立计划升 v1.5，全序列收口）** — 新增 `docs/plan/protocol-specs/`：`openai_chat_completions.md`（三变体差异落点：azure endpoint 归一 + `model` 回退 `deployment`、ollama OpenAI 兼容归一；`temperature` 恒下发与推理模型抑制口径）、`openai_responses.md`（顶层 `instructions`、`input` 项数组、`store:false`、事件驱动 SSE 全表与结束快照补齐、`response.failed` / `error` 带内错误）、`anthropic_messages.md`（官方 vs minimax 变体字段名与端点、`tool_use` / `tool_result` 配对、`input_json_delta` 累积、`stop_reason` 优先口径）、`google_gemini.md`（`ModelPathAdapter` 动态资源路径、`x-goog-api-key` 鉴权、`functionResponse` 函数名反查与缺失占位、`finishReason` 原生透传、`promptFeedback.blockReason` 带内错误）；每份含元数据 / 请求构建 / 非流式解析 / 流式事件序列 / 错误语义 / 边缘项与容错 / §4.3 决策索引 / 测试用例索引 8 节，字段名 / 帧名 / 函数名 / 用例名逐条核对 `itsm-backend/internal/llm/protocol/*.go` 源码；文末「存疑 / 待澄清」共 19 条（chat 3 / responses 5 / anthropic 5 / gemini 6；如 responses 流式 `[DONE]` 解析不对称、anthropic 字符串 `tool_choice` 直传、gemini `variant` 字段存而未用等）登记为后续修缮输入，不涉代码改动。文档同步：独立计划 `docs/plan/llm-protocol-adapter-plan.md` 升 v1.5（头部版本块 / §0 / §3 规格坐标 / §5 PA-6 行 ✅ / §6 证据行 / §9 修订记录），主计划 `docs/plan/multi-llm-provider-plan.md` §5 落地状态 ⑲ / §10 v1.23 / §11.3 同步。证据：`bash scripts/docs-gate/run-all.sh` → 5 gates / 0 failed（日志 `.dev/docs-gate-rerun.log`）；PA-1..PA-6 全序列收口
- **工单详情页图片查看器** — `/tickets/:id` 描述内的富文本图片可点击（或回车 / 空格）放大查看：放大 / 缩小（按钮、滚轮、`+` / `-`）、原始尺寸 1:1、适应窗口（默认，且不把小图放大）、左 / 右旋转 90°、水平 / 垂直翻转、放大后拖动平移、重置、多图左右切换、下载原图，Esc / 点击遮罩退出并把焦点还给原图片（`src/components/business/RichTextImageViewer.tsx` + `src/lib/rich-text/image-viewer.ts`，设计见 `docs/architecture/ticket-create-page-rich-input-optimization.md` §4.9）
- **通用附件 API（A1-A6）** — 新增与宿主解耦的附件端点：`POST/GET /api/v1/attachments`（上传 / 列表）、`POST /api/v1/attachments/batch-query`（批量元数据回填）、`GET /api/v1/attachments/:id`、`GET /api/v1/attachments/:id/content`（下载 / 预览，支持 Range）、`DELETE /api/v1/attachments/:id`（软删，被宿主正文 / 评论引用时返回 409/6105）。权限码 `attachment:read/write/delete`（系统管理员 / 租户管理员默认绑定），宿主维度（工单 / 知识库等）按 `§4.2` 权威表二次校验，跨租户与不存在的宿主一律 404/6101；新增 6101-6107 错误码段（宿主不存在 / 文件名非法 / 超限 / 类型不允许 / 被引用 / 配额 / 频率），全部在 `Fail`/`FailWithData` 双 switch 登记（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-4）
- **附件域内别名路由（知识库 / 服务请求）** — 知识库文章与服务请求接入通用附件 A1/A2/A4/A5：`GET|POST /api/v1/knowledge/articles/:id/attachments`、`GET|DELETE /api/v1/knowledge/articles/:id/attachments/:ref`（含 `/download`、`/preview`），以及 `/api/v1/service-requests/:id/attachments` 同形路径；静态权限复用宿主码（`knowledge:read/write/delete`、`service_request:read/write/delete`，零新增权限码）。附件必须归属路径宿主：跨宿主读取 / 删除按 404 处理，非法宿主 ID 与非法附件引用同样 404（不泄露资源存在性，且先于文件流输出检查）；服务请求宿主以 `reason` 正文参与删除引用保护（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-5）
- **旧工单附件端点接入通用后端（灰度切换）** — `/api/v1/tickets/:id/attachments` 系列端点（列表 / 上传 / 下载 / 预览 / 删除）在 `attachment.generic_read_enabled` / `attachment.generic_write_enabled` 打开后改走通用附件服务（部署级 `config.AttachmentConfig` + 租户级 `system_config` 覆盖，默认 `false`，关闭即回退旧表且 URL 不变）。响应 JSON 字段、状态码与错误文案与改造前**逐字段一致**（含 `uploader` 嵌套字段、`fileUrl` 的 `.../attachments/{id}/preview` 形态与历史「存储文件名」引用）；灰度期未回填的历史记录按未命中回退旧表，历史富文本 URL 不失效。契约测试 `handlers/ticket_attachment/handler_contract_test.go`（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-6）
- **内嵌图片引用完整性（写入期归属校验）** — 正文富文本里的 `<img data-attachment-id>` 在写入工单描述（新建 / 更新）与知识库文章时逐条校验宿主归属：A4 规范地址（`/api/v1/attachments/{id}/...`）必须命中「同租户 + 同宿主 + 存活」且与 `data-attachment-id` 一致；旧工单域内地址在工单 ID 不匹配时剥离；其它地址只在能解析到通用记录且宿主不符 / 已软删时剥离（`查不到` 不误伤，保障灰度期旧表引用）。违规引用整标签剥离并以 `Warnw` 逐条告警（`attachment_id` / `reason` / `src`），校验查询失败不阻塞写入（`service/attachment_refs.go`，实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-7）
- **附件生命周期清理与宿主删除级联（BE-8）** — 附件删除保持「仅软删」，新增后台清理任务按租户回收「软删且超过保留期」的附件：先删物理文件、后删元数据行，单条失败下轮重试；仍被宿主正文 / 评论引用的记录一律跳过（不误删）。配置 `attachment.cleanup_enabled`（默认 false，关闭即任务不注册）、`cleanup_purge_enabled`（默认 false = 演练 dry-run，只统计不落删）、`retention_days`（30 天）、`cleanup_interval_minutes`（360）、`cleanup_batch_size`（200，硬上限 1000），仅部署级生效。工单删除与知识库文章删除后按策略级联软删其附件（`SetAttachmentLifecycle` 注入，未注入时零行为变化）；被引用项保持 active，引用解除并过保留期后才进入回收序列（`service/attachment_cleanup.go`，实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-8，演练记录见 `docs/testing/attachment-cleanup-drill-2026-09-22.md`）
- **评论附件后端（先上传后绑定，BE-9）** — 工单评论支持携带 `attachments: number[]`：附件先经通用上传 A1 以 `biz_type='ticket'` + `usage='comment_attachment'` 落库（宿主权限沿用 `ticket:create`），评论创建 / 更新时按 ID 绑定。服务端只接受「同租户 + 同宿主（`biz_id` = 工单 ID）+ `usage='comment_attachment'` + `status='active'`」的通用附件，非法项整体拒绝且不产生部分写入；单评论上限 10 个、自动去重保序。更新路径补齐附件增删：`nil` = 不修改、`[]` = 清空引用、非空 = 全量替换。被评论引用的附件删除返回 409/6105，评论删除或更新移除引用后附件转无主，由 BE-8 保留期任务回收（`service/ticket_comment_service.go`、`dto/ticket_comment_dto.go`，实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-9）
- **前端附件公共契约与单一上传入口（FE-1）** — 新增 `src/lib/upload/types.ts`（`AttachmentUsage` / `AttachmentHostContext` / `AttachmentRef` / `AttachmentUploader` / `AttachmentDeleter` 与 10MB 上限常量，全站唯一契约来源，字段与后端通用附件响应同名同义）与 `src/lib/upload/attachment-uploader.ts`（`AttachmentUploader` 默认实现工厂 + `isAttachmentUsage` / `isAttachmentHostContext` / `isAttachmentRef` 类型守卫；零业务 API 直连，HTTP 与端点选择由 FE-4 的 `lib/api/attachment-api.ts` 注入）。后续工单 / 知识库 / 评论附件统一注入该实现，不再各自直连上传（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 FE-1）
- **附件 API 客户端去重与域内端点解析（FE-4）** — 新增 `itsm-frontend/src/lib/api/attachment-api.ts`，成为全站唯一附件 HTTP 实现（A1 上传 / A2 列表 / A3 明细 / A4 内容地址 / A5 软删 / A6 批量回填），并集中解析端点：工单默认用途仍走 `/api/v1/tickets/:id/attachments`（URL 形态与 `ticket:*` 权限不变，D5；普通用户 token 无 `attachment:*`，改走通用路由会被 403 拦下）、知识库 / 服务请求走 BE-5 域内别名路由（显式 `usage` 透传）、其余宿主或工单域需表达 `usage != attachment`（评论附件 / 内嵌图片）时走通用路由。旧 `TicketAttachment` 响应在客户端补齐 `bizType/bizId/usage` 成为统一契约，通用响应经契约守卫校验，非法结构直接抛错而不静默透传；`ticket-attachment-api.ts` 与 `ticket-api.ts` 的附件方法改为委托薄封装（不再各自自建 FormData / 直连 httpClient），旧方法签名、URL 与返回字段保持不变（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 FE-4）
- **附件错误文案 i18n 与错误码映射（FE-7）** — 新增 `itsm-frontend/src/lib/upload/error-messages.ts`：把 §3.4 错误码（2003 无宿主持有权限 / 6101 宿主不存在 / 6102 文件名非法 / 6103 超限 / 6104 类型不允许 / 6105 被引用 / 6106 配额 / 6107 频率）映射到 `attachment.error.*` 文案 key，并提供 `extractAttachmentErrorCode` / `formatAttachmentError`（登记码 → i18n 文案 → 后端 message → 兜底文案四级回落，超限透出服务端 `{maxSizeMB}`）。`http-client` 保持通用解包、不做附件特判，仅在抛出的 Error 上附带 `code` / `httpStatus` / `requestId`，同时修掉非 2xx 响应体被空 `catch` 吞掉、后端 message 无法透出的旧缺陷（附件 61xx 走 `Fail()` 真实 4xx，此前前端一律只看到 `HTTP error! status: 4xx`）。`AttachmentField` 状态标签、校验原因、上传 / 删除失败与重试提示全部改由 `attachment.field.*` 出文案，中英文案进 `lib/i18n/translations.ts`，组件源码不再硬编码中文提示（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 FE-7）
- **AI 助手会话页 UI 现代化（Markdown 渲染 + ChatGPT / DeepSeek 风格布局）** — 新增 `itsm-frontend/src/components/ai/MarkdownMessage.tsx`（`react-markdown` + `remark-gfm` + `rehype-sanitize`：GFM 表格与任务列表、代码块语言标签 + 复制按钮 + 横向滚动、链接 `target="_blank" rel="noopener noreferrer"`、流式输出光标；样式作用域 `.ai-md`、取色 `theme.useToken()`；测试 `src/components/ai/__tests__/MarkdownMessage.test.tsx`）；`AIChat.tsx` 重写为 ChatGPT / DeepSeek 风格布局——左侧 268px 会话历史栏（窄屏可收起）+ 主区工具条 + 居中消息列（`maxWidth 820`）+ 底部输入坞（`Enter` 发送 / `Shift+Enter` 换行 / `isComposing` 挡中文 IME），用户消息右对齐气泡、助手消息头像 + Markdown 渲染、空态建议卡片。证据：`npm run type-check` exit 0、`npx jest`（`ai-api` / `use-llm-provider-feature` / `MarkdownMessage`）3 套件 53 用例全绿、浏览器实测流式回答 Markdown 端到端与消息列布局量测（无横向溢出）

- **AI 助手「保存成文章」（整段会话 → 知识文章草稿）+ 单条回答保存完整正文** — 会话页标题栏新增「保存成文章」：跳转 `/knowledge/articles/new` 时路由 state 只带会话 ID（`buildConversationArticlePrefillState`），正文由新建页经 `GET /api/v1/ai/conversations/:id` 拉取后组装（`lib/knowledge/conversation-article.ts`：按消息 id 升序成对组织问答、保留完整回答不截断、助手消息 `{answer,sources}` JSON 与纯文本历史数据均可解析、引用来源按 `objectType:id` 去重汇总为文末「参考来源」章节）——整段会话动辄上万字，不塞 `history.state`，避免拖慢导航与逼近浏览器体积上限；拉取期间显示加载态并禁止提交（不落库空文章），会话无可用回答 / 拉取失败给出显式错误态与重试指引。单条回答「补充为知识文章」与整段会话收敛到同一份预填契约：`readArticlePrefillRequest` 区分 `answer`（正文内联）/ `conversation`（按 ID 拉取）两形态，旧 `readArticlePrefillState` 保持向后兼容；会话态副作用依赖原始会话 ID（避免 `location.state` 对象身份变化导致重复拉取与被取消的落地）。正文上限由后端 `CreateKnowledgeArticleRequest.Content` 的 `max=10000` 放宽到 `max=200000`（前端 `ARTICLE_CONTENT_MAX_LENGTH` 对齐，超限只告警、不静默截断），修复「长回答 / 长会话只能保存一小段」。另移除知识库文章详情页顶部与卡片内重复的页面级操作组（发布 / 取消发布 / 归档，含用户反馈的重复「发布文章」按钮），页面收敛为只渲染 `<ArticleDetail />`。证据：`npx tsc --noEmit` exit 0、`npx eslint`（8 个改动文件）exit 0、`npx jest`（`conversation-article` / `ai-article-prefill` / `knowledge-new-article-prefill`）3 套件 35 用例全绿；浏览器实测（localhost:3000）：`/knowledge/articles/3` 顶部重复按钮消失，`/ai/chat` 标题栏按钮出现 → 跳转后按会话 6 经接口拉取（正文 329 字符完整 Markdown，提问 + 回答 + 无截断，提示「正文来自 AI 助手会话」）、加载态与提交禁用生效
- **知识库文章版本历史（列表 / 对比 / 恢复三端点 + 详情页版本页签闭环）** — 补齐前端版本页签一直在调用、但后端从未注册的版本历史端点（此前页签固定 404）：`GET /api/v1/knowledge/articles/:id/versions`（`knowledge:read`；`data` 直接为版本数组、不做分页包络，字段对齐前端 `ArticleVersion` 契约——`version` / `title` / `content` / `contentType` / `category` / `tags` / `createdBy` / `createdByName` / `changeLog` / `createdAt`）、`GET .../versions/compare?from=&to=`（`knowledge:read`；`from == to` 参数错误；响应 `fromVersion` / `toVersion` / `diff` 逐行文本 + `changes[]`（`added` / `removed` + 行内容））、`POST .../versions/:version/restore`（`knowledge:write`；标题 / 正文回写为指定版本并追加「恢复到 v{n}」记录）。快照写入复用既有 `knowledge_article_versions` 表（ent schema 已存在、零迁移）：创建落 v1「初始版本」、每次更新成功追加「文章更新」、恢复同样只追加新版本（历史不可变，列表倒序第一条 = 当前正文）；读写全程租户过滤，跨租户按 404 处理不泄露版本历史。RBAC 预检生成物同步（`go run ./cmd/authz-gen`：两行 `knowledge:read` + 一行 `knowledge:write`）。证据：`go test ./handlers/knowledge/ -run TestArticleVersionHistory_Lifecycle -count=1` 全绿（创建 → 更新 → 列表倒序 → 恢复 → 对比 + 跨租户隔离）；浏览器实测页签正常渲染版本列表（当前版本标记、对比弹窗产出 `--- v2 / +++ v1` 差异文本）

### Security

- **登录/刷新响应令牌收敛** — access token 和 refresh token 不再通过 JSON 响应返回，改为仅通过 HttpOnly cookie 下发，防止 XSS 窃取
- **通用附件读路径宿主归属校验** — 旧工单端点灰度走通用附件服务时，下载 / 预览此前只按「附件 ID + 租户」取流；灰度期 `attachments` 与 `ticket_attachments` 主键序列独立、同号是常态，持有工单 A 路径的用户可读到工单 B（乃至其它 `biz_type`）的同号附件内容。现新增宿主定向读取 `AttachmentService.GetFileForHost`（`biz_type + biz_id + tenant_id` 三元一致，归属不符 / 已软删一律按未命中处理并回退旧表），并补回归用例（已做负向验证：去掉宿主过滤该用例立即红灯）
- **内嵌图片跨宿主引用剥离** — 富文本正文里的 `data-attachment-id` 此前只做属性白名单，不校验引用是否属于当前宿主，可把别处（其它工单 / 其它域）的附件写进自己的正文并留下跨宿主引用；现于工单与知识库写入路径做归属校验并剥离，详见上方 Added 条目

### Fixed

- **修复知识库文章详情页「版本历史」加载失败与错误重复弹出** — 页签调用的 `GET /api/v1/knowledge/articles/:id/versions` 长期 404（后端未实现，见上方 Added）；前端同时有三处放大故障的缺陷，一并收敛——加载请求加序号守卫（`loadSeqRef`：React StrictMode 双挂载 / 切换文章的过期响应与过期错误一律丢弃，不再弹两条「加载版本历史失败」）、toast 改固定 key（`kb-article-versions-load` / `-restore` / `-compare`，重复错误原地替换）、恢复确认由 `Popconfirm` 改为 `Modal.confirm` 且把 `catch` 放进 `onOk`（失败可见、不再产生未捕获 rejection）。顺带修正「当前版本」统计口径：组件不再信任父组件硬编码的 `currentVersion={1}`（`ArticleDetail` 停止传死值，仅列表为空时用兜底值），改由版本列表最大版本号推导（当前标记 / 可恢复版本数 / 预览弹窗恢复按钮同一口径）；antd 6 的 `Alert.message` 改用 `Alert.title`，消除控制台废弃告警。证据：`npm run type-check` exit 0、`npx eslint`（2 个改动文件）exit 0；浏览器实测版本页签渲染 v2（当前）/ v1，注入接口失败后 2 次并发请求仅保留 1 条 toast，控制台无新增告警
- **修复知识库 Markdown 正文「样式塌陷」（`/knowledge/articles/3` 标题与段落粘连、GFM 表格无框、列表丢符号）** — 上一轮把 Markdown 接进 `react-markdown` 后，容器 class 仍是 `prose max-w-none`，但前端只装 `tailwindcss@4`、未装 `@tailwindcss/typography`，`prose` 在项目里没有任何规则；再叠加 Tailwind preflight 对 `margin` / `list-style` / 链接颜色的清零，正文呈「标题贴着上一段、表格无边框无内边距且按内容宽度收缩、`ul/ol` 没有项目符号、链接与正文同色」。现 Markdown 分支直接复用 AI 回答的 `MarkdownMessage`（作用域 `<style>` `.ai-md`，取色走 antd token，14px / 1.7 行高与富文本分支 `.ticket-rich-text` 对齐）：标题层级与间距、列表符号、表格 1px 边框 + `6px 10px` 内边距 + 横向滚动包装、行内代码胶囊、代码块语言标签与复制按钮、外链 `target="_blank"` 全部生效（`ArticleContentRenderer.tsx`，`markdownClassName` 默认值由 `'prose max-w-none'` 改为无附加 class）。同时补上 `MarkdownMessage` 缺失的 `ul { list-style: disc }` / `ol { list-style: decimal }`（preflight 会清零，`globals.css` 的 `.ticket-rich-text` 早已显式补回；AI 回答的列表一并受益）；知识库评审页「内容预览」由 `SafeContent + prose`（Markdown 被当纯文本暴露）切换到同一 `ArticleContentRenderer`，与详情页 / 版本预览同口径。证据：`npx tsc --noEmit` exit 0、`npx eslint`（3 个改动文件）exit 0、`npx jest --coverage=false`（`article-content-renderer` / `MarkdownMessage`）2 套件 12 用例全绿（新增锁定：markdown 分支必须落在 `.ai-md` 作用域、`.prose` 不得出现、表格进 `.ai-md-tablewrap`；作用域 CSS 必须含 `list-style: disc / decimal`）。浏览器实测 `/knowledge/articles/3`：`.ai-md` 作用域存在、`.prose` 计数 0，`h2` 18px/600 + `margin 12/8`，表格 1149px 全宽 + `th/td` `padding 6px 10px` + 1px 边框 + 表头浅底 + 滚动包装，行内代码带底 / 圆角 / 等宽，`ul` 恢复 `disc`（`padding-left 22px`），页面无横向溢出（scrollWidth 1471 ≤ 视口 1486）

- **修复知识库 Markdown 文章不渲染（`/knowledge/articles/3` 把 `## 标题` / `| 表格 |` 原样当段落暴露）** — 正文单字段混存 Markdown / 富文本 HTML / 纯文本且无类型标记，详情页只有「HTML 或不是 HTML」两态启发式（`isHtmlContent`），Markdown 没有渲染支路，存量 Markdown 文章（id=3，1234 字符，`##` 标题 + GFM 表格 + `**加粗**` + 行内代码）被整段按纯文本输出。现引入显式「正文类型」`text` / `markdown` / `html` / `rich_text`：后端新增 `knowledge_articles.content_type` 列（幂等迁移 `migrations/20260926_knowledge_article_content_type_expand.sql`，已落库）与 `common/knowledgecontent`（`Normalize` / `Parse` / `Detect` / `Resolve`，兼容 `md` / `richtext` 等写法），创建 / 更新请求接受 `contentType`（非法值 400，留空表示自动判定并在落库前归一），响应统一返回**解析后的生效类型**——历史空值按内容形态兜底（命中块级 / 结构标签 → `rich_text`，其余 → `markdown`，纯空白 → `text`，与既有启发式口径一致，存量渲染零回归）；前端渲染收敛到 `components/knowledge/ArticleContentRenderer.tsx`：`markdown` 走 react-markdown + remark-gfm + rehype-sanitize（标题 / 表格 / 任务列表 / 代码块），`text` 保留换行不解析任何标记，`html` / `rich_text` 走 `sanitizeRichTextHtml` + 图片查看器；详情页与版本预览共用同一渲染器并按 `resolveArticleContentType` 分派，页面以 Tag 展示生效类型。编辑器形态同步收紧（`lib/knowledge/article-content-type.ts` 的 `editorKindForContentType`）：仅 `rich_text` 挂 TipTap 富文本，`markdown` / `text` / `html` 一律纯文本输入域（整篇 Markdown 进富文本会丢语义），新建页与编辑页新增「正文类型」选择器，跨形态切换不转换正文并提示。证据：后端 `go test ./common/knowledgecontent/... ./handlers/knowledge/... ./dto/...` 全绿、`go build ./...` exit 0；前端新增 `src/lib/knowledge/__tests__/article-content-type.test.ts`（12 用例）与 `src/components/knowledge/__tests__/article-content-renderer.test.tsx`（6 用例：`<h2>` / GFM 表格 / 字面量 `##` 不泄漏 / 脚本被净化 / `data-attachment-id` 保留）全绿，`npx tsc --noEmit` exit 0。浏览器实测（localhost:3000，后端换新二进制重启后）：`/knowledge/articles/3` 渲染出 3 个 `<h2>`、2 张带 `<th>` 的表格、加粗与行内代码，正文无 `##` / `| 状态 |` 字面量，类型 Tag 为「Markdown」，接口 `contentType` 为 `markdown`；历史 HTML 文章（id=1/2）仍走富文本分支（接口 `rich_text`、`<p>` 正常渲染）；`/knowledge/articles/3/edit` 类型选中 Markdown 且正文原样落在 textarea（无 TipTap）；新建页默认富文本，切到 Markdown 出现占位 `# 问题描述` 的 textarea 并提示「已切换到 Markdown…」，切回富文本 ProseMirror 正常挂载；控制台无新增错误

- **修复 AI 助手「补充为知识文章」不携带回答内容（跳转后仍是一张空白表单）** — 该入口此前只做 `navigate('/knowledge/articles/create')` 的裸跳转：消息本体在 `ChatMessageItem` 的 `onCreateArticle: () => void` 回调处即被丢弃，目标又是「只重定向、不保留 `location.state`」的历史别名（`routes/legacy-redirects.tsx`），新建页亦无任何承接逻辑——四段链路没有任何一段在传递数据（非回归，功能从未实现）。现新增共享契约 `lib/knowledge/ai-article-prefill.ts`：`buildArticlePrefillState` 投递、`readArticlePrefillState` 校验取回（非对象 / 来源不符 / 正文空白一律视为无预填且不抛错，可安全消费 `history.state` 的结构化克隆），标题按回答首个非空行推导（跳过代码围栏，剥图片 / 链接 / ATX / 引用 / 列表 / 表格与行内标记，超 80 字截断，永不空）。会话页改跳 `/knowledge/articles/new` 并以 `{ state }` 携带回答 **Markdown 原文**；新建页用 `useLocation` 取回后仅写入一次表单（`ref` 防重，用户后续改动不被覆盖），有预填时正文固定走 Markdown 模式——整篇 Markdown 塞进 TipTap 会被解析成纯段落、标题 / 表格语义尽失，与编辑页「HTML / Markdown 双读」同策略；无预填时仍为空白新建。测试：`npx jest src/lib/knowledge/__tests__/ai-article-prefill.test.ts src/pages/__tests__/knowledge-new-article-prefill.test.tsx --coverage=false` 2 套件 15 用例全绿（真实渲染断言预填写入表单 / 富文本分支不渲染 / 非法 state 不注入，外加写侧源码契约：必须带 state 且不得回退历史别名）；`npx tsc --noEmit` exit 0、`npx eslint`（5 个改动文件）exit 0。浏览器实测（localhost:3000 真实回答链路）：`/ai/chat` 收到真实回答后点击「补充为知识文章」→ URL 为 `/knowledge/articles/new`，标题与 261 字符 Markdown 正文已写入表单、富文本编辑器未挂载（正文落在 textarea）、提示语可见、`history.state.usr.source = 'ai-chat'`，控制台除浏览器扩展噪声与通知 WS 断开告警外无新错误

- **修复 AI 助手会话页整页布局未铺满视口（卡片矮一截 / 输入坞不贴底 / 外层文档滚动条 / 底部 64px 空白带）** — 聊天卡片此前写死 `height: calc(100vh - 150px)` 且 `minHeight: 480`，实际只有 480–627px 高，把页脚挤出视口并产生文档级滚动条；`AIChat.tsx` 新增 `lockChatPageLayout()`：挂载时（`useLayoutEffect`，提交前同步执行）把 `html` / `body` / `#root` / `.ant-layout-has-sider` / `#main-content` / `.main-content` / `.page-transition` 逐层用内联样式改造成 100vh 纵向 flex 链（`transition: none` 抵消 `ant-layout-content` 的 200ms 过渡，保证首帧即最终高度），卡片改为 `flex: 1 1 auto` 吃掉 Header 之外的全部高度；并在 `<html>` 上打 `data-ai-chat` 标记启用两条作用域规则（`.main-content` 底部内边距归零 + 隐藏全局页脚）清除卡片下方「16px 内边距 + 48px 页脚」的空白带，其中底部留白用 CSS 规则而非内联样式，因为 `MainLayout` 会在断点切换时重写 `.main-content` 的 `padding` 简写；卸载时逐项还原（不影响其它页面），`PageTransition.tsx` 补稳定类名 `page-transition` 供祖先链选择。证据：1486×763 / 1280×600 / 390×740 三档浏览器实测卡片底边与视口底边重合（0 留白）、`documentElement.scrollHeight === clientHeight` 且 `window.scrollTo` 无效（无文档滚动）、消息区高度 482 → 546px，rAF 逐帧首帧采样 0 个错误帧（无布局跳变），客户端路由离开后 `data-ai-chat` / 内联样式 / 页脚全部还原且 `/dashboard` 恢复自身滚动；`npx tsc --noEmit` exit 0、`npx eslint`（2 个改动文件）exit 0
- **修复多 LLM Provider 协议槽位用例断言顺序与 §3.1.4 映射表口径不一致** — `TestLLMProtocolOptions` 原断言假定 `openai_responses` 位于 `anthropic_messages` 之前，与实现（`service/llm_protocol_slot.go` 的 `llmProtocolSlots` 声明序 = 计划映射表行序）相反；实现未动，按计划口径修正断言与索引并注明顺序来源，同时把该用例并入定向集防再漏跑（QA-2 期间发现；此前不在 QA-3 定向集内故未暴露）
- **修复生产迁移被历史 checksum 漂移卡死（发布阻塞）** — prod 账本 6 条历史迁移 checksum 与仓库文件不一致，使 `GetPendingMigrations` 硬失败（所有 pending 一律不执行）；同时两个 pre-cutoff 菜单迁移（`20260628_add_connector_menu` / `20260830_ticket_types_menu_reparenting`）因 prod 被判"全新安装"而进入待执行队列（实跑必失败，但其目标终态早已满足）。按 runbook R1-R3 收敛 6 条 checksum + 收养 2 个菜单文件（**仅 `schema_migrations` 账本写，业务表零改动**），复核 `-ro` = `applied=50 pending=2`（仅剩本方案两条）、`ledger_drift` mismatch=0；脚本 `.dev/mig_r1r2.py`，备份/回滚/日志在 `.dev/backup/`，记录见 `docs/plan/multi-llm-provider-plan.md` §4.4⑤
- 修复 A5 重复删除退化为 404：`DELETE /api/v1/attachments/:id` 删除前复用 A3 `Get` 做宿主鉴权，而 BE-8 已把 A3 收紧为「软删即未命中」，导致 A5 契约「重复删除幂等（第二次仍 200）」失效；现新增任意状态查询 `LookupForAuthorization` 供删除路径完成宿主鉴权与幂等判定（A3/A4 仍保持软删 404），回归用例 `TestDeleteEndpointInUseReturns409ThenIdempotent`（BE-9 期间发现的 BE-8 回归）
- 修复 A3 元数据端点可读取软删记录：`GET /api/v1/attachments/:id` 此前未过滤 `status='active'`，附件软删后元数据仍可读取（内容侧 `GetFile`/`List`/`BatchGet` 早已过滤，读口径不一致）；现与 A4/A2/A6 对齐，软删记录一律 404（BE-8 收口 BE-6 遗留观察，回归用例 `TestAttachmentGetHidesSoftDeletedMetadata`）
- 修复知识库引用复核未排除已删除文章：`KnowledgeArticle` 未纳入全局软删拦截器，文章删除后其正文中的内嵌图片仍被判定为「被引用」，导致这些附件永远无法进入回收序列；现引用复核显式要求文章 `deleted_at IS NULL`（BE-8）
- 修复知识库正文内的附件回链（`data-attachment-id`）与图片对齐（`data-align`）在落库时被静默抹掉：知识库正文走 `common.SanitizeHTML`，bluemonday 的 UGCPolicy 默认剥离全部 `data-*` 属性，导致富文本图片与附件的关联、以及删除时的引用保护双双失效；现显式放行并按取值约束（附件 ID 仅数字、对齐仅 left/center/right），`on*` / `script` 等既有防护不变（`common/sanitizer.go`）
- 修复同一请求被打印两条 `[GIN]` 访问日志（两条耗时通常相差不到 1ms，易被误判为前端重复请求）：HTTP 引擎改用 `gin.New()`，不再与 `router.SetupRoutes` 中注册的 `gin.Logger()` / `gin.Recovery()` 叠加（回归测试 `internal/bootstrap/http_engine_test.go`）
- 修复侧边栏菜单「点一次、请求两次」：`MenuItems.tsx` 同时挂了 label 层与 `items[].onClick`（互为兜底），点击时事件冒泡导致 `router.push` 连续执行两次，浏览器发出两条完全相同的 `_rsc` 请求、服务端重复渲染。`Sidebar.tsx` 的 `handleMenuClick` 增加同路径 400ms 去重，保留两层兜底不变；实测同一次点击 `_rsc` 由 2 条降为 1 条，单条耗时由 3.5~4.8s 降至 0.46s
- 修复点击「待我审批」崩溃成 `Application error: a client-side exception has occurred`：目标 `/approvals/pending` 是 `redirect('/approvals')` 的兼容页，客户端导航进入该重定向路由时抛 React `Rendered more hooks than during the previous render`。侧边栏路径规范化表新增 `/approvals/pending` → `/approvals`（与 `/admin/index` 同款处理），同时省去一次整页 RSC 渲染
- 修复生产环境数据库迁移失败问题：部分迁移脚本内嵌事务控制语句导致整批迁移中止
- 修复生产部署登录失败：docker-compose 默认 RLS 模式从 `enforce` 改为 `off`，避免未携带租户上下文的公共路由（登录/注册）返回 401
- 修复新建用户返回 400：后端补齐创建路径的密码策略校验与错误信息映射，前端表单同步密码策略提示（排查记录见 `docs/archive/bug-reports/user-create-400-password-policy-2026-09-20.md`）
- 修复富文本编辑器插入的图片不显示：附件下载/预览路由（`GET /api/v1/tickets/:id/attachments/:attachment_id[/download|/preview]`）此前未注册导致全部 404；现补注册并同时兼容「数字附件 ID」与旧版 `fileUrl` 的存储文件名，新上传统一返回 `.../attachments/{id}/preview`（内联渲染，规避 `Content-Disposition: attachment`）
- 修复创建工单页插入的图片保存后消失（同一编辑器在编辑页却正常）：两处客户端缺陷导致「暂存图」链路断在提交层——Tiptap 图片节点未声明 `data-attachment-id` 属性（`insertContent` 时被 schema 静默剔除，`getHTML()` 丢标记，提交后既不触发附件上传也不回填正式地址），以及前端 DOMPurify 默认 URI 策略剥掉同源 `blob:` 占位图；现补属性声明并放行同源 `blob:`，创建页插图经「上传附件 → 回填 `/attachments/{id}/preview`」后正常落库与回显（排查与约束见 `docs/architecture/ticket-create-page-rich-input-optimization.md` §4.8）
- 修复全局限流响应实际回落为 HTTP 200：`middleware/security.go` 直接把 `http.StatusTooManyRequests`（429）当业务码传给统一包装，而该值未在错误码 switch 中登记，命中「未登记码静默 200」路径；现登记通用 `TooManyRequestsCode=429`，限流恢复真实 429 响应
- 修复 ACL 清单（`docs/acl-manifest.yaml`）长期少报路由：生成脚本 `scripts/generate-acl-manifest.js` 的路由路径正则要求至少 1 个字符，导致全部 `group.GET("", h)` 形式的分组根路径（如 `POST /api/v1/tickets`、`GET /api/v1/users` 等列表 / 创建端点）被静默丢弃；同时子路由文件（`*gin.RouterGroup` 入参）无法自证挂载前缀，新增的 `attachment_routes.go` 丢失 `/api/v1` 前缀与首两条路由。修复后重新生成：599 → 696 路由、权限覆盖 99.86%，`attachment:*` 与各域根路径端点全部在册（详见 `docs/plan/generic-attachment-richtext-control-plan.md` §4.3-7）

### Changed

- 前端附件 / 富文本控件目录下沉（FE-2）：`components/business/RichTextEditor.tsx`、`RichTextEditorImageMenu.tsx`、`RichTextEditorResizableImage.ts`、`RichTextImageViewer.tsx` 迁至 `components/common/rich-text/`，`components/business/AttachmentField.tsx` 迁至 `components/common/attachment/`（`git mv` 保留 blame，3 个行为用例随实现迁移）。旧路径保留 re-export 兼容层（文件头 `@deprecated`，只转发默认导出与全部具名 / 类型导出，一个发布周期后删除），既有调用点（`TicketDetail.tsx`、创建页、`DynamicFieldRenderer.tsx` 的相对导入、`lib/upload` 用例）零改动；新增 `business/__tests__/legacy-reexports.test.ts` 守卫（新旧模块默认导出 `toBe` 同一实现 + 4 组类型双向可赋值编译期探针）。纯结构调整，无行为变化
- 附件单文件上限前端默认值对齐后端：`AttachmentField`（含 `validateAttachmentFile` 缺省值）与工单创建页的显式传参由 50MB 改为 10MB（引用 `DEFAULT_ATTACHMENT_MAX_SIZE_MB`），消除「前端允许选择 50MB、上传后被后端 413 拒绝」的错配（FE-1，v1.0 限额决策）
- 评论附件引用校验收紧（BE-9）：`ticket_comments.attachments` 的合法取值从「同工单的旧 `ticket_attachments` ID」收紧为「同工单、`usage='comment_attachment'` 的存活通用附件 ID」。前端此前从未上传 / 展示过该字段，属未接线能力的口径对齐；只影响写入校验，不改变既有数据读取，历史引用如需继续更新须改用通用附件 ID
- 生产部署配置：`RLS_MODE` 默认值调整为 `off`（与后端安全默认对齐）。已配置 `.env.prod` 的部署不受影响
- 前端 dev 启动可选预热：新增 `npm run dev:warmup`（`scripts/dev-with-warmup.mjs`，起 `next dev` 后自动预热高频路由，避免首次点击菜单等待冷编译）与 `npm run dev:warm`（`scripts/dev-warmup.mjs`，对已运行的 dev server 手动预热），支持 `--port/--host/--concurrency/--cookie/--dry-run`；`next.config.ts` 顶部补充 dev 性能实测备忘（Turbopack 15.5 无持久化缓存、webpack 冷编译更慢但有磁盘缓存、`optimizePackageImports` 在 Turbopack 下被忽略、`<Link>` 仅 hover 预取、热请求 SSR 开销），未改动任何运行时配置
- **前端框架迁移：Next.js 15 App Router → Vite 7 + React Router 7 静态 SPA（FE-V1-01..52）** — 构建、路由、布局、守卫、静态资源与部署产物一次性切换：`next build` / `next dev` 改为 `vite build` / `vite`（保留 antd、图表、BPMN、富文本等 `manualChunks` 分包），`src/app/**` 页面整体搬入 `src/pages/**`（动态段 `[id]` → `$id`），嵌套布局改由 `src/layouts/{MainLayout,TicketsLayout}.tsx` + `<Outlet />` 承载，`middleware.ts` 收敛为 `src/routes/guards.tsx`、`LEGACY_MENU_REDIRECTS` 转为 `legacy-redirects.tsx`（历史菜单路径行为不变），全量 168 条路由统一 `React.lazy` + `Suspense`，13 条 redirect-only 路由用 `<Navigate replace>`。BFF 与 `src/app/api/**`（含 `/health`、`/uploads` 路由处理）删除：dev 由 `vite.config.ts` 的 `server.proxy` 顶替（`/api` → `ITSM_BACKEND_URL`，cookie domain 重写语义不变、端口 3000 strictPort），生产由 nginx 直连后端（D2 旁路部署核查结论：无直连前端 3000 的旁路，见 `docs/plan/vite-migration-d2-audit.md`）。环境变量 `NEXT_PUBLIC_*` 一次性改名 `VITE_*`（compose 构建参数同步）；`itsm-frontend` 镜像改为 `nginx:alpine` 托管 `dist/`（仍监听 3000，`/health` 保留），`/uploads` 卷改挂 `/usr/share/nginx/uploads`，附件 URL 前缀不变。测试基建 `next/jest` → `babel-jest`（`babel.config.cjs` + `tools/babel-plugin-import-meta-env.cjs`），E2E `webServer` 改 `vite dev` 并新增 168 条全路由可达性冒烟；随 Next 一并删除的还有 `dev:warm` / `dev:warmup` 预热脚本与 `next.config.ts`（dev 已无服务端编译冷启动）。纯工程迁移，无业务行为与视觉改动；计划、路由对照与残留门禁见 `docs/plan/vite-migration-plan.md`

---

## [1.6.10] - 2026-09-20

### Added

- **BPMN 定时事件** — 完整实现 Timer Event 基础设施：调度引擎、BPMN 引擎集成、设计器属性面板、任务到期计时（dueDate）和定时启动事件（Timer Start Event），附带 Prometheus 监控指标和任务超时扫描
- **审批链运行时能力** — 新增加签（allowAddApprover）、委派（allowDelegate）、拒绝策略配置和动态层级适配，均通过审批链配置驱动、运行时生效
- **运维命令批处理** — 支持 Outbox 命令的批量重放与取消
- **钉钉/企微入站回调** — 入站消息回调 API 及幂等去重
- **BPMN 模板重载 API** — `POST /api/v1/bpmn/workflow-templates/:key/reload`，从已发布版本创建新部署，版本号自动递增
- **AI 工作流模板治理** — 租户隔离的模板目录 API 和 `/admin/workflows` 管理 UI，支持草稿、版本递增、发布前 Lint、发布与停用
- **CMDB ontology 自描述端点** — `GET /api/v1/cmdb/ontology` 返回租户 CMDB 的机器可读描述（CI 类型、关系词汇、AI 工具定义），AI Agent 可运行时发现 CMDB 契约
- **CI 可读编号** — 每个新建配置项获得 `CI-YYYYMM-NNNNNN` 唯一编号，支持按编号精确查询
- **一键演示数据集** — `make dev-seed-demo` 生成 8 个事件、2 个问题、3 个变更和 5 篇知识文章的演示数据
- **10 个管理页面使用指南** — 新增 `UsageGuideCard` 组件，为管理页面提供基于实际代码逻辑的操作指引
- **Swagger 文档重建 + CI 新鲜度门禁** — 重新生成 158 个 OpenAPI 路径，CI 自动检测文档漂移

### Changed

- **BREAKING: RBAC 授权平面收敛** — 统一权限码、补全预检映射、实现路由→权限码自动生成，关闭未授权写入漏洞；管理员/技术员角色数据库权限空集修复
- **BREAKING: 审批架构迁移** — 统一消费 BPMN 任务，消除双审批实例，ApprovalRecord 标记废弃，清理冗余 DTO
- **BREAKING: API 响应字段统一 camelCase** — Ticket/Incident/SLA/BPMN 响应不再暴露 snake_case 字段，外部集成需同步迁移
- **BREAKING: SLA/BPMN 查询参数 camelCase** — `ticketType`/`customerTier`/`startDate`/`endDate`/`timeRange` 替代原有 snake_case 参数
- **BREAKING: SLA/BPMN 监控端点标准信封** — 统一返回 `{code, message, data}` 格式
- **BREAKING: CI 关系类型受控词汇表** — 关系类型统一由 ent schema 管理，未知类型返回 400 而非静默持久化
- **BREAKING: Ant Design `direction` → `orientation`** — `Space` 组件统一使用 v6 API
- **服务请求 BPMN 集成** — 服务请求审批走 Outbox 事务投递
- **工单/事件写入路径加固** — 乐观锁、NULL 时间戳处理、错误语义修正、升级操作行级安全校验
- **变更/发布/DataScope 写入路径** — 行级安全校验补全
- **流程设计器修复** — 属性写入丢失、版本解析、菜单 404、模板完整性校验、Lint 审批节点校验
- **CSRF 全链路修复** — Cookie 安全属性与 Token 传递对齐
- **通知偏好** — 接入按事件类型偏好 API，修复虚假 API 处理和接收者广播排除
- **查询参数契约收尾** — 统一 camelCase，关闭残留 snake_case 不一致
- **角色域修复** — 移除虚假控件，API 契约对齐 camelCase
- **生产部署加固** — 数据库迁移与索引对齐、开发/生产隔离、Schema 变更脱敏、迁移脚本检查、`VERSION` 不可变要求、诊断端口绑定 loopback
- **基础设施加固** — AI vector/telemetry schema 版本化迁移、HTTP 响应缓存规范化、租户级 Raw SQL 执行器覆盖、Incident/CI 编号使用结构化审计事务
- **前端 UX 审计** — 工单列表列宽、创建按钮尺寸、折叠侧边栏 flyout、看板卡片溢出、工单分析页空白、管理后台仪表盘虚假指标等 20+ 项修复

### Fixed

- **AI 助手知识库无命中时丢失产品自知上下文** — RAG 降级链路现在注入 AI-Native ITSM 的真实能力边界
- **AI Native 工作流生产闭环** — AI BPMN 生成/预览/模板推荐走认证租户与 workflow 权限
- **AI 生成 BPMN 导入失败** — 自动为缺失 DI 的 BPMN XML 注入布局信息
- **表达式引擎 `avg()` 除零 panic** — 空数组或全零数组返回 0
- **会签投票逻辑修复** — `counterSign` 模式下投票结果计数修正
- **流程设计器属性丢失** — 保存时 `extensionElements` 丢失，6 处属性写入不一致统一修复
- **服务请求审批链双缺陷** — 配置未生效和审批人重复
- **审批人自动分配层级丢失** — `autoAssign=true` 时未携带 `level` 字段
- **流程管理 5 项修复** — 版本 API 路径、semver 解析、菜单 404、列表分页、模板导入 DI 补全
- **工单分配搜索崩溃** — `description` 字段缺失时 crash
- **三个用户可见 bug** — 工单列表分页失效、事件详情页关联 tab crash、变更审批评论丢失
- **杂项修复** — SLA 预测窗口校验、WebSocket 重连指数退避、知识文章链接 404

### Security

- RBAC 未授权写入关闭（incident/change/problem/service-request/release/knowledge），含批量回归测试
- CSRF 全链路修复（Cookie 属性 + Token 传递）
- DataScope / 事件 / 发布 / 工单升级写入路径行级安全校验
- 生产发布门禁加固：高/危依赖和 gosec 发现阻断发布

---

## [1.6.9] - 2026-08-20

### Added

- **Problem Management expansion** — Added Problem trends, hotspots, SLA lookup, and per-problem comment read/write endpoints so operators can analyze recurring issues and discuss root cause without leaving the Problem record.
- **Incident comment deletion** — Added `DELETE /api/v1/incidents/:id/comments/:commentId` for tenant-scoped comment cleanup, mirroring the existing create/list semantics.
- **Knowledge recommendations** — Replaced the previous stub implementations of `/api/v1/knowledge/recommendations` and `/api/v1/knowledge/recent` with tenant-scoped queries that return real published articles with proper permission filtering.

### Fixed

- **Frontend Edge Runtime compatibility** — Replaced `Buffer.from(...)` calls in `src/middleware.ts` and `src/app/api/[...path]/route.ts` with an `atob()`-based JWT decoder so the Next.js middleware no longer raises `Code generation from strings disallowed for this context` in Edge Runtime (was breaking `itsm-frontend-prod` with HTTP 500).

### CI

- **Backend lint toolchain** — Pinned `gofumpt` to `v0.7.0` and cached `~/go/bin` between CI runs to make formatting results reproducible and shave install time.

### Tests

- **CMDB Service layer coverage** — Added 9 table-driven test functions covering CloudService / CloudAccount / CloudResource CRUD, Reconciliation (bound / unbound / orphan / unlinked / mixed), and Discovery operations via a `mockRepository` that isolates the service from the database.

---

## [1.6.8] - 2026-08-04

### Security

- **Go runtime baseline** — Raised the backend build and release toolchain to Go 1.25.12 to include the latest TLS, X.509, and MIME-header security fixes required by the production vulnerability gate.

---

## [1.6.7] - 2026-08-04

### Security

- **Go runtime baseline** — Raised the backend build and release toolchain to Go 1.25.10 as an intermediate production security baseline.

### Fixed

- **SLA calendar enforcement** — SLA deadlines now apply each definition's configured business calendar consistently across creation, lookup, and overdue detection; the prior unused implementation was removed.
- **Backend quality gate** — Removed obsolete change-status validation code so the release static-analysis gate has no dead-code findings.

---

## [1.6.6] - 2026-08-03

### Fixed

- **Cookie-only authentication test contract** — Updated API integration and HTTP client tests to assert credentialed browser requests without exposing HttpOnly session cookies through JavaScript `Authorization` headers.

---

## [1.6.5] - 2026-08-03

### Security

- **Production dependency remediation** — Updated frontend production dependencies and lockfile overrides for known Axios, DOMPurify, lodash, PostCSS, Sharp, and UUID advisories; `npm audit --omit=dev --audit-level=high` now reports zero vulnerabilities.
- **Package-manager integrity** — Removed the obsolete `pnpm-lock.yaml`; the frontend is governed solely by the committed npm lockfile, matching the release CI configuration.

---

## [1.6.4] - 2026-08-03

### Fixed

- **CI formatting gate** — Applied the repository's `gofumpt` formatting standard to backend source and tests, restoring the required backend release pipeline gate.

---

## [1.6.3] - 2026-08-03

### Fixed

- **Production authentication hardening** — Browser sessions now use HttpOnly, SameSite=Lax cookies exclusively. Access and refresh tokens are no longer exposed in JSON responses or read by frontend JavaScript; refresh token rotation is performed through the cookie transport.
- **Secure proxy cookie handling** — Authentication cookies now receive the `Secure` attribute when the request is HTTPS or terminated by a trusted HTTPS reverse proxy.
- **CSRF refresh coverage** — Both supported refresh routes are explicitly covered by the CSRF session-refresh exception.
- **Initialization readiness** — `/api/v1/readyz` now requires the latest registered post-schema migration rather than a stale, hard-coded migration version.
- **Workflow validation contract** — Removed the frontend call to an unimplemented validation endpoint; workflow designer preflight validation is deterministic and local, while create/update remain server-validated.

### Changed

- **Release metadata** — Frontend package, backend version endpoint, system configuration endpoint, and GA-readiness report now identify the release as `1.6.3`.

---

## [1.6.0] - 2026-08-01

### Fixed

- **Critical: Form Boundary Issue** - Fixed TicketTypeFormModal where approval/SLA tab fields were outside `<Form>` component, causing form validation failures.
- **Critical: Auth State Persistence** - Removed `isAuthenticated` from Zustand persist partialize to prevent false login state after browser refresh.
- **High: Timer Leak in Export/Import** - Fixed memory leaks in TicketCategoryExport and TicketCategoryImport with proper useRef cleanup on unmount and error paths.
- **High: ITIL Status Guard** - Added readonly protection on change/edit and release forms to prevent editing approved/completed records.
- **High: Suspense Boundary** - Added Suspense wrapper for useSearchParams in tickets page to fix CSR bailout.
- **High: Query Key Normalization** - Fixed useTicketsQuery to only include request params in queryKey, not response data.
- **High: NaN Route Guard** - Added Number.isFinite check for marketplace/[id] route parameter.
- **High: Error Handling** - Fixed workflow-api to throw errors instead of silently returning empty arrays.
- **Medium: useFormMemory** - Added debounce (500ms), enabled flag for edit mode, and dayjs serialization.
- **Medium: CIEditorForm** - Added min/max/precision props to InputNumber for numeric fields.
- **Medium: ChangeDetail Null Guard** - Added null check for createdAt before dayjs conversion.

### Changed

- **Ant Design destroyOnHidden** - Added to ProblemInvestigationTab modals.
- **SchemaField Type** - Added validation property with minValue/maxValue/precision/pattern.

---

## [1.5.2] - 2026-07-31

### Fixed

- **SLA Compliance Statistics** - Fixed negative compliance numbers caused by mismatched time scopes between total ticket count (30 days) and violated ticket count (all time). Backend now uses `HasTicketWith` edge predicate for consistent scope.
- **Timezone Inconsistency** - Fixed dashboard activity timestamps using `time.RFC3339` format instead of `Format("2006-01-02 15:04:05")` which browsers parse as UTC.
- **TicketDetail Duplicate Request** - Fixed double API call on ticket detail page by removing `fetchTicket`/`fetchSLAInfo` from useEffect dependency arrays.
- **Login Error Message** - Login page now shows actual backend error messages (e.g., "invalid credentials") instead of generic "登录失败".

### Changed

- **Ant Design TabPane Deprecation Migrated** - Converted all `Tabs.TabPane` / `<TabPane>` usage to `items` prop pattern across 8 files: analytics, applications, NotificationCenter, TicketTypeFormModal, IncidentManagement, FieldDesigner, profile, dashboard.
- **Space direction → orientation** - Migrated 6 instances of deprecated `Space direction="vertical"` to `orientation="vertical"`.
- **destroyOnClose → destroyOnHidden** - Migrated 1 instance in ApprovalTimeline component.
- **alert()/confirm() → antd message/modal** - Replaced native browser dialogs in marketplace and installations pages with antd `App.useApp()` message/modal.
- **console.log Cleanup** - Removed debug console.log from TicketDetail and BPMNDesigner.

---

## [1.5.0] - 2026-07-30

### Added

- **Connector/Skill Manifest Hardening** - All official connector manifests now declare `version`, `requiredPermissions`, and a deterministic SHA-256 `checksum`; registration is fail-closed (incomplete manifests are rejected at startup). Skill manifests share the same validation and checksum convention. Market API exposes `isOfficial` / `requiredPermissions` / `checksum`.
- **Post-Schema Migrations 008-010** - Initialization ledger (008), PostgreSQL RLS tenant isolation (009), ticket types in `itil-core` transaction (010).
- **Bootstrap Token** - One-time hashed bootstrap token with TTL, concurrent-consumption protection, replay defense, and break-glass flow for first-admin creation.
- **Endpoint ACL Manifest** - Versioned ACL manifest with 100% static coverage gate over protected routes (route-ACL-permission-menu).
- **Fencing Token Hardening** - Owner/token/lease re-verified inside the committing transaction; stale-writer prevention proven via PostgreSQL fault-injection tests.
- **Audit Routes** - New audit trail API endpoints with tenant isolation support
- **CI Attribute Validator** - Moved from handlers to service layer for better separation of concerns
- **Operator Context** - Enhanced audit trail with operator context tracking

### Fixed

- **Ant Design v6 Select Compatibility** - Replaced deprecated `<Select><Option>` child pattern with `options` prop across 100+ files. Fixes issue where clicking Select dropdowns had no response in antd v6. Affected modules: Ticket, Incident, Problem, Change, CMDB, Workflow, SLA, Service Catalog, Admin, and Reports pages.

### Changed

- **CMDB API Route Convergence** - `/api/v1/cmdb/*` is now the canonical prefix for all CMDB endpoints (CIs, CI types, relationships, relationship types, topology, impact analysis, change history, stats). Frontend API clients (`cmdb-api.ts`, `cmdb-relationship.ts`) have been switched to the canonical prefix. Added `GET /api/v1/cmdb/relationship-types` to the canonical tree. Note: change history is `GET /api/v1/cmdb/cis/:id/history` (the old `change-history` suffix only exists on the deprecated alias).
- **CMDB Multi-Tenant Isolation** - Added `tenant_id` field to CI relationships, configuration item history, and discovery sources. Added tenant-aware backfill migration.
- **CI Attribute Validation** - Migrated from `handlers/cmdb/attribute_validation.go` to `service/ci_attribute_validator.go`.

### Deprecated

- **`/api/v1/configuration-items/*` routes** - Kept as a compatibility alias for clients not yet upgraded. No new endpoints will be added under this prefix; removal will be evaluated after a regression period.

### Migration Notes

- **CMDB tenant backfill**: Run `itsm-backend/migrations/20260610_cmdb_tenant_id_backfill.sql` on existing databases to populate the new `tenant_id` fields.
- **Preset CI types**: Enable with `itsm-backend/migrations/20260611_enable_preset_ci_types.sql`.
- **Audit routes**: New endpoints are protected by existing JWT + RBAC + tenant middleware.

---

## [1.0.0] - 2026-03-07

### Added

#### Core ITIL Modules
- **Ticket Management** - Complete ticket lifecycle management with creation, assignment, tracking, and closure; built-in SLA management, priority handling, comments and attachments
- **Incident Management** - Incident discovery, logging, classification, escalation; real-time monitoring and alerts
- **Problem Management** - Root Cause Analysis (RCA), Known Error Database, problem resolution tracking
- **Change Management** - Change requests, risk assessment, multi-level approval workflows

#### Service & Knowledge Base
- **Service Catalog** - Service request templates, self-service portal, SLA management
- **Knowledge Base** - RAG intelligent search, knowledge categorization, FAQ management, vector retrieval

#### Workflow Engine
- **BPMN Workflow** - Visual process designer, approval workflow automation
- **Task Management** - Workflow task assignment and tracking

#### AI-Powered Features
- **Intelligent Classification** - Auto-identify ticket type, priority, impact scope
- **Auto-Summary** - AI-generated ticket/incident summaries
- **RAG Knowledge Base** - Vector search-based intelligent knowledge recommendation
- **Smart Suggestions** - Recommended solutions, similar tickets

#### User & Permissions
- **Multi-Tenant Architecture** - Complete tenant isolation and management
- **Role-Based Access Control** - RBAC permission system, fine-grained access control
- **User Management** - User CRUD, team and department management

#### SLA Monitoring
- **SLA Definition** - Service Level Agreement configuration
- **Real-Time Monitoring** - SLA compliance rate tracking
- **Alert Rules** - SLA violation alerts and notifications

### Technical Stack

| Category | Technology |
|----------|------------|
| Backend | Go 1.25+ / Gin / Ent ORM |
| Frontend | Next.js 15 / React 19 / TypeScript / Ant Design 6 |
| Database | PostgreSQL 17 / Redis 7 |
| Deployment | Docker / Docker Compose |

### Quick Start

```bash
# Docker Compose (Recommended)
git clone https://github.com/heidsoft/itsm.git
cd itsm
make dev-up

# Access
# Frontend: http://localhost:3000
# Backend: http://localhost:8090
# API Docs: http://localhost:8090/swagger

# Login
# Username: admin
# Password: admin123
```

### Known Limitations

- Mobile PWA features are under development
- Enterprise integration features (LDAP/SSO) planned for future releases

### Documentation

- [Development Guide](./docs/getting-started/install.md)
- [Deployment Guide](./docs/DEPLOYMENT_OPTIMIZATION.md)
- [API Documentation](./docs/api/API_REFERENCE.md)

---

*Thank you to all contributors for your support!*
