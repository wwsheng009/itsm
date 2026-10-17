package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Project holds the schema definition for the Project entity.
type Project struct {
	ent.Schema
}

// Fields of the Project.
func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Comment("项目名称").
			NotEmpty(),
		field.String("code").
			Comment("项目代码").
			NotEmpty(),
		field.Text("description").
			Comment("项目描述").
			Optional(),
		field.Int("manager_id").
			Comment("项目经理ID").
			Optional(),
		field.Int("department_id").
			Comment("所属部门ID").
			Optional(),
		field.Time("start_date").
			Comment("开始日期").
			Optional(),
		field.Time("end_date").
			Comment("结束日期").
			Optional(),
		field.String("status").
			Comment("状态: planning|active|suspended|completed|cancelled（P0 受控；存量映射见 docs/project-management/plan）").
			Default("active"),
		// ---- P0 项目管理扩展（权威定义：docs/project-management/plan/p0-data-ddl.md §2）----
		field.String("type").
			Comment("项目类型: internal|improvement|delivery（P0）").
			Default("internal").
			MaxLen(16),
		field.Int("parent_project_id").
			Comment("父子项目（P0）").
			Optional(),
		field.String("health").
			Comment("健康度: green|yellow|red（P0）").
			Default("green").
			MaxLen(8),
		field.Int("progress").
			Comment("进度 0-100（P0）").
			Default(0).
			Range(0, 100),
		field.Time("actual_start").
			Comment("实际开始时间（P0）").
			Optional(),
		field.Time("actual_end").
			Comment("实际结束时间（P0）").
			Optional(),
		field.Time("closed_at").
			Comment("结项时间（P0）").
			Optional(),
		field.Int("owner_team_id").
			Comment("团队归口（P0，集成契约 §1-2）").
			Optional(),
		// ---- P1 模板应用（PE-11；权威定义：docs/project-management/plan/p1-template-design.md）----
		field.Int("template_id").
			Comment("已应用的项目模板ID（PE-11；非空即已应用，幂等闸门）").
			Optional(),
		field.Time("template_applied_at").
			Comment("模板应用时间（PE-11）").
			Optional(),
		field.Int("template_applied_by").
			Comment("模板应用人ID（PE-11）").
			Optional(),
		// ---- P2 轻量预算（PE-22；权威定义：docs/project-management/plan/p2-budget-hours-design.md §4）----
		field.Float("budget_amount").
			Comment("项目预算金额（P2 轻量预算；不做财务级核算）").
			Optional().
			Nillable(),
		field.String("budget_currency").
			Comment("预算币种（P2；空=租户默认）").
			Optional().
			Nillable().
			MaxLen(8),
		// ---- P2 周容量默认（PE-23；权威定义：docs/project-management/plan/p2-resource-heatmap-design.md §3）----
		field.Float("default_weekly_capacity_hours").
			Comment("项目周容量默认（小时；P2 资源热图；NULL=内置 40h）").
			Optional().
			Nillable(),
		field.Int("version").
			Comment("乐观锁版本（P0）").
			Default(1).
			Positive(),
		field.Time("deleted_at").
			Comment("软删除时间（P0；DELETE 语义）").
			Optional().
			Nillable(),
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the Project（IP-P1-3b：代码唯一性收敛到租户内，替换全局唯一）。
func (Project) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").
			Unique().
			StorageKey("uq_project_tenant_code"),
		index.Fields("tenant_id", "status").StorageKey("idx_projects_tenant_status"),
		index.Fields("tenant_id", "type").StorageKey("idx_projects_tenant_type"),
		index.Fields("tenant_id", "owner_team_id").StorageKey("idx_projects_owner_team"),
		index.Fields("tenant_id", "manager_id").StorageKey("idx_projects_tenant_manager"),
	}
}

// Edges of the Project.
func (Project) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("department", Department.Type).
			Ref("projects").
			Field("department_id").
			Unique(),
		edge.To("applications", Application.Type).
			Comment("包含的应用"),
		edge.To("tags", Tag.Type).
			Comment("项目标签"),
	}
}
