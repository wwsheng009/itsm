package ai

import (
	"strings"
	"unicode/utf8"

	"itsm-backend/common"

	"github.com/gin-gonic/gin"
)

// V1-03 口语↔术语确认（方案 §7 V1-03：「我理解成：网络故障（高）→ 确认 / 修改」）。
//
// 口径：
//   - 本端点**只做理解**：把员工口语（"电脑连不上网了，很急"）映射为可核对的 ITSM 术语
//     （分类=网络故障、优先级=高），并带证据（命中的口语片段）与置信度；
//     不生成表单、不写任何业务字段，也不调用 LLM（确定性词典，可在无 Provider 时工作）；
//   - 返回 `needsConfirmation=true`：前端渲染「我理解成：… → 确认 / 修改」，
//     用户确认/修正后才进入表单生成（A2UI）与提交，沿用「AI 只给建议、写回需显式确认」不变式；
//   - 未识别时降级为 other/medium（`matched=false`、低置信度），不阻塞、不猜测；
//   - 术语/优先级取值与工单契约对齐：type ∈ {hardware,software,network,access,other}；
//     priority ∈ {low,medium,high,urgent,critical}（词典只产出 low/medium/high/urgent）。
const interpretMaxUtteranceRunes = 500

// interpretTicketIntentRequest 是理解请求体。
type interpretTicketIntentRequest struct {
	Utterance string `json:"utterance" binding:"required"`
}

// interpretTerm 是一次术语命中（含证据片段与置信度）。
type interpretTerm struct {
	Field      string  `json:"field"`      // type | priority
	Surface    string  `json:"surface"`    // 命中的口语片段（证据）
	Term       string  `json:"term"`       // 术语标签：网络故障 / 高
	Value      string  `json:"value"`      // 规范值：network / high
	Confidence float64 `json:"confidence"` // 0~1
}

// interpretFields 是可直接带进建单表单的建议字段。
type interpretFields struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
}

// interpretResult 是理解结果（供「理解确认卡」渲染）。
type interpretResult struct {
	Summary string `json:"summary"` // 人读归一化结果：网络故障（高）
	// Matched=false 表示未命中任何术语（已按 other/medium 默认值降级）。
	Matched           bool            `json:"matched"`
	NeedsConfirmation bool            `json:"needsConfirmation"`
	Confidence        float64         `json:"confidence"`
	Terms             []interpretTerm `json:"terms"`
	Fields            interpretFields `json:"fields"`
}

// glossaryPattern 是单条口语模式的置信度（更具体的说法置信度更高）。
type glossaryPattern struct {
	text       string
	confidence float64
}

// glossaryRule 是一个术语规则（field+value）及其口语模式集合。
// exclusive=true 的规则为「否定/强约束」模式：命中即锁定该字段，不再参与置信度比较
// （例如「不着急」必须压过「急」，否则口语否定会被误判为高优先级）。
type glossaryRule struct {
	field     string
	value     string
	term      string
	exclusive bool
	patterns  []glossaryPattern
}

// interpretTypeRules 分类词典：顺序即规则优先级（同类取置信度最高；沿用具体优先）。
var interpretTypeRules = []glossaryRule{
	{
		field: "type", value: "network", term: "网络故障",
		patterns: []glossaryPattern{
			{text: "连不上网", confidence: 0.93},
			{text: "上不了网", confidence: 0.92},
			{text: "上不去网", confidence: 0.90},
			{text: "打不开网页", confidence: 0.90},
			{text: "断网", confidence: 0.90},
			{text: "没网", confidence: 0.88},
			{text: "掉线", confidence: 0.85},
			{text: "wifi", confidence: 0.80},
			{text: "无线网", confidence: 0.80},
			{text: "网线", confidence: 0.80},
			{text: "内网", confidence: 0.80},
			{text: "外网", confidence: 0.80},
			{text: "网速", confidence: 0.80},
			{text: "上网", confidence: 0.78},
			{text: "网络", confidence: 0.75},
		},
	},
	{
		field: "type", value: "access", term: "账号权限",
		patterns: []glossaryPattern{
			{text: "登录不上", confidence: 0.92},
			{text: "登录不了", confidence: 0.90},
			{text: "登不上", confidence: 0.90},
			{text: "被锁", confidence: 0.85},
			{text: "密码", confidence: 0.85},
			{text: "账号", confidence: 0.85},
			{text: "账户", confidence: 0.85},
			{text: "权限", confidence: 0.85},
			{text: "vpn", confidence: 0.85},
			{text: "授权", confidence: 0.80},
			{text: "解锁", confidence: 0.80},
		},
	},
	{
		field: "type", value: "hardware", term: "硬件故障",
		patterns: []glossaryPattern{
			{text: "打印机", confidence: 0.90},
			{text: "开不了机", confidence: 0.90},
			{text: "键盘", confidence: 0.90},
			{text: "鼠标", confidence: 0.90},
			{text: "蓝屏", confidence: 0.88},
			{text: "显示器", confidence: 0.88},
			{text: "死机", confidence: 0.85},
			{text: "笔记本", confidence: 0.85},
			{text: "主机", confidence: 0.85},
			{text: "内存", confidence: 0.85},
			{text: "硬盘", confidence: 0.85},
			{text: "硬件", confidence: 0.85},
			{text: "电脑", confidence: 0.80},
		},
	},
	{
		field: "type", value: "software", term: "软件问题",
		patterns: []glossaryPattern{
			{text: "软件", confidence: 0.85},
			{text: "许可证", confidence: 0.85},
			{text: "license", confidence: 0.85},
			{text: "闪退", confidence: 0.85},
			{text: "客户端", confidence: 0.80},
			{text: "office", confidence: 0.80},
			{text: "安装", confidence: 0.80},
			{text: "崩溃", confidence: 0.80},
			{text: "系统", confidence: 0.75},
			{text: "报错", confidence: 0.70},
		},
	},
}

// interpretPriorityRules 优先级词典。exclusive 规则（否定表达）命中即锁定，
// 避免「不着急」被「急」覆盖；「紧急」类优先于「急」类（置信度也更高）。
var interpretPriorityRules = []glossaryRule{
	{
		field: "priority", value: "urgent", term: "紧急",
		patterns: []glossaryPattern{
			{text: "非常紧急", confidence: 0.95},
			{text: "救火", confidence: 0.92},
			{text: "宕机", confidence: 0.92},
			{text: "紧急", confidence: 0.90},
			{text: "线上挂了", confidence: 0.90},
			{text: "线上故障", confidence: 0.90},
			{text: "全公司", confidence: 0.90},
			{text: "生产环境", confidence: 0.85},
			{text: "整个部门", confidence: 0.85},
			{text: "影响业务", confidence: 0.85},
			{text: "所有人", confidence: 0.82},
			{text: "asap", confidence: 0.80},
		},
	},
	{
		field: "priority", value: "low", term: "低", exclusive: true,
		patterns: []glossaryPattern{
			{text: "不着急", confidence: 0.92},
			{text: "不急", confidence: 0.90},
			{text: "不紧急", confidence: 0.85},
			{text: "有空再", confidence: 0.85},
			{text: "闲时", confidence: 0.85},
			{text: "低优先级", confidence: 0.90},
		},
	},
	{
		field: "priority", value: "high", term: "高",
		patterns: []glossaryPattern{
			{text: "很急", confidence: 0.90},
			{text: "高优先级", confidence: 0.90},
			{text: "着急", confidence: 0.85},
			{text: "无法工作", confidence: 0.85},
			{text: "没法工作", confidence: 0.85},
			{text: "影响工作", confidence: 0.80},
			{text: "尽快", confidence: 0.80},
			{text: "急", confidence: 0.80},
			{text: "快点", confidence: 0.78},
			{text: "马上", confidence: 0.75},
		},
	},
}

// interpretTypeLabels / interpretPriorityLabels 是标签兜底表（summary 与 fields 回退用）。
var interpretTypeLabels = map[string]string{
	"hardware": "硬件故障",
	"software": "软件问题",
	"network":  "网络故障",
	"access":   "账号权限",
	"other":    "其他",
}

var interpretPriorityLabels = map[string]string{
	"low":    "低",
	"medium": "中",
	"high":   "高",
	"urgent": "紧急",
}

// InterpretTicketIntent handles POST /api/v1/ai/tickets/interpret（V1-03）。
//
// 只读理解：口语 → 术语（分类/优先级）+ 证据 + 置信度；不做写操作、不生成表单。
func (h *Handler) InterpretTicketIntent(c *gin.Context) {
	var req interpretTicketIntentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.BindValidationError(c, err, "请求参数错误")
		return
	}
	utterance := strings.TrimSpace(req.Utterance)
	if utterance == "" {
		common.Fail(c, common.BadRequestCode, "utterance 不能为空")
		return
	}
	if utf8.RuneCountInString(utterance) > interpretMaxUtteranceRunes {
		common.Fail(c, common.BadRequestCode, "utterance 超长（≤500 字）")
		return
	}

	common.Success(c, interpretTicketUtterance(utterance))
}

// interpretTicketUtterance 是纯函数理解入口（无 IO、无状态，便于契约单测）。
func interpretTicketUtterance(utterance string) interpretResult {
	normalized := strings.ToLower(strings.TrimSpace(utterance))

	terms := make([]interpretTerm, 0, 2)
	if match, ok := matchGlossary(normalized, interpretTypeRules); ok {
		terms = append(terms, interpretTerm{
			Field:      "type",
			Surface:    match.surface,
			Term:       match.rule.term,
			Value:      match.rule.value,
			Confidence: match.confidence,
		})
	}
	if match, ok := matchGlossary(normalized, interpretPriorityRules); ok {
		terms = append(terms, interpretTerm{
			Field:      "priority",
			Surface:    match.surface,
			Term:       match.rule.term,
			Value:      match.rule.value,
			Confidence: match.confidence,
		})
	}

	fields := interpretFields{
		Title:       interpretTitle(utterance),
		Type:        "other",
		Priority:    "medium",
		Description: utterance,
	}
	confidenceSum := 0.0
	for _, term := range terms {
		confidenceSum += term.Confidence
		switch term.Field {
		case "type":
			fields.Type = term.Value
		case "priority":
			fields.Priority = term.Value
		}
	}

	matched := len(terms) > 0
	confidence := 0.35
	if matched {
		confidence = confidenceSum / float64(len(terms))
		if confidence > 0.95 {
			confidence = 0.95
		}
	}

	typeLabel := interpretTypeLabels[fields.Type]
	if typeLabel == "" {
		typeLabel = interpretTypeLabels["other"]
	}
	priorityLabel := interpretPriorityLabels[fields.Priority]
	if priorityLabel == "" {
		priorityLabel = interpretPriorityLabels["medium"]
	}

	return interpretResult{
		Summary:           typeLabel + "（" + priorityLabel + "）",
		Matched:           matched,
		NeedsConfirmation: true,
		Confidence:        confidence,
		Terms:             terms,
		Fields:            fields,
	}
}

// glossaryMatch 是一次词典命中（规则 + 证据片段 + 置信度）。
type glossaryMatch struct {
	rule       glossaryRule
	surface    string
	confidence float64
}

// matchGlossary 在规则集内匹配：exclusive（否定）规则先于普通规则命中即锁定；
// 其余按置信度取最高（同分保留词典顺序，即更具体的规则在前）。
func matchGlossary(utterance string, rules []glossaryRule) (glossaryMatch, bool) {
	if utterance == "" {
		return glossaryMatch{}, false
	}
	best := glossaryMatch{}
	found := false
	for _, rule := range rules {
		for _, pattern := range rule.patterns {
			if !strings.Contains(utterance, pattern.text) {
				continue
			}
			candidate := glossaryMatch{rule: rule, surface: pattern.text, confidence: pattern.confidence}
			if rule.exclusive {
				candidate.confidence = pattern.confidence + exclusiveConfidenceBoost
				if candidate.confidence > 0.99 {
					candidate.confidence = 0.99
				}
				return candidate, true
			}
			if !found || candidate.confidence > best.confidence {
				best = candidate
				found = true
			}
		}
	}
	return best, found
}

// exclusiveConfidenceBoost 让否定/强约束命中在置信度语义上高于任何普通模式（0.99 上限）。
const exclusiveConfidenceBoost = 0.08

// interpretTitle 取口语第一行前 30 个字符（与 A2UI 建单表单的标题口径一致）。
func interpretTitle(utterance string) string {
	title := utterance
	if idx := strings.IndexAny(utterance, "\r\n"); idx > 0 && idx < 100 {
		title = strings.TrimSpace(utterance[:idx])
	}
	if utf8.RuneCountInString(title) > 30 {
		title = string([]rune(title)[:30])
	}
	return title
}
