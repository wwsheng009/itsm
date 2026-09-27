package registry

import (
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// MaxCanonicalToolNameLength 是模型/策略/审计可见名称的最大长度（provider 侧约束）。
const MaxCanonicalToolNameLength = 64

const (
	canonicalPrefix    = "mcp__"
	canonicalSeparator = "__"
)

// ServerNamePattern 定义 MCP 服务器稳定标识的合法格式（M0-08 保存时校验；本包做兜底校验）。
var ServerNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// CanonicalToolName 返回 MCP 工具的 canonical 投影名 `mcp__<server>__<tool>`。
//
// 规则（M0-02，冻结，逐条由投影测试锁定）：
//  1. 非法字符（`[^a-zA-Z0-9_-]`）替换为 `_`；只要发生替换（含首尾修剪）即追加 FNV-1a 短哈希，
//     避免 `a.b` 与 `a_b` 规整后碰撞；
//  2. 片段为空时回退为 `unnamed`；
//  3. 结果超过 64 字符时截断并追加 identity hash（SHA-256 前 6 字节），保证确定性与唯一性。
//
// 该函数是“名字 → 服务器 → 工具”确定性的唯一入口；列表出口与执行解析必须共用它。
func CanonicalToolName(server, rawName string) string {
	canonical := canonicalPrefix + portableNamePart(server) + canonicalSeparator + portableNamePart(rawName)
	if len(canonical) <= MaxCanonicalToolNameLength {
		return canonical
	}
	suffix := "_" + shortIdentityHash(server+"\x00"+rawName)
	return canonical[:MaxCanonicalToolNameLength-len(suffix)] + suffix
}

// portableNamePart 把任意字符串投影为 provider 安全片段（与参考实现逐字对齐）。
func portableNamePart(value string) string {
	raw := strings.TrimSpace(value)
	portable := strings.Trim(invalidNameChars.ReplaceAllString(raw, "_"), "_")
	if portable == "" {
		portable = "unnamed"
	}
	if portable != raw {
		portable += "_" + shortNameHash(raw)
	}
	return portable
}

// shortNameHash 是 FNV-1a 32 位哈希（8 位十六进制取前 6 位），用于非法字符替换后的防碰撞后缀。
func shortNameHash(value string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return fmt.Sprintf("%08x", hash.Sum32())[:6]
}

// shortIdentityHash 是 SHA-256 前 6 字节（12 位十六进制），用于超长截断的 identity 后缀。
func shortIdentityHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}
