package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/systemconfig"

	"go.uber.org/zap"
)

// TenantConfigInvalidationChannel 租户配置失效广播频道（Redis Pub/Sub，多实例一致性，方案 D6）。
const TenantConfigInvalidationChannel = "itsm:tenant-config:invalidate"

// tenantConfigCacheTTL 进程内缓存 TTL（兜底；正常路径由保存时主动失效 + Redis 广播驱动）。
const tenantConfigCacheTTL = 5 * time.Minute

// TenantConfigInvalidator 跨实例失效发布器（Redis 实现由 bootstrap 注入；nil = 仅本实例失效）。
type TenantConfigInvalidator interface {
	PublishTenantConfigInvalidation(ctx context.Context, tenantID int) error
}

type tenantConfigCacheEntry struct {
	values    map[string]string
	expiresAt time.Time
}

// TenantConfigResolver 租户配置运行时解析器（方案 P0-1 / SCD1）。
//
// 职责：把 system_configs 的租户行解析为业务可读的生效值，缓存租户全量键值，
// 保存后主动失效；多实例经 Redis 广播失效（best-effort，失败仅告警不阻断保存）。
//
// 生效优先级（方案 D5）由调用方组合：租户值 > 部署 env/配置 > 内置默认。
// 解析器只回答"租户是否显式配置了该键"；调用方（如 MailerProvider）负责回退链。
type TenantConfigResolver struct {
	client      *ent.Client
	logger      *zap.SugaredLogger
	ttl         time.Duration
	mu          sync.RWMutex
	cache       map[int]tenantConfigCacheEntry
	invalidator TenantConfigInvalidator
}

// NewTenantConfigResolver 创建解析器（TTL 默认 5 分钟）。
func NewTenantConfigResolver(client *ent.Client, logger *zap.SugaredLogger) *TenantConfigResolver {
	return &TenantConfigResolver{
		client: client,
		logger: logger,
		ttl:    tenantConfigCacheTTL,
		cache:  make(map[int]tenantConfigCacheEntry),
	}
}

// SetInvalidator 注入跨实例失效发布器（bootstrap 装配 Redis 实现）。
func (r *TenantConfigResolver) SetInvalidator(inv TenantConfigInvalidator) {
	if r == nil {
		return
	}
	r.invalidator = inv
}

// Get 读取租户配置值；第二个返回值为 false 表示该租户未配置该键（或读取失败，按未配置处理并告警）。
func (r *TenantConfigResolver) Get(ctx context.Context, tenantID int, key string) (string, bool) {
	if r == nil || tenantID <= 0 || strings.TrimSpace(key) == "" {
		return "", false
	}
	values := r.load(ctx, tenantID)
	value, ok := values[key]
	if !ok {
		return "", false
	}
	return value, true
}

// GetStringOr 读取租户配置字符串，未配置/为空时回退 fallback。
func (r *TenantConfigResolver) GetStringOr(ctx context.Context, tenantID int, key, fallback string) string {
	if value, ok := r.Get(ctx, tenantID, key); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

// GetBoolOr 读取租户布尔配置，未配置/非法时回退 fallback。
func (r *TenantConfigResolver) GetBoolOr(ctx context.Context, tenantID int, key string, fallback bool) bool {
	if value, ok := r.Get(ctx, tenantID, key); ok {
		if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}

// GetIntOr 读取租户数值配置，未配置/非法/非正数时回退 fallback。
func (r *TenantConfigResolver) GetIntOr(ctx context.Context, tenantID int, key string, fallback int) int {
	if value, ok := r.Get(ctx, tenantID, key); ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

// InvalidateLocal 清除本实例的租户配置缓存（Redis 订阅回调与本地保存路径共用）。
func (r *TenantConfigResolver) InvalidateLocal(tenantID int) {
	if r == nil || tenantID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.cache, tenantID)
	r.mu.Unlock()
}

// Invalidate 本地失效 + 广播跨实例失效（best-effort：广播失败仅告警，不影响保存结果）。
func (r *TenantConfigResolver) Invalidate(ctx context.Context, tenantID int) {
	if r == nil || tenantID <= 0 {
		return
	}
	r.InvalidateLocal(tenantID)
	if r.invalidator == nil {
		return
	}
	if err := r.invalidator.PublishTenantConfigInvalidation(ctx, tenantID); err != nil {
		r.logger.Warnw("tenant config invalidation broadcast failed", "error", err, "tenant_id", tenantID)
	}
}

// load 返回租户全量配置（缓存优先；未命中时单次查询回填）。
// 读取失败时返回空集合（调用方按未配置处理回退默认值），并输出告警，不阻塞业务。
func (r *TenantConfigResolver) load(ctx context.Context, tenantID int) map[string]string {
	r.mu.RLock()
	if entry, ok := r.cache[tenantID]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.RUnlock()
		return entry.values
	}
	r.mu.RUnlock()

	values := make(map[string]string)
	configs, err := r.client.SystemConfig.Query().
		Where(systemconfig.TenantIDEQ(tenantID), systemconfig.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		r.logger.Warnw("load tenant config failed; fallback to deployment defaults", "error", err, "tenant_id", tenantID)
		return values
	}
	for _, cfg := range configs {
		values[cfg.Key] = cfg.Value
	}

	r.mu.Lock()
	if r.cache == nil {
		r.cache = make(map[int]tenantConfigCacheEntry)
	}
	r.cache[tenantID] = tenantConfigCacheEntry{values: values, expiresAt: time.Now().Add(r.ttl)}
	r.mu.Unlock()
	return values
}
