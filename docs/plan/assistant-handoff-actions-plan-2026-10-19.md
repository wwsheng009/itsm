# AI 助手 handoff「转人工」三项实施方案（T5-04）

- **日期**：2026-10-19
- **状态**：T5-04a/T5-04b 均已实现（2026-10-19）
- **任务**：Dock 主方案 §14.2 **T5-04**；交互状态机 §3.2 L67
- **目标**：把“打开对应页面 / 预填表单 / 指派”从设计一行落为可执行、可审计、可回归的端到端能力
- **非目标**：不在本任务实现 IM/电话转接、不新增人工坐席排班、不做无用户确认的自动指派

---

## 1. 背景与现状

主方案 §3.2 定义 handoff 状态：

> AI 不适用/用户选择 → 转人工：打开对应页面/预填表单/指派。

当前代码状态：

- 已有 `handoff` 埋点及若干导航/预填钩子；
- 没有统一动作卡、动作契约、权限声明与失败语义；
- “预填”只存在于全局搜索/划词等入口，未与 handoff 统一；
- “指派”没有跨域适配层，需复用业务域 API 与确认/审计。

评审已将 handoff 列为“设计→任务悬空项”，见 `ai-assistant-dock-plan-review-2026-10-19.md` §3。

> 2026-10-19 实施进度：T5-04a 已交付（消息级入口/打开页面/预填表单）；T5-04b 指派待实施。证据
> `docs/plan/evidence/assistant-dock/T5-04a-handoff-open-prefill-evidence.md`。

> 2026-10-19 后续：T5-04b 已交付（active 候选人/二次确认/工单与事件 assign/失败可见）。证据
> `docs/plan/evidence/assistant-dock/T5-04b-handoff-assign-evidence.md`。

---

## 2. 设计原则

1. **用户显式触发**：AI 可以建议转人工，但不得自动跳转/自动预填/自动指派。
2. **只改草稿，不自动提交**：预填只写表单草稿；最终保存仍由用户点击。
3. **权限先于动作**：动作生成时即按权限裁剪；执行时后端再次校验。
4. **失败可见**：目标不存在、无权限、跨租户、指派冲突必须显示稳定原因码。
5. **可审计**：每个动作落 `handoff` 埋点与业务域审计；不采集对话正文。

---

## 3. 动作契约

建议前端统一结构（`lib/ai/handoff/types.ts`）：

```ts
type HandoffActionType = 'open' | 'prefill' | 'assign';

interface HandoffActionBase {
  type: HandoffActionType;
  labelKey: string;
  reasonCode?: string;
  disabledReason?: string;
}

interface HandoffOpenAction extends HandoffActionBase {
  type: 'open';
  targetType: string;
  targetId: number;
  route: string;
  permission?: { resource: string; action: string };
}

interface HandoffPrefillAction extends HandoffActionBase {
  type: 'prefill';
  route: string;
  targetType: string;
  targetId: number;
  payload: Record<string, unknown>;
  fields: string[]; // 白名单
  oneShot: true;
}

interface HandoffAssignAction extends HandoffActionBase {
  type: 'assign';
  targetType: string;
  targetId: number;
  permission: { resource: string; action: string };
  requiresConfirmation: true;
  candidates?: Array<{ id: number; name: string }>;
}
```

约束：

- `route` 必须来自 `ROUTE_PATHS` 或注册表解析，禁止模型自由生成 URL；
- `payload` 必须按 `fields` 白名单裁剪；
- `assign` 必须二次确认；
- 动作不得包含删除/下线/批量写语义。

---

## 4. 动作实现

### 4.1 打开对应页面

- 目标对象类型 → 路由适配器（`handoff-target-adapter.ts`）；
- 支持至少：工单、事件、问题、变更、服务请求、CMDB CI；
- 执行前检查：
  - 目标存在；
  - 当前租户；
  - 当前用户有读权限；
- 失败：显示“对象不存在或不可访问”，不跳空页。

### 4.2 预填表单

- 通过一次性 route state / store 传递：
  ```ts
  { source: 'assistant_handoff', handoffId, payload, fields }
  ```
- 页面消费规则：
  - 仅填充空白字段，不覆盖人工输入；
  - 只填草稿，不提交；
  - 消费后立即清除，防刷新重复预填；
  - 非法/过期 payload 丢弃并提示；
- 预填字段白名单由目标页面声明，不在 handoff 层猜测业务字段。

### 4.3 指派

- 通过域适配器调用现有指派 API（如工单 assignee 更新）；
- 执行流程：
  1. 权限检查 + 目标状态检查；
  2. 选择候选人（若未指定）；
  3. 二次确认；
  4. 调用域 API；
  5. 回执/失败原因；
- 必须处理：
  - 候选人不可用/已离职；
  - 目标已关闭/已分配；
  - 并发版本冲突；
  - 跨租户/无权限。
- 指派是写动作，必须进入现有确认/审计框架；**不得由 AI 自动执行**。

---

## 5. UI

新增 `HandoffActionCard`（Dock 与工作区共用）：

- 标题：转人工
- 原因说明：AI 为什么建议转人工
- 三个动作按钮/菜单：
  - 打开页面
  - 预填表单
  - 指派
- 无权限动作禁用 + tooltip；
- 指派动作打开候选人选择 + 确认；
- 结果回执：
  - 成功：目标链接 + 操作结果；
  - 失败：原因码 + 重试/改选；
- a11y：按钮可键盘操作，状态变化有 `role="status"` 提示。

---

## 6. 权限、审计与埋点

- 权限：
  - 打开/预填：目标域 `read`；
  - 指派：目标域 `write`（或既有 assign 权限）；
- 审计：
  - 复用业务域审计；
  - handoff 事件记录 `reasonCode`、`action`、`targetType/id`、`success`；
  - 不记录表单正文。
- 埋点：
  - `handoff` 已有事件扩展 `extra.action = open|prefill|assign`；
  - 增加 `handoff_failed`（或复用 error_shown + reason_code，需在事件字典二选一并冻结）。

---

## 7. 任务拆分

### T5-04a 打开页面 + 预填表单

- 动作契约与适配器；
- HandoffActionCard（open/prefill）；
- 至少两个域端到端：ticket、incident；
- 负向：无权限、目标不存在、预填不覆盖、不自动提交。

### T5-04b 指派

- 候选人选择与确认；
- ticket 指派端到端；
- 并发冲突/不可用候选人/目标状态失败；
- 权限与审计。

---

## 8. 验收标准

| 验收项 | 断言 |
|---|---|
| 打开页面 | 正确对象路由；无权限/不存在显式失败 |
| 预填表单 | 只填空白字段、不提交、一次性消费、刷新不重复 |
| 指派 | 二次确认、权限校验、成功回执、冲突/不可用失败可读 |
| 安全 | 跨租户/越权全部拒绝；动作不包含删除/下线 |
| 可观测 | 每个动作有埋点与审计；无正文泄露 |
| 测试 | 单元 + 组件 + 至少两个域端到端；tsc/eslint 全绿 |

---

## 9. 开放问题

1. `handoff_failed` 是否新增事件，还是复用 `error_shown + reason_code`？
2. 指派候选人来源：域 API 分页/搜索，还是复用已有用户选择器？
3. 预填跨页面刷新是否需要服务端一次性 token（当前建议前端 store + TTL）？
4. 哪些域首批接入：建议 ticket + incident，变更/服务请求后续。

---

## 10. 关联文档

- 主方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` §3.2 / §14.2
- 评审：`docs/plan/ai-assistant-dock-plan-review-2026-10-19.md` §3
- 交付台账：`docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
