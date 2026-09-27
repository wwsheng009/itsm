package registry

// QuarantineReason 是工具被隔离的原因（隔离工具不暴露、不可执行，仅可诊断）。
type QuarantineReason string

const (
	// ReasonCanonicalCollision：canonical 名与既有工具完全碰撞（后注册者隔离，先到者保留）。
	ReasonCanonicalCollision QuarantineReason = "canonical_collision"
	// ReasonInvalidServerName：服务器稳定标识不符合 ^[a-z0-9_-]{1,32}$。
	ReasonInvalidServerName QuarantineReason = "invalid_server_name"
	// ReasonEmptyToolName：原始工具名为空（trim 后）。
	ReasonEmptyToolName QuarantineReason = "empty_tool_name"
)

// QuarantinedTool 描述一个被隔离的工具。
type QuarantinedTool struct {
	Server       string           // 服务器标识（可能为非法输入，原样保留便于诊断）
	RawName      string           // 原始工具名（trim 后；空名为空串）
	CallableName string           // canonical 名（非法服务器名时为空）
	Reason       QuarantineReason // 隔离原因
	Detail       string           // 人读说明（如碰撞对象 `server/raw`）
}
