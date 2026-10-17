package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectTemplate holds the schema definition for the ProjectTemplate entity（PE-11）。
//
// 权威定义：docs/project-management/plan/p1-template-design.md。
// 说明：模板 = 可复用的交付方法论（阶段/任务/检查单/里程碑），
// 应用（apply）时经事务一键生成项目计划；仅 active 模板可应用。
// 命名消歧：与 workflow_templates/ticket_types preset 同层但不复用其表（域切片自治）。
type ProjectTemplate struct {
	ent.Schema
}

// Fields of the ProjectTemplate.
func (ProjectTemplate) Fields() []ent.Field {
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
		field.String("type").
			Comment("适用项目类型: internal|improvement|delivery").
			Default("internal").
			MaxLen(16),
		field.String("status").
			Comment("状态: draft|active|archived（仅 active 可应用）").
			Default("draft").
			MaxLen(16),
		field.JSON("payload", map[string]any{}).
			Comment("模板定义：{milestones:[{name,offsetDays}], tasks:[{stage,name,priority,startOffsetDays,durationDays,estimateHours,checklist[]}]}").
			Optional(),
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

// Indexes of the ProjectTemplate.
func (ProjectTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").
			Unique().
			StorageKey("uq_project_templates_tenant_name"),
		index.Fields("tenant_id", "status").StorageKey("idx_project_templates_tenant_status"),
	}
}
