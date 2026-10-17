package project

import (
	"context"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projecttemplate"
)

// 项目模板数据访问（PE-11）。权威定义：docs/project-management/plan/p1-template-design.md。

// ListTemplates 模板列表 + 总数（updatedAt 倒序；软删不可见）。
func (r *EntRepository) ListTemplates(ctx context.Context, f TemplateListFilter) ([]*ent.ProjectTemplate, int64, error) {
	query := r.client.ProjectTemplate.Query().
		Where(projecttemplate.TenantIDEQ(f.TenantID), projecttemplate.DeletedAtIsNil())
	if f.Status != "" {
		query = query.Where(projecttemplate.StatusEQ(f.Status))
	}
	if f.Type != "" {
		query = query.Where(projecttemplate.TypeEQ(f.Type))
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		query = query.Where(projecttemplate.Or(
			projecttemplate.NameContainsFold(kw),
			projecttemplate.DescriptionContainsFold(kw),
		))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.
		Order(ent.Desc(projecttemplate.FieldUpdatedAt), ent.Desc(projecttemplate.FieldID)).
		Offset(f.Offset).
		Limit(f.Limit).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, int64(total), nil
}

// GetTemplate 按租户取模板（软删不可见）。
func (r *EntRepository) GetTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectTemplate, error) {
	return r.client.ProjectTemplate.Query().
		Where(projecttemplate.IDEQ(id), projecttemplate.TenantIDEQ(tenantID), projecttemplate.DeletedAtIsNil()).
		Only(ctx)
}

// CreateTemplate 创建模板。
func (r *EntRepository) CreateTemplate(ctx context.Context, p TemplateCreateParams) (*ent.ProjectTemplate, error) {
	builder := r.client.ProjectTemplate.Create().
		SetTenantID(p.TenantID).
		SetName(p.Name).
		SetType(p.Type).
		SetStatus(p.Status).
		SetPayload(p.Payload).
		SetNillableCreatedBy(optionalInt(p.CreatedBy))
	if p.Description != "" {
		builder = builder.SetDescription(p.Description)
	}
	return builder.Save(ctx)
}

// UpdateTemplateCAS 更新模板（version>0 时校验乐观锁；未命中返回 ent.NotFound）。
func (r *EntRepository) UpdateTemplateCAS(ctx context.Context, tenantID, id int, p TemplateUpdateParams, version int) (*ent.ProjectTemplate, error) {
	upd := r.client.ProjectTemplate.Update().
		Where(projecttemplate.IDEQ(id), projecttemplate.TenantIDEQ(tenantID), projecttemplate.DeletedAtIsNil())
	if version > 0 {
		upd = upd.Where(projecttemplate.VersionEQ(version))
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
	if p.Status != nil {
		upd = upd.SetStatus(*p.Status)
	}
	if p.Payload != nil {
		upd = upd.SetPayload(p.Payload)
	}
	affected, err := upd.AddVersion(1).Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, &ent.NotFoundError{}
	}
	return r.GetTemplate(ctx, tenantID, id)
}

// SoftDeleteTemplate 软删除（软删不释放名称唯一性，与项目 code 先例一致）。
func (r *EntRepository) SoftDeleteTemplate(ctx context.Context, tenantID, id int) error {
	affected, err := r.client.ProjectTemplate.Update().
		Where(projecttemplate.IDEQ(id), projecttemplate.TenantIDEQ(tenantID), projecttemplate.DeletedAtIsNil()).
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

// TemplateNameExists 租户内名称占用（excludeID>0 时排除自身）。
func (r *EntRepository) TemplateNameExists(ctx context.Context, tenantID int, name string, excludeID int) (bool, error) {
	query := r.client.ProjectTemplate.Query().
		Where(projecttemplate.TenantIDEQ(tenantID), projecttemplate.NameEQ(name), projecttemplate.DeletedAtIsNil())
	if excludeID > 0 {
		query = query.Where(projecttemplate.IDNEQ(excludeID))
	}
	return query.Exist(ctx)
}

// TemplateAppliedProjectCount 模板被应用的项目数（删除门禁）。
func (r *EntRepository) TemplateAppliedProjectCount(ctx context.Context, tenantID, templateID int) (int, error) {
	return r.client.Project.Query().
		Where(project.TenantIDEQ(tenantID), project.TemplateIDEQ(templateID), project.DeletedAtIsNil()).
		Count(ctx)
}

// ApplyTemplateTx 事务内一键生成：CAS 认领 projects.template_id（幂等闸门）→ 阶段/任务/检查项/里程碑。
//
// 幂等语义：认领失败（已被应用）立即返回 ErrTemplateAlreadyApplied，不产生任何新数据；
// 生成过程任何一步失败整体回滚（含认领），不会留下半成品项目。
func (r *EntRepository) ApplyTemplateTx(ctx context.Context, p TemplateApplyParams) (*TemplateApplyOutcome, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	claimed, err := tx.Project.Update().
		Where(
			project.IDEQ(p.ProjectID),
			project.TenantIDEQ(p.TenantID),
			project.DeletedAtIsNil(),
			project.TemplateIDIsNil(),
		).
		SetTemplateID(p.TemplateID).
		SetTemplateAppliedAt(p.AppliedAt).
		SetTemplateAppliedBy(p.AppliedBy).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if claimed == 0 {
		return nil, ErrTemplateAlreadyApplied
	}

	outcome := &TemplateApplyOutcome{}
	for i, stage := range p.Stages {
		parentID := 0
		if stage.Name != "" {
			stageTask, err := tx.ProjectTask.Create().
				SetTenantID(p.TenantID).
				SetProjectID(p.ProjectID).
				SetName(stage.Name).
				SetSortOrder(i).
				SetNillableCreatedBy(optionalInt(p.AppliedBy)).
				Save(ctx)
			if err != nil {
				return nil, err
			}
			parentID = stageTask.ID
			outcome.StageCount++
		}
		for j, t := range stage.Tasks {
			task, err := createTemplateTask(ctx, tx, p, parentID, j, t)
			if err != nil {
				return nil, err
			}
			outcome.TaskCount++
			for k, item := range t.Checklist {
				if _, err := createTemplateTask(ctx, tx, p, task.ID, k, TemplateTaskPlan{Name: item}); err != nil {
					return nil, err
				}
				outcome.ChecklistCount++
			}
		}
	}
	for _, m := range p.Milestones {
		if _, err := tx.ProjectMilestone.Create().
			SetTenantID(p.TenantID).
			SetProjectID(p.ProjectID).
			SetName(m.Name).
			SetDueDate(m.DueDate).
			Save(ctx); err != nil {
			return nil, err
		}
		outcome.MilestoneCount++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return outcome, nil
}

// createTemplateTask 事务内建任务（parentID=0 → 项目根；检查项复用本函数）。
func createTemplateTask(ctx context.Context, tx *ent.Tx, p TemplateApplyParams, parentID, sortOrder int, t TemplateTaskPlan) (*ent.ProjectTask, error) {
	builder := tx.ProjectTask.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetName(t.Name).
		SetPriority(templatePlanPriority(t.Priority)).
		SetSortOrder(sortOrder).
		SetNillableParentTaskID(optionalInt(parentID)).
		SetNillablePlanStart(t.PlanStart).
		SetNillablePlanEnd(t.PlanEnd).
		SetNillableEstimateHours(t.EstimateHours).
		SetNillableCreatedBy(optionalInt(p.AppliedBy))
	if t.Description != "" {
		builder = builder.SetDescription(t.Description)
	}
	return builder.Save(ctx)
}

func templatePlanPriority(priority string) string {
	if p := strings.TrimSpace(priority); p != "" && validTaskPriorities[p] {
		return p
	}
	return "medium"
}
