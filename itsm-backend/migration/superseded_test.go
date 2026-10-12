package migration

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// 回归（2026-10-12，F4）：被替代的历史迁移与替代版本必须都在 migrations 目录中，
// 且收养逻辑在替代版本缺席时保持 no-op（旧版本部署不改变重放语义）。
func TestSupersededMigrations_RegistryConsistency(t *testing.T) {
	dir := "../migrations"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("skip: real migrations dir not available at %q (%v)", dir, err)
	}
	migs, err := FilesystemMigrations(dir)
	require.NoError(t, err)

	versions := make(map[string]bool, len(migs))
	for _, m := range migs {
		versions[m.Version] = true
	}
	for version, rule := range supersededMigrations {
		assert.True(t, versions[version], "被替代版本应保留在目录中以便追溯: %s", version)
		if rule.Replacement != "" {
			assert.True(t, versions[rule.Replacement], "替代版本必须存在: %s", rule.Replacement)
		}
	}
}

func TestReconcileSuperseded_NoReplacementIsNoop(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop().Sugar())
	available := []Migration{{Version: "20260501_rbac_endpoint_acls"}}
	require.NoError(t, m.reconcileSupersededMigrations(context.Background(), available))
}
