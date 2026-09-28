# MCP 接入方案：缺口登记与整改台账

> 文档类型：缺口登记与整改台账（对照实施方案自定判据的审计结论）
> Status: draft（本轮整改执行中；每项「本轮处置」列随执行回写）
> 编制日期：2026-09-27
> 适用范围：`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（下称「本方案」）中 M0/M1 已声称交付部分，以及 M2 未开工部分
> 目标读者：研发负责人 / QA / 安全 / 产品
> 关联文档：本方案 §2（前置与检查清单）、§3.1（里程碑）、§5（验收标准）、§7（风险登记）、§10（决策日志）、§11.4（未核实项）；证据目录 `docs/plan/evidence/mcp-m0/`、`docs/plan/evidence/mcp-m1/`
> 核查基线：分支 `feat/bot-mcp-integration`，核查时 HEAD `3b2de91f`
> 核查方式：静态核对（方案条款 ↔ 代码/测试/证据/CI 配置）；**不替代**里程碑出口评审
> 状态口径：`已补`（本轮闭环）/`部分补`（本轮推进但有残留）/`待外因`（需环境或他人决策）/`归 M2`（本方案内明确由 M2 承接）

## 1. 结论摘要

| 类别 | 数量 | 说明 |
| --- | --- | --- |
| A 类：已交付部分未满足方案判据（§5.2/§5.3/§5.4） | 8 | 其中 6 项本轮可补，2 项需真实浏览器会话（归 M2-05） |
| B 类：文档回写缺口 | 6 | 本轮全部回写 |
| C 类：方案内已登记、待 M2/阶段一承接的技术缺口 | 6 | 状态更新与责任归属 |
| D 类：M2 未开工 | 7 任务 / 8 验收项 | 属计划内后续范围，非缺陷 |

**核心结论**：M0/M1 功能交付属实（`integration_verified` / `flow_verified` 的**功能面**成立）；但按本方案 §5.4 规则 7，A 类 8 项均属「豁免」情形，而 §10 决策日志此前**无任何豁免登记**——本轮已补齐登记（见 §6、本方案 §10）。

---

## 2. A 类：未满足方案判据的缺口

| # | 缺口 | 方案依据 | 核查证据 | 影响 | 本轮处置 |
| --- | --- | --- | --- | --- | --- |
| A1 | **Postgres 双驱动迁移未验证**（A0-03 明写「SQLite 与 Postgres 双驱动」） | 本方案 §5.2 A0-03（:571）、§7 R-09（:696）、§11.4 U-2 | `itsm-backend/ent/schema/mcp_migration_test.go:13` 仅引 `go-sqlite3`；M0-03 证据 :40 自述「本地无 Docker/Postgres，须 CI 覆盖」 | A0-03 实际仅达 SQLite 单驱动；生产迁移风险未闭环 | **部分补**：门控用例已就位（§6.4），本机无 Postgres/Docker（已实测 5432 不可达）；**待外因**（CI service container，建议 S1） |
| A2 | **管理页人工冒烟 + 截图未做**（A0-12 目标级别含「+冒烟」） | §5.2 A0-12（:580）、§5.5 证据规范 | M0-12 证据 :5/:70、M0-14 证据 :7/:71/:75 | A0-12/A0-14 证据集不完整 | 归 M2-05（需真实后端 + 登录会话 + 浏览器；本轮登记为豁免） |
| A3 | **全量回归未执行**（`go test ./...` 从未跑通；前端此前仅定向套件） | §5.1 D-2（:557）、§5.4 规则 2 | M0-01 证据 :34 自述未执行 | M0/M1 出口「回归全绿」无证据 | **已补**：本轮执行全量后端 + 全量前端并逐类定性（§6.1/§6.2）；MCP 范围绿，非 MCP 红为既有/环境问题 |
| A4 | **A1-05「手工」、A1-06「E2E」部分未做** | §5.2 A1-05（:592）、A1-06（:593） | M1-10 证据 :20 声明浏览器 E2E 归 M2-05 | 两项 `flow_verified` 属部分达成 | 归 M2-05（本轮登记为豁免） |
| A5 | **T-09 的 Playwright 未在 M1 出口执行**（§5.3 原则：E2E 走 nightly + 里程碑出口） | §5.3（:624、:626） | `itsm-frontend/tests/e2e/mcp/` 目录不存在 | 与 A4 同源；M1 出口条件不齐 | 归 M2-05 |
| A6 | **`-race` 从未执行** | §5.1 D-2 精神 + 各任务卡「测试与证据」 | M0-07 证据 :43、M0-08 证据 :67、M0-14 证据 :79 三处自认待补 | 并发正确性无机器验证（O-1 恰为并发面） | **已补（核心面）**：`registry`/`transport`/`tests/mcpintegration` race 全绿、无 DATA RACE（§6.3/§6.7）；其余包建议 CI 常态化（S3） |
| A7 | **lint 未执行**；且本方案 §5.1 D-2 写的 `.golangci.yml` 与仓库实际 `.golangci-lint.yml` 不符 | §5.1 D-2（:557） | 仓库有 `.golangci-lint.yml`；`golangci-lint` 本机未安装；CI 实际为 gofumpt + staticcheck（`backend-ci.yml:8-11/:111/:132`） | D-2 未满足（或需改口径为「CI 既有质量门」） | **已补（并发现 9 处 CI 阻断项，全部修复）**：staticcheck 6 + gofumpt 2 + 800 行硬门 1（`service.go` 拆分）→ 复验 exit 0（§6.5）；D-2 口径已回写 |
| A8 | **豁免未登记**（A1–A7 均属 §5.4 规则 7 情形） | §5.4 规则 7（:636）、§10 变更控制（:766） | §10（:755-764）此前仅 Q1–Q8 | 程序性缺口：M0/M1 状态声明在方案口径下不成立 | 已补：本方案 §10 新增「实施期豁免与偏差登记」 |

---

## 3. B 类：文档回写缺口

| # | 缺口 | 依据 | 本轮处置 |
| --- | --- | --- | --- |
| B1 | §3.3 任务总表无「状态列」，与 §1.4/D-6/A2-08 的「回写 §3.3 状态列」自相矛盾 | 本方案 :159、:561、:541 | 已补：§3.3 追加「状态速览表」（31 任务逐项），注释同步改口径；偏差登记 §10 |
| B2 | §5.2 验收矩阵无「判定列」，A2-08 却要求回写 | 本方案 :610 | 已补：§5.2 追加「判定表（A0/A1 逐项 + A2 目标）」，A2-08 措辞同步为「判定表」；偏差登记 §10 |
| B3 | §2.3 检查清单 6 项未勾（P2/P3/P4/P7/CI 门禁/团队评审） | 本方案 :120-127 | 已补：P2/P4 勾选并附证据；P3/P7 标注「部分完成 + 残留」；CI 门禁标注现状（全量包自动覆盖、缺 `-race`/Playwright 专项）；评审标注未做 |
| B4 | §11.4 未核实项状态过期（U-1/U-4/U-6 已实际闭环） | 本方案 :851-856 | 已补：U-1/U-4/U-6 状态更新；U-2/U-3/U-5 维持开放并注明依赖 |
| B5 | §7 风险登记未随实施回写（R-09/R-12/R-13 仍写未核实；O-1/O-2 未入册） | 本方案 :686-700 | 已补：新增 §7.1「实施期状态更新」——R-09/R-12/R-13 状态确认 + 新增 R-14（O-1）/R-15（O-2） |
| B6 | §9 交付物偏差：`lib/types/mcp.ts` 未单列；`CHANGELOG.md` 无 MCP 条目 | 本方案 :746、:748 | 已补/部分补：交付物表登记偏差（类型并入 `lib/api/mcp-api.ts` 与 `pages/(admin)/mcp-servers/mcp-helpers.ts`）；CHANGELOG 回写归 M2-07（本轮登记） |

---

## 4. C 类：方案内已登记、待 M2/阶段一承接的技术缺口

| # | 缺口 | 现状 | 归属 |
| --- | --- | --- | --- |
| C1 | **R-12 队列内存态**：ToolQueue 重启丢 pending 写调用 | 已确认（实现为内存队列；M1-02 证据 :70 注明） | 阶段一 B1（未落地时按 R-12 预案人工处置） |
| C2 | **R-13 审批无过期状态机**：前端仅提示「可能已过期」 | 已确认（M1-05 证据 :51-53） | 阶段一 G3 |
| C3 | 生命周期事件不落库（仅内存环形缓冲） | 已确认（M1-08 证据 :54） | M2-06 或阶段一 |
| C4 | 审批/审计页无 URL query、invocation 无深链；审计页与 AI 场景审计无跨表联动 | 已确认（M1-06 证据 :50-52、M1-07 证据 :46/:48） | 建议 M2 单开小任务 |
| C5 | `risk` 为实时解析，服务器不可达时退化为空 | 已确认（M1-02 证据 :70） | 可并入 M2 加固 |
| C6 | R1「一次迁移」尚未与阶段一 B0 对齐（U-5 排期未定） | 开放 | 研发负责人 / 阶段一 |

---

## 5. D 类：M2 未开工清单（计划内）

| 任务 | 缺失证据（核查时） |
| --- | --- |
| M2-01 stdio 传输与沙箱 | 无 `itsm-backend/mcp/transport/stdio.go`（transport 目录仅 sse/streamable/ssrf/errors/options）；无沙箱测试 |
| M2-02 OAuth 2.1 | 无 `ent/schema/mcp_oauth_token.go`；无 `mcp/auth/` 包 |
| M2-03 工具面预算与元工具 | 无阈值（>40）告警与 token 占比测量；无元工具实现 |
| M2-04 平台共享服务器 × 租户授权 | 无授权矩阵模型/API |
| M2-05 浏览器 E2E 全链路 | `itsm-frontend/tests/e2e/mcp/` 不存在（A2/A4/A5 的共同载体） |
| M2-06 指标/告警看板与运维手册 | 无 `mcp/manager/metrics.go`；无 `docs/ops/mcp-runbook.md` |
| M2-07 accepted 复核与文档回写 | 未开始（§3.3/§5.2 判定、CHANGELOG 回写均在其范围） |

> 澄清（避免误读）：CI **并非**完全未覆盖 MCP——`backend-ci.yml:261` 以 `go test $TESTABLE_PKGS` 自动覆盖全部可测包（含 `./mcp/...`、`./tests/mcpintegration/`）；`frontend-ci.yml:53` 跑全量 jest。缺的是 **`-race`、Playwright MCP E2E、MCP 专项门禁**（§2.3 那条 checklist）。

---

## 6. 本轮补齐执行记录

（本节随执行结果回写；命令、原始输出摘要与文件变更见各小节。）

### 6.1 全量后端回归（A3 / EX-05）

- **命令**：`go test ./... -count=1`（Windows 本机；与前端全量 jest 并发执行，日志 `%TEMP%\itsm-mcp-full-regression.log`）
- **结果**：exit=1；失败包 8 个。逐类定性：

| 失败包 | 现象 | 定性 | 处置 |
| --- | --- | --- | --- |
| `mcp/client` | `TestConnect_SSE_Handshake`：`connect_timeout: 建连/握手超过 100ms` | **负载敏感用例**（100ms 握手预算在并发排程下误报） | 已修：预算 100ms → 3s，sleep 150ms → 3.2s（语义不变：会话不绑定握手超时） |
| `mcp/testutil/mockserver` | `TestMockServer_FaultSlowConnect`：墙钟断言 `> 3s` | **负载敏感用例**（观测 3.43s） | 已修：断言上限 3s → 10s（只守「不得挂起」） |
| `tests/mcpintegration` | `TestM0Flow_EndToEnd`：期望 6 个工具，实际 4 | **异步发现竞态**（healthy 早于工具落库） | 已修：改为「按目标数量有界等待（20s）+ 最终断言」 |
| `handlers/sla`、`handlers/service_catalog`、`handlers/service_request`、`service` | 42 处 `TempDir RemoveAll cleanup: unlinkat ... db: 进程占用`；隔离复跑 `handlers/sla` 全部子用例 PASS、仅 teardown 失败 | **既有 Windows 专有文件句柄释放问题**（非 MCP；Linux CI 不复现） | 记录为既有基线问题；不改动他人代码 |
| `pkg/seeder` | `seeder_test.go:359/374` 断言失败（204s） | **既有失败**（M0-10 证据已记录） | 归属维护者；不属 MCP 范围 |
| `service` + 2 个 handlers 包 | `panic: test timed out after 10m0s`（满载并行下 600s+） | **并发负载导致的包级超时**（隔离下 sla 3.8s） | 建议 CI 对测试步显式 `-timeout 20m`（见 §7 建议） |

- **复测（修复后）**：`go test ./mcp/client/ ./mcp/testutil/mockserver/ ./tests/mcpintegration/ -count=1` → **ok**（7.1s / 5.9s / 23.1s）；`go test ./mcp/... ./tests/mcpintegration/ ./handlers/mcp/` → 9 包全 **ok**。
- **结论**：MCP 范围回归**绿**；本机全量红由「既有 Windows 问题 + 既有失败 + 负载超时」构成，与 MCP 改动无因果关系。**D-2 的「`go test ./...` 全绿」应以 Linux CI 为准**（本机不可作为判据）。

### 6.2 全量前端 jest（A3 / EX-05）

- **命令**：`npm test -- --runInBand --forceExit --ci`（对齐 `frontend-ci.yml:53`；日志 `%TEMP%\itsm-mcp-full-jest.log`）
- **结果**：`Test Suites: 10 failed, 232 passed, 242 total`；`Tests: 18 failed, 13 skipped, 3853 passed, 3884 total`
- **分类**：
  - **预存在失败（非 MCP）**：`ticket-attachment-api`（他人附件 WIP，M1-05/06 已记录）、`api-contract`、`ErrorBoundary`、`TicketTypeFormModal`、`CIImpactAnalysisTab`、`TicketAttachmentSection`、`WorkflowAIModal`、`TicketDetailAssignSearch`、`useIncidentStats`。
  - **MCP 套件**：`admin/mcp-servers/__tests__/index.test.tsx` 在满载下耗时 **947s** 并触发 `Exceeded timeout of 300000 ms`；同套件在隔离复跑（`npx jest` 单套件、机器半载）仍 595s 超时。**定性：性能/超时问题（非功能回归）**——该套件 3 个用例均为「整页渲染 + 多步交互」，安静环境 255s、负载环境 595–947s，跨过 300s 单测上限即失败。
- **处置**：登记为 **R-16（新增风险）**；M2-05 落地时一并治理（拆分用例、mock 重子组件、或将 `testTimeout` 与 `--maxWorkers` 分离配置）。**未**在本轮改变前端实现。
- **结论**：MCP 前端套件在隔离环境**功能通过**（M1-10 证据 6 套件 37 用例全绿）；负载敏感性属已知缺口，不改变 M1 的功能判定。

### 6.3 `-race`（A6 / EX-06）

- `go test -race ./mcp/registry/ ./mcp/transport/ -count=1` → **ok**（1.9s / 1.5s）。
- `go test -race ./tests/mcpintegration/ -count=1` → **ok 19.9s**（首次因与全量回归/jest 并发导致工具 30 分钟上限被杀，轻载重跑得结论）。
- **全量**：`go test -race ./mcp/... ./tests/mcpintegration/ -count=1 -timeout 1800s` → **exit 0，8 包全 ok、无 DATA RACE**：admin 36.7s / client 8.8s / manager 4.5s / provider 23.6s / registry 2.1s / mockserver 7.3s / transport 2.5s / mcpintegration 38.9s（整轮 4 分 23 秒）。
- 已据此接入 CI：`backend-ci.yml` 新增 **`mcp-race`** job（`go test -race ./mcp/... ./tests/mcpintegration/ -count=1 -timeout 30m`），常态化覆盖并发面（R-14/O-1 相关）。

### 6.4 Postgres 门控迁移用例（A1 / EX-01）

- 新增 `itsm-backend/ent/schema/mcp_migration_postgres_test.go`：3 个用例（建表+联合列+幂等、旧行默认值、安全默认+唯一约束），按 `MCP_TEST_POSTGRES_DSN` 门控，**每用例独立 schema（`mcp_test_<用例名>`）+ 结束 DROP CASCADE**，不要求独占实例。
- 本机验证：`go vet ./ent/schema/` exit 0；`go test ./ent/schema/ -run Postgres -v` → 3 个用例 **SKIP**（未设 DSN），不污染 SQLite 用例。
- 环境探测：本机 `127.0.0.1:5432` 不可达、无 `psql`、无 `PG*` 环境变量 → **A1 在本机不可闭环**，需 CI service container（建议步骤见 §7）。

### 6.5 CI 等价质量门（A7 / EX-07）

- 工具：`staticcheck@v0.6.1`（CI 同款）+ `gofumpt@v0.7.0`（CI 定版；本机为其源码构建）。
- **首轮结果（真实 CI 阻断风险，全部已修）**：
  1. staticcheck：`mcp/admin/audit.go:84/108`（未使用函数 ×2）、`mcp/admin/store.go:202`、`mcp/manager/pool.go:146/274`（未使用方法 ×2）、`mcp/admin/credential_test.go:115`（S1025）→ 已修（删除死代码 5 处、改用 `String()`）。
  2. gofumpt：`mcp/admin/service_test.go`、`mcp/admin/store.go`（以及拆分后的新文件）→ 已 `gofumpt -w`，复跑**无输出**。
  3. **新文件 >800 行硬门**（`backend-ci.yml:135-179`）：`mcp/admin/service.go` 原 **1239 行**，CI 会直接 fail → 已拆为三个文件：`service.go` 564 行（生命周期/工具治理/辅助）、`service_servers.go` 381 行（服务器 CRUD + 测试连接）、`service_types.go` 298 行（类型与视图），全部 <800。
- **复验**：`staticcheck ./mcp/... ./tests/mcpintegration/` → **exit 0（无输出）**；`gofumpt -l`（MCP 范围）→ 空；`go build`/`go vet` → exit 0；9 个包测试全绿（§6.1 复测）。
- 备注：本机 gofumpt 还会列出仓库既有文件（`dto/*`、`handlers/auth|ticket*`）——属**预存在格式差异**（本地为源码构建、版本语义与 CI 发布包可能不同），本轮未触碰，避免引入无关 diff。

### 6.6 CI 接线（本轮新增，S1/S2/S3 落地）

`backend-ci.yml` 变更（YAML 已用 `js-yaml` 解析校验，job 列表 = lint / build / test / mcp-postgres-migrations / mcp-race / dependency-review）：

| 项 | 变更 | 目的 |
| --- | --- | --- |
| S2 | Test job 的 `go test` 增加 **`-timeout 20m`** | 消除满载下 `panic: test timed out after 10m0s` 的假红（本机观测到 3 个包命中） |
| S1 | 新增 **`mcp-postgres-migrations`** job：`postgres:16-alpine` service container（health-cmd `pg_isready`）+ `MCP_TEST_POSTGRES_DSN` + `go test ./ent/schema/ -run Postgres -count=1 -v` | 让 A0-03 的 Postgres 侧用例在 CI 真实执行（本地无 Docker/Postgres） |
| S3 | 新增 **`mcp-race`** job：`go test -race ./mcp/... ./tests/mcpintegration/ -count=1 -timeout 30m` | 并发面常态化（本机已全绿，见 §6.3） |

**风险与开关**：Postgres 侧用例此前从未在真实 Postgres 上跑过（本机仅 SKIP），故该 job **首轮以 `continue-on-error: true` 观察**；绿跑后删除该行即转为阻断门（已在 workflow 注释中写明）。`mcp-race` 有本机全绿依据，直接作为阻断门。

### 6.7 文档门禁（docs-gate）

- 本机无 `bash`（`bash: NOT FOUND`），`scripts/docs-gate/*.sh` 无法执行 → 以人工核对替代：本轮新增内容未引入 Markdown 链接（路径均为行内代码），不触发 C.3「断链」；未涉凭据/发布声明（C.1/C.4）。最终以 CI `docs-gate.yml` 为准。

### 6.8 收尾复核（本轮结束时更新）

- `-race` 集成复跑：`go test -race ./tests/mcpintegration/ -count=1` → **ok 19.9s，无 DATA RACE**（总耗时 1292s，主要是 race 插桩重编译）；加上 `./mcp/registry/`、`./mcp/transport/` 的 race 结果，**A6 的 MCP 核心面已闭环**；其余包建议 CI 常态化覆盖。
- 缺陷修复顺带产出：本轮修复 3 处负载敏感用例 + 6 处 staticcheck 问题 + 2 处格式问题 + 1 处 CI 行数硬门（`service.go` 拆分），详见 §6.1/§6.5。

---

## 7. 建议与归属（未在本轮闭环的部分）

| # | 建议动作 | 归属 | 说明 |
| --- | --- | --- | --- |
| S1 | ~~CI 增加 Postgres service container + `MCP_TEST_POSTGRES_DSN`~~ → **本轮已落地**（`mcp-postgres-migrations` job，首轮 `continue-on-error` 观察） | 后端 + CI 维护者 | 待首次 CI 绿跑后移除 `continue-on-error` 转阻断（§6.6） |
| S2 | ~~CI 测试步显式 `-timeout 20m`~~ → **本轮已落地** | CI 维护者 | 已消除满载 10m 包级超时假红（§6.6） |
| S3 | ~~常态化 `-race`~~ → **本轮已落地**（`mcp-race` job）+ 本机全量 8 包绿 | CI 维护者 | 依据见 §6.3（admin/client/manager/provider/registry/mockserver/transport/mcpintegration 全 ok） |
| S4 | 治理 `admin/mcp-servers` 前端套件性能（拆分 3 个巨型用例、mock 重子组件、独立 `testTimeout`） | 前端 + QA（M2-05） | 见 R-16：安静 255s / 负载 595–947s，超 300s 单测上限 |
| S5 | 处置既有 Windows 本机失败（42 处 `TempDir` 句柄占用）与 `pkg/seeder` 既有失败 | 仓库维护者 | 非 MCP 范围；Linux CI 不复现句柄问题 |
| S6 | 修复既有格式差异（`dto/*`、`handlers/auth|ticket*` 被本地 gofumpt 列出） | 仓库维护者 | 本地为源码构建的 gofumpt；CI 用 v0.7.0 发布包，需以 CI 结果为准 |
| S7 | M2-05 落地后补齐 A2/A4/A5（浏览器 E2E、冒烟截图、A1-05 手工、A1-06 E2E） | QA（M2-05） | 见 §2 缺口 A2/A4/A5 |

## 8. 变更记录

| 日期 | 变更 |
| --- | --- |
| 2026-09-27 | 首次登记：以方案 §5.1/§5.2/§5.3/§5.4 为判据完成审计（A 类 8 项 / B 类 6 项 / C 类 6 项 / D 类 7 任务） |
| 2026-09-27 | 本轮整改：方案回写（§2.3/§3.3.1/§5.2.1/§7.1/§9/§10/§11.4）；新增 Postgres 门控用例；全量后端+前端回归并分类；`-race` 核心面通过；CI 等价质量门（staticcheck/gofumpt/800 行硬门）发现并修复 9 处问题；修复 3 处负载敏感用例 |
| 2026-09-27 | 二次整改（CI 接线）：`-race` 全量 8 包绿（A6/EX-06 闭环）；`backend-ci.yml` 新增 `mcp-race`（阻断）与 `mcp-postgres-migrations`（service container，首轮 `continue-on-error` 观察）两个 job、Test job 增 `-timeout 20m`（S1/S2/S3 落地，新增 EX-09） |

