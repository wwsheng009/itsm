package dto

import "time"

// CreateTenantRequest 创建租户请求
type CreateTenantRequest struct {
	Name            string                 `json:"name" binding:"required,max=100" comment:"租户名称"`
	Code            string                 `json:"code" binding:"required,max=50" comment:"租户代码"`
	Domain          *string                `json:"domain,omitempty" binding:"omitempty,max=100" comment:"自定义域名"`
	Type            string                 `json:"type" binding:"required,oneof=internal saas_customer msp_provider msp_customer" comment:"租户类型（目标集合；legacy 值只读）"`
	Status          *string                `json:"status,omitempty" binding:"omitempty,oneof=active suspended expired deleted" comment:"租户状态"`
	Settings        map[string]interface{} `json:"settings,omitempty" comment:"租户配置"`
	Quota           map[string]interface{} `json:"quota,omitempty" comment:"资源配额"`
	ExpiresAt       *time.Time             `json:"expiresAt,omitempty" comment:"过期时间"`
	ParentTenantID  *int                   `json:"parentTenantId,omitempty" comment:"父租户ID"`
	MSPProviderID   *int                   `json:"mspProviderId,omitempty" comment:"MSP 提供方租户ID"`
	PlanCode        *string                `json:"planCode,omitempty" comment:"套餐编码"`
	BillingEnabled  *bool                  `json:"billingEnabled,omitempty" comment:"是否启用计费/核算"`
	CostCenterCode  *string                `json:"costCenterCode,omitempty" comment:"成本中心编码"`
	LegalEntityCode *string                `json:"legalEntityCode,omitempty" comment:"法人实体编码"`
	Currency        *string                `json:"currency,omitempty" comment:"结算币种"`
	ServiceTier     *string                `json:"serviceTier,omitempty" comment:"服务等级"`
	OwnerContact    *string                `json:"ownerContact,omitempty" comment:"租户负责人联系方式"`
}

// UpdateTenantRequest 更新租户请求
type UpdateTenantRequest struct {
	Name            *string                `json:"name,omitempty" binding:"omitempty,max=100" comment:"租户名称"`
	Domain          *string                `json:"domain,omitempty" binding:"omitempty,max=100" comment:"自定义域名"`
	Type            *string                `json:"type,omitempty" binding:"omitempty,oneof=internal saas_customer msp_provider msp_customer" comment:"租户类型（目标集合；legacy 值只读）"`
	Status          *string                `json:"status,omitempty" binding:"omitempty,oneof=active suspended expired deleted" comment:"租户状态"`
	Settings        map[string]interface{} `json:"settings,omitempty" comment:"租户配置"`
	Quota           map[string]interface{} `json:"quota,omitempty" comment:"资源配额"`
	ExpiresAt       *time.Time             `json:"expiresAt,omitempty" comment:"过期时间"`
	ParentTenantID  *int                   `json:"parentTenantId,omitempty" comment:"父租户ID"`
	MSPProviderID   *int                   `json:"mspProviderId,omitempty" comment:"MSP 提供方租户ID"`
	PlanCode        *string                `json:"planCode,omitempty" comment:"套餐编码"`
	BillingEnabled  *bool                  `json:"billingEnabled,omitempty" comment:"是否启用计费/核算"`
	CostCenterCode  *string                `json:"costCenterCode,omitempty" comment:"成本中心编码"`
	LegalEntityCode *string                `json:"legalEntityCode,omitempty" comment:"法人实体编码"`
	Currency        *string                `json:"currency,omitempty" comment:"结算币种"`
	ServiceTier     *string                `json:"serviceTier,omitempty" comment:"服务等级"`
	OwnerContact    *string                `json:"ownerContact,omitempty" comment:"租户负责人联系方式"`
}

// ListTenantsRequest 租户列表请求
type ListTenantsRequest struct {
	Page     int    `form:"page,default=1" binding:"min=1" comment:"页码"`
	PageSize int    `form:"pageSize,default=10" binding:"min=1,max=100" comment:"每页数量"`
	Status   string `form:"status" binding:"omitempty,oneof=active suspended expired deleted" comment:"状态过滤"`
	Type     string `form:"type" binding:"omitempty,oneof=standard internal saas_customer msp_provider msp_customer msp customer" comment:"类型过滤（兼容 legacy 输入，服务端归一）"`
	Search   string `form:"search" comment:"搜索关键词"`
}

// TenantResponse 租户响应
type TenantResponse struct {
	ID              int                    `json:"id"`
	Name            string                 `json:"name"`
	Code            string                 `json:"code"`
	Domain          *string                `json:"domain,omitempty"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Settings        map[string]interface{} `json:"settings,omitempty"`
	Quota           map[string]interface{} `json:"quota,omitempty"`
	ExpiresAt       *time.Time             `json:"expiresAt,omitempty"`
	ParentTenantID  *int                   `json:"parentTenantId,omitempty"`
	MSPProviderID   *int                   `json:"mspProviderId,omitempty"`
	PlanCode        *string                `json:"planCode,omitempty"`
	BillingEnabled  bool                   `json:"billingEnabled"`
	CostCenterCode  *string                `json:"costCenterCode,omitempty"`
	LegalEntityCode *string                `json:"legalEntityCode,omitempty"`
	Currency        *string                `json:"currency,omitempty"`
	ServiceTier     *string                `json:"serviceTier,omitempty"`
	OwnerContact    *string                `json:"ownerContact,omitempty"`
	Timezone        string                 `json:"timezone"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
}

// TenantListResponse 租户列表响应
type TenantListResponse struct {
	Tenants  []TenantResponse `json:"tenants"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// TenantQuotaLimits 租户硬配额上限（键缺省 / 0 = 不限；IP-P2-6）。
type TenantQuotaLimits struct {
	MaxUsers           int64 `json:"maxUsers,omitempty"`
	MaxTicketsPerMonth int64 `json:"maxTicketsPerMonth,omitempty"`
	MaxStorageMB       int64 `json:"maxStorageMB,omitempty"`
}

// TenantQuotaUsage 租户当前用量（与写入校验同口径）。
type TenantQuotaUsage struct {
	Users            int64 `json:"users"`
	TicketsThisMonth int64 `json:"ticketsThisMonth"`
	StorageBytes     int64 `json:"storageBytes"`
}

// TenantQuotaUsageResponse GET /api/v1/tenants/:id/usage 响应（治理页“配额 vs 用量”）。
type TenantQuotaUsageResponse struct {
	TenantID int               `json:"tenantId"`
	Limits   TenantQuotaLimits `json:"limits"`
	Used     TenantQuotaUsage  `json:"used"`
}
