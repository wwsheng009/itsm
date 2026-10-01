package invitation

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

	"itsm-backend/ent/enttest"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// IP-P1-4b 邀请 HTTP 契约测试（§4.0-C API）：创建 → 落地页回显 → 接受 → 撤销/重放拒绝。

type apiEnvelope struct {
	Code any             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func TestInvitationHandler_LifecycleContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:invitation_http_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })

	tenantA, err := client.Tenant.Create().SetName("HTTP A").SetCode("http-a").SetStatus("active").SetType("msp_provider").Save(ctx)
	require.NoError(t, err)
	admin, err := client.User.Create().
		SetUsername("http-admin").SetEmail("http-admin@example.com").SetName("Admin").
		SetPasswordHash("x").SetActive(true).SetTenantID(tenantA.ID).SetRole("super_admin").Save(ctx)
	require.NoError(t, err)
	roleAgent, err := client.Role.Create().SetName("Agent").SetCode("agent").SetTenantID(tenantA.ID).Save(ctx)
	require.NoError(t, err)

	svc := service.NewInvitationService(client, service.NewUserService(client, logger), logger)
	h := NewHandler(svc, logger)

	withActor := func(c *gin.Context) {
		c.Set("user_id", admin.ID)
		c.Set("tenant_id", tenantA.ID)
		c.Set("role", "super_admin")
		c.Set("username", "root")
	}
	router := gin.New()
	router.POST("/api/v1/users/invitations", withActor, h.Create)
	router.POST("/api/v1/users/invitations/:id/revoke", withActor, h.Revoke)
	router.GET("/api/v1/auth/invitations/:token", h.Inspect)
	router.POST("/api/v1/auth/invitations/:token/accept", h.Accept)

	do := func(method, path, body string) (*httptest.ResponseRecorder, apiEnvelope) {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var env apiEnvelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		return rec, env
	}

	// 1) 创建（tenantId 省略 → 取上下文租户）。
	rec, env := do(http.MethodPost, "/api/v1/users/invitations", fmt.Sprintf(`{"email":"new@example.com","roleId":%d}`, roleAgent.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, float64(0), env.Code)
	var created struct {
		ID        int    `json:"id"`
		Status    string `json:"status"`
		InviteURL string `json:"inviteUrl"`
		EmailSent bool   `json:"emailSent"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &created))
	assert.Equal(t, "pending", created.Status)
	assert.False(t, created.EmailSent)
	require.True(t, strings.Contains(created.InviteURL, "/invite/"))
	token := created.InviteURL[strings.LastIndex(created.InviteURL, "/")+1:]

	// 2) 落地页最小回显。
	rec, env = do(http.MethodGet, "/api/v1/auth/invitations/"+token, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var info map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &info))
	assert.Equal(t, "n***@example.com", info["emailMasked"])
	assert.Equal(t, "pending", info["status"])

	// 3) 接受（设置密码 + 建号 + membership）。
	rec, env = do(http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"password":"Str0ng!Pass2026","name":"New User"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var accepted map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &accepted))
	assert.Equal(t, "accepted", accepted["status"])
	assert.Greater(t, accepted["userId"].(float64), float64(0))
	assert.Greater(t, accepted["membershipId"].(float64), float64(0))

	// 4) 重放 → 409 INVITATION_ALREADY_ACCEPTED。
	rec, env = do(http.MethodPost, "/api/v1/auth/invitations/"+token+"/accept", `{"password":"Str0ng!Pass2026"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, service.InvitationCodeAlreadyAccepted, env.Code)

	// 5) 撤销 → 接受 410 INVITATION_REVOKED。
	rec, env = do(http.MethodPost, "/api/v1/users/invitations", fmt.Sprintf(`{"email":"revoke@example.com","roleId":%d}`, roleAgent.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var second struct {
		ID        int    `json:"id"`
		InviteURL string `json:"inviteUrl"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &second))
	secondToken := second.InviteURL[strings.LastIndex(second.InviteURL, "/")+1:]

	rec, env = do(http.MethodPost, fmt.Sprintf("/api/v1/users/invitations/%d/revoke", second.ID), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var revoked map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &revoked))
	assert.Equal(t, "revoked", revoked["status"])

	rec, env = do(http.MethodPost, "/api/v1/auth/invitations/"+secondToken+"/accept", `{"password":"Str0ng!Pass2026"}`)
	assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
	assert.Equal(t, service.InvitationCodeRevoked, env.Code)

	// 6) 非法 token → 404。
	rec, env = do(http.MethodGet, "/api/v1/auth/invitations/not-a-token", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Equal(t, service.InvitationCodeNotFound, env.Code)
}
