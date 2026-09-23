// Package ticket_attachment 是工单附件域的 HTTP handler 层（域切片架构）。
// 自 controller/ticket_attachment_controller.go 迁移而来（2026-09-02），
// 业务逻辑仍由 service.TicketAttachmentService 承载，本包只做参数解析与响应封装。
package ticket_attachment

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Handler 工单附件 HTTP handler
type Handler struct {
	attachmentService *service.TicketAttachmentService
	logger            *zap.SugaredLogger
}

// NewHandler 创建工单附件 handler 实例
func NewHandler(attachmentService *service.TicketAttachmentService, logger *zap.SugaredLogger) *Handler {
	return &Handler{
		attachmentService: attachmentService,
		logger:            logger,
	}
}

// tenantUserID 提取租户和用户 ID
func tenantUserID(c *gin.Context) (tenantID, userID int, ok bool) {
	tenantID = c.GetInt("tenant_id")
	userID = c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "认证信息缺失")
		return 0, 0, false
	}
	return tenantID, userID, true
}

// pathIDs 提取路径参数中的 ticketID 和 attachmentID
func pathIDs(c *gin.Context) (ticketID, attachmentID int, ok bool) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "无效的工单ID")
		return 0, 0, false
	}
	attachmentID, err = strconv.Atoi(c.Param("attachment_id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "无效的附件ID")
		return 0, 0, false
	}
	return ticketID, attachmentID, true
}

// attachmentRef 提取路径参数中的 ticketID 与「附件引用」。
//
// 附件引用既可能是数字 ID（前端 API 使用），也可能是上传时生成的存储文件名：
// 历史版本的 fileUrl 形如 /api/v1/tickets/{id}/attachments/{ticketID}_{nano}_{name}/download，
// 已被写入富文本 descriptionHtml 落库，因此下载/预览必须同时兼容两种形式。
func attachmentRef(c *gin.Context) (ticketID int, ref string, ok bool) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "无效的工单ID")
		return 0, "", false
	}
	ref = strings.TrimSpace(c.Param("attachment_id"))
	if ref == "" {
		common.Fail(c, common.ParamErrorCode, "无效的附件ID")
		return 0, "", false
	}
	return ticketID, ref, true
}

// ListTicketAttachments 获取工单附件列表
func (h *Handler) ListTicketAttachments(c *gin.Context) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "无效的工单ID")
		return
	}

	tenantID, userID, ok := tenantUserID(c)
	if !ok {
		return
	}

	attachments, err := h.attachmentService.ListAttachments(c.Request.Context(), ticketID, tenantID, userID)
	if err != nil {
		h.logger.Errorw("Failed to list ticket attachments", "error", err, "ticket_id", ticketID, "tenant_id", tenantID)
		common.Fail(c, common.InternalErrorCode, "获取附件列表失败")
		return
	}

	common.Success(c, dto.ListTicketAttachmentsResponse{
		Attachments: attachments,
		Total:       len(attachments),
	})
}

// UploadAttachment 上传附件
func (h *Handler) UploadAttachment(c *gin.Context) {
	ticketID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "无效的工单ID")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "请选择要上传的文件")
		return
	}

	src, err := file.Open()
	if err != nil {
		h.logger.Errorw("Failed to open uploaded file", "error", err)
		common.Fail(c, common.InternalErrorCode, "文件打开失败")
		return
	}
	defer src.Close()

	fileHeader := &service.FileHeader{
		Filename:    file.Filename,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
		Reader:      src,
	}

	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "认证信息缺失")
		return
	}

	attachment, err := h.attachmentService.UploadAttachment(c.Request.Context(), ticketID, fileHeader, userID, tenantID)
	if err != nil {
		h.logger.Errorw("Failed to upload attachment", "error", err, "ticket_id", ticketID, "tenant_id", tenantID)
		// 按可判定错误分派可读原因，替代此前“一句话概括”的模糊提示
		switch {
		case errors.Is(err, service.ErrAttachmentEmpty):
			common.Fail(c, common.ParamErrorCode, "文件内容为空（0 字节），无法上传")
		case errors.Is(err, service.ErrAttachmentTooLarge):
			common.Fail(c, common.ParamErrorCode, fmt.Sprintf("附件超过单文件大小上限（%dMB）", h.attachmentService.MaxFileSizeMB()))
		case errors.Is(err, service.ErrAttachmentTypeRejected):
			common.Fail(c, common.ParamErrorCode, "附件类型不在允许范围内（文档/表格/图片/压缩包）")
		default:
			common.Fail(c, common.ParamErrorCode, "附件上传失败，请稍后重试")
		}
		return
	}

	common.Success(c, attachment)
}

// DownloadAttachment 下载附件
func (h *Handler) DownloadAttachment(c *gin.Context) {
	ticketID, ref, ok := attachmentRef(c)
	if !ok {
		return
	}

	tenantID, userID, ok := tenantUserID(c)
	if !ok {
		return
	}

	attachmentFile, err := h.attachmentService.GetAttachmentFile(c.Request.Context(), ticketID, ref, tenantID, userID)
	if err != nil {
		h.logger.Errorw("Failed to get attachment file", "error", err, "ticket_id", ticketID, "attachment_ref", ref, "tenant_id", tenantID)
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return
	}
	defer attachmentFile.File.Close()

	mimeType := "application/octet-stream"
	if attachmentFile.MimeType != nil {
		mimeType = *attachmentFile.MimeType
	}
	c.Header("Content-Type", mimeType)
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachmentFile.FileName}))
	c.Header("Content-Length", strconv.FormatInt(attachmentFile.Size, 10))

	_, err = io.Copy(c.Writer, attachmentFile.File)
	if err != nil {
		h.logger.Errorw("Failed to copy file to response", "error", err)
	}
}

// PreviewAttachment 预览附件
func (h *Handler) PreviewAttachment(c *gin.Context) {
	ticketID, ref, ok := attachmentRef(c)
	if !ok {
		return
	}

	tenantID, userID, ok := tenantUserID(c)
	if !ok {
		return
	}

	attachmentFile, err := h.attachmentService.GetAttachmentFile(c.Request.Context(), ticketID, ref, tenantID, userID)
	if err != nil {
		h.logger.Errorw("Failed to get attachment file", "error", err, "ticket_id", ticketID, "attachment_ref", ref, "tenant_id", tenantID)
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return
	}
	defer attachmentFile.File.Close()

	mimeType := "application/octet-stream"
	if attachmentFile.MimeType != nil {
		mimeType = *attachmentFile.MimeType
	}
	c.Header("Content-Type", mimeType)
	// SVG/HTML 等可携带脚本的类型不允许内联渲染，否则会形成存储型 XSS；统一降级为下载
	disposition := "inline"
	if !inlineSafeMIME(mimeType) {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachmentFile.FileName}))
	c.Header("Content-Length", strconv.FormatInt(attachmentFile.Size, 10))

	_, err = io.Copy(c.Writer, attachmentFile.File)
	if err != nil {
		h.logger.Errorw("Failed to copy file to response", "error", err)
	}
}

// DeleteAttachment 删除附件
func (h *Handler) DeleteAttachment(c *gin.Context) {
	ticketID, attachmentID, ok := pathIDs(c)
	if !ok {
		return
	}

	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID == 0 || userID == 0 {
		common.Fail(c, common.AuthFailedCode, "认证信息缺失")
		return
	}

	err := h.attachmentService.DeleteAttachment(c.Request.Context(), ticketID, attachmentID, tenantID, userID)
	if err != nil {
		h.logger.Errorw("Failed to delete attachment", "error", err, "ticket_id", ticketID, "attachment_id", attachmentID, "tenant_id", tenantID)
		common.Fail(c, common.InternalErrorCode, "删除附件失败")
		return
	}

	common.Success(c, nil)
}

// inlineSafeMIME 仅对确定不会执行脚本的位图允许内联预览，其余（含 image/svg+xml）强制下载，
// 避免以附件为载体的存储型 XSS。
func inlineSafeMIME(mimeType string) bool {
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
