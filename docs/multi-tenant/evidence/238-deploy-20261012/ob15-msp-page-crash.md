# OB-15：/msp 客户管理页整页崩溃 —— 修复与 238 复验记录

- 日期：2026-10-13（CST）
- 环境：238 生产（`172.18.3.238:8088`，DB `itsm_prod_v2`，模式 `saas_msp`）
- 账号：`prov.ui.demo1012234321`（role=end_user，mspRole=provider_admin，tenant 2「冒烟服务商」）
- 版本：修复前 `1.8.0-dev.50cd02c5` 之前（b50e7c7a 同源前端）→ 修复后 `1.8.0-dev.788f83b7`
- 提交：`50cd02c5`（契约收口+前端容错）、`788f83b7`（分配列表客户列收口）

## 1. 现象（用户报告）

`http://172.18.3.238:8088/msp` 页面访问异常：整页显示 ErrorBoundary 兜底页
（「页面出现错误 / 抱歉，页面遇到了一个意外错误」），无法进入客户管理。

## 2. 取证（浏览器）

控制台（修复前）：

```
[error] Failed to load resource: 403 (Forbidden)          → GET /api/v1/dashboard/overview（噪声：页面不消费，hook 已兜住）
[error] Dashboard Overview API调用失败: Error: 权限不足   → 同上（console.error 兜底路径）
[error] TypeError: k.reduce is not a function             → 真凶：客户报表数据被当数组 reduce
[error] ErrorBoundary caught an error: TypeError: k.reduce is not a function
```

网络：`GET /api/v1/msp/reports/customers?startDate=…&endDate=…` → 200，响应体：

```json
{"code":0,"message":"success","data":{"reports":[{"date_from":"…","date_to":"…","status_summary":{"in_progress":1,"new":1},"total_tickets":2}],"total":1}}
```

## 3. 根因链

1. **契约断裂（主因）**：后端 `GetCustomerReports`（`itsm-backend/service/ticket_service.go`）为 V2 stub——
   把全部可访问客户聚合为**单条 map**（`total_tickets`/`status_summary`/`date_from`/`date_to`）并包在
   `{reports, total}` envelope 中；而前端 `MSPAPI.getCustomerReports` 声明返回 `MSPCustomerReport[]`，
   `MSPService.getCustomerReports` 直接 `return res || []` → 页面拿到**对象** →
   `reports.reduce(...)` 抛 `TypeError` → ErrorBoundary 整页崩溃。
2. **字段语义错配（次生）**：即便不崩，聚合行也没有 UI 需要的逐客户字段
   （`customerName`/`resolvedTickets`/`mspHandlingTimeAvg`/`slaComplianceRate`）→ 卡片 NaN、表格空列。
3. **同页列错位（顺带发现）**：分配列表「客户租户」列读 `customerName`，但后端分配 DTO 字段为
   `customerTenantName`（management 页已兼容，本页漏改）→ 该列恒空。

> 设计意图旁证：`dto.MSPCustomerReport`（camelCase 字段与前端类型逐字段一致）与
> service 测试名「V2 GetMSPCustomerReports 按 msp 员工可访问客户维度聚合统计」均已声明按客户维度输出，
> 说明 stub 从未收口；`multiple/单聚合` 属实现缺口而非契约变更。

## 4. 修复内容

后端（`50cd02c5`）：
- `GetMSPCustomerReports` 重写：按 `ensureAccessibleCustomerIDs`（fail-closed，无分配返回空集合）
  逐客户聚合，输出 `[]dto.MSPCustomerReport`：
  `total_tickets`=区间内该客户工单数；`resolved_tickets`=`resolved`+`closed`；
  `msp_handling_time_avg`=已解决工单 `(updated_at-created_at)` 平均小时（无样本=0）；
  `period`=`YYYY-MM-DD ~ YYYY-MM-DD`；`sla_compliance_rate` 暂无稳定口径 → `*float64` 为 `nil` 时省略（前端显示 `-`）。
- 客户名批量查询（Tenant by IDIn），排序（名称→ID）保证输出稳定。
- 测试：`TestTicketService_GetMSPCustomerReports_AllocationAware`（3 单/2 解决/1 未授权客户排除/无分配空集合）；
  `TestGetCustomerReports_HonorsCamelCaseDateRange` 增补逐客户行 + camelCase 字段断言。

前端（`50cd02c5` + `788f83b7`）：
- `MSPAPI.getCustomerReports` 返回类型改为 `{reports, total}` 契约；
- `MSPService.getCustomerReports` 解包 `{reports}`，兼容历史裸数组，异常形状收敛 `[]`；
- `/msp` 卡片 `reports.reduce` 空值兜底（`?? 0`）、SLA/均值空值显示 `-`；
- 分配列表「客户租户」列读 `customerTenantName` → 兼容 `customerName` → 兜底 `#id`。

## 5. 验证

- 后端定向：`go build ./...` OK；`go test ./service -run GetMSPCustomerReports` PASS；
  `go test ./handlers/msp -run TestGetCustomerReports` PASS。
- 前端定向：`msp-service/msp-api` 2 套件 44/44 PASS；`tsc -b` 0；`eslint`（msp/index.tsx）0。
- 238 部署：v4 `1.8.0-dev.50cd02c5`（全量：backend+frontend）→ v5 `1.8.0-dev.788f83b7`（增量：同 VERSION 链）；
  7/7 容器 healthy，`/api/v1/version` = `1.8.0-dev.788f83b7`。
- 238 浏览器复验（最终版）：
  - `/msp` 正常渲染：卡片「负责客户数 1｜分配数量 8｜本月工单 2｜已解决 0」；
  - 「分配列表」8 行客户列均显示「冒烟客户A」；
  - 「服务报表」页签：冒烟客户A｜工单总数 2｜已解决 0｜解决率 0%｜平均处理时长 `-`｜SLA `-`；
  - 接口 `GET /api/v1/msp/reports/customers` 返回
    `{"reports":[{"customerTenantId":3,"customerName":"冒烟客户A","period":"2026-09-13 ~ 2026-10-13","totalTickets":2,"resolvedTickets":0,"mspHandlingTimeAvg":0}]}`
    （无 `slaComplianceRate` 字段=无样本省略）；
  - **控制台零报错**（ErrorBoundary 不再触发）。

## 6. 残留 / 跟进

- `sla_compliance_rate`：票据 `sla_resolution_deadline` 可用但需明确「样本口径」（建议：仅有 deadline 的
  已解决工单计入，样本为 0 时省略字段）。待产品口径确认后实现，前端已按 `nil → '-'` 就绪。
- `GET /api/v1/dashboard/overview` 对纯 MSP 账号 403（首页降级为空数据，非本页问题）；
  如要求 MSP 账号首页可用，需评估 `dashboard.read` 对 provider 角色的授予口径（独立议题）。
