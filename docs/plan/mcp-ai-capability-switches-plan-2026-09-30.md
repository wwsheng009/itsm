# MCP / AI 能力开关的运行时化与可视化（管理后台可管、页面可见、逻辑可控）

> 文档类型：设计方案（实施前评审）
> Status: draft（待评审）
> 编制日期：2026-09-30
> 适用范围：`mcp.enabled` / `mcp.write_enabled` / `bot.enabled` 三个能力开关的运行时求值、后台管理、页面展示与执行期管控
> 目标读者：产品、后端、前端、测试、安全与运维
> 关联文档：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（§4 M0-08/M1-02、§6 四级回滚开关）、`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（B2-02 策略层）、`docs/plan/evidence/mcp-m2/bot-mcp-runtime-evidence.md`
> 核查基线：分支 `feat/bot-mcp-integration`（`3ef9c1bf`）；后端 `main.exe`（:8090）+ mock MCP（:19090）；前端 Vite dev（:3000）
> 核查方式：静态代码交叉核对 + 运行中进程实测（工具目录接口、Bot 授权接口）；本轮未编译、未运行测试
> 状态口径：`implemented → unit_verified → integration_verified → flow_verified → accepted`

## 0. 结论先行（TL;DR）

1. **问题**：`mcp.enabled`（L1）、`mcp.write_enabled`（L1.5）、`bot.enabled` 三个决定"AI 能力边界"的开关目前**只有环境变量 / config.yaml 一条路径**，改动必须重启后端；而写工具面关闭时，前端没有任何提示——用户在 Bot 里看到 2 个授权工具、会话里只有 1 个（详见 §1 实测），无法自助判断原因。
2. **方案**：把三个开关提升为**租户级运行时能力开关**（落 `system_configs`，category=`ai`，键名与静态配置同名），由**超级管理员**在管理后台开启/关闭；**默认不写库**（缺行 = 跟随环境默认，保持现有部署零行为变化），写库即覆盖生效。
3. **生效方式**：**免重启**。求值点从 bootstrap 前移到请求期——工具面（provider）、执行入口（Gate 0）、审批后执行（ToolQueue worker）、Bot 管理写端、MCP 管理写端共 5 处门禁统一读同一个 `capability.Source`；本机单实例立即生效，多实例 ≤30s 收敛（缓存 TTL）。
4. **可视化**：MCP 管理页新增「能力开关」卡（含来源、当前生效值、风险说明、恢复默认）；Bot 页、工具页、对话页按生效值显示状态横幅与禁用态（写工具徽标「全局写面已关闭」、授权/启停按钮禁用并给 tooltip），逻辑与展示同源（接口随列表下发 `capabilities` 块，不另开权限面）。
5. **兼容**：静态关闭（`mcp.enabled=false` / `bot.enabled=false`）仍走既有"整组不注册、零行为变化"路径，优先级高于运行时开关；本方案只新增"静态开 → 运行时可关"的能力，不改变任何既有默认。

## 1. 背景与现状（含实测证据）

### 1.1 今天的开关长什么样

| 开关 | 静态来源 | 消费点（bootstrap 一次性） | 关闭的语义 |
| --- | --- | --- | --- |
| `mcp.enabled` | `.env` / `config.yaml` | `internal/bootstrap/app.go:1052` → 组件初始化 + 路由注册 + `RegisterProvider` | 不建连、不注册路由、工具面为空（零行为变化） |
| `mcp.write_enabled` | `.env` / `config.yaml`（默认 **false**） | `app.go:1118-1121` → `mcpprovider.Options.IncludeWriteTools` | 写工具**不进工具面**（模型看不见、不可授权执行） |
| `bot.enabled` | `.env` / `config.yaml` | `app.go:1006/1043` → 指标服务、Bot 管理路由、权限策略注入 | 整组路由不注册；聊天链路退化为兼容默认（只读 + 遗留写白名单） |

### 1.2 用户实测到的现象（本方案要解决的场景）

- Bot 9「MCP 联调助手」授权 **2** 个工具：`mcp__mock__list_issues`（read）、`mcp__mock__create_issue`（act_low，`read_only=false`）。
- 运行中进程的 **工具目录** 只返回 2 条只读 MCP 工具（`list_issues`、`huge_output`），`create_issue` **不在面内**——根因 `mcp/provider/provider.go:174` + `app.go:1120`，因 `mcp.write_enabled=false`（默认）。
- 结论：能力边界正确，但**只存在于环境变量与代码里**，管理后台看不到、Bot/工具页没有任何提示，用户只能靠翻日志/问人。

### 1.3 已有可复用的基建

- `ent.SystemConfig`（租户级 key/value/valueType/category/description）+ `service.SystemConfigService`（默认清单、懒加载、按 key 查询、批量更新），路由 `/api/v1/system-configs*`（读 `system_config:read`、写 `system_config:write`）。
- 前端已有 `/admin/system-config` 页面与 `SystemConfigAPI` 封装；参数类开关（密码策略、附件灰度）已走该体系，**AI 能力开关缺位**。
- `middleware.AuditMiddleware` 对所有 POST/PUT/PATCH/DELETE 落审计（`middleware/audit.go:223-227`），能力开关的写操作天然入审计。

## 2. 目标设计

### 2.1 开关清单与默认解析（不改变任何既有默认）

| 键（`system_configs.key`） | 语义 | 静态默认（当前） | 运行时可写 | 说明 |
| --- | --- | --- | --- | --- |
| `mcp.enabled` | MCP 能力总开关（对应 L1） | `true`（本机 `.env`） | ✅ | 关 → 工具面为空 + MCP 管理/执行写端 403 + 执行期 Gate 0 拒绝 |
| `mcp.write_enabled` | 写工具面（对应 L1.5） | `false` | ✅ | 关 → 写工具不可见、不可执行（含已批准未执行者） |
| `bot.enabled` | Bot 能力（模板/授权/策略门禁/选择器） | `true`（本机；默认 false） | ✅ | 关 → 管理写端 403、选择器隐藏、策略回退到兼容默认 |

**解析顺序（fail-closed）**：`system_configs` 行（本租户，未软删，值可解析）→ 静态配置 `cfg.MCP.Enabled / cfg.MCP.WriteEnabled / cfg.Bot.Enabled` → 内置默认（`false`）。

- **不预置默认行**：`InitDefaultConfigs` **不新增**这三行。缺行 = "跟随环境默认"，因此现有部署升级后行为逐字节不变；管理员在面板上显式保存才产生覆盖行。
- 值类型 `boolean`，解析失败（脏值）→ 视为**未配置**并记录告警日志（不静默放宽）。
- 作用域：**租户级**（`system_configs` 天然带 `tenant_id`）；跨租户不共享。

### 2.2 运行时求值：`capability` 包（新建）

新增 `itsm-backend/capability/`（或 `service/capability/`，落位在实施时按包依赖方向定）：

```go
type Snapshot struct {
    MCPEnabled      bool
    MCPWriteEnabled bool
    BotEnabled      bool
    Source          map[string]string // key -> "override" | "default"
    UpdatedAt       time.Time
    UpdatedBy       int
}

type Source interface {
    // For 返回租户当前生效值；任何读取失败都回退到静态默认（fail-safe，不阻断主链路）。
    For(ctx context.Context, tenantID int) Snapshot
    Invalidate(tenantID int) // 写路径调用，立即失效
}
```

- **实现**：`entSystemConfigSource{client, staticDefaults, ttl: 30s}`。
  - 进程内缓存：`map[tenantID]cachedSnapshot` + `sync.RWMutex`；命中且未过期直接返回。
  - `Invalidate(tenantID)`：管理端点保存后调用；同时挂到 `SystemConfigService` 的更新路径（防止有人用通用配置接口改这三个键导致缓存不刷新）。
  - **多实例收敛 ≤ TTL（30s）**；一期不引入 pub/sub（写入方所在实例立即生效）。写库时附带 `updatedAt/updatedBy` 日志，便于定位。
- **静态关闭短路**：若静态 `cfg.MCP.Enabled=false`（或 `cfg.Bot.Enabled=false`），bootstrap 保持现状（不注册组件/路由），运行时不参与——"静态关"仍是最高优先级的硬回滚路径。
- **写入口**：仅在 `system_config:write`（超级管理员）下开放；写操作即 upsert/delete 对应行（`null` = 删除行 → 恢复跟随默认）。

### 2.3 门禁点（逻辑管控的落点，全部读同一 `Source`）

| # | 层 | 位置（现有代码） | 关闭时的行为 | 拒绝/原因码 |
| --- | --- | --- | --- | --- |
| G0 | 工具面（下发） | `mcp/provider/provider.go:snapshot()` / `ListTools` / `Resolve` | `mcp.enabled=false` → 空面；`write=false` → 写工具不进面（**替代**静态 `IncludeWriteTools`，静态值作为无 Source 时的回退） | —（不可见） |
| G1 | 执行入口（审批前） | `handlers/ai/service.go:ExecuteToolWithOptions`（Gate 2 之前） | MCP 工具：`mcp.enabled=false` 或（写工具 ∧ `write=false`）→ 直接拒绝，**不建 pending 单**、不产生审批噪音 | `capability_disabled:mcp` / `capability_disabled:mcp_write` |
| G2 | 审批后执行 | `service/tool_queue.go` worker → `WriteCapableProvider.ExecuteApprovedWrite` 之前 | 队列 worker 执行前复查：`mcp.enabled=false` ∨ `write=false` → 落 `failed` + `error=capability_disabled:mcp_write`（**已批准的写调用也不放行**） | 同上（写入 invocation.error） |
| G3 | Bot 管理面（写端） | `handlers/ai` Bot 管理 handler（POST/PUT/DELETE/grants） | `bot.enabled=false` → 403，响应体带 `reason=feature_disabled:bot`；**读端保留**（页面能显示"已关闭"） | `feature_disabled:bot` |
| G4 | Bot 策略/选择器（运行面） | `ai.Service.chatToolDecision` / `ValidateBotSelection` / `/agent/bots` | `bot.enabled=false` → 策略按"未注入"处理（回到兼容默认，与静态关闭语义一致）；`/agent/bots` 返回空列表 + `capabilities.botEnabled=false` | —（降级） |
| G5 | MCP 管理面（写端） | `handlers/mcp` 治理写 handler（server CRUD/启停/凭据/工具治理） | `mcp.enabled=false` → 403 `feature_disabled:mcp`；**读端保留**（页面显示已关闭与开关面板） | `feature_disabled:mcp` |

> 与现有 Gate 的关系：G1 位于 `Gate 2（工具级 RBAC）` 之前，作为**能力前置门**（命名 Gate 0）；拒绝同样落 `tool_invocations` 审计（`permission_check=denied`、`permission_reason=capability_disabled:*`），保证"拒绝了什么、为什么"可回溯。
> `bot.enabled` 关闭会**放宽**到兼容默认（遗留写白名单仍可用）——这是既有静态关闭语义，UI 必须显式提示，避免管理员误以为"关闭 Bot = 全禁写"。

### 2.4 API

| 方法/路径 | 权限 | 说明 |
| --- | --- | --- |
| `GET /api/v1/system-configs/ai-capabilities` | `system_config:read` | 返回 3 键生效值 + 来源（override/default）+ 静态默认 + updatedAt/updatedBy |
| `PUT /api/v1/system-configs/ai-capabilities` | `system_config:write` | 入参 `{mcpEnabled?:bool\|null, mcpWriteEnabled?:bool\|null, botEnabled?:bool\|null}`；`null` = 删除覆盖行、恢复跟随默认；返回同 GET；写后 `Invalidate` + 结构化日志 |
| 展示用（随现有接口下发，避免新开权限面） | 各页现有权限 | `GET /ai/mcp-servers` → 响应加 `capabilities{mcpEnabled,mcpWriteEnabled,botEnabled}`；`GET /admin/bots` 与 `GET /agent/bots` → 加 `capabilities{botEnabled,mcpWriteEnabled}`；`GET /agent/tools/catalog` → 加同块（工具页/对话工具面板用） |

> 新路由必须同步 `cd itsm-backend && go run ./cmd/authz-gen` 重新生成 `middleware/rbac_precheck_gen.go`（`TestPrecheckMapIsFresh` 守卫）。挂在 `system-configs` 族下即复用既有 `system_config` 权限码，**不新增权限码**（与 BD8 的口径一致）。

### 2.5 UI（展示与管控同源）

| 页面 | 新增内容 | 交互 |
| --- | --- | --- |
| `/admin/mcp-servers` | 「能力开关」卡片：3 个开关 + 来源徽标（管理台设置 / 跟随环境默认）+ 风险说明（写面开启=写工具可进入审批流；MCP 关闭=工具面立即清空）+ 「恢复默认」；无权限时只读展示 | 保存即生效（toast 提示"已生效，无需重启"）；保存后刷新页面数据 |
| `/admin/mcp-servers`（工具治理页签） | 写工具行加徽标「全局写面已关闭」；"启用/分类"操作保留但提示"配置已保存，全局写面关闭期间不会下发/执行" | tooltip 指向开关卡 |
| `/admin/bots` | 顶部横幅：`bot.enabled=false` → "Bot 能力已在管理后台关闭"；`mcp.write_enabled=false` → "外部写工具当前全局禁用"；开关关闭时**写操作按钮禁用**（诚实提示，不假装成功） | 授权抽屉内写工具条目显示徽标 + 「允许」按钮禁用 |
| `/admin/tools` | 顶部横幅 + MCP 写工具行徽标（同 MCP 治理口径），来源字段标明 `mcp` / 内置 | 只读提示，不提供开关入口 |
| `/ai/chat`（对话页） | `bot.enabled=false` → 隐藏/禁用 Bot 选择器（含提示）；选中已授权写工具时，若全局写面关闭，工具调用事件显示 `capability_disabled:*` 的可读文案 | 复用 SSE 事件与审批卡（M1-03/M1-05） |
| `/admin/system-config` | 「AI 能力」分区**只读**展示（说明"由能力开关面板管理，见 MCP 管理页 / Bot 管理页"），避免双入口写导致缓存口径漂移 | — |
| 审批页 `/ai/approval` | 待审批的 MCP 写单在全局写面关闭时标注"全局禁用中，批准后不会执行" | 批准按钮二次确认 |

i18n：新增 key 同步 `zh-CN` / `en-US`（`lib/i18n/translations.ts` 双语缺一不可，CI 有键一致性校验）。

### 2.6 审计与可观测

- 写操作：`AuditMiddleware` 已覆盖 PUT（`/system-configs/ai-capabilities`），额外输出结构化日志 `capability.settings.updated`（tenant/user/before/after/origin=ui|env）。
- 拒绝路径：Gate 0 与队列 worker 均写 `permission_reason=capability_disabled:*`；指标侧复用 `mcp_*` 现有指标（新增 `itsm_mcp_capability_blocked_total{layer,key}` 可选，归 M2-06 的告警体系）。

## 3. 任务分解（实施清单）

> 规模口径 S/M/L；状态列随实施回写。依赖关系：P1 → P2/P3/P4 → P5 → P6 → P7。

| ID | 任务 | 层 | 交付物（摘要） | 依赖 | 规模 |
| --- | --- | --- | --- | --- | --- |
| P1 | `capability` 运行时源（缓存/失效/静态回退） | 后端 | `capability/` 包 + `Source` 接口 + ent 实现 + TTL/失效单测 | — | M |
| P2 | 工具面与执行门禁接线（G0/G1/G2） | 后端 | provider 动态写面 + `ExecuteToolWithOptions` Gate 0 + ToolQueue worker 复查 + 单测/集成 | P1 | M |
| P3 | Bot 运行时门禁（G3/G4） | 后端 | Bot 管理写端 403、策略降级、`/agent/bots` 空列表 + 单测 | P1 | M |
| P4 | 能力开关 API（GET/PUT）+ 权限 + 失效 + 审计 | 后端 | `system-configs/ai-capabilities` 两端点 + `authz-gen` 重生成 + 单测 | P1 | M |
| P5 | 展示用 `capabilities` 块随列表下发 | 后端 | `mcp-servers` / `admin+bots` / `agent/bots` / `tools/catalog` 响应加块 + 契约测试 | P4 | S |
| P6 | 前端能力开关面板 + 各页状态展示与禁用态 | 前端 | MCP 页开关卡；Bot 页/工具页横幅与徽标；对话页选择器降级；审批页标注；i18n 双语 + jest | P5 | L |
| P7 | 回归与真实栈验证 | 全栈 | `go test ./mcp/... ./handlers/ai/ ./handlers/mcp/ ./service/... ./tests/...`；`tsc`/`eslint`/`jest`；浏览器冒烟（关/开写面各一轮） | P6 | M |
| P8 | 文档回写 | 文档 | 本方案回写实施记录；两份实施计划 §4/§6 与四级开关表同步；gap-register 新 EX 条目；证据 `evidence/mcp-m2/capability-switches-evidence.md` | P7 | S |

### 3.1 验收项

| ID | 判据 | 验证方式 |
| --- | --- | --- |
| AC-01 | 缺行时生效值 = 静态默认（`mcp.enabled=true` / `write=false` / `bot=true` 逐项成立），升级后行为零变化 | 单测 + 运行中进程实测 |
| AC-02 | 管理台把 `mcp.write_enabled` 置 true 后 **≤1s（本实例）** 工具目录出现写工具；置回 false 立即消失 | 集成测试 + 浏览器实测 |
| AC-03 | 写面关闭时：写工具不进面（G0）、直接执行被 G1 拒绝且 `permission_reason=capability_disabled:mcp_write`、**已批准的 pending 单批准后不执行**（G2 落 failed + 原因） | 单测 + 集成 |
| AC-04 | `mcp.enabled=false`：工具面为空、MCP 管理写端 403 `feature_disabled:mcp`、读端与开关面板仍可用（能重新打开） | 单测 + 浏览器 |
| AC-05 | `bot.enabled=false`：Bot 管理写端 403 `feature_disabled:bot`；`/agent/bots` 空；聊天回到兼容默认；选择器隐藏 | 单测 + 浏览器 |
| AC-06 | 权限：`system_config:read` 可读、`system_config:write` 才可写；**无新权限码**；`rbac_precheck_gen.go` 新鲜度过 | 单测 + `go test ./middleware/` |
| AC-07 | 审计：开关写操作落审计（who/when/前后值）；拒绝路径 `permission_reason` 可检索 | 单测 + SQLite 查询 |
| AC-08 | 页面展示与生效值同源：三处列表接口的 `capabilities` 与 `GET ai-capabilities` 一致 | 契约测试 + jest |
| AC-09 | 前端禁用态诚实：写面关闭时 Bot 授权抽屉的写工具不可提交；工具行徽标与 tooltip 正确；zh/en 键齐全 | jest + tsc + eslint |
| AC-10 | 静态关闭优先：`cfg.MCP.Enabled=false` 时仍不注册组件/路由（零行为变化），运行时开关不参与 | 回归（`router/bot_routes_test.go` 同族用例扩写） |

## 4. 测试计划

1. **后端单测**：capability 源（默认→覆盖→非法值→失效→TTL 过期）；provider 动态面；Gate 0 三态矩阵（mcp 关/写关/全开 × 读/写工具）；ToolQueue worker 复查；Bot 写端 403；API 权限与载荷。
2. **集成**：ent 内存库 + 假 `ToolSource`，同进程内改库 → 工具面随请求变化（证明"免重启"）。
3. **前端**：`capability-api` 单测；开关卡（保存/恢复默认/无权限只读）；Bot/工具页横幅与禁用态；对话页选择器降级。
4. **真实栈冒烟**（浏览器 + 运行中后端）：写面 off → 会话仅只读（当前现状复现）；面板打开 → 会话出现 `mcp__mock__create_issue`；面板关闭 → 工具立即消失；MCP 总开关关闭 → 管理页横幅 + 工具面清空；恢复默认 → 回到环境默认。
5. **E2E**：扩展 `tests/e2e/flows/flow-mcp-admin.spec.ts`（开关切换 → 工具目录断言），随 CI `e2e-mcp.yml` 首绿。

## 5. 兼容、回滚与既有口径

- **四级回滚开关不变**：`mcp.enabled`(L1) → `mcp.write_enabled`(L1.5) → `server.enabled`(L2) → `tool.enabled`/quarantine(L3)；本方案只是把 L1/L1.5 从"环境变量"扩展为"环境变量或管理台"，**静态关仍是硬回滚**（短路，不注册组件）。
- **零行为变化承诺**：不预置默认行、不改任何默认值、不新增权限码、不动 Gate 顺序（Gate 0 仅前置拒绝）。
- **回滚本功能**：删除/停用能力开关面板与 API 即可——运行时源缺失时按静态默认求值，系统回到今天的形态。
- **多实例**：写库实例立即生效，其他实例 ≤30s；如需秒级一致性，二期接 Redis pub/sub 或 DB 通知（本期不做，写入收敛 SLA 已记录）。
- **不做**：`ai.enabled`（整个 AI 助手总开关）不在本期；per-server 写开关（写面是平台级口径）不在本期；不引入 per-user 维度。

## 6. 风险与取舍

| 风险 | 影响 | 处置 |
| --- | --- | --- |
| 管理员误关 MCP 总开关导致工具面瞬间清空 | 进行中的对话工具调用失败 | 写面/总开关变更给出二次确认；写操作入审计；页面横幅提示"关闭后立即生效"；提供"恢复默认"一键回退 |
| 关闭 Bot 反而放宽到兼容默认（遗留写白名单） | 与直觉相反的安全姿态 | UI 显式文案"关闭 Bot 将回退为兼容默认（含内置写工具白名单）"；文档同步；如需"关闭即全禁"另开决策项（见 D-E） |
| 多实例窗口期内行为不一致（≤30s） | 短暂双口径 | 写路径 TTL + 日志；SLA 写入文档；二期 pub/sub |
| 通用 `/system-configs` 写入绕过缓存失效 | 面板显示与生效值漂移 | `SystemConfigService` 更新路径挂 `Invalidate`；GET 以 DB 为准并回填缓存 |
| 页面展示与执行管控漂移 | "看着开了其实没开" | 展示块与门禁读同一 `Source`（单一求值点）；契约测试断言一致 |

## 7. 待拍板项

> 2026-09-30 实施口径：CS-1…CS-5 均按「建议默认」执行（未收到异议）；如需调整，按本节表格回滚即可。

| ID | 事项 | 建议默认 | 阻塞 |
| --- | --- | --- | --- |
| CS-1 | 开关作用域：租户级 vs 平台级 | **租户级**（复用 `system_configs`，与附件灰度同构） | P1 |
| CS-2 | 写权限：`system_config:write`（超级管理员） | **是**（不新增权限码） | P4 |
| CS-3 | 面板位置：MCP 管理页为写入口 + 系统配置页只读；Bot 页只读横幅 | **是** | P6 |
| CS-4 | 关闭 Bot 的语义：沿用"兼容默认" vs 改为"全禁写" | **沿用兼容默认**（与静态关闭一致，零行为变化） | P3 |
| CS-5 | 多实例一致性：TTL 30s vs 立即（pub/sub） | **TTL 30s**（一期） | P1 |

## 8. 变更记录

| 日期 | 变更人 | 内容 |
| --- | --- | --- |
| 2026-09-30 | AI 辅助编制 | 初稿：问题定义（环境变量单通道 + 写面关闭无提示）、运行时能力开关设计（capability 源 + 5 处门禁 + API + UI + 审计）、P1–P8 任务分解与 AC-01…AC-10、测试计划、风险与待拍板项；待评审后进入实施 |
| 2026-09-30 | AI 辅助执行 | 实施完成：后端 `capability` 包 + 5 处门禁 + 管理端点 + 装配与权限预检重生成（11 个包测试全绿，真实进程验证免重启生效）；前端能力开关卡与 Bot/工具/对话页禁用态（tsc/eslint/jest 见证据）；证据 `evidence/mcp-m2/capability-switches-evidence.md` |

---

## 9. 实施记录（2026-09-30）

### 9.1 后端（已完成，测试全绿）

| 层 | 文件 | 说明 |
| --- | --- | --- |
| 能力源（新包） | `itsm-backend/capability/capability.go` | `Source`/`Snapshot`/`Patch` + `ConfigSource`（ent 读 + 进程缓存 + `Invalidate` + 30s TTL + 脏值/故障 fail-safe 回退静态默认）+ `Update/Clear/Defaults` |
| 能力源测试 | `itsm-backend/capability/capability_test.go` | 6 组：默认跟随 / 覆盖与免重启 / Clear 恢复默认 / 脏值 / TTL 收敛 / 非法租户与未知键（**ok 11.2s**） |
| 工具面门禁 | `itsm-backend/mcp/provider/provider.go` | `Options.Capabilities`；`gateView`（静态 `Enabled` 短路 ∧ 运行时）；`snapshot→face(includeWrite)`；`GateReason`（区分「被挡」与「不存在」） |
| 执行期门禁 | `itsm-backend/mcp/provider/execute.go` | 新错误码 `mcp_disabled` / `mcp_write_disabled`；面内解析失败先查 GateReason；解析成功后按最新开关复判（覆盖审批后队列路径 G2） |
| 工具面测试 | `itsm-backend/mcp/provider/capability_gate_test.go`（新增）、`provider_test.go`（既有期望更新并注明） | 写面开/关切换免重启、GateReason 三态、执行期拒绝、无能力源时静态口径（**ok 27.8s**） |
| 注册表 | `itsm-backend/service/tool_registry.go` | `ToolGateReasoner` 接口 + `GateReason`（内置优先，遍历 provider） |
| 队列 | `itsm-backend/service/tool_queue.go`、`service/tool_provider.go` | 已批准写单在开关关闭后落 `failed` + 精确错误码（不是笼统的「工具不可用」） |
| 执行入口 Gate 0 | `itsm-backend/handlers/ai/service.go` | `CapabilityDisabledError`（带 `capability_disabled:*` 原因）+ 审计落 denied；`SetCapabilitySource`；`botEnabled`（未注入源码时保持既有行为） |
| Gate 0 测试 | `itsm-backend/handlers/ai/service_capability_gate_test.go` | 三态：写面关闭 / 总开关关闭 / 无门禁回落未知工具，均断言审计原因（**handlers/ai ok 76.8s**） |
| Bot 门禁 | `itsm-backend/handlers/ai/bot_admin.go` | 写端 403（`feature_disabled:bot` 语义文案）；读端保留并带 `capabilities`；`/agent/bots` 关闭态返回空列表 + `capabilities` |
| Bot 门禁测试 | `itsm-backend/handlers/ai/bot_admin_capability_test.go` | 关闭态写端 403 / 选择器空列表 / 开启态行为不变 |
| MCP 门禁 | `itsm-backend/handlers/mcp/handler.go`、`router/mcp_routes.go`、`mcp/admin/service_types.go` | `RequireEnabled()` 挂在治理写组；列表响应新增 `capabilities`（snake_case） |
| MCP 门禁测试 | `itsm-backend/handlers/mcp/capability_gate_test.go` | 关闭态写端 403 + 读端 capabilities；开启态不误伤（**handlers/mcp ok 14.0s**） |
| 管理端点 | `itsm-backend/handlers/systemconfig/ai_capabilities.go`、`router/system_config_routes.go` | `GET/PUT /api/v1/system-configs/ai-capabilities`（`system_config:read` / `system_config:write`）；`reset` 语义；结构化日志 |
| 管理端点测试 | `itsm-backend/handlers/systemconfig/ai_capabilities_test.go` | 生效值/默认值/来源、patch+reset、未知键 400、缺租户 401、未注入源码 503（**ok 12.6s**） |
| 缓存失效挂钩 | `itsm-backend/service/system_config_service.go` | `SetCapabilityInvalidator` + 三处写入路径调用（防止通用配置接口绕过缓存） |
| 装配 | `itsm-backend/internal/bootstrap/app.go` | 构造 `capabilitySource` 并注入 ai.Service / provider / bot handler / mcp handler / systemconfig handler + service |
| 权限预检 | `middleware/rbac_precheck_gen.go` | `go run ./cmd/authz-gen` 重新生成（新增两条 system-configs 子路由，**未新增权限码**） |

### 9.2 后端回归（进行中）

| 范围 | 结果 |
| --- | --- |
| `go build ./...` | **exit 0** |
| `go test ./capability/ ./mcp/provider/ ./handlers/mcp/ ./handlers/systemconfig/` | **全部 ok**（11.2s / 27.8s / 14.0s / 12.6s） |
| `go test ./handlers/ai/ ./router/` | **ok**（76.8s / 45.7s） |
| `go test ./middleware/ ./service/ ./tests/mcpintegration/ ./tests/botintegration/` | **全部 ok**（14.8s / 1038.1s / 50.9s / 52.7s）；middleware 首轮因权限预检未重生成而红 → `authz-gen` 重生成后通过；mcpintegration 的 M1-01 标注用例更新为「精确原因」断言后通过 |
| 前端 `tsc --noEmit` / `eslint` / `jest` | **exit 0 / exit 0 / 9 套件 112 用例全绿**（API+组件 4 套件 80 例、页面 5 套件 32 例） |
| 真实栈验证（免重启切换） | **已通过**：`PUT mcpWriteEnabled=true` → 目录 2→3（`create_issue` 出现）；`reset` → 3→2；`mcp.enabled=false` → 目录 0 + 治理写端 403；`/admin/bots` 响应带 `capabilities`（详见 `evidence/mcp-m2/capability-switches-evidence.md` §3） |

### 9.3 决策执行口径（按建议默认，未收到异议）

CS-1 租户级 ✅｜CS-2 写权限 `system_config:write` ✅｜CS-3 写入口 = MCP 页开关卡（系统配置页只读）✅｜CS-4 关闭 Bot 沿用兼容默认 ✅｜CS-5 多实例 TTL 30s ✅。
