# 上游成果吸收规划（不退化优先 / Non-regression-first）

> **状态**：待裁决（P0 两项阻塞）；**未合并任何上游代码**
> **分支**：`feat/upstream-heidsoft-integration`（起点 `3cfe7a37`，与 `feat/msp-p0-hardening` 0/0）
> **上游**：`heidsoft/main = 63b9feb5`（merge-base `e7fd79e9`，2026-09-20）
> **姊妹篇**：[能力清单](../review/2026-11-06-upstream-heidsoft-feature-inventory.md)、[可移植性扫描](../review/2026-11-06-upstream-heidsoft-integration-scan.md)

## 0. 目标与不可协商底线

**目标**：吸收上游（`heidsoft/itsm`）的成果，以**能力**为单位移植进本地。

**底线（三条，全部可测量）**：

1. **契约面不得缩水**：路由注册数、权限预检条目、ent schema 数、迁移数、前端测试文件数，
   任一低于基线即门禁红；
2. **行为不得倒退**：本地 3760 个后端用例 + 375 个前端测试文件必须全绿，且每个吸收 PR 附「吸收前/吸收后」同口径对照；
3. **不变量不得被隐式改写**：唯一键语义、租户隔离边界（BD-1/RLS）、权限码归属（D7）
   属显式裁决项，**禁止**在移植中"顺手改"。

## 1. 基线（同一条命令测两侧，2026-11-06）

| 指标 | 本地 `HEAD` | 上游 | Δ | 判读 |
|---|---|---|---|---|
| 后端 test 函数 | 3760 | 3965 | **+205** | 上游净多的主要资产 |
| 后端 test 文件 | 704 | 723 | +19 | 同上 |
| 路由注册 | **999** | 745 | **−254** | 本地契约面远大于上游 |
| 权限预检条目 | **1123** | 891 | **−232** | 同上（生成物，不可取文件） |
| SQL 迁移 | **129** | 75 | **−54** | 本地版本化历史更长 |
| ent schema | **172** | 136 | **−36** | 本地模型更完整 |
| 前端测试文件 | **375** | 239 | **−136** | 本地测试覆盖更广（且已迁 Vite） |

**结论（决定整个规划的方向）**：**上游不是本地的超集。**
按同口径，上游在 5 项指标上更少。因此「合并上游」若以 merge/整目录替换形式进行，
**结果就是倒退本身** —— 少掉 254 条路由、232 条权限预检、36 个 schema、136 个前端测试文件。
可吸收的只有：① 上游**净增的 205 个测试函数**；② 本地**不存在的域/能力**；③ 若干工具与 CI 门禁。

## 2. 不退化的三层门禁（先建门禁，后做吸收）

### L1 结构门禁（落地位置：`scripts/check-engineering-contracts.js`）

**先用现成的，再补缺口**。本地已有两道与本目标直接相关的门禁，规划**不得**另造并行机制（本仓忌讳双源）：

| 现有门禁 | 它管什么 | 对「吸收上游」**管不到**什么 |
|---|---|---|
| `.github/workflows/test-coverage-guard.yml` + `scripts/test-coverage-guard.js` | 规则「改了源码 X 就必须改测试 X」，防生产码演进而测试原封不动 | 不管**测试总量下降**（整删测试文件仍可满足规则） |
| `.github/workflows/acl-gate.yml` + `scripts/verify-acl-coverage.go` | 每条非公开路由必须有 `RequirePermission`；manifest 版本与 router 版本一致；新路由必须声明权限 | **不阻止路由被删除** —— 路由变少时覆盖率依旧 100%，manifest 会「一致地」跟着变小 |
| `scripts/check-engineering-contracts.js`（Makefile:115 + `api-contract-check.yml`） | 8 条声明式契约（同源 API 基址、生产 env、compose 运行时、发布扫描、可复现文档等） | 现有条目**全是形态断言，无任何「面不减」断言**（实测脚本内无数量比较） |

因此 L1 的增量**只补现成机制覆盖不到的三类**，落在 `check-engineering-contracts.js`（有自身用例，扩条目即进 CI）：

| 新增契约 id | 断言（下限 = 2026-11-06 实测值） | 为什么现有门禁拦不住 |
|---|---|---|
| `absorption-surface-floor` | 路由注册 ≥ **999**；`rbac_precheck_gen.go` 路径键 ≥ **1123**；`ent/schema/*.go` ≥ **172**；`migrations/*.sql` ≥ **129**；前端 `*.{test,spec}.{ts,tsx}` ≥ **375** | acl-gate 只看「现存路由是否受保护」，coverage-guard 只看「改动是否配套测试」；**删除**动作两者都放行 |
| `absorption-no-nextjs-regression` | **forbid** `itsm-frontend/package.json` 出现 `"next"`；require `vite build` | 上游仍是 Next.js，任何「顺手带文件」都会把前端拖回旧架构 |
| `absorption-no-vendored-dist` | **forbid** `itsm-cli/dist/**` 入库 | 上游已把 144 个构建产物提交进 git，合并会原样带入 |

另两条**不是新门禁，而是既有约定的延伸**，写进流程而非脚本：

- **错误码同口径**：G12 已用单测钉住「后端 TUM 稳定码 == 前端映射」（当前 12==12）；
  吸收新通道时**同法**加断言，不另建 parity 工具；
- **每个吸收 PR 必须触发并通过 `test-coverage-guard`** —— 这就是「吸收代码不许不带测试」的现成执法器，
  规划直接引用它，不再设「测试数必须上升」这类软指标（软指标会被「加空测试」绕过）。

**自证有效要求**：P0 阶段必须现场演示「删一条路由 / 删一个测试文件 / 放入一个 `itsm-cli/dist` 文件」时新门禁**变红**，
否则判定门禁无效（本仓先例：某 header 冲突门禁因分支不可达而长期空转，见 `07:G9`）。

### P0 完成状态（2026-11-06 已实施）

**已落地**（`scripts/check-engineering-contracts.js` + 其 `node:test` 用例，均已在 CI 路径触发列表内）：

| 草案 id | 实际实现 | 说明 |
|---|---|---|
| `absorption-surface-floor` | **拆成 6 条具名护栏**：`absorption-backend-routes`(999) / `absorption-rbac-precheck-keys`(1123) / `absorption-ent-schemas`(172) / `absorption-sql-migrations`(129) / `absorption-frontend-tests`(375) / `absorption-vendored-cli-dist`(**上限 42**) | 拆开而非合并，是为了失败信息直指**哪一面**缩水；草案把 dist 写成"forbid 任何 dist"，实测**本地已有 42 个 `itsm-cli/dist` 文件入库**（`.gitignore` 的 `dist/` 规则对已跟踪文件无效），故改为**上限**——倒退定义变成"吸收不得增多产物"，清理既有 42 个另立任务 |
| `absorption-no-nextjs-regression` | 同名，落 `CONTRACTS` | require `"build": "tsc -b && vite build"`；forbid `"next":` / `"next-*":` |
| `absorption-no-vendored-dist` | 合入上行 `absorption-vendored-cli-dist` | 用 `git ls-files` 计数（非磁盘计数，避免本地构建产物误报）；git 不可用时**显式标注跳过**而非静默通过 |

**实测证据**：

1. `node scripts/check-engineering-contracts.js` → **15 passed, 0 failed**（8 条既有 + 1 条既有格式新契约 + 6 条面护栏）；
2. `node --test scripts/__tests__/check-engineering-contracts.test.js` → **10/10**（新增 7 条：路由下限、整目录删除必须显式失败、前端测试整删、dist 上限用注入 stub、git 不可用仍报告、Next.js 回归禁止、护栏集合自身一致性校验）；
3. **真实树破坏演示**（脚本化、演示后精确还原）：
   - 删 2 条路由注册 → `exit=1`，`absorption-backend-routes: count 997 violates min 999`；
   - 把 `package.json` 改回 `next build` 并注入 `next` 依赖 → `exit=1`，`absorption-no-nextjs-regression: missing …vite build; forbidden /"next":\s*"/`；
   - 还原后 → `exit=0`，`git status` 与演示前**完全一致**。

**P0 未完成部分**：附录 A 基线已内联进脚本注释（单一来源），但 **G18 / G19 两项裁决仍未登记**——
它们是 P2/P4 的前置，不是 P0 门禁的一部分。

### L2 行为门禁

- 后端：`go test ./...`（本机内存不足时按既定方案在 238/容器内跑，见部署证据 §6 R1 方法与 `.dockerignore` 坑）；
- 前端：`tsc --noEmit` + `jest --ci`（全量，不只目标套件）+ `eslint` 0 error；
- 每个吸收 PR 必须附**同口径前后对照表**（测试数、路由数、失败数），不许只写"已测试"。

### L3 运行门禁（238 演示栈）

沿用本轮已验证有效的方法：**同一端点、部署前后各调一次，断言字段/错误码出现次数**。
（先例：G13 部署前 `activeAdminCount` 出现 0 次 → 部署后 4 种查询组合均正确。）
新增能力（如 `on-call`）必须以 API 实测 + 浏览器点测双结论收口，写进 `docs/multi-tenant/evidence/`。

## 3. 吸收分级（能力级，不是提交级）

### A 类｜**实测后重排**：初稿 5 项中 4 项不成立（2026-11-06 S1 差集结果）

| 候选（初稿 A 类） | 实测耦合（逐 import 核对） | 结论 |
|---|---|---|
| `on_call` | 本地**已完整实现**，只是挂在 `handlers/email_intake`（`handler.go:66` → `/email-intake/on-call/*`；seeder 菜单 `seeder.go:1819`；服务测试 `TestOnCallService_RejectsOverlapAndFindsCurrentResolver`；前端页面 `email-intake/on-call/index.tsx`）；上游只是拆成独立域 10 端点 | ❌ 非新增 → **B 类（域边界重构）** |
| `cmd/ai-eval` | 依赖 `ai/eval` 的 7 个本地不存在符号（`LoadRAGCorpus`/`NewRealRAGRetriever`/`NormalizeRef`/`PredictionCase`/`RAGCase`/`SummarizeCandidateGenerator`/`SummarizeCase`）；本地 `ai/eval` 2 文件 vs 上游 5 | ❌ 需先差集 `ai/eval` |
| `cmd/aliyun-oss-api-verify` | 需 `aliyun-oss-go-sdk`（本地 `go.mod` **无此依赖**）+ `service/cloud` 分叉 | ❌ 非自包含 |
| `cmd/aliyun-sdk-api-verify`、`aliyun-security-api-verify` | 依赖 `service/cloud`（本地 8 / 上游 36）、`service/cloud/aliyun`（1/11）、`infrastructure/cloud`（1/6）、`infrastructure/cloud/alicloud`（1/5） | ❌ 非自包含 |
| `cmd/service-request-maintenance` | 依赖本地**不存在**的 `svc.TerminateStaleServiceRequestInstances`；且虽默认 dry-run，`-apply` 会改数据 | ❌ 需先移植服务方法 |

**重排后的真正 A 类（自包含、可独立验证）**：

| 单元 | 规模 | 依赖 | 试点价值 |
|---|---|---|---|
| `service/sla_breach_predictor.go` | 156 行 | **仅 `fmt` / `math`**（纯函数） | ✅ **P1 试点首选**：零 DB、零迁移、零接线风险，可用表驱动单测锁定行为 |
| `service/ai_quota.go` | 206 行 | **仅标准库**（`context/sql/fmt/math/os/strconv/strings/sync/time`） | A 类第二项；接线需先定配额读取点与告警口径 |
| ✅ `service/bpmn_template_digest.go` | 62 行纯标准库 | 本地已有 `//go:embed bpmn/*.bpmn` 的 `bpmnTemplates`（`bpmn_template_service.go:24-25`，20 个模板） | **已移植（P2）**：`BuiltinProcessTemplateKeys()` + `EmbeddedBPMNDigest()`；上游无测试，本地补 3 条（含独立重算的可打破用例） |
| ❌ `service_quality_report_service.go` | 150 行 | 实测依赖 `DashboardViewer`（本地 0 命中）、`DashboardOverviewSource`、`dto.AnalyticsTicketStatsResponse`（本地无）及 6 组 dashboard 数据类型 | **改判 B 类**（深耦合工作台/analytics 聚合层），不是「仅依赖 dto」 |

> **口径教训（S1 期间第四次同类错误）**：**目录名 ≠ 能力、import 列表 ≠ 耦合面**。已连续四次踩坑：① `handlers/change_review` 实为本地 `cab` 改名；② `service/cloud` 被误判为"本地没有"（实为同一包 8 vs 36 分叉）；③ `on_call` 被误判为"本地完全没有"（实为挂在 `email-intake` 下且已实现）；④ `service_quality_report_service.go` 看似"仅 import `dto`"，实则**同包符号**依赖 `DashboardViewer`/`DashboardOverviewSource` 与 6 组 dashboard 数据类型（本地全无）。
> **此后强制的 S1 规则**：判定「新能力」必须五处交叉证实 —— 功能字符串 / 路由 / 权限码 / 菜单 / 前端入口；任一命中即改判为「已存在，需差集」。

### B 类｜差集扩展（触碰已有域，先测绘后动手）

`service/cloud`（**同一包已分叉**：本地 8 / 上游 36，另 `service/cloud/aliyun` 1/11）+ `handlers/cloud`（**本地独有**的 HTTP 面，上游无此目录）；`service/marketplace`(13) ↔ 本地 `handlers/marketplace`；
`connector/builtin`(7)；`pkg/seeder`(28)；`workbench`；`account`。
→ 每一处都要回答："本地是否已有等价实现？若有，差异是 bug 还是设计？"（本仓多次教训：把已实现的能力当新能力移植）。

### C 类｜资产吸收（低风险高收益）

- 上游 **3965 个测试函数**里**不依赖其私有实现**的部分（RLS 作用域、租户唯一键、软删、错误码语义、能力矩阵）→ 作为本地守护测试移植；
- CI 门禁：`fresh-install-gate.yml`、`rls-golden-path.yml`（本地目前无同名门禁）。

### D 类｜拒绝（无论何时都不做）

`feat(role)!` 权限包重构（与 D7/G11/`RoleRank` 四处同源冲突）；Next.js 侧 `src/app/**` 与 `next` 依赖；
`itsm-cli/dist/**`；上游生成物文件（`rbac_precheck_gen.go`、`ent/**`、`docs/{docs.go,swagger.*}`、`acl-manifest.yaml`）；
`CHANGELOG.md` / `UPGRADE.md` / `docs/api-reference.md` 整文件替换（上游分别改过 242/57/67 次）。

## 4. 两项 P0 阻塞裁决（不移清就不能动 A/B 类）

### G18 `users.username` 唯一性语义

| 选项 | 内容 | 代价 |
|---|---|---|
| **A 对齐基线**（组合唯一） | 删全局唯一约束，建 `(tenant_id, username)` 唯一 | **代价比本规划初稿大且不可拆分**：登录在未指定租户时走 `UsernameEQ(username).Only(...)`（`handlers/common/service.go:195-199`），`.Only()` **多命中即报错** → 必须同步把登录改为「tenantCode/domain 必填或引入账号消歧」；另需复核注册查重（`handlers/auth/service.go:288-294`）、`pkg/bootstrap`、`cmd/initialize`、`cmd/provision_tenant`、`cmd/backfill_provider_admin`、邀请与 SSO 计划；邮箱仍保持全局唯一（上游同理，理由：找回密码按 email 单独定位） |
| B 维持全局唯一 | 保持现状 | 必须在 `AGENTS.md` 把 `users.username` **登记为例外并写明理由**（登录预认证阶段无法确定租户），否则「文档说组合唯一、代码全局唯一」的自相矛盾继续存在；同时每租户 `admin` 直觉不可得，上游多租户能力（含 `on_call` 排班组）吸收时需逐处改名适配 |

> 取证：本地 `ent/schema/user.go:21-23` = `field.String("username").Unique()`；
> 上游 `20261006_tenant_scope_user_username_unique.sql` 头注释给出症状与理由；基线出处 `AGENTS.md:245`。
> **结论修正（相对本规划初稿）**：这不是「选哪个更优雅」，而是**绑定决策** —— 唯一键口径与登录契约必须一起定，
> 任何一半单独实施都会让既有登录入口倒退（`.Only()` 直接失败）。详细论证与验收标准已登记 `07:G18`（§19）。

### G19 `cab` vs `change_review`（同能力两名）

四条 `/members` 端点方法与权限码完全一致（`change:read` / `change:write`，`router/cab_routes.go:15-18` ↔ 上游 `change_review_routes.go`），
表一个 `cab_member.go`、一个 `change_review_member.go`，上游另有 `20260921_drop_cab_members_table.sql`（注明 CAB 已重命名为 change_review）。
改名成本实测：后端 26 文件 / 前端 17 文件 / 文档与 manifest 5 文件。
→ 必须先定唯一命名（含前端入口、权限码、菜单 key、路由前缀、数据迁移方向），否则吸收即制造第二套名册；已登记 `07:G19`（§20）。
**裁决前禁止吸收 change 评审域任何文件**。

**决策单要求**（两项共用模板）：选项 → 影响面（带 `file:line` 证据）→ 迁移与回滚 → 验收判据 → 负责人 → 截止。

## 5. 阶段计划

| 阶段 | 内容 | 进入条件 | 退出判据 | 回滚 |
|---|---|---|---|---|
| **P0 门禁先行** | 实现 §2 L1 的面护栏 + `package.json` 回归契约 + 自证用例；登记 G18/G19 | 无 | ✅ **已完成**：门禁 15/15 绿、用例 10/10、真实树破坏演示三红两还原（§2「P0 完成状态」）；G18/G19 已登记 [`07-known-gaps.md`](../multi-tenant/07-known-gaps.md) §19/§20（含选项、实测代价、验收标准） | 撤脚本条目 |
| **P1 协议试点** | ✅ **已完成（2026-11-06）**：移植 `service/sla_breach_predictor.go`（156 行，纯 `fmt`/`math`）+ 5 条用例（4 条上游规格 + 1 条本地加固 `AdjustSpeedFactors`）；**未接线**（无路由/无调用方） | P0 绿 | ✔ 上游 4 条规格用例全过；`gofmt -l` 干净；**8 个新增标识符在 service 包内零冲突**；**本地加固**：`prioritySpeedFactor` 被 `AdjustSpeedFactors` 写、`Predict` 读，上游无锁（data race），已加 `RWMutex`（不改变任何输入→输出）；`test-coverage-guard` 满足（源码与测试同批）；契约门禁 15/15、面不低于附录 A 下限 | 单 PR revert（删 2 文件；无迁移、无接线、无外部副作用） |
| **P2 A 类铺开** | ✅ **`ai_quota` 已移植（2026-11-06，未接线）**：`service/ai_quota.go` 206 行纯标准库 + 6 用例（5 条上游规格 + 1 条本地补 `EstimateCost`）；SQL 兼容性已实测核对（本地 `ai_llm_calls.tenant_id` 由 `migrations/20261019_ai_llm_call_observability.sql` 增加）。✅ **`bpmn_template_digest` 已移植**：62 行纯标准库 + 3 条本地用例，前置 `bpmnTemplates` embed 本地已具备（20 模板），零新依赖/零迁移；**未接线**（是否纳入初始化组件 checksum 属版本账本变更，另立 PR）。❌ `service_quality_report` 经实测改判 B 类（缺 `DashboardViewer` 等 8 项）。→ 后续候选：BPMN `bpmn_template_digest` 的账本接线、`bpmn_compensation.go`（依赖 `service/bpmn` 与 ent）、C 类 CI 门禁 | P1 收口 | 每能力独立 PR + 前后对照表 +（若有接线）238 实测证据 | 逐能力 revert（互不依赖） |
| **P3 C 类资产** | 移植上游不依赖其实现的契约测试；引入 `fresh-install-gate` / `rls-golden-path` 两个 CI 门禁 | P2 至少 1 个能力收口 | CI 绿；**移植的每条测试均能独立跑绿且能故意打破本地实现而变红**（非装饰性断言）；无新增 flake | 撤 workflow 文件 |
| **P4 B 类差集** | `service/cloud`、`marketplace`、`connector/builtin`、`seeder`、`workbench`、`account` | **G18 已裁决** | 差集清单 + 逐点取舍记录 | 按域 revert |

> **P1 验证口径（须如实读）**：本地 `go test ./service` 因编译器内存上限（`runtime: cannot allocate memory`，
> 与代码无关）**未能执行**；改为把同一份代码/测试放入隔离包 `slaprobe`（仅 3 文件 + testify）实跑 —— **5/5 通过**，
> 跑完即删。**整包编译验证仍需 CI `backend-ci` 或 238 容器**（方法见部署证据 §6 R1：bind mount +
> `CGO_ENABLED=1`，注意 `.dockerignore` 剔除 `*_test.go` 的假通过坑）。

**每阶段硬规则**：一个能力一个 PR；PR 里禁止同时改唯一键/权限归属/RLS 边界；生成物一律本地重生成。

## 6. 风险登记

| 风险 | 具体形态 | 缓解 |
|---|---|---|
| **迁移撞号** | 上游 `013` 已被本地 `013_ticket_type_distribution_policies.sql` 占用；G17 的 DROP 迁移也自称 013 | 吸收/新增迁移一律**接续本地序号**，并在 PR 描述里登记映射表 |
| **checksum 账本漂移** | 历史上 prod 账本漂移 6 条导致全部 pending 不执行 | 每批迁移前后跑 `cmd/mig-verify -ro`（只读预检），零写入 |
| **双实现** | `cab`/`change_review`、两套 cloud 通道 | G19 先行；B 类必须先出差集清单 |
| **影子权限面** | 直接取 `rbac_precheck_gen.go` 会让 1123→891 | L1 条目 `contract-surface-rbac-precheck` 拦截 |
| **越权面扩大** | 为吸收功能放宽 RLS/跨租户读（G17 教训） | 任何 RLS 策略变更需 BD-1 评审 + 快照通道优先，禁止直接放宽读 |
| **前端倒退** | 取上游 Next.js 文件或 `next` 依赖 | L1 条目 `frontend-no-nextjs`；上游前端只当规格读 |
| **产物入库** | `itsm-cli/dist` 144 文件 | L1 条目 `no-vendored-cli-dist` |
| **静默降级** | 上游 DROP/退役类迁移照搬（如 `retire_legacy_sla_definitions`、`drop_legacy_workflow_tables`）会让本地功能"活着但降级" | D 类排除退役/删除型迁移；确需者单独立项并给行为对照测试 |

## 7. 需要人拍板的清单

| # | 决策 | 谁 | 阻塞什么 |
|---|---|---|---|
| 1 | G18 唯一性口径 | 架构 + 产品 | 全部 B 类、A 类中依赖多租户首管的项 |
| 2 | G19 CAB 命名统一 | 架构 | `change_review`、变更域相关吸收 |
| 3 | P1 是否批准（先吸收 5 个 `cmd/` 工具验证协议） | 你 | P2 起 |
| 4 | 382 个上游测试的移植准入标准（"不依赖其私有实现"如何判） | 架构 | P3 |
| 5 | `feat/upstream-heidsoft-integration` 是否推远端 / 何时并回 `feat/msp-p0-hardening` | 你 | 协作可见性 |

## 附录 A：指标口径（防后续争议，一条命令可复算）

```bash
for R in HEAD heidsoft/main; do
  printf "%-14s test函数=%s test文件=%s 路由=%s 权限预检=%s SQL=%s ent=%s 前端测试=%s\n" "$R" \
   "$(git grep -h '^func Test' $R -- 'itsm-backend/**/*.go' | wc -l)" \
   "$(git grep -l '^func Test' $R -- 'itsm-backend/**/*.go' | wc -l)" \
   "$(git grep -h -E '\.(GET|POST|PUT|DELETE|PATCH)\(' $R -- 'itsm-backend/router' | wc -l)" \
   "$(git grep -h -o '\"/api/v1[^\"]*\": *{' $R -- 'itsm-backend/middleware/rbac_precheck_gen.go' | wc -l)" \
   "$(git ls-tree -r --name-only $R -- itsm-backend/migrations | grep -c '\.sql$')" \
   "$(git ls-tree -r --name-only $R -- itsm-backend/ent/schema | grep -c '\.go$')" \
   "$(git ls-tree -r --name-only $R -- itsm-frontend/src | grep -cE '\.(test|spec)\.(ts|tsx)$')"
done
# 2026-11-06 实测：
# HEAD          3760 / 704 / 999 / 1123 / 129 / 172 / 375
# heidsoft/main 3965 / 723 / 745 /  891 /  75 / 136 / 239
```

口径说明：`test函数` 计 `^func Test`（不含 Example/benchmark）；`路由` 计 router 目录内方法注册调用；
`权限预检` 计生成文件里的路径键条目；`SQL` 计 `migrations/*.sql`（不含 `ent/migrate`）；
`前端测试` 按文件名模式统计（Vite/Next 目录形态不同，只作两侧同口径比较）。

## 附录 B：本规划自身的边界

- 未做任何代码吸收；未创建任何迁移；未改动 RLS/权限；
- 数字为**静态面**指标，不等于运行时正确性（运行正确性由 L2/L3 负责）；
- `heidsoft/main` 会持续前进（最近提交 2026-11-05），每阶段开始前需重跑附录 A 复算，避免按过期基线决策。
