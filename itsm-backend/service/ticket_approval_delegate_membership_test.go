package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/ticketapproval"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestApproveTicketDelegateValidatesMembership 锁定审批委派的跨租户边界：
// provider 凭客户租户 active membership 可接任审批；无 membership 的外部账号必须拒绝。
func TestApproveTicketDelegateValidatesMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-approval-delegate-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	customerTenant := client.Tenant.Create().SetCode("delegate-customer").SetName("delegate-customer").SaveX(ctx)
	providerTenant := client.Tenant.Create().SetCode("delegate-provider").SetName("delegate-provider").SaveX(ctx)
	foreignTenant := client.Tenant.Create().SetCode("delegate-foreign").SetName("delegate-foreign").SaveX(ctx)

	requester := client.User.Create().
		SetUsername("delegate-requester").SetEmail("delegate-requester@example.com").SetName("requester").
		SetPasswordHash("hash").SetTenantID(customerTenant.ID).SetActive(true).SaveX(ctx)
	approver := client.User.Create().
		SetUsername("delegate-approver").SetEmail("delegate-approver@example.com").SetName("approver").
		SetPasswordHash("hash").SetTenantID(customerTenant.ID).SetActive(true).SaveX(ctx)
	provider := client.User.Create().
		SetUsername("delegate-provider-tech").SetEmail("delegate-provider-tech@example.com").SetName("provider").
		SetPasswordHash("hash").SetTenantID(providerTenant.ID).SetActive(true).SaveX(ctx)
	foreign := client.User.Create().
		SetUsername("delegate-foreign-user").SetEmail("delegate-foreign-user@example.com").SetName("foreign").
		SetPasswordHash("hash").SetTenantID(foreignTenant.ID).SetActive(true).SaveX(ctx)

	client.UserTenantMembership.Create().
		SetUserID(requester.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	client.UserTenantMembership.Create().
		SetUserID(approver.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	providerMembership := client.UserTenantMembership.Create().
		SetUserID(provider.ID).SetTenantID(customerTenant.ID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetMspRole("msp_tech").
		SetStatus(usertenantmembership.StatusActive).SaveX(ctx)

	createTicketAndApproval := func(number string, assigneeID int) (int, int) {
		tk := client.Ticket.Create().
			SetTicketNumber(number).
			SetTitle("delegate membership").
			SetStatus("pending").
			SetPriority("medium").
			SetRequesterID(requester.ID).
			SetTenantID(customerTenant.ID).SaveX(ctx)
		approval := client.TicketApproval.Create().
			SetTicketID(tk.ID).
			SetLevel(1).
			SetLevelName("一级审批").
			SetApproverID(assigneeID).
			SetStatus(string(dto.ApprovalStatusPending)).
			SetTenantID(customerTenant.ID).SaveX(ctx)
		return tk.ID, approval.ID
	}

	svc := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
	ticketID, approvalID := createTicketAndApproval("TKT-DELEGATE-MB-1", approver.ID)

	require.NoError(t, svc.ApproveTicket(ctx, &dto.ApproveTicketRequest{
		TicketID:         ticketID,
		ApprovalID:       approvalID,
		Action:           "delegate",
		Comment:          "请代批",
		DelegateToUserID: &provider.ID,
	}, approver.ID, customerTenant.ID))

	original, err := client.TicketApproval.Get(ctx, approvalID)
	require.NoError(t, err)
	assert.Equal(t, string(dto.ApprovalStatusCancelled), original.Status)
	assert.Equal(t, providerMembership.ID, original.DelegateToMembershipID)

	delegated, err := client.TicketApproval.Query().
		Where(
			ticketapproval.TicketID(ticketID),
			ticketapproval.ApproverID(provider.ID),
			ticketapproval.Status(string(dto.ApprovalStatusPending)),
		).
		Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, providerMembership.ID, delegated.ApproverMembershipID)

	foreignTicketID, foreignApprovalID := createTicketAndApproval("TKT-DELEGATE-MB-2", approver.ID)
	err = svc.ApproveTicket(ctx, &dto.ApproveTicketRequest{
		TicketID:         foreignTicketID,
		ApprovalID:       foreignApprovalID,
		Action:           "delegate",
		Comment:          "非法委派",
		DelegateToUserID: &foreign.ID,
	}, approver.ID, customerTenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "委派目标校验失败")

	untouched, err := client.TicketApproval.Get(ctx, foreignApprovalID)
	require.NoError(t, err)
	assert.Equal(t, string(dto.ApprovalStatusPending), untouched.Status)
	assert.Zero(t, untouched.DelegateToUserID)
}
