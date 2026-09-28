package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B0-04：create_ticket 预览的字段投影与零业务写入契约。
//
// 零写入的**结构性**保证：NewToolRegistry(nil, nil, nil, nil) 未注入 ticket/cmdb 等服务，
// 若预览路径触碰任何业务写方法会立即失败（nil panic/错误），断言因此具备约束力。
func TestPreviewTool_CreateTicketProjectsFields(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)

	preview, err := reg.PreviewTool(context.Background(), 10, "create_ticket", map[string]interface{}{
		"title":       "打印机故障",
		"description": "三楼打印机无法出纸",
		"priority":    "high",
		"category":    "hardware",
		"ci_id":       float64(42),
		"assignee_id": float64(7),
	})
	require.NoError(t, err)
	require.NotNil(t, preview)

	assert.Equal(t, "create_ticket", preview.Tool)
	assert.Equal(t, "create", preview.Mode)
	assert.True(t, preview.DryRun)
	assert.NotEmpty(t, preview.Version, "预览快照必须带版本（B1-05 冻结参数用）")
	assert.Contains(t, preview.Note, "预览不保证最终成功", "BQ7：提示口径必须随预览返回")
	assert.Equal(t, "打印机故障", preview.Fields["title"])
	assert.Equal(t, "high", preview.Fields["priority"])
	assert.Equal(t, "hardware", preview.Fields["category"])
	assert.Equal(t, 42, preview.Fields["ci_id"])
	assert.Equal(t, 7, preview.Fields["assignee_id"])

	// 版本确定性：同输入同版本；输入变化 → 版本变化（防审批前参数被篡改）。
	again, err := reg.PreviewTool(context.Background(), 10, "create_ticket", map[string]interface{}{
		"title":       "打印机故障",
		"description": "三楼打印机无法出纸",
		"priority":    "high",
		"category":    "hardware",
		"ci_id":       float64(42),
		"assignee_id": float64(7),
	})
	require.NoError(t, err)
	assert.Equal(t, preview.Version, again.Version)

	changed, err := reg.PreviewTool(context.Background(), 10, "create_ticket", map[string]interface{}{
		"title":    "打印机故障（改）",
		"priority": "high",
	})
	require.NoError(t, err)
	assert.NotEqual(t, preview.Version, changed.Version, "参数变化必须导致版本变化")
}

// B0-04：读工具与未实现预览的写工具必须被拒绝，且不产生任何记录。
func TestPreviewTool_Guards(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	ctx := context.Background()

	_, err := reg.PreviewTool(ctx, 10, "list_tickets", map[string]interface{}{})
	require.ErrorIs(t, err, ErrPreviewNotWrite, "读工具无副作用，不走 dry-run")

	// delete_ci_relationship 是写工具但未实现预览分支（SupportsDryRun 未开放）。
	_, err = reg.PreviewTool(ctx, 10, "delete_ci_relationship", map[string]interface{}{"relationship_id": float64(3)})
	require.True(t, errors.Is(err, ErrPreviewUnsupported) || errors.Is(err, ErrPreviewNotWrite))

	_, err = reg.PreviewTool(ctx, 10, "no_such_tool", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// B0-04：create_ticket 缺 title → 预览即失败（快速失败，不落任何记录）。
func TestPreviewTool_CreateTicketRequiresTitle(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	_, err := reg.PreviewTool(context.Background(), 10, "create_ticket", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title is required")
}

// B0-04：update_ticket 需要只读读取当前值；无 ticket 服务时明确失败（而非静默返回空 diff）。
func TestPreviewTool_UpdateTicketRequiresTicketService(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	_, err := reg.PreviewTool(context.Background(), 10, "update_ticket", map[string]interface{}{"ticket_id": float64(1)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticket service not initialized")
}
