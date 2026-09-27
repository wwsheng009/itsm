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
}

// TestLoadConfig_MCPDefaults 验证未配置 mcp 块时的默认值：
// 开关全关（零行为变化）+ 连接与治理默认值生效（10/30/10/20）。
func TestLoadConfig_MCPDefaults(t *testing.T) {
	cfg := newTestConfig(t, "server:\n  port: 8080\n")

	if cfg.MCP.Enabled {
		t.Error("mcp.enabled 默认应为 false（关闭 = 对现有系统零行为变化）")
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
}
