# /msp「客户工单」列表标题/状态空白 —— 契约收口与 238 复验记录

- 日期：2026-10-13（CST）
- 环境：238 生产（`172.18.3.238:8088`，DB `itsm_prod_v2`，模式 `saas_msp`）
- 版本：修复前 `1.8.0-dev.788f83b7` → 修复后 `1.8.0-dev.a7972284`
- 提交：`a7972284`（`fix(msp): 客户工单列表契约收口，修复标题与状态列空白`）

## 1. 现象（用户报告）

`/msp` → 「客户工单」页签 → 选择客户后，列表渲染出行，但**「工单标题」「状态」两列为空**
（「创建时间」显示 `-`，「负责人」恒「未分配」）。

## 2. 根因

后端 `GET /api/v1/msp/customers/:customer_tenant_id/tickets` 把领域模型
`repository/ticket.Ticket`（无 json tag）直接交给 gin 序列化，响应键为 Go 字段名
**PascalCase**（`ID`/`Title`/`Status`/`CreatedAt`/`AssigneeID`）；前端表格与类型契约
（`types/msp.ts`）按 **camelCase**（`title`/`status`/`createdAt`）读取 → 标题、状态恒空。

旁证：同页其他 MSP 端点均使用 `dto.*` 显式 camelCase DTO（同域工作台列表
`dto.WorkbenchTicketItem` 也是 camelCase）；本端点从未收口。

## 3. 修复内容

后端（根因修复）：

- `dto/msp_dto.go`：新增 `MSPCustomerTicketItem` / `MSPCustomerTicketsResponse`（camelCase：
  `id/ticketNumber/title/status/priority/type/assigneeId/createdAt/updatedAt/tenantId`）；
- `handlers/msp/handler.go`：`GetCustomerTickets` 经 `toCustomerTicketItem` 映射为显式 DTO，
  不再序列化领域模型；
- 回归：`handlers/msp/handler_test.go::TestGetCustomerTickets_WithMSPContext` 增补契约断言
  （`title/status/ticketNumber/createdAt` 必须存在；`Title/Status` 不得出现）。

前端（类型收口 + 版本偏差窗口兜底）：

- `types/msp.ts`：`MSPCustomerTicketsResponse.tickets` 修正为**数组**（原类型声明为单对象），
  新增 `MSPCustomerTicket`；
- `msp-service.ts`：`getCustomerTickets` 归一化，兼容旧契约 PascalCase 键；
- 回归：`msp-service.test.ts` 增补旧契约归一化用例。

## 4. 验证

本地：

- 后端：`go build ./...` OK；`gofmt -l` 干净；`go test ./handlers/msp/...` PASS（含新契约断言）。
- 前端：`jest msp-service` 26/26 PASS；`tsc --noEmit` 0 错误；`eslint`（改动文件）0。

238 部署（v6，脚本 `.dev/deploy/deploy238_v6_*.sh`）：

- 版本 `1.8.0-dev.a7972284`；7/7 容器 healthy；`/`、`/msp`、`healthz`、`readyz` 均 200。
- 部署期处置：仓库 2379 个 root 属主文件（历史 sudo git 操作残留）阻断 `git checkout -f`
  （`unable to unlink ...: Permission denied`）→ `find -user root -exec chown n9e:n9e` 修正后
  重跑；部署脚本已补幂等 fetch 分支（分支已检出时跳过 fetch）。

238 功能复验（`1.8.0-dev.a7972284`）：

- API（provider_admin 演示账号，一次性口令，用后轮换作废）：
  `GET /api/v1/msp/customers/3/tickets` → 行键
  `assigneeId,createdAt,id,priority,status,tenantId,ticketNumber,title,type,updatedAt`；
  `title="[SMOKE-238-1012232309] 冒烟工单：VPN 无法连接"`、`status="in_progress"`；
  `rows=2`。
- 浏览器 UI（登录 → `/msp` → 「客户工单」→ 选择「冒烟客户A」）：

  | 工单标题 | 状态 | 创建时间 |
  |---|---|---|
  | `[SMOKE-238-1012232309] 冒烟工单：VPN 无法连接` | `in_progress` | 2026/10/12 23:23:11 |
  | `[SMOKE-238-1012232130] 冒烟工单：打印机离线` | `open` | 2026/10/12 23:21:33 |

  （浏览器快照为证；本轮截图仅存于会话，未落盘。）

## 5. 口令 / 账号卫生

- 本轮演示账号 `prov.ui.v6b1013112703`（id=22）与异常中断残留账号（id=21）的分配均已解除；
  id=22 口令已轮换（旧口令登录 → `2001`）。
- 平台侧用户治理接口（`/tenants/:id/users/:userId/status`、`force-logout`）在当前部署返回
  `TENANT_USER_ADMIN_DISABLED`（未启用），未能停用账号；id=21 口令为本轮进程内一次性随机值
  （未回显、未落盘），现为无分配的普通账号。
- 远端 `/tmp` 临时脚本与 cookie 已清理。

## 6. 残留 / 跟进

- **「负责人」列不回填**：DTO 仅返回 `assigneeId`，无 `assigneeName`，已分配工单仍显示
  「未分配」（接口实测 `assigneeId=13`）。修复需在服务端按 assigneeId 批量查 `user.name` 回填；
  属历史缺口，非本次回归。
- 平台侧用户治理开关未启用（`TENANT_USER_ADMIN_DISABLED`），238 上无法经平台通道停用账号，
  只能口令轮换。
