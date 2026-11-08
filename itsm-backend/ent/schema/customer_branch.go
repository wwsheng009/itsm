package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CustomerBranch struct{ ent.Schema }

func (CustomerBranch) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("customer_id").Positive(),
		field.String("code").Optional().MaxLen(64),
		field.String("name").NotEmpty().MaxLen(255),
		field.String("normalized_name").NotEmpty().MaxLen(255),
		field.String("branch_type").Default("site").MaxLen(32),
		field.JSON("aliases", []string{}).Optional(),
		field.JSON("address", map[string]any{}).Optional(),
		field.String("timezone").Optional().MaxLen(64),
		field.String("contact_phone").Optional().MaxLen(64),
		field.JSON("email_domains", []string{}).Optional(),
		field.String("service_tier").Optional().MaxLen(32),
		field.String("support_hours").Optional().MaxLen(64),
		field.Bool("is_primary").Default(false),
		field.Int("linked_tenant_id").Optional().Nillable(),
		field.String("status").Default("active").MaxLen(32),
		field.Time("deleted_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CustomerBranch) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", ServiceCustomer.Type).Ref("branches").Field("customer_id").Unique().Required(),
		edge.To("contracts", SupportContract.Type),
		edge.To("conversations", EmailConversation.Type),
	}
}

func (CustomerBranch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "customer_id", "normalized_name").
			Unique().
			StorageKey("uq_branch_tenant_name").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "code").
			Unique().
			StorageKey("uq_branch_tenant_code").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND code IS NOT NULL AND code <> ''")),
		index.Fields("tenant_id", "customer_id").
			Unique().
			StorageKey("uq_branch_primary").
			Annotations(entsql.IndexWhere("is_primary AND status = 'active' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_branch_tenant_id"),
		index.Fields("tenant_id", "customer_id", "id").
			Unique().
			StorageKey("uq_branch_tenant_customer_id"),
		index.Fields("tenant_id", "customer_id", "status"),
	}
}
