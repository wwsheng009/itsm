package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// ConfigSecretMask 敏感配置的 API 掩码占位符。
//
// 语义约定（方案 SCD4）：
//   - 读接口（列表/详情/更新回显）对敏感键返回该掩码，不回显明文/密文；
//   - 写接口收到该掩码时表示"保持原值不变"，不会把掩码写入数据库。
const ConfigSecretMask = "******"

// configSecretCipherPrefix 密文格式版本前缀：v1:<keyId>:<nonce>:<ciphertext>（nonce/密文为 base64 标准编码）。
const configSecretCipherPrefix = "v1:"

// ConfigSecretBox 系统配置敏感值加密盒（AES-256-GCM）。
//
// 密钥来源：环境变量 CONFIG_SECRET_KEY（64 位 hex 或 base64，解码后必须为 32 字节）。
//   - 生产环境（SERVER_ENV/ENVIRONMENT/APP_ENV = production/prod）缺失或非法密钥 → 禁用加密：
//     写入敏感值直接报错（fail-closed），绝不落明文；
//   - 开发/演示环境缺失密钥 → 由 JWT secret 派生（SHA-256）并输出 WARN，保证本地可跑；
//     该派生密钥仅用于本地/演示，生产必须显式配置 CONFIG_SECRET_KEY。
//
// 兼容：历史明文值（无 v1: 前缀）读取时原样返回；回填迁移由运维脚本完成（见方案 §5 SCD4）。
type ConfigSecretBox struct {
	aead    cipher.AEAD
	keyID   string
	enabled bool
	derived bool
	logger  *zap.SugaredLogger
}

// NewConfigSecretBoxFromEnv 按环境变量构造加密盒；任何异常都不阻断启动（由写路径 fail-closed 兜底）。
func NewConfigSecretBoxFromEnv(logger *zap.SugaredLogger, jwtSecret string) *ConfigSecretBox {
	box := &ConfigSecretBox{logger: logger}

	raw := strings.TrimSpace(os.Getenv("CONFIG_SECRET_KEY"))
	if raw != "" {
		key, ok := decodeConfigSecretKey(raw)
		if ok {
			if aead, err := newConfigAEAD(key); err == nil {
				box.aead = aead
				box.keyID = configSecretKeyID(key)
				box.enabled = true
				logger.Infow("config secret box enabled", "source", "CONFIG_SECRET_KEY", "key_id", box.keyID)
				return box
			} else {
				logger.Errorw("CONFIG_SECRET_KEY invalid, config secret box disabled", "error", err)
				return box
			}
		}
		logger.Errorw("CONFIG_SECRET_KEY format invalid (expect 64-char hex or base64 of 32 bytes), config secret box disabled")
		return box
	}

	// 未配置密钥：生产 fail-closed；开发/演示派生兜底。
	if isProductionEnv() {
		logger.Errorw("CONFIG_SECRET_KEY missing in production; sensitive config writes will be rejected (fail-closed)")
		return box
	}
	if jwtSecret == "" {
		logger.Warnw("CONFIG_SECRET_KEY missing and JWT secret empty; config secret box disabled")
		return box
	}
	derivedKey := sha256.Sum256([]byte("itsm-config-secret|" + jwtSecret))
	if aead, err := newConfigAEAD(derivedKey[:]); err == nil {
		box.aead = aead
		box.keyID = configSecretKeyID(derivedKey[:])
		box.enabled = true
		box.derived = true
		logger.Warnw("CONFIG_SECRET_KEY missing; using JWT-derived key for local/demo only (NOT for production)",
			"key_id", box.keyID)
	}
	return box
}

// Enabled 报告加密盒是否可用（生产缺失密钥时为 false，写敏感值必须报错）。
func (b *ConfigSecretBox) Enabled() bool {
	return b != nil && b.enabled
}

// Derived 报告当前密钥是否为开发/演示派生密钥。
func (b *ConfigSecretBox) Derived() bool {
	return b != nil && b.derived
}

// KeyID 返回当前密钥标识（用于密文头，不含密钥本身）。
func (b *ConfigSecretBox) KeyID() string {
	if b == nil {
		return ""
	}
	return b.keyID
}

// Encrypt 加密敏感值；空串原样返回（无需加密）。
func (b *ConfigSecretBox) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if !b.Enabled() {
		return "", fmt.Errorf("配置加密密钥未配置（CONFIG_SECRET_KEY），拒绝保存敏感配置明文")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成加密随机数失败: %w", err)
	}
	ciphertext := b.aead.Seal(nil, nonce, []byte(plain), nil)
	return fmt.Sprintf("%s%s:%s:%s",
		configSecretCipherPrefix,
		b.keyID,
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(ciphertext),
	), nil
}

// Decrypt 解密敏感值；历史明文（无 v1: 前缀）原样返回以保持兼容。
func (b *ConfigSecretBox) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !IsEncryptedSecret(stored) {
		// 历史明文值：兼容读取（迁移回填前不阻断业务）。
		return stored, nil
	}
	if !b.Enabled() {
		return "", fmt.Errorf("配置密文存在但加密密钥不可用（CONFIG_SECRET_KEY）")
	}
	parts := strings.SplitN(stored, ":", 4)
	if len(parts) != 4 {
		return "", fmt.Errorf("配置密文格式非法")
	}
	if parts[1] != b.keyID {
		return "", fmt.Errorf("配置密文密钥不匹配: cipher=%s current=%s", parts[1], b.keyID)
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("配置密文 nonce 解码失败: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("配置密文内容解码失败: %w", err)
	}
	plain, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("配置密文解密失败（密钥不匹配或数据损坏）")
	}
	return string(plain), nil
}

// IsEncryptedSecret 判断存量值是否为本系统加密格式。
func IsEncryptedSecret(value string) bool {
	if !strings.HasPrefix(value, configSecretCipherPrefix) {
		return false
	}
	return len(strings.SplitN(value, ":", 4)) == 4
}

// IsSensitiveConfigKey 判断配置键是否为敏感凭据（写加密 / 读掩码 / 审计不含值）。
//
// 规则：显式清单 + 后缀约定（*password、*_secret、*.secret、*apikey、*api_key）。
// 注意 passwordMinLength 等策略键不匹配后缀规则，不会被误判。
func IsSensitiveConfigKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	switch k {
	case "smtpPassword":
		return true
	}
	lower := strings.ToLower(k)
	suffixes := []string{"password", "_secret", ".secret", "apikey", "_apikey", "api_key", "accesskey", "access_key"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// MaskConfigValue 读路径掩码：敏感键且已有值时返回掩码，否则原样返回。
func MaskConfigValue(key, value string) string {
	if value != "" && IsSensitiveConfigKey(key) {
		return ConfigSecretMask
	}
	return value
}

// IsMaskedSecretValue 判断请求值是否为"保持不变"掩码。
func IsMaskedSecretValue(value string) bool {
	return strings.TrimSpace(value) == ConfigSecretMask
}

func newConfigAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func configSecretKeyID(key []byte) string {
	sum := sha256.Sum256(append([]byte("keyid|"), key...))
	return hex.EncodeToString(sum[:4])
}

// decodeConfigSecretKey 解析 CONFIG_SECRET_KEY：64 位 hex 或 base64（标准/URL），解码后必须 32 字节。
func decodeConfigSecretKey(raw string) ([]byte, bool) {
	if len(raw) == 64 {
		if key, err := hex.DecodeString(raw); err == nil && len(key) == 32 {
			return key, true
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == 32 {
			return key, true
		}
	}
	return nil, false
}

// isProductionEnv 读取部署环境标识（与部署文档约定一致：SERVER_ENV/ENVIRONMENT/APP_ENV）。
func isProductionEnv() bool {
	for _, name := range []string{"SERVER_ENV", "ENVIRONMENT", "APP_ENV"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "production", "prod":
			return true
		}
	}
	return false
}
