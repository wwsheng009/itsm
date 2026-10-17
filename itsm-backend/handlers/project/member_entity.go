package project

import "time"

// ---------------------------------------------------------------------------
// 成员 DTO（PE-3；权威定义：docs/project-management/04、plan/p0-integration-contracts.md §3.6/§3.7）
// ---------------------------------------------------------------------------

// 项目角色词表。
var validProjectRoles = map[string]bool{"pm": true, "member": true, "stakeholder": true}

// 成员来源派生标签（不落库；由目标租户 membership 的 source 派生）。
const (
	OriginInternal = "internal" // source=home：本单位成员
	OriginExternal = "external" // source=invite：外部邀请人员
	OriginProvider = "provider" // source=allocation：服务方投影人员
	OriginPlatform = "platform" // source=platform：平台人员
	OriginUnknown  = "unknown"
)

func memberOrigin(source string) string {
	switch source {
	case "home":
		return OriginInternal
	case "invite":
		return OriginExternal
	case "allocation":
		return OriginProvider
	case "platform":
		return OriginPlatform
	default:
		return OriginUnknown
	}
}

// MemberAddRequest 添加成员。
type MemberAddRequest struct {
	UserID      int    `json:"userId" binding:"required"`
	ProjectRole string `json:"projectRole"`
}

// MemberRoleUpdateRequest 调整成员角色。
type MemberRoleUpdateRequest struct {
	ProjectRole string `json:"projectRole" binding:"required"`
}

// MemberView 成员视图（含派生标签与到期时间；前端 ≤7 天预警）。
type MemberView struct {
	UserID       int        `json:"userId"`
	Name         string     `json:"name"`
	Username     string     `json:"username"`
	ProjectRole  string     `json:"projectRole"`
	Status       string     `json:"status"`
	MemberOrigin string     `json:"memberOrigin"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	JoinedAt     time.Time  `json:"joinedAt"`
	// WeeklyCapacityHours 成员周容量覆盖（P2/PE-23；NULL=继承项目默认，见 p2-resource-heatmap-design.md §3.2）。
	WeeklyCapacityHours *float64 `json:"weeklyCapacityHours,omitempty"`
}

// MemberCandidate 候选成员（最小字段：userId/name/username/memberOrigin/expiresAt）。
type MemberCandidate struct {
	UserID       int        `json:"userId"`
	Name         string     `json:"name"`
	Username     string     `json:"username"`
	MemberOrigin string     `json:"memberOrigin"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}
