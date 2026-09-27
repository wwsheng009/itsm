package transport

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newSSETransport 构造 SDK HTTP+SSE 客户端传输（兼容 2024-11-05 存量服务器）。
func newSSETransport(endpoint string, httpClient *http.Client) mcp.Transport {
	return &mcp.SSEClientTransport{Endpoint: endpoint, HTTPClient: httpClient}
}
