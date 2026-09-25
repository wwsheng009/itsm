package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LLMProviderConfig 租户级 LLM Provider 实例配置（多 Provider 支持与可切换方案 BE-1）。
//
// 设计要点（docs/plan/multi-llm-provider-plan.md §3.1.1）：
//   - 密钥只落密文：encrypted_api_key 存 AES-GCM 密文（AAD=tenant_id:name），与
//     connector_configs.encrypted_credentials 同先例，禁止明文落库或回显；
//   - protocol 为 API 形态四值枚举（openai_chat_completions / openai_responses /
//     anthropic_messages / google_gemini）；variant 为兼容变体槽位（P0：azure / ollama / minimax）；
//   - adapter_options 仅存变体专属非敏感参数（如 azure api_version、ollama keep_alive），
//     禁止存放密钥，白名单/大小校验由服务层承担（§3.5）；
//   - 软删除：deleted_at 非空即已删除，读取路径统一过滤（与 system_config 风格一致）；
//   - 「每租户至多一个默认实例」的部分唯一索引 ent 无法表达，由迁移层兜底：
//     migrations/20260924_create_llm_providers_expand.sql
//     → uq_llm_provider_configs_tenant_default（WHERE is_default AND deleted_at IS NULL）。
type LLMProviderConfig struct{ ent.Schema }

// Fields of the LLMProviderConfig.
func (LLMProviderConfig) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.String("name").
			Comment("实例 key（API/请求参数使用；正则 ^[a-z0-9][a-z0-9_-]*$ 由服务层校验）").
			NotEmpty().
			MaxLen(64),
		field.String("display_name").
			Comment("展示名").
			NotEmpty().
			MaxLen(100),
		field.String("protocol").
			Comment("API 形态枚举：openai_chat_completions | openai_responses | anthropic_messages | google_gemini").
			NotEmpty().
			MaxLen(32),
		field.String("variant").
			Comment("兼容变体槽位（空 = 标准实现）：azure | ollama | minimax").
			Optional().
			MaxLen(32),
		field.JSON("adapter_options", map[string]interface{}{}).
			Comment("变体专属非敏感参数（禁放密钥）").
			Optional(),
		field.String("model").
			Comment("传给 provider 的模型名（chat completions / anthropic / gemini 使用）").
			Optional().
			MaxLen(100),
		field.String("endpoint").
			Comment("接口地址；留空回退内置默认地址").
			Optional().
			MaxLen(500),
		field.String("deployment").
			Comment("部署名（variant=azure 使用）").
			Optional().
			MaxLen(100),
		field.Text("encrypted_api_key").
			Comment("API Key 的 AES-GCM 密文（AAD=tenant_id:name）；无鉴权实例可为空").
			Optional(),
		field.Bool("enabled").
			Comment("是否启用；禁用后不可被解析").
			Default(true),
		field.Bool("is_default").
			Comment("租户默认标记").
			Default(false),
		field.String("source").
			Comment("来源：manual | imported").
			Default("manual").
			MaxLen(20),
		field.String("status").
			Comment("最近一次测试结果：configured | ok | error").
			Default("configured").
			MaxLen(20),
		field.String("last_error").
			Comment("最近一次测试/调用失败的脱敏原因").
			Optional().
			MaxLen(2000),
		field.Time("last_tested_at").
			Comment("最近一次测试时间").
			Optional().
			Nillable(),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Time("deleted_at").
			Comment("软删除时间").
			Optional().
			Nillable(),
	}
}

// Indexes of the LLMProviderConfig.
//
// 软删场景下 (tenant_id, name) 仍按物理行唯一（同一 key 软删后需复用请服务层改写 name 或恢复原行）。
// 「每租户至多一个 is_default」的部分唯一索引 ent 不支持，见迁移文件。
func (LLMProviderConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "enabled"),
	}
}
