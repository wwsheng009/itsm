package dto

// DefaultTenantTemplateVersion 是平台当前支持的租户产品模板版本。
//
// pkg/seeder.CurrentTenantTemplateVersion 以别名方式引用本常量：service 层不能
// import pkg/seeder（seeder → service 已存在反向依赖，直接引用会构成 import cycle），
// 因此把版本的单一事实来源放在两家都能安全引用的 dto 包。
const DefaultTenantTemplateVersion = "1.0.0"

// TenantReadinessItem 是租户模板供给就绪度的单项计数。
// 字段名与 GET /api/v1/tenants/:id/readiness 的 items 契约一致。
type TenantReadinessItem struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Required bool   `json:"required"`
}

// TenantReadinessResponse 是 readiness / provision 共用的返回结构。
type TenantReadinessResponse struct {
	TenantID        int                   `json:"tenantId"`
	TemplateVersion string                `json:"templateVersion"`
	Ready           bool                  `json:"ready"`
	BootstrapAdmins int                   `json:"bootstrapAdmins"`
	Items           []TenantReadinessItem `json:"items"`
}

// ProvisionTenantRequest 是 POST /api/v1/tenants/:id/provision 的请求体。
// templateVersion 可空，空值表示使用 DefaultTenantTemplateVersion。
type ProvisionTenantRequest struct {
	TemplateVersion string `json:"templateVersion" binding:"omitempty,max=50"`
}

// BootstrapAdminRequest 是 POST /api/v1/tenants/:id/bootstrap-admin 的请求体。
// 三个字段均可空：缺失 password 时由服务端生成一次性强密码。
type BootstrapAdminRequest struct {
	Password string `json:"password" binding:"omitempty,min=12,max=128"`
	Username string `json:"username" binding:"omitempty,max=64"`
	Email    string `json:"email" binding:"omitempty,email,max=128"`
}

// BootstrapAdminResponse 是首管创建结果；password 仅在服务端生成时回传一次。
type BootstrapAdminResponse struct {
	UserID             int    `json:"userId"`
	Username           string `json:"username"`
	Email              string `json:"email"`
	Password           string `json:"password,omitempty"`
	Generated          bool   `json:"generated"`
	MustChangePassword bool   `json:"mustChangePassword"`
}
