package registry

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound 表示目标名称不存在（fail-closed 的“未找到”分支）。
// 调用方应先尝试 canonical 名，再尝试原始短名。
var ErrNotFound = errors.New("MCP 工具不存在")

// ErrEmptyName 表示查询名称为空（trim 后）。
var ErrEmptyName = errors.New("工具名称不能为空")

// AmbiguousToolError 表示原始短名命中多个服务，调用方必须改用 canonical 名。
// 多候选一律 fail-closed，不做“猜测式路由”。
type AmbiguousToolError struct {
	Name       string   // 触发歧义的查询名（通常是原始短名）
	Candidates []string // 排序后的 canonical 候选（可用于提示模型/管理员）
}

func (e *AmbiguousToolError) Error() string {
	return fmt.Sprintf("工具名 %q 在多个 MCP 服务中歧义，请改用 canonical 名（候选：%s）",
		e.Name, strings.Join(e.Candidates, "、"))
}
