package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"itsm-backend/common"
)

// PAT（Personal Access Token）认证支持。
//
// 设计见 docs/plan/personal-access-token-plan-2026-10-16.md：
//   - PAT 与 JWT 共用身份上下文契约：user_id / username / role / tenant_id / token
//     （middleware/auth.go:269-273），因此 RBAC / 租户隔离 / 审计 / 限流零改动即等价。
//   - 装配方式与 ConfigureAccessTokenRevocationRedis 一致（包级单例 + 显式配置），
//     AuthMiddleware(jwtSecret) 签名保持不变。
//   - 令牌明文形如 itsm_pat_<base64url(32B)>；库中只有 sha256 哈希与展示前缀。

// PATAuthTokenPrefix PAT 明文前缀：认证分支判定 + 密钥扫描规则。
const PATAuthTokenPrefix = "itsm_pat_"

// PATAuthSourcePAT 审计 source 取值（middleware/audit.go auditSourceForRequest 读取）。
const PATAuthSourcePAT = "pat"

// 上下文键：PAT 归因与类型标记（AuditMiddleware / RejectPATAuth 消费）。
const (
	CtxKeyAuthTokenID  = "auth_token_id"
	CtxKeyTokenPrefix  = "auth_token_prefix"
	CtxKeyTokenType    = "token_type"
	TokenTypePAT       = "pat"
	TokenTypeAccessJWT = "access"
)

// PATIdentity 认证期解析出的身份（由 handlers/pat 的解析器实现填充）。
type PATIdentity struct {
	TokenID     int
	TokenPrefix string
	UserID      int
	Username    string
	Role        string // 已过 EffectiveTokenRole
	TenantID    int
}

// PATAuthenticator PAT 解析器接口；失败一律返回 error（不区分原因，防枚举）。
type PATAuthenticator interface {
	Resolve(ctx context.Context, plaintext string) (*PATIdentity, error)
}

// PATAuthResult 认证结果：身份 + 是否放行（限流）。
type PATAuthResult struct {
	Identity    *PATIdentity
	RateLimited bool
}

// PATAuthenticatorV2 扩展接口：解析 + 限流 + 节流 touch（由 handlers/pat 实现统一处理）。
// 可选实现：AuthMiddleware 类型断言，未实现时行为同 V1。
type PATAuthenticatorV2 interface {
	PATAuthenticator
	// Authenticate 一次调用完成：解析 → 单令牌限流判定 → best-effort touch（带 IP）。
	// 返回 (identity, allowed, err)；err 非 nil 时 identity 为 nil。
	Authenticate(ctx context.Context, plaintext, clientIP string) (*PATIdentity, bool, error)
}

var patAuth PATAuthenticator // nil = 未启用

// ConfigurePersonalAccessTokens 装配 PAT 认证（enabled=false 时彻底关闭认证分支）。
func ConfigurePersonalAccessTokens(auth PATAuthenticator, enabled bool) {
	if !enabled {
		patAuth = nil
		return
	}
	patAuth = auth
}

func currentPATAuthenticator() PATAuthenticator { return patAuth }

// IsPATToken 判定 bearer 串是否为 PAT 明文。
func IsPATToken(tokenString string) bool {
	return len(tokenString) > len(PATAuthTokenPrefix) && tokenString[:len(PATAuthTokenPrefix)] == PATAuthTokenPrefix
}

// SafePATPrefix 取 PAT 明文的安全前缀（用于日志，绝不返回完整明文）。
func SafePATPrefix(tokenString string) string {
	if len(tokenString) > 17 {
		return tokenString[:17]
	}
	return tokenString
}

// RejectPATAuth 拒绝以 PAT 认证的身份调用（用于 PAT 管理面写路由，D-PAT-13）。
//
// 泄漏的令牌不得创建/轮换/吊销其它令牌（自我繁殖）；管理面写操作仅限浏览器会话。
// 必须挂在 AuthMiddleware 之后。
func RejectPATAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(CtxKeyTokenType) == TokenTypePAT {
			common.Fail(c, common.ForbiddenCode, "请使用浏览器会话管理访问令牌")
			c.Abort()
			return
		}
		c.Next()
	}
}
