package project

import (
	"context"
	"errors"
)

// PE-25 AI 端口（D-44）：bootstrap 注入 LLM Gateway 适配器（ai_adapter.go）；
// 未装配（nil）是合法状态——三端点按降级矩阵返回确定性内容，绝不 5xx。
//
// 端口只负责「调 LLM + 解析」；事实派生 / 降级决策 / 审计 / 可见性在服务与 HTTP 层。

// ErrAIParseOutput LLM 输出不可解析为约定结构（服务层映射 degradeReason=parse_error）。
var ErrAIParseOutput = errors.New("ai output parse failed")

// ErrAIPortUnavailable 端口未装配（服务层在端口非 nil 时不应收到该错误）。
var ErrAIPortUnavailable = errors.New("project ai port unavailable")

// ProjectAIPort 项目 AI 能力窄接口。
type ProjectAIPort interface {
	// Summarize 生成项目周报/状态摘要文本（事实由 facts 提供，不得杜撰数字）。
	Summarize(ctx context.Context, tenantID, userID, projectID int, cadence string, facts *AIProjectFacts) (string, error)
	// AssessRisks 基于同一事实生成风险预警判断（规则信号由服务层另行派生）。
	AssessRisks(ctx context.Context, tenantID, userID, projectID int, facts *AIProjectFacts) ([]AIRiskWarning, error)
	// ExtractTasks 从会议纪要文本抽取任务建议（只出建议，不落库）。
	ExtractTasks(ctx context.Context, tenantID, userID, projectID int, content string, limit int) ([]AITaskProposal, error)
}

// ProjectAIModel 可选能力：端口报告其模型名（响应 model 字段透出）。
type ProjectAIModel interface {
	ModelName() string
}

// aiModelName 读取端口模型名（未实现 / nil → 空串）。
func aiModelName(port ProjectAIPort) string {
	if named, ok := port.(ProjectAIModel); ok {
		return named.ModelName()
	}
	return ""
}