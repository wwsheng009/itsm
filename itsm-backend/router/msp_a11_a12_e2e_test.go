// msp_a11_a12_e2e_test.go：MSP 多 provider 端到端验收（canon A11/A12；IP-P2-1 出口）。
//
// 与三层单测的分工：
//   - handlers/msp、service 单测覆盖单点契约；本文件在**路由器级**跑完整中间件链
//     （Auth → RBAC → MSPMiddleware → RequireMSPPermission → handler → service），
//     覆盖「建 provider → 建客户 → 分配 → 建单 → 工作台 → 条目操作」的最小闭环。
//
// A11（N=1/N=2 同一套用例）：
//   - runScenario(nProviders) 是唯一剧本，N=1 与 N=2 用同一请求序列；
//   - 断言两次运行的 outcome 指纹完全一致（N=1 无额外参数/步骤）；
//   - N=2 额外断言 provider 收窄（P2 员工不可见 P1 客户）。
//
// A12（多 provider 工单流转）：
//   - 建单落 provider 快照（IsManagedByMSP / MSPProviderID）；
//   - provider 工作台可见（provider ∩ allocation 收窄；显式未分配客户 403 MSP_ALLOCATION_REQUIRED）；
//   - 指派校验（assigner ∈ allocation ∧ provider；未分配员工拒绝）；
//   - 跨 provider 拒绝（P2 员工访问 P1 客户 403；不带筛选仅见本 provider 客户）。
//   - 通知双投递（v1.40）：工作台回复/改状态在客户侧之外，向 provider 租户投递
//     （托管处理人 + provider 管理员；actor 自身排除），provider 侧行归属 provider 租户、
//     深链指向工作台；单测矩阵见 service/msp_provider_side_notification_test.go。
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	entNotification "itsm-backend/ent/notification"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/user"
	mspHandler "itsm-backend/handlers/msp"
	"itsm-backend/middleware"
	ticketrepo "itsm-backend/repository/ticket"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const mspE2EJWTSecret = "msp-a11-a12-e2e-secret"

// mspE2EOutcome 是跨 N=1/N=2 比较的「行为指纹」：同一剧本必须产生同构结果。
type mspE2EOutcome struct {
	WorkbenchListStatus int
	WorkbenchListCount  int
	SummaryStatus       int
	SummaryCustomers    int
	AssignStatus        int
}

type mspE2EEnv struct {
	client        *ent.Client
	engine        *gin.Engine
	provider      *ent.Tenant
	provider2     *ent.Tenant // 仅 N=2
	customer      *ent.Tenant
	customer2     *ent.Tenant
	customer3     *ent.Tenant // 仅 N=2（P2 的客户）
	agent         *ent.User   // P1，已分配 C1/C2
	agent2        *ent.User   // P1，未分配任何客户
	agent3        *ent.User   // P2（N=2），已分配 C3
	providerAdmin *ent.User   // P1 provider 管理员（A12 双投递收件人）
	ticket        *ticketrepo.Ticket
	ticketID3     int
}

func newMSPE2EEngine(t *testing.T, client *ent.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := zaptest.NewLogger(t).Sugar()
	ticketSvc := service.NewTicketServiceForTest(client, logger)
	notificationSvc := service.NewTicketNotificationService(client, logger)
	ticketSvc.SetNotificationService(notificationSvc)
	commentSvc := service.NewTicketCommentService(client, logger)
	commentSvc.SetNotificationService(notificationSvc)
	h := mspHandler.NewHandler(
		service.NewMSPAllocationService(client, logger),
		ticketSvc,
		service.NewMSPWorkbenchService(client, ticketSvc, commentSvc, logger),
		nil, // views：本剧本不覆盖（IP-P2-4a 另有单测）
		service.NewMSPAuditService(client, logger),
		logger,
	)
	engine := gin.New()
	SetupRoutes(engine, &RouterConfig{
		JWTSecret:  mspE2EJWTSecret,
		Logger:     logger,
		Client:     client,
		MSPHandler: h,
	})
	return engine
}

// seedMSPRole 在租户内创建 RBAC 角色与 role_permissions（与生产 DB 单源同构），返回角色实体。
func seedMSPRole(t *testing.T, ctx context.Context, client *ent.Client, tenantID int, code string, codes []string) *ent.Role {
	t.Helper()
	roleEntity, err := client.Role.Create().SetName(code).SetCode(code).SetTenantID(tenantID).Save(ctx)
	require.NoError(t, err)
	for _, permCode := range codes {
		parts := strings.SplitN(permCode, ":", 2)
		require.Len(t, parts, 2, "权限码格式 resource:action")
		permEntity, err := client.Permission.Create().
			SetCode(permCode).SetName(permCode).
			SetResource(parts[0]).SetAction(parts[1]).
			SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
		_, err = client.RolePermission.Create().
			SetRoleID(roleEntity.ID).SetPermissionID(permEntity.ID).SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
	}
	return roleEntity
}

func mspE2EToken(t *testing.T, u *ent.User) string {
	t.Helper()
	token, err := middleware.GenerateAccessToken(u.ID, u.Username, string(u.Role), u.TenantID, mspE2EJWTSecret, 15*time.Minute)
	require.NoError(t, err)
	return token
}

func mspE2ERequest(t *testing.T, engine *gin.Engine, method, path, token string, body any) (int, string) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

func decodeData(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var envelope map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), body)
	data, _ := envelope["data"].(map[string]interface{})
	return data
}

// runScenario 是 A11 的唯一剧本：N=1/N=2 走完全相同的请求序列，返回行为指纹。
func runScenario(t *testing.T, nProviders int) *mspE2EOutcome {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:msp_e2e_n%d?mode=memory&cache=shared&_fk=1", nProviders))
	defer client.Close()
	engine := newMSPE2EEngine(t, client)
	env := &mspE2EEnv{client: client, engine: engine}

	// 1) 建 provider（N=2 时建第二个）。
	env.provider = mustTenant(t, client, "Provider One", fmt.Sprintf("e2e-prov-1-%d", nProviders), "msp_provider")
	env.customer = mustTenant(t, client, "Customer One", fmt.Sprintf("e2e-cust-1-%d", nProviders), "msp_customer", env.provider.ID)
	env.customer2 = mustTenant(t, client, "Customer Two", fmt.Sprintf("e2e-cust-2-%d", nProviders), "msp_customer", env.provider.ID)
	if nProviders == 2 {
		env.provider2 = mustTenant(t, client, "Provider Two", fmt.Sprintf("e2e-prov-2-%d", nProviders), "msp_provider")
		env.customer3 = mustTenant(t, client, "Customer Three", fmt.Sprintf("e2e-cust-3-%d", nProviders), "msp_customer", env.provider2.ID)
	}

	// 2) RBAC 角色（每个 provider 租户一套；provider_agent → rbac msp_tech）。
	agentPerms := []string{"msp:read", "msp_ticket:read", "msp_ticket:write", "msp_customer:read", "msp_allocation:read"}
	role1 := seedMSPRole(t, ctx, client, env.provider.ID, "msp_tech", agentPerms)
	var role2 *ent.Role
	if nProviders == 2 {
		role2 = seedMSPRole(t, ctx, client, env.provider2.ID, "msp_tech", agentPerms)
	}

	// 3) 员工与分配（provider ∩ allocation 是唯一授权来源）。
	env.agent = mustMSPUser(t, client, "e2e-agent-1", env.provider.ID, role1.ID)
	env.agent2 = mustMSPUser(t, client, "e2e-agent-2", env.provider.ID, role1.ID) // 故意不分配
	env.providerAdmin = mustProviderAdmin(t, client, "e2e-provider-admin", env.provider.ID)
	_, err := client.MSPAllocation.Create().SetMspUserID(env.agent.ID).
		SetCustomerTenantID(env.customer.ID).SetProviderTenantID(env.provider.ID).SetRole("primary").Save(ctx)
	require.NoError(t, err)
	_, err = client.MSPAllocation.Create().SetMspUserID(env.agent.ID).
		SetCustomerTenantID(env.customer2.ID).SetProviderTenantID(env.provider.ID).SetRole("primary").Save(ctx)
	require.NoError(t, err)
	if nProviders == 2 {
		env.agent3 = mustMSPUser(t, client, "e2e-agent-3", env.provider2.ID, role2.ID)
		_, err = client.MSPAllocation.Create().SetMspUserID(env.agent3.ID).
			SetCustomerTenantID(env.customer3.ID).SetProviderTenantID(env.provider2.ID).SetRole("primary").Save(ctx)
		require.NoError(t, err)
	}

	// 4) 客户用户在 C1 建单（service 层，锁定 A12 快照派生）。
	requester := mustCustomerUser(t, client, "e2e-requester-1", env.customer.ID)
	ticketSvc := service.NewTicketServiceForTest(client, zaptest.NewLogger(t).Sugar())
	env.ticket, err = ticketSvc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title: "A12 端到端工单", Description: "e2e", Priority: "high", RequesterID: requester.ID,
	}, env.customer.ID)
	require.NoError(t, err)
	require.True(t, env.ticket.IsManagedByMSP, "A12：建单必须落 provider 快照")
	require.NotNil(t, env.ticket.MSPProviderID)
	assert.Equal(t, env.provider.ID, *env.ticket.MSPProviderID)

	// 快照断言以 DB 行为准（DTO 可能被后续读路径重算）。
	row, err := client.Ticket.Query().Where(ticket.IDEQ(env.ticket.ID)).Only(ctx)
	require.NoError(t, err)
	assert.True(t, row.IsManagedByMsp)
	require.NotZero(t, row.MspProviderID)
	assert.Equal(t, env.provider.ID, row.MspProviderID)

	if nProviders == 2 {
		requester3 := mustCustomerUser(t, client, "e2e-requester-3", env.customer3.ID)
		// ticket_number 全局唯一（S-4 约束），直接落库并显式编号，避免与 C1 的序列号碰撞。
		ticket3, err := client.Ticket.Create().
			SetTicketNumber(fmt.Sprintf("E2E-C3-%d", nProviders)).
			SetTitle("P2 客户工单").SetType("incident").SetPriority("medium").SetStatus("open").
			SetRequesterID(requester3.ID).SetTenantID(env.customer3.ID).
			SetIsManagedByMsp(true).SetMspProviderID(env.provider2.ID).
			Save(ctx)
		require.NoError(t, err)
		env.ticketID3 = ticket3.ID
	}

	agentToken := mspE2EToken(t, env.agent)

	// seed 自检：msp_tech 角色在 provider 租户内必须命中 msp_ticket:read
	// （失败时打印角色/权限/绑定三表，便于定位 RBAC 数据未就位）。
	if ok := middleware.HasResourcePermission(ctx, client, "msp_tech", "msp_ticket", "read", env.provider.ID); !ok {
		roles, _ := client.Role.Query().All(ctx)
		for _, r := range roles {
			t.Logf("role id=%d code=%s tenant=%d", r.ID, r.Code, r.TenantID)
		}
		bindings, _ := client.RolePermission.Query().All(ctx)
		for _, rp := range bindings {
			t.Logf("role_permission role=%d perm=%d tenant=%d", rp.RoleID, rp.PermissionID, rp.TenantID)
		}
		perms, _ := client.Permission.Query().All(ctx)
		for _, p := range perms {
			t.Logf("permission id=%d code=%s res=%s act=%s tenant=%d", p.ID, p.Code, p.Resource, p.Action, p.TenantID)
		}
		t.Fatalf("RBAC seed 自检失败：msp_tech 未命中 msp_ticket:read（provider=%d）", env.provider.ID)
	}

	// 5) 工作台：列表（显式筛选本客户）→ 必须可见。
	listStatus, listBody := mspE2ERequest(t, engine, http.MethodGet,
		fmt.Sprintf("/api/v1/msp/workbench/tickets?customerTenantIds=%d", env.customer.ID), agentToken, nil)
	require.Equal(t, http.StatusOK, listStatus, listBody)
	listData := decodeData(t, listBody)
	tickets, _ := listData["items"].([]interface{})
	require.Len(t, tickets, 1, "工作台应按 provider ∩ allocation 收窄到 1 条")
	first, _ := tickets[0].(map[string]interface{})
	assert.Equal(t, float64(env.ticket.ID), first["id"])

	// 6) summary：可见客户数 = 分配客户数（N=1/N=2 剧本同构）。
	summaryStatus, summaryBody := mspE2ERequest(t, engine, http.MethodGet, "/api/v1/msp/workbench/summary", agentToken, nil)
	require.Equal(t, http.StatusOK, summaryStatus, summaryBody)
	summaryData := decodeData(t, summaryBody)
	summaryCustomers, _ := summaryData["customers"].([]interface{})
	require.Len(t, summaryCustomers, 2, "summary 应只含已分配客户（C1/C2）")

	// 7) 条目操作（指派语义=指派给当前技术员）：已分配员工 → 成功且落 managed_by_user_id。
	assignStatus, assignBody := mspE2ERequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/msp/tickets/%d/assign", env.ticket.ID), agentToken,
		map[string]interface{}{"customerTenantId": env.customer.ID})
	require.Equal(t, http.StatusOK, assignStatus, assignBody)
	updated, err := client.Ticket.Query().Where(ticket.IDEQ(env.ticket.ID)).Only(ctx)
	require.NoError(t, err)
	require.NotZero(t, updated.ManagedByUserID, "指派后应写 managed_by_user_id（R11 第四字段）")
	assert.Equal(t, env.agent.ID, updated.ManagedByUserID)

	// 7.1) A12 通知双投递：工作台回复 → provider 侧通知落 provider 租户（客户侧仍落客户租户）。
	replyStatus, replyBody := mspE2ERequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/msp/tickets/%d/reply", env.ticket.ID), agentToken,
		map[string]interface{}{"customerTenantId": env.customer.ID, "content": "A12 e2e 回复"})
	require.Equal(t, http.StatusOK, replyStatus, replyBody)
	providerSide, err := client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.provider.ID), entNotification.UserIDEQ(env.providerAdmin.ID)).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, providerSide, "provider 侧通知应落 provider 租户（A12 双投递）")
	assert.Equal(t, "/msp/workbench", providerSide[0].ActionURL)
	// 回复者即托管处理人 → provider 侧不给自己发（actor 排除）。
	agentSelf, err := client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.provider.ID), entNotification.UserIDEQ(env.agent.ID)).
		All(ctx)
	require.NoError(t, err)
	assert.Empty(t, agentSelf, "评论者本人不应收到 provider 侧评论通知")

	// 7.2) 改状态：客户侧 requester 收状态通知 + provider 侧再投递一份。
	statusStatus, statusBody := mspE2ERequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/msp/tickets/%d/status", env.ticket.ID), agentToken,
		map[string]interface{}{"customerTenantId": env.customer.ID, "status": "in_progress"})
	require.Equal(t, http.StatusOK, statusStatus, statusBody)
	customerSide, err := client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.customer.ID), entNotification.TitleEQ("status_changed")).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, customerSide, "客户侧 requester 应收到状态变更通知")
	statusProviderSide, err := client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.provider.ID), entNotification.UserIDEQ(env.agent.ID),
			entNotification.TitleEQ("status_changed")).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, statusProviderSide, "provider 侧处理人应收到状态变更通知")

	// 8) 拒绝路径（A12）：未分配员工对 C1 的列表与指派一律 403 + MSP_ALLOCATION_REQUIRED。
	agent2Token := mspE2EToken(t, env.agent2)
	denyListStatus, denyListBody := mspE2ERequest(t, engine, http.MethodGet,
		fmt.Sprintf("/api/v1/msp/workbench/tickets?customerTenantIds=%d", env.customer.ID), agent2Token, nil)
	assert.Equal(t, http.StatusForbidden, denyListStatus, denyListBody)
	assert.Contains(t, denyListBody, "MSP_ALLOCATION_REQUIRED")
	denyAssignStatus, denyAssignBody := mspE2ERequest(t, engine, http.MethodPost,
		fmt.Sprintf("/api/v1/msp/tickets/%d/assign", env.ticket.ID), agent2Token,
		map[string]interface{}{"customerTenantId": env.customer.ID})
	assert.Equal(t, http.StatusForbidden, denyAssignStatus, denyAssignBody)
	assert.Contains(t, denyAssignBody, "MSP_ALLOCATION_REQUIRED")

	// 9) N=2：跨 provider 收窄——P2 员工对 P1 客户 403；不带筛选仅见本 provider 客户的工单。
	if nProviders == 2 {
		agent3Token := mspE2EToken(t, env.agent3)
		crossStatus, crossBody := mspE2ERequest(t, engine, http.MethodGet,
			fmt.Sprintf("/api/v1/msp/workbench/tickets?customerTenantIds=%d", env.customer.ID), agent3Token, nil)
		assert.Equal(t, http.StatusForbidden, crossStatus, crossBody)
		assert.Contains(t, crossBody, "MSP_ALLOCATION_REQUIRED")

		ownStatus, ownBody := mspE2ERequest(t, engine, http.MethodGet, "/api/v1/msp/workbench/tickets", agent3Token, nil)
		require.Equal(t, http.StatusOK, ownStatus, ownBody)
		ownTickets, _ := decodeData(t, ownBody)["items"].([]interface{})
		require.Len(t, ownTickets, 1)
		ownFirst, _ := ownTickets[0].(map[string]interface{})
		assert.Equal(t, float64(env.ticketID3), ownFirst["id"], "P2 员工只能看到 P2 客户（provider 收窄）")

		ownSummaryStatus, ownSummaryBody := mspE2ERequest(t, engine, http.MethodGet, "/api/v1/msp/workbench/summary", agent3Token, nil)
		require.Equal(t, http.StatusOK, ownSummaryStatus, ownSummaryBody)
		ownCustomers, _ := decodeData(t, ownSummaryBody)["customers"].([]interface{})
		assert.Len(t, ownCustomers, 1, "P2 员工 summary 仅 1 个客户（C3）")
	}

	return &mspE2EOutcome{
		WorkbenchListStatus: listStatus,
		WorkbenchListCount:  len(tickets),
		SummaryStatus:       summaryStatus,
		SummaryCustomers:    len(summaryCustomers),
		AssignStatus:        assignStatus,
	}
}

// TestMSP_A11_SameScenarioForN1AndN2 A11：同一套剧本在 N=1 与 N=2 下行为指纹一致。
func TestMSP_A11_SameScenarioForN1AndN2(t *testing.T) {
	outcomeN1 := runScenario(t, 1)
	outcomeN2 := runScenario(t, 2)
	assert.Equal(t, outcomeN1, outcomeN2, "A11：N=1/N=2 必须走同一套请求序列并得到同构结果（N=1 无额外步骤）")
}

// TestMSP_A12_ProviderScopedTicketFlow 在 N=2 下显式锚定 A12 断言（供验收/报告引用）。
func TestMSP_A12_ProviderScopedTicketFlow(t *testing.T) {
	outcome := runScenario(t, 2)
	assert.Equal(t, http.StatusOK, outcome.WorkbenchListStatus)
	assert.Equal(t, 1, outcome.WorkbenchListCount, "工作台可见且仅见本 provider ∩ allocation 单据")
	assert.Equal(t, http.StatusOK, outcome.AssignStatus, "指派校验：assigner ∈ allocation ∧ provider")
}

// ---- 夹具 ----

// mustProviderAdmin 创建 provider 租户管理员（A12 双投递的 provider 侧收件人）。
func mustProviderAdmin(t *testing.T, client *ent.Client, username string, tenantID int) *ent.User {
	t.Helper()
	entity, err := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash("hash").SetActive(true).
		SetTenantID(tenantID).SetRole("admin").SetMspRole(user.MspRole("provider_admin")).
		Save(context.Background())
	require.NoError(t, err)
	return entity
}

func mustTenant(t *testing.T, client *ent.Client, name, code, tenantType string, providerID ...int) *ent.Tenant {
	t.Helper()
	builder := client.Tenant.Create().
		SetName(name).SetCode(code).SetType(tenant.Type(tenantType)).SetStatus("active")
	if len(providerID) > 0 {
		builder = builder.SetMspProviderID(providerID[0])
	}
	entity, err := builder.Save(context.Background())
	require.NoError(t, err)
	return entity
}

// mustMSPUser 创建 provider 员工：账号 role=agent（JWT 主角色），并挂 m2m RBAC 角色
// msp_tech（RBACMiddleware 的路径预检按 m2m 角色并集判定，见 Phase 1 多角色）。
func mustMSPUser(t *testing.T, client *ent.Client, username string, tenantID, roleID int) *ent.User {
	t.Helper()
	ctx := context.Background()
	entity, err := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash("hash").SetActive(true).
		SetTenantID(tenantID).SetRole("agent").SetMspRole(user.MspRole("provider_agent")).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(entity.ID).AddRoleIDs(roleID).Save(ctx)
	require.NoError(t, err)
	return entity
}

func mustCustomerUser(t *testing.T, client *ent.Client, username string, tenantID int) *ent.User {
	t.Helper()
	entity, err := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash("hash").SetActive(true).SetRole("end_user").
		SetTenantID(tenantID).
		Save(context.Background())
	require.NoError(t, err)
	return entity
}
