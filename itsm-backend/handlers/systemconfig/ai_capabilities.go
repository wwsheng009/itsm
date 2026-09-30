// Package systemconfig 的能力开关端点（M2 能力开关，2026-09-30 方案 §2.4）。
//
// 设计要点：
//   - 路径挂 /api/v1/system-configs/ai-capabilities（读 = system_config:read，写 = system_config:write），
//     复用既有 system_config 权限码，超级管理员专属，**不新增权限码**；
//   - 写操作落 system_configs（category=ai）并立即失效运行时缓存（免重启生效）；
//   - 与展示面同源：MCP 管理页 / Bot 页 / 工具页读同一 Source（本端点返回同一快照口径）；
//   - 变更审计：AuditMiddleware 覆盖所有 PUT 请求，另有结构化日志（tenant/operator/before/after）。
package systemconfig

import (
	"context"

	"itsm-backend/capability"
	"itsm-backend/common"

	"github.com/gin-gonic/gin"
)

// SetCapabilitySource 注入运行时能力开关源（M2 能力开关；nil = 端点返回 503）。
//
// 同时识别管理面能力（Update/Clear/Defaults）——生产实现为 *capability.ConfigSource；
// 测试可注入等价替身（实现同一接口）。
func (h *Handler) SetCapabilitySource(src capability.Source) {
	h.capability = src
	if admin, ok := src.(capabilityAdmin); ok {
		h.capabilityAdmin = admin
	}
}

// capabilityAdmin 是能力开关的管理面能力（更新/清除/默认值）。
type capabilityAdmin interface {
	capability.Source
	Update(ctx context.Context, tenantID int, patch capability.Patch, operator string) (capability.Snapshot, error)
	Clear(ctx context.Context, tenantID int, keys ...string) (capability.Snapshot, error)
	Defaults() capability.Defaults
}

// AICapabilitiesRequest 是 PUT 入参（三态）：
//
//	字段缺省（nil）→ 该键不改动；
//	reset 数组    → 指定键删除覆盖行、恢复跟随环境默认（与显式赋值可同请求并用）。
type AICapabilitiesRequest struct {
	MCPEnabled      *bool    `json:"mcpEnabled"`
	MCPWriteEnabled *bool    `json:"mcpWriteEnabled"`
	BotEnabled      *bool    `json:"botEnabled"`
	Reset           []string `json:"reset"`
}

// AICapabilitiesDefaults 是静态配置提供的默认值（页面展示「跟随环境默认」时使用）。
type AICapabilitiesDefaults struct {
	MCPEnabled      bool `json:"mcpEnabled"`
	MCPWriteEnabled bool `json:"mcpWriteEnabled"`
	BotEnabled      bool `json:"botEnabled"`
}

// AICapabilitiesView 是 GET/PUT 的响应体。
type AICapabilitiesView struct {
	MCPEnabled      bool                   `json:"mcpEnabled"`
	MCPWriteEnabled bool                   `json:"mcpWriteEnabled"`
	BotEnabled      bool                   `json:"botEnabled"`
	Defaults        AICapabilitiesDefaults `json:"defaults"`
	// Overridden[key]=true 表示该键由管理台覆盖（否则跟随环境默认）。
	Overridden map[string]bool `json:"overridden"`
	UpdatedAt  string          `json:"updatedAt,omitempty"`
	UpdatedBy  string          `json:"updatedBy,omitempty"`
	// Keys 是能力开关键清单（管理台按此渲染；与后端单一来源一致）。
	Keys []string `json:"keys"`
}

func (h *Handler) capabilitiesView(c *gin.Context, tenantID int) (*AICapabilitiesView, bool) {
	if h.capability == nil {
		common.Fail(c, common.ServiceUnavailableCode, "能力开关服务未就绪")
		return nil, false
	}
	ctx := c.Request.Context()
	snap := h.capability.For(ctx, tenantID)

	view := &AICapabilitiesView{
		MCPEnabled:      snap.MCPEnabled,
		MCPWriteEnabled: snap.MCPWriteEnabled,
		BotEnabled:      snap.BotEnabled,
		Overridden:      snap.Overridden,
		UpdatedBy:       snap.UpdatedBy,
		Keys:            capability.KnownKeys(),
	}
	if !snap.UpdatedAt.IsZero() {
		view.UpdatedAt = snap.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if view.Overridden == nil {
		view.Overridden = map[string]bool{}
	}
	if h.capabilityAdmin != nil {
		defaults := h.capabilityAdmin.Defaults()
		view.Defaults = AICapabilitiesDefaults{
			MCPEnabled:      defaults.MCPEnabled,
			MCPWriteEnabled: defaults.MCPWriteEnabled,
			BotEnabled:      defaults.BotEnabled,
		}
	}
	return view, true
}

// GetAICapabilities GET /api/v1/system-configs/ai-capabilities
func (h *Handler) GetAICapabilities(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	view, ok := h.capabilitiesView(c, tid)
	if !ok {
		return
	}
	common.Success(c, view)
}

// UpdateAICapabilities PUT /api/v1/system-configs/ai-capabilities
func (h *Handler) UpdateAICapabilities(c *gin.Context) {
	tid, ok := tenantID(c)
	if !ok {
		return
	}
	if h.capability == nil || h.capabilityAdmin == nil {
		common.Fail(c, common.ServiceUnavailableCode, "能力开关服务未就绪")
		return
	}
	var req AICapabilitiesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamError(c, "请求体非法: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	operator := c.GetString("username")
	if operator == "" {
		operator = "system"
	}

	source := h.capabilityAdmin
	before, _ := h.capabilitiesView(c, tid)
	if req.MCPEnabled != nil || req.MCPWriteEnabled != nil || req.BotEnabled != nil {
		if _, err := source.Update(ctx, tid, capability.Patch{
			MCPEnabled:      req.MCPEnabled,
			MCPWriteEnabled: req.MCPWriteEnabled,
			BotEnabled:      req.BotEnabled,
		}, operator); err != nil {
			common.FailWithErr(c, err, "能力开关保存失败")
			return
		}
	}
	if len(req.Reset) > 0 {
		for _, key := range req.Reset {
			if !capability.IsKnownKey(key) {
				common.ParamError(c, "不支持的能力开关键: "+key)
				return
			}
		}
		if _, err := source.Clear(ctx, tid, req.Reset...); err != nil {
			common.FailWithErr(c, err, "能力开关恢复默认失败")
			return
		}
	}

	view, ok := h.capabilitiesView(c, tid)
	if !ok {
		return
	}
	// 结构化日志：变更前后值（审计中间件另有全量请求体落库）。
	if h.logger != nil {
		h.logger.Infow("capability.settings.updated",
			"tenant_id", tid, "operator", operator,
			"before", before, "after", view)
	}
	common.Success(c, view)
}
