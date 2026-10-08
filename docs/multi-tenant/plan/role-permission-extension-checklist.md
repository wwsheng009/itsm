# 新增角色 / 权限 / 路由评审清单（Role & Permission Extension Checklist）

> **定位**：`IP-P2-7` 收口（补丁 1–5）配套的**扩展纪律**——新增角色码、权限码、路由与通道前"先归类、后登记、再验证"的操作手册；与《[平台面权限屏蔽与角色架构设计](./platform-permission-shielding-design.md)》§3.3 评审规则配套使用。
>
> **上位（权威顺序）**：[canon](./msp-concept-model-and-architecture-canon.md) → [目标架构 §6](./msp-target-architecture.md) → [实施方案](./msp-implementation-plan.md)（IP-P2-7 批次）→ [平台面屏蔽设计](./platform-permission-shielding-design.md) → 本清单
>
> **关联**：守卫矩阵见 §5；`CHANGELOG.md`；[建号角色多选方案](./msp-provision-user-roles-multiselect-plan.md)（建号通道过滤）
>
> **状态**：v0.5（2026-10-08）——扩展纪律与平台家族码评估结论（§2.2）落定；R2/R3、角色码归类（补丁 6/7）、目录标注（补丁 8）与用户角色下拉 rank 收口（补丁 9）均已落地（§5）

---

## 0. 总原则

1. **默认收紧 + 显式豁免**：写路由默认必须挂权限中间件；平台面默认必须挂 L1（身份硬校验）；豁免必须写明理由并进守卫注册表——防"悄悄漏挂"。
2. **单一源登记**：角色词表、权限码、路由声明各只有一个权威源；新增 = 改单一源 + 跑守卫。
3. **先归类、后登记**：新增任何码/角色，先回答"它属于哪一类"（平台 / MSP / 租户本地 / 混合），再落代码。
4. **不变量测试化**：安全语义固化为 CI 守卫（§5）；防回退优先于防首犯。
5. **回填**：扩展落地后，回填本清单评估表/状态与设计文档、`CHANGELOG.md`。

## 1. 新增角色（Role Code）

### 1.1 归类决策（先回答）

| 类别 | 判定语义 | 登记位置（唯一源） | 自动生效面 |
|------|----------|--------------------|------------|
| 平台治理角色 | 仅平台根作用域可见/可授 | `middleware.PlatformRoleCodes()`（`middleware/rbac.go`） | 非平台根：隐藏 + 拒授 + 拒写（三面同口径） |
| MSP 专属角色 | 服务商员工身份；客户侧不得出现 | `middleware.MSPOnlyRoleCodes()` + `internal/authz` 客户侧基线 | 客户租户：隐藏 + 拒授；保留码写面拒绝 |
| 租户本地角色 | 其余（租户自管） | 无需登记（默认可见可授） | — |

> 保留码写面（创建/改名）由 `service.isReservedRoleCode` 自动覆盖"平台 + MSP"两词表；无需第三处登记。

### 1.2 检查项

- [ ] **词表登记**：平台/MSP 角色加入对应单一源函数（一处改、三面生效）
- [ ] **rank 映射**：`roleRank`（建号 rank 上限与主角色派生依赖；未登记角色默认 rank=0）
- [ ] **基线同步**：若属客户侧角色基线或平台/MSP 基线 → `internal/authz`（`roles.go` 绑定 / 客户侧基线函数）
- [ ] **克隆/供给对账**：seeder 模板与 `ProvisionTenant`（C1→C2 对账）是否需要同步
- [ ] **用例**：`service/msp_role_visibility_test.go` 矩阵、`service/platform_root_tenant_test.go` 谓词
- [ ] **可授予面**：`GrantableRoles`（通道/rank/租户类型过滤）是否需同步

### 1.3 反例（不归类会发生什么）

- 漏登记平台/MSP 词表 → 按"租户本地角色"默认放行：客户侧可见、可授（**扩张**）。
- 漏 rank 映射 → 建号 rank 上限与主角色派生按 0 处理（行为异常）。
- ✅ **守卫（补丁 7）**：`pkg/seeder/role_code_classification_guard_test.go`——`msp_`/`provider_` 前缀 ⊆ MSP 词表、平台保留码、词表反防腐化、兜底表键归类（含遗留白名单 `legacyFallbackRoleKeys`；历史键 `security` 登记为待收敛项）。

## 2. 新增权限（Permission Code）

### 2.1 登记与三处同步

权威源：`internal/authz.Definitions()`（`pkg/seeder` 的 `permissionDefinitions()` 为其 shim）。新增码必须满足：独立资源面 + 至少一个路由引用 + 至少一个角色授权。

- [ ] `internal/authz.Definitions()` 登记（码空间唯一源）
- [ ] 角色授权：`internal/authz/roles.go` 基线（默认角色矩阵）
- [ ] 预检映射：`cd itsm-backend && go run ./cmd/authz-gen` 重新生成 `middleware/rbac_precheck_gen.go`（`TestPrecheckMapIsFresh` / `TestRoutePrecheckAlignment` 守卫）
- [ ] 路由引用：`RequirePermission` 声明（目录守卫自动校验：引用未登记码 → CI 红）
- [ ] 分类：是否平台治理资源（见 §2.2）
- [ ] 前端联动：目录 `assignable` 标注（非平台根的通配/平台治理家族为 false）→ 角色页矩阵自动隐藏（补丁 8）

### 2.2 平台家族码评估（评估结论，2026-10-08）

判定标准：**该家族是否被"平台面"（L1 挂载路由 / 全局资源）独占依赖**——若是且租户无合法用途，纳入 `platformGovernedPermissionResources`（非平台根不可分配）；混合用途由 L1 身份校验承载，不纳入。

| 家族 | 码空间 | 路由面 | 角色基线 | 结论 |
|------|--------|--------|----------|------|
| `system` | 存在 | LLM 实例管理 / 运维命令 / RLS 指标 / 定时器（均挂 L1） | sysadmin | ✅ **纳入**（唯一平台治理家族） |
| `system_config` | 存在 | `/system-configs`、`/configs`、`/menus`、工单自动化（租户组，无 L1） | admin、directors | ⛔ 不纳入（租户管理面显式依赖） |
| `mcp` | 存在 | `/ai/mcp-servers`（租户组，无 L1） | admin / provider_tenant_admin 持 read+admin；sysadmin 全量 | ⛔ 不纳入（租户治理面；使用与治理分离） |
| `connector` | 存在 | `/connectors`（租户组，无 L1；handler 按 ctx tenant 作用域） | directors / sysadmin | ⛔ 不纳入（租户面） |
| `menu` | **不存在**（菜单管理复用 `system_config` 码） | — | — | ⛔ 无码可拦；若未来建码，按本节重新评估 |
| `federation` | **不存在**（码空间与路由均无引用） | — | — | ⛔ 同上 |
| `msp:*`（备注） | 存在 | 服务商面（allocation / membership 数据面收口） | MSP 五角色 | ⛔ 不适用本护栏（服务商自管面）；客户侧目录展示面为已登记残余（补丁 3） |

> 结论：护栏集合维持 `{system}`；机制为显式集合（补丁 6 起 `platformGovernedPermissionResources`）。**新增/调整家族必须先完成本表评估并更新本清单**，再改代码。

## 3. 新增路由（Route）

- [ ] **写路由**（POST/PUT/PATCH/DELETE）必须挂 `RequirePermission`（`router/route_permission_guard_test.go` 守卫；豁免仅三类且需写明理由）
- [ ] **平台面路由**（作用于全局/跨租户资源、写平台词表/身份）必须 **L1 + L2**（设计文档 §3.3）；租户治理写面为 handler 级 `requirePlatformAdmin`
- [ ] **预检映射**再生成（同 §2.1）
- [ ] **平台面登记**：平台面路由登记进守卫注册表（补丁 6：`router/platform_face_guard_test.go`；新增/改动平台面路由必须同步注册表）

## 4. 新增通道 / 场景（建号、分配、切换）

- [ ] **fail-closed**：无授权即拒绝，不做"宽松兜底"
- [ ] **membership 同步**：建号/分配通道必须同步 membership（MSW-1 教训：不写 membership → 切换后权限空窗）
- [ ] **同源过滤**：通道/rank/租户类型过滤与词表同源，不新增平行清单
- [ ] **数据面（L3）**：ctx 租户 + membership / allocation 双验

## 5. 不变量与守卫现状

| 不变量 | 守卫 | 状态 |
|--------|------|------|
| 写路由必须挂权限中间件 | `router/route_permission_guard_test.go` | ✅ |
| 路由引用码必须在权威目录 | `router/permission_code_catalog_guard_test.go` | ✅ |
| 预检映射新鲜 / 对齐 | `TestPrecheckMapIsFresh` / `TestRoutePrecheckAlignment` | ✅ |
| 角色绑定码 ⊆ 目录 | `pkg/seeder/role_permission_guard_test.go` | ✅ |
| 平台守卫口径（主角色 + m2m 并集） | `middleware/require_platform_admin_test.go` | ✅ |
| 角色三一致矩阵 | `service/msp_role_visibility_test.go` | ✅ |
| 保留码 / 通配 / 平台码三路径拒绝 | `service/role_config_bypass_guard_test.go` | ✅ |
| 平台根谓词 + 调用点 | `service/platform_root_tenant_test.go` | ✅ |
| 平台面路由必须挂 L1 | `router/platform_face_guard_test.go`（补丁 6：注册表 + 反向前缀 + handler 级登记） | ✅ |
| 平台家族码清单锁 | `service/role_service.go` 集合 + 决策锁用例 + 本清单 §2.2（补丁 6） | ✅ |
| 目录 assignable 标注与写面护栏同口径 | `service/permission_assignable_test.go`（补丁 8） | ✅ |
| 可授予候选标注与写面同口径（rank ≤ 调用者） | `service/role_grantable_test.go` + `handlers/rbac` 用例（补丁 9） | ✅ |
| 角色码归类完整性（`msp_`/`provider_` 前缀 ⊆ MSP 词表；兜底表键归类） | `pkg/seeder/role_code_classification_guard_test.go`（补丁 7，可失败性已验证） | ✅ |

## 6. 评审流程（怎么用本清单）

1. **扩展前**：按 §1/§2/§3/§4 完成归类决策与检查项勾选；
2. **扩展后**：跑守卫（`go test ./router/ ./service/ ./pkg/seeder/ ./middleware/` 定向）；平台面/实弹场景按需复测；
3. **回填**：更新本清单评估表/状态、设计文档 §5/§6 与 `CHANGELOG.md`。
