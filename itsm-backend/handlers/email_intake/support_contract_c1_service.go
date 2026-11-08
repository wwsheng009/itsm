package email_intake

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/contractamendment"
	"itsm-backend/ent/contractprojectlink"
	"itsm-backend/ent/contractserviceitem"
	"itsm-backend/ent/contractusageledger"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectcustomerlink"
	"itsm-backend/ent/servicecustomer"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/supportcontract"
)

var (
	ErrContractNotFound            = errors.New("support contract not found")
	ErrContractStatusInvalid       = errors.New("support contract status invalid")
	ErrContractNoDuplicate         = errors.New("support contract number duplicate")
	ErrContractAmountImmutable     = errors.New("support contract amount immutable")
	ErrContractCustomerImmutable   = errors.New("support contract customer immutable")
	ErrContractServiceItemRequired = errors.New("support contract service item required")
	ErrContractServiceItemInvalid  = errors.New("support contract service item invalid")
	ErrContractServiceItemMismatch = errors.New("contract project link service item mismatch")
	ErrContractAmendmentInvalid    = errors.New("contract amendment invalid")
	ErrContractUsageSourceConflict = errors.New("contract usage source conflict")
	ErrContractQuotaExceeded       = errors.New("contract quota exceeded")
	ErrContractAllocationExceeded  = errors.New("contract allocation exceeded")
	ErrContractProjectLinkInvalid  = errors.New("contract project link invalid")
	ErrContractProjectLinkNotFound = errors.New("contract project link not found")
)

var allowedContractStatuses = map[string]bool{
	"draft": true, "pending_sign": true, "pending": true, "active": true,
	"expired": true, "terminated": true, "renewed": true,
}

var allowedBillingUnits = map[string]bool{
	"month": true, "quarter": true, "year": true, "hour": true,
	"ticket": true, "device": true, "project": true,
}

var allowedAmendTypes = map[string]bool{
	"amount": true, "term": true, "scope": true, "id_only": true,
}

var allowedUsageSourceTypes = map[string]bool{
	"timesheet": true, "ticket": true, "device_snapshot": true,
	"project_close": true, "manual_adjustment": true,
}

var allowedContractProjectLinkTypes = map[string]bool{
	"basis": true, "coverage": true, "billing": true, "change": true,
}

type contractServiceItemRequest struct {
	CatalogItemID     *int     `json:"catalogItemId"`
	ItemCode          string   `json:"itemCode"`
	ItemName          string   `json:"itemName" binding:"required"`
	Spec              string   `json:"spec"`
	BillingUnit       string   `json:"billingUnit"`
	Quantity          *float64 `json:"quantity"`
	PeriodCount       *float64 `json:"periodCount"`
	UnitPrice         *float64 `json:"unitPrice"`
	Amount            *float64 `json:"amount"`
	QuotaTotal        *float64 `json:"quotaTotal"`
	Note              string   `json:"note"`
	SLADefinitionID   *int     `json:"slaDefinitionId"`
	ResponseTargetMin *int     `json:"responseTargetMin"`
	ResolveTargetMin  *int     `json:"resolveTargetMin"`
	SortOrder         int      `json:"sortOrder"`
	Status            string   `json:"status"`
}

type contractAmendmentRequest struct {
	AmendmentNo   string                 `json:"amendmentNo" binding:"required"`
	AmendType     string                 `json:"amendType" binding:"required"`
	EffectiveFrom *time.Time             `json:"effectiveFrom"`
	AmountDelta   *float64               `json:"amountDelta"`
	TermDeltaDays *int                   `json:"termDeltaDays"`
	Diff          map[string]interface{} `json:"diff"`
	Reason        string                 `json:"reason"`
	SignedAt      *time.Time             `json:"signedAt"`
}

type contractUsageRequest struct {
	ServiceItemID int        `json:"serviceItemId" binding:"required"`
	SourceType    string     `json:"sourceType" binding:"required"`
	SourceRef     string     `json:"sourceRef" binding:"required"`
	QuantityDelta *float64   `json:"quantityDelta"`
	AmountDelta   *float64   `json:"amountDelta"`
	OccurredAt    *time.Time `json:"occurredAt"`
	Note          string     `json:"note"`
}

type contractProjectLinkRequest struct {
	ProjectID       int        `json:"projectId" binding:"required"`
	RelationType    string     `json:"relationType"`
	ServiceItemID   *int       `json:"serviceItemId"`
	AllocatedAmount *float64   `json:"allocatedAmount"`
	AllocatedHours  *float64   `json:"allocatedHours"`
	AllocationPct   *int16     `json:"allocationPct"`
	IsPrimary       bool       `json:"isPrimary"`
	EffectiveFrom   *time.Time `json:"effectiveFrom"`
	EffectiveTo     *time.Time `json:"effectiveTo"`
	Note            string     `json:"note"`
}

type contractSignRequest struct {
	SignedAt *time.Time `json:"signedAt"`
}

type contractTerminateRequest struct {
	Reason       string     `json:"reason" binding:"required"`
	TerminatedAt *time.Time `json:"terminatedAt"`
}

type contractRenewRequest struct {
	ContractNumber  string     `json:"contractNumber" binding:"required"`
	EffectiveFrom   *time.Time `json:"effectiveFrom"`
	EffectiveUntil  *time.Time `json:"effectiveUntil"`
	Amount          *float64   `json:"amount"`
	Currency        string     `json:"currency"`
	CarryProjectIDs []int      `json:"carryProjectIds"`
}

func contractFloat(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func contractFloatPtr(value float64) *float64 { return &value }

func contractTimePtr(value time.Time) *time.Time { return &value }

func contractEffectiveDates(req *supportContractRequest) (*time.Time, *time.Time) {
	from := req.EffectiveFrom
	if from == nil {
		from = req.StartAt
	}
	until := req.EffectiveUntil
	if until == nil && req.EndAt != nil {
		nextDay := req.EndAt.AddDate(0, 0, 1)
		until = &nextDay
	}
	return from, until
}

func contractDisplayStatus(status string, until *time.Time, tier string, now time.Time) string {
	if status != "active" || until == nil {
		return status
	}
	if !until.After(now) {
		return "expired"
	}
	leadDays := 30
	switch strings.ToUpper(tier) {
	case "S":
		leadDays = 90
	case "A":
		leadDays = 60
	}
	if until.Before(now.Add(time.Duration(leadDays) * 24 * time.Hour)) {
		return "expiring"
	}
	return status
}

func (s *Service) supportContractByID(ctx context.Context, tenantID, contractID int) (*ent.SupportContract, error) {
	contract, err := s.client.SupportContract.Query().
		Where(
			supportcontract.IDEQ(contractID),
			supportcontract.TenantIDEQ(tenantID),
			supportcontract.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrContractNotFound
	}
	return contract, err
}

func (s *Service) effectiveContractAmount(ctx context.Context, tenantID, contractID int, base *float64) (float64, error) {
	amendments, err := s.client.ContractAmendment.Query().
		Where(
			contractamendment.TenantIDEQ(tenantID),
			contractamendment.ContractIDEQ(contractID),
			contractamendment.StatusEQ("effective"),
		).
		All(ctx)
	if err != nil {
		return 0, err
	}
	total := 0.0
	if base != nil {
		total = *base
	}
	for _, amendment := range amendments {
		total += amendment.AmountDelta
	}
	return math.Round(total*100) / 100, nil
}

type contractAllocationSnapshot struct {
	Amount      float64
	HoursByItem map[int]float64
}

func (s *Service) contractAllocation(ctx context.Context, tenantID, contractID int) (contractAllocationSnapshot, error) {
	links, err := s.client.ContractProjectLink.Query().
		Where(
			contractprojectlink.TenantIDEQ(tenantID),
			contractprojectlink.ContractIDEQ(contractID),
			contractprojectlink.RelationTypeEQ("billing"),
			contractprojectlink.StatusEQ("active"),
			contractprojectlink.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return contractAllocationSnapshot{}, err
	}
	result := contractAllocationSnapshot{HoursByItem: map[int]float64{}}
	for _, link := range links {
		if link.AllocatedAmount != nil {
			result.Amount += *link.AllocatedAmount
		}
		if link.AllocatedHours != nil && link.ServiceItemID != nil {
			result.HoursByItem[*link.ServiceItemID] += *link.AllocatedHours
		}
	}
	result.Amount = math.Round(result.Amount*100) / 100
	return result, nil
}

func (s *Service) contractUsedAmount(ctx context.Context, tenantID, contractID int) (float64, error) {
	entries, err := s.client.ContractUsageLedger.Query().
		Where(
			contractusageledger.TenantIDEQ(tenantID),
			contractusageledger.ContractIDEQ(contractID),
		).
		All(ctx)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, entry := range entries {
		total += entry.AmountDelta
	}
	return math.Round(total*100) / 100, nil
}

func (s *Service) contractBalance(ctx context.Context, tenantID, contractID int) (*contractBalanceResponse, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	items, err := s.client.ContractServiceItem.Query().
		Where(
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.DeletedAtIsNil(),
		).
		Order(contractserviceitem.BySortOrder(), contractserviceitem.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	allocation, err := s.contractAllocation(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	used, err := s.contractUsedAmount(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	effective, err := s.effectiveContractAmount(ctx, tenantID, contractID, contract.Amount)
	if err != nil {
		return nil, err
	}
	response := &contractBalanceResponse{
		ContractID: contractID, Amount: contract.Amount, EffectiveAmount: &effective,
		AllocatedAmount: allocation.Amount, UsedAmount: used,
		RemainingAmount: contractFloatPtr(effective - used),
		AvailableAmount: contractFloatPtr(effective - allocation.Amount),
		ServiceItems:    make([]contractServiceItemResponse, 0, len(items)),
	}
	for _, item := range items {
		response.ServiceItems = append(response.ServiceItems, mapContractServiceItem(item, allocation.HoursByItem[item.ID]))
	}
	return response, nil
}

func (s *Service) enrichSupportContractResponse(ctx context.Context, tenantID int, contract *ent.SupportContract, response supportContractResponse) (supportContractResponse, error) {
	balance, err := s.contractBalance(ctx, tenantID, contract.ID)
	if err != nil {
		return response, err
	}
	response.EffectiveAmount = balance.EffectiveAmount
	response.AllocatedAmount = balance.AllocatedAmount
	response.UsedAmount = balance.UsedAmount
	response.RemainingAmount = balance.RemainingAmount
	response.AvailableAmount = balance.AvailableAmount
	tier := "C"
	if customer, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(contract.CustomerID), servicecustomer.TenantIDEQ(tenantID)).
		Only(ctx); err == nil {
		tier = customer.Tier
	}
	until := contract.EffectiveUntil
	if until == nil {
		until = contract.EndAt
	}
	now := time.Now()
	response.DisplayStatus = contractDisplayStatus(contract.Status, until, tier, now)
	if until != nil {
		days := int(until.Sub(now).Hours() / 24)
		response.RemainingDays = &days
	}
	return response, nil
}

func validateSupportContractRequest(req *supportContractRequest) error {
	if req == nil {
		return ErrContractStatusInvalid
	}
	status := defaultStringOr(req.Status, "draft")
	if !allowedContractStatuses[status] {
		return ErrContractStatusInvalid
	}
	if req.Amount != nil && *req.Amount < 0 {
		return ErrContractServiceItemInvalid
	}
	if req.EffectiveFrom != nil && req.EffectiveUntil != nil && req.EffectiveUntil.Before(*req.EffectiveFrom) {
		return ErrContractStatusInvalid
	}
	return nil
}

func (s *Service) createSupportContractEntity(ctx context.Context, tenantID, actorID int, req *supportContractRequest) (*ent.SupportContract, error) {
	if err := validateSupportContractRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateSupportContractReferences(ctx, tenantID, req); err != nil {
		return nil, err
	}
	from, until := contractEffectiveDates(req)
	status := defaultStringOr(req.Status, "draft")
	create := s.client.SupportContract.Create().
		SetTenantID(tenantID).
		SetCustomerID(req.CustomerID).
		SetNillableBranchID(req.BranchID).
		SetContractNumber(strings.TrimSpace(req.ContractNumber)).
		SetNormalizedContractNumber(NormalizeContractNumber(req.ContractNumber)).
		SetContractName(req.ContractName).
		SetContractType(defaultStringOr(req.ContractType, "support")).
		SetStatus(status).
		SetNillableStartAt(req.StartAt).
		SetNillableEndAt(req.EndAt).
		SetNillableEffectiveFrom(from).
		SetNillableEffectiveUntil(until).
		SetNillableSignedAt(req.SignedAt).
		SetNillableAmount(req.Amount).
		SetCurrency(defaultStringOr(req.Currency, "CNY")).
		SetNillableParentContractID(req.ParentContractID).
		SetNillableRenewalOfContractID(req.RenewalOfContractID).
		SetNillableSLADefinitionID(req.SLADefinitionID).
		SetAutoRenew(req.AutoRenew).
		SetNillableTerminatedAt(req.TerminatedAt).
		SetTerminationReason(req.TerminationReason).
		SetNillableCreatedBy(optionalActor(actorID)).
		SetNillableUpdatedBy(optionalActor(actorID))
	entity, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractNoDuplicate
		}
		return nil, err
	}
	return entity, nil
}

func (s *Service) updateSupportContractEntity(ctx context.Context, tenantID, actorID, id int, req *supportContractRequest) (*ent.SupportContract, error) {
	if err := validateSupportContractRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateSupportContractReferences(ctx, tenantID, req); err != nil {
		return nil, err
	}
	current, err := s.supportContractByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.CustomerID != current.CustomerID {
		linked, err := s.client.ContractProjectLink.Query().
			Where(
				contractprojectlink.TenantIDEQ(tenantID),
				contractprojectlink.ContractIDEQ(id),
				contractprojectlink.StatusEQ("active"),
				contractprojectlink.DeletedAtIsNil(),
			).
			Exist(ctx)
		if err != nil {
			return nil, err
		}
		if linked {
			return nil, ErrContractCustomerImmutable
		}
	}
	if current.Status != "draft" && current.Status != "pending_sign" && current.Status != "pending" {
		if req.Amount != nil && (current.Amount == nil || math.Abs(*req.Amount-*current.Amount) > 0.001) {
			return nil, ErrContractAmountImmutable
		}
		if req.EffectiveFrom != nil && (current.EffectiveFrom == nil || !req.EffectiveFrom.Equal(*current.EffectiveFrom)) {
			return nil, ErrContractAmountImmutable
		}
		if req.EffectiveUntil != nil && (current.EffectiveUntil == nil || !req.EffectiveUntil.Equal(*current.EffectiveUntil)) {
			return nil, ErrContractAmountImmutable
		}
	}
	from, until := contractEffectiveDates(req)
	update := s.client.SupportContract.UpdateOneID(id).
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.DeletedAtIsNil()).
		SetCustomerID(req.CustomerID).
		SetNillableBranchID(req.BranchID).
		SetContractNumber(strings.TrimSpace(req.ContractNumber)).
		SetNormalizedContractNumber(NormalizeContractNumber(req.ContractNumber)).
		SetContractName(req.ContractName).
		SetNillableStartAt(req.StartAt).
		SetNillableEndAt(req.EndAt).
		SetAutoRenew(req.AutoRenew).
		SetTerminationReason(req.TerminationReason).
		SetNillableUpdatedBy(optionalActor(actorID)).
		AddVersion(1)
	if req.ContractType != "" {
		update.SetContractType(req.ContractType)
	}
	if req.Status != "" {
		update.SetStatus(req.Status)
	}
	if from != nil {
		update.SetEffectiveFrom(*from)
	}
	if until != nil {
		update.SetEffectiveUntil(*until)
	}
	if req.SignedAt != nil {
		update.SetSignedAt(*req.SignedAt)
	}
	if req.Amount != nil {
		update.SetAmount(*req.Amount)
	}
	if req.Currency != "" {
		update.SetCurrency(req.Currency)
	}
	if req.ParentContractID != nil {
		update.SetParentContractID(*req.ParentContractID)
	}
	if req.RenewalOfContractID != nil {
		update.SetRenewalOfContractID(*req.RenewalOfContractID)
	}
	if req.SLADefinitionID != nil {
		update.SetSLADefinitionID(*req.SLADefinitionID)
	}
	if req.TerminatedAt != nil {
		update.SetTerminatedAt(*req.TerminatedAt)
	}
	if req.Version != nil {
		update.Where(supportcontract.VersionEQ(*req.Version))
	}
	updated, err := update.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractNoDuplicate
		}
		if ent.IsNotFound(err) && req.Version != nil {
			return nil, ErrCustomerVersionConflict
		}
		return nil, err
	}
	return updated, nil
}

func (s *Service) validateSupportContractReferences(ctx context.Context, tenantID int, req *supportContractRequest) error {
	if req.SLADefinitionID != nil && *req.SLADefinitionID > 0 {
		exists, err := s.client.SLADefinition.Query().
			Where(sladefinition.IDEQ(*req.SLADefinitionID), sladefinition.TenantIDEQ(tenantID)).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return ErrCustomerInvalidReference
		}
	}
	for _, contractID := range []*int{req.ParentContractID, req.RenewalOfContractID} {
		if contractID == nil || *contractID < 1 {
			continue
		}
		exists, err := s.client.SupportContract.Query().
			Where(supportcontract.IDEQ(*contractID), supportcontract.TenantIDEQ(tenantID), supportcontract.DeletedAtIsNil()).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return ErrContractNotFound
		}
	}
	return nil
}

func validateContractServiceItemRequest(req *contractServiceItemRequest) (float64, error) {
	if req == nil || strings.TrimSpace(req.ItemName) == "" {
		return 0, ErrContractServiceItemInvalid
	}
	req.ItemName = strings.TrimSpace(req.ItemName)
	req.ItemCode = strings.TrimSpace(req.ItemCode)
	req.BillingUnit = defaultStringOr(req.BillingUnit, "month")
	if !allowedBillingUnits[req.BillingUnit] {
		return 0, ErrContractServiceItemInvalid
	}
	quantity := contractFloat(req.Quantity, 1)
	period := contractFloat(req.PeriodCount, 1)
	unitPrice := contractFloat(req.UnitPrice, 0)
	if quantity < 0 || period <= 0 || unitPrice < 0 {
		return 0, ErrContractServiceItemInvalid
	}
	if req.QuotaTotal != nil && *req.QuotaTotal < 0 {
		return 0, ErrContractServiceItemInvalid
	}
	if req.Status != "" && req.Status != "active" && req.Status != "inactive" {
		return 0, ErrContractServiceItemInvalid
	}
	computed := math.Round(quantity*unitPrice*period*100) / 100
	amount := computed
	if req.Amount != nil {
		amount = *req.Amount
		if amount < 0 {
			return 0, ErrContractServiceItemInvalid
		}
		if math.Abs(amount-computed) > 0.01 && strings.TrimSpace(req.Note) == "" {
			return 0, ErrContractServiceItemInvalid
		}
	}
	req.Quantity = &quantity
	req.PeriodCount = &period
	req.UnitPrice = &unitPrice
	req.Amount = &amount
	return amount, nil
}

func (s *Service) ListContractServiceItems(ctx context.Context, tenantID, contractID int) ([]contractServiceItemResponse, error) {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	items, err := s.client.ContractServiceItem.Query().
		Where(
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.DeletedAtIsNil(),
		).
		Order(contractserviceitem.BySortOrder(), contractserviceitem.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	allocation, err := s.contractAllocation(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	result := make([]contractServiceItemResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapContractServiceItem(item, allocation.HoursByItem[item.ID]))
	}
	return result, nil
}

func (s *Service) CreateContractServiceItem(ctx context.Context, tenantID, actorID, contractID int, req *contractServiceItemRequest) (*ent.ContractServiceItem, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if contract.Status == "terminated" || contract.Status == "renewed" {
		return nil, ErrContractStatusInvalid
	}
	amount, err := validateContractServiceItemRequest(req)
	if err != nil {
		return nil, err
	}
	create := s.client.ContractServiceItem.Create().
		SetTenantID(tenantID).
		SetContractID(contractID).
		SetNillableCatalogItemID(req.CatalogItemID).
		SetItemCode(req.ItemCode).
		SetItemName(req.ItemName).
		SetSpec(req.Spec).
		SetBillingUnit(req.BillingUnit).
		SetQuantity(*req.Quantity).
		SetPeriodCount(*req.PeriodCount).
		SetUnitPrice(*req.UnitPrice).
		SetAmount(amount).
		SetNillableQuotaTotal(req.QuotaTotal).
		SetQuotaUsed(0).
		SetNote(req.Note).
		SetNillableSLADefinitionID(req.SLADefinitionID).
		SetNillableResponseTargetMin(req.ResponseTargetMin).
		SetNillableResolveTargetMin(req.ResolveTargetMin).
		SetSortOrder(req.SortOrder).
		SetStatus(defaultStringOr(req.Status, "active")).
		SetNillableCreatedBy(optionalActor(actorID)).
		SetNillableUpdatedBy(optionalActor(actorID))
	entity, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractServiceItemInvalid
		}
		return nil, err
	}
	return entity, nil
}

func (s *Service) UpdateContractServiceItem(ctx context.Context, tenantID, actorID, contractID, itemID int, req *contractServiceItemRequest) (*ent.ContractServiceItem, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if contract.Status != "draft" && contract.Status != "pending_sign" && contract.Status != "pending" {
		return nil, ErrContractStatusInvalid
	}
	amount, err := validateContractServiceItemRequest(req)
	if err != nil {
		return nil, err
	}
	affected, err := s.client.ContractServiceItem.Update().
		Where(
			contractserviceitem.IDEQ(itemID),
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.DeletedAtIsNil(),
		).
		SetNillableCatalogItemID(req.CatalogItemID).
		SetItemCode(req.ItemCode).
		SetItemName(req.ItemName).
		SetSpec(req.Spec).
		SetBillingUnit(req.BillingUnit).
		SetQuantity(*req.Quantity).
		SetPeriodCount(*req.PeriodCount).
		SetUnitPrice(*req.UnitPrice).
		SetAmount(amount).
		SetNillableQuotaTotal(req.QuotaTotal).
		SetNote(req.Note).
		SetNillableSLADefinitionID(req.SLADefinitionID).
		SetNillableResponseTargetMin(req.ResponseTargetMin).
		SetNillableResolveTargetMin(req.ResolveTargetMin).
		SetSortOrder(req.SortOrder).
		SetStatus(defaultStringOr(req.Status, "active")).
		SetNillableUpdatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractServiceItemInvalid
		}
		return nil, err
	}
	if affected == 0 {
		return nil, ErrContractServiceItemInvalid
	}
	return s.client.ContractServiceItem.Get(ctx, itemID)
}

func (s *Service) DeleteContractServiceItem(ctx context.Context, tenantID, actorID, contractID, itemID int) error {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return err
	}
	if contract.Status != "draft" && contract.Status != "pending_sign" && contract.Status != "pending" {
		return ErrContractStatusInvalid
	}
	now := time.Now()
	affected, err := s.client.ContractServiceItem.Update().
		Where(
			contractserviceitem.IDEQ(itemID),
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.DeletedAtIsNil(),
		).
		SetStatus("inactive").
		SetDeletedAt(now).
		SetNillableUpdatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrContractServiceItemInvalid
	}
	return nil
}

func (s *Service) ListContractAmendments(ctx context.Context, tenantID, contractID int) ([]contractAmendmentResponse, error) {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	items, err := s.client.ContractAmendment.Query().
		Where(contractamendment.TenantIDEQ(tenantID), contractamendment.ContractIDEQ(contractID)).
		Order(contractamendment.ByEffectiveFrom(), contractamendment.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]contractAmendmentResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapContractAmendment(item))
	}
	return result, nil
}

func (s *Service) CreateContractAmendment(ctx context.Context, tenantID, actorID, contractID int, req *contractAmendmentRequest) (*ent.ContractAmendment, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if contract.Status == "terminated" || contract.Status == "renewed" {
		return nil, ErrContractStatusInvalid
	}
	if req == nil || !allowedAmendTypes[req.AmendType] || strings.TrimSpace(req.AmendmentNo) == "" {
		return nil, ErrContractAmendmentInvalid
	}
	amountDelta := contractFloat(req.AmountDelta, 0)
	if req.AmendType == "amount" && amountDelta == 0 {
		return nil, ErrContractAmendmentInvalid
	}
	if req.AmendType == "term" && (req.TermDeltaDays == nil || *req.TermDeltaDays == 0) {
		return nil, ErrContractAmendmentInvalid
	}
	effectiveFrom := time.Now()
	if req.EffectiveFrom != nil {
		effectiveFrom = *req.EffectiveFrom
	}
	diff := req.Diff
	if diff == nil {
		diff = map[string]interface{}{}
	}
	entity, err := s.client.ContractAmendment.Create().
		SetTenantID(tenantID).
		SetContractID(contractID).
		SetAmendmentNo(strings.TrimSpace(req.AmendmentNo)).
		SetAmendType(req.AmendType).
		SetEffectiveFrom(effectiveFrom).
		SetAmountDelta(amountDelta).
		SetNillableTermDeltaDays(req.TermDeltaDays).
		SetDiff(diff).
		SetReason(req.Reason).
		SetStatus("draft").
		SetNillableSignedAt(req.SignedAt).
		SetNillableCreatedBy(optionalActor(actorID)).
		SetNillableUpdatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractAmendmentInvalid
		}
		return nil, err
	}
	return entity, nil
}

func (s *Service) EffectContractAmendment(ctx context.Context, tenantID, actorID, contractID, amendmentID int) (*ent.ContractAmendment, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	amendment, err := s.client.ContractAmendment.Query().
		Where(
			contractamendment.IDEQ(amendmentID),
			contractamendment.TenantIDEQ(tenantID),
			contractamendment.ContractIDEQ(contractID),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrContractAmendmentInvalid
	}
	if err != nil {
		return nil, err
	}
	if amendment.Status == "effective" {
		return amendment, nil
	}
	if amendment.Status == "cancelled" {
		return nil, ErrContractAmendmentInvalid
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()
	updatedAmendment, err := tx.ContractAmendment.UpdateOneID(amendmentID).
		SetStatus("effective").
		SetSignedAt(now).
		SetNillableUpdatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	contractUpdate := tx.SupportContract.UpdateOneID(contractID).
		Where(supportcontract.TenantIDEQ(tenantID)).
		AddAmendmentCount(1).
		AddVersion(1).
		SetNillableUpdatedBy(optionalActor(actorID))
	if amendment.TermDeltaDays != nil {
		base := now
		if contract.EffectiveUntil != nil {
			base = *contract.EffectiveUntil
		}
		newUntil := base.AddDate(0, 0, *amendment.TermDeltaDays)
		contractUpdate.SetEffectiveUntil(newUntil).SetEndAt(newUntil)
	}
	if _, err := contractUpdate.Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updatedAmendment, nil
}

func (s *Service) ListContractUsage(ctx context.Context, tenantID, contractID int) ([]contractUsageEntryResponse, error) {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	entries, err := s.client.ContractUsageLedger.Query().
		Where(
			contractusageledger.TenantIDEQ(tenantID),
			contractusageledger.ContractIDEQ(contractID),
		).
		Order(contractusageledger.ByOccurredAt(), contractusageledger.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]contractUsageEntryResponse, 0, len(entries))
	for _, entry := range entries {
		result = append(result, mapContractUsageEntry(entry))
	}
	return result, nil
}

func (s *Service) RecordContractUsage(ctx context.Context, tenantID, actorID, contractID int, req *contractUsageRequest) (*ent.ContractUsageLedger, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if req == nil || !allowedUsageSourceTypes[req.SourceType] || strings.TrimSpace(req.SourceRef) == "" {
		return nil, ErrContractAmendmentInvalid
	}
	quantityDelta := contractFloat(req.QuantityDelta, 0)
	amountDelta := contractFloat(req.AmountDelta, 0)
	if quantityDelta == 0 && amountDelta == 0 {
		return nil, ErrContractAmendmentInvalid
	}
	item, err := s.client.ContractServiceItem.Query().
		Where(
			contractserviceitem.IDEQ(req.ServiceItemID),
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrContractServiceItemMismatch
	}
	if err != nil {
		return nil, err
	}
	existing, err := s.client.ContractUsageLedger.Query().
		Where(
			contractusageledger.TenantIDEQ(tenantID),
			contractusageledger.ServiceItemIDEQ(req.ServiceItemID),
			contractusageledger.SourceTypeEQ(req.SourceType),
			contractusageledger.SourceRefEQ(strings.TrimSpace(req.SourceRef)),
		).
		Only(ctx)
	if err == nil {
		if math.Abs(existing.QuantityDelta-quantityDelta) > 0.0001 || math.Abs(existing.AmountDelta-amountDelta) > 0.001 {
			return nil, ErrContractUsageSourceConflict
		}
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	occurredAt := time.Now()
	if req.OccurredAt != nil {
		occurredAt = *req.OccurredAt
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	entry, err := tx.ContractUsageLedger.Create().
		SetTenantID(tenantID).
		SetContractID(contract.ID).
		SetServiceItemID(item.ID).
		SetSourceType(req.SourceType).
		SetSourceRef(strings.TrimSpace(req.SourceRef)).
		SetQuantityDelta(quantityDelta).
		SetAmountDelta(amountDelta).
		SetOccurredAt(occurredAt).
		SetNote(req.Note).
		SetNillableCreatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractUsageSourceConflict
		}
		return nil, err
	}
	if quantityDelta != 0 {
		current, err := tx.ContractServiceItem.Query().
			Where(
				contractserviceitem.IDEQ(item.ID),
				contractserviceitem.TenantIDEQ(tenantID),
			).
			Only(ctx)
		if err != nil {
			return nil, err
		}
		newUsed := current.QuotaUsed + quantityDelta
		if newUsed < 0 || (current.QuotaTotal != nil && newUsed > *current.QuotaTotal+0.0001) {
			return nil, ErrContractQuotaExceeded
		}
		if _, err := tx.ContractServiceItem.UpdateOneID(item.ID).
			SetQuotaUsed(newUsed).
			SetNillableUpdatedBy(optionalActor(actorID)).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *Service) contractProjectLinkRows(ctx context.Context, tenantID int, predicate func(*ent.ContractProjectLinkQuery)) ([]*contractProjectLinkRow, error) {
	query := s.client.ContractProjectLink.Query().
		Where(
			contractprojectlink.TenantIDEQ(tenantID),
			contractprojectlink.StatusEQ("active"),
			contractprojectlink.DeletedAtIsNil(),
		)
	if predicate != nil {
		predicate(query)
	}
	links, err := query.Order(contractprojectlink.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	projectIDs := []int{}
	itemIDs := []int{}
	contractIDs := []int{}
	for _, link := range links {
		projectIDs = append(projectIDs, link.ProjectID)
		contractIDs = append(contractIDs, link.ContractID)
		if link.ServiceItemID != nil {
			itemIDs = append(itemIDs, *link.ServiceItemID)
		}
	}
	projects := map[int]*ent.Project{}
	if len(projectIDs) > 0 {
		rows, err := s.client.Project.Query().Where(project.TenantIDEQ(tenantID), project.IDIn(projectIDs...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			projects[row.ID] = row
		}
	}
	items := map[int]*ent.ContractServiceItem{}
	if len(itemIDs) > 0 {
		rows, err := s.client.ContractServiceItem.Query().
			Where(contractserviceitem.TenantIDEQ(tenantID), contractserviceitem.IDIn(itemIDs...)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			items[row.ID] = row
		}
	}
	contracts := map[int]*ent.SupportContract{}
	if len(contractIDs) > 0 {
		rows, err := s.client.SupportContract.Query().
			Where(supportcontract.TenantIDEQ(tenantID), supportcontract.IDIn(contractIDs...)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			contracts[row.ID] = row
		}
	}
	result := make([]*contractProjectLinkRow, 0, len(links))
	for _, link := range links {
		row := &contractProjectLinkRow{Link: link, Project: projects[link.ProjectID], Contract: contracts[link.ContractID]}
		if link.ServiceItemID != nil {
			row.ServiceItem = items[*link.ServiceItemID]
		}
		result = append(result, row)
	}
	return result, nil
}

func (s *Service) ListContractProjects(ctx context.Context, tenantID, contractID int) ([]contractProjectLinkResponse, error) {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	rows, err := s.contractProjectLinkRows(ctx, tenantID, func(query *ent.ContractProjectLinkQuery) {
		query.Where(contractprojectlink.ContractIDEQ(contractID))
	})
	if err != nil {
		return nil, err
	}
	result := make([]contractProjectLinkResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapContractProjectLink(row))
	}
	return result, nil
}

func (s *Service) ListProjectContracts(ctx context.Context, tenantID, projectID int) ([]contractProjectLinkResponse, error) {
	rows, err := s.contractProjectLinkRows(ctx, tenantID, func(query *ent.ContractProjectLinkQuery) {
		query.Where(contractprojectlink.ProjectIDEQ(projectID))
	})
	if err != nil {
		return nil, err
	}
	result := make([]contractProjectLinkResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapContractProjectLink(row))
	}
	return result, nil
}

func (s *Service) CreateContractProjectLink(ctx context.Context, tenantID, actorID, contractID int, req *contractProjectLinkRequest) (*ent.ContractProjectLink, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrContractProjectLinkInvalid
	}
	relationType := defaultStringOr(req.RelationType, "basis")
	if !allowedContractProjectLinkTypes[relationType] {
		return nil, ErrContractProjectLinkInvalid
	}
	if relationType == "basis" || relationType == "billing" {
		if contract.Status != "active" || (contract.EffectiveUntil != nil && !contract.EffectiveUntil.After(time.Now())) {
			return nil, ErrContractStatusInvalid
		}
	}
	if req.IsPrimary && relationType != "basis" {
		return nil, ErrContractProjectLinkInvalid
	}
	if relationType != "billing" && (req.ServiceItemID != nil || req.AllocatedAmount != nil || req.AllocatedHours != nil || req.AllocationPct != nil) {
		return nil, ErrContractProjectLinkInvalid
	}
	if relationType == "billing" && req.ServiceItemID == nil {
		return nil, ErrContractServiceItemMismatch
	}
	if req.AllocatedAmount != nil && *req.AllocatedAmount < 0 {
		return nil, ErrContractProjectLinkInvalid
	}
	if req.AllocatedHours != nil && *req.AllocatedHours < 0 {
		return nil, ErrContractProjectLinkInvalid
	}
	if req.AllocationPct != nil && (*req.AllocationPct < 0 || *req.AllocationPct > 100) {
		return nil, ErrContractProjectLinkInvalid
	}
	projectEntity, err := s.client.Project.Query().
		Where(
			project.IDEQ(req.ProjectID),
			project.TenantIDEQ(tenantID),
			project.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrContractProjectLinkInvalid
	}
	if err != nil {
		return nil, err
	}
	if projectEntity.Type == "internal" {
		return nil, ErrContractProjectLinkInvalid
	}
	conflictingPrimary, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.ProjectIDEQ(req.ProjectID),
			projectcustomerlink.IsPrimaryEQ(true),
			projectcustomerlink.CustomerIDNEQ(contract.CustomerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if conflictingPrimary {
		return nil, ErrContractProjectLinkInvalid
	}
	customerLinkExists, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.ProjectIDEQ(req.ProjectID),
			projectcustomerlink.CustomerIDEQ(contract.CustomerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !customerLinkExists {
		return nil, ErrContractProjectLinkInvalid
	}
	var serviceItem *ent.ContractServiceItem
	if req.ServiceItemID != nil {
		serviceItem, err = s.client.ContractServiceItem.Query().
			Where(
				contractserviceitem.IDEQ(*req.ServiceItemID),
				contractserviceitem.TenantIDEQ(tenantID),
				contractserviceitem.ContractIDEQ(contractID),
				contractserviceitem.StatusEQ("active"),
				contractserviceitem.DeletedAtIsNil(),
			).
			Only(ctx)
		if ent.IsNotFound(err) {
			return nil, ErrContractServiceItemMismatch
		}
		if err != nil {
			return nil, err
		}
		if req.AllocatedHours != nil && (serviceItem.BillingUnit == "month" || serviceItem.BillingUnit == "quarter" || serviceItem.BillingUnit == "year") {
			return nil, ErrContractServiceItemMismatch
		}
	}
	allocation, err := s.contractAllocation(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	effectiveAmount, err := s.effectiveContractAmount(ctx, tenantID, contractID, contract.Amount)
	if err != nil {
		return nil, err
	}
	newAmount := contractFloat(req.AllocatedAmount, 0)
	if newAmount > 0 && allocation.Amount+newAmount > effectiveAmount+0.001 {
		return nil, ErrContractAllocationExceeded
	}
	if req.AllocatedHours != nil && serviceItem != nil && serviceItem.QuotaTotal != nil {
		reserved := allocation.HoursByItem[serviceItem.ID]
		if serviceItem.QuotaUsed+reserved+*req.AllocatedHours > *serviceItem.QuotaTotal+0.0001 {
			return nil, ErrContractAllocationExceeded
		}
	}
	if req.IsPrimary {
		if _, err := s.client.ContractProjectLink.Update().
			Where(
				contractprojectlink.TenantIDEQ(tenantID),
				contractprojectlink.ProjectIDEQ(req.ProjectID),
				contractprojectlink.RelationTypeEQ("basis"),
				contractprojectlink.IsPrimaryEQ(true),
				contractprojectlink.StatusEQ("active"),
				contractprojectlink.DeletedAtIsNil(),
			).
			SetIsPrimary(false).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	create := s.client.ContractProjectLink.Create().
		SetTenantID(tenantID).
		SetContractID(contractID).
		SetProjectID(req.ProjectID).
		SetRelationType(relationType).
		SetNillableServiceItemID(req.ServiceItemID).
		SetNillableAllocatedAmount(req.AllocatedAmount).
		SetNillableAllocatedHours(req.AllocatedHours).
		SetNillableAllocationPct(req.AllocationPct).
		SetIsPrimary(req.IsPrimary).
		SetNote(req.Note).
		SetNillableEffectiveFrom(req.EffectiveFrom).
		SetNillableEffectiveTo(req.EffectiveTo).
		SetStatus("active").
		SetNillableCreatedBy(optionalActor(actorID))
	entity, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractProjectLinkInvalid
		}
		return nil, err
	}
	return entity, nil
}

func (s *Service) DeleteContractProjectLink(ctx context.Context, tenantID, actorID, contractID, linkID int) error {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return err
	}
	now := time.Now()
	affected, err := s.client.ContractProjectLink.Update().
		Where(
			contractprojectlink.IDEQ(linkID),
			contractprojectlink.TenantIDEQ(tenantID),
			contractprojectlink.ContractIDEQ(contractID),
			contractprojectlink.DeletedAtIsNil(),
		).
		SetStatus("cancelled").
		SetEffectiveTo(now).
		SetDeletedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrContractProjectLinkNotFound
	}
	return nil
}

func (s *Service) SignSupportContract(ctx context.Context, tenantID, actorID, contractID int, req *contractSignRequest) (*ent.SupportContract, error) {
	contract, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if contract.Status == "active" {
		return contract, nil
	}
	if contract.Status != "draft" && contract.Status != "pending_sign" && contract.Status != "pending" {
		return nil, ErrContractStatusInvalid
	}
	itemCount, err := s.client.ContractServiceItem.Query().
		Where(
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(contractID),
			contractserviceitem.StatusEQ("active"),
			contractserviceitem.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	if itemCount == 0 {
		return nil, ErrContractServiceItemRequired
	}
	signedAt := time.Now()
	if req != nil && req.SignedAt != nil {
		signedAt = *req.SignedAt
	}
	return s.client.SupportContract.UpdateOneID(contractID).
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.DeletedAtIsNil()).
		SetStatus("active").
		SetSignedAt(signedAt).
		SetNillableUpdatedBy(optionalActor(actorID)).
		AddVersion(1).
		Save(ctx)
}

func (s *Service) TerminateSupportContractWithReason(ctx context.Context, tenantID, actorID, contractID int, req *contractTerminateRequest) (*ent.SupportContract, error) {
	if _, err := s.supportContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.Reason) == "" {
		return nil, ErrContractStatusInvalid
	}
	terminatedAt := time.Now()
	if req.TerminatedAt != nil {
		terminatedAt = *req.TerminatedAt
	}
	return s.client.SupportContract.UpdateOneID(contractID).
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.DeletedAtIsNil()).
		SetStatus("terminated").
		SetTerminatedAt(terminatedAt).
		SetTerminationReason(strings.TrimSpace(req.Reason)).
		SetNillableUpdatedBy(optionalActor(actorID)).
		AddVersion(1).
		Save(ctx)
}

func (s *Service) RenewSupportContract(ctx context.Context, tenantID, actorID, contractID int, req *contractRenewRequest) (*ent.SupportContract, error) {
	old, err := s.supportContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	if old.Status != "active" && old.Status != "expired" && old.Status != "expiring" {
		return nil, ErrContractStatusInvalid
	}
	if req == nil || strings.TrimSpace(req.ContractNumber) == "" {
		return nil, ErrContractStatusInvalid
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	from := old.EffectiveFrom
	if req.EffectiveFrom != nil {
		from = req.EffectiveFrom
	}
	until := old.EffectiveUntil
	if req.EffectiveUntil != nil {
		until = req.EffectiveUntil
	}
	amount := old.Amount
	if req.Amount != nil {
		amount = req.Amount
	}
	currency := defaultStringOr(req.Currency, old.Currency)
	newCreate := tx.SupportContract.Create().
		SetTenantID(tenantID).
		SetCustomerID(old.CustomerID).
		SetNillableBranchID(old.BranchID).
		SetContractNumber(strings.TrimSpace(req.ContractNumber)).
		SetNormalizedContractNumber(NormalizeContractNumber(req.ContractNumber)).
		SetContractName(old.ContractName).
		SetContractType(old.ContractType).
		SetStatus("active").
		SetNillableStartAt(old.StartAt).
		SetNillableEndAt(old.EndAt).
		SetNillableEffectiveFrom(from).
		SetNillableEffectiveUntil(until).
		SetSignedAt(time.Now()).
		SetNillableAmount(amount).
		SetCurrency(defaultStringOr(currency, "CNY")).
		SetRenewalOfContractID(old.ID).
		SetNillableSLADefinitionID(old.SLADefinitionID).
		SetAutoRenew(old.AutoRenew).
		SetNillableCreatedBy(optionalActor(actorID)).
		SetNillableUpdatedBy(optionalActor(actorID))
	created, err := newCreate.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrContractNoDuplicate
		}
		return nil, err
	}
	items, err := tx.ContractServiceItem.Query().
		Where(
			contractserviceitem.TenantIDEQ(tenantID),
			contractserviceitem.ContractIDEQ(old.ID),
			contractserviceitem.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	itemMap := map[int]int{}
	for _, item := range items {
		newItem, err := tx.ContractServiceItem.Create().
			SetTenantID(tenantID).
			SetContractID(created.ID).
			SetNillableCatalogItemID(item.CatalogItemID).
			SetItemCode(item.ItemCode).
			SetItemName(item.ItemName).
			SetSpec(item.Spec).
			SetBillingUnit(item.BillingUnit).
			SetQuantity(item.Quantity).
			SetPeriodCount(item.PeriodCount).
			SetUnitPrice(item.UnitPrice).
			SetAmount(item.Amount).
			SetNillableQuotaTotal(item.QuotaTotal).
			SetQuotaUsed(0).
			SetNote(item.Note).
			SetNillableSLADefinitionID(item.SLADefinitionID).
			SetNillableResponseTargetMin(item.ResponseTargetMin).
			SetNillableResolveTargetMin(item.ResolveTargetMin).
			SetSortOrder(item.SortOrder).
			SetStatus(item.Status).
			SetNillableCreatedBy(optionalActor(actorID)).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		itemMap[item.ID] = newItem.ID
	}
	if len(req.CarryProjectIDs) > 0 {
		links, err := tx.ContractProjectLink.Query().
			Where(
				contractprojectlink.TenantIDEQ(tenantID),
				contractprojectlink.ContractIDEQ(old.ID),
				contractprojectlink.ProjectIDIn(req.CarryProjectIDs...),
				contractprojectlink.StatusEQ("active"),
				contractprojectlink.DeletedAtIsNil(),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		for _, link := range links {
			if _, err := tx.ContractProjectLink.UpdateOneID(link.ID).
				SetStatus("ended").
				SetEffectiveTo(now).
				Save(ctx); err != nil {
				return nil, err
			}
			newServiceItemID := (*int)(nil)
			if link.ServiceItemID != nil {
				if mapped, ok := itemMap[*link.ServiceItemID]; ok {
					newServiceItemID = &mapped
				} else if link.RelationType == "billing" {
					continue
				}
			}
			if link.RelationType == "billing" && newServiceItemID == nil {
				continue
			}
			if _, err := tx.ContractProjectLink.Create().
				SetTenantID(tenantID).
				SetContractID(created.ID).
				SetProjectID(link.ProjectID).
				SetNillableServiceItemID(newServiceItemID).
				SetRelationType(link.RelationType).
				SetNillableAllocatedAmount(link.AllocatedAmount).
				SetNillableAllocatedHours(link.AllocatedHours).
				SetNillableAllocationPct(link.AllocationPct).
				SetIsPrimary(link.IsPrimary).
				SetNote(link.Note).
				SetNillableEffectiveFrom(contractTimePtr(now)).
				SetNillableEffectiveTo(link.EffectiveTo).
				SetStatus("active").
				SetNillableCreatedBy(optionalActor(actorID)).
				Save(ctx); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.SupportContract.UpdateOneID(old.ID).
		Where(supportcontract.TenantIDEQ(tenantID)).
		SetStatus("renewed").
		SetNillableUpdatedBy(optionalActor(actorID)).
		AddVersion(1).
		Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}
