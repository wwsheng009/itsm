# T5-04a handoff 打开页面与预填表单证据

- **状态**：已完成（2026-10-19）
- **任务**：`docs/plan/assistant-handoff-actions-plan-2026-10-19.md` 的 **T5-04a**
- **范围**：消息级「转人工」入口 + 打开对应页面 + 预填新建工单/事件
- **不含**：指派（T5-04b）、IM/电话转接、自动提交
- **提交**：随本文件同一提交（`feat(ai): T5-04a handoff 打开与预填`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| 双界面消息级入口 | ✅ `ChatMessageItem` 新增「转人工」动作，Dock/工作区共用 | `ChatMessageItem.tsx`；两套件回归 |
| 打开对应页面 | ✅ 当前对象（ticket/incident/change/CI）按权限打开；无当前对象时回退工单/事件列表 | `HandoffActionCard.tsx` |
| 预填表单 | ✅ 预填新建工单/事件；只填标题/描述空白字段；消费后清除一次性 state | `prefill.ts`；两个创建页 |
| 不自动提交 | ✅ 预填仅 `form.setFieldsValue`，不调用创建 API | 创建页 diff；单测 |
| 权限裁剪 | ✅ 打开需目标域 `read`；预填需 `ticket:create` / `incident:write` | `HandoffActionCard` 测试 |
| 失败/空态 | ✅ 无可用动作显示空态；非法 state 丢弃 | 权限空态测试；prefill 单测 |
| 可观测 | ✅ 埋点 `handoff` 记录 action/targetType/targetId，不记录正文 | `HandoffActionCard` |

---

## 2. 实现清单

### 2.1 契约与预填

| 文件 | 改动 |
|---|---|
| `src/lib/ai/handoff/prefill.ts` | 新增 `HANDOFF_PREFILL_SOURCE`、`buildHandoffPrefillState`、`readHandoffPrefill`、`pickBlankHandoffPrefill`；标题/描述截断与空白字段保护 |
| `src/components/ai/handoff/HandoffActionCard.tsx` | 新增动作卡：打开当前对象/列表、预填工单/事件；权限裁剪与埋点 |

### 2.2 双界面接线

| 文件 | 改动 |
|---|---|
| `src/components/ai/chat-core/ChatMessageItem.tsx` | 新增 `onHandoff` prop 与「转人工」按钮 |
| `src/components/ai/dock/AssistantChatPanel.tsx` | 打开动作卡并传入当前消息 |
| `src/components/ai/AIChat.tsx` | 工作区同样打开动作卡 |
| `src/lib/i18n/translations-assistant.ts` | `assistant.handoff.*` zh/en 文案 |

### 2.3 创建页预填消费

| 文件 | 改动 |
|---|---|
| `src/pages/(main)/tickets/create/index.tsx` | 读取 `ticket` 预填 state；只填空白 title/description；清除 state，不提交 |
| `src/pages/(main)/incidents/create/index.tsx` | 读取 `incident` 预填 state；同上 |

---

## 3. 行为口径

| 场景 | 行为 |
|---|---|
| 当前页有 ticket/incident/change/CI 目标 | 显示「打开当前对象」并跳转对应详情 |
| 无当前目标 | 按权限回退「打开工单列表」或「打开事件列表」 |
| 预填新建工单 | 跳转 `/tickets/create`，携带 `assistant_handoff` state；仅填空白标题/描述 |
| 预填新建事件 | 跳转 `/incidents/create`，同上 |
| 用户已有输入 | 不覆盖；只补空白字段 |
| 刷新/后退 | state 已清除，不重复预填 |
| 无权限 | 动作不渲染；全部无权限显示空态 |
| 提交 | 仍需用户在创建页手动提交，handoff 不调用创建 API |

---

## 4. 验证

### 4.1 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm test -- --runTestsByPath src/lib/ai/handoff/__tests__/prefill.test.ts src/components/ai/handoff/__tests__/HandoffActionCard.test.tsx src/components/ai/chat-core/__tests__/ChatMessageItem.test.tsx src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx src/components/ai/__tests__/AIChat.diagnostics.test.tsx --coverage=false --runInBand --silent` | ✅ **5 suites / 29 tests passed** |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

关键断言：

- 打开当前对象 → `/tickets/42`，埋点 action=open。
- 预填新建工单 → `/tickets/create`，state 含 `source/targetType/prefill`。
- 无 `ticket:create` / `incident:write` → 只保留打开动作。
- 无任何权限 → 空态。
- 预填只填空白字段，已有值不覆盖。
- 消息级「转人工」把当前消息交给动作卡。

---

## 5. 局限与后续

1. **T5-04b 指派未实现**：候选人选择、域指派 API、确认与并发冲突仍待做。
2. **预填字段有限**：本期仅标题/描述；优先级、分类、CI 等字段需目标页声明白名单后扩展。
3. **未跑真实浏览器 E2E**：页面消费逻辑由共享 helper + 组件测试覆盖；刷新/后退行为建议在联调补浏览器断言。
4. **打开动作范围**：已覆盖 ticket/incident/change/CI 当前对象；工单/事件列表为回退。
5. **审计**：本期复用 `handoff` 埋点；业务域写操作的审计待 T5-04b 指派接入。

---

## 6. 变更文件

**新增**

- `itsm-frontend/src/lib/ai/handoff/prefill.ts`
- `itsm-frontend/src/lib/ai/handoff/__tests__/prefill.test.ts`
- `itsm-frontend/src/components/ai/handoff/HandoffActionCard.tsx`
- `itsm-frontend/src/components/ai/handoff/__tests__/HandoffActionCard.test.tsx`
- `docs/plan/evidence/assistant-dock/T5-04a-handoff-open-prefill-evidence.md`

**修改**

- `itsm-frontend/src/components/ai/chat-core/ChatMessageItem.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/ChatMessageItem.test.tsx`
- `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx`
- `itsm-frontend/src/components/ai/AIChat.tsx`
- `itsm-frontend/src/lib/i18n/translations-assistant.ts`
- `itsm-frontend/src/pages/(main)/tickets/create/index.tsx`
- `itsm-frontend/src/pages/(main)/incidents/create/index.tsx`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
