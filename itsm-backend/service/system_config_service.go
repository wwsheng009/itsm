package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/systemconfig"

	"go.uber.org/zap"
)

type SystemConfigService struct {
	client *ent.Client
	logger *zap.SugaredLogger

	// 密码策略缓存：system_configs 是热路径读（每次设密都要校验），
	// 配置写入路径会主动失效，TTL 仅作兜底。
	policyMu    sync.RWMutex
	policyCache map[int]passwordPolicyCacheEntry
	// M2 能力开关：配置写入后失效运行时能力开关缓存（可选注入）。
	capabilityInvalidator func(tenantID int)
	// SCD4：敏感配置加密盒（bootstrap 注入；nil = 未装配，敏感值写入 fail-closed）。
	secretBox *ConfigSecretBox
	// D6：租户配置保存后的统一失效回调（bootstrap 注入：解析器 + Mailer 缓存失效 + Redis 广播）。
	tenantConfigInvalidator func(tenantID int)
}

func NewSystemConfigService(client *ent.Client, logger *zap.SugaredLogger) *SystemConfigService {
	return &SystemConfigService{
		client:      client,
		logger:      logger,
		policyCache: make(map[int]passwordPolicyCacheEntry),
	}
}

// SetCapabilityInvalidator 注入能力开关缓存失效回调（M2 能力开关）。
//
// 目的：通用配置接口（/system-configs 或 /configs）写入 mcp.*/bot.* 键时，
// 运行时能力开关缓存不得滞留旧值（与专用端点同一失效口径）。
func (s *SystemConfigService) SetCapabilityInvalidator(fn func(tenantID int)) {
	if s == nil {
		return
	}
	s.capabilityInvalidator = fn
}

func (s *SystemConfigService) invalidateCapability(tenantID int) {
	if s != nil && s.capabilityInvalidator != nil {
		s.capabilityInvalidator(tenantID)
	}
}

// SetConfigSecretBox 注入敏感配置加密盒（SCD4；bootstrap 装配）。
func (s *SystemConfigService) SetConfigSecretBox(box *ConfigSecretBox) {
	if s == nil {
		return
	}
	s.secretBox = box
}

// SetTenantConfigInvalidator 注入租户配置失效回调（D6；bootstrap 装配解析器与 Mailer 缓存失效）。
func (s *SystemConfigService) SetTenantConfigInvalidator(fn func(tenantID int)) {
	if s == nil {
		return
	}
	s.tenantConfigInvalidator = fn
}

// InvalidateTenantConfig 租户配置保存后的统一失效入口（D6）：
// 本地密码策略缓存 + 能力开关缓存 + 运行时解析器/Mailer 缓存 + Redis 跨实例广播。
func (s *SystemConfigService) InvalidateTenantConfig(tenantID int) {
	if s == nil || tenantID <= 0 {
		return
	}
	s.InvalidatePasswordPolicy(tenantID)
	s.invalidateCapability(tenantID)
	if s.tenantConfigInvalidator != nil {
		s.tenantConfigInvalidator(tenantID)
	}
}

// encryptSensitiveValue 敏感键写入前加密（SCD4）。
// 非敏感键、空值、已加密值原样返回；加密盒不可用时返回错误（fail-closed，绝不落明文）。
func (s *SystemConfigService) encryptSensitiveValue(key, value string) (string, error) {
	if !IsSensitiveConfigKey(key) || value == "" || IsEncryptedSecret(value) {
		return value, nil
	}
	if s.secretBox == nil {
		return "", fmt.Errorf("配置加密密钥未初始化，拒绝保存敏感配置: %s", key)
	}
	encrypted, err := s.secretBox.Encrypt(value)
	if err != nil {
		return "", err
	}
	return encrypted, nil
}

// writeSensitiveConfigAudit 敏感配置变更审计（只记录键名，不含明文/密文，SCD4）。
func (s *SystemConfigService) writeSensitiveConfigAudit(ctx context.Context, tenantID int, action string, keys []string) {
	if s == nil || s.client == nil || len(keys) == 0 {
		return
	}
	body := "changed_keys=" + strings.Join(keys, ",")
	if len(body) > 512 {
		body = body[:512]
	}
	if _, err := s.client.AuditLog.Create().
		SetTenantID(tenantID).
		SetResource("system_config").
		SetAction(action).
		SetPath("/api/v1/system-configs").
		SetMethod("PUT").
		SetStatusCode(200).
		SetRequestBody(body).
		Save(ctx); err != nil {
		s.logger.Warnw("failed to write system config audit", "error", err, "tenant_id", tenantID, "action", action)
	}
}

// CreateSystemConfig 创建系统配置
func (s *SystemConfigService) CreateSystemConfig(ctx context.Context, req *dto.SystemConfigRequest, tenantID int) (*ent.SystemConfig, error) {
	// 检查key是否已存在
	exists, err := s.client.SystemConfig.Query().
		Where(systemconfig.KeyEQ(req.Key), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		Exist(ctx)
	if err != nil {
		s.logger.Errorf("检查配置key失败: %v", err)
		return nil, fmt.Errorf("检查配置key失败: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("配置key已存在: %s", req.Key)
	}

	storedValue, encErr := s.encryptSensitiveValue(req.Key, req.Value)
	if encErr != nil {
		s.logger.Warnw("encrypt sensitive config failed", "error", encErr, "key", req.Key, "tenant_id", tenantID)
		return nil, encErr
	}

	config, err := s.client.SystemConfig.Create().
		SetKey(req.Key).
		SetValue(storedValue).
		SetValueType(req.ValueType).
		SetCategory(req.Category).
		SetDescription(req.Description).
		SetTenantID(tenantID).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		s.logger.Errorf("创建系统配置失败: %v", err)
		return nil, fmt.Errorf("创建系统配置失败: %w", err)
	}

	s.InvalidateTenantConfig(tenantID)
	if IsSensitiveConfigKey(req.Key) && req.Value != "" {
		s.writeSensitiveConfigAudit(ctx, tenantID, "create_sensitive_config", []string{req.Key})
	}
	return config, nil
}

// GetSystemConfig 获取系统配置
func (s *SystemConfigService) GetSystemConfig(ctx context.Context, id int, tenantID int) (*ent.SystemConfig, error) {
	config, err := s.client.SystemConfig.Query().
		Where(systemconfig.ID(id), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("配置不存在: %d", id)
		}
		s.logger.Errorf("获取系统配置失败: %v", err)
		return nil, fmt.Errorf("获取系统配置失败: %w", err)
	}
	return config, nil
}

// GetSystemConfigByKey 根据key获取配置
func (s *SystemConfigService) GetSystemConfigByKey(ctx context.Context, key string, tenantID int) (*ent.SystemConfig, error) {
	config, err := s.client.SystemConfig.Query().
		Where(systemconfig.KeyEQ(key), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("配置不存在: %s", key)
		}
		s.logger.Errorf("获取系统配置失败: %v", err)
		return nil, fmt.Errorf("获取系统配置失败: %w", err)
	}
	return config, nil
}

// ListSystemConfigs 获取系统配置列表。
// 额外保证：当前租户没有任何配置时，自动补齐全部默认配置键（懒加载）
// 后再返回，避免前端首次进入 /admin/system-config 看到空表单。
func (s *SystemConfigService) ListSystemConfigs(ctx context.Context, tenantID int, category string, page, pageSize int) ([]*ent.SystemConfig, int, error) {
	total, err := s.client.SystemConfig.Query().
		Where(systemconfig.TenantIDEQ(tenantID), systemconfig.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		s.logger.Errorf("获取配置总数失败: %v", err)
		return nil, 0, fmt.Errorf("获取配置总数失败: %w", err)
	}

	// 懒加载首次默认配置（与 InitDefaultConfigs 端点等价，但不要求 UI 手动触发）
	if total == 0 {
		if err := s.InitDefaultConfigs(ctx, tenantID); err != nil {
			s.logger.Warnf("懒加载默认配置失败: %v", err)
		}
	}

	query := s.client.SystemConfig.Query().
		Where(systemconfig.TenantIDEQ(tenantID), systemconfig.DeletedAtIsNil())

	if category != "" {
		query = query.Where(systemconfig.CategoryEQ(category))
	}

	total, err = query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count filtered system configs: %w", err)
	}
	configs, err := query.
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Order(ent.Desc(systemconfig.FieldUpdatedAt)).
		All(ctx)
	if err != nil {
		s.logger.Errorf("获取配置列表失败: %v", err)
		return nil, 0, fmt.Errorf("获取配置列表失败: %w", err)
	}

	return configs, total, nil
}

// UpdateSystemConfig 更新系统配置
func (s *SystemConfigService) UpdateSystemConfig(ctx context.Context, id int, req *dto.UpdateSystemConfigRequest, tenantID int) (*ent.SystemConfig, error) {
	// 检查配置是否存在（取实体以便按 key 判定敏感键与加密）
	existing, err := s.client.SystemConfig.Query().
		Where(systemconfig.ID(id), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("配置不存在: %d", id)
		}
		s.logger.Errorf("检查配置失败: %v", err)
		return nil, fmt.Errorf("检查配置失败: %w", err)
	}

	update := s.client.SystemConfig.UpdateOneID(id).
		Where(systemconfig.TenantIDEQ(tenantID), systemconfig.DeletedAtIsNil()).
		SetUpdatedAt(time.Now())

	sensitiveChanged := false
	if req.Value != "" && !IsMaskedSecretValue(req.Value) {
		storedValue, encErr := s.encryptSensitiveValue(existing.Key, req.Value)
		if encErr != nil {
			s.logger.Warnw("encrypt sensitive config failed", "error", encErr, "key", existing.Key, "tenant_id", tenantID)
			return nil, encErr
		}
		update = update.SetValue(storedValue)
		sensitiveChanged = IsSensitiveConfigKey(existing.Key)
	}
	// 掩码值（******）语义：保持原值不变（前端回显掩码后原样提交的场景）。
	if req.ValueType != "" {
		update = update.SetValueType(req.ValueType)
	}
	if req.Description != "" {
		update = update.SetDescription(req.Description)
	}

	updated, err := update.Save(ctx)
	if err != nil {
		s.logger.Errorf("更新系统配置失败: %v", err)
		return nil, fmt.Errorf("更新系统配置失败: %w", err)
	}

	s.InvalidateTenantConfig(tenantID)
	if sensitiveChanged {
		s.writeSensitiveConfigAudit(ctx, tenantID, "update_sensitive_config", []string{existing.Key})
	}
	return updated, nil
}

// BatchUpdateSystemConfigs 批量更新系统配置
func (s *SystemConfigService) BatchUpdateSystemConfigs(ctx context.Context, configs []dto.UpdateSystemConfigRequest, tenantID int) ([]*ent.SystemConfig, error) {
	results := make([]*ent.SystemConfig, 0, len(configs))
	sensitiveKeys := make([]string, 0, 2)

	for _, cfg := range configs {
		sensitive := IsSensitiveConfigKey(cfg.Key)
		// 掩码值（******）语义：保持原值不变（仅对已有键有意义）。
		skipValue := sensitive && IsMaskedSecretValue(cfg.Value)

		// 尝试查找现有配置
		existing, err := s.client.SystemConfig.Query().
			Where(systemconfig.KeyEQ(cfg.Key), systemconfig.DeletedAtIsNil()).
			Where(systemconfig.TenantIDEQ(tenantID)).
			First(ctx)

		if err == nil && existing != nil {
			// 更新现有配置
			update := s.client.SystemConfig.UpdateOneID(existing.ID).
				SetValueType(cfg.ValueType).
				SetDescription(cfg.Description).
				SetUpdatedAt(time.Now())
			if !skipValue {
				storedValue, encErr := s.encryptSensitiveValue(cfg.Key, cfg.Value)
				if encErr != nil {
					s.logger.Errorw("encrypt sensitive config failed", "error", encErr, "key", cfg.Key, "tenant_id", tenantID)
					continue
				}
				update = update.SetValue(storedValue)
			}
			updated, err := update.Save(ctx)
			if err != nil {
				s.logger.Errorf("更新配置失败: %v", err)
				continue
			}
			results = append(results, updated)
		} else {
			// 创建新配置
			createValue := cfg.Value
			if skipValue {
				createValue = ""
			}
			storedValue, encErr := s.encryptSensitiveValue(cfg.Key, createValue)
			if encErr != nil {
				s.logger.Errorw("encrypt sensitive config failed", "error", encErr, "key", cfg.Key, "tenant_id", tenantID)
				continue
			}
			created, err := s.client.SystemConfig.Create().
				SetKey(cfg.Key).
				SetValue(storedValue).
				SetValueType(cfg.ValueType).
				SetDescription(cfg.Description).
				SetCategory("general").
				SetTenantID(tenantID).
				SetCreatedAt(time.Now()).
				SetUpdatedAt(time.Now()).
				Save(ctx)
			if err != nil {
				s.logger.Errorf("创建配置失败: %v", err)
				continue
			}
			results = append(results, created)
		}
		if sensitive && !skipValue {
			sensitiveKeys = append(sensitiveKeys, cfg.Key)
		}
	}

	// 批量保存（含安全设置页的密码策略）后立即失效缓存，保证保存即生效。
	s.InvalidateTenantConfig(tenantID)
	s.writeSensitiveConfigAudit(ctx, tenantID, "batch_update_sensitive_config", sensitiveKeys)
	return results, nil
}

// DeleteSystemConfig 删除系统配置
func (s *SystemConfigService) DeleteSystemConfig(ctx context.Context, id int, tenantID int) error {
	existing, err := s.client.SystemConfig.Query().
		Where(systemconfig.ID(id), systemconfig.DeletedAtIsNil()).
		Where(systemconfig.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("配置不存在: %d", id)
		}
		s.logger.Errorf("检查配置失败: %v", err)
		return fmt.Errorf("检查配置失败: %w", err)
	}

	_, err = s.client.SystemConfig.UpdateOneID(id).
		Where(systemconfig.TenantIDEQ(tenantID), systemconfig.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		s.logger.Errorf("删除系统配置失败: %v", err)
		return fmt.Errorf("删除系统配置失败: %w", err)
	}

	s.InvalidateTenantConfig(tenantID)
	if IsSensitiveConfigKey(existing.Key) {
		s.writeSensitiveConfigAudit(ctx, tenantID, "delete_sensitive_config", []string{existing.Key})
	}
	return nil
}

// InitDefaultConfigs 初始化默认配置。
// 覆盖前端 /admin/system-config 表单的 24 个键，避免首次进入页面看到空表单。
// 新增配置键必须同时在本表与前端 SystemConfiguration 表单中声明。
// 例外：attachment.* 分组为运维灰度开关（方案 §6.1），只在本表登记、不进前端表单。
func (s *SystemConfigService) InitDefaultConfigs(ctx context.Context, tenantID int) error {
	defaultConfigs := []dto.SystemConfigRequest{
		// —— 通用设置 ——
		{Key: "systemName", Value: "ITSM系统", ValueType: "string", Category: "general", Description: "系统名称"},
		{Key: "systemUrl", Value: "http://localhost:3000", ValueType: "string", Category: "general", Description: "系统URL"},
		{Key: "timezone", Value: "Asia/Shanghai", ValueType: "string", Category: "general", Description: "时区"},
		{Key: "language", Value: "zh-CN", ValueType: "string", Category: "general", Description: "语言"},
		{Key: "dateFormat", Value: "YYYY-MM-DD", ValueType: "string", Category: "general", Description: "日期格式"},
		{Key: "timeFormat", Value: "24h", ValueType: "string", Category: "general", Description: "时间格式"},
		// —— 会话设置 ——
		{Key: "sessionTimeout", Value: "30", ValueType: "number", Category: "session", Description: "会话超时时间(分钟)"},
		// —— 上传设置 ——
		{Key: "maxFileSize", Value: "10", ValueType: "number", Category: "upload", Description: "最大文件大小(MB)"},
		{Key: "allowedFileTypes", Value: ".pdf,.doc,.docx,.xls,.xlsx,.jpg,.png,.gif", ValueType: "string", Category: "upload", Description: "允许的文件类型"},
		// —— 附件域灰度开关（方案 §6.1；按租户覆盖部署级默认值，键名与 config.yaml 的 attachment 块一致） ——
		{Key: "attachment.generic_read_enabled", Value: "false", ValueType: "boolean", Category: "attachment", Description: "通用附件读路径灰度（按租户）"},
		{Key: "attachment.generic_write_enabled", Value: "false", ValueType: "boolean", Category: "attachment", Description: "通用附件写路径灰度（按租户）"},
		{Key: "attachment.dual_write_enabled", Value: "false", ValueType: "boolean", Category: "attachment", Description: "附件双写对账（P2）"},
		{Key: "attachment.inline_image_enabled", Value: "false", ValueType: "boolean", Category: "attachment", Description: "富文本内嵌图片走通用链路"},
		// —— 安全设置：密码策略 ——
		{Key: "passwordMinLength", Value: "8", ValueType: "number", Category: "security", Description: "密码最小长度"},
		{Key: "passwordRequireUppercase", Value: "true", ValueType: "boolean", Category: "security", Description: "需要大写字母"},
		{Key: "passwordRequireLowercase", Value: "true", ValueType: "boolean", Category: "security", Description: "需要小写字母"},
		{Key: "passwordRequireNumbers", Value: "true", ValueType: "boolean", Category: "security", Description: "需要数字"},
		{Key: "passwordRequireSpecialChars", Value: "false", ValueType: "boolean", Category: "security", Description: "需要特殊字符"},
		// —— 安全设置：账户安全 ——
		{Key: "loginMaxAttempts", Value: "5", ValueType: "number", Category: "security", Description: "登录失败次数限制"},
		{Key: "accountLockoutDuration", Value: "30", ValueType: "number", Category: "security", Description: "账户锁定时间(分钟)"},
		{Key: "enable2FA", Value: "false", ValueType: "boolean", Category: "security", Description: "启用双因素认证"},
		// —— 邮件设置：SMTP ——
		// 回退链（方案 D5）：租户值 > 部署级 env/config.yaml > 未配置（发信点告警跳过）。
		// 主机/用户名/发件人默认留空，表示"未配置"；占位值（example.com）在运行时同样按未配置处理。
		{Key: "smtpHost", Value: "", ValueType: "string", Category: "email", Description: "SMTP服务器（留空回退部署级配置）"},
		{Key: "smtpPort", Value: "465", ValueType: "number", Category: "email", Description: "SMTP端口"},
		{Key: "smtpUsername", Value: "", ValueType: "string", Category: "email", Description: "SMTP用户名（留空回退部署级配置）"},
		{Key: "smtpPassword", Value: "", ValueType: "string", Category: "email", Description: "SMTP密码"},
		{Key: "smtpEnableSSL", Value: "true", ValueType: "boolean", Category: "email", Description: "启用SSL/TLS"},
		// —— 邮件设置：模板 ——
		{Key: "emailFrom", Value: "", ValueType: "string", Category: "email", Description: "发件人邮箱（留空回退部署级配置）"},
		{Key: "systemNotificationTemplate", Value: "您好 {username}：\n您有一条新的系统通知：{message}\n— ITSM 团队", ValueType: "string", Category: "email", Description: "系统通知模板"},
	}

	for _, cfg := range defaultConfigs {
		// 检查是否已存在
		exists, err := s.client.SystemConfig.Query().
			Where(systemconfig.KeyEQ(cfg.Key), systemconfig.DeletedAtIsNil()).
			Where(systemconfig.TenantIDEQ(tenantID)).
			Exist(ctx)
		if err != nil {
			s.logger.Errorf("检查配置失败: %v", err)
			continue
		}
		if exists {
			continue
		}

		// 创建配置（敏感键加密，SCD4）
		storedValue, encErr := s.encryptSensitiveValue(cfg.Key, cfg.Value)
		if encErr != nil {
			s.logger.Errorf("加密默认配置失败: %v", encErr)
			continue
		}
		_, err = s.client.SystemConfig.Create().
			SetKey(cfg.Key).
			SetValue(storedValue).
			SetValueType(cfg.ValueType).
			SetCategory(cfg.Category).
			SetDescription(cfg.Description).
			SetTenantID(tenantID).
			SetCreatedAt(time.Now()).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			s.logger.Errorf("创建默认配置失败: %v", err)
		}
	}

	s.InvalidateTenantConfig(tenantID)
	return nil
}
