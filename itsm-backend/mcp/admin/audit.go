package admin

import (
	"context"
	"sync"
	"time"
)

// AuditEntry 是 MCP 管理操作审计条目（§5.5：actor/tenant/action/object/before-after/result/ip/ts）。
//
// 脱敏约束：Before/After 只允许放已脱敏快照（serverAuditSnapshot / maskSecretsMap 产出）；
// 凭据明文、密文、完整 token 一律不得进入审计。
type AuditEntry struct {
	TenantID   int
	ActorID    int
	Action     string
	ObjectType string
	ObjectID   string
	Before     map[string]any
	After      map[string]any
	Result     string // success|failure
	ErrorCode  string
	IP         string
	At         time.Time
}

// AuditSink 是审计出口。实现必须快速返回；失败不得阻断业务（调用方仅记录错误）。
type AuditSink interface {
	RecordMCPAudit(ctx context.Context, entry AuditEntry) error
}

// DiscardAudit 返回丢弃全部审计的 sink（默认；M0-11 接线到实际审计设施）。
func DiscardAudit() AuditSink { return discardAuditSink{} }

type discardAuditSink struct{}

func (discardAuditSink) RecordMCPAudit(context.Context, AuditEntry) error { return nil }

// MemoryAuditSink 记录审计条目（测试 / M0-11 接线前的本地观测）。
type MemoryAuditSink struct {
	mu      sync.Mutex
	entries []AuditEntry
}

// NewMemoryAuditSink 创建内存审计 sink。
func NewMemoryAuditSink() *MemoryAuditSink { return &MemoryAuditSink{} }

// RecordMCPAudit 实现 AuditSink。
func (s *MemoryAuditSink) RecordMCPAudit(_ context.Context, entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

// Entries 返回审计条目快照。
func (s *MemoryAuditSink) Entries() []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AuditEntry(nil), s.entries...)
}

// Last 返回最后一条审计。
func (s *MemoryAuditSink) Last() (AuditEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return AuditEntry{}, false
	}
	return s.entries[len(s.entries)-1], true
}

// Find 返回首个满足条件（action 相等）的审计条目。
func (s *MemoryAuditSink) Find(action string) (AuditEntry, bool) {
	for _, entry := range s.Entries() {
		if entry.Action == action {
			return entry, true
		}
	}
	return AuditEntry{}, false
}

// maskSecretsMap 生成敏感键值的安全展示：键名保留、值统一掩码。
func maskSecretsMap(values map[string]string) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	masked := make(map[string]any, len(values))
	for key, value := range values {
		masked[key] = maskSecretValue(value)
	}
	return masked
}

// toAnyMap 把**已掩码**的字符串映射转为审计可序列化形态（值直接透传，不重复掩码）。
func toAnyMap(values map[string]string) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	converted := make(map[string]any, len(values))
	for key, value := range values {
		converted[key] = value
	}
	return converted
}

// maskSecretValue 与 MaskSecret 同规则；空值显示为 ""（区别于"有值但短"）。
func maskSecretValue(value string) string {
	if value == "" {
		return ""
	}
	return MaskSecret(value)
}
