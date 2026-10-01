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
			Comment("状态: active, completed, suspended").
			Default("active"),
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
