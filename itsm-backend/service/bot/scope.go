package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// B3-01 入口枚举与上下文协议（ScopeResolver）。
//
// 边界（阶段一报告 G9/G10、BD2）：
//   - 身份、租户、目标对象**一律以服务端解析为准**；模型提供的目标参数只作提示，
//     任何需要目标对象的调用都必须在执行面重新校验（本组件负责入口侧的预检）。
//   - 入口值决定工具面过滤（`template.entrypoints_json` 必须命中该值），并写入
//     `bot_runs.entrypoint` 供审计。
//   - 目标对象预检 fail-closed：不存在 / 跨租户 / 无权访问 → 拒绝携入上下文，
//     绝不静默降级为「无目标」。
//
// 已知入口（与 §4.4 B3-01 协议一致；`chat` 常量定义在 policy.go）。
const (
	EntrypointTicketDetail   = "ticket_detail"
	EntrypointTicketList     = "ticket_list"
	EntrypointIncidentDetail = "incident_detail"
	EntrypointIncidentCreate = "incident_create"
	EntrypointCIDetail       = "ci_detail"
)

// KnownEntrypoints 返回全部合法入口（稳定顺序，供校验与前端枚举）。
func KnownEntrypoints() []string {
	return []string{
		EntrypointChat,
		EntrypointTicketDetail,
		EntrypointTicketList,
		EntrypointIncidentDetail,
		EntrypointIncidentCreate,
		EntrypointCIDetail,
	}
}

func isKnownEntrypoint(value string) bool {
	for _, candidate := range KnownEntrypoints() {
		if candidate == value {
			return true
		}
	}
	return false
}

// 目标对象类型（与 ent 实体一一对应）。
const (
	TargetTypeTicket   = "ticket"
	TargetTypeIncident = "incident"
	TargetTypeCI       = "ci"
)

// KnownTargetTypes 返回全部合法目标类型（稳定顺序）。
func KnownTargetTypes() []string {
	return []string{TargetTypeTicket, TargetTypeIncident, TargetTypeCI}
}

func isKnownTargetType(value string) bool {
	for _, candidate := range KnownTargetTypes() {
		if candidate == value {
			return true
		}
	}
	return false
}

// Scope 是入口上下文（协议：{entrypoint, target_type, target_id, summary}）。
//
// 说明：Summary 仅作提示（展示/检索线索），**不替代**执行面的对象校验。
type Scope struct {
	Entrypoint string
	TargetType string
	TargetID   int
	Summary    string
}

// HasTarget 报告上下文是否携带目标对象。
func (s Scope) HasTarget() bool { return s.TargetType != "" && s.TargetID > 0 }

// ScopeInput 是入口上下文原始入参（HTTP 层直传；允许空白与缺省）。
type ScopeInput struct {
	Entrypoint string
	TargetType string
	TargetID   int
	Summary    string
}

// 作用域校验错误（调用方据此映射 HTTP 语义：未知入口/参数不完整 → 400；目标拒绝 → 404）。
var (
	// ErrScopeEntrypointUnknown 入口值未知（防拼写错误静默降级为 chat）。
	ErrScopeEntrypointUnknown = errors.New("bot: 未知入口")
	// ErrScopeTargetPairIncomplete target_type/target_id 只给了一半。
	ErrScopeTargetPairIncomplete = errors.New("bot: 目标参数不完整")
	// ErrScopeTargetTypeUnknown 目标类型未知。
	ErrScopeTargetTypeUnknown = errors.New("bot: 未知目标类型")
	// ErrScopeTargetDenied 目标对象不存在/跨租户/无权访问（对外统一表现为「不可用」）。
	ErrScopeTargetDenied = errors.New("bot: 目标对象不可用")
	// ErrScopeCheckerUnavailable 未注入目标校验器但请求携带目标（fail-closed）。
	ErrScopeCheckerUnavailable = errors.New("bot: 目标校验器不可用")
)

// ScopeSummaryMaxRunes 是 Summary 的截断上限（提示性字段，超长截断而非报错）。
const ScopeSummaryMaxRunes = 300

// TargetChecker 校验目标对象存在且调用者可访问（G9 权限预检）。
//
// 实现要求：租户隔离 + 对象类型对应的读权限；任一不满足必须返回错误（fail-closed）。
type TargetChecker interface {
	CheckTarget(ctx context.Context, tenantID, userID int, role, targetType string, targetID int) error
}

// ScopeResolver 负责入口上下文的归一化与预检。
//
// 零依赖时（checker=nil）仍可归一化入口；一旦请求携带目标对象即 fail-closed。
type ScopeResolver struct {
	checker TargetChecker
}

// NewScopeResolver 构造解析器；checker 允许为 nil（关闭态/测试）。
func NewScopeResolver(checker TargetChecker) *ScopeResolver {
	return &ScopeResolver{checker: checker}
}

// Resolve 归一化并校验入口上下文。
//
// 入口：空白 → `chat`（既有行为）；未知值 → ErrScopeEntrypointUnknown（不静默降级）。
// 目标：两者皆空 → 无目标；只给一半 → ErrScopeTargetPairIncomplete；未知类型 →
// ErrScopeTargetTypeUnknown；校验失败/不可用 → 对应错误（fail-closed）。
func (r *ScopeResolver) Resolve(ctx context.Context, tenantID, userID int, role string, in ScopeInput) (Scope, error) {
	entrypoint := strings.ToLower(strings.TrimSpace(in.Entrypoint))
	if entrypoint == "" {
		entrypoint = EntrypointChat
	}
	if !isKnownEntrypoint(entrypoint) {
		return Scope{}, fmt.Errorf("%w: %q", ErrScopeEntrypointUnknown, in.Entrypoint)
	}

	targetType := strings.ToLower(strings.TrimSpace(in.TargetType))
	targetID := in.TargetID
	hasType, hasID := targetType != "", targetID > 0
	if hasType != hasID {
		return Scope{}, ErrScopeTargetPairIncomplete
	}

	scope := Scope{
		Entrypoint: entrypoint,
		Summary:    truncateSummary(strings.TrimSpace(in.Summary)),
	}
	if !hasType {
		return scope, nil
	}

	if !isKnownTargetType(targetType) {
		return Scope{}, fmt.Errorf("%w: %q", ErrScopeTargetTypeUnknown, in.TargetType)
	}
	if r == nil || r.checker == nil {
		return Scope{}, ErrScopeCheckerUnavailable
	}
	if err := r.checker.CheckTarget(ctx, tenantID, userID, role, targetType, targetID); err != nil {
		return Scope{}, fmt.Errorf("%w: %s#%d: %v", ErrScopeTargetDenied, targetType, targetID, err)
	}
	scope.TargetType = targetType
	scope.TargetID = targetID
	return scope, nil
}

// truncateSummary 按 rune 截断（不切坏多字节字符）。
func truncateSummary(value string) string {
	if utf8.RuneCountInString(value) <= ScopeSummaryMaxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:ScopeSummaryMaxRunes])
}
