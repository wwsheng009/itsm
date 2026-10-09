package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"
	"itsm-backend/middleware"
)

// 跨租户单据流转 P2-③ / IP-P2-12：变更/问题/服务请求读面投影测试。

var recordProjDBCounter int64

type recordProjEnv struct {
	client   *ent.Client
	actor    MSPWorkbenchActor
	provider *ent.Tenant
	custA    *ent.Tenant
	custB    *ent.Tenant
	custC    *ent.Tenant
	userA    *ent.User
	userP    *ent.User
	base     time.Time
}

func newRecordProjEnv(t *testing.T) *recordProjEnv {
	t.Helper()
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeMerge
	t.Cleanup(func() { middleware.PermissionConfig.Mode = prevMode })

	dsn := fmt.Sprintf("file:recordproj_test_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&recordProjDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()

	provider, err := client.Tenant.Create().
		SetCode("rec-prov").SetName("服务商").SetType(entTenant.Type("msp_provider")).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	custA, err := client.Tenant.Create().
		SetCode("rec-a").SetName("客户A").SetType(entTenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	custB, err := client.Tenant.Create().
		SetCode("rec-b").SetName("客户B").SetType(entTenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	custC, err := client.Tenant.Create().
		SetCode("rec-c").SetName("客户C").SetType(entTenant.Type("msp_customer")).SetStatus("active").
		SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)

	userP, err := client.User.Create().
		SetUsername("rec-provider").SetEmail("rec-provider@example.com").SetName("服务商员工").
		SetPasswordHash("hash").SetTenantID(provider.ID).SetMspRole("provider_admin").SetRole("admin").Save(ctx)
	require.NoError(t, err)
	userA, err := client.User.Create().
		SetUsername("rec-a-user").SetEmail("rec-a@example.com").SetName("客户A员工").
		SetPasswordHash("hash").SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)

	// 目标租户 RBAC（C2 同构最小子集）：问题状态推进门禁 problem:write（IP-P2-14 起按目标租户判定）。
	seedTenantRolePermissions(t, client, custA.ID, "msp_manager", "problem:write")
	middleware.InvalidateAllPermissionCaches()
	t.Cleanup(middleware.InvalidateAllPermissionCaches)

	base := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	// 变更：A 两条（时间序 1/2），B 一条（时间序 3）。
	for i, spec := range []struct {
		tenant int
		number string
		status string
		title  string
		at     time.Time
	}{
		{custA.ID, "CHG-1", "submitted", "A 变更一", base.Add(1 * time.Hour)},
		{custA.ID, "CHG-2", "approved", "A 变更二", base.Add(2 * time.Hour)},
		{custB.ID, "CHG-3", "draft", "B 变更三", base.Add(3 * time.Hour)},
		{custC.ID, "CHG-4", "draft", "C 变更四", base.Add(4 * time.Hour)},
	} {
		_, err := client.Change.Create().
			SetChangeNumber(spec.number).SetTitle(spec.title).SetStatus(spec.status).
			SetPriority("medium").SetDescription("描述-" + spec.number).
			SetCreatedBy(userA.ID).SetTenantID(spec.tenant).
			SetCreatedAt(spec.at).SetUpdatedAt(spec.at).Save(ctx)
		require.NoError(t, err, "change %d", i)
	}
	// 问题：A 一条（指派给客户 A 员工）。
	_, err = client.Problem.Create().
		SetProblemNumber("PRB-1").SetTitle("A 问题一").SetStatus("investigating").
		SetPriority("high").SetDescription("问题描述").SetRootCause("根因").
		SetAssigneeID(userA.ID).SetCreatedBy(userA.ID).SetTenantID(custA.ID).
		SetCreatedAt(base.Add(30 * time.Minute)).SetUpdatedAt(base.Add(30 * time.Minute)).Save(ctx)
	require.NoError(t, err)
	// 服务请求：A 一条（申请人=客户 A 员工；处理人=服务商员工）。
	_, err = client.ServiceRequest.Create().
		SetTenantID(custA.ID).SetCatalogID(1).SetRequesterID(userA.ID).
		SetTitle("A 服务请求一").SetStatus("approved").SetReason("申请原因").
		SetCurrentLevel(2).SetTotalLevels(2).SetCurrentApprover("审批人").SetProcessorID(userP.ID).
		SetCreatedAt(base.Add(10 * time.Minute)).SetUpdatedAt(base.Add(90 * time.Minute)).Save(ctx)
	require.NoError(t, err)

	return &recordProjEnv{
		client: client,
		actor: MSPWorkbenchActor{
			UserID: userP.ID, Username: "rec-provider", HomeTenantID: provider.ID,
			MSPRole: "provider_admin", AllowedCustomers: []int{custA.ID, custB.ID},
		},
		provider: provider, custA: custA, custB: custB, custC: custC,
		userA: userA, userP: userP, base: base,
	}
}

func (e *recordProjEnv) svc() *MSPWorkbenchService {
	return NewMSPWorkbenchService(e.client, nil, nil, nil)
}

func TestMSPRecordProjection_ListChangesMergedSortedAndFiltered(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()

	// 全量：允许集合 = A+B（C 不可见），按 updated_at 降序合并。
	resp, err := svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Total)
	require.Len(t, resp.Items, 3)
	assert.Equal(t, "CHG-3", resp.Items[0].Number)
	assert.Equal(t, "CHG-2", resp.Items[1].Number)
	assert.Equal(t, "CHG-1", resp.Items[2].Number)
	assert.Equal(t, env.custB.ID, resp.Items[0].CustomerTenantID)
	assert.Equal(t, "客户B", resp.Items[0].CustomerName)
	assert.Equal(t, MSPRecordChange, resp.Items[0].RecordType)
	require.Len(t, resp.Items[0].AllowedActions, 1)
	assert.Equal(t, "view", resp.Items[0].AllowedActions[0].Action)
	assert.True(t, resp.Items[0].AllowedActions[0].Allowed)

	// 状态筛选。
	resp, err = svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{Status: "approved"})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	assert.Equal(t, "CHG-2", resp.Items[0].Number)

	// 关键字（编号）。
	resp, err = svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{Q: "chg-1"})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	assert.Equal(t, "CHG-1", resp.Items[0].Number)

	// 逐租户游标分页：limit=1 逐页取完，无重复、无遗漏。
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err := svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{Limit: 1, Cursor: cursor})
		require.NoError(t, err)
		pages++
		require.LessOrEqual(t, pages, 6, "分页不得死循环")
		for _, it := range page.Items {
			require.False(t, seen[it.Number], "重复返回: %s", it.Number)
			seen[it.Number] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	assert.Equal(t, map[string]bool{"CHG-1": true, "CHG-2": true, "CHG-3": true}, seen)

	// 显式筛选单租户。
	resp, err = svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{CustomerTenantIDs: []int{env.custA.ID}})
	require.NoError(t, err)
	require.Equal(t, 2, resp.Total)
}

func TestMSPRecordProjection_ListProblemsAndServiceRequests(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()

	resp, err := svc.ListRecords(context.Background(), env.actor, MSPRecordProblem, dto.MSPRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	assert.Equal(t, "PRB-1", resp.Items[0].Number)
	assert.Equal(t, "high", resp.Items[0].Priority)
	assert.Equal(t, env.userA.ID, resp.Items[0].AssigneeID)
	assert.Equal(t, "客户A员工", resp.Items[0].AssigneeName)

	resp, err = svc.ListRecords(context.Background(), env.actor, MSPRecordServiceRequest, dto.MSPRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	item := resp.Items[0]
	assert.Contains(t, item.Number, "SR-")
	assert.Equal(t, "approved", item.Status)
	assert.Equal(t, env.userA.ID, item.RequesterID)
	assert.Equal(t, "客户A员工", item.RequesterName)
	assert.Equal(t, env.userP.ID, item.AssigneeID)
	assert.Equal(t, "服务商员工", item.AssigneeName, "provider 侧处理人经家租户作用域解析")

	// 未知类型 fail-closed。
	_, err = svc.ListRecords(context.Background(), env.actor, "ticket", dto.MSPRecordQuery{})
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
}

func TestMSPRecordProjection_ScopeDeniedAudited(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()

	_, err := svc.ListRecords(context.Background(), env.actor, MSPRecordChange, dto.MSPRecordQuery{CustomerTenantIDs: []int{env.custC.ID}})
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)

	rows, err := env.client.AuditLog.Query().
		Where(auditlog.ActionEQ("tenant.scope_denied"), auditlog.TenantIDEQ(env.provider.ID)).
		All(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1, "越界请求必须落审计")
}

func TestMSPRecordProjection_Detail(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	ctx := context.Background()

	changeID, err := env.client.Change.Query().Where().Order(ent.Asc("id")).First(ctx)
	require.NoError(t, err)

	resp, err := svc.GetRecord(ctx, env.actor, MSPRecordChange, changeID.ID, env.custA.ID)
	require.NoError(t, err)
	assert.Equal(t, "CHG-1", resp.Number)
	assert.Equal(t, "描述-CHG-1", resp.Description)
	assert.Equal(t, env.custA.ID, resp.CustomerTenantID)
	assert.Equal(t, "客户A", resp.CustomerName)
	require.Len(t, resp.AllowedActions, 1)
	assert.True(t, resp.AllowedActions[0].Allowed)

	// 未知 id → 404 口径。
	_, err = svc.GetRecord(ctx, env.actor, MSPRecordChange, 999999, env.custA.ID)
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerTenantNotFound, ae.Code)

	// 声明租户与资源租户不一致 → 400 口径。
	_, err = svc.GetRecord(ctx, env.actor, MSPRecordChange, changeID.ID, env.custB.ID)
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeResourceTenantMismatch, ae.Code)

	// 未分配客户（C 的记录存在，但不在允许集合）→ 403 口径。
	cChange, err := env.client.Change.Query().Where().Order(ent.Desc("id")).First(ctx)
	require.NoError(t, err)
	require.Equal(t, env.custC.ID, cChange.TenantID)
	_, err = svc.GetRecord(ctx, env.actor, MSPRecordChange, cChange.ID, env.custC.ID)
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)

	// 问题详情。
	problem, err := env.client.Problem.Query().Only(ctx)
	require.NoError(t, err)
	pResp, err := svc.GetRecord(ctx, env.actor, MSPRecordProblem, problem.ID, env.custA.ID)
	require.NoError(t, err)
	assert.Equal(t, "PRB-1", pResp.Number)
	assert.Equal(t, "根因", pResp.RootCause)
	assert.Equal(t, "客户A员工", pResp.AssigneeName)

	// 服务请求详情。
	sr, err := env.client.ServiceRequest.Query().Only(ctx)
	require.NoError(t, err)
	sResp, err := svc.GetRecord(ctx, env.actor, MSPRecordServiceRequest, sr.ID, env.custA.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, sResp.CurrentLevel)
	assert.Equal(t, 2, sResp.TotalLevels)
	assert.Equal(t, "审批人", sResp.CurrentApprover)
	assert.Equal(t, "申请原因", sResp.Description)
	assert.Equal(t, "服务商员工", sResp.AssigneeName)
	assert.Equal(t, "客户A员工", sResp.RequesterName)

	// 软删服务请求不投影（列表 + 详情都不可见）。
	require.NoError(t, env.client.ServiceRequest.UpdateOneID(sr.ID).SetDeletedAt(time.Now()).Exec(ctx))
	respSR, err := svc.ListRecords(ctx, env.actor, MSPRecordServiceRequest, dto.MSPRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, 0, respSR.Total)
	_, err = svc.GetRecord(ctx, env.actor, MSPRecordServiceRequest, sr.ID, env.custA.ID)
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerTenantNotFound, ae.Code)
}

func TestMSPRecordProjection_InactiveTenantReadable(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	ctx := context.Background()

	require.NoError(t, env.client.Tenant.UpdateOneID(env.custB.ID).SetStatus("suspended").Exec(ctx))
	resp, err := svc.ListRecords(ctx, env.actor, MSPRecordChange, dto.MSPRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Total, "暂停客户仍可读（只读面）")
	assert.True(t, resp.Items[0].AllowedActions[0].Allowed)

	// 绑定 RLS GUC 的调用方式不受影响（逐租户 ctx 重绑定）。
	_ = tenantctx.WithTenantID(ctx, env.custB.ID)
}
