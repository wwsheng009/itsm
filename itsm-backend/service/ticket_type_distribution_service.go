package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/mspcontract"
	"itsm-backend/ent/mspcontractslabinding"
	"itsm-backend/ent/tickettype"
	"itsm-backend/ent/tickettypedistribution"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// D-TT-1 工单类型受控下发（方案 §4.5a，Accepted v1.11，P2-3）。
//
// 语义（对齐 workflow_template_distributions 先例 P2-①）：
//   - 物化：provider 源类型 → customer 租户内副本（INSERT-only，INV-1；code 撞车机械后缀 -p<providerID>）；
//   - 授权：mspguard（allocation + 客户 active）唯一入口；跨租户事务走 tenantctx.WithSystemBypass
//     （MSP 管理操作，"副本写入走系统特权上下文" §4.1b），行数据显式带 tenant_id；
//     单客户读面用客户作用域 ctx（对齐先例，RLS 语义一致）；
//   - 溯源：副本 origin 三元组（controlled / origin_provider_tenant_id / origin_type_id）+ 分发追踪行；
//   - 同步：编辑源（dirty）→ 预览 diff → 发布（INV-2 origin 守卫 + INV-3 受控字段白名单）；
//   - 解绑：副本转 native（INV-4 零 DELETE）；源删除 → 副本/记录标 orphaned 保留；
//   - SLA 联动（v1.11 修正 IM-28）：源类型 default_sla_id → 客户生效合同的 binding（P1-2 起 L1 权威通道；
//     原文"自动创建 grant"随并轨闸门升级为合同链路）。
type TicketTypeDistributionService struct {
	client *ent.Client
	guard  customerAccessChecker
	logger *zap.SugaredLogger
}

var (
	ErrTicketTypeDistributionInvalid  = errors.New("ticket type distribution: invalid request")
	ErrTicketTypeDistributionNotFound = errors.New("ticket type distribution: record not found")
	ErrTicketTypeDistributionConflict = errors.New("ticket type distribution: active distribution already exists")
	ErrTicketTypeSourceInvalid        = errors.New("ticket type distribution: source type invalid")
)

const (
	ticketTypeOriginNative     = "native"
	ticketTypeOriginControlled = "controlled"

	distSyncStatusSynced   = "synced"
	distSyncStatusDirty    = "dirty"
	distSyncStatusFailed   = "failed"
	distSyncStatusOrphaned = "orphaned"
	distSyncStatusUnbound  = "unbound"
)

func NewTicketTypeDistributionService(client *ent.Client, guard customerAccessChecker, logger *zap.SugaredLogger) *TicketTypeDistributionService {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &TicketTypeDistributionService{client: client, guard: guard, logger: logger}
}

// Distribute 下发源类型到目标客户（物化副本 + 溯源行 + SLA 联动 binding）。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/:id/distribute
func (s *TicketTypeDistributionService) Distribute(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, sourceTypeID int) (*dto.DistributeTicketTypeResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("类型下发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || sourceTypeID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrTicketTypeDistributionInvalid)
	}
	if s.guard == nil {
		return nil, fmt.Errorf("类型下发授权校验未配置")
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}

	// 源类型：provider 自有（非受控副本，防链式下发）
	src, err := s.client.TicketType.Get(ctx, sourceTypeID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("%w: 源类型不存在", ErrTicketTypeSourceInvalid)
		}
		return nil, err
	}
	if int(src.TenantID) != actor.HomeTenantID {
		return nil, fmt.Errorf("%w: 源类型不属于当前服务商", ErrTicketTypeSourceInvalid)
	}
	if src.Origin == ticketTypeOriginControlled {
		return nil, fmt.Errorf("%w: 受控副本不可作为下发源", ErrTicketTypeSourceInvalid)
	}

	sctx := tenantctx.WithSystemBypass(ctx)
	now := time.Now()

	// 同源同客户活跃分发拒绝（防重复物化；unbound/orphaned 历史行不阻塞重新下发）
	exists, err := s.client.TicketTypeDistribution.Query().
		Where(
			tickettypedistribution.TenantIDEQ(int64(customerTenantID)),
			tickettypedistribution.ProviderTenantIDEQ(int64(actor.HomeTenantID)),
			tickettypedistribution.SourceTypeIDEQ(int64(sourceTypeID)),
			tickettypedistribution.SyncStatusNotIn(distSyncStatusUnbound, distSyncStatusOrphaned),
		).
		Exist(sctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrTicketTypeDistributionConflict
	}

	tx, err := s.client.Tx(sctx)
	if err != nil {
		return nil, fmt.Errorf("开启类型下发事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txc := tx.Client()

	// ① 物化副本（INSERT-only，INV-1）：code 撞车 → 机械后缀 -p<providerID>（仅 code 去重，name 保持原名）
	copyCode, err := s.uniqueCopyCode(sctx, txc, src.Code, actor.HomeTenantID, customerTenantID)
	if err != nil {
		return nil, err
	}
	copyRow, err := txc.TicketType.Create().
		SetCode(copyCode).
		SetName(src.Name).
		SetDescription(src.Description).
		SetIcon(src.Icon).
		SetColor(src.Color).
		SetStatus("active").
		SetDefaultPriority(src.DefaultPriority).
		SetSortOrder(src.SortOrder).
		SetCustomFields(src.CustomFields).
		SetSLAEnabled(src.SLAEnabled).
		SetSLAExempt(src.SLAExempt).
		SetAutoAssignEnabled(src.AutoAssignEnabled).
		SetAssignmentRules(src.AssignmentRules).
		SetNotificationConfig(src.NotificationConfig).
		SetApprovalChain([]interface{}{}).
		SetPermissionConfig(map[string]interface{}{}).
		SetOrigin(ticketTypeOriginControlled).
		SetOriginProviderTenantID(int64(actor.HomeTenantID)).
		SetOriginTypeID(int64(src.ID)).
		SetControlledByProvider(true).
		SetSyncStatus(distSyncStatusSynced).
		SetLastSyncedAt(now).
		SetTenantID(int64(customerTenantID)).
		SetCreatedBy(int64(actor.UserID)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return nil, fmt.Errorf("创建类型副本失败: %w", err)
	}

	// ② 分发追踪行（每次分发一行，历史可追溯；对齐 workflow_template_distributions 先例）
	distRow, err := txc.TicketTypeDistribution.Create().
		SetTenantID(int64(customerTenantID)).
		SetProviderTenantID(int64(actor.HomeTenantID)).
		SetSourceTypeID(int64(sourceTypeID)).
		SetCopyTypeID(int64(copyRow.ID)).
		SetSyncStatus(distSyncStatusSynced).
		SetLastSyncedAt(now).
		SetDistributedBy(int64(actor.UserID)).
		SetDistributedAt(now).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return nil, fmt.Errorf("记录分发来源失败: %w", err)
	}

	// ③ SLA 联动（v1.11 修正：合同 binding 通道；无生效合同 → 跳过并提示）
	slaNote := "skipped_no_sla"
	if src.DefaultSLAID > 0 {
		slaNote, err = s.upsertDistributionBinding(sctx, txc, actor.HomeTenantID, customerTenantID, int64(copyRow.ID), src.DefaultSLAID, now)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交类型下发事务失败: %w", err)
	}

	s.recordDistributionAudit(ctx, actor, customerTenantID, "distribute", sourceTypeID, copyRow.ID, map[string]any{
		"copy_code":   copyCode,
		"sla_binding": slaNote,
		"outcome":     "success",
	})

	return &dto.DistributeTicketTypeResponse{
		Distribution: s.toDistributionItem(distRow, src, copyRow),
		SLABinding:   slaNote,
	}, nil
}

// uniqueCopyCode code 去重：base → base-p<providerID> → base-p<providerID>-N（INV-1 机械后缀，name 不动）。
func (s *TicketTypeDistributionService) uniqueCopyCode(ctx context.Context, txc *ent.Client, baseCode string, providerTenantID, customerTenantID int) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		code := baseCode
		switch {
		case attempt == 1:
			code = fmt.Sprintf("%s-p%d", baseCode, providerTenantID)
		case attempt >= 2:
			code = fmt.Sprintf("%s-p%d-%d", baseCode, providerTenantID, attempt)
		}
		exists, err := txc.TicketType.Query().
			Where(tickettype.TenantIDEQ(int64(customerTenantID)), tickettype.CodeEQ(code)).
			Exist(ctx)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("%w: code 冲突消解失败（%s）", ErrTicketTypeSourceInvalid, baseCode)
}

// upsertDistributionBinding SLA 联动：源类型 default_sla_id → 客户生效合同的 binding（INV：定义归属 provider）。
// 返回联动结果注记：created|updated|unchanged|skipped_no_contract。
func (s *TicketTypeDistributionService) upsertDistributionBinding(ctx context.Context, txc *ent.Client, providerTenantID, customerTenantID int, copyTypeID, slaDefinitionID int64, now time.Time) (string, error) {
	def, err := txc.SLADefinition.Get(ctx, int(slaDefinitionID))
	if err != nil {
		if ent.IsNotFound(err) {
			return "", fmt.Errorf("%w: 源类型关联的 SLA 定义不存在", ErrTicketTypeSourceInvalid)
		}
		return "", err
	}
	if int(def.TenantID) != providerTenantID {
		return "", fmt.Errorf("%w: 源类型关联的 SLA 定义不属于当前服务商", ErrTicketTypeSourceInvalid)
	}
	if !def.IsActive {
		return "", fmt.Errorf("%w: 源类型关联的 SLA 定义未激活", ErrTicketTypeSourceInvalid)
	}

	contract, err := txc.MSPContract.Query().
		Where(
			mspcontract.TenantIDEQ(providerTenantID),
			mspcontract.CustomerTenantIDEQ(customerTenantID),
			mspcontract.StatusEQ("active"),
			mspcontract.EffectiveFromLTE(now),
			mspcontract.Or(
				mspcontract.EffectiveUntilIsNil(),
				mspcontract.EffectiveUntilGT(now),
			),
		).
		Order(ent.Desc(mspcontract.FieldEffectiveFrom), ent.Desc(mspcontract.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "skipped_no_contract", nil
		}
		return "", err
	}

	existing, err := txc.MSPContractSLABinding.Query().
		Where(
			mspcontractslabinding.ContractIDEQ(contract.ID),
			mspcontractslabinding.TicketTypeIDEQ(copyTypeID),
		).
		First(ctx)
	if ent.IsNotFound(err) {
		if _, err := txc.MSPContractSLABinding.Create().
			SetContractID(contract.ID).
			SetSLADefinitionID(int(slaDefinitionID)).
			SetTicketTypeID(copyTypeID).
			SetTenantID(providerTenantID).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			Save(ctx); err != nil {
			return "", fmt.Errorf("创建合同 SLA 绑定失败: %w", err)
		}
		return "created", nil
	}
	if err != nil {
		return "", err
	}
	if int64(existing.SLADefinitionID) != slaDefinitionID {
		if _, err := txc.MSPContractSLABinding.UpdateOne(existing).
			SetSLADefinitionID(int(slaDefinitionID)).
			SetUpdatedAt(now).
			Save(ctx); err != nil {
			return "", fmt.Errorf("更新合同 SLA 绑定失败: %w", err)
		}
		return "updated", nil
	}
	return "unchanged", nil
}

// findActiveContract 客户当前生效合同（provider 归属；与 L1 解析同序）。
func (s *TicketTypeDistributionService) findActiveContract(ctx context.Context, txc *ent.Client, providerTenantID, customerTenantID int, now time.Time) (*ent.MSPContract, error) {
	contract, err := txc.MSPContract.Query().
		Where(
			mspcontract.TenantIDEQ(providerTenantID),
			mspcontract.CustomerTenantIDEQ(customerTenantID),
			mspcontract.StatusEQ("active"),
			mspcontract.EffectiveFromLTE(now),
			mspcontract.Or(
				mspcontract.EffectiveUntilIsNil(),
				mspcontract.EffectiveUntilGT(now),
			),
		).
		Order(ent.Desc(mspcontract.FieldEffectiveFrom), ent.Desc(mspcontract.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return contract, nil
}

// ListDistributions 某客户的分发列表（provider 视角；含"有可用更新"提示）。
// GET /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions
func (s *TicketTypeDistributionService) ListDistributions(ctx context.Context, actor MSPWorkbenchActor, customerTenantID int) ([]*dto.TicketTypeDistributionItem, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("类型下发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrTicketTypeDistributionInvalid)
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	rows, err := s.client.TicketTypeDistribution.Query().
		Where(
			tickettypedistribution.TenantIDEQ(int64(customerTenantID)),
			tickettypedistribution.ProviderTenantIDEQ(int64(actor.HomeTenantID)),
		).
		Order(ent.Desc(tickettypedistribution.FieldDistributedAt), ent.Desc(tickettypedistribution.FieldID)).
		All(sctx)
	if err != nil {
		return nil, fmt.Errorf("查询分发记录失败: %w", err)
	}
	sourceIDs := make([]int, 0, len(rows))
	copyIDs := make([]int, 0, len(rows))
	for _, r := range rows {
		sourceIDs = append(sourceIDs, int(r.SourceTypeID))
		copyIDs = append(copyIDs, int(r.CopyTypeID))
	}
	srcMap := s.loadTicketTypesByIDs(sctx, sourceIDs)
	copyMap := s.loadTicketTypesByIDs(sctx, copyIDs)

	items := make([]*dto.TicketTypeDistributionItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, s.toDistributionItem(r, srcMap[int(r.SourceTypeID)], copyMap[int(r.CopyTypeID)]))
	}
	return items, nil
}

// Preview 发布预览：受控字段 diff + SLA(binding) 变更（① 编辑源 → ② 预览）。
// GET /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/preview
func (s *TicketTypeDistributionService) Preview(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.TicketTypeDistributionPreview, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("类型下发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || distributionID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrTicketTypeDistributionInvalid)
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	dist, err := s.loadDistribution(sctx, actor.HomeTenantID, customerTenantID, distributionID)
	if err != nil {
		return nil, err
	}
	if dist.SyncStatus == distSyncStatusUnbound {
		return nil, fmt.Errorf("%w: 分发已解绑，无发布语义", ErrTicketTypeDistributionInvalid)
	}
	src, copyRow, err := s.loadSourceAndCopy(sctx, actor.HomeTenantID, dist)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	changes := controlledDiff(src, copyRow)
	slaAction, defFrom, defTo, note := s.computeSLAAction(sctx, actor.HomeTenantID, customerTenantID, dist.CopyTypeID, src, now)
	return &dto.TicketTypeDistributionPreview{
		DistributionID:      dist.ID,
		Changes:             changes,
		SLAAction:           slaAction,
		SLADefinitionIDFrom: defFrom,
		SLADefinitionIDTo:   defTo,
		UpToDate:            len(changes) == 0 && slaAction == "none" && dist.SyncStatus == distSyncStatusSynced,
		Note:                note,
	}, nil
}

// Publish 执行发布（按副本逐个事务；INV-2 origin 守卫 + INV-3 字段白名单；失败可重试收敛）。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/publish
func (s *TicketTypeDistributionService) Publish(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.PublishTicketTypeDistributionResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("类型下发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || distributionID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrTicketTypeDistributionInvalid)
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	now := time.Now()

	dist, err := s.loadDistribution(sctx, actor.HomeTenantID, customerTenantID, distributionID)
	if err != nil {
		return nil, err
	}
	if dist.SyncStatus == distSyncStatusUnbound {
		return nil, fmt.Errorf("%w: 分发已解绑，无发布语义", ErrTicketTypeDistributionInvalid)
	}
	src, _, err := s.loadSourceAndCopy(sctx, actor.HomeTenantID, dist)
	if err != nil {
		return nil, err
	}

	tx, err := s.client.Tx(sctx)
	if err != nil {
		return nil, fmt.Errorf("开启发布事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txc := tx.Client()

	// INV-2：同步 UPDATE 必带 origin 三元组守卫——native 行 SQL 层物理不可达。
	// INV-3：仅覆盖受控字段白名单（name/description/sla_enabled）。
	affected, err := txc.TicketType.Update().
		Where(
			tickettype.IDEQ(int(dist.CopyTypeID)),
			tickettype.OriginEQ(ticketTypeOriginControlled),
			tickettype.OriginProviderTenantIDEQ(int64(actor.HomeTenantID)),
			tickettype.OriginTypeIDEQ(dist.SourceTypeID),
		).
		SetName(src.Name).
		SetDescription(src.Description).
		SetSLAEnabled(src.SLAEnabled).
		SetSyncStatus(distSyncStatusSynced).
		SetLastSyncedAt(now).
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return nil, fmt.Errorf("发布受控字段失败: %w", err)
	}
	if affected == 0 {
		// 守卫不匹配：副本缺失或 provenance 被篡改——标 failed 可观测可重试。
		_, _ = txc.TicketTypeDistribution.UpdateOne(dist).
			SetSyncStatus(distSyncStatusFailed).
			SetUpdatedAt(now).
			Save(sctx)
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("提交发布失败标记失败: %w", err)
		}
		s.recordDistributionAudit(ctx, actor, customerTenantID, "publish", int(dist.SourceTypeID), int(dist.CopyTypeID), map[string]any{
			"outcome": "failed",
			"error":   "origin guard mismatch (INV-2)",
		})
		return &dto.PublishTicketTypeDistributionResponse{
			DistributionID: dist.ID,
			Status:         distSyncStatusFailed,
			Error:          "origin 守卫不匹配（INV-2）：副本缺失或来源被修改",
		}, nil
	}

	// SLA 联动：源类型关联定义 → 客户生效合同 binding（v1.11 修正 IM-28）
	slaAction := "none"
	if src.DefaultSLAID > 0 {
		slaAction, err = s.upsertDistributionBinding(sctx, txc, actor.HomeTenantID, customerTenantID, dist.CopyTypeID, src.DefaultSLAID, now)
		if err != nil {
			return nil, err
		}
	}

	// 分发行收敛（provider 侧状态）
	if _, err := txc.TicketTypeDistribution.UpdateOne(dist).
		SetSyncStatus(distSyncStatusSynced).
		SetLastSyncedAt(now).
		SetUpdatedAt(now).
		Save(sctx); err != nil {
		return nil, fmt.Errorf("更新分发状态失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交发布事务失败: %w", err)
	}

	s.recordDistributionAudit(ctx, actor, customerTenantID, "publish", int(dist.SourceTypeID), int(dist.CopyTypeID), map[string]any{
		"sla_action": slaAction,
		"outcome":    "success",
	})
	syncedAt := now
	return &dto.PublishTicketTypeDistributionResponse{
		DistributionID: dist.ID,
		Status:         distSyncStatusSynced,
		SLAAction:      slaAction,
		SyncedAt:       &syncedAt,
	}, nil
}

// Unbind 解绑：副本转 native（INV-4 零 DELETE；保留治理字段，受控字段解锁）+ 级联移除合同 binding。
// POST /api/v1/msp/customers/:customer_tenant_id/ticket-types/distributions/:distId/unbind
func (s *TicketTypeDistributionService) Unbind(ctx context.Context, actor MSPWorkbenchActor, customerTenantID, distributionID int) (*dto.UnbindTicketTypeDistributionResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("类型下发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || distributionID <= 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrTicketTypeDistributionInvalid)
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	now := time.Now()

	dist, err := s.loadDistribution(sctx, actor.HomeTenantID, customerTenantID, distributionID)
	if err != nil {
		return nil, err
	}
	if dist.SyncStatus == distSyncStatusUnbound {
		return nil, fmt.Errorf("%w: 分发已解绑", ErrTicketTypeDistributionInvalid)
	}

	tx, err := s.client.Tx(sctx)
	if err != nil {
		return nil, fmt.Errorf("开启解绑事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txc := tx.Client()

	affected, err := txc.TicketType.Update().
		Where(
			tickettype.IDEQ(int(dist.CopyTypeID)),
			tickettype.OriginEQ(ticketTypeOriginControlled),
			tickettype.OriginProviderTenantIDEQ(int64(actor.HomeTenantID)),
			tickettype.OriginTypeIDEQ(dist.SourceTypeID),
		).
		SetOrigin(ticketTypeOriginNative).
		SetOriginProviderTenantID(0).
		SetOriginTypeID(0).
		SetControlledByProvider(false).
		SetSyncStatus("").
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return nil, fmt.Errorf("副本转 native 失败: %w", err)
	}

	if _, err := txc.TicketTypeDistribution.UpdateOne(dist).
		SetSyncStatus(distSyncStatusUnbound).
		SetUpdatedAt(now).
		Save(sctx); err != nil {
		return nil, fmt.Errorf("更新分发状态失败: %w", err)
	}

	// 解绑级联（§4.5a）：移除下发创建的合同 SLA 绑定（授权失效；有则删，无则 no-op）
	bindingRemoved := false
	contract, err := s.findActiveContract(sctx, txc, actor.HomeTenantID, customerTenantID, now)
	if err != nil {
		return nil, err
	}
	if contract != nil {
		n, err := txc.MSPContractSLABinding.Delete().
			Where(
				mspcontractslabinding.ContractIDEQ(contract.ID),
				mspcontractslabinding.TicketTypeIDEQ(dist.CopyTypeID),
			).
			Exec(sctx)
		if err != nil {
			return nil, fmt.Errorf("移除合同 SLA 绑定失败: %w", err)
		}
		bindingRemoved = n > 0
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交解绑事务失败: %w", err)
	}

	s.recordDistributionAudit(ctx, actor, customerTenantID, "unbind", int(dist.SourceTypeID), int(dist.CopyTypeID), map[string]any{
		"released":        affected > 0,
		"binding_removed": bindingRemoved,
		"outcome":         "success",
	})
	return &dto.UnbindTicketTypeDistributionResponse{
		DistributionID: dist.ID,
		CopyTypeID:     dist.CopyTypeID,
		Released:       affected > 0,
		BindingRemoved: bindingRemoved,
	}, nil
}

// MarkDistributedCopiesDirty 源类型受控字段变更后标记全部活跃副本 dirty（"有可用更新"，§4.5a ①②）。
// 由 TicketTypeService 更新钩子调用；orphaned/unbound 不复活。
func (s *TicketTypeDistributionService) MarkDistributedCopiesDirty(ctx context.Context, providerTenantID, sourceTypeID int) (int, error) {
	if s == nil || s.client == nil {
		return 0, nil
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	now := time.Now()
	n, err := s.client.TicketTypeDistribution.Update().
		Where(
			tickettypedistribution.ProviderTenantIDEQ(int64(providerTenantID)),
			tickettypedistribution.SourceTypeIDEQ(int64(sourceTypeID)),
			tickettypedistribution.SyncStatusIn(distSyncStatusSynced, distSyncStatusDirty, distSyncStatusFailed),
		).
		SetSyncStatus(distSyncStatusDirty).
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return 0, err
	}
	if _, err := s.client.TicketType.Update().
		Where(
			tickettype.OriginEQ(ticketTypeOriginControlled),
			tickettype.OriginProviderTenantIDEQ(int64(providerTenantID)),
			tickettype.OriginTypeIDEQ(int64(sourceTypeID)),
			tickettype.SyncStatusIn(distSyncStatusSynced, distSyncStatusDirty, distSyncStatusFailed),
		).
		SetSyncStatus(distSyncStatusDirty).
		SetUpdatedAt(now).
		Save(sctx); err != nil {
		return int(n), err
	}
	return int(n), nil
}

// MarkSourceOrphaned 源类型删除（归档）后，副本与分发记录标 orphaned 保留（INV-4；零 DELETE）。
// 由 TicketTypeService 归档钩子调用。
func (s *TicketTypeDistributionService) MarkSourceOrphaned(ctx context.Context, providerTenantID, sourceTypeID int) (int, error) {
	if s == nil || s.client == nil {
		return 0, nil
	}
	sctx := tenantctx.WithSystemBypass(ctx)
	now := time.Now()
	n, err := s.client.TicketTypeDistribution.Update().
		Where(
			tickettypedistribution.ProviderTenantIDEQ(int64(providerTenantID)),
			tickettypedistribution.SourceTypeIDEQ(int64(sourceTypeID)),
			tickettypedistribution.SyncStatusIn(distSyncStatusSynced, distSyncStatusDirty, distSyncStatusFailed),
		).
		SetSyncStatus(distSyncStatusOrphaned).
		SetUpdatedAt(now).
		Save(sctx)
	if err != nil {
		return 0, err
	}
	if _, err := s.client.TicketType.Update().
		Where(
			tickettype.OriginEQ(ticketTypeOriginControlled),
			tickettype.OriginProviderTenantIDEQ(int64(providerTenantID)),
			tickettype.OriginTypeIDEQ(int64(sourceTypeID)),
			tickettype.SyncStatusIn(distSyncStatusSynced, distSyncStatusDirty, distSyncStatusFailed),
		).
		SetSyncStatus(distSyncStatusOrphaned).
		SetUpdatedAt(now).
		Save(sctx); err != nil {
		return int(n), err
	}
	return int(n), nil
}

// ---- helpers ----

func (s *TicketTypeDistributionService) loadDistribution(ctx context.Context, providerTenantID, customerTenantID, distributionID int) (*ent.TicketTypeDistribution, error) {
	row, err := s.client.TicketTypeDistribution.Query().
		Where(
			tickettypedistribution.IDEQ(distributionID),
			tickettypedistribution.TenantIDEQ(int64(customerTenantID)),
			tickettypedistribution.ProviderTenantIDEQ(int64(providerTenantID)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTicketTypeDistributionNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *TicketTypeDistributionService) loadSourceAndCopy(ctx context.Context, providerTenantID int, dist *ent.TicketTypeDistribution) (*ent.TicketType, *ent.TicketType, error) {
	src, err := s.client.TicketType.Get(ctx, int(dist.SourceTypeID))
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, fmt.Errorf("%w: 源类型不存在", ErrTicketTypeSourceInvalid)
		}
		return nil, nil, err
	}
	if int(src.TenantID) != providerTenantID {
		return nil, nil, fmt.Errorf("%w: 源类型不属于当前服务商", ErrTicketTypeSourceInvalid)
	}
	copyRow, err := s.client.TicketType.Get(ctx, int(dist.CopyTypeID))
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, ErrTicketTypeDistributionNotFound
		}
		return nil, nil, err
	}
	return src, copyRow, nil
}

func (s *TicketTypeDistributionService) loadTicketTypesByIDs(ctx context.Context, ids []int) map[int]*ent.TicketType {
	out := make(map[int]*ent.TicketType, len(ids))
	if len(ids) == 0 {
		return out
	}
	rows, err := s.client.TicketType.Query().Where(tickettype.IDIn(ids...)).All(ctx)
	if err != nil {
		return out // 摘要降级：缺失不阻断列表
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// controlledDiff 受控字段 diff（INV-3 白名单：name/description/sla_enabled）。
func controlledDiff(src, copyRow *ent.TicketType) []dto.TicketTypeDistributionChange {
	changes := make([]dto.TicketTypeDistributionChange, 0, 3)
	if src.Name != copyRow.Name {
		changes = append(changes, dto.TicketTypeDistributionChange{Field: "name", From: copyRow.Name, To: src.Name})
	}
	if src.Description != copyRow.Description {
		changes = append(changes, dto.TicketTypeDistributionChange{Field: "description", From: copyRow.Description, To: src.Description})
	}
	if src.SLAEnabled != copyRow.SLAEnabled {
		changes = append(changes, dto.TicketTypeDistributionChange{
			Field: "slaEnabled",
			From:  fmt.Sprintf("%t", copyRow.SLAEnabled),
			To:    fmt.Sprintf("%t", src.SLAEnabled),
		})
	}
	return changes
}

// computeSLAAction SLA 联动 diff：源 default_sla_id vs 客户生效合同上该副本的 binding。
func (s *TicketTypeDistributionService) computeSLAAction(ctx context.Context, providerTenantID, customerTenantID int, copyTypeID int64, src *ent.TicketType, now time.Time) (action string, from, to int64, note string) {
	to = src.DefaultSLAID
	contract, err := s.findActiveContract(ctx, s.client, providerTenantID, customerTenantID, now)
	if err != nil {
		return "none", 0, to, "读取客户合同失败: " + err.Error()
	}
	if contract == nil {
		if src.DefaultSLAID > 0 {
			return "none", 0, to, "客户无生效合同，SLA 联动暂缓（类型可先行下发）"
		}
		return "none", 0, to, ""
	}
	binding, err := s.client.MSPContractSLABinding.Query().
		Where(
			mspcontractslabinding.ContractIDEQ(contract.ID),
			mspcontractslabinding.TicketTypeIDEQ(copyTypeID),
		).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return "none", 0, to, "读取合同绑定失败: " + err.Error()
	}
	current := int64(0)
	if binding != nil {
		current = int64(binding.SLADefinitionID)
	}
	if src.DefaultSLAID <= 0 || src.DefaultSLAID == current {
		return "none", current, to, ""
	}
	if current == 0 {
		return "create", 0, to, ""
	}
	return "update", current, to, ""
}

func (s *TicketTypeDistributionService) toDistributionItem(dist *ent.TicketTypeDistribution, src, copyRow *ent.TicketType) *dto.TicketTypeDistributionItem {
	item := &dto.TicketTypeDistributionItem{
		ID:               dist.ID,
		CustomerTenantID: dist.TenantID,
		ProviderTenantID: dist.ProviderTenantID,
		SourceTypeID:     dist.SourceTypeID,
		CopyTypeID:       dist.CopyTypeID,
		SyncStatus:       dist.SyncStatus,
		DistributedBy:    dist.DistributedBy,
		DistributedAt:    dist.DistributedAt,
		UpdateAvailable:  dist.SyncStatus == distSyncStatusDirty,
	}
	if !dist.LastSyncedAt.IsZero() {
		t := dist.LastSyncedAt
		item.LastSyncedAt = &t
	}
	if src != nil {
		item.SourceTypeCode = src.Code
		item.SourceTypeName = src.Name
	}
	if copyRow != nil {
		item.CopyTypeCode = copyRow.Code
		item.CopyTypeName = copyRow.Name
	}
	return item
}

// recordDistributionAudit 下发/发布/解绑审计（best-effort；source=workbench + target_tenant）。
func (s *TicketTypeDistributionService) recordDistributionAudit(ctx context.Context, actor MSPWorkbenchActor, customerTenantID int, op string, sourceTypeID, copyTypeID int, payload map[string]any) {
	if s.client == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["source"] = "workbench"
	payload["op"] = "ticket_type." + op
	payload["actor_user_id"] = actor.UserID
	payload["actor_home_tenant_id"] = actor.HomeTenantID
	payload["target_tenant_id"] = customerTenantID
	payload["source_type_id"] = sourceTypeID
	payload["copy_type_id"] = copyTypeID
	body, _ := json.Marshal(payload)
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = customerTenantID
	}
	actx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(customerTenantID).
		SetSource(middleware.AuditSourceWorkbench).
		SetResource("ticket_type").
		SetAction("ticket_type." + op).
		SetPath("/api/v1/msp/customers/:customer_tenant_id/ticket-types").
		SetMethod("POST").
		SetStatusCode(200).
		SetRequestBody(string(body)).
		Save(actx); err != nil {
		s.logger.Warnw("ticket type distribution audit write failed", "error", err, "op", op, "customer_tenant_id", customerTenantID)
	}
}
