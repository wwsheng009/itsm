package authz

import "itsm-backend/pkg/tenantmode"

// switch.go：租户切换（tenant.switch）授权判定单一入口（MSW-2；Q1=C）。
//
// 设计：纯函数判定——事实（目标租户、allocation、invite membership）由调用方
// （handlers/auth）查询后注入，便于决策表测试与口径单源；审计由调用方落
// （allow → tenant.switch/basis；deny → tenant.switch_denied/reasonCode）。
//
// Q1=C 语义：邀请来源放行的依据是"客户侧显式确认"——即目标客户租户内存在
// source=invite ∧ status=active ∧ invited_by 非空的存活 membership（该行只能由
// 目标租户管理员/平台/已有 allocation 的服务商管理员经邀请流建立）。不因 invite 泛化。

// 授权依据（allow 分支）。
const (
	SwitchBasisNative     = "native"
	SwitchBasisSuperAdmin = "super_admin"
	SwitchBasisAllocation = "allocation"
	SwitchBasisInvite     = "invite"
)

// 拒绝码（deny 分支；写入 tenant.switch_denied 审计 reasonCode）。
const (
	SwitchDenyUserInactive    = "user_inactive"
	SwitchDenyNoGrant         = "no_grant"
	SwitchDenyTargetNotFound  = "target_not_found"
	SwitchDenyTargetSuspended = "target_suspended"
	SwitchDenyTargetExpired   = "target_expired"
)

// SwitchActor 切换调用方（账号）快照。
type SwitchActor struct {
	HomeTenantID int
	// HomeTenantType 家租户类型（tenantmode 词表）；provider 判定用。
	HomeTenantType string
	Role           string
	MSPRole        string
	Active         bool
}

// SwitchScope 目标作用域事实。
type SwitchScope struct {
	TargetTenantID int
	TargetFound    bool
	TargetType     string // 目标租户类型（tenantmode 词表；未知为空）
	TargetStatus   string // active/suspended/...
	TargetExpired  bool   // 有到期时间且已过期
}

// SwitchFacts 授权依据事实（按目标租户预查询）。
type SwitchFacts struct {
	HasActiveAllocation bool // msp_allocations：未撤销（deassigned_at IS NULL）
	HasActiveInvite     bool // user_tenant_memberships：source=invite ∧ active ∧ invited_by 非空 ∧ 未软删
}

// SwitchDecision 判定结果。
type SwitchDecision struct {
	Allowed bool
	Basis   string // allow 时的依据（SwitchBasis*）
	Reason  string // deny 时的机器可读码（SwitchDeny*）
}

// DecideTenantSwitch 判定 tenant.switch：
//  1. 账号停用 → 拒绝（user_inactive；被停用账号不得换发令牌）；
//  2. native（目标=家租户）→ 放行；
//  3. super_admin → 放行（平台判定）；
//  4. provider 员工（msp_role 非空）→ 客户租户：active allocation 或
//     客户侧显式确认的 invite membership → 放行；
//  5. 其余一律拒绝（fail-closed）。
//
// 顺序：先判授权依据、后校验目标状态——未授权者不进入目标存在性分支
// （防枚举，统一 no_grant）；已授权者才可能得到 target_not_found/suspended/expired。
func DecideTenantSwitch(actor SwitchActor, scope SwitchScope, facts SwitchFacts) SwitchDecision {
	if !actor.Active {
		return SwitchDecision{Reason: SwitchDenyUserInactive}
	}
	basis := ""
	switch {
	case actor.HomeTenantID > 0 && actor.HomeTenantID == scope.TargetTenantID:
		basis = SwitchBasisNative
	case actor.Role == "super_admin":
		basis = SwitchBasisSuperAdmin
	case actor.MSPRole != "" && tenantmode.IsMSPProviderTenantType(actor.HomeTenantType) &&
		scope.TargetFound && tenantmode.IsCustomerTenantType(scope.TargetType):
		if facts.HasActiveAllocation {
			basis = SwitchBasisAllocation
		} else if facts.HasActiveInvite {
			basis = SwitchBasisInvite
		}
	}
	if basis == "" {
		return SwitchDecision{Reason: SwitchDenyNoGrant}
	}
	if !scope.TargetFound {
		return SwitchDecision{Reason: SwitchDenyTargetNotFound}
	}
	if scope.TargetStatus != "active" {
		return SwitchDecision{Reason: SwitchDenyTargetSuspended}
	}
	if scope.TargetExpired {
		return SwitchDecision{Reason: SwitchDenyTargetExpired}
	}
	return SwitchDecision{Allowed: true, Basis: basis}
}
