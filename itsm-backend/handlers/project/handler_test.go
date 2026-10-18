package project

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var handlerTestCounter int64

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// newHandlerEnv 构造 gin 测试路由 + sqlite 后端（租户上下文由中间件注入）。
func newHandlerEnv(t *testing.T) (*gin.Engine, *ent.Client, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:project_handler_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&handlerTestCounter, 1))
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenant, err := client.Tenant.Create().
		SetName("Handler 测试租户").
		SetCode(fmt.Sprintf("HT%d", atomic.AddInt64(&handlerTestCounter, 1))).
		SetStatus("active").
		Save(context.Background())
	require.NoError(t, err)

	admin, err := client.User.Create().
		SetUsername(fmt.Sprintf("ht-admin-%d", tenant.ID)).
		SetEmail(fmt.Sprintf("ht-admin-%d@example.com", tenant.ID)).
		SetName("测试管理员").
		SetPasswordHash("x").
		SetRole("admin").
		SetTenantID(tenant.ID).
		Save(context.Background())
	require.NoError(t, err)
	// 管理员也应有本租户活跃 membership（成员前置校验/加入项目的前提）。
	_, err = client.UserTenantMembership.Create().
		SetUserID(admin.ID).
		SetTenantID(tenant.ID).
		SetSource(usertenantmembership.SourceHome).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetStatus(usertenantmembership.StatusActive).
		Save(context.Background())
	require.NoError(t, err)

	svc := NewService(NewEntRepository(client))
	// PE-15：结项清单端口（复盘 KB 草稿 / CSAT 调研）——测试用假实现，避免外部依赖。
	svc.SetClosurePorts(&fakeClosureKnowledge{articleID: 7001}, &fakeClosureSurvey{surveyID: 8001})
	h := NewHandler(svc)
	h.SetAuditClient(client)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenant.ID})
		c.Set("user_id", admin.ID)
		c.Set("role", "admin")
		c.Next()
	})
	r.POST("/projects", h.CreateProject)
	r.GET("/projects", h.ListProjects)
	r.GET("/projects/:id", h.GetProject)
	r.GET("/projects/:id/hours-summary", h.HoursSummary)
	r.PUT("/projects/:id/capacity", h.UpdateCapacity)
	r.GET("/projects/:id/resource-heatmap", h.ResourceHeatmap)
	// P2（PE-24）：组合视图与度量。
	r.GET("/projects/portfolio", h.Portfolio)
	r.GET("/projects/metrics", h.Metrics)
	r.PUT("/projects/:id", h.UpdateProject)
	r.DELETE("/projects/:id", h.DeleteProject)
	r.POST("/projects/:id/status", h.TransitionStatus)
	r.GET("/projects/:id/gates", h.ListGates)
	r.POST("/projects/:id/gates/:gate/submit", h.SubmitGate)
	r.POST("/projects/:id/gates/:gate/decision", h.DecideGate)
	r.GET("/projects/:id/members", h.ListMembers)
	r.POST("/projects/:id/members", h.AddMember)
	r.PATCH("/projects/:id/members/:userId", h.UpdateMemberRole)
	r.DELETE("/projects/:id/members/:userId", h.RemoveMember)
	r.GET("/projects/:id/member-candidates", h.MemberCandidates)
	r.GET("/projects/my-tasks", h.MyTasks)
	r.GET("/projects/my-milestones", h.MyMilestones)
	r.GET("/projects/:id/tasks", h.ListTasks)
	r.POST("/projects/:id/tasks", h.CreateTask)
	r.GET("/projects/:id/tasks/:taskId", h.GetTask)
	r.PUT("/projects/:id/tasks/:taskId", h.UpdateTask)
	r.DELETE("/projects/:id/tasks/:taskId", h.DeleteTask)
	r.POST("/projects/:id/tasks/:taskId/status", h.TransitionTask)
	r.GET("/projects/:id/records", h.ListProjectRecords)
	r.POST("/projects/:id/tasks/:taskId/tickets", h.GenerateTaskTicket)
	r.DELETE("/projects/:id/tasks/:taskId/tickets", h.UnbindTaskTicket)
	r.POST("/tickets/:id/project", h.AttachTicket)
	r.DELETE("/tickets/:id/project", h.DetachTicket)
	r.GET("/tickets/:id/project", h.GetTicketProject)
	r.GET("/projects/:id/milestones", h.ListMilestones)
	r.POST("/projects/:id/milestones", h.CreateMilestone)
	r.PUT("/projects/:id/milestones/:milestoneId", h.UpdateMilestone)
	r.POST("/projects/:id/milestones/:milestoneId/status", h.TransitionMilestone)
	r.DELETE("/projects/:id/milestones/:milestoneId", h.DeleteMilestone)
	r.GET("/projects/:id/dependencies", h.ListDependencies)
	r.POST("/projects/:id/tasks/:taskId/dependencies", h.AddDependency)
	r.DELETE("/projects/:id/tasks/:taskId/dependencies/:dependsOnTaskId", h.RemoveDependency)
	r.GET("/projects/:id/baselines", h.ListBaselines)
	r.POST("/projects/:id/baselines", h.CreateBaseline)
	r.GET("/projects/:id/baselines/diff", h.BaselineDiff)
	r.GET("/projects/templates", h.ListTemplates)
	r.POST("/projects/templates", h.CreateTemplate)
	r.GET("/projects/templates/:templateId", h.GetTemplate)
	r.PUT("/projects/templates/:templateId", h.UpdateTemplate)
	r.DELETE("/projects/templates/:templateId", h.DeleteTemplate)
	r.POST("/projects/:id/templates/apply", h.ApplyTemplate)
	r.GET("/projects/:id/closure", h.GetClosure)
	r.POST("/projects/:id/closure/retro", h.GenerateRetroDraft)
	r.POST("/projects/:id/closure/assets", h.ConfirmClosureAssets)
	r.POST("/projects/:id/closure/members/recycle", h.RecycleClosureMembers)
	r.POST("/projects/:id/closure/csat", h.TriggerClosureCSAT)
	r.GET("/projects/report-templates", h.ListReportTemplates)
	r.POST("/projects/report-templates", h.CreateReportTemplate)
	r.GET("/projects/report-templates/:templateId", h.GetReportTemplate)
	r.PUT("/projects/report-templates/:templateId", h.UpdateReportTemplate)
	r.DELETE("/projects/report-templates/:templateId", h.DeleteReportTemplate)
	r.GET("/projects/report-groups", h.ReportGroups)
	r.POST("/projects/:id/reports/generate", h.GenerateProjectReport)
	r.GET("/projects/:id/risks", h.ListRisks)
	r.POST("/projects/:id/risks", h.CreateRisk)
	r.GET("/projects/:id/risks/:riskId", h.GetRisk)
	r.PUT("/projects/:id/risks/:riskId", h.UpdateRisk)
	r.DELETE("/projects/:id/risks/:riskId", h.DeleteRisk)
	r.POST("/projects/:id/risks/:riskId/status", h.TransitionRisk)
	r.POST("/projects/:id/risks/:riskId/convert", h.ConvertRisk)
	r.POST("/projects/:id/ai/summary", h.GenerateAISummary)
	r.POST("/projects/:id/ai/risk-warnings", h.GenerateAIRiskWarnings)
	r.POST("/projects/:id/ai/minutes/tasks", h.ExtractAIMinutesTasks)
	// P2（PE-26）：采购/资产结项登记。
	r.GET("/projects/:id/asset-links", h.ListAssetLinks)
	r.POST("/projects/:id/asset-links", h.CreateAssetLink)
	r.DELETE("/projects/:id/asset-links/:linkId", h.DeleteAssetLink)
	r.GET("/projects/:id/asset-link-candidates", h.ListAssetLinkCandidates)
	return r, client, tenant.ID
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "body=%s", w.Body.String())
	return w, env
}

// TestHandlerCreateAndList 创建（body 化）+ 列表分页聚合别名。
func TestHandlerCreateAndList(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	w, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "数据中台建设", "type": "improvement"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, common.SuccessCode, env.Code)

	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))
	assert.Equal(t, StatusPlanning, created.Status)
	assert.Equal(t, "improvement", created.Type)

	w, env = doJSON(t, r, http.MethodGet, "/projects?page=1&pageSize=10&type=improvement", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Items    []ProjectResponse `json:"items"`
		Total    int64             `json:"total"`
		Projects []ProjectResponse `json:"projects"` // 领域别名（inferDomainAlias）
	}
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Len(t, list.Items, 1)
	assert.Equal(t, int64(1), list.Total)
	assert.Len(t, list.Projects, 1, "领域别名 projects 应存在")
}

// TestHandlerTransition_BlockedWithBlockers 结项门禁：409 + blockers 明细；不存在 → 404。
func TestHandlerTransition_BlockedWithBlockers(t *testing.T) {
	r, client, tid := newHandlerEnv(t)

	ctx := context.Background()
	admin, err := client.User.Query().First(ctx)
	require.NoError(t, err)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "上线项目", "managerId": admin.ID})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// PE-12：立项门未通过 → activate 409 + 门禁明细。
	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/status", created.ID), map[string]any{"action": ActionActivate})
	require.Equal(t, http.StatusConflict, w.Code)
	var gateBlocked struct {
		Gate     string        `json:"gate"`
		Blockers []GateBlocker `json:"blockers"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &gateBlocked))
	assert.Equal(t, GateInitiation, gateBlocked.Gate)
	require.NotEmpty(t, gateBlocked.Blockers)

	// 登记成员 + 提交立项门（无审批端口 → 自动通过）。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": admin.ID, "projectRole": "pm"})
	require.Equal(t, http.StatusOK, w.Code)
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/gates/initiation/submit", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var initiation ProjectGateResponse
	require.NoError(t, json.Unmarshal(env.Data, &initiation))
	assert.Equal(t, GateStatusPassed, initiation.Status)

	// planning → active
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/status", created.ID), map[string]any{"action": ActionActivate})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, common.SuccessCode, env.Code)

	// 注入未终结任务 → complete 被门禁阻断
	_, err = client.ProjectTask.Create().SetTenantID(tid).SetProjectID(created.ID).SetName("演练").Save(ctx)
	require.NoError(t, err)

	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/status", created.ID), map[string]any{"action": ActionComplete})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, common.ConflictCode, env.Code)
	var blocked struct {
		Blockers []Blocker `json:"blockers"`
		Total    int       `json:"total"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &blocked))
	assert.NotEmpty(t, blocked.Blockers)
	assert.Equal(t, 1, blocked.Total)

	// 不存在 → 404
	w, env = doJSON(t, r, http.MethodGet, "/projects/999999", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, common.NotFoundCode, env.Code)
}

// TestHandlerUpdateCASConflict 乐观锁冲突 → 409 + currentVersion。
func TestHandlerUpdateCASConflict(t *testing.T) {
	r, _, _ := newHandlerEnv(t)
	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "CAS 项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	stale := 999
	w, env := doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/%d", created.ID), map[string]any{"name": "改名", "version": stale})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, common.ConflictCode, env.Code)
	var detail struct {
		CurrentVersion int `json:"currentVersion"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &detail))
	assert.Equal(t, created.Version, detail.CurrentVersion)
}
