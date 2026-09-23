package dto

import (
	"time"

	"itsm-backend/ent"
)

// CreateTicketCommentRequest 创建工单评论请求
type CreateTicketCommentRequest struct {
	Content    string `json:"content" binding:"required,min=1,max=5000"`
	IsInternal bool   `json:"isInternal"` // 是否内部备注
	Mentions   []int  `json:"mentions"`   // @的用户ID列表
	// Attachments 通用附件 ID 列表（BE-9「先上传后绑定」）：附件须以
	// biz_type='ticket' + biz_id=工单ID + usage='comment_attachment' 经 A1 上传，
	// 单评论上限 10 个；服务端逐条核验租户/宿主/用途/存活后落库。
	Attachments []int `json:"attachments" binding:"omitempty,max=10"`
}

// UpdateTicketCommentRequest 更新工单评论请求
type UpdateTicketCommentRequest struct {
	Content    string `json:"content" binding:"omitempty,min=1,max=5000"`
	IsInternal *bool  `json:"isInternal"` // 是否内部备注
	Mentions   []int  `json:"mentions"`   // @的用户ID列表
	// Attachments 为 nil 表示不修改；`[]` 表示清空引用（附件转为无主，由 BE-8
	// 清理任务在保留期后回收）；非空数组表示全量替换（单评论上限 10 个）。
	Attachments *[]int `json:"attachments" binding:"omitempty,max=10"`
}

// TicketCommentResponse 工单评论响应
type TicketCommentResponse struct {
	ID          int       `json:"id"`
	TicketID    int       `json:"ticketId"`
	UserID      int       `json:"userId"`
	Content     string    `json:"content"`
	IsInternal  bool      `json:"isInternal"`
	Mentions    []int     `json:"mentions"`
	Attachments []int     `json:"attachments"`
	User        *UserInfo `json:"user,omitempty"` // 评论人信息
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// AttachmentRefs 评论附件的展示元数据（BE-11）。`attachments` 仍是 BE-9 冻结的
	// ID 绑定契约；元数据独立下发，供普通用户（不持有兜底码 attachment:read，
	// 无法经通用 A3/A6 反查）在评论列表直接渲染文件名 / 大小 / 下载地址。
	AttachmentRefs []TicketCommentAttachmentRef `json:"attachmentRefs,omitempty"`
}

// TicketCommentAttachmentRef 评论附件展示元数据（BE-11）。
//
// 服务端只对「同租户 + 宿主 = 本工单 + usage = comment_attachment + 存活」的记录下发，
// 与 BE-9 的绑定校验同口径；已软删 / 跨宿主 / 其它用途的 ID 一律不出现在响应中。
// 下载地址为域内端点，权限由评论接口的 `ticket:read` 静态闸门把守。
type TicketCommentAttachmentRef struct {
	ID       int    `json:"id"`
	FileName string `json:"fileName"`
	FileSize int    `json:"fileSize"`
	MimeType string `json:"mimeType"`
	// DownloadURL 域内下载地址：/api/v1/tickets/:id/attachments/:ref
	DownloadURL string `json:"downloadUrl"`
	// PreviewURL 仅图片附件下发（inline 预览），其余为空串并被省略。
	PreviewURL string `json:"previewUrl,omitempty"`
}

// ListTicketCommentsResponse 工单评论列表响应
type ListTicketCommentsResponse struct {
	Items []*TicketCommentResponse `json:"items"`
	Total int                      `json:"total"`
}

func (r *CreateTicketCommentRequest) Normalize() {
}

func (r *UpdateTicketCommentRequest) Normalize() {
}

// ToTicketCommentResponse 将 Ent 实体转换为 DTO
func ToTicketCommentResponse(comment *ent.TicketComment, user *ent.User) *TicketCommentResponse {
	return ToTicketCommentResponseWithAttachments(comment, user, nil)
}

// ToTicketCommentResponseWithAttachments 在基础响应上追加附件展示元数据（BE-11）。
func ToTicketCommentResponseWithAttachments(comment *ent.TicketComment, user *ent.User, refs []TicketCommentAttachmentRef) *TicketCommentResponse {
	resp := &TicketCommentResponse{
		ID:         comment.ID,
		TicketID:   comment.TicketID,
		UserID:     comment.UserID,
		Content:    comment.Content,
		IsInternal: comment.IsInternal,
		CreatedAt:  comment.CreatedAt,
		UpdatedAt:  comment.UpdatedAt,
	}

	// 设置 mentions
	if comment.Mentions != nil {
		resp.Mentions = comment.Mentions
	}

	// 设置 attachments
	if comment.Attachments != nil {
		resp.Attachments = comment.Attachments
	}

	// 设置附件展示元数据（顺序与 attachments 一致；调用方保证已按宿主口径过滤）
	if len(refs) > 0 {
		resp.AttachmentRefs = refs
	}

	// 设置用户信息
	if user != nil {
		resp.User = &UserInfo{
			ID:         user.ID,
			Username:   user.Username,
			Name:       user.Name,
			Email:      user.Email,
			Role:       string(user.Role),
			Department: user.Department,
			TenantID:   user.TenantID,
		}
	}

	return resp
}
