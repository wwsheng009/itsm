package transport

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newStreamableTransport 构造 SDK Streamable HTTP 客户端传输。
func newStreamableTransport(endpoint string, httpClient *http.Client) mcp.Transport {
	return &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient}
}
