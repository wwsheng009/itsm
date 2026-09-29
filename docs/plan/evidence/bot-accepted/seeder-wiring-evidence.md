# Bot 功能 seeder 数据接线证据（B4-05 补强）

> 文档类型：实施证据（seeder 数据 + 装机链路接线）
> 关联方案：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（B2-01 / B3-03～B3-05 / B4-02 / B4-04）
> 关联报告：`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`
> 日期：2026-09-27
> 结论：**已交付并本机验证**（fresh install 与 `initialize apply` 重放一致；租户开通随克隆继承）

## 1. 背景与缺口

B0–B4 交付后存在三处「数据面」缺口，本任务补齐：

| # | 缺口 | 证据 |
| --- | --- | --- |
| 1 | `/ai/bot-metrics` 看板**无菜单种子行**（方案登记为 B4-02 条件项「菜单挂载点评审」） | seeder 菜单 specs 中无该 path；本机库 `/admin/bots` 菜单行也缺（老库未回填） |
| 2 | 内置「默认助手」仅在**首次列表访问时惰性种入**（`service/bot/admin.go:126-138`），装机不保证存在 | 本机 `GET /api/v1/admin/bots` 初始 0 条 |
| 3 | B3 场景 Bot（`SeedScenarioBots`）**无生产调用方**，仅测试引用；租户开通不克隆 Bot 模板 | `grep SeedScenarioBots` 仅 `service/bot/scenarios_test.go`；B4-04 残余风险「场景种子未接 provisioning」 |

另发现本机 Postgres（`127.0.0.1:15433/itsm_prod`）**缺 `bot_templates` 等 ent 表**（`ITSM_AUTO_MIGRATE=false` 且从未跑过 ent 基线），导致 `initialize apply` 首次失败（run 31，fail-closed 生效）。已用仓库自带基线工具补齐（见 §4）。

## 2. 改动清单（4 个代码文件）

| 文件 | 改动 |
| --- | --- |
| `itsm-backend/pkg/seeder/seeder.go` | ①菜单 specs 新增 `Bot 运行看板`（`/ai/bot-metrics`，父 `/ai/chat`，`ai:read`，SortOrder 116）——**菜单挂载点评审结论 = 挂 AI 助手子项**；②新增 `seedBotTemplates()`：默认助手（`SeedDefaultTemplate`）+ 三场景（`SeedScenarioBots`），目标 `default` 租户，幂等只增；③`SeedAll` 增加该步骤 |
| `itsm-backend/pkg/seeder/initialization_adapter.go` | 新增初始化组件 **`ai-bot-core`**（依赖 `identity-rbac`，checksum 独立）+ `verifyBotTemplates()`：默认助手存在 + 三场景模板与其定义授权逐一齐备，缺一即失败；`ProductionComponentNames` 同步追加 |
| `itsm-backend/pkg/seeder/tenant_provisioner.go` | `cloneTenantTemplates` 增加 Bot 模板与授权克隆（同 slug 跳过模板字段、授权按 `(bot, tool)` 去重），新租户随开通继承；复跑幂等 |
| `itsm-backend/pkg/seeder/bot_seed_test.go`（新增） | ①`TestSeedBotTemplatesSeedsAssistantAndScenarios`：模板 4 条 / 授权 12 条齐备 + 复跑幂等 + `verifyBotTemplates` 通过；②`TestCloneTenantTemplatesCopiesBotTemplates`：克隆数量一致 + 复跑幂等 |

设计口径：**只增不改**——管理员对模板/授权的显式修改优先；`ai-bot-core` 与 `seedMenus` 同为 `initialize apply` 的 reconcile 语义（存在即更新菜单字段，不存在则创建）。

## 3. 测试证据

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go build ./pkg/seeder/ ./service/bot/` | exit 0 |
| 2 | `go test ./pkg/seeder/ -count=1 -run 'TestSeedBotTemplatesSeedsAssistantAndScenarios\|TestCloneTenantTemplatesCopiesBotTemplates' -v` | **2/2 PASS**（2.005s） |
| 3 | `go test ./pkg/seeder/ -count=1`（全量对照） | 失败 5 例 = `TestSeedGroupsAndProvisionClones`、`TestProductionInitializersApplyAndVerifyCompleteDAG`、`TestProductionInitializersRepairMissingServiceCatalogWithoutOverwritingTenantCustomization`、`TestProvisionTenantReadinessAcrossDeploymentModes`、`TestProvisionTenantRollsBackWhenSourceTemplateIsIncomplete`——**与 B2-03 O-4 / M0-10 登记的既有失败同名同根因**（`resolve process definition incident_emergency_flow: ent: process_definition not found`），非本次引入；新增 2 例全绿 |
| 4 | `gofumpt -w` + `go build` | 干净 |

## 4. 本机回填与运行验证（2026-09-27）

| 步骤 | 命令 | 结果 |
| --- | --- | --- |
| ① 仅读预检 | `go run ./cmd/mig-verify -ro` | `applied=53 pending=0`（SQL 账本无欠账） |
| ② ent 基线 | `go run ./cmd/mig-verify -entbaseline -up` | `ENT BASELINE created (Schema.Create)`；补齐 `bot_*` 等 ent 表；`APPLIED 0 migration(s)` |
| ③ 种子重放 | `go run ./cmd/initialize -action=apply -release-version=local-2026-09-27` | `runId=32 status=succeeded`（含新组件 `ai-bot-core` 的 apply+verify） |
| ④ 运行验证（API，管理员会话） | `GET /api/v1/admin/bots` + `/grants` | `templates=4`：`default-assistant ga act_low grants=0`；`s1-ticket-assistant pilot grants=5`；`s2-incident-oncall pilot grants=5`；`s3-knowledge-assistant pilot grants=2`（合计 12 条，与 `ScenarioBots()` 定义一致） |
| ⑤ 菜单验证 | `GET /api/v1/auth/menus` | `has_bot_metrics=true`、`has_admin_bots=true`（`/admin/bots` 行由本会话早前经 `/api/v1/menus` 补录，id=313） |

> 说明：③ 之前的一次 apply（run 31）因本机缺 ent 表而失败——`ai-bot-core` 的 verify 在事务内拦截并整体回滚，属**预期的 fail-closed** 行为，非缺陷。

## 5. 残余与边界

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 存量失败 5 例 | `pkg/seeder` 的 `incident_emergency_flow` 系列（B2-03 O-4 / gap S5）；建议独立任务处置，不属本次范围 |
| 2 | 本机 DB 迁移路径 | 生产/CI 仍以 SQL 迁移账本为准；本次 ent 基线仅面向已存在的开发库（`mig-verify -entbaseline` 为仓库既有工具） |
| 3 | 场景模板可见性 | S1/S2/S3 为 `pilot` + `internal/all` 受众，管理页按受众过滤；终端用户不可见属预期 |
| 4 | `/ai/bot-metrics` 菜单 | 已随 `seedMenus` 幂等上架；已存在的旧租户在下次 `initialize apply`/重跑种子后生效（本机已生效） |
