package project

import (
	"context"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectreporttemplate"
	"itsm-backend/ent/team"
)

// PE-17 归口与报告数据访问。权威定义：docs/project-management/plan/p1-report-design.md。

// GetTeamRef 归口团队引用（含软删；同租户；不存在返回 ent.IsNotFound）。
func (r *EntRepository) GetTeamRef(ctx context.Context, tenantID, teamID int) (*TeamRef, error) {
	entity, err := r.client.Team.Query().
		Where(team.IDEQ(teamID), team.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	ref := teamRefFromEntity(entity)
	return &ref, nil
}

// ListTeamRefs 批量归口团队引用（缺失 ID 不出现在 map）。
func (r *EntRepository) ListTeamRefs(ctx context.Context, tenantID int, teamIDs []int) (map[int]TeamRef, error) {
	out := make(map[int]TeamRef, len(teamIDs))
	if len(teamIDs) == 0 {
		return out, nil
	}
	entities, err := r.client.Team.Query().
		Where(team.TenantIDEQ(tenantID), team.IDIn(teamIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, entity := range entities {
		out[entity.ID] = teamRefFromEntity(entity)
	}
	return out, nil
}

func teamRefFromEntity(entity *ent.Team) TeamRef {
	deleted := entity.DeletedAt != nil && !entity.DeletedAt.IsZero()
	return TeamRef{ID: entity.ID, Name: entity.Name, Code: entity.Code, Status: entity.Status, Deleted: deleted}
}

// ListActiveTeamRefs 归口候选（A2：项目「归口」维护入口的取数来源）。
// 只返回同租户、未软删且 status=active 的团队；keyword 匹配名称/代码。
func (r *EntRepository) ListActiveTeamRefs(
	ctx context.Context, tenantID int, keyword string, limit int,
) ([]TeamRef, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	query := r.client.Team.Query().
		Where(team.TenantIDEQ(tenantID), team.DeletedAtIsNil(), team.StatusEQ("active"))
	if kw := strings.TrimSpace(keyword); kw != "" {
		query = query.Where(team.Or(team.NameContainsFold(kw), team.CodeContainsFold(kw)))
	}
	entities, err := query.Order(ent.Asc(team.FieldName)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TeamRef, 0, len(entities))
	for _, entity := range entities {
		out = append(out, teamRefFromEntity(entity))
	}
	return out, nil
}

// ListReportTemplates 报告模板列表 + 总数（软删不可见；updatedAt 倒序）。
func (r *EntRepository) ListReportTemplates(ctx context.Context, f ReportTemplateListFilter) ([]*ent.ProjectReportTemplate, int64, error) {
	query := r.client.ProjectReportTemplate.Query().
		Where(projectreporttemplate.TenantIDEQ(f.TenantID), projectreporttemplate.DeletedAtIsNil())
	if f.Status != "" {
		query = query.Where(projectreporttemplate.StatusEQ(f.Status))
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		query = query.Where(projectreporttemplate.Or(
			projectreporttemplate.NameContainsFold(kw),
			projectreporttemplate.DescriptionContainsFold(kw),
		))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Order(ent.Desc(projectreporttemplate.FieldUpdatedAt), ent.Desc(projectreporttemplate.FieldID)).
		Offset(f.Offset).
		Limit(f.Limit).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, int64(total), nil
}

// GetReportTemplate 取报告模板（软删不可见）。
func (r *EntRepository) GetReportTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectReportTemplate, error) {
	return r.client.ProjectReportTemplate.Query().
		Where(
			projectreporttemplate.IDEQ(id),
			projectreporttemplate.TenantIDEQ(tenantID),
			projectreporttemplate.DeletedAtIsNil(),
		).
		Only(ctx)
}

// CreateReportTemplate 创建报告模板。
func (r *EntRepository) CreateReportTemplate(ctx context.Context, p ReportTemplateCreateParams) (*ent.ProjectReportTemplate, error) {
	builder := r.client.ProjectReportTemplate.Create().
		SetTenantID(p.TenantID).
		SetName(p.Name).
		SetCadence(p.Cadence).
		SetSections(p.Sections).
		SetStatus(p.Status).
		SetNillableCreatedBy(optionalInt(p.CreatedBy))
	if p.Description != "" {
		builder = builder.SetDescription(p.Description)
	}
	return builder.Save(ctx)
}

// UpdateReportTemplateCAS 更新报告模板（version>0 校验乐观锁；未命中返回 ent.NotFound）。
func (r *EntRepository) UpdateReportTemplateCAS(ctx context.Context, tenantID, id int, p ReportTemplateUpdateParams, version int) (*ent.ProjectReportTemplate, error) {
	upd := r.client.ProjectReportTemplate.Update().
		Where(
			projectreporttemplate.IDEQ(id),
			projectreporttemplate.TenantIDEQ(tenantID),
			projectreporttemplate.DeletedAtIsNil(),
		)
	if version > 0 {
		upd = upd.Where(projectreporttemplate.VersionEQ(version))
	}
	if p.Name != nil {
		upd = upd.SetName(*p.Name)
	}
	if p.Description != nil {
		upd = upd.SetDescription(*p.Description)
	}
	if p.Cadence != nil {
		upd = upd.SetCadence(*p.Cadence)
	}
	if p.Sections != nil {
		upd = upd.SetSections(*p.Sections)
	}
	if p.Status != nil {
		upd = upd.SetStatus(*p.Status)
	}
	affected, err := upd.AddVersion(1).Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, &ent.NotFoundError{}
	}
	return r.GetReportTemplate(ctx, tenantID, id)
}

// SoftDeleteReportTemplate 软删除（软删不释放名称唯一性）。
func (r *EntRepository) SoftDeleteReportTemplate(ctx context.Context, tenantID, id int) error {
	affected, err := r.client.ProjectReportTemplate.Update().
		Where(
			projectreporttemplate.IDEQ(id),
			projectreporttemplate.TenantIDEQ(tenantID),
			projectreporttemplate.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

// ReportTemplateNameExists 报告模板名占用（excludeID>0 时排除自身）。
func (r *EntRepository) ReportTemplateNameExists(ctx context.Context, tenantID int, name string, excludeID int) (bool, error) {
	query := r.client.ProjectReportTemplate.Query().
		Where(
			projectreporttemplate.TenantIDEQ(tenantID),
			projectreporttemplate.NameEQ(name),
			projectreporttemplate.DeletedAtIsNil(),
		)
	if excludeID > 0 {
		query = query.Where(projectreporttemplate.IDNEQ(excludeID))
	}
	return query.Exist(ctx)
}

// ListProjectsForReportGroups 报表分组取数：轻量列（健康度由服务层派生，PE-27 D-58）。
func (r *EntRepository) ListProjectsForReportGroups(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.Project, error) {
	query := r.client.Project.Query().
		Where(project.TenantIDEQ(tenantID), project.DeletedAtIsNil())
	if projectIDs != nil {
		if len(projectIDs) == 0 {
			return []*ent.Project{}, nil
		}
		query = query.Where(project.IDIn(projectIDs...))
	}
	// 说明：ent 的 GroupBy+Scan 对多词列名（owner_team_id）不做 struct 字段映射，
	// 这里用 Select 轻量列（健康度派生所需最小字段集，健康度在服务层派生）。
	return query.
		Select(
			project.FieldID, project.FieldOwnerTeamID, project.FieldStatus, project.FieldProgress,
			project.FieldStartDate, project.FieldEndDate, project.FieldBudgetAmount, project.FieldBudgetCurrency,
			// A2（回归遗留）：部门维度需要参与分组，必须取列（否则恒为 0 → 全部落「未设置」）。
			project.FieldDepartmentID,
		).
		All(ctx)
}
