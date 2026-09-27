package manager

import (
	"sync"
	"time"
)

// 生命周期事件（M0-07 要点 5；由 M0-11 接入 ITSM 既有审计/事件设施）。
//
// 约束：Event.Detail 必须已完成脱敏（不得含凭据/明文 token/内网结构）。
type EventType string

const (
	// EventServerConnected：连接建立且握手成功（含协议版本协商结果）。
	EventServerConnected EventType = "mcp.server.connected"
	// EventServerDisconnected：连接关闭（主动禁用/重载/健康失败均触发）。
	EventServerDisconnected EventType = "mcp.server.disconnected"
	// EventServerAuthRequired：401/407 或凭据被拒（需管理员处理，不自动重试为 healthy）。
	EventServerAuthRequired EventType = "mcp.server.auth_required"
	// EventServerReloadFailed：建连/重载失败（进入退避）。
	EventServerReloadFailed EventType = "mcp.server.reload_failed"
	// EventToolsDiscovered：一次工具发现完成（Detail 含 added/updated/removed 计数）。
	EventToolsDiscovered EventType = "mcp.tools.discovered"
	// EventToolStateChanged：工具治理位变化（M0-08 触发；manager 仅转发）。
	EventToolStateChanged EventType = "mcp.tool.state_changed"
	// EventToolQuarantined：工具被隔离（如 schema_hash 变更待复核）。
	EventToolQuarantined EventType = "mcp.tool.quarantined"
)

// Event 是生命周期事件。
type Event struct {
	Type     EventType
	TenantID int
	ServerID int
	Server   string // 服务器稳定标识（投影名主体）
	Tool     string // 相关工具（原始名）；非工具事件为空
	Detail   string // 已脱敏的人读说明
	At       time.Time
}

// EventSink 是事件出口。实现必须快速返回（不得阻塞连接生命周期）。
type EventSink interface {
	EmitMCPEvent(event Event)
}

// DiscardEvents 返回丢弃所有事件的 sink（默认值）。
func DiscardEvents() EventSink { return discardSink{} }

type discardSink struct{}

func (discardSink) EmitMCPEvent(Event) {}

// CollectEvents 返回记录事件的 sink（供测试与 M0-11 前的本地观测使用）。
func CollectEvents() *MemoryEventSink { return &MemoryEventSink{} }

// MemoryEventSink 记录收到的事件（线程安全）。
type MemoryEventSink struct {
	mu     sync.Mutex
	events []Event
}

// EmitMCPEvent 实现 EventSink。
func (s *MemoryEventSink) EmitMCPEvent(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

// Events 返回事件快照（拷贝）。
func (s *MemoryEventSink) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

// Count 返回指定类型的事件数量。
func (s *MemoryEventSink) Count(eventType EventType) int {
	count := 0
	for _, event := range s.Events() {
		if event.Type == eventType {
			count++
		}
	}
	return count
}
