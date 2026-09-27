package client

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool 是 SDK Tool 的 ITSM 投影：原始名 + 描述 + 原始 inputSchema（原样保存，不重写）。
type Tool struct {
	RawName     string
	Description string
	InputSchema json.RawMessage
}

// Content 是调用结果内容投影：文本直出；非文本保留原始 JSON（字节上限由上层控制）。
type Content struct {
	Type string // text | image | audio | resource_link | resource | ...
	Text string
	MIME string
	Raw  json.RawMessage
}

// CallResult 是 tools/call 结果投影。
type CallResult struct {
	Content           []Content
	StructuredContent json.RawMessage
	IsError           bool
}

// Session 是已握手会话（单连接；由 manager 持有并在 refresh 时替换）。
type Session struct {
	inner           *mcp.ClientSession
	observer        Observer
	timeouts        Timeouts
	cancel          context.CancelFunc
	protocolVersion string
	serverName      string
	serverVersion   string
}

// ProtocolVersion 返回协商后的 MCP 协议版本。
func (s *Session) ProtocolVersion() string { return s.protocolVersion }

// ServerName 返回服务器自称名称（serverInfo.name）。
func (s *Session) ServerName() string { return s.serverName }

// ServerVersion 返回服务器版本（serverInfo.version）。
func (s *Session) ServerVersion() string { return s.serverVersion }

// ID 返回传输层会话 ID（streamable HTTP 特有；SSE/内存传输可能为空）。
func (s *Session) ID() string { return s.inner.ID() }

// ListTools 拉取工具列表（调用超时默认 30s；inputSchema 原样保存）。
func (s *Session) ListTools(ctx context.Context) ([]Tool, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.timeouts.call())
	defer cancel()

	result, err := s.inner.ListTools(callCtx, nil)
	if err != nil {
		return nil, wrapError(err, "tools/list")
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, item := range result.Tools {
		if item == nil {
			continue
		}
		tools = append(tools, Tool{
			RawName:     item.Name,
			Description: item.Description,
			InputSchema: marshalRaw(item.InputSchema),
		})
	}
	return tools, nil
}

// CallTool 调用工具（调用超时默认 30s）。
// 结果内容按**不可信**处理：上层不得执行其中指令，字节上限与脱敏由 provider 负责。
func (s *Session) CallTool(ctx context.Context, name string, args map[string]any) (*CallResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.timeouts.call())
	defer cancel()

	result, err := s.inner.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, wrapError(err, "tools/call")
	}
	return fromSDKCallResult(result), nil
}

// Ping 健康检查（调用超时默认 30s）。
func (s *Session) Ping(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, s.timeouts.call())
	defer cancel()

	if err := s.inner.Ping(callCtx, nil); err != nil {
		return wrapError(err, "ping")
	}
	return nil
}

// Close 关闭会话；成功后触发 disconnected 事件（幂等）。
func (s *Session) Close() error {
	if s.cancel != nil {
		defer s.cancel()
	}
	if err := s.inner.Close(); err != nil {
		return wrapError(err, "close")
	}
	if s.observer != nil {
		s.observer.OnMCPEvent(Event{Type: EventDisconnected})
	}
	return nil
}

func fromSDKCallResult(result *mcp.CallToolResult) *CallResult {
	if result == nil {
		return &CallResult{}
	}
	out := &CallResult{
		IsError:           result.IsError,
		StructuredContent: marshalRaw(result.StructuredContent),
	}
	out.Content = make([]Content, 0, len(result.Content))
	for _, item := range result.Content {
		out.Content = append(out.Content, fromSDKContent(item))
	}
	return out
}

func fromSDKContent(item mcp.Content) Content {
	if text, ok := item.(*mcp.TextContent); ok {
		return Content{Type: "text", Text: text.Text}
	}
	raw := marshalRaw(item)
	var meta struct {
		Type     string `json:"type"`
		MIMEType string `json:"mimeType"`
	}
	_ = json.Unmarshal(raw, &meta)
	return Content{Type: meta.Type, MIME: meta.MIMEType, Raw: raw}
}

func marshalRaw(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}
