# ITSM Bot 能力落地实施方案

> 文档类型：实施方案（实施步骤 + 验收标准）
> Status: draft
> 编制日期：2026-09-27
> 适用范围：把 ITSM 单助手升级为 Bot 运行时的工程落地（B0–B4）：工具元数据与审计补全、run/step/event 运行态与 SSE 事件契约、对话内确认闭环、Bot 模板与工具授权、页面入口与场景 Bot、E2E 验收与状态回写；覆盖 `itsm-backend`（Go）、`itsm-frontend`（React）、ent 模型与迁移、测试设施与文档治理
> 目标读者：后端、前端、测试（QA）、安全、产品、SRE / 运维
> 关联文档：
> - 设计依据：`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`（48,468 字节；引用简称“阶段一报告”）
> - 协同方案（强耦合）：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（MCP 实施方案，86,733 字节；引用简称“MCP 方案”）——两者共享迁移窗口、事件契约、审批/确认闭环与测试设施，合并点见本文件 §11.3
> - 参考报告：`docs/plan/itsm-mcp-external-tool-integration-analysis-2026-09-27.md`（MCP 分析报告）
> - 治理规则：`plans/README.md:5`、`docs/documentation-governance.md`
> 核查基线：`feat/vite-migration` HEAD `7442fad5`（同阶段一报告与 MCP 方案）；工作树含未提交改动（`CHANGELOG.md`、`itsm-backend/handlers/ai/repository_impl.go`、`itsm-backend/handlers/ai/service.go`，新增 `itsm-backend/handlers/ai/conversation_title.go`、`conversation_title_test.go`、`repository_conversation_delete_test.go`，见阶段一报告 §3.5）
> 核查方式：基于阶段一报告与 MCP 方案的已核实锚点做静态交叉核对；本轮未编译、未运行测试、未改动任何代码；引用锚点见 §11.2，未核实处显式标注【未核实】
> 状态口径：五级 `implemented → unit_verified → integration_verified → flow_verified → accepted`；**未达到某一级，其后级别一律不成立**（同 MCP 方案 §1.3）

## 0. 结论先行（TL;DR）

1. **本方案做什么**：把阶段一报告 §5 的目标设计与 §6 的 B0–B4 分期，展开为 **34 个可执行任务**（B0×7 / B1×10 / B2×6 / B3×7 / B4×4）、**34 条验收项**（AB0×7 / AB1×10 / AB2×6 / AB3×7 / AB4×4）与 **9 组测试用例**（BT-01…BT-09）。每个任务给出目标、依赖、改动文件（精确路径）、实施要点、测试与 DoD；每条验收给出可执行验证方法与证据要求。
2. **里程碑出口**：B0 = `integration_verified`（元数据全量标注 + 一次迁移 + dry-run 零写入契约）；B1 = `flow_verified`（run/事件/确认闭环 + 队列重启恢复）；B2 = `flow_verified`（授权∩RBAC∩风险∩入口 交集门禁 + 管理闭环）；B3 = `flow_verified`（三入口 launcher + 三场景 Bot pilot）；B4 = `accepted`（E2E 双通道 + 状态回写 + 指标看板）。
3. **与 MCP 方案的双线合并（5 条硬协同）**：① B0-02 与 MCP M0-03 **同一迁移窗口**（工具元数据 G1 + 审计字段 G7 + MCP provider 字段，一次加列，禁止二次迁移）；② B1-01/B1-03 与 MCP M1-03 **共用一份 SSE 事件注册表**（v2 契约 + MCP 工具事件扩展）；③ B1-05/B1-07 与 MCP M1-02/M1-05 共用 Gate3 审批与确认卡片；④ B2-02 的授权交集叠加 MCP `risk` 标注与工具面预算（M2-03）；⑤ B3-02/B4-01 复用 MCP M2-05 的 Playwright 设施与 M2-07 的五级/证据口径。
4. **五条不可跳过的硬门槛**：① 迁移一次到位（B0-02，与 MCP 同窗口）；② dry-run 零业务写入契约（B0-04）；③ run/step/event 落库 + SSE 版本兼容与未知事件忽略（B1-01/B1-03/B1-04）；④ 幂等键 + 过期 + 执行后 verify（B0-05/B1-05/B1-06）；⑤ 授权交集默认拒绝（B2-02）。
5. **开工前必须先办的三件事**：① BQ1–BQ8 拍板（§2.1，其中写工具下发模式阻塞 B1 设计定稿）；② 与 MCP 方案联合冻结迁移窗口与事件契约（同一评审会）；③ 工作树未提交改动处置（与 MCP P1 同一批，避免两分支混提）。
6. **诚实声明**：本文档所有状态均为**目标状态**；本次编制未改动任何代码、未编译、未运行测试。任务规模（S/M/L）为粗估，需团队按人力复核后再排日历工期。

---

## 1. 实施总则

### 1.1 目标与非目标

**实施目标**（验收口径的最终对象）：

| # | 目标 | 判定依据 |
| --- | --- | --- |
| G-A | 写动作“可判定、可回溯”：14 个工具全量元数据标注；工具调用审计可按会话/运行/目标对象回溯；参数与结果脱敏落库 | §4.1 B0-01～B0-06；验收 AB0-01～AB0-06 |
| G-B | 运行态与事件：run/step/event 落库，SSE 事件契约 v2 上线且兼容旧客户端；运行状态可见、可审计 | §4.2 B1-01～B1-04、B1-08；验收 AB1-01～AB1-04、AB1-08 |
| G-C | 对话内确认闭环：dry-run 预览 → 状态机确认（confirm/reject/expire）→ 冻结参数幂等执行 → 执行后 verify → 结果回填会话 | §4.1 B0-04/B0-05；§4.2 B1-05～B1-07、B1-09、B1-10；验收 AB0-04/AB0-05、AB1-05～AB1-07、AB1-09/AB1-10 |
| G-D | 多 Bot 治理：Bot 模板 + 工具授权 + 策略门禁（授权∩RBAC∩风险上限∩入口）+ 管理页 CRUD 与影响面预览；硬编码白名单迁移为兼容默认 | §4.3 B2-01～B2-06；验收 AB2-01～AB2-06 |
| G-E | 嵌入生命周期：工单/事件/CI 三处页面 launcher 与上下文协议；S1/S2/S3 场景 Bot 以 pilot 上线；E2E 双通道与五级状态回写 | §4.4 B3-01～B3-07；§4.5 B4-01～B4-04；验收 AB3-01～AB3-07、AB4-01～AB4-04 |

**非目标**（继承阶段一报告 §6 与 §5.8 黑名单，防范围蔓延）：

- 记忆系统（G11）、多 Agent 协作运行时、跨会话经验沉淀。
- 任意 HTTP / shell / DB 工具；Admin API、权限管理工具；删除类工具（除既有 `delete_ci_relationship` 的管理员场景）。
- 知识自动发布、事件等级/状态自动变更、`financial_sensitive` 类动作进通用 Bot。
- 不引入 `E:\projects\ai-gateway` 代码依赖；不重命名 `conversation`；不新造第二套业务存储。
- 不新增 AI 权限位（一期复用 `ai:read` / `ai:write`，见 BD8）；`/ai/chat` 保持唯一工作区，不新建 `/bots` 页面。
### 1.2 继承的冻结决策（偏离即需变更评审）

> 编号用 `BD` 前缀，与 MCP 方案的 `D1–D12` 显式区分；下表全部继承自阶段一报告的设计原则与分期结论，本方案不作变更。任何偏离必须在本文件 §10 登记，并同步回写阶段一报告；若涉及与 MCP 方案的合并点（§11.3），必须同时同步 MCP 方案。

| ID | 决策 | 状态 | 依据 |
| --- | --- | --- | --- |
| BD1 | 业务事实源不变：工单/CI/知识库仍由既有 `service` + ent 写入；Bot 只做编排、建议、工件与受控写入，不新造第二套业务存储 | 已冻结 | 阶段一报告 §5.1-1 |
| BD2 | 作用域后端解析：`ScopeResolver` 统一解析「当前用户/租户/入口上下文对象」，工具入参中的身份/租户/目标字段一律以服务端解析为准 | 已冻结 | 阶段一报告 §5.1-2 |
| BD3 | 最小暴露面：工具下发面 = `Bot 授权 ∩ 调用者域 RBAC ∩ 风险上限 ∩ 入口策略`；默认只读，写工具按模板显式授权 | 已冻结 | 阶段一报告 §5.1-3、§5.8 |
| BD4 | 写动作三要素：可预览（dry-run）、参数冻结（确认后执行持久化参数，不允许模型重新生成）、执行后校验（verify/版本回读） | 已冻结 | 阶段一报告 §5.1-4、§2.2 |
| BD5 | 增量兼容：`conversation` 即 session v1（不重命名、不双写）；确认单**扩展 `ToolInvocation`、不拆分**（单一审批事实源；若 B2 出现多次确认/多次 dry-run 快照的真实需求再评估拆表） | 已冻结 | 阶段一报告 §5.1-6、§5.3 说明 |
| BD6 | 风险分级：`read(0) / plan(1) / act_low(2) / act_medium(3) / act_high(4)`；`financial_sensitive` 不进通用 Bot；Bot `risk_limit ≥ 工具 risk` 才允许下发 | 已冻结 | 阶段一报告 §5.5、§8.1 |
| BD7 | 授权对象是 **Tool** 而非 Skill；Skill 继续作为「可运营的 AI 能力单元」由 `/api/v1/skills` 管理，二者分工不在本期合并 | 已冻结 | 阶段一报告 §3.6、§8.2 |
| BD8 | 权限一期复用 `ai:read`（查看模板/授权/审计）与 `ai:write`（管理 Bot 与审批）；不新增权限位，待真实需要再评估 `bot:read/write` | 已冻结 | 阶段一报告 §5.8 |
| BD9 | 前端保持 `/ai/chat` 唯一工作区（不新建 `/bots` 页面）；在其上增补确认抽屉、证据面板、运行状态条三组件 | 已冻结 | 阶段一报告 §5.7 |
| BD10 | 与 MCP 方案同源：一次迁移（元数据+审计+MCP 字段）、共用事件注册表与审批/确认闭环、共用 mock 与 Playwright 设施、共用五级状态与证据口径 | 已冻结 | MCP 方案 §1.2 D9、§11.3；阶段一报告 §7-11 |

### 1.3 任务编码、规模口径与状态判定

- **任务编码**：`B<期>-<两位序号>`（如 `B0-02`）；验收项编码 `AB<期>-<两位序号>`（如 `AB1-05`）；测试用例组 `BT-01…BT-09` 为本方案九组用例的顺序编号（清单见 §5.3）。
- **规模口径**（粗估，需团队复核）：`S` ≈ 0.5–1 人日；`M` ≈ 1–3 人日；`L` ≈ 3–5 人日。里程碑总工作量不给日历承诺。
- **任务默认 DoD**：后端任务默认 `unit_verified`（含契约测试）；跨组件任务在里程碑验收矩阵中定义更高目标（`integration_verified` / `flow_verified`）。
- **状态判定规则**：`implemented`（代码合并）→ `unit_verified`（单测覆盖并通过）→ `integration_verified`（组件间集成测试通过，含真实 ent/DB）→ `flow_verified`（端到端业务流通过，含前端交互或 SSE 抓包证据）→ `accepted`（验收人按本文件 §5 逐条签字）。**未达前级，后级不成立**；禁止用 checkbox 冒充交付（`plans/README.md:5`）。
- **与 MCP 方案的编号隔离**：MCP 任务/验收为 `M<期>-NN` / `A<期>-NN`，本方案为 `B<期>-NN` / `AB<期>-NN`，跨文档引用必须带文档简称（如“MCP 方案 M0-03”）。

### 1.4 文档治理与状态回写

- 每个任务合并时，在 PR 描述中附：改动文件清单、测试命令与输出、证据链接/路径（截图、SSE 抓包、审计查询结果、Playwright trace）。
- 每个里程碑收口时回写：本文件 §3.3 任务总表状态列、§5.2 验收矩阵判定列、`CHANGELOG.md`；涉及路线图（ROADMAP / 阶段一报告 / AGENTS.md）同步更新。
- **联动变更规则**：凡改动涉及 §11.3 合并点的（迁移字段、事件契约、确认状态机、授权交集、E2E 设施、验收口径），必须**同一 PR / 同一评审**同步修改 MCP 方案对应章节（B0-02↔M0-03、B1-03↔M1-03、B1-05↔M1-02、B2-02↔M1-01/M2-03、B4-01↔M2-05/M2-07），禁止单边漂移。
- 证据归档建议：`docs/plan/evidence/bot-<b0|b1|b2|b3|accepted>/`（随 PR 提交或链接 CI artifact；不强制入库大文件）。

---

## 2. 前置条件与开工检查清单

### 2.1 决策拍板项（开工前必须闭环）

来源：阶段一报告 §7 开放问题。**默认按“建议”执行；若产品/安全不拍板，不得开工对应范围。**

| # | 决策问题 | 建议 | 影响范围 | 拍板人 | 截止时点 |
| --- | --- | --- | --- | --- | --- |
| BQ1 | 写工具下发模式：保留“进 tool loop + 审批挂起”，还是“策略层不下发” | 折中：写工具仍可被模型请求，但立即返回 `confirmation_required`，不进入任何执行分支（维持零写入契约） | B1-03/B1-05/B2-02 设计定稿 | 产品 + 安全 | **B1 开工前（阻塞）** |
| BQ2 | 审批与发起人分离（四眼原则）：现状是否允许自审自批【未核实】 | `act_high` 强制分离；`act_low/medium` 可同人（配置化） | B1-05/B1-09、MCP M1-02 协同 | 产品 + 安全 | B1 开工前 |
| BQ3 | 队列持久化选型：DB 表轮询 vs 既有消息设施【MQ/Redis 有无未核实】 | DB 表轮询 + 抽象接口（可换后端）；与 MCP 审批队列**同一实现** | B1-06、MCP M1-02 | 后端 + SRE | B1-06 开工前 |
| BQ4 | 事件与运行数据保留策略 | `bot_events` 90 天热存 + 归档任务；`bot_runs/steps` 180 天 | B1-01、运维成本 | 产品 + SRE | B1-01 开工前（模型冻结） |
| BQ5 | 模型能力降级提示 | 非工具调用 provider 下 UI 显式横幅“当前模型不支持工具调用” | B1-04、B3 | 产品 | B1-04 开工前 |
| BQ6 | 会话标题改动（工作树未提交，G8）是否并入 | 先独立提交并入，作为工作区侧栏基础；与 MCP P1 同一批处置 | B1-07/B1-08 | 研发负责人 | B0 开工前 |
| BQ7 | dry-run 语义边界与文案 | 文档 + UI 双处写明“预览不保证最终成功”；失败走错误回填 | B0-04、B1-07 | 产品 | B0-04 开工前 |
| BQ8 | 与 MCP 双线排期与迁移窗口 | B0-02 与 MCP M0-03 同窗口；B1 与 MCP M1 同迭代或紧邻 | §3.4 联合路线图 | 研发负责人 + 产品 | **B0-02 开工前（阻塞）** |

### 2.2 工程前置（BP1–BP8）

| # | 前置项 | 说明 | 状态要求 |
| --- | --- | --- | --- |
| BP1 | 分支与基线固定 | 已于 2026-09-27 从 `7442fad5` 创建 `feat/bot-mcp-integration`（与 MCP 方案共用单分支）；**开工前仍须处置工作树未提交改动**（与 MCP P1 同一批：确认并入、独立提交或暂存，禁止与实施提交混提） | 开工前 |
| BP2 | 迁移协调（与 MCP M0-03 联合） | B0-02 与 MCP M0-03 **同一迁移窗口、同一字段清单评审**：`tool_invocations` 扩展 + `ToolDefinition` 元数据 + MCP provider 字段一次到位；迁移入口 `itsm-backend/internal/bootstrap/app.go:1392`（前置兼容 `:1380-1391`）；新字段一律带默认值/nullable；SQLite/Postgres 双驱动差异【未核实】须在 CI 覆盖 | BQ8 拍板后、B0-02 开工前 |
| BP3 | SSE 事件契约评审（单一注册表） | 阶段一报告 §5.4 的事件表 v2 + MCP 工具事件（MCP 方案 M1-03）合并为**一份事件注册表**：事件名、字段、时机、版本、未知事件忽略策略；服务端与前端各持一份生成物 | B1-01 开工前 |
| BP4 | 队列持久化 spike | 表结构 + 启动扫描恢复 + 并发消费 + 满队列 503 的行为原型（阶段一报告 §5.4）；结论写入 B1-06 设计；与 MCP M1-02 共用 | B1-05 前 |
| BP5 | Bot 运行时模块骨架与开关 | 建议新建 `itsm-backend/service/bot/`（路径以评审为准）承载 BotPolicy / RunManager / ScopeResolver / Redactor；新增 `bot.enabled=false` 全局开关（关闭时行为与现状零差异）；配置落位参照 `itsm-backend/config/config.go` 既有 AI 开关范式 | B0-01 前 |
| BP6 | 权限与安全复核 | 复用 `ai:read`/`ai:write` 是否足够（BD8）；黑名单固化进负向测试：Admin API / 权限管理 / 删除类 / 任意 HTTP、shell、DB 永不进 Bot 工具面 | B2-01 前 |
| BP7 | 测试设施与数据矩阵 | mock provider（对齐 `docs/plan/multi-llm-provider-plan.md` 测试策略）+ Playwright 设施（复用 MCP P5 / M0-13）；租户/角色矩阵复用 MCP P7（2 租户 × 3 角色） | B0-07 前 |
| BP8 | 预算与护栏参数 | 每 run step/token/工具调用上限、单工具超时（默认 30s）、输出字节上限、`redaction_profile` 默认档，落配置并写测试 | B1-02 前 |

### 2.3 开工检查清单

> 检查方式：逐项确认并记录结论（是/否/不适用）；任一“否”阻塞对应任务开工。

- [x] BQ1–BQ8 全部按建议拍板并登记到 §10 决策日志（2026-09-27；BQ1/BQ8 阻塞 B0-02/B1；BQ3 阻塞 B1-06；BQ4 阻塞 B1-01）。
- [x] BP1 工作树改动处置完成（2026-09-27 独立提交 `d3471221` 于 `feat/vite-migration`，与 MCP P1 同一批）。
- [ ] BP2 与 MCP 联合迁移窗口书面确认（日期 + 字段清单冻结 + 双驱动验证方式）。
- [ ] BP3 单一 SSE 事件注册表评审通过（含 MCP 事件；v2 兼容策略明确）。
- [ ] BP4 队列持久化 spike 通过（重启恢复 demo 可复跑）【待实测】。
- [ ] BP5 模块骨架与 `bot.enabled` 开关——**实现完成，待评审**（2026-09-27：`itsm-backend/service/bot/`（`doc.go`/`gate.go`）落地；`bot.enabled` 默认 false、环境变量兜底 `BOT_ENABLED`；关闭态无任何装配点=零行为变化；证据 `docs/plan/evidence/bot-b0/BP5-B0-01-unit-evidence.md`；U-B8 归属结论 = `service/bot/`）。
- [ ] BP7 mock provider 与租户/角色矩阵就绪；Playwright 环境可启动。
- [ ] 本实施方案在团队评审通过（评审记录写入 PR 或本文件 §10）。

---

## 3. 里程碑总览与依赖

### 3.1 里程碑定义与出口条件

| 期 | 名称 | 前置 | 出口条件（全部满足才算达成） | 目标级别 |
| --- | --- | --- | --- | --- |
| **B0** | 元数据与审计补全（P0） | BP1/BP2/BP5；BQ6/BQ7 拍板 | ① 14 个工具元数据全量标注并可快照进 `ToolInvocation`（AB0-01）；② 与 MCP M0-03 同窗口完成一次迁移（AB0-02）；③ 聊天路径审计回填 `conversation_id` 等字段、拒绝原因回填会话（AB0-03）；④ dry-run 零业务写入契约通过（AB0-04）；⑤ 幂等键与基础脱敏落库（AB0-05/AB0-06）；⑥ B0 证据包齐全（AB0-07） | `integration_verified` |
| **B1** | 运行态与确认闭环（P0） | B0 出口；BQ1–BQ5 拍板；BP3/BP4/BP8 完成 | ① run/step/event 三表落库，SSE v2 抓包可见且旧事件兼容（AB1-01～AB1-04）；② 确认状态机五态 + 过期 + 幂等回放 + `expected_version` 冲突中止（AB1-05）；③ 队列持久化与重启恢复、满队列 503、执行后 verify（AB1-06）；④ 前端三组件（抽屉/证据面板/状态条）交互测试通过（AB1-07/AB1-08）；⑤ 审批页增强（AB1-09）；⑥ “对话→确认→执行→回读”全链路 E2E + run-summary（AB1-10） | `flow_verified` |
| **B2** | Bot 模板与工具授权（P1） | B1 出口；BP6 完成；安全评审通过 | ① 模板/授权两表与管理 API（AB2-01）；② `BotPolicy.FilterTools` 四重交集生效，硬编码白名单迁移为兼容默认（AB2-02）；③ 管理页 CRUD + 影响面预览（AB2-03）；④ 工作区 Bot 切换与会话归属（AB2-04）；⑤ 授权负向测试全过（AB2-05）；⑥ 2 角色 × 2 入口下发矩阵验收（AB2-06） | `flow_verified` |
| **B3** | 页面入口与场景 Bot（P1） | B2 出口；与 MCP M2 可并行 | ① entrypoint 枚举与上下文协议落地、权限预检生效（AB3-01）；② 工单/事件/CI 三处 launcher 可用（AB3-02）；③ S1/S2/S3 三场景 Bot 以 pilot 上线并附验收单（AB3-03～AB3-05）；④ 计划类工具产出 artifact（AB3-06）；⑤ 三入口上下文证据齐（AB3-07） | `flow_verified` |
| **B4** | E2E 验收与状态回写（P1） | B3 出口；与 MCP M2-05/M2-07 同迭代 | ① `e2e/` api+browser 双通道可复跑、run-summary 模板产出（AB4-01）；② 指标与成本看板可查（AB4-02）；③ 五级状态回写完成（本文件/阶段一报告/ROADMAP/CHANGELOG）（AB4-03）；④ S4/S5 评估结论 + `accepted` 签署（AB4-04） | `accepted` |

### 3.2 依赖关系（含与 MCP M0–M2 的交叉）

```text
Bot 线： B0 元数据/审计 ──► B1 运行态/确认 ──► B2 模板/授权 ──► B3 入口/场景 ──► B4 E2E
              │                    │                   │                  │
MCP 线：      ▼                    ▼                   ▼                  ▼
         M0-03 同窗口迁移      M1-03 事件/M1-05 卡片  M1-01 risk/M2-03 预算  M2-05 设施  M2-07 回写
```

- **S1（迁移一次到位）**：B0-02 与 MCP M0-03 同一迁移窗口、同一字段清单；不得二次加列（互逆 MCP R1）；任一侧变更字段必须同步另一侧文档与评审。
- **S2（事件契约同源）**：B1-01/B1-03 与 MCP M1-03 共用**单一 SSE 事件注册表**；任何新增/修改事件必须双线评审，版本号与未知事件忽略策略一并维护。
- **S3（确认闭环共用）**：B1-05/B1-07 的确认状态机与卡片为双线共用设施；MCP 写工具若启用确认闭环直接复用，未启用时按 MCP R2 退化形态执行（对话内待审批提示 + 外置审批页）。
- **S4（授权面叠加）**：B2-02 的 FilterTools 输出覆盖内置 14 工具 + MCP 工具（MCP 命名 `mcp__<server>__<tool>`）；风险上限读取 B0-01 标注（内置）与 MCP M1-01 标注（外部）。
- **S5（测试与验收复用）**：B3-02/B4-01 复用 MCP M2-05 的 Playwright 设施；B4-03 与 MCP M2-07 复用五级状态与 run-summary 证据思路，保持口径一致。
- **S6（队列单一实现）**：B1-06 的持久化队列若先行，MCP M1-02 直接复用；若 MCP 先行，B1-06 必须兼容 MCP 审批队列，不得出现两套队列。
- **S7（本方案新增）**：B0 可与 MCP M0 并行启动；B1 依赖 B0 出口但不依赖 MCP M1（事件契约按注册表独立演进）；B3 若 MCP M2 未排期可独立执行（launcher 与场景 Bot 不依赖 MCP 功能本身）。

### 3.3 任务总表（WBS 索引）

> 规模为粗估（S/M/L，见 §1.3）；状态列初始为 `未开始`，随交付回写。详细任务卡见 §4，验收项见 §5.2。跨文档依赖以“MCP 方案 Mx-xx”标注。

| ID | 任务 | 层 | 交付物（摘要） | 依赖 | 规模 | 目标级别 | 验收项 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| B0-01 | 工具元数据模型与全量标注 | 后端 | `ToolDefinition` 扩展 + 14 工具标注清单（含风险矩阵落地） | BP5 | M | `unit_verified` | AB0-01 |
| B0-02 | `ToolInvocation` 扩展与联合迁移 | 后端 | ent 字段扩展 + 与 MCP M0-03 同窗口迁移 | B0-01、BP2、BQ8 | L | `integration_verified` | AB0-02 |
| B0-03 | 审计回填与拒绝原因回填 | 后端 | 聊天路径回填 `conversation_id`/run 预留；拒绝原因回填会话 | B0-02 | M | `integration_verified` | AB0-03 |
| B0-04 | dry-run 分支与零写入契约 | 后端 | `create_ticket`/`update_ticket` 预览；契约测试（业务库零写入） | B0-02、BQ7 | L | `integration_verified` | AB0-04 |
| B0-05 | 幂等键生成与落库 | 后端 | hash（tenant+actor+tool+target+args）+ 唯一索引 | B0-02 | M | `unit_verified` | AB0-05 |
| B0-06 | 基础脱敏引擎 | 后端 | `redaction_profile`（default/strict）+ `input_redacted`/`output_summary` | B0-02 | M | `unit_verified` | AB0-06 |
| B0-07 | B0 集成验收 | 测试 | 契约 + 迁移 + 审计证据包 | B0-03～B0-06 | S | `integration_verified` | AB0-07 |
| B1-01 | run/step/event 三表与 run_id 贯通 | 后端 | `bot_runs`/`bot_steps`/`bot_events` + 聊天链路写入 | B0-07、BP3、BQ4 | L | `integration_verified` | AB1-01 |
| B1-02 | RunManager 与运行态广播 | 后端 | 生命周期、预算护栏、事件落库 + SSE 广播 | B1-01 | L | `unit_verified` | AB1-02 |
| B1-03 | SSE 契约 v2 服务端与兼容层 | 后端 | 事件注册表落地；新旧事件双发 | B1-02、BP3 | L | `integration_verified` | AB1-03 |
| B1-04 | 前端 SSE 解析升级 | 前端 | `ai-api.ts` 事件解析、未知事件忽略、降级复测 | B1-03 | M | `unit_verified` | AB1-04 |
| B1-05 | 确认状态机（过期/幂等/版本） | 后端 | 五态状态机 + 幂等回放 + `expected_version` 校验 | B0-05、BP4 | L | `integration_verified` | AB1-05 |
| B1-06 | 队列持久化与恢复 | 后端 | pending 落库、启动扫描、满队列 503、重试上限、执行后 verify | B1-05、BQ3 | L | `integration_verified` | AB1-06 |
| B1-07 | ConfirmationDrawer（确认抽屉） | 前端 | dry-run 摘要/差异、倒计时、通过/拒绝（原因必填） | B1-04、B1-05 | L | `unit_verified` | AB1-07 |
| B1-08 | EvidencePanel + RunStatusBar | 前端 | 证据/时间线面板 + 运行状态条 | B1-04、B1-02 | M | `unit_verified` | AB1-08 |
| B1-09 | 审批页增强 | 前端 | risk 徽标、目标跳转、dry-run 快照、倒计时；共用 API | B1-05 | M | `unit_verified` | AB1-09 |
| B1-10 | B1 流程验收 | 测试 | 全链路 E2E + 重启恢复 + run-summary | B1-03～B1-09 | M | `flow_verified` | AB1-10 |
| B2-01 | 模板/授权模型与管理 API | 后端 | `bot_templates`/`bot_tool_grants` + CRUD API | B1-10、BP6 | L | `integration_verified` | AB2-01 |
| B2-02 | `BotPolicy.FilterTools` 交集门禁 | 后端 | 授权∩RBAC∩风险∩入口；白名单兼容默认迁移 | B2-01、B0-01 | L | `integration_verified` | AB2-02 |
| B2-03 | 管理页模板/授权 CRUD | 前端 | 复用 Skill 管理页交互 + 影响面预览 | B2-01 | L | `unit_verified` | AB2-03 |
| B2-04 | BotSelector 与会话归属 | 前端+后端 | 工作区 Bot 切换、`conversation→bot` 绑定 | B2-01、B1-08 | M | `unit_verified` | AB2-04 |
| B2-05 | 授权负向安全测试集 | 测试 | 未授权不可见/不可调用、跨租户 fail-closed | B2-02 | M | `integration_verified` | AB2-05 |
| B2-06 | B2 集成验收 | 测试 | 2 角色 × 2 入口矩阵 + 审计证据 | B2-02～B2-05 | S | `flow_verified` | AB2-06 |
| B3-01 | entrypoint 枚举与上下文协议 | 后端 | `ScopeResolver` 扩展 + 入口权限预检 | B2-06 | M | `integration_verified` | AB3-01 |
| B3-02 | 页面 launcher 组件与三处接入 | 前端 | 工单/事件/CI 详情“问 AI”入口（携带上下文） | B3-01、B1-04 | L | `unit_verified` | AB3-02 |
| B3-03 | S1 工单助手 pilot | 全栈 | 模板 + 授权 + 场景验收单 | B3-01、B3-02 | M | `flow_verified` | AB3-03 |
| B3-04 | S2 事件助手 pilot | 全栈 | 含“不自动改等级/状态”负向断言 | B3-01、B3-02 | M | `flow_verified` | AB3-04 |
| B3-05 | S3 知识助手 pilot | 全栈 | `create_kb_draft`（act_low）；不自动发布 | B3-01、B3-02 | M | `flow_verified` | AB3-05 |
| B3-06 | 计划类工具与 artifact | 后端 | `draft_ticket_fields`/`analyze_ci_impact_plan`/`draft_kb_article` → `bot_artifacts` | B1-01、B2-02 | L | `unit_verified` | AB3-06 |
| B3-07 | B3 流程验收 | 测试 | 三入口可用 + 上下文生效证据 | B3-03～B3-06 | S | `flow_verified` | AB3-07 |
| B4-01 | `e2e/` Bot 用例与 run-summary | 测试 | api + browser 双通道；run-summary 模板 | B3-07、MCP 方案 M2-05 协同 | L | `flow_verified` | AB4-01 |
| B4-02 | 指标与成本看板 | 后端+前端 | run 成功率/确认率/verify 失败率/成本（按 bot/entrypoint） | B4-01 | M | `integration_verified` | AB4-02 |
| B4-03 | 五级状态回写与文档治理 | 文档 | 本文件/阶段一报告/ROADMAP/CHANGELOG 回写 | B4-01 | S | （治理项，不单独定级） | AB4-03 |
| B4-04 | S4/S5 评估与 B4 验收 | 产品+测试 | `accepted` 评审纪要、残余风险签字 | B4-01～B4-03 | M | `accepted` | AB4-04 |

#### 3.3.1 状态速览表（2026-09-27 起回写）

> 回写口径：与各任务卡「状态」段、§5.2 判定列同源同粒度；状态取「已实际验证的最高级别」。
> 原表未设状态列，回写形式为「速览表 + 任务卡状态段」（与 MCP 方案 §3.3.1 同一范式；偏差登记见 §10）。**未列出的任务一律为 `未开始`。**

| ID | 状态 | 证据 / 备注 |
| --- | --- | --- |
| BP5（前置） | 实现完成，待评审 | `docs/plan/evidence/bot-b0/BP5-B0-01-unit-evidence.md`（`service/bot/` + `bot.enabled`） |
| B0-01 | `unit_verified` | `evidence/bot-b0/BP5-B0-01-unit-evidence.md`（14/14 标注 + 守卫测试 + 调用时快照） |
| B0-02 | 部分完成（schema 已随 MCP M0-03 联合迁移落地） | 见任务卡「状态」段；实体读写契约测试待本任务执行时补齐 |

### 3.4 双线联合路线图（与 MCP 方案合并视图）

```text
迭代 1（可并行）   B0-01/03/04/05/06        +  M0-01/02/04/05/06（骨架/命名/传输/SSRF/凭据）
★ 联合窗口 1       B0-02 ＋ M0-03（同一迁移：元数据 + 审计 + MCP 字段；同日评审）
迭代 2             B0-07（B0 出口）          +  M0-07～M0-12（生命周期/管理 API/管理页）
★ 联合窗口 2       BP3 事件注册表评审 → B1-01～B1-06 ＋ M1-01～M1-03（事件/队列同源）
迭代 3             B1-07～B1-10（B1 出口）  +  M1-04～M1-08（用户侧/审计/运维）
迭代 4             B2-01～B2-06（B2 出口）  +  M1-09/M1-10（安全负向 / M1 出口）
迭代 5             B3-01～B3-07（B3 出口）  +  M2-01～M2-04（stdio/OAuth/预算/平台矩阵）
★ 联合窗口 3       B4-01～B4-04 ＋ M2-05～M2-07（联合 E2E + accepted 评审）
```

| 联合节点 | 内容 | 参与方 | 产出 | 漏做的后果 |
| --- | --- | --- | --- | --- |
| 联合窗口 1（迁移） | B0-02 × M0-03 | 后端 + QA | 字段清单冻结、单次迁移 PR、双驱动验证 | 二次加列、审计缺口（MCP R-08） |
| 联合窗口 2（事件/队列） | BP3 × B1-03 × M1-03；B1-06 × M1-02 | 前后端 + QA | 单一事件注册表 v2、单一队列实现、抓包用例 | 双线事件漂移、两套队列、旧客户端破坏 |
| 联合窗口 3（验收） | B4-01～B4-04 × M2-05～M2-07 | 全员 | 联合 E2E 套件、run-summary、accepted 纪要 | 验收口径分裂、状态虚标 |
| 常态协同 | B1-05/B1-07 × M1-02/M1-05；B2-02 × M1-01/M2-03 | 对应任务负责人 | 共用确认状态机与授权交集 | 权限面不一致、重复实现 |

> 排期约束：① B0 与 MCP M0 同期启动即可，但迁移必须同窗口（S1）；② B1 与 MCP M1 同迭代或紧邻（S2/S3/S6）；③ B3 可与 MCP M2 并行，若 MCP 未排期则 Bot 单线执行（S7）。任何排期变化影响上述节点时，需在 §10 登记并同步 MCP 方案 §3.4/§11.3。
---

## 4. 详细实施步骤（任务卡）

> 任务卡格式：目标 / 依赖 / 改动文件 / 要点 / 测试与证据 / DoD。新建路径以评审为准；引用行号锚点见 §11.2（均来自阶段一报告与 MCP 方案的已核实清单）。

### 4.1 B0：元数据与审计补全（P0）

#### B0-01 工具元数据模型与全量标注（后端）

- **目标**：为 14 个内置工具补齐稳定元数据（risk/category/dry-run/幂等/超时/输出上限/脱敏档），使工具下发与执行前可判定（对齐 G1）。
- **依赖**：BP5。
- **改动文件**：修改 `itsm-backend/service/tool_registry.go`（扩展 `ToolDefinition`，锚点 `:14-22`；14 个工具注册处 `:126-380` 逐条标注）；修改 `itsm-backend/handlers/ai/` 工具序列化处（如需）；新建标注完整性守卫测试。
- **要点**：
  1. risk 分级按阶段一报告 §5.5：`create_ticket=act_low`、`update_ticket=act_medium`、`link_ticket_ci=act_low`、`create_ci_relationship`/`delete_ci_relationship=act_high`、`create_ticket_type=act_high（管理审批）`；8 个读工具标 `read`/`plan`；
  2. 元数据以代码为单一来源，调用时快照进 `ToolInvocation.risk/category`（防元数据漂移导致审计歧义）；
  3. 缺标注工具按最保守处理（`act_high` + 不默认下发），守卫测试保证 14/14 全量标注；
  4. 兼容性：`ToolDefinition` 旧字段不移除、不改语义（`ReadOnly/Resource/Action/ArgsSchema/ResultSchema` 原样保留）。
- **测试与证据**：标注完整性表驱动测试；`go test ./service/... ./handlers/ai/...` 全绿；工具序列化快照对比。
- **DoD**：`unit_verified`。
- **状态**：`unit_verified`（2026-09-27）——①`ToolDefinition` 扩展 6 个元数据字段（`Category/SupportsDryRun/Idempotent/TimeoutMs/MaxOutputBytes/RedactionProfile`，`Risk` 复用 M1-02 字段）+ 取值常量 + `NormalizeToolMetadata`（缺失按 `act_high`+`strict` 兜底并返回 `annotated=false`）；②14/14 内置工具按冻结风险矩阵逐条标注（写工具：`link_ticket_ci`/`create_ticket`=act_low、`update_ticket`=act_medium、`create_ticket_type`/`create_ci_relationship`/`delete_ci_relationship`=act_high；读工具 8 个含 `get_ci_impact`=plan）；③工具面装配处（`listBuiltinToolsForTenant`）对未完整标注者**不默认下发**；④调用时快照：`ToolExecution` 携带 `Risk/Category`（内置取注册表、MCP 取治理标注），落 `tool_invocations.risk/category`（`handlers/ai/entity.go`+`repository_impl.go`+`service.go`）；⑤守卫测试 `service/tool_metadata_test.go`（14/14 完整性 + 矩阵逐项 + 兜底 + `GetTool` 归一化）+ 快照断言；`go build ./...` exit 0、gofumpt 无输出。证据 `docs/plan/evidence/bot-b0/BP5-B0-01-unit-evidence.md`。

#### B0-02 ToolInvocation 扩展与联合迁移（后端）

- **目标**：扩展 `tool_invocations` 承载运行态/风险快照/目标对象/幂等/脱敏/过期/verify 字段；与 MCP M0-03 **同一迁移窗口**一次落地（BD10/S1）。
- **依赖**：B0-01、BP2、BQ8。
- **改动文件**：
  - 修改 `itsm-backend/ent/schema/tool_invocation.go`（锚点 `:14-43`；新增：`run_id`/`step_id`（nullable）、`risk`/`category`、`target_type`/`target_id`/`support_ref`、`idempotency_key_hash`、`input_redacted`、`output_summary`、`expires_at`、`verify_state`/`verify_note`、`attempt_count`/`last_error_code`）；
  - 刷新 `itsm-backend/ent/` 生成代码；同步 `itsm-backend/handlers/ai/entity.go:7-48` 结构体；
  - 与 MCP M0-03 的 `provider`/`mcp_server_name`/`mcp_raw_tool_name`/`mcp_callable_name` 字段合并到**同一 PR / 同一迁移窗口**。
- **要点**：新字段一律带默认值或 nullable（参照 `ent/schema/tool_invocation.go:31` 向后兼容注释）；唯一索引 `(tenant_id, idempotency_key_hash)`（null 不阻塞读工具）；索引首列必须含 `tenant_id`；迁移统一由 `client.Schema.Create(ctx)` 执行（`internal/bootstrap/app.go:1392`，前置兼容 `:1380-1391`）；SQLite 与 Postgres 双驱动冒烟【差异未核实，CI 必须覆盖】；历史数据不强制回填，仅 nullable。
- **测试与证据**：空库建表 + 旧库升级双路径；ent 读写断言（默认值、唯一冲突、租户隔离）；迁移窗口联合评审记录归档。
- **DoD**：`integration_verified`。
- **状态**：ent schema 字段已随 MCP M0-03 **联合迁移窗口**一次落地（2026-09-27；`run_id/step_id/risk/category/target_type/target_id/support_ref/idempotency_key_hash/expires_at/verify_state/verify_note/attempt_count/last_error_code`；命名统一：`input_redacted` → `args_redacted`）；实体结构同步（`handlers/ai/entity.go`）与读写契约测试待本任务执行时补齐。

#### B0-03 审计回填与拒绝原因回填（后端）

- **目标**：聊天执行路径回填 `conversation_id` 与 run 预留字段（G7）；审批拒绝原因可从会话侧回填（现状缺失）。
- **依赖**：B0-02。
- **改动文件**：修改 `itsm-backend/handlers/ai/service.go`（`ExecuteTool` `:118-257`、`chatStream` `:447-616`、写工具回填 `:506-530`）；修改 `itsm-backend/handlers/ai/repository_impl.go`（回填读写）；扩展 `handlers/ai/` 相关测试。
- **要点**：创建 `ToolInvocation` 时注入 `conversation_id`（当前聊天路径未写，阶段一报告 §3.5）与 `run_id`（B1-01 落表前先预留）；拒绝分支生成结构化回填（拒绝原因作为 tool result 回填给模型重新规划，参照参考实现语义）；回填内容经 B0-06 脱敏；既有 RBAC 四件套（`user_id/permission_check/permission_reason/role_snapshot`）与审计行为不变。
- **测试与证据**：契约测试（拒绝 → 会话可见回填）；审计查询可按 `conversation_id` 回溯；`tool_execution_contract_test.go:19-56` 回归通过。
- **DoD**：`integration_verified`。

#### B0-04 dry-run 分支与零写入契约（后端）

- **目标**：`create_ticket`/`update_ticket` 支持 dry-run 预览（字段预览 / 字段 diff），预览路径**零业务写入**（G4 的第一步）。
- **依赖**：B0-02、BQ7。
- **改动文件**：修改 `itsm-backend/handlers/ai/service.go` 执行分支；在 `itsm-backend/service/` 下新增预览实现（如 `tool_preview.go`，命名以评审为准）；扩展 `itsm-backend/handlers/ai/tool_execution_contract_test.go`。
- **要点**：`dry_run` 字段已存在但未使用（阶段一报告 §3.4），本期启用；预览快照（拟创建字段/diff + 版本信息）随确认单保存，供 B1-05 冻结执行参数；预览不进入业务 service 写路径，只读校验可复用的约束；UI/文档写明“预览不保证最终成功”；预览超时与输出上限吃 B0-01 元数据。
- **测试与证据**：契约测试（dry-run 后业务库零写入，模式同 `:19-56`）；预览输出结构断言；失败回填路径。
- **DoD**：`integration_verified`。

#### B0-05 幂等键生成与落库（后端）

- **目标**：写工具统一幂等键（只存 hash），作用域 = 租户 + 发起人 + 工具 + 目标 + 参数哈希；重复提交返回首次结果（G5）。
- **依赖**：B0-02。
- **改动文件**：新建 `itsm-backend/service/bot/idempotency.go`（或随 BP5 评审确定归属）；修改 `handlers/ai/service.go` 执行路径接入；依赖 B0-02 的唯一索引。
- **要点**：hash 算法与拼接顺序冻结并写死测试；读工具不生成幂等键；同一幂等键重复请求 → 返回首次执行结果（为 B1-05 的幂等回放提供底座）；唯一冲突映射为幂等命中而非 500；hash 不含明文参数。
- **测试与证据**：UT：同参数同键命中、参数变更键变化、跨租户/跨用户不串键；唯一索引冲突路径。
- **DoD**：`unit_verified`。

#### B0-06 基础脱敏引擎（后端）

- **目标**：审计/消息/工件统一脱敏入口；工具参数与结果不再原文落库（G6）。
- **依赖**：B0-02。
- **改动文件**：新建 `itsm-backend/service/bot/redactor.go`；修改 `handlers/ai/service.go` 审计写入与消息持久化接入；`ent/schema/tool_invocation.go` 字段（B0-02 已含）。
- **要点**：`default` 档（常规字段保留、密钥/token/密码类强制掩码）与 `strict` 档（高敏工具全参数掩码）；`input_redacted`/`output_summary` 成为展示与审计的唯一来源，原始参数仅内存使用不入库；密钥类字段名单可配；错误文本沿既有脱敏范式（`llm_provider_admin_service.go:818`）。
- **测试与证据**：表驱动（密钥类、嵌套 JSON、超长输出截断）；DB 断言无明文密钥；与 MCP 凭据脱敏（MCP M0-06）字段口径互查。
- **DoD**：`unit_verified`。

#### B0-07 B0 集成验收（测试）

- **目标**：B0 出口证据包与验收。
- **依赖**：B0-03～B0-06。
- **改动文件**：无生产代码；证据归档 `docs/plan/evidence/bot-b0/`。
- **要点**：迁移双驱动结果、契约测试输出、审计回溯样例、dry-run 零写入证明、脱敏断言、与 MCP M0-03 联合评审记录。
- **测试与证据**：AB0-01～AB0-06 逐项证据；QA 抽检 ≥30% 可复跑。
- **DoD**：`integration_verified`。
### 4.2 B1：运行态与确认闭环（P0）

#### B1-01 run/step/event 三表与 run_id 贯通（后端）

- **目标**：新增 `bot_runs` / `bot_steps` / `bot_events` 三表，并与聊天链路贯通（G3）；`ToolInvocation.run_id` 开始有真实来源。
- **依赖**：B0-07、BP3、BQ4。
- **改动文件**：新建 `itsm-backend/ent/schema/bot_run.go`、`bot_step.go`、`bot_event.go`（字段见阶段一报告 §5.3(b)）；刷新生成代码；修改 `itsm-backend/handlers/ai/service.go`（`chatStream` `:447-616` 生成 `run_id` 并写入）；同步 `handlers/ai/entity.go`。
- **要点**：`bot_runs(conversation_id, bot_id 预留, entrypoint, status, model, budget_json, error_code, started_at/finished_at)`；`bot_steps(run_id + step_index 唯一, type=llm/tool/confirm, payload_ref, duration_ms)`；`bot_events(run_id + seq 唯一, type, payload_json)`；三表均含 `tenant_id` 且索引首列含租户维度（阶段一报告 §7-4）；保留策略按 BQ4（events 90 天热存 + 归档、runs/steps 180 天）。
- **测试与证据**：一次对话产生 run + steps + events 的集成断言；租户隔离断言；`(run_id, seq)` 唯一冲突路径。
- **DoD**：`integration_verified`。

#### B1-02 RunManager 与运行态广播（后端）

- **目标**：承载 run 生命周期、预算护栏、事件落库 + 广播（SSE 层解耦）。
- **依赖**：B1-01。
- **改动文件**：新建 `itsm-backend/service/bot/run_manager.go`（含预算判定）；修改 `handlers/ai/service.go` 接入；对应测试。
- **要点**：状态推进 `running → completed/failed`（`cancelled` 预留）；预算护栏：每 run step 数 / token / 工具调用次数 / 单工具超时（默认 30s）/ 输出字节上限，超限以 `error{code=budget_exceeded}` 结束并落审计（BP8）；事件**先落库后广播**，保证 SSE 重放与审计同源；RunManager 通过接口输出事件流，便于单测与未来替换。
- **测试与证据**：UT（预算超限、状态转换、落库顺序）；集成（chatStream 事件序列与 DB 一致）。
- **DoD**：`unit_verified`。

#### B1-03 SSE 契约 v2 服务端与兼容层（后端）

- **目标**：落地**单一事件注册表**（阶段一报告 §5.4 v2 + MCP 工具事件），新旧事件兼容（G3）。
- **依赖**：B1-02、BP3。
- **改动文件**：新建 `itsm-backend/handlers/ai/sse_events.go`（注册表 + 序列化 + 版本）；修改 `chatStream` 的 SSE 输出；新增 `sse_events_test.go`。
- **要点**：事件：`run_started` / `step` / `tool_call` / `confirmation_required` / `delta` / `sources` / `artifact` / `done` / `error`；旧事件名（`delta/sources/done/error`）保持可用（双发或等价映射，评审定）；未知事件忽略策略写入注册表文档；MCP 相关事件（工具调用来源为 `mcp__*`）同表登记，与 MCP 方案 M1-03 **同一注册表、同一 PR 节奏**（S2）。
- **测试与证据**：SSE 抓包断言事件名与字段；旧客户端兼容用例；与 MCP M1-03 的联合评审记录。
- **DoD**：`integration_verified`。

#### B1-04 前端 SSE 解析升级（前端）

- **目标**：`ai-api.ts` 支持 v2 事件解析；未知事件忽略（前向兼容，阶段一报告 §7-9）。
- **依赖**：B1-03。
- **改动文件**：修改 `itsm-frontend/src/lib/api/ai-api.ts`（锚点 `:521-660`、`:862-868`）；扩展 `src/lib/api/__tests__/ai-api.test.ts`。
- **要点**：事件名分发与状态归并分层；**保留并复测** CSRF 轮换重试、同源代理回退与失败降级逻辑；非工具调用 provider 的降级横幅（BQ5）；未知事件静默忽略但可日志上报。
- **测试与证据**：Jest 用例（v2 全事件 / 仅 v1 旧事件 / 未知事件三类）；手工联调截图。
- **DoD**：`unit_verified`。

#### B1-05 确认状态机（过期/幂等/版本）（后端）

- **目标**：`ToolInvocation` 从「审批」升级为**确认状态机**：`pending/confirmed/rejected/expired/cancelled` + 幂等回放 + `expected_version`（G4/G5）。
- **依赖**：B0-05、BP4。
- **改动文件**：修改 `itsm-backend/handlers/ai/service.go`（`ExecuteTool`/`ApproveTool` 路径 `:118-257`）；新增确认服务（`itsm-backend/service/bot/confirmation.go`，归属随 BP5）；扩展契约测试。
- **要点**：过期扫描（`expires_at` 默认 24h，定时任务；过期后不可执行）；拒绝原因结构化回填会话（B0-03 已铺路）；重复确认/重复提交**返回首次执行结果**（幂等回放，不重复写业务状态）；`update_ticket` 版本冲突中止并提示刷新（乐观锁，阶段一报告 §5.5）；四眼原则按 BQ2 落地（`act_high` 强制分离）；参数冻结：确认后执行持久化参数，模型不可改参。
- **测试与证据**：状态迁移矩阵（合法/非法迁移）；过期、重复提交、版本冲突、拒绝回填四类路径；四眼断言（若启用）。
- **DoD**：`integration_verified`。

#### B1-06 队列持久化与恢复（后端）

- **目标**：解决现状三个缺口：进程内队列重启即丢、满队列静默丢弃、执行后无 verify（阶段一报告 §3.4）。
- **依赖**：B1-05、BQ3。
- **改动文件**：修改 `itsm-backend/service/tool_queue.go`（锚点 `:30-130`；`Enqueue` 满队列行为 `:45-50`）；按 BP4 结论落库（新队列表或复用 ToolInvocation 状态字段）；扩展测试。
- **要点**：与 MCP M1-02 **共用同一实现**（S6），禁止两套队列；启动扫描恢复 pending；满队列返回 503（不再静默丢弃）；失败重试上限 + `last_error_code`/`attempt_count`；**执行后 verify**（回读目标对象存在性/状态）写 `verify_state`/`verify_note`；并发消费下幂等键生效。
- **测试与证据**：重启恢复用例（执行中 kill → 重启继续）；满队列 503 断言；verify 成功/失败路径；重试上限。
- **DoD**：`integration_verified`。

#### B1-07 ConfirmationDrawer（前端）

- **目标**：确认抽屉组件：dry-run 摘要/差异、过期倒计时、通过/拒绝（拒绝原因必填）。
- **依赖**：B1-04、B1-05。
- **改动文件**：新建 `itsm-frontend/src/components/ai/ConfirmationDrawer.tsx`（+组件测试）；接入 `itsm-frontend/src/components/ai/AIChat.tsx`。
- **要点**：数据仅来自确认单（脱敏参数 + dry-run 快照 + `expires_at`）；倒计时归零禁用操作；拒绝原因必填并回填会话；与审批页共用同一 API 与数据形状（B1-09）；不新增独立路由。
- **测试与证据**：组件测试（四态、倒计时、必填校验、过期禁用）；手工 E2E 截图。
- **DoD**：`unit_verified`。

#### B1-08 EvidencePanel + RunStatusBar（前端）

- **目标**：证据/时间线面板与运行状态条（BD9 三组件之二）。
- **依赖**：B1-04、B1-02。
- **改动文件**：新建 `itsm-frontend/src/components/ai/EvidencePanel.tsx`、`RunStatusBar.tsx`（+组件测试）；接入 AIChat。
- **要点**：时间线消费 `step`/`tool_call` 事件，含 `target_type/id` 跳转与 `support_ref` 展示；状态条展示 run 状态、模型、预算消耗；只读展示，不引入新交互路径；空态/错误态明确。
- **测试与证据**：组件测试（事件序列渲染、空态、错误态）。
- **DoD**：`unit_verified`。

#### B1-09 审批页增强（前端）

- **目标**：`/ai/approval` 页补 risk 徽标、目标对象跳转、dry-run 快照、过期倒计时（阶段一报告 §5.7）。
- **依赖**：B1-05。
- **改动文件**：修改 `itsm-frontend/src/pages/(main)/ai/approval/index.tsx`；复用抽屉子组件。
- **要点**：与抽屉共用同一 API/数据形状；保留既有权限校验列展示；过期项置灰并说明原因；移动端不劣化（现有页面范式）。
- **测试与证据**：组件测试；与抽屉数据一致性核对。
- **DoD**：`unit_verified`。

#### B1-10 B1 流程验收（测试）

- **目标**：B1 出口证据包：全链路 E2E + 重启恢复 + run-summary。
- **依赖**：B1-03～B1-09。
- **改动文件**：新增 E2E 用例（建议 `itsm-backend` 契约 + `e2e/` 雏形，完整化在 B4-01）；证据归档 `docs/plan/evidence/bot-b1/`。
- **要点**：一条「对话 → 确认 → 执行 → 回读」链路（含 SSE 抓包）、一条队列重启恢复用例、run-summary 模板定稿。
- **测试与证据**：AB1-01～AB1-09 逐项证据；QA 抽检。
- **DoD**：`flow_verified`。
### 4.3 B2：Bot 模板与工具授权（P1）

#### B2-01 模板/授权模型与管理 API（后端）

- **目标**：新增 `bot_templates` / `bot_tool_grants` 两表（阶段一报告 §5.3(b)）与管理 API（CRUD），管理权限复用 `ai:write`（BD8）。
- **依赖**：B1-10、BP6。
- **改动文件**：新建 `itsm-backend/ent/schema/bot_template.go`、`bot_tool_grant.go`；刷新生成代码；新增管理服务/API（建议 `itsm-backend/service/bot/admin.go` + handler，路径评审定）；`router/router.go` 装配（参考 skills 管理段范式 `:579-591`）。
- **要点**：`bot_templates(slug 唯一, name, audience, risk_limit, entrypoints_json, system_prompt_ref, status=draft/pilot/ga)`；`bot_tool_grants(bot_id + tool_name 唯一, risk_limit, args_policy_json)`；全表含 `tenant_id` 并索引首列；删除模板时的授权级联策略；内置"默认助手"种子模板保持现状等价行为（兼容默认）。
- **测试与证据**：CRUD 集成测试；唯一约束与租户隔离；路由权限（`ai:write`）测试。
- **DoD**：`integration_verified`。

#### B2-02 BotPolicy.FilterTools 交集门禁（后端）

- **目标**：工具下发/执行前四重交集：`授权 ∩ RBAC ∩ 风险上限 ∩ 入口`（G1/G2）；把硬编码写白名单迁移为兼容默认。
- **依赖**：B2-01、B0-01。
- **改动文件**：新建 `itsm-backend/service/bot/policy.go`；修改 `handlers/ai/service.go`（工具面装载 `:485-504`、写白名单 `:385-398`）。
- **要点**：`FilterTools(bot, caller, entrypoint)` 产出工具面；**执行侧二次校验**（不可只靠下发过滤）；MCP 工具（`mcp__*` 命名）与内置工具走同一策略（S4），risk 取 B0-01（内置）/MCP M1-01（外部）标注，风险上限取模板 `risk_limit`；拒绝记 `permission_reason` 模式审计；兼容默认：无模板命中时等价现状白名单（上线先保持行为一致，再按灰度切换）。
- **测试与证据**：角色 × 入口 × risk 矩阵测试；未授权调用被拒且审计可查；兼容默认回归（存量行为不变）。
- **DoD**：`integration_verified`。

#### B2-03 管理页模板/授权 CRUD（前端）

- **目标**：Bot 模板与工具授权管理页（CRUD + 影响面预览，阶段一报告 §5.7）。
- **依赖**：B2-01。
- **改动文件**：新增管理页面（挂载路径与菜单按评审；交互复用 Skill 管理页范式）与 API 层文件（命名评审定）。
- **要点**：保存前"影响面预览"：展示哪些角色/入口将获得该工具；表单校验（slug/risk_limit/入口枚举）；只读与写权限分离；变更审计。
- **测试与证据**：组件测试；手工全流程（新增 → 授权 → 生效 → 回收）。
- **DoD**：`unit_verified`。

#### B2-04 BotSelector 与会话归属（前端 + 后端）

- **目标**：工作区 Bot 切换；新会话绑定 Bot（`conversation → bot` 归属）。
- **依赖**：B2-01、B1-08。
- **改动文件**：新建 `itsm-frontend/src/components/ai/BotSelector.tsx` + AIChat 接入；后端会话关联字段与查询（`handlers/ai/entity.go`、repository）。
- **要点**：默认 Bot 兼容（未选择 = 默认助手）；切换仅影响新会话，不串改历史会话归属；`audience` 过滤可见 Bot。
- **测试与证据**：组件测试；集成测试（会话归属落库与回读）。
- **DoD**：`unit_verified`。

#### B2-05 授权负向安全测试集（测试）

- **目标**：安全负向用例固化（对应 G1/G2 的 fail-closed 要求）。
- **依赖**：B2-02。
- **改动文件**：扩展 `itsm-backend/handlers/ai/` 测试；前端组件权限测试。
- **要点**：未授权工具**不可见且不可调用**（双端）；跨租户 fail-closed；黑名单（Admin API / 权限管理 / 删除类 / 任意 HTTP、shell、DB）永不下发；模型试图覆盖身份/租户/目标参数被 `ScopeResolver` 覆写并审计。
- **测试与证据**：测试集输出；审计举证（拒绝原因快照）。
- **DoD**：`integration_verified`。

#### B2-06 B2 集成验收（测试）

- **目标**：B2 出口证据包：2 角色 × 2 入口下发矩阵 + 审计证据。
- **依赖**：B2-02～B2-05。
- **改动文件**：无生产代码；证据归档 `docs/plan/evidence/bot-b2/`。
- **要点**：矩阵逐格判定（可见/可调用/拒绝原因）；兼容默认回归报告。
- **测试与证据**：AB2-01～AB2-05 逐项证据。
- **DoD**：`flow_verified`。

### 4.4 B3：页面入口与场景 Bot（P1）

#### B3-01 entrypoint 枚举与上下文协议（后端）

- **目标**：定义入口枚举（`chat` / `ticket_detail` / `ticket_list` / `incident_detail` / `incident_create` / `ci_detail` …）与上下文协议 `{entrypoint, target_type, target_id, summary}`；`ScopeResolver` 扩展 + 权限预检（G9）。
- **依赖**：B2-06。
- **改动文件**：新建 `itsm-backend/service/bot/scope_resolver.go`（或扩展 `handlers/ai/` 既有注入逻辑，随 BP5）；修改 `ExecuteTool` 注入点；协议登记进 SSE/上下文文档。
- **要点**：身份/租户/目标一律以服务端解析为准（BD2）；目标对象存在性与访问权限**预检**（不可越权携入上下文）；上下文仅作提示，不替代每次调用的校验；入口值写入 `bot_runs.entrypoint` 供审计与工具面过滤。
- **测试与证据**：UT（伪造 target 被拒或覆写并审计）；集成（入口上下文进入 run 记录）。
- **DoD**：`integration_verified`。

#### B3-02 页面 launcher 组件与三处接入（前端）

- **目标**：工单/事件/CI 详情页"问 AI"入口，携带上下文打开工作区（G9/G10）。
- **依赖**：B3-01、B1-04。
- **改动文件**：新建 `itsm-frontend/src/components/ai/AskAILauncher.tsx`（+测试）；接入 `components/ticket/TicketDetail.tsx`、`pages/(main)/incidents/...`、CI 详情页（以现状路径为准）。
- **要点**：launch 携带 `entrypoint/target_type/target_id/summary` 打开 `/ai/chat`；无权限时隐藏/置灰（预检）；渐进增强，不改变页面主流程布局。
- **测试与证据**：组件测试（携带参数/权限态）；手工截图。
- **DoD**：`unit_verified`。

#### B3-03 S1 工单助手 pilot（全栈）

- **目标**：S1 场景 Bot 以 pilot 上线（阶段一报告 §5.6：读 `list_tickets`/`get_incident_stats`/`list_kb`，写 `create_ticket`(act_low)，plan `draft_ticket_fields`）。
- **依赖**：B3-01、B3-02。
- **改动文件**：模板/授权种子数据；场景验收单（`docs/plan/evidence/bot-b3/`）。
- **要点**：典型链路：自然语言查询 → 对话创建工单（确认抽屉 → 执行 → 回填工单号）；验收要点：SSE 含 `confirmation_required`、确认后执行 ≤30s、执行后 verify 回读、审计含 `conversation_id/target_type/target_id`。
- **测试与证据**：场景验收单 + E2E 片段 + 审计截图。
- **DoD**：`flow_verified`。

#### B3-04 S2 事件/值班助手 pilot（全栈）

- **目标**：S2 场景 Bot（`get_incident_stats`、`list_tickets`、`get_ci_impact`、`link_ticket_ci`(act_low)）。
- **依赖**：B3-01、B3-02。
- **改动文件**：模板/授权种子数据；场景验收单。
- **要点**：相似事件检索与影响面分析；定级建议**只建议不自动变更**（负向断言，与 `ROADMAP.md:146-159` human-in-the-loop 一致）；一键关联 CI 走确认闭环；影响面工具超时与 `max_output_bytes` 生效。
- **测试与证据**：负向断言（无自动改级/改状态调用）+ 场景验收单。
- **DoD**：`flow_verified`。

#### B3-05 S3 知识/自助助手 pilot（全栈）

- **目标**：S3 场景 Bot（读 `list_kb`；`draft_kb_article`；新增 `create_kb_draft`(act_low)）。
- **依赖**：B3-01、B3-02。
- **改动文件**：模板/授权种子数据；`create_kb_draft` 工具实现与注册；场景验收单。
- **要点**：问答引用 → 一键保存草稿（artifact + 知识草稿，归属当前用户）；**不得自动发布**（publish 属 act_high，后续评估）；草稿租户隔离。
- **测试与证据**：归属/隔离测试；负向断言（无自动发布）+ 场景验收单。
- **DoD**：`flow_verified`。

#### B3-06 计划类工具与 artifact（后端）

- **目标**：`draft_ticket_fields`(plan) / `analyze_ci_impact_plan`(analysis) / `draft_kb_article`(draft)（`propose_change_plan` 可选）落地，产出 `bot_artifacts`。
- **依赖**：B1-01、B2-02。
- **改动文件**：`itsm-backend/service/tool_registry.go` 注册与实现；`bot_artifacts` 表（本任务新增）与写入路径。
- **要点**：plan/analysis 类工具**只读不写业务库**；artifact 归属与会话隔离（`owner_user_id`）；输出含证据引用（RAG 来源/CI 关系）；预算与超时沿用 B0-01 元数据。
- **测试与证据**：UT + 集成（artifact 归属、租户隔离、零业务写入）。
- **DoD**：`unit_verified`。

#### B3-07 B3 流程验收（测试）

- **目标**：B3 出口证据包：三入口可用 + 上下文生效 + 三场景验收单 + 负向断言。
- **依赖**：B3-03～B3-06。
- **改动文件**：无生产代码；证据归档 `docs/plan/evidence/bot-b3/`。
- **要点**：逐入口上下文证据（run.entrypoint/target 落库）；S1/S2/S3 验收单；"不自动变更/不自动发布"断言。
- **测试与证据**：AB3-01～AB3-06 逐项证据。
- **DoD**：`flow_verified`。
### 4.5 B4：E2E 验收与状态回写（P1）

#### B4-01 `e2e/` Bot 用例与 run-summary（测试）

- **目标**：建立 Bot E2E 章程（G12）：api + browser 双通道用例与 run-summary 模板；与 MCP M2-05 共用 Playwright 设施。
- **依赖**：B3-07、MCP 方案 M2-05 协同。
- **改动文件**：新增 `e2e/` 目录与脚本入口（若 MCP 未排期则自建，见 S5/S7）；run-summary 模板文件；用例清单文档。
- **要点**：api 通道（确认→执行→verify、队列重启恢复、授权矩阵）；browser 通道（工作区 SSE 流、确认抽屉、证据面板、三处 launcher 入口）；mock provider 全量 + 少量 real smoke（阶段一报告 §7-10）；未知事件兼容用例。
- **测试与证据**：脚本可复跑；run-summary 示例归档。
- **DoD**：`flow_verified`。

#### B4-02 指标与成本看板（后端 + 前端）

- **目标**：复用 `/ai/metrics` 扩展 run 维度指标（阶段一报告 §5.9）。
- **依赖**：B4-01。
- **改动文件**：扩展 `itsm-backend/handlers/ai/` 指标端点；前端看板页面（挂载点评审定）；指标文档。
- **要点**：run 成功率、确认通过/拒绝/过期率、工具错误率、verify 失败率、平均步数与时延、token 消耗（按 bot/entrypoint/tool）；高风险/高频 Bot 单独限额（成本护栏）。
- **测试与证据**：指标断言测试；看板截图归档。
- **DoD**：`integration_verified`。

#### B4-03 五级状态回写与文档治理（文档）

- **目标**：按五级状态回写：本文件 §3.3 状态列与 §5.2 判定列、阶段一报告、`ROADMAP.md` / `CHANGELOG.md`（G12）。
- **依赖**：B4-01。
- **改动文件**：文档回写（本文件、阶段一报告、`CHANGELOG.md`，路线图如涉及）。
- **要点**：禁止用 checkbox 或"设计完成"冒充交付（`plans/README.md:5`）；本次回写 diff 留档；变更记录追加一行。
- **测试与证据**：回写 diff 归档。
- **DoD**：治理项（不单独定级，纳入 AB4-03）。

#### B4-04 S4/S5 评估与 B4 验收（产品 + 测试）

- **目标**：S4 变更方案助手 / S5 员工自助与服务请求助手的评估结论；B4 `accepted` 评审。
- **依赖**：B4-01～B4-03。
- **改动文件**：评估结论文档；验收纪要（`docs/plan/evidence/bot-accepted/`）。
- **要点**：逐条对照 AB4-01～AB4-03 判定；残余风险逐条登记并签字；S4/S5 给出"进入下一期 / 暂缓"明确结论。
- **测试与证据**：验收纪要 + 签字。
- **DoD**：`accepted`。

---

## 5. 验收标准

### 5.1 通用 DoD（所有任务适用）

1. **代码任务**：变更文件清单齐全；`go build ./...`、`go test ./...`（后端）/ `npm test`（前端）全绿；新增/修改行为有测试覆盖；lint（golangci-lint / eslint）通过。
2. **迁移任务**：空库建表 + 旧库升级双路径验证；新字段默认值/nullable；提供回滚预案（字段可空即兼容，避免不可逆变更）。
3. **前端任务**：组件测试 + 至少一条可复跑交互路径（手工截图或 Playwright）；不得只做静态渲染（阶段一报告 §2.6 教训）。
4. **安全任务**：负向用例必须全过方可合并（未授权/跨租户/黑名单/注入覆写）。
5. **文档任务**：状态列、变更记录、证据目录三者同步。
6. **证据四要素**：每个任务在 PR 中附「执行步骤/命令 / 结果 / 时间 / 执行人与环境」。

### 5.2 验收矩阵

> 目标级别为**最低要求**；未达前级，后级不成立。判定列在里程碑收口时回写（`达标 / 豁免 / 阻塞`）。

#### B0（目标：`integration_verified`）

| 验收项 | 验收内容（可验证陈述） | 关联任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| AB0-01 | 14/14 工具含 risk/category/dry-run/幂等/超时/输出上限/脱敏档标注；缺标注守卫测试失败；调用时快照进 `ToolInvocation` | B0-01 | 表驱动单测 + DB 断言 | 测试输出 | `unit_verified` |
| AB0-02 | 空库建表 + 旧库升级双路径通过；新字段默认值/nullable 正确；与 MCP M0-03 同一次迁移（单 PR / 同窗口）；双驱动冒烟（CI 具备时） | B0-02 | 迁移测试 + 联合评审记录 | 测试输出、迁移 diff | `integration_verified` |
| AB0-03 | 聊天路径 `conversation_id` 回填且可查询；拒绝原因回填会话可见；审计可按会话回溯工具调用 | B0-03 | 契约测试 + 审计查询 | 测试输出、审计截图 | `integration_verified` |
| AB0-04 | dry-run 预览产生**业务库零写入**；预览含字段/字段 diff；失败路径有回填 | B0-04 | 契约测试 | 测试输出 | `integration_verified` |
| AB0-05 | 幂等键 hash 落库、作用域正确；重复提交命中幂等返回首次结果；跨租户不串键 | B0-05 | UT + 集成 | 测试输出 | `unit_verified` |
| AB0-06 | 密钥/token 类字段不落库；`input_redacted`/`output_summary` 可用；strict 档生效 | B0-06 | 表驱动 + DB 断言 | 测试输出 | `unit_verified` |
| AB0-07 | B0 证据包齐全：迁移/契约/审计/dry-run/脱敏 + 与 MCP 联合评审记录 | B0-07 | 证据评审 | 证据目录 | `integration_verified` |

#### B1（目标：`flow_verified`）

| 验收项 | 验收内容（可验证陈述） | 关联任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| AB1-01 | 一次对话产生 `bot_runs/steps/events` 且 `run_id` 贯通（invocation 可关联）；三表租户索引生效 | B1-01 | 集成测试 | 测试输出 | `integration_verified` |
| AB1-02 | 预算超限以 `budget_exceeded` 结束并审计；事件先落库后广播（可重放） | B1-02 | UT + 集成 | 测试输出 | `unit_verified` |
| AB1-03 | SSE v2 事件名/字段符合注册表（抓包）；旧事件名仍可用；MCP 事件同表登记 | B1-03 | 抓包 + 双线联合评审 | 抓包文件 | `integration_verified` |
| AB1-04 | 前端解析 v2 事件；未知事件忽略；CSRF 轮换与降级逻辑复测通过 | B1-04 | Jest | 测试输出 | `unit_verified` |
| AB1-05 | 状态机五态合法/非法迁移断言；过期后不可执行；重复确认返回首次结果；`expected_version` 冲突中止 | B1-05 | 状态矩阵测试 | 测试输出 | `integration_verified` |
| AB1-06 | 重启恢复（执行中 kill → 恢复继续）；满队列返回 503；重试上限；执行后 verify 写 `verify_state` | B1-06 | 集成 + 重启用例 | 测试输出 | `integration_verified` |
| AB1-07 | 抽屉四态、倒计时、拒绝原因必填、与审批页共用 API | B1-07 | 组件测试 + 手工 | 截图、测试输出 | `unit_verified` |
| AB1-08 | 证据面板渲染 step/tool_call 时间线与 target 跳转；状态条显示 run 状态/预算 | B1-08 | 组件测试 | 测试输出 | `unit_verified` |
| AB1-09 | 审批页 risk 徽标、目标跳转、dry-run 快照、过期倒计时可用 | B1-09 | 组件测试 | 测试输出 | `unit_verified` |
| AB1-10 | 「对话 → 确认 → 执行 → 回读」全链路 E2E + run-summary；既有回归通过 | B1-10 | E2E | E2E 报告、抓包 | `flow_verified` |

#### B2（目标：`flow_verified`）

| 验收项 | 验收内容（可验证陈述） | 关联任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| AB2-01 | 模板/授权 CRUD 可用；唯一约束与租户隔离；路由权限 `ai:write` 生效 | B2-01 | 集成测试 | 测试输出 | `integration_verified` |
| AB2-02 | `FilterTools` 四重交集生效；未授权工具不可见且不可调用并审计；兼容默认回归通过 | B2-02 | 角色×入口×risk 矩阵测试 | 测试输出 | `integration_verified` |
| AB2-03 | 管理页 CRUD + 影响面预览可交互；只读/写分离 | B2-03 | 组件测试 + 手工 | 截图 | `unit_verified` |
| AB2-04 | 工作区 Bot 切换可用；新会话归属正确；`audience` 过滤生效 | B2-04 | 组件 + 集成 | 测试输出 | `unit_verified` |
| AB2-05 | 负向安全集全过：未授权/跨租户/黑名单/注入覆写 | B2-05 | 安全测试 | 测试输出 | `integration_verified` |
| AB2-06 | 2 角色 × 2 入口下发矩阵逐格判定 + 审计证据齐全 | B2-06 | 矩阵验收 | 矩阵判定表 | `flow_verified` |

#### B3（目标：`flow_verified`）

| 验收项 | 验收内容（可验证陈述） | 关联任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| AB3-01 | 入口枚举与上下文协议落地；伪造 target 被拒或覆写并审计；`run.entrypoint` 记录 | B3-01 | UT + 集成 | 测试输出 | `integration_verified` |
| AB3-02 | 三处 launcher 可用并携带上下文；无权限时隐藏/置灰 | B3-02 | 组件测试 + 手工 | 截图 | `unit_verified` |
| AB3-03 | S1 链路（查询 → 创建确认 → 回填工单号）；SSE 含 `confirmation_required`；≤30s；verify 回读；审计含三元组 | B3-03 | 场景 E2E | 验收单 | `flow_verified` |
| AB3-04 | S2 只建议不自动变更（负向断言）；关联 CI 走确认；超时/输出上限生效 | B3-04 | 场景 E2E | 验收单 | `flow_verified` |
| AB3-05 | S3 草稿归属与租户隔离正确；不自动发布（负向断言） | B3-05 | 场景测试 | 验收单 | `flow_verified` |
| AB3-06 | 计划类工具零业务写入；artifact 归属正确且含证据引用 | B3-06 | 集成测试 | 测试输出 | `unit_verified` |
| AB3-07 | 三入口上下文证据 + 三场景验收单 + 负向断言齐全 | B3-07 | 证据评审 | 证据目录 | `flow_verified` |

#### B4（目标：`accepted`）

| 验收项 | 验收内容（可验证陈述） | 关联任务 | 验证方法 | 证据 | 目标级别 |
| --- | --- | --- | --- | --- | --- |
| AB4-01 | `e2e/` api + browser 双通道可复跑；run-summary 模板产出；mock 全量 + real smoke | B4-01 | E2E | 脚本、summary | `flow_verified` |
| AB4-02 | 指标可查：run 成功率/确认率/verify 失败率/成本维度 | B4-02 | 指标断言 + 看板 | 测试输出、截图 | `integration_verified` |
| AB4-03 | 五级状态回写完成（本文件、阶段一报告、ROADMAP/CHANGELOG 回写 diff） | B4-03 | 治理检查 | 回写 diff | （治理项） |
| AB4-04 | `accepted` 签署；残余风险登记；S4/S5 结论明确 | B4-04 | 出口评审会 | 验收纪要 | `accepted` |
### 5.3 测试用例执行清单（BT-01…BT-09）

| 用例组 | 覆盖内容 | 测试层 | 执行时机 | 归属任务 | 关联验收 |
| --- | --- | --- | --- | --- | --- |
| BT-01 元数据与门禁 | 14 工具标注完整性；缺标注守卫失败；快照落库；未授权默认拒绝 | UT | 每次提交 CI | B0-01、B2-02 | AB0-01、AB2-02 |
| BT-02 迁移 | 空库/旧库双路径；默认值/nullable；租户索引；双驱动冒烟 | IT（ent+DB） | CI + 联合窗口 1 | B0-02 | AB0-02 |
| BT-03 dry-run 零写入 | `create_ticket`/`update_ticket` 预览业务库零写入；快照结构；失败回填 | 契约 | 每次提交 CI | B0-04 | AB0-04 |
| BT-04 审计与脱敏 | `conversation_id`/run/target/幂等回填；输入脱敏、输出截断；DB 无明文密钥 | UT + IT | CI | B0-03、B0-05、B0-06 | AB0-03、AB0-05、AB0-06 |
| BT-05 run/事件与 SSE | 三表落库与 run_id 贯通；v2 抓包；旧事件兼容；未知事件忽略 | IT + 抓包 | CI + 里程碑 | B1-01～B1-04 | AB1-01～AB1-04 |
| BT-06 确认状态机 | 五态迁移、过期、幂等回放、`expected_version` 冲突、四眼（若启用） | IT | CI | B1-05 | AB1-05 |
| BT-07 队列持久化 | 重启恢复、满队列 503、重试上限、执行后 verify | IT | CI + nightly | B1-06 | AB1-06 |
| BT-08 授权与隔离 | 角色×入口×risk 矩阵；跨租户 fail-closed；黑名单；注入覆写 | IT + 安全 | CI | B2-02、B2-05 | AB2-02、AB2-05 |
| BT-09 前端与 E2E | 抽屉/证据面板/状态条组件测试；launcher 上下文；三场景 pilot；api + browser E2E | 组件 + E2E | 组件每次 CI；E2E nightly/里程碑 | B1-07～B1-09、B2-03～B2-04、B3-02～B3-05、B4-01 | AB1-07～AB1-09、AB2-03、AB2-04、AB3-02～AB3-05、AB4-01 |

> 执行原则：BT-01/BT-03/BT-04 为**每次提交必跑**（快、确定性高）；BT-02/BT-05～BT-08 为 CI（可用内存 DB / mock）；BT-09 组件测试入库 CI，Playwright E2E 走 nightly + 里程碑出口。依赖真实 provider 的部分一律 mock 化，仅保留少量 smoke（对齐阶段一报告 §7-10 与 `docs/plan/multi-llm-provider-plan.md` 既有策略）。

### 5.4 阶段出口判定规则

1. **全量达标**：该里程碑全部 AB 项达到或超过目标级别；未达项一律阻塞，禁止"带病升级"。
2. **回归全绿**：此前里程碑全部验收项回归通过（回归集 = 既有自动化用例 + 既往 AB 项对应用例）。
3. **缺陷门槛**：无未决 P0；P1 必须修复或书面豁免（豁免需产品 + 安全签字并登记 §10）。
4. **证据完整**：§5.5 要求的证据归档齐全，抽查可复跑。
5. **治理回写**：任务状态、验收判定、`CHANGELOG.md` 回写完成。
6. **判定组织**：里程碑出口评审会（建议参与：研发负责人、QA、安全、产品）；输出验收纪要（日期/参与人/逐项判定/豁免项/结论）。
7. **双线联合评审**：联合窗口 1/2/3（§3.4）必须由 Bot 与 MCP 双方负责人共同签署；单方通过的迁移/事件契约变更无效。
8. **豁免处理**：任何豁免必须登记 §10 决策日志并同步降级受影响验收项的状态标注；无书面记录的"口头通过"无效。

### 5.5 验收证据规范

- **四要素**：每条证据包含「执行步骤或命令 / 结果 / 时间 / 执行人与环境」。
- **证据类型**：CI 测试输出；审计查询结果（SQL / 页面）；SSE 抓包；对话与页面截图/录屏；Playwright trace/video；安全测试报告；场景验收单；run-summary；验收纪要。
- **归档位置**：`docs/plan/evidence/bot-<b0|b1|b2|b3|accepted>/`；大文件（video/trace）走 CI artifact 并在目录内留链接；与 MCP 共用设施的证据（Playwright/E2E）允许在双方目录内互链。
- **命名规范**：`<验收项编号>-<简述>-<yyyyMMdd>.<ext>`（如 `AB1-10-confirm-e2e-20261120.md`）。
- **审核要求**：QA 对自动化证据抽检 ≥30% 可复跑；安全类验收（AB2-05、AB3-04、AB3-05）100% 人工复核。
- **反模式（直接判未达）**：用截图代替自动化断言；用 checkbox 或"设计完成"代替实现与测试；证据与版本不符（非当前 HEAD 产出）；以"预览通过"冒充 dry-run 零写入。

---

## 6. 测试与质量保障计划

### 6.1 测试分层与设施

| 层 | 设施 | 说明 |
| --- | --- | --- |
| UT（后端） | Go `testing`；既有 `handlers/ai/` 用例（`handler_test.go`、`service_rbac_test.go` 等） | 新增元数据/幂等/脱敏/状态机用例 |
| 契约测试 | 既有范式 `handlers/ai/tool_execution_contract_test.go:19-56` | dry-run 零写入、拒绝回填、参数冻结断言 |
| 集成（IT） | 真实 ent + SQLite/Postgres 双驱动【差异未核实】 | 迁移、run/step/event、队列恢复、授权矩阵 |
| 前端组件 | Jest + jsdom（`itsm-frontend/package.json:18,112`） | 抽屉/证据面板/状态条/管理页/launcher |
| E2E（browser） | Playwright（`:24`，flows/business 分项目；与 MCP M2-05 共用设施） | 工作区主路径 + 三入口 + 三场景 pilot |
| E2E（api） | Go 契约 + 脚本化 API 流 | 确认→执行→verify、队列重启、SSE 抓包 |
| 安全负向 | 角色/租户矩阵 + 注入用例 | BT-08；黑名单与覆写审计 |
| 依赖替身 | mock provider（多 provider 方案既有策略）+ mock MCP server（MCP M0-13） | 全量 mock + 少量 real smoke |

### 6.2 CI 门禁与合入策略（建议）

1. 每次提交：`go build ./...`、受影响包 `go test`、前端 `npm test`（组件）；lint（golangci-lint / eslint）。
2. 每个 PR：至少过 BT-01/BT-03/BT-04 快集；涉及迁移/事件的 PR 必须附联合评审记录（S1/S2）。
3. CI 全量：BT-02/BT-05～BT-08 在 CI 常态执行（可用内存 DB / mock）。
4. Nightly：BT-07 与 BT-09 的 E2E 通道（mock provider），失败自动开缺陷。
5. 里程碑出口：全量回归 + run-summary 产出 + 证据归档检查。
6. 合入纪律：单 PR 对应单一任务卡；**合并点变更（迁移字段/事件契约/确认状态机/授权交集）双线同 PR 或紧邻合并**，禁止单边先合后改。

### 6.3 环境与数据矩阵

- **Provider**：mock 全量回归 + 每里程碑 1 组 real smoke（低成本模型）；非工具调用 provider 必测降级横幅（BQ5）。
- **数据库**：SQLite（本地/CI 快）与 Postgres（CI/预发）双驱动；迁移差异未核实项必须在 CI 覆盖（同 MCP U-2）。
- **租户 × 角色**：2 租户 × 3 角色（管理员 / 普通坐席 / 无 `ai:write` 用户）；复用 MCP P7 矩阵。
- **浏览器**：沿用前端 Playwright 既有浏览器配置，不新增浏览器维度。

### 6.4 缺陷管理与回归

- 缺陷分级：P0（安全/数据错误/主链路不可用）→ 阻断；P1（功能缺陷/审计缺失）→ 修复或豁免；P2（体验）→ 排期。
- 回归集 = 既有自动化（后端 `handlers/ai/`、前端 Jest）+ 本方案全部 BT 用例 + 既往 AB 项对应用例。
- 每次里程碑出口跑全量回归；P0 未清零不得出口；P1 豁免需书面登记（§5.4-3）。
---

## 7. 风险登记（含实施期新增）

> 来源：阶段一报告 §7 开放问题（1–11）+ 双线协同新增。概率/影响口径：高/中/低。

| # | 风险 | 影响 | 应对 / 缓解 | 触发信号 | 责任 |
| --- | --- | --- | --- | --- | --- |
| R-B01 | 写工具下发模式未拍板（阶段一报告 §7-1，阻塞 B1 设计） | B1 设计定稿延期或返工 | BQ1 折中方案（可被请求但立即 `confirmation_required`、不进执行分支）先行评审 | BQ1 超期未决 | 产品 + 安全 |
| R-B02 | 四眼原则现状未知【未核实】（§7-2） | 自审自批风险 | BQ2 拍板；`act_high` 默认强制分离（配置化） | 安全评审发现自批案例 | 安全 |
| R-B03 | 队列持久化选型未定（§7-3）【MQ/Redis 有无未核实】 | 重启丢任务、双线重复建设 | BQ3 + BP4：DB 轮询起步 + 抽象接口；与 MCP M1-02 共用（S6） | spike 不通过或出现两套队列 | 后端 |
| R-B04 | 多租户红线（§7-4） | 跨租户数据泄露 | 所有新增表索引首列含 `tenant_id`；全查询强制租户条件；隔离测试（BT-08） | 审计/渗透发现越租户 | 后端 |
| R-B05 | 模型能力差异（§7-5；当前仅 OpenAI 兼容 provider 实现 `ChatStreamWithTools`） | 非工具调用 provider 下体验误导 | BQ5 显式横幅 + 沿用能力探测闸门（`handlers/ai/service.go:477-484`） | 用户反馈“工具不可用” | 前端 |
| R-B06 | 事件表膨胀（§7-6） | 存储与查询性能退化 | BQ4：90 天热存 + 归档任务；表体积指标监控 | 表体积超阈值 | SRE |
| R-B07 | 工作树未提交改动（§7-7） | 分支混提/覆盖 | BP1 与 MCP P1 同批处置 | 开工前未清 | 研发负责人 |
| R-B08 | dry-run 语义边界（§7-8） | 用户预期落差 | BQ7 文案 + 失败回填 + UI 提示“预览不保证成功” | 预览与实际不一致投诉 | 产品 |
| R-B09 | 前端 SSE 兼容（§7-9） | 旧客户端断流 | 未知事件忽略 + 旧事件保持可用 + CSRF/降级逻辑复测 | 客户端异常日志 | 前端 |
| R-B10 | E2E 环境与成本（§7-10） | 验收受阻、真实 provider 成本 | mock 全量 + 少量 smoke；复用 MCP M2-05 设施（S5） | nightly 不稳定 | QA |
| R-B11 | 文档治理（§7-11） | 状态虚标、checkbox 冒充交付 | 五级状态 + 证据规范 + 出口评审（§5.4/§5.5） | 抽查不符 | QA |
| R-B12 | 双线迁移窗口错配（新增） | 二次迁移、审计缺口 | 联合窗口 1 书面确认（S1）；字段清单联合冻结 | 排期漂移 | 双线负责人 |
| R-B13 | 高冲突文件并发改动（新增；`handlers/ai/service.go`、`components/ai/AIChat.tsx`、路由/菜单文件被双线同时触碰） | 合并冲突、回归增多 | 合并顺序表 + 小步 PR + 每日同步；同一文件的双线任务错峰排期 | 冲突频繁/回归失败 | 双线负责人 |
| R-B14 | 事件契约双线漂移（新增） | 双端解析不一致、抓包失败 | 单一事件注册表（S2）+ 双线联合评审 | 注册表分叉 | 架构 |
| R-B15 | 授权模型放大权限面（新增） | 越权调用 | 安全评审 + 默认拒绝 + 影响面预览 + 黑名单（BT-08） | 评审/渗透发现 | 安全 |

---

## 8. 回滚与降级预案

### 8.1 灰度与开关层级（四级，自上而下）

| 层级 | 开关 / 操作 | 作用面 | 生效方式 | 回退目标 |
| --- | --- | --- | --- | --- |
| L1 | `bot.enabled=false` | 全局：Bot 运行时、run/事件、抽屉、launcher 全部停用 | 配置（按现有 AI 开关范式，重启或热更新） | 行为 = 现状（聊天 + 审批挂起） |
| L1.5 | `bot.confirm.enforce=false`（建议新增） | 写路径：退回“审批挂起 + 外置审批页”，不启用对话内确认 | 配置 | 现状写审批 |
| L2 | 模板 `status` 降级（ga→pilot→paused） | 单个 Bot：paused = 入口隐藏、新会话不可选、存量会话只读 | 管理页操作（管理 API） | 单 Bot 停用 |
| L3 | `bot_tool_grants` 回收 / 工具 quarantine | 单个工具：立即不可见、不可调用 | 管理页操作 / 紧急脚本 | 工具面收缩 |

> 补充：L1 关闭时新增表/字段保持存在（纯增量、前向兼容，不回退 DB）；SSE 自动回退 v1 事件集；已产生的 run/event/artifact 保留供审计。

### 8.2 发布与回滚步骤

1. **预发**：B0/B1 全量回归 + E2E + run-summary；迁移先跑一次真实旧库升级路径。
2. **灰度**：以 pilot 模板 + 指定租户白名单启用（`bot.enabled=true` 但模板/入口/授权白名单受限）。
3. **观察**：run 成功率、确认通过/拒绝/过期率、verify 失败率、错误码分布；≥1 周或按团队标准。
4. **全量**：模板升 `ga`；指标阈值告警接入；文档状态回写（B4-03）。
5. **回滚顺序**：L3（收授权）→ L2（停 Bot）→ L1.5（停确认、退回审批）→ L1（全关）；每步观察 5–10 分钟并记录。
6. **数据可逆性**：新增表/字段为纯增量；**代码回滚无需 DB 回滚**；回滚期间产生的会话不影响后续 v2 解析（事件已落库，不依赖客户端）。
7. **回滚后复盘**：事故单 + §10 登记 + 触发条件分析（写入下一迭代评审）。

### 8.3 故障降级矩阵

| 故障 | 降级动作 | 用户可见影响 | 恢复条件 |
| --- | --- | --- | --- |
| RunManager / 事件写入故障 | 降级“直答模式”（保留消息与工具调用审计；暂停 run/artifact 新写入） | 无状态条/证据面板；对话可用 | DB 恢复后自动切回 |
| SSE v2 通道故障 | 回退 v1 事件集（`delta/sources/done/error`） | 无时间线/确认事件推送；改用审批页 | 通道恢复 |
| 队列（持久化）故障 | 暂停写路径（读与查询可用）；写请求明确提示暂不可用 | 无法发起写动作 | 队列恢复并完成恢复扫描 |
| 确认 / 授权服务故障 | fail-closed：拒绝写工具下发与执行 | 只读可用 | 服务恢复 |
| 脱敏引擎故障 | 阻断写路径与审计写入（不得原文落库） | 写不可用 | 引擎恢复 |
| provider 故障 / 不支持工具 | 提示 + 普通问答降级（横幅） | 工具能力不可用 | provider 恢复 |

### 8.4 应急操作手册

- **停止所有写路径（分钟级）**：`bot.confirm.enforce=false` + 暂停队列消费；必要时 `bot.enabled=false`。
- **停用单个 Bot**：模板 `status=paused`（入口隐藏，存量会话只读）。
- **回收单个工具**：删除对应 `bot_tool_grants` 记录（立即生效）或工具 quarantine。
- **审计导出**：按 `run_id` / `tenant_id` 联查 `tool_invocations` × `bot_events` 并导出留档。
- **演练要求**：accepted 前至少完成一次“确认闭环失效 → 退回审批”与一次“停止写路径”桌面演练，记录归档 `docs/plan/evidence/bot-accepted/`。
- **记录**：所有应急操作登记事故单与本文件 §10 决策日志。
---

## 9. 交付物清单

> 形态说明：代码与配置随 PR 合并；文档与证据归档入库；大文件（trace/video）走 CI artifact 并在证据目录内留链接。

| # | 类别 | 交付物 | 产出任务 |
| --- | --- | --- | --- |
| 1 | 文档 | 本实施方案；阶段一报告 / MCP 方案互链与状态回写 | 全流程、B4-03 |
| 2 | 文档 | run-summary 模板、场景验收单模板、指标说明与运维手册 | B1-10、B4-01、B4-02、B4-04 |
| 3 | 后端 | `service/bot/` 模块（BotPolicy / RunManager / ScopeResolver / Redactor / Confirmation / Idempotency；路径以 BP5 评审为准） | B0-05、B0-06、B1-02、B2-02、B3-01 |
| 4 | 后端 | ent schema：`bot_runs` / `bot_steps` / `bot_events` / `bot_artifacts` / `bot_templates` / `bot_tool_grants` + `tool_invocations` 扩展 | B0-02、B1-01、B2-01、B3-06 |
| 5 | 后端 | SSE 事件注册表与兼容层；队列持久化与恢复；审计 / 脱敏 / 幂等设施 | B0-03～B0-06、B1-03、B1-06 |
| 6 | 前端 | `ConfirmationDrawer` / `EvidencePanel` / `RunStatusBar` / `AskAILauncher` / `BotSelector` 组件 + AIChat 接入 + 审批页增强 + 管理页 | B1-07～B1-09、B2-03、B2-04、B3-02 |
| 7 | 测试 | BT-01～BT-09 用例集（UT / 契约 / IT / 组件 / E2E / 安全负向）+ mock provider 配置 | 全流程、B4-01 |
| 8 | 运维 | 指标看板、告警规则、应急操作手册、演练记录 | B4-02、B4-04、§8.4 |
| 9 | 证据 | `docs/plan/evidence/bot-{b0,b1,b2,b3,accepted}/` 归档 | 各里程碑验收任务 |
| 10 | 配置 | `bot.enabled`（+ 建议 `bot.confirm.enforce`）开关与预算护栏参数 | BP5、BP8、§8.1 |
---

## 10. 决策日志与变更控制

**已冻结登记（初始化）**：BD1–BD10（§1.2）自本方案发布起视为冻结（依据：阶段一报告 §5/§6、MCP 方案 D9/§11.3）。任何偏离按下方变更控制规则处理。

**决策项（2026-09-27 已按建议拍板）**：BQ1–BQ8 登记如下（依据项目负责人「按建议执行」指示）；如产品/安全在里程碑出口评审提出异议，按下方变更控制规则处理。

| 日期 | 决策项 | 结论 | 拍板人 | 备注 |
| --- | --- | --- | --- | --- |
| 2026-09-27 | BQ1 写工具下发模式 | 已拍板：折中——可被请求但立即 `confirmation_required`，不进执行分支 | 项目负责人（按建议执行） | 阻塞 B1 设计定稿 |
| 2026-09-27 | BQ2 四眼原则 | 已拍板：`act_high` 强制分离（配置化） | 项目负责人（按建议执行） | 影响 B1-05/B1-09 验收 |
| 2026-09-27 | BQ3 队列持久化选型 | 已拍板：DB 表轮询 + 抽象接口（与 MCP 共用） | 项目负责人（按建议执行） | 阻塞 B1-06 |
| 2026-09-27 | BQ4 事件保留策略 | 已拍板：`bot_events` 90 天热存 + 归档；runs/steps 180 天 | 项目负责人（按建议执行） | 阻塞 B1-01 模型冻结 |
| 2026-09-27 | BQ5 模型能力降级提示 | 已拍板：非工具 provider 显式横幅 | 项目负责人（按建议执行） | 阻塞 B1-04 |
| 2026-09-27 | BQ6 会话标题改动并入 | 已拍板并执行：独立提交 `d3471221`（`feat/vite-migration`） | 项目负责人（按建议执行） | 与 MCP P1 同批处置 |
| 2026-09-27 | BQ7 dry-run 语义边界 | 已拍板：文档 + UI 双写提示，失败走错误回填 | 项目负责人（按建议执行） | 阻塞 B0-04 |
| 2026-09-27 | BQ8 双线排期与迁移窗口 | 已拍板：B0-02 与 M0-03 同窗口；B1 与 M1 同迭代 | 项目负责人（按建议执行） | 阻塞 B0-02 |

**实施期偏差与结论登记**（2026-09-27 起；口径同 MCP 方案 §10）：

| 日期 | 变更项 | 结论 / 原因 | 影响 | 批准人 |
| --- | --- | --- | --- | --- |
| 2026-09-27 | §3.3 无「状态列」而 §1.4/§5.1(D-6) 要求回写状态列 | 回写形式改为「§3.3.1 状态速览表 + 任务卡状态段」（与 MCP 方案 §3.3.1 同范式） | 仅文档形式，不改验收口径 | AI 辅助执行（待团队评审追认） |
| 2026-09-27 | U-B8 Bot 运行时模块归属（`service/bot/` vs `handlers/ai/bot/`） | 结论：`service/bot/`——理由：与 MCP `mcp/` 模块同构（域逻辑在 service 包），`handlers/ai` 只做编排与 HTTP；避免 handlers 反向承载运行时 | BP5 落地位置固定；后续 B1/B2/B3 组件在此包内实现 | AI 辅助执行（待团队评审追认） |

**变更控制**：实施期任何偏离本方案（范围、设计、验收级别、里程碑顺序、合并点）必须在本表新增记录（日期 / 变更项 / 原因 / 影响 / 批准人），并同步回写阶段一报告对应章节；**凡涉及 §11.3 合并点的变更，必须同一评审同步修改 MCP 方案对应章节（§1.4 联动变更规则）**；未登记的偏离在里程碑验收时一律不认可。

---

## 11. 附录

### 11.1 验收项 ↔ 任务 ↔ 测试组对照（速查）

| 验收项 | 任务 | 测试组 | 验收项 | 任务 | 测试组 |
| --- | --- | --- | --- | --- | --- |
| AB0-01 | B0-01 | BT-01 | AB2-03 | B2-03 | BT-09 |
| AB0-02 | B0-02 | BT-02 | AB2-04 | B2-04 | BT-09 |
| AB0-03 | B0-03 | BT-04 | AB2-05 | B2-05 | BT-08 |
| AB0-04 | B0-04 | BT-03 | AB2-06 | B2-06 | BT-08 |
| AB0-05 | B0-05 | BT-04 | AB3-01 | B3-01 | BT-08 |
| AB0-06 | B0-06 | BT-04 | AB3-02 | B3-02 | BT-09 |
| AB0-07 | B0-07 | —（证据评审） | AB3-03 | B3-03 | BT-09 |
| AB1-01 | B1-01 | BT-05 | AB3-04 | B3-04 | BT-09 |
| AB1-02 | B1-02 | BT-05 | AB3-05 | B3-05 | BT-09 |
| AB1-03 | B1-03 | BT-05 | AB3-06 | B3-06 | BT-09 |
| AB1-04 | B1-04 | BT-05 | AB3-07 | B3-07 | —（证据评审） |
| AB1-05 | B1-05 | BT-06 | AB4-01 | B4-01 | BT-09 |
| AB1-06 | B1-06 | BT-07 | AB4-02 | B4-02 | —（指标断言） |
| AB1-07 | B1-07 | BT-09 | AB4-03 | B4-03 | —（治理检查） |
| AB1-08 | B1-08 | BT-09 | AB4-04 | B4-04 | —（出口评审） |
| AB1-09 | B1-09 | BT-09 | — | — | — |
| AB1-10 | B1-10 | BT-09 | — | — | — |
| AB2-01 | B2-01 | BT-08 | — | — | — |
| AB2-02 | B2-02 | BT-01 / BT-08 | — | — | — |

### 11.2 关键锚点清单

> 以下锚点均引自阶段一报告 §3/§8 与 MCP 方案 §11.2 的**已核实清单**（基线 `7442fad5`，2026-09-27）；施工前以现状复核为准。MCP 侧锚点（迁移窗口 / 事件 / 测试设施）以 MCP 方案 §11.2 为准，本表不重复。

| 领域 | 锚点（file:line） | 用途 |
| --- | --- | --- |
| 聊天与工具循环 | `itsm-backend/handlers/ai/service.go:447-616` | `chatStream` 主链路（B1-01/B1-03 改造点） |
| 工具执行与审批 | `itsm-backend/handlers/ai/service.go:118-257` | `ExecuteTool`/`recordToolAudit`/`ApproveTool`（B0-03/B1-05 改造点） |
| 写工具白名单 | `itsm-backend/handlers/ai/service.go:385-398` | 迁移为授权兼容默认（B2-02） |
| 工具面装载 / 回填 | `itsm-backend/handlers/ai/service.go:485-504`、`:506-530` | `FilterTools` 与 `confirmation_required` 接入点 |
| 消息持久化 | `itsm-backend/handlers/ai/service.go:554-613` | 会话/消息落库（B1-01 run 关联） |
| 工具注册表 | `itsm-backend/service/tool_registry.go:14-22`、`:126-380` | B0-01 元数据扩展（14 工具） |
| 工具队列 | `itsm-backend/service/tool_queue.go:30-130`（满队列 `:45-50`） | B1-06 持久化与恢复 |
| 契约测试范式 | `itsm-backend/handlers/ai/tool_execution_contract_test.go:19-56` | dry-run 零写入（B0-04） |
| ent schema | `itsm-backend/ent/schema/tool_invocation.go:14-43`（兼容注释 `:31`） | B0-02 字段扩展 |
| 实体结构 | `itsm-backend/handlers/ai/entity.go:7-48` | Conversation/Message/ToolInvocation |
| LLM 网关 | `itsm-backend/service/llm_gateway.go:81-89,357-408,512-565`；`service/llm_providers.go:150` | 工具调用能力探测（BQ5 降级） |
| 权限定义 | `itsm-backend/internal/authz/catalog.go:161-162`；`internal/authz/roles.go:38-148,252-254,425-427` | `ai:read` / `ai:write`（BD8） |
| 工具 RBAC | `itsm-backend/handlers/ai/handler.go:50`、`handlers/ai/service.go:139` | Gate2 域权限校验 |
| Skill 边界 | `itsm-backend/handlers/ai/builtin_skills.go:25-37`；`internal/bootstrap/app.go:1015-1021`；`router/router.go:579-591` | BD7 分工与 promote 范式（B2 参考） |
| 迁移入口 | `itsm-backend/internal/bootstrap/app.go:1392`（前置 `:1380-1391`） | B0-02 联合迁移 |
| 前端 SSE | `itsm-frontend/src/lib/api/ai-api.ts:521-660`、`:862-868` | B1-04 解析升级（兼容复测） |
| 工作区 | `itsm-frontend/src/components/ai/AIChat.tsx:3-20` | B1-07/B1-08/B2-04 接入点 |
| AI 审批/审计页 | `itsm-frontend/src/pages/(main)/ai/approval/index.tsx`、`pages/(main)/ai/audit/index.tsx` | B1-09 增强 |
| 路由与菜单 | `itsm-frontend/src/lib/router/route-config.ts:415-438` | `/ai/chat` 入口（B2-03 挂载参照） |
| 前端测试设施 | `itsm-frontend/package.json:18,24,112` | Jest / jsdom / Playwright（§6.1） |
| 治理纪律 | `plans/README.md:5` | 状态纪律（§1.4 / §5.4） |

**分析报告已核实、本方案直接引用的其余锚点**：`handlers/ai/service.go:477-484`（provider 能力闸门）；`llm_provider_admin_service.go:818`（错误文本脱敏范式）；`ROADMAP.md:146-159`（human-in-the-loop triage）。以上若与实施时现状不符，以实施时核对为准并更新本表。
### 11.3 与 MCP 方案的合并点（互逆视图）

> MCP 方案 §11.3 从 MCP 视角列出映射；本表从 Bot 视角给出生效规则与违约后果。**任一合并点变更须双线同批评审与合并**（§1.4 联动变更规则）。

| # | 协同项 | Bot 侧 | MCP 侧 | 联合动作 | 未协同的后果 |
| --- | --- | --- | --- | --- | --- |
| 1 | 迁移 | B0-02 | M0-03 | 同窗口、同字段清单、单 PR：`tool_invocations` 一次加列（运行态 + 风险 + 目标 + 幂等 + 脱敏 + 过期 + verify，与 MCP provider 字段合并）；`ToolDefinition` 元数据标注同批 | 二次迁移、审计缺口（MCP R-08） |
| 2 | 事件契约 | B1-01 / B1-03 | M1-03 | 单一事件注册表：v2 事件 + MCP 工具事件；未知事件忽略策略统一；双线评审 | 事件漂移、抓包/回放不一致 |
| 3 | 确认闭环 | B1-05 / B1-07 / B1-09 | M1-02 / M1-05 | 共用确认状态机与确认卡片；MCP 写工具直接复用；MCP 未落时按其 R2 退化（待审批提示 + 外置审批） | 两套确认 UX、参数冻结语义不一致 |
| 4 | 授权与风险 | B2-02 | M1-01 / M2-03 | Bot 授权面覆盖内置 + MCP 工具；risk 上限取双线标注；工具面预算叠加 | 权限面不一致、外部工具绕过门禁 |
| 5 | 队列 | B1-06 | M1-02 | 单一持久化队列实现（禁止两套）；恢复 / 503 / verify 语义一致 | 双队列、恢复语义分裂 |
| 6 | 页面与 E2E | B3-02 / B4-01 | M2-05 | 复用 launcher 范式与 Playwright 设施；三入口与 MCP 管理页共用测试工程 | E2E 重复建设、覆盖缺口 |
| 7 | 验收与回写 | B4-03 | M2-07 | 五级状态与 run-summary 证据口径一致；联合 `accepted` 评审 | 口径分裂、状态虚标 |

**联合评审点**（与 §3.4 对应）：① 迁移窗口（B0-02 × M0-03）——字段清单冻结签署；② 事件/队列（B1-03 / B1-06 × M1-03 / M1-02）——注册表与队列实现评审；③ 联合验收（B4-01～B4-04 × M2-05～M2-07）——E2E 与 `accepted` 联签。

### 11.4 未核实项与诚实声明

| # | 项 | 状态 | 处理 |
| --- | --- | --- | --- |
| U-B1 | 队列持久化选型；仓库是否已有 Redis / MQ【未核实】 | 未定 | BP4 spike（B1-05 前）；结论登记 §10 |
| U-B2 | 是否允许自审自批（四眼原则现状）【未核实】 | 待核对 | BQ2；实施时核对审批代码与角色配置 |
| U-B3 | SQLite 与 Postgres 迁移行为差异【未核实】 | 未定 | 同 MCP U-2：CI 双驱动（AB0-02 硬性） |
| U-B4 | CI 是否具备 Postgres / Docker 环境【未核实】 | 未定 | 同 MCP U-3；过渡方案见 §6.2 |
| U-B5 | 真实 provider 工具调用覆盖（当前仅 OpenAI 兼容实现 `ChatStreamWithTools`） | 部分已知 | 实施时逐 provider 探测；UI 降级横幅（BQ5） |
| U-B6 | 阶段一与 MCP 双线的实际排期 | 未定 | BQ8；决定 S1–S7 的生效形态 |
| U-B7 | 旧客户端 / 部署形态对 SSE 事件的实际依赖面【未核实】 | 未定 | BP3 评审以抓包与客户端清单确认 |
| U-B8 | Bot 运行时模块归属（`service/bot/` vs `handlers/ai/bot/`） | **已闭环（2026-09-27）**：结论 `service/bot/`（BP5 落地；登记见 §10） | 评审追认 |

**声明**：本文档为草案（draft），所有任务与验收均为**目标**；**编制时**未改动任何生产代码、未编译、未运行测试（实施期交付与实测记录见 §3.3.1、§5.2 判定与 `docs/plan/evidence/bot-b0/`）；任务规模与工作量估算为经验值，需团队复核后据此排期。ITSM 在编制时**无 Bot 运行时**（run/step/event、模板/授权、确认抽屉等均不存在，结论沿用阶段一报告 §3）；截至 2026-09-27 已落地 BP5 骨架与 B0-01 工具元数据（同上证据目录），其余仍按任务卡推进。本文档与阶段一报告、MCP 方案建立互链（详见变更记录），互链回写不改变既有结论。

---

## 变更记录

| 日期 | 作者 | 变更 |
| --- | --- | --- |
| 2026-09-27 | AI 辅助编制 | 初稿：基于《ITSM Bot 能力落地分析》（48,468 字节）与《ITSM 外部工具（MCP）接入实施方案》（86,733 字节），产出 B0–B4 共 34 个任务卡、34 条验收项（AB0×7 / AB1×10 / AB2×6 / AB3×7 / AB4×4）、9 组测试用例（BT-01…BT-09）、四级开关回滚预案与风险/决策登记；与 MCP 方案建立 7 项合并点与 3 个联合评审窗口；基线 HEAD `7442fad5`，未改动代码 |
| 2026-09-27 | AI 辅助编制 | 开工准备：BQ1–BQ8 按建议拍板登记（§10）；BP1 工作树处置完成（独立提交 `d3471221`）；实施分支 `feat/bot-mcp-integration` 创建与文档入库（`3172c12c`） |
| 2026-09-27 | AI 辅助执行 | 联合迁移窗口执行：B0-02 的 `tool_invocations` 字段随 MCP M0-03 一次加列（避免二次迁移）；命名统一 `input_redacted`→`args_redacted`；证据 `docs/plan/evidence/mcp-m0/M0-03-migration-evidence.md`。 |
| 2026-09-27 | AI 辅助执行 | **BP5 落地**：`service/bot/`（`doc.go`/`gate.go`）模块骨架 + `bot.enabled` 全局开关（默认 false、`BOT_ENABLED` 兜底；`config.yaml.example` 同步）；关闭态无装配点=零行为变化。**U-B8 归属结论 `service/bot/`**（§10/§11.4 已回写）。证据 `docs/plan/evidence/bot-b0/BP5-B0-01-unit-evidence.md`。 |
| 2026-09-27 | AI 辅助执行 | **B0-01 交付（`unit_verified`）**：`ToolDefinition` 扩展 6 个元数据字段 + 常量 + `NormalizeToolMetadata`（缺失 → `act_high`+`strict` 且 `annotated=false`，工具面**不默认下发**）；14/14 内置工具按冻结风险矩阵逐条标注（写 6 / 读 8）；调用时快照 `Risk/Category` 落 `tool_invocations`（内置取注册表、MCP 取治理标注）；守卫测试 `service/tool_metadata_test.go` + 快照往返 `handlers/ai/repository_metadata_snapshot_test.go`。§2.3 BP5 项、§3.3.1 状态速览表、任务卡状态段已回写；`go build ./...` exit 0、gofumpt 无输出。 |
