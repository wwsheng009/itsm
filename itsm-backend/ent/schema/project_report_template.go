package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectReportTemplate holds the schema definition for the ProjectReportTemplate entity（PE-17）。
//
// 权威定义：docs/project-management/plan/p1-report-design.md。
// 语义：状态报告"模板 + 周期"数据驱动——模板定义输出章节（sections）与周期（cadence），
// 报告正文由项目事实数据实时派生（先导出 Markdown/JSON，AI 增强后置 P2），不落报告快照。
// 命名消歧：与 project_templates（交付计划模板）分表——计划模板生成计划，报告模板生成报告。
type ProjectReportTemplate struct {
	ent.Schema
}

// Fields of the ProjectReportTemplate.
func (ProjectReportTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.String("name").
			Comment("模板名称（租户内唯一，软删不释放）").
			NotEmpty().
			MaxLen(128),
		field.Text("description").
			Comment("模板描述").
			Optional(),
		field.String("cadence").
			Comment("报告周期: weekly|biweekly|monthly").
			Default("weekly").
			MaxLen(16),
		field.JSON("sections", []string{}).
			Comment("输出章节: overview|tasks|milestones|gates|variance|team").
			Optional(),
		field.String("status").
			Comment("状态: draft|active|archived（仅 active 可生成）").
			Default("draft").
			MaxLen(16),
		field.Int("version").
			Comment("乐观锁版本").
			Default(1).
			Positive(),
		field.Int("created_by").
			Comment("创建人ID").
			Optional(),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Time("deleted_at").
			Comment("软删除时间").
			Optional().
			Nillable(),
	}
}

// Indexes of the ProjectReportTemplate.
func (ProjectReportTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").
			Unique().
			StorageKey("uq_project_report_templates_tenant_name"),
		index.Fields("tenant_id", "status").StorageKey("idx_project_report_templates_tenant_status"),
	}
}
