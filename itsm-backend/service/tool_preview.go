package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// B0-04：dry-run 预览分支与零写入契约。
//
// 设计约束（与方案 §4.1 B0-04 对齐）：
//   - **零业务写入**：预览只做参数投影与只读读取（update 模式按需读当前值），
//     绝不调用任何业务写方法（CreateTicket/UpdateTicket/...）；
//   - **快照冻结**：预览结果带 `version`（内容哈希），随工具调用记录落库，
//     供 B1-05 在确认时冻结执行参数；
//   - **不承诺成功**：`note` 明确提示预览不保证最终成功（BQ7 拍板：文档 + UI 双写）。
var (
	// ErrPreviewNotWrite 读工具不支持 dry-run（本就没有副作用，直接执行即可）。
	ErrPreviewNotWrite = errors.New("dry-run 仅适用于写工具")
	// ErrPreviewUnsupported 该写工具未实现预览分支（如 delete_ci_relationship）。
	ErrPreviewUnsupported = errors.New("该工具暂不支持 dry-run 预览")
)

// PreviewFieldDiff 是 diff 模式下单个字段的变化。
type PreviewFieldDiff struct {
	Field  string      `json:"field"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
	Change string      `json:"change"` // set（原值为空）|change|noop
}

// PreviewTarget 是 diff 模式的目标对象（只读读取，不修改）。
type PreviewTarget struct {
	Type   string `json:"type"`
	ID     int    `json:"id"`
	Number string `json:"number"`
	Status string `json:"status"`
}

// ToolPreview 是写工具的 dry-run 预览结果（随确认单快照持久化）。
type ToolPreview struct {
	Tool        string                 `json:"tool"`
	Mode        string                 `json:"mode"` // create|diff
	DryRun      bool                   `json:"dryRun"`
	Fields      map[string]interface{} `json:"fields,omitempty"`
	Diff        []PreviewFieldDiff     `json:"diff,omitempty"`
	Target      *PreviewTarget         `json:"target,omitempty"`
	Version     string                 `json:"version"`
	GeneratedAt time.Time              `json:"generatedAt"`
	Note        string                 `json:"note"`
}

// PreviewNotGuaranteedNote 是 dry-run 的固定提示（BQ7：文档 + UI 双写同一口径）。
const PreviewNotGuaranteedNote = "预览不保证最终成功：审批通过后按落库参数执行，业务校验、并发冲突或外部依赖仍可能导致失败。"

// PreviewTool 生成写工具的 dry-run 预览（零业务写入）。
func (t *ToolRegistry) PreviewTool(ctx context.Context, tenantID int, name string, args map[string]interface{}) (*ToolPreview, error) {
	td := t.GetTool(name)
	if td == nil {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	if td.ReadOnly {
		return nil, ErrPreviewNotWrite
	}
	if !td.SupportsDryRun {
		return nil, ErrPreviewUnsupported
	}

	var preview *ToolPreview
	var err error
	switch name {
	case "create_ticket":
		preview, err = t.previewCreateTicket(args)
	case "update_ticket":
		preview, err = t.previewUpdateTicket(ctx, tenantID, args)
	default:
		return nil, ErrPreviewUnsupported
	}
	if err != nil {
		return nil, err
	}

	preview.DryRun = true
	preview.GeneratedAt = time.Now().UTC()
	preview.Note = PreviewNotGuaranteedNote
	version, err := previewVersion(preview)
	if err != nil {
		return nil, err
	}
	preview.Version = version
	return preview, nil
}

// previewCreateTicket 投影「将要创建的字段」——不触碰业务写路径。
func (t *ToolRegistry) previewCreateTicket(args map[string]interface{}) (*ToolPreview, error) {
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("create_ticket: title is required")
	}
	fields := map[string]interface{}{
		"title":    title,
		"priority": strArgOr(args, "priority", "medium"),
	}
	for _, key := range []string{"description", "type", "category"} {
		if v, ok := args[key].(string); ok && v != "" {
			// 描述类字段限长，避免预览快照暴涨（脱敏由 B0-06 统一治理）。
			if key == "description" && len(v) > 2000 {
				v = v[:2000] + "…(truncated)"
			}
			fields[key] = v
		}
	}
	for _, key := range []string{"requester_id", "assignee_id", "ticket_type_id", "ci_id"} {
		if v, ok := args[key].(float64); ok && int(v) > 0 {
			fields[key] = int(v)
		}
	}
	return &ToolPreview{Tool: "create_ticket", Mode: "create", Fields: fields}, nil
}

// previewUpdateTicket 读取当前工单（只读）并生成字段 diff——不触碰业务写路径。
func (t *ToolRegistry) previewUpdateTicket(ctx context.Context, tenantID int, args map[string]interface{}) (*ToolPreview, error) {
	if t.ticket == nil {
		return nil, fmt.Errorf("ticket service not initialized")
	}
	ticketID := 0
	if v, ok := args["ticket_id"].(float64); ok {
		ticketID = int(v)
	}
	if ticketID == 0 {
		return nil, fmt.Errorf("update_ticket: ticket_id is required")
	}

	current, err := t.ticket.GetTicket(ctx, ticketID, tenantID)
	if err != nil {
		return nil, err
	}

	after := map[string]interface{}{}
	if v, ok := args["status"].(string); ok && v != "" {
		after["status"] = v
	}
	if v, ok := args["priority"].(string); ok && v != "" {
		after["priority"] = v
	}
	if v, ok := args["resolution"].(string); ok && v != "" {
		after["resolution"] = v
	}
	if v, ok := args["assignee_id"].(float64); ok && int(v) > 0 {
		after["assigneeId"] = int(v)
	}

	before := map[string]interface{}{
		"status":     string(current.Status),
		"priority":   string(current.Priority),
		"resolution": derefString(current.Resolution),
		"assigneeId": derefInt(current.AssigneeID),
	}

	diff := make([]PreviewFieldDiff, 0, len(after))
	for _, field := range []string{"status", "priority", "resolution", "assigneeId"} {
		newVal, ok := after[field]
		if !ok {
			continue
		}
		oldVal := before[field]
		change := "change"
		if isEmptyValue(oldVal) {
			change = "set"
		} else if fmt.Sprint(oldVal) == fmt.Sprint(newVal) {
			change = "noop"
		}
		diff = append(diff, PreviewFieldDiff{Field: field, Before: oldVal, After: newVal, Change: change})
	}

	return &ToolPreview{
		Tool: "update_ticket",
		Mode: "diff",
		Diff: diff,
		Target: &PreviewTarget{
			Type:   "ticket",
			ID:     current.ID,
			Number: current.TicketNumber,
			Status: string(current.Status),
		},
	}, nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func strArgOr(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func isEmptyValue(v interface{}) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case int:
		return x == 0
	}
	return false
}

// previewVersion 生成预览内容哈希（确定性：同输入同版本；用于确认时比对参数未被篡改）。
func previewVersion(p *ToolPreview) (string, error) {
	clone := *p
	clone.Version = ""
	clone.GeneratedAt = time.Time{}
	raw, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16]), nil
}
