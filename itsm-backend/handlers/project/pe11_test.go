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

// TestTemplateCRUDAndApply 项目模板：CRUD/状态生命周期 + 一键生成（阶段/任务/检查项/里程碑）与幂等（PE-11）。
func TestTemplateCRUDAndApply(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	member := seedUserWithMembership(t, client, tid, "tp", "home", nil)

	startOffset, duration := 3, 4
	hours := 6.5
	tpl, err := svc.CreateTemplate(ctx, tid, member.ID, &TemplateCreateRequest{
		Name:        "交付标准模板",
		Description: "标准交付",
		Type:        "delivery",
		Payload: &TemplatePayload{
			Milestones: []TemplateMilestoneSpec{{Name: "立项完成", OffsetDays: 2}},
			Tasks: []TemplateTaskSpec{
				{Stage: "启动", Name: "项目章程", Priority: "high", StartOffsetDays: &startOffset, DurationDays: &duration, EstimateHours: &hours, Checklist: []string{"干系人登记", "里程碑确认"}},
				{Stage: "启动", Name: "启动会"},
				{Name: "未分组任务"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, TemplateStatusDraft, tpl.Status)
	assert.Equal(t, 1, tpl.Version)
	assert.Equal(t, "delivery", tpl.Type)

	// 重名 409 语义。
	_, err = svc.CreateTemplate(ctx, tid, member.ID, &TemplateCreateRequest{
		Name:    "交付标准模板",
		Payload: &TemplatePayload{Tasks: []TemplateTaskSpec{{Name: "任意任务"}}},
	})
	require.ErrorIs(t, err, ErrTemplateNameExists)

	// 校验：空 payload / 非法 priority。
	_, err = svc.CreateTemplate(ctx, tid, member.ID, &TemplateCreateRequest{Name: "空模板", Payload: &TemplatePayload{}})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.CreateTemplate(ctx, tid, member.ID, &TemplateCreateRequest{
		Name:    "坏优先级",
		Payload: &TemplatePayload{Tasks: []TemplateTaskSpec{{Name: "任务", Priority: "urgent"}}},
	})
	require.ErrorIs(t, err, ErrValidation)

	projectStart := time.Now().Add(24 * time.Hour)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "模板应用项目", StartDate: &projectStart})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)

	// draft 不可应用（409 语义）。
	_, err = svc.ApplyTemplate(ctx, tid, p.ID, member.ID, true, &TemplateApplyRequest{TemplateID: tpl.ID})
	require.ErrorIs(t, err, ErrTemplateNotActive)

	// 激活（乐观锁 v1 → v2），随后版本冲突可判。
	active := TemplateStatusActive
	v1 := 1
	updated, err := svc.UpdateTemplate(ctx, tid, tpl.ID, &TemplateUpdateRequest{Status: &active, Version: &v1})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Version)
	_, err = svc.UpdateTemplate(ctx, tid, tpl.ID, &TemplateUpdateRequest{Status: &active, Version: &v1})
	var conflict *TemplateVersionConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 2, conflict.CurrentVersion)

	// 应用：计数与锚点（锚点 = 项目开始日期）。
	resp, err := svc.ApplyTemplate(ctx, tid, p.ID, member.ID, true, &TemplateApplyRequest{TemplateID: tpl.ID})
	require.NoError(t, err)
	anchor := dateOnlyUTC(projectStart)
	assert.Equal(t, 1, resp.StageCount)
	assert.Equal(t, 3, resp.TaskCount)
	assert.Equal(t, 2, resp.ChecklistCount)
	assert.Equal(t, 1, resp.MilestoneCount)
	assert.Equal(t, anchor.Format("2006-01-02"), resp.AnchorDate.Format("2006-01-02"))

	// 生成结构：阶段 L1 → 任务 L2 → 检查项 L3；未分组任务直挂项目根。
	tasks, err := svc.ListTasks(ctx, tid, p.ID, member.ID, true, "", 0)
	require.NoError(t, err)
	require.Len(t, tasks, 6)
	byName := map[string]*taskRow{}
	for _, item := range tasks {
		byName[item.Name] = &taskRow{id: item.ID, parentID: item.ParentTaskID, planStart: item.PlanStart, planEnd: item.PlanEnd, priority: item.Priority}
	}
	stageTask, ok := byName["启动"]
	require.True(t, ok)
	assert.Equal(t, 0, stageTask.parentID)
	charter, ok := byName["项目章程"]
	require.True(t, ok)
	assert.Equal(t, stageTask.id, charter.parentID)
	assert.Equal(t, "high", charter.priority)
	assert.Equal(t, anchor.AddDate(0, 0, startOffset), charter.planStart)
	assert.Equal(t, anchor.AddDate(0, 0, startOffset+duration), charter.planEnd)
	checkItem, ok := byName["干系人登记"]
	require.True(t, ok)
	assert.Equal(t, charter.id, checkItem.parentID)
	ungrouped, ok := byName["未分组任务"]
	require.True(t, ok)
	assert.Equal(t, 0, ungrouped.parentID)

	// 里程碑日期 = 锚点 + offsetDays。
	milestones, err := svc.ListMilestones(ctx, tid, p.ID, member.ID, true, "")
	require.NoError(t, err)
	require.Len(t, milestones, 1)
	assert.Equal(t, anchor.AddDate(0, 0, 2).Format("2006-01-02"), milestones[0].DueDate.Format("2006-01-02"))

	// 项目详情透出应用信息。
	fresh, err := svc.Get(ctx, tid, p.ID)
	require.NoError(t, err)
	assert.Equal(t, tpl.ID, fresh.TemplateID)
	assert.False(t, fresh.TemplateAppliedAt.IsZero())

	// 重复应用：409 语义且不产生重复数据（幂等）。
	_, err = svc.ApplyTemplate(ctx, tid, p.ID, member.ID, true, &TemplateApplyRequest{TemplateID: tpl.ID})
	var applied *TemplateAlreadyAppliedError
	require.ErrorAs(t, err, &applied)
	assert.Equal(t, tpl.ID, applied.TemplateID)
	assert.Equal(t, "交付标准模板", applied.TemplateName)
	tasksAfter, err := svc.ListTasks(ctx, tid, p.ID, member.ID, true, "", 0)
	require.NoError(t, err)
	assert.Len(t, tasksAfter, 6)

	// 被应用模板不可删除（409 语义）；归档后可删。
	require.ErrorIs(t, svc.DeleteTemplate(ctx, tid, tpl.ID), ErrTemplateInUse)
	archived := TemplateStatusArchived
	_, err = svc.UpdateTemplate(ctx, tid, tpl.ID, &TemplateUpdateRequest{Status: &archived})
	require.NoError(t, err)

	// 终态项目拒绝应用（另一模板，避免"已应用"分支干扰）。
	tpl2, err := svc.CreateTemplate(ctx, tid, member.ID, &TemplateCreateRequest{
		Name:    "收尾模板",
		Status:  TemplateStatusActive,
		Payload: &TemplatePayload{Tasks: []TemplateTaskSpec{{Name: "复盘"}}},
	})
	require.NoError(t, err)
	_, err = svc.Transition(ctx, tid, p.ID, &StatusRequest{Action: ActionCancel, Reason: "范围取消"})
	require.NoError(t, err)
	_, err = svc.ApplyTemplate(ctx, tid, p.ID, member.ID, true, &TemplateApplyRequest{TemplateID: tpl2.ID})
	require.ErrorIs(t, err, ErrValidation)

	// 可见性：非成员（非管理）应用不可见项目 → 404 语义。
	outsider := seedUserWithMembership(t, client, tid, "to", "home", nil)
	p2, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "不可见项目"})
	require.NoError(t, err)
	_, err = svc.ApplyTemplate(ctx, tid, p2.ID, outsider.ID, false, &TemplateApplyRequest{TemplateID: tpl2.ID})
	require.ErrorIs(t, err, ErrNotFound)

	// 跨租户模板不可见 → 404 语义。
	otherTenant, err := client.Tenant.Create().
		SetName("模板他租户").
		SetCode(fmt.Sprintf("TT%d", time.Now().UnixNano()%1000000)).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	otherTpl, err := svc.CreateTemplate(ctx, otherTenant.ID, member.ID, &TemplateCreateRequest{
		Name:    "他租户模板",
		Status:  TemplateStatusActive,
		Payload: &TemplatePayload{Tasks: []TemplateTaskSpec{{Name: "任务"}}},
	})
	require.NoError(t, err)
	_, err = svc.ApplyTemplate(ctx, tid, p2.ID, member.ID, true, &TemplateApplyRequest{TemplateID: otherTpl.ID})
	require.ErrorIs(t, err, ErrTemplateNotFound)

	// 列表过滤 + 软删不可见。
	list, total, err := svc.ListTemplates(ctx, TemplateListFilter{TenantID: tid, Status: TemplateStatusActive})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, list, 1)
	assert.Equal(t, tpl2.ID, list[0].ID)
	require.NoError(t, svc.DeleteTemplate(ctx, tid, tpl2.ID))
	_, err = svc.GetTemplate(ctx, tid, tpl2.ID)
	require.ErrorIs(t, err, ErrTemplateNotFound)
}

// taskRow 测试用任务关键字段投影（避免直接引用 ent 生成结构做断言路径）。
type taskRow struct {
	id        int
	parentID  int
	planStart time.Time
	planEnd   time.Time
	priority  string
}

// TestHandlerTemplateEndpoints 模板端点：CRUD + 一键生成 + 404/409/400 映射（PE-11）。
func TestHandlerTemplateEndpoints(t *testing.T) {
	r, _, _ := newHandlerEnv(t)

	// 创建（草稿）→ 详情 → 列表。
	w, env := doJSON(t, r, http.MethodPost, "/projects/templates", map[string]any{
		"name": "接口模板",
		"payload": map[string]any{
			"milestones": []map[string]any{{"name": "验收通过", "offsetDays": 10}},
			"tasks":      []map[string]any{{"stage": "实施", "name": "部署", "checklist": []string{"备份", "回滚演练"}}},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)
	var tpl ProjectTemplateResponse
	require.NoError(t, json.Unmarshal(env.Data, &tpl))
	assert.Equal(t, TemplateStatusDraft, tpl.Status)
	assert.Equal(t, 1, tpl.TaskCount)
	assert.Equal(t, 2, tpl.ChecklistCount)

	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/templates/%d", tpl.ID), nil)
	require.NotNil(t, env.Data)
	var detail ProjectTemplateResponse
	require.NoError(t, json.Unmarshal(env.Data, &detail))
	require.NotNil(t, detail.Payload)
	require.Len(t, detail.Payload.Tasks, 1)

	_, env = doJSON(t, r, http.MethodGet, "/projects/templates", nil)
	var listEnv struct {
		Items            []ProjectTemplateResponse `json:"items"`
		Total            int                       `json:"total"`
		ProjectTemplates []ProjectTemplateResponse `json:"projectTemplates"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &listEnv))
	require.Len(t, listEnv.Items, 1)
	assert.Equal(t, 1, listEnv.Total)
	assert.Len(t, listEnv.ProjectTemplates, 1) // 存量领域别名（projectTemplates）
	assert.Nil(t, listEnv.Items[0].Payload)    // 列表省略定义体

	// 建项目。
	_, env = doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "接口应用项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 未激活应用 → 409；激活后应用 → 200；重复应用 → 409（幂等）。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/templates/apply", created.ID), map[string]any{"templateId": tpl.ID})
	assert.Equal(t, http.StatusConflict, w.Code)

	w, _ = doJSON(t, r, http.MethodPut, fmt.Sprintf("/projects/templates/%d", tpl.ID), map[string]any{"status": "active"})
	require.Equal(t, http.StatusOK, w.Code)

	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/templates/apply", created.ID), map[string]any{"templateId": tpl.ID})
	require.Equal(t, http.StatusOK, w.Code)
	var applied TemplateApplyResponse
	require.NoError(t, json.Unmarshal(env.Data, &applied))
	assert.Equal(t, 1, applied.StageCount)
	assert.Equal(t, 1, applied.TaskCount)
	assert.Equal(t, 2, applied.ChecklistCount)
	assert.Equal(t, 1, applied.MilestoneCount)

	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/templates/apply", created.ID), map[string]any{"templateId": tpl.ID})
	assert.Equal(t, http.StatusConflict, w.Code)

	// 被应用模板删除 → 409；模板不存在 → 404；空 payload 创建 → 400。
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/templates/%d", tpl.ID), nil)
	assert.Equal(t, http.StatusConflict, w.Code)
	w, _ = doJSON(t, r, http.MethodGet, "/projects/templates/999999", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	w, _ = doJSON(t, r, http.MethodPost, "/projects/templates", map[string]any{"name": "空模板", "payload": map[string]any{}})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 项目详情透出模板应用信息。
	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d", created.ID), nil)
	var detailProject ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &detailProject))
	require.NotNil(t, detailProject.TemplateID)
	assert.Equal(t, tpl.ID, *detailProject.TemplateID)

	// 错误码稳定性：应用冲突 body 带 templateId 明细。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/templates/apply", created.ID), map[string]any{"templateId": tpl.ID})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, common.ConflictCode, env.Code)
	assert.Contains(t, string(env.Data), `"templateId"`)
}