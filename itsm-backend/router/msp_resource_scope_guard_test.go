package router

import (
	"os"
	"regexp"
	"testing"

	"itsm-backend/service"
)

// TestMSPRouteResourcesClassified 锁定 router/msp_routes.go 的 RequireMSPPermission(resource, …)
// 资源族必须已在 service 的 MSP 资源分类注册表中登记（跨租户数据面 / 区域可见性两类）。
//
// 背景（2026-10-13 客户可见性缺陷治本）：跨租户 MSP 数据面资源若未登记，
// 客户租户克隆会照常下发其授权（fail-open：前端入口可见、后端必然 403），
// 且存量对账剥离也无从判定。新增 msp_* 资源时本守卫先红，强制完成分类评估与登记：
//
//	service.mspCrossTenantPermissionResources —— 跨租户数据面（客户租户不下发）
//	service.mspAreaPermissionResources       —— 区域可见性（可下发，仅导航/入口）
func TestMSPRouteResourcesClassified(t *testing.T) {
	src, err := os.ReadFile("msp_routes.go")
	if err != nil {
		t.Fatalf("read msp_routes.go: %v", err)
	}
	re := regexp.MustCompile(`RequireMSPPermission\("([a-z_]+)"`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("msp_routes.go 未匹配到 RequireMSPPermission 调用，守卫失效（正则或文件结构变更）")
	}
	for _, m := range matches {
		resource := m[1]
		if service.IsMSPCrossTenantPermissionResource(resource) || service.IsMSPAreaPermissionResource(resource) {
			continue
		}
		t.Errorf("MSP 路由资源 %q 未分类：请在 service.mspCrossTenantPermissionResources"+
			"（跨租户数据面，客户租户不下发）或 mspAreaPermissionResources（区域可见性）中登记，并同步评估克隆/校验/前端守卫", resource)
	}
}
