package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/common"
	"itsm-backend/ent/auditlog"
	"itsm-backend/handlers/common/datascope"
)

// 跨租户单据流转 P2-③ 写面首批 / IP-P2-13：问题状态推进（授权链 + 审计 + 平移一致性）。

type stubProblemWriter struct {
	calls          int
	lastTenantID   int
	lastID         int
	lastActorID    int
	lastActorRole  string
	lastStatus     string
	lastResolution string
	err            error
	mutate         func(ctx context.Context, id int, status string)
}

func (w *stubProblemWriter) UpdateProblemStatus(ctx context.Context, tenantID, id, actorID int, actorRole, status, resolution string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID = tenantID, id, actorID
	w.lastActorRole, w.lastStatus, w.lastResolution = actorRole, status, resolution
	if w.err != nil {
		return w.err
	}
	if w.mutate != nil {
		w.mutate(ctx, id, status)
	}
	return nil
}

func problemIDOf(t *testing.T, env *recordProjEnv) int {
	t.Helper()
	row, err := env.client.Problem.Query().Only(context.Background())
	require.NoError(t, err)
	return row.ID
}

func TestMSPProblemWrite_SuccessChainAndAudit(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubProblemWriter{mutate: func(ctx context.Context, id int, status string) {
		require.NoError(t, env.client.Problem.UpdateOneID(id).SetStatus(status).Exec(ctx))
	}}
	svc.SetProblemStatusWriter(stub)

	resp, err := svc.UpdateProblemStatus(context.Background(), env.actor, problemIDOf(t, env), env.custA.ID, "identified", "临时规避")
	require.NoError(t, err)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, env.custA.ID, stub.lastTenantID)
	assert.Equal(t, env.userP.ID, stub.lastActorID)
	assert.Equal(t, "admin", stub.lastActorRole, "域守卫用家租户 users.role（深切换语义）")
	assert.Equal(t, "identified", stub.lastStatus)
	assert.Equal(t, "临时规避", stub.lastResolution)
	assert.Equal(t, "identified", resp.Status)
	require.Len(t, resp.AllowedActions, 2)
	assert.Equal(t, "status", resp.AllowedActions[1].Action)
	assert.True(t, resp.AllowedActions[1].Allowed)

	// 审计 workbench.action（resource=problem，outcome=success）。
	rows, err := env.client.AuditLog.Query().
		Where(auditlog.ActionEQ("workbench.action"), auditlog.ResourceEQ("problem"), auditlog.TenantIDEQ(env.provider.ID)).
		All(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1)
	require.NotNil(t, rows[len(rows)-1].RequestBody)
	assert.Contains(t, *rows[len(rows)-1].RequestBody, `"outcome":"success"`)
	assert.Contains(t, *rows[len(rows)-1].RequestBody, `"op":"problem.status"`)
}

func TestMSPProblemWrite_GuardChainFailClosed(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubProblemWriter{}
	svc.SetProblemStatusWriter(stub)
	ctx := context.Background()
	pid := problemIDOf(t, env)

	// 未分配客户（问题在 C 租户）。
	cProblem, err := env.client.Problem.Create().
		SetProblemNumber("PRB-C").SetTitle("C 问题").SetStatus("open").
		SetCreatedBy(env.userA.ID).SetTenantID(env.custC.ID).Save(ctx)
	require.NoError(t, err)
	_, err = svc.UpdateProblemStatus(ctx, env.actor, cProblem.ID, env.custC.ID, "investigating", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	assert.Zero(t, stub.calls)

	// 声明租户与资源租户不一致 → 400。
	_, err = svc.UpdateProblemStatus(ctx, env.actor, pid, env.custB.ID, "investigating", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeResourceTenantMismatch, ae.Code)
	assert.Zero(t, stub.calls)

	// 未知目标状态 → 拒绝。
	_, err = svc.UpdateProblemStatus(ctx, env.actor, pid, env.custA.ID, "open", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)

	// 客户租户暂停 → 403 CUSTOMER_INACTIVE。
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("suspended").Exec(ctx))
	_, err = svc.UpdateProblemStatus(ctx, env.actor, pid, env.custA.ID, "identified", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerInactive, ae.Code)
	assert.Zero(t, stub.calls)
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("active").Exec(ctx))

	// provider 侧写门禁：未知 MSP 角色回落 msp_viewer（无 msp_ticket:write）→ 403。
	viewer := env.actor
	viewer.MSPRole = "provider_observer"
	_, err = svc.UpdateProblemStatus(ctx, viewer, pid, env.custA.ID, "identified", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)

	// 终态 closed 不可再流转 → 403。
	require.NoError(t, env.client.Problem.UpdateOneID(pid).SetStatus("closed").Exec(ctx))
	_, err = svc.UpdateProblemStatus(ctx, env.actor, pid, env.custA.ID, "investigating", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)
}

func TestMSPProblemWrite_DomainErrorTranslation(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	pid := problemIDOf(t, env)

	// 域状态机拒绝（BusinessError 409）→ INVALID_TRANSITION。
	stub := &stubProblemWriter{err: common.NewBusinessError(common.ConflictCode, "当前问题状态不允许此操作", "investigating -> closed")}
	svc.SetProblemStatusWriter(stub)
	_, err := svc.UpdateProblemStatus(context.Background(), env.actor, pid, env.custA.ID, "resolved", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeInvalidTransition, ae.Code)

	// 域行级拒绝（AppError 403）→ ACTION_NOT_ALLOWED。
	stub = &stubProblemWriter{err: common.NewForbiddenError("无权限修改该问题单")}
	svc.SetProblemStatusWriter(stub)
	_, err = svc.UpdateProblemStatus(context.Background(), env.actor, pid, env.custA.ID, "resolved", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)

	// 未识别错误 → 原样上抛（500 语义，不伪装）。
	stub = &stubProblemWriter{err: errors.New("boom")}
	svc.SetProblemStatusWriter(stub)
	_, err = svc.UpdateProblemStatus(context.Background(), env.actor, pid, env.custA.ID, "resolved", "")
	require.Error(t, err)
	_, ok = AsCustomerAccessError(err)
	assert.False(t, ok)
}

// TestCanWriteRecord_ParityWithDataScope 锁定 service 层平移口径与 handlers/common/datascope 一致（防漂移）。
func TestCanWriteRecord_ParityWithDataScope(t *testing.T) {
	cases := []struct {
		actorID  int
		role     string
		ownerID  int
		assignee int
	}{
		{7, "admin", 1, 2}, {7, "super_admin", 1, 0}, {7, "manager", 0, 0}, {7, "sysadmin", 0, 0},
		{7, "agent", 7, 0}, {7, "technician", 0, 7}, {7, "agent", 1, 2}, {7, "agent", 0, 0},
		{0, "agent", 0, 0}, {0, "admin", 0, 0}, {7, "", 7, 0}, {7, "end_user", 9, 9},
	}
	for _, tc := range cases {
		var assigneePtr *int
		if tc.assignee > 0 {
			v := tc.assignee
			assigneePtr = &v
		}
		assert.Equal(t,
			datascope.CanWriteResource(tc.actorID, tc.role, tc.ownerID, assigneePtr),
			canWriteRecord(tc.actorID, tc.role, tc.ownerID, tc.assignee),
			"case actor=%d role=%s owner=%d assignee=%d", tc.actorID, tc.role, tc.ownerID, tc.assignee)
	}
}

// TestMSPProblemAllowedActions_Matrix 动作契约矩阵（与写路径前置校验同口径）。
func TestMSPProblemAllowedActions_Matrix(t *testing.T) {
	actor := MSPWorkbenchActor{UserID: 7}
	row := mspRecordRow{status: "investigating", ownerID: 7, assigneeID: 0}

	assert.True(t, problemAllowedActions(actor, true, true, "agent", row)[1].Allowed, "owner 可写")
	assert.False(t, problemAllowedActions(actor, false, true, "agent", row)[1].Allowed, "暂停禁写")
	assert.False(t, problemAllowedActions(actor, true, false, "agent", row)[1].Allowed, "无 RBAC 禁写")
	assert.False(t, problemAllowedActions(actor, true, true, "agent", mspRecordRow{status: "closed", ownerID: 7})[1].Allowed, "终态禁写")
	assert.False(t, problemAllowedActions(actor, true, true, "agent", mspRecordRow{status: "open", ownerID: 1, assigneeID: 2})[1].Allowed, "非 owner/assignee 禁写")
	assert.True(t, problemAllowedActions(actor, true, true, "admin", mspRecordRow{status: "open", ownerID: 1})[1].Allowed, "管理员可写")
	assert.False(t, problemAllowedActions(actor, true, false, "agent", row)[1].Allowed)
}
