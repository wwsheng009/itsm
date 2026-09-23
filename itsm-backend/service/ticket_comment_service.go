package service

import (
	"context"
	"fmt"
	"strings"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/ticketcomment"
	"itsm-backend/ent/user"

	"go.uber.org/zap"
)

type TicketCommentService struct {
	client              *ent.Client
	logger              *zap.SugaredLogger
	notificationService *TicketNotificationService // 可选的通知服务
}

func NewTicketCommentService(client *ent.Client, logger *zap.SugaredLogger) *TicketCommentService {
	return &TicketCommentService{
		client: client,
		logger: logger,
	}
}

// SetNotificationService 设置通知服务（用于依赖注入）
func (s *TicketCommentService) SetNotificationService(notificationService *TicketNotificationService) {
	s.notificationService = notificationService
}

// CreateTicketComment 创建工单评论
func (s *TicketCommentService) CreateTicketComment(ctx context.Context, ticketID int, req *dto.CreateTicketCommentRequest, userID, tenantID int) (*dto.TicketCommentResponse, error) {
	s.logger.Infow("Creating ticket comment", "ticket_id", ticketID, "user_id", userID)

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
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return nil, fmt.Errorf("comment content is required")
	}
	privileged, err := s.canManageInternalComments(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if req.IsInternal && !privileged {
		return nil, fmt.Errorf("permission denied: internal notes are restricted to support staff")
	}
	if err := s.validateCommentReferences(ctx, ticketID, tenantID, req.Mentions, req.Attachments); err != nil {
		return nil, err
	}

	// 创建评论
	comment, err := s.client.TicketComment.Create().
		SetTicketID(ticketID).
		SetUserID(userID).
		SetContent(req.Content).
		SetIsInternal(req.IsInternal).
		SetTenantID(tenantID).
		SetMentions(req.Mentions).
		SetAttachments(normalizeCommentAttachmentIDs(req.Attachments)).
		Save(ctx)
	if err != nil {
		s.logger.Errorw("Failed to create ticket comment", "error", err)
		return nil, fmt.Errorf("failed to create ticket comment: %w", err)
	}

	// 查询用户信息
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		s.logger.Warnw("Failed to get user", "error", err, "user_id", userID)
		// 用户信息获取失败不影响评论创建
		user = nil
	}

	// 发送评论通知
	if s.notificationService != nil {
		mentionedUserIDs := req.Mentions
		if err := s.notificationService.NotifyTicketCommented(ctx, ticketID, userID, mentionedUserIDs, tenantID); err != nil {
			s.logger.Warnw("Failed to send comment notification", "error", err)
		}
	}

	return dto.ToTicketCommentResponse(comment, user), nil
}

// ListTicketComments 获取工单评论列表
func (s *TicketCommentService) ListTicketComments(ctx context.Context, ticketID, tenantID int, currentUserID int) ([]*dto.TicketCommentResponse, error) {
	s.logger.Infow("Listing ticket comments", "ticket_id", ticketID)

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

	// 查询工单信息，判断当前用户是否有权限查看内部备注
	ticketInfo, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get ticket", "error", err)
		return nil, fmt.Errorf("failed to get ticket: %w", err)
	}

	// 查询评论（按时间倒序）
	comments, err := s.client.TicketComment.Query().
		Where(
			ticketcomment.TicketID(ticketID),
			ticketcomment.TenantID(tenantID),
		).
		Order(ent.Desc(ticketcomment.FieldCreatedAt)).
		WithUser().
		All(ctx)
	if err != nil {
		s.logger.Errorw("Failed to list ticket comments", "error", err)
		return nil, fmt.Errorf("failed to list ticket comments: %w", err)
	}

	// 判断当前用户是否有权限查看内部备注
	// 内部备注只对处理人员和具备支持角色的用户可见，申请人不能因是创建者而查看内部备注。
	privileged, privilegeErr := s.canManageInternalComments(ctx, currentUserID, tenantID)
	if privilegeErr != nil {
		return nil, privilegeErr
	}
	canViewInternal := privileged || (currentUserID == ticketInfo.AssigneeID && ticketInfo.AssigneeID > 0)

	// 转换为 DTO
	responses := make([]*dto.TicketCommentResponse, 0, len(comments))
	for _, comment := range comments {
		// 如果是内部备注，检查权限
		if comment.IsInternal && !canViewInternal {
			continue
		}

		var userEntity *ent.User
		if comment.Edges.User != nil {
			userEntity = comment.Edges.User
		} else {
			// 如果没有加载用户信息，单独查询
			userEntity, _ = s.client.User.Get(ctx, comment.UserID)
		}

		responses = append(responses, dto.ToTicketCommentResponse(comment, userEntity))
	}

	return responses, nil
}

// UpdateTicketComment 更新工单评论
func (s *TicketCommentService) UpdateTicketComment(ctx context.Context, ticketID, commentID int, req *dto.UpdateTicketCommentRequest, userID, tenantID int) (*dto.TicketCommentResponse, error) {
	s.logger.Infow("Updating ticket comment", "ticket_id", ticketID, "comment_id", commentID, "user_id", userID)

	// 查询评论
	comment, err := s.client.TicketComment.Query().
		Where(
			ticketcomment.ID(commentID),
			ticketcomment.TicketID(ticketID),
			ticketcomment.TenantID(tenantID),
		).
		WithUser().
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get ticket comment", "error", err)
		return nil, fmt.Errorf("ticket comment not found: %w", err)
	}

	// 权限检查：只有评论作者或工单处理人可以编辑
	ticketInfo, err := s.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get ticket", "error", err)
		return nil, fmt.Errorf("failed to get ticket: %w", err)
	}

	canEdit := comment.UserID == userID ||
		(ticketInfo.AssigneeID > 0 && ticketInfo.AssigneeID == userID)
	if !canEdit {
		return nil, fmt.Errorf("permission denied: only comment author or ticket assignee can edit")
	}

	// 更新评论
	update := s.client.TicketComment.UpdateOneID(commentID).
		Where(
			ticketcomment.TicketID(ticketID),
			ticketcomment.TenantID(tenantID),
		)
	if req.Content != "" {
		update = update.SetContent(req.Content)
	}
	if req.IsInternal != nil {
		if *req.IsInternal {
			privileged, privilegeErr := s.canManageInternalComments(ctx, userID, tenantID)
			if privilegeErr != nil {
				return nil, privilegeErr
			}
			if !privileged {
				return nil, fmt.Errorf("permission denied: internal notes are restricted to support staff")
			}
		}
		update = update.SetIsInternal(*req.IsInternal)
	}
	if req.Mentions != nil {
		if err := s.validateCommentReferences(ctx, ticketID, tenantID, req.Mentions, nil); err != nil {
			return nil, err
		}
		update = update.SetMentions(req.Mentions)
	}
	// BE-9 更新路径补齐附件增删：nil=不修改，[]=清空引用（附件转无主），非空=全量替换。
	if req.Attachments != nil {
		if err := s.validateCommentAttachments(ctx, ticketID, tenantID, *req.Attachments); err != nil {
			return nil, err
		}
		normalized := normalizeCommentAttachmentIDs(*req.Attachments)
		if normalized == nil {
			normalized = []int{}
		}
		update = update.SetAttachments(normalized)
	}

	updatedComment, err := update.Save(ctx)
	if err != nil {
		s.logger.Errorw("Failed to update ticket comment", "error", err)
		return nil, fmt.Errorf("failed to update ticket comment: %w", err)
	}

	// 重新加载用户信息
	var userEntity *ent.User
	if comment.Edges.User != nil {
		userEntity = comment.Edges.User
	} else {
		userEntity, _ = s.client.User.Get(ctx, updatedComment.UserID)
	}

	return dto.ToTicketCommentResponse(updatedComment, userEntity), nil
}

func (s *TicketCommentService) canManageInternalComments(ctx context.Context, userID, tenantID int) (bool, error) {
	u, err := s.client.User.Query().Where(user.ID(userID), user.TenantID(tenantID), user.Active(true)).Only(ctx)
	if err != nil {
		return false, fmt.Errorf("authenticated user not found")
	}
	switch string(u.Role) {
	case "super_admin", "admin", "manager", "agent", "technician", "security":
		return true, nil
	}
	return false, nil
}

func (s *TicketCommentService) validateCommentReferences(ctx context.Context, ticketID, tenantID int, mentions, attachments []int) error {
	seen := make(map[int]struct{}, len(mentions))
	for _, id := range mentions {
		if id <= 0 {
			return fmt.Errorf("invalid mentioned user")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		exists, err := s.client.User.Query().Where(user.ID(id), user.TenantID(tenantID), user.Active(true)).Exist(ctx)
		if err != nil || !exists {
			return fmt.Errorf("mentioned user %d not found or inactive", id)
		}
	}
	return s.validateCommentAttachments(ctx, ticketID, tenantID, attachments)
}

// maxCommentAttachmentsPerComment 单评论附件上限（方案 §3.4 边界值：评论附件单评论 ≤10 个）。
const maxCommentAttachmentsPerComment = 10

// normalizeCommentAttachmentIDs 去重并保序；空输入返回 nil（与既有「无附件 = NULL」口径一致）。
func normalizeCommentAttachmentIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(ids))
	normalized := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

// validateCommentAttachments 校验评论附件引用（BE-9「先上传后绑定」）。
//
// 只接受经 A1 通用上传、以 biz_type='ticket' + biz_id=ticketID + usage='comment_attachment'
// 落库且仍存活（status='active'）的通用附件；旧 ticket_attachments 表 ID 不再接受
// （前端从未真正接入过该参数，收紧不破坏既有链路）。单次批量 IN 查询，避免 N+1。
func (s *TicketCommentService) validateCommentAttachments(ctx context.Context, ticketID, tenantID int, ids []int) error {
	if len(ids) > maxCommentAttachmentsPerComment {
		return fmt.Errorf("too many comment attachments: %d (max %d)", len(ids), maxCommentAttachmentsPerComment)
	}
	normalized := normalizeCommentAttachmentIDs(ids)
	if len(normalized) == 0 {
		return nil
	}
	for _, id := range normalized {
		if id <= 0 {
			return fmt.Errorf("invalid attachment id")
		}
	}
	rows, err := s.client.Attachment.Query().
		Where(
			attachment.TenantID(tenantID),
			attachment.IDIn(normalized...),
			attachment.BizType(AttachmentBizTypeTicket),
			attachment.BizID(ticketID),
			attachment.Usage(AttachmentUsageCommentAttachment),
			attachment.Status(AttachmentStatusActive),
		).
		All(ctx)
	if err != nil {
		s.logger.Errorw("Failed to validate comment attachments", "error", err, "ticket_id", ticketID)
		return fmt.Errorf("failed to validate comment attachments: %w", err)
	}
	found := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		found[row.ID] = struct{}{}
	}
	for _, id := range normalized {
		if _, ok := found[id]; !ok {
			return fmt.Errorf("attachment %d is not an active comment attachment of this ticket", id)
		}
	}
	return nil
}

// DeleteTicketComment 删除工单评论
func (s *TicketCommentService) DeleteTicketComment(ctx context.Context, ticketID, commentID int, userID, tenantID int) error {
	s.logger.Infow("Deleting ticket comment", "ticket_id", ticketID, "comment_id", commentID, "user_id", userID)

	// 查询评论
	comment, err := s.client.TicketComment.Query().
		Where(
			ticketcomment.ID(commentID),
			ticketcomment.TicketID(ticketID),
			ticketcomment.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		s.logger.Errorw("Failed to get ticket comment", "error", err)
		return fmt.Errorf("ticket comment not found: %w", err)
	}

	// 权限检查：只有评论作者或工单处理人可以删除
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

	canDelete := comment.UserID == userID ||
		(ticketInfo.AssigneeID > 0 && ticketInfo.AssigneeID == userID)
	if !canDelete {
		return fmt.Errorf("permission denied: only comment author or ticket assignee can delete")
	}

	// 删除评论
	err = s.client.TicketComment.DeleteOneID(commentID).
		Where(
			ticketcomment.TicketID(ticketID),
			ticketcomment.TenantID(tenantID),
		).
		Exec(ctx)
	if err != nil {
		s.logger.Errorw("Failed to delete ticket comment", "error", err)
		return fmt.Errorf("failed to delete ticket comment: %w", err)
	}

	return nil
}
