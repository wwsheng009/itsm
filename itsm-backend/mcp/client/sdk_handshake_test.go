package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSDKHandshake_StreamableHTTP 是 M0-01 的 P2 SDK 编译/握手 spike：
// 使用官方 SDK（v1.4.0）同时起 Streamable HTTP 服务端与客户端，验证
// initialize（Connect 内含）→ tools/list → tools/call 全链路在本仓库
// go 1.25.13 下可用。
//
// 该用例同时作为 SDK 升级回归守卫：升级 go-sdk 版本后必须保持全绿，
// 否则视为协议/API 破坏性变更，需按 mcp/README.md 的升级策略处理。
func TestSDKHandshake_StreamableHTTP(t *testing.T) {
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "itsm-mcp-spike", Version: "v0.0.1"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "spike: echo back the message",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + args.Message}},
		}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "itsm-backend-spike", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("connect（initialize 握手）失败: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list 失败: %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tools/list 未返回 echo 工具: %+v", tools.Tools)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "itsm"},
	})
	if err != nil {
		t.Fatalf("tools/call 失败: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("tools/call 返回内容数量异常: %d", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "echo:itsm" {
		t.Fatalf("tools/call 返回内容异常: %+v", res.Content[0])
	}
}
