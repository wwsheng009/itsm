package email_intake

import (
	"context"
	"errors"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/customerrelationship"
	"itsm-backend/ent/servicecustomer"
)

var (
	ErrCustomerRelationNotFound = errors.New("customer relationship not found")
	ErrCustomerRelationInvalid  = errors.New("customer relationship invalid")
	ErrCustomerRelationExists   = errors.New("customer relationship exists")
	ErrCustomerRelationCycle    = errors.New("customer relationship cycle")
)

type customerRelationshipRequest struct {
	ToCustomerID    int        `json:"toCustomerId" binding:"required"`
	RelationType    string     `json:"relationType" binding:"required"`
	IsBidirectional bool       `json:"isBidirectional"`
	EffectiveFrom   *time.Time `json:"effectiveFrom"`
	EffectiveTo     *time.Time `json:"effectiveTo"`
	Note            string     `json:"note"`
}

type customerRelationshipUpdateRequest struct {
	EffectiveFrom *time.Time `json:"effectiveFrom"`
	EffectiveTo   *time.Time `json:"effectiveTo"`
	Note          string     `json:"note"`
	Status        string     `json:"status"`
}

type relationCustomerSummary struct {
	ID             int    `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	ShortName      string `json:"shortName"`
	Tier           string `json:"tier"`
	LifecycleStage string `json:"lifecycleStage"`
	Status         string `json:"status"`
}

type customerRelationshipRow struct {
	Relation     *ent.CustomerRelationship
	FromCustomer *ent.ServiceCustomer
	ToCustomer   *ent.ServiceCustomer
	Direction    string
}

type relationshipTreeNode struct {
	Customer     relationCustomerSummary `json:"customer"`
	RelationType string                  `json:"relationType"`
	Depth        int                     `json:"depth"`
	Children     []*relationshipTreeNode `json:"children"`
}

type customerRelationsTree struct {
	Customer        relationCustomerSummary    `json:"customer"`
	Parents         []*relationshipTreeNode    `json:"parents"`
	Children        []*relationshipTreeNode    `json:"children"`
	DirectRelations []*customerRelationshipRow `json:"directRelations"`
}

var allowedRelationTypes = map[string]bool{
	"parent": true, "subsidiary": true, "partner": true, "reseller": true,
	"vendor": true, "competitor": true, "successor": true, "former_name_of": true,
}

var relationReverseType = map[string]string{
	"parent":     "subsidiary",
	"subsidiary": "parent",
	"partner":    "partner",
	"competitor": "competitor",
}

func relationSummary(v *ent.ServiceCustomer) relationCustomerSummary {
	if v == nil {
		return relationCustomerSummary{}
	}
	return relationCustomerSummary{
		ID:             v.ID,
		Code:           v.Code,
		Name:           v.Name,
		ShortName:      v.ShortName,
		Tier:           v.Tier,
		LifecycleStage: v.LifecycleStage,
		Status:         v.Status,
	}
}

func (s *Service) ListCustomerRelations(ctx context.Context, tenantID, customerID int) ([]*customerRelationshipRow, error) {
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	relations, err := s.client.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
			customerrelationship.Or(
				customerrelationship.FromCustomerIDEQ(customerID),
				customerrelationship.ToCustomerIDEQ(customerID),
			),
		).
		Order(ent.Asc(customerrelationship.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichRelationshipRows(ctx, tenantID, customerID, relations)
}

func (s *Service) enrichRelationshipRows(ctx context.Context, tenantID, currentCustomerID int, relations []*ent.CustomerRelationship) ([]*customerRelationshipRow, error) {
	if len(relations) == 0 {
		return []*customerRelationshipRow{}, nil
	}
	idSet := map[int]struct{}{}
	for _, relation := range relations {
		idSet[relation.FromCustomerID] = struct{}{}
		idSet[relation.ToCustomerID] = struct{}{}
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	customers, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.TenantIDEQ(tenantID), servicecustomer.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int]*ent.ServiceCustomer{}
	for _, customer := range customers {
		byID[customer.ID] = customer
	}
	rows := make([]*customerRelationshipRow, 0, len(relations))
	for _, relation := range relations {
		direction := "outbound"
		if relation.ToCustomerID == currentCustomerID && relation.FromCustomerID != currentCustomerID {
			direction = "inbound"
		}
		rows = append(rows, &customerRelationshipRow{
			Relation:     relation,
			FromCustomer: byID[relation.FromCustomerID],
			ToCustomer:   byID[relation.ToCustomerID],
			Direction:    direction,
		})
	}
	return rows, nil
}

func (s *Service) CreateCustomerRelationship(ctx context.Context, tenantID, actorID, customerID int, req *customerRelationshipRequest) (*ent.CustomerRelationship, error) {
	if req.ToCustomerID == customerID {
		return nil, ErrCustomerRelationInvalid
	}
	if !allowedRelationTypes[strings.TrimSpace(req.RelationType)] {
		return nil, ErrCustomerRelationInvalid
	}
	if req.EffectiveFrom != nil && req.EffectiveTo != nil && req.EffectiveTo.Before(*req.EffectiveFrom) {
		return nil, ErrCustomerRelationInvalid
	}
	if exists, err := s.CustomerExists(ctx, tenantID, customerID); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrCustomerNotFound
	}
	if exists, err := s.CustomerExists(ctx, tenantID, req.ToCustomerID); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrCustomerRelationInvalid
	}
	relationType := strings.TrimSpace(req.RelationType)
	if relationType == "parent" || relationType == "subsidiary" {
		parentID, childID := customerID, req.ToCustomerID
		if relationType == "subsidiary" {
			parentID, childID = req.ToCustomerID, customerID
		}
		cycle, err := s.hasAncestor(ctx, tenantID, parentID, childID, 5)
		if err != nil {
			return nil, err
		}
		if cycle {
			return nil, ErrCustomerRelationCycle
		}
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	forward, err := tx.CustomerRelationship.Create().
		SetTenantID(tenantID).
		SetFromCustomerID(customerID).
		SetToCustomerID(req.ToCustomerID).
		SetRelationType(relationType).
		SetRelationRole("outbound").
		SetIsBidirectional(req.IsBidirectional).
		SetNillableEffectiveFrom(req.EffectiveFrom).
		SetNillableEffectiveTo(req.EffectiveTo).
		SetNote(req.Note).
		SetStatus("active").
		SetNillableCreatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrCustomerRelationExists
		}
		return nil, err
	}
	if reverseType, ok := relationReverseType[relationType]; ok {
		_, err = tx.CustomerRelationship.Create().
			SetTenantID(tenantID).
			SetFromCustomerID(req.ToCustomerID).
			SetToCustomerID(customerID).
			SetRelationType(reverseType).
			SetRelationRole("inbound").
			SetIsBidirectional(true).
			SetNillableEffectiveFrom(req.EffectiveFrom).
			SetNillableEffectiveTo(req.EffectiveTo).
			SetNote(req.Note).
			SetStatus("active").
			SetNillableCreatedBy(optionalActor(actorID)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return nil, ErrCustomerRelationExists
			}
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return forward, nil
}

func (s *Service) UpdateCustomerRelationship(ctx context.Context, tenantID, customerID, relationID int, req *customerRelationshipUpdateRequest) (*ent.CustomerRelationship, error) {
	if req.EffectiveFrom != nil && req.EffectiveTo != nil && req.EffectiveTo.Before(*req.EffectiveFrom) {
		return nil, ErrCustomerRelationInvalid
	}
	if value := strings.TrimSpace(req.Status); value != "" && value != "active" && value != "ended" {
		return nil, ErrCustomerRelationInvalid
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	relation, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.IDEQ(relationID),
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.FromCustomerIDEQ(customerID),
			customerrelationship.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerRelationNotFound
	}
	if err != nil {
		return nil, err
	}
	status := defaultStringOr(req.Status, relation.Status)
	update := tx.CustomerRelationship.UpdateOneID(relationID).
		SetNillableEffectiveFrom(req.EffectiveFrom).
		SetNillableEffectiveTo(req.EffectiveTo).
		SetNote(req.Note).
		SetStatus(status)
	updated, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	if reverseType, ok := relationReverseType[relation.RelationType]; ok {
		reverseUpdate := tx.CustomerRelationship.Update().
			Where(
				customerrelationship.TenantIDEQ(tenantID),
				customerrelationship.FromCustomerIDEQ(relation.ToCustomerID),
				customerrelationship.ToCustomerIDEQ(customerID),
				customerrelationship.RelationTypeEQ(reverseType),
				customerrelationship.DeletedAtIsNil(),
			).
			SetStatus(status).
			SetNote(req.Note)
		if req.EffectiveFrom != nil {
			reverseUpdate.SetEffectiveFrom(*req.EffectiveFrom)
		} else {
			reverseUpdate.ClearEffectiveFrom()
		}
		if req.EffectiveTo != nil {
			reverseUpdate.SetEffectiveTo(*req.EffectiveTo)
		} else {
			reverseUpdate.ClearEffectiveTo()
		}
		if _, err := reverseUpdate.Save(ctx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) DeleteCustomerRelationship(ctx context.Context, tenantID, customerID, relationID int) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	relation, err := tx.CustomerRelationship.Query().
		Where(
			customerrelationship.IDEQ(relationID),
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.FromCustomerIDEQ(customerID),
			customerrelationship.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return ErrCustomerRelationNotFound
	}
	if err != nil {
		return err
	}
	now := time.Now()
	if _, err := tx.CustomerRelationship.UpdateOneID(relationID).
		SetStatus("ended").
		SetDeletedAt(now).
		Save(ctx); err != nil {
		return err
	}
	if reverseType, ok := relationReverseType[relation.RelationType]; ok {
		if _, err := tx.CustomerRelationship.Update().
			Where(
				customerrelationship.TenantIDEQ(tenantID),
				customerrelationship.FromCustomerIDEQ(relation.ToCustomerID),
				customerrelationship.ToCustomerIDEQ(customerID),
				customerrelationship.RelationTypeEQ(reverseType),
				customerrelationship.DeletedAtIsNil(),
			).
			SetStatus("ended").
			SetDeletedAt(now).
			Save(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) GetCustomerRelationsTree(ctx context.Context, tenantID, customerID, depth int) (*customerRelationsTree, error) {
	if depth < 1 {
		depth = 2
	}
	if depth > 5 {
		depth = 5
	}
	customer, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.IDEQ(customerID), servicecustomer.TenantIDEQ(tenantID), servicecustomer.DeletedAtIsNil()).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, err
	}
	direct, err := s.ListCustomerRelations(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	parents, err := s.buildParentChain(ctx, tenantID, customerID, depth)
	if err != nil {
		return nil, err
	}
	children, err := s.buildChildTree(ctx, tenantID, customerID, depth, map[int]bool{customerID: true})
	if err != nil {
		return nil, err
	}
	return &customerRelationsTree{
		Customer:        relationSummary(customer),
		Parents:         parents,
		Children:        children,
		DirectRelations: direct,
	}, nil
}

func (s *Service) buildParentChain(ctx context.Context, tenantID, customerID, depth int) ([]*relationshipTreeNode, error) {
	result := make([]*relationshipTreeNode, 0, depth)
	current := customerID
	visited := map[int]bool{current: true}
	for level := 1; level <= depth; level++ {
		parentID, ok, err := s.findParentID(ctx, tenantID, current)
		if err != nil {
			return nil, err
		}
		if !ok || visited[parentID] {
			break
		}
		visited[parentID] = true
		customer, err := s.client.ServiceCustomer.Get(ctx, parentID)
		if err != nil {
			return nil, err
		}
		result = append(result, &relationshipTreeNode{
			Customer:     relationSummary(customer),
			RelationType: "parent",
			Depth:        level,
			Children:     []*relationshipTreeNode{},
		})
		current = parentID
	}
	return result, nil
}

func (s *Service) buildChildTree(ctx context.Context, tenantID, customerID, maxDepth int, visited map[int]bool) ([]*relationshipTreeNode, error) {
	if maxDepth < 1 {
		return []*relationshipTreeNode{}, nil
	}
	relations, err := s.client.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.FromCustomerIDEQ(customerID),
			customerrelationship.RelationTypeEQ("parent"),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*relationshipTreeNode, 0, len(relations))
	for _, relation := range relations {
		if visited[relation.ToCustomerID] {
			continue
		}
		visited[relation.ToCustomerID] = true
		customer, err := s.client.ServiceCustomer.Get(ctx, relation.ToCustomerID)
		if err != nil {
			return nil, err
		}
		children, err := s.buildChildTree(ctx, tenantID, relation.ToCustomerID, maxDepth-1, visited)
		if err != nil {
			return nil, err
		}
		result = append(result, &relationshipTreeNode{
			Customer:     relationSummary(customer),
			RelationType: "subsidiary",
			Depth:        1,
			Children:     children,
		})
	}
	return result, nil
}

func (s *Service) findParentID(ctx context.Context, tenantID, customerID int) (int, bool, error) {
	relation, err := s.client.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.ToCustomerIDEQ(customerID),
			customerrelationship.RelationTypeEQ("parent"),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
		).
		First(ctx)
	if err == nil {
		return relation.FromCustomerID, true, nil
	}
	if !ent.IsNotFound(err) {
		return 0, false, err
	}
	relation, err = s.client.CustomerRelationship.Query().
		Where(
			customerrelationship.TenantIDEQ(tenantID),
			customerrelationship.FromCustomerIDEQ(customerID),
			customerrelationship.RelationTypeEQ("subsidiary"),
			customerrelationship.StatusEQ("active"),
			customerrelationship.DeletedAtIsNil(),
		).
		First(ctx)
	if ent.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return relation.ToCustomerID, true, nil
}

func (s *Service) hasAncestor(ctx context.Context, tenantID, startID, ancestorID, maxDepth int) (bool, error) {
	current := startID
	visited := map[int]bool{}
	for depth := 0; depth < maxDepth; depth++ {
		if visited[current] {
			return false, nil
		}
		visited[current] = true
		parentID, ok, err := s.findParentID(ctx, tenantID, current)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if parentID == ancestorID {
			return true, nil
		}
		current = parentID
	}
	return false, nil
}
