# 03 · 待裁决问题与分期建议

> 状态：Draft v1.1 · 2026-11-08
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
| Q-09 | 011 的 `sla_definition_customer_read` 是否保留？ | A. 保留（显式授权通道例外）<br>B. 按 BD-1 收回，改为客户租户内物化快照 | ✅ **已裁决（2026-11-08）：B（B-lite）**。新增 `sla_definition_snapshots`（`020`，`source_kind=contract_binding|grant`）物化响应/解决时限、工作时段、匹配条件与客户工单类型；L1/provided 已改读快照，AI/搜索/导出须走同一快照查询；观察通过后 `021` 删除 `sla_definition_customer_read`。`sla_definition_grants` 保留为 provider 侧授权记录，`sla_grant_customer_read` 可继续作为授权元数据读取（07 §1.2 allowlist）；后续确认无直接查询后再评估删除 | 已关闭 |

## 2. 与既有契约的冲突点（需显式确认）

| 冲突点 | 既有约定 | 本设计处理 |
|---|---|---|
| 项目不得有客户字段 | `docs/project-management/02-data-model.md` D-01：不建任何指向他租户的字段、外键或关联表 | 不在 `projects` 加列，新增**同租户** `project_customer_links`；跨租户仍走受控访问 |
| 客户档案 ≠ 租户 | `docs/multi-tenant/03-customer-dimension.md` §7：`ServiceCustomer` 与租户是两回事，且不得替代隔离 | 全文坚持「档案按 `tenant_id` 归属」，租户映射用弱引用表 |
| 归属唯一写通道 | IP-P0-4：`msp_customer` 必须且只能通过 `mspProviderId` 归属 | 客户主档**不引入**新的归属字段，归属仍由 `tenants.msp_provider_id` 单点决定 |
| 成员身份单源 | IP-P1-1：`UserTenantMembership` 是账号在某租户内角色/组织/生效期的唯一载体 | 客户经理 / 服务负责人字段只存 `user_id`，写入时校验其为同租户活跃 membership（对齐 `projects.manager_id` 校验口径） |
| 平台级共享数据 | `tenant_guard.go` 中标签云、市场模板等跨租户共享 | 客户主数据**不属于**平台级共享数据，必须 RLS 全覆盖 |
| **`msp_contracts` 客户只读授信** | `012_msp_contract_policies.sql:54-58 / 81-88` 的 `msp_contract_customer_read`、`msp_contract_binding_customer_read` 允许 customer 租户直读 provider 合同行 | ⚠️ **判定违规**：违反 BD-1「业务数据不跨租户」，**裁决收回**。唯一真实依赖是 L1 SLA 匹配，改为读客户租户内快照 `contract_sla_snapshots`；结算/下发走 `settlement_statements`。**须先建快照通道再 DROP**，否则 L1 静默降级。见 [07 §3.2 / §4.5](./07-tenant-data-boundary.md) |
| **011 SLA 定义授信** | `011_sla_definition_grant_policies.sql:60-88` 的 `sla_grant_customer_read` / `sla_definition_customer_read` | ✅ **Q-09 裁决 B**：`sla_grant_customer_read` 仅作为授权元数据 allowlist；`sla_definition_customer_read` 在 `020` 快照双写、L1 切换并通过观察后，由 `021` 删除。客户统一读 `sla_definition_snapshots`，不再读 provider 的 `sla_definitions` 业务行 |

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

### C1（与 P1 衔接）：合同聚合根 + 项目额度闭环

- `support_contracts` 最终字段/状态/有效期（06 §7.2）；
- `contract_service_items`、`contract_amendments`、`contract_usage_ledger`；
- `contract_project_links` 的 `billing` 行、服务项复合外键与 M-1~M-5；
- 验收：预占/实耗分离、同一 source 不重复扣减、分配额硬阻断。

### C2（与 P2 衔接）：结算快照 + G17 收回

- `018` 建 `contract_sla_snapshots` / `settlement_statements` 并双写；
- L1 改读快照的 `sla_terms`，观察一个结算周期；
- 满足 C1–C4 后执行 `019` 删除 012 两条 customer_read 策略；
- 验收：客户侧无 `msp_contracts` 直读；合同域审计为 0 行（SLA 定义由 C3 收口）。

### C3（与 C2 并行/紧随）：SLA 定义快照 + 011 收回

- `020` 建客户租户内 `sla_definition_snapshots`（`source_kind=contract_binding|grant`，含 `terms`、`ticket_type_id`、`source_version`、`payload_hash`）+ RLS；
- provider 在定义/授权发布、变更时下发新版本，撤销时置 `revoked`；L1/provided 改读快照，AI/搜索/导出须走同一快照查询；
- 影子对比 L1 命中率与旧链路一致后，`021` 删除 `sla_definition_customer_read`；
- `sla_definition_grants` 保留为 provider 侧授权记录；`sla_grant_customer_read` 作为授权元数据 allowlist，后续确认无直接查询后再评估删除；
- 验收：customer 不再读 provider 的 `sla_definitions` 业务行，Q-09 关闭。

## 4. 风险登记

| 风险 | 等级 | 缓解 |
|---|---|---|
| 新增部分唯一索引在存量脏数据上创建失败 | 中 | 迁移前先跑「潜在冲突清单」查询，人工处理后再建索引 |
| 主档字段膨胀导致列表查询变慢 | 中 | 列表只 `SELECT` 展示列，详情单独拉取；必要字段走覆盖索引 |
| 合并误操作导致数据错乱 | 高 | 二次确认 + 影响面预览（工单/会话/合同/项目条数）+ 审计 + 7 天回滚窗口 |
| 客户关系形成环（A→B→A） | 中 | 写入时做环检测（深度优先，限深 5），拒绝成环 |
| RLS 漏登记导致跨租户可见 | **高** | 新表全部入 `rls-apply` 清单，CI 增加「新表未登记即失败」检查 |
| 同一租户行引用另一租户的 parent id | **高** | 父表 `(tenant_id,id)` 唯一索引 + 子表复合外键；服务层统一 validator |
| 合同金额/数量/预占/实耗混算 | **高** | 金额与数量分列；`billing` 行承载预占；`contract_usage_ledger` 承载实耗；M-1/M-2 用 `effective_amount` |
| SLA 定义快照漂移或 L1 命中率下降 | 高 | `020` 双写 + 影子对比；`source_version`/`payload_hash` 幂等；`021` 只在观察通过后执行 |

## 5. 验收清单（Definition of Done）

1. 客户列表 / 详情 / 新增 / 编辑在新字段下正常工作，中英文与暗色主题无布局破版；
2. 同一客户可挂 ≥2 个租户，且租户映射可标记结算锚点；
3. 客户关系可表达「集团 - 子公司」「伙伴」「前身」三类，树上无环；
4. 项目详情可见所属客户，客户详情可见关联项目；
5. 合并后：被合并档案只读、关联全部重指向、审计日志完整、可回滚；
6. 客户 A 租户的用户无法读取客户 B 租户的任何主数据（含联系人与关系）；
7. 邮件报障原有匹配用例 100% 通过；
8. 同租户 `customer_id` / `branch_id` / `contract_id` / `project_id` 引用由复合外键兜住；
9. 合同预占与实耗分离，`contract_usage_ledger` 幂等键生效，M-1/M-2 按 `effective_amount` 校验；
10. `018/019` 合同快照与 `020/021` SLA 定义快照全部通过观察；BD-1 审计为 0 行，Q-09 已关闭。
