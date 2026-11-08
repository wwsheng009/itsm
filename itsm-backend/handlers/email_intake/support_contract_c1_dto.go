package email_intake

import (
	"time"

	"itsm-backend/ent"
)

type contractServiceItemResponse struct {
	ID                  int       `json:"id"`
	ContractID          int       `json:"contractId"`
	CatalogItemID       *int      `json:"catalogItemId,omitempty"`
	ItemCode            string    `json:"itemCode"`
	ItemName            string    `json:"itemName"`
	Spec                string    `json:"spec"`
	BillingUnit         string    `json:"billingUnit"`
	Quantity            float64   `json:"quantity"`
	PeriodCount         float64   `json:"periodCount"`
	UnitPrice           float64   `json:"unitPrice"`
	Amount              float64   `json:"amount"`
	QuotaTotal          *float64  `json:"quotaTotal,omitempty"`
	QuotaUsed           float64   `json:"quotaUsed"`
	QuotaReserved       float64   `json:"quotaReserved"`
	AvailableToAllocate *float64  `json:"availableToAllocate,omitempty"`
	Note                string    `json:"note"`
	SLADefinitionID     *int      `json:"slaDefinitionId,omitempty"`
	ResponseTargetMin   *int      `json:"responseTargetMin,omitempty"`
	ResolveTargetMin    *int      `json:"resolveTargetMin,omitempty"`
	SortOrder           int       `json:"sortOrder"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type contractAmendmentResponse struct {
	ID            int                    `json:"id"`
	ContractID    int                    `json:"contractId"`
	AmendmentNo   string                 `json:"amendmentNo"`
	AmendType     string                 `json:"amendType"`
	EffectiveFrom time.Time              `json:"effectiveFrom"`
	AmountDelta   float64                `json:"amountDelta"`
	TermDeltaDays *int                   `json:"termDeltaDays,omitempty"`
	Diff          map[string]interface{} `json:"diff"`
	Reason        string                 `json:"reason"`
	Status        string                 `json:"status"`
	SignedAt      *time.Time             `json:"signedAt,omitempty"`
	CreatedAt     time.Time              `json:"createdAt"`
}

type contractUsageEntryResponse struct {
	ID            int       `json:"id"`
	ContractID    int       `json:"contractId"`
	ServiceItemID int       `json:"serviceItemId"`
	SourceType    string    `json:"sourceType"`
	SourceRef     string    `json:"sourceRef"`
	QuantityDelta float64   `json:"quantityDelta"`
	AmountDelta   float64   `json:"amountDelta"`
	OccurredAt    time.Time `json:"occurredAt"`
	Note          string    `json:"note"`
	CreatedAt     time.Time `json:"createdAt"`
}

type contractProjectLinkResponse struct {
	ID              int        `json:"id"`
	ContractID      int        `json:"contractId"`
	ContractNumber  string     `json:"contractNumber"`
	ContractName    string     `json:"contractName"`
	ProjectID       int        `json:"projectId"`
	ProjectCode     string     `json:"projectCode"`
	ProjectName     string     `json:"projectName"`
	RelationType    string     `json:"relationType"`
	ServiceItemID   *int       `json:"serviceItemId,omitempty"`
	ServiceItemName string     `json:"serviceItemName"`
	AllocatedAmount *float64   `json:"allocatedAmount,omitempty"`
	AllocatedHours  *float64   `json:"allocatedHours,omitempty"`
	AllocationPct   *int16     `json:"allocationPct,omitempty"`
	IsPrimary       bool       `json:"isPrimary"`
	Note            string     `json:"note"`
	EffectiveFrom   *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo     *time.Time `json:"effectiveTo,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type contractBalanceResponse struct {
	ContractID      int                           `json:"contractId"`
	Amount          *float64                      `json:"amount,omitempty"`
	EffectiveAmount *float64                      `json:"effectiveAmount,omitempty"`
	AllocatedAmount float64                       `json:"allocatedAmount"`
	UsedAmount      float64                       `json:"usedAmount"`
	RemainingAmount *float64                      `json:"remainingAmount,omitempty"`
	AvailableAmount *float64                      `json:"availableAmount,omitempty"`
	ServiceItems    []contractServiceItemResponse `json:"serviceItems"`
}

type contractProjectLinkRow struct {
	Link        *ent.ContractProjectLink
	Project     *ent.Project
	ServiceItem *ent.ContractServiceItem
	Contract    *ent.SupportContract
}

func mapContractServiceItem(v *ent.ContractServiceItem, reserved float64) contractServiceItemResponse {
	response := contractServiceItemResponse{
		ID: v.ID, ContractID: v.ContractID, CatalogItemID: v.CatalogItemID,
		ItemCode: v.ItemCode, ItemName: v.ItemName, Spec: v.Spec, BillingUnit: v.BillingUnit,
		Quantity: v.Quantity, PeriodCount: v.PeriodCount, UnitPrice: v.UnitPrice, Amount: v.Amount,
		QuotaTotal: v.QuotaTotal, QuotaUsed: v.QuotaUsed, QuotaReserved: reserved,
		Note: v.Note, SLADefinitionID: v.SLADefinitionID,
		ResponseTargetMin: v.ResponseTargetMin, ResolveTargetMin: v.ResolveTargetMin,
		SortOrder: v.SortOrder, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
	if v.QuotaTotal != nil {
		available := *v.QuotaTotal - v.QuotaUsed - reserved
		response.AvailableToAllocate = &available
	}
	return response
}

func mapContractAmendment(v *ent.ContractAmendment) contractAmendmentResponse {
	return contractAmendmentResponse{
		ID: v.ID, ContractID: v.ContractID, AmendmentNo: v.AmendmentNo,
		AmendType: v.AmendType, EffectiveFrom: v.EffectiveFrom,
		AmountDelta: v.AmountDelta, TermDeltaDays: v.TermDeltaDays,
		Diff: v.Diff, Reason: v.Reason, Status: v.Status, SignedAt: v.SignedAt,
		CreatedAt: v.CreatedAt,
	}
}

func mapContractUsageEntry(v *ent.ContractUsageLedger) contractUsageEntryResponse {
	return contractUsageEntryResponse{
		ID: v.ID, ContractID: v.ContractID, ServiceItemID: v.ServiceItemID,
		SourceType: v.SourceType, SourceRef: v.SourceRef,
		QuantityDelta: v.QuantityDelta, AmountDelta: v.AmountDelta,
		OccurredAt: v.OccurredAt, Note: v.Note, CreatedAt: v.CreatedAt,
	}
}

func mapContractProjectLink(row *contractProjectLinkRow) contractProjectLinkResponse {
	link := row.Link
	response := contractProjectLinkResponse{
		ID: link.ID, ContractID: link.ContractID, ProjectID: link.ProjectID,
		RelationType: link.RelationType, ServiceItemID: link.ServiceItemID,
		AllocatedAmount: link.AllocatedAmount, AllocatedHours: link.AllocatedHours,
		AllocationPct: link.AllocationPct, IsPrimary: link.IsPrimary, Note: link.Note,
		EffectiveFrom: link.EffectiveFrom, EffectiveTo: link.EffectiveTo,
		Status: link.Status, CreatedAt: link.CreatedAt,
	}
	if row.Project != nil {
		response.ProjectCode = row.Project.Code
		response.ProjectName = row.Project.Name
	}
	if row.ServiceItem != nil {
		response.ServiceItemName = row.ServiceItem.ItemName
	}
	if row.Contract != nil {
		response.ContractNumber = row.Contract.ContractNumber
		response.ContractName = row.Contract.ContractName
	}
	return response
}
