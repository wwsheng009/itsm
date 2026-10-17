package global_search

import (
	"context"
	"sort"

	"itsm-backend/ent"
	"itsm-backend/ent/change"
	"itsm-backend/ent/incident"
	"itsm-backend/ent/knowledgearticle"
	"itsm-backend/ent/problem"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectmember"
	"itsm-backend/ent/projecttask"
	"itsm-backend/ent/ticket"
)

const perTypeLimit = 10

// Service owns the tenant-scoped global-search use case.
type Service struct {
	client *ent.Client
}

func NewService(client *ent.Client) *Service { return &Service{client: client} }

// Search 跨域搜索：工单/事件/问题/变更/知识库 + 项目/项目任务（PE-16）。
//
// 可见性（集成契约 §12）：项目与项目任务强制可见性谓词——admin=false 时只命中
// "活跃成员 ∪ 项目经理" 的项目，避免搜索结果泄露不可见项目；admin=true 命中租户内全部未软删项目。
func (s *Service) Search(ctx context.Context, tenantID, viewerID int, admin bool, keyword string) (*SearchResponse, error) {
	results := make([]*SearchResult, 0)

	tickets, err := s.client.Ticket.Query().Where(ticket.TenantID(tenantID), ticket.Or(
		ticket.TitleContainsFold(keyword), ticket.DescriptionContainsFold(keyword), ticket.TicketNumberContainsFold(keyword),
	)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range tickets {
		results = append(results, &SearchResult{ID: item.ID, Type: "ticket", Title: item.Title, Description: item.Description, Status: item.Status, TicketNumber: item.TicketNumber})
	}

	incidents, err := s.client.Incident.Query().Where(incident.TenantID(tenantID), incident.Or(
		incident.TitleContainsFold(keyword), incident.DescriptionContainsFold(keyword), incident.IncidentNumberContainsFold(keyword),
	)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range incidents {
		results = append(results, &SearchResult{ID: item.ID, Type: "incident", Title: item.Title, Description: item.Description, Status: item.Status, TicketNumber: item.IncidentNumber})
	}

	problems, err := s.client.Problem.Query().Where(problem.TenantID(tenantID), problem.Or(
		problem.TitleContainsFold(keyword), problem.DescriptionContainsFold(keyword),
	)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range problems {
		results = append(results, &SearchResult{ID: item.ID, Type: "problem", Title: item.Title, Description: item.Description, Status: item.Status})
	}

	changes, err := s.client.Change.Query().Where(change.TenantID(tenantID), change.Or(
		change.TitleContainsFold(keyword), change.DescriptionContainsFold(keyword),
	)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range changes {
		results = append(results, &SearchResult{ID: item.ID, Type: "change", Title: item.Title, Description: item.Description, Status: item.Status})
	}

	articles, err := s.client.KnowledgeArticle.Query().Where(knowledgearticle.TenantID(tenantID), knowledgearticle.DeletedAtIsNil(), knowledgearticle.Or(
		knowledgearticle.TitleContainsFold(keyword), knowledgearticle.ContentContainsFold(keyword),
	)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range articles {
		status := "draft"
		if item.IsPublished {
			status = "published"
		}
		results = append(results, &SearchResult{ID: item.ID, Type: "knowledge", Title: item.Title, Description: item.Content, Status: status})
	}

	projectResults, taskResults, err := s.searchProjectsAndTasks(ctx, tenantID, viewerID, admin, keyword)
	if err != nil {
		return nil, err
	}
	results = append(results, projectResults...)
	results = append(results, taskResults...)

	return &SearchResponse{Results: results, Total: len(results)}, nil
}

// searchProjectsAndTasks 项目（名称/编号）与项目任务（任务名）搜索，结果均经可见性谓词过滤。
func (s *Service) searchProjectsAndTasks(ctx context.Context, tenantID, viewerID int, admin bool, keyword string) ([]*SearchResult, []*SearchResult, error) {
	visibleIDs, err := s.visibleProjectIDs(ctx, tenantID, viewerID, admin)
	if err != nil {
		return nil, nil, err
	}
	if len(visibleIDs) == 0 {
		return nil, nil, nil
	}

	projects, err := s.client.Project.Query().Where(
		project.TenantIDEQ(tenantID),
		project.DeletedAtIsNil(),
		project.IDIn(visibleIDs...),
		project.Or(project.NameContainsFold(keyword), project.CodeContainsFold(keyword)),
	).Order(ent.Asc(project.FieldID)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	projectResults := make([]*SearchResult, 0, len(projects))
	names := make(map[int]string, len(projects))
	codes := make(map[int]string, len(projects))
	for _, item := range projects {
		names[item.ID] = item.Name
		codes[item.ID] = item.Code
		projectResults = append(projectResults, &SearchResult{
			ID: item.ID, Type: "project", Title: item.Name,
			Description: item.Description, Status: item.Status, Code: item.Code,
		})
	}

	tasks, err := s.client.ProjectTask.Query().Where(
		projecttask.TenantIDEQ(tenantID),
		projecttask.DeletedAtIsNil(),
		projecttask.ProjectIDIn(visibleIDs...),
		projecttask.NameContainsFold(keyword),
	).Order(ent.Asc(projecttask.FieldID)).Limit(perTypeLimit).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(tasks) > 0 {
		// 任务结果附带项目名/编号（任务名命中但项目名未必命中，需补查）。
		if err := s.fillProjectLabels(ctx, tenantID, tasks, names, codes); err != nil {
			return nil, nil, err
		}
	}
	taskResults := make([]*SearchResult, 0, len(tasks))
	for _, item := range tasks {
		taskResults = append(taskResults, &SearchResult{
			ID: item.ID, Type: "project_task", Title: item.Name,
			Description: item.Description, Status: item.Status,
			ProjectID: item.ProjectID, ProjectName: names[item.ProjectID], Code: codes[item.ProjectID],
		})
	}
	return projectResults, taskResults, nil
}

// fillProjectLabels 为任务结果补充其所属项目的名称/编号（跳过已缓存的 ID）。
func (s *Service) fillProjectLabels(ctx context.Context, tenantID int, tasks []*ent.ProjectTask, names, codes map[int]string) error {
	missing := make([]int, 0, len(tasks))
	for _, item := range tasks {
		if _, ok := names[item.ProjectID]; !ok {
			missing = append(missing, item.ProjectID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	rows, err := s.client.Project.Query().Where(
		project.TenantIDEQ(tenantID),
		project.IDIn(missing...),
	).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		names[row.ID] = row.Name
		codes[row.ID] = row.Code
	}
	return nil
}

// visibleProjectIDs 搜索可见项目集：admin=租户内全部未软删项目；否则=活跃成员 ∪ 项目经理（去重升序）。
//
// 与项目域可见性谓词一致（非管理角色须为成员；项目经理为 projects.manager_id 唯一权威且不可被移除，故一并可见）。
func (s *Service) visibleProjectIDs(ctx context.Context, tenantID, viewerID int, admin bool) ([]int, error) {
	if admin {
		return s.client.Project.Query().Where(
			project.TenantIDEQ(tenantID), project.DeletedAtIsNil(),
		).IDs(ctx)
	}

	set := make(map[int]struct{})
	members, err := s.client.ProjectMember.Query().Where(
		projectmember.TenantIDEQ(tenantID),
		projectmember.UserIDEQ(viewerID),
		projectmember.StatusEQ("active"),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		set[m.ProjectID] = struct{}{}
	}

	managed, err := s.client.Project.Query().Where(
		project.TenantIDEQ(tenantID), project.DeletedAtIsNil(), project.ManagerIDEQ(viewerID),
	).IDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range managed {
		set[id] = struct{}{}
	}

	out := make([]int, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}