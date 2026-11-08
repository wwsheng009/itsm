# 客户主数据（Customer Master Data）设计

> 状态：**Draft v1.1（设计方案修订）**
> 更新日期：2026-11-08
> 适用：产品、架构、后端、前端、交付
> 关联模块：邮件报障（客户资料 / 支持合同 / 来源组织）、多租户（MSP 客户维度）、项目管理（交付项目）、SLA、CMDB

## 1. 为什么需要这份设计

当前「客户资料」已经具备一套**结构正确但字段偏薄**的主数据骨架：

| 实体 | 现有字段 | 能力边界 |
|---|---|---|
| `ServiceCustomer`（客户） | `tenant_id`、`name`、`normalized_name`、`short_name`、`aliases`、`historical_names`、`status`、`linked_customer_tenant_id` | 只能支撑「邮件来件按名字匹配到哪个客户」 |
| `CustomerBranch`（分支） | `tenant_id`、`customer_id`、`name`、`normalized_name`、`aliases`、`status` | 只是「客户的第二层名字」，无地址 / 时区 / 服务点语义 |
| `SupportContract`（合同） | `tenant_id`、`customer_id`、`branch_id`、`contract_number`、`status`、`start_at`、`end_at`、`source_document_id` | 无 SLA、无计费、无续签语义 |
| `SourceOrganization`（来源组织） | `tenant_id`、`name`、`normalized_name`、`email_addresses`、`email_domains` | 只作为邮件来源归一 |

这套模型在「邮件智能报障」场景是好用的，但一旦进入 **MSP 多租户交付、客户分级运营、项目交付、集团客户树** 这类场景，字段就不够了：

- 一个客户只能挂**一个**关联租户（`linked_customer_tenant_id` 单值），无法表达「集团一个主档 ↔ 多个法人租户」「生产 / 测试 / 归档多租户」「区域子公司多租户」；
- 客户与客户之间**没有任何关系表达**（集团 - 子公司、经销商、渠道伙伴、被并购方）；
- 客户与**项目**之间没有关联（项目管理侧按 D-01 明确不设 `customer_tenant_id`，需要一条合规的关联通道）；
- 客户没有**联系人**，只能借用 `users` 表；但客户联系人大多数并不是系统用户；
- 没有**编码 / 统一社会信用代码 / 行业 / 分级 / 负责人 / SLA / 结算**等运营主数据字段，无法做客户分层运营与对账；
- 没有**合并去重**通道，`aliases` / `historical_names` 只能靠人工塞字符串。

本文档在不破坏现有实现（邮件报障匹配、租户隔离、D-01 单租户原则）的前提下，把「客户资料」升级为**客户主数据（Customer Master）**。

## 2. 文档结构

| 文件 | 内容 |
|---|---|
| [01-customer-master-data-design.md](./01-customer-master-data-design.md) | 主设计：定位与原则、现状差距、实体分层、字段字典、关联模型、多租户场景、项目关联、生命周期、去重合并、AI 匹配、API、UI、迁移与验收 |
| [02-ddl-and-migration.md](./02-ddl-and-migration.md) | 可落库 DDL 草案、复合外键 / 唯一索引、交接包、回填脚本、RLS 与受管清单登记、回滚策略；合同最终 DDL 以 06 §7.2 为准（02 §12.1 过渡版已撤销） |
| [03-open-questions.md](./03-open-questions.md) | 待裁决问题、与既有契约的冲突点、分期落地建议 |
| [04-multi-provider-scenario.md](./04-multi-provider-scenario.md) | **多服务商场景**：三种拓扑辨析、不跨租户下的 5 个真实用法、服务边界 `coverage_scope`、交接包与归属切换、反模式清单 |
| [05-customer-contract-project-linkage.md](./05-customer-contract-project-linkage.md) | **客户 → 合同 → 项目业务闭环**：`contract_project_links`、五条归属铁律（含 `branch_id` 同租户/同客户修复与 L-04 合并例外）、`billing` 分配行、执行率核算、续签与在途项目时序联动、三向正查路径 |
| [06-contract-modeling-design.md](./06-contract-modeling-design.md) | **合同建模与业务数据逻辑优化**：三套合同实体（采购/交付/结算）职责划分、统一契约 CC-1~CC-8 的域适用矩阵、六个可验证缺陷、服务项/变更单/用量台账、`effective_amount` 与预占/实耗语义、状态派生、迁移风险 |
| [07-tenant-data-boundary.md](./07-tenant-data-boundary.md) | **租户数据边界（硬约束）**：BD-1~BD-4、授权表 allowlist 审计、**发现 `msp_contracts` RLS 012 违规**、**§4.5 收回裁决**、`018/019` 合同快照、**Q-09 裁决 B 的 `020/021` SLA 定义快照**、迁移与验收 |

## 3. 一句话结论

> 保留 `ServiceCustomer` 作为**租户内客户主档**不变，做三件事：
>
> ① 给主档补齐「标识 / 分类 / 分级 / 联系 / 服务 / 财务 / 生命周期 / 数据治理」八组字段；
>
> ② 用**关联表**（客户关系、租户映射、项目链接、联系人）替代单值字段与字符串别名，让一个客户能同时连到多个租户、多个项目、多个联系人和一张客户关系网；
>
> ③ 用**唯一键 + 合并通道 + 数据质量分**把「同一个客户的多张皮」收敛成一条主档。
>
> 全程遵守两条硬约束：**档案永远按 `tenant_id` 归属**（不替代租户隔离）；**跨租户一律用弱引用 + 服务层校验 + MSPAllocation 授权**（不建跨租户外键，不违反项目 D-01）。
>
> 合同侧另按 [06](./06-contract-modeling-design.md) 做“三域不合并、契约分阶段收口”：**金额与数量分列、预占与实耗分离、所有实耗经用量台账幂等回写**。

## 4. 关键设计取舍（先看这里）

| 取舍点 | 结论 | 理由 |
|---|---|---|
| 客户档案是否等于租户？ | **不等于**，`ServiceCustomer` 是租户内的业务档案，租户是隔离边界 | 现状 `docs/multi-tenant/03-customer-dimension.md` §7 已明确「两回事」，且禁止用档案替代隔离 |
| 客户 ↔ 租户是 1:1 还是 1:N？ | **1:N**（新增映射表），旧单值列降级为兼容只读 | 集团客户 / 多法人 / 多环境（生产 - 测试 - 归档）真实存在 |
| 项目是否加 `customer_tenant_id`？ | **不加**，改为租户内 `project_customer_links` | 遵守项目管理 D-01「不建任何指向他租户的字段、外键或关联表」 |
| 客户关系是否用闭包表 / 祖先路径？ | **只用邻接表 + 深度限制（≤5）**，需要树时递归 CTE | 客户关系规模远小于 BOM，闭包表维护成本大于收益 |
| 别名是否继续用 JSON 数组？ | **保留**用于模糊匹配，但**主键识别改用 `email_domains` + `tax_id`** | JSON 别名无法建唯一索引，无法做权威去重 |
| 合并是否物理删除？ | **不删**，`merged_into_id` 标记 + 关联重指向 + 审计 | 邮件会话、合同、工单历史必须可回查 |

## 5. 分期建议（摘要）

| 阶段 | 范围 | 价值 |
|---|---|---|
| **P0（2 周）** | 主档八组字段 + 联系人表 + 租户映射表 + 编码 / 税号唯一键 | 立刻可做客户分层、多租户映射、联系人通知 |
| **P1（2 周）** | 客户关系表 + 项目客户链接 + 合并通道 + 数据质量分 | 支撑集团客户树、项目交付视图、脏数据治理 |
| **P2（2 周）** | 外部标识对账 + AI 域名匹配增强 + 客户 360 视图 | 支撑跨系统对账、邮件报障命中率提升 |
| **C1（与 P1 衔接）** | 合同聚合根 + 服务项 / 变更单 / 用量台账 + `billing` 分配行 | 合同可履约、额度可核算、预占/实耗分离 |
| **C2（与 P2 衔接）** | `018` 快照双写 + `019` G17 收回 + 结算快照 | 客户侧不再直读 provider 合同，BD-1 可审计 |
| **C3（与 C2 并行）** | `020` SLA 定义快照 + `021` 删除 `sla_definition_customer_read` | Q-09 关闭，customer 不再读 provider 的 SLA 定义原行 |

详见 [03-open-questions.md](./03-open-questions.md)。

## 6. 当前实施状态（2026-11-08）

| 阶段 | 状态 | 已落地内容 |
|---|---|---|
| **P0** | **已实现** | `service_customers` 八组字段、`customer_branches` 服务点字段、`customer_contacts`、`customer_tenant_links`、编码/税号唯一键、客户详情/分组表单、联系人/租户映射/服务点 CRUD、服务端分页筛选、RLS 022、迁移与回滚、服务层/HTTP 回归测试 |
| **P1** | **已实现** | `customer_relationships`、`project_customer_links`、合并通道与 7 天撤销、数据质量分/排序/筛选、关系树 API/UI、客户详情关系网与关联项目 Tab、项目详情关联客户 Tab、RLS 023、迁移与回滚、服务层/HTTP 回归测试 |
| **P2** | **已实现** | `customer_external_ids`、AI 五级匹配（L1-L5）、客户 360 视图、外部标识对账、邮件候选客户回显 |
| **C1** | **已实现** | `support_contracts` 聚合根字段、服务项、变更单、用量台账、`contract_project_links`、签订/终止/续签、额度视图、合同侧与项目侧闭环 |
| **C2/C3** | 未开始 | 合同/SLA 定义快照与 RLS 收回 |

> P0/P1/P2/C1 实现入口：`/master-data/customers` 与 `/master-data/contracts`；API 前缀仍为 `/api/v1/master-data/*`，旧 `/api/v1/email-intake/*` 保持兼容。客户详情已展示关系树、关联项目、合并记录、外部标识和客户 360；合同详情已展示服务项、变更历史、用量台账、合同项目分配与额度视图。
