# T5-04b handoff 指派证据

- **状态**：已完成（2026-10-19）
- **任务**：`docs/plan/assistant-handoff-actions-plan-2026-10-19.md` 的 **T5-04b**
- **范围**：handoff 动作卡内的工单/事件指派；候选人加载、二次确认、权限、失败可见
- **不含**：批量指派、自动指派、IM/电话转接
- **提交**：随本文件同一提交（`feat(ai): T5-04b handoff 指派`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| 指派入口 | ✅ 当前对象为 ticket/incident 时动作卡显示「指派处理人」 | `HandoffActionCard.tsx` |
| 候选人 | ✅ 加载 active 用户，展示姓名 + 部门，支持搜索 | `UserApi.getUsers`；动作卡测试 |
| 二次确认 | ✅ 选择候选人后点击「确认指派」才调用域 API | 动作卡测试 |
| 工单指派 | ✅ `POST /tickets/:id/assign`，参数 `{assigneeId}` | `TicketApi.assignTicket` 断言 |
| 事件指派 | ✅ `IncidentAPI.assignIncident` | 代码接线 |
| 权限 | ✅ 工单 `ticket:assign`；事件 `incident:write` | 权限裁剪测试 |
| 失败可见 | ✅ 保留动作卡 + 展示后端原因；不静默失败 | 失败测试 |
| 审计/埋点 | ✅ `handoff` 事件带 action/targetType/targetId/assigneeId/success | 成功/失败断言 |
| 不自动执行 | ✅ 只有用户点击确认后才调用 API | 交互实现 |

---

## 2. 实现清单

| 文件 | 改动 |
|---|---|
| `src/components/ai/handoff/HandoffActionCard.tsx` | 新增指派模式、候选人 Select、确认/返回、错误 Alert、成功/失败埋点 |
| `src/lib/i18n/translations-assistant.ts` | `assistant.handoff.assign*` zh/en 文案 |
| `src/components/ai/handoff/__tests__/HandoffActionCard.test.tsx` | 指派成功/失败/权限裁剪测试 |

复用：

- `TicketApi.assignTicket(id, { assigneeId })` → `POST /api/v1/tickets/:id/assign`；
- `IncidentAPI.assignIncident(id, assigneeId)` → 事件域专用 assign 端点；
- `UserApi.getUsers({ page: 1, pageSize: 200, status: 'active' })`。

---

## 3. 行为口径

| 场景 | 行为 |
|---|---|
| 当前页 ticket + `ticket:assign` | 显示指派动作；候选人为 active 用户 |
| 当前页 incident + `incident:write` | 显示指派动作 |
| 无指派权限 | 动作不渲染 |
| 候选人加载失败 | 动作卡内错误提示；可返回重试 |
| 指派成功 | 提示成功、关闭动作卡、记录 `handoff(success=true)` |
| 指派失败 | 保留动作卡、显示后端原因、记录 `handoff(success=false)` |
| 目标已关闭/并发冲突/候选人不可用 | 使用域 API 返回语义，原样可见 |
| 默认 | 不自动指派；必须点击「确认指派」 |

---

## 4. 验证

### 4.1 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm test -- --runTestsByPath src/lib/ai/handoff/__tests__/prefill.test.ts src/components/ai/handoff/__tests__/HandoffActionCard.test.tsx src/components/ai/chat-core/__tests__/ChatMessageItem.test.tsx src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx src/components/ai/__tests__/AIChat.diagnostics.test.tsx --coverage=false --runInBand --silent` | ✅ **5 suites / 31 tests passed** |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

关键断言：

- 指派动作加载候选人并调用 `assignTicket(42, { assigneeId: 7 })`。
- 成功后 `handoff` 埋点含 `action=assign`、`success=true`。
- 失败后动作卡保留，显示「目标工单已关闭」，埋点 `success=false`。
- 无 `ticket:assign` 时不渲染指派动作。

---

## 5. 局限与后续

1. **候选人上限**：首屏加载 200 个 active 用户；大数据量租户需服务端搜索分页。
2. **事件指派未单独测试**：代码已接 `IncidentAPI.assignIncident`，建议联调补一条事件域用例。
3. **未跑真实浏览器/PG E2E**：冲突/状态错误依赖后端返回文案。
4. **批量/自动指派不在本任务**：仍遵守 handoff 用户显式触发原则。
5. **审计**：埋点记录动作元数据；域 API 自身审计沿用既有 ticket/incident 链路。

---

## 6. 变更文件

**修改**

- `itsm-frontend/src/components/ai/handoff/HandoffActionCard.tsx`
- `itsm-frontend/src/components/ai/handoff/__tests__/HandoffActionCard.test.tsx`
- `itsm-frontend/src/lib/i18n/translations-assistant.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
- `docs/plan/assistant-handoff-actions-plan-2026-10-19.md`

**新增**

- `docs/plan/evidence/assistant-dock/T5-04b-handoff-assign-evidence.md`
