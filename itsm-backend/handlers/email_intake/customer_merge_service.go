package email_intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/externalcontractreference"
	"itsm-backend/ent/servicecustomer"
	"itsm-backend/ent/supportcontract"
)

var (
	ErrCustomerMergeInvalid       = errors.New("customer merge invalid")
	ErrCustomerMergeConflict      = errors.New("customer merge conflict")
	ErrCustomerMergeUndoExpired   = errors.New("customer merge undo expired")
	ErrCustomerMergeAlreadyUndone = errors.New("customer merge already undone")
)

type CustomerMergeConflictError struct {
	Conflicts []string
}

func (e *CustomerMergeConflictError) Error() string {
	return strings.Join(e.Conflicts, "; ")
}

type customerMergeRequest struct {
	TargetCustomerID int `json:"targetCustomerId" binding:"required"`
}

type mergeBranchMove struct {
	OldID int `json:"oldId"`
	NewID int `json:"newId"`
}

type mergeChildMove struct {
	ID          int  `json:"id"`
	OldBranchID *int `json:"oldBranchId,omitempty"`
	NewBranchID *int `json:"newBranchId,omitempty"`
}

type mergeContactMove struct {
	ID           int  `json:"id"`
	OldBranchID  *int `json:"oldBranchId,omitempty"`
	NewBranchID  *int `json:"newBranchId,omitempty"`
	OldIsPrimary bool `json:"oldIsPrimary"`
}

type mergeContactDelete struct {
	ID           int    `json:"id"`
	OldBranchID  *int   `json:"oldBranchId,omitempty"`
	NewBranchID  *int   `json:"newBranchId,omitempty"`
	OldIsPrimary bool   `json:"oldIsPrimary"`
	OldStatus    string `json:"oldStatus"`
}

type mergeTenantLinkMove struct {
	ID          int    `json:"id"`
	OldLinkType string `json:"oldLinkType"`
	NewLinkType string `json:"newLinkType"`
	OldBilling  bool   `json:"oldBilling"`
	NewBilling  bool   `json:"newBilling"`
}

type mergeRelationshipMove struct {
	ID        int `json:"id"`
	OldFromID int `json:"oldFromId"`
	OldToID   int `json:"oldToId"`
}

type mergeRelationshipDelete struct {
	ID        int    `json:"id"`
	OldStatus string `json:"oldStatus"`
}

type mergeProjectLinkMove struct {
	ID           int  `json:"id"`
	OldBranchID  *int `json:"oldBranchId,omitempty"`
	NewBranchID  *int `json:"newBranchId,omitempty"`
	OldIsPrimary bool `json:"oldIsPrimary"`
	NewIsPrimary bool `json:"newIsPrimary"`
}

type mergeProjectLinkDelete struct {
	ID           int  `json:"id"`
	OldIsPrimary bool `json:"oldIsPrimary"`
}

type mergeSnapshot struct {
	Version               int                       `json:"version"`
	SourceID              int                       `json:"sourceId"`
	TargetID              int                       `json:"targetId"`
	SourceStatus          string                    `json:"sourceStatus"`
	SourceLinkedTenantID  *int                      `json:"sourceLinkedTenantId,omitempty"`
	SourceHistoricalNames []string                  `json:"sourceHistoricalNames"`
	SourceVersion         int64                     `json:"sourceVersion"`
	TargetVersion         int64                     `json:"targetVersion"`
	Branches              []mergeBranchMove         `json:"branches"`
	Contacts              []mergeContactMove        `json:"contacts"`
	ContactsDeleted       []mergeContactDelete      `json:"contactsDeleted"`
	TenantLinks           []mergeTenantLinkMove     `json:"tenantLinks"`
	TenantLinksDeleted    []int                     `json:"tenantLinksDeleted"`
	Relationships         []mergeRelationshipMove   `json:"relationships"`
	RelationshipsDeleted  []mergeRelationshipDelete `json:"relationshipsDeleted"`
	Contracts             []mergeChildMove          `json:"contracts"`
	ExternalRefs          []mergeChildMove          `json:"externalRefs"`
	ExternalIDs           []int                     `json:"externalIds"`
	Conversations         []mergeChildMove          `json:"conversations"`
	ProjectLinks          []mergeProjectLinkMove    `json:"projectLinks"`
	ProjectLinksDeleted   []mergeProjectLinkDelete  `json:"projectLinksDeleted"`
}

type mergeReport struct {
	SourceID             int   `json:"sourceId"`
	TargetID             int   `json:"targetId"`
	BranchesCreated      int   `json:"branchesCreated"`
	ContactsMoved        int   `json:"contactsMoved"`
	ContactsDeleted      int   `json:"contactsDeleted"`
	TenantLinksMoved     int   `json:"tenantLinksMoved"`
	TenantLinksDeleted   int   `json:"tenantLinksDeleted"`
	RelationshipsMoved   int   `json:"relationshipsMoved"`
	RelationshipsDeleted int   `json:"relationshipsDeleted"`
	ContractsMoved       int   `json:"contractsMoved"`
	ExternalRefsMoved    int   `json:"externalRefsMoved"`
	ExternalIDsMoved     int   `json:"externalIdsMoved"`
	ConversationsMoved   int   `json:"conversationsMoved"`
	ProjectLinksMoved    int   `json:"projectLinksMoved"`
	ProjectLinksDeleted  int   `json:"projectLinksDeleted"`
	DurationMs           int64 `json:"durationMs"`
}

type mergeAuditPayload struct {
	Kind     string        `json:"kind"`
	Snapshot mergeSnapshot `json:"snapshot"`
	Report   mergeReport   `json:"report"`
}

func (s *Service) MergeCustomers(ctx context.Context, tenantID, actorID, sourceID int, req *customerMergeRequest) (*mergeReport, error) {
	if req == nil || req.TargetCustomerID < 1 || req.TargetCustomerID == sourceID {
		return nil, ErrCustomerMergeInvalid
	}
	started := time.Now()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	source, err := getCustomerInTx(ctx, tx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	target, err := getCustomerInTx(ctx, tx, tenantID, req.TargetCustomerID)
	if err != nil {
		return nil, err
	}
	if source.MergedIntoID != nil {
		return nil, ErrCustomerMergeInvalid
	}

	conflicts, err := s.precheckMergeConflicts(ctx, tx, tenantID, source, target)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, &CustomerMergeConflictError{Conflicts: conflicts}
	}

	snapshot := mergeSnapshot{
		Version:               1,
		SourceID:              source.ID,
		TargetID:              target.ID,
		SourceStatus:          source.Status,
		SourceLinkedTenantID:  source.LinkedCustomerTenantID,
		SourceHistoricalNames: append([]string{}, source.HistoricalNames...),
		SourceVersion:         source.Version,
		TargetVersion:         target.Version,
	}
	report := mergeReport{SourceID: source.ID, TargetID: target.ID}

	branchMap, branchMoves, err := s.mergeCustomerBranches(ctx, tx, tenantID, source, target)
	if err != nil {
		return nil, err
	}
	snapshot.Branches = branchMoves
	report.BranchesCreated = len(branchMoves)

	contactsMoved, contactsDeleted, contactMoves, contactDeletes, err := s.mergeCustomerContacts(ctx, tx, tenantID, source.ID, target.ID, branchMap)
	if err != nil {
		return nil, err
	}
	snapshot.Contacts = contactMoves
	snapshot.ContactsDeleted = contactDeletes
	report.ContactsMoved = contactsMoved
	report.ContactsDeleted = contactsDeleted

	tenantLinkMoves, tenantLinkDeletes, err := s.mergeCustomerTenantLinks(ctx, tx, tenantID, source.ID, target.ID)
	if err != nil {
		return nil, err
	}
	snapshot.TenantLinks = tenantLinkMoves
	snapshot.TenantLinksDeleted = tenantLinkDeletes
	report.TenantLinksMoved = len(tenantLinkMoves)
	report.TenantLinksDeleted = len(tenantLinkDeletes)

	relationshipMoves, relationshipDeletes, err := s.mergeCustomerRelationships(ctx, tx, tenantID, source.ID, target.ID)
	if err != nil {
		return nil, err
	}
	snapshot.Relationships = relationshipMoves
	snapshot.RelationshipsDeleted = relationshipDeletes
	report.RelationshipsMoved = len(relationshipMoves)
	report.RelationshipsDeleted = len(relationshipDeletes)

	contractMoves, err := s.mergeCustomerContracts(ctx, tx, tenantID, source.ID, target.ID, branchMap)
	if err != nil {
		return nil, err
	}
	snapshot.Contracts = contractMoves
	report.ContractsMoved = len(contractMoves)

	externalMoves, err := s.mergeCustomerExternalRefs(ctx, tx, tenantID, source.ID, target.ID, branchMap)
	if err != nil {
		return nil, err
	}
	snapshot.ExternalRefs = externalMoves
	report.ExternalRefsMoved = len(externalMoves)

	externalIDMoves, err := s.mergeCustomerExternalIDs(ctx, tx, tenantID, source.ID, target.ID)
	if err != nil {
		return nil, err
	}
	snapshot.ExternalIDs = externalIDMoves
	report.ExternalIDsMoved = len(externalIDMoves)

	conversationMoves, err := s.mergeCustomerConversations(ctx, tx, tenantID, source.ID, target.ID, branchMap)
	if err != nil {
		return nil, err
	}
	snapshot.Conversations = conversationMoves
	report.ConversationsMoved = len(conversationMoves)

	projectLinkMoves, projectLinkDeletes, err := s.mergeCustomerProjectLinks(ctx, tx, tenantID, source.ID, target.ID, branchMap)
	if err != nil {
		return nil, err
	}
	snapshot.ProjectLinks = projectLinkMoves
	snapshot.ProjectLinksDeleted = projectLinkDeletes
	report.ProjectLinksMoved = len(projectLinkMoves)
	report.ProjectLinksDeleted = len(projectLinkDeletes)

	now := time.Now()
	historical := append([]string{}, source.HistoricalNames...)
	historical = appendUniqueStrings(historical, source.Name)
	historical = appendUniqueStrings(historical, source.Aliases...)
	if _, err := tx.ServiceCustomer.UpdateOneID(source.ID).
		Where(servicecustomer.TenantIDEQ(tenantID)).
		SetHistoricalNames(historical).
		SetMergedIntoID(target.ID).
		SetStatus("inactive").
		SetDeletedAt(now).
		AddVersion(1).
		Save(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.ServiceCustomer.UpdateOneID(target.ID).
		Where(servicecustomer.TenantIDEQ(tenantID)).
		AddVersion(1).
		Save(ctx); err != nil {
		return nil, err
	}

	report.DurationMs = time.Since(started).Milliseconds()
	payload := mergeAuditPayload{Kind: "merge", Snapshot: snapshot, Report: report}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := tx.AuditLog.Create().
		SetTenantID(tenantID).
		SetUserID(actorID).
		SetTargetTenantID(tenantID).
		SetResource("service_customer").
		SetAction("customer.merge").
		SetPath(fmt.Sprintf("/api/v1/master-data/customers/%d/merge", source.ID)).
		SetMethod("POST").
		SetStatusCode(200).
		SetRequestBody(string(body)).
		Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.recalculateCustomerQuality(ctx, tenantID, target.ID); err != nil {
		return nil, err
	}
	return &report, nil
}

func getCustomerInTx(ctx context.Context, tx *ent.Tx, tenantID, customerID int) (*ent.ServiceCustomer, error) {
	customer, err := tx.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(customerID), servicecustomer.TenantIDEQ(tenantID), servicecustomer.DeletedAtIsNil()).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerNotFound
	}
	return customer, err
}

func (s *Service) precheckMergeConflicts(ctx context.Context, tx *ent.Tx, tenantID int, source, target *ent.ServiceCustomer) ([]string, error) {
	conflicts := []string{}
	targetContracts, err := tx.SupportContract.Query().
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.CustomerIDEQ(target.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	contractNumbers := map[string]bool{}
	for _, contract := range targetContracts {
		contractNumbers[contract.NormalizedContractNumber] = true
	}
	sourceContracts, err := tx.SupportContract.Query().
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.CustomerIDEQ(source.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, contract := range sourceContracts {
		if contractNumbers[contract.NormalizedContractNumber] {
			conflicts = append(conflicts, fmt.Sprintf("合同号冲突：%s", contract.ContractNumber))
		}
	}

	targetRefs, err := tx.ExternalContractReference.Query().
		Where(externalcontractreference.TenantIDEQ(tenantID), externalcontractreference.CustomerIDEQ(target.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	refKeys := map[string]bool{}
	for _, ref := range targetRefs {
		refKeys[fmt.Sprintf("%d|%s", ref.SourceOrganizationID, ref.NormalizedExternalContractNumber)] = true
	}
	sourceRefs, err := tx.ExternalContractReference.Query().
		Where(externalcontractreference.TenantIDEQ(tenantID), externalcontractreference.CustomerIDEQ(source.ID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ref := range sourceRefs {
		key := fmt.Sprintf("%d|%s", ref.SourceOrganizationID, ref.NormalizedExternalContractNumber)
		if refKeys[key] {
			conflicts = append(conflicts, fmt.Sprintf("外部合同号冲突：%s", ref.ExternalContractNumber))
		}
	}

	relationConflicts, err := s.precheckMergeRelationshipCycles(ctx, tx, tenantID, source.ID, target.ID)
	if err != nil {
		return nil, err
	}
	conflicts = append(conflicts, relationConflicts...)
	return conflicts, nil
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return values
}
