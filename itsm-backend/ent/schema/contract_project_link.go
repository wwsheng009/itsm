package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ContractProjectLink is the M:N link between a support contract and a project.
// Only billing rows may carry allocated quota; semantic rows stay allocation-free.
type ContractProjectLink struct{ ent.Schema }

func (ContractProjectLink) Fields() []ent.Field {
	numeric := map[string]string{dialect.Postgres: "numeric(18,2)", dialect.SQLite: "decimal(18,2)"}
	hours := map[string]string{dialect.Postgres: "numeric(10,2)", dialect.SQLite: "decimal(10,2)"}
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("contract_id").Positive(),
		field.Int("project_id").Positive(),
		field.Int("service_item_id").Optional().Nillable(),
		field.String("relation_type").Default("basis").MaxLen(24),
		field.Float("allocated_amount").Optional().Nillable().SchemaType(numeric),
		field.Float("allocated_hours").Optional().Nillable().SchemaType(hours),
		field.Int16("allocation_pct").Optional().Nillable(),
		field.Bool("is_primary").Default(false),
		field.String("note").Optional().MaxLen(500),
		field.Time("effective_from").Optional().Nillable(),
		field.Time("effective_to").Optional().Nillable(),
		field.String("status").Default("active").MaxLen(16),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ContractProjectLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "contract_id", "project_id", "relation_type").
			Unique().
			StorageKey("uq_cpl_semantic").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND status = 'active' AND relation_type <> 'billing'")),
		index.Fields("tenant_id", "contract_id", "project_id", "service_item_id").
			Unique().
			StorageKey("uq_cpl_billing").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND status = 'active' AND relation_type = 'billing'")),
		index.Fields("tenant_id", "project_id").
			Unique().
			StorageKey("uq_cpl_primary").
			Annotations(entsql.IndexWhere("is_primary AND relation_type = 'basis' AND status = 'active' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "contract_id", "status").StorageKey("idx_cpl_tenant_contract"),
		index.Fields("tenant_id", "project_id", "status").StorageKey("idx_cpl_tenant_project"),
		index.Fields("tenant_id", "id").Unique().StorageKey("uq_cpl_tenant_id"),
	}
}
