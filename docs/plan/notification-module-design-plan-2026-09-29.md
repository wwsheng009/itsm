# 通知模块设计方案（借鉴 ai-gateway）

> 状态：Draft（待评审）｜日期：2026-09-29｜范围：站内信 / 邮件 / IM 与 Webhook 渠道 / 偏好 / 模板 / 投递可靠性 / 审计与可观测
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
5. **幂等优先**：以业务 occurrence key 派生幂等键；重试不产生重复通知；
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

**只新增两块**：事件目录（Event Catalog）与渲染层（Template Renderer）；投递可靠性、租户隔离、幂等全部复用现有基座。

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

- 落地：代码内置默认（`internal/notification/catalog.go`）+ 平台可覆盖表 `notification_event_catalog`（仅平台管理员写 + 审计）；
- 目的：**消灭三套词表漂移**（N7）——偏好 DTO、投递归一化、模板全部从目录取值；
- 兼容：保留现有短词表映射，映射表由目录生成（不破坏已落库偏好行）。

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
- 解析顺序：租户覆盖 → 平台模板 → 代码内置兜底（保证"永远能渲染"）。

### 4.4 偏好与静默规则

- 扩展偏好模型（在现有 `notification_preferences` 上演进，避免新表并行）：

| 维度/字段 | 说明 |
|---|---|
| `user × event_type` | 现有唯一约束保留 |
| `channels` | 由 4 个 bool 演进为 `channels jsonb`（或按渠道行）；消费端改为"目录默认 ∩ 用户偏好 − 必达豁免" |
| `min_severity` | 低于阈值的通知只进 in_app |
| `mute_until` / `quiet_hours` | **真正消费**（当前仅存储，N7）；必达事件不受静默限制 |
| `frequency` | `immediate`（默认）/`daily`（P2 摘要） |

- **判定顺序**：目录必达 → 用户偏好 → 静默时段（仅非必达）→ 渠道可用性（connector/平台 SMTP 是否就绪）；
- **结果留痕**：被跳过时写 `notification_deliveries.status=skipped` + `skip_reason`（`preference_off` / `quiet_hours` / `channel_not_configured` / `no_target`），**不再"静默成功"**（借鉴 R5，修 N5）。

### 4.5 平台通道 vs 租户通道（回答 multi-tenant Q4）

| 通道 | 用途 | 配置来源 | 投递实现 |
|---|---|---|---|
| **平台 SMTP** | 邀请、密码重置、安全告警、平台公告 | `smtp.*`（全局）+ `SMTP_*` 环境变量 | `EmailService`（**接线**：bootstrap 构造 → 注入）→ 走 `notification.deliver`（`scope=platform`） |
| **租户 connector** | 业务通知（工单/变更/SLA）、租户邮箱收发 | `connector_configs`（租户级，加密） | 既有 `connectorManager.Send` |
| **in_app** | 全部租户 | 无 | 既有 |

规则：

1. `scope=platform` 事件只能走平台通道；`scope=tenant` 事件默认租户通道，租户未配置时**降级 in_app** 并记 `skip_reason=channel_not_configured`（可配置为失败告警）；
2. 邀请/密码重置等"用户尚无租户上下文"的场景**固定走平台 SMTP**；
3. 平台 SMTP 未配置：邀请接口返回 `inviteUrl` + `emailSent=false`；密码重置返回明确提示——**不再静默跳过**（修 N2）；
4. 平台 SMTP 与租户 connector **互不兜底**（禁止跨域发信），避免"用客户邮箱发平台邮件"的合规问题。

### 4.6 可靠性增强（在既有 outbox 之上）

| 增强 | 说明 | 备注 |
|---|---|---|
| 每渠道限流 | 令牌桶（租户+渠道维度），超限 `available_at` 顺延 | ai-gateway 有字段无执行（X4），ITSM 落到 worker |
| 去重窗口 | 同 `recipient+event+resource` 在 N 分钟内合并（内容相同才合并） | 幂等键保持"一次业务事件一条命令" |
| 优先级 | critical 立即；低优先级用 `available_at` 延迟 | 防止通知风暴 |
| 错误分类 | `4xx/永久`（地址非法、目标缺失）→ 直接死信；`5xx/网络` → 退避 | 现状统一重试浪费次数 |
| 死信与重放 | **保留**既有 `dead_letter` + 运维重放 API | 不照搬 ai-gateway 的"无死信"（X3） |
| 悬空补偿 | 扫描超时 `pending` 的投递记录并重新入队/对账（发送前落 pending 的崩溃窗口） | 修 N13；参考 RG 的 lease 重领 |

### 4.7 安全与合规

- 凭据：平台 SMTP 密码仅环境变量/加密配置；connector 凭据沿用 `CONNECTOR_CONFIG_ENCRYPTION_KEY` 加密与 fail-closed（既有）；
- 目标地址：仅 Handler 运行时在租户内解析；日志/审计只存掩码（既有约定，保持）；
- 审计：模板变更、必达清单变更、渠道启停、人工重发/取消 → `audit_logs`；
- PII：渲染变量与正文在落库前 redact（借鉴 ai-gateway `sensitivePayload`）。

### 4.8 可观测

| 维度 | 指标/入口 |
|---|---|
| 投递 | 按 `(tenant, channel, status, error_code)` 聚合成功率与失败 Top |
| 命令 | 复用 outbox 指标：pending 数、最老等待、重试率、死信数 |
| 渠道 | `connector` health（`lastSuccessAt/lastFailureAt`）+ `last_test_status` + 平台 SMTP readiness |
| 一致性 | 通知中心未读数与 `notifications.read=false` 计数对账（修"未读陈旧"） |
| 运维 | 现有 `/admin/operations/commands`（重放/取消）+ 新增按 delivery 的查询/重发 |

---

## 5. 数据模型变更

| 变更 | 内容 | 批次 |
|---|---|---|
| `notification_event_catalog`（新） | 平台级事件目录：`event_type/scope/severity/default_channels/mandatory_channels/allow_user_opt_out/variables_schema/aliases/status` | P0-3 |
| `notification_templates`（新） | 见 §4.3；`tenant_id NULL=平台` | P1-1 |
| `notification_deliveries` 扩展 | `event_type`、`channel_scope(platform\|tenant)`、`template_version`、`rendered_subject/body`（redacted）、`skip_reason` | P1-1/P1-3 |
| `notification_preferences` 扩展 | 渠道维度（`channels jsonb` 或渠道行）、`min_severity`、`mute_until`；**消费** `quiet_hours`/`frequency` | P1-2 |
| `notifications`（读侧）扩展 | `source`、`event_type`、`severity`、`expires_at`（借鉴 R3）；`type` 保留兼容 | P1-3 |
| 迁移策略 | 全部为加表/加列（在线 DDL）；回填 `notifications.event_type`（由 `type` 映射）；**无唯一约束变更**；每步可独立回滚 | — |

## 6. API 与事件契约

| 变更 | 说明 | 批次 |
|---|---|---|
| `GET /notification-event-types` | 从**目录**读取（替代 DTO 硬编码 15 项），返回 canonical + 别名 + 可关闭性 | P0-3 |
| `GET/POST/PUT /notification-templates` | 平台管理员管理平台模板；租户管理员管理本租户覆盖 | P1-1 |
| `POST /notification-channels/:name/test` | **真实测试发送**（替换前端 `setTimeout` 假成功） | P1-5 |
| `GET /admin/notifications/deliveries` | 运维查询（按 tenant/event/channel/status 过滤） | P1-3 |
| `POST /admin/notifications/deliveries/:id/retry` | 人工重发（写审计） | P1-3 |
| 邀请/密码重置 | `POST /users/invitations` → `{inviteUrl, emailSent}`；`POST /auth/password-reset` → `{emailSent}`（未配置 SMTP 时 `false` + 提示） | P0-1 |
| 事件命名 | canonical 保留现短词（`ticket_created`…）；目录登记别名（`created`、`approval_completed` 等），偏好/模板/归一化统一走目录 | P0-3 |
| 兼容 | 前端 `/notification-preferences` 的 `/me`、`/templates`、`/apply-template` 要么实现、要么从 API client 移除（二选一，禁止悬空） | P1-2 |

---

## 7. 分期实施计划

### P0（修复批次，阻塞级）

| # | 任务 | 内容 | 对应缺口 |
|---|---|---|---|
| P0-1 | 平台 SMTP 接线 | bootstrap 依 `smtp.enabled` 构造 `EmailService` → 注入 `handlers/auth`；密码重置/邀请改走 `notification.deliver(scope=platform)`；未配置时 API 显式返回（`emailSent=false` + 提示） | N1/N2；multi-tenant Q4 |
| P0-2 | 旁路清理 | SLA critical 直发改走 outbox；删除 `TicketNotificationService` 旧同步路径（含 N×N 缺陷）；生产强制 `EnableTxOutbox`（fail-closed） | N3/N4 |
| P0-3 | 事件目录 v1 | canonical 词表 + 别名；替换 handler 归一化、偏好 DTO、前端事件列表的硬编码 | N7（前半） |
| P0-4 | 测试基线 | 通知 controller/service 测试；outbox 故障注入（重试/死信/围栏）；各渠道 mock E2E | N10 |

### P1（模块化）

| # | 任务 | 内容 | 对应缺口 |
|---|---|---|---|
| P1-1 | 模板与渲染 | `notification_templates` + 渲染器 + 管理 API + 三级兜底（租户→平台→代码） | N6 |
| P1-2 | 偏好升级 | 渠道维度、`min_severity`、`mute_until`；**消费** `quiet_hours/frequency`；必达白名单；前端 API 对齐（实现或移除） | N7（后半） |
| P1-3 | 投递可解释 | `skipped + skip_reason` 落库；`notifications` 读侧扩展（`source/event_type/severity/expires_at`）；运维 deliveries 查询/重发 API | N5 |
| P1-4 | 实时推送 | 通知落库后 `SendToUser` 推送；前端由轮询改订阅（含未读对账） | N8 |
| P1-5 | 渠道治理 | 真实"测试发送"、渠道健康视图、`webhook` 目标按租户解析、平台 SMTP readiness | N6/N9 |

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
| A1 | 平台 SMTP 未配置 → 发起邀请 | `200 {inviteUrl, emailSent:false}`；无静默失败；`notification_deliveries` 有 `skipped(channel_not_configured)` 或明确无邮件意图 |
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

---

## 9. 风险与开放问题

### 9.1 待决策（D1–D6）

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

## 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v0.1 | 2026-09-29 | 首版：ITSM 现状盘点（含 N1–N12 缺口清单）、ai-gateway 通知模块剖析（R1–R8 借鉴 / X1–X5 不照搬）、目标架构（事件目录/模板/偏好/平台-租户双轨/可靠性/可观测）、数据模型与 API 契约、P0–P2 分期、验收矩阵、风险与 D1–D6 决策项 |
| v0.2 | 2026-09-29 | 并入调研增量：新增 N13（投递审计与发送非同事务→悬空补偿）；N8 升为 P0（未读陈旧实测）、N9 补充"sms 偏好可开但必失败"；§3 补充 RG outbox 的 dedupe/silent 窗口与 dead_letter 落地样本、真实测试发送、用户 inbox 游标分页与未读汇总；§4.6 增加悬空补偿 |
