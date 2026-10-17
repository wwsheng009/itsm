package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectAssetLink holds the schema definition for the ProjectAssetLink entity（P2，PE-26）。
//
// 权威定义：docs/project-management/plan/p2-procurement-design.md §3。
// 语义：结项清单「采购/资产登记」→ 既有台账（asset/asset_license/vendor/contract）结构化链接；
// 弱引用 target_id（不建外键，同 project_risk 先例；写入时同租户存在性校验）。
// 未删行唯一 (tenant_id, project_id, link_type, target_id) 由迁移的 partial unique index 兜底
// （ent 不支持 partial index，SQLite 测试库以服务层查重为准）。
type ProjectAssetLink struct {
	ent.Schema
}

// Fields of the ProjectAssetLink.
func (ProjectAssetLink) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.Int("project_id").
			Comment("项目ID（同租户）").
			Positive(),
		field.String("link_type").
			Comment("链接类型: asset|asset_license|vendor|contract").
			NotEmpty().
			MaxLen(16),
		field.Int("target_id").
			Comment("目标ID（同租户弱引用）").
			Positive(),
		field.String("note").
			Comment("登记备注").
			Default("").
			MaxLen(512),
		field.Int("created_by").
			Comment("登记人ID").
			Optional(),
		field.Time("deleted_at").
			Comment("软删除时间（DELETE 语义）").
			Optional().
			Nillable(),
		field.Int("deleted_by").
			Comment("删除人ID").
			Optional(),
		field.Time("created_at").
			Default(time.Now),
	}
}

// Indexes of the ProjectAssetLink.
func (ProjectAssetLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "created_at").
			StorageKey("idx_project_asset_links_tenant_project"),
	}
}