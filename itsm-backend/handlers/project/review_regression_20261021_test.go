package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 2026-10-21 回归验证报告遗留项（A2 归口维护、报表部门维度）
// 上游：docs/project-management/reviews/2026-10-21-项目功能回归验证报告.md
// ---------------------------------------------------------------------------

// TestOwnerTeamCandidates A2：归口候选只返回同租户、未软删、active 的团队。
func TestOwnerTeamCandidates(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	active, err := client.Team.Create().
		SetName("交付部").SetCode("TEAM-DELIVERY").SetStatus("active").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	inactive, err := client.Team.Create().
		SetName("停用团队").SetCode("TEAM-OFF").SetStatus("inactive").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	removed, err := client.Team.Create().
		SetName("已删除团队").SetCode("TEAM-GONE").SetStatus("active").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Team.UpdateOneID(removed.ID).SetDeletedAt(time.Now()).Exec(ctx))

	// 其他租户团队不得出现。
	otherTenant, err := client.Tenant.Create().SetName("其它租户").SetCode("TEAM-T2").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	_, err = client.Team.Create().
		SetName("外租团队").SetCode("TEAM-FOREIGN").SetStatus("active").SetTenantID(otherTenant.ID).Save(ctx)
	require.NoError(t, err)

	items, err := svc.ListOwnerTeamCandidates(ctx, tid, "", 20)
	require.NoError(t, err)
	require.Len(t, items, 1, "只应有 1 个可用团队，实际 %+v", items)
	assert.Equal(t, active.ID, items[0].ID)
	assert.Equal(t, "交付部", items[0].Name)
	assert.Equal(t, "TEAM-DELIVERY", items[0].Code)
	for _, item := range items {
		assert.NotEqual(t, inactive.ID, item.ID)
		assert.NotEqual(t, removed.ID, item.ID)
	}

	// 关键词过滤（名称/代码）。
	byCode, err := svc.ListOwnerTeamCandidates(ctx, tid, "DELIVERY", 20)
	require.NoError(t, err)
	assert.Len(t, byCode, 1)
	byName, err := svc.ListOwnerTeamCandidates(ctx, tid, "交付", 20)
	require.NoError(t, err)
	assert.Len(t, byName, 1)
	none, err := svc.ListOwnerTeamCandidates(ctx, tid, "不存在", 20)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestOwnerTeamCandidatesEndpoint A2：端点信封 + 候选数据（创建/编辑项目共用）。
func TestOwnerTeamCandidatesEndpoint(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()
	team, err := client.Team.Create().
		SetName("信息技术部团队").SetCode("TEAM-IT").SetStatus("active").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)

	res := apiJSON(t, r, http.MethodGet, "/projects/owner-team-candidates", nil)
	require.Equal(t, common.SuccessCode, res.Code)
	var items []OwnerTeamCandidate
	require.NoError(t, json.Unmarshal(res.Data, &items))
	require.Len(t, items, 1)
	assert.Equal(t, team.ID, items[0].ID)
	assert.Equal(t, "信息技术部团队", items[0].Name)
}

// TestReportGroupsIncludeDepartment A2：归口未设置时报表仍按部门归因（不再全部「未归口」）。
func TestReportGroupsIncludeDepartment(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	deptA, err := client.Department.Create().
		SetName("信息技术部").SetCode("RG-DEPT-A").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	deptB, err := client.Department.Create().
		SetName("生产部").SetCode("RG-DEPT-B").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)

	for i, deptID := range []int{deptA.ID, deptA.ID, deptB.ID} {
		_, err := svc.Create(ctx, &CreateRequest{
			TenantID: tid, Name: fmt.Sprintf("报表分组项目%d", i+1), DepartmentID: &deptID,
		})
		require.NoError(t, err)
	}
	// 无部门项目（历史空壳）单列。
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "无部门项目"})
	require.NoError(t, err)

	groups, err := svc.ReportGroupsByOwnerTeam(ctx, tid, 0, true)
	require.NoError(t, err)
	require.Len(t, groups, 3, "两个部门 + 未设置部门，实际 %+v", groups)

	byDept := map[int]ReportGroupResponse{}
	for _, g := range groups {
		assert.Nil(t, g.OwnerTeamID, "本用例全部未归口")
		assert.Equal(t, "未归口", g.OwnerTeamName)
		if g.DepartmentID != nil {
			byDept[*g.DepartmentID] = g
		}
	}
	require.Contains(t, byDept, deptA.ID)
	require.Contains(t, byDept, deptB.ID)
	assert.Equal(t, 2, byDept[deptA.ID].ProjectCount)
	assert.Equal(t, "信息技术部", byDept[deptA.ID].DepartmentName)
	assert.Equal(t, 1, byDept[deptB.ID].ProjectCount)
	assert.Equal(t, "生产部", byDept[deptB.ID].DepartmentName)

	// 合计与项目总数一致（无重复计数）。
	total := 0
	for _, g := range groups {
		total += g.ProjectCount
	}
	assert.Equal(t, 4, total)
}
