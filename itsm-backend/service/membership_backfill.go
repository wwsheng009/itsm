package service

import (
	"context"
	"errors"
	"fmt"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/mspallocation"
	"itsm-backend/ent/user"
	"itsm-backend/ent/usertenantmembership"
)

// membership_backfill.go：存量 membership 回填修复器（MSW-FU-1；
// 重放迁移 20260504 回填 1/2 的语义，口径与建号/分配写通道完全一致）。
//
// 背景：M1（2026-10-07）之前创建的账号与分配没有 membership 写入路径，
// 巡检（cmd/membership-drift-check）表现为 missing_membership FAIL：
//   - 活跃账号缺 home membership；
//   - provider 员工有活跃分配但缺客户作用域 membership（切换后权限空窗）。
//
// 语义（与 20260504 回填一致，创建优先、不覆盖既有异源行）：
//   - 活跃账号缺 home membership → 创建（source=home，is_default=true，
//     account_kind 按账号类型快照，role 词表按建号通道口径）；
//   - 活跃分配缺客户作用域 membership → 创建（source=allocation，allocation_id，
//     is_default=false，role 词表按分配口径 specialist→msp_specialist / 其他→msp_tech）；
//   - 已存在同源存活行 → 默认跳过；Reconcile=true 时幂等校正（修 role_id/kind/status 漂移）；
//   - 异源存活行（invite/platform/migration 等）→ 不覆盖，记入 Conflicts 交巡检/人工处置。
//
// 每次实际写入复用 MembershipSyncService（同一幂等 upsert + membership.sync 审计）。
// Apply=false 为只读演练：不写库，仅统计"将创建/将跳过/冲突"。

// MembershipBackfillOptions 回填选项。
type MembershipBackfillOptions struct {
	Apply     bool // false=dry-run（默认）；true=写库
	Reconcile bool // true=对已存在同源行做幂等校正（修 role_id/kind/status 漂移）
}

// MembershipBackfillReport 回填报告（CLI -json 直接序列化）。
type MembershipBackfillReport struct {
	Applied            bool     `json:"applied"`
	Reconcile          bool     `json:"reconcile"`
	UsersScanned       int      `json:"usersScanned"`
	AllocationsScanned int      `json:"allocationsScanned"`
	HomeCreated        int      `json:"homeCreated"`
	HomeSkipped        int      `json:"homeSkipped"`
	HomeReconciled     int      `json:"homeReconciled"`
	HomeConflicts      int      `json:"homeConflicts"`
	AllocCreated       int      `json:"allocCreated"`
	AllocSkipped       int      `json:"allocSkipped"`
	AllocReconciled    int      `json:"allocReconciled"`
	AllocConflicts     int      `json:"allocConflicts"`
	Conflicts          []string `json:"conflicts,omitempty"`
	Errors             []string `json:"errors,omitempty"`
}

// BackfillMemberships 执行存量回填（幂等；可重复执行）。
func (s *MembershipSyncService) BackfillMemberships(
	ctx context.Context,
	meta MembershipAuditMeta,
	opts MembershipBackfillOptions,
) (*MembershipBackfillReport, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("membership sync service unavailable")
	}
	rep := &MembershipBackfillReport{Applied: opts.Apply, Reconcile: opts.Reconcile}

	users, err := s.client.User.Query().
		Where(user.ActiveEQ(true)).
		Order(ent.Asc(user.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active users: %w", err)
	}
	rep.UsersScanned = len(users)
	for _, u := range users {
		scopeCtx := tenantctx.WithTenantID(ctx, u.TenantID)
		live, err := s.liveMembership(scopeCtx, u.ID, u.TenantID)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("home user=%d tenant=%d: %v", u.ID, u.TenantID, err))
			continue
		}
		switch {
		case live == nil:
			if !opts.Apply {
				rep.HomeCreated++ // 演练口径：将创建
				continue
			}
			roleCode, mspRole := provisionMembershipRoles(u)
			if _, err := s.SyncHomeMembership(ctx, u.ID, u.TenantID, roleCode, mspRole, meta); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("home user=%d tenant=%d: %v", u.ID, u.TenantID, err))
				continue
			}
			rep.HomeCreated++
		case live.Source == usertenantmembership.SourceHome:
			if !opts.Reconcile {
				rep.HomeSkipped++
				continue
			}
			if !opts.Apply {
				rep.HomeReconciled++ // 演练口径：将校正
				continue
			}
			roleCode, mspRole := provisionMembershipRoles(u)
			if _, err := s.SyncHomeMembership(ctx, u.ID, u.TenantID, roleCode, mspRole, meta); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("home reconcile user=%d tenant=%d: %v", u.ID, u.TenantID, err))
				continue
			}
			rep.HomeReconciled++
		default:
			rep.HomeConflicts++
			rep.Conflicts = append(rep.Conflicts,
				fmt.Sprintf("home user=%d tenant=%d existing_source=%s（不覆盖）", u.ID, u.TenantID, live.Source))
		}
	}

	allocs, err := s.client.MSPAllocation.Query().
		Where(mspallocation.DeassignedAtIsNil()).
		Order(ent.Asc(mspallocation.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active allocations: %w", err)
	}
	rep.AllocationsScanned = len(allocs)
	for _, a := range allocs {
		scopeCtx := tenantctx.WithTenantID(ctx, a.CustomerTenantID)
		live, err := s.liveMembership(scopeCtx, a.MspUserID, a.CustomerTenantID)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("allocation=%d user=%d tenant=%d: %v", a.ID, a.MspUserID, a.CustomerTenantID, err))
			continue
		}
		switch {
		case live == nil:
			if !opts.Apply {
				rep.AllocCreated++
				continue
			}
			if _, err := s.SyncAllocationMembership(ctx, a, meta); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("allocation=%d user=%d tenant=%d: %v", a.ID, a.MspUserID, a.CustomerTenantID, err))
				continue
			}
			rep.AllocCreated++
		case live.Source == usertenantmembership.SourceAllocation:
			if !opts.Reconcile {
				rep.AllocSkipped++
				continue
			}
			if !opts.Apply {
				rep.AllocReconciled++
				continue
			}
			if _, err := s.SyncAllocationMembership(ctx, a, meta); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("allocation reconcile=%d user=%d tenant=%d: %v", a.ID, a.MspUserID, a.CustomerTenantID, err))
				continue
			}
			rep.AllocReconciled++
		default:
			rep.AllocConflicts++
			rep.Conflicts = append(rep.Conflicts,
				fmt.Sprintf("allocation=%d user=%d tenant=%d existing_source=%s（不覆盖）", a.ID, a.MspUserID, a.CustomerTenantID, live.Source))
		}
	}
	return rep, nil
}
