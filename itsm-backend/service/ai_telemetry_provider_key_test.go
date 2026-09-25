package service

// BE-6（主计划 §3.6/§3.7）可观测扩展单测：
//  1. provider_key 写入：DB 实例调用按生效 key 落库；
//  2. 静态回退/旧路径写 NULL（历史口径不变，QA-3 回归门禁）；
//  3. 按实例聚合：key 桶 + 静态回退桶（空 key、StaticFallback=true）、排序与成功率/延迟/令牌口径；
//  4. JSON 契约：camelCase；未聚合时 byProvider 字段不出现（omitempty）。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObserveLLMCallWithProviderKeyWritesKeyAndNull 锁定 §3.7 写入语义：
// 只有 DB 实例（请求级/个人默认/租户默认）写 key；静态回退与旧 Observe 路径写 NULL。
func TestObserveLLMCallWithProviderKeyWritesKeyAndNull(t *testing.T) {
	db, svc := newTelemetryTestDB(t)
	ctx := context.Background()
	require.NotNil(t, svc.repo)

	// DB 实例路径：写 key（两条同 key，覆盖聚合场景）
	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "prov_alpha", "openai", "gpt-4o", 120, 1500, true))
	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "prov_alpha", "openai", "gpt-4o", 80, 2500, false))
	// 静态回退路径：Observer 旧签名（Observe）与空 key 都必须落 NULL，不伪造 key
	require.NoError(t, svc.repo.ObserveLLMCall(ctx, "static-openai", "gpt-4o", 10, 100, true))
	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "", "static-openai", "gpt-4o", 20, 300, true))

	var keyed int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_llm_calls WHERE provider_key = 'prov_alpha'`).Scan(&keyed))
	assert.Equal(t, 2, keyed, "DB 实例调用必须写入生效 key")

	var staticFallback int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_llm_calls WHERE provider_key IS NULL`).Scan(&staticFallback))
	assert.Equal(t, 2, staticFallback, "静态回退/旧路径必须写 NULL")

	var emptyString int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_llm_calls WHERE provider_key = ''`).Scan(&emptyString))
	assert.Equal(t, 0, emptyString, "空 key 不得落空串（必须 NULLIF 收敛为 NULL）")
}

// TestAggregateLLMByProviderBuckets 锁定 §3.6 聚合口径：按 provider_key 分组、静态回退归空 key 桶、
// 调用数降序 + key 升序，以及 successRate/avgLatencySeconds/totalTokens 计算。
func TestAggregateLLMByProviderBuckets(t *testing.T) {
	_, svc := newTelemetryTestDB(t)
	ctx := context.Background()
	require.NotNil(t, svc.repo)

	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "prov_alpha", "openai", "gpt-4o", 120, 1500, true))
	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "prov_alpha", "openai", "gpt-4o", 80, 2500, false))
	require.NoError(t, svc.repo.ObserveLLMCallWithProviderKey(ctx, "prov_beta", "minimax", "abab6", 50, 1000, true))
	require.NoError(t, svc.repo.ObserveLLMCall(ctx, "static-openai", "gpt-4o", 10, 100, true))

	stats, err := svc.repo.AggregateLLMByProvider(ctx, 30)
	require.NoError(t, err)
	require.Len(t, stats, 3)

	// 排序：COUNT(*) DESC, provider_key ASC —— prov_alpha（2 次）居首
	assert.Equal(t, "prov_alpha", stats[0].ProviderKey)
	assert.Equal(t, "openai", stats[0].Provider)
	assert.False(t, stats[0].StaticFallback)
	assert.Equal(t, 2, stats[0].CallCount)
	assert.Equal(t, 1, stats[0].SuccessCount)
	assert.InDelta(t, 0.5, stats[0].SuccessRate, 1e-9)
	assert.InDelta(t, 2.0, stats[0].AvgLatencySeconds, 1e-9) // (1500+2500)/2/1000
	assert.Equal(t, int64(200), stats[0].TotalTokens)

	byKey := make(map[string]LLMProviderStat, len(stats))
	for _, s := range stats {
		byKey[s.ProviderKey] = s
	}

	static, ok := byKey[""]
	require.True(t, ok, "静态回退（NULL）必须归入空 key 桶")
	assert.True(t, static.StaticFallback, "空 key 桶必须标记 StaticFallback=true")
	assert.Equal(t, 1, static.CallCount)
	assert.Equal(t, 1, static.SuccessCount)
	assert.InDelta(t, 1.0, static.SuccessRate, 1e-9)
	assert.InDelta(t, 0.1, static.AvgLatencySeconds, 1e-9)
	assert.Equal(t, int64(10), static.TotalTokens)

	beta := byKey["prov_beta"]
	assert.Equal(t, 1, beta.CallCount)
	assert.Equal(t, "minimax", beta.Provider)
	assert.False(t, beta.StaticFallback)
	assert.InDelta(t, 1.0, beta.SuccessRate, 1e-9)
}

// TestAggregateLLMByProviderEmptyTable 空表返回空切片（不是 nil error、不是 panic）：
// 新部署/未产生调用的环境下 /ai/metrics 必须正常返回。
func TestAggregateLLMByProviderEmptyTable(t *testing.T) {
	_, svc := newTelemetryTestDB(t)
	stats, err := svc.repo.AggregateLLMByProvider(context.Background(), 30)
	require.NoError(t, err)
	assert.Empty(t, stats)
}

// TestLLMProviderStatJSONShapeCamelCase 前端 TS 契约：byProvider 条目字段全 camelCase，
// 禁止 Go 字段名（PascalCase）泄漏到响应体。
func TestLLMProviderStatJSONShapeCamelCase(t *testing.T) {
	raw, err := json.Marshal(LLMProviderStat{
		ProviderKey:       "prov_alpha",
		Provider:          "openai",
		StaticFallback:    false,
		CallCount:         2,
		SuccessCount:      1,
		SuccessRate:       0.5,
		AvgLatencySeconds: 2,
		TotalTokens:       200,
	})
	require.NoError(t, err)
	text := string(raw)

	for _, key := range []string{
		"providerKey", "provider", "staticFallback", "callCount",
		"successCount", "successRate", "avgLatencySeconds", "totalTokens",
	} {
		assert.Contains(t, text, `"`+key+`"`, "byProvider 条目必须包含 camelCase 字段 %s", key)
	}
	for _, forbidden := range []string{`"ProviderKey"`, `"CallCount"`, `"StaticFallback"`, `"TotalTokens"`} {
		assert.NotContains(t, text, forbidden, "不得泄漏 Go 字段名 %s", forbidden)
	}
}

// TestAIMetricsByProviderOmittedWhenNotAggregated QA-3 回归门禁：
// 灰度开关关闭时 GetMetrics 不填 ByProvider（nil），omitempty 保证响应与开关引入前逐字节一致；
// 开启并聚合后字段出现且为 camelCase 数组。
func TestAIMetricsByProviderOmittedWhenNotAggregated(t *testing.T) {
	raw, err := json.Marshal(&AIMetrics{TotalRequests: 1})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "byProvider", "开关关闭时响应不得出现 byProvider 字段")

	rawOn, err := json.Marshal(&AIMetrics{
		TotalRequests: 1,
		ByProvider:    []LLMProviderStat{{ProviderKey: "prov_alpha", CallCount: 1}},
	})
	require.NoError(t, err)
	assert.Contains(t, string(rawOn), `"byProvider"`)
	assert.Contains(t, string(rawOn), `"providerKey"`)
}

// TestObservationKeyStaticFallbackWritesEmpty 锁定网关 observationKey 映射（§3.7）：
// 静态回退写空串（落库 NULL），DB 实例三种来源都写 key。
func TestObservationKeyStaticFallbackWritesEmpty(t *testing.T) {
	assert.Equal(t, "", observationKey(ProviderResolution{Key: "static", Source: ProviderSourceStatic}))
	assert.Equal(t, "prov_alpha", observationKey(ProviderResolution{Key: "prov_alpha", Source: ProviderSourceRequest}))
	assert.Equal(t, "prov_alpha", observationKey(ProviderResolution{Key: "prov_alpha", Source: ProviderSourceUser}))
	assert.Equal(t, "prov_alpha", observationKey(ProviderResolution{Key: "prov_alpha", Source: ProviderSourceTenant}))
}
