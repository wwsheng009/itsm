package attachment

// handler_test.go 覆盖 BE-4 验收口径：A1-A6 端点成功路径 + 403/404/413/415/422/429
// 错误映射，以及 §4.2 宿主权限表的两层闸门（路由级兜底码 + 宿主维度动态复核）。
//
// 依赖说明：enttest(sqlite3) 建内存库，宿主行（tenant/user/ticket）满足外键；
// 路由声明与 router/attachment_routes.go 保持同构（权限码逐条一致），仅上下文注入
// 换成测试夹具；role=super_admin 时 AuthorizeResource 直通，无需播种权限表。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// handlerTestPNG 标准 PNG magic bytes + 可辨识载荷，保证嗅探结果为 image/png。
var handlerTestPNG = append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("attachment-handler-test-payload")...)

type testEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type attachmentTestEnv struct {
	t       *testing.T
	router  *gin.Engine
	handler *Handler
	svc     *service.AttachmentService
	client  *ent.Client
	tenant  *ent.Tenant
	user    *ent.User
	ticket  *ent.Ticket
	root    string
	role    string

	// BE-5 域内别名宿主
	article        *ent.KnowledgeArticle
	serviceRequest *ent.ServiceRequest
}

func newAttachmentTestEnv(t *testing.T) *attachmentTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:attachment_handler_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	root := t.TempDir()
	logger := zaptest.NewLogger(t).Sugar()
	svc := service.NewAttachmentService(client, logger, service.NewLocalStorageProvider(root))
	h := NewHandler(svc, logger)

	ctx := context.Background()
	tn, err := client.Tenant.Create().
		SetName("附件端点测试租户").
		SetCode(fmt.Sprintf("attach-handler-%d", time.Now().UnixNano())).
		SetDomain("attach-handler.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	u, err := client.User.Create().
		SetUsername(fmt.Sprintf("attach-handler-user-%d", tn.ID)).
		SetEmail("attach-handler@example.com").
		SetName("附件端点测试用户").
		SetPasswordHash("hashed-for-test").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)
	tk, err := client.Ticket.Create().
		SetTitle("附件端点测试工单").
		SetTicketNumber(fmt.Sprintf("T-ATT-%d", tn.ID)).
		SetRequesterID(u.ID).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)
	article, err := client.KnowledgeArticle.Create().
		SetTitle("附件别名测试文章").
		SetContent("<p>知识正文</p>").
		SetAuthorID(u.ID).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)
	svcReq, err := client.ServiceRequest.Create().
		SetTitle("附件别名测试服务请求").
		SetCatalogID(1).
		SetRequesterID(u.ID).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)

	env := &attachmentTestEnv{
		t: t, handler: h, svc: svc, client: client,
		tenant: tn, user: u, ticket: tk, root: root, role: "super_admin",
		article: article, serviceRequest: svcReq,
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		tenantID, userID, role := env.tenant.ID, env.user.ID, env.role
		if v := c.GetHeader("X-Test-Tenant"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				tenantID = n
			}
		}
		if v := c.GetHeader("X-Test-User"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				userID = n
			}
		}
		if v := c.GetHeader("X-Test-Role"); v != "" {
			role = v
		}
		c.Set("tenant_id", tenantID)
		c.Set("user_id", userID)
		c.Set("role", role)
		c.Set("client", env.client)
		c.Next()
	})

	api := r.Group("/api/v1")
	attachments := api.Group("/attachments")
	attachments.POST("", middleware.RequirePermission("attachment", "write"), h.Upload)
	attachments.GET("", middleware.RequirePermission("attachment", "read"), h.List)
	attachments.POST("/batch-query", middleware.RequirePermission("attachment", "read"), h.BatchQuery)
	attachments.GET("/:id", middleware.RequirePermission("attachment", "read"), h.Get)
	attachments.GET("/:id/content", middleware.RequirePermission("attachment", "read"), h.Download)
	attachments.DELETE("/:id", middleware.RequirePermission("attachment", "delete"), h.Delete)

	// 域内别名（BE-5）：与 router/knowledge_routes.go、router/service_request_routes.go 同构。
	articles := api.Group("/knowledge/articles")
	articles.GET("/:id/attachments", middleware.RequirePermission("knowledge", "read"), h.AliasList(service.AttachmentBizTypeKnowledgeArticle))
	articles.POST("/:id/attachments", middleware.RequirePermission("knowledge", "write"), h.AliasUpload(service.AttachmentBizTypeKnowledgeArticle))
	articles.GET("/:id/attachments/:ref", middleware.RequirePermission("knowledge", "read"), h.AliasDownload(service.AttachmentBizTypeKnowledgeArticle, false))
	articles.GET("/:id/attachments/:ref/download", middleware.RequirePermission("knowledge", "read"), h.AliasDownload(service.AttachmentBizTypeKnowledgeArticle, false))
	articles.GET("/:id/attachments/:ref/preview", middleware.RequirePermission("knowledge", "read"), h.AliasDownload(service.AttachmentBizTypeKnowledgeArticle, true))
	articles.DELETE("/:id/attachments/:ref", middleware.RequirePermission("knowledge", "delete"), h.AliasDelete(service.AttachmentBizTypeKnowledgeArticle))

	serviceRequests := api.Group("/service-requests")
	serviceRequests.GET("/:id/attachments", middleware.RequirePermission("service_request", "read"), h.AliasList(service.AttachmentBizTypeServiceRequest))
	serviceRequests.POST("/:id/attachments", middleware.RequirePermission("service_request", "write"), h.AliasUpload(service.AttachmentBizTypeServiceRequest))
	serviceRequests.GET("/:id/attachments/:ref", middleware.RequirePermission("service_request", "read"), h.AliasDownload(service.AttachmentBizTypeServiceRequest, false))
	serviceRequests.GET("/:id/attachments/:ref/preview", middleware.RequirePermission("service_request", "read"), h.AliasDownload(service.AttachmentBizTypeServiceRequest, true))
	serviceRequests.DELETE("/:id/attachments/:ref", middleware.RequirePermission("service_request", "delete"), h.AliasDelete(service.AttachmentBizTypeServiceRequest))

	env.router = r
	return env
}

// ---------------------------------------------------------------------------
// 夹具辅助
// ---------------------------------------------------------------------------

func (e *attachmentTestEnv) do(method, target, contentType string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) testEnvelope {
	t.Helper()
	var env testEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "body=%s", w.Body.String())
	return env
}

func decodeRef(t *testing.T, env testEnvelope) AttachmentRef {
	t.Helper()
	var ref AttachmentRef
	require.NoError(t, json.Unmarshal(env.Data, &ref))
	return ref
}

func multipartBody(t *testing.T, fields map[string]string, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range fields {
		require.NoError(t, writer.WriteField(k, v))
	}
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func (e *attachmentTestEnv) upload(filename string, content []byte, fields map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	body, contentType := multipartBody(e.t, fields, filename, content)
	return e.do(http.MethodPost, "/api/v1/attachments", contentType, body, nil)
}

// uploadPNG 上传一张 PNG 并返回契约 DTO（失败即终止用例）。
func (e *attachmentTestEnv) uploadPNG(fields map[string]string) AttachmentRef {
	e.t.Helper()
	w := e.upload("测试 图片.png", handlerTestPNG, fields)
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	env := decodeEnvelope(e.t, w)
	require.Equal(e.t, common.SuccessCode, env.Code)
	return decodeRef(e.t, env)
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	require.NoError(t, filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	}))
	return count
}

// seedRoleWithPermissions 播种「DB 已配置」角色（DBOnly 模式下 configured 分支以 DB 为准），
// 用于验证路由级兜底码通过、但宿主维度动态复核拒绝的越权场景。
func seedRoleWithPermissions(t *testing.T, client *ent.Client, tenantID int, code string, perms ...[2]string) {
	t.Helper()
	ctx := context.Background()
	role, err := client.Role.Create().
		SetName(code).
		SetCode(code).
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)
	for _, p := range perms {
		perm, err := client.Permission.Create().
			SetCode(p[0] + ":" + p[1]).
			SetName(p[0] + ":" + p[1]).
			SetResource(p[0]).
			SetAction(p[1]).
			SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
		_, err = client.RolePermission.Create().
			SetRoleID(role.ID).
			SetPermissionID(perm.ID).
			SetTenantID(tenantID).
			Save(ctx)
		require.NoError(t, err)
	}
}

// ---------------------------------------------------------------------------
// A1 上传
// ---------------------------------------------------------------------------

func TestUploadEndpointSuccessAndIdempotentReplay(t *testing.T) {
	env := newAttachmentTestEnv(t)
	fields := map[string]string{
		"bizType":     "ticket",
		"bizId":       strconv.Itoa(env.ticket.ID),
		"usage":       service.AttachmentUsageInlineImage,
		"clientToken": "tok-1",
	}

	ref := env.uploadPNG(fields)
	assert.Positive(t, ref.ID)
	assert.Equal(t, "ticket", ref.BizType)
	assert.Equal(t, env.ticket.ID, ref.BizID)
	assert.Equal(t, service.AttachmentUsageInlineImage, ref.Usage)
	assert.Equal(t, "测试 图片.png", ref.FileName)
	assert.Equal(t, len(handlerTestPNG), ref.FileSize)
	assert.Equal(t, "image/png", ref.MimeType)
	assert.Equal(t, env.user.ID, ref.UploadedBy)
	assert.Equal(t, fmt.Sprintf("/api/v1/attachments/%d/content", ref.ID), ref.FileURL)
	assert.Equal(t, fmt.Sprintf("/api/v1/attachments/%d/content?disposition=inline", ref.ID), ref.PreviewURL)
	assert.NotEmpty(t, ref.CreatedAt)

	// 幂等重放：同 clientToken → 同一 ID，且不产生第二条记录/第二个物理文件。
	replay := env.uploadPNG(fields)
	assert.Equal(t, ref.ID, replay.ID)

	count, err := env.client.Attachment.Query().Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, countFiles(t, env.root))

	// 默认 usage 归一为 attachment；previewUrl 由 MIME 决定（图片即有）。
	other := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})
	assert.Equal(t, service.AttachmentUsageAttachment, other.Usage)
	assert.NotEmpty(t, other.PreviewURL)
	assert.Equal(t, 2, countFiles(t, env.root))
}

func TestUploadEndpointHostNotFoundReturns404(t *testing.T) {
	env := newAttachmentTestEnv(t)

	w := env.upload("图片.png", handlerTestPNG, map[string]string{
		"bizType": "ticket",
		"bizId":   "99999999",
	})

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	resp := decodeEnvelope(t, w)
	assert.Equal(t, common.AttachmentHostNotFoundCode, resp.Code)
}

func TestUploadEndpointInvalidFilenameReturns400(t *testing.T) {
	env := newAttachmentTestEnv(t)

	// ".." 经清洗后为空 → ErrAttachmentInvalidFilename → 400/6102。
	w := env.upload("..", handlerTestPNG, map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	resp := decodeEnvelope(t, w)
	assert.Equal(t, common.AttachmentInvalidFilenameCode, resp.Code)
}

func TestUploadEndpointTooLargeReturns413(t *testing.T) {
	env := newAttachmentTestEnv(t)

	tooLarge := bytes.Repeat([]byte{'A'}, 10*1024*1024+1) // 默认上限 10MB（§3.4）
	w := env.upload("big.bin", tooLarge, map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	resp := decodeEnvelope(t, w)
	assert.Equal(t, common.AttachmentTooLargeCode, resp.Code)
	assert.Contains(t, resp.Message, strconv.Itoa(env.svc.MaxFileSizeMB()))
}

func TestUploadEndpointDisallowedTypeReturns415(t *testing.T) {
	env := newAttachmentTestEnv(t)

	// 扩展名 + magic 均不在白名单（MZ 头）→ ErrAttachmentTypeRejected → 415/6104。
	w := env.upload("payload.exe", append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0x00}, 32)...), map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code, w.Body.String())
	resp := decodeEnvelope(t, w)
	assert.Equal(t, common.AttachmentTypeNotAllowedCode, resp.Code)
}

func TestUploadEndpointParamValidationReturns400(t *testing.T) {
	env := newAttachmentTestEnv(t)

	for name, fields := range map[string]map[string]string{
		"缺 bizType": {"bizId": strconv.Itoa(env.ticket.ID)},
		"bizId 非法":  {"bizType": "ticket", "bizId": "abc"},
		"bizId 非正":  {"bizType": "ticket", "bizId": "0"},
	} {
		w := env.upload("图片.png", handlerTestPNG, fields)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", name, w.Body.String())
		assert.Equal(t, common.ParamErrorCode, decodeEnvelope(t, w).Code, name)
	}

	// clientToken 超长（>64）同样 400。
	w := env.upload("图片.png", handlerTestPNG, map[string]string{
		"bizType":     "ticket",
		"bizId":       strconv.Itoa(env.ticket.ID),
		"clientToken": strings.Repeat("t", 65),
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, common.ParamErrorCode, decodeEnvelope(t, w).Code)

	// 缺少认证上下文 → 401/2001（不进入业务层）。
	body, contentType := multipartBody(t, map[string]string{"bizType": "ticket", "bizId": "1"}, "图片.png", handlerTestPNG)
	anon := env.do(http.MethodPost, "/api/v1/attachments", contentType, body, map[string]string{"X-Test-Tenant": "0"})
	assert.Equal(t, http.StatusUnauthorized, anon.Code, anon.Body.String())
	assert.Equal(t, common.AuthFailedCode, decodeEnvelope(t, anon).Code)
}

// ---------------------------------------------------------------------------
// A2/A3 列表与元数据
// ---------------------------------------------------------------------------

func TestListEndpointReturnsAttachmentsAndTotal(t *testing.T) {
	env := newAttachmentTestEnv(t)
	first := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
		"usage":   service.AttachmentUsageInlineImage,
	})
	env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
		"usage":   service.AttachmentUsageAttachment,
	})

	w := env.do(http.MethodGet,
		fmt.Sprintf("/api/v1/attachments?bizType=ticket&bizId=%d", env.ticket.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := decodeEnvelope(t, w)
	require.Equal(t, common.SuccessCode, resp.Code)

	var list ListResponse
	require.NoError(t, json.Unmarshal(resp.Data, &list))
	assert.Equal(t, 2, list.Total)
	require.Len(t, list.Attachments, 2)

	// usage 过滤 + 分页参数钳制（pageSize 上限 200）。
	w = env.do(http.MethodGet,
		fmt.Sprintf("/api/v1/attachments?bizType=ticket&bizId=%d&usage=inline_image&pageSize=99999", env.ticket.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &list))
	assert.Equal(t, 1, list.Total)
	require.Len(t, list.Attachments, 1)
	assert.Equal(t, first.ID, list.Attachments[0].ID)

	// 缺宿主参数 → 400/1001。
	w = env.do(http.MethodGet, "/api/v1/attachments", "", nil, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, common.ParamErrorCode, decodeEnvelope(t, w).Code)
}

func TestGetEndpointReturnsRefAndHidesCrossTenant(t *testing.T) {
	env := newAttachmentTestEnv(t)
	ref := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})

	w := env.do(http.MethodGet, fmt.Sprintf("/api/v1/attachments/%d", ref.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := decodeRef(t, decodeEnvelope(t, w))
	assert.Equal(t, ref.ID, got.ID)
	assert.Equal(t, "ticket", got.BizType)

	// 跨租户：不泄漏存在性，统一 404/4004。
	w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/attachments/%d", ref.ID), "", nil,
		map[string]string{"X-Test-Tenant": strconv.Itoa(env.tenant.ID + 1000)})
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, common.NotFoundCode, decodeEnvelope(t, w).Code)

	// 非法 ID → 400/1001。
	w = env.do(http.MethodGet, "/api/v1/attachments/abc", "", nil, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, common.ParamErrorCode, decodeEnvelope(t, w).Code)
}

// ---------------------------------------------------------------------------
// A4 下载/预览
// ---------------------------------------------------------------------------

func TestDownloadEndpointServesContentRangeAndDisposition(t *testing.T) {
	env := newAttachmentTestEnv(t)
	ref := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})
	target := fmt.Sprintf("/api/v1/attachments/%d/content", ref.ID)

	// 默认强制下载 + nosniff。
	w := env.do(http.MethodGet, target, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, handlerTestPNG, w.Body.Bytes())
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment"),
		"默认必须强制下载: %s", w.Header().Get("Content-Disposition"))

	// 位图白名单 + disposition=inline → 内联预览。
	w = env.do(http.MethodGet, target+"?disposition=inline", "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline"),
		"位图应允许内联: %s", w.Header().Get("Content-Disposition"))

	// Range → 206 且只回请求片段。
	w = env.do(http.MethodGet, target, "", nil, map[string]string{"Range": "bytes=0-3"})
	assert.Equal(t, http.StatusPartialContent, w.Code, w.Body.String())
	assert.Equal(t, handlerTestPNG[:4], w.Body.Bytes())
	assert.Equal(t, "bytes", w.Header().Get("Accept-Ranges"))

	// 非位图（文本）即使要求 inline 也强制下载，防存储型 XSS。
	txtUpload := env.upload("备注.txt", []byte("hello attachment"), map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	})
	require.Equal(t, http.StatusOK, txtUpload.Code, txtUpload.Body.String())
	txtRef := decodeRef(t, decodeEnvelope(t, txtUpload))
	w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/attachments/%d/content?disposition=inline", txtRef.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment"),
		"非位图不得内联: %s", w.Header().Get("Content-Disposition"))
}

// ---------------------------------------------------------------------------
// A5 删除
// ---------------------------------------------------------------------------

func TestDeleteEndpointInUseReturns409ThenIdempotent(t *testing.T) {
	env := newAttachmentTestEnv(t)
	ref := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
		"usage":   service.AttachmentUsageInlineImage,
	})
	target := fmt.Sprintf("/api/v1/attachments/%d", ref.ID)

	// 宿主正文引用 → 409/6105，且状态不变。
	_, err := env.ticket.Update().
		SetDescriptionHTML(fmt.Sprintf(`<p>图</p><img src="/api/v1/attachments/%d/content" data-attachment-id="%d">`, ref.ID, ref.ID)).
		Save(context.Background())
	require.NoError(t, err)

	w := env.do(http.MethodDelete, target, "", nil, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, common.AttachmentInUseCode, decodeEnvelope(t, w).Code)

	att, err := env.client.Attachment.Get(context.Background(), ref.ID)
	require.NoError(t, err)
	assert.Equal(t, service.AttachmentStatusActive, att.Status)

	// 移除引用 → 200 + deleted；重复删除幂等，仍为 200。
	_, err = env.ticket.Update().SetDescriptionHTML("<p>已移除图片</p>").Save(context.Background())
	require.NoError(t, err)

	w = env.do(http.MethodDelete, target, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var deleted DeleteResponse
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &deleted))
	assert.Equal(t, ref.ID, deleted.ID)
	assert.Equal(t, service.AttachmentStatusDeleted, deleted.Status)

	w = env.do(http.MethodDelete, target, "", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 软删后不可再下载（404）。
	w = env.do(http.MethodGet, target+"/content", "", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestDeleteEndpointCommentReferenceReturns409 评论引用（usage='comment_attachment' 且
// ticket_comments.attachments 含该 ID）与宿主正文引用共用同一保护闸门（BE-9）：
// 未解除引用前 409/6105 且状态不变；评论清空引用后放行（物理文件由 BE-8 保留期任务回收）。
func TestDeleteEndpointCommentReferenceReturns409(t *testing.T) {
	env := newAttachmentTestEnv(t)
	ref := env.uploadPNG(map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
		"usage":   service.AttachmentUsageCommentAttachment,
	})
	target := fmt.Sprintf("/api/v1/attachments/%d", ref.ID)

	comment, err := env.client.TicketComment.Create().
		SetTicketID(env.ticket.ID).
		SetUserID(env.user.ID).
		SetContent("带附件评论").
		SetTenantID(env.tenant.ID).
		SetAttachments([]int{ref.ID}).
		Save(context.Background())
	require.NoError(t, err)

	w := env.do(http.MethodDelete, target, "", nil, nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, common.AttachmentInUseCode, decodeEnvelope(t, w).Code)

	att, err := env.client.Attachment.Get(context.Background(), ref.ID)
	require.NoError(t, err)
	assert.Equal(t, service.AttachmentStatusActive, att.Status)

	_, err = comment.Update().SetAttachments([]int{}).Save(context.Background())
	require.NoError(t, err)
	w = env.do(http.MethodDelete, target, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// A6 批量回填
// ---------------------------------------------------------------------------

func TestBatchQueryEndpointReturnsRefsAndEnforcesLimit(t *testing.T) {
	env := newAttachmentTestEnv(t)
	first := env.uploadPNG(map[string]string{"bizType": "ticket", "bizId": strconv.Itoa(env.ticket.ID)})
	second := env.uploadPNG(map[string]string{"bizType": "ticket", "bizId": strconv.Itoa(env.ticket.ID)})

	payload, err := json.Marshal(map[string][]int{"ids": {first.ID, second.ID, first.ID, 0}})
	require.NoError(t, err)
	w := env.do(http.MethodPost, "/api/v1/attachments/batch-query", "application/json", bytes.NewReader(payload), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var refs []AttachmentRef
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &refs))
	assert.Len(t, refs, 2, "重复/非法 ID 应去重过滤")

	// 超 200 条 → 400/1001（不触达业务层）。
	ids := make([]int, service.AttachmentMaxBatchIDs+1)
	for i := range ids {
		ids[i] = i + 1
	}
	payload, err = json.Marshal(map[string][]int{"ids": ids})
	require.NoError(t, err)
	w = env.do(http.MethodPost, "/api/v1/attachments/batch-query", "application/json", bytes.NewReader(payload), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, common.ParamErrorCode, decodeEnvelope(t, w).Code)
}

// ---------------------------------------------------------------------------
// 权限两层闸门（§4.2/§4.3）
// ---------------------------------------------------------------------------

func TestRequirePermissionRouteLevelDeniesUnknownRole(t *testing.T) {
	env := newAttachmentTestEnv(t)

	// 未配置角色（无 DB 角色行、无硬编码兜底）→ 路由级闸门 403/2003。
	w := env.do(http.MethodGet,
		fmt.Sprintf("/api/v1/attachments?bizType=ticket&bizId=%d", env.ticket.ID), "", nil,
		map[string]string{"X-Test-Role": fmt.Sprintf("attachment-handler-unknown-%d", time.Now().UnixNano())})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, common.ForbiddenCode, decodeEnvelope(t, w).Code)
}

// TestHostLevelAuthorizationDeniesCrossResource 验证第二层闸门：角色持有兜底码
// attachment:read 能过路由级静态声明，但没有宿主 ticket:read，仍被动态复核拒绝。
func TestHostLevelAuthorizationDeniesCrossResource(t *testing.T) {
	env := newAttachmentTestEnv(t)
	roleCode := fmt.Sprintf("attachment-only-reader-%d", time.Now().UnixNano())
	seedRoleWithPermissions(t, env.client, env.tenant.ID, roleCode, [2]string{"attachment", "read"})
	headers := map[string]string{"X-Test-Role": roleCode}
	target := fmt.Sprintf("/api/v1/attachments?bizType=ticket&bizId=%d", env.ticket.ID)

	w := env.do(http.MethodGet, target, "", nil, headers)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Equal(t, common.ForbiddenCode, decodeEnvelope(t, w).Code)

	// 该角色同样不能上传附件（路由级 attachment:write 即拒绝）。
	body, contentType := multipartBody(t, map[string]string{
		"bizType": "ticket",
		"bizId":   strconv.Itoa(env.ticket.ID),
	}, "图片.png", handlerTestPNG)
	w = env.do(http.MethodPost, "/api/v1/attachments", contentType, body, headers)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	// 未纳入 §4.2 的系统级 biz_type（ticket_comment 是已注册宿主但无宿主权限映射）
	// 由兜底码承担最终闸门（持 attachment:read 可读）。
	comment, err := env.client.TicketComment.Create().
		SetTicketID(env.ticket.ID).
		SetUserID(env.user.ID).
		SetContent("无宿主权限映射的评论").
		SetTenantID(env.tenant.ID).
		Save(context.Background())
	require.NoError(t, err)
	w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/attachments?bizType=ticket_comment&bizId=%d", comment.ID), "", nil, headers)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// TestAttachmentHostPoliciesMatchContract 锁定 §4.2 权威表，防止权限口径漂移。
func TestAttachmentHostPoliciesMatchContract(t *testing.T) {
	want := map[string]hostPolicy{
		service.AttachmentBizTypeTicket:           {resource: "ticket", read: "read", write: "create", remove: "delete"},
		service.AttachmentBizTypeKnowledgeArticle: {resource: "knowledge", read: "read", write: "write", remove: "delete"},
		service.AttachmentBizTypeServiceRequest:   {resource: "service_request", read: "read", write: "write", remove: "delete"},
		service.AttachmentBizTypeIncident:         {resource: "incident", read: "read", write: "write", remove: "delete"},
		service.AttachmentBizTypeProblem:          {resource: "problem", read: "read", write: "write", remove: "delete"},
		service.AttachmentBizTypeChange:           {resource: "change", read: "read", write: "write", remove: "delete"},
		"release":                                 {resource: "release", read: "read", write: "write", remove: "delete"},
		"cmdb_ci":                                 {resource: "cmdb", read: "read", write: "write", remove: "delete"},
	}
	assert.Equal(t, want, attachmentHostPolicies)
}

// ---------------------------------------------------------------------------
// 6106 / 6107 / 429 映射（BE-4 验收：6101-6107 全码 + 429 均有用例）
// ---------------------------------------------------------------------------

// TestRespondErrorMapsQuotaExceededTo422 6106 的生产者（配额校验）属 BE-8，
// 此处锁定 service 错误 → HTTP 契约的映射不退化。
func TestRespondErrorMapsQuotaExceededTo422(t *testing.T) {
	env := newAttachmentTestEnv(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	env.handler.respondError(c, fmt.Errorf("%w: tenant=%d", service.ErrAttachmentQuotaExceeded, env.tenant.ID), "附件上传失败")

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	resp := decodeEnvelope(t, w)
	assert.Equal(t, common.AttachmentQuotaExceededCode, resp.Code)
}

// TestRateLimitMiddlewareReturns429Envelope 回归 2026-09-22 修复：裸 429 当业务码
// 未被响应层登记时会静默回落 HTTP 200；登记 TooManyRequestsCode 后必须真实 429 且
// 包络内 code=429。
func TestRateLimitMiddlewareReturns429Envelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RateLimitMiddleware(middleware.NewRateLimiter(1, time.Minute)))
	router.GET("/api/v1/attachments", func(c *gin.Context) { common.Success(c, nil) })

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/attachments", nil))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/attachments", nil))
	assert.Equal(t, http.StatusTooManyRequests, second.Code, second.Body.String())
	assert.Equal(t, common.TooManyRequestsCode, decodeEnvelope(t, second).Code)
}

// TestRateLimitedAttachmentCodeMapsTo429 6107 保留给附件专属限流（§3.5-9 默认不新增阈值），
// 映射必须先行可用。
func TestRateLimitedAttachmentCodeMapsTo429(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	common.Fail(c, common.AttachmentRateLimitedCode, "上传过于频繁，请稍后重试")

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, common.AttachmentRateLimitedCode, decodeEnvelope(t, w).Code)
}

// ---------------------------------------------------------------------------
// BE-5 域内别名（§3.2：宿主由路径注入，权限复用宿主码）
// ---------------------------------------------------------------------------

// TestAliasKnowledgeArticleRoundTrip 覆盖知识库别名的完整闭环：上传 → 列表 → 原文件
// 下载 → /preview 内联 → 别名软删；各步的 bizType/bizId 必须落在路径宿主上。
func TestAliasKnowledgeArticleRoundTrip(t *testing.T) {
	env := newAttachmentTestEnv(t)
	base := fmt.Sprintf("/api/v1/knowledge/articles/%d/attachments", env.article.ID)

	// POST 别名上传：无需 bizType/bizId 表单字段，宿主来自路径。
	body, contentType := multipartBody(t, map[string]string{"usage": service.AttachmentUsageAttachment}, "知识图.png", handlerTestPNG)
	w := env.do(http.MethodPost, base, contentType, body, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ref := decodeRef(t, decodeEnvelope(t, w))
	assert.Equal(t, service.AttachmentBizTypeKnowledgeArticle, ref.BizType)
	assert.Equal(t, env.article.ID, ref.BizID)

	// GET 别名列表：仅含该宿主附件。
	w = env.do(http.MethodGet, base, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list ListResponse
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &list))
	require.Len(t, list.Attachments, 1)
	assert.Equal(t, ref.ID, list.Attachments[0].ID)
	assert.Equal(t, 1, list.Total)

	// GET 别名下载：/:ref 与 /:ref/download 等价；/preview 对白名单位图给 inline。
	w = env.do(http.MethodGet, fmt.Sprintf("%s/%d", base, ref.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, handlerTestPNG, w.Body.Bytes())
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment"))

	w = env.do(http.MethodGet, fmt.Sprintf("%s/%d/download", base, ref.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, handlerTestPNG, w.Body.Bytes())

	w = env.do(http.MethodGet, fmt.Sprintf("%s/%d/preview", base, ref.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline"),
		"别名 /preview 应内联位图: %s", w.Header().Get("Content-Disposition"))

	// DELETE 别名软删：幂等 200，软删后列表不再返回。
	w = env.do(http.MethodDelete, fmt.Sprintf("%s/%d", base, ref.ID), "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var deleted DeleteResponse
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &deleted))
	assert.Equal(t, service.AttachmentStatusDeleted, deleted.Status)

	w = env.do(http.MethodDelete, fmt.Sprintf("%s/%d", base, ref.ID), "", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String(), "重复删除应幂等")

	w = env.do(http.MethodGet, base, "", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(decodeEnvelope(t, w).Data, &list))
	assert.Zero(t, list.Total, "软删后别名列表不应再返回该附件")
}

// TestAliasServiceRequestUploadAndCrossHostIsolation 覆盖服务请求别名上传，以及三条
// 安全不变量：跨宿主引用按 404 处理（不泄露附件存在性）、非法宿主/附件 ID 统一 404、
// 宿主不存在仍走 6101。
func TestAliasServiceRequestUploadAndCrossHostIsolation(t *testing.T) {
	env := newAttachmentTestEnv(t)

	// 服务请求别名上传（宿主 = service_request）。
	base := fmt.Sprintf("/api/v1/service-requests/%d/attachments", env.serviceRequest.ID)
	body, contentType := multipartBody(t, nil, "申请附件.png", handlerTestPNG)
	w := env.do(http.MethodPost, base, contentType, body, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ref := decodeRef(t, decodeEnvelope(t, w))
	assert.Equal(t, service.AttachmentBizTypeServiceRequest, ref.BizType)
	assert.Equal(t, env.serviceRequest.ID, ref.BizID)

	// 同一附件换到知识库宿主的别名路径 → 404（归属复核先于响应体输出）。
	w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/knowledge/articles/%d/attachments/%d", env.article.ID, ref.ID), "", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	w = env.do(http.MethodDelete, fmt.Sprintf("/api/v1/knowledge/articles/%d/attachments/%d", env.article.ID, ref.ID), "", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// 跨宿主被拒后附件仍可用（未被误删）。
	w = env.do(http.MethodGet, fmt.Sprintf("%s/%d", base, ref.ID), "", nil, nil)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 非法宿主 ID / 非法附件 ref：统一 404（不返回 400/403，避免探测）。
	w = env.do(http.MethodGet, "/api/v1/knowledge/articles/abc/attachments", "", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	w = env.do(http.MethodGet, fmt.Sprintf("/api/v1/knowledge/articles/%d/attachments/not-a-number", env.article.ID), "", nil, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// 宿主不存在（ID 合法但无行）→ 404/6101。
	body, contentType = multipartBody(t, nil, "孤儿附件.png", handlerTestPNG)
	w = env.do(http.MethodPost, "/api/v1/service-requests/999999/attachments", contentType, body, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, common.AttachmentHostNotFoundCode, decodeEnvelope(t, w).Code)
}
