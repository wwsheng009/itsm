package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectTask holds the schema definition for the ProjectTask entity（P0）。
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §4.2。
// 命名消歧（01 §5）：与 ProcessTask（BPMN 流程任务）、ProvisioningTask（开通任务）区分。
// 约束：WBS 深度 ≤3 层（parent_task_id 链路，服务层校验）；assignee 必须是活跃 membership。
// 说明：不声明 ent edges/外键（同 attachment 先例；partial 索引由迁移兜底）。
type ProjectTask struct {
	ent.Schema
}

// Fields of the ProjectTask.
func (ProjectTask) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.Int("parent_task_id").
			Comment("父任务ID（WBS ≤3 层）").
			Optional(),
		field.String("name").
			Comment("任务名称").
			NotEmpty().
			MaxLen(255),
		field.Text("description").
			Comment("任务描述").
			Optional(),
		field.Int("assignee_id").
			Comment("负责人ID（活跃 membership 才可指派）").
			Optional(),
		field.String("status").
			Comment("状态: todo|in_progress|blocked|done|cancelled").
			Default("todo").
			MaxLen(16),
		field.String("priority").
			Comment("优先级: low|medium|high|critical（对齐 common/validator.go 词表）").
			Default("medium").
			MaxLen(16),
		field.Time("plan_start").
			Comment("计划开始时间").
			Optional(),
		field.Time("plan_end").
			Comment("计划结束时间").
			Optional(),
		field.Time("actual_start").
			Comment("实际开始时间").
			Optional(),
		field.Time("actual_end").
			Comment("实际结束时间").
			Optional(),
		field.Int("progress").
			Comment("进度 0-100").
			Default(0).
			Range(0, 100),
		field.Float("estimate_hours").
			Comment("估算工时（P2 展示）").
			Optional(),
		field.Float("actual_hours").
			Comment("实际工时（P2 落数；worklog 后置评估）").
			Optional(),
		field.Int("linked_ticket_id").
			Comment("任务生成的主工单ID（P0 一任务一主工单；解绑置空，见集成契约 §3.2）").
			Optional(),
		field.Int("sort_order").
			Comment("排序").
			Default(0),
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

// Indexes of the ProjectTask.
func (ProjectTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "status").StorageKey("idx_project_tasks_project"),
		index.Fields("tenant_id", "assignee_id", "status").StorageKey("idx_project_tasks_assignee"),
		index.Fields("parent_task_id").StorageKey("idx_project_tasks_parent"),
	}
}
