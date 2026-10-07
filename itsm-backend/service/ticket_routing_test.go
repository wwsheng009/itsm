package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var ticketRoutingDBCounter int64

// TestTicketCreate_TypeLevelMSPRouting 锁定 P0-d 类型级自动路由：
// 类型开启 msp_route_enabled + 客户归属有效 → 快照 + msp_routed_at；
// 类型未开启 / 非托管租户 → 不写路由标记。
func TestTicketCreate_TypeLevelMSPRouting(t *testing.T) {
	dsn := fmt.Sprintf("file:ticket_routing_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&ticketRoutingDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	svc := NewTicketServiceForTest(client, logger)

	mkTenant := func(name, code, typ string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(entTenant.Type(typ)).SetStatus("active")
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	mkUser := func(name string, tenantID int) *ent.User {
		u, err := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetTenantID(tenantID).SetActive(true).Save(ctx)
		require.NoError(t, err)
		return u
	}
	mkType := func(code string, tenantID int, route bool) *ent.TicketType {
		tt, err := client.TicketType.Create().
			SetCode(code).SetName(code).SetDescription("").SetIcon("").SetColor("").
			SetStatus("active").SetDefaultPriority("medium").
			SetTenantID(int64(tenantID)).SetCreatedBy(1).
			SetCreatedAt(time.Now()).SetUpdatedAt(time.Now()).
			SetCustomFields(map[string]interface{}{}).
			SetApprovalChain([]interface{}{}).
			SetAssignmentRules([]interface{}{}).
			SetNotificationConfig(map[string]interface{}{}).
			SetPermissionConfig(map[string]interface{}{}).
			SetMspRouteEnabled(route).Save(ctx)
		require.NoError(t, err)
		return tt
	}

	provider := mkTenant("Provider", "rt-prov", "msp_provider", 0)
	customer := mkTenant("Customer", "rt-cust", "msp_customer", provider.ID)
	standalone := mkTenant("Standalone", "rt-standalone", "internal", 0)
	custUser := mkUser("rt-cust-user", customer.ID)
	saUser := mkUser("rt-sa-user", standalone.ID)

	routedType := mkType("rt-routed", customer.ID, true)
	plainType := mkType("rt-plain", customer.ID, false)
	saRoutedType := mkType("rt-sa-routed", standalone.ID, true)

	// 1) 托管客户 + 类型开启路由 → 快照 + 路由标记。
	t1, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title: "routed ticket", Description: "desc", Priority: "medium",
		RequesterID: custUser.ID, TicketTypeID: &routedType.ID,
	}, customer.ID)
	require.NoError(t, err)
	assert.True(t, t1.IsManagedByMSP)
	require.NotNil(t, t1.MSPProviderID)
	assert.Equal(t, provider.ID, *t1.MSPProviderID)
	require.NotNil(t, t1.MSPRoutedAt, "类型开启路由 → 必须标记 msp_routed_at")

	// 2) 托管客户 + 类型未开启 → 快照仍派生，但不标记路由。
	t2, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title: "plain ticket", Description: "desc", Priority: "medium",
		RequesterID: custUser.ID, TicketTypeID: &plainType.ID,
	}, customer.ID)
	require.NoError(t, err)
	assert.True(t, t2.IsManagedByMSP)
	assert.Nil(t, t2.MSPRoutedAt, "类型未开启路由 → 不得标记")

	// 3) 非托管租户 + 类型开启 → 无归属，不标记。
	t3, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title: "standalone ticket", Description: "desc", Priority: "medium",
		RequesterID: saUser.ID, TicketTypeID: &saRoutedType.ID,
	}, standalone.ID)
	require.NoError(t, err)
	assert.False(t, t3.IsManagedByMSP)
	assert.Nil(t, t3.MSPProviderID)
	assert.Nil(t, t3.MSPRoutedAt)
}

// TestWorkbenchListRoutedOnly 锁定工作台 routedOnly 过滤：
// 默认（false）返回全部已分配客户条目；routedOnly=true 仅返回 msp_routed_at 非空条目。
func TestWorkbenchListRoutedOnly(t *testing.T) {
	env := newWorkbenchEnv(t)
	ctx := context.Background()
	_, err := env.client.Ticket.UpdateOneID(env.tA.ID).SetMspRoutedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	all, err := env.svc.ListTickets(ctx, env.actor, dto.WorkbenchTicketQuery{})
	require.NoError(t, err)
	assert.Len(t, all.Items, 3, "默认不过滤")

	routed, err := env.svc.ListTickets(ctx, env.actor, dto.WorkbenchTicketQuery{RoutedOnly: true})
	require.NoError(t, err)
	require.Len(t, routed.Items, 1)
	assert.Equal(t, env.tA.ID, routed.Items[0].ID)
}
