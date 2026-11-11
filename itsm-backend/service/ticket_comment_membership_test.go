package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"

	_ "github.com/mattn/go-sqlite3"
)

// TestCreateTicketCommentPersistsAuthorMembership 锁定评论 actor 的租户内身份：
// user_id 仍是全局账号，user_membership_id 是本次请求租户内的 membership。
func TestCreateTicketCommentPersistsAuthorMembership(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticket-comment-membership?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("comment-tenant").SetName("comment-tenant").SaveX(ctx)
	author := client.User.Create().
		SetUsername("comment-author").SetEmail("comment-author@example.com").SetName("author").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SetActive(true).SaveX(ctx)
	membership := client.UserTenantMembership.Create().
		SetUserID(author.ID).SetTenantID(tenant.ID).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetSource(usertenantmembership.SourceHome).
		SetStatus(usertenantmembership.StatusActive).SetIsDefault(true).SaveX(ctx)
	tkt := client.Ticket.Create().
		SetTicketNumber("TKT-COMMENT-MB-1").
		SetTitle("comment membership").
		SetStatus("open").
		SetRequesterID(author.ID).
		SetTenantID(tenant.ID).SaveX(ctx)

	svc := NewTicketCommentService(client, zaptest.NewLogger(t).Sugar())
	resp, err := svc.CreateTicketComment(ctx, tkt.ID, &dto.CreateTicketCommentRequest{
		Content: "provider/customer comment",
	}, author.ID, tenant.ID)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, membership.ID, resp.UserMembershipID)

	row, err := client.TicketComment.Get(ctx, resp.ID)
	require.NoError(t, err)
	assert.Equal(t, membership.ID, row.UserMembershipID)
}
