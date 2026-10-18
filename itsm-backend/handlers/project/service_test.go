package project

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testDBCounter int64

// newTestEnv 每个测试独立内存库（对齐 service 包 uniqueTestDSN 先例，避免共享缓存串库）。
func newTestEnv(t *testing.T) (*Service, *ent.Client, int) {
	t.Helper()
	dsn := fmt.Sprintf("file:project_p0_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&testDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenant, err := client.Tenant.Create().
		SetName("P0 测试租户").
		SetCode(fmt.Sprintf("P0T%d", atomic.AddInt64(&testDBCounter, 1))).
		SetStatus("active").
		Save(context.Background())
	require.NoError(t, err)

	return NewService(NewEntRepository(client)), client, tenant.ID
}

// TestServiceCreate_DefaultsAndAutoCode 创建：planning 起步 + 自动编号 + 重复 code 拒绝 + 非法类型拒绝。
func TestServiceCreate_DefaultsAndAutoCode(t *testing.T) {
	svc, _, tid := newTestEnv(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "核心系统上线"})
	require.NoError(t, err)
	assert.Equal(t, StatusPlanning, created.Status)
	assert.Equal(t, "internal", created.Type)
	assert.True(t, strings.HasPrefix(created.Code, "PRJ-"), "auto code got %q", created.Code)

	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "固定编号", Code: "PRJ-FIX-001"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "重复编号", Code: "PRJ-FIX-001"})
	require.ErrorIs(t, err, ErrCodeExists)

	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "非法类型", Type: "bogus"})
	require.ErrorIs(t, err, ErrValidation)
}

// TestServiceTransition_LifecycleAndClosureGate 状态机全链路 + 结项门禁 + CAS 冲突。
func TestServiceTransition_LifecycleAndClosureGate(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	manager := seedUserWithMembership(t, client, tid, "gatepm", "home", nil)
	managerID := manager.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "搬迁项目", ManagerID: &managerID})
	require.NoError(t, err)

	// PE-12 立项门：未提交/未通过前 activate 被拒（409 语义）。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	var gateBlocked *GateBlockedError
	require.ErrorAs(t, err, &gateBlocked)
	assert.Equal(t, GateInitiation, gateBlocked.Gate)
	require.NotEmpty(t, gateBlocked.Blockers)

	// 门禁规则：无成员 → 提交被门禁阻塞（记录 blockers）；登记成员 + 任命经理后可自动通过。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.ErrorAs(t, err, &gateBlocked)
	assert.Equal(t, "member_missing", gateBlocked.Blockers[0].Type)

	_, err = svc.AddMember(ctx, tid, p.ID, manager.ID, &MemberAddRequest{UserID: manager.ID, ProjectRole: "pm"})
	require.NoError(t, err)
	initiation, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateInitiation, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, initiation.Status)

	activated, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionActivate})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, activated.Status)
	assert.False(t, activated.ActualStart.IsZero(), "activate 应回填实际开始时间")

	suspended, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionSuspend})
	require.NoError(t, err)
	assert.Equal(t, StatusSuspended, suspended.Status)

	resumed, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionResume})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, resumed.Status)

	// 非法流转：active 不可再 resume。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionResume})
	require.ErrorIs(t, err, ErrInvalidTransition)

	// 结项门禁：存在未终结任务 → 409 语义错误（ClosureBlockedError）。
	task, err := client.ProjectTask.Create().
		SetTenantID(tid).
		SetProjectID(p.ID).
		SetName("上线演练").
		Save(ctx)
	require.NoError(t, err)

	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	var blocked *ClosureBlockedError
	require.ErrorAs(t, err, &blocked)
	require.NotEmpty(t, blocked.Blockers)
	assert.Equal(t, "task", blocked.Blockers[0].Type)

	// 任务终结后可结项，且回填 closed_at。
	_, err = client.ProjectTask.UpdateOneID(task.ID).SetStatus("done").Save(ctx)
	require.NoError(t, err)

	// PE-12 结项门：任务清零后仍要求四道门链路（plan：任务+里程碑 → acceptance：全部达成 → closure）。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	require.ErrorAs(t, err, &gateBlocked)
	assert.Equal(t, GateClosure, gateBlocked.Gate)

	// plan 门：缺里程碑 → 阻塞。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.ErrorAs(t, err, &gateBlocked)
	assert.Equal(t, "milestone_missing", gateBlocked.Blockers[0].Type)

	due := time.Now().Add(72 * time.Hour)
	milestone, err := svc.CreateMilestone(ctx, tid, p.ID, manager.ID, &MilestoneCreateRequest{Name: "验收通过", DueDate: &due})
	require.NoError(t, err)
	planGate, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GatePlan, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, planGate.Status)
	// 计划门通过 → 自动冻结基线 v1（不可变）。
	if _, err := svc.repo.LatestBaseline(ctx, tid, p.ID); err != nil {
		t.Fatalf("计划门通过后应存在基线: %v", err)
	}

	// acceptance 门：里程碑未达成 → 阻塞；达成后通过。
	_, err = svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.ErrorAs(t, err, &gateBlocked)
	assert.Equal(t, "milestone_open", gateBlocked.Blockers[0].Type)
	_, err = svc.TransitionMilestone(ctx, tid, p.ID, milestone.ID, &MilestoneStatusRequest{Action: MilestoneActionReach})
	require.NoError(t, err)
	acceptance, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateAcceptance, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, acceptance.Status)

	closureGate, err := svc.SubmitGate(ctx, tid, p.ID, manager.ID, GateClosure, "pm", true)
	require.NoError(t, err)
	assert.Equal(t, GateStatusPassed, closureGate.Status)

	// PE-15 结项清单：复盘草稿 + 资产登记 + 成员回收齐备后方可结项。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	var checklistBlocked *ClosureBlockedError
	require.ErrorAs(t, err, &checklistBlocked)
	assert.NotEmpty(t, checklistBlocked.Blockers)
	fulfillClosureChecklist(t, svc, tid, p.ID, manager.ID)

	completed, err := svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionComplete})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, completed.Status)
	assert.Equal(t, 100, completed.Progress, "结项后进度必须收敛为 100%")
	assert.False(t, completed.ClosedAt.IsZero(), "complete 应回填结项时间")

	// CAS 冲突：携带过期版本更新。
	wrong := completed.Version + 100
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{Name: strPtr("改名"), Version: &wrong})
	var conflict *CASConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Greater(t, conflict.CurrentVersion, 0)
}

// TestServiceDelete_GateAndSoftDelete 删除门禁 + 软删后不可见。
func TestServiceDelete_GateAndSoftDelete(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "待删项目"})
	require.NoError(t, err)

	requester, err := client.User.Create().
		SetUsername(fmt.Sprintf("p0-user-%d", p.ID)).
		SetEmail(fmt.Sprintf("p0-user-%d@example.com", p.ID)).
		SetName("P0 用户").
		SetPasswordHash("x").
		SetTenantID(tid).
		Save(ctx)
	require.NoError(t, err)

	ticket, err := client.Ticket.Create().
		SetTenantID(tid).
		SetTitle("关联工单").
		SetTicketNumber(fmt.Sprintf("TK-P0-%d", p.ID)).
		SetRequesterID(requester.ID).
		SetStatus("open").
		SetProjectID(p.ID).
		Save(ctx)
	require.NoError(t, err)

	err = svc.Delete(ctx, tid, p.ID)
	var blocked *DeleteBlockedError
	require.ErrorAs(t, err, &blocked)

	_, err = client.Ticket.UpdateOneID(ticket.ID).ClearProjectID().Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, tid, p.ID))
	_, err = svc.Get(ctx, tid, p.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func strPtr(s string) *string { return &s }
