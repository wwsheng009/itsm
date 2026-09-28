package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/ticketattachment"
	"itsm-backend/ent/user"

	"go.uber.org/zap"
)

type TicketAttachmentService struct {
	client       *ent.Client
	logger       *zap.SugaredLogger
	uploadDir    string
	maxFileSize  int64    // 最大文件大小（字节），默认10MB
	allowedTypes []string // 允许的文件类型
	virusScanner AttachmentVirusScanner

	// BE-6 薄适配：非空且对应开关打开时，旧工单端点内部转发通用附件服务
	// （service.AttachmentService，biz_type=ticket）；开关默认关闭，旧链路行为不变。
	generic      *AttachmentService
	genericFlags TicketAttachmentGenericFlags
}

// TicketAttachmentGenericFlags 旧工单端点的灰度开关（§6.1 开关与回滚矩阵）。
//
// P4 起按租户解析（system_config category=attachment）；当前由 bootstrap 注入
// 部署级静态实现（StaticTicketAttachmentFlags），租户级解析在 P4 叠加。
type TicketAttachmentGenericFlags interface {
	GenericReadEnabled(tenantID int) bool  // 列表/下载改读 attachments 表
	GenericWriteEnabled(tenantID int) bool // 上传/删除改走通用服务（软删 + 引用保护）
}

// StaticTicketAttachmentFlags 部署级静态开关实现。
type StaticTicketAttachmentFlags struct {
	Read  bool
	Write bool
}

func (f StaticTicketAttachmentFlags) GenericReadEnabled(int) bool  { return f.Read }
func (f StaticTicketAttachmentFlags) GenericWriteEnabled(int) bool { return f.Write }

type AttachmentVirusScanner interface {
	Scan(context.Context, string) error
}
type noopAttachmentVirusScanner struct{}

func (noopAttachmentVirusScanner) Scan(context.Context, string) error { return nil }

func NewTicketAttachmentService(client *ent.Client, logger *zap.SugaredLogger) *TicketAttachmentService {
	uploadDir := "uploads/tickets"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		logger.Warnw("Failed to create upload directory", "error", err, "dir", uploadDir)
	}

	return &TicketAttachmentService{
		client:       client,
		logger:       logger,
		uploadDir:    uploadDir,
		maxFileSize:  10 * 1024 * 1024, // 10MB
		allowedTypes: allowedAttachmentMIMEs(),
		virusScanner: noopAttachmentVirusScanner{},
	}
}

func (s *TicketAttachmentService) SetVirusScanner(scanner AttachmentVirusScanner) {
	if scanner != nil {
		s.virusScanner = scanner
	}
}

// SetGenericBackend 注入通用附件后端与灰度开关（bootstrap 调用）。
// generic 为 nil 或 flags 为 nil 时旧链路完全不变（可无损回滚）。
func (s *TicketAttachmentService) SetGenericBackend(generic *AttachmentService, flags TicketAttachmentGenericFlags) {
	s.generic = generic
	s.genericFlags = flags
}

func (s *TicketAttachmentService) genericReadEnabled(tenantID int) bool {
	return s.generic != nil && s.genericFlags != nil && s.genericFlags.GenericReadEnabled(tenantID)
}

func (s *TicketAttachmentService) genericWriteEnabled(tenantID int) bool {
	return s.generic != nil && s.genericFlags != nil && s.genericFlags.GenericWriteEnabled(tenantID)
}

// legacyTicketAttachmentURL 历史富文本内嵌地址形态：/tickets/:id/attachments/:ref/preview
// （D5：该 URL 永不失效，因此通用记录映射回旧端点时同样输出此形态）。
func legacyTicketAttachmentURL(ticketID, attachmentID int) string {
	return fmt.Sprintf("/api/v1/tickets/%d/attachments/%d/preview", ticketID, attachmentID)
}

// legacyTicketAttachmentResponse 通用视图 → 旧工单端点 DTO（BE-6 字段级兼容）。
//
// 字段口径与 dto.ToTicketAttachmentResponse 完全一致：filePath 复用存储相对路径
// （旧实现为 `uploads/tickets/<name>`，同为内部定位信息、不参与下载）；
// fileUrl 固定输出历史 preview 形态，保证前端与历史富文本行为不变。
func legacyTicketAttachmentResponse(view *AttachmentView, uploader *ent.User) *dto.TicketAttachmentResponse {
	if view == nil {
		return nil
	}
	resp := &dto.TicketAttachmentResponse{
		ID:         view.ID,
		TicketID:   view.BizID,
		FileName:   view.FileName,
		FilePath:   view.StorageKey,
		FileURL:    legacyTicketAttachmentURL(view.BizID, view.ID),
		FileSize:   view.FileSize,
		FileType:   view.FileType,
		MimeType:   view.MimeType,
		UploadedBy: view.UploadedBy,
		CreatedAt:  view.CreatedAt,
	}
	if uploader != nil {
		resp.Uploader = &dto.UserInfo{
			ID:         uploader.ID,
			Username:   uploader.Username,
			Name:       uploader.Name,
			Email:      uploader.Email,
			Role:       string(uploader.Role),
			Department: uploader.Department,
			TenantID:   uploader.TenantID,
		}
	}
	return resp
}

// legacyUploaderMap 批量补齐上传人信息（避免逐条查询）。查询失败仅告警：
// 旧实现同样把 uploader 视为可选字段（`json:"uploader,omitempty"`）。
func (s *TicketAttachmentService) legacyUploaderMap(ctx context.Context, views []*AttachmentView) map[int]*ent.User {
	ids := make([]int, 0, len(views))
	seen := make(map[int]struct{}, len(views))
	for _, view := range views {
		if view == nil || view.UploadedBy <= 0 {
			continue
		}
		if _, ok := seen[view.UploadedBy]; ok {
			continue
		}
		seen[view.UploadedBy] = struct{}{}
		ids = append(ids, view.UploadedBy)
	}
	if len(ids) == 0 {
		return nil
	}
	users, err := s.client.User.Query().Where(user.IDIn(ids...)).All(ctx)
	if err != nil {
		s.logger.Warnw("Failed to load attachment uploaders", "error", err, "user_ids", ids)
		return nil
	}
	uploaders := make(map[int]*ent.User, len(users))
	for _, u := range users {
		uploaders[u.ID] = u
	}
	return uploaders
}

// genericListAll 拉取工单宿主的全部 active 附件。
// 旧端点无分页语义（一次返回全部），因此循环取满；上限 2000 条防止异常数据拖垮响应，
// 截断时告警（旧实现无此上限，属灰度期的保护性差异）。
func (s *TicketAttachmentService) genericListAll(ctx context.Context, tenantID, ticketID int) ([]*AttachmentView, int, error) {
	const pageSize = 200
	const maxRecords = 2000

	views := make([]*AttachmentView, 0, pageSize)
	total := 0
	for offset := 0; offset < maxRecords; offset += pageSize {
		page, count, err := s.generic.List(ctx, tenantID, AttachmentBizTypeTicket, ticketID, "", offset, pageSize)
		if err != nil {
			return nil, 0, err
		}
		total = count
		views = append(views, page...)
		if len(page) < pageSize || len(views) >= count {
			break
		}
	}
	if len(views) < total {
		s.logger.Warnw("Ticket attachment list truncated by generic backend",
			"ticket_id", ticketID, "returned", len(views), "total", total)
	}
	return views, total, nil
}

// normalizeTicketAttachmentUsage 归一化域内端点收到的 `usage` 表单字段（BE-10）。
//
// 缺省（空串）按 attachment 处理，保证老客户端与历史 URL 字节级兼容；
// 仅接受 §3.1 登记的三档用途，其余一律拒绝，避免用途语义被静默改写。
func normalizeTicketAttachmentUsage(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", AttachmentUsageAttachment:
		return AttachmentUsageAttachment, nil
	case AttachmentUsageInlineImage:
		return AttachmentUsageInlineImage, nil
	case AttachmentUsageCommentAttachment:
		return AttachmentUsageCommentAttachment, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrAttachmentUsageInvalid, raw)
	}
}

// UploadAttachment 上传附件
//
// BE-10：新增 rawUsage 形参（域内端点 `usage` 表单字段，缺省 attachment），使工单域
// 能在**不改权限语义**的前提下表达内嵌图片 / 评论附件用途：通用路由 A1 的静态码是兜底码
// `attachment:write`（§4.3 仅绑定 admin/sysadmin），普通用户只能经域内端点上传，
// 而非默认用途又必须落通用表（旧 `ticket_attachments` 无 usage 列）。
func (s *TicketAttachmentService) UploadAttachment(
	ctx context.Context,
	ticketID int,
	fileHeader *FileHeader,
	rawUsage string,
	userID, tenantID int,
) (*dto.TicketAttachmentResponse, error) {
	usage, err := normalizeTicketAttachmentUsage(rawUsage)
	if err != nil {
		return nil, err
	}

	s.logger.Infow("Uploading attachment", "ticket_id", ticketID, "file_name", fileHeader.Filename, "usage", usage, "user_id", userID)

	// 验证文件大小：纯入参校验，先于数据库查询执行（同时保证该分支可单测）
	if fileHeader.Size > s.maxFileSize {
		return nil, fmt.Errorf("%w: size=%d bytes, max=%d bytes", ErrAttachmentTooLarge, fileHeader.Size, s.maxFileSize)
	}
	if fileHeader.Size <= 0 {
		// 0 字节文件若漏到这里，只会被 ent 的 file_size Positive() 拦下并抛出
		// "value out of range" 这类无法定位的报错，因此提前拦截并给出可读原因。
		return nil, fmt.Errorf("%w: size=%d bytes", ErrAttachmentEmpty, fileHeader.Size)
	}

	// BE-6 薄适配 + BE-10 用途透传：写开关打开（默认用途的灰度转发）或用途非默认
	// （旧表无 usage 列，必须落通用表）时转发通用服务（biz_type=ticket）。
	// 非默认用途不受 `attachment.generic_write_enabled` 约束：旧链路没有等价能力，
	// 该开关只用于「默认用途写入」的回退，不能把内嵌图片 / 评论附件一并关回旧表。
	// 校验错误哨兵与旧链路同源（ErrAttachmentEmpty/TooLarge/TypeRejected），
	// handler 的可读文案与状态码映射完全不变；宿主不存在沿用旧口径（→ 5001）。
	if usage != AttachmentUsageAttachment || s.genericWriteEnabled(tenantID) {
		if s.generic == nil {
			return nil, fmt.Errorf("%w: usage=%s", ErrAttachmentUsageBackendMissing, usage)
		}
		view, gerr := s.generic.Upload(ctx, tenantID, userID, AttachmentUploadInput{
			BizType: AttachmentBizTypeTicket,
			BizID:   ticketID,
			Usage:   usage,
			File:    fileHeader,
		})
		if gerr != nil {
			return nil, gerr
		}
		uploader, uerr := s.client.User.Get(ctx, userID)
		if uerr != nil {
			s.logger.Warnw("Failed to get uploader", "error", uerr, "user_id", userID)
			uploader = nil
		}
		return legacyTicketAttachmentResponse(view, uploader), nil
	}

	// 验证工单是否存在且属于当前租户
	ticketExists, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Exist(ctx)
	if err != nil {
		s.logger.Errorw("Failed to check ticket existence", "error", err)
		return nil, fmt.Errorf("failed to check ticket existence: %w", err)
	}
	if !ticketExists {
		return nil, fmt.Errorf("ticket not found")
	}

	// 1) 文件名清洗：拒绝路径遍历/控制字符，限制长度，防止 XSS/覆盖/目录穿越
	safeName := sanitizeFilename(fileHeader.Filename)
	if safeName == "" {
		return nil, fmt.Errorf("invalid filename: empty after sanitization")
	}

	// Client-provided Content-Type 可被伪造，仅作日志与兜底参考（无 Reader 的调用方），
	// 真实类型以内容嗅探为准。
	claimed := strings.TrimSpace(fileHeader.ContentType)
	if claimed == "" {
		// 尝试从文件扩展名推断
		claimed = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
	}

	// 2) Magic bytes / 实际内容嗅探：避免 Content-Type/扩展名 与 真实内容不一致
	//    从文件头最多读取 512 字节，调用 net/http.DetectContentType。
	//    注意：fileHeader.Reader 通常是一次性的，因此需要将嗅探过的字节 prepend 回去以便后续 saveFile 读取。
	detected := ""
	if fileHeader.Reader != nil {
		sniffBuf := make([]byte, 0, 512)
		tmp := make([]byte, 512)
		for len(sniffBuf) < 512 {
			n, rerr := fileHeader.Reader.Read(tmp)
			if n > 0 {
				sniffBuf = append(sniffBuf, tmp[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		if len(sniffBuf) == 0 {
			return nil, fmt.Errorf("%w: no readable bytes", ErrAttachmentEmpty)
		}
		detected = normalizeMIME(http.DetectContentType(sniffBuf))
		// 把嗅探过的字节塞回 Reader 的前面，保证 saveFile 读得到完整内容
		fileHeader.Reader = &prefixedReader{prefix: sniffBuf, r: fileHeader.Reader}
	}

	mimeType, err := s.resolveMIMEType(safeName, claimed, detected)
	if err != nil {
		return nil, err
	}

	// 生成唯一文件名（使用清洗后的文件名）
	fileName := fmt.Sprintf("%d_%d_%s", ticketID, time.Now().UnixNano(), safeName)
	filePath := filepath.Join(s.uploadDir, fileName)

	// 保存文件
	if err := s.saveFile(fileHeader, filePath); err != nil {
		s.logger.Errorw("Failed to save file", "error", err)
		return nil, fmt.Errorf("failed to save file: %w", err)
	}
	if err := s.virusScanner.Scan(ctx, filePath); err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("file rejected by malware scan")
	}

	// 创建附件记录（file_url 需等自增 ID 生成后再回写规范地址，见下方）
	attachment, err := s.client.TicketAttachment.Create().
		SetTicketID(ticketID).
		SetFileName(safeName).
		SetFilePath(filePath).
		SetFileSize(int(fileHeader.Size)).
		SetFileType(mimeType).
		SetMimeType(mimeType).
		SetUploadedBy(userID).
		SetTenantID(tenantID).
		Save(ctx)
	if err != nil {
		// 如果数据库保存失败，删除已上传的文件
		os.Remove(filePath)
		s.logger.Errorw("Failed to create attachment record", "error", err)
		return nil, fmt.Errorf("failed to create attachment record: %w", err)
	}

	// 规范访问地址：数字 ID + preview。
	// 该地址会被前端直接写入富文本 <img src>，只有 preview 端点按内联方式返回图片；
	// /download 携带 Content-Disposition: attachment，适合显式下载而非内嵌展示。
	fileURL := fmt.Sprintf("/api/v1/tickets/%d/attachments/%d/preview", ticketID, attachment.ID)
	if updated, uerr := attachment.Update().SetFileURL(fileURL).Save(ctx); uerr != nil {
		s.logger.Warnw("Failed to persist canonical attachment url", "error", uerr, "attachment_id", attachment.ID)
	} else {
		attachment = updated
	}

	// 查询上传人信息
	uploader, err := s.client.User.Get(ctx, userID)
	if err != nil {
		s.logger.Warnw("Failed to get uploader", "error", err, "user_id", userID)
		uploader = nil
	}

	return dto.ToTicketAttachmentResponse(attachment, uploader), nil
}

// ListAttachments 获取附件列表
func (s *TicketAttachmentService) ListAttachments(ctx context.Context, ticketID, tenantID, userID int) ([]*dto.TicketAttachmentResponse, error) {
	s.logger.Infow("Listing attachments", "ticket_id", ticketID)
	if err := s.authorizeTicketAttachmentAccess(ctx, ticketID, tenantID, userID); err != nil {
		return nil, err
	}

	// BE-6 薄适配：读开关打开时从通用表读取（返回结构与旧链路逐字段一致）。
	if s.genericReadEnabled(tenantID) {
		views, total, gerr := s.genericListAll(ctx, tenantID, ticketID)
		if gerr != nil {
			return nil, gerr
		}
		uploaders := s.legacyUploaderMap(ctx, views)
		responses := make([]*dto.TicketAttachmentResponse, 0, len(views))
		for _, view := range views {
			responses = append(responses, legacyTicketAttachmentResponse(view, uploaders[view.UploadedBy]))
		}
		s.logger.Infow("Listed ticket attachments via generic backend", "ticket_id", ticketID, "total", total)
		return responses, nil
	}

	// 验证工单是否存在且属于当前租户
	ticketExists, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Exist(ctx)
	if err != nil {
		s.logger.Errorw("Failed to check ticket existence", "error", err)
		return nil, fmt.Errorf("failed to check ticket existence: %w", err)
	}
	if !ticketExists {
		return nil, fmt.Errorf("ticket not found")
	}

	// 查询附件
	attachments, err := s.client.TicketAttachment.Query().
		Where(
			ticketattachment.TicketID(ticketID),
			ticketattachment.TenantID(tenantID),
		).
		Order(ent.Desc(ticketattachment.FieldCreatedAt)).
		WithUploader().
		All(ctx)
	if err != nil {
		s.logger.Errorw("Failed to list attachments", "error", err)
		return nil, fmt.Errorf("failed to list attachments: %w", err)
	}

	// 转换为 DTO
	responses := make([]*dto.TicketAttachmentResponse, 0, len(attachments))
	for _, attachment := range attachments {
		var uploader *ent.User
		if attachment.Edges.Uploader != nil {
			uploader = attachment.Edges.Uploader
		} else {
			uploader, _ = s.client.User.Get(ctx, attachment.UploadedBy)
		}
		responses = append(responses, dto.ToTicketAttachmentResponse(attachment, uploader))
	}

	return responses, nil
}

// GetAttachment 获取附件信息
func (s *TicketAttachmentService) GetAttachment(ctx context.Context, ticketID, attachmentID, tenantID int) (*dto.TicketAttachmentResponse, error) {
	attachment, err := s.client.TicketAttachment.Query().
		Where(
			ticketattachment.ID(attachmentID),
			ticketattachment.TicketID(ticketID),
			ticketattachment.TenantID(tenantID),
		).
		WithUploader().
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get attachment", "error", err)
		return nil, fmt.Errorf("attachment not found: %w", err)
	}

	var uploader *ent.User
	if attachment.Edges.Uploader != nil {
		uploader = attachment.Edges.Uploader
	} else {
		uploader, _ = s.client.User.Get(ctx, attachment.UploadedBy)
	}

	return dto.ToTicketAttachmentResponse(attachment, uploader), nil
}

// DeleteAttachment 删除附件
func (s *TicketAttachmentService) DeleteAttachment(ctx context.Context, ticketID, attachmentID, tenantID, userID int) error {
	s.logger.Infow("Deleting attachment", "ticket_id", ticketID, "attachment_id", attachmentID, "user_id", userID)

	// BE-6 薄适配 + BE-10：先尝试通用删除（软删 + 引用保护），未命中通用表再回退旧表硬删。
	// 与 BE-6 的差别是不再以写开关短路：非默认用途（inline_image / comment_attachment）
	// 的记录只可能在通用表，若按开关短路会出现「用户删不掉刚插进正文的图片 / 评论附件」；
	// 旧表记录在通用表必然未命中（含跨宿主、已软删），仍走原硬删路径，回滚口径不变。
	if s.generic != nil {
		gerr := s.deleteViaGeneric(ctx, ticketID, attachmentID, tenantID, userID)
		if gerr == nil {
			return nil
		}
		if !errors.Is(gerr, ErrAttachmentNotFound) {
			return gerr
		}
		if s.genericWriteEnabled(tenantID) {
			s.logger.Warnw("Attachment not found in generic backend, falling back to legacy delete",
				"ticket_id", ticketID, "attachment_id", attachmentID)
		}
	}

	// 查询附件
	attachment, err := s.client.TicketAttachment.Query().
		Where(
			ticketattachment.ID(attachmentID),
			ticketattachment.TicketID(ticketID),
			ticketattachment.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get attachment", "error", err)
		return fmt.Errorf("attachment not found: %w", err)
	}

	// 权限检查：只有上传人或工单处理人可以删除
	ticketInfo, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get ticket", "error", err)
		return fmt.Errorf("failed to get ticket: %w", err)
	}

	canDelete := attachment.UploadedBy == userID ||
		(ticketInfo.AssigneeID > 0 && ticketInfo.AssigneeID == userID) ||
		ticketInfo.RequesterID == userID
	if !canDelete {
		return fmt.Errorf("permission denied: only uploader, ticket assignee or requester can delete")
	}

	// 删除文件
	if err := os.Remove(attachment.FilePath); err != nil && !os.IsNotExist(err) {
		s.logger.Warnw("Failed to delete file", "error", err, "path", attachment.FilePath)
		// 继续删除数据库记录，即使文件删除失败
	}

	// 删除数据库记录
	err = s.client.TicketAttachment.DeleteOneID(attachmentID).
		Where(
			ticketattachment.TicketID(ticketID),
			ticketattachment.TenantID(tenantID),
		).
		Exec(ctx)
	if err != nil {
		s.logger.Errorw("Failed to delete attachment", "error", err)
		return fmt.Errorf("failed to delete attachment: %w", err)
	}

	return nil
}

// deleteViaGeneric 通用表删除路径：宿主归属校验 + 与旧链路一致的删除权限
// （上传人 / 工单处理人 / 工单发起人）+ 通用软删（引用保护由通用服务负责）。
func (s *TicketAttachmentService) deleteViaGeneric(ctx context.Context, ticketID, attachmentID, tenantID, userID int) error {
	view, err := s.generic.Get(ctx, tenantID, attachmentID)
	if err != nil {
		return err
	}
	if view.BizType != AttachmentBizTypeTicket || view.BizID != ticketID {
		return fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, attachmentID)
	}
	// 仅「存活」的通用记录承担旧端点的删除语义：通用表软删记录不再可删，
	// 交由调用方回退旧表——灰度期两表主键序列独立、同号是常态，若在此静默成功
	// 会把旧表同号记录漏删且对外报成功。
	if view.Status != AttachmentStatusActive {
		return fmt.Errorf("%w: id=%d status=%s", ErrAttachmentNotFound, attachmentID, view.Status)
	}

	ticketInfo, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}

	canDelete := view.UploadedBy == userID ||
		(ticketInfo.AssigneeID > 0 && ticketInfo.AssigneeID == userID) ||
		ticketInfo.RequesterID == userID
	if !canDelete {
		return fmt.Errorf("permission denied: only uploader, ticket assignee or requester can delete")
	}

	return s.generic.Delete(ctx, tenantID, userID, attachmentID)
}

// parseAttachmentRef 解析附件引用：
//   - 数字 → 附件 ID（前端 API 使用）；
//   - 其他 → 上传时生成的存储文件名（历史 fileUrl 形如
//     /api/v1/tickets/{id}/attachments/{ticketID}_{nano}_{name}/download，
//     已被写入富文本 descriptionHtml 落库，必须继续可访问）。
//
// 返回的 id > 0 时按 ID 查询，否则按 file_path 后缀匹配文件名。
func parseAttachmentRef(ref string) (id int, fileName string, err error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return 0, "", fmt.Errorf("empty attachment reference")
	}
	if parsed, aerr := strconv.Atoi(trimmed); aerr == nil && parsed > 0 {
		return parsed, "", nil
	}
	// 存储文件名不应包含路径分隔符，避免用构造出的相对路径去匹配 file_path
	if strings.ContainsAny(trimmed, `/\`) {
		return 0, "", fmt.Errorf("invalid attachment reference: %q", trimmed)
	}
	return 0, trimmed, nil
}

// GetAttachmentFile 获取附件文件（用于下载/预览），支持数字 ID 与存储文件名两种引用。
func (s *TicketAttachmentService) GetAttachmentFile(ctx context.Context, ticketID int, ref string, tenantID, userID int) (*AttachmentFile, error) {
	if err := s.authorizeTicketAttachmentAccess(ctx, ticketID, tenantID, userID); err != nil {
		return nil, err
	}

	attachmentID, fileName, err := parseAttachmentRef(ref)
	if err != nil {
		return nil, err
	}

	// BE-6 薄适配：读开关打开且引用为数字 ID 时优先读通用表；
	// 未命中（尚未回填的历史记录 / 归属不符 / 已软删）继续回退旧表，
	// 保证灰度期历史 URL 不失效，同时不串读其它宿主（乃至其它域）的同号附件。
	if s.genericReadEnabled(tenantID) && attachmentID > 0 {
		stream, gerr := s.generic.GetFileForHost(ctx, tenantID, AttachmentBizTypeTicket, ticketID, attachmentID)
		if gerr == nil {
			mimeType := stream.MimeType
			return &AttachmentFile{
				File:     stream.Reader,
				FileName: stream.FileName,
				MimeType: &mimeType,
				Size:     stream.Size,
			}, nil
		}
		if !errors.Is(gerr, ErrAttachmentNotFound) {
			return nil, gerr
		}
	}

	query := s.client.TicketAttachment.Query().
		Where(
			ticketattachment.TicketID(ticketID),
			ticketattachment.TenantID(tenantID),
		)
	if attachmentID > 0 {
		query = query.Where(ticketattachment.ID(attachmentID))
	} else {
		query = query.Where(ticketattachment.FilePathHasSuffix(fileName))
	}

	attachment, err := query.Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("attachment not found: %w", err)
	}

	file, err := os.Open(attachment.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	mimeType := attachment.MimeType
	if mimeType == "" {
		mimeType = attachment.FileType
	}
	return &AttachmentFile{
		File:     file,
		FileName: attachment.FileName,
		MimeType: &mimeType,
		Size:     int64(attachment.FileSize),
	}, nil
}

// 辅助方法

// FileHeader 文件头信息
type FileHeader struct {
	Filename    string
	Size        int64
	ContentType string
	Reader      io.Reader
}

// AttachmentFile 附件文件（File 由调用方负责 Close；本地存储实现返回 *os.File，
// 通用读链路的 StorageProvider 可能返回其他 io.ReadCloser 实现）。
type AttachmentFile struct {
	File     io.ReadCloser
	FileName string
	MimeType *string
	Size     int64
}

// saveFile 保存文件
func (s *TicketAttachmentService) saveFile(fileHeader *FileHeader, filePath string) error {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建文件
	dst, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	// 复制文件内容
	written, err := io.Copy(dst, io.LimitReader(fileHeader.Reader, s.maxFileSize+1))
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	if written > s.maxFileSize {
		_ = os.Remove(filePath)
		return fmt.Errorf("file size exceeds maximum allowed size (%d bytes)", s.maxFileSize)
	}
	if fileHeader.Size >= 0 && written != fileHeader.Size {
		_ = os.Remove(filePath)
		return fmt.Errorf("uploaded file size does not match declared size")
	}

	return nil
}

func (s *TicketAttachmentService) authorizeTicketAttachmentAccess(ctx context.Context, ticketID, tenantID, userID int) error {
	if userID <= 0 {
		return fmt.Errorf("authentication required")
	}
	t, err := s.client.Ticket.Query().Where(ticket.ID(ticketID), ticket.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return fmt.Errorf("ticket not found")
	}
	if t.RequesterID == userID || (t.AssigneeID > 0 && t.AssigneeID == userID) {
		return nil
	}
	u, err := s.client.User.Query().Where(user.ID(userID), user.TenantID(tenantID), user.Active(true)).Only(ctx)
	if err != nil {
		return fmt.Errorf("permission denied")
	}
	switch string(u.Role) {
	case "super_admin", "admin", "manager", "agent", "technician", "security":
		return nil
	}
	return fmt.Errorf("permission denied")
}

func SanitizeDownloadFilename(name string) string { return sanitizeFilename(name) }

// isAllowedType 检查文件类型是否允许
func (s *TicketAttachmentService) isAllowedType(mimeType string) bool {
	if mimeType == "" {
		return false
	}

	// 检查精确匹配
	for _, allowed := range s.allowedTypes {
		if mimeType == allowed {
			return true
		}
	}

	// 检查类型前缀（如 image/*, application/*）
	parts := strings.Split(mimeType, "/")
	if len(parts) == 2 {
		typePrefix := parts[0] + "/*"
		for _, allowed := range s.allowedTypes {
			if allowed == typePrefix {
				return true
			}
		}
	}

	return false
}

// MaxFileSizeMB 单文件大小上限（MB），供 handler 生成可读提示。
func (s *TicketAttachmentService) MaxFileSizeMB() int {
	return int(s.maxFileSize / (1024 * 1024))
}

// 附件校验的可判定错误：handler 层据此返回可读原因，
// 避免所有失败都落进同一句笼统的“附件上传失败，请检查文件类型和大小”。
var (
	// ErrAttachmentEmpty 0 字节（空）文件。
	ErrAttachmentEmpty = errors.New("attachment is empty")
	// ErrAttachmentTooLarge 超过服务端单文件大小上限。
	ErrAttachmentTooLarge = errors.New("attachment exceeds max size")
	// ErrAttachmentTypeRejected 真实内容/扩展名不在允许范围内。
	ErrAttachmentTypeRejected = errors.New("attachment type not allowed")
	// ErrAttachmentUsageInvalid 上传请求携带了未登记的附件用途（BE-10）。
	// 仅接受 attachment / inline_image / comment_attachment，缺省（空）按 attachment 处理。
	ErrAttachmentUsageInvalid = errors.New("attachment usage not allowed")
	// ErrAttachmentUsageBackendMissing 需要通用后端才能承载的用途未注入通用服务（部署未接线）。
	ErrAttachmentUsageBackendMissing = errors.New("attachment usage requires generic backend")
)

// allowedAttachmentMIMEs 附件 MIME 白名单：内容嗅探命中即放行。
// 嗅探无法唯一识别的类型（docx→application/zip、doc/xls/ppt→application/octet-stream 等）
// 由 allowedExtensionMIME 兜底，两张表需保持一致（见 TestAllowedAttachmentMIMEsCoverExtensionTable）。
func allowedAttachmentMIMEs() []string {
	return []string{
		// 图片
		"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/svg+xml",
		// 文档
		"application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		// 文本
		"text/plain", "text/csv", "text/markdown", "application/json", "application/xml",
		// 压缩文件（application/vnd.rar 是 net/http.DetectContentType 对 rar 的识别结果，
		// 与 x-rar-compressed 并存）
		"application/zip", "application/x-rar-compressed", "application/vnd.rar",
		"application/x-7z-compressed", "application/x-tar", "application/gzip",
	}
}

// allowedExtensionMIME 与前端 ACCEPTED_ATTACHMENT_EXTENSIONS（AttachmentField.tsx）对齐。
// net/http.DetectContentType 只能识别有限的 magic bytes：docx/xlsx/pptx 会被识别成
// application/zip，老式 doc/xls/ppt 与 bmp/7z/tar/gz 会落到 application/octet-stream，
// 这些类型只能用扩展名兜底判定，并落库为规范 MIME。
var allowedExtensionMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",

	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",

	".txt":  "text/plain",
	".csv":  "text/csv",
	".md":   "text/markdown",
	".json": "application/json",
	".log":  "text/plain",
	".xml":  "application/xml",

	".zip": "application/zip",
	".rar": "application/vnd.rar",
	".7z":  "application/x-7z-compressed",
	".tar": "application/x-tar",
	".gz":  "application/gzip",
}

// genericDetectedMIMEs 是“无法唯一映射到白名单主类型”的嗅探结果（容器/通用二进制/文本）。
// 命中时回退到扩展名白名单；未命中说明真实内容与扩展名明显不符（如 text/html 伪装成 .png），直接拒绝。
var genericDetectedMIMEs = map[string]struct{}{
	"application/octet-stream":     {},
	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/x-gzip":           {},
	"application/gzip":             {},
	"text/plain":                   {},
	"text/xml":                     {},
	"application/xml":              {},
}

// normalizeMIME 去掉 DetectContentType / 浏览器传来的参数（charset 等）并转小写，
// 例如 "text/plain; charset=utf-8" -> "text/plain"。
// 这正是此前 .txt/.csv/.md/.json/.xml 等文本附件被误判为“类型不允许”的原因。
func normalizeMIME(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(v); err == nil {
		return strings.ToLower(strings.TrimSpace(mediaType))
	}
	if idx := strings.IndexByte(v, ';'); idx >= 0 {
		v = v[:idx]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// resolveMIMEType 决定最终落库的 MIME 类型。
// detected 为内容嗅探结果（已 normalize），claimed 为客户端声明值（仅作日志/兜底）。
// 优先级：扩展名 + 容器/通用型嗅探结果（如 .docx → application/zip）> 嗅探结果直接命中白名单。
func (s *TicketAttachmentService) resolveMIMEType(filename, claimed, detected string) (string, error) {
	if detected == "" {
		// 无法嗅探（例如无 Reader 的调用方）时退回声明类型，保持既有行为
		detected = normalizeMIME(claimed)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	canonical, extAllowed := allowedExtensionMIME[ext]
	if _, generic := genericDetectedMIMEs[detected]; extAllowed && generic {
		// 嗅探无法区分容器/通用二进制（docx/xlsx/pptx 实为 zip，老式 Office 为 octet-stream），
		// 以扩展名白名单的规范 MIME 落库，避免丢掉“到底是哪种文档”的信息。
		return canonical, nil
	}
	if s.isAllowedType(detected) {
		return detected, nil
	}
	return "", fmt.Errorf("%w: filename=%q detected=%q claimed=%q", ErrAttachmentTypeRejected, filename, detected, claimed)
}

// sanitizeFilename cleans an upload filename for safe on-disk + header usage.
// - disallows path separators, control chars, NUL, leading dots, relative segments
// - limits length to 200 runes
func sanitizeFilename(name string) string {
	if name == "" {
		return ""
	}
	// 路径遍历防御：剥掉任何目录部分
	name = filepath.Base(name)
	// 去掉 Windows 驱动器前缀和反斜杠路径段
	if strings.ContainsRune(name, '\\') {
		parts := strings.FieldsFunc(name, func(r rune) bool { return r == '\\' })
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
	}
	// 剥掉控制字符、NUL、以及可能触发 shell/URL 二次解析的危险字符
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			continue // control / DEL
		case r == '/' || r == '\\' || r == 0:
			continue
		case r == '%' || r == '`' || r == '|' || r == '&' || r == ';' || r == '>' || r == '<' || r == '"' || r == '\'' || r == '*' || r == '?':
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	out = strings.TrimLeft(out, ". ") // 防 ../ 和 dotfiles
	if out == "" || out == "." || out == ".." {
		return ""
	}
	// 截断到 200 runes
	runes := []rune(out)
	if len(runes) > 200 {
		out = string(runes[:200])
	}
	return out
}

// prefixedReader prepends sniffed bytes back onto the original reader so
// downstream consumers of fileHeader.Reader see the full stream.
type prefixedReader struct {
	prefix []byte
	off    int
	r      io.Reader
}

func (p *prefixedReader) Read(b []byte) (int, error) {
	if p.off < len(p.prefix) {
		n := copy(b, p.prefix[p.off:])
		p.off += n
		if n == len(b) {
			return n, nil
		}
		// 继续从底层 reader 填剩下的空间
		n2, err := p.r.Read(b[n:])
		return n + n2, err
	}
	return p.r.Read(b)
}
