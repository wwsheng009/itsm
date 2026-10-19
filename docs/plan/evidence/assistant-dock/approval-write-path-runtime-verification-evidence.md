# 写路径审批闭环 · 浏览器实测与缺陷修复证据（238 demo，2026-10-18）

- 环境：`http://172.18.3.238:8089`，`itsm_demo`
- 修复前版本：`1.8.0-dev.d51ed172`（dock-plan 全量交付）
- 修复后版本：`1.8.0-dev.abd8705a`（本批，提交 `abd8705aab67f259df5146adaf8cdc1a0ba7b892`；写路径修复提交 `2926ddc8`）
- 验证方式：tabbit CLI + Playwright 真实浏览器（`tbG` 修复前、`tbG2` 修复后）

## 1. 结论

写路径审批闭环**在修复后完整打通**：LLM 调用 `create_ticket` → Gate3 待审批 → 对话内确认抽屉 → 人工批准 → 队列执行 → 工单真实落库 → 卡片收敛为「已处理」。

修复前同一路径**必然失败**，并在 UI 上呈现为误导性的「已过期/不可用」。

## 2. 修复前实测（tbG，版本 d51ed172）

| 观察点 | 实测结果 |
|---|---|
| 待审批创建 | ✅ invocation `#33`，`approvalState=pending`，卡片显示「待审批」+「确认 / 拒绝」 |
| 确认抽屉 | ✅ `执行确认`，风险 `act_low`，脱敏参数仅 `{"requester_id":1}` |
| 人工批准 | ✅ `approval_state=approved`、`approved_by=1`、`approved_at` 落库 |
| 执行结果 | ❌ `status=failed`、`error=create_ticket: title is required` |
| 卡片状态 | ❌ 显示「已过期/不可用」 |
| 后端请求 | ❌ 同一秒 20+ 次 `GET /agent/tools/33` → `429` |

LLM 当时自述：「create_ticket 工具当前入参仅支持 ci_id，不支持直接传入标题/描述/优先级/类型」——与实际执行层不符，正是 schema 缺参导致。

## 3. 根因与修复

### 缺陷 A：`create_ticket` 模型可见参数与执行层不一致（后端）

- 执行层（`service/tool_registry.go:1064` 起）读取 `title`/`description`/`priority`/`type`/`category`/`requester_id`/`assignee_id`/`ci_id`。
- `ArgsSchema` 只声明 `ci_id`，且无 `required` → LLM 无从得知 `title` 必填 → 审批通过后执行必然失败。
- 修复：`ArgsSchema` 补齐全部可执行字段 + `required: ["title"]`，`Description` 明确 title 必填。
- 回归锁：`TestCreateTicketArgsSchemaExposesExecutableFields`。

### 缺陷 B：审批卡请求风暴 + 瞬时失败被谎报为过期（前端）

- `ToolApprovalCard` 的 `load` 以 `onLoaded` 为依赖，而 `ChatMessageItem` 传入内联闭包，每次渲染都是新引用；
  `onLoaded` 内 `setDetailsById` 触发父组件重渲染 → 新闭包 → `load` 重建 → `useEffect` 再次拉取，形成无限循环。
- 失败后 `catch` 一律置为 `expired`，把 429/5xx/网络错误都显示成「已过期/不可用」。
- 修复：
  1. 回调改由 `useRef` 持有，移出 `useCallback` 依赖（`load` 仅随 `invocationId`/`now` 变化）；
  2. 新增 `unavailable` 态：仅 `httpStatus===404` 归为 `expired`，其余为「暂时无法获取状态」并可重试。
- 回归锁：新增 2 例（429 不得谎报过期、父组件内联回调变化不得重复拉取）。

## 4. 修复后实测（tbG2，版本 2926ddc8）

| 观察点 | 实测结果 |
|---|---|
| 工具调用参数 | ✅ 抽屉脱敏参数含 `title`/`description`/`priority`/`type` |
| 待审批 | ✅ invocation `#34`，`approvalState=pending`，`status=pending` |
| 批准 | ✅ `approval_state=approved`、`approved_by=1`、`approved_at=2026-10-19 04:54:41Z` |
| 执行 | ✅ `status=done`、`error_code` 空 |
| 业务落库 | ✅ 工单 `id=66`、`TKT-202610-000065`、`status=new`、`priority=low`、`type=incident` |
| 卡片收敛 | ✅ 「已处理」+「该请求已处理（#34）」 |
| 请求次数 | ✅ 卡片挂载到批准前仅 **1** 次 `GET /agent/tools/34`（修复前同秒 20+） |
| 前端控制台 | ✅ 0 console errors |

DB 证据：

```
 id | tool_name     | status | approval_state | approved_by | error_code | error
 33 | create_ticket | failed | approved       |           1 | internal_error | create_ticket: title is required
 34 | create_ticket | done   | approved       |           1 |                |
```

```
 id | ticket_number       | title                              | status | priority | type
 66 | TKT-202610-000065   | AI bot 验证-审批闭环复验-666817     | new    | low      | incident
```

## 5. 门禁

| 检查 | 结果 |
|---|---|
| `tool-approval-card.test.tsx` | 11/11 通过 |
| AI 表面回归（`src/components/ai` + `src/lib/ai`） | 48 suites / 316 tests 通过 |
| `tsc --noEmit` | 通过 |
| `eslint`（改动文件） | 0 errors（1 条既有 unused-disable 警告） |
| `go vet ./service/` | 通过 |
| `go test ./service/ -run 'TestToolRegistryWriteToolDefinitions\|TestCreateTicketArgsSchemaExposesExecutableFields'` | 通过 |
| 部署 | 双镜像 `2926ddc8` 构建 `BUILD_EXIT=0`；backend/worker/frontend `Up (healthy)`；`/api/v1/version` 对齐 |
| A/B 指纹 | 前端「暂时无法获取审批状态」旧镜像 0 → 新镜像 1 |

## 5.1 回归走查（`abd8705a`，2026-10-18）

修复部署后重新走查 Dock / 帮助 / 审批页 / 工作区：

| 检查 | 结果 |
|---|---|
| `/dashboard` Dock | ✅ `data-page-key=dashboard`、面板可开、建议卡、上下文条「仪表盘」 |
| 帮助页签 | ✅ `已覆盖`、`覆盖率 100%`、文档 1 篇、请求 `GET /ai/help/page?pageKey=dashboard` 200 |
| `/ai/approval` | ✅ 页面渲染「AI 工具审批队列 / 待审批 / 工具治理 / Autopilot 运行监督」 |
| `/ai/chat` | ✅ Bot 选择器 1 个、会话导出按钮 15 个 |
| 控制台 | ✅ 0 console errors、0 失败请求 |

### 缺陷 C：帮助面板加载中谎报「未注册页面」（前端，已修复）

- 现象：已注册页面（`dashboard`）在 `/ai/help/page` 返回前，`help-status` 因 `data=null` 落入
  `STATUS_META.unknown`，首帧显示「未注册页面」；首次走查因此误判（实际随后即为「已覆盖」）。
- 修复：加载中显示「查询中」；仅 `pageKey` 确实缺失/`unknown` 时显示「未注册页面」。
- 回归锁：`AssistantHelpPanel` 新增首帧断言；浏览器专项（延迟页面帮助请求 4s）实测
  `statusDuringLoad=查询中`、`unknownDuringLoad=0`，加载完成 `statusAfterLoad=已覆盖`。

> 说明：本机 Windows 上 `go build ./...` / `go test ./service/` 全包因 MinGW 链接器 `final link failed: memory exhausted` 失败，属本地环境限制；已用 `go vet` + 定向测试 + 远端镜像构建（`BUILD_EXIT=0`）替代验证。

## 6. 测试数据

- `TKT-202610-000065`（id=66）：本次修复后验证所建，标题含「AI bot 验证-审批闭环复验-」，**可删除**。
- invocation `#33`：修复前失败记录，保留作对照。
- invocation `#34`：修复后成功记录，保留作对照。

## 7. 复现命令

```powershell
& "E:\projects\itsm\itsm\.dev\tmp\tabbit\tb2.cmd" tbG2 180000 tbG2-approval-fixed.js
```

脚本：`.dev/tmp/tabbit/tbG2-approval-fixed.js`；截图：`.dev/tmp/tabbit/tbG2-approval-fixed.png`（面板截图超时未落盘，DOM 断言与 DB 证据已足够）。
