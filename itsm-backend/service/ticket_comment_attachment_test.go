package service

// ticket_comment_attachment_test.go 覆盖 BE-9 验收口径（方案 §3.2 注 / §5.3）：
// 评论附件「先上传后绑定」——附件先经 A1 以 biz_type='ticket' + usage='comment_attachment'
// 上传，评论创建/更新时按 ID 绑定；服务端只接受「同租户 + 同宿主 + 该用途 + 存活」的通用
// 附件（旧 ticket_attachments 表 ID 与其它 usage 一律拒绝），更新路径支持增删清空，
// 清空/删评论后附件转无主可被 A5 软删，物理文件由 BE-8 清理任务在保留期后回收。
//
// 夹具复用 attachment_service_test.go（newAttachmentTestService / seedHost / createTestTicket）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
	"itsm-backend/ent/ticketcomment"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type commentAttachmentEnv struct {
	t             *testing.T
	client        *ent.Client
	commentSvc    *TicketCommentService
	attachmentSvc *AttachmentService
	tenant        *ent.Tenant
	user          *ent.User
	ticket        *ent.Ticket
	otherTicket   *ent.Ticket
}

func newCommentAttachmentEnv(t *testing.T) *commentAttachmentEnv {
	t.Helper()
	attachmentSvc, client, _ := newAttachmentTestService(t)
	logger := zaptest.NewLogger(t).Sugar()
	suffix := fmt.Sprintf("comment-%d", time.Now().UnixNano())
	tn, user := seedHost(t, client, suffix)
	tk := createTestTicket(t, client, tn, user, "T-CMT-"+suffix)
	other := createTestTicket(t, client, tn, user, "T-CMT-OTHER-"+suffix)
	return &commentAttachmentEnv{
		t:             t,
		client:        client,
		commentSvc:    NewTicketCommentService(client, logger),
		attachmentSvc: attachmentSvc,
		tenant:        tn,
		user:          user,
		ticket:        tk,
		otherTicket:   other,
	}
}

// seedCommentAttachment 直插一条通用附件行（测试不关心上传链路，只关心绑定校验）。
func seedCommentAttachment(t *testing.T, client *ent.Client, tenantID, ticketID, uploaderID int, usage, status string) *ent.Attachment {
	t.Helper()
	row, err := client.Attachment.Create().
		SetTenantID(tenantID).
		SetBizType(AttachmentBizTypeTicket).
		SetBizID(ticketID).
		SetUsage(usage).
		SetFileName("评论配图.png").
		SetFilePath(fmt.Sprintf("%d/ticket/%d/seed-%d.png", tenantID, ticketID, time.Now().UnixNano())).
		SetFileSize(64).
		SetFileType("image/png").
		SetMimeType("image/png").
		SetUploadedBy(uploaderID).
		SetStatus(status).
		Save(context.Background())
	require.NoError(t, err)
	return row
}

// seedLegacyTicketAttachment 直插一条旧 ticket_attachments 行，用于证明旧表 ID 不再被接受。
func seedLegacyTicketAttachment(t *testing.T, client *ent.Client, tenantID, ticketID, uploaderID int) *ent.TicketAttachment {
	t.Helper()
	row, err := client.TicketAttachment.Create().
		SetTicketID(ticketID).
		SetFileName("旧链路附件.txt").
		SetFilePath("uploads/tickets/legacy-comment.txt").
		SetFileSize(16).
		SetFileType("text/plain").
		SetUploadedBy(uploaderID).
		SetTenantID(tenantID).
		Save(context.Background())
	require.NoError(t, err)
	return row
}

func (e *commentAttachmentEnv) createComment(attachments []int) (*dto.TicketCommentResponse, error) {
	return e.commentSvc.CreateTicketComment(context.Background(), e.ticket.ID, &dto.CreateTicketCommentRequest{
		Content:     "带附件的评论",
		Attachments: attachments,
	}, e.user.ID, e.tenant.ID)
}

func (e *commentAttachmentEnv) commentCount() int {
	e.t.Helper()
	count, err := e.client.TicketComment.Query().Where(ticketcomment.TenantID(e.tenant.ID)).Count(context.Background())
	require.NoError(e.t, err)
	return count
}

func (e *commentAttachmentEnv) storedAttachments(commentID int) []int {
	e.t.Helper()
	row, err := e.client.TicketComment.Get(context.Background(), commentID)
	require.NoError(e.t, err)
	return row.Attachments
}

// TestCommentAttachmentCreateBindsActiveCommentAttachments 正例：合法的 comment_attachment
// 按携带顺序去重落库，响应与库内一致。
func TestCommentAttachmentCreateBindsActiveCommentAttachments(t *testing.T) {
	env := newCommentAttachmentEnv(t)
	first := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)
	second := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)

	resp, err := env.createComment([]int{first.ID, second.ID})
	require.NoError(t, err)
	assert.Equal(t, []int{first.ID, second.ID}, resp.Attachments)
	assert.Equal(t, []int{first.ID, second.ID}, env.storedAttachments(resp.ID))

	// 去重：同一附件重复携带只绑定一次，顺序按首次出现。
	deduped, err := env.createComment([]int{second.ID, second.ID, first.ID})
	require.NoError(t, err)
	assert.Equal(t, []int{second.ID, first.ID}, deduped.Attachments)

	// 不带附件与既有口径一致（不写入引用）。
	noAttachments, err := env.createComment(nil)
	require.NoError(t, err)
	assert.Empty(t, noAttachments.Attachments)
	assert.Empty(t, env.storedAttachments(noAttachments.ID))
}

// TestCommentAttachmentCreateRejectsInvalidBinding 反例矩阵：旧表 ID、非评论用途、已软删、
// 跨工单、跨租户、不存在、非法 ID 一律拒绝且不落库；混入非法项时整体拒绝（无部分绑定）。
func TestCommentAttachmentCreateRejectsInvalidBinding(t *testing.T) {
	env := newCommentAttachmentEnv(t)

	valid := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)
	inline := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageInlineImage, AttachmentStatusActive)
	plain := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageAttachment, AttachmentStatusActive)
	deleted := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusDeleted)
	otherTicket := seedCommentAttachment(t, env.client, env.tenant.ID, env.otherTicket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)
	otherTenant, otherUser := seedHost(t, env.client, fmt.Sprintf("comment-other-%d", time.Now().UnixNano()))
	crossTenant := seedCommentAttachment(t, env.client, otherTenant.ID, env.ticket.ID, otherUser.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)

	// 旧表与新表 ID 空间相互独立：挑一条「新表不存在同号记录」的旧表行，确保本用例判定的是
	// 「旧 ticket_attachments ID 不被接受」，而不是「同号新表记录恰好是合法评论附件」。
	legacy := seedLegacyTicketAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID)
	for attempt := 0; attempt < 20; attempt++ {
		collides, qerr := env.client.Attachment.Query().Where(attachment.ID(legacy.ID)).Exist(context.Background())
		require.NoError(t, qerr)
		if !collides {
			break
		}
		legacy = seedLegacyTicketAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID)
	}
	require.False(t, func() bool {
		collides, qerr := env.client.Attachment.Query().Where(attachment.ID(legacy.ID)).Exist(context.Background())
		require.NoError(t, qerr)
		return collides
	}(), "旧表 ID 必须落在新表 ID 空间之外，否则用例不可判定")

	cases := []struct {
		name string
		ids  []int
	}{
		{"旧 ticket_attachments ID", []int{legacy.ID}},
		{"usage=inline_image", []int{inline.ID}},
		{"usage=attachment", []int{plain.ID}},
		{"已软删附件", []int{deleted.ID}},
		{"其它工单的附件", []int{otherTicket.ID}},
		{"跨租户附件", []int{crossTenant.ID}},
		{"不存在的附件", []int{999999}},
		{"非法 ID", []int{-1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.createComment(tc.ids)
			require.Error(t, err)
			assert.Equal(t, 0, env.commentCount(), "校验失败不得落库")
		})
	}

	_, err := env.createComment([]int{valid.ID, inline.ID})
	require.Error(t, err, "混入非法项应整体拒绝")
	assert.Equal(t, 0, env.commentCount())
}

// TestCommentAttachmentCreateEnforcesLimit 单评论附件上限 10（方案 §3.4 边界值）。
func TestCommentAttachmentCreateEnforcesLimit(t *testing.T) {
	env := newCommentAttachmentEnv(t)
	ids := make([]int, 0, maxCommentAttachmentsPerComment+1)
	for i := 0; i < maxCommentAttachmentsPerComment+1; i++ {
		ids = append(ids, seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
			AttachmentUsageCommentAttachment, AttachmentStatusActive).ID)
	}

	_, err := env.createComment(ids)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many comment attachments")
	assert.Equal(t, 0, env.commentCount())

	resp, err := env.createComment(ids[:maxCommentAttachmentsPerComment])
	require.NoError(t, err)
	assert.Len(t, resp.Attachments, maxCommentAttachmentsPerComment)
}

// TestCommentAttachmentUpdateLifecycleAndReferenceProtection 更新路径增删 + 引用保护正反例：
// 被评论引用时 A5 返回 ErrAttachmentInUse（handler 层 → 409/6105）且状态不变；清空引用后
// 附件转无主，A5 软删放行（物理文件由 BE-8 保留期任务回收）。
func TestCommentAttachmentUpdateLifecycleAndReferenceProtection(t *testing.T) {
	env := newCommentAttachmentEnv(t)
	ctx := context.Background()
	att := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)

	resp, err := env.createComment([]int{att.ID})
	require.NoError(t, err)

	// 正例：被评论引用时软删被拒，状态与删除标记均不变。
	err = env.attachmentSvc.Delete(ctx, env.tenant.ID, env.user.ID, att.ID)
	require.ErrorIs(t, err, ErrAttachmentInUse)
	row, err := env.client.Attachment.Get(ctx, att.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusActive, row.Status)
	assert.Nil(t, row.DeletedAt)

	// nil = 不修改附件引用（仅改文案）。
	updated, err := env.commentSvc.UpdateTicketComment(ctx, env.ticket.ID, resp.ID,
		&dto.UpdateTicketCommentRequest{Content: "改后的文案"}, env.user.ID, env.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, []int{att.ID}, updated.Attachments)

	// 全量替换：追加合法附件。
	second := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)
	replace := []int{second.ID}
	updated, err = env.commentSvc.UpdateTicketComment(ctx, env.ticket.ID, resp.ID,
		&dto.UpdateTicketCommentRequest{Attachments: &replace}, env.user.ID, env.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, []int{second.ID}, updated.Attachments)
	assert.Equal(t, []int{second.ID}, env.storedAttachments(resp.ID))

	// 非法替换整体拒绝且不产生部分写入（库内仍是替换前的集合）。
	invalid := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageInlineImage, AttachmentStatusActive)
	mixed := []int{att.ID, invalid.ID}
	_, err = env.commentSvc.UpdateTicketComment(ctx, env.ticket.ID, resp.ID,
		&dto.UpdateTicketCommentRequest{Attachments: &mixed}, env.user.ID, env.tenant.ID)
	require.Error(t, err)
	assert.Equal(t, []int{second.ID}, env.storedAttachments(resp.ID))

	// [] = 清空引用 → 全量替换为空，附件转无主，A5 软删放行。
	empty := []int{}
	updated, err = env.commentSvc.UpdateTicketComment(ctx, env.ticket.ID, resp.ID,
		&dto.UpdateTicketCommentRequest{Attachments: &empty}, env.user.ID, env.tenant.ID)
	require.NoError(t, err)
	assert.Empty(t, updated.Attachments)
	assert.Empty(t, env.storedAttachments(resp.ID))
	require.NoError(t, env.attachmentSvc.Delete(ctx, env.tenant.ID, env.user.ID, second.ID))
	row, err = env.client.Attachment.Get(ctx, second.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusDeleted, row.Status)
	assert.NotNil(t, row.DeletedAt)
}

// TestCommentAttachmentDeleteCommentReleasesReference 评论删除后引用消失，附件可被软删。
func TestCommentAttachmentDeleteCommentReleasesReference(t *testing.T) {
	env := newCommentAttachmentEnv(t)
	ctx := context.Background()
	att := seedCommentAttachment(t, env.client, env.tenant.ID, env.ticket.ID, env.user.ID,
		AttachmentUsageCommentAttachment, AttachmentStatusActive)

	resp, err := env.createComment([]int{att.ID})
	require.NoError(t, err)
	require.ErrorIs(t, env.attachmentSvc.Delete(ctx, env.tenant.ID, env.user.ID, att.ID), ErrAttachmentInUse)

	require.NoError(t, env.commentSvc.DeleteTicketComment(ctx, env.ticket.ID, resp.ID, env.user.ID, env.tenant.ID))
	require.NoError(t, env.attachmentSvc.Delete(ctx, env.tenant.ID, env.user.ID, att.ID))
}
