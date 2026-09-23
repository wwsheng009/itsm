package service

// 通用附件生命周期与级联清理（BE-8；方案 §5.3 生命周期、§6.1 开关与回滚矩阵）。
//
// 三条不变式：
//  1. 只有「已软删（status=deleted + deleted_at 非空）+ 超过保留期」的通用附件才进入回收
//     候选；active 记录永不回收。
//  2. 回收前复核引用：仍被存活宿主正文（inline_image 的 data-attachment-id）或评论
//     （comment_attachment 的 ticket_comments.attachments）引用的附件跳过，物理文件与元数据
//     都保留 —— 保证「不误删被引用文件」。
//  3. 宿主删除只做级联软删（CascadeHostDeletion），物理文件一律由保留期任务回收：
//     删除可审计、可回滚，回收可先演练（DryRun）再落删。
//
// 上线顺序固定为：开启任务（此时 DryRun 默认开）观察一轮 → 记录演练结果 →
// 显式关闭 dry_run 落删。开关与默认值见 config.AttachmentConfig。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
)

const (
	// AttachmentDefaultRetentionDays 软删保留期默认值（天）：软删后 30 天内仍可人工恢复，
	// 到期后由清理任务回收物理文件。
	AttachmentDefaultRetentionDays = 30
	// AttachmentDefaultCleanupIntervalMinutes 清理任务默认轮询间隔（分钟，6 小时）。
	AttachmentDefaultCleanupIntervalMinutes = 360
	// AttachmentDefaultCleanupBatchSize 单轮单租户默认处理上限（条）。
	AttachmentDefaultCleanupBatchSize = 200
	// attachmentCleanupMaxBatchSize 单轮单租户硬上限（防一次扫描过多记录）。
	attachmentCleanupMaxBatchSize = 1000
)

// 清理动作（AttachmentCleanupItem.Action）。
const (
	// AttachmentCleanupActionPurge 已回收（DryRun 时为「待回收」）。
	AttachmentCleanupActionPurge = "purge"
	// AttachmentCleanupActionSkipReferenced 仍被引用，跳过回收。
	AttachmentCleanupActionSkipReferenced = "skip_referenced"
	// AttachmentCleanupActionFail 处置失败（引用复核/删除文件/删除记录），下轮重试。
	AttachmentCleanupActionFail = "fail"
)

// AttachmentLifecycleCascader 宿主删除级联的注入缝：
// 宿主服务（如 TicketService / KnowledgeService）在删除宿主成功后调用，
// 未注入时宿主删除路径与改造前完全一致（零行为变化）。
type AttachmentLifecycleCascader interface {
	CascadeHostDeletion(ctx context.Context, tenantID int, bizType string, bizID int) (int, error)
}

// AttachmentCleanupOptions 清理入参。
//
// TenantID=0 表示不限租户（后台任务按租户循环时显式传入单租户 ID，便于配额与限流）。
type AttachmentCleanupOptions struct {
	TenantID  int
	Retention time.Duration
	BatchSize int
	DryRun    bool
	Now       time.Time
}

// AttachmentCleanupItem 逐条处置明细；DryRun 时即为「待回收清单」，供演练记录使用。
type AttachmentCleanupItem struct {
	AttachmentID int        `json:"attachmentId"`
	TenantID     int        `json:"tenantId"`
	BizType      string     `json:"bizType"`
	BizID        int        `json:"bizId"`
	Usage        string     `json:"usage"`
	FileSize     int        `json:"fileSize"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
	Action       string     `json:"action"`
	Reason       string     `json:"reason,omitempty"`
}

// AttachmentCleanupResult 单轮清理结果（按租户汇总）。
type AttachmentCleanupResult struct {
	Scanned           int                    `json:"scanned"`
	Purged            int                    `json:"purged"`
	SkippedReferenced int                    `json:"skippedReferenced"`
	Failed            int                    `json:"failed"`
	FreedBytes        int64                  `json:"freedBytes"`
	DryRun            bool                   `json:"dryRun"`
	Items             []AttachmentCleanupItem `json:"items,omitempty"`
}

// Summary 生成单行摘要，供日志与演练记录直接引用。
func (r *AttachmentCleanupResult) Summary() string {
	if r == nil {
		return "scanned=0 purged=0 skipped_referenced=0 failed=0 freed_bytes=0 dry_run=false"
	}
	return fmt.Sprintf("scanned=%d purged=%d skipped_referenced=%d failed=%d freed_bytes=%d dry_run=%t",
		r.Scanned, r.Purged, r.SkippedReferenced, r.Failed, r.FreedBytes, r.DryRun)
}

// CleanupExpired 回收「已软删 + 超过保留期」的附件：先删物理文件，再硬删元数据行。
//
// 语义：
//   - 逐条处置，单条失败不中断整轮（Failed 计数 + 下轮重试）；
//   - 引用复核失败或命中引用一律跳过（宁可漏回收，不可误删）；
//   - 物理文件缺失视为成功（StorageProvider.Delete 幂等）；
//   - DryRun=true 时只扫描与统计，不删文件、不删记录（演练模式）。
func (s *AttachmentService) CleanupExpired(ctx context.Context, opts AttachmentCleanupOptions) (*AttachmentCleanupResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("attachment service is not initialized")
	}
	if opts.TenantID < 0 {
		return nil, fmt.Errorf("tenantID must be >= 0")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	retention := opts.Retention
	if retention <= 0 {
		retention = time.Duration(AttachmentDefaultRetentionDays) * 24 * time.Hour
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = AttachmentDefaultCleanupBatchSize
	}
	if batchSize > attachmentCleanupMaxBatchSize {
		batchSize = attachmentCleanupMaxBatchSize
	}

	cutoff := now.Add(-retention)
	q := s.client.Attachment.Query().Where(
		attachment.StatusEQ(AttachmentStatusDeleted),
		attachment.DeletedAtNotNil(),
		attachment.DeletedAtLTE(cutoff),
	)
	if opts.TenantID > 0 {
		q = q.Where(attachment.TenantID(opts.TenantID))
	}
	rows, err := q.
		Order(ent.Asc(attachment.FieldDeletedAt), ent.Asc(attachment.FieldID)).
		Limit(batchSize).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired attachments: %w", err)
	}

	res := &AttachmentCleanupResult{DryRun: opts.DryRun, Scanned: len(rows)}
	for _, att := range rows {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		item := AttachmentCleanupItem{
			AttachmentID: att.ID,
			TenantID:     att.TenantID,
			BizType:      att.BizType,
			BizID:        att.BizID,
			Usage:        att.Usage,
			FileSize:     att.FileSize,
			DeletedAt:    att.DeletedAt,
		}

		referenced, reason, err := s.attachmentReferenced(ctx, att)
		if err != nil {
			item.Action = AttachmentCleanupActionFail
			item.Reason = "reference_check_failed: " + err.Error()
			res.Failed++
			res.Items = append(res.Items, item)
			s.logger.Warnw("attachment cleanup: reference check failed",
				"attachment_id", att.ID, "tenant_id", att.TenantID, "error", err)
			continue
		}
		if referenced {
			item.Action = AttachmentCleanupActionSkipReferenced
			item.Reason = reason
			res.SkippedReferenced++
			res.Items = append(res.Items, item)
			s.logger.Warnw("attachment cleanup: skipped, still referenced",
				"attachment_id", att.ID, "tenant_id", att.TenantID,
				"biz_type", att.BizType, "biz_id", att.BizID, "usage", att.Usage, "reason", reason)
			continue
		}

		if opts.DryRun {
			item.Action = AttachmentCleanupActionPurge
			item.Reason = "dry_run"
			res.Items = append(res.Items, item)
			continue
		}

		// 先删物理文件再删记录：文件删除失败保留记录，下轮重试（避免「记录没了、文件还在」）。
		if key := strings.TrimSpace(att.FilePath); key != "" {
			if err := s.storage.Delete(ctx, key); err != nil {
				item.Action = AttachmentCleanupActionFail
				item.Reason = "storage_delete_failed: " + err.Error()
				res.Failed++
				res.Items = append(res.Items, item)
				s.logger.Warnw("attachment cleanup: physical file delete failed",
					"attachment_id", att.ID, "tenant_id", att.TenantID, "error", err)
				continue
			}
		}
		if err := s.client.Attachment.DeleteOneID(att.ID).
			Where(attachment.TenantID(att.TenantID)).
			Exec(ctx); err != nil {
			item.Action = AttachmentCleanupActionFail
			item.Reason = "row_delete_failed: " + err.Error()
			res.Failed++
			res.Items = append(res.Items, item)
			s.logger.Warnw("attachment cleanup: metadata delete failed",
				"attachment_id", att.ID, "tenant_id", att.TenantID, "error", err)
			continue
		}

		item.Action = AttachmentCleanupActionPurge
		res.Purged++
		res.FreedBytes += int64(att.FileSize)
		res.Items = append(res.Items, item)
		s.auditAttachment(ctx, att.TenantID, 0, "purge",
			fmt.Sprintf("/api/v1/attachments/%d", att.ID), "DELETE", 200,
			fmt.Sprintf(`{"bizType":%q,"bizId":%d,"usage":%q,"fileSize":%d,"retentionDays":%d}`,
				att.BizType, att.BizID, att.Usage, att.FileSize, int(retention/(24*time.Hour))))
		s.logger.Infow("Attachment purged", "attachment_id", att.ID, "tenant_id", att.TenantID,
			"biz_type", att.BizType, "biz_id", att.BizID, "file_size", att.FileSize)
	}
	return res, nil
}

// CascadeHostDeletion 宿主删除级联策略（由宿主服务在删除成功后调用）：
//
//   - 把宿主下所有 active 附件置为 deleted（软删，保留物理文件，等保留期回收）；
//   - 仍被引用的附件跳过并保持 active（例如工单已删、评论仍引用 comment_attachment），
//     待引用方也删除后才进入回收序列 —— 不误删被引用文件；
//   - 幂等：已 deleted 的记录不再处理；未知 bizType 返回 ErrAttachmentHostNotFound；
//   - 单条失败只告警不返回错误（宿主删除已成功，不因附件级联失败回滚业务）。
//
// 返回实际软删的附件条数。
func (s *AttachmentService) CascadeHostDeletion(ctx context.Context, tenantID int, bizType string, bizID int) (int, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("attachment service is not initialized")
	}
	bizType = strings.TrimSpace(bizType)
	if tenantID <= 0 || bizType == "" || bizID <= 0 {
		return 0, fmt.Errorf("tenant, bizType and bizId are required")
	}
	if _, ok := s.hosts[bizType]; !ok {
		return 0, fmt.Errorf("%w: bizType=%s", ErrAttachmentHostNotFound, bizType)
	}

	rows, err := s.client.Attachment.Query().Where(
		attachment.TenantID(tenantID),
		attachment.BizTypeEQ(bizType),
		attachment.BizID(bizID),
		attachment.StatusEQ(AttachmentStatusActive),
	).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query host attachments: %w", err)
	}

	now := time.Now()
	deleted := 0
	for _, att := range rows {
		referenced, reason, err := s.attachmentReferenced(ctx, att)
		if err != nil {
			s.logger.Warnw("attachment cascade: reference check failed",
				"attachment_id", att.ID, "biz_type", bizType, "biz_id", bizID, "error", err)
			continue
		}
		if referenced {
			s.logger.Warnw("attachment cascade: skipped, still referenced",
				"attachment_id", att.ID, "biz_type", bizType, "biz_id", bizID, "usage", att.Usage, "reason", reason)
			continue
		}
		if _, err := s.client.Attachment.UpdateOneID(att.ID).
			Where(attachment.TenantID(tenantID), attachment.StatusEQ(AttachmentStatusActive)).
			SetStatus(AttachmentStatusDeleted).
			SetDeletedAt(now).
			Save(ctx); err != nil {
			s.logger.Warnw("attachment cascade: soft delete failed",
				"attachment_id", att.ID, "biz_type", bizType, "biz_id", bizID, "error", err)
			continue
		}
		s.auditAttachment(ctx, tenantID, 0, "cascade_delete",
			fmt.Sprintf("/api/v1/attachments/%d", att.ID), "DELETE", 200,
			fmt.Sprintf(`{"bizType":%q,"bizId":%d,"usage":%q,"reason":"host_deleted"}`, att.BizType, att.BizID, att.Usage))
		deleted++
	}
	if deleted > 0 {
		s.logger.Infow("Attachment cascade completed",
			"tenant_id", tenantID, "biz_type", bizType, "biz_id", bizID, "cascaded", deleted)
	}
	return deleted, nil
}

// attachmentReferenced 引用复核（删除保护与回收保护的唯一判定口径）。
//
// 判定「是否仍被存活引用方引用」：
//   - inline_image → 宿主注册表的 References（工单/服务请求由软删拦截器保证已删宿主不可见；
//     知识库文章显式过滤 deleted_at）；
//   - comment_attachment → ticket_comments.attachments 数组；
//   - 其它用途（普通附件）不参与引用保护。
//
// 返回 (是否被引用, 命中的保护原因, 错误)。
func (s *AttachmentService) attachmentReferenced(ctx context.Context, att *ent.Attachment) (bool, string, error) {
	if att == nil {
		return false, "", nil
	}
	switch att.Usage {
	case AttachmentUsageInlineImage:
		host, ok := s.hosts[att.BizType]
		if !ok || host == nil {
			return false, "", nil
		}
		referenced, err := host.References(ctx, s.client, att.TenantID, att.BizID, att.ID)
		if err != nil {
			return false, "", err
		}
		return referenced, "inline_image:" + att.BizType, nil
	case AttachmentUsageCommentAttachment:
		referenced, err := s.ticketCommentReferencesAttachment(ctx, att.TenantID, att)
		if err != nil {
			return false, "", err
		}
		return referenced, "comment_attachment", nil
	}
	return false, "", nil
}
