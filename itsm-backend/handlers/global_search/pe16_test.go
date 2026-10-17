package global_search

import (
	"context"
	"net/http"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSearchRouterAs 与 newSearchRouter 同构，但注入用户与角色（可见性用例）。
func newSearchRouterAs(client *ent.Client, tenantID, userID int, roles []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		ctx.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: tenantID})
		ctx.Set("user_id", userID)
		if len(roles) > 0 {
			ctx.Set("roles", roles)
		}
		ctx.Next()
	})
	NewHandler(NewService(client)).RegisterRoutes(router.Group("/api/v1"))
	return router
}

func seedSearchProject(t *testing.T, client *ent.Client, tenantID, managerID int, name, code string) *ent.Project {
	t.Helper()
	p, err := client.Project.Create().
		SetName(name).SetCode(code).SetTenantID(tenantID).SetManagerID(managerID).
		Save(context.Background())
	require.NoError(t, err)
	return p
}

func seedSearchProjectMember(t *testing.T, client *ent.Client, tenantID, projectID, userID int) {
	t.Helper()
	_, err := client.ProjectMember.Create().
		SetTenantID(tenantID).SetProjectID(projectID).SetUserID(userID).
		Save(context.Background())
	require.NoError(t, err)
}

func seedSearchTask(t *testing.T, client *ent.Client, tenantID, projectID int, name string) *ent.ProjectTask {
	t.Helper()
	task, err := client.ProjectTask.Create().
		SetTenantID(tenantID).SetProjectID(projectID).SetName(name).
		Save(context.Background())
	require.NoError(t, err)
	return task
}

// TestSearchFindsProjectsAndTasksWithVisibilityPredicate PE-16：
// 项目（名称/编号）与项目任务（任务名）纳入全局搜索，且强制可见性谓词（非管理角色仅成员 ∪ 项目经理）。
func TestSearchFindsProjectsAndTasksWithVisibilityPredicate(t *testing.T) {
	client := newSearchTestClient(t)
	tenantA, userA := createSearchTenantAndUser(t, client, "pa")
	tenantB, userB := createSearchTenantAndUser(t, client, "pb")
	_, userC := createSearchTenantAndUser(t, client, "pc") // A2 项目经理（非成员）
	_, userD := createSearchTenantAndUser(t, client, "pd") // 无任何项目

	projA1 := seedSearchProject(t, client, tenantA, userA, "迁移改造项目", "PRJ-MIG-1")
	seedSearchProjectMember(t, client, tenantA, projA1.ID, userA)
	taskA1 := seedSearchTask(t, client, tenantA, projA1.ID, "迁移任务一")
	projA2 := seedSearchProject(t, client, tenantA, userC, "迁移隐藏项目", "PRJ-MIG-2")
	taskA2 := seedSearchTask(t, client, tenantA, projA2.ID, "迁移隐藏任务")
	projB1 := seedSearchProject(t, client, tenantB, userB, "迁移B项目", "PRJ-B")
	taskB1 := seedSearchTask(t, client, tenantB, projB1.ID, "迁移B任务")

	// 非管理成员：仅命中自己参与的项目（A1）与其任务。
	status, response := performSearch(t, newSearchRouterAs(client, tenantA, userA, []string{"member"}), "/api/v1/global-search?q=迁移")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Results, 2)
	assert.Equal(t, "project", response.Data.Results[0].Type)
	assert.Equal(t, projA1.ID, response.Data.Results[0].ID)
	assert.Equal(t, "PRJ-MIG-1", response.Data.Results[0].Code)
	assert.Equal(t, "project_task", response.Data.Results[1].Type)
	assert.Equal(t, taskA1.ID, response.Data.Results[1].ID)
	assert.Equal(t, projA1.ID, response.Data.Results[1].ProjectID)
	assert.Equal(t, "迁移改造项目", response.Data.Results[1].ProjectName)

	// 项目经理（非成员）按 manager_id 权威可见其项目。
	status, response = performSearch(t, newSearchRouterAs(client, tenantA, userC, []string{"member"}), "/api/v1/global-search?q=迁移")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, response.Data.Total)
	assert.Equal(t, projA2.ID, response.Data.Results[0].ID)
	assert.Equal(t, taskA2.ID, response.Data.Results[1].ID)

	// 无项目用户：项目/任务 0 命中（fail-closed）。
	status, response = performSearch(t, newSearchRouterAs(client, tenantA, userD, []string{"member"}), "/api/v1/global-search?q=迁移")
	require.Equal(t, http.StatusOK, status)
	require.Zero(t, response.Data.Total)

	// admin：租户内全量（A1+A2 项目 + 两条任务），仍受租户隔离。
	status, response = performSearch(t, newSearchRouterAs(client, tenantA, userD, []string{"admin"}), "/api/v1/global-search?q=迁移")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 4, response.Data.Total)

	// 编号命中：项目 code 也是搜索词。
	status, response = performSearch(t, newSearchRouterAs(client, tenantA, userA, []string{"member"}), "/api/v1/global-search?q=PRJ-MIG-1")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 1, response.Data.Total)
	assert.Equal(t, "project", response.Data.Results[0].Type)

	// 跨租户：B 租户用户仅看到 B 项目/任务。
	status, response = performSearch(t, newSearchRouterAs(client, tenantB, userB, []string{"member"}), "/api/v1/global-search?q=迁移")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, response.Data.Total)
	assert.Equal(t, projB1.ID, response.Data.Results[0].ID)
	assert.Equal(t, taskB1.ID, response.Data.Results[1].ID)
}