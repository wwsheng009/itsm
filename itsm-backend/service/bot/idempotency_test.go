package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B0-05：幂等键的冻结口径——同输入同键、任一维度变化即变键、键中不含明文参数。
func TestBuildKey_DeterministicAndScoped(t *testing.T) {
	base := KeyInput{
		TenantID: 10,
		UserID:   7,
		Tool:     "create_ticket",
		Args:     map[string]interface{}{"title": "打印机故障", "priority": "high"},
	}

	k1, err := BuildKey(base)
	require.NoError(t, err)
	k2, err := BuildKey(base)
	require.NoError(t, err)
	assert.Equal(t, k1, k2, "同输入必须同键")

	// map 迭代顺序不影响键（canonical JSON 排序键）。
	kOrdered, err := BuildKey(KeyInput{
		TenantID: 10, UserID: 7, Tool: "create_ticket",
		Args: map[string]interface{}{"priority": "high", "title": "打印机故障"},
	})
	require.NoError(t, err)
	assert.Equal(t, k1, kOrdered, "参数键序不影响键")

	// 任一维度变化 → 键变化。
	cases := map[string]KeyInput{
		"租户变化": {TenantID: 11, UserID: 7, Tool: "create_ticket", Args: base.Args},
		"用户变化": {TenantID: 10, UserID: 8, Tool: "create_ticket", Args: base.Args},
		"工具变化": {TenantID: 10, UserID: 7, Tool: "update_ticket", Args: base.Args},
		"参数变化": {TenantID: 10, UserID: 7, Tool: "create_ticket", Args: map[string]interface{}{"title": "打印机故障（改）"}},
		"目标变化": {TenantID: 10, UserID: 7, Tool: "update_ticket", TargetType: "ticket", TargetID: "42", Args: base.Args},
	}
	for name, in := range cases {
		k, err := BuildKey(in)
		require.NoError(t, err, name)
		assert.NotEqual(t, k1, k, "%s 必须改变幂等键", name)
	}
}

// B0-05：键为 sha256 hex（64 位）且不含明文参数（只存 hash 的语义保证）。
func TestBuildKey_IsOpaqueHash(t *testing.T) {
	key, err := BuildKey(KeyInput{
		TenantID: 1, UserID: 2, Tool: "create_ticket",
		Args: map[string]interface{}{"title": "alice-secret", "token": "s3cr3t-value"},
	})
	require.NoError(t, err)
	assert.Len(t, key, 64, "sha256 hex 长度")
	assert.Regexp(t, "^[0-9a-f]{64}$", key)
	assert.NotContains(t, key, "alice")
	assert.NotContains(t, key, "s3cr3t")
	assert.NotContains(t, strings.ToLower(key), "create_ticket")
}

// B0-05：空参数与 nil 参数等价（同一键），且版本常量被冻结。
func TestBuildKey_NilArgsEqualsEmpty(t *testing.T) {
	withNil, err := BuildKey(KeyInput{TenantID: 1, UserID: 1, Tool: "update_ticket"})
	require.NoError(t, err)
	withEmpty, err := BuildKey(KeyInput{TenantID: 1, UserID: 1, Tool: "update_ticket", Args: map[string]interface{}{}})
	require.NoError(t, err)
	assert.Equal(t, withNil, withEmpty)

	assert.Equal(t, "v1", KeyVersion, "拼接版本变更必须显式升版本并更新本断言")
}

// B0-05：目标提取覆盖已知工具参数，且数值统一为整型字符串。
func TestTargetFromArgs(t *testing.T) {
	cases := []struct {
		args  map[string]interface{}
		typ   string
		idStr string
	}{
		{map[string]interface{}{"ticket_id": float64(42)}, "ticket", "42"},
		{map[string]interface{}{"ci_id": float64(7)}, "ci", "7"},
		{map[string]interface{}{"relationship_id": float64(3)}, "ci_relationship", "3"},
		{map[string]interface{}{"ticket_type_id": float64(9)}, "ticket_type", "9"},
		{map[string]interface{}{"title": "无目标"}, "", ""},
	}
	for _, c := range cases {
		typ, id := TargetFromArgs(c.args)
		assert.Equal(t, c.typ, typ, "目标类型：%v", c.args)
		assert.Equal(t, c.idStr, id, "目标 ID：%v", c.args)
	}
}

// B0-05：唯一冲突识别（把并发重复提交映射为幂等命中而非 500）。
func TestIsUniqueViolation(t *testing.T) {
	assert.False(t, IsUniqueViolation(nil))
	assert.True(t, IsUniqueViolation(assertErr("ent: constraint failed: UNIQUE constraint failed: tool_invocations.tenant_id, tool_invocations.idempotency_key_hash")))
	assert.True(t, IsUniqueViolation(assertErr("pq: duplicate key value violates unique constraint \"tool_invocations_tenant_id_idempotency_key_hash\"")))
	assert.False(t, IsUniqueViolation(assertErr("connection refused")))
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
