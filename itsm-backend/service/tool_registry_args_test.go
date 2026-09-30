package service

import (
	"testing"

	"itsm-backend/common"
	"itsm-backend/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateTicketRequestFromArgs_乐观锁与字段映射 覆盖 B1-05：
// expected_version 映射为乐观锁版本号；缺省/非正值不启用（保持既有行为）。
func TestUpdateTicketRequestFromArgs_乐观锁与字段映射(t *testing.T) {
	cases := []struct {
		name       string
		args       map[string]interface{}
		wantID     int
		wantReq    *dto.UpdateTicketRequest
		wantAbsent bool
	}{
		{
			name:   "全字段 + expected_version",
			args:   map[string]interface{}{"ticket_id": float64(7), "status": "resolved", "priority": "high", "resolution": "已修复", "assignee_id": float64(3), "expected_version": float64(5)},
			wantID: 7,
			wantReq: &dto.UpdateTicketRequest{
				Status: "resolved", Priority: "high", Resolution: "已修复", AssigneeID: 3, Version: 5,
			},
		},
		{
			name:    "缺 expected_version 不启用乐观锁",
			args:    map[string]interface{}{"ticket_id": float64(9), "status": "in_progress"},
			wantID:  9,
			wantReq: &dto.UpdateTicketRequest{Status: "in_progress"},
		},
		{
			name:    "expected_version<=0 视为未提供",
			args:    map[string]interface{}{"ticket_id": float64(9), "expected_version": float64(0)},
			wantID:  9,
			wantReq: &dto.UpdateTicketRequest{},
		},
		{
			name:       "缺 ticket_id",
			args:       map[string]interface{}{"status": "resolved"},
			wantID:     0,
			wantAbsent: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, req := updateTicketRequestFromArgs(tc.args)
			assert.Equal(t, tc.wantID, id)
			if tc.wantAbsent {
				require.NotNil(t, req, "请求体仍返回（由调用方判 ticket_id 缺失）")
				return
			}
			require.NotNil(t, req)
			assert.Equal(t, tc.wantReq.Status, req.Status)
			assert.Equal(t, tc.wantReq.Priority, req.Priority)
			assert.Equal(t, tc.wantReq.Resolution, req.Resolution)
			assert.Equal(t, tc.wantReq.AssigneeID, req.AssigneeID)
			assert.Equal(t, tc.wantReq.Version, req.Version)
			assert.False(t, req.Force, "Force 仅限内部受信调用，模型入参不得开启")
		})
	}
}

// TestErrorCodeOf_VersionConflict 固化 B1-05 的稳定错误码：
// 工单乐观锁冲突在审计/队列侧必须落 tool_version_conflict（而非 internal_error）。
func TestErrorCodeOf_VersionConflict(t *testing.T) {
	err := common.NewVersionConflictError("工单", 7, 2, 3)
	assert.Equal(t, "tool_version_conflict", errorCodeOf(err))
	assert.Equal(t, "internal_error", errorCodeOf(assert.AnError), "未分类错误保持既有口径")
}
