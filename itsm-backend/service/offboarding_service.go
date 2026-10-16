package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/pat"
	"itsm-backend/middleware"
)

// =============================================================================
// IP-P2-8：离职编排（停用账号 + 全量会话失效 + 撤销全部分配 + 审计）
//
// 语义（方案 §3.1，决策 D2=A）：
//  1. 停用先行（安全优先）：active=false + access/refresh 全量吊销；失败即整体失败；
//  2. 分配回收逐条幂等：单条失败不阻断，计入 pending 并告警（巡检兜底，与 MSW-1 口径一致）；
//  3. 汇总审计 user.offboard（分配侧 N×membership.revoke 由既有通道产生）。
// 幂等：重复调用 alreadyInactive=true、allocationsRevoked=0；pending 项可重跑收敛。
// =============================================================================

var (
	// ErrOffboardUserNotFound 目标用户不存在（或不属于调用者租户）。
	ErrOffboardUserNotFound = errors.New("用户不存在")
	// ErrSelfOffboard 不允许对当前登录用户执行离职（防自锁）。
	ErrSelfOffboard = errors.New("不能对当前登录用户执行离职")
)

// OffboardMeta 离职编排的调用上下文（审计归属与触发来源）。
type OffboardMeta struct {
	ActorUserID  int
	ActorAccount string
	Source       string
	Trigger      string
}

// OffboardResult 离职编排结果（计数用于审计与前端提示）。
type OffboardResult struct {
	UserID             int  `json:"userId"`
	Deactivated        bool `json:"deactivated"`
	AlreadyInactive    bool `json:"alreadyInactive"`
	AllocationsRevoked int  `json:"allocationsRevoked"`
	AllocationsPending int  `json:"allocationsPending"`
}

// OffboardingService 离职编排服务（组合 MSPAllocationService；不改变其单对撤销语义）。
type OffboardingService struct {
	client      *ent.Client
	allocations *MSPAllocationService
	logger      *zap.SugaredLogger
}

// NewOffboardingService 创建离职编排服务实例。
func NewOffboardingService(client *ent.Client, allocations *MSPAllocationService, logger *zap.SugaredLogger) *OffboardingService {
	return &OffboardingService{client: client, allocations: allocations, logger: logger}
}

// OffboardUser 执行离职编排：停用账号 → 全量会话失效 → 撤销全部分配 → 审计。
func (s *OffboardingService) OffboardUser(ctx context.Context, userID, homeTenantID int, meta OffboardMeta) (*OffboardResult, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("offboarding service unavailable")
	}
	if userID <= 0 || homeTenantID <= 0 {
		return nil, fmt.Errorf("invalid offboard scope: user=%d tenant=%d", userID, homeTenantID)
	}
	if meta.ActorUserID > 0 && meta.ActorUserID == userID {
		return nil, ErrSelfOffboard
	}

	u, err := s.client.User.Query().
		Where(user.IDEQ(userID), user.TenantIDEQ(homeTenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrOffboardUserNotFound
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	res := &OffboardResult{UserID: userID, AlreadyInactive: !u.Active}

	// ① 停用账号（安全优先；失败即整体失败）。
	if u.Active {
		if _, err := s.client.User.UpdateOneID(userID).
			Where(user.TenantIDEQ(homeTenantID)).
			SetActive(false).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("停用用户失败: %w", err)
		}
		res.Deactivated = true
	}

	// 全量会话失效：access（签发时间下限）+ refresh（用户级全局）双吊销。
	now := time.Now()
	if err := middleware.InvalidateUserAccessTokens(ctx, userID, now); err != nil {
		s.logger.Warnw("offboard: access token invalidation failed", "error", err, "user_id", userID)
	}
	if err := middleware.InvalidateUserRefreshTokens(ctx, userID, now); err != nil {
		s.logger.Warnw("offboard: refresh token invalidation failed", "error", err, "user_id", userID)
	}
	// PAT（D-PAT-6/R3）：离职编排 → 全部个人访问令牌吊销（security_event）。
	pat.RevokeAllForUserByClient(ctx, s.client, s.logger, userID)

	// ② 撤销全部分配（逐条幂等；失败不阻断，计入 pending 并告警——巡检兜底）。
	if s.allocations != nil {
		revoked, pending, err := s.allocations.RevokeAllForUser(ctx, userID, MembershipAuditMeta{
			ActorUserID:  meta.ActorUserID,
			ActorAccount: meta.ActorAccount,
			Source:       meta.Source,
			Trigger:      offboardTrigger(meta),
		})
		if err != nil {
			s.logger.Warnw("offboard: revoke all allocations failed", "error", err, "user_id", userID)
		}
		res.AllocationsRevoked = len(revoked)
		res.AllocationsPending = len(pending)
	}

	// ③ 汇总审计（尽力而为；失败仅告警）。
	s.writeOffboardAudit(ctx, userID, homeTenantID, meta, res)
	return res, nil
}

// offboardTrigger 归一触发标记（默认 user.offboard）。
func offboardTrigger(meta OffboardMeta) string {
	if t := strings.TrimSpace(meta.Trigger); t != "" {
		return t
	}
	return "user.offboard"
}

// writeOffboardAudit 写入 user.offboard 审计行（归属被停用用户 home 租户；target_user_id 标注）。
func (s *OffboardingService) writeOffboardAudit(ctx context.Context, userID, homeTenantID int, meta OffboardMeta, res *OffboardResult) {
	if s == nil || s.client == nil || res == nil {
		return
	}
	source := strings.TrimSpace(meta.Source)
	if source == "" {
		source = middleware.AuditSourceSystem
	}
	outcome := "offboarded"
	if res.AllocationsPending > 0 {
		outcome = "partial"
	}
	payload, _ := json.Marshal(map[string]any{
		"outcome":             outcome,
		"trigger":             offboardTrigger(meta),
		"already_inactive":    res.AlreadyInactive,
		"allocations_revoked": res.AllocationsRevoked,
		"allocations_pending": res.AllocationsPending,
	})
	auditCtx := tenantctx.WithTenantID(ctx, homeTenantID)
	auditCtx, cancel := context.WithTimeout(auditCtx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(homeTenantID).
		SetUserID(meta.ActorUserID).
		SetActorAccount(meta.ActorAccount).
		SetTargetTenantID(homeTenantID).
		SetTargetUserID(userID).
		SetSource(source).
		SetResource("user").
		SetAction("user.offboard").
		SetPath("service:offboarding").
		SetMethod("INTERNAL").
		SetStatusCode(200).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil {
		s.logger.Warnw("offboard audit write failed", "error", err, "user_id", userID, "tenant_id", homeTenantID)
	}
}
