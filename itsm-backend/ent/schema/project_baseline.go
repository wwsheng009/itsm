package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectBaseline holds the schema definition for the ProjectBaseline entity（P1，PE-13）。
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §3.7。
// **不可变**：服务层禁止 UPDATE/DELETE；基线变更 = 插入新 version（配合项目内变更日志）。
// snapshot 冻结内容：任务计划日期/里程碑/预算/范围说明引用（JSONB）。
type ProjectBaseline struct {
	ent.Schema
}

// Fields of the ProjectBaseline.
func (ProjectBaseline) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.Int("version").
			Comment("项目内基线版本（1,2,3…）").
			Positive(),
		field.JSON("snapshot", map[string]any{}).
			Comment("冻结快照：任务计划/里程碑/预算等").
			Optional(),
		field.String("reason").
			Comment("基线变更原因（项目内变更日志）").
			Optional().
			MaxLen(512),
		field.Int("created_by").
			Comment("创建人ID").
			Optional(),
		field.Time("created_at").
			Default(time.Now),
	}
}

// Indexes of the ProjectBaseline.
func (ProjectBaseline) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "version").
			Unique().
			StorageKey("uk_project_baselines"),
	}
}