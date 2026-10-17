# P1 · 里程碑到期通知（PE-14 收尾）设计

> 状态：**已实现 v1.0**（2026-10-23）
> 上游：`01-module-blueprint.md` 能力矩阵「里程碑」、`03-integration.md` §2.9 通知 / outbox、`06-implementation-roadmap.md` PE-14 与 §4.2 验收第 4 条
> 契约：`plan/p0-integration-contracts.md` §6 事件与通知清单（双词表）

---

## 1. 验收映射

| 验收点 | 落地 |
|---|---|
| 里程碑到期/逾期**可达**通知 | 新增租户级扫描器（`service/project_milestone_due_scanner.go`）：`planned` 且「已逾期或窗口内到期」→ 入箱 |
| **outbox 投递审计** | 复用 `notification.deliver` 命令（`operational_commands`）+ `notification_deliveries` 投递审计行；扫描轮次落 `source=job` 审计（`project.milestone_due_scan`） |
| 契约事件 `project.milestone.due` / 偏好词表 `project_milestone_due` | 后端 `dto.ListNotificationEventTypes` + 默认偏好 + 前端 `EVENT_TYPES` 与 zh/en 翻译同步（双词表归一） |
| 收件人 = PM + 干系人（目标租户内） | `projects.manager_id`（唯一权威）∪ 项目内 `pm`/`stakeholder` **active** 成员，去重；入箱时校验收件人为目标租户活跃用户（fail-closed） |
| 不重复打扰 / 重复扫描幂等 | 幂等键 = 里程碑 + 目标日期 + 触发类型；`operational_commands(tenant_id, command_type, idempotency_key)` 唯一索引兜底 |

## 2. 决策

| 编号 | 决策 | 理由 |
|---|---|---|
| D-23 | 里程碑到期通知 = **扫描入箱（outbox）+ 既有投递链路**，不做 fire-and-forget；幂等键 = `project_milestone_due:<milestoneId>:<dueDate:yyyymmdd>:<due_soon\|overdue>`；收件人 = `manager_id` ∪ 项目内 `pm`/`stakeholder` 活跃成员；终态项目与 `reached`/`missed` 里程碑不提醒 | 与 `bpmn_timeout_scanner` 同模型（可靠投递 + 幂等收敛 + job 审计）；`manager_id` 是 PM 唯一权威（契约 §3）；同一触发在其生命周期内只投递一次，避免 30 分钟一轮造成打扰 |

## 3. 扫描器（`itsm-backend/service/project_milestone_due_scanner.go`）

- **触发条件**：`project_milestones.status=planned` 且 `due_date <= now + 窗口`（默认 72h = 提前 3 天，`ITSM_PROJECT_MILESTONE_DUE_WINDOW_HOURS` 可覆盖）。
- **项目过滤**：项目状态必须为 `planning|active|suspended`（终态项目不再提醒；项目缺失/跨租户跳过）。
- **事件分型**：`due_date < now` → `overdue`（逾期）；否则 `due_soon`（临期）。正文含项目名、里程碑名、目标日期。
- **收件人解析**：`resolveRecipients`（`projects.manager_id` + 项目内 `pm`/`stakeholder` 活跃成员 + **归口团队负责人** `teams.manager_id`（PE-17 修订，团队软删/停用自动降级不计入），去重）；无收件人则跳过。收件人是否为目标租户活跃用户由 `EnqueueResourceNotificationTx` 校验（不合法 → 单条告警跳过）。
- **幂等**：`OccurrenceKey` 参与 `idempotency_key`（含 resourceType/channel/recipient/type 后缀）；唯一索引冲突按「已投递」收敛（不报错、不重复计数）。调期后（`due_date` 变化）视为新触发，可再次提醒。
- **审计与韧性**：本轮有入箱时落 `RecordJobAudit`（`component=project-milestone-due-scanner`、`action=project.milestone_due_scan`）；单轮上限 500 条；单条失败仅告警不中断整轮；`tenantctx.EnsureJobTenant` 防跨租户执行。

## 4. 投递（`itsm-backend/service/notification_delivery_command_handler.go`）

- 新增资源类型 `project_milestone`：按 `(tenant_id, id)` 加载里程碑（不存在 → 命令失败重试），动作链接指向所属项目工作台 `/projects/:id`，动作文案「查看项目里程碑」。
- `in_app` 渠道：写通用 `notifications` 行（标题=事件类型、正文=内容、actionURL/actionText）+ `notification_deliveries` 审计（`status=sent`）；项目通知不写 `ticket_notifications` 行。
- **偏好匹配**：`type=project_milestone_due` 即偏好词表长词表（无需归一化）；用户在某渠道显式关闭 → 命令成功且不落投递记录（与既有「按用户选择跳过」语义一致）。

## 5. 接线（`itsm-backend/internal/bootstrap/app.go`）

- `safeGo("project-milestone-due-scanner")`：30 分钟 tick；`tenantctx.SystemContext` 枚举租户后逐租户调用（每租户注入 `tenant_id`）。
- 环境变量 `ITSM_PROJECT_MILESTONE_DUE_WINDOW_HOURS`（默认 72）控制提醒窗口。

## 6. 前端词表

- `src/pages/(main)/notifications/index.tsx` `EVENT_TYPES` 增加 `project_milestone_due`（偏好页可配置）；
- `src/lib/i18n/translations.ts` zh/en 同步 `projectMilestoneDue` / `projectMilestoneDueDesc`（与后端 `dto.ListNotificationEventTypes` 同一词表）。

## 7. 非目标（后续）

- 任务级 `project_task_due_soon` / `project_task_overdue`（契约 §6 已预留词表；本案只做里程碑）；
- 邮件/IM 渠道默认开启与升级链路（沿用偏好表，默认 in_app 开、push/邮件按用户偏好）；
- 甘特/资源视图与 P2 报表、AI 批次统一评估。

## 8. 验证证据

| 项 | 命令 / 证据 | 结果 |
|---|---|---|
| 扫描器单测 | `go test ./service/ -run TestProjectMilestoneDueScanner -count=1 -v`（4 用例：PM+干系人收件/普通成员排除、重复扫描幂等、终态项目与跨租户跳过、非法租户） | ✅ 4/4 |
| 投递单测 | `go test ./service/ -run "TestNotificationDeliveryHandler" -count=1`（新增里程碑 in_app 投递 + 偏好关闭跳过；既有用例回归） | ✅ |
| 词表归一回归 | `go test ./service/ -run TestNormalizeNotificationEventType -count=1` | ✅ |
| 编译 | `go build ./internal/bootstrap/ ./service/ ./dto/` | ✅ |
| 前端 | `npx tsc --noEmit`、改动文件 `eslint`、`notifications` 页单测 | ✅ |
| 文档链接 | `docs/project-management/**` 链接检查 | ✅ |
| 真实库链路（P1 收口） | embedded-postgres（55432）+ 演示数据：扫描入箱 **2** → 重复扫描 **0**（幂等）→ 投递 **2/2** → `notifications` / `notification_deliveries` 落库（临时 harness：置于 `itsm-backend/` 内 `go run`，源码留档 `.tmp/pgverify/pe17-smoke-main.go`） | ✅ |
| 归口候选与降级 | `go test ./service/ -run TestProjectMilestoneDueScanner`（含归口团队负责人收件 + 团队软删降级用例） | ✅ |
