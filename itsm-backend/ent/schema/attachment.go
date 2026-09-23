package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Attachment 通用附件表（跨宿主：ticket / ticket_comment / knowledge_article / ...）。
//
// 与 ticket_attachments 的关系：biz_type='ticket' 时 biz_id 即 ticket_id，其余列同名同义
// （含 file_type 与 mime_type 同值的历史语义），P2 回填可直接 SELECT；历史 ID 必须平移
// （富文本 data-attachment-id 与 /tickets/:id/attachments/:ref 都引用旧 ID）。
//
// 索引、幂等唯一约束与 RLS 策略由迁移文件负责，ent 侧不声明 Indexes/Edges
// （与 ticket_attachments 先例一致，避免 ent 自建索引与迁移索引重名重复）：
// migrations/20260922_create_attachments_expand.sql —— idx_attachments_biz /
// uq_attachments_path / uq_attachments_client_token / tenant_isolation_attachments。
// 跨宿主设计不建外键（biz_type + biz_id 取代 ticket_id 外键），故无 Edges。
type Attachment struct {
	ent.Schema
}

// Fields of the Attachment.
func (Attachment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").
			Comment("租户ID").
			Positive(),
		field.String("biz_type").
			Comment("宿主业务类型：ticket | ticket_comment | knowledge_article | ...").
			NotEmpty().
			MaxLen(64),
		field.Int("biz_id").
			Comment("宿主业务ID（biz_type='ticket' 时即 ticket_id）").
			Positive(),
		field.String("usage").
			Comment("用途：attachment | inline_image | comment_attachment").
			NotEmpty().
			MaxLen(32).
			Default("attachment"),
		field.String("file_name").
			Comment("原始文件名（仅展示；存储 key 由服务端生成）").
			NotEmpty().
			MaxLen(255),
		field.String("file_path").
			Comment("存储 key；历史值形如 uploads/tickets/{ticketID}_{nano}_{安全文件名}，必须按原样读取").
			NotEmpty().
			MaxLen(512),
		field.String("file_url").
			Comment("访问URL（可选）").
			Optional().
			MaxLen(1024),
		field.Int("file_size").
			Comment("文件大小（字节）；0 字节不拒绝（错误码表未登记空文件），负数由服务层拦截").
			NonNegative(),
		field.String("file_type").
			Comment("历史语义：与 mime_type 同值；需要分类时用 usage/biz_type").
			NotEmpty().
			MaxLen(128),
		field.String("mime_type").
			Comment("MIME类型").
			Optional().
			MaxLen(128),
		field.String("sha256").
			Comment("内容摘要（秒传/去重，可选）").
			Optional().
			MaxLen(64),
		field.String("client_token").
			Comment("上传幂等键（可选；同一宿主内重复 token 复用既有行）").
			Optional().
			MaxLen(64),
		field.Int("uploaded_by").
			Comment("上传人ID").
			Positive(),
		field.String("status").
			Comment("状态：active | deleted（软删；被引用时删除返回 409）").
			NotEmpty().
			MaxLen(16).
			Default("active"),
		field.Time("created_at").
			Comment("创建时间").
			Default(time.Now),
		field.Time("deleted_at").
			Comment("软删除时间").
			Optional().
			Nillable(),
	}
}

// Edges of the Attachment.
//
// 跨宿主表刻意不建外键：宿主归属由 tenant_id + biz_type + biz_id 三元组在服务层校验，
// 避免与 ticket/user 表产生级联依赖（历史回填与宿主类型扩展都不受影响）。
func (Attachment) Edges() []ent.Edge { return nil }
