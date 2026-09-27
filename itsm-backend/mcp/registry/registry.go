// 投影索引：注册、隔离、解析（纯内存、纯逻辑、无 IO）。
//
// 线程安全：注册表会被连接生命周期（manager）与请求路径并发访问，所有导出方法均加锁。
package registry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ToolRef 描述一台 MCP 服务器返回的一个工具（注册输入）。
type ToolRef struct {
	Server  string // 服务器稳定标识：[a-z0-9_-]{1,32}
	RawName string // MCP 服务器返回的原始工具名
}

// CallableTool 是投影后的可执行工具身份。
// 执行归一化：调用方必须使用 Server + RawName 路由到对应客户端，禁止用名字反推服务。
type CallableTool struct {
	CallableName string // mcp__<server>__<tool>（对模型/策略/审计可见的唯一名）
	Server       string
	RawName      string
}

// Key 返回存储/审计口径的复合键：`server\x00raw_name`。
func (t CallableTool) Key() string { return t.Server + "\x00" + t.RawName }

// RegisterResult 汇总一次注册的产出。
type RegisterResult struct {
	Added       []CallableTool    // 本次成功投影并可暴露的工具（按输入顺序）
	Quarantined []QuarantinedTool // 本次被隔离的工具
	Duplicates  []CallableTool    // 同一 (server, raw_name) 重复注册（幂等忽略，非冲突）
}

// Registry 维护工具投影索引；由 manager 在连接建立/刷新时重建（Clear + Register）。
type Registry struct {
	mu          sync.RWMutex
	byKey       map[string]CallableTool    // server\x00raw → 工具
	byCanonical map[string]string          // canonical → key
	byRaw       map[string][]string        // 原始短名 → keys（按注册顺序）
	quarantine  map[string]QuarantinedTool // key → 隔离记录
}

// New 创建空注册表。
func New() *Registry {
	return &Registry{
		byKey:       make(map[string]CallableTool),
		byCanonical: make(map[string]string),
		byRaw:       make(map[string][]string),
		quarantine:  make(map[string]QuarantinedTool),
	}
}

// Register 注册一批工具（幂等）。
//
// 语义：
//   - 同一 (server, raw_name) 重复注册 → Duplicates（幂等忽略，不覆盖、不隔离）；
//   - canonical 完全碰撞 → 后注册者进入 quarantine（不覆盖既有，可诊断）；
//   - 非法服务器标识 / 空工具名 → 直接隔离；
//   - 成功后清除该键的历史隔离记录（隔离解除路径之一：Unregister 后重新注册）。
func (r *Registry) Register(refs []ToolRef) RegisterResult {
	var result RegisterResult

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ref := range refs {
		server := strings.TrimSpace(ref.Server)
		rawName := strings.TrimSpace(ref.RawName)

		if !ServerNamePattern.MatchString(server) {
			result.Quarantined = append(result.Quarantined, QuarantinedTool{
				Server:  server,
				RawName: rawName,
				Reason:  ReasonInvalidServerName,
				Detail:  "服务器标识必须匹配 ^[a-z0-9_-]{1,32}$",
			})
			continue
		}
		if rawName == "" {
			result.Quarantined = append(result.Quarantined, QuarantinedTool{
				Server:  server,
				RawName: rawName,
				Reason:  ReasonEmptyToolName,
				Detail:  "原始工具名为空（trim 后）",
			})
			continue
		}

		key := server + "\x00" + rawName
		if existing, ok := r.byKey[key]; ok {
			result.Duplicates = append(result.Duplicates, existing)
			continue
		}

		canonical := CanonicalToolName(server, rawName)
		if ownerKey, ok := r.byCanonical[canonical]; ok {
			owner := r.byKey[ownerKey]
			quarantined := QuarantinedTool{
				Server:       server,
				RawName:      rawName,
				CallableName: canonical,
				Reason:       ReasonCanonicalCollision,
				Detail:       fmt.Sprintf("canonical 名与 %s/%s 碰撞，后注册者隔离", owner.Server, owner.RawName),
			}
			r.quarantine[key] = quarantined
			result.Quarantined = append(result.Quarantined, quarantined)
			continue
		}

		tool := CallableTool{CallableName: canonical, Server: server, RawName: rawName}
		r.byKey[key] = tool
		r.byCanonical[canonical] = key
		r.byRaw[rawName] = append(r.byRaw[rawName], key)
		delete(r.quarantine, key)
		result.Added = append(result.Added, tool)
	}

	return result
}

// Unregister 移除单个工具（服务器下线 / 工具消失时调用）；同步清理其隔离记录。
func (r *Registry) Unregister(server, rawName string) {
	server = strings.TrimSpace(server)
	rawName = strings.TrimSpace(rawName)
	key := server + "\x00" + rawName

	r.mu.Lock()
	defer r.mu.Unlock()

	tool, ok := r.byKey[key]
	if !ok {
		return
	}
	delete(r.byKey, key)
	delete(r.byCanonical, tool.CallableName)
	if keys := r.byRaw[rawName]; len(keys) > 0 {
		remaining := make([]string, 0, len(keys))
		for _, candidate := range keys {
			if candidate != key {
				remaining = append(remaining, candidate)
			}
		}
		if len(remaining) == 0 {
			delete(r.byRaw, rawName)
		} else {
			r.byRaw[rawName] = remaining
		}
	}
	delete(r.quarantine, key)
}

// Clear 清空全部索引与隔离记录（连接重建前调用，避免残留投影）。
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byKey = make(map[string]CallableTool)
	r.byCanonical = make(map[string]string)
	r.byRaw = make(map[string][]string)
	r.quarantine = make(map[string]QuarantinedTool)
}

// List 返回全部可暴露工具（按 canonical 名排序，结果稳定）。
func (r *Registry) List() []CallableTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]CallableTool, 0, len(r.byKey))
	for _, tool := range r.byKey {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].CallableName < tools[j].CallableName })
	return tools
}

// ListQuarantined 返回隔离工具诊断快照（按 server、raw_name 排序）。
func (r *Registry) ListQuarantined() []QuarantinedTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]QuarantinedTool, 0, len(r.quarantine))
	for _, item := range r.quarantine {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Server == items[j].Server {
			return items[i].RawName < items[j].RawName
		}
		return items[i].Server < items[j].Server
	})
	return items
}

// Resolve 解析调用名（解析顺序，逐条由解析测试锁定）：
//  1. canonical 精确匹配优先；
//  2. 原始短名仅唯一命中时可用；
//  3. 多候选返回 *AmbiguousToolError（fail-closed，要求改用 canonical）；
//  4. 未找到返回 ErrNotFound，空名返回 ErrEmptyName。
//
// 解析成功后调用方必须以返回值的 (Server, RawName) 路由，杜绝串服务。
func (r *Registry) Resolve(name string) (CallableTool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CallableTool{}, ErrEmptyName
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if key, ok := r.byCanonical[name]; ok {
		return r.byKey[key], nil
	}

	keys := r.byRaw[name]
	switch len(keys) {
	case 1:
		return r.byKey[keys[0]], nil
	case 0:
		return CallableTool{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	default:
		candidates := make([]string, 0, len(keys))
		seen := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			callable := r.byKey[key].CallableName
			if _, exists := seen[callable]; exists {
				continue
			}
			seen[callable] = struct{}{}
			candidates = append(candidates, callable)
		}
		sort.Strings(candidates)
		return CallableTool{}, &AmbiguousToolError{Name: name, Candidates: candidates}
	}
}
