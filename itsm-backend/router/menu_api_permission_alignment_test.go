package router

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"itsm-backend/middleware"
)

// menuRouteAlignment 菜单页 → 页面主数据接口的权限对齐配对表（2026-10-10）。
//
// 背景：/tickets/analytics 菜单 permission_code=ticket:read，而接口曾要求 report:read，
// 结果 technician 菜单可见、进页后 403，前端 catch-all 又把它显示成"获取数据失败"。
// 这类漂移的根因是「菜单可见性 / 前端路由守卫 / 后端接口」三层权限码不同源。
//
// 本测试锁定配对表两侧的权限码一致：
//   - 菜单侧：AST 解析 pkg/seeder/seeder.go 的 []menuSpec 规格（种子数据单一来源）；
//   - 接口侧：middleware.ScanDeclaredPermissionRoutes 扫描路由声明（与预检生成同源）。
//
// 已知未对齐配对进 menuRouteMismatchAllowlist（须写明理由与去向）；修复对齐后
// 条目失效会被本测试强制清理（对齐 precheckMismatchAllowlist 的防腐化约定）。
var menuRouteAlignment = []struct {
	MenuPath  string
	Method    string
	RoutePath string // middleware.DeclaredRoute.FullPath（带 /api/v1 前缀、路径参数归一为 *）
}{
	// 2026-10-10 修复的漂移案例（三层同码 ticket:read）。
	{"/tickets/analytics", "GET", "/api/v1/analytics/tickets"},
	// 其余为核对过口径一致的页面主数据接口（只读）。
	{"/tickets", "GET", "/api/v1/tickets"},
	{"/tickets/types", "GET", "/api/v1/ticket-types"},
	{"/incidents", "GET", "/api/v1/incidents"},
	{"/problems", "GET", "/api/v1/problems"},
	{"/changes", "GET", "/api/v1/changes"},
	{"/knowledge", "GET", "/api/v1/knowledge/articles"},
	{"/cmdb", "GET", "/api/v1/cmdb/cis"},
	{"/assets", "GET", "/api/v1/assets"},
	{"/sla", "GET", "/api/v1/sla/definitions"},
	{"/releases", "GET", "/api/v1/releases"},
	{"/audit-logs", "GET", "/api/v1/audit-logs"},
	{"/notifications", "GET", "/api/v1/notifications"},
	// 2026-10-10 菜单挂载收口：菜单码与页面主接口同码（严格匹配，不再依赖别名映射）。
	{"/service-catalog", "GET", "/api/v1/service-catalogs"},
	// 入口守卫比主接口读码更严的三例（菜单按「入口守卫码」挂载，见 menuRouteMismatchAllowlist）。
	{"/admin/teams", "GET", "/api/v1/org/teams"},
	{"/admin/system-config", "GET", "/api/v1/system-configs"},
	{"/admin/menus", "GET", "/api/v1/menus"},
}

// menuRouteMismatchAllowlist 已知未对齐配对（key=MenuPath），必须写明理由与去向。
// 两侧对齐后，本测试会失败并要求移除条目（防僵尸豁免）。
var menuRouteMismatchAllowlist = map[string]string{
	"/tickets/types": "菜单码 ticket_type:read 比接口 GET /api/v1/ticket-types（ticket:read）更严：" +
		"无「可见却 403」的 UX 破坏，但属口径漂移；待产品确认工单类型可见范围后统一（2026-10-10 登记）。",
	"/admin/teams": "菜单码=页面入口守卫 team:write，主接口读通道为 team:read：菜单更严 → 只读角色不挂载、" +
		"无「可见却 403」；待入口守卫与接口读码统一后移除（2026-10-10 登记）。",
	"/admin/system-config": "菜单码=页面入口守卫 system_config:write，主接口读通道为 system_config:read：菜单更严 → " +
		"只读角色不挂载；待入口守卫与接口读码统一后移除（2026-10-10 登记）。",
	"/admin/menus": "菜单码=页面入口守卫 system_config:write，主接口读通道为 system_config:read：菜单更严 → " +
		"只读角色不挂载；待入口守卫与接口读码统一后移除（2026-10-10 登记）。",
}

// TestMenuPermissionAlignsWithRouteDeclarations 守卫：配对页面的「菜单 permission_code」
// 必须与「页面主数据接口的 RequirePermission 声明」一致。
func TestMenuPermissionAlignsWithRouteDeclarations(t *testing.T) {
	menus := loadSeederMenuPermissions(t)

	routes, err := middleware.ScanDeclaredPermissionRoutes()
	if err != nil {
		t.Fatalf("扫描路由声明失败: %v", err)
	}
	routeIndex := make(map[string]middleware.DeclaredRoute, len(routes))
	for _, r := range routes {
		routeIndex[r.Method+" "+r.FullPath] = r
	}

	var missingMenu, missingRoute, mismatches, staleAllowlist []string
	for _, pair := range menuRouteAlignment {
		menuPerm, menuOK := menus[pair.MenuPath]
		route, routeOK := routeIndex[pair.Method+" "+pair.RoutePath]
		if !menuOK {
			missingMenu = append(missingMenu, fmt.Sprintf("%s（配对声明 %s %s）", pair.MenuPath, pair.Method, pair.RoutePath))
			continue
		}
		if !routeOK {
			missingRoute = append(missingRoute, fmt.Sprintf("%s %s（菜单 %s）", pair.Method, pair.RoutePath, pair.MenuPath))
			continue
		}

		routePerm := route.Resource + ":" + route.Action
		aligned := menuPerm == routePerm
		if _, allowlisted := menuRouteMismatchAllowlist[pair.MenuPath]; allowlisted {
			if aligned {
				staleAllowlist = append(staleAllowlist,
					fmt.Sprintf("%s: 菜单=%s 接口=%s 已对齐，请从 menuRouteMismatchAllowlist 移除", pair.MenuPath, menuPerm, routePerm))
			}
			continue
		}
		if !aligned {
			mismatches = append(mismatches,
				fmt.Sprintf("%s: 菜单=%s ≠ 接口(%s %s)=%s", pair.MenuPath, menuPerm, pair.Method, pair.RoutePath, routePerm))
		}
	}

	// allowlist 键必须存在于配对表（防僵尸条目）。
	pairPaths := make(map[string]bool, len(menuRouteAlignment))
	for _, pair := range menuRouteAlignment {
		pairPaths[pair.MenuPath] = true
	}
	for path := range menuRouteMismatchAllowlist {
		if !pairPaths[path] {
			staleAllowlist = append(staleAllowlist, path+": 不在 menuRouteAlignment 配对表中，请移除")
		}
	}

	sort.Strings(missingMenu)
	sort.Strings(missingRoute)
	sort.Strings(mismatches)
	sort.Strings(staleAllowlist)

	if len(missingMenu) > 0 {
		t.Errorf("配对表中的菜单在 seeder 规格中不存在（菜单改名/删除后请同步更新配对表）:\n  %s", strings.Join(missingMenu, "\n  "))
	}
	if len(missingRoute) > 0 {
		t.Errorf("配对表中的接口未找到路由声明（路由改路径/改方法后请同步更新配对表）:\n  %s", strings.Join(missingRoute, "\n  "))
	}
	if len(mismatches) > 0 {
		t.Errorf("菜单 permission_code 与页面主接口权限不一致（用户会「菜单可见 → 进页 403」或反之）:\n  %s\n\n"+
			"修复方式：统一到同一权限码（三层同码：菜单 / 前端路由守卫 / 后端接口）；\n"+
			"确属待处理欠账的，加入 menuRouteMismatchAllowlist 并写明理由与去向。", strings.Join(mismatches, "\n  "))
	}
	if len(staleAllowlist) > 0 {
		t.Errorf("menuRouteMismatchAllowlist 存在失效条目（已对齐或不在配对表）:\n  %s", strings.Join(staleAllowlist, "\n  "))
	}
}

// TestMenuSpecsWithoutPermissionCodeAreAllowlisted 守卫（2026-10-10 菜单挂载收口）：
// 无 permission_code 的菜单项 = 对全部登录用户可见（后端 filterMenusByPermission 无条件保留），
// 必须显式登记并写明理由；当前仅服务台 /dashboard。新增无码菜单需在此说明理由。
func TestMenuSpecsWithoutPermissionCodeAreAllowlisted(t *testing.T) {
	menus := loadSeederMenuPermissions(t)

	allowed := map[string]string{
		"/dashboard": "服务台为所有登录用户默认落地页（无业务数据权限要求）",
	}

	var undeclared []string
	for path, perm := range menus {
		if perm != "" {
			continue
		}
		if _, ok := allowed[path]; !ok {
			undeclared = append(undeclared, path)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("存在未登记的无 permission_code 菜单（对所有登录用户可见）：\n  %s\n\n"+
			"修复方式：为菜单补 permission_code，或在 allowed 白名单登记并写明理由。",
			strings.Join(undeclared, "\n  "))
	}

	// 白名单防腐化：登记的路径必须仍是无码菜单，且写明理由。
	for path, reason := range allowed {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("白名单 %s 缺少理由", path)
		}
		if perm, ok := menus[path]; !ok {
			t.Errorf("白名单 %s 未命中任何 seeder 菜单（改名/删除后请同步）", path)
		} else if perm != "" {
			t.Errorf("白名单 %s 已补 permission_code=%s，请从白名单移除", path, perm)
		}
	}
}

// loadSeederMenuPermissions AST 解析 pkg/seeder/seeder.go 的 []menuSpec 规格，
// 返回 path → permission_code。不复制数据，结构变化时解析量骤降会被防失效断言拦下。
func loadSeederMenuPermissions(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "..", "pkg", "seeder", "seeder.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", srcPath, err)
	}

	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		arr, ok := cl.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		elt, ok := arr.Elt.(*ast.Ident)
		if !ok || elt.Name != "menuSpec" {
			return true
		}
		for _, item := range cl.Elts {
			itemLit, ok := item.(*ast.CompositeLit)
			if !ok {
				continue
			}
			var path, perm string
			for _, field := range itemLit.Elts {
				kv, ok := field.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				lit, ok := kv.Value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				switch key.Name {
				case "Path":
					path = val
				case "PermissionCode":
					perm = val
				}
			}
			if path != "" {
				out[path] = perm
			}
		}
		return true
	})

	// 防扫描失效：seeder 当前有 100+ 菜单规格；远低于该量级说明解析锚点已漂移。
	if len(out) < 50 {
		t.Fatalf("menuSpec 解析疑似失效：仅解析出 %d 条菜单（seeder.go 结构可能已变，请更新本测试的解析锚点）", len(out))
	}
	return out
}
