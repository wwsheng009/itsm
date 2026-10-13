package bootstrap

import (
	"context"
	"strconv"
	"strings"

	"itsm-backend/service"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// bootstrapRedisInvalidator 基于 Redis Pub/Sub 的租户配置失效发布器（方案 D6）。
type bootstrapRedisInvalidator struct {
	rdb *redis.Client
}

// PublishTenantConfigInvalidation 发布租户配置失效事件（best-effort；失败由调用方告警）。
func (b bootstrapRedisInvalidator) PublishTenantConfigInvalidation(ctx context.Context, tenantID int) error {
	if b.rdb == nil {
		return nil
	}
	return b.rdb.Publish(ctx, service.TenantConfigInvalidationChannel, strconv.Itoa(tenantID)).Err()
}

// startTenantConfigInvalidationSubscriber 订阅租户配置失效频道，清本实例缓存（多副本一致性）。
//
// 生命周期：随进程常驻（Redis 断线由 go-redis 自动重连；进程退出即终止）。
// 订阅失败不影响主流程：本地保存路径已直接失效，TTL 兜底 5 分钟。
func startTenantConfigInvalidationSubscriber(
	rdb *redis.Client,
	resolver *service.TenantConfigResolver,
	configs *service.SystemConfigService,
	mailers *service.MailerProvider,
	logger *zap.SugaredLogger,
) {
	if rdb == nil || resolver == nil {
		return
	}
	go func() {
		sub := rdb.Subscribe(context.Background(), service.TenantConfigInvalidationChannel)
		defer func() { _ = sub.Close() }()
		logger.Infow("tenant config invalidation subscriber started", "channel", service.TenantConfigInvalidationChannel)

		for msg := range sub.Channel() {
			tenantID, err := strconv.Atoi(strings.TrimSpace(msg.Payload))
			if err != nil || tenantID <= 0 {
				logger.Debugw("ignore malformed tenant config invalidation message", "payload", msg.Payload)
				continue
			}
			resolver.InvalidateLocal(tenantID)
			if configs != nil {
				configs.InvalidatePasswordPolicy(tenantID)
			}
			if mailers != nil {
				mailers.InvalidateTenant(tenantID)
			}
			logger.Debugw("tenant config cache invalidated from redis", "tenant_id", tenantID)
		}
	}()
}
