package dto

import "time"

// CreatePermissionRequest 创建权限请求
type CreatePermissionRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Resource    string `json:"resource" binding:"required"`
	Action      string `json:"action" binding:"required"`
}

// PermissionResponse 权限响应
type PermissionResponse struct {
	ID          int    `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	// Assignable 当前租户是否可分配该权限码（通配/平台治理家族在非平台根为 false；IP-P2-7 收口补丁 8）。
	Assignable bool      `json:"assignable"`
	TenantID   int       `json:"tenantId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
