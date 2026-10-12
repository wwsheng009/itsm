# 多租户优化待办（Backlog）

> 状态：**已完成（2026-10-12 收口）**｜定位：FDB-B1 批次发现/观察项与评估结论的**优化任务清单**（OB-1–OB-10，P1–P4），含状态跟踪与验收口径。
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

## 4. 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v0.1 | 2026-10-12 | 建立：FDB-B1 发现/观察项 → OB-1–OB-10（P1–P4）任务清单 |
| v0.2 | 2026-10-12 | **执行回填：OB-1–OB-10 全部完成**；执行中新发现 F7（refresh/switch 角色漂移 → MSP 续签后 403）已修复并回归；各任务提交与证据见 §3 |
