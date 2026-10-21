package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"itsm-backend/domain/role"
	"itsm-backend/ent"
)

// 成员域哨兵错误（PE-3）。
var (
	ErrMemberExists           = errors.New("project member already exists")
	ErrMemberNotFound         = errors.New("project member not found")
	ErrMemberNotInTenant      = errors.New("user has no active membership in this tenant")
	ErrManagerCannotBeRemoved = errors.New("project manager cannot be removed from members")
	ErrInvalidProjectRole     = errors.New("invalid project role")
)

// isAdminViewer 管理角色可见全量（单一源：domain/role.IsAdminLike）。
func isAdminViewer(roles []string) bool {
	for _, r := range roles {
		if role.IsAdminLike(r) {
			return true
		}
	}
	return false
}

// GetForViewer 可见性收窄的详情：非管理角色必须是项目存活成员，否则 404（不泄露存在性）。
func (s *Service) GetForViewer(ctx context.Context, tenantID, id, viewerID int, admin bool) (*ent.Project, error) {
	p, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if admin {
		return p, nil
	}
	member, err := s.repo.IsActiveMember(ctx, tenantID, id, viewerID)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	return p, nil
}

// ListForViewer 可见性收窄的列表：非管理角色仅返回其参与的项目。
func (s *Service) ListForViewer(ctx context.Context, f ListFilter, viewerID int, admin bool) ([]*ent.Project, int64, error) {
	if !admin {
		ids, err := s.repo.MemberProjectIDs(ctx, f.TenantID, viewerID)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) == 0 {
			return []*ent.Project{}, 0, nil
		}
		f.ProjectIDs = ids
	}
	return s.List(ctx, f)
}

// ListMembers 成员列表（含失效历史；memberOrigin/expiresAt 由目标租户 membership 派生）。
func (s *Service) ListMembers(ctx context.Context, tenantID, projectID, viewerID int, admin bool) ([]MemberView, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return []MemberView{}, nil
	}
	userIDs := make([]int, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	memberships, err := s.repo.MembershipsByUserIDs(ctx, tenantID, userIDs)
	if err != nil {
		return nil, err
	}
	users, err := s.repo.UsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	return buildMemberViews(members, memberships, users), nil
}

// AddMember 添加成员：前置校验活跃 membership + 非重复；角色白名单。
func (s *Service) AddMember(ctx context.Context, tenantID, projectID, actorID int, req *MemberAddRequest) (*MemberView, error) {
	role := strings.TrimSpace(req.ProjectRole)
	if role == "" {
		role = "member"
	}
	if !validProjectRoles[role] {
		return nil, fmt.Errorf("%w: %q", ErrInvalidProjectRole, req.ProjectRole)
	}
	if _, err := s.Get(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	if _, err := s.repo.ActiveMembership(ctx, tenantID, req.UserID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMemberNotInTenant
		}
		return nil, err
	}
	exists, err := s.repo.IsActiveMember(ctx, tenantID, projectID, req.UserID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrMemberExists
	}
	member, err := s.repo.AddMember(ctx, tenantID, projectID, req.UserID, role, actorID)
	if err != nil {
		return nil, err
	}
	views, err := s.memberView(ctx, tenantID, member)
	if err != nil {
		return nil, err
	}
	return views, nil
}

// UpdateMemberRole 调整存活成员角色。
func (s *Service) UpdateMemberRole(ctx context.Context, tenantID, projectID, userID int, req *MemberRoleUpdateRequest) (*MemberView, error) {
	roleName := strings.TrimSpace(req.ProjectRole)
	if !validProjectRoles[roleName] {
		return nil, fmt.Errorf("%w: %q", ErrInvalidProjectRole, req.ProjectRole)
	}
	member, err := s.repo.UpdateMemberRole(ctx, tenantID, projectID, userID, roleName)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMemberNotFound
		}
		return nil, err
	}
	return s.memberView(ctx, tenantID, member)
}

// RemoveMember 软失效成员；项目经理（projects.manager_id）不可被移除（集成契约 §3.7）。
func (s *Service) RemoveMember(ctx context.Context, tenantID, projectID, userID int) error {
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	if current.ManagerID > 0 && current.ManagerID == userID {
		return ErrManagerCannotBeRemoved
	}
	if err := s.repo.RemoveMember(ctx, tenantID, projectID, userID); err != nil {
		if ent.IsNotFound(err) {
			return ErrMemberNotFound
		}
		return err
	}
	return nil
}

// MemberCandidates 候选成员：目标租户活跃且未过期的 membership，排除在册成员，最小字段返回。
func (s *Service) MemberCandidates(ctx context.Context, tenantID, projectID int, keyword string, limit int) ([]MemberCandidate, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	members, err := s.repo.ListMembers(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	excluded := make(map[int]struct{}, len(members))
	for _, m := range members {
		if m.Status == "active" {
			excluded[m.UserID] = struct{}{}
		}
	}
	memberships, err := s.repo.ActiveMemberships(ctx, tenantID, 200)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int, 0, len(memberships))
	for _, mb := range memberships {
		if _, skip := excluded[mb.UserID]; skip {
			continue
		}
		userIDs = append(userIDs, mb.UserID)
	}
	users, err := s.repo.UsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	nameByID := make(map[int]*ent.User, len(users))
	for _, u := range users {
		nameByID[u.ID] = u
	}

	kw := strings.ToLower(strings.TrimSpace(keyword))
	candidates := make([]MemberCandidate, 0, limit)
	for _, mb := range memberships {
		if _, skip := excluded[mb.UserID]; skip {
			continue
		}
		u := nameByID[mb.UserID]
		if u == nil {
			continue
		}
		if kw != "" {
			name := strings.ToLower(u.Name)
			username := strings.ToLower(u.Username)
			if !strings.Contains(name, kw) && !strings.Contains(username, kw) {
				continue
			}
		}
		candidates = append(candidates, MemberCandidate{
			UserID:       mb.UserID,
			Name:         u.Name,
			Username:     u.Username,
			Department:   u.Department,
			Email:        u.Email,
			MemberOrigin: memberOrigin(string(mb.Source)),
			ExpiresAt:    mb.ExpiresAt,
		})
		if len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

// memberView 单成员视图组装（批量接口复用 buildMemberViews）。
func (s *Service) memberView(ctx context.Context, tenantID int, member *ent.ProjectMember) (*MemberView, error) {
	memberships, err := s.repo.MembershipsByUserIDs(ctx, tenantID, []int{member.UserID})
	if err != nil {
		return nil, err
	}
	users, err := s.repo.UsersByIDs(ctx, []int{member.UserID})
	if err != nil {
		return nil, err
	}
	views := buildMemberViews([]*ent.ProjectMember{member}, memberships, users)
	if len(views) == 0 {
		return nil, ErrMemberNotFound
	}
	return &views[0], nil
}

// buildMemberViews 组装成员视图（缺失 membership 时降级 unknown，不阻塞展示）。
func buildMemberViews(members []*ent.ProjectMember, memberships []*ent.UserTenantMembership, users []*ent.User) []MemberView {
	originByUser := make(map[int]string, len(memberships))
	expiresByUser := make(map[int]*time.Time, len(memberships))
	for _, mb := range memberships {
		originByUser[mb.UserID] = memberOrigin(string(mb.Source))
		if mb.ExpiresAt != nil {
			t := *mb.ExpiresAt
			expiresByUser[mb.UserID] = &t
		}
	}
	userByID := make(map[int]*ent.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	views := make([]MemberView, 0, len(members))
	for _, m := range members {
		origin, ok := originByUser[m.UserID]
		if !ok {
			origin = OriginUnknown
		}
		view := MemberView{
			UserID:              m.UserID,
			ProjectRole:         m.ProjectRole,
			Status:              m.Status,
			MemberOrigin:        origin,
			ExpiresAt:           expiresByUser[m.UserID],
			JoinedAt:            m.JoinedAt,
			WeeklyCapacityHours: m.WeeklyCapacityHours,
		}
		if u := userByID[m.UserID]; u != nil {
			view.Name = u.Name
			view.Username = u.Username
		}
		views = append(views, view)
	}
	return views
}
