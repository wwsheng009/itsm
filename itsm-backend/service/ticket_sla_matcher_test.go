package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =====================================================================
// P0-2: SLA 四级匹配链测试（方案 §4.2 v1.11）
// L1 grant > L2 tier policy > L3b 自建定义 > L3c 租户默认 > L4 fail-loud
// =====================================================================

func setupSLAMatcherTest(t *testing.T) (*ent.Client, *TicketSLAService, context.Context, *ent.Tenant, *ent.Tenant) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:sla_matcher_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	logger := zaptest.NewLogger(t).Sugar()
	svc := NewTicketSLAService(client, logger)
	ctx := context.Background()

	customer := client.Tenant.Create().
		SetName("Matcher Customer").
		SetCode(fmt.Sprintf("matcher-cust-%d", time.Now().UnixNano())).
		SetDomain("matcher-cust.example.com").
		SetStatus("active").
		SaveX(ctx)
	provider := client.Tenant.Create().
		SetName("Matcher Provider").
		SetCode(fmt.Sprintf("matcher-prov-%d", time.Now().UnixNano())).
		SetDomain("matcher-prov.example.com").
		SetStatus("active").
		SaveX(ctx)

	return client, svc, ctx, customer, provider
}

func createProviderDefinition(t *testing.T, ctx context.Context, client *ent.Client, providerTenantID int, name, serviceType, priority string, response, resolution int) *ent.SLADefinition {
	t.Helper()
	builder := client.SLADefinition.Create().
		SetName(name).
		SetResponseTime(response).
		SetResolutionTime(resolution).
		SetIsActive(true).
		SetTenantID(providerTenantID).
		SetOwnerProviderTenantID(providerTenantID)
	if serviceType != "" {
		builder = builder.SetServiceType(serviceType)
	}
	if priority != "" {
		builder = builder.SetPriority(priority)
	}
	return builder.SaveX(ctx)
}

func createGrant(t *testing.T, ctx context.Context, client *ent.Client, defID, customerTenantID, providerTenantID int, ticketTypeID int64, validUntil *time.Time) *ent.SLADefinitionGrant {
	t.Helper()
	builder := client.SLADefinitionGrant.Create().
		SetSLADefinitionID(defID).
		SetCustomerTenantID(customerTenantID).
		SetProviderTenantID(providerTenantID).
		SetTicketTypeID(ticketTypeID).
		SetIsActive(true).
		SetTenantID(providerTenantID)
	if validUntil != nil {
		builder = builder.SetValidUntil(*validUntil)
	}
	return builder.SaveX(ctx)
}

// L1：显式类型绑定优先于类型无关授权。
func TestTicketSLAMatcher_L1TypeBindingPreferred(t *testing.T) {
	client, svc, ctx, customer, provider := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	defBound := createProviderDefinition(t, ctx, client, provider.ID, "Contract Bound SLA", "incident", "high", 120, 480)
	defAgnostic := createProviderDefinition(t, ctx, client, provider.ID, "Contract Agnostic SLA", "incident", "high", 240, 960)
	createGrant(t, ctx, client, defBound.ID, customer.ID, provider.ID, 99, nil)
	createGrant(t, ctx, client, defAgnostic.ID, customer.ID, provider.ID, 0, nil)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 99, "")
	require.NoError(t, err)
	assert.Equal(t, defBound.ID, result.SLADefinitionID, "显式类型绑定的 grant 必须优先")
	assert.Equal(t, "L1", result.MatchedLayer)
	require.NotNil(t, result.ResponseDeadline)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(120*time.Minute)), "使用绑定定义的 120 分钟响应时限")
}

// L1：无显式类型绑定时命中类型无关授权。
func TestTicketSLAMatcher_L1TypeAgnostic(t *testing.T) {
	client, svc, ctx, customer, provider := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	def := createProviderDefinition(t, ctx, client, provider.ID, "Contract Agnostic SLA", "incident", "high", 90, 360)
	createGrant(t, ctx, client, def.ID, customer.ID, provider.ID, 0, nil)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, def.ID, result.SLADefinitionID)
	assert.Equal(t, "L1", result.MatchedLayer)
}

// L1：过期授权不命中（valid_until 已过），回落 L4（无其他数据）。
func TestTicketSLAMatcher_L1ExpiredGrantFallsThrough(t *testing.T) {
	client, svc, ctx, customer, provider := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	def := createProviderDefinition(t, ctx, client, provider.ID, "Expired Contract SLA", "incident", "high", 120, 480)
	expired := now.Add(-24 * time.Hour)
	createGrant(t, ctx, client, def.ID, customer.ID, provider.ID, 0, &expired)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, "L4", result.MatchedLayer, "过期授权必须被跳过")
	assert.Equal(t, 0, result.SLADefinitionID)
}

// L2：客户等级策略命中（customer_tier + ticket_type + priority）。
func TestTicketSLAMatcher_L2TierPolicy(t *testing.T) {
	client, svc, ctx, customer, _ := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	client.Tenant.UpdateOne(customer).SetServiceTier("gold").SaveX(ctx)
	client.SLAPolicy.Create().
		SetName("Gold Incident SLA").
		SetCustomerTier("gold").
		SetTicketType("incident").
		SetPriority("high").
		SetResponseTimeMinutes(30).
		SetResolutionTimeMinutes(120).
		SetIsActive(true).
		SetTenantID(customer.ID).
		SaveX(ctx)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, "L2", result.MatchedLayer, "有 tier 策略时必须命中 L2")
	require.NotNil(t, result.ResponseDeadline)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(30*time.Minute)))
}

// L3b：动态类型 code 快照优先于 legacy 类型值。
func TestTicketSLAMatcher_L3bDynamicTypeCode(t *testing.T) {
	client, svc, ctx, customer, _ := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	def := client.SLADefinition.Create().
		SetName("Hardware Fault SLA").
		SetServiceType("hardware_fault").
		SetPriority("high").
		SetResponseTime(45).
		SetResolutionTime(240).
		SetIsActive(true).
		SetTenantID(customer.ID).
		SaveX(ctx)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 99, "hardware_fault")
	require.NoError(t, err)
	assert.Equal(t, def.ID, result.SLADefinitionID, "快照 code 必须优先匹配（而非 legacy 'incident'）")
	assert.Equal(t, "L3b", result.MatchedLayer)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(45*time.Minute)))
}

// L3c：类型无关租户默认定义（service_type 为空）兜底。
func TestTicketSLAMatcher_L3cTenantDefault(t *testing.T) {
	client, svc, ctx, customer, _ := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	def := client.SLADefinition.Create().
		SetName("Tenant Default SLA").
		SetResponseTime(240).
		SetResolutionTime(1440).
		SetIsActive(true).
		SetTenantID(customer.ID).
		SaveX(ctx)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, def.ID, result.SLADefinitionID)
	assert.Equal(t, "L3c", result.MatchedLayer)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(240*time.Minute)))
}

// L4：全未命中 → fail-loud（默认 60/480 + MatchedLayer=L4，无定义 ID）。
func TestTicketSLAMatcher_L4FailLoud(t *testing.T) {
	client, svc, ctx, customer, _ := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, "L4", result.MatchedLayer)
	assert.Equal(t, 0, result.SLADefinitionID)
	require.NotNil(t, result.ResponseDeadline)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(60*time.Minute)), "L4 保持现状默认 60 分钟")
	require.NotNil(t, result.ResolutionDeadline)
	assert.True(t, result.ResolutionDeadline.Equal(now.Add(480*time.Minute)), "L4 保持现状默认 480 分钟")
}

// 优先级全链：同时存在 L1/L2/L3 数据时，L1 胜出。
func TestTicketSLAMatcher_PriorityL1Wins(t *testing.T) {
	client, svc, ctx, customer, provider := setupSLAMatcherTest(t)
	defer client.Close()
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	svc = svc.withNowFunc(func() time.Time { return now })

	// L3 自建
	l3def := client.SLADefinition.Create().
		SetName("Tenant Incident SLA").
		SetServiceType("incident").
		SetPriority("high").
		SetResponseTime(300).
		SetResolutionTime(1200).
		SetIsActive(true).
		SetTenantID(customer.ID).
		SaveX(ctx)
	// L2 策略
	client.Tenant.UpdateOne(customer).SetServiceTier("gold").SaveX(ctx)
	client.SLAPolicy.Create().
		SetName("Gold Policy").
		SetCustomerTier("gold").
		SetTicketType("incident").
		SetPriority("high").
		SetResponseTimeMinutes(60).
		SetResolutionTimeMinutes(240).
		SetIsActive(true).
		SetTenantID(customer.ID).
		SaveX(ctx)
	// L1 下发
	l1def := createProviderDefinition(t, ctx, client, provider.ID, "Contract SLA", "incident", "high", 30, 180)
	createGrant(t, ctx, client, l1def.ID, customer.ID, provider.ID, 0, nil)

	result, err := svc.CalculateSLADeadlineFromRequest(ctx, customer.ID, "incident", "high", 0, "")
	require.NoError(t, err)
	assert.Equal(t, l1def.ID, result.SLADefinitionID)
	assert.Equal(t, "L1", result.MatchedLayer, "L1（合同个案）必须高于 L2/L3")
	assert.NotEqual(t, l3def.ID, result.SLADefinitionID)
	assert.True(t, result.ResponseDeadline.Equal(now.Add(30*time.Minute)))
}
