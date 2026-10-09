package service

// membership_expiry.go：membership 到期回收（IP-P2-9）。
//
// 权威依据：
//   - offboarding 方案 §4 P2：`expires_at` 到期自动回收（后台任务）；
//   - scope-model B.3.5/B.8：到期 = 惰性判定（读路径 fail-closed）+ 后台回收任务；
//     home 为账号级语义（到期由账号停用/租户到期承载），不做自动回收。
//
// 语义：
//  1. 扫描「存活（deleted_at IS NULL）+ expires_at 非空且 <= now」的 membership；
//  2. 逐条执行与 MSW-3 `DeactivateMembership` 等价的回收核心（`revokeMembershipCore`）：
//     allocation 级联解除 + 条件软删 + `membership.revoke` 审计（trigger=membership.expired）
//     + 立即生效（权限缓存单点失效 + (user×tenant) 在途 refresh 吊销）；
//  3. source=home 的到期行跳过并告警（配置异常可见化，不自动回收）；
//  4. 幂等：重复执行无存活行可回收时返回 0；并发（多实例）靠条件更新去重（n=0 跳过审计）。
//
// 开关与间隔（部署登记见 docs/multi-tenant/02-deployment-and-configuration.md）：
//   - `MEMBERSHIP_EXPIRY_RECYCLE_ENABLED`：默认开；显式 false/0/no/off（大小写/空白容忍）关闭回滚；
//   - `MEMBERSHIP_EXPIRY_RECYCLE_INTERVAL_MINUTES`：默认 10；<=0/非法回退默认。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
)

// 到期回收默认参数。
const (
	membershipExpiryDefaultLimit = 200
	membershipExpiryDefaultEvery = 10 * time.Minute
)

// MembershipExpiryService membership 到期回收服务（后台任务；IP-P2-9）。
type MembershipExpiryService struct {
	client *ent.Client
	sync   *MembershipSyncService
	logger *zap.SugaredLogger
	now    func() time.Time // 测试注入；nil=time.Now
}

// MembershipExpiryResult 单轮回收结果（日志/验收口径）。
type MembershipExpiryResult struct {
	Scanned     int `json:"scanned"`
	Recycled    int `json:"recycled"`
	SkippedHome int `json:"skippedHome"`
	Failed      int `json:"failed"`
}

// NewMembershipExpiryService 构造到期回收服务；syncSvc 为空时内部构造；logger 为空时 Nop。
func NewMembershipExpiryService(client *ent.Client, syncSvc *MembershipSyncService, logger *zap.SugaredLogger) *MembershipExpiryService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	if syncSvc == nil && client != nil {
		syncSvc = NewMembershipSyncService(client, logger)
	}
	return &MembershipExpiryService{client: client, sync: syncSvc, logger: logger}
}

// MembershipExpiryRecycleEnabledFromEnv 解析 MEMBERSHIP_EXPIRY_RECYCLE_ENABLED（IP-P2-9）。
// 默认开（未设置/空/无法识别=开）；显式 false/0/no/off（大小写/空白容忍）才关闭（回滚口径）。
func MembershipExpiryRecycleEnabledFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// MembershipExpiryRecycleIntervalFromEnv 解析 MEMBERSHIP_EXPIRY_RECYCLE_INTERVAL_MINUTES（分钟）。
// 非法/<=0 回退 def（默认 10 分钟）。
func MembershipExpiryRecycleIntervalFromEnv(raw string, def time.Duration) time.Duration {
	if def <= 0 {
		def = membershipExpiryDefaultEvery
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || minutes <= 0 {
		return def
	}
	return time.Duration(minutes) * time.Minute
}

// RecycleExpired 执行一轮到期回收（跨租户；limit<=0 时默认 200）。
func (s *MembershipExpiryService) RecycleExpired(ctx context.Context, limit int) (*MembershipExpiryResult, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("membership expiry service unavailable")
	}
	if s.sync == nil {
		s.sync = NewMembershipSyncService(s.client, s.logger)
	}
	if limit <= 0 {
		limit = membershipExpiryDefaultLimit
	}
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()

	scanCtx := tenantctx.SystemContext(ctx, "membership-expiry-recycler:scan",
		"recycle expired memberships across tenants (IP-P2-9)")
	rows, err := s.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.ExpiresAtNotNil(),
			usertenantmembership.ExpiresAtLTE(now),
		).
		Order(ent.Asc(usertenantmembership.FieldExpiresAt)).
		Limit(limit).
		All(scanCtx)
	if err != nil {
		return nil, fmt.Errorf("scan expired memberships: %w", err)
	}

	res := &MembershipExpiryResult{Scanned: len(rows)}
	meta := MembershipAuditMeta{
		Source:  middleware.AuditSourceSystem,
		Trigger: "membership.expired",
	}
	for _, m := range rows {
		if m.Source == usertenantmembership.SourceHome {
			// home=账号级语义：到期由账号停用/租户到期承载；带 expires_at 的 home 行视为配置异常。
			res.SkippedHome++
			s.logger.Warnw("membership expiry: home membership with expires_at skipped (misconfiguration?)",
				"membership_id", m.ID, "user_id", m.UserID, "tenant_id", m.TenantID,
				"expires_at", m.ExpiresAt)
			continue
		}
		if err := s.sync.revokeMembershipCore(scanCtx, m, meta, "expired"); err != nil {
			res.Failed++
			s.logger.Warnw("membership expiry recycle failed",
				"membership_id", m.ID, "user_id", m.UserID, "tenant_id", m.TenantID, "error", err)
			continue
		}
		res.Recycled++
		s.logger.Infow("membership expired and recycled",
			"membership_id", m.ID, "user_id", m.UserID, "tenant_id", m.TenantID,
			"source", string(m.Source), "expires_at", m.ExpiresAt)
	}
	return res, nil
}
