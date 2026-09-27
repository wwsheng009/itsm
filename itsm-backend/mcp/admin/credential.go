package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"itsm-backend/middleware"
)

// MCP 凭据管理（M0-06）：加密落库、只写不读回、掩码投影、轮换。
//
// 复用范式（与 connector / LLM provider 一致）：
//   - 采用 middleware.EncryptionService（AES-GCM + base64；密钥经 SHA-256 派生）；
//   - 主密钥来自环境变量，生产缺失必须拒绝启动（bootstrap 在 M0-08 接线）；
//   - 读接口只返回掩码；明文仅在连接装配层（M0-07/M0-08）内部解密使用。

// MCPEncryptionKeyEnv 是 MCP 凭据加密主密钥的环境变量名。
const MCPEncryptionKeyEnv = "MCP_ENCRYPTION_KEY"

// MinEncryptionKeyLength 与 connector 配置存储保持一致（≥16 字符）。
const MinEncryptionKeyLength = 16

// mcpDerivedKeyPrefix 非生产未配置主密钥时的派生前缀（与 connector/LLM 同策略）。
const mcpDerivedKeyPrefix = "mcp-key-"

// SecretValues 是敏感键值集合（如 Authorization 请求头、OAuth access_token）。
//
// 安全约束：不导出内部 map、不提供序列化出口，并实现 String/GoString
// 以阻断日志 / 审计 / 事件中的误打印明文。
type SecretValues struct {
	values map[string]string
}

// NewSecretValues 创建敏感值集合（复制入参，避免外部继续持有引用）。
func NewSecretValues(values map[string]string) SecretValues {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return SecretValues{values: copied}
}

// Get 读取单个值（仅供连接装配层使用）。
func (s SecretValues) Get(key string) (string, bool) {
	value, ok := s.values[key]
	return value, ok
}

// Len 返回键数量。
func (s SecretValues) Len() int { return len(s.values) }

// Keys 返回排序后的键名（键名本身不是敏感信息）。
func (s SecretValues) Keys() []string {
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// String 阻断 %v/%s 打印（fmt 优先使用 Stringer）。
func (s SecretValues) String() string { return "SecretValues(<redacted>)" }

// GoString 阻断 %#v 打印。
func (s SecretValues) GoString() string { return "admin.SecretValues{<redacted>}" }

// Masked 返回掩码投影（管理 API 唯一允许的读形态）。
func (s SecretValues) Masked() map[string]string {
	masked := make(map[string]string, len(s.values))
	for key, value := range s.values {
		masked[key] = MaskSecret(value)
	}
	return masked
}

// ApplyPatch 返回应用补丁后的新集合：
//   - patch 中值为空字符串（或纯空白）表示「不修改」原值（与 connector 的更新语义一致）；
//   - patch 中的非空值覆盖原值；原集合中未出现的键保持不变；
//   - 一期不支持通过本方法删除键（删除须显式管理操作，见 M0-08）。
func (s SecretValues) ApplyPatch(patch map[string]string) SecretValues {
	merged := make(map[string]string, len(s.values)+len(patch))
	for key, value := range s.values {
		merged[key] = value
	}
	for key, value := range patch {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(value) == "" {
			continue
		}
		merged[key] = value
	}
	return NewSecretValues(merged)
}

// CredentialService 管理 MCP 服务器敏感配置的加密、掩码与轮换。
type CredentialService struct {
	encryption *middleware.EncryptionService
}

// NewCredentialService 以主密钥构造服务（长度不足直接拒绝）。
func NewCredentialService(secret string) (*CredentialService, error) {
	if len(strings.TrimSpace(secret)) < MinEncryptionKeyLength {
		return nil, fmt.Errorf("MCP 凭据加密密钥至少需要 %d 个字符", MinEncryptionKeyLength)
	}
	return &CredentialService{encryption: middleware.NewEncryptionService(secret)}, nil
}

// NewCredentialServiceWithEncryption 注入既有加密服务（装配复用 / 测试）。
func NewCredentialServiceWithEncryption(encryption *middleware.EncryptionService) (*CredentialService, error) {
	if encryption == nil {
		return nil, errors.New("MCP 凭据服务需要加密实例")
	}
	return &CredentialService{encryption: encryption}, nil
}

// Encrypt 序列化并加密敏感值集合；空集合返回空串（与 connector 语义一致）。
func (s *CredentialService) Encrypt(values SecretValues) (string, error) {
	if values.Len() == 0 {
		return "", nil
	}
	payload, err := json.Marshal(values.values)
	if err != nil {
		return "", fmt.Errorf("编码 MCP 凭据失败: %w", err)
	}
	ciphertext, err := s.encryption.Encrypt(string(payload))
	if err != nil {
		return "", fmt.Errorf("加密 MCP 凭据失败: %w", err)
	}
	return ciphertext, nil
}

// Decrypt 解密落库密文。**仅供连接装配层（M0-07/M0-08）调用**；
// 管理 API 禁止把明文返回给前端（只允许 Masked）。
func (s *CredentialService) Decrypt(ciphertext string) (SecretValues, error) {
	plain, err := s.encryption.Decrypt(ciphertext)
	if err != nil {
		return SecretValues{}, fmt.Errorf("解密 MCP 凭据失败: %w", err)
	}
	if strings.TrimSpace(plain) == "" {
		return NewSecretValues(nil), nil
	}
	values := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &values); err != nil {
		return SecretValues{}, fmt.Errorf("解析 MCP 凭据失败: %w", err)
	}
	return NewSecretValues(values), nil
}

// Masked 返回落库密文的掩码投影。
func (s *CredentialService) Masked(ciphertext string) (map[string]string, error) {
	values, err := s.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	return values.Masked(), nil
}

// ApplyPatch 在既有密文上应用更新并重新加密（轮换入口）。
// patch 值为空表示保留原值；重新加密使用随机 nonce，密文必然变化。
func (s *CredentialService) ApplyPatch(ciphertext string, patch map[string]string) (string, error) {
	existing, err := s.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	merged := existing.ApplyPatch(patch)
	if merged.Len() == 0 {
		return "", nil
	}
	return s.Encrypt(merged)
}

// ResolveEncryptionKey 解析 MCP 凭据主密钥：
//   - 环境变量 MCP_ENCRYPTION_KEY 优先（trim 后非空）；
//   - 缺失时：production=true 返回错误（调用方启动期 Fatal，禁止弱回退）；
//     非生产回退派生密钥（derived=true，调用方应打 Warn）。
func ResolveEncryptionKey(jwtSecret string, production bool) (key string, derived bool, err error) {
	if explicit := strings.TrimSpace(os.Getenv(MCPEncryptionKeyEnv)); explicit != "" {
		return explicit, false, nil
	}
	if production {
		return "", false, fmt.Errorf("%s 在生产模式必须显式配置", MCPEncryptionKeyEnv)
	}
	return mcpDerivedKeyPrefix + jwtSecret, true, nil
}

// MaskSecret 生成掩码：长度 ≥12 时保留前 4 后 2，否则固定 "****"。
// 掩码仅用于展示，不可逆。
func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) >= 12 {
		return value[:4] + "****" + value[len(value)-2:]
	}
	return "****"
}
