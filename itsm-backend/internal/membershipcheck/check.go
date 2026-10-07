// Package membershipcheck 提供多租户 membership（展示/权限单源）与请求期事实的一致性巡检
// （M4 / MSW-A6，方案 §4 M4）。
//
// 背景：菜单与权限清单走 membership 单源（IP-P1-2），而请求期 RBACMiddleware 仍以
// users.role / users.msp_role（JWT 角色码）为输入；两类事实由写入路径同步（M1），但历史
// 回填、手工数据与并发写入仍可能漂移。本包把可检测的漂移分类成清单（纯函数，便于夹具测试）。
//
// 分类与阈值：
//   - FAIL（必须修复；>0 时 CLI 退出码 1）：
//     missing_membership       活跃账号缺 home membership；provider 有活跃分配但缺客户作用域 membership
//     role_id_null             活跃 membership 的 role_id 为空（权限清单 fail-closed 为空集）
//     status_mismatch          停用账号仍有活跃 membership；已软删 membership 仍 status=active；
//     source=allocation 的活跃 membership 无活跃分配
//     customer_scope_conflict  客户账号存在 >1 条活跃 membership（DB 部分唯一索引兜底，巡检复核）
//   - WARN（观察项；默认不阻断，--strict 时计入退出码）：
//     account_kind_snapshot    membership.account_kind 与账号当前类型（platform/provider/customer）不一致
//     jwt_role_mismatch        客户账号：membership 角色码 vs users.role；
//     provider：membership.msp_role vs users.msp_role
package membershipcheck

import (
	"fmt"
	"sort"
	"strings"

	"itsm-backend/pkg/tenantmode"
)

// Severity 漂移级别。
type Severity string

const (
	// SeverityFail 必须修复（阻断）。
	SeverityFail Severity = "FAIL"
	// SeverityWarn 观察项（默认不阻断）。
	SeverityWarn Severity = "WARN"
)

// UserFact 账号当前事实（users 行）。
type UserFact struct {
	ID       int
	Username string
	Active   bool
	Role     string // users.role（legacy 角色码，JWT 输入）
	MSPRole  string // users.msp_role（provider 词表）
	TenantID int    // home 租户
}

// MembershipFact 成员身份事实（user_tenant_memberships 行）。
type MembershipFact struct {
	ID          int
	UserID      int
	TenantID    int
	AccountKind string // customer|provider|platform（快照）
	Source      string // home|allocation|platform|invite|migration
	RoleID      *int
	MSPRole     string
	Status      string // active|suspended
	IsDefault   bool
	DeletedAt   bool // 软删标记
}

// AllocationFact 活跃分配事实（msp_allocations 行，deassigned_at IS NULL）。
type AllocationFact struct {
	MSPUserID        int
	CustomerTenantID int
	ProviderTenantID int
}

// RoleFact 角色字典（roles 行，用于 role_id → code 比较）。
type RoleFact struct {
	ID       int
	TenantID int
	Code     string
}

// TenantFact 租户类型（account_kind 期望值判定输入，与写入路径 accountKindForHome 同口径）。
type TenantFact struct {
	ID   int
	Type string
}

// Facts 巡检输入快照。
type Facts struct {
	Users       []UserFact
	Memberships []MembershipFact
	Allocations []AllocationFact
	Roles       []RoleFact
	Tenants     []TenantFact
}

// Finding 单条漂移。
type Finding struct {
	Category string
	Severity Severity
	UserID   int
	TenantID int
	Detail   string
}

// Check 执行全部巡检规则，返回按 (category, userID, tenantID) 排序的漂移清单。
func Check(f Facts) []Finding {
	var out []Finding
	add := func(cat string, sev Severity, uid, tid int, detail string) {
		out = append(out, Finding{Category: cat, Severity: sev, UserID: uid, TenantID: tid, Detail: detail})
	}

	roleCode := make(map[int]string, len(f.Roles))
	for _, r := range f.Roles {
		roleCode[r.ID] = r.Code
	}
	userByID := make(map[int]UserFact, len(f.Users))
	for _, u := range f.Users {
		userByID[u.ID] = u
	}
	activeAlloc := make(map[[2]int]bool, len(f.Allocations))
	for _, a := range f.Allocations {
		activeAlloc[[2]int{a.MSPUserID, a.CustomerTenantID}] = true
	}
	activeByUser := make(map[int][]MembershipFact, len(f.Memberships))
	for _, m := range f.Memberships {
		if m.Status == "active" && !m.DeletedAt {
			activeByUser[m.UserID] = append(activeByUser[m.UserID], m)
		}
		if m.DeletedAt && m.Status == "active" {
			add("status_mismatch", SeverityFail, m.UserID, m.TenantID, "membership 已软删但 status=active")
		}
	}
	tenantType := make(map[int]string, len(f.Tenants))
	for _, t := range f.Tenants {
		tenantType[t.ID] = t.Type
	}

	for _, u := range f.Users {
		actives := activeByUser[u.ID]
		expected := expectedKind(u, tenantType[u.TenantID])

		home := false
		for _, m := range actives {
			if m.TenantID == u.TenantID && (m.IsDefault || m.Source == "home") {
				home = true
			}
		}
		if u.Active && !home {
			add("missing_membership", SeverityFail, u.ID, u.TenantID,
				"活跃账号缺 home membership（期望 source=home 或 is_default）")
		}
		if !u.Active && len(actives) > 0 {
			add("status_mismatch", SeverityFail, u.ID, u.TenantID,
				fmt.Sprintf("账号已停用但仍有 %d 条活跃 membership", len(actives)))
		}
		if expected == "customer" && len(actives) > 1 {
			add("customer_scope_conflict", SeverityFail, u.ID, u.TenantID,
				fmt.Sprintf("客户账号存在 %d 条活跃 membership（应恰 1 条）", len(actives)))
		}

		for _, m := range actives {
			if m.RoleID == nil {
				add("role_id_null", SeverityFail, u.ID, m.TenantID,
					"活跃 membership 的 role_id 为空（权限清单将为空集）")
			}
			if m.AccountKind != expected {
				add("account_kind_snapshot", SeverityWarn, u.ID, m.TenantID,
					fmt.Sprintf("account_kind=%s 与账号当前类型 %s 不一致（快照过期）", m.AccountKind, expected))
			}
			if m.Source == "allocation" && !activeAlloc[[2]int{u.ID, m.TenantID}] {
				add("status_mismatch", SeverityFail, u.ID, m.TenantID,
					"source=allocation 的活跃 membership 无对应活跃分配")
			}
			// JWT 角色 vs 权限码（请求期 RBAC 输入 vs membership 单源）。
			if expected == "customer" && m.TenantID == u.TenantID && m.RoleID != nil {
				if code, ok := roleCode[*m.RoleID]; ok && code != u.Role {
					add("jwt_role_mismatch", SeverityWarn, u.ID, m.TenantID,
						fmt.Sprintf("membership 角色码 %s vs users.role %s", code, u.Role))
				}
			}
			if expected == "provider" && m.TenantID == u.TenantID && m.MSPRole != u.MSPRole {
				if accept := acceptableScopeRoles(u.MSPRole); accept != nil && !contains(accept, m.MSPRole) {
					add("jwt_role_mismatch", SeverityWarn, u.ID, m.TenantID,
						fmt.Sprintf("membership.msp_role %q vs users.msp_role %q（期望 %s）",
							m.MSPRole, u.MSPRole, strings.Join(accept, "/")))
				}
			}
		}
	}

	// provider 员工有活跃分配但缺客户作用域 membership（切换后权限空窗的直接来源）。
	for _, a := range f.Allocations {
		u, ok := userByID[a.MSPUserID]
		if !ok || !u.Active || expectedKind(u, tenantType[u.TenantID]) != "provider" {
			continue
		}
		found := false
		for _, m := range activeByUser[u.ID] {
			if m.TenantID == a.CustomerTenantID && m.AccountKind == "provider" {
				found = true
				break
			}
		}
		if !found {
			add("missing_membership", SeverityFail, u.ID, a.CustomerTenantID,
				"provider 员工有活跃分配但缺客户作用域 membership（切换后权限空窗）")
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		return out[i].TenantID < out[j].TenantID
	})
	return out
}

// Summary 统计 FAIL/WARN 数量。
func Summary(findings []Finding) (fail, warn int) {
	for _, f := range findings {
		switch f.Severity {
		case SeverityFail:
			fail++
		case SeverityWarn:
			warn++
		}
	}
	return fail, warn
}

// expectedKind 账号当前类型（与写入路径 accountKindForHome 同口径：home 租户类型优先，
// 其次 users.msp_role 词表；不按目标租户类型倒置）。
// 唯一覆盖：super_admin 固定 platform（canon D5 平台根账号；同时兼容存量
// default 租户类型被标为 msp_provider 的数据异常，避免平台管理员被误判为 provider）。
func expectedKind(u UserFact, homeTenantType string) string {
	if u.Role == "super_admin" {
		return "platform"
	}
	if tenantmode.IsMSPProviderTenantType(homeTenantType) || isProviderMSPRole(u.MSPRole) {
		return "provider"
	}
	if tenantmode.IsCustomerTenantType(homeTenantType) {
		return "customer"
	}
	return "platform"
}

// isProviderMSPRole 服务方账号角色（users.msp_role 词表，与 service.isProviderMSPRole 同口径）。
func isProviderMSPRole(mspRole string) bool {
	switch strings.ToLower(strings.TrimSpace(mspRole)) {
	case "provider_admin", "provider_agent":
		return true
	}
	return false
}

// acceptableScopeRoles 账号级 msp_role → 作用域内 msp_role 词表（D10；与
// middleware.mspRoleToRBACRoleMap 同源：provider_admin→msp_manager、provider_agent→msp_tech；
// 兼容既有 msp_admin 口径）。返回 nil 表示未知账号词表，跳过比较（不产生噪声告警）。
func acceptableScopeRoles(accountRole string) []string {
	switch accountRole {
	case "provider_admin":
		return []string{"msp_admin", "msp_manager"}
	case "provider_agent":
		return []string{"msp_tech"}
	case "provider_viewer":
		return []string{"msp_viewer"}
	default:
		return nil
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
