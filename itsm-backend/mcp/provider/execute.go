package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"itsm-backend/mcp/transport"
	"itsm-backend/pkg/redact"
	"itsm-backend/service"

	"itsm-backend/mcp/client"
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

// ErrorCode 实现 service 侧的稳定错误码契约（供 ToolQueue 审计回填，避免 service 反向依赖本包）。
func (e *ExecuteError) ErrorCode() string { return e.Code }

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
// M0-11：所有返回路径都填充审计元数据（三元组 + 耗时 + 稳定错误码 + 脱敏输出摘要）。
func (p *Provider) Execute(ctx context.Context, tenantID int, name string, args map[string]interface{}) (*service.ToolExecution, error) {
	return p.execute(ctx, tenantID, name, args, false)
}

// ExecuteApprovedWrite 实现 service.WriteCapableProvider：**仅由 ToolQueue 在 Gate3 审批通过后调用**。
//
// 与 Execute 的唯一差别是允许执行写工具（read_only=false）。安全边界：
//   - 直接执行入口（ToolRegistry.ExecuteWithMeta / provider.Execute）对写工具保持拒绝，审批链路外的调用拿不到写路径；
//   - 写调用**单次执行、不自动重试**：传输层/管理器不存在自动重试（失败即失败，避免重复副作用）；
//   - 解析/租户/治理/健康/隔离/schema 校验与只读路径完全同源。
func (p *Provider) ExecuteApprovedWrite(ctx context.Context, tenantID int, name string, args map[string]interface{}) (*service.ToolExecution, error) {
	return p.execute(ctx, tenantID, name, args, true)
}

func (p *Provider) execute(ctx context.Context, tenantID int, name string, args map[string]interface{}, allowWrite bool) (*service.ToolExecution, error) {
	started := time.Now()
	execution := &service.ToolExecution{
		Provider:     ProviderName,
		CallableName: name,
	}
	finish := func(err error) {
		execution.DurationMs = time.Since(started).Milliseconds()
		// 亚毫秒往返记为 1ms：审计口径为「成功调用耗时 ≥1ms」，避免出现 0 让审计/统计误判为缺失。
		if execution.DurationMs == 0 {
			execution.DurationMs = 1
		}
		if err != nil {
			execution.ErrorCode = CodeOf(err)
		}
	}

	tool, ok := p.lookup(ctx, tenantID, name)
	if !ok {
		err := &ExecuteError{Code: CodeToolNotFound, Message: "工具不存在或当前不可用"}
		finish(err)
		return execution, err
	}
	execution.ServerName = tool.serverName
	execution.RawToolName = tool.rawName
	execution.CallableName = tool.callable

	if !tool.def.ReadOnly && !allowWrite {
		// 写工具必须经 Gate3 审批后由 ToolQueue 调用 ExecuteApprovedWrite；此处保持 fail-closed。
		err := &ExecuteError{Code: CodeToolNotFound, Message: "写工具需经审批链路，当前不可直接执行"}
		finish(err)
		return execution, err
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if tool.schema != nil {
		if err := tool.schema.Validate(args); err != nil {
			execErr := &ExecuteError{Code: CodeInvalidArgs, Message: "参数不符合工具 schema"}
			finish(execErr)
			return execution, execErr
		}
	}
	// 执行策略（M1-08）：读工具可至多重试 1 次（瞬时错误），写工具恒不重试。
	var result *client.CallResult
	var err error
	if policySource, ok := p.source.(RetryableToolSource); ok {
		result, err = policySource.CallToolWithPolicy(ctx, tool.serverID, tool.rawName, args, tool.def.ReadOnly)
	} else {
		result, err = p.source.CallTool(ctx, tool.serverID, tool.rawName, args)
	}
	if err != nil {
		mapped := mapCallError(err)
		finish(mapped)
		return execution, mapped
	}
	value, err := normalizeResult(result, p.opts.MaxResultBytes)
	if err != nil {
		finish(err)
		return execution, err
	}
	execution.Value = value
	// 摘要：脱敏 + 截断（落 tool_invocations.output_summary；不落原始 Value）。
	execution.OutputSummary = redact.ValueSummary(value, 512)
	finish(nil)
	return execution, nil
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
