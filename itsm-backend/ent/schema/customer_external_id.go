package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CustomerExternalID maps a tenant-scoped customer master record to an
// identifier in an external system (ERP/CRM/OA/ticketing).
//
// The external system is intentionally not validated at write time: this table
// records a reconciliation key, not a live integration.
type CustomerExternalID struct{ ent.Schema }

func (CustomerExternalID) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("customer_id").Positive(),
		field.String("system_code").NotEmpty().MaxLen(64),
		field.String("external_id").NotEmpty().MaxLen(160),
		field.String("external_url").Optional().MaxLen(500),
		field.Time("created_at").Default(time.Now),
	}
}

func (CustomerExternalID) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "system_code", "external_id").
			Unique().
			StorageKey("uq_extid_tenant_system_external"),
		index.Fields("tenant_id", "customer_id").
			StorageKey("idx_extid_tenant_customer"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_extid_tenant_id"),
	}
}
