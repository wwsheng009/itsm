// Package role 提供全后端统一的用户角色词表。
//
// 背景（2026-09-07 服务请求深测 P1-B）：角色字符串此前散落在 7+ 处硬编码
// （users.role 单字段、roles.code 种子表、各 handler 的 switch/case、
// 审批资格 fallback 词表），三套词汇已实际漂移——service_request 审批角色
// manager/it_admin/security_admin 在 roles 表缺失、审批 403。
//
// 规则：
//   - 判断/比较角色时一律引用本包常量，禁止裸写字符串字面量；
//   - 新增角色必须同时更新 SeedRoles（见 internal/bootstrap 种子）；
//   - users.role 单字段与 roles.code 种子表共用本词表，两处不同步视为 Bug。
package role

// 内置角色 code（users.role 单字段值 = roles.code 种子值，单一事实源）。
const (
	SuperAdmin    = "super_admin"
	Admin         = "admin"
	Manager       = "manager"
	ITAdmin       = "it_admin"
	SecurityAdmin = "security_admin"
	Agent         = "agent"
	Technician    = "technician"
	SysAdmin      = "sysadmin"
	EndUser       = "end_user"
)

// 全量合法角色清单（播种与校验用）。顺序即层级语义参考，不作权限依据。
var All = []string{
	SuperAdmin,
	Admin,
	Manager,
	ITAdmin,
	SecurityAdmin,
	Agent,
	Technician,
	SysAdmin,
	EndUser,
}

// IsAdminLike 判断角色是否为管理类（可见全租户数据/绕过行级 scope）。
// 单一源对齐 handlers/common/datascope.IsDataScopeAllRole 与各处
// role == "admin" || role == "super_admin" 式硬编码。
func IsAdminLike(r string) bool {
	switch r {
	case SuperAdmin, Admin, Manager, SysAdmin:
		return true
	default:
		return false
	}
}

// data_scope 词表（与 ent/schema/role.go、handlers/common/datascope 保持一致）。
const (
	DataScopeAll        = "all"
	DataScopeDepartment = "department"
	DataScopeOwner      = "owner"
)

// IsValidDataScope 判断 data_scope 是否为合法枚举值。
func IsValidDataScope(scope string) bool {
	switch scope {
	case DataScopeAll, DataScopeDepartment, DataScopeOwner:
		return true
	default:
		return false
	}
}

// DataScopeRank 返回数据范围的宽度序号：all(0) > department(1) > owner(2)；
// 非法值返回 3（按最窄处理，seeder 会以规范值修正）。
func DataScopeRank(scope string) int {
	switch scope {
	case DataScopeAll:
		return 0
	case DataScopeDepartment:
		return 1
	case DataScopeOwner:
		return 2
	default:
		return 3
	}
}

// DefaultDataScope 返回内置角色的规范数据范围（单一源，避免 seeder / provisioner /
// datascope 各自硬编码漂移）。
//
// 口径：
//   - 管理类（IsAdminLike）与 MSP/provider 角色 → all；
//   - 部门管理/一线/二线 → department（工单仓储暂按 owner 安全收窄）；
//   - 其余（agent/technician/end_user/guest/岗位角色）→ owner，保持既有硬编码行为。
//
// 注意：这只是「角色 code → 默认值」的规范；运行时仍以 roles.data_scope 为准，
// 管理员显式调整后应保留（seeder 仅收敛仍为旧默认 all 的内置角色）。
func DefaultDataScope(code string) string {
	switch code {
	case SuperAdmin, Admin, Manager, SysAdmin,
		"provider_tenant_admin",
		"msp_viewer", "msp_tech", "msp_specialist", "msp_manager", "msp_admin":
		return DataScopeAll
	case "dept_manager", "sd_manager", "l1_support", "l2_support":
		return DataScopeDepartment
	default:
		return DataScopeOwner
	}
}

// builtinRoleCodes 是内置角色 code 集合（含 config/seed/default.json 的岗位角色）。
// 用途：存量角色可能尚未标记 is_system，运行时仍需按内置规范收敛 data_scope，
// 避免旧默认值 all 在迁移前造成越权。
var builtinRoleCodes = map[string]struct{}{
	SuperAdmin: {}, Admin: {}, Manager: {}, ITAdmin: {}, SecurityAdmin: {},
	Agent: {}, Technician: {}, SysAdmin: {}, EndUser: {},
	"guest": {}, "it_director": {}, "ops_director": {}, "audit_admin": {},
	"ops_manager": {}, "ops_engineer": {}, "dba": {}, "network_eng": {},
	"sd_manager": {}, "l1_support": {}, "l2_support": {}, "l3_expert": {},
	"change_manager": {}, "service_catalog_admin": {}, "dept_manager": {},
	"provider_tenant_admin": {},
	"msp_viewer":            {}, "msp_tech": {}, "msp_specialist": {}, "msp_manager": {}, "msp_admin": {},
}

// IsBuiltinRoleCode 判断角色 code 是否属于内置词表。
func IsBuiltinRoleCode(code string) bool {
	_, ok := builtinRoleCodes[code]
	return ok
}

// IsSuperAdmin 判断是否超级管理员。
func IsSuperAdmin(r string) bool { return r == SuperAdmin }

// IsServiceRequestApprover 判断角色是否可参与服务请求审批
// （L1 manager / L2 it_admin / L3 security_admin 及管理类兜底）。
// 与 handlers/service_request.checkEligibility 的 fallback 词表对齐。
func IsServiceRequestApprover(r string) bool {
	switch r {
	case Manager, ITAdmin, SecurityAdmin, Agent, Technician, Admin, SuperAdmin:
		return true
	default:
		return false
	}
}
