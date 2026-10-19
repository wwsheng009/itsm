package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service"
)

// V2-02 异步任务体验：**先给可用答案，后台精算完成后通知**（方案 §11.4-4「先答后精 + 完成通知」）。
//
// 口径（与方案 §3.6 不打扰原则一致）：
//   - **先给可用答案**：`POST /ai/async/answers` 同步返回零 LLM 的快速参考（内嵌帮助索引命中，
//     复用 H0-03 数据面）；无命中时 `fast.source=none`，如实标注"暂无快速参考"。
//   - **后台精算**：进程内 goroutine 调 LLM（`AsyncAnswerGenerator`，有界并发 + 超时）；
//     完成/失败都落在内存任务表（TTL 30m、容量上限 200、租户+用户双隔离）。
//   - **完成通知**：精算成功经 `AsyncAnswerNotifier` 投递通知中心（best-effort，失败不影响任务结果）；
//     前端 Dock 另有 FAB 状态点/回执卡（任务表可轮询）。
//   - **诚实降级**：未装配生成器（无 Provider）→ 任务立即 `degraded`（reason=generator_unavailable），
//     快速参考照常返回，**不假装精算**；超时 → `failed`（reason=timeout）。
const (
	AsyncAnswerDefaultTTL        = 30 * time.Minute
	AsyncAnswerDefaultMaxEntries = 200
	AsyncAnswerMaxPromptRunes    = 1000
	AsyncAnswerFastItemLimit     = 3
	AsyncAnswerMaxConcurrency    = 4
	AsyncAnswerDefaultTimeout    = 60 * time.Second

	// AsyncAnswerStatusRunning 精算进行中（仅装配生成器时出现）。
	AsyncAnswerStatusRunning = "running"
	// AsyncAnswerStatusDone 精算完成（refined 非空）。
	AsyncAnswerStatusDone = "done"
	// AsyncAnswerStatusDegraded 未装配生成器：仅快速参考可用（诚实降级，非错误）。
	AsyncAnswerStatusDegraded = "degraded"
	// AsyncAnswerStatusFailed 精算失败（reason 说明；快速参考仍可用）。
	AsyncAnswerStatusFailed = "failed"

	asyncAnswerReasonGeneratorUnavailable = "generator_unavailable"
	asyncAnswerReasonTimeout              = "timeout"
	asyncAnswerReasonProviderError        = "provider_error"
	asyncAnswerReasonNotFound             = "not_found"
)

// AsyncAnswerRequest 一次异步精算请求（租户/用户由路由上下文注入）。
type AsyncAnswerRequest struct {
	TenantID int
	UserID   int
	Prompt   string
	PageKey  string
	Route    string
}

// AsyncAnswerFastItem 快速参考条目（来源：内嵌帮助索引）。
type AsyncAnswerFastItem struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Path    string `json:"path,omitempty"`
	PageKey string `json:"pageKey,omitempty"`
}

// AsyncAnswerFast 快速参考块（先给可用答案）。
type AsyncAnswerFast struct {
	Source string                `json:"source"` // help_index | none
	Items  []AsyncAnswerFastItem `json:"items"`
}

// AsyncAnswerRefined 后台精算结果。
type AsyncAnswerRefined struct {
	Model  string `json:"model,omitempty"`
	Answer string `json:"answer"`
}

// AsyncAnswerResult 任务快照（对外 JSON；按 taskId 轮询）。
type AsyncAnswerResult struct {
	TaskID     string              `json:"taskId"`
	Status     string              `json:"status"`
	Prompt     string              `json:"prompt"`
	PageKey    string              `json:"pageKey,omitempty"`
	CreatedAt  time.Time           `json:"createdAt"`
	FinishedAt *time.Time          `json:"finishedAt,omitempty"`
	Fast       AsyncAnswerFast     `json:"fast"`
	Refined    *AsyncAnswerRefined `json:"refined,omitempty"`
	Reason     string              `json:"reason,omitempty"`
}

// AsyncAnswerGenerator 精算生成器接口（测试注入 fake；生产由 LLMGateway 实现）。
type AsyncAnswerGenerator interface {
	Generate(ctx context.Context, req AsyncAnswerRequest, fast AsyncAnswerFast) (model string, answer string, err error)
}

// AsyncAnswerNotifier 完成通知落点（生产 = 通知中心；best-effort，失败只记日志）。
type AsyncAnswerNotifier interface {
	Notify(ctx context.Context, tenantID, userID int, result *AsyncAnswerResult) error
}

// AsyncAnswerSearcher 快速参考检索函数（默认内嵌帮助索引；测试注入 fake）。
type AsyncAnswerSearcher func(query, pageKey string, limit int) []helpindex.SearchResult

type asyncAnswerTask struct {
	tenantID  int
	userID    int
	expiresAt time.Time
	result    AsyncAnswerResult
}

// AsyncAnswerService 任务编排：快速参考 + 后台精算 + 完成通知（进程内任务表）。
type AsyncAnswerService struct {
	generator AsyncAnswerGenerator
	notifier  AsyncAnswerNotifier
	searcher  AsyncAnswerSearcher
	logger    *zap.SugaredLogger

	ttl        time.Duration
	maxEntries int
	timeout    time.Duration
	now        func() time.Time
	newID      func() string

	sem chan struct{}

	mu    sync.Mutex
	tasks map[string]*asyncAnswerTask
}

// NewAsyncAnswerService 构造服务；generator/notifier 可为 nil（分别对应诚实降级与跳过通知）。
func NewAsyncAnswerService(generator AsyncAnswerGenerator, notifier AsyncAnswerNotifier, logger *zap.SugaredLogger) *AsyncAnswerService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &AsyncAnswerService{
		generator:  generator,
		notifier:   notifier,
		searcher:   helpindex.Load().Search,
		logger:     logger,
		ttl:        AsyncAnswerDefaultTTL,
		maxEntries: AsyncAnswerDefaultMaxEntries,
		timeout:    AsyncAnswerDefaultTimeout,
		now:        time.Now,
		newID:      newAsyncAnswerID,
		sem:        make(chan struct{}, AsyncAnswerMaxConcurrency),
		tasks:      make(map[string]*asyncAnswerTask),
	}
}

// SetSearcher 覆盖快速参考检索函数（测试注入；nil 恢复默认）。
func (s *AsyncAnswerService) SetSearcher(searcher AsyncAnswerSearcher) {
	if s == nil {
		return
	}
	if searcher == nil {
		searcher = helpindex.Load().Search
	}
	s.searcher = searcher
}

// SetTimeout 覆盖精算超时（装配/测试用；<=0 不覆盖，保持默认 60s）。
func (s *AsyncAnswerService) SetTimeout(timeout time.Duration) {
	if s == nil || timeout <= 0 {
		return
	}
	s.timeout = timeout
}

// Start 生成快速参考并调度后台精算；立即返回任务快照（不阻塞等待精算）。
func (s *AsyncAnswerService) Start(ctx context.Context, req AsyncAnswerRequest) (*AsyncAnswerResult, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, ErrAsyncAnswerPromptRequired
	}
	if len([]rune(prompt)) > AsyncAnswerMaxPromptRunes {
		return nil, ErrAsyncAnswerPromptTooLong
	}

	fast := s.buildFastAnswer(prompt, req.PageKey)

	s.mu.Lock()
	s.pruneLocked()
	task := &asyncAnswerTask{
		tenantID:  req.TenantID,
		userID:    req.UserID,
		expiresAt: s.now().Add(s.ttl),
		result: AsyncAnswerResult{
			TaskID:    s.newID(),
			Status:    AsyncAnswerStatusRunning,
			Prompt:    prompt,
			PageKey:   req.PageKey,
			CreatedAt: s.now(),
			Fast:      fast,
		},
	}
	if s.generator == nil {
		// 诚实降级：无生成器（Provider 未配置）→ 只给快速参考，不假装精算。
		task.result.Status = AsyncAnswerStatusDegraded
		task.result.Reason = asyncAnswerReasonGeneratorUnavailable
		finished := task.result.CreatedAt
		task.result.FinishedAt = &finished
	}
	s.tasks[task.result.TaskID] = task
	s.enforceCapacityLocked()
	snapshot := task.result
	s.mu.Unlock()

	if s.generator != nil {
		go s.refine(req, task.result.TaskID, fast)
	}
	return &snapshot, nil
}

// Get 读取任务快照（租户 + 用户双隔离；未知/过期 → ok=false）。
func (s *AsyncAnswerService) Get(tenantID, userID int, taskID string) (*AsyncAnswerResult, bool) {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	task, ok := s.tasks[taskID]
	if !ok || task.tenantID != tenantID || task.userID != userID {
		return nil, false
	}
	if !s.now().Before(task.expiresAt) {
		return nil, false
	}
	snapshot := task.result
	return &snapshot, true
}

// refine 后台精算：有界并发 + 超时；成功通知，失败如实记 reason（快速参考保持可用）。
func (s *AsyncAnswerService) refine(req AsyncAnswerRequest, taskID string, fast AsyncAnswerFast) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-time.After(s.timeout):
		s.settle(taskID, AsyncAnswerStatusFailed, asyncAnswerReasonTimeout, nil)
		return
	}

	callCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	model, answer, err := s.generator.Generate(callCtx, req, fast)
	if err != nil {
		reason := asyncAnswerReasonProviderError
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			reason = asyncAnswerReasonTimeout
		}
		s.logger.Warnw("异步精算失败", "error", err, "taskID", taskID, "reason", reason)
		s.settle(taskID, AsyncAnswerStatusFailed, reason, nil)
		return
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		s.settle(taskID, AsyncAnswerStatusFailed, asyncAnswerReasonProviderError, nil)
		return
	}
	result := s.settle(taskID, AsyncAnswerStatusDone, "", &AsyncAnswerRefined{Model: model, Answer: answer})
	if result == nil || s.notifier == nil {
		return
	}
	if err := s.notifier.Notify(context.Background(), req.TenantID, req.UserID, result); err != nil {
		// 通知是 best-effort：失败不影响任务结果（前端仍有回执卡/FAB 状态点）。
		s.logger.Warnw("异步精算完成通知投递失败", "error", err, "taskID", taskID)
	}
}

// settle 收口任务（保持租户/用户隔离与 TTL）；返回收口后的快照（任务已被清理 → nil）。
func (s *AsyncAnswerService) settle(taskID, status, reason string, refined *AsyncAnswerRefined) *AsyncAnswerResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil
	}
	task.result.Status = status
	task.result.Reason = reason
	task.result.Refined = refined
	finished := s.now()
	task.result.FinishedAt = &finished
	snapshot := task.result
	return &snapshot
}

// buildFastAnswer 快速参考：内嵌帮助索引命中（零 LLM、零外部依赖）。
func (s *AsyncAnswerService) buildFastAnswer(prompt, pageKey string) AsyncAnswerFast {
	fast := AsyncAnswerFast{Source: "none", Items: []AsyncAnswerFastItem{}}
	if s.searcher == nil {
		return fast
	}
	hits := s.searcher(prompt, pageKey, AsyncAnswerFastItemLimit)
	if len(hits) == 0 {
		return fast
	}
	fast.Source = "help_index"
	for _, hit := range hits {
		item := AsyncAnswerFastItem{Title: hit.Title, Snippet: hit.Snippet, Path: hit.Path}
		if len(hit.PageKeys) > 0 {
			item.PageKey = hit.PageKeys[0]
		}
		fast.Items = append(fast.Items, item)
	}
	return fast
}

// pruneLocked 清理过期任务（调用方持锁）。
func (s *AsyncAnswerService) pruneLocked() {
	now := s.now()
	for id, task := range s.tasks {
		if !now.Before(task.expiresAt) {
			delete(s.tasks, id)
		}
	}
}

// enforceCapacityLocked 容量上限：优先清理已收口（非 running）的最旧任务（调用方持锁）。
func (s *AsyncAnswerService) enforceCapacityLocked() {
	for len(s.tasks) > s.maxEntries {
		var oldestID string
		var oldest time.Time
		for id, task := range s.tasks {
			if task.result.Status == AsyncAnswerStatusRunning {
				continue
			}
			if oldestID == "" || task.result.CreatedAt.Before(oldest) {
				oldestID, oldest = id, task.result.CreatedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.tasks, oldestID)
	}
}

// newAsyncAnswerID 任务 id（不可预测，避免跨用户枚举；失败时回退时间戳）。
func newAsyncAnswerID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("aa_%d", time.Now().UnixNano())
	}
	return "aa_" + hex.EncodeToString(buf)
}

// —— 错误与 Service 接线 ——

// ErrAsyncAnswersUnavailable 异步精算服务未装配（端点 503，fail-visible）。
var ErrAsyncAnswersUnavailable = errors.New("async answers service unavailable")

// ErrAsyncAnswerNotFound 任务不存在/过期/越权（统一 not_found，避免枚举）。
var ErrAsyncAnswerNotFound = errors.New("async answer task not found")

// ErrAsyncAnswerPromptRequired 提示词为空（400）。
var ErrAsyncAnswerPromptRequired = errors.New("prompt required")

// ErrAsyncAnswerPromptTooLong 提示词超长（400）。
var ErrAsyncAnswerPromptTooLong = errors.New("prompt too long")

// SetAsyncAnswers 注入异步精算服务（bootstrap 装配；nil = 端点 503）。
func (s *Service) SetAsyncAnswers(svc *AsyncAnswerService) {
	s.asyncAnswers = svc
}

// StartAsyncAnswer 发起异步精算（服务未装配 → ErrAsyncAnswersUnavailable）。
func (s *Service) StartAsyncAnswer(ctx context.Context, req AsyncAnswerRequest) (*AsyncAnswerResult, error) {
	if s == nil || s.asyncAnswers == nil {
		return nil, ErrAsyncAnswersUnavailable
	}
	return s.asyncAnswers.Start(ctx, req)
}

// GetAsyncAnswer 读取任务快照（不存在/过期/越权 → ErrAsyncAnswerNotFound）。
func (s *Service) GetAsyncAnswer(ctx context.Context, tenantID, userID int, taskID string) (*AsyncAnswerResult, error) {
	if s == nil || s.asyncAnswers == nil {
		return nil, ErrAsyncAnswersUnavailable
	}
	result, ok := s.asyncAnswers.Get(tenantID, userID, taskID)
	if !ok {
		return nil, ErrAsyncAnswerNotFound
	}
	return result, nil
}

// —— 生产装配：LLM 生成器 + 通知中心落点 ——

const asyncAnswerSystemPrompt = `你是 ITSM 系统的业务助手。用户已看到"快速参考"（帮助文档命中）；请给出完整、严谨的中文回答。
要求：先给结论，再给依据与操作步骤；引用帮助文档时注明标题；不确定处明确说明；不得编造数据或承诺写操作；不得自动提交任何变更。`

// llmAsyncAnswerGenerator 经 LLMGateway（§3.3 解析链）生成精算回答。
type llmAsyncAnswerGenerator struct {
	gateway *service.LLMGateway
	logger  *zap.SugaredLogger
}

// NewLLMAsyncAnswerGenerator 构造生成器；gateway 为 nil 时返回 nil（诚实降级为仅快速参考）。
func NewLLMAsyncAnswerGenerator(gateway *service.LLMGateway, logger *zap.SugaredLogger) AsyncAnswerGenerator {
	if gateway == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &llmAsyncAnswerGenerator{gateway: gateway, logger: logger}
}

func (g *llmAsyncAnswerGenerator) Generate(ctx context.Context, req AsyncAnswerRequest, fast AsyncAnswerFast) (string, string, error) {
	ctx = service.WithLLMTaskType(ctx, service.LLMTaskAsyncAnswer)
	providerReq := service.ProviderRequest{TenantID: req.TenantID, UserID: req.UserID}
	resolution, err := g.gateway.ResolveRequest(ctx, providerReq)
	if err != nil {
		return "", "", fmt.Errorf("resolve provider: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("用户问题：")
	builder.WriteString(req.Prompt)
	builder.WriteString("\n")
	if req.PageKey != "" {
		builder.WriteString(fmt.Sprintf("当前页面：%s\n", req.PageKey))
	}
	if req.Route != "" {
		builder.WriteString(fmt.Sprintf("当前路由：%s\n", req.Route))
	}
	if len(fast.Items) > 0 {
		builder.WriteString("快速参考（帮助文档命中，仅元数据）：\n")
		for _, item := range fast.Items {
			builder.WriteString(fmt.Sprintf("- %s：%s\n", item.Title, item.Snippet))
		}
	}
	builder.WriteString("请给出完整回答。")

	output, _, err := g.gateway.ChatWithRequest(ctx, providerReq, resolution.Model, []service.LLMMessage{
		{Role: "system", Content: asyncAnswerSystemPrompt},
		{Role: "user", Content: builder.String()},
	})
	if err != nil {
		return resolution.Model, "", err
	}
	return resolution.Model, output, nil
}

// entAsyncAnswerNotifier 完成通知落点：写通知中心（type=ai_async_answer，跳转助手面板）。
type entAsyncAnswerNotifier struct {
	client *ent.Client
}

// NewEntAsyncAnswerNotifier 构造通知落点；client 为 nil 时返回 nil（跳过通知，任务结果不受影响）。
func NewEntAsyncAnswerNotifier(client *ent.Client) AsyncAnswerNotifier {
	if client == nil {
		return nil
	}
	return &entAsyncAnswerNotifier{client: client}
}

func (n *entAsyncAnswerNotifier) Notify(ctx context.Context, tenantID, userID int, result *AsyncAnswerResult) error {
	if n == nil || n.client == nil || result == nil {
		return nil
	}
	prompt := result.Prompt
	if runes := []rune(prompt); len(runes) > 40 {
		prompt = string(runes[:40]) + "…"
	}
	_, err := n.client.Notification.Create().
		SetTenantID(tenantID).
		SetUserID(userID).
		SetType("ai_async_answer").
		SetTitle("AI 精算回答已就绪").
		SetMessage(fmt.Sprintf("「%s」的完整回答已生成，点击查看。", prompt)).
		SetActionURL("/ai/chat").
		SetActionText("查看回答").
		Save(ctx)
	return err
}
