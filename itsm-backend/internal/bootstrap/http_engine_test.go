package bootstrap

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestNewHTTPEngine_DoesNotDuplicateGinLogs 防止回归：
// gin.Default() 自带 Logger/Recovery，与 router.SetupRoutes 中再次注册的
// gin.Logger()/gin.Recovery() 叠加后，一个真实请求会被打印两条 [GIN] 日志，
// 表现为「前端重复请求」的假象（两条日志耗时通常相差不到 1ms）。
func TestNewHTTPEngine_DoesNotDuplicateGinLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousWriter := gin.DefaultWriter
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })

	var output bytes.Buffer
	gin.DefaultWriter = &output

	// 复刻生产链路：bootstrap 创建引擎 + router 注册全局中间件
	engine := newHTTPEngine()
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())
	engine.GET("/api/v1/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("请求应成功返回 200，实际 %d", recorder.Code)
	}

	if got := strings.Count(output.String(), "[GIN]"); got != 1 {
		t.Fatalf("单个请求应只产生 1 条 [GIN] 日志，实际 %d 条；\n"+
			"请确认 newHTTPEngine() 没有被改回 gin.Default()：\n%s", got, output.String())
	}
}
