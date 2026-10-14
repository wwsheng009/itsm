package sla

import (
	"errors"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// P2-2 结算报表（D-SLA-5/D-SLA-9，方案 §4.5/§13）
// 白名单口径（§13.2）+ 双口径对账（§13.5）+ matched_layer 过滤与重算双保险（§13.6）
// =====================================================================

func settlementRequester(t *testing.T, f *grantFixture) *ent.User {
	t.Helper()
	return f.client.User.Create().
		SetUsername("req-" + slaUniqueID()).SetName("Req").SetEmail("req@sla.test").
		SetPasswordHash("hash").SetTenantID(f.customer.ID).SaveX(f.ctx)
}

func settlementTicketType(t *testing.T, f *grantFixture, code string, exempt bool) *ent.TicketType {
	t.Helper()
	now := time.Now()
	return f.client.TicketType.Create().
		SetCode(code + "-" + slaUniqueID()).SetName(code).
		SetDescription("").SetIcon("").SetColor("").
		SetStatus("active").SetDefaultPriority("medium").
		SetTenantID(int64(f.customer.ID)).SetSLAExempt(exempt).
		SetCreatedBy(1).SetCreatedAt(now).SetUpdatedAt(now).
		SetCustomFields(map[string]interface{}{}).
		SetApprovalChain([]interface{}{}).
		SetAssignmentRules([]interface{}{}).
		SetNotificationConfig(map[string]interface{}{}).
		SetPermissionConfig(map[string]interface{}{}).
		SaveX(f.ctx)
}

// settlementTicket 建一张带类型与 MSP 归属的工单；createdAgo 控制客观时间。
func settlementTicket(t *testing.T, f *grantFixture, requesterID int, num string, typeID int, createdAgo time.Duration) *ent.Ticket {
	t.Helper()
	return f.client.Ticket.Create().
		SetTitle("结算工单 " + num).SetDescription("d").SetPriority("high").SetType("incident").
		SetStatus("open").SetTicketNumber(num).SetTenantID(f.customer.ID).
		SetRequesterID(requesterID).SetMspProviderID(f.provider.ID).
		SetTicketTypeID(typeID).
		SetCreatedAt(time.Now().Add(-createdAgo)).
		SaveX(f.ctx)
}

func settlementContract(t *testing.T, f *grantFixture, bindings ...MSPContractSLABinding) *MSPContract {
	t.Helper()
	c := &MSPContract{
		ProviderTenantID: f.provider.ID,
		CustomerTenantID: f.customer.ID,
		ContractNo:       "HT-STL-" + slaUniqueID(),
		Name:             "结算合同",
		Status:           "active",
		TenantID:         f.provider.ID,
		EffectiveFrom:    time.Now().Add(-48 * time.Hour),
		Currency:         "CNY",
		Bindings:         bindings,
	}
	created, err := f.svc.CreateContract(f.ctx, c)
	require.NoError(t, err)
	return created
}

func findSettlementRow(report *dto.SLASettlementReport, ticketID int) *dto.SLASettlementRow {
	for i := range report.Rows {
		if report.Rows[i].TicketID == ticketID {
			return &report.Rows[i]
		}
	}
	return nil
}

// 白名单口径：分母=合同覆盖类型工单；未覆盖/豁免工单单列桶、不进分母（SLA-A11）。
func TestSettlementReport_Whitelist(t *testing.T) {
	f := setupSLAGrantFixture(t)
	req := settlementRequester(t, f)
	ttA := settlementTicketType(t, f, "typeA", false)
	ttB := settlementTicketType(t, f, "typeB", false)
	ttC := settlementTicketType(t, f, "typeC", false) // 未覆盖（无绑定）
	ttE := settlementTicketType(t, f, "typeE", true)  // 豁免

	settlementContract(t, f,
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)},
		MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttB.ID)},
	)

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(time.Hour)

	// t1：合同基准达成（响应/解决均未超时）
	t1 := settlementTicket(t, f, req.ID, "STL-1", ttA.ID, 5*time.Hour)
	f.client.Ticket.UpdateOneID(t1.ID).
		SetFirstResponseAt(t1.CreatedAt.Add(10 * time.Minute)).
		SetResolvedAt(t1.CreatedAt.Add(200 * time.Minute)).
		ExecX(f.ctx)

	// t2：合同基准违约（至今未响应，已过响应截止）
	t2 := settlementTicket(t, f, req.ID, "STL-2", ttB.ID, 5*time.Hour)

	// t3：未覆盖类型；t4：豁免类型
	settlementTicket(t, f, req.ID, "STL-3", ttC.ID, 3*time.Hour)
	settlementTicket(t, f, req.ID, "STL-4", ttE.ID, 3*time.Hour)

	report, err := f.svc.GetSettlementReport(f.ctx, f.provider.ID, f.customer.ID, from, to)
	require.NoError(t, err)

	assert.Equal(t, 2, report.CoveredTickets, "分母=合同覆盖类型工单")
	assert.Equal(t, 1, report.UncoveredTickets, "未覆盖桶不进分母")
	assert.Equal(t, 1, report.ExemptTickets, "豁免桶不进分母")
	assert.Equal(t, 1, report.ContractMet)
	assert.Equal(t, 1, report.ContractBreached)
	assert.Equal(t, 1, report.SettlementBreaches)
	assert.Equal(t, 50.0, report.ComplianceRate)

	r1 := findSettlementRow(report, t1.ID)
	require.NotNil(t, r1)
	assert.Equal(t, "met", r1.ContractVerdict)
	r2 := findSettlementRow(report, t2.ID)
	require.NotNil(t, r2)
	assert.Equal(t, "breached", r2.ContractVerdict)
}

// 双口径对账：合同重算 vs 客户内部快照，差异行 driftFlag=true（§13.5，SLA-A13）。
func TestSettlementReport_DualVerdictDrift(t *testing.T) {
	f := setupSLAGrantFixture(t)
	req := settlementRequester(t, f)
	ttA := settlementTicketType(t, f, "typeA", false)
	settlementContract(t, f, MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)})

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(time.Hour)

	// tDrift：合同 480m 违约（实际 540m），客户内部快照 600m 达成 → 有害漂移行
	tDrift := settlementTicket(t, f, req.ID, "STL-D1", ttA.ID, 10*time.Hour)
	f.client.Ticket.UpdateOneID(tDrift.ID).
		SetFirstResponseAt(tDrift.CreatedAt.Add(10 * time.Minute)).
		SetResolvedAt(tDrift.CreatedAt.Add(540 * time.Minute)).
		SetSLAResponseDeadline(tDrift.CreatedAt.Add(60 * time.Minute)).
		SetSLAResolutionDeadline(tDrift.CreatedAt.Add(600 * time.Minute)).
		ExecX(f.ctx)

	// tBenign：合同达成（30m），客户内部快照 10m 违约 → 良性漂移行（§13.6）
	tBenign := settlementTicket(t, f, req.ID, "STL-D2", ttA.ID, 10*time.Hour)
	f.client.Ticket.UpdateOneID(tBenign.ID).
		SetFirstResponseAt(tBenign.CreatedAt.Add(10 * time.Minute)).
		SetResolvedAt(tBenign.CreatedAt.Add(30 * time.Minute)).
		SetSLAResponseDeadline(tBenign.CreatedAt.Add(5 * time.Minute)).
		SetSLAResolutionDeadline(tBenign.CreatedAt.Add(10 * time.Minute)).
		ExecX(f.ctx)

	report, err := f.svc.GetSettlementReport(f.ctx, f.provider.ID, f.customer.ID, from, to)
	require.NoError(t, err)

	rd := findSettlementRow(report, tDrift.ID)
	require.NotNil(t, rd)
	assert.Equal(t, "breached", rd.ContractVerdict)
	assert.Equal(t, "met", rd.InternalVerdict)
	assert.True(t, rd.DriftFlag, "合同违约/内部达成 = 有害漂移行")

	rb := findSettlementRow(report, tBenign.ID)
	require.NotNil(t, rb)
	assert.Equal(t, "met", rb.ContractVerdict)
	assert.Equal(t, "breached", rb.InternalVerdict)
	assert.True(t, rb.DriftFlag, "合同达成/内部违约 = 良性漂移行")

	assert.Equal(t, 1, report.ContractBreached, "tDrift 计入合同违约（tBenign 合同达成）")
	assert.Equal(t, 1, report.SettlementBreaches)
}

// matched_layer 过滤（§13.6）+ 裁决口径（§13.2 ⑤）：客户内部域违约不进结算；
// waived/未裁决 disputed 不计违约。
func TestSettlementReport_MatchedLayerFilterAndAdjudication(t *testing.T) {
	f := setupSLAGrantFixture(t)
	req := settlementRequester(t, f)
	ttA := settlementTicketType(t, f, "typeA", false)
	settlementContract(t, f, MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)})

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(time.Hour)

	mkV := func(ticket *ent.Ticket, layer, state string) {
		b := f.client.SLAViolation.Create().
			SetSLADefinitionID(f.def.ID).SetTicketID(ticket.ID).
			SetViolationType("response_time").SetSeverity("high").
			SetTenantID(f.customer.ID).SetMatchedLayer(layer)
		if state != "" {
			b = b.SetResolutionState(state)
		}
		_ = b.SaveX(f.ctx)
	}

	// 4 张合同基准违约的工单（未响应，已过截止）
	t1 := settlementTicket(t, f, req.ID, "STL-F1", ttA.ID, 5*time.Hour)
	t2 := settlementTicket(t, f, req.ID, "STL-F2", ttA.ID, 5*time.Hour)
	t3 := settlementTicket(t, f, req.ID, "STL-F3", ttA.ID, 5*time.Hour)
	t4 := settlementTicket(t, f, req.ID, "STL-F4", ttA.ID, 5*time.Hour)

	mkV(t1, "L3a", "accepted") // 客户内部域违约：不进结算面（即使 accepted）
	mkV(t2, "L1", "waived")    // 合同域豁免：不计违约
	mkV(t3, "L1", "disputed")  // 未裁决争议：不计违约
	mkV(t4, "", "pending")     // 存量空层级：交重算兜底，计违约

	report, err := f.svc.GetSettlementReport(f.ctx, f.provider.ID, f.customer.ID, from, to)
	require.NoError(t, err)

	assert.Equal(t, 4, report.ContractBreached, "合同基准重算违约=4")
	assert.Equal(t, 1, report.ExcludedInternalViolations, "L3a 违约被域过滤排除")
	assert.Equal(t, 0, report.AcceptedViolations, "L3a accepted 不进结算面")
	assert.Equal(t, 1, report.WaivedViolations)
	assert.Equal(t, 1, report.DisputedViolations)
	assert.Equal(t, 1, report.PendingViolations)
	assert.Equal(t, 2, report.SettlementBreaches, "计违约=t1(域过滤后重算兜底)+t4")

	r2 := findSettlementRow(report, t2.ID)
	require.NotNil(t, r2)
	assert.Equal(t, "waived", r2.ExcludedReason)
	r3 := findSettlementRow(report, t3.ID)
	require.NotNil(t, r3)
	assert.Equal(t, "disputed", r3.ExcludedReason)
}

// 归属校验先于取数：非归属 provider 查询 → Forbidden（响应面由 handler 收敛为无计数的 403，SLA-A6）。
func TestSettlementReport_OwnershipForbiddenBeforeFetch(t *testing.T) {
	f := setupSLAGrantFixture(t)
	ttA := settlementTicketType(t, f, "typeA", false)
	settlementContract(t, f, MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)})

	_, err := f.svc.GetSettlementReport(f.ctx, f.other.ID, f.customer.ID, time.Now().Add(-24*time.Hour), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不属于当前服务商")

	var be *common.BusinessError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, common.ForbiddenCode, be.Code)
}

// 出快照锁定（§13.3）：合同域违约 settled_flag=true；客户内部域不动；幂等。
func TestSettlementReport_LockSnapshot(t *testing.T) {
	f := setupSLAGrantFixture(t)
	req := settlementRequester(t, f)
	ttA := settlementTicketType(t, f, "typeA", false)
	settlementContract(t, f, MSPContractSLABinding{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)})

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(time.Hour)

	tk := settlementTicket(t, f, req.ID, "STL-L1", ttA.ID, 5*time.Hour)
	v1 := f.client.SLAViolation.Create().
		SetSLADefinitionID(f.def.ID).SetTicketID(tk.ID).
		SetViolationType("response_time").SetSeverity("high").
		SetTenantID(f.customer.ID).SetMatchedLayer("L1").SaveX(f.ctx)
	v2 := f.client.SLAViolation.Create().
		SetSLADefinitionID(f.def.ID).SetTicketID(tk.ID).
		SetViolationType("response_time").SetSeverity("high").
		SetTenantID(f.customer.ID).SetMatchedLayer("L3a").SaveX(f.ctx)

	locked, err := f.svc.LockSettlementSnapshot(f.ctx, f.provider.ID, f.customer.ID, from, to)
	require.NoError(t, err)
	assert.Equal(t, 1, locked, "仅合同域违约锁定")

	assert.True(t, f.client.SLAViolation.GetX(f.ctx, v1.ID).SettledFlag, "L1 违约被锁定")
	assert.False(t, f.client.SLAViolation.GetX(f.ctx, v2.ID).SettledFlag, "L3a 违约不属结算快照范围")

	locked2, err := f.svc.LockSettlementSnapshot(f.ctx, f.provider.ID, f.customer.ID, from, to)
	require.NoError(t, err)
	assert.Equal(t, 0, locked2, "幂等：已锁定行不重复计数")
}

// 信用额：penalties.credit_rate × 计违约数，受 cap 约束（§13.3）。
func TestSettlementReport_CreditCalculation(t *testing.T) {
	f := setupSLAGrantFixture(t)
	req := settlementRequester(t, f)
	ttA := settlementTicketType(t, f, "typeA", false)

	c := &MSPContract{
		ProviderTenantID: f.provider.ID,
		CustomerTenantID: f.customer.ID,
		ContractNo:       "HT-STL-" + slaUniqueID(),
		Name:             "信用额合同",
		Status:           "active",
		TenantID:         f.provider.ID,
		EffectiveFrom:    time.Now().Add(-48 * time.Hour),
		Currency:         "CNY",
		Penalties:        map[string]interface{}{"metric": "sla_breach", "credit_rate": 100.0, "cap": 150.0},
		Bindings: []MSPContractSLABinding{
			{SLADefinitionID: f.def.ID, TicketTypeID: int64(ttA.ID)},
		},
	}
	_, err := f.svc.CreateContract(f.ctx, c)
	require.NoError(t, err)

	// 2 张违约 + 1 张达成 → 取 min(cap, 2×100)
	settlementTicket(t, f, req.ID, "STL-C1", ttA.ID, 5*time.Hour)
	settlementTicket(t, f, req.ID, "STL-C2", ttA.ID, 5*time.Hour)
	t3 := settlementTicket(t, f, req.ID, "STL-C3", ttA.ID, 5*time.Hour)
	f.client.Ticket.UpdateOneID(t3.ID).
		SetFirstResponseAt(t3.CreatedAt.Add(10 * time.Minute)).
		SetResolvedAt(t3.CreatedAt.Add(100 * time.Minute)).
		ExecX(f.ctx)

	report, err := f.svc.GetSettlementReport(f.ctx, f.provider.ID, f.customer.ID, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 2, report.SettlementBreaches)
	assert.Equal(t, 100.0, report.Credit.CreditRate)
	assert.Equal(t, 150.0, report.Credit.Cap)
	assert.Equal(t, 150.0, report.Credit.Amount, "2×100=200 受 cap=150 约束")
	assert.Equal(t, "CNY", report.Credit.Currency)
}

// 报告期内无生效合同 → NotFound 业务错误（handler 映射 404，fail-loud）。
func TestSettlementReport_ContractMissing(t *testing.T) {
	f := setupSLAGrantFixture(t)

	_, err := f.svc.GetSettlementReport(f.ctx, f.provider.ID, f.customer.ID, time.Now().Add(-24*time.Hour), time.Now())
	require.Error(t, err)

	var be *common.BusinessError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, common.NotFoundCode, be.Code)
	assert.Contains(t, err.Error(), "没有生效合同")
}
