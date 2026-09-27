// M1-02：ToolQueue 入队契约（fail-closed）。
//
// 历史行为：Enqueue 在队列满时**静默丢弃**——审批人看到「已批准」，但记录永远不会被执行，
// 审计里也没有失败原因。M1-02 起改为显式返回 ErrToolQueueFull，由 ApproveTool 回滚为 pending
// 并提示重试。
package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolQueue_EnqueueFailClosedWhenFull(t *testing.T) {
	// 直接构造未启动 worker 的队列（同包可见），确保容量不会被消费。
	queue := &ToolQueue{jobs: make(chan ToolJob, 1)}

	require.NoError(t, queue.Enqueue(ToolJob{InvocationID: 1, TenantID: 1}), "首个任务必须入队成功")

	err := queue.Enqueue(ToolJob{InvocationID: 2, TenantID: 1})
	require.ErrorIs(t, err, ErrToolQueueFull, "队列满必须显式失败，不得静默丢弃")
	require.Len(t, queue.jobs, 1, "失败的任务不得入队")
}
