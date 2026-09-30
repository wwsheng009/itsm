// Package capability 提供 AI 能力开关（MCP / Bot）的运行时求值、覆盖与缓存。
//
// 背景（2026-09-30 方案 docs/plan/mcp-ai-capability-switches-plan-2026-09-30.md）：
//   - 此前 mcp.enabled / mcp.write_enabled / bot.enabled 只有环境变量一条路径，
//     改动必须重启后端；写工具面关闭时前端也没有任何提示。
//   - 本包把三者提升为「租户级运行时能力开关」：默认不写库（缺行 = 跟随静态默认，
//     升级后行为零变化），管理员在管理后台显式保存才产生覆盖行。
//
// 求值口径（fail-safe）：
//  1. system_configs 行（本租户、未软删、值可解析）→ 覆盖值；
//  2. 静态配置（env / config.yaml，由 bootstrap 注入 Defaults）；
//  3. 内置默认 false。
//
// 任一步读取失败都回退到静态默认并记警告：能力开关的读取**绝不允许**阻断主链路。
package capability

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/systemconfig"

	"go.uber.org/zap"
)

// 开关键名（与静态配置同名，category 固定 ai）。
const (
	KeyMCPEnabled      = "mcp.enabled"
	KeyMCPWriteEnabled = "mcp.write_enabled"
	KeyBotEnabled      = "bot.enabled"

	// Category 是能力开关在 system_configs 中的固定分类（管理后台按此分组展示）。
	Category = "ai"
)

// DefaultTTL 是缓存兜底 TTL：写路径会主动失效，TTL 只用于多实例收敛
// （写入方所在实例立即生效，其余实例 ≤ TTL 跟随）。
const DefaultTTL = 30 * time.Second

// Defaults 是静态配置提供的默认值（来自 cfg.MCP.Enabled / cfg.MCP.WriteEnabled / cfg.Bot.Enabled）。
type Defaults struct {
	MCPEnabled      bool
	MCPWriteEnabled bool
	BotEnabled      bool
}

// Snapshot 是某租户当前生效的能力开关快照。
type Snapshot struct {
	MCPEnabled      bool
	MCPWriteEnabled bool
	BotEnabled      bool
	// Overridden[key] = true 表示该键由 system_configs 覆盖（管理台设置）；
	// false = 跟随静态默认（UI 显示「跟随环境默认」）。
	Overridden map[string]bool
	// UpdatedAt / UpdatedBy 为覆盖行的最近写入信息（无覆盖时为零值）。
	UpdatedAt time.Time
	UpdatedBy string
}

// IsOverridden 判断某键是否被管理台覆盖。
func (s Snapshot) IsOverridden(key string) bool { return s.Overridden[key] }

// Source 是能力开关的运行时读取接口（消费方：provider / ai.Service / 路由门禁 / 管理 API）。
type Source interface {
	// For 返回租户当前生效值；实现必须 fail-safe（任何异常回退静态默认）。
	For(ctx context.Context, tenantID int) Snapshot
	// Invalidate 立即失效某租户缓存（写路径调用）。
	Invalidate(tenantID int)
}

// Patch 是能力开关的更新入参（三态）：
//   - 字段为 nil        → 该键不变（保持现状）；
//   - 字段指向 true/false → 写入/更新覆盖行；
//   - 需要「删除覆盖行、恢复跟随静态默认」时调用 Clear。
type Patch struct {
	MCPEnabled      *bool
	MCPWriteEnabled *bool
	BotEnabled      *bool
}

// errUnavailable 等稳定错误（handler 据此映射 HTTP 状态码）。
var (
	// ErrUnavailable 能力开关源未装配（ent 客户端缺失）。
	ErrUnavailable = errors.New("capability: source unavailable")
	// ErrInvalidTenant 租户非法。
	ErrInvalidTenant = errors.New("capability: invalid tenant")
	// ErrUnknownKey 键不属于能力开关集合。
	ErrUnknownKey = errors.New("capability: unknown key")
)

// ConfigSource 是基于 ent 的 Source 实现（生产实现）。
type ConfigSource struct {
	client   *ent.Client
	defaults Defaults
	ttl      time.Duration
	logger   *zap.SugaredLogger
	now      func() time.Time

	mu    sync.RWMutex
	cache map[int]cacheEntry
}

type cacheEntry struct {
	snap      Snapshot
	expiresAt time.Time
}

// NewConfigSource 构造运行时能力开关源。
//
// client 为 nil 时退化为「只读静态默认」的降级实现（测试/未装配场景，fail-safe）。
func NewConfigSource(client *ent.Client, defaults Defaults, logger *zap.SugaredLogger) *ConfigSource {
	return newConfigSource(client, defaults, DefaultTTL, logger)
}

func newConfigSource(client *ent.Client, defaults Defaults, ttl time.Duration, logger *zap.SugaredLogger) *ConfigSource {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &ConfigSource{
		client:   client,
		defaults: defaults,
		ttl:      ttl,
		logger:   logger,
		now:      time.Now,
		cache:    make(map[int]cacheEntry),
	}
}

// defaultsSnapshot 返回仅含静态默认的快照。
func (s *ConfigSource) defaultsSnapshot() Snapshot {
	return Snapshot{
		MCPEnabled:      s.defaults.MCPEnabled,
		MCPWriteEnabled: s.defaults.MCPWriteEnabled,
		BotEnabled:      s.defaults.BotEnabled,
		Overridden:      map[string]bool{},
	}
}

// For 实现 Source。
func (s *ConfigSource) For(ctx context.Context, tenantID int) Snapshot {
	if s == nil {
		return Snapshot{}
	}
	if s.client == nil || tenantID <= 0 {
		return s.defaultsSnapshot()
	}

	now := s.now()
	s.mu.RLock()
	entry, ok := s.cache[tenantID]
	s.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.snap
	}

	snap, err := s.load(ctx, tenantID)
	if err != nil {
		// fail-safe：读取失败回退静态默认，并短暂缓存以避免故障期反复打库。
		s.logger.Warnw("读取能力开关失败，回退静态默认", "tenant_id", tenantID, "error", err)
		snap = s.defaultsSnapshot()
	}
	s.mu.Lock()
	s.cache[tenantID] = cacheEntry{snap: snap, expiresAt: now.Add(s.ttl)}
	s.mu.Unlock()
	return snap
}

// Invalidate 实现 Source：删除某租户缓存。
func (s *ConfigSource) Invalidate(tenantID int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.cache, tenantID)
	s.mu.Unlock()
}

// Defaults 返回静态默认值（管理台展示「跟随环境默认」时使用）。
func (s *ConfigSource) Defaults() Defaults {
	if s == nil {
		return Defaults{}
	}
	return s.defaults
}

// InvalidateAll 清空全部缓存（测试与全局配置变更使用）。
func (s *ConfigSource) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cache = make(map[int]cacheEntry)
	s.mu.Unlock()
}

// load 从 system_configs 读取覆盖行并合成快照（缺失/非法值 = 跟随默认）。
func (s *ConfigSource) load(ctx context.Context, tenantID int) (Snapshot, error) {
	rows, err := s.client.SystemConfig.Query().
		Where(
			systemconfig.TenantIDEQ(tenantID),
			systemconfig.DeletedAtIsNil(),
			systemconfig.KeyIn(KeyMCPEnabled, KeyMCPWriteEnabled, KeyBotEnabled),
		).
		All(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	snap := s.defaultsSnapshot()
	for _, row := range rows {
		parsed, err := strconv.ParseBool(strings.TrimSpace(row.Value))
		if err != nil {
			// 脏值不得静默放宽：视为未配置并留痕，由管理台修正。
			s.logger.Warnw("能力开关值非法，按未配置处理",
				"tenant_id", tenantID, "key", row.Key, "value", row.Value)
			continue
		}
		switch row.Key {
		case KeyMCPEnabled:
			snap.MCPEnabled = parsed
		case KeyMCPWriteEnabled:
			snap.MCPWriteEnabled = parsed
		case KeyBotEnabled:
			snap.BotEnabled = parsed
		default:
			continue
		}
		snap.Overridden[row.Key] = true
		if row.UpdatedAt.After(snap.UpdatedAt) {
			snap.UpdatedAt = row.UpdatedAt
		}
		if row.CreatedBy != "" {
			snap.UpdatedBy = row.CreatedBy
		}
	}
	return snap, nil
}

// Update 写入覆盖行并返回最新快照；未在 patch 中出现的键保持不变。
//
// operator 记入 created_by（「最近操作人」；变更审计另由 AuditMiddleware 全量落库）。
func (c *ConfigSource) Update(ctx context.Context, tenantID int, patch Patch, operator string) (Snapshot, error) {
	if c == nil || c.client == nil {
		return Snapshot{}, ErrUnavailable
	}
	if tenantID <= 0 {
		return Snapshot{}, ErrInvalidTenant
	}

	upserts := make(map[string]bool, 3)
	if patch.MCPEnabled != nil {
		upserts[KeyMCPEnabled] = *patch.MCPEnabled
	}
	if patch.MCPWriteEnabled != nil {
		upserts[KeyMCPWriteEnabled] = *patch.MCPWriteEnabled
	}
	if patch.BotEnabled != nil {
		upserts[KeyBotEnabled] = *patch.BotEnabled
	}
	for key, value := range upserts {
		if err := c.upsert(ctx, tenantID, key, value, operator); err != nil {
			return Snapshot{}, err
		}
	}
	c.Invalidate(tenantID)
	return c.For(ctx, tenantID), nil
}

// Clear 删除若干键的覆盖行（恢复跟随静态默认）。
func (c *ConfigSource) Clear(ctx context.Context, tenantID int, keys ...string) (Snapshot, error) {
	if c == nil || c.client == nil {
		return Snapshot{}, ErrUnavailable
	}
	if tenantID <= 0 {
		return Snapshot{}, ErrInvalidTenant
	}
	for _, key := range keys {
		if !IsKnownKey(key) {
			return Snapshot{}, ErrUnknownKey
		}
		rows, err := c.client.SystemConfig.Query().
			Where(
				systemconfig.TenantIDEQ(tenantID),
				systemconfig.DeletedAtIsNil(),
				systemconfig.KeyEQ(key),
			).
			All(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		for _, row := range rows {
			if err := c.client.SystemConfig.UpdateOneID(row.ID).
				SetDeletedAt(time.Now()).
				Exec(ctx); err != nil {
				return Snapshot{}, err
			}
		}
	}
	c.Invalidate(tenantID)
	return c.For(ctx, tenantID), nil
}

func (c *ConfigSource) upsert(ctx context.Context, tenantID int, key string, value bool, operator string) error {
	valueStr := strconv.FormatBool(value)
	existing, err := c.client.SystemConfig.Query().
		Where(
			systemconfig.TenantIDEQ(tenantID),
			systemconfig.DeletedAtIsNil(),
			systemconfig.KeyEQ(key),
		).
		First(ctx)
	switch {
	case err == nil:
		return c.client.SystemConfig.UpdateOneID(existing.ID).
			SetValue(valueStr).
			SetValueType("boolean").
			SetCategory(Category).
			SetDescription(descriptionOf(key)).
			SetCreatedBy(operator).
			Exec(ctx)
	case ent.IsNotFound(err):
		_, createErr := c.client.SystemConfig.Create().
			SetKey(key).
			SetValue(valueStr).
			SetValueType("boolean").
			SetCategory(Category).
			SetDescription(descriptionOf(key)).
			SetCreatedBy(operator).
			SetTenantID(tenantID).
			Save(ctx)
		return createErr
	default:
		return err
	}
}

// IsKnownKey 判断键是否属于能力开关集合。
func IsKnownKey(key string) bool {
	switch key {
	case KeyMCPEnabled, KeyMCPWriteEnabled, KeyBotEnabled:
		return true
	default:
		return false
	}
}

// KnownKeys 返回全部能力开关键（顺序固定，管理 API 与 UI 同源）。
func KnownKeys() []string {
	return []string{KeyMCPEnabled, KeyMCPWriteEnabled, KeyBotEnabled}
}

func descriptionOf(key string) string {
	switch key {
	case KeyMCPEnabled:
		return "MCP 外部工具能力总开关（运行时；缺行=跟随环境默认）"
	case KeyMCPWriteEnabled:
		return "MCP 写工具面开关（运行时；关闭时写工具不可见、不可执行）"
	case KeyBotEnabled:
		return "Bot 能力开关（运行时；关闭时 Bot 管理写端与选择器停用）"
	default:
		return ""
	}
}
