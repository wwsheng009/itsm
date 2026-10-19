package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAILLMCallObservabilityMigrationDiscovered(t *testing.T) {
	migrations, err := FilesystemMigrations("../migrations")
	require.NoError(t, err)

	var target *Migration
	for i := range migrations {
		if migrations[i].Version == "20261019_ai_llm_call_observability" {
			target = &migrations[i]
			break
		}
	}
	require.NotNil(t, target, "迁移必须被目录发现器加载")
	assert.Contains(t, target.SQLContent, "tenant_id")
	assert.Contains(t, target.SQLContent, "task_type")
	assert.Contains(t, target.SQLContent, "error_class")
	assert.Contains(t, target.SQLContent, "tokens_estimated")
	assert.Contains(t, target.RollbackSQL, "DROP COLUMN IF EXISTS tenant_id")
}
