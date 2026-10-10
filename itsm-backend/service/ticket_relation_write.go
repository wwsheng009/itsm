// ticket_relation_write.go：工单关联写面（剩余缺口治理 B4）。
//
// 数据模型：tickets.related_tickets M2M 双向边（无类型化关系实体）。
// 因此写面只支持「相关」语义（relates_to，空值默认）；
// blocks/duplicates 等类型化关系为 roadmap（product-capabilities: advancedTicketRelations）。
//
// 契约（与前端 ticket-relations-api.ts / RelationPanel 对齐）：
//   - POST   /api/v1/tickets/relations            {sourceTicketId,targetTicketId,relationType?,description?}
//   - DELETE /api/v1/tickets/relations?sourceTicketId=&targetTicketId=
//
// 安全：全部操作按 tenantID 收口（跨租户目标返回 not found，不泄露存在性）；幂等可重试。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/ticket"
)

var (
	// ErrRelationSelf 关联自身。
	ErrRelationSelf = errors.New("ticket cannot relate to itself")
	// ErrRelationUnsupportedType 暂不支持的关系类型（仅 relates_to）。
	ErrRelationUnsupportedType = errors.New("unsupported relation type")
	// ErrRelationNotFound 工单不存在或不在当前租户（含跨租户 fail-closed）。
	ErrRelationNotFound = errors.New("ticket not found in tenant")
	// ErrRelationInvalidIDs 工单ID无效。
	ErrRelationInvalidIDs = errors.New("invalid ticket id")
)

// TicketRelationView 关联写操作的响应投影（M2M 无边属性；relationType 归一为 relates_to）。
type TicketRelationView struct {
	ID             int    `json:"id"`
	SourceTicketID int    `json:"sourceTicketId"`
	TargetTicketID int    `json:"targetTicketId"`
	RelationType   string `json:"relationType"`
	Description    string `json:"description,omitempty"`
}

// normalizeRelationType 仅接受「相关」语义（空值默认 relates_to）；其余类型显式拒绝。
func normalizeRelationType(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "relates_to", "related":
		return "relates_to", nil
	default:
		return "", ErrRelationUnsupportedType
	}
}

// queryTicketInTenant 读取租户内未删除工单；不存在/跨租户统一 ErrRelationNotFound。
func (s *TicketAssociationService) queryTicketInTenant(ctx context.Context, tenantID, ticketID int) (*ent.Ticket, error) {
	if ticketID <= 0 {
		return nil, ErrRelationInvalidIDs
	}
	query := s.client.Ticket.Query().
		Where(ticket.ID(ticketID), ticket.DeletedAtIsNil())
	if tenantID > 0 {
		query = query.Where(ticket.TenantIDEQ(tenantID))
	}
	ticketEntity, err := query.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrRelationNotFound
		}
		return nil, fmt.Errorf("查询工单失败: %w", err)
	}
	return ticketEntity, nil
}

// AddTicketRelation 建立 source↔target「相关」双向边（幂等；租户内校验）。
func (s *TicketAssociationService) AddTicketRelation(
	ctx context.Context,
	tenantID, sourceID, targetID int,
	relationType, description string,
) (*TicketRelationView, error) {
	normalized, err := normalizeRelationType(relationType)
	if err != nil {
		return nil, err
	}
	if sourceID <= 0 || targetID <= 0 {
		return nil, ErrRelationInvalidIDs
	}
	if sourceID == targetID {
		return nil, ErrRelationSelf
	}
	if _, err := s.queryTicketInTenant(ctx, tenantID, sourceID); err != nil {
		return nil, err
	}
	if _, err := s.queryTicketInTenant(ctx, tenantID, targetID); err != nil {
		return nil, err
	}

	// 幂等：已存在则直接返回视图，不重复写边。
	exists, err := s.client.Ticket.Query().
		Where(ticket.ID(sourceID), ticket.TenantIDEQ(tenantID)).
		QueryRelatedTickets().
		Where(ticket.ID(targetID), ticket.TenantIDEQ(tenantID)).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询现有关联失败: %w", err)
	}
	if !exists {
		tx, err := s.client.Tx(ctx)
		if err != nil {
			return nil, fmt.Errorf("启动关联事务失败: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		if err := tx.Ticket.UpdateOneID(sourceID).
			Where(ticket.TenantIDEQ(tenantID), ticket.DeletedAtIsNil()).
			AddRelatedTicketIDs(targetID).
			Exec(ctx); err != nil {
			return nil, fmt.Errorf("写入工单关联失败: %w", err)
		}
		if err := tx.Ticket.UpdateOneID(targetID).
			Where(ticket.TenantIDEQ(tenantID), ticket.DeletedAtIsNil()).
			AddRelatedTicketIDs(sourceID).
			Exec(ctx); err != nil {
			return nil, fmt.Errorf("写入反向工单关联失败: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("提交关联事务失败: %w", err)
		}
	}

	return &TicketRelationView{
		ID:             targetID,
		SourceTicketID: sourceID,
		TargetTicketID: targetID,
		RelationType:   normalized,
		Description:    description,
	}, nil
}

// RemoveTicketRelation 解除 source↔target 双向边（幂等；租户内校验）。
func (s *TicketAssociationService) RemoveTicketRelation(ctx context.Context, tenantID, sourceID, targetID int) error {
	if sourceID <= 0 || targetID <= 0 {
		return ErrRelationInvalidIDs
	}
	if sourceID == targetID {
		return ErrRelationSelf
	}
	if _, err := s.queryTicketInTenant(ctx, tenantID, sourceID); err != nil {
		return err
	}
	if _, err := s.queryTicketInTenant(ctx, tenantID, targetID); err != nil {
		return err
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("启动关联事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.Ticket.UpdateOneID(sourceID).
		Where(ticket.TenantIDEQ(tenantID), ticket.DeletedAtIsNil()).
		RemoveRelatedTicketIDs(targetID).
		Exec(ctx); err != nil {
		return fmt.Errorf("移除工单关联失败: %w", err)
	}
	if err := tx.Ticket.UpdateOneID(targetID).
		Where(ticket.TenantIDEQ(tenantID), ticket.DeletedAtIsNil()).
		RemoveRelatedTicketIDs(sourceID).
		Exec(ctx); err != nil {
		return fmt.Errorf("移除反向工单关联失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交关联事务失败: %w", err)
	}
	return nil
}
