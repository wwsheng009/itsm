package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/systemconfig"
	"itsm-backend/ent/tenant"
	"itsm-backend/middleware"
	"itsm-backend/pkg/tenantmode"
)

// MSPAllocationRoleMappingKey 是 provider 租户级分配角色映射的 system_configs 键。
const MSPAllocationRoleMappingKey = "msp.allocation_role_mapping"

// MSPAllocationRoleMappingCategory 是管理面分组。
const MSPAllocationRoleMappingCategory = "msp"

var mspAllocationRoleKeys = []string{"primary", "backup", "specialist"}

var mspAllocationRoleDefaults = map[string]string{
	"primary":    "msp_tech",
	"backup":     "msp_tech",
	"specialist": "msp_specialist",
}

var mspAllocationTargetRoles = []string{
	"msp_viewer",
	"msp_tech",
	"msp_specialist",
	"msp_manager",
	"msp_admin",
}

// ErrMSPAllocationRoleMappingInvalid 表示配置结构/取值不合法。
var ErrMSPAllocationRoleMappingInvalid = errors.New("invalid msp allocation role mapping")

// ErrMSPAllocationRoleMappingNotProvider 表示配置只能写入 provider 租户。
var ErrMSPAllocationRoleMappingNotProvider = errors.New("msp allocation role mapping requires provider tenant")

// ErrMSPAllocationRoleMappingCorrupt 表示存在覆盖行但内容不可用；运行时应 fail-closed。
var ErrMSPAllocationRoleMappingCorrupt = errors.New("msp allocation role mapping is corrupt")

// MSPAllocationRoleMappingAuditMeta 是配置变更审计的调用方元数据。
type MSPAllocationRoleMappingAuditMeta struct {
	ActorUserID  int
	ActorAccount string
	Source       string
	Path         string
	Method       string
	IP           string
}

// DefaultMSPAllocationRoleMapping 返回内置默认映射的副本（调用方可安全修改）。
func DefaultMSPAllocationRoleMapping() map[string]string {
	out := make(map[string]string, len(mspAllocationRoleDefaults))
	for k, v := range mspAllocationRoleDefaults {
		out[k] = v
	}
	return out
}

// AllowedMSPAllocationTargetRoles 返回客户租户内允许被映射到的角色词表。
func AllowedMSPAllocationTargetRoles() []string {
	out := make([]string, len(mspAllocationTargetRoles))
	copy(out, mspAllocationTargetRoles)
	return out
}

func normalizeAllocationRoleKey(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func defaultMSPAllocationRole(allocRole string) string {
	role := normalizeAllocationRoleKey(allocRole)
	if target, ok := mspAllocationRoleDefaults[role]; ok {
		return target
	}
	// 与历史行为一致：未知/空分配角色按 msp_tech 兜底。
	return mspAllocationRoleDefaults["primary"]
}

func isAllowedMSPAllocationTargetRole(v string) bool {
	v = normalizeAllocationRoleKey(v)
	for _, allowed := range mspAllocationTargetRoles {
		if v == allowed {
			return true
		}
	}
	return false
}

// validateMSPAllocationRoleMapping 校验 PUT 语义：必须且只能包含三个键，
// 目标角色必须在 MSP 五角色白名单内。
func validateMSPAllocationRoleMapping(input map[string]string) (map[string]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: mapping is empty", ErrMSPAllocationRoleMappingInvalid)
	}
	out := make(map[string]string, len(mspAllocationRoleKeys))
	seen := make(map[string]bool, len(mspAllocationRoleKeys))
	for key, value := range input {
		normalizedKey := normalizeAllocationRoleKey(key)
		if _, ok := mspAllocationRoleDefaults[normalizedKey]; !ok {
			return nil, fmt.Errorf("%w: unknown allocation role %q", ErrMSPAllocationRoleMappingInvalid, key)
		}
		if seen[normalizedKey] {
			return nil, fmt.Errorf("%w: duplicate allocation role %q", ErrMSPAllocationRoleMappingInvalid, key)
		}
		seen[normalizedKey] = true
		normalizedValue := normalizeAllocationRoleKey(value)
		if !isAllowedMSPAllocationTargetRole(normalizedValue) {
			return nil, fmt.Errorf("%w: target role %q for %q is not allowed", ErrMSPAllocationRoleMappingInvalid, value, key)
		}
		out[normalizedKey] = normalizedValue
	}
	for _, key := range mspAllocationRoleKeys {
		if !seen[key] {
			return nil, fmt.Errorf("%w: missing allocation role %q", ErrMSPAllocationRoleMappingInvalid, key)
		}
	}
	return out, nil
}

type allocationRoleMappingConfig struct {
	Mapping    map[string]string
	Overridden bool
	Corrupt    bool
	UpdatedAt  *time.Time
	UpdatedBy  string
	Warning    string
}

func defaultAllocationRoleMappingConfig() allocationRoleMappingConfig {
	return allocationRoleMappingConfig{
		Mapping: DefaultMSPAllocationRoleMapping(),
	}
}

// loadMSPAllocationRoleMapping 读取 provider 租户覆盖行；缺行/坏值回退默认。
// 返回的 Mapping 始终是完整的三个键，调用方不需要再判空。
func loadMSPAllocationRoleMapping(
	ctx context.Context,
	client *ent.Client,
	providerTenantID int,
) (allocationRoleMappingConfig, error) {
	cfg := defaultAllocationRoleMappingConfig()
	if client == nil || providerTenantID <= 0 {
		return cfg, nil
	}

	cfgCtx := tenantctx.WithTenantID(ctx, providerTenantID)
	row, err := client.SystemConfig.Query().
		Where(
			systemconfig.TenantIDEQ(providerTenantID),
			systemconfig.KeyEQ(MSPAllocationRoleMappingKey),
			systemconfig.DeletedAtIsNil(),
		).
		Order(ent.Desc(systemconfig.FieldID)).
		First(cfgCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("load allocation role mapping provider=%d: %w", providerTenantID, err)
	}

	cfg.Overridden = true
	updatedAt := row.UpdatedAt
	cfg.UpdatedAt = &updatedAt
	cfg.UpdatedBy = row.CreatedBy
	if strings.TrimSpace(row.Value) == "" {
		cfg.Corrupt = true
		cfg.Warning = "empty value; using defaults"
		return cfg, nil
	}

	var raw map[string]string
	if err := json.Unmarshal([]byte(row.Value), &raw); err != nil {
		cfg.Corrupt = true
		cfg.Warning = "invalid json; using defaults"
		return cfg, nil
	}

	merged := DefaultMSPAllocationRoleMapping()
	var issues []string
	seen := make(map[string]bool, len(mspAllocationRoleKeys))
	for key, value := range raw {
		normalizedKey := normalizeAllocationRoleKey(key)
		if _, ok := mspAllocationRoleDefaults[normalizedKey]; !ok {
			issues = append(issues, "unknown:"+key)
			continue
		}
		seen[normalizedKey] = true
		normalizedValue := normalizeAllocationRoleKey(value)
		if !isAllowedMSPAllocationTargetRole(normalizedValue) {
			issues = append(issues, "invalid:"+key)
			continue
		}
		merged[normalizedKey] = normalizedValue
	}
	for _, key := range mspAllocationRoleKeys {
		if !seen[key] {
			issues = append(issues, "missing:"+key)
		}
	}
	cfg.Mapping = merged
	if len(issues) > 0 {
		cfg.Corrupt = true
		cfg.Warning = "invalid mapping: " + strings.Join(issues, ",")
	}
	return cfg, nil
}

// ResolveMSPAllocationMembershipRole 解析分配角色对应的客户租户目标角色。
// source 仅用于日志/诊断：provider_config 或 default；warning 非空表示配置被部分忽略/回退。
func ResolveMSPAllocationMembershipRole(
	ctx context.Context,
	client *ent.Client,
	providerTenantID int,
	allocRole string,
) (roleCode string, source string, warning string, err error) {
	role := normalizeAllocationRoleKey(allocRole)
	if _, ok := mspAllocationRoleDefaults[role]; !ok {
		role = "primary"
	}

	cfg, err := loadMSPAllocationRoleMapping(ctx, client, providerTenantID)
	if err != nil {
		return "", "config_error", "", fmt.Errorf("load allocation role mapping provider=%d: %w", providerTenantID, err)
	}
	if cfg.Corrupt {
		return "", "config_error", cfg.Warning,
			fmt.Errorf("%w: provider=%d: %s", ErrMSPAllocationRoleMappingCorrupt, providerTenantID, cfg.Warning)
	}
	target := normalizeAllocationRoleKey(cfg.Mapping[role])
	if target == "" || !isAllowedMSPAllocationTargetRole(target) {
		return defaultMSPAllocationRole(role), "default", cfg.Warning, nil
	}
	if cfg.Overridden {
		return target, "provider_config", cfg.Warning, nil
	}
	return target, "default", cfg.Warning, nil
}

func (s *MSPAllocationService) ensureMSPProviderTenant(ctx context.Context, tenantID int) error {
	if s == nil || s.client == nil {
		return errors.New("msp allocation service unavailable")
	}
	if tenantID <= 0 {
		return fmt.Errorf("%w: tenant_id is required", ErrMSPAllocationRoleMappingNotProvider)
	}
	scopeCtx := tenantctx.WithTenantID(ctx, tenantID)
	t, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(scopeCtx)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("%w: tenant=%d not found", ErrMSPAllocationRoleMappingNotProvider, tenantID)
		}
		return fmt.Errorf("load provider tenant %d: %w", tenantID, err)
	}
	if !tenantmode.IsMSPProviderTenantType(string(t.Type)) {
		return fmt.Errorf("%w: tenant=%d type=%s", ErrMSPAllocationRoleMappingNotProvider, tenantID, t.Type)
	}
	return nil
}

// GetAllocationRoleMapping 返回 provider 租户当前生效映射、默认值与来源。
func (s *MSPAllocationService) GetAllocationRoleMapping(
	ctx context.Context,
	providerTenantID int,
) (*dto.MSPAllocationRoleMappingView, error) {
	if err := s.ensureMSPProviderTenant(ctx, providerTenantID); err != nil {
		return nil, err
	}
	cfg, err := loadMSPAllocationRoleMapping(ctx, s.client, providerTenantID)
	if err != nil {
		return nil, err
	}
	return &dto.MSPAllocationRoleMappingView{
		Mapping:      cfg.Mapping,
		Defaults:     DefaultMSPAllocationRoleMapping(),
		AllowedRoles: AllowedMSPAllocationTargetRoles(),
		Overridden:   cfg.Overridden,
		Corrupt:      cfg.Corrupt,
		UpdatedAt:    cfg.UpdatedAt,
		UpdatedBy:    cfg.UpdatedBy,
		Warning:      cfg.Warning,
	}, nil
}

// UpdateAllocationRoleMapping 写入 provider 租户级覆盖配置。
func (s *MSPAllocationService) UpdateAllocationRoleMapping(
	ctx context.Context,
	providerTenantID int,
	input map[string]string,
	meta MSPAllocationRoleMappingAuditMeta,
) (*dto.MSPAllocationRoleMappingView, error) {
	if err := s.ensureMSPProviderTenant(ctx, providerTenantID); err != nil {
		return nil, err
	}
	before, err := loadMSPAllocationRoleMapping(ctx, s.client, providerTenantID)
	if err != nil {
		return nil, err
	}
	normalized, err := validateMSPAllocationRoleMapping(input)
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode allocation role mapping: %w", err)
	}

	scopeCtx := tenantctx.WithTenantID(ctx, providerTenantID)
	existing, err := s.client.SystemConfig.Query().
		Where(
			systemconfig.TenantIDEQ(providerTenantID),
			systemconfig.KeyEQ(MSPAllocationRoleMappingKey),
			systemconfig.DeletedAtIsNil(),
		).
		Order(ent.Desc(systemconfig.FieldID)).
		First(scopeCtx)
	switch {
	case err == nil:
		if _, err := s.client.SystemConfig.UpdateOneID(existing.ID).
			SetValue(string(value)).
			SetValueType("json").
			SetCategory(MSPAllocationRoleMappingCategory).
			SetDescription("MSP allocation role -> customer tenant membership role mapping").
			SetCreatedBy(meta.ActorAccount).
			Save(scopeCtx); err != nil {
			return nil, fmt.Errorf("update allocation role mapping: %w", err)
		}
	case ent.IsNotFound(err):
		if _, err := s.client.SystemConfig.Create().
			SetTenantID(providerTenantID).
			SetKey(MSPAllocationRoleMappingKey).
			SetValue(string(value)).
			SetValueType("json").
			SetCategory(MSPAllocationRoleMappingCategory).
			SetDescription("MSP allocation role -> customer tenant membership role mapping").
			SetCreatedBy(meta.ActorAccount).
			Save(scopeCtx); err != nil {
			return nil, fmt.Errorf("create allocation role mapping: %w", err)
		}
	default:
		return nil, fmt.Errorf("load allocation role mapping: %w", err)
	}

	if s.logger != nil {
		s.logger.Infow("msp allocation role mapping updated",
			"provider_tenant_id", providerTenantID,
			"operator", meta.ActorAccount,
			"mapping", normalized)
	}
	s.writeAllocationRoleMappingAudit(ctx, providerTenantID, meta, "msp.allocation_role_mapping.update", map[string]any{
		"before": before.Mapping,
		"after":  normalized,
	})
	return s.GetAllocationRoleMapping(ctx, providerTenantID)
}

// ResetAllocationRoleMapping 软删覆盖行，恢复代码默认值。
func (s *MSPAllocationService) ResetAllocationRoleMapping(
	ctx context.Context,
	providerTenantID int,
	meta MSPAllocationRoleMappingAuditMeta,
) (*dto.MSPAllocationRoleMappingView, error) {
	if err := s.ensureMSPProviderTenant(ctx, providerTenantID); err != nil {
		return nil, err
	}
	before, err := loadMSPAllocationRoleMapping(ctx, s.client, providerTenantID)
	if err != nil {
		return nil, err
	}
	scopeCtx := tenantctx.WithTenantID(ctx, providerTenantID)
	if _, err := s.client.SystemConfig.Update().
		Where(
			systemconfig.TenantIDEQ(providerTenantID),
			systemconfig.KeyEQ(MSPAllocationRoleMappingKey),
			systemconfig.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(scopeCtx); err != nil {
		return nil, fmt.Errorf("reset allocation role mapping: %w", err)
	}
	if s.logger != nil {
		s.logger.Infow("msp allocation role mapping reset",
			"provider_tenant_id", providerTenantID,
			"operator", meta.ActorAccount)
	}
	s.writeAllocationRoleMappingAudit(ctx, providerTenantID, meta, "msp.allocation_role_mapping.reset", map[string]any{
		"before": before.Mapping,
		"after":  DefaultMSPAllocationRoleMapping(),
	})
	return s.GetAllocationRoleMapping(ctx, providerTenantID)
}

func (s *MSPAllocationService) writeAllocationRoleMappingAudit(
	ctx context.Context,
	providerTenantID int,
	meta MSPAllocationRoleMappingAuditMeta,
	action string,
	payload any,
) {
	if s == nil || s.client == nil {
		return
	}
	source := strings.TrimSpace(meta.Source)
	if source == "" {
		source = middleware.AuditSourceHeader
	}
	path := strings.TrimSpace(meta.Path)
	if path == "" {
		path = "service:msp_allocation_role_mapping"
	}
	method := strings.TrimSpace(meta.Method)
	if method == "" {
		method = "INTERNAL"
	}
	body, _ := json.Marshal(payload)
	auditCtx, cancel := context.WithTimeout(tenantctx.WithTenantID(ctx, providerTenantID), 2*time.Second)
	defer cancel()
	_, err := s.client.AuditLog.Create().
		SetTenantID(providerTenantID).
		SetTargetTenantID(providerTenantID).
		SetUserID(meta.ActorUserID).
		SetActorAccount(meta.ActorAccount).
		SetSource(source).
		SetResource("msp_allocation_role_mapping").
		SetAction(action).
		SetPath(path).
		SetMethod(method).
		SetIP(meta.IP).
		SetStatusCode(200).
		SetRequestBody(string(body)).
		Save(auditCtx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warnw("allocation role mapping audit write failed",
				"error", err, "provider_tenant_id", providerTenantID, "action", action)
		}
	}
}
