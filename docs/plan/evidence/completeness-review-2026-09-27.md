# ITSM 外部工具（MCP）接入——功能完整度确认（草案）

> 文档类型：完整度确认（能力矩阵 × 证据 × 缺口）
> Status: draft（**未签署**；M2-07 的 accepted 复核表另行归档 `docs/plan/evidence/mcp-accepted/`）
> 编制日期：2026-09-27
> 适用范围：一期 MCP 接入（M0 只读骨架 / M1 写治理与用户侧 / M2 加固与 E2E）的能力完整度确认
> 目标读者：研发、测试、运维、评审人
> 关联文档：`docs/plan/itsm-mcp-external-tool-integration-analysis-2026-09-27.md`（分析报告 §7.2 完整度矩阵为基线）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（§3.3.1 状态速览 / §5.2.1 判定表 / §10 豁免登记）、`docs/plan/evidence/mcp-m1/gap-register-2026-09-27.md`（缺口台账）
> 核查基线：分析报告 §7.2 完整度矩阵（22 项能力，M0/M1/M2 三档目标）
> 核查方式：静态证据核对（28 份证据文档 + 方案 §3.3.1/§5.2.1）+ **本轮实测复现**（后端 14 包、前端 7 套件）；未编译/未运行的项一律标注【未核实】
> 状态口径：五级不可跳跃 `implemented → unit_verified → integration_verified → flow_verified → accepted`；本表「现状」取已实际验证的最高级别

## 1. 结论摘要

1. **一期功能面已全部落地**：M0（14/14）与 M1（10/10）任务全部交付，M2 中 M2-03/M2-06 已交付；**不存在"设计有、代码无"的能力项**（唯一例外是按决策明确推期的 M2-01/02/04）。
2. **验证级别分层清楚**：后端能力普遍达 `integration_verified`，用户侧闭环（写审批、时间线、审批页/审计页）达 `flow_verified`；**未达 `accepted` 的原因不是功能缺失，而是三类外部依赖未闭环**：①Postgres 实跑（CI）、②浏览器真实栈执行（CI）、③M2-07 出口评审与签署（人工）。
3. **统计**：任务 31 项 → 完成 26 / 部分 1（M2-05）/ 未开始 4（M2-01/02/04 属决策推期，M2-07 属收口）；验收项 32 项 → 达成目标级别 26 / 未完成 1（A2-05）/ 未开始 5（A2-01/02/04 推期 + A2-07/08 收口）。
4. **硬门槛**（分析报告 §7.4 四项）：命名投影契约 ✅、SSRF 防护 ✅、审计三元组 ✅ 均已过；**浏览器 E2E**（第四项）用例与 CI 作业已就绪，待首绿。

## 2. 能力完整度矩阵（对照分析报告 §7.2）

| # | 能力项 | M0/M1/M2 目标 | 现状（已实际验证的最高级别） | 关键证据 | 缺口 |
| --- | --- | --- | --- | --- | --- |
| 1 | Streamable HTTP 传输 | unit / integration / flow | `integration_verified` | M0-04 unit + M0-14 端到端 + M1-10 失败注入；本机 `tests/mcpintegration` ok 25.1s | M2 的浏览器 flow 待 CI 首绿 |
| 2 | SSE 传输 | unit / integration / flow | `integration_verified` | M0-04 unit + M1-03 契约与旧客户端兼容 | 同上 |
| 3 | stdio（平台级） | — / unit / integration | **未开始（决策：一期不做）** | 方案 M2-01（依赖 Q1：旗舰私有化再启用） | 决策推期，非缺陷 |
| 4 | 连接生命周期（建连/断开/重连/退避） | unit / integration / flow | `integration_verified` | M0-07（状态机/退避/差分/隔离） | flow 归 M2（浏览器观察连接态） |
| 5 | 命名投影 + quarantine | **unit（最高优先）** / integration / flow | `integration_verified` | M0-02 契约（唯一/重名/非法/超长/遮蔽）+ M1-09（碰撞后到者隔离）+ M0-14 跨租户 fail-closed | — |
| 6 | 工具缓存 + schema_hash 变更隔离 | unit / integration / flow | `integration_verified` | M0-07 差分与 quarantine 复核流程 | — |
| 7 | 管理 CRUD + 审计 | unit / integration / flow | `integration_verified` | M0-08（202+轮询、乐观锁、审计留痕） | — |
| 8 | 测试连接 | unit / integration / flow | `integration_verified` | M0-08 + M1-10（超时/401/TLS/协议不匹配 4 类） | — |
| 9 | 启停/热重载（异步） | unit / integration / flow | `integration_verified` | M0-08（无 60s 阻塞；状态回读） | — |
| 10 | 工具治理（三态/批量/分类标注） | unit / integration / flow | `integration_verified` | M0-08/M0-09 + M1-01（read_only/risk/category 全链路） | — |
| 11 | 凭据加密/轮换 | unit / integration / flow | `integration_verified` | M0-06（AES-GCM/掩码/轮换）+ M0-08/M0-14 轮换重连 | — |
| 12 | OAuth 2.1（PKCE/发现/注册） | — / — / unit→integration | **未开始（二期）** | 方案 M2-02（依赖 Q6） | 决策推期 |
| 13 | 权限位（mcp:read/write/admin） | unit / integration / flow | `integration_verified` | M0-10（角色矩阵 + 跨租户 fail-closed） | — |
| 14 | Gate2/Gate3 接入 | unit / integration / flow | `flow_verified` | M1-02（参数冻结、不重试、拒绝回填）+ M1-10 | — |
| 15 | 审计元数据（provider/三元组） | unit / integration / flow | `flow_verified` | M0-11 后端三元组 + M1-07 用户可见来源维度 | — |
| 16 | SSE 事件（工具调用/待审批） | — / unit / flow | `integration_verified` | M1-03 契约（含旧客户端兼容） | M2 的浏览器 flow 待 CI |
| 17 | AIChat 工具时间线 | — / unit / flow | `flow_verified` | M1-04 四态组件 + M1-10 前端交互；浏览器断言见 M2-05 | 浏览器级待 CI 首绿 |
| 18 | 写工具确认/审批展示 | — / unit / flow | `flow_verified` | M1-05 卡片与跳转 + M1-06 来源增强 + M1-10 | 手工/写工具浏览器断言归 M2-05 |
| 19 | 管理页（服务器/治理/健康） | — / unit / flow | `unit_verified` | M0-12 单测（页面/交互/守卫） | **浏览器 E2E 用例已就绪，待 CI 首绿**（M2-05） |
| 20 | 审计页来源维度 | — / unit / flow | `flow_verified` | M1-07（后端跨状态断言 + 页面组件） | 浏览器级归 M2-05 |
| 21 | SSRF/输出上限/脱敏 | unit / integration / flow | `integration_verified` | M0-05 表驱动 12 例 + M1-09 负向 12+2 | — |
| 22 | 指标/健康摘要/告警 | — / unit / integration | `integration_verified` | M2-06（10 指标 + 10 告警规则 + runbook + 演练自动化复现） | 管理页点击的人工演练归 M2-05/M2-07 |

**矩阵外的新增交付**（分析报告未列，属本方案增量）：

| 能力 | 现状 | 证据 |
| --- | --- | --- |
| 工具面预算与元工具评估 | `unit_verified` | M2-03（`mcp/budget` 11 例 + manager 5 例 + `Advise` 产物） |
| 确定性 LLM 替身（E2E 驱动） | `unit_verified` | `service/llm_mock_provider.go` 8 例；双条件启用 |
| CI 专项门禁 | 已接线（待首绿） | `mcp-race`（阻断）、`mcp-postgres-migrations`、`e2e-mcp`（首轮观察） |
| 运维手册与告警规则 | 已交付 | `docs/ops/mcp-runbook.md`、`docs/ops/mcp-alert-rules.yml`（10 条） |

## 3. 本轮实测复现（2026-09-27）

**后端**：`go test ./mcp/... ./tests/mcpintegration/ ./handlers/mcp/ ./metrics/ ./config/ ./internal/authz/ ./middleware/ -count=1 -timeout 25m` → **exit 0，14 包全 ok**：admin 22.6s / budget 0.8s / client 7.1s / manager 3.9s / provider 18.5s / registry 0.6s / testutil·mockserver 5.6s / transport 1.1s / **mcpintegration 25.1s** / handlers·mcp 7.8s / metrics 0.4s / config 0.8s / authz 0.3s / middleware 9.6s。

**前端**：`npx jest --testPathPattern "(mcp|tool-approval-card|tool-call-timeline|ai/approval|ai/audit)"` → **7 套件 / 49 用例全部 PASS**（mcp-helpers、mcp-api、tool-call-timeline、tool-approval-card、ai/audit、ai/approval、admin/mcp-servers 页面）。
> ⚠️ 该命令**退出码为 1**，原因不是用例失败，而是 `jest.config.js:43` 的 `collectCoverage: true` + `coverageThreshold.global`（branches 64.5 / functions 80 / lines 80）在**只跑子集**时必然不达标——全量 jest 由 `frontend-ci.yml` 执行。同理，本机并行执行使单套件耗时被放大（mcp-servers 页面 374s），**不能据此判定性能回归**（R-16 见 §4）。

## 4. 未达项与阻塞清单

| # | 项 | 类型 | 阻塞点 | 承接 |
| --- | --- | --- | --- | --- |
| G-1 | A0-03 Postgres 侧迁移 | 环境 | 本机无 Docker/Postgres（已实测 5432 不可达）；门控用例已就绪 | CI `mcp-postgres-migrations` 首绿 |
| G-2 | A0-12/A0-14 管理页冒烟与截图 | 浏览器 | 需真实后端进程 + 登录会话 | CI `e2e-mcp` 首绿（用例已就绪） |
| G-3 | A1-05/A1-06/A1-10 的浏览器级证据 | 浏览器 | 同上；对话链路已由 mock LLM 替身具备确定性驱动 | CI `e2e-mcp` 首绿 |
| G-4 | A2-05 判定 | 浏览器 | 用例 + CI 作业 + 确定性驱动均已就绪，**待真实栈执行** | CI `e2e-mcp` 首绿后回写 `flow_verified` |
| G-5 | 写工具审批卡片的浏览器断言 | 用例 | 需在对话链路用例中把目标工具分类为写并断言 `tool-approval-card-*` | M2-05 收尾 |
| G-6 | R-16 前端套件性能 | 工程 | 缺**受控环境**（CI 串行/分片）下的耗时基线 | M2-05 |
| G-7 | A2-01/A2-02/A2-04 | 范围 | 依赖 Q1/Q6/Q5 决策（一期不做 / 二期） | 决策后立项 |
| G-8 | A2-07/A2-08 accepted 复核与签署 | 程序 | 需全部任务完成 + 人工评审 | M2-07 |

## 5. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | 首次完整度确认：22 项能力矩阵 + 本轮实测复现 + 8 项未达/阻塞清单；对应方案 §3.3.1/§5.2.1 与缺口台账 `gap-register-2026-09-27.md` |
