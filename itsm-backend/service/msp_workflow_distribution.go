package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/middleware"

	"go.uber.org/zap"
)

// 跨租户单据流转 P2-①：模板分发（copy-in）。
//
// 语义（方案 §7.2 / XFD5 定稿 D5=A、D6=A）：
//   - provider 设计、客户持有副本、运行时零跨租户引用（保持 B4/I6）；
//   - 分发 = 在客户租户创建**草稿副本**（draft，来源追踪）；发布/绑定由客户租户内既有流程完成；
//   - 仅 provider 已发布（published）版本可分发；重复分发在客户无同 key 草稿时生成新版本草稿（提示升级，不强制覆盖）；
//   - 授权 = mspguard（allocation + 客户 active）唯一入口；写入按客户租户重绑定 ctx（RLS GUC 同源）；
//   - 审计 = workflow_template.distribute（target_tenant + source=workbench，XF8）。

// customerAccessChecker 抽象 mspguard 授权入口（*MSPAccessValidator 实现；测试注入替身）。
type customerAccessChecker interface {
	CanAccessCustomer(ctx context.Context, mspUserID, customerTenantID int) error
}

// MSPWorkflowDistributionService 模板分发服务（provider 侧写通道）。
type MSPWorkflowDistributionService struct {
	db      *sql.DB
	catalog *BPMNWorkflowTemplateCatalog
	guard   customerAccessChecker
	client  *ent.Client // 审计写入（可空：测试/降级时跳过）
	logger  *zap.SugaredLogger
}

func NewMSPWorkflowDistributionService(
	db *sql.DB,
	catalog *BPMNWorkflowTemplateCatalog,
	guard customerAccessChecker,
	client *ent.Client,
	logger *zap.SugaredLogger,
) *MSPWorkflowDistributionService {
	return &MSPWorkflowDistributionService{db: db, catalog: catalog, guard: guard, client: client, logger: logger}
}

const workflowTemplateDistributionColumns = "id, tenant_id, provider_tenant_id, source_template_key, source_version, " +
	"customer_template_key, customer_template_version, distributed_by, distributed_at"

// providerTemplateCopy 分发源快照（JSON 列按原样字节复制，避免二次序列化失真）。
type providerTemplateCopy struct {
	Key         string
	Name        string
	Description string
	Domain      string
	Form        []byte
	Approval    []byte
	Bindings    []byte
	SLA         []byte
	BPMNXML     []byte
	Version     string
}

// Distribute 将 provider 已发布模板分发到客户租户（创建草稿副本 + 来源追踪）。
func (s *MSPWorkflowDistributionService) Distribute(
	ctx context.Context,
	actor MSPWorkbenchActor,
	customerTenantID int,
	key, version string,
) (*dto.DistributeWorkflowTemplateResponse, error) {
	if s == nil || s.db == nil || s.catalog == nil {
		return nil, fmt.Errorf("模板分发服务未配置")
	}
	key = strings.TrimSpace(key)
	version = strings.TrimSpace(version)
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 || key == "" {
		return nil, fmt.Errorf("%w: 分发参数不完整", ErrWorkflowTemplateInvalid)
	}
	if s.guard == nil {
		return nil, fmt.Errorf("模板分发授权校验未配置")
	}
	// 授权唯一入口：allocation + 客户归属 + 客户 active（mspguard）。
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}

	// 1) provider 侧源模板：仅已发布版本（指定版本或最新）。
	src, err := s.loadPublishedProviderTemplate(ctx, actor.HomeTenantID, key, version)
	if err != nil {
		return nil, err
	}

	// 2) 写入方向 = 客户租户（RLS enforce 下 GUC 单值 = 写入语义）。
	wctx := tenantctx.WithTenantID(ctx, customerTenantID)

	// 3) 客户已有同 key 草稿 → 冲突（要求客户先处理草稿，避免覆盖未决编辑）。
	var draftExists bool
	if err := s.db.QueryRowContext(wctx,
		"SELECT EXISTS(SELECT 1 FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='draft')",
		customerTenantID, key).Scan(&draftExists); err != nil {
		return nil, fmt.Errorf("检查客户草稿失败: %w", err)
	}
	if draftExists {
		return nil, ErrWorkflowTemplateConflict
	}

	// 4) 客户侧版本推进（沿用目录 nextVersion 口径）。
	next, err := s.catalog.nextVersion(wctx, customerTenantID, key)
	if err != nil {
		return nil, err
	}

	// 5) copy-in：客户租户内创建草稿副本（JSON/BPMN 原样复制；is_public 强制 false）。
	var copyID int
	if err := s.db.QueryRowContext(wctx, `INSERT INTO workflow_templates
		(key,name,description,domain,form_schema,approval_policy,ontology_bindings,sla_config,bpmn_xml,version,status,is_public,tenant_id,created_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft',false,$11,$12,NOW(),NOW()) RETURNING id`,
		src.Key, src.Name, src.Description, src.Domain,
		string(src.Form), string(src.Approval), string(src.Bindings), string(src.SLA), string(src.BPMNXML),
		next, customerTenantID, actor.UserID).Scan(&copyID); err != nil {
		return nil, fmt.Errorf("创建客户模板副本失败: %w", err)
	}

	// 6) 来源追踪行（每次分发一行，历史可追溯）。
	var distID int
	var distributedAt time.Time
	if err := s.db.QueryRowContext(wctx, `INSERT INTO workflow_template_distributions
		(tenant_id,provider_tenant_id,source_template_key,source_version,customer_template_key,customer_template_version,distributed_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, distributed_at`,
		customerTenantID, actor.HomeTenantID, key, src.Version, key, next, actor.UserID).Scan(&distID, &distributedAt); err != nil {
		return nil, fmt.Errorf("记录模板分发来源失败: %w", err)
	}

	// 7) 审计（best-effort，XF8：target_tenant + source）。
	s.recordDistributionAudit(ctx, actor, customerTenantID, key, src.Version, next, "success")

	return &dto.DistributeWorkflowTemplateResponse{Distribution: &dto.WorkflowTemplateDistribution{
		ID:                      distID,
		CustomerTenantID:        customerTenantID,
		ProviderTenantID:        actor.HomeTenantID,
		SourceTemplateKey:       key,
		SourceVersion:           src.Version,
		CustomerTemplateKey:     key,
		CustomerTemplateVersion: next,
		CustomerTemplateStatus:  "draft",
		DistributedBy:           actor.UserID,
		DistributedAt:           distributedAt,
		LatestProviderVersion:   src.Version,
		UpgradeAvailable:        false,
	}}, nil
}

// ListDistributions 分发状态：provider 视角列出对某客户的分发记录 + 升级提示（D6=A：提示不强制）。
func (s *MSPWorkflowDistributionService) ListDistributions(
	ctx context.Context,
	actor MSPWorkbenchActor,
	customerTenantID int,
) (*dto.WorkflowTemplateDistributionListResponse, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("模板分发服务未配置")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 || customerTenantID <= 0 {
		return nil, fmt.Errorf("%w: 查询参数不完整", ErrWorkflowTemplateInvalid)
	}
	if s.guard == nil {
		return nil, fmt.Errorf("模板分发授权校验未配置")
	}
	if err := s.guard.CanAccessCustomer(ctx, actor.UserID, customerTenantID); err != nil {
		return nil, err
	}

	rctx := tenantctx.WithTenantID(ctx, customerTenantID)
	rows, err := s.db.QueryContext(rctx, "SELECT "+workflowTemplateDistributionColumns+
		" FROM workflow_template_distributions WHERE tenant_id=$1 AND provider_tenant_id=$2 ORDER BY distributed_at DESC, id DESC",
		customerTenantID, actor.HomeTenantID)
	if err != nil {
		return nil, fmt.Errorf("查询分发记录失败: %w", err)
	}
	defer rows.Close()

	items := make([]*dto.WorkflowTemplateDistribution, 0)
	latestCache := make(map[string]string)
	for rows.Next() {
		var item dto.WorkflowTemplateDistribution
		if err := rows.Scan(&item.ID, &item.CustomerTenantID, &item.ProviderTenantID,
			&item.SourceTemplateKey, &item.SourceVersion, &item.CustomerTemplateKey,
			&item.CustomerTemplateVersion, &item.DistributedBy, &item.DistributedAt); err != nil {
			return nil, fmt.Errorf("读取分发记录失败: %w", err)
		}
		// 客户副本当前状态（可能已被客户发布/归档；缺失不阻断）。
		var status string
		if err := s.db.QueryRowContext(rctx,
			"SELECT status FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND version=$3",
			customerTenantID, item.CustomerTemplateKey, item.CustomerTemplateVersion).Scan(&status); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("读取客户副本状态失败: %w", err)
		}
		item.CustomerTemplateStatus = status

		// 升级提示：provider 最新已发布版本 vs 分发时来源版本。
		latest, ok := latestCache[item.SourceTemplateKey]
		if !ok {
			var v string
			if err := s.db.QueryRowContext(rctx,
				"SELECT version FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='published' ORDER BY updated_at DESC, id DESC LIMIT 1",
				actor.HomeTenantID, item.SourceTemplateKey).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("读取 provider 最新版本失败: %w", err)
			}
			latest = v
			latestCache[item.SourceTemplateKey] = v
		}
		item.LatestProviderVersion = latest
		item.UpgradeAvailable = latest != "" && latest != item.SourceVersion

		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取分发记录失败: %w", err)
	}
	return &dto.WorkflowTemplateDistributionListResponse{Items: items, Total: len(items)}, nil
}

// loadPublishedProviderTemplate 读取 provider 已发布模板（指定版本或最新）。
func (s *MSPWorkflowDistributionService) loadPublishedProviderTemplate(ctx context.Context, providerTenantID int, key, version string) (*providerTemplateCopy, error) {
	query := "SELECT key,name,description,domain,form_schema,approval_policy,ontology_bindings,sla_config,bpmn_xml,version" +
		" FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='published'"
	args := []interface{}{providerTenantID, key}
	if version != "" {
		query += " AND version=$3"
		args = append(args, version)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT 1"

	var src providerTemplateCopy
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&src.Key, &src.Name, &src.Description, &src.Domain,
		&src.Form, &src.Approval, &src.Bindings, &src.SLA, &src.BPMNXML, &src.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取 provider 模板失败: %w", err)
	}
	return &src, nil
}

// recordDistributionAudit 分发审计（事件 workflow_template.distribute；target_tenant + source=workbench）。
// ctx 必须携带 actor 家租户（enforce 下审计行 tenant_id = 当前租户作用域，fail-closed）。
func (s *MSPWorkflowDistributionService) recordDistributionAudit(
	ctx context.Context,
	actor MSPWorkbenchActor,
	customerTenantID int,
	key, sourceVersion, customerVersion, outcome string,
) {
	if s.client == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"source":                    "workbench",
		"op":                        "distribute",
		"actor_user_id":             actor.UserID,
		"actor_home_tenant_id":      actor.HomeTenantID,
		"target_tenant_id":          customerTenantID,
		"source_template_key":       key,
		"source_version":            sourceVersion,
		"customer_template_key":     key,
		"customer_template_version": customerVersion,
		"outcome":                   outcome,
	})
	rowTenant := actor.HomeTenantID
	if rowTenant <= 0 {
		rowTenant = customerTenantID
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.client.AuditLog.Create().
		SetCreatedAt(time.Now()).
		SetTenantID(rowTenant).
		SetUserID(actor.UserID).
		SetActorAccount(actor.Username).
		SetTargetTenantID(customerTenantID).
		SetSource(middleware.AuditSourceWorkbench).
		SetResource("workflow_template").
		SetAction("workflow_template.distribute").
		SetPath("/api/v1/msp/customers/:customer_tenant_id/workflow-templates/:key/distribute").
		SetMethod("POST").
		SetStatusCode(map[bool]int{true: 200, false: 403}[outcome == "success"]).
		SetRequestBody(string(payload)).
		Save(auditCtx); err != nil && s.logger != nil {
		s.logger.Warnw("workflow distribution audit write failed", "error", err, "key", key, "customer_tenant_id", customerTenantID)
	}
}
