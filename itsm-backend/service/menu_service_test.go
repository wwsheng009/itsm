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
