package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMultiProviderEnabledReadsEnvOverride 锁定 D9 开关读取语义：
// env 优先、ParseBool 兼容 1/0、非法值按关闭（与 protocolAdapterEnabled 同口径）。
func TestMultiProviderEnabledReadsEnvOverride(t *testing.T) {
	t.Setenv(LLMMultiProviderEnabledEnv, "true")
	assert.True(t, MultiProviderEnabled())

	t.Setenv(LLMMultiProviderEnabledEnv, "1")
	assert.True(t, MultiProviderEnabled(), "ParseBool 兼容 1")

	t.Setenv(LLMMultiProviderEnabledEnv, "false")
	assert.False(t, MultiProviderEnabled())

	t.Setenv(LLMMultiProviderEnabledEnv, "not-a-bool")
	assert.False(t, MultiProviderEnabled(), "非法值按关闭（零破坏优先）")
}

// TestMultiProviderEnabledDefaultsOff 未配置任何开关时默认关闭：
// 这是 QA-3「开关关闭时全链路行为与现状一致」的第一道门禁。
func TestMultiProviderEnabledDefaultsOff(t *testing.T) {
	t.Setenv(LLMMultiProviderEnabledEnv, "")
	assert.False(t, MultiProviderEnabled(), "未配置时默认关闭")
}
