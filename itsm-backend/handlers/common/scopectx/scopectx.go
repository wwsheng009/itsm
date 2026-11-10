// Package scopectx 提供请求期 actor 数据范围解析的统一入口。
//
// 设计动机：
//   - 深切换后 JWT 主角色仍是 users.role（如 end_user），而目标租户内的有效角色
//     来自 membership（如 msp_tech）；继续用 c.GetString("role") 判定 DataScope
//     会把 provider 员工收窄成“仅本人”，列表为空、统计却有数。
//   - RBACMiddleware 已把主角色 + m2m + membership 角色物化到 c "roles"，
//     本包只负责把「有效角色集合 → roles.data_scope」这一层收口。
//
// 安全语义：client 缺失（单测/极简嵌入）时 ResolveForActor 回退内置词表规范值，
// 不 fail-open；tenantID 无效时安全收窄为 owner。
package scopectx

import (
	"itsm-backend/ent"
	"itsm-backend/handlers/common/datascope"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// Resolve 解析当前请求在 tenantID 内的有效数据范围。
//
// 调用方：租户内列表/统计/行级写守卫。典型用法：
//
//	scope := scopectx.Resolve(c, tenantID)
func Resolve(c *gin.Context, tenantID int) datascope.DataScope {
	if c == nil || tenantID <= 0 {
		return datascope.DataScopeOwnedOrAssigned
	}
	if scope, ok := datascope.DataScopeFromContext(c.Request.Context()); ok {
		return scope
	}

	primaryRole := c.GetString("role")
	var client *ent.Client
	if value, ok := c.Get("client"); ok {
		if entClient, ok := value.(*ent.Client); ok {
			client = entClient
		}
	}

	return datascope.ResolveForActor(
		c.Request.Context(),
		client,
		tenantID,
		primaryRole,
		middleware.GetContextRoles(c),
	)
}

// Middleware 在租户路由组内统一解析一次 DataScope，并写入请求 context。
//
// 挂载位置：RBACMiddleware / TenantMiddleware 之后、业务 handler 之前。
// 这样读 handler 的 scopectx.Resolve 会直接命中 context，写路径的服务层
// 可通过 datascope.CanWriteResourceFromContext 复用同一范围，避免每层重复查库。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetInt("tenant_id")
		if tenantID > 0 {
			scope := Resolve(c, tenantID)
			c.Request = c.Request.WithContext(datascope.WithDataScope(c.Request.Context(), scope))
		}
		c.Next()
	}
}
