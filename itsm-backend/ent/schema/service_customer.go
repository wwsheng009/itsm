package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ServiceCustomer is a tenant-owned customer served by a NOC/MSP.
type ServiceCustomer struct{ ent.Schema }

func (ServiceCustomer) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Positive(),
		field.String("code").Optional().MaxLen(64),
		field.String("name").NotEmpty().MaxLen(255),
		field.String("normalized_name").NotEmpty().MaxLen(255),
		field.String("short_name").Optional().MaxLen(120),
		field.String("legal_name").Optional().MaxLen(255),
		field.String("en_name").Optional().MaxLen(255),
		field.JSON("aliases", []string{}).Optional(),
		field.JSON("historical_names", []string{}).Optional(),
		field.String("tax_id").Optional().MaxLen(64),
		field.String("normalized_tax_id").Optional().MaxLen(64),
		field.String("duns_number").Optional().MaxLen(32),
		field.String("registration_no").Optional().MaxLen(64),
		field.JSON("email_domains", []string{}).Optional(),
		field.String("customer_type").Default("direct").MaxLen(32),
		field.String("industry").Optional().MaxLen(64),
		field.String("sub_industry").Optional().MaxLen(64),
		field.String("size_class").Optional().MaxLen(32),
		field.String("ownership_type").Optional().MaxLen(32),
		field.String("tier").Default("C").MaxLen(16),
		field.Int16("vip_level").Default(0),
		field.String("credit_rating").Optional().MaxLen(16),
		field.String("website").Optional().MaxLen(255),
		field.String("main_phone").Optional().MaxLen(64),
		field.String("service_email").Optional().MaxLen(320),
		field.JSON("address", map[string]any{}).Optional(),
		field.String("timezone").Optional().MaxLen(64),
		field.String("language").Default("zh-CN").MaxLen(16),
		field.String("service_tier").Optional().MaxLen(32),
		field.Int("sla_definition_id").Optional().Nillable(),
		field.String("support_hours").Optional().MaxLen(64),
		field.JSON("coverage_scope", map[string]any{}).Optional(),
		field.Int("account_manager_id").Optional().Nillable(),
		field.Int("service_owner_id").Optional().Nillable(),
		field.Int("cs_manager_id").Optional().Nillable(),
		field.Int("oncall_group_id").Optional().Nillable(),
		field.String("billing_currency").Optional().MaxLen(8),
		field.String("payment_terms").Optional().MaxLen(32),
		field.Float("credit_limit").Optional().Nillable(),
		field.String("invoice_title").Optional().MaxLen(255),
		field.JSON("billing_address", map[string]any{}).Optional(),
		field.String("lifecycle_stage").Default("prospect").MaxLen(32),
		field.Time("onboarded_at").Optional().Nillable(),
		field.Time("contract_start_at").Optional().Nillable(),
		field.Time("contract_end_at").Optional().Nillable(),
		field.Time("churn_at").Optional().Nillable(),
		field.String("churn_reason").Optional().MaxLen(255),
		field.String("data_source").Default("manual").MaxLen(32),
		field.Int16("data_quality_score").Default(0),
		field.Int("merged_into_id").Optional().Nillable(),
		field.Int64("version").Default(1),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int("created_by").Optional().Nillable(),
		field.Int("updated_by").Optional().Nillable(),
		field.String("status").Default("active").MaxLen(32),
		field.Int("linked_customer_tenant_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ServiceCustomer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("branches", CustomerBranch.Type),
		edge.To("contracts", SupportContract.Type),
		edge.To("conversations", EmailConversation.Type),
	}
}

func (ServiceCustomer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "normalized_name").
			Unique().
			StorageKey("uq_customer_tenant_name").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("tenant_id", "code").
			Unique().
			StorageKey("uq_customer_tenant_code").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND code IS NOT NULL AND code <> ''")),
		index.Fields("tenant_id", "normalized_tax_id").
			Unique().
			StorageKey("uq_customer_tenant_taxid").
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND normalized_tax_id IS NOT NULL AND normalized_tax_id <> ''")),
		index.Fields("tenant_id", "tier", "status").
			StorageKey("idx_customer_tenant_tier"),
		index.Fields("tenant_id", "lifecycle_stage", "status").
			StorageKey("idx_customer_tenant_stage"),
		index.Fields("tenant_id", "account_manager_id").
			StorageKey("idx_customer_tenant_am"),
		index.Fields("tenant_id", "id").
			Unique().
			StorageKey("uq_customer_tenant_id"),
		index.Fields("tenant_id", "status"),
	}
}
