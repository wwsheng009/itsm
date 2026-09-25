package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/llmproviderconfig"
	"itsm-backend/ent/llmuserpreference"
	"itsm-backend/service"
)

// 本文件是主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-4 的管理服务层：
// ent 直连 CRUD + 协议槽位校验（BE-8）+ 密钥加密（EncryptionService）/掩码（common.MaskSecret）
// + 审计（复用既有 AI 审计通道：ai_feedbacks.item_type='ai_audit'）+ import-static 幂等
// + 个人偏好 upsert。（§3.4 契约、§3.5 安全）
//
// 依赖以窄接口注入，便于单测：
//   - ent client：数据面（所有查询强制 tenant_id 过滤 + deleted_at IS NULL）；
//   - LLMAdminSecretCipher：*middleware.EncryptionService 天然满足；
//   - LLMAdminInvalidator：*service.LLMProviderRegistry 天然满足（BE-5 接线，可空）；
//   - LLMAdminAuditSink：*Service（handlers/ai）天然满足（SaveFeedback → ai_feedbacks）；
//   - logger：结构化日志（密钥始终掩码，零明文）。
//
// 安全红线：入参 apiKey 只在此层加密；日志只出现掩码；响应 DTO 绝不携带明文/密文。

// 管理 API 错误码（§3.4）。AI_PROVIDER_* 与 service 层哨兵字符串一致，
// AI_PROTOCOL_* 复用 BE-8 哨兵字符串；其余为管理面校验码（§3.4 未逐字规定 422 分支码值，
// 仅规定 HTTP 状态，见实现报告「偏差」一节）。
const (
	LLMAdminCodeNotFound        = "AI_PROVIDER_NOT_FOUND"
	LLMAdminCodeDisabled        = "AI_PROVIDER_DISABLED"
	LLMAdminCodeKeyMissing      = "AI_PROVIDER_KEY_MISSING"
	LLMAdminCodeUnavailable     = "AI_PROVIDER_UNAVAILABLE"
	LLMAdminCodeIsDefault       = "AI_PROVIDER_IS_DEFAULT"
	LLMAdminCodeProtocolImpl    = "AI_PROTOCOL_NOT_IMPLEMENTED"
	LLMAdminCodeNameConflict    = "AI_PROVIDER_NAME_CONFLICT"
	LLMAdminCodeNameInvalid     = "AI_PROVIDER_NAME_INVALID"
	LLMAdminCodeEndpointMissing = "AI_PROVIDER_ENDPOINT_REQUIRED"
	LLMAdminCodeValidation      = "AI_PROVIDER_VALIDATION_ERROR"
	LLMAdminCodeInternal        = "AI_PROVIDER_INTERNAL_ERROR"
)

// llmAdminStaticSource import-static 写入的 source 值（§3.4）。
const llmAdminStaticSource = "imported"

// llmProviderNamePattern schema 层约束的 API 可见 key 形态（ent/schema/llm_provider_config.go）。
var llmProviderNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// LLMAdminSecretCipher 密钥加解密窄接口（Encrypt 入库、Decrypt 仅用于掩码预览）。
type LLMAdminSecretCipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// LLMAdminInvalidator 注册表缓存失效器（可空；*service.LLMProviderRegistry 满足）。
type LLMAdminInvalidator interface {
	Invalidate(tenantID int)
}

// LLMAdminAuditSink 既有 AI 审计写入点（*Service.SaveFeedback 满足；
// item_type='ai_audit' 即既有审计通道，不新增旁路表）。
type LLMAdminAuditSink interface {
	SaveFeedback(ctx context.Context, tenantID, userID int, requestID, kind, query, itemType string, itemID *int, useful bool, score *int, notes *string) error
}

// LLMProviderAdminDeps 管理服务依赖集合。
type LLMProviderAdminDeps struct {
	Client      *ent.Client
	Encrypter   LLMAdminSecretCipher
	Invalidator LLMAdminInvalidator
	Logger      *zap.SugaredLogger
	Audit       LLMAdminAuditSink
}

// LLMProviderAdminService 管理 API 服务层。
type LLMProviderAdminService struct {
	client      *ent.Client
	encrypter   LLMAdminSecretCipher
	invalidator LLMAdminInvalidator
	logger      *zap.SugaredLogger
	audit       LLMAdminAuditSink
}

// NewLLMProviderAdminService 构造管理服务（依赖可空，缺失在调用点显式失败，不 panic）。
func NewLLMProviderAdminService(deps LLMProviderAdminDeps) *LLMProviderAdminService {
	return &LLMProviderAdminService{
		client:      deps.Client,
		encrypter:   deps.Encrypter,
		invalidator: deps.Invalidator,
		logger:      deps.Logger,
		audit:       deps.Audit,
	}
}

// LLMAdminError 管理 API 领域错误：HTTP 状态 + 契约错误码字符串 + 可读消息。
type LLMAdminError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *LLMAdminError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *LLMAdminError) Unwrap() error { return e.Err }

func newLLMAdminError(status int, code, message string) *LLMAdminError {
	return &LLMAdminError{Status: status, Code: code, Message: message}
}

// mapLLMAdminError 把服务层/注册表哨兵错误映射为契约错误（404/409/422/503）。
func mapLLMAdminError(err error, notFoundMessage string) error {
	if err == nil {
		return nil
	}
	var adminErr *LLMAdminError
	if errors.As(err, &adminErr) {
		return adminErr
	}
	switch {
	case errors.Is(err, service.ErrProviderNotFound):
		return &LLMAdminError{Status: 404, Code: LLMAdminCodeNotFound, Message: notFoundMessage, Err: err}
	case errors.Is(err, service.ErrProviderDisabled):
		return &LLMAdminError{Status: 409, Code: LLMAdminCodeDisabled, Message: "实例已禁用", Err: err}
	case errors.Is(err, service.ErrProviderKeyMissing):
		return &LLMAdminError{Status: 422, Code: LLMAdminCodeKeyMissing, Message: "实例密钥缺失或解密失败", Err: err}
	case errors.Is(err, service.ErrProtocolNotImplemented):
		return &LLMAdminError{Status: 422, Code: LLMAdminCodeProtocolImpl, Message: "协议尚未实现（槽位）", Err: err}
	case errors.Is(err, service.ErrProtocolInvalid):
		return &LLMAdminError{Status: 422, Code: "AI_PROTOCOL_INVALID", Message: "协议枚举非法", Err: err}
	case errors.Is(err, service.ErrProtocolVariantInvalid):
		return &LLMAdminError{Status: 422, Code: "AI_PROTOCOL_VARIANT_INVALID", Message: "协议变体非法", Err: err}
	case errors.Is(err, service.ErrAdapterOptionsInvalid):
		return &LLMAdminError{Status: 422, Code: "AI_ADAPTER_OPTIONS_INVALID", Message: "adapter_options 非法", Err: err}
	case errors.Is(err, service.ErrProviderUnavailable):
		return &LLMAdminError{Status: 503, Code: LLMAdminCodeUnavailable, Message: "实例不可用", Err: err}
	default:
		return &LLMAdminError{Status: 500, Code: LLMAdminCodeInternal, Message: "服务内部错误", Err: err}
	}
}

// ensureReady 校验数据面与加密面就绪。
func (s *LLMProviderAdminService) ensureReady(requireEncrypter bool) error {
	if s.client == nil {
		return newLLMAdminError(503, LLMAdminCodeUnavailable, "数据面未就绪")
	}
	if requireEncrypter && s.encrypter == nil {
		return newLLMAdminError(503, LLMAdminCodeUnavailable, "加密服务未就绪（多 Provider 已按 §3.5 自动关闭语义处理）")
	}
	return nil
}

func (s *LLMProviderAdminService) invalidate(tenantID int) {
	if s.invalidator != nil {
		s.invalidator.Invalidate(tenantID)
	}
}

// recordAudit 写一条 AI 审计事件（创建/更新/删除/设默认/测试连通 各一条）。
// 审计失败只告警不阻断业务；日志与 notes 均不含明文密钥。
func (s *LLMProviderAdminService) recordAudit(ctx context.Context, tenantID, userID int, action, providerKey string, extra map[string]interface{}) {
	fields := map[string]interface{}{
		"action":      action,
		"providerKey": providerKey,
		"tenantId":    tenantID,
		"userId":      userID,
	}
	for key, value := range extra {
		fields[key] = value
	}
	if s.logger != nil {
		s.logger.Infow("llm provider admin action",
			"action", action, "provider_key", providerKey,
			"tenant_id", tenantID, "user_id", userID)
	}
	if s.audit == nil {
		return
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return
	}
	note := string(payload)
	requestID := fmt.Sprintf("llm_provider_admin_%d_%d", time.Now().UnixNano(), userID)
	useful := true
	if err := s.audit.SaveFeedback(ctx, tenantID, userID, requestID, "llm_provider_admin", providerKey, "ai_audit", nil, useful, nil, &note); err != nil {
		if s.logger != nil {
			s.logger.Warnw("llm provider admin audit write failed", "action", action, "error", err)
		}
	}
}

// decodeAdapterOptions 校验（BE-8：JSON 对象/≤4KB/敏感键黑名单/嵌套上限）并解码 adapter_options。
func decodeAdapterOptions(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if err := service.ValidateAdapterOptions(string(raw)); err != nil {
		return nil, mapLLMAdminError(err, "")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}
	options := map[string]interface{}{}
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, newLLMAdminError(422, "AI_ADAPTER_OPTIONS_INVALID", "adapter_options 不是合法 JSON 对象")
	}
	return options, nil
}

// validateName 校验实例 key（正则 + 长度），非法 → 422。
func validateName(name string) error {
	if !llmProviderNamePattern.MatchString(name) || len(name) > 64 {
		return newLLMAdminError(422, LLMAdminCodeNameInvalid,
			"name 必须匹配 ^[a-z0-9][a-z0-9_-]*$ 且长度 ≤64")
	}
	return nil
}

// validateProtocolSlot 校验 (protocol, variant) 槽位（BE-8 单一真源）。
func validateProtocolSlot(protocolName, variant string) error {
	if err := service.ValidateLLMProtocolVariant(protocolName, variant); err != nil {
		return mapLLMAdminError(err, "")
	}
	return nil
}

// validateEndpointRequirements 端点/部署必填规则（§3.4「endpoint 缺失 → 422」）：
// azure 变体必须显式给出 endpoint（无法回退内置默认）与 deployment。
func validateEndpointRequirements(protocolName, variant, endpoint, deployment string) error {
	if variant == service.LLMVariantAzure {
		if strings.TrimSpace(endpoint) == "" {
			return newLLMAdminError(422, LLMAdminCodeEndpointMissing, "variant=azure 必须提供 endpoint")
		}
		if strings.TrimSpace(deployment) == "" {
			return newLLMAdminError(422, LLMAdminCodeValidation, "variant=azure 必须提供 deployment")
		}
	}
	_ = protocolName
	return nil
}

// loadRecord 按 (tenant, id, 未软删) 取实例；不存在/跨租户一律 404（不泄漏存在性）。
func (s *LLMProviderAdminService) loadRecord(ctx context.Context, tenantID, id int) (*ent.LLMProviderConfig, error) {
	if id <= 0 {
		return nil, newLLMAdminError(400, LLMAdminCodeValidation, "id 非法")
	}
	record, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.IDEQ(id),
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newLLMAdminError(404, LLMAdminCodeNotFound, "实例不存在")
		}
		return nil, mapLLMAdminError(err, "实例不存在")
	}
	return record, nil
}

// maskedKey 生成掩码预览：解密成功 → common.MaskSecret；解密失败 → "****"（绝不明文回显）。
func (s *LLMProviderAdminService) maskedKey(ciphertext string) string {
	if strings.TrimSpace(ciphertext) == "" {
		return ""
	}
	if s.encrypter == nil {
		return "****"
	}
	plaintext, err := s.encrypter.Decrypt(ciphertext)
	if err != nil || plaintext == "" {
		return "****"
	}
	return common.MaskSecret(plaintext)
}

// toProviderDTO 实体 → 响应 DTO（无明文密钥/密文）。
func (s *LLMProviderAdminService) toProviderDTO(record *ent.LLMProviderConfig) dto.LLMProviderDTO {
	item := dto.LLMProviderDTO{
		ID:           record.ID,
		Key:          record.Name,
		DisplayName:  record.DisplayName,
		Protocol:     record.Protocol,
		Variant:      record.Variant,
		Model:        record.Model,
		Endpoint:     record.Endpoint,
		Deployment:   record.Deployment,
		Enabled:      record.Enabled,
		IsDefault:    record.IsDefault,
		Source:       record.Source,
		Status:       record.Status,
		LastError:    record.LastError,
		LastTestedAt: record.LastTestedAt,
		HasAPIKey:    strings.TrimSpace(record.EncryptedAPIKey) != "",
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
	}
	if item.HasAPIKey {
		item.MaskedAPIKey = s.maskedKey(record.EncryptedAPIKey)
	}
	if len(record.AdapterOptions) > 0 {
		item.AdapterOptions = record.AdapterOptions
	}
	return item
}

// ListProviders GET /ai/providers：本租户未软删实例列表（含掩码密钥、状态、默认标记）。
func (s *LLMProviderAdminService) ListProviders(ctx context.Context, tenantID int) (dto.LLMProviderListResponse, error) {
	response := dto.LLMProviderListResponse{
		Items:           []dto.LLMProviderDTO{},
		ProtocolOptions: protocolOptionDTOs(),
	}
	if err := s.ensureReady(false); err != nil {
		return response, err
	}
	records, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Order(ent.Desc(llmproviderconfig.FieldIsDefault), ent.Asc(llmproviderconfig.FieldName)).
		All(ctx)
	if err != nil {
		return response, mapLLMAdminError(err, "")
	}
	for _, record := range records {
		response.Items = append(response.Items, s.toProviderDTO(record))
	}
	response.Total = len(response.Items)
	return response, nil
}

// ListAvailable GET /ai/providers/available：选择器数据（仅启用且未软删；无密钥字段）。
func (s *LLMProviderAdminService) ListAvailable(ctx context.Context, tenantID int) ([]dto.LLMProviderAvailableDTO, error) {
	if err := s.ensureReady(false); err != nil {
		return nil, err
	}
	records, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.Enabled(true),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Order(ent.Desc(llmproviderconfig.FieldIsDefault), ent.Asc(llmproviderconfig.FieldName)).
		All(ctx)
	if err != nil {
		return nil, mapLLMAdminError(err, "")
	}
	items := make([]dto.LLMProviderAvailableDTO, 0, len(records))
	for _, record := range records {
		item := dto.LLMProviderAvailableDTO{
			Key:         record.Name,
			DisplayName: record.DisplayName,
			Protocol:    record.Protocol,
			Variant:     record.Variant,
			Model:       record.Model,
			IsDefault:   record.IsDefault,
		}
		if capabilities, capErr := service.LLMProtocolCapabilities(record.Protocol, record.Variant); capErr == nil {
			item.SupportsStream = capabilities.SupportsStream
			item.SupportsTools = capabilities.SupportsTools
			item.SupportsReasoning = capabilities.SupportsReasoning
			item.Implemented = capabilities.Implemented
		}
		items = append(items, item)
	}
	return items, nil
}

// protocolOptionDTOs 协议下拉选项（BE-8 LLMProtocolOptions 的 DTO 投影，供前端渲染 4 值）。
func protocolOptionDTOs() []dto.LLMProtocolOptionDTO {
	options := service.LLMProtocolOptions()
	out := make([]dto.LLMProtocolOptionDTO, 0, len(options))
	for _, option := range options {
		out = append(out, dto.LLMProtocolOptionDTO{
			Protocol:    option.Protocol,
			Implemented: option.Implemented,
			Variants:    option.Variants,
			Capabilities: dto.LLMCapabilitiesDTO{
				SupportsStream:    option.Capabilities.SupportsStream,
				SupportsTools:     option.Capabilities.SupportsTools,
				SupportsReasoning: option.Capabilities.SupportsReasoning,
				Implemented:       option.Capabilities.Implemented,
			},
		})
	}
	return out
}

// CreateProvider POST /ai/providers：新建实例（422 协议/variant/adapter_options/endpoint，
// 409 name 冲突；密钥加密落库；isDefault=true 时事务内先清后置）。
func (s *LLMProviderAdminService) CreateProvider(ctx context.Context, tenantID, userID int, req dto.LLMCreateProviderRequest) (dto.LLMProviderDTO, error) {
	if err := s.ensureReady(true); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	name := strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = name
	}
	if len(displayName) > 100 {
		return dto.LLMProviderDTO{}, newLLMAdminError(422, LLMAdminCodeValidation, "displayName 长度必须 ≤100")
	}
	protocolName := service.NormalizeLLMProtocol(req.Protocol)
	variant := service.NormalizeLLMVariant(req.Variant)
	if err := validateProtocolSlot(protocolName, variant); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	deployment := strings.TrimSpace(req.Deployment)
	if err := validateEndpointRequirements(protocolName, variant, endpoint, deployment); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	options, err := decodeAdapterOptions(req.AdapterOptions)
	if err != nil {
		return dto.LLMProviderDTO{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ciphertext := ""
	if strings.TrimSpace(req.APIKey) != "" {
		ciphertext, err = s.encrypter.Encrypt(req.APIKey)
		if err != nil {
			return dto.LLMProviderDTO{}, &LLMAdminError{Status: 500, Code: LLMAdminCodeInternal, Message: "密钥加密失败", Err: err}
		}
	}

	conflicts, err := s.client.LLMProviderConfig.Query().
		Where(llmproviderconfig.TenantIDEQ(tenantID), llmproviderconfig.NameEQ(name)).
		Count(ctx)
	if err != nil {
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	if conflicts > 0 {
		return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "name 已存在（含已删除实例占用的物理唯一键）")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	builder := tx.LLMProviderConfig.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetDisplayName(displayName).
		SetProtocol(protocolName).
		SetVariant(variant).
		SetModel(strings.TrimSpace(req.Model)).
		SetEndpoint(endpoint).
		SetDeployment(deployment).
		SetEnabled(enabled).
		SetIsDefault(false).
		SetSource("manual").
		SetStatus("configured")
	if ciphertext != "" {
		builder = builder.SetEncryptedAPIKey(ciphertext)
	}
	if len(options) > 0 {
		builder = builder.SetAdapterOptions(options)
	}
	record, err := builder.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsConstraintError(err) {
			return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "name 已存在")
		}
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	if req.IsDefault {
		if !enabled {
			_ = tx.Rollback()
			return dto.LLMProviderDTO{}, newLLMAdminError(422, LLMAdminCodeValidation, "禁用实例不能设为默认")
		}
		if _, err := tx.LLMProviderConfig.Update().
			Where(
				llmproviderconfig.TenantIDEQ(tenantID),
				llmproviderconfig.IsDefault(true),
				llmproviderconfig.DeletedAtIsNil(),
			).
			SetIsDefault(false).
			Save(ctx); err != nil {
			_ = tx.Rollback()
			return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
		}
		record, err = record.Update().SetIsDefault(true).Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
		}
	}
	if err := tx.Commit(); err != nil {
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}

	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "create", name, map[string]interface{}{
		"protocol": protocolName, "variant": variant, "isDefault": req.IsDefault,
	})
	return s.toProviderDTO(record), nil
}

// UpdateProvider PUT /ai/providers/:id：部分更新；apiKey 缺省不变、空串清空；
// name 不可修改（密文锚点）；禁用默认实例需先切换默认（409）。
func (s *LLMProviderAdminService) UpdateProvider(ctx context.Context, tenantID, userID, id int, req dto.LLMUpdateProviderRequest) (dto.LLMProviderDTO, error) {
	if err := s.ensureReady(true); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	record, err := s.loadRecord(ctx, tenantID, id)
	if err != nil {
		return dto.LLMProviderDTO{}, err
	}

	protocolName := record.Protocol
	if req.Protocol != nil {
		protocolName = service.NormalizeLLMProtocol(*req.Protocol)
	}
	variant := record.Variant
	if req.Variant != nil {
		variant = service.NormalizeLLMVariant(*req.Variant)
	}
	if err := validateProtocolSlot(protocolName, variant); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	endpoint := record.Endpoint
	if req.Endpoint != nil {
		endpoint = strings.TrimSpace(*req.Endpoint)
	}
	deployment := record.Deployment
	if req.Deployment != nil {
		deployment = strings.TrimSpace(*req.Deployment)
	}
	if err := validateEndpointRequirements(protocolName, variant, endpoint, deployment); err != nil {
		return dto.LLMProviderDTO{}, err
	}

	displayName := record.DisplayName
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if displayName == "" {
			return dto.LLMProviderDTO{}, newLLMAdminError(422, LLMAdminCodeValidation, "displayName 不能为空")
		}
	}
	if len(displayName) > 100 {
		return dto.LLMProviderDTO{}, newLLMAdminError(422, LLMAdminCodeValidation, "displayName 长度必须 ≤100")
	}

	enabled := record.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if !enabled && record.IsDefault {
		return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeIsDefault, "默认实例不能禁用，请先切换默认")
	}

	options, err := decodeAdapterOptions(req.AdapterOptions)
	if err != nil {
		return dto.LLMProviderDTO{}, err
	}

	update := record.Update().
		SetProtocol(protocolName).
		SetVariant(variant).
		SetModel(strings.TrimSpace(valueOr(req.Model, record.Model))).
		SetEndpoint(endpoint).
		SetDeployment(deployment).
		SetDisplayName(displayName).
		SetEnabled(enabled).
		SetUpdatedAt(time.Now())
	if req.AdapterOptions != nil {
		if options == nil {
			options = map[string]interface{}{}
		}
		update = update.SetAdapterOptions(options)
	}
	if req.APIKey != nil {
		if strings.TrimSpace(*req.APIKey) == "" {
			update = update.SetEncryptedAPIKey("")
		} else {
			ciphertext, encErr := s.encrypter.Encrypt(*req.APIKey)
			if encErr != nil {
				return dto.LLMProviderDTO{}, &LLMAdminError{Status: 500, Code: LLMAdminCodeInternal, Message: "密钥加密失败", Err: encErr}
			}
			update = update.SetEncryptedAPIKey(ciphertext)
		}
	}
	updated, err := update.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "更新违反唯一约束")
		}
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}

	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "update", updated.Name, map[string]interface{}{
		"protocol": updated.Protocol, "variant": updated.Variant, "enabled": updated.Enabled,
		"apiKeyChanged": req.APIKey != nil,
	})
	return s.toProviderDTO(updated), nil
}

// DeleteProvider DELETE /ai/providers/:id：软删 + 同租户个人偏好清理（同一事务）；
// 默认实例拒绝（409 AI_PROVIDER_IS_DEFAULT）。
func (s *LLMProviderAdminService) DeleteProvider(ctx context.Context, tenantID, userID, id int) (dto.LLMProviderDeleteResponse, error) {
	if err := s.ensureReady(false); err != nil {
		return dto.LLMProviderDeleteResponse{}, err
	}
	record, err := s.loadRecord(ctx, tenantID, id)
	if err != nil {
		return dto.LLMProviderDeleteResponse{}, err
	}
	if record.IsDefault {
		return dto.LLMProviderDeleteResponse{}, newLLMAdminError(409, LLMAdminCodeIsDefault, "默认实例不能删除，请先切换默认")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return dto.LLMProviderDeleteResponse{}, mapLLMAdminError(err, "")
	}
	now := time.Now()
	if _, err := tx.LLMProviderConfig.UpdateOneID(record.ID).
		SetDeletedAt(now).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return dto.LLMProviderDeleteResponse{}, mapLLMAdminError(err, "")
	}
	if _, err := tx.LLMUserPreference.Delete().
		Where(
			llmuserpreference.TenantIDEQ(tenantID),
			llmuserpreference.ProviderKeyEQ(record.Name),
		).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return dto.LLMProviderDeleteResponse{}, mapLLMAdminError(err, "")
	}
	if err := tx.Commit(); err != nil {
		return dto.LLMProviderDeleteResponse{}, mapLLMAdminError(err, "")
	}

	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "delete", record.Name, map[string]interface{}{"protocol": record.Protocol})
	return dto.LLMProviderDeleteResponse{ID: record.ID, Deleted: true}, nil
}

// SetDefaultProvider POST /ai/providers/:id/default：事务内「先清后置」；
// 禁用实例 409；不存在/跨租户 404。
func (s *LLMProviderAdminService) SetDefaultProvider(ctx context.Context, tenantID, userID, id int) (dto.LLMProviderDTO, error) {
	if err := s.ensureReady(false); err != nil {
		return dto.LLMProviderDTO{}, err
	}
	record, err := s.loadRecord(ctx, tenantID, id)
	if err != nil {
		return dto.LLMProviderDTO{}, err
	}
	if !record.Enabled {
		return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeDisabled, "实例已禁用，不能设为默认")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	if _, err := tx.LLMProviderConfig.Update().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.IsDefault(true),
			llmproviderconfig.DeletedAtIsNil(),
			llmproviderconfig.IDNEQ(record.ID),
		).
		SetIsDefault(false).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	updated, err := tx.LLMProviderConfig.UpdateOneID(record.ID).
		SetIsDefault(true).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsConstraintError(err) {
			return dto.LLMProviderDTO{}, newLLMAdminError(409, LLMAdminCodeInternal, "租户默认唯一约束冲突，请重试")
		}
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}
	if err := tx.Commit(); err != nil {
		return dto.LLMProviderDTO{}, mapLLMAdminError(err, "")
	}

	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "set_default", updated.Name, map[string]interface{}{"protocol": updated.Protocol})
	return s.toProviderDTO(updated), nil
}

// TestProvider POST /ai/providers/:id/test：构造最小 Chat 请求（15s 超时），
// 回写 status/last_error/last_tested_at；密钥缺失 → 422 AI_PROVIDER_KEY_MISSING。
func (s *LLMProviderAdminService) TestProvider(ctx context.Context, tenantID, userID, id int) (dto.LLMProviderTestResponse, error) {
	if err := s.ensureReady(true); err != nil {
		return dto.LLMProviderTestResponse{}, err
	}
	record, err := s.loadRecord(ctx, tenantID, id)
	if err != nil {
		return dto.LLMProviderTestResponse{}, err
	}
	now := time.Now()
	response := dto.LLMProviderTestResponse{ID: record.ID, Key: record.Name, TestedAt: now}

	if strings.TrimSpace(record.EncryptedAPIKey) == "" {
		s.persistTestResult(ctx, record, "error", "密钥缺失", now)
		s.recordAudit(ctx, tenantID, userID, "test", record.Name, map[string]interface{}{"ok": false, "status": "error", "reason": "key_missing"})
		return response, newLLMAdminError(422, LLMAdminCodeKeyMissing, "实例未配置 API Key")
	}
	apiKey, decErr := s.encrypter.Decrypt(record.EncryptedAPIKey)
	if decErr != nil || strings.TrimSpace(apiKey) == "" {
		s.persistTestResult(ctx, record, "error", "密钥解密失败", now)
		s.recordAudit(ctx, tenantID, userID, "test", record.Name, map[string]interface{}{"ok": false, "status": "error", "reason": "decrypt_failed"})
		return response, newLLMAdminError(422, LLMAdminCodeKeyMissing, "实例密钥解密失败")
	}

	provider, buildErr := buildLLMProviderForRecord(record, apiKey)
	if buildErr != nil {
		mapped := mapLLMAdminError(buildErr, "")
		s.persistTestResult(ctx, record, "error", mapped.(*LLMAdminError).Message, now)
		s.recordAudit(ctx, tenantID, userID, "test", record.Name, map[string]interface{}{"ok": false, "status": "error"})
		return response, mapped
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, chatErr := provider.Chat(timeoutCtx, record.Model, []service.LLMMessage{{Role: "user", Content: "ping"}})
	if chatErr != nil {
		message := sanitizeLLMAdminMessage(chatErr.Error(), apiKey)
		s.persistTestResult(ctx, record, "error", message, now)
		s.recordAudit(ctx, tenantID, userID, "test", record.Name, map[string]interface{}{"ok": false, "status": "error"})
		response.OK = false
		response.Status = "error"
		response.Error = message
		return response, nil
	}

	s.persistTestResult(ctx, record, "ok", "", now)
	s.recordAudit(ctx, tenantID, userID, "test", record.Name, map[string]interface{}{"ok": true, "status": "ok"})
	response.OK = true
	response.Status = "ok"
	return response, nil
}

// persistTestResult 回写连通性测试结果（失败只告警，不覆盖主流程错误）。
func (s *LLMProviderAdminService) persistTestResult(ctx context.Context, record *ent.LLMProviderConfig, status, lastError string, testedAt time.Time) {
	if len(lastError) > 2000 {
		lastError = lastError[:2000]
	}
	if _, err := record.Update().
		SetStatus(status).
		SetLastError(lastError).
		SetLastTestedAt(testedAt).
		SetUpdatedAt(testedAt).
		Save(ctx); err != nil && s.logger != nil {
		s.logger.Warnw("llm provider test result persist failed", "provider_key", record.Name, "error", err)
	}
}

// buildLLMProviderForRecord 按 (protocol, variant) 构建 provider：
// 适配器优先（BE-9），未注册协议回退既有分支（§3.1.4 映射）。
func buildLLMProviderForRecord(record *ent.LLMProviderConfig, apiKey string) (service.LLMProvider, error) {
	provider, err := service.NewProtocolProvider(service.ProtocolProviderOptions{
		Protocol: record.Protocol,
		Variant:  record.Variant,
		APIKey:   apiKey,
		Endpoint: record.Endpoint,
		Model:    record.Model,
	})
	if err == nil {
		return provider, nil
	}
	if !errors.Is(err, service.ErrProtocolNotImplemented) {
		return nil, err
	}
	legacyProvider, mapErr := service.MapProtocolToLegacyProvider(record.Protocol, record.Variant)
	if mapErr != nil {
		return nil, mapErr
	}
	return service.NewProviderFromConfig(service.ProviderConfig{
		Provider:   legacyProvider,
		Model:      record.Model,
		APIKey:     apiKey,
		Endpoint:   record.Endpoint,
		Deployment: record.Deployment,
	}), nil
}

// sanitizeLLMAdminMessage 脱敏 provider 错误文本：抹掉明文密钥、折叠换行、截断到 2000 字节。
func sanitizeLLMAdminMessage(message, secret string) string {
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "***")
	}
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

// ImportStatic POST /ai/providers/import-static：把静态配置导入为一条记录（source=imported，加密落库）。
//
// 幂等键 = (protocol, variant, endpoint, model, deployment)：命中则更新（updated=true），
// 未命中则创建（created=true）；仅 name 冲突（同租户同名不同记录）返回 409。
func (s *LLMProviderAdminService) ImportStatic(ctx context.Context, tenantID, userID int) (dto.LLMImportStaticResponse, error) {
	if err := s.ensureReady(true); err != nil {
		return dto.LLMImportStaticResponse{}, err
	}
	staticCfg := service.LoadLLMConfig()
	protocolName, variant, ok := staticProviderProtocolSpec(staticCfg.Provider)
	if !ok {
		return dto.LLMImportStaticResponse{}, newLLMAdminError(422, LLMAdminCodeUnavailable,
			"静态 provider 无法识别: "+strings.TrimSpace(staticCfg.Provider))
	}
	if err := validateProtocolSlot(protocolName, variant); err != nil {
		return dto.LLMImportStaticResponse{}, err
	}
	apiKey := staticEffectiveAPIKey(staticCfg)
	if strings.TrimSpace(apiKey) == "" {
		return dto.LLMImportStaticResponse{}, newLLMAdminError(422, LLMAdminCodeKeyMissing, "静态配置未提供 API Key，无法导入")
	}
	ciphertext, encErr := s.encrypter.Encrypt(apiKey)
	if encErr != nil {
		return dto.LLMImportStaticResponse{}, &LLMAdminError{Status: 500, Code: LLMAdminCodeInternal, Message: "密钥加密失败", Err: encErr}
	}
	endpoint := strings.TrimSpace(staticCfg.Endpoint)
	model := strings.TrimSpace(staticCfg.Model)
	deployment := strings.TrimSpace(staticCfg.Deployment)
	name := "static-" + staticLegacyProviderName(staticCfg.Provider)

	// 幂等键命中（含历史软删行，避免唯一索引冲突）
	identities, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.ProtocolEQ(protocolName),
			llmproviderconfig.VariantEQ(variant),
			llmproviderconfig.EndpointEQ(endpoint),
			llmproviderconfig.ModelEQ(model),
			llmproviderconfig.DeploymentEQ(deployment),
		).
		All(ctx)
	if err != nil {
		return dto.LLMImportStaticResponse{}, mapLLMAdminError(err, "")
	}
	nameRows, err := s.client.LLMProviderConfig.Query().
		Where(llmproviderconfig.TenantIDEQ(tenantID), llmproviderconfig.NameEQ(name)).
		All(ctx)
	if err != nil {
		return dto.LLMImportStaticResponse{}, mapLLMAdminError(err, "")
	}

	if len(identities) > 0 {
		target := identities[0]
		if len(nameRows) > 0 && nameRows[0].ID != target.ID {
			return dto.LLMImportStaticResponse{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "name "+name+" 已被其它实例占用")
		}
		if len(identities) > 1 {
			return dto.LLMImportStaticResponse{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "存在多条同源静态配置记录，请人工收敛后再导入")
		}
		updated, saveErr := target.Update().
			SetEncryptedAPIKey(ciphertext).
			SetSource(llmAdminStaticSource).
			SetStatus("configured").
			SetLastError("").
			SetDisplayName(ensureStaticDisplayName(target.DisplayName, staticCfg.Provider)).
			SetNillableDeletedAt(nil).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if saveErr != nil {
			return dto.LLMImportStaticResponse{}, mapLLMAdminError(saveErr, "")
		}
		s.invalidate(tenantID)
		s.recordAudit(ctx, tenantID, userID, "import_static", updated.Name, map[string]interface{}{
			"protocol": protocolName, "variant": variant, "updated": true,
		})
		return dto.LLMImportStaticResponse{Updated: true, Created: false, Provider: s.toProviderDTO(updated)}, nil
	}

	if len(nameRows) > 0 {
		return dto.LLMImportStaticResponse{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "name "+name+" 已被其它实例占用")
	}

	created, createErr := s.client.LLMProviderConfig.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetDisplayName(ensureStaticDisplayName("", staticCfg.Provider)).
		SetProtocol(protocolName).
		SetVariant(variant).
		SetModel(model).
		SetEndpoint(endpoint).
		SetDeployment(deployment).
		SetEncryptedAPIKey(ciphertext).
		SetEnabled(true).
		SetSource(llmAdminStaticSource).
		SetStatus("configured").
		Save(ctx)
	if createErr != nil {
		if ent.IsConstraintError(createErr) {
			return dto.LLMImportStaticResponse{}, newLLMAdminError(409, LLMAdminCodeNameConflict, "name 已存在")
		}
		return dto.LLMImportStaticResponse{}, mapLLMAdminError(createErr, "")
	}
	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "import_static", created.Name, map[string]interface{}{
		"protocol": protocolName, "variant": variant, "updated": false,
	})
	return dto.LLMImportStaticResponse{Updated: false, Created: true, Provider: s.toProviderDTO(created)}, nil
}

// GetUserPreference GET /ai/user-preference：个人默认 + 生效值（§3.3 解析链的前两级）。
func (s *LLMProviderAdminService) GetUserPreference(ctx context.Context, tenantID, userID int) (dto.LLMUserPreferenceResponse, error) {
	response := dto.LLMUserPreferenceResponse{Source: service.ProviderSourceStatic}
	if err := s.ensureReady(false); err != nil {
		return response, err
	}
	preference, err := s.client.LLMUserPreference.Query().
		Where(llmuserpreference.UserIDEQ(userID), llmuserpreference.TenantIDEQ(tenantID)).
		Only(ctx)
	if err == nil {
		response.ProviderKey = strings.TrimSpace(preference.ProviderKey)
	} else if !ent.IsNotFound(err) {
		return response, mapLLMAdminError(err, "")
	}
	if response.ProviderKey != "" && s.providerSelectable(ctx, tenantID, response.ProviderKey) {
		response.EffectiveProviderKey = response.ProviderKey
		response.Source = service.ProviderSourceUser
		return response, nil
	}
	defaultKey, defaultErr := s.tenantDefaultKey(ctx, tenantID)
	if defaultErr != nil {
		return response, defaultErr
	}
	if defaultKey != "" {
		response.EffectiveProviderKey = defaultKey
		response.Source = service.ProviderSourceTenant
	}
	return response, nil
}

// SetUserPreference PUT /ai/user-preference：设置/清除个人默认（校验租户归属 + enabled + 协议已实现）。
func (s *LLMProviderAdminService) SetUserPreference(ctx context.Context, tenantID, userID int, providerKey string) (dto.LLMUserPreferenceResponse, error) {
	if err := s.ensureReady(false); err != nil {
		return dto.LLMUserPreferenceResponse{}, err
	}
	key := strings.TrimSpace(providerKey)
	if key == "" {
		if _, err := s.client.LLMUserPreference.Delete().
			Where(llmuserpreference.UserIDEQ(userID), llmuserpreference.TenantIDEQ(tenantID)).
			Exec(ctx); err != nil {
			return dto.LLMUserPreferenceResponse{}, mapLLMAdminError(err, "")
		}
		s.invalidate(tenantID)
		s.recordAudit(ctx, tenantID, userID, "clear_user_preference", "", nil)
		return s.GetUserPreference(ctx, tenantID, userID)
	}

	record, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.NameEQ(key),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return dto.LLMUserPreferenceResponse{}, newLLMAdminError(404, LLMAdminCodeNotFound, "实例不存在")
		}
		return dto.LLMUserPreferenceResponse{}, mapLLMAdminError(err, "")
	}
	if !record.Enabled {
		return dto.LLMUserPreferenceResponse{}, newLLMAdminError(409, LLMAdminCodeDisabled, "实例已禁用，不能设为个人默认")
	}
	if !service.IsImplementedLLMProtocol(record.Protocol) {
		return dto.LLMUserPreferenceResponse{}, newLLMAdminError(422, LLMAdminCodeProtocolImpl, "协议尚未实现（槽位）")
	}

	preference, err := s.client.LLMUserPreference.Query().
		Where(llmuserpreference.UserIDEQ(userID), llmuserpreference.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return dto.LLMUserPreferenceResponse{}, mapLLMAdminError(err, "")
		}
		if _, createErr := s.client.LLMUserPreference.Create().
			SetUserID(userID).
			SetTenantID(tenantID).
			SetProviderKey(key).
			Save(ctx); createErr != nil {
			return dto.LLMUserPreferenceResponse{}, mapLLMAdminError(createErr, "")
		}
	} else if _, updateErr := preference.Update().SetProviderKey(key).SetUpdatedAt(time.Now()).Save(ctx); updateErr != nil {
		return dto.LLMUserPreferenceResponse{}, mapLLMAdminError(updateErr, "")
	}

	s.invalidate(tenantID)
	s.recordAudit(ctx, tenantID, userID, "set_user_preference", key, nil)
	return s.GetUserPreference(ctx, tenantID, userID)
}

// providerSelectable 报告本租户内该 key 是否指向「启用 + 未软删 + 协议已实现」的实例。
func (s *LLMProviderAdminService) providerSelectable(ctx context.Context, tenantID int, key string) bool {
	record, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.NameEQ(key),
			llmproviderconfig.Enabled(true),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		return false
	}
	return service.IsImplementedLLMProtocol(record.Protocol)
}

// tenantDefaultKey 返回本租户默认实例 key（无则空串）。
func (s *LLMProviderAdminService) tenantDefaultKey(ctx context.Context, tenantID int) (string, error) {
	record, err := s.client.LLMProviderConfig.Query().
		Where(
			llmproviderconfig.TenantIDEQ(tenantID),
			llmproviderconfig.IsDefault(true),
			llmproviderconfig.Enabled(true),
			llmproviderconfig.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", nil
		}
		return "", mapLLMAdminError(err, "")
	}
	return record.Name, nil
}

// staticProviderProtocolSpec 静态 provider 名 → (协议, 变体)：与 service/llm_registry.go
// 的静态侧映射表同源（openai/azure/local/minimax），保证导入记录与运行期回退一致。
func staticProviderProtocolSpec(provider string) (protocolName, variant string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "openai":
		return service.LLMProtocolOpenAIChatCompletions, service.LLMVariantDefault, true
	case "azure":
		return service.LLMProtocolOpenAIChatCompletions, service.LLMVariantAzure, true
	case "local":
		return service.LLMProtocolOpenAIChatCompletions, service.LLMVariantOllama, true
	case "minimax":
		return service.LLMProtocolAnthropicMessages, service.LLMVariantMiniMax, true
	default:
		return "", "", false
	}
}

// staticEffectiveAPIKey 解析静态配置的实际密钥（含与既有 NewProviderFromConfig 同源的环境变量兜底）。
func staticEffectiveAPIKey(cfg service.ProviderConfig) string {
	if key := strings.TrimSpace(cfg.APIKey); key != "" {
		return key
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "azure":
		return os.Getenv("AZURE_OPENAI_API_KEY")
	case "minimax":
		return os.Getenv("MINIMAX_API_KEY")
	default:
		return ""
	}
}

// staticLegacyProviderName 归一静态 provider 名（空值按 openai），用于导入记录命名。
func staticLegacyProviderName(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		return "openai"
	}
	return name
}

// ensureStaticDisplayName 导入记录的展示名（已存在时保留原值）。
func ensureStaticDisplayName(current, provider string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return "静态配置导入（" + staticLegacyProviderName(provider) + "）"
}

// valueOr 指针缺省时回退既有值。
func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
