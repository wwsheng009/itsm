# 工具目录查询 / MCP 默认启用 / Bot×MCP 集成测试 证据

> 文档类型：实施证据（工具查询能力 + 开关默认值变更 + 跨模块集成测试）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：MCP 外部工具接入（M0–M2）与 Bot 能力落地（B2）的交叉增量
> 目标读者：后端/前端开发、测试、SRE
> 关联文档：
> - `docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（方案）
> - `docs/plan/itsm-mcp-external-tool-integration-analysis-2026-09-27.md`（分析报告）
> - `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（Bot 方案）
> - `docs/plan/evidence/bot-b3/S4-S7-business-bots-evidence.md`（前置：管理面契约修复）
> 核查基线：`feat/bot-mcp-integration`（父提交 `f7dea6f3`）；本轮未提交前实测
> 核查方式：静态代码交叉核对 + 单元/集成测试实跑 + 本机进程浏览器实测；未核实处标注【未核实】
> 状态口径：`implemented → unit_verified → integration_verified → flow_verified → accepted`（不可跳跃）

## 1. 交付清单

| # | 交付 | 位置 | 状态 |
| --- | --- | --- | --- |
| 1 | 工具目录查询端点（内置 + MCP 统一投影，RBAC 过滤 + q/source/readOnly/risk/limit） | `itsm-backend/handlers/ai/handler.go`（`ListToolCatalog`）、`itsm-backend/router/ai_routes.go` | `integration_verified`（契约 + 用例实跑） |
| 2 | 前端 API：`aiListToolCatalog` / `ToolCatalogItem` | `itsm-frontend/src/lib/api/ai-api.ts` | 随 #4/#5 一并验证 |
| 3 | Bot 授权抽屉：可搜索工具选择器（内置/MCP 标注 + 风险预填 + MCP 提示 + 目录入口） | `itsm-frontend/src/pages/(main)/admin/bots/index.tsx` | 见 §4 |
| 4 | 独立「工具目录」页 `/admin/tools` | `itsm-frontend/src/pages/(main)/admin/tools/index.tsx` + 路由/菜单接线 | 见 §4 |
| 5 | `mcp.enabled` 默认开启 + 出站安全平台开关（本地联调 mock 服务器） | `itsm-backend/config/config.go`、`config.yaml.example`、`.env(.example)`、`itsm-backend/internal/bootstrap/app.go` | `unit_verified`（配置用例实跑） |
| 6 | Bot×MCP 集成测试（策略门禁 × 工具面收缩 × 真实执行） | `itsm-backend/tests/mcpintegration/bot_mcp_integration_test.go` | `integration_verified`（2 用例 PASS） |
| 7 | 「工具目录」种子菜单项（`ai:read`） | `itsm-backend/pkg/seeder/seeder.go` | 见 §4 |

## 2. 工具目录端点契约（`GET /api/v1/agent/tools/catalog`）

- 权限：`ai:read`（`itsm-backend/router/ai_routes.go` 注册；与 `GET /agent/tools` 同组）。
- 可见性：与 `ListTools` **同源**——`Service.ListToolsForTenant`（含租户动态化内置工具面 + MCP provider 投影）叠加当前角色 `resource:action` RBAC 过滤。
- 查询参数：

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `q` | 任意字符串 | 名称 / 描述 / 原始工具名 / 服务器名 子串（大小写不敏感，`toolCatalogMatches`） |
| `source` | `builtin` \| `mcp` | 来源过滤；空 = 不限；非法值 → 400 |
| `readOnly` | `true` \| `false`（兼容 `1`/`0`） | 读写面过滤；非法值 → 400 |
| `risk` | `read` \| `plan` \| `act_low` \| `act_medium` \| `act_high` | 治理标注过滤；非法值 → 400 |
| `limit` | 正整数 | 默认 200、上限 500（超出按上限钳制不报错）；非法/≤0 → 400 |

- 响应：`{code:0,message:"success",data:{items:[…],total:N}}`；`total` 为**过滤后匹配总数**，`items` 按 `limit` 截断。
- 条目字段（紧凑投影，**不下发** `argsSchema`/`resultSchema`）：`name/description/readOnly/risk/category/provider/serverName/rawToolName/resource/action/supportsDryRun/idempotent`。
- 排序：`provider` 升序（`builtin` 在前）→ `name` 升序，保证目录页与选择器展示稳定。

**测试**：`itsm-backend/handlers/ai/handler_tool_catalog_test.go`（`TestListToolCatalog_FiltersAndRBAC`，6 个子用例）：

```
=== RUN   TestListToolCatalog_FiltersAndRBAC
    --- PASS: …/全量：内置在前、MCP 带来源三元组
    --- PASS: …/source=mcp 仅返回外部工具
    --- PASS: …/readOnly/risk/描述关键词过滤
    --- PASS: …/limit 截断但 total 为全量匹配数；上限钳制
    --- PASS: …/RBAC：end_user 看不到 MCP 工具
    --- PASS: …/参数非法与身份缺失
ok  itsm-backend/handlers/ai  2.507s
```

## 3. 配置变更：`mcp.enabled` 默认开启 + 出站安全平台开关

**结论**：全局开关默认开启（`config.yaml.example` 的 `${MCP_ENABLED:true}` + 代码级默认：未显式配置且未设置环境变量时置 `true`）。**D7「默认拒绝」不变**——服务器默认禁用、新工具默认不启用、写面 `write_enabled` 默认 false；未配置服务器时不建连、工具面不含 MCP。

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| 默认开启 | `config/config.go`（`LoadConfig` + `mcpEnabledExplicitlyConfigured`） | 显式 `mcp.enabled: false` 或 `MCP_ENABLED=false` 仍可关闭（回滚 L1：零装配、零路由） |
| 示例配置 | `config.yaml.example` | `enabled: ${MCP_ENABLED:true}` + 语义注释 |
| 本地配置 | `.env`（`MCP_ENABLED=true`）、`.env.example` | 回滚方式注释在侧 |
| 出站安全开关 | `MCPConfig.AllowHTTP / AllowPrivateNetworks / AllowedPorts` | 默认 `false/false/空`（仅 https + 公网 + 443/80）；本地 mock 联调显式放开 |
| 装配接线 | `internal/bootstrap/app.go` | `transport.NewSSRFGuard(transport.SSRFConfig{AllowHTTP, AllowPrivate, AllowedPorts})` |

**测试**：`itsm-backend/config/config_mcp_test.go`

```
--- PASS: TestLoadConfig_MCPSwitches (0.04s)
--- PASS: TestLoadConfig_MCPDefaults (0.03s)          # 未配置 mcp 块 → Enabled=true
--- PASS: TestLoadConfig_MCPExplicitDisable (0.40s)   # yaml/environment 显式 false 优先
ok  itsm-backend/config  1.012s
```

> 安全边界：`allow_http` / `allow_private_networks` 仅供私有化与本地联调，生产必须保持 `false`；SSRF 拒绝能力仍由 `mcp/transport/ssrf_test.go` 与 M0-05 验收（含 DNS rebinding、私网、端口白名单）钉住。

## 4. 前端与种子（工具查询入口）

| 交付 | 验证 |
| --- | --- |
| Bot 授权抽屉：`AutoComplete` 工具选择器（`onFocus` 首拉、输入 300ms 防抖、`limit=200`）+ 选中后按「工具风险 ≤ 模板上限」预填授权上限 + MCP 工具提示 + 「打开工具目录」 | 见 §4.1 |
| 独立工具目录页 `/admin/tools`：搜索/来源/只读/风险过滤 + 表格（来源、服务器、风险、类别、描述）+ 复制名称 | 见 §4.1 |
| seeder 菜单：`{Name: "工具目录", Path: "/admin/tools", PermissionCode: "ai:read", SortOrder: 288}` | 菜单行随种子更新（`pkg/seeder`） |

### 4.1 测试与实测

| 项 | 命令 / 观察 | 结果 |
| --- | --- | --- |
| 前端类型检查（含 Bot 页与新目录页） | `npx tsc --noEmit` | exit 0 |
| Bot 页组件测试（8 用例，含新增「工具目录选择」用例） | `npx jest --testPathPattern "admin/bots" --coverage=false` | PASS：Test Suites 1/1、Tests 8/8（292s） |
| 工具目录页测试（3 用例） | `npx jest --testPathPattern "admin/tools" --coverage=false` | PASS：Test Suites 1/1、Tests 3/3（31s） |
| 浏览器实测 | `/admin/tools` 与 `/admin/bots` 授权抽屉（mock MCP 服务器在线） | 见 §5 |

### 4.2 后端复跑

```
go test ./handlers/ai/ ./router/ ./pkg/seeder/ ./tests/mcpintegration/ ./config/ -count=1 -timeout 25m
```

| 包 | 结果 |
| --- | --- |
| `itsm-backend/handlers/ai` | **ok 38.583s**（含 `TestListToolCatalog_*` 与既有 Bot/MCP 审批/来源用例） |
| `itsm-backend/router` | **ok 33.305s** |
| `itsm-backend/tests/mcpintegration` | **ok 33.281s**（M0/M1 既有 + 本轮 `TestBotMCP_*`） |
| `itsm-backend/config` | **ok 2.328s** |
| `itsm-backend/pkg/seeder` | FAIL（5 例）：`TestSeedGroupsAndProvisionClones` / `TestProductionInitializers*` / `TestProvisionTenant*`，根因均为**既有** `resolve process definition incident_emergency_flow: ent: process_definition not found`（与 Bot 方案变更记录中登记的既有失败同名同根因），**非本轮菜单项引入**；单独复跑输出确认报错文本不含菜单/工具目录相关内容 |

## 5. 本机端到端实测（mock MCP 服务器在线）

环境：`main.exe`（本地库；`.env` 置 `MCP_ENABLED=true` + `MCP_ALLOW_HTTP=true` + `MCP_ALLOW_PRIVATE_NETWORKS=true` + `MCP_ALLOWED_PORTS=19090`）
+ `go run ./cmd/mcp-mockserver -addr :19090 -tools default`（独立进程，控制面 `/__mock/*`）。

> 说明：管理面写请求需 `X-CSRF-Token`（先 `GET /api/v1/csrf-token` 再携带；与前端 `http-client` 同口径）。

| # | 步骤 | 结果 |
| --- | --- | --- |
| 1 | `POST /api/v1/ai/mcp-servers`（name=mock、transport=streamable、url=`http://127.0.0.1:19090`、credential_type=none） | `code=0`，id=1；`enabled=false`（D7 默认禁用服务器） |
| 2 | `POST /api/v1/ai/mcp-servers/1/test` | `ok=true`，protocol=`2025-06-18`，server=`itsm-mcp-mock`，工具预览 6 个 |
| 3 | `POST .../1/enable` → `GET .../1` | `running_status=healthy`，`tool_count=6`，`enabled_tool_count=0`（新工具默认不启用） |
| 4 | 治理：`PUT .../tools/mcp__mock__list_issues/classification`（read_only=true、risk=read、category=issue）+ `POST .../enable`；`mcp__mock__huge_output` 同 | 两工具 `enabled=true` |
| 5 | `GET /api/v1/agent/tools/catalog?source=mcp` | `code=0`，`total=2`（`mcp__mock__list_issues` / `mcp__mock__huge_output`，带 `serverName=mock`、`rawToolName`） |
| 6 | 浏览器 `/admin/tools` | 「共 19 个工具（内置 17 / MCP 2）」；MCP 行带 `MCP` 标签与「服务器：mock」，只读=是、风险=只读；内置行来源/风险/类别/描述完整 |
| 7 | 浏览器 `/admin/bots` → S4「工具授权」抽屉 | 统计行「可见工具 19 个（内置 17 / MCP 2）」；输入 `list_issues` 出现选项 `mcp__mock__list_issues（MCP · mock · 只读）`；选中后工具名回填、授权上限自动预填「只读」（工具风险 read ≤ 模板上限 plan）、出现 MCP 专项警示（Gate2/Gate3） |
| 8 | 种子重放（`initialize -action=apply`，runId=34）+ 侧边栏 | 「系统管理」下新增「工具目录」入口（与 MCP 外部工具 / Bot 管理与授权并列） |

## 6. Bot×MCP 集成测试（`itsm-backend/tests/mcpintegration/bot_mcp_integration_test.go`）

复用 M0-14 harness（真实 ent + sqlite + mock MCP 服务器；HardcodeOnly 权限模式）：

| 用例 | 断言 | 结果 |
| --- | --- | --- |
| `TestBotMCP_ToolFaceGating` | ① 已授权 MCP 读工具 `Allowed`；② 未授权 MCP 工具 `ReasonToolNotGranted`；③ 写面投影 `Action=write` 且已授权放行；④ 仅授权读工具的 Bot 对写工具 `ReasonToolNotGranted`；⑤ 授权上限 read + 工具风险 act_medium → `ReasonRiskExceeded`；⑥ RBAC 拒绝 → `ReasonRBACDenied` | PASS（1.64s） |
| `TestBotMCP_ExecuteReadToolAndServerDisableShrinksFace` | 工具定义三元组（provider=mcp / serverName=mock / rawToolName=list_issues）；经 `registry.Execute` 真实调用且 mock 侧记录到调用；停用服务器后 `GetToolForTenant == nil`（工具面收缩） | PASS（1.35s） |

```
ok  itsm-backend/tests/mcpintegration  4.061s
```

## 7. 残余与后续

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 工具目录端点的分页 | 当前 `limit`（≤500）+ `total`；若单租户工具面超 500（M2-03 预算 40 为软上限）再评估游标分页 |
| 2 | `allow_http` 等的生产防线 | 已注释警示；后续可在管理面「健康摘要」增加"当前处于放行模式"的显式提示【未核实】 |
| 3 | 工具目录页的 MCP 治理跳转 | 可选：行内直达「MCP 外部工具」的对应服务器抽屉（当前仅展示 serverName） |

## 变更记录

| 日期 | 变更人 | 变更内容 |
| --- | --- | --- |
| 2026-09-27 | AI 辅助执行 | 初稿：工具目录端点 + 前端选择器/目录页 + `mcp.enabled` 默认开启与出站安全开关 + Bot×MCP 集成测试 |
