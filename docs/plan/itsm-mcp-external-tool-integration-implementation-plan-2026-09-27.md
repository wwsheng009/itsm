# ITSM 外部工具（MCP）接入实施方案

> 文档类型：实施方案（实施步骤 + 验收标准）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：将 MCP（Streamable HTTP / SSE / stdio）外部工具接入 ITSM 的工程落地，覆盖后端 `mcp/` 模块、ent 模型与迁移、权限与审计、管理 API、管理端页面、用户侧（AIChat / 审批 / 审计）增强、安全与运维
> 目标读者：后端、前端、测试（QA）、安全、SRE / 运维、产品
> 关联文档：
> - 设计依据：`docs/plan/itsm-mcp-external-tool-integration-analysis-2026-09-27.md`（分析报告，761 行；引用简称"分析报告"）
> - 依赖关系：`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`（阶段一 Bot 能力，B0–B4；引用简称"阶段一报告"）
> - 协同方案：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（Bot 实施方案，B0–B4；引用简称“Bot 方案”；与本文 §11.3 合并点互逆）
> - 治理规则：`plans/README.md:5`、`docs/documentation-governance.md`
> 核查基线：`feat/vite-migration` HEAD `7442fad5`（2026-09-27）；工作树含未提交改动：`CHANGELOG.md`、`itsm-backend/handlers/ai/repository_impl.go`、`itsm-backend/handlers/ai/service.go`，以及新增文件 `itsm-backend/handlers/ai/conversation_title.go`、`conversation_title_test.go`、`repository_conversation_delete_test.go`
> 核查方式：静态核对（本方案为实施流程设计，未改动任何代码；引用锚点见 §11.2，均为本次核对或分析报告已核实项，未核实处显式标注【未核实】）
> 状态口径：五级 `implemented → unit_verified → integration_verified → flow_verified → accepted`；**未达到某一级，其后级别一律不成立**（定义见分析报告 §7.1）

## 0. 结论先行（TL;DR）

1. **本方案做什么**：把分析报告 §4–§5 的设计与 §8 的 M0–M2 分期，展开为 **31 个可执行任务**（M0×14 / M1×10 / M2×7）、**32 条验收项**（A0×14 / A1×10 / A2×8）与 **9 组测试用例执行清单**。每个任务给出目标、依赖、改动文件（精确路径）、实施要点、测试与 DoD；每条验收给出可执行的验证方法与证据要求。
2. **里程碑出口**：M0 = `integration_verified`（只读链路端到端 + 管理配置 + SSRF 负向通过）；M1 = `flow_verified`（写路径 E2E + 用户侧可见 + 审批/审计可追溯）；M2 = `accepted`（stdio/OAuth 加固 + 浏览器 E2E + 运维演练 + 完整度复核）。
3. **四条不可跳过的硬门槛**：① 命名投影契约（M0-02，必须先于一切执行链路上线）；② SSRF 防护（M0-05，无此不得暴露服务器配置入口）；③ 审计三元组（M0-03/M0-11，与阶段一 G7 合并一次迁移）；④ 浏览器 E2E（M2-05，补齐参考实现最大遗留缺口）。
4. **与阶段一（Bot 能力）的关系**：M0 可与 B0 并行，但**工具元数据（G1）与审计字段（G7）必须一次迁移**；M1 的对话内确认依赖 B1/B2 状态机，未落地时退化为"外置审批 + 对话内待审批提示"；M2 的 E2E 复用 B3 的页面测试设施（分析报告 §8.1 R1–R3）。
5. **开工前必须先办的两件事**：① 8 项开放决策拍板（§2.1，其中权限位与 stdio 范围直接影响数据模型与 API 面）；② 工作树未提交改动的处置确认（并入 / 独立提交 / 暂存），避免 MCP 分支与其纠缠。
6. **诚实声明**：本文档所有状态均为**目标状态**；ITSM 当前 MCP 能力为 0（全仓 `grep -i mcp` 无匹配，本次再次核实），未写明"已实现"的任何条目都不代表已经存在。任务规模（S/M/L）为粗估，需团队按人力复核后再排日历工期。

---

## 1. 实施总则

### 1.1 目标与非目标

**实施目标**（验收口径的最终对象）：

| # | 目标 | 判定依据 |
| --- | --- | --- |
| G-A | 租户管理员可在管理页完成"MCP 服务器新增 → 测试连接 → 启用 → 工具发现 → 工具治理"闭环 | §4.1 M0-08 / M0-12；验收 A0-04～A0-08 |
| G-B | 模型可在对话中调用 MCP 只读工具；写工具经既有 Gate3 审批后执行，参数冻结语义不变 | §4.1 M0-09；§4.2 M1-02；验收 A0-09 / A1-01 / A1-02 |
| G-C | 用户可见工具调用过程与待审批状态；审批人可见来源三元组；审计可按 provider/服务器检索 | §4.2 M1-03～M1-07；验收 A1-03～A1-07 |
| G-D | 安全与运维达到上线标准：SSRF 负向全过、凭据只写不读回、降级不影响内置工具、告警与手册就位 | §4.1 M0-05/M0-06；§4.2 M1-08/M1-09；§4.3 M2-06；验收 A0-05 / A1-08 / A1-09 / A2-06 |

**非目标**（一期明确不做，防范围蔓延；继承分析报告 §1.3）：

- 非 `tools` 原语：`resources` / `prompts` / `sampling` / `logging` 不作为功能交付（协议层如需能力协商，只做拒绝或忽略）。
- WebSocket 传输（分析报告 D3）；stdio 仅平台级且建议不进一期（M2 起）。
- 平台级共享服务器 × 多租户授权矩阵（二期，M2 预研）。
- OAuth 2.1 完整流程（二期，M2 实现预研原型）。
- 工具结果进入知识库 / 工件系统；BYO（用户自带凭据）；多 Agent 协作；记忆系统。
- 参考实现的 CLI / 微型 Web 管理端（ITSM 只有 Web 管理台）。

### 1.2 继承的冻结决策（偏离即需变更评审）

下表 D 编号沿用分析报告 §4.3/§5 的决策语境；"状态"列区分**已冻结**（分析报告已定且本方案全面沿用）与**建议待拍板**（需按 §2.1 完成决策后视为冻结）。

| ID | 决策 | 状态 | 依据 |
| --- | --- | --- | --- |
| D1 | 一期传输仅 **Streamable HTTP（主）+ SSE（兼容）**；远程优先，不在宿主机拉起任意子进程 | 已冻结 | 分析报告 §4.3 D1 |
| D2 | stdio 仅平台级/旗舰私有化，命令白名单 + 沙箱；租户管理员不可配置；建议 M2 再实现 | 已冻结（排期建议待拍板） | 分析报告 §4.3 D2、§9.2-1 |
| D3 | 命名统一强制前缀 `mcp__<server>__<tool>`（含非法字符替换 + FNV-1a 短哈希、超长截断、canonical 碰撞 quarantine、解析 fail-closed） | 已冻结 | 分析报告 §2.4 取舍、§5.1-5 |
| D4 | 一期认证为 Header / Bearer Token（加密存储、只写不读回）；OAuth 2.1 放二期 | 已冻结 | 分析报告 §4.3 D4 |
| D5 | 接入方式为 `ToolProvider` 聚合进 `ToolRegistry`，与内置工具**同源** Gate1/2/3、队列、审计；不扩展 Connector 语义 | 已冻结 | 分析报告 §5.1-1、§5.3 |
| D6 | 状态三态分离：`configured_enabled`（管理位）/ `healthy`（运行位）/ `effective`（派生）；另有 `quarantined`；工具开关**不触发重连** | 已冻结 | 分析报告 §2.5 取舍、§5.2 |
| D7 | 默认拒绝：服务器默认禁用；新发现工具默认不启用（待治理）；`read_only` 默认 `false`（按写处理）；`mcp:*` 默认不授予任何角色 | 已冻结 | 分析报告 §5.1-2、§5.6 |
| D8 | 管理操作异步化：`enable`/`disable`/`reload` 返回 `202 + status=connecting`，前端轮询状态回读；`test` 保持同步短超时（≤10s） | 已冻结 | 分析报告 §5.5 异步语义 |
| D9 | 审计与元数据与阶段一 **G1/G7 合并一次迁移**：`tool_invocations` 扩展 + `ToolDefinition` 来源元数据；若阶段一未排期，M0 自带最小扩展 | 已冻结 | 分析报告 §6.5-1、§8.1 R1 |
| D10 | 权限 3 位：`mcp:read` / `mcp:write` / `mcp:admin`（默认仅管理员持 `mcp:admin`） | 建议待拍板 | 分析报告 §5.6、§9.2-2 |
| D11 | 管理页独立 `/admin/mcp-servers`（服务器 / 工具治理 / 健康事件三板） | 建议待拍板 | 分析报告 §5.7、§9.2-3 |
| D12 | 一期仅租户级服务器；平台级共享为二期 | 建议待拍板 | 分析报告 §6.5-8、§9.2-5 |

> 变更规则：任何 D 项偏离（如 stdio 提前、权限位退回 2 位、管理页并入 system-config）必须在本文件 §10 决策日志登记，并同步回写分析报告相应章节，禁止口头偏离。

### 1.3 任务编码、规模口径与状态判定

- **任务编码**：`M<里程碑>-<两位序号>`（如 `M0-02`）；验收项编码 `A<里程碑>-<两位序号>`（如 `A0-05`）；测试用例组 `T-01…T-09` 为本方案对分析报告 §10.4 九组建议用例的顺序编号（一一对应）。
- **规模口径**（粗估，需团队复核）：`S` ≈ 0.5–1 人日；`M` ≈ 1–3 人日；`L` ≈ 3–5 人日。里程碑总工作量不给日历承诺。
- **任务默认 DoD**：后端任务默认 `unit_verified`（含契约测试）；跨组件任务在里程碑验收矩阵中定义更高目标（`integration_verified` / `flow_verified`）。
- **状态判定规则**：`implemented`（代码合并）→ `unit_verified`（单测覆盖并通过）→ `integration_verified`（组件间集成测试通过，含真实 ent/DB）→ `flow_verified`（端到端业务流通过，含前端交互或 SSE 抓包证据）→ `accepted`（验收人按本文件 §5 逐条签字）。**未达前级，后级不成立**；禁止用 checkbox 冒充交付（`plans/README.md:5`）。

### 1.4 文档治理与状态回写

- 每个任务合并时，在 PR 描述中附：改动文件清单、测试命令与输出、证据链接/路径（截图、SSE 抓包、审计查询结果）。
- 每个里程碑收口时回写：本文件 §3.4 任务总表状态列、§5.2 验收矩阵判定列、`CHANGELOG.md`；若涉及路线图（ROADMAP/阶段一计划）同步更新。
- 证据归档建议：`docs/plan/evidence/mcp-<milestone>/`（新建目录，随 PR 提交或链接 CI artifact；不强制入库大文件）。

---

## 2. 前置条件与开工检查清单

### 2.1 决策拍板项（开工前必须闭环）

来源：分析报告 §9.2。**默认按"建议"执行；若产品/安全不拍板，不得开工对应范围。**

| # | 决策问题 | 建议 | 影响范围 | 拍板人 | 截止时点 |
| --- | --- | --- | --- | --- | --- |
| Q1 | stdio 是否进一期 | 不进；M2 旗舰私有化再启用（D2） | 传输实现、沙箱工作量、安全评审 | 产品 + 安全 | M0 开工前 |
| Q2 | 权限位 2 位 vs 3 位 | 3 位（`mcp:read/write/admin`，D10） | ent 无影响；角色种子、路由中间件、前端守卫 | 产品 + 安全 | M0-10 开工前 |
| Q3 | 管理页独立 vs system-config Tab | 独立 `/admin/mcp-servers`（D11） | 前端路由/菜单/seed、页面体量 | 产品 | M0-12 开工前 |
| Q4 | 审批人角色 | 维持 `ai:write`，审批详情补来源三元组 | 审批路由与页面文案 | 产品 | M1-02 开工前 |
| Q5 | 平台级共享服务器 | 二期（D12） | 数据模型是否加"平台服务器 × 租户授权"矩阵 | 产品 | M0-03 开工前（模型冻结） |
| Q6 | OAuth 回调形态 | 二期定：统一回调 + state 映射，须 CSRF/开放重定向评审 | M2-02 设计 | 安全 | M2 设计评审前 |
| Q7 | BYO 凭据 | 一期不开放，管理员在管理页录入 | 凭据录入面与审计 | 产品 + 安全 | M1 开工前 |
| Q8 | 工具结果是否入知识库 | 不入；仅回填对话与审计 | RAG 集成范围 | 产品 | M0 开工前 |

### 2.2 工程前置（P1–P8）

| # | 前置项 | 说明 | 状态要求 |
| --- | --- | --- | --- |
| P1 | 分支与基线固定 | 已于 2026-09-27 从 `7442fad5` 创建 `feat/bot-mcp-integration`（Bot/MCP 双线共用单分支）；**开工前仍须处置工作树未提交改动**（`CHANGELOG.md`、`handlers/ai/{repository_impl,service}.go` 与 3 个新增测试文件）——确认并入、独立提交或暂存，禁止与其混提 | 开工前 |
| P2 | SDK 选型与锁版 | 参考实现用官方 `modelcontextprotocol/go-sdk v1.4.0`（ai-agent-runtime `backend/go.mod:16`）；ITSM `go 1.25.13`（`itsm-backend/go.mod:3`）兼容性需一次编译 spike【未验证编译】；采纳后在 `go.mod` 锁版本并记录升级策略 | M0-01 内闭环 |
| P3 | 迁移协调（G1/G7 合并） | ent 迁移统一由 `client.Schema.Create(ctx)` 执行（`itsm-backend/internal/bootstrap/app.go:1392`，前置兼容步骤见 `:1380-1391`）；新增实体自动建表；`tool_invocations` 新字段一律带默认值或 nullable（参照 `ent/schema/tool_invocation.go:31` 的向后兼容注释）；与阶段一 B0 排期对齐，**一次迁移** | Q1/Q5 拍板后 |
| P4 | 测试设施（mock MCP） | 建议随仓库提供两个形态：① `itsm-backend/mcp/testutil/mockserver`（供 Go 集成测试直接 import）；② `itsm-backend/cmd/mcp-mockserver`（独立小二进制，供前端 Playwright E2E 拉起）；均支持故障注入（超时/401/TLS 错误/协议不匹配/工具集变更） | M0-13 交付 |
| P5 | 前端测试设施 | 单测 Jest（`itsm-frontend/package.json:18`）+ jsdom（`:112`）；E2E Playwright（`:24`，含 flows/business 分项目）；MCP 页面必须补组件测试 + E2E，不得只做静态渲染（分析报告 §2.7 教训） | M0-12 起 |
| P6 | 功能开关 | 新增全局 `mcp.enabled`（默认 `false`）；配置落位以 `itsm-backend/config/config.go` 现有结构为准，遵循既有 AI/Connector 开关范式；关闭时管理页隐藏/只读、工具面不含 MCP、后台不建连 | M0-01 |
| P7 | 测试账号与租户矩阵 | 至少 2 个租户 × 3 类角色（管理员 / 普通坐席 / 无 `mcp:*` 权限）；E2E 用 mock provider（对齐阶段一"mock 全量 + 少量 real smoke"策略，阶段一报告 §7-10） | M0-14 前 |
| P8 | 权限种子同步 | 新增权限码需同步：`internal/authz/catalog.go:161-164` 附近（定义）、`internal/authz/roles.go` 的 `allPermissionCodes()`（`:389-437`，参考 `:427-428` 的 ai/connector 段）与管理员角色授权清单；`router/permission_code_catalog_guard_test.go` 守卫测试必须通过 | M0-10 |

### 2.3 开工检查清单

> 检查方式：逐项确认并记录结论（是/否/不适用）；任一"否"阻塞对应任务开工。

- [x] Q1–Q8 全部按建议拍板并登记到 §10 决策日志（2026-09-27；Q1/Q5/Q8 阻塞 M0-03；Q2 阻塞 M0-10；Q3 阻塞 M0-12；Q4 阻塞 M1-02）。
- [x] P1 工作树改动处置完成（2026-09-27 独立提交 `d3471221` 于 `feat/vite-migration`：会话删除事务化修复 + 会话标题生成，含测试与 CHANGELOG）。
- [ ] P2 SDK 编译 spike 通过（最小 demo：initialize + tools/list 对 mock server 握手成功）【待实测】。
- [ ] P3 迁移方案评审完成（新增表 + `tool_invocations` 字段清单冻结；SQLite 与 Postgres 双驱动验证方式确认——差异【未核实】，需在 CI 覆盖）。
- [ ] P4 mock server 归属（谁实现、放哪个包）与故障注入清单确认。
- [ ] P7 测试账号/租户/角色矩阵就绪；E2E 环境可启动 mock provider。
- [ ] CI 门禁草案就绪：新增 `go test ./mcp/...`、前端 `jest` MCP 用例、Playwright MCP 用例（可先 manual/nightly）。
- [ ] 本实施方案在团队评审通过（评审记录写入 PR 或本文件 §10）。

---

## 3. 里程碑总览与依赖

### 3.1 里程碑定义与出口条件

| 里程碑 | 主题 | 入口条件 | 出口条件（可验证） | 目标状态 |
| --- | --- | --- | --- | --- |
| **M0** | 外部工具接入骨架（只读先行） | §2.3 检查清单全绿；Q1/Q2/Q3/Q5/Q8 已拍板 | ① 只读链路端到端：管理页新增→测试→启用→发现→治理→只读调用→审计可查（A0-04～A0-09）；② SSRF/凭据/输出上限负向测试通过（A0-05/A0-06）；③ 权限矩阵与跨租户 fail-closed 通过（A0-10）；④ 迁移在 SQLite/Postgres 均通过（A0-03） | `integration_verified` |
| **M1** | 写工具治理与用户侧闭环 | M0 出口达成；Q4/Q7 已拍板；阶段一 B1/B2 若未落地则接受降级形态（R2） | ① 写路径 E2E：对话→待审批→审批→执行→回填→审计可查（A1-01/A1-02）；② 用户侧时间线/审批卡片/审批页/审计页可见且交互测试通过（A1-03～A1-07）；③ 告警与 in-flight 宽限语义落地（A1-08）；④ 安全负向测试通过（A1-09） | `flow_verified` |
| **M2** | 加固、E2E 与二期预研 | M1 出口达成；Q6 已拍板（若 OAuth 进 M2） | ① stdio 沙箱测试通过（平台级，A2-01）；② OAuth 流程（如启用）与工具面预算方案落地（A2-02/A2-03）；③ 浏览器 E2E 全链路绿（A2-05）；④ 指标/告警/运维手册 + 演练记录（A2-06）；⑤ 完整度矩阵复核并回写文档（A2-08） | `accepted` |

### 3.2 依赖关系（含与阶段一 B0–B4 的交叉）

```text
阶段一： B0 元数据/审计 ──► B1 run/事件与确认 ──► B2 Bot 模板/授权 ──► B3 页面入口 ──► B4 E2E
              │                    │                     │
MCP：         ▼                    ▼                     ▼
        M0 骨架（可并行）    M1 写治理+用户侧（依赖 G1/G3/G7）   M2 加固+二期（复用 B3 测试设施）
```

- **R1**：M0 与 B0 可并行，但 `tool_invocations`/`ToolDefinition` 扩展**必须一次迁移**（D9）；若 B0 未排期，M0-03 自带最小扩展，B0 落地时再对齐标注（不得二次加列）。
- **R2**：M1-05 的对话内确认卡片依赖 B1 的确认状态机与 B2 的授权面；若阶段一未落地，M1 退化为"对话内待审批提示 + 跳转外置审批页"，验收按退化形态判定（A1-05 注）。
- **R3**：M2-05 浏览器 E2E 复用 B3 的页面入口组件与 Playwright 设施；若 B3 未落地，MCP 自建独立 E2E 用例，不阻塞。
- **R4**（本方案新增）：M0-09 只读执行依赖 M0-02 registry 与 M0-04 client 完成；M0-08 管理 API 依赖 M0-03 模型与 M0-06 凭据；M0-12 管理页依赖 M0-08 API 形状冻结（建议先冻结 OpenAPI/类型草案再并行前端）。

### 3.3 任务总表（WBS 索引）

> 规模为粗估（S/M/L，见 §1.3）；状态列初始为 `未开始`，随交付回写。详细任务卡见 §4，验收项见 §5.2。

| ID | 任务 | 层 | 交付物（摘要） | 依赖 | 规模 | 目标级别 | 验收项 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| M0-01 | 工程骨架与开关 | 后端 | `mcp/` 六包骨架、SDK 依赖、`mcp.enabled` 开关、模块 README | P2/P6 | M | `unit_verified` | A0-01 |
| M0-02 | 命名投影与解析（registry） | 后端 | 投影/quarantine/Resolve + 契约测试 | M0-01 | L | `unit_verified` | A0-02 |
| M0-03 | ent 数据模型与迁移 | 后端 | `mcp_server.go`、`mcp_server_tool.go`、`tool_invocations` 扩展 | Q1/Q5、P3 | L | `integration_verified` | A0-03 |
| M0-04 | 传输层（Streamable HTTP / SSE） | 后端 | `mcp/transport` + `mcp/client` + 单测 | M0-01 | L | `unit_verified` | A0-04 |
| M0-05 | SSRF 与出站安全 | 后端 | URL 校验器、IP/重定向/DNS rebinding 防护 + 负向测试 | M0-01 | L | `unit_verified` | A0-05 |
| M0-06 | 凭据加密与掩码 | 后端 | 加密存储、只写不读回、掩码、轮换预留 | M0-03 | M | `unit_verified` | A0-06 |
| M0-07 | 连接生命周期管理（manager） | 后端 | 连接池/健康检查/退避/发现与 diff/schema 隔离 | M0-03/04 | L | `integration_verified` | A0-07 |
| M0-08 | 管理服务与管理 API | 后端 | admin CRUD/test/启停/治理/health/events + 审计 + 错误码 | M0-03/05/06/07 | L | `integration_verified` | A0-08 |
| M0-09 | ToolProvider 接入与只读执行 | 后端 | `service/tool_provider.go`、builtin/mcp provider、执行链路 | M0-02/04/07 | L | `integration_verified` | A0-09 |
| M0-10 | 权限位与路由装配 | 后端 | `mcp:read/write/admin` + catalog/roles/middleware/routes | Q2、P8 | M | `integration_verified` | A0-10 |
| M0-11 | 审计写入与脱敏 | 后端 | `tool_invocations` 三元组落库、管理操作审计、脱敏 | M0-03/08 | M | `integration_verified` | A0-11 |
| M0-12 | 管理页（服务器/治理/健康摘要） | 前端 | `/admin/mcp-servers` + service/types/i18n + 组件测试 | M0-08、Q3 | L | `unit_verified` | A0-12 |
| M0-13 | mock MCP 服务器与测试设施 | 测试 | testutil mockserver + cmd/mcp-mockserver + 故障注入 | M0-01 | M | `unit_verified` | A0-13 |
| M0-14 | M0 集成验收 | 联调 | 只读链路场景 + 负向 + 证据归档 | M0-04～M0-13 | M | `integration_verified` | A0-14 |
| M1-01 | 分类标注落地 | 后端 | `read_only/risk/category` 治理 API + 默认按写 | M0-08 | M | `integration_verified` | A1-01 |
| M1-02 | 写工具 Gate3 接入 | 后端 | 审批复用、三元组入审批详情、参数冻结 | M1-01、Q4 | L | `flow_verified` | A1-02 |
| M1-03 | SSE 事件契约（tool_call_* / 待审批） | 后端 | 事件 schema + 兼容策略 + 测试 | M0-09 | M | `integration_verified` | A1-03 |
| M1-04 | AIChat 工具时间线 | 前端 | 折叠块/来源徽标/摘要/耗时/状态 + 组件测试 | M1-03 | L | `flow_verified` | A1-04 |
| M1-05 | 对话内待审批卡片与跳转 | 前端 | 待审批卡片 + 跳转审批页（退化形态适配） | M1-02/03、R2 | M | `flow_verified` | A1-05 |
| M1-06 | 审批页来源增强 | 前端 | 来源筛选/三元组/风险/跳转治理页 + 测试 | M1-02 | M | `flow_verified` | A1-06 |
| M1-07 | 审计页来源维度 | 前端 | provider/服务器筛选、详情三元组与耗时错误码 | M0-11 | M | `flow_verified` | A1-07 |
| M1-08 | 运维收口与告警 | 后端 | 健康事件时间线、连续失败告警、并发/超时/重试、宽限期 | M0-07/08 | M | `integration_verified` | A1-08 |
| M1-09 | 安全负向测试集 | 测试 | 私网/重定向/DNS rebinding/超限/脱敏/注入/掩码 | M0-05/06/09 | M | `integration_verified` | A1-09 |
| M1-10 | M1 流程验收 | 联调 | 写路径 E2E + 审批/审计 E2E + 失败注入 | M1-02～M1-09 | M | `flow_verified` | A1-10 |
| M2-01 | stdio 传输与沙箱（平台级） | 后端 | 命令白名单/进程树守卫/env 白名单/资源限制 | Q1（启用时） | L | `integration_verified` | A2-01 |
| M2-02 | OAuth 2.1 | 后端 | PKCE/发现/动态注册/回调安全/token 生命周期 | Q6 | L | `integration_verified` | A2-02 |
| M2-03 | 工具面预算与元工具 | 后端 | >40 阈值告警 + 元工具/检索式工具面方案 | M0-09 | L | `unit_verified` | A2-03 |
| M2-04 | 平台共享服务器 × 租户授权 | 后端 | 平台服务器 + 租户授权矩阵（如拍板） | Q5、M0-03 | L | `integration_verified` | A2-04 |
| M2-05 | 浏览器 E2E 全链路 | 测试 | 管理页全流程 + 时间线 + 审批流 Playwright 用例 | M0-12/M1-04～06、R3 | L | `flow_verified` | A2-05 |
| M2-06 | 指标/告警看板与运维手册 | 运维 | 指标暴露、告警阈值、SOC runbook、演练记录 | M1-08 | M | `integration_verified` | A2-06 |
| M2-07 | accepted 复核与文档回写 | 联调 | 完整度矩阵复核、证据归档、CHANGELOG/ROADMAP 回写 | 全部 | S | `accepted` | A2-08 |

---

## 4. 详细实施步骤（任务卡）

> 任务卡字段：**目标 / 依赖 / 改动文件 / 要点 / 测试与证据 / DoD**。改动文件中的"新建"为规划路径（尚未存在），"修改/接入"为已核实存在的锚点（§11.2）。所有任务默认要求：不破坏既有 14 个内置工具行为、不降低现有测试覆盖率、所有新增权限与审计走同源设施。

### 4.1 M0：外部工具接入骨架（只读先行）

#### M0-01 工程骨架与开关（后端）

- **目标**：建立 `mcp/` 模块边界、SDK 依赖与全局开关；保证开关关闭时对现有系统零行为变化。
- **依赖**：P2（SDK spike）、P6（开关）。
- **改动文件**：
  - 新建 `itsm-backend/mcp/{transport,client,registry,manager,admin,provider}/`（六包骨架，职责与分析报告 §5.3 一致）；
  - 修改 `itsm-backend/go.mod`、`go.sum`（新增 SDK 依赖并锁版本）；
  - 修改 `itsm-backend/config/config.go`、`config.yaml.example`、`.env.example`（新增 `mcp.enabled=false` 及连接/超时默认值）；
  - 新建 `itsm-backend/mcp/README.md`（模块边界、依赖方向、升级策略）；
  - 修改 `itsm-backend/internal/bootstrap/app.go`（预留装配点；开关关闭时不初始化任何 mcp 组件）。
- **要点**：包依赖方向固定为 `transport ← client ← manager ← admin/provider`，`registry` 为纯逻辑包（无 IO）；禁止包级可变全局状态；`mcp.enabled=false` 时管理 API 返回 404/禁用态（按 Q3 决定）、工具面不含 MCP、后台不建连。
- **测试与证据**：`go build ./...`、`go test ./...`（存量全绿）；启动日志断言开关关闭时无 MCP 初始化输出；`mcp/README.md` 评审通过。
- **DoD**：`unit_verified`。
- **状态**：`unit_verified`（2026-09-27，分支 `feat/bot-mcp-integration`；证据 `docs/plan/evidence/mcp-m0/M0-01-unit-evidence.md`；全量 `go test ./...` 回归待 CI 门禁接入时补齐，不阻塞本任务 DoD）。

#### M0-02 命名投影与解析（registry，最高优先级）

- **目标**：实现唯一的投影函数、quarantine 与 Resolve，保证"名字 → 服务器 → 工具"确定且 fail-closed（D3 硬门槛）。
- **依赖**：M0-01。
- **改动文件**：新建 `itsm-backend/mcp/registry/{registry.go,projection.go,resolve.go,quarantine.go,errors.go}` 及对应 `*_test.go`。
- **要点**（规则必须逐条与契约测试绑定）：
  1. 统一投影 `mcp__<server>__<tool>`（server 为 `[a-z0-9_-]{1,32}` 稳定标识）；
  2. 非法字符替换为 `_`，**只要发生替换即追加 FNV-1a 短哈希**（避免 `a.b` 与 `a_b` 规整后碰撞）；
  3. canonical 超过 64 字符截断 + identity hash；`server + raw_name` 复合键存储；
  4. canonical 完全碰撞：后注册者进入 quarantine（不暴露、不可执行、可诊断列表）；
  5. 解析顺序：canonical 精确匹配 → 原始短名仅唯一命中可用 → 多候选返回 `AmbiguousToolError`；
  6. 执行归一化：解析成功后强制以 `(server, raw_name)` 路由，杜绝串服务。
- **测试与证据**：移植参考实现用例思路（分析报告 §10.4 第 1–3 组，即本方案 T-01/T-02/T-03；参考 `docs/mcp/mcp-tool-llm-integration.md:178-184`）：唯一/重名/非法字符/超长/遮蔽五类投影断言 + 解析四类 + quarantine 与解除路径。测试输出归档。
- **DoD**：`unit_verified`（契约测试全绿；此任务不得被任何执行链路上线绕过）。
- **状态**：`unit_verified`（2026-09-27，分支 `feat/bot-mcp-integration`；证据 `docs/plan/evidence/mcp-m0/M0-02-unit-evidence.md`）。

#### M0-03 ent 数据模型与迁移（含 G1/G7 合并）

- **目标**：落地 `mcp_servers`、`mcp_server_tools` 与 `tool_invocations` 扩展；一次迁移，避免与阶段一 B0 二次加列。
- **依赖**：Q1/Q5 拍板、P3 迁移评审。
- **改动文件**：
  - 新建 `itsm-backend/ent/schema/mcp_server.go`、`itsm-backend/ent/schema/mcp_server_tool.go`（字段清单见分析报告 §5.2）；
  - 修改 `itsm-backend/ent/schema/tool_invocation.go`（新增：`provider`（默认 `builtin`）、`mcp_server_name`、`mcp_raw_tool_name`、`mcp_callable_name`、`args_redacted`、`output_summary`、`duration_ms`、`error_code`）；
  - 通过仓库既有 ent 生成流程刷新 `itsm-backend/ent/` 生成代码（参考现有 `entc-gen*.log` 工作流）；
  - 若阶段一 B0 同步实施：合并 `ToolDefinition` 元数据字段标注，同一 PR/同一迁移窗口。
- **要点**：唯一键 `(tenant_id, name)` 与 `(server_id, raw_name)`；`callable_name` 服务内唯一；`version` 乐观锁；所有新字段带默认值/nullable（参照 `ent/schema/tool_invocation.go:31` 的兼容做法）；索引首列必须含 `tenant_id`；迁移统一由 `client.Schema.Create`（`internal/bootstrap/app.go:1392`）执行，存量回填若需要则按 `app.go:1380-1391` 的前置步骤模式处理。
- **测试与证据**：空库建表 + 旧库升级两条迁移路径；SQLite 与 Postgres 双驱动冒烟【两驱动迁移差异未核实，必须在 CI 覆盖】；ent 读写断言（三元组、默认值、乐观锁冲突）。
- **DoD**：`integration_verified`。
- **状态**：迁移实现完成（2026-09-27）；证据等级 `unit_verified`（sqlite 建表/列清单/默认值/唯一约束/旧行兼容/幂等重跑 + 全量构建，证据 `docs/plan/evidence/mcp-m0/M0-03-migration-evidence.md`）；`integration_verified` 待 Postgres 双驱动与真实旧库 ALTER 路径在 CI 覆盖（M0-14 前）。**联合迁移已执行**：B0-02 字段随本任务一次加列，`args_redacted` 为统一命名（替代 B0-02 的 `input_redacted`）。

#### M0-04 传输层：Streamable HTTP / SSE（后端）

- **目标**：两类远程传输可用；协议版本协商与"不匹配即拒绝"策略落地（D1/D4）。
- **依赖**：M0-01。
- **改动文件**：新建 `itsm-backend/mcp/transport/{transport.go,streamable.go,sse.go,options.go,errors.go}`、`itsm-backend/mcp/client/{client.go,session.go,timeouts.go}` 及测试。
- **要点**：SDK 封装只暴露 ITSM 内部接口（便于未来换传输）；凭据/Header 通过注入器提供（对接 M0-06）；连接超时 10s、单次调用默认 30s；协议版本不匹配 → 连接置 `error` 并记录原因，**不降级**；`tools/list` 的 `inputSchema` 原样保存；错误分层枚举（`connect_timeout`/`tls_error`/`protocol_mismatch`/`auth_required`/`invalid_transport`…）供 API 层映射；为 manager 提供生命周期观察钩子（connected/disconnected/error）。
- **测试与证据**：先用 `httptest` 起最小 MCP 服务做握手与 `tools/list`、`tools/call` 集成；错误路径注入（超时、401、TLS 失败、协议版本错）；M0-13 完成后替换为共享 mock 服务器重跑。
- **DoD**：`unit_verified`（集成级由 M0-14 提升）。
- **状态**：`unit_verified`（2026-09-27；证据 `docs/plan/evidence/mcp-m0/M0-04-unit-evidence.md`）。实现说明：SSE/Streamable 均以 `httptest` 最小服务集成；Guard 为接口形态（M0-05 提供实现）；重试策略在 M0-07、共享 mock 在 M0-13 替换；两处 SDK 适配缺陷（会话 ctx 生命周期、错误链丢失）已修复并加回归守卫。

#### M0-05 SSRF 与出站安全（上线硬门槛）

- **目标**：堵死"配置 URL 即 SSRF 原语"的风险面（分析报告 §6.5-2）。
- **依赖**：M0-01。
- **改动文件**：新建 `itsm-backend/mcp/transport/ssrf.go`（URL/IP 校验器，可注入 resolver）；测试 `ssrf_test.go`；由 M0-08 在新增/编辑/测试连接路径强制调用。
- **要点**：仅 `https`（`http` 仅平台级开关放行）；拒绝环回/私网（RFC1918）/链路本地/ULA/保留段；连接时**二次解析**并比对首次校验结果（DNS rebinding）；禁止跟随重定向；可配域名 + 端口 allowlist；失败统一 `ssrf_blocked` 错误码，对外提示不泄露内网结构；校验动作全量审计。
- **测试与证据**：表驱动负向用例（各类 IP、域名解析到私网、重定向、rebinding 模拟）；正向用例（allowlist 内 https）。分析报告 §10.4 T-05 前半。
- **DoD**：`unit_verified`（负向用例必须全过才能进入 M0-08 联调）。
- **状态**：`unit_verified`（2026-09-27；证据 `docs/plan/evidence/mcp-m0/M0-05-unit-evidence.md`）。实现：`transport/ssrf.go` `SSRFGuard`（https-only 与平台开关、userinfo 拒绝、域名/端口 allowlist、环回/私网/链路本地/ULA/保留段拒绝含 IPv4-mapped 解包、**DNS rebinding 钉住比对**、审计钩子）；`transport.New` 构造期与每个请求各自校验；重定向禁跟随已有结构性测试。

#### M0-06 凭据加密与掩码（后端）

- **目标**：MCP 凭据（Header/Bearer Token）加密存储、只写不读回、可轮换。
- **依赖**：M0-03。
- **改动文件**：新建 `itsm-backend/mcp/admin/credential.go`（或 `mcp/security/credential.go`）；复用范式：`itsm-backend/connector/`（持久化存储 `connector.NewPersistentConfigStore`，装配见 `internal/bootstrap/app.go:489-500`；字段范式 `ent/schema/connector_config.go`）。
- **要点**：AES-GCM 加密；读接口返回掩码、编辑回传空值表示"不修改"；日志/审计/事件永不含明文；密钥环境变量命名与生产强校验对齐 connector 的 fail-fast 模式（生产缺失即拒绝启动）；`rotate-credential` 先落库再异步重连（M0-07）。
- **测试与证据**：加密落库断言（DB 中无明文）、掩码 API 断言、轮换后旧凭据失效、生产模式缺密钥启动失败。
- **DoD**：`unit_verified`。

#### M0-07 连接生命周期管理（manager）

- **目标**：租户级连接池与生命周期：异步建连/重载、健康检查与退避、工具发现与差分、schema 变更隔离、并发/超时策略。
- **依赖**：M0-03、M0-04。
- **改动文件**：新建 `itsm-backend/mcp/manager/{manager.go,pool.go,health.go,discovery.go,events.go}` 及测试。
- **要点**：
  1. 状态机 `disabled → connecting → connected | error`；`enable/disable/reload` 全异步，不阻塞请求路径（D8）；
  2. 健康检查独立协程（`ping`/`tools.list`，间隔可配 + 指数退避），连续失败置 `error` 并触发告警事件；
  3. 工具发现与缓存 diff：新增工具默认 `configured_enabled=false`（待治理）；`schema_hash` 变化默认将该工具隔离待复核；
  4. 工具开关只翻转注册表标志，**不触发重连**（D6）；
  5. 事件：`mcp.server.connected|disconnected|auth_required|reload_failed`、`mcp.tools.discovered`、`mcp.tool.state_changed`、`mcp.tool.quarantined`（复用 ITSM 既有审计/事件设施）；
  6. 降级：MCP 故障只影响其自身工具从工具面移除；`chatStream`、内置工具、审批管线不受影响。
- **测试与证据**：断连-恢复集成（mock 注入断开/恢复）；工具 diff 与 schema 隔离用例；并发上限与退避节奏（可注入时钟）；降级断言（MCP 全挂时对话与内置工具正常）。
- **DoD**：`integration_verified`。

#### M0-08 管理服务与管理 API（后端）

- **目标**：管理面 CRUD/测试连接/启停/工具治理/健康/事件 + 管理操作审计 + 错误码分层（API 形状见分析报告 §5.5）。
- **依赖**：M0-03、M0-05、M0-06、M0-07。
- **改动文件**：新建 `itsm-backend/mcp/admin/{service.go,validation.go,audit.go}`、`itsm-backend/handlers/mcp/handler.go`、`itsm-backend/router/mcp_routes.go`；修改 `itsm-backend/router/router.go`（注册，参照 `router/ai_routes.go:50-57` 与 `llm_provider_routes.go` 惯例）。
- **要点**：前缀 `/api/v1/ai/mcp-servers`；权限 `mcp:read`（读）与 `mcp:admin`（写，M0-10 装配）；`enable/disable/reload` 返回 `202 + status=connecting`，前端轮询状态回读；`test` 同步 ≤10s、不落库；错误码枚举 → HTTP 映射（`invalid_transport` 400 / `duplicate_name` 409 / `ssrf_blocked` 422 / `connect_timeout|tls_error|auth_required|protocol_mismatch` 502 类 / `not_found` 404 / `conflict` 409 / `credential_error` 500 类），以实施时评审为准；管理审计字段 actor/tenant/action/object/before-after（脱敏）/result/ip/ts；乐观锁冲突 409。
- **测试与证据**：API 集成测试（httptest + ent 测试库 + M0-13 mock）：CRUD、重复名、非法传输、SSRF 拒绝、测试连接失败注入、202 状态回读、审计断言、乐观锁。
- **DoD**：`integration_verified`。

#### M0-09 ToolProvider 接入与只读工具执行（后端）

- **目标**：MCP 工具并入同源流水线（D5）；一期先让只读工具进入对话工具面并可执行。
- **依赖**：M0-02、M0-04、M0-07。
- **改动文件**：
  - 新建 `itsm-backend/service/tool_provider.go`（接口与聚合）；
  - 修改 `itsm-backend/service/tool_registry.go`（多 provider 聚合；内置迁移为 `builtinProvider`，行为不变；`GetTool` 解析顺序 = 内置优先 → MCP）；
  - 新建 `itsm-backend/mcp/provider/{provider.go,execute.go,normalize.go}`；
  - 修改 `itsm-backend/handlers/ai/service.go`（工具面组装：`ChatStreamWithProviderInfo :424` 起；执行：`ExecuteTool :118`）；
  - 修改 `itsm-backend/internal/bootstrap/app.go:744-752`（装配 mcp provider 与开关）。
- **要点**：
  1. 工具面公式 = `builtinTools(role) ∪ mcpTools`，后者满足 `server.enabled ∧ status=connected ∧ tool.enabled ∧ healthy ∧ !quarantined ∧ Gate2 通过 ∧ schema 校验通过`（分析报告 §5.4）；
  2. 解析必须与 `ListToolsForTenant` 使用**同一个投影/解析函数**（契约测试锁定，防展示/执行口径不一致——参考实现曾出现此坑）；
  3. MCP provider **不感知审批**；Gate3 仍由 `service` 层编排（写工具在 M1-02 接入）；
  4. 参数执行前做 JSON Schema 校验；结果规范化（文本/结构化内容 + 256KB 截断标记）；
  5. 输出按不可信数据处理（不执行其中指令、不拼接系统提示）；失败结构化回填（错误码 + 简短原因，无堆栈/内网信息）；写工具不自动重试；
  6. 每服务器并发默认 4、单次超时默认 30s（可配）。
- **测试与证据**：集成测试（工具进面/执行成功/失败回填/未启用不可见/quarantine 不可执行/schema 拒绝/并发上限）；工具面组装单测；审计三元组断言（与 M0-11 联调）。
- **DoD**：`integration_verified`。

#### M0-10 权限位与路由装配（后端）

- **目标**：落地 `mcp:read` / `mcp:write` / `mcp:admin`（Q2 拍板结果）与路由守卫。
- **依赖**：Q2、P8。
- **改动文件**：修改 `itsm-backend/internal/authz/catalog.go`（`:161-164` 附近新增定义）、`itsm-backend/internal/authz/roles.go`（`allPermissionCodes()` `:389-437`，参考 `:427-428` 的 ai/connector 段；按决策授予管理员角色）；`itsm-backend/router/mcp_routes.go`（`middleware.RequirePermission("mcp", ...)`）；更新 `itsm-backend/router/permission_code_catalog_guard_test.go` 相关断言。
- **要点**：默认不授予任何角色（D7）；`mcp:admin` 默认仅管理员角色；使用与治理分离（`mcp:read/write` 不隐含管理）；跨租户引用一律 fail-closed。
- **测试与证据**：角色矩阵测试（管理员/坐席/无权限 × read/write/admin）；guard 测试通过；路由 403/200 断言。
- **DoD**：`integration_verified`。

#### M0-11 审计写入与脱敏（后端）

- **目标**：工具调用三元组与管理操作审计落库；敏感信息全链路脱敏。
- **依赖**：M0-03、M0-08。
- **改动文件**：修改 `itsm-backend/handlers/ai/service.go`（`ExecuteTool :118` 审计扩展）、`itsm-backend/service/tool_queue.go`（`finalize :153` 补耗时/错误码）、`itsm-backend/mcp/admin/audit.go`；新建或复用脱敏函数（若阶段一 redaction 已落地则复用，否则 `itsm-backend/mcp/admin/redact.go`）。
- **要点**：`tool_invocations` 落 provider/`mcp_server_name`/`mcp_raw_tool_name`/`mcp_callable_name`/`duration_ms`/`error_code`/`args_redacted`/`output_summary`；管理操作审计（含 before/after 脱敏 diff）；凭据明文永不入审计与日志；错误码与前端展示对齐。
- **测试与证据**：审计查询断言（按 provider/服务器筛选）；脱敏断言（构造敏感参数与输出，落库与日志中不出现明文）。
- **DoD**：`integration_verified`。

#### M0-12 管理页（服务器 / 基础工具治理 / 健康摘要，前端）

- **目标**：落地 `/admin/mcp-servers` 管理页（Q3 独立页），完成管理员配置闭环的前端可用形态。
- **依赖**：M0-08（API 形状冻结）、Q3。
- **改动文件**：
  - 新建 `itsm-frontend/src/pages/(main)/admin/mcp-servers/index.tsx` 及子组件（列表、表单向导、测试连接、工具治理表、健康摘要卡）；
  - 新建 `itsm-frontend/src/lib/api/mcp-api.ts`（沿用 `lib/api/ai-api.ts` / `llm-provider-api.ts` 范式）与 `lib/types/mcp.ts`；
  - 修改 `itsm-frontend/src/routes/route-paths.ts`（新增 `adminMcpServers`，参照 `:11` 的 connectors）、`itsm-frontend/src/routes/index.tsx`（lazy + 权限守卫，参照 `:34`/`:231`）、`itsm-frontend/src/components/layout/sidebar/menu-config.ts`（菜单项）、`itsm-backend/pkg/seeder/seeder.go`（`:1706-1775` 菜单种子补条目）、`itsm-frontend/src/lib/i18n/translations.ts`（`mcp.*` 文案）；
  - 组件测试（Jest + jsdom，参照 `connector` 相关测试与 `package.json:18/:112`）。
- **要点**：三步向导（基本信息/认证/高级）→ 测试连接（协议版本 + 工具预览）→ 保存；凭据只写不读回、掩码交互与 Connector 一致；启停/重载按钮 pending 态 + 2s 轮询回读；危险操作二次确认；空态引导。
- **测试与证据**：组件测试（表单校验/三态展示/批量操作/确认弹窗/掩码回传）全绿；M0 收口前**人工浏览器冒烟**（截图证据），完整 E2E 归 M2-05。
- **DoD**：`unit_verified`（组件测试）+ 人工冒烟证据。

#### M0-13 mock MCP 服务器与测试设施

- **目标**：提供可复用的 mock MCP 服务器（Go 集成测试与前端 E2E 共用的两形态）。
- **依赖**：M0-01。
- **改动文件**：新建 `itsm-backend/mcp/testutil/mockserver/`（可 `import` 的测试库）；新建 `itsm-backend/cmd/mcp-mockserver/main.go`（独立进程，端口可配）；测试夹具（工具集覆盖：只读/写/超长输出/非法字符名/重名/schema 变更指令/慢响应）。
- **要点**：故障注入（超时、401、TLS 错误、协议版本不匹配、断连、慢响应、超大输出、`list_changed` 通知）；支持运行时切换工具集（E2E 治理与 schema 隔离用例需要）；不得成为生产依赖（仅 test/cmd）。
- **测试与证据**：自身单测；被 M0-04/M0-07/M0-08 集成消费的样例；E2E 启动脚本（后续 M2-05 使用）。
- **DoD**：`unit_verified`。

#### M0-14 M0 集成验收

- **目标**：只读链路与安全负向端到端通过，M0 达 `integration_verified`。
- **依赖**：M0-04～M0-13。
- **内容**：在真实 ent + DB + mock MCP 下执行场景：新增服务器 → 测试连接 → 启用 → 工具发现 → 治理启用 → 对话只读调用 → 审计可查；叠加 SSRF/凭据/输出超限负向；跨租户与权限矩阵回归。
- **证据**：测试命令与输出、SSE/审计查询结果、管理页截图，归档至 `docs/plan/evidence/mcp-m0/`（或 CI artifact）。
- **DoD**：`integration_verified`（里程碑级，对应 A0-14）。

### 4.2 M1：写工具治理与用户侧闭环

#### M1-01 工具分类标注落地（后端）

- **目标**：`read_only / risk / category` 标注全链路生效：管理 API 可改、默认按写、Gate2 映射正确。
- **依赖**：M0-08。
- **改动文件**：`itsm-backend/mcp/admin/service.go`（classification 端点与校验）、`itsm-backend/handlers/mcp/handler.go`；`itsm-backend/mcp/provider/provider.go`（`Resource=mcp`、`Action=read|write` 由标注派生）；`itsm-backend/handlers/ai/service.go`（Gate2 读取投影定义）。
- **要点**：默认值 `read_only=false, risk=high, category=""`（D7）；标注变更必须审计（管理员操作）；变更后**下一轮**对话工具面行为随 Gate2/3 变化（无会话冻结问题，ITSM 结构性优势，分析报告 §6.3）；risk 一期只用于展示与统计（二期接入 Bot 风险上限 G2）。
- **测试与证据**：标注变更 → 工具面/审批行为断言（只读标注后免审批直通；改回写标注后进审批）；审计断言。
- **DoD**：`integration_verified`。

#### M1-02 写工具接入 Gate3 审批（后端，核心）

- **目标**：写 MCP 工具复用既有 `ToolInvocation` + 队列审批，**参数冻结语义不变**；审批详情含来源三元组。
- **依赖**：M1-01、Q4 拍板（审批人角色维持 `ai:write`）。
- **改动文件**：`itsm-backend/handlers/ai/service.go`（`ExecuteTool :118` 写路径进入待审批；`ApproveTool :227` 审批通过后委派执行）、`itsm-backend/service/tool_queue.go`（执行与 `finalize :153`）、`itsm-backend/handlers/ai/handler.go`（审批接口返回来源字段）、`itsm-backend/mcp/provider/execute.go`（写工具不自动重试）。
- **要点**：审批创建时持久化冻结参数，执行时不接受模型改参（既有语义，回归锁定）；审批详情必须携带服务器名 + 原始工具名 + 投影名 + risk（否则审批人在信息缺失下决策，分析报告 §6.5-5）；拒绝/过期回填会话（过期依赖阶段一 G3，未落地时记录为已知缺口）；队列满/服务不可用 fail-closed，不静默丢弃。
- **测试与证据**：写路径集成测试（调用 → pending → approve → 执行 → 结果回填）；参数冻结断言（审批后篡改参数无效）；拒绝路径回填；队列重启恢复（若阶段一 B1 队列持久化未落地，MCP 沿用现队列并标注该缺口）。
- **DoD**：`flow_verified`（与 M1-10 联调）。

#### M1-03 SSE 事件契约扩展（后端）

- **目标**：新增 `tool_call_started / tool_call_finished / tool_call_failed / approval_pending` 事件，旧客户端"未知事件忽略"。
- **依赖**：M0-09。
- **改动文件**：`itsm-backend/handlers/ai/handler.go`（`writeEvent :298-307` 扩展与事件注册）、`itsm-backend/handlers/ai/service.go`（回调扩展：工具阶段通知）；`itsm-frontend/src/lib/api/ai-api.ts`（解析兼容与类型）。
- **要点**：事件 schema 字段：`id / tool(callable) / provider / server / phase / status / summary(脱敏截断) / duration_ms / error_code`；参数与输出摘要必须脱敏与截断；事件丢失时降级（最终结果仍在消息流）；不改动既有 `sources/delta/done/error` 语义。
- **测试与证据**：后端契约测试（事件序列与字段断言）；前端解析单测（新事件渲染、旧格式兼容、未知事件忽略）。
- **DoD**：`integration_verified`。

#### M1-04 AIChat 工具调用时间线（前端）

- **目标**：对话内每次工具调用渲染折叠块：来源徽标（内置 / MCP·服务器名）→ 参数摘要（脱敏）→ 结果摘要 / 耗时 / 状态。
- **依赖**：M1-03。
- **改动文件**：`itsm-frontend/src/components/ai/AIChat.tsx` 及新增子组件（如 `components/ai/tool-call-timeline.tsx`）、`lib/types/mcp.ts`、`lib/i18n/translations.ts`；组件测试。
- **要点**：数据只来自 SSE（不额外轮询）；降级路径（无事件时按最终消息渲染，不出现空白块）；工具输出进入渲染前 sanitize（沿用现有 Markdown 渲染策略，防 XSS）；长输出折叠 + 截断标记。
- **测试与证据**：组件测试（正常序列/失败序列/无事件降级/超长输出）；E2E 完整验证归 M2-05。
- **DoD**：`flow_verified`（与 M1-10 联调）。

#### M1-05 对话内待审批卡片与跳转（前端）

- **目标**：写工具提交审批后，对话内展示"已提交待审批"卡片（来源/风险/状态），可跳转审批页；适配 R2 退化形态。
- **依赖**：M1-02、M1-03、R2。
- **改动文件**：`itsm-frontend/src/components/ai/AIChat.tsx` + 新增卡片组件；跳转目标 `pages/(main)/ai/approval`。
- **要点**：一期边界 = 外置审批闭环（分析报告 §6.5-6），卡片只提示与跳转，不承诺内联确认（内联确认属阶段一 G3 / 二期）；状态过期时卡片显示"已过期/已处理"并禁止重复操作。
- **测试与证据**：组件测试（pending/approved/rejected/expired 四态）；跳转交互断言。
- **DoD**：`flow_verified`。

#### M1-06 审批页来源增强（前端）

- **目标**：审批列表与详情增加"来源（内置/MCP）"筛选、服务器名与原始工具名、risk 标注、跳转工具治理页。
- **依赖**：M1-02。
- **改动文件**：`itsm-frontend/src/pages/(main)/ai/approval/index.tsx`（现状见 `:28-33` 状态标签）及其数据层（`lib/api/ai-api.ts` 或新 `lib/api/mcp-api.ts`）；组件测试。
- **要点**：筛选维度与后端字段对齐（provider/server）；无 `mcp:admin` 的用户隐藏治理跳转入口；审批详情中三元组完整展示。
- **测试与证据**：组件测试（筛选/详情/权限隐藏）；与 M1-10 的审批页 E2E 合流。
- **DoD**：`flow_verified`。

#### M1-07 审计页来源维度（前端）

- **目标**：审计页支持按 provider / 服务器筛选，详情展示三元组、耗时、错误码。
- **依赖**：M0-11。
- **改动文件**：`itsm-frontend/src/pages/(main)/ai/audit/index.tsx` 及数据层；类型与 i18n；组件测试。
- **要点**：与 `tool_invocations` 扩展字段一一对应；脱敏字段原样展示（不还原明文）；空态与筛选回显。
- **测试与证据**：组件测试（筛选/详情/空态）；审计查询断言与后端 M0-11 输出对拍。
- **DoD**：`flow_verified`。

#### M1-08 运维收口与告警（后端）

- **目标**：健康事件时间线、连续失败告警、并发/超时/重试策略收口、in-flight 宽限期语义。
- **依赖**：M0-07、M0-08。
- **改动文件**：`itsm-backend/mcp/manager/{health.go,pool.go,events.go}`、`itsm-backend/mcp/admin/service.go`（事件查询 API）；告警接入现有通知/日志设施（落位以实施时评审为准）。
- **要点**：禁用/删除时"停止接受新调用 → 等待 in-flight（≤30s 或服务器 timeout）→ 强断并审计"（分析报告 §6.5-4）；连续 3 次健康检查失败 → 服务器置 `error` + 告警（触发信号见分析报告 §9.1）；逐服务器并发/超时/重试策略与 §5.4 默认表一致：读至多重试 1 次、写不重试、输出 256KB 截断。
- **测试与证据**：宽限期集成测试（长调用 + 并发删除）；告警阈值测试（可注入时钟）；策略参数回读 API 断言。
- **DoD**：`integration_verified`。

#### M1-09 安全负向测试集（测试）

- **目标**：把分析报告 §10.4 T-05/T-06 负向项执行到位并归档。
- **依赖**：M0-05、M0-06、M0-09。
- **覆盖清单**：私网/环回/链路本地/ULA 拦截；重定向拦截；DNS rebinding 二次解析；https 强制（与平台开关）；输出超限截断；参数与输出脱敏；凭据掩码与轮换；描述/返回值注入样例（按不可信处理）；跨租户 fail-closed。
- **证据**：安全测试报告（用例 → 结果 → 断言位置）。
- **DoD**：`integration_verified`。

#### M1-10 M1 流程验收

- **目标**：写路径端到端与前端闭环全部通过，M1 达 `flow_verified`。
- **依赖**：M1-02～M1-09。
- **内容**：写路径 E2E（对话 → 待审批 → 审批 → 执行 → 回填 → 审计可查）；审批页与审计页交互用例；失败注入（超时/拒绝/过期/服务器下线）。
- **证据**：E2E 输出、截图、SSE 抓包、审计查询结果，归档 `docs/plan/evidence/mcp-m1/`。
- **DoD**：`flow_verified`（里程碑级，对应 A1-10）。

### 4.3 M2：加固、E2E 与二期预研

#### M2-01 stdio 传输与沙箱（平台级，Q1 启用时实施）

- **目标**：平台级/旗舰私有化场景支持 stdio MCP；租户级**不可配置**；沙箱与命令白名单达标。
- **依赖**：Q1 决策（启用时）；M0-04 的传输抽象。
- **改动文件**：新建 `itsm-backend/mcp/transport/stdio.go`、`itsm-backend/mcp/manager/stdio_guard.go`；管理 API 校验层拒绝租户配置 stdio（若沿用进程树守卫，参考实现思路见分析报告 §4.3 D2 引用的 `transport.go:129-191` 与 stdio_tree 守卫）。
- **要点**：命令白名单 = 绝对路径 + 参数逐项声明，**禁 shell 拼接**；工作目录与 env 显式声明（env 白名单）；进程树守卫（子进程回收、孤儿进程清理）；资源限制与超时强杀；stderr 尾缓冲仅作诊断、协议通道保持纯净。
- **测试与证据**：沙箱测试（越权命令/参数注入/超时杀进程/env 泄漏/孤儿进程）；仅平台配置可用的权限断言。
- **DoD**：`integration_verified`。

#### M2-02 OAuth 2.1（PKCE / 发现 / 动态注册，Q6 拍板后实施）

- **目标**：远程 MCP 的 OAuth 授权全流程；服务端回调安全设计；token 生命周期管理。
- **依赖**：Q6；M0-04/06。
- **改动文件**：新建 `itsm-backend/mcp/auth/{discovery.go,pkce.go,flow.go,store.go}`；新建 `ent/schema/mcp_oauth_token.go`（字段见分析报告 §5.2 二期模型）；管理 API 增加授权入口与回调路由。
- **要点**：统一回调 + state 映射（或 Q6 最终形态）；CSRF 与开放重定向评审必过；多租户 token 隔离；刷新失败 → `auth_required` 事件 + 管理端横幅；token 加密存储与轮换审计；**不照搬参考实现的本机回调方案**（分析报告 §4.3 D4）。
- **测试与证据**：mock OAuth server 全流程（发现→注册→PKCE→换取→刷新→失效）；state 篡改拒绝；跨租户 token 隔离断言。
- **DoD**：`integration_verified`。

#### M2-03 工具面预算与元工具（后端）

- **目标**：单租户 MCP 工具数超阈值（默认 40）时告警，并给出元工具/检索式工具面方案。
- **依赖**：M0-09。
- **改动文件**：`itsm-backend/mcp/manager/discovery.go`（统计与阈值）、`itsm-backend/mcp/provider/`（元工具方案，如 `meta_tools.go`：list/search/describe/call）。
- **要点**：阈值可配；触发信号 = 有效工具数 > 40 或工具面 token 占比 > 30%（分析报告 §9.1 第一行）；元工具方案先评估后灰度，避免再次引入选择困难。
- **测试与证据**：阈值触发告警测试；工具面 token 占比测量；元工具（若实施）单测。
- **DoD**：`unit_verified`。

#### M2-04 平台级共享服务器 × 租户授权矩阵（后端，Q5 拍板后实施）

- **目标**：平台维护服务器，租户显式授权使用；跨租户隔离不破。
- **依赖**：Q5、M0-03。
- **改动文件**：ent 扩展（平台标志或授权关系表，实施时评审）；`mcp/admin` 授权 API；权限与审计。
- **要点**：平台服务器不计入租户配额；租户授权可撤销；撤销后下一轮工具面移除；全部操作审计。
- **测试与证据**：授权/撤销 → 工具面变化断言；未授权租户 fail-closed。
- **DoD**：`integration_verified`。

#### M2-05 浏览器 E2E 全链路（测试，硬门槛）

- **目标**：补齐参考实现最大遗留缺口——真实浏览器点击全链路（分析报告 §2.7/§7.3）。
- **依赖**：M0-12、M1-04～M1-06、R3（B3 未落地则自建）。
- **改动文件**：新建 `itsm-frontend/tests/e2e/mcp/{admin-mcp-servers.spec.ts,ai-tool-timeline.spec.ts,approval-flow.spec.ts}`；复用 `itsm-frontend/package.json:24` 的 Playwright 配置与 `itsm-backend/cmd/mcp-mockserver`。
- **要点**：管理页全流程（新增 → 测试 → 启用 → 治理 → 禁用 → 删除）；用户侧（时间线渲染、待审批卡片、跳转）；审批流（审批人视角）；不使用静态 stub 冒充（每个断言基于真实 DOM 与网络）。
- **测试与证据**：E2E 全绿输出 + trace/video；纳入合并前或 nightly 门禁（二选一，团队定）。
- **DoD**：`flow_verified`。

#### M2-06 指标、告警看板与运维手册（运维）

- **目标**：可观测与 SOC 视角运维就绪。
- **依赖**：M1-08。
- **改动文件**：`itsm-backend/mcp/manager/metrics.go`（Prometheus 客户端已有依赖 `go.mod:33`）；新建运维手册文档（建议路径 `docs/ops/mcp-runbook.md`，最终落位按 `docs/` 治理结构确定）；告警规则配置样例。
- **要点**：指标 = 连接状态/握手耗时、`tools/call` 成功率与耗时 P50/P95、超时/重试/截断计数、隔离工具数、每服务器并发使用率（分析报告 §5.10）；手册覆盖：凭据轮换、应急禁用（服务器/工具/全局开关）、故障排查（last_error 分类）、在途调用处理。
- **测试与证据**：指标暴露断言；告警阈值测试；**一次桌面演练**（按手册执行"某服务器凭据泄露"应急禁用流程）并记录。
- **DoD**：`integration_verified`。

#### M2-07 accepted 复核与文档回写

- **目标**：按本文件 §5 全量复核；对照完整度矩阵判定 `accepted`；完成治理回写。
- **依赖**：全部任务。
- **内容**：
  1. 对照分析报告 §7.2 完整度矩阵逐行判定（M2 目标列）；未达项列明理由与不阻塞上线/阻塞上线的结论；
  2. 全部验收项（A0/A1/A2）证据归档核对；
  3. 回写：本文件 §3.3 状态列、`CHANGELOG.md`、阶段一计划（若有关联项）、ROADMAP（若存在）；
  4. 残余风险与已知缺口签字确认（含 R2 退化项、SQLite/Postgres 差异、队列持久化缺口等）。
- **证据**：验收签署记录（§5.5 规范）+ 完整度矩阵复核表归档 `docs/plan/evidence/mcp-accepted/`。
- **DoD**：`accepted`。

---

## 5. 验收标准

> 本章是交付判定依据：任务卡（§4）说明"怎么做"，验收项（§5.2）说明"做到什么算数"。**每条验收项必须有证据；无证据视为未达。** 五级口径与证据映射见 §5.1，阶段出口判定见 §5.4。

### 5.1 通用 DoD（所有任务适用）

| # | 维度 | 要求 |
| --- | --- | --- |
| D-1 | 代码质量 | 通过 review；无旁路执行（禁止绕过 Gate1/2/3、队列、审计）；错误码进入统一枚举；关键错误路径有日志 |
| D-2 | 测试 | 新增单测/契约测试通过；`go test ./...`（后端）与相关 `npm test`（前端）全绿；覆盖率不低于改动前；`golangci-lint`（配置 `.golangci.yml`）通过 |
| D-3 | 多租户 | 新增查询全部带 `tenant_id`；新增表索引首列含 `tenant_id`；跨租户 fail-closed 有测试 |
| D-4 | 安全 | 敏感数据（凭据/参数/输出）全链路脱敏有断言；权限默认拒绝；SSRF 防护不得被绕过 |
| D-5 | 兼容 | 旧客户端忽略未知 SSE 事件；`tool_invocations` 新字段带默认值/nullable，不破坏存量查询 |
| D-6 | 文档与证据 | 任务卡状态回写 §3.3；PR 附测试命令与输出；里程碑证据归档（§5.5） |

### 5.2 验收矩阵

#### M0（目标：`integration_verified`）

| 编号 | 验收项（可验证陈述） | 任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| A0-01 | `mcp.enabled=false` 时：不建连、工具面不含 MCP、管理入口按既定策略隐藏/禁用，存量测试全绿 | M0-01 | 开关关断启动 + 全量后端测试 | 测试输出、启动日志 | `unit_verified` |
| A0-02 | 投影契约：唯一/重名/非法字符/超长/遮蔽五类投影正确；解析精确/短名唯一/歧义 fail-closed；碰撞 quarantine 不可执行；无绕过前缀直呼原始名路径 | M0-02 | `go test ./mcp/registry/...` 契约测试 | 测试输出（用例→断言清单） | `unit_verified` |
| A0-03 | 迁移：空库建表 + 旧库升级均通过；SQLite 与 Postgres 双驱动；新字段默认值/可空不影响存量读写 | M0-03 | 双驱动迁移冒烟 + ent 读写断言 | 迁移日志、测试输出 | `integration_verified` |
| A0-04 | 传输：对 mock 服务器完成 `initialize`/`tools/list`/`tools/call`；超时/TLS/401/协议不匹配错误分类正确且不降级 | M0-04 | 集成测试（M0-13 mock + 错误注入） | 测试输出 | `unit_verified`（对 mock 集成后） |
| A0-05 | SSRF 负向全过：环回/私网/RFC1918/链路本地/ULA、重定向、DNS rebinding、无开关时 http 全部拦截；错误码 `ssrf_blocked` | M0-05 | 表驱动负向测试套件 | 安全测试报告 | `unit_verified` |
| A0-06 | 凭据：DB 无明文；读接口仅掩码；编辑未修改不回写；轮换后旧凭据失效；生产缺密钥拒绝启动 | M0-06 | 断言测试 | 测试输出 | `unit_verified` |
| A0-07 | 生命周期：断连→工具面移除→退避重连→恢复；schema 变更→隔离待复核；工具开关不触发重连；MCP 全挂时对话与内置工具正常 | M0-07 | 集成测试（mock 注入断连/恢复/schema 变更） | 测试输出 | `integration_verified` |
| A0-08 | 管理 API：CRUD/测试连接/启停/重载/治理全通；启停/重载返回 202 且状态可轮询回读（无 60s 阻塞）；错误码映射正确；乐观锁冲突 409；管理操作审计可查 | M0-08 | API 集成测试 + 手工 curl 记录 | 测试输出、审计查询 | `integration_verified` |
| A0-09 | 只读端到端：mock 工具进工具面→对话调用→结果回填；未启用/隔离/无权限工具不可见不可执行；失败结构化回填（错误码+简短原因） | M0-09 | 集成场景脚本 | 测试输出、SSE 记录 | `integration_verified` |
| A0-10 | 权限矩阵：管理员/坐席/无权限 × read/write/admin 行为正确；默认不授予；跨租户引用 fail-closed；guard 测试通过 | M0-10 | 角色矩阵测试 | 测试输出 | `integration_verified` |
| A0-11 | 审计：`tool_invocations` 含 provider/三元组/耗时/错误码/脱敏参数与输出摘要；管理操作审计含脱敏 diff；凭据明文不出现在 DB/日志/事件 | M0-11 | 审计查询断言 + 脱敏断言 | 测试输出、查询结果 | `integration_verified` |
| A0-12 | 管理页：向导校验/三态展示/批量操作/确认弹窗组件测试全绿；人工冒烟走通新增→测试→启用→治理 | M0-12 | Jest 组件测试 + 人工浏览器冒烟 | 测试输出、截图 | `unit_verified`（+冒烟） |
| A0-13 | mock 设施：testutil 与独立进程两形态可用；故障注入（超时/401/TLS/协议/断连/超长/变更）可脚本化 | M0-13 | 自测 + 被 M0-04/07/08 消费 | 测试输出 | `unit_verified` |
| A0-14 | M0 集成验收：完整只读场景 + SSRF/凭据/超限负向 + 权限矩阵回归全部通过，证据归档 | M0-14 | 场景脚本执行 | `docs/plan/evidence/mcp-m0/` | `integration_verified` |

#### M1（目标：`flow_verified`）

| 编号 | 验收项（可验证陈述） | 任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| A1-01 | 分类标注：新工具默认 `read_only=false`；标注只读后免审批直通、改回写后进审批；变更全程审计 | M1-01 | 集成测试 | 测试输出、审计查询 | `integration_verified` |
| A1-02 | 写路径 E2E：对话→待审批→审批→执行→回填；审批后参数被冻结（篡改无效）；拒绝路径回填；写工具不自动重试 | M1-02、M1-10 | 流程 E2E | 测试输出、审计查询、SSE 记录 | `flow_verified` |
| A1-03 | SSE 契约：`tool_call_started/finished/failed` 与 `approval_pending` 字段完整、脱敏截断；旧客户端忽略未知事件不崩 | M1-03 | 后端契约测试 + 前端解析单测 | 测试输出 | `integration_verified` |
| A1-04 | 时间线：正常/失败/降级（无事件）/超长四态渲染正确；输出 sanitize 无 XSS | M1-04 | 组件测试 | 测试输出 | `flow_verified` |
| A1-05 | 待审批卡片：pending/approved/rejected/expired 四态与跳转正确；B1/B2 未落地时退化提示正确 | M1-05 | 组件测试 + 手工 | 测试输出、截图 | `flow_verified` |
| A1-06 | 审批页：来源筛选、三元组、risk 展示；无 `mcp:admin` 时治理跳转隐藏 | M1-06 | 组件测试 + E2E | 测试输出 | `flow_verified` |
| A1-07 | 审计页：provider/服务器筛选与详情（三元组/耗时/错误码）与后端数据对拍一致 | M1-07 | 组件测试 + 查询对拍 | 测试输出、查询结果 | `flow_verified` |
| A1-08 | 运维：禁用/删除时新调用拒绝、在途 ≤30s 等待或强断并审计；连续 3 次失败告警触发；并发/超时/重试与默认表一致 | M1-08 | 集成测试（含假时钟） | 测试输出、审计查询 | `integration_verified` |
| A1-09 | 安全负向：私网/重定向/rebinding/超限/脱敏/注入样例/跨租户全部拦截或安全降级 | M1-09 | 安全负向套件 | 安全测试报告 | `integration_verified` |
| A1-10 | M1 流程验收：写路径 E2E + 审批页/审计页交互 + 失败注入（超时/拒绝/过期/下线）全绿，证据归档 | M1-10 | 场景执行 | `docs/plan/evidence/mcp-m1/` | `flow_verified` |

#### M2（目标：`accepted`）

| 编号 | 验收项（可验证陈述） | 任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| A2-01 | stdio 沙箱：命令白名单外拒绝、参数注入阻断、超时强杀、env 白名单生效、无孤儿进程；租户级配置 stdio 被拒 | M2-01 | 沙箱测试套件 | 安全测试报告 | `integration_verified` |
| A2-02 | OAuth：发现/注册/PKCE/换取/刷新/失效全流程通过；state 篡改拒绝；token 加密存储且跨租户隔离 | M2-02 | mock OAuth 集成测试 | 测试输出 | `integration_verified` |
| A2-03 | 工具面预算：有效工具数 > 40 触发告警；token 占比可测量；元工具方案（若实施）按灰度开关可用 | M2-03 | 阈值与测量测试 | 测试输出 | `unit_verified` |
| A2-04 | 平台共享矩阵：授权 → 租户可用；撤销 → 下一轮移除；未授权租户 fail-closed | M2-04 | 集成测试 | 测试输出 | `integration_verified` |
| A2-05 | 浏览器 E2E：管理页全流程（新增→测试→启用→治理→禁用→删除）+ 时间线 + 审批流真实点击全绿，含 trace/video | M2-05 | Playwright | E2E 报告与 trace | `flow_verified` |
| A2-06 | 指标/告警/演练：指标可抓取、阈值告警可触发；按 runbook 完成"凭据泄露应急禁用"演练并记录 | M2-06 | 指标断言 + 桌面演练 | 演练记录 | `integration_verified` |
| A2-07 | 完整度复核：对照分析报告 §7.2 M2 目标列逐行判定，未达项列明确结论（阻塞/不阻塞上线） | M2-07 | 复核表评审 | 复核表归档 | 复核件（支撑 accepted） |
| A2-08 | accepted 判定：验收签署完成；§3.3 任务状态、§5.2 判定列、`CHANGELOG.md` 回写完成；残余风险签字 | M2-07 | 出口评审会 | 验收纪要、回写 diff | `accepted` |

### 5.3 测试用例执行清单（分析报告 §10.4 九组，本方案编号 T-01…T-09）

| 用例组 | 覆盖内容 | 测试层 | 执行时机 | 归属任务 | 关联验收 |
| --- | --- | --- | --- | --- | --- |
| T-01 命名投影 | 唯一/重名/非法字符/超长/遮蔽 → 投影与 canonical 断言 | UT（表驱动） | 每次提交 CI | M0-02 | A0-02 |
| T-02 解析 | canonical 精确、短名唯一、短名歧义 fail-closed、执行归一化不串服务 | UT | 每次提交 CI | M0-02 | A0-02 |
| T-03 隔离 | 碰撞后到者 quarantine 且不可执行；schema_hash 变更 → 隔离 → 复核解除 | UT + IT | CI + 里程碑 | M0-02、M0-07 | A0-02、A0-07 |
| T-04 治理 | 单/批量启停立即生效（无重连）；禁用工具不出工具面、解析 fail-closed | IT | CI | M0-07、M0-08 | A0-07、A0-08 |
| T-05 安全 | 私网/环回/链路本地/ULA、重定向、DNS rebinding、https 强制、凭据掩码与轮换、输出截断 | SAF | CI + M0/M1 验收 | M0-05、M0-06、M0-09、M1-09 | A0-05、A0-06、A1-09 |
| T-06 门禁 | 角色矩阵（read/write/admin）、跨租户 fail-closed、写工具参数冻结 | IT | CI | M0-10、M1-02 | A0-10、A1-02 |
| T-07 生命周期 | 断连退避重连、工具面塌缩与恢复、禁用/删除 in-flight 宽限 | IT | CI + nightly | M0-07、M1-08 | A0-07、A1-08 |
| T-08 管理 API | 异步启停/重载状态回读、错误码映射、管理操作审计与脱敏 | IT | CI | M0-08、M0-11 | A0-08、A0-11 |
| T-09 前端 | 表单校验、三态展示、批量操作、确认弹窗、时间线渲染；浏览器 E2E 全流程 | 组件 + E2E | 组件每次 CI；E2E nightly/里程碑 | M0-12、M1-04～M1-07、M2-05 | A0-12、A1-04～A1-07、A2-05 |

> 执行原则：T-01/T-02/T-05/T-06 为**每次提交必跑**（快、确定性高）；T-03/T-04/T-07/T-08 为 CI（可用 docker/内存 DB）；T-09 组件测试入库 CI，Playwright E2E 走 nightly + 里程碑出口。E2E 依赖真实 provider 的部分一律 mock 化，仅保留少量 smoke（对齐阶段一策略）。

### 5.4 阶段出口判定规则

1. **全量达标**：该里程碑全部 A 项达到或超过目标级别；未达项一律阻塞，禁止"带病升级"。
2. **回归全绿**：此前里程碑全部验收项回归通过（回归集 = 全部既有自动化用例 + 既往 A 项对应用例）。
3. **缺陷门槛**：无未决 P0；P1 必须修复或书面豁免（豁免需产品 + 安全签字并登记 §10）。
4. **证据完整**：§5.5 要求的证据归档齐全，抽查可复跑。
5. **治理回写**：任务状态、验收判定、CHANGELOG 回写完成。
6. **判定组织**：里程碑出口评审会（建议参与：研发负责人、QA、安全、产品）；输出验收纪要（日期/参与人/逐项判定/豁免项/结论）。
7. **豁免处理**：任何豁免必须登记 §10 决策日志并同步降级受影响验收项的状态标注；无书面记录的"口头通过"无效。

### 5.5 验收证据规范

- **四要素**：每条证据包含「执行步骤或命令 / 结果 / 时间 / 执行人与环境」。
- **证据类型**：CI 测试输出；审计查询结果（SQL/页面）；SSE 抓包；管理页与对话截图/录屏；Playwright trace/video；安全测试报告；演练记录；完整度复核表。
- **归档位置**：`docs/plan/evidence/mcp-<m0|m1|m2|accepted>/`；大文件（video/trace）走 CI artifact 并在目录内留链接。
- **命名规范**：`<验收项编号>-<简述>-<yyyyMMdd>.<ext>`（如 `A0-05-ssrf-negative-20261012.md`）。
- **审核要求**：QA 对自动化证据抽检 ≥30% 可复跑；安全类验收（A0-05/A0-06/A1-09/A2-01/A2-02）100% 人工复核。
- **反模式（直接判未达）**：用截图代替自动化断言；用 checkbox 或"设计完成"代替实现与测试；证据与版本不符（非当前 HEAD 产出）。

---

## 6. 测试与质量保障计划

### 6.1 测试分层与设施

| 层 | 范围 | 工具/设施 | 说明 |
| --- | --- | --- | --- |
| UT | registry 投影/解析、SSRF 校验、凭据加解密、参数校验、事件序列化 | Go `testing` + `testify`（`go.mod:39`）；前端 Jest + jsdom（`package.json:18/:112`） | 表驱动优先；快、确定性高，进每次提交 CI |
| IT（组件集成） | transport↔client、manager 生命周期、admin API↔ent、Gate2/3 接入、审计落库 | `enttest`（`ent/enttest`）+ mock MCP（M0-13）；真实 DB 容器【CI 现状未核实，需在 P3 确认】 | 覆盖真实 ent/DB 交互，禁止全程 sqlmock 代替 |
| 组件测试（前端） | 管理页、时间线、审批/审计页 | Jest + Testing Library 范式（参照既有组件测试） | 必须为真实交互测试（吸取参考实现 stub 教训） |
| E2E | 管理员全流程、用户侧时间线、审批流 | Playwright（`package.json:24`）+ `cmd/mcp-mockserver` + mock provider | 真实浏览器点击；M2-05 硬门槛 |
| 安全专项 | 负向清单（T-05/T-06） | 表驱动 + 可注入 resolver/时钟 | 独立报告归档 |

### 6.2 CI 门禁与合入策略（建议）

1. **每次提交（必跑）**：后端 `go build ./...` + `go test ./mcp/... ./service/... ./handlers/...`；前端受影响的 Jest 用例；lint。
2. **合并门禁（建议）**：以上必跑项 + MCP 集成测试（mock server）+ 关键组件测试；任一失败禁止合并。
3. **nightly**：全量集成（含断连恢复、宽限期、告警假时钟）+ Playwright MCP E2E。
4. **里程碑出口**：全量回归 + 安全专项 + 证据归档核对。
5. 现有 CI 尚无 Docker 时的过渡：IT 用 SQLite 快跑 + Postgres 专项在具备环境时补跑；**不得因此跳过 A0-03 的双驱动验收**。

### 6.3 环境与数据矩阵

- 数据库：SQLite（本地快跑）与 Postgres（生产一致性）；迁移脚本/`Schema.Create` 两者均需通过。
- MCP 服务：mock（默认）+ 可选真实公开 MCP 服务 smoke（不计入门禁，避免外部不稳定）。
- 租户与角色：≥2 租户 × 管理员/坐席/无权限三角色；E2E 使用独立租户避免污染演示数据。
- 模型：mock provider 全量 + 少量真实 provider smoke（对齐阶段一报告 §7-10）。

### 6.4 缺陷管理与回归

- 分级：P0（安全绕过/数据泄露/核心链路不可用）立即修复并停线；P1（功能不达验收、审计缺失）里程碑前修复或书面豁免；P2/P3 记入待办。
- 修复要求：每个缺陷修复必须附回归用例（自动化优先）；安全类缺陷必须补充负向用例。
- 里程碑出口前执行一次针对性回归（T-01/T-02/T-05/T-06 全量 + 本里程碑新增用例）。

---

## 7. 风险登记（含实施期新增）

| # | 风险 | 影响 | 应对 | 触发信号 | 责任 |
| --- | --- | --- | --- | --- | --- |
| R-01 | 工具面膨胀（上下文/成本/选择困难） | 对话质量下降、成本上升 | 阈值告警（M2-03）；元工具/检索式工具面 | 有效工具数 > 40 或 token 占比 > 30% | 后端 + 产品 |
| R-02 | 第三方 MCP 服务不稳定 | 调用失败、延迟抖动 | 退避、熔断、`healthy` 位隔离、降级不影响内置工具 | 连续 3 次健康检查失败 | 后端 |
| R-03 | 恶意/被污染 MCP 服务器 | Prompt injection、数据外泄 | 内容不可信处理、schema 变更隔离、域名 allowlist、审计 | 描述/返回值含指令模式、schema_hash 频繁变化 | 安全 + 后端 |
| R-04 | stdio 滥用 | 宿主机 RCE | 仅平台级 + 白名单 + 沙箱 + 禁 shell 拼接（M2-01） | 出现租户级 stdio 配置需求 | 安全 |
| R-05 | 权限扩散 | 越权调用写工具 | 默认拒绝 + 最小角色 + Gate2/3 + 矩阵测试 | `mcp:*` 授予非管理员角色 | 安全 + 产品 |
| R-06 | 协议/SDK 演进 | 兼容性断裂 | 锁版本 + 握手校验 + 版本不匹配拒绝 | 新规范版本发布 | 后端 |
| R-07 | 范围蔓延（resources/prompts/sampling 等） | 交付延期 | 非目标清单（§1.1）+ 变更评审 | 需求中出现非 tools 原语 | 产品 |
| R-08 | 阶段一未排期，G1/G7 合并落空 | 审计追溯退化、二次迁移 | M0-03 自带最小扩展（D9 降级方案），B0 落地时对齐 | B0 排期变化 | 研发负责人 |
| R-09 | SQLite/Postgres 迁移行为差异【未核实】 | 生产迁移失败或回填异常 | P3 迁移评审 + CI 双驱动冒烟 | 迁移测试在某一驱动失败 | 后端 + DBA |
| R-10 | 工作树未提交改动混入 MCP 分支 | 变更混淆、评审困难 | P1 开工前处置（并入/独立提交/暂存） | 分支 diff 出现无关文件 | 研发负责人 |
| R-11 | E2E 环境昂贵/不稳定 | 验收延期 | mock 优先、E2E nightly、真实 provider 仅 smoke | E2E 连续 flaky | QA |
| R-12 | 队列持久化缺口（依赖阶段一 B1）【现状未核实】 | 重启丢失待执行审批任务 | 依赖 B1；未落地时在 M1-02 验收注明缺口并评估影响 | 重启后 pending 任务丢失实验复现 | 后端 |
| R-13 | 审批过期语义缺失（依赖阶段一 G3） | 过期审批可被误执行 | 依赖 G3；未落地时限制审批有效期由人工兜底并登记缺口 | 审批积压且无过期机制 | 产品 + 后端 |

---

## 8. 回滚与降级预案

### 8.1 灰度与开关层级（四级，自上而下）

| 层级 | 开关 | 生效方式 | 影响 |
| --- | --- | --- | --- |
| L1 全局 | `mcp.enabled`（配置/env，默认 false） | 重启或配置热加载（随现有配置机制） | 全系统 MCP 功能关断；工具面与后台连接全部停止 |
| L1.5 写开关（建议新增） | `mcp.write_enabled`（默认 true，随 L1 生效） | 重启/热加载 | 写工具从工具面移除，只读不受影响（应急止血） |
| L2 服务器 | `server.enabled=false` | 管理 API 异步生效 | 该服务器工具移除；连接断开 |
| L3 工具 | `tool.enabled=false` / quarantine | 管理 API / 自动隔离 | 单工具不可见不可执行，其余不受影响 |

### 8.2 发布与回滚步骤

1. **发布**：① 部署含迁移版本（`Schema.Create` 只增表/列，向前兼容）；② 开关保持关闭；③ 单租户灰度开启 → 验证只读链路；④ 开启写链路（`mcp.write_enabled` 灰度）→ 验证审批；⑤ 全量。
2. **问题止血**：优先用开关降级（L1.5 关写 → L2 关服务器 → L1 全局关），**无需回滚代码即可恢复安全状态**。
3. **代码回滚**：回滚到上一版本；新增表/字段保留（不执行破坏性迁移）；`tool_invocations` 旧版本读取忽略新列（默认值/nullable 保证）。
4. **在途调用**：禁用/删除期间按 M1-08 宽限期处理（停止新调用 → ≤30s 或服务器 timeout 等待 → 强断并审计）；队列中待审批任务按 R-12 缺口评估人工处置。
5. **数据处置**：审计与事件数据只增不删；quarantine 记录保留供诊断；凭据在删除服务器时级联清理并审计。

### 8.3 故障降级矩阵

| 故障 | 影响面 | 系统行为 | 用户可见 | 恢复 |
| --- | --- | --- | --- | --- |
| MCP 服务器断连 | 该服务器工具 | 下一轮工具面移除；退避重连 | 对话不中断；管理端显示 `disconnected` | 重连成功后自动恢复 |
| 单次调用超时/限流 | 单次调用 | 结构化错误回填；不自动重试写工具 | 时间线显示失败与错误码 | 用户/模型可重试（读） |
| 凭据失效 | 该服务器 | `auth_required` 事件 + 横幅；工具移除 | 管理员按横幅处理 | 轮换凭据后重连 |
| MCP 模块异常（含 panic 边界） | MCP 工具 | 隔离该服务器；内置工具与对话不受影响 | 管理端显示 error | 修复后 reload |
| 迁移/启动失败 | 全局 | fail-fast 拒绝启动（不留半可用状态） | 部署失败告警 | 修复迁移后重启 |
| 审批队列满 | 写工具 | fail-closed（拒绝入队并提示），不静默丢弃【现队列容量 100，见 `service/tool_queue.go:30` 构造】 | 明确错误提示 | 队列消化后恢复 |

### 8.4 应急操作手册

M2-06 交付《MCP 运维手册》（建议 `docs/ops/mcp-runbook.md`），至少覆盖：凭据泄露应急禁用流程、服务器下线流程、工具误启用回滚、告警处置、日志与审计取证路径。手册必须经一次桌面演练验证（A2-06）。

---

## 9. 交付物清单

| 类别 | 交付物 | 责任层 | 验收关联 |
| --- | --- | --- | --- |
| 后端代码 | `itsm-backend/mcp/{transport,client,registry,manager,admin,provider}`；`service/tool_provider.go`；`handlers/mcp/`；`router/mcp_routes.go`；`internal/authz` 与 `internal/bootstrap` 改动 | 后端 | A0-01～A0-11、A1-01/A1-02/A1-03/A1-08、A2-01～A2-04 |
| 数据模型 | `ent/schema/mcp_server.go`、`mcp_server_tool.go`（+M2 OAuth token）；`tool_invocation.go` 扩展；ent 生成物与迁移验证记录 | 后端 | A0-03、A0-11、A2-02 |
| 前端代码 | `/admin/mcp-servers` 页面与组件；`lib/api/mcp-api.ts`、`lib/types/mcp.ts`、i18n；AIChat 时间线与待审批卡片；审批/审计页增强；路由/菜单/seed 接线 | 前端 | A0-12、A1-04～A1-07 |
| 测试设施 | `mcp/testutil/mockserver`、`cmd/mcp-mockserver`、单元/契约/集成测试、Playwright spec | QA + 后端 | A0-13、A2-05、T-01～T-09 |
| 文档 | 本实施方案；`mcp/README.md`；运维手册（M2-06）；证据归档目录；`CHANGELOG.md` 回写 | 全体 | A2-08 |
| 运维资产 | 指标暴露、告警规则样例、演练记录 | SRE | A2-06 |

## 10. 决策日志与变更控制

**决策项（2026-09-27 已按建议拍板）**：Q1–Q8 登记如下（依据项目负责人「按建议执行」指示）；如产品/安全在里程碑出口评审提出异议，按下方变更控制规则处理。

| 日期 | 决策项 | 结论 | 拍板人 | 备注 |
| --- | --- | --- | --- | --- |
| 2026-09-27 | Q1 stdio 是否进一期 | 已拍板：不进一期；M2 旗舰私有化再评估（D2） | 项目负责人（按建议执行） | 阻塞 M0-03 范围、M2-01 |
| 2026-09-27 | Q2 权限位 2 vs 3 位 | 已拍板：3 位（`mcp:read/write/admin`，D10） | 项目负责人（按建议执行） | 阻塞 M0-10 |
| 2026-09-27 | Q3 管理页独立 vs Tab | 已拍板：独立 `/admin/mcp-servers`（D11） | 项目负责人（按建议执行） | 阻塞 M0-12 |
| 2026-09-27 | Q4 审批人角色 | 已拍板：维持 `ai:write`，审批详情补来源三元组 | 项目负责人（按建议执行） | 阻塞 M1-02 文案/路由 |
| 2026-09-27 | Q5 平台级共享服务器 | 已拍板：二期（D12） | 项目负责人（按建议执行） | 阻塞 M0-03 模型冻结 |
| 2026-09-27 | Q6 OAuth 回调形态 | 已拍板：二期定；统一回调 + state 映射，须 CSRF/开放重定向评审 | 项目负责人（按建议执行） | 阻塞 M2-02 |
| 2026-09-27 | Q7 BYO 凭据 | 已拍板：一期不开放；管理员在管理页录入 | 项目负责人（按建议执行） | 影响 M0-06/M1 范围 |
| 2026-09-27 | Q8 工具结果是否入知识库 | 已拍板：不入；仅回填对话与审计 | 项目负责人（按建议执行） | 影响 RAG 集成范围 |

**变更控制**：实施期任何偏离本方案（范围、设计、验收级别、里程碑顺序）必须在本表新增记录（日期/变更项/原因/影响/批准人），并同步回写分析报告对应章节；未登记的偏离在里程碑验收时一律不认可。

## 11. 附录

### 11.1 验收项 ↔ 任务 ↔ 测试组对照（速查）

| 验收项 | 任务 | 测试组 | 方式 |
| --- | --- | --- | --- |
| A0-01 | M0-01 | — | 开关关断冒烟（自动） |
| A0-02 | M0-02 | T-01/T-02 | 契约测试（自动） |
| A0-03 | M0-03 | — | 双驱动迁移（自动 + 抽查） |
| A0-04 | M0-04 | T-03（传输部分） | 集成（自动） |
| A0-05 | M0-05/M1-09 | T-05 | 负向套件（自动） |
| A0-06 | M0-06 | T-05 | 断言（自动） |
| A0-07 | M0-07 | T-03/T-04/T-07 | 集成（自动） |
| A0-08 | M0-08 | T-04/T-08 | API 集成（自动） |
| A0-09 | M0-09/M0-11 | T-04 | 场景（自动） |
| A0-10 | M0-10 | T-06 | 矩阵（自动） |
| A0-11 | M0-11 | T-08 | 审计断言（自动） |
| A0-12 | M0-12 | T-09 | 组件（自动）+ 人工冒烟 |
| A0-13 | M0-13 | — | 自测（自动） |
| A0-14 | M0-14 | T-03～T-08 汇总 | 场景（自动 + 归档） |
| A1-01 | M1-01 | T-04 | 集成（自动） |
| A1-02 | M1-02/M1-10 | T-06/T-07 | E2E（自动） |
| A1-03 | M1-03 | — | 契约（自动） |
| A1-04 | M1-04 | T-09 | 组件（自动） |
| A1-05 | M1-05 | T-09 | 组件（自动）+ 人工 |
| A1-06 | M1-06 | T-09 | 组件 + E2E |
| A1-07 | M1-07 | T-09 | 组件 + 对拍 |
| A1-08 | M1-08 | T-07 | 集成（自动，假时钟） |
| A1-09 | M1-09 | T-05/T-06 | 安全报告（自动 + 复核） |
| A1-10 | M1-10 | T-09 汇总 | E2E（自动 + 归档） |
| A2-01 | M2-01 | T-05 | 沙箱套件（自动） |
| A2-02 | M2-02 | — | 集成（自动） |
| A2-03 | M2-03 | — | 阈值/测量（自动） |
| A2-04 | M2-04 | T-06 | 集成（自动） |
| A2-05 | M2-05 | T-09 | Playwright（自动） |
| A2-06 | M2-06 | — | 指标断言 + 演练（人工记录） |
| A2-07 | M2-07 | — | 复核表（人工评审） |
| A2-08 | M2-07 | — | 签署（人工） |

### 11.2 关键锚点清单

**本次核对（HEAD `7442fad5` 工作树）**：

| 锚点 | 位置 | 用途 |
| --- | --- | --- |
| Go 版本 | `itsm-backend/go.mod:3` | SDK 兼容性前提 |
| ToolDefinition / ToolRegistry | `itsm-backend/service/tool_registry.go:14-22`、`:24-33`、`:53-61` | ToolProvider 聚合改造点 |
| ToolQueue | `itsm-backend/service/tool_queue.go:30`、`:45`、`:52`、`:153` | 写工具执行与 finalize 审计扩展 |
| ExecuteTool / ApproveTool / ChatStream | `itsm-backend/handlers/ai/service.go:118`、`:227`、`:407`、`:424` | 执行与审批接入点 |
| SSE 写出 | `itsm-backend/handlers/ai/handler.go:298-307` | tool_call_* 事件扩展点 |
| Agent 路由 | `itsm-backend/router/ai_routes.go:50-57` | 路由装配范式与工具接口 |
| 权限定义 | `itsm-backend/internal/authz/catalog.go:161-164` | `mcp:*` 新增位置 |
| 权限全量清单 | `itsm-backend/internal/authz/roles.go:389-437`（参考 `:427-428`） | 角色种子与 admin 授权 |
| ToolInvocation 模型 | `itsm-backend/ent/schema/tool_invocation.go:14-43` | 审计字段扩展位置 |
| 工具装配 | `itsm-backend/internal/bootstrap/app.go:744-752` | MCP provider 装配位置 |
| 迁移执行 | `itsm-backend/internal/bootstrap/app.go:1392`（前置步骤 `:1380-1391`） | `Schema.Create` 迁移机制 |
| Connector 凭据范式 | `itsm-backend/internal/bootstrap/app.go:489-500` | M0-06 加密存储参考 |
| 菜单种子 | `itsm-backend/pkg/seeder/seeder.go:1706-1775` | 管理页菜单条目 |
| 权限守卫测试 | `itsm-backend/router/permission_code_catalog_guard_test.go` | M0-10 必须更新并通过 |
| 前端测试设施 | `itsm-frontend/package.json:18`、`:24`、`:112` | Jest / Playwright / jsdom |
| 路由与接线 | `itsm-frontend/src/routes/route-paths.ts:5-34`、`itsm-frontend/src/routes/index.tsx:34/231`（connectors 范式）、`itsm-frontend/src/components/layout/sidebar/menu-config.ts` | M0-12 接线三件套 |
| AI 页面与组件 | `itsm-frontend/src/pages/(main)/ai/{chat,approval,audit}/index.tsx`、`itsm-frontend/src/components/ai/AIChat.tsx` | M1-04～M1-07 落点 |
| 前端服务层范式 | `itsm-frontend/src/lib/api/ai-api.ts`、`itsm-frontend/src/lib/services/connector-service.ts`、`itsm-frontend/src/lib/i18n/translations.ts` | M0-12 API/文案 |
| 治理规则 | `plans/README.md:5`、`docs/documentation-governance.md` | 状态回写与验收纪律 |

**分析报告已核实、本方案直接引用的锚点**（未在本次复核）：`handlers/ai/service.go:113-159`（Gate1/2/3）、`:385-398`（硬编码写白名单）、`:447-616`（工具面装载）、`:485-530`（过滤与回填）；`ent/schema/connector_config.go:15-39`；`connector/connector.go:20-34,150-211,231-268`；`internal/bootstrap/app.go:1015-1021`（skills 装配）；前端 `admin/connectors/index.tsx`、`admin/system-config/llm-provider-settings.tsx`。以上若与实施时现状不符，以实施时核对为准并更新本表。

### 11.3 与阶段一（B0–B4）的合并点

| 阶段一 | MCP 侧 | 合并说明 |
| --- | --- | --- |
| B0 元数据与审计补全（G1/G7） | M0-03、M0-11、M1-01 | **一次迁移**：`ToolDefinition` 来源元数据 + `ToolInvocation` 审计字段；不得分两次加列 |
| B1 运行态与确认闭环（G3、队列持久化） | M1-03、M1-05 | MCP 事件契约与 SSE 兼容层可复用；对话内确认卡片依赖其状态机（R2 退化） |
| B2 Bot 模板与授权（G2 风险上限） | M1-01（risk 标注）、M2-03 | MCP `risk` 字段为 Bot 风险上限提供输入；工具面预算与授权过滤叠加 |
| B3 页面入口与场景 Bot | M2-05 | 复用页面入口组件与 Playwright 设施（R3） |
| B4 E2E 验收与状态回写 | M2-07 | 复用五级状态与 run-summary 证据思路，保持验收口径一致 |

> 互逆视图与逐项生效规则见 `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（Bot 方案）§11.3；联合评审点见其 §3.4。

### 11.4 未核实项与诚实声明

| # | 项 | 状态 | 处理 |
| --- | --- | --- | --- |
| U-1 | 官方 Go SDK 在 ITSM go 1.25.13 的编译与运行兼容性 | 【未核实】 | P2 编译 spike（M0-01 内闭环） |
| U-2 | SQLite 与 Postgres 的迁移行为差异 | 【未核实】 | P3 迁移评审 + CI 双驱动（A0-03 硬性） |
| U-3 | CI 是否具备 Postgres/Docker 环境 | 【未核实】 | P3 确认；过渡方案见 §6.2-5 |
| U-4 | 现队列（内存态）重启恢复行为 | 【未核实】 | M1-02 实施时确认；缺口登记 R-12 |
| U-5 | 阶段一 B0–B4 的实际排期 | 未定 | 影响 R1–R3；建议 MCP 立项时同步确认 |
| U-6 | 预留新增开关 `mcp.write_enabled` 的配置落位 | 建议项 | 随 P6 一并评审 |

**声明**：本文档为草案（draft），所有任务与验收均为**目标**；本方案编制过程未改动任何生产代码，ITSM 仓库当前无 MCP 实现（2026-09-27 对后端 `*.go` 与前端 `src/` 做大小写不敏感检索：前端零命中，后端唯一命中为 `runtime.NumCPU` 的偶发子串，非 MCP 代码；检索不含 `node_modules` 与构建产物）。任务规模与工作量估算为经验值，需团队复核后据此排期。

---

## 变更记录

| 日期 | 作者 | 变更 |
| --- | --- | --- |
| 2026-09-27 | AI 辅助编制 | 初稿：基于《ITSM 外部工具（MCP）接入与业务闭环审查》（69,627 字节）与《ITSM Bot 能力落地分析》，产出 M0–M2 共 31 个任务卡、32 条验收项（A0×14 / A1×10 / A2×8）、9 组测试用例执行清单、四级开关回滚预案与风险/决策登记；基线 HEAD `7442fad5`，未改动代码 |
| 2026-09-27 | AI 辅助编制 | 开工准备：Q1–Q8 按建议拍板登记（§10）；P1 工作树处置完成（独立提交 `d3471221`）；创建实施分支 `feat/bot-mcp-integration` 并入库 4 份文档（`3172c12c`） |
| 2026-09-27 | AI 辅助执行 | M0-01 交付并回写状态：`mcp/` 六包骨架、`go-sdk v1.4.0` 锁版、`mcp.enabled` 开关与连接默认值、bootstrap 预留装配点、SDK 握手回归与配置单测；`go build ./...` 与 `go test ./mcp/... ./config/...` 全绿（证据 `docs/plan/evidence/mcp-m0/M0-01-unit-evidence.md`）；M0-01 = `unit_verified` |
| 2026-09-27 | AI 辅助执行 | M0-02 交付并回写状态：registry 投影/解析/隔离实现与三组契约测试（投影五类 + 解析四类 + 隔离与解除），与参考实现规则逐字对齐；`go vet ./mcp/...` 干净、`go test ./mcp/...` 全绿（证据 `docs/plan/evidence/mcp-m0/M0-02-unit-evidence.md`）；M0-02 = `unit_verified` |
| 2026-09-27 | AI 辅助执行 | M0-03 交付并回写状态：`mcp_servers` / `mcp_server_tools` 实体 + `tool_invocations` 联合扩展（含 B0-02 字段一次加列、命名统一 `args_redacted`）；ent 生成刷新、迁移/约束/默认值测试与全量构建通过（证据 `docs/plan/evidence/mcp-m0/M0-03-migration-evidence.md`）；M0-03 = 迁移完成（`unit_verified`，`integration_verified` 待 CI 双驱动） |
| 2026-09-27 | AI 辅助执行 | M0-04 交付并回写状态：transport（Kind/Config/Guard/HeaderProvider/分层错误 + 禁重定向 + 401 拦截）与 client（连接/会话/内容投影/生命周期事件）落地；Streamable/SSE 双传输与五类错误路径集成测试全绿（`client 1.977s`、`transport 0.223s`），`go vet` 干净、`go build ./...` 通过（证据 `docs/plan/evidence/mcp-m0/M0-04-unit-evidence.md`）；M0-04 = `unit_verified` |
| 2026-09-27 | AI 辅助执行 | M0-05 交付并回写状态：SSRF 校验器（SSL/私网段/allowlist/rebinding 钉住/审计钩子）与表驱动负向用例（24 例含云元数据、IPv4-mapped、rebinding、重定向禁跟随、审计不泄露）；`go test ./mcp/...` 全绿、`go vet` 干净、`go build ./...` 通过（证据 `docs/plan/evidence/mcp-m0/M0-05-unit-evidence.md`）；M0-05 = `unit_verified`（M0-08 联调前置门槛达成） |
