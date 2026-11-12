package dto

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateTicketRequestDoesNotBindForceFromJSON(t *testing.T) {
	var req UpdateTicketRequest
	require.NoError(t, json.Unmarshal([]byte(`{"title":"updated","force":true,"version":1}`), &req))
	require.False(t, req.Force)
	require.Equal(t, 1, req.Version)
}

func TestUpdateTicketRequestAssigneePresence(t *testing.T) {
	for _, tc := range []struct {
		body    string
		present bool
		id      int
	}{
		{`{}`, false, 0},
		{`{"assigneeId":null}`, false, 0},
		{`{"assigneeId":0}`, true, 0},
		{`{"assigneeId":7}`, true, 7},
	} {
		t.Run(tc.body, func(t *testing.T) {
			var req UpdateTicketRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
			if tc.present {
				require.NotNil(t, req.AssigneeID)
				require.Equal(t, tc.id, *req.AssigneeID)
			} else {
				require.Nil(t, req.AssigneeID)
			}
			raw, err := json.Marshal(req)
			require.NoError(t, err)
			var output map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &output))
			_, present := output["assigneeId"]
			require.Equal(t, tc.present, present, "zero must survive encoding; omission must not clear")
		})
	}
}

// P2 富文本契约：请求/响应字段名与校验标签必须与前端 descriptionHtml 约定一致。
func TestTicketRichTextDescriptionHTMLContract(t *testing.T) {
	// 请求绑定：camelCase JSON + max=524288（512KB）
	createField, ok := reflect.TypeOf(CreateTicketRequest{}).FieldByName("DescriptionHTML")
	require.True(t, ok, "CreateTicketRequest.DescriptionHTML 必须存在")
	require.Equal(t, "descriptionHtml", createField.Tag.Get("json"))
	require.Equal(t, "omitempty,max=524288", createField.Tag.Get("binding"))

	updateField, ok := reflect.TypeOf(UpdateTicketRequest{}).FieldByName("DescriptionHTML")
	require.True(t, ok, "UpdateTicketRequest.DescriptionHTML 必须存在")
	require.Equal(t, "descriptionHtml", updateField.Tag.Get("json"))
	require.Equal(t, "omitempty,max=524288", updateField.Tag.Get("binding"))

	var createReq CreateTicketRequest
	require.NoError(t, json.Unmarshal(
		[]byte(`{"title":"t","description":"纯文本","descriptionHtml":"<p>富文本</p>"}`),
		&createReq,
	))
	require.Equal(t, "纯文本", createReq.Description)
	require.Equal(t, "<p>富文本</p>", createReq.DescriptionHTML)

	var updateReq UpdateTicketRequest
	require.NoError(t, json.Unmarshal([]byte(`{"descriptionHtml":"<p>更新</p>"}`), &updateReq))
	require.Equal(t, "<p>更新</p>", updateReq.DescriptionHTML)

	// 响应序列化：camelCase 字段名；零值（未携带）时 omitempty 不应出现
	raw, err := json.Marshal(TicketResponse{DescriptionHTML: "<p>a</p>", DescriptionFormat: "html"})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, "<p>a</p>", payload["descriptionHtml"])
	require.Equal(t, "html", payload["descriptionFormat"])

	rawEmpty, err := json.Marshal(TicketResponse{})
	require.NoError(t, err)
	require.NotContains(t, string(rawEmpty), "descriptionHtml")
	require.NotContains(t, string(rawEmpty), "descriptionFormat")
}
