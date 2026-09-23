# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added

- **工单详情页图片查看器** — `/tickets/:id` 描述内的富文本图片可点击（或回车 / 空格）放大查看：放大 / 缩小（按钮、滚轮、`+` / `-`）、原始尺寸 1:1、适应窗口（默认，且不把小图放大）、左 / 右旋转 90°、水平 / 垂直翻转、放大后拖动平移、重置、多图左右切换、下载原图，Esc / 点击遮罩退出并把焦点还给原图片（`src/components/business/RichTextImageViewer.tsx` + `src/lib/rich-text/image-viewer.ts`，设计见 `docs/architecture/ticket-create-page-rich-input-optimization.md` §4.9）
- **通用附件 API（A1-A6）** — 新增与宿主解耦的附件端点：`POST/GET /api/v1/attachments`（上传 / 列表）、`POST /api/v1/attachments/batch-query`（批量元数据回填）、`GET /api/v1/attachments/:id`、`GET /api/v1/attachments/:id/content`（下载 / 预览，支持 Range）、`DELETE /api/v1/attachments/:id`（软删，被宿主正文 / 评论引用时返回 409/6105）。权限码 `attachment:read/write/delete`（系统管理员 / 租户管理员默认绑定），宿主维度（工单 / 知识库等）按 `§4.2` 权威表二次校验，跨租户与不存在的宿主一律 404/6101；新增 6101-6107 错误码段（宿主不存在 / 文件名非法 / 超限 / 类型不允许 / 被引用 / 配额 / 频率），全部在 `Fail`/`FailWithData` 双 switch 登记（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-4）
- **附件域内别名路由（知识库 / 服务请求）** — 知识库文章与服务请求接入通用附件 A1/A2/A4/A5：`GET|POST /api/v1/knowledge/articles/:id/attachments`、`GET|DELETE /api/v1/knowledge/articles/:id/attachments/:ref`（含 `/download`、`/preview`），以及 `/api/v1/service-requests/:id/attachments` 同形路径；静态权限复用宿主码（`knowledge:read/write/delete`、`service_request:read/write/delete`，零新增权限码）。附件必须归属路径宿主：跨宿主读取 / 删除按 404 处理，非法宿主 ID 与非法附件引用同样 404（不泄露资源存在性，且先于文件流输出检查）；服务请求宿主以 `reason` 正文参与删除引用保护（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-5）
- **旧工单附件端点接入通用后端（灰度切换）** — `/api/v1/tickets/:id/attachments` 系列端点（列表 / 上传 / 下载 / 预览 / 删除）在 `attachment.generic_read_enabled` / `attachment.generic_write_enabled` 打开后改走通用附件服务（部署级 `config.AttachmentConfig` + 租户级 `system_config` 覆盖，默认 `false`，关闭即回退旧表且 URL 不变）。响应 JSON 字段、状态码与错误文案与改造前**逐字段一致**（含 `uploader` 嵌套字段、`fileUrl` 的 `.../attachments/{id}/preview` 形态与历史「存储文件名」引用）；灰度期未回填的历史记录按未命中回退旧表，历史富文本 URL 不失效。契约测试 `handlers/ticket_attachment/handler_contract_test.go`（实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-6）
- **内嵌图片引用完整性（写入期归属校验）** — 正文富文本里的 `<img data-attachment-id>` 在写入工单描述（新建 / 更新）与知识库文章时逐条校验宿主归属：A4 规范地址（`/api/v1/attachments/{id}/...`）必须命中「同租户 + 同宿主 + 存活」且与 `data-attachment-id` 一致；旧工单域内地址在工单 ID 不匹配时剥离；其它地址只在能解析到通用记录且宿主不符 / 已软删时剥离（`查不到` 不误伤，保障灰度期旧表引用）。违规引用整标签剥离并以 `Warnw` 逐条告警（`attachment_id` / `reason` / `src`），校验查询失败不阻塞写入（`service/attachment_refs.go`，实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-7）
- **附件生命周期清理与宿主删除级联（BE-8）** — 附件删除保持「仅软删」，新增后台清理任务按租户回收「软删且超过保留期」的附件：先删物理文件、后删元数据行，单条失败下轮重试；仍被宿主正文 / 评论引用的记录一律跳过（不误删）。配置 `attachment.cleanup_enabled`（默认 false，关闭即任务不注册）、`cleanup_purge_enabled`（默认 false = 演练 dry-run，只统计不落删）、`retention_days`（30 天）、`cleanup_interval_minutes`（360）、`cleanup_batch_size`（200，硬上限 1000），仅部署级生效。工单删除与知识库文章删除后按策略级联软删其附件（`SetAttachmentLifecycle` 注入，未注入时零行为变化）；被引用项保持 active，引用解除并过保留期后才进入回收序列（`service/attachment_cleanup.go`，实现与验收见 `docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-8，演练记录见 `docs/testing/attachment-cleanup-drill-2026-09-22.md`）

### Security

- **登录/刷新响应令牌收敛** — access token 和 refresh token 不再通过 JSON 响应返回，改为仅通过 HttpOnly cookie 下发，防止 XSS 窃取
- **通用附件读路径宿主归属校验** — 旧工单端点灰度走通用附件服务时，下载 / 预览此前只按「附件 ID + 租户」取流；灰度期 `attachments` 与 `ticket_attachments` 主键序列独立、同号是常态，持有工单 A 路径的用户可读到工单 B（乃至其它 `biz_type`）的同号附件内容。现新增宿主定向读取 `AttachmentService.GetFileForHost`（`biz_type + biz_id + tenant_id` 三元一致，归属不符 / 已软删一律按未命中处理并回退旧表），并补回归用例（已做负向验证：去掉宿主过滤该用例立即红灯）
- **内嵌图片跨宿主引用剥离** — 富文本正文里的 `data-attachment-id` 此前只做属性白名单，不校验引用是否属于当前宿主，可把别处（其它工单 / 其它域）的附件写进自己的正文并留下跨宿主引用；现于工单与知识库写入路径做归属校验并剥离，详见上方 Added 条目

### Fixed

- 修复 A3 元数据端点可读取软删记录：`GET /api/v1/attachments/:id` 此前未过滤 `status='active'`，附件软删后元数据仍可读取（内容侧 `GetFile`/`List`/`BatchGet` 早已过滤，读口径不一致）；现与 A4/A2/A6 对齐，软删记录一律 404（BE-8 收口 BE-6 遗留观察，回归用例 `TestAttachmentGetHidesSoftDeletedMetadata`）
- 修复知识库引用复核未排除已删除文章：`KnowledgeArticle` 未纳入全局软删拦截器，文章删除后其正文中的内嵌图片仍被判定为「被引用」，导致这些附件永远无法进入回收序列；现引用复核显式要求文章 `deleted_at IS NULL`（BE-8）
- 修复知识库正文内的附件回链（`data-attachment-id`）与图片对齐（`data-align`）在落库时被静默抹掉：知识库正文走 `common.SanitizeHTML`，bluemonday 的 UGCPolicy 默认剥离全部 `data-*` 属性，导致富文本图片与附件的关联、以及删除时的引用保护双双失效；现显式放行并按取值约束（附件 ID 仅数字、对齐仅 left/center/right），`on*` / `script` 等既有防护不变（`common/sanitizer.go`）
- 修复同一请求被打印两条 `[GIN]` 访问日志（两条耗时通常相差不到 1ms，易被误判为前端重复请求）：HTTP 引擎改用 `gin.New()`，不再与 `router.SetupRoutes` 中注册的 `gin.Logger()` / `gin.Recovery()` 叠加（回归测试 `internal/bootstrap/http_engine_test.go`）
- 修复侧边栏菜单「点一次、请求两次」：`MenuItems.tsx` 同时挂了 label 层与 `items[].onClick`（互为兜底），点击时事件冒泡导致 `router.push` 连续执行两次，浏览器发出两条完全相同的 `_rsc` 请求、服务端重复渲染。`Sidebar.tsx` 的 `handleMenuClick` 增加同路径 400ms 去重，保留两层兜底不变；实测同一次点击 `_rsc` 由 2 条降为 1 条，单条耗时由 3.5~4.8s 降至 0.46s
- 修复点击「待我审批」崩溃成 `Application error: a client-side exception has occurred`：目标 `/approvals/pending` 是 `redirect('/approvals')` 的兼容页，客户端导航进入该重定向路由时抛 React `Rendered more hooks than during the previous render`。侧边栏路径规范化表新增 `/approvals/pending` → `/approvals`（与 `/admin/index` 同款处理），同时省去一次整页 RSC 渲染
- 修复生产环境数据库迁移失败问题：部分迁移脚本内嵌事务控制语句导致整批迁移中止
- 修复生产部署登录失败：docker-compose 默认 RLS 模式从 `enforce` 改为 `off`，避免未携带租户上下文的公共路由（登录/注册）返回 401
- 修复新建用户返回 400：后端补齐创建路径的密码策略校验与错误信息映射，前端表单同步密码策略提示（排查记录见 `docs/archive/bug-reports/user-create-400-password-policy-2026-09-20.md`）
- 修复富文本编辑器插入的图片不显示：附件下载/预览路由（`GET /api/v1/tickets/:id/attachments/:attachment_id[/download|/preview]`）此前未注册导致全部 404；现补注册并同时兼容「数字附件 ID」与旧版 `fileUrl` 的存储文件名，新上传统一返回 `.../attachments/{id}/preview`（内联渲染，规避 `Content-Disposition: attachment`）
- 修复创建工单页插入的图片保存后消失（同一编辑器在编辑页却正常）：两处客户端缺陷导致「暂存图」链路断在提交层——Tiptap 图片节点未声明 `data-attachment-id` 属性（`insertContent` 时被 schema 静默剔除，`getHTML()` 丢标记，提交后既不触发附件上传也不回填正式地址），以及前端 DOMPurify 默认 URI 策略剥掉同源 `blob:` 占位图；现补属性声明并放行同源 `blob:`，创建页插图经「上传附件 → 回填 `/attachments/{id}/preview`」后正常落库与回显（排查与约束见 `docs/architecture/ticket-create-page-rich-input-optimization.md` §4.8）
- 修复全局限流响应实际回落为 HTTP 200：`middleware/security.go` 直接把 `http.StatusTooManyRequests`（429）当业务码传给统一包装，而该值未在错误码 switch 中登记，命中「未登记码静默 200」路径；现登记通用 `TooManyRequestsCode=429`，限流恢复真实 429 响应
- 修复 ACL 清单（`docs/acl-manifest.yaml`）长期少报路由：生成脚本 `scripts/generate-acl-manifest.js` 的路由路径正则要求至少 1 个字符，导致全部 `group.GET("", h)` 形式的分组根路径（如 `POST /api/v1/tickets`、`GET /api/v1/users` 等列表 / 创建端点）被静默丢弃；同时子路由文件（`*gin.RouterGroup` 入参）无法自证挂载前缀，新增的 `attachment_routes.go` 丢失 `/api/v1` 前缀与首两条路由。修复后重新生成：599 → 696 路由、权限覆盖 99.86%，`attachment:*` 与各域根路径端点全部在册（详见 `docs/plan/generic-attachment-richtext-control-plan.md` §4.3-7）

### Changed

- 生产部署配置：`RLS_MODE` 默认值调整为 `off`（与后端安全默认对齐）。已配置 `.env.prod` 的部署不受影响
- 前端 dev 启动可选预热：新增 `npm run dev:warmup`（`scripts/dev-with-warmup.mjs`，起 `next dev` 后自动预热高频路由，避免首次点击菜单等待冷编译）与 `npm run dev:warm`（`scripts/dev-warmup.mjs`，对已运行的 dev server 手动预热），支持 `--port/--host/--concurrency/--cookie/--dry-run`；`next.config.ts` 顶部补充 dev 性能实测备忘（Turbopack 15.5 无持久化缓存、webpack 冷编译更慢但有磁盘缓存、`optimizePackageImports` 在 Turbopack 下被忽略、`<Link>` 仅 hover 预取、热请求 SSR 开销），未改动任何运行时配置

---

## [1.6.10] - 2026-09-20

### Added

- **BPMN 定时事件** — 完整实现 Timer Event 基础设施：调度引擎、BPMN 引擎集成、设计器属性面板、任务到期计时（dueDate）和定时启动事件（Timer Start Event），附带 Prometheus 监控指标和任务超时扫描
- **审批链运行时能力** — 新增加签（allowAddApprover）、委派（allowDelegate）、拒绝策略配置和动态层级适配，均通过审批链配置驱动、运行时生效
- **运维命令批处理** — 支持 Outbox 命令的批量重放与取消
- **钉钉/企微入站回调** — 入站消息回调 API 及幂等去重
- **BPMN 模板重载 API** — `POST /api/v1/bpmn/workflow-templates/:key/reload`，从已发布版本创建新部署，版本号自动递增
- **AI 工作流模板治理** — 租户隔离的模板目录 API 和 `/admin/workflows` 管理 UI，支持草稿、版本递增、发布前 Lint、发布与停用
- **CMDB ontology 自描述端点** — `GET /api/v1/cmdb/ontology` 返回租户 CMDB 的机器可读描述（CI 类型、关系词汇、AI 工具定义），AI Agent 可运行时发现 CMDB 契约
- **CI 可读编号** — 每个新建配置项获得 `CI-YYYYMM-NNNNNN` 唯一编号，支持按编号精确查询
- **一键演示数据集** — `make dev-seed-demo` 生成 8 个事件、2 个问题、3 个变更和 5 篇知识文章的演示数据
- **10 个管理页面使用指南** — 新增 `UsageGuideCard` 组件，为管理页面提供基于实际代码逻辑的操作指引
- **Swagger 文档重建 + CI 新鲜度门禁** — 重新生成 158 个 OpenAPI 路径，CI 自动检测文档漂移

### Changed

- **BREAKING: RBAC 授权平面收敛** — 统一权限码、补全预检映射、实现路由→权限码自动生成，关闭未授权写入漏洞；管理员/技术员角色数据库权限空集修复
- **BREAKING: 审批架构迁移** — 统一消费 BPMN 任务，消除双审批实例，ApprovalRecord 标记废弃，清理冗余 DTO
- **BREAKING: API 响应字段统一 camelCase** — Ticket/Incident/SLA/BPMN 响应不再暴露 snake_case 字段，外部集成需同步迁移
- **BREAKING: SLA/BPMN 查询参数 camelCase** — `ticketType`/`customerTier`/`startDate`/`endDate`/`timeRange` 替代原有 snake_case 参数
- **BREAKING: SLA/BPMN 监控端点标准信封** — 统一返回 `{code, message, data}` 格式
- **BREAKING: CI 关系类型受控词汇表** — 关系类型统一由 ent schema 管理，未知类型返回 400 而非静默持久化
- **BREAKING: Ant Design `direction` → `orientation`** — `Space` 组件统一使用 v6 API
- **服务请求 BPMN 集成** — 服务请求审批走 Outbox 事务投递
- **工单/事件写入路径加固** — 乐观锁、NULL 时间戳处理、错误语义修正、升级操作行级安全校验
- **变更/发布/DataScope 写入路径** — 行级安全校验补全
- **流程设计器修复** — 属性写入丢失、版本解析、菜单 404、模板完整性校验、Lint 审批节点校验
- **CSRF 全链路修复** — Cookie 安全属性与 Token 传递对齐
- **通知偏好** — 接入按事件类型偏好 API，修复虚假 API 处理和接收者广播排除
- **查询参数契约收尾** — 统一 camelCase，关闭残留 snake_case 不一致
- **角色域修复** — 移除虚假控件，API 契约对齐 camelCase
- **生产部署加固** — 数据库迁移与索引对齐、开发/生产隔离、Schema 变更脱敏、迁移脚本检查、`VERSION` 不可变要求、诊断端口绑定 loopback
- **基础设施加固** — AI vector/telemetry schema 版本化迁移、HTTP 响应缓存规范化、租户级 Raw SQL 执行器覆盖、Incident/CI 编号使用结构化审计事务
- **前端 UX 审计** — 工单列表列宽、创建按钮尺寸、折叠侧边栏 flyout、看板卡片溢出、工单分析页空白、管理后台仪表盘虚假指标等 20+ 项修复

### Fixed

- **AI 助手知识库无命中时丢失产品自知上下文** — RAG 降级链路现在注入 AI-Native ITSM 的真实能力边界
- **AI Native 工作流生产闭环** — AI BPMN 生成/预览/模板推荐走认证租户与 workflow 权限
- **AI 生成 BPMN 导入失败** — 自动为缺失 DI 的 BPMN XML 注入布局信息
- **表达式引擎 `avg()` 除零 panic** — 空数组或全零数组返回 0
- **会签投票逻辑修复** — `counterSign` 模式下投票结果计数修正
- **流程设计器属性丢失** — 保存时 `extensionElements` 丢失，6 处属性写入不一致统一修复
- **服务请求审批链双缺陷** — 配置未生效和审批人重复
- **审批人自动分配层级丢失** — `autoAssign=true` 时未携带 `level` 字段
- **流程管理 5 项修复** — 版本 API 路径、semver 解析、菜单 404、列表分页、模板导入 DI 补全
- **工单分配搜索崩溃** — `description` 字段缺失时 crash
- **三个用户可见 bug** — 工单列表分页失效、事件详情页关联 tab crash、变更审批评论丢失
- **杂项修复** — SLA 预测窗口校验、WebSocket 重连指数退避、知识文章链接 404

### Security

- RBAC 未授权写入关闭（incident/change/problem/service-request/release/knowledge），含批量回归测试
- CSRF 全链路修复（Cookie 属性 + Token 传递）
- DataScope / 事件 / 发布 / 工单升级写入路径行级安全校验
- 生产发布门禁加固：高/危依赖和 gosec 发现阻断发布

---

## [1.6.9] - 2026-08-20

### Added

- **Problem Management expansion** — Added Problem trends, hotspots, SLA lookup, and per-problem comment read/write endpoints so operators can analyze recurring issues and discuss root cause without leaving the Problem record.
- **Incident comment deletion** — Added `DELETE /api/v1/incidents/:id/comments/:commentId` for tenant-scoped comment cleanup, mirroring the existing create/list semantics.
- **Knowledge recommendations** — Replaced the previous stub implementations of `/api/v1/knowledge/recommendations` and `/api/v1/knowledge/recent` with tenant-scoped queries that return real published articles with proper permission filtering.

### Fixed

- **Frontend Edge Runtime compatibility** — Replaced `Buffer.from(...)` calls in `src/middleware.ts` and `src/app/api/[...path]/route.ts` with an `atob()`-based JWT decoder so the Next.js middleware no longer raises `Code generation from strings disallowed for this context` in Edge Runtime (was breaking `itsm-frontend-prod` with HTTP 500).

### CI

- **Backend lint toolchain** — Pinned `gofumpt` to `v0.7.0` and cached `~/go/bin` between CI runs to make formatting results reproducible and shave install time.

### Tests

- **CMDB Service layer coverage** — Added 9 table-driven test functions covering CloudService / CloudAccount / CloudResource CRUD, Reconciliation (bound / unbound / orphan / unlinked / mixed), and Discovery operations via a `mockRepository` that isolates the service from the database.

---

## [1.6.8] - 2026-08-04

### Security

- **Go runtime baseline** — Raised the backend build and release toolchain to Go 1.25.12 to include the latest TLS, X.509, and MIME-header security fixes required by the production vulnerability gate.

---

## [1.6.7] - 2026-08-04

### Security

- **Go runtime baseline** — Raised the backend build and release toolchain to Go 1.25.10 as an intermediate production security baseline.

### Fixed

- **SLA calendar enforcement** — SLA deadlines now apply each definition's configured business calendar consistently across creation, lookup, and overdue detection; the prior unused implementation was removed.
- **Backend quality gate** — Removed obsolete change-status validation code so the release static-analysis gate has no dead-code findings.

---

## [1.6.6] - 2026-08-03

### Fixed

- **Cookie-only authentication test contract** — Updated API integration and HTTP client tests to assert credentialed browser requests without exposing HttpOnly session cookies through JavaScript `Authorization` headers.

---

## [1.6.5] - 2026-08-03

### Security

- **Production dependency remediation** — Updated frontend production dependencies and lockfile overrides for known Axios, DOMPurify, lodash, PostCSS, Sharp, and UUID advisories; `npm audit --omit=dev --audit-level=high` now reports zero vulnerabilities.
- **Package-manager integrity** — Removed the obsolete `pnpm-lock.yaml`; the frontend is governed solely by the committed npm lockfile, matching the release CI configuration.

---

## [1.6.4] - 2026-08-03

### Fixed

- **CI formatting gate** — Applied the repository's `gofumpt` formatting standard to backend source and tests, restoring the required backend release pipeline gate.

---

## [1.6.3] - 2026-08-03

### Fixed

- **Production authentication hardening** — Browser sessions now use HttpOnly, SameSite=Lax cookies exclusively. Access and refresh tokens are no longer exposed in JSON responses or read by frontend JavaScript; refresh token rotation is performed through the cookie transport.
- **Secure proxy cookie handling** — Authentication cookies now receive the `Secure` attribute when the request is HTTPS or terminated by a trusted HTTPS reverse proxy.
- **CSRF refresh coverage** — Both supported refresh routes are explicitly covered by the CSRF session-refresh exception.
- **Initialization readiness** — `/api/v1/readyz` now requires the latest registered post-schema migration rather than a stale, hard-coded migration version.
- **Workflow validation contract** — Removed the frontend call to an unimplemented validation endpoint; workflow designer preflight validation is deterministic and local, while create/update remain server-validated.

### Changed

- **Release metadata** — Frontend package, backend version endpoint, system configuration endpoint, and GA-readiness report now identify the release as `1.6.3`.

---

## [1.6.0] - 2026-08-01

### Fixed

- **Critical: Form Boundary Issue** - Fixed TicketTypeFormModal where approval/SLA tab fields were outside `<Form>` component, causing form validation failures.
- **Critical: Auth State Persistence** - Removed `isAuthenticated` from Zustand persist partialize to prevent false login state after browser refresh.
- **High: Timer Leak in Export/Import** - Fixed memory leaks in TicketCategoryExport and TicketCategoryImport with proper useRef cleanup on unmount and error paths.
- **High: ITIL Status Guard** - Added readonly protection on change/edit and release forms to prevent editing approved/completed records.
- **High: Suspense Boundary** - Added Suspense wrapper for useSearchParams in tickets page to fix CSR bailout.
- **High: Query Key Normalization** - Fixed useTicketsQuery to only include request params in queryKey, not response data.
- **High: NaN Route Guard** - Added Number.isFinite check for marketplace/[id] route parameter.
- **High: Error Handling** - Fixed workflow-api to throw errors instead of silently returning empty arrays.
- **Medium: useFormMemory** - Added debounce (500ms), enabled flag for edit mode, and dayjs serialization.
- **Medium: CIEditorForm** - Added min/max/precision props to InputNumber for numeric fields.
- **Medium: ChangeDetail Null Guard** - Added null check for createdAt before dayjs conversion.

### Changed

- **Ant Design destroyOnHidden** - Added to ProblemInvestigationTab modals.
- **SchemaField Type** - Added validation property with minValue/maxValue/precision/pattern.

---

## [1.5.2] - 2026-07-31

### Fixed

- **SLA Compliance Statistics** - Fixed negative compliance numbers caused by mismatched time scopes between total ticket count (30 days) and violated ticket count (all time). Backend now uses `HasTicketWith` edge predicate for consistent scope.
- **Timezone Inconsistency** - Fixed dashboard activity timestamps using `time.RFC3339` format instead of `Format("2006-01-02 15:04:05")` which browsers parse as UTC.
- **TicketDetail Duplicate Request** - Fixed double API call on ticket detail page by removing `fetchTicket`/`fetchSLAInfo` from useEffect dependency arrays.
- **Login Error Message** - Login page now shows actual backend error messages (e.g., "invalid credentials") instead of generic "登录失败".

### Changed

- **Ant Design TabPane Deprecation Migrated** - Converted all `Tabs.TabPane` / `<TabPane>` usage to `items` prop pattern across 8 files: analytics, applications, NotificationCenter, TicketTypeFormModal, IncidentManagement, FieldDesigner, profile, dashboard.
- **Space direction → orientation** - Migrated 6 instances of deprecated `Space direction="vertical"` to `orientation="vertical"`.
- **destroyOnClose → destroyOnHidden** - Migrated 1 instance in ApprovalTimeline component.
- **alert()/confirm() → antd message/modal** - Replaced native browser dialogs in marketplace and installations pages with antd `App.useApp()` message/modal.
- **console.log Cleanup** - Removed debug console.log from TicketDetail and BPMNDesigner.

---

## [1.5.0] - 2026-07-30

### Added

- **Connector/Skill Manifest Hardening** - All official connector manifests now declare `version`, `requiredPermissions`, and a deterministic SHA-256 `checksum`; registration is fail-closed (incomplete manifests are rejected at startup). Skill manifests share the same validation and checksum convention. Market API exposes `isOfficial` / `requiredPermissions` / `checksum`.
- **Post-Schema Migrations 008-010** - Initialization ledger (008), PostgreSQL RLS tenant isolation (009), ticket types in `itil-core` transaction (010).
- **Bootstrap Token** - One-time hashed bootstrap token with TTL, concurrent-consumption protection, replay defense, and break-glass flow for first-admin creation.
- **Endpoint ACL Manifest** - Versioned ACL manifest with 100% static coverage gate over protected routes (route-ACL-permission-menu).
- **Fencing Token Hardening** - Owner/token/lease re-verified inside the committing transaction; stale-writer prevention proven via PostgreSQL fault-injection tests.
- **Audit Routes** - New audit trail API endpoints with tenant isolation support
- **CI Attribute Validator** - Moved from handlers to service layer for better separation of concerns
- **Operator Context** - Enhanced audit trail with operator context tracking

### Fixed

- **Ant Design v6 Select Compatibility** - Replaced deprecated `<Select><Option>` child pattern with `options` prop across 100+ files. Fixes issue where clicking Select dropdowns had no response in antd v6. Affected modules: Ticket, Incident, Problem, Change, CMDB, Workflow, SLA, Service Catalog, Admin, and Reports pages.

### Changed

- **CMDB API Route Convergence** - `/api/v1/cmdb/*` is now the canonical prefix for all CMDB endpoints (CIs, CI types, relationships, relationship types, topology, impact analysis, change history, stats). Frontend API clients (`cmdb-api.ts`, `cmdb-relationship.ts`) have been switched to the canonical prefix. Added `GET /api/v1/cmdb/relationship-types` to the canonical tree. Note: change history is `GET /api/v1/cmdb/cis/:id/history` (the old `change-history` suffix only exists on the deprecated alias).
- **CMDB Multi-Tenant Isolation** - Added `tenant_id` field to CI relationships, configuration item history, and discovery sources. Added tenant-aware backfill migration.
- **CI Attribute Validation** - Migrated from `handlers/cmdb/attribute_validation.go` to `service/ci_attribute_validator.go`.

### Deprecated

- **`/api/v1/configuration-items/*` routes** - Kept as a compatibility alias for clients not yet upgraded. No new endpoints will be added under this prefix; removal will be evaluated after a regression period.

### Migration Notes

- **CMDB tenant backfill**: Run `itsm-backend/migrations/20260610_cmdb_tenant_id_backfill.sql` on existing databases to populate the new `tenant_id` fields.
- **Preset CI types**: Enable with `itsm-backend/migrations/20260611_enable_preset_ci_types.sql`.
- **Audit routes**: New endpoints are protected by existing JWT + RBAC + tenant middleware.

---

## [1.0.0] - 2026-03-07

### Added

#### Core ITIL Modules
- **Ticket Management** - Complete ticket lifecycle management with creation, assignment, tracking, and closure; built-in SLA management, priority handling, comments and attachments
- **Incident Management** - Incident discovery, logging, classification, escalation; real-time monitoring and alerts
- **Problem Management** - Root Cause Analysis (RCA), Known Error Database, problem resolution tracking
- **Change Management** - Change requests, risk assessment, multi-level approval workflows

#### Service & Knowledge Base
- **Service Catalog** - Service request templates, self-service portal, SLA management
- **Knowledge Base** - RAG intelligent search, knowledge categorization, FAQ management, vector retrieval

#### Workflow Engine
- **BPMN Workflow** - Visual process designer, approval workflow automation
- **Task Management** - Workflow task assignment and tracking

#### AI-Powered Features
- **Intelligent Classification** - Auto-identify ticket type, priority, impact scope
- **Auto-Summary** - AI-generated ticket/incident summaries
- **RAG Knowledge Base** - Vector search-based intelligent knowledge recommendation
- **Smart Suggestions** - Recommended solutions, similar tickets

#### User & Permissions
- **Multi-Tenant Architecture** - Complete tenant isolation and management
- **Role-Based Access Control** - RBAC permission system, fine-grained access control
- **User Management** - User CRUD, team and department management

#### SLA Monitoring
- **SLA Definition** - Service Level Agreement configuration
- **Real-Time Monitoring** - SLA compliance rate tracking
- **Alert Rules** - SLA violation alerts and notifications

### Technical Stack

| Category | Technology |
|----------|------------|
| Backend | Go 1.25+ / Gin / Ent ORM |
| Frontend | Next.js 15 / React 19 / TypeScript / Ant Design 6 |
| Database | PostgreSQL 17 / Redis 7 |
| Deployment | Docker / Docker Compose |

### Quick Start

```bash
# Docker Compose (Recommended)
git clone https://github.com/heidsoft/itsm.git
cd itsm
make dev-up

# Access
# Frontend: http://localhost:3000
# Backend: http://localhost:8090
# API Docs: http://localhost:8090/swagger

# Login
# Username: admin
# Password: admin123
```

### Known Limitations

- Mobile PWA features are under development
- Enterprise integration features (LDAP/SSO) planned for future releases

### Documentation

- [Development Guide](./docs/getting-started/install.md)
- [Deployment Guide](./docs/DEPLOYMENT_OPTIMIZATION.md)
- [API Documentation](./docs/api/API_REFERENCE.md)

---

*Thank you to all contributors for your support!*
