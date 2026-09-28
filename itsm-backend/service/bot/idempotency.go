// B0-05：写工具统一幂等键（只存 hash）。
//
// 作用域 = 租户 + 发起人 + 工具 + 目标 + 参数；同一幂等键重复提交应返回首次结果
// （G5；为 B1-05 的确认单幂等回放提供底座）。
//
// 安全约束：只落库 hash（`tool_invocations.idempotency_key_hash`），明文参数不进键、
// 不进日志；hash 不可逆，避免从幂等键反推业务参数。
package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// KeyVersion 是幂等键拼接格式版本：算法或拼接顺序变更必须升版本，
// 否则历史键会被误判为「不同请求」。测试对本常量与拼接顺序做冻结断言。
const KeyVersion = "v1"

// KeyInput 是幂等键的全部输入维度。
type KeyInput struct {
	TenantID   int
	UserID     int
	Tool       string
	TargetType string
	TargetID   string
	Args       map[string]interface{}
}

// BuildKey 生成幂等键 hash（hex(sha256)）。
//
// 拼接顺序（冻结；变更须升 KeyVersion 并同步测试）：
//
//	v1|tenant=<id>|user=<id>|tool=<name>|target=<type>:<id>|args=<canonical-json>
//
// canonical JSON 由 encoding/json 对 map 键排序产生（嵌套 map 同样排序；数组顺序保留语义）。
// 因此 {"a":1,"b":2} 与 {"b":2,"a":1} 得同一键，而任一维度或参数值变化必然改变键。
func BuildKey(in KeyInput) (string, error) {
	args := in.Args
	if args == nil {
		args = map[string]interface{}{}
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		// 参数不可序列化时不生成键（fail-closed：宁可不做幂等，也不生成歧义键）。
		return "", fmt.Errorf("幂等键：参数无法序列化: %w", err)
	}
	payload := fmt.Sprintf("%s|tenant=%d|user=%d|tool=%s|target=%s:%s|args=%s",
		KeyVersion, in.TenantID, in.UserID, in.Tool, in.TargetType, in.TargetID, canonical)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

// TargetFromArgs 从参数中提取幂等目标（ticket/ci/relationship/ticket_type）。
// 无目标时返回空串（键仍由其余维度区分）；数值统一按整型字符串化，避免 1 与 1.0 产生两键。
func TargetFromArgs(args map[string]interface{}) (targetType, targetID string) {
	candidates := []struct {
		key string
		typ string
	}{
		{"ticket_id", "ticket"},
		{"ci_id", "ci"},
		{"relationship_id", "ci_relationship"},
		{"ticket_type_id", "ticket_type"},
	}
	for _, c := range candidates {
		if v, ok := args[c.key].(float64); ok && int(v) > 0 {
			return c.typ, fmt.Sprintf("%d", int(v))
		}
		if v, ok := args[c.key].(int); ok && v > 0 {
			return c.typ, fmt.Sprintf("%d", v)
		}
	}
	return "", ""
}

// ErrDuplicateKey 表示幂等键已存在：调用方应回放既有记录（首次结果），不得重复执行。
var ErrDuplicateKey = errors.New("幂等键已存在：命中既有调用记录")

// IsUniqueViolation 判定「唯一索引冲突」（ent/SQLite/Postgres 驱动措辞统一收口），
// 用于把并发重复提交映射为幂等命中而非 500。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unique constraint",
		"constraint failed",
		"duplicate key",
		"constraint violation",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
