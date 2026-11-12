# MSP 分配角色映射配置化方案（Provider 级）

> 状态：**Implemented v1.1（2026-11-12，provider 级配置 + API/UI + 审计/fail-closed 加固）**
> 范围：把 `allocation.role → 客户租户 membership 角色` 的硬编码映射改为 provider 租户级可配置，并提供专用 API/UI、默认兜底与存量重算路径。
> 关联：[Membership + Switch 收口方案](./msp-membership-switch-consolidation-plan.md)（MSW-Q5）｜[目标架构 §5.3](./msp-target-architecture.md)｜[MSP 用户操作手册](../08-msp-user-operations-manual.md)｜[角色权限扩展清单](./role-permission-extension-checklist.md)

---

## 1. 背景与现状

当前分配同步客户 membership 时，映射是代码硬编码：

```go
// itsm-backend/service/membership_sync.go:483-489
func allocationMembershipRoles(allocRole string) (roleCode, mspRole string) {
    if strings.EqualFold(strings.TrimSpace(allocRole), "specialist") {
        return "msp_specialist", "msp_specialist"
    }
    return "msp_tech", "msp_tech"
}
```

当前口径：

| `allocation.role` | 客户租户 membership 角色 |
|---|---|
| `primary` | `msp_tech` |
| `backup` | `msp_tech` |
| `specialist` | `msp_specialist` |
| 空值/其他 | `msp_tech` |

与目标架构 §5.3 的 `primary → msp_manager` 存在已知口径差（MSW-Q5）。该差异已决定按最小权限维持 `msp_tech`，但不同服务商/合同可能需要不同映射，当前只能改代码。

另外两个固定点保持不变：

- `allocation.role` 合法值仍为 `primary / backup / specialist`（DTO `oneof` + 前端下拉）；
- 目标角色仍限定在客户租户 MSP 五角色：`msp_viewer / msp_tech / msp_specialist / msp_manager / msp_admin`。

本方案只把**映射关系**配置化，不把分配角色枚举开放为任意字符串。

---

## 2. 目标与非目标

### 2.1 目标

1. provider 管理员可在不重启、不改代码的情况下调整三个分配角色的客户租户目标角色；
2. 配置缺失、非法或目标角色不存在时，安全回退代码默认值；
3. 配置变更可审计；
4. 新分配立即使用新映射；
5. 存量 allocation membership 可通过现有 `backfill_membership -apply -reconcile` 重算；
6. 不破坏现有默认行为：没有配置行时与当前完全一致。

### 2.2 非目标

1. 不支持自定义新的 `allocation.role` 枚举；
2. 不支持任意 RBAC 角色（如 `admin`、`super_admin`）作为目标；
3. 本批不做客户级差异映射（留作后续可扩展点）；
4. 本批不改变 C1/C2 角色权限矩阵，只改变“选择哪个客户租户角色”。

---

## 3. 设计

### 3.1 配置载体

使用现有 `system_configs` 表，按 **provider 租户**存储：

| 字段 | 值 |
|---|---|
| `tenant_id` | provider 租户 ID |
| `key` | `msp.allocation_role_mapping` |
| `value_type` | `json` |
| `category` | `msp` |
| `value` | 三键 JSON 对象 |

示例：

```json
{
  "primary": "msp_tech",
  "backup": "msp_tech",
  "specialist": "msp_specialist"
}
```

选择 `system_configs` 的原因：

- 已有租户隔离（RLS）、缓存失效与配置审计基础；
- 无需新增表/迁移；
- 缺行 = 跟随默认值，升级零行为变化。

### 3.2 默认值与优先级

代码内置默认值保持现状：

```text
primary    → msp_tech
backup     → msp_tech
specialist → msp_specialist
```

读取优先级：

```text
provider 级 system_configs 覆盖
        ↓
代码默认值
```

后续如需要客户级差异，再加一层：

```text
customer 级覆盖 > provider 级覆盖 > 代码默认值
```

本批不实现 customer 级。

### 3.3 解析与角色校验

在 `MembershipSyncService.SyncAllocationMembership` 中：

1. 读取 `alloc.ProviderTenantID` 对应的 provider 配置；
2. 校验 JSON：键必须是 `primary/backup/specialist`，值必须是 MSP 五角色之一；
3. 取 `alloc.Role` 对应目标角色；
4. 在客户租户 `roles` 表中解析目标角色（同租户、启用、code 匹配）；
5. 目标角色不存在时：
   - 回退该分配角色的代码默认值；
   - 再解析一次；
   - 仍失败则保持现有行为：`role_id=NULL` + Warn（进入巡检清单），不得静默越权。
6. `membership.msp_role` 写最终目标角色 code（与现有行为一致）。

配置读取失败/JSON 非法（**v1.1 fail-closed**）：

- 无配置行：使用代码默认值（兼容历史行为）；
- 有配置行但为空/JSON 非法/缺键/未知键/非法值：标记 `corrupt=true`；
- 运行时 `SyncAllocationMembership` **返回错误、不创建/更新客户 membership**（分配行仍创建，需修复配置后重算）；
- 记 Warn，GET 返回 `corrupt=true` + `warning`，UI 红色告警并允许重新保存合法映射修复；
- 这样避免“管理员把权限收紧为 msp_viewer，但坏配置回退 msp_tech 反而放大权限”的 fail-open。

### 3.4 专用 API

不直接复用通用 `/system-configs` 编辑器（其权限为 `system_config:*`，范围过宽），新增 MSP 专用端点：

```http
GET /api/v1/msp/allocation-role-mapping
PUT /api/v1/msp/allocation-role-mapping
DELETE /api/v1/msp/allocation-role-mapping
```

权限：

| 方法 | 权限 |
|---|---|
| GET | `msp_allocation:read` |
| PUT/DELETE | `msp_allocation:write` |

GET 响应：

```json
{
  "mapping": {
    "primary": "msp_tech",
    "backup": "msp_tech",
    "specialist": "msp_specialist"
  },
  "defaults": {
    "primary": "msp_tech",
    "backup": "msp_tech",
    "specialist": "msp_specialist"
  },
  "allowedRoles": ["msp_viewer", "msp_tech", "msp_specialist", "msp_manager", "msp_admin"],
  "overridden": false,
  "updatedAt": null,
  "updatedBy": ""
}
```

PUT 请求：

```json
{
  "mapping": {
    "primary": "msp_tech",
    "backup": "msp_tech",
    "specialist": "msp_specialist"
  }
}
```

校验：

- 必须包含且只包含三个键；
- 值必须在 `allowedRoles` 内；
- 当前租户必须是 `msp_provider`；
- 写入后立即生效（新分配）。

DELETE：软删覆盖行，恢复代码默认值。

### 3.5 前端入口

在 `/msp/management` 增加「分配角色映射」卡片：

- 三行 Select：主支持 / 备份 / 专家；
- 显示当前生效值、默认值、来源（provider 配置 / 系统默认）；
- 保存、恢复默认；
- 保存后提示：仅影响新建分配；存量分配需执行重算命令；
- 无 `msp_allocation:write` 时只读。

### 3.6 安全边界

映射会改变客户租户内的授权角色，属于安全敏感配置：

1. 目标角色白名单仅 MSP 五角色；
2. 禁止 `admin/super_admin/agent` 等非 MSP 角色；
3. 建议平台侧后续增加“高危目标角色（`msp_manager/msp_admin`）审批”；本批先通过白名单 + 审计 + 文档约束；
4. 客户管理员仍可在客户租户内调整 `msp_*` 角色的权限内容（既有能力）；
5. 任何配置变更写入 `audit_logs`（v1.1 已落地：`resource=msp_allocation_role_mapping`，
   `action=msp.allocation_role_mapping.update/reset`，含 before/after、actor、provider tenant、
   请求路径/方法/IP）；MSP 路由不经过通用 AuditMiddleware，因此由服务层显式写审计；

### 3.7 存量重算

配置只影响后续分配。存量 allocation membership 使用现有命令重算：

```bash
# 预演
go run ./cmd/backfill_membership -reconcile

# 实际重算（幂等；source=allocation 且 active 的行）
go run ./cmd/backfill_membership -apply -reconcile
```

约束：

- 仅处理 `source=allocation` 的存活 membership；
- 不覆盖 `home/invite` 等异源 membership；
- 每次写入产生 `membership.sync` 审计；
- 重算后建议运行 `cmd/membership-drift-check`，确认 `FAIL=0`。

### 3.8 缓存与多实例

- 配置读取发生在分配创建/重算时，非高频热路径，**本批不引入进程内缓存**；
- 写入后下一次分配即读取新值；
- 若后续需要热路径缓存，复用 `system_configs` 失效广播与 TTL 模式。

---

## 4. 实施清单

### 4.1 后端

- [x] 新增 `service/msp_allocation_role_mapping.go`
      - 默认映射、允许角色、读取/校验/解析函数
      - `MSPAllocationService.Get/Update/ResetAllocationRoleMapping`
- [x] `service/membership_sync.go` 改用配置解析器
- [x] `dto/msp_dto.go` 新增视图/请求 DTO
- [x] `handlers/msp/handler.go` 新增 GET/PUT/DELETE handler
- [x] `router/msp_routes.go` 注册路由与权限
- [x] 单测：
      - 无配置 → 默认映射
      - 合法覆盖 → 目标角色
      - 非法 JSON/非法角色 → 回退默认
      - 目标角色不存在 → 回退默认，仍缺失则 `role_id=NULL`
      - 非 provider 租户拒绝
- [x] 审计与日志

### 4.2 前端

- [x] `types/msp.ts` 新增配置类型
- [x] `lib/api/msp-api.ts` 新增 GET/PUT/DELETE
- [x] `lib/services/msp-service.ts` 透传
- [x] `/msp/management` 增加映射配置卡片
- [x] 仅写权限可见保存/恢复默认
- [x] 前端类型检查与相关测试

### 4.3 文档

- [x] 本方案文档
- [x] 更新 `docs/multi-tenant/08-msp-user-operations-manual.md` 映射说明
- [x] 更新 `docs/multi-tenant/INDEX.md` 文档清单
- [x] CHANGELOG 记录

---

## 5. 验收

| 编号 | 场景 | 期望 |
|---|---|---|
| ARM-A1 | 无配置 | 新分配仍为 `primary/backup→msp_tech`、`specialist→msp_specialist` |
| ARM-A2 | 配置 `primary→msp_manager` | 新分配客户 membership 为 `msp_manager` |
| ARM-A3 | 配置非法 JSON/非法角色 | 回退默认且记 Warn，不阻断分配 |
| ARM-A4 | 客户租户缺少目标角色 | 回退默认；仍缺则 `role_id=NULL` + 巡检 |
| ARM-A5 | 非 provider 租户调用 PUT | 403/400 |
| ARM-A6 | GET 权限 | `msp_allocation:read` 可见；无写权限不可保存 |
| ARM-A7 | 配置变更 | 审计可见路径/操作者/前后值 |
| ARM-A8 | 存量重算 | `backfill_membership -apply -reconcile` 后角色更新，`membership.sync` 审计 |
| ARM-A9 | 默认行为回归 | 现有 membership_sync/router/E2E 测试通过 |
| ARM-A10 | 配置变更审计 | `audit_logs` 出现 update/reset 行，含 before/after 与 actor |
| ARM-A11 | 配置损坏 | GET `corrupt=true`；新分配 membership 同步 fail-closed，不写 membership |
| ARM-A12 | 目标角色停用 | 回退默认；默认也停用时 `role_id=NULL` + Warn |

---

## 6. 回滚

1. 删除/软删 `msp.allocation_role_mapping` 行 → 恢复代码默认；
2. 回滚代码 → 配置行保留但不再读取，无副作用；
3. 存量已重算的 membership 如需恢复：
   - 恢复默认配置后执行 `backfill_membership -apply -reconcile`；
   - 或按 allocation 重新分配。

---

## 7. 风险

| 风险 | 处置 |
|---|---|
| provider 将 primary 映射为 `msp_admin`，扩大客户权限 | 白名单 + 审计 + 文档；后续可加平台审批 |
| 配置指向客户租户不存在的角色 | 回退默认 + 巡检；不静默写空 |
| 存量 membership 未重算 | 文档明确命令；UI 保存提示 |
| 多实例读到旧值 | 本批无缓存，读取直连 DB；后续缓存需失效广播 |
| 配置行被误删 | 缺行 = 默认值，安全降级 |

---

## 8. 后续扩展

- 客户级覆盖表 `msp_allocation_role_mappings`（provider × customer）；
- 高危目标角色平台审批；
- 配置变更自动触发存量重算任务（异步 + 审计）。

---

## 9. 实施记录（2026-11-12）

### 9.1 变更文件

| 文件 | 变更 |
|---|---|
| `itsm-backend/service/msp_allocation_role_mapping.go` | 新增默认映射、白名单、配置读取/校验、GET/PUT/DELETE 服务方法、显式 audit_logs、corrupt 检测、DB 错误语义 |
| `itsm-backend/service/membership_sync.go` | 分配同步改用 provider 配置解析；目标角色缺失/停用回退默认；配置损坏 fail-closed |
| `itsm-backend/dto/msp_dto.go` | 新增映射视图/更新请求 DTO |
| `itsm-backend/handlers/msp/handler.go` | 新增映射 GET/PUT/DELETE handler |
| `itsm-backend/router/msp_routes.go` | 注册 `/api/v1/msp/allocation-role-mapping` |
| `itsm-backend/middleware/rbac_precheck_gen.go` | `authz-gen` 重新生成预检映射 |
| `itsm-frontend/src/types/msp.ts` | 新增映射类型 |
| `itsm-frontend/src/lib/api/msp-api.ts` | 新增映射 API |
| `itsm-frontend/src/lib/services/msp-service.ts` | 新增映射服务透传 |
| `itsm-frontend/src/pages/(main)/msp/management/index.tsx` | 新增「分配角色映射」卡片 |
| `itsm-backend/service/msp_allocation_role_mapping_test.go` | 默认/覆盖/重置/非法值/目标缺失/审计/损坏 fail-closed/停用角色回退测试 |
| `itsm-frontend/src/lib/api/__tests__/msp-api.test.ts` | 映射 API 契约测试 |

### 9.2 验证记录

| 命令/范围 | 结果 |
|---|---|
| `go build ./service ./handlers/msp ./router ./dto` | 通过 |
| `go test ./service -run 'TestMSPAllocationRoleMapping|TestMembershipSyncService|TestMSPAllocationService' -count=1` | 通过（含审计、损坏 fail-closed、停用角色回退） |
| `go test ./middleware -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1` | 通过 |
| `go test ./handlers/msp ./router -count=1` | 通过 |
| `npx tsc --noEmit -p tsconfig.json` | 通过 |
| `npx jest src/lib/api/__tests__/msp-api.test.ts src/lib/services/__tests__/msp-service.test.ts` | 通过 |
| `npm run build` | 通过 |
| `go test ./service ./handlers/msp ./router ./middleware ./dto -count=1` | `service` 全量运行时既有 `TestTicketSLAMatcher_L1TypeBindingPreferred` 唯一约束失败；单独/定向重跑通过，与本改动无关 |

### 9.3 使用口径

- 无配置行：行为与改动前完全一致；
- provider 管理员在 `/msp/management` 保存后：新分配立即生效；
- 存量分配：`go run ./cmd/backfill_membership -apply -reconcile`；
- 恢复默认：UI「恢复默认」或 `DELETE /api/v1/msp/allocation-role-mapping`。
- 配置损坏：新分配 membership 同步失败（分配行仍创建），修复配置后重跑重算；
- UI：加载中/加载失败可重试、配置损坏红色告警、高权限映射二次确认、响应式三列布局。
