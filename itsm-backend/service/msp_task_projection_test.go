package service

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/processtask"
	entTenant "itsm-backend/ent/tenant"
	entUser "itsm-backend/ent/user"
	"itsm-backend/middleware"
	"itsm-backend/service/bpmn"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var taskProjDBCounter int64

type taskProjEnv struct {
	client    *ent.Client
	svc       *MSPTaskProjectionService
	actor     MSPWorkbenchActor
	provider  *ent.Tenant
	custA     *ent.Tenant
	custB     *ent.Tenant
	otherUser *ent.User
	tAssigned *ent.ProcessTask
	tQueue    *ent.ProcessTask
	tOther    *ent.ProcessTask
	tInB      *ent.ProcessTask
}

// newTaskProjEnv 构造任务投影夹具：
// provider（MSP）+ 客户 A（active，actor=primary allocation，msp_tech 客户侧基线含 task:read/update）
// + 客户 B（active，有任务但 actor 未分配）。
func newTaskProjEnv(t *testing.T) *taskProjEnv {
	t.Helper()
	prevMode := middleware.PermissionConfig.Mode
	middleware.PermissionConfig.Mode = middleware.PermissionConfigModeMerge
	t.Cleanup(func() { middleware.PermissionConfig.Mode = prevMode })

	dsn := fmt.Sprintf("file:taskproj_test_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&taskProjDBCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()

	tenant := func(name, code, typ, status string, providerID int) *ent.Tenant {
		tb := client.Tenant.Create().SetName(name).SetCode(code).SetType(entTenant.Type(typ)).SetStatus(status)
		if providerID > 0 {
			tb = tb.SetMspProviderID(providerID)
		}
		tn, err := tb.Save(ctx)
		require.NoError(t, err)
		return tn
	}
	provider := tenant("Provider", "prov", "msp_provider", "active", 0)
	custA := tenant("Customer A", "cust-a", "msp_customer", "active", provider.ID)
	custB := tenant("Customer B", "cust-b", "msp_customer", "active", provider.ID)

	mkUser := func(name string, tenantID int, mspRole string) *ent.User {
		ub := client.User.Create().
			SetUsername(name).SetEmail(name + "@example.com").SetName(name).
			SetPasswordHash("hash").SetTenantID(tenantID)
		if mspRole != "" {
			ub = ub.SetMspRole(entUser.MspRole(mspRole))
		}
		u, err := ub.Save(ctx)
		require.NoError(t, err)
		return u
	}
	actorUser := mkUser("prov-agent", provider.ID, "provider_agent")
	otherUser := mkUser("prov-agent2", provider.ID, "provider_agent")

	_, err := client.MSPAllocation.Create().
		SetMspUserID(actorUser.ID).SetCustomerTenantID(custA.ID).SetProviderTenantID(provider.ID).SetRole("primary").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.MSPAllocation.Create().
		SetMspUserID(otherUser.ID).SetCustomerTenantID(custA.ID).SetProviderTenantID(provider.ID).SetRole("backup").
		Save(ctx)
	require.NoError(t, err)

	// 客户侧 msp_tech 角色基线（C2）：task:read/update（provider 员工在客户租户内的任务面权限）。
	roleRow, err := client.Role.Create().SetName("MSP 技术").SetCode("msp_tech").SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)
	permRead, err := client.Permission.Create().
		SetCode("task:read").SetName("任务读取").SetResource("task").SetAction("read").SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)
	permUpdate, err := client.Permission.Create().
		SetCode("task:update").SetName("任务处理").SetResource("task").SetAction("update").SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(roleRow.ID).SetPermissionID(permRead.ID).SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.RolePermission.Create().SetRoleID(roleRow.ID).SetPermissionID(permUpdate.ID).SetTenantID(custA.ID).Save(ctx)
	require.NoError(t, err)

	instances := map[int]int{}
	definitions := map[int]int{}
	deployments := map[int]int{}
	mkDeployment := func(tenantID int) int {
		if did, ok := deployments[tenantID]; ok {
			return did
		}
		dep, err := client.ProcessDeployment.Create().
			SetDeploymentID(fmt.Sprintf("DEP-%d-%d", tenantID, atomic.AddInt64(&taskProjDBCounter, 1))).
			SetDeploymentName("部署").SetTenantID(tenantID).Save(ctx)
		require.NoError(t, err)
		deployments[tenantID] = dep.ID
		return dep.ID
	}
	mkDefinition := func(tenantID int) int {
		if did, ok := definitions[tenantID]; ok {
			return did
		}
		pd, err := client.ProcessDefinition.Create().
			SetKey("pd").SetName("流程定义").SetVersion("1").SetBpmnXML([]byte("<definitions/>")).
			SetDeploymentID(mkDeployment(tenantID)).SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
		definitions[tenantID] = pd.ID
		return pd.ID
	}
	mkInstance := func(tenantID int) int {
		if iid, ok := instances[tenantID]; ok {
			return iid
		}
		inst, err := client.ProcessInstance.Create().
			SetProcessInstanceID(fmt.Sprintf("PI-%d-%d", tenantID, atomic.AddInt64(&taskProjDBCounter, 1))).
			SetProcessDefinitionKey("pd").SetProcessDefinitionID(mkDefinition(tenantID)).SetStatus("running").SetTenantID(tenantID).SetVersion(1).
			Save(ctx)
		require.NoError(t, err)
		instances[tenantID] = inst.ID
		return inst.ID
	}
	mkTask := func(tenantID int, taskID, name, assignee, candUsers, candGroups, status string) *ent.ProcessTask {
		tb := client.ProcessTask.Create().
			SetTaskID(taskID).SetProcessInstanceID(mkInstance(tenantID)).SetProcessDefinitionKey("pd").
			SetTaskDefinitionKey("td").SetTaskName(name).SetTenantID(tenantID).SetStatus(status).
			SetCandidateUsers(candUsers).SetCandidateGroups(candGroups).SetCreatedTime(time.Now())
		if assignee != "" {
			tb = tb.SetAssignee(assignee)
		}
		tk, err := tb.Save(ctx)
		require.NoError(t, err)
		return tk
	}
	uidStr := strconv.Itoa(actorUser.ID)
	tAssigned := mkTask(custA.ID, "TASK-assigned", "指派给我", uidStr, "", "", "created")
	mkTask(custA.ID, "TASK-candidate", "候选给我", "", uidStr, "", "assigned")
	tQueue := mkTask(custA.ID, "TASK-queue", "服务商处理", "", "", "msp_provider", "created")
	tOther := mkTask(custA.ID, "TASK-other", "他人任务", strconv.Itoa(otherUser.ID), "", "", "created")
	mkTask(custA.ID, "TASK-terminal", "已完成", uidStr, "", "", "completed")
	tInB := mkTask(custB.ID, "TASK-in-b", "B 客户任务", uidStr, "", "", "created")

	return &taskProjEnv{
		client:   client,
		svc:      NewMSPTaskProjectionService(client, zaptest.NewLogger(t).Sugar()),
		actor:    MSPWorkbenchActor{UserID: actorUser.ID, Username: actorUser.Username, HomeTenantID: provider.ID, MSPRole: "provider_agent", AllowedCustomers: []int{custA.ID}},
		provider: provider, custA: custA, custB: custB, otherUser: otherUser,
		tAssigned: tAssigned, tQueue: tQueue, tOther: tOther, tInB: tInB,
	}
}

func findAction(t *testing.T, actions []dto.WorkbenchAllowedAction, name string) dto.WorkbenchAllowedAction {
	t.Helper()
	for _, a := range actions {
		if a.Action == name {
			return a
		}
	}
	t.Fatalf("action %s not found", name)
	return dto.WorkbenchAllowedAction{}
}

func TestMSPTaskProjection_List_FiltersAndActions(t *testing.T) {
	env := newTaskProjEnv(t)
	resp, err := env.svc.ListTasks(context.Background(), env.actor, dto.MSPTaskQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Total)
	byID := map[string]dto.MSPTaskItem{}
	for _, it := range resp.Items {
		byID[it.TaskID] = it
	}
	require.Contains(t, byID, "TASK-assigned")
	require.Contains(t, byID, "TASK-candidate")
	require.Contains(t, byID, "TASK-queue")
	assert.Equal(t, "assigned", byID["TASK-assigned"].Source)
	assert.Equal(t, "queue", byID["TASK-queue"].Source)
	assert.Equal(t, env.custA.ID, byID["TASK-queue"].CustomerTenantID)
	assert.Equal(t, env.custA.Name, byID["TASK-queue"].CustomerTenantName)

	queueActions := byID["TASK-queue"].AllowedActions
	assert.True(t, findAction(t, queueActions, "claim").Allowed)
	assert.False(t, findAction(t, queueActions, "complete").Allowed)
	assignedActions := byID["TASK-assigned"].AllowedActions
	assert.False(t, findAction(t, assignedActions, "claim").Allowed)
	assert.True(t, findAction(t, assignedActions, "complete").Allowed)

	// 终态 / 他人任务 / 未分配客户（B）均不可见。
	require.NotContains(t, byID, "TASK-terminal")
	require.NotContains(t, byID, "TASK-other")
	require.NotContains(t, byID, "TASK-in-b")
}

func TestMSPTaskProjection_List_ViewerHasNoWriteActions(t *testing.T) {
	env := newTaskProjEnv(t)
	viewer := env.actor
	viewer.MSPRole = "provider_viewer"
	resp, err := env.svc.ListTasks(context.Background(), viewer, dto.MSPTaskQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Total)
	for _, it := range resp.Items {
		for _, ac := range it.AllowedActions {
			assert.False(t, ac.Allowed, "%s/%s 应不可写", it.TaskID, ac.Action)
			assert.Equal(t, CodeActionNotAllowed, ac.ReasonCode)
		}
	}
}

func TestMSPTaskProjection_Claim(t *testing.T) {
	env := newTaskProjEnv(t)
	item, err := env.svc.ClaimTask(context.Background(), env.actor, "TASK-queue", 0)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(env.actor.UserID), item.Assignee)
	assert.Equal(t, "assigned", item.Status)

	// 幂等：重复认领（已由我认领）不报错。
	_, err = env.svc.ClaimTask(context.Background(), env.actor, "TASK-queue", 0)
	require.NoError(t, err)

	// 他人任务不可认领（分配规则外）。
	_, err = env.svc.ClaimTask(context.Background(), env.actor, "TASK-other", 0)
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeActionNotAllowed, ae.Code)

	count, err := env.client.AuditLog.Query().Where(auditlog.ActionEQ("workbench.action")).Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count, "两次成功认领各写一条审计")
}

type stubCompletionEngine struct {
	called    int
	taskID    string
	bpmnTenant int
	rlsTenant  int
	variables map[string]interface{}
	err       error
}

func (s *stubCompletionEngine) CompleteTask(ctx context.Context, taskID string, variables map[string]interface{}) error {
	s.called++
	s.taskID = taskID
	if v, ok := ctx.Value(bpmn.BPMNTenantIDContextKey).(int); ok {
		s.bpmnTenant = v
	}
	if v, ok := tenantctx.TenantID(ctx); ok {
		s.rlsTenant = v
	}
	s.variables = variables
	return s.err
}

func TestMSPTaskProjection_Complete(t *testing.T) {
	env := newTaskProjEnv(t)
	stub := &stubCompletionEngine{}
	svc := newMSPTaskProjectionServiceWithEngine(env.client, stub, zaptest.NewLogger(t).Sugar())

	err := svc.CompleteTask(context.Background(), env.actor, "TASK-assigned", 0, map[string]interface{}{"approved": true})
	require.NoError(t, err)
	assert.Equal(t, 1, stub.called)
	assert.Equal(t, "TASK-assigned", stub.taskID)
	assert.Equal(t, env.custA.ID, stub.bpmnTenant, "BPMN 租户上下文=客户租户")
	assert.Equal(t, env.custA.ID, stub.rlsTenant, "RLS GUC=客户租户")
	assert.Equal(t, true, stub.variables["approved"])

	// 非负责人（未认领的池任务）不可完成。
	err = svc.CompleteTask(context.Background(), env.actor, "TASK-queue", 0, nil)
	require.Error(t, err)

	// 终态不可完成。
	err = svc.CompleteTask(context.Background(), env.actor, "TASK-terminal", 0, nil)
	require.Error(t, err)

	// 引擎失败 → 审计 failed（此处仅断言错误透传）。
	stub.err = fmt.Errorf("engine boom")
	err = svc.CompleteTask(context.Background(), env.actor, "TASK-assigned", 0, nil)
	require.Error(t, err)
}

func TestMSPTaskProjection_Authorize_Denials(t *testing.T) {
	env := newTaskProjEnv(t)
	// 声明未分配客户 → 403 语义 + scope_denied 审计。
	_, err := env.svc.ClaimTask(context.Background(), env.actor, "TASK-in-b", env.custB.ID)
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	count, err := env.client.AuditLog.Query().Where(auditlog.ActionEQ("tenant.scope_denied")).Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// 任务位于未分配客户（未声明）→ 404 语义（防枚举）。
	_, err = env.svc.ClaimTask(context.Background(), env.actor, "TASK-in-b", 0)
	assert.ErrorIs(t, err, ErrMSPTaskNotFound)

	// 未知任务 → 404 语义。
	_, err = env.svc.ClaimTask(context.Background(), env.actor, "TASK-nope", 0)
	assert.ErrorIs(t, err, ErrMSPTaskNotFound)
}

func TestMSPTaskProjection_InactiveCustomerReadOnly(t *testing.T) {
	env := newTaskProjEnv(t)
	_, err := env.client.Tenant.UpdateOneID(env.custA.ID).SetStatus("suspended").Save(context.Background())
	require.NoError(t, err)

	_, err = env.svc.ClaimTask(context.Background(), env.actor, "TASK-queue", 0)
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeCustomerInactive, ae.Code)

	resp, err := env.svc.ListTasks(context.Background(), env.actor, dto.MSPTaskQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.Total)
	for _, it := range resp.Items {
		for _, ac := range it.AllowedActions {
			assert.False(t, ac.Allowed)
			assert.Equal(t, CodeCustomerInactive, ac.ReasonCode)
		}
	}
}

func TestResolveProviderTaskAssignee_Priority(t *testing.T) {
	env := newTaskProjEnv(t)
	uid, providerID, role, ok := resolveProviderTaskAssignee(context.Background(), env.client, env.custA.ID)
	require.True(t, ok)
	assert.Equal(t, strconv.Itoa(env.actor.UserID), uid, "primary 优先于 backup")
	assert.Equal(t, env.provider.ID, providerID)
	assert.Equal(t, "primary", role)

	noProvider, err := env.client.Tenant.Create().SetName("No Provider").SetCode("no-prov").
		SetType(entTenant.Type("msp_customer")).SetStatus("active").Save(context.Background())
	require.NoError(t, err)
	_, _, _, ok = resolveProviderTaskAssignee(context.Background(), env.client, noProvider.ID)
	assert.False(t, ok)
}

// TestCreateUserTask_ProviderMarkerResolvesAllocation 验证引擎侧解析 hook：
// candidateGroups 含保留标记 msp_provider 时，任务 assignee = allocation primary 员工，
// 且任务变量记录投影元数据；无 provider 客户保持未分配（池兜底）。
func TestCreateUserTask_ProviderMarkerResolvesAllocation(t *testing.T) {
	env := newTaskProjEnv(t)
	engine := &CustomProcessEngine{
		client:         env.client,
		logger:         zaptest.NewLogger(t).Sugar(),
		parser:         NewBPMNParser(),
		exprEngine:     NewExpressionEngine(),
		expressionVars: map[string]interface{}{},
	}
	ctx := tenantctx.WithTenantID(context.Background(), env.custA.ID)
	depA, err := env.client.ProcessDeployment.Create().
		SetDeploymentID("DEP-provider-marker").SetDeploymentName("部署").SetTenantID(env.custA.ID).Save(ctx)
	require.NoError(t, err)
	defA, err := env.client.ProcessDefinition.Create().
		SetKey("pd-marker").SetName("流程定义").SetVersion("1").SetBpmnXML([]byte("<definitions/>")).
		SetDeploymentID(depA.ID).SetTenantID(env.custA.ID).Save(ctx)
	require.NoError(t, err)
	instRow, err := env.client.ProcessInstance.Create().
		SetProcessInstanceID("PI-provider-marker").SetProcessDefinitionKey("pd-marker").SetProcessDefinitionID(defA.ID).SetStatus("running").
		SetTenantID(env.custA.ID).SetVersion(1).Save(ctx)
	require.NoError(t, err)
	instance := &ent.ProcessInstance{ID: instRow.ID, TenantID: env.custA.ID, ProcessDefinitionKey: "pd-marker", Variables: map[string]interface{}{}}
	process := &BPMNProcess{}
	task := &BPMNUserTask{ID: "Task_Provider", Name: "服务商处理", CandidateGroups: "msp_provider"}
	require.NoError(t, engine.createUserTask(ctx, env.client, instance, process, task))

	row, err := env.client.ProcessTask.Query().Where(processtask.TaskDefinitionKeyEQ("Task_Provider")).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(env.actor.UserID), row.Assignee)
	assert.Equal(t, true, row.TaskVariables["mspProjected"])
	assert.Equal(t, "msp_provider", row.CandidateGroups, "标记保留（待办池识别）")
	assert.NotNil(t, row.TaskVariables["mspProviderTenantId"])

	// 无 provider 的客户：保持未分配 + 标记未解析。
	noProvider, err := env.client.Tenant.Create().SetName("No Provider").SetCode("no-prov").
		SetType(entTenant.Type("msp_customer")).SetStatus("active").Save(context.Background())
	require.NoError(t, err)
	ctx2 := tenantctx.WithTenantID(context.Background(), noProvider.ID)
	depNP, err := env.client.ProcessDeployment.Create().
		SetDeploymentID("DEP-provider-marker-2").SetDeploymentName("部署").SetTenantID(noProvider.ID).Save(ctx2)
	require.NoError(t, err)
	defNP, err := env.client.ProcessDefinition.Create().
		SetKey("pd-marker").SetName("流程定义").SetVersion("1").SetBpmnXML([]byte("<definitions/>")).
		SetDeploymentID(depNP.ID).SetTenantID(noProvider.ID).Save(ctx2)
	require.NoError(t, err)
	instRow2, err := env.client.ProcessInstance.Create().
		SetProcessInstanceID("PI-provider-marker-2").SetProcessDefinitionKey("pd-marker").SetProcessDefinitionID(defNP.ID).SetStatus("running").
		SetTenantID(noProvider.ID).SetVersion(1).Save(ctx2)
	require.NoError(t, err)
	instance2 := &ent.ProcessInstance{ID: instRow2.ID, TenantID: noProvider.ID, ProcessDefinitionKey: "pd-marker", Variables: map[string]interface{}{}}
	task2 := &BPMNUserTask{ID: "Task_Provider2", Name: "服务商处理", CandidateGroups: "msp_provider"}
	require.NoError(t, engine.createUserTask(ctx2, env.client, instance2, process, task2))
	row2, err := env.client.ProcessTask.Query().Where(processtask.TaskDefinitionKeyEQ("Task_Provider2")).Only(ctx2)
	require.NoError(t, err)
	assert.Equal(t, "", row2.Assignee)
	assert.Equal(t, true, row2.TaskVariables["mspUnresolved"])
}
