// runtime.go：公共能力面（GET /api/v1/capabilities）使用的运行时能力源注入（V2-01）。
//
// 与 handlers/capability 的产品能力清单解耦：清单由静态注册表描述；
// 本文件只承载「需要按租户运行时求值」的开关快照（如 assistant.memory_enabled），
// 由 bootstrap 注入 *ConfigSource（与 MCP/Bot 开关同一实例），测试可注入等价替身。
package capability

import (
	"context"
	"sync"
)

var (
	runtimeMu     sync.RWMutex
	runtimeSource Source
)

// SetRuntimeSource 注入运行时能力源（bootstrap 装配；nil = 恢复未注入）。
func SetRuntimeSource(src Source) {
	runtimeMu.Lock()
	runtimeSource = src
	runtimeMu.Unlock()
}

// SnapshotFor 读取当前运行时能力快照；未注入时返回 (零值, false)。
//
// fail-safe：调用方在 ok=false 时按产品默认值回退——能力读取绝不允许阻断主链路。
func SnapshotFor(ctx context.Context, tenantID int) (Snapshot, bool) {
	runtimeMu.RLock()
	src := runtimeSource
	runtimeMu.RUnlock()
	if src == nil {
		return Snapshot{}, false
	}
	return src.For(ctx, tenantID), true
}
