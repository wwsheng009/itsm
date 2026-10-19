# H0-04 面板帮助视图与草稿通道 · 交付证据

- 日期：2026-10-18
- 任务：H0-04（方案 §5.4/§5.5/§7 P0；**P0 收口的最后一项**）
- 状态：`unit_verified`（KB 实体草稿/审阅发布通道归 H1）
- 交付物：
  - 前端：`components/ai/dock/AssistantHelpPanel.tsx`（页面帮助视图）、`AssistantChatPanel.tsx`（对话/帮助页签）、
    `lib/api/ai-api.ts`（`aiGetPageHelp` / `aiSearchHelp`）
  - 后端：`service/tool_registry_help.go` 新增 `help_create_draft`（plan：只落 bot_artifacts，零业务写入、不发布）
  - 测试：`AssistantHelpPanel.test.tsx`（5 例）、`AssistantChatPanel.test.tsx`（+1 页签用例）、
    `tool_registry_help_test.go`（+1 草稿产物用例：归属/租户隔离/fail-visible）

## 1. 交互与口径

| 项 | 实现 |
|---|---|
| 页签 | 面板顶部 Segmented：**对话 / 帮助**；帮助视图懒挂载（切页签才请求 `/ai/help/page`） |
| 当前页帮助 | 状态 Tag（已覆盖/需复核/缺失/未注册）+ 覆盖率 + 文档正文（复用 `MarkdownMessage`） |
| 未注册页面 | 提示"未注册页面键"，不发起请求（不误导） |
| 缺失/过时引导 | Alert +「让 AI 生成初稿」：切回对话页签并发送结构化 prompt（含 `pageKey`、参考同类文档结构、调用 `help_create_draft`、**不要发布**） |
| 帮助检索 | `Input.Search` → `GET /ai/help/search`（全局检索，片段+路径展示） |
| 草稿通道（后端） | `help_create_draft`（ToolRiskPlan）：标题缺省"帮助文档草稿：<页面标题>"；产物含 `pageKey/markdown/status=draft` 与证据（页面状态 + 参考文档路径）；归属/租户隔离由 ArtifactStore 保证 |

## 2. 验证证据（可复跑）

```powershell
# 前端
cd itsm-frontend
npx jest src/components/ai/dock --coverage=false --runInBand        # 3 suites / 13 tests
npx jest src/components/ai src/lib/ai --coverage=false --runInBand   # 16 suites / 119 tests
npx tsc --noEmit                                                     # 0 错误
npx eslint src/components/ai/dock src/lib/api/ai-api.ts src/lib/ai/page-context

# 后端
cd ../itsm-backend
go test ./service/ -run 'BuiltinToolMetadata|HelpTools|HelpCreateDraft|NormalizeToolMetadata' -count=1
go test ./service/bot/ -count=1
go test ./handlers/ai/ -count=1
```

关键断言：
- 已覆盖页（`/tickets/42`）：状态=已覆盖、覆盖率 100%、正文含"分诊建议"；
- 缺失页：Alert「暂无帮助文档」+ 点击生成初稿 → prompt 含 `pageKey=dashboard`/`help_create_draft`/`不要发布`；
- 过时页：提示「已过时」仍可生成初稿；未注册页：提示且**不请求**；
- 检索：提交 `SLA` → `aiSearchHelp('SLA', undefined, 5)` 被调用且结果渲染；
- 页签：对话→帮助→对话 往返正常（帮助视图挂载/卸载）；
- 草稿工具：`help_create_draft` 落 `bot_artifacts`（Kind=draft、Owner=发起人、Tenant 隔离；证据含 `pageStatus:"ok"`）；
  未注入 ArtifactStore / 参数缺失 → fail-visible。

## 3. 里程碑意义

H0-04 收口后，方案 **P0 全部交付**：D0-01～D0-05（Dock 常驻层：内核/容器/注册表/协议/建议卡+上下文条）
与 H0-01～H0-04（帮助文档：骨架/Manifest+Ledger/工具数据面/面板视图）。
下一步进入 **P1（D1 场景化 + H1 帮助闭环）** 与 **V 系列试点（V0-01 工单三件套 + 基线采集）**。

## 4. 边界与遗留（条件项）

- **KB 实体草稿/审阅/发布**：`help_submit_review` / `help_publish` 与 KB 草稿通道、PR 通道归 H1；
  当前"生成初稿"产出对话草稿 +（工具可用时）`help_create_draft` 产物；
- 帮助视图文案为静态中文（同批 i18n 条件项）；
- 帮助检索为词法检索（后端口径）；向量检索在 P2 分析增强时评估；
- 浏览器级 E2E（登录态 → 面板帮助/生成初稿）归联调。