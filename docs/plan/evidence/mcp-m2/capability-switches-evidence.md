# MCP / AI 能力开关运行时化与可视化：实施与验证证据

> 文档类型：实施证据（运行时开关 + 管理面 + 页面管控）
> Status: done（页面部分见 §5）
> 编制日期：2026-09-30
> 适用范围：`mcp.enabled` / `mcp.write_enabled` / `bot.enabled` 的运行时求值、管理端点、门禁与页面展示
> 目标读者：产品、后端、前端、测试、安全与运维
> 关联文档：`docs/plan/mcp-ai-capability-switches-plan-2026-09-30.md`（设计与任务分解）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（§6 四级回滚开关）
> 核查基线：分支 `feat/bot-mcp-integration`；后端 `main.exe`（2026-09-30 11:00 构建，:8090）+ mock MCP（:19090）；前端 Vite dev（:3000）
> 核查方式：静态代码交叉核对 + 单元/集成测试 + **真实进程 API 实测**（免重启切换）+ 前端 tsc/eslint/jest
> 状态口径：`implemented → unit_verified → integration_verified → flow_verified → accepted`

## 1. 结论摘要

1. 三个能力开关已从「仅环境变量 + 重启」升级为**租户级运行时开关**：缺行 = 跟随静态默认（升级零行为变化），管理台保存即写入 `system_configs`（category=`ai`）并立即失效缓存。
2. **免重启生效**已在真实进程上验证：`PUT mcpWriteEnabled=true` → 工具目录由 2 个只读工具变为 3 个（含写工具 `mcp__mock__create_issue`），全程未重启后端；`reset` 后回到 2 个。
3. **逻辑管控**落到 5 处门禁（工具面 / 执行入口 Gate 0 / 审批后队列 G2 / MCP 治理写端 / Bot 管理写端），全部读同一 `capability.Source`；`mcp.enabled=false` 时目录空面、治理写端 403「MCP 能力已在管理后台关闭」。
4. 管理端点 `GET/PUT /api/v1/system-configs/ai-capabilities`（`system_config:read` / `system_config:write`）已上线；权限预检映射已用 `cmd/authz-gen` 重生成，**未新增权限码**。
5. 前端（MCP 管理页「能力开关」卡 + Bot/工具/对话页状态与禁用态）与后端**同源展示**：展示块随既有列表接口下发，避免两套口径。

## 2. 交付清单

### 2.1 后端

| 文件 | 关键实现 |
| --- | --- |
| `itsm-backend/capability/capability.go` | `Source`/`Snapshot`/`Patch`；`ConfigSource`（ent 读 + 进程缓存 + `Invalidate` + 30s TTL + fail-safe）；`Update/Clear/Defaults`；键清单与分类常量 |
| `itsm-backend/mcp/provider/provider.go` | `Options.Capabilities`；`gateView`（静态短路 ∧ 运行时）；`snapshot→face(includeWrite)`；`GateReason`（辨识「被挡」与「不存在」） |
| `itsm-backend/mcp/provider/execute.go` | 错误码 `mcp_disabled` / `mcp_write_disabled`；解析失败先查门禁原因；执行前按最新开关复判（含审批后路径 G2） |
| `itsm-backend/service/tool_registry.go` | `ToolGateReasoner` + `GateReason`（内置优先，遍历 provider） |
| `itsm-backend/service/tool_queue.go` | 已批准写单在开关关闭后落 `failed` + 精确错误码（原为「外部工具当前不可用」） |
| `itsm-backend/service/tool_provider.go` | `ErrorCodeMCPDisabled` / `ErrorCodeMCPWriteDisabled` / `ReasonCapabilityMCP*` / `CapabilityGateError` |
| `itsm-backend/handlers/ai/service.go` | `CapabilityDisabledError`（带原因）+ Gate 0 审计；`SetCapabilitySource`；`botEnabled`（未注入源码时行为不变） |
| `itsm-backend/handlers/ai/bot_admin.go` | 写端 403；读端带 `capabilities`；`/agent/bots` 关闭态空列表 + `capabilities` |
| `itsm-backend/handlers/ai/handler.go` | `tools/catalog` 响应带 `capabilities`（工具页/对话面板同源） |
| `itsm-backend/handlers/mcp/handler.go` | `RequireEnabled()` 治理写门禁；列表响应 `capabilities`（snake_case） |
| `itsm-backend/handlers/systemconfig/ai_capabilities.go` | 管理端点（三态 patch + `reset`）；`capabilityAdmin` 接口（生产 = `ConfigSource`） |
| `itsm-backend/service/system_config_service.go` | `SetCapabilityInvalidator` + 三处写入路径失效（防止通用接口绕过） |
| `itsm-backend/internal/bootstrap/app.go` | 构造 `capabilitySource` 并注入 5 处消费方 |
| `itsm-backend/router/{mcp_routes.go,system_config_routes.go}` | 写端门禁挂载 + 新端点注册 |
| `itsm-backend/middleware/rbac_precheck_gen.go` | `go run ./cmd/authz-gen` 重生成（新增 2 条 system-configs 子路由） |

### 2.2 前端

见 §5（由并行子任务实施，遵循同一接口契约）。

## 3. 运行时验证（真实进程，2026-09-30）

> 环境：后端 `main.exe`（本次构建）+ mock MCP（:19090）+ 真实登录会话；写操作携带 CSRF token（`GET /api/v1/csrf-token`）。

| 步骤 | 操作 | 实测结果 |
| --- | --- | --- |
| 1 | `GET /api/v1/system-configs/ai-capabilities` | `{mcpEnabled:true, mcpWriteEnabled:false, botEnabled:true, defaults:{…同值}, overridden:{}, keys:[三键]}` |
| 2 | 目录基线 `GET /agent/tools/catalog?source=mcp` | **2** 个：`mcp__mock__huge_output`, `mcp__mock__list_issues`（写工具不在面内） |
| 3 | `PUT {mcpWriteEnabled:true}` | 200；`overridden={mcp.write_enabled:true}` |
| 4 | 立即再查目录（**未重启**） | **3** 个：`mcp__mock__create_issue` 出现 ✅ |
| 5 | `PUT {reset:["mcp.write_enabled"]}` | 200；`mcpWriteEnabled=false`、`overridden` 空 → 目录回到 **2** 个（恢复跟随环境默认） |
| 6 | `PUT {mcpEnabled:false}` | 200；目录 **0** 个；治理写端 `POST /api/v1/ai/mcp-servers` → **403** + `MCP 能力已在管理后台关闭（mcp.enabled=false），请先在管理后台启用后再操作` |
| 7 | `PUT {mcpEnabled:true}` | 200；目录回到 **2** 个（写面仍 false） |
| 8 | `GET /api/v1/admin/bots` | 响应含 `capabilities:{botEnabled:true, mcpWriteEnabled:false}`（页面禁用态数据源） |

**结论**：①②③④⑤⑥⑦ 覆盖 AC-01/02/04 的运行面；⑧ 覆盖 AC-08 的展示面（Bot 页）。

## 4. 自动化测试结果

### 4.1 后端

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | **exit 0** |
| `go test ./capability/` | **ok 11.2s**（6 组：默认/覆盖免重启/Clear/脏值/TTL/非法入参） |
| `go test ./mcp/provider/` | **ok 27.8s**（新 `capability_gate_test.go` 3 组 + 既有用例更新） |
| `go test ./handlers/mcp/` | **ok 14.0s**（门禁 403 + 列表 capabilities） |
| `go test ./handlers/systemconfig/` | **ok 12.6s**（端点五组） |
| `go test ./handlers/ai/` | **ok 76.8s**（Gate 0 三态 + Bot 管理门禁 + 既有回归） |
| `go test ./router/` | **ok 45.7s** |
| `go test ./middleware/` | **ok 14.8s**（`authz-gen` 重生成后 `TestPrecheckMapIsFresh` 通过） |
| `go test ./service/` | **ok 1038.1s** |
| `go test ./tests/mcpintegration/` | **ok 50.9s**（含 M1-01 标注/面切换用例更新为精确原因断言） |
| `go test ./tests/botintegration/` | **ok 52.7s** |

### 4.2 前端

见 §5.4。

## 5. 页面与前端

### 5.1 交付物（前端）

| 文件 | 变更 |
| --- | --- |
| `src/pages/(main)/admin/mcp-servers/CapabilitySwitchesCard.tsx`（新增） | 「能力开关」卡：三开关 + 来源徽标（已由管理台覆盖 / 跟随环境默认 + 环境默认值）+ 风险说明 + 保存（仅提交变更字段，避免把「跟随默认」意外固化为覆盖）+ 恢复默认（二次确认）；无 `system_config:write` 时只读 |
| `src/pages/(main)/admin/mcp-servers/index.tsx` | 挂载开关卡；工具治理页签在 `mcpWriteEnabled=false` 时显示 Alert「外部写工具面已关闭…配置仍可保存，但暂不生效」 |
| `src/lib/api/system-config-api.ts` | `AICapabilities` / `AICapabilitiesPatch` / `AICapabilityKey` 类型 + `getAICapabilities()` / `updateAICapabilities()`；`isAICapabilityOverridden()` 兼容 raw / `mcp.writeEnabled` / `mcpWriteEnabled` 三种 key 形态 |
| `src/lib/api/{mcp-api,bot-api,ai-api}.ts` | 列表响应类型补 `capabilities` 字段（展示用，覆盖已有测试与类型） |
| `src/pages/(main)/admin/bots/index.tsx` | 能力横幅（Bot 关闭 / 写面关闭）+ 写操作禁用态 + 授权抽屉写工具徽标 |
| `src/pages/(main)/admin/tools/index.tsx` | 能力横幅 + MCP 写工具徽标 |
| `src/components/ai/{AIChat.tsx,BotSelector.tsx}` | `botEnabled=false` 时禁用选择器并提示（与候选列表同一次请求取 capabilities，不新增请求） |
| `src/lib/i18n/translations.ts` | 上述文案 zh-CN / en-US 双语键 |

### 5.2 真实浏览器验证（`/admin/mcp-servers`）

| 位置 | 实测 |
| --- | --- |
| 「能力开关」卡 | 三开关 + 徽标渲染正确；`外部写工具面` 显示「跟随环境默认 · 环境默认：false」；更新人/时间随保存刷新 |
| 保存流程 | 开关改为「开启」→「保存」→ toast「已生效，无需重启」；徽标变为「已由管理台覆盖 · 环境默认：false」 |
| 恢复默认 | 二次确认弹窗（「将删除已覆盖的键对应的配置行…立即生效，无需重启」）→ 徽标回到「跟随环境默认」 |
| 工具治理页签 | 写面关闭时显示 Alert「外部写工具面已关闭：全局写面关闭期间，写工具不会下发/执行；『启用/分类』配置仍可保存，但暂不生效」 |
| 服务器行 | 「工具（启用/总数）= 3/6」、「生效工具 3」（A9 修复后的正确渲染） |
| Bot 页 | 列表与授权入口正常渲染；写面状态经 `/admin/bots` 的 `capabilities` 下发（关闭态下写操作禁用，由 jest 用例覆盖） |

> 说明：浏览器工具的工作区根不包含 `docs/`，未能落盘截图；页面截图/trace 随 CI `e2e-mcp.yml` 归档。

### 5.3 自动化结果（前端）

| 命令 | 结果 |
| --- | --- |
| `npx tsc --noEmit` | **exit 0** |
| `npx eslint <改动文件>` | **exit 0** |
| `npx jest`（system-config-api / mcp-api / ai-api / bot-selector） | **4 套件 / 80 用例全绿** |
| `npx jest --testPathPattern "admin/(mcp-servers|bots|tools)"` | **5 套件 / 32 用例全绿**（见 §5.4） |

### 5.4 页面级用例（5 套件 / 32 用例）

| 套件 | 覆盖 |
| --- | --- |
| `CapabilitySwitchesCard.test.tsx` | 三键生效值 + 来源徽标；保存仅提交变更字段（缺省=不改）；恢复默认仅重置已覆盖键；无 `system_config:write` 只读；读取失败（无 read / 503）降级不阻塞 |
| `admin/mcp-servers/__tests__/index.test.tsx` | 能力开关卡挂载 + 写面关闭时治理页签 Alert 与写工具徽标（只读行不加）；服务器三态/隔离计数/工具生效状态/批量停用二次确认；凭据只写不读回；降级路径（404/503） |
| `admin/bots/__tests__/index.test.tsx` | `botEnabled=false` → 顶部 Alert + 新建/编辑/删除/授权保存全部禁用（含 tooltip）；`mcpWriteEnabled=false` → 写面 Alert + 写工具授权徽标 + 选中写工具后禁止保存；`bot.enabled` 静态关闭（404）降级；无 `ai:write`（403）只读 |
| `admin/tools/__tests__/index.test.tsx` | 能力关闭禁用态：MCP 关闭 / 写面关闭 Alert + MCP 写工具行徽标（只读行不加） |
| `mcp-helpers` 等既有套件 | 回归保持 |

> 修复记录：`isAICapabilityOverridden` 的 key 形态兼容（raw / `mcp.writeEnabled` / `mcpWriteEnabled`）在首轮失败后修正——`http-client.ts` 的 `toCamelCase` 只转 `_x`、**保留点号**，测试与实现按真实归一化规则对齐（`system-config-api.test.ts` 三形态断言全绿）。

## 6. 残留与后续

| 项 | 说明 |
| --- | --- |
| 多实例收敛 | 写入实例立即生效；其他实例 ≤30s（TTL）。二期可接 pub/sub 做秒级一致 |
| 截图与 E2E | 页面截图与 Playwright 用例（切换开关 → 目录断言）随 CI `e2e-mcp.yml` 归档；本机浏览器工具无法写 `docs/` 目录（工作区根限制） |
| `ai.enabled` | 整个 AI 助手总开关不在本期范围（如需另开决策） |

## 7. 变更记录

| 日期 | 变更人 | 内容 |
| --- | --- | --- |
| 2026-09-30 | AI 辅助执行 | 初稿：后端交付清单、真实进程免重启验证（8 步）、后端 11 个包测试结果、残留与后续；前端与页面证据待补 |
| 2026-09-30 | AI 辅助执行 | 补全 §5：前端交付清单、真实浏览器验证（能力开关卡/保存/恢复默认/工具治理 Alert）、前端 tsc/eslint 与 9 套件 112 用例全绿；记录 `isAICapabilityOverridden` key 形态修复 |
