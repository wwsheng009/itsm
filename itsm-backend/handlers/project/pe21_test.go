package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRiskProblemPort 风险→问题单假端口（PE-21 测试）。
type fakeRiskProblemPort struct {
	id      int
	fail    error
	calls   int
	lastIn  RiskConvertInput
	lastTID int
}

func (f *fakeRiskProblemPort) CreateProblemFromRisk(_ context.Context, tenantID, _ int, in RiskConvertInput) (int, error) {
	f.calls++
	f.lastTID = tenantID
	f.lastIn = in
	if f.fail != nil {
		return 0, f.fail
	}
	return f.id, nil
}

// fakeRiskChangePort 风险→变更单假端口（PE-21 测试）。
type fakeRiskChangePort struct {
	id      int
	fail    error
	calls   int
	lastIn  RiskConvertInput
	lastTID int
}

func (f *fakeRiskChangePort) CreateChangeFromRisk(_ context.Context, tenantID, _ int, in RiskConvertInput) (int, error) {
	f.calls++
	f.lastTID = tenantID
	f.lastIn = in
	if f.fail != nil {
		return 0, f.fail
	}
	return f.id, nil
}

// TestRiskWorkflow 风险/问题台账：CRUD + 词表 + 负责人/工单校验 + 状态机 + 乐观锁 + 可见性（PE-21）。
func TestRiskWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "risk", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "风险项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	// 创建：默认值（kind=risk/level=medium/status=open）。
	r1, err := svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{
		Title: "机房搬迁延期", Level: RiskLevelHigh, Probability: RiskLevelMedium, Impact: RiskLevelHigh, OwnerID: member.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, RiskKindRisk, r1.Kind)
	assert.Equal(t, RiskLevelHigh, r1.Level)
	assert.Equal(t, RiskStatusOpen, r1.Status)
	assert.Equal(t, member.ID, r1.OwnerID)

	// 非法 level / 非成员负责人 / 不存在工单。
	_, err = svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{Title: "X", Level: "critical"})
	require.ErrorIs(t, err, ErrValidation)
	outsider := seedUserWithMembership(t, client, tid, "risk-out", "home", nil)
	_, err = svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{Title: "X", OwnerID: outsider.ID})
	require.ErrorIs(t, err, ErrRiskOwnerNotMember)
	_, err = svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{Title: "X", RelatedTicketID: 99999})
	require.ErrorIs(t, err, ErrRiskTicketNotFound)

	// 有效工单关联（同租户弱引用校验通过）。
	tk, err := client.Ticket.Create().
		SetTitle("关联工单").
		SetTicketNumber(fmt.Sprintf("TK-RISK-%d", tid)).
		SetRequesterID(member.ID).
		SetTenantID(tid).
		Save(ctx)
	require.NoError(t, err)
	r2, err := svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{
		Kind: RiskKindIssue, Title: "服务中断", RelatedTicketID: tk.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, RiskKindIssue, r2.Kind)
	assert.Equal(t, tk.ID, r2.RelatedTicketID)

	// 状态机：close 缺 resolution → 拒绝；mitigate → close → reopen。
	_, err = svc.TransitionRisk(ctx, tid, p.ID, r1.ID, &RiskStatusRequest{Action: RiskActionClose})
	require.ErrorIs(t, err, ErrRiskResolutionRequired)
	mit, err := svc.TransitionRisk(ctx, tid, p.ID, r1.ID, &RiskStatusRequest{Action: RiskActionMitigate})
	require.NoError(t, err)
	assert.Equal(t, RiskStatusMitigating, mit.Status)
	closed, err := svc.TransitionRisk(ctx, tid, p.ID, r1.ID, &RiskStatusRequest{Action: RiskActionClose, Resolution: "已调整计划并复核"})
	require.NoError(t, err)
	assert.Equal(t, RiskStatusClosed, closed.Status)
	assert.Equal(t, "已调整计划并复核", closed.Resolution)
	reopened, err := svc.TransitionRisk(ctx, tid, p.ID, r1.ID, &RiskStatusRequest{Action: RiskActionReopen})
	require.NoError(t, err)
	assert.Equal(t, RiskStatusOpen, reopened.Status)
	// 非法流转：open 不能再 reopen。
	_, err = svc.TransitionRisk(ctx, tid, p.ID, r1.ID, &RiskStatusRequest{Action: RiskActionReopen})
	require.ErrorIs(t, err, ErrInvalidRiskState)

	// 更新 + 乐观锁冲突。
	newTitle := "机房搬迁延期（更新）"
	updated, err := svc.UpdateRisk(ctx, tid, p.ID, r1.ID, &RiskUpdateRequest{Title: &newTitle, Version: &reopened.Version})
	require.NoError(t, err)
	assert.Equal(t, newTitle, updated.Title)
	_, err = svc.UpdateRisk(ctx, tid, p.ID, r1.ID, &RiskUpdateRequest{Title: &newTitle, Version: &reopened.Version})
	var cas *CASConflictError
	require.ErrorAs(t, err, &cas)
	// 无可更新字段。
	_, err = svc.UpdateRisk(ctx, tid, p.ID, r1.ID, &RiskUpdateRequest{})
	require.ErrorIs(t, err, ErrNoUpdateFields)

	// 列表过滤（level/kind）。
	high, err := svc.ListRisks(ctx, tid, p.ID, member.ID, true, "", "", RiskLevelHigh)
	require.NoError(t, err)
	assert.Len(t, high, 1)
	issues, err := svc.ListRisks(ctx, tid, p.ID, member.ID, true, RiskKindIssue, "", "")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	_, err = svc.ListRisks(ctx, tid, p.ID, member.ID, true, "", "bogus", "")
	require.ErrorIs(t, err, ErrValidation)

	// 可见性：非成员统一 404。
	_, err = svc.GetRiskForViewer(ctx, tid, p.ID, r1.ID, outsider.ID, false)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.ListRisks(ctx, tid, p.ID, outsider.ID, false, "", "", "")
	require.ErrorIs(t, err, ErrNotFound)

	// 负责人展示名填充。
	names := svc.RiskOwnerNames(ctx, []*ent.ProjectRisk{r1})
	assert.Equal(t, member.Name, names[member.ID])

	// 软删 + 删除后不可见。
	require.NoError(t, svc.DeleteRisk(ctx, tid, p.ID, r2.ID))
	_, err = svc.GetRiskForViewer(ctx, tid, p.ID, r2.ID, member.ID, false)
	require.ErrorIs(t, err, ErrRiskNotFound)
	require.ErrorIs(t, svc.DeleteRisk(ctx, tid, p.ID, r2.ID), ErrRiskNotFound)
}

// TestRiskConvertWorkflow 转换链路：端口未装配 503 / 成功回写 / 二次 409 / 端口失败 / 终态项目拒绝（PE-21）。
func TestRiskConvertWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "rc", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "转换项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)
	r1, err := svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{Title: "转问题", Level: RiskLevelHigh})
	require.NoError(t, err)

	// 端口未装配 → 503 语义。
	_, _, err = svc.ConvertRisk(ctx, tid, p.ID, r1.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetProblem})
	require.ErrorIs(t, err, ErrRiskPortUnavailable)

	problemPort := &fakeRiskProblemPort{id: 5001}
	changePort := &fakeRiskChangePort{id: 6001}
	svc.SetRiskPorts(problemPort, changePort)

	// 成功转换 → 回写关联 + 端口收到映射入参。
	linked, createdID, err := svc.ConvertRisk(ctx, tid, p.ID, r1.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetProblem})
	require.NoError(t, err)
	assert.Equal(t, 5001, createdID)
	assert.Equal(t, 5001, linked.RelatedProblemID)
	assert.Equal(t, tid, problemPort.lastTID)
	assert.Equal(t, r1.Title, problemPort.lastIn.Title)
	assert.Equal(t, p.Code, problemPort.lastIn.ProjectCode)
	assert.Equal(t, RiskLevelHigh, problemPort.lastIn.Level)

	// 同目标二次转换 → 409 + existingId。
	_, existingID, err := svc.ConvertRisk(ctx, tid, p.ID, r1.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetProblem})
	var converted *RiskAlreadyConvertedError
	require.ErrorAs(t, err, &converted)
	assert.Equal(t, 5001, converted.ExistingID)
	assert.Equal(t, 5001, existingID)

	// 另一目标允许（风险可同时升级问题与变更）。
	linked2, createdID2, err := svc.ConvertRisk(ctx, tid, p.ID, r1.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetChange})
	require.NoError(t, err)
	assert.Equal(t, 6001, createdID2)
	assert.Equal(t, 6001, linked2.RelatedChangeID)
	assert.Equal(t, 5001, linked2.RelatedProblemID, "问题关联保持")

	// 非法目标。
	_, _, err = svc.ConvertRisk(ctx, tid, p.ID, r1.ID, member.ID, &RiskConvertRequest{Target: "ticket"})
	require.ErrorIs(t, err, ErrValidation)

	// 端口失败 → ErrRiskConversionFailed（不写回关联）。
	r2, err := svc.CreateRisk(ctx, tid, p.ID, member.ID, &RiskCreateRequest{Title: "转换失败"})
	require.NoError(t, err)
	svc.SetRiskPorts(&fakeRiskProblemPort{fail: errors.New("boom")}, changePort)
	_, _, err = svc.ConvertRisk(ctx, tid, p.ID, r2.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetProblem})
	require.ErrorIs(t, err, ErrRiskConversionFailed)
	reloaded, err := svc.GetRiskForViewer(ctx, tid, p.ID, r2.ID, member.ID, true)
	require.NoError(t, err)
	assert.Zero(t, reloaded.RelatedProblemID)

	// 终态项目拒绝转换（D-30）。
	_, err = client.Project.UpdateOneID(p.ID).SetStatus(StatusCompleted).Save(ctx)
	require.NoError(t, err)
	_, _, err = svc.ConvertRisk(ctx, tid, p.ID, r2.ID, member.ID, &RiskConvertRequest{Target: ConvertTargetChange})
	require.ErrorIs(t, err, ErrValidation)
}

// TestHandlerRiskEndpoints 端点级：CRUD + 关闭校验 400 + 转换 503（未装配）+ 404（PE-21）。
func TestHandlerRiskEndpoints(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "风险接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 创建（缺 level → 默认 medium）。
	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/risks", created.ID), map[string]any{
		"title": "接口风险", "kind": "risk",
	})
	require.Equal(t, http.StatusOK, w.Code)
	var risk ProjectRiskResponse
	require.NoError(t, json.Unmarshal(env.Data, &risk))
	assert.Equal(t, RiskLevelMedium, risk.Level)
	assert.Equal(t, RiskStatusOpen, risk.Status)

	// 列表 + 详情。
	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/risks", created.ID), nil)
	var list []ProjectRiskResponse
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list, 1)
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/risks/%d", created.ID, risk.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)

	// 关闭缺 resolution → 400；补齐后 200。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/risks/%d/status", created.ID, risk.ID), map[string]any{"action": "close"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/risks/%d/status", created.ID, risk.ID), map[string]any{
		"action": "close", "resolution": "接口关闭",
	})
	require.Equal(t, http.StatusOK, w.Code)
	var closedRisk ProjectRiskResponse
	require.NoError(t, json.Unmarshal(env.Data, &closedRisk))
	assert.Equal(t, RiskStatusClosed, closedRisk.Status)

	// 转换：端口未装配 → 503。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/risks/%d/convert", created.ID, risk.ID), map[string]any{"target": "problem"})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// 非法目标 → 400。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/risks/%d/convert", created.ID, risk.ID), map[string]any{"target": "bogus"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 不存在 → 404；非法 level 过滤器 → 400。
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/risks/%d", created.ID, 999999), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/risks?level=critical", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 删除 + 重复删除 404。
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/risks/%d", created.ID, risk.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/risks/%d", created.ID, risk.ID), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}