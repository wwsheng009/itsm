package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/mcpserver"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/transport"
)

func (s *Service) ListServers(ctx context.Context, actor Actor) (ServerListResult, error) {
	rows, err := s.client.MCPServer.Query().
		Where(mcpserver.TenantIDEQ(actor.TenantID)).
		Order(ent.Asc(mcpserver.FieldName)).
		All(ctx)
	if err != nil {
		return ServerListResult{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查询服务器失败", err)
	}
	result := ServerListResult{Items: make([]ServerView, 0, len(rows))}
	for _, row := range rows {
		view, err := s.serverView(ctx, actor.TenantID, row)
		if err != nil {
			return ServerListResult{}, err
		}
		result.Items = append(result.Items, view)
		if view.Enabled {
			result.Summary.Enabled++
		}
		switch manager.ServerStatus(view.RunningStatus) {
		case manager.StatusHealthy:
			result.Summary.Connected++
		case manager.StatusError:
			result.Summary.Error++
		}
		result.Summary.Tools += view.ToolCount
		result.Summary.EnabledTools += view.EnabledToolCount
		result.Summary.QuarantinedTools += view.QuarantinedToolNum
	}
	result.Summary.Total = len(result.Items)
	return result, nil
}

// GetServer 返回单个服务器详情。
func (s *Service) GetServer(ctx context.Context, actor Actor, serverID int) (ServerView, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	return s.serverView(ctx, actor.TenantID, entity)
}

// —— 写（CRUD） ——

// CreateServer 新建服务器（默认 disabled；工具默认待治理）。
func (s *Service) CreateServer(ctx context.Context, actor Actor, req CreateServerRequest) (ServerView, error) {
	kind := transport.Kind(strings.TrimSpace(req.Transport))
	if err := ValidateServerName(req.Name); err != nil {
		return ServerView{}, err
	}
	if err := ValidateDisplayName(req.DisplayName); err != nil {
		return ServerView{}, err
	}
	if err := ValidateTransport(kind); err != nil {
		return ServerView{}, err
	}
	if err := ValidateURL(req.URL); err != nil {
		return ServerView{}, err
	}
	if err := ValidateTimeout(req.TimeoutMS); err != nil {
		return ServerView{}, err
	}
	if err := ValidateParallelCalls(req.MaxParallelCalls); err != nil {
		return ServerView{}, err
	}
	if err := ValidateRetry(req.MaxRetry); err != nil {
		return ServerView{}, err
	}
	credentialType, err := normalizeCredentialType(req.CredentialType)
	if err != nil {
		return ServerView{}, err
	}
	trustLevel, err := normalizeTrustLevel(req.TrustLevel)
	if err != nil {
		return ServerView{}, err
	}
	headers, err := NormalizeSecrets(req.Headers)
	if err != nil {
		return ServerView{}, err
	}
	credential, err := NormalizeSecrets(req.Credential)
	if err != nil {
		return ServerView{}, err
	}
	headersCipher, err := s.credentials.Encrypt(NewSecretValues(headers))
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}
	credentialCipher, err := s.credentials.Encrypt(NewSecretValues(credential))
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeCredentialError, "凭据加密失败", err)
	}

	duplicated, err := s.client.MCPServer.Query().
		Where(mcpserver.TenantIDEQ(actor.TenantID), mcpserver.NameEQ(strings.TrimSpace(req.Name))).
		Exist(ctx)
	if err != nil {
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "查重失败", err)
	}
	if duplicated {
		return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "name 已存在")
	}

	create := s.client.MCPServer.Create().
		SetTenantID(actor.TenantID).
		SetName(strings.TrimSpace(req.Name)).
		SetDisplayName(req.DisplayName).
		SetTransport(string(kind)).
		SetURL(strings.TrimSpace(req.URL)).
		SetHeadersEncrypted(headersCipher).
		SetCredentialType(credentialType).
		SetCredentialEncrypted(credentialCipher).
		SetTrustLevel(trustLevel).
		SetEnabled(false).
		SetStatus(string(manager.StatusConfigured))
	if req.TimeoutMS > 0 {
		create = create.SetTimeoutMs(req.TimeoutMS)
	}
	if req.MaxParallelCalls > 0 {
		create = create.SetMaxParallelCalls(req.MaxParallelCalls)
	}
	if req.MaxRetry > 0 {
		create = create.SetMaxRetry(req.MaxRetry)
	}
	entity, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "name 已存在")
		}
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "创建服务器失败", err)
	}

	s.recordAudit(ctx, actor, "create_server", "mcp_server", entity.Name, nil,
		s.serverAuditSnapshot(entity, NewSecretValues(headers).Masked(), NewSecretValues(credential).Masked()), nil)
	return s.serverView(ctx, actor.TenantID, entity)
}

// UpdateServer 编辑服务器（乐观锁；凭据空值 = 不修改；写后异步重连）。
func (s *Service) UpdateServer(ctx context.Context, actor Actor, serverID int, req UpdateServerRequest) (ServerView, error) {
	if req.Version <= 0 {
		return ServerView{}, NewAdminError(http.StatusBadRequest, CodeValidationFailed, "version 必填（乐观锁）")
	}
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	beforeHeaders, beforeCredential := s.maskedSecrets(entity)

	update := s.client.MCPServer.UpdateOneID(entity.ID).
		Where(mcpserver.VersionEQ(req.Version)).
		AddVersion(1)

	if req.DisplayName != nil {
		if err := ValidateDisplayName(*req.DisplayName); err != nil {
			return ServerView{}, err
		}
		update = update.SetDisplayName(*req.DisplayName)
	}
	if req.Transport != nil {
		kind := transport.Kind(strings.TrimSpace(*req.Transport))
		if err := ValidateTransport(kind); err != nil {
			return ServerView{}, err
		}
		update = update.SetTransport(string(kind))
	}
	if req.URL != nil {
		if err := ValidateURL(*req.URL); err != nil {
			return ServerView{}, err
		}
		update = update.SetURL(strings.TrimSpace(*req.URL))
	}
	if req.TimeoutMS != nil {
		if err := ValidateTimeout(*req.TimeoutMS); err != nil {
			return ServerView{}, err
		}
		update = update.SetTimeoutMs(*req.TimeoutMS)
	}
	if req.MaxParallelCalls != nil {
		if err := ValidateParallelCalls(*req.MaxParallelCalls); err != nil {
			return ServerView{}, err
		}
		update = update.SetMaxParallelCalls(*req.MaxParallelCalls)
	}
	if req.MaxRetry != nil {
		if err := ValidateRetry(*req.MaxRetry); err != nil {
			return ServerView{}, err
		}
		update = update.SetMaxRetry(*req.MaxRetry)
	}
	if req.TrustLevel != nil {
		trustLevel, err := normalizeTrustLevel(*req.TrustLevel)
		if err != nil {
			return ServerView{}, err
		}
		update = update.SetTrustLevel(trustLevel)
	}
	if req.CredentialType != nil {
		normalized, err := normalizeCredentialType(*req.CredentialType)
		if err != nil {
			return ServerView{}, err
		}
		update = update.SetCredentialType(normalized)
	}
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
		if ent.IsNotFound(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeConflict, "版本冲突，请刷新后重试")
		}
		if ent.IsConstraintError(err) {
			return ServerView{}, NewAdminError(http.StatusConflict, CodeDuplicateName, "更新违反唯一约束")
		}
		return ServerView{}, WrapAdminError(http.StatusInternalServerError, CodeInternal, "更新服务器失败", err)
	}

	updated, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ServerView{}, err
	}
	afterHeaders, afterCredential := s.maskedSecrets(updated)
	s.recordAudit(ctx, actor, "update_server", "mcp_server", updated.Name,
		s.serverAuditSnapshot(entity, beforeHeaders, beforeCredential),
		s.serverAuditSnapshot(updated, afterHeaders, afterCredential), nil)

	// 先落库再异步重连（§5.10）：启用中的服务器热重载新配置。
	if updated.Enabled {
		s.upsertManager(updated)
		_ = s.manager.Reload(ctx, updated.ID)
	}
	return s.serverView(ctx, actor.TenantID, updated)
}

// DeleteServer 删除服务器（M1-08：立即拒绝新调用 → 在途 ≤ 宽限期 → 强断并审计；级联工具缓存）。
func (s *Service) DeleteServer(ctx context.Context, actor Actor, serverID int) error {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return err
	}
	headers, credential := s.maskedSecrets(entity)
	// Retire 而非 Remove：保留 in-flight 宽限语义（宽限超时会发出审计事件）。
	_ = s.manager.Retire(ctx, entity.ID)
	if err := s.store.DeleteServerTools(ctx, actor.TenantID, entity.ID); err != nil {
		return WrapAdminError(http.StatusInternalServerError, CodeInternal, "清理工具缓存失败", err)
	}
	if err := s.client.MCPServer.DeleteOneID(entity.ID).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return NewAdminError(http.StatusNotFound, CodeNotFound, "服务器不存在")
		}
		return WrapAdminError(http.StatusInternalServerError, CodeInternal, "删除服务器失败", err)
	}
	s.recordAudit(ctx, actor, "delete_server", "mcp_server", entity.Name,
		s.serverAuditSnapshot(entity, headers, credential), nil, nil)
	return nil
}

// —— 测试连接（同步 ≤10s；不落库） ——

// testServerID 是测试连接使用的临时槽位（负值不与真实服务器冲突）。
const testServerID = -1

// TestServer 对已存服务器（可带覆盖项）做一次同步连接测试。
//
// 语义：只做一次短握手 + 工具预览，**不落库、不改运行态**；连接失败按 §5.5 返回映射错误
// （ssrf_blocked→422 / connect_timeout|tls_error|auth_required|protocol_mismatch→502 等）。
func (s *Service) TestServer(ctx context.Context, actor Actor, serverID int, req TestServerRequest) (ConnectionTestResult, error) {
	entity, err := s.serverEntity(ctx, actor.TenantID, serverID)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	cfg, err := s.buildServerConfig(entity)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	if strings.TrimSpace(req.Transport) != "" {
		kind := transport.Kind(strings.TrimSpace(req.Transport))
		if err := ValidateTransport(kind); err != nil {
			return ConnectionTestResult{}, err
		}
		cfg.Transport = kind
	}
	if strings.TrimSpace(req.URL) != "" {
		if err := ValidateURL(req.URL); err != nil {
			return ConnectionTestResult{}, err
		}
		cfg.URL = strings.TrimSpace(req.URL)
	}
	if len(req.Headers) > 0 {
		normalized, err := NormalizeSecrets(req.Headers)
		if err != nil {
			return ConnectionTestResult{}, err
		}
		for key, value := range normalized {
			cfg.Headers[key] = value
		}
	}
	if len(req.Credential) > 0 {
		normalized, err := NormalizeSecrets(req.Credential)
		if err != nil {
			return ConnectionTestResult{}, err
		}
		for key, value := range normalized {
			cfg.Headers[key] = value
		}
	}
	cfg.ID = testServerID
	cfg.Enabled = true

	tester := manager.New(manager.Options{
		Guard:          s.guard,
		ConnectTimeout: 10 * time.Second,
		CallTimeout:    10 * time.Second,
		HealthInterval: time.Hour,
		Now:            s.now,
	})
	defer tester.Stop()

	tester.Upsert(cfg)
	started := s.now()
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := tester.ConnectNow(testCtx, testServerID); err != nil {
		return ConnectionTestResult{}, MapTransportError(err)
	}
	discovery, discoverErr := tester.DiscoverNow(testCtx, testServerID)
	snapshot, _ := tester.Status(testServerID)
	result := ConnectionTestResult{
		OK:              true,
		ProtocolVersion: snapshot.ProtocolVersion,
		DurationMS:      s.now().Sub(started).Milliseconds(),
	}
	var info struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(snapshot.ServerInfo), &info); err == nil {
		result.ServerName = info.Name
		result.ServerVersion = info.Version
	}
	if discoverErr == nil {
		for _, record := range discovery.Records {
			result.Tools = append(result.Tools, ToolPreview{
				RawName:      record.RawName,
				CallableName: record.CallableName,
				Description:  record.Description,
			})
		}
		result.ToolCount = len(result.Tools)
	}
	tester.Remove(testServerID)
	return result, nil
}

// —— 启停 / 重载（异步：202 + 状态回读，D8） ——

// EnableServer 异步启用：落库 enabled=true，提交 manager 建连（返回时状态为 connecting）。
