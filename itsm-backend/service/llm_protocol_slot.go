package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"itsm-backend/internal/llm/protocol"
)

// 本文件是主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-8 的交付物：
// 协议槽位（4 值枚举常量 + (protocol, variant) → 既有实现映射 + 校验 + 能力位 + adapter_options 约束）。
//
// 口径（§3.1.4 / §11.2）：
//   - 4 值协议枚举：openai_chat_completions / openai_responses / anthropic_messages / google_gemini；
//   - 已适配：openai_chat_completions（含 azure / ollama 变体）、anthropic_messages（含 minimax 变体）、
//     openai_responses（PA-3）与 google_gemini（PA-4）均由协议包同一适配器承载
//     （一协议一实现，变体只是构造期选项）；4 值枚举在 PA-4 收官后全部 implemented=true，
//     槽位分支（ErrProtocolNotImplemented → AI_PROTOCOL_NOT_IMPLEMENTED(422)）保留给后续协议扩展，
//     当前仅"未知协议/不在白名单的变体"可达；
//   - variant 白名单：openai_chat_completions = {"", azure, ollama}；anthropic_messages = {"", minimax}；
//     openai_responses = {""}；google_gemini = {""}；
//   - adapter_options：JSON 对象、原始串 ≤4KB、键名黑名单（密钥只允许走 encrypted_api_key）；
//   - 能力位（supportsStream / supportsTools / supportsReasoning / implemented）按承载实现真实填写
//     （口径见 LLMCapabilities 注释；开关感知的收敛在 PA-5）。
//
// 边界（§11.3 启动门禁）：本文件只提供纯函数，不修改 NewProviderFromConfig 与既有 4 个实现分支；
// "适配器优先、旧分支回退"的构建分派在 service/llm_registry.go（BE-9），这里只给出回退侧的映射表。
// 消费方：BE-2（Registry 构建 slot 时的校验）、BE-4（DTO 枚举与 available 响应）、FE-2（下拉项）。

const (
	// 协议枚举（与 internal/llm/protocol 同源，主计划 §2.2 D3）。
	LLMProtocolOpenAIChatCompletions = protocol.ProtocolOpenAIChatCompletions
	LLMProtocolOpenAIResponses       = protocol.ProtocolOpenAIResponses
	LLMProtocolAnthropicMessages     = protocol.ProtocolAnthropicMessages
	LLMProtocolGoogleGemini          = protocol.ProtocolGoogleGemini

	// 兼容变体白名单取值（空字符串 = 标准实现）。
	LLMVariantDefault = protocol.VariantDefault
	LLMVariantAzure   = protocol.VariantAzure
	LLMVariantOllama  = protocol.VariantOllama
	LLMVariantMiniMax = protocol.VariantMiniMax

	// LLMAdapterOptionsMaxBytes 是 adapter_options 原始 JSON 的字节上限（主计划 §3.1.1：Text ≤4KB）。
	LLMAdapterOptionsMaxBytes = 4096

	// llmAdapterOptionsMaxDepth 限制嵌套层级，避免异常深层结构落库。
	llmAdapterOptionsMaxDepth = 6
)

// 槽位校验错误哨兵（主计划 §3.4：创建/更新返回 422；由 BE-4 在 DTO 层映射为统一错误响应）。
var (
	// ErrProtocolInvalid 协议枚举非法（不在 4 值内）。
	ErrProtocolInvalid = errors.New("AI_PROTOCOL_INVALID")
	// ErrProtocolVariantInvalid variant 不在该协议的白名单内。
	ErrProtocolVariantInvalid = errors.New("AI_PROTOCOL_VARIANT_INVALID")
	// ErrAdapterOptionsInvalid adapter_options 非法（非 JSON 对象 / 超限 / 含敏感键 / 嵌套过深）。
	ErrAdapterOptionsInvalid = errors.New("AI_ADAPTER_OPTIONS_INVALID")
)

// LLMCapabilities 实例能力位（主计划 §3.4 `available` 响应 / §11.2 槽位 4）。
// 取值按现有实现真实填写（PA-5 收敛为「开关感知」的真实值，本任务先按各 (协议,变体)
// 当前承载实现填写）：
//   - openai_chat_completions / "" ：OpenAIProvider 同时具备流式与工具调用；
//   - openai_chat_completions / azure / ollama 与 anthropic_messages 两变体：
//     旧分支当前只有非流式 Chat（PA-1/PA-2 的适配器能力位在 PA-5 统一收敛）；
//   - openai_responses / ""：PA-3 适配器为唯一承载（无旧分支），流式/工具/推理全支持；
//   - google_gemini / ""：PA-4 适配器为唯一承载（无旧分支），流式/工具/推理全支持。
type LLMCapabilities struct {
	SupportsStream    bool `json:"supportsStream"`
	SupportsTools     bool `json:"supportsTools"`
	SupportsReasoning bool `json:"supportsReasoning"`
	// Implemented 协议在 P0 是否已实现（false = 槽位，前端置灰、API 422）。
	Implemented bool `json:"implemented"`
}

// LLMProtocolOption 协议下拉选项（DTO / 前端消费；顺序与 §3.1.4 映射表一致）。
type LLMProtocolOption struct {
	Protocol     string          `json:"protocol"`
	Implemented  bool            `json:"implemented"`
	Variants     []string        `json:"variants"`
	Capabilities LLMCapabilities `json:"capabilities"`
}

// llmProtocolVariantSlot 是 (协议, 变体) 的一行映射（§3.1.4 映射表）。
type llmProtocolVariantSlot struct {
	variant        string
	legacyProvider string
	capabilities   LLMCapabilities
}

// llmProtocolSlot 是一个协议槽位（含变体白名单与映射）。
type llmProtocolSlot struct {
	protocol    string
	implemented bool
	variants    []llmProtocolVariantSlot
}

// llmProtocolSlots 是 §3.1.4 映射表的代码化：由 BE-8 单测逐行锁定。
var llmProtocolSlots = []llmProtocolSlot{
	{
		protocol:    LLMProtocolOpenAIChatCompletions,
		implemented: true,
		variants: []llmProtocolVariantSlot{
			{
				variant:        LLMVariantDefault,
				legacyProvider: "openai",
				capabilities: LLMCapabilities{
					SupportsStream:    true,
					SupportsTools:     true,
					SupportsReasoning: true,
					Implemented:       true,
				},
			},
			{
				variant:        LLMVariantAzure,
				legacyProvider: "azure",
				capabilities:   LLMCapabilities{Implemented: true},
			},
			{
				variant:        LLMVariantOllama,
				legacyProvider: "local",
				capabilities:   LLMCapabilities{Implemented: true},
			},
		},
	},
	{
		protocol:    LLMProtocolAnthropicMessages,
		implemented: true,
		variants: []llmProtocolVariantSlot{
			{variant: LLMVariantDefault, legacyProvider: "minimax", capabilities: LLMCapabilities{Implemented: true}},
			{variant: LLMVariantMiniMax, legacyProvider: "minimax", capabilities: LLMCapabilities{Implemented: true}},
		},
	},
	{
		protocol:    LLMProtocolOpenAIResponses,
		implemented: true,
		variants: []llmProtocolVariantSlot{
			{
				variant: LLMVariantDefault,
				// 空 legacyProvider = 无旧分支可回退（PA-3 载体决策：官方 Responses API）。
				// 适配器不可用时该组合直接不可用，不得静默回退到 openai_chat_completions；
				// 由 service/llm_registry.go 的 buildProvider 显式拦下。
				legacyProvider: "",
				capabilities: LLMCapabilities{
					SupportsStream:    true,
					SupportsTools:     true,
					SupportsReasoning: true,
					Implemented:       true,
				},
			},
		},
	},
	{
		protocol:    LLMProtocolGoogleGemini,
		implemented: true,
		variants: []llmProtocolVariantSlot{
			{
				variant: LLMVariantDefault,
				// 空 legacyProvider = 无旧分支可回退（PA-4：Gemini v1beta 适配器为唯一承载）。
				legacyProvider: "",
				capabilities: LLMCapabilities{
					SupportsStream:    true,
					SupportsTools:     true,
					SupportsReasoning: true,
					Implemented:       true,
				},
			},
		},
	},
}

// NormalizeLLMProtocol 归一化协议枚举（去空白 + 小写），用于比较与落库前校验。
func NormalizeLLMProtocol(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// NormalizeLLMVariant 归一化变体（去空白 + 小写；空串保持空串 = 标准实现）。
func NormalizeLLMVariant(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// SupportedLLMProtocols 返回 4 值协议枚举（顺序固定 = §3.1.4 映射表顺序）。
func SupportedLLMProtocols() []string {
	names := make([]string, 0, len(llmProtocolSlots))
	for _, slot := range llmProtocolSlots {
		names = append(names, slot.protocol)
	}
	return names
}

// LLMProtocolOptions 返回协议下拉数据（含 implemented 与 variant 白名单），供 BE-4 DTO / FE-2 使用。
// 空变体在 Variants 中以 "" 保留（前端映射为「标准」选项）。
func LLMProtocolOptions() []LLMProtocolOption {
	options := make([]LLMProtocolOption, 0, len(llmProtocolSlots))
	for _, slot := range llmProtocolSlots {
		options = append(options, LLMProtocolOption{
			Protocol:     slot.protocol,
			Implemented:  slot.implemented,
			Variants:     slot.variantNames(),
			Capabilities: slot.defaultCapabilities(),
		})
	}
	return options
}

// SupportedLLMVariants 返回协议的变体白名单（含空串标准项）；未实现协议返回空切片。
func SupportedLLMVariants(protocolName string) []string {
	slot, ok := findLLMProtocolSlot(protocolName)
	if !ok {
		return []string{}
	}
	return slot.variantNames()
}

// IsImplementedLLMProtocol 报告协议在 P0 是否有实现（false = 槽位：创建/更新返回 422）。
func IsImplementedLLMProtocol(protocolName string) bool {
	slot, ok := findLLMProtocolSlot(protocolName)
	return ok && slot.implemented
}

// ValidateLLMProtocolVariant 校验 (协议, 变体) 组合：
//   - 协议不在 4 值内 → ErrProtocolInvalid；
//   - 协议合法但 P0 未实现 → ErrProtocolNotImplemented（wrap，错误码 AI_PROTOCOL_NOT_IMPLEMENTED）；
//   - 变体不在白名单内 → ErrProtocolVariantInvalid。
//
// 校验通过返回 nil；协议/变体均做去空白 + 小写归一。
func ValidateLLMProtocolVariant(protocolName, variant string) error {
	slot, ok := findLLMProtocolSlot(protocolName)
	if !ok {
		return fmt.Errorf("%w: 未知协议 %q（支持: %s）",
			ErrProtocolInvalid, strings.TrimSpace(protocolName), strings.Join(SupportedLLMProtocols(), ", "))
	}
	if !slot.implemented {
		return fmt.Errorf("%w: 协议 %q 属槽位（P0 未实现，见独立计划 llm-protocol-adapter-plan）",
			ErrProtocolNotImplemented, slot.protocol)
	}
	if _, ok := slot.variantSlot(variant); !ok {
		return fmt.Errorf("%w: 协议 %q 不支持变体 %q（白名单: %s）",
			ErrProtocolVariantInvalid, slot.protocol, NormalizeLLMVariant(variant),
			strings.Join(quoteVariants(slot.variantNames()), ", "))
	}
	return nil
}

// MapProtocolToLegacyProvider 把 (协议, 变体) 映射到既有 NewProviderFromConfig 的 provider 名（§3.1.4）。
//
// 该函数只覆盖"旧分支回退侧"：适配器优先的构建分派由 service/llm_registry.go（BE-9）决定，
// 调用方应先尝试适配器、失败后再用本映射回退。校验失败语义与 ValidateLLMProtocolVariant 一致。
func MapProtocolToLegacyProvider(protocolName, variant string) (string, error) {
	if err := ValidateLLMProtocolVariant(protocolName, variant); err != nil {
		return "", err
	}
	slot, _ := findLLMProtocolSlot(protocolName)
	item, _ := slot.variantSlot(variant)
	return item.legacyProvider, nil
}

// LLMProtocolCapabilities 返回 (协议, 变体) 的能力位；协议/变体非法时返回与校验一致的错误。
func LLMProtocolCapabilities(protocolName, variant string) (LLMCapabilities, error) {
	if err := ValidateLLMProtocolVariant(protocolName, variant); err != nil {
		return LLMCapabilities{}, err
	}
	slot, _ := findLLMProtocolSlot(protocolName)
	item, _ := slot.variantSlot(variant)
	return item.capabilities, nil
}

// ValidateAdapterOptions 校验 adapter_options 原始 JSON：
//   - 空串 / 空白 / "null" 视为未设置（合法）；
//   - 原始串长度 ≤ LLMAdapterOptionsMaxBytes（4KB）；
//   - 必须是合法 JSON 对象（拒绝数组/标量/尾随内容）且不得有超出 llmAdapterOptionsMaxDepth 的嵌套；
//   - 键名命中敏感黑名单（api_key / token / secret / authorization / password 等）一律拒绝：
//     密钥只允许写入 encrypted_api_key（§3.5）。
//
// 失败一律 wrap ErrAdapterOptionsInvalid（匿名键名可回显，敏感键名仅提示键名、不含键值）。
func ValidateAdapterOptions(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil
	}
	if len(raw) > LLMAdapterOptionsMaxBytes {
		return fmt.Errorf("%w: 超过 %d 字节上限（实际 %d）",
			ErrAdapterOptionsInvalid, LLMAdapterOptionsMaxBytes, len(raw))
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: 不是合法 JSON: %v", ErrAdapterOptionsInvalid, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}

	object, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: 顶层必须是 JSON 对象", ErrAdapterOptionsInvalid)
	}
	return validateAdapterOptionsValue(object, 1)
}

// IsSensitiveAdapterOptionKey 报告 adapter_options 键名是否疑似密钥载体。
//
// 判定口径（避免误伤合法参数）：
//   - 键名按 `_` / `-` / `.` / 空格 / 大小写驼峰切分为单词后，任一单词命中黑名单（key/token/secret/...）；
//   - 或归一化后的键名整体包含强敏感子串（apikey / accesskey / privatekey / secret / password / ...）。
//
// 因而 `max_tokens`、`num_tokens`、`api_version`、`keep_alive` 等合法参数不会被误判。
func IsSensitiveAdapterOptionKey(name string) bool {
	components := splitAdapterOptionKey(name)
	sensitiveWords := map[string]struct{}{
		"key": {}, "token": {}, "secret": {}, "password": {}, "passwd": {},
		"authorization": {}, "auth": {}, "credential": {}, "credentials": {},
		"bearer": {}, "apikey": {}, "accesskey": {}, "privatekey": {}, "secretkey": {},
	}
	for _, component := range components {
		if _, hit := sensitiveWords[component]; hit {
			return true
		}
	}

	normalized := strings.Join(components, "")
	strongMarkers := []string{
		"apikey", "accesskey", "privatekey", "secretkey", "sessionkey",
		"secret", "password", "passwd", "credential", "authorization",
	}
	for _, marker := range strongMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func findLLMProtocolSlot(protocolName string) (llmProtocolSlot, bool) {
	normalized := NormalizeLLMProtocol(protocolName)
	for _, slot := range llmProtocolSlots {
		if slot.protocol == normalized {
			return slot, true
		}
	}
	return llmProtocolSlot{}, false
}

func (s llmProtocolSlot) variantSlot(variant string) (llmProtocolVariantSlot, bool) {
	normalized := NormalizeLLMVariant(variant)
	for _, item := range s.variants {
		if item.variant == normalized {
			return item, true
		}
	}
	return llmProtocolVariantSlot{}, false
}

func (s llmProtocolSlot) variantNames() []string {
	names := make([]string, 0, len(s.variants))
	for _, item := range s.variants {
		names = append(names, item.variant)
	}
	return names
}

// defaultCapabilities 返回协议默认变体（空变体）的能力位；槽位返回零值（Implemented=false）。
func (s llmProtocolSlot) defaultCapabilities() LLMCapabilities {
	if item, ok := s.variantSlot(LLMVariantDefault); ok {
		return item.capabilities
	}
	return LLMCapabilities{}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: 存在多余内容（只允许单个 JSON 对象）", ErrAdapterOptionsInvalid)
		}
		return fmt.Errorf("%w: 尾部不是合法 JSON: %v", ErrAdapterOptionsInvalid, err)
	}
	return nil
}

func validateAdapterOptionsValue(value any, depth int) error {
	if depth > llmAdapterOptionsMaxDepth {
		return fmt.Errorf("%w: 嵌套层级超过 %d", ErrAdapterOptionsInvalid, llmAdapterOptionsMaxDepth)
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if IsSensitiveAdapterOptionKey(key) {
				return fmt.Errorf("%w: 键名 %q 疑似密钥载体，密钥请使用 encrypted_api_key",
					ErrAdapterOptionsInvalid, key)
			}
			if err := validateAdapterOptionsValue(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateAdapterOptionsValue(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// splitAdapterOptionKey 把键名切分为小写单词：分隔符（非字母数字）与驼峰边界都算切点。
func splitAdapterOptionKey(name string) []string {
	var (
		components []string
		current    strings.Builder
		runes      = []rune(name)
	)
	flush := func() {
		if current.Len() > 0 {
			components = append(components, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for index, symbol := range runes {
		switch {
		case !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol):
			flush()
		case unicode.IsUpper(symbol) && index > 0:
			previous := runes[index-1]
			if unicode.IsLower(previous) || unicode.IsDigit(previous) {
				flush()
			}
			current.WriteRune(symbol)
		default:
			current.WriteRune(symbol)
		}
	}
	flush()
	return components
}

func quoteVariants(variants []string) []string {
	quoted := make([]string, 0, len(variants))
	for _, variant := range variants {
		if variant == "" {
			quoted = append(quoted, `""(标准)`)
			continue
		}
		quoted = append(quoted, variant)
	}
	return quoted
}
