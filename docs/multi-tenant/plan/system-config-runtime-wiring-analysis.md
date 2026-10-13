# 系统配置运行时接入分析与改造方案（/admin/system-config）

> 状态：**Draft v0.1（分析完成，待评审排期）**｜日期：2026-10-13｜基准：仓库 HEAD `8a7c03be`；238 演示栈 `1.8.0-dev.5e4eab85`（DB `itsm_demo`）
> 定位：**系统配置页字段的"存储 → 运行时生效"落差分析 + 租户级邮件 / 会话 / 账户安全策略接入方案**。起因：用户提问「不同租户都设置邮件服务器，是否会同时生效」。
> 关联：[演示环境指南](../demo-environment.md)｜[canon（L1 概念权威）](./msp-concept-model-and-architecture-canon.md)｜[多租户 INDEX](../INDEX.md)｜[backlog](../backlog.md)
> 读者：架构评审、后端/前端研发、测试 QA、产品决策。
> 局部编号（引用须带前缀）：`SC1–SC6`（现状结论）、`SCD1–SC6`（设计决策）、`SCA1–SC6`（验收）、`SCR1–SCR4`（风险）。

---

## 0. 摘要（TL;DR）

`/admin/system-config` 的字段**按租户独立存储**（`system_configs.tenant_id`，数据面已实测隔离），
但**运行时的"读取面"覆盖不齐**：一部分字段真正按租户生效，一部分**完全没有读取方**（存而不用），
还有一类是**接口已按租户设计、装配层仍接静态/全局配置**。

| 字段组（页面页签） | 键 | 存储 | 运行时读取方 | 是否按租户生效 |
|---|---|---|---|---|
| 安全设置·密码策略 | `passwordMinLength` / `passwordRequire*` | 租户隔离 | `SystemConfigService.GetPasswordPolicy(tenantID)`（租户缓存 + 保存失效） | ✅ **是** |
| 安全设置·账户安全 | `loginMaxAttempts` / `accountLockoutDuration` / `enable2FA` | 租户隔离 | **无** | ❌ 否（存而不用） |
| 会话设置 | `sessionTimeout` | 租户隔离 | **无**（JWT TTL 由部署级 `JWT_EXPIRE_TIME` 决定） | ❌ 否（存而不用） |
| 邮件设置·SMTP/模板 | `smtpHost/Port/Username/Password/EnableSSL`、`emailFrom`、`systemNotificationTemplate` | 租户隔离 | **无** | ❌ 否（存而不用） |
| 基础信息 | `systemName/systemUrl/timezone/...` | 租户隔离 | 前端读取展示（页面自身）；后端未消费 | ⚠️ 仅 UI 展示 |
| 附件域灰度 | `attachment.*` | 租户隔离 | `TicketAttachmentFlags` 接口按 tenantID 设计，**装配为静态 `cfg.Attachment.*`** | ❌ 否（接口已预留，装配未接） |
| AI/MCP 能力开关 | `ai-capabilities`（capability 域） | 租户隔离 | `capability.ConfigSource`（按租户查询 + 缓存） | ✅ 是 |
| 邮件接入模式 | `email_intake.*` | 租户隔离 | `handlers/email_intake/orchestrator.go`（按租户查询） | ✅ 是 |

**对用户问题的直接回答**：不同租户各自设置邮件服务器 → **既不会"同时生效"，也不会"各自生效"**：
通知/重置密码邮件的发送服务 `EmailService` 在装配层**从未被实例化**（发信点 nil 守卫静默跳过）；
唯一真实的发信路径（事件告警）读的是**部署级环境变量**（`SMTP_HOST`/`alerting.smtp.*`），全局一套。

---

## 1. 背景与范围

### 1.1 起因

演示环境（`http://172.18.3.238:8089`）使用 `/admin/system-config` 时提出：
1. 该页是跨租户还是租户隔离？（已实测：**租户隔离**，见 §2.1）
2. 「安全设置」「邮件服务器」如果不同租户都配置，是否会同时生效？（本文回答，见 §2.3/§2.4）

### 1.2 范围

- 覆盖：`system_configs` 表驱动的全部字段组的**运行时读取面**盘点；邮件发送链路（通知/告警/连接器）边界；
  会话与账户安全字段的生效面；租户级邮件配置的改造方案与分期计划。
- 非目标：连接器市场「IMAP/SMTP 邮箱」自身能力（收件/回复）的改造；通知模板体系重构；计费/配额类配置。

---

## 2. 现状事实（证据）

### 2.1 数据面：租户隔离（已实测）

- 表结构：`system_configs.tenant_id`，索引 `(tenant_id, category)`、`(tenant_id, key)`（`ent/schema/systemconfig.go:38-42`）；
- 读写路径全带租户：`handlers/systemconfig/routes.go:26-43`（从 JWT 上下文取 `tenant_id`）、
  `service/system_config_service.go:123-169`（查询/更新均 `TenantIDEQ(tenantID)`，更新存在性检查 `ID + tenant_id`）；
- 实测（238 演示库）：同 key `systemName` 三个租户各一行（id 6/62/90，值互不相同）；
  跨租户按 ID 直读（mfg 读 med、med 读 mfg、平台超管读 mfg）**全部 404**；
  MSP 路径通道（`X-Customer-Tenant-ID`）**403**；服务商**切换租户**进入客户作用域后 `system-configs` 仍 **403**
  （同会话 `/tickets` 200）→ 系统配置**不在 MSP 跨租户数据面内**。
- 结论：**存储与 API 是租户隔离的**；本文的落差全部发生在"运行时消费侧"。

### 2.2 字段清单（默认键，`service/system_config_service.go:286-321`）

| 类别 | 键 |
|---|---|
| general | `systemName` `systemUrl` `timezone` `language` `dateFormat` `timeFormat` |
| session | `sessionTimeout` |
| upload | `maxFileSize` `allowedFileTypes` |
| attachment | `attachment.generic_read_enabled` `generic_write_enabled` `dual_write_enabled` `inline_image_enabled` |
| security（密码策略） | `passwordMinLength` `passwordRequireUppercase` `passwordRequireLowercase` `passwordRequireNumbers` `passwordRequireSpecialChars` |
| security（账户安全） | `loginMaxAttempts` `accountLockoutDuration` `enable2FA` |
| email（SMTP/模板） | `smtpHost` `smtpPort` `smtpUsername` `smtpPassword` `smtpEnableSSL` `emailFrom` `systemNotificationTemplate` |

页面页签与字段：`itsm-frontend/src/pages/(main)/admin/system-config/index.tsx`（基础信息 / 会话 / 上传 / 安全设置 / 邮件设置）。

### 2.3 逐组读取面结论（全仓检索 + 运行验证）

**SC1｜密码策略：按租户生效（唯一完整闭环的字段组）**
- `service/password_policy.go:92-164`：`GetPasswordPolicy(ctx, tenantID)` 按租户查询 5 个键，
  **租户级缓存（TTL 5min）+ 保存即失效**（`InvalidatePasswordPolicy(tenantID)`，`system_config_service.go:83/198/248/275/354`）；
- 消费点：注册、找回/重置密码（`handlers/auth/service.go:369+`）、邀请接受（`invitation_service.go:595,599`）、
  租户用户治理重置/生成（`tenant_user_admin.go:390,406,763`）。
- 结论：不同租户不同策略，**各自生效、互不影响**。

**SC2｜账户安全（`loginMaxAttempts`/`accountLockoutDuration`/`enable2FA`）：无读取方**
- 全仓仅出现在默认键清单（`system_config_service.go:310-312`），无任何业务读取 → **存而不用**。

**SC3｜会话超时（`sessionTimeout`）：无读取方**
- 全仓仅默认键（`system_config_service.go:294`）；JWT 有效期来自部署级 `JWT_EXPIRE_TIME`（`docker-compose.prod.yml` env）→ **全局**。

**SC4｜SMTP / 发件人 / 模板：无读取方**
- 全仓检索 `smtpHost|smtpPort|smtpUsername|smtpPassword|smtpEnableSSL|emailFrom`：
  - 命中 1：默认键清单（`system_config_service.go:314-320`）；
  - 命中 2：连接器 `connector/builtin/email/email.go`（**另一套 settings**，见 SC6）；
  - 命中 3：事件告警读**环境变量**（`service/incident_alerting_service.go:361-388`，`SMTP_HOST`/`alerting.smtp.*`）；
  - **没有**任何代码用 `tenant_id` 读取这些键用于发信。

**SC5｜邮件发送服务在装配层缺失（关键缺口）**
- `service/email_service.go:28-60`：`EmailService` 持有**进程级单例 `EmailConfig`**；`Send` 用 `smtp.SendMail`（同文件 `:132`）。
- 全仓 `NewEmailService(...)` **仅测试引用**；`SetEmailService(...)`（`handlers/auth/service.go:43`、
  `service/ticket_notification_service.go:53`）**无生产调用方** → 生产运行中 `emailService == nil`。
- 所有发信点均有 nil 守卫、**静默跳过**：
  - 工单通知邮件：`service/ticket_notification_service.go:170`（`if s.emailService != nil`）；
  - 找回密码邮件：`handlers/auth/service.go:361`（同上）。
- 结论：**通知类邮件在当前构建中根本不会发出**（无论租户或部署级 SMTP 怎么配）。

**SC6｜真实可用的两条邮件链路（均不是系统配置页的 SMTP）**
- ① **事件告警**：`IncidentAlertingService`（`internal/bootstrap/app.go:417,741-742`）——读部署级 env（全局），
  用于监控告警外发；与租户 system-config 无关。
- ② **邮箱连接器**：`connector/builtin/email`（IMAP/SMTP，settings 含 `smtpHost` 等）——按租户安装配置，
  用于**收件接入 / 回复邮件**（`handlers/email_intake/orchestrator.go`，读租户键 `email_intake.*`）；
  与系统配置页 SMTP **不是同一套**。

**SC7｜附件域灰度：接口按租户设计，装配接静态**
- `service/ticket_attachment_service.go:44-55` 定义 `GenericReadEnabled(tenantID)/GenericWriteEnabled(tenantID)`；
  但装配为 `StaticTicketAttachmentFlags`（`internal/bootstrap/app.go:406-407`，取自 `cfg.Attachment.*`）→
  `system_configs.attachment.*` 的租户覆盖**未接入**（同类落差的"接口已预留"变体）。

### 2.4 安全观察

- `smtpPassword` 以**明文**存储于 `system_configs`，且页面直接回填（`index.tsx` 邮件设置页签）；
  对比：LLM Provider 密钥已有 `LLM_PROVIDER_ENCRYPTION_KEY` 加密先例（`docker-compose.prod.yml` env）。

### 2.5 页面文案与实际行为的落差

- 文案：「本页管理**全局**默认值……修改会影响所有用户」——实为**租户内**全局；
- 文案：「保存后触发一条真实通知（如工单分配）验证邮件送达」——当前**不会触发任何邮件**（SC5），
  用户按提示操作会得到"保存成功但无效果"的误导体验。

---

## 3. 影响分析

| # | 影响面 | 说明 |
|---|---|---|
| 1 | 多租户邮件隔离 | 通知/重置密码邮件无法按租户发信（品牌、域名、送达通道均不可配）；SaaS/MSP 场景下这是硬需求 |
| 2 | 安全 | `smtpPassword` 明文入库 + 明文回显；`enable2FA`/`loginMaxAttempts`/`accountLockoutDuration` 存而不用，页面给人以"已启用安全策略"的错觉 |
| 3 | 体验/信任 | 配置页"保存成功但无效果"；演示环境同样如此，影响对外演示说服力 |
| 4 | 会话治理 | `sessionTimeout` 未生效，无法按租户收敛会话时长 |
| 5 | 双轨配置 | 「系统配置 SMTP」与「邮箱连接器 SMTP」两套并存且语义不同，运维/客户易混 |
| 6 | 附件灰度 | `attachment.*` 租户覆盖未接入，灰度只能全局切换 |

---

## 4. 目标与非目标

**目标**
- G1：系统配置页各字段组的"是否生效"有**明确结论与出口**：生效 / 租户化改造 / 明确下线（含 UI 调整）；
- G2：邮件配置**按租户生效**（通知与重置密码），并保留部署级 env 兜底；
- G3：SMTP 凭据**加密存储 + 掩码回显 + 审计**；
- G4：会话/账户安全字段给出**租户化或下线**的产品决策并落地其一；
- G5：消除"双轨 SMTP"的歧义（文档 + 页面提示）。

**非目标**
- 不做邮件模板体系重构（仅消费 `systemNotificationTemplate` 现键）；
- 不改连接器邮箱的收件/回复能力；不做 SMTP 送达回执/重试平台化（P2 可选项）。

---

## 5. 方案设计

### SCD1｜统一"租户配置解析器"（对齐密码策略模式）

新增 `TenantConfigResolver`（建议落在 `service/system_config_resolver.go`）：

```go
type TenantConfigResolver interface {
    GetString(ctx context.Context, tenantID int, key string) (string, bool)
    GetBool(ctx, tenantID, key) (bool, bool)
    GetInt(ctx, tenantID, key) (int, bool)
}
```

- 实现复用 `system_configs` 按租户查询；**租户级缓存 + 保存即失效**（与 `password_policy.go` 同模式，避免各自造缓存）；
- 语义：**租户配置 > 部署级 env > 内置默认**；读取失败 fail-safe（保持默认，不因配置异常放行/阻断）；
- `InvalidatePasswordPolicy(tenantID)` 泛化为 `InvalidateTenantConfig(tenantID)`（兼容旧名保留）。

### SCD2｜邮件服务改造：单例 → 按租户解析

- `EmailService` 从"持有单例 EmailConfig"改为**每次发送前按 tenantID 解析配置**（`SCD1`），
  或引入 `MailerFactory(tenantID) (*EmailService, error)`（推荐后者：保持 `EmailService` 无状态、便于测试与连接池复用）；
- 发信点补 tenantID（来源：`ticket.tenant_id` / 收件人 `user.tenant_id` / 重置令牌归属租户）：
  - `service/ticket_notification_service.go:170`（通知）；
  - `handlers/auth/service.go:361`（重置密码）；
  - `service/sla_alert_service.go:546`、`ticket_notification_service.go:557`（SLA 邮件）；
- 装配：`internal/bootstrap/app.go` 注入 resolver/factory；无租户配置且 env 未配置时**明确记录日志并跳过**（不再静默）。

### SCD3｜事件告警邮件统一

- `IncidentAlertingService.getAlertChannels`（`incident_alerting_service.go:355-388`）改为经 `SCD1` 解析：
  租户配置存在 → 用租户 SMTP；否则回退部署级 env（保留现行为）；
- 统一后，"告警邮件"与"通知邮件"共用同一配置源，消除双轨。

### SCD4｜SMTP 凭据治理

- 存储加密：`smtpPassword` 写入前 AES-GCM 加密（主密钥来源：优先 `LLM_PROVIDER_ENCRYPTION_KEY` 同源派生，
  或新增 `CONFIG_SECRET_KEY`，二选一由决策点 D2 定）；读取解密仅在发送侧；
- API 回显掩码：`ListConfigs/GetConfig` 对 `smtpPassword` 返回 `"******"`（或空串 + `hasValue: true`），
  仅 `PUT` 可写、不回显明文；
- 审计：SMTP 配置变更写 `audit_logs`（含 `changed_keys`，不含明文值）。

### SCD5｜会话 / 账户安全字段决策（二选一，须产品拍板）

- 选项 A（租户化）：`sessionTimeout` 在**签发与刷新 JWT**时按租户读取（`JWT_EXPIRE_TIME` 作为兜底/上限）；
  `loginMaxAttempts`/`accountLockoutDuration` 接入登录限流与锁定（当前登录限流为进程级 10/min）；
  `enable2FA` 未实现前**从 UI 隐藏或标注"未启用"**；
- 选项 B（下线/隐藏）：未实现字段统一标注"预留（未生效）"，页面加统一提示条，避免"配置了以为生效"；
- 推荐：**密码策略维持现状（已生效）**；`sessionTimeout` 走 A（改动小、价值明确）；
  `loginMaxAttempts/accountLockoutDuration/enable2FA` 走 B（2FA 属独立项目，先标注）。

### SCD6｜附件灰度接入（顺带收口）

- 将 `StaticTicketAttachmentFlags` 替换为 `TenantConfigResolver` 驱动的实现
  （`attachment.generic_read_enabled` 等 4 键，按租户读，缺失回退 `cfg.Attachment.*`）；
- 与 SCD1 同批交付，回归 `handlers/ticket_attachment` 现有契约测试。

### 边界澄清（写入文档与页面提示）

| 场景 | 配置位置 | 生效范围 |
|---|---|---|
| 系统通知/重置密码邮件 | `/admin/system-config` → 邮件设置（改造后） | 租户 |
| 监控告警邮件 | `/admin/system-config` → 邮件设置（改造后，env 兜底） | 租户 > 部署 |
| 邮箱收件/回复（IMAP/SMTP） | 连接器市场 → IMAP/SMTP 邮箱 | 租户（连接器安装配置） |

---

## 6. 分阶段实施计划

### P0（必做，1 个迭代内可闭环）

| 项 | 内容 | 交付物 |
|---|---|---|
| P0-1 | `SCD1` 解析器 + 缓存/失效 | `service/system_config_resolver.go` + 单测（租户隔离/缓存失效/回退链） |
| P0-2 | `SCD2` 邮件按租户解析 + 装配 + 发信点补 tenantID | `email_service` 改造 + `bootstrap` 注入 + 4 处调用点 |
| P0-3 | `SCD4` 凭据加密 + 掩码回显 + 审计 | 迁移脚本（存量明文加密回填）+ API/前端掩码 + 审计 |
| P0-4 | 页面文案修订（"全局"→"本租户"；删除/修正"保存后触发真实通知"误导句；未生效字段标注） | `index.tsx` / `EnhancedSystemConfig.tsx` |
| P0-5 | 回归：密码策略、通知、重置密码、附件灰度既有测试 | 测试报告（见 §7） |

### P1（下一迭代）

- `SCD3` 告警邮件统一（env 兜底）；
- `SCD5` 选项 A：`sessionTimeout` 租户化（签发/刷新读取，env 为上限）；
- `SCD6` 附件灰度按租户接入（替换静态 flags）；
- 页面「测试发信」按钮（调用租户 SMTP 发送一封测试邮件，替代"保存后触发真实通知"的错误指引）。

### P2（可选）

- 邮件送达重试/退避与失败可见性（复用 outbox）；
- 租户级发件人名称/签名/模板变量扩展；
- 登录失败锁定（`loginMaxAttempts` 真接入，替代/叠加进程级限流）。

---

## 7. 验收标准（SCA）

| # | 场景 | 验收 |
|---|---|---|
| SCA1 | 租户 A/B 各配不同 SMTP（mock SMTP 断言） | A 的通知/重置密码邮件从 A 的服务器发出，B 不受影响；互不覆盖 |
| SCA2 | 租户未配 SMTP，部署 env 已配 | 回退 env 发信并记录一次 warn；env 未配则明确日志跳过（不静默） |
| SCA3 | 保存 SMTP 后立即发信 | 缓存按租户失效，无需重启/等待 TTL（对齐密码策略的"保存即生效"） |
| SCA4 | `smtpPassword` 读写 | DB 为密文；API/页面回显掩码；日志/审计不含明文 |
| SCA5 | 密码策略回归 | 不同租户不同复杂度策略各自生效（既有用例全绿） |
| SCA6 | 附件灰度（P1） | 租户 A 开读路径、租户 B 关闭：同一实例内并行验证 |

**测试清单**：解析器单测（隔离/缓存/失效/回退/脏值）；EmailService 工厂单测；通知与重置密码集成测试
（sqlite + mock SMTP）；告警统一回归；前端配置页掩码与提示的组件测试；238 演示栈端到端（A/B 租户双 SMTP mock）。

---

## 8. 风险与回滚（SCR）

| # | 风险 | 缓解 |
|---|---|---|
| SCR1 | 存量 `smtpPassword` 明文迁移失败/密钥丢失 | 迁移幂等 + 迁移前备份；加密 key 写入部署文档；密文无法解密时按"未配置"处理并告警 |
| SCR2 | 按租户解析引入发送延迟 | 与密码策略同款缓存（TTL 5min）+ 保存失效；发送路径不引入额外 DB 往返（缓存命中） |
| SCR3 | 回退链改变既有行为（env 发信变租户发信） | 灰度开关：`MAIL_TENANT_CONFIG_ENABLED`（默认 on，可回退 env-only） |
| SCR4 | 会话超时租户化导致存量会话提前失效 | `sessionTimeout` 只作用于**新签发/刷新**的令牌；不追溯已签发令牌 |

**回滚**：P0 全部变更均可用开关回退到"env-only + 明文读取"旧行为；DB 侧加密回填不可逆，回滚需保留解密读兼容分支（保留 1 个版本）。

---

## 9. 开放决策点

| # | 决策 | 建议 |
|---|---|---|
| D1 | `enable2FA` / `loginMaxAttempts` / `accountLockoutDuration`：租户化 or 标注预留 | 标注预留（2FA 独立立项）；锁定策略 P2 再做 |
| D2 | SMTP 加密主密钥来源 | 复用 `LLM_PROVIDER_ENCRYPTION_KEY` 派生 vs 新增 `CONFIG_SECRET_KEY`（推荐新增，职责分离） |
| D3 | 平台超管是否可代配/查看租户 SMTP | 建议：不可查看明文；可经"重置密码式"流程触发租户侧配置（或不做，保持租户自治） |
| D4 | 系统配置 SMTP 与邮箱连接器的收敛方向 | 保持两套但文档+页面显式区分（推荐）；长期评估连接器作为唯一邮箱出入口 |

---

## 附录 A：证据索引（file:line）

| 事实 | 位置 |
|---|---|
| system_configs 索引 `(tenant_id,key)` | `itsm-backend/ent/schema/systemconfig.go:38-42` |
| 列表/更新按租户 | `itsm-backend/handlers/systemconfig/routes.go:26-43`、`service/system_config_service.go:123-169` |
| 默认键清单（含 SMTP/会话/账户安全） | `itsm-backend/service/system_config_service.go:286-321` |
| 密码策略按租户 + 缓存/失效 | `itsm-backend/service/password_policy.go:92-164` |
| EmailService 单例配置 + smtp.SendMail | `itsm-backend/service/email_service.go:19-34,132` |
| `NewEmailService/SetEmailService` 无生产调用方 | 全仓检索（仅测试引用；`handlers/auth/service.go:43`、`service/ticket_notification_service.go:53` 为定义） |
| 通知邮件 nil 守卫 | `itsm-backend/service/ticket_notification_service.go:170` |
| 重置密码邮件 nil 守卫 | `itsm-backend/handlers/auth/service.go:361` |
| 事件告警读 env SMTP | `itsm-backend/service/incident_alerting_service.go:361-388`；装配 `internal/bootstrap/app.go:417,741-742` |
| 邮箱连接器（租户级 settings） | `itsm-backend/connector/builtin/email/email.go:50-85`；接入 `internal/bootstrap/app.go:1231-1238` |
| 附件 flags 接口 vs 静态装配 | `itsm-backend/service/ticket_attachment_service.go:44-55`；`internal/bootstrap/app.go:406-407` |
| capability 按租户读取 | `itsm-backend/capability/capability.go:205-216` |
| 页面文案与字段 | `itsm-frontend/src/pages/(main)/admin/system-config/index.tsx:549,555`（及邮件/安全页签） |

## 附录 B：实测记录（238 演示栈，2026-10-13）

- 数据面：同 key `systemName` 三租户各一行（id 6/62/90），值互不相同；跨租户按 ID 直读全部 404；
  平台超管读他租户行 404；MSP 路径通道 403；切换租户后 `system-configs` 仍 403（`/tickets` 200）。
- 写入面：三租户分别批量写 `systemName`，互不影响；写入后按租户失效密码策略缓存（代码路径确认）。
- 读取面：`smtpHost/smtpPort/smtpUsername/smtpPassword/smtpEnableSSL/emailFrom`、`sessionTimeout`、
  `loginMaxAttempts/accountLockoutDuration/enable2FA` 在全仓无业务读取方（仅默认键清单）。
