package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SupportContract struct{ ent.Schema }

func (SupportContract) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("customer_id").Positive(),
		field.Int("branch_id").Optional().Nillable(),
		field.String("contract_number").NotEmpty().MaxLen(160),
		field.String("normalized_contract_number").NotEmpty().MaxLen(160),
		field.String("status").Default("active").MaxLen(32),
		field.Time("start_at").Optional().Nillable(),
		field.Time("end_at").Optional().Nillable(),
		field.Int("source_document_id").Optional().Nillable(),
		field.Time("effective_from").Optional().Nillable(),
		field.Time("effective_until").Optional().Nillable(),
		field.Time("signed_at").Optional().Nillable(),
		field.Float("amount").Optional().Nillable().SchemaType(map[string]string{
			dialect.Postgres: "numeric(18,2)", dialect.SQLite: "decimal(18,2)",
		}),
		field.String("currency").Optional().MaxLen(8),
		field.String("contract_type").Default("support").MaxLen(32),
		field.String("contract_name").Optional().MaxLen(255),
		field.Int("parent_contract_id").Optional().Nillable(),
		field.Int("renewal_of_contract_id").Optional().Nillable(),
		field.Int("amendment_count").Default(0),
		field.Int("sla_definition_id").Optional().Nillable(),
		field.Bool("auto_renew").Default(false),
		field.Time("terminated_at").Optional().Nillable(),
		field.String("termination_reason").Optional().MaxLen(255),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int64("version").Default(1),
		field.Int("created_by").Optional().Nillable(),
		field.Int("updated_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SupportContract) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", ServiceCustomer.Type).Ref("contracts").Field("customer_id").Unique().Required(),
		edge.From("branch", CustomerBranch.Type).Ref("contracts").Field("branch_id").Unique(),
		edge.To("external_references", ExternalContractReference.Type),
		edge.To("conversations", EmailConversation.Type),
	}
}

func (SupportContract) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "normalized_contract_number").
			Unique().
			StorageKey("uq_sc_tenant_no").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "customer_id", "status"),
		index.Fields("tenant_id", "effective_until").
			StorageKey("idx_sc_tenant_effective").
			Annotations(entsql.IndexWhere("status = 'active' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "contract_type", "status").
			StorageKey("idx_sc_tenant_type"),
		index.Fields("tenant_id", "id").Unique().StorageKey("uq_sc_tenant_id"),
	}
}
