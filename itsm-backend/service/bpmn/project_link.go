package bpmn

import (
	"context"
	"fmt"
	"strconv"

	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/handlers/common/projectlink"
)

// 创建时挂接项目（PE-5 遗留项收口）：
//
// HTTP 五域创建路径已统一经 handlers/common/projectlink.GuardCreate 做前置校验，
// 但 BPMN 服务任务（create_incident / create_change / create_request）此前直插 ent，
// 完全不读取 projectId，导致「流程创建的单据无法挂项目」且与 HTTP 口径不一致。
//
// 本文件把同一端口接到 BPMN 侧：
//   - 流程变量 projectId（兼容 project_id）表示挂接意图；
//   - 校验口径与 HTTP 完全一致（project:write 动态复核 + 项目存在且非终态）；
//   - 校验通过后 project_id 随 INSERT 一次写入，不做「先建单后挂接」；
//   - 未装配端口时携带 projectId 的创建任务 fail-closed（不静默丢弃关联意图）。

// ProjectIDFromVars 解析流程变量中的项目挂接意图。
// 优先 camelCase projectId（与 HTTP DTO 一致），兼容 snake_case project_id；
// 空值/非正数/无法解析一律视为「不挂接」。
func ProjectIDFromVars(variables map[string]interface{}) *int {
	if len(variables) == 0 {
		return nil
	}
	for _, key := range []string{"projectId", "project_id"} {
		v, ok := variables[key]
		if !ok {
			continue
		}
		switch val := v.(type) {
		case int:
			if val > 0 {
				id := val
				return &id
			}
		case int64:
			if val > 0 {
				id := int(val)
				return &id
			}
		case float64:
			if val > 0 {
				id := int(val)
				return &id
			}
		case string:
			id, err := strconv.Atoi(val)
			if err == nil && id > 0 {
				return &id
			}
		}
	}
	return nil
}

// GuardProjectAttach 创建前置校验：返回需要写入的 project_id（nil=不挂接）。
//
// 调用方在 ResolveTenantID 之后、Create().Save() 之前调用；校验失败时不落库，
// 避免「单据已创建但项目未挂上」的部分成功。
func GuardProjectAttach(ctx context.Context, client *ent.Client, validator projectlink.Validator, tenantID int, variables map[string]interface{}) (*int, error) {
	projectID := ProjectIDFromVars(variables)
	if projectID == nil {
		return nil, nil
	}
	roles := resolveInitiatorRoles(ctx, client, tenantID, variables)
	if err := projectlink.GuardCreate(ctx, validator, tenantID, projectID, roles); err != nil {
		return nil, err
	}
	return projectID, nil
}

// resolveInitiatorRoles 解析流程发起人的生效角色（主角色 + 同租户 m2m 附加角色），
// 与 RBACMiddleware 物化到 gin 上下文的 roles 口径一致。
//
// 优先使用受信上下文（BPMNUserIDContextKey，仅由认证中间件注入）；
// 缺失时回退到流程变量中的发起人字段——这些字段本身就是各 create_* 任务
// 写入 created_by/reporter_id/requester_id 的来源，口径保持一致。
func resolveInitiatorRoles(ctx context.Context, client *ent.Client, tenantID int, variables map[string]interface{}) []string {
	if client == nil || tenantID <= 0 {
		return nil
	}

	actorID, _ := ctx.Value(BPMNUserIDContextKey).(int)
	if actorID <= 0 {
		for _, key := range []string{"initiator_id", "created_by", "reporter_id", "requester_id"} {
			if id := GetIntFromVars(variables, key); id > 0 {
				actorID = id
				break
			}
		}
	}
	if actorID <= 0 {
		return nil
	}

	u, err := client.User.Query().
		Where(user.IDEQ(actorID), user.TenantIDEQ(tenantID)).
		WithRoles().
		Only(ctx)
	if err != nil || u == nil {
		return nil
	}

	roles := []string{string(u.Role)}
	for _, r := range u.Edges.Roles {
		if r != nil && r.IsActive && r.TenantID == tenantID {
			roles = append(roles, r.Code)
		}
	}
	return roles
}

// errProjectAttach 统一错误包装，便于日志/测试识别是挂接校验失败而非建单失败。
func errProjectAttach(err error) error {
	return fmt.Errorf("创建时挂接项目校验失败: %w", err)
}
