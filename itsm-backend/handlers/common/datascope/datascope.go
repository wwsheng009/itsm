// Package datascope 提供跨域复用的行级数据权限（DataScope）原语。
//
// 该模式最初在 ticket 域落地（repository/ticket，阻断8 修复）：普通角色
// 只能看到本人创建或分配给自己的单据，管理角色可见全租户。此处将其抽离为
// 共享包，供 change/incident/problem/release/service_request 等域统一接入，
// 避免每个域各写一份枚举与角色判定。
package datascope

import (
	"context"

	"itsm-backend/common/tenantctx"
	"itsm-backend/domain/role"
	"itsm-backend/ent"
	entrole "itsm-backend/ent/role"
	"itsm-backend/ent/ticket"
)

// DataScope 行级数据权限范围。
type DataScope int

type dataScopeContextKey struct{}

const (
	// DataScopeAll 全租户可见（admin/manager/super_admin/sysadmin 等管理角色）。
	DataScopeAll DataScope = iota
	// DataScopeDepartment 仅可见本部门数据（需部门树查询支持）。
	DataScopeDepartment
	// DataScopeOwnedOrAssigned 仅可见本人创建或分配给本人的单据（end_user/agent）。
	DataScopeOwnedOrAssigned
)

// ParseDataScope 从 ent Role 的 data_scope 字段解析 DataScope 常量。
// 未知值默认返回 DataScopeOwnedOrAssigned（安全收窄）。
func ParseDataScope(dataScope string) DataScope {
	switch dataScope {
	case "all":
		return DataScopeAll
	case "department":
		return DataScopeDepartment
	case "owner":
		return DataScopeOwnedOrAssigned
	default:
		// 未知值安全收窄
		return DataScopeOwnedOrAssigned
	}
}

// FromRoleEntity 从 ent Role 实体获取 DataScope。
func FromRoleEntity(r *ent.Role) DataScope {
	if r == nil {
		return DataScopeOwnedOrAssigned
	}
	// role.DataScope 是 role.DataScope 类型，转换为 string 再解析
	return ParseDataScope(string(r.DataScope))
}

// IsDataScopeAllRole 判断角色是否拥有全租户可见权限（DataScopeAll）。
// 管理角色（super_admin/admin/manager/sysadmin）可见全租户数据，
// 其余角色（end_user/agent 等）只能查看本人创建或分配给自己的数据。
// 未知/空角色按非全量处理（安全默认：收窄而非放宽）。
// 词表单一源：domain/role.IsAdminLike。
func IsDataScopeAllRole(r string) bool {
	return role.IsAdminLike(r)
}

// IsDataScopeDepartmentRole 判断角色是否拥有本部门数据权限。
// 某些角色（如 department_manager）可配置为 department 档。
func IsDataScopeDepartmentRole(role string) bool {
	switch role {
	case "department_manager", "l1_support", "l2_support":
		return true
	default:
		return false
	}
}

// DataScopeFromRole 根据角色代码推断默认 DataScope。
// 注意：此函数用于 Role.data_scope 字段为空时的兜底推断，
// 正常应优先使用 Role 实体的 data_scope 字段。
func DataScopeFromRole(roleCode string) DataScope {
	return ParseDataScope(role.DefaultDataScope(roleCode))
}

// ResolveForActor 解析 actor 在当前租户内的有效数据范围。
//
// 角色来源：JWT 主角色 + RBACMiddleware 物化的生效角色（m2m 附加角色 +
// membership 指向的租户内角色）。调用方通常传 middleware.GetContextRoles(c)。
//
// 解析规则（安全优先）：
//  1. super_admin → DataScopeAll（平台直通，保持既有语义）；
//  2. 逐角色查询当前租户 roles.data_scope；
//  3. 内置角色（is_system=true 或 code 命中内置词表）取「DB 值与规范默认值中更窄者」：
//     既允许管理员把 msp_tech 从 all 收紧为 owner，也防止旧默认 all 让
//     end_user/agent 在迁移前越权；
//  4. 自定义角色以 DB data_scope 为准；
//  5. 角色行缺失/DB 不可用 → 回退 roles 词表的规范默认值；
//  6. 多角色取最宽范围（all > department > owner），与权限并集语义一致。
func ResolveForActor(
	ctx context.Context,
	client *ent.Client,
	tenantID int,
	primaryRole string,
	roleCodes []string,
) DataScope {
	if primaryRole == role.SuperAdmin {
		return DataScopeAll
	}
	codes := mergeRoleCodes(primaryRole, roleCodes)
	if len(codes) == 0 {
		return DataScopeOwnedOrAssigned
	}

	// 无 DB 客户端 / 无效租户：按内置词表规范值回退（测试与迁移窗口）。
	if client == nil || tenantID <= 0 {
		return dataScopeFromCanonical(codes)
	}

	ctx = tenantctx.WithTenantID(ctx, tenantID)
	entities, err := client.Role.Query().
		Where(
			entrole.TenantID(tenantID),
			entrole.CodeIn(codes...),
		).
		All(ctx)
	if err != nil || len(entities) == 0 {
		return dataScopeFromCanonical(codes)
	}

	byCode := make(map[string]*ent.Role, len(entities))
	for _, entity := range entities {
		byCode[entity.Code] = entity
	}

	best := DataScopeOwnedOrAssigned
	for _, code := range codes {
		entity, ok := byCode[code]
		if !ok || entity == nil {
			best = widerDataScope(best, ParseDataScope(role.DefaultDataScope(code)))
			continue
		}
		effective := FromRoleEntity(entity)
		if entity.IsSystem || role.IsBuiltinRoleCode(code) {
			// 内置角色：DB 值只允许比规范值更窄，不允许旧默认 all 放宽。
			effective = narrowerDataScope(effective, ParseDataScope(role.DefaultDataScope(code)))
		}
		best = widerDataScope(best, effective)
	}
	return best
}

// WithDataScope 把已解析的数据范围写入请求 context，供服务层写守卫复用。
func WithDataScope(ctx context.Context, scope DataScope) context.Context {
	return context.WithValue(ctx, dataScopeContextKey{}, scope)
}

// DataScopeFromContext 读取请求 context 中已解析的数据范围。
func DataScopeFromContext(ctx context.Context) (DataScope, bool) {
	if ctx == nil {
		return DataScopeOwnedOrAssigned, false
	}
	scope, ok := ctx.Value(dataScopeContextKey{}).(DataScope)
	return scope, ok
}

// dataScopeFromCanonical 按内置词表规范值计算角色集合的最宽范围。
func dataScopeFromCanonical(codes []string) DataScope {
	best := DataScopeOwnedOrAssigned
	for _, code := range codes {
		best = widerDataScope(best, ParseDataScope(role.DefaultDataScope(code)))
	}
	return best
}

// mergeRoleCodes 去重合并主角色与生效角色列表，忽略空值。
func mergeRoleCodes(primaryRole string, roleCodes []string) []string {
	seen := make(map[string]struct{}, len(roleCodes)+1)
	merged := make([]string, 0, len(roleCodes)+1)
	appendCode := func(code string) {
		if code == "" {
			return
		}
		if _, ok := seen[code]; ok {
			return
		}
		seen[code] = struct{}{}
		merged = append(merged, code)
	}
	appendCode(primaryRole)
	for _, code := range roleCodes {
		appendCode(code)
	}
	return merged
}

// widerDataScope 返回更宽的范围（数值越小越宽）。
func widerDataScope(a, b DataScope) DataScope {
	if a <= b {
		return a
	}
	return b
}

// narrowerDataScope 返回更窄的范围（数值越大越窄）。
func narrowerDataScope(a, b DataScope) DataScope {
	if a >= b {
		return a
	}
	return b
}

// CanWriteResource 判定 actor 是否可对单据执行写/删操作（行级写权限）。
//
// 原则：写权限 ⊆ 读权限。DataScope 决定读不到的单据，同样不允许写/删：
//   - 管理角色（IsDataScopeAllRole：super_admin/admin/manager/sysadmin）全租户可写；
//   - 其余角色仅 owner（创建人/报告人）或当前受理人可写。
//
// ownerID 为单据的归属人字段（ticket.RequesterID / change.CreatedBy /
// problem.CreatedBy / incident.ReporterID）；assigneeID 可为 nil（未分配）。
// actorID <= 0 视为无有效身份，按拒绝处理（安全收窄）。
func CanWriteResource(actorID int, actorRole string, ownerID int, assigneeID *int) bool {
	if IsDataScopeAllRole(actorRole) {
		return true
	}
	return CanWriteResourceWithScope(actorID, DataScopeOwnedOrAssigned, ownerID, assigneeID)
}

// CanWriteResourceWithScope 按已解析的 DataScope 判定行级写权限。
// 规则与 CanWriteResource 一致，但角色来源由调用方按租户 membership 解析，
// 避免深切换后 JWT 主角色（end_user）与 membership 角色（msp_tech）口径分叉。
func CanWriteResourceWithScope(actorID int, scope DataScope, ownerID int, assigneeID *int) bool {
	if scope == DataScopeAll {
		return true
	}
	if actorID <= 0 {
		return false
	}
	if actorID == ownerID {
		return true
	}
	if assigneeID != nil && *assigneeID > 0 && actorID == *assigneeID {
		return true
	}
	return false
}

// CanWriteResourceFromContext 优先使用请求 context 中已解析的租户级 DataScope；
// 未注入时回退到按 JWT 主角色推断的旧行为（单测/非租户路由兼容）。
func CanWriteResourceFromContext(ctx context.Context, actorID int, actorRole string, ownerID int, assigneeID *int) bool {
	if scope, ok := DataScopeFromContext(ctx); ok {
		return CanWriteResourceWithScope(actorID, scope, ownerID, assigneeID)
	}
	return CanWriteResource(actorID, actorRole, ownerID, assigneeID)
}

// ApplyTicketFilter 将 DataScope 过滤器应用到 ticket 查询。
// query 为 ent Ticket 查询，userID 为当前用户 ID，departmentID 为用户所属部门。
//
// 示例:
//
//	query := client.Ticket.Query()
//	query = datascope.ApplyTicketFilter(query, 123, 456, DataScopeOwnedOrAssigned)
func ApplyTicketFilter(query *ent.TicketQuery, userID, departmentID int, ds DataScope) *ent.TicketQuery {
	switch ds {
	case DataScopeAll:
		// 无过滤，全量
		return query
	case DataScopeDepartment:
		// 本人创建 或 本部门
		if userID <= 0 {
			return query.Where(ticket.IDEQ(-1)) // 安全收窄
		}
		return query.Where(ticket.Or(
			ticket.RequesterID(userID),
			ticket.DepartmentID(departmentID),
		))
	case DataScopeOwnedOrAssigned:
		// 本人创建 或 分配给本人
		if userID <= 0 {
			return query.Where(ticket.IDEQ(-1)) // 安全收窄
		}
		return query.Where(ticket.Or(
			ticket.RequesterID(userID),
			ticket.AssigneeID(userID),
		))
	default:
		// 未知 scope 安全收窄
		return query.Where(ticket.IDEQ(-1))
	}
}
