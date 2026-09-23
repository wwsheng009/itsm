package service

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
)

// 回归背景：0 字节附件此前落到 ent 的 file_size Positive() 校验，
// 报 "value out of range" 并在 handler 被统一改写成“附件上传失败，请检查文件类型和大小”；
// 文本类附件则因嗅探结果带 "; charset=utf-8" 参数而误判为类型不允许。
func newAttachmentServiceForTest() *TicketAttachmentService {
	return &TicketAttachmentService{
		logger:       zap.NewNop().Sugar(),
		maxFileSize:  10 * 1024 * 1024,
		allowedTypes: allowedAttachmentMIMEs(),
	}
}

func TestNormalizeMIME(t *testing.T) {
	cases := map[string]string{
		"text/plain; charset=utf-8": "text/plain",
		"text/xml; charset=utf-8":   "text/xml",
		"IMAGE/PNG":                 "image/png",
		"application/pdf":           "application/pdf",
		"":                          "",
	}
	for in, want := range cases {
		if got := normalizeMIME(in); got != want {
			t.Errorf("normalizeMIME(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveMIMEType(t *testing.T) {
	svc := newAttachmentServiceForTest()

	accepted := []struct {
		name     string
		filename string
		detected string
		want     string
	}{
		{"png 图片", "screenshot.png", "image/png", "image/png"},
		{"pdf 文档", "report.pdf", "application/pdf", "application/pdf"},
		{"txt 文本（嗅探结果带 charset）", "key.txt", normalizeMIME("text/plain; charset=utf-8"), "text/plain"},
		{"csv 表格", "export.csv", normalizeMIME("text/plain; charset=utf-8"), "text/csv"},
		{"docx（真实内容是 zip 容器）", "spec.docx", "application/zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"xlsx（真实内容是 zip 容器）", "data.xlsx", "application/zip", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"老式 doc（octet-stream）", "legacy.doc", "application/octet-stream", "application/msword"},
		{"rar 压缩包", "pkg.rar", "application/vnd.rar", "application/vnd.rar"},
		{"7z 压缩包", "pkg.7z", "application/octet-stream", "application/x-7z-compressed"},
		{"svg 矢量图", "icon.svg", "text/xml", "image/svg+xml"},
		{"xml 文档", "config.xml", "text/xml", "application/xml"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.resolveMIMEType(tc.filename, "", tc.detected)
			if err != nil {
				t.Fatalf("resolveMIMEType(%q, %q) 返回错误: %v", tc.filename, tc.detected, err)
			}
			if got != tc.want {
				t.Fatalf("resolveMIMEType(%q, %q) = %q, want %q", tc.filename, tc.detected, got, tc.want)
			}
		})
	}

	rejected := []struct {
		name     string
		filename string
		detected string
	}{
		{"html 伪装成 png", "fake.png", "text/html"},
		{"可执行文件扩展名不在白名单", "setup.exe", "application/octet-stream"},
		{"无扩展名的未知二进制", "blob", "application/octet-stream"},
		{"html 文件", "page.html", "text/html"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.resolveMIMEType(tc.filename, "", tc.detected)
			if err == nil {
				t.Fatalf("resolveMIMEType(%q, %q) = %q，期望被拒绝", tc.filename, tc.detected, got)
			}
			if !errors.Is(err, ErrAttachmentTypeRejected) {
				t.Fatalf("错误类型应为 ErrAttachmentTypeRejected，实际: %v", err)
			}
		})
	}
}

func TestAllowedAttachmentMIMEsCoverExtensionTable(t *testing.T) {
	svc := newAttachmentServiceForTest()
	for ext, canonical := range allowedExtensionMIME {
		if !svc.isAllowedType(canonical) {
			t.Errorf("扩展名 %s 映射的 %s 不在 MIME 白名单中", ext, canonical)
		}
	}
}

func TestUploadAttachmentRejectsEmptyFile(t *testing.T) {
	svc := newAttachmentServiceForTest()
	_, err := svc.UploadAttachment(context.Background(), 1, &FileHeader{Filename: "key.txt", Size: 0}, "", 1, 1)
	if !errors.Is(err, ErrAttachmentEmpty) {
		t.Fatalf("0 字节附件应返回 ErrAttachmentEmpty，实际: %v", err)
	}
}

func TestUploadAttachmentRejectsOversizeFile(t *testing.T) {
	svc := newAttachmentServiceForTest()
	_, err := svc.UploadAttachment(context.Background(), 1, &FileHeader{Filename: "big.pdf", Size: 11 * 1024 * 1024}, "", 1, 1)
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("超限附件应返回 ErrAttachmentTooLarge，实际: %v", err)
	}
}

// 回归背景：附件地址有两种历史形态——数字 ID（前端 API 使用）与存储文件名
// （旧版 fileUrl：/attachments/{ticketID}_{nano}_{name}/download，已随富文本落库）。
// 二者都必须能被下载/预览端点解析，否则编辑器里已插入的图片会 404。
func TestParseAttachmentRef(t *testing.T) {
	cases := []struct {
		name     string
		ref      string
		wantID   int
		wantName string
		wantErr  bool
	}{
		{"数字 ID", "15", 15, "", false},
		{"带空白的数字 ID", " 15 ", 15, "", false},
		{"存储文件名", "5_1790039002028317400_sap.png", 0, "5_1790039002028317400_sap.png", false},
		{"存储文件名带空格", "5_1790038978914200200_sap icon.jpg", 0, "5_1790038978914200200_sap icon.jpg", false},
		{"空引用", "   ", 0, "", true},
		{"含路径分隔符", "../uploads/x.png", 0, "", true},
		{"反斜杠路径", `uploads\x.png`, 0, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, name, err := parseAttachmentRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseAttachmentRef(%q) 期望报错，实际 id=%d name=%q", tc.ref, id, name)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAttachmentRef(%q) 返回错误: %v", tc.ref, err)
			}
			if id != tc.wantID || name != tc.wantName {
				t.Fatalf("parseAttachmentRef(%q) = (%d, %q)，期望 (%d, %q)", tc.ref, id, name, tc.wantID, tc.wantName)
			}
		})
	}
}

// BE-10：域内端点 usage 表单字段的归一化口径。
//
// 缺省（空串 / 纯空白）必须回落 attachment——老客户端与历史 URL 不带该字段，
// 归一化一旦收紧就会把存量上传打成参数错误。
func TestNormalizeTicketAttachmentUsage(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"缺省", "", AttachmentUsageAttachment, false},
		{"空白", "   ", AttachmentUsageAttachment, false},
		{"显式默认", " attachment ", AttachmentUsageAttachment, false},
		{"内嵌图片", "inline_image", AttachmentUsageInlineImage, false},
		{"评论附件", "comment_attachment", AttachmentUsageCommentAttachment, false},
		{"未登记用途", "avatar", "", true},
		{"大小写不符", "Inline_Image", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTicketAttachmentUsage(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrAttachmentUsageInvalid) {
					t.Fatalf("normalizeTicketAttachmentUsage(%q) 期望 ErrAttachmentUsageInvalid，实际: %v", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTicketAttachmentUsage(%q) 返回错误: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeTicketAttachmentUsage(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// BE-10：非法用途在落盘前即被拒绝（不产生孤儿文件、不落任何表）。
func TestUploadAttachmentRejectsUnknownUsage(t *testing.T) {
	svc := newAttachmentServiceForTest()
	_, err := svc.UploadAttachment(context.Background(), 1, &FileHeader{Filename: "key.txt", Size: 1}, "avatar", 1, 1)
	if !errors.Is(err, ErrAttachmentUsageInvalid) {
		t.Fatalf("非法用途应返回 ErrAttachmentUsageInvalid，实际: %v", err)
	}
}

// BE-10：需要通用后端的用途在未接线（generic == nil）时必须显式失败，
// 不能静默退化成默认用途——否则内嵌图片会以普通附件身份落库，用途语义丢失。
func TestUploadAttachmentUsageRequiresGenericBackend(t *testing.T) {
	svc := newAttachmentServiceForTest()
	_, err := svc.UploadAttachment(context.Background(), 1, &FileHeader{Filename: "key.txt", Size: 1}, AttachmentUsageInlineImage, 1, 1)
	if !errors.Is(err, ErrAttachmentUsageBackendMissing) {
		t.Fatalf("未接线通用后端时应返回 ErrAttachmentUsageBackendMissing，实际: %v", err)
	}
}
