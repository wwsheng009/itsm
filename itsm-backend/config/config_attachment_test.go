package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// newTestConfig 在隔离的临时目录中重置全局 viper 并调用 LoadConfig。
//
// viper 是包级单例，override 值会跨调用残留（生产进程只调用一次 LoadConfig，
// 因此不受影响）；测试必须在每个用例前 Reset，才能验证「未配置 = 默认全关」。
func newTestConfig(t *testing.T, yaml string) *Config {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	viper.Reset()
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	return cfg
}

// TestLoadConfig_AttachmentSwitches 覆盖 P0-4 验收：附件域四个灰度开关可被
// config.yaml 解析，并可按 config.yaml.example 的 `${VAR:default}` 形式被
// ATTACHMENT_* 环境变量覆盖（灰度/回退只改环境变量即可，无需改代码）。
func TestLoadConfig_AttachmentSwitches(t *testing.T) {
	// 环境变量覆盖：write false → true，dual_write false → true
	t.Setenv("ATTACHMENT_GENERIC_WRITE_ENABLED", "true")
	t.Setenv("ATTACHMENT_DUAL_WRITE_ENABLED", "true")

	cfg := newTestConfig(t, `attachment:
  generic_read_enabled: true
  generic_write_enabled: ${ATTACHMENT_GENERIC_WRITE_ENABLED:false}
  dual_write_enabled: ${ATTACHMENT_DUAL_WRITE_ENABLED:false}
  inline_image_enabled: true
`)

	if !cfg.Attachment.GenericReadEnabled {
		t.Error("generic_read_enabled 应为 true（来自 config.yaml）")
	}
	if !cfg.Attachment.GenericWriteEnabled {
		t.Error("generic_write_enabled 应为 true（来自 ATTACHMENT_* 环境变量覆盖）")
	}
	if !cfg.Attachment.DualWriteEnabled {
		t.Error("dual_write_enabled 应为 true（来自 ATTACHMENT_* 环境变量覆盖）")
	}
	if !cfg.Attachment.InlineImageEnabled {
		t.Error("inline_image_enabled 应为 true（来自 config.yaml）")
	}
}

// TestLoadConfig_AttachmentSwitchesDefaultOff 验证未配置 attachment 块时四个开关全为
// false，即「默认全关 = 保留现网旧链路、零行为变化」。
func TestLoadConfig_AttachmentSwitchesDefaultOff(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if cfg.Attachment.GenericReadEnabled || cfg.Attachment.GenericWriteEnabled ||
		cfg.Attachment.DualWriteEnabled || cfg.Attachment.InlineImageEnabled {
		t.Errorf("附件域开关默认应为全 false，实际: %+v", cfg.Attachment)
	}
}

// TestLoadConfig_AttachmentCleanupSwitches BE-8：清理任务开关与数值项可被
// config.yaml + `${VAR:default}` 环境变量覆盖解析；布尔开关保持「零值即关闭」。
func TestLoadConfig_AttachmentCleanupSwitches(t *testing.T) {
	t.Setenv("ATTACHMENT_CLEANUP_PURGE_ENABLED", "true")
	t.Setenv("ATTACHMENT_RETENTION_DAYS", "7")

	cfg := newTestConfig(t, `attachment:
  cleanup_enabled: true
  cleanup_purge_enabled: ${ATTACHMENT_CLEANUP_PURGE_ENABLED:false}
  retention_days: ${ATTACHMENT_RETENTION_DAYS:30}
  cleanup_interval_minutes: 60
  cleanup_batch_size: 50
`)

	if !cfg.Attachment.CleanupEnabled {
		t.Error("cleanup_enabled 应为 true（来自 config.yaml）")
	}
	if !cfg.Attachment.CleanupPurgeEnabled {
		t.Error("cleanup_purge_enabled 应为 true（来自 ATTACHMENT_* 环境变量覆盖）")
	}
	if cfg.Attachment.RetentionDays != 7 {
		t.Errorf("retention_days 应为 7（来自 ATTACHMENT_* 环境变量覆盖），实际 %d", cfg.Attachment.RetentionDays)
	}
	if cfg.Attachment.CleanupIntervalMinutes != 60 {
		t.Errorf("cleanup_interval_minutes 应为 60，实际 %d", cfg.Attachment.CleanupIntervalMinutes)
	}
	if cfg.Attachment.CleanupBatchSize != 50 {
		t.Errorf("cleanup_batch_size 应为 50，实际 %d", cfg.Attachment.CleanupBatchSize)
	}
}

// TestLoadConfig_AttachmentCleanupDefaults 验证未配置 attachment 块时：
// 清理任务开关全 false（不启动、不落删），数值项补齐 30 天 / 360 分钟 / 200 条默认值。
func TestLoadConfig_AttachmentCleanupDefaults(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if cfg.Attachment.CleanupEnabled || cfg.Attachment.CleanupPurgeEnabled {
		t.Errorf("清理任务开关默认应为 false，实际: %+v", cfg.Attachment)
	}
	if cfg.Attachment.RetentionDays != attachmentDefaultRetentionDays {
		t.Errorf("retention_days 默认应为 %d，实际 %d", attachmentDefaultRetentionDays, cfg.Attachment.RetentionDays)
	}
	if cfg.Attachment.CleanupIntervalMinutes != attachmentDefaultCleanupIntervalMinutes {
		t.Errorf("cleanup_interval_minutes 默认应为 %d，实际 %d",
			attachmentDefaultCleanupIntervalMinutes, cfg.Attachment.CleanupIntervalMinutes)
	}
	if cfg.Attachment.CleanupBatchSize != attachmentDefaultCleanupBatchSize {
		t.Errorf("cleanup_batch_size 默认应为 %d，实际 %d",
			attachmentDefaultCleanupBatchSize, cfg.Attachment.CleanupBatchSize)
	}
}
