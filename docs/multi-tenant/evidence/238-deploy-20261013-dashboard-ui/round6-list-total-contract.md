# 238 缺口收口证据：列表 Total 契约（2026-10-13 第六轮）

> 状态：已完成（238 复验通过）｜定位：缺口收口证据（2026-10-13 第六轮）｜版本：`1.8.0-dev.22ee3603`（demo 8089）｜提交：`22ee3603`

## 1. 缺口

工作台工单列表与跨租户单据列表响应中 `total = len(items)`（**本页条数**），
与游标分页/limit 相关，无法支撑「共 N 条」类展示（上轮 round5 证据已登记）。

## 2. 修复

| 层 | 变更 |
|---|---|
| 工单列表（`service.ListTickets`） | 按目标租户在相同筛选（status/priority/q/updatedAfter）下 `Count` 求和 → `Total=全量`；与 limit/游标无关 |
| 单据列表（`service.listRecordRows`） | 三类（change/problem/service_request）在 status/q 筛选下计数并随行返回，聚合为 `Total` |
| DTO/前端类型 | `WorkbenchTicketListResponse.Total`、`MSPRecordListResponse.Total` 契约注释同步 |
| 前端展示 | 工作台「当前范围摘要」新增「**列表 N 条（当前筛选）**」，与「未关闭」口径并列 |
| 测试 | 游标分页用例断言 `Total` 恒为全量（A×2+B×1=3，分页两页均为 3）；go build + service 测试全绿 |

## 3. 线上验证（`GET /msp/workbench/*`，demo.msp.manager）

| 请求 | 修复前（预期） | 修复后实测 |
|---|---|---|
| tickets?customerTenantIds=3&limit=5（第 1 页） | total=5 ❌ | items=5，**total=12** ✅ |
| 同上（cursor 第 2 页） | total=5 ❌ | items=5，**total=12** ✅ |
| tickets?customerTenantIds=3&status=pending | total=2（巧合） | items=2，**total=2** ✅（筛选感知） |
| tickets?（全部客户） | total=50 ❌ | items=35，**total=35** ✅ |
| changes/problems/service-requests?customerTenantIds=3&limit=1 | total=1 ❌ | items=1，**total=3** ✅ |

前端：工作台摘要卡显示「未关闭 23 ｜ 超期 6 ｜ 临近 5 ｜ 未指派 0 ｜ **列表 35 条（当前筛选）**」，与列表 35 行一致。

## 4. 备注

- 本次仅改列表 `Total` 计数口径（多一次逐租户 `Count`，N=客户数个），未改其余分页/游标契约；
- 顺带观察（未处理）：nginx 日志出现来自 `/dashboard` 的 `GET /msp/workbench/summary → 403`（疑似非 MSP 会话触发顶栏 CustomerFilter 请求），
  与本次 Total 修复无关，建议后续核查顶栏过滤器的渲染门控是否也需要服务商上下文条件。
