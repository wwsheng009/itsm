# 多 LLM Provider 支持与可切换方案（含默认 Provider 选择）

> 文档类型：技术方案 + 实施计划（Proposed / v1.6 评审定稿候选）
> 适用范围：`itsm-backend`（Go）、`itsm-frontend`
> 编制日期：2026-09-24
> 版本：v1.6（v1.4/v1.5 决策全部保留；v1.6 注记 BE-9 **接线完成**：协议包（接口 + 注册表 + `openai_chat_completions` 适配器）之外，`service/llm_registry.go` 的构建路径分派、`LLM_PROTOCOL_ADAPTER_ENABLED` 开关（默认关、环境变量优先）与对照测试已落地工作树；DB 实例路径（BE-1..BE-5）仍未开工，其余 3 协议由独立计划推进）
> 目标读者：后端、前端、测试、SRE
> 关联文档：`docs/plan/llm-protocol-adapter-plan.md`（协议适配层独立计划，槽位预留）、`docs/plan/generic-attachment-richtext-control-plan.md`（文档格式参照）、`docs/documentation-governance.md`、`docs/plan/_data/vite-route-map.csv`（路由唯一真相源）
> 外部参考实现（只读参考，不引入依赖）：`E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`（`ProtocolAdapter` 接口 + 4 协议适配器：`openai.go` / `codex.go`=Responses / `anthropic.go` / `gemini.go`）
> 关键代码坐标：`itsm-backend/service/llm_providers.go`、`itsm-backend/service/llm_gateway.go`、`itsm-backend/internal/bootstrap/app.go:603-643`、`itsm-backend/router/ai_routes.go`、`itsm-backend/handlers/ai/handler.go:179-188`、`itsm-frontend/src/components/ai/AIChat.tsx`

---

## 0. 结论先行（TL;DR）与范围

**现状一句话**：后端现有 4 个 provider 实现分支（`openai` / `azure` / `local` / `minimax`，`llm_providers.go:600-612`），但分类口径把"API 形态"与"厂商/部署变体"混成了平级枚举（`azure`/`local` 实为 Chat Completions 形态变体、`minimax` 实为 Anthropic Messages 形态变体）；且**同一时刻只能生效一个**——由 `config.yaml` / 环境变量的 `llm.provider` 在进程启动时选定并固化为单个 `LLMGateway`，运行期无法切换；前端**不存在任何 LLM 配置页面**（`/admin/system-config` 只覆盖通用/安全/邮件等 24+ 键，`/admin/vector-store` 是只读状态页）。

**本方案要交付的三件事**：

1. **后端支持多 provider 并存**：新增租户级 `llm_provider_configs` 表，系统管理员可在「系统管理 → 系统配置 → LLM 模型」页维护 N 个 provider 实例（密钥加密落库），网关具备运行期按 key 解析 provider 的能力。**协议按 4 种 API 形态建模**并按最新设计落地：P0 **实现 `openai_chat_completions` 协议适配器**（`internal/llm/protocol/`：`ProtocolAdapter` 接口 + 注册表 + 该适配器；语义与既有 `OpenAIProvider` 等价、可开关回退），`anthropic_messages` 由既有分支承载（可用、未适配器化），`openai_responses` / `google_gemini` **预留槽位**（枚举/字段/错误码到位、暂不提供实现）；**其余 3 协议的适配器化与语义归一化**由独立计划 `docs/plan/llm-protocol-adapter-plan.md` 推进（参考 `E:\projects\ai-agent-runtime`）。
2. **系统管理员可切换 provider**：AI 对话（`/ai/chat`、`/ai/chat/stream`）请求可携带 `provider` 覆盖参数；会话页切换选择器**仅对系统管理员**（`system:write`）渲染，普通用户行为与现状完全一致。
3. **可以选默认 provider**：租户级默认（系统管理员设置，DB 内唯一）+ 个人默认（系统管理员的个人偏好，服务端存储、可选跟随租户默认）；解析优先级链见 §3.3。

**核心约束（必须保持）**：密钥绝不明文回显/落日志；生产环境不允许以"无任何可用真实密钥"的状态启动（现硬约束的语义扩展，不放松）；DB 无配置时行为与现状 100% 兼容（回退 `config.yaml` 静态单 provider）。

**范围**

- 后端：数据模型（ent schema + 迁移）、Provider Registry/Resolver、`LLMGateway` 运行期解析改造、管理 API、审计与可观测、启动硬约束矩阵化。
- 前端：在既有「系统管理 → 系统配置」页新增「LLM 模型」页签（CRUD/测试连通/启用禁用/设默认，**不新增路由与菜单**）、AI 会话页 provider 选择器与请求透传（仅系统管理员可见）。
- 兼容：灰度开关 `LLM_MULTI_PROVIDER_ENABLED`（默认关闭）、迁移与回滚脚本、测试与验收清单。

**非目标（本期不做）**

| 非目标 | 说明 |
|:---|:---|
| Embedding / RAG 向量模型的 provider 化 | `app.go:603-609` 的 `Embedder` 仍由静态 `cfg.LLM` 构建；每 provider 独立 embedding 属后续迭代（§7 风险 R-7） |
| 同一 provider 多模型矩阵 / 模型路由策略 | 一条记录 = 一个协议 + 一个模型；"同协议多实例不同模型"已可表达，细粒度模型选择不做 |
| 自动跨 provider 故障转移（failover） | 显式拒绝：切换可能把数据发往非预期供应商，违反合规预期；仅保留同 provider 重试（现状 `llm_gateway.go:78-130` 策略不变） |
| per-provider 限流/配额 | 维持网关级 `FixedWindowLimiter`（`llm.token_cap`） |
| 新权限码 | 复用既有 `system:write`（"系统管理"，`internal/authz/catalog.go:153`；内置角色中仅 `sysadmin` 持有，`internal/authz/roles.go:24-28`），不触发"新码三件套"（catalog + 角色绑定 + 守卫测试）流程 |
| `itsm-ai-service`（Python，端口 8000） | 全仓库检索确认 Go 后端与前端均未引用该服务；其自带 `config.yaml` 的 provider 配置（openai/ollama/vllm/deepseek/zhipu/baidu/ali）不在本方案治理范围 |
| 其余协议适配（`anthropic_messages` 适配器化、`openai_responses` / `google_gemini` 真实接入、4 协议语义归一化） | 由独立计划 `docs/plan/llm-protocol-adapter-plan.md` 推进（槽位已在本方案预留，见 §11）；本方案 P0 仅交付 `openai_chat_completions` 适配器，其余协议仍走"枚举 → 现有实现分支"映射，不引入语义归一化 |
| 既有 4 个实现分支的内部重构 | `NewProviderFromConfig` 与 `OpenAIProvider` / `AzureProvider` / `LocalProvider` / `MiniMaxProvider` 均不重构（`openai_chat_completions` 走新适配器，旧分支保留为回退）；整体收敛随适配层计划进行 |

---

## 1. 现状诊断（证据坐标）

### 1.1 后端协议支持矩阵

协议实现在 `itsm-backend/service/llm_providers.go`，统一由 `NewProviderFromConfig(cfg ProviderConfig)` 按 `Provider` 字段分派（`llm_providers.go:586-613`）。下表"现状枚举"只是**实现分支选择器**；最后一列是目标口径（v1.2 修正：现状把"API 形态"与"厂商/部署变体"混为平级枚举）：

| 现状枚举值 | 实现 | 端点来源 | 流式（SSE） | 工具调用 | 归并后的 API 形态（目标） |
|:---|:---|:---|:---:|:---:|:---|
| `openai` | `OpenAIProvider`（`go-openai` 库，`llm_providers.go:18-36`） | `llm.endpoint`（缺省官方地址） | ✅ `ChatStream` | ✅ `ChatStreamWithTools` | `openai_chat_completions` 标准形态（自定义 endpoint 可接 DeepSeek / Qwen / vLLM / Ollama `/v1` 等兼容网关） |
| `azure` | `AzureProvider`（`llm_providers.go:307-357`） | `llm.endpoint` + `llm.deployment` | ❌ | ❌ | `openai_chat_completions` + `variant=azure`（同一形态的鉴权/路由变体，非独立协议） |
| `local` | `LocalProvider`（Ollama 原生 `/api/chat`，默认 `http://localhost:11434`，`llm_providers.go:359-562`） | `llm.endpoint` | ❌ | ❌ | `openai_chat_completions` + `variant=ollama`（P0 复用现有行为；Ollama 原生形态归一化由独立计划处理） |
| `minimax` | `MiniMaxProvider`（Anthropic Messages 形态，`x-api-key` + `anthropic-version: 2023-06-01`，`llm_providers.go:379-493`） | 内置 `api.minimaxi.com/anthropic/v1/messages` | ❌ | ❌ | `anthropic_messages` + `variant=minimax`（endpoint 按 Q4 开放为可选字段） |
| 未识别值 | `default` 分支静默退化为 `openai`（`llm_providers.go:609-612`） | — | — | — | 目标：API 层枚举校验 422 拒绝（D3），不再静默退化 |

密钥来源支持环境变量兜底：`OPENAI_API_KEY` / `AZURE_OPENAI_API_KEY` / `MINIMAX_API_KEY`（`llm_providers.go:589-598`）。

### 1.2 配置与启动：单 provider 固化

- 配置读取：`LoadLLMConfig()` 从 viper 读 `llm.provider/model/api_key/endpoint/deployment/token_cap`（`llm_providers.go:574-584`），无多实例结构。
- 启动装配：`internal/bootstrap/app.go:611-643` —— 加载静态配置 → 生产环境占位符硬终止（`app.go:616-629`）→ `service.NewProviderFromConfig` 构造唯一 provider → `NewLLMGateway(llmProvider, limiter, llmObserver, llmConfig.Provider)`（`app.go:642`）。
- 关键含义：**运行期无法新增/切换 provider**；改变 provider 必须改配置 + 重启进程。

### 1.3 网关与消费方：单实例指针被广泛持有

`LLMGateway{provider, limiter, observer, providerName}`（`llm_gateway.go:12-18`）在构造时捕获单个 `LLMProvider`。运行期切换若靠"重建 gateway"，需要替换所有消费方持有的指针，不现实。消费方清单（非测试代码，`*LLMGateway` 引用）：

| 消费方 | 文件 |
|:---|:---|
| A2UI 工单表单服务 | `service/a2ui_ticket_service.go:68-74` |
| BPMN AI 生成 | `service/bpmn_ai_generator_service.go:14-28` |
| 影响面解释 | `service/impact_explanation_service.go:26-36` |
| RAG `AskWithLLM` | `service/rag_service.go:672-743` |
| 根因分析（两处） | `service/root_cause_service.go:31-44`、`service/root_cause_analysis_service.go:21-33` |
| SLA 预测 Skill | `service/sla_forecast_skill.go:56-64` |
| 摘要服务 | `service/summarize_service.go:13-20` |
| 分诊服务 | `service/triage_service.go:59-123` |

结论：**切换能力必须内化到 `LLMGateway` 内部（按调用解析 provider），而不是替换 gateway 实例**。

### 1.4 API 与权限现状

`router/ai_routes.go:15-47`：`/ai/chat`、`/ai/chat/stream` 等全部挂 `ai:read`；写类（feedback/audit/approve）挂 `ai:write`。ChatStream 请求体仅 `{query, limit, conversationId}`（`handlers/ai/handler.go:180-188`），**无 provider/model 字段**；`ai-api.ts:508-511` 的前端请求结构与之对齐。现状**不存在任何 provider 管理端点**；本方案管理面不复用 `ai:*` 码，改用 `system:write`（仅系统管理员，见 §2.2 D12）。

### 1.5 前端现状：无 LLM 配置页面

| 页面 | 路径 | 与 LLM 配置的关系 |
|:---|:---|:---|
| 系统配置 | `itsm-frontend/src/pages/(main)/admin/system-config/index.tsx`（`tabItems` 在 `:494-513`） | 仅 通用/安全/邮件/... 分组（后端对应 `service/system_config_service.go:258-300` 的 24+ 键），**无 AI/LLM 分组**；本方案在此新增「LLM 模型」页签 |
| 向量存储 | `itsm-frontend/src/pages/(main)/admin/vector-store/index.tsx` | 只读状态 + 连通性测试；配置在部署侧 `VECTOR_STORE_CONFIG`（页面文案明确"不在页面上编辑"，`:97-103`） |
| 连接器 | `itsm-frontend/src/pages/(main)/admin/connectors/index.tsx` | 有"配置落库 + 密钥凭据 + 健康检查 + 启停"的完整交互先例（**本方案页签交互参照实现**） |
| AI 会话 | `src/components/ai/AIChat.tsx`、`src/lib/api/ai-api.ts:490-531` | 无任何 provider/model 选择控件；`AIChat.tsx:1-11` 注释概括了现有能力（流式/引用/会话历史） |

### 1.6 可复用的安全与数据基础设施

| 能力 | 坐标 | 复用方式 |
|:---|:---|:---|
| 密钥加密（AES-GCM，SHA256 派生密钥） | `middleware/encryption.go:21-28` `NewEncryptionService(secretKey)` | provider API Key 加密落库（与 `connector/persistent_store.go:20` 同源密钥） |
| 加密落库先例 | `ent/schema/connector_config.go:11-33`（`encrypted_credentials` Text + `settings` JSON + `(tenant_id,name,provider)` 唯一索引） | `llm_provider_configs` schema 参照 |
| 迁移机制 | `itsm-backend/migrations/`（`YYYYMMDD_snake_case.sql`，可选 `_down.sql` / `_expand.sql` / `_contract.sql`；最新到 `20260923_attachments_backfill.sql`） | 新增 expand/down 迁移 |
| 租户隔离守卫 | `internal/schema/tenant_guard.go:58-89` | 新表带 `tenant_id`，不申请豁免 |
| 菜单注入先例 | `migrations/20260628_add_connector_menu.sql`（`INSERT ... SELECT FROM tenants ... ON CONFLICT (tenant_id, path) DO NOTHING`） | 本方案为页签形态、**不新增路由/菜单**，该先例仅作背景参考（无菜单迁移交付物） |
| 路由真相源 | `docs/plan/_data/vite-route-map.csv`（表头 9 列，逐路由一行） | 本方案不新增路由，CSV 不变；若实施改回独立页面则必须同步一行 |
| 用户级偏好先例 | `ent/schema/notification_preference.go`（user_id 维度偏好） | 个人默认 provider 偏好表参照（写入者仅系统管理员） |
| 系统管理端点权限先例 | `router/router.go:387`、`handlers/vector_store/handler.go:224`（`middleware.RequirePermission("system","write")`）；`internal/authz/roles.go:24-28`（`system:write` 仅 `sysadmin` 持有） | 本方案管理面与切换面统一照此收口 |
| LLM 可观测 | `service/ai_telemetry_repository.go:29-36` 写 `ai_llm_calls(provider, model, tokens, latency_ms, success)`；平台级无 tenant_id（`tenant_guard.go:85` 豁免） | 新增 `provider_key` 列区分"协议"与"具体配置实例" |

### 1.7 差距矩阵

| 诉求 | 现状 | 差距 |
|:---|:---:|:---|
| 多个 provider 并存 | ❌ | 无存储、无注册表，配置是单值 |
| 协议分类 | ⚠️ 混用 | 现状 4 值是实现分支选择器而非 API 形态（`azure`/`local`/`minimax` 与 `openai` 平级）；目标按 4 种 API 形态建模、用 `variant` 表达厂商差异，2 种协议预留槽位（§3.1.4、§11） |
| 运行期切换 provider | ❌ | 网关单 provider 固化，请求无覆盖参数 |
| 系统管理员配置 provider | ❌ | 无 API、无页面；只能改文件+重启 |
| 选择/设置默认 provider | ❌ | 无租户默认概念，无个人偏好 |
| 密钥安全 | ⚠️ 部分 | 静态配置支持 `MaskSecret` 日志脱敏；DB 存储路径尚不存在 |
| 可观测到具体实例 | ⚠️ 部分 | `ai_llm_calls.provider` 只记协议类型，多实例后无法区分 |
| 向后兼容 | ✅ | 需保证：无 DB 配置时行为与现状逐字节一致 |

---

## 2. 目标与关键设计决策

### 2.1 目标

- G1：管理员可在页面上维护 N 个 LLM provider 实例（增删改查、启用/禁用、连通性测试、设租户默认），无需改配置文件或重启。
- G2：AI 会话请求可显式指定 provider；未指定时按优先级链自动解析（§3.3）。
- G3：用户可设置"我的默认 provider"（跟随租户默认 / 指定某个已启用实例），跨设备生效（服务端存储）。
- G4：切换行为可审计、可观测（谁在何时用了哪个 provider），密钥全链路不明文。
- G5：全部能力由灰度开关控制，关闭时行为与当前版本完全一致。

### 2.2 关键决策记录

| # | 决策点 | 选择 | 理由与备选 |
|:---|:---|:---|:---|
| D1 | 存储位置 | 新增 ent 表 `llm_provider_configs`（租户级） | 备选"塞进 `system_configs` KV"被否：结构化字段（enabled/default/health）+ 密钥加密 + 唯一约束，KV 表达力不足且会污染现有 24 键契约（`system_config_service.go:258-261` 要求前后端同步声明，KV 方案会强迫前端表单声明密钥字段，安全面更大） |
| D2 | 密钥存储 | AES-GCM 密文列（复用 `middleware.EncryptionService`），AAD 绑定 `tenant_id:name` | 与连接器凭据（`connector/persistent_store.go`）同源机制；AAD 防止密文跨租户/跨实例搬运 |
| D3 | 协议枚举（v1.2 修正） | 按 **API 形态** 建模 4 值：`openai_chat_completions` / `openai_responses` / `anthropic_messages` / `google_gemini`；厂商与部署差异用 `variant` + `adapter_options` 表达（`azure` / `ollama` / `minimax` 等）；API 层枚举校验；P0 无实现落地的协议（`openai_responses`、`google_gemini`）返回 422 `AI_PROTOCOL_NOT_IMPLEMENTED` | 修正现状分类错误：`azure`/`local` 实为 Chat Completions 形态变体、`minimax` 实为 Anthropic Messages 形态变体，不应与 `openai` 平级；同时消除"未识别值静默退化 openai"缺陷（`llm_providers.go:609-612`）。参考实现：`E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`（4 适配器按 API 形态划分、厂商差异走兼容层）；适配层细节见 §11 独立计划 |
| D4 | 切换粒度 | 请求级覆盖（仅 `/ai/chat`、`/ai/chat/stream` 开放 `provider` 参数） | 工具调用链（A2UI/agent）与后台任务保持默认链解析，避免能力矩阵（流式/工具调用）在业务链路中漂移；详见 §3.3 限制 |
| D5 | 解析时机 | `LLMGateway` 内新增 `ProviderResolver`，按调用解析 + Registry 内存缓存（TTL 30s + 写操作显式失效） | 不可替换 gateway 指针（§1.3 消费方过多）；多副本部署允许 ≤30s 收敛，写入响应携带版本号，前端刷新即可见 |
| D6 | 默认唯一性 | 租户内 `is_default=true` 至多一条，DB 部分唯一索引 + 事务内互斥切换 | 应用层事务保证体验，DB 索引兜底并发写 |
| D7 | 个人默认存储 | 新表 `llm_user_preferences`（user_id 唯一），`provider_key` 为空 = 跟随租户默认；**写入者固定为系统管理员**（多管理员各自偏好互不影响） | 参照 `notification_preference` 先例；备选"localStorage"被否：不跨设备、不可审计，且"可选择默认"需要持久语义 |
| D8 | 可观测扩展 | `ai_llm_calls` 增加可空列 `provider_key`（具体实例 key），原 `provider` 列语义保持"协议类型" | 兼容既有查询（`ai_evaluator.go:348-352`、`ai_telemetry_repository.go` 聚合）不破坏；`LLMObserver` 通过可选接口扩展，旧调用路径零改动 |
| D9 | 灰度与回滚 | 配置开关 `llm.multi_provider_enabled`（env `LLM_MULTI_PROVIDER_ENABLED`，默认 `false`）；关闭时管理 API 不注册、网关走单 provider 快速路径、前端不渲染页签与选择器 | 回滚 = 关开关（可选回退迁移 down），无需回滚代码 |
| D10 | 静态配置定位 | DB 记录优先；DB 无启用记录时回退静态 `config.yaml` 单 provider（现状行为）；「LLM 模型」页签提供"从静态配置导入"按钮显式落库 | 避免启动期自动把 env 明文密钥写入 DB；双源真相通过 UI 展示与导入动作收敛 |
| D11 | 生产硬约束 | 语义扩展为矩阵：仅当"静态密钥为占位符 **且** DB 无任何可解析密钥的启用实例"才 `log.Fatalf`；否则 Warn/Info | 保持 `app.go:613-629` 的防护意图；占位符判定与 `MaskSecret` 脱敏继续复用 |
| D12 | 权限模型（评审定稿：系统管理员） | 管理与切换面统一要求 `system:write`：路由级 `middleware.RequirePermission("system","write")`（全部 §3.4 端点，含读）；`/ai/chat*` 的可选 `provider` 参数在 handler 内二次校验（无权限 → 403，不静默忽略）；前端以 `hasPermission('system','write')` 控制页签与选择器渲染 | 权限范围 = 系统管理员：内置角色中仅 `sysadmin` 持有 `system:write`（`it_director/ops_director` 显式排除，`internal/authz/roles.go:24-28`），`super_admin` 走既有超管旁路；不新增权限码；与 `router/router.go:387`、`handlers/vector_store/handler.go:224` 的系统管理端点先例一致 |
| D13 | 协议槽位 + 首适配器（v1.4 更新，自 v1.2 槽位决策演进） | 5 类槽位：① 枚举 4 值；② 存储字段 `variant` + `adapter_options`（JSON，禁放密钥，§3.5）；③ 协议层（`ProtocolAdapter` 接口 + 注册表，形状对齐参考实现：BuildRequest/BuildHeaders/HandleResponse/ProcessResponse/IsReasoningModel/GetAPIPath）——**P0 实现并接线 `openai_chat_completions` 适配器**（语义等价既有 `OpenAIProvider`，开关可回退），其余 3 协议不接线；④ 能力位 `supportsStream` / `supportsTools` / `supportsReasoning`；⑤ 错误码 `AI_PROTOCOL_NOT_IMPLEMENTED`(422)。其余 3 协议的适配器化与语义归一化（请求/响应/流式/工具/推理链）由 `docs/plan/llm-protocol-adapter-plan.md` 推进 | 按最新协议设计演进主链路：首个适配器覆盖使用面最广的 API 形态（兼容网关/DeepSeek/Qwen/vLLM/Ollama `/v1` 等），先用对照测试锁定"与既有实现等价"再扩面；其余协议差异大（Responses/Gemini 事件模型、Anthropic thinking 等），留在独立计划避免拖慢主线；槽位先行保证数据模型/API/前端一次成型，后续接入零破坏（DB 无迁移、API 无破坏性变更） |

---

## 3. 目标架构

### 3.1 数据模型

#### 3.1.1 `llm_provider_configs`（ent schema：`ent/schema/llm_provider_config.go`）

| 字段 | 类型 | 约束/默认 | 说明 |
|:---|:---|:---|:---|
| `id` | int | PK | |
| `tenant_id` | int | 必填，Positive | 租户隔离，进 `(tenant_id, ...)` 索引 |
| `name` | string(64) | 必填 | 实例 key（API/请求参数用它，如 `deepseek-prod`）；正则 `^[a-z0-9][a-z0-9_-]*$` |
| `display_name` | string(100) | 必填 | 展示名 |
| `protocol` | string(32) | 必填，枚举 `openai_chat_completions` / `openai_responses` / `anthropic_messages` / `google_gemini` | 按 API 形态建模（D3）；P0 仅前两者有等价实现 |
| `variant` | string(32) | 可选，默认空 = 标准实现 | 兼容变体（槽位）：P0 白名单 `azure` / `ollama` / `minimax`；语义解释由适配层计划接管（§11） |
| `adapter_options` | Text(JSON, ≤4KB) | 可选 | 预留槽位：变体专属非敏感参数（如 azure `api_version`、Ollama `keep_alive`）；**禁止存放密钥**（§3.5 校验）；P0 只存储/回显，不解释语义 |
| `model` | string(100) | 可选 | 传给 provider 的模型名（chat completions / anthropic / gemini 使用） |
| `endpoint` | string(500) | 可选 | 全部协议可选；留空回退内置默认地址（Q4：`anthropic_messages` 亦开放） |
| `deployment` | string(100) | 可选 | `variant=azure` 使用 |
| `encrypted_api_key` | Text | 可选 | AES-GCM 密文（AAD=`tenant_id:name`）；无鉴权实例可为空（如 `variant=ollama`） |
| `enabled` | bool | 默认 true | 禁用后不可被解析 |
| `is_default` | bool | 默认 false | 租户默认标记 |
| `source` | string(20) | 默认 `manual` | `manual` / `imported`（从静态配置导入，便于溯源） |
| `status` | string(20) | 默认 `configured` | `configured` / `ok` / `error`（最近一次测试结果） |
| `last_error` | string(2000) | 可选 | 最近一次测试/调用失败的脱敏原因 |
| `last_tested_at` | time | 可空 | |
| `created_at`/`updated_at` | time | Default/UpdateDefault | |
| `deleted_at` | time | 可空 | 软删除（查询统一 `deleted_at IS NULL`，与 `system_config` 风格一致） |

索引：

- `index.Fields("tenant_id", "name").Unique()`（软删场景下由服务层查重 + 迁移层部分唯一索引兜底）
- `index.Fields("tenant_id", "enabled")`
- 迁移中补充：`CREATE UNIQUE INDEX ... ON llm_provider_configs (tenant_id) WHERE is_default AND deleted_at IS NULL`

#### 3.1.2 `llm_user_preferences`（ent schema：`ent/schema/llm_user_preference.go`）

| 字段 | 类型 | 说明 |
|:---|:---|:---|
| `user_id` | int，唯一 | 用户维度 |
| `tenant_id` | int | 冗余租户（校验 provider 归属，避免越租户引用） |
| `provider_key` | string(64)，可空 | 空 = 跟随租户默认 |
| `created_at`/`updated_at` | time | |

约束：写入时校验 `provider_key` 对应用户所在租户的已启用实例；实例被禁用/软删时，读取路径降级为租户默认并在响应中标注 `providerSource=tenant`（不自动改写个人偏好，「LLM 模型」页签可作提示）。

#### 3.1.3 `ai_llm_calls` 扩展（expand 迁移）

`ALTER TABLE ai_llm_calls ADD COLUMN provider_key TEXT NULL;`（回滚 = `DROP COLUMN`，独立 `_down.sql`）。该表平台级无租户维度（`tenant_guard.go:85`），`provider_key` 仅用于区分同协议多实例，不引入租户过滤。

#### 3.1.4 协议枚举 × P0 实现映射（槽位）

P0 不改协议语义：registry 按 `(protocol, variant)` 映射到现有实现分支（§1.1），保证行为与现状逐字节一致；未实现协议在 API 层 422 拒绝。

| 规范协议（API 形态） | P0 实现 | `variant` 白名单 | 映射到 `NewProviderFromConfig` | 说明 |
|:---|:---|:---|:---|:---|
| `openai_chat_completions` | ✅ **P0 协议适配器**（`internal/llm/protocol/openai_chat.go`，语义等价 `OpenAIProvider`；旧分支保留回退） | 空（标准/兼容网关）、`azure`、`ollama` | `openai` / `azure` / `local` | 兼容网关（DeepSeek/Qwen/vLLM 等）用空 variant + 自定义 endpoint；`azure`/`ollama` 变体语义 P0 仍由旧分支承载，适配器化归独立计划 |
| `anthropic_messages` | ✅ `MiniMaxProvider`（P0 载体，**未适配器化**） | 空（标准 Anthropic 兼容端点）、`minimax` | `minimax` | 端点必须显式可配（Q4）；适配器化归独立计划（P0 由既有分支承载） |
| `openai_responses` | 🕳 槽位（未实现） | — | — | 创建/更新返回 422 `AI_PROTOCOL_NOT_IMPLEMENTED`；前端选项置灰并标注「待接入 · 独立计划」 |
| `google_gemini` | 🕳 槽位（未实现） | — | — | 同上 |

> 槽位与独立计划的边界见 §11；本映射表由 BE-8 落单测（每条映射一行用例），`openai_chat_completions` 适配器的等价性由 BE-9 对照测试锁定。

### 3.2 后端分层与改造点

```text
internal/bootstrap/app.go（装配）
  ├─ encryptionService（复用 connector 密钥来源）
  ├─ staticCfg := service.LoadLLMConfig()                     # 现状保留
  ├─ registry := service.NewLLMProviderRegistry(client, encryptionService, staticCfg, sugar)
  ├─ gateway := service.NewLLMGateway(staticProvider, limiter, observer, staticCfg.Provider).
  │                WithResolver(registry)                      # 新增：可选 resolver
  └─ 各业务服务继续持有同一 *LLMGateway（签名不变，§1.3 消费方零改动）

service/llm_registry.go     # 新：租户快照加载/缓存/失效/密钥解密/实例构建
internal/llm/protocol/            # P0 实现（BE-9）：adapter.go（ProtocolAdapter 接口）+ registry.go + openai_chat.go（首适配器）；其余协议不接线（§11）
service/llm_gateway.go      # 改：Chat/ChatStream/ChatStreamWithTools 接受可选 provider 覆盖
handlers/ai/handler.go      # 改：chat/chat/stream 解析 provider 参数并透传；done 事件回带生效 provider
handlers/ai/llm_provider_handler.go   # 新：管理 CRUD/测试/设默认/导入/available/个人偏好（路由级 system:write）
router/ai_routes.go         # 改：注册 §3.4 路由（受开关控制；统一 middleware.RequirePermission("system","write")）
```

**`LLMGateway` 改造（最小侵入）**：

- 新增方法（旧方法签名与行为不变，内部等价于 `override=""`）：
  - `ChatWithProvider(ctx, providerKey, model, messages)`
  - `ChatStreamWithProvider(ctx, providerKey, model, messages, callback)`
  - `SupportsToolCallingFor(providerKey) `（供前端/上层探测，P1 可用）
- 解析失败语义（`override` 非空时）：`AI_PROVIDER_NOT_FOUND` / `AI_PROVIDER_DISABLED` / `AI_PROVIDER_KEY_MISSING` → 4xx，**不静默回退**（用户显式选择必须可见地失败，避免"以为切了其实没切"）。
- `override` 为空时：resolver 返回租户默认/用户偏好，解析失败则回退静态 provider（现状路径）；静态也无效则维持现状错误（如 401），不新增崩溃点。
- 重试策略、限流、`observer` 调用点保持原样（同 provider 内重试，不跨 provider）。

**`LLMProviderRegistry`（新）职责**：

1. `Resolve(ctx, tenantID int, override string) (slot ProviderSlot, source string, err error)`；`ProviderSlot{Key, Protocol, Variant, Model, Provider LLMProvider, SupportsTools bool}`。
2. 缓存：`map[tenantID]snapshot{version, defaultKey, slots}`，TTL 30s；管理写操作（创建/更新/删除/启停/设默认）成功后调用 `Invalidate(tenantID)`，多副本靠 TTL 收敛（D5）。
3. 构建（按协议分派）：解密 `encrypted_api_key` → `openai_chat_completions` 走协议适配器（`internal/llm/protocol`：按 `(protocol, variant)` 选适配器并构建 `LLMProvider` 实现，语义等价既有 `OpenAIProvider`，等价性由 BE-9 对照测试保证）→ 其余协议按 §3.1.4 映射表归一为现有实现分支并组装 `ProviderConfig{Provider: <映射值>, Model, APIKey, Endpoint, Deployment}` → 复用 `NewProviderFromConfig`（开关关闭或适配器不可用时回退此路径，保证回滚能力；`openai_responses`/`google_gemini` 在进入构建前已被 422 拦截）。
4. 解密失败/密钥缺失：该 slot 标记不可用（跳过 + `Warnw`，仅输出 `name` 与脱敏值），不影响其它 slot；`Resolve` 命中不可用 slot 时返回 `AI_PROVIDER_KEY_MISSING`。
5. 租户隔离：所有查询强制 `tenant_id` 过滤（`tenant_guard` 守卫测试覆盖）。

### 3.3 解析优先级与降级语义

```text
请求参数 provider（仅 /ai/chat、/ai/chat/stream 开放）
  └─ 非空 → 调用者须持 system:write（否则 403 AI_PROVIDER_FORBIDDEN）；且必须是本租户 enabled 实例，否则 4xx（不回退）
  └─ 空 ↓
个人默认 llm_user_preferences.provider_key（系统管理员；已启用才生效）
  └─ 失效 → 记日志 + 降级 ↓（响应标注 providerSource=tenant）
  └─ 空 ↓
租户默认 is_default=true（已启用）
  └─ 无 ↓
静态配置（config.yaml / env，现状行为，providerSource=static）
  └─ 无/密钥为占位符 → AI 端点返回 503 AI_PROVIDER_UNAVAILABLE（开发环境）
```

**显式限制（P0）**：

- 工具调用路径（A2UI、agent 工具、BPMN 生成等）**不读取请求级 override**，始终走"个人默认→租户默认→静态"链；若默认实例不支持工具调用，行为与现状一致（`llm_gateway.go:250-287` 的优雅降级保持）。
- 后台/异步任务（分诊、摘要、根因、SLA 预测）同工具调用路径，不开放覆盖。
- 响应回带：`/ai/chat` 响应体与 `/ai/chat/stream` 的 `done` 事件新增 `provider`（生效 key）与 `providerSource`（`request|user|tenant|static`），前端据此展示标签；不返回明文密钥。

### 3.4 API 契约（全部挂在 `/api/v1/ai` 下，受 D9 开关控制；权限统一 `system:write` = 系统管理员）

| 方法 | 路径 | 权限 | 说明 | 失败态 |
|:---|:---|:---|:---|:---|
| GET | `/ai/providers` | `system:write` | 管理列表（含掩码密钥 `sk-****abcd`、状态、是否默认、来源） | 403 |
| POST | `/ai/providers` | `system:write` | 新建（422：协议非法/协议未实现/variant 非法/`adapter_options` 非法/endpoint 缺失） | 400/409/422 |
| PUT | `/ai/providers/:id` | `system:write` | 更新；`apiKey` 缺省表示不变，传空串表示清空；协议改为未实现值 → 422 | 404/422 |
| DELETE | `/ai/providers/:id` | `system:write` | 软删；若为默认则要求先切换默认（409 `AI_PROVIDER_IS_DEFAULT`）；同时清理引用它的个人偏好 | 404/409 |
| POST | `/ai/providers/:id/test` | `system:write` | 连通性测试：构造最小 `Chat` 请求（超时 15s），写 `status/last_error/last_tested_at` | 200（结果字段 ok/error） |
| POST | `/ai/providers/:id/default` | `system:write` | 设租户默认（事务：先清后置，DB 部分唯一索引兜底） | 404/409 |
| POST | `/ai/providers/import-static` | `system:write` | 把当前静态配置导入为一条记录（`source=imported`，加密落库）；**幂等**：按 `(protocol, variant, endpoint, model, deployment)` 命中则更新并返回 `200 {updated:true}`；仅 `name` 冲突返回 409 | 409/422 |
| GET | `/ai/providers/available` | `system:write` | 选择器数据：`[{key, displayName, protocol, variant, model, supportsStream, supportsTools, supportsReasoning, implemented, isDefault}]`（无密钥）；仅系统管理员可读 | 200/403 |
| GET | `/ai/user-preference` | `system:write` | 我的默认（`providerKey` 可空 + 生效值 effectiveProviderKey），仅系统管理员 | 200/403 |
| PUT | `/ai/user-preference` | `system:write` | 设置/清除我的默认（校验租户归属与 enabled），仅系统管理员 | 400/404/422 |

**`provider` 覆盖参数（`/ai/chat`、`/ai/chat/stream`）**：路由仍为 `ai:read`；请求体携带非空 `provider` 时，handler 内校验调用者具备 `system:write`，否则 403 `AI_PROVIDER_FORBIDDEN`（显式失败，不静默忽略）。校验复用 RBAC 的权限解析（`middleware/rbac.go` 的角色→权限查询），实现时抽为一个 `middleware` 级小工具函数并加单测。

**请求/响应示例**：

```jsonc
// POST /api/v1/ai/chat/stream
{ "query": "如何重置 VPN 密码", "limit": 5, "conversationId": 42, "provider": "deepseek-prod" }
// SSE done 事件（扩展字段，兼容旧前端）
{ "type": "done", "conversationId": 42, "provider": "deepseek-prod", "providerSource": "request" }
```

**错误码统一**（`common` 既有错误响应结构）：`AI_PROVIDER_FORBIDDEN`(403，无 `system:write` 使用覆盖参数)、`AI_PROVIDER_NOT_FOUND`(404)、`AI_PROVIDER_DISABLED`(409)、`AI_PROVIDER_KEY_MISSING`(422)、`AI_PROVIDER_UNAVAILABLE`(503)、`AI_PROVIDER_IS_DEFAULT`(409)、`AI_PROTOCOL_NOT_IMPLEMENTED`(422，协议枚举合法但 P0 无实现，如 `openai_responses` / `google_gemini`)。

### 3.5 安全与权限

- **不回显**：所有读接口只返回掩码（`common.MaskSecret`，与 `app.go:619` 同一工具）；请求日志禁用 body 打印（现有中间件行为需在联调时抽查确认）。
- **加密**：`middleware.EncryptionService`（AES-GCM + SHA256 派生），密钥来源与连接器一致；若部署未配置加密密钥，则启动期 `Warn` 且多 provider 功能自动关闭（开关置 false 语义），不降级为明文存储。
- **AAD**：`tenant_id:name` 参与认证，防密文搬运；更新 name 时需重新加密（服务层显式处理，事务内完成）。
- **`adapter_options` 非敏感约束**：仅允许非敏感键（JSON ≤4KB；键名黑名单命中 `api_key` / `token` / `secret` / `authorization` / `password` 等 → 422），密钥只允许走 `encrypted_api_key`；回显与日志沿用掩码规则。
- **审计**：创建/更新/删除/设默认/切换测试写一条审计事件（复用现有 AI 审计通道 `POST /ai/audit` 的服务端写入点或 `ai_audit` 既有机制；实施时以 `service/ai_audit*` 现有封装为准，不新增旁路）。请求级切换（系统管理员）记录在 `ai_llm_calls.provider_key`（调用级证据）+ 结构化日志（`tenant/user/provider/source`），不记录 prompt 全文（现状不变）。
- **权限（评审定稿：系统管理员）**：全部管理端点（含读）统一 `middleware.RequirePermission("system","write")`；`system:write` 在内置角色中仅 `sysadmin` 持有（`super_admin` 走既有超管旁路）。理由：配置面暴露端点/模型等基础设施信息，按"系统管理"最小面收敛；普通用户不渲染入口、不发放选择器数据，会话与现状一致。演进（P1，非本期）：若需"全员可切换"，仅把 `available`/`user-preference` 两个读端点降为 `ai:read` 并放开前端渲染条件，网关与数据模型零改动。
- **租户隔离**：`internal/schema/tenant_guard.go` 守卫测试必须覆盖新表（存在 `tenant_id` 字段即通过）；跨租户 provider 引用（用户偏好/请求参数）一律 404，不泄漏存在性。

### 3.6 前端设计

**方案（评审定稿）：并入既有系统配置页，不新增路由/菜单**

入口：「系统管理 → 系统配置」(`/admin/system-config`) 新增第 4 个页签「LLM 模型」，与现有 通用设置/安全设置/邮件设置 并列（`src/pages/(main)/admin/system-config/index.tsx:494-513` 的 `tabItems` 追加 `{key:'llm', label:'LLM 模型', icon:<Bot/>, children:<LLMProviderSettings/>}`）。页签渲染条件为 `hasPermission('system','write')`（`src/lib/hooks/use-permissions.ts:35-36`）；非系统管理员看不到页签，直连 API 亦 403（前后端双保险）。

- 列表：展示名、key、协议徽标（协议 + variant 后缀）、模型、endpoint、启用状态、默认标记、健康状态（最近测试时间/错误脱敏摘要）。
- 表单弹窗：协议下拉（4 值；`openai_responses` / `google_gemini` 置灰并标注「待接入 · 独立计划」）、兼容变体下拉（标准 / `azure` / `ollama` / `minimax`，随协议联动）、按协议动态显示 model/endpoint/deployment 字段、`adapter_options` 高级参数（JSON 文本框，前端预校验合法性与敏感键）、API Key（密码框，编辑时占位"留空保持不变"）、启用开关。
- 操作：测试连通（结果内联展示）、设为默认、启用/禁用、删除（二次确认，默认实例给出先切换提示）、"从静态配置导入"按钮。
- 组件与文件（不新增路由）：`src/pages/(main)/admin/system-config/llm-provider-settings.tsx`（页签内容组件，表单/表格可再拆子组件）；API 客户端：`src/lib/api/llm-provider-api.ts`。
- 宿主页改动仅限 `tabItems` 追加一项与权限判定，不动既有三个页签的表单与保存逻辑。

**AI 会话页选择器（`components/ai/AIChat.tsx`）**：

- 可见性：开关开启 **且** `hasPermission('system','write')` **且** `available.length > 1` 时才渲染；不满足时 UI 与现状完全一致（普通用户零变化）。
- 头部 `Select`：数据源 `GET /ai/providers/available`；默认选中 `userPreference.effectiveProviderKey`；选项副标题展示协议（+variant）与模型；能力徽标（不支持流式的实例标注"非流式"）。
- 透传：`ai-api.ts` 的 `AIChatStreamRequest` 与 `AIApi.chat` 增加 `provider?: string`；`done` 事件解析 `provider/providerSource` 并在助手消息 meta 行展示（如"回答由 deepseek-prod 生成"，同样仅系统管理员可见）。
- 个人默认：选择器内"设为我的默认"操作 → `PUT /ai/user-preference`；失败 toast 不阻断会话。
- 降级：请求返回 `AI_PROVIDER_DISABLED/NOT_FOUND` 时自动刷新列表 + 提示 + 回落到生效默认重试一次（仅这一种自动重试，且仅限非流式失败重发）。

**路由/菜单同步（本方案不需要）**：不新增路由 → `src/routes/index.tsx` 与 `docs/plan/_data/vite-route-map.csv` 均不变；不新增菜单 → 无菜单迁移（原 v1.0 的三件套要求随页面形态取消；若评审改回独立页面，必须恢复三件套同步）。

### 3.7 可观测性

| 观测点 | 现状 | 变更 |
|:---|:---|:---|
| `ai_llm_calls.provider` | 协议类型 | 自 v1.2 起写**规范协议形态值**（4 值之一）；历史行保留旧实现名（`openai`/`azure`/...）不回填，查询按文本兼容（§7 R-12） |
| `ai_llm_calls.provider_key` | 无 | 新增可空列；resolver 命中 DB 实例时写入其 key，静态回退时写 NULL |
| 协议槽位 | 无 | `protocol`/`variant` 进入解析日志与 `available` 能力位；未实现协议拒绝返回 422 并计数（P1 可选指标） |
| 日志 | 启动期掩码密钥 | provider 解析结果、切换行为结构化日志（tenant/user/key/source/原因），密钥始终掩码 |
| 系统配置页「LLM 模型」页签 | 无 | 状态/最近测试错误可见（脱敏）；仅系统管理员可访问 |
| 指标 | `/ai/metrics` 已有平均延迟 | 可选 P1：按 provider_key 维度聚合（复用 `ai_telemetry_repository.go` 仓储，不在 P0 范围） |

---

## 4. 兼容、迁移与回滚

### 4.1 启动硬约束矩阵（`app.go:613-629` 语义扩展）

| 静态密钥 | DB 启用实例（可解密） | 环境 | 行为 |
|:---:|:---:|:---|:---|
| 真实 | 任意 | 任意 | Info 日志（掩码），正常启动 |
| 占位符/空 | 有 | 生产 | Warn（提示以 DB 实例为准），正常启动 |
| 占位符/空 | 无 | 生产 | **Fatal 终止**（现状保持） |
| 占位符/空 | 任意 | 开发 | Warn，AI 功能按现状降级/禁用 |
| 真实 | 有 | 任意 | Info 记录静态配置已存在，DB 优先 |

判定"可解密"依赖加密密钥可用；加密服务不可用时按"无 DB 实例"处理（生产会 Fatal，属预期：无法解密即不可用）。

### 4.2 迁移清单（`itsm-backend/migrations/`）

| 迁移 | 内容 | 回滚 |
|:---|:---|:---|
| `20260924_create_llm_providers_expand.sql` | 建 `llm_provider_configs`（含 `protocol` / `variant` / `adapter_options` 列）、`llm_user_preferences`；索引（含默认唯一部分索引） | `_down.sql`：DROP 两表（表内无客户业务数据，允许回滚；执行前提示备份） |
| `20260924_ai_llm_calls_provider_key_expand.sql` | `ALTER TABLE ai_llm_calls ADD COLUMN provider_key TEXT NULL` | `_down.sql`：DROP COLUMN |

命名与日期以实施日为准；遵循"目录即真相"与 expand/contract 惯例，不依赖启动期自动建表。**无菜单/路由迁移**：本方案为「系统配置」页内页签，复用既有 `/admin/system-config` 菜单与路由（原 v1.0 的 `add_llm_provider_menu.sql` 取消）。实测（2026-09-24，真实 DB）：两条迁移在存量库/空库两形态的 up+down 全部通过；prod 侧上线前置（账本漂移 B1 + 两个历史菜单文件 B2/B3）与处置 runbook 见 §4.4。

### 4.3 灰度与回滚路径

1. **代码默认关闭**：`llm.multi_provider_enabled=false` 时——管理 API 不注册（404）、registry 不参与解析、网关走单 provider 快速路径、前端不渲染页签与选择器；行为等于当前线上版本。
2. **灰度开启**：`LLM_MULTI_PROVIDER_ENABLED=true`（或 config.yaml）→ 注册路由 + 页签可见（仅系统管理员）；建议先在测试租户验证。
3. **回滚**：关开关（秒级，无需重启即无 API 影响；进程重启后连页面入口一并消失）→ 如确认不再启用，执行 down 迁移。
4. **数据保留**：软删 provider 不物理删数据；用户偏好引用失效时读取路径自动降级（§3.1.2）。

### 4.4 数据库迁移实测与上线前置（2026-09-24，真实 DB `172.18.3.238`）

> 环境：`itsm@172.18.3.238`（本机经隧道 `127.0.0.1:15433` 访问，scratch 库 `itsm_mig_legacy` / `itsm_mig_empty`）。验证工具：`itsm-backend/cmd/mig-verify`（复刻 bootstrap 全路径：ent 基线 → 注册流 → 目录发现 → 收养 → 应用）；断言与账本脚本在 `.dev/`（`mig_check.py`、`ledger_drift.py`、`mig_reconcile.py`、`mk_empty.py`）。**prod 侧只执行只读预检（`-ro`），零写入。**

**① 本方案两条迁移：存量库 / 空库两形态 up+down 全绿**

| 形态 | up | down | 断言（全部 PASS） |
|:---|:---|:---|:---|
| 存量库克隆（prod 账本 1:1，48 行 + 业务数据） | 48→50 | 50→48 | 2 表 / `provider_key` 可空 / 两表 RLS 开启 + 策略各 1 / 4 个命名索引（`uq_llm_provider_configs_tenant_default` 为 UNIQUE+partial）/ 7 个关键列；数据不变（tenants=1、ai_llm_calls=57） |
| 空库（ent `Schema.Create` 基线，模拟全新安装） | 21→23 | 23→21 | 同上（空数据形态） |

复跑：`$env:DB_NAME='itsm_mig_legacy'; go run ./cmd/mig-verify -up -only 20260924_ai_llm_calls_provider_key_expand,20260924_create_llm_providers_expand` → `python .dev/mig_check.py itsm_mig_legacy after_up 50` → `-down 2` → `after_down 48`；空库加 `-entbaseline`（21→23→21）。

**② prod 只读预检：当前会被历史阻塞卡住（上线前置）**

```
$env:DB_NAME='itsm_prod'; go run ./cmd/mig-verify -ro
RO MODE (read-only): discovered=31 merged=46
STATUS ERROR (read-only): migration checksum mismatch for 20260501_rbac_endpoint_acls: applied=23bcb4da… current=fdca4d77…
```

| # | 阻塞（均为 BE-1 之前既存） | 证据 | 影响 |
|:---|:---|:---|:---|
| B1 | 账本 checksum 漂移 6 条：`20260501_rbac_endpoint_acls`、`20260616_security_role_permissions`、`20260620_create_marketplace`、`20260620_process_routing_enhancement`、`add_missing_indexes`、`add_missing_indexes_batch2` | prod `-ro` 首行 mismatch；`python .dev/ledger_drift.py itsm_prod` 全量清单（applied vs current 哈希） | `GetPendingMigrations` 硬失败 ⇒ **所有 pending（含本方案 2 条）都不执行** |
| B2 | `20260628_add_connector_menu`：`SELECT …, id, … FROM menus m CROSS JOIN tenants t` 中 `id` 二义 | 存量克隆实跑：`pq: column reference "id" is ambiguous` (42702) | 批次在此中断，后续迁移不执行 |
| B3 | `20260830_ticket_types_menu_reparenting`：写死目标 id（34/40）与 prod 不符；DELETE 不命中 → INSERT 出第二条 `/tickets/analytics` → 校验子查询报 `more than one row returned by a subquery` (21000)（空库则 `parent /tickets (id=2) not found`） | 存量克隆实跑 21000；prod 五条目标菜单终态**已满足**（`/tickets/types`→`/tickets`、`/tickets/analytics`→`/tickets`、`/audit-logs`、`/notifications` 顶级、`/workflow/audit`→`/workflow`），`/admin/connectors` 亦已存在 | 同上 |

机制：prod 最早 `007+` 记账时间 `2026-09-18 08:00` 晚于收养分界 `2026-09-08`（`migration/legacy_record.go`）⇒ 被判为"非既有安装"，两个 pre-cutoff 文件不获收养、按 pending 执行——这是 B2/B3 进入待执行队列的原因。

**③ 上线前置操作（prod，按序、可审计；R1-R3 已于 2026-09-25 执行，记录见 ⑤）**

1. **R1 收敛 6 条 checksum**：逐文件核对（`add_missing_indexes*` 为纯索引脚本，可先 `BEGIN` 执行当前文件内容验证幂等后 `COMMIT`，再更新账本）→ 模板：`UPDATE schema_migrations SET checksum='<当前文件 sha256>' WHERE version='<version>';`（逐条）。
2. **R2 收养两个菜单文件**（终态已满足，遵循"adopt, don't replay"）：
   ```sql
   INSERT INTO schema_migrations (version, description, applied_at, rollback_sql, checksum, execution_ms, release_version)
   VALUES
     ('20260628_add_connector_menu', '2026-06-28 添加连接器市场菜单', NOW(), '', '', 0, 'adopted'),
     ('20260830_ticket_types_menu_reparenting', '2026-08-30: 菜单归位修复（菜单信息架构 hygiene）', NOW(), '', '', 0, 'adopted')
   ON CONFLICT (version) DO NOTHING;
   ```
3. **R3 复核**：`go run ./cmd/mig-verify -ro`（prod）应显示 `pending=2`（仅本方案两条）。
4. **R4 发布与验后**：按 §4.3 灰度开启；启动日志确认两条 `Migration applied successfully`；用与 `.dev/mig_check.py` 同源的 SQL 复核表 / RLS / 索引。

**④ 附带发现（记录，不在本方案内修复）**

- 空库"全量重放"路径当前不可用：`-up` 在第 2 个文件 `20260501_rbac_endpoint_acls` 即失败（`relation "permission_definition" does not exist`）⇒ 磁盘迁移历史并非为全新重放设计（全新安装依赖 ent 基线 + 既有库路径）。
- `cmd/migrate -down` 以"版本字典序最后一条"为目标（`main.go:219/239/389`），遇 `add_missing_indexes*` 等非日期别名会选错靶；`cmd/mig-verify` 已按 `applied_at` 倒序修正（测试工具侧，未改 `cmd/migrate`）。

**⑤ R1/R2 执行记录与残留差异（2026-09-25 12:20 +08，prod 已执行）**

- **工具**：`.dev/mig_r1r2.py`（verify / dry-run / apply 三态：终态核对 → 账本备份 → 单事务守卫写 → 事后复核）；配套 `.dev/mig_desc_check.py`（收养行描述与 `discovery.go` 解析逐字节比对）、`.dev/mig_audit_diff.py`（执行前后逐行审计）。先在克隆库 `itsm_mig_legacy` 彩排（含按 prod 旧值回填后的 R1 演练）全绿，再对 prod 执行。
- **终态核对（写前）**：8 个版本结构对象 145 项 + 语义 6 项全部通过——含 `add_missing_indexes*` 的 120 个索引逐名核对（含 2026-09-08 修正过的 `wf_instance_entity_idx`）、`marketplace`/`process_routing` 全量对象、菜单按名称断言（prod `/tickets` id=3、`/workflow` id=15，文件内写死的 id=2 不适用）。
- **执行**：R1 六条守卫更新（`UPDATE … WHERE version=%s AND checksum=<写入前旧值>`，逐条 `rowcount=1`，无并发改写）；R2 收养 2 行（`checksum=''`、`execution_ms=0`、`release_version='adopted'`，描述 md5 与解析规则一致）；全程单事务提交。
- **复核**：`.dev/ledger_drift.py itsm_prod` → `mismatch=0`；`go run ./cmd/mig-verify -ro`（prod）→ `applied=50 pending=2`（仅本方案两条）；`.dev/mig_audit_diff.py` 对比写前备份 → 差异仅 6 条 checksum + 2 条收养，**业务表零写入**。
- **证据/回滚**：`.dev/backup/itsm_prod_schema_migrations_20260925T122016.json`（写前 48 行全量）、`.dev/backup/itsm_prod_rollback_r1r2_20260925T122016.sql`（旧 checksum 还原 + 收养行删除）、`.dev/backup/itsm_prod_r1r2_20260925T122016.log`。
- **残留差异（2 项，记录在案、不阻断发布，交 owner 决策）**：① `endpoint_acls` 的 `update_endpoint_acls_timestamp` 触发器与函数从未落地（该文件尾段自带 `Optional` 标注，晚于首次执行加入）；② security 角色（`roles.id=106`，tenant 1）缺 `cmdb:read` / `dashboard:read` / `sla:read` 三条授权（该历史文件引用了库中不存在的 `resource_type` 列，无法重放；本轮只做账本写，不动业务表）。
- **R4（发布）未执行**：prod 当前 `pending=2`，由下一版本部署时 bootstrap 自动应用（启动日志应出现两条 `Migration applied successfully`）。

---

## 5. 任务拆解

> 依赖顺序：BE-1 → BE-2/BE-3/BE-8 → BE-4 → BE-5/BE-9（BE-9 依赖 BE-2、BE-3）→ FE-1..FE-4 → QA-1..QA-3 → DOC-1。批次划分、出口门禁与关键路径见 §5.4。每个任务验收证据必须包含可复现命令或测试用例名。
>
> 落地状态（2026-09-24；BE-9 接线与 BE-8 槽位轨先行落地，BE-1 由子代理执行中）：① 协议包 `itsm-backend/internal/llm/protocol`（`ProtocolAdapter` 接口 + 注册表 + `openai_chat_completions` 适配器 + 桩服务测试，`go test ./internal/llm/protocol/... -count=1` 全绿）；② **接线完成**：`service/llm_registry.go` 按 `(protocol, variant)` 构建分派（适配器优先、未注册回退既有分支）、`NewProviderFromConfig` 接入、`NewProtocolProvider` + `ErrProtocolNotImplemented`（422 哨兵）、开关 `LLM_PROTOCOL_ADAPTER_ENABLED` / `llm.protocol_adapter_enabled`（默认关、env 优先、非法值按关）；③ 对照测试锁定与既有 `OpenAIProvider` 等价（同一桩服务下非流式正文、流式增量序列、工具调用累积、请求体字段与鉴权头、错误身份与网关重试口径逐项比对），并覆盖开关关闭回退与 azure/ollama/minimax 变体回退；④ **BE-8 协议槽位落地**（本轮）：`service/llm_protocol_slot.go` 提供 4 值协议枚举常量（与 `internal/llm/protocol` 同源）、§3.1.4 映射表代码化（`openai_chat_completions` → openai/azure/local、`anthropic_messages` → minimax）、variant 白名单校验、能力位（`supportsStream`/`supportsTools`/`supportsReasoning`/`implemented`，按 P0 现有实现真实填写）、槽位哨兵（`AI_PROTOCOL_INVALID` / `AI_PROTOCOL_VARIANT_INVALID` / `AI_ADAPTER_OPTIONS_INVALID`，未实现协议复用 `AI_PROTOCOL_NOT_IMPLEMENTED`）与 `adapter_options` 校验（JSON 对象、≤4KB、敏感键黑名单、嵌套深度上限），`LLMProtocolOptions()` 作为 BE-4 DTO / FE-2 下拉数据源；`service/llm_protocol_slot_test.go` 逐行锁定映射表并覆盖全部 422 分支（`go test ./service/ -run 'TestLLMProtocolSlot|TestValidateAdapterOptions|TestIsSensitiveAdapterOptionKey' -count=1` 全绿、`gofmt -l` 无输出）；⑤ **BE-1 数据层完成（2026-09-24）**：`ent/schema/llm_provider_config.go` 与 `llm_user_preference.go` 两表 + 生成代码（8 个既有文件增量、11 个新生成文件；`git diff --shortstat` = 8 files changed, 2709 insertions(+), 38 deletions(-)）+ 4 个迁移（`20260924_create_llm_providers_expand[_down].sql`、`20260924_ai_llm_calls_provider_key_expand[_down].sql`；含 RLS 策略、`(tenant_id,name)` 唯一索引、每租户单默认的部分唯一索引、索引名与 ent 生成名对齐以避免「迁移建 + ent 建」重复）；证据（子代理产出后父代理复跑）：`go build ./...` exit 0、`go test ./internal/schema/ -count=1` ok、`go run ./cmd/migration-lint -migration-sql <expand.sql> -strict` 两文件均 exit 0（`OK: every new NOT NULL column carries DEFAULT`）；**未执行项（环境受限）**：Postgres 空库/存量库 up+down 未跑（本机 5432 无实例、`ITSM_TEST_DSN` 为空），补跑命令 `go run ./cmd/migrate -up|-status|-down` 或 `ITSM_TEST_DSN=<dsn> go test ./integration/`；⑥ **BE-2 完成（B2）**：`service/llm_registry.go`（租户快照加载/缓存 TTL 30s/`Invalidate`/密钥解密/实例构建/降级）+ `ProviderSlot` + 四 `ProviderSource` 常量 + 四哨兵错误 + `NewLLMProviderRegistry`；证据：`go test ./service/ -run 'TestLLMProviderRegistry' -count=1` 全绿（10 组用例：override 命中/未命中/禁用/软删、密钥缺失不 panic 且日志无明文、租户默认与静态回退、租户默认不可用透出错误、TTL 与并发失效、协议变体映射）；⑦ **BE-3 完成（B3）**：`service/llm_gateway.go` 新增 `WithResolver`（窄接口 `ProviderResolver`；实现 `UserDefaultResolver` 时自动接线个人默认）、`ProviderRequest{Key,TenantID,UserID}` 与 `ProviderResolution{Key,Source,SupportsTools}`、`ChatWithProvider[Info]`/`ChatStreamWithProvider[Info]`/`ChatWithRequest`/`ChatStreamWithRequest`/`SupportsToolCallingFor`；语义：显式选择失败一律可见失败（不静默回退）、空 override 走「个人默认→租户默认→静态」链、静态回退与现状一致；重试/限流/观测口径不变（同 provider 内重试、只 Observe 最终结果）；既有 4 个旧方法零改动（回归门禁）；证据：`go test ./service/ -run 'TestLLMGateway|TestLLMProviderRegistry|TestProtocolAdapterEnabled|TestNewProviderFromConfig|TestProtocolProvider|TestNewProtocolProvider|TestResolveProtocolURL|TestLLMProtocolSlot' -count=1 -v` = **144 用例全绿**（ok 6.107s；含 gateway 新增 8 组）、`gofmt -e -l` 无输出；⚠️ **计划偏差（已实现，待评审追认）**：`SupportsToolCallingFor` 增补 `ctx` 参数（解析需租户维度）、新增 `*Info`/`*Request` 方法族（§3.4 要求回带 `provider`/`providerSource`，且 service 层无 user id 上下文，个人默认需显式 `UserID` 入参）；⑧ 待办：BE-4/BE-6/BE-7（B4 批次）、BE-5 接线（`WithResolver` 注入 + 开关读取）、QA-3 全链路回归；任务依赖顺序不变。⑨ **B4/B5 后端轨完成（2026-09-24，v1.10）**：BE-4 管理 API（§3.4 全 10 条路由 + DTO/校验/掩码/审计/`available`/个人偏好/`import-static` 幂等）、BE-6（`ai_llm_calls.provider_key` 写入 + `ai_metrics.byProvider` 聚合，静态回退 NULL、开关关闭时字段省略）、BE-7（chat/stream 可选 `provider` 覆盖，非 `system:write` → 403 `AI_PROVIDER_FORBIDDEN`，done 事件回带 `provider`/`providerSource`）、BE-5 bootstrap 接线（加密服务 → registry → 网关注入、§4.1 硬约束矩阵、双开关核对）均已落地工作树；证据：`go test ./internal/llm/protocol/... ./internal/bootstrap/ ./middleware/ ./router/ ./handlers/ai/ -count=1` 全绿 + `service` 定向集 60+ 用例全绿；RBAC 生成物同步（`go run ./cmd/authz-gen` 重新生成 `middleware/rbac_precheck_gen.go`，新路由预检由 `ai:read`/`ai:write` 回退修正为声明的 `system:write`，`TestPrecheckMapIsFresh`/`TestRoutePrecheckAlignment` 红灯转绿）；遗留：QA-3 全链路回归（依赖前端批次）与 Postgres 迁移空库/存量库 up+down（环境受限）。

> ⑩ **B6 前端完成（FE-1..FE-4，2026-09-24）**：① FE-1 `lib/api/llm-provider-api.ts`（§3.4 十端点 + DTO 对齐类型 + `describeLLMProviderError`/`llmProviderErrorCode` 错误映射）与共用探测 hook `lib/hooks/use-llm-provider-feature.ts`（仅 `system:write` 探测 `available`，fail-closed，`refresh`/`setPreference`）；② FE-2 `pages/(main)/admin/system-config/llm-provider-settings.tsx`（列表/新建编辑/连通性测试/设默认/启停/软删/`import-static` 导入；未实现协议选项置灰；422 错误码转可读提示；密钥只写不回显）挂宿主 `index.tsx` 的 `tabItems`（仅开关开启时追加）；③ FE-3 权限与回归——宿主页与 hook 按 `hasPermission('system','write')` 门控，`use-permissions.test.ts` 追加 FE-3 用例，`src/routes/index.tsx` 与 `docs/plan/_data/vite-route-map.csv` **零 diff**；④ FE-4 会话页 `components/ai/AIChat.tsx`（选择器 + 设为/清除我的默认 + 生效实例标签 + 失效提示）与 `lib/api/ai-api.ts`（SSE 与降级请求体仅在显式选择时携带 `provider`；`done` 回带 `provider`/`providerSource`，无字段时 `onDone` 仍按单参数调用 → 既有调用方零感知）；⑤ 证据：`npx tsc --noEmit` exit 0、`npx eslint`（7 个改动文件）无输出、`npx jest src/lib/api/__tests__/ai-api.test.ts src/lib/hooks/__tests__/use-llm-provider-feature.test.ts src/lib/hooks/__tests__/use-permissions.test.ts --silent` 3 套件全绿（ai-api 41 用例，含 QA-3 字节一致与回调兼容用例）；⑥ 遗留：QA-2 端到端验收（需运行后端 + 浏览器）与 QA-3 服务端侧全链路回归待执行。

> ⑪ **QA-3 开关关闭全链路回归完成（2026-09-25）**：13 项门禁维度全部通过——① 开关语义（未配置默认关 / env 优先 / 非法值按关）；② 无新路由：新增 `router/llm_provider_routes_test.go` 运行时断言（handler=nil ⇒ `/api/v1/ai/providers` 404；handler 注入 ⇒ 401 进入鉴权链）＋ `internal/bootstrap/app.go:989` 装配门禁（开关关闭不构造 handler）；③ 旧响应形状：chat 走既有 `svc.Chat` 路径、指标不含 `byProvider`、`NewProviderFromConfig` 回退既有分支、启动探针不启用；④ 前端零新增 UI / 零请求（非 `system:write` fail-closed 用例）＋ 路由与 route-map **零 diff**；⑤ 请求体逐字节一致与 `onDone` 单参数回调兼容（ai-api 4 用例）；⑥ 后端六组测试 + 前端三套件（94 用例）+ `tsc`/`eslint` 全绿；证据归档 `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`。浏览器级人工视觉复核并入 QA-2。⑩ ⑥ 所列「QA-3 服务端侧全链路回归待执行」就此关闭。

> ⑫ **QA-2 端到端验收（自动化口径）完成（2026-09-24）**：① 新增 `handlers/ai/llm_provider_qa2_flow_test.go`（`TestLLMProviderQA2RuntimeSwitchFlow`）——内存 SQLite + `httptest` 桩（**不连 prod、不连真实 LLM**），按生产同构接线（registry 兼作管理面 Invalidator + `gateway.WithResolver`）把「管理写 → 缓存失效 → 解析链 → 真实 HTTP 出站」串成单条链，十段断言覆盖 §6.2 场景 1-6/10 与删除 / 禁用可见失败路径（显式覆盖零回退、个人默认优先、禁用降级、静态回退 `providerSource=static`）；② 逐场景状态矩阵（场景 7/8/9/10/11 由 QA-1 单测 / QA-3 门禁 / `internal/llm/protocol/equivalence_test.go` 4 个对照用例承接）归档 `docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md`；③ 顺带修正 `TestLLMProtocolOptions` 断言顺序为 §3.1.4 表行序（原断言假定 `openai_responses` 在 `anthropic_messages` 之前，属**测试口径错误、非实现回归**；该用例此前不在 QA-3 定向集内故未暴露，已并入定向集防再漏跑）；④ 证据：`go test ./handlers/ai/ ./router/ ./internal/bootstrap/ ./middleware/ -count=1` 四包全绿 + `go test ./service/ -run 'LLM|Provider|Protocol|Registry|Gateway' -count=1` ok + `go test ./internal/llm/protocol/... -count=1` ok + `gofmt -l` 无输出；⑤ 遗留（真实环境人工）：浏览器级视觉复核（场景 3/5/7/8 的 UI 口径）、真实 Ollama / Anthropic 桩联调（场景 1 的 `variant=ollama`、场景 10①、场景 11 抽查）、运行期日志明文检索（场景 7）——前置条件 / 步骤 / 通过标准见验收记录 §5；**QA-2 完整通过前 B7 不关**。

> ⑬ **DOC-1 文档同步核验（2026-09-24；门禁复跑 2026-09-25）**：① `docs/api-reference.md` 已含「AI 供应商管理（多 LLM Provider）」章节（§3.4 全 10 端点 + `chat[/stream]` 的 `provider` 覆盖与 `done` 回带字段）；`CHANGELOG.md [Unreleased]` 已收录 BE/FE/QA-2/QA-3 条目；本方案 §10 回填至 v1.17；`docs/plan/llm-protocol-adapter-plan.md` v0.7 已记启动门禁达成；② 本轮纠偏：计划内四处 `docs/api/API_REFERENCE.md` 路径不存在（实际文件为 `docs/api-reference.md`），已修正 DOC-1 / B8 / §6.3 DoD / 独立计划引用；③ 新增 **§9.4 验收证据索引**（QA-1 单测清单 / QA-2 报告 + 链路用例 / QA-3 报告 + 路由门禁，含复跑命令），§6.3 DoD 引用同步改指 §9.4；④ 证据：`bash scripts/docs-gate/run-all.sh`（Git Bash）→ **5 gates / 0 failed**（All docs gates passed；末次复跑含 §9.4 与 §10 v1.16 全部改动、退出码 0，日志 `.dev/docs-gate-rerun.log`）；⑤ B8 正式收口待 B7 人工项（浏览器级 UI / 真实桩 / 日志检索）关闭后随批次执行。

> ⑭ **独立计划解冻与 PA-1 交付（2026-09-25）**：① `docs/plan/llm-protocol-adapter-plan.md` 由 Stub（v0.7）升 **v1.0 正式方案**（详细设计 §4 + PA-1..PA-6 拆解 §5 + 变体语义表 + 差异清单 + 验收证据 §6），§11.3 启动门禁就此关闭；② **PA-1 `anthropic_messages` 适配器**落地：新增 `internal/llm/protocol/anthropic_messages.go`（官方 + `minimax` 变体；`x-api-key` + `anthropic-version: 2023-06-01`、system 提升顶层、工具结果转 `user`/`tool_result`、tools `input_schema`、`tool_choice` 归一、事件驱动 SSE 含 `input_json_delta` 工具参数累积、非流式拼接全部 `text` 块、带内 `error` → `*ProtocolError`），`registry.go` 注册 `(anthropic_messages, "")` / `(anthropic_messages, minimax)` 并新增 `DefaultEndpoint(protocol, variant)`；`service/llm_registry.go` 补变体缺省参数（`maxTokens` 4096；`temperature` minimax 1.0 / 官方 0.3）、端点回退，并把 DB 实例路径的构建分派由「openai 默认变体硬编码」泛化为「协议包注册表优先、未注册回退旧分支」（anthropic 两变体由此改由适配器承载，`TestLLMProviderRegistryProtocolVariantMapping` 同步更新期望；见独立计划 §4.3/§4.4 差异清单）；③ 对照测试 `service/llm_anthropic_equivalence_test.go` 锁定与既有 `MiniMaxProvider` 在请求路径 / 鉴权头 / 请求体字段 / 返回文本上等价，协议单测 `internal/llm/protocol/anthropic_messages_test.go` 覆盖非流式 / 流式 / 工具累积 / 带内错误；④ 零破坏：`LLM_PROTOCOL_ADAPTER_ENABLED` 仍默认关、开关关闭构建路径不变、无 DB / API 契约改动；⑤ 证据：`go test ./internal/llm/protocol/... -count=1` ok、service 定向集（Anthropic / ProviderVariantDefaults / ResolveProtocolURL / NewProviderFromConfig / DefaultRegistry / LLMProviderRegistry）全绿、`go build ./...` 与 `go vet ./internal/llm/protocol/... ./service/` exit 0、改动文件 `gofmt -l` 无输出、文档门禁复跑 `bash scripts/docs-gate/run-all.sh` → **5 gates / 0 failed**（日志 `.dev/docs-gate-rerun.log`）。后续 PA-2（`azure` / `ollama` 变体）起按独立计划排期推进。

### 5.1 后端（BE）

| # | 任务 | 依赖 | 交付物 | 验收 |
|:---|:---|:---|:---|:---|
| BE-1 | 数据模型与迁移：两张 ent schema（含 4 值 `protocol`、`variant`、`adapter_options`）+ 生成代码 + expand/down 迁移（**无菜单迁移**） | — | `ent/schema/llm_provider_config.go`、`ent/schema/llm_user_preference.go`、2 个迁移文件（create_expand + down、ai_llm_calls_expand + down）、`make ent` 生成 diff 审查 | `go build ./...` 通过；迁移在空库/存量库（含租户数据）执行成功且可 down；`internal/schema` 守卫测试通过（新表含 tenant_id）；协议枚举/variant 白名单/`adapter_options` 列落库并可按 Q4 留空 endpoint（回退内置地址） |
| BE-2 | `LLMProviderRegistry`：加载/缓存(TTL 30s)/失效/解密/构建/降级 + 单测（含解密失败、跨租户、禁用） | BE-1 | `service/llm_registry.go` + `service/llm_registry_test.go` | `go test ./service/ -run TestLLMProviderRegistry -v` 全绿；解密失败不 panic、日志无明文 |
| BE-3 | `LLMGateway` 覆盖解析：新增 `*WithProvider` 方法、`WithResolver`、错误语义（4xx 不回退）；旧方法行为不变 | BE-2 | `service/llm_gateway.go` 改造 + `service/llm_gateway_test.go` 增量用例 | 既有 gateway 测试全绿（回归门禁）；新增用例覆盖 override 命中/未命中/禁用/key 缺失/静态回退 |
| BE-4 | 管理 API 与处理器：§3.4 全部路由 + DTO + 校验 + 掩码 + 审计 + available + 个人偏好 + import-static（幂等） | BE-3 | `handlers/ai/llm_provider_handler.go`、`dto/llm_provider_dto.go`、`router/ai_routes.go` 变更（开关控制注册；统一 `RequirePermission("system","write")`） | `go test ./handlers/ai/ -v`；curl 冒烟脚本覆盖 §3.4 每行（含 403/404/409/422 分支）；无 `system:write` 角色访问每个端点均 403 |
| BE-5 | bootstrap 接线：加密服务获取、registry 注入网关、硬约束矩阵（§4.1）、开关读取 | BE-4 | `internal/bootstrap/app.go` 变更 | 矩阵表 5 行各有测试或可复现命令证据（含生产占位符+DB 实例可用 → 正常启动） |
| BE-6 | 可观测扩展：`ai_llm_calls.provider_key` 写入 + 可选 Observer 接口 + 日志字段 | BE-3 | `service/ai_telemetry*.go`、`service/llm_gateway.go` observer 路径 | 单测断言 provider_key 写入；静态回退写 NULL；`ai_evaluator` 既有测试不回归 |
| BE-7 | chat/chat-stream 请求覆盖 + `done` 事件回带 provider/source | BE-3 | `handlers/ai/handler.go` 变更 | SSE 集成测试：指定 provider 的调用在 `ai_llm_calls.provider_key` 可见；未指定时走默认链 |
| BE-8 | 协议槽位（v1.2，独立计划的前置）：4 值枚举校验、`(protocol, variant)` → 现有分支映射（§3.1.4）、能力位 `supportsReasoning`、未实现协议 422 `AI_PROTOCOL_NOT_IMPLEMENTED`、`adapter_options` 白名单/大小/敏感键校验 | BE-1 | `service/llm_protocol_slot.go`（枚举常量 + 映射 + 校验）+ `service/llm_protocol_slot_test.go`；DTO 枚举常量 | `go test ./service/ -run TestLLMProtocolSlot -v` 全绿；映射表 4 行各有用例；`openai_responses`/`google_gemini` 创建返回 422；非法 `variant`、非法 `adapter_options`（含敏感键）返回 422 |
| BE-9 | 协议适配器首实现（v1.4，协议按最新设计落地）：`ProtocolAdapter` 接口 + 注册表 + `openai_chat_completions` 适配器（请求构建/响应解析/流式事件/工具调用/错误映射，语义等价既有 `OpenAIProvider`）；Registry 构建按协议分派（适配器优先、旧分支回退，开关控制） | BE-2、BE-3 | `internal/llm/protocol/adapter.go`、`internal/llm/protocol/registry.go`、`internal/llm/protocol/openai_chat.go` + 桩服务测试；`service/llm_registry.go` 构建路径分派 | `go test ./internal/llm/protocol/... ./service/...` 全绿；**对照测试**：同一桩服务下流式 chunk 序列/工具调用降级/错误映射与既有 `OpenAIProvider` 逐项等价；开关关闭时仍走旧路径（diff 级） |

### 5.2 前端（FE）

| # | 任务 | 依赖 | 交付物 | 验收 |
|:---|:---|:---|:---|:---|
| FE-1 | API 客户端 `llm-provider-api.ts`（10 个端点 + 类型 + 错误映射） | BE-4 | `src/lib/api/llm-provider-api.ts` | 类型与后端 DTO 对齐（tsc 通过）；错误码可读提示 |
| FE-2 | 「系统管理 → 系统配置 → LLM 模型」页签（列表/表单：协议 4 值 + variant + adapter_options /测试/设默认/启停/删除/导入） | FE-1 | `src/pages/(main)/admin/system-config/llm-provider-settings.tsx` + 宿主页 `index.tsx` 的 `tabItems` 变更 | 手动验收清单：创建→测试→设默认→禁用→删除全链路；未实现协议选项置灰且提交时 422 提示可读；无 `system:write` 用户看不到页签（直连 API 403） |
| FE-3 | 权限渲染与回归：页签/选择器按 `hasPermission('system','write')` 控制；**路由与菜单零变更** | FE-2 | 宿主页权限判定 + `src/lib/hooks/__tests__/use-permissions.test.ts` 追加用例 | 系统管理员可见；非系统管理员页签不渲染、会话 UI 与现状一致；`src/routes/index.tsx` 与 CSV 无 diff |
| FE-4 | 会话页 provider 选择器 + 请求透传 + 生效标签 + 个人默认（仅系统管理员） | FE-1 | `components/ai/AIChat.tsx`、`lib/api/ai-api.ts` 变更 | 切换后请求体含 provider；done 标签显示生效实例；无 `system:write`/单 provider/开关关闭时不渲染（UI 与现状一致） |

### 5.3 测试与文档（QA/DOC）

| # | 任务 | 依赖 | 交付物 | 验收 |
|:---|:---|:---|:---|:---|
| QA-1 | 后端单测/集成测试补齐（registry、gateway、handler、协议槽位、协议适配器、硬约束矩阵、加密往返） | BE-1..BE-9 | 测试文件增量 | `go test ./internal/llm/protocol/... ./service/... ./handlers/ai/... ./internal/bootstrap/...` 全绿；覆盖率不低于改动前 |
| QA-2 | 端到端验收（多协议真实/桩服务）：chat completions 桩（variant 空 + `azure`）+ anthropic messages 桩（variant `minimax`）各建实例，切换验证；未实现协议拒绝路径验证；`openai_chat_completions` 适配器路径与旧分支等价对照 | FE-4, QA-1 | `handlers/ai/llm_provider_qa2_flow_test.go`（API 级链路用例）+ `docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md`（验收记录：逐场景状态 + 人工遗留） | §6.2 场景 1-11 全部通过（自动化口径状态见验收记录；UI / 日志人工项见其 §5） |
| QA-3 | 回归门禁：开关关闭时全链路行为与现状一致（diff 级） | BE-5, FE-3 | 回归清单与证据（`docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`；新增 `router/llm_provider_routes_test.go` 门禁用例） | 关闭开关：无新路由、无新 UI、接口响应字段与现状一致 |
| DOC-1 | 文档同步：`CHANGELOG.md [Unreleased]`、`docs/api-reference.md`（新端点；已含「AI 供应商管理（多 LLM Provider）」章节）、本方案 §10 修订记录回填、`docs/plan/llm-protocol-adapter-plan.md` 槽位状态同步 | 全部 | 文档 diff | `make docs-gate` 通过（advisory） |

### 5.4 迭代批次与排期（最佳实践）

> **调度原则**：① **关键路径优先**（数据层 → 服务内核 → 网关 → API 契约 → 接线/首适配器 → 验收）；② **风险前置**（协议槽位、适配器等价性、注册表解密/缓存最易出错，放最早批次并先出单测与对照测试）；③ **测试左移**（单测随任务交付，QA-1 自 B2 起滚动收敛，不在末尾集中）；④ **契约冻结**（B4 出口冻结 §3.4 契约与 DTO 枚举常量，前端此后只依赖契约，契约变更须回写 §3.4 并升版通知）；⑤ **并行安全**（同批次任务文件所有权互不相交，WIP ≤ 3）；⑥ **每批可回滚**（开关默认关闭，B1–B5 全程无用户可见变更；迁移 expand/down 独立可逆；适配器异常时回退旧分支）；⑦ **时间盒 + 缓冲**（单任务超 3 日即拆分，总工期含 20% 缓冲）。
>
> **排期假设**：1 名后端 + 1 名前端 + QA 滚动介入（可兼岗）；「Dn」= 第 n 个工作日，自评审通过次日起算；桩服务（chat completions / anthropic messages）在 B0 就绪。

| 批次 | 目标（出口状态） | 任务（并行轨） | 净人日 | 依赖 | 出口门禁（未过不进制下一批） | 自然日 |
|:---|:---|:---|:---:|:---|:---|:---|
| **B0 评审冻结与准备** | 方案签署、环境就绪 | —（评审 + 桩服务/迁移工具链演练） | 0.5 | — | §11 评审检查点 4 项确认；两个桩服务可返回（含流式）响应 | D1 |
| **B1 数据层** | 表与迁移落地，零行为变化 | BE-1 | 2 | B0 | `go build ./...`；空库/存量库 up+down 成功；schema 守卫测试（tenant_id）过；`protocol`/`variant`/`adapter_options` 列可见 | D2–D3 |
| **B2 服务内核** | 注册表与协议槽位可用（未接线） | BE-2 ∥ BE-8 | 3.5 | B1 | `TestLLMProviderRegistry`、`TestLLMProtocolSlot` 全绿；映射表 4 行用例 + 422 分支（未实现协议/非法 `variant`/敏感键 `adapter_options`）全覆盖；解密失败不 panic、日志无明文 | D4–D5 |
| **B3 网关覆盖** | 覆盖解析与降级语义可用（未对外） | BE-3 | 1.5 | B2 | 既有 gateway 测试全绿（回归门禁）；override 命中/未命中/禁用/key 缺失/静态回退用例齐 | D6–D7 |
| **B4 API 契约与可观测** | 管理 API 完成、契约冻结，观测字段落库 | BE-4 ∥ BE-6 ∥ BE-7 | 5 | B3 | curl 冒烟覆盖 §3.4 每行（含 403/404/409/422）；无 `system:write` 全端点 403；**契约冻结公告（解锁 FE-1）**；`provider_key` 写入与静态回退 NULL 单测过 | D8–D10 |
| **B5 接线、开关与首适配器** | 全链路可开箱即用（默认关）+ 协议层按最新设计落地 | BE-5 ∥ BE-9 | 4 | B4 | §4.1 硬约束矩阵 5 行证据；开关关闭时与现状 diff 级一致；`go test ./internal/llm/protocol/...` 全绿且对照测试证明与旧实现等价；**独立计划启动门禁达成（见下）** | D11–D13 |
| **B6 前端** | 配置页签 + 会话选择器可用（受权限/开关控制） | FE-1 → FE-2 →（FE-3 ∥ FE-4） | 6 | B4（契约） | tsc 过；手动全链路（创建→测试→设默认→禁用→删除）；未实现协议置灰且 422 提示可读；`src/routes/index.tsx` 与 CSV 零 diff；非系统管理员不可见（直连 403） | D11–D16 |
| **B7 验收** | 场景 1–11 全过 + 回归门禁 | QA-2（QA-1 自 B2 滚动、QA-3 随 B6 并行） | 5 | B5、B6 | §6.2 场景 1–11 全部通过（含拒绝路径与适配器等价对照）；QA-3 关闭开关回归证据；覆盖率不低于改动前 | D17–D19 |
| **B8 交付** | 文档同步与合入 | DOC-1 | 0.5 | B7 | `make docs-gate`（advisory）过；`CHANGELOG.md [Unreleased]`、`docs/api-reference.md`、两计划互引回填 | D20 |

- **工期与关键路径**（净 ≈27.5 人日）：`B1→B2→B3→B4→B5`（后端 D1–D13）与 `B6 前端`（至 D16）后段汇合，`QA-2 → DOC-1` 收口 —— **总工期约 20 个工作日（4 周），含 20% 缓冲约 24 个工作日（5 周）**（BE-9 的 3 个工作日与前端并行、被 B6 吸收，不改总工期）。关键路径上 B2/B5/B6 为最大块；BE-8（并行 BE-2）与 BE-9（并行 BE-5）均不在关键路径上，但二者共同构成独立计划启动门禁。
- **人力弹性**：① 双后端 → BE-6/BE-7 前移至 B3 与 BE-3 并行（文件不相交），关键路径压缩 ≈2 个工作日；② 前端按 §3.4 契约先做 UI 骨架（mock 数据），B6 可提前 2–3 日与 B4/B5 并行，总工期压缩至 ≈17 个工作日；③ 无专职 QA 时 QA-1 由开发承担，**QA-3 开关回归门禁不得豁免**。
- **独立计划启动门禁**：B5 出口达成（此时 BE-8 已随 B2 合入、BE-9 已随 B5 合入）后，`docs/plan/llm-protocol-adapter-plan.md` 方可升 v1.0 并排期；**B1–B5 期间不得并行修改** `NewProviderFromConfig`、既有 4 个实现分支与 `internal/llm/protocol/`（避免与适配层计划产生冲突，见 §11.3）。

---

## 6. 测试策略与验收标准

### 6.1 测试分层

| 层 | 范围 | 方式 |
|:---|:---|:---|
| 单元 | registry 缓存/失效/解密失败；gateway override；DTO 校验；默认互斥事务 | `go test ./service/... ./handlers/ai/...`（sqlite in-memory，参照 `service/ai_evaluator_test.go` 建表模式） |
| 协议槽位 | 4 值枚举、`(protocol, variant)` 映射、`adapter_options` 校验、未实现协议 422 | `go test ./service/ -run TestLLMProtocolSlot -v` |
| 协议适配器 | P0：`openai_chat_completions` 的请求构建/响应解析/流式事件/工具调用/错误映射，以及与既有 `OpenAIProvider` 的对照等价 | `go test ./internal/llm/protocol/...`（桩服务 httptest） |
| 集成 | 迁移可执行性（expand/down）；SSE 覆盖参数；租户隔离（跨租户 404） | 测试库 + httptest；迁移走 `migration` 工具链 |
| 契约 | 新端点请求/响应/错误码 vs 前端类型 | curl 冒烟脚本 + tsc |
| 手动 | 「LLM 模型」页签全链路、选择器体验、开关关闭回归 | 验收记录（QA-2/QA-3） |
| 安全 | 密钥不回显（全端点扫描响应体）、日志无明文、加密 AAD 绑定 | 自动化断言 + 日志抽查 |

### 6.2 验收场景（QA-2 必过项）

1. 系统管理员创建实例 A（`openai_chat_completions`，variant 空，兼容网关桩）、实例 B（`openai_chat_completions` + `variant=ollama`，Ollama 桩），均测试连通成功。
2. 设 A 为租户默认；与会话中不传 provider → 命中 A（`providerSource=tenant`，`ai_llm_calls.provider_key='A'`）。
3. 系统管理员在会话选择器切换到 B 并发送 → 命中 B（`providerSource=request`）；因 B 不支持流式，前端按既有降级路径产出一次性回答（不报错）。
4. 系统管理员设置"我的默认 = B" → 新会话不传 provider 命中 B（`providerSource=user`）。
5. 系统管理员禁用 B → 该管理员下次请求降级为租户默认 A（`providerSource=tenant`），会话页出现失效提示；直接 `provider=B` 请求返回 409 `AI_PROVIDER_DISABLED`。
6. 删除全部 DB 实例并关闭开关 → 行为回到静态配置单 provider（现状），重启进程后与当前线上版本一致。
7. 安全断言：`GET /ai/providers` 响应仅含掩码；后端日志全文检索无明文密钥；生产占位符 + DB 可用实例场景启动成功（矩阵 §4.1）。
8. 权限边界（非系统管理员）：系统配置页看不到「LLM 模型」页签；`GET /ai/providers`、`GET /ai/providers/available`、`GET /ai/user-preference` 均 403；`POST /ai/chat` 携带 `provider` 返回 403 `AI_PROVIDER_FORBIDDEN`；不带 `provider` 的会话与现状一致（命中租户默认/静态）。
9. 幂等导入：对同一静态配置连续执行两次 `import-static` → 第一次 200 创建、第二次 200 `{updated:true}`，记录数不增。
10. 协议槽位与映射：① 创建 `anthropic_messages` + `variant=minimax`（endpoint 留空回退内置地址）→ 走 `minimax` 分支，测试连通成功；② 创建 `protocol=openai_responses` 或 `google_gemini` → 422 `AI_PROTOCOL_NOT_IMPLEMENTED`，前端对应选项置灰；③ `variant` 非法值、`adapter_options` 携带 `api_key`/`authorization` 等敏感键 → 422。
11. 协议适配器等价性：同一 chat completions 桩服务下，同一实例分别以「适配器路径（开关开）」与「旧分支路径（开关关）」调用 → 流式 chunk 序列、工具调用降级行为、错误映射逐项一致（对照测试 + 人工抽查）。

### 6.3 完成定义（DoD）

- BE/FE 全部任务合入且 CI 全绿；QA-1..QA-3 证据归档在 PR 描述或本方案 §9.4 验收证据索引。
- `CHANGELOG.md` 更新；`docs/api-reference.md` 更新；ROADMAP/README 若有能力声明按 governance 规则同步。
- 灰度开关默认关闭发布；开启步骤写入部署说明（README 或 `docs/` 运维文档，按 governance 归属）。

---

## 7. 风险与对策

| # | 风险 | 等级 | 对策 |
|:---|:---|:---:|:---|
| R-1 | 密钥二次存储面扩大（DB 泄露面） | 高 | AES-GCM + AAD；API 仅掩码；日志断言；无加密密钥时功能整体关闭而非明文降级（§3.5） |
| R-2 | 多副本缓存不一致（≤30s） | 中 | 写入响应携带版本提示；页面操作后前端主动刷新；对"禁用"提供的兜底为请求级校验（resolver 每次调用前校验 enabled 快照，禁用后最迟 TTL 后全面生效） |
| R-3 | 误删/误禁用默认 provider 造成全租户 AI 不可用 | 中 | 默认实例禁止直接删除（409 引导先切换）；禁用默认实例时返回 409 或显式二次确认（实施取二选一，建议 409 强约束）；降级链保留静态配置兜底 |
| R-4 | 协议能力差异（流式/工具调用）导致体验不一致 | 中 | `available` 返回能力位；前端徽标 + 非流式降级路径已有（`llm_gateway.go:250-287`）；工具调用链不开放覆盖（D4） |
| R-5 | 切换能力（系统管理员）→ 数据发往非预期供应商（合规） | 高 | 实例必须由系统管理员显式创建/启用（相当于白名单）；默认仍为租户默认；切换行为可审计；切换面仅系统管理员（普通用户无入口）；对公网端点在部署文档中提示数据出域风险 |
| R-6 | 双源真相漂移（config.yaml vs DB） | 中 | D10 优先级链 + 管理页"静态回退"状态可见 + import-static 显式收敛 |
| R-7 | RAG embedding 仍绑定静态配置，用户误以为切换 provider 后 RAG 也切换 | 低 | 页面文案明确"Embedding/知识库检索仍使用部署级配置"；非目标已声明 |
| R-8 | `ai_llm_calls` 列变更影响既有统计 SQL | 低 | 仅新增可空列；`ai_telemetry_repository.go`/`ai_evaluator.go` 的既有查询不动；回归测试覆盖 |
| R-9 | 未知/未实现协议被误用（现状"静默退化 openai"缺陷的变体） | 低 | API 层枚举校验 + 未实现协议 422 `AI_PROTOCOL_NOT_IMPLEMENTED`（D3/BE-8）+ 表单下拉置灰，三重阻断 |
| R-10 | 产品口径"用户可切换"与权限定稿"系统管理员"存在认知差 | 低 | 本文档 §0/§8-Q1 已明确口径；`CHANGELOG`/README 能力描述统一写"系统管理员可切换 provider"；若后续产品要求全员切换，按 Q1 的 P1 路径单点放开（仅权限码与前端渲染条件，网关与数据模型零改动） |
| R-11 | 用户误以为 4 种协议全部可用（P0 仅 2 种有实现） | 低 | 前端选项置灰并标注「待接入 · 独立计划」；`available` 返回能力位 + `implemented` 标志；文档口径统一（§0、§3.1.4、§11） |
| R-12 | `ai_llm_calls.provider` 新旧值并存（历史行为旧实现名） | 低 | 不做历史回填（仅新增语义）；实例粒度由 `provider_key` 提供；统计如需归类在查询层处理（非 P0），`ai_evaluator` 既有测试不回归 |
| R-13 | 首适配器与既有实现行为漂移（流式/工具调用/错误语义） | 中 | 对照测试（同桩服务逐 chunk/逐字段比对，BE-9）+ 开关关闭即回退旧分支（D9）+ B5 出口门禁强制等价证据；R-4 的能力位与非流式降级路径继续兜底 |
| R-14 | prod 迁移队列被历史缺陷卡住（6 条账本 checksum 漂移 + 2 个 pre-cutoff 菜单文件会中断批次），本方案迁移届时无法自动应用 | 中 | 上线前置 runbook §4.4③：R1 收敛 checksum → R2 收养两菜单文件（终态已满足）→ R3 `-ro` 复核 pending=2 → R4 发布；全部可在发布前独立完成 |

---

## 8. 开放问题（已按最佳实践确认，2026-09-24 关闭）

> 以下为评审定稿结论（用户决策 + 最佳实践），实施以本节为准；如需变更须回写本节并升版。

| # | 问题 | 定稿结论（已确认） | 落实位置 |
|:---|:---|:---|:---|
| Q1 | 可切换 provider 的范围 | **系统管理员**（`system:write` 持有者，内置即 `sysadmin`；`super_admin` 走既有超管旁路）。普通用户不渲染选择器、不可设置个人默认；请求带 `provider` 但无权限 → 403 `AI_PROVIDER_FORBIDDEN`。放宽路径（P1）：仅把 `available`/`user-preference` 读端点降为 `ai:read` 并放开前端渲染条件，网关与数据模型零改动 | §2.2 D12、§3.4、§3.5、§3.6 |
| Q2 | 个人默认的管理员可见性 | P0 仅本人可见可改；P1 在「LLM 模型」页签内加只读"个人偏好"列表（排障/交接用），不阻塞 P0 | §3.6 |
| Q3 | 同协议多实例并存 | **允许**：`name` 唯一即可，`(protocol, model, endpoint)` 不设唯一约束；多环境（生产网关/备用网关/局域网模型）按实例并存 | §3.1.1 |
| Q4 | `anthropic_messages`（`variant=minimax`）端点是否可配（原表述：`minimax` 端点） | **开放为可选字段**：`endpoint` 留空时回退现有内置地址，向后兼容；避免私有化/代理部署为换网关改代码（与其它协议字段一致） | §3.1.1、§3.1.4、§5.1 BE-1 |
| Q5 | 静态配置重复导入 | **幂等**：按 `(protocol, variant, endpoint, model, deployment)` 归一化匹配既有记录 → 命中则更新（含密钥重新加密）并返回 `200 {updated:true}`；仅 `name` 冲突且配置不同 → 409 提示改名。避免同源实例重复堆积 | §3.4、§6.2 场景 9 |
| Q6 | 是否按场景绑定默认 | 本期不做：维持"请求 → 个人默认 → 租户默认 → 静态"单一解析链，避免配置爆炸；若 P1 需要，在 Registry 的 `Resolve` 增加 `purpose` 入参即可（调用面已隔离在网关/registry 内部） | §3.3 |
| Q7 | 协议分类口径与适配层落地路径（v1.2 新增，v1.4 更新） | **定稿**：协议按 4 种 API 形态建模（`openai_chat_completions` / `openai_responses` / `anthropic_messages` / `google_gemini`），`azure`/`ollama`/`minimax` 降为 `variant`；本方案预留 5 类槽位（D13）并**在 P0 实现首个适配器 `openai_chat_completions`**（BE-9：接口 + 注册表 + 适配器，语义等价既有实现、开关可回退）；其余 3 协议的适配器化与语义归一化由独立计划 `docs/plan/llm-protocol-adapter-plan.md` 推进，参考 `E:\projects\ai-agent-runtime`（`ProtocolAdapter` + 4 适配器 + 兼容层） | §2.2 D3/D13、§3.1.4、§5.1 BE-9、§11 |

---

## 9. 附录

### 9.1 关键代码坐标索引

| 主题 | 坐标 |
|:---|:---|
| 4 协议实现与分派（现状，P0 映射目标） | `itsm-backend/service/llm_providers.go:18-36`（openai）、`:307-357`（azure）、`:359-562`（local）、`:379-493`（minimax）、`:564-613`（ProviderConfig / NewProviderFromConfig） |
| 协议适配参考实现（外部仓库，只读） | `E:\projects\ai-agent-runtime\backend\internal\llm\adapter\`：`adapter.go:70-114`（`ProtocolAdapter` 接口）、`factory.go:9-22`（类型→适配器注册）、`openai.go:1111`（`/v1/chat/completions`）、`codex.go:876`（`/v1/responses`）、`anthropic.go:795`（`/v1/messages`）、`gemini.go:591-595`（v1beta，路径由调用侧拼装）；厂商兼容层 `providercompat/`（`Context.Protocol` + `Context.Profile` 双维度，`providercompat.go:10-42`、`registry.go:3-12`）；能力解析 `internal/llm/model_capability.go:16-36` |
| 协议槽位文档（本仓库） | `docs/plan/llm-protocol-adapter-plan.md`（独立计划，槽位预留；边界见本方案 §11） |
| 网关结构与能力探测 | `itsm-backend/service/llm_gateway.go:12-76`（结构/接口）、`:78-130`（重试策略）、`:250-287`（工具调用降级） |
| 启动装配与硬约束 | `itsm-backend/internal/bootstrap/app.go:603-643` |
| AI 路由与现状权限 | `itsm-backend/router/ai_routes.go:15-47`；`internal/authz/catalog.go:161-162`（现状 `ai:read/ai:write`，本方案管理面不复用） |
| 系统管理权限先例 | `itsm-backend/router/router.go:387`、`handlers/vector_store/handler.go:224`（`RequirePermission("system","write")`）；`internal/authz/roles.go:24-28`、`catalog.go:153` |
| SSE 请求契约 | `itsm-backend/handlers/ai/handler.go:179-188`；前端 `src/lib/api/ai-api.ts:490-531` |
| 加密服务 | `itsm-backend/middleware/encryption.go:21-28`；`connector/persistent_store.go:20` |
| 加密落库先例 | `itsm-backend/ent/schema/connector_config.go:11-39` |
| 个人偏好先例 | `itsm-backend/ent/schema/notification_preference.go:16-60`（仅系统管理员写入） |
| 可观测仓储 | `itsm-backend/service/ai_telemetry_repository.go:29-36`；`service/ai_evaluator.go:346-352` |
| 租户守卫 | `itsm-backend/internal/schema/tenant_guard.go:58-89` |
| 迁移命名规范 | `itsm-backend/migrations/`（`YYYYMMDD_snake_case.sql` + `_down.sql`），规范见 `docs/plan/generic-attachment-richtext-control-plan.md` §1.2；菜单注入先例 `20260628_add_connector_menu.sql` 本方案不使用 |
| 前端管理页先例 | `itsm-frontend/src/pages/(main)/admin/connectors/index.tsx`；页签接入点 `src/pages/(main)/admin/system-config/index.tsx:494-513`；前端权限 hook `src/lib/hooks/use-permissions.ts:35-36` |
| 前端会话页 | `itsm-frontend/src/components/ai/AIChat.tsx`；`src/pages/(main)/ai/chat/index.tsx` |
| 路由真相源 | `docs/plan/_data/vite-route-map.csv`（表头 9 列；本方案不新增路由，不变） |

### 9.2 查询现状的命令速查（评审复现用）

```text
# 后端协议分派点
rg -n "NewProviderFromConfig|case \"minimax\"|case \"azure\"" itsm-backend/service/llm_providers.go
# 网关单实例装配
rg -n "NewLLMGateway|IsPlaceholderSecret" itsm-backend/internal/bootstrap/app.go
# 前端是否存在 LLM 配置实现（本方案实施前预期：无命中）
rg -n "llm.provider|LLM_PROVIDER|llm-provider" itsm-frontend/src
# 权限收口：system:write 的持有者（预期仅 sysadmin 的 allPermissionCodes 命中，其余为显式排除列表）
rg -n '"system:write"' itsm-backend/internal/authz/roles.go
```

### 9.3 术语表

| 术语 | 含义 |
|:---|:---|
| 协议（protocol） | 4 种 **API 形态**：`openai_chat_completions` / `openai_responses` / `anthropic_messages` / `google_gemini`（与厂商无关） |
| 兼容变体（variant） | 同一 API 形态下的厂商/部署差异（槽位字段）：`azure` / `ollama` / `minimax` 等；语义由适配层计划接管 |
| 协议槽位 | 本方案为协议层预置的接入点：枚举 / `variant` / `adapter_options` / 协议层接口与首适配器（P0 交付）/ 能力位 / 错误码（D13、§11） |
| 实例 / provider key | 协议 + 端点 + 模型 + 密钥的一条具体配置（表内 `name`），如 `deepseek-prod` |
| 租户默认 | DB 内 `is_default=true`，租户内唯一 |
| 个人默认 | `llm_user_preferences.provider_key`，空 = 跟随租户默认；仅系统管理员可设置 |
| 静态配置 | `config.yaml` / 环境变量中的 `llm.*`，部署级单 provider，作为最终回退 |

### 9.4 验收证据索引（QA-1..QA-3）

| 任务 | 证据 | 入口 |
|:---|:---|:---|
| QA-1 | 后端单测 / 集成测试（registry、gateway、协议槽位、协议适配器、启动矩阵、遥测聚合、管理面、会话覆盖、路由门禁） | `itsm-backend/service/llm_registry_test.go`、`service/llm_gateway_test.go`、`service/llm_protocol_slot_test.go`、`service/llm_provider_startup_test.go`、`service/ai_telemetry_provider_key_test.go`、`handlers/ai/llm_provider_admin_test.go`、`handlers/ai/llm_provider_chat_test.go`、`internal/llm/protocol/*_test.go`、`router/llm_provider_routes_test.go` |
| QA-2 | 端到端验收（API 级链路；浏览器 / 真实桩 / 日志人工项见报告 §5） | `docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md`；`itsm-backend/handlers/ai/llm_provider_qa2_flow_test.go`（`TestLLMProviderQA2RuntimeSwitchFlow`） |
| QA-3 | 开关关闭全链路回归（13 维度） | `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`；`itsm-backend/router/llm_provider_routes_test.go` |

复跑（workdir `itsm-backend`）：

```powershell
go test ./handlers/ai/ ./router/ ./internal/bootstrap/ ./middleware/ -count=1
go test ./service/ -run 'LLM|Provider|Protocol|Registry|Gateway' -count=1
go test ./internal/llm/protocol/... -count=1
```

前端（workdir `itsm-frontend`）：`npx tsc --noEmit`；`npx jest src/lib/api/__tests__/ai-api.test.ts src/lib/hooks/__tests__/use-llm-provider-feature.test.ts src/lib/hooks/__tests__/use-permissions.test.ts --silent`。

---

## 10. 修订记录

| 版本 | 日期 | 说明 |
|:---|:---|:---|
| v1.0 | 2026-09-24 | 初稿：现状诊断（4 协议/单实例/无前端配置页）、多 provider 存储与网关解析设计、管理 API 与前端页面、灰度与回滚、任务拆解与验收标准；开放问题 Q1-Q6 待评审 |
| v1.1 | 2026-09-24 | 评审定稿：① 配置入口并入「系统管理 → 系统配置」页签（取消 `/admin/llm-providers` 独立页与路由/菜单/CSV 三件套）；② 权限范围收敛为系统管理员（`system:write`，含读端点与 chat `provider` 覆盖参数的服务端校验）；③ §8 开放问题按最佳实践确认关闭（Q1 系统管理员可切换、Q4 minimax endpoint 可选、Q5 导入幂等、Q6 维持单默认链）；④ 迁移清单/任务拆解/验收场景/风险同步更新 |
| v1.2 | 2026-09-24 | 协议口径修正与槽位预留：① 协议枚举由 `openai/azure/local/minimax` 修正为 4 种 API 形态（`openai_chat_completions`/`openai_responses`/`anthropic_messages`/`google_gemini`），`azure`/`ollama`/`minimax` 降为 `variant`（D3）；② 新增 `variant` + `adapter_options` 字段与 §3.1.4 映射表、`AI_PROTOCOL_NOT_IMPLEMENTED`(422)；③ 预留 5 类槽位（D13、§11）并把协议适配层拆为独立计划 `docs/plan/llm-protocol-adapter-plan.md`（参考 `E:\projects\ai-agent-runtime`）；④ §0/§1/§3/§4/§5/§6/§7/§8/§9 同步（新增 BE-8、验收场景 10、风险 R-11/R-12、开放问题 Q7） |
| v1.3 | 2026-09-24 | 排期落地（最佳实践）：① 新增 §5.4 迭代批次与排期——B0 评审冻结 → B1 数据层 → B2 服务内核（BE-2 ∥ BE-8）→ B3 网关覆盖 → B4 API 契约与可观测（BE-4 ∥ BE-6 ∥ BE-7）→ B5 接线与开关 → B6 前端 → B7 验收 → B8 交付，含逐批出口门禁、净 ≈24.5 人日与 ≈20/24 工作日工期；② 调度原则（关键路径优先/风险前置/测试左移/契约冻结/并行安全/每批可回滚/时间盒+20% 缓冲）与人力弹性；③ 独立计划启动门禁（B5 出口）回写 §11.3，并同步 `docs/plan/llm-protocol-adapter-plan.md` v0.3 |
| v1.4 | 2026-09-24 | AI 协议按最新设计处理（P0 实现首个适配器）：① 新增 BE-9——`ProtocolAdapter` 接口 + 注册表 + `openai_chat_completions` 适配器（语义等价既有 `OpenAIProvider`、开关可回退），Registry 构建按协议分派；② D13 由"槽位占位"升级为"槽位 + 首适配器 P0 落地"，§3.1.4/§3.2 同步（`azure`/`ollama` 变体 P0 仍走旧分支，适配器化归独立计划）；③ §5 任务/依赖、§5.4 排期（B5 = BE-5 ∥ BE-9，净 ≈27.5 人日）、§6 测试与验收（新增场景 11 适配器等价对照，QA 门禁 1–11）、§7 新增 R-13 行为漂移风险；④ §11 槽位表/交接清单与独立计划范围缩小为"其余 3 协议 + 收敛"，启动门禁改为 B5 出口（BE-8/BE-9 均已合入）；⑤ 同步 `docs/plan/llm-protocol-adapter-plan.md` v0.4 |
| v1.5 | 2026-09-24 | BE-9 状态注记：`internal/llm/protocol` 协议包（接口/注册表/`openai_chat_completions` 适配器 + 桩服务对照测试）已先行落地并通过 `go test ./internal/llm/protocol/...`；接线（Registry 分派/开关/回归）仍随 B5 |
| v1.6 | 2026-09-24 | BE-9 接线完成（B5 的适配器轨）:① `service/llm_registry.go` 落地按 `(protocol, variant)` 的构建分派（适配器优先、未注册回退旧分支）+ `NewProtocolProvider` + `ErrProtocolNotImplemented`（422 哨兵）；② `NewProviderFromConfig` 接入分派 + `ProviderConfig.ProtocolAdapterEnabled` 字段 + `config.yaml`/env 开关 `LLM_PROTOCOL_ADAPTER_ENABLED`（默认关、env 优先、非法值按关）；③ `service/llm_registry_test.go` 对照测试锁定与既有 `OpenAIProvider` 等价（非流式正文/流式增量序列/工具调用累积/请求体字段与鉴权头/错误身份与网关重试口径），并覆盖开关关闭回退与 azure/ollama/minimax 变体回退；④ 证据：`go test ./internal/llm/protocol/... -count=1`、`go test ./service/ -run TestProtocol -count=1` 等三组用例全绿、`gofmt -l` 无输出；DB 实例路径（BE-1..BE-5）未开工 |
| v1.7 | 2026-09-24 | BE-8 协议槽位落地（B2 的独立轨，与 BE-1 并行）：① 新增 `service/llm_protocol_slot.go`——4 值协议枚举常量（与 `internal/llm/protocol` 同源）、§3.1.4 映射表代码化（`openai_chat_completions` → openai/azure/local、`anthropic_messages` → minimax）、variant 白名单校验、能力位（按 P0 现有实现真实填写：仅 openai 默认变体具备流式+工具+推理）、槽位哨兵（`AI_PROTOCOL_INVALID`/`AI_PROTOCOL_VARIANT_INVALID`/`AI_ADAPTER_OPTIONS_INVALID`，未实现协议复用 `AI_PROTOCOL_NOT_IMPLEMENTED`）、`adapter_options` 校验（JSON 对象 ≤4KB、敏感键黑名单含驼峰/连字符切分且不误伤 `max_tokens`/`api_version`、嵌套深度上限 6，密钥只允许走 `encrypted_api_key`）；`LLMProtocolOptions()` 作为 BE-4 DTO / FE-2 下拉数据源；② 新增 `service/llm_protocol_slot_test.go`：映射表逐行锁定、变体非法/协议非法/槽位 422 分支、能力位、`adapter_options` 14 类非法输入 + 4KB 边界；③ 证据：`go test ./service/ -run 'TestLLMProtocolSlot|TestValidateAdapterOptions|TestIsSensitiveAdapterOptionKey' -count=1` 全绿、`gofmt -l` 无输出；④ 边界：未触碰 `NewProviderFromConfig` 与既有实现分支（§11.3 门禁） |
| v1.8 | 2026-09-24 | BE-1 数据层完成（B1 出口）：① 两租户级表 ent schema + 生成代码 + 4 个 expand/down 迁移落地（RLS 策略、`(tenant_id,name)` 唯一索引、每租户单默认部分唯一索引、索引名与 ent 生成名对齐）；② 证据：`go build ./...` exit 0、`internal/schema` 守卫测试 ok、`cmd/migration-lint -strict` 双迁移 exit 0；Postgres 空库/存量库 up+down 因本机无实例列入环境受限项（补跑命令已记录于 §5 落地状态）；③ BE-2 `LLMProviderRegistry` 已下发子代理（追加式改动），B2 批次推进中 |
| v1.9 | 2026-09-24 | BE-2 + BE-3 完成（B2/B3 出口）：① `LLMProviderRegistry`（快照缓存 TTL 30s/失效/解密/构建/降级；四来源常量 + 四哨兵；跨租户强制过滤）与 `LLMGateway` 覆盖解析（`WithResolver` + `ProviderRequest`/`ProviderResolution` + `ChatWithProvider[Info]`/`ChatStreamWithProvider[Info]`/`ChatWithRequest`/`ChatStreamWithRequest`/`SupportsToolCallingFor`）；② 语义：显式选择失败不回退、空 override 走「个人默认→租户默认→静态」、静态回退保持现状、旧方法零改动、重试/限流/观测口径不变；③ 计划偏差（已实现，待评审追认）：`SupportsToolCallingFor` 加 `ctx`、新增 `*Info`/`*Request` 方法族（回带 `provider`/`providerSource`；个人默认需显式 `UserID`，因 service 层无 user id 上下文）；④ 证据：定向集 144 用例全绿（registry 10 组 + gateway 新增 8 组）、`gofmt -e -l` 无输出；⑤ 待办：BE-4/BE-5/BE-6/BE-7（BE-5 注入 resolver 时顺带核对 `ProviderConfig.ProtocolAdapterEnabled` 双开关；BE-7 传 `ProviderRequest{Key,TenantID,UserID}`） |
| v1.10 | 2026-09-24 | BE-4 + BE-6 + BE-7 + BE-5 完成（B4/B5 后端轨）：① BE-4 管理 API——§3.4 全 10 条路由（列表 / 新建 / 更新 / 软删 / 连通性测试 / 设默认 / `import-static` 幂等 / `available` / 个人偏好读写）+ DTO 校验（协议 / variant / `adapter_options` / endpoint）+ 密钥掩码 + 审计 + 默认互斥；统一 `system:write`，受 `LLM_MULTI_PROVIDER_ENABLED` 注册门禁（`router/llm_provider_routes.go`，分组级权限挂载以通过路由守卫一致性测试）；② BE-6 可观测——`ai_llm_calls.provider_key` 写入（静态回退写 NULL）+ `ai_metrics.byProvider` 聚合（开关关闭时零查询零字段）；③ BE-7——`/ai/chat`、`/ai/chat/stream` 可选 `provider` 覆盖，非 `system:write` → 403 `AI_PROVIDER_FORBIDDEN`，SSE `done` 回带 `provider`/`providerSource`；④ BE-5 bootstrap 接线——加密服务（`ResolveLLMProviderEncryptionKey`）→ registry → 网关注入、§4.1 硬约束矩阵（生产 + 占位密钥 + 无可用 DB 实例 → Fatal；有可用实例 → 放行）、双开关读取；⑤ 证据：`go test ./internal/llm/protocol/... ./internal/bootstrap/ ./middleware/ ./router/ ./handlers/ai/ -count=1` 全绿 + `service` 定向集 60+ 用例全绿（含 `TestEvaluateLLMKeyStartupMatrix`、遥测聚合 6 组、registry 10 组、gateway 全量）；⑥ RBAC 生成物同步：`go run ./cmd/authz-gen` 重新生成 `middleware/rbac_precheck_gen.go`（新路由预检由 `ai:read`/`ai:write` 回退修正为声明的 `system:write`，`TestPrecheckMapIsFresh`/`TestRoutePrecheckAlignment` 红灯转绿）；⑦ 遗留：QA-3 开关关闭全链路回归（依赖前端批次）与 Postgres 迁移空库/存量库 up+down（环境受限） |
| v1.11 | 2026-09-24 | BE-1 迁移实测完成（真实 DB `172.18.3.238`，隧道 `127.0.0.1:15433`）：① 存量库克隆（prod 账本 1:1）与空库（ent 基线）两形态 up+down 全绿（断言：2 表 / `provider_key` 可空 / RLS+策略×2 / 4 命名索引（默认唯一索引 UNIQUE+partial）/ 7 关键列；存量数据不变 `tenants=1`、`ai_llm_calls=57`），v1.8/v1.10 的"环境受限"项关闭；② 新增 `cmd/mig-verify -ro` 只读预检并实测 prod：发现上线前置阻塞 B1-B3——账本 checksum 漂移 6 条（`GetPendingMigrations` 硬失败 ⇒ 全部 pending 不执行）+ 两个 pre-cutoff 历史菜单文件（`20260628_add_connector_menu` `id` 二义 42702；`20260830_ticket_types_menu_reparenting` 与 prod 数据不匹配，实跑 21000，且其目标终态在 prod 已满足）；机制：prod 最早 007+ 记账 2026-09-18 晚于收养分界 2026-09-08 ⇒ 被判"新装"，两文件不获收养而按 pending 执行；③ 处置 runbook R1-R4（收敛 checksum → 收养两菜单文件 → `-ro` 复核 pending=2 → 发布）与附带发现（空库全量重放更早失败于 `20260501_rbac_endpoint_acls`；`cmd/migrate -down` 版本字典序选靶缺陷）见 §4.4；④ 测试工具：`cmd/mig-verify`（status/ro/up/down/only/entbaseline）、`.dev/mig_check.py`、`.dev/ledger_drift.py`、`.dev/mig_reconcile.py`、`.dev/mk_empty.py` |
| v1.12 | 2026-09-25 | 发布阻塞已解除（prod R1-R3 执行完成，流程=「终态核对 → 克隆库彩排 → 账本备份 → 单事务守卫写 → 独立复核」）：① R1 六条 checksum 守卫更新（`WHERE version=%s AND checksum=<旧值>`，逐条 `rowcount=1`）+ R2 收养 2 个 pre-cutoff 菜单文件（`checksum=''`、`release_version='adopted'`，描述 md5 与 `discovery.go` 解析一致）；② prod 复核：`ledger_drift` mismatch=0、`cmd/mig-verify -ro` → `applied=50 pending=2`（仅本方案两条）、`.dev/mig_audit_diff.py` 证明变更仅 6+2 行（**业务表零写入**）；③ 终态核对 8 版本 145 结构项 + 6 语义项通过，残留差异 2 项记录在案（`endpoint_acls` 可选触发器未落地；security 角色缺 3 条 read 授权——历史文件引用不存在的 `resource_type` 列，无法重放，交 owner 决策）；④ 工具：`.dev/mig_r1r2.py`（verify/dry-run/apply）、`.dev/mig_desc_check.py`、`.dev/mig_audit_diff.py`、`.dev/mig_redrift_clone.py`；备份/回滚/日志见 §4.4⑤；⑤ R4（发布）留待部署：prod `pending=2` 由新版本启动时 bootstrap 应用 |
| v1.13 | 2026-09-24 | B6 前端批次完成（FE-1..FE-4）：① FE-1 API 客户端 `lib/api/llm-provider-api.ts` + 共用探测 hook `use-llm-provider-feature.ts`（仅 `system:write` 探测、fail-closed、普通用户零新增请求）；② FE-2「系统配置 → LLM 模型」页签（CRUD/测试/设默认/启停/软删/导入、未实现协议置灰、422 可读提示、密钥只写不回显）；③ FE-3 权限门控与回归：`use-permissions.test.ts` 追加用例，`src/routes/index.tsx` 与 `docs/plan/_data/vite-route-map.csv` 零 diff；④ FE-4 会话页选择器 + 个人默认 + 生效实例标签 + 失效回退；`ai-api.ts` 仅在显式选择时携带 `provider`（未选择时请求体与现状逐字节一致），`done` 无 provider 字段时 `onDone` 单参数调用以保持既有回调兼容；⑤ 证据：`npx tsc --noEmit` exit 0、`npx eslint` 无输出、`npx jest`（ai-api / use-llm-provider-feature / use-permissions）3 套件全绿；⑥ 遗留：QA-2 端到端验收与 QA-3 服务端侧全链路回归待执行 |
| v1.14 | 2026-09-25 | QA-3 完成（B7 回归门禁之一）：开关关闭全链路 13/13 维度通过，报告归档 `docs/testing/multi-llm-provider-qa3-regression-2026-09-24.md`；新增 `router/llm_provider_routes_test.go` 两条运行时用例（handler=nil ⇒ `/api/v1/ai/providers` 404；注入 ⇒ 401 进入鉴权链），把「开关关闭无新路由」从注释约定升为测试断言；后端六组测试 + 前端三套件（94 用例）+ `tsc`/`eslint` + 路由零 diff 复核全绿。残留：QA-2 端到端验收（含浏览器级 UI 视觉复核）待执行 |
| v1.15 | 2026-09-24 | QA-2 端到端验收（自动化口径）完成（B7 进行中）：① 新增 API 级链路用例 `handlers/ai/llm_provider_qa2_flow_test.go`（内存 SQLite + `httptest` 桩，管理写 → 缓存失效 → 解析 → 真实出站单链；覆盖 §6.2 场景 1-6/10 + 删除/禁用可见失败路径）与验收记录 `docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md`（逐场景状态矩阵 + 人工遗留清单 §5）；② 修正 `TestLLMProtocolOptions` 槽位顺序断言为 §3.1.4 表行序（测试口径错误，非实现回归；该用例已并入定向集防漏跑）；③ 证据：`handlers/ai` + `router` + `internal/bootstrap` + `middleware` 四包全绿、service LLM 子集 ok、`internal/llm/protocol` ok（含 4 个等价性对照）、`gofmt -l` 无输出；④ 遗留（人工，需真实环境）：浏览器级 UI 复核 / 真实 Ollama·Anthropic 桩联调 / 运行期日志明文检索——B7 待其关闭 |
| v1.16 | 2026-09-24 | DOC-1 文档同步核验（B8 内容项就绪）：① 核验 `CHANGELOG.md [Unreleased]`、`docs/api-reference.md`（「AI 供应商管理（多 LLM Provider）」章节含 §3.4 全 10 端点）、本方案 §10（v1.2–v1.15）、`docs/plan/llm-protocol-adapter-plan.md` v0.7 启动门禁注记均已就位；② 纠偏计划内不存在的路径引用 `docs/api/API_REFERENCE.md` → 实际 `docs/api-reference.md`（DOC-1 / B8 / §6.3 DoD / 独立计划共 4 处）；③ 证据：`bash scripts/docs-gate/run-all.sh` → 5 gates / 0 failed；④ B8 正式收口待 B7 人工项关闭后执行 |
| v1.17 | 2026-09-25 | 验收证据索引与门禁复跑（B7/B8 收尾）：① 新增 **§9.4 验收证据索引**——QA-1 后端单测清单（registry / gateway / 协议槽位 / 协议适配器 / 启动矩阵 / 遥测聚合 / 管理面 / 会话覆盖 / 路由门禁）、QA-2 报告 + `TestLLMProviderQA2RuntimeSwitchFlow`、QA-3 报告 + `router/llm_provider_routes_test.go`，附三类复跑命令（后端定向集 / 协议包 / 前端 `tsc` + 3 套 jest）；② §6.3 DoD 引用同步由「§10 附录」改指「§9.4 验收证据索引」；③ 文档门禁在 §9.4 与 §10 v1.16 全部改动落盘后**复跑**：`bash scripts/docs-gate/run-all.sh`（Git Bash）exit 0 → `Docs Gates Summary: 5 total, 0 failed`（新增/改动文档未产生失效链接，C.3 advisory 清单均为既有历史链接），日志 `.dev/docs-gate-rerun.log`（`.dev/` 已 gitignore，本地留存）；④ 旁证（非本方案交付项）：`go test ./handlers/ai/ ./router/ ./internal/bootstrap/ ./middleware/ ./internal/llm/protocol/... -count=1` 全绿；全量 `./service/` 在**双作业并行抢 CPU** 下暴露 `TestBiz_MultipleInstancesIndependent` 偶发失败（`PI-<key>-<UnixNano>` 同刻撞唯一键，`service/bpmn_process_executor.go:81`，单独复跑通过）——属既有 BPMN 用例在高负载下的时钟分辨率 flake，与本方案无关，建议另行跟进 |

| v1.18 | 2026-09-25 | 独立计划解冻与 PA-1 交付：① `docs/plan/llm-protocol-adapter-plan.md` 由 Stub v0.7 升 **v1.0 正式方案**（§4 详细设计：契约与不变式 / 变体语义表 / PA-1 差异清单 6 条 / 参数默认值 / 能力位收敛；§5 PA-1..PA-6 任务拆解；§6 验收证据索引），主计划 §11.3 启动门禁「B5 出口」关闭；② **PA-1 `anthropic_messages` 适配器**（官方 + `minimax` 变体）落地：新增 `internal/llm/protocol/anthropic_messages.go` + `anthropic_messages_test.go`（7 组：请求体 / 工具与工具结果 / 鉴权头 / `ProcessResponse` / 非流式含带内错误 / 流式事件 / 推理模型与端点）；`registry.go` 注册两槽位并新增 `DefaultEndpoint`；`service/llm_registry.go` 补变体缺省参数、端点回退与构建分派泛化（注册表优先，anthropic 两变体改由适配器承载）；`service/llm_anthropic_equivalence_test.go` 对照旧 `MiniMaxProvider` 锁定等价（路径 / 鉴权头 / 请求体字段 / 返回文本），并覆盖静态配置开关开启与 DB 实例路径两条承载切换用例；③ 零破坏：开关默认关、无迁移、无 API 契约改动；④ 证据：`go test ./internal/llm/protocol/... -count=1` ok、service 定向集（含新增 `TestRegistryBuildProviderUsesAnthropicAdapterForMiniMax`）全绿、`go build ./...` + `go vet ./internal/llm/protocol/... ./service/` exit 0、`gofmt -l`（本轮改动文件）无输出、文档门禁复跑 5 gates / 0 failed（日志 `.dev/docs-gate-rerun.log`）；⑤ 与旧分支的有意差异 6 条（键序无影响 / 支持流式与工具属能力增益 / 非流式取全部 text 块且 tool_use-only 返回空串 / 超时改由 ctx 控制 / `model` 覆盖不写回实例 / temperature 默认按变体）已逐条登记于独立计划 §4.3 |

---

## 11. 协议适配层（独立计划，槽位预留）

> 本节是 v1.2 新增的**槽位说明**：协议适配层不在本方案实施，落地由独立计划 `docs/plan/llm-protocol-adapter-plan.md` 推进（参考实现 `E:\projects\ai-agent-runtime`）。

### 11.1 四种协议（API 形态）口径

| 协议（枚举值） | 形态要点 | 端点（参考实现） | 关键差异 |
|:---|:---|:---|:---|
| `openai_chat_completions` | OpenAI Chat Completions | `/v1/chat/completions`（`adapter/openai.go:1111`） | `messages[]` + `choices[].delta`；工具调用 `tools/tool_calls` |
| `openai_responses` | OpenAI Responses | `/v1/responses`（`adapter/codex.go:876`，参考实现以 `codex` 命名承载） | `input/output` 事件模型、`max_output_tokens`、内置工具（如 image_generation） |
| `anthropic_messages` | Anthropic Messages | `/v1/messages`（`adapter/anthropic.go:795`） | `x-api-key` + `anthropic-version`；`content[]` 块、`thinking`、事件驱动 SSE、`max_tokens` 必填 |
| `google_gemini` | Google Gemini | v1beta API（`adapter/gemini.go:591-595`，路径由调用侧拼装） | `contents/parts`、`candidates`；鉴权与路径形态独立 |

厂商差异（`azure` / `ollama` / `minimax` / 兼容网关）= `variant` + `adapter_options`，不新增协议枚举（参考实现同构：`providercompat.Context` 的 `Protocol` 与 `Profile` 双维度）。

### 11.2 本方案已预留的槽位（P0 只定义不实现）

| # | 槽位 | 位置 | P0 行为 |
|:---|:---|:---|:---|
| 1 | 协议枚举 4 值 | DB `protocol`、DTO、前端下拉 | 校验 + 映射（§3.1.4）；未实现协议 422 |
| 2 | `variant` + `adapter_options` | DB 列、DTO、表单 | 白名单 + 非敏感校验；存储/回显，不解释语义 |
| 3 | 协议层 `ProtocolAdapter` | `internal/llm/protocol/`：`adapter.go`（接口）+ `registry.go`（注册表） | **P0 实现 `openai_chat_completions` 适配器并接线（BE-9）**；其余协议不接线、不改变现有调用链 |
| 4 | 能力位 | `available` 响应 `supportsStream` / `supportsTools` / `supportsReasoning` / `implemented` | 按现有实现真实填写 |
| 5 | 错误码 | `AI_PROTOCOL_NOT_IMPLEMENTED`(422) | 未实现协议、非法 `variant` 拒绝 |

### 11.3 独立计划边界（交接清单）

- **输入**：本方案 §3.1.4 映射表、§11.2 槽位定义、参考实现坐标（§9.1）、BE-9 交付的协议层基线（接口/注册表/`openai_chat_completions` 适配器与对照测试）。
- **范围**：**剩余 3 协议**的统一请求/响应/流式/工具/推理链归一化（`anthropic_messages` 适配器化、`openai_responses` / `google_gemini` 从零接入）；`variant` 语义落地（azure `api_version`、Ollama 原生形态归一、Anthropic 兼容端点等）；把 P0 的"映射到既有分支"与旧实现分支逐步收敛到协议层（`NewProviderFromConfig` 在收敛前保持不变）。
- **零破坏约束**：不改本方案已定的 DB 结构（无需新迁移）、不改 §3.4 API 契约、不改前端字段与校验（仅把置灰选项打开）。
- **验收**：4 协议各自真实/桩服务连通 + 流式 + 工具调用矩阵全绿（`openai_chat_completions` 沿用 P0 基线与对照测试）；`protocol/variant` 组合零迁移升级。
- **启动门禁（已达成，2026-09-25 解冻）**：主计划 **B5 出口**各项已满足（BE-8 随 B2、BE-9/BE-5 随 B5 合入；B6/B7 回归见 §5 落地状态 ⑪-⑭），独立计划 `docs/plan/llm-protocol-adapter-plan.md` 已升 v1.0 并交付 PA-1；解冻后 `NewProviderFromConfig`、既有实现分支与 `internal/llm/protocol/` 的改动按该计划 PA 任务执行（避免与本方案冲突）。

---

> 评审检查点：(1) §2.2 决策表 D1-D13 是否全部认可（D12 权限口径已按"系统管理员"定稿；D3 按 4 种 API 形态、D13 按"P0 实现 `openai_chat_completions` 适配器 + 其余 3 协议槽位"定稿）；(2) §8 开放问题已按最佳实践确认关闭，如需变更须回写本节并升版；(3) §5.4 批次划分/出口门禁/排期假设（1 后端 + 1 前端 + QA 滚动）是否认可（BE-8 已排入 B2 并与 BE-2 并行；BE-9 首适配器已排入 B5 并与 BE-5 并行）；(4) §11 独立计划《LLM 协议适配层》的边界、交接清单与启动门禁（B5 出口）是否认可。

