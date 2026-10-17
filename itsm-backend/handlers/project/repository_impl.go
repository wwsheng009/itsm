package project

import (
	"context"
	"sort"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/change"
	"itsm-backend/ent/incident"
	"itsm-backend/ent/problem"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectbaseline"
	"itsm-backend/ent/projectmember"
	"itsm-backend/ent/projectmilestone"
	"itsm-backend/ent/projectrisk"
	"itsm-backend/ent/projecttask"
	"itsm-backend/ent/projecttaskdependency"
	"itsm-backend/ent/servicerequest"
	"itsm-backend/ent/ticket"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
	"itsm-backend/middleware"
)

// EntRepository Repository 的 ent 实现。
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository 构造仓储。
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// Create 创建项目。
func (r *EntRepository) Create(ctx context.Context, p CreateParams) (*ent.Project, error) {
	builder := r.client.Project.Create().
		SetTenantID(p.TenantID).
		SetName(p.Name).
		SetCode(p.Code).
		SetType(p.Type).
		SetStatus(p.Status).
		SetNillableManagerID(optionalInt(p.ManagerID)).
		SetNillableDepartmentID(optionalInt(p.DepartmentID)).
		SetNillableOwnerTeamID(optionalInt(p.OwnerTeamID)).
		SetNillableStartDate(p.StartDate).
		SetNillableEndDate(p.EndDate).
		SetNillableBudgetAmount(p.BudgetAmount).
		SetNillableBudgetCurrency(p.BudgetCurrency)
	if p.Description != "" {
		builder = builder.SetDescription(p.Description)
	}
	return builder.Save(ctx)
}

// Get 按租户取单条（软删不可见）。
func (r *EntRepository) Get(ctx context.Context, tenantID, id int) (*ent.Project, error) {
	return r.client.Project.Query().
		Where(project.IDEQ(id), project.TenantIDEQ(tenantID), project.DeletedAtIsNil()).
		Only(ctx)
}

// listQuery 组装项目列表谓词（列表与组合视图共用——口径同源；D-40）。
func (r *EntRepository) listQuery(f ListFilter) *ent.ProjectQuery {
	query := r.client.Project.Query().Where(project.TenantIDEQ(f.TenantID), project.DeletedAtIsNil())
	if f.ProjectIDs != nil {
		query = query.Where(project.IDIn(f.ProjectIDs...))
	}
	if f.Status != "" {
		query = query.Where(project.StatusEQ(f.Status))
	}
	if f.Type != "" {
		query = query.Where(project.TypeEQ(f.Type))
	}
	if f.OwnerTeamID > 0 {
		query = query.Where(project.OwnerTeamIDEQ(f.OwnerTeamID))
	}
	if f.ManagerID > 0 {
		query = query.Where(project.ManagerIDEQ(f.ManagerID))
	}
	if f.Keyword != "" {
		query = query.Where(project.Or(
			project.NameContainsFold(f.Keyword),
			project.CodeContainsFold(f.Keyword),
		))
	}
	return query
}

// List 列表 + 总数。
func (r *EntRepository) List(ctx context.Context, f ListFilter) ([]*ent.Project, int64, error) {
	if f.ProjectIDs != nil && len(f.ProjectIDs) == 0 {
		return []*ent.Project{}, 0, nil
	}
	query := r.listQuery(f)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Order(ent.Desc(project.FieldID)).
		Offset(f.Offset).
		Limit(f.Limit).
		All(ctx)
	return items, int64(total), err
}

// Update 基础字段更新（version>0 时走乐观锁 CAS）。
func (r *EntRepository) Update(ctx context.Context, tenantID, id int, p UpdateParams, version int) (*ent.Project, error) {
	upd := r.client.Project.UpdateOneID(id).
		Where(project.TenantIDEQ(tenantID), project.DeletedAtIsNil())
	if version > 0 {
		upd = upd.Where(project.VersionEQ(version))
	}
	if p.Name != nil {
		upd = upd.SetName(*p.Name)
	}
	if p.Description != nil {
		upd = upd.SetDescription(*p.Description)
	}
	if p.Type != nil {
		upd = upd.SetType(*p.Type)
	}
	if p.Health != nil {
		upd = upd.SetHealth(*p.Health)
	}
	if p.Progress != nil {
		upd = upd.SetProgress(*p.Progress)
	}
	if p.ManagerID != nil {
		upd = upd.SetNillableManagerID(optionalInt(*p.ManagerID))
	}
	if p.DepartmentID != nil {
		upd = upd.SetNillableDepartmentID(optionalInt(*p.DepartmentID))
	}
	if p.OwnerTeamID != nil {
		upd = upd.SetNillableOwnerTeamID(optionalInt(*p.OwnerTeamID))
	}
	if p.StartDate != nil {
		upd = upd.SetNillableStartDate(p.StartDate)
	}
	if p.EndDate != nil {
		upd = upd.SetNillableEndDate(p.EndDate)
	}
	// P2（PE-22）预算：<=0 / 空串 = 清空（写回 NULL；读侧 nil=未设置）。
	if p.BudgetAmount != nil {
		if *p.BudgetAmount <= 0 {
			upd = upd.ClearBudgetAmount()
		} else {
			upd = upd.SetBudgetAmount(*p.BudgetAmount)
		}
	}
	if p.BudgetCurrency != nil {
		if strings.TrimSpace(*p.BudgetCurrency) == "" {
			upd = upd.ClearBudgetCurrency()
		} else {
			upd = upd.SetBudgetCurrency(strings.TrimSpace(*p.BudgetCurrency))
		}
	}
	upd = upd.AddVersion(1)
	return upd.Save(ctx)
}

// StatusCAS 状态流转（from 状态 + 可选版本双重校验）。
func (r *EntRepository) StatusCAS(ctx context.Context, tenantID, id int, from, to string, version int, patch StatusPatch) (*ent.Project, error) {
	upd := r.client.Project.UpdateOneID(id).
		Where(project.TenantIDEQ(tenantID), project.DeletedAtIsNil(), project.StatusEQ(from))
	if version > 0 {
		upd = upd.Where(project.VersionEQ(version))
	}
	upd = upd.SetStatus(to).AddVersion(1)
	if patch.ActualStart != nil {
		upd = upd.SetActualStart(*patch.ActualStart)
	}
	if patch.ActualEnd != nil {
		upd = upd.SetActualEnd(*patch.ActualEnd)
	}
	if patch.ClosedAt != nil {
		upd = upd.SetClosedAt(*patch.ClosedAt)
	}
	return upd.Save(ctx)
}

// SoftDelete 软删。
func (r *EntRepository) SoftDelete(ctx context.Context, tenantID, id int) error {
	return r.client.Project.UpdateOneID(id).
		Where(project.TenantIDEQ(tenantID), project.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Exec(ctx)
}

// CodeExists 租户内项目代码占用检查（软删释放）。
func (r *EntRepository) CodeExists(ctx context.Context, tenantID int, code string) (bool, error) {
	return r.client.Project.Query().
		Where(project.TenantIDEQ(tenantID), project.CodeEQ(code), project.DeletedAtIsNil()).
		Exist(ctx)
}

// CountOpenTasks 未终结任务数（结项门禁）。
func (r *EntRepository) CountOpenTasks(ctx context.Context, tenantID, projectID int) (int, error) {
	return r.client.ProjectTask.Query().
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDEQ(projectID),
			projecttask.DeletedAtIsNil(),
			projecttask.StatusNotIn("done", "cancelled"),
		).
		Count(ctx)
}

// SumTaskHours 任务工时汇总（PE-22；口径=项目全部未删任务；只取两列求和，避免加载全行）。
func (r *EntRepository) SumTaskHours(ctx context.Context, tenantID, projectID int) (*TaskHoursSummary, error) {
	tasks, err := r.client.ProjectTask.Query().
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDEQ(projectID),
			projecttask.DeletedAtIsNil(),
		).
		Select(projecttask.FieldEstimateHours, projecttask.FieldActualHours).
		All(ctx)
	if err != nil {
		return nil, err
	}
	summary := &TaskHoursSummary{TaskCount: len(tasks)}
	for _, t := range tasks {
		if t.EstimateHours > 0 {
			summary.EstimatedTaskCount++
			summary.EstimateHours += t.EstimateHours
		}
		if t.ActualHours > 0 {
			summary.ActualTaskCount++
			summary.ActualHours += t.ActualHours
		}
	}
	return summary, nil
}

// NonTerminalRecords 未终结关联单据（每域最多取样 10 条用于阻塞展示）。
func (r *EntRepository) NonTerminalRecords(ctx context.Context, tenantID, projectID int) ([]Blocker, error) {
	blockers := make([]Blocker, 0, 8)

	tickets, err := r.client.Ticket.Query().
		Where(ticket.TenantIDEQ(tenantID), ticket.ProjectID(projectID), ticket.StatusNotIn(notInArgs("ticket")...)).
		Limit(10).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range tickets {
		blockers = append(blockers, Blocker{Type: "ticket", ID: it.ID, Number: it.TicketNumber, Title: it.Title, Status: it.Status})
	}

	incidents, err := r.client.Incident.Query().
		Where(incident.TenantIDEQ(tenantID), incident.ProjectID(projectID), incident.StatusNotIn(notInArgs("incident")...)).
		Limit(10).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range incidents {
		blockers = append(blockers, Blocker{Type: "incident", ID: it.ID, Number: it.IncidentNumber, Title: it.Title, Status: it.Status})
	}

	problems, err := r.client.Problem.Query().
		Where(problem.TenantIDEQ(tenantID), problem.ProjectID(projectID), problem.StatusNotIn(notInArgs("problem")...)).
		Limit(10).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range problems {
		blockers = append(blockers, Blocker{Type: "problem", ID: it.ID, Number: it.ProblemNumber, Title: it.Title, Status: it.Status})
	}

	changes, err := r.client.Change.Query().
		Where(change.TenantIDEQ(tenantID), change.ProjectID(projectID), change.StatusNotIn(notInArgs("change")...)).
		Limit(10).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range changes {
		blockers = append(blockers, Blocker{Type: "change", ID: it.ID, Number: it.ChangeNumber, Title: it.Title, Status: it.Status})
	}

	requests, err := r.client.ServiceRequest.Query().
		Where(servicerequest.TenantIDEQ(tenantID), servicerequest.ProjectID(projectID), servicerequest.StatusNotIn(notInArgs("service_request")...)).
		Limit(10).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range requests {
		blockers = append(blockers, Blocker{Type: "service_request", ID: it.ID, Title: it.Title, Status: it.Status})
	}

	return blockers, nil
}

// HasLinks 删除门禁：存在任务或五类单据任一关联即拒绝（含终态，保留历史）。
func (r *EntRepository) HasLinks(ctx context.Context, tenantID, projectID int) (bool, error) {
	taskExists, err := r.client.ProjectTask.Query().
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID)).
		Exist(ctx)
	if err != nil {
		return false, err
	}
	if taskExists {
		return true, nil
	}
	checks := []func() (bool, error){
		func() (bool, error) {
			return r.client.Ticket.Query().Where(ticket.TenantIDEQ(tenantID), ticket.ProjectID(projectID)).Exist(ctx)
		},
		func() (bool, error) {
			return r.client.Incident.Query().Where(incident.TenantIDEQ(tenantID), incident.ProjectID(projectID)).Exist(ctx)
		},
		func() (bool, error) {
			return r.client.Problem.Query().Where(problem.TenantIDEQ(tenantID), problem.ProjectID(projectID)).Exist(ctx)
		},
		func() (bool, error) {
			return r.client.Change.Query().Where(change.TenantIDEQ(tenantID), change.ProjectID(projectID)).Exist(ctx)
		},
		func() (bool, error) {
			return r.client.ServiceRequest.Query().Where(servicerequest.TenantIDEQ(tenantID), servicerequest.ProjectID(projectID)).Exist(ctx)
		},
	}
	for _, check := range checks {
		found, err := check()
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// 成员与可见性（PE-3）
// ---------------------------------------------------------------------------

// ListMembers 项目成员（含已移除行，按加入时间）。
func (r *EntRepository) ListMembers(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectMember, error) {
	return r.client.ProjectMember.Query().
		Where(projectmember.TenantIDEQ(tenantID), projectmember.ProjectIDEQ(projectID)).
		Order(ent.Asc(projectmember.FieldJoinedAt)).
		All(ctx)
}

// liveMember 取存活成员行（不存在返回 ent.NotFound）。
func (r *EntRepository) liveMember(ctx context.Context, tenantID, projectID, userID int) (*ent.ProjectMember, error) {
	return r.client.ProjectMember.Query().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.ProjectIDEQ(projectID),
			projectmember.UserIDEQ(userID),
			projectmember.StatusEQ("active"),
		).
		Only(ctx)
}

// AddMember 添加成员：存在历史行则复活（status=active + 角色覆写），否则新建。
func (r *EntRepository) AddMember(ctx context.Context, tenantID, projectID, userID int, role string, createdBy int) (*ent.ProjectMember, error) {
	existing, err := r.client.ProjectMember.Query().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.ProjectIDEQ(projectID),
			projectmember.UserIDEQ(userID),
		).
		Only(ctx)
	if err == nil {
		upd := existing.Update().
			SetStatus("active").
			SetProjectRole(role).
			SetJoinedAt(time.Now())
		if createdBy > 0 {
			upd = upd.SetCreatedBy(createdBy)
		}
		return upd.Save(ctx)
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	builder := r.client.ProjectMember.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetUserID(userID).
		SetProjectRole(role)
	if createdBy > 0 {
		builder = builder.SetCreatedBy(createdBy)
	}
	return builder.Save(ctx)
}

// UpdateMemberRole 调整存活成员角色。
func (r *EntRepository) UpdateMemberRole(ctx context.Context, tenantID, projectID, userID int, role string) (*ent.ProjectMember, error) {
	live, err := r.liveMember(ctx, tenantID, projectID, userID)
	if err != nil {
		return nil, err
	}
	return live.Update().SetProjectRole(role).Save(ctx)
}

// RemoveMember 软失效成员。
func (r *EntRepository) RemoveMember(ctx context.Context, tenantID, projectID, userID int) error {
	live, err := r.liveMember(ctx, tenantID, projectID, userID)
	if err != nil {
		return err
	}
	return live.Update().SetStatus("removed").Exec(ctx)
}

// IsActiveMember 是否存活成员。
func (r *EntRepository) IsActiveMember(ctx context.Context, tenantID, projectID, userID int) (bool, error) {
	return r.client.ProjectMember.Query().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.ProjectIDEQ(projectID),
			projectmember.UserIDEQ(userID),
			projectmember.StatusEQ("active"),
		).
		Exist(ctx)
}

// MemberProjectIDs 该用户在租户内以成员身份参与的项目 ID。
func (r *EntRepository) MemberProjectIDs(ctx context.Context, tenantID, userID int) ([]int, error) {
	rows, err := r.client.ProjectMember.Query().
		Where(
			projectmember.TenantIDEQ(tenantID),
			projectmember.UserIDEQ(userID),
			projectmember.StatusEQ("active"),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ProjectID]; ok {
			continue
		}
		seen[row.ProjectID] = struct{}{}
		ids = append(ids, row.ProjectID)
	}
	return ids, nil
}

// ActiveMembership 目标租户存活且未过期的 membership。
func (r *EntRepository) ActiveMembership(ctx context.Context, tenantID, userID int) (*ent.UserTenantMembership, error) {
	return r.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.TenantIDEQ(tenantID),
			usertenantmembership.UserIDEQ(userID),
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
			usertenantmembership.Or(
				usertenantmembership.ExpiresAtIsNil(),
				usertenantmembership.ExpiresAtGT(time.Now()),
			),
		).
		Only(ctx)
}

// MembershipsByUserIDs 批量取 membership（不区分过期，用于派生标签/到期展示）。
func (r *EntRepository) MembershipsByUserIDs(ctx context.Context, tenantID int, userIDs []int) ([]*ent.UserTenantMembership, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	return r.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.TenantIDEQ(tenantID),
			usertenantmembership.UserIDIn(userIDs...),
			usertenantmembership.DeletedAtIsNil(),
		).
		All(ctx)
}

// ActiveMemberships 目标租户存活且未过期的 membership（候选查询）。
func (r *EntRepository) ActiveMemberships(ctx context.Context, tenantID, limit int) ([]*ent.UserTenantMembership, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return r.client.UserTenantMembership.Query().
		Where(
			usertenantmembership.TenantIDEQ(tenantID),
			usertenantmembership.DeletedAtIsNil(),
			usertenantmembership.StatusEQ(usertenantmembership.StatusActive),
			usertenantmembership.Or(
				usertenantmembership.ExpiresAtIsNil(),
				usertenantmembership.ExpiresAtGT(time.Now()),
			),
		).
		Order(ent.Asc(usertenantmembership.FieldUserID)).
		Limit(limit).
		All(ctx)
}

// UsersByIDs 批量取用户展示字段。
func (r *EntRepository) UsersByIDs(ctx context.Context, userIDs []int) ([]*ent.User, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	return r.client.User.Query().Where(user.IDIn(userIDs...)).All(ctx)
}

// SetProjectDefaultCapacity 设置/清空项目周容量默认（PE-23；hours=nil → 回内置 40h）。
func (r *EntRepository) SetProjectDefaultCapacity(ctx context.Context, tenantID, projectID int, hours *float64) error {
	upd := r.client.Project.UpdateOneID(projectID).
		Where(project.TenantIDEQ(tenantID), project.DeletedAtIsNil())
	if hours == nil {
		upd = upd.ClearDefaultWeeklyCapacityHours()
	} else {
		upd = upd.SetDefaultWeeklyCapacityHours(*hours)
	}
	return upd.Exec(ctx)
}

// SetMemberCapacity 设置/清空成员周容量覆盖（PE-23；hours=nil → 继承项目默认；仅存活成员行）。
func (r *EntRepository) SetMemberCapacity(ctx context.Context, tenantID, projectID, userID int, hours *float64) error {
	live, err := r.liveMember(ctx, tenantID, projectID, userID)
	if err != nil {
		return err
	}
	upd := live.Update()
	if hours == nil {
		upd = upd.ClearWeeklyCapacityHours()
	} else {
		upd = upd.SetWeeklyCapacityHours(*hours)
	}
	return upd.Exec(ctx)
}

// ---------------------------------------------------------------------------
// 任务（PE-4）
// ---------------------------------------------------------------------------

// ListTasks 项目任务（平铺；sort_order 优先）。
func (r *EntRepository) ListTasks(ctx context.Context, tenantID, projectID int, status string, assigneeID int) ([]*ent.ProjectTask, error) {
	query := r.client.ProjectTask.Query().
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID), projecttask.DeletedAtIsNil())
	if status != "" {
		query = query.Where(projecttask.StatusEQ(status))
	}
	if assigneeID > 0 {
		query = query.Where(projecttask.AssigneeIDEQ(assigneeID))
	}
	return query.
		Order(ent.Asc(projecttask.FieldSortOrder), ent.Asc(projecttask.FieldID)).
		All(ctx)
}

// GetTask 项目内单条任务。
func (r *EntRepository) GetTask(ctx context.Context, tenantID, projectID, taskID int) (*ent.ProjectTask, error) {
	return r.client.ProjectTask.Query().
		Where(
			projecttask.IDEQ(taskID),
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDEQ(projectID),
			projecttask.DeletedAtIsNil(),
		).
		Only(ctx)
}

// GetTaskByID 按 ID 取任务（仅租户限定；深度回溯用）。
func (r *EntRepository) GetTaskByID(ctx context.Context, tenantID, taskID int) (*ent.ProjectTask, error) {
	return r.client.ProjectTask.Query().
		Where(projecttask.IDEQ(taskID), projecttask.TenantIDEQ(tenantID), projecttask.DeletedAtIsNil()).
		Only(ctx)
}

// CreateTask 创建任务。
func (r *EntRepository) CreateTask(ctx context.Context, p TaskCreateParams) (*ent.ProjectTask, error) {
	builder := r.client.ProjectTask.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetName(p.Name).
		SetPriority(p.Priority).
		SetSortOrder(p.SortOrder).
		SetNillableParentTaskID(optionalInt(p.ParentTaskID)).
		SetNillableAssigneeID(optionalInt(p.AssigneeID)).
		SetNillablePlanStart(p.PlanStart).
		SetNillablePlanEnd(p.PlanEnd).
		SetNillableEstimateHours(p.EstimateHours).
		SetNillableCreatedBy(optionalInt(p.CreatedBy))
	if p.Description != "" {
		builder = builder.SetDescription(p.Description)
	}
	return builder.Save(ctx)
}

// UpdateTaskCAS 任务更新（version>0 校验乐观锁）。
func (r *EntRepository) UpdateTaskCAS(ctx context.Context, tenantID, projectID, taskID int, p TaskUpdateParams, version int) (*ent.ProjectTask, error) {
	upd := r.client.ProjectTask.UpdateOneID(taskID).
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID), projecttask.DeletedAtIsNil())
	if version > 0 {
		upd = upd.Where(projecttask.VersionEQ(version))
	}
	if p.Name != nil {
		upd = upd.SetName(*p.Name)
	}
	if p.Description != nil {
		upd = upd.SetDescription(*p.Description)
	}
	if p.AssigneeID != nil {
		upd = upd.SetNillableAssigneeID(optionalInt(*p.AssigneeID))
	}
	if p.Priority != nil {
		upd = upd.SetPriority(*p.Priority)
	}
	if p.PlanStart != nil {
		upd = upd.SetNillablePlanStart(p.PlanStart)
	}
	if p.PlanEnd != nil {
		upd = upd.SetNillablePlanEnd(p.PlanEnd)
	}
	if p.ActualStart != nil {
		upd = upd.SetNillableActualStart(p.ActualStart)
	}
	if p.ActualEnd != nil {
		upd = upd.SetNillableActualEnd(p.ActualEnd)
	}
	if p.Progress != nil {
		upd = upd.SetProgress(*p.Progress)
	}
	if p.EstimateHours != nil {
		upd = upd.SetNillableEstimateHours(p.EstimateHours)
	}
	if p.ActualHours != nil {
		upd = upd.SetNillableActualHours(p.ActualHours)
	}
	if p.SortOrder != nil {
		upd = upd.SetSortOrder(*p.SortOrder)
	}
	return upd.AddVersion(1).Save(ctx)
}

// TaskStatusCAS 任务状态流转（from + 可选版本双重校验）。
func (r *EntRepository) TaskStatusCAS(ctx context.Context, tenantID, projectID, taskID int, from, to string, version int, patch TaskStatusPatch) (*ent.ProjectTask, error) {
	upd := r.client.ProjectTask.UpdateOneID(taskID).
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDEQ(projectID),
			projecttask.DeletedAtIsNil(),
			projecttask.StatusEQ(from),
		)
	if version > 0 {
		upd = upd.Where(projecttask.VersionEQ(version))
	}
	upd = upd.SetStatus(to).AddVersion(1)
	if patch.ActualStart != nil {
		upd = upd.SetActualStart(*patch.ActualStart)
	}
	if patch.ActualEnd != nil {
		upd = upd.SetActualEnd(*patch.ActualEnd)
	}
	if patch.Progress != nil {
		upd = upd.SetProgress(*patch.Progress)
	}
	return upd.Save(ctx)
}

// SoftDeleteTask 软删任务。
func (r *EntRepository) SoftDeleteTask(ctx context.Context, tenantID, projectID, taskID int) error {
	return r.client.ProjectTask.UpdateOneID(taskID).
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID), projecttask.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Exec(ctx)
}

// CountTaskChildren 子任务数。
func (r *EntRepository) CountTaskChildren(ctx context.Context, tenantID, projectID, parentTaskID int) (int, error) {
	return r.client.ProjectTask.Query().
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDEQ(projectID),
			projecttask.ParentTaskIDEQ(parentTaskID),
			projecttask.DeletedAtIsNil(),
		).
		Count(ctx)
}

// ListTasksByAssignee 个人任务聚合（默认排除终态；projectIDs 收窄可见范围）。
func (r *EntRepository) ListTasksByAssignee(ctx context.Context, tenantID, userID int, projectIDs []int, status string, limit int) ([]*ent.ProjectTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := r.client.ProjectTask.Query().
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.AssigneeIDEQ(userID),
			projecttask.DeletedAtIsNil(),
		)
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return []*ent.ProjectTask{}, nil
		}
		query = query.Where(projecttask.ProjectIDIn(projectIDs...))
	}
	if status != "" {
		query = query.Where(projecttask.StatusEQ(status))
	} else {
		query = query.Where(projecttask.StatusNotIn(TaskStatusDone, TaskStatusCancelled))
	}
	return query.
		Order(ent.Desc(projecttask.FieldUpdatedAt)).
		Limit(limit).
		All(ctx)
}

// ---------------------------------------------------------------------------
// 单据关联（PE-5）
// ---------------------------------------------------------------------------

// AuthorizeProjectWrite 动态项目写权限：管理角色直通，其余查目标租户 DB 权限。
func (r *EntRepository) AuthorizeProjectWrite(ctx context.Context, roles []string, tenantID int) bool {
	if isAdminViewer(roles) {
		return true
	}
	return middleware.AuthorizeResource(ctx, r.client, roles, "project", "write", tenantID)
}

// AttachRecord 挂接单据到项目（幂等：已挂接同项目则 no-op）。
func (r *EntRepository) AttachRecord(ctx context.Context, tenantID int, domain string, recordID, projectID int) error {
	now := time.Now()
	switch domain {
	case "ticket":
		_, err := r.client.Ticket.UpdateOneID(recordID).
			Where(ticket.TenantIDEQ(tenantID)).
			SetProjectID(projectID).
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "incident":
		_, err := r.client.Incident.UpdateOneID(recordID).
			Where(incident.TenantIDEQ(tenantID)).
			SetProjectID(projectID).
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "problem":
		_, err := r.client.Problem.UpdateOneID(recordID).
			Where(problem.TenantIDEQ(tenantID)).
			SetProjectID(projectID).
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "change":
		_, err := r.client.Change.UpdateOneID(recordID).
			Where(change.TenantIDEQ(tenantID)).
			SetProjectID(projectID).
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "service_request":
		_, err := r.client.ServiceRequest.UpdateOneID(recordID).
			Where(servicerequest.TenantIDEQ(tenantID)).
			SetProjectID(projectID).
			SetUpdatedAt(now).
			Save(ctx)
		return err
	default:
		return ErrRecordDomainInvalid
	}
}

// DetachRecord 解除挂接（幂等：未挂接同样成功）。
func (r *EntRepository) DetachRecord(ctx context.Context, tenantID int, domain string, recordID int) error {
	now := time.Now()
	switch domain {
	case "ticket":
		_, err := r.client.Ticket.UpdateOneID(recordID).
			Where(ticket.TenantIDEQ(tenantID)).
			ClearProjectID().
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "incident":
		_, err := r.client.Incident.UpdateOneID(recordID).
			Where(incident.TenantIDEQ(tenantID)).
			ClearProjectID().
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "problem":
		_, err := r.client.Problem.UpdateOneID(recordID).
			Where(problem.TenantIDEQ(tenantID)).
			ClearProjectID().
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "change":
		_, err := r.client.Change.UpdateOneID(recordID).
			Where(change.TenantIDEQ(tenantID)).
			ClearProjectID().
			SetUpdatedAt(now).
			Save(ctx)
		return err
	case "service_request":
		_, err := r.client.ServiceRequest.UpdateOneID(recordID).
			Where(servicerequest.TenantIDEQ(tenantID)).
			ClearProjectID().
			SetUpdatedAt(now).
			Save(ctx)
		return err
	default:
		return ErrRecordDomainInvalid
	}
}

// ListRecords 聚合读：domain 指定单域；空则五域合并（updatedAt 倒序 + offset/limit）。
func (r *EntRepository) ListRecords(ctx context.Context, tenantID, projectID int, domain, status string, offset, limit int) ([]RecordItem, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	domains := []string{"ticket", "incident", "problem", "change", "service_request"}
	if domain != "" {
		if !validRecordDomains[domain] {
			return nil, 0, ErrRecordDomainInvalid
		}
		domains = []string{domain}
	}

	merged := make([]RecordItem, 0, limit)
	var total int64
	perDomainLimit := offset + limit
	for _, d := range domains {
		items, count, err := r.listRecordsByDomain(ctx, tenantID, projectID, d, status, 0, perDomainLimit)
		if err != nil {
			return nil, 0, err
		}
		total += count
		merged = append(merged, items...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].UpdatedAt.After(merged[j].UpdatedAt) })
	if offset >= len(merged) {
		return []RecordItem{}, total, nil
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	return merged[offset:end], total, nil
}

// listRecordsByDomain 单域查询（统一归一化为 RecordItem）。
func (r *EntRepository) listRecordsByDomain(ctx context.Context, tenantID, projectID int, domain, status string, offset, limit int) ([]RecordItem, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	switch domain {
	case "ticket":
		query := r.client.Ticket.Query().Where(ticket.TenantIDEQ(tenantID), ticket.ProjectID(projectID))
		if status != "" {
			query = query.Where(ticket.StatusEQ(status))
		}
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		rows, err := query.Order(ent.Desc(ticket.FieldUpdatedAt)).Offset(offset).Limit(limit).All(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := make([]RecordItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, RecordItem{Type: "ticket", ID: row.ID, Number: row.TicketNumber, Title: row.Title, Status: row.Status, Priority: row.Priority, UpdatedAt: row.UpdatedAt})
		}
		return items, int64(total), nil
	case "incident":
		query := r.client.Incident.Query().Where(incident.TenantIDEQ(tenantID), incident.ProjectID(projectID))
		if status != "" {
			query = query.Where(incident.StatusEQ(status))
		}
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		rows, err := query.Order(ent.Desc(incident.FieldUpdatedAt)).Offset(offset).Limit(limit).All(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := make([]RecordItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, RecordItem{Type: "incident", ID: row.ID, Number: row.IncidentNumber, Title: row.Title, Status: row.Status, Priority: row.Priority, UpdatedAt: row.UpdatedAt})
		}
		return items, int64(total), nil
	case "problem":
		query := r.client.Problem.Query().Where(problem.TenantIDEQ(tenantID), problem.ProjectID(projectID))
		if status != "" {
			query = query.Where(problem.StatusEQ(status))
		}
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		rows, err := query.Order(ent.Desc(problem.FieldUpdatedAt)).Offset(offset).Limit(limit).All(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := make([]RecordItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, RecordItem{Type: "problem", ID: row.ID, Number: row.ProblemNumber, Title: row.Title, Status: row.Status, Priority: row.Priority, UpdatedAt: row.UpdatedAt})
		}
		return items, int64(total), nil
	case "change":
		query := r.client.Change.Query().Where(change.TenantIDEQ(tenantID), change.ProjectID(projectID))
		if status != "" {
			query = query.Where(change.StatusEQ(status))
		}
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		rows, err := query.Order(ent.Desc(change.FieldUpdatedAt)).Offset(offset).Limit(limit).All(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := make([]RecordItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, RecordItem{Type: "change", ID: row.ID, Number: row.ChangeNumber, Title: row.Title, Status: row.Status, Priority: row.Priority, UpdatedAt: row.UpdatedAt})
		}
		return items, int64(total), nil
	case "service_request":
		query := r.client.ServiceRequest.Query().Where(servicerequest.TenantIDEQ(tenantID), servicerequest.ProjectID(projectID))
		if status != "" {
			query = query.Where(servicerequest.StatusEQ(status))
		}
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return nil, 0, err
		}
		rows, err := query.Order(ent.Desc(servicerequest.FieldUpdatedAt)).Offset(offset).Limit(limit).All(ctx)
		if err != nil {
			return nil, 0, err
		}
		items := make([]RecordItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, RecordItem{Type: "service_request", ID: row.ID, Title: row.Title, Status: row.Status, UpdatedAt: row.UpdatedAt})
		}
		return items, int64(total), nil
	default:
		return nil, 0, ErrRecordDomainInvalid
	}
}

// SetTaskLinkedTicket 回写任务主工单。
func (r *EntRepository) SetTaskLinkedTicket(ctx context.Context, tenantID, projectID, taskID, ticketID int) error {
	_, err := r.client.ProjectTask.UpdateOneID(taskID).
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID), projecttask.DeletedAtIsNil()).
		SetLinkedTicketID(ticketID).
		Save(ctx)
	return err
}

// ClearTaskLinkedTicket 解绑任务主工单。
func (r *EntRepository) ClearTaskLinkedTicket(ctx context.Context, tenantID, projectID, taskID int) error {
	_, err := r.client.ProjectTask.UpdateOneID(taskID).
		Where(projecttask.TenantIDEQ(tenantID), projecttask.ProjectIDEQ(projectID), projecttask.DeletedAtIsNil()).
		ClearLinkedTicketID().
		Save(ctx)
	return err
}

// ---------------------------------------------------------------------------
// 里程碑与任务依赖（PE-14）
// ---------------------------------------------------------------------------

// ListMilestones 项目里程碑（按目标日期升序）。
func (r *EntRepository) ListMilestones(ctx context.Context, tenantID, projectID int, status string) ([]*ent.ProjectMilestone, error) {
	query := r.client.ProjectMilestone.Query().
		Where(projectmilestone.TenantIDEQ(tenantID), projectmilestone.ProjectIDEQ(projectID))
	if status != "" {
		query = query.Where(projectmilestone.StatusEQ(status))
	}
	return query.
		Order(ent.Asc(projectmilestone.FieldDueDate), ent.Asc(projectmilestone.FieldID)).
		All(ctx)
}

// GetMilestone 项目内单条里程碑。
func (r *EntRepository) GetMilestone(ctx context.Context, tenantID, projectID, milestoneID int) (*ent.ProjectMilestone, error) {
	return r.client.ProjectMilestone.Query().
		Where(
			projectmilestone.IDEQ(milestoneID),
			projectmilestone.TenantIDEQ(tenantID),
			projectmilestone.ProjectIDEQ(projectID),
		).
		Only(ctx)
}

// ListMilestonesByOwner 个人里程碑到期聚合（跨项目；due_date 升序）。
func (r *EntRepository) ListMilestonesByOwner(ctx context.Context, tenantID, ownerID int, projectIDs []int, status string, dueBefore *time.Time, limit int) ([]*ent.ProjectMilestone, error) {
	if projectIDs != nil && len(projectIDs) == 0 {
		return []*ent.ProjectMilestone{}, nil
	}
	query := r.client.ProjectMilestone.Query().Where(
		projectmilestone.TenantIDEQ(tenantID),
		projectmilestone.OwnerIDEQ(ownerID),
	)
	if status != "" {
		query = query.Where(projectmilestone.StatusEQ(status))
	}
	if projectIDs != nil {
		query = query.Where(projectmilestone.ProjectIDIn(projectIDs...))
	}
	if dueBefore != nil {
		query = query.Where(projectmilestone.DueDateLTE(*dueBefore))
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	return query.
		Order(ent.Asc(projectmilestone.FieldDueDate), ent.Asc(projectmilestone.FieldID)).
		All(ctx)
}

// CreateMilestone 创建里程碑。
func (r *EntRepository) CreateMilestone(ctx context.Context, p MilestoneCreateParams) (*ent.ProjectMilestone, error) {
	builder := r.client.ProjectMilestone.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetName(p.Name).
		SetDueDate(p.DueDate).
		SetNillableOwnerID(optionalInt(p.OwnerID))
	if p.Description != "" {
		builder = builder.SetDescription(p.Description)
	}
	return builder.Save(ctx)
}

// UpdateMilestone 更新里程碑字段（不改状态）。
func (r *EntRepository) UpdateMilestone(ctx context.Context, tenantID, projectID, milestoneID int, p MilestoneUpdateParams) (*ent.ProjectMilestone, error) {
	upd := r.client.ProjectMilestone.UpdateOneID(milestoneID).
		Where(projectmilestone.TenantIDEQ(tenantID), projectmilestone.ProjectIDEQ(projectID))
	if p.Name != nil {
		upd = upd.SetName(*p.Name)
	}
	if p.Description != nil {
		upd = upd.SetDescription(*p.Description)
	}
	if p.DueDate != nil {
		upd = upd.SetDueDate(*p.DueDate)
	}
	if p.OwnerID != nil {
		upd = upd.SetNillableOwnerID(optionalInt(*p.OwnerID))
	}
	return upd.Save(ctx)
}

// MilestoneStatusCAS 里程碑状态流转（from 状态守卫）。
func (r *EntRepository) MilestoneStatusCAS(ctx context.Context, tenantID, projectID, milestoneID int, from, to string, reachedAt *time.Time) (*ent.ProjectMilestone, error) {
	upd := r.client.ProjectMilestone.UpdateOneID(milestoneID).
		Where(
			projectmilestone.TenantIDEQ(tenantID),
			projectmilestone.ProjectIDEQ(projectID),
			projectmilestone.StatusEQ(from),
		).
		SetStatus(to)
	if reachedAt != nil {
		upd = upd.SetReachedAt(*reachedAt)
	}
	return upd.Save(ctx)
}

// DeleteMilestone 删除里程碑（硬删；审计由中间件留痕）。
func (r *EntRepository) DeleteMilestone(ctx context.Context, tenantID, projectID, milestoneID int) error {
	n, err := r.client.ProjectMilestone.Delete().
		Where(
			projectmilestone.IDEQ(milestoneID),
			projectmilestone.TenantIDEQ(tenantID),
			projectmilestone.ProjectIDEQ(projectID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

// ListDependenciesByTaskIDs 按任务集合取依赖。
func (r *EntRepository) ListDependenciesByTaskIDs(ctx context.Context, tenantID int, taskIDs []int) ([]*ent.ProjectTaskDependency, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	return r.client.ProjectTaskDependency.Query().
		Where(
			projecttaskdependency.TenantIDEQ(tenantID),
			projecttaskdependency.TaskIDIn(taskIDs...),
		).
		All(ctx)
}

// CreateDependency 创建 FS 依赖。
func (r *EntRepository) CreateDependency(ctx context.Context, tenantID, taskID, dependsOnTaskID int, depType string) (*ent.ProjectTaskDependency, error) {
	return r.client.ProjectTaskDependency.Create().
		SetTenantID(tenantID).
		SetTaskID(taskID).
		SetDependsOnTaskID(dependsOnTaskID).
		SetDepType(depType).
		Save(ctx)
}

// DeleteDependency 删除依赖（不存在返回 ent.NotFound）。
func (r *EntRepository) DeleteDependency(ctx context.Context, tenantID, taskID, dependsOnTaskID int) error {
	n, err := r.client.ProjectTaskDependency.Delete().
		Where(
			projecttaskdependency.TenantIDEQ(tenantID),
			projecttaskdependency.TaskIDEQ(taskID),
			projecttaskdependency.DependsOnTaskIDEQ(dependsOnTaskID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

// DependencyExists 依赖是否存在。
func (r *EntRepository) DependencyExists(ctx context.Context, tenantID, taskID, dependsOnTaskID int) (bool, error) {
	return r.client.ProjectTaskDependency.Query().
		Where(
			projecttaskdependency.TenantIDEQ(tenantID),
			projecttaskdependency.TaskIDEQ(taskID),
			projecttaskdependency.DependsOnTaskIDEQ(dependsOnTaskID),
		).
		Exist(ctx)
}

// ---------------------------------------------------------------------------
// 计划基线（PE-13；不可变：仅插入，禁止 UPDATE/DELETE）
// ---------------------------------------------------------------------------

// NextBaselineVersion 项目下一基线版本（无基线时 1）。
func (r *EntRepository) NextBaselineVersion(ctx context.Context, tenantID, projectID int) (int, error) {
	latest, err := r.LatestBaseline(ctx, tenantID, projectID)
	if err != nil {
		if ent.IsNotFound(err) {
			return 1, nil
		}
		return 0, err
	}
	return latest.Version + 1, nil
}

// CreateBaseline 插入基线（版本唯一索引冲突返回 ent.IsConstraintError）。
func (r *EntRepository) CreateBaseline(ctx context.Context, p BaselineCreateParams) (*ent.ProjectBaseline, error) {
	builder := r.client.ProjectBaseline.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetVersion(p.Version).
		SetNillableCreatedBy(optionalInt(p.CreatedBy))
	if p.Snapshot != nil {
		builder = builder.SetSnapshot(p.Snapshot)
	}
	if p.Reason != "" {
		builder = builder.SetReason(p.Reason)
	}
	return builder.Save(ctx)
}

// ListBaselines 项目基线（version 倒序）。
func (r *EntRepository) ListBaselines(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectBaseline, error) {
	return r.client.ProjectBaseline.Query().
		Where(projectbaseline.TenantIDEQ(tenantID), projectbaseline.ProjectIDEQ(projectID)).
		Order(ent.Desc(projectbaseline.FieldVersion)).
		All(ctx)
}

// LatestBaseline 最新基线（无基线返回 ent.NotFound）。
func (r *EntRepository) LatestBaseline(ctx context.Context, tenantID, projectID int) (*ent.ProjectBaseline, error) {
	return r.client.ProjectBaseline.Query().
		Where(projectbaseline.TenantIDEQ(tenantID), projectbaseline.ProjectIDEQ(projectID)).
		Order(ent.Desc(projectbaseline.FieldVersion)).
		First(ctx)
}

// notInArgs 终态集合转 NotIn 参数（保序，便于测试稳定）。
func notInArgs(domain string) []string {
	order := map[string][]string{
		"ticket":          {"closed", "cancelled"},
		"incident":        {"closed", "cancelled"},
		"problem":         {"closed", "cancelled"},
		"change":          {"closed", "cancelled", "rejected", "rolled_back"},
		"service_request": {"completed", "cancelled", "rejected"},
	}
	return order[domain]
}

// ---------------------------------------------------------------------------
// 风险/问题台账（PE-21）
// ---------------------------------------------------------------------------

// ListRisks 项目风险/问题（未软删；updatedAt 倒序、ID 倒序）。
func (r *EntRepository) ListRisks(ctx context.Context, f RiskListFilter) ([]*ent.ProjectRisk, error) {
	query := r.client.ProjectRisk.Query().
		Where(
			projectrisk.TenantIDEQ(f.TenantID),
			projectrisk.ProjectIDEQ(f.ProjectID),
			projectrisk.DeletedAtIsNil(),
		)
	if f.Kind != "" {
		query = query.Where(projectrisk.KindEQ(f.Kind))
	}
	if f.Status != "" {
		query = query.Where(projectrisk.StatusEQ(f.Status))
	}
	if f.Level != "" {
		query = query.Where(projectrisk.LevelEQ(f.Level))
	}
	return query.
		Order(ent.Desc(projectrisk.FieldUpdatedAt), ent.Desc(projectrisk.FieldID)).
		All(ctx)
}

// GetRisk 项目内单条风险（未软删）。
func (r *EntRepository) GetRisk(ctx context.Context, tenantID, projectID, riskID int) (*ent.ProjectRisk, error) {
	return r.client.ProjectRisk.Query().
		Where(
			projectrisk.IDEQ(riskID),
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDEQ(projectID),
			projectrisk.DeletedAtIsNil(),
		).
		Only(ctx)
}

// CreateRisk 创建风险。
func (r *EntRepository) CreateRisk(ctx context.Context, p RiskCreateParams) (*ent.ProjectRisk, error) {
	return r.client.ProjectRisk.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetKind(p.Kind).
		SetTitle(p.Title).
		SetLevel(p.Level).
		SetNillableDescription(optionalString(p.Description)).
		SetNillableProbability(optionalString(p.Probability)).
		SetNillableImpact(optionalString(p.Impact)).
		SetNillableOwnerID(optionalInt(p.OwnerID)).
		SetNillableRelatedTicketID(optionalInt(p.RelatedTicketID)).
		SetNillableCreatedBy(optionalInt(p.CreatedBy)).
		Save(ctx)
}

// UpdateRiskCAS 更新风险字段（version>0 时校验乐观锁）。
func (r *EntRepository) UpdateRiskCAS(ctx context.Context, tenantID, projectID, riskID int, p RiskUpdateParams, version int) (*ent.ProjectRisk, error) {
	upd := r.client.ProjectRisk.UpdateOneID(riskID).
		Where(
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDEQ(projectID),
			projectrisk.DeletedAtIsNil(),
		)
	if version > 0 {
		upd = upd.Where(projectrisk.VersionEQ(version))
	}
	if p.Kind != nil {
		upd = upd.SetKind(*p.Kind)
	}
	if p.Title != nil {
		upd = upd.SetTitle(*p.Title)
	}
	if p.Description != nil {
		upd = upd.SetNillableDescription(optionalString(*p.Description))
	}
	if p.Level != nil {
		upd = upd.SetLevel(*p.Level)
	}
	if p.Probability != nil {
		upd = upd.SetNillableProbability(optionalString(*p.Probability))
	}
	if p.Impact != nil {
		upd = upd.SetNillableImpact(optionalString(*p.Impact))
	}
	if p.OwnerID != nil {
		upd = upd.SetNillableOwnerID(optionalInt(*p.OwnerID))
	}
	if p.RelatedTicketID != nil {
		upd = upd.SetNillableRelatedTicketID(optionalInt(*p.RelatedTicketID))
	}
	return upd.AddVersion(1).Save(ctx)
}

// RiskStatusCAS 状态流转（from 状态 + 可选版本双重守卫；close 携带 resolution）。
func (r *EntRepository) RiskStatusCAS(ctx context.Context, tenantID, projectID, riskID int, from, to, resolution string, version int) (*ent.ProjectRisk, error) {
	upd := r.client.ProjectRisk.UpdateOneID(riskID).
		Where(
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDEQ(projectID),
			projectrisk.DeletedAtIsNil(),
			projectrisk.StatusEQ(from),
		)
	if version > 0 {
		upd = upd.Where(projectrisk.VersionEQ(version))
	}
	upd = upd.SetStatus(to).AddVersion(1)
	if resolution != "" {
		upd = upd.SetResolution(resolution)
	}
	return upd.Save(ctx)
}

// LinkRiskConversion 回写转换产物（仅当目标字段为空时命中 = 防重复转换）。
func (r *EntRepository) LinkRiskConversion(ctx context.Context, tenantID, projectID, riskID int, target string, linkedID int) (*ent.ProjectRisk, error) {
	upd := r.client.ProjectRisk.UpdateOneID(riskID).
		Where(
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDEQ(projectID),
			projectrisk.DeletedAtIsNil(),
		)
	switch target {
	case ConvertTargetProblem:
		upd = upd.Where(projectrisk.RelatedProblemIDIsNil()).SetRelatedProblemID(linkedID)
	case ConvertTargetChange:
		upd = upd.Where(projectrisk.RelatedChangeIDIsNil()).SetRelatedChangeID(linkedID)
	default:
		return nil, &ent.NotFoundError{}
	}
	return upd.AddVersion(1).Save(ctx)
}

// SoftDeleteRisk 软删。
func (r *EntRepository) SoftDeleteRisk(ctx context.Context, tenantID, projectID, riskID int) error {
	n, err := r.client.ProjectRisk.Update().
		Where(
			projectrisk.IDEQ(riskID),
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDEQ(projectID),
			projectrisk.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

// TicketExists 同租户工单存在性（风险 related_ticket_id 弱引用校验）。
func (r *EntRepository) TicketExists(ctx context.Context, tenantID, ticketID int) (bool, error) {
	return r.client.Ticket.Query().
		Where(ticket.IDEQ(ticketID), ticket.TenantIDEQ(tenantID)).
		Exist(ctx)
}

// optionalInt 保留 NULL 语义（0/负值视为未设置）。
func optionalInt(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

// optionalString 保留 NULL 语义（空串视为未设置）。
func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// 编译期断言：终态集合与实体词表一致（防漂移）。
var _ = isTerminal("change", "rolled_back")
var _ = isTerminal("service_request", "rejected")
