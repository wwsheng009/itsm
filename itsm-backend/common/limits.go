package common

// 请求体与附件上限的进程级默认值。
//
// 服务端通过 GET /api/v1/capabilities 的 limits 字段下发给客户端，
// 避免客户端把上限写死；上传路由使用「文件上限 + multipart 余量」，
// 其余 API 仍保持 10 MB，防止大文件上限放大普通 JSON 接口的攻击面。
const (
	// DefaultRequestMaxBytes 普通 API 请求体上限（10 MB）。
	DefaultRequestMaxBytes int64 = 10 * 1024 * 1024

	// AttachmentMultipartOverheadBytes 为 multipart 边界、字段名与文件头预留的余量。
	// 实际文件大小仍由 AttachmentService 按服务端配置精确校验。
	AttachmentMultipartOverheadBytes int64 = 2 * 1024 * 1024
)
