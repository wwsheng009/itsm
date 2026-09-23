package service

// BE-8 验收用例：保留期回收 / 引用保护 / dry-run 演练 / 租户隔离 / 批量上限 /
// 宿主删除级联（软删 → 保留期 → 物理回收全链路）。

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
	"itsm-backend/ent/ticket"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cleanupTestRetention = 24 * time.Hour

// registerTicketSoftDeleteInterceptor 在用例内复刻生产端的 Ticket 软删读透明
// （internal/bootstrap 启动时调用 database.RegisterSoftDeleteInterceptors）：
// 工单软删后宿主引用判定必须看不到它，级联回收才能继续。enttest 建的库默认没有该拦截器。
func registerTicketSoftDeleteInterceptor(client *ent.Client) {
	client.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if q, ok := query.(*ent.TicketQuery); ok {
				q.Where(ticket.DeletedAtIsNil())
			}
			return next.Query(ctx, query)
		})
	}))
}

// softDeleteExpired 绕过服务层直接落库，构造「已软删 + 已过期」的历史记录
// （服务层 Delete 受引用保护，无法直接产生“被引用但仍已软删”的脏数据）。
func softDeleteExpired(t *testing.T, client *ent.Client, id int, ago time.Duration) {
	t.Helper()
	_, err := client.Attachment.UpdateOneID(id).
		SetStatus(AttachmentStatusDeleted).
		SetDeletedAt(time.Now().Add(-ago)).
		Save(context.Background())
	require.NoError(t, err)
}

func TestAttachmentCleanupPurgesExpiredUnreferenced(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "cleanup")
	tk := createTestTicket(t, client, tn, user, "T-2001")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("gone.png", pngMagic),
	})
	require.NoError(t, err)
	require.Equal(t, 1, countFilesUnder(t, root))
	softDeleteExpired(t, client, view.ID, 48*time.Hour)

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention, BatchSize: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Purged)
	assert.Equal(t, 0, res.SkippedReferenced)
	assert.Equal(t, 0, res.Failed)
	assert.False(t, res.DryRun)
	assert.Equal(t, int64(len(pngMagic)), res.FreedBytes)
	assert.Equal(t, 0, countFilesUnder(t, root), "物理文件应被回收")
	_, err = client.Attachment.Get(ctx, view.ID)
	assert.True(t, ent.IsNotFound(err), "元数据行应被硬删")
}

func TestAttachmentCleanupKeepsWithinRetention(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "fresh")
	tk := createTestTicket(t, client, tn, user, "T-2002")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("fresh.png", pngMagic),
	})
	require.NoError(t, err)
	softDeleteExpired(t, client, view.ID, 1*time.Hour) // 软删 1 小时 < 保留期 24 小时

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 0, res.Scanned, "未过保留期的记录不应进入回收候选")
	assert.Equal(t, 0, res.Purged)
	assert.Equal(t, 1, countFilesUnder(t, root), "保留期内文件必须保留（可人工恢复）")

	att, err := client.Attachment.Get(ctx, view.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusDeleted, att.Status)
	require.NotNil(t, att.DeletedAt)
}

func TestAttachmentCleanupSkipsStillReferenced(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "cleanup-ref")
	tk := createTestTicket(t, client, tn, user, "T-2003")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, Usage: AttachmentUsageInlineImage,
		File: pngHeader("inline.png", pngMagic),
	})
	require.NoError(t, err)
	_, err = tk.Update().
		SetDescriptionHTML(fmt.Sprintf(`<img src="/api/v1/attachments/%d/content" data-attachment-id="%d">`, view.ID, view.ID)).
		Save(ctx)
	require.NoError(t, err)
	// 历史脏数据：引用仍在，但记录已被软删且过期 → 回收前必须复核并跳过
	softDeleteExpired(t, client, view.ID, 48*time.Hour)

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 0, res.Purged)
	assert.Equal(t, 1, res.SkippedReferenced)
	require.Len(t, res.Items, 1)
	assert.Equal(t, AttachmentCleanupActionSkipReferenced, res.Items[0].Action)
	assert.Equal(t, 1, countFilesUnder(t, root), "被引用的文件不得回收")
	_, err = client.Attachment.Get(ctx, view.ID)
	require.NoError(t, err, "被引用的记录必须保留")
}

func TestAttachmentCleanupDryRunKeepsEverything(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "dryrun")
	tk := createTestTicket(t, client, tn, user, "T-2004")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("dry.png", pngMagic),
	})
	require.NoError(t, err)
	softDeleteExpired(t, client, view.ID, 48*time.Hour)

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention, DryRun: true})
	require.NoError(t, err)
	assert.True(t, res.DryRun)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 0, res.Purged)
	assert.Equal(t, int64(0), res.FreedBytes)
	require.Len(t, res.Items, 1, "dry-run 必须输出待回收清单（演练记录用）")
	assert.Equal(t, AttachmentCleanupActionPurge, res.Items[0].Action)
	assert.Equal(t, "dry_run", res.Items[0].Reason)
	assert.Equal(t, 1, countFilesUnder(t, root), "dry-run 不删物理文件")
	_, err = client.Attachment.Get(ctx, view.ID)
	require.NoError(t, err, "dry-run 不删元数据行")

	// 落删开关打开后，同一批候选才会被真正回收
	res, err = svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Purged)
	assert.Equal(t, 0, countFilesUnder(t, root))
}

func TestAttachmentCleanupTenantScoped(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn1, user1 := seedHost(t, client, "cleanup-t1")
	tn2, user2 := seedHost(t, client, "cleanup-t2")
	tk1 := createTestTicket(t, client, tn1, user1, "T-2005")
	tk2 := createTestTicket(t, client, tn2, user2, "T-2006")

	v1, err := svc.Upload(ctx, tn1.ID, user1.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk1.ID, File: pngHeader("t1.png", pngMagic)})
	require.NoError(t, err)
	v2, err := svc.Upload(ctx, tn2.ID, user2.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk2.ID, File: pngHeader("t2.png", pngMagic)})
	require.NoError(t, err)
	softDeleteExpired(t, client, v1.ID, 48*time.Hour)
	softDeleteExpired(t, client, v2.ID, 48*time.Hour)

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{TenantID: tn1.ID, Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Purged)

	_, err = client.Attachment.Get(ctx, v1.ID)
	assert.True(t, ent.IsNotFound(err))
	_, err = client.Attachment.Get(ctx, v2.ID)
	require.NoError(t, err, "其它租户的候选记录不得被本租户清理影响")
	assert.Equal(t, 1, countFilesUnder(t, root))
}

func TestAttachmentCleanupToleratesMissingPhysicalFile(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "missing")
	tk := createTestTicket(t, client, tn, user, "T-2007")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("missing.png", pngMagic),
	})
	require.NoError(t, err)
	require.NoError(t, svc.Storage().Delete(ctx, view.StorageKey)) // 文件先被人工/脚本删除
	softDeleteExpired(t, client, view.ID, 48*time.Hour)

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Purged, "文件缺失（Delete 幂等）不应阻塞元数据回收")
	assert.Equal(t, 0, res.Failed)
	_, err = client.Attachment.Get(ctx, view.ID)
	assert.True(t, ent.IsNotFound(err))
	assert.Equal(t, 0, countFilesUnder(t, root))
}

func TestAttachmentCleanupRespectsBatchSize(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "batch")
	tk := createTestTicket(t, client, tn, user, "T-2008")

	ids := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
			BizType: AttachmentBizTypeTicket, BizID: tk.ID,
			File: pngHeader(fmt.Sprintf("batch-%d.png", i), pngMagic),
		})
		require.NoError(t, err)
		softDeleteExpired(t, client, view.ID, 48*time.Hour)
		ids = append(ids, view.ID)
	}
	require.Equal(t, 3, countFilesUnder(t, root))

	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention, BatchSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Scanned, "单轮处理量受 BatchSize 限制")
	assert.Equal(t, 2, res.Purged)
	assert.Equal(t, 1, countFilesUnder(t, root))

	res, err = svc.CleanupExpired(ctx, AttachmentCleanupOptions{Retention: cleanupTestRetention, BatchSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Purged)
	assert.Equal(t, 0, countFilesUnder(t, root))

	rows, err := client.Attachment.Query().Where(attachment.IDIn(ids...)).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, rows)
}

// TestAttachmentCascadeHostDeletionLifecycle 覆盖 BE-8 验收主链路：
// 宿主删除 → 级联软删（跳过仍被引用项）→ 保留期到期 → 物理文件回收。
func TestAttachmentCascadeHostDeletionLifecycle(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	registerTicketSoftDeleteInterceptor(client)
	tn, user := seedHost(t, client, "cascade")
	tk := createTestTicket(t, client, tn, user, "T-2009")

	plain, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("plain.png", pngMagic),
	})
	require.NoError(t, err)
	inline, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, Usage: AttachmentUsageInlineImage,
		File: pngHeader("inline.png", pngMagic),
	})
	require.NoError(t, err)
	_, err = tk.Update().
		SetDescriptionHTML(fmt.Sprintf(`<img data-attachment-id="%d">`, inline.ID)).
		Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, countFilesUnder(t, root))

	// 宿主仍存活（正文还在引用 inline）：级联只软删未被引用的普通附件
	n, err := svc.CascadeHostDeletion(ctx, tn.ID, AttachmentBizTypeTicket, tk.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	plainRow, err := client.Attachment.Get(ctx, plain.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusDeleted, plainRow.Status)
	require.NotNil(t, plainRow.DeletedAt)
	inlineRow, err := client.Attachment.Get(ctx, inline.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusActive, inlineRow.Status, "仍被宿主正文引用的附件不得级联删除")

	// 幂等：重复级联不再改动已软删记录，且引用未解除时仍跳过 inline
	n, err = svc.CascadeHostDeletion(ctx, tn.ID, AttachmentBizTypeTicket, tk.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// 宿主删除（软删）后引用保护随之解除：再次级联应软删 inline
	_, err = tk.Update().SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	n, err = svc.CascadeHostDeletion(ctx, tn.ID, AttachmentBizTypeTicket, tk.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "宿主删除后其内嵌图片应进入级联回收序列")

	// 保留期到期后物理回收
	softDeleteExpired(t, client, plain.ID, 48*time.Hour)
	softDeleteExpired(t, client, inline.ID, 48*time.Hour)
	res, err := svc.CleanupExpired(ctx, AttachmentCleanupOptions{TenantID: tn.ID, Retention: cleanupTestRetention})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Purged)
	assert.Equal(t, 0, countFilesUnder(t, root), "级联软删 + 保留期到期后物理文件应被回收")
}

func TestAttachmentCascadeRejectsUnregisteredHost(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, _ := seedHost(t, client, "cascade-unknown")

	_, err := svc.CascadeHostDeletion(ctx, tn.ID, "unknown_host", 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentHostNotFound))
}

// TestAttachmentGetHidesSoftDeletedMetadata 钉住 BE-8 对 BE-6 遗留观察的收紧：
// A3 元数据读取与 A4 内容读取口径一致，软删记录一律 404。
func TestAttachmentGetHidesSoftDeletedMetadata(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "a3-deleted")
	tk := createTestTicket(t, client, tn, user, "T-2010")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("a3.png", pngMagic),
	})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, tn.ID, user.ID, view.ID))

	_, err = svc.Get(ctx, tn.ID, view.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentNotFound), "软删记录的元数据不可再读（与 GetFile 一致）")
	_, err = svc.GetFile(ctx, tn.ID, view.ID)
	assert.True(t, errors.Is(err, ErrAttachmentNotFound))

	// 记录仍在库中（保留期内可人工恢复），仅对读路径不可见
	row, err := client.Attachment.Get(ctx, view.ID)
	require.NoError(t, err)
	assert.Equal(t, AttachmentStatusDeleted, row.Status)
}
