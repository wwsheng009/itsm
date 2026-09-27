package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M0-11：管理操作审计落库（audit_logs，resource=mcp）+ 纵深脱敏。

func TestEntAuditSink_RecordsMaskedEntry(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:mcp_audit_ent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	sink := NewEntAuditSink(client)
	require.NotNil(t, sink)

	at := time.Now().Add(-time.Minute)
	err := sink.RecordMCPAudit(context.Background(), AuditEntry{
		TenantID:   1,
		ActorID:    9,
		Action:     "server.create",
		ObjectType: "mcp_server",
		ObjectID:   "12",
		Before:     map[string]interface{}{},
		After: map[string]interface{}{
			"name":       "gitlab",
			"transport":  "streamable_http",
			"token":      "plain-token-should-never-persist",
			"headers":    map[string]interface{}{"Authorization": "Bearer abc123"},
			"long_value": strings.Repeat("x", 4096),
		},
		Result: "success",
		IP:     "10.0.0.1",
		At:     at,
	})
	require.NoError(t, err)

	rows, err := client.AuditLog.Query().All(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := rows[0]

	assert.Equal(t, "mcp", row.Resource)
	assert.Equal(t, "server.create", row.Action)
	assert.Equal(t, "MCP_ADMIN", row.Method)
	assert.Equal(t, "/api/v1/ai/mcp-servers/12", row.Path)
	assert.Equal(t, 200, row.StatusCode)
	assert.Equal(t, 1, row.TenantID)
	assert.Equal(t, 9, row.UserID)
	assert.Equal(t, "10.0.0.1", row.IP)
	assert.WithinDuration(t, at, row.CreatedAt, time.Second)

	body := row.RequestBody
	require.NotNil(t, body)
	assert.NotContains(t, *body, "plain-token-should-never-persist", "凭据明文不得入审计")
	assert.NotContains(t, *body, "Bearer abc123", "Authorization 头不得入审计")
	assert.Contains(t, *body, "****")
	assert.Contains(t, *body, "gitlab")
	assert.NotContains(t, *body, strings.Repeat("x", 4096), "超长值必须截断")
	assert.LessOrEqual(t, len(*body), redactBodyLimitForTest(), "审计体必须限额")
}

func TestEntAuditSink_FailureStatusAndNilClient(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:mcp_audit_ent_fail?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	sink := NewEntAuditSink(client)
	err := sink.RecordMCPAudit(context.Background(), AuditEntry{
		TenantID:   2,
		Action:     "server.enable",
		ObjectType: "mcp_server",
		ObjectID:   "3",
		Result:     "failure",
		ErrorCode:  "server_error",
	})
	require.NoError(t, err)

	rows, err := client.AuditLog.Query().All(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 500, rows[0].StatusCode)
	require.NotNil(t, rows[0].RequestBody)
	assert.Contains(t, *rows[0].RequestBody, "server_error")

	// nil client → DiscardAudit（fail-safe，不 panic、不返回 typed-nil）。
	discard := NewEntAuditSink(nil)
	require.NotNil(t, discard)
	require.NoError(t, discard.RecordMCPAudit(context.Background(), AuditEntry{}))
}

func redactBodyLimitForTest() int { return 8 * 1024 }
