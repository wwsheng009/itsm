# I2-01 Autopilot 监督面（暂停/回滚/审批联动） · 交付证据

- 日期：2026-10-18
- 任务：I2-01（方案 §3.4 Autopilot 形态｜验收：与 `/ai/approval`、通知中心联动）
- 状态：`integration_verified`（运行列表 + 暂停 + 回滚（冻结待审批 + 通知中心）+ 审批按运行收敛；页面接线 + 测试全绿）
- 交付物：
  - 后端动作面：`bot.RunStore.CancelRun`（仅 running 可取消、条件更新幂等）+ `bot.Manager.Cancel`
  - 后端数据面：`bot.TraceService.ListRuns`（倒序运行列表 + 步骤/工具/待审批/产物计数，批量 RunIDIn 免 N+1）
  - 后端编排：`handlers/ai` `ListAutopilotRuns` / `CancelAutopilotRun` / `RollbackAutopilotRun`
    （回滚 = 终止运行 + 冻结该运行全部待审批写动作 `cancelled`（永不执行）+ 拒绝原因回填会话 + 通知中心投递）；
    `ToolInvocationFilter.RunID` + `?runId=` 审批联动过滤
  - 端点：`GET /ai/runs`（ai:read）、`POST /ai/runs/:id/cancel`、`POST /ai/runs/:id/rollback`（ai:write）+
    `GET /agent/tools/invocations?runId=`（ai:read）+ `authz-gen` 预检再生
  - 前端：`aiListAutopilotRuns/aiCancelAutopilotRun/aiRollbackAutopilotRun` +
    `/ai/approval` 页「Autopilot 运行监督」卡（运行列表/计数/状态过滤/暂停/回滚/轨迹深链/
    通知中心入口/「查看待审批」→ 按 runId 收敛并显示可清除过滤 chip）

## 1. 形态与治理口径（§3.4：不做"无感自动写"）

| 动作 | 语义 | 边界 |
|---|---|---|
| 暂停（cancel） | running → cancelled（写 finished_at + error_code） | 仅 running 生效；已收口/跨租户 → `cancelled=false`（幂等，不报错） |
| 回滚（rollback） | 终止运行（可已收口）+ 该运行全部 `pending` 确认单 → `cancelled`（`NormalizeConfirmationState` 终态，后续无法再批准/执行）+ 结果回填会话 | 已执行的写动作不回滚（由业务侧/Inline undo 负责）；不静默 |
| 审批联动 | 监督面「查看待审批」→ `?runId=` 收敛审批队列；行内可跳轨迹回放 | 审批仍按既有 `/agent/tools/:id/approve`（ai:write） |
| 通知中心联动 | 回滚写 `notifications`（`type=ai_autopilot`、深链 `/ai/approval`）：活跃管理员优先，无管理员回退操作人 | 与 I1-03 徽标链路同源（`ai_*` 类型计入 FAB 徽标） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l service/bot/run.go service/bot/trace.go handlers/ai/autopilot.go handlers/ai/service.go
go test ./service/bot/ -run 'TestRunStore_CancelRun|TestTraceService_ListRuns' -count=1
go test ./handlers/ai/ -run 'TestAutopilot' -count=1
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1   # 5 包全绿
go run ./cmd/authz-gen   # 再生：/ai/runs → ai:read；/ai/runs/*/cancel|rollback → ai:write

cd ../itsm-frontend
npx jest --testPathPattern "ai/approval" --coverage=false --watchAll=false --runInBand  # 2 suites / 11 tests
npx jest src/components/ai src/lib/ai src/lib/store src/lib/api/__tests__/ai-api.test.ts --runInBand  # 37 suites/302 tests
npx tsc --noEmit ; npx eslint "src/pages/*/ai/approval/*" src/lib/api/ai-api.ts
```

关键断言：
- 取消：running → `cancelled` + `finished_at` 落库；重复取消/已收口/跨租户 → `false` 且不改他租户状态；nil 服务 → 明确错误；
- 列表：仅本租户；按 started_at 倒序；计数正确（2 步骤 / 2 工具 / **仅 pending 计待审批** / 1 产物）；状态过滤与 7 天窗口生效；空租户 → 空列表；
- 回滚：`cancelled=true` + `rejectedInvocations=1` + `notifications=1`；被冻结确认单 `approval_state=status=cancelled`、reason/approved_by 落库；
  通知行 `type=ai_autopilot`、`action_url=/ai/approval`、回退收件人=操作人（无管理员时）；
- 端点：列表/过滤/非法状态 400；`?runId=` 联动只回该运行待审批、非法 runId 400；未装配 → 503（列表与取消）；
- 前端：监督卡渲染 run#/计数/待审批高亮/轨迹链接；暂停→`aiCancelAutopilotRun(id, 'paused_from_supervision')` 且刷新列表；
  回滚→`aiRollbackAutopilotRun(id, ...)`；「查看待审批」→ `aiGetToolApprovals('pending', {provider:'',server:'',runId:21})` + 过滤 chip；
  无 `ai:write` → 不渲染暂停/回滚（负向），仍可只读监督与查看待审批。

## 3. 附带修复（非本批回归，登记）

`ai/approval/__tests__/b1-09.test.tsx` 的权限 mock 为全 `false`，而页面「通过/驳回」按钮由 `ai:write` 门控（HEAD 起即如此）
→ 该用例在 HEAD 已失败（既有测试问题）。本次将其 mock 收敛为仅放开 `ai:write`（`mcp:admin` 保持 false），
用例恢复通过且语义更贴近「验证时限行为」；其余既有问题（TicketDetailAssignSearch、tickets/types）不在本批范围。

## 4. 条件项（不阻塞）

- 定时/批量 Autopilot 任务的**触发面**（调度器、批量草稿）不在本批：本次交付的是监督与闸门（暂停/回滚/审批/通知）；
- 暂停不中断已进入 LLM 流的单个请求（取消的是运行档案与后续写动作；真实流中断依赖前端停止 + 运行收口），
  流级中断（context cancel 注册表）归后续；
- 已执行写动作的自动回滚不实现（需业务语义/Inline undo），保持"不做无感自动写"边界；
- 浏览器 E2E（监督面暂停/回滚 → 通知中心/审批队列联动）归联调。