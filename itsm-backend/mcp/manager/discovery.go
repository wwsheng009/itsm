package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"itsm-backend/mcp/registry"
)

// QuarantineReasonSchemaChanged 是 schema 变更导致的隔离原因（M0-07 要点 3）。
const QuarantineReasonSchemaChanged = "schema_changed"

// SchemaHash 计算 inputSchema 的稳定哈希：先标准化（Unmarshal→Marshal，键序确定），
// 解析失败时退回原始字节哈希（保证任何输入都有确定结果）。
func SchemaHash(inputSchema json.RawMessage) string {
	canonical := inputSchema
	if len(inputSchema) > 0 {
		var decoded any
		if err := json.Unmarshal(inputSchema, &decoded); err == nil {
			if encoded, err := json.Marshal(decoded); err == nil {
				canonical = encoded
			}
		}
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// DiscoveredTool 是一次发现得到的工具（尚未落库/治理）。
type DiscoveredTool struct {
	RawName      string
	CallableName string
	Description  string
	InputSchema  json.RawMessage
	SchemaHash   string
}

// ToolRecord 是工具缓存记录（字段口径对齐 ent `mcp_server_tool`）。
type ToolRecord struct {
	ServerID         int
	TenantID         int
	RawName          string
	CallableName     string
	Description      string
	InputSchema      json.RawMessage
	SchemaHash       string
	ReadOnly         bool
	Risk             string
	Category         string
	Enabled          bool
	Healthy          bool
	Quarantined      bool
	QuarantineReason string
	LastError        string
	DiscoveredAt     time.Time
	UpdatedAt        time.Time
}

// ToolDelta 描述一次发现的差分（元素为原始工具名）。
type ToolDelta struct {
	Added       []string
	Updated     []string
	Removed     []string
	Quarantined []string
	Unchanged   int
}

// Changed 返回发生变化的工具数。
func (d ToolDelta) Changed() int {
	return len(d.Added) + len(d.Updated) + len(d.Removed) + len(d.Quarantined)
}

// DiscoveryResult 是发现结果（新缓存内容 + 差分）。
type DiscoveryResult struct {
	Server  string
	Tools   []DiscoveredTool
	Records []ToolRecord
	Delta   ToolDelta
}

// PlanDiscovery 计算一次发现的结果：
//   - 新工具：默认 Enabled=false、ReadOnly=false、Risk=high（D7 默认拒绝，待治理）；
//   - schema_hash 变化：默认隔离待复核（保留已有的治理位，避免丢失管理员标注）；
//   - schema 未变：保留治理位与隔离状态（复核解除由 M0-08 处理）；
//   - 消失的工具：不进入新缓存（调用方按 Removed 标记下线）。
func PlanDiscovery(serverID, tenantID int, serverName string, previous []ToolRecord, discovered []DiscoveredTool, now time.Time) DiscoveryResult {
	previousByRaw := make(map[string]ToolRecord, len(previous))
	for _, record := range previous {
		previousByRaw[record.RawName] = record
	}

	result := DiscoveryResult{Server: serverName}
	seen := make(map[string]struct{}, len(discovered))

	for _, tool := range discovered {
		tool.RawName = strings.TrimSpace(tool.RawName)
		if tool.RawName == "" {
			continue
		}
		tool.CallableName = registry.CanonicalToolName(serverName, tool.RawName)
		tool.SchemaHash = SchemaHash(tool.InputSchema)
		result.Tools = append(result.Tools, tool)
		seen[tool.RawName] = struct{}{}

		record := ToolRecord{
			ServerID:     serverID,
			TenantID:     tenantID,
			RawName:      tool.RawName,
			CallableName: tool.CallableName,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			SchemaHash:   tool.SchemaHash,
			Risk:         "high",
			Healthy:      true,
			DiscoveredAt: now,
			UpdatedAt:    now,
		}

		prev, exists := previousByRaw[tool.RawName]
		switch {
		case !exists:
			result.Delta.Added = append(result.Delta.Added, tool.RawName)

		case prev.SchemaHash != "" && prev.SchemaHash != tool.SchemaHash:
			record.Enabled = prev.Enabled
			record.ReadOnly = prev.ReadOnly
			record.Risk = orDefault(prev.Risk, "high")
			record.Category = prev.Category
			record.Quarantined = true
			record.QuarantineReason = QuarantineReasonSchemaChanged
			record.DiscoveredAt = prev.DiscoveredAt
			result.Delta.Quarantined = append(result.Delta.Quarantined, tool.RawName)

		default:
			record.Enabled = prev.Enabled
			record.ReadOnly = prev.ReadOnly
			record.Risk = orDefault(prev.Risk, "high")
			record.Category = prev.Category
			record.Quarantined = prev.Quarantined
			record.QuarantineReason = prev.QuarantineReason
			record.DiscoveredAt = prev.DiscoveredAt
			if prev.Description != tool.Description {
				result.Delta.Updated = append(result.Delta.Updated, tool.RawName)
			} else {
				result.Delta.Unchanged++
			}
		}
		result.Records = append(result.Records, record)
	}

	for _, record := range previous {
		if _, ok := seen[record.RawName]; !ok {
			result.Delta.Removed = append(result.Delta.Removed, record.RawName)
		}
	}

	sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].RawName < result.Tools[j].RawName })
	sort.Slice(result.Records, func(i, j int) bool { return result.Records[i].RawName < result.Records[j].RawName })
	sort.Strings(result.Delta.Added)
	sort.Strings(result.Delta.Updated)
	sort.Strings(result.Delta.Removed)
	sort.Strings(result.Delta.Quarantined)
	return result
}

// ToolCache 保存各服务器最近一次发现结果（M0-08 用 DB 实现替换；manager 只读写缓存）。
type ToolCache interface {
	List(serverID int) []ToolRecord
	Replace(serverID int, records []ToolRecord)
}

// MemoryToolCache 是线程安全的内存实现（默认）。
type MemoryToolCache struct {
	mu       sync.RWMutex
	byServer map[int][]ToolRecord
}

// NewMemoryToolCache 创建内存缓存。
func NewMemoryToolCache() *MemoryToolCache {
	return &MemoryToolCache{byServer: map[int][]ToolRecord{}}
}

// List 返回服务器最近一次发现（拷贝）。
func (c *MemoryToolCache) List(serverID int) []ToolRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]ToolRecord(nil), c.byServer[serverID]...)
}

// Replace 覆盖服务器最近一次发现（拷贝入参）。
func (c *MemoryToolCache) Replace(serverID int, records []ToolRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byServer[serverID] = append([]ToolRecord(nil), records...)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
