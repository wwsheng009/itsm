package migration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// O0-02 条件项闭环回归：保留期清理的支撑索引迁移必须可被自发现器加载。
//
// 背景：service/ai_telemetry_housekeeping.go 按 occurred_at < cutoff 跨租户分批删除；
// 若索引迁移缺席（被误删/改名），清理会退化为全表扫描。本测试同时锁定：
//   - 迁移文件版本号（= 文件名 stem，参与排序与登记）；
//   - SQL 内容包含幂等索引语句（CREATE INDEX IF NOT EXISTS）。
func TestFilesystemMigrations_AITelemetryRetentionIndex(t *testing.T) {
	dir := "../migrations"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("skip: real migrations dir not available at %q (%v)", dir, err)
	}
	migs, err := FilesystemMigrations(dir)
	require.NoError(t, err)

	const version = "20261018_ai_telemetry_events_retention"
	var found *Migration
	for i := range migs {
		if migs[i].Version == version {
			found = &migs[i]
			break
		}
	}
	require.NotNil(t, found, "保留期索引迁移必须存在: %s.sql", version)
	assert.Contains(t, found.SQLContent, "ai_telemetry_events_occurred_idx")
	assert.Contains(t, found.SQLContent, "IF NOT EXISTS")
}
