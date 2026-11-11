package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entuser "itsm-backend/ent/user"
)

// TestAuthorizeTicketAttachmentAccess_AllowsActiveUserRoles
// 锁定 ticket 74 回归：附件列表的额外授权不能把 end_user / it_admin /
// security_admin 或已深度切换到客户租户的 provider 员工误判为 500。
func TestAuthorizeTicketAttachmentAccess_AllowsActiveUserRoles(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:attachment_access_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()

	tenant, err := client.Tenant.Create().
		SetName("Attachment Customer").SetCode("att-customer").
		SetDomain("att-customer.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	provider, err := client.Tenant.Create().
		SetName("Attachment Provider").SetCode("att-provider").
		SetDomain("att-provider.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)

	mkUser := func(name, role string, active bool, tenantID int) *ent.User {
		u, err := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetRole(entuser.Role(role)).SetActive(active).SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
		return u
	}

	requester := mkUser("att-requester", "admin", true, tenant.ID)
	assignee := mkUser("att-assignee", "agent", true, tenant.ID)
	endUser := mkUser("att-end-user", "end_user", true, tenant.ID)
	itAdmin := mkUser("att-it-admin", "it_admin", true, tenant.ID)
	secAdmin := mkUser("att-sec-admin", "security_admin", true, tenant.ID)
	inactive := mkUser("att-inactive", "end_user", false, tenant.ID)
	providerUser := mkUser("att-provider-user", "end_user", true, provider.ID)

	ticket, err := client.Ticket.Create().
		SetTitle("attachment access ticket").SetDescription("attachment access regression").
		SetPriority("medium").SetType("incident").SetStatus("new").
		SetTicketNumber("TKT-ATT-ACCESS-001").SetTenantID(tenant.ID).
		SetRequesterID(requester.ID).SetAssigneeID(assignee.ID).Save(ctx)
	require.NoError(t, err)

	svc := &TicketAttachmentService{client: client, logger: zap.NewNop().Sugar()}
	for _, u := range []*ent.User{endUser, itAdmin, secAdmin, providerUser} {
		require.NoError(t, svc.authorizeTicketAttachmentAccess(ctx, ticket.ID, tenant.ID, u.ID),
			"active user %s (%s) must be allowed", u.Username, u.Role)
	}
	require.ErrorIs(t,
		svc.authorizeTicketAttachmentAccess(ctx, ticket.ID, tenant.ID, inactive.ID),
		ErrTicketAttachmentForbidden,
	)
	require.ErrorIs(t,
		svc.authorizeTicketAttachmentAccess(ctx, 999999, tenant.ID, endUser.ID),
		ErrTicketAttachmentNotFound,
	)
}
