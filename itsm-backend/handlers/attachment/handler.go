// Package attachment 通用附件 HTTP handler（A1-A6 端点，契约见
// docs/plan/generic-attachment-richtext-control-plan.md §3.2）。
//
// 职责边界：本包只做参数解析、响应封装与错误码映射（61xx）；业务规则（宿主存在性、
// 幂等 clientToken、加固四件套、软删与引用保护）全部在 service.AttachmentService。
//
// 权限模型（§4.2/§4.3，两层）：
//  1. 路由级静态声明：通用路由挂兜底码 attachment:read/write/delete
//     （router/attachment_routes.go）；域内别名路由静态声明宿主权限码。
//  2. 宿主维度动态复核：本包按 §4.2 权威表（biz_type → 宿主资源/动作）再校验一次，
//     避免"仅持兜底码即可读写任意宿主附件"的越权放大；未纳入 §4.2 的 biz_type
//     （系统级/无宿主）以路由级兜底码为最终闸门。
//
// 响应约定：成功一律 HTTP 200 + code=0（common.Success），不引入 201/204（§3.4）。
package attachment

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	attachmentDispositionAttachment = "attachment"
	attachmentDispositionInline     = "inline"
	attachmentDefaultPageSize       = 20
	attachmentMaxPageSize           = 200
)

// AttachmentRef A1-A6 统一响应体；字段与 §3.1 TS 契约 AttachmentRef 同名同义。
type AttachmentRef struct {
	ID         int    `json:"id"`
	BizType    string `json:"bizType"`
	BizID      int    `json:"bizId"`
	Usage      string `json:"usage"`
	FileName   string `json:"fileName"`
	FileSize   int    `json:"fileSize"`
	MimeType   string `json:"mimeType"`
	FileURL    string `json:"fileUrl,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	UploadedBy int    `json:"uploadedBy,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// ListResponse A2 列表响应（data: {attachments, total}）。
type ListResponse struct {
	Attachments []AttachmentRef `json:"attachments"`
	Total       int             `json:"total"`
}

// DeleteResponse A5 软删响应（data: {id, status}）。
type DeleteResponse struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

// Handler 通用附件 handler。
type Handler struct {
	service *service.AttachmentService
	logger  *zap.SugaredLogger
}

// NewHandler 创建通用附件 handler。
func NewHandler(attachmentService *service.AttachmentService, logger *zap.SugaredLogger) *Handler {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &Handler{service: attachmentService, logger: logger}
}

// ---------------------------------------------------------------------------
// A1-A6 端点
// ---------------------------------------------------------------------------

// Upload A1 POST /attachments：multipart 上传并绑定宿主（通用路由，宿主来自表单）。
func (h *Handler) Upload(c *gin.Context) {
	bizType := strings.TrimSpace(c.PostForm("bizType"))
	bizID, err := strconv.Atoi(strings.TrimSpace(c.PostForm("bizId")))
	if bizType == "" || err != nil || bizID <= 0 {
		common.Fail(c, common.ParamErrorCode, "bizType 与 bizId 必填且必须合法")
		return
	}
	h.uploadToHost(c, bizType, bizID)
}

// uploadToHost A1 主体；宿主已解析（通用路由来自表单，域内别名来自路径宿主）。
func (h *Handler) uploadToHost(c *gin.Context, bizType string, bizID int) {
	tenantID, userID, ok := tenantUserID(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "请选择要上传的文件")
		return
	}
	clientToken := strings.TrimSpace(c.PostForm("clientToken"))
	if len(clientToken) > 64 {
		common.Fail(c, common.ParamErrorCode, "clientToken 长度不能超过 64")
		return
	}
	if !h.authorizeHost(c, bizType, "write") {
		return
	}

	src, err := file.Open()
	if err != nil {
		common.FailWithErr(c, err, "文件打开失败")
		return
	}
	defer src.Close()

	view, err := h.service.Upload(c.Request.Context(), tenantID, userID, service.AttachmentUploadInput{
		BizType:     bizType,
		BizID:       bizID,
		Usage:       strings.TrimSpace(c.PostForm("usage")),
		ClientToken: clientToken,
		File: &service.FileHeader{
			Filename:    file.Filename,
			Size:        file.Size,
			ContentType: file.Header.Get("Content-Type"),
			Reader:      src,
		},
	})
	if err != nil {
		h.respondError(c, err, "附件上传失败")
		return
	}
	common.Success(c, toAttachmentRef(view))
}

// List A2 GET /attachments：列表/元数据（通用路由，宿主 + usage 过滤，分页）。
func (h *Handler) List(c *gin.Context) {
	bizType := strings.TrimSpace(c.Query("bizType"))
	bizID, err := strconv.Atoi(strings.TrimSpace(c.Query("bizId")))
	if bizType == "" || err != nil || bizID <= 0 {
		common.Fail(c, common.ParamErrorCode, "bizType 与 bizId 必填且必须合法")
		return
	}
	h.listHost(c, bizType, bizID)
}

// listHost A2 主体；宿主已解析。
func (h *Handler) listHost(c *gin.Context, bizType string, bizID int) {
	tenantID, _, ok := tenantUserID(c)
	if !ok {
		return
	}
	page := positiveQueryInt(c.Query("page"), 1)
	pageSize := positiveQueryInt(c.Query("pageSize"), attachmentDefaultPageSize)
	if pageSize > attachmentMaxPageSize {
		pageSize = attachmentMaxPageSize
	}
	if !h.authorizeHost(c, bizType, "read") {
		return
	}

	views, total, err := h.service.List(c.Request.Context(), tenantID, bizType, bizID,
		strings.TrimSpace(c.Query("usage")), (page-1)*pageSize, pageSize)
	if err != nil {
		h.respondError(c, err, "获取附件列表失败")
		return
	}
	refs := make([]AttachmentRef, 0, len(views))
	for _, v := range views {
		refs = append(refs, toAttachmentRef(v))
	}
	common.Success(c, ListResponse{Attachments: refs, Total: total})
}

// Get A3 GET /attachments/:id：单条元数据。
func (h *Handler) Get(c *gin.Context) {
	tenantID, _, ok := tenantUserID(c)
	if !ok {
		return
	}
	id, ok := parseAttachmentID(c)
	if !ok {
		return
	}
	view, err := h.service.Get(c.Request.Context(), tenantID, id)
	if err != nil {
		h.respondError(c, err, "附件不存在或无法访问")
		return
	}
	if !h.authorizeHost(c, view.BizType, "read") {
		return
	}
	common.Success(c, toAttachmentRef(view))
}

// Download A4 GET /attachments/:id/content：下载/预览（通用路由）；支持 Range（本地
// 存储走 http.ServeContent，其他 StorageProvider 实现退化为整文件流）。
func (h *Handler) Download(c *gin.Context) {
	h.downloadAttachment(c, nil, false)
}

// downloadAttachment A4 主体。scope 非空时为域内别名：附件必须归属该宿主，否则按不存在
// 处理（404，避免跨宿主枚举）；forcePreview 对应别名的 /preview 路径，仍受安全 MIME 约束。
func (h *Handler) downloadAttachment(c *gin.Context, scope *hostScope, forcePreview bool) {
	tenantID, _, ok := tenantUserID(c)
	if !ok {
		return
	}
	id, ok := parseAttachmentIDParam(c, scope)
	if !ok {
		return
	}
	// 先取元数据以完成宿主归属判定（跨租户/不存在统一 404），再打开文件流。
	view, err := h.service.Get(c.Request.Context(), tenantID, id)
	if err != nil {
		h.respondError(c, err, "附件不存在或无法访问")
		return
	}
	if !scope.matches(view) {
		h.logger.Warnw("Attachment alias host mismatch",
			"attachment_id", id, "attachment_biz_type", view.BizType, "attachment_biz_id", view.BizID,
			"expected_biz_type", scope.bizType, "expected_biz_id", scope.bizID, "path", c.Request.URL.Path)
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return
	}
	if !h.authorizeHost(c, view.BizType, "read") {
		return
	}
	stream, err := h.service.GetFile(c.Request.Context(), tenantID, id)
	if err != nil {
		h.respondError(c, err, "附件文件不可用")
		return
	}
	defer stream.Reader.Close()

	mimeType := strings.TrimSpace(stream.MimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	disposition := attachmentDispositionAttachment
	inlineRequested := forcePreview || strings.EqualFold(strings.TrimSpace(c.Query("disposition")), attachmentDispositionInline)
	if inlineRequested && inlineSafeAttachmentMIME(mimeType) {
		disposition = attachmentDispositionInline
	}
	c.Header("Content-Type", mimeType)
	c.Header("Content-Disposition", contentDisposition(disposition, stream.FileName))
	c.Header("X-Content-Type-Options", "nosniff")

	if rs, ok := stream.Reader.(io.ReadSeeker); ok {
		// ServeContent 负责 Content-Length / Accept-Ranges / Range 与条件请求。
		http.ServeContent(c.Writer, c.Request, stream.FileName, time.Time{}, rs)
		return
	}
	c.Header("Accept-Ranges", "none")
	c.Header("Content-Length", strconv.FormatInt(stream.Size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, stream.Reader); err != nil {
		h.logger.Warnw("Attachment stream aborted", "error", err, "attachment_id", id)
	}
}

// Delete A5 DELETE /attachments/:id：软删（通用路由；被引用时 409/6105，重复删除幂等）。
func (h *Handler) Delete(c *gin.Context) {
	h.deleteAttachment(c, nil)
}

// deleteAttachment A5 主体。scope 非空时为域内别名：附件必须归属该宿主，否则按不存在处理。
func (h *Handler) deleteAttachment(c *gin.Context, scope *hostScope) {
	tenantID, userID, ok := tenantUserID(c)
	if !ok {
		return
	}
	id, ok := parseAttachmentIDParam(c, scope)
	if !ok {
		return
	}
	view, err := h.service.Get(c.Request.Context(), tenantID, id)
	if err != nil {
		h.respondError(c, err, "附件不存在或无法访问")
		return
	}
	if !scope.matches(view) {
		h.logger.Warnw("Attachment alias host mismatch",
			"attachment_id", id, "attachment_biz_type", view.BizType, "attachment_biz_id", view.BizID,
			"expected_biz_type", scope.bizType, "expected_biz_id", scope.bizID, "path", c.Request.URL.Path)
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return
	}
	if !h.authorizeHost(c, view.BizType, "delete") {
		return
	}
	if err := h.service.Delete(c.Request.Context(), tenantID, userID, id); err != nil {
		h.respondError(c, err, "删除附件失败")
		return
	}
	common.Success(c, DeleteResponse{ID: id, Status: service.AttachmentStatusDeleted})
}

// BatchQuery A6 POST /attachments/batch-query：批量元数据回填（≤200 条）。
func (h *Handler) BatchQuery(c *gin.Context) {
	tenantID, _, ok := tenantUserID(c)
	if !ok {
		return
	}
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.BindValidationError(c, err, "ids 必须为数组")
		return
	}
	if len(req.IDs) > service.AttachmentMaxBatchIDs {
		common.Fail(c, common.ParamErrorCode, fmt.Sprintf("批量查询最多 %d 条", service.AttachmentMaxBatchIDs))
		return
	}
	views, err := h.service.BatchGet(c.Request.Context(), tenantID, req.IDs)
	if err != nil {
		h.respondError(c, err, "批量查询附件失败")
		return
	}
	// 宿主鉴权按 (biz_type, biz_id) 去重后逐宿主校验一次，禁止逐条查宿主（§3.2 A6）。
	if !h.authorizeHosts(c, views) {
		return
	}
	refs := make([]AttachmentRef, 0, len(views))
	for _, v := range views {
		refs = append(refs, toAttachmentRef(v))
	}
	common.Success(c, refs)
}

// ---------------------------------------------------------------------------
// 域内别名入口（BE-5，§3.2）
//
// 域别名把宿主编码进路径（bizType 常量 + 宿主 :id），静态权限在路由层声明为宿主权限码
// （router/knowledge_routes.go、router/service_request_routes.go）；处理器复用 A1/A2/A4/A5
// 主体，并复核附件确实归属该宿主，杜绝仅凭路径拼接读写他人宿主的附件。
// ---------------------------------------------------------------------------

// hostScope 域内别名的宿主范围；nil 表示通用路由（宿主由请求参数解析）。
type hostScope struct {
	bizType string
	bizID   int
}

// matches 别名归属复核：scope 为 nil 时不管束（通用路由由 §4.2 动态鉴权兜底）。
func (s *hostScope) matches(v *service.AttachmentView) bool {
	if s == nil {
		return true
	}
	return v != nil && v.BizType == s.bizType && v.BizID == s.bizID
}

// AliasUpload A1 域内别名：POST /{domain}/:id/attachments。
func (h *Handler) AliasUpload(bizType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := aliasScopeFromPath(c, bizType)
		if !ok {
			return
		}
		h.uploadToHost(c, scope.bizType, scope.bizID)
	}
}

// AliasList A2 域内别名：GET /{domain}/:id/attachments。
func (h *Handler) AliasList(bizType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := aliasScopeFromPath(c, bizType)
		if !ok {
			return
		}
		h.listHost(c, scope.bizType, scope.bizID)
	}
}

// AliasDownload A4 域内别名：GET /{domain}/:id/attachments/:ref[/download|/preview]。
func (h *Handler) AliasDownload(bizType string, preview bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := aliasScopeFromPath(c, bizType)
		if !ok {
			return
		}
		h.downloadAttachment(c, scope, preview)
	}
}

// AliasDelete A5 域内别名：DELETE /{domain}/:id/attachments/:ref。
func (h *Handler) AliasDelete(bizType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := aliasScopeFromPath(c, bizType)
		if !ok {
			return
		}
		h.deleteAttachment(c, scope)
	}
}

// aliasScopeFromPath 解析域别名的宿主 :id；非法或不存在统一 404，不暴露宿主存在性。
func aliasScopeFromPath(c *gin.Context, bizType string) (*hostScope, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		common.Fail(c, common.NotFoundCode, "宿主不存在或无权访问")
		return nil, false
	}
	return &hostScope{bizType: bizType, bizID: id}, true
}

// ---------------------------------------------------------------------------
// 权限（§4.2 宿主映射）
// ---------------------------------------------------------------------------

// hostPolicy §4.2 权威表的单行：biz_type → 宿主资源 + 三个动作。
type hostPolicy struct {
	resource string
	read     string
	write    string
	remove   string
}

func (p hostPolicy) actionFor(action string) string {
	switch action {
	case "read":
		return p.read
	case "write":
		return p.write
	case "delete":
		return p.remove
	default:
		return ""
	}
}

// attachmentHostPolicies §4.2 权威表（含预留宿主）。ticket 的上传动作刻意维持
// 现网语义 ticket:create（R3），知识库/服务请求用 write，删除一律 delete。
var attachmentHostPolicies = map[string]hostPolicy{
	service.AttachmentBizTypeTicket:           {resource: "ticket", read: "read", write: "create", remove: "delete"},
	service.AttachmentBizTypeKnowledgeArticle: {resource: "knowledge", read: "read", write: "write", remove: "delete"},
	"service_request":                         {resource: "service_request", read: "read", write: "write", remove: "delete"},
	"incident":                                {resource: "incident", read: "read", write: "write", remove: "delete"},
	"problem":                                 {resource: "problem", read: "read", write: "write", remove: "delete"},
	"change":                                  {resource: "change", read: "read", write: "write", remove: "delete"},
	"release":                                 {resource: "release", read: "read", write: "write", remove: "delete"},
	"cmdb_ci":                                 {resource: "cmdb", read: "read", write: "write", remove: "delete"},
}

// authorizeHost 动态复核 §4.2 宿主权限；未映射的 biz_type 视为系统级/无宿主，
// 由路由级兜底码承担最终闸门（返回 true）。
func (h *Handler) authorizeHost(c *gin.Context, bizType, action string) bool {
	policy, ok := attachmentHostPolicies[strings.TrimSpace(bizType)]
	if !ok {
		return true
	}
	perm := policy.actionFor(action)
	if perm == "" {
		return true
	}
	client, ok := clientFromContext(c)
	if !ok {
		h.logger.Errorw("Attachment host authorization context missing",
			"biz_type", bizType, "path", c.Request.URL.Path)
		common.Fail(c, common.InternalErrorCode, "附件鉴权上下文缺失")
		return false
	}
	tenantID := c.GetInt("tenant_id")
	if !middleware.AuthorizeResource(c.Request.Context(), client, middleware.GetContextRoles(c), policy.resource, perm, tenantID) {
		h.logger.Warnw("Attachment host permission denied",
			"biz_type", bizType, "resource", policy.resource, "action", perm,
			"path", c.Request.URL.Path, "tenant_id", tenantID)
		common.Fail(c, common.ForbiddenCode, "无权访问该宿主的附件")
		return false
	}
	return true
}

// authorizeHosts A6 批量：按 (biz_type, biz_id) 去重后逐宿主演一次，任一失败整体 403。
func (h *Handler) authorizeHosts(c *gin.Context, views []*service.AttachmentView) bool {
	checked := make(map[string]struct{}, len(views))
	for _, v := range views {
		if v == nil {
			continue
		}
		key := v.BizType + "/" + strconv.Itoa(v.BizID)
		if _, ok := checked[key]; ok {
			continue
		}
		checked[key] = struct{}{}
		if !h.authorizeHost(c, v.BizType, "read") {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// 错误码映射（§3.4：6101-6107）与响应辅助
// ---------------------------------------------------------------------------

// respondError 将服务层可判定错误映射为 §3.4 的 61xx；不可判定错误兜底 5001（记日志）。
func (h *Handler) respondError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, service.ErrAttachmentHostNotFound):
		common.Fail(c, common.AttachmentHostNotFoundCode, "宿主不存在或无权访问")
	case errors.Is(err, service.ErrAttachmentInvalidFilename):
		common.Fail(c, common.AttachmentInvalidFilenameCode, "文件名非法")
	case errors.Is(err, service.ErrAttachmentTooLarge):
		common.Fail(c, common.AttachmentTooLargeCode, fmt.Sprintf("文件超过 %dMB 上限", h.service.MaxFileSizeMB()))
	case errors.Is(err, service.ErrAttachmentTypeRejected), errors.Is(err, service.ErrAttachmentRejected):
		common.Fail(c, common.AttachmentTypeNotAllowedCode, "文件类型不允许")
	case errors.Is(err, service.ErrAttachmentInUse):
		common.Fail(c, common.AttachmentInUseCode, "附件已被引用，无法删除")
	case errors.Is(err, service.ErrAttachmentQuotaExceeded):
		common.Fail(c, common.AttachmentQuotaExceededCode, "附件配额已用尽")
	case errors.Is(err, service.ErrAttachmentNotFound):
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
	case errors.Is(err, service.ErrAttachmentEmpty):
		common.Fail(c, common.ParamErrorCode, "文件内容为空")
	case errors.Is(err, service.ErrAttachmentSizeMismatch):
		common.Fail(c, common.ParamErrorCode, "文件大小与声明不一致")
	default:
		common.FailWithErr(c, err, fallback)
	}
}

// tenantUserID 提取租户与操作者；缺失统一 401（不进入业务层）。
func tenantUserID(c *gin.Context) (tenantID, userID int, ok bool) {
	tenantID = c.GetInt("tenant_id")
	userID = c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "认证信息缺失")
		return 0, 0, false
	}
	return tenantID, userID, true
}

// clientFromContext 取认证/RBAC 中间件注入的 ent 客户端。
func clientFromContext(c *gin.Context) (*ent.Client, bool) {
	v, ok := c.Get("client")
	if !ok {
		return nil, false
	}
	client, ok := v.(*ent.Client)
	return client, ok && client != nil
}

func parseAttachmentID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		common.Fail(c, common.ParamErrorCode, "无效的附件ID")
		return 0, false
	}
	return id, true
}

// parseAttachmentIDParam 解析附件 ID：通用路由读 :id（非法 → 400）；域内别名读 :ref，
// 非法一律 404（与「附件不存在」同响应，避免探测宿主内附件编号）。
func parseAttachmentIDParam(c *gin.Context, scope *hostScope) (int, bool) {
	if scope == nil {
		return parseAttachmentID(c)
	}
	ref, err := strconv.Atoi(strings.TrimSpace(c.Param("ref")))
	if err != nil || ref <= 0 {
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return 0, false
	}
	return ref, true
}

func positiveQueryInt(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// toAttachmentRef 服务层视图 → 契约 DTO；previewUrl 仅图片给（A4 inline）。
func toAttachmentRef(v *service.AttachmentView) AttachmentRef {
	if v == nil {
		return AttachmentRef{}
	}
	ref := AttachmentRef{
		ID:         v.ID,
		BizType:    v.BizType,
		BizID:      v.BizID,
		Usage:      v.Usage,
		FileName:   v.FileName,
		FileSize:   v.FileSize,
		MimeType:   v.MimeType,
		FileURL:    v.FileURL,
		SHA256:     v.SHA256,
		UploadedBy: v.UploadedBy,
	}
	if ref.FileURL == "" {
		ref.FileURL = fmt.Sprintf("/api/v1/attachments/%d/content", v.ID)
	}
	if strings.HasPrefix(strings.ToLower(ref.MimeType), "image/") {
		ref.PreviewURL = fmt.Sprintf("/api/v1/attachments/%d/content?disposition=inline", v.ID)
	}
	if !v.CreatedAt.IsZero() {
		ref.CreatedAt = v.CreatedAt.UTC().Format(time.RFC3339)
	}
	return ref
}

// contentDisposition 生成 RFC 6266 头；mime.FormatMediaType 对非 ASCII 文件名
// 自动改用 filename*=UTF-8”…，避免头部注入（§3.5-7）。
func contentDisposition(disposition, filename string) string {
	if strings.TrimSpace(filename) == "" {
		return disposition
	}
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": filename}); v != "" {
		return v
	}
	return disposition
}

// inlineSafeAttachmentMIME 仅确定不会执行脚本的位图允许内联渲染（SVG 含 image/svg+xml
// 一律强制下载），与工单附件的存储型 XSS 防线同口径（handlers/ticket_attachment）。
func inlineSafeAttachmentMIME(mimeType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(mimeType))
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return true
	default:
		return false
	}
}
