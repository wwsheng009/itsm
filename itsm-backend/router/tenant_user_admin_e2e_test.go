// tenant_user_admin_e2e_test.go：TUM-A1–A8 API 通道端到端（路由器级全中间件链）。
//
// 与外部脚本的分工：
//   - scripts/msp/acceptance/run-tenant-user-admin-acceptance.ps1 面向联调实例（真实登录/CSRF/多实例）；
//   - 本文件在**路由器级**跑完整中间件链（Auth → RBAC → TenantMiddleware → handler → service），
//     ent/sqlite 内存库，无外部环境、可 CI 复跑，覆盖 TUM-A1–A8 的接口契约与副作用
//     （口令哈希/双吊销/审计/通知），剧本对齐 scenarios/09（S1–S3）。
//
// 说明：真实「旧口令登录 401」在外部脚本覆盖；本文件以 bcrypt 哈希比对 + active + 吊销状态
// 等价断言（与 service 层口径一致，避免为测试引入完整 AuthHandler 依赖）。
package router

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/ent"
	entAudit "itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	entNotification "itsm-backend/ent/notification"
	"itsm-backend/ent/user"
	domainCommon "itsm-backend/handlers/common"
	tenantHandler "itsm-backend/handlers/tenant"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

type tumE2EEnv struct {
	client           *ent.Client
	engine           *gin.Engine
	tenantA          *ent.Tenant // 被治理的客户租户
	tenantB          *ent.Tenant // 平台运营租户（actor home）
	platform         *ent.User   // super_admin
	adminA1          *ent.User   // 租户 A 管理员 1
	adminA2          *ent.User   // 租户 A 管理员 2
	normalA          *ent.User   // 租户 A 普通用户（被治理对象）
	normalB          *ent.User   // 租户 B 普通用户（IDOR 目标）
	platformToken    string
	tenantAdminToken string
}

func newTUME2EEnv(t *testing.T) *tumE2EEnv {
	t.Helper()
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:tum_e2e_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	logger := zaptest.NewLogger(t).Sugar()

	tenantB := mustTenant(t, client, "平台运营租户", "tum-plat", "internal")
	tenantA := mustTenant(t, client, "客户A", "tum-a", "saas_customer")
	platform := mustTUMUser(t, client, "tum-platform", tenantB.ID, user.RoleSuperAdmin, "Acpt@2026Plat!")
	adminA1 := mustTUMUser(t, client, "tum-a-admin1", tenantA.ID, user.RoleAdmin, "Acpt@2026Admin!")
	adminA2 := mustTUMUser(t, client, "tum-a-admin2", tenantA.ID, user.RoleAdmin, "Acpt@2026Admin2!")
	normalA := mustTUMUser(t, client, "tum-a-user1", tenantA.ID, user.RoleEndUser, "OldPass123")
	normalB := mustTUMUser(t, client, "tum-b-user", tenantB.ID, user.RoleEndUser, "OldPass123")

	userSvc := service.NewUserService(client, logger)
	tumSvc := service.NewTenantUserAdminService(client, userSvc, logger)
	tumSvc.SetEnabled(true)
	tumSvc.SetNotifier(service.NewNotificationService(client))

	tenantSvc := service.NewTenantService(client, logger)
	tenantSvc.SetTenantQuotaService(service.NewTenantQuotaService(client, logger))
	h := tenantHandler.NewHandler(tenantSvc, logger)
	h.SetTenantUserAdminService(tumSvc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	commonHandler := domainCommon.NewHandler(domainCommon.NewService(domainCommon.NewEntRepository(client), mspE2EJWTSecret, logger, client))
	SetupRoutes(engine, &RouterConfig{
		JWTSecret:     mspE2EJWTSecret,
		Logger:        logger,
		Client:        client,
		CommonHandler: commonHandler,
		TenantHandler: h,
	})

	return &tumE2EEnv{
		client: client, engine: engine,
		tenantA: tenantA, tenantB: tenantB,
		platform: platform, adminA1: adminA1, adminA2: adminA2, normalA: normalA, normalB: normalB,
		platformToken: mspE2EToken(t, platform), tenantAdminToken: mspE2EToken(t, adminA1),
	}
}

func mustTUMUser(t *testing.T, client *ent.Client, username string, tenantID int, role user.Role, password string) *ent.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	entity, err := client.User.Create().
		SetUsername(username).SetEmail(username + "@example.com").SetName(username).
		SetPasswordHash(string(hash)).SetActive(true).
		SetTenantID(tenantID).SetRole(role).
		Save(context.Background())
	require.NoError(t, err)
	return entity
}

func tumHashMatches(t *testing.T, client *ent.Client, userID int, password string) bool {
	t.Helper()
	u, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// TestTUM_A1_A8_APIE2E 是唯一剧本：平台管理员对租户 A 用户的读通道与三类治理动作，
// 覆盖 TUM-A1–A8 正向 + 反例（403/404/409/400），断言接口码、副作用与审计/通知留痕。
func TestTUM_A1_A8_APIE2E(t *testing.T) {
	ctx := context.Background()
	env := newTUME2EEnv(t)
	base := fmt.Sprintf("/api/v1/tenants/%d/users", env.tenantA.ID)

	// ---------- A1 读通道：列表/检索/详情 ----------
	listStatus, listBody := mspE2ERequest(t, env.engine, http.MethodGet, base+"?page=1&pageSize=20", env.platformToken, nil)
	require.Equal(t, http.StatusOK, listStatus, listBody)
	listData := decodeData(t, listBody)
	items, _ := listData["items"].([]interface{})
	require.Len(t, items, 3, "租户 A 应有 3 个用户")
	total, _ := listData["total"].(float64)
	assert.Equal(t, float64(3), total)
	first, _ := items[0].(map[string]interface{})
	assert.Contains(t, first, "mustChangePassword")
	assert.Contains(t, first, "role")
	assert.Contains(t, first, "isBootstrapAdmin")

	searchStatus, searchBody := mspE2ERequest(t, env.engine, http.MethodGet, base+"?keyword=tum-a-user1", env.platformToken, nil)
	require.Equal(t, http.StatusOK, searchStatus, searchBody)
	searchItems, _ := decodeData(t, searchBody)["items"].([]interface{})
	require.Len(t, searchItems, 1, "关键字检索应命中唯一用户")

	detailStatus, detailBody := mspE2ERequest(t, env.engine, http.MethodGet, fmt.Sprintf("%s/%d", base, env.normalA.ID), env.platformToken, nil)
	require.Equal(t, http.StatusOK, detailStatus, detailBody)
	assert.Equal(t, float64(env.normalA.ID), decodeData(t, detailBody)["id"])

	// 反例 1：租户管理员（admin + tenant:read/write 走静态兜底）→ 服务层平台面判定 403。
	r1Status, r1Body := mspE2ERequest(t, env.engine, http.MethodGet, base, env.tenantAdminToken, nil)
	assert.Equal(t, http.StatusForbidden, r1Status, r1Body)
	assert.Contains(t, r1Body, "PLATFORM_SCOPE_REQUIRED")

	// 反例 2：跨租户 IDOR（用 B 租户路径拼 A 用户）→ 404。
	r2Status, r2Body := mspE2ERequest(t, env.engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%d/users/%d", env.tenantB.ID, env.normalA.ID), env.platformToken, nil)
	assert.Equal(t, http.StatusNotFound, r2Status, r2Body)
	assert.Contains(t, r2Body, "TENANT_USER_NOT_FOUND")

	// ---------- A2 重置·一次性（S1）：旧口令失效/新口令可验/强制改密/双吊销 ----------
	reset1Status, reset1Body := mspE2ERequest(t, env.engine, http.MethodPost, fmt.Sprintf("%s/%d/reset-password", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"mode": "generated"})
	require.Equal(t, http.StatusOK, reset1Status, reset1Body)
	reset1Data := decodeData(t, reset1Body)
	gen1, _ := reset1Data["generatedPassword"].(string)
	require.GreaterOrEqual(t, len(gen1), 8, "一次性口令仅本次回传")
	assert.Equal(t, true, reset1Data["mustChangePassword"])
	assert.False(t, tumHashMatches(t, env.client, env.normalA.ID, "OldPass123"), "旧口令必须失效")
	assert.True(t, tumHashMatches(t, env.client, env.normalA.ID, gen1), "新一次性口令可验")
	u, err := env.client.User.Get(ctx, env.normalA.ID)
	require.NoError(t, err)
	assert.True(t, u.MustChangePassword)
	revoked, err := middleware.IsUserRefreshRevoked(ctx, env.normalA.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked, "重置后历史 refresh 双吊销")

	// A2e：并发二次 generated——后一次生效、前一次失效。
	reset2Status, reset2Body := mspE2ERequest(t, env.engine, http.MethodPost, fmt.Sprintf("%s/%d/reset-password", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"mode": "generated"})
	require.Equal(t, http.StatusOK, reset2Status, reset2Body)
	gen2, _ := decodeData(t, reset2Body)["generatedPassword"].(string)
	require.NotEqual(t, gen1, gen2)
	assert.False(t, tumHashMatches(t, env.client, env.normalA.ID, gen1), "前一次口令失效")
	assert.True(t, tumHashMatches(t, env.client, env.normalA.ID, gen2), "后一次口令生效")

	// ---------- A3 重置·指定：策略拦截 + 新口令可验 ----------
	weakStatus, weakBody := mspE2ERequest(t, env.engine, http.MethodPost, fmt.Sprintf("%s/%d/reset-password", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"mode": "specified", "newPassword": "short"})
	assert.Equal(t, http.StatusBadRequest, weakStatus, weakBody)
	assert.Contains(t, weakBody, "PASSWORD_POLICY_VIOLATION")

	const strongPass = "StrongPass456"
	strongStatus, strongBody := mspE2ERequest(t, env.engine, http.MethodPost, fmt.Sprintf("%s/%d/reset-password", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"mode": "specified", "newPassword": strongPass})
	require.Equal(t, http.StatusOK, strongStatus, strongBody)
	assert.Equal(t, false, decodeData(t, strongBody)["mustChangePassword"])
	assert.True(t, tumHashMatches(t, env.client, env.normalA.ID, strongPass))
	assert.False(t, tumHashMatches(t, env.client, env.normalA.ID, gen2), "指定重置后旧一次性口令失效")

	// ---------- A4 启停（S2）：即时失效 / 恢复 / 最后管理员护栏 / 自停用 ----------
	disStatus, disBody := mspE2ERequest(t, env.engine, http.MethodPut, fmt.Sprintf("%s/%d/status", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"active": false})
	require.Equal(t, http.StatusOK, disStatus, disBody)
	u, err = env.client.User.Get(ctx, env.normalA.ID)
	require.NoError(t, err)
	assert.False(t, u.Active, "停用即时生效")
	revoked, err = middleware.IsUserRefreshRevoked(ctx, env.normalA.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked, "停用触发 refresh 双吊销")

	enStatus, enBody := mspE2ERequest(t, env.engine, http.MethodPut, fmt.Sprintf("%s/%d/status", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{"active": true})
	require.Equal(t, http.StatusOK, enStatus, enBody)
	u, err = env.client.User.Get(ctx, env.normalA.ID)
	require.NoError(t, err)
	assert.True(t, u.Active, "启用恢复")

	// A4c：停 adminA2 允许（adminA1 仍可用）→ 再停 adminA1 = 最后管理员 409。
	disAdmin2Status, disAdmin2Body := mspE2ERequest(t, env.engine, http.MethodPut, fmt.Sprintf("%s/%d/status", base, env.adminA2.ID),
		env.platformToken, map[string]interface{}{"active": false})
	require.Equal(t, http.StatusOK, disAdmin2Status, disAdmin2Body)
	lastAdminStatus, lastAdminBody := mspE2ERequest(t, env.engine, http.MethodPut, fmt.Sprintf("%s/%d/status", base, env.adminA1.ID),
		env.platformToken, map[string]interface{}{"active": false})
	assert.Equal(t, http.StatusConflict, lastAdminStatus, lastAdminBody)
	assert.Contains(t, lastAdminBody, "LAST_ADMIN_PROTECTED")

	// R3：平台管理员停用自己 → 409 SELF_OPERATION_FORBIDDEN。
	selfStatus, selfBody := mspE2ERequest(t, env.engine, http.MethodPut,
		fmt.Sprintf("/api/v1/tenants/%d/users/%d/status", env.tenantB.ID, env.platform.ID),
		env.platformToken, map[string]interface{}{"active": false})
	assert.Equal(t, http.StatusConflict, selfStatus, selfBody)
	assert.Contains(t, selfBody, "SELF_OPERATION_FORBIDDEN")

	// ---------- S3 强制下线：仅吊销会话 ----------
	forceStatus, forceBody := mspE2ERequest(t, env.engine, http.MethodPost, fmt.Sprintf("%s/%d/force-logout", base, env.normalA.ID),
		env.platformToken, map[string]interface{}{})
	require.Equal(t, http.StatusOK, forceStatus, forceBody)
	revoked, err = middleware.IsUserRefreshRevoked(ctx, env.normalA.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	assert.True(t, revoked, "强制下线后历史 refresh 被拒")

	// ---------- A5 审计：三类动作按 target 可查 + 明文口令掩码 ----------
	auditRows, err := env.client.AuditLog.Query().
		Where(
			entAudit.TenantIDEQ(env.tenantB.ID),
			entAudit.TargetTenantIDEQ(env.tenantA.ID),
			entAudit.TargetUserIDEQ(env.normalA.ID),
		).
		All(ctx)
	require.NoError(t, err)
	actions := map[string]int{}
	for _, row := range auditRows {
		actions[row.Action]++
		if row.RequestBody != nil {
			assert.NotContains(t, *row.RequestBody, strongPass, "指定重置 newPassword 必须掩码")
			assert.NotContains(t, *row.RequestBody, gen2, "一次性口令不得落审计")
		}
	}
	assert.GreaterOrEqual(t, actions["user.admin_password_reset"], 3, "3 次重置（generated×2 + specified）")
	assert.GreaterOrEqual(t, actions["user.admin_status"], 2, "停用+启用")
	assert.GreaterOrEqual(t, actions["user.admin_force_logout"], 1)

	// ---------- A8 通知触达（C5）：目标租户管理员收通知，不含口令、不含被操作用户 ----------
	notifications, err := env.client.Notification.Query().
		Where(entNotification.TenantIDEQ(env.tenantA.ID), entNotification.TypeEQ("user_admin_action")).
		All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, notifications, "治理动作必须触达目标租户管理员")
	allowed := map[int]bool{env.adminA1.ID: true, env.adminA2.ID: true}
	for _, n := range notifications {
		assert.True(t, allowed[n.UserID], "收件人只能是目标租户管理员（got user=%d）", n.UserID)
		assert.NotContains(t, n.Message, strongPass)
		assert.NotContains(t, n.Message, gen2)
	}

	// ---------- A8 对账（C4）：列表 total 与配额 users 用量一致 ----------
	usageStatus, usageBody := mspE2ERequest(t, env.engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%d/usage", env.tenantA.ID), env.platformToken, nil)
	require.Equal(t, http.StatusOK, usageStatus, usageBody)
	used, _ := decodeData(t, usageBody)["used"].(map[string]interface{})
	require.NotNil(t, used)
	assert.Equal(t, float64(3), used["users"], "用量 users 与列表 total 同源（3 用户）")
}
