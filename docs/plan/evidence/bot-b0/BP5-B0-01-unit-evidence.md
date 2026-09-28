# BP5 + B0-01 单元/集成证据

> 文档类型：实施证据（任务 BP5、B0-01）
> Status: draft
> 编制日期：2026-09-27
> 任务：BP5（Bot 运行时模块骨架与开关）、B0-01（工具元数据模型与全量标注）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§2.2 BP5、§4.1 B0-01、§5.2 AB0-01）
> 核查方式：本机编译 + 定向测试 + gofumpt（v0.7.0，与 CI 同版本）；未执行项明确标注

## 1. 结论

| 任务 | 目标级别 | 本轮判定 | 依据 |
| --- | --- | --- | --- |
| BP5 | 前置（开工检查清单项） | **完成（代码面）** | `service/bot/` 包骨架 + `bot.enabled` 开关（默认 false）+ 配置/测试；关闭态零行为变化（无任何装配点） |
| B0-01 | `unit_verified` | **`unit_verified`** | 14/14 工具元数据标注 + 守卫测试 + 调用时快照落库链路 |

## 2. 交付物

| 交付物 | 位置 | 说明 |
| --- | --- | --- |
| Bot 运行时骨架 | `itsm-backend/service/bot/{doc.go,gate.go,gate_test.go}` | 包边界（BotPolicy/RunManager/ScopeResolver/Redactor 的落位与归属任务）；`Gate` 为唯一开关判定入口，禁止包内直读全局配置 |
| Bot 全局开关 | `itsm-backend/config/config.go`（`BotConfig`、`applyBotDefaults`、`LoadConfig` 接线）、`itsm-backend/config.yaml.example` | `bot.enabled` 默认 **false**；环境变量兜底 `BOT_ENABLED`；关闭时不装配任何组件（当前无装配点，天然零行为变化） |
| 工具元数据模型 | `itsm-backend/service/tool_registry.go` | `ToolDefinition` 新增 `Category/SupportsDryRun/Idempotent/TimeoutMs/MaxOutputBytes/RedactionProfile`（`Risk` 复用 M1-02 已有字段）；新增风险/脱敏/默认值常量与 `NormalizeToolMetadata` |
| 14 工具全量标注 | 同上（`ListTools()` 内 14 处） | 风险矩阵：读工具 `read`（8，其中 `get_ci_impact`=`plan`）；写工具 `link_ticket_ci/create_ticket=act_low`、`update_ticket=act_medium`、`create_ticket_type/create_ci_relationship/delete_ci_relationship=act_high` |
| 保守兜底 + 不默认下发 | 同上（`GetTool` 归一化、`listBuiltinToolsForTenant` 丢弃未标注） | 缺失元数据 → `act_high` + `strict` + `annotated=false`；未完整标注的内置工具**不进工具面** |
| 守卫测试 | `itsm-backend/service/tool_metadata_test.go` | 14/14 标注完整性 + 风险矩阵逐项比对 + 兜底语义 + `GetTool` 归一化 |
| 调用时快照 | `handlers/ai/entity.go`、`handlers/ai/repository_impl.go`、`handlers/ai/service.go`、`service/tool_provider.go`、`mcp/provider/execute.go` | `ToolInvocation.Risk/Category`：内置取自注册表、MCP 取自治理标注，随 pending/审计一次落库（`tool_invocations.risk/category`，列由 MCP M0-03 联合迁移预先创建） |
| 配置测试 | `itsm-backend/config/config_bot_test.go` | `bot.enabled` 解析（yaml + 环境变量覆盖）与默认关闭 |

**兼容性**：`ToolDefinition` 旧字段未移除、语义未变；新增字段均为 `omitempty`（API 增量）。`config.LoadConfig` 新增 `viper.Set("bot", ...)` 与既有 `mcp/attachment` 等同一范式（缺该行会导致 `${BOT_ENABLED:false}` 占位符无法解析——本轮实测暴露并修复）。

## 3. 测试记录（本机）

| 命令 | 结果 |
| --- | --- |
| `gofumpt -w` + `gofumpt -l ./service ./handlers/ai ./mcp/provider ./config` | 无输出（与 CI 同版本 v0.7.0） |
| `go build ./...` | exit 0 |
| `go test ./service/ -run 'TestBuiltinToolMetadataComplete\|TestNormalizeToolMetadata\|TestGetToolReturnsNormalized\|TestToolRegistry'` | **ok**（守卫测试 + 既有注册表用例） |
| `go test ./config/ -run 'Bot\|MCP'`、`go test ./service/bot/` | **ok** |
| `go test ./service/ ./handlers/ai/ ./mcp/provider/ ./config/ -count=1 -timeout 25m` | 见文末「本轮收尾复跑」（全量受影响包） |

## 4. 与方案的偏差与未闭环项

1. **BP5 范围收缩（有意）**：BP5 只交付「骨架 + 开关」；`BotPolicy/RunManager/ScopeResolver/Redactor` 的空壳实现留到各自任务（B2-02/B1-02/B3-01/B0-06），避免以占位代码伪装进度。BP8（预算/护栏参数）按计划在 B1-02 前扩展同一配置块。
2. **`SupportsDryRun` 已在元数据标注**，但 dry-run 执行分支属 B0-04；本轮只落标注与断言。
3. **幂等键生成/落库属 B0-05**；本轮仅落地 `Idempotent` 元数据与「写工具一律要求幂等」的归一化规则。
4. **`input_redacted` 命名**：与 MCP M0-03 联合迁移统一为 `args_redacted`（工具侧同时保留 `redact.ArgsJSON` 既有入口）；`output_summary` 已存在。B0-06 在此之上补 `redaction_profile` 档位差异。
5. **未执行**：Postgres 侧迁移验证（本机无 Docker/Postgres）——沿用 MCP `mcp-postgres-migrations` CI 作业覆盖；前端/浏览器级验证不在本任务范围。

## 5. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | BP5 + B0-01 交付证据：模块骨架与开关、14 工具标注、守卫测试、调用时快照；含实测命令与结果 |
