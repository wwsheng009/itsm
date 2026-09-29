# 通知模块设计方案（借鉴 ai-gateway）

> 状态：**Confirmed v0.4**（2026-09-29 按最佳实践确认：自检 + 独立对抗式评审；D1 运维配置为 P0-1 前置）｜日期：2026-09-29｜范围：站内信 / 邮件 / IM 与 Webhook 渠道 / 偏好 / 模板 / 投递可靠性 / 审计与可观测
> 关联：[multi-tenant 方案 Q4 邀请邮件](./../multi-tenant/plan/msp-user-lifecycle-and-tenant-switching-plan.md)（§3 F4、§9.2 Q4）、[Operational Command / Outbox 基座](../architecture/operational-command-outbox.md)、[领域所有权与迁移边界](../architecture/domain-ownership.md)（"通知：多渠道统一走 `notification.deliver`"）
> 参考实现：`E:\projects\ai\ai-gateway` 通知模块（证据见 §3 与附录 A）
> 目的：① 借鉴 ai-gateway 的完整通知模块；② 在 ITSM 既有 outbox 基座上补齐"通知模块"的完整能力面；③ 回答 multi-tenant 方案 Q4（邀请邮件通道）。

---

## 0. 摘要（TL;DR）

**现状一句话**：**底座强、上层弱**。可靠投递基座（`operational_commands` outbox：租约/围栏/幂等/退避/死信/重放 API）已是企业级；但通知模块的上层——**渠道实现（邮件未接线）、模板、偏好粒度、可观测、测试覆盖**——明显不足，且存在"看起来有、实际不发"的隐性缺口。

**目标**：不推翻既有基座，在 `notification.deliver` 之上补齐"完整通知模块"：

1. **渠道**：站内信（已有）+ 邮件（**接线 + 双轨**：平台 SMTP 用于邀请/密码重置，租户 connector 用于业务通知）+ IM/Webhook（已有 connector）+ 短信（预留）；
2. **渲染**：模板化（事件类型 → 渠道 → 模板/语言），正文不再由调用方手拼字符串；
3. **偏好**：事件类型 × 渠道 × 用户/租户默认值，含"必须送达"的白名单（安全类通知不可关闭）；
4. **可靠性**：沿用 outbox 语义，补齐**每渠道限流、去重窗口、优先级/降级**；
5. **安全**：凭据不落 payload（已约定）、目标地址运行时解析（已约定）、审计与脱敏；
6. **可观测**：投递成功率/失败分类/最老等待/死信数看板 + 前端"通知中心"状态一致。

**关键决策（详见 §9）**：D1 平台 SMTP 与租户 connector 的双轨边界；D2 模板存储（DB vs 代码）；D3 偏好粒度与"必须送达"清单；D4 是否引入通知聚合（digest）；D5 短信通道是否本期纳入。

---

## 1. 目标与范围

### 1.1 目标读者与适用场景

- **产品/运营**：理解通知能力边界与配置项（哪些能关、哪些必达）；
- **后端开发**：按统一契约新增事件类型/渠道，不再各自拼装发送逻辑；
- **运维**：SMTP/connector 配置、投递失败排查、死信重放；
- **安全/合规**：审计与 PII 边界。

### 1.2 目标（可验收）

| # | 目标 | 验收口径 |
|---|---|---|
| G1 | 任何业务通知**只经** `notification.deliver` 出箱，禁止请求内直发 | 静态检查 + 代码评审清单；现有直发路径全部移除或标记废弃 |
| G2 | 邮件通道**真正可用**（当前未接线） | 配置 `smtp.enabled=true` 后：邀请邮件、密码重置邮件、租户业务通知均可达；未配置时明确降级并可见 |
| G3 | 通知内容**模板化** | 新增事件类型不改代码即可上线（模板表/文件 + 预览接口） |
| G4 | 偏好**可控且可审计** | 用户可关非必达渠道；"必达"事件清单由平台配置并审计变更 |
| G5 | 投递**可观测** | 成功率、失败分类、重试、死信、最老等待指标 + 运维重放（复用现有 API） |
| G6 | **租户隔离** | 所有查询/投递带 `tenant_id`；跨租户目标解析 fail-closed（已有约定） |
| G7 | 测试覆盖 | 每个渠道至少 1 条"入箱 → 出箱 → 投递（mock）→ 审计"E2E；outbox 故障注入用例 |

### 1.3 范围

**包含**：站内信、邮件（平台 + 租户）、IM/Webhook（飞书/钉钉/企微/通用 Webhook）、偏好、模板、投递可靠性与可观测、审计、运维控制面。

**不包含（本期）**：短信通道实现（预留接口与表结构）、通知聚合/摘要（digest，见 D4）、移动推送（APNs/FCM）、营销类群发、外部工单系统双向同步。

### 1.4 设计原则

1. **复用基座，不造第二条链路**：一切外部投递必须走 `operational_commands` + `notification.deliver`（`docs/architecture/domain-ownership.md` 已定门禁）；
2. **事务内入箱**：业务变更与命令同事务提交，禁止"先提交业务，再尽力 enqueue"；
3. **凭据与目标不落 payload**：payload 只存引用与不可变参数；邮箱/手机号/open_id 由 Handler 在租户范围内实时解析（既有约定）；
4. **fail-closed**：解析不到目标、租户不匹配、渠道未配置 → 明确失败/降级并留痕，绝不静默丢弃或跨租户兜底；
5. **幂等优先（B4 修正）**：以业务 occurrence key 派生幂等键——**命令级**保证同一业务事件最多一条命令；**渠道级**去重取决于 Provider 能力（§4.6 幂等矩阵），SMTP 等无幂等渠道为至少一次，重复经 `status=unknown` 暴露并人工确认；
6. **可解释**：每次投递可从 `notification_deliveries` 还原"为什么发/为什么没发/发给了谁（掩码）/重试了几次"；
7. **平台与租户分层**：平台级通道（邀请、密码重置、安全告警）与租户级通道（业务通知）显式分离，配置与审计各自独立。

---

## 2. 现状盘点（ITSM）

> 证据来自只读调研（2026-09-29），格式 `file:line`；标注"未找到"的条目为已检索未命中。

### 2.1 基座：`operational_commands` outbox（**强项，保留**）

| 能力 | 现状 | 证据 |
|---|---|---|
| 状态机 | `pending → processing → succeeded`；`retry` 回 `pending`；超限 `dead_letter` | `docs/architecture/operational-command-outbox.md:9-14` |
| 调度 | `available_at` + 指数退避（`min(2^attempt,300)s`） | `internal/commandbus/commandbus.go:298-326` |
| 租约与围栏 | `lease_owner/lease_expires_at/fencing_token`，Worker 按租约 1/3 心跳；领取时 `attempt+1` | `commandbus.go:193-229, 267-296` |
| 幂等 | `(tenant_id, command_type, idempotency_key)` 唯一；`Enqueue/EnqueueTx/EnqueueSQLTx` 支持与业务同事务 | `commandbus.go:98-136`；`ent/schema/operational_command.go:16-42` |
| 死信与重放 | 运维 API：列表/详情/重放（保留 attempt 与幂等键、增 fencing）/取消，均与 `audit_logs` 同事务 | `operational-command-outbox.md:78-87` |
| 装配 | registry + worker（owner=hostname），仅 `ProcessModeWorker|All` 启动；handler 注册于 bootstrap | `internal/bootstrap/app.go:444-463, 525-526, 1706-1708, 1763-1765` |

结论：**投递可靠性底座已达到企业级**，通知模块不应另起链路。

### 2.2 生产链路：入队 → 出队 → 投递

**入队（业务侧）**

- 统一入口 `EnqueueResourceNotificationTx`（事务内）：校验收件人在**事务租户内 Active**；幂等键 `occurrenceKey:resourceType:resourceID:channel:recipientID:type`；`MaxAttempts=8`（`service/notification_outbox.go:25-58`）；
- 业务变体：`NotifyTicketCreatedTx` / `NotifySLABreachedTx` / `NotifySLAAlertLevelChangedTx` / `NotifyChangeApprovalRequiredTx` / `NotifyChangeApprovalDecidedTx`（`service/ticket_notification_service.go:598, 621, 661, 703, 740`）；
- 开关：`EnableOutbox`（兼容外部触发）/`EnableTxOutbox`（事务路径，**未启用时 fail-closed**）（`:36-41`）；链路语义有测试（`service/change_approval_notification_tx_test.go:72-102, 106-146, 150`）。

**出队（Worker）**：1s tick；领取最早可执行命令；未注册 handler → fail（`commandbus.go:174-264`）。

**投递 Handler**（`service/notification_delivery_command_handler.go`）

| 步骤 | 行为 | 行号 |
|---|---|---|
| 幂等前置 | 同命令已有 `sent` 投递记录 → 直接返回 | `:35-43` |
| 资源/收件人解析 | ticket/change/service_request；收件人须租户内 Active | `:45-92` |
| 偏好消费 | 无偏好=放行；查询失败=放行+Warn；显式关闭=**标记命令成功**、不写投递记录 | `:94-114` |
| 词表归一化 | `created→ticket_created`、`sla_breached→sla_violated`、`change_approval_decided→approval_completed` 等 | `:125-150` |
| 渠道分流 | `in_app` → `deliverInApp`；其余 → `deliverConnector` | `:116-119` |
| in_app 投递 | 单事务写 `ticket_notifications(sent)` + `notifications` + `notification_deliveries(sent)`；约束冲突视为已投递 | `:181-219` |
| 外部渠道投递 | 先落 pending 投递记录 → `connectorManager.Send`（`Message.ID = cmd.IdempotencyKey`）→ 成功 `sent` / 失败 `failed`+安全错误分类 | `:221-291` |
| 目标解析 | feishu→`FeishuOpenID`；dingtalk/wecom→`Username`；email→`Email`；sms→`Phone`；**webhook→固定串 `"tenant-default-webhook"`** | `:317-332` |

### 2.3 数据模型（现存）

| 表/实体 | 用途 | 关键点 |
|---|---|---|
| `notifications` | **读侧**站内通知主表 | `title/message/type(默认 info)/read/action_url/action_text/user_id/tenant_id`；**无 channel/status/source**（`ent/schema/notification.go:18-41`） |
| `ticket_notifications` | 工单通知（渠道+状态） | `type/channel(email, in_app, sms)/status(pending,sent,read)/sent_at/read_at`（`ent/schema/ticket_notification.go:19-49`） |
| `notification_deliveries` | **投递审计** | `operational_command_id` **唯一**；`channel/target_masked/status/attempt/provider_message_id/error_code/error_message`；3 个租户维度索引（`ent/schema/notification_delivery.go:16-39`） |
| `notification_preferences` | 用户偏好 | `user × event_type` 唯一；4 个渠道 bool + `frequency` + `quiet_hours_*` + `timezone`（`ent/schema/notification_preference.go:19-58`） |
| `operational_commands` | outbox | 见 §2.1 |
| `email_outbound_messages` | 工单邮箱出站 | 唯一 `(tenant_id, conversation_id, reply_type, revision)`（`ent/schema/email_outbound_message.go:16-45`） |

**枚举散落**：渠道常量仅 4 个（`common/constants.go:258-263`）；命令/投递状态为字符串字面量（`internal/commandbus/commandbus.go:20-25` 与 handler 内）。

### 2.4 渠道与 connector

- 内置渠道（自注册）：`console`、`dingtalk`、`feishu`、`wecom`、`webhook`、`email`（IMAP/SMTP 工单邮箱，能力 `CapSendMessage/CapReceiveMessage/CapReplyMessage/CapHealthCheck`）；**无 sms connector**（`connector/builtin/`）；
- `Manager.Send`：按 `tenantID+name+Enabled` 取实例，未装配 → `connector %q not provisioned for tenant %d`；成败均记录 `lastSuccessAt/lastFailureAt/lastError`（`connector/manager.go:125-134, 177-205`）；`Provision` 失败即 `Revoke`（fail-closed，`:72-106`）；
- 凭据：`CONNECTOR_CONFIG_ENCRYPTION_KEY`（<16 字符报错）加密存 `connector_configs.encrypted_credentials`，解密失败 fail-closed（`connector/persistent_store.go:20-27, 43-53, 79-89`）；
- 注册门禁：manifest（name/version/required_permissions）+ checksum，重复注册 panic（`connector/registry.go:39-53`）。

### 2.5 偏好：语义与漂移

- 粒度 `user × event_type`，渠道 4 bool，默认 `email=true / sms=false / in_app=true / push=false`，`frequency=immediate`、`timezone=UTC`（`ent/schema/notification_preference.go:19-58`；`service/notification_preference_service.go:85-90, 250`）；
- 消费端只读 4 bool：email/in_app/sms 各自开关；**feishu/dingtalk/wecom/webhook/slack 统一按 `push_enabled`**（`notification_delivery_command_handler.go:154-179`）；
- **`quiet_hours_*` 与 `frequency` 仅存储、无消费点**（全仓未检索到投递侧读取）；
- **三套事件词表漂移**：DTO 15 项含 `change_approved/change_rejected`（`dto/notification_preference_dto.go:50-67`）；handler 归一化产出 `approval_completed`（`notification_delivery_command_handler.go:143-146`）；schema 注释仅 10 个短词（`ent/schema/notification_preference.go:26`）；
- 后端路由：`GET/POST/PUT/DELETE /notification-preferences*`（`router/common_system_routes.go:186-196`）；**前端 API 漂移**：`/me`、`/:userId`、`/templates`、`/me/apply-template` 后端未注册（`itsm-frontend/src/lib/api/notification-preference-api.ts:71, 78, 111, 118`）。

### 2.6 读侧与前端

- API：`GET /notifications`、`GET /unread-count`、`PUT /:id/read`、`PUT /read-all`、`PUT /batch/read`、`DELETE /batch`、`DELETE /:id`、`POST ""`（`router/common_system_routes.go:201-211`）；Swagger 与实际方法不一致（`handlers/notification/handler.go:92` 写 `POST /{id}/read`）；
- 未读数：`notifications.read=false` 计数（`service/notification_service.go:193-205`）；
- WebSocket：票据机制存在（`router/router.go:426-435, 437-455`），但 **`SendToUser` 仅用于工单事件**（`service/websocket_service.go:170` 调用点 `:372-443`），**不推送"新通知"**；
- 前端：`NotificationCenter.tsx` 轮询列表（`:147-177`），已读/全读/删除为真实 API（`:185-215`）；**模板新建、渠道保存、"测试渠道"均为本地 state 假功能**（`:232-239, 249-270, 272-279`）；`NotificationDrawer` 纯展示；`useNotification` 只是 antd 别名；
- 已知问题：通知 generation 任务未运行、未读陈旧（`docs/archive/testing-reports/browser-button-functional-test-report-2026-08-02.md:164`）；通知 controller 测试缺口与口径矛盾（`docs/testing/controller-failing-list.md:70, 123`）；`docs/archive/capability-matrix.md:131` 标"站内通知 ✅ 实时推送"与实现不符。

### 2.7 邮件：**平台级通道缺失**（Q4 根因）

- `service/email_service.go` 实现了 `Send/SendTemplate/SendTicketNotification/SendSLANotification/SendPasswordResetEmail`，但 **`NewEmailService(` 无生产调用点、`SetEmailService(` 无调用方** → 未接线；
- 直接后果：`handlers/auth/service.go:234-238` 的密码重置邮件被静默跳过；`service/sla_alert_service.go:529-556` 的 critical 直发邮件分支静默失效（且该分支**绕过 outbox**）；
- 在用邮件路径：**租户级 email connector**（工单邮箱，需账号/授权码/IMAP/SMTP 配置）+ `email_outbound_messages` 出站表（`email_intake`）；
- 平台级邮件（邀请/密码重置/安全告警）**无归属通道**——正是 multi-tenant 方案 Q4 的未决问题。

### 2.8 缺口清单（编号）

| # | 缺口 | 严重度 | 证据 | 建议动作 |
|---|---|---|---|---|
| N1 | 平台级邮件通道未接线（`EmailService` 无实例化/注入） | **P0** | `service/email_service.go:54`；`handlers/auth/service.go:40`；`service/ticket_notification_service.go:52` | bootstrap 构造并注入；`smtp.enabled` 开关 |
| N2 | 密码重置邮件静默跳过（用户拿 token 收不到信） | **P0** | `handlers/auth/service.go:234-238` | 随 N1 修复；未配置时返回明确状态 |
| N3 | SLA critical 直发绕过 outbox（与门禁冲突） | **P0** | `service/sla_alert_service.go:529-556` | 改走 `notification.deliver`；删除直发分支 |
| N4 | `TicketNotificationService` 保留旧同步路径，且存在 **N×N 重复发送缺陷** | **P0** | `service/ticket_notification_service.go:92-211`（`:154` 内层重复遍历 `req.UserIDs`） | 删除旧路径；生产强制 Tx outbox |
| N5 | 读侧 `notifications` 与投递 `notification_deliveries` 两套模型，站内信由 worker 补写 | P1 | `ent/schema/notification.go`；`notification_delivery_command_handler.go:181-219` | 明确读写模型边界；补 `source/status` |
| N6 | **无通知模板实体**；前端模板/渠道/测试为假功能 | P1 | `ent/schema` 无 `notification_template*`；`NotificationCenter.tsx:232-279` | 引入模板表 + 渲染 + 管理 API；前端接线 |
| N7 | 偏好三套事件词表漂移 + `quiet_hours/frequency` 未消费 | P1 | `dto/notification_preference_dto.go:50-67`；`notification_delivery_command_handler.go:143-146`；`ent/schema/notification_preference.go:26` | 统一事件目录（单一事实源）+ 消费静默时段 |
| N8 | WS 不推送通知（仅工单事件），前端只能轮询；实测未读陈旧 | **P0** | `service/websocket_service.go:170, 372-443`；`NotificationCenter.tsx:147-177`；`docs/archive/testing-reports/browser-button-functional-test-report-2026-08-02.md:164` | 通知落库后推送（或轮询 + 失效标记） |
| N9 | 无 sms connector，但偏好可开 `sms_enabled`、目标解析支持 `Phone` → **开启即必失败**；`webhook` 目标硬编码 `"tenant-default-webhook"` | P1 | `connector/builtin/`；`notification_delivery_command_handler.go:317-332` | 补 SMS provider 或在偏好层禁用该渠道；webhook 目标按租户解析 |
| N10 | 通知相关 controller/service **无测试** | P1 | `docs/archive/生产就绪审计报告-2026-07-12.md:58`；`docs/testing/controller-failing-list.md:70` | 按 §8 补 E2E 与故障注入 |
| N11 | 文档与实现不符（能力矩阵"实时推送 ✅"） | P2 | `docs/archive/capability-matrix.md:131, 135` | 修正能力矩阵；登记 v1.1 通知中心重构范围 |
| N12 | 枚举散落、遗留表 `user_notification_preferences` 与现行表并存 | P2 | `internal/commandbus/commandbus.go:20-25`；`migration/migrations.go:131-144` | 集中枚举；清理遗留迁移 |
| N13 | 投递审计与 connector 调用**非同事务**：先提交 `pending` 记录再发送，崩溃窗口留下悬空 pending | P1 | `notification_delivery_command_handler.go:229-263` | 补偿扫描（超时 pending 重新入队/对账） |

---

## 3. ai-gateway 通知模块参考剖析

> 证据来自只读调研（`E:\projects\ai\ai-gateway`，2026-09-29）；格式 `file:line`。仅列与 ITSM 设计相关的部分。

### 3.1 模块地图

| 模块 | 职责 | 关键证据 |
|---|---|---|
| `internal/service/notification/` | **核心引擎**：事件 → 分发 → 渲染 → 投递 → 收件箱；`Emit()` 写 `notification_events` → `createDispatches` → **同请求内联** `processDispatch`；另有 15s ticker 轮询 `ProcessPendingDispatches(20)` | `service.go:22, 241, 503-539`；`delivery.go`（目标解析/渲染/发送/`failDispatch`）；`helpers.go`（去重/重试/脱敏） |
| `internal/service/usernotification/` | 用户偏好/订阅 + **邮件候选入箱（只入箱不发送）** + readiness；`RouteObservedEvent()` 纯函数匹配；`MutateSubscriptions` 事务内 upsert | `router.go:70`；`email_dispatch.go:144`（注释明确"deliberately does not process…"）；`preferences.go:29`；`readiness.go:35`；`service.go:81` |
| `internal/gateway/resourcegovernance/notifications/` | **outbox + dispatcher + job_runner 参考实现**（去重窗/静默窗、lease、行锁重领） | `outbox.go:116`；`dispatcher.go:99`；`job.go:41`；`job_runner.go:137`；`gorm_repository.go:24, 269` |
| `internal/service/aisitereviewnotification/` | 策略驱动的审核通知（policy + recipients） | `service.go:16-23, 107` |
| `internal/service/private_message/` | 私信（线程/成员/回执/审计；与通知分属两套） | `service.go:32-36`；迁移 045 |
| Admin/Portal API/UI | 管理侧 channels/templates/events/dispatches/retry/inbox；用户侧 inbox/unread/preferences | `router/gin.go:3247-3260, 3318-3321, 3885-3891`；`frontend/src/pages/admin/Notifications.tsx` 等 |

### 3.2 数据模型（可借鉴的结构）

| 表 | 关键设计 |
|---|---|
| `notification_events` | `event_type/source_type/source_id/severity/payload/occurred_at/dedupe_key`（**dedupe_key 仅普通索引，无唯一约束**） |
| `notification_dispatches` | 状态 `pending/sending/delivered/failed/skipped`；`attempt_count`、`next_retry_at`、`rendered_*`、`delivery_target` |
| `notification_inbox_items` | `recipient/category/priority/read_at/archived_at/expires_at` + 4 个部分索引（含未读汇总 `WHERE read_at IS NULL`） |
| `notification_channels` | `name` 唯一、`channel_type`、`enabled`、`scope`、`config/secret_config/capabilities`、`last_test_status`；**无 tenant_id**（平台单租户假设） |
| `notification_templates` | `name` 唯一、`event_types`、`channel_type`、`title/body` 模板、`format`、`variables_schema`、`version` |
| `notification_route_states` | 投递时按 `channel_id + route_key + enabled` 解析并校验 `expires_at`；含 `State/SecretState/TargetRef` |
| `user_notification_subscriptions` | 唯一 `(user_id, scope_type, scope_id, event_type, category) WHERE deleted_at IS NULL`；`channels jsonb`、`frequency`、`min_severity`、`mute_until`、`quiet_hours`、`conditions` |
| `resource_governance_notification_outbox` | `outbox_id` 唯一、**`dedupe_identity` 部分唯一**、`lease_owner/claimed_at/leased_until`、`attempt jsonb/attempt_number/attempt_status` |

### 3.3 投递管线与可靠性

- **两条入队路径（事务边界不一致）**：核心 `Emit` 非事务、请求内直发；AI Sites 路径在 distribution **事务内**写 inbox + 邮件 dispatch，交给后台轮询（`distribution.go:188-215`）；
- **dispatcher**：取 `status=pending OR (failed AND next_retry_at<=now)`，`created_at` 升序、批量 20（`service.go:2032-2061`）；`processDispatch` 状态机 render→resolve→`sending`+attempt+1→`deliver`→`delivered`，失败走 `failDispatch`（`delivery.go:215-275`）；
- **重试**：`defaultMaxAttempts=3`、`defaultRetryDelay=5m`，退避 `attempt<=1→5m，否则 attempt*5m`；超限终态 `failed`——**没有 dead_letter 状态**（`service.go:21-24`；`helpers.go:602-607`；`delivery.go:945-965`）；手动重试 API 拒绝 `delivered/sending/skipped`（`service.go:675-682`）；
- **RG 实现更完整（同一仓库）**：dedupe/silent 双窗口 + 抑制原因枚举（`dedupe_window/silent_window/sink_error`）（`dispatcher.go:26-33, 99-130`）；永久失败→`failed`、attempt 耗尽→**`dead_letter`**（`job_runner.go:199-224`）——即"核心引擎缺的，RG outbox 有"（借鉴 R5 的落地样本）；
- **并发**：核心引擎为单进程 ticker 串行，**无 claim/限流**（`rate_limit_per_hour` 仅透传字段），多实例存在重复发送风险；RG outbox 则有 lease + `FOR UPDATE` 行锁 + 重领（`gorm_repository.go:269`；`job.go:41-71`）——**同一仓库内两种可靠性水位并存**。

### 3.4 渠道 / 模板 / 偏好

- **渠道**：枚举仅 4 种 `telegram / wechat_session / email / in_app`；**无通用 webhook/IM 渠道**；投递按 `channel_type` 硬编码 switch（`delivery.go:317-330`），**无 Provider 注册表**；能力以 `capabilities jsonb` 声明（`supports_test_send/rich…`）；
- **凭据**：`notification_channels.secret_config` + 微信账号表回退（`delivery.go:923-943`）；`route_states` 支撑路由态与过期；
- **模板**：`event_types × channel_type × version` + `variables_schema`，渲染在投递前完成并把 `rendered_*` 落库；
- **偏好/订阅**：`scope_type/scope_id/event_type/category` 四维匹配 + `min_severity` + `mute_until` + `quiet_hours` + `conditions`；默认偏好集中在 `preferences.go:29`；匹配函数是**纯函数**（`RouteObservedEvent`），便于单测。

### 3.5 可观测与运维

- `readiness.go`：邮件可用性探针（未配置即显式不可用）；
- `last_test_status` + **真实测试发送**（`TestChannel` 走 Emit 真发一条并落状态：`service.go:541-565`；路由 `gin.go:3252`）；
- 管理侧 `dispatches/retry` API（人工重试）；
- 错误与渲染内容在落库前 **redact**（`delivery.go:239-248, 950-953`；`helpers.go:582-592`）。
- 用户 inbox：**游标分页**（`next_cursor`）+ 未读汇总按 `category/source_type` 分桶 + 4 个 partial index，无缓存（`notifications_user.go:25-34, 77-116`；迁移 288）——ITSM 可借鉴其读模型（R3）。

### 3.6 借鉴 / 不照搬清单

**可借鉴（8 条）**

| # | 借鉴点 | 用途 |
|---|---|---|
| R1 | 模板表：`event_types × channel_type × format × variables_schema × version` | ITSM 引入通知模板（当前无模板实体，N6） |
| R2 | 订阅/偏好模型：scope 维度 + `min_severity` + `mute_until` + `quiet_hours` + `conditions`，匹配为纯函数 | 补齐 ITSM 偏好（当前仅 4 bool，`quiet_hours/frequency` 未消费，N7） |
| R3 | 收件箱 `read_at/archived_at/expires_at` + 未读汇总部分索引 | 通知中心 UX 重构（v1.1）的读模型 |
| R4 | outbox：`dedupe_identity` 部分唯一 + lease + `FOR UPDATE` 重领 | 可反向加固 ITSM 现有幂等键设计（已强，取长补短） |
| R5 | dispatcher 状态机含 **`skipped`** 终态 | ITSM 当前"偏好关闭=命令成功且无投递记录"，缺"为什么没发"的显式记录（N5） |
| R6 | "只入箱不发送"的候选模式（事务内写候选，发送交后台） | 邀请/批量场景的推荐模式 |
| R7 | readiness + `last_test_status` + 测试发送 | 渠道可用性显式化，替代 ITSM 前端"假测试"（N6） |
| R8 | 管理侧 dispatches/retry 控制面 | ITSM 已有 commands 控制面，扩展为通知视角 |

**不建议照搬（5 条）**

| # | 不照搬 | 原因 |
|---|---|---|
| X1 | 核心 `Emit` 的"请求内直发 + 非事务" | ITSM 已有强门禁（事务内入箱 + 异步出箱），不允许回退 |
| X2 | 硬编码 switch 无 Provider 注册表 | ITSM `connector` registry 已有 manifest/checksum/租户装配门禁，更优 |
| X3 | 核心引擎无 `dead_letter` 终态（RG 实现有，取其 RG 版本） | ITSM 已有死信 + 重放 API（保留） |
| X4 | 单进程 ticker、无 claim/限流 | ITSM worker 有租约/围栏；多实例安全不可回退 |
| X5 | 渠道/模板表无 `tenant_id` | ITSM 多租户下必须租户隔离（平台级模板例外，见 §4.5） |

---

## 4. 目标架构

### 4.1 分层（复用 + 两处新增）

```text
业务事务
  └─ EnqueueNotificationTx（事件目录驱动 + 收件人解析规则）        [新增：目录]
       └─ operational_commands(notification.deliver)               [既有]
            └─ NotificationDeliveryCommandHandler                  [既有，扩展]
                 ├─ 偏好 / 静默规则判定 → 通过 or skipped(原因)     [新增：skipped 记录]
                 ├─ 模板渲染（event × channel × locale × version）  [新增：渲染层]
                 ├─ 目标解析（租户内实时，掩码落库）                 [既有]
                 └─ 渠道投递
                      ├─ in_app → notifications + deliveries        [既有]
                      └─ 外部   → connector.Manager.Send            [既有]
```

**新增三处**：事件目录（Event Catalog）、渲染层（Template Renderer）与 **commandbus 错误分类契约**（`PermanentError` → 直接死信，见 §4.6/B9）；投递可靠性、租户隔离、幂等复用现有基座。

### 4.2 事件目录（单一事实源）

| 字段 | 说明 |
|---|---|
| `event_type` | canonical 名（如 `ticket_created`）；兼容别名列表（`created`、`change_approval_decided`→`approval_completed`） |
| `scope` | `platform`（邀请/密码重置/安全告警）｜`tenant`（业务通知） |
| `severity` | info/warning/critical（供偏好 `min_severity` 与优先级） |
| `default_channels` | 默认渠道集合 |
| `mandatory_channels` | **必达渠道**（用户不可关），如安全告警的 in_app+email |
| `allow_user_opt_out` | 是否允许用户关闭 |
| `variables_schema` | 模板变量契约（JSON Schema 子集） |
| `schema_version` | 事件 payload 契约版本（新增字段=minor；破坏性变更=major，靠别名映射兼容旧版） |

- **命名规范**：`<domain>_<object>_<past_tense>`、小写下划线（如 `change_approval_completed`）；**禁止把两个源事件合并为一个泛化词**（`change_approval_decided` 与 `approval_completed` 必须拆开或明确别名到其一）；旧短词仅作 alias；
- 落地：代码内置默认（`internal/notification/catalog.go`）+ 平台可覆盖表 `notification_event_catalog`（仅平台管理员写 + 审计）；
- 目的：**消灭三套词表漂移**（N7）——偏好 DTO、投递归一化、模板全部从目录取值；
- 兼容：保留现有短词表映射，映射表由目录生成（不破坏已落库偏好行）。
- **优先级与治理**：DB `status=enabled` 行 > 代码默认；删除须先 `deprecated`（保留别名解析）再下线；状态枚举 `active/deprecated/blocked`；
- **契约测试**：目录 ↔ 偏好 DTO ↔ 模板 ↔ 前端事件列表 一致性用例（防再次漂移，见 §8 A17）。

### 4.3 模板与渲染

- 表 `notification_templates`：

| 字段 | 说明 |
|---|---|
| `tenant_id` | **NULL = 平台模板**；非空 = 租户覆盖 |
| `event_type` / `channel` / `locale` | 匹配维度 |
| `title_template` / `body_template` / `format` | text/html/markdown |
| `variables_schema` / `version` / `status` | 变量契约、版本、草稿/启用 |

- 唯一：`(COALESCE(tenant_id,0), event_type, channel, locale, version)` 有效期内唯一；
- 渲染：投递前完成；渲染结果与变量 **redact 后**落 `notification_deliveries.rendered_*`（借鉴 R1/R5）；
- 缺变量：默认失败进重试；可按渠道配置降级为纯文本并告警；
- 解析顺序：租户覆盖 → 平台模板 → 代码内置兜底（保证"永远能渲染"）；
- **转义与白名单（B17）**：按格式默认转义——HTML 用 `html/template` 语义；Markdown 用 allowlist sanitizer；纯文本原样（剥离标签）；模板**禁止内联脚本/事件属性/外部资源**；租户模板与平台模板**同过** sanitizer；新增 XSS payload 单测；
- **生命周期（B18）**：草稿 → 预览（`POST /notification-templates/preview`，样例变量、不发信）→ 发布（`version+1`）→ 回滚（切回上一版本）；**版本不可变**，`(tenant,event,channel,locale) WHERE status='active'` 部分唯一；激活/回滚写审计并支持 feature flag 灰度；
- **变量校验时机（B18）**：入箱事务内按 `variables_schema` 校验；渲染期缺变量 → **永久失败 → dead_letter + 告警**（与 §4.6 错误分类一致）。

### 4.4 偏好与静默规则

- 扩展偏好模型（在现有 `notification_preferences` 上演进，避免新表并行）：

| 维度/字段 | 说明 |
|---|---|
| `user × event_type` | 现有唯一约束保留 |
| `channels` | 由 4 个 bool 演进为 `channels jsonb`（或按渠道行）；消费端改为"目录默认 ∩ 用户偏好 − 必达豁免" |
| `min_severity` | 低于阈值的通知只进 in_app |
| `mute_until` / `quiet_hours` | **真正消费**（当前仅存储，N7）；必达事件不受静默限制 |
| `frequency` | `immediate`（默认）/`daily`（P2 摘要） |

- **时区语义（B15）**：按收件人 IANA 时区的**本地 HH:MM 窗口**计算（跨午夜与 DST 按当地日历）；非必达延迟到窗口结束 + 抖动；**最长 12h** 后仅投 in_app 并记 `skip_reason=quiet_hours_expired`；
- **判定顺序**：目录必达 → 用户偏好 → 静默时段（仅非必达）→ 渠道可用性（connector/平台 SMTP 是否就绪）；
- **结果留痕（B11）**：被跳过时写 `notification_deliveries.status=skipped` + `skip_reason`，枚举**仅限** `preference_off` / `quiet_hours` / `channel_not_configured` / `suppressed` / `superseded` / `channel_circuit_open` / `quota_exceeded`；**`no_target` 属永久失败 → dead_letter**（`error_code=no_target`），不列入 skipped；**不再"静默成功"**（借鉴 R5，修 N5）。

### 4.5 平台通道 vs 租户通道（回答 multi-tenant Q4）

| 通道 | 用途 | 配置来源 | 投递实现 |
|---|---|---|---|
| **平台 SMTP** | 邀请、密码重置、安全告警、平台公告 | `smtp.*`（全局）+ `SMTP_*` 环境变量 | `EmailService`（**接线**：bootstrap 构造 → 注入）→ 走 `notification.deliver`（`scope=platform`） |
| **租户 connector** | 业务通知（工单/变更/SLA）、租户邮箱收发 | `connector_configs`（租户级，加密） | 既有 `connectorManager.Send` |
| **in_app** | 全部租户 | 无 | 既有 |

规则：

0. **平台 scope 入箱契约（B1，Blocker）**：平台级命令以**目标租户 `tenant_id`** 承载审计与隔离；payload 只存 `invitationId` / `recipientRef`（**不存邮箱**）；新增 `EnqueuePlatformNotificationTx` 与 Handler 的 `resourceType=platform_invitation|platform_account` 分支，由 Handler 依据邀请/账号记录解析邮箱；**不得要求邀请对象是租户内 Active 用户**；
1. `scope=platform` 事件只能走平台通道；`scope=tenant` 事件默认租户通道，租户未配置时**降级 in_app** 并记 `skip_reason=channel_not_configured`（可配置为失败告警）；
2. 邀请（对象尚无用户记录）与密码重置（平台级安全邮件，与租户 connector 解耦）**固定走平台 SMTP**；
3. 平台 SMTP 未配置：邀请接口返回 `inviteUrl` + `emailSent=false`；密码重置返回明确提示——**不再静默跳过**（修 N2）；
4. 平台 SMTP 与租户 connector **互不兜底**（禁止跨域发信），避免"用客户邮箱发平台邮件"的合规问题；
5. **平台公告 = 订阅类（B13）**：附 RFC 8058 `List-Unsubscribe`/`List-Unsubscribe-Post` + 频次上限 + 退订记录；**邀请/密码重置邮件禁止夹带公告或营销内容**（事务/营销分流）；
6. **可见性（B20）**：`channel_scope=platform` 的投递记录仅平台 `system:write` 可见，租户查询默认排除（防邀请/重置邮件元数据泄露）。

### 4.6 可靠性增强（在既有 outbox 之上）

| 增强 | 说明 | 备注 |
|---|---|---|
| 每渠道限流 + 配额 | 令牌桶（租户+渠道）+ **每租户日配额**；超限记 `skipped(quota_exceeded)` 或延期；worker 按租户加权轮询防队头阻塞 | **前移为 P0-1 前置**（至少平台 SMTP 全局 + 每租户日配额）；B7/B21 |
| 去重窗口 | 同 `recipient+event+resource` 在 N 分钟内合并（内容相同才合并） | 幂等键保持"一次业务事件一条命令" |
| 优先级 | critical 立即；低优先级经 `commandbus.DeferCommand(id, until)` 延期（**不消耗 attempt**） | 新增 `priority/not_before` 字段，不复用 `available_at`（B8） |
| 退避抖动 | 退避叠加随机抖动（`base ± rand`），避免重试风暴 | 现实现无抖动（`commandbus.go:298-326`） |
| 熔断与降级 | 渠道连续失败达阈值 → 熔断期内直接 `skipped(channel_circuit_open)`；业务通知降级 in_app | 防止无效重试打满队列 |
| 退信/投诉抑制 | 硬退信、投诉命中 → 地址进抑制名单，后续 `skipped(suppressed)` | 保护发信声誉（配合 §4.7） |
| 错误分类 | commandbus 新增 **`PermanentError`** 类型（`errors.As` → 直接 `dead_letter`）；永久类=`no_target`/地址非法/模板缺变量；`5xx/网络` → 退避 | B9/B11；§4.1"新增三处"之一 |
| 死信与重放 | **保留**既有 `dead_letter` + 运维重放 API | 不照搬 ai-gateway 的"无死信"（X3） |
| 悬空补偿 | 超时 `pending` **仅经 fencing 重领原命令**（不新增命令、不复用幂等键）；无法确认是否已发送的渠道标记 `status=unknown` 进人工重发队列 | 修 N13；B3（同键重入队会被唯一约束拒绝） |
| 顺序性 | **不保证跨命令顺序**；同一 `(recipient, resource, event_family)` 的状态类通知采用 supersede（新状态使旧命令置 `skipped(superseded)`） | B10 |
| 渠道幂等矩阵 | 命令级幂等=同一业务事件最多一条命令；渠道级去重取决于 Provider 能力——IM/Webhook 可透传 ID，**SMTP 无 Provider 幂等（至少一次）**，重复经 `status=unknown` 暴露并人工确认 | B4（修正 §1.4-5 的过度承诺） |

### 4.7 安全与合规

- 凭据：平台 SMTP 密码仅环境变量/加密配置；connector 凭据沿用 `CONNECTOR_CONFIG_ENCRYPTION_KEY` 加密与 fail-closed（既有）；
- 目标地址：仅 Handler 运行时在租户内解析；日志/审计只存掩码（既有约定，保持）；
- 审计：模板变更、必达清单变更、渠道启停、人工重发/取消 → `audit_logs`；
- PII：渲染变量与正文在落库前 redact（借鉴 ai-gateway `sensitivePayload`）；
- **邮件合规（B12/B13，P0-1 前置，未完成不得开启平台 SMTP）**：

  | 项 | 要求 |
  |---|---|
  | 发件域 | 独立子域 `notify.<platform-domain>` + 独立 Return-Path/bounce 子域 |
  | 认证 | SPF + DKIM(2048) + DMARC（`p=none → quarantine`，`rua/ruf`） |
  | 通道 | 用 ESP relay（不自建 MTA）；ESP bounce/complaint webhook → 抑制名单（§4.6） |
  | 分流 | 事务邮件（邀请/重置/安全）与订阅类（公告）分流；订阅类附 RFC 8058 一键退订 + 频次上限 |
  | 预热 | 30 天发信预热曲线 + 每日配额 |

- **出站 Webhook（B19）**：沿用既有 HMAC-SHA256 签名（`X-ITSM-Signature`，`connector/builtin/webhook/webhook.go:76-80`）并**强制配置 secret**、加 timestamp/nonce 防重放；**补 SSRF 防护**——复用仓库既有 `mcp/transport.SSRFGuard`（拒私网/链路本地/元数据地址，DNS pin + 协议/端口 allowlist），配置与发送两侧均校验并审计；作为 P1-5 前置；
- **数据保留**：`notifications`/`notification_deliveries` 按保留期清理（默认 180 天，可配；审计与合规例外），复用附件清理的 dry-run + 审计模式（`service/attachment_cleanup.go` 先例）。

### 4.8 可观测

| 维度 | 指标/入口 |
|---|---|
| 投递 | 按 `(tenant, channel, status, error_code)` 聚合成功率与失败 Top |
| 命令 | 复用 outbox 指标：pending 数、最老等待、重试率、死信数 |
| 渠道 | `connector` health（`lastSuccessAt/lastFailureAt`）+ `last_test_status` + 平台 SMTP readiness |
| 一致性 | 通知中心未读数与 `notifications.read=false` 计数对账（修"未读陈旧"） |
| 运维 | 现有 `/admin/operations/commands`（重放/取消）+ 新增按 delivery 的查询/重发 |
| SLO（B22） | 成功率：in_app ≥ 99.9%、email ≥ 97%（不含用户偏好跳过）；最老 pending ≤ 5m；死信率 ≤ 0.1% |
| 告警阈值（B22） | DLQ 增长 > 0/15m；熔断打开；readiness=false；bounce > 2%；complaint > 0.1%；单渠道失败率与 pending 最老等待超阈 |
| 失败分类枚举（B23） | `permanent / retryable / rate_limited / circuit_open / config / template` 作为 dashboard 与重试策略的**唯一维度**；connector health 展示前对 `lastError` 分类脱敏（不落 provider 原始错误） |
| 运行手册（B22） | `docs/ops/` 增：SMTP 送达率骤降、渠道 401/403、DLQ 重放、配额调整、模板回滚 |

---

## 5. 数据模型变更

| 变更 | 内容 | 批次 |
|---|---|---|
| `notification_event_catalog`（新） | 平台级事件目录：`event_type/scope/severity/default_channels/mandatory_channels/allow_user_opt_out/variables_schema/aliases/schema_version/status`；**必须登记 `TenantExemptTables`（reason/owner/reviewed_at）+ 一致性单测 + `docs/architecture/tenant-isolation.md` 豁免章节 + CHANGELOG，否则生产启动 fail-closed（B2）** | P0-3 |
| `notification_templates`（新） | 见 §4.3；`tenant_id NULL=平台` | P1-1 |
| `notification_deliveries` 扩展 | `event_type`、`channel_scope(platform\|tenant)`、`template_version`、`rendered_subject/body`（redacted）、`skip_reason` | P1-1/P1-3 |
| `notification_preferences` 扩展 | **定稿（B16）**：保留 4 bool 为兼容读模型，新增 `notification_preference_channels` 行表为**唯一写入源**并双写回填（含回填/回滚脚本，修正 §5"add-only"表述）；新增 `min_severity`、`mute_until`、`locale`（B18）；**消费** `quiet_hours`/`frequency` | P1-2 |
| `notifications`（读侧）扩展 | `source`、`event_type`、`severity`、`expires_at`（借鉴 R3）；`type` 保留兼容 | P1-3 |
| `notification_suppressions`（新） | 抑制名单：`tenant_id`、`address_hash`、`reason(hard_bounce/complaint/manual)`、`expires_at`、`created_by`；命中即 `skipped(suppressed)` | P1-5 |
| 保留策略（B24） | `notification_deliveries` 180d、`rendered_*` 30d、读侧 `notifications` 180d（用户删除级联）、`operational_commands` 180d、审计按合规策略；每日清理任务（dry-run + 审计）+ 用户删除/导出流程 + 法律保留例外 | P1-3 |
| 迁移策略 | 加表/加列为主（**例外：偏好渠道化需双写回填**，见上）；在线 DDL；回填 `notifications.event_type`（由 `type` 映射）+ **回填校验（计数+抽样+幂等重跑，A18）**；**无唯一约束变更**；每步可独立回滚 | — |

## 6. API 与事件契约

| 变更 | 说明 | 批次 |
|---|---|---|
| `GET /notification-event-types` | 从**目录**读取（替代 DTO 硬编码 15 项），返回 canonical + 别名 + 可关闭性 | P0-3 |
| `/notification-templates`（GET/POST/PUT）+ `/notification-templates/preview`（样例变量渲染，不发信）+ `/notification-templates/:id/activate\|rollback` | 平台管理员管理平台模板；租户管理员管理本租户覆盖；**版本不可变**，激活/回滚写审计 | P1-1 |
| `POST /notification-channels/:name/test` | **真实测试发送**（替换前端 `setTimeout` 假成功） | P1-5 |
| `GET /admin/notifications/deliveries` | 运维查询（按 tenant/event/channel/status 过滤）；`channel_scope=platform` 记录仅平台 `system:write` 可见（B20） | P1-3 |
| `POST /admin/notifications/deliveries/:id/retry` | 人工重发（写审计） | P1-3 |
| 邀请/密码重置 | `POST /users/invitations` → `{inviteUrl, emailSent}`；`POST /auth/password-reset` → `{emailSent}`（未配置 SMTP 时 `false` + 提示） | P0-1 |
| 事件命名 | canonical 保留现短词（`ticket_created`…）；目录登记别名（`created`、`approval_completed` 等），偏好/模板/归一化统一走目录 | P0-3 |
| 兼容 | 前端 `/notification-preferences` 的 `/me`、`/templates`、`/apply-template` 要么实现、要么从 API client 移除（二选一，禁止悬空） | P1-2 |

---

## 7. 分期实施计划

### P0（修复批次，阻塞级）

| # | 任务 | 内容 | 对应缺口 |
|---|---|---|---|
| P0-1 | 平台 SMTP 接线 | bootstrap 依 `smtp.enabled` 构造 `EmailService` → 注入 `handlers/auth`；密码重置/邀请改走 `notification.deliver(scope=platform)`；未配置时 API 显式返回（`emailSent=false` + 提示）。**接线前置（B14）**：关闭 `EmailService` 内部重试（避免 3×8=24 次）、生成稳定 Message-ID、强制 STARTTLS/TLS（不可用 fail-closed）、日志与错误脱敏；**限流与每租户日配额前移（B7）**；**邮件合规项（§4.7）未完成不得开启** | N1/N2；multi-tenant Q4 |
| P0-2 | 旁路清理 | SLA critical 直发改走 outbox；删除 `TicketNotificationService` 旧同步路径（含 N×N 缺陷）；生产强制 `EnableTxOutbox`（fail-closed） | N3/N4 |
| P0-3 | 事件目录 v1 | canonical 词表 + 别名；替换 handler 归一化、偏好 DTO、前端事件列表的硬编码 | N7（前半） |
| P0-4 | 测试基线 | 通知 controller/service 测试；outbox 故障注入（重试/死信/围栏）；各渠道 mock E2E；**风暴/负载用例** | N10 |

### P1（模块化）

| # | 任务 | 内容 | 对应缺口 |
|---|---|---|---|
| P1-1 | 模板与渲染 | `notification_templates` + 渲染器（**转义/白名单**）+ 管理 API + 三级兜底（租户→平台→代码）+ **发布流程（草稿→预览→发布→回滚）** | N6 |
| P1-2 | 偏好升级 | 渠道维度、`min_severity`、`mute_until`；**消费** `quiet_hours/frequency`；必达白名单；前端 API 对齐（实现或移除） | N7（后半） |
| P1-3 | 投递可解释 | `skipped + skip_reason` 落库；`notifications` 读侧扩展（`source/event_type/severity/expires_at`）；运维 deliveries 查询/重发 API | N5 |
| P1-4 | 实时推送 | 通知落库后 `SendToUser` 推送；前端由轮询改订阅（含未读对账） | N8 |
| P1-5 | 渠道治理 | 真实"测试发送"、渠道健康视图、`webhook` 目标按租户解析 + **SSRF 校验**、平台 SMTP readiness、**退信/投诉抑制名单** | N6/N9 |

### P2（增强）

| # | 任务 | 内容 |
|---|---|---|
| P2-1 | 摘要（digest） | `frequency=daily` 聚合投递（借鉴 R2 的 `frequency` 语义） |
| P2-2 | SMS 通道 | 新增 sms connector（预留接口与偏好字段已就绪） |
| P2-3 | 限流/优先级/去重窗口 | 令牌桶、critical 优先、短窗合并 |
| P2-4 | 多语言 | 模板 locale + 收件人语言偏好 |

**依赖关系**：P0-1 关闭 multi-tenant 方案 Q4；P1-1 是邀请流"邮件自动发送"的前置；P0-3 是 P1-1/P1-2 的前置（目录先于模板/偏好）。

---

## 8. 验收标准与测试计划

| # | 场景 | 期望 |
|---|---|---|
| A1 | 平台 SMTP 未配置 → 发起邀请 | `200 {inviteUrl, emailSent:false}`；**必须留投递记录**（`skipped(channel_not_configured)` 或平台 scope 的明确未入箱原因），不得"无记录"（B11） |
| A2 | 配置 SMTP → 邀请 / 密码重置 | 邮件可达；deliveries 记录 `sent` + `target_masked` + `provider_message_id` |
| A3 | 用户关闭非必达渠道（email） | `skipped(skip_reason=preference_off)`；in_app 正常；**必达渠道不受影响** |
| A4 | 静默时段（quiet_hours）内非必达通知 | 延迟或跳过（按配置）；critical/必达立即送达 |
| A5 | 模板解析 | 租户覆盖 → 平台 → 代码兜底三级命中；变量缺失 → 重试 + 告警 |
| A6 | 租户未配置 connector | 降级 in_app + `skip_reason=channel_not_configured`；不跨域用平台 SMTP |
| A7 | 幂等 | 同一 occurrence key 重复入队 → 仅 1 条命令；不同 occurrence → 各自投递 |
| A8 | 错误分类 | 地址非法（永久）→ 直接 `dead_letter`；网络/5xx → 退避重试至上限 |
| A9 | 多实例并发 | 同一命令仅一个 Worker 提交成功（lease + fencing） |
| A10 | 越权 | 跨租户目标解析失败（403/错误分类）；模板与目录租户隔离 |
| A11 | 回归 | `go test ./service/... ./internal/commandbus/...`；docs gate 5/5 |
| A12 | 退信/投诉 | 硬退信地址进入抑制名单；后续投递 `skipped(suppressed)`；解除需人工 + 审计 |
| A13 | Webhook SSRF/签名 | 私网/元数据地址被拒绝配置；出站请求带 `X-ITSM-Signature` 且可验签 |
| A14 | 熔断/抖动 | 渠道连续失败触发熔断（不再无效重试）；多次退避间隔含随机抖动 |
| A15 | 保留清理 | 超保留期的通知/投递记录被清理（dry-run 可预演，清理写审计） |
| A16 | 风暴/负载（B26） | 每租户 1k 通知/分钟、单资源 10k 收件人：无队头阻塞、无重试风暴（抖动生效）、配额按预期生效 |
| A17 | 契约测试（B25） | 目录 ↔ 偏好 DTO ↔ 模板 ↔ 前端事件列表 一致性用例通过 |
| A18 | 回填校验（B26） | 回填后计数一致、抽样映射正确、脚本可幂等重跑 |
| A19 | 灰度与回滚（B26） | 每阶段 feature flag 灰度；旧路径删除前"告警灰度一周"；模板/偏好可回滚 |

---

## 9. 风险与开放问题

### 9.1 决策项（D1–D6）

> 2026-09-29 已按最佳实践确认：**最终取值见 §11.3**；下表保留原始问题与建议，作为决策轨迹。

| # | 问题 | 建议 |
|---|---|---|
| D1 | 平台 SMTP 的配置责任与发件域名（SPF/DKIM）、是否允许向客户域名发信 | 运维项，P0-1 前置；建议单独发件域名（如 `no-reply@itsm.example.com`） |
| D2 | 模板存储：DB vs 代码 | **DB（含平台/租户两级）+ 代码内置兜底**；避免"改文案要发版" |
| D3 | 必达清单范围 | 安全告警、邀请、密码重置、审批结果；平台配置 + 审计 + 用户界面明示 |
| D4 | 是否本期做摘要（digest） | 建议 P2（先保证即时通知正确） |
| D5 | SMS 供应商与合规 | P2 单独立项；本期只预留字段与接口 |
| D6 | 通知中心 UX 重构范围 | 与 v1.1 规划对齐（`docs/archive/capability-matrix.md:135`），本方案提供后端能力 |

### 9.2 风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| SMTP 送达率（进垃圾箱） | 中 | 独立发件域名 + SPF/DKIM + 限流；先用测试邮箱验证 |
| 通知风暴（批量操作/循环触发） | 中 | 去重窗口 + 限流 + 低优先级延迟（P2-3） |
| HTML 模板注入 | 中 | 渲染转义 + 变量白名单 + 仅管理员可写模板 |
| 跨租户模板/目录泄露 | 高 | 查询强制租户维度；平台模板只读可见 |
| connector 凭据轮换遗漏 | 中 | 沿用 fail-closed 解密；轮换后自动健康检查 + 告警 |
| 旧路径删除引入回归 | 中 | P0-2 先加"旧路径调用即告警"灰度一周，再删除 |

---

## 10. 附录 A：证据索引

### A.1 ITSM（现状）

| 主题 | 位置 |
|---|---|
| outbox 基座与门禁 | `docs/architecture/operational-command-outbox.md:9-21, 30-41, 54-62, 78-87`；`docs/architecture/domain-ownership.md:14, 63` |
| 命令总线实现 | `itsm-backend/internal/commandbus/commandbus.go:26-41, 98-136, 174-229, 298-326` |
| 入队（资源通知） | `itsm-backend/service/notification_outbox.go:25-58` |
| 投递 Handler | `itsm-backend/service/notification_delivery_command_handler.go:35-43, 94-119, 125-179, 181-291, 317-332` |
| 工单通知与旧路径 | `itsm-backend/service/ticket_notification_service.go:36-41, 62-211, 216-244, 274-291, 598-740` |
| 数据模型 | `itsm-backend/ent/schema/{notification,ticket_notification,notification_delivery,notification_preference,operational_command,email_outbound_message}.go` |
| 渠道 connector | `itsm-backend/connector/manager.go:72-134, 177-237`；`connector/registry.go:39-53`；`connector/persistent_store.go:20-89`；`connector/builtin/{console,dingtalk,feishu,wecom,webhook,email}` |
| 偏好服务与路由 | `itsm-backend/service/notification_preference_service.go:85-90, 201, 250`；`router/common_system_routes.go:186-211` |
| 邮件未接线 | `itsm-backend/service/email_service.go:54-63, 189, 222, 284, 368`；`handlers/auth/service.go:40, 234-238`；`service/sla_alert_service.go:529-556` |
| 前端 | `itsm-frontend/src/components/business/NotificationCenter.tsx:147-279`；`components/layout/header/NotificationDrawer.tsx:15, 192-198`；`lib/api/notification-preference-api.ts:41, 71, 78, 111, 118`；`lib/services/notification-ws.ts` |
| 审计与已知问题 | `docs/archive/生产就绪审计报告-2026-07-12.md:58`；`docs/testing/controller-failing-list.md:70, 123`；`docs/archive/testing-reports/browser-button-functional-test-report-2026-08-02.md:164`；`docs/archive/capability-matrix.md:131, 135` |

### A.2 ai-gateway（参考）

| 主题 | 位置 |
|---|---|
| 核心引擎与两条入队路径 | `internal/service/notification/service.go:22, 241, 503-539`；`internal/service/aisiteevents/distribution.go:188-215` |
| dispatcher/重试/状态机 | `service.go:2032-2061`；`delivery.go:215-275, 317-330, 945-965`；`helpers.go:602-607` |
| outbox 参考实现 | `internal/gateway/resourcegovernance/notifications/{outbox,dispatcher,job,job_runner,gorm_repository}.go`；`migrations/146_*_postgres.sql:38-56` |
| 订阅/偏好 | `migrations/238_*_postgres.sql:1-17`；`migrations/247_*`；`internal/service/usernotification/{preferences,router,service,readiness}.go` |
| 模板/渠道/收件箱 | `internal/model/entity/{notification_template,notification_channel,notification_inbox_item,notification_dispatch,notification_event}.go`；`migrations/288_*_postgres.sql:1-15` |
| 管理/用户 API | `internal/gateway/router/gin.go:3247-3260, 3318-3321, 3885-3891` |

---

## 11. 最佳实践对照与方案确认（2026-09-29）

### 11.1 确认方式与判定口径

- **依据**：通知/投递系统通行最佳实践——事务性 outbox、幂等消费、退避与**抖动**、死信与重放、**熔断与降级**、限流与配额、邮件认证（SPF/DKIM/**DMARC**）、退订（RFC 8058）、退信/投诉抑制、偏好与静默（含**时区**）、模板版本化与预览、渲染转义、凭据与 PII、出站 Webhook **签名与 SSRF**、多租户隔离与配额、SLI/SLO 与告警阈值、数据保留与清理、事件命名与 schema 版本、契约与故障注入测试、灰度与回滚。
- **方式**：主代理自检 + **独立评审子代理对抗式复核**（只读，逐条给出方案锚点与修订建议）；结论落本方案并同步修订正文。
- **判定口径**：✅ 已覆盖 ｜ ⚠️ 需补（本轮已写入修订）｜ ➖ 不适用/本期不做（附理由）。

### 11.2 对照矩阵（自检）

| # | 最佳实践 | 方案锚点 | 判定 | 修订/说明 |
|---|---|---|---|---|
| 1 | 事务性 outbox（业务与命令同事务） | §1.4-2、§2.1、§4.1 | ✅ | 既有门禁，禁止回退 |
| 2 | 至少一次 + 幂等键 | §2.1、§4.6 | ✅ | `(tenant, type, idempotency_key)` 唯一 |
| 3 | 退避 + **抖动** | §4.6 | ⚠️ | 现退避 `min(2^attempt,300)s` **无抖动**（`commandbus.go:298-326`）→ §4.6 增抖动 |
| 4 | 死信 + 重放 | §2.1、§4.6 | ✅ | 保留并复用 |
| 5 | **熔断/降级** | — | ⚠️ | 渠道连续失败应快速失败并降级 in_app → §4.6 增 |
| 6 | 限流与**按租户配额** | §4.6 | ✅ | P2-3；补"配额+公平调度"表述 |
| 7 | 悬空补偿 | §4.6 | ✅ | 修 N13 |
| 8 | 邮件认证 SPF/DKIM/**DMARC** | §9.1 D1 | ⚠️ | 原仅提 SPF/DKIM → §4.7/§11.3 补 DMARC 与渐进策略 |
| 9 | 退订（RFC 8058） | — | ⚠️ | 非事务性邮件需 `List-Unsubscribe`；安全/事务类例外 → §4.7 增 |
| 10 | 退信/投诉抑制 | — | ⚠️ | 硬退信与投诉命中即 `skipped(suppressed)` → §4.6/§5 增 `notification_suppressions` |
| 11 | 独立发件域 + 预热 | §9.1 D1 | ✅ | 补"预热"与配额 |
| 12 | 偏好/必达/静默（**时区**） | §4.4 | ⚠️ | 静默时段需明确取用户时区、缺省租户时区 → §4.4 补 |
| 13 | 模板版本/发布/回滚 | §4.3 | ⚠️ | 有 `version/status` 但无发布流程 → §4.3 补草稿→预览→发布→回滚 |
| 14 | 渲染转义/XSS | §9.2 风险 | ⚠️ | 缓解在风险表，未入正文 → §4.3 补转义与白名单 |
| 15 | 凭据与 PII 最小化 | §4.7 | ✅ | 既有加密 + 运行时解析 + redact |
| 16 | 出站 Webhook 签名 | §4.7 | ✅ | **ITSM 已实现** HMAC-SHA256（`webhook.go:76-80`，`X-ITSM-Signature`）→ 方案改为"引用并保持" |
| 17 | 出站 Webhook **SSRF** 防护 | — | ⚠️ | `webhook.go:51-55` 仅校验 url 非空，未拦私网/元数据地址（email connector 有先例 `validateConfiguredHost`）→ §4.7 增 |
| 18 | 多租户隔离 + 平台模板可见性 | §4.5、§4.7 | ✅ | 平台/租户两级，平台模板只读 |
| 19 | SLI/SLO 与告警阈值 | §4.8 | ⚠️ | 有指标无目标 → §4.8 增 SLO 与告警阈值 |
| 20 | 运行手册 | §4.8 | ⚠️ | 控制面有了，手册缺 → §4.8 增入口（`docs/ops/`） |
| 21 | 数据保留与清理（GDPR） | — | ⚠️ | 未提 → §4.7/§5 增保留策略（参照既有 `attachment_cleanup.go` 先例） |
| 22 | 事件命名与 **schema 版本** | §4.2、§6 | ⚠️ | 有 canonical/别名，无版本 → §4.2 增 `schema_version` 与命名规范 |
| 23 | 契约/故障注入/**风暴测试** | §8 | ⚠️ | 有 A1–A11，无负载/风暴 → §8 增 A12–A15 |
| 24 | 灰度与回滚 | §7 P0-2 | ✅ | 旧路径"告警灰度一周再删"；补模板/偏好变更 feature flag |
| 25 | 可访问性（aria-live） | — | ➖ | 属前端 UX 重构（v1.1）范围，本方案不展开 |
| 26 | 平台 scope 入箱契约（Blocker） | §4.5 规则 0 | ✅ | 独立评审 B1；`EnqueuePlatformNotificationTx` + 平台资源分支 |
| 27 | 平台表 tenant guard 豁免（Blocker） | §5 | ✅ | 独立评审 B2；`TenantExemptTables` + 单测 + 文档 + CHANGELOG |

> 上表 ⚠️ 项均已在 §11.4/§11.6 落地；独立评审共 26 项（B1–B26）+ 6 条门禁矛盾，处置见 §11.6。

### 11.3 确认结论（D1–D6 最终值）

| 决策 | 最终确认（含独立评审修订） | 依据 / 前置 |
|---|---|---|
| **D1** 平台 SMTP | **现在定，P0-1 前置**：独立子域 `notify.<platform-domain>` + 独立 Return-Path；SPF + DKIM(2048) + DMARC（`p=none → quarantine`，`rua/ruf`）；**用 ESP relay，不自建 MTA**；允许向客户域名发事务邮件、**禁止用客户 connector 发平台邮件**；本期不做租户自定义 From。**未完成前 P0-1 退化为仅返回 `inviteUrl`**（对齐 multi-tenant Q4 建议） | 送达率与合规门槛，晚定必返工 |
| **D2** 模板存储 | **DB 两级 + 代码兜底**，附条件：**版本不可变、单 active（部分唯一）、发布/回滚审计、兜底仅覆盖 P0 事务模板、契约测试防漂移** | 可运营性；全量可编辑会引入漂移与 XSS 面 |
| **D3** 必达清单 | **收窄**：所有事件 in_app 必达；仅**密码重置/安全告警/邀请** email 必达；审批结果默认开但**可关**（in_app 兜底）；禁 SMS/IM 必达；平台清单租户不可改、变更审计 + UI 明示 | 强制邮件与静默/退订预期冲突，制造投诉 |
| **D4** digest | **P2**；**P1 不暴露 `daily`**；schema 既有 `hourly_digest/daily_digest` 与方案 `immediate/daily` **收敛为同一枚举** | 避免"选了 daily 仍即时发送"的口径不一致 |
| **D5** SMS | **P2 单独立项**；**立即隐藏/置灰 `sms_enabled`**；立项须含同意记录、退订、静默时段、送达回执，不自建网关 | 消除 N9"开启即必失败"；SMS 合规成本高于邮件 |
| **D6** 通知中心 UX | 与 v1.1 对齐；**现在冻结最小后端契约**（未读游标分页、WS 失效事件、幂等已读、对账修复任务）；前端假功能"实现或移除" | P0 未读陈旧不依赖 UX 重构即可修复，避免接口二次返工 |

### 11.4 本次确认引入的正文修订

| 修订 | 落点 |
|---|---|
| 退避增加抖动；渠道熔断与降级；退信/投诉抑制 | §4.6 |
| 邮件 DMARC 与渐进策略；`List-Unsubscribe`；Webhook SSRF 校验；数据保留与清理 | §4.7 |
| 静默时段时区语义；模板转义/白名单；模板发布流程 | §4.4 / §4.3 |
| SLO 与告警阈值；运行手册入口 | §4.8 |
| `schema_version` 与命名规范 | §4.2 / §6 |
| `notification_suppressions` 表；保留期配置 | §5 |
| 风暴/负载测试与 SSRF/退信用例 | §8（A12–A15） |
| P1-5 增 SSRF 与抑制；P1-1 增发布流程；P0-4 增风暴用例 | §7 |

### 11.5 明确不采纳（避免过度设计）

| 不采纳 | 理由 |
|---|---|
| 引入独立消息队列（Kafka/RabbitMQ） | 现有 outbox + Worker 已满足；引入新基建不划算 |
| 第三方通知 SaaS | 自托管与多租户隔离优先；SaaS 仅作邮件通道备选（D1 备选） |
| 用户级渠道自定义（BYO 渠道） | 仅平台/租户两级；用户级只做偏好开关 |
| 多级审批式模板发布 | 单人审核 + 审计足够；避免流程空转 |
| 跨渠道优先级抢占调度 | P2 再评估；当前用延迟调度即可 |
| 通知已读/未读的强一致缓存 | 先用 DB 计数 + 前端对账；缓存引入一致性成本 |
| ai-gateway **全量订阅模型**（scope/category/conditions DSL） | 现模型够用；只取 `quiet_hours`/`mute_until`，conditions DSL 过度设计 |
| **租户自定义事件目录** | 目录须平台治理，否则词表漂移、跨租户统计失效（与 §4.2 目标矛盾） |
| **营销平台能力**（campaign/名单/A-B/打开像素） | §1.3 已排除；仅平台公告需最小一键退订 |
| **多 SMTP Provider 切换 / 每租户独立 IP 或域名** | 成本高收益低；单 ESP + DLQ + 人工重放足够 |
| **P1 做 digest** | 先把即时正确性、静默、去重做对（同 D4） |
| `silent_window` 与 `quiet_hours` 并列 | 两窗叠加难解释"为什么没发"；统一为 `quiet_hours + mute_until` |
| **全量实时 WS 推送 / 多端同步协议** | 用"落库 → 失效事件 → 拉取"即可 |
| **阅读回执/打开追踪** | 隐私与 GDPR 风险，对 ITSM 无业务价值 |
| **exactly-once / 全局内容哈希去重** | 与 occurrence key 冲突，会误吞合法重复事件 |
| **富文本 WYSIWYG 模板编辑器** | 文本 + 变量 + 预览足够，减少 XSS 面与治理成本 |

### 11.6 独立评审发现与处置（B1–B26）

> 独立评审子代理（只读、对抗式）对 v0.2 出具 26 项问题 + 6 条门禁矛盾；下表为处置记录（已并入正文者标注落点）。

| # | 严重度 | 发现（摘要） | 处置 / 落点 |
|---|---|---|---|
| B1 | **Blocker** | 平台 scope 无法入箱（现有入箱要求 tenant_id>0、recipientId>0 且收件人 Active；Handler 仅支持 3 类资源） | ✅ §4.5 规则 0：`EnqueuePlatformNotificationTx` + Handler 平台分支 + payload 存 `invitationId/recipientRef` |
| B2 | **Blocker** | `notification_event_catalog` 缺 `tenant_id` → 生产 tenant guard **拒启** | ✅ §5：登记 `TenantExemptTables` + 单测 + tenant-isolation 文档 + CHANGELOG |
| B3 | Major | 悬空补偿"重新入队"与幂等唯一键冲突 | ✅ §4.6：仅 fencing 重领；`status=unknown` 人工重发 |
| B4 | Major | "重试不重复"过度承诺（SMTP 无 Provider 幂等） | ✅ §4.6 渠道幂等矩阵；§1.4-5 口径修正 |
| B5 | Major | 退避无抖动 → 重试风暴 | ✅ §4.6：full jitter + `retry_burst` 指标 |
| B6 | Major | 无熔断/降级，SMTP 故障耗尽 8 次尝试 | ✅ §4.6：按 `(tenant,channel)` 熔断 + 半开探测 + 告警 |
| B7 | Major | 限流/去重被推到 P2，而 P0-1 先开邮件 | ✅ §4.6/§7：限流与每租户日配额**前移为 P0-1 前置** |
| B8 | Major | `available_at` 语义过载 | ✅ §4.6：新增 `priority/not_before` + `DeferCommand`（不耗 attempt） |
| B9 | Major | 永久失败直死信需 commandbus 支持，与"只新增两块"矛盾 | ✅ §4.1 改"新增三处"；§4.6 `PermanentError` |
| B10 | Minor | 未识别顺序性 | ✅ §4.6：声明无序 + supersede 语义 |
| B11 | Minor | `no_target` 语义冲突；A1 留痕口径矛盾 | ✅ §4.4 枚举收敛；`no_target`→dead_letter；A1 修正 |
| B12 | Major | 邮件合规缺失（DMARC/Return-Path/退信/投诉/分流/预热） | ✅ §4.7 合规表（P0-1 前置） |
| B13 | Major | 平台公告混入事务流、无退订 | ✅ §4.5 规则 5：订阅类 + RFC 8058 退订 + 频次上限 |
| B14 | Major | 接线 `EmailService` 的隐患（3×8 重试/日志/未强制 TLS） | ✅ §7 P0-1 前置改造清单 |
| B15 | Major | 静默时段未定义（本地窗口/跨午夜/DST/最大延迟） | ✅ §4.4 时区语义 + 12h 上限 |
| B16 | Major | 偏好存储二选一未决，与 §5 add-only 不自洽 | ✅ §5 定稿：渠道行表 + 双写回填 + 修正表述 |
| B17 | Major | XSS 仅风险表一句，无实现规范 | ✅ §4.3：按格式转义 + sanitizer + 禁项 + 单测 |
| B18 | Minor | 缺 preview API/单 active 约束/变量校验时机/locale | ✅ §4.3/§5/§6：预览 API、部分唯一、入箱校验、`locale` |
| B19 | Major | 出站 Webhook 无 SSRF 防护、HMAC 未强制 | ✅ §4.7：复用 `mcp/transport.SSRFGuard` + 强制签名 + 防重放 |
| B20 | Minor | 平台 scope 投递记录的租户可见性 | ✅ §4.5 规则 6：仅平台可见 |
| B21 | Major | 无每租户配额与公平调度（队头阻塞） | ✅ §4.6：日配额 + 加权轮询 |
| B22 | Major | 无 SLI/SLO、告警阈值、运行手册 | ✅ §4.8：SLO/阈值/手册清单 |
| B23 | Minor | 失败分类无枚举；health 暴露 provider 原始错误 | ✅ §4.8：枚举 + 展示前脱敏 |
| B24 | Major | 数据生命周期（保留/清理/GDPR）缺失 | ✅ §5 保留策略 + 清理任务 + 删除/导出 |
| B25 | Major | 代码/DB 双事实源优先级与 schema 版本未定义 | ✅ §4.2：DB>代码、deprecated 流程、命名与契约测试 |
| B26 | Major | 无风暴/契约/回填/灰度验收 | ✅ §8 A16–A19 |

**C. 门禁矛盾（6 条）**：① 平台 scope 例外契约（B1，已补）；② tenant guard 豁免（B2，已补）；③ 永久失败分类需基座支持（B9，已补）；④ connector health 暴露原始错误（B23，已补）；⑤ `EmailService` 现状与 outbox 语义冲突（B14，已补）；⑥ **跨方案 Q4 口径**——multi-tenant 方案标"待确认"，本方案曾称"P0-1 关闭 Q4"；已统一为：**D1 已确认，未完成前 P0-1 退化为返回 `inviteUrl`**（multi-tenant 方案 Q4 行同步更新）。

---

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：ITSM 现状盘点（含 N1–N12 缺口清单）、ai-gateway 通知模块剖析（R1–R8 借鉴 / X1–X5 不照搬）、目标架构（事件目录/模板/偏好/平台-租户双轨/可靠性/可观测）、数据模型与 API 契约、P0–P2 分期、验收矩阵、风险与 D1–D6 决策项 |
| v0.2 | 2026-09-29 | 并入调研增量：新增 N13（投递审计与发送非同事务→悬空补偿）；N8 升为 P0（未读陈旧实测）、N9 补充"sms 偏好可开但必失败"；§3 补充 RG outbox 的 dedupe/silent 窗口与 dead_letter 落地样本、真实测试发送、用户 inbox 游标分页与未读汇总；§4.6 增加悬空补偿 |
| v0.3 | 2026-09-29 | **按最佳实践完成方案确认**（新增 §11：确认方式、25 项对照矩阵、D1–D6 最终值、修订清单、明确不采纳项）。正文修订：§4.2 `schema_version` 与命名规范；§4.3 渲染转义与模板发布流程；§4.4 静默时段时区语义；§4.6 退避抖动/熔断降级/退信抑制；§4.7 DMARC、`List-Unsubscribe`、Webhook SSRF、数据保留；§4.8 SLO 与运行手册；§5 `notification_suppressions` 与保留策略；§7/§8 同步用例与任务 |
| v0.4 | 2026-09-29 | **并入独立对抗式评审（B1–B26 + 6 条门禁矛盾）并完成最终确认**：2 个 Blocker（平台 scope 入箱契约、catalog 的 tenant guard 豁免）与 20+ Major/Minor 全部处置（§11.6）；D1–D6 按评审建议收敛（§11.3）；新增 §11.5 不采纳清单（11 项）；正文同步：§4.1"新增三处"、§4.5 平台契约/公告/可见性、§4.6 抖动/熔断/配额/`PermanentError`/supersede/幂等矩阵、§4.7 邮件合规表与 SSRFGuard、§4.8 SLO 与失败枚举、§5 偏好定稿/保留策略/豁免登记、§6 模板预览与激活 API、§7 P0-1 前置、§8 A16–A19、§1.4-5 幂等口径 |
