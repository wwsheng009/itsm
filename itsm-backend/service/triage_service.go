package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// guidanceTriageClient defines the interface for Guidance sidecar calls
// This enables dependency injection for testing
type guidanceTriageClient interface {
	Triage(ctx context.Context, title, description string, tenantID int, spec *GuidanceLLMSpec) (*GuidanceTriageResponse, error)
}

// guidanceMethodFallback 是 Guidance sidecar 在 LLM 不可用（未配置 LLM_API_KEY、
// 端点/模型不可达等）时"静默降级"为关键词分类的标记值（itsm-ai-service/api/guidance.py）。
// 该降级以 HTTP 200 返回，调用方无法通过 error 识别，必须显式检查 method 字段。
const guidanceMethodFallback = "guidance-fallback"

// isGuidanceDegraded 判定 sidecar 是否以"降级结果"应答（method="guidance-fallback"）。
// 向后兼容：旧版 sidecar 不返回 method（空串）时按正常结果处理，避免误伤；
// nil 响应（异常态）按降级处理，交由下方网关链路兜底。
func isGuidanceDegraded(resp *GuidanceTriageResponse) bool {
	if resp == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(resp.Method), guidanceMethodFallback)
}

// normalizeResult ensures triage result has valid category, priority, and confidence values
func (t *TriageService) normalizeResult(result TriageResult) TriageResult {
	// Validate and normalize category
	if result.Category == "" || !isValidCategory(result.Category) {
		result.Category = "general"
		// Reset assignee when category is invalid — the original assignee
		// is associated with an unrecognized category and is unreliable.
		result.AssigneeID = 0
	}
	// Validate and normalize priority
	if result.Priority == "" || !isValidPriority(result.Priority) {
		result.Priority = "medium"
	}
	// Validate and normalize confidence
	if result.Confidence == 0 {
		result.Confidence = 0.6
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		result.Confidence = 0.6
	}
	// Set default assignee based on category when not already assigned
	if result.AssigneeID == 0 {
		if assigneeID, ok := t.defaultAssignees[result.Category]; ok {
			result.AssigneeID = assigneeID
		}
	}
	return result
}

// TriageResult holds classification suggestions for incidents/tickets
type TriageResult struct {
	Category     string  `json:"category"`
	Priority     string  `json:"priority"`
	AssigneeID   int     `json:"assigneeId"`
	Confidence   float64 `json:"confidence"`
	Explanation  string  `json:"explanation"`
	SuggestedFix string  `json:"suggestedFix,omitempty"`
}

// TriageService provides LLM-powered ticket triage and classification
type TriageService struct {
	gateway          *LLMGateway
	guidanceClient   guidanceTriageClient
	logger           *zap.Logger
	keywordFallback  bool
	defaultAssignees map[string]int
	// guidanceSpecResolver 下发 sidecar 的租户级 LLM 目标解析器（F2，2026-10-18）。
	// nil = 多 Provider 关闭：sidecar 调用不带请求级目标（旧行为，sidecar 读自身配置）。
	guidanceSpecResolver GuidanceSpecResolver
}

// Default assignee IDs by category
var defaultAssignees = map[string]int{
	"database":    101,
	"network":     102,
	"server":      103,
	"application": 104,
	"security":    100,
	"storage":     105,
	"user_access": 106,
	"general":     0,
}

// NewTriageService creates a new LLM-powered triage service
func NewTriageService(gateway *LLMGateway, logger *zap.Logger) *TriageService {
	return &TriageService{
		gateway:          gateway,
		logger:           logger,
		keywordFallback:  true,
		defaultAssignees: defaultAssignees,
	}
}

// NewTriageServiceWithSugaredLogger creates a new LLM-powered triage service with SugaredLogger
func NewTriageServiceWithSugaredLogger(gateway *LLMGateway, logger *zap.SugaredLogger) *TriageService {
	return &TriageService{
		gateway:          gateway,
		logger:           logger.Desugar(),
		keywordFallback:  true,
		defaultAssignees: defaultAssignees,
	}
}

// NewTriageServiceWithGuidance creates a triage service with Guidance sidecar
func NewTriageServiceWithGuidance(gateway *LLMGateway, guidanceClient *GuidanceClient, logger *zap.Logger) *TriageService {
	return &TriageService{
		gateway:          gateway,
		guidanceClient:   guidanceClient,
		logger:           logger,
		keywordFallback:  true,
		defaultAssignees: defaultAssignees,
	}
}

// NewTriageServiceWithGuidanceAndSugaredLogger creates with SugaredLogger
func NewTriageServiceWithGuidanceAndSugaredLogger(gateway *LLMGateway, guidanceClient *GuidanceClient, logger *zap.SugaredLogger) *TriageService {
	return &TriageService{
		gateway:          gateway,
		guidanceClient:   guidanceClient,
		logger:           logger.Desugar(),
		keywordFallback:  true,
		defaultAssignees: defaultAssignees,
	}
}

// NewTriageServiceWithConfig creates a service with custom configuration
func NewTriageServiceWithConfig(gateway *LLMGateway, logger *zap.Logger, fallback bool, assignees map[string]int) *TriageService {
	svc := &TriageService{
		gateway:          gateway,
		logger:           logger,
		keywordFallback:  fallback,
		defaultAssignees: defaultAssignees,
	}
	if assignees != nil {
		svc.defaultAssignees = assignees
	}
	return svc
}

// SetGuidanceSpecResolver 注入 sidecar LLM 目标解析器（F2，2026-10-18）。
// bootstrap 在多 Provider 开启（应用内配置成为唯一事实源）时接线；不接线 = 旧行为：
// sidecar 调用不带请求级目标，由 sidecar 读自身 env/config.yaml。
func (t *TriageService) SetGuidanceSpecResolver(resolver GuidanceSpecResolver) {
	t.guidanceSpecResolver = resolver
}

// resolveGuidanceSpec 解析本次 sidecar 调用应携带的 LLM 目标。
//
//   - 未接线（多 Provider 关闭）：(nil, true)——不下发目标、继续调用 sidecar（旧行为）；
//   - 解析失败 / 无租户默认 / 实例形态 sidecar 不支持：(nil, false)——跳过 sidecar 直接
//     走网关。应用内配置是唯一事实源，不静默回退 sidecar 自身可能过期的配置。
func (t *TriageService) resolveGuidanceSpec(ctx context.Context, tenantID int) (*GuidanceLLMSpec, bool) {
	if t.guidanceSpecResolver == nil {
		return nil, true
	}
	spec, ok, err := t.guidanceSpecResolver.ResolveGuidanceSpec(ctx, tenantID)
	if err != nil {
		t.logger.Warn("TriageService: resolve sidecar LLM target failed, using LLM gateway",
			zap.Int("tenant_id", tenantID), zap.Error(err))
		return nil, false
	}
	if !ok {
		t.logger.Debug("TriageService: no sidecar-compatible tenant LLM target, using LLM gateway",
			zap.Int("tenant_id", tenantID))
		return nil, false
	}
	return &spec, true
}

// Suggest returns classification suggestions using LLM
func (t *TriageService) Suggest(ctx context.Context, title, description string) TriageResult {
	return t.SuggestForTenant(ctx, title, description, 1)
}

// SuggestForTenant returns classification suggestions scoped to a tenant.
func (t *TriageService) SuggestForTenant(ctx context.Context, title, description string, tenantID int) TriageResult {
	text := strings.ToLower(title + " " + description)

	result := TriageResult{
		Category:    "general",
		Priority:    "medium",
		AssigneeID:  0,
		Confidence:  0.5,
		Explanation: "keyword heuristic",
	}

	// Priority 1: Use Guidance sidecar for constrained generation
	if t.guidanceClient != nil {
		if spec, useSidecar := t.resolveGuidanceSpec(ctx, tenantID); useSidecar {
			guidanceResult, err := t.guidanceClient.Triage(ctx, title, description, tenantID, spec)
			if err != nil {
				t.logger.Warn("TriageService: Guidance sidecar failed, falling back", zap.Error(err))
			} else if isGuidanceDegraded(guidanceResult) {
				// F1（2026-10-18）：sidecar 的 LLM 不可用是"静默降级"——HTTP 200 +
				// method="guidance-fallback"，仅凭 err 无法识别。若直接返回该关键词结果，
				// 会永久屏蔽下方 Priority 2 网关链路（应用级/租户 provider），且不产生
				// ai_llm_calls 遥测。故视同 sidecar 不可用继续回落：网关可用时拿真实 LLM
				// 结果，网关失败仍由下方关键词兜底。
				t.logger.Warn("TriageService: Guidance sidecar degraded to keyword fallback, using LLM gateway",
					zap.String("method", guidanceResult.Method),
					zap.String("explanation", guidanceResult.Explanation),
					zap.Float64("confidence", guidanceResult.Confidence))
			} else {
				t.logger.Info("TriageService: Guidance-constrained classification",
					zap.String("method", guidanceResult.Method),
					zap.String("category", guidanceResult.Category),
					zap.String("priority", guidanceResult.Priority),
					zap.Float64("confidence", guidanceResult.Confidence),
					zap.Float64("latency_ms", guidanceResult.LatencyMs))
				return t.normalizeResult(TriageResult{
					Category:     guidanceResult.Category,
					Priority:     guidanceResult.Priority,
					AssigneeID:   guidanceResult.AssigneeID,
					Confidence:   guidanceResult.Confidence,
					Explanation:  guidanceResult.Explanation,
					SuggestedFix: guidanceResult.SuggestedFix,
				})
			}
		}
	}

	// Priority 2: Use LLM gateway directly
	if t.gateway != nil {
		llmResult, err := t.llmClassify(ctx, title, description)
		if err != nil {
			t.logger.Warn("TriageService: LLM classification failed, using fallback", zap.Error(err))
			if t.keywordFallback {
				return t.keywordBasedSuggest(text)
			}
			return result
		}

		// LLM-first: use LLM result if confidence is acceptable
		if llmResult.Confidence < 0.5 && t.keywordFallback {
			keywordResult := t.keywordBasedSuggest(text)
			if keywordResult.Confidence > llmResult.Confidence {
				t.logger.Info("TriageService: keyword result has higher confidence",
					zap.Float64("llm_confidence", llmResult.Confidence),
					zap.Float64("keyword_confidence", keywordResult.Confidence))
				return keywordResult
			}
		}

		return llmResult
	}

	// Priority 3: Use keyword-based fallback
	if t.keywordFallback {
		return t.keywordBasedSuggest(text)
	}

	return result
}

// llmClassify uses LLM for intelligent ticket classification
// Uses Guidance-style constrained prompting to ensure valid JSON output
func (t *TriageService) llmClassify(ctx context.Context, title, description string) (TriageResult, error) {
	// Guidance-style constrained prompt: explicit schema with strict enum values
	// This guides the model to output valid JSON matching our schema
	prompt := fmt.Sprintf(`You are an expert IT service management triage assistant.
Your task is to classify IT tickets into exactly ONE of these categories:
- database (MySQL, PostgreSQL, MongoDB, Redis, Oracle, SQL issues)
- network (WiFi, router, switch, firewall, connectivity issues)
- server (CPU, memory, disk, Linux, Windows Server, hardware)
- application (software, app, API, deployment, code bugs)
- security (vulnerability, attack, permission, authentication issues)
- storage (disk space, backup, snapshot, storage capacity)
- user_access (account, login, password, access control)
- general (everything else)

Priority levels (choose ONE):
- critical (system down, security breach, data loss)
- high (major feature broken, many users impacted)
- medium (degraded functionality, some users impacted)
- low (minor issue, cosmetic problems)

Title: %s

Description:
%s

IMPORTANT: Respond with ONLY valid JSON in this exact format, no other text:
{"category": "EXACT_CATEGORY_FROM_LIST", "priority": "EXACT_PRIORITY_FROM_LIST", "confidence": 0.0-1.0, "explanation": "brief reason", "suggestedFix": "brief solution or null"}`, title, description)

	messages := []LLMMessage{
		{Role: "system", Content: "You are an IT service management triage assistant. Always output valid JSON."},
		{Role: "user", Content: prompt},
	}

	resp, err := t.gateway.Chat(ctx, "", messages)
	if err != nil {
		return TriageResult{}, fmt.Errorf("LLM classification failed: %w", err)
	}

	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var classification TriageResult
	if err := json.Unmarshal([]byte(resp), &classification); err != nil {
		// Fallback: try to find JSON in response
		start := strings.Index(resp, "{")
		end := strings.LastIndex(resp, "}")
		if start >= 0 && end >= start {
			if err := json.Unmarshal([]byte(resp[start:end+1]), &classification); err != nil {
				return TriageResult{}, fmt.Errorf("failed to parse LLM response: %w", err)
			}
		} else {
			return TriageResult{}, fmt.Errorf("no JSON found in LLM response")
		}
	}

	// LLM assignee IDs are not trustworthy — always reset to category default
	classification.AssigneeID = 0

	// P1-1 fix: Go's case-insensitive JSON decoder does not ignore underscores, so an
	// LLM that emits the snake_case key `suggested_fix` would lose the field. Backfill
	// from the snake_case key when the camelCase field is empty.
	if classification.SuggestedFix == "" {
		var snake struct {
			SuggestedFix string `json:"suggested_fix"`
		}
		_ = json.Unmarshal([]byte(resp), &snake)
		classification.SuggestedFix = snake.SuggestedFix
	}

	// Normalize and validate enum values using shared helper
	return t.normalizeResult(classification), nil
}

// isValidCategory checks if category is a valid enum value
func isValidCategory(cat string) bool {
	validCategories := []string{"database", "network", "server", "application", "security", "storage", "user_access", "general"}
	for _, v := range validCategories {
		if cat == v {
			return true
		}
	}
	return false
}

// isValidPriority checks if priority is a valid enum value
func isValidPriority(pri string) bool {
	validPriorities := []string{"critical", "high", "medium", "low"}
	for _, v := range validPriorities {
		if pri == v {
			return true
		}
	}
	return false
}

// keywordBasedSuggest provides fallback classification using keywords
func (t *TriageService) keywordBasedSuggest(text string) TriageResult {
	result := TriageResult{
		Category:    "general",
		Priority:    "medium",
		AssigneeID:  0,
		Confidence:  0.55,
		Explanation: "keyword heuristic",
	}

	// Category detection based on keywords
	if containsAny(text, "database", "db", "mysql", "postgresql", "oracle", "sql", "mongodb", "redis", "数据库", "慢查询", "连接池") {
		result.Category = "database"
		result.AssigneeID = t.defaultAssignees["database"]
		result.Confidence = 0.7
	} else if containsAny(text, "network", "wifi", "switch", "router", "firewall", "网络", "无法上网", "交换机", "路由器", "防火墙", "丢包") {
		result.Category = "network"
		result.AssigneeID = t.defaultAssignees["network"]
		result.Confidence = 0.68
	} else if containsAny(text, "storage", "disk space", "space", "backup", "snapshot", "存储", "磁盘空间", "空间不足", "备份", "快照", "容量") {
		result.Category = "storage"
		result.AssigneeID = t.defaultAssignees["storage"]
		result.Confidence = 0.66
	} else if containsAny(text, "server", "cpu", "memory", "disk", "linux", "windows server", "服务器", "主机", "内存", "磁盘", "操作系统") {
		result.Category = "server"
		result.AssigneeID = t.defaultAssignees["server"]
		result.Confidence = 0.65
	} else if containsAny(text, "application", "software", "app", "api", "deploy", "应用", "软件", "接口", "部署", "发布", "报错", "服务异常") {
		result.Category = "application"
		result.AssigneeID = t.defaultAssignees["application"]
		result.Confidence = 0.62
	} else if containsAny(text, "security", "vulnerability", "attack", "permission", "authentication", "安全", "漏洞", "攻击", "入侵", "恶意", "认证绕过") {
		result.Category = "security"
		result.AssigneeID = t.defaultAssignees["security"]
		result.Confidence = 0.75
		result.Priority = "critical"
	} else if containsAny(text, "user", "account", "login", "password", "access", "用户", "账号", "账户", "登录", "密码", "权限", "访问") {
		result.Category = "user_access"
		result.AssigneeID = t.defaultAssignees["user_access"]
		result.Confidence = 0.6
	}

	// Priority escalation
	if containsAny(text, "down", "unavailable", "urgent", "critical", "outage", "宕机", "不可用", "紧急", "严重", "中断", "故障") {
		if result.Priority != "critical" {
			result.Priority = "high"
		}
		result.Confidence += 0.1
	}

	if containsAny(text, "impact", "affect", "users", "business", "影响", "多个用户", "大量用户", "所有用户", "全员", "业务") {
		if result.Priority == "medium" {
			result.Priority = "high"
		}
	}

	return result
}

// containsAny checks if text contains any of the given substrings
func containsAny(text string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(text, sub) {
			return true
		}
	}
	return false
}

// BatchSuggest processes multiple tickets for triage suggestions
func (t *TriageService) BatchSuggest(ctx context.Context, tickets []struct {
	ID          int
	Title       string
	Description string
}) ([]struct {
	ID     int
	Result TriageResult
}, error,
) {
	results := make([]struct {
		ID     int
		Result TriageResult
	}, len(tickets))

	for i, ticket := range tickets {
		results[i] = struct {
			ID     int
			Result TriageResult
		}{
			ID:     ticket.ID,
			Result: t.Suggest(ctx, ticket.Title, ticket.Description),
		}
	}

	return results, nil
}

// TriageWithMetadata includes timing and method information
type TriageWithMetadata struct {
	Result  TriageResult  `json:"result"`
	Method  string        `json:"method"`
	Latency time.Duration `json:"latencyMs"`
	Model   string        `json:"model,omitempty"`
}

// SuggestWithMetadata returns triage result with additional metadata
func (t *TriageService) SuggestWithMetadata(ctx context.Context, title, description string) (*TriageWithMetadata, error) {
	start := time.Now()

	text := strings.ToLower(title + " " + description)

	result := &TriageWithMetadata{
		Result: TriageResult{
			Category:    "general",
			Priority:    "medium",
			AssigneeID:  0,
			Confidence:  0.5,
			Explanation: "keyword heuristic",
		},
		Method:  "fallback",
		Latency: time.Since(start),
	}

	if t.gateway == nil {
		if t.keywordFallback {
			result.Result = t.keywordBasedSuggest(text)
			result.Method = "fallback"
		}
		return result, nil
	}

	llmResult, err := t.llmClassify(ctx, title, description)
	if err != nil {
		t.logger.Warn("TriageService: LLM classification failed", zap.Error(err))
		if t.keywordFallback {
			result.Result = t.keywordBasedSuggest(text)
			result.Method = "fallback"
		}
		return result, nil
	}

	result.Result = llmResult
	result.Method = "llm"
	result.Model = "gpt-4o-mini"
	result.Latency = time.Since(start)

	return result, nil
}

// SuggestAssignee suggests an assignee based on ticket content
func (t *TriageService) SuggestAssignee(ctx context.Context, category, title, description string) (int, string, error) {
	if t.gateway != nil {
		prompt := fmt.Sprintf(`Recommend the best handler for this IT ticket:

Category: %s
Title: %s
Description: %s

Choose from:
- Zhang San (101) - Database expert
- Li Si (102) - Network engineer
- Wang Wu (103) - Server admin
- Zhao Liu (104) - Application developer
- Zhou Qi (100) - Security expert
- Chen Ba (105) - Storage expert

Return only the ID number, or 0 if unsure.`, category, title, description)

		messages := []LLMMessage{
			{Role: "system", Content: "You are an IT service management assistant."},
			{Role: "user", Content: prompt},
		}

		resp, err := t.gateway.Chat(ctx, "", messages)
		if err != nil {
			t.logger.Warn("TriageService: assignee suggestion failed", zap.Error(err))
		} else {
			resp = strings.TrimSpace(resp)
			for _, line := range strings.Split(resp, "\n") {
				line = strings.TrimSpace(line)
				if len(line) <= 3 {
					continue
				}
				for i := len(line) - 1; i >= 0; i-- {
					if line[i] < '0' || line[i] > '9' {
						if i < len(line)-1 {
							if id, err := parseAssigneeID(line[i+1:]); err == nil {
								return id, "", nil
							}
						}
						break
					}
				}
			}
		}
	}

	if assigneeID, ok := t.defaultAssignees[category]; ok {
		return assigneeID, "", nil
	}

	return 0, "", nil
}

func parseAssigneeID(s string) (int, error) {
	var id int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			id = id*10 + int(c-'0')
		} else {
			break
		}
	}
	if id == 0 {
		return 0, fmt.Errorf("no valid ID found")
	}
	return id, nil
}
