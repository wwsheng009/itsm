package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 富文本第二波（change / incident / problem）宿主注册守卫。
//
// 这三个 biz_type 的域内别名路由在 router/{change,incident,problem}_routes.go 静态声明，
// 实际解析依赖 defaultAttachmentHosts 注册表；缺登记会退化为
// ErrAttachmentHostNotFound（404/6101），故在此锁定「常量 ↔ 宿主」一一对应。

// TestDefaultAttachmentHostsCoverDeclaredBizTypes 守卫：对外声明的每个 biz_type 常量都必须有宿主。
func TestDefaultAttachmentHostsCoverDeclaredBizTypes(t *testing.T) {
	hosts := defaultAttachmentHosts()
	for _, bizType := range []string{
		AttachmentBizTypeTicket,
		AttachmentBizTypeTicketComment,
		AttachmentBizTypeKnowledgeArticle,
		AttachmentBizTypeServiceRequest,
		AttachmentBizTypeChange,
		AttachmentBizTypeIncident,
		AttachmentBizTypeProblem,
	} {
		assert.Contains(t, hosts, bizType, "biz_type %q 缺少宿主登记", bizType)
	}
}

// TestDefaultAttachmentHostsWave2 锁定三个新宿主的 Exists/References 语义：
// 跨租户不可见；引用保护覆盖本轮声明承载 HTML 的字段（change 三字段并联，incident/problem 单字段）。
func TestDefaultAttachmentHostsWave2(t *testing.T) {
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:attachment_hosts_wave2_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tn, user := seedHost(t, client, "wave2")
	other, _ := seedHost(t, client, "wave2-other")

	ch, err := client.Change.Create().
		SetTitle("变更富文本宿主").
		SetCreatedBy(user.ID).
		SetTenantID(tn.ID).
		SetDescription(`<p>实施要点见 <img data-attachment-id="101"></p>`).
		SetImplementationPlan("<p>步骤一</p>").
		SetRollbackPlan("<p>回滚见 <img data-attachment-id=202></p>").
		Save(ctx)
	require.NoError(t, err)

	inc, err := client.Incident.Create().
		SetTitle("事件富文本宿主").
		SetIncidentNumber("INC-WAVE2-1").
		SetReporterID(user.ID).
		SetTenantID(tn.ID).
		SetDescription(`<p>现场见 <img data-attachment-id='303'></p>`).
		Save(ctx)
	require.NoError(t, err)

	pb, err := client.Problem.Create().
		SetTitle("问题富文本宿主").
		SetCreatedBy(user.ID).
		SetTenantID(tn.ID).
		SetDescription(`<p>分析见 <img data-attachment-id="404"></p>`).
		Save(ctx)
	require.NoError(t, err)

	hosts := defaultAttachmentHosts()
	cases := []struct {
		name    string
		bizType string
		bizID   int
		refs    map[int]bool // attachmentID → 期望引用命中
	}{
		{name: "change", bizType: AttachmentBizTypeChange, bizID: ch.ID, refs: map[int]bool{101: true, 202: true, 999: false}},
		{name: "incident", bizType: AttachmentBizTypeIncident, bizID: inc.ID, refs: map[int]bool{303: true, 999: false}},
		{name: "problem", bizType: AttachmentBizTypeProblem, bizID: pb.ID, refs: map[int]bool{404: true, 999: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := hosts[tc.bizType]
			require.NotNil(t, host)

			exists, err := host.Exists(ctx, client, tn.ID, tc.bizID)
			require.NoError(t, err)
			assert.True(t, exists)

			// 跨租户 biz_id 不解析（同时验证 tenant 过滤）。
			exists, err = host.Exists(ctx, client, other.ID, tc.bizID)
			require.NoError(t, err)
			assert.False(t, exists)

			for attachmentID, want := range tc.refs {
				got, err := host.References(ctx, client, tn.ID, tc.bizID, attachmentID)
				require.NoError(t, err)
				assert.Equalf(t, want, got, "attachment %d 引用判定", attachmentID)
			}

			// 不存在的宿主 ID 视为无引用（不报错，删除保护不误伤）。
			got, err := host.References(ctx, client, tn.ID, 999999, 101)
			require.NoError(t, err)
			assert.False(t, got)
		})
	}
}
