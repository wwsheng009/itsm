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
	"itsm-backend/ent/usertenantmembership"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedUserWithMembership 建用户 + 目标租户 membership（source/expires 可控）。
func seedUserWithMembership(t *testing.T, client *ent.Client, tenantID int, tag, source string, expires *time.Time) *ent.User {
	t.Helper()
	ctx := context.Background()
	u, err := client.User.Create().
		SetUsername(fmt.Sprintf("pm-%s-%d", tag, tenantID)).
		SetEmail(fmt.Sprintf("pm-%s-%d@example.com", tag, tenantID)).
		SetName("成员-" + tag).
		SetPasswordHash("x").
		SetTenantID(tenantID).
		Save(ctx)
	require.NoError(t, err)

	builder := client.UserTenantMembership.Create().
		SetUserID(u.ID).
		SetTenantID(tenantID).
		SetSource(usertenantmembership.Source(source)).
		SetAccountKind(usertenantmembership.AccountKindCustomer).
		SetStatus(usertenantmembership.StatusActive)
	if expires != nil {
		builder = builder.SetExpiresAt(*expires)
	}
	_, err = builder.Save(ctx)
	require.NoError(t, err)
	return u
}

// TestMemberWorkflow 成员增删改查 + 派生标签 + 前置校验 + PM 移除保护。
func TestMemberWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	internal := seedUserWithMembership(t, client, tid, "internal", "home", nil)
	expires := time.Now().Add(5 * 24 * time.Hour)
	external := seedUserWithMembership(t, client, tid, "external", "invite", &expires)

	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "成员项目"})
	require.NoError(t, err)

	// 候选：应同时含两人（无在册成员）。
	candidates, err := svc.MemberCandidates(ctx, tid, p.ID, "", 50)
	require.NoError(t, err)
	assert.Len(t, candidates, 2)

	// 添加内部成员 → memberOrigin=internal。
	view, err := svc.AddMember(ctx, tid, p.ID, internal.ID, &MemberAddRequest{UserID: internal.ID, ProjectRole: "member"})
	require.NoError(t, err)
	assert.Equal(t, OriginInternal, view.MemberOrigin)
	_, err = svc.AddMember(ctx, tid, p.ID, internal.ID, &MemberAddRequest{UserID: internal.ID})
	require.ErrorIs(t, err, ErrMemberExists)

	// 无活跃 membership 的用户拒绝。
	orphan, err := client.User.Create().
		SetUsername(fmt.Sprintf("orphan-%d", tid)).
		SetEmail(fmt.Sprintf("orphan-%d@example.com", tid)).
		SetName("无身份用户").
		SetPasswordHash("x").
		SetTenantID(tid).
		Save(ctx)
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, orphan.ID, &MemberAddRequest{UserID: orphan.ID})
	require.ErrorIs(t, err, ErrMemberNotInTenant)

	// 外部邀请成员 → memberOrigin=external + expiresAt 透出。
	viewExternal, err := svc.AddMember(ctx, tid, p.ID, external.ID, &MemberAddRequest{UserID: external.ID, ProjectRole: "stakeholder"})
	require.NoError(t, err)
	assert.Equal(t, OriginExternal, viewExternal.MemberOrigin)
	require.NotNil(t, viewExternal.ExpiresAt)

	// 候选排除在册成员。
	candidates, err = svc.MemberCandidates(ctx, tid, p.ID, "", 50)
	require.NoError(t, err)
	assert.Empty(t, candidates)

	// 成员列表（管理视角）。
	views, err := svc.ListMembers(ctx, tid, p.ID, internal.ID, true)
	require.NoError(t, err)
	assert.Len(t, views, 2)

	// 调角色。
	updated, err := svc.UpdateMemberRole(ctx, tid, p.ID, internal.ID, &MemberRoleUpdateRequest{ProjectRole: "pm"})
	require.NoError(t, err)
	assert.Equal(t, "pm", updated.ProjectRole)

	// PM 移除保护（projects.manager_id 唯一权威）。
	managerID := internal.ID
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{ManagerID: &managerID})
	require.NoError(t, err)
	require.ErrorIs(t, svc.RemoveMember(ctx, tid, p.ID, internal.ID), ErrManagerCannotBeRemoved)

	// 非 PM 可移除；再次添加复活历史行。
	require.NoError(t, svc.RemoveMember(ctx, tid, p.ID, external.ID))
	revived, err := svc.AddMember(ctx, tid, p.ID, external.ID, &MemberAddRequest{UserID: external.ID, ProjectRole: "member"})
	require.NoError(t, err)
	assert.Equal(t, "active", revived.Status)
	assert.Equal(t, "member", revived.ProjectRole)
}

// TestVisibilityPredicate 可见性：非管理仅成员项目；管理全量；不可见返回 404。
func TestVisibilityPredicate(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	viewer := seedUserWithMembership(t, client, tid, "viewer", "home", nil)
	p1, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "参与项目"})
	require.NoError(t, err)
	p2, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "无关项目"})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p1.ID, viewer.ID, &MemberAddRequest{UserID: viewer.ID})
	require.NoError(t, err)

	items, total, err := svc.ListForViewer(ctx, ListFilter{TenantID: tid, Limit: 20}, viewer.ID, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, p1.ID, items[0].ID)

	_, err = svc.GetForViewer(ctx, tid, p2.ID, viewer.ID, false)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.GetForViewer(ctx, tid, p1.ID, viewer.ID, false)
	require.NoError(t, err)

	_, total, err = svc.ListForViewer(ctx, ListFilter{TenantID: tid, Limit: 20}, 0, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
}

// TestHandlerMemberEndpoints 成员端点：添加/重复 409/候选/列表/移除。
func TestHandlerMemberEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	user := seedUserWithMembership(t, client, tid, "api", "home", nil)

	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "成员接口项目"})
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	w, env := doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{
		"userId": user.ID, "projectRole": "member",
	})
	require.Equal(t, http.StatusOK, w.Code)
	var member MemberView
	require.NoError(t, json.Unmarshal(env.Data, &member))
	assert.Equal(t, OriginInternal, member.MemberOrigin)

	// 重复添加 → 409。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/members", created.ID), map[string]any{"userId": user.ID})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, common.ConflictCode, env.Code)

	// 候选（排除在册成员；测试管理员有 membership 且不在册 → 1 条）。
	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/member-candidates", created.ID), nil)
	var candidates []MemberCandidate
	require.NoError(t, json.Unmarshal(env.Data, &candidates))
	require.Len(t, candidates, 1)
	assert.NotEqual(t, user.ID, candidates[0].UserID)

	// 列表。
	_, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/members", created.ID), nil)
	var list []MemberView
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list, 1)

	// 移除。
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/members/%d", created.ID, user.ID), nil)
	assert.Equal(t, http.StatusOK, w.Code)
}
