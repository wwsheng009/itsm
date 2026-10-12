# 238 新库建租户冒烟运行摘要（2026-10-12）

> 状态：**PASS（9/9）**｜定位：全新生产库 `itsm_prod_v2` 上的平台建租户链路冒烟（服务商+客户租户 → 模板开通 → 首管 → 登录/对账），经公网入口 `http://172.18.3.238:8088`。
> 关联：[238 部署运行摘要](./run-summary.md)｜[backlog](../../backlog.md)

## 1. 结果（PASS=9 / FAIL=0）

| # | 用例 | 结果 | 证据 |
|---|---|---|---|
| 1 | 平台管理员登录 | PASS | `code=0`（admin / super_admin） |
| 2 | 创建服务商租户 | PASS | `id=2` `code=SMKMSP1012231808` `type=msp_provider` |
| 3 | 创建客户租户（绑定 provider=2） | PASS | `id=3` `code=SMKCUST1012231808` `type=msp_customer` |
| 4 | 模板开通（服务商） | PASS | `ready=true` |
| 5 | 模板开通（客户） | PASS | `ready=true` |
| 6 | 服务商首管创建 + 首登强制改密标志 | PASS | `smoke.msp.admin`（`mustChangePassword=true`） |
| 7 | 客户首管创建 | PASS | `smoke.cust.admin` |
| 8 | 租户列表对账（含两新租户） | PASS | 列表返回包含两 code |
| 9 | 客户用量对账 | PASS | `used.users=1 / ticketsThisMonth=0` |

## 2. 说明

- 脚本：本机 `.dev/deploy/deploy238_smoke_tenants.sh`（未入库），在 238 上执行；管理员口令仅从 `.env.prod` 读取、不回显。
- **CSRF 要点**：实例写接口需 `X-CSRF-Token`（登录后 `GET /api/v1/csrf-token` 取 `data.csrf_token`；校验成功后会轮换令牌，故每次写前重取——与前端 SPA 行为一致）。v1 冒烟因未携带被 `4031 CSRF token missing` 拒绝，补 token 后全绿。
- 冒烟新建资源保留供后续联调（服务商 `SMKMSP1012231808`、客户 `SMKCUST1012231808`）；如需清理可在“租户管理”停用/删除（当前版本无物理删除，停用为准）。
- 本轮为 API 级冒烟（覆盖后端建租户/开通/首管/登录全链路）；UI 级点击路径可另用租户管理员账号执行。
