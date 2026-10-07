// Command membership-drift-check 运行 membership（展示/权限单源）与请求期事实
// （users.role/msp_role、msp_allocations、角色快照）的一致性巡检（M4 / MSW-A6）。
//
// 只读：仅查询 users / user_tenant_memberships / msp_allocations / roles，不写库。
// 退出码：FAIL>0 → 1；--strict 且 WARN>0 → 1；否则 0。
//
// 用法：
//
//	go run ./cmd/membership-drift-check            # 文本报告
//	go run ./cmd/membership-drift-check -json      # JSON（供 CI/巡检平台消费）
//	go run ./cmd/membership-drift-check -strict    # WARN 也阻断
//
// 分类与阈值定义见 internal/membershipcheck 包注释。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"itsm-backend/config"
	"itsm-backend/database"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/internal/membershipcheck"
)

func main() {
	strict := flag.Bool("strict", false, "WARN 也计入退出码（默认仅 FAIL 阻断）")
	asJSON := flag.Bool("json", false, "输出 JSON 报告")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	client, err := database.InitDatabase(&cfg.Database)
	if err != nil {
		log.Fatalf("ent client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	users, err := client.User.Query().All(ctx)
	if err != nil {
		log.Fatalf("query users: %v", err)
	}
	memberships, err := client.UserTenantMembership.Query().All(ctx)
	if err != nil {
		log.Fatalf("query memberships: %v", err)
	}
	allocations, err := client.MSPAllocation.Query().Where(mspallocation.DeassignedAtIsNil()).All(ctx)
	if err != nil {
		log.Fatalf("query active allocations: %v", err)
	}
	roles, err := client.Role.Query().All(ctx)
	if err != nil {
		log.Fatalf("query roles: %v", err)
	}
	tenants, err := client.Tenant.Query().All(ctx)
	if err != nil {
		log.Fatalf("query tenants: %v", err)
	}

	facts := membershipcheck.Facts{
		Users:       make([]membershipcheck.UserFact, 0, len(users)),
		Memberships: make([]membershipcheck.MembershipFact, 0, len(memberships)),
		Allocations: make([]membershipcheck.AllocationFact, 0, len(allocations)),
		Roles:       make([]membershipcheck.RoleFact, 0, len(roles)),
		Tenants:     make([]membershipcheck.TenantFact, 0, len(tenants)),
	}
	for _, u := range users {
		facts.Users = append(facts.Users, membershipcheck.UserFact{
			ID: u.ID, Username: u.Username, Active: u.Active,
			Role: string(u.Role), MSPRole: string(u.MspRole), TenantID: u.TenantID,
		})
	}
	for _, m := range memberships {
		mspRole := ""
		if m.MspRole != nil {
			mspRole = *m.MspRole
		}
		facts.Memberships = append(facts.Memberships, membershipcheck.MembershipFact{
			ID: m.ID, UserID: m.UserID, TenantID: m.TenantID,
			AccountKind: string(m.AccountKind), Source: string(m.Source),
			RoleID: m.RoleID, MSPRole: mspRole, Status: string(m.Status),
			IsDefault: m.IsDefault, DeletedAt: m.DeletedAt != nil,
		})
	}
	for _, a := range allocations {
		facts.Allocations = append(facts.Allocations, membershipcheck.AllocationFact{
			MSPUserID: a.MspUserID, CustomerTenantID: a.CustomerTenantID, ProviderTenantID: a.ProviderTenantID,
		})
	}
	for _, r := range roles {
		facts.Roles = append(facts.Roles, membershipcheck.RoleFact{ID: r.ID, TenantID: r.TenantID, Code: r.Code})
	}
	for _, t := range tenants {
		facts.Tenants = append(facts.Tenants, membershipcheck.TenantFact{ID: t.ID, Type: string(t.Type)})
	}

	findings := membershipcheck.Check(facts)
	fail, warn := membershipcheck.Summary(findings)

	if *asJSON {
		report := map[string]any{
			"generatedAt": time.Now().Format(time.RFC3339),
			"scope": map[string]int{
				"users": len(facts.Users), "memberships": len(facts.Memberships),
				"activeAllocations": len(facts.Allocations), "roles": len(facts.Roles), "tenants": len(facts.Tenants),
			},
			"counts":   map[string]int{"fail": fail, "warn": warn},
			"findings": findings,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatalf("encode report: %v", err)
		}
	} else {
		fmt.Printf("membership drift check (read-only) — %s\n", time.Now().Format(time.RFC3339))
		fmt.Printf("scope: users=%d memberships=%d active_allocations=%d roles=%d tenants=%d\n",
			len(facts.Users), len(facts.Memberships), len(facts.Allocations), len(facts.Roles), len(facts.Tenants))
		fmt.Printf("FAIL=%d WARN=%d\n", fail, warn)
		for _, f := range findings {
			fmt.Printf("[%s] %-24s user=%-4d tenant=%-4d %s\n", f.Severity, f.Category, f.UserID, f.TenantID, f.Detail)
		}
		if fail == 0 && warn == 0 {
			fmt.Println("summary: 0 drift（种子/生产数据无漂移）")
		} else {
			fmt.Printf("summary: FAIL=%d WARN=%d（阈值：FAIL>0 阻断；WARN 默认仅观察，--strict 时阻断）\n", fail, warn)
		}
	}

	if fail > 0 || (*strict && warn > 0) {
		os.Exit(1)
	}
}
