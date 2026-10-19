package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service"
)

// H1-01 文档待办队列：合并排序（纯函数）+ 端点契约（含埋点降级）。

func TestBuildHelpTodos_MergeAndSort(t *testing.T) {
	idx := helpindex.Load()

	items := buildHelpTodos(idx, []service.AssistantHelpGapCluster{
		{PageKey: "dashboard", GapCount: 3, DraftCount: 1, LastSeen: "2026-10-18T02:00:00Z"},
		{PageKey: "ghost.page", GapCount: 9},
	}, 50)

	require.GreaterOrEqual(t, len(items), 2)
	// 未注册页面（unknown）优先于「已覆盖但有用户求助缺口」的页面。
	assert.Equal(t, "ghost.page", items[0].PageKey)
	assert.Equal(t, "unknown", items[0].DocStatus)
	assert.Equal(t, helpTodoReasonUnregistered, items[0].Reason)
	assert.Equal(t, 9, items[0].GapCount)

	assert.Equal(t, "dashboard", items[1].PageKey)
	assert.Equal(t, "ok", items[1].DocStatus)
	assert.Equal(t, helpTodoReasonUserGaps, items[1].Reason)
	assert.Equal(t, 3, items[1].GapCount)
	assert.Equal(t, 1, items[1].DraftCount)
	assert.Equal(t, "2026-10-18T02:00:00Z", items[1].LastSeen)
	assert.Equal(t, "仪表盘", items[1].Title)

	// limit 截断。
	assert.Len(t, buildHelpTodos(idx, []service.AssistantHelpGapCluster{{PageKey: "dashboard"}}, 1), 1)
}

func newHelpTodosTelemetry(t *testing.T) (*service.AITelemetryService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return service.NewAITelemetryService(db), mock
}

func newHelpTodosRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(svc)
	r := gin.New()
	r.GET("/api/v1/ai/help/todos", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		h.GetHelpTodos(c)
	})
	return r
}

type helpTodosPayload struct {
	Code int `json:"code"`
	Data struct {
		Days  int `json:"days"`
		Items []struct {
			PageKey        string `json:"pageKey"`
			DocStatus      string `json:"docStatus"`
			Reason         string `json:"reason"`
			GapCount       int    `json:"gapCount"`
			DraftCount     int    `json:"draftCount"`
			LastSeen       string `json:"lastSeen"`
			State          string `json:"state"`
			StateUpdatedBy int    `json:"stateUpdatedBy"`
			StateUpdatedAt string `json:"stateUpdatedAt"`
		} `json:"items"`
		Totals map[string]int `json:"totals"`
		// telemetry 为嵌套对象；此处用 map 读取避免字段名耦合。
		Telemetry map[string]interface{} `json:"telemetry"`
		States    map[string]interface{} `json:"states"`
	} `json:"data"`
}

func doHelpTodos(t *testing.T, r *gin.Engine, path string) helpTodosPayload {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp helpTodosPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func TestGetHelpTodos_EndpointWithTelemetry(t *testing.T) {
	telemetry, mock := newHelpTodosTelemetry(t)
	mock.ExpectQuery(regexp.QuoteMeta("FROM ai_telemetry_events")).
		WithArgs(int64(7), int64(30), int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "gap_count", "draft_count", "last_seen"}).
			AddRow("dashboard", 3, 1, time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)).
			AddRow("ghost.page", 9, 0, nil))

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	resp := doHelpTodos(t, newHelpTodosRouter(svc), "/api/v1/ai/help/todos")

	assert.Equal(t, 30, resp.Data.Days)
	assert.Equal(t, true, resp.Data.Telemetry["available"])
	require.GreaterOrEqual(t, len(resp.Data.Items), 2)
	assert.Equal(t, "ghost.page", resp.Data.Items[0].PageKey)
	assert.Equal(t, "dashboard", resp.Data.Items[1].PageKey)
	assert.Equal(t, 3, resp.Data.Items[1].GapCount)
	assert.Equal(t, "2026-10-18T02:00:00Z", resp.Data.Items[1].LastSeen)
	assert.Equal(t, len(resp.Data.Items), resp.Data.Totals["items"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetHelpTodos_EndpointDegradesWithoutTelemetry(t *testing.T) {
	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r := newHelpTodosRouter(svc)

	resp := doHelpTodos(t, r, "/api/v1/ai/help/todos?days=7&limit=10")
	assert.Equal(t, 7, resp.Data.Days)
	assert.Equal(t, false, resp.Data.Telemetry["available"])
	// H1-04 条件项：状态服务未装配 → states.available=false（状态按 open 缺省渲染，不 5xx）。
	assert.Equal(t, false, resp.Data.States["available"])
	// 仅账本缺口：当前种子文档无缺口 → 空队列（不为 null）。
	assert.NotNil(t, resp.Data.Items)

	// limit 上限钳制到 200（不报错、不越界）。
	resp = doHelpTodos(t, r, "/api/v1/ai/help/todos?limit=1000")
	assert.Equal(t, 30, resp.Data.Days)
}

// H1-01/H1-03/H1-04 条件项闭环：待办状态持久化（GET 合并 + PUT 写面）。

func newHelpTodoStateService(t *testing.T) (*service.HelpDocTodoStateService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return service.NewHelpDocTodoStateService(db, nil), mock
}

func TestGetHelpTodos_MergesPersistedStates(t *testing.T) {
	telemetry, mock := newHelpTodosTelemetry(t)
	mock.ExpectQuery(regexp.QuoteMeta("FROM ai_telemetry_events")).
		WithArgs(int64(7), int64(30), int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "gap_count", "draft_count", "last_seen"}).
			AddRow("dashboard", 3, 1, time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)))

	// 状态面独立 sqlmock：验证「遥测聚类 + 持久化状态」合并，而两条数据面互不耦合。
	states, stateMock := newHelpTodoStateService(t)
	stateMock.ExpectQuery(regexp.QuoteMeta("FROM ai_doc_todo_states")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "state", "updated_by", "updated_at"}).
			AddRow("dashboard", service.HelpDocTodoStateClaimed, int64(9),
				time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)))

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, telemetry)
	svc.SetHelpDocTodoStates(states)
	resp := doHelpTodos(t, newHelpTodosRouter(svc), "/api/v1/ai/help/todos")

	assert.Equal(t, true, resp.Data.States["available"])
	assert.Equal(t, 1, resp.Data.Totals["claimed"])
	assert.Equal(t, 0, resp.Data.Totals["ignored"])

	found := -1
	for i := range resp.Data.Items {
		if resp.Data.Items[i].PageKey == "dashboard" {
			found = i
			break
		}
	}
	require.GreaterOrEqual(t, found, 0, "dashboard 待办项应存在")
	assert.Equal(t, service.HelpDocTodoStateClaimed, resp.Data.Items[found].State)
	assert.Equal(t, 9, resp.Data.Items[found].StateUpdatedBy)
	assert.Equal(t, "2026-10-18T03:00:00Z", resp.Data.Items[found].StateUpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.NoError(t, stateMock.ExpectationsWereMet())
}

func newHelpTodoStateRouter(svc *Service, tenantID, userID int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(svc)
	r := gin.New()
	r.PUT("/api/v1/ai/help/todos/state", func(c *gin.Context) {
		if tenantID > 0 {
			c.Set("tenant_id", tenantID)
		}
		if userID > 0 {
			c.Set("user_id", userID)
		}
		h.SetHelpTodoState(c)
	})
	return r
}

func doHelpTodoStatePut(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/ai/help/todos/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

type helpTodoStatePayload struct {
	Code int `json:"code"`
	Data struct {
		PageKey   string `json:"pageKey"`
		State     string `json:"state"`
		UpdatedBy int    `json:"updatedBy"`
		UpdatedAt string `json:"updatedAt"`
	} `json:"data"`
}

func TestSetHelpTodoState_EndpointClaimAndReopen(t *testing.T) {
	states, stateMock := newHelpTodoStateService(t)
	stateMock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ai_doc_todo_states")).
		WithArgs(int64(7), "dashboard", service.HelpDocTodoStateClaimed, int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"state", "updated_by", "updated_at"}).
			AddRow(service.HelpDocTodoStateClaimed, int64(9),
				time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)))

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetHelpDocTodoStates(states)
	r := newHelpTodoStateRouter(svc, 7, 9)

	// 认领：幂等 UPSERT，返回落库状态。
	w := doHelpTodoStatePut(t, r, `{"pageKey":"dashboard","state":"claimed"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp helpTodoStatePayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.SuccessCode, resp.Code)
	assert.Equal(t, "dashboard", resp.Data.PageKey)
	assert.Equal(t, service.HelpDocTodoStateClaimed, resp.Data.State)
	assert.Equal(t, 9, resp.Data.UpdatedBy)
	assert.Equal(t, "2026-10-18T04:00:00Z", resp.Data.UpdatedAt)

	// 恢复（open）：删除行回到缺省态。
	stateMock.ExpectExec(regexp.QuoteMeta("DELETE FROM ai_doc_todo_states")).
		WithArgs(int64(7), "dashboard").
		WillReturnResult(sqlmock.NewResult(0, 1))
	w = doHelpTodoStatePut(t, r, `{"pageKey":"dashboard","state":"open"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp = helpTodoStatePayload{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, common.SuccessCode, resp.Code)
	assert.Equal(t, service.HelpDocTodoStateOpen, resp.Data.State)
	assert.Empty(t, resp.Data.UpdatedAt)

	assert.NoError(t, stateMock.ExpectationsWereMet())
}

func TestSetHelpTodoState_ValidationUnavailableAndIdentity(t *testing.T) {
	// 未装配状态服务 → 503（写路径可见地失败，不静默丢状态）。
	unavailable := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	w := doHelpTodoStatePut(t, newHelpTodoStateRouter(unavailable, 7, 9), `{"pageKey":"dashboard","state":"claimed"}`)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// 身份缺失 → 401（零库访问）。
	w = doHelpTodoStatePut(t, newHelpTodoStateRouter(unavailable, 0, 0), `{"pageKey":"dashboard","state":"claimed"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 非法输入 → 400（零库访问：stateMock 无任何预期）。
	states, stateMock := newHelpTodoStateService(t)
	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetHelpDocTodoStates(states)
	r := newHelpTodoStateRouter(svc, 7, 9)

	for name, body := range map[string]string{
		"unknown state": `{"pageKey":"dashboard","state":"done"}`,
		"missing key":   `{"state":"claimed"}`,
		"malformed":     `not-json`,
	} {
		w = doHelpTodoStatePut(t, r, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
	}
	assert.NoError(t, stateMock.ExpectationsWereMet(), "非法输入不得触达数据库")
}
