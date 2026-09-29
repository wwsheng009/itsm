package ai

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"itsm-backend/service/bot"
)

// B2-05 参数守卫：模型**不可**经工具参数覆盖身份/租户/权限。
//
// 依据（阶段一报告 G1/G2 与实施方案 B2-05）：身份、租户、权限一律以服务端上下文为准，
// 模型生成的参数只允许携带业务字段。此前 args 原样透传给执行器——若某工具把
// `tenant_id`/`user_id`/`role` 之类键透传到下游查询，模型即可跨租户读取或提权。
//
// 策略：**剥离 + 留痕**（不是报错）：剥离只影响模型伪造的键，业务参数不受影响；
// 留痕写入工具调用审计的 permission_reason（`args_stripped:<keys>`）与 WARN 日志，
// 便于安全复盘"谁在尝试覆写身份"。
//
// 边界：目标对象参数（target_type/target_id/entrypoint 等**协议键**）由 B3-01
// ScopeResolver 以服务端解析值覆写（见 reservedScopeArgKeys）；业务键（如工具
// 自身语义的 `ticket_id`/`ci_id`）不在本表——那是模型可正常携带的业务参数。
var reservedIdentityArgKeys = map[string]bool{
	"tenant_id": true, "tenantid": true,
	"user_id": true, "userid": true,
	"actor_id": true, "actorid": true,
	"acting_user_id": true, "actinguserid": true,
	"current_user_id": true, "currentuserid": true,
	"role": true, "auth_role": true, "authrole": true,
	"is_admin": true, "isadmin": true, "super_admin": true, "superadmin": true,
}

// reservedScopeArgKeys 是入口上下文的**协议键**（B3-01）：模型只能经 HTTP 请求体
// 的受校验字段（entrypoint/targetType/targetId）表达——工具参数中的同名键一律剥离，
// 由服务端解析结果覆写（仅当模型尝试使用这些键时，见 injectScopeArgs）。
var reservedScopeArgKeys = map[string]bool{
	"entrypoint": true, "entry_point": true, "entrypoint_name": true,
	"target_type": true, "targettype": true,
	"target_id": true, "targetid": true,
	"scope": true, "scope_target_type": true, "scope_target_id": true,
}

// sanitizeReservedArgs 剥离身份/租户/权限键。
//
// 返回剥离后的参数与**升序排列**的被剥离键名（便于审计与断言稳定）。
// 无剥离时原样返回入参（零分配、调用方语义不变）。
func sanitizeReservedArgs(args map[string]interface{}) (map[string]interface{}, []string) {
	if len(args) == 0 {
		return args, nil
	}
	var stripped []string
	cleaned := make(map[string]interface{}, len(args))
	for key, value := range args {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if reservedIdentityArgKeys[normalized] || reservedScopeArgKeys[normalized] {
			stripped = append(stripped, key)
			continue
		}
		cleaned[key] = value
	}
	if len(stripped) == 0 {
		return args, nil
	}
	sort.Strings(stripped)
	return cleaned, stripped
}

// argsStrippedMarker 生成审计留痕（稳定前缀，测试与安全复盘据此检索）。
func argsStrippedMarker(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return "args_stripped:" + strings.Join(keys, ",")
}

// scopeProtocolKeysStripped 报告被剥离的键里是否含入口上下文协议键（B3-01）。
func scopeProtocolKeysStripped(keys []string) bool {
	for _, key := range keys {
		if reservedScopeArgKeys[strings.ToLower(strings.TrimSpace(key))] {
			return true
		}
	}
	return false
}

// injectScopeArgs 以服务端解析的入口上下文**覆写**模型尝试携带的目标协议键（B3-01）。
//
// 语义：仅当模型确实用了协议键（scopeKeysTried=true）时才注入——模型没提目标时
// 不改变工具入参形状（零行为变化）；模型提到目标时，值一律以服务端校验过的
// scope 为准（无目标上下文则保持剥离，模型不得凭空指定目标）。
func injectScopeArgs(args map[string]interface{}, scope bot.Scope, scopeKeysTried bool) map[string]interface{} {
	if !scopeKeysTried || !scope.HasTarget() {
		return args
	}
	if args == nil {
		args = make(map[string]interface{}, 2)
	}
	args["target_type"] = scope.TargetType
	args["target_id"] = scope.TargetID
	return args
}

// respondBotSelectionError 把 B2-05 的 Bot 归属校验错误映射为 HTTP 语义：
//   - 不存在/跨租户 → 404（与「跨租户一律表现为不存在」的既有口径一致）；
//   - 校验依赖不可用 → 503 fail-closed（绝不静默放行）。
func respondBotSelectionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBotSelectionNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"code":  "BOT_NOT_FOUND",
			"error": "所选 Bot 不存在或不属于当前租户",
		})
	case errors.Is(err, ErrBotSelectionUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":  "BOT_SELECTION_UNAVAILABLE",
			"error": "Bot 归属校验不可用，请稍后重试",
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "内部错误"})
	}
}
