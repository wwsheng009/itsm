package migration

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// F4 回归（2026-10-12）：历史迁移不兼容兜底的判定边界。
func TestIsLegacyVersion(t *testing.T) {
	cases := map[string]bool{
		"20260616_security_role_permissions": true,
		"20260901":                           true, // 边界日：含当日
		"20260901_boundary_suffix":           false,
		"20260902_after_cutoff":              false,
		"20261012_rbac_endpoint_acls_fresh_schema": false,
		"add_missing_indexes":                      true, // 无日期前缀 = 历史脚本
		"":                                         false,
	}
	for version, want := range cases {
		assert.Equal(t, want, isLegacyVersion(version), version)
	}
}

func TestIsLegacySchemaError(t *testing.T) {
	assert.True(t, isLegacySchemaError(errors.New(
		`pq: column "created_at" of relation "role_permissions" does not exist (42703)`)))
	assert.True(t, isLegacySchemaError(errors.New(
		`pq: null value in column "created_at" of relation "marketplace_items" violates not-null constraint (23502)`)))
	assert.True(t, isLegacySchemaError(errors.New(`pq: relation "endpoint_acls" does not exist (42P01)`)))
	assert.True(t, isLegacySchemaError(errors.New(`pq: duplicate key value violates unique constraint "uq_process_bindings_active_route_key" (23505)`)))
	assert.False(t, isLegacySchemaError(errors.New(`pq: syntax error at or near "SELET" (42601)`)))
	assert.False(t, isLegacySchemaError(nil))
}
