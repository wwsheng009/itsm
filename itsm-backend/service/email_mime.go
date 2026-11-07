package service

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const mimeCRLF = "\r\n"

// randomBoundary 生成不可预测的 MIME boundary，避免正文/附件内容与固定 boundary 冲突导致解析截断。
func randomBoundary(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

// encodeHeaderWord 按 RFC 2047 编码非 ASCII 头部值，避免中文主题被客户端解析为乱码。
func encodeHeaderWord(value string) string {
	if value == "" {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return mime.QEncoding.Encode("UTF-8", value)
		}
	}
	return value
}

func formatAddress(name, address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return address
	}
	if name != "" {
		parsed.Name = name
	}
	return parsed.String()
}

func formatAddressList(list []string) string {
	parts := make([]string, 0, len(list))
	for _, item := range list {
		if addr := formatAddress("", item); addr != "" {
			parts = append(parts, addr)
		}
	}
	return strings.Join(parts, ", ")
}

func messageIDDomain(from string) string {
	if parsed, err := mail.ParseAddress(strings.TrimSpace(from)); err == nil {
		if idx := strings.LastIndex(parsed.Address, "@"); idx >= 0 && idx+1 < len(parsed.Address) {
			return parsed.Address[idx+1:]
		}
	}
	return "itsm.local"
}

// wrapBase64 按 RFC 2045 要求把 base64 正文折行为 76 列。
func wrapBase64(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	if encoded == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		b.WriteString(encoded[i:end])
		b.WriteString(mimeCRLF)
	}
	return b.String()
}

func attachmentFilenameParams(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "attachment"
	}
	ascii := true
	for _, r := range filename {
		if r < 0x20 || r > 0x7e {
			ascii = false
			break
		}
	}
	quoted := strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(filename)
	if ascii {
		return "filename=\"" + quoted + "\""
	}
	return "filename=\"" + quoted + "\"; filename*=UTF-8''" + url.PathEscape(filename)
}

// buildContentPart 构造正文部分：文本+HTML 用 multipart/alternative 承载，单一格式时直接作为叶子部分。
func buildContentPart(msg *EmailMessage) (contentType string, body []byte) {
	hasText := strings.TrimSpace(msg.BodyText) != ""
	hasHTML := strings.TrimSpace(msg.Body) != ""

	if hasText && hasHTML {
		boundary := randomBoundary("alt")
		var b strings.Builder
		b.WriteString("--" + boundary + mimeCRLF)
		b.WriteString("Content-Type: text/plain; charset=UTF-8" + mimeCRLF)
		b.WriteString("Content-Transfer-Encoding: base64" + mimeCRLF + mimeCRLF)
		b.WriteString(wrapBase64([]byte(msg.BodyText)))
		b.WriteString("--" + boundary + mimeCRLF)
		b.WriteString("Content-Type: text/html; charset=UTF-8" + mimeCRLF)
		b.WriteString("Content-Transfer-Encoding: base64" + mimeCRLF + mimeCRLF)
		b.WriteString(wrapBase64([]byte(msg.Body)))
		b.WriteString("--" + boundary + "--" + mimeCRLF)
		return "multipart/alternative; boundary=\"" + boundary + "\"", []byte(b.String())
	}

	partType := "text/plain; charset=UTF-8"
	content := msg.BodyText
	if !hasText && hasHTML {
		partType = "text/html; charset=UTF-8"
		content = msg.Body
	}
	var b strings.Builder
	b.WriteString("Content-Type: " + partType + mimeCRLF)
	b.WriteString("Content-Transfer-Encoding: base64" + mimeCRLF + mimeCRLF)
	b.WriteString(wrapBase64([]byte(content)))
	return partType, []byte(b.String())
}

// buildEmailMessage 组装完整 MIME 报文。
// 关键点：统一 CRLF 行结束；boundary 随机；附件场景使用 multipart/mixed 包裹 multipart/alternative。
func buildEmailMessage(msg *EmailMessage, cfg EmailConfig) []byte {
	contentType, contentBody := buildContentPart(msg)

	var b strings.Builder
	b.WriteString("From: " + formatAddress(cfg.FromName, cfg.From) + mimeCRLF)
	b.WriteString("To: " + formatAddressList(msg.To) + mimeCRLF)
	if len(msg.CC) > 0 {
		b.WriteString("Cc: " + formatAddressList(msg.CC) + mimeCRLF)
	}
	b.WriteString("Subject: " + encodeHeaderWord(msg.Subject) + mimeCRLF)
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + mimeCRLF)
	b.WriteString("Message-ID: <" + randomBoundary("itsm") + "@" + messageIDDomain(cfg.From) + ">" + mimeCRLF)
	b.WriteString("MIME-Version: 1.0" + mimeCRLF)

	if len(msg.Attachments) == 0 {
		b.WriteString("Content-Type: " + contentType + mimeCRLF + mimeCRLF)
		b.WriteString(string(contentBody))
		return []byte(b.String())
	}

	boundary := randomBoundary("mix")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"" + mimeCRLF + mimeCRLF)
	b.WriteString("--" + boundary + mimeCRLF)
	b.WriteString("Content-Type: " + contentType + mimeCRLF + mimeCRLF)
	b.WriteString(string(contentBody))
	for _, att := range msg.Attachments {
		attType := att.ContentType
		if strings.TrimSpace(attType) == "" {
			attType = "application/octet-stream"
		}
		b.WriteString("--" + boundary + mimeCRLF)
		b.WriteString("Content-Type: " + attType + mimeCRLF)
		b.WriteString("Content-Transfer-Encoding: base64" + mimeCRLF)
		b.WriteString("Content-Disposition: attachment; " + attachmentFilenameParams(att.Filename) + mimeCRLF + mimeCRLF)
		b.WriteString(wrapBase64(att.Data))
	}
	b.WriteString("--" + boundary + "--" + mimeCRLF)
	return []byte(b.String())
}

// sendMailWithTimeout 以显式拨号超时发送邮件，支持 STARTTLS 与可选认证，避免 SMTP 不可达时长时间阻塞。
func sendMailWithTimeout(ctx context.Context, cfg EmailConfig, auth smtp.Auth, from string, to []string, message []byte) error {
	host := cfg.Host
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadlineFromContext(ctx, 30*time.Second))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client failed: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls failed: %w", err)
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL FROM failed: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT TO failed: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA failed: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		return fmt.Errorf("smtp write failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close failed: %w", err)
	}
	return client.Quit()
}

// deadlineFromContext 结合调用方 context 截止时间与兜底时限。
func deadlineFromContext(ctx context.Context, fallback time.Duration) time.Time {
	deadline := time.Now().Add(fallback)
	if ctx != nil {
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			return d
		}
	}
	return deadline
}
