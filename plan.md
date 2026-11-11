# 多租户功能审查问题修复计划

基于 `docs/review/2026-10-28-multi-tenant-functional-review.md`，共修复 **13 个问题**（P1×4、P2×7、P3×3）。

## 修复策略
- **后端（我直接负责，保证数据口径与测试一致）**：P1-1、P1-2、P1-3(后端)、P3-3。
- **前端（按独立文件簇并行实施，互不冲突）**：P1-4、P2-1、P2-2、P2-3、P2-4、P2-5、P2-6、P2-7、P3-1、P3-2。
- 文案类改动沿用代码库既有风格：**行内中文字符串**（现有组件如 `WorkflowProgressCard` 已大量使用），不强行接入 i18n 词表，避免大范围改动。
- 每条修复配套最小验证（单测 / 类型检查 / 既有测试不退化）。

---

## 立即修（下次发版前）

### P1-2 ｜ 租户列表"资源使用"用户数恒为 0
- **根因**：`handlers/tenant/handler.go:fillUsageCounts` → `service/tenant_service.go:UsageCounts` → `service/tenant_quota_service.go:Counts`。该路径在 RLS enforce 模式下依赖每条语句 `tenantctx.WithTenantID` 重绑租户作用域；平台管理员跨租户计数若因 RLS 作用域收窄而失败，handler 仅打 WARN、字段保持 0（handler.go:332 的"降级为 0"分支），列表遂显示 0。
- **修复**：
  1. 在 `TenantQuotaService` 新增跨租户、绕过 RLS 的计数路径（用 `tenantctx.WithSystemBypass` 包裹 `UsageCounts` 调用，或在 `Counts` 内对该读路径显式 system-bypass），确保平台/服务商视角的用户数真实。
  2. 在 `handler.go:fillUsageCounts` 调用处使用该跨租户计数；保留"计数失败不阻断列表"的降级语义。
  3. 补充/修正 `tenant_service_test.go`、`tenant_quota_test.go`：断言新租户（有 N 个用户）的列表用量 `Users == N`。
- **文件**：`service/tenant_quota_service.go`、`service/tenant_service.go`、`handlers/tenant/handler.go`、`common/tenantctx/*`。

### P1-4 ｜ 顶栏角色标签显示 "End_user"
- **根因**：`components/layout/header/UserMenuDropdown.tsx:45-47` 用 `user.role` 三元映射（admin/super_admin/其它→"用户"），未覆盖 `provider_admin` 等有效角色；且 `user.role` 取的是默认/降级角色而非"有效角色解析"（与 `07-known-gaps.md` G3 同现象）。文档实测服务商经理被标成 "End_user"。
- **修复**：
  1. 在 `lib/api/user-api.ts` 的 `PRIMARY_ROLE_LABEL` / `PRIMARY_ROLE_OPTIONS` 补齐 `provider_admin → 服务商经理`（及 `it_admin` 等已定义角色），使标签取数统一为同一词表。
  2. `UserMenuDropdown` 改用 `PRIMARY_ROLE_LABEL[user.role] ?? <有效角色兜底>`，不再硬编码三元；对多角色场景优先展示有效（管理/服务商）角色而非默认 end_user。
  3. 同步核对 `auth-store.ts` / `session-mappers.ts` 写入的 `role` 是否为"有效角色"；必要时复用 G3 的有效角色解析逻辑。
- **文件**：`components/layout/header/UserMenuDropdown.tsx`、`lib/api/user-api.ts`、`lib/store/auth-store.ts`、`lib/auth/session-mappers.ts`。

### P2-1 ｜ 跨租户改状态后详情弹窗不刷新
- **根因**：`pages/(main)/msp/workbench/index.tsx` 行内/批量"改状态"成功后只刷新列表（line 662-671 `message.success('状态已更新')`），但右侧详情抽屉的 `detail` 状态对象（line 1143-1144 `STATUS_LABEL.get(detail.status)`）未同步，仍显示旧原始枚举值。
- **修复**：改状态成功回调中，若 `detail` 指向被改单据，则局部更新 `detail.status`（或重新拉取该单据详情），使抽屉与列表一致；提示文案改为"已更新为<目标状态中文>"。
- **文件**：`pages/(main)/msp/workbench/index.tsx`。

---

## 本迭代内

### P1-3 ｜ 工单详情"流转进度"暴露 BPMN 原始标识（user_task / exclusiveGateway）
- **根因**：`components/business/WorkflowProgressCard.tsx` 直接渲染后端返回的 `currentActivityType`/`activityType`（line 234、266）以及 `currentActivityName`/`activityName`（line 236、268）；当 BPMN 节点未配置业务显示名时，返回的是技术 id（`user_task`、`exclusiveGateway`）。
- **修复**：
  1. **后端**：在工单 workflow 状态聚合（dto `ticket_workflow_dto.go` / 对应 service 的 enrich 阶段）中优先返回节点业务显示名（`name` 属性）；缺失时回退为友好的节点类型中文标签（用户任务/网关/服务任务…，见 `types/ticket-workflow-state.ts:31` 的 `BpmnActivityType`）。新增"节点类型 → 中文"映射。
  2. **前端**：`WorkflowProgressCard` 与 `ApprovalWorkflowPanel` 渲染显示名；type 标签展示中文类型（`用户任务`/`网关节点`…）而非原始 `userTask`/`exclusiveGateway`；显示名缺失时降级为"处理中（第 N 步）"而非回落原始 id。
- **文件**：后端 `service/*` workflow 状态聚合 + `dto/ticket_workflow_dto.go`；前端 `components/business/WorkflowProgressCard.tsx`、`components/business/detail-tabs/ApprovalWorkflowPanel.tsx`。

### P1-1 ｜ 新租户"模板开通全绿"但服务目录/工单类型为空
- **根因**：`pkg/seeder/tenant_readiness.go` 的 7 项就绪度检查不含服务目录与工单类型；模板开通（Go `pkg/seeder/tenant_provisioner.go` 的 `ProvisionTenant`）只克隆角色/权限/菜单等基线，不生成业务数据。演示环境每租户 3 个目录项来自 `seed_demo_dataset.py`（种子脚本），非产品开通能力。
- **修复**：
  1. **开通落引子**：扩展租户供给器，为新租户写入**至少 1 个通用服务目录项**与**1 个通用工单类型**（对齐角色/菜单的克隆口径），使新客户开箱即有可走的服务请求。
  2. **就绪度补全**：在 `tenantReadinessChecks()` 增补 `service_catalog_items`、`ticket_types` 两项（非 required 的"业务数据"提示项，避免冲淡基线就绪语义），让开通向导能区分"基线就绪 / 业务数据待配置"。
  3. **空态引导**：服务目录页 `pages/(main)/service-catalog/index.tsx` 空态给出"还没有服务项，去创建"按钮，而非纯空白。
  4. 补 `tenant_readiness_test.go` 覆盖新两项计数。
- **文件**：`pkg/seeder/tenant_provisioner.go`、`pkg/seeder/tenant_readiness.go`、`pages/(main)/service-catalog/index.tsx`、`pkg/seeder/tenant_readiness_test.go`。

### P2-3 ｜ 用户列表角色列中英混排（系统管理员 + customer_user）
- **根因**：`pages/(main)/admin/users/index.tsx:280-287` 对主角色走 `PRIMARY_ROLE_LABEL` 映射，但对 `record.roleNames`（RBAC 角色 code）直接透出原始值 `customer_user`，未映射。
- **修复**：在 `lib/api/user-api.ts` 建立**统一的角色 code → 中文标签表**（含 `customer_user → 终端用户`、`it_admin`、`provider_admin` 等全部可见角色 code），用户列表与组管理（`groups/index.tsx`、`TenantUsersDrawer.tsx`）的 `roleNames` 统一经该表映射；多角色用统一数量的徽标呈现，未映射 code 不裸透出。
- **文件**：`lib/api/user-api.ts`、`pages/(main)/admin/users/index.tsx`、`pages/(main)/admin/groups/index.tsx`、`pages/(main)/admin/tenants/components/TenantUsersDrawer.tsx`。

### P2-4 ｜ 新建工单提示与校验自相矛盾
- **根因**：`pages/(main)/tickets/create/index.tsx:687` 顶部提示"填写最少信息即可提交"，但描述字段有"描述最少 10 个字符"校验（实测仅填标题、描述留空被拦截）。
- **修复**：将提示文案改为准确定义的"标题 + 描述即可提交"（或允许描述为空并走默认占位）。同步核对实际校验规则，使文案与规则一致；补对应组件测试。
- **文件**：`pages/(main)/tickets/create/index.tsx`（含其校验逻辑所在文件）。

---

## 可排期（本批一并修复）

### P2-2 ｜ 工作台表格关键列被截断（SLA 截止等需横向滚动）
- **修复**：`pages/(main)/msp/workbench/index.tsx` 表格列重排——将**状态、优先级、SLA 截止**前置；超期行级红色高亮（把"2026/10/17 09:33:31 超期"中的超期状态拆为醒目标记）；或提供紧凑模式/列配置。优先保证 SLA 截止在视口内。
- **文件**：`pages/(main)/msp/workbench/index.tsx`、`components/.../CrossTenantBoard.tsx`。

### P2-5 ｜ 无审批环节却提示"无法进行审批操作"
- **根因**：`components/ticket/TicketDetail.tsx` 在申请人打开自己工单时渲染"您是此工单的申请人，无法进行审批操作"，但该工单并无待办审批任务，制造误解。
- **修复**：仅当存在待办审批任务时渲染该行；无审批任务时改为"本单当前无审批环节"或不渲染。
- **文件**：`components/ticket/TicketDetail.tsx`（含相关权限/审批任务判断逻辑）。

### P2-6 ｜ 工单详情空区块无兜底文案（SLA 定义/配置项/知识推荐）
- **修复**：SLA 定义、受影响配置项、知识推荐等空区块统一给明确占位文案（如"未配置 SLA 定义，请联系管理员""暂无关联配置项"），区分"本就没有"与"加载失败"。
- **文件**：`components/ticket/TicketDetail.tsx`、相关 detail-tab 组件。

### P2-7 ｜ 下拉浮层在滚动容器内离屏（可访问性）
- **修复**：创建租户"MSP 服务商"下拉、建号"角色"下拉（antd `Select`）设置 `getPopupContainer={trigger => trigger.parentElement}`（挂到就近容器/body portal），并确已具备 `role="listbox"/"option"` 与键盘上下键（antd 默认具备，需验证不被 overflow 裁剪）。
- **文件**：`components/provisioning/ProvisionUserModal.tsx`、租户新建弹窗组件（tenant create modal）、相关全局 CSS。

### P3-1 ｜ 分配管理页说明暴露技术实现（X-Customer-Tenant-ID）
- **修复**：`pages/(main)/msp/management/index.tsx:202` 说明改为面向管理员的业务语言（"分配后即可在工作台中查看并处理该客户的工单"），HTTP 头说明移至开发者文档。
- **文件**：`pages/(main)/msp/management/index.tsx`。

### P3-2 ｜ 审计看板"按事件分布"中英混排
- **根因**：`pages/(main)/msp/audit/index.tsx:164` "按事件分布"混合中文（登录/工作台操作…）与英文枚举（view/search/pat/update/create/delete/export/upload）。
- **修复**：统一中文标签（查看/搜索/个人令牌/更新/创建/删除/导出/上传），原始技术枚举放入 tooltip。
- **文件**：`pages/(main)/msp/audit/index.tsx`。

### P3-3 ｜ 演示数据 SLA 几乎 100% 超期
- **根因**：演示种子（`seed_demo_dataset.py` 或 Go seed_demo.go）的 SLA 截止时间分布导致未关闭工单几乎全部超期，影响演示说服力。
- **修复**：调整种子脚本 SLA 截止时间分布为约 7:2:1（正常/临近/超期）样本；保留少量临近与超期样本作演示，其余落在正常区间。需定位种子脚本并修改，注意与现有演示租户数据兼容。
- **文件**：`scripts/seed_demo_dataset.py`（或 `itsm-backend/seed_demo.go`，定位后确定）。

---

## 验证
- **后端**：`cd itsm-backend && go build ./...`；运行相关单测（`tenant_service_test.go`、`tenant_quota_test.go`、`tenant_readiness_test.go`、`tenant_provisioning_test.go`、`provisioning_test.go`）。
- **前端**：`cd itsm-frontend && npx tsc --noEmit`（或 `pnpm typecheck`）确认类型；运行受影响的组件/`pages` 单测（`workbench`、`users`、`msp/management`、`msp/audit`、`tickets/create`、`UserMenuDropdown` 等）。
- **回归**：确认既有 RLS/越权测试（如 `rls_integration_test.go`、`tenant_user_admin_e2e_test.go`）不因计数路径改动退化。

## 备注
- P1-2 的确切根因需在 demo（`RLS_MODE`）下确认；计划采用"system-bypass 跨租户计数"修复，对任意 RLS 模式都成立，并保留列表可用性优先的降级语义。
- P1-1 的"开通落引子"涉及供给器写入租户作用域业务数据，需对齐现有克隆口径与 RLS（供给通道 `tenantctx.WithProvisioningBypass`），改动后补供给单测。
---

## 2026-11-10 · MSP Demo 多轮多用户 E2E 验证与修复计划（新增）

> 本节不替代上面的 2026-10-28 修复计划；新增任务已拆到
> [`docs/multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md`](docs/multi-tenant/plan/e2e-demo-validation-and-remediation-plan.md)。

**范围与顺序**

1. **Round 0**：238 环境/版本/账号/Playwright 远端运行设施校准（新增 `playwright.demo.config.ts` 与 `tests/e2e/demo/`，不再依赖本地 3000/webServer）。
2. **Round 1**：只读权限与租户隔离基线（平台/服务商经理/两服务商工程师/客户六角色）。
3. **Round 2**：跨单据关联矩阵（工单↔工单、问题↔工单/事件/变更、事件→问题、变更↔CMDB、服务请求↔目录/开通、项目关联、知识库↔工单）。
4. **Round 3**：多用户业务流程（事件→问题→变更、服务请求多级审批/开通、工单协作、MSP 跨租户工作台、平台治理）。
5. **Round 4**：UI/UX/响应式/可访问性（无裸 i18n key/角色 code/BPMN 原始 id；空态/错误态/加载态；360–1440 宽度）。
6. **Round 5**：确认缺陷的最小修复 + 回归（先有失败用例）。
7. **Round 6**：校正当前文档事实、产出运行手册与证据报告。

**优先候选缺陷**

- `D1`：变更 `affectedCis` 允许 CI 编号输入，但后端 CMDB 影响分析只解析数字 ID。
- `D2`：服务请求 L2/L3 审批桥接可能仍失败（种子报告已出现 `同步流程审批任务失败`）。
- `D3`：现有 Playwright auth-utils/fixtures 不适用于 238（端口被剥、旧账号、webServer 强依赖）。
- `D4`：`itsm_msp.py workbench-tickets` 参数与当前后端 `customerTenantIds/q/limit/cursor` 不一致。
- `D5`：`demo-environment.md` 的版本号、工作台数量、SLA 样本表述需按实测校正。

**执行原则**：不覆盖本文件上方历史计划；只改本任务相关文件；测试数据统一 `[E2E-<runId>]` 前缀并清理；历史评审文档保留 `Status: historical`，不复写历史结论。**执行进展（2026-11-11）**：Round 0–6 已完成；238 已从当前工作区 HEAD `af3cba9cb` 全量发布 `1.8.0-dev.af3cba9c`，全量 demo E2E **44 passed / 0 预期失败**；D1–D18 已修复并完成运行态回归（含 provider→customer→返回工作台、多租户业务操作、全局发号与附件角色访问）。报告见 `docs/review/2026-11-10-msp-demo-e2e-validation.md`，运行摘要见 `docs/review/evidence/2026-11-10-msp-demo-e2e/run-summary.md`。