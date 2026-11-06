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

### A 类｜纯新增，不触碰本地不变量（优先做）

| 能力 | 上游证据 | 本地现状 | 移植要点 |
|---|---|---|---|
| `on_call` 值班排班 | `handlers/on_call` 3 文件 + `router` 入口 `/api/v1/on-call/*` | **无该域** | 新域：schema/迁移需**本地重编号**（勿沿用上游号，见 §5 撞号风险）；权限码需过 D7 归属校验 |
| `cloudsignal` 云信号 | `handlers/cloudsignal` 20 文件 | 无 | 与工单/事件主流程接线；注意它依赖 `service/cloud` |
| `cmd/` 验证与维护工具 | `ai-eval`、`aliyun-oss-api-verify`、`aliyun-sdk-api-verify`、`aliyun-security-api-verify`、`service-request-maintenance` | 无 | **最低风险起步项**：独立 main 包，不碰契约面，适合验证协议是否顺手 |
| `ai_quota` / `sla_breach_predictor` / `service_quality_report` | 对应 `service/*.go` + 测试 | 无同能力 | 有本地既有口径（`ai_llm_calls`、SLA 定义授权），先差集再实现 |
| `bpmn_compensation` / `pending_inventory` / `template_digest` | `service/bpmn_*.go` | 本地有 BPMN 但无这三层 | 高价值；需与本地 `service/bpmn` 4 文件同源逻辑合并评审 |

### B 类｜差集扩展（触碰已有域，先测绘后动手）

`service/cloud`(29) ↔ 本地 `handlers/cloud`(3)；`service/marketplace`(13) ↔ 本地 `handlers/marketplace`；
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
| **A（推荐评估）** | 采纳 `(tenant_id, username)` 组合唯一，回归 `AGENTS.md:245` 基线 | 需迁移 + 重述 `07:G2` 的 `admin-<tenantCode>` 绕行；复核 `pkg/bootstrap`、`cmd/initialize`、`cmd/provision_tenant`、`cmd/backfill_provider_admin`、登录/找回/邀请全链；邮箱仍保持全局唯一（上游同样保留，理由：`handlers/auth/service.go` 按 email 单独定位） |
| B | 维持全局唯一 | 继续偏离仓库自身基线；每个新租户首管必须改名，`admin` 这一产品直觉永远不可得；上游相关能力（多租户交付、`on_call` 排班组等）移植时都会撞上 |

> 取证：本地 `ent/schema/user.go:21-23` = `field.String("username").Unique()`；
> 上游 `20261006_tenant_scope_user_username_unique.sql` 头注释给出症状与理由；基线出处 `AGENTS.md:245`。
> **注意**：本仓历史上多次出现"文档说 A 代码做 B"，此项必须先定，否则后续吸收会把不一致固化。

### G19 `cab` vs `change_review`（同能力两名）

四条 `/members` 端点权限完全一致（`change:read` / `change:write`），表一个 `cab_member.go`、一个 `change_review_member.go`，
上游另有 `20260921_drop_cab_members_table.sql`（注明 CAB 已重命名为 change_review）。
→ 必须先定唯一命名（含前端入口、权限码、菜单、数据迁移方向），否则吸收即制造第二套名册。

**决策单要求**（两项共用模板）：选项 → 影响面（带 `file:line` 证据）→ 迁移与回滚 → 验收判据 → 负责人 → 截止。

## 5. 阶段计划

| 阶段 | 内容 | 进入条件 | 退出判据 | 回滚 |
|---|---|---|---|---|
| **P0 门禁先行** | 实现 §2 L1 的面护栏 + `package.json` 回归契约 + 自证用例；登记 G18/G19 | 无 | ✅ **门禁部分已完成**（15/15 绿、10/10 用例、真实树破坏演示三红两还原，见 §2「P0 完成状态」）；⏸ G18/G19 待登记 | 撤脚本条目 |
| **P1 协议试点** | 吸收 `cmd/` 5 个验证/维护工具（A 类最低风险） | P0 绿 | L1+L2+L3 全绿；**每个新工具自带用例并通过 `test-coverage-guard`**；契约面不低于附录 A 下限 | 单 PR revert |
| **P2 A 类铺开** | `on_call` → `cloudsignal` → `ai_quota` / `sla_breach_predictor` / `service_quality_report` / BPMN 三件套 | G18/G19 至少 G19 已定 | 每能力独立 PR + 前后对照表 + 238 实测证据 | 逐能力 revert（互不依赖） |
| **P3 C 类资产** | 移植上游不依赖其实现的契约测试；引入 `fresh-install-gate` / `rls-golden-path` 两个 CI 门禁 | P2 至少 1 个能力收口 | CI 绿；**移植的每条测试均能独立跑绿且能故意打破本地实现而变红**（非装饰性断言）；无新增 flake | 撤 workflow 文件 |
| **P4 B 类差集** | `service/cloud`、`marketplace`、`connector/builtin`、`seeder`、`workbench`、`account` | **G18 已裁决** | 差集清单 + 逐点取舍记录 | 按域 revert |

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
