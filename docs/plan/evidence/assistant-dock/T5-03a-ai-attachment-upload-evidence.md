# T5-03a AI 附件上传面与契约证据

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-03a**（T5-03 拆分；双界面/视觉降级归 T5-03b）
- **范围**：AI 专用附件宿主、上传/下载端点、前端上传 API
- **不含**：双界面选择器、消息附件展示、对话请求附件字段、多模态视觉
- **提交**：随本文件同一提交（`feat(ai): T5-03a AI 附件上传面`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| 附件宿主可复用附件域 | ✅ 新增 `ai_chat` 宿主，`biz_id=userID`，校验同租户用户存在 | `attachment_service.go`；host 测试 |
| 普通用户可上传 | ✅ 专用 `POST /api/v1/ai/attachments`，权限 `ai:read`，不再被 `attachment:write` 兜底码拦截 | `ai_routes.go`；handler |
| 附件读取隔离 | ✅ `GET /api/v1/ai/attachments/:id/content` 校验租户 + `ai_chat` + `bizId=userID` + 上传人 | `attachments.go` |
| 复用加固链路 | ✅ 复用通用附件：大小/类型校验、配额、病毒扫描、审计、存储 | `AttachmentService.Upload/GetFile` |
| 前端上传 API | ✅ `aiUploadAttachment(file, onProgress)` + `aiAttachmentContentUrl(id, disposition)` | `ai-api.ts`；API 测试 |
| 服务未装配 | ✅ 端点返回 503，不误报成功 | handler 测试 |

---

## 2. 实现清单

### 2.1 后端

| 文件 | 改动 |
|---|---|
| `service/attachment_service.go` | 新增 `AttachmentBizTypeAIChat="ai_chat"` 与宿主（用户存在性；无富文本引用保护） |
| `handlers/ai/service.go` | 注入 `AttachmentService`（nil = 503） |
| `handlers/ai/attachments.go` | 新增 `UploadAIAttachment` / `DownloadAIAttachment`；上传固定宿主，下载校验归属；inline 仅图片 |
| `handlers/ai/attachments_test.go` | 未装配 → 503 |
| `internal/bootstrap/app.go` | 注入 attachment service |
| `router/ai_routes.go` | 注册两个 `ai:read` 路由 |

### 2.2 前端

| 文件 | 改动 |
|---|---|
| `lib/api/ai-api.ts` | 新增 `AIAttachmentRef`、`aiUploadAttachment`、`aiAttachmentContentUrl` |
| `lib/api/__tests__/ai-api.test.ts` | 上传端点/进度透传/预览地址测试 |

---

## 3. 端点契约

```http
POST /api/v1/ai/attachments
Content-Type: multipart/form-data
file: <binary>

200 { id, fileName, fileSize, mimeType, fileUrl }
503 服务未装配
```

```http
GET /api/v1/ai/attachments/:id/content
GET /api/v1/ai/attachments/:id/content?disposition=inline

200 文件流（inline 仅对 image/* 生效）
404 不存在 / 跨用户 / 非 AI 附件
```

安全口径：

- 上传宿主固定 `biz_type=ai_chat`、`biz_id=userID`，客户端不能指定宿主；
- 下载要求同租户、同宿主、同 `userID`、同上传人；
- 文件大小/类型/配额/病毒扫描沿用通用附件服务；
- 审计资源仍为 `attachment`，动作 `upload`。

---

## 4. 验证

### 4.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `go test -count=1 ./service ./handlers/ai ./router` | ✅ 3 包通过（69.307s / 6.679s / 4.891s） |
| `gofmt -w <改动 Go 文件>` | ✅ 无格式差异 |

关键断言：

- `TestDefaultAttachmentHostAIChat`：同租户用户存在；跨租户/不存在用户拒绝。
- `TestAIAttachmentEndpointsUnavailable`：上传/下载未装配均 503。
- host 覆盖守卫包含 `ai_chat`。

### 4.2 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm test -- --runTestsByPath src/lib/api/__tests__/ai-api.test.ts --coverage=false --runInBand --silent` | ✅ 1 suite / 66 tests passed |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

关键断言：

- `aiUploadAttachment` 调用 `/api/v1/ai/attachments` 并透传进度；
- `aiAttachmentContentUrl` 生成内容/inline 地址。

---

## 5. 局限与后续（T5-03b）

1. **双界面 UI 未做**：Dock 与 `/ai/chat` 还没有附件选择器/预览/删除。
2. **对话请求未带附件**：`/ai/chat/stream` 尚无 `attachments[]`；由 T5-03b 接入并做后端归属校验。
3. **视觉能力未接**：模型不支持视觉时需文本描述降级；文本文件提取也待 T5-03b。
4. **附件为草稿**：当前按用户级草稿保存，会话关联/清理策略在 T5-03b 明确。
5. **未跑真实浏览器/PG E2E**：上传端点仅服务/handler 级验证。

---

## 6. 变更文件

**新增**

- `itsm-backend/handlers/ai/attachments.go`
- `itsm-backend/handlers/ai/attachments_test.go`
- `itsm-backend/service/attachment_hosts_ai_test.go`
- `docs/plan/evidence/assistant-dock/T5-03a-ai-attachment-upload-evidence.md`

**修改**

- `itsm-backend/service/attachment_service.go`
- `itsm-backend/service/attachment_hosts_wave2_test.go`
- `itsm-backend/handlers/ai/service.go`
- `itsm-backend/internal/bootstrap/app.go`
- `itsm-backend/router/ai_routes.go`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/lib/api/__tests__/ai-api.test.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
