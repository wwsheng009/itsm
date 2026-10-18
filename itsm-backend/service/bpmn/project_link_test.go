package bpmn

import (
	"context"
	"errors"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/common/projectlink"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// ===================== 测试替身 =====================

// fakeProjectLink 记录调用并返回预设错误，用于验证 BPMN 侧确实走了共享端口。
type fakeProjectLink struct {
	calls     int
	tenantID  int
	projectID int
	roles     []string
	err       error
}

func (f *fakeProjectLink) ValidateAttach(ctx context.Context, tenantID, projectID int, roles []string) error {
	f.calls++
	f.tenantID = tenantID
	f.projectID = projectID
	f.roles = roles
	return f.err
}

// ===================== 流程变量解析 =====================

func TestProjectIDFromVars(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]interface{}
		want *int
	}{
		{name: "nil map", vars: nil, want: nil},
		{name: "missing", vars: map[string]interface{}{"title": "x"}, want: nil},
		{name: "camelCase int", vars: map[string]interface{}{"projectId": 7}, want: intPtr(7)},
		{name: "camelCase float64 (JSON)", vars: map[string]interface{}{"projectId": float64(9)}, want: intPtr(9)},
		{name: "camelCase int64", vars: map[string]interface{}{"projectId": int64(11)}, want: intPtr(11)},
		{name: "camelCase numeric string", vars: map[string]interface{}{"projectId": "13"}, want: intPtr(13)},
		{name: "snake_case fallback", vars: map[string]interface{}{"project_id": 17}, want: intPtr(17)},
		{name: "camelCase wins", vars: map[string]interface{}{"projectId": 5, "project_id": 6}, want: intPtr(5)},
		{name: "zero means no attach", vars: map[string]interface{}{"projectId": 0}, want: nil},
		{name: "negative means no attach", vars: map[string]interface{}{"projectId": -3}, want: nil},
		{name: "garbage string ignored", vars: map[string]interface{}{"projectId": "abc"}, want: nil},
		{name: "bool ignored", vars: map[string]interface{}{"projectId": true}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectIDFromVars(tt.vars)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}

// ===================== 端口未装配：fail-closed =====================

func TestGuardProjectAttach_NoValidatorFailsClosed(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:project_link_novalidator?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	_, err := GuardProjectAttach(context.Background(), client, nil, 1, map[string]interface{}{"projectId": 3})
	require.Error(t, err, "携带 projectId 但端口未装配时必须拒绝创建")

	var bizErr *common.BusinessError
	require.ErrorAs(t, err, &bizErr)
	assert.Equal(t, common.ServiceUnavailableCode, bizErr.Code)
}

func TestGuardProjectAttach_NoProjectIDIsNoop(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:project_link_noid?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	// 端口未装配，但不携带 projectId → 直通（不影响存量流程）。
	got, err := GuardProjectAttach(context.Background(), client, nil, 1, map[string]interface{}{"title": "x"})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ===================== 发起人角色解析 =====================

func TestResolveInitiatorRoles_PrimaryAndAttached(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:project_link_roles?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("T").SetCode("t").SetDomain("t.test").SaveX(ctx)
	other := client.Tenant.Create().SetName("O").SetCode("o").SetDomain("o.test").SaveX(ctx)

	// 同租户附加角色（生效）→ 并入。
	role := client.Role.Create().
		SetName("项目经理").SetCode("project_manager").SetTenantID(tenant.ID).SetIsActive(true).
		SaveX(ctx)
	// 跨租户角色 → 必须排除。
	crossRole := client.Role.Create().
		SetName("外部角色").SetCode("cross_role").SetTenantID(other.ID).SetIsActive(true).
		SaveX(ctx)

	u := client.User.Create().
		SetUsername("initiator").SetEmail("i@t.test").SetName("Initiator").
		SetPasswordHash("h").SetRole(user.RoleAdmin).SetActive(true).SetTenantID(tenant.ID).
		AddRoleIDs(role.ID, crossRole.ID).
		SaveX(ctx)

	// 上下文操作人优先。
	workflowCtx := context.WithValue(ctx, BPMNUserIDContextKey, u.ID)
	roles := resolveInitiatorRoles(workflowCtx, client, tenant.ID, map[string]interface{}{"created_by": 999999})
	assert.ElementsMatch(t, []string{"admin", "project_manager"}, roles)

	// 上下文缺失时回退到流程变量中的发起人（各 create_* 写 created_by/reporter_id/requester_id）。
	roles = resolveInitiatorRoles(ctx, client, tenant.ID, map[string]interface{}{"reporter_id": u.ID})
	assert.ElementsMatch(t, []string{"admin", "project_manager"}, roles)

	// 跨租户用户不得解析出角色（防越权）。
	roles = resolveInitiatorRoles(ctx, client, other.ID, map[string]interface{}{"created_by": u.ID})
	assert.Empty(t, roles)
}

// ===================== 三个 create_* 服务任务 =====================

func newProjectLinkFixture(t *testing.T) (*ent.Client, context.Context, int, *ent.User) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:project_link_handlers?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("PL").SetCode("pl").SetDomain("pl.test").SaveX(ctx)
	u := client.User.Create().
		SetUsername("pl-user").SetEmail("pl@pl.test").SetName("PL User").
		SetPasswordHash("h").SetRole(user.RoleAdmin).SetActive(true).SetTenantID(tenant.ID).
		SaveX(ctx)

	return client, context.WithValue(ctx, BPMNTenantIDContextKey, tenant.ID), tenant.ID, u
}

func TestCreateIncident_PersistsProjectIDOnInsert(t *testing.T) {
	client, ctx, tenantID, u := newProjectLinkFixture(t)
	validator := &fakeProjectLink{}
	h := NewIncidentServiceTaskHandler(client, zaptest.NewLogger(t).Sugar())
	h.SetProjectLink(validator)

	res, err := h.Execute(ctx, nil, map[string]interface{}{
		"action":          "create_incident",
		"title":           "流程建事件",
		"type":            "incident",
		"priority":        "high",
		"severity":        "high",
		"reporter_id":     u.ID,
		"projectId":       42,
		"tenant_id":       tenantID,
		"incident_number": "INC-PL-1",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)

	// 端口被调用且参数正确（租户/项目/发起人角色）。
	assert.Equal(t, 1, validator.calls)
	assert.Equal(t, tenantID, validator.tenantID)
	assert.Equal(t, 42, validator.projectID)
	assert.Contains(t, validator.roles, "admin")

	// project_id 随 INSERT 一次写入。
	incidentID := GetIntFromVars(res.OutputVars, "incident_id")
	require.Positive(t, incidentID)
	row, err := client.Incident.Get(ctx, incidentID)
	require.NoError(t, err)
	assert.Equal(t, 42, row.ProjectID)
}

func TestCreateIncident_ValidatorErrorBlocksInsert(t *testing.T) {
	client, ctx, tenantID, u := newProjectLinkFixture(t)
	validator := &fakeProjectLink{err: common.NewForbiddenError("需要项目写权限（project:write）")}
	h := NewIncidentServiceTaskHandler(client, zaptest.NewLogger(t).Sugar())
	h.SetProjectLink(validator)

	_, err := h.Execute(ctx, nil, map[string]interface{}{
		"action":      "create_incident",
		"title":       "应被拒绝",
		"reporter_id": u.ID,
		"projectId":   42,
		"tenant_id":   tenantID,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, validator.err) || err.Error() != "")

	// 校验失败不落库（无部分成功）。
	count, err := client.Incident.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestCreateChange_PersistsProjectIDOnInsert(t *testing.T) {
	client, ctx, tenantID, u := newProjectLinkFixture(t)
	validator := &fakeProjectLink{}
	h := NewChangeServiceTaskHandler(client, zaptest.NewLogger(t).Sugar())
	h.SetProjectLink(validator)

	res, err := h.Execute(ctx, nil, map[string]interface{}{
		"action":     "create_change",
		"title":      "流程建变更",
		"type":       "normal",
		"priority":   "medium",
		"created_by": u.ID,
		"project_id": 77, // snake_case 兼容
		"tenant_id":  tenantID,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)

	assert.Equal(t, 1, validator.calls)
	assert.Equal(t, 77, validator.projectID)

	changeID := GetIntFromVars(res.OutputVars, "change_id")
	require.Positive(t, changeID)
	row, err := client.Change.Get(ctx, changeID)
	require.NoError(t, err)
	assert.Equal(t, 77, row.ProjectID)
}

func TestCreateServiceRequest_PersistsProjectIDOnInsert(t *testing.T) {
	client, ctx, tenantID, u := newProjectLinkFixture(t)
	validator := &fakeProjectLink{}
	h := NewServiceRequestServiceTaskHandler(client, zaptest.NewLogger(t).Sugar())
	h.SetProjectLink(validator)

	catalog := client.ServiceCatalog.Create().
		SetName("云主机").SetTenantID(tenantID).SaveX(ctx)

	res, err := h.Execute(ctx, nil, map[string]interface{}{
		"action":       "create_request",
		"title":        "流程建服务请求",
		"catalog_id":   catalog.ID,
		"requester_id": u.ID,
		"projectId":    88,
		"tenant_id":    tenantID,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)

	assert.Equal(t, 1, validator.calls)
	assert.Equal(t, 88, validator.projectID)

	requestID := GetIntFromVars(res.OutputVars, "request_id")
	require.Positive(t, requestID)
	row, err := client.ServiceRequest.Get(ctx, requestID)
	require.NoError(t, err)
	assert.Equal(t, 88, row.ProjectID)
}

func TestCreateHandlers_NoProjectIDDoesNotTouchValidator(t *testing.T) {
	client, ctx, tenantID, u := newProjectLinkFixture(t)
	validator := &fakeProjectLink{}

	incidentHandler := NewIncidentServiceTaskHandler(client, zaptest.NewLogger(t).Sugar())
	incidentHandler.SetProjectLink(validator)
	_, err := incidentHandler.Execute(ctx, nil, map[string]interface{}{
		"action": "create_incident", "title": "无项目", "reporter_id": u.ID, "tenant_id": tenantID,
	})
	require.NoError(t, err)
	assert.Zero(t, validator.calls, "未携带 projectId 时不应触发项目校验")
}

// intPtr 测试辅助。
func intPtr(v int) *int { return &v }

// 编译期断言：fakeProjectLink 满足共享端口。
var _ projectlink.Validator = (*fakeProjectLink)(nil)
