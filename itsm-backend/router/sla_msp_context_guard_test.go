package router

import (
	"os"
	"regexp"
	"testing"
)

// TestSLAMSPGroupsInstallMSPContext 守卫（2026-10-14 验收修复回归）：
// /sla 路由族的 MSP 子组（grants/contracts/settlement-report）使用
// RequireMSPPermission，其前置 MSPContext 必须由 MSPMiddleware 装配。
// /sla 族不经过 /api/v1/msp 的 MSPMiddleware；遗漏时合同/授权/结算端点
// 对所有 MSP 用户恒 403「仅MSP员工可访问」（P1-2/P2-2 接线缺陷，验收发现）。
func TestSLAMSPGroupsInstallMSPContext(t *testing.T) {
	src, err := os.ReadFile("sla_routes.go")
	if err != nil {
		t.Fatalf("read sla_routes.go: %v", err)
	}
	s := string(src)
	if !regexp.MustCompile(`RequireMSPPermission\(`).MatchString(s) {
		t.Fatal("sla_routes.go 未匹配到 RequireMSPPermission，守卫失效（正则或文件结构变更）")
	}
	for _, group := range []string{"grants", "contracts", "settlement-report"} {
		re := regexp.MustCompile(`Group\("/` + regexp.QuoteMeta(group) + `",[\s\S]{0,300}?MSPMiddleware\(`)
		if !re.MatchString(s) {
			t.Errorf("MSP 子组 /%s 缺少 MSPMiddleware 装配（MSPContext 缺省 → RequireMSPPermission 恒 403）", group)
		}
	}
}
