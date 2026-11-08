package email_intake

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/customerbranch"
	"itsm-backend/ent/servicecustomer"
)

func (s *Service) UndoCustomerMerge(ctx context.Context, tenantID, actorID, sourceID int) (*mergeReport, error) {
	payload, err := s.findMergeAuditPayload(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	source, err := tx.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(sourceID), servicecustomer.TenantIDEQ(tenantID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerMergeAlreadyUndone
	}
	if err != nil {
		return nil, err
	}
	if source.MergedIntoID == nil || *source.MergedIntoID != payload.Snapshot.TargetID {
		return nil, ErrCustomerMergeAlreadyUndone
	}
	if _, err := tx.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(payload.Snapshot.TargetID), servicecustomer.TenantIDEQ(tenantID)).
		Only(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCustomerMergeInvalid
		}
		return nil, err
	}

	snapshot := payload.Snapshot
	for _, move := range snapshot.ProjectLinks {
		update := tx.ProjectCustomerLink.UpdateOneID(move.ID).
			SetCustomerID(sourceID).
			SetIsPrimary(move.OldIsPrimary)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.ProjectLinksDeleted {
		if _, err := tx.ProjectCustomerLink.UpdateOneID(move.ID).
			SetCustomerID(sourceID).
			ClearDeletedAt().
			SetIsPrimary(move.OldIsPrimary).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.Contracts {
		update := tx.SupportContract.UpdateOneID(move.ID).SetCustomerID(sourceID)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.ExternalRefs {
		update := tx.ExternalContractReference.UpdateOneID(move.ID).SetCustomerID(sourceID)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, externalID := range snapshot.ExternalIDs {
		if _, err := tx.CustomerExternalID.UpdateOneID(externalID).
			SetCustomerID(sourceID).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.Conversations {
		update := tx.EmailConversation.UpdateOneID(move.ID).SetCustomerID(sourceID)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.Contacts {
		update := tx.CustomerContact.UpdateOneID(move.ID).
			SetCustomerID(sourceID).
			SetIsPrimary(move.OldIsPrimary)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.ContactsDeleted {
		update := tx.CustomerContact.UpdateOneID(move.ID).
			SetCustomerID(sourceID).
			ClearDeletedAt().
			SetStatus(move.OldStatus).
			SetIsPrimary(move.OldIsPrimary)
		if move.OldBranchID != nil {
			update.SetBranchID(*move.OldBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.TenantLinks {
		if _, err := tx.CustomerTenantLink.UpdateOneID(move.ID).
			SetCustomerID(sourceID).
			SetLinkType(move.OldLinkType).
			SetIsBillingAnchor(move.OldBilling).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, id := range snapshot.TenantLinksDeleted {
		if _, err := tx.CustomerTenantLink.UpdateOneID(id).
			SetCustomerID(sourceID).
			ClearDeletedAt().
			Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.Relationships {
		if _, err := tx.CustomerRelationship.UpdateOneID(move.ID).
			SetFromCustomerID(move.OldFromID).
			SetToCustomerID(move.OldToID).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	for _, move := range snapshot.RelationshipsDeleted {
		if _, err := tx.CustomerRelationship.UpdateOneID(move.ID).
			ClearDeletedAt().
			SetStatus(move.OldStatus).
			Save(ctx); err != nil {
			return nil, err
		}
	}

	newBranchIDs := make([]int, 0, len(snapshot.Branches))
	for _, move := range snapshot.Branches {
		newBranchIDs = append(newBranchIDs, move.NewID)
	}
	if len(newBranchIDs) > 0 {
		if _, err := tx.CustomerBranch.Delete().
			Where(customerbranch.IDIn(newBranchIDs...)).
			Exec(ctx); err != nil {
			return nil, err
		}
	}

	sourceUpdate := tx.ServiceCustomer.UpdateOneID(sourceID).
		Where(servicecustomer.TenantIDEQ(tenantID)).
		SetStatus(snapshot.SourceStatus).
		SetHistoricalNames(snapshot.SourceHistoricalNames).
		ClearMergedIntoID().
		ClearDeletedAt().
		AddVersion(1)
	if snapshot.SourceLinkedTenantID != nil {
		sourceUpdate.SetLinkedCustomerTenantID(*snapshot.SourceLinkedTenantID)
	} else {
		sourceUpdate.ClearLinkedCustomerTenantID()
	}
	if _, err := sourceUpdate.Save(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.ServiceCustomer.UpdateOneID(snapshot.TargetID).
		Where(servicecustomer.TenantIDEQ(tenantID)).
		AddVersion(1).
		Save(ctx); err != nil {
		return nil, err
	}

	payload.Kind = "undo"
	payload.Report.DurationMs = 0
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := tx.AuditLog.Create().
		SetTenantID(tenantID).
		SetUserID(actorID).
		SetTargetTenantID(tenantID).
		SetResource("service_customer").
		SetAction("customer.merge.undo").
		SetPath(fmt.Sprintf("/api/v1/master-data/customers/%d/merge/undo", sourceID)).
		SetMethod("POST").
		SetStatusCode(200).
		SetRequestBody(string(body)).
		Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = s.recalculateCustomerQuality(ctx, tenantID, sourceID)
	_ = s.recalculateCustomerQuality(ctx, tenantID, snapshot.TargetID)
	return &payload.Report, nil
}

func (s *Service) findMergeAuditPayload(ctx context.Context, tenantID, sourceID int) (*mergeAuditPayload, error) {
	logs, err := s.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(tenantID),
			auditlog.ResourceEQ("service_customer"),
			auditlog.ActionIn("customer.merge", "customer.merge.undo"),
			auditlog.CreatedAtGTE(time.Now().Add(-7*24*time.Hour)),
		).
		Order(ent.Desc(auditlog.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, log := range logs {
		if log.RequestBody == nil {
			continue
		}
		var payload mergeAuditPayload
		if err := json.Unmarshal([]byte(*log.RequestBody), &payload); err != nil {
			continue
		}
		if payload.Snapshot.SourceID != sourceID {
			continue
		}
		if payload.Kind == "undo" {
			return nil, ErrCustomerMergeAlreadyUndone
		}
		if payload.Kind == "merge" {
			return &payload, nil
		}
	}
	return nil, ErrCustomerMergeUndoExpired
}
