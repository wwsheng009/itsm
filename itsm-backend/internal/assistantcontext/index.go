// Package assistantcontext 提供页面助手上下文注册表的内嵌副本（D1-01）。
//
// 数据来源：itsm-frontend/scripts/gen-page-manifest.mjs 从
// itsm-frontend/src/lib/ai/page-context/registry.ts 生成的 assistant-registry.json。
// 前端注册表是唯一真源；本包只做只读加载，不在本层做权限判断
// （可见 Bot / 工具授权 / RBAC 过滤由 handlers/ai 的 /ai/assistant/context 负责）。
//
// 口径：
//   - 未知 pageKey → Declaration 返回 nil（端点返回 known=false，前端回退静态基线）；
//   - 只读、零文件系统依赖（go:embed）；解析失败 → 空注册表（端点按 known=false 降级）。
package assistantcontext

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed assistant-registry.json
var registryJSON []byte

// Action 是一条建议（任务卡/问题卡）的完整声明。
type Action struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // task | question
	Label string `json:"label"`
	// Prompt 为点击后发送的提示词（零输入发起）。
	Prompt string `json:"prompt"`
	// ReadOnly：零输入可执行必须为只读/plan（方案 §D4 不变量）。
	ReadOnly bool `json:"readOnly"`
	// Tools：该建议依赖的工具名（可选）。服务端据此做「Bot 授权 ∩ RBAC ∩ 风险」过滤；
	// 未声明依赖的建议只做场景可见性过滤。
	Tools []string `json:"tools,omitempty"`
}

// Declaration 是一个页面的助手声明（L1 页面声明）。
type Declaration struct {
	PageKey     string   `json:"pageKey"`
	Pattern     string   `json:"pattern"`
	Title       string   `json:"title"`
	Scene       string   `json:"scene"`
	Entrypoint  string   `json:"entrypoint"`
	Suggestions []Action `json:"suggestions"`
}

type registryFile struct {
	GeneratedAt string        `json:"generatedAt"`
	Pages       []Declaration `json:"pages"`
}

// Registry 是加载后的注册表索引（进程内单例由 Load 保证）。
type Registry struct {
	GeneratedAt string
	pages       []Declaration
	byKey       map[string]Declaration
}

var (
	once sync.Once
	reg  *Registry
)

// Load 返回进程内注册表（解析失败 → 空注册表，端点按 known=false 降级）。
func Load() *Registry {
	once.Do(func() {
		reg = buildRegistry()
	})
	return reg
}

func buildRegistry() *Registry {
	r := &Registry{byKey: map[string]Declaration{}}

	var raw registryFile
	if err := json.Unmarshal(registryJSON, &raw); err != nil {
		return r
	}
	r.GeneratedAt = raw.GeneratedAt
	for _, page := range raw.Pages {
		if page.PageKey == "" {
			continue
		}
		r.pages = append(r.pages, page)
		r.byKey[page.PageKey] = page
	}
	return r
}

// Declaration 返回页面声明（未知 pageKey 返回 nil）。
func (r *Registry) Declaration(pageKey string) *Declaration {
	if r == nil || pageKey == "" {
		return nil
	}
	page, ok := r.byKey[pageKey]
	if !ok {
		return nil
	}
	return &page
}

// Len 返回注册表页面数（诊断/测试用）。
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.pages)
}
