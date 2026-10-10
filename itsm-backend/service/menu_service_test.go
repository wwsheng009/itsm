package service

import (
	"testing"

	"itsm-backend/ent"
)

func TestShouldRestrictMenuForRole(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		roleCodes map[string]bool
		want      bool
	}{
		{
			name:      "end user cannot see workflow menu",
			path:      "/workflow/dashboard",
			roleCodes: map[string]bool{"end_user": true},
			want:      true,
		},
		{
			name:      "security cannot see admin menu",
			path:      "/admin/users",
			roleCodes: map[string]bool{"security": true},
			want:      true,
		},
		{
			name:      "admin can see workflow menu",
			path:      "/workflow/dashboard",
			roleCodes: map[string]bool{"admin": true},
			want:      false,
		},
		{
			name:      "provider tenant admin is elevated (07:G11)",
			path:      "/admin/users",
			roleCodes: map[string]bool{"provider_tenant_admin": true},
			want:      false,
		},
		{
			name:      "main menu stays visible for end user",
			path:      "/incidents",
			roleCodes: map[string]bool{"end_user": true},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRestrictMenuForRole(tt.path, tt.roleCodes)
			if got != tt.want {
				t.Fatalf("shouldRestrictMenuForRole(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestFilterMenusByPermissionRestrictsLowPrivilegeAdminMenus(t *testing.T) {
	svc := &MenuService{}
	menus := []*ent.Menu{
		{Name: "事件管理", Path: "/incidents", PermissionCode: "incident:read"},
		{Name: "工作流", Path: "/workflow", PermissionCode: "workflow:read"},
		{Name: "用户管理", Path: "/admin/users", PermissionCode: "user:read"},
	}

	filtered := svc.filterMenusByPermission(
		menus,
		map[string]bool{
			"incident:read": true,
			"workflow:read": true,
			"user:read":     true,
		},
		map[string]bool{"end_user": true},
	)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 visible menu, got %d", len(filtered))
	}

	if filtered[0].Path != "/incidents" {
		t.Fatalf("expected incidents menu to remain visible, got %s", filtered[0].Path)
	}
}

// TestBuildMenuTreeReturnsEmptySlicesNotNull （2026-10-04 UI 验收回归）：
// provider 用户没有管理菜单时，buildMenuTree 曾返回 nil → JSON `null` →
// 前端 Header 面包屑 `for...of null` 抛 TypeError → 整页 ErrorBoundary。
// 契约：main/admin 恒为非 nil 数组（可为空）。
func TestBuildMenuTreeReturnsEmptySlicesNotNull(t *testing.T) {
	svc := &MenuService{}

	mainMenus, adminMenus := svc.buildMenuTree([]*ent.Menu{
		{Name: "事件管理", Path: "/incidents"},
	})
	if mainMenus == nil || adminMenus == nil {
		t.Fatalf("仅主菜单时不得返回 nil：main=%v admin=%v", mainMenus, adminMenus)
	}
	if len(adminMenus) != 0 {
		t.Fatalf("admin 应为空数组，实际 %d 项", len(adminMenus))
	}

	mainOnlyAdmin, adminOnlyAdmin := svc.buildMenuTree([]*ent.Menu{
		{Name: "用户管理", Path: "/admin/users"},
	})
	if mainOnlyAdmin == nil || adminOnlyAdmin == nil {
		t.Fatalf("仅管理菜单时不得返回 nil：main=%v admin=%v", mainOnlyAdmin, adminOnlyAdmin)
	}
	if len(mainOnlyAdmin) != 0 || len(adminOnlyAdmin) != 1 {
		t.Fatalf("分组错误：main=%d admin=%d", len(mainOnlyAdmin), len(adminOnlyAdmin))
	}
}

// TestFilterMenusByPermissionStrictSemantics（2026-10-10 菜单挂载收口）：
// 菜单匹配收紧为与 middleware.checkPermissionMatch 同语义——不再做 action/resource 别名映射。
// 回归案例：admin 角色持 system_config:write，不得因此看到 permission_code=system:write 的
// /admin/tenants 菜单（此前靠 system→system_config 别名误挂载，点进去 403）。
func TestFilterMenusByPermissionStrictSemantics(t *testing.T) {
	svc := &MenuService{}
	tenantsMenu := &ent.Menu{Name: "租户管理", Path: "/admin/tenants", PermissionCode: "system:write"}
	teamsMenu := &ent.Menu{Name: "团队管理", Path: "/admin/teams", PermissionCode: "team:write"}
	mcpReadMenu := &ent.Menu{Name: "MCP 外部工具", Path: "/admin/mcp-servers", PermissionCode: "mcp:read"}

	tests := []struct {
		name        string
		menu        *ent.Menu
		permissions map[string]bool
		want        bool
	}{
		{"别名不泄漏：system_config:write 不授予 system:write", tenantsMenu, map[string]bool{"system_config:write": true}, false},
		{"别名不泄漏：org:read 不授予 team:write", teamsMenu, map[string]bool{"org:read": true}, false},
		{"精确匹配授予", tenantsMenu, map[string]bool{"system:write": true}, true},
		{"资源通配授予", tenantsMenu, map[string]bool{"system:*": true}, true},
		{"全局通配授予", tenantsMenu, map[string]bool{"*": true}, true},
		{"资源 admin 蕴含具体动作", tenantsMenu, map[string]bool{"system:admin": true}, true},
		{"mcp:admin 不蕴含 mcp:read（与 middleware 一致）", mcpReadMenu, map[string]bool{"mcp:admin": true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := svc.filterMenusByPermission(
				[]*ent.Menu{tt.menu},
				tt.permissions,
				map[string]bool{"admin": true},
			)
			got := len(filtered) == 1
			if got != tt.want {
				t.Fatalf("菜单 %s + 权限 %v：可见=%v，期望 %v", tt.menu.Path, tt.permissions, got, tt.want)
			}
		})
	}
}

// TestFilterMenusByPermissionKeepsAncestorsOfVisibleChildren（容器节点语义）：
// 父菜单自身权限未命中、任一子菜单命中时，父节点保留（否则层级菜单子项整体孤儿化）；
// 无任何命中时父节点照常剔除。
func TestFilterMenusByPermissionKeepsAncestorsOfVisibleChildren(t *testing.T) {
	svc := &MenuService{}
	parent := &ent.Menu{ID: 1, Name: "系统管理", Path: "/admin", PermissionCode: "system:write"}
	child := &ent.Menu{ID: 2, Name: "用户管理", Path: "/admin/users", PermissionCode: "user:read", ParentID: &parent.ID}
	hiddenChild := &ent.Menu{ID: 3, Name: "租户管理", Path: "/admin/tenants", PermissionCode: "system:write", ParentID: &parent.ID}

	// 仅 user:read：父（system:write 未命中）因可见子项保留；system:write 子项剔除。
	filtered := svc.filterMenusByPermission(
		[]*ent.Menu{parent, child, hiddenChild},
		map[string]bool{"user:read": true},
		map[string]bool{"admin": true},
	)
	if len(filtered) != 2 || filtered[0].Path != "/admin" || filtered[1].Path != "/admin/users" {
		t.Fatalf("容器保留语义失败：%v", menuPaths(filtered))
	}

	// 无任何命中：父与子全部剔除（不做空容器保留）。
	filtered = svc.filterMenusByPermission(
		[]*ent.Menu{parent, child, hiddenChild},
		map[string]bool{"ticket:read": true},
		map[string]bool{"admin": true},
	)
	if len(filtered) != 0 {
		t.Fatalf("无命中时不应保留容器：%v", menuPaths(filtered))
	}
}

func menuPaths(menus []*ent.Menu) []string {
	paths := make([]string, 0, len(menus))
	for _, m := range menus {
		paths = append(paths, m.Path)
	}
	return paths
}
