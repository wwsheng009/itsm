package ai

import (
	"encoding/json"
	"errors"
	"sort"

	"itsm-backend/service/bot"
)

// B1-03：SSE **单一事件注册表**（服务端唯一事件名定义处）。
//
// 设计口径（与实施方案 §4.2 B1-03、阶段一报告 §5.4 v2、MCP 方案 M1-03 同一张表）：
//
//  1. 事件名只在本文件定义；`writeToolEvent` 等发送点一律引用本表常量，杜绝"同名不同义"。
//  2. **向后兼容策略 = 只增不改**：
//     - v1 事件（sources/delta/done/error/tool_call_*/approval_pending）保持**事件名与载荷字节级不变**；
//     - v2 新增事件（run_started/step/confirmation_required/artifact）在对象载荷中携带 `"v":2` 版本字段；
//     - `confirmation_required` 与 v1 的 `approval_pending` **双发**（同一语义两种名字），
//     旧客户端命中旧名，新客户端读新名，双方都不需要改代码即可灰度。
//  3. **未知事件忽略策略（客户端契约）**：客户端遇到未登记事件名必须静默忽略（可选日志上报），
//     不得中断流、不得把未知事件当错误；服务端因此可以随时新增事件而不破坏旧客户端。
//  4. 与 MCP 线同一注册表：MCP 工具调用（provider=mcp）走 `tool_call_*` / `confirmation_required`，
//     不另立事件名；工具事件载荷中的 `provider/server` 即来源标识。
//  5. `artifact` 为 B3-06 预留（计划类工具产物），本期登记但**不发送**。
//
// 版本语义：v2 的机器可读标记是对象载荷里的 `v` 字段（数组载荷如 sources 不带版本字段，
// 客户端以事件名判断）；v1 载荷不带 `v`，因此「无 v + 已知 v1 名字」= 旧协议，兼容路径明确。
const (
	// SSEProtocolVersionV2 是 v2 协议版本号（写入 v2 事件的 `v` 字段）。
	SSEProtocolVersionV2 = 2

	// ---- v1 事件（载荷不再变更） ----

	// SSEEventSources：RAG 检索来源数组（载荷为数组）。
	SSEEventSources = "sources"
	// SSEEventDelta：正文增量（载荷 {content}）。
	SSEEventDelta = "delta"
	// SSEEventDone：流正常结束（载荷 {conversationId, provider?, providerSource?}）。
	SSEEventDone = "done"
	// SSEEventError：流以错误结束（载荷 {message, errorCode?}）。
	SSEEventError = "error"
	// SSEEventToolCallStarted：工具调用开始（tool_call 家族）。
	SSEEventToolCallStarted = "tool_call_started"
	// SSEEventToolCallFinished：工具调用成功结束（tool_call 家族）。
	SSEEventToolCallFinished = "tool_call_finished"
	// SSEEventToolCallFailed：工具调用失败（tool_call 家族）。
	SSEEventToolCallFailed = "tool_call_failed"
	// SSEEventApprovalPending：写工具等待人工确认（v1 名字，与 confirmation_required 双发）。
	SSEEventApprovalPending = "approval_pending"

	// ---- v2 新增事件（对象载荷携带 `v`） ----

	// SSEEventRunStarted：一次运行开始（运行态档案 B1-01/B1-02 的对外可见入口）。
	SSEEventRunStarted = "run_started"
	// SSEEventStep：运行步骤（llm/tool/confirm），与 bot_steps 同源。
	SSEEventStep = "step"
	// SSEEventConfirmationRequired：写工具等待人工确认（v2 正名）。
	SSEEventConfirmationRequired = "confirmation_required"
	// SSEEventArtifact：计划类工具产出的 artifact（B3-06 起发送）。
	SSEEventArtifact = "artifact"
)

// 事件家族：同一家族的多态事件共享语义与载荷骨架（前端可按家族聚合渲染）。
const (
	SSEFamilyToolCall     = "tool_call"
	SSEFamilyConfirmation = "confirmation"
)

// SSEEventSpec 是注册表的一行。
type SSEEventSpec struct {
	// Name：对外事件名（`event:` 行）。
	Name string
	// Version：引入版本（1 或 2）。
	Version int
	// Family：事件家族（空 = 独立事件）。
	Family string
	// Aliases：同一语义的别名事件（服务端双发；旧名在前）。
	Aliases []string
	// Payload：载荷字段清单（文档用途，供评审与前端类型对齐）。
	Payload string
}

// sseEventRegistry 是**唯一**的事件注册表；新增事件必须先在此登记（并有测试保证发送点覆盖）。
var sseEventRegistry = []SSEEventSpec{
	{Name: SSEEventSources, Version: 1, Payload: "[{objectType,id,title,snippet,score}]（数组载荷）"},
	{Name: SSEEventDelta, Version: 1, Payload: "{content}"},
	{Name: SSEEventDone, Version: 1, Payload: "{conversationId, provider?, providerSource?}"},
	{Name: SSEEventError, Version: 1, Payload: "{message, errorCode?}"},
	{Name: SSEEventToolCallStarted, Version: 1, Family: SSEFamilyToolCall, Payload: "{id?,tool,provider,server?,phase,status:\"started\"}"},
	{Name: SSEEventToolCallFinished, Version: 1, Family: SSEFamilyToolCall, Payload: "{id?,tool,provider,server?,phase,status:\"done\",summary,durationMs}"},
	{Name: SSEEventToolCallFailed, Version: 1, Family: SSEFamilyToolCall, Payload: "{id?,tool,provider,server?,phase,status:\"failed\",errorCode}"},
	{Name: SSEEventRunStarted, Version: 2, Payload: "{v:2, runId, entrypoint, conversationId?}"},
	{Name: SSEEventStep, Version: 2, Payload: "{v:2, runId, stepIndex, type, payloadRef?, durationMs}"},
	// 同一语义双名：v2 正名 + v1 旧名（别名）。旧名载荷保持 v1 原样，新名载荷追加 `v:2`。
	{Name: SSEEventConfirmationRequired, Version: 2, Family: SSEFamilyConfirmation, Aliases: []string{SSEEventApprovalPending}, Payload: "{v:2, id,tool,provider,server?,phase:\"write\",status:\"pending\"}；别名 approval_pending 为同一载荷（无 v）"},
	{Name: SSEEventArtifact, Version: 2, Payload: "{v:2, artifactId, kind, title, conversationId?}（B3-06 起发送）"},
}

// SSEEventSpecs 返回注册表副本（供评审文档、前端类型与测试断言）。
func SSEEventSpecs() []SSEEventSpec {
	out := make([]SSEEventSpec, len(sseEventRegistry))
	for i, spec := range sseEventRegistry {
		out[i] = spec
		if len(spec.Aliases) > 0 {
			out[i].Aliases = append([]string(nil), spec.Aliases...)
		}
	}
	return out
}

// IsKnownSSEEvent 判定事件名是否已登记（含别名）。
func IsKnownSSEEvent(name string) bool {
	for _, spec := range sseEventRegistry {
		if spec.Name == name {
			return true
		}
		for _, alias := range spec.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
}

// sseAliasesOf 返回事件名的别名列表（无别名返回 nil）。
func sseAliasesOf(name string) []string {
	for _, spec := range sseEventRegistry {
		if spec.Name == name {
			return spec.Aliases
		}
	}
	return nil
}

// sseEnvelopeV2 把对象载荷包装为 v2 信封（附加 `v` 字段）；非对象载荷回落为 {v, value}。
//
// v1 事件**绝不**经过本函数：兼容承诺是 v1 载荷字节级不变。
func sseEnvelopeV2(payload interface{}) map[string]interface{} {
	envelope := map[string]interface{}{"v": SSEProtocolVersionV2}
	if payload == nil {
		return envelope
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return envelope
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		envelope["value"] = string(encoded)
		return envelope
	}
	for key, value := range fields {
		envelope[key] = value
	}
	return envelope
}

// sseRunEventNames 是运行侧事件类型 → SSE 事件名的映射（仅登记可外发的事件）。
//
// 不外发：`tool_call`（已由 tool_call_* 三态事件承载，避免双份）、`run_finished`
// （流终止由 `done`/`error` 承载，运行档案本身走审计页）。
var sseRunEventNames = map[string]string{
	"run_started": SSEEventRunStarted,
	"step":        SSEEventStep,
}

// writeSSERunEvent 把运行侧事件映射为 SSE 帧；返回是否已发送。
//
// 调用前提：该事件**已落库成功**（先落库后广播），因此本函数不做持久化。
func writeSSERunEvent(write func(event string, payload interface{}), eventType string, payload map[string]interface{}) bool {
	name, ok := sseRunEventNames[eventType]
	if !ok {
		return false
	}
	write(name, sseEnvelopeV2(payload))
	return true
}

// sseErrorPayload 组装 error 事件载荷：稳定错误码优先（budget_exceeded → 内建；
// 外部错误类型实现 ErrorCode() 接口则透传；其余仅 message）。
//
// 该函数与 v1 error 契约一致：**不**添加 `v` 字段（v1 载荷不变）。
func sseErrorPayload(err error) map[string]string {
	payload := map[string]string{"message": err.Error()}
	if err == nil {
		return payload
	}
	if errors.Is(err, bot.ErrBudgetExceeded) {
		payload["errorCode"] = bot.ErrorCodeBudgetExceeded
		return payload
	}
	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) {
		if code := coder.ErrorCode(); code != "" {
			payload["errorCode"] = code
		}
	}
	return payload
}

// sortedSSEEventNames 返回排序后的事件名（含别名），供文档生成与测试断言。
func sortedSSEEventNames() []string {
	names := make([]string, 0, len(sseEventRegistry))
	for _, spec := range sseEventRegistry {
		names = append(names, spec.Name)
		names = append(names, spec.Aliases...)
	}
	sort.Strings(names)
	return names
}
