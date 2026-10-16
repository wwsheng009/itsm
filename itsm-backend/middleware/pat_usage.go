package middleware

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PAT 使用审计与限流闸门（PAT 方案 D-PAT-9/R-3c，实施文档 T6）。
//
// 设计：
//   - 使用行去重：同一 (tokenID, method, path) 在窗口期内只写一条 audit 行
//     （Redis SETNX + TTL；无 Redis 退化为进程内 map，per-instance 去重可接受）。
//   - last_used_at/use_count 落库节流：每令牌窗口期最多一次（避免每请求写放大）。
//   - 单令牌限流：每令牌每分钟请求数上限（全局限流按 IP，无法约束轮换 IP 的泄漏令牌）。

var (
	patUsageRedis      *redis.Client
	patUsageWindowSecs = 60
	patRatePerMinute   = 0 // 0 = 不启用单令牌限流
	patUsageMu         sync.Mutex
	patUsageMem        = map[string]time.Time{} // key -> 过期时间
	patLastMu          sync.Mutex
	patLastMem         = map[string]time.Time{}
)

// ConfigurePATUsageGate 装配使用审计闸门（bootstrap 调用）。
func ConfigurePATUsageGate(rdb *redis.Client, windowSeconds, rateLimitPerMinute int) {
	patUsageMu.Lock()
	patUsageRedis = rdb
	patUsageMu.Unlock()
	if windowSeconds > 0 {
		patUsageWindowSecs = windowSeconds
	}
	patRatePerMinute = rateLimitPerMinute
}

func patUsageWindow() time.Duration {
	return time.Duration(patUsageWindowSecs) * time.Second
}

// allowByGate 通用 SETNX 闸门：窗口内首次返回 true。
func allowByGate(ctx context.Context, key string) bool {
	if patUsageRedis == nil {
		// 内存退化（per-instance）
		patUsageMu.Lock()
		defer patUsageMu.Unlock()
		now := time.Now()
		if exp, ok := patUsageMem[key]; ok && exp.After(now) {
			return false
		}
		patUsageMem[key] = now.Add(patUsageWindow())
		// 简单防膨胀：超过 10k 条目时清理过期项
		if len(patUsageMem) > 10000 {
			for k, exp := range patUsageMem {
				if !exp.After(now) {
					delete(patUsageMem, k)
				}
			}
		}
		return true
	}
	ok, err := patUsageRedis.SetNX(ctx, key, 1, patUsageWindow()).Result()
	if err != nil {
		// Redis 故障 fail-open（可用性优先；审计为 best-effort 观测面）
		return true
	}
	return ok
}

// AllowUsageRecord 使用行去重：窗口内同一 (tokenID, method, path) 只写一次。
func AllowUsageRecord(ctx context.Context, tokenID int, method, path string) bool {
	key := fmt.Sprintf("pat:audit:%d:%s:%s", tokenID, method, path)
	return allowByGate(ctx, key)
}

// ShouldTouchLastUsed last_used_at/use_count 落库节流：每令牌窗口期最多一次。
func ShouldTouchLastUsed(ctx context.Context, tokenID int) bool {
	key := fmt.Sprintf("pat:touch:%d", tokenID)
	return allowByGate(ctx, key)
}

// AllowPATRequest 单令牌限流（R-3c）：每令牌每分钟上限；0/未启用 → 放行。
// 超限返回 false（调用方 429）。
func AllowPATRequest(ctx context.Context, tokenID int) bool {
	if patRatePerMinute <= 0 {
		return true
	}
	if patUsageRedis == nil {
		// 内存退化：固定窗口计数（per-instance）
		patLastMu.Lock()
		defer patLastMu.Unlock()
		key := strconv.Itoa(tokenID)
		now := time.Now()
		windowStart := now.Truncate(time.Minute)
		type counter struct {
			start time.Time
			n     int
		}
		// patLastMem 复用为计数器存储（与 touch 节流不共用 key 空间，前缀区分）
		if v, ok := patRateMem[key]; ok && v.start.Equal(windowStart) {
			v.n++
			patRateMem[key] = v
			return v.n <= patRatePerMinute
		}
		patRateMem[key] = counter{start: windowStart, n: 1}
		return true
	}
	key := fmt.Sprintf("pat:rate:%d:%d", tokenID, time.Now().UTC().Unix()/60)
	n, err := patUsageRedis.Incr(ctx, key).Result()
	if err == nil && n == 1 {
		_ = patUsageRedis.Expire(ctx, key, 2*time.Minute)
	}
	if err != nil {
		return true // Redis 故障 fail-open
	}
	return n <= int64(patRatePerMinute)
}

var patRateMem = map[string]struct {
	start time.Time
	n     int
}{}
