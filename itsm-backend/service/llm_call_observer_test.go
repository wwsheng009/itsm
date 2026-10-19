package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/common/tenantctx"
)

type detailedCallObserver struct {
	mu    sync.Mutex
	calls []LLMCallObservation
}

func (o *detailedCallObserver) Observe(string, string, int, time.Duration, error) {}

func (o *detailedCallObserver) ObserveLLMCall(_ context.Context, call LLMCallObservation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, call)
}

func (o *detailedCallObserver) snapshot() []LLMCallObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]LLMCallObservation(nil), o.calls...)
}

func TestLLMGatewayDetailedObserverCarriesTenantTaskAndError(t *testing.T) {
	ctx := tenantctx.WithTenantID(context.Background(), 42)
	ctx = WithLLMTaskType(ctx, LLMTaskChat)

	observer := &detailedCallObserver{}
	gateway := NewLLMGateway(&MockLLMProvider{
		ShouldError: true,
		ErrorMsg:    "upstream status 503 unavailable",
	}, nil, observer, "openai")

	_, err := gateway.Chat(ctx, "gpt-4o", []LLMMessage{{Role: "user", Content: "hello"}})
	require.Error(t, err)

	calls := observer.snapshot()
	require.Len(t, calls, 1)
	call := calls[0]
	assert.Equal(t, 42, call.TenantID)
	assert.Equal(t, LLMTaskChat, call.TaskType)
	assert.Equal(t, ProviderSourceStatic, call.ProviderSource)
	assert.Equal(t, "openai_chat_completions", call.Provider)
	assert.Equal(t, "gpt-4o", call.Model)
	assert.False(t, call.Success)
	assert.Equal(t, LLMCallErrorServer, call.ErrorClass)
	assert.Equal(t, 503, call.HTTPStatus)
	assert.Contains(t, call.ErrorMessage, "unavailable")
}
