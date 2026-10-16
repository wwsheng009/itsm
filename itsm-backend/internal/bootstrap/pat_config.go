package bootstrap

import (
	"itsm-backend/config"
	"itsm-backend/handlers/pat"
)

// patConfigView 把全局配置映射为 PAT 服务层视图（config.PATConfig → pat.PATConfigView）。
func patConfigView(cfg *config.Config) pat.PATConfigView {
	return pat.PATConfigView{
		Enabled:          cfg.PAT.Enabled,
		DefaultTTLDays:   cfg.PAT.DefaultTTLDays,
		MaxTTLDays:       cfg.PAT.MaxTTLDays,
		MaxActivePerUser: cfg.PAT.MaxActivePerUser,
	}
}
