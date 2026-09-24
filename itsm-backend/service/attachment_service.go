package service

// 通用附件服务（BE-3，方案 §3/§5.3）。
//
// 设计要点：
//   - 宿主无关：`biz_type + biz_id` 由 AttachmentHost 注册表解析，本期内置 ticket /
//     ticket_comment / knowledge_article / service_request，新增宿主只需 SetHost 注册，
//     不改本文件的上传/读取主流程。
//   - 存储抽象：StorageProvider 隔离路径生成与 IO；本期提供本地磁盘实现
//     LocalStorageProvider（复用现网 `uploads/tickets` 根目录），对象存储留待后续适配。
//   - 加固继承：完整平移工单附件的四件套——magic bytes 嗅探（不信任 Content-Type）、
//     病毒扫描（保存后扫描，失败即删文件）、写后大小二次复核、文件名清洗 + 扩展名白名单。
//     其中 sanitizeFilename / normalizeMIME / allowedAttachmentMIMEs / allowedExtensionMIME /
//     genericDetectedMIMEs / prefixedReader / ErrAttachmentEmpty|TooLarge|TypeRejected 为
//     本包既有实现（ticket_attachment_service.go），此处直接复用，不复制表数据。
//   - 幂等：`client_token` 唯一索引（迁移 §5.1）去重，重试返回既有记录；并发下发唯一冲突
//     时回查赢家并清理本次多余文件（先例 handlers/email_intake/orchestrator.go）。
//   - 删除：仅软删（status=deleted + deleted_at），被引用的 inline_image / comment_attachment
//     返回 ErrAttachmentInUse（→ 409/6105）且不改变任何状态；物理文件由 BE-8 清理任务回收。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
	changeent "itsm-backend/ent/change"
	incidentent "itsm-backend/ent/incident"
	"itsm-backend/ent/knowledgearticle"
	probent "itsm-backend/ent/problem"
	"itsm-backend/ent/servicerequest"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/ticketcomment"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// 宿主业务类型（写入 attachments.biz_type，长度上限 64）。
	AttachmentBizTypeTicket           = "ticket"
	AttachmentBizTypeTicketComment    = "ticket_comment"
	AttachmentBizTypeKnowledgeArticle = "knowledge_article"
	AttachmentBizTypeServiceRequest   = "service_request"
	AttachmentBizTypeChange           = "change"
	AttachmentBizTypeIncident         = "incident"
	AttachmentBizTypeProblem          = "problem"

	// 用途（写入 attachments.usage，长度上限 32）。
	AttachmentUsageAttachment        = "attachment"
	AttachmentUsageInlineImage       = "inline_image"
	AttachmentUsageCommentAttachment = "comment_attachment"

	// 状态（写入 attachments.status）。
	AttachmentStatusActive  = "active"
	AttachmentStatusDeleted = "deleted"

	// AttachmentMaxBatchIDs A6 批量回填的上限（防滥用，handler 层同步校验）。
	AttachmentMaxBatchIDs = 200

	attachmentDefaultMaxFileSize = 10 * 1024 * 1024 // 10MB，与工单附件口径一致（v1.0 决策）
	attachmentDefaultPageSize    = 20
	attachmentRootDir            = "uploads/tickets" // 根目录沿用现网，配置化后迁移（§5.3）
)

// 服务层可判定错误：handler 据此映射错误码（§3.4 的 6101-6107）。
var (
	// ErrAttachmentNotFound 记录不存在或不属于当前租户（统一 404，不泄露跨租户存在性）。
	ErrAttachmentNotFound = errors.New("attachment not found")
	// ErrAttachmentHostNotFound 宿主不存在/跨租户/未注册的 biz_type（→ 404/6101）。
	ErrAttachmentHostNotFound = errors.New("attachment host not found")
	// ErrAttachmentInvalidFilename 清洗后文件名为空（→ 400/6102）。
	ErrAttachmentInvalidFilename = errors.New("attachment filename invalid")
	// ErrAttachmentInUse 被宿主正文或评论引用，拒绝软删（→ 409/6105）。
	ErrAttachmentInUse = errors.New("attachment is in use")
	// ErrAttachmentSizeMismatch 落盘字节数与声明不一致（→ 400）。
	ErrAttachmentSizeMismatch = errors.New("attachment size does not match declared size")
	// ErrAttachmentStorage 存储层失败（→ 500）。
	ErrAttachmentStorage = errors.New("attachment storage failure")
	// ErrAttachmentRejected 病毒扫描拒绝（→ 415/6104，与类型拒绝同码便于前端提示重新选文件）。
	ErrAttachmentRejected = errors.New("attachment rejected by malware scan")
	// ErrAttachmentQuotaExceeded 配额超限（→ 422/6106）。单宿主/单用户配额校验由后续批次
	// （BE-8 清理与配额任务）接入；本期登记错误映射，避免配额落地时新增未登记码。
	ErrAttachmentQuotaExceeded = errors.New("attachment quota exceeded")
)

// StorageProvider 存储抽象（§5.3）：隔离路径生成与 IO，便于后续接对象存储。
//
// 约定：
//   - key 由服务层生成（`{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}`），实现不得信任调用方传入的路径分隔符；
//   - Save 必须自带写后大小复核（读取 maxBytes+1 即判超限），返回实际写入字节数；
//   - Delete 幂等：目标不存在视为成功；
//   - Open 返回的 ReadCloser 由调用方负责关闭。
type StorageProvider interface {
	Save(ctx context.Context, key string, r io.Reader, maxBytes int64) (written int64, err error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (int64, error)
}

// LocalStorageProvider 本地磁盘实现。
type LocalStorageProvider struct {
	root string
}

// NewLocalStorageProvider root 为相对/绝对根目录（默认 `uploads/tickets`）。
func NewLocalStorageProvider(root string) *LocalStorageProvider {
	if strings.TrimSpace(root) == "" {
		root = attachmentRootDir
	}
	return &LocalStorageProvider{root: root}
}

// Root 返回根目录（测试与运维排查用）。
func (p *LocalStorageProvider) Root() string { return p.root }

// LocalPath 返回 key 对应的文件系统路径，供病毒扫描等需要真实路径的组件使用。
func (p *LocalStorageProvider) LocalPath(key string) (string, error) { return p.resolve(key) }

// resolve 双规则寻址（§5.3）：
//   - 历史值形如 `uploads/tickets/{ticketID}_{nano}_{name}`（P2 回填的数据），是仓库相对路径，按原样读取；
//   - 新 key 形如 `{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}`，相对 root 拼接。
func (p *LocalStorageProvider) resolve(key string) (string, error) {
	k := strings.TrimSpace(key)
	if k == "" || strings.Contains(k, "..") {
		return "", fmt.Errorf("%w: invalid storage key %q", ErrAttachmentStorage, key)
	}
	k = filepath.FromSlash(k)
	// 防御：绝对路径一律拒绝。注意 Windows 上 `filepath.IsAbs("/etc/passwd")` 为 false，
	// 故同时拦截 Unix 风格根路径，避免跨平台行为差异。
	if filepath.IsAbs(k) || strings.HasPrefix(filepath.ToSlash(k), "/") {
		return "", fmt.Errorf("%w: absolute storage key %q", ErrAttachmentStorage, key)
	}
	if strings.HasPrefix(filepath.ToSlash(k), attachmentRootDir+"/") {
		return k, nil
	}
	return filepath.Join(p.root, k), nil
}

func (p *LocalStorageProvider) Save(ctx context.Context, key string, r io.Reader, maxBytes int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r == nil {
		return 0, fmt.Errorf("%w: nil reader", ErrAttachmentStorage)
	}
	path, err := p.resolve(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("%w: create dir: %v", ErrAttachmentStorage, err)
	}
	dst, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("%w: create file: %v", ErrAttachmentStorage, err)
	}
	defer dst.Close()

	// 写后大小二次复核：多读 1 字节即可判定超限。
	written, err := io.Copy(dst, io.LimitReader(r, maxBytes+1))
	if err != nil {
		_ = os.Remove(path)
		return 0, fmt.Errorf("%w: write file: %v", ErrAttachmentStorage, err)
	}
	if written > maxBytes {
		_ = os.Remove(path)
		return 0, fmt.Errorf("%w: written=%d bytes, max=%d bytes", ErrAttachmentTooLarge, written, maxBytes)
	}
	return written, nil
}

func (p *LocalStorageProvider) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := p.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrAttachmentStorage, key, err)
	}
	return f, nil
}

func (p *LocalStorageProvider) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := p.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: remove %q: %v", ErrAttachmentStorage, key, err)
	}
	return nil
}

func (p *LocalStorageProvider) Stat(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	path, err := p.resolve(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("%w: stat %q: %v", ErrAttachmentStorage, key, err)
	}
	return info.Size(), nil
}

// AttachmentHost 宿主适配：存在性校验与正文引用判定（引用保护，§5.3）。
//
// References 仅负责"宿主正文是否引用了该附件"（如 ticket.description_html 中的
// `data-attachment-id`）；评论附件走 ticket_comments.attachments 数组，由服务内单独的
// 判定函数处理（biz_type='ticket' + usage='comment_attachment'，见 §3.2 注）。
type AttachmentHost interface {
	Exists(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error)
	References(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error)
}

// AttachmentHostFunc 便于测试注入与宿主扩展。
type AttachmentHostFunc struct {
	ExistsFunc     func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error)
	ReferencesFunc func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error)
}

func (f AttachmentHostFunc) Exists(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
	if f.ExistsFunc == nil {
		return false, nil
	}
	return f.ExistsFunc(ctx, client, tenantID, bizID)
}

func (f AttachmentHostFunc) References(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
	if f.ReferencesFunc == nil {
		return false, nil
	}
	return f.ReferencesFunc(ctx, client, tenantID, bizID, attachmentID)
}

// AttachmentService 通用附件服务。
type AttachmentService struct {
	client       *ent.Client
	logger       *zap.SugaredLogger
	storage      StorageProvider
	maxFileSize  int64
	allowedTypes []string
	virusScanner AttachmentVirusScanner
	hosts        map[string]AttachmentHost
}

// AttachmentUploadInput A1 上传入参（handler 负责 multipart 解析与权限判定）。
type AttachmentUploadInput struct {
	BizType     string
	BizID       int
	Usage       string
	ClientToken string
	File        *FileHeader
}

// AttachmentView 服务层视图（BE-4 映射为 HTTP DTO）。
// 存储 key 属内部实现：只以 `json:"-"` 暴露给同进程适配器（BE-6 旧工单端点映射 filePath），
// 不进入任何 HTTP 响应；外部下载统一走 A4 端点。
type AttachmentView struct {
	ID          int        `json:"id"`
	TenantID    int        `json:"tenantId"`
	BizType     string     `json:"bizType"`
	BizID       int        `json:"bizId"`
	Usage       string     `json:"usage"`
	FileName    string     `json:"fileName"`
	FileURL     string     `json:"fileUrl"`
	FileSize    int        `json:"fileSize"`
	FileType    string     `json:"fileType"`
	MimeType    string     `json:"mimeType"`
	SHA256      string     `json:"sha256,omitempty"`
	ClientToken string     `json:"clientToken,omitempty"`
	UploadedBy  int        `json:"uploadedBy"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
	// StorageKey 存储相对路径（attachments.file_path），仅供同进程适配器读取。
	StorageKey string `json:"-"`
}

// AttachmentStream A4 下载/预览流（handler 负责 Content-Disposition/Range）。
type AttachmentStream struct {
	Reader   io.ReadCloser
	FileName string
	MimeType string
	Size     int64
	View     *AttachmentView
}

// NewAttachmentService storage 为 nil 时使用本地实现（`uploads/tickets` 根目录）。
func NewAttachmentService(client *ent.Client, logger *zap.SugaredLogger, storage StorageProvider) *AttachmentService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	if storage == nil {
		storage = NewLocalStorageProvider(attachmentRootDir)
	}
	return &AttachmentService{
		client:       client,
		logger:       logger,
		storage:      storage,
		maxFileSize:  attachmentDefaultMaxFileSize,
		allowedTypes: allowedAttachmentMIMEs(),
		virusScanner: noopAttachmentVirusScanner{},
		hosts:        defaultAttachmentHosts(),
	}
}

// SetVirusScanner 注入病毒扫描器（nil 忽略，保持 noop）。
func (s *AttachmentService) SetVirusScanner(scanner AttachmentVirusScanner) {
	if scanner != nil {
		s.virusScanner = scanner
	}
}

// SetHost 注册/覆盖宿主适配（测试注入与后续宿主扩展）。
func (s *AttachmentService) SetHost(bizType string, host AttachmentHost) {
	if s.hosts == nil {
		s.hosts = map[string]AttachmentHost{}
	}
	s.hosts[strings.TrimSpace(bizType)] = host
}

// Storage 返回存储实现（测试与运维用）。
func (s *AttachmentService) Storage() StorageProvider { return s.storage }

// MaxFileSizeMB 单文件大小上限（MB），供 handler 生成可读提示。
func (s *AttachmentService) MaxFileSizeMB() int { return int(s.maxFileSize / (1024 * 1024)) }

// Upload A1：校验 → 宿主存在性 → 幂等预检 → 加固四件套 → 落盘 → 落库 → 回写 file_url → 审计。
func (s *AttachmentService) Upload(ctx context.Context, tenantID, userID int, in AttachmentUploadInput) (*AttachmentView, error) {
	if tenantID <= 0 || userID <= 0 {
		return nil, fmt.Errorf("tenant and user are required")
	}
	header := in.File
	if header == nil || header.Reader == nil {
		return nil, fmt.Errorf("attachment file content is required")
	}
	bizType := strings.TrimSpace(in.BizType)
	if bizType == "" || in.BizID <= 0 {
		return nil, fmt.Errorf("bizType and bizId are required")
	}
	if len(bizType) > 64 {
		return nil, fmt.Errorf("bizType is too long (max 64)")
	}
	usage := strings.TrimSpace(in.Usage)
	if usage == "" {
		usage = AttachmentUsageAttachment
	}
	if len(usage) > 32 {
		return nil, fmt.Errorf("usage is too long (max 32)")
	}
	token := strings.TrimSpace(in.ClientToken)
	if len(token) > 64 {
		return nil, fmt.Errorf("clientToken is too long (max 64)")
	}

	// 入参级大小校验先于任何 IO/DB 查询，保证该分支可单测（与工单附件同序）。
	if header.Size > s.maxFileSize {
		return nil, fmt.Errorf("%w: size=%d bytes, max=%d bytes", ErrAttachmentTooLarge, header.Size, s.maxFileSize)
	}
	if header.Size <= 0 {
		return nil, fmt.Errorf("%w: size=%d bytes", ErrAttachmentEmpty, header.Size)
	}
	if err := s.ensureHostExists(ctx, bizType, tenantID, in.BizID); err != nil {
		return nil, err
	}

	// 幂等预检：命中即复用既有记录，不做任何文件 IO（重试不产生新文件）。
	if token != "" {
		existing, err := s.findByClientToken(ctx, tenantID, bizType, in.BizID, token)
		if err == nil {
			s.logger.Infow("Attachment upload idempotent hit", "attachment_id", existing.ID, "biz_type", bizType, "biz_id", in.BizID)
			return toAttachmentView(existing), nil
		}
		if !ent.IsNotFound(err) {
			return nil, fmt.Errorf("failed to check client token: %w", err)
		}
	}

	// 加固①：文件名清洗（路径遍历/控制字符/长度）。
	safeName := sanitizeFilename(header.Filename)
	if safeName == "" {
		return nil, fmt.Errorf("%w: %q", ErrAttachmentInvalidFilename, header.Filename)
	}
	// 客户端声明的 Content-Type 可伪造，仅作兜底参考。
	claimed := strings.TrimSpace(header.ContentType)
	if claimed == "" {
		claimed = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	// 加固②：magic bytes 嗅探（不信任 Content-Type）。
	detected, err := sniffAttachmentContent(header)
	if err != nil {
		return nil, err
	}
	mimeType, err := resolveAttachmentMIMEType(safeName, claimed, detected, s.allowedTypes)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%d/%s/%d/%s%s", tenantID, bizType, in.BizID, uuid.NewString(), strings.ToLower(filepath.Ext(safeName)))
	hasher := sha256.New()
	// 加固③：写后大小二次复核在 StorageProvider.Save 内完成（LimitReader max+1）。
	written, err := s.storage.Save(ctx, key, io.TeeReader(header.Reader, hasher), s.maxFileSize)
	if err != nil {
		return nil, err
	}
	if written != header.Size {
		_ = s.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: declared=%d bytes, written=%d bytes", ErrAttachmentSizeMismatch, header.Size, written)
	}
	// 加固④：病毒扫描（保存后扫描，失败即删文件，调用点语义与工单附件一致）。
	if err := s.scanAttachment(ctx, key); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: %v", ErrAttachmentRejected, err)
	}

	create := s.client.Attachment.Create().
		SetTenantID(tenantID).
		SetBizType(bizType).
		SetBizID(in.BizID).
		SetUsage(usage).
		SetFileName(safeName).
		SetFilePath(key).
		SetFileSize(int(written)).
		SetFileType(mimeType).
		SetMimeType(mimeType).
		SetUploadedBy(userID).
		SetStatus(AttachmentStatusActive)
	if hash := hex.EncodeToString(hasher.Sum(nil)); hash != "" {
		create = create.SetSha256(hash)
	}
	if token != "" {
		create = create.SetClientToken(token)
	}

	att, err := create.Save(ctx)
	if err != nil {
		// 并发重试：唯一索引冲突说明别的请求已赢，回查赢家并清理本次多余文件。
		if token != "" && ent.IsConstraintError(err) {
			if existing, qerr := s.findByClientToken(ctx, tenantID, bizType, in.BizID, token); qerr == nil {
				_ = s.storage.Delete(ctx, key)
				return toAttachmentView(existing), nil
			}
		}
		_ = s.storage.Delete(ctx, key)
		s.logger.Errorw("Failed to create attachment record", "error", err, "biz_type", bizType, "biz_id", in.BizID)
		return nil, fmt.Errorf("failed to create attachment record: %w", err)
	}

	// 规范访问地址（A4）；回写失败不回滚记录，仅告警。
	fileURL := fmt.Sprintf("/api/v1/attachments/%d/content", att.ID)
	if updated, uerr := att.Update().SetFileURL(fileURL).Save(ctx); uerr != nil {
		s.logger.Warnw("Failed to persist canonical attachment url", "error", uerr, "attachment_id", att.ID)
	} else {
		att = updated
	}

	s.auditAttachment(ctx, tenantID, userID, "upload", fileURL, "POST", 200,
		fmt.Sprintf(`{"bizType":%q,"bizId":%d,"usage":%q,"fileName":%q,"size":%d}`, bizType, in.BizID, usage, safeName, written))

	s.logger.Infow("Attachment uploaded", "attachment_id", att.ID, "biz_type", bizType, "biz_id", in.BizID, "size", written)
	return toAttachmentView(att), nil
}

// findByClientToken 幂等键查询（租户 + 宿主 + token 三元组，与唯一索引同构）。
func (s *AttachmentService) findByClientToken(ctx context.Context, tenantID int, bizType string, bizID int, token string) (*ent.Attachment, error) {
	return s.client.Attachment.Query().
		Where(
			attachment.TenantID(tenantID),
			attachment.BizTypeEQ(bizType),
			attachment.BizID(bizID),
			attachment.ClientTokenEQ(token),
		).
		Only(ctx)
}

// sniffAttachmentContent 加固②：读取文件头（最多 512 字节）做内容嗅探，
// 并把嗅探过的字节 prepend 回 header.Reader，保证后续写盘拿到完整内容。
// 语义与 ticket_attachment_service.go 的同名逻辑一致（BE-6 统一前保持双实现）。
func sniffAttachmentContent(header *FileHeader) (string, error) {
	sniffBuf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for len(sniffBuf) < 512 {
		n, rerr := header.Reader.Read(tmp)
		if n > 0 {
			sniffBuf = append(sniffBuf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	if len(sniffBuf) == 0 {
		return "", fmt.Errorf("%w: no readable bytes", ErrAttachmentEmpty)
	}
	header.Reader = &prefixedReader{prefix: sniffBuf, r: header.Reader}
	return normalizeMIME(http.DetectContentType(sniffBuf)), nil
}

// isAllowedAttachmentType 与工单附件同口径：精确匹配或类型前缀（如 image/*）匹配。
func isAllowedAttachmentType(mimeType string, allowed []string) bool {
	if mimeType == "" {
		return false
	}
	for _, a := range allowed {
		if mimeType == a {
			return true
		}
	}
	if parts := strings.Split(mimeType, "/"); len(parts) == 2 {
		prefix := parts[0] + "/*"
		for _, a := range allowed {
			if a == prefix {
				return true
			}
		}
	}
	return false
}

// resolveAttachmentMIMEType 决定落库 MIME（与工单附件同优先级）：
// 扩展名 + 容器/通用型嗅探结果（docx→zip、doc→octet-stream）→ 扩展名规范 MIME；
// 否则嗅探结果命中白名单即放行；再否则拒绝（ErrAttachmentTypeRejected）。
func resolveAttachmentMIMEType(filename, claimed, detected string, allowed []string) (string, error) {
	if detected == "" {
		detected = normalizeMIME(claimed)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	canonical, extAllowed := allowedExtensionMIME[ext]
	if _, generic := genericDetectedMIMEs[detected]; extAllowed && generic {
		return canonical, nil
	}
	if isAllowedAttachmentType(detected, allowed) {
		return detected, nil
	}
	return "", fmt.Errorf("%w: filename=%q detected=%q claimed=%q", ErrAttachmentTypeRejected, filename, detected, claimed)
}

// scanAttachment 病毒扫描；本地存储下用真实路径，其他实现退化为 key（由扫描器自行解释）。
func (s *AttachmentService) scanAttachment(ctx context.Context, key string) error {
	if s.virusScanner == nil {
		return nil
	}
	target := key
	if p, ok := s.storage.(interface {
		LocalPath(string) (string, error)
	}); ok {
		if local, err := p.LocalPath(key); err == nil {
			target = local
		}
	}
	return s.virusScanner.Scan(ctx, target)
}

// ensureHostExists 宿主存在性校验：不注册/不存在/跨租户一律 ErrAttachmentHostNotFound（404）。
func (s *AttachmentService) ensureHostExists(ctx context.Context, bizType string, tenantID, bizID int) error {
	host, ok := s.hosts[bizType]
	if !ok || host == nil {
		return fmt.Errorf("%w: unsupported bizType=%q", ErrAttachmentHostNotFound, bizType)
	}
	exists, err := host.Exists(ctx, s.client, tenantID, bizID)
	if err != nil {
		return fmt.Errorf("failed to check attachment host: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: %s/%d", ErrAttachmentHostNotFound, bizType, bizID)
	}
	return nil
}

// auditAttachment 审计直写（先例 service/role_service.go：写失败不回滚业务，但必须告警）。
func (s *AttachmentService) auditAttachment(ctx context.Context, tenantID, userID int, action, path, method string, statusCode int, body string) {
	if s.client == nil {
		return
	}
	create := s.client.AuditLog.Create().
		SetTenantID(tenantID).
		SetResource("attachment").
		SetAction(action).
		SetPath(path).
		SetMethod(method).
		SetStatusCode(statusCode)
	if userID > 0 {
		create = create.SetUserID(userID)
	}
	if body != "" {
		if len(body) > 512 {
			body = body[:512]
		}
		create = create.SetRequestBody(body)
	}
	if _, err := create.Save(ctx); err != nil {
		s.logger.Warnw("Failed to write attachment audit log", "error", err, "action", action, "attachment_path", path)
	}
}

// toAttachmentView 实体 → 服务层视图（不暴露 file_path）。
func toAttachmentView(att *ent.Attachment) *AttachmentView {
	if att == nil {
		return nil
	}
	view := &AttachmentView{
		ID:          att.ID,
		TenantID:    att.TenantID,
		BizType:     att.BizType,
		BizID:       att.BizID,
		Usage:       att.Usage,
		FileName:    att.FileName,
		FileURL:     att.FileURL,
		FileSize:    att.FileSize,
		FileType:    att.FileType,
		MimeType:    att.MimeType,
		SHA256:      att.Sha256,
		ClientToken: att.ClientToken,
		UploadedBy:  att.UploadedBy,
		Status:      att.Status,
		CreatedAt:   att.CreatedAt,
		DeletedAt:   att.DeletedAt,
		StorageKey:  att.FilePath,
	}
	return view
}

// List A2：宿主维度列表（默认仅 active），返回分页与总数。
func (s *AttachmentService) List(ctx context.Context, tenantID int, bizType string, bizID int, usage string, offset, limit int) ([]*AttachmentView, int, error) {
	bizType = strings.TrimSpace(bizType)
	if tenantID <= 0 || bizID <= 0 || bizType == "" {
		return nil, 0, fmt.Errorf("bizType and bizId are required")
	}
	if err := s.ensureHostExists(ctx, bizType, tenantID, bizID); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = attachmentDefaultPageSize
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	q := s.client.Attachment.Query().Where(
		attachment.TenantID(tenantID),
		attachment.BizTypeEQ(bizType),
		attachment.BizID(bizID),
		attachment.StatusEQ(AttachmentStatusActive),
	)
	if u := strings.TrimSpace(usage); u != "" {
		q = q.Where(attachment.UsageEQ(u))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count attachments: %w", err)
	}
	rows, err := q.
		Order(ent.Desc(attachment.FieldCreatedAt), ent.Desc(attachment.FieldID)).
		Offset(offset).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list attachments: %w", err)
	}
	views := make([]*AttachmentView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toAttachmentView(row))
	}
	return views, total, nil
}

// Get A3：单条元数据（租户过滤，跨租户统一 ErrAttachmentNotFound）。
//
// BE-8 收紧：软删记录按未命中处理（与 GetFile/List/BatchGet 一致），
// 避免「内容不可读、元数据仍可读」的中间态泄漏下载入口（旧工单链路走 GetFile，不受影响）。
func (s *AttachmentService) Get(ctx context.Context, tenantID, id int) (*AttachmentView, error) {
	if tenantID <= 0 || id <= 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
	}
	att, err := s.client.Attachment.Query().
		Where(
			attachment.ID(id),
			attachment.TenantID(tenantID),
			attachment.StatusEQ(AttachmentStatusActive),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
		}
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	return toAttachmentView(att), nil
}

// LookupForAuthorization A5 辅助：按租户取任意状态（含已软删）的元数据，仅供 handler
// 做宿主归属鉴权与幂等判定，不对外暴露为读取接口（A3 Get 仍按 soft-deleted → 404）。
//
// 存在原因：A5 契约要求「重复删除幂等」（第二次仍 200），但删除前必须完成宿主鉴权，
// 而 BE-8 已把 Get 收紧为「软删即未命中」——若复用 Get，第二次删除会退化成 404。
func (s *AttachmentService) LookupForAuthorization(ctx context.Context, tenantID, id int) (*AttachmentView, error) {
	if tenantID <= 0 || id <= 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
	}
	att, err := s.client.Attachment.Query().
		Where(
			attachment.ID(id),
			attachment.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
		}
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	return toAttachmentView(att), nil
}

// GetFile A4：下载/预览流（软删记录不可读取）；调用方负责关闭 Reader。
func (s *AttachmentService) GetFile(ctx context.Context, tenantID, id int) (*AttachmentStream, error) {
	return s.getFile(ctx, tenantID, id, "", 0)
}

// GetFileForHost A4 的宿主定向变体：除租户外再校验 biz_type/biz_id 归属。
//
// 供「路径宿主 + 附件 ID」的旧端点适配使用：灰度期通用表与旧表主键序列独立、
// 同号是常态，若只按 ID + 租户取流，调用方就能用宿主 A 的路径读到宿主 B
// （乃至其它 biz_type）的同号附件——归属不符一律按未命中处理。
func (s *AttachmentService) GetFileForHost(ctx context.Context, tenantID int, bizType string, bizID, id int) (*AttachmentStream, error) {
	return s.getFile(ctx, tenantID, id, bizType, bizID)
}

// getFile 按租户（bizType 非空时追加宿主）取附件流；软删记录不可读取。
func (s *AttachmentService) getFile(ctx context.Context, tenantID, id int, bizType string, bizID int) (*AttachmentStream, error) {
	if tenantID <= 0 || id <= 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
	}
	q := s.client.Attachment.Query().Where(attachment.ID(id), attachment.TenantID(tenantID))
	if bizType != "" {
		q = q.Where(attachment.BizTypeEQ(bizType), attachment.BizID(bizID))
	}
	att, err := q.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
		}
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	if att.Status != AttachmentStatusActive {
		return nil, fmt.Errorf("%w: id=%d status=%s", ErrAttachmentNotFound, id, att.Status)
	}
	rc, err := s.storage.Open(ctx, att.FilePath)
	if err != nil {
		return nil, err
	}
	mimeType := att.MimeType
	if mimeType == "" {
		mimeType = att.FileType
	}
	return &AttachmentStream{
		Reader:   rc,
		FileName: att.FileName,
		MimeType: mimeType,
		Size:     int64(att.FileSize),
		View:     toAttachmentView(att),
	}, nil
}

// Delete A5：仅软删。被宿主体引用（inline_image）或评论引用（comment_attachment）时返回
// ErrAttachmentInUse（→ 409/6105）且不改变任何状态；重复删除幂等。
func (s *AttachmentService) Delete(ctx context.Context, tenantID, userID, id int) error {
	if tenantID <= 0 || id <= 0 {
		return fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
	}
	att, err := s.client.Attachment.Query().
		Where(attachment.ID(id), attachment.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("%w: id=%d", ErrAttachmentNotFound, id)
		}
		return fmt.Errorf("failed to get attachment: %w", err)
	}
	if att.Status == AttachmentStatusDeleted {
		return nil // 幂等：已软删视为成功
	}

	referenced, _, err := s.attachmentReferenced(ctx, att)
	if err != nil {
		return fmt.Errorf("failed to check attachment references: %w", err)
	}
	if referenced {
		return fmt.Errorf("%w: id=%d usage=%s", ErrAttachmentInUse, att.ID, att.Usage)
	}

	// 软删：状态迁移 + 标记时间；物理文件由 BE-8 清理任务在保留期后回收。
	if _, err := s.client.Attachment.UpdateOneID(att.ID).
		Where(attachment.TenantID(tenantID)).
		SetStatus(AttachmentStatusDeleted).
		SetDeletedAt(time.Now()).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to soft delete attachment: %w", err)
	}
	s.auditAttachment(ctx, tenantID, userID, "delete", fmt.Sprintf("/api/v1/attachments/%d", att.ID), "DELETE", 200,
		fmt.Sprintf(`{"bizType":%q,"bizId":%d,"usage":%q}`, att.BizType, att.BizID, att.Usage))
	s.logger.Infow("Attachment soft deleted", "attachment_id", att.ID, "user_id", userID)
	return nil
}

// BatchGet A6：批量元数据回填（≤200，去重，租户过滤；调用方负责按 (biz_type,biz_id) 去重后
// 批量校验宿主，禁止逐条查宿主）。
func (s *AttachmentService) BatchGet(ctx context.Context, tenantID int, ids []int) ([]*AttachmentView, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("tenant is required")
	}
	if len(ids) > AttachmentMaxBatchIDs {
		return nil, fmt.Errorf("too many attachment ids (max %d)", AttachmentMaxBatchIDs)
	}
	uniq := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return []*AttachmentView{}, nil
	}
	rows, err := s.client.Attachment.Query().
		Where(
			attachment.TenantID(tenantID),
			attachment.IDIn(uniq...),
			attachment.StatusEQ(AttachmentStatusActive),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query attachments: %w", err)
	}
	views := make([]*AttachmentView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toAttachmentView(row))
	}
	return views, nil
}

// ticketCommentReferencesAttachment 评论引用判定：ticket_comments.attachments 数组是否含该附件。
// biz_type='ticket' 时按 TicketID 匹配全工单评论（§3.2 注：评论附件以 ticket 为宿主上传）；
// biz_type='ticket_comment' 时按评论自身 ID 匹配。
func (s *AttachmentService) ticketCommentReferencesAttachment(ctx context.Context, tenantID int, att *ent.Attachment) (bool, error) {
	q := s.client.TicketComment.Query().Where(ticketcomment.TenantID(tenantID))
	switch att.BizType {
	case AttachmentBizTypeTicket:
		q = q.Where(ticketcomment.TicketID(att.BizID))
	case AttachmentBizTypeTicketComment:
		q = q.Where(ticketcomment.ID(att.BizID))
	default:
		return false, nil
	}
	comments, err := q.All(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range comments {
		for _, id := range c.Attachments {
			if id == att.ID {
				return true, nil
			}
		}
	}
	return false, nil
}

// htmlReferencesAttachment 判定富文本是否引用指定附件：`data-attachment-id="N"`（含单引号/
// 无引号形式）。无引号形式需保证数字边界，避免 12 误匹配 123。
func htmlReferencesAttachment(html string, attachmentID int) bool {
	if html == "" || attachmentID <= 0 {
		return false
	}
	id := strconv.Itoa(attachmentID)
	const marker = "data-attachment-id="
	for idx := 0; idx < len(html); {
		pos := strings.Index(html[idx:], marker)
		if pos < 0 {
			break
		}
		start := idx + pos + len(marker)
		if start < len(html) && (html[start] == '"' || html[start] == '\'') {
			start++
		}
		if strings.HasPrefix(html[start:], id) {
			next := start + len(id)
			if next >= len(html) || html[next] < '0' || html[next] > '9' {
				return true
			}
		}
		idx = idx + pos + len(marker)
	}
	return false
}

// defaultAttachmentHosts 内置宿主适配（BE-5 的知识库/服务请求别名路由复用
// knowledge_article 与 service_request 两项；富文本第二波新增 change / incident /
// problem 三项，路由见 router/change_routes.go 等域内别名块）。
func defaultAttachmentHosts() map[string]AttachmentHost {
	return map[string]AttachmentHost{
		AttachmentBizTypeTicket: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.Ticket.Query().Where(ticket.ID(bizID), ticket.TenantID(tenantID)).Exist(ctx)
			},
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				t, err := client.Ticket.Query().Where(ticket.ID(bizID), ticket.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(t.DescriptionHTML, attachmentID), nil
			},
		},
		AttachmentBizTypeTicketComment: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.TicketComment.Query().Where(ticketcomment.ID(bizID), ticketcomment.TenantID(tenantID)).Exist(ctx)
			},
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				c, err := client.TicketComment.Query().Where(ticketcomment.ID(bizID), ticketcomment.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				for _, id := range c.Attachments {
					if id == attachmentID {
						return true, nil
					}
				}
				return false, nil
			},
		},
		AttachmentBizTypeKnowledgeArticle: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.KnowledgeArticle.Query().Where(knowledgearticle.ID(bizID), knowledgearticle.TenantID(tenantID)).Exist(ctx)
			},
			// 知识库正文的 data-attachment-id 保真依赖净化对齐（BE-7）；对齐前该判定可能因
			// 属性被剥离而恒为 false，故 BE-7 落地前不依赖此路径做删除保护。
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				// BE-8：已软删文章不再保护附件（KnowledgeArticle 未纳入全局软删拦截器，需显式过滤），
				// 否则文章删除后其内嵌图片永远无法进入回收序列。
				article, err := client.KnowledgeArticle.Query().
					Where(
						knowledgearticle.ID(bizID),
						knowledgearticle.TenantID(tenantID),
						knowledgearticle.DeletedAtIsNil(),
					).
					Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(article.Content, attachmentID), nil
			},
		},
		AttachmentBizTypeServiceRequest: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.ServiceRequest.Query().Where(servicerequest.ID(bizID), servicerequest.TenantID(tenantID)).Exist(ctx)
			},
			// 服务请求的富文本正文落在 reason（schema 无 description 字段）；无正文引用时删除不被保护。
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				sr, err := client.ServiceRequest.Query().Where(servicerequest.ID(bizID), servicerequest.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(sr.Reason, attachmentID), nil
			},
		},
		AttachmentBizTypeChange: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.Change.Query().Where(changeent.ID(bizID), changeent.TenantID(tenantID)).Exist(ctx)
			},
			// 变更单富文本正文可能落在 description / implementation_plan / rollback_plan；
			// 本轮仅这些字段可能承载 HTML（后续波次扩字段时同步更新），任一字段命中引用即保护。
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				ch, err := client.Change.Query().Where(changeent.ID(bizID), changeent.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(ch.Description, attachmentID) ||
					htmlReferencesAttachment(ch.ImplementationPlan, attachmentID) ||
					htmlReferencesAttachment(ch.RollbackPlan, attachmentID), nil
			},
		},
		AttachmentBizTypeIncident: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.Incident.Query().Where(incidentent.ID(bizID), incidentent.TenantID(tenantID)).Exist(ctx)
			},
			// 事件富文本正文落在 description；本轮仅该字段可能承载 HTML（后续波次扩字段时同步更新）。
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				inc, err := client.Incident.Query().Where(incidentent.ID(bizID), incidentent.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(inc.Description, attachmentID), nil
			},
		},
		AttachmentBizTypeProblem: AttachmentHostFunc{
			ExistsFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID int) (bool, error) {
				return client.Problem.Query().Where(probent.ID(bizID), probent.TenantID(tenantID)).Exist(ctx)
			},
			// 问题富文本正文落在 description；本轮仅该字段可能承载 HTML（后续波次扩字段时同步更新）。
			ReferencesFunc: func(ctx context.Context, client *ent.Client, tenantID, bizID, attachmentID int) (bool, error) {
				pb, err := client.Problem.Query().Where(probent.ID(bizID), probent.TenantID(tenantID)).Only(ctx)
				if err != nil {
					if ent.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return htmlReferencesAttachment(pb.Description, attachmentID), nil
			},
		},
	}
}
