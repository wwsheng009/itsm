package service

import (
	"context"
	"testing"

	"itsm-backend/dto"
	"itsm-backend/ent/enttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// P2 富文本：创建/更新链路的服务端清洗与双写（description + description_html）。
func TestTicketService_RichTextDoubleWrite(t *testing.T) {
	client := enttest.Open(t, "sqlite3", testDSN())
	defer client.Close()

	logger := zaptest.NewLogger(t).Sugar()
	svc := NewTicketServiceForTest(client, logger)
	ctx := context.Background()

	tenant, err := client.Tenant.Create().
		SetName("Rich Text Tenant").
		SetCode("richtext").
		SetDomain("richtext.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetUsername("richtext_user").
		SetEmail("richtext@example.com").
		SetName("Rich Text User").
		SetPasswordHash("hash").
		SetRole("end_user").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	rawHTML := `<p>正文 <strong>加粗</strong></p>` +
		`<script>alert(1)</script>` +
		`<p onclick="evil()">带事件</p>` +
		`<a href="javascript:alert(1)">坏链接</a>` +
		`<a href="https://example.com">好链接</a>` +
		`<img src="data:image/png;base64,AAAA">` +
		`<img src="/uploads/a.png" alt="附件图">`

	created, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title:           "富文本工单",
		Description:     "纯文本描述",
		DescriptionHTML: rawHTML,
		Priority:        "medium",
		Type:            "incident",
		RequesterID:     user.ID,
	}, tenant.ID)
	require.NoError(t, err)

	// 双写：纯文本保持不变；HTML 列存服务端清洗结果；格式标记 html。
	assert.Equal(t, "纯文本描述", created.Description)
	assert.Equal(t, "html", created.DescriptionFormat)
	require.NotEmpty(t, created.DescriptionHTML)
	assert.Contains(t, created.DescriptionHTML, "<strong>加粗</strong>")
	assert.Contains(t, created.DescriptionHTML, `src="/uploads/a.png"`)
	assert.NotContains(t, created.DescriptionHTML, "<script")
	assert.NotContains(t, created.DescriptionHTML, "javascript:")
	assert.NotContains(t, created.DescriptionHTML, "onclick")
	assert.NotContains(t, created.DescriptionHTML, "data:image")

	// 落库可回读（repository 层 ent 字段读写接线正确）。
	persisted, err := svc.GetTicket(ctx, created.ID, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, created.DescriptionHTML, persisted.DescriptionHTML)
	assert.Equal(t, "html", persisted.DescriptionFormat)

	// 更新未携带 descriptionHtml：部分更新语义，不得清空已有 HTML。
	updated, err := svc.UpdateTicket(ctx, created.ID, &dto.UpdateTicketRequest{
		Title:   "富文本工单-改",
		Version: created.Version,
	}, tenant.ID, user.ID, "admin")
	require.NoError(t, err)
	assert.Equal(t, created.DescriptionHTML, updated.DescriptionHTML)
	assert.Equal(t, "html", updated.DescriptionFormat)

	// 更新携带 descriptionHtml：重新清洗并覆盖，纯文本同步更新。
	updated2, err := svc.UpdateTicket(ctx, created.ID, &dto.UpdateTicketRequest{
		Description:     "新的纯文本描述",
		DescriptionHTML: `<p>新正文</p><img src="data:image/png;base64,BBBB">`,
		Version:         updated.Version,
	}, tenant.ID, user.ID, "admin")
	require.NoError(t, err)
	assert.Equal(t, "新的纯文本描述", updated2.Description)
	assert.Equal(t, "html", updated2.DescriptionFormat)
	assert.Contains(t, updated2.DescriptionHTML, "新正文")
	assert.NotContains(t, updated2.DescriptionHTML, "data:image")

	// 未携带富文本的旧行为：仍为 plain（DB 默认），不写入 HTML。
	plain, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{
		Title:       "纯文本工单",
		Description: "只有纯文本",
		Priority:    "medium",
		Type:        "incident",
		RequesterID: user.ID,
	}, tenant.ID)
	require.NoError(t, err)
	assert.Empty(t, plain.DescriptionHTML)
	assert.Equal(t, "plain", plain.DescriptionFormat)
}
