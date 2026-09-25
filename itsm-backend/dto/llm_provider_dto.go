package dto

import (
	"encoding/json"
	"time"
)

// 本文件是主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-4 的 DTO 契约（§3.4）。
//
// 约束：
//   - json tag 一律 camelCase，与既有 dto 包风格一致；
//   - 响应结构绝不包含明文密钥或密文：只暴露 hasApiKey + maskedApiKey（MaskSecret 结果）；
//   - 请求侧 adapterOptions 用 json.RawMessage 承接原始 JSON，交给
//     service.ValidateAdapterOptions 做「≤4KB / 对象 / 敏感键黑名单」校验后再落库。

// LLMCreateProviderRequest POST /api/v1/ai/providers 请求体。
type LLMCreateProviderRequest struct {
	Name           string          `json:"name"`
	DisplayName    string          `json:"displayName"`
	Protocol       string          `json:"protocol"`
	Variant        string          `json:"variant"`
	Model          string          `json:"model"`
	Endpoint       string          `json:"endpoint"`
	Deployment     string          `json:"deployment"`
	APIKey         string          `json:"apiKey"`
	AdapterOptions json.RawMessage `json:"adapterOptions"`
	Enabled        *bool           `json:"enabled"`
	IsDefault      bool            `json:"isDefault"`
}

// LLMUpdateProviderRequest PUT /api/v1/ai/providers/:id 请求体。
//
// 指针语义：nil = 不修改；apiKey 传空串 = 清空密钥；adapterOptions 传 "{}" = 清空。
// name 不可修改（唯一键 + 密文归属校验锚点），更新 name 需删除重建。
type LLMUpdateProviderRequest struct {
	DisplayName    *string         `json:"displayName"`
	Protocol       *string         `json:"protocol"`
	Variant        *string         `json:"variant"`
	Model          *string         `json:"model"`
	Endpoint       *string         `json:"endpoint"`
	Deployment     *string         `json:"deployment"`
	APIKey         *string         `json:"apiKey"`
	AdapterOptions json.RawMessage `json:"adapterOptions"`
	Enabled        *bool           `json:"enabled"`
}

// LLMUserPreferenceRequest PUT /api/v1/ai/user-preference 请求体。
// providerKey 为 nil / 空串 = 清除个人默认（跟随租户默认）。
type LLMUserPreferenceRequest struct {
	ProviderKey *string `json:"providerKey"`
}

// LLMCapabilitiesDTO 协议/实例能力位（§3.4 available 响应）。
type LLMCapabilitiesDTO struct {
	SupportsStream    bool `json:"supportsStream"`
	SupportsTools     bool `json:"supportsTools"`
	SupportsReasoning bool `json:"supportsReasoning"`
	Implemented       bool `json:"implemented"`
}

// LLMProtocolOptionDTO 协议下拉选项（BE-8 LLMProtocolOptions 的 DTO 投影）。
type LLMProtocolOptionDTO struct {
	Protocol     string             `json:"protocol"`
	Implemented  bool               `json:"implemented"`
	Variants     []string           `json:"variants"`
	Capabilities LLMCapabilitiesDTO `json:"capabilities"`
}

// LLMProviderDTO 管理列表 / 详情响应项。
//
// 安全：不含明文密钥与密文；MaskedAPIKey 为 common.MaskSecret 结果（解密失败时退化为 "****"）。
type LLMProviderDTO struct {
	ID             int                    `json:"id"`
	Key            string                 `json:"key"`
	DisplayName    string                 `json:"displayName"`
	Protocol       string                 `json:"protocol"`
	Variant        string                 `json:"variant"`
	Model          string                 `json:"model"`
	Endpoint       string                 `json:"endpoint"`
	Deployment     string                 `json:"deployment"`
	AdapterOptions map[string]interface{} `json:"adapterOptions,omitempty"`
	Enabled        bool                   `json:"enabled"`
	IsDefault      bool                   `json:"isDefault"`
	Source         string                 `json:"source"`
	Status         string                 `json:"status"`
	LastError      string                 `json:"lastError,omitempty"`
	LastTestedAt   *time.Time             `json:"lastTestedAt,omitempty"`
	HasAPIKey      bool                   `json:"hasApiKey"`
	MaskedAPIKey   string                 `json:"maskedApiKey,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
}

// LLMProviderListResponse GET /api/v1/ai/providers 响应 data。
type LLMProviderListResponse struct {
	Items           []LLMProviderDTO       `json:"items"`
	Total           int                    `json:"total"`
	ProtocolOptions []LLMProtocolOptionDTO `json:"protocolOptions"`
}

// LLMProviderAvailableDTO GET /api/v1/ai/providers/available 响应项（无密钥）。
type LLMProviderAvailableDTO struct {
	Key               string `json:"key"`
	DisplayName       string `json:"displayName"`
	Protocol          string `json:"protocol"`
	Variant           string `json:"variant"`
	Model             string `json:"model"`
	SupportsStream    bool   `json:"supportsStream"`
	SupportsTools     bool   `json:"supportsTools"`
	SupportsReasoning bool   `json:"supportsReasoning"`
	Implemented       bool   `json:"implemented"`
	IsDefault         bool   `json:"isDefault"`
}

// LLMProviderTestResponse POST /api/v1/ai/providers/:id/test 响应 data。
// HTTP 恒为 200（除 404/422 前置校验失败）；连通性失败体现在 ok=false + error 字段（已脱敏）。
type LLMProviderTestResponse struct {
	ID       int       `json:"id"`
	Key      string    `json:"key"`
	OK       bool      `json:"ok"`
	Status   string    `json:"status"`
	Error    string    `json:"error,omitempty"`
	TestedAt time.Time `json:"testedAt"`
}

// LLMImportStaticResponse POST /api/v1/ai/providers/import-static 响应 data。
type LLMImportStaticResponse struct {
	Updated  bool           `json:"updated"`
	Created  bool           `json:"created"`
	Provider LLMProviderDTO `json:"provider"`
}

// LLMUserPreferenceResponse GET/PUT /api/v1/ai/user-preference 响应 data。
type LLMUserPreferenceResponse struct {
	ProviderKey          string `json:"providerKey"`
	EffectiveProviderKey string `json:"effectiveProviderKey"`
	Source               string `json:"source"`
}

// LLMProviderDeleteResponse DELETE /api/v1/ai/providers/:id 响应 data。
type LLMProviderDeleteResponse struct {
	ID      int  `json:"id"`
	Deleted bool `json:"deleted"`
}
