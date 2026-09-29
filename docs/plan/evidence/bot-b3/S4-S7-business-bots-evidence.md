# S4–S7 业务 Bot 模板扩展证据（含 Bot 与 AI Provider 关系说明）

> 文档类型：实施证据（业务模板扩展 + 测试 + 装机验证 + Provider 配置口径）
> 关联方案：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（B3-03～B3-05 场景家族；B4-04 S4/S5 评估）
> 关联证据：`docs/plan/evidence/bot-b3/B3-scenario-pilots-acceptance.md`（S1～S3 验收单）、`docs/plan/evidence/bot-accepted/seeder-wiring-evidence.md`（种子接线）
> 日期：2026-09-27
> 结论：**已交付并本机验证**（`integration_verified`：定义/种子/策略矩阵/装机重放四层证据齐备；pilot 语义，正式发布由管理员显式改 `ga`）

## 1. 背景与范围

S1～S3（工单助手 / 事件值班 / 知识自助）覆盖「处理中 + 现场 + 自助」三段。本次按 ITSM 业务链路补齐四段**新角色场景**，全部基于**现有 17 个内置工具**（B0-01 标注），不新增工具、不改运行时：

| 业务段 | 新模板 | 服务角色 |
| --- | --- | --- |
| 变更前评估 | S4 变更影响分析助手 | 变更经理 / CAB 证据链 |
| 事件后复盘 | S5 事件复盘助手 | 事件经理 / 知识沉淀 |
| 服务台受理 | S6 服务台受理助手 | 一线坐席（查重→建议→建单） |
| 工单质量治理 | S7 工单质量巡检助手 | 服务台班长 / 数据治理 |

与 B4-04《S4/S5 评估》的关系：那份结论针对「**变更方案助手**（需变更域写工具 + CAB 审批语义）」与「**员工自助**（需服务目录/服务请求工具）」——均依赖**尚不存在**的工具，暂缓/下期结论不变；本次交付的是**可用现有工具闭环**的相邻场景（影响分析、复盘、受理、质量），不触碰其结论。

## 2. 模板定义矩阵（`service/bot/scenarios.go`）

| # | slug | 名称 | 受众 | 风险上限 | 入口 | 授权（风险） |
| --- | --- | --- | --- | --- | --- | --- |
| S4 | `s4-change-impact-analyst` | S4 变更影响分析助手 | internal | `plan` | `chat` / `ci_detail` | `list_cis`(read)、`get_ci`(read)、`get_ci_relationships`(read)、`get_ci_tickets`(read)、`get_ci_impact`(plan)、`analyze_ci_impact_plan`(plan)、`list_tickets`(read)、`get_incident_stats`(read) |
| S5 | `s5-incident-postmortem` | S5 事件复盘助手 | internal | `plan` | `chat` / `incident_detail` | `list_tickets`(read)、`get_incident_stats`(read)、`list_cis`(read)、`get_ci_tickets`(read)、`get_ci_impact`(plan)、`draft_kb_article`(plan) |
| S6 | `s6-service-desk-intake` | S6 服务台受理助手 | internal | `act_low` | `chat` / `ticket_list` / `incident_create` | `list_tickets`(read)、`list_kb`(read)、`get_incident_stats`(read)、`draft_ticket_fields`(plan)、`create_ticket`(act_low) |
| S7 | `s7-ticket-quality-auditor` | S7 工单质量巡检助手 | internal | `act_medium` | `chat` / `ticket_list` / `ticket_detail` | `list_tickets`(read)、`draft_ticket_fields`(plan)、`update_ticket`(act_medium) |

**业务边界（写进定义并被测试锁定）**：

- S4/S5 **零写入**（授权风险 ≤ plan，工具集与写工具零交集）；S5 的 `draft_kb_article` 只落 `bot_artifacts`，**不发布**知识。
- S6 写面**仅** `create_ticket`（Gate3 人工确认后落库）；不含改单/删除类。
- S7 写面**仅** `update_ticket`，且不授任何 `act_high`/删除类（`create_ticket_type`/`create_ci_relationship`/`delete_ci_relationship` 零交集）。
- 四个模板均为 `pilot`；**pilot 与 ga 在策略层等价**（仅 `draft` 被拒，见 `service/bot/policy.go:177`），`ga` 仅为治理语义标记。

## 3. 风险口径（`act_medium` 的最小必要说明）

原 B3 口径为「pilot 期统一 act_low」。本次调整为**场景最小必要**：

| 上限 | 采用场景 | 依据 |
| --- | --- | --- |
| `plan` | S4/S5 | 只读 + 计划类产物；`plan` 是能表达「可读+可产草案、不可写」的最小级别 |
| `act_low` | S1/S2/S3/S6 | 写工具自身即 `act_low`（`create_ticket`/`link_ticket_ci`） |
| `act_medium` | S7 | 唯一写工具 `update_ticket` 自身标注即 `act_medium`（`service/tool_registry.go:665`）；**不取其下级别**（会导致授权永不放行），**不取其上级别**（无任何 `act_high` 工具被授予） |

四级防线未变：模板/授权风险上限 ∩ RBAC ∩ 入口 ∩ Gate3 人工确认；所有写操作仍需用户确认后由队列执行（非聊天链路内直接写入）。

## 4. 测试证据

| # | 用例 | 覆盖面 | 结果 |
| --- | --- | --- | --- |
| 1 | `service/bot`: `TestScenarioBots_Definitions` | 7 个 slug / 风险矩阵一致；每场景写面与期望**完全一致**（`ElementsMatch`）；S4/S5 零写入；S6 仅 `create_ticket`；S7 仅 `update_ticket` 且不触 `act_high`；授权上限 ≤ 模板上限 | ✅ `go test ./service/bot/ -count=1` → ok 7.042s |
| 2 | `service/bot`: `TestSeedScenarioBots_IdempotentAndNonOverwriting` | 种子幂等（7 模板 / 34 授权；复跑 0 新增）、管理员改动不被覆盖 | ✅ 同上 |
| 3 | `handlers/ai`: `TestScenarioBots_GrantsExistInRegistryAndDecideAllows` | **三方交叉**：①每条授权在真实注册表存在；②工具风险 ≤ 授权上限 ≤ 模板上限；③声明入口下 `Decide` 全放行；④未授权工具一律 `tool_not_granted`；⑤未声明入口一律 `entrypoint_denied` | ✅ 7 子测试全通过（`go test ./handlers/ai/ -run TestScenarioBots -v`） |
| 4 | `pkg/seeder`: `TestSeedBotTemplates*` / `TestCloneTenantTemplatesCopiesBotTemplates` | 种子装配（含新场景）与租户开通克隆沿用动态断言（`1+len(ScenarioBots())`） | ✅ ok 1.152s |

> 用例 3 的价值：把「业务模板」与「工具注册表」锁死——注册表改名/调级会让场景定义立即变红，避免种子指向不存在的工具而静默空授权。

## 5. 本机重放与运行验证

| 步骤 | 命令 / 请求 | 结果 |
| --- | --- | --- |
| 重放 | `go run ./cmd/initialize -action=apply -release-version=local-2026-09-27.2` | runId=33 succeeded；`bot templates seeded: templates_created=4, grants_created=22, grants_skipped=12`（12 = S1～S3 既有授权，幂等跳过） |
| 验证 | `GET /api/v1/admin/bots` + `/grants` | `templates=8`（默认助手 + S1～S7）；授权数 `0/5/5/2/8/6/5/3`；风险上限 `act_low/act_low/act_low/act_low/plan/plan/act_low/act_medium` |

### 5.1 浏览器实测（`http://localhost:3000/admin/bots`）

| # | 观察项 | 结果 |
| --- | --- | --- |
| 1 | 列表渲染 | 8 行：默认助手 + S1～S7；S4/S5 风险列「规划」（plan）、S6「低风险写」、S7「中风险写」（act_medium）；入口列与定义一致（S4 `chat,ci_detail`；S6 `chat,ticket_list,incident_create`；S7 `chat,ticket_list,ticket_detail`） |
| 2 | S4 授权抽屉 | 8 条授权，风险标签正确（`analyze_ci_impact_plan`/`get_ci_impact`=规划；其余只读），抽屉顶部提示「模板风险上限：规划」 |
| 3 | S7 授权抽屉 | 3 条授权：`update_ticket`=中风险写、`draft_ticket_fields`=规划、`list_tickets`=只读；抽屉顶部提示「模板风险上限：中风险写」 |
| 4 | 工作区选择器 | `GET /api/v1/agent/bots` 返回 `code=0`、8 条（audience/入口允许的子集），修复后非空 |

### 5.2 契约缺陷与修复（本次一并闭环）

**现象**：`/admin/bots` 页面 HTTP 200 但表格恒为「暂无 Bot 模板」；`/api/v1/agent/bots` 同样为空。

**根因**：`handlers/ai/bot_admin.go`（B2-01）所有响应为**裸 JSON**（`{"items":…}` / `{"error":…}`），而前端 `http-client.ts:529` 统一按 `{code,message,data}` 包络只解包 `data`（MCP/LLM Provider 等管理端点均用 `common.Success`）→ `data` 为 `undefined`，列表静默为空。

**修复**（`bot_admin.go`）：

- 成功响应统一 `common.Success(c, …)`（列表/详情/授权/删除）；创建保留 **201** 语义并携带同一包络（`common.Response{Code:0,Message:"success",Data:…}`）；
- 错误响应统一 `common.Fail(c, code, message)`（未找到 404 / 校验 400 / 内部 500；租户上下文缺失 401）；
- 契约由此与其余管理端点、`itsm-frontend/src/lib/api/bot-api.ts` 的 `httpClient` 解包口径一致。

**回归**：`handlers/ai/bot_admin_test.go` 的 `decodeBody` 改为断言 `code=0` 并解包 `data`（列表/创建/更新/授权/详情全覆盖）；`go test ./handlers/ai/ -run TestBotAdmin` + `./router/` 全绿；后端 `main.exe` 重建重启后实测 `GET /api/v1/admin/bots` → `code=0, items=8`，页面 8 行与抽屉正常。

> 说明：前端组件单测此前只 mock `botApi`（未覆盖真实 HTTP 解包），故 B2-03 未暴露该缺陷；本次以浏览器实测闭环。

## 6. Bot 与 AI Provider 的关系（现状口径 + 配置方法）

**结论：当前 Bot 模板与 AI Provider 之间没有绑定关系。** Bot 决定「工具面 / 入口 / 风险上限」，Provider 在**每次请求**上解析。

**现网解析链**（优先级从高到低，`service/llm_registry.go:615` `Resolve`；来源常量见同文件 `:520-527`）：

| 级别 | 来源 | 配置入口 |
| --- | --- | --- |
| ① 请求级 | `provider` 字段（聊天请求体，`handlers/ai/handler.go:146-148`、`:276-278`） | 系统管理员显式覆盖；解析失败**不回退**（`handler` 映射 404/409/422/503，见 `handlers/ai/service.go:969-997`） |
| ② 个人默认 | `llm_user_preferences.provider_key` | 前端 AI 助手右上角切换 / `PUT /api/v1/ai/user-preference`（写端点仍要求 `system:write`；`GET` 全员可用） |
| ③ 租户默认 | `llm_provider_configs.is_default=true` 且 `enabled`、未软删 | 管理面「系统配置 → LLM Providers」（`/admin/system-config`） |
| ④ 静态兜底 | `config.yaml` / 环境变量（`AI_PROVIDER*`） | 部署配置 |

**为什么 Bot 无需（当前也不）绑 Provider**：工具面由 Bot 决定、模型能力由 Provider 决定，两者解耦可让同一 Bot 在不同租户用不同模型；`bot_templates.system_prompt_ref` 目前也**仅管理面存储**（`service/bot/admin.go:72/255/303`），运行时未消费。

**若要「按 Bot 绑定 Provider/模型」**（例如 S5 复盘默认走长上下文模型），改造点为三处：
1. 数据：`bot_templates` 增 `provider_key`（可空）与可选 `model`（`ent/schema/bot_template.go`）；
2. 解析：`Service.ResolveChatProvider`（`handlers/ai/service.go:977`）在调用 `llmGateway.ResolveRequest` 前插入「Bot 绑定」层（新增 `ProviderSourceBot`，优先级置于请求级之后、个人默认之前），并沿用「显式指定失败不回退」的语义；
3. 管理面：模板 DTO/管理页增加字段并校验（实例存在、`enabled`、协议已实现）。

> 该改造未在本批次实施（属新特性，需产品确认优先级）；本批次仅确认现状口径并预留插入点。

## 7. 残余与边界

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | S4 无 `change_detail` 入口 | 已知入口仅 `chat/ticket_detail/ticket_list/incident_detail/incident_create/ci_detail`（`service/bot/scope.go:22-28`）；变更域入口需与变更模块联调（列入后续候选） |
| 2 | S6/S7 依赖 RBAC 授权面 | 授权仅在角色具备对应权限时生效（交集门禁）；管理员可在 `/admin/bots` 按角色实际权限裁剪 |
| 3 | S7 为唯一 `act_medium` 预置 | 若组织暂不放开 `update_ticket`，可直接停用该模板（不影响其余 7 个）或降级其授权 |
| 4 | 浏览器端人工冒烟 | 列表与授权抽屉已实测（§5.1，2026-09-27）；截图未入库（会话内留存），长期证据建议纳入 BT-09/B4-01 浏览器通道 |
