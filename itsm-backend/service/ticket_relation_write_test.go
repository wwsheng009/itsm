package service

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/require"
)

// TestAddRemoveTicketRelation_TenantScoped 工单关联写面（B4）：
// 双向边写入/幂等/自身拒绝/类型白名单/跨租户 fail-closed/删除双向清除。
func TestAddRemoveTicketRelation_TenantScoped(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ticketrelationwrite?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	tenantA := createTicketAssociationTenant(t, ctx, client, "rel-a")
	tenantB := createTicketAssociationTenant(t, ctx, client, "rel-b")
	userA := createTicketAssociationUser(t, ctx, client, tenantA.ID, "rel-user-a")
	userB := createTicketAssociationUser(t, ctx, client, tenantB.ID, "rel-user-b")
	t1 := createTicketAssociationTicket(t, ctx, client, tenantA.ID, userA.ID, "TKT-REL-001")
	t2 := createTicketAssociationTicket(t, ctx, client, tenantA.ID, userA.ID, "TKT-REL-002")
	t3 := createTicketAssociationTicket(t, ctx, client, tenantB.ID, userB.ID, "TKT-REL-003")

	svc := NewTicketAssociationService(client)

	view, err := svc.AddTicketRelation(ctx, tenantA.ID, t1.ID, t2.ID, "", "")
	require.NoError(t, err)
	require.Equal(t, "relates_to", view.RelationType, "空类型归一为 relates_to")
	require.Equal(t, t2.ID, view.TargetTicketID)

	// 双向边可见。
	deps, err := svc.GetTicketDependencies(ctx, t1.ID)
	require.NoError(t, err)
	require.Len(t, deps.RelatedTickets, 1)
	depsReverse, err := svc.GetTicketDependencies(ctx, t2.ID)
	require.NoError(t, err)
	require.Len(t, depsReverse.RelatedTickets, 1)

	// 幂等：重复添加不产生重复边。
	_, err = svc.AddTicketRelation(ctx, tenantA.ID, t1.ID, t2.ID, "related", "")
	require.NoError(t, err)
	deps, err = svc.GetTicketDependencies(ctx, t1.ID)
	require.NoError(t, err)
	require.Len(t, deps.RelatedTickets, 1)

	// 关联自身拒绝。
	_, err = svc.AddTicketRelation(ctx, tenantA.ID, t1.ID, t1.ID, "", "")
	require.ErrorIs(t, err, ErrRelationSelf)

	// 类型白名单：blocks 等类型化关系为 roadmap，显式拒绝。
	_, err = svc.AddTicketRelation(ctx, tenantA.ID, t1.ID, t2.ID, "blocks", "")
	require.ErrorIs(t, err, ErrRelationUnsupportedType)

	// 跨租户目标 fail-closed（不泄露存在性）。
	_, err = svc.AddTicketRelation(ctx, tenantA.ID, t1.ID, t3.ID, "", "")
	require.ErrorIs(t, err, ErrRelationNotFound)

	// 非法 ID。
	_, err = svc.AddTicketRelation(ctx, tenantA.ID, 0, t2.ID, "", "")
	require.ErrorIs(t, err, ErrRelationInvalidIDs)

	// 删除：双向清除 + 幂等。
	require.NoError(t, svc.RemoveTicketRelation(ctx, tenantA.ID, t1.ID, t2.ID))
	deps, err = svc.GetTicketDependencies(ctx, t1.ID)
	require.NoError(t, err)
	require.Empty(t, deps.RelatedTickets)
	depsReverse, err = svc.GetTicketDependencies(ctx, t2.ID)
	require.NoError(t, err)
	require.Empty(t, depsReverse.RelatedTickets)
	require.NoError(t, svc.RemoveTicketRelation(ctx, tenantA.ID, t1.ID, t2.ID), "重复删除幂等")

	// 跨租户删除 fail-closed。
	require.ErrorIs(t, svc.RemoveTicketRelation(ctx, tenantA.ID, t1.ID, t3.ID), ErrRelationNotFound)
}
