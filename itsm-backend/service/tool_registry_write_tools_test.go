package service

import (
	"testing"
)

// TestToolRegistryWriteToolDefinitions 验证写工具（create_ticket/update_ticket/create_ticket_type/
// link_ticket_ci/create_ci_relationship/delete_ci_relationship）已正确注册为需审批的写操作
// （ReadOnly=false，且拥有独立的 resource/action）。
// 这是 AI 写路径打通的基础：聊天 LLM 与 agent 路径都依赖这些定义判断 RBAC 与审批流。
func TestToolRegistryWriteToolDefinitions(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	tools := reg.ListTools()

	byName := make(map[string]ToolDefinition, len(tools))
	for _, td := range tools {
		byName[td.Name] = td
	}

	cases := []struct {
		name     string
		resource string
		action   string
	}{
		{"create_ticket", "ticket", "write"},
		{"update_ticket", "ticket", "write"},
		{"create_ticket_type", "ticket_type", "write"},
		{"link_ticket_ci", "cmdb", "write"},
		{"create_ci_relationship", "cmdb", "write"},
		{"delete_ci_relationship", "cmdb", "write"},
	}

	for _, c := range cases {
		td, ok := byName[c.name]
		if !ok {
			t.Fatalf("tool %s 未注册", c.name)
		}
		if td.ReadOnly {
			t.Errorf("tool %s 应为写操作(ReadOnly=false)，实际 ReadOnly=true", c.name)
		}
		if td.Resource != c.resource {
			t.Errorf("tool %s resource 期望 %s，实际 %s", c.name, c.resource, td.Resource)
		}
		if td.Action != c.action {
			t.Errorf("tool %s action 期望 %s，实际 %s", c.name, c.action, td.Action)
		}
	}

	// 只读工具不应被误标为写操作
	for _, ro := range []string{"get_incident_stats", "list_kb", "list_tickets", "list_cis"} {
		td, ok := byName[ro]
		if !ok {
			t.Fatalf("只读工具 %s 未注册", ro)
		}
		if !td.ReadOnly {
			t.Errorf("只读工具 %s 被错误标记为写操作", ro)
		}
	}
}

// TestCreateTicketArgsSchemaExposesExecutableFields 锁定 create_ticket 的模型可见参数
// 与执行层契约一致：执行层读取 title/description/priority/type/ci_id 等，若 schema 不声明，
// LLM 永远看不到这些参数 → 审批通过后必然失败于 "title is required"（2026-10-18 浏览器实测）。
func TestCreateTicketArgsSchemaExposesExecutableFields(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	var createTicket *ToolDefinition
	for _, td := range reg.ListTools() {
		if td.Name == "create_ticket" {
			d := td
			createTicket = &d
			break
		}
	}
	if createTicket == nil {
		t.Fatal("create_ticket 未注册")
	}

	props, ok := createTicket.ArgsSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("create_ticket ArgsSchema.properties 缺失或类型错误: %#v", createTicket.ArgsSchema)
	}
	for _, field := range []string{"title", "description", "priority", "type", "ci_id"} {
		if _, exists := props[field]; !exists {
			t.Errorf("create_ticket ArgsSchema 未声明执行层支持的参数 %q", field)
		}
	}

	required, ok := createTicket.ArgsSchema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "title" {
		t.Errorf("create_ticket ArgsSchema.required 期望 [title]，实际 %#v", createTicket.ArgsSchema["required"])
	}

	title, ok := props["title"].(map[string]interface{})
	if !ok || title["type"] != "string" {
		t.Errorf("create_ticket title 参数应为 string，实际 %#v", props["title"])
	}
}
