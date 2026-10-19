package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// T5-03a：附件服务未装配时 AI 附件端点 fail-closed（503）。
func TestAIAttachmentEndpointsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil)
	router := gin.New()
	router.POST("/api/v1/ai/attachments", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("user_id", 9)
		h.UploadAIAttachment(c)
	})
	router.GET("/api/v1/ai/attachments/:id/content", func(c *gin.Context) {
		c.Set("tenant_id", 7)
		c.Set("user_id", 9)
		h.DownloadAIAttachment(c)
	})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/ai/attachments", ""},
		{http.MethodGet, "/api/v1/ai/attachments/1/content", ""},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, tc.path)
	}
}

func TestBuildAIChatAttachmentContext(t *testing.T) {
	require.Equal(t, "原始问题", buildAIChatAttachmentContext("原始问题", nil))

	prompt := buildAIChatAttachmentContext("请看附件", []*ent.Attachment{
		{FileName: "截图.png", MimeType: "image/png", FileSize: 1024},
		{FileName: "日志.txt", MimeType: "text/plain", FileSize: 2048},
	})
	require.Contains(t, prompt, "请看附件")
	require.Contains(t, prompt, "截图.png（image/png，1024 bytes）")
	require.Contains(t, prompt, "日志.txt（text/plain，2048 bytes）")
	require.Contains(t, prompt, "未自动读取内容")
	require.Contains(t, prompt, "请用户补充文字描述")
}

// T5-03b：对话请求中的附件只接受“当前租户 + 当前用户 + ai_chat + active”的记录；
// 任一附件不可用即整体拒绝，且返回顺序与请求一致（去重后）。
func TestResolveAIChatAttachmentsScope(t *testing.T) {
	client := enttest.Open(t, "sqlite3", filepath.Join(t.TempDir(), "ai-chat-attachments.db")+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	handler := NewHandler(&Service{entClient: client})
	ctx := context.Background()

	create := func(tenantID, bizID, uploadedBy int, bizType, status, name string) int {
		t.Helper()
		row, err := client.Attachment.Create().
			SetTenantID(tenantID).
			SetBizType(bizType).
			SetBizID(bizID).
			SetUsage(service.AttachmentUsageAttachment).
			SetFileName(name).
			SetFilePath(filepath.ToSlash(filepath.Join("uploads", "ai_chat", name))).
			SetFileType("image/png").
			SetMimeType("image/png").
			SetFileSize(1024).
			SetUploadedBy(uploadedBy).
			SetStatus(status).
			Save(ctx)
		require.NoError(t, err)
		return row.ID
	}

	first := create(7, 9, 9, service.AttachmentBizTypeAIChat, service.AttachmentStatusActive, "first.png")
	second := create(7, 9, 9, service.AttachmentBizTypeAIChat, service.AttachmentStatusActive, "second.png")

	rows, err := handler.resolveAIChatAttachments(ctx, 7, 9, []int{second, first, second})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, second, rows[0].ID)
	require.Equal(t, first, rows[1].ID)

	for name, id := range map[string]int{
		"跨租户":     create(8, 9, 9, service.AttachmentBizTypeAIChat, service.AttachmentStatusActive, "tenant.png"),
		"跨用户":     create(7, 10, 10, service.AttachmentBizTypeAIChat, service.AttachmentStatusActive, "user.png"),
		"非 AI 宿主": create(7, 9, 9, service.AttachmentBizTypeTicket, service.AttachmentStatusActive, "ticket.png"),
		"已软删":     create(7, 9, 9, service.AttachmentBizTypeAIChat, service.AttachmentStatusDeleted, "deleted.png"),
	} {
		_, err := handler.resolveAIChatAttachments(ctx, 7, 9, []int{id})
		require.Error(t, err, name)
	}

	_, err = handler.resolveAIChatAttachments(ctx, 7, 9, []int{0})
	require.Error(t, err)
	_, err = handler.resolveAIChatAttachments(ctx, 7, 9, []int{first, second, first, second, first, second})
	require.Error(t, err)
}
