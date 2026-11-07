package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EmailConfig 邮件配置
type EmailConfig struct {
	Host     string // SMTP服务器地址
	Port     int    // SMTP端口
	Username string // 用户名
	Password string // 密码
	From     string // 发件人地址
	FromName string // 发件人名称
	// SSL 隐式 TLS（SMTPS，典型端口 465）；false 时使用 STARTTLS/明文（由服务器能力决定）。
	SSL bool
}

// EmailService 邮件服务
type EmailService struct {
	config EmailConfig
	logger *zap.SugaredLogger
	mu     sync.Mutex
	recent map[string][]time.Time
}

// EmailMessage 邮件消息
type EmailMessage struct {
	To          []string          // 收件人列表
	CC          []string          // 抄送人列表
	Subject     string            // 邮件主题
	Body        string            // 邮件正文（HTML）
	BodyText    string            // 邮件正文（纯文本）
	Attachments []EmailAttachment // 附件
}

// EmailAttachment 邮件附件
type EmailAttachment struct {
	Filename    string // 文件名
	ContentType string // 内容类型
	Data        []byte // 文件内容
}

// NewEmailService 创建邮件服务
func NewEmailService(config EmailConfig, logger *zap.SugaredLogger) *EmailService {
	return &EmailService{
		config: config,
		logger: logger,
		recent: make(map[string][]time.Time),
	}
}

// Send 发送邮件
func (s *EmailService) Send(ctx context.Context, msg *EmailMessage) error {
	if err := s.validateMessage(msg); err != nil {
		return err
	}
	if err := s.checkRateLimit(msg.To, 20, time.Minute); err != nil {
		return err
	}
	s.logger.Infow(
		"Sending email",
		"to", msg.To,
		"subject", msg.Subject,
	)

	payload := buildEmailMessage(msg, s.config)

	// 发送邮件（匿名中继允许 Username 为空）
	var auth smtp.Auth
	if s.config.Username != "" {
		auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	}
	addr := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	recipients := append(append([]string{}, msg.To...), msg.CC...)

	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if s.config.SSL && s.config.Port == 465 {
			// 隐式 TLS（SMTPS）：net/smtp.SendMail 不支持 465 先握手后协商，需手动建连。
			err = sendMailImplicitTLS(addr, s.config.Host, auth, s.config.From, recipients, payload)
		} else {
			err = sendMailWithTimeout(ctx, s.config, auth, s.config.From, recipients, payload)
		}
		if err == nil {
			break
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
			}
		}
	}
	if err != nil {
		s.logger.Errorw("Failed to send email", "error", err, "to", msg.To)
		return fmt.Errorf("failed to send email: %w", err)
	}

	s.logger.Infow("Email sent successfully", "to", msg.To, "subject", msg.Subject)
	return nil
}

// sendMailImplicitTLS 以隐式 TLS（SMTPS，端口 465）发送邮件。
func sendMailImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("smtps dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtps client failed: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtps auth failed: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtps MAIL FROM failed: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtps RCPT TO failed: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtps DATA failed: %w", err)
	}
	if _, err := writer.Write(msg); err != nil {
		return fmt.Errorf("smtps write failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtps close failed: %w", err)
	}
	return client.Quit()
}

func (s *EmailService) validateMessage(msg *EmailMessage) error {
	if msg == nil || len(msg.To) == 0 {
		return fmt.Errorf("email recipient is required")
	}
	if strings.ContainsAny(msg.Subject, "\r\n") {
		return fmt.Errorf("email subject contains invalid characters")
	}
	for _, address := range append(append([]string{}, msg.To...), msg.CC...) {
		if _, err := mail.ParseAddress(address); err != nil {
			return fmt.Errorf("invalid email recipient")
		}
	}
	return nil
}

func (s *EmailService) checkRateLimit(keys []string, limit int, window time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// 顺带回收完全过期的收件人键，避免进程内 map 无界增长导致内存泄漏。
	if len(s.recent) > 1024 {
		for k, stamps := range s.recent {
			alive := false
			for _, sent := range stamps {
				if now.Sub(sent) < window {
					alive = true
					break
				}
			}
			if !alive {
				delete(s.recent, k)
			}
		}
	}
	for _, key := range keys {
		kept := s.recent[key][:0]
		for _, sent := range s.recent[key] {
			if now.Sub(sent) < window {
				kept = append(kept, sent)
			}
		}
		if len(kept) >= limit {
			s.recent[key] = kept
			return fmt.Errorf("email rate limit exceeded")
		}
		s.recent[key] = append(kept, now)
	}
	return nil
}

// SendTemplate 发送模板邮件
func (s *EmailService) SendTemplate(ctx context.Context, msg *EmailMessage, templateName string, data interface{}) error {
	s.logger.Infow("Sending template email", "template", templateName, "to", msg.To)

	// 解析模板
	tmpl, err := template.New(templateName).Option("missingkey=error").Parse(msg.Body)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// 执行模板
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	msg.Body = buf.String()
	if msg.BodyText != "" {
		textTmpl, parseErr := template.New(templateName + "_text").Option("missingkey=error").Parse(msg.BodyText)
		if parseErr != nil {
			return fmt.Errorf("failed to parse text template: %w", parseErr)
		}
		buf.Reset()
		if executeErr := textTmpl.Execute(&buf, data); executeErr != nil {
			return fmt.Errorf("failed to execute text template: %w", executeErr)
		}
		msg.BodyText = buf.String()
	}

	return s.Send(ctx, msg)
}

// SendTicketNotification 发送工单通知邮件
func (s *EmailService) SendTicketNotification(ctx context.Context, to []string, ticketNumber, ticketTitle, action, content string) error {
	subject := fmt.Sprintf("[ITSM] 工单 %s - %s", ticketNumber, action)

	bodyText := fmt.Sprintf(`
工单通知
========

工单编号: %s
工单标题: %s
操作: %s

%s

---
此邮件由ITSM系统自动发送
发送时间: %s
`, ticketNumber, ticketTitle, action, content, time.Now().Format("2006-01-02 15:04:05"))

	bodyHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6;">
    <div style="max-width: 600px; margin: 0 auto; padding: 20px;">
        <h2 style="color: #333;">工单通知</h2>
        <table style="width: 100%%; border-collapse: collapse; margin: 20px 0;">
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">工单编号</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">工单标题</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">操作</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
        </table>
        <div style="background-color: #f5f5f5; padding: 15px; border-radius: 5px;">
            <p style="margin: 0;">%s</p>
        </div>
        <hr style="border: none; border-top: 1px solid #ddd; margin: 20px 0;">
        <p style="color: #888; font-size: 12px;">此邮件由ITSM系统自动发送</p>
    </div>
</body>
</html>
`, ticketNumber, ticketTitle, action, content)

	msg := &EmailMessage{
		To:       to,
		Subject:  subject,
		Body:     bodyHTML,
		BodyText: bodyText,
	}

	return s.Send(ctx, msg)
}

// SendSLANotification 发送SLA告警邮件
func (s *EmailService) SendSLANotification(ctx context.Context, to []string, ticketNumber, ticketTitle, slaType, deadline string) error {
	subject := fmt.Sprintf("[ITSM SLA告警] 工单 %s - %s 即将到期", ticketNumber, slaType)

	bodyText := fmt.Sprintf(`
SLA告警通知
===========

工单编号: %s
工单标题: %s
SLA类型: %s
截止时间: %s

请及时处理，避免SLA违规。

---
此邮件由ITSM系统自动发送
发送时间: %s
`, ticketNumber, ticketTitle, slaType, deadline, time.Now().Format("2006-01-02 15:04:05"))

	bodyHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6;">
    <div style="max-width: 600px; margin: 0 auto; padding: 20px;">
        <h2 style="color: #f59e0b;">⚠️ SLA告警通知</h2>
        <table style="width: 100%%; border-collapse: collapse; margin: 20px 0;">
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">工单编号</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">工单标题</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">SLA类型</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd;">%s</td>
            </tr>
            <tr>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; font-weight: bold;">截止时间</td>
                <td style="padding: 10px; border-bottom: 1px solid #ddd; color: #ef4444;">%s</td>
            </tr>
        </table>
        <div style="background-color: #fef3c7; padding: 15px; border-radius: 5px; border-left: 4px solid #f59e0b;">
            <p style="margin: 0; color: #92400e;">请及时处理，避免SLA违规！</p>
        </div>
        <hr style="border: none; border-top: 1px solid #ddd; margin: 20px 0;">
        <p style="color: #888; font-size: 12px;">此邮件由ITSM系统自动发送</p>
    </div>
</body>
</html>
`, ticketNumber, ticketTitle, slaType, deadline)

	msg := &EmailMessage{
		To:       to,
		Subject:  subject,
		Body:     bodyHTML,
		BodyText: bodyText,
	}

	return s.Send(ctx, msg)
}

// ValidateConfig 验证邮件配置
func (s *EmailService) ValidateConfig() error {
	if s.config.Host == "" {
		return fmt.Errorf("email host is required")
	}
	if s.config.Port == 0 {
		return fmt.Errorf("email port is required")
	}
	if s.config.Username == "" && s.config.Password != "" {
		return fmt.Errorf("email password requires a username")
	}
	if s.config.From == "" {
		return fmt.Errorf("email from address is required")
	}
	if _, err := mail.ParseAddress(s.config.From); err != nil {
		return fmt.Errorf("email from address is invalid")
	}
	return nil
}

// SendPasswordResetEmail 发送密码重置邮件
func (s *EmailService) SendPasswordResetEmail(ctx context.Context, to []string, resetToken, baseURL string) error {
	// 构建重置链接
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", baseURL, resetToken)

	subject := "[ITSM] 密码重置"

	bodyText := fmt.Sprintf(`
密码重置
========

您收到此邮件是因为您请求重置ITSM系统的密码。

请点击以下链接重置密码：
%s

此链接将在1小时后失效。

如果您没有请求重置密码，请忽略此邮件。

---
此邮件由ITSM系统自动发送
发送时间: %s
`, resetURL, time.Now().Format("2006-01-02 15:04:05"))

	bodyHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.6;">
    <div style="max-width: 600px; margin: 0 auto; padding: 20px;">
        <h2 style="color: #333;">密码重置</h2>
        <div style="background-color: #f5f5f5; padding: 20px; border-radius: 5px; margin: 20px 0;">
            <p style="margin: 0 0 15px 0;">您收到此邮件是因为您请求重置ITSM系统的密码。</p>
            <p style="margin: 0 0 15px 0;">请点击下方按钮重置密码：</p>
            <p style="text-align: center; margin: 30px 0;">
                <a href="%s" style="background-color: #1890ff; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; display: inline-block;">
                    重置密码
                </a>
            </p>
            <p style="margin: 0; color: #888; font-size: 12px;">此链接将在1小时后失效</p>
        </div>
        <div style="background-color: #fff7e6; padding: 15px; border-radius: 5px; border-left: 4px solid #faad14;">
            <p style="margin: 0; color: #d48806;">如果您没有请求重置密码，请忽略此邮件。</p>
        </div>
        <hr style="border: none; border-top: 1px solid #ddd; margin: 20px 0;">
        <p style="color: #888; font-size: 12px;">此邮件由ITSM系统自动发送</p>
    </div>
</body>
</html>
`, resetURL)

	msg := &EmailMessage{
		To:       to,
		Subject:  subject,
		Body:     bodyHTML,
		BodyText: bodyText,
	}

	return s.Send(ctx, msg)
}
