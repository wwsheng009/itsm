package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件守卫「租户 token 配额与单价口径」的性质。前五条移植自上游同名测试（作规格），
// 第六条（EstimateCost）为本地补充：上游实现有该能力但**没有测试**。
//
// 三条诚实原则，每条都有对应测试锁：
//  1. 未配置配额 = 不限制，但如实报 quotaConfigured=false（不装有限额）；
//  2. 单价未配置 = estimatedCost=0 且 costConfigured=false（绝不给猜的单价）；
//  3. 配额评估失败 fail-open（配额是预算保护不是安全边界，DB 抖动不该让 AI 全体不可用）。

// newQuotaTestDB 建内存库并写入本月计量行。
func newQuotaTestDB(t *testing.T, tenantID int, tokens []int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:quota_"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS ai_llm_calls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TIMESTAMP NOT NULL,
		provider TEXT, model TEXT, tokens INTEGER, latency_ms INTEGER, success BOOLEAN,
		tenant_id INTEGER NOT NULL DEFAULT 0, request_id TEXT)`)
	require.NoError(t, err)
	now := time.Now()
	for _, tk := range tokens {
		_, err := db.Exec(`INSERT INTO ai_llm_calls (created_at, provider, model, tokens, latency_ms, success, tenant_id)
			VALUES (?, 'p', 'm', ?, 10, 1, ?)`, now, tk, tenantID)
		require.NoError(t, err)
	}
	return db
}

// TestQuotaUnconfiguredIsHonest：未配置配额时不限制、不装有限额。
func TestQuotaUnconfiguredIsHonest(t *testing.T) {
	db := newQuotaTestDB(t, 1, []int{100, 200})
	q := NewAIQuotaService(db)                  // 无环境变量
	t.Setenv("ITSM_AI_TOKEN_MONTHLY_QUOTA", "") // 显式清空

	usage, err := q.Evaluate(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, usage.QuotaConfigured, "未配置就是未配置，不得装作有配额")
	assert.Equal(t, int64(300), usage.TokensUsed, "计量照实聚合")
	assert.Empty(t, usage.Alert)

	// Check 恒放行
	allowed, _, err := q.Check(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, allowed)
}

// TestQuotaWarningAtThreshold：达到预警阈值（默认 0.8）时 Alert=warning。
func TestQuotaWarningAtThreshold(t *testing.T) {
	db := newQuotaTestDB(t, 1, []int{850}) // 850/1000 = 85% > 80%
	q := NewAIQuotaService(db)
	q.SetQuota(1000)

	usage, err := q.Evaluate(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, usage.QuotaConfigured)
	assert.Equal(t, "warning", usage.Alert)
	assert.Contains(t, usage.AlertMessage, "85%")
	assert.Equal(t, int64(150), usage.TokensRemaining)
}

// TestQuotaExceededBlocks：超限后 Check 拒绝（网关应把它变成一次拒绝的调用，不是 500）。
func TestQuotaExceededBlocks(t *testing.T) {
	db := newQuotaTestDB(t, 1, []int{1200}) // 1200 > 1000
	q := NewAIQuotaService(db)
	q.SetQuota(1000)

	usage, err := q.Evaluate(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "exceeded", usage.Alert)
	assert.Zero(t, usage.TokensRemaining, "剩余不得为负")

	allowed, _, err := q.Check(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, allowed, "超限必须拒绝")
}

// TestQuotaMonthWindowIsCurrentMonthOnly：上月用量不计入本月配额。
func TestQuotaMonthWindowIsCurrentMonthOnly(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:quota_lastmonth?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE ai_llm_calls (
		id INTEGER PRIMARY KEY AUTOINCREMENT, created_at TIMESTAMP NOT NULL,
		provider TEXT, model TEXT, tokens INTEGER, latency_ms INTEGER, success BOOLEAN,
		tenant_id INTEGER NOT NULL DEFAULT 0, request_id TEXT)`)
	require.NoError(t, err)

	now := time.Now()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	lastMonthEnd := thisMonthStart.Add(-time.Hour) // 上月最后一小时
	_, err = db.Exec(`INSERT INTO ai_llm_calls (created_at, provider, model, tokens, latency_ms, success, tenant_id)
		VALUES (?, 'p', 'm', 99999, 10, 1, 1)`, lastMonthEnd)
	require.NoError(t, err)

	q := NewAIQuotaService(db)
	q.SetQuota(100)
	usage, err := q.Evaluate(context.Background(), 1)
	require.NoError(t, err)
	assert.Zero(t, usage.TokensUsed, "上月用量不得计入本月配额窗口")
}

// TestQuotaFailOpenOnDBError：评估失败放行——配额是预算保护不是安全边界。
func TestQuotaFailOpenOnDBError(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:quota_dead?mode=memory")
	require.NoError(t, err)
	// 不建表：查询必失败
	q := NewAIQuotaService(db)
	q.SetQuota(1)

	allowed, _, err := q.Check(context.Background(), 1)
	require.Error(t, err, "评估错误应透传给调用方记录")
	assert.True(t, allowed, "评估失败必须放行（fail-open）")
}

// TestEstimateCostHonesty 本地补充（上游无此用例）：单价未配置时不得给数字；
// 配置后按「每千 token」口径换算并保留 4 位小数。
func TestEstimateCostHonesty(t *testing.T) {
	t.Setenv("ITSM_AI_TOKEN_PRICE_PER_1K", "")
	t.Setenv("ITSM_AI_TOKEN_PRICE_CURRENCY", "")
	db := newQuotaTestDB(t, 1, nil)

	unconfigured := NewAIQuotaService(db).EstimateCost(1000)
	assert.False(t, unconfigured.CostConfigured, "未配置单价不得声称已配置成本口径")
	assert.Zero(t, unconfigured.EstimatedCost, "宁可不给数字，也不给猜的单价")
	assert.Equal(t, "CNY", unconfigured.Currency, "币种默认 CNY")

	t.Setenv("ITSM_AI_TOKEN_PRICE_PER_1K", "0.5")
	t.Setenv("ITSM_AI_TOKEN_PRICE_CURRENCY", "USD")
	configured := NewAIQuotaService(db).EstimateCost(1234)
	assert.True(t, configured.CostConfigured)
	assert.Equal(t, "USD", configured.Currency)
	assert.InDelta(t, 0.617, configured.EstimatedCost, 0.0001, "1234 tokens × 0.5/1000 = 0.617")
	assert.Equal(t, 0.5, configured.PricePer1K)
}
