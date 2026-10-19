package ai

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
)

// T5-03a：AI 对话附件上传/下载专用端点。
//
// 复用通用附件服务的存储、大小/类型校验、配额、病毒扫描与审计；与通用 A1/A4 的差异：
// - 路由权限为 ai:read，普通助手用户可用（通用 A1/A4 是 attachment:* 兜底码）；
// - 宿主固定 ai_chat / bizId=userID，属于当前用户的草稿附件；
// - 读取时再次校验租户、宿主、上传人，避免跨用户读取。

type aiAttachmentRef struct {
	ID       int    `json:"id"`
	FileName string `json:"fileName"`
	FileSize int    `json:"fileSize"`
	MimeType string `json:"mimeType"`
	FileURL  string `json:"fileUrl"`
}

func (h *Handler) aiAttachmentsAvailable(c *gin.Context) bool {
	if h == nil || h.svc == nil || h.svc.attachments == nil {
		common.Fail(c, common.ServiceUnavailableCode, "AI 附件服务未就绪")
		return false
	}
	return true
}

// UploadAIAttachment POST /api/v1/ai/attachments
func (h *Handler) UploadAIAttachment(c *gin.Context) {
	if !h.aiAttachmentsAvailable(c) {
		return
	}
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "租户或用户信息缺失")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		common.ParamError(c, "缺少上传文件")
		return
	}
	src, err := file.Open()
	if err != nil {
		common.FailWithErr(c, err, "读取上传文件失败")
		return
	}
	defer func() { _ = src.Close() }()

	view, err := h.svc.attachments.Upload(c.Request.Context(), tenantID, userID, service.AttachmentUploadInput{
		BizType: service.AttachmentBizTypeAIChat,
		BizID:   userID,
		Usage:   service.AttachmentUsageAttachment,
		File: &service.FileHeader{
			Filename:    file.Filename,
			Size:        file.Size,
			ContentType: file.Header.Get("Content-Type"),
			Reader:      src,
		},
	})
	if err != nil {
		common.FailWithErr(c, err, "附件上传失败")
		return
	}

	common.Success(c, aiAttachmentRef{
		ID:       view.ID,
		FileName: view.FileName,
		FileSize: view.FileSize,
		MimeType: view.MimeType,
		FileURL:  fmt.Sprintf("/api/v1/ai/attachments/%d/content", view.ID),
	})
}

// DownloadAIAttachment GET /api/v1/ai/attachments/:id/content
func (h *Handler) DownloadAIAttachment(c *gin.Context) {
	if !h.aiAttachmentsAvailable(c) {
		return
	}
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "租户或用户信息缺失")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "附件 ID 非法")
		return
	}
	stream, err := h.svc.attachments.GetFile(c.Request.Context(), tenantID, id)
	if err != nil {
		common.FailWithErr(c, err, "附件不存在或无法访问")
		return
	}
	defer func() { _ = stream.Reader.Close() }()
	if stream.View == nil ||
		stream.View.BizType != service.AttachmentBizTypeAIChat ||
		stream.View.BizID != userID ||
		stream.View.UploadedBy != userID {
		common.Fail(c, common.NotFoundCode, "附件不存在或无法访问")
		return
	}

	disposition := "attachment"
	if strings.EqualFold(strings.TrimSpace(c.Query("disposition")), "inline") &&
		strings.HasPrefix(strings.ToLower(stream.MimeType), "image/") {
		disposition = "inline"
	}
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disposition, stream.FileName))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, stream.Size, stream.MimeType, stream.Reader, nil)
}
