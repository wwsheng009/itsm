// Package provider 将 MCP 工具聚合为 ToolProvider 并接入既有 ToolRegistry。
//
// 契约（D5）：MCP 工具与内置工具**同源**——同一 Gate1（身份）/ Gate2（域 RBAC）/ Gate3（审批）链路、
// 同一 ToolQueue 与同一 tool_invocations 审计；不扩展 Connector 语义。
//
// 落地任务：M0-09（只读工具进面与执行）。
package provider

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"itsm-backend/ent"
	"itsm-backend/ent/mcpserver"
	"itsm-backend/mcp/client"
	"itsm-backend/mcp/manager"
	"itsm-backend/mcp/registry"
	"itsm-backend/service"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	// ProviderName 是审计/展示口径的 provider 标识。
	ProviderName = "mcp"
	// ResourceName 是 Gate2 资源位（分析报告 §5.5：Resource=mcp，Action=read|write）。
	ResourceName = "mcp"
	// DefaultMaxResultBytes 是单次调用结果的字节上限（超出截断并标记）。
	DefaultMaxResultBytes = 256 * 1024
	// maxDescriptionRunes 是描述进入工具面的长度上限（防止超大/恶意描述挤占上下文）。
	maxDescriptionRunes = 1024
)

// ToolSource 是 provider 依赖的运行时能力（生产由 manager 实现；测试可注入替身）。
type ToolSource interface {
	// EffectiveTools 返回 healthy ∧ enabled ∧ 未隔离的工具记录。
	EffectiveTools() []manager.ToolRecord
	// CallTool 按 (serverID, rawName) 路由执行，禁止用名字反推服务。
	CallTool(ctx context.Context, serverID int, rawName string, args map[string]interface{}) (*client.CallResult, error)
}

// Options 是 provider 选项。
type Options struct {
	// Enabled: mcp.enabled 全局开关；false 时工具面恒为空（零行为变化）。
	Enabled bool
	// MaxResultBytes: 结果字节上限；<=0 时用 DefaultMaxResultBytes。
	MaxResultBytes int
	// IncludeWriteTools: 是否把未标注只读的工具放入工具面。
	// 一期（M0-09）为 false——默认拒绝（D7）下新工具 read_only=false，先只读先行；
	// M1-02 接入 Gate3 审批后由 bootstrap 置 true。
	IncludeWriteTools bool
}

// Provider 实现 service.ToolProvider。
type Provider struct {
	client *ent.Client
	source ToolSource
	opts   Options
}

// New 构造 provider（client/source 为 nil 或 Enabled=false 时退化为空工具面，fail-closed）。
func New(client *ent.Client, source ToolSource, opts Options) *Provider {
	if opts.MaxResultBytes <= 0 {
		opts.MaxResultBytes = DefaultMaxResultBytes
	}
	return &Provider{client: client, source: source, opts: opts}
}

// ProviderName 实现 service.ToolProvider。
func (p *Provider) ProviderName() string { return ProviderName }

// ListTools 实现 service.ToolProvider：同一次快照内完成可见性过滤、投影与 schema 校验。
func (p *Provider) ListTools(ctx context.Context, tenantID int) []service.ToolDefinition {
	face := p.snapshot(ctx, tenantID)
	if len(face.tools) == 0 {
		return nil
	}
	out := make([]service.ToolDefinition, 0, len(face.tools))
	for index := range face.tools {
		out = append(out, face.tools[index].def)
	}
	return out
}

// Resolve 实现 service.ToolProvider：解析顺序与投影同源（registry.Resolve：canonical 精确 →
// 唯一短名 → 歧义 fail-closed）。
func (p *Provider) Resolve(ctx context.Context, tenantID int, name string) (*service.ToolDefinition, bool) {
	tool, ok := p.lookup(ctx, tenantID, name)
	if !ok {
		return nil, false
	}
	def := tool.def
	return &def, true
}

// faceTool 是工具面中的一个条目：展示定义 + 执行路由所需的 (serverID, rawName)。
type faceTool struct {
	def        service.ToolDefinition
	serverID   int
	serverName string // M0-11：审计三元组用（tool_invocations.mcp_server_name）
	rawName    string
	callable   string
	schema     *jsonschema.Resolved // 参数校验器；nil = 无 schema（不做参数校验）
}

// toolFace 是一次租户快照的产物。
type toolFace struct {
	tools       []faceTool
	byCanonical map[string]int
	registry    *registry.Registry
}

func emptyFace() *toolFace {
	return &toolFace{byCanonical: map[string]int{}, registry: registry.New()}
}

// snapshot 组装某租户的 MCP 工具面：
//
//	server.enabled(租户内) ∧ manager.EffectiveTools（healthy ∧ tool.enabled ∧ ¬quarantined）
//	∧ registry 投影未被隔离（canonical 碰撞）∧ inputSchema 可解析
//
// 任一步失败都 fail-closed（跳过该工具 / 返回空面），不影响内置工具与对话主链路。
func (p *Provider) snapshot(ctx context.Context, tenantID int) *toolFace {
	if p == nil || !p.opts.Enabled || p.client == nil || p.source == nil || tenantID <= 0 {
		return emptyFace()
	}
	servers, err := p.client.MCPServer.Query().
		Where(mcpserver.TenantIDEQ(tenantID), mcpserver.EnabledEQ(true)).
		All(ctx)
	if err != nil || len(servers) == 0 {
		return emptyFace()
	}
	nameByID := make(map[int]string, len(servers))
	for _, server := range servers {
		nameByID[server.ID] = server.Name
	}

	records := p.source.EffectiveTools()
	// 先跑投影/隔离判定（唯一实现于 mcp/registry），隔离者不得进面。
	refs := make([]registry.ToolRef, 0, len(records))
	for _, record := range records {
		name, ok := nameByID[record.ServerID]
		if !ok {
			continue // 跨租户 / 服务器未启用：fail-closed
		}
		refs = append(refs, registry.ToolRef{Server: name, RawName: record.RawName})
	}
	projection, registered := registerTools(refs)

	face := emptyFace()
	face.registry = projection
	for _, record := range records {
		serverName, ok := nameByID[record.ServerID]
		if !ok {
			continue
		}
		callable, ok := registered[serverName+"\x00"+record.RawName]
		if !ok {
			continue // 隔离或非法
		}
		resolved, argsSchema, schemaOK := resolveArgsSchema(record.InputSchema)
		if !schemaOK {
			continue // schema 不可解析：该工具不进面（隔离待复核，待 M0-07 隔离位回写）
		}
		if !record.ReadOnly && !p.opts.IncludeWriteTools {
			continue // 一期只读先行（M1-02 打开写工具面）
		}
		readOnly := record.ReadOnly
		definition := service.ToolDefinition{
			Name:         callable.CallableName,
			Description:  describe(serverName, record.Description),
			ReadOnly:     readOnly,
			Resource:     ResourceName,
			Action:       actionFor(readOnly),
			ArgsSchema:   argsSchema,
			ResultSchema: nil,
		}
		face.byCanonical[definition.Name] = len(face.tools)
		face.tools = append(face.tools, faceTool{
			def:        definition,
			serverID:   record.ServerID,
			serverName: serverName,
			rawName:    record.RawName,
			callable:   callable.CallableName,
			schema:     resolved,
		})
	}
	// 稳定排序：canonical 升序（工具面顺序稳定，利于提示缓存与断言）。
	sort.SliceStable(face.tools, func(i, j int) bool { return face.tools[i].def.Name < face.tools[j].def.Name })
	face.byCanonical = make(map[string]int, len(face.tools))
	for index := range face.tools {
		face.byCanonical[face.tools[index].def.Name] = index
	}
	return face
}

// registerTools 用 registry 完成投影 + 隔离判定：
// 返回填充后的 registry（供同名解析复用）与「server\x00raw → 可暴露工具」索引。
func registerTools(refs []registry.ToolRef) (*registry.Registry, map[string]registry.CallableTool) {
	reg := registry.New()
	result := reg.Register(refs)
	allowed := make(map[string]registry.CallableTool, len(result.Added))
	for _, tool := range result.Added {
		allowed[tool.Key()] = tool
	}
	return reg, allowed
}

// lookup 解析工具名（canonical 精确 → 唯一短名 → 歧义/未知 fail-closed）。
func (p *Provider) lookup(ctx context.Context, tenantID int, name string) (*faceTool, bool) {
	face := p.snapshot(ctx, tenantID)
	if len(face.tools) == 0 {
		return nil, false
	}
	callable, err := face.registry.Resolve(name)
	if err != nil {
		return nil, false
	}
	index, ok := face.byCanonical[callable.CallableName]
	if !ok {
		return nil, false
	}
	return &face.tools[index], true
}

func actionFor(readOnly bool) string {
	if readOnly {
		return "read"
	}
	return "write"
}

// describe 生成工具面描述：前缀来源标识、剥离控制字符、限制长度（输出按不可信数据处理）。
func describe(serverName, description string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, description)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		cleaned = "（无描述）"
	}
	runes := []rune(cleaned)
	if len(runes) > maxDescriptionRunes {
		cleaned = string(runes[:maxDescriptionRunes]) + "…"
	}
	return "[MCP:" + serverName + "] " + cleaned
}

// resolveArgsSchema 解析工具参数 schema：
//   - 空 schema → (nil, nil, true)：不阻断进面，执行时跳过参数校验（服务器自行处理）；
//   - 可解析 → (resolved, map, true)：进面并用于执行前校验；
//   - 不可解析 → (nil, nil, false)：调用方必须跳过该工具（fail-closed，杜绝畸形 schema 进面）。
func resolveArgsSchema(raw json.RawMessage) (*jsonschema.Resolved, map[string]interface{}, bool) {
	if len(raw) == 0 {
		return nil, nil, true
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, nil, false
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, nil, false
	}
	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil, nil, false
	}
	return resolved, asMap, true
}
