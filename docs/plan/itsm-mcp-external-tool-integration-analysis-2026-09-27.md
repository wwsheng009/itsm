# ITSM 外部工具（MCP）接入与业务闭环审查

> 文档类型：分析报告（参考实现拆解 + 接入设计 + 闭环与完整度审查）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`（React）
> 目标读者：后端、前端、测试、产品、运维
> 关联文档：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（本报告对应实施方案：实施步骤与验收标准）、`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`（阶段一：Bot 能力落地分析）、`docs/plan/llm-protocol-adapter-plan.md`、`docs/articles/07-ai-native-architecture-guidance-harness-skill.md`、`AGENTS.md`、`ROADMAP.md`
> 外部参考（只读参考，不引入代码依赖）：`E:\projects\ai-agent-runtime`（HEAD `29eeb62d`）——`docs/mcp/mcp-tool-llm-integration.md`、`docs/plan/mcp-management-ui-plan.md`、`backend/internal/mcp/**`、`frontend/src/components/workspace/settings/**`、`frontend/src/types/runtime/mcp.ts`
> 核查基线：`feat/vite-migration`，HEAD `7442fad5`；工作树含未提交改动（与阶段一报告 §3.5 相同基线）
> 核查方式：静态代码与文档交叉核对；本轮未编译、未运行测试。ITSM 侧 MCP 相关代码尚不存在（2026-09-27 大小写不敏感检索 `itsm-backend`/`itsm-frontend` 源码，无 MCP 相关命中），本文属**设计态**报告，凡未核实处均标注【未核实】
> 状态口径：沿用 ai-gateway 完整度审查的五级状态 `implemented / unit_verified / integration_verified / flow_verified / accepted`（定义见阶段一报告 §2.6）

---

## 0. 结论先行（TL;DR）

**一句话**：ITSM 工具层已具备「注册表 + 域 RBAC + 写工具审批 + 审计」的成熟主干，缺的是**外部工具（MCP）接入面**；建议按「远程优先、只读先行、默认拒绝、复用既有审批与审计」接入，把 MCP 工具并入与内置工具同源的执行流水线，而不是单开旁路。

**6 条关键判断**：

1. **协议形态做减法**：主通道选 **Streamable HTTP**（MCP 现行远程传输，官方 Go SDK 已支持），兼容 **SSE**（存量部署）；**stdio 仅限私有化/平台级**（须沙箱 + 白名单），WebSocket 非规范必需、一期不做。参考实现四类全支持并做了别名归一化（`backend/internal/mcp/transport/transport.go:78-107`），ITSM 不需要同款复杂度。
2. **接入位置不进 Connector 框架**：Connector 的 `Connector` 接口是「消息收发」语义（`itsm-backend/connector/connector.go:231-248`），无法表达「列工具 / 调工具 / 工具面治理」；应新建 `itsm-backend/mcp/` 子系统，但复用 Connector 的**凭据加密、租户隔离、生命周期与权限范式**（`ent/schema/connector_config.go`、`handlers/connector/handler.go`）。
3. **运行时同源治理**：MCP 工具经 `ToolProvider` 适配器并入现有 `service.ToolRegistry` 工具面，沿用 Gate1/2/3（`handlers/ai/service.go:113-159`）；MCP 工具**默认按非只读处理**（必须审批），只读分类必须由管理员显式标注，不做自动信任。
4. **命名确定性隔离**：MCP 工具统一投影为 `mcp__<server>__<tool>`；非法字符/超长哈希化，投影碰撞 fail-closed 隔离（借鉴参考实现 §2.2/§2.5 的 quarantine 机制，但 ITSM 不引入「不重名就保留原名」的兼容逻辑）。
5. **前端两扇门**：管理员侧建议独立 `/admin/mcp-servers` 页（Tabs：服务器 / 工具治理 / 健康与事件），范式复用 `admin/connectors`（市场列表 + Provision 弹窗 + 测试连接 + 健康状态）与 `admin/system-config/llm-provider-settings.tsx`；用户侧在 `AIChat` 增加「工具调用时间线 + 写工具确认抽屉」，并增强 `/ai/approval` 的 MCP 来源标识——**不向普通用户开放自带 MCP 服务器**。
6. **闭环结论**：设计后主干闭环成立，但落地有两个硬前置：**(a)** 阶段一 G1（工具元数据）与 G7（审计回填）先行或并行；**(b)** 出站安全（SSRF 防护 / 域名白名单 / 输出上限）与 stdio 沙箱必须在 M0 一并交付。详见 §6 与 §7。

---

## 1. 背景与目标

### 1.1 任务来源

阶段一报告《ITSM Bot 能力落地分析》交付后，任务范围在「工具层面」上追加：

- 接入**外部工具**，重点是 **MCP（Model Context Protocol）工具层**，覆盖 MCP 的**几种协议形态**；
- 参考 `E:\projects\ai-agent-runtime` 的既有实现（该仓库有一套完整、已落地的 MCP 栈）；
- 补齐**前端接入**设计：用户使用侧页面 + 管理员配置页面；
- 对整体方案做**业务闭环审查与功能完整性审查**。

### 1.2 本文要回答的四个问题

1. ai-agent-runtime 的 MCP 实现里，哪些契约与工程细节值得直接借鉴、哪些不应照搬？
2. ITSM 现有工具层（ToolRegistry / 审批 / 审计 / 前端）距离「安全接入 MCP」还差什么？
3. 后端与前端各需要新增/改造哪些组件、数据模型、API 与页面，才能形成闭环？
4. 以五级验收口径衡量，当前完整度是多少、分期落地后每一项应达到什么状态？

### 1.3 范围界定

- **能力范围**：一期只做 MCP 的 **Tools** 原语（`tools/list` + `tools/call`，含工具级启停与治理）；`resources` / `prompts` / `sampling` / `roots` 一期不做（参考实现也只额外暴露了一个 `list_mcp_resources` 元工具，`backend/internal/llm/adapter/mcp_meta_tools.go:4-26`，说明资源面并非工具接入的必要条件）。
- **接入对象**：以**管理员配置的租户级/平台级 MCP 服务器**为唯一来源；不开放终端用户自带服务器（C 端 App 场景除外，见 §9 开放问题）。
- **不引入代码依赖**：沿用 ITSM 对参考仓库的既有「只读参考」先例（`docs/plan/llm-protocol-adapter-plan.md`），不复制代码、不引入私有包；如采纳官方 `modelcontextprotocol/go-sdk`（参考实现 `backend/go.mod:16` 使用 `v1.4.0`），作为新的公开依赖单独评审。

---

## 2. 参考实现拆解：ai-agent-runtime 的 MCP 栈

> 本节事实来自 `E:\projects\ai-agent-runtime`（HEAD `29eeb62d`），行号均指该仓库内文件。引用时以 `ai-agent-runtime:` 语义前缀区分（正文中直接写相对路径）。

### 2.1 传输层：四类协议 + 别名归一化

参考实现了 4 类传输，统一封装官方 Go SDK 的 `mcp.Transport`：

| 传输 | 实现 | 关键机制 | 代码锚点 |
| --- | --- | --- | --- |
| `stdio` | `mcp.CommandTransport` | 子进程树守卫（Windows Job Object / Unix 进程组）、stderr 尾缓冲诊断、环境变量注入 | `backend/internal/mcp/transport/transport.go:109-191`；`transport/stdio_tree*.go`、`transport/stdio_stderr.go` |
| `sse` | `mcp.SSEClientTransport` | HTTP 头注入（支持按 env 回退）、OAuth RoundTripper | `transport/transport.go:193-229`；`transport/oauth_roundtripper.go` |
| `streamable` | `mcp.StreamableClientTransport` | 现行远程标准传输；别名（如 `streamableHttp`）归一 | `transport/transport.go:78-107`；`transport/streamable.go`；`transport/remote_headers_test.go:38-63` |
| `websocket` | 自实现 `WSClientTransport`（连接/读写/关闭/SessionID） | 非 MCP 规范必需，自定义扩展 | `transport/websocket.go:26-173` |

工程细节（值得借鉴的稳定性设计）：

- **统一 Transport 抽象**：`Transport` 接口暴露 `Type()/Config()/AddLifecycleObserver()/ToMCPSdkTransport()`（`transport/transport.go:20-27`），便于观测与测试。
- **stdout 协议纯净性**：stdio 的 stderr 单独收集为尾缓冲，避免污染 JSON-RPC 通道（`transport/transport.go:143-146`）。
- **进程树守卫降级可观测**：守卫失败时发出生命周期事件（如 `mcp.stdio.tree_guard_degraded`，`transport/transport.go:137`）。
- **OAuth 全流程**：RFC 9728/8414 发现（`auth/discovery.go:60-118`）、PKCE S256（`auth/pkce.go:18-35`）、动态客户端注册（`auth/flow.go:162`）、本机回调 + 手动粘贴 URL 双通道（`auth/pending.go`、`auth/flow.go:206-283`）、token 原子写与文件权限校验（`auth/store.go:23-256`）、`NeedsAuth` 状态与原因（`auth/session.go:138-157`）。
- **凭据不落配置**：OAuth token 通过运行时 `TokenSource` 注入，`yaml:"-"` 不序列化（`config/types.go:122-138`）。

### 2.2 配置模型：单文件 + 字段完整

`MCPConfig` 覆盖了接入所需的全部字段（`backend/internal/mcp/config/types.go:22-125`）：

| 字段 | 说明 |
| --- | --- |
| `name` / `description` / `type` | 身份与传输类型（`stdio \| sse \| websocket \| streamable`） |
| `trustLevel` | `local / trusted_remote / untrusted_remote` 三档（`types.go:13-19`） |
| `command` / `args` / `env` / `workingDir` | stdio 启动参数 |
| `url` / `headers` | 远程传输参数（`headers` 优先于 `env`，见 `types.go:107-110`） |
| `enabled` / `disabled` | 启停（`disabled` 为兼容官方格式的反义写法，`types.go:113-114`） |
| `timeout` / `maxRetry` / `maxParallelCalls` | 超时、重试、并发上限 |
| `healthCheck` | 结构化健康检查（工具名 + 参数 + 资源，`frontend/src/types/runtime/mcp.ts:32-37`） |
| `tools` | **工具级启停**（`tools.<raw>.enabled`，`types.go:194-198`） |
| `auth` | OAuth 配置（`clientId/clientSecret/scopes/callbackPort/authorizationServer/resource`，`types.go:140-165`） |

全局配置 `GlobalConfig` 含健康检查间隔、连接超时与健康检查细节（`types.go:86-91`）。`EnvError` 仅内存态：环境变量插值缺失的 server 不参与连接并带错误进运行时状态（`types.go:97-99`）。

### 2.3 管理面：共享 Service + 原子写 + 热重载

参考实现把「配置读写 + 运行时重载」收敛为一个可复用的管理服务，供 console / 微型 Web / CLI 三端共用（这是其管理 UI 方案的核心决策，`docs/plan/mcp-management-ui-plan.md:26-34`）：

- `admin.Service` 方法集：`List / Get / Add / Update / Remove / SetEnabled / Reload / SetToolEnabled / SetToolsEnabled`（`backend/internal/mcp/admin/service.go:107-320`）。
- 写路径统一走 `applyLocked`：读文件 → 修改 → 校验 → **临时文件 + rename 原子写** → `ReloadConfig()` → `Start()`（`admin/service.go:402`；`docs/plan/mcp-management-ui-plan.md:82-88`）。
- `EnsureManagerLocked` 支持 manager 未注入时按配置路径惰性创建（`admin/service.go:420`）。
- HTTP 路由（`docs/plan/mcp-management-ui-plan.md:38-50`，实现核对见 `:91-101`；工具级路由契约见 `frontend/src/types/runtime/mcp.ts:1-17`）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/runtime/mcps` | 列表（配置 + 连接状态 + 工具数 + 配置来源摘要） |
| POST | `/api/runtime/mcps` | 新增 |
| PUT / DELETE | `/api/runtime/mcps/{name}` | 编辑 / 删除 |
| POST | `/api/runtime/mcps/{name}/enable` `/disable` | 启停（持久化 + 重连） |
| POST | `/api/runtime/mcps/reload` | 热重载 |
| GET | `/api/runtime/mcps/{name}/tools` | 全量工具（含禁用），返回 `enabled / configured_enabled / healthy / inputSchema` |
| POST | `/api/runtime/mcps/{name}/tools/{tool}/enable` `/disable` | 单工具启停（不重连） |
| POST | `/api/runtime/mcps/{name}/tools/enable` `/disable` | 批量启停，空数组 = 全部 |

- 鉴权沿用 `authorizeUsageAdmin`；写操作另受 mutation policy（`read_only`）与 `disable_reload_ops` 约束（`backend/internal/api/runtimeapi/mcp_admin_handlers.go:399-409`；`docs/plan/mcp-management-ui-plan.md:93-95`）。
- 可观测：`GET /api/runtime/mcps` 额外返回 `config{path,source,exists,size_bytes,mod_time,manager_loaded,candidates[]}` 与 `summary{total,enabled,disabled,connected,tools}`，启动日志输出 `MCP config loaded`（path/source/servers/enabled）（`docs/mcp/mcp-tool-llm-integration.md:319-325`）。

### 2.4 命名投影与重名隔离（最值得逐条移植的契约）

参考实现把「名字到服务的确定性路由」当作第一类问题处理（`docs/mcp/mcp-tool-llm-integration.md:97-143`）：

| 条件 | 对模型暴露的名称 |
| --- | --- |
| 原始名全局唯一 且 满足 provider 命名规则 `^[a-zA-Z0-9_-]{1,64}$` 且 不遮蔽其他工具 canonical 名 | 保留原始名（向后兼容） |
| 两个及以上服务存在同名工具 | 各自投影 `mcp__<server>__<tool>` |
| 原始名不满足命名规则（空格/特殊字符/超长） | `mcp__<server>__<tool>`（超长截断 + hash） |
| 原始名恰好等于另一个工具的 canonical 名（遮蔽） | 投影为自身的 `mcp__<server>__<原始名>` |

配套的 fail-closed 细节：

- `portableNamePart` 非法字符替换为 `_`，**只要发生替换就追加 FNV-1a 短哈希**，避免 `server.name` 与 `server_name` 规整后碰撞（`backend/internal/mcp/registry/registry.go:534-553`）。
- canonical 超 64 字符截断 + identity hash（`registry.go:220-228`）。
- **解析顺序**：canonical 精确匹配优先；原始短名仅唯一命中时可用；多候选返回 `AmbiguousToolError` 并要求调用方改用 canonical（`registry.go:297-326`）。
- **执行归一化**：解析成功后强制以 registry 中的 `(mcp_name, tool.name)` 调用对应 client，防止错误路由（`manager.go:348-350, 367`）。
- **canonical 完全碰撞**：后注册者拒绝注册并进 quarantine（不暴露、不可执行，可 `ListQuarantinedTools` 诊断）（`registry.go:144-165`）。
- 存储层用 `(mcp 服务, 原始工具名)` 复合键隔离；审计元数据含 `mcp_name / mcp_raw_tool_name / mcp_canonical_name / tool_callable_name`（`registry.go:62,128-130`；`tools/manager.go:512-524`）。

**对 ITSM 的取舍**：参考实现的「不重名就不加前缀」是为兼容既有工具面与 prompt-cache 前缀冻结而做的妥协（文档明确「列表中出现无前缀工具不代表缺少隔离」）。ITSM 没有这两项历史包袱（每次 `chatStream` 重新拉取工具面，见 §3.1），建议**统一强制前缀** `mcp__<server>__<tool>`，换取三个确定性：模型调用名唯一、策略（授权/allowlist）配置对象唯一、审计追溯对象唯一；仅当工具数爆炸需要元工具检索时才回头评估短名投影。

### 2.5 工具级启停与状态语义（三态分离）

- 配置缺省启用（向后兼容），`tools.<raw>.enabled: false` 只禁用工具、服务保持连接（`mcp.yaml` 示例见 `docs/mcp/mcp-tool-llm-integration.md:145-155`）。
- **状态位分离**：`registry.ToolInfo.Enabled`（运行时健康位）与 `ToolInfo.UserDisabled`（管理配置位）；暴露条件 = `Enabled && !UserDisabled`；健康检查不会「复活」被管理员禁用的工具（`:157-158`）。
- 生效路径：写回配置 → 直接翻转注册表标志（**不重连 MCP 服务**）→ catalog 刷新 → 事件 `mcp.tool.state_changed`（`:158`）。
- 禁用工具不进入工具面/catalog/搜索索引；解析 fail-closed（`:159`）。
- 管理 API 返回三态 `enabled / configured_enabled / healthy`（`:160`）。
- 冻结语义：会话工具面按 prompt-cache 语义冻结，工具开关对已存在会话**下一轮新会话或显式刷新后**生效（`:161`）。

**对 ITSM 的取舍**：三态分离必须照搬（ITSM 需要 `discovered / healthy / admin_enabled` 三层，再加一个派生位 `effective`）；「工具开关不触发重连」也必须照搬；会话冻结问题在 ITSM 不存在（无前缀缓存冻结机制），开关下一轮即生效，这是 ITSM 的结构性优势。

### 2.6 面向 LLM 的暴露出口与本地优先级

- 所有面向模型/技能的列表出口统一走 `CallableToolNames`：runtime-server 主链路（`backend/internal/tools/manager.go:81-92`）、CLI skill 链路（`backend/internal/skill/mcp_adapter.go:143-158`）、catalog 快照链路（`backend/internal/mcp/catalog/manager_gateway.go:17-28`）。
- **本地优先规则**：`ls / glob / grep / view` 四个名字无条件本地优先；其他名字仅当 MCP 服务名为内置 `toolkit` 时本地优先，否则同名时 MCP 优先（`docs/mcp/mcp-tool-llm-integration.md:167-171`；`tools/manager.go:188-251,291-322`）。
- 策略 allowlist 按「暴露名」精确匹配；重名投影后按原始短名配置的 allowlist 会匹配失败并被拦截（fail-closed）（`:171`）。

**对 ITSM 的取舍**：ITSM 内置 14 个工具是「本地工具」，规则应简化为**内置优先 + MCP 不得遮蔽**：投影名强制前缀天然保证不遮蔽；`ListToolsForTenant` 与执行解析必须调用**同一个投影/解析函数**（参考实现出现过分链路展示口径不一致的坑，见 `:172`），并在契约测试中锁定。

### 2.7 参考实现的前端落点、测试覆盖与已知缺口

**Console 前端（`frontend/src`）**：

- 页面：`components/workspace/settings/backend-config-settings-page/sections/modes/mcp.tsx`（列表 + 增删改 + 启停 + 热重载 + 刷新）+ `mcp-form.tsx`（全字段表单）+ `mcp-tools-dialog.tsx`（工具级启停对话框，含单工具开关、批量启停、`configured_enabled/enabled/healthy` 三态展示）+ `key-value-editor.tsx`（env/headers 行编辑器）。
- API/类型：`api/runtime/mcp.ts`、`types/runtime/mcp.ts`（路由契约注释 `:1-17`；工具响应三态字段 `:127-164`）。
- i18n：`i18n/resources/{zh-CN,en-US}/runtime-config/mcp.ts`。
- 测试：`api/runtime/mcp.test.ts`（8 例）、`mcp-form.test.ts`（21 例）、`mcp-tools-dialog.test.tsx`（交互测试，`document.body` 断言）、`key-value-editor.test.tsx`（13 例）。

**微型 Web / CLI 双端**：`web_mcp_handlers.go` + `web/js/mcp.js`（同一 admin Service、写令牌守卫）；`aicli chat` 增加 `/mcp` 子命令（`chat_mcp_command.go`，10 例测试）（`docs/plan/mcp-management-ui-plan.md:117-124,158-167`）。

**已知缺口（必须避免把参考实现当"已完成"标杆）**：

1. **浏览器实机点击未执行**——两个前端面板只做了逻辑/静态渲染测试，运行进程仍为旧二进制（`mcp-management-ui-plan.md:133-137,169-175`）。
2. `/mcp add|enable|disable|remove|reload` **同步等待热重载/重连完成（60s 上限）**，慢远端会让命令单元停留数秒（`:172-173`）。
3. 微型 Web 行编辑器 DOM 层用自写 stub 验证（未引入 jsdom）（`:174`）。
4. Win7 兼容构建（`win7compat`）下无重名隔离，但该构建 MCP 整体禁用，属已知边界（`docs/mcp/mcp-tool-llm-integration.md:173`）。
5. 集成文档层面还有「异步连接延迟」与「会话工具面冻结」两点需使用方知晓的语义（`docs/mcp/mcp-tool-llm-integration.md` §8-9）。

**对 ITSM 的启示**：一期就要把「测试连接/热重载」做成**异步任务 + 状态回读**（吸 2）；验收必须包含浏览器 E2E（吸 1）；工具治理面板需要真实交互测试（吸 3）。

---

## 3. ITSM 现状盘点（MCP 视角）

> 本节基于阶段一报告已核实清单（`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md` §3）与本次补充核对；ITSM 仓库检索 `modelcontextprotocol|mcp` 无 MCP 相关命中（唯一偶发子串命中为 `runtime.NumCPU`）——**当前完全没有 MCP 依赖与代码**。

### 3.1 后端工具主干（可直接承载 MCP 工具面）

- **注册表**：`ToolDefinition`（名称/描述/参数 schema/`Resource`/`Action`/`ReadOnly` 等，`itsm-backend/service/tool_registry.go:14-22`）；注册/查询/租户过滤（`:24-61,89-124`）；内置 **8 读 + 6 写**共 14 个工具（`:126-380`）。
- **三层门禁**（`handlers/ai/service.go:113-159`）：
  - Gate 1 路由级 RBAC：`/api/v1/agent/*` 检查 `ai:read` / `ai:write`；
  - Gate 2 工具级 RBAC：按 `ToolDefinition.Resource/Action` 走 `HasResourcePermission` 多租户校验；
  - Gate 3 审批流：写工具（`!ReadOnly`）进入 `ToolInvocation` 待审批，通过后由队列执行，**参数在持久化时冻结、执行时不接受模型改参**（`service.go:118-257`；`service/tool_queue.go:30-130`）。
- **硬编码写工具白名单**：`service.go:385-398`（阶段一报告 §3.4 已列为治理短板）。
- **工具面装载**：每次 `chatStream` 前按角色/租户过滤工具清单再交给 LLM，工具结果回填后继续循环（`service.go:447-616`，过滤与回填 `:485-530`）。
- **权限位**：`internal/authz/catalog.go:161-164` 定义 `ai:read` / `ai:write` 等；路由装配 `router/ai_routes.go:50-57`。

### 3.2 审批、审计与队列（MCP 写工具可直接复用）

- `ToolInvocation` 持久化：工具名、参数、状态（`pending/approved/...`）、审批人、结果（`ent/schema/tool_invocation.go:14-43`）。
- 审批页 `itsm-frontend/src/pages/(main)/ai/approval/index.tsx`（状态标签 `:28-33`）、审计页与聊天页 `pages/(main)/ai/{chat,audit}`。
- **前端缺口**：`grep` 全前端 `toolCall|tool_call` 无匹配——**AIChat 目前不渲染工具调用过程**（阶段一报告 §3.7 同结论），写工具审批在对话外闭环。

### 3.3 Connector 子系统（可借鉴的工程范式，但语义不匹配）

- 类型含 `llm_tool`（`itsm-backend/connector/connector.go:20-34`）；`Manifest` 含 checksum/校验（`:150-211`）；接口为 `Connector`（Init/Start/Stop/Send/Receive…，`:231-248`）与 `Receiver/PollingReceiver`（`:250-268`）——**消息收发语义**，无法表达 `tools/list`、`tools/call`、工具级治理。
- 管理面完整：Provision/Revoke/Test/Health/RotateSecret/Lifecycle + `maskConfig`（`handlers/connector/handler.go`）；路由 `router/connector_routes.go:12-23`；凭据加密存储与租户约束（`ent/schema/connector_config.go:15-39`）。
- 装配点：`internal/bootstrap/app.go:470-500`（connector）、`:741-747`（AI 工具注册）、`:1015-1021`（skills）。
- 前端范式：`admin/connectors/index.tsx`（市场/实例 Tabs + Provision 弹窗 + 测试连接 + 健康状态 + 吊销 + 发送抽屉）、`lib/services/connector-service.ts`、路由/菜单接线（`routes/route-paths.ts:9-33`、`routes/index.tsx:32-56,229-253`、`menu-config.ts:41-44`）。
- AI 管理页先例：`admin/system-config/llm-provider-settings.tsx`（表单 + 特性开关 + 测试）。

### 3.4 差量清单（MCP 接入前）

| 维度 | 现状 | 接入 MCP 需要 | 差距等级 |
| --- | --- | --- | --- |
| 工具注册 | 编译期静态注册 14 个内置工具 | 运行期**动态工具源**（MCP 提供者），带健康与启停 | 大 |
| 工具元数据 | 有 Resource/Action/ReadOnly | 增 `risk`/`category`/`timeout`/`idempotency`/`redaction` 与来源三元组（阶段一 G1） | 中 |
| 执行门禁 | Gate1/2/3 完整 | 写工具审批已有；MCP 只需给出正确的 `Resource/Action/ReadOnly` 投影（默认按写处理） | 小 |
| 审计 | `ToolInvocation` 基础字段 | 补 `provider/server/raw_name/callable_name/args_redacted/output_summary/耗时/错误码`（阶段一 G7） | 中 |
| 出站安全 | Webhook 有基础校验 | SSRF 防护（IP/域名白名单、禁内网段）、输出字节上限、超时/并发/重试策略 | 大 |
| 凭据 | Connector 已有加密存储范例 | MCP 服务器凭据（Header/Token/OAuth）加密 + 轮换 | 中 |
| 前端 | 无任何 MCP 页面/类型 | 管理页（服务器/工具/健康）+ 用户侧工具调用展示与确认 | 大 |
| 事件/可观测 | 聊天 SSE + 审计页 | MCP 连接/工具状态事件、健康摘要、告警 | 中 |

## 4. 协议选型：MCP 的几种形态与 ITSM 取舍

### 4.1 协议背景

MCP 是 JSON-RPC 2.0 之上的工具/资源/提示协议，一次典型会话的生命周期为：`initialize`（能力与版本协商）→ `tools/list` → `tools/call` → 关闭/保活。工具以 `name + description + inputSchema(JSON Schema)` 描述，与本报告关注的「外部工具接入」完全对应。远程传输还涉及会话头、协议版本协商与服务端能力声明（`capabilities`）。公开规范版本序列为 `2024-11-05`（HTTP+SSE 双通道时代）→ `2025-03-26`（引入 Streamable HTTP）→ `2025-06-18`（现行）；具体版本以官方 SDK 支持为准。

参考实现封装官方 Go SDK `modelcontextprotocol/go-sdk v1.4.0`（`backend/go.mod:16`）；ITSM 当前 `go 1.25.13`（`itsm-backend/go.mod:3`），如采纳同一 SDK，版本兼容性无障碍【未验证编译】。

### 4.2 四种传输形态对比

| 传输 | 规范状态 | 连接方向 | 认证 | 典型场景 | ITSM 建议 |
| --- | --- | --- | --- | --- | --- |
| **stdio** | 规范标准 | 本机子进程（stdin/stdout） | 进程环境变量 | IDE、桌面、单机工具链（参考实现的 chrome-devtools 等） | **仅旗舰私有化/平台级**；命令白名单 + 沙箱（进程树守卫、资源限制、禁 shell 拼接） |
| **SSE** | 规范早期标准（`2024-11-05`） | 服务端 → 客户端长连接（双通道） | URL/Header/OAuth | 存量远程 MCP 服务 | **兼容支持**（迁移期）；新接入不推荐 |
| **Streamable HTTP** | 现行标准（`2025-03-26+`） | 单端点 POST + 可选 SSE 流 | Header/OAuth（RFC 9728 资源元数据） | 现代远程 MCP 服务、SaaS | **一期主通道** |
| **WebSocket** | 非规范必需，自定义扩展 | 双向 | Header/Token | 特殊网关环境 | **一期不做**（无标准约束、治理成本高） |

### 4.3 选型决策

**D1（主通道）**：一期仅支持 **Streamable HTTP + SSE** 两类远程传输；远程优先是 ITSM 的部署形态（多租户 SaaS / 私有化服务器）决定的——MCP 服务器由管理员配置为**网络服务**，而不是在宿主机上拉起任意子进程。

**D2（stdio 收紧）**：stdio 仅在「平台级/旗舰私有化」形态开放，且必须：命令白名单（不允许任意可执行文件路径）、进程树守卫（借鉴 `backend/internal/mcp/transport/transport.go:129-191` 与 stdio_tree 守卫）、工作目录与 env 显式声明、资源限制与超时、stderr 尾缓冲诊断（协议通道纯净性）。**租户管理员不得配置 stdio 命令**。

**D3（不追全）**：参考实现支持 WebSocket 是因为其自用工具链存在该形态；ITSM 一期不做 WebSocket，符合「做减法」原则。

**D4（认证分层）**：一期支持 Header/Token 类认证（静态凭据 + 加密存储）；**OAuth 2.1（PKCE + 动态客户端注册 + RFC 9728/8414 发现）放二期**。参考实现的 OAuth 全流程（`auth/discovery.go`、`auth/pkce.go`、`auth/flow.go:162`、`auth/store.go`）是二期最值得复刻的资产，但服务端回调地址、多租户 token 隔离、管理端授权交互都需要 ITSM 侧重新设计，不能直接照搬其本机回调方案。

### 4.4 协议版本与兼容策略

- 连接建立时按 SDK 协商协议版本；服务端版本不匹配 → 连接置 `error` 并记录原因，不降级到未声明行为；
- `tools/list` 的 `inputSchema` **原样持久化**，调用前做服务端 JSON Schema 校验；
- 工具集变更（`list_changed` 通知 / 重连后发现差异）→ 更新工具缓存并产生差异事件（新增/删除/签名变化），签名变化默认将该工具**隔离**待管理员复核（schema 变更 = 行为变更）。

---

## 5. 目标设计

### 5.1 总体架构与设计原则

```text
                         ┌────────────────────────────── itsm-frontend ──────────────────────────────┐
                         │ /admin/mcp-servers（管理：服务器/工具治理/健康）   AIChat（用户：工具时间线+确认） │
                         └───────────────▲───────────────────────────────────────▲────────────────────┘
                                         │ /api/v1/ai/mcp-servers（mcp:read/write）│ SSE 事件（工具调用/待确认）
┌────────────────────────────────────────┴───────────────────────────────────────┴────────────────────┐
│ itsm-backend                                                                                          │
│  handlers/ai ── chatStream ──► ToolRegistry ──► ToolProvider 接口 ──► [内置工具] [MCP 工具提供者]      │
│        │            ▲                │                                    │                          │
│        │ Gate1/2/3  │                │ ExecuteTool（审批/队列/审计）         │                          │
│        ▼            │                ▼                                    ▼                          │
│  service/tool_queue  │        tool_invocations（provider=mcp/三元组）    mcp/manager（连接池/健康/热重载）│
│  handlers/mcp（管理 API）──► mcp/admin（CRUD/测试/工具治理/凭据）          │                          │
│                              │                                            ▼                          │
│                        ent: mcp_servers / mcp_server_tools        mcp/transport（Streamable/SSE）       │
└───────────────────────────────────────────────────────────────────┬────────────────────────────────────┘
                                                                    ▼
                                                    远程 MCP 服务器（Streamable HTTP / SSE）
```

**设计原则**：

1. **同源流水线**：MCP 工具与内置工具走同一套 `ToolRegistry → Gate1/2/3 → 队列 → 审计`，禁止旁路执行。
2. **默认拒绝**：新发现的 MCP 工具默认不可用于生产对话（`admin_enabled=false`，或默认进入「待治理」清单），由管理员显式启用；写分类未知的工具默认按写处理（需审批）。
3. **管理面与运行面分离**：管理 API 管配置与治理；运行面只消费「有效工具面」快照；运行面永不被管理操作阻塞（连接/重载全异步）。
4. **租户隔离**：服务器、凭据、工具、事件、审计全部带 `tenant_id`；跨租户引用一律 fail-closed（复用 Connector 的隔离范式）。
5. **确定性命名**：单一投影函数 + quarantine，保证「名字 → 服务器 → 工具」唯一可解。
6. **不可信输入**：MCP 服务器返回内容视为不可信数据（工具结果可含注入指令），只作为结果回填，不作为指令执行；日志脱敏。

### 5.2 数据模型（ent 草案）

**`MCPServer`**（`ent/schema/mcp_server.go`）：

| 字段 | 说明 |
| --- | --- |
| `tenant_id` + `name` | 唯一键；`name` 为 `[a-z0-9_-]{1,32}` 的稳定标识（用于投影名），显示名另存 |
| `transport` | `streamable \| sse \| stdio`（枚举约束） |
| `url` / `command` / `args` / `env` / `working_dir` | 传输参数（stdio 字段仅平台级可见） |
| `headers_encrypted` / `credential_type` / `credential_encrypted` | 凭据（复用 Connector 加密字段范式；只写不读回，读接口返回掩码） |
| `trust_level` | `trusted \| untrusted`（一期两档；stdio 隐含平台级） |
| `enabled` | 服务器级总开关 |
| `status` / `last_error` / `last_connected_at` / `protocol_version` / `server_info` | 运行态（由 manager 回写，非管理员输入） |
| `timeout_ms` / `max_parallel_calls` / `max_retry` | 执行策略（默认 30s / 4 / 1） |
| `version` | 乐观锁，防管理端并发覆盖 |

**`MCPServerTool`**（`ent/schema/mcp_server_tool.go`）：

| 字段 | 说明 |
| --- | --- |
| `server_id` + `raw_name` | 唯一键（服务端原始工具名） |
| `callable_name` | 投影名（`mcp__<server>__<tool>`，服务内唯一） |
| `description` / `input_schema` | 原样缓存；`input_schema` 存 JSON |
| `schema_hash` | 变更检测（变化 → 隔离待复核） |
| `read_only` / `risk` / `category` | **管理员标注**（默认 `read_only=false, risk=high, category=""`） |
| `enabled`（管理位）/ `healthy`（运行位）/ `quarantined` + `quarantine_reason` | 三态 + 隔离 |
| `discovered_at` / `updated_at` / `last_error` | 维护字段 |

**`MCPOAuthToken`**（二期）：`server_id`、`access_token_enc`、`refresh_token_enc`、`expires_at`、`scopes`、`token_endpoint`、`issuer`。

**`tool_invocations` 扩展**（与阶段一 G1/G7 合并实施，避免两次迁移）：`provider`（`builtin|mcp`）、`mcp_server_name`、`mcp_raw_tool_name`、`mcp_callable_name`、`args_redacted`、`output_summary`、`duration_ms`、`error_code`。

### 5.3 后端模块与契约

模块布局（新建 `itsm-backend/mcp/`，与 `connector/`、`skill/` 平级）：

```text
itsm-backend/mcp/
  transport/   # Streamable/SSE 客户端封装（SDK 适配 + Header/凭据注入 + 生命周期观察者）
  client/      # 单连接会话：initialize/ping/tools.list/tools.call/close（超时与重试）
  registry/    # 工具缓存 + 命名投影 + quarantine + Resolve（纯逻辑，最先落地、最易测试）
  manager/     # 按 server 的连接生命周期：连接池/健康检查/重连/热重载/变更事件
  admin/       # 管理服务：CRUD/测试连接/启停/工具治理/凭据轮换（写路径 + 审计）
  provider/    # ToolProvider 适配器：向 service.ToolRegistry 暴露 MCP 工具面与执行入口
```

**ToolProvider 接口草案**（对 `service.ToolRegistry` 做最小侵入的扩展点）：

```go
// service/tool_provider.go
type ToolProvider interface {
    ProviderID() string                    // "builtin" | "mcp"
    ListForTenant(ctx context.Context, tenantID int, role string) ([]ToolDefinition, error)
    GetTool(ctx context.Context, tenantID int, callable string) (ToolDefinition, bool, error)
    Execute(ctx context.Context, req ToolExecRequest) (ToolExecResult, error)
}
```

- `ToolRegistry` 改为聚合多个 provider：内置实现迁移为 `builtinProvider`（行为不变），MCP 新增 `mcpProvider`；`GetTool` 解析顺序 = 内置优先 → MCP（前缀名天然无冲突）。
- `ListToolsForTenant` 输出统一使用投影名；`ExecuteTool` 按解析结果路由到对应 provider；审批与队列仍由 `service` 层统一编排（**MCP provider 不感知审批**）。
- 事件：`mcp.server.connected|disconnected|auth_required|reload_failed`、`mcp.tools.discovered`（含增删改摘要）、`mcp.tool.state_changed`、`mcp.tool.quarantined`；复用 ITSM 既有审计/事件设施，管理端通过轮询或 SSE 消费。

### 5.4 运行时接入（工具面组装与执行）

**工具面组装**（`chatStream` 前，替换现有过滤段 `service.go:485-504` 的输入源）：

```text
effectiveTools(user, tenant, role) =
  builtinTools(role)                                # 现有 14 个
  ∪ mcpTools(tenant, role)  where
      server.enabled ∧ server.status=connected
      ∧ tool.enabled ∧ tool.healthy ∧ ¬tool.quarantined
      ∧ Gate2 权限通过（mcp:read / mcp:write）
      ∧ schema 校验通过
```

**执行路径**（扩展现有 `ExecuteTool`，`service.go:118-257`）：

1. 名字解析：canonical 精确匹配（唯一实现于 `mcp/registry`）；不可解析 → 工具不存在（fail-closed）；
2. Gate2：`Resource=mcp`、`Action=read|write`（由工具标注决定）；
3. Gate3：`ReadOnly=false` → 进入既有 `ToolInvocation` 审批流（参数冻结）；`ReadOnly=true` 直通；
4. 执行：`mcpProvider.Execute` → 参数 JSON Schema 校验 → 并发/超时治理 → `tools/call` → 结果规范化（文本/结构化内容 + 字节上限）；
5. 审计：写 `tool_invocations`（provider/三元组/耗时/错误码/脱敏参数与输出摘要）；
6. 回填：结果作为工具消息回填模型；**内容按不可信处理**（不执行其中指令，不拼接进系统提示）。

**策略默认值**：

| 项 | 默认 | 说明 |
| --- | --- | --- |
| `read_only` | `false`（按写处理，需审批） | 管理员标注为只读后才免审批 |
| `risk` | `high` | 一期只用于展示与统计，二期接入 Bot 风险上限（阶段一 G2） |
| `timeout` | 30s（单次调用） | 连接超时 10s；健康检查独立 |
| 重试 | 读工具至多 1 次；写工具不自动重试 | 防重复副作用 |
| 输出上限 | 256KB/次，超出截断并标记 | 防上下文与内存放大 |
| 并发 | 每服务器 4（可配） | 防打爆第三方 |

### 5.5 管理 API 设计

挂载在 AI 路由组下（与 `router/ai_routes.go:50-57`、`router/llm_provider_routes.go:34-49` 的挂载惯例一致；前缀 `/api/v1/ai/mcp-servers`）：

| 方法 | 路径 | 权限 | 说明 |
| --- | --- | --- | --- |
| GET | `/` | `mcp:read` | 列表：`items[{config,status}] + summary{total,enabled,connected,error,tools}`（形状对齐参考实现 `docs/mcp/mcp-tool-llm-integration.md:319-325`） |
| GET | `/:id` | `mcp:read` | 详情（凭据字段掩码） |
| POST | `/` | `mcp:admin` | 新增（默认 `enabled=false`；工具默认待治理） |
| PUT | `/:id` | `mcp:admin` | 编辑（乐观锁 `version`；改凭据走独立接口） |
| DELETE | `/:id` | `mcp:admin` | 删除（级联工具缓存；断开连接；二次确认） |
| POST | `/:id/test` | `mcp:admin` | 测试连接：同步短超时（≤10s），返回协议版本/服务端信息/工具预览/诊断；**不落库** |
| POST | `/:id/enable` / `/:id/disable` | `mcp:admin` | 启停：**异步**（202 + 状态回读），不阻塞请求 |
| POST | `/:id/reload` | `mcp:admin` | 重连 + 重新发现工具：**异步** |
| GET | `/:id/tools` | `mcp:read` | 全量工具（含禁用/隔离）：`raw_name/callable_name/description/input_schema/enabled/healthy/configured_enabled/quarantined` |
| POST | `/:id/tools/:callable/enable` / `disable` | `mcp:admin` | 单工具治理（**不触发重连**） |
| POST | `/:id/tools/bulk` | `mcp:admin` | 批量 `{tools:[]}`，空数组 = 全部 |
| PUT | `/:id/tools/:callable/classification` | `mcp:admin` | 标注 `read_only/risk/category`（影响审批，必审计） |
| POST | `/:id/rotate-credential` | `mcp:admin` | 凭据轮换（复用 Connector `RotateSecret` 范式） |
| GET | `/health` | `mcp:read` | 租户健康摘要（供管理页 Header 与告警） |
| GET | `/:id/events` | `mcp:read` | 近期连接/工具变更事件（或并入统一审计查询） |

**异步语义（吸取参考实现教训）**：`enable/reload` 立即返回 `202 + status=connecting`，前端轮询 `GET /:id`（2s 间隔、可退避）回读状态；禁止 60s 同步等待（参考实现 `docs/plan/mcp-management-ui-plan.md:172-173`）。`test` 保留同步是因为它只做一次短握手，超时即报错即可。

**管理操作审计**：`actor/tenant/action/object/before-after(脱敏)/result/ip/ts`，写路径统一走审计中间件；凭据明文永不入审计与日志。

**错误码分层**（枚举 + HTTP 映射，借鉴 `mcp_admin_handlers.go:418` 的分层思路）：`invalid_transport`/`duplicate_name`/`ssrf_blocked`/`connect_timeout`/`tls_error`/`auth_required`/`protocol_mismatch`/`not_found`/`conflict(版本)`/`credential_error`。

### 5.6 权限模型（新增 3 个权限位）

`internal/authz/catalog.go:161-164` 同源新增：

| 权限位 | 授予对象 | 作用 |
| --- | --- | --- |
| `mcp:read` | 租户管理员（管理侧只读）、可选坐席（使用只读工具） | 查看服务器/工具/健康；调用**只读** MCP 工具（Gate2：`Resource=mcp, Action=read`） |
| `mcp:write` | 可选坐席/写场景 | 调用**写** MCP 工具（Gate2：`Resource=mcp, Action=write`），仍需 Gate3 审批 |
| `mcp:admin` | 租户管理员/系统管理员 | 管理 API 全部写操作（CRUD/启停/重载/工具治理/凭据轮换） |

- **使用与治理分离**：普通使用者最多持有 `mcp:read/mcp:write`，不能改配置；`mcp:admin` 默认只发管理员角色。若产品希望最小化权限面，可退化为 2 位方案（`mcp:read`=查看+只读调用，`mcp:write`=管理+写调用），代价是「能配置」与「能调用」无法分离——不推荐。
- **默认拒绝**：任何角色默认不持有 `mcp:*`；服务器默认禁用；工具默认不启用；写分类默认 `read_only=false`。
- Gate3 审批沿用现有写工具管线：审批人仍需 `ai:write`（现有审批路由）；审批详情中必须携带 `mcp 服务器 + 原始工具名 + 投影名`，供审批人判断来源。
- 会话内工具可见性：继承 Gate1（`ai:read`）与 Gate2；`GET /api/v1/agent/tools`（`handlers/ai/handler.go:32-54`）输出增加 `provider/mcp_server/read_only/risk` 元数据与 `canExecute`，供前端提示。

### 5.7 前端设计（一）：管理员配置页

**落点建议**：新建独立页 `pages/(main)/admin/mcp-servers/index.tsx`（与 `admin/connectors` 同范式），而不是塞进 `admin/system-config`（后者已承载 LLM 提供商配置，MCP 需要列表 + 治理矩阵 + 事件三块，体量超出一个 Tab）。备选方案是作为 `system-config` 子 Tab（若一期要省一条路由，可接受，但二期大概率要拆出来）。

**接线三处**（参照 checkpoint 已核实清单）：`routes/route-paths.ts`（新增 `adminMcpServers`）、`routes/index.tsx`（懒加载 + 权限守卫）、`menu-config.ts`（菜单项，归入管理/集成分组）。

**页面结构**：

```text
/admin/mcp-servers
├─ Header 摘要卡：服务器数 | 已连接 | 异常(auth_required/error) | 工具数(启用/总数)
├─ Tab 1 服务器
│   ├─ 列表：名称/传输/端点/状态徽标(toolTip=last_error)/工具数/信任级/启用开关/操作
│   ├─ 操作：测试连接、编辑、启停、重载、删除（危险操作二次确认）
│   └─ 新增/编辑向导（三步）：
│       ① 基本信息（name 稳定标识/显示名/transport/URL）
│       ② 认证（none / header 行编辑器 / bearer token；凭据只写不读回）
│       ③ 高级（timeout/maxParallelCalls/trustLevel）→ 测试连接（协议版本+工具预览）→ 保存
├─ Tab 2 工具治理
│   ├─ 表格：原始名 | 投影名(callable) | 描述 | 三态(enabled/healthy/configured) | 隔离 | read_only 标注 | risk
│   ├─ 单工具/批量启停；classification 编辑（read_only/risk/category）
│   └─ 隔离复核：schema_hash 变化 diff 提示 → 解除隔离/禁用
└─ Tab 3 健康与事件
    ├─ 状态时间线：connected/disconnected/auth_required/reload_failed
    └─ 诊断详情：last_error、协议版本、最近一次成功调用时间；stdio 追加 stderr tail（平台级）
```

**交互要点**：

- 状态实时性：启停/重载后按钮进入 pending 态，轮询 `GET /:id` 直到终态；失败展示 `last_error` 与重试入口。
- 凭据安全：编辑表单永远显示掩码，未修改则回传空值（与 Connector 一致）；`rotate-credential` 独立入口。
- 测试连接是**非持久化**动作（不产生服务器记录），但已存在服务器可对修改中的草稿做一次测试。
- 空态引导：一键跳转「添加 MCP 服务器」；工具治理空态说明「连接成功后发现工具」。

**前端文件清单**：`pages/(main)/admin/mcp-servers/index.tsx`（页面）、`lib/api/mcp-api.ts`（或 `lib/services/mcp-service.ts`，随仓库现状）、`lib/types/mcp.ts`、`lib/i18n/translations.ts`（新增 `mcp.*` 文案，仓库已有 `lib/i18n/translations.ts`）、组件测试（表单校验/三态展示/批量操作/确认弹窗）。

**验收要求**：浏览器 E2E 至少覆盖「新增→测试→启用→治理工具→禁用→删除」全链路（参考实现因未做实机点击而遗留缺口，见 §2.7）。

### 5.8 前端设计（二）：用户使用侧

现有 `components/ai/AIChat.tsx` 只有消息流与 Markdown 渲染，`grep toolCall|tool_call` 无匹配——工具调用对用户完全不可见。MCP 接入后用户侧需三层增强：

1. **工具调用时间线**（对话内）：为每次工具调用渲染折叠块——`工具名（来源徽标：内置/MCP·服务器名）→ 参数摘要（脱敏）→ 结果摘要/耗时/状态`；数据来自 SSE 事件（现有 `handlers/ai/handler.go` 的 `event/data` 通道基础上新增 `tool_call_started / tool_call_finished / tool_call_failed` 事件）。
2. **写工具确认抽屉**：写 MCP 工具进入审批（或二期就地确认，阶段一 G3 的 ConfirmationDrawer）时，在对话内展示确认卡：工具来源、参数、风险标注、影响范围、确认/取消；与 `pages/(main)/ai/approval` 的审批管线共用状态机，避免两套真相。
3. **工具范围提示**：对话头部的「可用工具」入口（调用 `GET /api/v1/agent/tools` 的新元数据字段），让用户知道当前会话能用哪些 MCP 工具、哪些需要审批；不可用工具（无权限/隔离/禁用）不展示具体内容，只提示存在受限工具。

**审批页增强**（`pages/(main)/ai/approval/index.tsx`）：列表与详情增加「来源（内置/MCP）」筛选、服务器名与原始工具名、风险标注；跳转到工具治理页（管理员可见时）。审计页 `pages/(main)/ai/audit` 增加同样的来源维度与筛选（对应 `tool_invocations` 扩展字段）。

### 5.9 安全设计（与 MCP 强相关项）

| 风险 | 控制 |
| --- | --- |
| SSRF（管理员配置 URL 指向内网） | 仅 https（http 由平台级开关放行）；URL 解析后 IP 校验（禁环回/私网/链路本地/ULA）；连接时二次解析防 DNS rebinding；禁跟随重定向；可配域名+端口 allowlist |
| 凭据泄露 | AES-GCM 加密存储（复用 Connector 范式）；只写不读回；掩码展示；轮换审计；日志/事件/审计脱敏 |
| stdio 等价 RCE | 仅平台级；命令白名单（绝对路径 + 参数逐项声明，禁 shell 拼接）；进程树守卫；env 白名单；超时强杀 |
| 输出放大 / 上下文污染 | 输出 256KB 上限 + 截断标记；数组/深度限制；工具描述长度上限 |
| Prompt Injection（描述/返回值） | 描述与结果按不可信数据处理；结果不回灌为指令；关键写操作仍走持久化参数审批（模型不能改参） |
| XSS（Markdown 渲染） | 工具返回内容进入 AIChat 前 sanitize（沿用/加强现有 Markdown 渲染策略） |
| 供应链 | SDK 版本固定；MCP 工具 schema_hash 变更自动隔离待复核 |

### 5.10 可观测与运维

- **指标**：连接状态/握手耗时、`tools/call` 成功率与耗时 P50/P95、超时/重试/截断计数、隔离工具数、每服务器并发使用率。
- **事件**：解析失败/协议不匹配/凭据失效（`auth_required`/连续失败阈值触发服务器置 `error`）产生管理端横幅与审计记录。
- **降级**：MCP 故障只影响其自身工具从工具面移除；`chatStream`、内置工具、审批管线不受影响。
- **健康检查**：独立协程周期 `ping`/`tools.list`（间隔可配 + 指数退避），与请求路径隔离。
- **热重载**：管理写操作先落库再异步重连；运行态以「有效工具面快照」为准，快照刷新失败保留旧快照并告警（不中断会话）。

## 6. 业务闭环审查

> 审查方法：对 MCP 相关的四条端到端链路（管理员配置、用户使用、运行时执行、治理与生命周期）逐步骤走查「入口 → 状态流转 → 失败路径 → 审计 → 恢复」，并标注设计后的闭环状态与缺口。审查对象是 §5 的设计，而非存量代码（存量无 MCP）。

### 6.1 管理员配置闭环

| # | 步骤 | 入口/API | 状态流转 | 失败路径 | 设计后闭环 |
| --- | --- | --- | --- | --- | --- |
| 1 | 新增服务器 | 向导 → `POST /` | `—` → `disabled` | 4xx 错误码（`duplicate_name`/`invalid_transport`/`ssrf_blocked`） | ✅ |
| 2 | 测试连接 | `POST /:id/test` | 状态不变 | `connect_timeout`/`tls_error`/`auth_required` 诊断返回 | ✅ |
| 3 | 启用/建连 | `POST /:id/enable`（202） | `disabled` → `connecting` → `connected\|error` | 异步失败 → `error + last_error`，管理端横幅 | ✅（异步回读） |
| 4 | 工具发现 | 连接成功后自动 | 工具落库（`configured_enabled=false`） | 发现失败 → 服务器 `error`；schema 解析失败 → 该工具隔离 | ✅ |
| 5 | 工具治理 | `GET/POST tools`、`classification` | `configured_enabled`/`read_only`/`risk` 更新 | 无效 callable → 404；隔离工具先复核 | ✅ |
| 6 | 上线使用 | 无需额外操作 | 工具进入 `effectiveTools` | Gate2 无权限 → 该角色不可见 | ✅ |
| 7 | 日常运维 | `GET /health`、`GET /:id/events` | 健康检查周期刷新 `healthy/status` | 连续失败 → `error` + 告警；工具 `healthy=false` → 移出工具面 | ✅（指标/事件需落地） |
| 8 | 凭据轮换 | `POST /rotate-credential` | 原子写 + 重连 | 轮换失败保留旧凭据 + 告警 | ✅ |
| 9 | 下线 | `disable` / `delete` | `disabled`；删除级联工具缓存 | in-flight 调用：等待超时后断开；审计保留 | ⚠️ 需定义 in-flight 宽限期（§6.5-6） |

### 6.2 用户使用闭环

| # | 步骤 | 设计 | 失败路径 | 闭环 |
| --- | --- | --- | --- | --- |
| 1 | 提问/工具面组装 | `chatStream` 合并内置 + MCP（§5.4） | MCP 故障 → 工具塌缩，不影响对话 | ✅ |
| 2 | 模型选工具 | 投影名唯一（§2.4 契约） | 解析失败 → 工具不存在，错误回填 + 审计 | ✅ |
| 3 | 只读工具执行 | Gate2（`mcp:read`）→ 直通 | 超时/限流 → 结构化错误回填 | ✅ |
| 4 | 写工具执行 | Gate2（`mcp:write`）→ Gate3 审批（参数冻结） | 拒绝/过期 → 拒绝结果回填（阶段一 G3 补过期） | ✅（依赖阶段一） |
| 5 | 过程可见 | SSE `tool_call_*` 事件 → AIChat 时间线 | 事件丢失 → 结果仍在消息中（降级） | ⚠️ 前端需实现（§5.8） |
| 6 | 审批交互 | 审批页展示 MCP 来源与工具三元组 | 审批人无 `ai:write` → 403 | ⚠️ 前端需增强 |
| 7 | 审计追溯 | `tool_invocations` 扩展字段 + 审计页筛选 | — | ⚠️ 依赖 G7 / MCP 自带迁移 |

### 6.3 运行时闭环（连接生命周期与工具面一致性）

| 场景 | 行为设计 | 一致性结论 |
| --- | --- | --- |
| 连接断开 | 工具面下一轮移除该服务器工具；状态 `disconnected`；重连退避 | ✅ 无陈旧工具调用（ITSM 每轮拉取工具面，天然强一致） |
| 重连成功 | 工具重新进入工具面；重新发现工具并与缓存 diff | ✅ |
| 工具 schema 变更 | `schema_hash` 变化 → 新工具默认待治理/既有工具隔离待复核 | ✅ fail-closed |
| 工具被管理员禁用 | 下一轮即从工具面移除（参考实现受 prompt-cache 冻结约束，ITSM 无此问题） | ✅ |
| 服务器禁用/删除 | 工具面移除；in-flight 等待/中断按 §6.5-6 处理 | ⚠️ |
| 多租户 | 全部对象带 `tenant_id`；跨租户解析 fail-closed | ✅ |

### 6.4 分析与审计闭环

- 管理操作：审计中间件 + 脱敏 diff（§5.5）→ ✅ 设计完整。
- 工具调用：`tool_invocations` 扩展（provider/三元组/耗时/错误码/脱敏参数与输出摘要）→ ⚠️ 不落地 G7 则追溯能力退化（只有投影名、无服务器来源）。
- 统计：工具使用率/成功率可基于扩展字段聚合；风险标注（`risk`）为二期 Bot 风险上限提供输入（阶段一 G2）。

### 6.5 闭环缺口清单（落地前必须解决）

1. **元数据与审计前置（G1/G7）**：若阶段一 G1/G7 未排期，MCP 一期应自带最小迁移（`tool_invocations` 增 5 字段 + `ToolDefinition` 增来源元数据），不得让 MCP 工具以「无名工具」形式进审计。**建议：合并实施，一次迁移。**
2. **SSRF 防护是上线硬门槛**：没有 §5.9 第一行控制，MCP 服务器配置等价于租户级 SSRF 原语。必须在 M0 完成（含测试：私网段、重定向、DNS rebinding）。
3. **失败回填话术**：MCP 工具失败必须给模型结构化错误（错误码 + 简短原因，不含堆栈/内网信息），避免模型幻觉式重试；写工具失败不自动重试。
4. **in-flight 与删除/禁用竞态**：定义宽限期（建议 30s 或服务器 timeout），期间停止接受新调用、等待进行中调用结束后断连；超时强断并审计记录。
5. **审批可见性**：审批页/对话内必须能看到 MCP 来源与原始工具名，否则审批人在信息缺失下做安全决策——这是「功能存在但闭环不完整」的典型。
6. **对话内确认与外部审批的边界**：一期沿用现有审批页（外置闭环）可接受，但需在对话内以卡片提示「已提交待审批」并可跳转；二期内联确认（阶段一 G3）。
7. **工具面预算**：单租户 MCP 工具数超过阈值（建议 40）时，需元工具/检索式工具面方案（二期，参考 meta-tool 思路 `backend/internal/llm/adapter/mcp_meta_tools.go:4-26`）。
8. **平台级共享服务器**：一期仅租户级配置；多租户共享（平台维护、租户授权）为二期，需要「平台服务器 × 租户授权」矩阵。

### 6.6 闭环结论

| 闭环 | 判定 | 依据 |
| --- | --- | --- |
| 管理员配置 | ✅ 设计闭环成立（含失败/恢复路径） | §6.1；异步化与状态回读已吸收参考实现教训 |
| 用户使用 | ⚠️ 设计成立、前端实现缺位 | §6.2 步骤 5-7 需前端增强，否则过程不可见、来源不可审 |
| 运行时一致性 | ✅ 优于参考实现 | 每轮刷新工具面 + fail-closed 隔离 + schema 变更复核 |
| 审计与追溯 | ⚠️ 依赖元数据迁移 | §6.5-1；不建议推迟 |
| 安全边界 | ⚠️ 依赖 M0 安全项 | §6.5-2；无 SSRF 防护不得上线 |

---

## 7. 功能完整性审查（五级口径）

### 7.1 口径

沿用 ai-gateway 完整度审查的五级状态：`implemented`（代码存在）→ `unit_verified`（单测覆盖）→ `integration_verified`（组件间集成验证）→ `flow_verified`（端到端业务流验证）→ `accepted`（验收通过，可交付）。**判定规则：未达到某一级，其后级别一律不成立。**

### 7.2 完整度矩阵（现状 = 全部未开始）

| 能力项 | 现状 | M0 目标 | M1 目标 | M2 目标 | 验收方式（关键证据） |
| --- | --- | --- | --- | --- | --- |
| Streamable HTTP 传输 | 无 | unit | integration | flow | 客户端单测 + 对 mock/本地 MCP 服务集成测试 |
| SSE 传输 | 无 | unit | integration | flow | 同上 |
| stdio（平台级） | 无 | — | unit（沙箱） | integration | 私有化环境集成；沙箱/进程树守卫测试 |
| 连接生命周期（连接/断开/重连/退避） | 无 | unit | integration | flow | 断连-恢复集成测试 |
| 命名投影 + quarantine | 无 | **unit（最高优先）** | integration | flow | 碰撞/歧义/遮蔽/超长/非法字符契约测试（移植参考实现用例思路） |
| 工具缓存 + schema_hash 变更隔离 | 无 | unit | integration | flow | schema 变更 → 隔离 → 复核 集成测试 |
| 管理 CRUD + 审计 | 无 | unit | integration | flow | API 测试 + 审计断言 |
| 测试连接 | 无 | unit | integration | flow | 失败注入（超时/401/TLS/协议不匹配） |
| 启停/热重载（异步） | 无 | unit | integration | flow | 202 + 状态回读；无 60s 阻塞 |
| 工具治理（三态/批量/分类标注） | 无 | unit | integration | flow | 治理后下一轮工具面变化 E2E |
| 凭据加密/轮换 | 无 | unit | integration | flow | 加密落库断言 + 轮换重连 |
| OAuth 2.1（PKCE/发现/注册） | 无 | — | — | unit→integration | 二期；借参考实现用例清单 |
| 权限位（mcp:read/write/admin） | 无 | unit | integration | flow | 角色矩阵测试（跨租户 fail-closed） |
| Gate2/Gate3 接入 | 无 | unit | integration | flow | 写工具审批全链路（含参数冻结断言） |
| 审计元数据（provider/三元组） | 无 | unit | integration | flow | 审计查询断言 |
| SSE 事件（工具调用/待审批） | 无 | — | unit | flow | AIChat E2E |
| AIChat 工具时间线 | 无 | — | unit | flow | 组件测试 + E2E |
| 写工具确认/审批展示 | 无 | — | unit | flow | 审批页 E2E（含 MCP 来源） |
| 管理页（服务器/治理/健康） | 无 | — | unit | flow | 浏览器 E2E（新增→测试→启用→治理→删除） |
| 审计页来源维度 | 无 | — | unit | flow | 筛选/详情断言 |
| SSRF/输出上限/脱敏 | 无 | unit | integration | flow | 安全负向测试（私网/重定向/超限/脱敏） |
| 指标/健康摘要/告警 | 无 | — | unit | integration | 指标暴露 + 阈值告警测试 |

### 7.3 与参考实现的完整度对比

| 能力 | ai-agent-runtime | ITSM 设计 | 说明 |
| --- | --- | --- | --- |
| 四类传输 | ✅ 全支持 | 一期 2 类（streamable/sse）+ stdio 平台级 | 做减法，符合部署形态 |
| 命名隔离/quarantine | ✅（含 win7 简化分支） | ✅ 统一前缀版，逻辑更简单 | 不背兼容包袱 |
| 工具级治理三态 | ✅ | ✅ | 照搬并加 `quarantined` |
| OAuth | ✅ 完整（本机回调） | 二期（服务端回调重设计） | 不能照搬回调模型 |
| 管理面（多端共享 Service） | ✅ console + 微型 Web + CLI | 一期 console 管理页；CLI/微型 Web 不在 ITSM 范围 | ITSM 只有 Web |
| 前端实机验证 | ❌ 遗留 | **验收强制 E2E** | 补齐参考实现最大缺口 |
| 重载异步化 | ❌ 同步等待 60s | ✅ 设计为异步 | 补齐 |
| 多租户隔离 | 部分（单机为主） | 全链路 `tenant_id` | ITSM 强项，必须保持 |
| 观测/审计 | 事件 + 日志 | 审计 + 指标 + 事件 | ITSM 复用既有设施 |

### 7.4 审查结论

- **存量完整度**：ITSM MCP 能力为 **0（无实现）**；本文给出的设计覆盖了从传输、治理、权限、审计到前端的完整链路。
- **设计完整度**：以本报告为验收基线，M0 完成后核心链路（只读工具 + 管理配置）可达 `integration_verified`；M1 完成后（写审批 + 前端 + 审计）可达 `flow_verified`；M2 补齐 E2E/安全负向/运维后具备 `accepted` 条件。
- **不可跳过的硬门槛**：命名投影契约（M0）、SSRF 防护（M0）、审计三元组（M1）、浏览器 E2E（M2）。
- **与阶段一的关系**：MCP 的写工具确认体验、风险标注消费、审计扩展与阶段一 G1/G2/G3/G7 天然重合，建议合并排期，避免两次迁移与两套状态机。

## 8. 分期路线图

### 8.1 依赖关系

```text
阶段一（Bot 能力）：  B0 元数据/审计 ──► B1 run/事件与确认 ──► B2 Bot 模板/授权 ──► B3 页面入口 ──► B4 E2E
                          │                    │                    │
MCP：                     ▼                    ▼                    ▼
                M0 接入骨架（可并行）   M1 写治理+用户侧（依赖 G1/G3/G7）   M2 加固+二期预研（依赖 B3 组件）
```

**R1**：M0 与阶段一 B0 可并行，但**元数据/审计扩展必须一次迁移**（§6.5-1）；**R2**：M1 的对话内确认与审批体验依赖阶段一 B1/B2 的确认状态机，若阶段一未落地，M1 退化为「外置审批 + 对话内待审批提示」；**R3**：M2 的 E2E 依赖阶段一 B3 的页面入口组件与测试设施。

### 8.2 M0：外部工具接入骨架（只读先行）

**后端**：

1. `mcp/registry`：命名投影 + quarantine + Resolve（**契约测试先行**，用例移植参考实现 `Collision/Callable/Ambiguous/Shadow` 测试思路，`docs/mcp/mcp-tool-llm-integration.md:178-184`）；
2. `mcp/transport` + `mcp/client`：Streamable HTTP / SSE（SDK 封装 + 凭据注入 + 超时）；stdio 留接口不实现；
3. `mcp/manager`：租户级连接池、异步建连/重载、健康检查与退避、工具发现与差分；
4. `mcp/admin`：CRUD、测试连接、启停、工具治理（单/批量/三态）；
5. ent：`mcp_servers` / `mcp_server_tools` + `tool_invocations` 最小扩展；迁移脚本；
6. 安全：SSRF 防护、凭据加密（复用 Connector 范式）、输出上限；
7. 权限：`mcp:read/write/admin` 权限位 + 路由装配；`ToolProvider` 接入，只读工具进 `chatStream` 工具面；
8. 审计：管理操作审计 + 工具调用三元组落库。

**前端**：`/admin/mcp-servers` 页（服务器 Tab 全量 + 工具治理基础 + 健康摘要）；类型/API/文案；组件测试。

**验收**：单测（registry/SSRF/凭据）+ 集成（本地 mock MCP 服务器：连接→发现→治理→只读调用→审计）+ 管理页可用（非 E2E 也算 integration）。

### 8.3 M1：写工具治理与用户侧闭环

1. `read_only/risk/category` 分类标注落地，写工具接入 Gate3（复用 `ToolInvocation` 与队列，参数冻结语义不变）；
2. 审计扩展补全（耗时/错误码/脱敏输出摘要/审批链）；
3. 用户侧：SSE `tool_call_*` 事件、AIChat 工具时间线、待审批卡片与跳转；审批页/审计页来源维度；
4. 运维：健康事件时间线、连续失败告警、并发/超时/重试策略收口；
5. 安全负向测试（私网/重定向/DNS rebinding/输出超限/脱敏）。

**验收**：写路径 E2E（对话 → 审批 → 执行 → 结果回填 → 审计可查）+ 审批页 E2E + 失败注入。

### 8.4 M2：加固、E2E 与二期预研

1. stdio（旗舰/私有化）沙箱与命令白名单；进程树守卫测试；
2. OAuth 2.1 预研与实现（PKCE/发现/动态注册/服务端回调安全设计；借参考实现 `auth/**` 用例清单）；
3. 工具面预算：阈值 + 元工具/检索式工具面（>40 工具触发）；
4. 平台级共享服务器 × 租户授权矩阵；
5. 全套浏览器 E2E、指标/告警看板、运维手册（SOC 视角的凭据轮换/应急禁用流程）。

### 8.5 每期交付物与验收口径

| 期 | 交付物 | 验收口径 | 对应五级状态 |
| --- | --- | --- | --- |
| M0 | 代码 + 迁移 + 单测/集成测试 + 管理页 | 只读链路端到端、治理生效、SSRF 负向通过 | integration_verified |
| M1 | 写治理 + 用户侧前端 + 审计补全 | 写路径 E2E、审批/审计可追溯、前端交互测试 | flow_verified |
| M2 | stdio/OAuth/预算/E2E/运维 | 安全负向 + 浏览器 E2E + 运维演练 | accepted |

---

## 9. 风险与开放问题

### 9.1 风险登记

| 风险 | 影响 | 缓解 | 触发信号 |
| --- | --- | --- | --- |
| 工具面膨胀（LLM 上下文/成本/选择困难） | 对话质量下降、成本上升 | 单租户工具阈值告警；M2 元工具/检索式工具面 | 有效工具数 > 40 或 token 占比 > 30% |
| 第三方 MCP 服务不稳定 | 调用失败、延迟抖动 | 断连退避、熔断、工具 `healthy` 位隔离 | 连续 3 次健康检查失败 |
| 恶意/被污染的 MCP 服务器 | Prompt injection、数据外泄 | 内容不可信处理、schema 变更隔离、域名 allowlist、审计 | 描述/返回值含指令模式、schema_hash 频繁变化 |
| stdio 滥用 | 宿主机 RCE | 仅平台级 + 白名单 + 沙箱 + 禁 shell 拼接 | 出现租户级 stdio 配置需求 |
| 权限扩散 | 越权调用写工具 | 默认拒绝 + 最小角色 + Gate2/3 + 审计 | `mcp:*` 授予非管理员角色 |
| 协议/SDK 演进 | 兼容性断裂 | SDK 版本固定 + 握手校验 + 版本不匹配拒绝 | 新规范版本发布 |
| 范围蔓延（resources/prompts/sampling） | 交付延期 | 明确非目标（§1.3），走变更评审 | 需求中出现非 tools 原语 |

### 9.2 开放问题（需产品/安全拍板）

1. **stdio 是否进一期**：建议不进（M2 旗舰预留）；若必须，需先出沙箱安全评审。
2. **权限位 2 位 vs 3 位**：建议 3 位（`mcp:read/write/admin`），代价是多一次角色种子迁移。
3. **管理页独立 vs system-config Tab**：建议独立页；若一期求快可先 Tab，但工具治理矩阵会很拥挤。
4. **审批人角色**：维持 `ai:write`（现状）还是要求 `mcp:admin`？建议维持现状，仅在审批详情补齐来源信息。
5. **平台级共享服务器**：建议二期；一期所有服务器均租户级。
6. **OAuth 回调形态**：二期需定「统一回调 + state 映射」还是「每服务器独立回调」；须做 CSRF/开放重定向评审。
7. **BYO（租户自带凭据）**：一期由管理员在管理页录入，不开放用户自带；与企业 SSO/密钥托管（Vault）集成列入长期。
8. **工具结果是否进入工件/知识库**：建议一期仅回填对话与审计，不入知识库，避免污染检索。

---

## 10. 附录

### 10.1 术语

| 术语 | 含义 |
| --- | --- |
| MCP | Model Context Protocol，JSON-RPC 2.0 之上的工具/资源/提示协议 |
| Streamable HTTP | MCP 现行远程传输（单端点 POST + 可选 SSE 流、会话头） |
| SSE 传输 | MCP 早期远程传输（HTTP + SSE 双通道，`2024-11-05`） |
| stdio 传输 | 本机子进程 stdin/stdout 通道 |
| callable name / 投影名 | 面向模型暴露的工具名（本设计统一 `mcp__<server>__<tool>`） |
| raw name / 原始名 | MCP 服务端声明的工具名 |
| quarantine / 隔离 | 投影碰撞或 schema 变更时拒绝暴露/执行，等待管理员复核 |
| 三态 | `configured_enabled`（管理位）/ `healthy`（运行位）/ `effective`（派生） |
| 三元组 | 审计中的 `server_name / raw_tool_name / callable_name` |
| G1/G2/G3/G7 | 阶段一报告的差距项编号（工具元数据/Bot 风险上限/确认闭环/审计回填） |

### 10.2 参考实现（ai-agent-runtime）引用清单

- `docs/mcp/mcp-tool-llm-integration.md`：命名隔离、工具启停、暴露规则、验证命令（§2-§4）、可观测字段（§8-§9）。
- `docs/plan/mcp-management-ui-plan.md`：共享 admin Service、HTTP 路由、console/微型 Web 双端落地与遗留事项。
- `backend/internal/mcp/config/types.go`、`admin/service.go`、`transport/transport.go`、`registry/registry.go`、`manager/manager.go`、`auth/**`：配置模型、原子写与热重载、四类传输、投影/隔离/解析、OAuth 流程。
- `backend/internal/api/runtimeapi/mcp_admin_handlers.go`、`frontend/src/types/runtime/mcp.ts`、`frontend/src/components/workspace/settings/backend-config-settings-page/sections/modes/{mcp.tsx,mcp-form.tsx,mcp-tools-dialog.tsx,key-value-editor.tsx}`：管理 API 形状与前端交互范式。

### 10.3 ITSM 侧关键锚点

- 工具主干：`itsm-backend/service/tool_registry.go`、`service/tool_queue.go`、`handlers/ai/service.go`、`handlers/ai/handler.go`、`router/ai_routes.go`。
- 权限目录：`itsm-backend/internal/authz/catalog.go`。
- 数据模型：`itsm-backend/ent/schema/tool_invocation.go`；待新增 `mcp_server.go`、`mcp_server_tool.go`。
- 可复用范式：`itsm-backend/connector/connector.go`、`handlers/connector/handler.go`、`ent/schema/connector_config.go`。
- 前端落点：`itsm-frontend/src/pages/(main)/admin/connectors/index.tsx`、`pages/(main)/admin/system-config/llm-provider-settings.tsx`、`components/ai/AIChat.tsx`、`pages/(main)/ai/{approval,audit}`、`routes/{route-paths.ts,index.tsx}`、`menu-config.ts`、`lib/i18n/translations.ts`。

### 10.4 建议测试用例清单（可直接作为验收清单）

1. **命名投影**：唯一名/重名/非法字符/超长/schema 遮蔽 → 投影与 canonical 断言；
2. **解析**：canonical 精确、短名唯一、短名歧义 fail-closed、执行归一化不串服务；
3. **隔离**：投影碰撞后到者 quarantine 且不可执行；schema_hash 变更 → 隔离 → 复核解除；
4. **治理**：单/批量启停立即生效（无重连）；禁用工具不出现在工具面、解析 fail-closed；
5. **安全**：私网 IP/环回/链路本地/ULA 拦截、重定向拦截、DNS rebinding 二次解析、https 强制、凭据掩码与轮换、输出截断；
6. **门禁**：角色矩阵（`mcp:read/write/admin`）、跨租户 fail-closed、写工具审批参数冻结；
7. **生命周期**：断连退避重连、工具面塌缩与恢复、禁用/删除时 in-flight 宽限；
8. **管理 API**：异步启停/重载状态回读、错误码映射、管理操作审计与脱敏；
9. **前端**：表单校验、三态展示、批量操作、确认弹窗、时间线渲染（浏览器 E2E：新增→测试→启用→治理→禁用→删除）。

---

## 变更记录

| 日期 | 作者 | 变更 |
| --- | --- | --- |
| 2026-09-27 | AI 辅助分析 | 初稿：完成 ai-agent-runtime MCP 栈拆解与可借鉴性判定、ITSM 差距盘点、协议选型、后端/前端目标设计、业务闭环与五级完整度审查、M0–M2 分期建议（基于 `feat/vite-migration` HEAD `7442fad5` 与参考仓库 HEAD `29eeb62d` 的静态核对，未运行测试） |
