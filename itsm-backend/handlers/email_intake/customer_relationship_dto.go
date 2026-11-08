package email_intake

import (
	"time"
)

type relationCustomerSummaryResponse struct {
	ID             int    `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	ShortName      string `json:"shortName"`
	Tier           string `json:"tier"`
	LifecycleStage string `json:"lifecycleStage"`
	Status         string `json:"status"`
}

func mapRelationCustomerSummary(v relationCustomerSummary) relationCustomerSummaryResponse {
	return relationCustomerSummaryResponse{
		ID:             v.ID,
		Code:           v.Code,
		Name:           v.Name,
		ShortName:      v.ShortName,
		Tier:           v.Tier,
		LifecycleStage: v.LifecycleStage,
		Status:         v.Status,
	}
}

type customerRelationshipResponse struct {
	ID              int                             `json:"id"`
	FromCustomerID  int                             `json:"fromCustomerId"`
	ToCustomerID    int                             `json:"toCustomerId"`
	RelationType    string                          `json:"relationType"`
	RelationRole    string                          `json:"relationRole"`
	IsBidirectional bool                            `json:"isBidirectional"`
	EffectiveFrom   *time.Time                      `json:"effectiveFrom,omitempty"`
	EffectiveTo     *time.Time                      `json:"effectiveTo,omitempty"`
	Note            string                          `json:"note"`
	Status          string                          `json:"status"`
	Direction       string                          `json:"direction"`
	FromCustomer    relationCustomerSummaryResponse `json:"fromCustomer"`
	ToCustomer      relationCustomerSummaryResponse `json:"toCustomer"`
	CreatedAt       time.Time                       `json:"createdAt"`
	UpdatedAt       time.Time                       `json:"updatedAt"`
}

func mapCustomerRelationship(row *customerRelationshipRow) customerRelationshipResponse {
	result := customerRelationshipResponse{
		ID:              row.Relation.ID,
		FromCustomerID:  row.Relation.FromCustomerID,
		ToCustomerID:    row.Relation.ToCustomerID,
		RelationType:    row.Relation.RelationType,
		RelationRole:    row.Relation.RelationRole,
		IsBidirectional: row.Relation.IsBidirectional,
		EffectiveFrom:   row.Relation.EffectiveFrom,
		EffectiveTo:     row.Relation.EffectiveTo,
		Note:            row.Relation.Note,
		Status:          row.Relation.Status,
		Direction:       row.Direction,
		CreatedAt:       row.Relation.CreatedAt,
		UpdatedAt:       row.Relation.UpdatedAt,
	}
	if row.FromCustomer != nil {
		result.FromCustomer = mapRelationCustomerSummary(relationSummary(row.FromCustomer))
	}
	if row.ToCustomer != nil {
		result.ToCustomer = mapRelationCustomerSummary(relationSummary(row.ToCustomer))
	}
	return result
}

type relationshipTreeNodeResponse struct {
	Customer     relationCustomerSummaryResponse `json:"customer"`
	RelationType string                          `json:"relationType"`
	Depth        int                             `json:"depth"`
	Children     []*relationshipTreeNodeResponse `json:"children"`
}

func mapRelationshipTreeNode(node *relationshipTreeNode) *relationshipTreeNodeResponse {
	if node == nil {
		return nil
	}
	result := &relationshipTreeNodeResponse{
		Customer:     mapRelationCustomerSummary(node.Customer),
		RelationType: node.RelationType,
		Depth:        node.Depth,
		Children:     make([]*relationshipTreeNodeResponse, 0, len(node.Children)),
	}
	for _, child := range node.Children {
		result.Children = append(result.Children, mapRelationshipTreeNode(child))
	}
	return result
}

type projectSummaryResponse struct {
	ID       int    `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
}

type branchSummaryResponse struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type projectCustomerLinkResponse struct {
	ID           int                              `json:"id"`
	ProjectID    int                              `json:"projectId"`
	CustomerID   int                              `json:"customerId"`
	BranchID     *int                             `json:"branchId,omitempty"`
	RelationType string                           `json:"relationType"`
	IsPrimary    bool                             `json:"isPrimary"`
	Note         string                           `json:"note"`
	Project      *projectSummaryResponse          `json:"project,omitempty"`
	Customer     *relationCustomerSummaryResponse `json:"customer,omitempty"`
	Branch       *branchSummaryResponse           `json:"branch,omitempty"`
	CreatedAt    time.Time                        `json:"createdAt"`
	UpdatedAt    time.Time                        `json:"updatedAt"`
}

func mapProjectCustomerLink(row *projectCustomerLinkRow) projectCustomerLinkResponse {
	result := projectCustomerLinkResponse{
		ID:           row.Link.ID,
		ProjectID:    row.Link.ProjectID,
		CustomerID:   row.Link.CustomerID,
		BranchID:     row.Link.BranchID,
		RelationType: row.Link.RelationType,
		IsPrimary:    row.Link.IsPrimary,
		Note:         row.Link.Note,
		CreatedAt:    row.Link.CreatedAt,
		UpdatedAt:    row.Link.UpdatedAt,
	}
	if row.Project != nil {
		result.Project = &projectSummaryResponse{
			ID:       row.Project.ID,
			Code:     row.Project.Code,
			Name:     row.Project.Name,
			Type:     row.Project.Type,
			Status:   row.Project.Status,
			Progress: row.Project.Progress,
		}
	}
	if row.Customer != nil {
		customer := mapRelationCustomerSummary(relationSummary(row.Customer))
		result.Customer = &customer
	}
	if row.Branch != nil {
		result.Branch = &branchSummaryResponse{
			ID:   row.Branch.ID,
			Code: row.Branch.Code,
			Name: row.Branch.Name,
		}
	}
	return result
}

func mapProjectCustomerLinks(rows []*projectCustomerLinkRow) []projectCustomerLinkResponse {
	result := make([]projectCustomerLinkResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapProjectCustomerLink(row))
	}
	return result
}

func mapCustomerRelationsTree(tree *customerRelationsTree) map[string]any {
	if tree == nil {
		return nil
	}
	parents := make([]*relationshipTreeNodeResponse, 0, len(tree.Parents))
	for _, parent := range tree.Parents {
		parents = append(parents, mapRelationshipTreeNode(parent))
	}
	children := make([]*relationshipTreeNodeResponse, 0, len(tree.Children))
	for _, child := range tree.Children {
		children = append(children, mapRelationshipTreeNode(child))
	}
	direct := make([]customerRelationshipResponse, 0, len(tree.DirectRelations))
	for _, relation := range tree.DirectRelations {
		direct = append(direct, mapCustomerRelationship(relation))
	}
	return map[string]any{
		"customer":        mapRelationCustomerSummary(tree.Customer),
		"parents":         parents,
		"children":        children,
		"directRelations": direct,
	}
}
