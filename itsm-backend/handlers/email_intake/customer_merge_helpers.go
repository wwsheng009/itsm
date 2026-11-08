package email_intake

import (
	"context"
	"fmt"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/customerbranch"
	"itsm-backend/ent/customercontact"
	"itsm-backend/ent/customerexternalid"
	"itsm-backend/ent/customerrelationship"
	"itsm-backend/ent/customertenantlink"
	"itsm-backend/ent/emailconversation"
	"itsm-backend/ent/externalcontractreference"
	"itsm-backend/ent/projectcustomerlink"
	"itsm-backend/ent/supportcontract"
)

func (s *Service) mergeCustomerBranches(ctx context.Context, tx *ent.Tx, tenantID int, source, target *ent.ServiceCustomer) (map[int]int, []mergeBranchMove, error) {
	sourceBranches, err := tx.CustomerBranch.Query().
		Where(customerbranch.TenantIDEQ(tenantID), customerbranch.CustomerIDEQ(source.ID), customerbranch.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetBranches, err := tx.CustomerBranch.Query().
		Where(customerbranch.TenantIDEQ(tenantID), customerbranch.CustomerIDEQ(target.ID), customerbranch.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetNames := map[string]bool{}
	targetCodes := map[string]bool{}
	targetPrimary := false
	for _, branch := range targetBranches {
		targetNames[branch.NormalizedName] = true
		if branch.Code != "" {
			targetCodes[branch.Code] = true
		}
		if branch.IsPrimary && branch.Status == "active" {
			targetPrimary = true
		}
	}

	branchMap := map[int]int{}
	moves := make([]mergeBranchMove, 0, len(sourceBranches))
	for _, branch := range sourceBranches {
		name := branch.Name
		normalized := branch.NormalizedName
		if targetNames[normalized] {
			name = branch.Name + "（来自 " + source.Name + "）"
			normalized = NormalizeName(name)
			name, normalized = s.uniqueBranchName(ctx, tx, tenantID, target.ID, name, normalized, targetNames)
		}
		code := branch.Code
		if code != "" {
			code = fmt.Sprintf("%s-M%d", code, branch.ID)
			if len(code) > 64 {
				code = ""
			}
			for index := 2; code != "" && targetCodes[code]; index++ {
				candidate := fmt.Sprintf("M-%d-%d", branch.ID, index)
				if len(candidate) > 64 {
					code = ""
					break
				}
				code = candidate
			}
		}
		isPrimary := branch.IsPrimary && !targetPrimary
		created, err := tx.CustomerBranch.Create().
			SetTenantID(tenantID).
			SetCustomerID(target.ID).
			SetCode(code).
			SetName(name).
			SetNormalizedName(normalized).
			SetBranchType(branch.BranchType).
			SetAliases(nonNilStrings(branch.Aliases)).
			SetAddress(nonNilMap(branch.Address)).
			SetTimezone(branch.Timezone).
			SetContactPhone(branch.ContactPhone).
			SetEmailDomains(nonNilStrings(branch.EmailDomains)).
			SetServiceTier(branch.ServiceTier).
			SetSupportHours(branch.SupportHours).
			SetIsPrimary(isPrimary).
			SetNillableLinkedTenantID(branch.LinkedTenantID).
			SetStatus(branch.Status).
			Save(ctx)
		if err != nil {
			return nil, nil, err
		}
		targetNames[normalized] = true
		if code != "" {
			targetCodes[code] = true
		}
		if isPrimary {
			targetPrimary = true
		}
		branchMap[branch.ID] = created.ID
		moves = append(moves, mergeBranchMove{OldID: branch.ID, NewID: created.ID})
	}
	return branchMap, moves, nil
}

func (s *Service) uniqueBranchName(ctx context.Context, tx *ent.Tx, tenantID, customerID int, name, normalized string, existing map[string]bool) (string, string) {
	if !existing[normalized] {
		return name, normalized
	}
	base := name
	for index := 2; index < 100; index++ {
		candidate := fmt.Sprintf("%s %d", base, index)
		candidateNormalized := NormalizeName(candidate)
		if !existing[candidateNormalized] {
			exists, err := tx.CustomerBranch.Query().
				Where(
					customerbranch.TenantIDEQ(tenantID),
					customerbranch.CustomerIDEQ(customerID),
					customerbranch.NormalizedNameEQ(candidateNormalized),
					customerbranch.DeletedAtIsNil(),
				).
				Exist(ctx)
			if err == nil && !exists {
				return candidate, candidateNormalized
			}
		}
	}
	return base, normalized
}

func mappedBranchID(old *int, mapping map[int]int) *int {
	if old == nil {
		return nil
	}
	if value, ok := mapping[*old]; ok {
		return &value
	}
	return nil
}

func (s *Service) mergeCustomerContacts(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int, branchMap map[int]int) (int, int, []mergeContactMove, []mergeContactDelete, error) {
	sourceContacts, err := tx.CustomerContact.Query().
		Where(customercontact.TenantIDEQ(tenantID), customercontact.CustomerIDEQ(sourceID), customercontact.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	targetContacts, err := tx.CustomerContact.Query().
		Where(customercontact.TenantIDEQ(tenantID), customercontact.CustomerIDEQ(targetID), customercontact.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	targetEmails := map[string]bool{}
	targetPrimaryRoles := map[string]bool{}
	for _, contact := range targetContacts {
		if contact.Status == "active" && contact.NormalizedEmail != "" {
			targetEmails[contact.NormalizedEmail] = true
		}
		if contact.Status == "active" && contact.IsPrimary {
			targetPrimaryRoles[contact.ContactRole] = true
		}
	}

	moves := []mergeContactMove{}
	deletes := []mergeContactDelete{}
	now := time.Now()
	for _, contact := range sourceContacts {
		newBranchID := mappedBranchID(contact.BranchID, branchMap)
		if contact.NormalizedEmail != "" && targetEmails[contact.NormalizedEmail] {
			if _, err := tx.CustomerContact.UpdateOneID(contact.ID).
				SetDeletedAt(now).
				SetStatus("inactive").
				SetIsPrimary(false).
				Save(ctx); err != nil {
				return 0, 0, nil, nil, err
			}
			deletes = append(deletes, mergeContactDelete{
				ID: contact.ID, OldBranchID: contact.BranchID, NewBranchID: newBranchID,
				OldIsPrimary: contact.IsPrimary, OldStatus: contact.Status,
			})
			continue
		}
		newPrimary := contact.IsPrimary
		if newPrimary && targetPrimaryRoles[contact.ContactRole] {
			newPrimary = false
		}
		update := tx.CustomerContact.UpdateOneID(contact.ID).
			SetCustomerID(targetID).
			SetIsPrimary(newPrimary)
		if newBranchID != nil {
			update.SetBranchID(*newBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return 0, 0, nil, nil, err
		}
		if contact.NormalizedEmail != "" {
			targetEmails[contact.NormalizedEmail] = true
		}
		if newPrimary {
			targetPrimaryRoles[contact.ContactRole] = true
		}
		moves = append(moves, mergeContactMove{
			ID: contact.ID, OldBranchID: contact.BranchID, NewBranchID: newBranchID,
			OldIsPrimary: contact.IsPrimary,
		})
	}
	return len(moves), len(deletes), moves, deletes, nil
}

func (s *Service) mergeCustomerTenantLinks(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int) ([]mergeTenantLinkMove, []int, error) {
	sourceLinks, err := tx.CustomerTenantLink.Query().
		Where(customertenantlink.TenantIDEQ(tenantID), customertenantlink.CustomerIDEQ(sourceID), customertenantlink.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetLinks, err := tx.CustomerTenantLink.Query().
		Where(customertenantlink.TenantIDEQ(tenantID), customertenantlink.CustomerIDEQ(targetID), customertenantlink.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetLinked := map[int]bool{}
	targetPrimary := false
	targetBilling := false
	for _, link := range targetLinks {
		targetLinked[link.LinkedTenantID] = true
		if link.LinkType == "primary" {
			targetPrimary = true
		}
		if link.IsBillingAnchor {
			targetBilling = true
		}
	}
	moves := []mergeTenantLinkMove{}
	deletes := []int{}
	now := time.Now()
	for _, link := range sourceLinks {
		if targetLinked[link.LinkedTenantID] {
			if _, err := tx.CustomerTenantLink.UpdateOneID(link.ID).SetDeletedAt(now).Save(ctx); err != nil {
				return nil, nil, err
			}
			deletes = append(deletes, link.ID)
			continue
		}
		newLinkType := link.LinkType
		newBilling := link.IsBillingAnchor
		if newLinkType == "primary" && targetPrimary {
			newLinkType = "secondary"
		}
		if newBilling && targetBilling {
			newBilling = false
		}
		if _, err := tx.CustomerTenantLink.UpdateOneID(link.ID).
			SetCustomerID(targetID).
			SetLinkType(newLinkType).
			SetIsBillingAnchor(newBilling).
			Save(ctx); err != nil {
			return nil, nil, err
		}
		targetLinked[link.LinkedTenantID] = true
		if newLinkType == "primary" {
			targetPrimary = true
		}
		if newBilling {
			targetBilling = true
		}
		moves = append(moves, mergeTenantLinkMove{
			ID: link.ID, OldLinkType: link.LinkType, NewLinkType: newLinkType,
			OldBilling: link.IsBillingAnchor, NewBilling: newBilling,
		})
	}
	return moves, deletes, nil
}

func (s *Service) mergeCustomerRelationships(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int) ([]mergeRelationshipMove, []mergeRelationshipDelete, error) {
	sourceRelations, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
			customerrelationship.Or(
				customerrelationship.FromCustomerIDEQ(sourceID),
				customerrelationship.ToCustomerIDEQ(sourceID),
			),
		).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetRelations, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetKeys := map[string]bool{}
	for _, relation := range targetRelations {
		if relation.FromCustomerID == sourceID || relation.ToCustomerID == sourceID {
			continue
		}
		targetKeys[fmt.Sprintf("%d|%d|%s", relation.FromCustomerID, relation.ToCustomerID, relation.RelationType)] = true
	}
	moves := []mergeRelationshipMove{}
	deletes := []mergeRelationshipDelete{}
	now := time.Now()
	for _, relation := range sourceRelations {
		newFrom := relation.FromCustomerID
		newTo := relation.ToCustomerID
		if newFrom == sourceID {
			newFrom = targetID
		}
		if newTo == sourceID {
			newTo = targetID
		}
		key := fmt.Sprintf("%d|%d|%s", newFrom, newTo, relation.RelationType)
		if newFrom == newTo || targetKeys[key] {
			if _, err := tx.CustomerRelationship.UpdateOneID(relation.ID).
				SetStatus("ended").
				SetDeletedAt(now).
				Save(ctx); err != nil {
				return nil, nil, err
			}
			deletes = append(deletes, mergeRelationshipDelete{ID: relation.ID, OldStatus: relation.Status})
			continue
		}
		if _, err := tx.CustomerRelationship.UpdateOneID(relation.ID).
			SetFromCustomerID(newFrom).
			SetToCustomerID(newTo).
			Save(ctx); err != nil {
			return nil, nil, err
		}
		targetKeys[key] = true
		moves = append(moves, mergeRelationshipMove{ID: relation.ID, OldFromID: relation.FromCustomerID, OldToID: relation.ToCustomerID})
	}
	return moves, deletes, nil
}

func (s *Service) precheckMergeRelationshipCycles(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int) ([]string, error) {
	relations, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.RelationTypeEQ("parent"),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	edges := map[int][]int{}
	for _, relation := range relations {
		if relation.FromCustomerID == sourceID || relation.ToCustomerID == sourceID {
			continue
		}
		edges[relation.FromCustomerID] = append(edges[relation.FromCustomerID], relation.ToCustomerID)
	}
	sourceRelations, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
			customerrelationship.Or(
				customerrelationship.FromCustomerIDEQ(sourceID),
				customerrelationship.ToCustomerIDEQ(sourceID),
			),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, relation := range sourceRelations {
		from := relation.FromCustomerID
		to := relation.ToCustomerID
		if relation.RelationType == "subsidiary" {
			from, to = to, from
		} else if relation.RelationType != "parent" {
			continue
		}
		if from == sourceID {
			from = targetID
		}
		if to == sourceID {
			to = targetID
		}
		if from != to {
			edges[from] = append(edges[from], to)
		}
	}
	if hasCycle(edges) {
		return []string{"客户关系迁移会形成环"}, nil
	}
	return nil, nil
}

func hasCycle(edges map[int][]int) bool {
	const (
		visiting = 1
		visited  = 2
	)
	state := map[int]int{}
	var visit func(int) bool
	visit = func(node int) bool {
		switch state[node] {
		case visiting:
			return true
		case visited:
			return false
		}
		state[node] = visiting
		for _, next := range edges[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = visited
		return false
	}
	for node := range edges {
		if visit(node) {
			return true
		}
	}
	return false
}

func (s *Service) mergeCustomerContracts(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int, branchMap map[int]int) ([]mergeChildMove, error) {
	contracts, err := tx.SupportContract.Query().
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.CustomerIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	moves := make([]mergeChildMove, 0, len(contracts))
	for _, contract := range contracts {
		newBranchID := mappedBranchID(contract.BranchID, branchMap)
		update := tx.SupportContract.UpdateOneID(contract.ID).SetCustomerID(targetID)
		if newBranchID != nil {
			update.SetBranchID(*newBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
		moves = append(moves, mergeChildMove{ID: contract.ID, OldBranchID: contract.BranchID, NewBranchID: newBranchID})
	}
	return moves, nil
}

func (s *Service) mergeCustomerExternalRefs(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int, branchMap map[int]int) ([]mergeChildMove, error) {
	refs, err := tx.ExternalContractReference.Query().
		Where(externalcontractreference.TenantIDEQ(tenantID), externalcontractreference.CustomerIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	moves := make([]mergeChildMove, 0, len(refs))
	for _, ref := range refs {
		newBranchID := mappedBranchID(ref.BranchID, branchMap)
		update := tx.ExternalContractReference.UpdateOneID(ref.ID).SetCustomerID(targetID)
		if newBranchID != nil {
			update.SetBranchID(*newBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
		moves = append(moves, mergeChildMove{ID: ref.ID, OldBranchID: ref.BranchID, NewBranchID: newBranchID})
	}
	return moves, nil
}

func (s *Service) mergeCustomerExternalIDs(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int) ([]int, error) {
	rows, err := tx.CustomerExternalID.Query().
		Where(customerexternalid.TenantIDEQ(tenantID), customerexternalid.CustomerIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		if _, err := tx.CustomerExternalID.UpdateOneID(row.ID).
			SetCustomerID(targetID).
			Save(ctx); err != nil {
			return nil, err
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (s *Service) mergeCustomerConversations(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int, branchMap map[int]int) ([]mergeChildMove, error) {
	conversations, err := tx.EmailConversation.Query().
		Where(emailconversation.TenantIDEQ(tenantID), emailconversation.CustomerIDEQ(sourceID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	moves := make([]mergeChildMove, 0, len(conversations))
	for _, conversation := range conversations {
		newBranchID := mappedBranchID(conversation.BranchID, branchMap)
		update := tx.EmailConversation.UpdateOneID(conversation.ID).SetCustomerID(targetID)
		if newBranchID != nil {
			update.SetBranchID(*newBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, err
		}
		moves = append(moves, mergeChildMove{ID: conversation.ID, OldBranchID: conversation.BranchID, NewBranchID: newBranchID})
	}
	return moves, nil
}

func (s *Service) mergeCustomerProjectLinks(ctx context.Context, tx *ent.Tx, tenantID, sourceID, targetID int, branchMap map[int]int) ([]mergeProjectLinkMove, []mergeProjectLinkDelete, error) {
	sourceLinks, err := tx.ProjectCustomerLink.Query().
		Where(projectcustomerlink.TenantIDEQ(tenantID), projectcustomerlink.CustomerIDEQ(sourceID), projectcustomerlink.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetLinks, err := tx.ProjectCustomerLink.Query().
		Where(projectcustomerlink.TenantIDEQ(tenantID), projectcustomerlink.CustomerIDEQ(targetID), projectcustomerlink.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	targetKeys := map[string]bool{}
	targetPrimaryProjects := map[int]bool{}
	for _, link := range targetLinks {
		targetKeys[fmt.Sprintf("%d|%s", link.ProjectID, link.RelationType)] = true
		if link.IsPrimary {
			targetPrimaryProjects[link.ProjectID] = true
		}
	}
	moves := []mergeProjectLinkMove{}
	deletes := []mergeProjectLinkDelete{}
	now := time.Now()
	for _, link := range sourceLinks {
		newBranchID := mappedBranchID(link.BranchID, branchMap)
		key := fmt.Sprintf("%d|%s", link.ProjectID, link.RelationType)
		if targetKeys[key] {
			if _, err := tx.ProjectCustomerLink.UpdateOneID(link.ID).SetDeletedAt(now).Save(ctx); err != nil {
				return nil, nil, err
			}
			deletes = append(deletes, mergeProjectLinkDelete{ID: link.ID, OldIsPrimary: link.IsPrimary})
			continue
		}
		newPrimary := link.IsPrimary
		if newPrimary && targetPrimaryProjects[link.ProjectID] {
			newPrimary = false
		}
		update := tx.ProjectCustomerLink.UpdateOneID(link.ID).
			SetCustomerID(targetID).
			SetIsPrimary(newPrimary)
		if newBranchID != nil {
			update.SetBranchID(*newBranchID)
		} else {
			update.ClearBranchID()
		}
		if _, err := update.Save(ctx); err != nil {
			return nil, nil, err
		}
		targetKeys[key] = true
		if newPrimary {
			targetPrimaryProjects[link.ProjectID] = true
		}
		moves = append(moves, mergeProjectLinkMove{
			ID: link.ID, OldBranchID: link.BranchID, NewBranchID: newBranchID,
			OldIsPrimary: link.IsPrimary, NewIsPrimary: newPrimary,
		})
	}
	return moves, deletes, nil
}
