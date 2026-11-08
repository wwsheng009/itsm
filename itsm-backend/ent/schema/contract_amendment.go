package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ContractAmendment records an in-place change to a signed support contract.
type ContractAmendment struct{ ent.Schema }

func (ContractAmendment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("contract_id").Positive(),
		field.String("amendment_no").NotEmpty().MaxLen(64),
		field.String("amend_type").NotEmpty().MaxLen(32),
		field.Time("effective_from"),
		field.Float("amount_delta").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "numeric(18,2)", dialect.SQLite: "decimal(18,2)",
		}),
		field.Int("term_delta_days").Optional().Nillable(),
		field.JSON("diff", map[string]interface{}{}).Optional(),
		field.String("reason").Optional().MaxLen(500),
		field.String("status").Default("draft").MaxLen(16),
		field.Time("signed_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Int("updated_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ContractAmendment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "contract_id", "amendment_no").Unique().StorageKey("uq_amend_no"),
		index.Fields("tenant_id", "id").Unique().StorageKey("uq_amend_tenant_id"),
		index.Fields("tenant_id", "contract_id", "status").StorageKey("idx_amend_tenant_contract"),
	}
}
