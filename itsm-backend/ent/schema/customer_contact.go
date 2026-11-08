package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CustomerContact is a non-login contact person for a service customer.
//
// It intentionally does not reuse users: customer contacts should not consume
// tenant user quota or gain login semantics. A contact can be linked to a user
// later through linked_user_id.
type CustomerContact struct{ ent.Schema }

func (CustomerContact) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.Int("customer_id").Positive(),
		field.Int("branch_id").Optional().Nillable(),
		field.String("name").NotEmpty().MaxLen(255),
		field.String("name_en").Optional().MaxLen(255),
		field.String("title").Optional().MaxLen(128),
		field.String("department").Optional().MaxLen(128),
		field.String("email").Optional().MaxLen(320),
		field.String("normalized_email").Optional().MaxLen(320),
		field.String("mobile").Optional().MaxLen(64),
		field.String("phone").Optional().MaxLen(64),
		field.String("wechat").Optional().MaxLen(128),
		field.String("contact_role").Default("business").MaxLen(32),
		field.Bool("is_primary").Default(false),
		field.String("language").Optional().MaxLen(16),
		field.String("timezone").Optional().MaxLen(64),
		field.Bool("opt_in").Default(true),
		field.Int("linked_user_id").Optional().Nillable(),
		field.String("source").Default("manual").MaxLen(32),
		field.String("status").Default("active").MaxLen(16),
		field.Time("deleted_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CustomerContact) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "customer_id", "normalized_email").
			Unique().
			StorageKey("uq_contact_email").
			Annotations(entsql.IndexWhere("status = 'active' AND deleted_at IS NULL AND normalized_email IS NOT NULL AND normalized_email <> ''")),
		index.Fields("tenant_id", "customer_id", "contact_role").
			Unique().
			StorageKey("uq_contact_primary").
			Annotations(entsql.IndexWhere("is_primary AND status = 'active' AND deleted_at IS NULL")),
		index.Fields("tenant_id", "customer_id", "status").
			StorageKey("idx_contact_tenant_customer"),
		index.Fields("tenant_id", "normalized_email").
			StorageKey("idx_contact_tenant_email"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_contact_tenant_id"),
	}
}
