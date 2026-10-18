package project

import (
	"context"

	"itsm-backend/ent"
	"itsm-backend/ent/projectbaseline"
	"itsm-backend/ent/projectrisk"
)

// ---------------------------------------------------------------------------
// PE-27 健康度事实源批量取数（D-58：列表/组合/分组一次批量装载，避免 N+1）
// 权威设计：docs/project-management/plan/p3-health-design.md §5
// ---------------------------------------------------------------------------

// ListRisksByProjects 批量风险/问题（跨项目；未软删；口径同 ListRisks 无过滤视图）。
func (r *EntRepository) ListRisksByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectRisk, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectRisk{}, nil
	}
	return r.client.ProjectRisk.Query().
		Where(
			projectrisk.TenantIDEQ(tenantID),
			projectrisk.ProjectIDIn(projectIDs...),
			projectrisk.DeletedAtIsNil(),
		).
		Order(ent.Asc(projectrisk.FieldProjectID), ent.Asc(projectrisk.FieldID)).
		All(ctx)
}

// ListBaselinesByProjects 批量基线（跨项目；version 升序）。
func (r *EntRepository) ListBaselinesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectBaseline, error) {
	if len(projectIDs) == 0 {
		return []*ent.ProjectBaseline{}, nil
	}
	return r.client.ProjectBaseline.Query().
		Where(
			projectbaseline.TenantIDEQ(tenantID),
			projectbaseline.ProjectIDIn(projectIDs...),
		).
		Order(ent.Asc(projectbaseline.FieldProjectID), ent.Asc(projectbaseline.FieldVersion)).
		All(ctx)
}
