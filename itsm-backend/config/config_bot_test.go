package config

import (
	"testing"
	"time"
)

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

// TestLoadConfig_BotBudgetDefaults 覆盖 BP8 验收：未配置时预算护栏取默认值，
// 且默认值本身是「有上限」的（非 0 = 无限制的语义错误）。
func TestLoadConfig_BotBudgetDefaults(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if cfg.Bot.RedactionProfile != "default" {
		t.Errorf("redaction_profile 默认应为 default，实际 %q", cfg.Bot.RedactionProfile)
	}
	if cfg.Bot.Budget.MaxSteps != 24 {
		t.Errorf("max_steps 默认应为 24，实际 %d", cfg.Bot.Budget.MaxSteps)
	}
	if cfg.Bot.Budget.MaxTokens != 100000 {
		t.Errorf("max_tokens 默认应为 100000，实际 %d", cfg.Bot.Budget.MaxTokens)
	}
	if cfg.Bot.Budget.MaxToolCalls != 12 {
		t.Errorf("max_tool_calls 默认应为 12，实际 %d", cfg.Bot.Budget.MaxToolCalls)
	}
	if cfg.Bot.Budget.ToolTimeoutSeconds != 30 {
		t.Errorf("tool_timeout_seconds 默认应为 30，实际 %d", cfg.Bot.Budget.ToolTimeoutSeconds)
	}
	if cfg.Bot.Budget.MaxOutputBytes != 65536 {
		t.Errorf("max_output_bytes 默认应为 65536，实际 %d", cfg.Bot.Budget.MaxOutputBytes)
	}
	if got := cfg.Bot.Budget.BotToolTimeout(); got != 30*time.Second {
		t.Errorf("BotToolTimeout 默认应为 30s，实际 %v", got)
	}
	if cfg.Bot.ConfirmationTTLHours != 24 {
		t.Errorf("confirmation_ttl_hours 默认应为 24（B1-05），实际 %d", cfg.Bot.ConfirmationTTLHours)
	}
}

// TestLoadConfig_BotConfirmationTTL 覆盖 B1-05 验收：TTL 可用环境变量覆盖，
// 非法值（<=0）回落 24h —— 护栏不得被配置成「永不过期」。
func TestLoadConfig_BotConfirmationTTL(t *testing.T) {
	t.Setenv("BOT_CONFIRMATION_TTL_HOURS", "72")
	cfg := newTestConfig(t, "server:\n  port: 8080\n")
	if cfg.Bot.ConfirmationTTLHours != 72 {
		t.Errorf("BOT_CONFIRMATION_TTL_HOURS 覆盖失败，实际 %d", cfg.Bot.ConfirmationTTLHours)
	}

	t.Setenv("BOT_CONFIRMATION_TTL_HOURS", "0")
	cfg = newTestConfig(t, "server:\n  port: 8080\n")
	if cfg.Bot.ConfirmationTTLHours != 24 {
		t.Errorf("非正 TTL 必须回落 24h，实际 %d", cfg.Bot.ConfirmationTTLHours)
	}
}

// TestLoadConfig_BotBudgetFromYAMLAndEnv 覆盖 BP8 验收：预算参数可被 config.yaml 解析，
// 也可被 BOT_BUDGET_* / BOT_REDACTION_PROFILE 环境变量覆盖（部署侧只改环境变量）。
func TestLoadConfig_BotBudgetFromYAMLAndEnv(t *testing.T) {
	t.Setenv("BOT_BUDGET_MAX_STEPS", "40")
	t.Setenv("BOT_BUDGET_TOOL_TIMEOUT_SECONDS", "45")
	t.Setenv("BOT_REDACTION_PROFILE", "strict")

	cfg := newTestConfig(t, `bot:
  enabled: true
  redaction_profile: ${BOT_REDACTION_PROFILE:default}
  budget:
    max_steps: ${BOT_BUDGET_MAX_STEPS:24}
    max_tokens: 8000
    max_tool_calls: 5
    tool_timeout_seconds: ${BOT_BUDGET_TOOL_TIMEOUT_SECONDS:30}
    max_output_bytes: 4096
`)

	if cfg.Bot.RedactionProfile != "strict" {
		t.Errorf("redaction_profile 应为 strict（环境变量覆盖），实际 %q", cfg.Bot.RedactionProfile)
	}
	if cfg.Bot.Budget.MaxSteps != 40 {
		t.Errorf("max_steps 应为 40（环境变量覆盖），实际 %d", cfg.Bot.Budget.MaxSteps)
	}
	if cfg.Bot.Budget.MaxTokens != 8000 {
		t.Errorf("max_tokens 应为 8000（config.yaml），实际 %d", cfg.Bot.Budget.MaxTokens)
	}
	if cfg.Bot.Budget.MaxToolCalls != 5 {
		t.Errorf("max_tool_calls 应为 5（config.yaml），实际 %d", cfg.Bot.Budget.MaxToolCalls)
	}
	if cfg.Bot.Budget.ToolTimeoutSeconds != 45 {
		t.Errorf("tool_timeout_seconds 应为 45（环境变量覆盖），实际 %d", cfg.Bot.Budget.ToolTimeoutSeconds)
	}
	if got := cfg.Bot.Budget.BotToolTimeout(); got != 45*time.Second {
		t.Errorf("BotToolTimeout 应为 45s，实际 %v", got)
	}
	if cfg.Bot.Budget.MaxOutputBytes != 4096 {
		t.Errorf("max_output_bytes 应为 4096（config.yaml），实际 %d", cfg.Bot.Budget.MaxOutputBytes)
	}
}

// TestLoadConfig_BotBudgetInvalidFallsBack 覆盖 fail-safe：非正值/非法值不得把护栏
// 关成「无上限」，未知 redaction_profile 不得放大可见字段。
func TestLoadConfig_BotBudgetInvalidFallsBack(t *testing.T) {
	t.Setenv("BOT_BUDGET_MAX_TOKENS", "not-a-number")

	cfg := newTestConfig(t, `bot:
  enabled: true
  redaction_profile: verbose
  budget:
    max_steps: -3
    max_tokens: 1234
    max_tool_calls: 0
    tool_timeout_seconds: -1
    max_output_bytes: 0
`)

	if cfg.Bot.RedactionProfile != "default" {
		t.Errorf("未知 redaction_profile 应回落 default，实际 %q", cfg.Bot.RedactionProfile)
	}
	if cfg.Bot.Budget.MaxSteps != 24 {
		t.Errorf("负数 max_steps 应回落默认 24，实际 %d", cfg.Bot.Budget.MaxSteps)
	}
	// 非法环境变量被忽略（不静默清零），保留 config.yaml 的 1234。
	if cfg.Bot.Budget.MaxTokens != 1234 {
		t.Errorf("非数字环境变量应被忽略并保留 yaml 值 1234，实际 %d", cfg.Bot.Budget.MaxTokens)
	}
	if cfg.Bot.Budget.MaxToolCalls != 12 {
		t.Errorf("0 值 max_tool_calls 应回落默认 12，实际 %d", cfg.Bot.Budget.MaxToolCalls)
	}
	if cfg.Bot.Budget.ToolTimeoutSeconds != 30 {
		t.Errorf("负超时应回落默认 30，实际 %d", cfg.Bot.Budget.ToolTimeoutSeconds)
	}
	if cfg.Bot.Budget.MaxOutputBytes != 65536 {
		t.Errorf("0 值 max_output_bytes 应回落默认 65536，实际 %d", cfg.Bot.Budget.MaxOutputBytes)
	}
}

// TestGetEnvIntWithDefault 锁定 BP8 环境变量解析语义（含 ITSM_ 前缀与非法值忽略）。
func TestGetEnvIntWithDefault(t *testing.T) {
	const key = "BOT_BUDGET_MAX_STEPS"

	for _, tc := range []struct {
		name     string
		bare     string
		prefixed string
		want     int
	}{
		{name: "缺失取默认", want: 24},
		{name: "裸键生效", bare: "42", want: 42},
		{name: "ITSM_ 前缀生效", prefixed: "36", want: 36},
		{name: "裸键优先于前缀", bare: "42", prefixed: "36", want: 42},
		{name: "零值被忽略", bare: "0", want: 24},
		{name: "负值被忽略", bare: "-5", want: 24},
		{name: "非数字被忽略", bare: "abc", want: 24},
		{name: "空白被忽略", bare: "   ", want: 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.bare)
			t.Setenv("ITSM_"+key, tc.prefixed)
			if got := getEnvIntWithDefault(key, 24); got != tc.want {
				t.Errorf("getEnvIntWithDefault = %d, want %d", got, tc.want)
			}
		})
	}
}
