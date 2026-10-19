# I0-02 任务卡模型（tasks[] 分组） · 交付证据

- 日期：2026-10-18
- 任务：I0-02（方案 §3.3 任务优先 / I 系列；注册表新增 `tasks[]`，建议区分组；验收：Top 20 页面每页 ≥1 个零输入任务卡）
- 状态：`integration_verified`（模型 + 注册表迁移 + 消费端分组渲染 + 生成物同步 + 契约门禁）
- 交付物：
  - 类型：`lib/ai/page-context/types.ts`（`PageAssistantTask`/`PageAssistantQuestion` 分组模型 +
    `flattenPageActions()` 统一扁平视图——任务卡在前）
  - 注册表：`lib/ai/page-context/registry.ts`（21 页拆分为 `tasks[]`/`questions[]`；**6 页补齐任务卡**）
  - 生成器：`scripts/gen-page-manifest.mjs`（分组 → 扁平 `suggestions`，服务端 D1-01 注册表同源再生）
  - 消费端：`SuggestionCards`（任务卡/问题卡两组渲染 + 组头 + 全局 rank 保持）与
    `use-assistant-suggestions`（静态基线扁平化）
  - 契约：`registry.test.ts` 重写门禁（≥1 任务卡/页、任务 1–2、问题 0–2、总数 ≤3、id 唯一、D4 只读、任务在前）

## 1. 口径（实现即契约）

| 规则 | 值 |
|---|---|
| 每页任务卡 | **≥1**（I0-02 验收）；≤2 |
| 每页问题卡 | 0–2 |
| 每页动作总数 | ≤3（保持建议区紧凑，兼容 §3.3 "每页 1–3 个动作"） |
| id 唯一性 | 页内唯一（跨 tasks/questions 合并检查） |
| 只读不变量 | 任务卡与问题卡 `readOnly: true`（D4：零输入不得触写） |
| 顺序口径 | `flattenPageActions()` = 任务卡在前、问题卡在后；与服务端 rank、建议区渲染顺序一致 |

新增任务卡（原先仅有问题卡/零任务卡的页面补齐）：

| 页面 | 新增任务卡 | 依赖工具（D1-01 门禁） |
|---|---|---|
| ticket.aiCreate | 把描述整理成工单字段（只读草稿） | draft_ticket_fields |
| problem.list | 汇总未关闭问题（只读） | — |
| ci.create | 整理配置项填写清单（只读） | — |
| knowledge.list | 整理知识检索清单（只读） | list_kb |
| service.catalog | 推荐应选的服务条目（只读） | — |
| ai.observability | 汇总近期错误与降级（只读） | — |

## 2. 分组渲染（I0-02 "建议区分组"）

`SuggestionCards` 渲染两组：`suggestion-group-tasks`（组头「任务卡（零输入可执行；只读）」）与
`suggestion-group-questions`（组头「问题卡（了解）」）；`suggestion_clicked` 的 `rank` 仍为**全局序号**
（任务组在前），与 `suggestion_shown` 及服务端过滤顺序对齐，观测口径不漂移。

## 3. 验证证据（可复跑）

```powershell
cd itsm-frontend
node scripts/gen-page-manifest.mjs            # 21 页覆盖 100%；再生 page-manifest + 内嵌 helpindex/assistantcontext
node scripts/gen-page-manifest.mjs --check    # assistant registry in sync + ratchet OK
npx jest src/components/ai src/lib/ai --coverage=false --watchAll=false --runInBand   # 19 suites / 142 tests
npx tsc --noEmit ; npx eslint src/lib/ai/page-context scripts/gen-page-manifest.mjs src/components/ai/chat-core/SuggestionCards.tsx …

cd ../itsm-backend
go test ./internal/assistantcontext/ ./handlers/ai/ -count=1    # 全 ok（内嵌注册表再生后契约/过滤用例全绿）
```

关键断言：
- 契约测试：21 页全部 ≥1 任务卡；任务/问题数量上限；id 唯一；D4 全只读；扁平化任务在前；
- 组件测试（新增 1 例）：ticket.detail 渲染两组、任务组含「分诊建议」、问题组含「知识库有没有相关方案？」、
  DOM 顺序任务组在前；原有点击/埋点/服务端过滤用例保持全绿；
- 生成物：`assistant-registry.json` 与 `page-manifest.json` 由单一真源再生，漂移门禁（`--check`）通过。

## 4. 影响与条件项（不阻塞）

- 6 个页面的**页面定义发生变化**（新增任务卡）→ H1-03 夜批会将其识别为 `manifest_changed:suggestions`
  并生成 `review_update` 提议（预期行为：帮助文档可顺带复核）；
- 新增文案为静态中文（i18n 与帮助视图同批条件项）；
- 分组视觉（组头字色/间距）按最小改动实现，可用性走查归 I 系列审查批次；
- 后续项：I0-03（快捷键/命令面板）、I0-04（Outcome Contract 回执）、I1-01（Inline Apply 分诊一键应用）。