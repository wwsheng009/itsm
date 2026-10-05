package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	accessTokenRevocationPrefix = "jwt:revoked:"
	// userMinIATPrefix 存储每个用户的 access token 最低可接受签发时间。
	// 早于该时间签发的 token 视为失效（用于角色变更/停用/改密后批量吊销存量 token）。
	userMinIATPrefix = "jwt:user_min_iat:"
	// userMinIATTTL 只需覆盖 access token 最长生命周期（15 分钟）加余量。
	userMinIATTTL = time.Hour
	// userRefreshMinIATPrefix 存储每个用户 refresh token 的最低可接受签发时间（TUM-D6）。
	// 停用/重置密码/强制下线后调用：早于该时间签发的 refresh 一律拒绝续签。
	userRefreshMinIATPrefix = "jwt:user_refresh_min_iat:"
	// userRefreshMinIATTTL 覆盖 refresh token 最长生命周期（7 天）加余量。
	userRefreshMinIATTTL = 8 * 24 * time.Hour
)

type accessTokenRevocationStore interface {
	IsRevoked(ctx context.Context, token string) (bool, error)
	Revoke(ctx context.Context, token string, expiresAt time.Time) error
	// MinIssuedAt 返回该用户 access token 的最低可接受签发时间；零值表示无约束。
	MinIssuedAt(ctx context.Context, userID int) (time.Time, error)
	// SetMinIssuedAt 将该用户 access token 的最低签发时间提升到 t（只增不减）。
	SetMinIssuedAt(ctx context.Context, userID int, t time.Time) error
	// MinRefreshIssuedAt 返回该用户 refresh token 的最低可接受签发时间；零值表示无约束。
	MinRefreshIssuedAt(ctx context.Context, userID int) (time.Time, error)
	// SetMinRefreshIssuedAt 将该用户 refresh token 的最低签发时间提升到 t（只增不减）。
	SetMinRefreshIssuedAt(ctx context.Context, userID int, t time.Time) error
}

var (
	revocationStoreMu sync.RWMutex
	revocationStore   accessTokenRevocationStore = newMemoryAccessTokenRevocationStore()
)

// ConfigureAccessTokenRevocationRedis enables shared revocation across instances.
// The process-local fallback still makes logout safe in single-instance setups.
func ConfigureAccessTokenRevocationRedis(client *redis.Client) {
	if client != nil {
		setAccessTokenRevocationStore(&redisAccessTokenRevocationStore{client: client})
	}
}

func setAccessTokenRevocationStore(store accessTokenRevocationStore) {
	revocationStoreMu.Lock()
	defer revocationStoreMu.Unlock()
	revocationStore = store
}

func currentAccessTokenRevocationStore() accessTokenRevocationStore {
	revocationStoreMu.RLock()
	defer revocationStoreMu.RUnlock()
	return revocationStore
}

// RevokeAccessToken invalidates one access token until its JWT expiry.
func RevokeAccessToken(ctx context.Context, token string, expiresAt time.Time) error {
	return currentAccessTokenRevocationStore().Revoke(ctx, token, expiresAt)
}

func isAccessTokenRevoked(ctx context.Context, token string) (bool, error) {
	return currentAccessTokenRevocationStore().IsRevoked(ctx, token)
}

// InvalidateUserAccessTokens 将 userID 全部存量 access token 置为失效：
// 之后任何签发时间早于 t 的 access token 都会被拒绝。
// 适用于：角色变更、账户停用、密码重置等权限收窄场景（P1-2 修复）。
func InvalidateUserAccessTokens(ctx context.Context, userID int, t time.Time) error {
	return currentAccessTokenRevocationStore().SetMinIssuedAt(ctx, userID, t)
}

// userMinIATKey 返回用户最低签发时间的存储键（分片按租户无关，仅按用户）。
func userMinIATKey(userID int) string {
	return fmt.Sprintf("%s%d", userMinIATPrefix, userID)
}

// InvalidateUserRefreshTokens 将 userID 全部存量 refresh token 置为失效（TUM-D6）：
// 之后任何签发时间早于 t 的 refresh 都无法再续签。
// 与按 token 黑名单互补：黑名单只覆盖单个被轮换/切换的 token，本机制覆盖“该用户全部历史 refresh”。
func InvalidateUserRefreshTokens(ctx context.Context, userID int, t time.Time) error {
	return currentAccessTokenRevocationStore().SetMinRefreshIssuedAt(ctx, userID, t)
}

// IsUserRefreshRevoked 判定 refresh token 是否被用户级吊销（fail-closed）：
//   - 无吊销记录 → 未吊销；
//   - 有记录且签发时间早于记录时间 → 吊销；
//   - 有记录但 token 缺失 iat（零值）→ 吊销（无法证明其晚于吊销时刻）。
func IsUserRefreshRevoked(ctx context.Context, userID int, issuedAt time.Time) (bool, error) {
	minIAT, err := currentAccessTokenRevocationStore().MinRefreshIssuedAt(ctx, userID)
	if err != nil {
		return false, err
	}
	if minIAT.IsZero() {
		return false, nil
	}
	if issuedAt.IsZero() {
		return true, nil
	}
	return issuedAt.Before(minIAT), nil
}

// userRefreshMinIATKey 返回用户 refresh 最低签发时间的存储键。
func userRefreshMinIATKey(userID int) string {
	return fmt.Sprintf("%s%d", userRefreshMinIATPrefix, userID)
}

func accessTokenRevocationKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return accessTokenRevocationPrefix + hex.EncodeToString(sum[:])
}

type redisAccessTokenRevocationStore struct {
	client *redis.Client
}

func (s *redisAccessTokenRevocationStore) IsRevoked(ctx context.Context, token string) (bool, error) {
	count, err := s.client.Exists(ctx, accessTokenRevocationKey(token)).Result()
	return count > 0, err
}

func (s *redisAccessTokenRevocationStore) Revoke(ctx context.Context, token string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	return s.client.Set(ctx, accessTokenRevocationKey(token), "1", ttl).Err()
}

func (s *redisAccessTokenRevocationStore) MinIssuedAt(ctx context.Context, userID int) (time.Time, error) {
	val, err := s.client.Get(ctx, userMinIATKey(userID)).Result()
	if err == redis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	ts, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return time.Time{}, nil // 脏数据按无约束处理
	}
	return time.Unix(ts, 0), nil
}

func (s *redisAccessTokenRevocationStore) SetMinIssuedAt(ctx context.Context, userID int, t time.Time) error {
	// 只增不减：避免并发写回退约束。
	const luaScript = `local cur = redis.call('GET', KEYS[1])
local curTs = tonumber(cur) or 0
local newTs = tonumber(ARGV[1])
if newTs > curTs then
  redis.call('SET', KEYS[1], tostring(newTs), 'EX', tonumber(ARGV[2]))
end
return 1`
	return s.client.Eval(ctx, luaScript, []string{userMinIATKey(userID)},
		t.Unix(), int(userMinIATTTL.Seconds())).Err()
}

func (s *redisAccessTokenRevocationStore) MinRefreshIssuedAt(ctx context.Context, userID int) (time.Time, error) {
	val, err := s.client.Get(ctx, userRefreshMinIATKey(userID)).Result()
	if err == redis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	ts, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return time.Time{}, nil // 脏数据按无约束处理
	}
	return time.Unix(ts, 0), nil
}

func (s *redisAccessTokenRevocationStore) SetMinRefreshIssuedAt(ctx context.Context, userID int, t time.Time) error {
	// 只增不减：避免并发写回退约束（与 access 同款 Lua）。
	const luaScript = `local cur = redis.call('GET', KEYS[1])
local curTs = tonumber(cur) or 0
local newTs = tonumber(ARGV[1])
if newTs > curTs then
  redis.call('SET', KEYS[1], tostring(newTs), 'EX', tonumber(ARGV[2]))
end
return 1`
	return s.client.Eval(ctx, luaScript, []string{userRefreshMinIATKey(userID)},
		t.Unix(), int(userRefreshMinIATTTL.Seconds())).Err()
}

type memoryAccessTokenRevocationStore struct {
	mu      sync.Mutex
	expires map[string]time.Time
	// userMinIAT 记录每用户 access token 最低可接受签发时间（内存实现）。
	userMinIAT map[int]time.Time
	// userRefreshMinIAT 记录每用户 refresh token 最低可接受签发时间（内存实现）。
	userRefreshMinIAT map[int]time.Time
}

func newMemoryAccessTokenRevocationStore() *memoryAccessTokenRevocationStore {
	return &memoryAccessTokenRevocationStore{
		expires:           make(map[string]time.Time),
		userMinIAT:        make(map[int]time.Time),
		userRefreshMinIAT: make(map[int]time.Time),
	}
}

func (s *memoryAccessTokenRevocationStore) IsRevoked(_ context.Context, token string) (bool, error) {
	key := accessTokenRevocationKey(token)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt, ok := s.expires[key]
	if ok && !now.Before(expiresAt) {
		delete(s.expires, key)
		return false, nil
	}
	return ok, nil
}

func (s *memoryAccessTokenRevocationStore) Revoke(_ context.Context, token string, expiresAt time.Time) error {
	if token == "" || !time.Now().Before(expiresAt) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expires[accessTokenRevocationKey(token)] = expiresAt
	return nil
}

func (s *memoryAccessTokenRevocationStore) MinIssuedAt(_ context.Context, userID int) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userMinIAT[userID], nil
}

func (s *memoryAccessTokenRevocationStore) SetMinIssuedAt(_ context.Context, userID int, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.userMinIAT[userID]; !ok || t.After(cur) {
		s.userMinIAT[userID] = t
	}
	return nil
}

func (s *memoryAccessTokenRevocationStore) MinRefreshIssuedAt(_ context.Context, userID int) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userRefreshMinIAT[userID], nil
}

func (s *memoryAccessTokenRevocationStore) SetMinRefreshIssuedAt(_ context.Context, userID int, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.userRefreshMinIAT[userID]; !ok || t.After(cur) {
		s.userRefreshMinIAT[userID] = t
	}
	return nil
}
