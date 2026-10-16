package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// PAT 审计归因测试（D-PAT-9，实施 T6）：
// source=pat 的请求 → 审计行带 token_id；dedupe 模式 60s 窗口去重；
// 顺序契约：audit_source 在 AuthMiddleware（PAT 分支）写入、AuditMiddleware 读取。

func TestAuditMiddleware_PATAttribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:pat_audit?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	// 复位 dedupe 闸门（避免与其他测试串扰）
	patUsageMu.Lock()
	patUsageMem = map[string]time.Time{}
	patUsageMu.Unlock()
	ConfigurePATUsageAuditMode("dedupe")
	t.Cleanup(func() { ConfigurePATUsageAuditMode("dedupe") })

	run := func(method, path string, tokenID int) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(method, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Set("tenant_id", 7)
		c.Set("user_id", 42)
		c.Set("username", "patuser")
		c.Set("audit_source", AuditSourcePAT)
		c.Set(CtxKeyAuthTokenID, tokenID)
		AuditMiddleware(client)(c)
	}

	// POST：PAT 请求落行且带 token_id
	run("POST", "/api/v1/tickets", 42)
	rows, err := client.AuditLog.Query().
		Where(auditlog.TenantIDEQ(7), auditlog.SourceEQ(AuditSourcePAT)).
		All(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].TokenID)
	require.Equal(t, 42, rows[0].TokenID)

	// dedupe：60s 内同一 (token, method, path) 再次请求 → 不新增行
	run("POST", "/api/v1/tickets", 42)
	rows2, _ := client.AuditLog.Query().
		Where(auditlog.TenantIDEQ(7), auditlog.SourceEQ(AuditSourcePAT)).
		Count(context.Background())
	require.Equal(t, 1, rows2)

	// 不同路径 → 新行
	run("POST", "/api/v1/tickets/9/comments", 42)
	rows3, _ := client.AuditLog.Query().
		Where(auditlog.TenantIDEQ(7), auditlog.SourceEQ(AuditSourcePAT)).
		Count(context.Background())
	require.Equal(t, 2, rows3)

	// JWT 请求（无 PAT 归因）→ source != pat，无 token_id
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "/api/v1/users", strings.NewReader(`{}`))
	c.Request = req
	c.Set("tenant_id", 7)
	c.Set("user_id", 43)
	c.Set("username", "jwtuser")
	AuditMiddleware(client)(c)
	jwtRows, _ := client.AuditLog.Query().
		Where(auditlog.TenantIDEQ(7), auditlog.UserIDEQ(43)).
		All(context.Background())
	require.Len(t, jwtRows, 1)
	require.Zero(t, jwtRows[0].TokenID) // ent Optional int 非 Nillable：NULL 读回 0
	require.NotEqual(t, AuditSourcePAT, jwtRows[0].Source)
}

func TestAuditMiddleware_PATUsageModeOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:pat_audit_off?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ConfigurePATUsageAuditMode("off")
	t.Cleanup(func() { ConfigurePATUsageAuditMode("dedupe") })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "/api/v1/tickets", nil)
	c.Request = req
	c.Set("tenant_id", 7)
	c.Set("user_id", 42)
	c.Set("audit_source", AuditSourcePAT)
	c.Set(CtxKeyAuthTokenID, 9)
	AuditMiddleware(client)(c)

	n, _ := client.AuditLog.Query().Count(context.Background())
	require.Equal(t, 0, n) // off 模式下 GET 类 PAT 请求不落使用行
}

func TestAuditMiddleware_PATUsageModeAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := enttest.Open(t, "sqlite3", "file:pat_audit_all?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	ConfigurePATUsageAuditMode("all")
	t.Cleanup(func() { ConfigurePATUsageAuditMode("dedupe") })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "/api/v1/tickets", nil)
		c.Request = req
		c.Set("tenant_id", 7)
		c.Set("user_id", 42)
		c.Set("audit_source", AuditSourcePAT)
		c.Set(CtxKeyAuthTokenID, 9)
		AuditMiddleware(client)(c)
	}
	n, _ := client.AuditLog.Query().Count(context.Background())
	require.Equal(t, 3, n) // all 模式每请求一行
}
