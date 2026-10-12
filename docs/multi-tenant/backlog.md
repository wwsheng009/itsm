# 多租户优化待办（Backlog）

> 状态：**进行中（OB-1–OB-13 已完成；OB-14 开放）**｜定位：FDB-B1 批次与 238 部署/冒烟的**优化任务清单**（OB-1–OB-14，P1–P4），含状态跟踪与验收口径。
> 关联：[FDB-B1 run-summary](./evidence/fresh-db-browser-B1/run-summary-20261012-1925.md)｜[12 测试计划](./scenarios/12-fresh-db-multi-tenant-browser-test-plan.md)｜[13 操作手册](./scenarios/13-fresh-db-multi-tenant-browser-operations-manual.md)｜[INDEX](./INDEX.md)

---

## 1. 说明

- 来源：批次 FDB-B1（2026-10-12）实测发现（F1–F6、G4/G5）、观察项（O1–O4）与计划 12 风险表（#11/#12）。
- 优先级：**P1** 体验/契约收口（低风险先行）；**P2** 治理/审计口径；**P3** 测试覆盖补强；**P4** 工程/流程。
- 维护约定：每项完成后更新「状态」并在 §3 执行记录追加一行（含提交 hash 与证据指针）；全部完成后本文件头部状态置为「已完成」。

## 2. 任务清单

| # | 优先级 | 任务 | 现状与证据 | 动作 | 验收 | 状态 |
|---|---|---|---|---|---|---|
| OB-1 | P1 | 工作台「改状态」下拉按权限收敛 | `WORKBENCH_STATUS_OPTIONS` 含 `resolved/closed/cancelled` 且未过滤（`workbench/index.tsx:79-85`）；服务商选终态必然失败（计划 12 风险 #12） | 工作台（行内 + 批量）状态下拉收敛为非终态（open/in_progress/pending）；补组件测试 | 下拉不含终态；测试通过；行内/批量均生效 | **已完成** |
| OB-2 | P1 | 邀请体验三连（O1/O2 + SMTP） | `APP_BASE_URL` 默认 `http://localhost:5173`（`invitation_service.go:184-195`）；落地页未提示「登录名=邮箱本地部分」；SMTP 未配置时仅 inviteUrl | ① 默认改为 `http://localhost:3000` 并同步文档；② 落地页/邀请弹窗补用户名形态提示；③ 部署文档补 SMTP 配置与 `emailSent=false` 引导 | 默认值对齐本地 dev；提示可见；文档含 SMTP 章节；相关测试通过 | **已完成** |
| OB-3 | P1 | F2 契约锁死 + 风险表状态更新 | 修复已复验（专用 resolve 入口 + 终态拦截），计划 12 风险 #11/#12 仍为开放口径 | 补测试锁定「终态编辑 → 400 `TICKET_TERMINAL_STATUS_REQUIRES_DEDICATED_ENDPOINT`」与 resolve 入口；更新风险表状态 | 测试通过；风险表 #11 关闭 / #12 指向 OB-1 | **已完成** |
| OB-4 | P2 | 审计视图/API 口径统一（O3/O4） | 平台审计 API（`targetTenantId`）未返回 `workbench.action` 行、不含租户域邀请行；MSP 看板/租户视图可见 | 文档固化口径 + 查询示例（必要时补 API `source` 过滤） | 口径与示例入库；O3/O4 标注「已解释」 | **已完成** |
| OB-5 | P2 | TUM 写通道默认开启评估 | 默认关 + 首管兜底恢复（TUM-D3 修订）；R4 回滚演练 10/10 + restore 6/6 | 出具评估结论与灰度步骤（默认保持关 / 何时开） | 结论入库并更新 TUM 计划/部署文档备注 | **已完成** |
| OB-6 | P3 | 问题/变更/SR 跨租户样本 + 读面验证 | 手册 §7.4 读面为空态（本批次无样本） | 造样本（各 ≥1 条）→ 工作台「跨租户单据」三段可见；记录推进能力 | 三段列表可见样本；证据入 run-summary/backlog | **已完成** |
| OB-7 | P3 | 批量护栏边界端到端 | 服务端单测覆盖超限/高危动作；缺端到端 | API 端到端：101 条 → 超限拒绝；含失效条目 → 逐条失败给原因；`delete` 动作 → 拒绝 | 四场景实测（超限/失效条目/高危动作/未分配客户）+ 证据 | **已完成** |
| OB-8 | P3 | 性能冒烟基线 | 本批次未测 | 本地冒烟：登录/工作台列表/单据读面/批量拒绝路径延迟 p50/p95 | 基线数据入库（注明环境局限） | **已完成** |
| OB-9 | P4 | 截图证据策略收口 | 截图工具受工作区根限制；PNG 不入库（`.gitignore:122`） | 手册 §8.1 固化「本地留存 + CDP 直采兜底」约定 | 手册更新 | **已完成** |
| OB-10 | P4 | 登录限流指引 | 默认 10/min 误伤密集验收（脚本前置已注） | 部署文档/手册补 `LOGIN_RATE_LIMIT_PER_MIN` 验收建议值与示例 | 文档更新 | **已完成** |
| OB-11 | P1 | 全新库 init 被「无模板的示例绑定」阻断（F8，238 部署发现） | 迁移 `20260620_process_routing_enhancement.sql` 示例绑定引用 4 个不存在的模板 key（`change_emergency_flow`/`incident_general_flow`/`release_test_flow`/`expense_approval_flow`）→ `verifyWorkflowTemplates` 失败 → workflow-core 事务回滚 → compose 依赖链中止（站点 502） | 已修复：前向迁移 `20261013_prune_unbacked_process_bindings.sql`（幂等删除无模板绑定；不改历史迁移避免 checksum 漂移）+ 静态回归测试 | 全新库 init 直通；回归用例覆盖 | **已完成（2026-10-12）**：238 全新库探针 `init exit 0`（ledger=75/defs=18/unbacked=0）；测试 `migration/process_bindings_consistency_test.go` PASS；提交 `7c90af22` |
| OB-12 | P2 | nginx:alpine 入口脚本在 snap-docker 挂起（F9，238 部署发现） | `10-listen-on-ipv6-by-default.sh` 卡在 `apk manifest nginx`（实测 8s 超时不返回）；前端容器 nginx 不启动（3000 拒绝）→ 站点 502 | 已修复：前端 Dockerfile 生产阶段 `RUN rm -f /docker-entrypoint.d/10-listen-on-ipv6-by-default.sh` | 重建后前端容器免 override 直接 healthy | **已完成（2026-10-12）**：移除服务端 override 后重建，前端 t=5s healthy；提交 `7c90af22` |
| OB-13 | P3 | 分配管理身份口径提示（238 冒烟观察） | `POST /msp/allocations` 需 `msp_allocation:write`；`role=admin`（rank 4）按 07:G3 遮蔽 `provider_admin→msp_manager`（rank 3）→ 平台通道建的 provider 管理员做分配会 403 权限不足 | 文档口径已固化（手册 §2.1/§6.1）；建号表单提示/自动提级评估留待后续（如需） | 口径固化（手册/部署文档或表单提示） | **已完成（2026-10-12）** |
| OB-14 | P1 | 三类单据「推进状态」写面契约不一致/不可达（F10，238 UI 写面验证） | 读面 `allowedActions.status=true`（按 `msp_role` 映射 ∩ 客户租户业务码计算），但提交被**路由通用预检**拒绝 `403 权限不足`（预检按 token 角色，`admin` 遮蔽 `msp_manager` 后无 `msp_ticket:write`）；且平台可授予角色与 `canWriteRecord`/预检白名单交集为空（仅 `sysadmin` 同时满足且禁授）→ 平台通道 MSP 用户对 record 写面事实上不可达 | ① 预检对 `/msp/*` 改用 MSP 映射角色（与 RequireMSPPermission 同口径）；② `canWriteRecord` 接受 MSP 管理角色；③ 或为 record 写面定义 MSP 授权矩阵并补 parity 回归 | 读面 allowedActions 与写路径一致；平台通道 MSP 用户可完成 record 推进（含反例 fail-closed） | **待处理** |

## 3. 执行记录

| 时间 | 项 | 结果 | 证据/提交 |
|---|---|---|---|
| 2026-10-12 | 建立清单（OB-1–OB-10） | 建立 | 提交 `7ae84176` |
| 2026-10-12 | OB-1 工作台状态下拉收敛 | 已完成：`WORKBENCH_WRITABLE_STATUS_OPTIONS`（行内+批量，排除终态）；3 个新测试，工作台套件 13/13 PASS | 提交 `a3df8fca` |
| 2026-10-12 | OB-2 邀请体验三连 | 已完成：`APP_BASE_URL` 默认 `http://localhost:3000`；落地页/邀请弹窗补登录名提示；部署文档补 APP_BASE_URL/INVITATION_TTL_HOURS/SMTP；invitation 后端测试 + 落地页 5/5 PASS | 提交 `a3ec6888` |
| 2026-10-12 | OB-3 F2 契约锁死 | 已完成：`TestHandler_Update_TerminalStatusGuard`（resolved/closed 400 + reasonCode、非终态放行）；风险表 #11/#12 关闭 | 提交 `8b741a91` |
| 2026-10-12 | OB-4 审计口径统一 | 已完成：场景 08 §1.1「审计视图口径」（平台/租户/MSP 三视图 + 过滤参数 + 查询示例）；O3/O4 标注已解释 | 提交 `7c96c33f` |
| 2026-10-12 | OB-5 TUM 默认开启评估 | 已完成：结论=生产默认关，按环境灰度开启（前置 4 条）；TUM 计划 v0.12 + 部署文档 §3.1 回填 | 提交 `a514a727` |
| 2026-10-12 | **F7（执行中新增发现）** | **发现并修复**：refresh/switch 重签用原始 `users.role`，MSP 用户（end_user+provider_agent）首次续签（15min）后 role 掉回 end_user → MSP 端点 403；统一 `middleware.EffectiveTokenRole`（provider/home 作用域提级）+ 回归测试；shell 实测 refresh 后 role=msp_tech、端点 200 | 提交 `70af97a9` |
| 2026-10-12 | OB-6 三类单据跨租户读面 | 已完成：变更/问题/SR 样本各 1 条；API 读面 3/3 + 浏览器「跨租户单据」三段可见（含「推进状态」入口） | 提交 `05326382`；`evidence/workbench-probes/run-summary-20261012-221028.md` |
| 2026-10-12 | OB-7 批量护栏边界端到端 | 已完成：B1 101 条→400 `BATCH_LIMIT_EXCEEDED`；B2 失效条目逐条失败（`CUSTOMER_TENANT_NOT_FOUND`）；B3 `delete`→403 `ACTION_NOT_ALLOWED`；B4 未分配客户→`MSP_ALLOCATION_REQUIRED` | 提交 `05326382` |
| 2026-10-12 | OB-8 性能冒烟基线 | 已完成：登录 p50≈1123ms / 列表 p50≈1501ms / 单据读面 p50≈1626ms / 批量拒绝路径 p50≈1218ms（本地 dev 基线，非生产压测） | 提交 `480d83f6`；`evidence/perf-smoke/run-summary-20261012-222130.md` |
| 2026-10-12 | OB-9/OB-10 文档收口 | 已完成：手册 §8.1 截图约定（本地留存 + CDP 兜底）；部署文档补 `LOGIN_RATE_LIMIT_PER_MIN` 验收指引 | 提交 `3c69f017` |
| 2026-10-12 | **238 生产部署（新版程序 + 新库）** | 已上线：`2d00a52e` 镜像 + 新库 `itsm_prod_v2`（saas_msp），7/7 healthy，公网 8088 验证通过；部署期发现并现场处置 F8/F9（→ OB-11/OB-12） | `evidence/238-deploy-20261012/run-summary.md` |
| 2026-10-12 | **238 新库冒烟轮** | 建租户 9/9 PASS；建号+建单+协同 14/14 PASS（三账号建号 → 建单 → 分配 → 工作台回复/改状态 → 客户回看）；新增观察 OB-13（分配管理身份口径） | `evidence/238-deploy-20261012/smoke-tenants.md`、`smoke-users-tickets.md` |
| 2026-10-12 | **238 三类单据读面冒烟** | 15/15 PASS：建号（mgr2/agent2/custAdmin2）→ 分配 → 客户侧造样本（目录项/变更/问题/SR）→ 工作台 changes/problems/service-requests 读面各 items=1；同时 OB-13 口径固化进手册 §2.1/§6.1 | `evidence/238-deploy-20261012/smoke-records-readface.md` |
| 2026-10-12 | **OB-11/OB-12 修复部署与回归（238 v2）** | 已完成：版本升级 `1.8.0-dev.7c90af22`；init exit 0 + 新迁移入账 + 前端免 override healthy + 7/7 容器 healthy + HTTP 全 200；**全新库探针零人工干预 init 直通** | 提交 `7c90af22`；`evidence/238-deploy-20261012/run-summary.md` §3.1 |
| 2026-10-12 | **238 UI 级冒烟（浏览器）** | 4/4 PASS：UI 登录（MSP 菜单）→ 工作台工单列表（2 单）→ 跨租户单据三类读面 → 行内回复（toast + 评论落库 `[UI-238]`）；演示账号口令已轮换作废 | `evidence/238-deploy-20261012/smoke-ui.md` |
| 2026-10-12 | **238 单据写面 UI 验证（发现 F10）** | 读面按设计收敛（普通角色禁用 + 明确 reasonText）；管理账号提交 403 与读面 `allowedActions=true` 不一致；授权矩阵核实「平台可授予角色 ∩ 双门禁白名单 = 空（仅 sysadmin 且禁授）」→ 登记 OB-14；演示账号口令轮换 | `evidence/238-deploy-20261012/smoke-records-writeface.md` |

## 4. 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v0.1 | 2026-10-12 | 建立：FDB-B1 发现/观察项 → OB-1–OB-10（P1–P4）任务清单 |
| v0.2 | 2026-10-12 | **执行回填：OB-1–OB-10 全部完成**；执行中新发现 F7（refresh/switch 角色漂移 → MSP 续签后 403）已修复并回归；各任务提交与证据见 §3 |
| v0.3 | 2026-10-12 | **238 生产部署回填**：新增 OB-11（全新库 init 被无模板示例绑定阻断，F8）与 OB-12（nginx 入口脚本挂起，F9）；部署运行摘要见 `evidence/238-deploy-20261012/` |
| v0.4 | 2026-10-12 | **238 新库冒烟回填**：建租户 9/9、建号+建单+协同 14/14 通过；新增 OB-13（分配管理身份口径提示）；证据见 `evidence/238-deploy-20261012/smoke-*.md` |
| v0.5 | 2026-10-12 | **238 三类单据读面冒烟回填（15/15）**；OB-13 口径固化到手册 → 标记完成（表单提示/自动提级评估留待如需） |
| v0.6 | 2026-10-12 | **OB-11/OB-12 修复收口（提交 `7c90af22`）并完成 238 v2 部署回归**：版本 `1.8.0-dev.7c90af22`，init exit 0、前端免 override healthy、全新库探针 init 直通（ledger=75/defs=18/unbacked=0） |
| v0.7 | 2026-10-12 | **238 UI 级冒烟回填（4/4）**：浏览器完成登录/工作台/跨租户读面/行内回复写路径；演示账号口令即时轮换作废 |
| v0.8 | 2026-10-12 | **单据写面 UI 验证回填：新增 OB-14（F10：推进状态写面契约不一致/不可达）**；含双层门禁根因矩阵与修复方向；证据见 `smoke-records-writeface.md` |
