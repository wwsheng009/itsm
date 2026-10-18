package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTeam 建归口团队（PE-17；可后续软删）。
func seedTeam(t *testing.T, client *ent.Client, tenantID int, name string, managerID int) *ent.Team {
	t.Helper()
	entity, err := client.Team.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetCode("TEAM-" + name).
		SetStatus("active").
		SetManagerID(managerID).
		Save(context.Background())
	require.NoError(t, err)
	return entity
}

func ptrInt(t *testing.T, v int) *int {
	t.Helper()
	return &v
}

// TestOwnerTeamGovernance 归口写入校验（同租户/未软删/active）+ 读取富化 + 软删降级 + 清空。
func TestOwnerTeamGovernance(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	pm := seedUserWithMembership(t, client, tid, "own-pm", "home", nil)
	teamEntity := seedTeam(t, client, tid, "交付一组", pm.ID)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "归口项目", OwnerTeamID: ptrInt(t, teamEntity.ID)})
	require.NoError(t, err)
	resp := ToResponseWithOwnerTeams(p, svc.OwnerTeamRefs(ctx, tid, []*ent.Project{p}))
	require.NotNil(t, resp.OwnerTeamID)
	assert.Equal(t, "交付一组", resp.OwnerTeamName)
	assert.False(t, resp.OwnerTeamDeleted)

	missing := 999999
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "坏归口", OwnerTeamID: &missing})
	assert.ErrorIs(t, err, ErrOwnerTeamUnavailable)

	plain, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "无归口项目"})
	require.NoError(t, err)
	_, err = client.Team.UpdateOneID(teamEntity.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	refs := svc.OwnerTeamRefs(ctx, tid, []*ent.Project{p})
	require.Contains(t, refs, teamEntity.ID)
	assert.True(t, refs[teamEntity.ID].Deleted)

	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "删后归口", OwnerTeamID: ptrInt(t, teamEntity.ID)})
	assert.ErrorIs(t, err, ErrOwnerTeamUnavailable)
	_, err = svc.Update(ctx, tid, plain.ID, &UpdateRequest{OwnerTeamID: ptrInt(t, teamEntity.ID)})
	assert.ErrorIs(t, err, ErrOwnerTeamUnavailable)

	// 读取降级：软删团队仍展示名称 + deleted 标记（UI 提示重新归口）。
	degraded := ToResponseWithOwnerTeams(p, refs)
	assert.Equal(t, "交付一组", degraded.OwnerTeamName)
	assert.True(t, degraded.OwnerTeamDeleted)

	// 显式清空归口（0 = 清空）允许。
	cleared, err := svc.Update(ctx, tid, plain.ID, &UpdateRequest{OwnerTeamID: ptrInt(t, 0)})
	require.NoError(t, err)
	assert.Zero(t, cleared.OwnerTeamID)
}

// TestReportTemplateLifecycle 报告模板 CRUD：词表校验/去重/未知章节丢弃/乐观锁/软删。
func TestReportTemplateLifecycle(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	admin := seedUserWithMembership(t, client, tid, "rep-admin", "home", nil)

	created, err := svc.CreateReportTemplate(ctx, tid, admin.ID, &ReportTemplateCreateRequest{
		Name:     "周报模板",
		Cadence:  ReportCadenceWeekly,
		Sections: []string{ReportSectionOverview, ReportSectionTasks, "unknown", ReportSectionOverview},
	})
	require.NoError(t, err)
	assert.Equal(t, ReportTemplateStatusDraft, created.Status)
	assert.Equal(t, []string{ReportSectionOverview, ReportSectionTasks}, created.Sections, "未知章节丢弃且去重保序")
	assert.Equal(t, 1, created.Version)

	_, err = svc.CreateReportTemplate(ctx, tid, admin.ID, &ReportTemplateCreateRequest{Name: "周报模板"})
	assert.ErrorIs(t, err, ErrReportTemplateExists)
	_, err = svc.CreateReportTemplate(ctx, tid, admin.ID, &ReportTemplateCreateRequest{Name: "日报", Cadence: "daily"})
	assert.ErrorIs(t, err, ErrValidation)

	active := ReportTemplateStatusActive
	v1 := created.Version
	updated, err := svc.UpdateReportTemplate(ctx, tid, created.ID, &ReportTemplateUpdateRequest{
		Status: &active, Version: &v1,
	})
	require.NoError(t, err)
	assert.Equal(t, ReportTemplateStatusActive, updated.Status)
	assert.Equal(t, 2, updated.Version)

	// 旧版本 → 409 语义（CAS 冲突）。
	_, err = svc.UpdateReportTemplate(ctx, tid, created.ID, &ReportTemplateUpdateRequest{Status: &active, Version: &v1})
	var casErr *ReportTemplateCASError
	require.ErrorAs(t, err, &casErr)
	assert.Equal(t, 2, casErr.CurrentVersion)

	items, total, err := svc.ListReportTemplates(ctx, ReportTemplateListFilter{
		TenantID: tid, Status: ReportTemplateStatusActive,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)

	require.NoError(t, svc.DeleteReportTemplate(ctx, tid, created.ID))
	_, err = svc.GetReportTemplate(ctx, tid, created.ID)
	assert.ErrorIs(t, err, ErrReportTemplateNotFound)
	// 软删后列表不含。
	_, total, err = svc.ListReportTemplates(ctx, ReportTemplateListFilter{TenantID: tid})
	require.NoError(t, err)
	assert.Zero(t, total)
}

// TestGenerateProjectReportDataDerived 报告数据派生：任务/里程碑统计、偏差（无基线省略）、
// 模板章节裁剪、归档模板拒绝、可见性收窄。
func TestGenerateProjectReportDataDerived(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	now := time.Now()
	pm := seedUserWithMembership(t, client, tid, "report-pm", "home", nil)
	teamEntity := seedTeam(t, client, tid, "平台组", pm.ID)
	stranger := seedUserWithMembership(t, client, tid, "report-stranger", "home", nil)

	p, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "报告项目", ManagerID: ptrInt(t, pm.ID), OwnerTeamID: ptrInt(t, teamEntity.ID),
	})
	require.NoError(t, err)
	// 报告可见性 = D-06 成员可见性：PM 需登记为活跃成员（立项门口径）。
	_, err = client.ProjectMember.Create().SetTenantID(tid).SetProjectID(p.ID).
		SetUserID(pm.ID).SetProjectRole("pm").SetStatus("active").Save(ctx)
	require.NoError(t, err)

	for _, seed := range []struct {
		name     string
		status   string
		planEnd  time.Time
	}{
		{name: "完成", status: "done", planEnd: now.Add(-48 * time.Hour)},
		{name: "逾期", status: "in_progress", planEnd: now.Add(-24 * time.Hour)},
		{name: "临期", status: "todo", planEnd: now.Add(24 * time.Hour)},
	} {
		_, err := client.ProjectTask.Create().
			SetTenantID(tid).SetProjectID(p.ID).SetName(seed.name).
			SetStatus(seed.status).SetPlanEnd(seed.planEnd).Save(ctx)
		require.NoError(t, err)
	}
	reachedAt := now.Add(-96 * time.Hour)
	_, err = client.ProjectMilestone.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetName("按期达成").
		SetDueDate(now.Add(-72 * time.Hour)).SetStatus("reached").SetReachedAt(reachedAt).Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectMilestone.Create().
		SetTenantID(tid).SetProjectID(p.ID).SetName("逾期未达成").
		SetDueDate(now.Add(-24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	report, err := svc.GenerateProjectReport(ctx, tid, p.ID, pm.ID, false, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, report.Data.TaskStats.Total)
	assert.Equal(t, 1, report.Data.TaskStats.Done)
	assert.Equal(t, 2, report.Data.TaskStats.Open)
	assert.Equal(t, 1, report.Data.TaskStats.Overdue)
	assert.Equal(t, 1, report.Data.TaskStats.DueSoon)
	assert.Equal(t, 2, report.Data.MilestoneStats.Total)
	assert.Equal(t, 1, report.Data.MilestoneStats.Reached)
	assert.Equal(t, 1, report.Data.MilestoneStats.OnTime)
	assert.Equal(t, 1, report.Data.MilestoneStats.Overdue)
	assert.InDelta(t, 0.5, report.Data.MilestoneStats.Rate, 0.0001)
	assert.Nil(t, report.Data.Variance, "无基线 → 偏差省略")
	require.NotNil(t, report.Data.OwnerTeam)
	assert.Equal(t, "平台组", report.Data.OwnerTeam.Name)
	assert.False(t, report.Data.OwnerTeam.Deleted)
	assert.Contains(t, report.Markdown, "## 概览")
	assert.Contains(t, report.Markdown, "未冻结计划基线")

	// 冻结基线 → 偏差汇总出现；模板裁剪章节。
	_, err = svc.CreateBaseline(ctx, tid, p.ID, pm.ID, &BaselineCreateRequest{Reason: "首个基线"})
	require.NoError(t, err)
	tpl, err := svc.CreateReportTemplate(ctx, tid, pm.ID, &ReportTemplateCreateRequest{
		Name: "偏差摘要", Cadence: ReportCadenceBiweekly,
		Sections: []string{ReportSectionOverview, ReportSectionVariance}, Status: ReportTemplateStatusActive,
	})
	require.NoError(t, err)
	templated, err := svc.GenerateProjectReport(ctx, tid, p.ID, pm.ID, false, tpl.ID)
	require.NoError(t, err)
	require.NotNil(t, templated.Data.Variance)
	assert.Equal(t, 1, templated.Data.Variance.BaselineVersion)
	require.NotNil(t, templated.Data.Template)
	assert.Equal(t, ReportCadenceBiweekly, templated.Data.Template.Cadence)
	assert.Equal(t, []string{ReportSectionOverview, ReportSectionVariance}, templated.Data.Sections)
	assert.NotContains(t, templated.Markdown, "## 任务", "模板未选章节不输出")

	// 归档模板拒绝生成。
	archived := ReportTemplateStatusArchived
	v := tpl.Version
	_, err = svc.UpdateReportTemplate(ctx, tid, tpl.ID, &ReportTemplateUpdateRequest{Status: &archived, Version: &v})
	require.NoError(t, err)
	_, err = svc.GenerateProjectReport(ctx, tid, p.ID, pm.ID, false, tpl.ID)
	assert.ErrorIs(t, err, ErrReportTemplateArchived)

	// 可见性：非成员 → 404 语义。
	_, err = svc.GenerateProjectReport(ctx, tid, p.ID, stranger.ID, false, 0)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestReportGroupsByOwnerTeam 报表分组：归口聚合 + 健康度分布 + 未归口行 + 可见性收窄。
func TestReportGroupsByOwnerTeam(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	admin := seedUserWithMembership(t, client, tid, "group-admin", "home", nil)
	teamA := seedTeam(t, client, tid, "A 组", admin.ID)
	teamB := seedTeam(t, client, tid, "B 组", admin.ID)

	pA1, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "A1", OwnerTeamID: ptrInt(t, teamA.ID)})
	require.NoError(t, err)
	pA2, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "A2", OwnerTeamID: ptrInt(t, teamA.ID)})
	require.NoError(t, err)
	// PE-27（D-54）：健康度改为事实派生 —— A2 逾期里程碑 → yellow；
	past := time.Now().Add(-48 * time.Hour)
	_, err = client.ProjectMilestone.Create().SetTenantID(tid).SetProjectID(pA2.ID).
		SetName("逾期里程碑").SetDueDate(past).SetStatus("planned").Save(ctx)
	require.NoError(t, err)
	pB, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "B1", OwnerTeamID: ptrInt(t, teamB.ID)})
	require.NoError(t, err)
	// B1 计划结束日已过（非终态）→ red。
	_, err = svc.Update(ctx, tid, pB.ID, &UpdateRequest{EndDate: &past})
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "未归口"})
	require.NoError(t, err)

	groups, err := svc.ReportGroupsByOwnerTeam(ctx, tid, admin.ID, true)
	require.NoError(t, err)
	require.Len(t, groups, 3, "两个归口团队 + 未归口")
	assert.Nil(t, groups[0].OwnerTeamID, "未归口行排最前")
	assert.Equal(t, "未归口", groups[0].OwnerTeamName)
	assert.Equal(t, 1, groups[0].ProjectCount)

	byTeam := make(map[int]ReportGroupResponse, 2)
	for _, group := range groups {
		if group.OwnerTeamID != nil {
			byTeam[*group.OwnerTeamID] = group
		}
	}
	require.Contains(t, byTeam, teamA.ID)
	assert.Equal(t, 2, byTeam[teamA.ID].ProjectCount)
	assert.Equal(t, 1, byTeam[teamA.ID].GreenCount)
	assert.Equal(t, 1, byTeam[teamA.ID].YellowCount)
	assert.Equal(t, "A 组", byTeam[teamA.ID].OwnerTeamName)
	require.Contains(t, byTeam, teamB.ID)
	assert.Equal(t, 1, byTeam[teamB.ID].ProjectCount)
	assert.Equal(t, 1, byTeam[teamB.ID].RedCount)

	// 可见性收窄：viewer 只为 A1 活跃成员 → 仅 A1 计入分组。
	viewer := seedUserWithMembership(t, client, tid, "group-viewer", "home", nil)
	_, err = svc.AddMember(ctx, tid, pA1.ID, admin.ID, &MemberAddRequest{UserID: viewer.ID, ProjectRole: "member"})
	require.NoError(t, err)
	narrowed, err := svc.ReportGroupsByOwnerTeam(ctx, tid, viewer.ID, false)
	require.NoError(t, err)
	require.Len(t, narrowed, 1)
	assert.Equal(t, 1, narrowed[0].ProjectCount)
	assert.Equal(t, 1, narrowed[0].GreenCount)

	// 无项目可见 → 空数组（不 404）。
	stranger := seedUserWithMembership(t, client, tid, "group-stranger", "home", nil)
	empty, err := svc.ReportGroupsByOwnerTeam(ctx, tid, stranger.ID, false)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// 软删团队：分组行降级标记，名称保留。
	_, err = client.Team.UpdateOneID(teamB.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	degraded, err := svc.ReportGroupsByOwnerTeam(ctx, tid, admin.ID, true)
	require.NoError(t, err)
	for _, group := range degraded {
		if group.OwnerTeamID != nil && *group.OwnerTeamID == teamB.ID {
			assert.True(t, group.OwnerTeamDeleted)
			assert.Equal(t, "B 组", group.OwnerTeamName)
		}
	}
}

// TestReportEndpoints HTTP 端点 + 审计：模板 CRUD、报告生成（含 409）、报表分组。
func TestReportEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	// 项目 + 里程碑（报告数据源）。
	createProject := apiJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "端点报告项目"})
	require.Equal(t, common.SuccessCode, createProject.Code)
	var projectResp ProjectResponse
	require.NoError(t, json.Unmarshal(createProject.Data, &projectResp))
	_, err := client.ProjectMilestone.Create().SetTenantID(tid).SetProjectID(projectResp.ID).
		SetName("端点里程碑").SetDueDate(time.Now().Add(12 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	// 模板：创建（active）→ 列表 → 生成报告 → 归档后生成 409 → 删除。
	createTemplate := apiJSON(t, r, http.MethodPost, "/projects/report-templates", map[string]any{
		"name": "端点模板", "cadence": "weekly", "status": "active",
		"sections": []string{"overview", "milestones"},
	})
	require.Equal(t, common.SuccessCode, createTemplate.Code, string(createTemplate.Data))
	var templateResp ReportTemplateResponse
	require.NoError(t, json.Unmarshal(createTemplate.Data, &templateResp))
	assert.Equal(t, ReportTemplateStatusActive, templateResp.Status)

	list := apiJSON(t, r, http.MethodGet, "/projects/report-templates?status=active&q=端点", nil)
	require.Equal(t, common.SuccessCode, list.Code)
	assert.Contains(t, string(list.Data), "端点模板")

	generate := apiJSON(t, r, http.MethodPost, pathInt("/projects/%d/reports/generate", projectResp.ID), map[string]any{
		"templateId": templateResp.ID,
	})
	require.Equal(t, common.SuccessCode, generate.Code, string(generate.Data))
	var report ReportResponse
	require.NoError(t, json.Unmarshal(generate.Data, &report))
	assert.Contains(t, report.Markdown, "端点报告项目")
	assert.Equal(t, 1, report.Data.MilestoneStats.Total)
	require.NotNil(t, report.Data.Template)
	assert.Equal(t, "端点模板", report.Data.Template.Name)

	archive := apiJSON(t, r, http.MethodPut, pathInt("/projects/report-templates/%d", templateResp.ID), map[string]any{
		"status": ReportTemplateStatusArchived, "version": templateResp.Version,
	})
	require.Equal(t, common.SuccessCode, archive.Code)
	blocked := apiJSON(t, r, http.MethodPost, pathInt("/projects/%d/reports/generate", projectResp.ID), map[string]any{
		"templateId": templateResp.ID,
	})
	assert.Equal(t, common.ConflictCode, blocked.Code)

	groups := apiJSON(t, r, http.MethodGet, "/projects/report-groups", nil)
	require.Equal(t, common.SuccessCode, groups.Code)
	assert.Contains(t, string(groups.Data), "未归口")

	deleted := apiJSON(t, r, http.MethodDelete, pathInt("/projects/report-templates/%d", templateResp.ID), nil)
	require.Equal(t, common.SuccessCode, deleted.Code)

	// 审计：模板创建/更新/删除 + 报告生成（resource=project）。
	for _, action := range []string{"report_template_create", "report_template_update", "report_template_delete", "report_generate"} {
		count, err := client.AuditLog.Query().Where(auditlog.ActionEQ(action)).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "action=%s 应留痕", action)
	}
}

// pathInt 路径格式化助手（fmt.Sprintf 的语义别名，保持断言行简洁）。
func pathInt(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// apiJSON 复用 handler_test.go 的 doJSON（丢弃 recorder，仅取统一响应）。
func apiJSON(t *testing.T, r *gin.Engine, method, path string, body any) envelope {
	t.Helper()
	_, env := doJSON(t, r, method, path, body)
	return env
}
