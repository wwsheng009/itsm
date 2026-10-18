package project

import (
	"context"
	"testing"

	"itsm-backend/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// recordingProvider 记录 gateway 传入的 model，用于断言适配器不再覆盖 provider 模型。
type recordingProvider struct {
	gotModel string
}

func (p *recordingProvider) Chat(ctx context.Context, model string, messages []service.LLMMessage) (string, error) {
	p.gotModel = model
	return "ok", nil
}

// TestProjectAIAdapterDelegatesModelToProvider 回归：项目 AI 必须把模型选择委派给
// 已解析的 provider（传空串），否则静态 LLM_MODEL（gpt-4o-mini）会覆盖租户 DB
// provider 的模型（deepseek-v4.1-flash），导致千帆 Token Plan 403。
func TestProjectAIAdapterDelegatesModelToProvider(t *testing.T) {
	provider := &recordingProvider{}
	gateway := service.NewLLMGateway(provider, nil, nil, "test")
	adapter := NewProjectAIAdapter(gateway, "gpt-4o-mini", zaptest.NewLogger(t).Sugar())

	_, err := adapter.Summarize(
		context.Background(), 0, 0, 1, ReportCadenceWeekly,
		&AIProjectFacts{PromptText: "facts"},
	)
	require.NoError(t, err)
	assert.Equal(t, "", provider.gotModel, "项目 AI 不应把静态模型传入 provider")
	assert.Equal(t, "gpt-4o-mini", adapter.ModelName(), "静态回退时回落到构造参数")
}
