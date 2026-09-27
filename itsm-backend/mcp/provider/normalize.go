package provider

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"itsm-backend/mcp/client"
)

// 结果规范化（M0-09 要点 4/5）：
//   - 输出按**不可信数据**处理：只做结构化投影、控制字符清理与字节上限截断；
//     不解析其中的指令、不拼接系统提示、不执行任何内容；
//   - MCP 语义：工具自身报错（IsError=true）属于**给模型看的结果**（保留原文），
//     协议/传输失败才以 Go error 上抛（稳定错误码，见 execute.go）；
//   - 超出上限 → 截断并置 Truncated=true，绝不无声丢弃。
type Output struct {
	Provider  string          `json:"provider"`
	IsError   bool            `json:"is_error"`
	Content   []OutputContent `json:"content"`
	Truncated bool            `json:"truncated"`
	Bytes     int             `json:"bytes"`
}

// OutputContent 是单条内容的规范化投影。
type OutputContent struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	MIME string          `json:"mime,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

const truncationMarker = "…[truncated]"

func normalizeResult(result *client.CallResult, limit int) (any, error) {
	if result == nil {
		return Output{Provider: ProviderName, Content: []OutputContent{}}, nil
	}
	if limit <= 0 {
		limit = DefaultMaxResultBytes
	}
	out := Output{
		Provider: ProviderName,
		IsError:  result.IsError,
		Content:  make([]OutputContent, 0, len(result.Content)+1),
	}
	for _, item := range result.Content {
		out.Content = append(out.Content, OutputContent{
			Type: item.Type,
			Text: sanitizeText(item.Text),
			MIME: item.MIME,
			Data: item.Raw,
		})
	}
	if len(result.StructuredContent) > 0 {
		out.Content = append(out.Content, OutputContent{Type: "structured", Data: result.StructuredContent})
	}
	if len(out.Content) == 0 {
		out.Content = append(out.Content, OutputContent{Type: "text", Text: ""})
	}

	payload, err := json.Marshal(out)
	if err != nil {
		return nil, &ExecuteError{Code: CodeToolError, Message: "结果序列化失败"}
	}
	if len(payload) <= limit {
		out.Bytes = len(payload)
		return out, nil
	}

	trimToLimit(&out, limit)
	out.Truncated = true
	payload, err = json.Marshal(out)
	if err != nil {
		return nil, &ExecuteError{Code: CodeToolError, Message: "结果序列化失败"}
	}
	out.Bytes = len(payload)
	return out, nil
}

// trimToLimit 先裁剪文本内容，再丢弃尾部条目，保证最终 JSON ≤ limit。
func trimToLimit(out *Output, limit int) {
	overflow := len(mustJSON(out)) + len(truncationMarker) - limit
	if overflow > 0 {
		for index := range out.Content {
			if overflow <= 0 {
				break
			}
			text := out.Content[index].Text
			if len(text) == 0 {
				continue
			}
			cut := overflow
			if cut > len(text) {
				cut = len(text)
			}
			out.Content[index].Text = cutTail(text, cut) + truncationMarker
			overflow -= cut
		}
	}
	// 仍超限（如单条 Raw 巨大）：从尾部逐步丢弃内容条目（保留至少一条）。
	for len(mustJSON(out)) > limit && len(out.Content) > 1 {
		out.Content = out.Content[:len(out.Content)-1]
	}
	if len(mustJSON(out)) > limit {
		// 极端兜底：只剩一条仍超限 → 只保留类型占位。
		out.Content = []OutputContent{{Type: out.Content[0].Type, Text: truncationMarker}}
	}
}

func mustJSON(out *Output) []byte {
	payload, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return payload
}

// cutTail 从尾部裁剪 cut 字节（对齐 UTF-8 边界，不产生半个字符）。
func cutTail(text string, cut int) string {
	end := len(text) - cut
	if end <= 0 {
		return ""
	}
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

// sanitizeText 清理控制字符（保留换行/制表符），并归一化换行。
func sanitizeText(text string) string {
	if text == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}
