package seeder

import (
	"context"
	"fmt"
	"sort"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/approvalworkflow"
	"itsm-backend/ent/bottemplate"
	"itsm-backend/ent/bottoolgrant"
	"itsm-backend/ent/citype"
	"itsm-backend/ent/group"
	"itsm-backend/ent/menu"
	"itsm-backend/ent/permission"
	"itsm-backend/ent/processbinding"
	"itsm-backend/ent/processdefinition"
	"itsm-backend/ent/processdeployment"
	"itsm-backend/ent/role"
	"itsm-backend/ent/rolepermission"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/systemconfig"
	"itsm-backend/ent/tenant"
	"itsm-backend/internal/authz"
	"itsm-backend/pkg/tenantmode"
)

// CurrentTenantTemplateVersion 单一事实来源在 dto：service 层不能 import 本包
// （本包反向依赖 service），共享常量避免版本字符串两处维护。
const CurrentTenantTemplateVersion = dto.DefaultTenantTemplateVersion

// provisionBulkChunkSize 是模板供给批量写入的单条 INSERT 行数上限。
// 供给基线规模约 4–5k 行（角色权限绑定占大头）；逐条写入在跨网/隧道环境下会被
// 往返延迟放大成分钟级。2026-10-06 改为「批量加载 + 内存比对 + 批量写入」：
// 总查询数从数千降到数十，幂等语义与字段集保持不变。
const provisionBulkChunkSize = 500

// ProvisionTenant installs the tenant-scoped operating baseline from the
// verified default tenant. The operation is transactional and idempotent.
//
// 性能约定：克隆与校验只允许「每类资源常数次」批量查询，禁止在源/目标行上
// 再逐行查询（N+1）。回归守护见 TestProvisionTenantQueryBudgetStaysBatched。
func (s *Seeder) ProvisionTenant(ctx context.Context, tenantID int, templateVersion string) error {
	if tenantID <= 0 {
		return fmt.Errorf("tenant id must be positive")
	}
	if templateVersion != CurrentTenantTemplateVersion {
		return fmt.Errorf("unsupported tenant template version %q", templateVersion)
	}
	source, err := s.client.Tenant.Query().Where(tenant.CodeEQ("default")).Only(ctx)
	if err != nil {
		return fmt.Errorf("load product template tenant: %w", err)
	}
	target, err := s.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("load target tenant: %w", err)
	}
	if source.ID == tenantID {
		return s.validateTenantReadiness(ctx, tenantID)
	}
	// 客户类型租户内的 msp_* 角色按 C2 客户侧基线供给（实施计划 §3.0-C2），
	// 与 provider 租户的 C1 跨租户矩阵区分（canon §2.3：深度切换复用客户侧页面与权限模型）。
	customerScope := tenantmode.IsCustomerTenantType(string(target.Type))

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant provisioning: %w", err)
	}
	defer tx.Rollback()
	c := tx.Client()

	if err := cloneTenantBaseline(ctx, c, source.ID, tenantID); err != nil {
		return err
	}
	if customerScope {
		if err := applyCustomerScopeMSPRoleBaseline(ctx, c, tenantID); err != nil {
			return err
		}
	}
	if err := cloneTenantTemplates(ctx, c, source.ID, tenantID); err != nil {
		return err
	}
	if err := validateTenantClone(ctx, c, source.ID, tenantID, customerScope); err != nil {
		return fmt.Errorf("validate tenant template before commit: %w", err)
	}
	versionKey := fmt.Sprintf("tenant.bootstrap.version.%d", tenantID)
	version, err := c.SystemConfig.Query().Where(systemconfig.KeyEQ(versionKey), systemconfig.DeletedAtIsNil()).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = c.SystemConfig.Create().
			SetKey(versionKey).SetValue(templateVersion).SetCategory("bootstrap").
			SetDescription("Tenant product template version").SetCreatedBy("system").
			SetTenantID(tenantID).Save(ctx)
	} else if err == nil {
		_, err = version.Update().SetValue(templateVersion).Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("record tenant template version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant provisioning: %w", err)
	}
	return validateTenantReadinessWithClient(ctx, s.client, tenantID)
}

// chunkSlice 按 size 切分批次；空输入或 size<=0 时不分片。
func chunkSlice[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(items)
	}
	chunks := make([][]T, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

// cloneTenantBaseline 克隆租户 RBAC 与组织骨架：角色、权限、角色权限绑定、菜单、审批组。
// 全部走「批量加载目标 + 内存差集 + 批量写入」；已存在记录跳过，保留目标租户自定义。
func cloneTenantBaseline(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	sourceRoles, err := c.Role.Query().Where(role.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetRoleByCode, err := loadRolesByCode(ctx, c, tenantID)
	if err != nil {
		return err
	}
	if err := createMissingRoles(ctx, c, sourceRoles, targetRoleByCode, tenantID); err != nil {
		return err
	}

	sourcePermissions, err := c.Permission.Query().Where(permission.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetPermissionByCode, err := loadPermissionsByCode(ctx, c, tenantID)
	if err != nil {
		return err
	}
	if err := createMissingPermissions(ctx, c, sourcePermissions, targetPermissionByCode, tenantID); err != nil {
		return err
	}

	if err := cloneRolePermissions(ctx, c, sourceID, tenantID, sourceRoles, sourcePermissions, targetRoleByCode, targetPermissionByCode); err != nil {
		return err
	}
	if err := cloneMenus(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	return cloneGroups(ctx, c, sourceID, tenantID)
}

func loadRolesByCode(ctx context.Context, c *ent.Client, tenantID int) (map[string]*ent.Role, error) {
	items, err := c.Role.Query().Where(role.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]*ent.Role, len(items))
	for _, item := range items {
		byCode[item.Code] = item
	}
	return byCode, nil
}

func loadPermissionsByCode(ctx context.Context, c *ent.Client, tenantID int) (map[string]*ent.Permission, error) {
	items, err := c.Permission.Query().Where(permission.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]*ent.Permission, len(items))
	for _, item := range items {
		byCode[item.Code] = item
	}
	return byCode, nil
}

func createMissingRoles(ctx context.Context, c *ent.Client, source []*ent.Role, target map[string]*ent.Role, tenantID int) error {
	missing := make([]*ent.Role, 0, len(source))
	for _, item := range source {
		if _, ok := target[item.Code]; !ok {
			missing = append(missing, item)
		}
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.RoleCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.Role.Create().
				SetCode(item.Code).SetName(item.Name).SetDescription(item.Description).
				SetIsSystem(item.IsSystem).SetIsActive(item.IsActive).SetTenantID(tenantID))
		}
		created, err := c.Role.CreateBulk(builders...).Save(ctx)
		if err != nil {
			return fmt.Errorf("provision role %s: %w", batch[0].Code, err)
		}
		for _, item := range created {
			target[item.Code] = item
		}
	}
	return nil
}

func createMissingPermissions(ctx context.Context, c *ent.Client, source []*ent.Permission, target map[string]*ent.Permission, tenantID int) error {
	missing := make([]*ent.Permission, 0, len(source))
	for _, item := range source {
		if _, ok := target[item.Code]; !ok {
			missing = append(missing, item)
		}
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.PermissionCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.Permission.Create().
				SetCode(item.Code).SetName(item.Name).SetDescription(item.Description).
				SetResource(item.Resource).SetAction(item.Action).SetTenantID(tenantID))
		}
		created, err := c.Permission.CreateBulk(builders...).Save(ctx)
		if err != nil {
			return fmt.Errorf("provision permission %s: %w", batch[0].Code, err)
		}
		for _, item := range created {
			target[item.Code] = item
		}
	}
	return nil
}

// cloneRolePermissions 以 (role, permission) 为目标去重键补齐授权：
// 源授权一次性加载，目标现有授权一次性加载，差集批量插入（幂等）。
func cloneRolePermissions(
	ctx context.Context,
	c *ent.Client,
	sourceID, tenantID int,
	sourceRoles []*ent.Role,
	sourcePermissions []*ent.Permission,
	targetRoleByCode map[string]*ent.Role,
	targetPermissionByCode map[string]*ent.Permission,
) error {
	sourceRoleCodeByID := make(map[int]string, len(sourceRoles))
	for _, item := range sourceRoles {
		sourceRoleCodeByID[item.ID] = item.Code
	}
	sourcePermissionCodeByID := make(map[int]string, len(sourcePermissions))
	for _, item := range sourcePermissions {
		sourcePermissionCodeByID[item.ID] = item.Code
	}

	sourceGrants, err := c.RolePermission.Query().Where(rolepermission.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetGrants, err := c.RolePermission.Query().Where(rolepermission.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	type grantPair struct{ roleID, permissionID int }
	existing := make(map[grantPair]struct{}, len(targetGrants))
	for _, item := range targetGrants {
		existing[grantPair{item.RoleID, item.PermissionID}] = struct{}{}
	}

	pending := make([]grantPair, 0)
	for _, item := range sourceGrants {
		roleCode, ok := sourceRoleCodeByID[item.RoleID]
		if !ok {
			return fmt.Errorf("provision role permission: source role %d not found", item.RoleID)
		}
		permissionCode, ok := sourcePermissionCodeByID[item.PermissionID]
		if !ok {
			return fmt.Errorf("provision role permission: source permission %d not found", item.PermissionID)
		}
		targetRole, ok := targetRoleByCode[roleCode]
		if !ok {
			return fmt.Errorf("provision role permission: role %s missing in target tenant", roleCode)
		}
		targetPermission, ok := targetPermissionByCode[permissionCode]
		if !ok {
			return fmt.Errorf("provision role permission: permission %s missing in target tenant", permissionCode)
		}
		pair := grantPair{targetRole.ID, targetPermission.ID}
		if _, ok := existing[pair]; ok {
			continue
		}
		existing[pair] = struct{}{}
		pending = append(pending, pair)
	}

	for _, batch := range chunkSlice(pending, provisionBulkChunkSize) {
		builders := make([]*ent.RolePermissionCreate, 0, len(batch))
		for _, pair := range batch {
			builders = append(builders, c.RolePermission.Create().
				SetRoleID(pair.roleID).SetPermissionID(pair.permissionID).SetTenantID(tenantID))
		}
		if _, err := c.RolePermission.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision role permission: %w", err)
		}
	}
	return nil
}

// applyCustomerScopeMSPRoleBaseline 把客户类型租户内 msp_* 角色的授权从 C1（跨租户 msp_* 码，
// 随 default 租户克隆落入）对账到 C2 客户侧基线（实施计划 §3.0-C2；canon §2.3 深度切换）：
//   - 移除「C1 有、C2 无」的码（错误基线残留），保留其它自定义扩展；
//   - 补齐 C2 缺失码；批量加载 + 内存差集 + 批量写入（幂等）。
//
// ReconcileCustomerScopeMSPRoles 为导出包装：存量客户租户回填入口（cmd/reconcile_customer_scope_roles），
// 与供给期同一实现（幂等）。C2 基线演进（如 IP-P2-14 新增 ticket:assign/problem:write）后，
// 存量租户需重跑本对账。
func ReconcileCustomerScopeMSPRoles(ctx context.Context, c *ent.Client, tenantID int) error {
	return applyCustomerScopeMSPRoleBaseline(ctx, c, tenantID)
}

func applyCustomerScopeMSPRoleBaseline(ctx context.Context, c *ent.Client, tenantID int) error {
	desired := authz.MSPCustomerScopeRolePermissionCodes()
	builtin := authz.BuiltinRolePermissionCodes()

	roleCodes := make([]string, 0, len(desired))
	for code := range desired {
		roleCodes = append(roleCodes, code)
	}
	sort.Strings(roleCodes)
	roles, err := c.Role.Query().Where(role.TenantIDEQ(tenantID), role.CodeIn(roleCodes...)).All(ctx)
	if err != nil {
		return fmt.Errorf("load customer-scope MSP roles: %w", err)
	}
	if len(roles) == 0 {
		return nil
	}
	permissions, err := c.Permission.Query().Where(permission.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load customer-scope permissions: %w", err)
	}
	permissionIDByCode := make(map[string]int, len(permissions))
	permissionCodeByID := make(map[int]string, len(permissions))
	for _, item := range permissions {
		permissionIDByCode[item.Code] = item.ID
		permissionCodeByID[item.ID] = item.Code
	}
	grants, err := c.RolePermission.Query().Where(rolepermission.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load customer-scope grants: %w", err)
	}
	roleCodeByID := make(map[int]string, len(roles))
	for _, item := range roles {
		roleCodeByID[item.ID] = item.Code
	}
	have := make(map[int]map[string]struct{}, len(roles))
	for _, item := range grants {
		if _, ok := roleCodeByID[item.RoleID]; !ok {
			continue
		}
		code, ok := permissionCodeByID[item.PermissionID]
		if !ok {
			continue
		}
		if have[item.RoleID] == nil {
			have[item.RoleID] = make(map[string]struct{})
		}
		have[item.RoleID][code] = struct{}{}
	}

	adds := make([]*ent.RolePermissionCreate, 0, len(roleCodes)*4)
	for _, item := range roles {
		want, ok := desired[item.Code]
		if !ok {
			continue
		}
		wantSet := make(map[string]struct{}, len(want))
		for _, code := range want {
			wantSet[code] = struct{}{}
			if _, exists := have[item.ID][code]; exists {
				continue
			}
			permissionID, ok := permissionIDByCode[code]
			if !ok {
				return fmt.Errorf("customer-scope baseline permission %s missing in tenant %d", code, tenantID)
			}
			adds = append(adds, c.RolePermission.Create().
				SetRoleID(item.ID).SetPermissionID(permissionID).SetTenantID(tenantID))
		}
		// 移除 C1 残留（C1 有、C2 无）：跨租户 msp_* 码不应出现在客户租户 msp 角色上。
		removeIDs := make([]int, 0, len(builtin[item.Code]))
		for _, code := range builtin[item.Code] {
			if _, inWant := wantSet[code]; inWant {
				continue
			}
			if _, exists := have[item.ID][code]; !exists {
				continue
			}
			if permissionID, ok := permissionIDByCode[code]; ok {
				removeIDs = append(removeIDs, permissionID)
			}
		}
		if len(removeIDs) > 0 {
			if _, err := c.RolePermission.Delete().
				Where(rolepermission.RoleIDEQ(item.ID), rolepermission.PermissionIDIn(removeIDs...)).
				Exec(ctx); err != nil {
				return fmt.Errorf("strip C1 baseline from %s: %w", item.Code, err)
			}
		}
	}
	for _, batch := range chunkSlice(adds, provisionBulkChunkSize) {
		if _, err := c.RolePermission.CreateBulk(batch...).Save(ctx); err != nil {
			return fmt.Errorf("provision customer-scope MSP baseline: %w", err)
		}
	}
	return nil
}

func cloneMenus(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	sourceMenus, err := c.Menu.Query().Where(menu.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetMenus, err := c.Menu.Query().Where(menu.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(targetMenus))
	for _, item := range targetMenus {
		existing[item.Path] = struct{}{}
	}
	missing := make([]*ent.Menu, 0)
	for _, item := range sourceMenus {
		if _, ok := existing[item.Path]; ok {
			continue
		}
		existing[item.Path] = struct{}{}
		missing = append(missing, item)
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.MenuCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.Menu.Create().
				SetName(item.Name).SetPath(item.Path).SetIcon(item.Icon).
				SetPermissionCode(item.PermissionCode).SetSortOrder(item.SortOrder).
				SetIsVisible(item.IsVisible).SetIsEnabled(item.IsEnabled).
				SetDescription(item.Description).SetTenantID(tenantID))
		}
		if _, err := c.Menu.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision menu %s: %w", batch[0].Path, err)
		}
	}
	return nil
}

// 审批组克隆：BPMN candidateGroups / assignee_type(group) 依赖 groups 表，
// 缺失时任务候选集为空（2026-09-15 复盘 R2）。组是租户级骨架数据，成员关系
// 不克隆（成员用户尚未开通），由管理员开通后从角色人员中拉入。
func cloneGroups(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	sourceGroups, err := c.Group.Query().Where(group.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetGroups, err := c.Group.Query().Where(group.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(targetGroups))
	for _, item := range targetGroups {
		existing[item.Name] = struct{}{}
	}
	missing := make([]*ent.Group, 0)
	for _, item := range sourceGroups {
		if _, ok := existing[item.Name]; ok {
			continue
		}
		existing[item.Name] = struct{}{}
		missing = append(missing, item)
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.GroupCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.Group.Create().
				SetName(item.Name).SetDescription(item.Description).
				SetTenantID(tenantID))
		}
		if _, err := c.Group.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision group %s: %w", batch[0].Name, err)
		}
	}
	return nil
}

// cloneTenantTemplates 克隆租户产品模板的其余部分：SLA 定义、CI 类型、审批工作流、
// 流程定义/部署/绑定、Bot 模板与工具授权。批量加载 + 内存差集 + 批量写入，幂等。
func cloneTenantTemplates(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	if err := cloneSLADefinitions(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	if err := cloneCITypes(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	if err := cloneApprovalWorkflows(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	if err := cloneProcessDefinitions(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	if err := cloneProcessBindings(ctx, c, sourceID, tenantID); err != nil {
		return err
	}
	return cloneBotTemplates(ctx, c, sourceID, tenantID)
}

func cloneSLADefinitions(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	source, err := c.SLADefinition.Query().Where(sladefinition.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	target, err := c.SLADefinition.Query().Where(sladefinition.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(target))
	for _, item := range target {
		existing[item.Name] = struct{}{}
	}
	missing := make([]*ent.SLADefinition, 0)
	for _, item := range source {
		if _, ok := existing[item.Name]; ok {
			continue
		}
		existing[item.Name] = struct{}{}
		missing = append(missing, item)
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.SLADefinitionCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.SLADefinition.Create().
				SetName(item.Name).SetDescription(item.Description).SetServiceType(item.ServiceType).
				SetPriority(item.Priority).SetResponseTime(item.ResponseTime).SetResolutionTime(item.ResolutionTime).
				SetBusinessHours(item.BusinessHours).SetEscalationRules(item.EscalationRules).
				SetConditions(item.Conditions).SetIsActive(item.IsActive).SetTenantID(tenantID))
		}
		if _, err := c.SLADefinition.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision SLA %s: %w", batch[0].Name, err)
		}
	}
	return nil
}

func cloneCITypes(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	source, err := c.CIType.Query().Where(citype.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	target, err := c.CIType.Query().Where(citype.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(target))
	for _, item := range target {
		existing[item.Name] = struct{}{}
	}
	missing := make([]*ent.CIType, 0)
	for _, item := range source {
		if _, ok := existing[item.Name]; ok {
			continue
		}
		existing[item.Name] = struct{}{}
		missing = append(missing, item)
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.CITypeCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.CIType.Create().
				SetName(item.Name).SetDescription(item.Description).SetIcon(item.Icon).SetColor(item.Color).
				SetAttributeSchema(item.AttributeSchema).SetIsActive(item.IsActive).SetTenantID(tenantID))
		}
		if _, err := c.CIType.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision CI type %s: %w", batch[0].Name, err)
		}
	}
	return nil
}

func cloneApprovalWorkflows(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	source, err := c.ApprovalWorkflow.Query().Where(approvalworkflow.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	target, err := c.ApprovalWorkflow.Query().Where(approvalworkflow.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(target))
	for _, item := range target {
		existing[item.Name] = struct{}{}
	}
	missing := make([]*ent.ApprovalWorkflow, 0)
	for _, item := range source {
		if _, ok := existing[item.Name]; ok {
			continue
		}
		existing[item.Name] = struct{}{}
		missing = append(missing, item)
	}
	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.ApprovalWorkflowCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.ApprovalWorkflow.Create().
				SetName(item.Name).SetDescription(item.Description).SetTicketType(item.TicketType).
				SetPriority(item.Priority).SetNodes(item.Nodes).SetStatus(item.Status).
				SetIsActive(item.IsActive).SetTenantID(tenantID))
		}
		if _, err := c.ApprovalWorkflow.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision approval workflow %s: %w", batch[0].Name, err)
		}
	}
	return nil
}

// cloneProcessDefinitions 克隆流程定义及其部署记录：
// 部署按目标 ID（"<源部署ID>-tenant-<租户>"）去重批量创建，定义按 (key, version) 差集批量创建。
func cloneProcessDefinitions(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	sourceDefs, err := c.ProcessDefinition.Query().Where(processdefinition.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	if len(sourceDefs) == 0 {
		return nil
	}
	sourceDeployments, err := c.ProcessDeployment.Query().Where(processdeployment.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	sourceDeploymentByID := make(map[int]*ent.ProcessDeployment, len(sourceDeployments))
	for _, item := range sourceDeployments {
		sourceDeploymentByID[item.ID] = item
	}
	targetDefs, err := c.ProcessDefinition.Query().Where(processdefinition.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	type definitionKey struct{ key, version string }
	existing := make(map[definitionKey]struct{}, len(targetDefs))
	for _, item := range targetDefs {
		existing[definitionKey{item.Key, item.Version}] = struct{}{}
	}
	missing := make([]*ent.ProcessDefinition, 0)
	for _, item := range sourceDefs {
		key := definitionKey{item.Key, item.Version}
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		missing = append(missing, item)
	}
	if len(missing) == 0 {
		return nil
	}

	targetDeployments, err := c.ProcessDeployment.Query().Where(processdeployment.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	targetDeploymentByCode := make(map[string]*ent.ProcessDeployment, len(targetDeployments))
	for _, item := range targetDeployments {
		targetDeploymentByCode[item.DeploymentID] = item
	}
	// 需要创建的部署：按目标部署 ID 去重；源部署缺失时保持旧错误语义（load process deployment for <key>）。
	missingDeployments := make([]*ent.ProcessDeployment, 0)
	scheduled := make(map[string]struct{})
	for _, item := range missing {
		sourceDeployment, ok := sourceDeploymentByID[item.DeploymentID]
		if !ok {
			return fmt.Errorf("load process deployment for %s: %w", item.Key, provisionNotFoundError("process_deployment"))
		}
		targetDeploymentID := fmt.Sprintf("%s-tenant-%d", sourceDeployment.DeploymentID, tenantID)
		if _, ok := targetDeploymentByCode[targetDeploymentID]; ok {
			continue
		}
		if _, ok := scheduled[targetDeploymentID]; ok {
			continue
		}
		scheduled[targetDeploymentID] = struct{}{}
		missingDeployments = append(missingDeployments, sourceDeployment)
	}
	if len(missingDeployments) > 0 {
		for _, batch := range chunkSlice(missingDeployments, provisionBulkChunkSize) {
			builders := make([]*ent.ProcessDeploymentCreate, 0, len(batch))
			for _, item := range batch {
				builders = append(builders, c.ProcessDeployment.Create().
					SetDeploymentID(fmt.Sprintf("%s-tenant-%d", item.DeploymentID, tenantID)).
					SetDeploymentName(item.DeploymentName).
					SetDeploymentSource(item.DeploymentSource).
					SetDeployedBy("tenant-provisioner").
					SetDeploymentComment(item.DeploymentComment).
					SetIsActive(item.IsActive).
					SetDeploymentCategory(item.DeploymentCategory).
					SetDeploymentMetadata(item.DeploymentMetadata).
					SetTenantID(tenantID))
			}
			if _, err := c.ProcessDeployment.CreateBulk(builders...).Save(ctx); err != nil {
				return fmt.Errorf("provision process deployment %s: %w", batch[0].DeploymentID, err)
			}
		}
		// 回读一次拿到新建部署的 ID（批量插入不保证按顺序回填目标映射）。
		targetDeployments, err = c.ProcessDeployment.Query().Where(processdeployment.TenantIDEQ(tenantID)).All(ctx)
		if err != nil {
			return err
		}
		for _, item := range targetDeployments {
			targetDeploymentByCode[item.DeploymentID] = item
		}
	}

	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.ProcessDefinitionCreate, 0, len(batch))
		for _, item := range batch {
			sourceDeployment := sourceDeploymentByID[item.DeploymentID]
			targetDeployment := targetDeploymentByCode[fmt.Sprintf("%s-tenant-%d", sourceDeployment.DeploymentID, tenantID)]
			if targetDeployment == nil {
				return fmt.Errorf("provision process definition %s: target deployment %s missing", item.Key, sourceDeployment.DeploymentID)
			}
			builders = append(builders, c.ProcessDefinition.Create().
				SetKey(item.Key).SetName(item.Name).SetDescription(item.Description).
				SetVersion(item.Version).SetCategory(item.Category).SetBpmnXML(item.BpmnXML).
				SetProcessVariables(item.ProcessVariables).SetIsActive(item.IsActive).
				SetIsLatest(item.IsLatest).SetDeploymentID(targetDeployment.ID).
				SetDeploymentName(targetDeployment.DeploymentName).SetTenantID(tenantID))
		}
		if _, err := c.ProcessDefinition.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision process definition %s: %w", batch[0].Key, err)
		}
	}
	return nil
}

// cloneProcessBindings 克隆激活的流程绑定；目标定义按 key 取激活的最大版本（与旧逻辑一致）。
func cloneProcessBindings(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	source, err := c.ProcessBinding.Query().Where(
		processbinding.TenantIDEQ(sourceID),
		processbinding.IsActiveEQ(true),
	).All(ctx)
	if err != nil {
		return err
	}
	if len(source) == 0 {
		return nil
	}
	target, err := c.ProcessBinding.Query().Where(
		processbinding.TenantIDEQ(tenantID),
		processbinding.IsActiveEQ(true),
	).All(ctx)
	if err != nil {
		return err
	}
	type bindingKey struct {
		businessType string
		businessSub  string
		departmentID int
		teamID       int
		scenario     string
		category     string
	}
	existing := make(map[bindingKey]struct{}, len(target))
	for _, item := range target {
		existing[bindingKey{item.BusinessType, item.BusinessSubType, item.DepartmentID, item.TeamID, item.Scenario, item.Category}] = struct{}{}
	}
	missing := make([]*ent.ProcessBinding, 0)
	for _, item := range source {
		key := bindingKey{item.BusinessType, item.BusinessSubType, item.DepartmentID, item.TeamID, item.Scenario, item.Category}
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		missing = append(missing, item)
	}
	if len(missing) == 0 {
		return nil
	}

	targetDefs, err := c.ProcessDefinition.Query().Where(
		processdefinition.TenantIDEQ(tenantID),
		processdefinition.IsActiveEQ(true),
	).All(ctx)
	if err != nil {
		return err
	}
	targetActiveDefByKey := make(map[string]*ent.ProcessDefinition, len(targetDefs))
	for _, item := range targetDefs {
		current := targetActiveDefByKey[item.Key]
		if current == nil || item.Version > current.Version {
			targetActiveDefByKey[item.Key] = item
		}
	}

	for _, batch := range chunkSlice(missing, provisionBulkChunkSize) {
		builders := make([]*ent.ProcessBindingCreate, 0, len(batch))
		for _, item := range batch {
			targetDefinition := targetActiveDefByKey[item.ProcessDefinitionKey]
			if targetDefinition == nil {
				return fmt.Errorf("resolve process definition %s: %w", item.ProcessDefinitionKey, provisionNotFoundError("process_definition"))
			}
			builders = append(builders, c.ProcessBinding.Create().
				SetBusinessType(item.BusinessType).SetBusinessSubType(item.BusinessSubType).
				SetProcessDefinitionKey(item.ProcessDefinitionKey).SetProcessVersion(item.ProcessVersion).
				SetIsDefault(item.IsDefault).SetPriority(item.Priority).SetIsActive(item.IsActive).
				SetScenario(item.Scenario).SetCategory(item.Category).SetConditions(item.Conditions).
				SetOverrides(item.Overrides).SetTenantID(tenantID).
				SetProcessDefinitionID(targetDefinition.ID))
		}
		if _, err := c.ProcessBinding.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision process binding: %w", err)
		}
	}
	return nil
}

// cloneBotTemplates 克隆 Bot 模板与工具授权（B2-01 默认助手 + B3 场景 pilot）：
// 与菜单/SLA 同级的租户产品基线；同 slug 已存在时跳过模板字段（管理员修改优先），
// 授权按 (bot, tool) 去重补齐。
func cloneBotTemplates(ctx context.Context, c *ent.Client, sourceID, tenantID int) error {
	sourceBots, err := c.BotTemplate.Query().Where(bottemplate.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	if len(sourceBots) == 0 {
		return nil
	}
	targetBots, err := c.BotTemplate.Query().Where(bottemplate.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	targetBySlug := make(map[string]*ent.BotTemplate, len(targetBots))
	for _, item := range targetBots {
		targetBySlug[item.Slug] = item
	}
	sourceSlugByID := make(map[int]string, len(sourceBots))
	missingBots := make([]*ent.BotTemplate, 0)
	for _, item := range sourceBots {
		sourceSlugByID[item.ID] = item.Slug
		if _, ok := targetBySlug[item.Slug]; ok {
			continue
		}
		missingBots = append(missingBots, item)
	}
	if len(missingBots) > 0 {
		for _, batch := range chunkSlice(missingBots, provisionBulkChunkSize) {
			builders := make([]*ent.BotTemplateCreate, 0, len(batch))
			for _, item := range batch {
				builders = append(builders, c.BotTemplate.Create().
					SetSlug(item.Slug).SetName(item.Name).SetAudience(item.Audience).
					SetRiskLimit(item.RiskLimit).SetEntrypointsJSON(item.EntrypointsJSON).
					SetSystemPromptRef(item.SystemPromptRef).SetStatus(item.Status).
					SetTenantID(tenantID))
			}
			created, err := c.BotTemplate.CreateBulk(builders...).Save(ctx)
			if err != nil {
				return fmt.Errorf("provision bot template %s: %w", batch[0].Slug, err)
			}
			for _, item := range created {
				targetBySlug[item.Slug] = item
			}
		}
	}

	sourceGrants, err := c.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetGrants, err := c.BotToolGrant.Query().Where(bottoolgrant.TenantIDEQ(tenantID)).All(ctx)
	if err != nil {
		return err
	}
	type grantKey struct {
		botID    int
		toolName string
	}
	existingGrants := make(map[grantKey]struct{}, len(targetGrants))
	for _, item := range targetGrants {
		existingGrants[grantKey{item.BotID, item.ToolName}] = struct{}{}
	}
	type pendingGrant struct {
		botID     int
		botSlug   string
		toolName  string
		riskLimit string
		argsJSON  string
	}
	pending := make([]pendingGrant, 0)
	for _, item := range sourceGrants {
		slug, ok := sourceSlugByID[item.BotID]
		if !ok {
			return fmt.Errorf("provision bot grant: source bot %d not found", item.BotID)
		}
		targetBot, ok := targetBySlug[slug]
		if !ok {
			return fmt.Errorf("provision bot template %s: missing in target tenant", slug)
		}
		key := grantKey{targetBot.ID, item.ToolName}
		if _, ok := existingGrants[key]; ok {
			continue
		}
		existingGrants[key] = struct{}{}
		pending = append(pending, pendingGrant{targetBot.ID, slug, item.ToolName, item.RiskLimit, item.ArgsPolicyJSON})
	}
	for _, batch := range chunkSlice(pending, provisionBulkChunkSize) {
		builders := make([]*ent.BotToolGrantCreate, 0, len(batch))
		for _, item := range batch {
			builders = append(builders, c.BotToolGrant.Create().
				SetToolName(item.toolName).SetRiskLimit(item.riskLimit).
				SetArgsPolicyJSON(item.argsJSON).
				SetTenantID(tenantID).SetBotID(item.botID))
		}
		if _, err := c.BotToolGrant.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("provision bot grant %s/%s: %w", batch[0].botSlug, batch[0].toolName, err)
		}
	}
	return nil
}

// provisionNotFoundError 复刻 ent NotFound 文案（旧实现由 Only() 产生），
// 供批量校验在缺少目标记录时保持错误语义可读。
func provisionNotFoundError(label string) error {
	return fmt.Errorf("ent: %s not found", label)
}

func (s *Seeder) validateTenantReadiness(ctx context.Context, tenantID int) error {
	return validateTenantReadinessWithClient(ctx, s.client, tenantID)
}

// validateTenantClone 在提交前做等价完整性校验（批量加载 + 内存比对，无逐行查询）：
// 角色/权限/授权/菜单/组/流程定义/流程绑定必须齐全，且激活绑定必须有激活流程定义。
// 错误顺序与文案保持与旧实现一致（"managed role/permission/grant/..."）。
func validateTenantClone(ctx context.Context, client *ent.Client, sourceID, targetID int, customerScope bool) error {
	sourceRoles, err := client.Role.Query().Where(role.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetRoleByCode, err := loadRolesByCode(ctx, client, targetID)
	if err != nil {
		return err
	}

	sourcePermissions, err := client.Permission.Query().Where(permission.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	sourcePermissionCodeByID := make(map[int]string, len(sourcePermissions))
	for _, item := range sourcePermissions {
		sourcePermissionCodeByID[item.ID] = item.Code
	}
	targetPermissionByCode, err := loadPermissionsByCode(ctx, client, targetID)
	if err != nil {
		return err
	}

	sourceGrants, err := client.RolePermission.Query().Where(rolepermission.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	grantsByRole := make(map[int][]int, len(sourceRoles))
	for _, item := range sourceGrants {
		grantsByRole[item.RoleID] = append(grantsByRole[item.RoleID], item.PermissionID)
	}
	targetGrants, err := client.RolePermission.Query().Where(rolepermission.TenantIDEQ(targetID)).All(ctx)
	if err != nil {
		return err
	}
	type grantPair struct{ roleID, permissionID int }
	targetGrantSet := make(map[grantPair]struct{}, len(targetGrants))
	for _, item := range targetGrants {
		targetGrantSet[grantPair{item.RoleID, item.PermissionID}] = struct{}{}
	}

	customerBaseline := map[string][]string(nil)
	if customerScope {
		customerBaseline = authz.MSPCustomerScopeRolePermissionCodes()
	}
	for _, sourceRole := range sourceRoles {
		targetRole, ok := targetRoleByCode[sourceRole.Code]
		if !ok {
			return fmt.Errorf("managed role %s missing: %w", sourceRole.Code, provisionNotFoundError("role"))
		}
		if override, ok := customerBaseline[sourceRole.Code]; ok {
			// 客户租户内 msp_* 角色按 C2 基线验收：C1 跨租户码已被
			// applyCustomerScopeMSPRoleBaseline 替换，不要求与源租户同名授权一致。
			for _, permissionCode := range override {
				targetPermission, ok := targetPermissionByCode[permissionCode]
				if !ok {
					return fmt.Errorf("managed permission %s missing: %w", permissionCode, provisionNotFoundError("permission"))
				}
				if _, ok := targetGrantSet[grantPair{targetRole.ID, targetPermission.ID}]; !ok {
					return fmt.Errorf("managed grant %s -> %s missing", sourceRole.Code, permissionCode)
				}
			}
			continue
		}
		for _, permissionID := range grantsByRole[sourceRole.ID] {
			permissionCode, ok := sourcePermissionCodeByID[permissionID]
			if !ok {
				return fmt.Errorf("managed permission %d missing: %w", permissionID, provisionNotFoundError("permission"))
			}
			targetPermission, ok := targetPermissionByCode[permissionCode]
			if !ok {
				return fmt.Errorf("managed permission %s missing: %w", permissionCode, provisionNotFoundError("permission"))
			}
			if _, ok := targetGrantSet[grantPair{targetRole.ID, targetPermission.ID}]; !ok {
				return fmt.Errorf("managed grant %s -> %s missing", sourceRole.Code, permissionCode)
			}
		}
	}

	sourceMenus, err := client.Menu.Query().Where(menu.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetMenus, err := client.Menu.Query().Where(menu.TenantIDEQ(targetID)).All(ctx)
	if err != nil {
		return err
	}
	targetMenuPaths := make(map[string]struct{}, len(targetMenus))
	for _, item := range targetMenus {
		targetMenuPaths[item.Path] = struct{}{}
	}
	for _, item := range sourceMenus {
		if _, ok := targetMenuPaths[item.Path]; !ok {
			return fmt.Errorf("managed menu %s missing", item.Path)
		}
	}

	sourceGroups, err := client.Group.Query().Where(group.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetGroups, err := client.Group.Query().Where(group.TenantIDEQ(targetID)).All(ctx)
	if err != nil {
		return err
	}
	targetGroupNames := make(map[string]struct{}, len(targetGroups))
	for _, item := range targetGroups {
		targetGroupNames[item.Name] = struct{}{}
	}
	for _, item := range sourceGroups {
		if _, ok := targetGroupNames[item.Name]; !ok {
			return fmt.Errorf("managed group %s missing", item.Name)
		}
	}

	sourceDefinitions, err := client.ProcessDefinition.Query().Where(processdefinition.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetDefinitions, err := client.ProcessDefinition.Query().Where(processdefinition.TenantIDEQ(targetID)).All(ctx)
	if err != nil {
		return err
	}
	type definitionKey struct{ key, version string }
	targetDefinitionSet := make(map[definitionKey]struct{}, len(targetDefinitions))
	activeDefinitionKeys := make(map[string]struct{})
	for _, item := range targetDefinitions {
		targetDefinitionSet[definitionKey{item.Key, item.Version}] = struct{}{}
		if item.IsActive {
			activeDefinitionKeys[item.Key] = struct{}{}
		}
	}
	for _, item := range sourceDefinitions {
		if _, ok := targetDefinitionSet[definitionKey{item.Key, item.Version}]; !ok {
			return fmt.Errorf("managed process definition %s@%s missing", item.Key, item.Version)
		}
	}

	sourceBindings, err := client.ProcessBinding.Query().Where(processbinding.TenantIDEQ(sourceID)).All(ctx)
	if err != nil {
		return err
	}
	targetBindings, err := client.ProcessBinding.Query().Where(processbinding.TenantIDEQ(targetID)).All(ctx)
	if err != nil {
		return err
	}
	type bindingKey struct{ businessType, businessSub, definitionKey string }
	targetBindingSet := make(map[bindingKey]struct{}, len(targetBindings))
	for _, item := range targetBindings {
		targetBindingSet[bindingKey{item.BusinessType, item.BusinessSubType, item.ProcessDefinitionKey}] = struct{}{}
	}
	for _, item := range sourceBindings {
		if _, ok := targetBindingSet[bindingKey{item.BusinessType, item.BusinessSubType, item.ProcessDefinitionKey}]; !ok {
			return fmt.Errorf("managed process binding %s/%s missing", item.BusinessType, item.BusinessSubType)
		}
	}

	activeTargetBindings, err := client.ProcessBinding.Query().Where(
		processbinding.TenantIDEQ(targetID),
		processbinding.IsActiveEQ(true),
	).All(ctx)
	if err != nil {
		return err
	}
	for _, item := range activeTargetBindings {
		if _, ok := activeDefinitionKeys[item.ProcessDefinitionKey]; !ok {
			return fmt.Errorf("active binding %s has no active process definition", item.ProcessDefinitionKey)
		}
	}
	return validateTenantReadinessWithClient(ctx, client, targetID)
}
