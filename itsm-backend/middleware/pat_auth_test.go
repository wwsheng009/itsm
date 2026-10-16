package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// PAT 认证分支测试（方案 T12；不做 DB——用 stub 解析器）。

type stubResolver struct {
	identity *PATIdentity
	err      error
	usedV2   bool
}

func (s *stubResolver) Resolve(ctx context.Context, plaintext string) (*PATIdentity, error) {
	return s.identity, s.err
}

func (s *stubResolver) Authenticate(ctx context.Context, plaintext, clientIP string) (*PATIdentity, bool, error) {
	s.usedV2 = true
	return s.identity, true, s.err
}

func patTestRouter(t *testing.T, resolver PATAuthenticator, jwtSecret string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	original := patAuth
	ConfigurePersonalAccessTokens(resolver, true)
	t.Cleanup(func() { ConfigurePersonalAccessTokens(original, true) })

	r := gin.New()
	r.GET("/protected", AuthMiddleware(jwtSecret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":    c.GetInt("user_id"),
			"username":   c.GetString("username"),
			"role":       c.GetString("role"),
			"tenant_id":  c.GetInt("tenant_id"),
			"token_type": c.GetString(CtxKeyTokenType),
			"source":     c.GetString("audit_source"),
			"token_id":   c.GetInt(CtxKeyAuthTokenID),
		})
	})
	return r
}

func TestAuthMiddleware_PATAuthenticatesAndSetsContext(t *testing.T) {
	resolver := &stubResolver{identity: &PATIdentity{
		TokenID: 42, TokenPrefix: "itsm_pat_5Yk8mQ2v",
		UserID: 7, Username: "operator", Role: "admin", TenantID: 3,
	}}
	r := patTestRouter(t, resolver, "test-secret")

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer itsm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"user_id":7`)
	require.Contains(t, body, `"tenant_id":3`)
	require.Contains(t, body, `"role":"admin"`)
	require.Contains(t, body, `"token_type":"pat"`)
	require.Contains(t, body, `"source":"pat"`) // audit_source 归因（顺序契约：早于 AuditMiddleware）
	require.Contains(t, body, `"token_id":42`)
}

func TestAuthMiddleware_PATRejectedWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := patAuth
	ConfigurePersonalAccessTokens(&stubResolver{}, false)
	t.Cleanup(func() { ConfigurePersonalAccessTokens(original, true) })

	r := gin.New()
	r.GET("/protected", AuthMiddleware("test-secret"), func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer itsm_pat_AAA=")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "token无效")
}

func TestAuthMiddleware_PATResolveErrorIsUnified401(t *testing.T) {
	// 防枚举：不存在/已吊销/已过期统一「token无效」
	for _, name := range []string{"not-found", "revoked", "expired"} {
		t.Run(name, func(t *testing.T) {
			r := patTestRouter(t, &stubResolver{err: errPATStub}, "test-secret")
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer itsm_pat_BBB=")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Contains(t, w.Body.String(), "token无效")
		})
	}
}

var errPATStub = &patStubError{}

type patStubError struct{}

func (e *patStubError) Error() string { return "any reason" }

func TestAuthMiddleware_NonPATUnaffected(t *testing.T) {
	// JWT 路径回归：带非 PAT 前缀的 JWT 仍走 JWT 分支
	secret := "test-secret"
	jwtToken, err := GenerateAccessToken(9, "u9", "agent", 1, secret, time.Hour)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	ConfigurePersonalAccessTokens(&stubResolver{identity: &PATIdentity{}}, true)
	r := gin.New()
	r.GET("/protected", AuthMiddleware(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetInt("user_id"), "token_type": c.GetString(CtxKeyTokenType)})
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"user_id":9`)
	require.NotContains(t, w.Body.String(), `"token_type":"pat"`)
}

func TestRejectPATAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/manage", RejectPATAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	// PAT 身份 → 403
	req := httptest.NewRequest(http.MethodPost, "/manage", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req.WithContext(context.Background()))
	// 模拟 PAT 已认证（token_type=pat）
	r2 := gin.New()
	r2.POST("/manage", func(c *gin.Context) {
		c.Set(CtxKeyTokenType, TokenTypePAT)
		RejectPATAuth()(c)
		if !c.IsAborted() {
			c.Status(http.StatusOK)
		}
	}, func(c *gin.Context) { c.Status(http.StatusOK) })
	req2 := httptest.NewRequest(http.MethodPost, "/manage", nil)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusForbidden, w2.Code)

	// JWT 身份（无 token_type=pat）→ 放行
	req3 := httptest.NewRequest(http.MethodPost, "/manage", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
}

func TestSafePATPrefix(t *testing.T) {
	full := "itsm_pat_5Yk8mQ2vR7pLxN1sT4uW6zA9bC0dE3fG7hJ2kM5nP8q="
	require.Equal(t, "itsm_pat_5Yk8mQ2v", SafePATPrefix(full))
	require.NotEqual(t, full, SafePATPrefix(full))
}

func TestIsPATToken(t *testing.T) {
	require.True(t, IsPATToken("itsm_pat_AAA="))
	require.False(t, IsPATToken("eyJhbGciOiJIUzI1NiIs"))
	require.False(t, IsPATToken(""))
}
