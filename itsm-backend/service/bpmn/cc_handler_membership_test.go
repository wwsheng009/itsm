package bpmn

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/ticketcc"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestCCTaskHandlerValidatesRecipientMembership 锁定 BPMN CC service task 的接收人边界：
// provider 客户租户身份可写入并落 membership；无 membership 的外部账号必须 fail-closed。
func TestCCTaskHandlerValidatesRecipientMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:bpmn-cc-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	customerTenant := client.Tenant.Create().SetCode("bpmn-cc-customer").SetName("bpmn-cc-customer").SaveX(ctx)
	providerTenant := client.Tenant.Create().SetCode("bpmn-cc-provider").SetName("bpmn-cc-provider").SaveX(ctx)
	foreignTenant := client.Tenant.Create().SetCode("bpmn-cc-foreign").SetName("bpmn-cc-foreign").SaveX(ctx)

	requester := client.User.Create().
		SetUsername("bpmn-cc-requester").SetEmail("bpmn-cc-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(customerTenant.ID).SetActive(true).SaveX(ctx)
	provider := client.User.Create().
		SetUsername("bpmn-cc-provider-tech").SetEmail("bpmn-cc-provider-tech@example.com").SetName("provider").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).SaveX(ctx)
	foreign := client.User.Create().
		SetUsername("bpmn-cc-foreign-user").SetEmail("bpmn-cc-foreign-user@example.com").SetName("foreign").
		SetPasswordHash("hash").SetTenantID(foreignTenant.ID).SetActive(true).SaveX(ctx)

	client.UserTenantMembership.Create().
		SetUserID(requester.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	providerMembership := client.UserTenantMembership.Create().
		SetUserID(provider.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetMspRole("msp_tech").
		SetStatus(usertenantmembership.StatusActive).SaveX(ctx)

	tkt := client.Ticket.Create().
		SetTicketNumber("TKT-BPMN-CC-MB-1").
		SetTitle("bpmn cc membership").
		SetStatus("open").
		SetRequesterID(requester.ID).
		SetTenantID(customerTenant.ID).SaveX(ctx)

	handler := NewCCTaskHandler(client, zaptest.NewLogger(t).Sugar())
	_, err := handler.Execute(ctx, nil, map[string]interface{}{
		"ticket_id": tkt.ID,
		"ccType":    "user",
		"ccUserIds": strconv.Itoa(provider.ID),
		"addedBy":   requester.ID,
		"tenant_id": customerTenant.ID,
		"ccNotify":  false,
	})
	require.NoError(t, err)

	row, err := client.TicketCC.Query().
		Where(ticketcc.TicketID(tkt.ID), ticketcc.UserID(provider.ID), ticketcc.TenantID(customerTenant.ID)).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, providerMembership.ID, row.UserMembershipID)
	assert.NotZero(t, row.AddedByMembershipID)

	_, err = handler.Execute(ctx, nil, map[string]interface{}{
		"ticket_id": tkt.ID,
		"ccType":    "user",
		"ccUserIds": strconv.Itoa(foreign.ID),
		"addedBy":   requester.ID,
		"tenant_id": customerTenant.ID,
		"ccNotify":  false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不属于当前租户")

	count, err := client.TicketCC.Query().
		Where(ticketcc.TicketID(tkt.ID), ticketcc.UserID(foreign.ID)).
		Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count)
}
