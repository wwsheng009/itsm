# T5-03b 附件/截图双界面接线证据

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-03b**（父任务 T5-03 的第二个子项）
- **范围**：Dock/工作区附件选择器、消息附件展示、`/ai/chat/stream` `attachments[]` 契约、后端归属校验、非视觉模型文本降级
- **不含**：真实图片 OCR/视觉理解、文件正文解析、跨会话附件绑定与回收任务
- **提交**：随本文件同一提交（`feat(ai): T5-03b 附件双界面接线`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| Dock 可选择/上传/移除附件 | ✅ 与工作区共用 `ChatAttachmentPicker` | `AssistantChatPanel.tsx`；`ChatAttachmentPicker.test.tsx` |
| `/ai/chat` 工作区可选择附件 | ✅ 同一选择器接入输入坞 | `AIChat.tsx` |
| 用户消息展示附件 | ✅ 文件名芯片随消息渲染 | `ChatMessageItem.tsx`；消息项测试 |
| 对话请求携带附件 ID | ✅ 仅非空时落 `attachments`，缺省请求体不变 | `use-ai-chat-session.ts`；`ai-api.ts`；API/会话测试 |
| 后端校验附件归属 | ✅ 同租户 + 同用户 + `ai_chat` + `active` + 未软删；任一失败整体拒绝 | `attachments.go`；`TestResolveAIChatAttachmentsScope` |
| 非视觉模型降级 | ✅ 注入“仅元数据、未读取内容”的诚实提示，要求用户补充文字 | `buildAIChatAttachmentContext`；单测 |
| 上限与白名单 | ✅ 最多 5 个；单文件默认 10MB；复用共享扩展名白名单 | 选择器；附件域共享常量 |
| 后台精算不误用附件 | ✅ 有待发送附件时禁用“后台精算”并给出原因提示 | `AssistantChatPanel.tsx`；zh/en 文案 |
| 双界面行为一致 | ✅ 同一选择器、同一会话内核、同一请求字段 | 两表面接入点 |

---

## 2. 实现清单

### 2.1 前端

| 文件 | 改动 |
|---|---|
| `components/ai/chat-core/ChatAttachmentPicker.tsx` | 新增共享选择器：上传、进度、错误、芯片移除、5 个上限、10MB/扩展名校验 |
| `components/ai/dock/AssistantChatPanel.tsx` | 接入选择器；发送后清空；附件存在时禁用后台精算 |
| `components/ai/AIChat.tsx` | 工作区接入同一选择器与会话内核附件读取 |
| `components/ai/chat-core/use-ai-chat-session.ts` | 发送时读取附件、写入用户消息、请求体带 ID、发送后清空 |
| `components/ai/chat-core/types.ts` | `ChatMessage.attachments` 元数据 |
| `components/ai/chat-core/ChatMessageItem.tsx` | 用户消息附件芯片 |
| `lib/api/ai-api.ts` | `AIChatStreamRequest.attachments`；仅非空时落字段 |
| `lib/i18n/translations-assistant.ts` | zh/en 附件文案（含后台精算不支持提示） |

关键交互：

1. 选择文件后立即上传，成功才产生可发送附件；
2. 失败/白名单外文件只显示错误，不产生附件芯片；
3. 发送时把附件 ID 写入请求，用户消息保留元数据用于展示；
4. 发送入队后立即清空输入坞附件，避免下一条消息误带；
5. 未携带附件时请求体不新增字段，保持既有逐字节契约。

### 2.2 后端

| 文件 | 改动 |
|---|---|
| `handlers/ai/handler.go` | `Chat` / `ChatStream` 接受 `attachments[]`，校验后改写问题文本 |
| `handlers/ai/attachments.go` | `resolveAIChatAttachments` 归属/状态/上限校验；`buildAIChatAttachmentContext` 文本降级 |
| `handlers/ai/attachments_test.go` | 归属校验负向测试 + 降级文本测试 |

降级口径：

```text
[用户上传附件（仅元数据，未自动读取内容）]
- 截图.png（image/png，1024 bytes）
如果回答依赖附件正文或图片内容，请明确说明当前模型不能直接读取，并请用户补充文字描述。
```

> 该口径不伪造 OCR/视觉结果。后续接入真实多模态时，再替换为结构化内容块。

---

## 3. 安全边界

- 客户端只提交附件 ID，不能提交文件名、MIME、大小或宿主；
- 服务端重新读取附件行并按以下条件过滤：
  - `tenant_id = 当前租户`
  - `biz_type = ai_chat`
  - `biz_id = 当前用户`
  - `uploaded_by = 当前用户`
  - `status = active` 且 `deleted_at IS NULL`
- 任一 ID 不可用即整单拒绝，避免“部分附件被静默忽略”；
- 重复 ID 去重，最多 5 个；
- 下载仍走 T5-03a 的归属校验端点，对话接口不返回文件内容；
- 附件内容未进入模型上下文，避免把未扫描/未解析内容误当作可信输入。

---

## 4. 验证

### 4.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `gofmt -w handlers/ai/attachments.go handlers/ai/handler.go handlers/ai/attachments_test.go` | ✅ 无格式差异 |
| `go test -count=1 ./service` | ✅ `ok itsm-backend/service 66.456s` |
| `go test -count=1 -p 1 ./handlers/ai` | ✅ `ok itsm-backend/handlers/ai 6.806s` |
| `go test -count=1 -p 1 ./router` | ✅ `ok itsm-backend/router 4.500s` |

说明：三包一次并行执行时 Windows 链接器出现 `memory exhausted`；串行重跑 3 包均通过，不是用例失败。

关键断言：

- `TestResolveAIChatAttachmentsScope`：请求顺序保序、重复 ID 去重；跨租户/跨用户/非 AI 宿主/软删/非法 ID/超过 5 个全部拒绝；
- `TestBuildAIChatAttachmentContext`：无附件时原文不变；有附件时包含文件名、MIME、大小与“未自动读取内容”提示；
- `TestAIAttachmentEndpointsUnavailable`：T5-03a 的服务未装配 503 行为保持。

### 4.2 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |
| `npm test -- --runTestsByPath <6 suites> --coverage=false --runInBand --silent` | ✅ 6 suites / 107 tests passed |

关键断言：

- `ChatAttachmentPicker`：上传成功回传元数据；上传失败不产生芯片；白名单外文件不发请求；
- `useAIChatSession`：附件进入用户消息与请求体，发送后清空；
- `ChatMessageItem`：用户消息展示附件芯片；
- `ai-api`：仅有附件时落 `attachments`，缺省不落；
- Dock 面板回归：既有 15 项行为不受影响。

---

## 5. 局限与后续

1. **未接真实视觉/文件解析**：当前按元数据 + 文本提示降级；模型不会读取附件正文。
2. **附件仍是用户级草稿**：`biz_id=userID`，未与会话/消息建立持久关联；跨会话附件绑定、保留期与孤儿清理仍需后续任务。
3. **历史消息不回填附件芯片**：会话消息接口未持久化附件元数据；刷新/切换会话后仅保留文本。
4. **未跑真实浏览器上传/PG E2E**：本次为 handler/service 与组件级验证。
5. **截图粘贴未接**：当前覆盖文件选择器；剪贴板粘贴/拖拽可作为增强项。

---

## 6. 变更文件

**新增**

- `itsm-frontend/src/components/ai/chat-core/ChatAttachmentPicker.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/ChatAttachmentPicker.test.tsx`
- `docs/plan/evidence/assistant-dock/T5-03b-ai-attachment-dock-evidence.md`

**修改**

- `itsm-backend/handlers/ai/attachments.go`
- `itsm-backend/handlers/ai/handler.go`
- `itsm-backend/handlers/ai/attachments_test.go`
- `itsm-frontend/src/components/ai/AIChat.tsx`
- `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx`
- `itsm-frontend/src/components/ai/chat-core/types.ts`
- `itsm-frontend/src/components/ai/chat-core/use-ai-chat-session.ts`
- `itsm-frontend/src/components/ai/chat-core/ChatMessageItem.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/use-ai-chat-session.test.tsx`
- `itsm-frontend/src/components/ai/chat-core/__tests__/ChatMessageItem.test.tsx`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/lib/api/__tests__/ai-api.test.ts`
- `itsm-frontend/src/lib/i18n/translations-assistant.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
