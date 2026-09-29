// Package botintegration 的 B1-01 聊天链路集成断言：
// 走一次真实的 /api/v1/ai/chat/stream 请求（真实 ent/SQLite + 真实 RAG 关键字路径），
// 断言运行态三表落库（run + step + events）与收口状态，覆盖「一次对话产生运行档案」。
package botintegration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
	"itsm-backend/service/bot"

	_ "github.com/mattn/go-sqlite3"
)

// TestB1ChatStreamWritesRunArchive 覆盖 B1-01 的核心集成判据：
// 一次对话 → 1 条 bot_runs（收口后非 running）+ ≥1 条 bot_steps（llm）+ ≥2 条 bot_events
// （run_started/run_finished），且事件 seq 从 0 单调。
func TestB1ChatStreamWritesRunArchive(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	// 真实 RAG（无向量、空知识库：关键字路径可安全空跑）+ 桩工具面。
	rag := service.NewRAGService(h.client, nil, nil, zap.NewNop().Sugar(), service.RAGConfig{UseKeyword: true})
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), rag, h.registry, h.queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	svc.SetBotRunStore(h.store)

	handler := ai.NewHandler(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/ai/chat/stream", func(c *gin.Context) {
		c.Set("tenant_id", h.tenantID)
		c.Set("user_id", h.userID)
		c.Set("role", "super_admin")
		handler.ChatStream(c)
	})

	body, err := json.Marshal(map[string]any{
		"query":          "打印机坏了，怎么处理？",
		"conversationId": h.convID,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	// SSE 响应无论如何都必须回 200 并对客户端有终态事件（run 记录不改变既有契约）。
	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	// 运行档案断言（本用例的核心判据）。
	runs, err := h.client.BotRun.Query().Where(botrun.TenantID(h.tenantID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1, "一次对话必须恰好产生一条运行记录")
	run := runs[0]
	assert.Equal(t, h.convID, run.ConversationID)
	assert.Equal(t, "chat", run.Entrypoint)
	assert.NotEqual(t, "running", run.Status, "请求结束后运行必须收口")
	require.NotNil(t, run.FinishedAt)

	steps, err := h.store.ListRunSteps(ctx, h.tenantID, run.ID)
	require.NoError(t, err)
	require.NotEmpty(t, steps, "至少记录一条 llm 步骤")
	assert.Equal(t, "llm", steps[0].Type)
	assert.GreaterOrEqual(t, steps[0].DurationMs, 0)

	events, err := h.store.ListRunEvents(ctx, h.tenantID, run.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 2, "至少 run_started 与 run_finished 两条事件")
	assert.Equal(t, "run_started", events[0].Type)
	assert.Equal(t, 0, events[0].Seq)
	assert.Equal(t, "run_finished", events[len(events)-1].Type)
	for index := 1; index < len(events); index++ {
		assert.Equal(t, events[index-1].Seq+1, events[index].Seq, "事件 seq 必须连续递增")
	}

	// 本次对话内如有工具调用，必须全部带 run_id（贯通一致性）。
	invocations, err := h.client.ToolInvocation.Query().All(ctx)
	require.NoError(t, err)
	for _, invocation := range invocations {
		if invocation.ConversationID == h.convID {
			assert.Equal(t, run.ID, invocation.RunID,
				"会话 %d 内调用记录必须归属本次运行", h.convID)
		}
	}

	// 二次对话 → 第二条运行（不复用、不覆盖）。
	recorder2 := httptest.NewRecorder()
	request2 := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", bytes.NewBuffer(body))
	request2.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder2, request2)
	require.Equal(t, http.StatusOK, recorder2.Code)

	runsAfter, err := h.client.BotRun.Query().Where(botrun.TenantID(h.tenantID)).All(ctx)
	require.NoError(t, err)
	assert.Len(t, runsAfter, 2, "每次对话各产生一条独立运行")
}

// TestB1RunStoreNilSafe 锁定关闭态：未注入 RunStore 时运行态三表零写入。
func TestB1RunStoreNilSafe(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	rag := service.NewRAGService(h.client, nil, nil, zap.NewNop().Sugar(), service.RAGConfig{UseKeyword: true})
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), rag, h.registry, h.queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	// 故意不注入 RunStore（等价 bot.enabled=false）。

	handler := ai.NewHandler(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/ai/chat/stream", func(c *gin.Context) {
		c.Set("tenant_id", h.tenantID)
		c.Set("user_id", h.userID)
		c.Set("role", "super_admin")
		handler.ChatStream(c)
	})
	body := []byte(`{"query":"关闭态检查","conversationId":1}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	count, err := h.client.BotRun.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count, "未注入 RunStore 时不得写入任何运行记录（零行为变化）")
}

// TestB1ChatStreamRunManagerSequenceMatchesDB 覆盖 B1-02 的集成判据：
// RunManager 接管后，真实 chat/stream 的「广播顺序 == 落库顺序」，且 Observer
// 回调触发时事件必已可反查（先落库后广播，SSE 重放与审计同源）。
func TestB1ChatStreamRunManagerSequenceMatchesDB(t *testing.T) {
	ctx := context.Background()
	h := newB1Harness(t)

	var (
		mu         sync.Mutex
		observed   []string
		notYetInDB []string
	)
	manager := bot.NewManager(bot.NewRunStore(h.client), bot.Budget{MaxSteps: 8, MaxToolCalls: 2}).
		WithObserver(bot.ObserverFunc(func(tenantID, runID, seq int, eventType string, payload map[string]any) {
			exists, err := h.client.BotEvent.Query().
				Where(botevent.RunID(runID), botevent.Seq(seq)).
				Exist(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || !exists {
				notYetInDB = append(notYetInDB, eventType)
			}
			observed = append(observed, eventType)
		}))

	rag := service.NewRAGService(h.client, nil, nil, zap.NewNop().Sugar(), service.RAGConfig{UseKeyword: true})
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), rag, h.registry, h.queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	svc.SetBotRunner(manager)

	handler := ai.NewHandler(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/ai/chat/stream", func(c *gin.Context) {
		c.Set("tenant_id", h.tenantID)
		c.Set("user_id", h.userID)
		c.Set("role", "super_admin")
		handler.ChatStream(c)
	})
	body, err := json.Marshal(map[string]any{"query": "事件顺序核对", "conversationId": h.convID})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	runs := h.client.BotRun.Query().Where(botrun.TenantID(h.tenantID)).AllX(ctx)
	require.Len(t, runs, 1)
	runID := runs[0].ID

	events := h.client.BotEvent.Query().
		Where(botevent.RunID(runID)).
		Order(ent.Asc(botevent.FieldSeq)).
		AllX(ctx)
	require.Len(t, events, 2, "无工具调用的对话应恰好产生 run_started 与 run_finished")

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, notYetInDB, "Observer 回调时事件必须已落库（先落库后广播）")
	assert.Equal(t, []string{events[0].Type, events[1].Type}, observed,
		"广播顺序必须与落库 seq 顺序一致")
	assert.Equal(t, []string{"run_started", "run_finished"}, observed)

	steps := h.client.BotStep.Query().
		Where(botstep.RunID(runID)).
		Order(ent.Asc(botstep.FieldStepIndex)).
		AllX(ctx)
	require.Len(t, steps, 1, "无工具调用时应只有一条 llm 步骤")
	assert.Equal(t, "llm", steps[0].Type)
	assert.Equal(t, 0, steps[0].StepIndex)

	// B1-03：SSE 帧契约——v2 运行事件带 `v:2` + runId，v1 事件载荷不变，done 收尾。
	frames := parseSSEFrames(t, recorder.Body.String())
	require.NotEmpty(t, frames)
	assert.Equal(t, "run_started", frames[0].Event, "运行开始必须是首帧（有运行档案时）")
	runStarted := frames[0].Object(t)
	assert.Equal(t, float64(ai.SSEProtocolVersionV2), runStarted["v"])
	assert.Equal(t, float64(runID), runStarted["runId"])
	assert.Equal(t, "chat", runStarted["entrypoint"])
	assert.Equal(t, "done", frames[len(frames)-1].Event)

	var stepFrames, deltaFrames, doneFrames int
	for _, frame := range frames {
		assert.True(t, ai.IsKnownSSEEvent(frame.Event), "帧事件名 %s 必须已登记", frame.Event)
		switch frame.Event {
		case "step":
			stepFrames++
			payload := frame.Object(t)
			assert.Equal(t, float64(ai.SSEProtocolVersionV2), payload["v"])
			assert.Equal(t, "llm", payload["type"])
		case "delta":
			deltaFrames++
			// v1 兼容承诺：delta 载荷保持 {content} 且不带 `v`。
			payload := frame.Object(t)
			assert.NotContains(t, payload, "v")
			assert.Contains(t, payload, "content")
		case "done":
			doneFrames++
			payload := frame.Object(t)
			assert.NotContains(t, payload, "v")
			assert.Equal(t, float64(h.convID), payload["conversationId"])
		}
	}
	assert.Equal(t, 1, stepFrames, "llm 步骤应广播一次")
	assert.Equal(t, 1, doneFrames)
	// delta 帧数量取决于 RAG 回答路径（无网关时可能为 0），其载荷形状已在循环内逐帧断言。
	assert.GreaterOrEqual(t, deltaFrames, 0)
}

// TestB1ChatStreamBudgetAbortEndToEnd 覆盖 AB1-02/AB1-03 的端到端判据：
// 模型连续发起工具调用触发预算超限 → 记录 error{budget_exceeded} 并收口为 failed
// → **中止主链路**（流以 error 结束、不再有 done），且 v1 工具事件契约不变。
func TestB1ChatStreamBudgetAbortEndToEnd(t *testing.T) {
	h := newB1Harness(t)
	rag := service.NewRAGService(h.client, nil, nil, zap.NewNop().Sugar(), service.RAGConfig{UseKeyword: true})
	svc := ai.NewService(ai.NewEntRepository(h.client), zap.NewNop().Sugar(), rag, h.registry, h.queue,
		nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(h.client)
	// 确定性模型替身：每次模型轮次都发起一次 stub__list_notes 调用（工具循环见 rag_service）。
	svc.SetLLMGateway(service.NewLLMGateway(
		service.NewMockProvider(service.MockProviderOptions{
			Trigger:  "__tool__",
			ToolName: "stub__list_notes",
			ToolArgs: "{}",
		}), nil, nil, "mock"))
	manager := bot.NewManager(bot.NewRunStore(h.client), bot.Budget{MaxSteps: 8, MaxToolCalls: 1})
	svc.SetBotRunner(manager)

	handler := ai.NewHandler(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/ai/chat/stream", func(c *gin.Context) {
		c.Set("tenant_id", h.tenantID)
		c.Set("user_id", h.userID)
		c.Set("role", "super_admin")
		handler.ChatStream(c)
	})
	body, err := json.Marshal(map[string]any{"query": "__tool__ 查一下备注", "conversationId": h.convID})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat/stream", bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	frames := parseSSEFrames(t, recorder.Body.String())
	require.NotEmpty(t, frames)

	var (
		startedFrames, finishedFrames, errorFrames, doneFrames int
		errorPayload                                           map[string]interface{}
	)
	for _, frame := range frames {
		switch frame.Event {
		case "tool_call_started":
			startedFrames++
			assert.NotContains(t, frame.Object(t), "v", "v1 工具事件载荷不得因 B1-03 变化")
		case "tool_call_finished":
			finishedFrames++
		case "error":
			errorFrames++
			errorPayload = frame.Object(t)
		case "done":
			doneFrames++
		}
	}
	assert.Equal(t, 1, startedFrames, "第二次调用在执行点被预算闸门拒绝：不再发 started")
	assert.Equal(t, 1, finishedFrames, "首次调用正常完成")
	assert.Equal(t, 1, errorFrames, "流以 error 结束")
	assert.Contains(t, errorPayload["errorCode"], "budget_exceeded")
	assert.Zero(t, doneFrames, "预算超限中止主链路：不得再发 done")

	// 运行档案：收口 failed + budget_exceeded，error 事件落库一次，工具调用事件成对。
	runs := h.client.BotRun.Query().Where(botrun.TenantID(h.tenantID)).AllX(context.Background())
	require.Len(t, runs, 1)
	runID := runs[0].ID
	assert.Equal(t, "failed", runs[0].Status)
	assert.Equal(t, "budget_exceeded", runs[0].ErrorCode)

	events := h.client.BotEvent.Query().
		Where(botevent.RunID(runID)).
		Order(ent.Asc(botevent.FieldSeq)).
		AllX(context.Background())
	var budgetEvents, toolDoneEvents, toolFailedEvents int
	for _, event := range events {
		switch event.Type {
		case "error":
			budgetEvents++
			assert.Contains(t, event.PayloadJSON, `"code":"budget_exceeded"`)
			assert.Contains(t, event.PayloadJSON, `"reason":"max_tool_calls"`)
		case "tool_call":
			if strings.Contains(event.PayloadJSON, `"status":"done"`) {
				toolDoneEvents++
			}
			if strings.Contains(event.PayloadJSON, `"status":"failed"`) {
				toolFailedEvents++
			}
		}
	}
	assert.Equal(t, 1, budgetEvents, "预算超限只记一条审计（重复拒绝不重复落库）")
	assert.Equal(t, 1, toolDoneEvents, "首次工具调用正常落库")
	assert.Equal(t, 1, toolFailedEvents, "被拒绝的调用只落一条 failed（带 budget_exceeded 错误码）")
}

// sseFrame 是 SSE 响应体解析出的一帧。
type sseFrame struct {
	Event string
	Data  string
}

// Object 把帧载荷解析为对象（数组载荷调用会失败，测试中仅对象帧调用本方法）。
func (f sseFrame) Object(t *testing.T) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(f.Data), &payload), "帧 %s 载荷必须是 JSON 对象：%s", f.Event, f.Data)
	return payload
}

// parseSSEFrames 解析 `event:` / `data:` 帧序列（body 为 handler 的 SSE 输出）。
func parseSSEFrames(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		frame := sseFrame{}
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				frame.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frame.Data = strings.TrimPrefix(line, "data: ")
			}
		}
		if frame.Event != "" {
			frames = append(frames, frame)
		}
	}
	return frames
}
