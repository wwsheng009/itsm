package capability

import (
	"context"
	"testing"
	"time"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
)

// 能力开关运行时源的契约：
//  1. 缺行 = 跟随静态默认（升级零行为变化）；
//  2. 覆盖行优先，写路径 Invalidate 后立即生效（免重启）；
//  3. 脏值/读取故障 fail-safe 回退默认，绝不放宽、绝不阻断主链路；
//  4. TTL 兜底（多实例收敛口径）。

func newCapTestSource(t *testing.T, defaults Defaults) (*ConfigSource, *ent.Client) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:capability?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	src := newConfigSource(client, defaults, time.Minute, zap.NewNop().Sugar())
	return src, client
}

func staticDefaults() Defaults {
	return Defaults{MCPEnabled: true, MCPWriteEnabled: false, BotEnabled: true}
}

func TestConfigSource_FollowsStaticDefaultsWhenNoOverride(t *testing.T) {
	src, _ := newCapTestSource(t, staticDefaults())

	snap := src.For(context.Background(), 7)
	assert.True(t, snap.MCPEnabled)
	assert.False(t, snap.MCPWriteEnabled)
	assert.True(t, snap.BotEnabled)
	assert.Empty(t, snap.Overridden, "无覆盖行时 Overridden 必须为空")
	assert.False(t, snap.IsOverridden(KeyMCPWriteEnabled))
}

func TestConfigSource_OverrideWinsAndInvalidateAppliesImmediately(t *testing.T) {
	src, _ := newCapTestSource(t, staticDefaults())
	ctx := context.Background()

	// 先读一次触发缓存（默认值）
	require.False(t, src.For(ctx, 7).MCPWriteEnabled)

	on := true
	snap, err := src.Update(ctx, 7, Patch{MCPWriteEnabled: &on}, "admin")
	require.NoError(t, err)
	assert.True(t, snap.MCPWriteEnabled, "写路径返回的快照必须是新值")
	assert.True(t, snap.IsOverridden(KeyMCPWriteEnabled))
	assert.Equal(t, "admin", snap.UpdatedBy)
	assert.False(t, snap.UpdatedAt.IsZero())

	// 免重启：同一进程下一次读取立即生效
	assert.True(t, src.For(ctx, 7).MCPWriteEnabled)

	// 关回去
	off := false
	snap, err = src.Update(ctx, 7, Patch{MCPWriteEnabled: &off}, "admin")
	require.NoError(t, err)
	assert.False(t, snap.MCPWriteEnabled)

	// 未在 patch 中出现的键保持不变（MCPEnabled 仍跟随默认 true）
	assert.True(t, src.For(ctx, 7).MCPEnabled)
}

func TestConfigSource_ClearRestoresDefault(t *testing.T) {
	src, _ := newCapTestSource(t, staticDefaults())
	ctx := context.Background()

	off := false
	_, err := src.Update(ctx, 7, Patch{MCPEnabled: &off}, "admin")
	require.NoError(t, err)
	require.False(t, src.For(ctx, 7).MCPEnabled)

	snap, err := src.Clear(ctx, 7, KeyMCPEnabled)
	require.NoError(t, err)
	assert.True(t, snap.MCPEnabled, "删除覆盖行后必须回到静态默认")
	assert.False(t, snap.IsOverridden(KeyMCPEnabled))

	// 软删后再次查询不复活
	assert.True(t, src.For(ctx, 7).MCPEnabled)
}

func TestConfigSource_DirtyValueFallsBackToDefault(t *testing.T) {
	src, client := newCapTestSource(t, staticDefaults())
	ctx := context.Background()

	// 脏值（非布尔字面量）：按未配置处理，不得放宽/收窄口径
	_, err := client.SystemConfig.Create().
		SetKey(KeyMCPWriteEnabled).SetValue("yes").SetValueType("boolean").
		SetCategory(Category).SetTenantID(7).Save(ctx)
	require.NoError(t, err)
	src.Invalidate(7)

	snap := src.For(ctx, 7)
	assert.False(t, snap.MCPWriteEnabled, "脏值必须回退静态默认 false")
	assert.False(t, snap.IsOverridden(KeyMCPWriteEnabled), "脏值不得被标记为已覆盖")
}

func TestConfigSource_TTLExpiryReReadsWithoutInvalidate(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:capability_ttl?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	src := newConfigSource(client, staticDefaults(), 20*time.Millisecond, zap.NewNop().Sugar())
	ctx := context.Background()

	require.False(t, src.For(ctx, 7).MCPWriteEnabled)

	// 直接用 DB 写入（模拟另一实例的写入路径），不调用 Invalidate
	_, err := client.SystemConfig.Create().
		SetKey(KeyMCPWriteEnabled).SetValue("true").SetValueType("boolean").
		SetCategory(Category).SetTenantID(7).Save(ctx)
	require.NoError(t, err)

	assert.False(t, src.For(ctx, 7).MCPWriteEnabled, "TTL 内仍读缓存（多实例收敛窗口）")
	time.Sleep(30 * time.Millisecond)
	assert.True(t, src.For(ctx, 7).MCPWriteEnabled, "TTL 到期后必须重新读库")
}

func TestConfigSource_FailSafeOnUnavailableClientAndBadTenant(t *testing.T) {
	// nil client：退化为只读静态默认
	fallback := newConfigSource(nil, staticDefaults(), time.Minute, nil)
	assert.True(t, fallback.For(context.Background(), 7).MCPEnabled)

	// 非法租户：默认值，不落缓存/不报错
	src, _ := newCapTestSource(t, staticDefaults())
	assert.True(t, src.For(context.Background(), 0).MCPEnabled)

	// Update/Clear 的稳定错误
	_, err := src.Update(context.Background(), 0, Patch{}, "admin")
	assert.ErrorIs(t, err, ErrInvalidTenant)
	_, err = src.Clear(context.Background(), 7, "mcp.unknown")
	assert.ErrorIs(t, err, ErrUnknownKey)
}

func TestKnownKeys_StableOrder(t *testing.T) {
	assert.Equal(t, []string{KeyMCPEnabled, KeyMCPWriteEnabled, KeyBotEnabled}, KnownKeys())
	for _, key := range KnownKeys() {
		assert.True(t, IsKnownKey(key))
	}
	assert.False(t, IsKnownKey("mcp.other"))
}
