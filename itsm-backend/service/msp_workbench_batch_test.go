package service

import (
	"context"
	"testing"

	"itsm-backend/dto"

	"github.com/stretchr/testify/require"
)

// TestWorkbenchBatch_Validation 批量护栏（IP-P1-6）：空/超限拒绝、高危动作白名单、限流窗口。
func TestWorkbenchBatch_Validation(t *testing.T) {
	s := &MSPWorkbenchService{}
	actor := MSPWorkbenchActor{UserID: 1, HomeTenantID: 7}
	ctx := context.Background()

	t.Run("空条目拒绝", func(t *testing.T) {
		_, err := s.Batch(ctx, actor, dto.WorkbenchBatchRequest{Action: "reply"})
		ae, ok := AsCustomerAccessError(err)
		require.True(t, ok)
		require.Equal(t, CodeBatchLimitExceeded, ae.Code)
	})

	t.Run("超限拒绝（>100）", func(t *testing.T) {
		items := make([]dto.WorkbenchBatchItem, workbenchBatchMax+1)
		_, err := s.Batch(ctx, actor, dto.WorkbenchBatchRequest{Action: "reply", Items: items})
		ae, ok := AsCustomerAccessError(err)
		require.True(t, ok)
		require.Equal(t, CodeBatchLimitExceeded, ae.Code)
	})

	t.Run("高危/未知动作拒绝", func(t *testing.T) {
		items := []dto.WorkbenchBatchItem{{TicketID: 1, CustomerTenantID: 2}}
		for _, action := range []string{"delete", "close_and_archive", ""} {
			_, err := s.Batch(ctx, actor, dto.WorkbenchBatchRequest{Action: action, Items: items})
			ae, ok := AsCustomerAccessError(err)
			require.True(t, ok, "action=%q", action)
			require.Equal(t, CodeActionNotAllowed, ae.Code, "action=%q", action)
		}
	})

	t.Run("单租户限流窗口（20/分钟）", func(t *testing.T) {
		limited := &MSPWorkbenchService{}
		for i := 0; i < workbenchBatchPerMin; i++ {
			require.True(t, limited.batchAllowed(42), "第 %d 次应放行", i+1)
		}
		require.False(t, limited.batchAllowed(42), "超过窗口上限应拒绝")
		// 其他租户不受影响（护栏按租户粒度）。
		require.True(t, limited.batchAllowed(43))
	})
}
