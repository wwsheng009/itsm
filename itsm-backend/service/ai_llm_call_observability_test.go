package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyLLMCallError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantClass  string
		wantStatus int
	}{
		{name: "nil", err: nil, wantClass: "", wantStatus: 0},
		{name: "local rate limit", err: ErrRateLimited, wantClass: LLMCallErrorRateLimited, wantStatus: 429},
		{name: "cancel", err: context.Canceled, wantClass: LLMCallErrorCancelled},
		{name: "timeout", err: context.DeadlineExceeded, wantClass: LLMCallErrorTimeout},
		{name: "auth", err: &openai.APIError{HTTPStatusCode: 401, Message: "invalid key"}, wantClass: LLMCallErrorAuth, wantStatus: 401},
		{name: "server", err: &openai.APIError{HTTPStatusCode: 503, Message: "unavailable"}, wantClass: LLMCallErrorServer, wantStatus: 503},
		{name: "text status", err: errors.New("request failed: status 429 too many requests"), wantClass: LLMCallErrorRateLimited, wantStatus: 429},
		{name: "unknown", err: errors.New("decode response failed"), wantClass: LLMCallErrorUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, status := ClassifyLLMCallError(tc.err)
			assert.Equal(t, tc.wantClass, class)
			assert.Equal(t, tc.wantStatus, status)
		})
	}
}

func TestSanitizeLLMCallErrorAndTaskType(t *testing.T) {
	secret := "sk-live-abcdefghijklmnop"
	message := fmt.Sprintf("provider error: %s Bearer abc.def api_key=plain-secret", secret)
	sanitized := SanitizeLLMCallError(errors.New(message))
	assert.NotContains(t, sanitized, secret)
	assert.NotContains(t, sanitized, "abc.def")
	assert.NotContains(t, sanitized, "plain-secret")
	assert.Contains(t, sanitized, "****")

	long := SanitizeLLMCallError(errors.New(strings.Repeat("错", 600)))
	assert.LessOrEqual(t, len([]rune(long)), 513)
	assert.True(t, strings.HasSuffix(long, "…"))

	assert.Equal(t, LLMTaskUnknown, NormalizeLLMTaskType(""))
	assert.Equal(t, LLMTaskUnknown, NormalizeLLMTaskType("Chat 页面"))
	assert.Equal(t, "chat.v2", NormalizeLLMTaskType(" Chat.V2 "))
}

func TestLLMCallRepositoryListAndSummaryTenantScoped(t *testing.T) {
	_, svc := newTelemetryTestDB(t)
	ctx := context.Background()
	require.NotNil(t, svc.repo)

	calls := []LLMCallObservation{
		{
			TenantID: 1, ProviderKey: "p1", Provider: "openai_chat_completions",
			ProviderSource: ProviderSourceTenant, Model: "gpt-4o-mini", TaskType: LLMTaskChat,
			Tokens: 100, Latency: 200 * time.Millisecond, Success: true,
		},
		{
			TenantID: 1, ProviderKey: "p1", Provider: "openai_chat_completions",
			ProviderSource: ProviderSourceTenant, Model: "gpt-4o", TaskType: LLMTaskChat,
			Tokens: 200, Latency: 500 * time.Millisecond, Success: false,
			ErrorClass: LLMCallErrorRateLimited, HTTPStatus: 429,
			ErrorMessage: "upstream 429 rate limited",
		},
		{
			TenantID: 2, ProviderKey: "p2", Provider: "anthropic_messages",
			ProviderSource: ProviderSourceTenant, Model: "claude", TaskType: LLMTaskDynamicSuggestion,
			Tokens: 50, Latency: 100 * time.Millisecond, Success: true,
		},
	}
	for _, call := range calls {
		require.NoError(t, svc.repo.ObserveLLMCallDetailed(ctx, call))
	}

	list, err := svc.ListLLMCalls(ctx, 1, LLMCallFilter{Days: 7})
	require.NoError(t, err)
	require.Len(t, list.Items, 2, "必须只返回当前租户调用")
	assert.Equal(t, 2, list.Total)
	assert.Equal(t, "p1", list.Items[0].ProviderKey)
	assert.True(t, list.Items[0].TokensEstimated)

	failed := false
	failedList, err := svc.ListLLMCalls(ctx, 1, LLMCallFilter{Days: 7, Success: &failed})
	require.NoError(t, err)
	require.Len(t, failedList.Items, 1)
	assert.Equal(t, LLMCallErrorRateLimited, failedList.Items[0].ErrorClass)
	assert.Equal(t, 429, failedList.Items[0].HTTPStatus)

	summary, err := svc.SummarizeLLMCalls(ctx, 1, LLMCallFilter{Days: 7})
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Total)
	assert.Equal(t, 1, summary.Success)
	assert.Equal(t, 1, summary.Failed)
	assert.InDelta(t, 0.5, summary.SuccessRate, 1e-9)
	assert.InDelta(t, 350, summary.AvgLatencyMs, 1e-9)
	assert.Equal(t, int64(300), summary.Tokens)
	assert.True(t, summary.TokensEstimated)

	require.Len(t, summary.ByErrorClass, 1)
	assert.Equal(t, LLMCallErrorRateLimited, summary.ByErrorClass[0].Key)
	assert.Equal(t, 1, summary.ByErrorClass[0].Count)
	require.Len(t, summary.ByProvider, 1)
	assert.Equal(t, "p1", summary.ByProvider[0].Key)
	assert.Equal(t, 2, summary.ByProvider[0].Count)
	require.Len(t, summary.ByTask, 1)
	assert.Equal(t, LLMTaskChat, summary.ByTask[0].Key)
	assert.Equal(t, 2, summary.ByTask[0].Count)

	page, err := svc.ListLLMCalls(ctx, 1, LLMCallFilter{Days: 7, Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, 2, page.Total)
}
