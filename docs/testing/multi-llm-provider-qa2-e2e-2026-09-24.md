# 多 LLM Provider · QA-2 端到端验收记录（API 级）

> 任务：`docs/plan/multi-llm-provider-plan.md` §5.3 QA-2 ——「端到端验收（多协议真实/桩服务）：chat completions 桩（variant 空 + `azure`）+ anthropic messages 桩（variant `minimax`）各建实例，切换验证；未实现协议拒绝路径验证；`openai_chat_completions` 适配器路径与旧分支等价对照」
> 执行日期：2026-09-24；环境：Windows / pwsh，工作树（`itsm-backend`），**不连生产库、不连真实 LLM**
> 结论：**✅ 自动化口径通过**——新增 API 级链路用例 `TestLLMProviderQA2RuntimeSwitchFlow`，把「管理 API 写入 → 缓存失效 → 运行期解析 → 真实 HTTP 出站」串成单条链（内存 SQLite + `httptest` 桩），覆盖 §6.2 场景 1-6、10 与删除 / 禁用失败路径；场景 7/8/9/10/11 的其余口径由 QA-1 单测、QA-3 门禁与适配器对照测试承接（逐项见 §2）。
> 遗留（需真实运行环境，人工执行）：① 浏览器级视觉复核；② 真实 Ollama / Anthropic 桩联调；③ 生产日志明文检索。前置条件、步骤与通过标准见 §5。

## 1. 环境与接线（为什么不是「起服务点页面」）

| # | 说明 |
|:--|:--|
| 1 | **不连生产库**：`.dev/dev.ps1` 的默认配置指向 prod 库，且 prod 侧本方案两条迁移尚未随发布应用（§4.4⑤ R4），因此 QA-2 一律使用**内存 SQLite**（`enttest.Open`，每用例独立 DSN `file:ai_admin_test_N?mode=memory&cache=shared&_fk=1`）。 |
| 2 | **不连真实 LLM**：所有出站端点均为 `httptest` 桩（固定正文 `PONG-*` + 调用计数 + 末次请求路径），避免外部依赖与费用；真实供应商联调列入 §5 遗留项。 |
| 3 | **接线与生产同构**（`newQA2FlowEnv`）：`service.NewLLMProviderRegistry(client, cipher, staticCfg, logger)` 同时作为管理服务的 `Invalidator`（对应 `internal/bootstrap/app.go` 的生产装配，写后即刻失效）；`service.NewLLMGateway(...).WithResolver(registry)` 接上 §3.3 解析链（含个人默认自动接线）。 |
| 4 | **静态回退可观测**：静态配置指向第三个桩（`stubStatic`），任何「不该回退」的路径都会留下调用计数，可直接断言为 0。 |
| 5 | **与既有单测的分工**：既有 registry / gateway / admin 单测各自验证单层语义；本用例专门覆盖**跨层装配**（写库 → 缓存失效 → 解析 → 出站），补「各层各自正确但装配断线」的盲区。 |
| 6 | 用例位置：`itsm-backend/handlers/ai/llm_provider_qa2_flow_test.go`（`package ai_test`；复用同包 `llm_provider_admin_test.go` 的 `llmAdminTestDSN` / `llmAdminTestCipher` / `llmAdminTestAudit` / `validCreateRequest` / `llmAdminTenantID` / `llmAdminUserID` / `llmAdminBoolPtr`）。 |

## 2. §6.2 场景覆盖矩阵

| 场景 | 计划要求 | 覆盖方式与证据 | 状态 |
|:--|:--|:--|:--|
| 1 | 创建实例 A（variant 空）与 B（`variant=ollama`），均测试连通成功 | A 全链路：`TestLLMProviderQA2RuntimeSwitchFlow`（创建 `openai_chat_completions` + 默认 + `TestProvider` 真实出站，断言 `testResp.OK` 且 `stubA.callCount()≥1`）；B 在本用例中以默认变体创建（覆盖请求级切换）；`variant=azure` / `ollama` / `minimax` 的构建映射由 `TestLLMProviderRegistryProtocolVariantMapping`（`service/llm_registry_test.go:966`）锁定，**桩侧 Ollama 实例连通留 §5.2 人工** | ⚠️ 部分（自动化主干通过，Ollama 桩连通待人工） |
| 2 | 设 A 为租户默认；不传 provider → 命中 A（`providerSource=tenant`，`provider_key` 落库） | 同用例：断言 `res.Source==ProviderSourceTenant`、`res.Key=="qa2-a"`、正文 `PONG-A`；`provider_key` 写入由 BE-6 单测（`TestAIMetrics*` / telemetry provider-key 用例）与 QA-3 §1 行 5 承接 | ✅ 通过（组合证据） |
| 3 | 会话选择器切到 B → 命中 B（`providerSource=request`）；B 不支持流式时按既有降级路径产出一次性回答 | 后端解析：同用例断言 `ProviderSourceRequest` / `res.Key=="qa2-b"` / `PONG-B` 且静态桩零调用；非流式降级路径由既有 gateway 降级实现（`service/llm_gateway.go` 非流式降级）与前端 `ai-api.test.ts`（降级 `AIApi.chat` 请求体用例）承接；**选择器交互与降级呈现留 §5.1 人工** | ⚠️ 部分（后端通过，UI 交互待人工） |
| 4 | 设「我的默认 = B」→ 新会话不传 provider 命中 B（`providerSource=user`） | 同用例：`SetUserPreference` 后断言 `ProviderSourceUser` / `res.Key=="qa2-a"`（用例内 A/B 角色与计划相反，语义等价：个人默认优先于租户默认） | ✅ 通过 |
| 5 | 禁用 B → 下次请求降级租户默认 A；直接 `provider=B` 返回 409 `AI_PROVIDER_DISABLED` | 同用例（角色互换）：禁用后显式引用断言 `errors.Is(err, service.ErrProviderDisabled)`；个人默认指向禁用实例时降级 `ProviderSourceTenant`；HTTP 409 映射由 `TestChatProviderOverrideDisabledIsVisible`（`handlers/ai/llm_provider_chat_test.go:118`）覆盖；**会话页失效提示留 §5.1 人工** | ⚠️ 部分（后端通过，失效提示待人工） |
| 6 | 删除全部 DB 实例并关闭开关 → 回到静态配置单 provider（现状），重启后与线上一致 | 前半：同用例无实例租户断言 `ProviderSourceStatic` / `res.Key=="openai"` / 静态桩被调用；后半（开关关闭零装配、重启一致）由 QA-3 承接——开关语义、bootstrap 装配门禁（`internal/bootstrap/app.go:987-993`）、`/ai/providers` 404、`NewProviderFromConfig` 回退旧分支 | ✅ 通过（组合证据） |
| 7 | 安全断言：`GET /ai/providers` 仅掩码；日志全文无明文；占位符 + DB 可用实例启动成功 | 掩码：`TestLLMProviderAdminCreatePersistsEncryptedKeyAndMaskedDTO`（创建 + 列表断言 `****`、无 `enc:`、无明文）、`TestLLMProviderAdminHandlerEnvelope`（HTTP 信封断言）；启动矩阵：`TestEvaluateLLMKeyStartupMatrix`（`service/llm_provider_startup_test.go`，§4.1 矩阵 5 行逐行锁定）；**运行期日志全文检索留 §5.3 人工**（单测用 `zap.NewNop()`，不产生可检索日志） | ⚠️ 部分（掩码/矩阵通过，日志检索待人工） |
| 8 | 权限边界：非系统管理员看不到页签；三个 GET 403；`POST /ai/chat` 带 provider 403 `AI_PROVIDER_FORBIDDEN`；不带 provider 与现状一致 | 后端：`TestChatProviderOverrideForbiddenForNonSystemAdmin`（`llm_provider_chat_test.go:94`）+ 路由声明级守卫 `go test ./router/ -run 'TestWriteRoutesRequirePermission\|TestRoutePermissionCodesAreDefined'`；前端：QA-3 §1 行 9（页签 gating 用例 `should gate LLM provider tab by system:write (FE-3)`、探测 fail-closed 零请求）；**浏览器可见性复核留 §5.1 人工** | ⚠️ 部分（声明/单测通过，浏览器复核待人工） |
| 9 | 幂等导入：两次 `import-static` → 第一次创建、第二次 `{updated:true}`，记录数不增 | `TestLLMProviderAdminImportStaticIdempotent`（`llm_provider_admin_test.go:640`，断言 `first.Created==true`、`second.Updated==true`） | ✅ 通过（QA-1 用例） |
| 10 | 协议槽位与映射：① `anthropic_messages`+`variant=minimax` 建实例走 minimax 分支；② 未实现协议 422；③ 非法 variant / 敏感键 422 | ②③ 与映射表：`TestLLMProtocolSlot*`、`TestValidateAdapterOptions`、`TestIsSensitiveAdapterOptionKey`、`TestLLMProtocolOptions`、`TestLLMProtocolCapabilities`（本轮复跑全绿）；`openai_chat_completions` 真实出站与协议回带：`TestLLMProviderQA2RuntimeSwitchFlow`（断言 `res.Protocol==LLMProtocolOpenAIChatCompletions`）；① **minimax 分支出站留 §5.2 人工**（需 Anthropic/Minimax 形态桩） | ⚠️ 部分（槽位/422 通过，minimax 出站待人工） |
| 11 | 协议适配器等价性：同桩下适配器路径与旧分支路径逐项一致 | `internal/llm/protocol/equivalence_test.go` 4 个对照用例：`TestEquivalenceNonStreamChat` / `TestEquivalenceStreamWithTools` / `TestEquivalenceToolCallConsistency` / `TestEquivalenceErrorMapping`（BE-9 交付，本轮随 `go test ./internal/llm/protocol/...` 复跑；另有 `TestNewProviderFromConfigSwitchOffKeepsLegacyProviders` 锁定开关关闭回退）；**人工抽查留 §5.2** | ✅ 通过（自动化对照）；人工抽查可选 |

## 3. 复现命令（本轮实测）

```powershell
# --- 后端（workdir: itsm-backend）---
go test ./handlers/ai/ -run TestLLMProviderQA2RuntimeSwitchFlow -count=1 -v   # PASS（QA-2 新增用例，1.3s）
gofmt -l handlers/ai/llm_provider_qa2_flow_test.go                            # 无输出
go test ./handlers/ai/ -count=1                                               # ok（6.6s，含既有管理面/会话覆盖用例）
go test ./handlers/ai/ ./router/ ./internal/bootstrap/ ./middleware/ -count=1 # 四包 ok（15.8s / 11.1s / 2.5s / 4.9s）
go test ./service/ -run 'LLM|Provider|Protocol|Registry|Gateway' -count=1     # ok（23.8s）
go test ./service/ -run 'TestLLMProtocolSlot|TestValidateAdapterOptions|TestIsSensitiveAdapterOptionKey|TestLLMProtocolOptions|TestLLMProtocolCapabilities' -count=1   # ok
go test ./internal/llm/protocol/... -count=1                                  # ok（0.9s，含 4 个等价性对照用例）
```

> 说明：以上均为**工作树复跑**结果；QA-2 用例依赖内存库与 `httptest`，可在无 DB / 无网络的开发机上重复执行。

## 4. 关键断言链（单用例内按序）

| # | 动作（管理面） | 断言（运行期） |
|:--|:--|:--|
| 1 | `CreateProvider("qa2-a", 默认, 协议 openai_chat_completions)` | `IsDefault=true`、`Enabled=true`、`Protocol` 回带 |
| 2 | `TestProvider(A)` | `OK=true`、`status=ok`、桩 A `callCount≥1`（证明真实出站而非仅写库） |
| 3 | 无覆盖请求（`ProviderRequest{TenantID}`） | 正文 `PONG-A`、`Source=tenant`、`Key=qa2-a`、`Protocol=openai_chat_completions`、末次路径以 `/chat/completions` 结尾 |
| 4 | `CreateProvider("qa2-b")` 后带 `Key=qa2-b` 请求 | 正文 `PONG-B`、`Source=request`、静态桩 `callCount==0`（**显式覆盖不得回落**） |
| 5 | `SetDefaultProvider(B)` 后无覆盖请求 | 正文 `PONG-B`、`Source=tenant`（切换默认即刻生效，无需重启/等 TTL） |
| 6 | `SetUserPreference(user=7 → qa2-a)` 后请求带 `UserID=7` | 正文 `PONG-A`、`Source=user`（个人默认优先于租户默认） |
| 7 | `UpdateProvider(A, Enabled=false)` 后带 `Key=qa2-a` 请求 | `errors.Is(err, ErrProviderDisabled)` 可见失败、`res.Source=request`（失败仍回带来源） |
| 8 | 同禁用态下带 `UserID=7`（个人默认指向 A）请求 | 降级命中租户默认 B：正文 `PONG-B`、`Source=tenant` |
| 9 | `DeleteProvider(A)` 后带 `Key=qa2-a` 请求 | `errors.Is(err, ErrProviderNotFound)`（不静默回退）、`res.Key=qa2-a` |
| 10 | 无实例租户（`TenantID+1`）无覆盖请求 | 正文 `PONG-STATIC`、`Source=static`、`Key=openai`、静态桩被调用 |

> 第 4/7/9 条分别为「显式覆盖」「禁用」「删除」三条**可见失败 / 不回退**语义的端到端锁定；第 6/8 条锁定 `个人默认 → 租户默认 → 静态` 解析链的两级降级。

## 5. 遗留人工步骤（~~未执行~~ → ✅ 2026-10-11 真实环境补齐回填）

> 以下三项因环境不满足（无本地 Postgres、无真实供应商凭据、无容器运行时）未在 2026-09-24 轮执行；**已于 2026-10-11 在本地开发栈补齐**（真实后端进程 `LLM_MULTI_PROVIDER_ENABLED=1` + `LLM_PROTOCOL_ADAPTER_ENABLED=1`、Vite dev :3000、形态桩 `127.0.0.1:19091`——openai chat completions / ollama `/api/chat` / anthropic messages 三形态；截图 `.tmp/web-check/qa2/`，未跟踪）。执行细节与快照见各节；**QA-2 至此完整通过，B7 出口条件满足**。

### 5.1 浏览器级视觉与交互复核（对应场景 3/5/7/8 的 UI 口径）—— ✅ 通过（含 1 项口径演进）

- ① 系统管理员「系统管理 → 系统配置 → LLM 模型」页签可见（多 Provider 开启判定生效），创建 `qa2-a`（指向本地桩）→ 测试（**连通正常**，真实出站命中桩）→ 设为默认（toast + 「租户默认」徽标迁移，刷新复核）→ 禁用（API，`available` 收缩）→ 删除全链 ✅；截图 `01/02/05/06/07-*.png`。删除守卫顺带取证：删除默认实例 → **409 `AI_PROVIDER_IS_DEFAULT`** 可见拒绝，恢复默认后删除成功。
- ② 未实现协议置灰：**口径演进**——PA-4 收官后 4 值枚举全部 `implemented=true`（`service/llm_protocol_slot.go:21-22`），FE 按 `disabled: !implemented` 渲染 → 当前**无置灰项属正确行为**（置灰机制就位、数据集为空）；密钥单向写入实测：创建后列表仅显掩码 `Qa2S****9a2c`，弹窗标注「仅写入加密存储，不回显明文」✅；422 可读错误映射由 FE 错误码转译实现 + `llmProviderErrorCode` 单测承接（本轮无未实现协议可触发，未重复造桩）。
- ③ 会话页选择器：仅开关开启且实例 ≥2 时渲染 ✅（本租户 4 实例）；显式选择 `qa2-b` 后发送 → 回答 **`PONG-qa2-b`**（来自桩）+ 气泡生效实例标签 **「由 QA2 Stub B 回答」** ✅；截图 `08-chat-provider-label.png`。
- ④ 禁用所选实例（`PUT /providers/4 {enabled:false}`）后再次发送 → **失效回退生效**：回答 `PONG-qa2-a` + 标签「由 QA2 桩 A 回答」（回落租户默认）✅；截图 `09-chat-fallback-disabled.png`。
- ⑤ 普通用户（tenant 1 探针 technician `qa2_probe_user`，无 `system:write`）：管理页签不可见（直达 `/admin/system-config` 无 LLM 页签、无管理功能菜单）✅；**选择器可见 = 口径演进**——P1（2026-09-26）决策「选择器读端点降 `ai:read`、会话页全员切换」（api-reference LLM 章节权限注记），本报告 §5.1⑤ 原预期写于该决策之前；按现行口径复核：探针会话仅命中 `available`/`user-preference` 两个 **`ai:read`** 读端点 + conversations，**零管理端请求**（无 `GET /ai/providers` 管理列表）✅；截图 `11-probe-user-negative.png`。

### 5.2 真实桩联调（对应场景 1 的 `variant=ollama`、场景 10① 的 `anthropic_messages` + `minimax`、场景 11 人工抽查）—— ✅ 通过（适配器关侧见注）

- ① `openai_chat_completions + variant=ollama`（endpoint 指向桩）→ 连通性测试 **ok=True**：命中桩 `/api/chat` 形态端点（桩仅三个 POST 路径，错误路径 404 → 失败；200 即形态命中）✅。
- ② `anthropic_messages + variant=minimax`（endpoint 指向桩 `/v1/messages` 形态）→ 测试 **ok=True** ✅；`endpoint 留空回退内置地址` 变体未测（需真实 Minimax 凭据，环境受限——回退逻辑由 `NewProviderFromConfig` 单测与生产默认路径长期运行承接）。
- ③ 适配器开/关对照抽查：**开侧（`LLM_PROTOCOL_ADAPTER_ENABLED=true`）**本会话全程验证（创建/测试/切换/流式回答均经适配器分派）；**关侧**未重启重跑——由 `internal/llm/protocol/equivalence_test.go` 4 个对照用例（同一桩下非流式正文/流式增量/工具调用累积/错误身份逐项比对）+ 生产默认路径（适配器开关默认关）持续运行承接，无偏差。
- 管理面管理 API 建实例（qa2-b/ollama/anthropic 经 `POST /ai/providers`）与 UI 建实例（qa2-a 经表单）双路径均验证。

### 5.3 运行期日志明文检索（对应场景 7 的日志口径）—— ✅ 通过

- 步骤：本轮共输入 4 个明文密钥（qa2-a/qa2-b/qa2-ollama/qa2-anthropic，值形如 `Qa2StubSecretKey_*`），全部创建/更新/测试/列表操作在当前进程（`logs/itsm.log` 覆盖窗口）内执行。
- 结果：4 个明文值 **全部 0 命中**；`enc:` 密文前缀 **0 命中**；`"apiKey"` JSON 字段 **0 命中**（日志不落密钥字段）。满足「零命中」通过标准，R-1 无需提级。

## 6. 本轮发现与修正

| # | 发现 | 判定与处置 |
|:--|:--|:--|
| 1 | `service/llm_protocol_slot_test.go` 的 `TestLLMProtocolOptions` 假定槽位顺序为 `openai_chat_completions → openai_responses → anthropic_messages → google_gemini`，与实现（`service/llm_protocol_slot.go:94` `llmProtocolSlots`）及主计划 §3.1.4 映射表行序 `openai_chat_completions → anthropic_messages → openai_responses → google_gemini` 不一致 | **测试断言错误**（非实现回归：实现顺序 = 计划口径）。已按实现/计划修正断言与索引，并在用例内注明顺序口径来源；未改任何实现代码 |
| 2 | 该断言此前未暴露：QA-3 定向集（`TestLLMProtocolSlot\|TestValidateAdapterOptions\|TestIsSensitiveAdapterOptionKey`）不含 `TestLLMProtocolOptions`，故红灯潜伏至本轮全量跑 `service` LLM 子集才被发现 | 已在 §3 复现命令中把该用例并入定向集（`TestLLMProtocolSlot\|…\|TestLLMProtocolOptions\|TestLLMProtocolCapabilities`），避免再次漏跑 |
| 3 | 用例注释原将「删除实例 → NOT_FOUND」标注为场景 9，与计划 §6.2 场景 9（幂等导入）编号错位 | 已修正注释映射（场景 9 由 `TestLLMProviderAdminImportStaticIdempotent` 覆盖；删除 / 禁用作为补充覆盖单列） |
