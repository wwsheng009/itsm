// Package systemconfig 是系统配置域的 HTTP handler 层（域切片架构）。
// 自 controller/system_config_controller.go 迁移而来（2026-09-02），
// 业务逻辑仍由 service.SystemConfigService 承载，本包只做参数解析与响应封装。
package systemconfig

import (
	"itsm-backend/capability"
	"itsm-backend/service"

	"go.uber.org/zap"
)

// Handler 系统配置 HTTP handler
type Handler struct {
	configService *service.SystemConfigService
	logger        *zap.SugaredLogger
	// M2 能力开关：运行时能力开关源（SetCapabilitySource 注入；nil = 端点返回 503）。
	capability capability.Source
	// capabilityAdmin 是同一来源的管理面能力（Update/Clear/Defaults）；未实现时为 nil。
	capabilityAdmin capabilityAdmin
}

// NewHandler 创建系统配置 handler 实例
func NewHandler(configService *service.SystemConfigService, logger *zap.SugaredLogger) *Handler {
	return &Handler{
		configService: configService,
		logger:        logger,
	}
}
