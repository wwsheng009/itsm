package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectTaskDependency holds the schema definition for the ProjectTaskDependency entity（P1，PE-14）。
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §4.3。
// v1 仅支持 FS（finish-to-start）；SS/FF 与关键路径后置（见 07 §2）。
// 环检测与同项目校验由服务层负责（本表不建外键/edges）。
type ProjectTaskDependency struct {
	ent.Schema
}

// Fields of the ProjectTaskDependency.
func (ProjectTaskDependency) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("task_id").
			Comment("任务ID（后继）").
			Positive(),
		field.Int("depends_on_task_id").
			Comment("前置任务ID").
			Positive(),
		field.String("dep_type").
			Comment("依赖类型: fs（v1 仅 FS）").
			Default("fs").
			MaxLen(8),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectTaskDependency.
func (ProjectTaskDependency) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "depends_on_task_id").
			Unique().
			StorageKey("uk_project_task_deps"),
		index.Fields("depends_on_task_id").StorageKey("idx_project_task_deps_reverse"),
	}
}