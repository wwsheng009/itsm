# ITSM 演示环境「多角色 × 多租户」E2E 业务测试计划与场景手册（2026-11-13）

> **状态**：draft → executing（本文档同时作为测试计划与执行手册）
> **被测环境**：`http://172.18.3.238:8089`（独立演示栈 `itsm-demo`，部署模式 `saas_msp`）
> **版本基线**：`.env.demo.example` 中 `VERSION=1.8.0-dev.a29ad103`（执行时以运行态 `/api/v1/version` 实测值为准并在此处回填）
> **文档定位**：面向"业务可用性"的黑盒 E2E 计划。区别于
> [`role-based-product-test-plan.md`](./role-based-product-test-plan.md)（GA 门禁与自动化套件口径）与
> [`../multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md`](../multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md)
> （2026-11-10 MSP 专项缺陷修复轮），本文档聚焦**按"角色 × 租户"组合驱动真实业务流程闭环**，
> 并把"发现问题 → 定位代码 → 修复 → 部署 → 回归"作为同一轮闭环的一部分。
> **关联**：[`../multi-tenant/demo-environment.md`](../multi-tenant/demo-environment.md)（账号与数据集事实源）、
> [`../multi-tenant/scenarios/`](../multi-tenant/scenarios/)（多租户操作场景）、
> [`./test-cases/`](./test-cases/)（按模块拆分的用例集）。

---

## 1. 测试目标与判定

### 1.1 目标

1. **角色覆盖**：验证 1 个平台超管 + 3 类服务商角色 + 3 个客户租户 × 6 类岗位角色，共 **22 个账号**在真实 UI 上可登录、可见正确菜单、可执行其岗位应有的业务动作。
2. **租户隔离**：验证同一角色在不同租户下数据严格收敛；跨租户读/写/删一律拒绝且不泄露资源存在性。
3. **业务闭环**：以"工单 / 事件 / 问题 / 变更 / 服务请求 / 知识库 / 项目"七条主链路，验证从创建 → 流转 → 审批 → 关闭 → 归档的端到端可用性。
4. **AI 能力**：验证已接入的千帆（`qianfan.baidubce.com` / `tokenplan`）通道在 AI 分诊、AI 摘要、AI 助手对话等入口产生真实 LLM 结果，而非静默降级为关键词启发式。
5. **缺陷闭环**：所有 P0/P1 缺陷必须有"复现证据 → 代码定位 → 修复 → 重新部署 → 回归通过"的完整链路记录。

### 1.2 完成判定（DoD）

- [ ] 22 个账号全部可登录，落地路由与角色匹配（服务商落 `/msp/workbench`，客户角色落 `/dashboard`）。
- [ ] 七条业务主链路的关键用例（P0+P1）通过率 100%；P2 用例通过率 ≥ 90%。
- [ ] 跨租户隔离用例 100% 按预期拒绝（404/403/400，不得 500，不得泄露标题/编号等敏感字段）。
- [ ] AI 入口至少 3 处产生真实 LLM 响应（响应体含模型标识或非启发式来源标记）。
- [ ] 无裸 i18n key、无裸角色 code（如 `msp_specialist`、`end_user` 直接展示）、无 BPMN 原始标识出现在关键页面。
- [ ] 关键页面在 1280 / 1440 宽度下无严重横向溢出、无操作控件不可达。
- [ ] 本轮新增/修改的测试数据使用 `[E2E-<runId>]` 前缀，结束后清理；**不删除、不改写** 演示种子账号与 `[DEMO]` 种子单据。
- [ ] 本文档 §9 的缺陷台账填写完整，含最终状态。

### 1.3 非目标

- 不做性能压测与容量评估（另见 `docs/ops`）。
- 不做 OWASP 全量渗透（仅做 §7 SEC 组的功能级越权验证）。
- 不验证第三方连接器（飞书/钉钉/企微）的真实外发。
- 不重复 2026-11-10 MSP 专项已验证的 46 个用例，仅做**回归确认**。

---

## 2. 环境基线

| 项 | 值 | 核验方式 |
|---|---|---|
| 入口 | `http://172.18.3.238:8089` | 浏览器打开 |
| 部署栈 | `itsm-demo`（backend / worker / frontend / nginx / redis / ai-service 6 容器 + `itsm-init-demo` 一次性迁移） | 见 demo-environment.md §1 |
| 数据库 | `itsm_demo`（宿主 `itsm-postgres-prod` 独立库） | 只读核对，禁止直改业务数据 |
| 部署模式 | `saas_msp`（MSP 跨租户能力开启） | 服务商账号可见 `/msp/*` 菜单 |
| AI 通道 | 千帆 `https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions` | AI 入口实测响应 |
| 主站隔离 | 主站 `8088` 与演示栈业务数据/会话/队列互不影响 | 副作用核对 |

> **执行前置检查（每次开跑前 3 分钟）**
> 1. `GET /api/v1/version` 记录版本号与构建 hash。
> 2. `admin` 登录确认仪表盘 200、无 5xx。
> 3. 记录工作台工单总数基线（用于后续比对"是否有测试数据污染"）。
> 4. 打开浏览器 DevTools Network 面板，全程监控 `4xx/5xx` 与 console error。

---

## 3. 账号与角色矩阵

统一下发口令：租户账号 `Demo@2026!`；平台管理员 `Demo@2026!Admin`。

### 3.1 平台层

| 账号 | 角色 | 落地路由（期望） | 主要验证面 |
|---|---|---|---|
| `admin` | `super_admin` | `/dashboard` | 租户治理、开通、用户/角色/菜单、审计、系统配置 |

### 3.2 服务商（云维科技，`demo-provider`）

| 账号 | 角色 | 落地路由（期望） | 覆盖客户 | 主要验证面 |
|---|---|---|---|---|
| `demo.msp.manager` | `provider_admin` | `/msp/workbench` | 制造 / 医疗 / 教育（3 个 primary） | MSP 分配、跨客户工作台、审计看板 |
| `demo.msp.agent1` | `provider_agent` | `/msp/workbench` | 制造（backup）/ 教育（primary） | 一线工程师接单、状态推进 |
| `demo.msp.agent2` | `provider_agent` | `/msp/workbench` | 医疗（backup）/ 制造（specialist） | 二线工程师处理医疗单 |

### 3.3 客户租户（3 租户同构，各 6 角色）

| 岗位 | 账号后缀 | 角色 code | 主要职责 | 高风险动作（需谨慎） |
|---|---|---|---|---|
| 信息化管理员 | `.admin` | `admin` | 租户内全配置、用户邀请、服务目录 | 角色分配、目录下架 |
| IT 服务台 | `.agent` | `agent` | 接单、分派、回复、关闭 | 强制关闭、改优先级 |
| 部门经理 / 审批人 | `.lead` | `manager` | 一级审批（部门范围） | 批准 / 驳回 |
| 终端用户 | `.user` | `end_user` | 提单、查自有单、浏览知识库 | 仅自有工单写权限 |
| IT 主管 | `.itadmin` | `it_admin` | 二级审批（L2） | 批准 / 驳回 |
| 安全管理员 | `.secadmin` | `security_admin` | 三级审批（L3）、安全事件、审计 | 审批放行、封禁 |

租户前缀：制造 `demo.mfg.*`（华辰智能制造，tenant 3）、医疗 `demo.med.*`（仁和医疗集团，tenant 4）、教育 `demo.edu.*`（云帆教育科技，tenant 5）。

### 3.4 账号 × 能力期望矩阵（权限断言基线）

> 图例：✅ 允许 / ❌ 拒绝（含菜单不可见即视为拒绝）/ ⚠️ 行级收敛（仅本人或本部门可见）

| 能力 | super_admin | provider_admin | provider_agent | admin | agent | manager | it_admin | security_admin | end_user |
|---|---|---|---|---|---|---|---|---|---|
| 跨租户工作台 `/msp/workbench` | ✅ | ✅ | ✅（限分配客户） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| 租户管理 `/admin/tenants` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| 用户管理 `/admin/users` | ✅ | ⚠️（成员域） | ❌ | ✅ | ❌ | ❌ | ⚠️ | ❌ | ❌ |
| 工单列表 `/tickets` | ✅ | ⚠️（工作台口径） | ⚠️ | ✅ | ✅ | ⚠️ | ✅ | ⚠️ | ⚠️（自有） |
| 创建工单 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 问题管理 `/problems` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| 已知错误 KEDB | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| 变更 `/changes` | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ❌ |
| 服务请求审批 `/approvals` | ✅ | ❌ | ❌ | ✅ | ❌ | ⚠️（本部门） | ⚠️（L2） | ⚠️（L3） | ❌ |
| 知识库读 `/knowledge` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 知识库写（新建/发布） | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ❌ |
| 项目管理 `/projects` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️（任务只读） |
| 审计日志 `/audit-logs` | ✅ | ✅（服务商域） | ❌ | ✅ | ❌ | ❌ | ⚠️ | ✅ | ❌ |
| AI 助手 `/ai/chat` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

> **执行说明**：本矩阵是"期望值"，不是"已实现值"。凡实测与期望不符，一律登记为缺陷（Δ 偏差），
> 并区分类型：`menu_missing`（菜单未渲染）/ `route_404`（路由不存在）/ `api_403`（接口拒绝）/ `leak`（数据越界可见）。

---

## 4. 测试数据规范

### 4.1 命名与隔离

- 新建单据标题统一前缀 `[E2E-<runId>]`，`runId` 形如 `20261113A`（日期 + 轮次字母）。
- 描述首行写入 `trace: <用例ID>`，便于事后按用例回溯。
- **禁止**修改或删除：`demo.*` 种子账号、`[DEMO]` 前缀单据、`[DEMO]` 知识文章、`review-test` 遗留租户。

### 4.2 清理策略

- 本轮产生的 `[E2E-*]` 单据在轮次结束后：工单 → `cancelled`（不物理删除，保留审计轨迹）；知识文章 → 下架（unpublish）。
- 项目类测试数据保留（用于演示价值），但在描述中标注 `[E2E-*]`。

### 4.3 基线漂移记录

每轮开始记录并在报告里对比：

| 指标 | 采集口径 |
|---|---|
| 工作台工单总数 | `demo.msp.manager` → `/msp/workbench` 表尾总数 |
| 各租户未关闭工单数 | 客户 `admin` → `/tickets` 筛选未关闭 |
| SLA 超期 / 临近数 | 工作台 SLA 看板 |
| 知识文章数 | `/knowledge` 列表总数 |

---

## 5. 场景目录

> **用例编号规则**：`<域>-<序号>`，域取 PLT / MSP / TKT / INC / PRB / CHG / SR / KB / PRJ / AI / SEC / UX。
> **优先级**：P0 = 业务不可用级，必须 100% 通过；P1 = 主链路，必须通过；P2 = 体验与边界。
> **执行方式**：`UI` = 浏览器手工操作；`UI+API` = UI 操作后用接口/页面状态交叉验证。

### 5.1 A 组 · 平台治理（账号 `admin`）

| ID | 优先级 | 场景 | 步骤要点 | 期望结果 |
|---|---|---|---|---|
| PLT-01 | P0 | 平台管理员登录与落地 | 登录 → 观察侧栏 | 落地 `/dashboard`；侧栏含"系统管理/客户管理/工作流"；顶栏显示"超级管理员" |
| PLT-02 | P0 | 租户列表可读 | `/admin/tenants` | 4 个业务租户（provider + 3 客户）+ `review-test` 可见；状态、模式列完整 |
| PLT-03 | P1 | 租户详情与 readiness | 打开某客户租户详情 | readiness 项渲染；`service_catalog_items` / `ticket_types` 以"提示"而非"阻断"呈现 |
| PLT-04 | P1 | 用户与角色管理 | `/admin/users` → 筛选某租户 | 用户列表分页正确；角色列显示中文名而非裸 code |
| PLT-05 | P1 | 菜单与权限配置 | `/admin/menus`、`/admin/permissions` | 树形结构可展开；保存后刷新保持 |
| PLT-06 | P1 | 审计日志 | `/audit-logs` | 可见登录、工单创建、审批等事件；时间/IP/来源列完整 |
| PLT-07 | P2 | 系统配置 | `/admin/system-config` | 只读展示项与可编辑项区分清晰；保存有成功提示 |
| PLT-08 | P1 | 平台管理员在服务商租户的越权判定 | 切到 provider 租户视角访问 `/msp/customers`、`/msp/workbench/summary` | 不应 403（见 2026-10-18 已修复项，需回归确认） |

### 5.2 B 组 · 服务商 MSP 跨客户协同（账号 `demo.msp.*`）

| ID | 优先级 | 场景 | 步骤要点 | 期望结果 |
|---|---|---|---|---|
| MSP-01 | P0 | 服务商经理登录与落地 | `demo.msp.manager` 登录 | 落地 `/msp/workbench`；顶栏显示"服务经理"而非 `End_user` |
| MSP-02 | P0 | 跨客户工作台数据面 | 观察工作台 3 个客户 Tab 与汇总卡 | 客户 3 个；工单数、未关闭、超期、临近四卡数值与客户明细一致 |
| MSP-03 | P1 | 客户筛选与关键字过滤 | `CustomerFilter` 切"全部客户" / 单客户；关键字 `[DEMO]` | 切换后表格总数与筛选一致；"全部客户"选项对合法服务商员工保留 |
| MSP-04 | P0 | 跨租户回复工单 | 打开某 MSP 托管单 → 回复 | 回复成功；客户侧可见该回复；时间线顺序正确 |
| MSP-05 | P0 | 跨租户改状态 | 工作台行内改状态 `in_progress → pending` | 状态落库；刷新后保持；客户侧状态同步 |
| MSP-06 | P1 | 跨租户指派 | 工作台指派给 agent2 | 负责人变更可见；被指派 agent 在其工作台可见该单 |
| MSP-07 | P1 | agent1 覆盖边界 | `demo.msp.agent1` 登录 | 仅见制造 + 教育客户；访问医疗客户数据被拒 |
| MSP-08 | P1 | agent2 覆盖边界 | `demo.msp.agent2` 登录 | 仅见医疗 + 制造（specialist）；教育不可见 |
| MSP-09 | P2 | 服务商审计看板 | `/msp/audit`（manager） | 列表有数据；无 5xx；列宽不溢出 |
| MSP-10 | P1 | SLA 风险看板口径 | 观察超期/临近 | 超期数与客户列表筛选结果一致；`slaDueSoon` 为 0 时显示空态而非错误 |

### 5.3 C 组 · 工单全生命周期（重点：创建与跟踪）

| ID | 优先级 | 账号 | 场景 | 步骤要点 | 期望结果 |
|---|---|---|---|---|---|
| TKT-01 | P0 | `demo.mfg.user` | 终端用户提单 | `/tickets/create` 填标题/描述/类型/优先级 → 提交 | 提交成功；跳详情或列表可见；列表仅见自有单 |
| TKT-02 | P0 | `demo.mfg.user` | 自有工单跟踪 | 打开自己刚建的单 → 添加评论 | 可读可评论；无"分派/关闭"等管理按钮 |
| TKT-03 | P0 | `demo.mfg.user` | 越权访问他人工单 | 直接构造他人单号 URL | 403 或 404；不得渲染标题、描述 |
| TKT-04 | P0 | `demo.mfg.agent` | 服务台接单与分派 | 打开新单 → 改状态 `new→open` → 指派处理人 | 状态推进成功；分派人下拉有候选；时间线记录流转 |
| TKT-05 | P1 | `demo.mfg.agent` | 回复与内部备注 | 添加客户可见回复 + 内部备注 | 两种可见性区分正确；内部备注不对 end_user 展示 |
| TKT-06 | P1 | `demo.mfg.agent` | 解决与关闭 | `open→in_progress→resolved→closed` | 每步合法；非法跃迁被拦截并给出提示 |
| TKT-07 | P1 | `demo.mfg.admin` | 强制关闭与重开 | 关闭后重开 | 重开状态正确；审计有记录 |
| TKT-08 | P1 | `demo.mfg.agent` | 工单类型驱动动态表单 | 选择"通用事件"/"账号权限申请"类型 | 动态字段按类型渲染；必填校验生效 |
| TKT-09 | P1 | `demo.mfg.agent` | SLA 展示 | 打开带 SLA 的单 | SLA 截止/剩余时间渲染；超期高亮 |
| TKT-10 | P2 | `demo.mfg.agent` | CC 与关注人 | `/tickets/cc` 添加抄送 | 抄送人可在其列表看到该单 |
| TKT-11 | P2 | `demo.mfg.admin` | 工单模板 | `/tickets/templates` | 模板列表可读；套用后预填字段 |
| TKT-12 | P2 | `demo.mfg.admin` | 工单分析 | `/tickets/analytics`、`/tickets/dashboard` | 图表渲染无空白；无 console error |
| TKT-13 | P0 | `demo.med.user` | 跨租户同构验证 | 重复 TKT-01/02（医疗租户） | 行为一致；数据不与制造租户串台 |
| TKT-14 | P0 | `demo.edu.user` | 跨租户同构验证 | 重复 TKT-01/02（教育租户） | 行为一致；数据不与前两租户串台 |
| TKT-15 | P1 | `demo.mfg.lead` | 审批视角的工单可见性 | 登录看 `/tickets` | 仅见本人创建/被指派/本部门相关单，不越界 |

### 5.4 D 组 · 事件与问题管理

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| INC-01 | P1 | `demo.mfg.agent` | 事件列表与状态机 | 7 种状态样本可见；非法流转被拒 |
| INC-02 | P1 | `demo.mfg.agent` | 新建事件 `/incidents/create` | 创建成功；进入 `new`；可从事件升级为问题 |
| INC-03 | P2 | `demo.mfg.agent` | 事件升级与 NOC 工作台 | `/noc` 有数据；升级动作产生关联记录 |
| PRB-01 | P0 | `demo.mfg.agent` | 问题列表 `/problems` | 现有 `open/investigating/identified` 样本可见 |
| PRB-02 | P1 | `demo.mfg.agent` | 新建问题 `/problems/new` | 创建成功；可关联工单/事件 |
| PRB-03 | P1 | `demo.mfg.agent` | 已知错误 KEDB | `/problems/known-errors` 展示症状/根因/规避/解决四段 |
| PRB-04 | P1 | `demo.mfg.admin` | 跨租户问题关联拒绝 | 关联他租户工单/事件/变更 → 400 校验错误（非 500） |
| PRB-05 | P2 | `demo.mfg.agent` | 问题趋势报表 | `/problems/trends` 与 `/reports/problem-efficiency` 渲染正常 |

### 5.5 E 组 · 变更管理

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| CHG-01 | P1 | `demo.mfg.agent` | 变更列表行级收敛 | 非管理角色仅见本人创建/被指派 |
| CHG-02 | P0 | `demo.mfg.admin` | 变更全链路 | `draft→submit→approve→schedule→start→complete` 每步成功；状态机无死锁 |
| CHG-03 | P1 | `demo.mfg.admin` | 受影响 CI 影响分析 | `affectedCis` 填 **CI 编号**与**数字 ID** 均能算出影响面（回归 2026-11-10 修复项） |
| CHG-04 | P2 | `demo.mfg.agent` | 变更日历 / PIR | `/changes/pirs` 渲染；无 5xx |
| CHG-05 | P2 | `demo.mfg.admin` | 标准变更 | `/standard-changes` 列表可读；预授权变更可跳过审批 |

### 5.6 F 组 · 服务请求与多级审批（角色差异主战场）

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| SR-01 | P1 | `demo.mfg.user` | 浏览服务目录并下单 | `/service-catalog` 可见 3 个 `[DEMO]` 目录项；下单生成 `submitted` 请求 |
| SR-02 | P0 | `demo.mfg.lead` | L1 审批待办 | `/service-requests` 待审批页可见本部门（生产部）员工提交的待批请求 |
| SR-03 | P0 | `demo.mfg.lead` | L1 批准 | 批准后状态 `manager_approved`；申请人可见进度 |
| SR-04 | P0 | `demo.mfg.itadmin` | L2 审批 | 仅见已过 L1 的请求；批准后 `it_approved` |
| SR-05 | P1 | `demo.mfg.secadmin` | L3 审批 | 仅见已过 L2 的请求；无 L1/L2 越级能力 |
| SR-06 | P0 | `demo.mfg.lead` | 驳回链路 | 驳回后状态与理由落库；申请人可读理由 |
| SR-07 | P1 | `demo.mfg.lead` | 跨部门不可见 | 制造部 lead 看不到信息技术部员工的待批请求 |
| SR-08 | P1 | `demo.edu.lead` | 教育租户两级已批样本 | `it_approved` 样本可读；不串租户 |
| SR-09 | P2 | `demo.mfg.agent` | 服务目录页面权限 | `agent` 已演示级补授 `service_catalog:read`，可读（回归登记项） |
| SR-10 | P1 | `demo.mfg.lead` | 审批中心待办任务 | `/approvals`、`/admin/approvals` 可见 `ticket_general_flow` 待领取任务，含批准/拒绝入口 |

### 5.7 G 组 · 知识库

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| KB-01 | P1 | `demo.mfg.user` | 浏览与检索 | `/knowledge` 可见 5 篇已发布文章；搜索命中标题/正文 |
| KB-02 | P1 | `demo.mfg.user` | 只读边界 | end_user 无"新建/编辑/发布"入口 |
| KB-03 | P0 | `demo.mfg.agent` | 新建文章并发布 | `/knowledge/articles/new` 保存 → 提交审核 → 发布；列表可见 |
| KB-04 | P1 | `demo.mfg.admin` | 审核队列 | `/knowledge/reviews` 可见待审文章；通过/退回生效 |
| KB-05 | P2 | `demo.mfg.agent` | 富文本与附件 | 标题/正文/附件/标签落库正确；预览不丢格式 |
| KB-06 | P1 | `demo.med.user` / `demo.edu.user` | 行业定制内容隔离 | 各租户只见本行业文章，无跨租户串台 |
| KB-07 | P2 | `demo.mfg.agent` | 工单转知识 | 从工单生成知识草稿，字段回填正确 |

### 5.8 H 组 · 项目管理

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| PRJ-01 | P1 | `demo.mfg.admin` | 项目列表与详情 | `/projects` 列表可读；详情进度条 `aria-valuenow` 与任务一致 |
| PRJ-02 | P0 | `demo.mfg.admin` | 进度派生正确性 | 建任务 → 项目进度按叶子任务均值重算；软删任务 → 回到 0（回归 2026-10-18 修复项） |
| PRJ-03 | P1 | `demo.mfg.admin` | 项目结项 | `complete` 后进度强制 100% |
| PRJ-04 | P2 | `demo.mfg.admin` | 模板与组合视图 | `/projects/templates`、`/projects/portfolio`、`/projects/reports` 渲染正常 |
| PRJ-05 | P2 | `demo.mfg.user` | 我的项目待办 | 仪表盘"我的项目任务"卡片：无任务时显示空态而非报错 |

### 5.9 I 组 · AI 能力（千帆通道）

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| AI-01 | P0 | `demo.mfg.user` | AI 助手对话 | `/ai/chat` 发送一条 IT 问题，收到**真实 LLM 回复**（非固定模板/关键词） |
| AI-02 | P0 | `demo.mfg.agent` | AI 智能分诊 | 新建工单时触发分诊，返回分类 + 置信度；日志/响应可辨认为请求级 LLM 目标 |
| AI-03 | P1 | `demo.mfg.agent` | 工单 AI 摘要 | 详情页 AI 摘要生成本轮对话摘要，内容与工单事实一致 |
| AI-04 | P1 | `demo.mfg.admin` | AI 建单 `/tickets/ai-create` | 自然语言描述 → 生成工单草稿字段 |
| AI-05 | P2 | `demo.mfg.admin` | AI 可观测性 | `/ai/observability`、`/ai/audit` 有调用记录；无 5xx |
| AI-06 | P1 | `admin` | 跨租户 LLM 目标隔离 | 无 provider 的目标租户不触发下发，降级路径符合设计且不报错 |
| AI-07 | P2 | `demo.edu.agent` | 教育租户 AI 同构 | 同 AI-01，验证多租户下 AI 均可用 |

### 5.10 J 组 · 跨租户隔离与安全

| ID | 优先级 | 账号 | 场景 | 期望结果 |
|---|---|---|---|---|
| SEC-01 | P0 | `demo.mfg.admin` | 访问服务商页面 | 手动访问 `/msp/workbench` | 重定向 `/403`；无 MSP 菜单 |
| SEC-02 | P0 | `demo.mfg.admin` | 跨租户读他租户工单 | 构造医疗租户工单 URL | 404；不渲染任何字段 |
| SEC-03 | P0 | `demo.mfg.admin` | 跨租户删除 | `DELETE /tickets/:id`（他租户） | 404（非 500），不泄露存在性 |
| SEC-04 | P0 | `demo.mfg.user` | 未认证直访 | 清 Cookie 后访问 `/tickets` | 跳 `/login`；无数据闪现 |
| SEC-05 | P1 | `demo.med.admin` | 租户切换边界 | 若存在切换入口，切到无权租户 | 明确拒绝；不残留上一租户数据 |
| SEC-06 | P1 | `demo.mfg.secadmin` | 安全事件与审计 | `/audit-logs` | 只在本租户范围；无跨租户条目 |
| SEC-07 | P1 | `demo.edu.agent` | 越权写 | 尝试修改他租户工单状态 | 403/404；状态未变更 |

### 5.11 K 组 · UI/UX 与国际化

| ID | 优先级 | 场景 | 期望结果 |
|---|---|---|---|
| UX-01 | P1 | 裸 i18n key 扫描 | 关键 12 个页面无 `xxx.yyy.zzz` 形式裸 key |
| UX-02 | P1 | 裸角色 code 扫描 | 顶栏/侧栏/列表角色列显示中文岗位名 |
| UX-03 | P2 | 响应式 1280 / 1440 | 无横向滚动条溢出；表格操作列可达 |
| UX-04 | P2 | 亮/暗主题切换 | 切换后对比度正常；无文字不可见 |
| UX-05 | P1 | 控制台洁净度 | 关键路径 0 console error、0 failed response（除已知 401 探测） |

---

## 6. 执行轮次与排期

采用"**纵向穿透**"而非"横向扫页"：每轮选 1~2 条主链路，跨角色跑完整闭环。

| 轮次 | 主链路 | 账号组合 | 产出 |
|---|---|---|---|
| R1 | 基线 + 平台治理 + 账号落地 | `admin`、全部 21 个租户账号各登录一次 | 登录落地矩阵、菜单可见性矩阵 |
| R2 | 工单全生命周期（制造租户为主） | `mfg.user → mfg.agent → mfg.admin` + `msp.agent1` | TKT-01~10 结果；跨租户协同证据 |
| R3 | 服务请求多级审批 | `mfg.user → mfg.lead → mfg.itadmin → mfg.secadmin` | SR-01~10 结果；部门收敛证据 |
| R4 | 事件 → 问题 → 变更 | `mfg.agent`、`mfg.admin` | INC/PRB/CHG 结果；影响分析证据 |
| R5 | 知识库 + 项目 | `mfg.agent`、`mfg.admin`、`mfg.user` | KB/PRJ 结果；进度派生证据 |
| R6 | AI 能力 | 3 租户各 1 个角色 + `admin` | AI-01~07 结果；模型响应证据 |
| R7 | 隔离与安全回归 | 跨租户组合 | SEC-01~07 全绿 |
| R8 | 医疗 / 教育租户同构验证 | `med.*`、`edu.*` | 跨租户一致性结论 |
| R9 | 缺陷修复后回归 | 受影响账号 | 回归通过清单 |

> **节奏约定**：每轮结束必须落一次证据（截图 + Network 关键响应）与缺陷台账更新，再开下一轮。

---

## 7. 缺陷分级与处置流程

### 7.1 分级

| 级别 | 定义 | 处置要求 |
|---|---|---|
| **P0 阻断** | 登录失败、主链路断裂、跨租户数据泄露、500 错误 | 当轮停止后续用例，立即定位修复，当天部署回归 |
| **P1 严重** | 功能与期望矩阵不符但可绕过（如按钮缺失、状态机死锁、审批链路断） | 本轮内修复；无法修复则登记并降级说明 |
| **P2 一般** | 展示/文案/布局/i18n/空态问题 | 本轮汇总，批量修复 |
| **P3 建议** | 交互优化、信息层级 | 记入 backlog，不在本轮修 |

### 7.2 处置闭环（每缺陷必走）

1. **复现**：记录账号、URL、操作序列、时间戳；截图 + Network 响应体。
2. **归因**：定位到具体层（frontend 组件 / route-permission / backend handler / 迁移 / seed）。
3. **修复**：最小改动，附单测或可复跑断言。
4. **验证链**：`go build`/`tsc`/相关单测 → 构建镜像 → 部署 238 → UI 回归。
5. **归档**：写入 §9 台账，并在 `docs/review/` 或 `docs/multi-tenant/evidence/` 留证据目录。

### 7.3 部署通道（238）

- 代码位置：本仓库工作区（`itsm-backend` / `itsm-frontend` / `itsm-ai-service`）。
- 部署栈：`docker-compose.demo.yml`，一次性迁移 `itsm-init-demo`。
- 部署后必须核对：容器 healthy 数、`/api/v1/version` 版本号与构建 hash、A/B 指纹（关键日志行数变化）。
- **只允许**通过 compose 重建 demo 栈；不得触碰主站 `8088` 栈与 `itsm-postgres-prod` 的其他库。

---

## 8. 证据要求

每个 P0/P1 用例至少留：

| 证据类型 | 内容 |
|---|---|
| 截图 | 操作前后各 1 张，含 URL 与关键字段 |
| Network | 关键接口的请求路径 + 状态码 + 响应体关键字段 |
| 时间线 | 操作时间戳（用于与后端日志对齐） |
| 数据核对 | 刷新后状态保持（证明落库而非前端乐观更新） |

证据目录约定：`docs/multi-tenant/evidence/e2e-2026-11-13-<runId>/`，内含 `run-summary.md` + 图片。

---

## 9. 缺陷台账（执行时填写）

| ID | 级别 | 用例 | 账号 | 现象 | 归因 | 状态 | 证据 |
|---|---|---|---|---|---|---|---|
| — | — | — | — | 待执行填写 | — | open/fixed/verified | — |

---

## 10. 退出准则

本轮测试可宣布结束，当且仅当：

1. §1.2 DoD 全部勾选完成，或未完成项有明确"降级理由 + 后续计划"。
2. §9 台账无 `open` 状态的 P0/P1 缺陷。
3. 所有 P0/P1 修复项已在 238 运行态验证通过（非仅本地构建通过）。
4. 测试数据已按 §4.2 清理，工作台基线回到 §4.3 记录的预期区间（允许 `[E2E-*]` 残留但需说明）。
5. 本文档回填实测版本号、实际通过率、遗留风险，并更新 [`./README.md`](./README.md) 的"当前入口"清单。

---

## 附：与既有文档的关系

| 文档 | 关系 |
|---|---|
| [`role-based-product-test-plan.md`](./role-based-product-test-plan.md) | 上游 GA 门禁方案；本文档是其"演示环境业务化落地"子集，不复用其旧账号（`admin/admin123`） |
| [`../multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md`](../multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md) | 2026-11-10 MSP 专项轮；本文档对其 46 个用例只做回归，不重复设计 |
| [`../multi-tenant/demo-environment.md`](../multi-tenant/demo-environment.md) | 账号与数据集的**事实源**；本文档 §3 全部引用自该文件 |
| [`../multi-tenant/scenarios/`](../multi-tenant/scenarios/) | 多租户"操作步骤"级手册；本文档提供"验收判定"级场景 |
| [`./test-cases/`](./test-cases/) | 按产品模块拆分的用例集（TC-TICKET / TC-KNOWLEDGE 等）；本文档跨模块编排业务流程 |
