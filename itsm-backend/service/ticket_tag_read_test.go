package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketTagService_ListTicketTagsSortedAndBound(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ticket_tag_read?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant, err := client.Tenant.Create().
		SetName("Tag Read Tenant").
		SetCode("tag-read").
		SetDomain("tag-read.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("tag-read-user").
		SetEmail("tag-read@test.com").
		SetName("Tag Read User").
		SetPasswordHash("hash").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	ticketEntity, err := client.Ticket.Create().
		SetTitle("标签读取").
		SetStatus("open").
		SetPriority("medium").
		SetTicketNumber("TAG-READ-1").
		SetRequesterID(user.ID).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	tagBeta, err := client.TicketTag.Create().
		SetName("Beta").
		SetColor("#111111").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	tagAlpha, err := client.TicketTag.Create().
		SetName("Alpha").
		SetColor("#222222").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	unbound, err := client.TicketTag.Create().
		SetName("Gamma").
		SetColor("#333333").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = ticketEntity.Update().AddTags(tagBeta, tagAlpha).Save(ctx)
	require.NoError(t, err)

	tags, err := NewTicketTagService(client).ListTicketTags(ctx, ticketEntity.ID, tenant.ID)
	require.NoError(t, err)
	require.Len(t, tags, 2)
	assert.Equal(t, []string{"Alpha", "Beta"}, []string{tags[0].Name, tags[1].Name})
	assert.NotEqual(t, unbound.ID, tags[0].ID)
}
