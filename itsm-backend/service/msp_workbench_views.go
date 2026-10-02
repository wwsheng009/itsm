// msp_workbench_views.go：工作台自定义视图（IP-P2-4a）——保存的过滤器组合，支持同 provider 分享。
//
// 设计约束：
//  1. 作用域：tenant_id = provider 租户（视图归属域）；跨 provider 一律不可见（查询条件强制 tenant 匹配）。
//  2. 可见性：owner 全部可见可写；is_shared=true 对同 provider 全体可见（他人只读）。
//  3. 过滤器一律服务端校验：customerTenantIds ⊆ 调用方可访问集合（MSPContext.AllowedCustomers），
//     不接受客户端以视图作为授权依据（视图仅是筛选保存）。
//  4. 灰度：WORKBENCH_VIEWS_ENABLED=true 开启（默认关，可回退）；SetViewsEnabled 供测试/按租户灰度。
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/workbenchview"

	"go.uber.org/zap"
)

// 视图错误码（handler 按 Code 映射 HTTP 语义）。
const (
	CodeWorkbenchViewsDisabled = "WORKBENCH_VIEWS_DISABLED"
	CodeWorkbenchViewNotFound  = "WORKBENCH_VIEW_NOT_FOUND"
	CodeWorkbenchViewOwnerOnly = "WORKBENCH_VIEW_OWNER_ONLY"
	CodeWorkbenchViewNameDup   = "WORKBENCH_VIEW_NAME_CONFLICT"
	CodeWorkbenchViewInvalid   = "WORKBENCH_VIEW_INVALID"
)

// WorkbenchViewError 视图领域错误（含建议 HTTP 状态码）。
type WorkbenchViewError struct {
	Code    string
	Message string
	Status  int
}

func (e *WorkbenchViewError) Error() string { return e.Message }

func newViewError(code, msg string, status int) *WorkbenchViewError {
	return &WorkbenchViewError{Code: code, Message: msg, Status: status}
}

// AsWorkbenchViewError 判定并提取视图领域错误。
func AsWorkbenchViewError(err error) (*WorkbenchViewError, bool) {
	var ve *WorkbenchViewError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// MSPWorkbenchViewService 工作台自定义视图服务。
type MSPWorkbenchViewService struct {
	client       *ent.Client
	logger       *zap.SugaredLogger
	viewsEnabled bool
}

// NewMSPWorkbenchViewService 构造视图服务。
// 灰度开关：环境变量 WORKBENCH_VIEWS_ENABLED=true 时开启（默认关，可回退）。
func NewMSPWorkbenchViewService(client *ent.Client, logger *zap.SugaredLogger) *MSPWorkbenchViewService {
	return &MSPWorkbenchViewService{
		client:       client,
		logger:       logger,
		viewsEnabled: strings.EqualFold(os.Getenv("WORKBENCH_VIEWS_ENABLED"), "true"),
	}
}

// SetViewsEnabled 显式设置灰度开关（测试与按租户灰度使用）。
func (s *MSPWorkbenchViewService) SetViewsEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.viewsEnabled = enabled
}

// ViewsEnabled 当前灰度状态（供 handler/前端介绍接口）。
func (s *MSPWorkbenchViewService) ViewsEnabled() bool {
	return s != nil && s.viewsEnabled
}

func (s *MSPWorkbenchViewService) ensureReady(actor MSPWorkbenchActor) error {
	if !s.viewsEnabled {
		return newViewError(CodeWorkbenchViewsDisabled, "自定义视图未开启（WORKBENCH_VIEWS_ENABLED）", http.StatusNotFound)
	}
	if s.client == nil {
		return fmt.Errorf("ent client not available for workbench views")
	}
	if actor.UserID <= 0 || actor.HomeTenantID <= 0 {
		return newViewError(CodeWorkbenchViewOwnerOnly, "缺少 MSP 身份上下文", http.StatusForbidden)
	}
	return nil
}

// ---------- 过滤器规范化 ----------

const (
	workbenchViewNameMax = 60
	workbenchViewQMax    = 100
	workbenchViewCodeMax = 40
)

// normalizeViewFilter 规范化并校验过滤器（customerTenantIds ⊆ 可访问集合）。
func normalizeViewFilter(actor MSPWorkbenchActor, in dto.WorkbenchViewFilter) (dto.WorkbenchViewFilter, error) {
	out := dto.WorkbenchViewFilter{CustomerTenantIDs: []int{}}
	if len(in.CustomerTenantIDs) > workbenchTenantMax {
		return out, newViewError(CodeWorkbenchViewInvalid, fmt.Sprintf("客户数量超过上限 %d", workbenchTenantMax), http.StatusBadRequest)
	}
	allowed := make(map[int]bool, len(actor.AllowedCustomers))
	for _, id := range actor.AllowedCustomers {
		allowed[id] = true
	}
	seen := make(map[int]bool, len(in.CustomerTenantIDs))
	for _, id := range in.CustomerTenantIDs {
		if id <= 0 || !allowed[id] {
			return out, newViewError(CodeWorkbenchViewInvalid, fmt.Sprintf("客户租户 %d 不在可访问集合内", id), http.StatusBadRequest)
		}
		if !seen[id] {
			seen[id] = true
			out.CustomerTenantIDs = append(out.CustomerTenantIDs, id)
		}
	}
	out.Status = strings.TrimSpace(in.Status)
	if utf8.RuneCountInString(out.Status) > workbenchViewCodeMax {
		return out, newViewError(CodeWorkbenchViewInvalid, "status 过长", http.StatusBadRequest)
	}
	out.Priority = strings.TrimSpace(in.Priority)
	if utf8.RuneCountInString(out.Priority) > workbenchViewCodeMax {
		return out, newViewError(CodeWorkbenchViewInvalid, "priority 过长", http.StatusBadRequest)
	}
	out.Q = strings.TrimSpace(in.Q)
	if utf8.RuneCountInString(out.Q) > workbenchViewQMax {
		return out, newViewError(CodeWorkbenchViewInvalid, "q 过长", http.StatusBadRequest)
	}
	if in.AssigneeID < 0 {
		return out, newViewError(CodeWorkbenchViewInvalid, "assigneeId 非法", http.StatusBadRequest)
	}
	out.AssigneeID = in.AssigneeID
	out.Sort = strings.TrimSpace(in.Sort)
	if out.Sort != "" && out.Sort != "updated" && out.Sort != "sla" {
		return out, newViewError(CodeWorkbenchViewInvalid, "sort 仅支持 updated/sla", http.StatusBadRequest)
	}
	return out, nil
}

// normalizeViewName 校验视图名。
func normalizeViewName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", newViewError(CodeWorkbenchViewInvalid, "视图名不能为空", http.StatusBadRequest)
	}
	if utf8.RuneCountInString(name) > workbenchViewNameMax {
		return "", newViewError(CodeWorkbenchViewInvalid, fmt.Sprintf("视图名超长（≤%d）", workbenchViewNameMax), http.StatusBadRequest)
	}
	return name, nil
}

// viewFilterToMap 规范过滤器 → 存储 JSON。
func viewFilterToMap(f dto.WorkbenchViewFilter) map[string]any {
	ids := f.CustomerTenantIDs
	if ids == nil {
		ids = []int{}
	}
	m := map[string]any{"customerTenantIds": ids}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.Priority != "" {
		m["priority"] = f.Priority
	}
	if f.AssigneeID > 0 {
		m["assigneeId"] = f.AssigneeID
	}
	if f.Q != "" {
		m["q"] = f.Q
	}
	if f.Sort != "" {
		m["sort"] = f.Sort
	}
	return m
}

// viewFilterFromMap 存储 JSON → DTO（容错解析）。
func viewFilterFromMap(m map[string]any) dto.WorkbenchViewFilter {
	f := dto.WorkbenchViewFilter{CustomerTenantIDs: []int{}}
	if m == nil {
		return f
	}
	switch v := m["customerTenantIds"].(type) {
	case []any:
		for _, x := range v {
			if n, ok := x.(float64); ok && n > 0 {
				f.CustomerTenantIDs = append(f.CustomerTenantIDs, int(n))
			}
		}
	case []int:
		for _, n := range v {
			if n > 0 {
				f.CustomerTenantIDs = append(f.CustomerTenantIDs, n)
			}
		}
	}
	f.Status, _ = m["status"].(string)
	f.Priority, _ = m["priority"].(string)
	if n, ok := m["assigneeId"].(float64); ok && n > 0 {
		f.AssigneeID = int(n)
	}
	f.Q, _ = m["q"].(string)
	f.Sort, _ = m["sort"].(string)
	return f
}

func toWorkbenchViewResponse(e *ent.WorkbenchView, actorUserID int) dto.WorkbenchViewResponse {
	return dto.WorkbenchViewResponse{
		ID:          e.ID,
		Name:        e.Name,
		Filters:     viewFilterFromMap(e.Filters),
		IsShared:    e.IsShared,
		IsDefault:   e.IsDefault,
		IsOwner:     e.OwnerUserID == actorUserID,
		OwnerUserID: e.OwnerUserID,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// ---------- 用例 ----------

// ListViews 列出当前用户可见视图（own + 同 provider 分享态）。
func (s *MSPWorkbenchViewService) ListViews(ctx context.Context, actor MSPWorkbenchActor) ([]dto.WorkbenchViewResponse, error) {
	if err := s.ensureReady(actor); err != nil {
		return nil, err
	}
	rows, err := s.client.WorkbenchView.Query().
		Where(
			workbenchview.TenantID(actor.HomeTenantID),
			workbenchview.Or(
				workbenchview.OwnerUserID(actor.UserID),
				workbenchview.IsShared(true),
			),
		).
		Order(ent.Desc(workbenchview.FieldIsDefault), ent.Desc(workbenchview.FieldUpdatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.WorkbenchViewResponse, 0, len(rows))
	for _, e := range rows {
		out = append(out, toWorkbenchViewResponse(e, actor.UserID))
	}
	return out, nil
}

// CreateView 创建视图（owner=调用方；重名冲突）。
func (s *MSPWorkbenchViewService) CreateView(ctx context.Context, actor MSPWorkbenchActor, req dto.WorkbenchViewCreateRequest) (*dto.WorkbenchViewResponse, error) {
	if err := s.ensureReady(actor); err != nil {
		return nil, err
	}
	name, err := normalizeViewName(req.Name)
	if err != nil {
		return nil, err
	}
	filters, err := normalizeViewFilter(actor, req.Filters)
	if err != nil {
		return nil, err
	}
	dup, err := s.client.WorkbenchView.Query().
		Where(
			workbenchview.TenantID(actor.HomeTenantID),
			workbenchview.OwnerUserID(actor.UserID),
			workbenchview.Name(name),
		).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, newViewError(CodeWorkbenchViewNameDup, "视图名称已存在", http.StatusConflict)
	}
	e, err := s.client.WorkbenchView.Create().
		SetTenantID(actor.HomeTenantID).
		SetOwnerUserID(actor.UserID).
		SetName(name).
		SetFilters(viewFilterToMap(filters)).
		SetIsShared(req.IsShared).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, newViewError(CodeWorkbenchViewNameDup, "视图名称已存在", http.StatusConflict)
		}
		return nil, err
	}
	resp := toWorkbenchViewResponse(e, actor.UserID)
	return &resp, nil
}

// getOwnedView 读取并校验 owner（跨 provider/非 owner 一律拒绝）。
func (s *MSPWorkbenchViewService) getOwnedView(ctx context.Context, actor MSPWorkbenchActor, id int) (*ent.WorkbenchView, error) {
	if id <= 0 {
		return nil, newViewError(CodeWorkbenchViewNotFound, "视图不存在", http.StatusNotFound)
	}
	e, err := s.client.WorkbenchView.Query().
		Where(workbenchview.ID(id), workbenchview.TenantID(actor.HomeTenantID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, newViewError(CodeWorkbenchViewNotFound, "视图不存在", http.StatusNotFound)
		}
		return nil, err
	}
	if e.OwnerUserID != actor.UserID {
		return nil, newViewError(CodeWorkbenchViewOwnerOnly, "仅创建者可修改该视图", http.StatusForbidden)
	}
	return e, nil
}

// UpdateView 更新视图（owner 本人；替换名称/过滤器/分享态）。
func (s *MSPWorkbenchViewService) UpdateView(ctx context.Context, actor MSPWorkbenchActor, id int, req dto.WorkbenchViewUpdateRequest) (*dto.WorkbenchViewResponse, error) {
	if err := s.ensureReady(actor); err != nil {
		return nil, err
	}
	e, err := s.getOwnedView(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	name, err := normalizeViewName(req.Name)
	if err != nil {
		return nil, err
	}
	filters, err := normalizeViewFilter(actor, req.Filters)
	if err != nil {
		return nil, err
	}
	if name != e.Name {
		dup, err := s.client.WorkbenchView.Query().
			Where(
				workbenchview.TenantID(actor.HomeTenantID),
				workbenchview.OwnerUserID(actor.UserID),
				workbenchview.Name(name),
				workbenchview.IDNEQ(e.ID),
			).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if dup {
			return nil, newViewError(CodeWorkbenchViewNameDup, "视图名称已存在", http.StatusConflict)
		}
	}
	updated, err := e.Update().
		SetName(name).
		SetFilters(viewFilterToMap(filters)).
		SetIsShared(req.IsShared).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, newViewError(CodeWorkbenchViewNameDup, "视图名称已存在", http.StatusConflict)
		}
		return nil, err
	}
	resp := toWorkbenchViewResponse(updated, actor.UserID)
	return &resp, nil
}

// DeleteView 删除视图（owner 本人）。
func (s *MSPWorkbenchViewService) DeleteView(ctx context.Context, actor MSPWorkbenchActor, id int) error {
	if err := s.ensureReady(actor); err != nil {
		return err
	}
	e, err := s.getOwnedView(ctx, actor, id)
	if err != nil {
		return err
	}
	return s.client.WorkbenchView.DeleteOneID(e.ID).Exec(ctx)
}

// SetDefaultView 设为当前用户默认视图（事务内先清后置，保证每 owner 至多一条默认）。
func (s *MSPWorkbenchViewService) SetDefaultView(ctx context.Context, actor MSPWorkbenchActor, id int) (*dto.WorkbenchViewResponse, error) {
	if err := s.ensureReady(actor); err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	e, err := tx.WorkbenchView.Query().
		Where(workbenchview.ID(id), workbenchview.TenantID(actor.HomeTenantID)).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, newViewError(CodeWorkbenchViewNotFound, "视图不存在", http.StatusNotFound)
		}
		return nil, err
	}
	if e.OwnerUserID != actor.UserID {
		_ = tx.Rollback()
		return nil, newViewError(CodeWorkbenchViewOwnerOnly, "仅创建者可设为默认", http.StatusForbidden)
	}
	if _, err := tx.WorkbenchView.Update().
		Where(
			workbenchview.OwnerUserID(actor.UserID),
			workbenchview.IsDefault(true),
			workbenchview.IDNEQ(e.ID),
		).
		SetIsDefault(false).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	explicit := false
	updated, err := tx.WorkbenchView.UpdateOneID(e.ID).SetIsDefault(true).Save(ctx)
	if err == nil {
		explicit = true
		err = tx.Commit()
	}
	if !explicit || err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	resp := toWorkbenchViewResponse(updated, actor.UserID)
	return &resp, nil
}
