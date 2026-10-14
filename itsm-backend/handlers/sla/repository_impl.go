package sla

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/mspcontract"
	"itsm-backend/ent/mspcontractslabinding"
	"itsm-backend/ent/predicate"
	"itsm-backend/ent/slaalerthistory"
	"itsm-backend/ent/slaalertrule"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/sladefinitiongrant"
	"itsm-backend/ent/slametric"
	"itsm-backend/ent/slaviolation"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/tickettype"
	"itsm-backend/service"
)

type EntRepository struct {
	client *ent.Client
}

func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// Map ent SLADefinition to domain SLADefinition
func toSLADefinitionDomain(e *ent.SLADefinition) *SLADefinition {
	if e == nil {
		return nil
	}
	return &SLADefinition{
		ID:                    e.ID,
		Name:                  e.Name,
		Description:           e.Description,
		ServiceType:           e.ServiceType,
		Priority:              e.Priority,
		ResponseTime:          e.ResponseTime,
		ResolutionTime:        e.ResolutionTime,
		BusinessHours:         e.BusinessHours,
		EscalationRules:       e.EscalationRules,
		Conditions:            e.Conditions,
		IsActive:              e.IsActive,
		TenantID:              e.TenantID,
		OwnerProviderTenantID: e.OwnerProviderTenantID,
		CreatedAt:             e.CreatedAt,
		UpdatedAt:             e.UpdatedAt,
	}
}

func (r *EntRepository) CreateDefinition(ctx context.Context, s *SLADefinition) (*SLADefinition, error) {
	e, err := r.client.SLADefinition.Create().
		SetName(s.Name).
		SetDescription(s.Description).
		SetServiceType(s.ServiceType).
		SetPriority(s.Priority).
		SetResponseTime(s.ResponseTime).
		SetResolutionTime(s.ResolutionTime).
		SetBusinessHours(s.BusinessHours).
		SetEscalationRules(s.EscalationRules).
		SetConditions(s.Conditions).
		SetIsActive(s.IsActive).
		SetOwnerProviderTenantID(s.OwnerProviderTenantID).
		SetTenantID(s.TenantID).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLADefinitionDomain(e), nil
}

func (r *EntRepository) GetDefinition(ctx context.Context, id int, tenantID int) (*SLADefinition, error) {
	e, err := r.client.SLADefinition.Query().
		Where(sladefinition.ID(id), sladefinition.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return toSLADefinitionDomain(e), nil
}

func (r *EntRepository) ListDefinitions(ctx context.Context, tenantID int, page, size int) ([]*SLADefinition, int, error) {
	q := r.client.SLADefinition.Query().Where(sladefinition.TenantID(tenantID))
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	es, err := q.Limit(size).Offset((page - 1) * size).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	var results []*SLADefinition
	for _, e := range es {
		results = append(results, toSLADefinitionDomain(e))
	}
	return results, total, nil
}

func (r *EntRepository) UpdateDefinition(ctx context.Context, s *SLADefinition) (*SLADefinition, error) {
	e, err := r.client.SLADefinition.UpdateOneID(s.ID).
		Where(sladefinition.TenantID(s.TenantID)).
		SetName(s.Name).
		SetDescription(s.Description).
		SetServiceType(s.ServiceType).
		SetPriority(s.Priority).
		SetResponseTime(s.ResponseTime).
		SetResolutionTime(s.ResolutionTime).
		SetBusinessHours(s.BusinessHours).
		SetEscalationRules(s.EscalationRules).
		SetConditions(s.Conditions).
		SetIsActive(s.IsActive).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLADefinitionDomain(e), nil
}

func (r *EntRepository) DeleteDefinition(ctx context.Context, id int, tenantID int) error {
	_, err := r.client.SLADefinition.Delete().
		Where(sladefinition.ID(id), sladefinition.TenantID(tenantID)).
		Exec(ctx)
	return err
}

// --- SLA Definition Grants（D-SLA-1）---

func toSLADefinitionGrantDomain(e *ent.SLADefinitionGrant) *SLADefinitionGrant {
	if e == nil {
		return nil
	}
	g := &SLADefinitionGrant{
		ID:               e.ID,
		SLADefinitionID:  e.SLADefinitionID,
		CustomerTenantID: e.CustomerTenantID,
		ProviderTenantID: e.ProviderTenantID,
		TicketTypeID:     e.TicketTypeID,
		ValidUntil:       e.ValidUntil,
		IsActive:         e.IsActive,
		TenantID:         e.TenantID,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
	if def := e.Edges.SLADefinition; def != nil {
		g.DefinitionName = def.Name
	}
	return g
}

// CreateGrant 事务内：① customer 归属校验（msp_provider_id == provider）；
// ② 定义所有权校验（属于 provider）；③ deactivate-then-activate（同 customer+type 旧授权先停用）。
//
// 实施期修正 IM-10：FOR UPDATE 行锁因 ent 无方言判断入口暂缓（sqlite 测试不支持该语法），
// 以"事务内校验 + 解绑级联停用"双保险承载；PG 行锁加固随 P1 解绑编排同批评估。
func (r *EntRepository) CreateGrant(ctx context.Context, g *SLADefinitionGrant) (*SLADefinitionGrant, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// ① customer 归属校验
	customer, err := tx.Tenant.Query().Where(tenant.IDEQ(g.CustomerTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, common.NewBusinessError(common.NotFoundCode, "客户租户不存在", "")
		}
		return nil, err
	}
	if customer.MspProviderID != g.ProviderTenantID {
		return nil, common.NewBusinessError(common.ForbiddenCode,
			"客户租户不属于当前服务商（msp_provider_id 不匹配）", "")
	}

	// ② 定义所有权校验：定义必须归属 provider（tenant_id 或 owner_provider_tenant_id）
	def, err := tx.SLADefinition.Query().
		Where(sladefinition.IDEQ(g.SLADefinitionID), sladefinition.TenantIDEQ(g.ProviderTenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, common.NewBusinessError(common.NotFoundCode, "SLA 定义不存在或不属于当前服务商", "")
		}
		return nil, err
	}
	if def.OwnerProviderTenantID == 0 {
		// 兼容：provider 租户自建（owner 未显式标记）的定义同样可下发，但显式标记为佳。
		if _, err := tx.SLADefinition.UpdateOneID(def.ID).
			SetOwnerProviderTenantID(g.ProviderTenantID).Save(ctx); err != nil {
			return nil, err
		}
	}

	// ③ deactivate-then-activate：同 customer + 同 type（0=类型无关）的旧授权先停用
	if _, err := tx.SLADefinitionGrant.Update().
		Where(
			sladefinitiongrant.CustomerTenantIDEQ(g.CustomerTenantID),
			sladefinitiongrant.TicketTypeIDEQ(g.TicketTypeID),
			sladefinitiongrant.IsActiveEQ(true),
		).
		SetIsActive(false).
		Save(ctx); err != nil {
		return nil, err
	}

	created, err := tx.SLADefinitionGrant.Create().
		SetSLADefinitionID(g.SLADefinitionID).
		SetCustomerTenantID(g.CustomerTenantID).
		SetProviderTenantID(g.ProviderTenantID).
		SetTicketTypeID(g.TicketTypeID).
		SetNillableValidUntil(g.ValidUntil).
		SetIsActive(true).
		SetTenantID(g.TenantID).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toSLADefinitionGrantDomain(created), nil
}

func (r *EntRepository) ListGrants(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*SLADefinitionGrant, error) {
	q := r.client.SLADefinitionGrant.Query().
		Where(sladefinitiongrant.TenantIDEQ(providerTenantID)).
		WithSLADefinition().
		Order(ent.Desc(sladefinitiongrant.FieldID))
	if v, ok := filters["customerTenantId"].(int); ok && v > 0 {
		q = q.Where(sladefinitiongrant.CustomerTenantIDEQ(v))
	}
	if v, ok := filters["isActive"].(bool); ok {
		q = q.Where(sladefinitiongrant.IsActiveEQ(v))
	}
	if v, ok := filters["slaDefinitionId"].(int); ok && v > 0 {
		q = q.Where(sladefinitiongrant.SLADefinitionIDEQ(v))
	}
	es, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*SLADefinitionGrant, 0, len(es))
	for _, e := range es {
		out = append(out, toSLADefinitionGrantDomain(e))
	}
	return out, nil
}

func (r *EntRepository) GetGrant(ctx context.Context, id int, providerTenantID int) (*SLADefinitionGrant, error) {
	e, err := r.client.SLADefinitionGrant.Query().
		Where(sladefinitiongrant.IDEQ(id), sladefinitiongrant.TenantIDEQ(providerTenantID)).
		WithSLADefinition().
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return toSLADefinitionGrantDomain(e), nil
}

func (r *EntRepository) UpdateGrantValidUntil(ctx context.Context, id int, providerTenantID int, validUntil *time.Time) (*SLADefinitionGrant, error) {
	upd := r.client.SLADefinitionGrant.UpdateOneID(id).
		Where(sladefinitiongrant.TenantIDEQ(providerTenantID))
	if validUntil != nil {
		upd = upd.SetValidUntil(*validUntil)
	} else {
		upd = upd.ClearValidUntil()
	}
	e, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLADefinitionGrantDomain(e), nil
}

func (r *EntRepository) DeactivateGrant(ctx context.Context, id int, providerTenantID int) error {
	_, err := r.client.SLADefinitionGrant.Update().
		Where(sladefinitiongrant.IDEQ(id), sladefinitiongrant.TenantIDEQ(providerTenantID)).
		SetIsActive(false).
		Save(ctx)
	return err
}

// ListProvidedDefinitions customer 侧只读视图（P1-2 起数据源升级为合同链）：
// active 合同（合同期内）→ bindings → 定义（active）。grant 表不再参与读路径。
func (r *EntRepository) ListProvidedDefinitions(ctx context.Context, customerTenantID int) ([]*ProvidedDefinition, error) {
	now := time.Now()
	contracts, err := r.client.MSPContract.Query().
		Where(
			mspcontract.CustomerTenantIDEQ(customerTenantID),
			mspcontract.StatusEQ("active"),
			mspcontract.EffectiveFromLTE(now),
			mspcontract.Or(
				mspcontract.EffectiveUntilIsNil(),
				mspcontract.EffectiveUntilGT(now),
			),
		).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Order(ent.Desc(mspcontract.FieldEffectiveFrom), ent.Desc(mspcontract.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ProvidedDefinition, 0)
	for _, c := range contracts {
		for _, b := range c.Edges.SLABindings {
			def := b.Edges.SLADefinition
			if def == nil || !def.IsActive {
				continue
			}
			out = append(out, &ProvidedDefinition{
				ContractID:       c.ID,
				ContractNo:       c.ContractNo,
				SLADefinitionID:  def.ID,
				Name:             def.Name,
				Description:      def.Description,
				ServiceType:      def.ServiceType,
				Priority:         def.Priority,
				ResponseTime:     def.ResponseTime,
				ResolutionTime:   def.ResolutionTime,
				TicketTypeID:     b.TicketTypeID,
				ProviderTenantID: c.ProviderTenantID,
				ValidUntil:       c.EffectiveUntil,
			})
		}
	}
	return out, nil
}

// --- MSP SLA Contracts（D-SLA-4）---

func toMSPContractDomain(e *ent.MSPContract) *MSPContract {
	if e == nil {
		return nil
	}
	c := &MSPContract{
		ID:               e.ID,
		ProviderTenantID: e.ProviderTenantID,
		CustomerTenantID: e.CustomerTenantID,
		ContractNo:       e.ContractNo,
		Name:             e.Name,
		EffectiveFrom:    e.EffectiveFrom,
		EffectiveUntil:   e.EffectiveUntil,
		Status:           e.Status,
		Penalties:        e.Penalties,
		SettlementCycle:  e.SettlementCycle,
		Currency:         e.Currency,
		TenantID:         e.TenantID,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
	for _, b := range e.Edges.SLABindings {
		cb := MSPContractSLABinding{
			ID:              b.ID,
			ContractID:      b.ContractID,
			SLADefinitionID: b.SLADefinitionID,
			TicketTypeID:    b.TicketTypeID,
			TenantID:        b.TenantID,
		}
		if def := b.Edges.SLADefinition; def != nil {
			cb.DefinitionName = def.Name
		}
		c.Bindings = append(c.Bindings, cb)
	}
	return c
}

// createContractBindings 事务内批量创建绑定：每个绑定校验定义归属 provider
// （与 grant 的归属三件套同源；owner 未标记时补写）。
func createContractBindings(ctx context.Context, tx *ent.Tx, providerTenantID, contractID int, bindings []MSPContractSLABinding) error {
	if len(bindings) == 0 {
		return common.NewBusinessError(common.ParamErrorCode, "合同至少需要一条 SLA 定义绑定", "")
	}
	seen := make(map[int64]bool, len(bindings))
	for _, b := range bindings {
		if seen[b.TicketTypeID] {
			return common.NewBusinessError(common.ParamErrorCode, "同一工单类型只能绑定一条 SLA 定义", "")
		}
		seen[b.TicketTypeID] = true

		def, err := tx.SLADefinition.Query().
			Where(sladefinition.IDEQ(b.SLADefinitionID), sladefinition.TenantIDEQ(providerTenantID)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return common.NewBusinessError(common.NotFoundCode, "SLA 定义不存在或不属于当前服务商", "")
			}
			return err
		}
		if def.OwnerProviderTenantID == 0 {
			if _, err := tx.SLADefinition.UpdateOneID(def.ID).
				SetOwnerProviderTenantID(providerTenantID).Save(ctx); err != nil {
				return err
			}
		}
		if _, err := tx.MSPContractSLABinding.Create().
			SetContractID(contractID).
			SetSLADefinitionID(b.SLADefinitionID).
			SetTicketTypeID(b.TicketTypeID).
			SetTenantID(providerTenantID).
			Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *EntRepository) CreateContract(ctx context.Context, c *MSPContract) (*MSPContract, error) {
	if len(c.Bindings) == 0 {
		return nil, common.NewBusinessError(common.ParamErrorCode, "合同至少需要一条 SLA 定义绑定", "")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// ① customer 归属校验
	customer, err := tx.Tenant.Query().Where(tenant.IDEQ(c.CustomerTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, common.NewBusinessError(common.NotFoundCode, "客户租户不存在", "")
		}
		return nil, err
	}
	if customer.MspProviderID != c.ProviderTenantID {
		return nil, common.NewBusinessError(common.ForbiddenCode,
			"客户租户不属于当前服务商（msp_provider_id 不匹配）", "")
	}

	// ② contract_no 租户内唯一预检（唯一索引兜底；友好错误优先）
	exists, err := tx.MSPContract.Query().
		Where(mspcontract.TenantIDEQ(c.ProviderTenantID), mspcontract.ContractNoEQ(c.ContractNo)).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, common.NewBusinessError(common.ParamErrorCode, "合同编号已存在", "")
	}

	// ③ 创建合同
	builder := tx.MSPContract.Create().
		SetProviderTenantID(c.ProviderTenantID).
		SetCustomerTenantID(c.CustomerTenantID).
		SetContractNo(c.ContractNo).
		SetName(c.Name).
		SetNillableEffectiveUntil(c.EffectiveUntil).
		SetTenantID(c.ProviderTenantID)
	if !c.EffectiveFrom.IsZero() {
		builder = builder.SetEffectiveFrom(c.EffectiveFrom)
	}
	if c.Status != "" {
		builder = builder.SetStatus(c.Status)
	}
	if c.Penalties != nil {
		builder = builder.SetPenalties(c.Penalties)
	}
	if c.SettlementCycle != "" {
		builder = builder.SetSettlementCycle(c.SettlementCycle)
	}
	if c.Currency != "" {
		builder = builder.SetCurrency(c.Currency)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}

	// ④ bindings 批量创建（定义归属校验）
	if err := createContractBindings(ctx, tx, c.ProviderTenantID, created.ID, c.Bindings); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetContract(ctx, created.ID, c.ProviderTenantID)
}

func (r *EntRepository) ListContracts(ctx context.Context, providerTenantID int, filters map[string]interface{}) ([]*MSPContract, error) {
	q := r.client.MSPContract.Query().
		Where(mspcontract.TenantIDEQ(providerTenantID)).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Order(ent.Desc(mspcontract.FieldID))
	if v, ok := filters["customerTenantId"].(int); ok && v > 0 {
		q = q.Where(mspcontract.CustomerTenantIDEQ(v))
	}
	if v, ok := filters["status"].(string); ok && v != "" {
		q = q.Where(mspcontract.StatusEQ(v))
	}
	es, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*MSPContract, 0, len(es))
	for _, e := range es {
		out = append(out, toMSPContractDomain(e))
	}
	return out, nil
}

func (r *EntRepository) GetContract(ctx context.Context, id int, providerTenantID int) (*MSPContract, error) {
	e, err := r.client.MSPContract.Query().
		Where(mspcontract.IDEQ(id), mspcontract.TenantIDEQ(providerTenantID)).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return toMSPContractDomain(e), nil
}

func (r *EntRepository) UpdateContract(ctx context.Context, c *MSPContract) (*MSPContract, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	upd := tx.MSPContract.UpdateOneID(c.ID).
		Where(mspcontract.TenantIDEQ(c.ProviderTenantID))
	if c.Name != "" {
		upd = upd.SetName(c.Name)
	}
	if c.Status != "" {
		upd = upd.SetStatus(c.Status)
	}
	if c.EffectiveUntil != nil {
		upd = upd.SetEffectiveUntil(*c.EffectiveUntil)
	}
	if c.Penalties != nil {
		upd = upd.SetPenalties(c.Penalties)
	}
	if c.SettlementCycle != "" {
		upd = upd.SetSettlementCycle(c.SettlementCycle)
	}
	if c.Currency != "" {
		upd = upd.SetCurrency(c.Currency)
	}
	if _, err := upd.Save(ctx); err != nil {
		return nil, err
	}

	// Bindings 非 nil = 全量替换意图（先删后建，同事务）
	if c.Bindings != nil {
		if _, err := tx.MSPContractSLABinding.Delete().
			Where(
				mspcontractslabinding.ContractIDEQ(c.ID),
				mspcontractslabinding.TenantIDEQ(c.ProviderTenantID),
			).
			Exec(ctx); err != nil {
			return nil, err
		}
		if err := createContractBindings(ctx, tx, c.ProviderTenantID, c.ID, c.Bindings); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetContract(ctx, c.ID, c.ProviderTenantID)
}

func (r *EntRepository) FindActiveContractForCustomer(ctx context.Context, customerTenantID int, now time.Time) (*MSPContract, error) {
	e, err := r.client.MSPContract.Query().
		Where(
			mspcontract.CustomerTenantIDEQ(customerTenantID),
			mspcontract.StatusEQ("active"),
			mspcontract.EffectiveFromLTE(now),
			mspcontract.Or(
				mspcontract.EffectiveUntilIsNil(),
				mspcontract.EffectiveUntilGT(now),
			),
		).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Order(ent.Desc(mspcontract.FieldEffectiveFrom), ent.Desc(mspcontract.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toMSPContractDomain(e), nil
}

// --- Violations ---

func toSLAViolationDomain(e *ent.SLAViolation) *SLAViolation {
	if e == nil {
		return nil
	}
	var resolvedAt *time.Time
	if !e.ResolvedAt.IsZero() {
		t := e.ResolvedAt
		resolvedAt = &t
	}
	var waivedAt *time.Time
	if !e.WaivedAt.IsZero() {
		t := e.WaivedAt
		waivedAt = &t
	}
	return &SLAViolation{
		ID:              e.ID,
		CreatedBy:       e.CreatedBy,
		TicketID:        e.TicketID,
		SLADefinitionID: e.SLADefinitionID,
		SLAName:         e.SLAName,
		ViolationType:   e.ViolationType,
		ViolationTime:   e.ViolationTime,
		Description:     e.Description,
		Severity:        e.Severity,
		IsResolved:      e.IsResolved,
		ResolvedAt:      resolvedAt,
		ResolutionNotes: e.ResolutionNotes,
		ResolutionState: e.ResolutionState,
		DisputeReason:   e.DisputeReason,
		WaivedReason:    e.WaivedReason,
		WaivedBy:        e.WaivedBy,
		WaivedAt:        waivedAt,
		SettledFlag:     e.SettledFlag,
		TenantID:        e.TenantID,
	}
}

func (r *EntRepository) CreateViolation(ctx context.Context, v *SLAViolation) (*SLAViolation, error) {
	creator := r.client.SLAViolation.Create().
		SetCreatedBy(v.CreatedBy).
		SetTicketID(v.TicketID).
		SetSLADefinitionID(v.SLADefinitionID).
		SetViolationType(v.ViolationType).
		SetViolationTime(v.ViolationTime).
		SetDescription(v.Description).
		SetSeverity(v.Severity).
		SetIsResolved(v.IsResolved).
		SetResolutionNotes(v.ResolutionNotes).
		SetTenantID(v.TenantID)

	if v.ResolvedAt != nil {
		creator.SetResolvedAt(*v.ResolvedAt)
	}

	e, err := creator.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAViolationDomain(e), nil
}

func (r *EntRepository) ListViolations(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAViolation, int, error) {
	q := r.client.SLAViolation.Query().Where(slaviolation.TenantID(tenantID))
	if val, ok := filters["is_resolved"]; ok {
		q = q.Where(slaviolation.IsResolved(val.(bool)))
	}
	if val, ok := filters["severity"]; ok {
		q = q.Where(slaviolation.Severity(val.(string)))
	}
	if val, ok := filters["violation_type"]; ok {
		q = q.Where(slaviolation.ViolationType(val.(string)))
	}
	if val, ok := filters["sla_definition_id"]; ok {
		q = q.Where(slaviolation.SLADefinitionID(val.(int)))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	es, err := q.Limit(size).Offset((page - 1) * size).Order(ent.Desc(slaviolation.FieldViolationTime)).
		WithTicket(func(tq *ent.TicketQuery) {
			tq.Select(ticket.FieldTitle).Select(ticket.FieldTicketNumber).Select(ticket.FieldPriority)
		}).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var res []*SLAViolation
	for _, e := range es {
		d := toSLAViolationDomain(e)
		// 工单标题/编号/优先级来自 ticket edge（监控大屏列表展示需要）
		if t := e.Edges.Ticket; t != nil {
			d.TicketTitle = t.Title
			d.TicketNumber = t.TicketNumber
			d.TicketPriority = t.Priority
		}
		res = append(res, d)
	}
	return res, total, nil
}

func (r *EntRepository) UpdateViolationStatus(ctx context.Context, id int, isResolved bool, notes string, tenantID int) error {
	update := r.client.SLAViolation.UpdateOneID(id).
		Where(slaviolation.TenantID(tenantID)).
		SetIsResolved(isResolved).
		SetResolutionNotes(notes)
	if isResolved {
		update.SetResolvedAt(time.Now())
	}
	return update.Exec(ctx)
}

// GetViolationResolutionState 读取归责状态与结算锁定标记（D-SLA-8 状态机校验用）。
func (r *EntRepository) GetViolationResolutionState(ctx context.Context, id, tenantID int) (string, bool, error) {
	e, err := r.client.SLAViolation.Query().
		Where(slaviolation.IDEQ(id), slaviolation.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return "", false, err
	}
	return e.ResolutionState, e.SettledFlag, nil
}

// TransitionViolationResolutionState CAS 更新（WHERE resolution_state=expected AND settled_flag=false）。
// 返回受影响行数：0 表示状态已并发变更或已进结算快照（调用方按冲突拒绝）。
func (r *EntRepository) TransitionViolationResolutionState(ctx context.Context, id, tenantID int, expectedState, target, reason string, actorID int) (int, error) {
	upd := r.client.SLAViolation.Update().
		Where(
			slaviolation.IDEQ(id),
			slaviolation.TenantID(tenantID),
			slaviolation.ResolutionStateEQ(expectedState),
			slaviolation.SettledFlagEQ(false),
		).
		SetResolutionState(target).
		SetUpdatedAt(time.Now())
	switch target {
	case "disputed":
		upd = upd.SetDisputeReason(reason)
	case "waived":
		upd = upd.SetWaivedReason(reason).SetWaivedBy(actorID).SetWaivedAt(time.Now())
	}
	return upd.Save(ctx)
}

// --- Alert Rules ---

func toSLAAlertRuleDomain(e *ent.SLAAlertRule) *SLAAlertRule {
	if e == nil {
		return nil
	}
	return &SLAAlertRule{
		ID:                   e.ID,
		SLADefinitionID:      e.SLADefinitionID,
		Name:                 e.Name,
		ThresholdPercentage:  e.ThresholdPercentage,
		AlertLevel:           e.AlertLevel,
		NotificationChannels: e.NotificationChannels,
		IsActive:             e.IsActive,
		TenantID:             e.TenantID,
	}
}

func (r *EntRepository) CreateAlertRule(ctx context.Context, ar *SLAAlertRule) (*SLAAlertRule, error) {
	e, err := r.client.SLAAlertRule.Create().
		SetSLADefinitionID(ar.SLADefinitionID).
		SetName(ar.Name).
		SetThresholdPercentage(ar.ThresholdPercentage).
		SetAlertLevel(ar.AlertLevel).
		SetNotificationChannels(ar.NotificationChannels).
		SetIsActive(ar.IsActive).
		SetTenantID(ar.TenantID).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAAlertRuleDomain(e), nil
}

func (r *EntRepository) GetAlertRule(ctx context.Context, id int, tenantID int) (*SLAAlertRule, error) {
	e, err := r.client.SLAAlertRule.Query().Where(slaalertrule.ID(id), slaalertrule.TenantID(tenantID)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAAlertRuleDomain(e), nil
}

func (r *EntRepository) ListAlertRules(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAAlertRule, error) {
	q := r.client.SLAAlertRule.Query().Where(slaalertrule.TenantID(tenantID))
	if val, ok := filters["sla_definition_id"]; ok {
		q = q.Where(slaalertrule.SLADefinitionID(val.(int)))
	}
	es, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	var res []*SLAAlertRule
	for _, e := range es {
		res = append(res, toSLAAlertRuleDomain(e))
	}
	return res, nil
}

func (r *EntRepository) UpdateAlertRule(ctx context.Context, ar *SLAAlertRule) (*SLAAlertRule, error) {
	e, err := r.client.SLAAlertRule.UpdateOneID(ar.ID).
		Where(slaalertrule.TenantID(ar.TenantID)).
		SetName(ar.Name).
		SetThresholdPercentage(ar.ThresholdPercentage).
		SetAlertLevel(ar.AlertLevel).
		SetNotificationChannels(ar.NotificationChannels).
		SetIsActive(ar.IsActive).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAAlertRuleDomain(e), nil
}

func (r *EntRepository) DeleteAlertRule(ctx context.Context, id int, tenantID int) error {
	_, err := r.client.SLAAlertRule.Delete().Where(slaalertrule.ID(id), slaalertrule.TenantID(tenantID)).Exec(ctx)
	return err
}

// --- Alert History ---

func toSLAAlertHistoryDomain(e *ent.SLAAlertHistory) *SLAAlertHistory {
	if e == nil {
		return nil
	}
	var resolvedAt *time.Time
	if !e.ResolvedAt.IsZero() {
		t := e.ResolvedAt
		resolvedAt = &t
	}
	return &SLAAlertHistory{
		ID:                  e.ID,
		AlertRuleID:         e.AlertRuleID,
		TicketID:            e.TicketID,
		TicketNumber:        e.TicketNumber,
		TicketTitle:         e.TicketTitle,
		AlertLevel:          e.AlertLevel,
		ThresholdPercentage: e.ThresholdPercentage,
		ActualPercentage:    e.ActualPercentage,
		NotificationSent:    e.NotificationSent,
		CreatedAt:           e.CreatedAt,
		ResolvedAt:          resolvedAt,
		TenantID:            e.TenantID,
	}
}

func (r *EntRepository) CreateAlertHistory(ctx context.Context, h *SLAAlertHistory) (*SLAAlertHistory, error) {
	creator := r.client.SLAAlertHistory.Create().
		SetAlertRuleID(h.AlertRuleID).
		SetTicketID(h.TicketID).
		SetTicketNumber(h.TicketNumber).
		SetTicketTitle(h.TicketTitle).
		SetAlertLevel(h.AlertLevel).
		SetThresholdPercentage(h.ThresholdPercentage).
		SetActualPercentage(h.ActualPercentage).
		SetNotificationSent(h.NotificationSent).
		SetTenantID(h.TenantID)

	if h.ResolvedAt != nil {
		creator.SetResolvedAt(*h.ResolvedAt)
	}

	e, err := creator.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAAlertHistoryDomain(e), nil
}

func (r *EntRepository) ListAlertHistory(ctx context.Context, tenantID int, page, size int, filters map[string]interface{}) ([]*SLAAlertHistory, int, error) {
	q := r.client.SLAAlertHistory.Query().Where(slaalerthistory.TenantID(tenantID))
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	es, err := q.Limit(size).Offset((page - 1) * size).Order(ent.Desc(slaalerthistory.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var res []*SLAAlertHistory
	for _, e := range es {
		res = append(res, toSLAAlertHistoryDomain(e))
	}
	return res, total, nil
}

// --- Metrics ---

func toSLAMetricDomain(e *ent.SLAMetric) *SLAMetric {
	if e == nil {
		return nil
	}
	return &SLAMetric{
		ID:              e.ID,
		SLADefinitionID: e.SLADefinitionID,
		MetricType:      e.MetricType,
		MetricName:      e.MetricName,
		MetricValue:     e.MetricValue,
		Unit:            e.Unit,
		MeasurementTime: e.MeasurementTime,
		TenantID:        e.TenantID,
	}
}

func (r *EntRepository) CreateMetric(ctx context.Context, m *SLAMetric) (*SLAMetric, error) {
	e, err := r.client.SLAMetric.Create().
		SetSLADefinitionID(m.SLADefinitionID).
		SetMetricType(m.MetricType).
		SetMetricName(m.MetricName).
		SetMetricValue(m.MetricValue).
		SetUnit(m.Unit).
		SetMeasurementTime(m.MeasurementTime).
		SetTenantID(m.TenantID).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return toSLAMetricDomain(e), nil
}

func (r *EntRepository) GetMetrics(ctx context.Context, tenantID int, filters map[string]interface{}) ([]*SLAMetric, error) {
	q := r.client.SLAMetric.Query().Where(slametric.TenantID(tenantID))
	if val, ok := filters["sla_definition_id"]; ok {
		q = q.Where(slametric.SLADefinitionID(val.(int)))
	}
	if val, ok := filters["metric_type"]; ok {
		q = q.Where(slametric.MetricType(val.(string)))
	}
	es, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	var res []*SLAMetric
	for _, e := range es {
		res = append(res, toSLAMetricDomain(e))
	}
	return res, nil
}

// monitoringScanLimit 是单次聚合允许扫描的工单上限。命中上限时 truncated=true，
// 大屏必须提示样本不完整，而不是把偏小的分母当成全量真相。
const monitoringScanLimit = 20000

// monitoringAlertListLimit 大屏告警表最多返回的明细条数；activeAlerts 仍是全量计数。
const monitoringAlertListLimit = 50

// ticketResolvedStatuses 计入「已解决」的工单状态。以生命周期状态为权威口径，
// 不用 resolved_at 猜测，因为工单可以从 resolved 回到 in_progress 而时间戳仍保留。
var ticketResolvedStatuses = map[string]bool{
	common.TicketStatusResolved: true,
	common.TicketStatusClosed:   true,
}

// ticketFinishedStatuses 表示工单已无需继续响应/解决，用于风险判定。
var ticketFinishedStatuses = map[string]bool{
	common.TicketStatusResolved:  true,
	common.TicketStatusClosed:    true,
	common.TicketStatusCancelled: true,
}

// slaCohortTicket 是聚合所需的最小工单列集合。
type slaCohortTicket struct {
	id                    int
	status                string
	priority              string
	slaDefinitionID       int
	createdAt             time.Time
	firstResponseAt       time.Time
	resolvedAt            time.Time
	slaResponseDeadline   time.Time
	slaResolutionDeadline time.Time
}

// slaCohort 是监控大屏与绩效聚合共用的统计种群快照。
type slaCohort struct {
	tickets      []slaCohortTicket
	serviceTypes map[int]string // slaDefinitionID -> service_type
	violated     map[int]bool   // 当前仍有未解决违约的工单ID
	truncated    bool
}

// loadSLACohort 按租户 + 时间窗口加载统计种群，并可选按服务类型 / 优先级预过滤。
//
// 服务类型不是工单字段，它来自工单绑定的 SLA 定义，因此服务类型过滤会先解析出该
// 类型下的 SLA 定义ID集合，再作为工单谓词下推；本租户不存在该服务类型时返回空种群
// （而不是回退到全部工单）。
func (r *EntRepository) loadSLACohort(
	ctx context.Context,
	tenantID int,
	start, end time.Time,
	serviceTypeFilter, priorityFilter string,
) (*slaCohort, error) {
	defRows, err := r.client.SLADefinition.Query().
		Where(sladefinition.TenantID(tenantID)).
		Select(sladefinition.FieldID, sladefinition.FieldServiceType).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load sla definitions: %w", err)
	}

	serviceTypes := make(map[int]string, len(defRows))
	var filterDefinitionIDs []int
	for _, d := range defRows {
		serviceTypes[d.ID] = strings.TrimSpace(d.ServiceType)
		if serviceTypeFilter != "" && serviceTypes[d.ID] == serviceTypeFilter {
			filterDefinitionIDs = append(filterDefinitionIDs, d.ID)
		}
	}

	cohort := &slaCohort{serviceTypes: serviceTypes, violated: map[int]bool{}}

	predicates := []predicate.Ticket{
		ticket.TenantID(tenantID),
		ticket.DeletedAtIsNil(),
		ticket.CreatedAtGTE(start),
		ticket.CreatedAtLTE(end),
	}
	if priorityFilter != "" {
		predicates = append(predicates, ticket.PriorityEQ(priorityFilter))
	}
	if serviceTypeFilter != "" {
		if serviceTypeFilter == SLAPerformanceUnassignedKey {
			predicates = append(predicates, ticket.SLADefinitionIDIsNil())
		} else {
			if len(filterDefinitionIDs) == 0 {
				return cohort, nil
			}
			predicates = append(predicates, ticket.SLADefinitionIDIn(filterDefinitionIDs...))
		}
	}

	rows, err := r.client.Ticket.Query().
		Where(predicates...).
		Select(
			ticket.FieldID,
			ticket.FieldStatus,
			ticket.FieldPriority,
			ticket.FieldSLADefinitionID,
			ticket.FieldCreatedAt,
			ticket.FieldFirstResponseAt,
			ticket.FieldResolvedAt,
			ticket.FieldSLAResponseDeadline,
			ticket.FieldSLAResolutionDeadline,
		).
		Limit(monitoringScanLimit + 1).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load ticket cohort: %w", err)
	}

	cohort.tickets = make([]slaCohortTicket, 0, len(rows))
	for _, t := range rows {
		cohort.tickets = append(cohort.tickets, slaCohortTicket{
			id:                    t.ID,
			status:                t.Status,
			priority:              t.Priority,
			slaDefinitionID:       t.SLADefinitionID,
			createdAt:             t.CreatedAt,
			firstResponseAt:       t.FirstResponseAt,
			resolvedAt:            t.ResolvedAt,
			slaResponseDeadline:   t.SLAResponseDeadline,
			slaResolutionDeadline: t.SLAResolutionDeadline,
		})
	}
	if len(cohort.tickets) > monitoringScanLimit {
		cohort.tickets = cohort.tickets[:monitoringScanLimit]
		cohort.truncated = true
	}
	if len(cohort.tickets) == 0 {
		return cohort, nil
	}

	ids := make([]int, 0, len(cohort.tickets))
	for _, t := range cohort.tickets {
		ids = append(ids, t.id)
	}
	var groups []struct {
		TicketID int `json:"ticket_id"` // json tag matches SQL column for ent GroupBy().Scan()
	}
	if err := r.client.SLAViolation.Query().
		Where(
			slaviolation.TenantID(tenantID),
			slaviolation.IsResolved(false),
			slaviolation.TicketIDIn(ids...),
		).
		GroupBy(slaviolation.FieldTicketID).
		Scan(ctx, &groups); err != nil {
		return nil, fmt.Errorf("load violated tickets: %w", err)
	}
	for _, g := range groups {
		cohort.violated[g.TicketID] = true
	}

	return cohort, nil
}

// slaAggregation 是种群上的可加计数；按维度分组时每个分组一份。
type slaAggregation struct {
	total       int
	resolved    int
	violated    int
	atRisk      int
	respSamples int
	respMet     int
	resSamples  int
	resMet      int
	respDur     time.Duration
	respDurN    int
	resDur      time.Duration
	resDurN     int
}

// add 把一张工单计入聚合。所有判定只依赖数据库中的权威时间戳与状态，
// 截止时间缺失时不计入对应样本，避免把“未配置 SLA”当成违约或达标。
func (a *slaAggregation) add(t slaCohortTicket, violated bool, now time.Time) {
	a.total++
	if violated {
		a.violated++
	}
	if ticketResolvedStatuses[t.status] {
		a.resolved++
	}

	if !t.slaResponseDeadline.IsZero() && !t.firstResponseAt.IsZero() {
		a.respSamples++
		if !t.firstResponseAt.After(t.slaResponseDeadline) {
			a.respMet++
		}
	}
	if !t.slaResolutionDeadline.IsZero() && !t.resolvedAt.IsZero() {
		a.resSamples++
		if !t.resolvedAt.After(t.slaResolutionDeadline) {
			a.resMet++
		}
	}
	if !t.createdAt.IsZero() && !t.firstResponseAt.IsZero() {
		a.respDur += t.firstResponseAt.Sub(t.createdAt)
		a.respDurN++
	}
	if !t.createdAt.IsZero() && !t.resolvedAt.IsZero() {
		a.resDur += t.resolvedAt.Sub(t.createdAt)
		a.resDurN++
	}

	if ticketFinishedStatuses[t.status] {
		return
	}
	overdueResponse := !t.slaResponseDeadline.IsZero() &&
		t.slaResponseDeadline.Before(now) &&
		(t.firstResponseAt.IsZero() || t.firstResponseAt.After(t.slaResponseDeadline))
	overdueResolution := !t.slaResolutionDeadline.IsZero() && t.slaResolutionDeadline.Before(now)
	if overdueResponse || overdueResolution {
		a.atRisk++
	}
}

func (a *slaAggregation) toMonitoringMetrics(data *SLAMonitoringData) {
	data.TotalTickets = a.total
	data.ResolvedTickets = a.resolved
	data.ResolutionRate = ratePercent(a.resolved, a.total)
	data.ViolatedTickets = a.violated
	data.MetSlaTickets = a.total - a.violated
	data.ComplianceRate = ratePercent(a.total-a.violated, a.total)
	data.ViolationRate = ratePercent(a.violated, a.total)
	data.AtRiskTickets = a.atRisk
	data.ResponseTimeSamples = a.respSamples
	data.ResponseTimeMet = a.respMet
	data.ResponseTimeCompliance = ratePercent(a.respMet, a.respSamples)
	data.ResolutionTimeSamples = a.resSamples
	data.ResolutionTimeMet = a.resMet
	data.ResolutionTimeCompliance = ratePercent(a.resMet, a.resSamples)
	data.AverageResponseMinutes = averageMinutes(a.respDur, a.respDurN)
	data.AverageResolutionMinutes = averageMinutes(a.resDur, a.resDurN)
}

// ratePercent 计算百分数并保留一位小数；样本为 0 时诚实返回 0，
// 由前端根据对应的样本数量字段渲染“暂无样本”，不得回退成 100%。
func ratePercent(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Round(float64(part)/float64(whole)*1000) / 10
}

// averageMinutes 把累计时长换算成分钟平均值并保留一位小数；无样本时返回 0。
func averageMinutes(total time.Duration, count int) float64 {
	if count <= 0 {
		return 0
	}
	perTicket := float64(total) / float64(count)
	return math.Round((perTicket/float64(time.Minute))*10) / 10
}

// GetSLAMonitoring 返回监控大屏的完整指标快照。
//
// 与旧实现的区别：严格遵守传入的时间窗口（旧实现接受了但从未使用），
// 合规率不再在零样本时返回 1.0，告警数量来自真实的未解决 SLAAlertHistory。
func (r *EntRepository) GetSLAMonitoring(ctx context.Context, tenantID int, start, end time.Time) (*SLAMonitoringData, error) {
	cohort, err := r.loadSLACohort(ctx, tenantID, start, end, "", "")
	if err != nil {
		return nil, err
	}

	agg := &slaAggregation{}
	for _, t := range cohort.tickets {
		agg.add(t, cohort.violated[t.id], time.Now())
	}

	data := &SLAMonitoringData{
		StartTime: start.UTC().Format(time.RFC3339),
		EndTime:   end.UTC().Format(time.RFC3339),
		Truncated: cohort.truncated,
		Alerts:    []*SLAAlertItem{},
	}
	agg.toMonitoringMetrics(data)

	// 违约记录计数（记录数，与上面的工单数口径不同）
	totalViolations, err := r.client.SLAViolation.Query().Where(
		slaviolation.TenantID(tenantID),
		slaviolation.ViolationTimeGTE(start),
		slaviolation.ViolationTimeLTE(end),
	).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count violations: %w", err)
	}
	resolvedViolations, err := r.client.SLAViolation.Query().Where(
		slaviolation.TenantID(tenantID),
		slaviolation.ViolationTimeGTE(start),
		slaviolation.ViolationTimeLTE(end),
		slaviolation.IsResolved(true),
	).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count resolved violations: %w", err)
	}
	data.TotalViolations = totalViolations
	data.ResolvedViolations = resolvedViolations
	data.ActiveViolations = totalViolations - resolvedViolations

	activeSlas, err := r.client.SLADefinition.Query().Where(
		sladefinition.TenantID(tenantID),
		sladefinition.IsActive(true),
	).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count active sla definitions: %w", err)
	}
	data.ActiveSlas = activeSlas

	activeAlertRules, err := r.client.SLAAlertRule.Query().Where(
		slaalertrule.TenantID(tenantID),
		slaalertrule.IsActive(true),
	).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count alert rules: %w", err)
	}
	data.ActiveAlertRules = activeAlertRules

	// 告警一经触发就是“当前活跃”，因此只按窗口结束时刻截断，
	// 不用开始时间把更早触发但仍未解决的告警藏起来。
	alertPredicates := []predicate.SLAAlertHistory{
		slaalerthistory.TenantID(tenantID),
		slaalerthistory.ResolvedAtIsNil(),
		slaalerthistory.CreatedAtLTE(end),
	}
	activeAlerts, err := r.client.SLAAlertHistory.Query().Where(alertPredicates...).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count active alerts: %w", err)
	}
	data.ActiveAlerts = activeAlerts

	alerts, err := r.listSLAAlertItems(ctx, tenantID, alertPredicates, monitoringAlertListLimit)
	if err != nil {
		return nil, err
	}
	data.Alerts = alerts

	return data, nil
}

// listSLAAlertItems 把告警历史映射为大屏告警行，并补齐工单优先级与剩余时间。
func (r *EntRepository) listSLAAlertItems(
	ctx context.Context,
	tenantID int,
	alertPredicates []predicate.SLAAlertHistory,
	limit int,
) ([]*SLAAlertItem, error) {
	es, err := r.client.SLAAlertHistory.Query().
		Where(alertPredicates...).
		Order(ent.Desc(slaalerthistory.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active alerts: %w", err)
	}
	items := make([]*SLAAlertItem, 0, len(es))
	if len(es) == 0 {
		return items, nil
	}

	ticketIDs := make([]int, 0, len(es))
	seen := make(map[int]bool, len(es))
	for _, e := range es {
		if !seen[e.TicketID] {
			seen[e.TicketID] = true
			ticketIDs = append(ticketIDs, e.TicketID)
		}
	}
	// 只加载本租户的工单列，跨租户的脏数据不得泄露优先级与截止时间。
	tks, err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.IDIn(ticketIDs...)).
		Select(ticket.FieldID, ticket.FieldPriority, ticket.FieldSLAResolutionDeadline).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load alert tickets: %w", err)
	}
	ticketByID := make(map[int]*ent.Ticket, len(tks))
	for _, t := range tks {
		ticketByID[t.ID] = t
	}

	now := time.Now()
	for _, e := range es {
		item := &SLAAlertItem{
			ID:            e.ID,
			TicketID:      e.TicketID,
			TicketNumber:  e.TicketNumber,
			TicketTitle:   e.TicketTitle,
			AlertLevel:    e.AlertLevel,
			AlertRuleName: e.AlertRuleName,
			ThresholdPct:  e.ThresholdPercentage,
			ActualPct:     e.ActualPercentage,
			CreatedAt:     e.CreatedAt,
			Priority:      "",
		}
		if t, ok := ticketByID[e.TicketID]; ok {
			item.Priority = t.Priority
			if !t.SLAResolutionDeadline.IsZero() {
				item.TimeRemaining = &SLATimeRemaining{
					Hours:    math.Round(t.SLAResolutionDeadline.Sub(now).Hours()*10) / 10,
					Deadline: t.SLAResolutionDeadline.UTC().Format(time.RFC3339),
				}
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// performanceKeyOf 计算工单在指定维度下的分组 key。
// 未绑定 SLA 定义或未填写服务类型的工单归入 unassigned，禁止静默丢弃。
func performanceKeyOf(dimension string, t slaCohortTicket, serviceTypes map[int]string) string {
	if dimension == SLADimensionPriority {
		if strings.TrimSpace(t.priority) == "" {
			return common.PriorityMedium
		}
		return t.priority
	}
	if t.slaDefinitionID == 0 {
		return SLAPerformanceUnassignedKey
	}
	if st := strings.TrimSpace(serviceTypes[t.slaDefinitionID]); st != "" {
		return st
	}
	return SLAPerformanceUnassignedKey
}

// ListSLAPerformance 按维度聚合窗口内工单绩效，返回按工单数降序、key 升序排列的行。
func (r *EntRepository) ListSLAPerformance(
	ctx context.Context,
	tenantID int,
	dimension string,
	start, end time.Time,
	serviceTypeFilter, priorityFilter string,
) ([]*SLAPerformanceRow, bool, error) {
	cohort, err := r.loadSLACohort(ctx, tenantID, start, end, serviceTypeFilter, priorityFilter)
	if err != nil {
		return nil, false, err
	}

	now := time.Now()
	grouped := make(map[string]*slaAggregation, len(cohort.tickets))
	for _, t := range cohort.tickets {
		key := performanceKeyOf(dimension, t, cohort.serviceTypes)
		agg, ok := grouped[key]
		if !ok {
			agg = &slaAggregation{}
			grouped[key] = agg
		}
		agg.add(t, cohort.violated[t.id], now)
	}

	rows := make([]*SLAPerformanceRow, 0, len(grouped))
	for key, agg := range grouped {
		rows = append(rows, &SLAPerformanceRow{
			Key:                       key,
			TotalTickets:              agg.total,
			ResolvedTickets:           agg.resolved,
			ResolutionRate:            ratePercent(agg.resolved, agg.total),
			ViolatedTickets:           agg.violated,
			MetSlaTickets:             agg.total - agg.violated,
			ComplianceRate:            ratePercent(agg.total-agg.violated, agg.total),
			ResponseSamples:           agg.respSamples,
			ResponseAchievementRate:   ratePercent(agg.respMet, agg.respSamples),
			ResolutionSamples:         agg.resSamples,
			ResolutionAchievementRate: ratePercent(agg.resMet, agg.resSamples),
			AverageResponseMinutes:    averageMinutes(agg.respDur, agg.respDurN),
			AverageResolutionMinutes:  averageMinutes(agg.resDur, agg.resDurN),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalTickets != rows[j].TotalTickets {
			return rows[i].TotalTickets > rows[j].TotalTickets
		}
		return rows[i].Key < rows[j].Key
	})

	return rows, cohort.truncated, nil
}

// ===== P2-2 结算报表（D-SLA-5/D-SLA-9，方案 §4.5/§13）=====

// GetSettlementScope 结算取数。归属校验先于任何数据读取（越权 fail-closed，
// 响应不携带任何计数——SLA-A6）；工单窗口 = 报告窗口 ∩ 合同有效期（白名单条件④）。
func (r *EntRepository) GetSettlementScope(ctx context.Context, providerTenantID, customerTenantID int, from, to time.Time) (*SettlementScope, error) {
	// ① customer 归属校验（取数前）
	customer, err := r.client.Tenant.Query().Where(tenant.IDEQ(customerTenantID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, common.NewBusinessError(common.NotFoundCode, "客户租户不存在", "")
		}
		return nil, err
	}
	if customer.MspProviderID != providerTenantID {
		return nil, common.NewBusinessError(common.ForbiddenCode,
			"客户租户不属于当前服务商（msp_provider_id 不匹配）", "")
	}

	// ② 报告期内生效合同（最新生效优先；与 L1 匹配同序）
	contract, err := r.client.MSPContract.Query().
		Where(
			mspcontract.TenantIDEQ(providerTenantID),
			mspcontract.CustomerTenantIDEQ(customerTenantID),
			mspcontract.StatusEQ("active"),
			mspcontract.EffectiveFromLTE(to),
			mspcontract.Or(
				mspcontract.EffectiveUntilIsNil(),
				mspcontract.EffectiveUntilGT(from),
			),
		).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Order(ent.Desc(mspcontract.FieldEffectiveFrom), ent.Desc(mspcontract.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, common.NewBusinessError(common.NotFoundCode, "该客户在报告期内没有生效合同", "")
		}
		return nil, err
	}

	scope := &SettlementScope{
		CustomerName: customer.Name,
		Contract: &MSPContract{
			ID:               contract.ID,
			ProviderTenantID: contract.ProviderTenantID,
			CustomerTenantID: contract.CustomerTenantID,
			ContractNo:       contract.ContractNo,
			Name:             contract.Name,
			EffectiveFrom:    contract.EffectiveFrom,
			EffectiveUntil:   contract.EffectiveUntil,
			Status:           contract.Status,
			Penalties:        contract.Penalties,
			SettlementCycle:  contract.SettlementCycle,
			Currency:         contract.Currency,
			TenantID:         contract.TenantID,
			CreatedAt:        contract.CreatedAt,
			UpdatedAt:        contract.UpdatedAt,
		},
		Bindings: make(map[int64]*SettlementDefinitionRef),
	}

	// 绑定覆盖（白名单条件③的"合同覆盖范围"；仅活跃定义，与 L1 同语义）
	bindingRows, err := r.client.MSPContractSLABinding.Query().
		Where(mspcontractslabinding.ContractIDEQ(contract.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range bindingRows {
		def, err := r.client.SLADefinition.Query().
			Where(sladefinition.IDEQ(b.SLADefinitionID), sladefinition.IsActive(true)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue // 定义不存在/未激活：与 L1 跳过语义一致
			}
			return nil, err
		}
		ref := &SettlementDefinitionRef{
			ID:             def.ID,
			Name:           def.Name,
			ResponseTime:   def.ResponseTime,
			ResolutionTime: def.ResolutionTime,
			BusinessHours:  def.BusinessHours,
			IsActive:       def.IsActive,
		}
		if b.TicketTypeID == 0 {
			if scope.TypeAgnostic == nil {
				scope.TypeAgnostic = ref
			}
		} else if _, exists := scope.Bindings[b.TicketTypeID]; !exists {
			scope.Bindings[b.TicketTypeID] = ref
		}
	}

	// ③ 工单窗口 ∩ 合同有效期；豁免类型集（sla_exempt=true 不进分母，单列豁免桶）
	windowFrom, windowTo := from, to
	if contract.EffectiveFrom.After(windowFrom) {
		windowFrom = contract.EffectiveFrom
	}
	if contract.EffectiveUntil != nil && contract.EffectiveUntil.Before(windowTo) {
		windowTo = *contract.EffectiveUntil
	}

	exemptIDs := make(map[int]struct{})
	exemptRows, err := r.client.TicketType.Query().
		Where(tickettype.TenantIDEQ(int64(customerTenantID)), tickettype.SLAExempt(true)).
		Select(tickettype.FieldID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, tt := range exemptRows {
		exemptIDs[tt.ID] = struct{}{}
	}

	tickets, err := r.client.Ticket.Query().
		Where(
			ticket.TenantIDEQ(customerTenantID),
			ticket.MspProviderIDEQ(providerTenantID),
			ticket.CreatedAtGTE(windowFrom),
			ticket.CreatedAtLTE(windowTo),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range tickets {
		ttID := int64(t.TicketTypeID)
		if _, exempt := exemptIDs[int(ttID)]; exempt {
			scope.ExemptCount++
			continue
		}
		// 覆盖判定：显式类型绑定优先；类型无关绑定兜底（白名单条件③）
		if _, ok := scope.Bindings[ttID]; !ok && scope.TypeAgnostic == nil {
			scope.UncoveredCount++
			continue
		}
		st := &SettlementTicket{
			ID:                    t.ID,
			TicketNumber:          t.TicketNumber,
			Title:                 t.Title,
			TicketTypeID:          ttID,
			TicketTypeCode:        t.TicketTypeCodeSnapshot,
			CreatedAt:             t.CreatedAt,
			FirstResponseAt:       t.FirstResponseAt,
			ResolvedAt:            t.ResolvedAt,
			SLAResponseDeadline:   t.SLAResponseDeadline,
			SLAResolutionDeadline: t.SLAResolutionDeadline,
			SLADefinitionID:       t.SLADefinitionID,
			SLAMatchedLayer:       t.SLAMatchedLayer,
		}
		scope.CoveredTickets = append(scope.CoveredTickets, st)
		scope.CoveredIDs = append(scope.CoveredIDs, t.ID)
	}

	// ④ 覆盖工单的违约行（matched_layer/resolution_state 供结算过滤，§13.6）
	if len(scope.CoveredIDs) > 0 {
		vrows, err := r.client.SLAViolation.Query().
			Where(
				slaviolation.TenantIDEQ(customerTenantID),
				slaviolation.TicketIDIn(scope.CoveredIDs...),
				slaviolation.TicketTypeNEQ("incident"),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range vrows {
			scope.Violations = append(scope.Violations, &SettlementViolation{
				ID:              v.ID,
				TicketID:        v.TicketID,
				SLADefinitionID: v.SLADefinitionID,
				ViolationType:   v.ViolationType,
				ResolutionState: v.ResolutionState,
				MatchedLayer:    v.MatchedLayer,
				SettledFlag:     v.SettledFlag,
				ViolationTime:   v.ViolationTime,
			})
		}
	}

	return scope, nil
}

// ComputeSLADeadlines 按定义参数从 baseTime 起算截止时间（结算重算与建单路径同口径）。
func (r *EntRepository) ComputeSLADeadlines(ctx context.Context, tenantID int, responseMinutes, resolutionMinutes int, businessHours map[string]interface{}, baseTime time.Time) (time.Time, time.Time) {
	resp := service.ComputeDeadlineWithBusinessHours(ctx, r.client, tenantID, baseTime, responseMinutes, businessHours)
	res := service.ComputeDeadlineWithBusinessHours(ctx, r.client, tenantID, baseTime, resolutionMinutes, businessHours)
	return resp, res
}

// LockSettlementViolations 出快照锁定（§13.3）：范围内合同域违约置 settled_flag=true。
// 幂等：仅影响未锁定行；客户内部域违约（L2/L3*）不属结算快照范围，保持原状。
func (r *EntRepository) LockSettlementViolations(ctx context.Context, providerTenantID, customerTenantID int, from, to time.Time) (int, error) {
	scope, err := r.GetSettlementScope(ctx, providerTenantID, customerTenantID, from, to)
	if err != nil {
		return 0, err
	}
	if len(scope.CoveredIDs) == 0 {
		return 0, nil
	}
	n, err := r.client.SLAViolation.Update().
		Where(
			slaviolation.TenantIDEQ(customerTenantID),
			slaviolation.TicketIDIn(scope.CoveredIDs...),
			slaviolation.TicketTypeNEQ("incident"),
			slaviolation.SettledFlagEQ(false),
			slaviolation.Or(
				slaviolation.MatchedLayerEQ("L1"),
				slaviolation.MatchedLayerEQ(""),
			),
		).
		SetSettledFlag(true).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// GetComplianceReportData retrieves data for SLA compliance report
func (r *EntRepository) GetComplianceReportData(ctx context.Context, tenantID int, startDate, endDate time.Time) (totalTickets, metSLA, violatedSLA int, avgResponseTime, avgResolutionTime float64, err error) {
	// 1. 统计「窗口内创建」的工单，作为合规率统计的统一口径种群。
	//    总数与达标数必须基于同一批工单，否则会出现负数合规率（详见 R-003）。
	cohort, err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.CreatedAtGTE(startDate), ticket.CreatedAtLTE(endDate)).
		Select(ticket.FieldID).
		All(ctx)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	totalTickets = len(cohort)
	if totalTickets == 0 {
		return 0, 0, 0, 0, 0, nil
	}
	cohortIDs := make([]int, 0, totalTickets)
	for _, t := range cohort {
		cohortIDs = append(cohortIDs, t.ID)
	}

	// 2. 统计同一批（窗口内创建）工单中、在窗口内发生未解决 SLA 违约的数量。
	//    通过 TicketIDIn(cohortIDs) 将违约统计限定在上述种群内，
	//    保证 violatedSLA <= totalTickets，从根本上消除负数合规率。
	var groups []struct {
		TicketID int `json:"ticket_id"` // json tag matches SQL column for ent GroupBy().Scan()
	}
	scanErr := r.client.SLAViolation.Query().
		Where(
			slaviolation.TenantID(tenantID),
			slaviolation.IsResolved(false),
			slaviolation.TicketIDIn(cohortIDs...),
			slaviolation.ViolationTimeGTE(startDate),
			slaviolation.ViolationTimeLTE(endDate),
		).
		GroupBy(slaviolation.FieldTicketID).
		Scan(ctx, &groups)
	if scanErr != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("compliance scan query failed: %w", scanErr)
	}
	violatedSLA = len(groups)
	metSLA = totalTickets - violatedSLA

	// 3. Average response time (minutes) for tickets with first_response_at
	responseRows, respErr := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.CreatedAtGTE(startDate), ticket.CreatedAtLTE(endDate)).
		Select(ticket.FieldCreatedAt, ticket.FieldFirstResponseAt).
		All(ctx)
	if respErr == nil {
		var totalDur time.Duration
		count := 0
		for _, t := range responseRows {
			if !t.CreatedAt.IsZero() && !t.FirstResponseAt.IsZero() {
				totalDur += t.FirstResponseAt.Sub(t.CreatedAt)
				count++
			}
		}
		if count > 0 {
			avgResponseTime = float64(totalDur) / float64(time.Minute) / float64(count)
		}
	}

	// 4. Average resolution time (minutes) for tickets with resolved_at
	resolutionRows, resErr := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID), ticket.CreatedAtGTE(startDate), ticket.CreatedAtLTE(endDate)).
		Select(ticket.FieldCreatedAt, ticket.FieldResolvedAt).
		All(ctx)
	if resErr == nil {
		var totalDur time.Duration
		count := 0
		for _, t := range resolutionRows {
			if !t.CreatedAt.IsZero() && !t.ResolvedAt.IsZero() {
				totalDur += t.ResolvedAt.Sub(t.CreatedAt)
				count++
			}
		}
		if count > 0 {
			avgResolutionTime = float64(totalDur) / float64(time.Minute) / float64(count)
		}
	}

	return totalTickets, metSLA, violatedSLA, avgResponseTime, avgResolutionTime, nil
}

// GetTicketStats retrieves basic ticket statistics for compliance calculation
func (r *EntRepository) GetTicketStats(ctx context.Context, tenantID int) (total int, metSLA int, err error) {
	// 获取总工单数
	total, err = r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID)).
		Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	// 获取有SLA违规的工单数（被视为未达标）
	violatedTickets, err := r.client.SLAViolation.Query().
		Where(slaviolation.TenantID(tenantID)).
		Select(slaviolation.FieldTicketID).
		All(ctx)
	if err != nil {
		return total, 0, err
	}

	// 使用违规工单的独特ID数量
	uniqueViolated := make(map[int]bool)
	for _, v := range violatedTickets {
		uniqueViolated[v.TicketID] = true
	}

	metSLA = total - len(uniqueViolated)
	if metSLA < 0 {
		metSLA = 0
	}

	return total, metSLA, nil
}

// GetTicketSLA retrieves per-ticket SLA timing fields used by check-compliance.
// P1-07 修复：返回创建的首次响应 / 解决时间点，让 Service 能计算 actual_response_minutes。
// 修复：同时返回 SLA 截止时间，让 Service 判断合规时对比 deadline 而非仅检查是否有响应。
func (r *EntRepository) GetTicketSLA(ctx context.Context, ticketID int, tenantID int) (createdAt, firstResponseAt, resolvedAt, slaResponseDeadline, slaResolutionDeadline time.Time, found bool, err error) {
	t, err := r.client.Ticket.Query().
		Where(ticket.IDEQ(ticketID), ticket.TenantID(tenantID)).
		Select(ticket.FieldCreatedAt, ticket.FieldFirstResponseAt, ticket.FieldResolvedAt,
			ticket.FieldSLAResponseDeadline, ticket.FieldSLAResolutionDeadline).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}, false, nil
		}
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}, false, err
	}
	return t.CreatedAt, t.FirstResponseAt, t.ResolvedAt, t.SLAResponseDeadline, t.SLAResolutionDeadline, true, nil
}
