package multi_tenant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	changeHandler "itsm-backend/handlers/change"
	"itsm-backend/handlers/common/membershipref"
	problemHandler "itsm-backend/handlers/problem"
	srHandler "itsm-backend/handlers/service_request"

	_ "github.com/mattn/go-sqlite3"
)

// TestBusinessMembershipIsolation 覆盖 C 方案 Phase 4 第二批的跨租户矩阵：
//   - (user, tenant) membership 解析拒绝跨租户混用；
//   - 问题/变更/服务请求的读路径按 tenant_id 隔离；
//   - 写路径不能把行或 membership 引用写到其他租户；
//   - 业务行写入失败时原行保持不变。
func TestBusinessMembershipIsolation(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:business_membership_isolation?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("iso-a").SetName("isolation A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("iso-b").SetName("isolation B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("iso-user-a").SetEmail("iso-a@example.com").SetName("user A").
		SetPasswordHash("hash").SetTenantID(tenantA.ID).SetActive(true).SaveX(ctx)
	userB := client.User.Create().
		SetUsername("iso-user-b").SetEmail("iso-b@example.com").SetName("user B").
		SetPasswordHash("hash").SetTenantID(tenantB.ID).SetActive(true).SaveX(ctx)
	membershipA := client.UserTenantMembership.Create().
		SetUserID(userA.ID).SetTenantID(tenantA.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	membershipB := client.UserTenantMembership.Create().
		SetUserID(userB.ID).SetTenantID(tenantB.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)

	// 1) membership 解析：跨租户混用必须 fail-closed。
	_, err := membershipref.ResolveActor(ctx, client, userA.ID, tenantB.ID, membershipA.ID)
	require.Error(t, err, "membership A 不能用于 tenant B")

	_, err = membershipref.ResolveAssignable(ctx, client, userB.ID, tenantA.ID)
	require.Error(t, err, "tenant B 用户不能被分配为 tenant A 处理人")

	// 2) 问题单。
	problemRepo := problemHandler.NewEntRepository(client)
	problemCreated, err := problemRepo.Create(ctx, &problemHandler.Problem{
		Title:                 "isolation problem",
		Priority:              "medium",
		Status:                "open",
		CreatedBy:             userA.ID,
		CreatedByMembershipID: membershipA.ID,
		TenantID:              tenantA.ID,
	})
	require.NoError(t, err)
	_, err = problemRepo.Get(ctx, problemCreated.ID, tenantB.ID)
	require.Error(t, err, "tenant B 不能读取 tenant A 问题单")

	problemCreated.Title = "mutated by tenant B"
	problemCreated.TenantID = tenantB.ID
	_, err = problemRepo.Update(ctx, problemCreated)
	require.Error(t, err, "tenant B 不能更新 tenant A 问题单")
	problemStored, err := problemRepo.Get(ctx, problemCreated.ID, tenantA.ID)
	require.NoError(t, err)
	require.Equal(t, "isolation problem", problemStored.Title)

	_, err = problemRepo.Create(ctx, &problemHandler.Problem{
		Title:                 "forged membership",
		Priority:              "medium",
		Status:                "open",
		CreatedBy:             userA.ID,
		CreatedByMembershipID: membershipB.ID,
		TenantID:              tenantA.ID,
	})
	require.Error(t, err, "tenant B membership 不能写入 tenant A 问题单")

	// 3) 变更单。
	changeRepo := changeHandler.NewEntRepository(client, nil)
	changeCreated, err := changeRepo.Create(ctx, &changeHandler.Change{
		Title:                 "isolation change",
		Type:                  "normal",
		Status:                "draft",
		Priority:              "medium",
		ImpactScope:           "medium",
		RiskLevel:             "medium",
		CreatedBy:             userA.ID,
		CreatedByMembershipID: membershipA.ID,
		TenantID:              tenantA.ID,
	})
	require.NoError(t, err)
	_, err = changeRepo.Get(ctx, changeCreated.ID, tenantB.ID)
	require.Error(t, err, "tenant B 不能读取 tenant A 变更单")

	changeCreated.Title = "mutated by tenant B"
	changeCreated.TenantID = tenantB.ID
	_, err = changeRepo.Update(ctx, changeCreated)
	require.Error(t, err, "tenant B 不能更新 tenant A 变更单")
	changeStored, err := changeRepo.Get(ctx, changeCreated.ID, tenantA.ID)
	require.NoError(t, err)
	require.Equal(t, "isolation change", changeStored.Title)

	_, err = changeRepo.Create(ctx, &changeHandler.Change{
		Title:                 "forged membership",
		Type:                  "normal",
		Status:                "draft",
		Priority:              "medium",
		ImpactScope:           "medium",
		RiskLevel:             "medium",
		CreatedBy:             userA.ID,
		CreatedByMembershipID: membershipB.ID,
		TenantID:              tenantA.ID,
	})
	require.Error(t, err, "tenant B membership 不能写入 tenant A 变更单")

	// 4) 服务请求。
	srRepo := srHandler.NewEntRepository(client)
	srCreated, err := srRepo.Create(ctx, &srHandler.ServiceRequest{
		TenantID:              tenantA.ID,
		CatalogID:             1,
		RequesterID:           userA.ID,
		RequesterMembershipID: membershipA.ID,
		Status:                srHandler.SRStatusSubmitted,
		CurrentLevel:          1,
		TotalLevels:           3,
		DataClassification:    "internal",
	}, nil)
	require.NoError(t, err)
	_, err = srRepo.Get(ctx, srCreated.ID, tenantB.ID)
	require.Error(t, err, "tenant B 不能读取 tenant A 服务请求")

	srCreated.Title = "mutated by tenant B"
	srCreated.TenantID = tenantB.ID
	_ = srRepo.Update(ctx, srCreated) // 0 行更新不报错，但不得改动原行
	srStored, err := srRepo.Get(ctx, srCreated.ID, tenantA.ID)
	require.NoError(t, err)
	require.NotEqual(t, "mutated by tenant B", srStored.Title)

	_, err = srRepo.Create(ctx, &srHandler.ServiceRequest{
		TenantID:              tenantA.ID,
		CatalogID:             1,
		RequesterID:           userA.ID,
		RequesterMembershipID: membershipB.ID,
		Status:                srHandler.SRStatusSubmitted,
		CurrentLevel:          1,
		TotalLevels:           3,
		DataClassification:    "internal",
	}, nil)
	require.Error(t, err, "tenant B membership 不能写入 tenant A 服务请求")
}
