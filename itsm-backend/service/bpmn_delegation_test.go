package service

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/processapprovaldecision"
	"itsm-backend/ent/processtask"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/service/bpmn"

	_ "github.com/mattn/go-sqlite3"

	"go.uber.org/zap/zaptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDelegationFixture(t *testing.T, allowDelegate bool) (*CustomProcessEngine, context.Context, int) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:delegation_test_"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	engine := NewCustomProcessEngine(client, logger).(*CustomProcessEngine)

	ctx := context.Background()
	tenant, err := client.Tenant.Create().
		SetName("Delegate Tenant").SetCode("delegate").SetDomain("delegate.test").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	approver, err := client.User.Create().
		SetUsername("approver").SetEmail("approver@test.com").SetName("审批人").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	delegatee, err := client.User.Create().
		SetUsername("delegatee").SetEmail("delegatee@test.com").SetName("被委托人").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	deployment, err := client.ProcessDeployment.Create().
		SetDeploymentID("DEP-DELEGATE").SetDeploymentName("Delegate Deploy").
		SetIsActive(true).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	def, err := client.ProcessDefinition.Create().
		SetKey("delegate_test").SetName("委托测试").SetVersion("1").
		SetIsLatest(true).SetIsActive(true).SetBpmnXML([]byte("<bpmn/>")).
		SetDeploymentID(deployment.ID).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.ProcessInstance.Create().
		SetProcessInstanceID("PI-DELEGATE-1").
		SetProcessDefinitionKey(def.Key).SetProcessDefinitionID(def.ID).
		SetStatus("running").
		SetVariables(map[string]interface{}{"business_type": "ticket", "business_id": "1"}).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	taskVars := map[string]interface{}{
		"taskPurpose":   "approval",
		"allowDelegate": allowDelegate,
	}
	_, err = client.ProcessTask.Create().
		SetTaskID("TASK-DELEGATE-1").
		SetTaskDefinitionKey("ApprovalTask").
		SetTaskName("审批").
		SetProcessDefinitionKey(def.Key).
		SetProcessInstanceID(instance.ID).
		SetStatus("assigned").
		SetAssignee(approver.Username).
		SetTenantID(tenant.ID).
		SetTaskVariables(taskVars).
		Save(ctx)
	require.NoError(t, err)

	_ = delegatee
	workflowCtx := context.WithValue(ctx, bpmn.BPMNTenantIDContextKey, tenant.ID)
	workflowCtx = context.WithValue(workflowCtx, bpmn.BPMNUserIDContextKey, approver.ID)

	return engine, workflowCtx, tenant.ID
}

func TestDelegateTask_Allowed_WritesAuditRecord(t *testing.T) {
	engine, workflowCtx, tenantID := setupDelegationFixture(t, true)

	err := engine.TaskService().DelegateTask(workflowCtx, "TASK-DELEGATE-1", "delegatee")
	require.NoError(t, err)

	task, err := engine.TaskService().GetTask(workflowCtx, "TASK-DELEGATE-1")
	require.NoError(t, err)
	assert.Equal(t, "delegatee", task.Assignee)
	assert.Equal(t, "assigned", task.Status)
	assert.Equal(t, "approver", task.TaskVariables["delegated_from"])

	client := engine.client
	decisions, err := client.ProcessApprovalDecision.Query().
		Where(processapprovaldecision.TenantID(tenantID)).
		All(workflowCtx)
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Equal(t, "delegate", decisions[0].Action)
	assert.Equal(t, "delegated", decisions[0].Decision)
}

// TestDelegateTask_AllowsProviderMembershipTarget 锁定 provider 客户租户身份：
// 目标账号 home tenant 不同，但持有当前租户 active membership 时必须允许委派。
func TestDelegateTask_AllowsProviderMembershipTarget(t *testing.T) {
	engine, workflowCtx, tenantID := setupDelegationFixture(t, true)
	ctx := context.Background()

	providerTenant, err := engine.client.Tenant.Create().
		SetName("Provider Tenant").SetCode("delegate-provider").SetDomain("delegate-provider.test").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	provider, err := engine.client.User.Create().
		SetUsername("provider-delegatee").SetEmail("provider-delegatee@test.com").SetName("Provider Delegatee").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(providerTenant.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = engine.client.UserTenantMembership.Create().
		SetUserID(provider.ID).SetTenantID(tenantID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetMspRole("msp_tech").
		SetStatus(usertenantmembership.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	err = engine.TaskService().DelegateTask(workflowCtx, "TASK-DELEGATE-1", provider.Username)
	require.NoError(t, err)

	task, err := engine.TaskService().GetTask(workflowCtx, "TASK-DELEGATE-1")
	require.NoError(t, err)
	assert.Equal(t, provider.Username, task.Assignee)
	assert.Equal(t, "assigned", task.Status)
}

// TestDelegateTask_AllowsProviderMembershipActor 锁定 provider 作为客户租户审批人时
// 可以发起委托；不能再以 users.tenant_id 判定操作人资格。
func TestDelegateTask_AllowsProviderMembershipActor(t *testing.T) {
	engine, workflowCtx, tenantID := setupDelegationFixture(t, true)
	ctx := context.Background()

	providerTenant, err := engine.client.Tenant.Create().
		SetName("Provider Actor Tenant").SetCode("delegate-provider-actor").SetDomain("delegate-provider-actor.test").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	provider, err := engine.client.User.Create().
		SetUsername("provider-approver").SetEmail("provider-approver@test.com").SetName("Provider Approver").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).SetTenantID(providerTenant.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = engine.client.UserTenantMembership.Create().
		SetUserID(provider.ID).SetTenantID(tenantID).
		SetAccountKind(usertenantmembership.AccountKindProvider).
		SetSource(usertenantmembership.SourceAllocation).
		SetMspRole("msp_tech").
		SetStatus(usertenantmembership.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	_, err = engine.client.ProcessTask.Update().
		Where(processtask.TaskID("TASK-DELEGATE-1"), processtask.TenantID(tenantID)).
		SetAssignee(provider.Username).
		Save(ctx)
	require.NoError(t, err)

	workflowCtx = context.WithValue(workflowCtx, bpmn.BPMNUserIDContextKey, provider.ID)
	err = engine.TaskService().DelegateTask(workflowCtx, "TASK-DELEGATE-1", "delegatee")
	require.NoError(t, err)

	task, err := engine.TaskService().GetTask(workflowCtx, "TASK-DELEGATE-1")
	require.NoError(t, err)
	assert.Equal(t, "delegatee", task.Assignee)
	assert.Equal(t, "assigned", task.Status)
}

func TestDelegateTask_NotAllowed_ReturnsError(t *testing.T) {
	engine, workflowCtx, _ := setupDelegationFixture(t, false)

	err := engine.TaskService().DelegateTask(workflowCtx, "TASK-DELEGATE-1", "delegatee")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不允许委托")
}

func TestDelegateTask_EmptyTarget_ReturnsError(t *testing.T) {
	engine, workflowCtx, _ := setupDelegationFixture(t, true)

	err := engine.TaskService().DelegateTask(workflowCtx, "TASK-DELEGATE-1", "  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "委托目标不能为空")
}

func TestDelegateTaskByID_DelegatesCorrectly(t *testing.T) {
	engine, workflowCtx, tenantID := setupDelegationFixture(t, true)

	client := engine.client
	task, err := client.ProcessTask.Query().All(workflowCtx)
	require.NoError(t, err)
	require.Len(t, task, 1)
	taskID := task[0].ID

	err = engine.TaskService().DelegateTaskByID(workflowCtx, taskID, "delegatee")
	require.NoError(t, err)

	updated, err := client.ProcessTask.Get(workflowCtx, taskID)
	require.NoError(t, err)
	assert.Equal(t, "delegatee", updated.Assignee)
	assert.Equal(t, "assigned", updated.Status)

	decisions, err := client.ProcessApprovalDecision.Query().
		Where(processapprovaldecision.TenantID(tenantID)).
		All(workflowCtx)
	require.NoError(t, err)
	assert.Len(t, decisions, 1)
}
