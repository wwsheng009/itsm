# MSP Demo 多轮多用户 E2E 验证与缺陷修复计划（2026-11-10）

> **状态**：executed-deployed（2026-11-10 完成验证与修复；2026-11-11 从当前工作区 HEAD 全量发布 238 并完成复核）
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，部署模式 `saas_msp`）
> **当前镜像版本**：`1.8.0-dev.44faf311`（2026-11-11 全量重部署后实测）
> **定位**：以现有 demo 数据为基线，执行多轮、多用户、多场景 E2E；先验证、后修复、再回归；只修改可复现且有证据的缺陷。
> **关联**：`plan.md`（2026-10-28 的 13 项修复计划）、`docs/review/2026-10-28-*`（历史快照）、`docs/multi-tenant/demo-environment.md`
>
> **执行结果**：40 个 demo E2E 用例通过，0 个预期失败；
> 缺陷与修复见 [`docs/review/2026-11-10-msp-demo-e2e-validation.md`](../../review/2026-11-10-msp-demo-e2e-validation.md)；
> 原始运行摘要见 [`docs/review/evidence/2026-11-10-msp-demo-e2e/run-summary.md`](../../review/evidence/2026-11-10-msp-demo-e2e/run-summary.md)。

---

## 1. 目标与成功标准

### 1.1 总目标

1. 在真实 demo 环境完成多轮 E2E：登录/角色/权限、跨单据关联、核心业务流程、MSP 跨租户、前端 UI/UX。
2. 对确认缺陷做最小修复，并补充可复跑的回归用例。
3. 校正当前文档中的过时/错误事实，历史评审文档保留快照属性，不篡改历史结论。
4. 产出可重复执行的 demo E2E 套件与完整证据。

### 1.2 完成判定（DoD）

- [x] `itsm-frontend` 新增 demo E2E 配置与套件，针对 238 可一键运行，默认不依赖 localhost / 本地 webServer。
- [x] P0：跨租户隔离、未认证、未分配 MSP 客户、越权写操作全部按预期拒绝；无数据泄露。
- [x] P1：核心单据关联矩阵全部有明确结果；D1–D15 已修复并有回归。
- [x] P1：事件→问题→变更、服务请求审批/开通、变更审批/实施、工单协作、MSP 工作台回复/改状态/指派链路可复跑。
- [x] P1：关键页面无裸 i18n key、无裸角色 code、无 BPMN 原始标识；无未解释的 console error / 5xx。
- [x] P1：360/768/1280/1440 宽度下关键页面无严重横向溢出或操作不可达。
- [x] 已修复项均有失败复现 → 修复 → 回归通过的证据。
- [x] 当前文档中的版本号、工作台数量、SLA 样本分布等事实被校正；新增本次 E2E 运行手册/报告。
- [x] 测试数据使用 `[E2E-<runId>]` 前缀并在结束后清理；不删除/改写 demo 种子账号与种子单据（历史残留单独记录）。
- [x] 工作区原有未提交改动不被覆盖；提交范围仅包含本任务相关文件。

---

## 2. 当前基线（已核验）

### 2.1 账号

统一租户演示口令：`Demo@2026!`；平台管理员：`Demo@2026!Admin`。

| 账号 | 角色 | 用途 |
|---|---|---|
| `admin` | `super_admin` | 平台租户/开通/用户治理 |
| `demo.msp.manager` | `provider_admin` | MSP 分配、跨客户工作台、审计 |
| `demo.msp.agent1` | `provider_agent` | 制造/教育客户操作边界 |
| `demo.msp.agent2` | `provider_agent` | 医疗/制造 specialist 边界 |
| `demo.mfg.admin/agent/lead/user/itadmin/secadmin` | `admin/agent/manager/end_user/it_admin/security_admin` | 客户角色矩阵 |
| `demo.med.*` / `demo.edu.*` | 同上 | 跨客户、跨行业场景 |

### 2.2 数据事实

- demo 定义：1 个服务商 + 3 个客户（`demo-mfg`=3 / `demo-med`=4 / `demo-edu`=5）。
- MSP 分配 7 条：manager 3 个 primary；agent1 制造 backup + 教育 primary；agent2 医疗 backup + 制造 specialist。
- `[DEMO]` 工单 34 张：制造 12 / 医疗 11 / 教育 11，覆盖 7 种状态。
- MSP 托管单 4 张：`#7/#8`（agent1）、`#19`（agent2）、`#30`（agent1），均 `in_progress`。
- 工作台实际工单 65 张：demo 34 + 历史验收/E2E 残留 31。
- 当前 `review-test`（tenant id=6）为 2026-10-28 评审遗留测试租户；`provider_admin` 管理面可见，但无 MSP 分配、不进入工作台汇总。
- 当前工作台 `open` 全部处于 SLA 超期：`slaDueSoon=0`；与文档“含临近样本”的表述不一致。
- 制造租户 `#9 [DEMO] 车间显示器闪烁报修（待受理）` 已被改为 `closed`，当前 demo 基线发生漂移。

### 2.3 现有自动化设施风险

- `itsm-frontend/tests/e2e/auth-utils.ts` 会把 baseURL 端口剥掉（`http://host:8089` → `http://host`），不能直接用于 238。
- `tests/e2e/fixtures/auth.ts` 使用旧账号/旧 API 基址（`user1/engineer1/...`、`VITE_API_URL` 默认无端口），与 demo 账号矩阵不一致。
- `playwright.config.ts` 默认带 `webServer`，即使 `PLAYWRIGHT_BASE_URL` 指向远端也会尝试启动本地 3000；远端运行需要独立配置。
- 多个既有 flow spec 使用错误端点或弱断言（例如接受 200/404 均通过、用 `/tickets` 模拟服务请求、使用旧字段名），不能作为本次验收证据。
- `itsm_msp.py workbench-tickets` 的过滤参数与当前后端契约不一致：脚本发送 `customerTenantId/search/pageSize`，后端读取 `customerTenantIds/q/limit/cursor`。

### 2.4 已从代码中看到、并已在 238 实测确认的修复

- 邀请链接已不再硬编码 `localhost:3000`，改为请求 Host / `APP_BASE_URL` 推导。
- 侧边栏/顶栏角色已接入统一角色词表；`msp_specialist` 已有映射。
- 租户 readiness 已增加 `service_catalog_items`、`ticket_types` 两个非 required 提示项。
- 审批面板 i18n 已使用带默认值的 `t(key, undefined, fallback)` 形式。
- 工作台表格已把 SLA 截止前置并支持超期高亮；改状态后会局部刷新详情。
- 以上均已通过 238 运行态 E2E 或 API 断言确认；未覆盖项在报告“限制”中列明。

---

## 3. 测试架构

### 3.1 新增独立 demo 测试配置

新增 `itsm-frontend/playwright.demo.config.ts`：

- `baseURL`：`process.env.PLAYWRIGHT_BASE_URL`，默认 `http://172.18.3.238:8089`。
- 不使用 `webServer`（远端已运行，避免误启本地 Vite）。
- 单 project `chromium`（本机无 Chrome 时用 Playwright bundled Chromium；必要时 `npx playwright install chromium`）。
- `workers: 1`（登录限流 10/min；多用户场景需串行）。
- `timeout: 60_000`、`expect.timeout: 10_000`，保留失败 trace/screenshot。
- `reporter: [['list'], ['html', { outputFolder: '.dev/playwright-demo-report' }]]`。
- 输出目录建议 `.dev/playwright-demo-results`，避免污染仓库。

### 3.2 新增测试目录

```text
itsm-frontend/tests/e2e/demo/
├── README.md                 # 远端运行说明、账号、清理策略
├── fixtures/
│   ├── demo-accounts.ts      # 21+demo 账号、口令环境变量
│   └── demo-auth.ts          # cookie 登录、token 缓存、多角色 session
├── support/
│   ├── api.ts                # envelope 断言、Bearer、X-Customer-Tenant-ID
│   ├── data.ts               # [E2E-<runId>] 数据工厂
│   ├── cleanup.ts            # 统一清理
│   └── assertions.ts         # 无裸 key/code、console/response 断言
├── 00-smoke.spec.ts
├── 01-permissions.spec.ts
├── 02-associations.spec.ts
├── 03-workflows.spec.ts
├── 04-ui-ux.spec.ts
└── README.md
```

### 3.3 认证与会话原则

- 所有 API 请求使用**同源相对路径** `/api/v1/...`，由 Playwright `baseURL` 保留端口。
- `POST /api/v1/auth/login` 后从浏览器 cookie jar 提取 `access_token`；不要假设响应体一定回显 token。
- 每个角色在同一 worker 内缓存 token/storageState；减少登录次数，规避限流。
- UI 测试使用 `browser.newContext({ storageState })` 或独立 context，避免多角色 cookie 互相覆盖。
- MSP 跨租户请求显式携带 `X-Customer-Tenant-ID`；工作台读写优先走 `/msp/*` 面。
- 所有测试先调用 `/api/v1/version` 或 `/healthz` 做环境闸门；非 `saas_msp` 或版本不符时明确 skip 并记录原因，不制造假红。

### 3.4 测试数据与清理

- 所有新建数据标题/描述前缀 `[E2E-<runId>]`。
- 使用独立 `runId = YYYYMMDD-HHmmss-<short>`，避免并行/重跑冲突。
- 只清理本次创建的 ID：工单、变更、问题、服务请求、工单关联、问题关联、项目关联；不触碰种子数据。
- 清理失败要输出残留 ID 与原因，写入证据，不静默吞掉。
- 对无法删除的邀请/审批记录，改用只读或现有数据验证；如必须创建，记录残留并单独说明。

---

## 4. 多轮执行计划

### Round 0 · 环境与测试设施校准

1. 校验 238：`/healthz`、`/readyz`、`/api/v1/version`、`DEPLOYMENT_MODE=saas_msp`。
2. 校验 8 个代表账号登录：admin、msp.manager、msp.agent1/2、mfg.admin/agent/lead/user、med/edu 各一个。
3. 运行新 demo 配置的最小 smoke（登录 + dashboard + 关键菜单）。
4. 记录浏览器/Playwright 版本、截图、首轮失败点。
5. 若 Playwright 浏览器缺失，安装 Chromium 或切换本机 Chrome channel。

### Round 1 · 只读权限与租户隔离基线

不写业务数据，使用现有 demo 数据：

- 每个角色：登录、`/auth/me`、菜单可见性、关键列表页。
- 列表行级收敛：end_user 仅本人相关；agent 处理范围；manager 待审批；admin 全租户。
- 负向：跨租户 GET/PUT/DELETE、伪造 `X-Customer-Tenant-ID`、未分配客户、未认证、过期/无效 token、非 MSP 访问 `/msp/*`。
- MSP：manager 与 agent 的 allowedCustomers 对比；provider_admin 可见全部客户 vs 工作台条目级授权。
- 记录每个断言的 HTTP 状态码、reasonCode、页面表现、审计事件。

### Round 2 · 跨单据关联矩阵（重点）

| # | 关联 | 正向 | 负向/边界 |
|---|---|---|---|
| A1 | 工单 ↔ 工单 | 创建 `relates_to`；双方详情均可见；删除后双方消失 | 自关联、重复关联、跨租户目标、无 `ticket:update` |
| A2 | 问题 ↔ 工单 | 添加/移除；详情计数与列表一致 | 重复添加、跨租户工单、只读角色 |
| A3 | 问题 ↔ 事件 | 添加/移除；双向可见 | 跨租户事件、重复 |
| A4 | 问题 ↔ 变更 | 添加/移除；双向可见 | 跨租户变更、重复 |
| A5 | 事件 → 问题 | `POST /incidents/:id/convert-to-problem`；问题显示关联事件；事件显示 problemId | 重复转换、描述过短、跨租户 |
| A6 | 变更 ↔ CMDB | `affectedCis` 用 **CI ID** 与 **CI 编号** 两种输入；`/changes/:id/cmdb-impact` 计数一致；风险/回滚门禁生效 | 空 CI、无效 CI、跨租户 CI、同 CI 冲突排期 |
| A7 | 变更 ↔ 关联工单 | `relatedTickets` 保存与展示 | 不存在/跨租户工单编号是否被接受（记录现状） |
| A8 | 服务请求 ↔ 目录/开通 | 目录下单 → 审批 → provisioning → tasks | L2/L3 桥接失败复现、非法状态、越权审批 |
| A9 | 工单 ↔ CMDB | `GET /tickets/:id/configuration-items`；详情展示 | 无关联时空态、跨租户 CI |
| A10 | 项目 ↔ 事件/问题/变更/服务请求 | attach/detach 并回显 | 跨租户、重复关联、权限 |
| A11 | 知识库 ↔ 工单 | 工单详情知识推荐/关联入口 | 若为 mock/未接线，登记为产品缺口，不伪造通过 |

### Round 3 · 多用户业务流程

**B1 事件 → 问题 → 变更（制造租户）**

- `demo.mfg.user` 报事件 → `demo.mfg.agent` 确认/处理 → 转问题 → 添加根因/解决方案 → 创建变更 → 补充受影响 CI、实施/回滚计划 → 提交 → `demo.mfg.lead/itadmin` 审批 → 排期/开始/完成 → PIR。
- 断言：关联双向可见、状态机合法、审批人校验、SLA/审计、页面刷新一致。

**B2 服务请求审批与开通（医疗/教育租户）**

- `demo.med.user` 或 `demo.edu.user` 从目录下单 → `lead` L1 → `itadmin` L2 → `secadmin` L3 → provisioning → task execute → delivered。
- 若 L2/L3 出现 `同步流程审批任务失败` 或 `无权限执行该审批链层级的审批`，记录请求/响应/审计并定位 BPMN 桥接或审批人绑定问题。

**B3 工单协作与状态机（制造租户）**

- end_user 建单 → agent 评论/改状态/指派 → manager 审批（如流程需要）→ resolve/close；非法状态、同状态重放、终态修改必须拒绝。
- 验证并发/乐观锁：两次更新同一 version，第二次 409。

**B4 MSP 跨租户工作台**

- manager 查看 3 客户；agent1 仅 3/5；agent2 仅 4/3。
- 对 MSP 托管单执行回复、改状态、指派；客户侧刷新可见。
- 非托管单/未分配客户：按钮禁用或接口 403；批量 ≤100、低危白名单。
- 审计：`source=workbench`、操作人/目标租户/条目。

**B5 平台治理**

- admin 创建/开通测试租户（若执行，必须使用 `[E2E]` 租户编码并记录清理/停用方式）；验证首管用户名派生、强制改密、readiness 9 项（7 required + 2 hint）。
- 若创建租户清理成本高，则改用 `review-test` 只读核查 readiness，并在报告中注明未做新建。

### Round 4 · UI/UX、响应式与可访问性

页面清单：

- `/login`、`/dashboard`、`/tickets`、`/tickets/create`、`/tickets/:id`
- `/incidents`、`/incidents/:id`、`/problems`、`/problems/:id`
- `/changes`、`/changes/:id`、`/service-catalog`、`/service-requests`
- `/msp/workbench`、`/msp/management`、`/msp/audit`
- `/cmdb`、`/cmdb/cis/:id`、`/knowledge`
- `/admin/tenants`、`/admin/users`、`/admin/roles`

断言：

- 无裸 i18n key（形如 `detailTabs.*`、`ticketDetail.*`、`common.*`）。
- 无裸角色 code（`end_user`、`customer_user`、`super_admin`、`msp_specialist` 等）。
- 无 BPMN 原始标识（`user_task`、`userTask`、`exclusiveGateway`）。
- 空态/错误态/加载态有明确文案，不出现“空冒号”或纯空白。
- 权限不足时给出可理解提示，不是白屏或无限 loading。
- 关键页面在 360/768/1280/1440 下无严重横向溢出；主操作可见。
- console error、失败请求（4xx/5xx）按白名单归类；非预期即缺陷。
- 重点复核历史问题：邀请链接域名、租户列表用户数、顶栏/侧边栏角色、审批链 i18n、SLA 空态、服务目录计数、readiness 9 项、工作台列与详情刷新。

---

## 5. 候选缺陷与修复策略

> 只对“失败用例 + 证据”确认的缺陷动手；未复现的仅记录。

| 编号 | 候选缺陷 | 现有证据 | 处理策略 |
|---|---|---|---|
| D1 | 变更 `affectedCis` 支持 CI 编号输入，但 CMDB 影响分析只解析数字 ID | `changes/new` 与 `edit` 的输入框提示 `CI-ECS-001`；后端 `GetCMDBImpactSummary` 用 `strconv.Atoi` 跳过非数字 | 先写失败用例（CI number → impact 0），再统一解析 ID/ciNumber；同步修正冲突检测的 canonical ID 比较 |
| D2 | 服务请求 L2/L3 审批桥接失败 | 种子报告存在 `同步流程审批任务失败` / `无权限执行该审批链层级的审批` | 复现并定位 BPMN 审批任务/操作人绑定；可局部修复则修，否则登记为受阻断缺陷并保留完整证据 |
| D3 | 现有 E2E 设施不能跑 238 | auth-utils 剥端口；fixtures 旧账号；webServer 强依赖本地 | 新增 demo 配置/夹具；旧设施不强行改造，必要时修共享 helper 的端口 bug |
| D4 | `itsm_msp.py workbench-tickets` 参数与后端不一致 | 实测 `--customer-id` 过滤无效；后端读 `customerTenantIds/q/limit/cursor` | 更新脚本参数映射并回归 3 客户筛选 |
| D5 | 文档版本/数量/SLA 分布过时 | `demo-environment.md` 写旧 VERSION、工作台 34 张、SLA 有临近样本；实测 65 张、0 临近 | 校正当前文档；历史评审保持 historical |
| D6 | `t(key) || fallback` 惰性兜底 | 多处存在该写法；`t()` 缺键返回 key（truthy） | 只在实测出现裸 key 时修；可补统一 util/测试，不进行全仓大改 |
| D7 | 历史 UI 问题残留（角色、i18n、SLA、列宽、邀请链接、readiness） | 代码显示多数已修，238 未验 | 逐项 UI 断言；有失败再修 |

> 执行中另确认并修复 D8–D15（权限契约、AI 门控、跨租户发布引用、发布↔变更 UI、服务请求删除与 404 语义）；
> 完整现象、根因、修复与回归见 [`docs/review/2026-11-10-msp-demo-e2e-validation.md`](../../review/2026-11-10-msp-demo-e2e-validation.md)。

### 修复纪律

1. 每个缺陷先落一个**会失败**的最小测试。
2. 后端修复优先补 Go 单测/集成测试；前端修复补 Jest/组件测试。
3. 修复后重跑该用例 + 相关既有测试 + 本轮 P0/P1 套件。
4. 涉及状态机/BPMN/权限的修复必须同时验证：
   - 合法路径成功；
   - 非法路径 4xx；
   - 跨租户/越权 4xx；
   - 审计与幂等。
5. 不做与缺陷无关的重构；不覆盖工作区已有未提交改动。

---

## 6. 文档校正计划

### 6.1 必改（当前文档事实错误/过时）

- `docs/multi-tenant/demo-environment.md`
  - 版本号改为远端实际 `VERSION`；
  - 工作台数量：明确 34 张 `[DEMO]`，并说明当前另有 31 张历史验收/E2E 残留（总计 65）；
  - SLA：当前 `slaDueSoon=0`、未关闭全部超期；修正“含临近样本”的断言，或说明需重跑 SLA 回填/调整种子；
  - 重跑幂等基线加上“以最近一次运行报告为准”，避免把历史 `created=0/existing=188` 当作永久现状。
- `itsm-frontend/tests/e2e/BEST_PRACTICES.md`
  - 旧账号 `admin/admin123`、`user1/...` 已不适用于 demo；改为指向 `tests/e2e/demo/README.md` 与 demo 账号环境变量。
- `docs/multi-tenant/scenarios/README.md` / `00-environment-and-accounts.md`
  - 增加“本地开发账号 ≠ 238 demo 账号”的显式提示，链接 demo 账号矩阵。

### 6.2 新增

- `docs/review/2026-11-10-msp-demo-e2e-validation.md`：本轮执行报告（环境、轮次、账号、结果、缺陷、修复、残留）。
- `docs/review/evidence/2026-11-10-msp-demo-e2e/`：run-summary、关键截图、失败请求摘录、API 响应证据（脱敏）。
- `itsm-frontend/tests/e2e/demo/README.md`：远端运行命令、账号、数据前缀、清理、限流说明。

### 6.3 不改

- `docs/review/2026-10-28-*` 等历史评审保留原文与 `Status: historical`；若需更正，在新报告中标注“已被 2026-11-10 报告取代/复核”，不直接改写历史结论。

---

## 7. 证据与运行命令（草案）

```bash
# 环境闸门
curl -fsS http://172.18.3.238:8089/api/v1/healthz
curl -fsS http://172.18.3.238:8089/api/v1/version

# Playwright（本地，针对远端）
cd itsm-frontend
PLAYWRIGHT_BASE_URL=http://172.18.3.238:8089 \
PLAYWRIGHT_DEMO_CHANNEL=chrome \
npx playwright test -c playwright.demo.config.ts --workers=1

# 分组执行
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/01-permissions.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/02-associations.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/03-workflows.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/04-ui-ux.spec.ts --workers=1
```

后端定向回归示例：

```bash
cd itsm-backend
go test ./handlers/change/... ./handlers/problem/... ./handlers/incident/... ./handlers/service_request/... ./pkg/seeder/...
```

---

## 8. 风险与约束

| 风险 | 缓解 |
|---|---|
| 登录限流 10/min | token 缓存、worker=1、失败按 `retryAfterSeconds` 退避 |
| demo 数据被污染 | `[E2E-<runId>]` 前缀 + 只清理创建 ID；不删除种子数据 |
| 238 镜像与本地代码不一致 | 先读 `/api/v1/version`；缺陷复现以 238 为准，修复后需重新部署才可验收 |
| BPMN/审批修复复杂 | 先定位是否审批人绑定/桥接层局部问题；不可控则登记阻断，不冒险改引擎 |
| Playwright 浏览器缺失 | 安装 bundled Chromium 或使用本机 Chrome channel |
| 现有工作树有未提交改动 | 修改前核对并仅触碰本任务文件；不使用全量格式化 |
| 测试误报/假红 | 所有断言绑定真实业务结果；skip 必须带原因；失败保留 trace |

---

## 9. 执行顺序与检查点

| 阶段 | 内容 | 检查点 |
|---|---|---|
| A | 环境/账号/测试设施校准 | Round 0 smoke 通过；失败点清单 |
| B | 只读权限与隔离 | P0 矩阵通过；越权证据 |
| C | 关联矩阵 | A1–A11 有结论；D1 复现 |
| D | 多用户业务流程 | B1–B5 有结论；D2 复现 |
| E | UI/UX/响应式 | 关键页面无裸 key/code；console/失败请求清单 |
| F | 修复 | 缺陷测试先红后绿；定向回归通过 |
| G | 全量回归 + 文档 + 证据 | DoD 全勾；报告与文档提交 |

> A–G 已全部完成：Round 0–4 全量 40/40 通过；D1–D16 修复已随当前工作区 HEAD `44faf3110` 全量发布到 238（`1.8.0-dev.44faf311`）并复核。