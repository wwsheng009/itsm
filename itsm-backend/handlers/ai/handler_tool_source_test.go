// M1-02：审批接口返回「来源（内置/MCP）+ 服务器 + 原始工具名 + 投影名 + 风险」。
//
// 动机（分析报告 §6.5-5）：审批人在信息缺失下决策——只看到投影名，无法判断是哪个服务器、
// 原始工具是什么、风险级别如何。本用例锁定 list 响应的来源字段与 risk 实时解析。
package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// sourceGuardProvider 是最小 MCP provider 替身：只提供工具面解析（不执行），
// 用于让 handler 的 risk 实时解析命中一个真实定义。
type sourceGuardProvider struct {
	def service.ToolDefinition
}

func (s sourceGuardProvider) ProviderName() string { return "mcp" }

func (s sourceGuardProvider) ListTools(_ context.Context, _ int) []service.ToolDefinition {
	return []service.ToolDefinition{s.def}
}

func (s sourceGuardProvider) Resolve(_ context.Context, _ int, name string) (*service.ToolDefinition, bool) {
	if name != s.def.Name {
		return nil, false
	}
	def := s.def
	return &def, true
}

func (s sourceGuardProvider) Execute(_ context.Context, _ int, _ string, _ map[string]interface{}) (*service.ToolExecution, error) {
	return &service.ToolExecution{Provider: "mcp"}, nil
}

func TestListToolInvocations_ExposesMCPSourceAndRisk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:tool_source_detail?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tenant := client.Tenant.Create().SetName("Tools").SetCode("tools-src").SetDomain("tools-src.test").SaveX(ctx)
	user := client.User.Create().SetUsername("approver").SetEmail("approver@example.com").
		SetName("Approver").SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	registry := service.NewToolRegistry(nil, nil, nil, nil)
	registry.RegisterProvider(sourceGuardProvider{def: service.ToolDefinition{
		Name:        "mcp__mock__create_issue",
		ReadOnly:    false,
		Resource:    "mcp",
		Action:      "write",
		Provider:    "mcp",
		ServerName:  "mock",
		RawToolName: "create_issue",
		Risk:        "act_high",
	}})
	svc := ai.NewService(ai.NewEntRepository(client), zap.NewNop().Sugar(), nil, registry, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(client)
	h := ai.NewHandler(svc)

	// 待审批记录（模拟 ExecuteTool 写路径的落库结果）。
	invocation := client.ToolInvocation.Create().
		SetTenantID(tenant.ID).
		SetUserID(user.ID).
		SetToolName("mcp__mock__create_issue").
		SetArguments(`{"title":"打印机故障","token":"s3cr3t-value"}`).
		SetArgsRedacted(`{"title":"打印机故障","token":"****"}`).
		SetStatus("pending").
		SetNeedsApproval(true).
		SetApprovalState("pending").
		SetProvider("mcp").
		SetMcpServerName("mock").
		SetMcpRawToolName("create_issue").
		SetMcpCallableName("mcp__mock__create_issue").
		SetRoleSnapshot("sysadmin").
		SaveX(ctx)

	r := gin.New()
	r.GET("/api/v1/agent/tools/invocations", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", user.ID)
		h.ListToolInvocations(c)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/agent/tools/invocations?state=pending", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var response struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]interface{} `json:"items"`
			State string                   `json:"state"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Data.Items, 1)

	item := response.Data.Items[0]
	assert.Equal(t, float64(invocation.ID), item["id"])
	assert.Equal(t, "mcp", item["provider"], "审批人必须看到来源（内置/MCP）")
	assert.Equal(t, "mock", item["serverName"], "必须看到 MCP 服务器标识")
	assert.Equal(t, "create_issue", item["rawToolName"], "必须看到原始工具名")
	assert.Equal(t, "mcp__mock__create_issue", item["callableName"], "必须看到投影名")
	assert.Equal(t, "act_high", item["risk"], "必须看到风险标注（实时解析）")
	assert.Equal(t, "sysadmin", item["roleSnapshot"])
	// 脱敏口径：列表返回 argsRedacted，且不得出现明文敏感值。
	assert.NotContains(t, w.Body.String(), "s3cr3t-value", "明文敏感值不得出现在审批列表响应中")
	assert.NotContains(t, w.Body.String(), `"arguments"`, "原始参数不得回显（仅作执行真源留存）")
	assert.Equal(t, `{"title":"打印机故障","token":"****"}`, item["argsRedacted"], "列表返回脱敏参数快照")

	// 详情接口（GET /tools/:id）与列表同一份字段装配（M1-05 对话内审批卡片依赖本接口）。
	r.GET("/api/v1/agent/tools/:id", func(c *gin.Context) {
		c.Set("tenant_id", tenant.ID)
		c.Set("user_id", user.ID)
		h.GetToolInvocation(c)
	})
	detailRecorder := httptest.NewRecorder()
	r.ServeHTTP(detailRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/agent/tools/"+strconv.Itoa(invocation.ID), nil))
	require.Equal(t, http.StatusOK, detailRecorder.Code, detailRecorder.Body.String())

	var detailResponse struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	assert.Equal(t, float64(invocation.ID), detailResponse.Data["id"])
	assert.Equal(t, "mcp", detailResponse.Data["provider"])
	assert.Equal(t, "mock", detailResponse.Data["serverName"])
	assert.Equal(t, "create_issue", detailResponse.Data["rawToolName"])
	assert.Equal(t, "mcp__mock__create_issue", detailResponse.Data["callableName"])
	assert.Equal(t, "act_high", detailResponse.Data["risk"])
	assert.Equal(t, `{"title":"打印机故障","token":"****"}`, detailResponse.Data["argsRedacted"])
	assert.NotContains(t, detailRecorder.Body.String(), "s3cr3t-value", "详情接口同样不得回显明文")
	assert.NotContains(t, detailRecorder.Body.String(), `"arguments"`)
}
