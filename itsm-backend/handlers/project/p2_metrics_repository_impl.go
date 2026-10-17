package project

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectgate"
	"itsm-backend/ent/projectmember"
	"itsm-backend/ent/projectmilestone"
	"itsm-backend/ent/projecttask"
)

// ---------------------------------------------------------------------------
// P2 PE-24 组合视图与度量：跨项目批量取数（避免 N+1；口径与列表/热图同源）
// 权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md（D-40~D-43）
// ---------------------------------------------------------------------------

// ListForAggregate 组合视图取数：与 List 同谓词（D-40 口径同源）、不分页；
// id 降序与列表一致；limit<=0 时按 PortfolioMaxProjects 截断（截断标记由服务层给）。
func (r *EntRepository) ListForAggregate(ctx context.Context, f ListFilter, limit int) ([]*ent.Project, error) {
	if f.ProjectIDs != nil && len(f.ProjectIDs) == 0 {
		return []*ent.Project{}, nil
	}
	if limit <= 0 {
		limit = PortfolioMaxProjects
	}
	return r.listQuery(f).
		Order(ent.Desc(project.FieldID)).
		Limit(limit).
		All(ctx)
}

// ListMilestonesByProjects 批量里程碑（projectIDs 为空 = 空集，不发全表查询）。
func (r *EntRepository) ListMilestonesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectMilestone, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectMilestone{}, nil
	}
	return r.client.ProjectMilestone.Query().
		Where(projectmilestone.TenantIDEQ(tenantID), projectmilestone.ProjectIDIn(projectIDs...)).
		Order(ent.Asc(projectmilestone.FieldDueDate), ent.Asc(projectmilestone.FieldID)).
		All(ctx)
}

// ListGatesByProjects 批量阶段门（gate 升序，与单项目列表同序）。
func (r *EntRepository) ListGatesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectGate, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectGate{}, nil
	}
	return r.client.ProjectGate.Query().
		Where(projectgate.TenantIDEQ(tenantID), projectgate.ProjectIDIn(projectIDs...)).
		Order(ent.Asc(projectgate.FieldProjectID), ent.Asc(projectgate.FieldGate)).
		All(ctx)
}

// ListTasksByProjects 批量任务（未删；口径同 ListTasks）。
func (r *EntRepository) ListTasksByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectTask, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectTask{}, nil
	}
	return r.client.ProjectTask.Query().
		Where(
			projecttask.TenantIDEQ(tenantID),
			projecttask.ProjectIDIn(projectIDs...),
			projecttask.DeletedAtIsNil(),
		).
		Order(ent.Asc(projecttask.FieldProjectID), ent.Asc(projecttask.FieldSortOrder), ent.Asc(projecttask.FieldID)).
		All(ctx)
}

// ListMembersByProjects 批量成员（含已移除行；口径同 ListMembers）。
func (r *EntRepository) ListMembersByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectMember, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectMember{}, nil
	}
	return r.client.ProjectMember.Query().
		Where(projectmember.TenantIDEQ(tenantID), projectmember.ProjectIDIn(projectIDs...)).
		Order(ent.Asc(projectmember.FieldProjectID), ent.Asc(projectmember.FieldJoinedAt)).
		All(ctx)
}