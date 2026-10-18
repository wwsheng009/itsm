package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent/usertenantmembership"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PE-28 写入完整性校验（D-60~D-63）；权威设计：
// docs/project-management/plan/p3-write-integrity-design.md。

// pe28Time 构造 UTC 日期指针（创建/更新入参）。
func pe28Time(t *testing.T, year int, month time.Month, day int) *time.Time {
	t.Helper()
	v := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &v
}

// TestManagerWriteValidation D-60：负责人必须为本租户活跃 membership（软删/过期/跨租户均拒绝）。
func TestManagerWriteValidation(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	ok := seedUserWithMembership(t, client, tid, "pe28-ok", "home", nil)
	mid := ok.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "负责人校验", ManagerID: &mid})
	require.NoError(t, err)
	assert.Equal(t, ok.ID, p.ManagerID)

	// 非成员用户（无 membership）→ 拒绝（创建与更新同口径）。
	outsider, err := client.User.Create().
		SetUsername("pe28-outsider").SetEmail("pe28-outsider@example.com").
		SetName("外部用户").SetPasswordHash("x").SetTenantID(tid).
		Save(ctx)
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "悬空负责人", ManagerID: &outsider.ID})
	require.ErrorIs(t, err, ErrManagerUnavailable)
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{ManagerID: &outsider.ID})
	require.ErrorIs(t, err, ErrManagerUnavailable)

	// 过期 membership → 拒绝。
	expired := time.Now().Add(-24 * time.Hour)
	stale := seedUserWithMembership(t, client, tid, "pe28-stale", "invite", &expired)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "过期负责人", ManagerID: &stale.ID})
	require.ErrorIs(t, err, ErrManagerUnavailable)

	// 软删 membership → 拒绝。
	removed := seedUserWithMembership(t, client, tid, "pe28-removed", "home", nil)
	_, err = client.UserTenantMembership.Update().
		Where(usertenantmembership.UserIDEQ(removed.ID)).
		SetDeletedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "软删负责人", ManagerID: &removed.ID})
	require.ErrorIs(t, err, ErrManagerUnavailable)

	// 跨租户（他租户成员，目标租户无 membership）→ 拒绝。
	other, err := client.Tenant.Create().SetName("他租户").SetCode("PE28-OT").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	foreign := seedUserWithMembership(t, client, other.ID, "pe28-foreign", "home", nil)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "跨租户负责人", ManagerID: &foreign.ID})
	require.ErrorIs(t, err, ErrManagerUnavailable)

	// 更新：合法成员通过；0 沿用既有 optionalInt 语义 = 不修改（不触发校验）。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{ManagerID: &mid})
	require.NoError(t, err)
	zero := 0
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{ManagerID: &zero})
	require.NoError(t, err)
	assert.Equal(t, mid, updated.ManagerID, "0 = 不修改（optionalInt 语义）")
}

// TestDepartmentWriteValidation D-61：部门必须同租户且未软删。
func TestDepartmentWriteValidation(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()

	dept, err := client.Department.Create().SetName("交付部").SetCode("PE28-DEPT").SetTenantID(tid).Save(ctx)
	require.NoError(t, err)
	did := dept.ID
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "部门校验", DepartmentID: &did})
	require.NoError(t, err)
	assert.Equal(t, dept.ID, p.DepartmentID)

	// 不存在 / 软删 / 跨租户 → 拒绝。
	missing := 99999
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "悬空部门", DepartmentID: &missing})
	require.ErrorIs(t, err, ErrDepartmentUnavailable)

	obsolete, err := client.Department.Create().
		SetName("旧部门").SetCode("PE28-OLD").SetTenantID(tid).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "软删部门", DepartmentID: &obsolete.ID})
	require.ErrorIs(t, err, ErrDepartmentUnavailable)

	other, err := client.Tenant.Create().SetName("部门他租户").SetCode("PE28-DT").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	foreign, err := client.Department.Create().SetName("外租部门").SetCode("PE28-FD").SetTenantID(other.ID).Save(ctx)
	require.NoError(t, err)
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "跨租部门", DepartmentID: &foreign.ID})
	require.ErrorIs(t, err, ErrDepartmentUnavailable)

	// 更新：非法引用拒绝；合法通过；0 沿用既有 optionalInt 语义 = 不修改。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{DepartmentID: &missing})
	require.ErrorIs(t, err, ErrDepartmentUnavailable)
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{DepartmentID: &did})
	require.NoError(t, err)
	zero := 0
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{DepartmentID: &zero})
	require.NoError(t, err)
	assert.Equal(t, did, updated.DepartmentID, "0 = 不修改（optionalInt 语义）")
}

// TestProjectDateRangeValidation D-62：endDate 不得早于 startDate（创建两端 / 更新合并存量）。
func TestProjectDateRangeValidation(t *testing.T) {
	svc, _, tid := newTestEnv(t)
	ctx := context.Background()

	// 创建：逆序拒绝；同日通过。
	_, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "逆序日期",
		StartDate: pe28Time(t, 2026, time.April, 1), EndDate: pe28Time(t, 2026, time.March, 1),
	})
	require.ErrorIs(t, err, ErrValidation)

	p, err := svc.Create(ctx, &CreateRequest{
		TenantID: tid, Name: "同日日期",
		StartDate: pe28Time(t, 2026, time.March, 1), EndDate: pe28Time(t, 2026, time.March, 1),
	})
	require.NoError(t, err)

	// 更新：单端与存量合并判定（存量 3/1~3/1）。
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{StartDate: pe28Time(t, 2026, time.April, 1)})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.Update(ctx, tid, p.ID, &UpdateRequest{EndDate: pe28Time(t, 2026, time.February, 1)})
	require.ErrorIs(t, err, ErrValidation)
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{EndDate: pe28Time(t, 2026, time.April, 1)})
	require.NoError(t, err)
	assert.True(t, updated.EndDate.Equal(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)), "endDate got %s", updated.EndDate)

	// 单端存在（另一端未设置）不阻断。
	soloStart, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "仅开始", StartDate: pe28Time(t, 2026, time.March, 1)})
	require.NoError(t, err)
	_, err = svc.Update(ctx, tid, soloStart.ID, &UpdateRequest{EndDate: pe28Time(t, 2026, time.April, 1)})
	require.NoError(t, err)

	soloEnd, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "仅结束", EndDate: pe28Time(t, 2026, time.April, 1)})
	require.NoError(t, err)
	_, err = svc.Update(ctx, tid, soloEnd.ID, &UpdateRequest{StartDate: pe28Time(t, 2026, time.February, 1)})
	require.NoError(t, err)
	_, err = svc.Update(ctx, tid, soloEnd.ID, &UpdateRequest{StartDate: pe28Time(t, 2026, time.May, 1)})
	require.ErrorIs(t, err, ErrValidation)
}

// TestBudgetCurrencyNormalization D-63：币种 trim + 大写归一 + ^[A-Z]{2,8}$ 字符集校验。
func TestBudgetCurrencyNormalization(t *testing.T) {
	svc, _, tid := newTestEnv(t)
	ctx := context.Background()

	amount := 120000.0
	lower := "usd"
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "币种归一", BudgetAmount: &amount, BudgetCurrency: &lower})
	require.NoError(t, err)
	require.NotNil(t, p.BudgetCurrency)
	assert.Equal(t, "USD", *p.BudgetCurrency)

	// 2 字母合法；更新路径同样归一再校验。
	two := "cn"
	_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "两字母币种", BudgetAmount: &amount, BudgetCurrency: &two})
	require.NoError(t, err)
	spaced := " eur "
	updated, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: &amount, BudgetCurrency: &spaced})
	require.NoError(t, err)
	require.NotNil(t, updated.BudgetCurrency)
	assert.Equal(t, "EUR", *updated.BudgetCurrency)

	// 非法字符集 / 过短 → 400。
	for _, bad := range []string{"c", "CNY$", "1A", "人民币"} {
		cur := bad
		_, err = svc.Create(ctx, &CreateRequest{TenantID: tid, Name: "非法币种-" + bad, BudgetAmount: &amount, BudgetCurrency: &cur})
		require.ErrorIs(t, err, ErrValidation, "currency %q should be rejected", bad)
	}

	// 清空回归（D-33）：amount 0 + 空串 = 未设置。
	zeroAmount, empty := 0.0, ""
	cleared, err := svc.Update(ctx, tid, p.ID, &UpdateRequest{BudgetAmount: &zeroAmount, BudgetCurrency: &empty})
	require.NoError(t, err)
	assert.Nil(t, cleared.BudgetCurrency)
}

// TestWriteValidationEndpoints 端点级：四类 400 的 HTTP 映射 + 合法写入（含币种归一）。
func TestWriteValidationEndpoints(t *testing.T) {
	r, client, _ := newHandlerEnv(t)
	ctx := context.Background()
	admin, err := client.User.Query().Only(ctx)
	require.NoError(t, err)

	// 负责人不可用 → 400。
	_, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "悬空负责人", "managerId": 999999})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "项目经理不可用")

	// 部门不可用 → 400。
	_, env = doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "悬空部门", "departmentId": 999999})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "所属部门不可用")

	// 日期逆序 → 400。
	_, env = doJSON(t, r, http.MethodPost, "/projects", map[string]any{
		"name": "逆序日期", "startDate": "2026-05-01T00:00:00Z", "endDate": "2026-04-01T00:00:00Z",
	})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "endDate must not be before startDate")

	// 币种非法 → 400。
	_, env = doJSON(t, r, http.MethodPost, "/projects", map[string]any{
		"name": "非法币种", "budgetAmount": 100, "budgetCurrency": "bad$",
	})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "budget currency")

	// 合法写入：admin 为本租户活跃成员 + 币种小写（HTTP 路径归一）→ 成功。
	_, env = doJSON(t, r, http.MethodPost, "/projects", map[string]any{
		"name": "合法项目", "managerId": admin.ID,
		"startDate": "2026-03-01T00:00:00Z", "endDate": "2026-04-01T00:00:00Z",
		"budgetAmount": 120000, "budgetCurrency": "usd",
	})
	require.Equal(t, common.SuccessCode, env.Code)
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))
	require.NotNil(t, created.BudgetCurrency)
	assert.Equal(t, "USD", *created.BudgetCurrency, "币种应大写归一")

	path := fmt.Sprintf("/projects/%d", created.ID)

	// 更新：合并存量日期校验 → 400（新 start 晚于存量 end）。
	_, env = doJSON(t, r, http.MethodPut, path, map[string]any{"startDate": "2026-05-01T00:00:00Z"})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "endDate must not be before startDate")

	// 更新：负责人不可用 → 400。
	_, env = doJSON(t, r, http.MethodPut, path, map[string]any{"managerId": 999999})
	assert.Equal(t, common.ParamErrorCode, env.Code)
	assert.Contains(t, env.Message, "项目经理不可用")

	// 更新：合法日期扩展 → 成功。
	_, env = doJSON(t, r, http.MethodPut, path, map[string]any{"endDate": "2026-06-01T00:00:00Z"})
	assert.Equal(t, common.SuccessCode, env.Code)
}
