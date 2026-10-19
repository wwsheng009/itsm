# AI 助手模型分级路由与成本上限实施方案（T5-07b / Q5）

- **日期**：2026-10-19
- **状态**：调用观测已交付；预算/金额与模型分级路由延后（用户口径：由上游管理）
- **任务**：Dock 主方案 §14.2 **T5-07b**；开放问题 §12 Q5
- **前置**：T5-07a 已完成首 token / 整体 P50+P95 口径修正
- **目标**：按任务复杂度选择模型档位，并提供租户预算/成本上限与可见降级状态
- **非目标**：不做自动采购/计费结算；不做跨 Provider 价格谈判；不改变现有 Provider 解析优先级

> **2026-10-19 口径调整**：用户明确“先不作预算之类的管理，由上游管理；先作调用报表与观测性，
> 比如观测 LLM 调用的历史、是否错误”。本轮因此只交付 **调用账本 + 历史/汇总 API + 观测看板**，
> 不做预算闸门、金额/计费、自动降级或模型分级路由。相关证据：
> `docs/plan/evidence/assistant-dock/T5-07b-llm-call-observability-evidence.md`。

---

## 1. 背景与现状

主方案 §3.7 L119：

> 成本：模型分级路由（任务卡/轻问答用轻模型，复杂任务用强模型）；预算与限流状态在面板可见。

主方案 §12 Q5：

> 模型分级路由策略与成本上限：任务卡/轻问答 → 轻模型；复杂任务 → 强模型；租户预算闸门。

当前能力：

- 已有多 Provider 解析与显式 `provider` 覆盖（BE-7）；
- 已有 Bot 每 run 预算护栏：`bot.budget.max_steps / max_tokens / max_tool_calls / tool_timeout_seconds / max_output_bytes`；
- 已有首 token / 整体 P50/P95 与模型分布观测（T5-06b/T5-07a）；
- **没有**模型档位定义、按任务类型路由、租户级成本预算或超限策略。

---

## 2. 必须先拍板的 Q5 口径

以下 5 项未定前不建议写代码：

| # | 决策 | 建议默认 | 影响 |
|---|---|---|---|
| Q5-1 | 档位划分 | `light / standard / reasoning` 三档 | 路由与配置结构 |
| Q5-2 | 路由输入 | 任务类型 + 工具调用 + 上下文长度 + 显式覆盖 | 路由函数 |
| Q5-3 | 预算单位 | 先 token，后金额 | 成本表与闸门 |
| Q5-4 | 超限行为 | `reject`（稳定错误码）为默认，`degrade` 可配 | 用户体验与安全 |
| Q5-5 | 配置层级 | 平台默认 + 租户覆盖 | system_configs/新表 |

> 若 Q5-3 选择金额，需补模型价格表与币种；若暂不补价格，成本上限只能按 token 计，不得把 token 当金额展示。

---

## 3. 档位与路由

### 3.1 档位定义

```ts
type ModelTier = 'light' | 'standard' | 'reasoning';

interface ModelTierPolicy {
  tier: ModelTier;
  providerKey?: string;       // 绑定实例；空 = 走默认解析
  model?: string;             // 可选模型名
  maxContextTokens: number;
  supportsVision: boolean;
  supportsTools: boolean;
}
```

约束：

- 档位只是**候选策略**，不绕过 Provider 可用性/RBAC/租户授权；
- 未配置档位 = 完全保持现状，不静默换模型；
- 显式 `provider` 覆盖优先级最高；
- 模型不可用时按配置 `fallbackTier` 降级，降级必须记录原因并可见。

### 3.2 路由输入

```ts
interface RouteInput {
  taskType?: 'chat' | 'task_card' | 'help' | 'batch' | 'async';
  requiresTools?: boolean;
  requiresVision?: boolean;
  estimatedContextTokens?: number;
  explicitTier?: ModelTier;
  explicitProvider?: string;
  tenantId: number;
}
```

建议规则（可配置）：

1. 显式 tier/provider；
2. 需要视觉 → 支持视觉的档位；
3. 需要工具/长上下文/复杂任务 → `reasoning`；
4. 任务卡/轻问答 → `light`；
5. 其他 → `standard`。

### 3.3 路由结果

```ts
interface RouteDecision {
  tier: ModelTier;
  providerKey: string;
  model?: string;
  reasons: string[];
  fallbackFrom?: ModelTier;
}
```

- 路由失败：稳定错误码 `model_tier_unavailable`；
- 降级：`fallbackFrom` 必填并落审计；
- 不返回密钥/内部 URL。

---

## 4. 预算与成本上限

### 4.1 预算维度

建议按以下顺序：

1. **单请求 token 上限**：输入 + 输出合计；
2. **单任务 token 上限**：任务卡/异步任务整体；
3. **租户日/月 token 预算**；
4. **租户日/月金额预算**（仅在价格表落地后）；
5. **限流**：每分钟请求数/并发数。

现有 `bot.budget` 是每 run 的 step/token/工具上限，不能直接替代租户成本预算。

### 4.2 闸门位置

- 请求前：
  - 解析档位；
  - 检查租户预算/限流；
  - 超限 → 稳定错误码，不发 Provider 请求。
- 运行中：
  - token 累计；
  - 超单任务上限 → 中止并收口；
  - 已发生成本必须记录。
- 请求后：
  - 写 usage/cost；
  - 更新预算余额；
  - 指标聚合。

### 4.3 错误与降级

| 原因码 | 用户文案 | 动作 |
|---|---|---|
| `budget_exceeded` | 本月/本日 AI 预算已用完 | 联系管理员 / 稍后重试 |
| `rate_limited` | 请求过于频繁 | 重试 |
| `model_tier_unavailable` | 当前模型档位不可用 | 切换档位/重试 |
| `model_fallback` | 已降级到可用模型 | 显示降级说明 |

- 默认不把预算超限伪装成普通错误；
- 降级不得发生在已声明“强模型”的高风险任务上，除非配置显式允许。

---

## 5. 配置与数据

### 5.1 配置建议

`system_configs` 当前是 key/value 字符串，布尔能力开关已由 `capability` 管理；模型路由与预算适合**结构化配置**：

- 方案 A：`system_configs` 存 JSON（实现快，但校验弱）；
- 方案 B：新增 `ai_model_policies` / `ai_budget_policies` 表（推荐，便于租户/版本/审计）。

建议：
- 档位映射与预算策略用新表或强类型 JSON；
- 复用现有配置审计与租户隔离；
- 读路径 fail-safe：配置缺失 = 现状默认。

### 5.2 成本记录

需要新增/扩展 usage 记录：

- 请求 id、tenant、user、conversation、run；
- tier、provider、model；
- input/output tokens；
- 单价/成本（价格表存在时）；
- 是否降级、原因；
- 预算窗口快照。

现有 `ai_telemetry_events` 的 `model` 字段可做分布，但不能替代账本级成本记录。

---

## 6. 看板与告警

在 `/ai/observability` 增加：

- 档位分布（light/standard/reasoning）；
- 模型分布（已有，补充档位维度）；
- token/成本按任务/租户；
- 预算使用率、超限率、降级率；
- P50/P95 首 token 与整体时延（T5-07a 已有）。

告警建议：

- 租户预算 ≥80% 预警；
- 超限率异常；
- 降级率异常；
- 单请求 token 超阈值。

---

## 7. 任务拆分

### T5-07b-1 口径与配置（前置）

- Q5 五项决策记录；
- 档位/预算表结构或 JSON schema；
- 平台默认 + 租户覆盖；
- 管理台只读展示 + 写入权限。

### T5-07b-2 路由

- `RouteInput → RouteDecision` 纯函数；
- Provider 可用性/能力过滤；
- 显式覆盖与 fallback；
- 路由原因/审计；
- 单测表驱动。

### T5-07b-3 预算闸门

- 请求前检查；
- 运行中 token 累计/中止；
- 预算余额与限流；
- 错误码与前端提示；
- 并发/幂等边界。

### T5-07b-4 看板与告警

- 档位/成本/预算指标；
- 告警规则；
- 运营报表。

---

## 8. 验收标准

| 验收项 | 断言 |
|---|---|
| 默认兼容 | 无档位/预算配置时请求体与路由结果保持现状 |
| 路由 | 表驱动：轻问答→light、复杂/工具→reasoning、显式覆盖优先 |
| 能力过滤 | 不支持视觉/工具的档位不得被选中 |
| 预算 | 请求前超限不调用 Provider；运行中超限中止并记录已发生成本 |
| 降级 | 有原因、有审计、有用户可见说明；高风险任务默认不降级 |
| 权限 | 非授权用户不能改档位/预算；不能跨租户 |
| 可观测 | 档位/模型/token/成本/超限/降级可聚合 |
| 测试 | 单测 + 集成 + 看板测试；tsc/eslint 全绿 |

---

## 9. 风险与开放问题

1. **价格表缺失**：金额预算暂不可执行，先 token 预算。
2. **模型能力不统一**：视觉/工具/上下文需显式能力元数据，不能靠模型名猜。
3. **降级与高风险任务冲突**：写工具/审批任务默认禁止静默降级。
4. **多 Provider 与租户授权**：档位候选必须经过现有 Provider 解析与租户授权。
5. **成本归属**：重试/流中断/取消如何计费需与 Provider usage 对齐。
6. **看板存储**：`ai_telemetry_events` 是否新增成本字段，还是独立账单表，需在 T5-07b-1 决定。

---

## 10. 关联文档

- 主方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` §3.7 / §12 Q5 / §13.3 / §14.2
- 多 Provider：`docs/plan/multi-llm-provider-plan.md`
- 协议适配：`docs/plan/llm-protocol-adapter-plan.md`
- Bot 预算护栏：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md` BP8 / B1-02
- T5-07a 证据：`docs/plan/evidence/assistant-dock/T5-07a-cost-latency-evidence.md`
- 交付台账：`docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
