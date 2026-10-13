package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestConfigSecretBox_RoundTrip(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("ab", 32)) // 64 位 hex
	t.Setenv("SERVER_ENV", "development")
	logger := zaptest.NewLogger(t).Sugar()

	box := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	require.True(t, box.Enabled())
	require.False(t, box.Derived())
	require.NotEmpty(t, box.KeyID())

	encrypted, err := box.Encrypt("s3cret-p@ss")
	require.NoError(t, err)
	require.True(t, IsEncryptedSecret(encrypted))
	require.NotContains(t, encrypted, "s3cret-p@ss")

	decrypted, err := box.Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "s3cret-p@ss", decrypted)

	// 幂等：已加密值再次加密保持不变（由服务层保证跳过，这里验证识别函数）。
	require.True(t, IsEncryptedSecret(encrypted))

	// 空值不加密。
	empty, err := box.Encrypt("")
	require.NoError(t, err)
	require.Equal(t, "", empty)

	// 历史明文兼容读取。
	legacy, err := box.Decrypt("legacy-plain-value")
	require.NoError(t, err)
	require.Equal(t, "legacy-plain-value", legacy)
}

func TestConfigSecretBox_DerivedKeyInDevelopment(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", "")
	t.Setenv("SERVER_ENV", "development")
	logger := zaptest.NewLogger(t).Sugar()

	box := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	require.True(t, box.Enabled())
	require.True(t, box.Derived())

	encrypted, err := box.Encrypt("dev-secret")
	require.NoError(t, err)
	decrypted, err := box.Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "dev-secret", decrypted)
}

func TestConfigSecretBox_FailClosedInProduction(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", "")
	t.Setenv("SERVER_ENV", "production")
	logger := zaptest.NewLogger(t).Sugar()

	box := NewConfigSecretBoxFromEnv(logger, "jwt-secret")
	require.False(t, box.Enabled())

	_, err := box.Encrypt("prod-secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "CONFIG_SECRET_KEY")
}

func TestConfigSecretBox_KeyMismatch(t *testing.T) {
	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("11", 32))
	t.Setenv("SERVER_ENV", "development")
	logger := zaptest.NewLogger(t).Sugar()
	boxA := NewConfigSecretBoxFromEnv(logger, "jwt")
	encrypted, err := boxA.Encrypt("value")
	require.NoError(t, err)

	t.Setenv("CONFIG_SECRET_KEY", strings.Repeat("22", 32))
	boxB := NewConfigSecretBoxFromEnv(logger, "jwt")
	_, err = boxB.Decrypt(encrypted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "密钥不匹配")
}

func TestIsSensitiveConfigKey(t *testing.T) {
	sensitive := []string{"smtpPassword", "ldapBindPassword", "connector.secret", "webhook_secret", "llmApiKey", "llm_api_key", "accessKey"}
	for _, key := range sensitive {
		require.True(t, IsSensitiveConfigKey(key), "expect sensitive: %s", key)
	}
	notSensitive := []string{"passwordMinLength", "passwordRequireUppercase", "smtpHost", "emailFrom", "sessionTimeout", ""}
	for _, key := range notSensitive {
		require.False(t, IsSensitiveConfigKey(key), "expect NOT sensitive: %s", key)
	}
}

func TestMaskConfigValue(t *testing.T) {
	require.Equal(t, ConfigSecretMask, MaskConfigValue("smtpPassword", "v1:abc:def:ghi"))
	require.Equal(t, ConfigSecretMask, MaskConfigValue("smtpPassword", "plaintext"))
	require.Equal(t, "", MaskConfigValue("smtpPassword", ""))
	require.Equal(t, "smtp.example.com", MaskConfigValue("smtpHost", "smtp.example.com"))
	require.True(t, IsMaskedSecretValue(ConfigSecretMask))
	require.False(t, IsMaskedSecretValue("real-secret"))
}
