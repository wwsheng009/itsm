package bot

import (
	"context"
	"encoding/json"
	"fmt"

	"itsm-backend/ent"
	"itsm-backend/ent/botartifact"
)

// B3-06 产物存储：plan/analysis/draft 类工具的产出落 `bot_artifacts`。
//
// 契约：
//   - Create：租户 + 归属发起人 + 会话/运行（可选）+ 类型 + 内容/证据（JSON）。
//   - Get/List：查询一律**租户 + 归属**双条件收敛——跨租户/跨用户读取表现为不存在。
//   - 本存储**不写业务表**（plan/analysis 工具只读业务库）。
type ArtifactStore struct {
	client *ent.Client
}

// ArtifactKind 取值（与 schema 注释一致）。
const (
	ArtifactKindPlan     = "plan"
	ArtifactKindAnalysis = "analysis"
	ArtifactKindDraft    = "draft"
)

// ArtifactInput 是一次产物写入的完整入参。
type ArtifactInput struct {
	TenantID       int
	OwnerUserID    int
	ConversationID int
	RunID          int
	Kind           string
	ToolName       string
	Title          string
	Content        interface{}
	Evidence       interface{}
}

// NewArtifactStore 构造产物存储；client 为 nil 时返回 nil（调用方据此关闭该能力）。
func NewArtifactStore(client *ent.Client) *ArtifactStore {
	if client == nil {
		return nil
	}
	return &ArtifactStore{client: client}
}

// Create 写入一条产物并返回记录。
func (s *ArtifactStore) Create(ctx context.Context, in ArtifactInput) (*ent.BotArtifact, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: artifact store 未初始化")
	}
	if in.TenantID <= 0 || in.OwnerUserID <= 0 {
		return nil, fmt.Errorf("bot: artifact 需要租户与归属发起人")
	}
	kind := in.Kind
	switch kind {
	case ArtifactKindPlan, ArtifactKindAnalysis, ArtifactKindDraft:
	default:
		return nil, fmt.Errorf("bot: 未知产物类型 %q", in.Kind)
	}

	content, err := json.Marshal(in.Content)
	if err != nil {
		return nil, fmt.Errorf("bot: 产物内容序列化失败: %w", err)
	}
	evidence := []byte("{}")
	if in.Evidence != nil {
		evidence, err = json.Marshal(in.Evidence)
		if err != nil {
			return nil, fmt.Errorf("bot: 产物证据序列化失败: %w", err)
		}
	}

	create := s.client.BotArtifact.Create().
		SetTenantID(in.TenantID).
		SetOwnerUserID(in.OwnerUserID).
		SetKind(kind).
		SetToolName(in.ToolName).
		SetTitle(in.Title).
		SetContentJSON(string(content)).
		SetEvidenceJSON(string(evidence))
	if in.ConversationID > 0 {
		create.SetConversationID(in.ConversationID)
	}
	if in.RunID > 0 {
		create.SetRunID(in.RunID)
	}
	return create.Save(ctx)
}

// Get 按「租户 + 归属 + ID」读取；不满足任一条件即表现为不存在。
func (s *ArtifactStore) Get(ctx context.Context, tenantID, ownerUserID, id int) (*ent.BotArtifact, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: artifact store 未初始化")
	}
	return s.client.BotArtifact.Query().
		Where(
			botartifact.ID(id),
			botartifact.TenantID(tenantID),
			botartifact.OwnerUserID(ownerUserID),
		).
		Only(ctx)
}

// List 按「租户 + 归属」分页列出（可选类型过滤；按创建时间倒序）。
func (s *ArtifactStore) List(ctx context.Context, tenantID, ownerUserID int, kind string, limit int) ([]*ent.BotArtifact, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("bot: artifact store 未初始化")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := s.client.BotArtifact.Query().
		Where(botartifact.TenantID(tenantID), botartifact.OwnerUserID(ownerUserID)).
		Order(ent.Desc(botartifact.FieldCreatedAt)).
		Limit(limit)
	if kind != "" {
		query = query.Where(botartifact.Kind(kind))
	}
	return query.All(ctx)
}
