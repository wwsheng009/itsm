// Package budget 提供 MCP 工具面的规模估算与预算判定（M2-03）。
//
// 背景（分析报告 §9.1 第一行）：外部工具面过大会挤占模型上下文并降低工具选择准确率。
// 本包提供两个触发信号的**可测量口径**：
//
//  1. 单租户有效工具数 > 阈值（默认 40）；
//  2. 工具面 token 估算占上下文预算的比例 > 阈值（默认 30%）。
//
// 估算口径：CJK 字符按 1 token/字、其余字符按 4 字符/token（均向上取整）。
// 该口径面向**阈值告警**（比精确分词保守、无外部依赖），不用于计费或精确计量；
// 精确计量如需上线，应在 M2-06 指标层以真实分词器替换本包实现（接口不变）。
package budget

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const (
	// DefaultToolLimit 是单租户有效工具数的默认预算。
	DefaultToolLimit = 40
	// DefaultContextTokens 是工具面 token 占比判定使用的默认上下文预算。
	DefaultContextTokens = 128000
	// DefaultTokenShare 是工具面 token 占上下文预算的默认比例上限。
	DefaultTokenShare = 0.30
	// runesPerToken 是非 CJK 字符的估算系数（4 字符 ≈ 1 token）。
	runesPerToken = 4
)

// Entry 是一个工具进入工具面的估算输入（名称 + 描述 + 参数 schema）。
type Entry struct {
	// Name 是暴露给模型的工具名（投影后的 callable name）。
	Name string
	// Description 是工具描述。
	Description string
	// Schema 是参数 schema 的原始 JSON（可为空）。
	Schema []byte
}

// Surface 是工具面规模（工具数、字节数、估算 token 数）。
type Surface struct {
	Tools  int `json:"tools"`
	Bytes  int `json:"bytes"`
	Tokens int `json:"tokens"`
}

// EstimateTokens 估算一段文本的 token 数：CJK 按 1 token/字，其余按 4 字符/token。
//
// 空文本为 0；非空文本至少为 1（避免“很短但非空”被算作 0 而低估）。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	cjk, other := 0, 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
			continue
		}
		other++
	}
	tokens := cjk + int(math.Ceil(float64(other)/runesPerToken))
	if tokens < 1 {
		return 1
	}
	return tokens
}

// isCJK 判定是否按“1 字符 1 token”计数的宽字符（CJK 及全角标点）。
func isCJK(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
		return true
	case unicode.Is(unicode.Hangul, r):
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK 标点
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // 全角字符
		return true
	}
	return false
}

// Measure 计算工具面规模：Tools 为条目数，Bytes 为名称+描述+schema 的字节数，
// Tokens 为三者（名称与描述按 rune 估算、schema 按原文估算）之和。
func Measure(entries []Entry) Surface {
	surface := Surface{Tools: len(entries)}
	for _, entry := range entries {
		surface.Bytes += len(entry.Name) + len(entry.Description) + len(entry.Schema)
		surface.Tokens += EstimateTokens(entry.Name) + EstimateTokens(entry.Description) + EstimateTokens(string(entry.Schema))
	}
	return surface
}

// Limits 是预算阈值。
type Limits struct {
	// Tools 是工具数上限；<=0 时用 DefaultToolLimit。
	Tools int
	// ContextTokens 是上下文预算；<=0 时用 DefaultContextTokens。
	ContextTokens int
	// Share 是 token 占比上限（0-1）；<=0 时用 DefaultTokenShare。
	Share float64
}

// WithDefaults 补齐零值。
func (l Limits) WithDefaults() Limits {
	if l.Tools <= 0 {
		l.Tools = DefaultToolLimit
	}
	if l.ContextTokens <= 0 {
		l.ContextTokens = DefaultContextTokens
	}
	if l.Share <= 0 {
		l.Share = DefaultTokenShare
	}
	return l
}

// Report 是一次预算判定结果。
type Report struct {
	Surface       Surface  `json:"surface"`
	ToolLimit     int      `json:"tool_limit"`
	ContextTokens int      `json:"context_tokens"`
	Share         float64  `json:"share"`
	ShareLimit    float64  `json:"share_limit"`
	Exceeded      bool     `json:"exceeded"`
	Reasons       []string `json:"reasons,omitempty"`
}

// Evaluate 判定工具面是否超出预算（两个信号任一命中即 Exceeded）。
//
// 占比按四舍五入保留 4 位小数，避免浮点噪声影响判定与展示一致性；
// 判定本身用未取整的比例与上限直接比较。
func Evaluate(surface Surface, limits Limits) Report {
	limits = limits.WithDefaults()
	share := 0.0
	if limits.ContextTokens > 0 {
		share = float64(surface.Tokens) / float64(limits.ContextTokens)
	}
	report := Report{
		Surface:       surface,
		ToolLimit:     limits.Tools,
		ContextTokens: limits.ContextTokens,
		Share:         math.Round(share*10000) / 10000,
		ShareLimit:    limits.Share,
	}
	if surface.Tools > limits.Tools {
		report.Exceeded = true
		report.Reasons = append(report.Reasons,
			"有效工具数 "+strconv.Itoa(surface.Tools)+" 超过预算 "+strconv.Itoa(limits.Tools))
	}
	if share > limits.Share {
		report.Exceeded = true
		report.Reasons = append(report.Reasons,
			"工具面估算 "+strconv.Itoa(surface.Tokens)+" tokens，占上下文 "+strconv.Itoa(limits.ContextTokens)+
				" 的 "+percent(share)+"，超过上限 "+percent(limits.Share))
	}
	return report
}

// percent 把比例格式化为百分比（一位小数）。
func percent(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value*100), "0"), ".") + "%"
}

// 工具面治理动作（M2-03「元工具/检索式工具面方案」评估结论）。
const (
	// ActionNone：工具面在预算内，维持直出（无需元工具）。
	ActionNone = "none"
	// ActionMetaTools：建议启用检索式工具面（元工具），需灰度开关、默认关闭。
	ActionMetaTools = "meta_tools_candidate"
)

// MetaToolNames 是评估建议的元工具形态（检索式工具面最小集）。
//
// 设计约束（若实施）：元工具与普通外部工具**同源**——同一命名投影前缀、同一 Gate1/Gate2/Gate3、
// 同一审计；`call` 必须按 canonical 名精确解析并沿用既有审批语义；默认不启用（灰度开关控制）。
var MetaToolNames = []string{"mcp__meta__list", "mcp__meta__search", "mcp__meta__describe", "mcp__meta__call"}

// Recommendation 是工具面治理建议（评估产物，供管理面/指标层展示与灰度决策使用）。
type Recommendation struct {
	// Action 取值见 ActionNone / ActionMetaTools。
	Action string `json:"action"`
	// Reason 是人读理由（含命中的信号与规模）。
	Reason string `json:"reason"`
	// MetaTools 在 Action=ActionMetaTools 时给出建议形态；否则为空。
	MetaTools []string `json:"meta_tools,omitempty"`
}

// Advise 依据预算判定给出治理建议：
//   - 未超限 → 维持直出；
//   - 超限（任一信号）→ 建议评估启用检索式工具面（元工具），并提示必须灰度、默认关闭。
//
// 注意：本函数只给建议，不改变工具面（元工具是否实施由 M2 灰度评审决定）。
func Advise(report Report) Recommendation {
	if !report.Exceeded {
		return Recommendation{
			Action: ActionNone,
			Reason: "工具面在预算内（tools=" + strconv.Itoa(report.Surface.Tools) + "/" + strconv.Itoa(report.ToolLimit) +
				"，tokens=" + strconv.Itoa(report.Surface.Tokens) + "/" + strconv.Itoa(report.ContextTokens) + "）",
		}
	}
	return Recommendation{
		Action:    ActionMetaTools,
		Reason:    "工具面超限，建议评估检索式工具面（元工具）：" + strings.Join(report.Reasons, "；") + "；须以灰度开关默认关闭上线",
		MetaTools: append([]string(nil), MetaToolNames...),
	}
}
