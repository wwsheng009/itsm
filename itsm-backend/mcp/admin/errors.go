package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"itsm-backend/mcp/transport"
)

// ErrorCode 是管理 API 的字符串错误码（分析报告 §5.5 枚举；前端据此分支）。
type ErrorCode string

const (
	// CodeInvalidTransport：传输类型非法/缺失（400）。
	CodeInvalidTransport ErrorCode = "invalid_transport"
	// CodeDuplicateName：同租户 name 冲突（409）。
	CodeDuplicateName ErrorCode = "duplicate_name"
	// CodeSSRFBlocked：出站安全校验拒绝（422；不暴露内网细节）。
	CodeSSRFBlocked ErrorCode = "ssrf_blocked"
	// CodeConnectTimeout：建连超时（502 类）。
	CodeConnectTimeout ErrorCode = "connect_timeout"
	// CodeTLSError：TLS 握手/证书失败（502 类）。
	CodeTLSError ErrorCode = "tls_error"
	// CodeAuthRequired：服务端要求认证或凭据被拒（502 类；需管理员处理）。
	CodeAuthRequired ErrorCode = "auth_required"
	// CodeProtocolMismatch：MCP 协议版本不支持（502 类；不降级）。
	CodeProtocolMismatch ErrorCode = "protocol_mismatch"
	// CodeUnreachable：DNS/网络不可达（502 类）。
	CodeUnreachable ErrorCode = "unreachable"
	// CodeNotFound：对象不存在（404）。
	CodeNotFound ErrorCode = "not_found"
	// CodeConflict：乐观锁冲突（409）。
	CodeConflict ErrorCode = "conflict"
	// CodeCredentialError：凭据处理失败（500 类；不回显明文）。
	CodeCredentialError ErrorCode = "credential_error"
	// CodeValidationFailed：参数校验失败（400）。
	CodeValidationFailed ErrorCode = "validation_failed"
	// CodeServerDisabled：服务器未启用（409，用于「未启用即调用」等非法状态）。
	CodeServerDisabled ErrorCode = "server_disabled"
	// CodeUnavailable：管理服务或依赖未就绪（503）。
	CodeUnavailable ErrorCode = "unavailable"
	// CodeInternal：未分类内部错误（500）。
	CodeInternal ErrorCode = "internal_error"
)

// AdminError 是带 HTTP 映射的管理错误。
type AdminError struct {
	Status  int
	Code    ErrorCode
	Message string
	Err     error
}

// Error 实现 error（消息不含凭据；底层错误已由各层脱敏）。
func (e *AdminError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is/As 链。
func (e *AdminError) Unwrap() error { return e.Err }

// NewAdminError 构造管理错误。
func NewAdminError(status int, code ErrorCode, message string) *AdminError {
	return &AdminError{Status: status, Code: code, Message: message}
}

// WrapAdminError 构造带底层原因的管理错误。
func WrapAdminError(status int, code ErrorCode, message string, err error) *AdminError {
	return &AdminError{Status: status, Code: code, Message: message, Err: err}
}

// AsAdminError 提取管理错误（非管理错误返回 false）。
func AsAdminError(err error) (*AdminError, bool) {
	var target *AdminError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// MapTransportError 把 transport 分层错误码映射为管理错误（§5.5 的 HTTP 映射）。
func MapTransportError(err error) *AdminError {
	if err == nil {
		return nil
	}
	if adminErr, ok := AsAdminError(err); ok {
		return adminErr
	}
	switch transport.CodeOf(err) {
	case transport.CodeInvalidTransport:
		return WrapAdminError(http.StatusBadRequest, CodeInvalidTransport, "传输配置非法", err)
	case transport.CodeSSRFBlocked:
		return WrapAdminError(http.StatusUnprocessableEntity, CodeSSRFBlocked, "目标地址被出站安全策略拒绝", err)
	case transport.CodeConnectTimeout:
		return WrapAdminError(http.StatusBadGateway, CodeConnectTimeout, "连接超时", err)
	case transport.CodeTLSError:
		return WrapAdminError(http.StatusBadGateway, CodeTLSError, "TLS 握手失败", err)
	case transport.CodeAuthRequired:
		return WrapAdminError(http.StatusBadGateway, CodeAuthRequired, "服务端要求认证或凭据被拒", err)
	case transport.CodeProtocolMismatch:
		return WrapAdminError(http.StatusBadGateway, CodeProtocolMismatch, "MCP 协议版本不兼容", err)
	case transport.CodeUnreachable:
		return WrapAdminError(http.StatusBadGateway, CodeUnreachable, "目标不可达", err)
	case transport.CodeCanceled:
		return WrapAdminError(http.StatusRequestTimeout, CodeConnectTimeout, "请求已取消", err)
	default:
		// 兜底启发式：SDK/传输层在部分连接类失败上未携带分层码，按文本归类，
		// 避免把「连不上」误报为 500（M0-14 可回填 transport 侧更精确的分类）。
		if looksLikeTimeout(err) {
			return WrapAdminError(http.StatusBadGateway, CodeConnectTimeout, "连接超时", err)
		}
		if looksLikeUnreachable(err) {
			return WrapAdminError(http.StatusBadGateway, CodeUnreachable, "目标不可达", err)
		}
		return WrapAdminError(http.StatusBadGateway, ErrorCode(transport.CodeTransportError), "传输失败", err)
	}
}

func looksLikeTimeout(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "deadline exceeded") ||
		strings.Contains(text, "i/o timeout") ||
		strings.Contains(text, "timeout awaiting") ||
		strings.Contains(text, "handshake timeout")
}

func looksLikeUnreachable(err error) bool {
	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused",
		"actively refused",
		"no such host",
		"no route to host",
		"network is unreachable",
		"connection reset",
		"connectex",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
