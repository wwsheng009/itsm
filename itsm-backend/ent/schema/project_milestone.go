package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectMilestone holds the schema definition for the ProjectMilestone entity（P1，PE-14）。
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §4.4。
// 语义：里程碑是零工期检查点；reached 时回填 reached_at（服务层写）。
// 说明：不声明 ent edges/外键（同 project_member 先例；索引由迁移兜底）。
type ProjectMilestone struct {
	ent.Schema
}

// Fields of the ProjectMilestone.
func (ProjectMilestone) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.String("name").
			Comment("里程碑名称").
			NotEmpty().
			MaxLen(255),
		field.Text("description").
			Comment("说明").
			Optional(),
		field.Time("due_date").
			Comment("目标日期"),
		field.String("status").
			Comment("状态: planned|reached|missed").
			Default("planned").
			MaxLen(16),
		field.Int("owner_id").
			Comment("负责人ID（须为项目活跃成员）").
			Optional(),
		field.Time("reached_at").
			Comment("达成时间（status=reached 时回填）").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectMilestone.
func (ProjectMilestone) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "status").StorageKey("idx_project_milestones"),
	}
}