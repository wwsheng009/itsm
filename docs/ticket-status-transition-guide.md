---
page_keys: [ticket.detail, ticket.list, ticket.create]
audience: internal
version: 1.8
last_reviewed: 2026-11-12
owner: service-desk
---

# 工单状态修改与流转说明

本文说明**工单创建之后**如何修改状态、如何驱动流转，以及相关权限、终态与留痕规则。
内容依据帮助文档、前端实现（`itsm-frontend/src/components/ticket/TicketDetail.tsx`、
`src/lib/utils/row-scope.ts`）与后端表结构（`itsm-backend/migrate_ticket_workflow.sql`）核对整理。

## 1. 状态流转主链

产品文档口径（`docs/help/tickets/list.md`、`tickets/detail.md`）：

```
新建 → 处理中 → 待用户 → 已解决 → 已关闭
```

系统实际支持的 11 个状态值（前端 `createStatusMap`）：

| 状态值 | 含义 | 展示色 |
|---|---|---|
| `new` | 新建 | default |
| `open` | 开启 | default |
| `in_progress` | 处理中 | processing |
| `assigned` | 已分配 | processing |
| `pending` | 待用户（挂起等回复） | warning |
| `pending_approval` | 待审批 | warning |
| `resolved` | 已解决 | success |
| `closed` | 已关闭 | default |
| `cancelled` | 已取消 | error |
| `rejected` | 已拒绝 | error |
| `approved` | 已批准 | success |

其中 `resolved / closed / cancelled / rejected / approved` 属于**终态**（`isFinalStatus`），
进入终态后不可再做生命周期写操作。

## 2. 修改状态的五个合法入口

均在「工单详情」页操作区，按权限与状态条件显隐：

| 操作 | 作用 | 接口 | 状态影响 | 前置条件 |
|---|---|---|---|---|
| **编辑** | 改标题/描述/优先级/分类等字段 | `PUT /tickets/:id` | 字段更新；**不能直写 `resolved`/`closed`** | `ticket:update` + 行级可写 + 非终态 |
| **分配** | 转派受理人 | `POST /tickets/:id/assign` | 进入已分配/处理中 | `ticket:assign` + 行级可写 + 非终态 |
| **提交解决方案** | 填写解决方案后结单 | 专用 resolve 端点 | → `resolved`，**解决方案必填** | `ticket:update` + 行级可写 + 非终态 |
| **审批 / 拒绝** | 处理审批链 | 审批端点 | → `approved` / `rejected` | 有审批权限；不能审批自己提交的单 |
| **关闭工单** | 确认办结 | 关闭端点 | → `closed`（终态） | **仅当状态为 `resolved` 或 `approved`** 时出现，需二次确认 |

## 3. 关键约束

1. **终态锁定**：`isFinalStatus` 为真时，审批、分配、编辑、提交解决方案、关闭、抄送按钮全部置灰，
   页面显示终态提示文案。后端同步拦截 `PUT` 直接改写成 `resolved/closed`，必须走专用端点。
2. **行级权限（fail-closed）**：`canWriteRow` 与后端 `datascope.CanWriteResource` 对齐——
   管理角色（`super_admin/admin/manager/sysadmin`）与 **MSP/provider 角色**全租户可写；
   其余角色仅**创建人**或**受理人**可写；身份未知或无 id 返回 `false`。
   权限码存在但行级不可写时，写类按钮在入口即隐藏，避免「可点必败」。
   - **MSP 场景要点**：provider 侧账号在客户租户内 `/auth/me` 返回的是
     `role=end_user` + `mspRole=provider_admin`（实测 `demo.msp.manager`），
     真正的身份在 `mspRole`。前端须按
     `provider_admin→msp_manager`、`provider_agent→msp_tech`、`customer_user→end_user`
     映射后再判定，否则「服务商经理」会被误判为只读。
     `provider_admin`(→`msp_manager`)、`provider_agent`(→`msp_tech`) 的规范
     `data_scope` 为 `all`，后端本就放行；`customer_user`(→`end_user`) 不享受提级。
   - 判定面覆盖 `role` / `mspRole` / `roles[]` 三者并集（对齐后端「多角色取最宽范围」）。
3. **审批链**：工单类型开启 `approval_enabled` 后，提交会先进入 `pending_approval`，
   审批通过才继续流转；「审批链」Tab 可查看各层审批记录（`ticket_approvals`）。
4. **AI 不改单**：AI 仅产出分诊建议、答复草稿、影响分析（只读），所有写动作需人工确认。
5. **流程与自动化**：节点、条件、自动分派规则在 `/workflow` 的「自动化」「审批链」中配置；
   已运行实例沿用启动时的定义版本，改流程**不会**回溯改写旧实例。

## 4. 留痕机制

每次流转写入 `ticket_workflow_records`：

| 字段 | 说明 |
|---|---|
| `action` | 动作类型（创建/评论/转派/解决/关闭等） |
| `from_status` / `to_status` | 变更前后状态 |
| `operator_id` | 操作人 |
| `from_user_id` / `to_user_id` | 受理人变更前后 |
| `comment` / `reason` | 备注与原因 |
| `created_at` | 发生时间 |

详情页「历史」Tab 按时间倒序展示，与审计日志口径一致。

## 5. 操作路径（坐席视角）

1. 进入「工单管理」列表，按状态/优先级/分派人/SLA 风险筛选，点击行进入详情；
2. 阅读摘要、时间线与 SLA 剩余时间，确认影响面；
3. 用「分配」转派、用「编辑」修正字段、必要时走「审批 / 拒绝」；
4. 处理完成后点「提交解决方案」填写方案并结单（→ 已解决）；
5. 复核结果后点「关闭工单」进入终态；
6. 需要沉淀时，把解决方案整理为知识文章并关联到工单。

## 6. 常见问题

- **看不到解决/关闭按钮？** 依次检查：是否终态、是否具备 `ticket:update`、是否为创建人/受理人/管理角色。
- **关闭工单按钮不出现？** 该按钮仅在 `resolved` 或 `approved` 状态渲染，需先提交解决方案。
- **改了流程但旧工单没变？** 已运行实例沿用启动时的定义版本，属预期行为。
- **列表为空或工单不可见？** 与数据范围（部门/团队/客户）和角色权限有关。
