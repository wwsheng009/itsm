// Package projectlink 提供「单据创建时挂接项目」的共享校验端口。
//
// 背景（PE-5 增补，2026-10-18）：五类单据（工单/事件/问题/变更/服务请求）存量已有
// 「加入/移出项目」端点（handlers/project 的 attach/detach），但创建时至多只能先建单再挂接；
// 本包把创建前置校验（项目存在、非终态、project:write 动态复核）收敛为单一端口：
//   - 端口由 handlers/project.Service 实现（ValidateAttach）；
//   - bootstrap 按域注入各域 handler（SetProjectLink）；
//   - 各域 handler 在创建请求携带 projectId 时调用 GuardCreate，校验通过后再落库
//     （project_id 随 INSERT 一次写入，避免「先建单后挂接」的部分成功）。
//
// 未装配端口（注入缺失）时携带 projectId 的创建请求 fail-closed（503），
// 不静默丢弃关联意图。
package projectlink

import (
	"context"

	"itsm-backend/common"
)

// Validator 目标项目校验端口（由项目管理服务实现；调用方仅依赖此窄接口）。
type Validator interface {
	// ValidateAttach 校验目标项目可挂接：存在（租户内）、非终态（completed/cancelled）、
	// 且调用者具备 project:write（管理角色直通）。错误按 common 语义返回（403/404/400）。
	ValidateAttach(ctx context.Context, tenantID, projectID int, roles []string) error
}

// GuardCreate 创建前置校验：projectID 为空（nil 或 <=0）时直通；
// 携带 projectId 但端口未装配时返回 503（fail-closed，不创建「看似关联实则丢弃」的单据）。
func GuardCreate(ctx context.Context, v Validator, tenantID int, projectID *int, roles []string) error {
	if projectID == nil || *projectID <= 0 {
		return nil
	}
	if v == nil {
		return common.NewBusinessError(common.ServiceUnavailableCode,
			"项目管理服务未装配，暂不支持创建时挂接项目", "")
	}
	return v.ValidateAttach(ctx, tenantID, *projectID, roles)
}
