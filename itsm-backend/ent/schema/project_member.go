package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectMember holds the schema definition for the ProjectMember entity（P0）。
//
// 权威定义：docs/project-management/plan/p0-data-ddl.md §4.1。
// 语义约束（docs/project-management/04）：user_id 必须是该租户的活跃 membership；
// 外部人员不建实体——"外部"标签由 membership 的 source/account_kind 派生。
//
// 说明：刻意不声明 ent edges/外键（与 attachment 先例一致：索引与幂等约束由迁移兜底，
// 查询按 tenant_id/project_id 字段收窄，RLS 为最后防线）。
type ProjectMember struct {
	ent.Schema
}

// Fields of the ProjectMember.
func (ProjectMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.Int("user_id").
			Comment("用户ID（须为该租户活跃 membership，见 04 §3.1）").
			Positive(),
		field.String("project_role").
			Comment("项目角色: pm|member|stakeholder（pm 为协同分工，审批解析以 projects.manager_id 为准）").
			Default("member").
			MaxLen(16),
		field.String("status").
			Comment("状态: active|removed（软失效，保留历史）").
			Default("active").
			MaxLen(16),
		// ---- P2 成员周容量覆盖（PE-23；权威定义：docs/project-management/plan/p2-resource-heatmap-design.md §3）----
		field.Float("weekly_capacity_hours").
			Comment("成员周容量覆盖（小时；NULL=继承项目默认；PE-23 资源热图）").
			Optional().
			Nillable(),
		field.Time("joined_at").
			Comment("加入时间").
			Default(time.Now),
		field.Int("created_by").
			Comment("创建人ID").
			Optional(),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectMember.
func (ProjectMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "user_id").
			Unique().
			StorageKey("uk_project_members_live").
			Annotations(entsql.IndexWhere("status = 'active'")),
		index.Fields("tenant_id", "user_id").
			StorageKey("idx_project_members_tenant_user"),
	}
}
