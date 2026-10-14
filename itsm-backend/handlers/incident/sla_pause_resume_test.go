package incident

import (
	"context"
	"testing"
	"time"

	"itsm-backend/ent/enttest"
	"itsm-backend/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// P1-3 接线：incident 暂停/恢复委托 SLAMonitorService 真实计时。
// 未装配时 fail-closed（不触碰数据）；装配后暂停记录、恢复追加 deadline。
func TestIncidentService_PauseResumeSLA_Delegation(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:inc_pause_resume?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	tenant := client.Tenant.Create().
		SetName("PauseTenant").SetCode("pause-tenant").SetDomain("pause.test").SaveX(ctx)
	user := client.User.Create().
		SetUsername("pause-user").SetName("pause").SetEmail("pause@pause.test").
		SetPasswordHash("hash").SetTenantID(tenant.ID).SaveX(ctx)

	inc := client.Incident.Create().
		SetTitle("Pause Me").
		SetIncidentNumber("INC-PR-1").
		SetStatus("open").
		SetReporterID(user.ID).
		SetTenantID(tenant.ID).
		SetSLAResponseDeadline(time.Now().Add(2 * time.Hour)).
		SetSLAResolutionDeadline(time.Now().Add(8 * time.Hour)).
		SaveX(ctx)

	svc := NewService(NewEntRepository(client), nil, nil, nil, nil, logger)

	// 未装配：fail-closed（503 语义），且不触碰数据。
	err := svc.PauseSLA(ctx, inc.ID, tenant.ID)
	require.Error(t, err)
	assert.NotEqual(t, "paused", client.Incident.GetX(ctx, inc.ID).SLAStatus)

	// 装配后：真实暂停/恢复。
	svc.SetSLAMonitorService(service.NewSLAMonitorService(client, logger))

	require.NoError(t, svc.PauseSLA(ctx, inc.ID, tenant.ID))
	assert.Equal(t, "paused", client.Incident.GetX(ctx, inc.ID).SLAStatus)

	// 已暂停再暂停 → 业务错误（ParamError 包装）。
	require.Error(t, svc.PauseSLA(ctx, inc.ID, tenant.ID))

	past := time.Now().Add(-20 * time.Minute)
	client.Incident.UpdateOneID(inc.ID).SetSLAPausedAt(past).ExecX(ctx)
	before := client.Incident.GetX(ctx, inc.ID)

	require.NoError(t, svc.ResumeSLA(ctx, inc.ID, tenant.ID))
	after := client.Incident.GetX(ctx, inc.ID)
	assert.Equal(t, "active", after.SLAStatus)
	delta := after.SLAResponseDeadline.Sub(before.SLAResponseDeadline)
	assert.GreaterOrEqual(t, delta, 19*time.Minute)
	assert.LessOrEqual(t, delta, 21*time.Minute)
}
