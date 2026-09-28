package admin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/middleware"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

const testKey = "unit-test-mcp-encryption-key"

func newService(t *testing.T, key string) *CredentialService {
	t.Helper()
	service, err := NewCredentialService(key)
	require.NoError(t, err)
	return service
}

func TestNewCredentialService_KeyValidation(t *testing.T) {
	_, err := NewCredentialService("short")
	require.Error(t, err)
	_, err = NewCredentialService("                ")
	require.Error(t, err)

	service, err := NewCredentialServiceWithEncryption(middleware.NewEncryptionService(testKey))
	require.NoError(t, err)
	require.NotNil(t, service)

	_, err = NewCredentialServiceWithEncryption(nil)
	require.Error(t, err)
}

func TestResolveEncryptionKey(t *testing.T) {
	t.Run("环境变量优先", func(t *testing.T) {
		t.Setenv(MCPEncryptionKeyEnv, "  explicit-mcp-key-1234  ")
		key, derived, err := ResolveEncryptionKey("jwt-secret", true)
		require.NoError(t, err)
		require.False(t, derived)
		require.Equal(t, "explicit-mcp-key-1234", key)
	})

	t.Run("生产缺密钥拒绝启动", func(t *testing.T) {
		t.Setenv(MCPEncryptionKeyEnv, "")
		_, _, err := ResolveEncryptionKey("jwt-secret", true)
		require.Error(t, err)
		require.Contains(t, err.Error(), MCPEncryptionKeyEnv)
	})

	t.Run("非生产派生回退", func(t *testing.T) {
		t.Setenv(MCPEncryptionKeyEnv, "")
		key, derived, err := ResolveEncryptionKey("jwt-secret", false)
		require.NoError(t, err)
		require.True(t, derived)
		require.Equal(t, "mcp-key-jwt-secret", key)
	})
}

func TestEncryptDecrypt_RoundTripAndNoPlaintext(t *testing.T) {
	service := newService(t, testKey)
	values := NewSecretValues(map[string]string{
		"Authorization": "Bearer super-secret-token-123",
		"X-Api-Key":     "api-key-abcdef",
	})

	ciphertext, err := service.Encrypt(values)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	require.NotContains(t, ciphertext, "super-secret-token-123")
	require.NotContains(t, ciphertext, "api-key-abcdef")
	require.NotContains(t, ciphertext, "Authorization", "键名同样不应出现在密文中（整体加密）")

	decrypted, err := service.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, 2, decrypted.Len())
	got, ok := decrypted.Get("Authorization")
	require.True(t, ok)
	require.Equal(t, "Bearer super-secret-token-123", got)

	// 空集合 → 空密文（避免制造"已配置"错觉）。
	empty, err := service.Encrypt(NewSecretValues(nil))
	require.NoError(t, err)
	require.Empty(t, empty)
	back, err := service.Decrypt("")
	require.NoError(t, err)
	require.Equal(t, 0, back.Len())
}

func TestDecrypt_WrongKeyAndTamperFails(t *testing.T) {
	serviceA := newService(t, testKey)
	serviceB := newService(t, "another-unit-test-key-4321")

	ciphertext, err := serviceA.Encrypt(NewSecretValues(map[string]string{"Authorization": "Bearer x"}))
	require.NoError(t, err)

	_, err = serviceB.Decrypt(ciphertext)
	require.Error(t, err, "换主密钥后旧密文必须失效")

	tampered := ciphertext[:len(ciphertext)-4] + "AAAA"
	_, err = serviceA.Decrypt(tampered)
	require.Error(t, err, "GCM 完整性校验应拒绝篡改密文")
}

func TestSecretValues_NoLeakInFormatting(t *testing.T) {
	values := NewSecretValues(map[string]string{"Authorization": "Bearer leak-me-if-you-can"})
	formatted := []string{
		fmt.Sprintf("%v", values),
		values.String(),
		fmt.Sprintf("%#v", values),
	}
	for _, text := range formatted {
		require.NotContains(t, text, "leak-me-if-you-can")
		require.Contains(t, text, "redacted")
	}
}

func TestMaskSecret(t *testing.T) {
	require.Equal(t, "", MaskSecret(""))
	require.Equal(t, "****", MaskSecret("short"))
	require.Equal(t, "****", MaskSecret("12345678901"))
	require.Equal(t, "Bear****en", MaskSecret("Bearer token"))
}

func TestMaskedProjection(t *testing.T) {
	service := newService(t, testKey)
	ciphertext, err := service.Encrypt(NewSecretValues(map[string]string{
		"Authorization": "Bearer super-secret-token-123",
		"X-Short":       "tiny",
	}))
	require.NoError(t, err)

	masked, err := service.Masked(ciphertext)
	require.NoError(t, err)
	require.Len(t, masked, 2)
	require.Equal(t, "Bear****23", masked["Authorization"])
	require.Equal(t, "****", masked["X-Short"])
	require.NotContains(t, masked["Authorization"], "secret")
}

func TestApplyPatch_EmptyMeansUnchanged(t *testing.T) {
	existing := NewSecretValues(map[string]string{
		"Authorization": "Bearer old-token-0001",
		"X-Env":         "prod",
	})

	merged := existing.ApplyPatch(map[string]string{
		"Authorization": "   ", // 空白 → 不修改
		"X-Trace":       "on",  // 新增
		"X-Env":         "",    // 空 → 保留
		"":              "ignored",
	})

	require.Equal(t, 3, merged.Len())
	auth, _ := merged.Get("Authorization")
	require.Equal(t, "Bearer old-token-0001", auth)
	env, _ := merged.Get("X-Env")
	require.Equal(t, "prod", env)
	trace, _ := merged.Get("X-Trace")
	require.Equal(t, "on", trace)
}

func TestApplyPatch_RotateInvalidatesOldValue(t *testing.T) {
	service := newService(t, testKey)
	oldCiphertext, err := service.Encrypt(NewSecretValues(map[string]string{"Authorization": "Bearer old-token"}))
	require.NoError(t, err)

	// 空 patch → 值不变（"不修改"语义），密文重新加密不影响可读性。
	unchanged, err := service.ApplyPatch(oldCiphertext, map[string]string{"Authorization": ""})
	require.NoError(t, err)
	values, err := service.Decrypt(unchanged)
	require.NoError(t, err)
	got, _ := values.Get("Authorization")
	require.Equal(t, "Bearer old-token", got)

	// 轮换：新值覆盖；旧值不再可读；密文因随机 nonce 必然变化。
	rotated, err := service.ApplyPatch(oldCiphertext, map[string]string{"Authorization": "Bearer new-token"})
	require.NoError(t, err)
	require.NotEqual(t, oldCiphertext, rotated)

	rotatedValues, err := service.Decrypt(rotated)
	require.NoError(t, err)
	newToken, _ := rotatedValues.Get("Authorization")
	require.Equal(t, "Bearer new-token", newToken)
	require.NotContains(t, newToken, "old-token")

	// 旧密文本身仍可解（历史审计），但存储路径已不再引用旧值。
	historyValues, err := service.Decrypt(oldCiphertext)
	require.NoError(t, err)
	historyToken, _ := historyValues.Get("Authorization")
	require.Equal(t, "Bearer old-token", historyToken)
}

// TestEncryptedAtRestInDatabase 断言"DB 中无明文"（M0-06 测试与证据要求）。
func TestEncryptedAtRestInDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1",
		strings.NewReplacer("/", "_", " ", "_", "\\", "_").Replace(t.Name()))

	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()

	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	defer db.Close()

	service := newService(t, testKey)
	plainToken := "Bearer db-at-rest-secret-98765"
	credentialCipher, err := service.Encrypt(NewSecretValues(map[string]string{"Authorization": plainToken}))
	require.NoError(t, err)
	headerCipher, err := service.Encrypt(NewSecretValues(map[string]string{"X-Api-Key": "header-secret-123456"}))
	require.NoError(t, err)

	_, err = client.MCPServer.Create().
		SetTenantID(1).
		SetName("github").
		SetCredentialType("static_header").
		SetCredentialEncrypted(credentialCipher).
		SetHeadersEncrypted(headerCipher).
		Save(ctx)
	require.NoError(t, err)

	var storedCredential, storedHeaders string
	err = db.QueryRow(`SELECT credential_encrypted, headers_encrypted FROM mcp_servers WHERE name = 'github'`).
		Scan(&storedCredential, &storedHeaders)
	require.NoError(t, err)

	require.NotEmpty(t, storedCredential)
	require.NotContains(t, storedCredential, "db-at-rest-secret-98765")
	require.NotContains(t, storedCredential, plainToken)
	require.NotContains(t, storedHeaders, "header-secret-123456")

	roundTrip, err := service.Decrypt(storedCredential)
	require.NoError(t, err)
	value, ok := roundTrip.Get("Authorization")
	require.True(t, ok)
	require.Equal(t, plainToken, value)
}
