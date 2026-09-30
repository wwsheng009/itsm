// B1-05：ToolInvocation 的确认状态机（pending/confirmed/rejected/expired/cancelled）。
//
// 设计边界：
//   - 本文件只承载**纯逻辑**（状态定义、迁移判定、过期判定），不依赖 ent/DB；
//     持久化与定时扫描由 handlers/ai 侧实现（ConfirmationStore）。
//   - 与既有 `approval_state` 取值（none|pending|approved|rejected|auto）保持兼容：
//     `NormalizeConfirmationState` 把 approved 映射为 confirmed，其余未知值按
//     status 派生（fail-safe：无法识别时按终态只读处理，不允许再执行）。
package bot

import "time"

// ConfirmationState 是确认单的规范化状态（API/审计/前端共用的词汇表）。
type ConfirmationState string

const (
	// ConfirmationPending：待确认（唯一可决策的状态）。
	ConfirmationPending ConfirmationState = "pending"
	// ConfirmationConfirmed：已确认（等价既有 approval_state=approved；执行由队列负责）。
	ConfirmationConfirmed ConfirmationState = "confirmed"
	// ConfirmationRejected：已拒绝（终态，不可执行）。
	ConfirmationRejected ConfirmationState = "rejected"
	// ConfirmationExpired：已过期（终态，不可执行；由 TTL 或惰性判定产生）。
	ConfirmationExpired ConfirmationState = "expired"
	// ConfirmationCancelled：已取消（终态；会话/运行被取消时使用，预留）。
	ConfirmationCancelled ConfirmationState = "cancelled"
	// ConfirmationUnknown：无法识别的组合（只读展示；一律视为不可决策）。
	ConfirmationUnknown ConfirmationState = "unknown"
)

// DefaultConfirmationTTL 是确认单默认有效期（B1-05：24h）。
const DefaultConfirmationTTL = 24 * time.Hour

// NormalizeConfirmationState 把 (approval_state, status) 归一为规范化状态。
//
// 兼容口径：
//   - `approved` → confirmed（既有 MCP/审计数据与前端契约不变）；
//   - `rejected` → rejected；`expired`/`cancelled` 原样；
//   - 其余（none/auto/空）按 status 派生：pending→pending、rejected→rejected、
//     expired→expired、cancelled→cancelled、其余→unknown（fail-safe，只读）。
func NormalizeConfirmationState(approvalState, status string) ConfirmationState {
	switch ConfirmationState(approvalState) {
	case ConfirmationPending:
		return ConfirmationPending
	case ConfirmationConfirmed:
		return ConfirmationConfirmed
	case "approved": // 既有持久化取值（M1-02 及以前）
		return ConfirmationConfirmed
	case ConfirmationRejected:
		return ConfirmationRejected
	case ConfirmationExpired:
		return ConfirmationExpired
	case ConfirmationCancelled:
		return ConfirmationCancelled
	}
	// approval_state 未定（none/auto/未知）：按 status 兜底派生。
	switch ConfirmationState(status) {
	case ConfirmationPending:
		return ConfirmationPending
	case ConfirmationRejected:
		return ConfirmationRejected
	case ConfirmationExpired:
		return ConfirmationExpired
	case ConfirmationCancelled:
		return ConfirmationCancelled
	default:
		// 已执行成功/失败等记录：不属于确认单生命周期，按只读的 unknown 展示。
		return ConfirmationUnknown
	}
}

// IsDecidable 判断该状态是否仍可被确认/拒绝（仅 pending 可决策）。
func (s ConfirmationState) IsDecidable() bool { return s == ConfirmationPending }

// IsTerminal 判断是否为终态（终态不可再迁移，也不可执行）。
func (s ConfirmationState) IsTerminal() bool {
	switch s {
	case ConfirmationConfirmed, ConfirmationRejected, ConfirmationExpired, ConfirmationCancelled:
		return true
	default:
		return false
	}
}

// IsExpiredAt 判定 pending 记录在给定时刻是否已过期（零值 expiresAt = 不设期限）。
func IsExpiredAt(expiresAt *time.Time, now time.Time) bool {
	return expiresAt != nil && !expiresAt.IsZero() && now.After(*expiresAt)
}

// DecisionOutcome 描述一次确认决策请求与既有状态的匹配结果。
type DecisionOutcome string

const (
	// DecisionApply：状态为 pending 且未过期 —— 正常落决策。
	DecisionApply DecisionOutcome = "apply"
	// DecisionReplay：与既有决策**同人同向** —— 幂等回放，不重复执行。
	DecisionReplay DecisionOutcome = "replay"
	// DecisionConflict：与既有决策不一致（异人/异向/终态） —— 冲突，不改状态。
	DecisionConflict DecisionOutcome = "conflict"
)

// EvaluateDecision 计算「再次确认」应走的语义（B1-05 幂等回放口径）：
//   - pending 且未过期 → apply；
//   - 已 confirmed/rejected/expired/cancelled 且**同一决策人 + 同一决策方向** → replay；
//   - 其余（异人、改判、unknown）→ conflict。
//
// 「同人同向」是幂等键的等价物：客户端重试（网络抖动）必然是同一用户重复同一动作；
// 换人/改判属真实冲突，必须显式报错而不是静默回放。
func EvaluateDecision(current ConfirmationState, approvedBy, actor int, approveNow bool, priorApprove bool) DecisionOutcome {
	if current == ConfirmationPending {
		return DecisionApply
	}
	// 仅对「真实决策结果」允许回放：confirmed / rejected。
	// expired、cancelled 属生命周期终止而非人决策 —— 一律冲突（必须重新发起确认），
	// 否则客户端会误以为过期单仍可执行。
	if current == ConfirmationRejected || current == ConfirmationConfirmed {
		if approvedBy > 0 && approvedBy == actor && approveNow == priorApprove {
			return DecisionReplay
		}
	}
	return DecisionConflict
}
