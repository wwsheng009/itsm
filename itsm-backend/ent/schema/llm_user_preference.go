package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LLMUserPreference 用户级 LLM Provider 偏好（多 Provider 支持与可切换方案 BE-1，§3.1.2）。
//
// 设计要点：
//   - provider_key 为空 = 跟随租户默认；非空时由服务层校验其指向同租户的已启用实例；
//   - tenant_id 是冗余租户列：用于跨租户引用校验与租户过滤，不因 user_id 唯一而省略；
//   - 实例被禁用/软删时读取路径降级为租户默认，不自动改写个人偏好（响应标注来源）；
//   - 软删除由服务层/DB 语义决定，本表按用户维度唯一，故只保留 created_at/updated_at。
type LLMUserPreference struct{ ent.Schema }

// Fields of the LLMUserPreference.
func (LLMUserPreference) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").
			Comment("用户ID").
			Positive(),
		field.Int("tenant_id").
			Comment("租户ID（冗余，用于 provider 归属校验）").
			Positive(),
		field.String("provider_key").
			Comment("选用的 provider 实例 key；空 = 跟随租户默认").
			Optional().
			MaxLen(64),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("updated_at").
			Comment("更新时间").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the LLMUserPreference.
//
// user_id 全局唯一：一个用户一条 LLM 偏好（写入统一走 upsert 语义，避免读时多行歧义）。
func (LLMUserPreference) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
	}
}
