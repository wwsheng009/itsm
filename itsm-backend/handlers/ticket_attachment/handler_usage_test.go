package ticket_attachment

// handler_usage_test.go 冻结 BE-10（域内端点 usage 透传）口径。
//
// 背景：通用路由 A1-A6 的静态权限码是**兜底码** `attachment:write/read/delete`
// （§4.3 仅绑定 admin/sysadmin），普通用户（technician / agent）无法经通用路由表达
// `usage`；而 `inline_image` / `comment_attachment` 又必须落通用表（旧
// `ticket_attachments` 无 usage 列）。因此域内端点接受可选 `usage` 表单字段：
//
//  1. 非默认用途不受 `attachment.generic_write_enabled` 约束（旧链路无等价能力）；
//  2. 缺省（空）用途行为与改造前完全一致（写旧表）；
//  3. 未登记用途一律参数错误，且不落任何表；
//  4. 用途记录可由普通用户经域内端点删除（不依赖写开关），删除走通用软删。

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent/attachment"
	"itsm-backend/service"

	"github.com/stretchr/testify/require"
)

// uploadWithUsage 在旧工单上传端点上附加 `usage` 表单字段（BE-10 的请求夹具）。
func (e *contractEnv) uploadWithUsage(ticketID int, filename string, content []byte, usage string) *httptest.ResponseRecorder {
	e.t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(e.t, err)
	_, err = part.Write(content)
	require.NoError(e.t, err)
	if usage != "" {
		require.NoError(e.t, writer.WriteField("usage", usage))
	}
	require.NoError(e.t, writer.Close())
	return e.do(e.router, http.MethodPost,
		fmt.Sprintf("/api/v1/tickets/%d/attachments", ticketID), writer.FormDataContentType(), body)
}

func TestLegacyTicketAttachmentContract_UsagePassthrough(t *testing.T) {
	env := newContractEnv(t)
	ctx := context.Background()
	require.False(t, env.flags.write, "前置条件：通用写开关关闭（模拟未灰度租户）")

	// 1) 内嵌图片：usage=inline_image 必须落通用表，响应仍是旧字段形态（D5 地址不变）
	w := env.uploadWithUsage(env.ticket.ID, "embed.png", contractPNG, "inline_image")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	envelope, obj, _ := decodeContractObject(t, w)
	require.Equal(t, common.SuccessCode, envelope.Code)
	require.Regexp(t, contractFileURLPattern, obj["fileUrl"])

	inlineCount, err := env.client.Attachment.Query().
		Where(attachment.UsageEQ(service.AttachmentUsageInlineImage)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, inlineCount, "非默认用途必须落通用表")

	legacyCount, err := env.client.TicketAttachment.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, legacyCount, "非默认用途不得写旧表（旧表无 usage 列）")

	// 2) 缺省用途：行为与改造前一致（写旧表，通用表不再新增）
	plain := env.uploadPNG("plain.png")
	require.Equal(t, http.StatusOK, plain.Code, plain.Body.String())
	legacyCount, err = env.client.TicketAttachment.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, legacyCount, "缺省用途仍走旧表")

	// 3) 未登记用途：参数错误 + 不落任何表
	before, err := env.client.Attachment.Query().Count(ctx)
	require.NoError(t, err)
	bad := env.uploadWithUsage(env.ticket.ID, "avatar.png", contractPNG, "avatar")
	// 与 BE-10 冻结口径一致：参数错误走 common.Fail(ParamErrorCode) → HTTP 400，
	// 与通用路由同名错误的 HTTP 状态 / 业务码保持一致（见 ErrorCodeParity 契约）。
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())
	require.Equal(t, common.ParamErrorCode, decodeContractEnvelope(t, bad).Code)
	after, err := env.client.Attachment.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "非法用途不得落库")

	// 4) 删除：普通用户经域内端点删自己的内嵌图片（写开关关闭也必须成功）
	row, err := env.client.Attachment.Query().
		Where(attachment.UsageEQ(service.AttachmentUsageInlineImage)).
		Only(ctx)
	require.NoError(t, err)
	del := env.remove(strconv.Itoa(row.ID))
	require.Equal(t, http.StatusOK, del.Code, del.Body.String())
	require.Equal(t, common.SuccessCode, decodeContractEnvelope(t, del).Code)

	stored, err := env.client.Attachment.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted", stored.Status, "通用记录走软删（引用保护由通用服务负责）")
}
