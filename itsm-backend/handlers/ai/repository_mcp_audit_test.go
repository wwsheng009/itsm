package ai_test

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"
	"itsm-backend/ent/toolinvocation"
	"itsm-backend/handlers/ai"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// M0-11：tool_invocations 的 MCP 审计字段真实落库 + 按 provider/服务器筛选（DB 往返）。
func TestEntRepository_MCPAuditRoundTrip(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ai_repo_mcp_audit?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := ai.NewEntRepository(client)
	ctx := context.Background()

	// tool_invocations.user_id 是到 users 的外键（可选但显式写入时必须有真实行）；
	// 先建租户与用户，保持与生产链路一致的归属完整性。
	tenant, err := client.Tenant.Create().SetCode("mcp-audit").SetName("MCP Audit").Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("mcp-audit-user").
		SetEmail("mcp-audit@example.com").
		SetName("MCP Audit User").
		SetPasswordHash("x").
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	created, err := repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:         tenant.ID,
		UserID:           user.ID,
		ToolName:         "mcp__gitlab__list_issues",
		Arguments:        `{"token":"raw-plain-secret"}`,
		ArgsRedacted:     `{"token":"****"}`,
		Status:           "executed",
		ApprovalState:    "auto",
		PermissionCheck:  "passed",
		RoleSnapshot:     "admin",
		Provider:         "mcp",
		McpServerName:    "gitlab",
		McpRawToolName:   "list_issues",
		McpCallableName:  "mcp__gitlab__list_issues",
		OutputSummary:    `{"issues":[]}`,
		DurationMs:       42,
		ErrorCode:        "",
		NeedsApproval:    false,
		PermissionReason: "",
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "mcp", created.Provider)
	assert.Equal(t, "gitlab", created.McpServerName)
	assert.Equal(t, int64(42), created.DurationMs)

	// 审计查询断言：按 provider + 服务器筛选（管理/审计页的检索路径）。
	rows, err := client.ToolInvocation.Query().
		Where(
			toolinvocation.TenantID(tenant.ID),
			toolinvocation.ProviderEQ("mcp"),
			toolinvocation.McpServerNameEQ("gitlab"),
		).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "list_issues", rows[0].McpRawToolName)
	assert.Equal(t, "mcp__gitlab__list_issues", rows[0].McpCallableName)
	assert.Equal(t, `{"token":"****"}`, rows[0].ArgsRedacted)
	assert.NotContains(t, rows[0].ArgsRedacted, "raw-plain-secret")
	assert.Equal(t, 42, rows[0].DurationMs)

	// 不同服务器/内置工具不应命中该筛选（口径无误伤）。
	_, err = repo.CreateToolInvocation(ctx, &ai.ToolInvocation{
		TenantID:      tenant.ID,
		UserID:        user.ID,
		ToolName:      "list_tickets",
		Status:        "executed",
		ApprovalState: "auto",
		Provider:      "builtin",
	})
	require.NoError(t, err)
	rows, err = client.ToolInvocation.Query().
		Where(toolinvocation.ProviderEQ("mcp"), toolinvocation.TenantID(tenant.ID)).
		All(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "内置工具行不落入 mcp provider 筛选")
}
