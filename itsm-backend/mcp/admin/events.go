package admin

import (
	"sync"

	"itsm-backend/mcp/manager"
)

// EventBuffer 保存各服务器近期生命周期事件（供 GET /:id/events 与健康页时间线）。
//
// 实现 manager.EventSink：装配时把同一实例传给 manager.Options.Events（M0-10）。
// 仅内存保留最近 N 条；持久化审计属 M0-11。
type EventBuffer struct {
	mu       sync.Mutex
	limit    int
	byServer map[int][]manager.Event
	all      []manager.Event
}

// NewEventBuffer 创建事件环形缓冲（limit<=0 时默认 100/服务器）。
func NewEventBuffer(limit int) *EventBuffer {
	if limit <= 0 {
		limit = 100
	}
	return &EventBuffer{limit: limit, byServer: map[int][]manager.Event{}}
}

// EmitMCPEvent 实现 manager.EventSink。
func (b *EventBuffer) EmitMCPEvent(event manager.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	perServer := append(b.byServer[event.ServerID], event)
	if len(perServer) > b.limit {
		perServer = perServer[len(perServer)-b.limit:]
	}
	b.byServer[event.ServerID] = perServer

	b.all = append(b.all, event)
	if len(b.all) > b.limit*4 {
		b.all = b.all[len(b.all)-b.limit*4:]
	}
}

// List 返回某服务器的事件（时间升序拷贝）。
func (b *EventBuffer) List(serverID int) []manager.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]manager.Event(nil), b.byServer[serverID]...)
}

// ListAll 返回全部保留事件（时间升序拷贝）。
func (b *EventBuffer) ListAll() []manager.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]manager.Event(nil), b.all...)
}
