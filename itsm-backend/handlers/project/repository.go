package project

import (
	"context"
	"time"

	"itsm-backend/ent"
)

// ListFilter 列表过滤（P0：状态/类型/归口团队/负责人/关键字 + page/pageSize）。
type ListFilter struct {
	TenantID    int
	Status      string
	Type        string
	OwnerTeamID int
	ManagerID   int
	Keyword     string
	// ProjectIDs 可见性收窄（非管理角色）；nil=不限，空切片=无可见项目（由服务层短路）。
	ProjectIDs []int
	Offset     int
	Limit      int
}

// CreateParams 创建参数。
type CreateParams struct {
	TenantID     int
	Name         string
	Code         string
	Description  string
	Type         string
	Status       string
	ManagerID    int
	DepartmentID int
	OwnerTeamID  int
	StartDate    *time.Time
	EndDate      *time.Time
	// P2（PE-22）轻量预算：nil=不设置；amount>0 时写入（币种可空=租户默认）。
	BudgetAmount   *float64
	BudgetCurrency *string
}

// UpdateParams 更新参数（指针 nil = 不修改）。
type UpdateParams struct {
	Name         *string
	Description  *string
	Type         *string
	Progress     *int
	ManagerID    *int
	DepartmentID *int
	OwnerTeamID  *int
	StartDate    *time.Time
	EndDate      *time.Time
	// P2（PE-22）预算：nil=不修改；非 nil=写入（amount<=0 或空串 → 清空，见 service.normalizeBudget）。
	BudgetAmount   *float64
	BudgetCurrency *string
}

// TaskHoursSummary 任务工时汇总（PE-22；口径=项目全部未删任务，不做状态过滤）。
type TaskHoursSummary struct {
	TaskCount          int
	EstimatedTaskCount int
	ActualTaskCount    int
	EstimateHours      float64
	ActualHours        float64
}

// StatusPatch 状态流转附带写字段。
type StatusPatch struct {
	ActualStart *time.Time
	ActualEnd   *time.Time
	ClosedAt    *time.Time
	// Progress 非 nil 时写入项目进度（结项强制 100；任务汇总由仓储事务维护）。
	Progress *int
}

// Repository 项目仓储（数据访问层；仅 ent，不承载业务规则）。
type Repository interface {
	Create(ctx context.Context, p CreateParams) (*ent.Project, error)
	Get(ctx context.Context, tenantID, id int) (*ent.Project, error)
	List(ctx context.Context, f ListFilter) ([]*ent.Project, int64, error)
	// Update 带可选乐观锁（version>0 时校验版本；冲突/不存在返回 ent.IsNotFound）。
	Update(ctx context.Context, tenantID, id int, p UpdateParams, version int) (*ent.Project, error)
	// StatusCAS 状态流转 CAS（from 状态 + 可选版本双重校验；失败返回 ent.IsNotFound 由服务层区分冲突/不存在）。
	StatusCAS(ctx context.Context, tenantID, id int, from, to string, version int, patch StatusPatch) (*ent.Project, error)
	SoftDelete(ctx context.Context, tenantID, id int) error
	CodeExists(ctx context.Context, tenantID int, code string) (bool, error)
	// CountOpenTasks 未终结任务数（结项门禁）。
	CountOpenTasks(ctx context.Context, tenantID, projectID int) (int, error)
	// SumTaskHours 任务工时汇总（PE-22；estimate_hours/actual_hours 全量求和）。
	SumTaskHours(ctx context.Context, tenantID, projectID int) (*TaskHoursSummary, error)
	// NonTerminalRecords 未终结关联单据（结项门禁；按域终态集合排除）。
	NonTerminalRecords(ctx context.Context, tenantID, projectID int) ([]Blocker, error)
	// HasLinks 是否存在关联（任务或五类单据）——删除门禁。
	HasLinks(ctx context.Context, tenantID, projectID int) (bool, error)

	// ---- 成员与可见性（PE-3）----

	// ListMembers 项目成员（全部状态，按加入时间）。
	ListMembers(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectMember, error)
	// AddMember 添加成员（存在 removed 行时复活该行，避免行膨胀）。
	AddMember(ctx context.Context, tenantID, projectID, userID int, role string, createdBy int) (*ent.ProjectMember, error)
	// UpdateMemberRole 调整存活成员角色。
	UpdateMemberRole(ctx context.Context, tenantID, projectID, userID int, role string) (*ent.ProjectMember, error)
	// RemoveMember 软失效成员（status=removed）。
	RemoveMember(ctx context.Context, tenantID, projectID, userID int) error
	// IsActiveMember 是否为该项目存活成员（可见性谓词用）。
	IsActiveMember(ctx context.Context, tenantID, projectID, userID int) (bool, error)
	// MemberProjectIDs 该用户在租户内以成员身份参与的项目 ID（列表可见性收窄用）。
	MemberProjectIDs(ctx context.Context, tenantID, userID int) ([]int, error)
	// ActiveMembership 目标租户内该用户的存活且未过期 membership（成员前置校验/标签派生用）。
	ActiveMembership(ctx context.Context, tenantID, userID int) (*ent.UserTenantMembership, error)
	// MembershipsByUserIDs 批量取 membership（ListMembers 派生 memberOrigin/expiresAt）。
	MembershipsByUserIDs(ctx context.Context, tenantID int, userIDs []int) ([]*ent.UserTenantMembership, error)
	// ActiveMemberships 目标租户存活且未过期的 membership（候选查询；limit≤200）。
	ActiveMemberships(ctx context.Context, tenantID, limit int) ([]*ent.UserTenantMembership, error)
	// UsersByIDs 批量取用户（展示字段；不按 home tenant 过滤——provider 投影用户归属他租户）。
	UsersByIDs(ctx context.Context, userIDs []int) ([]*ent.User, error)
	// DepartmentsByIDs 批量取部门（展示字段；同租户、未软删）。
	DepartmentsByIDs(ctx context.Context, tenantID int, departmentIDs []int) ([]*ent.Department, error)

	// ---- 引用校验（PE-28）----

	// ActiveDepartment 部门引用（同租户、未软删；不存在返回 ent.IsNotFound）。
	ActiveDepartment(ctx context.Context, tenantID, departmentID int) (*ent.Department, error)

	// ---- 任务（PE-4）----

	ListTasks(ctx context.Context, tenantID, projectID int, status string, assigneeID int) ([]*ent.ProjectTask, error)
	GetTask(ctx context.Context, tenantID, projectID, taskID int) (*ent.ProjectTask, error)
	// GetTaskByID 按 ID 取任务（跨项目——深度校验沿 parent 链回溯时用，仍限定租户）。
	GetTaskByID(ctx context.Context, tenantID, taskID int) (*ent.ProjectTask, error)
	CreateTask(ctx context.Context, p TaskCreateParams) (*ent.ProjectTask, error)
	// UpdateTaskCAS version>0 时校验乐观锁（冲突返回 ent.IsNotFound）。
	UpdateTaskCAS(ctx context.Context, tenantID, projectID, taskID int, p TaskUpdateParams, version int) (*ent.ProjectTask, error)
	// TaskStatusCAS 任务状态流转 CAS（from + 可选版本）。
	TaskStatusCAS(ctx context.Context, tenantID, projectID, taskID int, from, to string, version int, patch TaskStatusPatch) (*ent.ProjectTask, error)
	SoftDeleteTask(ctx context.Context, tenantID, projectID, taskID int) error
	// CountTaskChildren 子任务数（删除门禁）。
	CountTaskChildren(ctx context.Context, tenantID, projectID, parentTaskID int) (int, error)
	// ListTasksByAssignee 个人任务聚合（projectIDs 为可见项目收窄；nil=不限）。
	ListTasksByAssignee(ctx context.Context, tenantID, userID int, projectIDs []int, status string, limit int) ([]*ent.ProjectTask, error)

	// ---- 单据关联（PE-5）----

	// AuthorizeProjectWrite 项目写权限动态校验（管理角色直通；其余查 DB 权限）。
	AuthorizeProjectWrite(ctx context.Context, roles []string, tenantID int) bool
	// AttachRecord 挂接（幂等；跨租户/不存在返回 ent.IsNotFound）。
	AttachRecord(ctx context.Context, tenantID int, domain string, recordID, projectID int) error
	// DetachRecord 解除挂接（幂等：未挂接也成功）。
	DetachRecord(ctx context.Context, tenantID int, domain string, recordID int) error
	// ListRecords 聚合读（domain 为空 = 五域合并；offset/limit 作用于合并结果）。
	ListRecords(ctx context.Context, tenantID, projectID int, domain, status string, offset, limit int) ([]RecordItem, int64, error)
	// GetRecordProject 反向查询：单据当前挂接的 project_id（recordFound=false 表示单据不存在/跨租户；未挂接返回 0）。
	GetRecordProject(ctx context.Context, tenantID int, domain string, recordID int) (projectID int, recordFound bool, err error)
	// SetTaskLinkedTicket 回写任务主工单（同租户/同项目校验）。
	SetTaskLinkedTicket(ctx context.Context, tenantID, projectID, taskID, ticketID int) error
	// ClearTaskLinkedTicket 解绑任务主工单。
	ClearTaskLinkedTicket(ctx context.Context, tenantID, projectID, taskID int) error

	// ---- P1：里程碑与任务依赖（PE-14）----

	ListMilestones(ctx context.Context, tenantID, projectID int, status string) ([]*ent.ProjectMilestone, error)
	// ListMilestonesByOwner 个人里程碑到期聚合（跨项目；owner=当前用户；projectIDs 为可见项目收窄，nil=不限；
	// dueBefore 非零时只取该时间前到期的行；due_date 升序）。
	ListMilestonesByOwner(ctx context.Context, tenantID, ownerID int, projectIDs []int, status string, dueBefore *time.Time, limit int) ([]*ent.ProjectMilestone, error)
	GetMilestone(ctx context.Context, tenantID, projectID, milestoneID int) (*ent.ProjectMilestone, error)
	CreateMilestone(ctx context.Context, p MilestoneCreateParams) (*ent.ProjectMilestone, error)
	UpdateMilestone(ctx context.Context, tenantID, projectID, milestoneID int, p MilestoneUpdateParams) (*ent.ProjectMilestone, error)
	// MilestoneStatusCAS 状态流转 CAS（from 状态守卫；reached 时回填 reached_at）。
	MilestoneStatusCAS(ctx context.Context, tenantID, projectID, milestoneID int, from, to string, reachedAt *time.Time) (*ent.ProjectMilestone, error)
	DeleteMilestone(ctx context.Context, tenantID, projectID, milestoneID int) error

	ListDependenciesByTaskIDs(ctx context.Context, tenantID int, taskIDs []int) ([]*ent.ProjectTaskDependency, error)
	CreateDependency(ctx context.Context, tenantID, taskID, dependsOnTaskID int, depType string) (*ent.ProjectTaskDependency, error)
	DeleteDependency(ctx context.Context, tenantID, taskID, dependsOnTaskID int) error
	DependencyExists(ctx context.Context, tenantID, taskID, dependsOnTaskID int) (bool, error)

	// ---- P1：计划基线（PE-13）----

	// NextBaselineVersion 项目下一基线版本（无基线时返回 1）。
	NextBaselineVersion(ctx context.Context, tenantID, projectID int) (int, error)
	// CreateBaseline 插入基线（不可变；版本唯一索引冲突返回 ent.IsConstraintError）。
	CreateBaseline(ctx context.Context, p BaselineCreateParams) (*ent.ProjectBaseline, error)
	// ListBaselines 项目基线（version 倒序）。
	ListBaselines(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectBaseline, error)
	// LatestBaseline 最新基线（无基线返回 ent.NotFound）。
	LatestBaseline(ctx context.Context, tenantID, projectID int) (*ent.ProjectBaseline, error)

	// ---- P1：项目模板（PE-11）----

	// ListTemplates 模板列表（分页 + status/type/q 过滤）。
	ListTemplates(ctx context.Context, f TemplateListFilter) ([]*ent.ProjectTemplate, int64, error)
	// GetTemplate 按租户取模板（软删不可见）。
	GetTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectTemplate, error)
	CreateTemplate(ctx context.Context, p TemplateCreateParams) (*ent.ProjectTemplate, error)
	// UpdateTemplateCAS version>0 时校验乐观锁（冲突/不存在返回 ent.IsNotFound）。
	UpdateTemplateCAS(ctx context.Context, tenantID, id int, p TemplateUpdateParams, version int) (*ent.ProjectTemplate, error)
	SoftDeleteTemplate(ctx context.Context, tenantID, id int) error
	// TemplateNameExists 租户内名称占用（excludeID>0 时排除自身）。
	TemplateNameExists(ctx context.Context, tenantID int, name string, excludeID int) (bool, error)
	// TemplateAppliedProjectCount 模板被应用的项目数（删除门禁）。
	TemplateAppliedProjectCount(ctx context.Context, tenantID, templateID int) (int, error)
	// ApplyTemplateTx 事务内：CAS 认领 projects.template_id（幂等闸门）+ 生成阶段/任务/检查项/里程碑。
	// 认领失败（已被应用）返回 ErrTemplateAlreadyApplied；任何生成失败整体回滚。
	ApplyTemplateTx(ctx context.Context, p TemplateApplyParams) (*TemplateApplyOutcome, error)

	// --- PE-12 阶段门 ---

	// ListGates 项目阶段门列表（gate 升序）。
	ListGates(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectGate, error)

	// ---- P2 组合视图与度量（PE-24）----

	// ListForAggregate 组合视图取数（与 List 同谓词，不分页，id 降序）。
	ListForAggregate(ctx context.Context, f ListFilter, limit int) ([]*ent.Project, error)
	// ListMilestonesByProjects 批量里程碑（跨项目；空集合不发查询）。
	ListMilestonesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectMilestone, error)
	// ListGatesByProjects 批量阶段门（跨项目）。
	ListGatesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectGate, error)
	// ListTasksByProjects 批量任务（跨项目；未删，口径同 ListTasks）。
	ListTasksByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectTask, error)
	// ListMembersByProjects 批量成员（跨项目；含已移除行，口径同 ListMembers）。
	ListMembersByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectMember, error)
	// ListRisksByProjects 批量风险/问题（跨项目；未软删，口径同 ListRisks 无过滤视图）——PE-27 健康度事实源。
	ListRisksByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectRisk, error)
	// ListBaselinesByProjects 批量基线（跨项目；version 升序，取每项目最新）——PE-27 健康度事实源。
	ListBaselinesByProjects(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.ProjectBaseline, error)

	// GetGate 取单门（不存在返回 ent.IsNotFound）。
	GetGate(ctx context.Context, tenantID, projectID int, gate string) (*ent.ProjectGate, error)

	// GetGateByID 按ID取门（租户内）。
	GetGateByID(ctx context.Context, tenantID, id int) (*ent.ProjectGate, error)

	// CreateGate 创建门记录（唯一索引冲突由调用方处理）。
	CreateGate(ctx context.Context, p GateCreateParams) (*ent.ProjectGate, error)

	// GateSubmitCAS 提交/重提交 CAS（status + version）。
	GateSubmitCAS(ctx context.Context, tenantID, id int, fromStatus string, version int, patch GateSubmitPatch, toStatus string) (*ent.ProjectGate, error)

	// GateDecisionCAS 裁决 CAS（status + version）。
	GateDecisionCAS(ctx context.Context, tenantID, id int, fromStatus string, version int, patch GateDecisionPatch, toStatus string) (*ent.ProjectGate, error)

	// --- PE-15 结项清单 ---

	// GetClosure 取结项清单（不存在返回 ent.IsNotFound）。
	GetClosure(ctx context.Context, tenantID, projectID int) (*ent.ProjectClosure, error)

	// CreateClosure 创建结项清单（一项目一行；唯一索引冲突由调用方回读处理）。
	CreateClosure(ctx context.Context, tenantID, projectID, createdBy int) (*ent.ProjectClosure, error)

	// UpdateClosureCAS version 乐观锁更新（冲突/不存在返回 CASConflictError）。
	UpdateClosureCAS(ctx context.Context, tenantID, id, version int, p ClosurePatch) (*ent.ProjectClosure, error)

	// RecycleProjectMembers 成员回收（member 角色活跃成员置 removed），返回回收数量。
	RecycleProjectMembers(ctx context.Context, tenantID, projectID, managerID int) (int, error)

	// --- PE-17 归口与报告 ---

	// GetTeamRef 归口团队引用（含软删标记；同租户；不存在返回 ent.IsNotFound）。
	GetTeamRef(ctx context.Context, tenantID, teamID int) (*TeamRef, error)

	// ListTeamRefs 批量归口团队引用（列表/分组富化用；缺失 ID 不出现在 map）。
	ListTeamRefs(ctx context.Context, tenantID int, teamIDs []int) (map[int]TeamRef, error)

	// ListReportTemplates 报告模板列表 + 总数（软删不可见；updatedAt 倒序）。
	ListReportTemplates(ctx context.Context, f ReportTemplateListFilter) ([]*ent.ProjectReportTemplate, int64, error)

	// GetReportTemplate 取报告模板（软删不可见）。
	GetReportTemplate(ctx context.Context, tenantID, id int) (*ent.ProjectReportTemplate, error)

	// CreateReportTemplate 创建报告模板（唯一索引冲突由调用方映射）。
	CreateReportTemplate(ctx context.Context, p ReportTemplateCreateParams) (*ent.ProjectReportTemplate, error)

	// UpdateReportTemplateCAS 更新报告模板（version>0 校验乐观锁；未命中返回 ent.NotFound）。
	UpdateReportTemplateCAS(ctx context.Context, tenantID, id int, p ReportTemplateUpdateParams, version int) (*ent.ProjectReportTemplate, error)

	// SoftDeleteReportTemplate 软删除报告模板（软删不释放名称唯一性）。
	SoftDeleteReportTemplate(ctx context.Context, tenantID, id int) error

	// ReportTemplateNameExists 报告模板名占用（excludeID>0 时排除自身）。
	ReportTemplateNameExists(ctx context.Context, tenantID int, name string, excludeID int) (bool, error)

	// ListProjectsForReportGroups 报表分组取数：轻量列（id/owner_team_id/状态/进度/日期/预算），
	// 健康度由服务层派生（PE-27 D-58）；projectIDs 收窄，nil=不限。
	ListProjectsForReportGroups(ctx context.Context, tenantID int, projectIDs []int) ([]*ent.Project, error)

	// --- PE-21 风险/问题台账 ---

	// ListRisks 项目风险/问题（kind/status/level 过滤；未软删；updatedAt 倒序、ID 倒序）。
	ListRisks(ctx context.Context, f RiskListFilter) ([]*ent.ProjectRisk, error)

	// GetRisk 项目内单条风险（未软删；不存在返回 ent.IsNotFound）。
	GetRisk(ctx context.Context, tenantID, projectID, riskID int) (*ent.ProjectRisk, error)

	// CreateRisk 创建风险。
	CreateRisk(ctx context.Context, p RiskCreateParams) (*ent.ProjectRisk, error)

	// UpdateRiskCAS 更新风险字段（version>0 时校验乐观锁；未命中返回 ent.IsNotFound）。
	UpdateRiskCAS(ctx context.Context, tenantID, projectID, riskID int, p RiskUpdateParams, version int) (*ent.ProjectRisk, error)

	// RiskStatusCAS 状态流转 CAS（from 状态 + 可选版本守卫；close 携带 resolution）。
	RiskStatusCAS(ctx context.Context, tenantID, projectID, riskID int, from, to, resolution string, version int) (*ent.ProjectRisk, error)

	// LinkRiskConversion 回写转换产物（target=problem|change；仅当目标字段为空时命中 = 防重复转换）。
	LinkRiskConversion(ctx context.Context, tenantID, projectID, riskID int, target string, linkedID int) (*ent.ProjectRisk, error)

	// SoftDeleteRisk 软删（已转换记录仍可删，关联实体不受影响）。
	SoftDeleteRisk(ctx context.Context, tenantID, projectID, riskID int) error

	// TicketExists 同租户工单存在性（风险 related_ticket_id 弱引用校验）。
	TicketExists(ctx context.Context, tenantID, ticketID int) (bool, error)

	// --- PE-23 资源负荷（周容量配置）---

	// SetProjectDefaultCapacity 设置/清空项目周容量默认（hours=nil → NULL=内置 40h；不改 version）。
	SetProjectDefaultCapacity(ctx context.Context, tenantID, projectID int, hours *float64) error

	// SetMemberCapacity 设置/清空成员周容量覆盖（hours=nil → NULL=继承项目默认；仅存活成员行）。
	SetMemberCapacity(ctx context.Context, tenantID, projectID, userID int, hours *float64) error

	// --- PE-26 采购/资产结项登记 ---

	// ListAssetLinks 项目登记列表（未删行；ID 倒序；limit 为硬上限 +1 截断探测口径）。
	ListAssetLinks(ctx context.Context, tenantID, projectID, limit int) ([]*ent.ProjectAssetLink, error)

	// GetAssetLink 取单条登记（未删行；不存在返回 ent.IsNotFound）。
	GetAssetLink(ctx context.Context, tenantID, projectID, id int) (*ent.ProjectAssetLink, error)

	// CreateAssetLink 新增登记（重复由服务层查重；唯一索引冲突兜底）。
	CreateAssetLink(ctx context.Context, tenantID, projectID, actorID int, linkType string, targetID int, note string) (*ent.ProjectAssetLink, error)

	// SoftDeleteAssetLink 软删未删行（返回影响行数；0 = 不存在/已删）。
	SoftDeleteAssetLink(ctx context.Context, tenantID, projectID, id, actorID int, now time.Time) (int, error)

	// FindActiveAssetLink 查未删行（同项目同类型同目标；重复登记 409 判定）。
	FindActiveAssetLink(ctx context.Context, tenantID, projectID int, linkType string, targetID int) (*ent.ProjectAssetLink, error)

	// AssetLinkTargetExists 目标存在性（同租户弱引用校验，D-50）。
	AssetLinkTargetExists(ctx context.Context, tenantID int, linkType string, targetID int) (bool, error)

	// AssetLinkTargetInfos 批量富化目标标签（同租户；缺失 ID 不出现在 map）。
	AssetLinkTargetInfos(ctx context.Context, tenantID int, linkType string, ids []int) (map[int]AssetLinkTargetInfo, error)

	// SearchAssetLinkTargets 候选查询（同租户 + 关键词 + limit+1 截断探测，D-52）。
	SearchAssetLinkTargets(ctx context.Context, tenantID int, linkType, keyword string, limit int) ([]AssetLinkCandidate, bool, error)
}

// TemplateListFilter 模板列表过滤（PE-11）。
type TemplateListFilter struct {
	TenantID int
	Status   string
	Type     string
	Keyword  string
	Offset   int
	Limit    int
}

// TemplateCreateParams 模板创建参数。
type TemplateCreateParams struct {
	TenantID    int
	Name        string
	Description string
	Type        string
	Status      string
	Payload     map[string]any
	CreatedBy   int
}

// TemplateUpdateParams 模板更新参数（nil = 不修改）。
type TemplateUpdateParams struct {
	Name        *string
	Description *string
	Type        *string
	Status      *string
	Payload     map[string]any // nil = 不修改
}

// TeamRef 归口团队引用（PE-17；Deleted=软删，读取侧降级提示，写入侧拒绝）。
type TeamRef struct {
	ID      int
	Name    string
	Status  string
	Deleted bool
}

// ReportTemplateListFilter 报告模板列表过滤（PE-17）。
type ReportTemplateListFilter struct {
	TenantID int
	Status   string
	Keyword  string
	Offset   int
	Limit    int
}

// ReportTemplateCreateParams 报告模板创建参数。
type ReportTemplateCreateParams struct {
	TenantID    int
	Name        string
	Description string
	Cadence     string
	Sections    []string
	Status      string
	CreatedBy   int
}

// ReportTemplateUpdateParams 报告模板更新参数（nil = 不修改）。
type ReportTemplateUpdateParams struct {
	Name        *string
	Description *string
	Cadence     *string
	Sections    *[]string
	Status      *string
}

// ReportGroupAggregate 报表分组聚合行（归口团队 × 健康度分布）。
type ReportGroupAggregate struct {
	OwnerTeamID  int
	ProjectCount int
	GreenCount   int
	YellowCount  int
	RedCount     int
}

// RiskListFilter 风险列表过滤（PE-21）。
type RiskListFilter struct {
	TenantID  int
	ProjectID int
	Kind      string
	Status    string
	Level     string
}

// RiskCreateParams 风险创建参数（PE-21）。
type RiskCreateParams struct {
	TenantID        int
	ProjectID       int
	Kind            string
	Title           string
	Description     string
	Level           string
	Probability     string
	Impact          string
	OwnerID         int
	RelatedTicketID int
	CreatedBy       int
}

// RiskUpdateParams 风险更新参数（nil = 不修改；OwnerID/RelatedTicketID 传 0 清空）。
type RiskUpdateParams struct {
	Kind            *string
	Title           *string
	Description     *string
	Level           *string
	Probability     *string
	Impact          *string
	OwnerID         *int
	RelatedTicketID *int
}

// TemplateApplyParams 模板应用参数（单事务；Anchor 为计划日期锚点 = 项目开始日期）。
type TemplateApplyParams struct {
	TenantID   int
	ProjectID  int
	TemplateID int
	AppliedBy  int
	AppliedAt  time.Time
	Stages     []TemplateStagePlan
	Milestones []TemplateMilestonePlan
}

// TemplateStagePlan 生成计划：阶段（L1 分组任务；Name 为空 = 未分组任务直接挂项目根）。
type TemplateStagePlan struct {
	Name  string
	Tasks []TemplateTaskPlan
}

// TemplateTaskPlan 生成计划：任务（L2；Checklist → L3 检查项子任务）。
type TemplateTaskPlan struct {
	Name          string
	Description   string
	Priority      string
	PlanStart     *time.Time
	PlanEnd       *time.Time
	EstimateHours *float64
	Checklist     []string
}

// TemplateMilestonePlan 生成计划：里程碑（DueDate 已按锚点换算）。
type TemplateMilestonePlan struct {
	Name    string
	DueDate time.Time
}

// TemplateApplyOutcome 生成计数（响应回显）。
type TemplateApplyOutcome struct {
	StageCount     int
	TaskCount      int
	ChecklistCount int
	MilestoneCount int
}

// BaselineCreateParams 基线创建参数（不可变快照）。
type BaselineCreateParams struct {
	TenantID  int
	ProjectID int
	Version   int
	Snapshot  map[string]any
	Reason    string
	CreatedBy int
}

// MilestoneCreateParams 里程碑创建参数。
type MilestoneCreateParams struct {
	TenantID    int
	ProjectID   int
	Name        string
	Description string
	DueDate     time.Time
	OwnerID     int
}

// MilestoneUpdateParams 里程碑更新参数（nil = 不修改）。
type MilestoneUpdateParams struct {
	Name        *string
	Description *string
	DueDate     *time.Time
	OwnerID     *int
}

// TaskCreateParams 任务创建参数。
type TaskCreateParams struct {
	TenantID      int
	ProjectID     int
	ParentTaskID  int
	Name          string
	Description   string
	AssigneeID    int
	Priority      string
	PlanStart     *time.Time
	PlanEnd       *time.Time
	EstimateHours *float64
	SortOrder     int
	CreatedBy     int
}

// TaskUpdateParams 任务更新参数（指针 nil = 不修改）。
type TaskUpdateParams struct {
	Name          *string
	Description   *string
	AssigneeID    *int
	Priority      *string
	PlanStart     *time.Time
	PlanEnd       *time.Time
	ActualStart   *time.Time
	ActualEnd     *time.Time
	Progress      *int
	EstimateHours *float64
	ActualHours   *float64
	SortOrder     *int
}

// TaskStatusPatch 任务状态流转附带写字段。
type TaskStatusPatch struct {
	ActualStart *time.Time
	ActualEnd   *time.Time
	Progress    *int
}
