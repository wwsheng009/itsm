package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/mcpserver"
	"itsm-backend/ent/mcpservertool"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"
)

// 管理服务（M0-08）：CRUD / 测试连接 / 异步启停 / 工具治理 / 健康与事件 / 审计。
//
// 设计约束：
//   - 凭据只写不读回：读接口一律 `Masked` 投影（M0-06）；
//   - 写路径先落库再异步重连（§5.10）；工具开关只翻转治理位、**不触发重连**（D6）；
//   - 审计字段 actor/tenant/action/object/before-after(脱敏)/result/ip/ts（§5.5）。

// Actor 是管理操作发起者（由 handler 从身份上下文提取）。
func (s *Service) EnableServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if !entity.Enabled {
		if _, err := s.client.MCPServer.UpdateOneID(entity.ID).SetEnabled(true).Save(ctx); err != nil {
			return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "启用失败", err)
		}
		entity.Enabled = true
	}
	s.upsertManager(entity)
	if err := s.manager.Enable(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "enable_server", "mcp_server", entity.Name, map[string]any{"enabled": false}, map[string]any{"enabled": true}, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// DisableServer 异步禁用：落库 enabled=false，后台宽限关闭连接。
func (s *Service) DisableServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if entity.Enabled {
		if _, err := s.client.MCPServer.UpdateOneID(entity.ID).SetEnabled(false).Save(ctx); err != nil {
			return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "禁用失败", err)
		}
		entity.Enabled = false
	}
	if err := s.manager.Disable(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "disable_server", "mcp_server", entity.Name, map[string]any{"enabled": true}, map[string]any{"enabled": false}, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// ReloadServer 异步重连 + 重新发现（服务器必须已启用）。
func (s *Service) ReloadServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if !entity.Enabled {
		return ServerView{}, NewAdminError(http.StatusConflict, CodeServerDisabled, "服务器未启用，无法重载")
	}
	s.upsertManager(entity)
	if err := s.manager.Reload(ctx, entity.ID); err != nil {
		return ServerView{}, MapTransportError(err)
	}
	s.recordAudit(ctx, actor, "reload_server", "mcp_server", entity.Name, nil, nil, nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// —— 工具治理（不触发重连，D6） ——

// ListTools 返回服务器全量工具（含禁用/隔离）。
func (s *Service) ListTools(ctx context.Context, actor Actor, serverID int) ([]ToolView, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return nil, err
	}
	rows, err := s.client.MCPServerTool.Query().
		Where(mcpservertool.TenantIDEQ(actor.TenantID), mcpservertool.ServerIDEQ(serverID)).
		Order(ent.Asc(mcpservertool.FieldRawName)).
		All(ctx)
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询工具失败", err)
	}
	runningState := s.runningState(serverID)
	views := make([]ToolView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toolView(row, runningState))
	}
	return views, nil
}

// SetToolEnabled 单工具启停（只翻转治理位，不重连）。
func (s *Service) SetToolEnabled(ctx context.Context, actor Actor, serverID int, callableName string, enabled bool) (ToolView, error) {
	row, err := s.toolEntity(ctx, actor.TenantID, serverID, callableName)
	if err != nil {
		return ToolView{}, err
	}
	updated, err := s.client.MCPServerTool.UpdateOneID(row.ID).SetEnabled(enabled).Save(ctx)
	if err != nil {
		return ToolView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新工具状态失败", err)
	}
	s.recordAudit(ctx, actor, "set_tool_enabled", "mcp_tool", updated.CallableName,
		map[string]any{"enabled": row.Enabled, "server": serverID}, map[string]any{"enabled": enabled, "server": serverID}, nil)
	return toolView(updated, s.runningState(serverID)), nil
}

// BulkSetTools 批量启停（tools 为空 = 全部工具）。
func (s *Service) BulkSetTools(ctx context.Context, actor Actor, serverID int, req BulkToolRequest) (int, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return 0, err
	}
	update := s.client.MCPServerTool.Update().
		Where(mcpservertool.TenantIDEQ(actor.TenantID), mcpservertool.ServerIDEQ(serverID))
	if len(req.Tools) > 0 {
		update = update.Where(mcpservertool.CallableNameIn(req.Tools...))
	}
	affected, err := update.SetEnabled(req.Enabled).Save(ctx)
	if err != nil {
		return 0, WrapAdminError(http.StatusInternalServerError, CodeInternal, "批量更新失败", err)
	}
	s.recordAudit(ctx, actor, "bulk_set_tools", "mcp_tool", "server:"+strconv.Itoa(serverID),
		map[string]any{"tools": len(req.Tools)}, map[string]any{"enabled": req.Enabled, "affected": affected}, nil)
	return affected, nil
}

// SetToolClassification 标注 read_only/risk/category（影响审批，必审计）。
func (s *Service) SetToolClassification(ctx context.Context, actor Actor, serverID int, callableName string, req ClassificationRequest) (ToolView, error) {
	row, err := s.toolEntity(ctx, actor.TenantID, serverID, callableName)
	if err != nil {
		return ToolView{}, err
	}
	update := s.client.MCPServerTool.UpdateOneID(row.ID)
	before := map[string]any{"read_only": row.ReadOnly, "risk": row.Risk, "category": row.Category}
	after := map[string]any{"read_only": row.ReadOnly, "risk": row.Risk, "category": row.Category}

	if req.ReadOnly != nil {
		update = update.SetReadOnly(*req.ReadOnly)
		after["read_only"] = *req.ReadOnly
	}
	if strings.TrimSpace(req.Risk) != "" {
		risk := strings.TrimSpace(req.Risk)
		if !validRisk(risk) {
			return ToolView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "risk 仅允许 read|plan|act_low|act_medium|act_high")
		}
		update = update.SetRisk(risk)
		after["risk"] = risk
	}
	if strings.TrimSpace(req.Category) != "" {
		category := strings.TrimSpace(req.Category)
		if len(category) > 32 {
			return ToolView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "category 超长（≤32）")
		}
		update = update.SetCategory(category)
		after["category"] = category
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return ToolView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新工具标注失败", err)
	}
	s.recordAudit(ctx, actor, "set_tool_classification", "mcp_tool", updated.CallableName, before, after, nil)
	return toolView(updated, s.runningState(serverID)), nil
}

// RotateCredential 轮换凭据/请求头（先落库再异步重连；旧值不再引用）。
func (s *Service) RotateCredential(ctx context.Context, actor Actor, serverID int, req RotateCredentialRequest) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	credentialType := entity.CredentialType
	if strings.TrimSpace(req.CredentialType) != "" {
		normalized, err := normalizeCredentialType(req.CredentialType)
		if err != nil {
			return ServerView{}, err
		}
		credentialType = normalized
	}
	update := s.client.MCPServer.UpdateOneID(entity.ID).AddVersion(1).SetCredentialType(credentialType)
	if len(req.Headers) > 0 {
		normalized, err := NormalizeSecrets(req.Headers)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.HeadersEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.HeadersEncrypted {
			update = update.SetHeadersEncrypted(mergedCipher)
		}
	}
	if len(req.Credential) > 0 {
		normalized, err := NormalizeSecrets(req.Credential)
		if err != nil {
			return ServerView{}, err
		}
		mergedCipher, err := s.mergeSecrets(entity.CredentialEncrypted, normalized)
		if err != nil {
			return ServerView{}, err
		}
		if mergedCipher != entity.CredentialEncrypted {
			update = update.SetCredentialEncrypted(mergedCipher)
		}
	}
	if _, err := update.Save(ctx); err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "轮换凭据失败", err)
	}
	updated, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	afterHeaders, afterCredential := s.maskedSecrets(updated)
	s.recordAudit(ctx, actor, "rotate_credential", "mcp_server", updated.Name, nil,
		s.serverAuditSnapshot(updated, afterHeaders, afterCredential), nil)

	if updated.Enabled {
		s.upsertManager(updated)
		_ = s.manager.Reload(ctx, updated.ID)
	}
	return s.serverView(ctx, actor.TenantID, updated)
}

// —— 健康与事件 ——

// HealthSummary 返回租户健康摘要（供管理页 Header 与告警）。
func (s *Service) HealthSummary(ctx context.Context, actor Actor) (ServerListResult, error) {
	list, err := s.ListServers(ctx, actor)
	if err != nil {
		return ServerListResult{}, err
	}
	authRequired := 0
	for _, item := range list.Items {
		if s.lastAuthRequired(item.ID) {
			authRequired++
		}
	}
	summary := list.Summary
	summary.AuthRequired = authRequired
	list.Summary = summary
	return list, nil
}

// ServerEvents 返回服务器近期生命周期事件（时间升序）。
func (s *Service) ServerEvents(ctx context.Context, actor Actor, serverID int) ([]manager.Event, error) {
	if _, err := s.serverEntity(ctx, actor.TenantID, serverID); err != nil {
		return nil, err
	}
	return s.events.List(serverID), nil
}

// —— 内部：视图与装载 ——

func (s *Service) serverEntity(ctx context.Context, tenantID, serverID int) (*ent.MCPServer, error) {
	entity, err := s.client.MCPServer.Query().
		Where(mcpserver.IDEQ(serverID), mcpserver.TenantIDEQ(tenantID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, NewAdminError(http.StatusNotFound, CodeNotFound, "服务器不存在")
	}
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询服务器失败", err)
	}
	return entity, nil
}

func (s *Service) toolEntity(ctx context.Context, tenantID, serverID int, callableName string) (*ent.MCPServerTool, error) {
	row, err := s.client.MCPServerTool.Query().
		Where(
			mcpservertool.TenantIDEQ(tenantID),
			mcpservertool.ServerIDEQ(serverID),
			mcpservertool.CallableNameEQ(strings.TrimSpace(callableName)),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, NewAdminError(http.StatusNotFound, CodeNotFound, "工具不存在")
	}
	if err != nil {
		return nil, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询工具失败", err)
	}
	return row, nil
}

// serverView 组装服务器视图（凭据掩码；运行位以 manager 为准，DB 状态为回写快照）。
func (s *Service) serverView(ctx context.Context, tenantID int, entity *ent.MCPServer) (ServerView, error) {
	headers, credential := s.maskedSecrets(entity)
	total, enabledTools, quarantined, err := s.store.ToolCounts(ctx, tenantID, entity.ID)
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "统计工具失败", err)
	}
	running := s.runningState(entity.ID)
	status := entity.Status
	if running != "" {
		status = running
	}
	return ServerView{
		ID:                 entity.ID,
		Name:               entity.Name,
		DisplayName:        entity.DisplayName,
		Transport:          entity.Transport,
		URL:                entity.URL,
		CredentialType:     entity.CredentialType,
		TrustLevel:         entity.TrustLevel,
		Enabled:            entity.Enabled,
		Status:             status,
		RunningStatus:      running,
		LastError:          entity.LastError,
		ProtocolVersion:    entity.ProtocolVersion,
		ServerInfo:         entity.ServerInfo,
		TimeoutMS:          entity.TimeoutMs,
		MaxParallelCalls:   entity.MaxParallelCalls,
		MaxRetry:           entity.MaxRetry,
		Version:            entity.Version,
		ToolCount:          total,
		EnabledToolCount:   enabledTools,
		QuarantinedToolNum: quarantined,
		HeadersMasked:      headers,
		CredentialMasked:   credential,
		// M1-08：有效执行策略回读（§5.4 默认表 + 服务器覆盖；输出上限在 provider 层）。
		Policy:    s.policyView(entity),
		CreatedAt: entity.CreatedAt,
		UpdatedAt: entity.UpdatedAt,
	}, nil
}

// maskedSecrets 解密并掩码（解密失败按空集处理：不阻断列表，运行期自会暴露 last_error）。
func (s *Service) maskedSecrets(entity *ent.MCPServer) (map[string]string, map[string]string) {
	headers := map[string]string{}
	if entity.HeadersEncrypted != "" {
		if values, err := s.credentials.Decrypt(entity.HeadersEncrypted); err == nil {
			headers = values.Masked()
		}
	}
	credential := map[string]string{}
	if entity.CredentialEncrypted != "" {
		if values, err := s.credentials.Decrypt(entity.CredentialEncrypted); err == nil {
			credential = values.Masked()
		}
	}
	return headers, credential
}

// mergeSecrets 在既有密文上应用更新：patch 空值 = 保留原值；
// 合并结果与现值一致时**返回原密文**（避免随机 nonce 造成的无谓改写与乐观锁噪声）。
func (s *Service) mergeSecrets(existingCipher string, patch map[string]string) (string, error) {
	current, err := s.credentials.Decrypt(existingCipher)
	if err != nil {
		return "", WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据解密失败", err)
	}
	merged := current.ApplyPatch(patch)
	if sameSecretValues(current, merged) {
		return existingCipher, nil
	}
	ciphertext, err := s.credentials.Encrypt(merged)
	if err != nil {
		return "", WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}
	return ciphertext, nil
}

func sameSecretValues(left, right SecretValues) bool {
	if left.Len() != right.Len() {
		return false
	}
	leftValues := left.Values()
	rightValues := right.Values()
	for key, value := range leftValues {
		if other, ok := rightValues[key]; !ok || other != value {
			return false
		}
	}
	return true
}

// shortError 截断错误文本（审计/日志用，≤200 字符；不用于对外响应）。
func shortError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

// serverAuditSnapshot 生成审计快照（不含明文；凭据只保留掩码）。
func (s *Service) serverAuditSnapshot(entity *ent.MCPServer, headers, credential map[string]string) map[string]any {
	return map[string]any{
		"id":                entity.ID,
		"name":              entity.Name,
		"display_name":      entity.DisplayName,
		"transport":         entity.Transport,
		"url":               entity.URL,
		"credential_type":   entity.CredentialType,
		"trust_level":       entity.TrustLevel,
		"enabled":           entity.Enabled,
		"status":            entity.Status,
		"timeout_ms":        entity.TimeoutMs,
		"max_parallel":      entity.MaxParallelCalls,
		"max_retry":         entity.MaxRetry,
		"version":           entity.Version,
		"headers_masked":    toAnyMap(headers),
		"credential_masked": toAnyMap(credential),
	}
}

// buildServerConfig 构建 manager 配置（解密凭据注入请求头；凭据键覆盖同名请求头）。
func (s *Service) buildServerConfig(entity *ent.MCPServer) (manager.ServerConfig, error) {
	headers := map[string]string{}
	if entity.HeadersEncrypted != "" {
		values, err := s.credentials.Decrypt(entity.HeadersEncrypted)
		if err != nil {
			return manager.ServerConfig{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "请求头解密失败", err)
		}
		headers = values.Values()
	}
	if entity.CredentialEncrypted != "" {
		values, err := s.credentials.Decrypt(entity.CredentialEncrypted)
		if err != nil {
			return manager.ServerConfig{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据解密失败", err)
		}
		for key, value := range values.Values() {
			headers[key] = value
		}
	}
	return manager.ServerConfig{
		ID:               entity.ID,
		TenantID:         entity.TenantID,
		Name:             entity.Name,
		Transport:        transport.Kind(entity.Transport),
		URL:              entity.URL,
		Headers:          headers,
		TimeoutMS:        entity.TimeoutMs,
		MaxParallelCalls: entity.MaxParallelCalls,
		MaxRetry:         entity.MaxRetry,
		Enabled:          entity.Enabled,
		TrustLevel:       entity.TrustLevel,
		Version:          entity.Version,
	}, nil
}

// upsertManager 把最新配置装入 manager（不建连；启停/重载另行触发）。
func (s *Service) upsertManager(entity *ent.MCPServer) {
	cfg, err := s.buildServerConfig(entity)
	if err != nil {
		return // 凭据解密失败：保留旧配置，运行态由 last_error 暴露
	}
	s.manager.Upsert(cfg)
}

func (s *Service) runningState(serverID int) string {
	snapshot, err := s.manager.Status(serverID)
	if err != nil {
		return ""
	}
	return string(snapshot.Status)
}

// lastAuthRequired 判定最近一次连接结论是否为「认证失效」。
func (s *Service) lastAuthRequired(serverID int) bool {
	events := s.events.List(serverID)
	for index := len(events) - 1; index >= 0; index-- {
		switch events[index].Type {
		case manager.EventServerAuthRequired:
			return true
		case manager.EventServerConnected:
			return false
		}
	}
	return false
}

func (s *Service) recordAudit(ctx context.Context, actor Actor, action, objectType, objectID string, before, after map[string]any, cause error) {
	entry := AuditEntry{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     action,
		ObjectType: objectType,
		ObjectID:   objectID,
		Before:     before,
		After:      after,
		Result:     "success",
		IP:         actor.IP,
		At:         s.now(),
	}
	if cause != nil {
		entry.Result = "failure"
		if adminErr, ok := AsAdminError(cause); ok {
			entry.ErrorCode = string(adminErr.Code)
		} else {
			entry.ErrorCode = string(CodeInternal)
		}
	}
	_ = s.audit.RecordMCPAudit(ctx, entry)
}

func toolView(row *ent.MCPServerTool, runningState string) ToolView {
	effective := row.Enabled && row.Healthy && !row.Quarantined
	return ToolView{
		ID:                 row.ID,
		RawName:            row.RawName,
		CallableName:       row.CallableName,
		Description:        row.Description,
		InputSchema:        row.InputSchema,
		SchemaHash:         row.SchemaHash,
		ReadOnly:           row.ReadOnly,
		Risk:               row.Risk,
		Category:           row.Category,
		Enabled:            effective,
		Healthy:            row.Healthy,
		ConfiguredEnabled:  row.Enabled,
		Quarantined:        row.Quarantined,
		QuarantineReason:   row.QuarantineReason,
		LastError:          row.LastError,
		DiscoveredAt:       row.DiscoveredAt,
		UpdatedAt:          row.UpdatedAt,
		ServerRunningState: runningState,
	}
}

func normalizeCredentialType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "none":
		return "none", nil
	case "static_header":
		return "static_header", nil
	case "oauth2":
		return "oauth2", nil
	default:
		return "", NewAdminError(http.StatusBadRequest, CodeValidationFailed, "credential_type 仅允许 none|static_header|oauth2")
	}
}

func normalizeTrustLevel(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "untrusted":
		return "untrusted", nil
	case "trusted":
		return "trusted", nil
	default:
		return "", NewAdminError(http.StatusBadRequest, CodeValidationFailed, "trust_level 仅允许 trusted|untrusted")
	}
}

func validRisk(value string) bool {
	switch value {
	case "read", "plan", "act_low", "act_medium", "act_high":
		return true
	default:
		return false
	}
}
