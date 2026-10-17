package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/ent"
)

// 依赖域哨兵错误（PE-14）。
var (
	ErrDependencySelf     = errors.New("task cannot depend on itself")
	ErrDependencyExists   = errors.New("dependency already exists")
	ErrDependencyCycle    = errors.New("dependency cycle detected")
	ErrDependencyNotFound = errors.New("dependency not found")
)

// ListDependencies 项目内全部任务依赖（先取项目任务集合，再按任务集合查依赖）。
func (s *Service) ListDependencies(ctx context.Context, tenantID, projectID, viewerID int, admin bool) ([]*ent.ProjectTaskDependency, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	taskIDs := make([]int, 0, len(tasks))
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
	}
	return s.repo.ListDependenciesByTaskIDs(ctx, tenantID, taskIDs)
}

// AddDependency 添加 FS 依赖：同项目、非自依赖、无重复、无环。
func (s *Service) AddDependency(ctx context.Context, tenantID, projectID, taskID int, req *DependencyCreateRequest) (*ent.ProjectTaskDependency, error) {
	depType := strings.TrimSpace(req.DepType)
	if depType == "" {
		depType = "fs"
	}
	if depType != "fs" {
		return nil, fmt.Errorf("%w: only fs dependency is supported in v1", ErrValidation)
	}
	if req.DependsOnTaskID <= 0 {
		return nil, fmt.Errorf("%w: dependsOnTaskId is required", ErrValidation)
	}
	if req.DependsOnTaskID == taskID {
		return nil, ErrDependencySelf
	}
	// 两个任务都必须属于当前项目（跨项目/不存在统一 404 语义）。
	if _, err := s.repo.GetTask(ctx, tenantID, projectID, taskID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	if _, err := s.repo.GetTask(ctx, tenantID, projectID, req.DependsOnTaskID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	exists, err := s.repo.DependencyExists(ctx, tenantID, taskID, req.DependsOnTaskID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDependencyExists
	}
	// 环检测：在"任务 → 其前置任务"的有向图上，加入 task→dep 前要求 dep 不可达 task。
	// 仅取两个任务相关依赖不足以覆盖全链，故以全项目依赖建图。
	tasks, err := s.repo.ListTasks(ctx, tenantID, projectID, "", 0)
	if err != nil {
		return nil, err
	}
	allIDs := make([]int, 0, len(tasks))
	for _, t := range tasks {
		allIDs = append(allIDs, t.ID)
	}
	allDeps, err := s.repo.ListDependenciesByTaskIDs(ctx, tenantID, allIDs)
	if err != nil {
		return nil, err
	}
	graph := make(map[int][]int, len(allDeps)+1)
	for _, d := range allDeps {
		graph[d.TaskID] = append(graph[d.TaskID], d.DependsOnTaskID)
	}
	graph[taskID] = append(graph[taskID], req.DependsOnTaskID)
	if hasPath(graph, req.DependsOnTaskID, taskID) {
		return nil, ErrDependencyCycle
	}
	return s.repo.CreateDependency(ctx, tenantID, taskID, req.DependsOnTaskID, depType)
}

// RemoveDependency 删除依赖（幂等语义：不存在返回 404 语义错误）。
func (s *Service) RemoveDependency(ctx context.Context, tenantID, projectID, taskID, dependsOnTaskID int) error {
	if _, err := s.repo.GetTask(ctx, tenantID, projectID, taskID); err != nil {
		if ent.IsNotFound(err) {
			return ErrTaskNotFound
		}
		return err
	}
	if err := s.repo.DeleteDependency(ctx, tenantID, taskID, dependsOnTaskID); err != nil {
		if ent.IsNotFound(err) {
			return ErrDependencyNotFound
		}
		return err
	}
	return nil
}

// hasPath 判断 from 是否可达 to（深度优先，visited 防环）。
func hasPath(graph map[int][]int, from, to int) bool {
	seen := map[int]bool{}
	var dfs func(int) bool
	dfs = func(n int) bool {
		if n == to {
			return true
		}
		if seen[n] {
			return false
		}
		seen[n] = true
		for _, next := range graph[n] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	return dfs(from)
}
