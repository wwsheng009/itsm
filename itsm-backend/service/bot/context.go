package bot

import "context"

// runIDKey 在调用链内部传递「当前运行 ID」（B1-01 贯通）。
//
// 安全语义说明：本键**不承载任何鉴权语义**（租户/用户仍走显式参数）；
// 无值时行为与既有调用完全一致（不写 run_id 列，取默认值）。
type runIDKey struct{}

// WithRunID 把运行 ID 注入上下文（id<=0 时原样返回）。
func WithRunID(ctx context.Context, id int) context.Context {
	if id <= 0 {
		return ctx
	}
	return context.WithValue(ctx, runIDKey{}, id)
}

// RunIDFromContext 读取当前运行 ID；不存在或非法时返回 0。
func RunIDFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(runIDKey{}).(int); ok && v > 0 {
		return v
	}
	return 0
}
