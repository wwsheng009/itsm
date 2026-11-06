package dto

import "time"

// tenant_user_admin_dto.go：平台侧租户用户管理（TUM-1/TUM-2）契约。
// 编号与语义见 docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §3.1。

// TenantUserListQuery GET /api/v1/tenants/:id/users 查询参数。
type TenantUserListQuery struct {
	Page     int    `form:"page" json:"page" comment:"页码（默认 1）"`
	PageSize int    `form:"pageSize" json:"pageSize" comment:"每页条数（默认 20，上限 100）"`
	Keyword  string `form:"keyword" json:"keyword" comment:"关键字：用户名/姓名/邮箱模糊匹配"`
	Status   string `form:"status" json:"status" comment:"active | inactive；空=全部"`
	Role     string `form:"role" json:"role" comment:"角色过滤（users.role 词表）"`
}

// TenantUserItem 平台视角下的租户用户条目（不含敏感字段）。
type TenantUserItem struct {
	ID                 int       `json:"id"`
	Username           string    `json:"username"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	Role               string    `json:"role"`
	MSPRole            *string   `json:"mspRole,omitempty"`
	Department         string    `json:"department,omitempty"`
	Active             bool      `json:"active"`
	MustChangePassword bool      `json:"mustChangePassword"`
	IsBootstrapAdmin   bool      `json:"isBootstrapAdmin"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// TenantUserListResponse 列表响应。
type TenantUserListResponse struct {
	Items []TenantUserItem `json:"items"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	// ActiveAdminCount 目标租户内「活跃 admin」全量计数：不受 keyword/status/role 筛选与分页影响，
	// 供前端「最后管理员」护栏做按钮级禁用（G13）。停用动作本身仍以服务层租户级校验为权威
	// （SetActive: 同租户其他活跃 admin 为 0 → 409 LAST_ADMIN_PROTECTED），前端判定只是 UX 收敛。
	ActiveAdminCount int `json:"activeAdminCount"`
	PageSize         int `json:"pageSize"`
}

// 重置密码模式（TUM-D4）：generated=一次性口令+强制改密（默认）；specified=管理员指定新密码。
const (
	TenantUserResetModeGenerated = "generated"
	TenantUserResetModeSpecified = "specified"
)

// ResetTenantUserPasswordRequest POST /api/v1/tenants/:id/users/:userId/reset-password。
type ResetTenantUserPasswordRequest struct {
	Mode        string `json:"mode" binding:"required,oneof=generated specified" comment:"generated | specified"`
	NewPassword string `json:"newPassword" comment:"mode=specified 时必填；生成模式忽略"`
}

// ResetTenantUserPasswordResponse GeneratedPassword 仅本次响应回传明文：
// 不落库、不落日志、不写审计（TUM-D4/A5）。
type ResetTenantUserPasswordResponse struct {
	UserID             int    `json:"userId"`
	Mode               string `json:"mode"`
	GeneratedPassword  string `json:"generatedPassword,omitempty"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

// SetTenantUserStatusRequest PUT /api/v1/tenants/:id/users/:userId/status。
type SetTenantUserStatusRequest struct {
	Active      *bool  `json:"active" binding:"required" comment:"true=启用；false=停用"`
	ConfirmCode string `json:"confirmCode" comment:"default 租户停用时必填租户编码（防误操作）"`
}

// ForceLogoutTenantUserResponse POST /api/v1/tenants/:id/users/:userId/force-logout。
type ForceLogoutTenantUserResponse struct {
	UserID    int       `json:"userId"`
	RevokedAt time.Time `json:"revokedAt"`
}
