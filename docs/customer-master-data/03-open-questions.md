# 03 · 待裁决问题与分期建议

> 状态：Draft v1.0 · 2026-11-06
> 用途：把设计里「需要拍板」的点集中列出，避免边写代码边改模型。

## 1. 必须裁决的问题（影响表结构）

| 编号 | 问题 | 备选方案 | 建议 | 影响面 |
|---|---|---|---|---|
| Q-01 | 客户是否会成为**跨租户共享实体**（一个客户主档被多个服务商 / 多个内部租户共用）？ | A. 严格租户内（现状）<br>B. 引入全局客户池 + 租户订阅 | ✅ **已裁决（2026-11-06，用户拍板）：A —— 客户不跨租户共享，所有数据都不跨租户。** 不引入全局客户池、不引入 `customer_global_id`。多服务商场景的完整用法见 [04-multi-provider-scenario.md](./04-multi-provider-scenario.md) | 已关闭 |
| Q-02 | `linked_customer_tenant_id` 是否保留？ | A. 保留只读兼容<br>B. 直接废弃 | **A**。灰度期双读，稳定后（约 2 个版本）再标记废弃 | 中 |
| Q-03 | 客户主档是否允许**一个客户对应多个 `msp_provider`**？ | A. 允许（多服务商分包）<br>B. 单一 provider | ✅ **随 Q-01 收敛为 B**：客户只归属一个 provider（`tenants.msp_provider_id` 单值，对齐 IP-P0-4）。"多服务商"= N 个 provider 各自持有该客户的**独立主档副本**，物理隔离、互不可见，靠 `tax_id` / `email_domains` 做逻辑对齐 | 已关闭 |
| Q-04 | 项目客户关联是否需要支持**跨租户**（服务商项目服务客户租户）？ | A. 仅同租户<br>B. 允许 provider 租户项目关联 customer 租户 | **A（v1）**，跨租户走 `docs/project-management/05-msp-delivery.md` 的受控访问，不建链接表 | 高 |
| Q-05 | 客户关系表是否需要**时间维度**（历史集团结构）？ | A. 只存当前<br>B. `effective_from/to` + `status` | **B**，成本低，审计友好 | 中 |
| Q-06 | 合并后原客户档案是否允许**恢复**？ | A. 不可恢复<br>B. 保留 `merged_into_id` 可回滚 | **B**，但需限制为 7 天内 + 需 `customer.merge.undo` 权限 | 中 |
| Q-07 | 联系人是否复用 `users` 表？ | A. 独立表 + 可选 `linked_user_id`<br>B. 直接建用户 | **A**。客户联系人大多不是系统用户，建用户会污染成员身份与配额 | 高 |
| Q-08 | `tax_id` 唯一性范围？ | A. 租户内唯一<br>B. 全局唯一 | **A**，与既有 `(tenant_id, normalized_name)` 口径一致 | 中 |

## 2. 与既有契约的冲突点（需显式确认）

| 冲突点 | 既有约定 | 本设计处理 |
|---|---|---|
| 项目不得有客户字段 | `docs/project-management/02-data-model.md` D-01：不建任何指向他租户的字段、外键或关联表 | 不在 `projects` 加列，新增**同租户** `project_customer_links`；跨租户仍走受控访问 |
| 客户档案 ≠ 租户 | `docs/multi-tenant/03-customer-dimension.md` §7：`ServiceCustomer` 与租户是两回事，且不得替代隔离 | 全文坚持「档案按 `tenant_id` 归属」，租户映射用弱引用表 |
| 归属唯一写通道 | IP-P0-4：`msp_customer` 必须且只能通过 `mspProviderId` 归属 | 客户主档**不引入**新的归属字段，归属仍由 `tenants.msp_provider_id` 单点决定 |
| 成员身份单源 | IP-P1-1：`UserTenantMembership` 是账号在某租户内角色/组织/生效期的唯一载体 | 客户经理 / 服务负责人字段只存 `user_id`，写入时校验其为同租户活跃 membership（对齐 `projects.manager_id` 校验口径） |
| 平台级共享数据 | `tenant_guard.go` 中标签云、市场模板等跨租户共享 | 客户主数据**不属于**平台级共享数据，必须 RLS 全覆盖 |
| **`msp_contracts` 客户只读授信** | `012_msp_contract_policies.sql:54-58 / 81-88` 的 `msp_contract_customer_read`、`msp_contract_binding_customer_read` 允许 customer 租户直读 provider 合同行 | ⚠️ **判定违规**：违反 BD-1「业务数据不跨租户」，**裁决收回**。唯一真实依赖是 L1 SLA 匹配，改为读客户租户内快照 `contract_sla_snapshots`；结算/下发走 `settlement_statements`。**须先建快照通道再 DROP**，否则 L1 静默降级。见 [07 §3.2 / §4.5](./07-tenant-data-boundary.md) |

## 3. 分期落地建议

### P0（约 2 周）：字段补齐 + 两张关联表

- 主档八组字段（标识 / 分类 / 分级 / 联系 / 服务 / 财务 / 生命周期 / 治理）；
- `customer_contacts`、`customer_tenant_links`；
- 编码 + 税号唯一键；分支服务点字段；
- 前端：客户详情抽屉 + 新增/编辑表单分组；列表增加「等级 / 客户经理 / 关联租户数 / 联系人」列；
- 验收：老数据全部可读、邮件报障匹配结果不变。

### P1（约 2 周）：关系网 + 项目关联 + 合并

- `customer_relationships`、`project_customer_links`；
- 合并通道（`POST /customers/:id/merge`）+ 关联重指向 + 审计；
- `data_quality_score` 计算与列表排序；
- 验收：集团树可展开；项目详情可看到客户；合并后历史工单/会话仍可回查。

### P2（约 2 周）：对账 + AI 增强 + 360

- `customer_external_ids`；
- 邮件 intake 匹配升级为「域名 → 税号 → 精确名 → 别名 → 模糊」五级；
- 客户 360 视图（工单 / 合同 / 项目 / 资产 / SLA / 联系人一屏）。

## 4. 风险登记

| 风险 | 等级 | 缓解 |
|---|---|---|
| 新增部分唯一索引在存量脏数据上创建失败 | 中 | 迁移前先跑「潜在冲突清单」查询，人工处理后再建索引 |
| 主档字段膨胀导致列表查询变慢 | 中 | 列表只 `SELECT` 展示列，详情单独拉取；必要字段走覆盖索引 |
| 合并误操作导致数据错乱 | 高 | 二次确认 + 影响面预览（工单/会话/合同/项目条数）+ 审计 + 7 天回滚窗口 |
| 客户关系形成环（A→B→A） | 中 | 写入时做环检测（深度优先，限深 5），拒绝成环 |
| RLS 漏登记导致跨租户可见 | **高** | 新表全部入 `rls-apply` 清单，CI 增加「新表未登记即失败」检查 |

## 5. 验收清单（Definition of Done）

1. 客户列表 / 详情 / 新增 / 编辑在新字段下正常工作，中英文与暗色主题无布局破版；
2. 同一客户可挂 ≥2 个租户，且租户映射可标记结算锚点；
3. 客户关系可表达「集团 - 子公司」「伙伴」「前身」三类，树上无环；
4. 项目详情可见所属客户，客户详情可见关联项目；
5. 合并后：被合并档案只读、关联全部重指向、审计日志完整、可回滚；
6. 客户 A 租户的用户无法读取客户 B 租户的任何主数据（含联系人与关系）；
7. 邮件报障原有匹配用例 100% 通过。
