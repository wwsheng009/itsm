# 238 demo 验收 Runbook —— SLA-A1…A17（部署 c700c060 后执行）

> 目标环境：`172.18.3.238` demo 栈（`http://172.18.3.238:8089/`，后端直连 `127.0.0.1:8091`）。
> 版本：`1.8.0-dev.c700c060`（P0+P1+P2-1+P2-2+P2-3+P3 全量）。
> 前置：部署 D 步 `D-STATUS: DONE`。

## 0. 凭证与通用准备

```bash
# SSH helper（本地 PowerShell 先设 ITSM_DEPLOY_HOST/USER/PASSWORD，见 skill）
python .dev/deploy/remote.py --sudo 'grep -E "^DEMO_ADMIN_PASSWORD=" /home/n9e/itsm/repo/.env.demo'
# 库内账号清单
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc \
  "SELECT id, username, tenant_id, active FROM users ORDER BY id LIMIT 20"
# demo.msp.* / demo.mfg.* 口令策略见 pkg/seeder（如非 DEMO_ADMIN_PASSWORD 需按 seeder 规则）
```

登录取 token（admin 为例）：
```bash
curl -s -X POST http://127.0.0.1:8091/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<DEMO_ADMIN_PASSWORD>"}' | jq -r '.data.token'
```
后续所有鉴权请求带 `-H "Authorization: Bearer $TOKEN"`。

## 1. 已可由部署 D 步覆盖的机械锚点

| 锚点 | 覆盖方式（D 步输出） |
|---|---|
| A6 部分 | `settlement=401`（路由存在 + 鉴权）；越权 403 需登录后测（见 §3） |
| A8 部分 | 无 token 全部 401（settle/settlement/distributions 探针） |
| A15/A16/A17 前置 | `ticket_type_distributions` 表 + RLS 013 策略在位；distributions 路由 401 |

## 2. 无需登录的场景锚点（DB + 服务端副作用断言）

| 锚点 | 验证方法 |
|---|---|
| A3 fail-loud | 在无下发的 demo 租户建单（用户侧）→ 断 `tickets.sla_definition_id=0 AND sla_matched_layer='L4'`；`sla_unbound` 结构化日志（worker/backend logs，`SLA_UNBOUND_ALERT_ENABLED` 关时为 Warn） |
| A11/A13/A14 | 构造场景后 `GET /sla/settlement-report`（provider token）断言：分母=覆盖工单、rows 三列、L3* 违约不计（已由 service 测试锁定；238 侧做数据复验） |
| A15/A16/A17 | 走 `POST /msp/customers/:cid/ticket-types/:id/distribute`（provider token）+ DB 断言 origin 三元组 / INV-1/2/3/4 / 解绑转 native / 源删除 orphaned |

## 3. HTTP 级断言（无 token / 越权 / 非 MSP）

```bash
# 无 token（期望 401，全列表）
for p in /api/v1/sla/settlement-report /api/v1/sla/violations/1/settle; do ...
# 非 MSP 部署 gate：demo 为 saas_msp 不受影响；prod 部署模式相关 404 见 router 测试
# A6 越权：provider A 的 token 查 provider B 的 customerTenantId → 403 且响应无任何计数（仍由服务端测试锁定）
```

## 4. 需 UI/口令项（标记）

| 锚点 | 说明 |
|---|---|
| A7 表单枚举 / A15 customer 侧受控锁 UI | 浏览器操作需 demo 口令（P9）；API 面已由服务测试覆盖，UI 面人工抽查 |
| A13/A17 前端页（/reports/sla、监控页争议按钮、工单进度条） | 浏览器抽查（强刷）|
| A1/A2/A5/A9 | 需建单/合同场景，建议脚本化：用 admin/msp token 建数据后走 DB 断言 |

## 5. 反例清单（必须拒绝，逐条打勾）

- [ ] provider 直改 customer 定义行 → 拒绝（RLS/归属校验）
- [ ] customer 自建 violation 进合同结算 → 拒绝（matched_layer 过滤 + 重算）
- [ ] 匹配失败无痕 → 拒绝（告警日志 + 报表未绑定桶）
- [ ] 结算报表取数前未校验归属（响应带计数）→ 拒绝
- [ ] 非归属 customer 写 grant/distribution → 拒绝（mspguard）
- [ ] 类型下发触碰 native 行 → 拒绝（INV-1~5）
- [ ] 非 saas_msp 下 grant 写端点未 404 → 拒绝（部署模式 gate）
- [ ] 放宽 sla_definitions 列表/强开 FORCE RLS 做"列表注入" → 拒绝（走 provided 端点）

> 执行记录回填：`.dev/deploy/DEMO-FULL-<date>-c700c060-EVIDENCE.md`（部署证据）与本 runbook 勾选结果。
