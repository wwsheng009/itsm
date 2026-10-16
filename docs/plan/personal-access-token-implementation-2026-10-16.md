# 个人访问令牌（PAT）实施文档

> **Status: draft** ｜ 日期：2026-10-16 ｜ 定位：**实施文档（施工单，实施前需先过方案评审）**
> 关联：[方案文档](./personal-access-token-plan-2026-10-16.md) ｜ [验收标准](./personal-access-token-acceptance-2026-10-16.md) ｜ [ent 代码生成手册](../entc-codegen-runbook.md) ｜ [文档治理规范](../documentation-governance.md)
>
> 行号取自 2026-10-16 工作树；实施时以「文件 + 符号名」定位。

---

## 0. 摘要与实施范围

本批交付一条**端到端可用**的链路：用户在个人中心创建 PAT → 外部脚本用 `Authorization: Bearer itsm_pat_xxx` 创建工单 → 权限/租户/审计约束与用户名登录等价 → 使用历史可查。

**实施顺序（强依赖）**：`T1 数据层` → `T2 迁移/RLS` → `T3 认证分支` → `T4 领域服务` → `T5 路由装配` → `T6 审计归因` → `T7 历史读取` → `T8 安全事件接线` → `T9 前端` → `T10 审计页` → `T11 文档` → `T12 测试闭环`。

**写范围隔离**：T1/T2 只动 `ent/`、`migrations/`、`database/rls/`；T3 只动 `middleware/`；T4/T5 只动 `handlers/pat/`、`router/`、`internal/bootstrap/`；T6/T7 动 `middleware/audit.go`、`service/auditlog_service.go`、`dto/`；T9/T10 只动 `itsm-frontend/`。**同一文件不得被两个工单并行修改**。

---

## 1. 前置条件

| 项 | 要求 |
|---|---|
| Go 工具链 | 与仓库一致（`itsm-backend` 可 `go build ./...`） |
| 内存 | ent codegen 峰值 ~4.1GB（见 [runbook](../entc-codegen-runbook.md)）；**禁止用管道**，输出重定向到文件 |
| 数据库 | 本地 dev 或 238 演示库（`itsm_demo`）；RLS 验收需 enforce 档位 |
| 演示环境 | `http://172.18.3.238:8089`（独立栈 `itsm-demo`），账号见 [demo-environment §2](../multi-tenant/demo-environment.md)；部署通道见 [itsm-deploy SKILL](../../.agents/skills/itsm-deploy/SKILL.md) |
| **网络前置（2026-10-16 实测）** | 本工作站直连 238 的 `:8089` / `:22` **超时**；经 SOCKS5 `127.0.0.1:10808` 返回 **502**。端到端验收须在具备到 238 通道的执行机上运行（或在 238 本机执行） |
| 基线 | 先确认远端 demo 栈当前 tag（`1.8.0-dev.<sha>`），bundle 基线取**远端真实 HEAD** |

---

## 2. 工单总览

| # | 工单 | 优先级 | 依赖 | 写范围（advisory） | 估时 |
|---|---|---|---|---|---|
| T1 | ent schema `personal_access_tokens` + codegen | P0 | — | `itsm-backend/ent/schema/personal_access_token.go`、`itsm-backend/ent/**`（生成物） | 0.5d（含 5min codegen） |
| T2 | 迁移 + RLS 策略（新表策略、`audit_logs.token_id`、索引） | P0 | T1 | `itsm-backend/migrations/20261016_*`、`itsm-backend/database/rls/migrations/014_*` | 0.5d |
| T3 | 认证分支（`AuthMiddleware` + PAT 解析器 + 配置 + 装配） | P0 | T2 | `itsm-backend/middleware/pat_auth.go`、`middleware/auth.go`、`config/config.go`、`internal/bootstrap/app.go` | 1.5d |
| T4 | 领域切片 `handlers/pat/`（service/repository/handler/entity） | P0 | T2 | `itsm-backend/handlers/pat/**`、`itsm-backend/dto/pat_dto.go` | 2d |
| T5 | 路由注册 + 守卫豁免 + 显式 CSRF | P0 | T3,T4 | `itsm-backend/router/common_system_routes.go`、`router/route_permission_guard_test.go`、`router/pat_routes_test.go` | 0.5d |
| T6 | 审计归因（`source=pat`、`token_id`、使用行策略、节流） | P0 | T3,T5 | `itsm-backend/middleware/audit.go`、`middleware/pat_usage.go`、`ent/schema/auditlog.go` | 1d |
| T7 | 历史读取（令牌使用历史 + 本人安全事件 + 审计过滤） | P1 | T6 | `itsm-backend/handlers/pat/usages.go`、`service/auditlog_service.go`、`dto/auditlog_dto.go` | 1d |
| T8 | 安全事件批量吊销接线（改密/停用/删除/离职） | P0 | T4 | `service/user_service.go`、`service/tenant_user_admin.go`、`service/offboarding_service.go` | 0.5d |
| T9 | 前端：API 客户端 + 令牌面板 + 安全事件面板 + `/profile` 集成 | P0 | T5,T7 | `itsm-frontend/src/lib/api/personal-access-token-api.ts`、`src/pages/(main)/profile/**`、`src/lib/i18n/translations.ts` | 2d |
| T10 | 前端：审计页「来源」列与过滤 | P1 | T7 | `itsm-frontend/src/pages/(main)/audit-logs/index.tsx`、`src/lib/api/auditlog-api.ts` | 0.5d |
| T11 | 文档同步与运维交付（CHANGELOG / api-reference【**P0 硬依赖，未交付不放行**】/ env 示例 / gitleaks 规则 / CLI 说明） | P0 | T5 | `CHANGELOG.md`、`docs/api-reference.md`、`.env*.example`、CI 扫描配置、`itsm-cli/README.md` | 0.5d |
| T12 | 测试闭环 + 门禁全绿 | P0 | 全部 | 各包 `*_test.go` | 2d |
| T13 | 令牌行清理任务（housekeeping） | P2（建议随批） | T4 | `service/pat_housekeeping.go`、`internal/bootstrap/app.go`（挂 job） | 0.5d |

合计约 **13.5 人日**（不含 238 部署与端到端验收执行时间）。

**T13 说明**：后台定期任务清理「`expires_at` 过期且超过 180 天」的令牌行（复用 `expires_at` 索引；仿 `service/attachment_cleanup.go` 的保留期模式）；吊销/轮换血缘已由 `audit_logs` 承载，删行不损审计。清理属 P2，可与 T8 合并实施，但不做则表无限膨胀（Q8 已登记）。

---

## 3. 详细实施步骤

### T1 数据层：ent schema + codegen

**新增** `itsm-backend/ent/schema/personal_access_token.go`：

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PersonalAccessToken 个人访问令牌（PAT）。
// 安全约定：库中只存 sha256(明文) 与展示用前缀，明文永不落库；
// 权限不冻结在本表，鉴权时按 user_id 实时解析（见 middleware/rbac.go）。
type PersonalAccessToken struct {
	ent.Schema
}

func (PersonalAccessToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").Positive().Comment("归属用户ID"),
		field.Int("tenant_id").Positive().Comment("归属租户ID（RLS 键）"),
		field.String("name").NotEmpty().MaxLen(64).Comment("用户可读名称"),
		field.String("description").Optional().MaxLen(255),
		field.String("token_hash").NotEmpty().MaxLen(64).Unique().Comment("hex(sha256(明文))；明文不落库"),
		field.String("token_prefix").NotEmpty().MaxLen(24).Comment("明文前 17 字符（itsm_pat_+8位），仅用于展示；maskedToken=prefix+****，不含真实后缀"),
		field.Time("expires_at").Comment("过期时间（必填，默认 90 天，上限 365 天）"),
		field.Time("last_used_at").Optional().Nillable(),
		field.String("last_used_ip").Optional().MaxLen(64),
		field.Int("use_count").Default(0),
		field.Time("revoked_at").Optional().Nillable().Comment("非空即失效"),
		field.String("revoked_reason").Optional().MaxLen(32).Comment("self|rotated|revoke_all|security_event"),
		field.Int("rotated_to_token_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PersonalAccessToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Fields("user_id", "revoked_at"),
		index.Fields("tenant_id", "revoked_at"),
		index.Fields("expires_at"),
	}
}
```

**同时修改** `itsm-backend/ent/schema/auditlog.go`（在 `source` 之后追加）：

```go
field.Int("token_id").Optional().Comment("PAT 归因（source=pat 时非空）；NULL=非令牌请求"),
```

**生成**（见 [runbook](../entc-codegen-runbook.md)）：

```bash
cd itsm-backend
go build -o /tmp/entc entgo.io/ent/cmd/ent
cd ent && ( /usr/bin/time -l /tmp/entc generate ./schema ) > /tmp/entc.log 2>&1   # 禁止管道
cd .. && go build ./...
git status --porcelain -- ent | wc -l    # 只应有新实体相关文件变动
```

**完成定义（DoD）**：`go build ./...` 通过；`ent/personalaccesstoken/` 与 `ent/auditlog` 生成物就位；未产生无关 diff。

---

### T2 迁移与 RLS

**新增** `itsm-backend/migrations/20261016_personal_access_tokens.sql`（幂等范式照抄 `migrations/20261014f_ticket_type_distribution.sql`）：

```sql
-- 20261016 个人访问令牌（PAT）：
--   1) audit_logs 增加 token_id 归因列与索引（列由 ent Schema.Create 先行创建，本文件为幂等兜底）
--   2) 索引：令牌来源过滤与按令牌检索
DO $$
BEGIN
  IF to_regclass('public.audit_logs') IS NULL THEN
    RAISE NOTICE 'skip 20261016: audit_logs not found';
  ELSE
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='audit_logs' AND column_name='token_id') THEN
      ALTER TABLE audit_logs ADD COLUMN token_id INTEGER;
    END IF;
    CREATE INDEX IF NOT EXISTS idx_audit_token ON audit_logs (tenant_id, token_id, created_at);
    CREATE INDEX IF NOT EXISTS idx_audit_source_created ON audit_logs (tenant_id, source, created_at);
  END IF;
END $$;
```

配套 `..._down.sql`：`DROP INDEX IF EXISTS idx_audit_source_created; DROP INDEX IF EXISTS idx_audit_token; ALTER TABLE audit_logs DROP COLUMN IF EXISTS token_id;`

**新增 RLS** `itsm-backend/database/rls/migrations/014_personal_access_tokens_policies.sql`（照抄 `013_ticket_type_distribution_policies.sql` 形状：`get_current_tenant_id()` + `ENABLE`（**不加 FORCE**）+ `USING/WITH CHECK`），配套 `_rollback.sql`（`DROP POLICY IF EXISTS`）。

**应用与验证**：

```bash
cd itsm-backend
go run -tags rlsapply ./cmd/rls-apply -files 014_personal_access_tokens_policies.sql
go run -tags rlsapply ./cmd/rls-apply -verify
```

**DoD**：策略存在；enforce 档位下「带租户上下文可读写、无租户上下文不可见」双向验证通过（见 T12 的 RLS 集成测试）。

> **注意**：`tests/multi_tenant/rls_matrix_test.go` 的 17 张表清单是 sqlite 契约测试；新增表是否需登记进该矩阵，**实施前向 RLS owner 确认**（不确定项 U-1）。

---

### T3 认证分支

**新增** `itsm-backend/middleware/pat_auth.go`：

```go
package middleware

// PAT（Personal Access Token）认证分支。
// 与 JWT 共用身份上下文契约：user_id / username / role / tenant_id / token。
// 配置方式与 ConfigureAccessTokenRevocationRedis 一致（包级单例 + 显式装配）。

const (
	PATAuthTokenPrefix   = "itsm_pat_"
	PATAuthSourcePAT     = "pat"
	ctxKeyAuthTokenID    = "auth_token_id"
	ctxKeyAuthTokenPref  = "auth_token_prefix"
)

type PATAuthenticator interface {
	// Resolve 校验明文令牌并返回已解析身份；失败一律返回 error（不区分原因，防枚举）。
	Resolve(ctx context.Context, plaintext string) (*PATIdentity, error)
}

type PATIdentity struct {
	TokenID     int
	TokenPrefix string
	UserID      int
	Username    string
	Role        string // 已过 EffectiveTokenRole
	TenantID    int
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

// tryAuthenticatePAT 返回 (identity, handled)；handled=false 表示"不是 PAT 串"，交回 JWT 分支。
func tryAuthenticatePAT(c *gin.Context, tokenString string) (*PATIdentity, bool)
```

**修改** `itsm-backend/middleware/auth.go`：在 `auth.go:188`（JWT 解析之前）插入：

```go
// —— PAT 分支：必须在 JWT 解析之前（否则非 JWT 串会先被判 401）——
if strings.HasPrefix(tokenString, PATAuthTokenPrefix) {
	identity, handled := tryAuthenticatePAT(c, tokenString)
	if !handled { // 未启用：按无效令牌处理（fail-closed）
		common.Fail(c, common.AuthFailedCode, "token无效")
		c.Abort()
		return
	}
	if identity == nil {
		zap.S().Warnw("AuthMiddleware: invalid personal access token",
			"path", c.Request.URL.Path, "token_prefix", safePrefix(tokenString))
		common.Fail(c, common.AuthFailedCode, "token无效") // 不区分「不存在/已吊销/已过期」
		c.Abort()
		return
	}
	// 审计归因：必须在 AuditMiddleware 之前写入（读取点 middleware/audit.go:339-356）
	c.Set("audit_source", PATAuthSourcePAT)
	c.Set(ctxKeyAuthTokenID, identity.TokenID)
	c.Set(ctxKeyAuthTokenPref, identity.TokenPrefix)
	c.Set("user_id", identity.UserID)
	c.Set("username", identity.Username)
	c.Set("role", identity.Role)
	c.Set("tenant_id", identity.TenantID)
	c.Set("token", tokenString)
	c.Set("token_type", "pat")
	c.Next()
	return
}
```

**约束**：新增日志**不得**打印 `tokenString`（只允许前缀）；复用 `safePrefix`（新增小工具，取前 16 字符）。

**新增** `itsm-backend/handlers/pat/repository_impl.go` 中的解析实现（与 T4 同包），关键查询：

```go
// Resolve 认证期点查：租户上下文尚未建立（认证先于租户解析，TenantMiddleware 在
// AuthMiddleware 之后才挂载，见 router.go:396 vs :499）。
// RLS enforce 档位下，无租户上下文的语句会被驱动 fail-closed 拒绝（database/rls/driver.go
// 「enforce mode requires tenant_id in context」），且 IsSystemBypass 是唯一的豁免通道；
// 因此这里使用**带审计的** SystemContext（每次调用输出 zap 日志，可追踪），而不是裸 WithSystemBypass。
// 同类先例：bootstrap_tokens 同样在租户上下文建立前校验一次性令牌（RLS 010 已对其启用策略）。
// 查询本身按 token_hash 唯一索引点查，不做跨租户扫描，旁路范围 = 仅这一条认证查询。
func (r *patResolver) Resolve(ctx context.Context, plaintext string) (*middleware.PATIdentity, error) {
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])

	qctx := tenantctx.SystemContext(ctx, "pat_auth:resolve", "authenticate PAT before tenant resolution")
	row, err := r.client.PersonalAccessToken.Query().
		Where(personalaccesstoken.TokenHashEQ(hash)).
		Only(qctx)
	if err != nil { return nil, err }                       // 不存在 → 统一错误
	if row.RevokedAt != nil { return nil, ErrPATRevoked }
	if !row.ExpiresAt.After(time.Now()) { return nil, ErrPATExpired }

	u, err := r.client.User.Get(qctx, row.UserID)           // 用户不存在 → 401
	if err != nil { return nil, err }
	if !u.Active { return nil, ErrPATUserInactive }         // 停用 → 认证层 401（兜底，防 R3 批量吊销竞态；等价于 JWT 停用即 401）
	role := middleware.EffectiveTokenRole(u.Role, u.MSPRole)
	return &middleware.PATIdentity{...}, nil
}
```

**装配** `itsm-backend/internal/bootstrap/app.go`（`ConfigureAccessTokenRevocationRedis` 邻近）：

```go
patResolver := pat.NewResolver(client)
middleware.ConfigurePersonalAccessTokens(patResolver, cfg.PAT.Enabled)
```

**配置** `itsm-backend/config/config.go`：新增 `PAT PATConfig` 段（`mapstructure:"pat"`）+ 环境变量读取：

| 字段 | env | 默认 |
|---|---|---|
| `Enabled` | `PAT_ENABLED` | `true`（env 文件首轮置 `false`，见 §5 灰度） |
| `DefaultTTLDays` | `PAT_DEFAULT_TTL_DAYS` | `90` |
| `MaxTTLDays` | `PAT_MAX_TTL_DAYS` | `365` |
| `MaxActivePerUser` | `PAT_MAX_ACTIVE_PER_USER` | `20` |
| `UsageAuditMode` | `PAT_USAGE_AUDIT_MODE` | `dedupe` |
| `UsageAuditWindowSeconds` | `PAT_USAGE_AUDIT_WINDOW_SECONDS` | `60` |
| `RateLimitPerMinute` | `PAT_RATE_LIMIT_PER_MINUTE` | `120`（单令牌限流，复用 Redis 闸门；无 Redis 退化内存计数） |

**新增** `middleware.RejectPATAuth()`（D-PAT-13，防自我繁殖）：

```go
// RejectPATAuth 拒绝以 PAT 认证的身份调用（用于 PAT 管理面写路由）。
// 泄漏的令牌不得创建/轮换/吊销其它令牌；管理面写操作仅限浏览器会话。
func RejectPATAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("token_type") == "pat" {
			common.Fail(c, common.ForbiddenCode, "请使用浏览器会话管理访问令牌")
			c.Abort()
			return
		}
		c.Next()
	}
}
```

**DoD**：`PAT_ENABLED=false` 时携带 PAT 的请求 401 且管理面 404；`true` 时 PAT 请求的上下文键与 JWT 逐键一致（单测断言）。

---

### T4 领域切片 `handlers/pat/`

目录（遵循 `handlers/<domain>/` 目标架构）：

```
itsm-backend/handlers/pat/
├── entity.go           # 领域实体与 DTO 映射（camelCase）
├── service.go          # 创建/轮换/吊销/查询 业务规则与事务边界
├── repository.go       # 仓储接口
├── repository_impl.go  # ent 实现（含 Resolve 认证解析器）
└── handler.go          # HTTP 层（绑定/校验/调 service/返回 DTO）
```

**核心业务规则（service）**：

| 操作 | 规则 |
|---|---|
| Create | 校验 `name` 必填 ≤64；`expiresInDays` ∈ [1, MaxTTLDays]（缺省 DefaultTTLDays）；`expiresAt` 与 `expiresInDays` 互斥；校验 active 数 < MaxActivePerUser（否则 422，即 HTTP 422 / `code=4220`，`common.UnprocessableEntityCode`）；**平台角色拒绝**：`middleware.IsPlatformRole(user.Role)`（super_admin/sysadmin）→ 403（D-PAT-14，365 天全权限旁路凭据不予发放）；生成明文 → 存 hash+prefix；**返回明文仅此一次**；写审计 `auth.token_created` |
| Rotate | 仅本人 + 仅 active；新令牌继承 `name`/`description`/剩余 `expires_at`；`gracePeriodHours` ∈ {0,24}；`0` → 旧令牌立即 `revoked_at=now, reason=rotated`；`24` → 旧令牌 `expires_at=min(旧, now+24h)` 且 `rotated_to_token_id=新ID`；写审计 `auth.token_rotated` |
| Revoke | 仅本人；幂等（已吊销再调返回当前状态，不报错）；写审计 `auth.token_revoked` |
| RevokeAll | 仅本人；批量置 `revoked_at=now, reason=revoke_all`；返回 `revokedCount`；写审计 `auth.token_revoked_all` |
| List | 仅本人；按 `created_at DESC`；**不返回 hash**；`status` 由 `revoked_at`/`expires_at` 派生 |
| Get(id) | 非本人 → **404**（防枚举） |

**已知竞态（接受并文档化）**：① MaxActive 为 check-then-insert，并发创建可短暂超限（可接受，下次创建时被拒）；② RevokeAll 与并发 Create 之间创建的令牌会存活（无 created_at 条件），如需严格语义可在吊销时附加 `created_at < revoke 请求时间` 条件；③ 到期判定用应用服务器时钟，与 DB 时钟偏差可忽略。

**事务与租户**：**注意——这些路由不能使用 `middleware.GetTenantID` / `TenantIDOrUnauthorized`**（它们依赖 `TenantMiddleware` 写入的 `TenantContext`，而 `authGrp` 只挂逐路由 `AuthMiddleware`，无 TenantMiddleware）。租户取值一律用 `c.GetInt("tenant_id")`（由 AuthMiddleware 从 PAT 身份 / JWT claims 写入，`middleware/auth.go:272`）。所有查询使用 `tenantctx.WithTenantID(ctx, tenantID)` 并附加 `user_id` 过滤；`row.TenantID != tenantID` 视为 404。

**DoD**：service 单测覆盖创建/上限/过期边界/轮换两档/幂等吊销/越权 404；**断言落库无明文**（仿 `handlers/ai/llm_provider_admin_test.go:261-270`）。

---

### T5 路由与守卫

> **注册方式（重要）**：必须**直接在 `authGrp` 上用完整子路径注册**，不要建子 Group——守卫扫描器以「路由调用的原始 path 参数」构造豁免键（`route_permission_guard_test.go:223-235`），子 Group 的 `pat.POST("", ...)` 会产生空 path，键无法对齐。gin 支持静态段与参数段同级共存（既有先例：`ticket_routes.go` 中 `/views` 与 `/:id`），故 `/revoke-all` 与 `/:id/revoke` 可并存。

**修改** `itsm-backend/router/common_system_routes.go`（`authGrp` 块内，紧跟 `change-password`）：

```go
if config.PATHandler != nil {
    // 自省面：仅需已认证身份 + 本人作用域；浏览器会话必须过 CSRF（Bearer 调用自动跳过）。
    patAuth := middleware.AuthMiddleware(config.JWTSecret)
    patCSRF := func() gin.HandlerFunc { return middleware.CSRFProtectionMiddleware(middleware.DefaultCSRFConfig()) }
    authGrp.GET("/personal-access-tokens", patAuth, config.PATHandler.List)
    authGrp.POST("/personal-access-tokens", patAuth, config.PATHandler.Create)                       // CSRF 按下方条件挂
    authGrp.POST("/personal-access-tokens/revoke-all", patAuth, config.PATHandler.RevokeAll)
    authGrp.POST("/personal-access-tokens/:id/rotate", patAuth, config.PATHandler.Rotate)
    authGrp.POST("/personal-access-tokens/:id/revoke", patAuth, config.PATHandler.Revoke)
    authGrp.GET("/personal-access-tokens/:id/usages", patAuth, config.PATHandler.Usages)
    authGrp.GET("/security-events", patAuth, config.PATHandler.SecurityEvents)
}
```

> CSRF 需按 `config.CSRFEnabled` 条件挂载（与 `router.go:400-403` 同口径）：`CSRFEnabled=true` 时，上述每个 POST 在 `patAuth` 之后追加 `patCSRF()`。实施时以 `if config.CSRFEnabled` 分支拼中间件链，**不要**在 CSRF 关闭时也挂（会引入 token 获取依赖）。

**RouterConfig**：新增 `PATHandler *pat.Handler`（`router/router.go:128-257`），并在 `internal/bootstrap/app.go` 装配。

**守卫豁免**（`router/route_permission_guard_test.go:27-50`，与 `change-password` / `me/preferences` 同组；键 = `common_system_routes.go` + 方法 + **原始 path 参数**，必须写理由）：

```go
"common_system_routes.go POST /personal-access-tokens":            "个人访问令牌自助创建：授权依据=已认证身份 + 仅操作本人令牌；凭据发放动作不属资源动作权限",
"common_system_routes.go POST /personal-access-tokens/revoke-all": "个人访问令牌全部吊销：同上（仅本人令牌）",
"common_system_routes.go POST /personal-access-tokens/:id/rotate": "个人访问令牌自助轮换：同上（仅本人令牌）",
"common_system_routes.go POST /personal-access-tokens/:id/revoke": "个人访问令牌自助吊销：同上（仅本人令牌）",
```

**PAT 管理面写路由防自我繁殖（D-PAT-13，必做）**：新增 `middleware.RejectPATAuth()`（读取 `c.GetString("token_type")`，`pat` → 403「请使用浏览器会话管理令牌」），按 `config.CSRFEnabled` 同样的条件方式追加在 4 个写路由的 `patAuth` 之后。读路由（list/usages/security-events）**不**挂载。配套测试：T12 守卫/路由测试断言「PAT 调 create/rotate/revoke/revoke-all 一律 403，且未产生任何令牌变更」。

**不需要** `go run ./cmd/authz-gen`（未新增 `RequirePermission`）；**不新增权限码**（见 D-PAT-10）。

**DoD**：`go test ./router/... -run 'TestWriteRoutesRequirePermission|TestRoutePrecheck'` 通过。

---

### T6 审计归因与使用行

**修改** `itsm-backend/middleware/audit.go`：

1. 新增常量：`AuditSourcePAT = "pat"`（`:27-35` 附近）。
2. `AuditMiddleware`（`:191-336`）写行时补 `SetTokenID(c.GetInt(ctxKeyAuthTokenID))`（非 PAT 请求为 0 → 不设置）。
3. 触发条件：`shouldAuditRequest`（`:359`）在 `c.GetString("audit_source") == AuditSourcePAT` 时**返回 true**（PAT 请求一律留痕），再交由去重闸门决定是否真正落库。

**新增** `itsm-backend/middleware/pat_usage.go`：Redis 闸门（无 Redis 时退化为内存 map，与 `token_revocation.go` 的内存实现同风格）

```go
// AllowUsageRecord 60s 窗口去重：同一 (tokenId, method, path) 只写一次使用行。
func AllowUsageRecord(ctx context.Context, tokenID int, method, path string) bool

// ShouldTouchLastUsed 同一令牌 60s 最多更新一次 last_used_at/use_count。
func ShouldTouchLastUsed(ctx context.Context, tokenID int) bool
```

**使用行写入**（在 `AuditMiddleware` 内，落库前后按 `cfg.PAT.UsageAuditMode` 分支）：

| 模式 | 行为 |
|---|---|
| `dedupe` | 通过 `AllowUsageRecord` 才写；`action='auth.token_used'`、`resource='auth'`、`source='pat'`、`token_id` |
| `all` | 每请求写 |
| `off` | 仅原有敏感操作规则生效 |

**令牌自省更新**：在 PAT 分支成功后 best-effort 更新（`ShouldTouchLastUsed` 通过时）：`use_count+1`、`last_used_at=now`、`last_used_ip=c.ClientIP()`；**失败只记 warn，不影响请求**。

**DoD**：单测断言 ① `source='pat'`；② `token_id` 非空；③ 60s 内重复同路径只产生 1 行；④ 审计 `request_body` 与日志中**不含明文令牌**。

---

### T7 历史读取

- `GET /api/v1/auth/personal-access-tokens/:id/usages`：本人令牌；查 `audit_logs`（`tenant_id` + `token_id`），分页（pageSize 默认 20，上限 100），返回 `summary{useCount,lastUsedAt,lastUsedIp,firstUsedAt}`。
- `GET /api/v1/auth/security-events?kind=all|login|token`：本人 `resource='auth'` 的审计行（登录/切换/改密/令牌事件），按 `created_at DESC` 分页（**pageSize 默认 20，上限 100，与 usages 同口径**）；**不需要 `audit:read`**（自作用域）。
- `GET /api/v1/audit-logs`：`service/auditlog_service.go` 增加 `tokenId` 过滤；`dto/auditlog_dto.go` 响应增加 `tokenId`（`source` 已存在）。
- **索引依赖**：`idx_audit_token`、`idx_audit_source_created`（T2）。

**DoD**：三个读取端点的分页/过滤单测通过；本人作用域外的 `userId` 无法越权读取（构造他人 `tokenId` → 空集）。

---

### T8 安全事件批量吊销接线

在既有失效调用点**并列**新增 PAT 批量吊销（保持 JWT 行为不变）：

| 位置 | 事件 | 追加调用 |
|---|---|---|
| `service/user_service.go:539-546` | 角色变更 | **不吊销**（D-PAT-6/R4）——仅登记注释说明原因 |
| `service/user_service.go:563-595` | 删除用户 | `RevokeAllForUser(ctx, id, "security_event")` |
| `service/user_service.go:629-634` | 停用账户 | 同上 |
| `service/user_service.go:669-670` | 重置密码 | 同上 |
| `service/user_service.go:777-778` | 批量停用 | 同上 |
| `service/tenant_user_admin.go:697` | 租户侧停用用户 | 同上 |
| `service/offboarding_service.go:102` | 离职编排 | 同上 |

**DoD**：单测断言「改密后旧 PAT 401 / 角色变更后旧 PAT 仍可用但权限按新角色判定」。

---

### T9 前端实施

1. **新增** `src/lib/api/personal-access-token-api.ts`：7 个端点；camelCase；`httpClient.get/post`；`baseURL='/api/v1/auth/personal-access-tokens'`；`securityEvents` 走 `/api/v1/auth/security-events`。并在 `src/lib/api/index.ts` 导出。
2. **新增** `src/pages/(main)/profile/components/AccessTokenPanel.tsx`：
   - 表格列：名称 / 令牌（`maskedToken` 等宽字体）/ 状态 `<Tag>`（有效/已吊销/已过期）/ 最后使用（`lastUsedAt`+`lastUsedIp`，空显示「从未使用」）/ 有效期 / 操作；
   - 操作：`使用历史`（抽屉，读 `usages`）、`轮换`（弹窗选宽限期）、`吊销`（`Popconfirm`，`okText="确定吊销"`）、页头 `吊销全部`；
   - `api` prop 依赖注入（便于 Jest 测试，仿 `InvitationManagementModal`）；
   - 空态 `<Empty description="暂无访问令牌" />`；错误态复用页面 `message` 提示。
3. **新增** `CreateAccessTokenModal.tsx`：名称 + 有效期（7/30/90/180/365 天单选 + 自定义）+ 风险提示；提交成功后切换为**一次性明文视图**（`Alert`「仅显示一次，关闭后无法再次查看」+ 复制按钮 + 「我已保存」勾选后方可关闭）+ 一段可直接复制的 curl 使用示例（`curl -H "Authorization: Bearer itsm_pat_..."`，指向 api-reference）。
   **明文生命周期硬约束**：明文只存在于该弹窗的**局部组件 state**；禁止写入 React Query 缓存、Zustand/persist store、`localStorage`/`sessionStorage`、错误上报面包屑（Sentry/console 的对象捕获）。关闭弹窗即从 state 清除。**测试断言**：弹窗关闭后 query cache 与两个 storage 中均检索不到明文（Jest）。
4. **新增** `SecurityEventsPanel.tsx`：合并展示本人登录/切换/令牌事件（来源标签、IP、时间、结果）；替换 `profile/index.tsx:869-877` 的 disabled「登录历史」。
5. **修改** `profile/index.tsx`：`security` Tab（`:787-881`）新增「访问令牌」`<Card>` 行 + 面板；`useI18n` 已导入但未使用，新文案一律走 `t()`。
6. **修改** `src/lib/i18n/translations.ts`：`profile.accessTokens.*`、`profile.securityEvents.*`（zh-CN + en-US 两侧）。**注意**：全仓无 zh/en 键位对齐门禁（`useI18n.ts:37-44` 缺失 en-US 键时静默回退中文）→ 需人工核对两侧，并仿 `pages/(main)/admin/permissions/__tests__/permissions-i18n.test.ts` 为新命名空间加一个存在性测试。
7. **Hook 规范（CLAUDE.md Frontend Hook Patterns，强制）**：面板内异步获取必须满足 **Pattern A**（`requestIdRef` 守卫，防 stale response 覆盖）、**Pattern C**（deps 纪律）、**Pattern F**（i18n 文案经 ref 读取）；列表/事件面板含 ≥3 个单测（happy/partial/failure）+ requestId race 回归测试（两次慢速响应，先旧后新，断言显示新数据）。注意 `profile/index.tsx` 现状是裸 `useState`+`useEffect`（无 React Query、无守卫，`index.tsx:171-176`），新组件**不要**复制这个裸模式。
8. **测试**：`src/pages/(main)/profile/components/__tests__/AccessTokenPanel.test.tsx`（列表渲染 / 创建 → 明文一次性 / 吊销 → 重取 / 复制 / 明文生命周期断言）、`src/lib/api/__tests__/personal-access-token-api.test.ts`（路径与载荷断言）。

**不做**：不改 `http-client.ts` 的鉴权/CSRF/刷新逻辑；不改 `token-storage.ts`；不新增路由与菜单。

**DoD**：`npm run type-check` + `npm run lint:check` + 新增 Jest 用例通过。

---

### T10 前端审计页

- `src/lib/api/auditlog-api.ts`：`AuditLog` 增加 `source`、`actorAccount`、`tokenId`；`ListAuditLogsParams` 增加 `source`、`tokenId`。
- `src/pages/(main)/audit-logs/index.tsx`：新增「来源」列（标签映射复用 `src/lib/api/msp-audit-api.ts:61-70` 的文案，建议抽到共享模块）+ 「来源」下拉过滤（含「令牌(PAT)」）+ 「令牌ID」列。

**DoD**：来源过滤返回 PAT 行；标签正确显示「令牌」。

---

### T11 文档同步与运维交付（同批次提交）

| 文档 | 内容 |
|---|---|
| `CHANGELOG.md` `[Unreleased]` | 用户视角条目：新增个人访问令牌（创建/轮换/吊销/使用历史），支持以 `Authorization: Bearer` 调用 API |
| `docs/api-reference.md` | 新增「个人访问令牌」章节：7 个端点、请求/响应示例、错误码（**401 统一「token无效」为防枚举设计——排障请到个人中心核对令牌状态，无法从响应区分过期/吊销/不存在**）、`expiresInDays` 上限、单令牌限流、**安全警示：令牌仅接受 Authorization 头，禁止放入 URL query（会进入访问日志）**、**CORS 说明：面向服务端/CLI 调用；浏览器内 JS 集成需配置 CORS 白名单（默认不发 Allow-Origin）**、PAT 可调用面矩阵（含 logout/refresh/switch-tenant 行为）、管理员调查指引（审计页 `source=pat` + tokenId） |
| `itsm-cli/README.md` | 说明可用 PAT 作为长期凭据（`Authorization: Bearer`） |
| `.env.example` / `.env.demo.example` | 增补 §3.12 全部 PAT_* 键（含注释：生产首轮 `PAT_ENABLED=false`，验收后开启） |
| CI 密钥扫描 | 新增 gitleaks/trivy 自定义规则：`itsm_pat_[A-Za-z0-9_=-]{43,}`（防令牌误入库），并验证对示例串误报为 0 |
| 本文三件套 | 实施完成后把「方案/实施/验收」状态由 `draft` 更新为 `current`，并补验收证据链接 |
| `ROADMAP.md` | 若纳入 v1.7 迭代则登记一条收敛项（否则不动，避免路线图噪音） |

---

### T12 测试闭环

**后端**

| 测试 | 文件 | 断言 |
|---|---|---|
| 认证分支 | `middleware/pat_auth_test.go` | 合法 PAT 上下文 5 键与 JWT 一致；未启用 → 401；已吊销/过期/停用 → 401；日志不含明文；**query/body 传令牌一律无效**（负向） |
| 防自我繁殖 | `middleware/pat_auth_test.go` + `router/pat_routes_test.go` | `token_type=pat` 调 4 个写路由 → 403 且零副作用；读路由正常 |
| 平台角色禁令 | `handlers/pat/service_test.go` | super_admin/sysadmin 创建 → 403（D-PAT-14） |
| 单令牌限流 | `middleware/pat_usage_test.go` | 超过 `PAT_RATE_LIMIT_PER_MINUTE` → 429；Redis 故障退化内存计数 |
| 解析器与 RLS | `handlers/pat/repository_rls_test.go`（Postgres 环境门控） | enforce 档位下 `SystemContext` 点查成功 |
| 服务规则 | `handlers/pat/service_test.go` | 上限 4220、有效期边界、轮换两档、吊销幂等、越权 404、平台角色 403、落库无明文 |
| 使用历史 | `handlers/pat/usages_test.go` | 分页、汇总、越权空集 |
| 审计归因 | `middleware/audit_test.go`（扩展） | `source=pat`、`token_id`、60s 去重 |
| 端到端 | `router/pat_routes_test.go` | 会话创建 PAT → 用 PAT `POST /api/v1/tickets` 201/200 → PAT 调管理面写 403 → 吊销后 401 → 跨租户 header 401 |
| logout 无害性 | `router/pat_routes_test.go` | PAT 调 `/auth/logout` → 401 且令牌状态不变（无吊销副作用） |
| 守卫 | 既有守卫测试 | 全绿 |

**前端**：见 T9/T10。

**必须运行的命令**（详见 §7）。

---

## 4. 代码骨架（关键片段汇总）

- 认证分支：见 T3。
- 解析器：见 T3。
- 领域服务签名：

```go
type Service struct {
	repo   Repository
	cfg    PATConfigView   // Enabled/DefaultTTLDays/MaxTTLDays/MaxActivePerUser
	logger *zap.SugaredLogger
}

func (s *Service) Create(ctx context.Context, userID, tenantID int, in CreateInput) (*CreateResult, error)
func (s *Service) List(ctx context.Context, userID, tenantID int) ([]*Item, error)
func (s *Service) Rotate(ctx context.Context, userID, tenantID, tokenID, graceHours int) (*CreateResult, error)
func (s *Service) Revoke(ctx context.Context, userID, tenantID, tokenID int) (*Item, error)
func (s *Service) RevokeAll(ctx context.Context, userID, tenantID int, reason string) (int, error)
func (s *Service) Usages(ctx context.Context, userID, tenantID, tokenID, page, pageSize int) (*UsagePage, error)
func (s *Service) SecurityEvents(ctx context.Context, userID, tenantID, page, pageSize int, kind string) (*EventPage, error)
```

- handler 统一模式：`middleware.UserIDOrUnauthorized` + `c.GetInt("tenant_id")`（**不可用** `middleware.GetTenantID`——authGrp 无 TenantMiddleware）→ 调 service → `common.Success(c, dto)`；错误经 `common.Fail(c, code, msg)`（参数 400 / 上限 422 / 越权 404）。

---

## 5. 迁移、回滚与灰度

**上线顺序**：① 迁移 + RLS（幂等）→ ② 后端（`PAT_ENABLED=false` 灰度）→ ③ 前端 → ④ 打开 `PAT_ENABLED=true` → ⑤ 演示环境端到端验收。

**回滚**：
1. **首选**：`PAT_ENABLED=false` + 重启后端（认证分支与管理面同时失效，无需回滚代码）。
2. 数据回滚：`migrations/20261016_..._down.sql`（删索引与 `token_id` 列）。**注意**：down.sql 仅在**同时回滚 ent schema 变更**时语义成立——`ent/schema/auditlog.go` 的 `token_id` 字段未回滚时，下次启动 `Schema.Create` 会把该列重新加回（幂等无害，但回滚叙述需知悉；`internal/bootstrap/app.go:1672-1677` 顺序为 ent 先建、SQL 后补）。**已产生的令牌行保留**，避免误删审计线索。
3. RLS 回滚：`014_..._rollback.sql`（删策略；表保留）。
4. 表回滚：默认**不删表**（审计与血缘需要）；确需删除时人工执行 `DROP TABLE personal_access_tokens`。

**灰度**：`PAT_ENABLED` 默认 `true` 之前，建议先在 238 demo 打开并完成验收，再决定生产默认值（生产建议首轮 `false`，验证后开启）。

---

## 6. 验证命令清单（实施者与验收者共用）

```bash
# 后端：编译 + 目标包测试 + 全量
cd itsm-backend && go build ./... && go vet ./middleware/... ./handlers/pat/... ./router/...
go test ./middleware/... ./handlers/pat/... ./service/... -count=1
go test ./router/... -count=1
go test ./... 2>&1 | tail -40            # 全量（耗时较长，建议后台）

# 门禁
cd itsm-backend && go test ./middleware/... -run 'TestPrecheckFreshness|TestRoutePrecheckAlignment' -count=1
go test ./router/... -run 'TestWriteRoutesRequirePermission|TestPermissionCodeCatalog' -count=1

# 迁移与 RLS
cd itsm-backend && go run -tags rlsapply ./cmd/rls-apply -files 014_personal_access_tokens_policies.sql
go run -tags rlsapply ./cmd/rls-apply -verify

# 前端
cd itsm-frontend && npm run type-check && npm run lint:check
npm run test:unit -- --testPathPattern 'personal-access-token|AccessTokenPanel|auditlog'

# 文档门禁（advisory）
make docs-gate
```

---

## 7. 交付物清单

| 类别 | 交付物 |
|---|---|
| 后端 | `ent/schema/personal_access_token.go`、`ent/schema/auditlog.go`（改）、`middleware/pat_auth.go`、`middleware/pat_usage.go`、`middleware/auth.go`（改）、`middleware/audit.go`（改）、`handlers/pat/**`、`dto/pat_dto.go`、`router/common_system_routes.go`（改）、`router/route_permission_guard_test.go`（改）、`config/config.go`（改）、`internal/bootstrap/app.go`（改）、`migrations/20261016_*`、`database/rls/migrations/014_*` |
| 前端 | `src/lib/api/personal-access-token-api.ts`、`src/pages/(main)/profile/components/{AccessTokenPanel,CreateAccessTokenModal,SecurityEventsPanel}.tsx`、`src/pages/(main)/profile/index.tsx`（改）、`src/pages/(main)/audit-logs/index.tsx`（改）、`src/lib/api/auditlog-api.ts`（改）、`src/lib/i18n/translations.ts`（改） |
| 文档 | 本三件套（状态更新）、`CHANGELOG.md`、`docs/api-reference.md`、`itsm-cli/README.md` |
| 证据 | 238 demo 部署证据（`.dev/deploy/DEMO-*-EVIDENCE.md`）+ 端到端验收记录（见验收标准 §5 模板） |

---

## 8. 实施纪律（每工单完成定义）

1. **先跑目标包测试再跑全量**；失败不许进入下一工单。
2. **不改无关文件**：发现既有缺陷（如 `ValidateRefreshToken` 缺 HMAC 校验、`token_blacklist_service.go:85` 原始 token 进 key）**只登记不顺手改**，避免混入无关变更。
3. **明文零泄漏**：任何新增日志/审计/响应都不得包含 `itsm_pat_` 之后的完整串；新增代码走 `safePrefix`。
4. **等价性优先**：任何「PAT 与 JWT 行为不同」的实现都要在方案文档 §4 登记为决策，不允许默默分叉。
5. **门禁必绿**：写路由守卫、预检新鲜度、权限码目录、前端 type-check/lint。
6. **文档同批**：`CHANGELOG` + `api-reference` 与代码同一提交。
7. **验收证据留痕**：命令 + 原始输出 + 时间 + 版本 tag，落到验收文档。

---

## 9. 实施前置的不确定项（实施前必须澄清）

| # | 不确定项 | 处理 |
|---|---|---|
| U-1 | 新增表是否需要登记进 `tests/multi_tenant/rls_matrix_test.go` 的 17 张表清单 | 实施前问 RLS owner；若需，同批更新计数与用例 |
| U-2 | `tests/multi_tenant/rls_matrix_test.go` 是 sqlite 契约测试，enforce 真实验证需 Postgres | 用 238 demo（或本地 Postgres）做一次 enforce 验证并留证据 |
| U-3 | `PAT_ENABLED` 生产默认值（`true` / 首轮 `false`） | 与运维/安全确认后写入配置默认 |
| U-4 | 演示环境网络通道（本工作站不可达） | 确认验收执行机；见验收标准 §4 前置条件 |
| U-5 | 单用户 20 个 active 令牌上限是否满足演示/CI 场景 | 与产品确认（Q4） |

---

## 11. 实施执行记录（2026-10-16）

> 实施以本文档为单一事实源。状态：**T1–T13 全部完成（代码层面）；238 demo 端到端验收待网络通道就绪后执行（验收文档 §4）。**

| 工单 | 状态 | 实际落点（与计划的偏差） |
|---|---|---|
| T1 schema + codegen | ✅ | `ent/schema/personal_access_token.go`；`auditlog.go` 增 `token_id`；entc 生成（`ent/personalaccesstoken/`） |
| T2 迁移 + RLS | ✅ | `migrations/20261016_personal_access_tokens{,_down}.sql`；`database/rls/migrations/014_personal_access_tokens_policies{,_rollback}.sql` |
| T3 认证分支 | ✅ | `middleware/pat_auth.go`（PATAuthenticator/V2 + RejectPATAuth + SafePATPrefix）；`auth.go` PAT 分支（JWT 解析前）；`middleware/pat_usage.go`（dedupe 闸门 + 单令牌限流）；config `PATConfig` |
| T4 领域切片 | ✅ | `handlers/pat/{entity,repository,repository_impl,service,handler}.go` |
| T5 路由 + 守卫 | ✅ | `common_system_routes.go`（PATHandler 非 nil 即注册，**独立于 CommonHandler**；`router.go:651` 调用条件放宽为 `CommonHandler != nil \|\| PATHandler != nil`）；豁免 4 条带理由 |
| T6 审计归因 | ✅ | `audit.go`：`AuditSourcePAT` + `token_id` 写入 + `shouldAuditRequest` PAT 一律留痕 + 三模式闸门（dedupe/all/off） |
| T7 历史读取 | ✅ | usages / security-events 端点 + `auditlog_service.go` `tokenId` 过滤 + DTO `tokenId` |
| T8 安全事件接线 | ✅ | `pat.RevokeAllForUserByClient` 接入：user_service（删除/停用/重置密码/批量停用）、tenant_user_admin（revokeUserSessions）、offboarding |
| T9 前端 | ✅ | `personal-access-token-api.ts` + `AccessTokenPanel.tsx` + `SecurityEventsPanel.tsx` + profile 集成（替换 disabled 登录历史占位）+ i18n zh/en |
| T10 审计页 | ✅ | `auditlog-api.ts` 补 source/actorAccount/tokenId + 过滤；页面增「来源/令牌」列 |
| T11 文档同步 | ✅ | CHANGELOG [Unreleased]、`docs/api-reference.md` PAT 章节、`.env.example`、`scripts/check-pat-secrets.sh`（实测 0 误报）、config.yaml(.example) |
| T12 测试闭环 | ✅ | middleware：PAT 认证/禁用/防枚举/RejectPATAuth/JWT 回归；pat：创建/上限/TTL/轮换两档/幂等吊销/越权404/安全事件/解析器（含停用拒绝）；audit：归因/dedupe/all/off；router e2e：创建→读→4 写路由 403 零副作用→平台禁令→吊销 401；`./router` `./service` `./middleware` `./handlers/pat` `./config` `./dto` 全绿；FE jest 5/5 + tsc + eslint 绿 |
| T13 清理 | ✅ | `service/pat_housekeeping.go`（过期超 180 天回收；PAT_ENABLED 联动启停）+ `HousekeepingRetentionDays` 配置 |

**实施中的关键决策记录（与计划文字的偏差）**：

1. **管理面注册位置最终落点**：实施中发现 `SetupCommonSystemRoutes` 整体由 `if config.CommonHandler != nil` 门控，而 PAT 面需独立可用 → PAT 块移至该函数顶部独立注册，并放宽 `router.go` 的调用条件；**不可挂 auth 组**（有 RBACMiddleware，自省面路径会被预检判 403，实施时实测确认）。
2. **touch 同步执行**：原计划 goroutine 异步更新 `last_used_at`，实测与内存 sqlite 测试库产生锁竞争（下一个请求 401）→ 改为同步（已被 60s 节流，开销可忽略）；生产 Postgres 无此问题，保持同步语义更简单可测。
3. **V2 认证接口**：`PATAuthenticatorV2.Authenticate(ctx, plaintext, clientIP)` 一步完成解析 + 限流 + touch，`AuthMiddleware` 类型断言可选启用（未实现时退化为 V1）。
4. **frontend tests**：antd Modal 关闭动画期间 children 仍挂载 → reveal 弹窗必须 `destroyOnClose` 才能满足 AC-41（明文关闭即从 DOM 清除）；断言时注意「创建令牌」按钮双入口与「登录」文案双语义（action/来源）。

**遗留（不阻断，登记跟进）**：
- `ValidateRefreshToken` 缺 HMAC 算法校验（既有缺陷，实施纪律 §8.2 只登记不顺手改）。
- RLS enforce 真实验证需 Postgres 环境（sqlite 契约测试已过）；238 demo Runbook 执行时一并验证（验收 AC-36/37/38）。
- 监控指标（PAT 认证失败计数器）未接入 metrics 通道（完整性审查第 29 项，P2）。

---

## 10. 审查记录

| 轮次 | 时间 | 方式 | 结论与修正 |
|---|---|---|---|
| 1 | 2026-10-16 | 独立子代理交叉审查（对照源码逐条验证方案断言、锚点与守卫约束） | 见下方「修正」 |
| 2 | 待实施前 | 人工评审（架构 + 安全） | 待补 |

**审查修正（两轮：① 实施前交叉审查 6 项；② 完整性审查——安全/功能走查/前后端集成 30 项）**：

**第一轮（交叉审查）**

1. **【BLOCKER】守卫豁免键**：扫描器以路由调用的**字面 path 参数**构造键（不含组前缀）→ T5 改为直接在 `authGrp` 上以完整子路径注册，豁免键与字面参数一一对应。
2. **【MAJOR】管理面租户取值**：`middleware.GetTenantID` 依赖 `TenantMiddleware` 的 `TenantContext`，而 `authGrp` 无该中间件 → T4/T7/§4 改为 `c.GetInt("tenant_id")`。
3. **【MAJOR】停用用户语义统一为 401**：认证层 `Active` 校验兜底 401（R3 批量吊销为主防线），方案 §3.5 与 AC-21 同步改为 401。
4. **【MINOR】AC-29 与 60s 节流对齐**；5. **【MINOR】AC-18 隔离断言改为 tenantId 集合校验**；6. **【MINOR】长度/掩码修正**（前缀 9 字符、`token_prefix` 前 17 字符、maskedToken 无真实后缀）。
7. **CSRF**：`authGrp` 无组级 CSRF → 管理面写路由逐路由显式挂载（`CSRFEnabled` 条件）。
8. **RLS**：认证期租户上下文未建立 → 带审计的 `tenantctx.SystemContext` 点查（先例 `bootstrap_tokens`/RLS 010）。
9. **MinIssuedAt 语义冲突** → 表内吊销状态（D-PAT-6）。10. **audit_source 顺序契约**需测试锁定。11. **前端为 Vite + React Router 7**（非 Next.js）。12. **补 `idx_audit_source_created` 索引**。

**第二轮（完整性审查：安全 12 项 + 功能/用户操作 12 项 + 前后端集成 6 项）**

13. **【HIGH】防自我繁殖（D-PAT-13）**：PAT 可调管理面写路由（Bearer 免 CSRF）→ 泄漏令牌可续发新令牌。新增 `RejectPATAuth()`（T3/T5）、AC-39、路由测试。
14. **【MEDIUM】平台角色禁令（D-PAT-14）**：super_admin 的 PAT = 365 天 RBAC 旁路凭据（`rbac.go:1270-1273`）→ Create 拒绝平台角色；AC-40。
15. **【MEDIUM】限流按 IP 不按凭据** → 新增单令牌限流 `PAT_RATE_LIMIT_PER_MINUTE` + 高频 401 异常信号；文档明确等价性为逐请求而非逐身份。
16. **【MEDIUM】PAT_ENABLED 默认值与灰度矛盾** → 代码默认 true，env 文件首轮 false；`.env*.example` 增补 + gitleaks 规则列入 T11（U-3 关闭）。
17. **【MEDIUM】PII 保留** → Q7 登记遗留项（审计无保留期，PAT 增加 IP/UA 存量）。
18. **【MEDIUM】前端明文生命周期** → T9 明文仅存局部 state、禁止入缓存/存储/上报；AC-41。
19. **【LOW】logout 无害性** → PAT 调 logout 返回 401 无副作用（`handler.go:159-163`）；`token` 上下文键纳入「不得打印」纪律；AC-42。
20. **【LOW】query/Referer 泄漏** → gin.Logger 记录完整 URL → 负向测试 + API 警示；AC-43。
21. **【LOW】WS 票据等调用面** → 方案 §3.9 新增「PAT 可调用面矩阵」（ws/ticket 允许、refresh/switch-tenant 天然失效、logout 401 无害）。
22. **【LOW】生命周期竞态**（MaxActive check-then-insert、RevokeAll 与并发 Create、时钟偏差）→ T4 接受并文档化。
23. **【P1】令牌行清理** → 新增 T13（过期超 180 天清理；Q8）。
24. **【P1】离职审计断言** → AC-24 增补 `auth.token_revoked_all (security_event)` 断言；api-reference 增管理员调查指引。
25. **【P1】api-reference 硬依赖** → 提升为与 T5 同批 + 列入放行判定。
26. **【P2】401 不可区分排障指引** → api-reference「防枚举设计 + 到个人中心核对状态」。
27. **【P2】CORS 说明** → api-reference「面向服务端/CLI；浏览器集成需 CORS 白名单」。
28. **【P2】启用/停用开关** → Q9 登记 P2。
29. **【P2】监控** → 建议认证失败计数器（随 PAT_RATE_LIMIT 一起接入 metrics 通道，P2 精化）。
30. **【P2】集成侧精度**：422 = HTTP 422 / `code=4220`；down.sql 与 ent schema 回滚顺序语义；T9 补 Pattern A/C/F + race 回归测试（profile 页现状为裸 useEffect，勿复制）；i18n 无对齐门禁需人工核对 + 存在性测试；authGrp 忽略 `X-Tenant-*` 头（无冲突，记录即可）；security-events 分页上限对齐。**已核实无误**：camelCase DTO、响应信封、守卫键格式、前端无新路由不触 guard、ent 无 privacy/mixin 特殊要求、RouterConfig nil-guard、迁移顺序（`app.go:1672-1677`）、http-client CSRF 自动附加与 401 刷新不冲突、InvitationManagementModal 测试模板有效。
