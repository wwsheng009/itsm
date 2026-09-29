package config

import "testing"

// TestLoadConfig_MCPSwitches 覆盖 M0-01 验收：
// mcp 块可被 config.yaml 解析，且 `${MCP_*:默认}` 形式可被环境变量覆盖
// （部署侧只改环境变量即可开关与调参，无需改代码）。
func TestLoadConfig_MCPSwitches(t *testing.T) {
	t.Setenv("MCP_ENABLED", "true")

	cfg := newTestConfig(t, `mcp:
  enabled: ${MCP_ENABLED:false}
  connect_timeout_seconds: 15
  call_timeout_seconds: 45
  test_timeout_seconds: 30
  max_servers_per_tenant: 5
  tools_budget: 12
  tools_context_tokens: 64000
  tools_token_share: 0.5
`)

	if !cfg.MCP.Enabled {
		t.Error("mcp.enabled 应为 true（来自 MCP_ENABLED 环境变量覆盖）")
	}
	if cfg.MCP.ConnectTimeoutSeconds != 15 {
		t.Errorf("connect_timeout_seconds 应为 15，实际 %d", cfg.MCP.ConnectTimeoutSeconds)
	}
	if cfg.MCP.CallTimeoutSeconds != 45 {
		t.Errorf("call_timeout_seconds 应为 45，实际 %d", cfg.MCP.CallTimeoutSeconds)
	}
	if cfg.MCP.TestTimeoutSeconds != 10 {
		t.Errorf("test_timeout_seconds 应被钳制到上限 10，实际 %d", cfg.MCP.TestTimeoutSeconds)
	}
	if cfg.MCP.MaxServersPerTenant != 5 {
		t.Errorf("max_servers_per_tenant 应为 5，实际 %d", cfg.MCP.MaxServersPerTenant)
	}
	if cfg.MCP.ToolsBudget != 12 {
		t.Errorf("tools_budget 应为 12，实际 %d", cfg.MCP.ToolsBudget)
	}
	if cfg.MCP.ToolsContextTokens != 64000 {
		t.Errorf("tools_context_tokens 应为 64000，实际 %d", cfg.MCP.ToolsContextTokens)
	}
	if cfg.MCP.ToolsTokenShare != 0.5 {
		t.Errorf("tools_token_share 应为 0.5，实际 %v", cfg.MCP.ToolsTokenShare)
	}
}

// TestLoadConfig_MCPDefaults 验证未配置 mcp 块时的默认值：
// 全局开关默认**开启**（2026-09-27 变更：管理面与组件就绪；服务器、工具与写面仍默认拒绝）
// + 连接与治理默认值生效（10/30/10/20）。
func TestLoadConfig_MCPDefaults(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if !cfg.MCP.Enabled {
		t.Error("mcp.enabled 未显式配置时默认应为 true（管理面/组件就绪；显式 false 仍可关闭）")
	}
	if cfg.MCP.ConnectTimeoutSeconds != 10 {
		t.Errorf("connect_timeout_seconds 默认应为 10，实际 %d", cfg.MCP.ConnectTimeoutSeconds)
	}
	if cfg.MCP.CallTimeoutSeconds != 30 {
		t.Errorf("call_timeout_seconds 默认应为 30，实际 %d", cfg.MCP.CallTimeoutSeconds)
	}
	if cfg.MCP.TestTimeoutSeconds != 10 {
		t.Errorf("test_timeout_seconds 默认应为 10，实际 %d", cfg.MCP.TestTimeoutSeconds)
	}
	if cfg.MCP.MaxServersPerTenant != 20 {
		t.Errorf("max_servers_per_tenant 默认应为 20，实际 %d", cfg.MCP.MaxServersPerTenant)
	}
	// M2-03 工具面预算默认值（须与 config.yaml.example 同值）。
	if cfg.MCP.ToolsBudget != 40 {
		t.Errorf("tools_budget 默认应为 40，实际 %d", cfg.MCP.ToolsBudget)
	}
	if cfg.MCP.ToolsContextTokens != 128000 {
		t.Errorf("tools_context_tokens 默认应为 128000，实际 %d", cfg.MCP.ToolsContextTokens)
	}
	if cfg.MCP.ToolsTokenShare != 0.30 {
		t.Errorf("tools_token_share 默认应为 0.30，实际 %v", cfg.MCP.ToolsTokenShare)
	}
}

// TestLoadConfig_MCPWriteEnabledEnvOverride 验证 mcp.write_enabled（L1.5 回滚开关）的
// 环境变量兜底与优先级：本地/CI 联调写工具审批链路时无需改 config.yaml 即可开启；
// 环境变量显式设置时优先于 YAML（与 MCP_ENABLED 同口径），未设置时保持 YAML/默认值。
func TestLoadConfig_MCPWriteEnabledEnvOverride(t *testing.T) {
	// ① 未设置环境变量：YAML 显式 false 保持 false（默认值口径）。
	cfg := newTestConfig(t, "mcp:\n  write_enabled: false\n")
	if cfg.MCP.WriteEnabled {
		t.Error("write_enabled 未配置环境变量时应保持 YAML 值 false")
	}

	// ② 环境变量 true：覆盖 YAML 的 false。
	t.Setenv("MCP_WRITE_ENABLED", "true")
	cfg = newTestConfig(t, "mcp:\n  write_enabled: false\n")
	if !cfg.MCP.WriteEnabled {
		t.Error("MCP_WRITE_ENABLED=true 应覆盖 YAML 中的 write_enabled: false")
	}

	// ③ 无 mcp 块 + 环境变量 true：默认 false 被覆盖为 true。
	cfg = newTestConfig(t, "server:\n  port: 8080\n")
	if !cfg.MCP.WriteEnabled {
		t.Error("无 mcp 块时 MCP_WRITE_ENABLED=true 应把默认 false 覆盖为 true")
	}
}

// TestLoadConfig_MCPExplicitDisable 验证显式关闭优先于「默认开启」（回滚 L1）：
// config.yaml 里写 enabled: false（或 MCP_ENABLED=false）时，全局开关必须保持关闭。
func TestLoadConfig_MCPExplicitDisable(t *testing.T) {
	t.Run("yaml 显式 false", func(t *testing.T) {
		cfg := newTestConfig(t, "mcp:\n  enabled: false\n")
		if cfg.MCP.Enabled {
			t.Error("mcp.enabled 显式 false 时必须保持关闭（零装配、零路由）")
		}
	})

	t.Run("环境变量覆盖为 false", func(t *testing.T) {
		t.Setenv("MCP_ENABLED", "false")
		cfg := newTestConfig(t, "mcp:\n  enabled: ${MCP_ENABLED:true}\n")
		if cfg.MCP.Enabled {
			t.Error("MCP_ENABLED=false 必须覆盖 yaml 默认（部署侧一键回滚）")
		}
	})
}

// TestLoadConfig_MCPOutboundSecurity 覆盖出站安全平台开关（M0-05）：
// 默认严格（仅 https + 公网 + 默认端口）；环境变量可放开（本地联调 mock MCP），
// 部署侧无需改 config.yaml（生产必须保持严格）。
func TestLoadConfig_MCPOutboundSecurity(t *testing.T) {
	t.Run("默认严格", func(t *testing.T) {
		cfg := newTestConfig(t, "server:\n  port: 8080\n")
		if cfg.MCP.AllowHTTP || cfg.MCP.AllowPrivateNetworks {
			t.Error("allow_http/allow_private_networks 默认必须为 false（仅 https + 公网）")
		}
		if ports := cfg.MCP.AllowedPortList(); len(ports) != 0 {
			t.Errorf("allowed_ports 默认应为空，实际 %v", ports)
		}
	})

	t.Run("环境变量放开与端口解析", func(t *testing.T) {
		t.Setenv("MCP_ALLOW_HTTP", "true")
		t.Setenv("MCP_ALLOW_PRIVATE_NETWORKS", "true")
		t.Setenv("MCP_ALLOWED_PORTS", "19090,8443,oops,70000")
		cfg := newTestConfig(t, "server:\n  port: 8080\n")
		if !cfg.MCP.AllowHTTP || !cfg.MCP.AllowPrivateNetworks {
			t.Error("MCP_ALLOW_HTTP/MCP_ALLOW_PRIVATE_NETWORKS=true 应生效")
		}
		ports := cfg.MCP.AllowedPortList()
		if len(ports) != 2 || ports[0] != 19090 || ports[1] != 8443 {
			t.Errorf("allowed_ports 应解析出 [19090 8443]（非法项忽略），实际 %v", ports)
		}
	})
}
