package dto

// UserPreferencesResponse 用户偏好读写响应（IP-P1-6c）。
// 当前白名单键：workbenchFilter（工作台过滤器：mode/customerTenantIds 等）。
type UserPreferencesResponse struct {
	Preferences map[string]any `json:"preferences"`
}
