package service

// BE-7：内嵌图片引用完整性（净化对齐 + 归属校验）。
//
// 背景：`internal/sanitize` 会把 `<img data-attachment-id="N">` 原样保留，但
// 保留只解决了「属性不被剥离」，没有回答「这个 N 是不是真的属于当前正在写的宿主」。
// 灰度期两张附件表（legacy `ticket_attachments` / 通用 `attachments`）主键序列独立，
// 且通用附件读接口 A4 按租户放行，因此把别处（其它工单 / 其它域 / 其它租户）的附件
// 写进自己的正文，会留下一个跨宿主引用：删除保护、引用完整性、审计都会错位。
//
// 本文件的校验器在**写入路径**上做最后一道把关：解析正文里的 `<img>`，把
// 「确定不该出现在该宿主正文里」的引用整标签剥离，并回报违规明细供调用方告警。
//
// 设计取舍（灰度期安全第一，不误伤旧数据）：
//   - 只剥离有**确定证据**的引用，绝不对「查不到」一概而论——旧表附件在通用表中
//     本来就查不到，一刀切会把历史正文里的图片全部清空（D5：旧 URL 永不失效）；
//   - A4 规范地址（`/api/v1/attachments/{id}/...`）是新链路产物，语义明确：
//     必须能在通用表中解析到「同租户 + 同宿主 + 存活」的记录，否则剥离；
//   - 旧工单域内地址（`/api/v1/tickets/{tid}/attachments/...`）按路径宿主判定：
//     目标宿主是工单且 tid 与当前工单不一致时按越权引用剥离；
//   - 其它地址（外链等）只有在「能解析到通用记录且宿主不符 / 已软删」时才剥离。
//
// 失败策略：查询出错时返回 error，调用方记录告警并沿用已清洗内容（不因校验失败
// 阻塞业务写入）；未注入 ent 客户端时整体跳过，与改造前行为一致。

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/attachment"

	"go.uber.org/zap"
)

// InlineRefViolation 描述一个被剥离的内嵌图片引用。
type InlineRefViolation struct {
	// AttachmentID 引用指向的附件 ID（无法解析时为 0）。
	AttachmentID int
	// Reason 剥离原因，取值见 InlineRefReason* 常量。
	Reason string
	// Src 原始图片地址（截断前的完整值，便于排查）。
	Src string
}

// 剥离原因常量（可直接用于日志字段 / 埋点）。
const (
	// InlineRefReasonNotHosted 引用的附件存在，但宿主不是当前正文所属实体。
	InlineRefReasonNotHosted = "not_hosted"
	// InlineRefReasonNotFound A4 规范地址指向的附件在通用表中不存在。
	InlineRefReasonNotFound = "not_found"
	// InlineRefReasonInactive 引用的附件已软删。
	InlineRefReasonInactive = "inactive"
	// InlineRefReasonRefMismatch A4 地址中的 ID 与 data-attachment-id 不一致。
	InlineRefReasonRefMismatch = "ref_mismatch"
	// InlineRefReasonLegacyCrossHost 旧工单域内地址指向的工单与当前宿主不一致。
	InlineRefReasonLegacyCrossHost = "legacy_cross_host"
)

var (
	// inlineImgTagPattern 匹配正文中的完整 <img> 标签（bluemonday 输出恒为双引号）。
	inlineImgTagPattern = regexp.MustCompile(`(?is)<img\b[^>]*>`)

	// inlineImgAttrPattern 解析 src / data-attachment-id（兼容双引号 / 单引号 / 无引号）。
	inlineImgAttrPattern = regexp.MustCompile(`(?is)\b(src|data-attachment-id)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

	// genericAttachmentSrcPattern 匹配 A4 规范地址：/api/v1/attachments/{id}[/content|/preview|/download]。
	genericAttachmentSrcPattern = regexp.MustCompile(`(?i)^/api/v1/attachments/(\d+)(?:/(?:content|preview|download))?/?$`)

	// legacyTicketSrcPattern 匹配旧工单域内地址：/api/v1/tickets/{tid}/attachments/...。
	legacyTicketSrcPattern = regexp.MustCompile(`(?i)^/api/v1/tickets/(\d+)/attachments(?:/|$)`)
)

// inlineImgRef 是单个 <img> 标签解析后的引用信息。
type inlineImgRef struct {
	src        string
	refID      int
	refPresent bool
	genericID  int
	legacyTID  int
}

// ValidateRichTextInlineRefs 校验正文内嵌图片引用是否归属指定宿主，返回清洗后的 HTML
// 与违规明细；`bizID` 为 0 表示宿主尚未创建（如新建工单 / 新建文章），此时任何能解析到
// 通用记录的引用都属于「他人的附件」，一律剥离。
func ValidateRichTextInlineRefs(ctx context.Context, client *ent.Client, tenantID int, bizType string, bizID int, input string) (string, []InlineRefViolation, error) {
	if input == "" || client == nil || tenantID <= 0 || bizType == "" {
		return input, nil, nil
	}
	if !strings.Contains(strings.ToLower(input), "<img") {
		return input, nil, nil
	}

	tags := inlineImgTagPattern.FindAllString(input, -1)
	if len(tags) == 0 {
		return input, nil, nil
	}

	refs := make([]inlineImgRef, 0, len(tags))
	lookupIDs := make(map[int]struct{})
	for _, tag := range tags {
		ref := parseInlineImgRef(tag)
		refs = append(refs, ref)
		if ref.genericID > 0 {
			lookupIDs[ref.genericID] = struct{}{}
		}
		if ref.refID > 0 {
			lookupIDs[ref.refID] = struct{}{}
		}
	}

	records := make(map[int]*ent.Attachment, len(lookupIDs))
	if len(lookupIDs) > 0 {
		ids := make([]int, 0, len(lookupIDs))
		for id := range lookupIDs {
			ids = append(ids, id)
		}
		rows, err := client.Attachment.Query().
			Where(attachment.TenantID(tenantID), attachment.IDIn(ids...)).
			All(ctx)
		if err != nil {
			return input, nil, fmt.Errorf("query inline attachment refs: %w", err)
		}
		for _, row := range rows {
			records[row.ID] = row
		}
	}

	var (
		builder    strings.Builder
		rest       = input
		violations []InlineRefViolation
	)
	for i, tag := range tags {
		idx := strings.Index(rest, tag)
		if idx < 0 {
			// 理论上不可达（tags 来自同一字符串）；保守起见停止重写并保留剩余内容。
			break
		}
		builder.WriteString(rest[:idx])
		rest = rest[idx+len(tag):]

		ref := refs[i]
		if reason := inlineRefViolationReason(ref, bizType, bizID, records); reason != "" {
			violations = append(violations, InlineRefViolation{
				AttachmentID: ref.primaryID(),
				Reason:       reason,
				Src:          ref.src,
			})
			continue // 剥离整个 <img> 标签
		}
		builder.WriteString(tag)
	}
	builder.WriteString(rest)

	return builder.String(), violations, nil
}

// ValidateRichTextInlineRefs 供 handler 层复用（经 TicketService 持有 ent 客户端，
// 见 handlers/ticket/service.go 的更新路径）；服务或客户端缺失时原样返回。
func (s *TicketService) ValidateRichTextInlineRefs(ctx context.Context, tenantID int, bizType string, bizID int, input string) (string, []InlineRefViolation, error) {
	if s == nil || s.client == nil {
		return input, nil, nil
	}
	return ValidateRichTextInlineRefs(ctx, s.client, tenantID, bizType, bizID, input)
}

// primaryID 返回该标签用于告警的附件 ID（优先 A4 地址中的 ID）。
func (r inlineImgRef) primaryID() int {
	if r.genericID > 0 {
		return r.genericID
	}
	return r.refID
}

// logInlineRefViolations 统一输出剥离告警（每条违规一行，便于按 attachment_id 追溯）。
func logInlineRefViolations(logger *zap.SugaredLogger, tenantID int, bizType string, bizID int, violations []InlineRefViolation) {
	if logger == nil || len(violations) == 0 {
		return
	}
	for _, v := range violations {
		logger.Warnw("剥离越权内嵌图片引用",
			"tenant_id", tenantID,
			"biz_type", bizType,
			"biz_id", bizID,
			"attachment_id", v.AttachmentID,
			"reason", v.Reason,
			"src", v.Src,
		)
	}
}

// parseInlineImgRef 解析 <img> 标签的 src 与 data-attachment-id。
func parseInlineImgRef(tag string) inlineImgRef {
	var ref inlineImgRef
	for _, m := range inlineImgAttrPattern.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		value := m[2]
		if value == "" {
			value = m[3]
		}
		if value == "" {
			value = m[4]
		}
		switch name {
		case "src":
			ref.src = html.UnescapeString(strings.TrimSpace(value))
		case "data-attachment-id":
			ref.refPresent = true
			if n, err := strconv.Atoi(strings.TrimSpace(html.UnescapeString(value))); err == nil && n > 0 {
				ref.refID = n
			}
		}
	}
	if m := genericAttachmentSrcPattern.FindStringSubmatch(ref.src); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			ref.genericID = n
		}
	}
	if m := legacyTicketSrcPattern.FindStringSubmatch(ref.src); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			ref.legacyTID = n
		}
	}
	return ref
}

// inlineRefViolationReason 返回剥离原因；空串表示保留。
func inlineRefViolationReason(ref inlineImgRef, bizType string, bizID int, records map[int]*ent.Attachment) string {
	// 1) A4 规范地址：新链路产物，语义明确，必须完整命中「同租户 + 同宿主 + 存活」。
	if ref.genericID > 0 {
		if !ref.refPresent || ref.refID != ref.genericID {
			return InlineRefReasonRefMismatch
		}
		rec := records[ref.genericID]
		if rec == nil {
			return InlineRefReasonNotFound
		}
		if rec.Status != AttachmentStatusActive {
			return InlineRefReasonInactive
		}
		if rec.BizType != bizType || rec.BizID != bizID {
			return InlineRefReasonNotHosted
		}
		return ""
	}

	// 2) 旧工单域内地址：路径宿主必须与当前宿主一致（仅当目标宿主是工单时可判定）；
	//    bizID=0（新建工单）时任何旧工单地址都属于他人的工单，同样剥离。
	if ref.legacyTID > 0 && bizType == AttachmentBizTypeTicket && ref.legacyTID != bizID {
		return InlineRefReasonLegacyCrossHost
	}

	// 3) 其它地址：只剥离「能解析到通用记录且宿主不符 / 已软删」的确定性越权引用。
	if ref.refID > 0 {
		if rec := records[ref.refID]; rec != nil {
			if rec.BizType != bizType || rec.BizID != bizID {
				return InlineRefReasonNotHosted
			}
			if rec.Status != AttachmentStatusActive {
				return InlineRefReasonInactive
			}
		}
	}
	return ""
}
