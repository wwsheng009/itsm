package email_intake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"itsm-backend/ent"
	"itsm-backend/ent/customerbranch"
	"itsm-backend/ent/customercontact"
	"itsm-backend/ent/customerexternalid"
	"itsm-backend/ent/customertenantlink"
	"itsm-backend/ent/group"
	"itsm-backend/ent/predicate"
	"itsm-backend/ent/servicecustomer"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/supportcontract"
	tenantpkg "itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
)

var (
	ErrCustomerNotFound          = errors.New("customer not found")
	ErrCustomerVersionConflict   = errors.New("customer version conflict")
	ErrCustomerDuplicate         = errors.New("customer duplicate")
	ErrCustomerInvalidReference  = errors.New("customer reference invalid")
	ErrCustomerInvalidEnum       = errors.New("customer enum invalid")
	ErrCustomerTenantLinkInvalid = errors.New("customer tenant link invalid")
	ErrCustomerContactInvalid    = errors.New("customer contact invalid")
	ErrCustomerContactDuplicate  = errors.New("customer contact duplicate")
)

// CustomerListFilters are the server-side filters for the customer master list.
// Keeping filters in the service prevents the UI from pretending a paginated
// page is the full result set.
type CustomerListFilters struct {
	Keyword          string
	Tier             string
	Status           string
	LifecycleStage   string
	AccountManagerID *int
	Industry         string
	HasTenantLink    *bool
	QualityBelow     *int16
	SortBy           string
	SortOrder        string
	Page             int
	PageSize         int
}

// CustomerListRow is the list projection for the customer master page.
type CustomerListRow struct {
	Customer           *ent.ServiceCustomer
	BranchCount        int
	ContractCount      int
	ContactCount       int
	TenantLinkCount    int
	AccountManagerName string
}

// CustomerDetail is the customer detail projection. Relationships/projects are
// loaded by their own panels; P2 external IDs are included here because they
// are part of the core master-data detail read.
type CustomerDetail struct {
	Customer           *ent.ServiceCustomer
	Branches           []*ent.CustomerBranch
	Contacts           []*ent.CustomerContact
	TenantLinks        []*ent.CustomerTenantLink
	ExternalIDs        []*ent.CustomerExternalID
	BranchCount        int
	ContractCount      int
	ContactCount       int
	TenantLinkCount    int
	AccountManagerName string
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeTaxID(value string) string {
	return strings.ToUpper(nonWord.ReplaceAllString(strings.TrimSpace(value), ""))
}

func nonNilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func customerCode(id int) string {
	return fmt.Sprintf("CUS-%04d", id)
}

func defaultStringOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func jsonArrayContainsFold(column, keyword string) predicate.ServiceCustomer {
	pattern := "%" + strings.ToLower(strings.TrimSpace(keyword)) + "%"
	return func(s *sql.Selector) {
		s.Where(sql.ExprP("LOWER(CAST("+s.C(column)+" AS TEXT)) LIKE ?", pattern))
	}
}

func (s *Service) ListCustomers(ctx context.Context, tenantID int, filters CustomerListFilters) ([]*CustomerListRow, int, error) {
	query := s.client.ServiceCustomer.Query().Where(
		servicecustomer.TenantIDEQ(tenantID),
		servicecustomer.DeletedAtIsNil(),
	)
	if value := strings.TrimSpace(filters.Tier); value != "" {
		query.Where(servicecustomer.TierEQ(value))
	}
	if value := strings.TrimSpace(filters.Status); value != "" {
		query.Where(servicecustomer.StatusEQ(value))
	}
	if value := strings.TrimSpace(filters.LifecycleStage); value != "" {
		query.Where(servicecustomer.LifecycleStageEQ(value))
	}
	if filters.AccountManagerID != nil && *filters.AccountManagerID > 0 {
		query.Where(servicecustomer.AccountManagerIDEQ(*filters.AccountManagerID))
	}
	if value := strings.TrimSpace(filters.Industry); value != "" {
		query.Where(servicecustomer.IndustryEQ(value))
	}
	if filters.QualityBelow != nil {
		query.Where(servicecustomer.DataQualityScoreLT(*filters.QualityBelow))
	}
	if keyword := strings.TrimSpace(filters.Keyword); keyword != "" {
		query.Where(servicecustomer.Or(
			servicecustomer.NameContainsFold(keyword),
			servicecustomer.NormalizedNameContainsFold(keyword),
			servicecustomer.ShortNameContainsFold(keyword),
			servicecustomer.LegalNameContainsFold(keyword),
			servicecustomer.EnNameContainsFold(keyword),
			servicecustomer.CodeContainsFold(keyword),
			servicecustomer.TaxIDContainsFold(keyword),
			servicecustomer.ServiceEmailContainsFold(keyword),
			jsonArrayContainsFold(servicecustomer.FieldAliases, keyword),
			jsonArrayContainsFold(servicecustomer.FieldHistoricalNames, keyword),
		))
	}
	if filters.HasTenantLink != nil {
		linkedIDs, err := s.client.CustomerTenantLink.Query().
			Where(
				customertenantlink.TenantIDEQ(tenantID),
				customertenantlink.DeletedAtIsNil(),
			).
			Select(customertenantlink.FieldCustomerID).
			Ints(ctx)
		if err != nil {
			return nil, 0, err
		}
		if *filters.HasTenantLink {
			if len(linkedIDs) == 0 {
				return []*CustomerListRow{}, 0, nil
			}
			query.Where(servicecustomer.IDIn(linkedIDs...))
		} else if len(linkedIDs) > 0 {
			query.Where(servicecustomer.IDNotIn(linkedIDs...))
		}
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(filters.Page, filters.PageSize)
	order := ent.Desc(servicecustomer.FieldUpdatedAt)
	switch strings.TrimSpace(filters.SortBy) {
	case "qualityScore":
		if strings.EqualFold(filters.SortOrder, "asc") {
			order = ent.Asc(servicecustomer.FieldDataQualityScore)
		} else {
			order = ent.Desc(servicecustomer.FieldDataQualityScore)
		}
	case "name":
		if strings.EqualFold(filters.SortOrder, "desc") {
			order = ent.Desc(servicecustomer.FieldName)
		} else {
			order = ent.Asc(servicecustomer.FieldName)
		}
	case "updatedAt":
		if strings.EqualFold(filters.SortOrder, "asc") {
			order = ent.Asc(servicecustomer.FieldUpdatedAt)
		}
	}
	customers, err := query.
		Order(order).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.enrichCustomerRows(ctx, tenantID, customers)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func (s *Service) enrichCustomerRows(ctx context.Context, tenantID int, customers []*ent.ServiceCustomer) ([]*CustomerListRow, error) {
	if len(customers) == 0 {
		return []*CustomerListRow{}, nil
	}
	ids := make([]int, 0, len(customers))
	managerIDs := make([]int, 0, len(customers))
	for _, customer := range customers {
		ids = append(ids, customer.ID)
		if customer.AccountManagerID != nil && *customer.AccountManagerID > 0 {
			managerIDs = append(managerIDs, *customer.AccountManagerID)
		}
	}

	branchCounts, err := s.countBranchesByCustomer(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	contractCounts, err := s.countContractsByCustomer(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	contactCounts, err := s.countContactsByCustomer(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	linkCounts, err := s.countTenantLinksByCustomer(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	managerNames, err := s.userNames(ctx, tenantID, managerIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]*CustomerListRow, 0, len(customers))
	for _, customer := range customers {
		managerName := ""
		if customer.AccountManagerID != nil {
			managerName = managerNames[*customer.AccountManagerID]
		}
		rows = append(rows, &CustomerListRow{
			Customer:           customer,
			BranchCount:        branchCounts[customer.ID],
			ContractCount:      contractCounts[customer.ID],
			ContactCount:       contactCounts[customer.ID],
			TenantLinkCount:    linkCounts[customer.ID],
			AccountManagerName: managerName,
		})
	}
	return rows, nil
}

func (s *Service) countBranchesByCustomer(ctx context.Context, tenantID int, customerIDs []int) (map[int]int, error) {
	result := map[int]int{}
	rows, err := s.client.CustomerBranch.Query().
		Where(
			customerbranch.TenantIDEQ(tenantID),
			customerbranch.CustomerIDIn(customerIDs...),
			customerbranch.DeletedAtIsNil(),
		).
		Select(customerbranch.FieldCustomerID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.CustomerID]++
	}
	return result, nil
}

func (s *Service) countContractsByCustomer(ctx context.Context, tenantID int, customerIDs []int) (map[int]int, error) {
	result := map[int]int{}
	rows, err := s.client.SupportContract.Query().
		Where(
			supportcontract.TenantIDEQ(tenantID),
			supportcontract.CustomerIDIn(customerIDs...),
		).
		Select(supportcontract.FieldCustomerID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.CustomerID]++
	}
	return result, nil
}

func (s *Service) countContactsByCustomer(ctx context.Context, tenantID int, customerIDs []int) (map[int]int, error) {
	result := map[int]int{}
	rows, err := s.client.CustomerContact.Query().
		Where(
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDIn(customerIDs...),
			customercontact.StatusEQ("active"),
			customercontact.DeletedAtIsNil(),
		).
		Select(customercontact.FieldCustomerID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.CustomerID]++
	}
	return result, nil
}

func (s *Service) countTenantLinksByCustomer(ctx context.Context, tenantID int, customerIDs []int) (map[int]int, error) {
	result := map[int]int{}
	rows, err := s.client.CustomerTenantLink.Query().
		Where(
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDIn(customerIDs...),
			customertenantlink.DeletedAtIsNil(),
		).
		Select(customertenantlink.FieldCustomerID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.CustomerID]++
	}
	return result, nil
}

func (s *Service) userNames(ctx context.Context, tenantID int, userIDs []int) (map[int]string, error) {
	result := map[int]string{}
	if len(userIDs) == 0 {
		return result, nil
	}
	rows, err := s.client.User.Query().
		Where(user.TenantIDEQ(tenantID), user.IDIn(userIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.Name
	}
	return result, nil
}

func (s *Service) GetCustomerDetail(ctx context.Context, tenantID, id int) (*CustomerDetail, error) {
	customer, err := s.client.ServiceCustomer.Query().
		Where(
			servicecustomer.IDEQ(id),
			servicecustomer.TenantIDEQ(tenantID),
			servicecustomer.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, err
	}

	branches, err := s.client.CustomerBranch.Query().
		Where(
			customerbranch.TenantIDEQ(tenantID),
			customerbranch.CustomerIDEQ(id),
			customerbranch.DeletedAtIsNil(),
		).
		Order(ent.Desc(customerbranch.FieldIsPrimary), ent.Asc(customerbranch.FieldName)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	contacts, err := s.client.CustomerContact.Query().
		Where(
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(id),
			customercontact.DeletedAtIsNil(),
		).
		Order(ent.Desc(customercontact.FieldIsPrimary), ent.Asc(customercontact.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	activeContactCount, err := s.client.CustomerContact.Query().
		Where(
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(id),
			customercontact.StatusEQ("active"),
			customercontact.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	links, err := s.client.CustomerTenantLink.Query().
		Where(
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDEQ(id),
			customertenantlink.DeletedAtIsNil(),
		).
		Order(ent.Desc(customertenantlink.FieldIsBillingAnchor), ent.Asc(customertenantlink.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	externalIDs, err := s.client.CustomerExternalID.Query().
		Where(
			customerexternalid.TenantIDEQ(tenantID),
			customerexternalid.CustomerIDEQ(id),
		).
		Order(ent.Asc(customerexternalid.FieldSystemCode), ent.Asc(customerexternalid.FieldExternalID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	contractCount, err := s.client.SupportContract.Query().
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.CustomerIDEQ(id)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	managerName := ""
	if customer.AccountManagerID != nil {
		managerName = s.userNamesForOne(ctx, tenantID, *customer.AccountManagerID)
	}
	detail := &CustomerDetail{
		Customer:           customer,
		Branches:           branches,
		Contacts:           contacts,
		TenantLinks:        links,
		ExternalIDs:        externalIDs,
		BranchCount:        len(branches),
		ContractCount:      contractCount,
		ContactCount:       activeContactCount,
		TenantLinkCount:    len(links),
		AccountManagerName: managerName,
	}
	return detail, nil
}

func (s *Service) userNamesForOne(ctx context.Context, tenantID, userID int) string {
	if userID < 1 {
		return ""
	}
	row, err := s.client.User.Query().
		Where(user.IDEQ(userID), user.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		return ""
	}
	return row.Name
}

func (s *Service) CreateCustomerMaster(ctx context.Context, tenantID, actorID int, req *customerRequest) (*ent.ServiceCustomer, error) {
	if err := s.validateCustomerReferences(ctx, tenantID, req); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	entity, err := s.client.ServiceCustomer.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetNormalizedName(NormalizeName(name)).
		SetShortName(req.ShortName).
		SetLegalName(req.LegalName).
		SetEnName(req.EnName).
		SetAliases(nonNilStrings(req.Aliases)).
		SetHistoricalNames(nonNilStrings(req.HistoricalNames)).
		SetTaxID(req.TaxID).
		SetNormalizedTaxID(normalizeTaxID(req.TaxID)).
		SetDunsNumber(req.DunsNumber).
		SetRegistrationNo(req.RegistrationNo).
		SetEmailDomains(nonNilStrings(req.EmailDomains)).
		SetCustomerType(defaultStringOr(req.CustomerType, "direct")).
		SetIndustry(req.Industry).
		SetSubIndustry(req.SubIndustry).
		SetSizeClass(req.SizeClass).
		SetOwnershipType(req.OwnershipType).
		SetTier(defaultStringOr(req.Tier, "C")).
		SetVipLevel(req.VIPLevel).
		SetCreditRating(req.CreditRating).
		SetWebsite(req.Website).
		SetMainPhone(req.MainPhone).
		SetServiceEmail(req.ServiceEmail).
		SetAddress(nonNilMap(req.Address)).
		SetTimezone(req.Timezone).
		SetLanguage(defaultStringOr(req.Language, "zh-CN")).
		SetServiceTier(req.ServiceTier).
		SetNillableSLADefinitionID(req.SLADefinitionID).
		SetSupportHours(req.SupportHours).
		SetCoverageScope(nonNilMap(req.CoverageScope)).
		SetNillableAccountManagerID(req.AccountManagerID).
		SetNillableServiceOwnerID(req.ServiceOwnerID).
		SetNillableCsManagerID(req.CSManagerID).
		SetNillableOncallGroupID(req.OncallGroupID).
		SetBillingCurrency(req.BillingCurrency).
		SetPaymentTerms(req.PaymentTerms).
		SetNillableCreditLimit(req.CreditLimit).
		SetInvoiceTitle(req.InvoiceTitle).
		SetBillingAddress(nonNilMap(req.BillingAddress)).
		SetLifecycleStage(defaultStringOr(req.LifecycleStage, "prospect")).
		SetNillableOnboardedAt(req.OnboardedAt).
		SetNillableContractStartAt(req.ContractStartAt).
		SetNillableContractEndAt(req.ContractEndAt).
		SetNillableChurnAt(req.ChurnAt).
		SetChurnReason(req.ChurnReason).
		SetDataSource(defaultStringOr(req.DataSource, "manual")).
		SetStatus(defaultStringOr(req.Status, "active")).
		SetNillableLinkedCustomerTenantID(req.LinkedCustomerTenantID).
		SetNillableCreatedBy(optionalActor(actorID)).
		SetNillableUpdatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerDuplicate
		}
		return nil, err
	}
	if strings.TrimSpace(req.Code) != "" {
		entity, err = s.client.ServiceCustomer.UpdateOneID(entity.ID).
			SetCode(strings.TrimSpace(req.Code)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return nil, ErrCustomerDuplicate
			}
			return nil, err
		}
	} else {
		entity, err = s.client.ServiceCustomer.UpdateOneID(entity.ID).
			SetCode(customerCode(entity.ID)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return nil, ErrCustomerDuplicate
			}
			return nil, err
		}
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, entity.ID); err != nil {
		return nil, err
	}
	return s.client.ServiceCustomer.Get(ctx, entity.ID)
}

func optionalActor(actorID int) *int {
	if actorID < 1 {
		return nil
	}
	return &actorID
}

func (s *Service) validateCustomerReferences(ctx context.Context, tenantID int, req *customerRequest) error {
	if req.VIPLevel < 0 || req.VIPLevel > 5 {
		return ErrCustomerInvalidEnum
	}
	if req.CreditLimit != nil && *req.CreditLimit < 0 {
		return ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.CustomerType); value != "" && !allowedCustomerTypes[value] {
		return ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.Tier); value != "" && !allowedTiers[value] {
		return ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.LifecycleStage); value != "" && !allowedLifecycleStages[value] {
		return ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.DataSource); value != "" && !allowedDataSources[value] {
		return ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.Status); value != "" && !allowedCustomerStatuses[value] {
		return ErrCustomerInvalidEnum
	}
	userIDs := make(map[int]struct{})
	for _, id := range []*int{req.AccountManagerID, req.ServiceOwnerID, req.CSManagerID} {
		if id != nil && *id > 0 {
			userIDs[*id] = struct{}{}
		}
	}
	if len(userIDs) > 0 {
		ids := make([]int, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		count, err := s.client.User.Query().
			Where(user.TenantIDEQ(tenantID), user.IDIn(ids...), user.ActiveEQ(true)).
			Count(ctx)
		if err != nil {
			return err
		}
		if count != len(ids) {
			return ErrCustomerInvalidReference
		}
	}
	if req.OncallGroupID != nil && *req.OncallGroupID > 0 {
		exists, err := s.client.Group.Query().
			Where(group.IDEQ(*req.OncallGroupID), group.TenantIDEQ(tenantID)).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return ErrCustomerInvalidReference
		}
	}
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
	return nil
}

var allowedCustomerTypes = map[string]bool{
	"direct": true, "group": true, "subsidiary": true, "msp_endcustomer": true, "internal": true,
}

var allowedTiers = map[string]bool{"S": true, "A": true, "B": true, "C": true}

var allowedLifecycleStages = map[string]bool{
	"prospect": true, "onboarding": true, "active": true, "renewal": true, "churned": true, "archived": true,
}

var allowedDataSources = map[string]bool{
	"manual": true, "email_intake": true, "import": true, "crm_sync": true, "ai_extract": true,
}

var allowedCustomerStatuses = map[string]bool{"active": true, "inactive": true, "suspended": true}

var allowedContactRoles = map[string]bool{
	"business": true, "technical": true, "billing": true, "escalation": true, "noc": true, "executive": true,
}

var allowedTenantLinkTypes = map[string]bool{
	"primary": true, "secondary": true, "sandbox": true, "archive": true, "regional": true,
}

var allowedProvisionStatuses = map[string]bool{
	"pending": true, "provisioning": true, "ready": true, "failed": true,
}

func (s *Service) UpdateCustomerMaster(ctx context.Context, tenantID, actorID, id int, req *customerRequest) (*ent.ServiceCustomer, error) {
	if err := s.validateCustomerReferences(ctx, tenantID, req); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	update := s.client.ServiceCustomer.Update().
		Where(
			servicecustomer.IDEQ(id),
			servicecustomer.TenantIDEQ(tenantID),
			servicecustomer.DeletedAtIsNil(),
		)
	if req.Version != nil {
		update.Where(servicecustomer.VersionEQ(*req.Version))
	}
	update.
		SetName(name).
		SetNormalizedName(NormalizeName(name)).
		SetCode(strings.TrimSpace(req.Code)).
		SetShortName(req.ShortName).
		SetLegalName(req.LegalName).
		SetEnName(req.EnName).
		SetAliases(nonNilStrings(req.Aliases)).
		SetHistoricalNames(nonNilStrings(req.HistoricalNames)).
		SetTaxID(req.TaxID).
		SetNormalizedTaxID(normalizeTaxID(req.TaxID)).
		SetDunsNumber(req.DunsNumber).
		SetRegistrationNo(req.RegistrationNo).
		SetEmailDomains(nonNilStrings(req.EmailDomains)).
		SetCustomerType(defaultStringOr(req.CustomerType, "direct")).
		SetIndustry(req.Industry).
		SetSubIndustry(req.SubIndustry).
		SetSizeClass(req.SizeClass).
		SetOwnershipType(req.OwnershipType).
		SetTier(defaultStringOr(req.Tier, "C")).
		SetVipLevel(req.VIPLevel).
		SetCreditRating(req.CreditRating).
		SetWebsite(req.Website).
		SetMainPhone(req.MainPhone).
		SetServiceEmail(req.ServiceEmail).
		SetAddress(nonNilMap(req.Address)).
		SetTimezone(req.Timezone).
		SetLanguage(defaultStringOr(req.Language, "zh-CN")).
		SetServiceTier(req.ServiceTier).
		SetSupportHours(req.SupportHours).
		SetCoverageScope(nonNilMap(req.CoverageScope)).
		SetBillingCurrency(req.BillingCurrency).
		SetPaymentTerms(req.PaymentTerms).
		SetInvoiceTitle(req.InvoiceTitle).
		SetBillingAddress(nonNilMap(req.BillingAddress)).
		SetLifecycleStage(defaultStringOr(req.LifecycleStage, "prospect")).
		SetChurnReason(req.ChurnReason).
		SetDataSource(defaultStringOr(req.DataSource, "manual")).
		SetStatus(defaultStringOr(req.Status, "active")).
		SetNillableUpdatedBy(optionalActor(actorID))
	applyOptionalCustomerUpdate(update, req)
	update.AddVersion(1)
	affected, err := update.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerDuplicate
		}
		return nil, err
	}
	if affected == 0 {
		if req.Version != nil {
			return nil, ErrCustomerVersionConflict
		}
		return nil, ErrCustomerNotFound
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, id); err != nil {
		return nil, err
	}
	return s.client.ServiceCustomer.Get(ctx, id)
}

func applyOptionalCustomerUpdate(update *ent.ServiceCustomerUpdate, req *customerRequest) {
	if req.SLADefinitionID != nil {
		update.SetSLADefinitionID(*req.SLADefinitionID)
	} else {
		update.ClearSLADefinitionID()
	}
	if req.AccountManagerID != nil {
		update.SetAccountManagerID(*req.AccountManagerID)
	} else {
		update.ClearAccountManagerID()
	}
	if req.ServiceOwnerID != nil {
		update.SetServiceOwnerID(*req.ServiceOwnerID)
	} else {
		update.ClearServiceOwnerID()
	}
	if req.CSManagerID != nil {
		update.SetCsManagerID(*req.CSManagerID)
	} else {
		update.ClearCsManagerID()
	}
	if req.OncallGroupID != nil {
		update.SetOncallGroupID(*req.OncallGroupID)
	} else {
		update.ClearOncallGroupID()
	}
	if req.CreditLimit != nil {
		update.SetCreditLimit(*req.CreditLimit)
	} else {
		update.ClearCreditLimit()
	}
	if req.OnboardedAt != nil {
		update.SetOnboardedAt(*req.OnboardedAt)
	} else {
		update.ClearOnboardedAt()
	}
	if req.ContractStartAt != nil {
		update.SetContractStartAt(*req.ContractStartAt)
	} else {
		update.ClearContractStartAt()
	}
	if req.ContractEndAt != nil {
		update.SetContractEndAt(*req.ContractEndAt)
	} else {
		update.ClearContractEndAt()
	}
	if req.ChurnAt != nil {
		update.SetChurnAt(*req.ChurnAt)
	} else {
		update.ClearChurnAt()
	}
	if req.LinkedCustomerTenantID != nil {
		update.SetLinkedCustomerTenantID(*req.LinkedCustomerTenantID)
	} else {
		update.ClearLinkedCustomerTenantID()
	}
}

func (s *Service) DisableCustomerMaster(ctx context.Context, tenantID, actorID, id int) (*ent.ServiceCustomer, error) {
	affected, err := s.client.ServiceCustomer.Update().
		Where(
			servicecustomer.IDEQ(id),
			servicecustomer.TenantIDEQ(tenantID),
			servicecustomer.DeletedAtIsNil(),
		).
		SetStatus("inactive").
		SetNillableUpdatedBy(optionalActor(actorID)).
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrCustomerNotFound
	}
	return s.client.ServiceCustomer.Get(ctx, id)
}

// ─── Customer contacts ──────────────────────────────────────────────────────

type customerContactRequest struct {
	BranchID     *int   `json:"branchId"`
	Name         string `json:"name" binding:"required"`
	NameEn       string `json:"nameEn"`
	Title        string `json:"title"`
	Department   string `json:"department"`
	Email        string `json:"email"`
	Mobile       string `json:"mobile"`
	Phone        string `json:"phone"`
	Wechat       string `json:"wechat"`
	ContactRole  string `json:"contactRole"`
	IsPrimary    bool   `json:"isPrimary"`
	Language     string `json:"language"`
	Timezone     string `json:"timezone"`
	OptIn        *bool  `json:"optIn"`
	LinkedUserID *int   `json:"linkedUserId"`
	Source       string `json:"source"`
	Status       string `json:"status"`
}

func (s *Service) ListCustomerContacts(ctx context.Context, tenantID, customerID int) ([]*ent.CustomerContact, error) {
	if _, err := s.GetCustomerDetail(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return s.client.CustomerContact.Query().
		Where(
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(customerID),
			customercontact.DeletedAtIsNil(),
		).
		Order(ent.Desc(customercontact.FieldIsPrimary), ent.Asc(customercontact.FieldCreatedAt)).
		All(ctx)
}

func (s *Service) CreateCustomerContact(ctx context.Context, tenantID, customerID int, req *customerContactRequest) (*ent.CustomerContact, error) {
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	if !allowedContactRoles[defaultStringOr(req.ContactRole, "business")] {
		return nil, ErrCustomerInvalidEnum
	}
	if err := s.validateCustomerBranch(ctx, tenantID, customerID, req.BranchID); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if req.IsPrimary {
		if _, err := tx.CustomerContact.Update().
			Where(
				customercontact.TenantIDEQ(tenantID),
				customercontact.CustomerIDEQ(customerID),
				customercontact.ContactRoleEQ(defaultStringOr(req.ContactRole, "business")),
				customercontact.StatusEQ("active"),
				customercontact.DeletedAtIsNil(),
			).
			SetIsPrimary(false).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	entity, err := tx.CustomerContact.Create().
		SetTenantID(tenantID).
		SetCustomerID(customerID).
		SetNillableBranchID(req.BranchID).
		SetName(strings.TrimSpace(req.Name)).
		SetNameEn(req.NameEn).
		SetTitle(req.Title).
		SetDepartment(req.Department).
		SetEmail(req.Email).
		SetNormalizedEmail(normalizeEmail(req.Email)).
		SetMobile(req.Mobile).
		SetPhone(req.Phone).
		SetWechat(req.Wechat).
		SetContactRole(defaultStringOr(req.ContactRole, "business")).
		SetIsPrimary(req.IsPrimary).
		SetLanguage(req.Language).
		SetTimezone(req.Timezone).
		SetOptIn(boolOrDefault(req.OptIn, true)).
		SetNillableLinkedUserID(req.LinkedUserID).
		SetSource(defaultStringOr(req.Source, "manual")).
		SetStatus(defaultStringOr(req.Status, "active")).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerContactDuplicate
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *Service) UpdateCustomerContact(ctx context.Context, tenantID, customerID, contactID int, req *customerContactRequest) (*ent.CustomerContact, error) {
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	if !allowedContactRoles[defaultStringOr(req.ContactRole, "business")] {
		return nil, ErrCustomerInvalidEnum
	}
	if err := s.validateCustomerBranch(ctx, tenantID, customerID, req.BranchID); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if req.IsPrimary {
		if _, err := tx.CustomerContact.Update().
			Where(
				customercontact.TenantIDEQ(tenantID),
				customercontact.CustomerIDEQ(customerID),
				customercontact.IDNEQ(contactID),
				customercontact.ContactRoleEQ(defaultStringOr(req.ContactRole, "business")),
				customercontact.StatusEQ("active"),
				customercontact.DeletedAtIsNil(),
			).
			SetIsPrimary(false).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	affected, err := tx.CustomerContact.Update().
		Where(
			customercontact.IDEQ(contactID),
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(customerID),
			customercontact.DeletedAtIsNil(),
		).
		SetNillableBranchID(req.BranchID).
		SetName(strings.TrimSpace(req.Name)).
		SetNameEn(req.NameEn).
		SetTitle(req.Title).
		SetDepartment(req.Department).
		SetEmail(req.Email).
		SetNormalizedEmail(normalizeEmail(req.Email)).
		SetMobile(req.Mobile).
		SetPhone(req.Phone).
		SetWechat(req.Wechat).
		SetContactRole(defaultStringOr(req.ContactRole, "business")).
		SetIsPrimary(req.IsPrimary).
		SetLanguage(req.Language).
		SetTimezone(req.Timezone).
		SetOptIn(boolOrDefault(req.OptIn, true)).
		SetNillableLinkedUserID(req.LinkedUserID).
		SetSource(defaultStringOr(req.Source, "manual")).
		SetStatus(defaultStringOr(req.Status, "active")).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerContactDuplicate
		}
		return nil, err
	}
	if affected == 0 {
		return nil, ErrCustomerContactInvalid
	}
	entity, err := tx.CustomerContact.Get(ctx, contactID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *Service) DeleteCustomerContact(ctx context.Context, tenantID, customerID, contactID int) error {
	affected, err := s.client.CustomerContact.Update().
		Where(
			customercontact.IDEQ(contactID),
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(customerID),
			customercontact.DeletedAtIsNil(),
		).
		SetStatus("inactive").
		SetIsPrimary(false).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCustomerContactInvalid
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return err
	}
	return nil
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (s *Service) validateCustomerBranch(ctx context.Context, tenantID, customerID int, branchID *int) error {
	if branchID == nil || *branchID < 1 {
		return nil
	}
	exists, err := s.client.CustomerBranch.Query().
		Where(
			customerbranch.IDEQ(*branchID),
			customerbranch.TenantIDEQ(tenantID),
			customerbranch.CustomerIDEQ(customerID),
			customerbranch.DeletedAtIsNil(),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrCustomerContactInvalid
	}
	return nil
}

// ─── Customer tenant links ──────────────────────────────────────────────────

type customerTenantLinkRequest struct {
	LinkedTenantID  int        `json:"linkedTenantId" binding:"required"`
	LinkType        string     `json:"linkType"`
	IsBillingAnchor bool       `json:"isBillingAnchor"`
	ProvisionStatus string     `json:"provisionStatus"`
	ProvisionedAt   *time.Time `json:"provisionedAt"`
	Note            string     `json:"note"`
}

func (s *Service) ListCustomerTenantLinks(ctx context.Context, tenantID, customerID int) ([]*ent.CustomerTenantLink, error) {
	if _, err := s.GetCustomerDetail(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return s.client.CustomerTenantLink.Query().
		Where(
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDEQ(customerID),
			customertenantlink.DeletedAtIsNil(),
		).
		Order(ent.Desc(customertenantlink.FieldIsBillingAnchor), ent.Asc(customertenantlink.FieldCreatedAt)).
		All(ctx)
}

func (s *Service) CreateCustomerTenantLink(ctx context.Context, tenantID, customerID int, req *customerTenantLinkRequest) (*ent.CustomerTenantLink, error) {
	if !allowedTenantLinkTypes[defaultStringOr(req.LinkType, "primary")] {
		return nil, ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.ProvisionStatus); value != "" && !allowedProvisionStatuses[value] {
		return nil, ErrCustomerInvalidEnum
	}
	if exists, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(customerID), servicecustomer.TenantIDEQ(tenantID), servicecustomer.DeletedAtIsNil()).
		Exist(ctx); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrCustomerNotFound
	}
	if err := s.validateTenantLinkTarget(ctx, tenantID, req.LinkedTenantID); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := clearTenantLinkConflicts(ctx, tx, tenantID, customerID, 0, req); err != nil {
		return nil, err
	}
	entity, err := tx.CustomerTenantLink.Create().
		SetTenantID(tenantID).
		SetCustomerID(customerID).
		SetLinkedTenantID(req.LinkedTenantID).
		SetLinkType(defaultStringOr(req.LinkType, "primary")).
		SetIsBillingAnchor(req.IsBillingAnchor).
		SetProvisionStatus(defaultStringOr(req.ProvisionStatus, "pending")).
		SetNillableProvisionedAt(req.ProvisionedAt).
		SetNote(req.Note).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *Service) UpdateCustomerTenantLink(ctx context.Context, tenantID, customerID, linkID int, req *customerTenantLinkRequest) (*ent.CustomerTenantLink, error) {
	if !allowedTenantLinkTypes[defaultStringOr(req.LinkType, "primary")] {
		return nil, ErrCustomerInvalidEnum
	}
	if value := strings.TrimSpace(req.ProvisionStatus); value != "" && !allowedProvisionStatuses[value] {
		return nil, ErrCustomerInvalidEnum
	}
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	if err := s.validateTenantLinkTarget(ctx, tenantID, req.LinkedTenantID); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := clearTenantLinkConflicts(ctx, tx, tenantID, customerID, linkID, req); err != nil {
		return nil, err
	}
	update := tx.CustomerTenantLink.Update().
		Where(
			customertenantlink.IDEQ(linkID),
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDEQ(customerID),
			customertenantlink.DeletedAtIsNil(),
		).
		SetLinkedTenantID(req.LinkedTenantID).
		SetLinkType(defaultStringOr(req.LinkType, "primary")).
		SetIsBillingAnchor(req.IsBillingAnchor).
		SetNillableProvisionedAt(req.ProvisionedAt).
		SetNote(req.Note)
	if strings.TrimSpace(req.ProvisionStatus) != "" {
		update.SetProvisionStatus(req.ProvisionStatus)
	}
	affected, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrCustomerTenantLinkInvalid
	}
	entity, err := tx.CustomerTenantLink.Get(ctx, linkID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *Service) DeleteCustomerTenantLink(ctx context.Context, tenantID, customerID, linkID int) error {
	affected, err := s.client.CustomerTenantLink.Update().
		Where(
			customertenantlink.IDEQ(linkID),
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDEQ(customerID),
			customertenantlink.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCustomerTenantLinkInvalid
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, customerID); err != nil {
		return err
	}
	return nil
}

func clearTenantLinkConflicts(ctx context.Context, tx *ent.Tx, tenantID, customerID, linkID int, req *customerTenantLinkRequest) error {
	if defaultStringOr(req.LinkType, "primary") == "primary" {
		update := tx.CustomerTenantLink.Update().
			Where(
				customertenantlink.TenantIDEQ(tenantID),
				customertenantlink.CustomerIDEQ(customerID),
				customertenantlink.LinkTypeEQ("primary"),
				customertenantlink.DeletedAtIsNil(),
			)
		if linkID > 0 {
			update.Where(customertenantlink.IDNEQ(linkID))
		}
		if _, err := update.SetLinkType("secondary").Save(ctx); err != nil {
			return err
		}
	}
	if req.IsBillingAnchor {
		update := tx.CustomerTenantLink.Update().
			Where(
				customertenantlink.TenantIDEQ(tenantID),
				customertenantlink.CustomerIDEQ(customerID),
				customertenantlink.IsBillingAnchorEQ(true),
				customertenantlink.DeletedAtIsNil(),
			)
		if linkID > 0 {
			update.Where(customertenantlink.IDNEQ(linkID))
		}
		if _, err := update.SetIsBillingAnchor(false).Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateTenantLinkTarget(ctx context.Context, tenantID, linkedTenantID int) error {
	current, err := s.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return ErrCustomerTenantLinkInvalid
	}
	target, err := s.client.Tenant.Get(ctx, linkedTenantID)
	if err != nil {
		return ErrCustomerTenantLinkInvalid
	}
	if target.Status != "active" {
		return ErrCustomerTenantLinkInvalid
	}
	if current.Type == tenantpkg.TypeMspProvider {
		if target.Type != tenantpkg.TypeMspCustomer {
			return ErrCustomerTenantLinkInvalid
		}
		if target.MspProviderID != current.ID {
			return ErrCustomerTenantLinkInvalid
		}
	}
	return nil
}
