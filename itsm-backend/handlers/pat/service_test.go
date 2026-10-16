package pat

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
)

// PAT 服务规则测试（方案 T12：上限/边界/轮换/吊销/越权/平台角色/落库无明文）。

func randomSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func patTestEnv(t *testing.T, ttlCfg ...int) (*ent.Client, Repository, *Service) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:pat_svc_"+randomSuffix()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	logger := zap.NewNop().Sugar()
	repo := NewRepository(client, logger)
	cfg := DefaultPATConfigView()
	if len(ttlCfg) > 0 {
		cfg.MaxActivePerUser = ttlCfg[0]
	}
	return client, repo, NewService(repo, cfg, logger)
}

// seedTenantUser 建租户 + 用户（最小字段集）。
func seedTenantUser(t *testing.T, client *ent.Client, username, role string, active bool) (int, int) {
	t.Helper()
	ctx := context.Background()
	tn, err := client.Tenant.Create().
		SetName("t-" + username).
		SetCode("tc-" + username).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	u, err := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@test.local").
		SetName(username).
		SetRole(user.Role(role)).
		SetPasswordHash("test-hash-not-real").
		SetTenantID(tn.ID).
		SetActive(active).
		Save(ctx)
	require.NoError(t, err)
	return u.ID, tn.ID
}

func TestService_CreateAndList(t *testing.T) {
	client, repo, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "alice", "admin", true)
	ctx := context.Background()

	res, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "CI 发布脚本", ExpiresInDays: 30}, "admin")
	require.NoError(t, err)
	require.Contains(t, res.PlaintextToken, "itsm_pat_")
	require.Equal(t, StatusActive, res.Token.Status)

	// 落库无明文（AC-34 后端断言）：只有 64 位哈希与 17 字符前缀
	rows, err := repo.ListOwn(ctx, userID, tenantID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].TokenHash, 64)
	require.NotContains(t, rows[0].TokenHash, res.PlaintextToken)
	require.NotContains(t, rows[0].TokenPrefix, res.PlaintextToken[17:])
	require.Equal(t, res.PlaintextToken[:17], rows[0].TokenPrefix)

	items, err := svc.List(ctx, userID, tenantID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	// 掩码不含真实后缀字符
	require.Equal(t, res.PlaintextToken[:17]+"****", items[0].MaskedToken)
}

func TestService_PlatformRoleForbidden(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "rootadmin", "super_admin", true)

	_, err := svc.Create(context.Background(), userID, tenantID, CreateInput{Name: "x"}, "super_admin")
	require.ErrorIs(t, err, ErrPATForbidden)
}

func TestService_TTLLimits(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "bob", "admin", true)
	ctx := context.Background()

	// 超上限
	_, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "x", ExpiresInDays: 400}, "admin")
	require.Error(t, err)
	// 过去时间
	past := time.Now().Add(-time.Hour)
	_, err = svc.Create(ctx, userID, tenantID, CreateInput{Name: "x", ExpiresAt: &past}, "admin")
	require.Error(t, err)
	// 默认 90 天
	res, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "default"}, "admin")
	require.NoError(t, err)
	require.NotNil(t, res.Token.ExpiresAt)
	require.False(t, res.Token.NeverExpires)
	require.WithinDuration(t, time.Now().AddDate(0, 0, 90), *res.Token.ExpiresAt, 5*time.Minute)
}

// 永不过期令牌（D-PAT-8 v2）：哨兵时间落库；DTO nil + neverExpires；状态 active；吊销仍全覆盖。
func TestService_NeverExpires(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "halo", "admin", true)
	ctx := context.Background()

	res, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "forever", NeverExpires: true}, "admin")
	require.NoError(t, err)
	require.Nil(t, res.Token.ExpiresAt)
	require.True(t, res.Token.NeverExpires)
	require.Equal(t, StatusActive, res.Token.Status)

	// 落库为哨兵时间（清理/派生逻辑兼容）
	items, _ := svc.List(ctx, userID, tenantID)
	require.True(t, items[0].NeverExpires)
	require.Nil(t, items[0].ExpiresAt)

	// 互斥：neverExpires 与 expiresInDays 同传 → 永久优先（忽略天数）
	res2, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "both", NeverExpires: true, ExpiresInDays: 7}, "admin")
	require.NoError(t, err)
	require.True(t, res2.Token.NeverExpires)

	// 吊销全覆盖
	_, err = svc.Revoke(ctx, userID, tenantID, res.Token.ID)
	require.NoError(t, err)
	items2, _ := svc.List(ctx, userID, tenantID)
	for _, it := range items2 {
		if it.ID == res.Token.ID {
			require.Equal(t, StatusRevoked, it.Status)
		}
	}
}

func TestService_ActiveLimit(t *testing.T) {
	client, _, svc := patTestEnv(t, 2)
	userID, tenantID := seedTenantUser(t, client, "carol", "admin", true)
	ctx := context.Background()

	_, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "1"}, "admin")
	require.NoError(t, err)
	_, err = svc.Create(ctx, userID, tenantID, CreateInput{Name: "2"}, "admin")
	require.NoError(t, err)
	_, err = svc.Create(ctx, userID, tenantID, CreateInput{Name: "3"}, "admin")
	require.ErrorIs(t, err, ErrPATLimit)
}

func TestService_RotateImmediateAndGrace(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "dave", "admin", true)
	ctx := context.Background()

	first, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "rot", ExpiresInDays: 30}, "admin")
	require.NoError(t, err)

	// 宽限轮换：旧令牌仍 active 但 expiresAt 收紧
	second, err := svc.Rotate(ctx, userID, tenantID, first.Token.ID, 24)
	require.NoError(t, err)
	require.NotEqual(t, first.PlaintextToken, second.PlaintextToken)

	items, _ := svc.List(ctx, userID, tenantID)
	byID := map[int]*Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	require.Equal(t, StatusActive, byID[first.Token.ID].Status)
	require.True(t, byID[first.Token.ID].ExpiresAt.Before(time.Now().Add(25*time.Hour)))

	// 立即轮换（对新令牌）
	third, err := svc.Rotate(ctx, userID, tenantID, second.Token.ID, 0)
	require.NoError(t, err)
	items2, _ := svc.List(ctx, userID, tenantID)
	byID2 := map[int]*Item{}
	for _, it := range items2 {
		byID2[it.ID] = it
	}
	require.Equal(t, StatusRevoked, byID2[second.Token.ID].Status)
	require.Equal(t, RevokeReasonRotated, byID2[second.Token.ID].RevokedReason)
	require.Equal(t, StatusActive, byID2[third.Token.ID].Status)
}

func TestService_RevokeIdempotent(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "erin", "admin", true)
	ctx := context.Background()

	res, _ := svc.Create(ctx, userID, tenantID, CreateInput{Name: "r"}, "admin")
	it1, err := svc.Revoke(ctx, userID, tenantID, res.Token.ID)
	require.NoError(t, err)
	it2, err := svc.Revoke(ctx, userID, tenantID, res.Token.ID)
	require.NoError(t, err)
	require.Equal(t, it1.RevokedAt.Unix(), it2.RevokedAt.Unix())
}

func TestService_CrossUserIs404(t *testing.T) {
	client, _, svc := patTestEnv(t)
	aliceID, tenantID := seedTenantUser(t, client, "alice2", "admin", true)
	bobID, _ := seedTenantUser(t, client, "bob2", "admin", true)
	ctx := context.Background()

	res, _ := svc.Create(ctx, aliceID, tenantID, CreateInput{Name: "a"}, "admin")
	_, err := svc.Revoke(ctx, bobID, tenantID, res.Token.ID)
	require.ErrorIs(t, err, ErrPATNotFound)
	_, err = svc.Usages(ctx, bobID, tenantID, res.Token.ID, 1, 20)
	require.ErrorIs(t, err, ErrPATNotFound)
}

func TestService_SecurityEventRevocation(t *testing.T) {
	client, _, svc := patTestEnv(t)
	userID, tenantID := seedTenantUser(t, client, "frank", "admin", true)
	ctx := context.Background()

	_, err := svc.Create(ctx, userID, tenantID, CreateInput{Name: "x"}, "admin")
	require.NoError(t, err)
	n, err := svc.RevokeAllForUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	items, _ := svc.List(ctx, userID, tenantID)
	require.Equal(t, StatusRevoked, items[0].Status)
	require.Equal(t, RevokeReasonSecurityEvent, items[0].RevokedReason)
}

// 认证解析器：有效令牌 → 身份；停用用户 → 拒绝；错误明文 → 统一错误。
func TestResolver_Authenticate(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:pat_res_"+randomSuffix()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	logger := zap.NewNop().Sugar()

	userID, tenantID := seedTenantUser(t, client, "gina", "admin", true)

	plaintext, prefix, err := GeneratePlaintext()
	require.NoError(t, err)
	_, err = NewRepository(client, logger).Create(context.Background(), CreateRow{
		UserID: userID, TenantID: tenantID, Name: "t", TokenHash: HashToken(plaintext), TokenPrefix: prefix,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	require.NoError(t, err)

	r := NewResolver(client, logger)
	v2, ok := r.(middleware.PATAuthenticatorV2)
	require.True(t, ok)

	identity, allowed, err := v2.Authenticate(context.Background(), plaintext, "1.2.3.4")
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, userID, identity.UserID)
	require.Equal(t, tenantID, identity.TenantID)
	require.Equal(t, "admin", identity.Role)
	require.Equal(t, prefix, identity.TokenPrefix)

	// 停用 → 拒绝（认证层 401 语义）
	require.NoError(t, client.User.UpdateOneID(userID).SetActive(false).Exec(context.Background()))
	_, _, err = v2.Authenticate(context.Background(), plaintext, "1.2.3.4")
	require.Error(t, err)

	// 错误明文 → 统一错误（防枚举）
	_, _, err = v2.Authenticate(context.Background(), plaintext+"x", "1.2.3.4")
	require.Error(t, err)
}
