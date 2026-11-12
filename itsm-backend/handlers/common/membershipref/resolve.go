// Package membershipref 提供业务写路径的 membership 引用解析。
//
// C 方案约定：
//   - user_id 是全局登录账号，membership_id 是 (user, tenant) 作用域内的租户身份；
//   - handler 从 RBAC 上下文取得 membership_id 并下传，作为“必须解析成功”的强信号；
//   - 直接调用 service/repository 的旧测试或后台任务没有 membership 上下文时，
//     保持迁移窗口兼容：能解析则写入，解析不到则保留 NULL，由巡检兜底。
package membershipref

import (
	"context"
	"fmt"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/user"
	"itsm-backend/middleware"
)

// ResolveActor 解析操作用户在目标租户内的 active membership。
//
// providedID > 0 表示调用方（handler）已经拿到 RBAC 解析出的 membership，
// 此时必须解析成功且与目标租户一致，否则 fail-closed。
func ResolveActor(ctx context.Context, client *ent.Client, userID, tenantID, providedID int) (int, error) {
	if client == nil || userID <= 0 || tenantID <= 0 {
		if providedID > 0 {
			return 0, common.NewBadRequestError("缺少解析租户内身份所需的账号或租户上下文", nil)
		}
		return 0, nil
	}
	membership, err := middleware.ResolveActiveMembership(ctx, client, userID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("解析租户内身份失败: %w", err)
	}
	if membership == nil {
		if providedID > 0 {
			return 0, common.NewBadRequestError(
				fmt.Sprintf("账号 %d 不是租户 %d 的有效成员", userID, tenantID), nil,
			)
		}
		return 0, nil
	}
	if providedID > 0 && membership.ID != providedID {
		return 0, common.NewForbiddenError("membership 上下文与目标租户不一致")
	}
	return membership.ID, nil
}

// ResolveOptional 解析目标用户在目标租户内的 active membership；无匹配返回 0。
func ResolveOptional(ctx context.Context, client *ent.Client, userID, tenantID int) (int, error) {
	if client == nil || userID <= 0 || tenantID <= 0 {
		return 0, nil
	}
	membership, err := middleware.ResolveActiveMembership(ctx, client, userID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("解析租户内身份失败: %w", err)
	}
	if membership == nil {
		return 0, nil
	}
	return membership.ID, nil
}

// ResolveAssignable 解析业务处理人的 membership；无 membership 时回退旧校验：
// 目标账号必须存在、active 且属于同一租户。返回值 0 表示走 user_id 兼容列。
func ResolveAssignable(ctx context.Context, client *ent.Client, userID, tenantID int) (int, error) {
	membershipID, err := ResolveOptional(ctx, client, userID, tenantID)
	if err != nil {
		return 0, err
	}
	if membershipID > 0 {
		return membershipID, nil
	}
	if client == nil {
		return 0, nil
	}
	exists, err := client.User.Query().
		Where(user.IDEQ(userID), user.TenantIDEQ(tenantID), user.ActiveEQ(true)).
		Exist(ctx)
	if err != nil {
		return 0, fmt.Errorf("验证处理人失败: %w", err)
	}
	if !exists {
		return 0, common.NewBadRequestError("处理人不存在或不可用", nil)
	}
	return 0, nil
}
