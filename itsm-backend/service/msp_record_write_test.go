package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	entChange "itsm-backend/ent/change"
	"itsm-backend/handlers/common/datascope"
	"itsm-backend/middleware"
)

// 跨租户单据流转 P2-③ 写面首批 / IP-P2-13：问题状态推进（授权链 + 审计 + 平移一致性）。

// seedTenantRolePermissions 在目标租户内建角色 + 权限 + 授权（sqlite 夹具；C2 基线同构最小子集）。
// 生产语义：权限只在目标租户内计算（canon I2/I5），故写门禁用例必须落 DB 行（DB-only 亦可通过）。
func seedTenantRolePermissions(t *testing.T, client *ent.Client, tenantID int, roleCode string, codes ...string) {
	t.Helper()
	ctx := context.Background()
	role, err := client.Role.Create().SetCode(roleCode).SetName(roleCode).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	for _, code := range codes {
		parts := strings.SplitN(code, ":", 2)
		require.Len(t, parts, 2, "permission code must be resource:action")
		perm, err := client.Permission.Create().
			SetCode(code).SetName(code).SetResource(parts[0]).SetAction(parts[1]).SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
		_, err = client.RolePermission.Create().
			SetRoleID(role.ID).SetPermissionID(perm.ID).SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
	}
}

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

	// 撤掉目标租户 problem:write 授权 → 门禁拒绝（证明门禁在目标租户内计算，非 provider 侧）。
	_, err = env.client.RolePermission.Delete().Exec(context.Background())
	require.NoError(t, err)
	middleware.InvalidateAllPermissionCaches()
	_, err = svc.UpdateProblemStatus(context.Background(), env.actor, problemIDOf(t, env), env.custA.ID, "investigating", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
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

// ---- IP-P2-15：变更非审批流转 / 服务请求履约状态（写面扩展） ----

type stubChangeWriter struct {
	calls         int
	lastTenantID  int
	lastID        int
	lastActorID   int
	lastActorRole string
	lastTarget    string
	lastComment   string
	err           error
	mutate        func(ctx context.Context, id int, status string)
}

func (w *stubChangeWriter) TransitionChangeStatus(ctx context.Context, tenantID, id, actorID int, targetStatus, comment, actorRole string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID = tenantID, id, actorID
	w.lastTarget, w.lastComment, w.lastActorRole = targetStatus, comment, actorRole
	if w.err != nil {
		return w.err
	}
	if w.mutate != nil {
		w.mutate(ctx, id, targetStatus)
	}
	return nil
}

type stubRequestWriter struct {
	calls         int
	lastTenantID  int
	lastID        int
	lastActorID   int
	lastActorRole string
	lastStatus    string
	err           error
	mutate        func(ctx context.Context, id int, status string)
}

func (w *stubRequestWriter) UpdateRequestStatus(ctx context.Context, tenantID, id, actorID int, actorRole, status string) error {
	w.calls++
	w.lastTenantID, w.lastID, w.lastActorID = tenantID, id, actorID
	w.lastActorRole, w.lastStatus = actorRole, status
	if w.err != nil {
		return w.err
	}
	if w.mutate != nil {
		w.mutate(ctx, id, status)
	}
	return nil
}

func changeIDByNumber(t *testing.T, env *recordProjEnv, number string) int {
	t.Helper()
	row, err := env.client.Change.Query().Where(entChange.ChangeNumberEQ(number)).Only(context.Background())
	require.NoError(t, err)
	return row.ID
}

func serviceRequestIDOf(t *testing.T, env *recordProjEnv) int {
	t.Helper()
	row, err := env.client.ServiceRequest.Query().Only(context.Background())
	require.NoError(t, err)
	return row.ID
}

func TestMSPChangeWrite_SuccessChainAndAudit(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubChangeWriter{mutate: func(ctx context.Context, id int, status string) {
		require.NoError(t, env.client.Change.UpdateOneID(id).SetStatus(status).Exec(ctx))
	}}
	svc.SetChangeStatusWriter(stub)

	// CHG-2：approved → scheduled（D8 非审批流转）。
	cid := changeIDByNumber(t, env, "CHG-2")
	resp, err := svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, "scheduled", "排期至周五")
	require.NoError(t, err)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, env.custA.ID, stub.lastTenantID)
	assert.Equal(t, env.userP.ID, stub.lastActorID)
	assert.Equal(t, "admin", stub.lastActorRole, "域守卫用家租户 users.role（深切换语义）")
	assert.Equal(t, "scheduled", stub.lastTarget)
	assert.Equal(t, "排期至周五", stub.lastComment)
	assert.Equal(t, "scheduled", resp.Status)
	require.Len(t, resp.AllowedActions, 2)
	assert.Equal(t, "status", resp.AllowedActions[1].Action)
	assert.True(t, resp.AllowedActions[1].Allowed)

	// 链式推进：scheduled → in_progress → completed → closed。
	for _, step := range []string{"in_progress", "completed", "closed"} {
		_, err = svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, step, "")
		require.NoError(t, err, "推进 %s", step)
	}
	assert.Equal(t, 4, stub.calls)
	// 终态后动作收回（closed → status.allowed=false）。
	detail, err := svc.GetRecord(context.Background(), env.actor, MSPRecordChange, cid, env.custA.ID)
	require.NoError(t, err)
	assert.Equal(t, "closed", detail.Status)
	assert.False(t, detail.AllowedActions[1].Allowed, "终态动作必须收回")

	// 审计 workbench.action（resource=change，op=change.status，outcome=success）。
	rows, err := env.client.AuditLog.Query().
		Where(auditlog.ActionEQ("workbench.action"), auditlog.ResourceEQ("change"), auditlog.TenantIDEQ(env.provider.ID)).
		All(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1)
	last := rows[len(rows)-1]
	require.NotNil(t, last.RequestBody)
	assert.Contains(t, *last.RequestBody, `"outcome":"success"`)
	assert.Contains(t, *last.RequestBody, `"op":"change.status"`)
	assert.Contains(t, last.Path, "/api/v1/msp/changes/", "审计路径按记录类型 slug（changes）")

	// 撤掉目标租户 change:write 授权 → 门禁拒绝（证明门禁在目标租户内计算，非 provider 侧）。
	require.NoError(t, env.client.Change.UpdateOneID(cid).SetStatus("approved").Exec(context.Background()))
	_, err = env.client.RolePermission.Delete().Exec(context.Background())
	require.NoError(t, err)
	middleware.InvalidateAllPermissionCaches()
	_, err = svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
}

func TestMSPChangeWrite_GuardChainFailClosed(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubChangeWriter{}
	svc.SetChangeStatusWriter(stub)
	ctx := context.Background()
	cid := changeIDByNumber(t, env, "CHG-1")

	// 未分配客户（变更在 C 租户）→ 403 MSP_ALLOCATION_REQUIRED。
	_, err := svc.UpdateChangeStatus(ctx, env.actor, changeIDByNumber(t, env, "CHG-4"), env.custC.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	assert.Zero(t, stub.calls)

	// 声明租户与资源租户不一致 → 400。
	_, err = svc.UpdateChangeStatus(ctx, env.actor, cid, env.custB.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeResourceTenantMismatch, ae.Code)
	assert.Zero(t, stub.calls)

	// 审批类/授权类状态不在 MSP 面（D8：approve/reject/rollback 保留客户治理面）→ 拒绝。
	for _, forbidden := range []string{"approved", "rejected", "rolled_back"} {
		_, err = svc.UpdateChangeStatus(ctx, env.actor, cid, env.custA.ID, forbidden, "")
		require.Error(t, err, "目标 %s 必须被拒", forbidden)
		ae, ok = AsCustomerAccessError(err)
		require.True(t, ok)
		assert.Equal(t, CodeActionNotAllowed, ae.Code)
	}
	assert.Zero(t, stub.calls)

	// 客户租户暂停 → 403 CUSTOMER_INACTIVE。
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("suspended").Exec(ctx))
	_, err = svc.UpdateChangeStatus(ctx, env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerInactive, ae.Code)
	assert.Zero(t, stub.calls)
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("active").Exec(ctx))

	// provider 侧写门禁：未知 MSP 角色回落（无目标租户 change:write）→ 403。
	viewer := env.actor
	viewer.MSPRole = "provider_observer"
	_, err = svc.UpdateChangeStatus(ctx, viewer, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)

	// 终态（closed/cancelled/rejected/rolled_back）不可再流转 → 403。
	for _, terminal := range []string{"closed", "cancelled", "rejected", "rolled_back"} {
		require.NoError(t, env.client.Change.UpdateOneID(cid).SetStatus(terminal).Exec(ctx))
		_, err = svc.UpdateChangeStatus(ctx, env.actor, cid, env.custA.ID, "scheduled", "")
		require.Error(t, err, "终态 %s 必须被拒", terminal)
		ae, ok = AsCustomerAccessError(err)
		require.True(t, ok)
		assert.Equal(t, CodeActionNotAllowed, ae.Code)
	}
	assert.Zero(t, stub.calls)

	// 未配置写入器 → fail-closed。
	svc.SetChangeStatusWriter(nil)
	require.NoError(t, env.client.Change.UpdateOneID(cid).SetStatus("approved").Exec(ctx))
	_, err = svc.UpdateChangeStatus(ctx, env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	_, ok = AsCustomerAccessError(err)
	assert.False(t, ok, "内部配置错误原样上抛（不伪装 4xx）")
}

func TestMSPChangeWrite_DomainErrorTranslation(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	cid := changeIDByNumber(t, env, "CHG-1")

	stub := &stubChangeWriter{err: common.NewBusinessError(common.ConflictCode, "状态机拒绝", "submitted -> scheduled")}
	svc.SetChangeStatusWriter(stub)
	_, err := svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeInvalidTransition, ae.Code)

	stub = &stubChangeWriter{err: common.NewForbiddenError("无权限操作该变更单：仅创建人、受理人或管理员可执行状态流转")}
	svc.SetChangeStatusWriter(stub)
	_, err = svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)

	stub = &stubChangeWriter{err: errors.New("boom")}
	svc.SetChangeStatusWriter(stub)
	_, err = svc.UpdateChangeStatus(context.Background(), env.actor, cid, env.custA.ID, "scheduled", "")
	require.Error(t, err)
	_, ok = AsCustomerAccessError(err)
	assert.False(t, ok)
}

func TestMSPRequestWrite_SuccessChainAndAudit(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubRequestWriter{mutate: func(ctx context.Context, id int, status string) {
		require.NoError(t, env.client.ServiceRequest.UpdateOneID(id).SetStatus(status).Exec(ctx))
	}}
	svc.SetRequestStatusWriter(stub)
	ctx := context.Background()

	sid := serviceRequestIDOf(t, env)
	// 夹具初始 approved（审批中）→ 先置 security_approved（客户审批完成后进入履约窗口）。
	require.NoError(t, env.client.ServiceRequest.UpdateOneID(sid).SetStatus("security_approved").Exec(ctx))

	resp, err := svc.UpdateRequestStatus(ctx, env.actor, sid, env.custA.ID, "provisioning")
	require.NoError(t, err)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, env.custA.ID, stub.lastTenantID)
	assert.Equal(t, env.userP.ID, stub.lastActorID)
	assert.Equal(t, "admin", stub.lastActorRole, "域守卫用家租户 users.role（深切换语义）")
	assert.Equal(t, "provisioning", stub.lastStatus)
	assert.Equal(t, "provisioning", resp.Status)
	require.Len(t, resp.AllowedActions, 2)
	assert.Equal(t, "status", resp.AllowedActions[1].Action)
	assert.True(t, resp.AllowedActions[1].Allowed)

	// 链式：provisioning → delivered（终态履约窗口外 → 动作收回）。
	_, err = svc.UpdateRequestStatus(ctx, env.actor, sid, env.custA.ID, "delivered")
	require.NoError(t, err)
	assert.Equal(t, 2, stub.calls)
	detail, err := svc.GetRecord(ctx, env.actor, MSPRecordServiceRequest, sid, env.custA.ID)
	require.NoError(t, err)
	assert.Equal(t, "delivered", detail.Status)
	assert.False(t, detail.AllowedActions[1].Allowed, "delivered 为履约终态 → 动作收回")

	// 审计 workbench.action（resource=service_request，op=service_request.status）。
	rows, err := env.client.AuditLog.Query().
		Where(auditlog.ActionEQ("workbench.action"), auditlog.ResourceEQ("service_request"), auditlog.TenantIDEQ(env.provider.ID)).
		All(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1)
	last := rows[len(rows)-1]
	require.NotNil(t, last.RequestBody)
	assert.Contains(t, *last.RequestBody, `"outcome":"success"`)
	assert.Contains(t, *last.RequestBody, `"op":"service_request.status"`)
	assert.Contains(t, last.Path, "/api/v1/msp/service-requests/", "审计路径按记录类型 slug（service-requests）")
}

func TestMSPRequestWrite_GuardChainFailClosed(t *testing.T) {
	env := newRecordProjEnv(t)
	svc := env.svc()
	stub := &stubRequestWriter{}
	svc.SetRequestStatusWriter(stub)
	ctx := context.Background()
	sid := serviceRequestIDOf(t, env)

	// 审批类状态不在 MSP 面（approval 保留客户治理面）→ 拒绝。
	for _, forbidden := range []string{"approved", "cancelled", "security_approved"} {
		_, err := svc.UpdateRequestStatus(ctx, env.actor, sid, env.custA.ID, forbidden)
		require.Error(t, err, "目标 %s 必须被拒", forbidden)
		ae, ok := AsCustomerAccessError(err)
		require.True(t, ok)
		assert.Equal(t, CodeActionNotAllowed, ae.Code)
	}
	assert.Zero(t, stub.calls)

	// 履约窗口前置：approved（审批中）不可由 provider 推进 → 403。
	_, err := svc.UpdateRequestStatus(ctx, env.actor, sid, env.custA.ID, "provisioning")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)

	// 未分配客户（C 租户新建 SR）→ 403 MSP_ALLOCATION_REQUIRED。
	cSR, err := env.client.ServiceRequest.Create().
		SetTenantID(env.custC.ID).SetCatalogID(1).SetRequesterID(env.userA.ID).
		SetTitle("C 服务请求").SetStatus("security_approved").SetReason("x").Save(ctx)
	require.NoError(t, err)
	_, err = svc.UpdateRequestStatus(ctx, env.actor, cSR.ID, env.custC.ID, "provisioning")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	assert.Zero(t, stub.calls)

	// 声明租户与资源租户不一致 → 400。
	_, err = svc.UpdateRequestStatus(ctx, env.actor, sid, env.custB.ID, "provisioning")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeResourceTenantMismatch, ae.Code)
	assert.Zero(t, stub.calls)

	// 客户租户暂停 → 403 CUSTOMER_INACTIVE（先进入履约窗口）。
	require.NoError(t, env.client.ServiceRequest.UpdateOneID(sid).SetStatus("security_approved").Exec(ctx))
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("suspended").Exec(ctx))
	_, err = svc.UpdateRequestStatus(ctx, env.actor, sid, env.custA.ID, "provisioning")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerInactive, ae.Code)
	assert.Zero(t, stub.calls)
	require.NoError(t, env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("active").Exec(ctx))

	// provider 侧写门禁：未知 MSP 角色回落 → 403。
	viewer := env.actor
	viewer.MSPRole = "provider_observer"
	_, err = svc.UpdateRequestStatus(ctx, viewer, sid, env.custA.ID, "provisioning")
	require.Error(t, err)
	ae, ok = AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)
	assert.Zero(t, stub.calls)
}

// TestMSPChangeAllowedActions_Matrix 变更动作契约矩阵（与写路径前置校验同口径）。
func TestMSPChangeAllowedActions_Matrix(t *testing.T) {
	actor := MSPWorkbenchActor{UserID: 7}
	row := mspRecordRow{status: "approved", ownerID: 7}

	assert.True(t, changeAllowedActions(actor, true, true, "agent", row)[1].Allowed, "owner 可写")
	assert.False(t, changeAllowedActions(actor, false, true, "agent", row)[1].Allowed, "暂停禁写")
	assert.False(t, changeAllowedActions(actor, true, false, "agent", row)[1].Allowed, "无 change:write 禁写")
	assert.False(t, changeAllowedActions(actor, true, true, "agent", mspRecordRow{status: "closed", ownerID: 7})[1].Allowed, "终态禁写")
	assert.False(t, changeAllowedActions(actor, true, true, "agent", mspRecordRow{status: "rejected", ownerID: 7})[1].Allowed, "驳回终态禁写")
	assert.False(t, changeAllowedActions(actor, true, true, "agent", mspRecordRow{status: "approved", ownerID: 1, assigneeID: 2})[1].Allowed, "非 owner/assignee 禁写")
	assert.True(t, changeAllowedActions(actor, true, true, "admin", mspRecordRow{status: "approved", ownerID: 1})[1].Allowed, "管理员可写")
}

// TestMSPRequestAllowedActions_Matrix 服务请求动作契约矩阵（与写路径前置校验同口径）。
func TestMSPRequestAllowedActions_Matrix(t *testing.T) {
	actor := MSPWorkbenchActor{UserID: 7}
	row := mspRecordRow{status: "security_approved"}

	assert.True(t, requestAllowedActions(actor, true, true, "agent", row)[1].Allowed, "履约窗口内 + 操作人")
	assert.False(t, requestAllowedActions(actor, false, true, "agent", row)[1].Allowed, "暂停禁写")
	assert.False(t, requestAllowedActions(actor, true, false, "agent", row)[1].Allowed, "无 service_request:write 禁写")
	assert.False(t, requestAllowedActions(actor, true, true, "agent", mspRecordRow{status: "approved"})[1].Allowed, "履约窗口外（审批中）")
	assert.False(t, requestAllowedActions(actor, true, true, "end_user", row)[1].Allowed, "非操作人角色")
	assert.True(t, requestAllowedActions(actor, true, true, "technician", row)[1].Allowed)
}

// TestCanOperateServiceRequest_Semantics 锁定与 handlers/service_request.isServiceRequestOperator 同口径
// （service 不可反向依赖 handlers/*；跨包一致性由 handler 层经真实域服务的行为用例背书）。
func TestCanOperateServiceRequest_Semantics(t *testing.T) {
	trueCases := []string{"admin", "super_admin", "agent", "technician", "Admin", " TECHNICIAN "}
	falseCases := []string{"", "user", "end_user", "manager", "sysadmin", "msp_tech", "provider_admin"}
	for _, role := range trueCases {
		assert.True(t, canOperateServiceRequest(role), "role=%q 应为操作人", role)
	}
	for _, role := range falseCases {
		assert.False(t, canOperateServiceRequest(role), "role=%q 不应为操作人", role)
	}
}
