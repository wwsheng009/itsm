package email_intake

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/ent/customercontact"
	"itsm-backend/ent/customertenantlink"
	"itsm-backend/ent/projectcustomerlink"
	"itsm-backend/ent/servicecustomer"
)

// recalculateCustomerQuality applies the P1 quality score formula from
// docs/customer-master-data/01 §9.3. The score is derived, never user-edited.
func (s *Service) recalculateCustomerQuality(ctx context.Context, tenantID, customerID int) error {
	customer, err := s.client.ServiceCustomer.Query().
		Where(
			servicecustomer.IDEQ(customerID),
			servicecustomer.TenantIDEQ(tenantID),
			servicecustomer.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	score := 0
	// 标识完整 25
	if customer.Code != "" {
		score += 5
	}
	if customer.LegalName != "" {
		score += 5
	}
	if customer.TaxID != "" {
		score += 10
	}
	if len(customer.EmailDomains) > 0 {
		score += 5
	}
	// 分类完整 15
	if customer.Industry != "" {
		score += 8
	}
	if customer.SizeClass != "" {
		score += 4
	}
	if customer.OwnershipType != "" {
		score += 3
	}
	// 分级完整 10
	if customer.Tier != "" && customer.Tier != "C" {
		score += 6
	}
	if customer.VipLevel > 0 {
		score += 4
	}
	// 服务配置 20
	if customer.SLADefinitionID != nil {
		score += 8
	}
	if customer.AccountManagerID != nil {
		score += 8
	}
	if customer.SupportHours != "" {
		score += 4
	}
	// 联系可达 20
	hasPrimaryContact, err := s.client.CustomerContact.Query().
		Where(
			customercontact.TenantIDEQ(tenantID),
			customercontact.CustomerIDEQ(customerID),
			customercontact.StatusEQ("active"),
			customercontact.DeletedAtIsNil(),
			customercontact.IsPrimaryEQ(true),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if hasPrimaryContact {
		score += 8
	}
	if customer.MainPhone != "" {
		score += 4
	}
	if customer.ServiceEmail != "" {
		score += 4
	}
	if hasNonEmptyMap(customer.Address) {
		score += 4
	}
	// 关系完整 10
	tenantLinkCount, err := s.client.CustomerTenantLink.Query().
		Where(
			customertenantlink.TenantIDEQ(tenantID),
			customertenantlink.CustomerIDEQ(customerID),
			customertenantlink.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if tenantLinkCount > 0 {
		score += 6
	}
	projectLinkCount, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.CustomerIDEQ(customerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if projectLinkCount > 0 {
		score += 4
	}
	if score > 100 {
		score = 100
	}
	_, err = s.client.ServiceCustomer.UpdateOneID(customerID).
		Where(servicecustomer.TenantIDEQ(tenantID)).
		SetDataQualityScore(int16(score)).
		Save(ctx)
	return err
}

func hasNonEmptyMap(value map[string]interface{}) bool {
	for _, item := range value {
		if text, ok := item.(string); ok {
			if text != "" {
				return true
			}
			continue
		}
		if item != nil {
			return true
		}
	}
	return false
}

// RecalculateAllCustomerQuality is used by the daily/manual backfill command.
func (s *Service) RecalculateAllCustomerQuality(ctx context.Context) (int, error) {
	customers, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, customer := range customers {
		if err := s.recalculateCustomerQuality(ctx, customer.TenantID, customer.ID); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
