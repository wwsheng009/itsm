package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ContractUsageLedger is the idempotent source of truth for quota consumption.
type ContractUsageLedger struct{ ent.Schema }

func (ContractUsageLedger) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("contract_id").Positive(),
		field.Int("service_item_id").Positive(),
		field.String("source_type").NotEmpty().MaxLen(32),
		field.String("source_ref").NotEmpty().MaxLen(160),
		field.Float("quantity_delta").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "numeric(18,4)", dialect.SQLite: "decimal(18,4)",
		}),
		field.Float("amount_delta").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "numeric(18,2)", dialect.SQLite: "decimal(18,2)",
		}),
		field.Time("occurred_at").Default(time.Now),
		field.String("note").Optional().MaxLen(500),
		field.Int("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
	}
}

func (ContractUsageLedger) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "service_item_id", "source_type", "source_ref").
			Unique().
			StorageKey("uq_usage_source"),
		index.Fields("tenant_id", "contract_id", "occurred_at").StorageKey("idx_usage_tenant_contract"),
		index.Fields("tenant_id", "service_item_id", "occurred_at").StorageKey("idx_usage_tenant_item"),
		index.Fields("tenant_id", "id").Unique().StorageKey("uq_usage_tenant_id"),
	}
}
