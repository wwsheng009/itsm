package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/pat"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// PAT 端到端链路测试（方案 T12；验收 AC-13/19/39/40/42 的进程内版本）：
// 会话（JWT）创建 PAT → PAT 调管理面读 200 → PAT 调管理面写 403（防自我繁殖 D-PAT-13）
// → 吊销后 401 → 平台角色创建 403（D-PAT-14）。

type patE2E struct {
	router     *gin.Engine
	client     *ent.Client
	jwtSecret  string
	userID     int
	tenantID   int
	superID    int
	superTenID int
}

func patE2ESetup(t *testing.T, patEnabled bool) *patE2E {
	t.Helper()
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:pat_e2e_"+fmt.Sprintf("%d", time.Now().UnixNano())+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	logger := zaptest.NewLogger(t).Sugar()
	const jwtSecret = "pat-e2e-test-secret"
	ctx := context.Background()

	tn, err := client.Tenant.Create().SetName("PATTenant").SetCode("PATT1").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	u, err := client.User.Create().SetUsername("patuser").SetEmail("patuser@test.local").SetName("PAT User").
		SetRole(user.RoleAdmin).SetPasswordHash("unused").SetTenantID(tn.ID).SetActive(true).Save(ctx)
	require.NoError(t, err)
	su, err := client.User.Create().SetUsername("patsuper").SetEmail("patsuper@test.local").SetName("PAT Super").
		SetRole(user.RoleSuperAdmin).SetPasswordHash("unused").SetTenantID(tn.ID).SetActive(true).Save(ctx)
	require.NoError(t, err)

	repo := pat.NewRepository(client, logger)
	svc := pat.NewService(repo, pat.PATConfigView{Enabled: patEnabled, DefaultTTLDays: 90, MaxTTLDays: 365, MaxActivePerUser: 20}, logger)
	middleware.ConfigurePersonalAccessTokens(pat.NewResolver(client, logger), patEnabled)
	t.Cleanup(func() { middleware.ConfigurePersonalAccessTokens(nil, false) })

	router := gin.New()
	SetupRoutes(router, &RouterConfig{
		JWTSecret:  jwtSecret,
		Logger:     logger,
		Client:     client,
		PATHandler: pat.NewHandler(svc),
	})
	return &patE2E{router: router, client: client, jwtSecret: jwtSecret, userID: u.ID, tenantID: tn.ID, superID: su.ID, superTenID: tn.ID}
}

func (e *patE2E) do(t *testing.T, method, path, bearer string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *patE2E) jwt(t *testing.T, userID int, username, role string, tenantID int) string {
	t.Helper()
	token, err := middleware.GenerateAccessToken(userID, username, role, tenantID, e.jwtSecret, 15*time.Minute)
	require.NoError(t, err)
	return token
}

func TestPATRoutes_EndToEnd(t *testing.T) {
	e := patE2ESetup(t, true)

	// 1. 会话（JWT）创建 PAT
	sessionJWT := e.jwt(t, e.userID, "patuser", "admin", e.tenantID)
	w := e.do(t, http.MethodPost, "/api/v1/auth/personal-access-tokens", sessionJWT,
		[]byte(`{"name":"e2e-token","expiresInDays":30}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var createRes struct {
		Code int `json:"code"`
		Data struct {
			Token          pat.Item `json:"token"`
			PlaintextToken string   `json:"plaintextToken"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createRes))
	require.Equal(t, common.SuccessCode, createRes.Code)
	patToken := createRes.Data.PlaintextToken
	require.Contains(t, patToken, "itsm_pat_")
	tokenID := createRes.Data.Token.ID

	// 2. PAT 调管理面读（list）→ 200
	w = e.do(t, http.MethodGet, "/api/v1/auth/personal-access-tokens", patToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// 响应不含明文
	require.NotContains(t, w.Body.String(), patToken)

	// 3. 防自我繁殖（AC-39/D-PAT-13）：PAT 调 4 个写路由 → 403 且零副作用
	for _, writeCase := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/auth/personal-access-tokens", `{"name":"evil"}`},
		{http.MethodPost, "/api/v1/auth/personal-access-tokens/revoke-all", `{}`},
		{http.MethodPost, fmt.Sprintf("/api/v1/auth/personal-access-tokens/%d/rotate", tokenID), `{}`},
		{http.MethodPost, fmt.Sprintf("/api/v1/auth/personal-access-tokens/%d/revoke", tokenID), `{}`},
	} {
		w := e.do(t, writeCase.method, writeCase.path, patToken, []byte(writeCase.body))
		require.Equal(t, http.StatusForbidden, w.Code, writeCase.path+": "+w.Body.String())
	}
	// 零副作用：令牌仍 active
	w = e.do(t, http.MethodGet, "/api/v1/auth/personal-access-tokens", sessionJWT, nil)
	require.Contains(t, w.Body.String(), `"status":"active"`)

	// 4. 平台角色禁令（AC-40/D-PAT-14）
	superJWT := e.jwt(t, e.superID, "patsuper", "super_admin", e.superTenID)
	w = e.do(t, http.MethodPost, "/api/v1/auth/personal-access-tokens", superJWT, []byte(`{"name":"super"}`))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// 5. 吊销后 401（AC-19）
	w = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/auth/personal-access-tokens/%d/revoke", tokenID), sessionJWT, []byte(`{}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.do(t, http.MethodGet, "/api/v1/auth/personal-access-tokens", patToken, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// 6. logout 无害性（AC-42）：PAT 调 logout → 401 且令牌已吊销状态不变
	//    （吊销态即 401，此用例在 238 demo Runbook 中用 active 令牌验证）
}

func TestPATRoutes_DisabledReturns401(t *testing.T) {
	e := patE2ESetup(t, false)
	w := e.do(t, http.MethodGet, "/api/v1/auth/personal-access-tokens", "itsm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
