package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/bottemplate"
	"itsm-backend/ent/bottoolgrant"
)

// TemplateAdmin 提供 Bot 模板与工具授权的管理服务（B2-01）。
//
// 设计边界（与 B2-02 的分工）：
//   - 本服务只做**管理面**的 CRUD 与校验（slug/status/risk_limit/entrypoints 合法性；
//     授权风险上限不得超过模板上限）；
//   - 「授权 ∩ RBAC ∩ 风险上限 ∩ 入口」的**运行时交集门禁**属 B2-02（BotPolicy.FilterTools），
//     本服务不参与工具面组装。
//
// 级联策略：删除模板时在同一事务内先删授权再删模板（不依赖驱动层 FK 行为，
// SQLite 与 Postgres 语义一致；schema 侧仍保留 required 边保证引用完整性）。
type TemplateAdmin struct {
	client *ent.Client
}

// 稳定错误（handler 据此映射 HTTP 状态码）。
var (
	ErrTemplateNotFound = errors.New("bot: 模板不存在")
	ErrGrantNotFound    = errors.New("bot: 授权不存在")
	ErrTemplateSlugUsed = errors.New("bot: 同租户内 slug 已存在")
	ErrValidation       = errors.New("bot: 参数非法")
)

// 模板状态与风险级别（与 service.ToolRisk* 同字面量；本包不反向依赖 service，避免循环导入）。
const (
	StatusDraft = "draft"
	StatusPilot = "pilot"
	StatusGA    = "ga"

	RiskRead      = "read"
	RiskPlan      = "plan"
	RiskActLow    = "act_low"
	RiskActMedium = "act_medium"
	RiskActHigh   = "act_high"
)

// DefaultTemplateSlug 是内置「默认助手」种子模板的 slug（B2-01 兼容默认）。
const DefaultTemplateSlug = "default-assistant"

var riskRank = map[string]int{
	RiskRead: 0, RiskPlan: 1, RiskActLow: 2, RiskActMedium: 3, RiskActHigh: 4,
}

// RiskRank 返回风险级别的可比序（越大越危险）；未知级别返回 -1。
func RiskRank(level string) int {
	if rank, ok := riskRank[level]; ok {
		return rank
	}
	return -1
}

// TemplateInput 是模板创建/更新的入参（空字符串表示不改动；创建时使用默认值）。
type TemplateInput struct {
	Slug            string
	Name            string
	Audience        string
	RiskLimit       string
	Entrypoints     []string
	SystemPromptRef string
	Status          string
}

// GrantInput 是授权 upsert 的入参。
type GrantInput struct {
	ToolName       string
	RiskLimit      string
	ArgsPolicyJSON string
}

func NewTemplateAdmin(client *ent.Client) *TemplateAdmin {
	return &TemplateAdmin{client: client}
}

func validateStatus(status string) error {
	switch status {
	case StatusDraft, StatusPilot, StatusGA:
		return nil
	default:
		return fmt.Errorf("%w: status 必须是 draft|pilot|ga，实际 %q", ErrValidation, status)
	}
}

func validateRisk(level, what string) error {
	if RiskRank(level) < 0 {
		return fmt.Errorf("%w: %s 风险级别非法（read|plan|act_low|act_medium|act_high），实际 %q",
			ErrValidation, what, level)
	}
	return nil
}

func validateEntrypoints(entrypoints []string) error {
	for _, entry := range entrypoints {
		if strings.TrimSpace(entry) == "" {
			return fmt.Errorf("%w: entrypoints 不得包含空字符串", ErrValidation)
		}
	}
	return nil
}

func entrypointsJSON(entrypoints []string) (string, error) {
	if entrypoints == nil {
		entrypoints = []string{}
	}
	raw, err := json.Marshal(entrypoints)
	if err != nil {
		return "", fmt.Errorf("%w: entrypoints 序列化失败: %v", ErrValidation, err)
	}
	return string(raw), nil
}

// ListTemplates 返回租户下全部模板（按 id 升序）。
//
// 兼容默认：租户**当前没有任何模板**时，幂等种入内置「默认助手」（SeedDefaultTemplate），
// 保证管理面与运行时始终有一个与既有行为等价的兜底模板。
// 注意语义：非空租户不会被自动改写（改名/改状态/删单个模板均保留管理员意图）；
// 但把模板**全部删光**后再访问会重新种入默认助手（保护性兜底，属既定行为）。
func (a *TemplateAdmin) ListTemplates(ctx context.Context, tenantID int) ([]*ent.BotTemplate, error) {
	rows, err := a.client.BotTemplate.Query().
		Where(bottemplate.TenantID(tenantID)).
		Order(ent.Asc(bottemplate.FieldID)).
		All(ctx)
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	if _, _, err := a.SeedDefaultTemplate(ctx, tenantID); err != nil {
		return nil, err
	}
	return a.client.BotTemplate.Query().
		Where(bottemplate.TenantID(tenantID)).
		Order(ent.Asc(bottemplate.FieldID)).
		All(ctx)
}

// —— audience 取值与可见性（B2-04 工作区选择器）——

// audience 约定取值：internal（内部人员，B2-01 缺省）/ end_user（终端用户）/ all（不限）。
const (
	AudienceInternal = "internal"
	AudienceEndUser  = "end_user"
	AudienceAll      = "all"
)

// VisibleForRole 判定某 audience 的 Bot 对角色是否可见（fail-closed）。
//
// 规则：空值视为 internal；end_user 角色仅可见 end_user/all；其余角色可见
// internal/all/end_user；未知取值仅内部角色可见（宁可少露不可多露）。
func VisibleForRole(audience, role string) bool {
	audience = strings.ToLower(strings.TrimSpace(audience))
	if audience == "" {
		audience = AudienceInternal
	}
	isEndUser := strings.EqualFold(strings.TrimSpace(role), AudienceEndUser)
	switch audience {
	case AudienceAll, AudienceEndUser:
		return true
	case AudienceInternal:
		return !isEndUser
	default:
		return !isEndUser
	}
}

// ListVisibleForChat 返回工作区选择器可见的 Bot（B2-04）。
//
// 过滤：① 非 draft（draft 不下发，与策略层一致）；② audience 对角色可见；
// ③ 入口允许 chat（空/损坏 entrypoints 一律不可见，与策略层 fail-closed 同口径）。
//
// 兼容默认：租户无模板时沿用 ListTemplates 的幂等种入（内置默认助手必然在列）。
func (a *TemplateAdmin) ListVisibleForChat(ctx context.Context, tenantID int, role string) ([]*ent.BotTemplate, error) {
	rows, err := a.ListTemplates(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	visible := make([]*ent.BotTemplate, 0, len(rows))
	for _, tpl := range rows {
		if strings.EqualFold(strings.TrimSpace(tpl.Status), StatusDraft) {
			continue
		}
		if !VisibleForRole(tpl.Audience, role) {
			continue
		}
		if !entrypointAllowed(tpl.EntrypointsJSON, EntrypointChat) {
			continue
		}
		visible = append(visible, tpl)
	}
	return visible, nil
}

// GetTemplate 返回模板 + 授权（租户隔离；不存在返回 ErrTemplateNotFound）。
func (a *TemplateAdmin) GetTemplate(ctx context.Context, tenantID, id int) (*ent.BotTemplate, error) {
	found, err := a.client.BotTemplate.Query().
		Where(bottemplate.ID(id), bottemplate.TenantID(tenantID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrTemplateNotFound
	}
	return found, err
}

// CreateTemplate 创建模板（slug 租户内唯一；status 默认 draft）。
func (a *TemplateAdmin) CreateTemplate(ctx context.Context, tenantID int, in TemplateInput) (*ent.BotTemplate, error) {
	slug := strings.TrimSpace(in.Slug)
	name := strings.TrimSpace(in.Name)
	if slug == "" || name == "" {
		return nil, fmt.Errorf("%w: slug/name 必填", ErrValidation)
	}
	status := in.Status
	if status == "" {
		status = StatusDraft
	}
	if err := validateStatus(status); err != nil {
		return nil, err
	}
	risk := in.RiskLimit
	if risk == "" {
		risk = RiskActLow
	}
	if err := validateRisk(risk, "risk_limit"); err != nil {
		return nil, err
	}
	if err := validateEntrypoints(in.Entrypoints); err != nil {
		return nil, err
	}
	raw, err := entrypointsJSON(in.Entrypoints)
	if err != nil {
		return nil, err
	}

	audience := strings.TrimSpace(in.Audience)
	if audience == "" {
		audience = "internal"
	}

	builder := a.client.BotTemplate.Create().
		SetTenantID(tenantID).
		SetSlug(slug).
		SetName(name).
		SetAudience(audience).
		SetRiskLimit(risk).
		SetEntrypointsJSON(raw).
		SetSystemPromptRef(strings.TrimSpace(in.SystemPromptRef)).
		SetStatus(status)
	created, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("%w: slug=%s", ErrTemplateSlugUsed, slug)
		}
		return nil, err
	}
	return created, nil
}

// UpdateTemplate 更新模板（nil 字段不改动；slug 不可改——改名走 name）。
func (a *TemplateAdmin) UpdateTemplate(ctx context.Context, tenantID, id int, in TemplateInput) (*ent.BotTemplate, error) {
	current, err := a.GetTemplate(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	update := a.client.BotTemplate.UpdateOneID(current.ID).Where(bottemplate.TenantID(tenantID))
	if name := strings.TrimSpace(in.Name); name != "" {
		update = update.SetName(name)
	}
	if in.Audience != "" {
		update = update.SetAudience(strings.TrimSpace(in.Audience))
	}
	if in.RiskLimit != "" {
		if err := validateRisk(in.RiskLimit, "risk_limit"); err != nil {
			return nil, err
		}
		update = update.SetRiskLimit(in.RiskLimit)
	}
	if in.Status != "" {
		if err := validateStatus(in.Status); err != nil {
			return nil, err
		}
		update = update.SetStatus(in.Status)
	}
	if in.Entrypoints != nil {
		if err := validateEntrypoints(in.Entrypoints); err != nil {
			return nil, err
		}
		raw, err := entrypointsJSON(in.Entrypoints)
		if err != nil {
			return nil, err
		}
		update = update.SetEntrypointsJSON(raw)
	}
	if in.SystemPromptRef != "" {
		update = update.SetSystemPromptRef(strings.TrimSpace(in.SystemPromptRef))
	}

	if _, err := update.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTemplateNotFound
		}
		return nil, err
	}
	return a.GetTemplate(ctx, tenantID, id)
}

// DeleteTemplate 删除模板并级联删除其授权（同一事务；不存在返回 ErrTemplateNotFound）。
//
// 顺序：**先删授权再删模板**——SQLite（`_fk=1`）与 Postgres 都在父行被引用时拒绝删除，
// 反过来做会撞 FK 约束；两步同一事务保证不出现「模板已删、授权残留」。
func (a *TemplateAdmin) DeleteTemplate(ctx context.Context, tenantID, id int) error {
	if _, err := a.GetTemplate(ctx, tenantID, id); err != nil {
		return err
	}
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.BotToolGrant.Delete().
		Where(bottoolgrant.TenantID(tenantID), bottoolgrant.BotID(id)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	deleted, err := tx.BotTemplate.Delete().
		Where(bottemplate.ID(id), bottemplate.TenantID(tenantID)).
		Exec(ctx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if deleted == 0 {
		_ = tx.Rollback()
		return ErrTemplateNotFound
	}
	return tx.Commit()
}

// ListGrants 返回模板的授权清单（租户隔离；模板不存在返回 ErrTemplateNotFound）。
func (a *TemplateAdmin) ListGrants(ctx context.Context, tenantID, botID int) ([]*ent.BotToolGrant, error) {
	if _, err := a.GetTemplate(ctx, tenantID, botID); err != nil {
		return nil, err
	}
	return a.client.BotToolGrant.Query().
		Where(bottoolgrant.TenantID(tenantID), bottoolgrant.BotID(botID)).
		Order(ent.Asc(bottoolgrant.FieldToolName)).
		All(ctx)
}

// UpsertGrant 新增或更新一条授权（同模板同工具唯一）。
//
// 校验：授权风险上限**不得超过模板上限**（管理面防线；运行时交集由 B2-02 判定）。
func (a *TemplateAdmin) UpsertGrant(ctx context.Context, tenantID, botID int, in GrantInput) (*ent.BotToolGrant, error) {
	toolName := strings.TrimSpace(in.ToolName)
	if toolName == "" {
		return nil, fmt.Errorf("%w: tool_name 必填", ErrValidation)
	}
	tpl, err := a.GetTemplate(ctx, tenantID, botID)
	if err != nil {
		return nil, err
	}
	risk := in.RiskLimit
	if risk == "" {
		risk = RiskActLow
	}
	if err := validateRisk(risk, "grant.risk_limit"); err != nil {
		return nil, err
	}
	if RiskRank(risk) > RiskRank(tpl.RiskLimit) {
		return nil, fmt.Errorf("%w: 授权风险 %s 超过模板上限 %s", ErrValidation, risk, tpl.RiskLimit)
	}

	existing, err := a.client.BotToolGrant.Query().
		Where(bottoolgrant.TenantID(tenantID), bottoolgrant.BotID(botID), bottoolgrant.ToolName(toolName)).
		Only(ctx)
	switch {
	case err == nil:
		updated, err := a.client.BotToolGrant.UpdateOneID(existing.ID).
			SetRiskLimit(risk).
			SetArgsPolicyJSON(in.ArgsPolicyJSON).
			Save(ctx)
		if ent.IsNotFound(err) {
			return nil, ErrGrantNotFound
		}
		return updated, err
	case ent.IsNotFound(err):
		created, err := a.client.BotToolGrant.Create().
			SetTenantID(tenantID).
			SetBotID(botID).
			SetToolName(toolName).
			SetRiskLimit(risk).
			SetArgsPolicyJSON(in.ArgsPolicyJSON).
			Save(ctx)
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("%w: 同模板同工具授权已存在（并发创建），请重试", ErrValidation)
		}
		return created, err
	default:
		return nil, err
	}
}

// DeleteGrant 删除一条授权（不属于该模板/租户返回 ErrGrantNotFound）。
func (a *TemplateAdmin) DeleteGrant(ctx context.Context, tenantID, botID, grantID int) error {
	deleted, err := a.client.BotToolGrant.Delete().
		Where(bottoolgrant.ID(grantID), bottoolgrant.TenantID(tenantID), bottoolgrant.BotID(botID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return ErrGrantNotFound
	}
	return nil
}

// SeedDefaultTemplate 保证租户存在内置「默认助手」模板（幂等；B2-01 兼容默认）。
//
// 语义等价现状：允许入口 chat、风险上限 act_low、状态 ga、系统提示为空（沿用既有内置技能）。
// 已存在（含被改名/改状态）时不覆盖——管理员的显式修改优先。
func (a *TemplateAdmin) SeedDefaultTemplate(ctx context.Context, tenantID int) (*ent.BotTemplate, bool, error) {
	existing, err := a.client.BotTemplate.Query().
		Where(bottemplate.TenantID(tenantID), bottemplate.SlugEQ(DefaultTemplateSlug)).
		Only(ctx)
	if err == nil {
		return existing, false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, err
	}
	created, err := a.client.BotTemplate.Create().
		SetTenantID(tenantID).
		SetSlug(DefaultTemplateSlug).
		SetName("默认助手").
		SetAudience("internal").
		SetRiskLimit(RiskActLow).
		SetEntrypointsJSON(`["chat"]`).
		SetStatus(StatusGA).
		Save(ctx)
	if ent.IsConstraintError(err) {
		// 并发种子：让位给已落库的那条。
		existing, getErr := a.client.BotTemplate.Query().
			Where(bottemplate.TenantID(tenantID), bottemplate.SlugEQ(DefaultTemplateSlug)).
			Only(ctx)
		if getErr != nil {
			return nil, false, getErr
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}
