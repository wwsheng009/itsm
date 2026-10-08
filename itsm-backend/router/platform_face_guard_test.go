package router

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// platform_face_guard_test.go：平台面路由 L1 守卫（IP-P2-7 收口补丁 6 / 设计文档 §5.2 R2）。
//
// 背景：平台治理面（LLM 实例管理、运维命令、RLS 指标、定时器、权限词表写面）除 L2 权限码外
// 必须叠加 L1 身份硬校验（middleware.RequirePlatformAdmin，补丁 4）。本守卫把该不变量固化为 CI 规则：
//  1. 注册表（platformFaceRoutes）：清单内每条路由必须存在且挂 L1（行内参数或分组级继承）；
//  2. 反向前缀（platformFacePathPrefixes）：直接注册在平台前缀下的路由必须挂 L1，
//     否则需进 platformFaceExemptions 并写明理由；
//  3. handler 级平台面（handlerLevelPlatformFaces）与豁免表防腐化：条目必须命中真实路由，理由不得为空。
//
// 扩展纪律：新增平台面路由时同步更新本注册表
// （见 docs/multi-tenant/plan/role-permission-extension-checklist.md §3）。

// routeKey 路由唯一键（file+recv 区分同文件内不同分组上的同名路由，如 router.go 的 GET ""）。
type routeKey struct {
	file   string
	recv   string
	method string
	path   string
}

// platformFaceRoutes 平台面路由注册表（与设计文档 §3.3 平台治理面清单对应）。
var platformFaceRoutes = map[routeKey]string{
	// —— LLM 实例管理（llm_provider 无租户字段，全局资源；分组级 L1 + system:write）——
	{"llm_provider_routes.go", "admin", "GET", "/providers"}:                "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "POST", "/providers"}:               "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "PUT", "/providers/:id"}:            "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "DELETE", "/providers/:id"}:         "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "POST", "/providers/:id/test"}:      "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "POST", "/providers/:id/default"}:   "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "POST", "/providers/import-static"}: "LLM 实例管理：全局资源",
	{"llm_provider_routes.go", "admin", "PUT", "/user-preference"}:          "LLM 实例管理：个人默认写入（平台面）",
	// —— 运维命令（分组级 L1 + system:write）——
	{"router.go", "operationRoutes", "GET", ""}:              "运维命令平台面",
	{"router.go", "operationRoutes", "GET", "/:id"}:          "运维命令平台面",
	{"router.go", "operationRoutes", "POST", "/:id/replay"}:  "运维命令平台面",
	{"router.go", "operationRoutes", "POST", "/:id/cancel"}:  "运维命令平台面",
	{"router.go", "operationRoutes", "POST", "/bulk-replay"}: "运维命令平台面",
	{"router.go", "operationRoutes", "POST", "/bulk-cancel"}: "运维命令平台面",
	// —— RLS 指标（行内 L1 + system:read）——
	{"router.go", "auth", "GET", "/admin/rls/stats"}: "RLS 指标平台运维面",
	// —— 定时器（分组级 L1 + system:read）——
	{"router.go", "timerRoutes", "GET", ""}:       "定时器平台调度面",
	{"router.go", "timerRoutes", "GET", "/stats"}: "定时器平台调度面",
	{"router.go", "timerRoutes", "GET", "/:id"}:   "定时器平台调度面",
	// —— 权限词表写面（行内 L1 + role:write；读面租户开放，不在本表）——
	{"common_system_routes.go", "permissions", "POST", ""}:      "权限词表写面：词表所有权",
	{"common_system_routes.go", "permissions", "POST", "/init"}: "权限词表写面：词表所有权",
}

// platformFaceExemptions 反向前缀检查豁免（直接注册在平台前缀下、但按设计不需中间件级 L1 的路由）。
// 键 = routeKey；值 = 理由（必填）。当前为空：租户治理写面为 handler 级 requirePlatformAdmin，
// 不在中间件层，故不落入"中间件前缀"扫描；见 handlerLevelPlatformFaces。
var platformFaceExemptions = map[routeKey]string{}

// platformFacePathPrefixes 平台面路径前缀（直接注册场景）：命中且未挂 L1 → CI 红。
var platformFacePathPrefixes = []string{
	"/admin/operations",
	"/admin/rls",
	"/timers",
	"/ai/providers",
	"/permissions",
}

// handlerLevelPlatformFaces 平台面中由 handler 级 requirePlatformAdmin 收口的写路由（不在中间件层）。
// 仅做存在性防腐化（handler 内部校验由 handlers/tenant 单测覆盖），防止"路由改名后无人复核"。
var handlerLevelPlatformFaces = map[routeKey]string{
	{"common_system_routes.go", "tenants", "POST", ""}:                     "租户治理写面：handler 级 requirePlatformAdmin（补丁 3）",
	{"common_system_routes.go", "tenants", "PUT", "/:id"}:                  "租户治理写面：handler 级 requirePlatformAdmin",
	{"common_system_routes.go", "tenants", "DELETE", "/:id"}:               "租户治理写面：handler 级 requirePlatformAdmin",
	{"common_system_routes.go", "tenants", "PUT", "/:id/status"}:           "租户治理写面：handler 级 requirePlatformAdmin",
	{"common_system_routes.go", "tenants", "POST", "/:id/provision"}:       "租户治理写面：handler 级 requirePlatformAdmin",
	{"common_system_routes.go", "tenants", "POST", "/:id/bootstrap-admin"}: "租户治理写面：handler 级 requirePlatformAdmin",
}

func TestPlatformFaceRoutesRequirePlatformAdmin(t *testing.T) {
	routes := scanRouteRegistrations(t)

	seen := make(map[routeKey]bool, len(routes))
	withL1 := make(map[routeKey]bool, len(routes))
	for _, r := range routes {
		k := routeKey{file: r.file, recv: r.recv, method: r.method, path: r.path}
		seen[k] = true
		if r.hasPlatformAdmin {
			withL1[k] = true
		}
	}

	var problems []string

	// ① 注册表：必须命中真实路由且挂 L1。
	for key, reason := range platformFaceRoutes {
		label := fmt.Sprintf("%s %s %s %s", key.file, key.recv, key.method, key.path)
		if strings.TrimSpace(reason) == "" {
			problems = append(problems, label+" —— 缺少注册理由")
			continue
		}
		if !seen[key] {
			problems = append(problems, label+" —— 未命中任何已注册路由（路由改名/删除后请同步注册表）")
			continue
		}
		if !withL1[key] {
			problems = append(problems, label+" —— 平台面路由未挂 RequirePlatformAdmin（行内或分组级）")
		}
	}

	// ② 反向前缀：直接注册在平台前缀下必须挂 L1（或显式豁免+理由）。
	for _, r := range routes {
		if !hasAnyPrefix(r.path, platformFacePathPrefixes) || r.hasPlatformAdmin {
			continue
		}
		k := routeKey{file: r.file, recv: r.recv, method: r.method, path: r.path}
		if reason, ok := platformFaceExemptions[k]; ok && strings.TrimSpace(reason) != "" {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s:%d %s %s —— 平台前缀路由未挂 L1（如属租户面请移出前缀，或加豁免并写明理由）",
			r.file, r.line, r.method, r.path))
	}

	// ③ 豁免表与 handler 级平台面防腐化：必须命中真实路由且写明理由。
	for key, reason := range platformFaceExemptions {
		label := fmt.Sprintf("%s %s %s %s", key.file, key.recv, key.method, key.path)
		if strings.TrimSpace(reason) == "" {
			problems = append(problems, label+" —— 缺少豁免理由")
		}
		if !seen[key] {
			problems = append(problems, label+" —— 未命中任何已注册路由（路由改名/删除后请同步豁免表）")
		}
	}
	for key, reason := range handlerLevelPlatformFaces {
		label := fmt.Sprintf("%s %s %s %s", key.file, key.recv, key.method, key.path)
		if strings.TrimSpace(reason) == "" {
			problems = append(problems, label+" —— 缺少登记理由")
		}
		if !seen[key] {
			problems = append(problems, label+" —— 未命中任何已注册路由（路由改名/删除后请同步登记表）")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("平台面 L1 守卫失败：\n  %s", strings.Join(problems, "\n  "))
	}
}

// hasAnyPrefix 判断路径是否命中任一大前缀。
func hasAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
