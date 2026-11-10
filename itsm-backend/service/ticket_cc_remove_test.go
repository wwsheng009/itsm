package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestTicketWorkflowService_RemoveTicketCCRecordPermissions(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:remove_ticket_cc?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant, err := client.Tenant.Create().
		SetName("CC Remove Tenant").
		SetCode("cc-remove").
		SetDomain("cc-remove.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	requester, err := client.User.Create().
		SetUsername("cc-requester").
		SetEmail("cc-requester@test.com").
		SetName("CC Requester").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	ccUser, err := client.User.Create().
		SetUsername("cc-target").
		SetEmail("cc-target@test.com").
		SetName("CC Target").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	outsider, err := client.User.Create().
		SetUsername("cc-outsider").
		SetEmail("cc-outsider@test.com").
		SetName("CC Outsider").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	ticketEntity, err := client.Ticket.Create().
		SetTitle("抄送移除").
		SetStatus("open").
		SetPriority("medium").
		SetTicketNumber("CC-REMOVE-1").
		SetRequesterID(requester.ID).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	record, err := client.TicketCC.Create().
		SetTicketID(ticketEntity.ID).
		SetUserID(ccUser.ID).
		SetAddedBy(requester.ID).
		SetTenantID(tenant.ID).
		SetIsActive(true).
		Save(ctx)
	require.NoError(t, err)

	service := NewTicketWorkflowService(client, zaptest.NewLogger(t).Sugar())
	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, outsider.ID, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无权管理")

	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, requester.ID, tenant.ID)
	require.NoError(t, err)
	removed, err := client.TicketCC.Get(ctx, record.ID)
	require.NoError(t, err)
	assert.False(t, removed.IsActive)

	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, record.ID, requester.ID, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "抄送记录不存在")

	second, err := client.TicketCC.Create().
		SetTicketID(ticketEntity.ID).
		SetUserID(ccUser.ID).
		SetAddedBy(requester.ID).
		SetTenantID(tenant.ID).
		SetIsActive(true).
		Save(ctx)
	require.NoError(t, err)
	err = service.RemoveTicketCCRecord(ctx, ticketEntity.ID, second.ID, ccUser.ID, tenant.ID)
	require.NoError(t, err)
	selfRemoved, err := client.TicketCC.Get(ctx, second.ID)
	require.NoError(t, err)
	assert.False(t, selfRemoved.IsActive)
}
