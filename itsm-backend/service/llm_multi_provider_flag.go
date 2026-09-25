package service

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// 多 Provider 灰度开关（主计划 §2.2 D9）：
//   - 配置键 llm.multi_provider_enabled（config.yaml，默认 false）；
//   - 环境变量 LLM_MULTI_PROVIDER_ENABLED 优先（便于升级部署不改进文件即开）；
//   - 取值非法或未配置一律按关闭处理（零破坏优先，与 LLM_PROTOCOL_ADAPTER_ENABLED 同语义）。
//
// 关闭时（默认）：管理 API 不注册、网关走单 provider 快速路径（旧行为逐字节不变）、
// 前端不渲染页签与选择器；回滚 = 关开关，无需回滚代码。
const (
	// llmMultiProviderEnabledKey 部署级开关（config.yaml）。
	llmMultiProviderEnabledKey = "llm.multi_provider_enabled"
	// LLMMultiProviderEnabledEnv 环境变量覆盖（优先级高于配置文件）。
	LLMMultiProviderEnabledEnv = "LLM_MULTI_PROVIDER_ENABLED"
)

// MultiProviderEnabled 读取多 Provider 灰度开关（默认关、env 优先、非法值按关）。
//
// 消费者：① BE-4 管理 API 注册门禁；② BE-5 bootstrap 决定是否注入 resolver；
// ③ BE-7 chat 覆盖参数门禁；④ FE 侧由 available 接口可达性间接体现。
func MultiProviderEnabled() bool {
	if raw := strings.TrimSpace(os.Getenv(LLMMultiProviderEnabledEnv)); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			return value
		}
		return false
	}
	return viper.GetBool(llmMultiProviderEnabledKey)
}
