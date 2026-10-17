package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/operationalcommand"
	"itsm-backend/internal/commandbus"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// milestoneDueFixture 里程碑到期扫描夹具：租户 + PM/干系人/普通成员 + 活跃项目。
func milestoneDueFixture(t *testing.T) (*ent.Client, context.Context, *ent.Tenant, *ent.User, *ent.User, *ent.User, *ent.Project) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "-"))
	client := enttest.Open(t, dialect.SQLite, dsn)
	ctx := context.Background()

	tenantEntity, err := client.Tenant.Create().SetName("Milestone Tenant").SetCode("milestone-tenant").
		SetDomain("milestone.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	mkUser := func(username string) *ent.User {
		u, err := client.User.Create().SetUsername(username).SetEmail(username + "@example.com").
			SetName(username).SetPasswordHash("hash").SetRole("agent").SetActive(true).
			SetTenantID(tenantEntity.ID).Save(ctx)
		require.NoError(t, err)
		return u
	}
	manager := mkUser("milestone-pm")
	stakeholder := mkUser("milestone-stakeholder")
	member := mkUser("milestone-member")

	projectEntity, err := client.Project.Create().SetTenantID(tenantEntity.ID).
		SetName("交付项目").SetCode("PRJ-MILESTONE").SetStatus("active").
		SetManagerID(manager.ID).Save(ctx)
	require.NoError(t, err)
	for _, seed := range []struct {
		userID int
		role   string
	}{
		{userID: manager.ID, role: "pm"},
		{userID: stakeholder.ID, role: "stakeholder"},
		{userID: member.ID, role: "member"},
	} {
		_, err := client.ProjectMember.Create().SetTenantID(tenantEntity.ID).
			SetProjectID(projectEntity.ID).SetUserID(seed.userID).
			SetProjectRole(seed.role).SetStatus("active").Save(ctx)
		require.NoError(t, err)
	}
	return client, ctx, tenantEntity, manager, stakeholder, member, projectEntity
}

func newMilestoneDueScannerForTest(t *testing.T, client *ent.Client, now time.Time) *ProjectMilestoneDueScanner {
	t.Helper()
	scanner := NewProjectMilestoneDueScanner(client, zap.NewNop().Sugar())
	scanner.now = func() time.Time { return now }
	return scanner
}

func countDeliverCommands(t *testing.T, client *ent.Client, ctx context.Context, tenantID int) []*ent.OperationalCommand {
	t.Helper()
	commands, err := client.OperationalCommand.Query().
		Where(
			operationalcommand.TenantID(tenantID),
			operationalcommand.CommandTypeEQ(commandbus.CommandDeliverNotification),
		).
		Order(ent.Asc(operationalcommand.FieldID)).
		All(ctx)
	require.NoError(t, err)
	return commands
}

func TestProjectMilestoneDueScannerEnqueuesForPMAndStakeholders(t *testing.T) {
	client, ctx, tenantEntity, manager, stakeholder, member, projectEntity := milestoneDueFixture(t)
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	dueSoon, err := client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("UAT 完成").
		SetDueDate(now.Add(48 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)
	overdue, err := client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("上线验收").
		SetDueDate(now.Add(-24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)
	// 已达成的里程碑不再提醒。
	_, err = client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("已完成里程碑").
		SetDueDate(now.Add(-2 * time.Hour)).SetStatus("reached").Save(ctx)
	require.NoError(t, err)
	// 窗口外（10 天后）不提醒。
	_, err = client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("远期里程碑").
		SetDueDate(now.Add(10 * 24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	scanner := newMilestoneDueScannerForTest(t, client, now)
	enqueued, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	// 2 个里程碑 × 收件人（PM + 干系人）；普通成员不收件。
	assert.Equal(t, 4, enqueued)

	commands := countDeliverCommands(t, client, ctx, tenantEntity.ID)
	require.Len(t, commands, 4)
	recipients := make(map[int]int, 4)
	contents := make([]string, 0, 4)
	for _, command := range commands {
		assert.Equal(t, "project_milestone", command.AggregateType)
		assert.Equal(t, ProjectMilestoneDueNotificationType, command.Payload["type"])
		assert.Equal(t, "in_app", command.Payload["channel"])
		recipient := int(command.Payload["recipientId"].(float64))
		recipients[recipient]++
		contents = append(contents, command.Payload["content"].(string))
	}
	assert.Equal(t, 2, recipients[manager.ID])
	assert.Equal(t, 2, recipients[stakeholder.ID])
	assert.Zero(t, recipients[member.ID], "普通 member 角色不接收里程碑到期通知")
	assert.Equal(t, 4, recipients[manager.ID]+recipients[stakeholder.ID])

	aggregateIDs := make(map[int]bool, 2)
	for _, command := range commands {
		aggregateIDs[command.AggregateID] = true
	}
	assert.True(t, aggregateIDs[dueSoon.ID])
	assert.True(t, aggregateIDs[overdue.ID])
	assert.Contains(t, strings.Join(contents, "\n"), "逾期")
	assert.Contains(t, strings.Join(contents, "\n"), "将于")

	// 入箱投递审计（source=job）。
	auditCount, err := client.AuditLog.Query().
		Where(auditlog.ActionEQ("project.milestone_due_scan")).
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, auditCount)
}

func TestProjectMilestoneDueScannerIsIdempotentAcrossScans(t *testing.T) {
	client, ctx, tenantEntity, _, _, _, projectEntity := milestoneDueFixture(t)
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	_, err := client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("临期里程碑").
		SetDueDate(now.Add(12 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	scanner := newMilestoneDueScannerForTest(t, client, now)
	first, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, first)

	// 重复扫描（同一触发）：幂等键收敛，不再入箱。
	second, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	assert.Zero(t, second)
	assert.Len(t, countDeliverCommands(t, client, ctx, tenantEntity.ID), 2)
}

func TestProjectMilestoneDueScannerSkipsTerminalProjectsAndOtherTenants(t *testing.T) {
	client, ctx, tenantEntity, _, _, _, projectEntity := milestoneDueFixture(t)
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

	// 终态项目：不提醒。
	completed, err := client.Project.Create().SetTenantID(tenantEntity.ID).
		SetName("已结项项目").SetCode("PRJ-DONE").SetStatus("completed").Save(ctx)
	require.NoError(t, err)
	for _, projectID := range []int{completed.ID} {
		_, err := client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
			SetProjectID(projectID).SetName("终态项目里程碑").
			SetDueDate(now.Add(6 * time.Hour)).SetStatus("planned").Save(ctx)
		require.NoError(t, err)
	}
	// 跨租户：其他租户里程碑不被误扫。
	otherTenant, err := client.Tenant.Create().SetName("Other Tenant").SetCode("other-tenant").
		SetDomain("other.example.com").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	otherProject, err := client.Project.Create().SetTenantID(otherTenant.ID).
		SetName("Other Project").SetCode("PRJ-OTHER").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectMilestone.Create().SetTenantID(otherTenant.ID).
		SetProjectID(otherProject.ID).SetName("Other Milestone").
		SetDueDate(now.Add(time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	// 本租户活跃项目放一个临期里程碑以证明扫描本身有效。
	_, err = client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("本租户临期里程碑").
		SetDueDate(now.Add(2 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	scanner := newMilestoneDueScannerForTest(t, client, now)
	enqueued, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, enqueued, "仅本租户活跃项目的里程碑入箱（PM + 干系人各一条）")
	commands := countDeliverCommands(t, client, ctx, tenantEntity.ID)
	require.Len(t, commands, 2)
	assert.Contains(t, commands[0].Payload["content"].(string), "本租户临期里程碑")
	assert.Empty(t, countDeliverCommands(t, client, ctx, otherTenant.ID))
}

func TestProjectMilestoneDueScannerInvalidTenant(t *testing.T) {
	client, ctx, _, _, _, _, _ := milestoneDueFixture(t)
	scanner := NewProjectMilestoneDueScanner(client, zap.NewNop().Sugar())
	_, err := scanner.ScanDueMilestones(ctx, 0)
	require.Error(t, err)
}

// TestProjectMilestoneDueScannerIncludesOwnerTeamManager PE-17：归口团队负责人进入通知候选；
// 团队软删后自动降级（不再计入）。
func TestProjectMilestoneDueScannerIncludesOwnerTeamManager(t *testing.T) {
	client, ctx, tenantEntity, manager, stakeholder, _, projectEntity := milestoneDueFixture(t)
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

	teamManager, err := client.User.Create().SetUsername("milestone-team-lead").
		SetEmail("milestone-team-lead@example.com").SetName("team-lead").
		SetPasswordHash("hash").SetRole("agent").SetActive(true).
		SetTenantID(tenantEntity.ID).Save(ctx)
	require.NoError(t, err)
	teamEntity, err := client.Team.Create().SetTenantID(tenantEntity.ID).
		SetName("交付一组").SetCode("TEAM-D1").SetStatus("active").
		SetManagerID(teamManager.ID).Save(ctx)
	require.NoError(t, err)
	_, err = client.Project.UpdateOneID(projectEntity.ID).SetOwnerTeamID(teamEntity.ID).Save(ctx)
	require.NoError(t, err)

	_, err = client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("归口里程碑").
		SetDueDate(now.Add(24 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	scanner := newMilestoneDueScannerForTest(t, client, now)
	enqueued, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	// PM + 干系人 + 归口团队负责人（普通 member 不收件）。
	assert.Equal(t, 3, enqueued)
	commands := countDeliverCommands(t, client, ctx, tenantEntity.ID)
	require.Len(t, commands, 3)
	recipients := make(map[int]bool, 3)
	for _, command := range commands {
		recipients[int(command.Payload["recipientId"].(float64))] = true
	}
	assert.True(t, recipients[manager.ID])
	assert.True(t, recipients[stakeholder.ID])
	assert.True(t, recipients[teamManager.ID], "归口团队负责人应进入通知候选")

	// 团队软删 → 后续里程碑不再通知团队负责人（自动降级）。
	_, err = client.Team.UpdateOneID(teamEntity.ID).SetDeletedAt(now).Save(ctx)
	require.NoError(t, err)
	_, err = client.ProjectMilestone.Create().SetTenantID(tenantEntity.ID).
		SetProjectID(projectEntity.ID).SetName("软删团队里程碑").
		SetDueDate(now.Add(30 * time.Hour)).SetStatus("planned").Save(ctx)
	require.NoError(t, err)

	second, err := scanner.ScanDueMilestones(ctx, tenantEntity.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, second, "软删团队：仅 PM + 干系人")
	assert.Len(t, countDeliverCommands(t, client, ctx, tenantEntity.ID), 5)
}
