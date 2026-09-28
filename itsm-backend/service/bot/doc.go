// Package bot 承载 Bot 能力的运行时组件（方案 §2.2 BP5 的模块边界）。
//
// 归属与落地顺序（任务卡见 `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md` §4）：
//   - BotPolicy（工具面交集门禁）：B2-02；
//   - RunManager（run/step/event 生命周期与广播）：B1-02；
//   - ScopeResolver（入口上下文与权限预检）：B3-01；
//   - Redactor（审计/消息/工件统一脱敏入口）：B0-06。
//
// 开关：`bot.enabled`（`config.BotConfig.Enabled`，环境变量兜底 `BOT_ENABLED`）默认
// **false**——关闭时不装配任何运行时组件，聊天链路与工具面保持现状（零行为变化）。
// 组件通过 `Gate` 判定开关，禁止在包内直读全局配置（便于测试与后续按租户灰度）。
package bot

// Gate 是 `bot.enabled` 的运行时判定入口。
//
// 约定：调用方在**装配阶段**用 Gate 决定是否挂载 Bot 组件；关闭时不得产生任何
// 副作用（不建表、不起后台任务、不改工具面）。Gate 为值对象，可安全复制。
type Gate struct {
	enabled bool
}

// NewGate 创建开关判定；enabled 来自 config.BotConfig.Enabled。
func NewGate(enabled bool) Gate {
	return Gate{enabled: enabled}
}

// Enabled 返回 Bot 能力是否启用。
func (g Gate) Enabled() bool {
	return g.enabled
}
