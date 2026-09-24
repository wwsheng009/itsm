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

// 富文本第三波（release / cmdb_ci / known_error）宿主注册守卫。
//
// 这三个 biz_type 的域内别名路由分别在 router/release_routes.go、router/cmdb_routes.go
// 与 handlers/known_error/handler.go 静态声明，实际解析依赖 defaultAttachmentHosts
// 注册表；缺登记会退化为 ErrAttachmentHostNotFound（404/6101）。biz_type 常量与宿主的
// 一一对应由 attachment_hosts_wave2_test.go 的 TestDefaultAttachmentHostsCoverDeclaredBizTypes
// 统一守护，本文件锁定三个宿主的 Exists/References 语义。

// TestDefaultAttachmentHostsWave3 锁定三个新宿主的 Exists/References 语义：
// 跨租户不可见；引用保护覆盖本轮声明承载 HTML 的字段（release 四字段并联，
// cmdb_ci 单字段 description，known_error workaround+resolution）。
func TestDefaultAttachmentHostsWave3(t *testing.T) {
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:attachment_hosts_wave3_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tn, user := seedHost(t, client, "wave3")
	other, _ := seedHost(t, client, "wave3-other")

	rel, err := client.Release.Create().
		SetReleaseNumber("REL-WAVE3-1").
		SetTitle("发布富文本宿主").
		SetCreatedBy(user.ID).
		SetTenantID(tn.ID).
		SetDescription(`<p>发布目标见 <img data-attachment-id="101"></p>`).
		SetReleaseNotes("<p>说明</p>").
		SetRollbackProcedure(`<p>回滚见 <img data-attachment-id=202></p>`).
		SetValidationCriteria(`<p>验收见 <img data-attachment-id='203'></p>`).
		Save(ctx)
	require.NoError(t, err)

	// ConfigurationItem 的 ci_type_id 是外键：必须先落 ci_type 行，否则 sqlite(_fk=1) 报
	// FOREIGN KEY constraint failed（不能写死 ID=1）。
	ciType, err := client.CIType.Create().
		SetName("wave3-ci-type").
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)

	ci, err := client.ConfigurationItem.Create().
		SetName("CI 富文本宿主").
		SetCiTypeID(ciType.ID).
		SetCiType("wave3-ci-type").
		SetStatus("active").
		SetTenantID(tn.ID).
		SetDescription(`<p>用途见 <img data-attachment-id="303"></p>`).
		Save(ctx)
	require.NoError(t, err)

	ke, err := client.KnownError.Create().
		SetTitle("已知错误富文本宿主").
		SetCreatedBy(user.ID).
		SetTenantID(tn.ID).
		SetWorkaround(`<p>绕行见 <img data-attachment-id="404"></p>`).
		SetResolution(`<p>修复见 <img data-attachment-id="405"></p>`).
		SetDescription("<p>纯文本描述</p>").
		Save(ctx)
	require.NoError(t, err)

	hosts := defaultAttachmentHosts()
	cases := []struct {
		name    string
		bizType string
		bizID   int
		refs    map[int]bool // attachmentID → 期望引用命中
	}{
		{
			name:    "release",
			bizType: AttachmentBizTypeRelease,
			bizID:   rel.ID,
			refs:    map[int]bool{101: true, 202: true, 203: true, 999: false},
		},
		{
			name:    "cmdb_ci",
			bizType: AttachmentBizTypeCMDBci,
			bizID:   ci.ID,
			refs:    map[int]bool{303: true, 999: false},
		},
		{
			name:    "known_error",
			bizType: AttachmentBizTypeKnownError,
			bizID:   ke.ID,
			refs:    map[int]bool{404: true, 405: true, 999: false},
		},
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

// TestDefaultAttachmentHostsWave3NonRichTextFieldsNotProtected 锁定「未接入富文本的字段
// 不参与引用保护」的边界：known_error 的 description 保持纯文本，若其内容出现
// data-attachment-id 痕迹也不应命中（避免后续误扩字段时静默改变删除语义）。
func TestDefaultAttachmentHostsWave3NonRichTextFieldsNotProtected(t *testing.T) {
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:attachment_hosts_wave3_boundary_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tn, user := seedHost(t, client, "wave3-boundary")

	ke, err := client.KnownError.Create().
		SetTitle("边界用例").
		SetCreatedBy(user.ID).
		SetTenantID(tn.ID).
		SetDescription(`<p>历史痕迹 <img data-attachment-id="777"></p>`).
		Save(ctx)
	require.NoError(t, err)

	host := defaultAttachmentHosts()[AttachmentBizTypeKnownError]
	require.NotNil(t, host)

	got, err := host.References(ctx, client, tn.ID, ke.ID, 777)
	require.NoError(t, err)
	assert.False(t, got, "known_error.description 未接入富文本，不应参与引用保护")
}
