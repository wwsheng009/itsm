package change

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveAffectedCIIDs_AcceptsCiNumber 锁定 D1：
// 变更表单提示用户填写 ciNumber（如 CI-ECS-001），影响分析与冲突检测必须
// 同时接受 CI 数字 ID 和 ciNumber，而不是只对 Atoi 成功的值生效。
func TestResolveAffectedCIIDs_AcceptsCiNumber(t *testing.T) {
	svc, client, _, tenantID := setupChangeChainTest(t)
	ctx := context.Background()

	ciType, err := client.CIType.Create().
		SetName("server").
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	ci, err := client.ConfigurationItem.Create().
		SetName("E2E affected CI").
		SetCiNumber("CI-E2E-IMPACT-001").
		SetCiType("server").
		SetCiTypeID(ciType.ID).
		SetStatus("active").
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	ids, err := svc.resolveAffectedCIIDs(ctx, tenantID, []string{
		"CI-E2E-IMPACT-001",
		strconv.Itoa(ci.ID),
		"CI-E2E-IMPACT-001",
	})
	require.NoError(t, err)
	assert.Equal(t, []int{ci.ID}, ids, "ciNumber 与数字 ID 应规范化为同一 CI，且去重")
}

func TestHasIntIntersection(t *testing.T) {
	assert.True(t, hasIntIntersection([]int{1, 2}, []int{2, 3}))
	assert.True(t, hasIntIntersection([]int{1}, []int{1}))
	assert.False(t, hasIntIntersection([]int{1, 2}, []int{3}))
	assert.False(t, hasIntIntersection(nil, []int{1}))
	assert.False(t, hasIntIntersection([]int{1}, nil))
}