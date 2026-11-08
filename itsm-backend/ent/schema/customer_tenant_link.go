package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CustomerTenantLink maps a tenant-scoped customer master record to one or
// more tenant instances. linked_tenant_id is a deliberate weak reference
// (tenants.id) and must be validated by the service layer.
type CustomerTenantLink struct{ ent.Schema }

func (CustomerTenantLink) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("customer_id").Positive(),
		field.Int("linked_tenant_id").Positive(),
		field.String("link_type").Default("primary").MaxLen(24),
		field.Bool("is_billing_anchor").Default(false),
		field.String("provision_status").Default("pending").MaxLen(24),
		field.Time("provisioned_at").Optional().Nillable(),
		field.String("note").Optional().MaxLen(500),
		field.Time("deleted_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CustomerTenantLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "customer_id", "linked_tenant_id").
			Unique().
			StorageKey("uq_ctlink").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "customer_id").
			Unique().
			StorageKey("uq_ctlink_primary").
			Annotations(entsql.IndexWhere("link_type = 'primary' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "linked_tenant_id").
			Unique().
			StorageKey("uq_ctlink_linked_tenant").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "customer_id").
			Unique().
			StorageKey("uq_ctlink_billing").
			Annotations(entsql.IndexWhere("is_billing_anchor AND deleted_at IS NULL")),
		index.Fields("tenant_id", "linked_tenant_id").
			StorageKey("idx_ctlink_tenant_linked"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_ctlink_tenant_id"),
	}
}
