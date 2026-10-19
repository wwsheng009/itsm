# T5-09a 帮助写面与一键审阅证据

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-09a**（T5-09 拆分；向量化归 T5-09b）
- **范围**：`help_update_draft` / `help_submit_review` / `help_publish`、帮助草稿审阅队列、行级 diff、一键审阅 UI
- **通道**：KB「用户指南」草稿 + KB 发布/版本链路；写动作经既有审批队列，不直写 KB
- **不含**：`help-index` 向量化（T5-09b）、Git PR 通道、跨租户发布
- **提交**：随本文件同一提交（`feat(ai): T5-09a 帮助写面与一键审阅`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| `help_update_draft` | ✅ 仅 owner 可改；递增 revision；审阅中/已发布禁止改正文 | `tool_registry_help.go`；`TestHelpWriteFlow_UpdateSubmitPublish` |
| `help_submit_review` | ✅ act_low；落「用户指南」KB 草稿；artifact 记 `in_review + articleId`；作者保持原 owner | 同上；`TestHelpDraft_PublishBoundary` |
| `help_publish` | ✅ act_medium + `knowledge:admin`；只发布关联的「用户指南」草稿 | 同上 |
| 审批不绕过 | ✅ 两个写工具 `ReadOnly=false`，由 `ExecuteTool` 建 pending，审批后由 ToolQueue 执行 | `tool_registry_help.go` 元数据；既有审批链路回归 |
| diff 预览 | ✅ 服务端按行输出 added/removed；无既有文档时全部按新增 | `handlers/ai/help_drafts.go`；`TestHelpDraftDiff` |
| 一键审阅 UI | ✅ 覆盖率看板新增草稿队列；查看 diff、提交审阅、发布、打开文章 | `HelpDraftReviewCard.tsx`；2 suites / 13 tests |
| 队列隔离 | ✅ 同租户跨 owner 仅 `knowledge:write` 可读；跨租户不返回 | handler 测试；`GetTenantDraft/ListTenantHelpDrafts` |
| 降级 | ✅ 草稿接口失败不阻断覆盖率主数据；未装配返回 503 | 页面测试；handler 测试 |
| 权限预检 | ✅ 重新生成 `rbac_precheck_gen.go`，路由声明与预检一致 | `TestPrecheckMapIsFresh` / `TestRoutePrecheckAlignment` |

---

## 2. 实现清单

### 2.1 后端

| 文件 | 改动 |
|---|---|
| `service/tool_registry_help.go` | 新增 3 个写/plan 工具；草稿内容契约；审阅/发布状态机；作者/发布人归属 |
| `service/tool_registry.go` | 注入 `HelpDocPublisher`；内置执行分支 |
| `service/bot/artifact.go` | `UpdateDraft` / `GetTenantDraft` / `ListTenantHelpDrafts` |
| `handlers/knowledge/service.go` | `CreateHelpDraft`（用户指南草稿）与 `PublishHelpDraft`（分类边界） |
| `handlers/ai/help_drafts.go` | `GET /ai/help/drafts` 审阅队列 + 行级 diff |
| `handlers/ai/service.go` | 注入 artifact store |
| `internal/bootstrap/app.go` | 装配 artifact store 与 KB publisher |
| `router/ai_routes.go` | 注册 `GET /ai/help/drafts`（`knowledge:write`） |
| `middleware/rbac_precheck_gen.go` | 重新生成路由预检 |
| `docs/acl-manifest.yaml` | 重新生成 ACL 清单（含 T5-03a / T5-09a 路由） |

状态机：

```text
draft --help_submit_review(审批)--> in_review + knowledgeArticleId
in_review --help_publish(审批)--> published
in_review/published --help_update_draft--> 拒绝（防止 artifact 与 KB 草稿漂移）
```

边界：

- `help_update_draft` 仍为 plan（只写 `bot_artifacts`），不产生业务写入；
- `help_submit_review` 要求 `knowledge:write`，创建 KB 草稿但不发布；
- `help_publish` 要求 `knowledge:admin`，只允许发布「用户指南」分类；
- 审阅者可以提交/发布他人草稿，但 KB 草稿作者仍为 artifact owner；
- 发布走 `PublishArticle`，版本、发布说明、向量 outbox 均复用既有链路。

### 2.2 前端

| 文件 | 改动 |
|---|---|
| `lib/api/ai-api.ts` | `aiListHelpDrafts` 与审阅队列/diff 类型 |
| `pages/(main)/ai/help-coverage/HelpDraftReviewCard.tsx` | 草稿表、状态、diff 弹窗、提交/发布/打开文章 |
| `pages/(main)/ai/help-coverage/index.tsx` | 独立加载草稿队列；失败不阻断覆盖率主数据；工具动作提示待审批 |
| 对应测试 | API 解析、队列动作、diff 展示、状态动作裁剪 |

---

## 3. 端点/工具契约

```http
GET /api/v1/ai/help/drafts
权限：knowledge:write

200 {
  items: [{
    artifactId, ownerUserId, pageKey, title, status,
    revision, knowledgeArticleId?, markdown, baselineMarkdown,
    diff: [{type: "added"|"removed", content}],
    sourceDocs?, submittedAt?, publishedAt?, createdAt
  }],
  total
}
503 草稿队列未装配
```

```json
{
  "name": "help_update_draft",
  "args": {"artifactId": 31, "content": "## 更新后的 Markdown"}
}
```

```json
{
  "name": "help_submit_review",
  "args": {"artifactId": 31}
}
```

```json
{
  "name": "help_publish",
  "args": {"artifactId": 31, "changeLog": "审阅通过"}
}
```

后两者是写工具：`POST /api/v1/agent/tools/execute` 返回 pending，人工审批通过后由 ToolQueue 执行。

---

## 4. 验证

### 4.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `gofmt -w <改动 Go 文件>` | ✅ 无格式差异 |
| `go test -count=1 -p 1 ./handlers/ai` | ✅ `8.006s` |
| `go test -count=1 -p 1 ./service` | ✅ `62.608s` |
| `go test -count=1 -p 1 ./handlers/knowledge` | ✅ `1.548s` |
| `go test -count=1 -p 1 ./service/bot` | ✅ `3.476s` |
| `go test -count=1 -p 1 ./router` | ✅ `5.376s` |
| `go test -count=1 -p 1 ./middleware -run 'TestPrecheckMapIsFresh\|TestRoutePrecheckAlignment'` | ✅ `1.542s` |
| `go run ./cmd/authz-gen` | ✅ 预检生成物已同步 |

关键断言：

- 更新 → 提交审阅 → 发布完整链路；
- 跨 owner 更新拒绝，跨 owner 审阅允许且 KB 作者保持原 owner；
- 审阅中不可改正文；未提交审阅不可发布；已发布发布动作幂等；
- 非「用户指南」文章不能经 `PublishHelpDraft` 发布；
- 审阅队列按租户收敛，diff 对重复行不会误报多条删除；
- 服务/产物未装配时 fail-visible（503/明确错误）。

### 4.2 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |
| `npm test -- --runTestsByPath <3 suites> --coverage=false --runInBand --silent` | ✅ 3 suites / 81 tests passed |

关键断言：

- 草稿队列渲染标题/状态/revision/owner；
- diff 弹窗展示 added/removed 行；
- draft 显示「提交审阅」，in_review 显示「发布」，published 显示文章入口；
- 提交动作调用 `aiExecuteTool('help_submit_review', {artifactId})`，pending 后提示等待审批；
- 草稿接口失败不阻断覆盖率主数据。

---

## 5. 局限与后续

1. **T5-09b 未做**：`help-index` 仍是词法检索，无语义向量索引。
2. **未接 Git PR 通道**：当前写面走 KB「用户指南」草稿 + 发布链路，适合租户运营；docs-as-code PR 通道需仓库凭据，未纳入本批。
3. **审阅状态不单独开表**：`draft/in_review/published` 存在 artifact `content_json`，审阅队列按工具名过滤；规模增大时可迁移到强类型列/表。
4. **无逐次 revision 快照**：artifact 只保留当前正文 + revision 号；KB 侧保留发布版本并支持历史对比。
5. **未跑真实浏览器 + 生产 PG 审批 E2E**：本批为 service/handler/组件级验证；审批队列沿用既有回归覆盖。
6. **ACL 清单仍有存量 59 条未保护路由**：本次仅重新生成清单，不修改既有欠账。

---

## 6. 变更文件

**新增**

- `itsm-backend/handlers/ai/help_drafts.go`
- `itsm-backend/handlers/ai/help_drafts_test.go`
- `itsm-backend/handlers/knowledge/help_write_test.go`
- `itsm-frontend/src/pages/(main)/ai/help-coverage/HelpDraftReviewCard.tsx`
- `itsm-frontend/src/pages/(main)/ai/help-coverage/__tests__/HelpDraftReviewCard.test.tsx`
- `docs/plan/evidence/assistant-dock/T5-09a-help-write-review-evidence.md`

**修改**

- `itsm-backend/service/tool_registry_help.go`
- `itsm-backend/service/tool_registry_help_test.go`
- `itsm-backend/service/tool_registry.go`
- `itsm-backend/service/tool_metadata_test.go`
- `itsm-backend/service/bot/artifact.go`
- `itsm-backend/handlers/knowledge/service.go`
- `itsm-backend/handlers/ai/service.go`
- `itsm-backend/internal/bootstrap/app.go`
- `itsm-backend/router/ai_routes.go`
- `itsm-backend/middleware/rbac_precheck_gen.go`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `itsm-frontend/src/lib/api/__tests__/ai-api.test.ts`
- `itsm-frontend/src/pages/(main)/ai/help-coverage/index.tsx`
- `itsm-frontend/src/pages/(main)/ai/help-coverage/__tests__/index.test.tsx`
- `docs/acl-manifest.yaml`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
