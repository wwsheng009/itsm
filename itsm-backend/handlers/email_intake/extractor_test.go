package email_intake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"itsm-backend/service"
)

type fakeEmailLLM struct {
	output string
	err    error
}

type sequenceEmailLLM struct {
	outputs []string
	index   int
}

type flakyEmailLLM struct {
	calls int
}

func (f *flakyEmailLLM) Chat(context.Context, string, []service.LLMMessage) (string, error) {
	f.calls++
	if f.calls == 1 {
		return "", context.DeadlineExceeded
	}
	return `{"intent":"report_incident","customerName":"客户A","reportedContractNumber":"SUP-RETRY","title":"线路中断","description":"down","impact":"high","urgency":"high","missingFields":[],"confidence":0.95}`, nil
}

func (f *sequenceEmailLLM) Chat(context.Context, string, []service.LLMMessage) (string, error) {
	output := f.outputs[f.index]
	if f.index < len(f.outputs)-1 {
		f.index++
	}
	return output, nil
}

func (f fakeEmailLLM) Chat(context.Context, string, []service.LLMMessage) (string, error) {
	return f.output, f.err
}

// capturingEmailLLM 记录网关收到的 model 参数，用于锁定「不得下发静态模型」。
type capturingEmailLLM struct {
	model string
}

func (f *capturingEmailLLM) Chat(_ context.Context, model string, _ []service.LLMMessage) (string, error) {
	f.model = model
	return `{"intent":"other","confidence":0.9}`, nil
}

// TestEmailIntakeExtractorDelegatesModelToGateway 锁定回归（2026-10-18 项目 AI 同类根因）：
// 构造时即使给了静态 LLM_MODEL（gpt-4o-mini），提取器也必须传空模型，由 provider 决定；
// 显式传静态模型会在 Token Plan 类网关（千帆）触发 403 覆盖失败。
func TestEmailIntakeExtractorDelegatesModelToGateway(t *testing.T) {
	gateway := &capturingEmailLLM{}
	extractor := NewEmailIntakeExtractor(gateway, "gpt-4o-mini")

	_, _, err := extractor.Extract(context.Background(), "线路中断", "MPLS down")
	require.NoError(t, err)
	require.Equal(t, "", gateway.model, "不得把静态 LLM_MODEL 传给网关覆盖 provider 模型")
}

func TestEmailIntakeExtractorValidatesStructuredOutput(t *testing.T) {
	extractor := NewEmailIntakeExtractor(fakeEmailLLM{output: `{"intent":"report_incident","customerName":"上海ABC有限公司","reportedContractNumber":"SUP-1","title":"线路中断","description":"MPLS down","impact":"high","urgency":"high","missingFields":[],"confidence":0.96}`}, "test-model")
	result, raw, err := extractor.Extract(context.Background(), "报障", "邮件正文")
	require.NoError(t, err)
	require.Contains(t, raw, "report_incident")
	require.Equal(t, "上海ABC有限公司", result.CustomerName)
	require.Equal(t, 0.96, result.Confidence)
}

func TestEmailIntakeExtractorRejectsInvalidEnumsAndConfidence(t *testing.T) {
	extractor := NewEmailIntakeExtractor(fakeEmailLLM{output: `{"intent":"delete_database","customerName":"A","reportedContractNumber":"1","title":"x","description":"x","impact":"extreme","urgency":"high","confidence":2}`}, "test-model")
	_, _, err := extractor.Extract(context.Background(), "x", "x")
	require.Error(t, err)
}

func TestEmailIntakeExtractorRejectsTrailingOutput(t *testing.T) {
	extractor := NewEmailIntakeExtractor(fakeEmailLLM{output: `{"intent":"other","confidence":0.9} {"unexpected":true}`}, "test-model")
	_, _, err := extractor.Extract(context.Background(), "x", "x")
	require.Error(t, err)
}
