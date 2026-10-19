# 存量失败测试治理方案（2026-10-19）

- **状态**：已执行 / 归档（本方案作为后续回归基线与防复发约定）
- **关联提交**：`4938945a836b098811821fd9a69e605fa3dcf8d9`
- **提交标题**：`fix(tests): 修复存量失败测试（5 套件 / 15 用例）`
- **执行日期**：2026-10-19
- **范围**：前端 Jest 全量回归、6 个测试/调用点、1 份证据文档
- **结论**：6 套件 / 19 用例修复；全量 `359/359` suites 通过（`4735 passed / 13 skipped / 0 failed`）

## 1. 背景与目标

T5-13 文案 i18n 批次验收时，局部回归（40/41 suites）仅剩 `TicketDetailAssignSearch` 4 例失败，已登记为存量失败。按“先修复再进行下一步评估”的要求，先修该套件，再跑全量回归；全量扫描又暴露 5 套件 / 15 例同源腐烂。本方案目标：

1. 清零存量失败，恢复全量绿色基线；
2. 记录每类失败的根因与修法，避免“只改断言、不修契约”；
3. 建立防复发治理（mock 契约、权限门控、API 扫描器、CI 全量门禁）；
4. 为后续 T5 任务评估提供可信的回归基线。

## 2. 基线与结果

| 阶段 | 套件 | 用例 | 说明 |
|---|---:|---:|---|
| 初始已知 | 1 | 4 | `TicketDetailAssignSearch`（T5-13 期间登记） |
| 全量扫描新增 | 5 | 15 | MSP 门控 3 套件、`tickets/types`、`api-contract` |
| 合计修复 | 6 | 19 | 同一提交 `4938945a` |
| 最终全量 | 359 | 4735 passed / 13 skipped / 0 failed | 359/359 suites 通过 |

> 口径说明：提交标题中的“5 套件 / 15 用例”指全量扫描**新增发现**的部分；提交内容同时包含先行修复的 `TicketDetailAssignSearch`（4 例），因此实际交付口径为 **6 套件 / 19 用例**。本文按实际内容记录。

## 3. 失败分类与根因

| # | 套件 | 用例 | 根因类别 | 根因 |
|---|---|---:|---|---|
| 1 | `components/ticket/__tests__/TicketDetailAssignSearch.test.tsx` | 4 | mock 契约漂移（非 hook 读取） | auth-store mock 只有 hook 形态，缺 `getState`；`use-permissions.hasPermission` 走 `useAuthStore.getState()` |
| 2 | `components/auth/__tests__/MspProviderGate.test.tsx` | 2 | 门禁契约漂移 | `canUseMspWorkbench` 自 2026-10-18 起要求 `userId` + `mspRole`，mock 未跟进 |
| 3 | `pages/(main)/msp/dashboard/__tests__/index.test.tsx` | 10 | 门禁契约漂移 | 同上 |
| 4 | `pages/(main)/dashboard/components/__tests__/CrossTenantOverview.test.tsx` | 1 | 门禁契约漂移 | 同上 |
| 5 | `pages/(main)/tickets/types/__tests__/index.test.tsx` | 1 | 权限门控变更 | 组件新增 `ticket_type:manage` 门控「新建类型」；测试默认全 false 却断言按钮存在 |
| 6 | `lib/__tests__/api-contract.test.ts` | 1 | 扫描器形态盲区 | `aiGetPageHelp` 使用 `page${qs}`（`qs` 自带 `?`），静态扫描无法还原路径；后端路由实际存在 |

**共性**：6 类失败均不是产品代码回归，而是**测试/扫描器未随契约演进**；其中 2–4 是同一门禁契约变更的连带影响，应作为一次契约升级统一处理。

## 4. 修复方案

### 4.1 `TicketDetailAssignSearch`：补齐非 hook 读取路径

按 `lib/hooks/__tests__/use-permissions.test.ts` 的既有约定，mock 同时提供 hook 调用与 `getState`：

```ts
jest.mock('@/lib/store/auth-store', () => {
  const state = {
    user: { id: 1, username: 'admin', name: '系统管理员' },
    hasPermission: () => true,
  };
  return {
    useAuthStore: Object.assign(() => state, { getState: () => state }),
  };
});
```

**要点**：不能只补一个 `getState` 空实现；返回的 state 必须包含被测组件实际读取的字段（`user`、`hasPermission` 等），否则会从“缺方法”变成“缺字段”。

### 4.2 MSP 门控三套件：升级为全契约 mock

`canUseMspWorkbench` 的判定链为：`userId` + 服务商租户类型 + 服务商角色（`msp_role`）+ `msp_ticket:read` 操作码。三个套件的 `setAuth` 统一升级：

```ts
function setAuth(
  permissions: string[],
  tenantType = 'msp_provider',
  mspRole: string | null = 'provider_admin'
) {
  (useAuthStore as unknown as jest.Mock).mockImplementation(selector =>
    selector({
      user: { id: 1, permissions, mspRole },
      currentTenant: tenantType ? { type: tenantType } : null,
      hasPermission: (permission: string) =>
        permissions.includes(permission) || permissions.includes('*'),
    })
  );
}
```

- 默认 `mspRole='provider_admin'`，保留原有正向用例语义；
- `MspProviderGate` 新增负例：服务商租户 + 操作码但 `mspRole=null` → 不渲染，锁定 2026-10-18 的门禁修复；
- 调用点参数不变，避免测试意图被改写。

### 4.3 `tickets/types`：按新增门控重写场景

组件已用 `hasPermission('ticket_type', 'manage')` 门控「新建类型」。首例改为“持 `manage`、不持 `install_preset`”：

```ts
mockHasPermission = (...args) =>
  args[0] === 'ticket_type' && args[1] === 'manage';
```

这样既保留“无 `install_preset` 不渲染「从预设安装」”的原断言，又满足「新建类型」的权限前提；不再用“默认全 false”掩盖门控。

### 4.4 `api-contract`：统一 `?${qs}` 查询串形态

`aiGetPageHelp` 由 `page${qs}` 改为：

```ts
const qs = `pageKey=${encodeURIComponent(pageKey)}`;
return httpClient.get<HelpPageResult>(`/api/v1/ai/help/page?${qs}`);
```

**运行期 URL 完全不变**；改动只是让静态扫描器能按字面 `?` 剥离查询串。该形态与 `ai-api.ts` 其他调用一致，并已在代码注释中记录约束。

### 4.5 证据回写

在 `docs/plan/evidence/assistant-dock/T5-13-i18n-copy-batch-evidence.md` 追加 `## 5. 补记：存量失败测试治理（2026-10-19）`，记录 6 处修复、扫描器形态说明与全量验证结论。

## 5. 验证与验收

| 检查 | 命令/方式 | 结果 |
|---|---|---|
| 单文件 | `npx jest src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx` | 4/4 通过 |
| 5 个新增失败套件 | `npx jest --runTestsByPath ...` | 35/35 通过 |
| 全量回归 | `npx jest --no-coverage --maxWorkers=4` | 359/359 suites；4735 passed / 13 skipped / 0 failed |
| ESLint | `npx eslint <6 个改动文件>` | exit 0 |
| 类型检查 | `npm run type-check` | exit 0 |
| 提交卫生 | `git show --check`、`git diff --cached --check` | 干净 |

**验收结论**：存量失败清零；无新增失败；运行期行为未改变（仅测试契约与一处等价 URL 写法）。

## 6. 防复发治理

### 6.1 全量回归门禁

- 合并/发布前至少执行一次全量 Jest，不只看改动文件；本次 5 个套件正是“局部验证盲区”。
- 建议使用现有 `npm run test:ci`（或等价的 `npx jest --no-coverage --maxWorkers=4`）作为 CI 必过项。
- 失败必须先归因（真回归 / 存量腐烂 / 环境），再决定修复或登记，不允许静默跳过。

### 6.2 auth-store mock 统一工厂

- 所有 mock `@/lib/store/auth-store` 的测试统一使用 `Object.assign(() => state, { getState: () => state })` 形态。
- 同一 state 同时服务 hook 与非 hook 读取，避免两套数据源漂移。
- 新字段（`mspRole`、`currentTenant` 等）只改工厂默认值，不在各测试重复拼装。

### 6.3 权限门控双态覆盖

- 组件新增 `hasPermission(resource, action)` 门控时，同一 PR 必须补“有权限 / 无权限”两态断言。
- 修改门禁函数（如 `canUseMspWorkbench`）时，用 `rg "canUseMspWorkbench"` 找出全部测试与调用点，统一升级。
- 对 fail-closed 门禁，负例至少覆盖“缺角色 / 缺租户类型 / 缺操作码”中的关键维度。

### 6.4 API 契约扫描器约定

- 前端查询串统一写成 `` `path?${qs}` ``，禁止 `` `path${qs}` ``（`qs` 自带 `?`）。
- 禁止嵌套模板字面量；确需动态路径时，扩展扫描器并补测试，而不是只改测试期望。
- 新增/调整后端路由时，同步跑 `api-contract`，确认前端调用点可被静态还原。

### 6.5 契约变更联动清单

后端或共享层变更以下契约时，必须同步搜索前端 mock/测试：

- 认证 store 形状（`user`、`mspRole`、`currentTenant`、`hasPermission`）；
- 门禁函数与权限码（`canUseMspWorkbench`、`ticket_type:manage` 等）；
- API 路径与查询串形态；
- 事件/埋点字段（`model`、`query_hash`、`citation` 等，后续 T5-06 相关）。

### 6.6 证据与台账

- 存量失败修复必须写入对应证据文档，附提交哈希与验证数字；
- 跨任务/跨批次的腐烂在证据文档中单列“补记”，不混入原任务验收；
- 交付台账保持为任务状态唯一入口，方案 §7 的进度块仅作历史增量。

## 7. 风险与边界

1. **未覆盖真实环境**：本次仅前端单测与静态检查；真实 Provider、真实 PG、全栈 E2E 仍属方案 §14.3 条件项。
2. **扫描器仍有盲区**：本次只消除 `page${qs}` 一种形态；其他动态拼接路径仍可能误报/漏报，需持续补充约定与测试。
3. **mock 仍可能漂移**：全契约 mock 降低了概率，但没有类型级约束；后续可考虑共享测试工厂或从真实 store 派生默认 state。
4. **提交口径差异**：提交标题为“5 套件 / 15 用例”，实际内容含先行修复的 1 套件 / 4 用例；本文按 6/19 记录，避免后续统计误读。
5. **工作树排除项**：`.agents/skills/**`、`scripts/*`、`.zcodeignore` 等无关改动未纳入本次提交，后续也不应顺手 `git add -A`。

## 8. 交付记录

| 项 | 内容 |
|---|---|
| 提交 | `4938945a836b098811821fd9a69e605fa3dcf8d9` |
| 标题 | `fix(tests): 修复存量失败测试（5 套件 / 15 用例）` |
| 时间 | 2026-10-19 17:00:34 +0800 |
| 规模 | 7 files changed, +94 / −29 |
| 代码文件 | `TicketDetailAssignSearch.test.tsx`、`MspProviderGate.test.tsx`、`msp/dashboard/__tests__/index.test.tsx`、`CrossTenantOverview.test.tsx`、`tickets/types/__tests__/index.test.tsx`、`lib/api/ai-api.ts` |
| 文档 | `docs/plan/evidence/assistant-dock/T5-13-i18n-copy-batch-evidence.md`（§5 补记） |
| 分支 | `feat/msp-p0-hardening`（提交后领先 origin 17） |

## 9. 关联文档

- 方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`（§14.2 补任务、§14.3 条件项、§14.4 待决策）
- 交付台账：`docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`（§T5 状态）
- 本批证据：`docs/plan/evidence/assistant-dock/T5-13-i18n-copy-batch-evidence.md`（§5 补记）
- 相关存量失败登记点（均在 `docs/plan/evidence/assistant-dock/`）：
  - `V0-01-ticket-inline-trio-evidence.md` §4
  - `I0-03-shortcuts-and-command-palette-evidence.md` §3
  - `I2-01-autopilot-supervision-evidence.md` §3
  - `V1-01-batch-ai-evidence.md` §4

---

## 附录 A：剩余任务紧要性评估（2026-10-19）

> 评估对象：方案 §14.2 剩余任务（T5-03 ~ T5-07、T5-09）与 §14.3 条件项。
>
> 评估口径：以“是否阻塞放量/验收、是否影响用户兜底、是否有明确前置依赖、是否已具备决策输入”为依据；**不替产品/运营拍板口径**，需拍板项单列。

### A.1 结论速览

| 优先级 | 任务 | 结论 | 关键依据 |
|---|---|---|---|
| P0 先做 | **T5-06 质量层数据源** | 紧要，建议第一顺位 | §14.4-3：T5-06 前质量层三指标只能标“暂不可测”，放量门槛不可执行；T5-07 依赖其数据链路 |
| P0/P1 放量前 | **T5-05 灰度开关** | 放量前必须；若近期无放量可紧随 T5-06 | 全仓未发现 `assistant.dock.enabled` / `help.agent.enabled` 读写点；缺开关无法安全放量 |
| P1 先做可做部分 | **T5-04 handoff** | 拆两段：打开页面+预填可先做；“指派”待产品口径 | 现有多处 `handoff` 埋点与 handoff 状态条，但无打开/预填/指派动作；`pendingPrompt` 预填通道已有 |
| P2 可后作 | **T5-03 附件/截图** | 可后作 | 通用附件域存在，但无 AI 聊天上传/解析链路；需上传面、视觉/OCR、留存与权限口径 |
| P2 可后作 | **T5-07 成本口径** | 排在 T5-06 之后 | `ai_llm_calls` 有 model/tokens/总延迟；无首 token 延迟列、无 P50；Q5 模型分级/成本上限未决策 |
| P2 可后作 | **T5-09 帮助写面** | 可后作，先拆分 | `tool_registry_help.go` 注释明确写面属后续批次；diff 预览 UI、help-index 向量化均未落地 |

### A.2 逐项评估

#### T5-06 质量层数据源（P0）

- **目标**：citation 埋点（sources 数 / 无引用回答）、回答级点踩、`model` 上报、`query_hash` 补链；验收“指标可算 + 看板接线”。
- **现状证据**：
  - `ai_feedbacks` / `SaveFeedback` / `doc_feedback` 已有基础；`ai_llm_calls` 已有 `provider/model/tokens/latency_ms`。
  - `query_hash` 仅出现在 `middleware/cache_middleware.go`，未进入 AI 遥测链路。
  - 未在助手消息区发现回答级点踩接线；现有 `doc_feedback` 是帮助文档级反馈，不是回答级。
- **紧要理由**：§14.4-3 明确“T5-06 落地前质量层三指标应标暂不可测”；不放量可以等，但要执行放量门槛就必须先有数据。
- **建议拆分**：T5-06a 事件契约 + 前端埋点 + 后端字段/落库；T5-06b 聚合口径 + 看板接线。先做 a，b 可与 T5-07 合并评审。
- **需拍板**：三指标精确定义（引用覆盖率分母、无引用回答判定、点踩口径）与数据保留周期。

#### T5-05 灰度开关（P0/P1）

- **目标**：`assistant.dock.enabled` / `help.agent.enabled`，平台配置 + 免重启生效。
- **现状证据**：前端 `src` 与后端全仓搜索两个键名均为 0 命中；未发现现成开关。
- **紧要理由**：没有开关就只能“全量开/全量关”，无法按租户/环境灰度，也无法在异常时快速回退；属于放量安全前置。
- **建议**：若近期有放量计划，与 T5-06 并行；否则排在 T5-06 之后。先确认是否复用现有平台配置中心，再定开关粒度（全局/租户/用户）与默认值。
- **需拍板**：开关粒度、默认值、回退策略、配置变更审计。

#### T5-04 handoff 转人工（P1）

- **目标**：打开页面 / 预填 / 指派三项，端到端用例。
- **现状证据**：`trackAssistantEvent('handoff', ...)` 多处存在；`AssistantInteractionBanner` 有 handoff 状态与“已转人工处理”文案；`assistant-store.pendingPrompt` 已有预填通道（I0-03）。但未发现“打开业务页面 + 预填 + 指派”的完整动作链。
- **紧要理由**：转人工是助手失败时的用户兜底，优先级高于“锦上添花”的附件/成本；但“指派”语义未定，不能整体开工。
- **建议拆分**：T5-04a 打开页面 + 预填（可立即做，复用 `pendingPrompt` 与路由能力）；T5-04b 指派（待产品口径）。
- **需拍板**：指派目标（当前处理人/队列/技能组/轮询）、是否创建工单、权限与通知。

#### T5-03 附件/截图输入（P2）

- **目标**：上传面 + 双界面接线；语音输入可显式关闭。
- **现状证据**：后端有通用附件域（`/attachments`，`attachment:write`）及各宿主域内附件别名；未发现 AI 聊天专用上传/解析端点。
- **可后作理由**：文本助手不依赖附件即可用；附件涉及存储、文件类型/大小、视觉/OCR、模型能力、留存与权限，跨栈且需选型。
- **建议**：先确认“复用通用附件域还是新建 AI 附件域”，再评估模型侧是否能读图/读文件；在此之前不排期。

#### T5-07 成本口径（P2，排在 T5-06 后）

- **目标**：首 token P95 / 整体 P95 区分、补 P50；模型分级路由与成本上限（Q5）。
- **现状证据**：`ai_llm_calls` 存 `model/tokens/latency_ms/success`；现有 `ai_telemetry` 聚合以平均延迟为主；`ai_pilot_report` 的 P95 是任务级（plan/create）延迟，不是首 token/整体回答延迟；未发现 P50。
- **可后作理由**：需要先有 T5-06 的回答级数据链路，且“首 token”定义、成本口径、Q5 路由/上限均需评审；当前不影响功能可用性。
- **需拍板**：首 token 计时起点、整体 P95 样本范围、P50 是否必需、模型分级与成本上限策略。

#### T5-09 帮助写面收口（P2）

- **目标**：`help_update_draft` / `help_submit_review` / `help_publish` + diff 预览/一键审阅 UI；help-index 向量化。
- **现状证据**：`service/tool_registry_help.go:18` 注释“写面（help_submit_review/help_publish）属后续批次”；未发现写面工具实现；diff 预览 UI 与向量化未见落地。
- **可后作理由**：当前助手以只读帮助为主，写面涉及审批、权限、审计与内容安全；向量化还依赖 embedding 基础设施与索引迁移，范围最大。
- **建议**：先拆为“工具契约与权限”“diff/审阅 UI”“help-index 向量化”三个子任务，分别估时后再排期。

### A.3 §14.3 条件项

| 条件项 | 紧要性 | 说明 |
|---|---|---|
| 真实 Provider / 真库 E2E、移动端入口 | 联调后 | 依赖环境/端侧排期，当前不可执行 |
| O0-03 真实 PG 删除语义回归 + 全栈 E2E | 联调后必做 | 涉及 O 系列验收口径，不能以 mock 替代 |
| P2 MSP 真实 PG 冒烟 | 联调后 | 与 P2 验收绑定 |
| Q1“核心 40 页”量化目标 | 可后作 | 产品口径，复核保号或降级 |
| Q2 助手侧客户上下文联动 | 可后作 | 增强项，与 MSP 场景一并评估 |
| Q4 平台侧季度覆盖率 / stale 复核 | 可后作 | 运营制度化后关闭 |

### A.4 建议顺序

1. **T5-06a**（数据契约 + 前端埋点）与 **T5-05**（若有放量计划）并行；
2. **T5-06b**（聚合/看板）+ **T5-07**（口径评审后接入）；
3. **T5-04a**（打开页面 + 预填）；同步拍板 T5-04b 指派语义；
4. **T5-03 / T5-09** 在选型与范围确认后启动；
5. §14.3 条件项随联调环境/运营制度就绪推进。

### A.5 需拍板清单（不替用户决定）

- T5-04：指派目标与是否建单；
- T5-05：开关粒度、默认值、回退策略；
- T5-06：三指标定义与数据保留；
- T5-07：首 token/整体 P95/P50 口径、Q5 模型分级与成本上限；
- T5-03：上传面选型、视觉/OCR 能力、留存与权限；
- T5-09：写面审批/审计流程、向量化基础设施与范围。

> 本附录为评估建议，不改变方案 §14.2/§14.3/§14.4 的既有状态；任务启动前仍需按 §14.5 补 ID、验收与证据路径。
