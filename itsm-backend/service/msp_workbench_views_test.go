package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func newViewTestDSN(_ *testing.T, tag string) string {
	return fmt.Sprintf("file:workbenchviews_%s_%d?mode=memory&cache=shared&_fk=1", tag, time.Now().UnixNano())
}

func newViewActor(id, tenant int, allowed ...int) MSPWorkbenchActor {
	return MSPWorkbenchActor{UserID: id, HomeTenantID: tenant, AllowedCustomers: allowed}
}

// TestMSPWorkbenchViews_Lifecycle 覆盖：灰度默认关 / 创建 / 重名 / 非法客户 /
// 分享可见性 / 越权拒绝 / 默认唯一 / 删除。
func TestMSPWorkbenchViews_Lifecycle(t *testing.T) {
	client := enttest.Open(t, "sqlite3", newViewTestDSN(t, "lifecycle"))
	defer client.Close()
	ctx := context.Background()
	actor := newViewActor(7, 100, 1, 2, 3)
	other := newViewActor(8, 100, 1, 2)

	// 默认关（未显式开启时 fail-closed）
	disabled := NewMSPWorkbenchViewService(client, zaptest.NewLogger(t).Sugar())
	_, err := disabled.ListViews(ctx, actor)
	require.Error(t, err)
	ve, ok := AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewsDisabled, ve.Code)

	svc := NewMSPWorkbenchViewService(client, zaptest.NewLogger(t).Sugar())
	svc.SetViewsEnabled(true)

	// 创建（分享态 + 过滤器规范化）
	created, err := svc.CreateView(ctx, actor, dto.WorkbenchViewCreateRequest{
		Name: "  Acme 紧急  ",
		Filters: dto.WorkbenchViewFilter{
			CustomerTenantIDs: []int{1, 2, 2}, // 重复 ID 去重
			Status:            "open",
			Priority:          " high ",
			Sort:              "sla",
		},
		IsShared: true,
	})
	require.NoError(t, err)
	require.Equal(t, "Acme 紧急", created.Name)
	assert.True(t, created.IsShared)
	assert.True(t, created.IsOwner)
	assert.Equal(t, []int{1, 2}, created.Filters.CustomerTenantIDs)
	assert.Equal(t, "high", created.Filters.Priority)
	assert.Equal(t, "sla", created.Filters.Sort)

	// 重名冲突
	_, err = svc.CreateView(ctx, actor, dto.WorkbenchViewCreateRequest{Name: "Acme 紧急"})
	require.Error(t, err)
	ve, ok = AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewNameDup, ve.Code)

	// 未分配客户 → 非法
	_, err = svc.CreateView(ctx, actor, dto.WorkbenchViewCreateRequest{
		Name:    "越权",
		Filters: dto.WorkbenchViewFilter{CustomerTenantIDs: []int{99}},
	})
	require.Error(t, err)
	ve, ok = AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewInvalid, ve.Code)

	// sort 白名单
	_, err = svc.CreateView(ctx, actor, dto.WorkbenchViewCreateRequest{
		Name:    "坏排序",
		Filters: dto.WorkbenchViewFilter{Sort: "created"},
	})
	require.Error(t, err)

	// 他人可见分享视图（只读）；私有视图不可见
	private, err := svc.CreateView(ctx, actor, dto.WorkbenchViewCreateRequest{
		Name:    "私有",
		Filters: dto.WorkbenchViewFilter{CustomerTenantIDs: []int{1}},
	})
	require.NoError(t, err)
	list, err := svc.ListViews(ctx, other)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, created.ID, list[0].ID)
	assert.False(t, list[0].IsOwner)

	// 非 owner 更新/删除 → 403
	_, err = svc.UpdateView(ctx, other, created.ID, dto.WorkbenchViewUpdateRequest{Name: "改名"})
	require.Error(t, err)
	ve, ok = AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewOwnerOnly, ve.Code)
	require.Error(t, svc.DeleteView(ctx, other, created.ID))

	// owner 更新（替换过滤器 + 取消分享）
	updated, err := svc.UpdateView(ctx, actor, created.ID, dto.WorkbenchViewUpdateRequest{
		Name:    "Acme 紧急2",
		Filters: dto.WorkbenchViewFilter{CustomerTenantIDs: []int{3}},
	})
	require.NoError(t, err)
	assert.Equal(t, []int{3}, updated.Filters.CustomerTenantIDs)
	assert.False(t, updated.IsShared)

	// 默认唯一：连续设两次，仅最后一次生效
	_, err = svc.SetDefaultView(ctx, actor, created.ID)
	require.NoError(t, err)
	_, err = svc.SetDefaultView(ctx, actor, private.ID)
	require.NoError(t, err)
	list, err = svc.ListViews(ctx, actor)
	require.NoError(t, err)
	var defaults []int
	for _, v := range list {
		if v.IsDefault {
			defaults = append(defaults, v.ID)
		}
	}
	require.Len(t, defaults, 1)
	assert.Equal(t, private.ID, defaults[0])

	// 删除（owner）→ 再删 404
	require.NoError(t, svc.DeleteView(ctx, actor, private.ID))
	err = svc.DeleteView(ctx, actor, private.ID)
	require.Error(t, err)
	ve, ok = AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewNotFound, ve.Code)
}

// TestMSPWorkbenchViews_TenantIsolation 跨 provider 租户不可见/不可改（即使 userID 相同）。
func TestMSPWorkbenchViews_TenantIsolation(t *testing.T) {
	client := enttest.Open(t, "sqlite3", newViewTestDSN(t, "isolation"))
	defer client.Close()
	ctx := context.Background()
	svc := NewMSPWorkbenchViewService(client, zaptest.NewLogger(t).Sugar())
	svc.SetViewsEnabled(true)

	actorA := newViewActor(7, 100, 1)
	actorB := newViewActor(7, 200, 1)

	created, err := svc.CreateView(ctx, actorA, dto.WorkbenchViewCreateRequest{Name: "A 视图", IsShared: true})
	require.NoError(t, err)

	listB, err := svc.ListViews(ctx, actorB)
	require.NoError(t, err)
	require.Empty(t, listB, "跨 provider 不可见（即使分享态）")

	_, err = svc.UpdateView(ctx, actorB, created.ID, dto.WorkbenchViewUpdateRequest{Name: "改名"})
	require.Error(t, err)
	ve, ok := AsWorkbenchViewError(err)
	require.True(t, ok)
	require.Equal(t, CodeWorkbenchViewNotFound, ve.Code)
}
