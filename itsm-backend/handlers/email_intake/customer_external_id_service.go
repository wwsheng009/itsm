package email_intake

import (
	"context"
	"errors"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/customerexternalid"
)

var (
	ErrCustomerExternalIDNotFound  = errors.New("customer external id not found")
	ErrCustomerExternalIDInvalid   = errors.New("customer external id invalid")
	ErrCustomerExternalIDDuplicate = errors.New("customer external id duplicate")
)

type customerExternalIDRequest struct {
	SystemCode  string `json:"systemCode" binding:"required"`
	ExternalID  string `json:"externalId" binding:"required"`
	ExternalURL string `json:"externalUrl"`
}

type customerExternalIDResponse struct {
	ID          int       `json:"id"`
	CustomerID  int       `json:"customerId"`
	SystemCode  string    `json:"systemCode"`
	ExternalID  string    `json:"externalId"`
	ExternalURL string    `json:"externalUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

func normalizeSystemCode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateCustomerExternalIDRequest(req *customerExternalIDRequest) error {
	if req == nil {
		return ErrCustomerExternalIDInvalid
	}
	req.SystemCode = normalizeSystemCode(req.SystemCode)
	req.ExternalID = strings.TrimSpace(req.ExternalID)
	req.ExternalURL = strings.TrimSpace(req.ExternalURL)
	if req.SystemCode == "" || len([]rune(req.SystemCode)) > 64 {
		return ErrCustomerExternalIDInvalid
	}
	if req.ExternalID == "" || len([]rune(req.ExternalID)) > 160 {
		return ErrCustomerExternalIDInvalid
	}
	if len([]rune(req.ExternalURL)) > 500 {
		return ErrCustomerExternalIDInvalid
	}
	return nil
}

func (s *Service) ListCustomerExternalIDs(ctx context.Context, tenantID, customerID int) ([]*ent.CustomerExternalID, error) {
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	return s.client.CustomerExternalID.Query().
		Where(
			customerexternalid.TenantIDEQ(tenantID),
			customerexternalid.CustomerIDEQ(customerID),
		).
		Order(ent.Asc(customerexternalid.FieldSystemCode), ent.Asc(customerexternalid.FieldExternalID)).
		All(ctx)
}

func (s *Service) CreateCustomerExternalID(ctx context.Context, tenantID, customerID int, req *customerExternalIDRequest) (*ent.CustomerExternalID, error) {
	if err := validateCustomerExternalIDRequest(req); err != nil {
		return nil, err
	}
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	entity, err := s.client.CustomerExternalID.Create().
		SetTenantID(tenantID).
		SetCustomerID(customerID).
		SetSystemCode(req.SystemCode).
		SetExternalID(req.ExternalID).
		SetExternalURL(req.ExternalURL).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerExternalIDDuplicate
		}
		return nil, err
	}
	return entity, nil
}

func (s *Service) UpdateCustomerExternalID(ctx context.Context, tenantID, customerID, externalID int, req *customerExternalIDRequest) (*ent.CustomerExternalID, error) {
	if err := validateCustomerExternalIDRequest(req); err != nil {
		return nil, err
	}
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	affected, err := s.client.CustomerExternalID.Update().
		Where(
			customerexternalid.IDEQ(externalID),
			customerexternalid.TenantIDEQ(tenantID),
			customerexternalid.CustomerIDEQ(customerID),
		).
		SetSystemCode(req.SystemCode).
		SetExternalID(req.ExternalID).
		SetExternalURL(req.ExternalURL).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerExternalIDDuplicate
		}
		return nil, err
	}
	if affected == 0 {
		return nil, ErrCustomerExternalIDNotFound
	}
	entity, err := s.client.CustomerExternalID.Query().
		Where(customerexternalid.IDEQ(externalID), customerexternalid.TenantIDEQ(tenantID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerExternalIDNotFound
	}
	return entity, err
}

func (s *Service) DeleteCustomerExternalID(ctx context.Context, tenantID, customerID, externalID int) error {
	affected, err := s.client.CustomerExternalID.Delete().
		Where(
			customerexternalid.IDEQ(externalID),
			customerexternalid.TenantIDEQ(tenantID),
			customerexternalid.CustomerIDEQ(customerID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCustomerExternalIDNotFound
	}
	return nil
}

func mapCustomerExternalID(v *ent.CustomerExternalID) customerExternalIDResponse {
	return customerExternalIDResponse{
		ID:          v.ID,
		CustomerID:  v.CustomerID,
		SystemCode:  v.SystemCode,
		ExternalID:  v.ExternalID,
		ExternalURL: v.ExternalURL,
		CreatedAt:   v.CreatedAt,
	}
}

func mapCustomerExternalIDs(items []*ent.CustomerExternalID) []customerExternalIDResponse {
	result := make([]customerExternalIDResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapCustomerExternalID(item))
	}
	return result
}
