package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRateLimitMiddlewareSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewRateLimiter(1, time.Minute)
	defer limiter.Stop()

	router := gin.New()
	router.Use(RateLimitMiddleware(limiter))
	router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	retryAfter, err := strconv.Atoi(second.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, retryAfter, 1)
	require.LessOrEqual(t, retryAfter, 60)
}

func TestRequestSizeMiddlewareUploadPathUsesLargerLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestSizeMiddlewareWithUploadLimit(10, 50))
	router.POST("/api/v1/attachments", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/v1/tickets", func(c *gin.Context) { c.Status(http.StatusOK) })

	upload := httptest.NewRequest(http.MethodPost, "/api/v1/attachments", nil)
	upload.ContentLength = 20
	uploadRecorder := httptest.NewRecorder()
	router.ServeHTTP(uploadRecorder, upload)
	require.Equal(t, http.StatusOK, uploadRecorder.Code, "上传路由应使用放宽后的上限")

	normal := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", nil)
	normal.ContentLength = 20
	normalRecorder := httptest.NewRecorder()
	router.ServeHTTP(normalRecorder, normal)
	require.Equal(t, http.StatusBadRequest, normalRecorder.Code, "普通 API 仍应使用 10MB 上限")
}
