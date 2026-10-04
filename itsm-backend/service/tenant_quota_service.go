package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/attachment"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/tenantquota"
)

// tenant_quota_service.go：租户硬配额（limits）的读取、用量统计与校验（IP-P2-6）。
//
// 设计要点：
//   - 配额来源唯一 = tenants.quota（tenantquota.Limits）；NULL/零值 = 不限（fail-open）；
//   - 校验方法对 nil 接收者安全：未接入配额的业务路径（如单测默认装配）保持既有行为；
//   - 超限统一返回 *tenantquota.ExceededError（HTTP 422 + reasonCode=TENANT_QUOTA_EXCEEDED；
//     附件路径由 AttachmentService 包装为既有 6106 语义，保持客户端错误分支不变）。
type TenantQuotaService struct {
	client *ent.Client
	logger *zap.SugaredLogger
}

func NewTenantQuotaService(client *ent.Client, logger *zap.SugaredLogger) *TenantQuotaService {
	return &TenantQuotaService{client: client, logger: logger}
}

// TenantQuotaUsage 租户当前用量快照（与校验口径一致，供治理页/巡检复用）。
type TenantQuotaUsage struct {
	Users            int64 `json:"users"`
	TicketsThisMonth int64 `json:"ticketsThisMonth"`
	StorageBytes     int64 `json:"storageBytes"`
}

// LimitsOf 读取租户配额；租户不存在返回错误（fail-closed：拒绝写入而不是放行）。
func (s *TenantQuotaService) LimitsOf(ctx context.Context, tenantID int) (tenantquota.Limits, error) {
	if s == nil || s.client == nil {
		return tenantquota.Limits{}, nil
	}
	// RLS（enforce）：配额/用量属于**目标租户**的数据面。平台管理员、服务商建号等
	// 调用方的请求 ctx 携带的是自己家租户，直接查询会被策略收窄为 0 行——
	// 轻则误判超限（Q2），重则**静默绕过配额**（Q5：超限仍建号成功）。统一重绑定。
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	row, err := s.client.Tenant.Query().
		Where(tenant.IDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return tenantquota.Limits{}, fmt.Errorf("tenant %d not found", tenantID)
		}
		return tenantquota.Limits{}, fmt.Errorf("load tenant quota: %w", err)
	}
	return row.Quota, nil
}

// Usage 汇总当前用量：用户数 / 本自然月新建工单数（未删除）/ 有效附件字节数。
func (s *TenantQuotaService) Usage(ctx context.Context, tenantID int) (*TenantQuotaUsage, error) {
	if s == nil || s.client == nil {
		return &TenantQuotaUsage{}, nil
	}
	// 同 LimitsOf：用量计数以目标租户为作用域（RLS enforce 下缺此绑定会静默清零）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	users, err := s.client.User.Query().Where(user.TenantID(tenantID)).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count tenant users: %w", err)
	}
	ticketsThisMonth, err := s.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.CreatedAtGTE(monthStart(time.Now())),
			ticket.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count tenant tickets: %w", err)
	}
	storage, err := s.storageBytes(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &TenantQuotaUsage{
		Users:            int64(users),
		TicketsThisMonth: int64(ticketsThisMonth),
		StorageBytes:     storage,
	}, nil
}

// storageBytes 有效附件（status=active 且未软删）字节数合计。
func (s *TenantQuotaService) storageBytes(ctx context.Context, tenantID int) (int64, error) {
	sizes, err := s.client.Attachment.Query().
		Where(
			attachment.TenantID(tenantID),
			attachment.StatusEQ("active"),
			attachment.DeletedAtIsNil(),
		).
		Select(attachment.FieldFileSize).
		Ints(ctx)
	if err != nil {
		return 0, fmt.Errorf("sum tenant attachment storage: %w", err)
	}
	var total int64
	for _, size := range sizes {
		total += int64(size)
	}
	return total, nil
}

// CheckUserCreate 校验“再建一个用户”是否超限；nil 接收者/未接入 → 放行。
func (s *TenantQuotaService) CheckUserCreate(ctx context.Context, tenantID int) error {
	if s == nil || s.client == nil {
		return nil
	}
	// RLS（enforce）：内联计数同样必须以目标租户为作用域；否则平台/服务商上下文中
	// 计数被策略收窄为 0 → **超限仍放行**（Q5 实锤：maxUsers 形同虚设）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	limits, err := s.LimitsOf(ctx, tenantID)
	if err != nil {
		return err
	}
	if limits.MaxUsers <= 0 {
		return nil
	}
	used, err := s.client.User.Query().Where(user.TenantID(tenantID)).Count(ctx)
	if err != nil {
		return fmt.Errorf("count tenant users: %w", err)
	}
	if exceeded := limits.CheckUsers(int64(used)); exceeded != nil {
		s.logQuotaExceeded(tenantID, exceeded)
		return exceeded
	}
	return nil
}

// CheckTicketCreate 校验“再建一张当月工单”是否超限。
func (s *TenantQuotaService) CheckTicketCreate(ctx context.Context, tenantID int) error {
	if s == nil || s.client == nil {
		return nil
	}
	// 同 CheckUserCreate：内联计数与目标租户作用域绑定（RLS enforce）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	limits, err := s.LimitsOf(ctx, tenantID)
	if err != nil {
		return err
	}
	if limits.MaxTicketsPerMonth <= 0 {
		return nil
	}
	used, err := s.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.CreatedAtGTE(monthStart(time.Now())),
			ticket.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count tenant tickets: %w", err)
	}
	if exceeded := limits.CheckTicketsThisMonth(int64(used)); exceeded != nil {
		s.logQuotaExceeded(tenantID, exceeded)
		return exceeded
	}
	return nil
}

// CheckStorageAdd 校验“再写入 addBytes 字节附件”是否超限。
func (s *TenantQuotaService) CheckStorageAdd(ctx context.Context, tenantID int, addBytes int64) error {
	if s == nil || s.client == nil {
		return nil
	}
	// 同 CheckUserCreate：内联存储计数与目标租户作用域绑定（RLS enforce）。
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	limits, err := s.LimitsOf(ctx, tenantID)
	if err != nil {
		return err
	}
	if limits.StorageLimitBytes() <= 0 {
		return nil
	}
	used, err := s.storageBytes(ctx, tenantID)
	if err != nil {
		return err
	}
	if exceeded := limits.CheckStorage(used, addBytes); exceeded != nil {
		s.logQuotaExceeded(tenantID, exceeded)
		return exceeded
	}
	return nil
}

func (s *TenantQuotaService) logQuotaExceeded(tenantID int, exceeded *tenantquota.ExceededError) {
	if s.logger == nil {
		return
	}
	s.logger.Warnw("tenant quota exceeded",
		"tenant_id", tenantID,
		"quota", exceeded.Quota,
		"limit", exceeded.Limit,
		"used", exceeded.Used,
	)
}

// monthStart 本自然月起点（服务器本地时区；部署口径见 02-deployment §5 TZ）。
func monthStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
}
