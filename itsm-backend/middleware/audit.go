package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type loginAuditRequestKey struct{}

type LoginAuditRequest struct{ IP, UserAgent string }

func WithLoginAuditRequest(ctx context.Context, ip, userAgent string) context.Context {
	return context.WithValue(ctx, loginAuditRequestKey{}, LoginAuditRequest{IP: ip, UserAgent: userAgent})
}

// 审计 source 枚举（IP-P0-10 / §3.0-E canonical；NULL=legacy 仅历史行）。
const (
	AuditSourceLogin            = "login"
	AuditSourceSwitch           = "switch"
	AuditSourceHeader           = "header"
	AuditSourceWorkbench        = "workbench"
	AuditSourcePlatformSelected = "platform_selected"
	AuditSourceJob              = "job"
	AuditSourceSystem           = "system"
)

// AuthAuditEntry 认证/会话类审计事件（login/switch 及拒绝事件；事件名以 §3.0-E 为准）。
type AuthAuditEntry struct {
	UserID         int
	TenantID       int // 审计行归属租户（通常为 actor 家租户）
	TargetTenantID int // 跨租户目标（切换/条目级操作）
	ActorAccount   string
	Source         string
	Action         string // 事件目录：auth.login / tenant.switch / tenant.switch_denied ...
	Path           string
	Method         string
	StatusCode     int
	FailureReason  string
}

// RecordAuthAudit writes only explicitly allow-listed authentication metadata.
// Passwords and issued/reset tokens are never accepted by this API.
func RecordAuthAudit(ctx context.Context, client *ent.Client, e AuthAuditEntry) {
	if client == nil {
		return
	}
	req, _ := ctx.Value(loginAuditRequestKey{}).(LoginAuditRequest)
	payload, _ := json.Marshal(map[string]string{"username": e.ActorAccount, "userAgent": req.UserAgent, "failureReason": e.FailureReason})
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// R2B 阴影观察（2026-10-03）：审计写入是跨切面路径，登录阶段尚无租户上下文；
	// 已知租户时显式补齐（RLS WITH CHECK 可用），未知时走 system bypass（审计不丢）。
	if e.TenantID > 0 {
		auditCtx = tenantctx.WithTenantID(auditCtx, e.TenantID)
	} else {
		auditCtx = tenantctx.WithSystemBypass(auditCtx)
	}
	create := client.AuditLog.Create().SetCreatedAt(time.Now()).
		SetUserID(e.UserID).SetIP(req.IP).SetResource("auth").SetAction(e.Action).
		SetPath(e.Path).SetMethod(e.Method).SetStatusCode(e.StatusCode).
		SetRequestBody(string(payload)).SetSource(e.Source)
	if e.TenantID > 0 {
		create = create.SetTenantID(e.TenantID)
	}
	if e.TargetTenantID > 0 {
		create = create.SetTargetTenantID(e.TargetTenantID)
	}
	if e.ActorAccount != "" {
		create = create.SetActorAccount(e.ActorAccount)
	}
	if err := create.Exec(auditCtx); err != nil && globalLogger != nil {
		globalLogger.Errorw("failed to save auth audit", "error", err, "action", e.Action)
	}
}

// RecordTenantDeniedAudit 记录租户面拒绝事件（IP-P1-8）：
//
//	action=tenant.scope_denied —— 显式请求未分配客户（header 通道等，防枚举逐租户记录）；
//	action=tenant.probe_denied —— 跨作用域探测（如 header/JWT 冲突被拒，G9 告警）。
//
// 行归属 actor home 租户（tenant_id），target=被请求/冲突的目标租户，同步写、2s 超时。
func RecordTenantDeniedAudit(
	client *ent.Client,
	c *gin.Context,
	action, source, resource string,
	targetTenantID, statusCode int,
	reasonCode string,
) {
	if client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"reasonCode":     reasonCode,
		"targetTenantId": targetTenantID,
	})
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 同上：actor home 租户优先，未知时 system bypass。
	if tid := c.GetInt("tenant_id"); tid > 0 {
		auditCtx = tenantctx.WithTenantID(auditCtx, tid)
	} else {
		auditCtx = tenantctx.WithSystemBypass(auditCtx)
	}
	create := client.AuditLog.Create().SetCreatedAt(time.Now()).
		SetResource(resource).SetAction(action).
		SetPath(c.FullPath()).SetMethod(c.Request.Method).SetStatusCode(statusCode).
		SetRequestBody(string(payload)).SetSource(source).
		SetIP(c.ClientIP()).SetRequestID(c.GetString("request_id"))
	if uid := c.GetInt("user_id"); uid > 0 {
		create = create.SetUserID(uid)
	}
	if tid := c.GetInt("tenant_id"); tid > 0 {
		create = create.SetTenantID(tid)
	}
	if targetTenantID > 0 {
		create = create.SetTargetTenantID(targetTenantID)
	}
	if acc := c.GetString("username"); acc != "" {
		create = create.SetActorAccount(acc)
	}
	if err := create.Exec(auditCtx); err != nil {
		if globalLogger != nil {
			globalLogger.Errorw("failed to save tenant denied audit", "error", err, "action", action)
		} else {
			zap.S().Errorw("failed to save tenant denied audit", "error", err, "action", action)
		}
	}
}

// AuditableAction 可审计的操作类型
type AuditableAction string

const (
	ActionLogin         AuditableAction = "login"
	ActionLogout        AuditableAction = "logout"
	ActionCreate        AuditableAction = "create"
	ActionUpdate        AuditableAction = "update"
	ActionDelete        AuditableAction = "delete"
	ActionView          AuditableAction = "view"
	ActionSearch        AuditableAction = "search"
	ActionExport        AuditableAction = "export"
	ActionImport        AuditableAction = "import"
	ActionAssign        AuditableAction = "assign"
	ActionEscalate      AuditableAction = "escalate"
	ActionResolve       AuditableAction = "resolve"
	ActionClose         AuditableAction = "close"
	ActionReopen        AuditableAction = "reopen"
	ActionComment       AuditableAction = "comment"
	ActionAttachment    AuditableAction = "attachment"
	ActionPermission    AuditableAction = "permission"
	ActionConfiguration AuditableAction = "configuration"
	// 变更（及其他资源）生命周期动词，避免所有 POST 被笼统记为 create
	ActionSubmit   AuditableAction = "submit"
	ActionApprove  AuditableAction = "approve"
	ActionReject   AuditableAction = "reject"
	ActionSchedule AuditableAction = "schedule"
	ActionStart    AuditableAction = "start"
	ActionComplete AuditableAction = "complete"
	ActionRollback AuditableAction = "rollback"
	ActionCancel   AuditableAction = "cancel"
)

// SensitiveResource 敏感资源类型
var SensitiveResources = map[string]bool{
	"users":          true,
	"roles":          true,
	"permissions":    true,
	"configurations": true,
	"audit_logs":     true,
	"system":         true,
}

// AuditMiddleware persists audit logs for operations with enhanced security tracking
func AuditMiddleware(client *ent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 判断是否需要审计
		if !shouldAuditRequest(c) {
			c.Next()
			return
		}

		// read body safely and restore
		var bodyBytes []byte
		if c.Request.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				if globalLogger != nil {
					globalLogger.Warnw("failed to read request body for audit", "error", err)
				}
			}
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		start := time.Now()
		c.Next()
		duration := time.Since(start)

		// collect fields
		rid := c.GetString("request_id")
		tenantID := c.GetInt("tenant_id")
		userID := 0
		username := ""

		if v, ok := c.Get("user_id"); ok {
			switch t := v.(type) {
			case int:
				userID = t
			case int32:
				userID = int(t)
			case int64:
				userID = int(t)
			}
		}

		if v, ok := c.Get("username"); ok {
			if uname, ok := v.(string); ok {
				username = uname
			}
		}

		ip := c.ClientIP()
		path := c.Request.URL.Path
		methodStr := c.Request.Method
		status := c.Writer.Status()
		userAgent := c.Request.UserAgent()

		// limit stored body size to avoid oversized logs
		requestBody := string(bodyBytes)
		if len(requestBody) > 4096 {
			requestBody = requestBody[:4096] + "..."
		}
		// mask sensitive fields
		requestBody = MaskSensitiveFields(requestBody)

		// 确定操作类型和资源
		action, resource, _ := determineActionAndResource(c)

		// 检查是否为敏感操作
		isSensitive := isSensitiveOperation(action, resource, status)

		// IP-P0-10：作用域字段（source / target_tenant_id / actor_account）。
		// TUM-3：target_user_id 由业务 handler 显式标记（账号治理类动作的目标用户）。
		source := auditSourceForRequest(c)
		targetTenantID := c.GetInt("audit_target_tenant_id")
		if targetTenantID <= 0 {
			targetTenantID = tenantID
		}
		targetUserID := c.GetInt("audit_target_user_id")

		// 审计记录是企业合规数据，不能使用无确认的 goroutine（进程退出时会静默丢失）。
		auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// R2B 阴影观察（2026-10-03）：补齐租户上下文（已知租户）或 system bypass，
		// 否则 enforce 下审计写入会被 RLS 装饰器 fail-closed 拦下。
		if tenantID > 0 {
			auditCtx = tenantctx.WithTenantID(auditCtx, tenantID)
		} else {
			auditCtx = tenantctx.WithSystemBypass(auditCtx)
		}
		auditCreate := client.AuditLog.Create().
			SetCreatedAt(time.Now()).
			SetTenantID(tenantID).
			SetUserID(userID).
			SetRequestID(rid).
			SetIP(ip).
			SetPath(path).
			SetMethod(methodStr).
			SetStatusCode(status).
			SetResource(resource).
			SetAction(action).
			SetRequestBody(requestBody).
			SetSource(source)
		if targetTenantID > 0 {
			auditCreate = auditCreate.SetTargetTenantID(targetTenantID)
		}
		if targetUserID > 0 {
			auditCreate = auditCreate.SetTargetUserID(targetUserID)
		}
		if username != "" {
			auditCreate = auditCreate.SetActorAccount(username)
		}

		err := auditCreate.Exec(auditCtx)
		if err != nil && globalLogger != nil {
			globalLogger.Errorw("Failed to save audit log", "error", err)
		}

		// 记录结构化日志
		logFields := []interface{}{
			"audit_type", "user_action",
			"request_id", rid,
			"tenant_id", tenantID,
			"user_id", userID,
			"username", username,
			"action", action,
			"resource", resource,
			"path", path,
			"method", methodStr,
			"status_code", status,
			"client_ip", ip,
			"user_agent", userAgent,
			"latency_ms", duration.Milliseconds(),
			"success", status < 400,
			"sensitive", isSensitive,
			"audit_source", source,
			"target_tenant_id", targetTenantID,
			"target_user_id", targetUserID,
		}

		if globalLogger != nil {
			if isSensitive || status >= 400 {
				globalLogger.Warnw("Audit log - Sensitive/Failed operation", logFields...)
			} else {
				globalLogger.Infow("Audit log", logFields...)
			}
		}
	}
}

// auditSourceForRequest 派生本轮请求的租户上下文来源（§3.0-E 枚举；写死优先级，禁止调用方误传）。
func auditSourceForRequest(c *gin.Context) string {
	if v := c.GetString("audit_source"); v != "" {
		return v
	}
	if strings.Contains(c.Request.URL.Path, "/workbench") {
		return AuditSourceWorkbench
	}
	if c.GetString("tenant_source") == "switch" {
		return AuditSourceSwitch
	}
	if c.GetHeader("X-Customer-Tenant-ID") != "" || c.GetHeader("X-Tenant-Code") != "" {
		return AuditSourceHeader
	}
	if c.GetInt("user_id") > 0 {
		return AuditSourceLogin
	}
	return AuditSourceSystem
}

// shouldAuditRequest 判断是否需要审计请求
func shouldAuditRequest(c *gin.Context) bool {
	// 跳过健康检查和静态资源
	skipPaths := []string{
		"/health",
		"/metrics",
		"/favicon.ico",
		"/static/",
		"/assets/",
	}

	path := c.Request.URL.Path
	for _, skipPath := range skipPaths {
		if strings.HasPrefix(path, skipPath) {
			return false
		}
	}

	// 审计所有POST、PUT、DELETE请求
	method := c.Request.Method
	if method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE" {
		return true
	}

	// 审计敏感资源的GET请求
	for resource := range SensitiveResources {
		if strings.Contains(path, resource) {
			return true
		}
	}

	return false
}

// determineActionAndResource 确定操作类型和资源
func determineActionAndResource(c *gin.Context) (string, string, string) {
	method := c.Request.Method
	path := c.Request.URL.Path

	// 解析路径获取资源和ID
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	var resource, resourceID, action string

	// 基本资源识别
	if len(pathParts) >= 3 && pathParts[0] == "api" && pathParts[1] == "v1" {
		resource = pathParts[2]
		if len(pathParts) >= 4 && !isActionPath(pathParts[3]) {
			resourceID = pathParts[3]
		}
	}

	// 根据HTTP方法和路径确定操作
	switch method {
	case "GET":
		if resourceID != "" {
			action = string(ActionView)
		} else {
			action = string(ActionSearch)
		}
	case "POST":
		if strings.Contains(path, "login") {
			action = string(ActionLogin)
			resource = "auth"
		} else if strings.Contains(path, "logout") {
			action = string(ActionLogout)
			resource = "auth"
		} else if strings.Contains(path, "assign") {
			action = string(ActionAssign)
		} else if strings.Contains(path, "escalate") {
			action = string(ActionEscalate)
		} else if strings.Contains(path, "resolve") {
			action = string(ActionResolve)
		} else if strings.Contains(path, "close") {
			action = string(ActionClose)
		} else if strings.Contains(path, "reopen") {
			action = string(ActionReopen)
		} else if strings.Contains(path, "comment") {
			action = string(ActionComment)
		} else if strings.Contains(path, "attachment") {
			action = string(ActionAttachment)
		} else if strings.Contains(path, "export") {
			action = string(ActionExport)
		} else if strings.Contains(path, "import") {
			action = string(ActionImport)
		} else if strings.Contains(path, "submit") {
			action = string(ActionSubmit)
		} else if strings.Contains(path, "approve") {
			action = string(ActionApprove)
		} else if strings.Contains(path, "reject") {
			action = string(ActionReject)
		} else if strings.Contains(path, "schedule") {
			action = string(ActionSchedule)
		} else if strings.Contains(path, "/start") || strings.HasSuffix(path, "/start") {
			// 避免与 /api/v1/start* 等无意义路径误匹配；本项目变更 /start 才是生命周期动作
			action = string(ActionStart)
		} else if strings.Contains(path, "complete") {
			action = string(ActionComplete)
		} else if strings.Contains(path, "rollback") || strings.Contains(path, "rolled_back") {
			action = string(ActionRollback)
		} else if strings.Contains(path, "cancel") {
			action = string(ActionCancel)
		} else {
			action = string(ActionCreate)
		}
	case "PUT", "PATCH":
		action = string(ActionUpdate)
	case "DELETE":
		action = string(ActionDelete)
	default:
		action = strings.ToLower(method)
	}

	return action, resource, resourceID
}

// isActionPath 判断路径部分是否为操作而非资源ID
func isActionPath(pathPart string) bool {
	actionPaths := []string{
		"assign", "escalate", "resolve", "close", "reopen",
		"comment", "attachment", "export", "import",
		"search", "stats", "analytics", "batch",
		"submit", "approve", "reject", "schedule", "start", "complete",
		"rollback", "cancel",
	}

	for _, actionPath := range actionPaths {
		if pathPart == actionPath {
			return true
		}
	}

	return false
}

// isSensitiveOperation 判断是否为敏感操作
func isSensitiveOperation(action, resource string, statusCode int) bool {
	// 敏感操作类型
	sensitiveActions := map[string]bool{
		string(ActionDelete):        true,
		string(ActionPermission):    true,
		string(ActionConfiguration): true,
	}

	// 敏感资源操作
	if SensitiveResources[resource] {
		return true
	}

	// 敏感操作类型
	if sensitiveActions[action] {
		return true
	}

	// 失败的登录尝试
	if action == string(ActionLogin) && statusCode >= 400 {
		return true
	}

	return false
}
