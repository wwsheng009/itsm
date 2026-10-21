package project

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"itsm-backend/ent"

	"go.uber.org/zap"
)

// 服务层哨兵错误（handler 统一映射 HTTP 语义）。
var (
	ErrNotFound          = errors.New("project not found")
	ErrCodeExists        = errors.New("project code already exists")
	ErrInvalidTransition = errors.New("invalid project status transition")
	ErrValidation        = errors.New("project validation failed")
	ErrReasonRequired    = errors.New("reason required")
	ErrNoUpdateFields    = errors.New("no update fields")
	// PE-28（D-60/D-61）：引用型字段写入校验。
	ErrManagerUnavailable    = errors.New("manager is not an available tenant member")
	ErrDepartmentUnavailable = errors.New("department is not available")
	// A1：创建项目的关键归属字段缺失（生产装配开启严格模式后生效）。
	ErrRequiredProjectFields = errors.New("project requires manager, department and plan dates")
)

// ClosureBlockedError 结项门禁阻塞（409 + blockers 明细）。
type ClosureBlockedError struct {
	Blockers []Blocker
}

func (e *ClosureBlockedError) Error() string {
	return fmt.Sprintf("project closure blocked by %d item(s)", len(e.Blockers))
}

// DeleteBlockedError 删除门禁阻塞（409）。
type DeleteBlockedError struct {
	Blockers []Blocker
}

func (e *DeleteBlockedError) Error() string {
	return fmt.Sprintf("project delete blocked by %d item(s)", len(e.Blockers))
}

// CASConflictError 乐观锁/状态 CAS 冲突（409）。
type CASConflictError struct {
	CurrentVersion int
}

func (e *CASConflictError) Error() string {
	return fmt.Sprintf("project version conflict (current=%d)", e.CurrentVersion)
}

// Service 项目业务服务（状态机/编号/门禁）。
type Service struct {
	repo    Repository
	now     func() time.Time
	codeGen func(now time.Time) string
	// strictCreate A1：创建项目时强制要求项目经理/所属部门/计划起止（bootstrap 生产装配开启）。
	// 关闭时保持历史兼容（导入/测试/存量脚本可仅用 name 创建），由前端先行拦截默认路径。
	strictCreate bool
	// ticketGen 工单生成端口（PE-5；bootstrap 注入，未注入时生成端点 503）。
	ticketGen TicketGenerator
	// gateApproval 阶段门审批端口（PE-12；bootstrap 注入，未注入时提交即降级自动通过）。
	gateApproval GateApprovalPort
	// closureKnowledge / closureSurvey 结项清单端口（PE-15；bootstrap 注入，未注入时动作 503）。
	closureKnowledge ClosureKnowledgePort
	closureSurvey    ClosureSurveyPort
	// riskProblem / riskChange 风险转换端口（PE-21；bootstrap 注入，未注入时转换 503）。
	riskProblem RiskProblemPort
	riskChange  RiskChangePort
	// aiPort 项目 AI 端口（PE-25；bootstrap 注入 LLM Gateway 适配器；未注入 = 降级模式）。
	aiPort ProjectAIPort
	// logger 服务日志（结项 CSAT 等尽力而为动作的失败留痕）。
	logger *zap.SugaredLogger
}

// SetStrictCreate 开启/关闭创建项目的严格必填校验（A1：前后端双层硬校验的后端一层）。
func (s *Service) SetStrictCreate(enabled bool) {
	s.strictCreate = enabled
}

// NewService 构造服务。
func NewService(repo Repository) *Service {
	return &Service{
		repo:   repo,
		now:    time.Now,
		logger: zap.NewNop().Sugar(),
		codeGen: func(t time.Time) string {
			// A6：编号日期按业务时区生成（存储仍 UTC），避免 UTC 与本地跨天导致编号日期错位。
			return fmt.Sprintf("PRJ-%s-%04d", InBusinessTime(t).Format("20060102"), rand.Intn(9000)+1000)
		},
	}
}

// SetLogger 注入日志（bootstrap；默认 Nop）。
func (s *Service) SetLogger(logger *zap.SugaredLogger) {
	if logger != nil {
		s.logger = logger
	}
}

// Create 创建项目：缺省 code 自动生成；新项目从 planning 起步（状态流转走 Transition）。
func (s *Service) Create(ctx context.Context, req *CreateRequest) (*ent.Project, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	// A1：严格模式下项目必须有归属主体与计划区间，避免产生空壳项目污染健康度/归口/报表。
	if s.strictCreate {
		if req.ManagerID == nil || *req.ManagerID <= 0 {
			return nil, fmt.Errorf("%w: managerId is required", ErrRequiredProjectFields)
		}
		if req.DepartmentID == nil || *req.DepartmentID <= 0 {
			return nil, fmt.Errorf("%w: departmentId is required", ErrRequiredProjectFields)
		}
		if req.StartDate == nil || req.EndDate == nil {
			return nil, fmt.Errorf("%w: startDate and endDate are required", ErrRequiredProjectFields)
		}
	}
	projectType := strings.TrimSpace(req.Type)
	if projectType == "" {
		projectType = "internal"
	}
	if !validTypes[projectType] {
		return nil, fmt.Errorf("%w: invalid type %q", ErrValidation, req.Type)
	}

	code := strings.TrimSpace(req.Code)
	now := s.now()
	if code == "" {
		var err error
		code, err = s.generateCode(ctx, req, name, projectType, now)
		if err != nil {
			return nil, err
		}
	} else {
		exists, err := s.repo.CodeExists(ctx, req.TenantID, code)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrCodeExists
		}
	}

	// PE-17：归口团队写入校验（同租户、未软删且 active）。
	if err := s.ensureOwnerTeam(ctx, req.TenantID, derefInt(req.OwnerTeamID)); err != nil {
		return nil, err
	}

	// PE-28：写入完整性校验（负责人 D-60 / 部门 D-61 / 日期区间 D-62）。
	if err := s.ensureManager(ctx, req.TenantID, derefInt(req.ManagerID)); err != nil {
		return nil, err
	}
	if err := s.ensureDepartment(ctx, req.TenantID, derefInt(req.DepartmentID)); err != nil {
		return nil, err
	}
	if err := validateDateRange(req.StartDate, req.EndDate); err != nil {
		return nil, err
	}

	// PE-22：轻量预算校验（成对提供；amount<=0 = 未设置）。
	budgetAmount, budgetCurrency, err := normalizeBudget(req.BudgetAmount, req.BudgetCurrency)
	if err != nil {
		return nil, err
	}
	if budgetAmount != nil && *budgetAmount <= 0 {
		budgetAmount, budgetCurrency = nil, nil
	}

	return s.repo.Create(ctx, CreateParams{
		TenantID:       req.TenantID,
		Name:           name,
		Code:           code,
		Description:    strings.TrimSpace(req.Description),
		Type:           projectType,
		Status:         StatusPlanning,
		ManagerID:      derefInt(req.ManagerID),
		DepartmentID:   derefInt(req.DepartmentID),
		OwnerTeamID:    derefInt(req.OwnerTeamID),
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		BudgetAmount:   budgetAmount,
		BudgetCurrency: budgetCurrency,
	})
}

// generateCode 自动编号（最多重试 5 次避免碰撞）。
func (s *Service) generateCode(ctx context.Context, req *CreateRequest, name, projectType string, now time.Time) (string, error) {
	// 生成基数按业务时区日期（A6），避免 UTC 与本地跨天导致编号日期与创建日不符。
	dayStart := InBusinessTime(now)
	for i := 0; i < 5; i++ {
		code := s.codeGen(dayStart)
		exists, err := s.repo.CodeExists(ctx, req.TenantID, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("%w: cannot allocate project code", ErrCodeExists)
}

// List 列表。
func (s *Service) List(ctx context.Context, f ListFilter) ([]*ent.Project, int64, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	return s.repo.List(ctx, f)
}

// Get 详情。
func (s *Service) Get(ctx context.Context, tenantID, id int) (*ent.Project, error) {
	p, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// PE-28 写入完整性校验（D-60~D-63；权威定义：
// docs/project-management/plan/p3-write-integrity-design.md）。
// ---------------------------------------------------------------------------

// ensureManager 负责人校验（D-60）：必须为本租户活跃 membership（未软删/active/未过期）。
func (s *Service) ensureManager(ctx context.Context, tenantID, userID int) error {
	if userID <= 0 {
		return nil
	}
	if _, err := s.repo.ActiveMembership(ctx, tenantID, userID); err != nil {
		if ent.IsNotFound(err) {
			return ErrManagerUnavailable
		}
		return err
	}
	return nil
}

// ensureDepartment 部门校验（D-61）：必须同租户且未软删。
func (s *Service) ensureDepartment(ctx context.Context, tenantID, departmentID int) error {
	if departmentID <= 0 {
		return nil
	}
	if _, err := s.repo.ActiveDepartment(ctx, tenantID, departmentID); err != nil {
		if ent.IsNotFound(err) {
			return ErrDepartmentUnavailable
		}
		return err
	}
	return nil
}

// validateDateRange 创建路径日期校验（D-62）：两端齐备时 endDate 不得早于 startDate。
func validateDateRange(start, end *time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	return checkDateOrder(*start, *end)
}

// validateDateRangeMerged 更新路径日期校验（D-62）：单端更新与存量合并后判定。
func validateDateRangeMerged(curStart, curEnd time.Time, reqStart, reqEnd *time.Time) error {
	start, end := curStart, curEnd
	if reqStart != nil {
		start = *reqStart
	}
	if reqEnd != nil {
		end = *reqEnd
	}
	if start.IsZero() || end.IsZero() {
		return nil
	}
	return checkDateOrder(start, end)
}

// checkDateOrder 日期区间断言（同日通过）。
func checkDateOrder(start, end time.Time) error {
	if end.Before(start) {
		return fmt.Errorf("%w: endDate must not be before startDate", ErrValidation)
	}
	return nil
}

// Update 基础字段更新（version 可选乐观锁）。
func (s *Service) Update(ctx context.Context, tenantID, id int, req *UpdateRequest) (*ent.Project, error) {
	if req.Type != nil && !validTypes[*req.Type] {
		return nil, fmt.Errorf("%w: invalid type %q", ErrValidation, *req.Type)
	}
	// PE-27（D-54）：health 移出写面（读时派生，含 healthReasons 解释），不再校验。
	if req.Progress != nil && (*req.Progress < 0 || *req.Progress > 100) {
		return nil, fmt.Errorf("%w: progress must be within 0-100", ErrValidation)
	}
	if req.Name == nil && req.Description == nil && req.Type == nil &&
		req.Progress == nil && req.ManagerID == nil && req.DepartmentID == nil && req.OwnerTeamID == nil &&
		req.StartDate == nil && req.EndDate == nil && req.BudgetAmount == nil && req.BudgetCurrency == nil {
		return nil, ErrNoUpdateFields
	}
	// PE-17：仅当显式归口到具体团队时校验（nil/0 = 不修改/清空）。
	if req.OwnerTeamID != nil && *req.OwnerTeamID > 0 {
		if err := s.ensureOwnerTeam(ctx, tenantID, *req.OwnerTeamID); err != nil {
			return nil, err
		}
	}
	// PE-28：写入完整性校验（仅当显式指向具体用户/部门；nil/0 = 不修改，optionalInt 语义）。
	if req.ManagerID != nil && *req.ManagerID > 0 {
		if err := s.ensureManager(ctx, tenantID, *req.ManagerID); err != nil {
			return nil, err
		}
	}
	if req.DepartmentID != nil && *req.DepartmentID > 0 {
		if err := s.ensureDepartment(ctx, tenantID, *req.DepartmentID); err != nil {
			return nil, err
		}
	}
	// PE-28（D-62）：日期区间校验——单端更新与存量合并后判定。
	if req.StartDate != nil || req.EndDate != nil {
		current, err := s.repo.Get(ctx, tenantID, id)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if err := validateDateRangeMerged(current.StartDate, current.EndDate, req.StartDate, req.EndDate); err != nil {
			return nil, err
		}
	}
	// PE-22：轻量预算校验（成对提供；amount<=0 / 空串 = 清空，由仓储写回 NULL）。
	budgetAmount, budgetCurrency, err := normalizeBudget(req.BudgetAmount, req.BudgetCurrency)
	if err != nil {
		return nil, err
	}

	version := derefInt(req.Version)
	updated, err := s.repo.Update(ctx, tenantID, id, UpdateParams{
		Name:           req.Name,
		Description:    req.Description,
		Type:           req.Type,
		Progress:       req.Progress,
		ManagerID:      req.ManagerID,
		DepartmentID:   req.DepartmentID,
		OwnerTeamID:    req.OwnerTeamID,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		BudgetAmount:   budgetAmount,
		BudgetCurrency: budgetCurrency,
	}, version)
	if err != nil {
		if ent.IsNotFound(err) {
			current, getErr := s.repo.Get(ctx, tenantID, id)
			if getErr != nil {
				return nil, ErrNotFound
			}
			return nil, &CASConflictError{CurrentVersion: current.Version}
		}
		return nil, err
	}
	return updated, nil
}

// Transition 状态流转（专用端点；complete 走结项门禁，cancel 需 reason）。
func (s *Service) Transition(ctx context.Context, tenantID, id int, req *StatusRequest) (*ent.Project, error) {
	action := strings.TrimSpace(req.Action)
	if _, ok := actionTransitions[action]; !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidTransition, req.Action)
	}

	current, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	from := normalizeStatus(current.Status)
	to, ok := actionTransitions[action][from]
	if !ok {
		return nil, fmt.Errorf("%w: action %q not allowed from %q", ErrInvalidTransition, action, from)
	}
	if req.Version != nil && *req.Version != current.Version {
		return nil, &CASConflictError{CurrentVersion: current.Version}
	}
	if action == ActionCancel && strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	patch := StatusPatch{}
	now := s.now()
	switch action {
	case ActionActivate:
		// PE-12 立项门：planning → active 前必须通过。
		if err := s.requireGatePassed(ctx, tenantID, id, GateInitiation); err != nil {
			return nil, err
		}
		if current.ActualStart.IsZero() {
			patch.ActualStart = &now
		}
	case ActionComplete:
		blockers, err := s.closureBlockers(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		if len(blockers) > 0 {
			return nil, &ClosureBlockedError{Blockers: blockers}
		}
		// PE-12 结项门：→ completed 前必须通过（在业务阻塞项之后判定，先暴露真实缺口）。
		if err := s.requireGatePassed(ctx, tenantID, id, GateClosure); err != nil {
			return nil, err
		}
		// PE-15 结项清单：复盘草稿/资产登记/成员回收齐备才可结项（CSAT 不阻塞）。
		if err := s.requireClosureChecklist(ctx, tenantID, id); err != nil {
			return nil, err
		}
		patch.ActualEnd = &now
		patch.ClosedAt = &now
		// 结项是项目级终态：无论任务如何分布（含无任务/全取消），对外进度都收敛为 100%。
		progress := 100
		patch.Progress = &progress
	case ActionCancel:
		if current.ActualEnd.IsZero() {
			patch.ActualEnd = &now
		}
	}

	updated, err := s.repo.StatusCAS(ctx, tenantID, id, from, to, derefInt(req.Version), patch)
	if err != nil {
		if ent.IsNotFound(err) {
			latest, getErr := s.repo.Get(ctx, tenantID, id)
			if getErr != nil {
				return nil, ErrNotFound
			}
			return nil, &CASConflictError{CurrentVersion: latest.Version}
		}
		return nil, err
	}
	if action == ActionComplete {
		// 结项成功后的 CSAT 触发（尽力而为：失败不阻塞结项，可经结项页手动重试）。
		s.autoTriggerCSAT(ctx, tenantID, id)
	}
	return updated, nil
}

// closureBlockers 结项门禁：未终结任务 + 未终结关联单据。
func (s *Service) closureBlockers(ctx context.Context, tenantID, projectID int) ([]Blocker, error) {
	blockers := make([]Blocker, 0, 8)
	openTasks, err := s.repo.CountOpenTasks(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if openTasks > 0 {
		blockers = append(blockers, Blocker{
			Type:   "task",
			Title:  fmt.Sprintf("%d 个未终结任务", openTasks),
			Status: "open",
		})
	}
	records, err := s.repo.NonTerminalRecords(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	blockers = append(blockers, records...)
	return blockers, nil
}

// Delete 软删（存在关联任务/单据时拒绝）。
func (s *Service) Delete(ctx context.Context, tenantID, id int) error {
	if _, err := s.Get(ctx, tenantID, id); err != nil {
		return err
	}
	hasLinks, err := s.repo.HasLinks(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if hasLinks {
		return &DeleteBlockedError{Blockers: []Blocker{
			{Type: "linked_records", Title: "存在关联任务或单据，需先解除关联", Status: "existing"},
		}}
	}
	if err := s.repo.SoftDelete(ctx, tenantID, id); err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
