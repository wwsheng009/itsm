package manager

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"itsm-backend/mcp/registry"
)

func TestSchemaHash_StableAcrossKeyOrder(t *testing.T) {
	first := json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"integer"}}}`)
	second := json.RawMessage(`{"properties":{"a":{"type":"integer"},"b":{"type":"string"}},"type":"object"}`)
	require.Equal(t, SchemaHash(first), SchemaHash(second), "键序不同必须得到相同哈希（避免误隔离）")

	third := json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"}}}`)
	require.NotEqual(t, SchemaHash(first), SchemaHash(third))

	broken := json.RawMessage(`{not json`)
	require.NotEmpty(t, SchemaHash(broken), "非法 JSON 退回原始字节哈希，仍必须确定")
	require.Equal(t, SchemaHash(broken), SchemaHash(broken))
}

func TestPlanDiscovery_NewToolsSafeDefaults(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	discovered := []DiscoveredTool{
		{RawName: "list_issues", Description: "list", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{RawName: "weird name!", Description: "illegal chars", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}

	result := PlanDiscovery(7, 1, "github", nil, discovered, now)
	require.Equal(t, []string{"list_issues", "weird name!"}, result.Delta.Added)
	require.Len(t, result.Records, 2)

	for _, record := range result.Records {
		require.False(t, record.Enabled, "新工具默认不启用（D7）")
		require.False(t, record.ReadOnly, "默认按非只读处理（D7）")
		require.Equal(t, "high", record.Risk)
		require.True(t, record.Healthy)
		require.False(t, record.Quarantined)
		require.Equal(t, 7, record.ServerID)
		require.Equal(t, 1, record.TenantID)
	}
	require.Equal(t, registry.CanonicalToolName("github", "list_issues"), result.Records[0].CallableName)
	require.Equal(t, registry.CanonicalToolName("github", "weird name!"), result.Records[1].CallableName)
	require.Contains(t, result.Records[1].CallableName, "mcp__github__")
}

func TestPlanDiscovery_SchemaChangeQuarantinesPreservingGovernance(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	oldSchema := json.RawMessage(`{"type":"object"}`)
	previous := []ToolRecord{{
		ServerID:     7,
		TenantID:     1,
		RawName:      "list_issues",
		CallableName: registry.CanonicalToolName("github", "list_issues"),
		Description:  "list",
		InputSchema:  oldSchema,
		SchemaHash:   SchemaHash(oldSchema),
		ReadOnly:     true,
		Risk:         "read",
		Category:     "issue",
		Enabled:      true,
		Healthy:      true,
		DiscoveredAt: now.Add(-time.Hour),
		UpdatedAt:    now.Add(-time.Hour),
	}}
	changed := []DiscoveredTool{{
		RawName:     "list_issues",
		Description: "list",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"state":{"type":"string"}}}`),
	}}

	result := PlanDiscovery(7, 1, "github", previous, changed, now)
	require.Equal(t, []string{"list_issues"}, result.Delta.Quarantined)

	record := result.Records[0]
	require.True(t, record.Quarantined, "schema 变化必须隔离待复核")
	require.Equal(t, QuarantineReasonSchemaChanged, record.QuarantineReason)
	require.True(t, record.Enabled, "治理位保留（便于复核后快速恢复）")
	require.True(t, record.ReadOnly)
	require.Equal(t, "read", record.Risk)
	require.Equal(t, "issue", record.Category)
	require.Equal(t, previous[0].DiscoveredAt, record.DiscoveredAt)
	require.NotEqual(t, previous[0].SchemaHash, record.SchemaHash)
}

func TestPlanDiscovery_UnchangedKeepsQuarantineUntilReviewed(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	schema := json.RawMessage(`{"type":"object"}`)
	previous := []ToolRecord{{
		ServerID:         7,
		TenantID:         1,
		RawName:          "list_issues",
		CallableName:     registry.CanonicalToolName("github", "list_issues"),
		Description:      "list",
		InputSchema:      schema,
		SchemaHash:       SchemaHash(schema),
		Quarantined:      true,
		QuarantineReason: QuarantineReasonSchemaChanged,
		DiscoveredAt:     now.Add(-time.Hour),
	}}
	discovered := []DiscoveredTool{{RawName: "list_issues", Description: "list", InputSchema: schema}}

	result := PlanDiscovery(7, 1, "github", previous, discovered, now)
	require.Equal(t, 1, result.Delta.Unchanged)
	require.True(t, result.Records[0].Quarantined, "未复核前隔离必须保持")
	require.Equal(t, QuarantineReasonSchemaChanged, result.Records[0].QuarantineReason)
}

func TestPlanDiscovery_DescriptionUpdateAndRemovedTools(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	schema := json.RawMessage(`{"type":"object"}`)
	previous := []ToolRecord{
		{ServerID: 7, TenantID: 1, RawName: "alpha", CallableName: registry.CanonicalToolName("github", "alpha"), Description: "old", InputSchema: schema, SchemaHash: SchemaHash(schema), Enabled: true},
		{ServerID: 7, TenantID: 1, RawName: "beta", CallableName: registry.CanonicalToolName("github", "beta"), Description: "beta", InputSchema: schema, SchemaHash: SchemaHash(schema)},
	}
	discovered := []DiscoveredTool{{RawName: "alpha", Description: "new", InputSchema: schema}}

	result := PlanDiscovery(7, 1, "github", previous, discovered, now)
	require.Equal(t, []string{"alpha"}, result.Delta.Updated)
	require.Equal(t, []string{"beta"}, result.Delta.Removed)
	require.True(t, result.Records[0].Enabled, "描述更新不得重置治理位")
	require.Equal(t, "new", result.Records[0].Description)
}

func TestMemoryToolCache_CopySemantics(t *testing.T) {
	cache := NewMemoryToolCache()
	cache.Replace(1, []ToolRecord{{RawName: "a"}})

	got := cache.List(1)
	got[0].RawName = "mutated"
	require.Equal(t, "a", cache.List(1)[0].RawName, "必须返回拷贝，外部改动不得污染缓存")
}
