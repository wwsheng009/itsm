package config

import "testing"

// TestLoadConfig_BotSwitches 覆盖 BP5 验收：
// bot 块可被 config.yaml 解析，`${BOT_ENABLED:默认}` 可被环境变量覆盖
// （部署侧只改环境变量即可开关，无需改代码）。
func TestLoadConfig_BotSwitches(t *testing.T) {
	t.Setenv("BOT_ENABLED", "true")

	cfg := newTestConfig(t, `bot:
  enabled: ${BOT_ENABLED:false}
`)

	if !cfg.Bot.Enabled {
		t.Error("bot.enabled 应为 true（来自 BOT_ENABLED 环境变量覆盖）")
	}
}

// TestLoadConfig_BotDefaults 验证未配置 bot 块时开关默认关闭（零行为变化）。
func TestLoadConfig_BotDefaults(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if cfg.Bot.Enabled {
		t.Error("bot.enabled 默认应为 false（关闭 = 对现有系统零行为变化）")
	}
}
