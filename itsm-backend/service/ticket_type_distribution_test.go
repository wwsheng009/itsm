package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/mspcontractslabinding"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =====================================================================
// P2-3 D-TT-1 工单类型受控下发（方案 §4.5a）
// 物化(INV-1) / 受控锁 / 发布(INV-2/3) / 解绑转 native(INV-4) / 不收编(INV-5) / SLA binding 联动
// =====================================================================

type ttDistFixture struct {
	client   *ent.Client
	ctx      context.Context
	svc      *TicketTypeDistributionService
	typeSvc  *TicketTypeService
	provider *ent.Tenant
	customer *ent.Tenant
	actor    MSPWorkbenchActor
}

func setupTTDistFixture(t *testing.T) *ttDistFixture {
	t.Helper()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:ttdist_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()
	uid := time.Now().UnixNano()

	provider := client.Tenant.Create().
		SetName("TT Provider").SetCode(fmt.Sprintf("ttprov%d", uid)).SetType("msp_provider").SetStatus("active").SaveX(ctx)
	customer := client.Tenant.Create().
		SetName("TT Customer").SetCode(fmt.Sprintf("ttcust%d", uid)).SetType("msp_customer").
		SetMspProviderID(provider.ID).SetStatus("active").SaveX(ctx)

	typeSvc := NewTicketTypeService(client, logger)
	distSvc := NewTicketTypeDistributionService(client, stubCustomerAccessChecker{}, logger)
	typeSvc.SetDistributionHooks(distSvc)

	return &ttDistFixture{
		client: client, ctx: ctx, svc: distSvc, typeSvc: typeSvc,
		provider: provider, customer: customer,
		actor: MSPWorkbenchActor{UserID: 7, Username: "msp-admin", HomeTenantID: provider.ID},
	}
}

// mkTTType 建一个满足 TicketType 全部必填字段的类型行。
func mkTTType(t *testing.T, client *ent.Client, ctx context.Context, tenantID int64, code string) *ent.TicketType {
	t.Helper()
	now := time.Now()
	return client.TicketType.Create().
		SetCode(code).SetName(code).SetDescription("").SetIcon("").SetColor("").
		SetStatus("active").SetDefaultPriority("medium").
		SetTenantID(tenantID).SetCreatedBy(1).SetCreatedAt(now).SetUpdatedAt(now).
		SetCustomFields(map[string]interface{}{}).
		SetApprovalChain([]interface{}{}).
		SetAssignmentRules([]interface{}{}).
		SetNotificationConfig(map[string]interface{}{}).
		SetPermissionConfig(map[string]interface{}{}).
		SaveX(ctx)
}

func (f *ttDistFixture) distribute(t *testing.T, srcID int) *dto.DistributeTicketTypeResponse {
	t.Helper()
	resp, err := f.svc.Distribute(f.ctx, f.actor, f.customer.ID, srcID)
	require.NoError(t, err)
	require.NotNil(t, resp.Distribution)
	return resp
}

// 物化副本：origin 三元组 + 受控锁 + 溯源行；重复下发拒绝（防重复物化）。
func TestTicketTypeDistribution_DistributeMaterializesCopy(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "hw_fault")
	f.client.TicketType.UpdateOne(src).SetName("硬件故障").SetDescription("hw").ExecX(f.ctx)

	resp := f.distribute(t, src.ID)
	item := resp.Distribution
	assert.Equal(t, f.customer.ID, int(item.CustomerTenantID))
	assert.Equal(t, f.provider.ID, int(item.ProviderTenantID))
	assert.Equal(t, src.ID, int(item.SourceTypeID))
	assert.Equal(t, "synced", item.SyncStatus)
	assert.False(t, item.UpdateAvailable)

	copyRow := f.client.TicketType.GetX(f.ctx, int(item.CopyTypeID))
	assert.Equal(t, "controlled", copyRow.Origin)
	assert.Equal(t, int64(f.provider.ID), copyRow.OriginProviderTenantID)
	assert.Equal(t, int64(src.ID), copyRow.OriginTypeID)
	assert.True(t, copyRow.ControlledByProvider)
	assert.Equal(t, "synced", copyRow.SyncStatus)
	assert.Equal(t, int64(f.customer.ID), copyRow.TenantID)
	assert.Equal(t, "hw_fault", copyRow.Code, "无冲突时复用原 code")
	assert.Equal(t, "硬件故障", copyRow.Name, "name 保持 provider 原名")

	dist := f.client.TicketTypeDistribution.GetX(f.ctx, item.ID)
	assert.Equal(t, "synced", dist.SyncStatus)
	assert.Equal(t, int64(f.customer.ID), dist.TenantID)
	assert.Equal(t, int64(f.provider.ID), dist.ProviderTenantID)

	// 重复下发（同源同客户）拒绝
	_, err := f.svc.Distribute(f.ctx, f.actor, f.customer.ID, src.ID)
	assert.True(t, errors.Is(err, ErrTicketTypeDistributionConflict))
}

// code 撞车机械后缀（INV-1/INV-5）：native 行零改动，副本以新行物化。
func TestTicketTypeDistribution_CodeCollisionSuffixAndNativeUntouched(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "hw_fault")
	native := mkTTType(t, f.client, f.ctx, int64(f.customer.ID), "hw_fault")
	f.client.TicketType.UpdateOne(native).SetName("客户自建类型").ExecX(f.ctx)

	resp := f.distribute(t, src.ID)
	copyRow := f.client.TicketType.GetX(f.ctx, int(resp.Distribution.CopyTypeID))
	assert.Equal(t, fmt.Sprintf("hw_fault-p%d", f.provider.ID), copyRow.Code, "撞车走机械后缀")
	assert.Equal(t, "hw_fault", src.Code, "源侧 code 不动")

	nativeAfter := f.client.TicketType.GetX(f.ctx, native.ID)
	assert.Equal(t, "客户自建类型", nativeAfter.Name, "INV-1/INV-5：native 行零改动")
	assert.Equal(t, "native", nativeAfter.Origin)
	assert.False(t, nativeAfter.ControlledByProvider)
}

// 受控锁：customer 改受控字段（name/description/slaEnabled/defaultSlaId）拒绝；治理字段可改。
func TestTicketTypeDistribution_ControlledLock(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "lock_type")
	resp := f.distribute(t, src.ID)
	copyID := int(resp.Distribution.CopyTypeID)

	newName := "客户改名"
	_, err := f.typeSvc.UpdateTicketType(f.ctx, copyID, &dto.UpdateTicketTypeRequest{Name: &newName}, f.customer.ID, 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "受控字段")

	newIcon := "box"
	updated, err := f.typeSvc.UpdateTicketType(f.ctx, copyID, &dto.UpdateTicketTypeRequest{Icon: &newIcon}, f.customer.ID, 9)
	require.NoError(t, err, "治理字段（icon）可改")
	assert.Equal(t, "box", updated.Icon)
	assert.Equal(t, src.Name, updated.Name, "受控字段保持 provider 值")
}

// 源编辑 → 全部活跃副本标 dirty（"有可用更新"，提交后钩子）。
func TestTicketTypeDistribution_SourceEditMarksDirty(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "dirty_type")
	resp := f.distribute(t, src.ID)

	newName := "改名后"
	_, err := f.typeSvc.UpdateTicketType(f.ctx, src.ID, &dto.UpdateTicketTypeRequest{Name: &newName}, f.provider.ID, f.actor.UserID)
	require.NoError(t, err)

	copyRow := f.client.TicketType.GetX(f.ctx, int(resp.Distribution.CopyTypeID))
	assert.Equal(t, "dirty", copyRow.SyncStatus)
	assert.Equal(t, "dirty", f.client.TicketTypeDistribution.GetX(f.ctx, resp.Distribution.ID).SyncStatus)

	list, err := f.svc.ListDistributions(f.ctx, f.actor, f.customer.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.True(t, list[0].UpdateAvailable, "dirty → 前端'有可用更新'")
}

// 发布流程：预览 diff → 发布（受控字段白名单）→ 收敛 synced → 再预览 up-to-date；治理字段不动（INV-3）。
func TestTicketTypeDistribution_PreviewAndPublish(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "pub_type")
	resp := f.distribute(t, src.ID)
	distID := resp.Distribution.ID
	copyID := int(resp.Distribution.CopyTypeID)

	// 客户先调治理字段（发布不得覆盖）
	icon := "mine"
	_, err := f.typeSvc.UpdateTicketType(f.ctx, copyID, &dto.UpdateTicketTypeRequest{Icon: &icon}, f.customer.ID, 9)
	require.NoError(t, err)

	newName := "发布名"
	newDesc := "发布描述"
	_, err = f.typeSvc.UpdateTicketType(f.ctx, src.ID, &dto.UpdateTicketTypeRequest{Name: &newName, Description: &newDesc}, f.provider.ID, f.actor.UserID)
	require.NoError(t, err)

	preview, err := f.svc.Preview(f.ctx, f.actor, f.customer.ID, distID)
	require.NoError(t, err)
	assert.False(t, preview.UpToDate)
	require.Len(t, preview.Changes, 2)
	assert.Equal(t, "name", preview.Changes[0].Field)
	assert.Equal(t, "发布名", preview.Changes[0].To)

	pub, err := f.svc.Publish(f.ctx, f.actor, f.customer.ID, distID)
	require.NoError(t, err)
	assert.Equal(t, "synced", pub.Status)
	assert.NotNil(t, pub.SyncedAt)

	copyRow := f.client.TicketType.GetX(f.ctx, copyID)
	assert.Equal(t, "发布名", copyRow.Name)
	assert.Equal(t, "发布描述", copyRow.Description)
	assert.Equal(t, "synced", copyRow.SyncStatus)
	assert.Equal(t, "mine", copyRow.Icon, "INV-3：治理字段永不在 provider 写路径内")

	preview2, err := f.svc.Preview(f.ctx, f.actor, f.customer.ID, distID)
	require.NoError(t, err)
	assert.True(t, preview2.UpToDate)
}

// INV-2：origin 守卫——provenance 被改的副本发布时 SQL 层不可达，标 failed 可重试。
func TestTicketTypeDistribution_PublishOriginGuard(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "guard_type")
	resp := f.distribute(t, src.ID)
	distID := resp.Distribution.ID
	copyID := int(resp.Distribution.CopyTypeID)

	// 篡改副本 provenance（模拟应用层 bug / 非法直改）
	f.client.TicketType.UpdateOneID(copyID).SetOrigin("native").SetControlledByProvider(false).ExecX(f.ctx)

	newName := "不该到达"
	_, err := f.typeSvc.UpdateTicketType(f.ctx, src.ID, &dto.UpdateTicketTypeRequest{Name: &newName}, f.provider.ID, f.actor.UserID)
	require.NoError(t, err)

	pub, err := f.svc.Publish(f.ctx, f.actor, f.customer.ID, distID)
	require.NoError(t, err)
	assert.Equal(t, "failed", pub.Status)
	assert.NotEmpty(t, pub.Error)
	assert.NotEqual(t, "不该到达", f.client.TicketType.GetX(f.ctx, copyID).Name, "native 行物理不可达（INV-2）")
}

// 解绑：副本转 native（保留值，受控解锁）+ 分发行 unbound + 合同 binding 级联移除；零 DELETE、可重新下发。
func TestTicketTypeDistribution_UnbindReleasesCopy(t *testing.T) {
	f := setupTTDistFixture(t)
	def := mkTTSLADef(t, f, "unbind_def")
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "unbind_type")
	f.client.TicketType.UpdateOne(src).SetSLAEnabled(true).SetDefaultSLAID(int64(def.ID)).ExecX(f.ctx)
	contract := mkTTContract(t, f)

	resp := f.distribute(t, src.ID)
	assert.Equal(t, "created", resp.SLABinding, "下发即联动合同 binding")
	copyID := int(resp.Distribution.CopyTypeID)

	icon := "mine"
	_, err := f.typeSvc.UpdateTicketType(f.ctx, copyID, &dto.UpdateTicketTypeRequest{Icon: &icon}, f.customer.ID, 9)
	require.NoError(t, err)

	out, err := f.svc.Unbind(f.ctx, f.actor, f.customer.ID, resp.Distribution.ID)
	require.NoError(t, err)
	assert.True(t, out.Released)
	assert.True(t, out.BindingRemoved)

	copyRow := f.client.TicketType.GetX(f.ctx, copyID)
	assert.Equal(t, "native", copyRow.Origin)
	assert.Equal(t, int64(0), copyRow.OriginProviderTenantID)
	assert.Equal(t, int64(0), copyRow.OriginTypeID)
	assert.False(t, copyRow.ControlledByProvider)
	assert.Equal(t, "", copyRow.SyncStatus)
	assert.Equal(t, "mine", copyRow.Icon, "治理字段保留")
	assert.Equal(t, "unbind_type", copyRow.Code, "类型保留（INV-4 零 DELETE）")

	n := f.client.MSPContractSLABinding.Query().
		Where(
			mspcontractslabinding.ContractIDEQ(contract.ID),
			mspcontractslabinding.TicketTypeIDEQ(int64(copyID)),
		).
		CountX(f.ctx)
	assert.Equal(t, 0, n, "解绑级联：binding 移除")

	// 解绑后可重新下发（新副本新行；历史行保留为 unbound）
	resp2 := f.distribute(t, src.ID)
	assert.NotEqual(t, resp.Distribution.ID, resp2.Distribution.ID)
	assert.NotEqual(t, int64(copyID), resp2.Distribution.CopyTypeID)

	_, err = f.svc.Publish(f.ctx, f.actor, f.customer.ID, resp.Distribution.ID)
	require.Error(t, err, "已解绑分发行不可再发布")
}

// 无生效合同：类型可先行下发，SLA 联动标注 skipped_no_contract（发布时再尝试）。
func TestTicketTypeDistribution_DistributeSkippedNoContract(t *testing.T) {
	f := setupTTDistFixture(t)
	def := mkTTSLADef(t, f, "no_contract_def")
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "no_contract_type")
	f.client.TicketType.UpdateOne(src).SetSLAEnabled(true).SetDefaultSLAID(int64(def.ID)).ExecX(f.ctx)

	resp := f.distribute(t, src.ID)
	assert.Equal(t, "skipped_no_contract", resp.SLABinding)

	preview, err := f.svc.Preview(f.ctx, f.actor, f.customer.ID, resp.Distribution.ID)
	require.NoError(t, err)
	assert.Equal(t, "none", preview.SLAAction, "无合同 → 联动暂缓（发布时再尝试）")
	assert.NotEmpty(t, preview.Note)
}

// 源归档 → 副本/记录 orphaned 保留（INV-4）；受控副本禁止客户删除。
func TestTicketTypeDistribution_SourceArchiveOrphaned(t *testing.T) {
	f := setupTTDistFixture(t)
	src := mkTTType(t, f.client, f.ctx, int64(f.provider.ID), "orphan_type")
	resp := f.distribute(t, src.ID)
	copyID := int(resp.Distribution.CopyTypeID)

	err := f.typeSvc.DeleteTicketType(f.ctx, copyID, f.customer.ID, 9)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不可删除")

	require.NoError(t, f.typeSvc.DeleteTicketType(f.ctx, src.ID, f.provider.ID, f.actor.UserID))
	assert.Equal(t, "orphaned", f.client.TicketType.GetX(f.ctx, copyID).SyncStatus)
	assert.Equal(t, "orphaned", f.client.TicketTypeDistribution.GetX(f.ctx, resp.Distribution.ID).SyncStatus)
}

// ---- fixtures ----

func mkTTSLADef(t *testing.T, f *ttDistFixture, name string) *ent.SLADefinition {
	t.Helper()
	return f.client.SLADefinition.Create().
		SetName(name).SetResponseTime(60).SetResolutionTime(480).
		SetTenantID(f.provider.ID).SetOwnerProviderTenantID(f.provider.ID).SetIsActive(true).
		SaveX(f.ctx)
}

func mkTTContract(t *testing.T, f *ttDistFixture) *ent.MSPContract {
	t.Helper()
	return f.client.MSPContract.Create().
		SetProviderTenantID(f.provider.ID).SetCustomerTenantID(f.customer.ID).
		SetContractNo(fmt.Sprintf("TT-HT-%d", time.Now().UnixNano())).SetName("TT 合同").
		SetStatus("active").SetEffectiveFrom(time.Now().Add(-24 * time.Hour)).
		SetTenantID(f.provider.ID).
		SaveX(f.ctx)
}
