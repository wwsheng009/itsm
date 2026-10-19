package config

import "testing"

// O0-02 条件项闭环 + O1-02 条件项闭环：AI 观测/健康配置块解析
// （config.yaml + `${VAR:default}` 环境变量覆盖；缺省 = 保留期生效 / 巡检关闭）。

func TestLoadConfig_AITelemetryRetentionSwitches(t *testing.T) {
	t.Setenv("AI_TELEMETRY_RETENTION_DAYS", "30")
	t.Setenv("AI_TELEMETRY_CLEANUP_BATCH_SIZE", "500")

	cfg := newTestConfig(t, `ai_telemetry:
  retention_days: ${AI_TELEMETRY_RETENTION_DAYS:90}
  cleanup_interval_minutes: ${AI_TELEMETRY_CLEANUP_INTERVAL_MINUTES:1440}
  cleanup_batch_size: ${AI_TELEMETRY_CLEANUP_BATCH_SIZE:5000}
`)
	if cfg.AITelemetry.RetentionDays != 30 {
		t.Errorf("retention_days 应为 30（来自 AI_TELEMETRY_* 环境变量覆盖），实际 %d", cfg.AITelemetry.RetentionDays)
	}
	if cfg.AITelemetry.CleanupIntervalMinutes != 1440 {
		t.Errorf("cleanup_interval_minutes 应为默认 1440，实际 %d", cfg.AITelemetry.CleanupIntervalMinutes)
	}
	if cfg.AITelemetry.CleanupBatchSize != 500 {
		t.Errorf("cleanup_batch_size 应为 500（来自 AI_TELEMETRY_* 环境变量覆盖），实际 %d", cfg.AITelemetry.CleanupBatchSize)
	}
}

func TestLoadConfig_AITelemetryDefaultsZero(t *testing.T) {
	// 未配置 ai_telemetry 块 → 零值（保留期清理缺省由 service 构造器兜底：90 天 / 24h / 5000 行）。
	cfg := newTestConfig(t, "server:\n  port: 8080\n")
	if cfg.AITelemetry.RetentionDays != 0 || cfg.AITelemetry.CleanupIntervalMinutes != 0 || cfg.AITelemetry.CleanupBatchSize != 0 {
		t.Errorf("未配置时 ai_telemetry 应为零值，实际: %+v", cfg.AITelemetry)
	}
}

func TestLoadConfig_AIHealthSweeperSwitches(t *testing.T) {
	t.Setenv("AI_HEALTH_SWEEPER_ENABLED", "true")
	t.Setenv("AI_HEALTH_SWEEPER_INTERVAL_MINUTES", "5")

	cfg := newTestConfig(t, `ai_health:
  sweeper_enabled: ${AI_HEALTH_SWEEPER_ENABLED:false}
  sweeper_interval_minutes: ${AI_HEALTH_SWEEPER_INTERVAL_MINUTES:15}
  sweeper_window_days: 14
`)
	if !cfg.AIHealth.SweeperEnabled {
		t.Error("sweeper_enabled 应为 true（来自 AI_HEALTH_* 环境变量覆盖）")
	}
	if cfg.AIHealth.SweeperIntervalMinutes != 5 {
		t.Errorf("sweeper_interval_minutes 应为 5，实际 %d", cfg.AIHealth.SweeperIntervalMinutes)
	}
	if cfg.AIHealth.SweeperWindowDays != 14 {
		t.Errorf("sweeper_window_days 应为 14，实际 %d", cfg.AIHealth.SweeperWindowDays)
	}
}

func TestLoadConfig_AIHealthSweeperDefaultsDisabled(t *testing.T) {
	// 缺省 = 不自动打扰（仅显式触发端点可用）；数值零值由 service 构造器兜底（15 分钟 / 7 天）。
	cfg := newTestConfig(t, "server:\n  port: 8080\n")
	if cfg.AIHealth.SweeperEnabled {
		t.Error("sweeper_enabled 默认必须为 false（不默认打扰）")
	}
	if cfg.AIHealth.SweeperIntervalMinutes != 0 || cfg.AIHealth.SweeperWindowDays != 0 {
		t.Errorf("未配置时 ai_health 数值项应为零值，实际: %+v", cfg.AIHealth)
	}
}
