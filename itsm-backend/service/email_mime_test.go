package service

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== MIME 报文构造测试 ====================

func testEmailConfig() EmailConfig {
	return EmailConfig{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "sender@example.com",
		Password: "password",
		From:     "sender@example.com",
		FromName: "ITSM 通知",
	}
}

// 中文主题必须按 RFC 2047 编码，否则部分客户端会出现乱码。
func TestBuildEmailMessage_EncodesNonASCIISubject(t *testing.T) {
	subject := "工单 TK-1 状态变更"
	msg := &EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  subject,
		Body:     "<p>hello</p>",
		BodyText: "hello",
	}

	raw := buildEmailMessage(msg, testEmailConfig())
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)

	encoded := parsed.Header.Get("Subject")
	assert.NotEqual(t, subject, encoded, "非 ASCII 主题应被编码")
	assert.True(t, strings.HasPrefix(encoded, "=?UTF-8?"), "应使用 RFC 2047 编码字形")

	decoded, err := new(mime.WordDecoder).DecodeHeader(encoded)
	require.NoError(t, err)
	assert.Equal(t, subject, decoded)
}

// 报文必须只使用 CRLF 行结束，不能出现裸 LF。
func TestBuildEmailMessage_UsesCRLFOnly(t *testing.T) {
	msg := &EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "test",
		Body:     "<p>body</p>",
		BodyText: "body",
	}

	raw := string(buildEmailMessage(msg, testEmailConfig()))
	assert.Contains(t, raw, "\r\n")
	assert.NotContains(t, strings.ReplaceAll(raw, "\r\n", ""), "\n", "不应存在裸 LF")
}

// 无附件时应为 multipart/alternative，且 boundary 不可预测。
func TestBuildEmailMessage_NoAttachmentIsAlternative(t *testing.T) {
	msg := &EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "test",
		Body:     "<p>html</p>",
		BodyText: "text",
	}

	raw := buildEmailMessage(msg, testEmailConfig())
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/alternative", mediaType)

	boundary := params["boundary"]
	assert.NotEmpty(t, boundary)
	assert.NotEqual(t, "Mixed_123456789", boundary, "boundary 不应是硬编码固定值")

	reader := multipart.NewReader(parsed.Body, boundary)
	parts := 0
	for {
		_, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		parts++
	}
	assert.Equal(t, 2, parts)
}

// 带附件时外层必须是 multipart/mixed，正文以 multipart/alternative 嵌套，附件独立成部分。
func TestBuildEmailMessage_AttachmentUsesMultipartMixed(t *testing.T) {
	msg := &EmailMessage{
		To:       []string{"user@example.com"},
		CC:       []string{"cc@example.com"},
		Subject:  "报表",
		Body:     "<p>html</p>",
		BodyText: "text",
		Attachments: []EmailAttachment{
			{Filename: "报表.csv", ContentType: "text/csv", Data: []byte("a,b\n1,2\n")},
		},
	}

	raw := buildEmailMessage(msg, testEmailConfig())
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/mixed", mediaType, "附件场景外层应为 multipart/mixed")

	reader := multipart.NewReader(parsed.Body, params["boundary"])
	first, err := reader.NextPart()
	require.NoError(t, err)
	innerType, _, err := mime.ParseMediaType(first.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/alternative", innerType, "正文部分应为 multipart/alternative")

	attachment, err := reader.NextPart()
	require.NoError(t, err)
	disp := attachment.Header.Get("Content-Disposition")
	assert.Contains(t, disp, "attachment")
	assert.Contains(t, disp, "filename", "非 ASCII 文件名应带 filename 参数")

	data, err := io.ReadAll(attachment)
	require.NoError(t, err)
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(data)))
	require.NoError(t, err)
	assert.Contains(t, string(decoded), "a,b", "附件内容应可解析")
}

// 纯文本单部分邮件不应包裹 multipart。
func TestBuildEmailMessage_TextOnlyIsSinglePart(t *testing.T) {
	msg := &EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "test",
		BodyText: "plain only",
	}

	raw := buildEmailMessage(msg, testEmailConfig())
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)

	mediaType, _, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "text/plain", mediaType)
}

// 匿名中继（无用户名）应通过配置校验，但只有密码没有用户名应被拒绝。
func TestValidateConfig_AllowsAnonymousRelay(t *testing.T) {
	svc := NewEmailService(EmailConfig{
		Host: "smtp.example.com",
		Port: 25,
		From: "noreply@example.com",
	}, nil)
	assert.NoError(t, svc.ValidateConfig())

	svcWithPassword := NewEmailService(EmailConfig{
		Host:     "smtp.example.com",
		Port:     25,
		Password: "secret",
		From:     "noreply@example.com",
	}, nil)
	assert.Error(t, svcWithPassword.ValidateConfig())

	svcBadFrom := NewEmailService(EmailConfig{
		Host: "smtp.example.com",
		Port: 25,
		From: "not-an-address",
	}, nil)
	assert.Error(t, svcBadFrom.ValidateConfig())
}
