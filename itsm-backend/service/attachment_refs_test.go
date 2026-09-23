package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
)

func newInlineRefTestClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1",
		strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	return enttest.Open(t, "sqlite3", dsn)
}

func createInlineRefAttachment(t *testing.T, client *ent.Client, tenantID int, bizType string, bizID int, status string) *ent.Attachment {
	t.Helper()
	create := client.Attachment.Create().
		SetTenantID(tenantID).
		SetBizType(bizType).
		SetBizID(bizID).
		SetUsage(AttachmentUsageAttachment).
		SetFileName("图.png").
		SetFilePath(fmt.Sprintf("uploads/tickets/%d_%d_图.png", tenantID, bizID)).
		SetFileType("image/png").
		SetMimeType("image/png").
		SetFileSize(1024).
		SetUploadedBy(1)
	if status != "" {
		create = create.SetStatus(status)
	}
	att, err := create.Save(context.Background())
	require.NoError(t, err)
	return att
}

// a4Img 构造 A4 规范地址的内嵌图片（新链路产物）。
func a4Img(id int) string {
	return fmt.Sprintf(`<img src="/api/v1/attachments/%d/content" data-attachment-id="%d">`, id, id)
}

// TestValidateRichTextInlineRefs_HostScope 覆盖 BE-7 的剥离/保留规则。
func TestValidateRichTextInlineRefs_HostScope(t *testing.T) {
	client := newInlineRefTestClient(t)
	ctx := context.Background()

	mine := createInlineRefAttachment(t, client, 1, AttachmentBizTypeTicket, 100, AttachmentStatusActive)
	other := createInlineRefAttachment(t, client, 1, AttachmentBizTypeTicket, 200, AttachmentStatusActive)
	softDeleted := createInlineRefAttachment(t, client, 1, AttachmentBizTypeTicket, 100, AttachmentStatusDeleted)
	article := createInlineRefAttachment(t, client, 1, AttachmentBizTypeKnowledgeArticle, 7, AttachmentStatusActive)
	foreignTenant := createInlineRefAttachment(t, client, 2, AttachmentBizTypeTicket, 100, AttachmentStatusActive)

	cases := []struct {
		name         string
		bizType      string
		bizID        int
		input        string
		wantKept     bool
		wantReason   string
		wantStripID  int
	}{
		{
			name:     "A4 同宿主保留",
			bizType:  AttachmentBizTypeTicket,
			bizID:    100,
			input:    `<p>说明</p>` + a4Img(mine.ID),
			wantKept: true,
		},
		{
			name:        "A4 跨工单剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       a4Img(other.ID),
			wantReason:  InlineRefReasonNotHosted,
			wantStripID: other.ID,
		},
		{
			name:        "A4 不存在剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       a4Img(999999),
			wantReason:  InlineRefReasonNotFound,
			wantStripID: 999999,
		},
		{
			name:        "A4 地址与引用 ID 不一致剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       fmt.Sprintf(`<img src="/api/v1/attachments/%d/content" data-attachment-id="%d">`, mine.ID, other.ID),
			wantReason:  InlineRefReasonRefMismatch,
			wantStripID: mine.ID,
		},
		{
			name:        "A4 已软删剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       a4Img(softDeleted.ID),
			wantReason:  InlineRefReasonInactive,
			wantStripID: softDeleted.ID,
		},
		{
			name:        "A4 跨租户按不存在剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       a4Img(foreignTenant.ID),
			wantReason:  InlineRefReasonNotFound,
			wantStripID: foreignTenant.ID,
		},
		{
			name:        "旧工单地址跨工单剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       `<img src="/api/v1/tickets/200/attachments/5/preview" data-attachment-id="5">`,
			wantReason:  InlineRefReasonLegacyCrossHost,
			wantStripID: 5,
		},
		{
			name:     "旧工单地址同工单保留",
			bizType:  AttachmentBizTypeTicket,
			bizID:    100,
			input:    `<img src="/api/v1/tickets/100/attachments/5/preview" data-attachment-id="5">`,
			wantKept: true,
		},
		{
			name:     "外链图片保留",
			bizType:  AttachmentBizTypeTicket,
			bizID:    100,
			input:    `<img src="https://cdn.example.com/a.png" alt="远">`,
			wantKept: true,
		},
		{
			name:        "非规范地址指向他人附件剥离",
			bizType:     AttachmentBizTypeTicket,
			bizID:       100,
			input:       fmt.Sprintf(`<img src="/uploads/tickets/legacy.png" data-attachment-id="%d">`, other.ID),
			wantReason:  InlineRefReasonNotHosted,
			wantStripID: other.ID,
		},
		{
			name:     "非规范地址无法解析时保留（灰度期不误伤旧表引用）",
			bizType:  AttachmentBizTypeTicket,
			bizID:    100,
			input:    `<img src="/uploads/tickets/legacy.png" data-attachment-id="424242">`,
			wantKept: true,
		},
		{
			name:        "新建工单剥离他人附件",
			bizType:     AttachmentBizTypeTicket,
			bizID:       0,
			input:       a4Img(mine.ID),
			wantReason:  InlineRefReasonNotHosted,
			wantStripID: mine.ID,
		},
		{
			name:        "新建工单剥离旧工单地址",
			bizType:     AttachmentBizTypeTicket,
			bizID:       0,
			input:       `<img src="/api/v1/tickets/100/attachments/5/preview" data-attachment-id="5">`,
			wantReason:  InlineRefReasonLegacyCrossHost,
			wantStripID: 5,
		},
		{
			name:     "知识库同宿主保留",
			bizType:  AttachmentBizTypeKnowledgeArticle,
			bizID:    7,
			input:    a4Img(article.ID),
			wantKept: true,
		},
		{
			name:        "知识库引用工单附件剥离",
			bizType:     AttachmentBizTypeKnowledgeArticle,
			bizID:       7,
			input:       a4Img(mine.ID),
			wantReason:  InlineRefReasonNotHosted,
			wantStripID: mine.ID,
		},
		{
			name:     "无图片原样返回",
			bizType:  AttachmentBizTypeTicket,
			bizID:    100,
			input:    `<p>纯文本</p>`,
			wantKept: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, violations, err := ValidateRichTextInlineRefs(ctx, client, 1, tc.bizType, tc.bizID, tc.input)
			require.NoError(t, err)

			if tc.wantKept {
				require.Equal(t, tc.input, out, "应当原样保留")
				require.Empty(t, violations)
				return
			}

			require.NotContains(t, strings.ToLower(out), "<img", "违规引用必须整标签剥离")
			require.Len(t, violations, 1)
			require.Equal(t, tc.wantReason, violations[0].Reason)
			require.Equal(t, tc.wantStripID, violations[0].AttachmentID)
		})
	}

	// 混合正文：只剥离越权图片，其余内容与合法图片保持不动。
	mixed := `<p>前言</p>` + a4Img(mine.ID) + a4Img(other.ID) + `<p>后记</p>`
	out, violations, err := ValidateRichTextInlineRefs(ctx, client, 1, AttachmentBizTypeTicket, 100, mixed)
	require.NoError(t, err)
	require.Contains(t, out, "<p>前言</p>")
	require.Contains(t, out, "<p>后记</p>")
	require.Contains(t, out, fmt.Sprintf(`data-attachment-id="%d"`, mine.ID))
	require.NotContains(t, out, fmt.Sprintf(`data-attachment-id="%d"`, other.ID))
	require.Len(t, violations, 1)
	require.Equal(t, other.ID, violations[0].AttachmentID)
}

// TestValidateRichTextInlineRefs_Skips 覆盖跳过路径（不阻塞写入、无客户端不 panic）。
func TestValidateRichTextInlineRefs_Skips(t *testing.T) {
	ctx := context.Background()
	input := a4Img(1)

	out, violations, err := ValidateRichTextInlineRefs(ctx, nil, 1, AttachmentBizTypeTicket, 100, input)
	require.NoError(t, err)
	require.Equal(t, input, out)
	require.Empty(t, violations)

	client := newInlineRefTestClient(t)
	for _, tc := range []struct {
		name      string
		tenantID  int
		bizType   string
		input     string
	}{
		{"空正文", 1, AttachmentBizTypeTicket, ""},
		{"无图片", 1, AttachmentBizTypeTicket, "<p>纯文本</p>"},
		{"租户缺失", 0, AttachmentBizTypeTicket, input},
		{"宿主类型缺失", 1, "", input},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, violations, err := ValidateRichTextInlineRefs(ctx, client, tc.tenantID, tc.bizType, 100, tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.input, out)
			require.Empty(t, violations)
		})
	}
}
