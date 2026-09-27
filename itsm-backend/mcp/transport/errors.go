package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Code 是 MCP 连接/传输错误的分层枚举（供管理 API 映射；对外提示不得泄露内网结构）。
type Code string

const (
	// CodeInvalidTransport：配置非法（未知传输类型、缺 URL、缺 SSRF 校验器等）。
	CodeInvalidTransport Code = "invalid_transport"
	// CodeSSRFBlocked：出站安全校验拒绝（M0-05；对外提示统一此码，不暴露内网细节）。
	CodeSSRFBlocked Code = "ssrf_blocked"
	// CodeConnectTimeout：建连/握手超时（连接超时默认 10s）。
	CodeConnectTimeout Code = "connect_timeout"
	// CodeCanceled：调用方取消（非超时）。
	CodeCanceled Code = "canceled"
	// CodeTLSError：TLS 证书/握手失败。
	CodeTLSError Code = "tls_error"
	// CodeAuthRequired：服务器要求认证（HTTP 401/407）。
	CodeAuthRequired Code = "auth_required"
	// CodeProtocolMismatch：MCP 协议版本不受支持（不降级）。
	CodeProtocolMismatch Code = "protocol_mismatch"
	// CodeUnreachable：DNS 解析失败 / 网络不可达 / 连接被拒。
	CodeUnreachable Code = "unreachable"
	// CodeServerError：远端 JSON-RPC 错误（如工具不存在、服务端内部错误）。
	CodeServerError Code = "server_error"
	// CodeTransportError：其它传输层错误（兜底）。
	CodeTransportError Code = "transport_error"
)

// Error 是带分层码的传输/连接错误。
type Error struct {
	Code Code
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	detail := ""
	if e.Err != nil {
		detail = e.Err.Error()
	}
	if e.Op == "" {
		return fmt.Sprintf("%s: %s", e.Code, detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Op, detail)
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf 返回错误的分层码（nil → ""；未分类 → CodeTransportError）。
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return Classify(err)
}

// Classify 对底层错误分类。已带分层码的错误原样返回其码。
func Classify(err error) Code {
	if err == nil {
		return ""
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}

	// 协议版本不匹配（SDK 内部错误类型未导出，按错误文本识别；SDK 升级时由回归测试守卫）。
	if strings.Contains(err.Error(), "unsupported protocol version") {
		return CodeProtocolMismatch
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return CodeConnectTimeout
	}
	if errors.Is(err, context.Canceled) {
		return CodeCanceled
	}

	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return CodeTLSError
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return CodeTLSError
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return CodeTLSError
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return CodeTLSError
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return CodeTLSError
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return CodeUnreachable
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return CodeUnreachable
	}

	msg := strings.ToLower(err.Error())

	// 兜底：第三方 SDK 可能用 %v 包装错误（丢失 Unwrap 链），按自身分层码文本识别。
	// 注意：不包含 CodeTransportError（"transport_error" 是兜底码，避免自我匹配）。
	selfCodes := []Code{
		CodeSSRFBlocked, CodeProtocolMismatch, CodeAuthRequired, CodeConnectTimeout,
		CodeTLSError, CodeInvalidTransport, CodeServerError, CodeUnreachable, CodeCanceled,
	}
	for _, code := range selfCodes {
		if strings.Contains(msg, string(code)) {
			return code
		}
	}

	switch {
	case strings.Contains(msg, "jsonrpc"):
		// 远端 JSON-RPC 错误（如 method not found / 服务端内部错误）。
		return CodeServerError
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return CodeConnectTimeout
	case strings.Contains(msg, "x509"), strings.Contains(msg, "certificate"), strings.Contains(msg, "tls"):
		return CodeTLSError
	default:
		return CodeTransportError
	}
}
