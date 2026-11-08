package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectCustomerLink is the same-tenant link between a project and a customer
// master record. It deliberately does not add a customer column to projects
// (project-management D-01).
type ProjectCustomerLink struct{ ent.Schema }

func (ProjectCustomerLink) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("project_id").Positive(),
		field.Int("customer_id").Positive(),
		field.Int("branch_id").Optional().Nillable(),
		field.String("relation_type").Default("delivery").MaxLen(24),
		field.Bool("is_primary").Default(false),
		field.String("note").Optional().MaxLen(500),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ProjectCustomerLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "project_id", "customer_id", "relation_type").
			Unique().
			StorageKey("uq_pclink").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "project_id").
			Unique().
			StorageKey("uq_pclink_primary").
			Annotations(entsql.IndexWhere("is_primary AND deleted_at IS NULL")),
		index.Fields("tenant_id", "customer_id").
			StorageKey("idx_pclink_tenant_customer"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_pclink_tenant_id"),
	}
}
