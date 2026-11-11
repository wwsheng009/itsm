package ticket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"itsm-backend/ent"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type notFoundTicketRepo struct {
	*mockRepository
}

func (r *notFoundTicketRepo) GetByID(_ context.Context, _ int, _ int) (*Ticket, error) {
	return nil, &ent.NotFoundError{}
}

// TestDeleteTicket_NotFoundReturns404 锁定跨租户/不存在删除的错误语义：
// 行级校验在目标租户内查不到工单时返回 ent.NotFoundError，handler 必须映射
// 404（不泄露资源是否存在），不能兑底 500。
func TestDeleteTicket_NotFoundReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &notFoundTicketRepo{mockRepository: newMockRepository()}
	h := NewHandler(NewService(repo, nil, zap.NewNop().Sugar()))
	r := gin.New()
	auth := func(c *gin.Context) {
		c.Set(middleware.TenantContextKey, &middleware.TenantContext{TenantID: 2})
		c.Set("user_id", 1)
		c.Set("role", "agent")
		c.Next()
	}
	api := r.Group("/api/v1", auth)
	api.DELETE("/tickets/:id", h.DeleteTicket)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tickets/999999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"code":4004`)
	assert.Contains(t, w.Body.String(), "工单不存在")
}