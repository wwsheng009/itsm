package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/internal/assistantcontext"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// P2 LLM 动态建议（缓存 + 审计）。
//
// 口径：
//   - **生成**：按页面声明（title/scene/entrypoint/现有任务与问题卡文案）下发严格 JSON 约束提示词，
//     仅允许只读任务卡/问题卡（readOnly 强制 true、写动作不进入建议区——§D4 不变量）；
//   - **缓存**：进程内 TTL（默认 5m）+ 容量上限；键 = tenant|pageKey|role|route；
//     命中缓存直接返回（Cached=true），**不重复调用 LLM、不重复审计**；
//   - **审计**：每次真正生成（成功/失败）写一条 `ai_feedback(item_type=ai_audit)`，
//     scenario=`assistant_dynamic_suggestion`（经 SaveFeedback 落库，沿用 /ai/audit-logs 与评估口径）；
//   - **fail-safe**：生成/解析失败 → Generated=false + Reason，**前端保持静态基线**（动态建议只是增强）；
//   - 并发单飞：同键并发请求只触发一次 LLM 调用（其余等待后读缓存）。
const (
	DynamicSuggestionsDefaultTTL        = 5 * time.Minute
	DynamicSuggestionsDefaultMaxEntries = 200
	DynamicSuggestionsMaxItems          = 3
	DynamicSuggestionsMaxLabelRunes     = 60
	DynamicSuggestionsMaxPromptRunes    = 500
	DynamicSuggestionsTimeout           = 20 * time.Second
)

// DynamicSuggestion 一条动态建议（与静态注册表建议同构 + source 标注）。
type DynamicSuggestion struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // task | question
	Label    string `json:"label"`
	Prompt   string `json:"prompt"`
	ReadOnly bool   `json:"readOnly"`
	Source   string `json:"source"` // llm
}

// DynamicSuggestionRequest 生成请求（页面声明 + 身份上下文）。
type DynamicSuggestionRequest struct {
	TenantID   int
	UserID     int
	PageKey    string
	Route      string
	Role       string
	Title      string
	Scene      string
	Entrypoint string
	Tasks      []string
	Questions  []string
}

// DynamicSuggestionResult 下发结果（Known 由 handler 依据注册表补齐）。
type DynamicSuggestionResult struct {
	PageKey     string              `json:"pageKey"`
	Known       bool                `json:"known"`
	Generated   bool                `json:"generated"`
	Cached      bool                `json:"cached"`
	Model       string              `json:"model,omitempty"`
	Suggestions []DynamicSuggestion `json:"suggestions"`
	Reason      string              `json:"reason,omitempty"`
}

// DynamicSuggestionGenerator 生成原始 LLM 输出（接口化：测试注入 fake；生产由 LLMGateway 实现）。
type DynamicSuggestionGenerator interface {
	Generate(ctx context.Context, req DynamicSuggestionRequest) (model string, output string, err error)
}

// DynamicSuggestionAuditEntry 一次生成尝试的审计条目。
type DynamicSuggestionAuditEntry struct {
	TenantID  int
	UserID    int
	PageKey   string
	Model     string
	Generated bool
	Count     int
	Reason    string
}

// DynamicSuggestionAuditSink 审计落点（best-effort；失败仅记录日志，不影响主流程）。
type DynamicSuggestionAuditSink interface {
	Record(ctx context.Context, entry DynamicSuggestionAuditEntry) error
}

type dynamicCacheEntry struct {
	result    *DynamicSuggestionResult
	expiresAt time.Time
}

// DynamicSuggestionService 缓存 + 单飞 + 审计编排（生成实现由 generator 注入）。
type DynamicSuggestionService struct {
	generator DynamicSuggestionGenerator
	audit     DynamicSuggestionAuditSink
	logger    *zap.SugaredLogger

	ttl        time.Duration
	maxEntries int
	now        func() time.Time

	mu       sync.Mutex
	cache    map[string]dynamicCacheEntry
	inflight map[string]chan struct{}
}

// NewDynamicSuggestionService 构造服务；generator 为 nil 时服务不可用（调用方 503 降级）。
func NewDynamicSuggestionService(generator DynamicSuggestionGenerator, audit DynamicSuggestionAuditSink, logger *zap.SugaredLogger) *DynamicSuggestionService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &DynamicSuggestionService{
		generator:  generator,
		audit:      audit,
		logger:     logger,
		ttl:        DynamicSuggestionsDefaultTTL,
		maxEntries: DynamicSuggestionsDefaultMaxEntries,
		now:        time.Now,
		cache:      make(map[string]dynamicCacheEntry),
		inflight:   make(map[string]chan struct{}),
	}
}

// cacheKey 键 = tenant|pageKey|role|route（角色/路由不同 → 建议不同）。
func dynamicCacheKey(req DynamicSuggestionRequest) string {
	return fmt.Sprintf("%d|%s|%s|%s", req.TenantID, req.PageKey, req.Role, strings.TrimSpace(req.Route))
}

// Available 是否可用（用于端点降级判定）。
func (s *DynamicSuggestionService) Available() bool {
	return s != nil && s.generator != nil
}

// Get 读取（或生成）动态建议；失败按 fail-safe 返回带 Reason 的空建议。
func (s *DynamicSuggestionService) Get(ctx context.Context, req DynamicSuggestionRequest) *DynamicSuggestionResult {
	base := &DynamicSuggestionResult{
		PageKey:     req.PageKey,
		Suggestions: []DynamicSuggestion{},
	}
	if !s.Available() {
		base.Reason = "disabled"
		return base
	}

	key := dynamicCacheKey(req)
	if cached, ok := s.lookupCache(key); ok {
		hit := *cached
		hit.Cached = true
		return &hit
	}

	// 单飞：同键已有生成在进行 → 等待后复查缓存。
	s.mu.Lock()
	if ch, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			base.Reason = "context_cancelled"
			return base
		}
		if cached, ok := s.lookupCache(key); ok {
			hit := *cached
			hit.Cached = true
			return &hit
		}
		base.Reason = "generation_failed"
		return base
	}
	gate := make(chan struct{})
	s.inflight[key] = gate
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(gate)
	}()

	return s.generateAndCache(ctx, req, base)
}

func (s *DynamicSuggestionService) lookupCache(key string) (*DynamicSuggestionResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	if s.now().After(entry.expiresAt) {
		delete(s.cache, key)
		return nil, false
	}
	return entry.result, true
}

func (s *DynamicSuggestionService) storeCache(key string, result *DynamicSuggestionResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= s.maxEntries {
		// 容量上限：淘汰最早过期的一项（近似 LRU；P2 规模下足够）。
		oldestKey := ""
		var oldest time.Time
		for k, entry := range s.cache {
			if oldestKey == "" || entry.expiresAt.Before(oldest) {
				oldestKey, oldest = k, entry.expiresAt
			}
		}
		if oldestKey != "" {
			delete(s.cache, oldestKey)
		}
	}
	s.cache[key] = dynamicCacheEntry{result: result, expiresAt: s.now().Add(s.ttl)}
}

// generateAndCache 调用生成器 → 解析校验 → 缓存 + 审计（成功/失败都审计）。
func (s *DynamicSuggestionService) generateAndCache(ctx context.Context, req DynamicSuggestionRequest, base *DynamicSuggestionResult) *DynamicSuggestionResult {
	model, output, err := s.generator.Generate(ctx, req)
	base.Model = model
	if err != nil {
		base.Generated = false
		base.Reason = "llm_unavailable"
		s.recordAudit(ctx, req, DynamicSuggestionAuditEntry{
			TenantID: req.TenantID, UserID: req.UserID, PageKey: req.PageKey,
			Model: model, Generated: false, Count: 0, Reason: "llm_unavailable",
		})
		s.logger.Warnw("dynamic suggestions: llm call failed", "error", err, "pageKey", req.PageKey)
		return base
	}

	items, parseErr := parseDynamicSuggestions(output)
	if parseErr != nil || len(items) == 0 {
		base.Generated = false
		base.Reason = "parse_failed"
		s.recordAudit(ctx, req, DynamicSuggestionAuditEntry{
			TenantID: req.TenantID, UserID: req.UserID, PageKey: req.PageKey,
			Model: model, Generated: false, Count: 0, Reason: "parse_failed",
		})
		s.logger.Warnw("dynamic suggestions: parse failed", "error", parseErr, "pageKey", req.PageKey)
		return base
	}

	base.Generated = true
	base.Suggestions = items
	s.storeCache(dynamicCacheKey(req), base)
	s.recordAudit(ctx, req, DynamicSuggestionAuditEntry{
		TenantID: req.TenantID, UserID: req.UserID, PageKey: req.PageKey,
		Model: model, Generated: true, Count: len(items), Reason: "",
	})
	return base
}

func (s *DynamicSuggestionService) recordAudit(ctx context.Context, _ DynamicSuggestionRequest, entry DynamicSuggestionAuditEntry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		s.logger.Warnw("dynamic suggestions: audit failed", "error", err, "pageKey", entry.PageKey)
	}
}

// —— 解析与校验（纯函数） ——

type rawDynamicSuggestion struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Prompt string `json:"prompt"`
}

// parseDynamicSuggestions 从 LLM 输出中提取 JSON 数组（容忍 markdown 代码围栏/前后噪声），
// 校验并收敛为最多 DynamicSuggestionsMaxItems 条只读建议。
func parseDynamicSuggestions(output string) ([]DynamicSuggestion, error) {
	text := strings.TrimSpace(output)
	if text == "" {
		return nil, fmt.Errorf("empty output")
	}
	if strings.HasPrefix(text, "```") {
		// 去掉首行围栏与结尾围栏。
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("json array not found")
	}

	var raw []rawDynamicSuggestion
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("decode suggestions: %w", err)
	}

	out := make([]DynamicSuggestion, 0, DynamicSuggestionsMaxItems)
	for index, item := range raw {
		if len(out) >= DynamicSuggestionsMaxItems {
			break
		}
		kind := strings.TrimSpace(strings.ToLower(item.Kind))
		if kind != "task" && kind != "question" {
			kind = "task"
		}
		label := truncateRunes(strings.TrimSpace(item.Label), DynamicSuggestionsMaxLabelRunes)
		prompt := truncateRunes(strings.TrimSpace(item.Prompt), DynamicSuggestionsMaxPromptRunes)
		if label == "" || prompt == "" {
			continue
		}
		out = append(out, DynamicSuggestion{
			ID:       fmt.Sprintf("dyn:%d", index),
			Kind:     kind,
			Label:    label,
			Prompt:   prompt,
			ReadOnly: true, // §D4：建议区只读（写动作一律走确认闭环）
			Source:   "llm",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid suggestions")
	}
	return out, nil
}

// —— 生产装配：LLM 生成器 + 审计落点 ——

// llmDynamicSuggestionGenerator 经 LLMGateway（§3.3 解析链）生成动态建议。
type llmDynamicSuggestionGenerator struct {
	gateway *service.LLMGateway
	logger  *zap.SugaredLogger
}

// NewLLMDynamicSuggestionGenerator 构造生成器；gateway 为 nil 时返回 nil（服务不可用）。
func NewLLMDynamicSuggestionGenerator(gateway *service.LLMGateway, logger *zap.SugaredLogger) DynamicSuggestionGenerator {
	if gateway == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &llmDynamicSuggestionGenerator{gateway: gateway, logger: logger}
}

const dynamicSuggestionSystemPrompt = `你是 ITSM 系统的页面助手。请只输出一个 JSON 数组（不要任何解释文字），最多 3 条建议。
每条：{"kind":"task"|"question","label":"≤20字中文短句","prompt":"用户点击时发送给助手的中文提示词（可执行、只读、不包含写操作/提交动作）"}。
要求：task 是零输入可执行的只读动作（查询/分析/草稿），question 是了解型提问；禁止任何写操作或自动提交。`

func (g *llmDynamicSuggestionGenerator) Generate(ctx context.Context, req DynamicSuggestionRequest) (string, string, error) {
	providerReq := service.ProviderRequest{TenantID: req.TenantID, UserID: req.UserID}
	resolution, err := g.gateway.ResolveRequest(ctx, providerReq)
	if err != nil {
		return "", "", fmt.Errorf("resolve provider: %w", err)
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("页面：%s（pageKey=%s）\n", req.Title, req.PageKey))
	if req.Scene != "" {
		builder.WriteString(fmt.Sprintf("场景 Bot：%s\n", req.Scene))
	}
	if req.Entrypoint != "" {
		builder.WriteString(fmt.Sprintf("入口：%s\n", req.Entrypoint))
	}
	if len(req.Tasks) > 0 {
		builder.WriteString(fmt.Sprintf("现有任务卡：%s\n", strings.Join(req.Tasks, "；")))
	}
	if len(req.Questions) > 0 {
		builder.WriteString(fmt.Sprintf("现有问题卡：%s\n", strings.Join(req.Questions, "；")))
	}
	builder.WriteString("请给出与该页面当前工作最相关、且与现有卡片不重复的建议。")

	callCtx, cancel := context.WithTimeout(ctx, DynamicSuggestionsTimeout)
	defer cancel()
	callCtx = service.WithLLMTaskType(callCtx, service.LLMTaskDynamicSuggestion)
	output, _, err := g.gateway.ChatWithRequest(callCtx, providerReq, resolution.Model, []service.LLMMessage{
		{Role: "system", Content: dynamicSuggestionSystemPrompt},
		{Role: "user", Content: builder.String()},
	})
	if err != nil {
		return resolution.Model, "", err
	}
	return resolution.Model, output, nil
}

// feedbackDynamicSuggestionAuditSink 审计落点：复用 SaveFeedback（ai_feedbacks / item_type=ai_audit），
// 与 /ai/audit-logs、/ai/evaluation 同一数据面（scenario=assistant_dynamic_suggestion）。
type feedbackDynamicSuggestionAuditSink struct {
	svc *Service
}

// DynamicSuggestionAuditSink 返回审计落点（供 bootstrap 装配）。
func (s *Service) DynamicSuggestionAuditSink() DynamicSuggestionAuditSink {
	if s == nil {
		return nil
	}
	return &feedbackDynamicSuggestionAuditSink{svc: s}
}

func (s *feedbackDynamicSuggestionAuditSink) Record(ctx context.Context, entry DynamicSuggestionAuditEntry) error {
	if s == nil || s.svc == nil {
		return nil
	}
	score := dynamicSuggestionAuditScore(entry.Generated, entry.Count)
	note, _ := json.Marshal(map[string]interface{}{
		"reason": entry.Reason,
		"count":  entry.Count,
		"model":  entry.Model,
	})
	noteStr := string(note)
	requestID := fmt.Sprintf("dyn_suggest_%d_%d", entry.TenantID, time.Now().UnixNano())
	return s.svc.SaveFeedback(ctx, entry.TenantID, entry.UserID, requestID,
		"assistant_dynamic_suggestion", entry.PageKey, "ai_audit", nil, entry.Generated, &score, &noteStr)
}

// dynamicSuggestionAuditScore 审计信心分代理：生成成功按条数给分（1 条=50 / 3 条=70），失败 0。
//
// 说明：LLM 未回带置信度，此处为**代理口径**（用于 /ai/evaluation 的 avgConfidence 参考），
// 不代表建议质量；质量由 suggestion_clicked/ignored 埋点闭环衡量。
func dynamicSuggestionAuditScore(generated bool, count int) int {
	if !generated {
		return 0
	}
	score := 40 + 10*count
	if score > 100 {
		score = 100
	}
	return score
}

// —— 端点 ——

// GetDynamicSuggestions handles GET /api/v1/ai/assistant/suggestions/dynamic?pageKey=&route=
//
// P2：LLM 动态建议（缓存 + 审计）。未知 pageKey → known=false（前端保持静态基线）；
// 服务未装配 → 503；生成失败 → 200 + generated=false + reason（fail-safe，不阻断建议区）。
func (h *Handler) GetDynamicSuggestions(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	role := c.GetString("role")
	if tenantID <= 0 || userID <= 0 || role == "" {
		common.AuthFailed(c, "缺少有效身份上下文")
		return
	}
	pageKey := strings.TrimSpace(c.Query("pageKey"))
	if pageKey == "" {
		common.Fail(c, common.ParamErrorCode, "pageKey 不能为空")
		return
	}
	route := strings.TrimSpace(c.Query("route"))

	decl := assistantcontext.Load().Declaration(pageKey)
	if decl == nil {
		common.Success(c, DynamicSuggestionResult{
			PageKey: pageKey, Known: false, Generated: false, Cached: false,
			Reason: "unknown_page", Suggestions: []DynamicSuggestion{},
		})
		return
	}
	if !h.svc.dynamicSuggestions.Available() {
		common.Fail(c, common.ServiceUnavailableCode, "动态建议未启用")
		return
	}

	tasks := make([]string, 0, len(decl.Suggestions))
	questions := make([]string, 0, len(decl.Suggestions))
	for _, action := range decl.Suggestions {
		if action.Kind == "question" {
			questions = append(questions, action.Label)
		} else {
			tasks = append(tasks, action.Label)
		}
	}

	result := h.svc.dynamicSuggestions.Get(c.Request.Context(), DynamicSuggestionRequest{
		TenantID: tenantID, UserID: userID,
		PageKey: pageKey, Route: route, Role: role,
		Title: decl.Title, Scene: decl.Scene, Entrypoint: decl.Entrypoint,
		Tasks: tasks, Questions: questions,
	})
	if result == nil {
		common.Fail(c, common.InternalErrorCode, "生成动态建议失败")
		return
	}
	if result.Reason == "disabled" {
		common.Fail(c, common.ServiceUnavailableCode, "动态建议未启用")
		return
	}
	result.Known = true
	common.Success(c, result)
}
