package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/handlers/ai"
	"itsm-backend/internal/helpindex"
)

// V2-02 异步精算契约：先给可用答案（快速参考）/ 后台精算 / 完成通知 / 诚实降级 / 租户+用户隔离。

type stubAsyncGenerator struct {
	model  string
	answer string
	err    error
	wait   time.Duration

	mu    sync.Mutex
	calls int
}

func (s *stubAsyncGenerator) Generate(ctx context.Context, _ ai.AsyncAnswerRequest, _ ai.AsyncAnswerFast) (string, string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.wait > 0 {
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	return s.model, s.answer, s.err
}

func (s *stubAsyncGenerator) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type stubAsyncNotifier struct {
	mu    sync.Mutex
	calls []stubAsyncNotification
}

type stubAsyncNotification struct {
	TenantID int
	UserID   int
	TaskID   string
	Status   string
}

func (s *stubAsyncNotifier) Notify(_ context.Context, tenantID, userID int, result *ai.AsyncAnswerResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubAsyncNotification{TenantID: tenantID, UserID: userID, TaskID: result.TaskID, Status: result.Status})
	return nil
}

func (s *stubAsyncNotifier) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// stubSearcher 返回固定快速参考命中（隔离内嵌帮助索引，保证测试确定性）。
func stubSearcher(items ...helpindex.SearchResult) ai.AsyncAnswerSearcher {
	return func(query, pageKey string, limit int) []helpindex.SearchResult {
		return items
	}
}

func waitAsyncStatus(t *testing.T, svc *ai.AsyncAnswerService, tenantID, userID int, taskID, want string) *ai.AsyncAnswerResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if result, ok := svc.Get(tenantID, userID, taskID); ok && result.Status == want {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待任务 %s 到状态 %s 超时", taskID, want)
	return nil
}

func newAsyncAnswerRouter(domain *ai.Service, tenantID, userID int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := ai.NewHandler(domain)
	r := gin.New()
	withIdentity := func(c *gin.Context) {
		if tenantID > 0 {
			c.Set("tenant_id", tenantID)
		}
		if userID > 0 {
			c.Set("user_id", userID)
		}
	}
	r.POST("/api/v1/ai/async/answers", withIdentity, h.StartAsyncAnswer)
	r.GET("/api/v1/ai/async/answers/:id", withIdentity, h.GetAsyncAnswer)
	return r
}

type asyncAnswerResponse struct {
	Code int `json:"code"`
	Data struct {
		TaskID    string  `json:"taskId"`
		Status    string  `json:"status"`
		Prompt    string  `json:"prompt"`
		PageKey   string  `json:"pageKey"`
		Reason    string  `json:"reason"`
		CreatedAt string  `json:"createdAt"`
		Finished  *string `json:"finishedAt"`
		Fast      struct {
			Source string `json:"source"`
			Items  []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				Path    string `json:"path"`
				PageKey string `json:"pageKey"`
			} `json:"items"`
		} `json:"fast"`
		Refined *struct {
			Model  string `json:"model"`
			Answer string `json:"answer"`
		} `json:"refined"`
	} `json:"data"`
}

func TestAsyncAnswerService_FastAnswerThenRefineAndNotify(t *testing.T) {
	generator := &stubAsyncGenerator{model: "gpt-4o", answer: "完整回答：先看网络配置…"}
	notifier := &stubAsyncNotifier{}
	svc := ai.NewAsyncAnswerService(generator, notifier, zap.NewNop().Sugar())
	svc.SetSearcher(stubSearcher(
		helpindex.SearchResult{Path: "docs/help/network.md", Title: "网络故障排查", Snippet: "检查 DNS…", PageKeys: []string{"ticket.detail"}},
		helpindex.SearchResult{Path: "docs/help/vpn.md", Title: "VPN 连接", Snippet: "重连 VPN…"},
	))

	result, err := svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 7, UserID: 42, Prompt: "连不上网怎么办", PageKey: "ticket.detail"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.TaskID)
	assert.Equal(t, ai.AsyncAnswerStatusRunning, result.Status, "有生成器 → 立即返回 running（不阻塞精算）")
	assert.Equal(t, "help_index", result.Fast.Source)
	require.Len(t, result.Fast.Items, 2)
	assert.Equal(t, "网络故障排查", result.Fast.Items[0].Title)
	assert.Equal(t, "ticket.detail", result.Fast.Items[0].PageKey)

	settled := waitAsyncStatus(t, svc, 7, 42, result.TaskID, ai.AsyncAnswerStatusDone)
	require.NotNil(t, settled.Refined)
	assert.Equal(t, "gpt-4o", settled.Refined.Model)
	assert.Equal(t, "完整回答：先看网络配置…", settled.Refined.Answer)
	assert.NotNil(t, settled.FinishedAt)
	assert.Equal(t, 1, generator.callCount())

	require.Eventually(t, func() bool { return notifier.callCount() == 1 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, 7, notifier.calls[0].TenantID)
	assert.Equal(t, 42, notifier.calls[0].UserID)
	assert.Equal(t, ai.AsyncAnswerStatusDone, notifier.calls[0].Status)

	// 租户 + 用户双隔离：他人/未知 id 一律 not found。
	_, ok := svc.Get(7, 43, result.TaskID)
	assert.False(t, ok, "同租户其他用户不可见")
	_, ok = svc.Get(8, 42, result.TaskID)
	assert.False(t, ok, "跨租户不可见")
	_, ok = svc.Get(7, 42, "aa_unknown")
	assert.False(t, ok, "未知任务不可见")
}

func TestAsyncAnswerService_GeneratorFailureKeepsFastAnswer(t *testing.T) {
	generator := &stubAsyncGenerator{err: errors.New("provider down")}
	notifier := &stubAsyncNotifier{}
	svc := ai.NewAsyncAnswerService(generator, notifier, zap.NewNop().Sugar())
	svc.SetSearcher(stubSearcher(helpindex.SearchResult{Title: "帮助主题", Snippet: "片段"}))

	result, err := svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 1, UserID: 2, Prompt: "怎么打不开"})
	require.NoError(t, err)
	assert.Equal(t, "help_index", result.Fast.Source)

	settled := waitAsyncStatus(t, svc, 1, 2, result.TaskID, ai.AsyncAnswerStatusFailed)
	assert.Equal(t, "provider_error", settled.Reason)
	assert.Nil(t, settled.Refined)
	assert.Len(t, settled.Fast.Items, 1, "精算失败不影响快速参考（先给可用答案仍成立）")
	assert.Equal(t, 0, notifier.callCount(), "失败不投递完成通知")
}

func TestAsyncAnswerService_NoGeneratorDegradesHonestly(t *testing.T) {
	svc := ai.NewAsyncAnswerService(nil, &stubAsyncNotifier{}, nil)
	svc.SetSearcher(stubSearcher(helpindex.SearchResult{Title: "A", Snippet: "B"}))

	result, err := svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 1, UserID: 1, Prompt: "问题"})
	require.NoError(t, err)
	assert.Equal(t, ai.AsyncAnswerStatusDegraded, result.Status, "无生成器 → 不假装精算")
	assert.Equal(t, "generator_unavailable", result.Reason)
	assert.NotNil(t, result.FinishedAt)
	assert.Len(t, result.Fast.Items, 1, "快速参考照常可用")
	assert.Nil(t, result.Refined)
}

func TestAsyncAnswerService_TimeoutMapped(t *testing.T) {
	generator := &stubAsyncGenerator{wait: 200 * time.Millisecond}
	svc := ai.NewAsyncAnswerService(generator, nil, nil)
	svc.SetTimeout(30 * time.Millisecond)
	svc.SetSearcher(stubSearcher())

	result, err := svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 1, UserID: 1, Prompt: "慢问题"})
	require.NoError(t, err)
	assert.Equal(t, "none", result.Fast.Source, "无命中如实标注 none")

	settled := waitAsyncStatus(t, svc, 1, 1, result.TaskID, ai.AsyncAnswerStatusFailed)
	assert.Equal(t, "timeout", settled.Reason)
}

func TestAsyncAnswerService_PromptValidation(t *testing.T) {
	svc := ai.NewAsyncAnswerService(&stubAsyncGenerator{answer: "x"}, nil, nil)
	svc.SetSearcher(stubSearcher())

	_, err := svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 1, UserID: 1, Prompt: "   "})
	assert.ErrorIs(t, err, ai.ErrAsyncAnswerPromptRequired)

	_, err = svc.Start(context.Background(), ai.AsyncAnswerRequest{TenantID: 1, UserID: 1, Prompt: strings.Repeat("问", 1001)})
	assert.ErrorIs(t, err, ai.ErrAsyncAnswerPromptTooLong)
}

func TestStartAsyncAnswerHandler_ReturnsFastAnswerThenGet(t *testing.T) {
	generator := &stubAsyncGenerator{model: "m1", answer: "精算答案"}
	svc := ai.NewAsyncAnswerService(generator, nil, nil)
	svc.SetSearcher(stubSearcher(helpindex.SearchResult{Title: "网络故障排查", Snippet: "检查 DNS"}))
	domain := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	domain.SetAsyncAnswers(svc)

	r := newAsyncAnswerRouter(domain, 7, 42)
	w := doJSON(r, http.MethodPost, "/api/v1/ai/async/answers", map[string]interface{}{
		"prompt":  "连不上网怎么办",
		"pageKey": "ticket.detail",
		"route":   "/tickets/1",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body asyncAnswerResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	assert.NotEmpty(t, body.Data.TaskID)
	assert.Equal(t, ai.AsyncAnswerStatusRunning, body.Data.Status)
	assert.Equal(t, "help_index", body.Data.Fast.Source)
	require.Len(t, body.Data.Fast.Items, 1)
	assert.Equal(t, "网络故障排查", body.Data.Fast.Items[0].Title)

	settled := waitAsyncStatus(t, svc, 7, 42, body.Data.TaskID, ai.AsyncAnswerStatusDone)

	w = doJSON(r, http.MethodGet, "/api/v1/ai/async/answers/"+body.Data.TaskID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got asyncAnswerResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, settled.TaskID, got.Data.TaskID)
	assert.Equal(t, ai.AsyncAnswerStatusDone, got.Data.Status)
	require.NotNil(t, got.Data.Refined)
	assert.Equal(t, "精算答案", got.Data.Refined.Answer)

	// 他人（不同用户上下文）读取 → 404（不泄露存在性）。
	other := newAsyncAnswerRouter(domain, 7, 99)
	w = doJSON(other, http.MethodGet, "/api/v1/ai/async/answers/"+body.Data.TaskID, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestStartAsyncAnswerHandler_ValidationAndUnavailable(t *testing.T) {
	svc := ai.NewAsyncAnswerService(nil, nil, nil)
	domain := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	domain.SetAsyncAnswers(svc)
	r := newAsyncAnswerRouter(domain, 7, 42)

	w := doJSON(r, http.MethodPost, "/api/v1/ai/async/answers", map[string]interface{}{"prompt": "  "})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = doJSON(r, http.MethodPost, "/api/v1/ai/async/answers", map[string]interface{}{"prompt": "ok"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = doJSON(r, http.MethodGet, "/api/v1/ai/async/answers/aa_missing", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// 服务未装配 → 503 fail-visible（不假装可用）。
	noService := ai.NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r2 := newAsyncAnswerRouter(noService, 7, 42)
	w = doJSON(r2, http.MethodPost, "/api/v1/ai/async/answers", map[string]interface{}{"prompt": "ok"})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	w = doJSON(r2, http.MethodGet, "/api/v1/ai/async/answers/aa_x", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	// 缺少租户上下文 → 401 语义（fail-closed）。
	anonymous := newAsyncAnswerRouter(domain, 0, 0)
	w = doJSON(anonymous, http.MethodPost, "/api/v1/ai/async/answers", map[string]interface{}{"prompt": "ok"})
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
