package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entTenant "itsm-backend/ent/tenant"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var snapshotBackfillDBCounter int64

// TestBackfillTicketMSPSnapshots 锁定 IP-P0-3 回填语义：
// dry-run 不写库；apply 幂等；未托管/暂停/软删行不触碰。
func TestBackfillTicketMSPSnapshots(t *testing.T) {
	dsn := fmt.Sprintf("file:snap_backfill_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&snapshotBackfillDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	ctx := context.Background()
	logger := zap.NewNop().Sugar()

	mkTenant := func(name, code, typ, status string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(entTenant.Type(typ)).SetStatus(status)
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
			SetPasswordHash("hash").SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
		return u
	}
	mkTicket := func(number string, tenantID, requesterID int) *ent.Ticket {
		tk, err := client.Ticket.Create().
			SetTicketNumber(number).SetTitle(number).SetStatus("open").
			SetTenantID(tenantID).SetRequesterID(requesterID).SetPriority("high").
			Save(ctx)
		require.NoError(t, err)
		return tk
	}

	provider := mkTenant("Provider", "snap-prov", "msp_provider", "active", 0)
	providerOff := mkTenant("ProviderOff", "snap-prov-off", "msp_provider", "suspended", 0)
	custA := mkTenant("Customer A", "snap-a", "msp_customer", "active", provider.ID)
	custB := mkTenant("Customer B", "snap-b", "msp_customer", "active", 0) // 未托管
	custC := mkTenant("Customer C", "snap-c", "msp_customer", "active", providerOff.ID)

	userA := mkUser("snap-user-a", custA.ID)
	userB := mkUser("snap-user-b", custB.ID)
	userC := mkUser("snap-user-c", custC.ID)

	tA := mkTicket("SNAP-A-1", custA.ID, userA.ID) // 待回填
	tA2 := mkTicket("SNAP-A-2", custA.ID, userA.ID)
	_, err := client.Ticket.UpdateOneID(tA2.ID).SetIsManagedByMsp(true).SetMspProviderID(provider.ID).Save(ctx)
	require.NoError(t, err)
	tB := mkTicket("SNAP-B-1", custB.ID, userB.ID) // 未托管 → skip
	tC := mkTicket("SNAP-C-1", custC.ID, userC.ID) // provider 暂停 → skip
	tD := mkTicket("SNAP-D-1", custA.ID, userA.ID) // 软删 → 不入候选
	_, err = client.Ticket.UpdateOneID(tD.ID).SetDeletedAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)

	// dry-run：候选=1（仅 tA），不写库。
	res, err := BackfillTicketMSPSnapshots(ctx, client, logger, false, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Tenants, "A/B/C 均有候选；C 的 provider 暂停 → 派生失败计入 skipped")
	assert.Equal(t, 3, res.Scanned, "tA/tB/tC 计入；tA2 已正确、tD 软删不计")
	assert.Equal(t, 1, res.Candidates)
	assert.Equal(t, 0, res.Updated)
	got, err := client.Ticket.Get(ctx, tA.ID)
	require.NoError(t, err)
	assert.False(t, got.IsManagedByMsp, "dry-run 不得写库")
	assert.Zero(t, got.MspProviderID)

	// apply：仅 tA 更新。
	res, err = BackfillTicketMSPSnapshots(ctx, client, logger, true, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)
	got, err = client.Ticket.Get(ctx, tA.ID)
	require.NoError(t, err)
	assert.True(t, got.IsManagedByMsp)
	assert.Equal(t, provider.ID, got.MspProviderID)

	// 幂等：二次 apply 无候选。
	res, err = BackfillTicketMSPSnapshots(ctx, client, logger, true, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Candidates)
	assert.Equal(t, 0, res.Updated)

	// tenant 过滤：仅处理指定租户。
	res, err = BackfillTicketMSPSnapshots(ctx, client, logger, false, custB.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Tenants)
	assert.Equal(t, 1, res.Scanned)

	_ = tB
	_ = tC
}
