package email_intake

import (
	"context"
	"encoding/json"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/servicecustomer"
)

type customerMergeHistoryItem struct {
	SourceID      int       `json:"sourceId"`
	SourceCode    string    `json:"sourceCode"`
	SourceName    string    `json:"sourceName"`
	TargetID      int       `json:"targetId"`
	MergedAt      time.Time `json:"mergedAt"`
	UndoExpiresAt time.Time `json:"undoExpiresAt"`
	CanUndo       bool      `json:"canUndo"`
}

// ListCustomerMergeHistory returns the recent (7-day) merges whose target is the
// given customer. The UI uses CanUndo to expose the undo action only while the
// source row still points at this target.
func (s *Service) ListCustomerMergeHistory(ctx context.Context, tenantID, targetID int) ([]*customerMergeHistoryItem, error) {
	exists, err := s.CustomerExists(ctx, tenantID, targetID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}

	logs, err := s.client.AuditLog.Query().
		Where(
			auditlog.TenantIDEQ(tenantID),
			auditlog.ResourceEQ("service_customer"),
			auditlog.ActionEQ("customer.merge"),
			auditlog.CreatedAtGTE(time.Now().Add(-7*24*time.Hour)),
		).
		Order(ent.Desc(auditlog.FieldCreatedAt)).
		Limit(200).
		All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*customerMergeHistoryItem, 0)
	seen := map[int]bool{}
	sourceIDs := make([]int, 0)
	for _, log := range logs {
		if log.RequestBody == nil {
			continue
		}
		var payload mergeAuditPayload
		if err := json.Unmarshal([]byte(*log.RequestBody), &payload); err != nil {
			continue
		}
		if payload.Kind != "merge" || payload.Snapshot.TargetID != targetID {
			continue
		}
		sourceID := payload.Snapshot.SourceID
		if sourceID < 1 || seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		sourceIDs = append(sourceIDs, sourceID)
		items = append(items, &customerMergeHistoryItem{
			SourceID:      sourceID,
			TargetID:      targetID,
			MergedAt:      log.CreatedAt,
			UndoExpiresAt: log.CreatedAt.Add(7 * 24 * time.Hour),
		})
	}
	if len(items) == 0 {
		return items, nil
	}

	sources, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.TenantIDEQ(tenantID), servicecustomer.IDIn(sourceIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int]*ent.ServiceCustomer{}
	for _, source := range sources {
		byID[source.ID] = source
	}
	for _, item := range items {
		source := byID[item.SourceID]
		if source == nil {
			continue
		}
		item.SourceCode = source.Code
		item.SourceName = source.Name
		item.CanUndo = source.MergedIntoID != nil && *source.MergedIntoID == targetID
	}
	return items, nil
}
