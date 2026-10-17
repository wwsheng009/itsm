package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectRisk holds the schema definition for the ProjectRisk entity（P2，PE-21）。
//
// 权威定义：docs/project-management/plan/p2-risk-design.md §3（上游 02 §3.5）。
// 语义：风险/问题台账（kind=risk|issue）；可转问题单/变更（related_problem_id/related_change_id 回写）。
// 说明：不声明 ent edges/外键（同 project_milestone 先例；索引由迁移兜底）。
type ProjectRisk struct {
	ent.Schema
}

// Fields of the ProjectRisk.
func (ProjectRisk) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.String("kind").
			Comment("类型: risk|issue").
			Default("risk").
			MaxLen(16),
		field.String("title").
			Comment("标题").
			NotEmpty().
			MaxLen(255),
		field.Text("description").
			Comment("描述").
			Optional(),
		field.String("level").
			Comment("等级: low|medium|high").
			Default("medium").
			MaxLen(16),
		field.String("probability").
			Comment("概率: low|medium|high（可空）").
			Optional().
			MaxLen(16),
		field.String("impact").
			Comment("影响: low|medium|high（可空）").
			Optional().
			MaxLen(16),
		field.Int("owner_id").
			Comment("负责人ID（须为项目活跃成员；0=未指派）").
			Optional(),
		field.String("status").
			Comment("状态: open|mitigating|closed").
			Default("open").
			MaxLen(16),
		field.Text("resolution").
			Comment("结论/处置说明（close 时必填）").
			Optional(),
		field.Int("related_ticket_id").
			Comment("关联工单ID（同租户弱引用，可空）").
			Optional(),
		field.Int("related_problem_id").
			Comment("转换产物：问题单ID（可空）").
			Optional(),
		field.Int("related_change_id").
			Comment("转换产物：变更单ID（可空）").
			Optional(),
		field.Int("created_by").
			Comment("创建人ID").
			Optional(),
		field.Int("version").
			Comment("乐观锁版本").
			Default(1).
			Positive(),
		field.Time("deleted_at").
			Comment("软删除时间（DELETE 语义）").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectRisk.
func (ProjectRisk) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "status").StorageKey("idx_project_risks_tenant_project_status"),
		index.Fields("tenant_id", "project_id", "level").StorageKey("idx_project_risks_tenant_project_level"),
	}
}