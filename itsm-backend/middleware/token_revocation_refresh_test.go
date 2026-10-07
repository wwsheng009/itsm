package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TUM-D6：refresh 按用户吊销——内存 store 仅增不减语义 + IsUserRefreshRevoked fail-closed 判定。
func TestUserRefreshRevocation_MemoryStore(t *testing.T) {
	store := newMemoryAccessTokenRevocationStore()
	setAccessTokenRevocationStore(store)
	defer setAccessTokenRevocationStore(newMemoryAccessTokenRevocationStore())
	ctx := context.Background()

	// 无吊销记录 → 任何签发时间都不算被吊销。
	revoked, err := IsUserRefreshRevoked(ctx, 7001, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.False(t, revoked)

	cutoff := time.Now()
	require.NoError(t, InvalidateUserRefreshTokens(ctx, 7001, cutoff))

	// 早于 cutoff → 吊销；晚于 cutoff → 放行；零值 iat → fail-closed。
	revoked, err = IsUserRefreshRevoked(ctx, 7001, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.True(t, revoked)
	revoked, err = IsUserRefreshRevoked(ctx, 7001, cutoff.Add(time.Second))
	require.NoError(t, err)
	assert.False(t, revoked)
	revoked, err = IsUserRefreshRevoked(ctx, 7001, time.Time{})
	require.NoError(t, err)
	assert.True(t, revoked)

	// 仅增不减：回退写入被忽略，提升生效。
	require.NoError(t, InvalidateUserRefreshTokens(ctx, 7001, cutoff.Add(-time.Hour)))
	minIAT, err := store.MinRefreshIssuedAt(ctx, 7001)
	require.NoError(t, err)
	assert.WithinDuration(t, cutoff, minIAT, time.Second)

	later := cutoff.Add(time.Minute)
	require.NoError(t, InvalidateUserRefreshTokens(ctx, 7001, later))
	minIAT, err = store.MinRefreshIssuedAt(ctx, 7001)
	require.NoError(t, err)
	assert.WithinDuration(t, later, minIAT, time.Second)

	// 其他用户不受影响。
	revoked, err = IsUserRefreshRevoked(ctx, 7002, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.False(t, revoked)
}

// MSW-3 / Q3=A：(user×tenant) 作用域 refresh 吊销——作用域隔离；与用户级并联 fail-closed。
func TestUserRefreshRevocationForTenant_MemoryStore(t *testing.T) {
	store := newMemoryAccessTokenRevocationStore()
	setAccessTokenRevocationStore(store)
	defer setAccessTokenRevocationStore(newMemoryAccessTokenRevocationStore())
	ctx := context.Background()

	// 无记录 → 放行。
	revoked, err := IsUserRefreshRevokedForTenant(ctx, 8001, 11, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.False(t, revoked)

	cutoff := time.Now()
	require.NoError(t, InvalidateUserRefreshTokensForTenant(ctx, 8001, 11, cutoff))

	// 目标作用域：早于 cutoff 吊销、晚于放行、零值 iat fail-closed。
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8001, 11, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.True(t, revoked)
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8001, 11, cutoff.Add(time.Second))
	require.NoError(t, err)
	assert.False(t, revoked)
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8001, 11, time.Time{})
	require.NoError(t, err)
	assert.True(t, revoked)

	// 作用域隔离：其它租户 / 其它用户不受影响。
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8001, 12, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.False(t, revoked)
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8002, 11, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.False(t, revoked)

	// 用户级吊销对 tenant 判定并联生效（fail-closed）。
	require.NoError(t, InvalidateUserRefreshTokens(ctx, 8001, cutoff))
	revoked, err = IsUserRefreshRevokedForTenant(ctx, 8001, 12, cutoff.Add(-time.Second))
	require.NoError(t, err)
	assert.True(t, revoked)

	// 仅增不减：回退写入被忽略。
	require.NoError(t, InvalidateUserRefreshTokensForTenant(ctx, 8001, 11, cutoff.Add(-time.Hour)))
	minIAT, err := store.MinRefreshIssuedAtForTenant(ctx, 8001, 11)
	require.NoError(t, err)
	assert.WithinDuration(t, cutoff, minIAT, time.Second)
}
