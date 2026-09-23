package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// 说明：本套用例覆盖 BE-3 验收口径——跨租户 404、宿主不存在、幂等重试、超限，
// 以及加固继承四项（magic bytes 嗅探 / 病毒扫描失败即删 / 写后大小复核 / 文件名清洗）。
// 注：`uq_attachments_client_token` 唯一索引由迁移 DDL 建立（ent schema 刻意不声明索引），
// enttest 建的库不含该索引，故并发唯一冲突路径由 Postgres 集成测试覆盖，此处覆盖串行幂等。
//
// 宿主行需满足外键：ticket.requester_id → users、users.tenant_id → tenants、
// ticket_comments.ticket_id/user_id → tickets/users，故统一用 seedHost 造数。

var pngMagic = append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("attachment-test-payload")...)

type stubVirusScanner struct{ err error }

func (s stubVirusScanner) Scan(context.Context, string) error { return s.err }

func newAttachmentTestService(t *testing.T) (*AttachmentService, *ent.Client, string) {
	t.Helper()
	dsn := fmt.Sprintf("file:attachment_test_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	root := t.TempDir()
	svc := NewAttachmentService(client, zaptest.NewLogger(t).Sugar(), NewLocalStorageProvider(root))
	return svc, client, root
}

// seedHost 创建一对满足外键的租户 + 用户。
func seedHost(t *testing.T, client *ent.Client, suffix string) (*ent.Tenant, *ent.User) {
	t.Helper()
	ctx := context.Background()
	tn, err := client.Tenant.Create().
		SetName("附件测试租户" + suffix).
		SetCode("attach-" + suffix).
		SetDomain("attach-" + suffix + ".test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	u, err := client.User.Create().
		SetUsername("attach-user-" + suffix).
		SetEmail("attach-" + suffix + "@example.com").
		SetName("附件测试用户").
		SetPasswordHash("hashed-for-test").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)
	return tn, u
}

func createTestTicket(t *testing.T, client *ent.Client, tn *ent.Tenant, requester *ent.User, number string) *ent.Ticket {
	t.Helper()
	tk, err := client.Ticket.Create().
		SetTitle("附件测试工单").
		SetTicketNumber(number).
		SetRequesterID(requester.ID).
		SetTenantID(tn.ID).
		Save(context.Background())
	require.NoError(t, err)
	return tk
}

func countFilesUnder(t *testing.T, root string) int {
	t.Helper()
	count := 0
	require.NoError(t, filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	}))
	return count
}

func pngHeader(name string, content []byte) *FileHeader {
	return &FileHeader{Filename: name, Size: int64(len(content)), Reader: bytes.NewReader(content)}
}

func TestAttachmentUploadStoresFileAndRecord(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "store")
	tk := createTestTicket(t, client, tn, user, "T-1001")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		Usage:   AttachmentUsageInlineImage,
		File:    pngHeader("内嵌 图片.png", pngMagic),
	})
	require.NoError(t, err)

	assert.Equal(t, tn.ID, view.TenantID)
	assert.Equal(t, AttachmentBizTypeTicket, view.BizType)
	assert.Equal(t, tk.ID, view.BizID)
	assert.Equal(t, AttachmentUsageInlineImage, view.Usage)
	assert.Equal(t, "内嵌 图片.png", view.FileName)
	assert.Equal(t, len(pngMagic), view.FileSize)
	assert.Equal(t, "image/png", view.FileType)
	assert.Equal(t, "image/png", view.MimeType)
	assert.Equal(t, user.ID, view.UploadedBy)
	assert.Equal(t, AttachmentStatusActive, view.Status)
	assert.NotEmpty(t, view.SHA256)
	assert.Equal(t, fmt.Sprintf("/api/v1/attachments/%d/content", view.ID), view.FileURL)

	// 存储 key 规则 `{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}` + 文件真实落盘
	att, err := client.Attachment.Get(ctx, view.ID)
	require.NoError(t, err)
	assert.True(t, filepath.IsLocal(filepath.FromSlash(att.FilePath)))
	assert.Equal(t, fmt.Sprintf("%d/%s/%d", tn.ID, AttachmentBizTypeTicket, tk.ID), filepath.ToSlash(filepath.Dir(att.FilePath)))
	assert.Equal(t, ".png", filepath.Ext(att.FilePath))
	assert.Equal(t, 1, countFilesUnder(t, root))
	onDisk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(att.FilePath)))
	require.NoError(t, err)
	assert.Equal(t, pngMagic, onDisk)

	// 审计已写
	logs, err := client.AuditLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "attachment", logs[0].Resource)
	assert.Equal(t, "upload", logs[0].Action)
}

func TestAttachmentUploadRejectsCrossTenantHost(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn7, user7 := seedHost(t, client, "cross-a")
	tn8, user8 := seedHost(t, client, "cross-b")
	tk := createTestTicket(t, client, tn7, user7, "T-1002")

	_, err := svc.Upload(ctx, tn8.ID, user8.ID, AttachmentUploadInput{ // 用租户 B 去挂租户 A 的工单
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    pngHeader("a.png", pngMagic),
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentHostNotFound), "跨租户宿主必须按不存在处理（404/6101）")
	assert.Equal(t, 0, countFilesUnder(t, root), "校验失败不得留下孤儿文件")
}

func TestAttachmentUploadRejectsMissingHostAndUnsupportedBizType(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "missing")

	_, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: 999999, File: pngHeader("a.png", pngMagic)})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentHostNotFound))

	_, err = svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: "unknown_domain", BizID: 1, File: pngHeader("a.png", pngMagic)})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentHostNotFound))
}

func TestAttachmentUploadRejectsOversizeAndEmpty(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "size")
	tk := createTestTicket(t, client, tn, user, "T-1003")

	big := &FileHeader{Filename: "big.bin", Size: svc.maxFileSize + 1, Reader: bytes.NewReader([]byte("x"))}
	_, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: big})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentTooLarge))

	_, err = svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: &FileHeader{Filename: "empty.png", Size: 0, Reader: bytes.NewReader(nil)}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentEmpty))
	assert.Equal(t, 0, countFilesUnder(t, root))
	assert.Equal(t, 10, svc.MaxFileSizeMB())
}

func TestAttachmentUploadRejectsDisguisedContentByMagicBytes(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "magic")
	tk := createTestTicket(t, client, tn, user, "T-1004")

	// 扩展名与 Content-Type 都伪装成 PNG，真实内容是 HTML → 必须被内容嗅探拒绝
	html := []byte("<html><body><script>alert(1)</script></body></html>")
	_, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    &FileHeader{Filename: "evil.png", ContentType: "image/png", Size: int64(len(html)), Reader: bytes.NewReader(html)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentTypeRejected), "应基于内容嗅探拒绝伪装文件")
	assert.Equal(t, 0, countFilesUnder(t, root))
}

func TestAttachmentUploadDetectsDeclaredSizeMismatch(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "mismatch")
	tk := createTestTicket(t, client, tn, user, "T-1005")

	// 声明 4096 字节，实际只给 8 字节 PNG → 写后复核必须发现并删除半成品
	_, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    &FileHeader{Filename: "short.png", Size: 4096, Reader: bytes.NewReader(pngMagic)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentSizeMismatch))
	assert.Equal(t, 0, countFilesUnder(t, root), "大小不符必须删除已写文件")
	rows, qerr := client.Attachment.Query().Count(ctx)
	require.NoError(t, qerr)
	assert.Equal(t, 0, rows)
}

func TestAttachmentUploadVirusScannerFailureRemovesFile(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "virus")
	tk := createTestTicket(t, client, tn, user, "T-1006")
	svc.SetVirusScanner(stubVirusScanner{err: errors.New("eicar detected")})

	_, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("virus.png", pngMagic)})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentRejected))
	assert.Equal(t, 0, countFilesUnder(t, root), "扫描失败必须删除已保存文件")
	rows, qerr := client.Attachment.Query().Count(ctx)
	require.NoError(t, qerr)
	assert.Equal(t, 0, rows)
}

func TestAttachmentUploadIdempotentClientToken(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "idem")
	tk := createTestTicket(t, client, tn, user, "T-1007")
	in := AttachmentUploadInput{
		BizType:     AttachmentBizTypeTicket,
		BizID:       tk.ID,
		Usage:       AttachmentUsageCommentAttachment,
		ClientToken: "comment-abc-1",
		File:        pngHeader("retry.png", pngMagic),
	}

	first, err := svc.Upload(ctx, tn.ID, user.ID, in)
	require.NoError(t, err)
	second, err := svc.Upload(ctx, tn.ID, user.ID, in)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "重复 clientToken 必须复用既有记录")
	rows, qerr := client.Attachment.Query().Count(ctx)
	require.NoError(t, qerr)
	assert.Equal(t, 1, rows)
	assert.Equal(t, 1, countFilesUnder(t, root), "重试不得产生第二个文件")
}

func TestAttachmentUploadSanitizesFilename(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "name")
	tk := createTestTicket(t, client, tn, user, "T-1008")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    pngHeader(`..\..\evil<>;|".png`, pngMagic),
	})
	require.NoError(t, err)
	assert.NotContains(t, view.FileName, "/")
	assert.NotContains(t, view.FileName, `\`)
	for _, ch := range []string{"<", ">", ";", "|", `"`, "*", "?"} {
		assert.NotContains(t, view.FileName, ch)
	}

	// 清洗后为空 → 明确拒绝（而非落一个无名文件）
	_, err = svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    &FileHeader{Filename: "..", Size: int64(len(pngMagic)), Reader: bytes.NewReader(pngMagic)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentInvalidFilename))
}

func TestAttachmentDeleteBlockedWhileCommentReferences(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "cmt")
	tk := createTestTicket(t, client, tn, user, "T-1009")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		Usage:   AttachmentUsageCommentAttachment,
		File:    pngHeader("c.png", pngMagic),
	})
	require.NoError(t, err)
	comment, err := client.TicketComment.Create().
		SetTicketID(tk.ID).SetUserID(user.ID).SetContent("带附件评论").SetTenantID(tn.ID).
		SetAttachments([]int{view.ID}).
		Save(ctx)
	require.NoError(t, err)

	err = svc.Delete(ctx, tn.ID, user.ID, view.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentInUse), "被评论引用必须 409/6105 且不改状态")
	att, gerr := client.Attachment.Get(ctx, view.ID)
	require.NoError(t, gerr)
	assert.Equal(t, AttachmentStatusActive, att.Status)
	assert.Nil(t, att.DeletedAt)

	// 移除引用后可软删；重复删除幂等
	_, err = comment.Update().SetAttachments([]int{}).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, tn.ID, user.ID, view.ID))
	att, gerr = client.Attachment.Get(ctx, view.ID)
	require.NoError(t, gerr)
	assert.Equal(t, AttachmentStatusDeleted, att.Status)
	require.NotNil(t, att.DeletedAt)
	require.NoError(t, svc.Delete(ctx, tn.ID, user.ID, view.ID))
}

func TestAttachmentDeleteBlockedWhileInlineImageReferenced(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "inline")
	tk := createTestTicket(t, client, tn, user, "T-1010")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		Usage:   AttachmentUsageInlineImage,
		File:    pngHeader("inline.png", pngMagic),
	})
	require.NoError(t, err)
	_, err = tk.Update().SetDescriptionHTML(fmt.Sprintf(`<p>图</p><img src="/api/v1/attachments/%d/content" data-attachment-id="%d">`, view.ID, view.ID)).Save(ctx)
	require.NoError(t, err)

	err = svc.Delete(ctx, tn.ID, user.ID, view.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentInUse))

	_, err = tk.Update().SetDescriptionHTML("<p>已移除图片</p>").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, tn.ID, user.ID, view.ID))
}

func TestHTMLReferencesAttachment(t *testing.T) {
	cases := []struct {
		name string
		html string
		id   int
		want bool
	}{
		{"双引号", `<img data-attachment-id="12">`, 12, true},
		{"单引号", `<img data-attachment-id='12'>`, 12, true},
		{"无引号", `<img data-attachment-id=12>`, 12, true},
		{"数字边界", `<img data-attachment-id="123">`, 12, false},
		{"无引号数字边界", `<img data-attachment-id=123>`, 12, false},
		{"不匹配", `<img data-attachment-id="13">`, 12, false},
		{"空 HTML", ``, 12, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, htmlReferencesAttachment(tc.html, tc.id))
		})
	}
}

func TestAttachmentGetListAndBatchAreTenantScoped(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn7, user7 := seedHost(t, client, "scope-a")
	tn8, user8 := seedHost(t, client, "scope-b")
	tk7 := createTestTicket(t, client, tn7, user7, "T-1011")
	tk8 := createTestTicket(t, client, tn8, user8, "T-1012")

	v7, err := svc.Upload(ctx, tn7.ID, user7.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk7.ID, File: pngHeader("a.png", pngMagic)})
	require.NoError(t, err)
	v8, err := svc.Upload(ctx, tn8.ID, user8.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk8.ID, File: pngHeader("b.png", pngMagic)})
	require.NoError(t, err)

	// 跨租户读取 → 404
	_, err = svc.Get(ctx, tn8.ID, v7.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentNotFound))

	// 列表按宿主 + 租户隔离
	list7, total7, err := svc.List(ctx, tn7.ID, AttachmentBizTypeTicket, tk7.ID, "", 0, 0)
	require.NoError(t, err)
	require.Len(t, list7, 1)
	assert.Equal(t, 1, total7)
	assert.Equal(t, v7.ID, list7[0].ID)

	// 跨租户访问他人宿主 → 404（不泄露宿主存在性）
	_, _, err = svc.List(ctx, tn8.ID, AttachmentBizTypeTicket, tk7.ID, "", 0, 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentHostNotFound))

	// 批量回填：租户过滤 + 去重 + 上限
	rows, err := svc.BatchGet(ctx, tn8.ID, []int{v7.ID, v8.ID, v8.ID, 0})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, v8.ID, rows[0].ID)

	tooMany := make([]int, AttachmentMaxBatchIDs+1)
	_, err = svc.BatchGet(ctx, tn8.ID, tooMany)
	require.Error(t, err)
}

func TestAttachmentGetFileRejectsDeletedAndStreamsContent(t *testing.T) {
	svc, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "stream")
	tk := createTestTicket(t, client, tn, user, "T-1013")

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("d.png", pngMagic)})
	require.NoError(t, err)

	stream, err := svc.GetFile(ctx, tn.ID, view.ID)
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Reader.Close()
	assert.Equal(t, "d.png", stream.FileName)
	assert.Equal(t, "image/png", stream.MimeType)
	assert.Equal(t, int64(len(pngMagic)), stream.Size)
	got := make([]byte, len(pngMagic))
	n, rerr := io.ReadFull(stream.Reader, got)
	require.NoError(t, rerr)
	assert.Equal(t, len(pngMagic), n)
	assert.Equal(t, pngMagic, got)

	require.NoError(t, svc.Delete(ctx, tn.ID, user.ID, view.ID))
	_, err = svc.GetFile(ctx, tn.ID, view.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentNotFound), "软删记录不可再读取")
}

func TestLocalStorageProviderKeyRules(t *testing.T) {
	root := t.TempDir()
	p := NewLocalStorageProvider(root)
	ctx := context.Background()

	// 新 key：相对 root 拼接
	got, err := p.LocalPath("7/ticket/12/abc.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, filepath.FromSlash("7/ticket/12/abc.png")), got)

	// 历史值（P2 回填）：`uploads/tickets/...` 按原样读取，不再拼 root
	legacy, err := p.LocalPath("uploads/tickets/12_1700000000_a.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("uploads/tickets/12_1700000000_a.png"), legacy)

	// 防御：相对穿越与绝对路径一律拒绝
	_, err = p.LocalPath("../etc/passwd")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentStorage))
	_, err = p.LocalPath("/etc/passwd")
	require.Error(t, err)

	// Delete 幂等：目标不存在视为成功
	require.NoError(t, p.Delete(ctx, "7/ticket/12/not-exist.png"))
}

func TestAttachmentUploadUsesStorageProvider(t *testing.T) {
	// 注入自定义存储实现，验证服务层不直接触碰文件系统（存储抽象达成）。
	_, client, _ := newAttachmentTestService(t)
	ctx := context.Background()
	tn, user := seedHost(t, client, "storage")
	tk := createTestTicket(t, client, tn, user, "T-1014")
	rec := &recordingStorage{}
	svc := NewAttachmentService(client, zaptest.NewLogger(t).Sugar(), rec)

	view, err := svc.Upload(ctx, tn.ID, user.ID, AttachmentUploadInput{BizType: AttachmentBizTypeTicket, BizID: tk.ID, File: pngHeader("s.png", pngMagic)})
	require.NoError(t, err)
	assert.Equal(t, 1, rec.saves)
	assert.Equal(t, fmt.Sprintf("%d/%s/%d", tn.ID, AttachmentBizTypeTicket, tk.ID), filepath.ToSlash(filepath.Dir(rec.lastKey)))
	assert.Equal(t, len(pngMagic), view.FileSize)
}

type recordingStorage struct {
	saves   int
	deletes int
	lastKey string
	data    map[string][]byte
}

func (r *recordingStorage) Save(_ context.Context, key string, reader io.Reader, maxBytes int64) (int64, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return 0, err
	}
	if int64(len(body)) > maxBytes {
		return 0, fmt.Errorf("%w: too large", ErrAttachmentTooLarge)
	}
	if r.data == nil {
		r.data = map[string][]byte{}
	}
	r.data[key] = body
	r.saves++
	r.lastKey = key
	return int64(len(body)), nil
}

func (r *recordingStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := r.data[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAttachmentNotFound, key)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (r *recordingStorage) Delete(_ context.Context, key string) error {
	delete(r.data, key)
	r.deletes++
	return nil
}

func (r *recordingStorage) Stat(_ context.Context, key string) (int64, error) {
	body, ok := r.data[key]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrAttachmentNotFound, key)
	}
	return int64(len(body)), nil
}
