package email_intake

import (
	"context"
	"errors"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/customerbranch"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectcustomerlink"
	"itsm-backend/ent/servicecustomer"
)

var (
	ErrProjectCustomerLinkNotFound = errors.New("project customer link not found")
	ErrProjectCustomerLinkInvalid  = errors.New("project customer link invalid")
	ErrProjectCustomerLinkExists   = errors.New("project customer link exists")
	ErrProjectCustomerTypeInvalid  = errors.New("project customer type invalid")
)

type projectCustomerLinkRequest struct {
	ProjectID    int    `json:"projectId"`
	CustomerID   int    `json:"customerId"`
	BranchID     *int   `json:"branchId"`
	RelationType string `json:"relationType"`
	IsPrimary    bool   `json:"isPrimary"`
	Note         string `json:"note"`
}

type projectCustomerLinkRow struct {
	Link     *ent.ProjectCustomerLink
	Project  *ent.Project
	Customer *ent.ServiceCustomer
	Branch   *ent.CustomerBranch
}

var allowedProjectLinkTypes = map[string]bool{
	"presales": true, "delivery": true, "implementation": true,
	"support": true, "renewal": true, "escalation": true,
}

func (s *Service) ListCustomerProjects(ctx context.Context, tenantID, customerID int) ([]*projectCustomerLinkRow, error) {
	exists, err := s.CustomerExists(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomerNotFound
	}
	links, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.CustomerIDEQ(customerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Order(ent.Desc(projectcustomerlink.FieldIsPrimary), ent.Desc(projectcustomerlink.FieldUpdatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichProjectCustomerLinks(ctx, tenantID, links)
}

func (s *Service) ListProjectCustomerLinks(ctx context.Context, tenantID, projectID int) ([]*projectCustomerLinkRow, error) {
	if _, err := s.getProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	links, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.ProjectIDEQ(projectID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Order(ent.Desc(projectcustomerlink.FieldIsPrimary), ent.Desc(projectcustomerlink.FieldUpdatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichProjectCustomerLinks(ctx, tenantID, links)
}

func (s *Service) enrichProjectCustomerLinks(ctx context.Context, tenantID int, links []*ent.ProjectCustomerLink) ([]*projectCustomerLinkRow, error) {
	if len(links) == 0 {
		return []*projectCustomerLinkRow{}, nil
	}
	projectIDs := map[int]struct{}{}
	customerIDs := map[int]struct{}{}
	branchIDs := map[int]struct{}{}
	for _, link := range links {
		projectIDs[link.ProjectID] = struct{}{}
		customerIDs[link.CustomerID] = struct{}{}
		if link.BranchID != nil {
			branchIDs[*link.BranchID] = struct{}{}
		}
	}
	projects, err := s.client.Project.Query().
		Where(project.TenantIDEQ(tenantID), project.IDIn(intKeys(projectIDs)...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	customers, err := s.client.ServiceCustomer.Query().
		Where(servicecustomer.TenantIDEQ(tenantID), servicecustomer.IDIn(intKeys(customerIDs)...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	branches, err := s.client.CustomerBranch.Query().
		Where(customerbranch.TenantIDEQ(tenantID), customerbranch.IDIn(intKeys(branchIDs)...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	projectByID := map[int]*ent.Project{}
	for _, item := range projects {
		projectByID[item.ID] = item
	}
	customerByID := map[int]*ent.ServiceCustomer{}
	for _, item := range customers {
		customerByID[item.ID] = item
	}
	branchByID := map[int]*ent.CustomerBranch{}
	for _, item := range branches {
		branchByID[item.ID] = item
	}
	rows := make([]*projectCustomerLinkRow, 0, len(links))
	for _, link := range links {
		var branch *ent.CustomerBranch
		if link.BranchID != nil {
			branch = branchByID[*link.BranchID]
		}
		rows = append(rows, &projectCustomerLinkRow{
			Link:     link,
			Project:  projectByID[link.ProjectID],
			Customer: customerByID[link.CustomerID],
			Branch:   branch,
		})
	}
	return rows, nil
}

func intKeys(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}

func (s *Service) CreateCustomerProjectLink(ctx context.Context, tenantID, actorID, customerID int, req *projectCustomerLinkRequest) (*ent.ProjectCustomerLink, error) {
	if req.ProjectID < 1 {
		return nil, ErrProjectCustomerLinkInvalid
	}
	return s.createProjectCustomerLink(ctx, tenantID, actorID, customerID, req.ProjectID, req)
}

func (s *Service) CreateProjectCustomerLink(ctx context.Context, tenantID, actorID, projectID int, req *projectCustomerLinkRequest) (*ent.ProjectCustomerLink, error) {
	if req.CustomerID < 1 {
		return nil, ErrProjectCustomerLinkInvalid
	}
	return s.createProjectCustomerLink(ctx, tenantID, actorID, req.CustomerID, projectID, req)
}

func (s *Service) createProjectCustomerLink(ctx context.Context, tenantID, actorID, customerID, projectID int, req *projectCustomerLinkRequest) (*ent.ProjectCustomerLink, error) {
	relationType := defaultStringOr(strings.TrimSpace(req.RelationType), "delivery")
	if !allowedProjectLinkTypes[relationType] {
		return nil, ErrProjectCustomerLinkInvalid
	}
	if exists, err := s.CustomerExists(ctx, tenantID, customerID); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrCustomerNotFound
	}
	projectEntity, err := s.getProject(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if projectEntity.Type == "internal" {
		return nil, ErrProjectCustomerTypeInvalid
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
		if _, err := tx.ProjectCustomerLink.Update().
			Where(
				projectcustomerlink.TenantIDEQ(tenantID),
				projectcustomerlink.ProjectIDEQ(projectID),
				projectcustomerlink.IsPrimaryEQ(true),
				projectcustomerlink.DeletedAtIsNil(),
			).
			SetIsPrimary(false).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	entity, err := tx.ProjectCustomerLink.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetCustomerID(customerID).
		SetNillableBranchID(req.BranchID).
		SetRelationType(relationType).
		SetIsPrimary(req.IsPrimary).
		SetNote(req.Note).
		SetNillableCreatedBy(optionalActor(actorID)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrProjectCustomerLinkExists
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

func (s *Service) DeleteCustomerProjectLink(ctx context.Context, tenantID, customerID, linkID int) error {
	affected, err := s.client.ProjectCustomerLink.Update().
		Where(
			projectcustomerlink.IDEQ(linkID),
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.CustomerIDEQ(customerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrProjectCustomerLinkNotFound
	}
	return s.recalculateCustomerQuality(ctx, tenantID, customerID)
}

func (s *Service) DeleteProjectCustomerLink(ctx context.Context, tenantID, projectID, linkID int) error {
	link, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.IDEQ(linkID),
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.ProjectIDEQ(projectID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return ErrProjectCustomerLinkNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.client.ProjectCustomerLink.UpdateOneID(linkID).
		SetDeletedAt(time.Now()).
		Save(ctx); err != nil {
		return err
	}
	return s.recalculateCustomerQuality(ctx, tenantID, link.CustomerID)
}

func (s *Service) getProject(ctx context.Context, tenantID, projectID int) (*ent.Project, error) {
	item, err := s.client.Project.Query().
		Where(project.IDEQ(projectID), project.TenantIDEQ(tenantID), project.DeletedAtIsNil()).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrProjectCustomerLinkInvalid
	}
	return item, err
}
