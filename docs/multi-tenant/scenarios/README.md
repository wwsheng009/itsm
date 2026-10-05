# 多租户业务场景目录（浏览器实操剧本）

> **状态**：当前（2026-10-05 全链路实测）｜**定位**：从**操作者视角**记录"在浏览器里怎么一步步做完多租户业务闭环"的可重现、可验证剧本
> **读者**：测试/QA、实施交付、服务商管理员、客户方管理员、研发联调
> **上位口径**：[canon（概念权威）](../plan/msp-concept-model-and-architecture-canon.md) ｜**设计与契约**：[实施方案](../plan/msp-implementation-plan.md) ｜**验收设计**：[业务验收设计](../plan/msp-multi-tenant-business-acceptance-design.md)
> **前置**：先读 [00 环境与账号](./00-environment-and-accounts.md)，再按本页顺序执行

---

## 1. 本目录是什么（与其它文档的分工）

| 文档位置 | 回答的问题 | 视角 |
|---|---|---|
| `plan/*` | 应该怎么设计（目标态、契约、决策） | 设计者 |
| `scripts/msp/acceptance/*.ps1` | 脚本化批量回归（API 通道，可 CI） | 自动化 |
| **`scenarios/*`（本目录）** | **一个人打开浏览器，从登录到交付，每一步点哪里、填什么、看到什么、怎么验证** | **操作者** |

本目录的每条步骤都满足：**界面反馈可观察**（点击/表单/提示/页面状态）+ **独立可验证**（接口或数据库命令），并标注 2026-10-05 实测结论。

### 1.1 一张图看懂全流程（2026-10-05 实测）

```text
[平台管理员]
   ├─ 场景01：建租户 MSPB2C（msp_provider）
   ├─ 场景02：建租户 B2CCUST（msp_customer，绑定 MSPB2C）
   └─ 场景03：开通向导（模板供给 → 首管 b2cprov_admin / b2ccust_admin → 首登改密）

[服务商管理员 b2cprov_admin]
   ├─ 场景04：建号 b2c_tech（provider_agent）
   └─ 场景05：分配（b2cprov_admin / b2c_tech → B2CCUST，primary）

[客户管理员 b2ccust_admin]
   └─ 场景06：建工单类型 → 建工单 #220（TKT-202610-000173）

[服务商员工（b2cprov_admin / b2c_tech）]
   └─ 场景07：工作台（跨客户筛选 → 回复 → 改状态=处理中 → 指派/批量）
             └─ 客户侧刷新可见回复与状态

[服务商管理员]
   └─ 场景08：审计看板（操作/拒绝留痕）+ 隔离反例（403 / 401 / 404）
```

## 2. 场景总览（建议按序执行）

| # | 文件 | 角色 | 核心动作 | 关键验证 |
|---|---|---|---|---|
| 00 | [00-environment-and-accounts.md](./00-environment-and-accounts.md) | 运维/测试 | 起服务、备账号、掌握通用验证手法 | `/healthz`、`/readyz`、登录 |
| 01 | [01-platform-create-provider-tenant.md](./01-platform-create-provider-tenant.md) | 平台管理员 | 新建 **MSP 服务商**租户 | 列表出现且 `type=msp_provider` |
| 02 | [02-platform-create-customer-tenant.md](./02-platform-create-customer-tenant.md) | 平台管理员 | 新建 **MSP 客户**租户并绑定服务商 | 详情显示所属服务商 |
| 03 | [03-tenant-provisioning-first-admin.md](./03-tenant-provisioning-first-admin.md) | 平台管理员 | 开通向导：模板供给 → 首个管理员 → 完成；首登强制改密 | readiness 全绿、首管可登录 |
| 04 | [04-user-provisioning.md](./04-user-provisioning.md) | 平台/服务商/客户管理员 | 建号（三条通道 + 邀请扩展） | 新账号可登录、租户正确 |
| 05 | [05-msp-allocation-management.md](./05-msp-allocation-management.md) | 服务商管理员 | 为员工分配客户；列表核对；解除 | 分配列表 2 条、agent 跨客户可访问 |
| 06 | [06-customer-ticket-type-and-ticket.md](./06-customer-ticket-type-and-ticket.md) | 客户方管理员 | 建工单类型 → 新建工单 | 工单号生成、列表可见 |
| 07 | [07-msp-workbench-collaboration.md](./07-msp-workbench-collaboration.md) | 服务商员工 | 跨客户工作台：筛选、回复、改状态、指派、批量 | 客户侧同步可见 |
| 08 | [08-audit-and-isolation-verification.md](./08-audit-and-isolation-verification.md) | 服务商管理员/测试 | 审计看板 + 隔离负向验证（403/401/404） | 错误码与审计留痕 |

> **依赖链**：01 → 02 → 03（两个租户都需要开通）→ 04 → 05 → 06 → 07 → 08。
> **可重入**：每个场景独立成篇；重跑建议**换一批新编码**（如 `MSPB2C02`/`B2CCUST02`），避免与既有数据混淆。

## 3. 角色与账号矩阵

| 角色 | 登录入口 | 账号口径 | 本次实测账号（测试环境专用） |
|---|---|---|---|
| 平台管理员 | `/login` | 本地开发缺省 `admin / admin123`（生产走 bootstrap 一次性口令，见 `docs/install.md`） | `admin` |
| 服务商管理员 | `/login` | 首管默认 `admin-<租户编码>`；类型 `msp_provider` → 身份 `provider_admin` + 租户内 `msp_admin` 角色 | `b2cprov_admin`（MSPB2C） |
| 服务商技术员 | `/login` | 服务商租户内建号，必带 MSP 角色 `provider_agent` | `b2c_tech` |
| 客户方管理员 | `/login` | 首管默认 `admin-<租户编码>`；类型 `msp_customer` → `customer_user` + `admin` 角色 | `b2ccust_admin`（B2CCUST） |
| 客户方普通用户 | `/login` 或邀请链接 `/invite/<token>` | 客户租户内建号或邀请受理 | （可选，按 04 执行） |

> 账号名与口令为**测试环境**数据，请勿用于生产；生产首管口令由开通向导一次性生成下发。

## 4. 通用验证手法（三件套）

1. **界面**：操作后的列表/详情/徽标/提示（本目录逐条给出预期文案）。
2. **接口**：浏览器已登录时直接在地址栏/DevTools 里调同源 API，或 PowerShell 脚本（见 [00 §6](./00-environment-and-accounts.md)）。
3. **数据库（可选）**：`psql` 直查租户/成员/分配/工单（命令见 [00 §5](./00-environment-and-accounts.md)）。

常用错误码速记：`401` 未认证/租户冲突拒绝；`403` 无权限（`MSP_ALLOCATION_REQUIRED`=未分配客户）；`404` 资源不存在或跨作用域隐藏；`409` 重复（如已存在首个管理员）；`422` 配额超限 `TENANT_QUOTA_EXCEEDED`。

## 5. 2026-10-05 实测基线（一次完整跑通）

| 项 | 实测值（示例） | 说明 |
|---|---|---|
| 服务商租户 | `MSPB2C`（id=7，`msp_provider`） | 场景 01 产出 |
| 客户租户 | `B2CCUST`（id=8，`msp_customer`，服务商=MSPB2C） | 场景 02 产出 |
| 首管 | `b2cprov_admin`、`b2ccust_admin`（一次性口令 → 首登强制改密） | 场景 03 产出 |
| 建号 | `b2c_tech`（`provider_agent`） | 场景 04 产出 |
| 分配 | 2 条 `primary` 指向 B2CCUST（技术员 + 服务商管理员） | 场景 05 产出；修复后管理页可见 |
| 工单 | `#220`（编号 `TKT-202610-000173`，客户 B2CCUST） | 场景 06 产出 |
| 协作 | 服务商回复可见、状态改为处理中 | 场景 07 产出 |
| 审计 | `/msp/audit` 打开正常（空集也不再白屏） | 场景 08 验证 |

> 你的环境里 id/编号会不同（顺序生成）；文档中的数值仅作对照样例。

## 6. 已知限制（执行前必读）

1. **建号通道未写 home membership / RBAC 角色边**：通过平台/MSP 通道新建的**员工**账号权限为空——能登录、MSP 数据面（有分配时）可调用，但菜单与前端路由守卫会拒绝（表现为 403 页/菜单缺失）。规避：临时用目标租户**首管**账号完成角色配置，或按 [04 §7](./04-user-provisioning.md) 的说明先行知悉。
2. **租户模板 7 项不含工单类型**：新客户租户「新建工单」类型选择器为空。规避：按 [06](./06-customer-ticket-type-and-ticket.md) 步骤先在 `/tickets/types` 建类型（或用「从预设安装」，需相应权限）。
3. **一键供给在低配机器上可能超过前端 120s 超时**：超时后模板可能仍在服务端完成。规避：重开向导查看 readiness；或使用 CLI 供给（见 [03 §6](./03-tenant-provisioning-first-admin.md)）。

以上均已在 [实施方案修订记录 v1.69](../plan/msp-implementation-plan.md) 登记，修复后请回填本页。

## 7. 维护约定

- **修订即回填**：界面文案/接口契约变化时，同步更新本目录受影响步骤与 `plan/` 对应文档；
- **不引入新编号空间**：本目录只用"场景 N"与文件名序号，不新增 `XX-#` 编号（编号注册表见 canon 附录 C）；
- **证据可追溯**：实测结论标注日期；关键修复关联提交与实施方案修订记录；
- **门禁**：本目录随 `docs/multi-tenant` 一并受 docs-gate **C.6** 约束（头部含日期与状态/定位；不得出现已废止锚点）。
