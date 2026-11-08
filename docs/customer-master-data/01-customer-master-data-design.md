# 01 · 客户主数据设计方案（主文档）

> 状态：Draft v1.1 · 2026-11-08
> 目标读者：产品、架构、后端、前端、交付
> 前置阅读：[README](./README.md)、[02 DDL 草案](./02-ddl-and-migration.md)、[03 待裁决问题](./03-open-questions.md)
> 事实基线：`itsm-backend/ent/schema/service_customer.go`、`customer_branch.go`、`support_contract.go`、`source_organization.go`、`email_conversation.go`、`tenant.go`、`msp_allocation.go`、`user_tenant_membership.go`、`project.go`、`project_member.go`；`itsm-backend/migrations/20260824_email_intake_poc.sql`；`docs/multi-tenant/03-customer-dimension.md`；`docs/project-management/02-data-model.md`

---

## 1. 定位与设计原则

### 1.1 三个概念必须先分清

| 概念 | 实体 | 语义 | 隔离作用 |
|---|---|---|---|
| **租户（Tenant）** | `tenants` | 隔离边界：用户、角色、菜单、工单、CMDB、流程全部落在租户内 | 是隔离边界本身 |
| **客户主档（Customer Master）** | `service_customers` | 一个「被服务方」的业务档案：谁、什么等级、谁负责、走什么 SLA、结算给谁 | 档案归属某租户，但**不承担隔离职责** |
| **客户租户（Customer Tenant）** | `tenants.type='msp_customer'` | 某个客户实际使用的租户实例 | 客户用户只能进自己的租户 |

> 现有文档已明确「客户档案与租户是两回事，且不得用档案替代租户隔离」（`docs/multi-tenant/03-customer-dimension.md` §7）。本设计**不改变**这个前提，只把档案做得更完整。

### 1.2 六条设计原则

| 编号 | 原则 | 落地要求 |
|---|---|---|
| P-01 | **档案永远归属租户** | 所有客户主数据表首列语义为 `tenant_id`，RLS 全覆盖；不引入全局客户池 |
| P-02 | **跨租户只弱引用** | `linked_tenant_id`、`project_id`、`sla_definition_id` 存 ID 不建外键，由服务层校验 + 授权 |
| P-03 | **主档不重复租户配置** | 套餐、配额、计费开关、到期时间**只存在 `tenants`**，客户主档最多缓存只读快照用于展示 |
| P-04 | **名字不是身份** | 识别靠 `code` / `tax_id` / `email_domains`，`name` / `aliases` 只参与模糊匹配 |
| P-05 | **不删除，只标记** | 停用 `status`、软删 `deleted_at`、合并 `merged_into_id`；历史工单与会话必须可回查 |
| P-06 | **编码即契约** | `code` 租户内唯一、创建后不可改，作为对外集成与对账的稳定句柄 |

### 1.3 与现有能力的兼容承诺

| 现有能力 | 本设计影响 |
|---|---|
| 邮件报障按「客户 / 分支 / 合同」匹配 | **零破坏**。匹配入参、匹配结果字段不变，只在匹配链路前增加「域名/税号」两级优先判定 |
| `(tenant_id, normalized_name)` 唯一键 | 保留。新增 `code`、`tax_id` 唯一键与之并行 |
| `aliases` / `historical_names` | 保留，继续用于模糊召回；合并时自动追加到 `historical_names` |
| `linked_customer_tenant_id` | 降级为兼容只读列，写入改走 `customer_tenant_links` |
| 项目 D-01「不设客户字段」 | 遵守。客户关联落在独立链接表，不改 `projects` 表结构 |

---

## 2. 现状盘点与差距

### 2.1 现状实体与真实字段

| 实体 | 字段（实测） | 缺口 |
|---|---|---|
| `service_customers` | `id`、`tenant_id`、`name`、`normalized_name`、`short_name`、`aliases`、`historical_names`、`status`、`linked_customer_tenant_id`、`created_at`、`updated_at` | 无编码、无税号、无行业、无分级、无负责人、无 SLA、无结算、无生命周期、无软删 |
| `customer_branches` | `id`、`tenant_id`、`customer_id`、`name`、`normalized_name`、`aliases`、`status` | 无地址、无时区、无服务点类型、无主点标记、无独立 SLA |
| `support_contracts` | `id`、`tenant_id`、`customer_id`、`branch_id`、`contract_number`、`normalized_contract_number`、`status`、`start_at`、`end_at`、`source_document_id` | 无合同类型、无金额、无 SLA 绑定、无续签链 |
| `source_organizations` | `id`、`tenant_id`、`name`、`normalized_name`、`email_addresses`、`email_domains`、`status` | 仅邮件来源，未与客户建立弱关联 |
| `email_conversations` | `source_organization_id`、`customer_id`、`branch_id`、`support_contract_id`、`confidence` | 匹配链路已有四路绑定，可作为客户活跃度指标来源 |

### 2.2 五个结构性差距

1. **一档一租户**：`linked_customer_tenant_id` 是单值，集团/多环境场景无法表达。
2. **无客户关系**：无法表达集团-子公司、渠道、前身；`historical_names` 只能存字符串。
3. **无项目通道**：项目侧按 D-01 不设客户字段，客户与交付项目之间缺一条合法连接。
4. **无联系人**：客户侧人员只能进 `users`，而 `users.tenant_id` 必填 + 占用配额，不适配「只是被通知的人」。
5. **无治理闭环**：没有合并、没有质量分、没有外部 ID 对账，脏数据只能靠人工改名。

### 2.3 前端现状（实测页面 `/email-intake/customers`）

| 项 | 现状 |
|---|---|
| 列表列 | `ID` / 客户名称 / 简称 / 别名 / 分支数 / 状态 / 操作 |
| 新增表单 | 仅 3 个字段：客户名称（必填）、简称、别名（逗号分隔） |
| 行操作 | 编辑、停用；展开行显示分支 |
| 分页 | 有（当前数据仅 1 条） |

> 结论：列表与表单都需要重新分组，否则 40+ 新字段无法落位。UI 方案见 §13。

---

## 3. 实体分层模型

### 3.1 五层结构

```
┌─────────────────────────────────────────────────────────────┐
│ L0 平台层：平台级共享（标签云、市场模板、AI Prompt 模板）        │
│     —— 客户主数据不进入本层                                    │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│ L1 租户层：tenants（internal | saas_customer | msp_provider | │
│            msp_customer）+ msp_provider_id 归属              │
└─────────────────────────────────────────────────────────────┘
        ▲ 弱引用（customer_tenant_links）
        │
┌─────────────────────────────────────────────────────────────┐
│ L2 客户主数据层（本设计重点，全部 tenant_id 归属）              │
│  service_customers  ←─ 主档（唯一事实源）                      │
│    ├── customer_branches      服务点 / 地点                   │
│    ├── customer_contacts      联系人（≠ 用户）                 │
│    ├── customer_relationships 客户关系网（集团/伙伴/前身）      │
│    ├── customer_tenant_links  客户 ↔ 租户（1:N）              │
│    ├── customer_external_ids  外部系统标识（对账）             │
│    └── support_contracts      支持合同                        │
└─────────────────────────────────────────────────────────────┘
        ▲ 弱引用（project_customer_links，同租户）
        │
┌─────────────────────────────────────────────────────────────┐
│ L3 业务事实层：projects / tickets / incidents / changes /     │
│    service_requests / assets / CIs / SLA                      │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│ L4 接入层：email_conversations / source_organizations         │
│     —— 邮件、API、表单等 intake 通道，产出「候选客户」         │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 实体职责边界（一句话版）

| 实体 | 一句话职责 | 不负责 |
|---|---|---|
| `service_customers` | 客户是谁、什么等级、谁负责、走什么服务标准、怎么结算 | 不负责隔离、不存套餐配额 |
| `customer_branches` | 客户在哪儿有服务点，每个点各自的服务标准 | 不承担客户身份识别 |
| `customer_contacts` | 找谁、按什么角色联系、是否同意接收通知 | 不给登录能力（除非 `linked_user_id`） |
| `customer_relationships` | 客户与客户之间是什么关系、从什么时候开始 | 不表达数据权限继承（v1 不做集团穿透授权） |
| `customer_tenant_links` | 这条客户档案对应哪几个租户实例 | 不改租户 `type` / `status` / `quota` |
| `project_customer_links` | 哪个项目在为哪个客户交付 | 不改 `projects` 表、不做跨租户项目 |
| `support_contracts` | 按合同号维度的服务有效期 | 不做财务应收（走计费/ERP） |
| `customer_external_ids` | 与 ERP/CRM/OA 的对账键 | 不做实时同步（只存映射） |

### 3.3 关系总览（ER 文字版）

```
tenants 1 ──── N customer_tenant_links N ──── 1 service_customers
service_customers 1 ──── N customer_branches
service_customers 1 ──── N customer_contacts
service_customers 1 ──── N customer_relationships (self, from/to)
service_customers 1 ──── N support_contracts
service_customers 1 ──── N customer_external_ids
service_customers 1 ──── N project_customer_links N ──── 1 projects
customer_branches 1 ──── N support_contracts
customer_branches 1 ──── N customer_contacts
support_contracts 1 ──── N email_conversations
service_customers 1 ──── N email_conversations
```

> 所有 `N ──── 1` 的右侧实体若跨表类型不同租户，一律**弱引用 + 服务层校验**；`service_customers` 内部的 `REFERENCES` 保持真实外键（同租户内安全）。

---

## 4. 字段字典：主档八组字段

> 命名与类型遵循 [02 DDL 草案](./02-ddl-and-migration.md)；`*` 表示 P0 必做。

### 4.1 标识域（Identity）

| 字段 | 类型 | 必填 | 说明 | 唯一性 |
|---|---|---|---|---|
| `code`* | varchar(64) | 建议 | 客户编码，建议 `CUS-0001`，创建后不可改 | 租户内唯一（存活行） |
| `name`* | varchar(255) | 是 | 客户名称（沿用现有） | `(tenant_id, normalized_name)` 唯一 |
| `normalized_name`* | varchar(255) | 系统 | 名称归一（去空格/全半角/法律后缀） | — |
| `short_name` | varchar(120) | — | 简称（沿用） | — |
| `legal_name`* | varchar(255) | 建议 | 法定全称（合同、发票用） | — |
| `en_name` | varchar(255) | — | 英文名（涉外客户） | — |
| `aliases` | jsonb | — | 别名数组（沿用，供模糊召回） | — |
| `historical_names` | jsonb | — | 历史名（改名/合并自动追加） | — |
| `tax_id` | varchar(64) | 建议 | 统一社会信用代码 / 税号 | — |
| `normalized_tax_id` | varchar(64) | 系统 | 去分隔符大写 | 租户内唯一（非空） |
| `duns_number` | varchar(32) | — | 邓白氏编码（外企/跨国） | — |
| `registration_no` | varchar(64) | — | 工商注册号（旧版） | — |
| `email_domains`* | jsonb | 建议 | 主域名数组，AI 匹配首选特征 | 建议租户内域名唯一（软校验） |

**规则**
- `code` 生成：`CUS-` + 4 位以上自增，租户内独立计数；
- 改名时自动 `historical_names` 追加旧名，`normalized_name` 重算；
- `email_domains` 不建硬唯一索引（同一集团多品牌可共用域名），但保存时给出冲突提示。

### 4.2 分类域（Classification）

| 字段 | 类型 | 取值 | 说明 |
|---|---|---|---|
| `customer_type`* | varchar(32) | `direct` / `group` / `subsidiary` / `msp_endcustomer` / `internal` | 直客 / 集团总部 / 子公司 / MSP 最终客户 / 内部客户 |
| `industry` | varchar(64) | 行业字典 | 建议对齐国标行业门类 + 项目内既有行业词表 |
| `sub_industry` | varchar(64) | — | 细分行业（零售、制造、金融-证券…） |
| `size_class` | varchar(32) | `tiny`/`small`/`medium`/`large`/`enterprise` | 按员工数或营收分档 |
| `ownership_type` | varchar(32) | `soe`/`private`/`foreign`/`listed`/`government`/`ngo` | 所有制性质 |
| `tags` | 复用 `tag` 表 | — | 不新增列，走既有标签体系（`tenant_id` 内） |

**规则**：`customer_type='group'` 的客户才能作为 `customer_relationships.relation_type='parent'` 的父节点（软校验，允许例外并告警）。

### 4.3 分级域（Segmentation）

| 字段 | 类型 | 取值 | 说明 |
|---|---|---|---|
| `tier`* | varchar(16) | `S`/`A`/`B`/`C` | 服务等级分层，默认 `C` |
| `vip_level` | smallint | 0–5 | 同 tier 内的精细优先级，0 为普通 |
| `credit_rating` | varchar(16) | 自由词表 | 信用评级（外部输入，不做计算） |
| `strategic_flag` | 由 tag 表达 | — | 战略客户用标签，不新增列（避免列膨胀） |

**分级影响面（建议）**

| 影响点 | S | A | B | C |
|---|---|---|---|---|
| 默认响应时限 | 15min | 30min | 2h | 4h |
| 是否进 NOC 重点看板 | 是 | 是 | 否 | 否 |
| 客户经理必填 | 是 | 是 | 建议 | 否 |
| 季度回访任务 | 强制 | 强制 | 建议 | 否 |

> 说明：上表是**建议默认值**，实际 SLA 以 `sla_definition_id` 绑定的定义为准，`tier` 只作为未绑定时的话术与看板分组依据。

### 4.4 联系域（Contact & Reachability）

| 字段 | 类型 | 说明 |
|---|---|---|
| `website` | varchar(255) | 官网 |
| `main_phone` | varchar(64) | 总机 |
| `service_email` | varchar(320) | 服务受理邮箱（如 `support@`，与邮件报障联动） |
| `address` | jsonb | 注册地址 `{country,province,city,district,street,postalCode}` |
| `timezone` | varchar(64) | 客户主时区，默认继承租户 |
| `language` | varchar(16) | 通知语言，默认 `zh-CN` |

> 具体「人」的字段全部落到 `customer_contacts`（见 §5.2），主档只保留可达性入口。

### 4.5 服务域（Service Commitment）

| 字段 | 类型 | 说明 |
|---|---|---|
| `service_tier` | varchar(32) | 服务档位（基础/标准/尊享…），与 `tier` 区分：`tier` 是客户分层，`service_tier` 是买的服务包 |
| `sla_definition_id`* | bigint | 弱引用 `sladefinitions.id`（同租户），客户级默认 SLA |
| `support_hours` | varchar(64) | `7x24` / `5x8` / `custom` |
| `coverage_scope` | jsonb | 服务边界声明 `{inScope,outOfScope,boundaryNote,contactForBoundary}`，多服务商场景必填（见 §6.6） |
| `account_manager_id`* | bigint | 客户经理（`users.id`，须同租户活跃 membership） |
| `service_owner_id` | bigint | 服务负责人（交付侧） |
| `cs_manager_id` | bigint | 客户成功经理 |
| `oncall_group_id` | bigint | 默认值班组（弱引用 `groups.id`） |

**规则**：`account_manager_id` / `service_owner_id` / `cs_manager_id` 写入时按 `projects.manager_id` 同款口径校验（同租户、未软删、`status='active'`、未过期），校验失败 fail-closed 返回 422。

### 4.6 财务域（Billing）

| 字段 | 类型 | 说明 |
|---|---|---|
| `billing_currency` | varchar(8) | 结算币种，空 = 租户默认 |
| `payment_terms` | varchar(32) | `prepaid` / `net30` / `net60` / `net90` |
| `credit_limit` | numeric(18,2) | 信用额度（展示 + 预警，不做扣减） |
| `invoice_title` | varchar(255) | 开票抬头 |
| `billing_address` | jsonb | 开票地址 |
| `tax_id` | 见标识域 | 开票税号 |

> 明确的**非目标**：不做应收应付、不做对账单生成、不做税率计算。这些属于财务系统。注意：`contracts` 只有 `value`（待迁 `NUMERIC`），`msp_contracts` 只有 `currency`、没有金额；客户支持合同的金额由 [06](./06-contract-modeling-design.md) 的 `support_contracts.amount` 补齐，定位为“授权额度”，不是应收。

### 4.7 生命周期域（Lifecycle）

| 字段 | 类型 | 取值 | 说明 |
|---|---|---|---|
| `status`* | varchar(32) | `active` / `inactive` / `suspended` | 沿用现有，控制是否参与匹配 |
| `lifecycle_stage`* | varchar(32) | `prospect` / `onboarding` / `active` / `renewal` / `churned` / `archived` | 运营阶段 |
| `onboarded_at` | timestamptz | — | 完成开通时间 |
| `contract_start_at` | timestamptz | — | 当前合同起始（由 `support_contracts.effective_from` 派生，只读） |
| `contract_end_at` | timestamptz | — | 当前合同到期（由 `support_contracts.effective_until` 派生，只读，用于续约提醒） |
| `churn_at` | timestamptz | — | 流失时间 |
| `churn_reason` | varchar(255) | — | 流失原因 |

**状态机（`lifecycle_stage`）**

```
prospect ──开通──> onboarding ──readiness 通过──> active
                     │                              │
                      └────失败/放弃────> archived     ├──进入续约提前期──> renewal ──续签──> active
                                                    └──退租/终止────> churned ──归档──> archived
archived / churned ──重启合作──> onboarding
```

> 续约提前期不是固定 90 天，按客户等级 S 90 / A 60 / B·C 30 天（与 [06 §4.2](./06-contract-modeling-design.md) 一致）。

**与租户状态的关系（重要）**

| 客户 `lifecycle_stage` | 关联租户 `status` | 是否允许不一致 |
|---|---|---|
| `onboarding` | `active`（已建但未开通） | 是，正常中间态 |
| `active` | `active` | — |
| `churned` | `suspended` / `deleted` | 是，但需提示「客户已流失而租户仍活跃」 |
| `archived` | `deleted` | 是 |

> 一致性巡检：每日比对 `lifecycle_stage` 与 `customer_tenant_links.provision_status` + `tenants.status`，不一致写入运营待办，**不自动改状态**（改状态涉及权限与计费，必须人工确认）。

### 4.8 治理域（Governance）

| 字段 | 类型 | 说明 |
|---|---|---|
| `data_source` | varchar(32) | `manual` / `email_intake` / `import` / `crm_sync` / `ai_extract` |
| `data_quality_score` | smallint | 0–100，见 §10.3 |
| `merged_into_id` | bigint | 合并去向；非空即只读 |
| `version` | bigint | 乐观锁（对齐 `projects.version`） |
| `deleted_at` | timestamptz | 软删 |
| `created_by` / `updated_by` | bigint | 操作人 |

---

## 5. 关联模型设计

### 5.1 客户 ↔ 客户：`customer_relationships`

**关系类型词表**

| `relation_type` | 语义 | 反向自动生成 | 示例 |
|---|---|---|---|
| `parent` | A 是 B 的母公司 | `subsidiary` | 集团总部 → 区域公司 |
| `subsidiary` | A 是 B 的子公司 | `parent` | 反向记录 |
| `partner` | 生态伙伴 / 联合交付 | 双向 | 集成商 ↔ 硬件商 |
| `reseller` | A 是 B 的经销商 | 无（`is_bidirectional=false`） | 渠道 |
| `vendor` | A 是 B 的供应商 | 无 | 外包供应商 |
| `competitor` | 竞争关系 | 双向 | 用于冲突提示 |
| `successor` | A 承继 B 的业务（并购/重组） | 无 | 合并后主体 |
| `former_name_of` | A 是 B 的曾用名主体（组织变更） | 无 | 更名但法人不同 |

**关键规则**
1. **禁止自环**：`from_customer_id <> to_customer_id`（数据库 CHECK）。
2. **禁止成环**：`parent` / `subsidiary` 关系写入前做深度优先环检测，最大深度 5，成环返回 409。
3. **反向冗余**：写 `parent` 时同事务写一条 `subsidiary`（`is_bidirectional=true`），保证双向查询单表可解。
4. **时间维度**：`effective_from` / `effective_to` + `status`，支持历史集团结构调整。
5. **不做权限继承（v1）**：集团总部用户**不会**自动看到子公司数据；需要时通过租户 + membership 授权解决。

**典型查询**

```sql
-- 某客户向上找集团总部（递归 CTE，限深 5）
WITH RECURSIVE up AS (
  SELECT r.from_customer_id AS id, 1 AS depth
  FROM customer_relationships r
  WHERE r.tenant_id = $1 AND r.to_customer_id = $2
    AND r.relation_type = 'parent' AND r.status = 'active' AND r.deleted_at IS NULL
  UNION ALL
  SELECT r.from_customer_id, up.depth + 1
  FROM customer_relationships r
  JOIN up ON r.to_customer_id = up.id
  WHERE r.tenant_id = $1 AND r.relation_type = 'parent'
    AND r.status = 'active' AND r.deleted_at IS NULL AND up.depth < 5
)
SELECT * FROM up ORDER BY depth;
```

### 5.2 客户 ↔ 人：`customer_contacts`

**为什么独立成表而不是建用户**

| 维度 | 建 `users` | 独立 `customer_contacts` |
|---|---|---|
| `users.tenant_id` 必填 | 被迫塞进客户租户 | 不受限 |
| 租户配额 `maxUsers` | 占用配额 | 不占 |
| 登录能力 | 自动获得账号语义 | 默认无，需要时才 `linked_user_id` |
| 联系人数量 | 通常 1–3 人 | 可达数十人（多角色多分支） |
| 离职处理 | 需停用账号 | 只标 `inactive` |

**角色词表**：`technical`（技术对接）/ `business`（业务对接）/ `billing`（财务）/ `escalation`（升级投诉）/ `noc`（值班/监控接收）/ `executive`（高管）。

**规则**
1. 每个 `contact_role` 下最多一个 `is_primary`（部分唯一索引）；
2. `normalized_email` 在同一客户内唯一（防重复录入）；
3. `opt_in=false` 的联系人**不进入**任何自动通知收件人列表（合规章节）；
4. 联系人可归属到 `branch_id`，为空表示客户级联系；
5. 转化路径：联系人 → 邀请成为客户租户用户时，创建邀请并回填 `linked_user_id`，**不复制**人员字段。

### 5.3 客户 ↔ 租户：`customer_tenant_links`

**为什么必须 1:N**

| 场景 | 单值字段能否表达 |
|---|---|
| 集团一个主档，下有 3 个法人各自独立租户 | ❌ |
| 同一客户：生产租户 + 测试租户 + 归档租户 | ❌ |
| 客户从旧租户迁移到新租户（并存过渡期） | ❌ |
| 区域子公司各自租户，集团统一结算 | ❌（还需 `is_billing_anchor`） |

**字段语义**

| 字段 | 语义 |
|---|---|
| `tenant_id` | 档案所属租户（**数据归属**，RLS 作用域） |
| `linked_tenant_id` | 指向的租户实例（**弱引用** `tenants.id`） |
| `link_type` | `primary` / `secondary` / `sandbox` / `archive` / `regional` |
| `is_billing_anchor` | 该客户在此租户上做结算锚点（每客户最多 1 个） |
| `provision_status` | `pending` / `provisioning` / `ready` / `failed`，与 `POST /tenants/:id/provision` 联动 |

**规则**
1. `link_type='primary'` 每客户最多一个（部分唯一索引）；
2. 同一 `tenant_id` 下，一个 `linked_tenant_id` 只能对应一条存活主档映射（反向唯一索引 `uq_ctlink_linked_tenant`）；
3. 写入时校验 `linked_tenant_id` 存在，且若当前档案所属租户是 `msp_provider`，则目标租户必须 `type='msp_customer'` 且 `msp_provider_id` 等于当前租户（对齐 IP-P0-4 归属唯一写通道）；
4. **不修改** `tenants` 的任何字段（套餐、配额、状态仍由租户管理页负责）；
5. `provision_status` 由租户开通接口回写，本表只读展示。

### 5.4 客户 ↔ 项目：`project_customer_links`

> **合同正查缺口**：本表只负责「项目 ↔ 客户」，**不承载 `contract_id`**，因此不能回答「这份合同下有哪些项目」。合同 ↔ 项目的 M:N 闭环由 `contract_project_links` 补齐，详见 [05-customer-contract-project-linkage.md](./05-customer-contract-project-linkage.md)。

**为什么不在 `projects` 加列**

项目管理 D-01 明确「不建任何指向他租户的字段、外键或关联表」，且 `docs/project-management/02-data-model.md` §2 已显式标注 `~~customer_tenant_id~~` **明确不做**。若在 `projects` 加 `customer_id`：

- 服务商租户的项目要指向客户租户的档案 → 跨租户外键，直接违规；
- 客户档案合并时需要改 `projects` 主表，影响面大；
- 一个项目服务多个客户时无法表达。

因此采用**独立链接表 + 同租户约束**：

| 字段 | 语义 |
|---|---|
| `tenant_id` | 项目所属租户（RLS 作用域，与 `projects` 一致） |
| `project_id` | `projects.id`（同租户，复合外键 `(tenant_id, project_id)`） |
| `customer_id` | 必须是**同租户**的 `service_customers.id`（复合外键） |
| `branch_id` | 可选，指定服务点；必须属于同一客户（三列复合外键） |
| `relation_type` | `presales`（售前）/ `delivery`（交付）/ `implementation`（实施）/ `support`（运维）/ `renewal`（续约）/ `escalation`（升级专项） |
| `is_primary` | 主客户（每项目最多 1 个） |

> 本表**不再保留 `contract_id`**；合同与项目的多对多关系统一走 `contract_project_links`，避免同一业务事实出现两个来源。

**跨租户怎么办（MSP 场景）**

```
服务商租户(provider)                    客户租户(customer)
   projects(交付项目)         ✗ 不直连
   project_customer_links ── 同租户客户档案（服务商侧维护的客户主档）
                                   │ customer_tenant_links（弱引用）
                                   ▼
                              tenants(msp_customer)
```

即：服务商在自己的租户里维护一份客户主档（`customer_type='msp_endcustomer'`），用 `customer_tenant_links` 指向客户租户；项目通过 `project_customer_links` 关联服务商侧的这份主档。跨租户取数仍走 `MSPAllocation` 授权，**不建任何跨租户外键**。

### 5.5 客户 ↔ 合同：`support_contracts` 增强（P1）

> **合同实体最终字段与业务数据逻辑以 [06-contract-modeling-design.md](./06-contract-modeling-design.md) 为准**；02 §12.1 的过渡 DDL 已撤销；合同与项目的三方闭环见 [05-customer-contract-project-linkage.md](./05-customer-contract-project-linkage.md)。

| 新增字段 | 类型 | 说明 |
|---|---|---|
| `contract_type` | varchar(32) | `support` / `maintenance` / `subscription` / `project` / `framework` |
| `contract_name` | varchar(255) | 合同名称 |
| `amount` | numeric(18,2) | 合同授权总额（TCV），用于服务项汇总与项目分配上限；不是应收/开票金额 |
| `currency` | varchar(8) | 币种 |
| `effective_from` / `effective_until` | timestamptz | 统一有效期（半开区间；旧 `start_at/end_at` 只读） |
| `sla_definition_id` | bigint | 合同级 SLA（弱引用），优先级高于客户级 |
| `parent_contract_id` | bigint | 框架合同 → 子合同 |
| `renewal_of_contract_id` | bigint | 续签链 |
| `auto_renew` | boolean | 是否自动续约 |
| `terminated_at` / `termination_reason` | — | 提前终止 |
| `deleted_at` / `version` | — | 软删、乐观锁（CC-5/6） |

**SLA 优先级**（修订版，服务项级最高；与 [06 §6.2](./06-contract-modeling-design.md) 一致）

```
服务项级 sla_definition_id（contract_service_items）
  > 合同级 sla_definition_id（support_contracts）
    > 分支级 SLA（customer_branches）
      > 客户级 sla_definition_id（service_customers）
        > 租户默认 SLA
```

### 5.6 客户 ↔ 外部系统：`customer_external_ids`

用于与 ERP / CRM / OA / 外部工单系统对账：`(tenant_id, system_code, external_id)` 唯一。写入不校验外部系统真实存在，只保证映射唯一；对账任务定期比对差异并出报表。

---

## 6. 多租户场景设计

### 6.1 四个角色视角

| 角色 | 所在租户类型 | 能看到什么 | 能改什么 |
|---|---|---|---|
| 平台运营 | `internal` | 全部租户元信息（不含业务数据） | 租户开关、配额 |
| 服务商 MSP | `msp_provider` | 自有客户主档 + 被分配客户租户的业务数据 | 客户主档、分配关系 |
| 客户管理员 | `msp_customer` | 自己租户内一切 | 本租户内的客户档案（如自建子客户） |
| 客户最终用户 | `msp_customer` | 自己租户内被授权的部分 | 无 |

### 6.2 场景 A：单客户单租户（最常见）

```
tenants(id=10, type=msp_customer, msp_provider_id=3)
service_customers(id=100, tenant_id=3, customer_type='msp_endcustomer')
customer_tenant_links(tenant_id=3, customer_id=100, linked_tenant_id=10, link_type='primary', is_billing_anchor=true)
```

要点：服务商租户 3 里维护主档，指向客户租户 10。客户用户登录后**看不到**这条主档（它在租户 3），只看到自己租户内的业务数据。

### 6.3 场景 B：集团客户，一档多租户

```
service_customers(id=200, tenant_id=3, customer_type='group', name='XX控股')
  ├─ customer_relationships: 200 --parent--> 201（XX制造）
  ├─ customer_relationships: 200 --parent--> 202（XX零售）
  ├─ customer_tenant_links: 200 → 租户 30（集团总部，primary, billing_anchor）
  ├─ customer_tenant_links: 200 → 租户 31（制造子公司，regional）
  └─ customer_tenant_links: 200 → 租户 32（零售子公司，regional）
```

要点：
- 集团与子公司**各自**是独立客户档案（201、202），用 `customer_relationships` 连成树；
- 也可以只建集团一条主档 + 三条租户映射（当子公司不需要独立运营时）；
- 两种模式可混用，但**同一集团内应统一**，否则报表口径会分裂（建议在 `03-open-questions.md` Q-05 明确）。

### 6.4 场景 C：同一客户多环境租户

```
customer_tenant_links: 客户 100 → 租户 10  (primary, 生产)
customer_tenant_links: 客户 100 → 租户 11  (sandbox, 测试)
customer_tenant_links: 客户 100 → 租户 12  (archive, 归档)
```

要点：`is_billing_anchor` 只设在生产租户；`link_type` 区分用途；退租时只改 `archive` 那条的状态。

### 6.5 场景 D：服务商内部客户（internal）

`customer_type='internal'`，`tenant_id` 为服务商自己的租户，不建 `customer_tenant_links`（或指向自身租户）。用于内部部门提报工单的场景，避免内部需求污染外部客户指标。

### 6.6 场景 E：多服务商（一客户多 provider）

> 完整设计见 [04-multi-provider-scenario.md](./04-multi-provider-scenario.md)。此处只给结论。

前提（已裁决）：**客户不跨租户共享，所有数据都不跨租户。**

```
provider A 租户(id=3)                          provider B 租户(id=7)
  customer 100: tax_id=T, code=CUS-0100          customer 200: tax_id=T, code=BE-0042
  （A 自己维护的视图）                            （B 自己维护的视图）
        ╳ 互不可见（RLS）                              ╳
  客户租户(id=10) 只能挂 msp_provider_id = 3 或 7 之一
```

| 事实 | 结论 |
|---|---|
| 同一真实客户 N 个服务商服务 | 系统内存在 **N 条互不可见的独立主档**，这是设计选择 |
| 跨 provider 去重 / 联合 360 / 数据同步 | ❌ 一律不做 |
| 客户归属 | 只能一个 provider（`tenants.msp_provider_id` 单值，对齐 IP-P0-4） |
| 语义对齐方式 | 客户中性标识（`tax_id` / `email_domains`）+ `customer_external_ids` 字符串引用 |
| 服务商切换 | 走「客户授权 → 离线交接包 → 各自登记」，不是共享管道 |

**用法要点**
1. **服务边界**：主档新增 `coverage_scope`，声明"我负责哪一块"，解决多服务商推诿；交接包必带该字段。
2. **联系人各自维护**：A、B 认识的对接人本就不同，`customer_contacts` 天然适配，不跨租户发送通知。
3. **SLA / 合同完全独立**：只能报"本 provider 责任范围内"达成率，禁止合成"客户整体可用性"。
4. **项目关联不变**：`project_customer_links` 全程同租户，A 的项目只连 A 侧主档。
5. **交接留痕**：`customer_handover_packages` 记录导出范围、文件哈希、客户授权证据，仅存"这件事发生过"，不存数据本体。

### 6.7 隔离与权限矩阵

| 数据 | 作用域 | 读取校验 | 写入校验 |
|---|---|---|---|
| 客户主档 | `tenant_id` | RLS + `customer.read` | RLS + `customer.write` |
| 联系人 | `tenant_id` | 同上 + `opt_in` 过滤（通知场景） | `customer.write` |
| 客户关系 | `tenant_id` | 同上 | `customer.write` + 环检测 |
| 租户映射 | `tenant_id` | 同上 | `customer.write` + `tenant.read`（校验目标租户） |
| 项目链接 | 项目租户 | 同上 + 项目可见性 | `project.write` + `customer.read` |
| 合并 | `tenant_id` | — | `customer.merge` + 二次确认 |

**跨租户读取的唯一合法路径**：服务商用户持有 `MSPAllocation`（活跃），走既有「受控访问目标租户」中间件，读到的仍是目标租户内的数据，不是「跨租户 JOIN」。

> ⚠️ **受控访问 ≠ 跨租户读表**：`MSPAllocation` 只授予「进入目标租户」的资格，进入后读的仍是目标租户自己的行（RLS 谓词依然是 `tenant_id = ctx`）。任何通过 **RLS 放宽**（如 `msp_contracts` 曾有的 `customer_tenant_id = ctx` 只读策略）让一个租户直接读另一个租户业务行的做法，均违反「业务数据不跨租户」，处置方案见 [07-tenant-data-boundary.md](./07-tenant-data-boundary.md)。

### 6.8 与租户生命周期的联动

| 租户事件 | 对客户主数据的动作 |
|---|---|
| 创建租户 `type=msp_customer` | 提示「是否同时创建/关联客户主档」（不强制） |
| 开通 `provision` 成功 | 回写 `customer_tenant_links.provision_status='ready'`，若客户 `lifecycle_stage='onboarding'` 则提示可转 `active` |
| 租户 `suspended` | 客户主档**不变**，但详情页显示警示条 |
| 租户 `deleted`（退租） | `customer_tenant_links` 软删；`lifecycle_stage` 提示转 `churned` |

---

## 7. 客户 360 视图（P2）

一屏聚合，全部按 `tenant_id` 收窄、按权限裁剪：

| 区块 | 数据来源 | 关键指标 |
|---|---|---|
| 概览 | 主档 | 等级、生命周期、负责人、质量分 |
| 服务健康 | `slaviolations`、`slametric` | 近 30 天达标率、违约次数 |
| 工单 | `tickets`、`incidents` | 未结数、超期数、P1/P2 分布 |
| 合同 | `support_contracts` | 当前合同、剩余天数、续约提醒 |
| 项目 | `project_customer_links` | 在途项目、健康度红黄、里程碑风险 |
| 资产/CI | `assets`、`configurationitems` | 数量、关键系统清单 |
| 联系人 | `customer_contacts` | 主联系人、升级路径 |
| 关系 | `customer_relationships` | 集团树、伙伴 |
| 租户 | `customer_tenant_links` | 生产/测试/归档、开通状态 |

---

## 8. AI 与邮件报障集成

### 8.1 五级匹配链路（替换现有单级名字匹配）

| 级别 | 依据 | 置信度 | 命中即停 |
|---|---|---|---|
| L1 | 发件域名 ∈ `email_domains`（精确） | 0.95 | 是 |
| L2 | 联系人邮箱精确命中 `customer_contacts.normalized_email` | 0.95 | 是 |
| L3 | `normalized_tax_id` 在正文/签名中被提取 | 0.90 | 是 |
| L4 | `normalized_name` 精确 / 别名精确 | 0.85 | 是 |
| L5 | 别名/历史名模糊（编辑距离 ≤2 或包含关系） | 0.60–0.80 | 否，出候选列表 |

**规则**
- 置信度 < 0.85 时**不自动绑定** `customer_id`，写入 `email_conversations.missing_fields` 并进入人工确认队列；
- L5 命中多个候选时，全部写入 `email_conversations.canonical_data.candidates` 供人工选择；
- 所有匹配结果记录 `field_sources`（哪个字段、哪一级、什么依据），保持现有可解释性设计。

### 8.2 反向沉淀（自动建档）

当出现「域名不在任何客户上、但同一域名 30 天内 ≥3 次来信」时：

1. 生成**候选客户档案**草稿（`status='inactive'`、`lifecycle_stage='prospect'`、`data_source='email_intake'`）；
2. **不自动启用**，进入「待确认」列表，由运营一键采纳；采纳后再置 `status='active'`；
3. 采纳时自动补 `email_domains`、`main_phone`（从签名提取）、`service_email`。

> `status` 控制是否参与匹配（§4.7），候选档案必须保持 `inactive`；否则“待确认”草稿会被下一封邮件立即命中。这是防止「AI 自动建出一堆脏档案」的关键闸门，必须保留人工确认。

### 8.3 客户活跃度指标（供分级运营）

| 指标 | 口径 |
|---|---|
| 近 30 天来件数 | `email_conversations` 按 `customer_id` 计数 |
| 近 30 天工单数 | `tickets` 按客户关联计数 |
| 平均首次响应 | `tickets` 首次响应时间均值 |
| 趋势 | 环比上一 30 天 |

---

## 9. 合并与去重

### 9.1 候选识别（只提示，不自动合并）

| 规则 | 触发条件 | 处理 |
|---|---|---|
| R1 税号相同 | `normalized_tax_id` 相等（同租户） | 强提示 |
| R2 域名重叠 | `email_domains` 交集非空 | 中提示 |
| R3 名称近似 | 归一名称编辑距离 ≤2 | 弱提示 |
| R4 历史名互含 | A 的 `historical_names` 含 B 现名 | 中提示 |

> R1/R2 命中时**禁止**直接新建（前端提示「疑似重复，是否关联到已有客户」），但允许强制新建并记录理由。

### 9.2 合并流程（`POST /customers/:id/merge`）

```
1. 预览影响面：工单数 / 会话数 / 合同数 / 服务项 / 项目链接 / 分支数 / 联系人 / 租户映射 / 外部标识 / 交接包
2. 选择保留主档（target），确认被合并档（source）
3. 冲突处理：
   - 标量字段：逐字段选「保留 target / 取 source」
   - 联系人：全部迁移，同邮箱去重
   - 分支：迁移，重名时追加「(来自 source.name)」
   - 关系：迁移，成环则拒绝并列出冲突
   - 租户映射：primary / billing anchor 冲突时先由用户选定唯一锚点
   - 项目/合同链接：项目主客户、billing 行、合同客户冲突时拒绝并列出明细
4. 事务内执行：
   - 先迁移 `project_customer_links`，再迁移 `support_contracts.customer_id`（L-04 的合并例外），最后迁移联系人/分支/关系/租户映射/外部标识/交接包/会话
   - source.historical_names += [source.name, ...source.aliases]
   - source.merged_into_id = target.id；source.status='inactive'；source.deleted_at=now()
   - 写 auditlog（含 before/after 快照与操作人）
5. 返回合并报告（迁移条数、冲突项、耗时）
```

> 合并是 [05 L-04](./05-customer-contract-project-linkage.md) 的唯一例外，必须使用 `customer.merge` 权限并在同一事务内写审计；任何唯一键冲突都必须回滚，不允许部分合并。

**回滚**：`POST /customers/:id/merge/undo`（7 天内 + `customer.merge.undo` 权限），依据 auditlog 快照反向恢复。

### 9.3 数据质量分（0–100）

| 维度 | 权重 | 计分规则 |
|---|---|---|
| 标识完整 | 25 | 有 `code` +5，`legal_name` +5，`tax_id` +10，`email_domains` 非空 +5 |
| 分类完整 | 15 | `industry` +8，`size_class` +4，`ownership_type` +3 |
| 分级完整 | 10 | `tier` 非默认 +6，`vip_level>0` +4 |
| 服务配置 | 20 | `sla_definition_id` +8，`account_manager_id` +8，`support_hours` +4 |
| 联系可达 | 20 | 主联系人 +8，`main_phone` +4，`service_email` +4，地址 +4 |
| 关系完整 | 10 | 有租户映射 +6，有项目链接 +4 |

> 计算时机：写入后异步重算 + 每日全量校对；列表支持按分排序，运营可用「质量分 < 60」筛选待补全客户。

---

## 10. API 设计

### 10.1 主档

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/customers` | `customer.read` | 列表：`keyword`、`tier`、`status`、`lifecycleStage`、`accountManagerId`、`hasTenantLink`、`qualityBelow`、分页排序 |
| GET | `/api/v1/customers/:id` | `customer.read` | 详情（含分支、联系人、关联数） |
| GET | `/api/v1/customers/:id/360` | `customer.read` | 360 聚合（P2） |
| POST | `/api/v1/customers` | `customer.write` | 创建（重复检测：R1/R2 命中返回 409 + 候选） |
| PUT | `/api/v1/customers/:id` | `customer.write` | 更新（`version` 乐观锁，冲突 409） |
| PUT | `/api/v1/customers/:id/status` | `customer.write` | 启停 |
| PUT | `/api/v1/customers/:id/lifecycle` | `customer.write` | 阶段流转（校验状态机） |
| POST | `/api/v1/customers/:id/merge` | `customer.merge` | 合并 |
| POST | `/api/v1/customers/:id/merge/undo` | `customer.merge.undo` | 撤销合并 |
| GET | `/api/v1/customers/duplicates` | `customer.read` | 重复候选清单 |
| POST | `/api/v1/customers/import` | `customer.import` | 批量导入（预校验 + 差异报告） |

### 10.2 子资源

| 方法 | 路径 | 说明 |
|---|---|---|
| CRUD | `/api/v1/customers/:id/branches` | 服务点 |
| CRUD | `/api/v1/customers/:id/contacts` | 联系人 |
| CRUD | `/api/v1/customers/:id/relationships` | 客户关系（`direction=from/to`） |
| GET | `/api/v1/customers/:id/relations-tree` | 关系树（`depth` ≤5） |
| CRUD | `/api/v1/customers/:id/tenant-links` | 租户映射 |
| POST | `/api/v1/customers/:id/tenant-links/:linkId/provision` | 触发开通（代理租户开通接口） |
| CRUD | `/api/v1/customers/:id/external-ids` | 外部标识 |
| GET | `/api/v1/customers/:id/projects` | 关联项目 |
| POST | `/api/v1/projects/:id/customer-links` | 项目侧建链接 |

### 10.3 请求示例

```json
POST /api/v1/customers
{
  "name": "示例科技股份有限公司",
  "legalName": "示例科技股份有限公司",
  "shortName": "示例科技",
  "customerType": "direct",
  "taxId": "91310000MA1K3XXXXX",
  "industry": "制造业",
  "sizeClass": "large",
  "tier": "A",
  "emailDomains": ["example.com"],
  "mainPhone": "021-5000XXXX",
  "serviceEmail": "it-support@example.com",
  "timezone": "Asia/Shanghai",
  "slaDefinitionId": 12,
  "accountManagerId": 88,
  "billingCurrency": "CNY",
  "aliases": ["示例", "Example Tech"]
}
```

### 10.4 统一错误码

| 码 | HTTP | 场景 |
|---|---|---|
| `CUSTOMER_DUPLICATE_TAX_ID` | 409 | 税号重复（附候选） |
| `CUSTOMER_DUPLICATE_DOMAIN` | 409 | 域名冲突（附候选） |
| `CUSTOMER_VERSION_CONFLICT` | 409 | 乐观锁冲突 |
| `CUSTOMER_RELATION_CYCLE` | 409 | 关系成环 |
| `CUSTOMER_MERGED_READONLY` | 422 | 操作已合并档案 |
| `CUSTOMER_INVALID_MEMBERSHIP` | 422 | 负责人不是同租户活跃成员 |
| `CUSTOMER_TENANT_LINK_INVALID` | 422 | 目标租户类型/归属不匹配 |
| `CUSTOMER_STAGE_TRANSITION_INVALID` | 422 | 非法生命周期流转 |

---

## 11. 前端设计

### 11.1 列表页（改造现有 `/email-intake/customers`）

| 列 | 变化 |
|---|---|
| ID | 保留（内部 id） |
| 客户编码 | **新增**（`code`） |
| 客户名称 | 保留，副标题显示 `legal_name` |
| 简称 / 别名 | 合并为一列「别名」，超出省略 + tooltip |
| 等级 | **新增**（tier 徽标，S 红 / A 橙 / B 蓝 / C 灰） |
| 客户经理 | **新增**（头像 + 名字） |
| 关联租户 | **新增**（数量 + 展开看列表） |
| 分支数 | 保留 |
| 生命周期 | **新增**（stage 标签） |
| 质量分 | **新增**（进度环，<60 红） |
| 状态 | 保留 |
| 操作 | 编辑 / 停用 / **合并** / 更多 |

**筛选区**：关键字、等级、生命周期、客户经理、行业、是否已关联租户、质量分区间。

### 11.2 新增/编辑表单（分组 Tab）

| Tab | 字段 |
|---|---|
| 基本信息 | 名称*、法定全称、简称、英文名、编码、别名、客户类型* |
| 分类分级 | 行业、细分行业、规模、所有制、等级*、VIP、信用评级 |
| 联系方式 | 官网、总机、服务邮箱、主域名（多值）、地址、时区、语言 |
| 服务配置 | 服务档位、SLA 定义、服务时间、客户经理、服务负责人、客户成功 |
| 财务结算 | 币种、账期、信用额度、开票抬头、开票地址 |
| 生命周期 | 阶段*、开通时间、合同起止、流失原因 |

> 交互要求：必填项仅「名称 / 客户类型 / 等级 / 阶段」，其余可后补；保存后若质量分 < 60，右上角提示「档案待补全」并可跳转。

### 11.3 详情页布局

```
[头部] 名称 + 等级徽标 + 生命周期 + 质量分 + 操作(编辑/合并/停用)
[左栏] 基本信息卡 / 分类分级卡 / 服务配置卡 / 财务卡
[右栏] 关联租户(表格) / 联系人(表格) / 服务点(表格) / 客户关系(树) / 关联项目(表格)
[Tabs] 概览 | 工单 | 合同 | 项目 | 资产 | 变更历史
```

### 11.4 关系树交互

- 默认展开 2 层，节点点击右侧展示详情；
- 节点右键：设为父级 / 添加子公司 / 添加伙伴 / 解除关系；
- 成环时前端即时提示，禁止提交；
- 深度 > 5 时提示「已超出建议层级」。

---

## 12. 实施路线与工作量估算

| 阶段 | 交付物 | 估算 |
|---|---|---|
| P0 | DDL + ent schema + 主档 CRUD + 联系人 + 租户映射 + 列表/表单改造 | 后端 6 人日，前端 5 人日 |
| P1 | 关系表 + 项目链接 + 合并 + 质量分 + 关系树 UI | 后端 6 人日，前端 5 人日 |
| P2 | 外部 ID + AI 五级匹配 + 360 视图 | 后端 5 人日，前端 6 人日 |
| C1（与 P1 衔接） | 合同聚合根 + 服务项/变更单/用量台账 + `contract_project_links` | 见 [06 §9.1](./06-contract-modeling-design.md)，另行估算 |
| C2（与 P2 衔接） | `018/019` 快照下发 + G17 收回 + 结算快照 | 见 [07 §4.5](./07-tenant-data-boundary.md)，另行估算 |
| C3（与 C2 并行） | `020/021` SLA 定义快照 + 011 收回（Q-09 B） | 见 [07 §4.6](./07-tenant-data-boundary.md)，另行估算 |
| 贯穿 | 迁移脚本、RLS 登记、单测/E2E、文档 | 4 人日 |

**前置依赖**
- P0 需先确认 `03-open-questions.md` 的 Q-01 / Q-03 / Q-07（影响表结构）；
- P1 的合并功能需 auditlog 支持结构化快照（现有 `auditlog` 需确认字段容量）。
- C1 依赖 P1 的 `project_customer_links`；C2 依赖 C1 完成且 `018` 快照双写观察通过，禁止先执行 `019`。
- C3 依赖 L1 读取改造；`020` 快照双写观察通过后才能执行 `021`，禁止先 DROP。

---

## 13. 验收与风险

### 13.1 验收清单

1. 现有客户数据全部可读、可编辑，邮件报障匹配结果**无回归**；
2. 同一客户可挂 ≥2 个租户，`is_billing_anchor` 唯一生效；
3. 客户关系可表达 parent / partner / former_name_of，树上无环；
4. 项目详情可见所属客户，客户详情可见关联项目；
5. 合并后：被合并档案只读、关联全部重指向、审计完整、7 天内可撤销；
6. 客户 A 租户用户无法读取客户 B 租户的任何客户主数据（含联系人、关系、映射）；
7. 列表筛选「质量分 < 60」返回结果与后端计算一致；
8. 新增表单 6 个 Tab 在 1280 / 1440 / 1920 宽度下无破版，中英文切换正常；
9. 跨租户 `customer_id` / `branch_id` / `contract_id` / `project_id` 写入被复合外键拒绝；
10. 客户合并可在 L-04 例外路径完成，且所有唯一键冲突都能回滚。

### 13.2 主要风险

| 风险 | 缓解 |
|---|---|
| 字段过多导致表单不可用 | 分组 Tab + 必填极简 + 质量分驱动补全 |
| 合并误操作 | 影响面预览 + 二次确认 + 审计 + 撤销窗口 |
| RLS 漏登记 | 新表强制登记 + CI 检查 |
| 跨租户越权 | 同租户引用用复合外键，跨租户用弱引用 + 服务层校验 + 现有受控访问中间件 |
| 与租户配置双写不一致 | 原则 P-03：租户配置只在 `tenants`，主档只缓存只读快照 |
