package admin

import (
	"context"
	"encoding/json"
	"errors"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/mcpservertool"
	"itsm-backend/mcp/manager"
)

// EntStore 是 manager 运行态的 ent 持久化实现（M0-08）：
//   - manager.StatusWriter：`mcp_servers` 运行态回写；
//   - manager.ToolCache：`mcp_server_tools` 发现结果落库与差分收敛。
type EntStore struct {
	client *ent.Client
}

// NewEntStore 创建 store。
func NewEntStore(client *ent.Client) (*EntStore, error) {
	if client == nil {
		return nil, errors.New("MCP ent store 需要数据库客户端")
	}
	return &EntStore{client: client}, nil
}

// sysCtx 返回平台组件作用域 ctx：MCP manager 的运行态同步/工具缓存是系统任务，
// 不隶属单个租户；R2B 阴影观察（2026-10-03）前用裸 background，enforce 下会被
// RLS 装饰器 fail-closed 拦截（且丢失审计归因）。
func (s *EntStore) sysCtx(reason string) context.Context {
	return tenantctx.SystemContext(context.Background(), "mcp:admin:store", reason)
}

// UpdateServerStatus 实现 manager.StatusWriter（不含凭据；服务器已删除时忽略）。
func (s *EntStore) UpdateServerStatus(ctx context.Context, serverID int, patch manager.StatusPatch) error {
	// R2B 阴影观察（2026-10-03）：manager 运行态回写为平台组件任务（调用方可能带裸 ctx），
	// 显式 system 作用域，避免 enforce 下 fail-closed（唯一调用方=mcp manager）。
	ctx = tenantctx.SystemContext(ctx, "mcp:admin:store", "server status write (manager runtime)")
	update := s.client.MCPServer.UpdateOneID(serverID).
		SetStatus(string(patch.Status)).
		SetLastError(truncateText(patch.LastError, 2000)).
		SetProtocolVersion(truncateText(patch.ProtocolVersion, 32))
	if patch.ServerInfo != "" {
		update = update.SetServerInfo(patch.ServerInfo)
	}
	if !patch.ConnectedAt.IsZero() {
		connectedAt := patch.ConnectedAt
		update = update.SetNillableLastConnectedAt(&connectedAt)
	}
	err := update.Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

// List 实现 manager.ToolCache：返回服务器当前的工具治理记录。
// 说明：接口不带 error（manager 的健康循环不因 DB 故障中断）；查询失败返回空集合。
func (s *EntStore) List(serverID int) []manager.ToolRecord {
	rows, err := s.client.MCPServerTool.Query().
		Where(mcpservertool.ServerIDEQ(serverID)).
		Order(ent.Asc(mcpservertool.FieldRawName)).
		All(s.sysCtx("tool cache list"))
	if err != nil {
		return nil
	}
	records := make([]manager.ToolRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, manager.ToolRecord{
			ServerID:         row.ServerID,
			TenantID:         row.TenantID,
			RawName:          row.RawName,
			CallableName:     row.CallableName,
			Description:      row.Description,
			InputSchema:      json.RawMessage(row.InputSchema),
			SchemaHash:       row.SchemaHash,
			ReadOnly:         row.ReadOnly,
			Risk:             row.Risk,
			Category:         row.Category,
			Enabled:          row.Enabled,
			Healthy:          row.Healthy,
			Quarantined:      row.Quarantined,
			QuarantineReason: row.QuarantineReason,
			LastError:        row.LastError,
			DiscoveredAt:     row.DiscoveredAt,
			UpdatedAt:        row.UpdatedAt,
		})
	}
	return records
}

// Replace 实现 manager.ToolCache：把一次发现结果落库。
//
// 语义：
//   - 新工具：按 record 创建（默认 enabled=false、read_only=false、risk=high，来自 PlanDiscovery）；
//   - 已存在：更新元数据与隔离位；**不覆盖 enabled/read_only**（治理位由管理 API 维护，避免竞态回写）；
//   - canonical 名已被同租户其它服务器占用：跳过落库（唯一索引冲突），由 registry/M0-09 侧隔离；
//   - 消失的工具：healthy=false（保留治理位与历史，由管理端决定删除）。
func (s *EntStore) Replace(serverID int, records []manager.ToolRecord) {
	ctx := s.sysCtx("tool cache replace (discovery sync)")
	server, err := s.client.MCPServer.Get(ctx, serverID)
	if err != nil {
		return
	}

	existing, err := s.client.MCPServerTool.Query().
		Where(mcpservertool.TenantIDEQ(server.TenantID), mcpservertool.ServerIDEQ(serverID)).
		All(ctx)
	if err != nil {
		return
	}
	byRawName := make(map[string]*ent.MCPServerTool, len(existing))
	for _, row := range existing {
		byRawName[row.RawName] = row
	}

	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		seen[record.RawName] = struct{}{}
		risk := record.Risk
		if risk == "" {
			risk = "high"
		}
		if row, ok := byRawName[record.RawName]; ok {
			_, _ = s.client.MCPServerTool.UpdateOneID(row.ID).
				SetCallableName(record.CallableName).
				SetDescription(record.Description).
				SetInputSchema(string(record.InputSchema)).
				SetSchemaHash(record.SchemaHash).
				SetRisk(risk).
				SetCategory(record.Category).
				SetHealthy(true).
				SetQuarantined(record.Quarantined).
				SetQuarantineReason(truncateText(record.QuarantineReason, 500)).
				SetLastError("").
				Save(ctx)
			continue
		}

		conflict, _ := s.client.MCPServerTool.Query().
			Where(
				mcpservertool.TenantIDEQ(server.TenantID),
				mcpservertool.CallableNameEQ(record.CallableName),
				mcpservertool.ServerIDNEQ(serverID),
			).
			Exist(ctx)
		if conflict {
			continue
		}
		_, _ = s.client.MCPServerTool.Create().
			SetTenantID(server.TenantID).
			SetServerID(serverID).
			SetRawName(record.RawName).
			SetCallableName(record.CallableName).
			SetDescription(record.Description).
			SetInputSchema(string(record.InputSchema)).
			SetSchemaHash(record.SchemaHash).
			SetReadOnly(record.ReadOnly).
			SetRisk(risk).
			SetCategory(record.Category).
			SetEnabled(record.Enabled).
			SetHealthy(true).
			SetQuarantined(record.Quarantined).
			SetQuarantineReason(truncateText(record.QuarantineReason, 500)).
			SetDiscoveredAt(record.DiscoveredAt).
			Save(ctx)
	}

	missingIDs := make([]int, 0)
	for _, row := range existing {
		if _, ok := seen[row.RawName]; !ok {
			missingIDs = append(missingIDs, row.ID)
		}
	}
	if len(missingIDs) > 0 {
		_, _ = s.client.MCPServerTool.Update().
			Where(mcpservertool.IDIn(missingIDs...)).
			SetHealthy(false).
			Save(ctx)
	}
}

// DeleteServerTools 级联删除服务器工具缓存（删除服务器时调用）。
func (s *EntStore) DeleteServerTools(ctx context.Context, tenantID, serverID int) error {
	_, err := s.client.MCPServerTool.Delete().
		Where(mcpservertool.TenantIDEQ(tenantID), mcpservertool.ServerIDEQ(serverID)).
		Exec(ctx)
	return err
}

// ToolCounts 统计服务器的工具治理计数（列表/健康摘要用）。
func (s *EntStore) ToolCounts(ctx context.Context, tenantID, serverID int) (total, enabled, quarantined int, err error) {
	base := s.client.MCPServerTool.Query().Where(mcpservertool.TenantIDEQ(tenantID), mcpservertool.ServerIDEQ(serverID))
	total, err = base.Clone().Count(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	enabled, err = base.Clone().Where(mcpservertool.EnabledEQ(true)).Count(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	quarantined, err = base.Clone().Where(mcpservertool.QuarantinedEQ(true)).Count(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	return total, enabled, quarantined, nil
}

func truncateText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

var (
	_ manager.StatusWriter = (*EntStore)(nil)
	_ manager.ToolCache    = (*EntStore)(nil)
)
