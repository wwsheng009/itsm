package capability

import "sync"

// RuntimeLimits 是 GET /api/v1/capabilities 下发的服务端硬限制。
//
// 客户端据此在发送前预检，避免把 8/10/50 MB 等上限写死在客户端；
// 数值均为字节，chunkUpload=false 表示当前只支持单次 multipart。
type RuntimeLimits struct {
	AttachmentMaxBytes int64 `json:"attachmentMaxBytes"`
	RequestMaxBytes    int64 `json:"requestMaxBytes"`
	ChunkUpload        bool  `json:"chunkUpload"`
}

var (
	limitsMu      sync.RWMutex
	runtimeLimits = RuntimeLimits{
		AttachmentMaxBytes: 10 * 1024 * 1024,
		RequestMaxBytes:    10 * 1024 * 1024,
		ChunkUpload:        false,
	}
)

// SetRuntimeLimits 注入部署级限制（bootstrap 调用）。
//
// 非正数字段忽略，保持安全默认；上限本身不在这里做业务裁剪，
// attachment.max_file_size_mb 的合法范围由 config 层负责。
func SetRuntimeLimits(limits RuntimeLimits) {
	limitsMu.Lock()
	if limits.AttachmentMaxBytes > 0 {
		runtimeLimits.AttachmentMaxBytes = limits.AttachmentMaxBytes
	}
	if limits.RequestMaxBytes > 0 {
		runtimeLimits.RequestMaxBytes = limits.RequestMaxBytes
	}
	runtimeLimits.ChunkUpload = limits.ChunkUpload
	limitsMu.Unlock()
}

// RuntimeLimitsSnapshot 返回当前限制快照。
func RuntimeLimitsSnapshot() RuntimeLimits {
	limitsMu.RLock()
	defer limitsMu.RUnlock()
	return runtimeLimits
}
