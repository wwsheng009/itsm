package sla

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// P1-2: MSP SLA 合同（D-SLA-4）——CRUD / 归属 / 唯一 / bindings / 生效解析
// 复用 sla_grant_test.go 的 grantFixture（provider/other/customer/def 已播种）。
// =====================================================================

func newTestContract(f *grantFixture, name string, bindings ...MSPContractSLABinding) *MSPContract {
	return &MSPContract{
		ProviderTenantID: f.provider.ID,
		CustomerTenantID: f.customer.ID,
		ContractNo:       "HT-" + name,
		Name:             name,
		Status:           "active",
		TenantID:         f.provider.ID,
		Bindings:         bindings,
	}
}

// 创建成功 + 详情富化（bindings 含定义名）。
func TestMSPContract_CreateAndGet(t *testing.T) {
	f := setupSLAGrantFixture(t)

	created, err := f.svc.CreateContract(f.ctx, newTestContract(f, "年度运维",
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: 0},
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: 88},
	))
	require.NoError(t, err)
	require.NotEmpty(t, created.Bindings)
	assert.Equal(t, "active", created.Status)
	assert.Len(t, created.Bindings, 2)
	assert.Equal(t, "Grant Def", created.Bindings[0].DefinitionName, "bindings 应富化定义名")

	got, err := f.svc.GetContract(f.ctx, created.ID, f.provider.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ContractNo, got.ContractNo)
	assert.Len(t, got.Bindings, 2)
}

// 归属不符（customer.msp_provider_id != provider）拒绝。
func TestMSPContract_ProviderMismatchForbidden(t *testing.T) {
	f := setupSLAGrantFixture(t)
	c := newTestContract(f, "冒名", MSPContractSLABinding{SLADefinitionID: f.def.ID})
	c.ProviderTenantID = f.other.ID
	c.TenantID = f.other.ID
	_, err := f.svc.CreateContract(f.ctx, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不属于当前服务商")
}

// contract_no 租户内唯一（预检友好错误）。
func TestMSPContract_DuplicateContractNo(t *testing.T) {
	f := setupSLAGrantFixture(t)
	_, err := f.svc.CreateContract(f.ctx, newTestContract(f, "重复",
		MSPContractSLABinding{SLADefinitionID: f.def.ID}))
	require.NoError(t, err)

	_, err = f.svc.CreateContract(f.ctx, newTestContract(f, "重复",
		MSPContractSLABinding{SLADefinitionID: f.def.ID}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "合同编号已存在")
}

// bindings 不得为空；同一类型不得重复绑定。
func TestMSPContract_BindingValidation(t *testing.T) {
	f := setupSLAGrantFixture(t)

	_, err := f.svc.CreateContract(f.ctx, newTestContract(f, "无绑定"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "至少需要一条")

	_, err = f.svc.CreateContract(f.ctx, newTestContract(f, "重复类型",
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: 7},
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: 7},
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "同一工单类型")
}

// 更新：状态流转 + bindings 全量替换（先删后建）。
func TestMSPContract_UpdateStatusAndReplaceBindings(t *testing.T) {
	f := setupSLAGrantFixture(t)
	created, err := f.svc.CreateContract(f.ctx, newTestContract(f, "更新",
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: 0}))
	require.NoError(t, err)

	terminated := "terminated"
	updated, err := f.svc.UpdateContract(f.ctx, &MSPContract{
		ID:               created.ID,
		ProviderTenantID: f.provider.ID,
		TenantID:         f.provider.ID,
		Status:           terminated,
		Bindings: []MSPContractSLABinding{
			{SLADefinitionID: f.def.ID, TicketTypeID: 5},
			{SLADefinitionID: f.def.ID, TicketTypeID: 6},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "terminated", updated.Status)
	require.Len(t, updated.Bindings, 2, "bindings 全量替换")
	assert.Empty(t, findBinding(updated.Bindings, 0), "旧绑定（类型无关）已被移除")
	require.NotEmpty(t, findBinding(updated.Bindings, 5))
}

func findBinding(bindings []MSPContractSLABinding, ticketTypeID int64) []MSPContractSLABinding {
	out := make([]MSPContractSLABinding, 0)
	for _, b := range bindings {
		if b.TicketTypeID == ticketTypeID {
			out = append(out, b)
		}
	}
	return out
}

// FindActiveContractForCustomer：active + 合同期内命中；terminated/过期不命中。
func TestMSPContract_FindActive(t *testing.T) {
	f := setupSLAGrantFixture(t)

	created, err := f.svc.CreateContract(f.ctx, newTestContract(f, "生效中",
		MSPContractSLABinding{SLADefinitionID: f.def.ID}))
	require.NoError(t, err)
	// 基准时钟取创建之后：合同默认 effective_from=创建时 now，避免毫秒级边界。
	now := time.Now()

	found, err := f.repo.FindActiveContractForCustomer(f.ctx, f.customer.ID, now)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)

	// terminated 后不再命中
	status := "terminated"
	_, err = f.svc.UpdateContract(f.ctx, &MSPContract{
		ID: created.ID, ProviderTenantID: f.provider.ID, TenantID: f.provider.ID, Status: status,
	})
	require.NoError(t, err)
	found, err = f.repo.FindActiveContractForCustomer(f.ctx, f.customer.ID, now)
	require.NoError(t, err)
	assert.Nil(t, found, "terminated 合同不参与匹配")
}

// 过期合同不命中（effective_until 已过）。
func TestMSPContract_FindActiveExcludesExpired(t *testing.T) {
	f := setupSLAGrantFixture(t)
	now := time.Now()
	expired := now.Add(-time.Hour)

	c := newTestContract(f, "过期", MSPContractSLABinding{SLADefinitionID: f.def.ID})
	c.EffectiveUntil = &expired
	_, err := f.svc.CreateContract(f.ctx, c)
	require.NoError(t, err)
	now = time.Now()

	found, err := f.repo.FindActiveContractForCustomer(f.ctx, f.customer.ID, now)
	require.NoError(t, err)
	assert.Nil(t, found, "过期合同不参与匹配")
}
