package provider

import (
	"context"
	"errors"
	"strings"

	"itsm-backend/mcp/transport"
)

// 稳定错误码（Gate3 之后由 handlers 回填给模型/审计；**不含**堆栈、URL、内网地址与凭据）。
const (
	CodeToolNotFound      = "tool_not_found"
	CodeInvalidArgs       = "invalid_args"
	CodeTargetUnavailable = "target_unavailable"
	CodeToolTimeout       = "tool_timeout"
	CodeToolCanceled      = "tool_canceled"
	CodeAuthRequired      = "auth_required"
	CodeServerError       = "server_error"
	CodeToolError         = "tool_error"
)

// ExecuteError 是 provider 的稳定错误（可 errors.Is/As 判定，安全可回显）。
type ExecuteError struct {
	Code    string
	Message string
}

func (e *ExecuteError) Error() string { return e.Code + ": " + e.Message }

// CodeOf 提取 provider 错误码（非 ExecuteError → ""）。
func CodeOf(err error) string {
	var execErr *ExecuteError
	if errors.As(err, &execErr) {
		return execErr.Code
	}
	return ""
}

// Execute 实现 service.ToolProvider：解析 → 只读校验 → 参数 schema 校验 → 调用 → 规范化。
//
// 并发与单次超时由 manager 治理（每服务器并发默认 4、超时取服务器 TimeoutMS），此处不重复叠加。
func (p *Provider) Execute(ctx context.Context, tenantID int, name string, args map[string]interface{}) (interface{}, error) {
	tool, ok := p.lookup(ctx, tenantID, name)
	if !ok {
		return nil, &ExecuteError{Code: CodeToolNotFound, Message: "工具不存在或当前不可用"}
	}
	if !tool.def.ReadOnly {
		// M0-09 一期只暴露只读工具；写工具的 Gate3 编排在 M1-02 接入。
		return nil, &ExecuteError{Code: CodeToolNotFound, Message: "写工具需经审批链路，当前不可直接执行"}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if tool.schema != nil {
		if err := tool.schema.Validate(args); err != nil {
			return nil, &ExecuteError{Code: CodeInvalidArgs, Message: "参数不符合工具 schema"}
		}
	}
	result, err := p.source.CallTool(ctx, tool.serverID, tool.rawName, args)
	if err != nil {
		return nil, mapCallError(err)
	}
	return normalizeResult(result, p.opts.MaxResultBytes)
}

// mapCallError 把传输层错误映射为稳定、可回显的 provider 错误码。
func mapCallError(err error) error {
	switch transport.CodeOf(err) {
	case transport.CodeConnectTimeout:
		return &ExecuteError{Code: CodeToolTimeout, Message: "调用超时"}
	case transport.CodeCanceled:
		return &ExecuteError{Code: CodeToolCanceled, Message: "调用已取消"}
	case transport.CodeAuthRequired:
		return &ExecuteError{Code: CodeAuthRequired, Message: "目标服务器要求认证"}
	case transport.CodeProtocolMismatch:
		return &ExecuteError{Code: CodeToolError, Message: "协议版本不匹配"}
	case transport.CodeServerError:
		return &ExecuteError{Code: CodeServerError, Message: "服务器返回错误"}
	case transport.CodeUnreachable, transport.CodeSSRFBlocked, transport.CodeTLSError, transport.CodeInvalidTransport:
		return &ExecuteError{Code: CodeTargetUnavailable, Message: "目标服务器不可达"}
	case transport.CodeTransportError:
		return &ExecuteError{Code: CodeToolError, Message: "传输失败"}
	default:
		// 兜底：不携带原始错误文本（防内网信息/堆栈外泄），只给稳定码。
		return &ExecuteError{Code: CodeToolError, Message: "工具调用失败"}
	}
}

// SummarizeError 供审计/日志使用的短摘要（≤200 字符，无堆栈）。
func SummarizeError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 200 {
		text = text[:200]
	}
	return strings.TrimSpace(text)
}
