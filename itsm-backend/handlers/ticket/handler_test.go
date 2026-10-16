package ticket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/handlers/common/datascope"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// -----------------------------------------------------------------------------
// mockRepository
// -----------------------------------------------------------------------------

type mockRepository struct {
	mu              sync.Mutex
	tickets         map[int]*Ticket
	nextID          int
	statsCalled     bool
	lastListFilters map[string]interface{} // 记录最近一次 List 传入的 filters（过滤补全回归用）
}

func newMockRepository() *mockRepository {
	return &mockRepository{tickets: make(map[int]*Ticket)}
}

func (m *mockRepository) Create(ctx context.Context, params *CreateParams, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	t := &Ticket{
		ID:                m.nextID,
		TicketNumber:      "TKT-" + time.Now().Format("20060102") + "-001",
		Title:             params.Title,
		Description:       params.Description,
		DescriptionHTML:   params.DescriptionHTML,
		DescriptionFormat: params.DescriptionFormat,
		Status:            "new",
		Priority:          params.Priority,
		Type:              params.Type,
		RequesterID:       params.RequesterID,
		AssigneeID:        params.AssigneeID,
		TenantID:          tenantID,
		Version:           1,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	m.tickets[t.ID] = t
	return t, nil
}

func (m *mockRepository) GetByID(ctx context.Context, id int, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, errors.New("ticket not found")
	}
	return t, nil
}

func (m *mockRepository) GetByNumber(ctx context.Context, ticketNumber string, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tickets {
		if t.TicketNumber == ticketNumber && t.TenantID == tenantID {
			return t, nil
		}
	}
	return nil, errors.New("ticket not found")
}

func (m *mockRepository) Update(ctx context.Context, id int, params *UpdateParams, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, errors.New("ticket not found")
	}
	if params.Title != nil {
		t.Title = *params.Title
	}
	if params.Status != nil {
		t.Status = *params.Status
	}
	if params.Priority != nil {
		t.Priority = *params.Priority
	}
	if params.DescriptionHTML != nil {
		t.DescriptionHTML = *params.DescriptionHTML
	}
	if params.DescriptionFormat != nil {
		t.DescriptionFormat = *params.DescriptionFormat
	}
	t.Version++
	t.UpdatedAt = time.Now()
	return t, nil
}

func (m *mockRepository) Delete(ctx context.Context, id int, tenantID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.TenantID != tenantID {
		return errors.New("ticket not found")
	}
	delete(m.tickets, id)
	return nil
}

func (m *mockRepository) List(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}, ds datascope.DataScope, currentUserID int) ([]*Ticket, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 回归测试用：记录 handler 实际传入的 filters（列表过滤补全验证）
	m.lastListFilters = filters
	var out []*Ticket
	for _, t := range m.tickets {
		if t.TenantID != tenantID {
			continue
		}
		// 简单按 status/priority/assignee_id 过滤（键为仓储翻译器的 snake_case 契约）
		if v, ok := filters["status"].(string); ok && v != "" && string(t.Status) != v {
			continue
		}
		if v, ok := filters["priority"].(string); ok && v != "" && string(t.Priority) != v {
			continue
		}
		if v, ok := filters["assignee_id"].(int); ok && v > 0 && (t.AssigneeID == nil || *t.AssigneeID != v) {
			continue
		}
		out = append(out, t)
	}
	return out, len(out), nil
}

func (m *mockRepository) BatchDelete(ctx context.Context, ids []int, tenantID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.tickets, id)
	}
	return nil
}

func (m *mockRepository) Exists(ctx context.Context, id int, tenantID int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	return ok && t.TenantID == tenantID, nil
}

func (m *mockRepository) FindByAssignee(ctx context.Context, assigneeID int, tenantID int) ([]*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Ticket
	for _, t := range m.tickets {
		if t.TenantID == tenantID && t.AssigneeID != nil && *t.AssigneeID == assigneeID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *mockRepository) FindOverdue(ctx context.Context, tenantID int) ([]*Ticket, error) {
	return []*Ticket{}, nil
}

func (m *mockRepository) Search(ctx context.Context, keyword string, tenantID int) ([]*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Ticket
	for _, t := range m.tickets {
		if t.TenantID == tenantID {
			if contains(t.Title, keyword) || contains(t.Description, keyword) {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func (m *mockRepository) GetStats(ctx context.Context, tenantID int) (*TicketStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statsCalled = true
	stats := &TicketStats{}
	for _, t := range m.tickets {
		if t.TenantID == tenantID {
			stats.TotalTickets++
		}
	}
	return stats, nil
}

func (m *mockRepository) GenerateTicketNumber(ctx context.Context, tenantID int) (string, error) {
	return "TKT-" + time.Now().Format("20060102") + "-001", nil
}

func (m *mockRepository) UpdateStatus(ctx context.Context, id int, status string, tenantID int) (*Ticket, error) {
	return m.Update(ctx, id, &UpdateParams{Status: &status}, tenantID)
}

func (m *mockRepository) AssignTicket(ctx context.Context, id int, assigneeID int, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, errors.New("ticket not found")
	}
	t.AssigneeID = &assigneeID
	if t.Status == "new" {
		t.Status = "open"
	}
	t.Version++
	return t, nil
}

func (m *mockRepository) ResolveTicket(ctx context.Context, id int, resolution string, tenantID int) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[id]
	if !ok || t.TenantID != tenantID {
		return nil, errors.New("ticket not found")
	}
	t.Status = "resolved"
	t.Resolution = &resolution
	now := time.Now()
	t.ResolvedAt = &now
	t.Version++
	return t, nil
}

func (m *mockRepository) CloseTicket(ctx context.Context, id int, tenantID int) (*Ticket, error) {
	return m.UpdateStatus(ctx, id, "closed", tenantID)
}

func (m *mockRepository) EscalateTicket(ctx context.Context, id int, reason string, tenantID int, escalatedBy int) (*Ticket, error) {
	return m.UpdateStatus(ctx, id, "in_progress", tenantID)
}

func (m *mockRepository) UpdateSLADeadlines(ctx context.Context, id int, responseDeadline, resolutionDeadline *time.Time, slaDefinitionID *int, matchedLayer string, tenantID int) error {
	return nil
}

func (m *mockRepository) CreateTemplate(ctx context.Context, tmpl *TicketTemplate, tenantID int) (*TicketTemplate, error) {
	return tmpl, nil
}

func (m *mockRepository) UpdateTemplate(ctx context.Context, id int, tmpl *TicketTemplate, tenantID int) (*TicketTemplate, error) {
	return tmpl, nil
}

func (m *mockRepository) DeleteTemplate(ctx context.Context, id int, tenantID int) error {
	return nil
}

func (m *mockRepository) GetTemplate(ctx context.Context, id int, tenantID int) (*TicketTemplate, error) {
	return nil, errors.New("template not found")
}

func (m *mockRepository) ListTemplates(ctx context.Context, tenantID int) ([]*TicketTemplate, error) {
	return []*TicketTemplate{}, nil
}

func (m *mockRepository) UpdateTemplateStatus(ctx context.Context, id int, isActive bool, tenantID int) (*TicketTemplate, error) {
	return &TicketTemplate{ID: id, IsActive: isActive}, nil
}

func (m *mockRepository) CopyTemplate(ctx context.Context, id int, newName string, tenantID int) (*TicketTemplate, error) {
	return &TicketTemplate{ID: id, Name: newName}, nil
}

func (m *mockRepository) GetTemplateCategories(ctx context.Context, tenantID int) ([]string, error) {
	return []string{}, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// test harness
// -----------------------------------------------------------------------------

func newTestHarness(t *testing.T) (*gin.Engine, *mockRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newMockRepository()
	svc := NewService(repo, nil, zap.NewNop().Sugar())
	h := NewHandler(svc)
	r := gin.New()

	auth := func(c *gin.Context) {
		if v := c.GetHeader("X-Test-TenantID"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: n})
			}
		}
		if v := c.GetHeader("X-Test-UserID"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				c.Set("user_id", n)
			}
		}
		if v := c.GetHeader("X-Test-Role"); v != "" {
			c.Set("role", v)
		} else {
			c.Set("role", "agent")
		}
		c.Next()
	}

	api := r.Group("/api/v1", auth)
	api.POST("/tickets", h.CreateTicket)
	api.GET("/tickets", h.ListTickets)
	api.GET("/tickets/:id", h.GetTicket)
	api.PUT("/tickets/:id", h.UpdateTicket)
	api.DELETE("/tickets/:id", h.DeleteTicket)
	api.POST("/tickets/:id/assign", h.AssignTicket)
	api.POST("/tickets/:id/escalate", h.EscalateTicket)
	api.POST("/tickets/:id/resolve", h.ResolveTicket)
	api.POST("/tickets/:id/close", h.CloseTicket)
	api.PUT("/tickets/:id/status", h.UpdateTicketStatus)
	api.GET("/tickets/stats", h.GetTicketStats)
	api.GET("/tickets/search", h.SearchTickets)

	return r, repo
}

func doJSON(t *testing.T, r http.Handler, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// -----------------------------------------------------------------------------
// handler tests
// -----------------------------------------------------------------------------

func TestHandler_Create_TableDriven(t *testing.T) {
	cases := []struct {
		name      string
		body      interface{}
		tenantHdr string
		userHdr   string
		wantCode  int
	}{
		{
			name:      "rejects empty title",
			body:      map[string]interface{}{},
			tenantHdr: "1",
			userHdr:   "7",
			wantCode:  400,
		},
		{
			name:      "rejects missing tenant",
			body:      dto.CreateTicketRequest{Title: "Test", Priority: "low"},
			tenantHdr: "0",
			userHdr:   "7",
			wantCode:  401,
		},
		{
			name:      "rejects missing user",
			body:      dto.CreateTicketRequest{Title: "Test", Priority: "low"},
			tenantHdr: "1",
			userHdr:   "0",
			wantCode:  401,
		},
		{
			name:      "happy path",
			body:      dto.CreateTicketRequest{Title: "Server down", Description: "Production", Priority: "high"},
			tenantHdr: "1",
			userHdr:   "7",
			wantCode:  200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestHarness(t)
			w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
				tc.body,
				map[string]string{
					"X-Test-TenantID": tc.tenantHdr,
					"X-Test-UserID":   tc.userHdr,
				},
			)
			assert.Equal(t, tc.wantCode, w.Code, "body=%s", w.Body.String())
		})
	}
}

func TestHandler_Get_NotFoundTable(t *testing.T) {
	cases := []struct {
		name      string
		idParam   string
		tenantHdr string
		want      int
	}{
		// GetTicket 现支持业务工单号 fallback（非数字 ID 走 GetByNumber），
		// 无效/不存在的工单号统一返回 404 而非 400。
		{"invalid id", "abc", "1", 404},
		{"non-existing id", "999", "1", 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestHarness(t)
			w := doJSON(t, r, http.MethodGet,
				"/api/v1/tickets/"+tc.idParam, nil,
				map[string]string{"X-Test-TenantID": tc.tenantHdr, "X-Test-UserID": "7"},
			)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestHandler_Get_TenantIsolation(t *testing.T) {
	r, repo := newTestHarness(t)

	// Seed a ticket for tenant 1
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "T1 ticket", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	if len(repo.tickets) == 0 {
		t.Fatal("seed not recorded")
	}
	var id int
	for id = range repo.tickets {
		break
	}

	// Same id, but read with tenant 2 → must NOT leak data
	w = doJSON(t, r, http.MethodGet,
		"/api/v1/tickets/"+strconv.Itoa(id), nil,
		map[string]string{"X-Test-TenantID": "2", "X-Test-UserID": "8"},
	)
	assert.NotEqual(t, 200, w.Code, "tenant 2 must not see tenant 1's ticket")
}

func TestHandler_Update_TableDriven(t *testing.T) {
	r, repo := newTestHarness(t)

	// Seed ticket
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Original", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}

	cases := []struct {
		name string
		id   string
		body interface{}
		want int
	}{
		{"invalid id", "xyz", dto.UpdateTicketRequest{}, 400},
		{"valid update", strconv.Itoa(id), dto.UpdateTicketRequest{Title: "Updated"}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(t, r, http.MethodPut,
				"/api/v1/tickets/"+tc.id,
				tc.body,
				map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
			)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}

// F2 契约锁定（OB-3）：终态（resolved/closed）不允许经编辑直改状态，
// 必须走专用入口（POST /tickets/:id/resolve 或 /close），避免绕过解决方案前置。
func TestHandler_Update_TerminalStatusGuard(t *testing.T) {
	r, repo := newTestHarness(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Terminal guard", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	require.Equal(t, 200, w.Code, w.Body.String())

	var id int
	for id = range repo.tickets {
		break
	}

	for _, status := range []string{"resolved", "closed"} {
		t.Run("reject_"+status, func(t *testing.T) {
			w := doJSON(t, r, http.MethodPut,
				"/api/v1/tickets/"+strconv.Itoa(id),
				dto.UpdateTicketRequest{Status: status},
				map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
			)
			assert.Equal(t, 400, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "TICKET_TERMINAL_STATUS_REQUIRES_DEDICATED_ENDPOINT")
		})
	}

	// 非终态编辑仍放行（回归保护）
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Status: "in_progress"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())
}

// TestHandler_Update_StateMachineGuard（2026-10-14 缺口①回归锁定）：
// PUT 编辑链路此前绕过状态机（new→pending 静默成功），现必须经
// common.IsValidTicketStatusTransition 校验，非法转换返回 400。
func TestHandler_Update_StateMachineGuard(t *testing.T) {
	r, repo := newTestHarness(t)
	hdr := map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"}

	// new 工单
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "SM guard", Priority: "low"},
		hdr,
	)
	require.Equal(t, 200, w.Code, w.Body.String())
	var id int
	for id = range repo.tickets {
		break
	}

	// new → pending 是状态图外的非法转换，PUT 直改必须 400。
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Status: "pending"},
		hdr,
	)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid status transition")

	// new → in_progress 合法，放行。
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Status: "in_progress"},
		hdr,
	)
	assert.Equal(t, 200, w.Code, w.Body.String())

	// in_progress → open 亦非法（状态机不允许回退），PUT 必须拒绝。
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Status: "open"},
		hdr,
	)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid status transition")
}

func TestHandler_Delete_TableDriven(t *testing.T) {
	r, repo := newTestHarness(t)

	// Seed ticket
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "To delete", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}

	// Delete
	w = doJSON(t, r, http.MethodDelete,
		"/api/v1/tickets/"+strconv.Itoa(id), nil,
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	// Verify gone
	w = doJSON(t, r, http.MethodGet,
		"/api/v1/tickets/"+strconv.Itoa(id), nil,
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 404, w.Code)
}

// TestHandler_WritePath_RowLevelForbidden 锁定 P1-DataScope 行级写权限：
// 测试基建默认 role=agent；agent 仅能改/删自己创建的工单，
// 换一个非 owner 的 agent 身份访问必须 403。
func TestHandler_WritePath_RowLevelForbidden(t *testing.T) {
	r, repo := newTestHarness(t)

	// user 7 (agent) 创建工单 → requester_id=7
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Owner only", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}

	// user 8 (agent, 非 owner 非 assignee) 更新 → 403
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Title: "hijack"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "8"},
	)
	assert.Equal(t, 403, w.Code, w.Body.String())

	// user 8 删除 → 403
	w = doJSON(t, r, http.MethodDelete,
		"/api/v1/tickets/"+strconv.Itoa(id), nil,
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "8"},
	)
	assert.Equal(t, 403, w.Code, w.Body.String())

	// owner (user 7) 更新仍成功
	w = doJSON(t, r, http.MethodPut,
		"/api/v1/tickets/"+strconv.Itoa(id),
		dto.UpdateTicketRequest{Title: "owner edit"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())
	assert.NotEqual(t, "hijack", repo.tickets[id].Title, "被拒绝的修改不应落库")
}

// TestHandler_LifecycleOps_RowLevelForbidden 锁定 P1-DataScope #25 批次：
// resolve/close/escalate/updateStatus 四个生命周期写操作与 Update/Delete
// 同风险面——非 owner 普通角色（agent）操作他人工单必须 403，且行级
// AppError 不得被兑底吞成 500；owner 操作正常。
func TestHandler_LifecycleOps_RowLevelForbidden(t *testing.T) {
	r, repo := newTestHarness(t)

	// user 7 (agent) 创建工单 → requester_id=7
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Lifecycle guard", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}
	idStr := strconv.Itoa(id)

	// 非 owner（user 8, agent）四操作全部 403
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   interface{}
	}{
		{"resolve 非owner拒绝", http.MethodPost, "/api/v1/tickets/" + idStr + "/resolve", dto.ResolveTicketRequest{Resolution: "fixed"}},
		{"close 非owner拒绝", http.MethodPost, "/api/v1/tickets/" + idStr + "/close", dto.CloseTicketRequest{}},
		{"escalate 非owner拒绝", http.MethodPost, "/api/v1/tickets/" + idStr + "/escalate", dto.EscalateTicketRequest{Reason: "breach"}},
		{"updateStatus 非owner拒绝", http.MethodPut, "/api/v1/tickets/" + idStr + "/status", map[string]string{"status": "closed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(t, r, tc.method, tc.path, tc.body,
				map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "8"},
			)
			assert.Equal(t, 403, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "仅创建人、受理人或管理员可操作", "必须是行级守卫文案而非通用 RBAC 拒绝")
		})
	}

	// owner（user 7）：new 状态直接 close 必须被状态机拒绝（2026-10-14 缺口②修复，
	// 仅 resolved 可关闭），随后 resolve（new→resolved 快速解决）+ close 正常放行。
	w = doJSON(t, r, http.MethodPost, "/api/v1/tickets/"+idStr+"/close", dto.CloseTicketRequest{},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "仅已解决的工单可关闭")

	w = doJSON(t, r, http.MethodPost, "/api/v1/tickets/"+idStr+"/resolve",
		dto.ResolveTicketRequest{Resolution: "fixed by owner"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())

	w = doJSON(t, r, http.MethodPost, "/api/v1/tickets/"+idStr+"/close", dto.CloseTicketRequest{},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())

	// 终态重放拒绝（缺口②）：closed→closed 不得再返回成功。
	w = doJSON(t, r, http.MethodPost, "/api/v1/tickets/"+idStr+"/close", dto.CloseTicketRequest{},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 400, w.Code, w.Body.String())
}

// TestHandler_LifecycleOps_AdminLikeBypass 锁定管理角色全租户可写语义：
// 非 owner 的 admin-like 角色四操作放行。
func TestHandler_LifecycleOps_AdminLikeBypass(t *testing.T) {
	r, repo := newTestHarness(t)

	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Admin bypass", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}
	idStr := strconv.Itoa(id)

	w = doJSON(t, r, http.MethodPost, "/api/v1/tickets/"+idStr+"/resolve",
		dto.ResolveTicketRequest{Resolution: "fixed by admin"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "9", "X-Test-Role": "manager"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())
}

func TestHandler_AssignTicket(t *testing.T) {
	r, repo := newTestHarness(t)

	// Seed ticket
	w := doJSON(t, r, http.MethodPost, "/api/v1/tickets",
		dto.CreateTicketRequest{Title: "Unassigned", Priority: "low"},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)

	var id int
	for id = range repo.tickets {
		break
	}

	w = doJSON(t, r, http.MethodPost,
		"/api/v1/tickets/"+strconv.Itoa(id)+"/assign",
		dto.AssignTicketRequest{AssigneeID: 42},
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code)
}

func TestHandler_SearchTickets_EmptyKeyword(t *testing.T) {
	r, _ := newTestHarness(t)
	w := doJSON(t, r, http.MethodGet,
		"/api/v1/tickets/search?q=", nil,
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 400, w.Code)
}

// P2 富文本：handler 层创建/更新链路需服务端清洗并双写，空 HTML 更新不得覆盖已有值。
func TestService_RichTextSanitizeAndDoubleWrite(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, nil, zap.NewNop().Sugar())
	ctx := context.Background()

	created, err := svc.Create(ctx, 1, &CreateParams{
		Title:           "富文本工单",
		Description:     "纯文本描述",
		DescriptionHTML: `<p>正文</p><script>alert(1)</script><img src="data:image/png;base64,AAAA">`,
		Priority:        "medium",
		Type:            "incident",
		RequesterID:     7,
	})
	require.NoError(t, err)
	assert.Equal(t, "纯文本描述", created.Description)
	assert.Equal(t, "html", created.DescriptionFormat)
	assert.Contains(t, created.DescriptionHTML, "<p>正文</p>")
	assert.NotContains(t, created.DescriptionHTML, "<script")
	assert.NotContains(t, created.DescriptionHTML, "data:image")

	// 落库经 Repository 参数映射：HTML 字段确实写入（而非只在返回值上）。
	stored := repo.tickets[created.ID]
	require.NotNil(t, stored)
	assert.Equal(t, created.DescriptionHTML, stored.DescriptionHTML)
	assert.Equal(t, "html", stored.DescriptionFormat)

	// 更新未携带 descriptionHtml：保留已有 HTML（部分更新语义）。
	title := "改名"
	updated, err := svc.Update(ctx, 1, created.ID, &UpdateParams{
		Title:   &title,
		Version: created.Version,
	}, created.RequesterID, "end_user")
	require.NoError(t, err)
	assert.Equal(t, created.DescriptionHTML, updated.DescriptionHTML)
	assert.Equal(t, "html", updated.DescriptionFormat)

	// 更新携带 descriptionHtml：重新清洗并覆盖。
	newHTML := `<p>新正文</p><img src="data:image/png;base64,BBBB">`
	updated2, err := svc.Update(ctx, 1, created.ID, &UpdateParams{
		DescriptionHTML: &newHTML,
		Version:         updated.Version,
	}, created.RequesterID, "end_user")
	require.NoError(t, err)
	assert.Contains(t, updated2.DescriptionHTML, "新正文")
	assert.NotContains(t, updated2.DescriptionHTML, "data:image")

	// 未携带富文本的旧行为：保持 plain 语义（不写 HTML）。
	plain, err := svc.Create(ctx, 1, &CreateParams{
		Title:       "纯文本工单",
		Description: "只有纯文本",
		Priority:    "medium",
		Type:        "incident",
		RequesterID: 7,
	})
	require.NoError(t, err)
	assert.Empty(t, plain.DescriptionHTML)
	assert.Empty(t, plain.DescriptionFormat)
}

// TestUpdateTicketStatus_StateMachineAndProtectedResolved （D-7 回归）：
// PUT /tickets/:id/status 必须遵守状态机（closed 为终态），且 resolved 受保护
// （无解决方案时直改必须 400 且提示走 ResolveTicket）。
func TestUpdateTicketStatus_StateMachineAndProtectedResolved(t *testing.T) {
	r, repo := newTestHarness(t)
	hdr := map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"}

	repo.mu.Lock()
	repo.tickets[42] = &Ticket{
		ID: 42, TicketNumber: "TKT-TEST-42", Title: "closed ticket",
		Status: "closed", TenantID: 1, RequesterID: 7, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	repo.mu.Unlock()

	// closed 为终态：closed → open 必须被拒（400），不再静默放行。
	w := doJSON(t, r, http.MethodPut, "/api/v1/tickets/42/status",
		map[string]string{"status": "open"}, hdr)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid status transition")

	// resolved 受保护：in_progress → resolved 无解决方案时必须 400 且指向 ResolveTicket。
	repo.mu.Lock()
	repo.tickets[42].Status = "in_progress"
	repo.tickets[42].Resolution = nil
	repo.mu.Unlock()

	w2 := doJSON(t, r, http.MethodPut, "/api/v1/tickets/42/status",
		map[string]string{"status": "resolved"}, hdr)
	assert.Equal(t, 400, w2.Code, w2.Body.String())
	assert.Contains(t, w2.Body.String(), "ResolveTicket")

	// 合法转移不误伤：in_progress → pending。
	w3 := doJSON(t, r, http.MethodPut, "/api/v1/tickets/42/status",
		map[string]string{"status": "pending"}, hdr)
	assert.Equal(t, 200, w3.Code, w3.Body.String())
}

// TestHandler_List_FiltersPassedThrough（列表过滤补全回归）：
// 修复前 assigneeId/requesterId/type/dateFrom 等参数被绑定但 handler 未写入 filters，
// 自动化（PAT 调用方）按处理人/时间段筛选工单不可用。锁定这些参数必须透传到仓储层
// （键为仓储翻译器的 snake_case 契约）。
func TestHandler_List_FiltersPassedThrough(t *testing.T) {
	r, repo := newTestHarness(t)

	w := doJSON(t, r, http.MethodGet,
		"/api/v1/tickets?status=open&priority=high&type=incident&assigneeId=9&requesterId=7&categoryId=3&dateFrom=2026-01-01T00:00:00Z&dateTo=2026-12-31T00:00:00Z",
		nil,
		map[string]string{"X-Test-TenantID": "1", "X-Test-UserID": "7"},
	)
	assert.Equal(t, 200, w.Code, w.Body.String())

	f := repo.lastListFilters
	assert.NotNil(t, f, "filters must reach repository")
	assert.Equal(t, "open", f["status"])
	assert.Equal(t, "high", f["priority"])
	assert.Equal(t, "incident", f["type"])
	assert.Equal(t, 9, f["assignee_id"])
	assert.Equal(t, 7, f["requester_id"])
	assert.Equal(t, 3, f["category_id"])
	assert.Contains(t, f, "date_from")
	assert.Contains(t, f, "date_to")
}
