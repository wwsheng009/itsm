package project

import (
	"context"

	"itsm-backend/ent"
)

// 展示层字典富化（B3）：项目响应禁止裸 ID 出现在负责人/部门展示位。
//
// 设计取舍：
//   - 只做读取侧批量富化（不改变权威字段 managerId / departmentId 的契约）；
//   - 查询失败不阻塞主流程（降级为空名称，前端回退 ID），与 OwnerTeamRefs 同风格；
//   - 名称来源：user.name（跨 home tenant 取，provider 投影用户也可解析）、department.name（同租户未软删）。

// EnrichProjectDisplayNames 批量富化 ProjectResponse 的 managerName / departmentName。
func (s *Service) EnrichProjectDisplayNames(
	ctx context.Context, tenantID int, resps []*ProjectResponse,
) {
	if len(resps) == 0 {
		return
	}
	managerIDs := make([]int, 0, len(resps))
	departmentIDs := make([]int, 0, len(resps))
	seenManager := make(map[int]bool, len(resps))
	seenDepartment := make(map[int]bool, len(resps))
	for _, resp := range resps {
		if resp == nil {
			continue
		}
		if resp.ManagerID != nil && *resp.ManagerID > 0 && !seenManager[*resp.ManagerID] {
			seenManager[*resp.ManagerID] = true
			managerIDs = append(managerIDs, *resp.ManagerID)
		}
		if resp.DepartmentID != nil && *resp.DepartmentID > 0 && !seenDepartment[*resp.DepartmentID] {
			seenDepartment[*resp.DepartmentID] = true
			departmentIDs = append(departmentIDs, *resp.DepartmentID)
		}
	}

	nameByUserID := map[int]string{}
	if len(managerIDs) > 0 {
		if users, err := s.repo.UsersByIDs(ctx, managerIDs); err == nil {
			for _, u := range users {
				if u == nil {
					continue
				}
				name := u.Name
				if name == "" {
					name = u.Username
				}
				nameByUserID[u.ID] = name
			}
		} else {
			s.logger.Warnw("project display: load manager names failed",
				"tenant_id", tenantID, "error", err)
		}
	}

	nameByDepartmentID := map[int]string{}
	if len(departmentIDs) > 0 {
		if departments, err := s.repo.DepartmentsByIDs(ctx, tenantID, departmentIDs); err == nil {
			for _, d := range departments {
				if d != nil {
					nameByDepartmentID[d.ID] = d.Name
				}
			}
		} else {
			s.logger.Warnw("project display: load department names failed",
				"tenant_id", tenantID, "error", err)
		}
	}

	for _, resp := range resps {
		if resp == nil {
			continue
		}
		if resp.ManagerID != nil {
			resp.ManagerName = nameByUserID[*resp.ManagerID]
		}
		if resp.DepartmentID != nil {
			resp.DepartmentName = nameByDepartmentID[*resp.DepartmentID]
		}
	}
}

// enrichProjects 便捷入口：实体 → 响应后统一富化（handler 侧调用）。
func (s *Service) enrichProjects(ctx context.Context, tenantID int, resps []*ProjectResponse) {
	s.EnrichProjectDisplayNames(ctx, tenantID, resps)
}

// managerName 报告等非 DTO 场景的人员名称解析（失败返回空，调用方回退 ID）。
func (s *Service) managerName(ctx context.Context, userID int) string {
	if userID <= 0 {
		return ""
	}
	users, err := s.repo.UsersByIDs(ctx, []int{userID})
	if err != nil || len(users) == 0 {
		return ""
	}
	if users[0].Name != "" {
		return users[0].Name
	}
	return users[0].Username
}

// 编译期断言：仓储接口新增方法后此处保持显式引用，防止 mock 漂移时静默降级。
var _ = func(ctx context.Context, r Repository, tenantID int, ids []int) ([]*ent.Department, error) {
	return r.DepartmentsByIDs(ctx, tenantID, ids)
}
