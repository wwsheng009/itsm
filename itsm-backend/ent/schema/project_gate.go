package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectGate holds the schema definition for the ProjectGate entity（P1，PE-12）。
//
// 权威定义：docs/project-management/plan/p1-gate-design.md。
// 四道阶段门（initiation|plan|acceptance|closure）每项目每门一条记录：
// 提交时执行门禁规则（不通过 → 409 + blockers），通过后触发 BPMN 审批
// （business_type=project，business_sub_type=<gate>，business_key=project:<id>）。
type ProjectGate struct {
	ent.Schema
}

// Fields of the ProjectGate.
func (ProjectGate) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.String("gate").
			Comment("阶段门：initiation|plan|acceptance|closure").
			MaxLen(32).
			NotEmpty(),
		field.String("status").
			Comment("状态：pending|in_review|passed|rejected").
			MaxLen(32).
			Default("pending"),
		field.Int("process_instance_id").
			Comment("关联 BPMN 流程实例ID（未触发/降级通过为 0）").
			Optional().
			Default(0),
		field.String("approval_chain_id").
			Comment("审批链ID（取自 process_binding）").
			Optional().
			Default("").
			MaxLen(128),
		field.Int("submitted_by").
			Comment("提交人ID").
			Optional().
			Default(0),
		field.Time("submitted_at").
			Comment("提交时间").
			Optional().
			Nillable(),
		field.Int("decided_by").
			Comment("裁决人ID").
			Optional().
			Default(0),
		field.Time("decided_at").
			Comment("裁决时间").
			Optional().
			Nillable(),
		field.String("decision_comment").
			Comment("裁决意见（拒绝必填）").
			Optional().
			Default("").
			MaxLen(512),
		field.JSON("blockers", []map[string]any{}).
			Comment("最近一次门禁阻塞明细（通过时清空）").
			Optional(),
		field.Int("reject_count").
			Comment("累计被拒绝次数（PE-24 一次通过率度量：passed 且 reject_count=0 记为一次通过）").
			Default(0),
		field.Int("version").
			Comment("乐观锁版本").
			Default(1),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectGate.
func (ProjectGate) Indexes() []ent.Index {
	return []ent.Index{
		// 一项目一阶段门一条记录（提交/裁决复用同一行）。
		index.Fields("tenant_id", "project_id", "gate").
			Unique().
			StorageKey("uk_project_gates"),
		index.Fields("project_id", "status"),
	}
}
