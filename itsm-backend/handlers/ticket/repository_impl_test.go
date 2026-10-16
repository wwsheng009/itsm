package ticket

import (
	"testing"

	"itsm-backend/handlers/common/datascope"
	repoticket "itsm-backend/repository/ticket"
)

// TestToRepositoryDataScope_EnumAlignment（2026-10-16 回归锁定）：
// datascope 与 repository/ticket 的 DataScope 枚举集合数值不对齐
// （datascope.OwnedOrAssigned=2，ticket.DataScope.OwnedOrAssigned=1），
// 历史的直接转换 ticket.DataScope(dataScope) 会把 OwnedOrAssigned(2)
// 静默吞成 All(0)——end_user/agent 列表可见全租户工单（阻断8 回归）。
// 本测试锁定显式映射；若任一包调整枚举值，此处必须同步复核。
func TestToRepositoryDataScope_EnumAlignment(t *testing.T) {
	cases := []struct {
		name string
		in   datascope.DataScope
		want repoticket.DataScope
	}{
		{"all", datascope.DataScopeAll, repoticket.DataScopeAll},
		{"owned-or-assigned", datascope.DataScopeOwnedOrAssigned, repoticket.DataScopeOwnedOrAssigned},
		{"department-safe-narrow", datascope.DataScopeDepartment, repoticket.DataScopeOwnedOrAssigned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toRepositoryDataScope(tc.in); got != tc.want {
				t.Fatalf("toRepositoryDataScope(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	// 前提断言：直接类型转换必须仍然是错的（两包枚举数值不同）。
	// 若未来两包枚举收敛为同一集合，本测试失败即是提醒：复核映射函数并更新本测试。
	if repoticket.DataScope(datascope.DataScopeOwnedOrAssigned) == repoticket.DataScopeOwnedOrAssigned {
		t.Fatal("两包 DataScope 枚举数值已对齐：请复核 toRepositoryDataScope（可直接转换后删除映射），并更新本测试")
	}
}
