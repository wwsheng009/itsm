package ai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLLMCallFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet,
		"/api/v1/ai/llm-calls?days=14&success=false&page=2&pageSize=50&providerKey=p1&model=gpt-4o&taskType=chat&errorClass=rate_limited",
		nil)

	filter, message := parseLLMCallFilter(c, true)
	require.Empty(t, message)
	assert.Equal(t, 14, filter.Days)
	require.NotNil(t, filter.Success)
	assert.False(t, *filter.Success)
	assert.Equal(t, 2, filter.Page)
	assert.Equal(t, 50, filter.PageSize)
	assert.Equal(t, "p1", filter.ProviderKey)
	assert.Equal(t, "gpt-4o", filter.Model)
	assert.Equal(t, "chat", filter.TaskType)
	assert.Equal(t, "rate_limited", filter.ErrorClass)
}

func TestParseLLMCallFilterRejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []string{
		"/api/v1/ai/llm-calls?days=0",
		"/api/v1/ai/llm-calls?success=maybe",
		"/api/v1/ai/llm-calls?page=0",
		"/api/v1/ai/llm-calls?pageSize=101",
	}
	for _, target := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		_, message := parseLLMCallFilter(c, true)
		assert.NotEmpty(t, message, target)
	}
}

func TestGetLLMCallsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai/llm-calls", nil)
	c.Set("tenant_id", 1)

	h := &Handler{}
	h.GetLLMCalls(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
