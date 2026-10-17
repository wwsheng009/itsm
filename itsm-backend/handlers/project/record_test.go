package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTicket 建工单（含 requester，满足外键）。
func seedTicket(t *testing.T, client *ent.Client, tenantID int, number, title string) *ent.Ticket {
	t.Helper()
	ctx := context.Background()
	requester, err := client.User.Create().
		SetUsername(fmt.Sprintf("p5-user-%s", number)).
		SetEmail(fmt.Sprintf("p5-%s@example.com", number)).
		SetName("P5 用户").
		SetPasswordHash("x").
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	tk, err := client.Ticket.Create().
		SetTenantID(tenantID).
		SetTitle(title).
		SetTicketNumber(number).
		SetRequesterID(requester.ID).
		SetStatus("open").
		Save(ctx)
	require.NoError(t, err)
	return tk
}

// TestRecordAttachDetachAndList 挂接/解除/聚合读 + 权限与终态门禁。
func TestRecordAttachDetachAndList(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "关联项目"})
	require.NoError(t, err)
	tk1 := seedTicket(t, client, tid, "TK-P5-1", "关联工单1")
	tk2 := seedTicket(t, client, tid, "TK-P5-2", "关联工单2")

	// 非管理角色且无 DB 授权 → 项目写权限缺失（403 语义）。
	require.ErrorIs(t, svc.AttachRecord(ctx, tid, "ticket", tk1.ID, p.ID, []string{"end_user"}), ErrProjectWriteRequired)

	// 管理角色直通；幂等；跨租户/不存在 → 404 语义。
	require.NoError(t, svc.AttachRecord(ctx, tid, "ticket", tk1.ID, p.ID, []string{"admin"}))
	require.NoError(t, svc.AttachRecord(ctx, tid, "ticket", tk2.ID, p.ID, []string{"admin"}))
	require.NoError(t, svc.AttachRecord(ctx, tid, "ticket", tk1.ID, p.ID, []string{"admin"}))
	require.ErrorIs(t, svc.AttachRecord(ctx, tid, "ticket", 999999, p.ID, []string{"admin"}), ErrRecordNotFound)
	require.ErrorIs(t, svc.AttachRecord(ctx, tid, "bogus", tk1.ID, p.ID, []string{"admin"}), ErrRecordDomainInvalid)

	// 聚合读：单域 + 总数。
	items, total, err := svc.ListRecords(ctx, tid, p.ID, 0, true, "ticket", "", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	assert.Equal(t, "ticket", items[0].Type)
	assert.NotEmpty(t, items[0].Number)

	// 终态项目拒绝新挂接。
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionCancel, Reason: "测试取消"})
	require.NoError(t, err)
	tk3 := seedTicket(t, client, tid, "TK-P5-3", "关联工单3")
	require.ErrorIs(t, svc.AttachRecord(ctx, tid, "ticket", tk3.ID, p.ID, []string{"admin"}), ErrValidation)

	// 解除（幂等语义由服务层保证挂接存在性）：解除后聚合减少。
	require.NoError(t, svc.DetachRecord(ctx, tid, "ticket", tk1.ID, []string{"admin"}))
	items, total, err = svc.ListRecords(ctx, tid, p.ID, 0, true, "ticket", "", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, tk2.ID, items[0].ID)
}

// TestTaskTicketGeneration 任务→工单生成/重复/解绑（生成器端口）。
func TestTaskTicketGeneration(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "p5-task", "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "工单生成项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)
	task, err := svc.CreateTask(ctx, tid, p.ID, member.ID, &TaskCreateRequest{Name: "生成工单任务", AssigneeID: member.ID})
	require.NoError(t, err)

	// 未配置生成器 → 503 语义。
	_, err = svc.GenerateTaskTicket(ctx, tid, p.ID, task.ID, member.ID, &TaskTicketRequest{})
	require.ErrorIs(t, err, ErrTicketGeneratorNotConfigured)

	// 注入桩生成器（返回固定工单号；挂接由真实适配器完成，此处仅验证链接回写）。
	svc.SetTicketGenerator(TicketGeneratorFunc(func(ctx context.Context, in TicketGenerateInput) (int, error) {
		return 7788, nil
	}))
	generated, err := svc.GenerateTaskTicket(ctx, tid, p.ID, task.ID, member.ID, &TaskTicketRequest{Title: "来自任务"})
	require.NoError(t, err)
	assert.Equal(t, 7788, generated.LinkedTicketID)

	// 重复生成 → 409 语义；解绑后可重建。
	_, err = svc.GenerateTaskTicket(ctx, tid, p.ID, task.ID, member.ID, &TaskTicketRequest{})
	require.ErrorIs(t, err, ErrTaskTicketExists)
	unbound, err := svc.UnbindTaskTicket(ctx, tid, p.ID, task.ID)
	require.NoError(t, err)
	assert.Zero(t, unbound.LinkedTicketID)
	_, err = svc.UnbindTaskTicket(ctx, tid, p.ID, task.ID)
	require.ErrorIs(t, err, ErrTaskTicketNotLinked)
}

// TestTenantIsolation 跨租户隔离（服务层兜底；RLS 为最后防线）：详情/挂接/任务一律 404。
func TestTenantIsolation(t *testing.T) {
	svc, client, tidA := newTestEnv(t)
	ctx := context.Background()

	tenantB, err := client.Tenant.Create().
		SetName("租户B").
		SetCode(fmt.Sprintf("TB%d", tidA)).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	pB, err := svc.Create(ctx, &CreateRequest{TenantID: tenantB.ID, Name: "B 的项目"})
	require.NoError(t, err)

	// A 租户视角不可见 B 的项目。
	_, err = svc.Get(ctx, tidA, pB.ID)
	require.ErrorIs(t, err, ErrNotFound)

	// A 租户不能把单据挂到 B 租户项目。
	tk := seedTicket(t, client, tidA, fmt.Sprintf("TK-ISO-%d", tidA), "隔离工单")
	require.ErrorIs(t, svc.AttachRecord(ctx, tidA, "ticket", tk.ID, pB.ID, []string{"admin"}), ErrNotFound)

	// A 租户不能在 B 项目下创建任务。
	_, err = svc.CreateTask(ctx, tidA, pB.ID, 0, &TaskCreateRequest{Name: "越权任务"})
	require.ErrorIs(t, err, ErrNotFound)
}

// TestHandlerRecordEndpoints 挂接/聚合读/生成未配置（503）端点级验证。
func TestHandlerRecordEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ticket := seedTicket(t, client, tid, "TK-P5-H1", "接口关联工单")

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "接口关联项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	w, _ := doJSON(t, r, http.MethodPost, fmt.Sprintf("/tickets/%d/project", ticket.ID), map[string]any{"projectId": created.ID})
	require.Equal(t, http.StatusOK, w.Code)

	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/records?type=ticket", created.ID), nil)
	var list struct {
		Items   []RecordItem `json:"items"`
		Records []RecordItem `json:"records"`
		Total   int64        `json:"total"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list.Items, 1)
	assert.Len(t, list.Records, 1, "领域别名 records 应存在")
	assert.Equal(t, int64(1), list.Total)

	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/tickets/%d/project", ticket.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)

	// 任务→工单：handler env 未注入生成器 → 503（映射验证）。
	_, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", created.ID), map[string]any{"name": "任务"})
	var task ProjectTaskResponse
	require.NoError(t, json.Unmarshal(env.Data, &task))
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/tasks/%d/tickets", created.ID, task.ID), map[string]any{})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, common.ServiceUnavailableCode, env.Code)
}
