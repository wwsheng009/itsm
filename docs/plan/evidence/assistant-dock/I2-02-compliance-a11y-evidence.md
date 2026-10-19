# I2-02 合规与无障碍专项（会话导出/删除 + axe 扫描） · 交付证据

- 日期：2026-10-18
- 任务：I2-02（方案 I 系列｜验收：会话导出/删除；axe 扫描通过）
- 状态：`integration_verified`（导出端点 + 工作区导出入口 + 下载链路单测 + 3 个 Copilot 界面 axe 扫描全通过）
- 交付物：
  - 后端：`handlers/ai/export.go`（`ExportConversation`：Markdown 快照生成；结构化回答抽取可读文本）+
    路由 `GET /api/v1/ai/conversations/:id/export`（ai:read；跨租户=不存在同口径 404）+
    `authz-gen` 预检再生（`/api/v1/ai/conversations/*/export → ai:read`）
  - 前端：`exportConversation()`（ai-api）+ `lib/utils/download-text-file.ts`（Blob → `<a download>` → revoke，
    受限环境静默降级）+ `use-ai-chat-session.handleExportConversation`（导出→落盘→提示；失败不抛错）+
    工作区会话列表「导出会话」按钮（只读，不受 `ai:write` 门控；删除仍按 `ai:write` 既有门控）
  - 无障碍：`jest-axe` + `axe-core` 依赖与
    `components/ai/__tests__/a11y-surfaces.test.tsx`（面板 / 诊断抽屉（含轨迹视图）/ 内联建议条 三界面扫描）

## 1. 口径与边界

| 项 | 口径 |
|---|---|
| 导出范围 | 租户内可导出（与 `GET /ai/conversations/:id` 同一边界）；跨租户与不存在同口径 404（不泄漏存在性） |
| 导出内容 | Markdown：标题 / 导出时间 / 消息数 + 逐条「角色 · 时间 + 内容」；AI 回答为 JSON 载荷时优先抽取 `answer/content/message` 文本 |
| 下载落盘 | 前端 Blob 下载（`ai-conversation-<id>.md`）；无 Blob URL 支持的环境**静默跳过下载**并返回 false（数据获取已完成，不阻断） |
| 删除 | 既有能力（`DELETE /ai/conversations/:id`，ai:write）+ 工作区二次确认（Popconfirm），本批未改动语义 |
| axe 扫描 | WCAG 2.1 A/AA + best-practice；**仅**禁用 `color-contrast`（jsdom 无真实布局/级联样式，无法可靠计算，其他规则全开） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l handlers/ai/export.go handlers/ai/export_handler_test.go
go test ./handlers/ai/ -run 'TestExportConversation' -count=1        # ok（快照内容/跨租户 404/非法 400）
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1   # 5 包全绿
go run ./cmd/authz-gen        # 再生：/api/v1/ai/conversations/*/export → ai:read

cd ../itsm-frontend
npx jest src/components/ai/__tests__/a11y-surfaces.test.tsx src/lib/utils --coverage=false --watchAll=false --runInBand
npx jest "src/components/ai/chat-core/__tests__/use-ai-chat-session.test.tsx" --runInBand
npx jest src/components/ai src/lib/ai src/lib/store src/lib/api/__tests__/ai-api.test.ts src/lib/utils --runInBand
# → 51 suites / 505 tests 全绿
npx tsc --noEmit ; npx eslint src/lib/utils src/components/ai/__tests__/a11y-surfaces.test.tsx src/components/ai/AIChat.tsx …
```

关键断言：
- 导出端点：Markdown 含标题/消息数/「## 用户 ·」/「## AI ·」；JSON 回答抽取为纯文本（**不含 `"answer"` 壳**）；
  跨租户与未命中 → 404，非法 id → 400；
- 前端导出：调用 `exportConversation(5)` 且 `downloadTextFile('ai-conversation-5.md', markdown)`；
  失败路径（403）不抛错、不触发下载；下载工具在缺少 Blob URL API 时返回 false 且不抛错、正常路径 anchor 点击 + revoke；
- axe：面板（空态/页签/输入坞）、诊断抽屉（含轨迹视图，扫描 body portal）、内联建议条 **0 violations**。

## 3. 条件项（不阻塞）

- 无障碍扫描覆盖面：当前为 Copilot 三界面；工作区 `/ai/chat`、帮助面板、审批页等可在后续批量纳入
  （同一 `AXE_OPTIONS` 口径）；真实浏览器的对比度/键盘走查归联调；
- 导出为**用户主动**行为（不落服务端文件，避免留存面）；如后续需要"合规报送导出"，再评估服务端落盘与保留期策略；
- 会话删除的审计留痕沿用既有删除链路；如需独立保留期策略（§13.1）归后续数据治理批。