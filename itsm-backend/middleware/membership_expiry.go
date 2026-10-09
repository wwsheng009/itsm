package middleware

import (
	"time"

	"itsm-backend/ent/predicate"
	"itsm-backend/ent/usertenantmembership"
)

// membership_expiry.go：membership 到期判定（IP-P2-9 惰性判定，读路径 fail-closed）。
//
// 语义（权威：offboarding 方案 §4 P2 / scope-model B.3.5、B.8）：
//   - expires_at IS NULL → 永不过期（既有行口径不变）；
//   - expires_at > now  → 有效；
//   - expires_at <= now → 视为不存在（fail-closed），与后台回收任务（MembershipExpiryService）
//     同一判定；到期即时失效不依赖后台任务先跑。
//
// 用法：所有「active membership」解析点必须在 Where 中追加本谓词：
// ResolvePermissions（权限解析）、切换邀请依据、工作台成员计数、组织挂靠校验、
// 客户租户「服务方成员」展示。home 行由写入方保证不设 expires_at（后台任务只告警不回收）。
func MembershipNotExpired() predicate.UserTenantMembership {
	return usertenantmembership.Or(
		usertenantmembership.ExpiresAtIsNil(),
		usertenantmembership.ExpiresAtGT(time.Now()),
	)
}
