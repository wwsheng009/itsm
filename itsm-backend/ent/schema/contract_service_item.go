package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ContractServiceItem is a deliverable line inside a support contract.
type ContractServiceItem struct{ ent.Schema }

func (ContractServiceItem) Fields() []ent.Field {
	numeric := map[string]string{dialect.Postgres: "numeric(18,2)", dialect.SQLite: "decimal(18,2)"}
	quantity := map[string]string{dialect.Postgres: "numeric(18,4)", dialect.SQLite: "decimal(18,4)"}
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("contract_id").Positive(),
		field.Int("catalog_item_id").Optional().Nillable(),
		field.String("item_code").Optional().MaxLen(64),
		field.String("item_name").NotEmpty().MaxLen(255),
		field.String("spec").Optional().MaxLen(500),
		field.String("billing_unit").Default("month").MaxLen(32),
		field.Float("quantity").Default(1).SchemaType(quantity),
		field.Float("period_count").Default(1).SchemaType(quantity),
		field.Float("unit_price").Default(0).SchemaType(numeric),
		field.Float("amount").Default(0).SchemaType(numeric),
		field.Float("quota_total").Optional().Nillable().SchemaType(quantity),
		field.Float("quota_used").Default(0).SchemaType(quantity),
		field.String("note").Optional().MaxLen(500),
		field.Int("sla_definition_id").Optional().Nillable(),
		field.Int("response_target_min").Optional().Nillable(),
		field.Int("resolve_target_min").Optional().Nillable(),
		field.Int("sort_order").Default(0),
		field.String("status").Default("active").MaxLen(16),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Int("updated_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ContractServiceItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "id").Unique().StorageKey("uq_csi_tenant_id"),
		index.Fields("tenant_id", "contract_id", "item_code").
			Unique().
			StorageKey("uq_csi_contract_code").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND item_code IS NOT NULL AND item_code <> ''")),
		index.Fields("tenant_id", "contract_id", "status").StorageKey("idx_csi_tenant_contract"),
	}
}
