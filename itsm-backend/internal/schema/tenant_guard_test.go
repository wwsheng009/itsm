package schema

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTenantExemptTables_NoDuplicates 单一真相源内部一致性：表名不能重复。
func TestTenantExemptTables_NoDuplicates(t *testing.T) {
	seen := map[string]int{}
	for _, e := range TenantExemptTables {
		seen[e.TableName]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("duplicate exempt entry: %s appears %d times", name, count)
		}
	}
}

// TestTenantExemptTables_RequiredFields 每条豁免必须写齐 reason/owner/scope + 复核日期。
func TestTenantExemptTables_RequiredFields(t *testing.T) {
	for _, e := range TenantExemptTables {
		assert.NotEmpty(t, e.TableName, "TableName required")
		assert.NotEmpty(t, e.Reason, "%s: reason required (governance)", e.TableName)
		assert.NotEmpty(t, e.Owner, "%s: owner required", e.TableName)
		assert.Contains(t, []string{"platform", "global", "derived"}, e.Scope,
			"%s: scope must be one of platform/global/derived", e.TableName)
		assert.False(t, e.ReviewedAt.IsZero(), "%s: reviewed_at required", e.TableName)
		assert.True(t, e.ReviewedAt.Before(time.Now().UTC().AddDate(0, 0, 1)),
			"%s: reviewed_at must not be in the future", e.TableName)
	}
}

// TestBuildExemptSet 与 TenantExemptTables 一一对应。
func TestBuildExemptSet(t *testing.T) {
	set := buildExemptSet()
	assert.Len(t, set, len(TenantExemptTables))
	for _, e := range TenantExemptTables {
		assert.True(t, set[e.TableName], "missing %s in exempt set", e.TableName)
	}
}

// TestResolvePolicy 验证策略解析：env 默认 + 显式覆盖。
func TestResolvePolicy(t *testing.T) {
	cases := []struct {
		env, policy string
		want        Policy
	}{
		{"production", "", PolicyFatal},
		{"private", "", PolicyFatal},
		{"saas", "", PolicyFatal},
		{"saas_msp", "", PolicyFatal},
		{"dev", "", PolicyWarn},
		{"", "", PolicyWarn}, // unset → dev default
		{"production", "warn", PolicyWarn},
		{"production", "silent", PolicySilent},
		{"production", "fatal", PolicyFatal},
		{"production", "bogus", PolicyFatal}, // invalid → fallback to env-based
	}
	for _, c := range cases {
		t.Run(c.env+"_"+c.policy, func(t *testing.T) {
			t.Setenv("ENV", c.env)
			t.Setenv("ITSM_TENANT_GUARD_POLICY", c.policy)
			assert.Equal(t, c.want, ResolvePolicy())
		})
	}
}

// TestApplyGuard_Silent 验证 silent 策略不读 DB（用于测试隔离）。
func TestApplyGuard_Silent(t *testing.T) {
	violations, err := ApplyGuard(context.TODO(), nil, nil, PolicySilent)
	require.NoError(t, err)
	assert.Nil(t, violations)
}

// TestTenantExemptTables_DerivedChildTables 覆盖 2026-10-18 新增豁免
// （问题调查四表 + PIR 评审关系表）：均为「父表继承租户」型子表，
// 豁免理由必须指向实际承载租户维度的父表/隔离方式，且复核日期为登记日。
func TestTenantExemptTables_DerivedChildTables(t *testing.T) {
	reviewed := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	want := map[string]struct {
		owner     string
		reasonSub string
	}{
		"problem_investigations":      {"problem", "JOIN problems"},
		"problem_investigation_steps": {"problem", "JOIN problems"},
		"problem_root_cause_analyses": {"problem", "JOIN problems"},
		"problem_solutions":           {"problem", "JOIN problems"},
		"user_pir_reviews":            {"change", "change_pi_rs"},
	}

	seen := map[string]bool{}
	for _, e := range TenantExemptTables {
		w, ok := want[e.TableName]
		if !ok {
			continue
		}
		seen[e.TableName] = true
		assert.Equal(t, "derived", e.Scope, "%s: 子表豁免 scope 应为 derived", e.TableName)
		assert.Equal(t, w.owner, e.Owner, "%s: owner 应由业务域负责", e.TableName)
		assert.Contains(t, e.Reason, w.reasonSub, "%s: reason 需引用承载租户维度的父表/隔离方式", e.TableName)
		assert.True(t, e.ReviewedAt.Equal(reviewed), "%s: reviewed_at 应为登记日 2026-10-18", e.TableName)
	}
	for name := range want {
		assert.True(t, seen[name], "%s 缺失于 TenantExemptTables（新增豁免必须登记）", name)
	}
}
