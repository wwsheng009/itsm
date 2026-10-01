package common

import (
	"time"
)

// User represents a system user
type User struct {
	ID           int     `json:"id"`
	Username     string  `json:"username"`
	Email        string  `json:"email"`
	Name         string  `json:"name"`
	Role         string  `json:"role"`
	MSPRole      *string `json:"mspRole,omitempty"`
	Department   string  `json:"department"`
	DepartmentID int     `json:"departmentId"`
	Phone        string  `json:"phone"`
	Active       bool    `json:"active"`
	// MustChangePassword 首登强制改密标志（IP-P1-5）：前端据此进入改密流程。
	MustChangePassword bool      `json:"mustChangePassword"`
	TenantID           int       `json:"tenantId"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Permissions        []string  `json:"permissions,omitempty"` // 用户权限列表
}

// Department represents a spatial or organizational unit
type Department struct {
	ID          int           `json:"id"`
	Name        string        `json:"name"`
	Code        string        `json:"code"`
	Description string        `json:"description"`
	ManagerID   int           `json:"managerId"`
	ParentID    int           `json:"parentId"`
	TenantID    int           `json:"tenantId"`
	Children    []*Department `json:"children,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// Team represents a group of users
type Team struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	ManagerID   int       `json:"managerId"`
	TenantID    int       `json:"tenantId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Tag represents a metadata label
type Tag struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Color       string    `json:"color"`
	TenantID    int       `json:"tenantId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AuditLog represents a system activity record
type AuditLog struct {
	ID          int       `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	TenantID    int       `json:"tenantId"`
	UserID      int       `json:"userId"`
	RequestID   string    `json:"requestId"`
	IP          string    `json:"ip"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Path        string    `json:"path"`
	Method      string    `json:"method"`
	StatusCode  int       `json:"statusCode"`
	RequestBody string    `json:"requestBody"`
}

// AuthResult contains tokens and user context
// TenantSelection 描述服务端派生的登录作用域（IP-P0-6）：
// 登录不接受客户端选择租户，仅支持登录后经 /auth/switch-tenant 显式切换。
type TenantSelection struct {
	Mode string `json:"mode"` // single | home | platform | switch
}

// TenantInfo 是认证响应中的租户最小视图（避免直接序列化 ent.Tenant）。
type TenantInfo struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type AuthResult struct {
	AccessToken     string           `json:"-"`
	RefreshToken    string           `json:"-"`
	User            *User            `json:"user"`
	Tenant          *TenantInfo      `json:"tenant,omitempty"`
	TenantSelection *TenantSelection `json:"tenantSelection,omitempty"`
}
