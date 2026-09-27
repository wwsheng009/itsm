package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// M0-09：ToolRegistry 的多 provider 聚合契约（内置优先、去重、委派、写工具拒绝）。

type fakeToolProvider struct {
	name       string
	tools      []ToolDefinition
	execResult interface{}
	execErr    error
	executed   []string
}

func (f *fakeToolProvider) ProviderName() string { return f.name }

func (f *fakeToolProvider) ListTools(context.Context, int) []ToolDefinition { return f.tools }

func (f *fakeToolProvider) Resolve(_ context.Context, _ int, name string) (*ToolDefinition, bool) {
	for index := range f.tools {
		if f.tools[index].Name == name {
			def := f.tools[index]
			return &def, true
		}
	}
	return nil, false
}

func (f *fakeToolProvider) Execute(_ context.Context, _ int, name string, _ map[string]interface{}) (*ToolExecution, error) {
	f.executed = append(f.executed, name)
	return &ToolExecution{
		Value:        f.execResult,
		Provider:     f.name,
		ServerName:   "github",
		RawToolName:  "list_issues",
		CallableName: name,
		DurationMs:   7,
	}, f.execErr
}

func fakeMCPTool(name string, readOnly bool) ToolDefinition {
	action := "write"
	if readOnly {
		action = "read"
	}
	return ToolDefinition{
		Name:        name,
		Description: "[MCP:github] " + name,
		ReadOnly:    readOnly,
		Resource:    "mcp",
		Action:      action,
		ArgsSchema:  map[string]interface{}{"type": "object"},
	}
}

func TestToolRegistry_ProviderAggregation(t *testing.T) {
	ctx := context.Background()
	registry := NewToolRegistry(nil, nil, nil, nil)
	builtinCount := len(registry.ListTools())

	provider := &fakeToolProvider{
		name:       "mcp",
		tools:      []ToolDefinition{fakeMCPTool("mcp__github__list_issues", true)},
		execResult: map[string]interface{}{"ok": true},
	}
	registry.RegisterProvider(provider)
	registry.RegisterProvider(nil) // nil 忽略

	// 工具面 = 内置 ∪ provider（内置在前）。
	all := registry.ListToolsForTenant(ctx, 1)
	require.Len(t, all, builtinCount+1)
	require.Equal(t, "mcp__github__list_issues", all[len(all)-1].Name)

	// 内置优先：provider 同名工具被忽略（不得遮蔽内置）。
	shadow := &fakeToolProvider{name: "shadow", tools: []ToolDefinition{fakeMCPTool("list_tickets", true)}}
	registry.RegisterProvider(shadow)
	all = registry.ListToolsForTenant(ctx, 1)
	require.Len(t, all, builtinCount+1, "provider 重名工具不得进入工具面")
	require.Equal(t, "ticket", registry.GetToolForTenant(ctx, 1, "list_tickets").Resource)

	// 解析：内置优先 → provider；未知名 → nil。
	require.Equal(t, "cmdb", registry.GetToolForTenant(ctx, 1, "list_cis").Resource)
	mcpTool := registry.GetToolForTenant(ctx, 1, "mcp__github__list_issues")
	require.NotNil(t, mcpTool)
	require.Equal(t, "mcp", mcpTool.Resource)
	require.Equal(t, "read", mcpTool.Action)
	require.Nil(t, registry.GetToolForTenant(ctx, 1, "mcp__github__missing"))

	// 执行：内置走内置实现；provider 工具委派；未知报错。
	if _, err := registry.Execute(ctx, 1, "mcp__github__list_issues", nil); err != nil {
		t.Fatalf("provider 工具执行失败: %v", err)
	}
	require.Equal(t, []string{"mcp__github__list_issues"}, provider.executed)

	_, err := registry.Execute(ctx, 1, "definitely_not_a_tool", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown tool")

	// provider 错误透传（脱敏由 provider 负责）。
	provider.execErr = errors.New("injected")
	_, err = registry.Execute(ctx, 1, "mcp__github__list_issues", nil)
	require.Error(t, err)
}

func TestToolRegistry_ProviderWriteToolRequiresApproval(t *testing.T) {
	ctx := context.Background()
	registry := NewToolRegistry(nil, nil, nil, nil)
	provider := &fakeToolProvider{
		name:       "mcp",
		tools:      []ToolDefinition{fakeMCPTool("mcp__github__create_issue", false)},
		execResult: "should-not-execute",
	}
	registry.RegisterProvider(provider)

	// 写工具不得被直接执行（Gate3 未接入前 fail-closed），也不得委派给 provider。
	_, err := registry.Execute(ctx, 1, "mcp__github__create_issue", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires approval")
	require.Empty(t, provider.executed)

	// canExecuteWriteTool 只服务内置工具：provider 名一律 false（ToolQueue 不得委派）。
	require.False(t, registry.canExecuteWriteTool("mcp__github__create_issue"))
	require.False(t, registry.canExecuteWriteTool("not_a_tool"))
}

func TestToolRegistry_ProvidersAccessorIsCopy(t *testing.T) {
	registry := NewToolRegistry(nil, nil, nil, nil)
	provider := &fakeToolProvider{name: "mcp"}
	registry.RegisterProvider(provider)

	list := registry.Providers()
	require.Len(t, list, 1)
	list[0] = nil
	require.Len(t, registry.Providers(), 1)
	require.NotNil(t, registry.Providers()[0], "Providers 返回副本，外部改动不得影响注册表")
}
