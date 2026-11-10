package config

import "testing"

// TestLoadConfig_SMTPEnvContract 验证生产 compose 使用的 SMTP_* 环境变量
// 与 config.yaml 的映射一致，并覆盖 SMTP_FROM -> SMTP_FROM_EMAIL 的兼容别名。
func TestLoadConfig_SMTPEnvContract(t *testing.T) {
	t.Setenv("SMTP_ENABLED", "true")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", "mailer@example.com")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_FROM_EMAIL", "")
	t.Setenv("SMTP_FROM", "ops@example.com")
	t.Setenv("SMTP_FROM_NAME", "ITSM Ops")
	t.Setenv("SMTP_ENCRYPTION", "ssl")

	cfg := newTestConfig(t, `smtp:
  enabled: false
  host: placeholder
  port: 587
  username: ""
  password: ""
  from_email: noreply@example.com
  from_name: ITSM
  encryption: tls
`)

	if !cfg.SMTP.Enabled {
		t.Error("SMTP_ENABLED=true 应启用 SMTP")
	}
	if cfg.SMTP.Host != "smtp.example.com" {
		t.Errorf("SMTP_HOST 未生效，实际 %q", cfg.SMTP.Host)
	}
	if cfg.SMTP.Port != 465 {
		t.Errorf("SMTP_PORT 未生效，实际 %d", cfg.SMTP.Port)
	}
	if cfg.SMTP.Username != "mailer@example.com" {
		t.Errorf("SMTP_USERNAME 未生效，实际 %q", cfg.SMTP.Username)
	}
	if cfg.SMTP.Password != "secret" {
		t.Error("SMTP_PASSWORD 未生效")
	}
	if cfg.SMTP.FromEmail != "ops@example.com" {
		t.Errorf("SMTP_FROM 兼容别名未生效，实际 %q", cfg.SMTP.FromEmail)
	}
	if cfg.SMTP.FromName != "ITSM Ops" {
		t.Errorf("SMTP_FROM_NAME 未生效，实际 %q", cfg.SMTP.FromName)
	}
	if cfg.SMTP.Encryption != "ssl" {
		t.Errorf("SMTP_ENCRYPTION 未生效，实际 %q", cfg.SMTP.Encryption)
	}
}

func TestLoadConfig_SMTPFromEmailPrecedence(t *testing.T) {
	t.Setenv("SMTP_FROM_EMAIL", "canonical@example.com")
	t.Setenv("SMTP_FROM", "legacy@example.com")

	cfg := newTestConfig(t, `smtp:
  from_email: noreply@example.com
`)

	if cfg.SMTP.FromEmail != "canonical@example.com" {
		t.Errorf("SMTP_FROM_EMAIL 应优先于 SMTP_FROM，实际 %q", cfg.SMTP.FromEmail)
	}
}
