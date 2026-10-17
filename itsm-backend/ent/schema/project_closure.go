package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectClosure holds the schema definition for the ProjectClosure entity（P1，PE-15）。
//
// 权威定义：docs/project-management/plan/p1-closure-design.md。
// 结项清单（一项目一行，D-18）：自动项（任务清零/单据终态/结项门）实时派生不入库，
// 人工与产物项（复盘 KB 草稿、资产/许可登记确认、成员回收、CSAT 触发）落库。
type ProjectClosure struct {
	ent.Schema
}

// Fields of the ProjectClosure.
func (ProjectClosure) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户；一项目一行）").
			Positive(),

		// 复盘 KB 草稿（自动生成，不自动发布）。
		field.Int("retro_kb_article_id").
			Comment("复盘 KB 草稿文章ID（knowledge_articles，未生成为 0）").
			Optional().
			Default(0),
		field.Int("retro_generated_by").
			Comment("复盘草稿生成人ID").
			Optional().
			Default(0),
		field.Time("retro_generated_at").
			Comment("复盘草稿生成时间").
			Optional().
			Nillable(),

		// 资产/许可登记确认（P1 为人工确认 + 备注；P2 链接 asset/license/vendor/contract）。
		field.Int("assets_confirmed_by").
			Comment("资产登记确认人ID").
			Optional().
			Default(0),
		field.Time("assets_confirmed_at").
			Comment("资产登记确认时间").
			Optional().
			Nillable(),
		field.String("assets_note").
			Comment("资产/许可登记备注").
			Optional().
			Default("").
			MaxLen(1024),
		field.Int("assets_waived_by").
			Comment("资产登记豁免人ID").
			Optional().
			Default(0),
		field.Time("assets_waived_at").
			Comment("资产登记豁免时间").
			Optional().
			Nillable(),
		field.String("assets_waive_reason").
			Comment("资产登记豁免原因（豁免必填）").
			Optional().
			Default("").
			MaxLen(512),

		// 成员回收（回收 member 角色活跃成员；保留 pm/stakeholder，D-19）。
		field.Int("members_recycled_by").
			Comment("成员回收操作人ID").
			Optional().
			Default(0),
		field.Time("members_recycled_at").
			Comment("成员回收时间").
			Optional().
			Nillable(),
		field.Int("members_recycled_count").
			Comment("成员回收数量").
			Optional().
			Default(0),

		// CSAT（非阻塞项；结项时尽力触发，可手动重试）。
		field.Int("csat_survey_id").
			Comment("CSAT 调研ID（survey，未触发为 0）").
			Optional().
			Default(0),
		field.Time("csat_triggered_at").
			Comment("CSAT 触发时间").
			Optional().
			Nillable(),

		field.Int("version").
			Comment("乐观锁版本").
			Default(1),
		field.Int("created_by").
			Comment("创建人ID").
			Optional().
			Default(0),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the ProjectClosure.
func (ProjectClosure) Indexes() []ent.Index {
	return []ent.Index{
		// 一项目一结项清单（动作复用同一行）。
		index.Fields("tenant_id", "project_id").
			Unique().
			StorageKey("uk_project_closures"),
	}
}