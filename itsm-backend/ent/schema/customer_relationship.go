package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CustomerRelationship stores a directed relation between two customer master
// records in the same tenant. Reverse rows are written by the service for
// parent/subsidiary, partner and competitor relations.
type CustomerRelationship struct{ ent.Schema }

func (CustomerRelationship) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("from_customer_id").Positive(),
		field.Int("to_customer_id").Positive(),
		field.String("relation_type").MaxLen(32),
		field.String("relation_role").Default("outbound").MaxLen(16),
		field.Bool("is_bidirectional").Default(false),
		field.Time("effective_from").Optional().Nillable(),
		field.Time("effective_to").Optional().Nillable(),
		field.String("note").Optional().MaxLen(500),
		field.String("status").Default("active").MaxLen(16),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CustomerRelationship) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "from_customer_id", "to_customer_id", "relation_type").
			Unique().
			StorageKey("uq_rel_pair_type").
			Annotations(entsql.IndexWhere("status = 'active' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "from_customer_id", "status").
			StorageKey("idx_rel_tenant_from"),
		index.Fields("tenant_id", "to_customer_id", "status").
			StorageKey("idx_rel_tenant_to"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_rel_tenant_id"),
	}
}
