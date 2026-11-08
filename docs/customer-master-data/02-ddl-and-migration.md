# 02 · DDL 草案与迁移方案

> 状态：Draft v1.1 · 2026-11-08
> 说明：本文件为**设计草案**，非最终迁移脚本。落库前需按 `docs/project-management/plan/p0-data-ddl.md` 同款评审流程确认，并同步 `cmd/rls-apply` 受管表清单与 `internal/schema/tenant_guard.go`。
> 命名口径：沿用现有 `service_customers` / `customer_branches` / `support_contracts` 风格（`tenant_id` 必须存在、`created_at` / `updated_at` 标配、`normalized_*` 用于唯一匹配）。

## 1. 主档扩展（ALTER，全部可空、向后兼容）

```sql
-- 客户主档：标识域 / 分类域 / 分级域 / 联系域 / 服务域 / 财务域 / 生命周期域 / 治理域
ALTER TABLE service_customers
  ADD COLUMN IF NOT EXISTS code                VARCHAR(64),      -- 客户编码（租户内唯一，建议 CUS-0001）
  ADD COLUMN IF NOT EXISTS legal_name          VARCHAR(255),     -- 法定全称
  ADD COLUMN IF NOT EXISTS en_name             VARCHAR(255),
  ADD COLUMN IF NOT EXISTS tax_id              VARCHAR(64),      -- 统一社会信用代码 / 税号
  ADD COLUMN IF NOT EXISTS normalized_tax_id   VARCHAR(64),
  ADD COLUMN IF NOT EXISTS duns_number         VARCHAR(32),
  ADD COLUMN IF NOT EXISTS registration_no     VARCHAR(64),
  ADD COLUMN IF NOT EXISTS customer_type       VARCHAR(32) NOT NULL DEFAULT 'direct',
      -- direct|group|subsidiary|msp_endcustomer|internal
  ADD COLUMN IF NOT EXISTS industry            VARCHAR(64),
  ADD COLUMN IF NOT EXISTS sub_industry        VARCHAR(64),
  ADD COLUMN IF NOT EXISTS size_class          VARCHAR(32),      -- tiny|small|medium|large|enterprise
  ADD COLUMN IF NOT EXISTS ownership_type      VARCHAR(32),      -- soe|private|foreign|listed|government|ngo
  ADD COLUMN IF NOT EXISTS tier                VARCHAR(16) NOT NULL DEFAULT 'C', -- S|A|B|C
  ADD COLUMN IF NOT EXISTS vip_level           SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS credit_rating       VARCHAR(16),
  ADD COLUMN IF NOT EXISTS email_domains       JSONB DEFAULT '[]',  -- 主域名（AI 匹配首选）
  ADD COLUMN IF NOT EXISTS website             VARCHAR(255),
  ADD COLUMN IF NOT EXISTS main_phone          VARCHAR(64),
  ADD COLUMN IF NOT EXISTS service_email       VARCHAR(320),
  ADD COLUMN IF NOT EXISTS address             JSONB DEFAULT '{}',
      -- {country,province,city,district,street,postalCode}
  ADD COLUMN IF NOT EXISTS timezone            VARCHAR(64),
  ADD COLUMN IF NOT EXISTS language            VARCHAR(16) DEFAULT 'zh-CN',
  ADD COLUMN IF NOT EXISTS service_tier        VARCHAR(32),
  ADD COLUMN IF NOT EXISTS sla_definition_id   BIGINT,           -- 弱引用 sladefinitions.id（同租户）
  ADD COLUMN IF NOT EXISTS support_hours       VARCHAR(64),       -- 7x24|5x8|custom
  ADD COLUMN IF NOT EXISTS coverage_scope      JSONB DEFAULT '{}',
      -- 服务边界（多服务商场景必需）：{inScope:[],outOfScope:[],boundaryNote:"",contactForBoundary:null}
  ADD COLUMN IF NOT EXISTS account_manager_id  BIGINT,           -- 客户经理（users.id，须活跃 membership）
  ADD COLUMN IF NOT EXISTS service_owner_id    BIGINT,           -- 服务负责人
  ADD COLUMN IF NOT EXISTS cs_manager_id       BIGINT,           -- 客户成功
  ADD COLUMN IF NOT EXISTS oncall_group_id     BIGINT,           -- 默认值班组（弱引用 groups.id，同租户）
  ADD COLUMN IF NOT EXISTS billing_currency    VARCHAR(8),
  ADD COLUMN IF NOT EXISTS payment_terms       VARCHAR(32),       -- net30|net60|prepaid|...
  ADD COLUMN IF NOT EXISTS credit_limit        NUMERIC(18,2),
  ADD COLUMN IF NOT EXISTS invoice_title       VARCHAR(255),
  ADD COLUMN IF NOT EXISTS billing_address     JSONB DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS lifecycle_stage     VARCHAR(32) NOT NULL DEFAULT 'prospect',
      -- prospect|onboarding|active|renewal|churned|archived
  ADD COLUMN IF NOT EXISTS onboarded_at        TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS contract_start_at   TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS contract_end_at     TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS churn_at            TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS churn_reason        VARCHAR(255),
  ADD COLUMN IF NOT EXISTS data_source         VARCHAR(32) DEFAULT 'manual',
      -- manual|email_intake|import|crm_sync|ai_extract
  ADD COLUMN IF NOT EXISTS data_quality_score  SMALLINT DEFAULT 0,
  ADD COLUMN IF NOT EXISTS merged_into_id      BIGINT,           -- 合并去向（只读标记，禁止再编辑）
  ADD COLUMN IF NOT EXISTS version             BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS deleted_at          TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS created_by          BIGINT,
  ADD COLUMN IF NOT EXISTS updated_by          BIGINT;

-- 唯一性：编码 + 税号（partial，避免空值互相冲突）
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_code
  ON service_customers (tenant_id, code)
  WHERE deleted_at IS NULL AND code IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_taxid
  ON service_customers (tenant_id, normalized_tax_id)
  WHERE deleted_at IS NULL AND normalized_tax_id IS NOT NULL AND normalized_tax_id <> '';

CREATE INDEX IF NOT EXISTS idx_customer_tenant_tier
  ON service_customers (tenant_id, tier, status);

CREATE INDEX IF NOT EXISTS idx_customer_tenant_stage
  ON service_customers (tenant_id, lifecycle_stage, status);

CREATE INDEX IF NOT EXISTS idx_customer_tenant_am
  ON service_customers (tenant_id, account_manager_id);
```

> **兼容性**：`(tenant_id, normalized_name)` 唯一键**保留不动**；`aliases` / `historical_names` 保留（供 AI 模糊匹配与历史检索）；新增字段全部可空；现有邮件报障匹配路径零改动。

## 2. 分支升级为「服务点 / 地点」

```sql
ALTER TABLE customer_branches
  ADD COLUMN IF NOT EXISTS code             VARCHAR(64),
  ADD COLUMN IF NOT EXISTS branch_type      VARCHAR(32) NOT NULL DEFAULT 'site',
      -- hq|site|datacenter|warehouse|store|subsidiary
  ADD COLUMN IF NOT EXISTS address          JSONB DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS timezone         VARCHAR(64),
  ADD COLUMN IF NOT EXISTS contact_phone    VARCHAR(64),
  ADD COLUMN IF NOT EXISTS email_domains    JSONB DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS service_tier     VARCHAR(32),
  ADD COLUMN IF NOT EXISTS support_hours    VARCHAR(64),
  ADD COLUMN IF NOT EXISTS is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS linked_tenant_id BIGINT,   -- 弱引用：该服务点对应的客户租户（可选）
  ADD COLUMN IF NOT EXISTS deleted_at       TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_code
  ON customer_branches (tenant_id, code)
  WHERE deleted_at IS NULL AND code IS NOT NULL;

-- 同一客户下最多一个主服务点
CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_primary
  ON customer_branches (tenant_id, customer_id)
  WHERE is_primary AND status = 'active' AND deleted_at IS NULL;

-- 软删/合并后允许复用名称：原 UNIQUE(tenant_id, normalized_name) 需改为 partial
-- 执行前确认实际约束名（不同环境可能由 ent 生成不同名称）。
ALTER TABLE service_customers
  DROP CONSTRAINT IF EXISTS service_customers_tenant_id_normalized_name_key;
DROP INDEX IF EXISTS service_customers_tenant_id_normalized_name;
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_name
  ON service_customers (tenant_id, normalized_name)
  WHERE deleted_at IS NULL;

ALTER TABLE customer_branches
  DROP CONSTRAINT IF EXISTS customer_branches_tenant_id_customer_id_normalized_name_key;
DROP INDEX IF EXISTS customer_branches_tenant_id_customer_id_normalized_name;
CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_name
  ON customer_branches (tenant_id, customer_id, normalized_name)
  WHERE deleted_at IS NULL;
```

## 3. 新增：客户联系人

```sql
CREATE TABLE IF NOT EXISTS customer_contacts (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  customer_id      BIGINT NOT NULL REFERENCES service_customers(id),
  branch_id        BIGINT REFERENCES customer_branches(id),
  name             VARCHAR(255) NOT NULL,
  name_en          VARCHAR(255),
  title            VARCHAR(128),
  department       VARCHAR(128),
  email            VARCHAR(320),
  normalized_email VARCHAR(320),
  mobile           VARCHAR(64),
  phone            VARCHAR(64),
  wechat           VARCHAR(128),
  contact_role     VARCHAR(32) NOT NULL DEFAULT 'business',
      -- technical|business|billing|escalation|noc|executive
  is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  language         VARCHAR(16),
  timezone         VARCHAR(64),
  opt_in           BOOLEAN NOT NULL DEFAULT TRUE,   -- 通知同意
  linked_user_id   BIGINT,                          -- 可空：转为系统用户后回填
  source           VARCHAR(32) NOT NULL DEFAULT 'manual',
      -- manual|email_intake|import|ai_extract
  status           VARCHAR(16) NOT NULL DEFAULT 'active',  -- active|inactive
  deleted_at       TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contact_email
  ON customer_contacts (tenant_id, customer_id, normalized_email)
  WHERE status = 'active' AND deleted_at IS NULL
    AND normalized_email IS NOT NULL AND normalized_email <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_contact_primary
  ON customer_contacts (tenant_id, customer_id, contact_role)
  WHERE is_primary AND status = 'active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_contact_tenant_customer
  ON customer_contacts (tenant_id, customer_id, status);

CREATE INDEX IF NOT EXISTS idx_contact_tenant_email
  ON customer_contacts (tenant_id, normalized_email);
```

## 4. 新增：客户关联关系（客户树 / 生态网）

```sql
CREATE TABLE IF NOT EXISTS customer_relationships (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  from_customer_id BIGINT NOT NULL REFERENCES service_customers(id),
  to_customer_id   BIGINT NOT NULL REFERENCES service_customers(id),
  relation_type    VARCHAR(32) NOT NULL,
      -- parent|subsidiary|partner|reseller|vendor|competitor|successor|former_name_of
  relation_role    VARCHAR(16) NOT NULL DEFAULT 'outbound', -- outbound|inbound（反向冗余，便于查询）
  is_bidirectional BOOLEAN NOT NULL DEFAULT FALSE,
  effective_from   DATE,
  effective_to     DATE,
  note             VARCHAR(500),
  status           VARCHAR(16) NOT NULL DEFAULT 'active',   -- active|ended
  deleted_at       TIMESTAMPTZ,
  created_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_rel_not_self CHECK (from_customer_id <> to_customer_id)
);

-- 同一对客户同一关系只允许一条存活记录
CREATE UNIQUE INDEX IF NOT EXISTS uq_rel_pair_type
  ON customer_relationships (tenant_id, from_customer_id, to_customer_id, relation_type)
  WHERE status = 'active' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_rel_tenant_from
  ON customer_relationships (tenant_id, from_customer_id, status);

CREATE INDEX IF NOT EXISTS idx_rel_tenant_to
  ON customer_relationships (tenant_id, to_customer_id, status);
```

## 5. 新增：客户 ↔ 租户映射（替代单值 `linked_customer_tenant_id`）

```sql
CREATE TABLE IF NOT EXISTS customer_tenant_links (
  id                BIGSERIAL PRIMARY KEY,
  tenant_id         BIGINT NOT NULL,   -- 档案所属租户（数据归属）
  customer_id       BIGINT NOT NULL REFERENCES service_customers(id),
  linked_tenant_id  BIGINT NOT NULL,   -- 指向 tenants.id（弱引用，不建外键）
  link_type         VARCHAR(24) NOT NULL DEFAULT 'primary',
      -- primary|secondary|sandbox|archive|regional
  is_billing_anchor BOOLEAN NOT NULL DEFAULT FALSE,
  provision_status  VARCHAR(24) NOT NULL DEFAULT 'pending',
      -- pending|provisioning|ready|failed
  provisioned_at    TIMESTAMPTZ,
  note              VARCHAR(500),
  deleted_at        TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink
  ON customer_tenant_links (tenant_id, customer_id, linked_tenant_id)
  WHERE deleted_at IS NULL;

-- 每客户最多一个 primary 映射；同一 provider 租户内一个客户租户只能对应一条主档
CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_primary
  ON customer_tenant_links (tenant_id, customer_id)
  WHERE link_type = 'primary' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_linked_tenant
  ON customer_tenant_links (tenant_id, linked_tenant_id)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_ctlink_billing
  ON customer_tenant_links (tenant_id, customer_id)
  WHERE is_billing_anchor AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ctlink_tenant_linked
  ON customer_tenant_links (tenant_id, linked_tenant_id);
```

## 6. 新增：客户 ↔ 项目链接（遵守 D-01）

```sql
-- 项目侧不新增 customer_tenant_id（D-01 / docs/project-management/02-data-model.md §2）。
-- 关联落在「项目所属租户」内的链接表，customer_id 必须是同租户客户档案。
-- 合同关系只走 contract_project_links；本表不再保留 contract_id，避免双写。
CREATE TABLE IF NOT EXISTS project_customer_links (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL,          -- 项目所属租户
  project_id    BIGINT NOT NULL,          -- projects.id（同租户，弱引用）
  customer_id   BIGINT NOT NULL REFERENCES service_customers(id),
  branch_id     BIGINT REFERENCES customer_branches(id),
  relation_type VARCHAR(24) NOT NULL DEFAULT 'delivery',
      -- presales|delivery|implementation|support|renewal|escalation
  is_primary    BOOLEAN NOT NULL DEFAULT FALSE,
  note          VARCHAR(500),
  deleted_at    TIMESTAMPTZ,
  created_by    BIGINT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_pclink
  ON project_customer_links (tenant_id, project_id, customer_id, relation_type)
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_pclink_primary
  ON project_customer_links (tenant_id, project_id)
  WHERE is_primary AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_pclink_tenant_customer
  ON project_customer_links (tenant_id, customer_id);
```

## 7. 新增：客户外部标识（多系统对账）

```sql
CREATE TABLE IF NOT EXISTS customer_external_ids (
  id           BIGSERIAL PRIMARY KEY,
  tenant_id    BIGINT NOT NULL,
  customer_id  BIGINT NOT NULL REFERENCES service_customers(id),
  system_code  VARCHAR(64) NOT NULL,   -- erp|crm|oa|salesforce|外部工单系统
  external_id  VARCHAR(160) NOT NULL,
  external_url VARCHAR(500),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, system_code, external_id)
);
```

### 7.1 新增：客户主数据交接包（多服务商）

> 业务说明见 [04-multi-provider-scenario.md](./04-multi-provider-scenario.md) §4.2。仅记录交接事件与导出快照哈希，不存客户数据本体。

```sql
CREATE TABLE IF NOT EXISTS customer_handover_packages (
  id                     BIGSERIAL PRIMARY KEY,
  tenant_id              BIGINT NOT NULL,          -- 发起方/接收方自己的租户
  customer_id            BIGINT NOT NULL REFERENCES service_customers(id),
  direction              VARCHAR(16) NOT NULL,      -- export|import
  counterpart_ref        VARCHAR(160),              -- 对方服务商标识（字符串弱引用）
  scope                  JSONB DEFAULT '{}',
  package_hash           VARCHAR(64),
  item_counts            JSONB DEFAULT '{}',
  status                 VARCHAR(24) NOT NULL DEFAULT 'draft',
      -- draft|authorized|exported|imported|revoked
  authorized_by          VARCHAR(255),
  authorized_at          TIMESTAMPTZ,
  authorization_evidence VARCHAR(500),
  created_by             BIGINT,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_handover_tenant_customer
  ON customer_handover_packages (tenant_id, customer_id, status);
```

### 7.2 归属一致性：复合唯一键与复合外键

> RLS 只保证「当前行属于当前租户」，**不保证外键指向的另一行属于同一租户**。下列复合约束必须与表和索引同批迁移；否则服务层一次 ID 传错，就会产生跨租户引用。

```sql
-- 父端唯一键（复合外键的被引用端）
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_tenant_id
  ON service_customers (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_id
  ON customer_branches (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_branch_tenant_customer_id
  ON customer_branches (tenant_id, customer_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_contract_tenant_id
  ON support_contracts (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_project_tenant_id
  ON projects (tenant_id, id);

-- 客户主数据内部
ALTER TABLE customer_contacts
  ADD CONSTRAINT fk_contact_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id);
ALTER TABLE customer_contacts
  ADD CONSTRAINT fk_contact_branch_tenant
  FOREIGN KEY (tenant_id, customer_id, branch_id)
  REFERENCES customer_branches (tenant_id, customer_id, id);
ALTER TABLE customer_relationships
  ADD CONSTRAINT fk_rel_from_tenant
  FOREIGN KEY (tenant_id, from_customer_id) REFERENCES service_customers (tenant_id, id),
  ADD CONSTRAINT fk_rel_to_tenant
  FOREIGN KEY (tenant_id, to_customer_id) REFERENCES service_customers (tenant_id, id);
ALTER TABLE customer_tenant_links
  ADD CONSTRAINT fk_ctlink_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id);
ALTER TABLE customer_external_ids
  ADD CONSTRAINT fk_extid_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id);
ALTER TABLE customer_handover_packages
  ADD CONSTRAINT fk_handover_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id);

-- 合同 / 项目域
ALTER TABLE support_contracts
  ADD CONSTRAINT fk_sc_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id);
ALTER TABLE project_customer_links
  ADD CONSTRAINT fk_pcl_project_tenant
  FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id),
  ADD CONSTRAINT fk_pcl_customer_tenant
  FOREIGN KEY (tenant_id, customer_id) REFERENCES service_customers (tenant_id, id),
  ADD CONSTRAINT fk_pcl_branch_tenant
  FOREIGN KEY (tenant_id, customer_id, branch_id)
  REFERENCES customer_branches (tenant_id, customer_id, id);
```

> `contract_project_links`、`contract_service_items`、`contract_amendments`、`contract_usage_ledger` 的复合外键在建表时内联声明（见 §12.3 与 06 §4.3/§4.4/§4.5）。约束创建前先查 `pg_constraint` 判重，避免重复执行报错；`projects` 若已有同名唯一索引则跳过。跨租户弱引用（`linked_tenant_id`、外部系统标识）不在此列，仍由服务层显式授权校验。

```sql
-- 复合外键上线前的跨租户引用预检（必须返回 0 条）
SELECT 'support_contracts' AS table_name, c.id, c.tenant_id, sc.tenant_id AS parent_tenant_id
FROM support_contracts c JOIN service_customers sc ON sc.id = c.customer_id
WHERE c.tenant_id <> sc.tenant_id
UNION ALL
SELECT 'project_customer_links', l.id, l.tenant_id, p.tenant_id
FROM project_customer_links l JOIN projects p ON p.id = l.project_id
WHERE l.tenant_id <> p.tenant_id;
```

## 8. 回填与迁移顺序

| 步骤 | 动作 | 幂等性 |
|---|---|---|
| M1 | 执行 §1~§7 DDL（含 §7.1 交接包；全部 `IF NOT EXISTS`） | 幂等 |
| M2 | `code` 回填：按 `id` 生成 `CUS-0001`（仅 `code IS NULL`） | 幂等 |
| M3 | `customer_type` 回填：`linked_customer_tenant_id IS NULL → 'direct'`，否则 `'msp_endcustomer'` | 幂等 |
| M4 | `linked_customer_tenant_id` → `customer_tenant_links`（`link_type='primary'`、`is_billing_anchor=true`），旧列保留只读 | 幂等（`NOT EXISTS` 判重） |
| M5 | `email_domains` 回填：取 `source_organizations.email_domains` 中与该客户有会话记录的域名，按出现频次取最高 | 幂等 |
| M6 | `data_quality_score` 首次计算（见主设计 §10 评分规则） | 幂等（全量重算） |
| M7 | `lifecycle_stage` 回填：`status='active' AND linked_tenant 存在 → 'active'`，否则保持 `'prospect'` | 幂等 |
| M7b | 建父端唯一索引 + 客户/项目侧复合外键（§7.2） | 需先清洗跨租户引用；需人工确认 |
| M8 | 新表入 RLS：`cmd/rls-apply` 表清单 + `internal/schema/tenant_guard.go` | 需人工登记 |
| M9 | 补 `support_contracts` 增强列（**以 [06 §7.2](./06-contract-modeling-design.md) 为最终版**） | 幂等 |
| M10 | 存量合同 `contract_type` 回填：`effective_from/effective_until` 跨度 ≥ 330 天 → `framework`，否则 `support` | 幂等 |
| M11 | 清理 L-01 违规数据（分支不属于客户），**需人工确认** | 需人工 |
| M12 | 建复合外键 `fk_contract_branch_tenant_customer`（见 §12.2） | 幂等（需查 `pg_constraint`） |
| M13 | `contract_project_links` 建表 + 存量**仅生成建议清单**（不自动建链） | 幂等 |

```sql
-- M2 示例
UPDATE service_customers
SET code = 'CUS-' || LPAD(id::text, 4, '0')
WHERE code IS NULL;

-- M4 示例
INSERT INTO customer_tenant_links
  (tenant_id, customer_id, linked_tenant_id, link_type, is_billing_anchor)
SELECT sc.tenant_id, sc.id, sc.linked_customer_tenant_id, 'primary', TRUE
FROM service_customers sc
WHERE sc.linked_customer_tenant_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM customer_tenant_links l
    WHERE l.tenant_id = sc.tenant_id
      AND l.customer_id = sc.id
      AND l.linked_tenant_id = sc.linked_customer_tenant_id
      AND l.deleted_at IS NULL
  );
```

## 9. RLS 与受管清单登记

新表 `customer_contacts`、`customer_relationships`、`customer_tenant_links`、`project_customer_links`、`customer_external_ids`、`customer_handover_packages` 必须同时登记到：

1. `itsm-backend/cmd/rls-apply` 的受管表清单（策略统一 `tenant_id = get_current_tenant_id()`）；
2. `itsm-backend/internal/schema/tenant_guard.go` 的租户守护清单；
3. `docs/multi-tenant/07-known-gaps.md` 若存在暂缓项需登记。

> 注意：`project_customer_links` 的 RLS 以**项目所属租户**为作用域，与 `projects` 一致，避免出现「项目可见但关联被 RLS 拦截」的半可见状态。

## 10. 回滚策略

- 全部为**新增列 / 新增表**，不删不改原列；回滚只需 `DROP` 新表、`ALTER TABLE ... DROP COLUMN`；
- 旧路径（邮件报障匹配、`linked_customer_tenant_id` 读取）在灰度期保持双读：先读新表，缺失回退旧列；
- 开关：`CUSTOMER_MASTER_V2_ENABLED`（默认 `false`，按租户灰度）。

## 11. Ent Schema 落地提示

按本仓既有风格（`ent/schema/service_customer.go`）扩展时注意：

- 新增 `field.Time(...).Optional().Nillable()` 用于可空时间，避免零值写入；
- 部分唯一索引使用 `entsql.IndexWhere`，与 `MSPAllocation` / `ProjectMember` 既有写法一致；
- 同租户内部引用应使用 `(tenant_id, parent_id)` 复合外键（§7.2）；**跨租户引用一律弱引用**（`customer_tenant_links.linked_tenant_id`、外部系统标识），由服务层授权校验，与 `ProjectMember` 注释口径一致；
- `customer_relationships` 的自引用边需显式命名，避免 ent 生成冲突（建议 `edge.From("from_customer", ServiceCustomer.Type)` + `edge.From("to_customer", ServiceCustomer.Type)`，并在 `ServiceCustomer` 侧用不同 Ref 名）。

---

## 12. 附：合同增强与合同 ↔ 项目链接 DDL（P1）

> 补齐「客户 → 合同 → 项目」业务闭环。设计说明见 [05-customer-contract-project-linkage.md](./05-customer-contract-project-linkage.md)。
>
> ⚠️ **合同实体字段以 [06 §7.2](./06-contract-modeling-design.md) 为最终版**：§12.1 的过渡 DDL 已撤销；最终版补 `effective_from/effective_until`（统一契约 CC-3）、`parent_contract_id`/`renewal_of_contract_id`/`amendment_count`、`deleted_at`/`version`/`created_by`/`updated_by`，并新增 `contracts`（vendor 采购）的全局唯一性缺陷修复与 `contract_service_items`/`contract_amendments`/`contract_usage_ledger` 三张表。落库请以 06 为准。

### 12.1 合同增强（`support_contracts`）

> **本节的过渡 DDL 已撤销。** `support_contracts` 的最终字段、状态、金额与索引统一见 [06 §7.2](./06-contract-modeling-design.md)。本节只保留合同 ↔ 项目链接与归属一致性约束，避免同一字段出现两套权威定义。

### 12.2 归属一致性铁律 L-01：复合外键（修复真实漏洞）

当前 `support_contracts.branch_id` 仅 `REFERENCES customer_branches(id)`，**允许存出「合同属于客户 A、却引用客户 B 分支」的脏数据**。

```sql
-- 步骤 1：先排查存量异常（必须返回 0 条才能继续）
--   SELECT c.id, c.contract_number, c.tenant_id, c.customer_id, c.branch_id,
--          b.tenant_id, b.customer_id
--   FROM support_contracts c JOIN customer_branches b ON b.id = c.branch_id
--   WHERE c.branch_id IS NOT NULL
--     AND (b.tenant_id <> c.tenant_id OR b.customer_id <> c.customer_id);

-- 步骤 2：加复合唯一键，作为复合外键的被引用端
-- 已在 §7.2 建立：
--   uq_branch_tenant_id         (tenant_id, id)
--   uq_branch_tenant_customer_id(tenant_id, customer_id, id)

-- 步骤 3：加复合外键（branch_id 为 NULL 时不校验，正好符合"客户级合同"语义）
ALTER TABLE support_contracts
  ADD CONSTRAINT fk_contract_branch_tenant_customer
  FOREIGN KEY (tenant_id, customer_id, branch_id)
  REFERENCES customer_branches (tenant_id, customer_id, id);
```

> `support_contracts.customer_id` 也必须用 `(tenant_id, customer_id) → service_customers(tenant_id, id)` 复合外键兜住（见 §7.2），否则 L-01 只修了分支、没修合同客户本身。步骤 3 需先查 `pg_constraint` 判重，避免重复执行报错。

### 12.3 合同 ↔ 项目链接（`contract_project_links`）

```sql
CREATE TABLE IF NOT EXISTS contract_project_links (
  id               BIGSERIAL PRIMARY KEY,
  tenant_id        BIGINT NOT NULL,
  contract_id      BIGINT NOT NULL REFERENCES support_contracts(id),
  project_id       BIGINT NOT NULL,          -- projects.id（同租户）
  service_item_id  BIGINT,                   -- billing 行必须指向 contract_service_items.id
  relation_type    VARCHAR(24) NOT NULL DEFAULT 'basis',
      -- basis（立项依据）|coverage（运维覆盖）|billing（额度消耗）|change（变更追加）
  -- 只有 billing 行承载额度；其余关系行只表达语义，避免同一项目重复记账
  allocated_amount NUMERIC(18,2),            -- 金额额度（billing）
  allocated_hours  NUMERIC(10,2),            -- 数量额度（工时/工单/设备/项目，按服务项 billing_unit 解释）
  allocation_pct   SMALLINT,                 -- 比例额度（billing，与金额/数量二选一或并存）
  is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
  note             VARCHAR(500),
  effective_from   DATE,
  effective_to     DATE,
  status           VARCHAR(16) NOT NULL DEFAULT 'active',   -- active|ended|cancelled
  deleted_at       TIMESTAMPTZ,
  created_by       BIGINT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_cpl_amount CHECK (allocated_amount IS NULL OR allocated_amount >= 0),
  CONSTRAINT ck_cpl_hours  CHECK (allocated_hours IS NULL OR allocated_hours >= 0),
  CONSTRAINT ck_cpl_pct    CHECK (allocation_pct IS NULL OR (allocation_pct >= 0 AND allocation_pct <= 100)),
  CONSTRAINT ck_cpl_billing_only CHECK (
    relation_type = 'billing'
    OR (allocated_amount IS NULL AND allocated_hours IS NULL
        AND allocation_pct IS NULL AND service_item_id IS NULL)
  ),
  CONSTRAINT ck_cpl_billing_item CHECK (
    relation_type <> 'billing' OR service_item_id IS NOT NULL
  ),
  CONSTRAINT ck_cpl_billing_amount CHECK (
    relation_type <> 'billing'
    OR allocated_amount IS NOT NULL
    OR allocated_hours IS NOT NULL
    OR allocation_pct IS NOT NULL
  ),
  CONSTRAINT ck_cpl_primary_basis CHECK (
    NOT is_primary OR relation_type = 'basis'
  ),
  CONSTRAINT fk_cpl_contract_tenant
    FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id),
  CONSTRAINT fk_cpl_project_tenant
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id)
);

-- 非 billing 的语义关系：同一合同+项目+关系类型只允许一条存活记录
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_semantic
  ON contract_project_links (tenant_id, contract_id, project_id, relation_type)
  WHERE deleted_at IS NULL AND status = 'active' AND relation_type <> 'billing';

-- billing 的额度台账：同一合同+项目+服务项只允许一条生效记录
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_billing
  ON contract_project_links (tenant_id, contract_id, project_id, service_item_id)
  WHERE deleted_at IS NULL AND status = 'active' AND relation_type = 'billing';

-- 每项目最多一条「主依据」
CREATE UNIQUE INDEX IF NOT EXISTS uq_cpl_primary
  ON contract_project_links (tenant_id, project_id)
  WHERE is_primary AND relation_type = 'basis'
    AND status = 'active' AND deleted_at IS NULL;

-- 【关键】合同 → 项目 正查路径（原设计缺失）
CREATE INDEX IF NOT EXISTS idx_cpl_tenant_contract
  ON contract_project_links (tenant_id, contract_id, status);
CREATE INDEX IF NOT EXISTS idx_cpl_tenant_project
  ON contract_project_links (tenant_id, project_id, status);
```

> `service_item_id` 的复合外键 `(tenant_id, service_item_id) → contract_service_items(tenant_id, id)` 在 06 §7.3 建表后追加，避免 02 的 M13 早于 06 的 M14 执行时约束失败。

### 12.4 服务层必须实现的五条铁律

| 铁律 | 校验内容 | 错误码 |
|---|---|---|
| L-01 | `support_contracts.branch_id` 所属客户 = 合同客户（数据库复合外键兜底） | `CONTRACT_BRANCH_CUSTOMER_MISMATCH` |
| L-02 | 项目主客户 = 合同客户（两端客户一致） | `CPL_CUSTOMER_MISMATCH` |
| L-03 | 仅 `projects.type <> 'internal'` 可关联客户 | `PCL_PROJECT_TYPE_INVALID` |
| L-04 | 已挂项目的合同禁止改 `customer_id`；**客户合并是唯一例外**，需 `customer.merge` 权限 + 同事务审计 | `CONTRACT_CUSTOMER_IMMUTABLE` |
| L-05 | `billing` 行的 `service_item_id` 必须属于同一合同，且额度单位与 `billing_unit` 一致 | `CPL_SERVICE_ITEM_MISMATCH` |

> 数据库兜住 L-01；L-02 / L-03 / L-04 / L-05 必须由服务层 fail-closed 实现，并补单测覆盖。客户合并的例外只允许在合并事务内触发，且必须按“项目客户链接 → 合同客户 → 其他关联”的顺序执行。

### 12.5 RLS 登记补充

`contract_project_links`、`contract_service_items`、`contract_amendments`、`contract_usage_ledger` 必须与 `support_contracts` 同作用域登记到：

1. `cmd/rls-apply` 受管表清单；
2. `internal/schema/tenant_guard.go` 租户守护清单。

### 12.6 RLS 收回与快照表登记（018 建表 / 019 收回，2026-11-06 裁决后新增）

本节是「业务数据不跨租户」硬约束（BD-1）落到迁移清单的收口项，完整因果见 [07 §3.2 / §4.5](./07-tenant-data-boundary.md)。

#### 12.6.1 新增受管表（客户租户内副本）

| 表 | tenant_id 归属 | 作用域 | 用途 |
|---|---|---|---|
| `contract_sla_snapshots` | **customer 租户** | `tenant_id = get_current_tenant_id()` | L1 SLA 匹配的本地副本，替代跨租户读 `msp_contracts` |
| `settlement_statements` | **customer 租户** | 同上 | 结算/下发快照（含 `payload_hash`），替代跨租户读结算报表 |

两表必须同时登记到：

1. `itsm-backend/cmd/rls-apply` 受管表清单；
2. `itsm-backend/internal/schema/tenant_guard.go` 租户守护清单。

> 关键约束：**这两张表没有 `customer_tenant_id` 这种用于 RLS 的"对方租户"列**，RLS 只认自己的 `tenant_id`。`contract_sla_snapshots.source_tenant_id` 仅用于溯源，**不得进入任何 policy 谓词**；任何想靠"行上有个对方租户 ID"实现可见性的写法，都会重犯 012 的错误。`settlement_statements` 的建表与 RLS 策略见 [07 §4.2](./07-tenant-data-boundary.md)。

#### 12.6.2 `contract_sla_snapshots` 最小字段集

```sql
CREATE TABLE IF NOT EXISTS contract_sla_snapshots (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           BIGINT NOT NULL,                    -- 客户租户，RLS 唯一作用域
    source_contract_id  BIGINT NOT NULL,                    -- provider 侧 msp_contracts.id，仅作引用，不建外键
    source_tenant_id    BIGINT NOT NULL,                    -- provider 租户，仅作溯源，不参与 RLS
    contract_no         TEXT NOT NULL,
    status              TEXT NOT NULL,                       -- active|expired|terminated，随源合同同步
    effective_from      TIMESTAMPTZ NOT NULL,
    effective_until     TIMESTAMPTZ,
    sla_definition_ids  BIGINT[] NOT NULL DEFAULT '{}',      -- 引用集，便于追溯
    sla_terms           JSONB NOT NULL DEFAULT '{}'::jsonb,  -- 物化的时限事实（响应/解决/工作时段/条件），不含罚则
    ticket_type_map     JSONB NOT NULL DEFAULT '{}'::jsonb,  -- {ticket_type_id: sla_definition_id}
    payload_hash        TEXT NOT NULL,                       -- 幂等与篡改校验
    synced_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_css_tenant_source UNIQUE (tenant_id, source_tenant_id, source_contract_id)
);
CREATE INDEX IF NOT EXISTS idx_css_tenant_status ON contract_sla_snapshots (tenant_id, status);

ALTER TABLE contract_sla_snapshots ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS contract_sla_snapshots_tenant_all ON contract_sla_snapshots;
CREATE POLICY contract_sla_snapshots_tenant_all ON contract_sla_snapshots
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());
```

**刻意不包含的字段**（这是与 012 违规策略的核心差别）：

| 字段 | 为什么不进快照 |
|---|---|
| `penalties`（违约率/上限） | 客户只需知道自己适用哪套时限，不需要看到服务商的违约罚则口径 |
| `currency` | 结算口径，随 `settlement_statements` 按结算期下发 |
| `settlement_cycle` | 同上，属于结算事实而非 SLA 事实 |
| 合同商务条款全文 | 争议条款不应在无授权、无生命周期校验的行级策略下暴露 |

> `sla_terms` 必须包含 L1 计算所需的响应/解决时限、工作时段与匹配条件；它是一份**裁剪后的时限事实**，不是 provider 的完整条款。这样 L1 不需要再回读 `sla_definitions`。

#### 12.6.3 019 迁移的执行前置检查（四项，缺一不可）

| # | 检查项 | 判定标准 | 不通过的后果 |
|---|---|---|---|
| C1 | 客户侧无 `msp_contracts` 直读调用 | 访问日志中 `msp_contracts` 的 SELECT 全部来自 provider 租户；`matchContractDefinition` 已改读快照 | 直接 DROP 后该调用返回 0 行 |
| C2 | 存量结算单已迁入 `settlement_statements` | 历史结算报表可从快照复现 | 客户侧结算页空白 |
| C3 | 快照双写观察满一个结算周期 | L1 命中率与改前一致（有对比基线） | L1 静默降级到 L4 默认 60/480 分钟 |
| C4 | 回滚脚本已演练 | 重新 `CREATE POLICY` 可在 1 分钟内恢复 | 出现问题无法快速止损 |

#### 12.6.4 迁移与回滚

```sql
-- 018_contract_sla_snapshots.sql 建 contract_sla_snapshots + settlement_statements 并双写；
-- 019_msp_contract_drop_customer_read.sql（在 C1~C4 全部通过后执行）
DROP POLICY IF EXISTS msp_contract_customer_read ON msp_contracts;
DROP POLICY IF EXISTS msp_contract_binding_customer_read ON msp_contract_sla_bindings;
```

回滚脚本 `019_..._rollback.sql` 需原样恢复 012 的两条策略定义（含 `USING` 谓词），并同步回退 `matchContractDefinition` 到读 `msp_contracts` 的旧实现。

> ⚠️ **禁止的路径**：先执行 019 再想办法改 L1。降级不可观测——只有 warn 日志，工单照常计时但用的是默认值，覆盖率虚高、漏报违约、结算基准失真。

### 12.7 SLA 定义快照（020 建表 / 021 收回 Q-09）

> Q-09 已裁决 B（B-lite）：`sla_definitions` 仍是 provider 唯一权威源；customer 只读自己租户内的 `sla_definition_snapshots`；`sla_definition_grants` 保留为 provider 侧授权记录。
>
> 落库文件：`itsm-backend/migrations/20261108_sla_definition_snapshots.sql` + `database/rls/migrations/020_sla_definition_snapshots_policies.sql`；收回：`021_sla_definition_drop_customer_read.sql`。

```sql
CREATE TABLE IF NOT EXISTS sla_definition_snapshots (
  id                   BIGSERIAL PRIMARY KEY,
  tenant_id            BIGINT NOT NULL,              -- 客户租户，RLS 唯一作用域
  source_kind          VARCHAR(24) NOT NULL,          -- contract_binding|grant
  source_contract_id   BIGINT,                        -- provider msp_contracts.id；grant 行可空
  source_contract_no   VARCHAR(160),                  -- 仅展示/溯源
  source_binding_id    BIGINT,                        -- provider msp_contract_sla_bindings.id
  source_grant_id      BIGINT,                        -- provider sla_definition_grants.id；合同行可空
  source_definition_id BIGINT NOT NULL,              -- provider sla_definitions.id，仅溯源
  source_tenant_id     BIGINT NOT NULL,              -- provider 租户，仅溯源
  source_version       BIGINT NOT NULL,              -- provider 发布版本，单调递增
  definition_name      VARCHAR(255) NOT NULL,        -- 客户可见的展示名
  description          TEXT,
  service_type         VARCHAR(64),
  priority             VARCHAR(32),
  ticket_type_id       BIGINT,                       -- 客户租户工单类型；NULL=类型无关
  terms                JSONB NOT NULL DEFAULT '{}'::jsonb,
      -- {response_time,resolution_time,business_hours,escalation_rules,conditions,priority,service_type}
  effective_from       TIMESTAMPTZ NOT NULL,
  effective_until      TIMESTAMPTZ,                  -- 半开区间；NULL=长期有效
  status               VARCHAR(16) NOT NULL DEFAULT 'active', -- active|superseded|expired|revoked
  payload_hash         VARCHAR(64) NOT NULL,
  synced_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_sds_terms CHECK (jsonb_typeof(terms) = 'object'),
  CONSTRAINT ck_sds_time CHECK (effective_until IS NULL OR effective_until > effective_from),
  CONSTRAINT ck_sds_status CHECK (status IN ('active','superseded','expired','revoked')),
  CONSTRAINT ck_sds_source_kind CHECK (source_kind IN ('contract_binding','grant'))
);

CREATE INDEX IF NOT EXISTS idx_sds_tenant_effective
  ON sla_definition_snapshots (tenant_id, source_kind, status, effective_from, effective_until);
CREATE INDEX IF NOT EXISTS idx_sds_contract
  ON sla_definition_snapshots (tenant_id, source_contract_id, status);
CREATE INDEX IF NOT EXISTS idx_sds_ticket_type
  ON sla_definition_snapshots (tenant_id, ticket_type_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sds_contract_version
  ON sla_definition_snapshots (tenant_id, source_contract_id, source_binding_id, source_version)
  WHERE source_kind = 'contract_binding'
    AND source_contract_id IS NOT NULL
    AND source_binding_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_sds_grant_version
  ON sla_definition_snapshots (tenant_id, source_grant_id, source_version)
  WHERE source_kind = 'grant'
    AND source_grant_id IS NOT NULL;

ALTER TABLE sla_definition_snapshots ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS sla_definition_snapshots_tenant_all ON sla_definition_snapshots;
CREATE POLICY sla_definition_snapshots_tenant_all ON sla_definition_snapshots
  FOR ALL
  USING (tenant_id = get_current_tenant_id())
  WITH CHECK (tenant_id = get_current_tenant_id());
```

> `sla_definition_snapshots` 必须同时登记到 `cmd/rls-apply` 与 `internal/schema/tenant_guard.go`。

> `ticket_type_id` 由服务层校验必须属于 customer 租户；`NULL` 表示类型无关授权。`source_kind` 用于 `contract_binding|grant` 读取分流；其余 `source_*` 只作溯源，均不参与 RLS。

**020 回填（active grants 示例；合同绑定由 `cmd/backfill_sla_snapshots` 一并回填）**

```sql
WITH src AS (
  SELECT
    g.customer_tenant_id AS tenant_id,
    'grant'::varchar     AS source_kind,
    NULL::bigint         AS source_contract_id,
    NULL::varchar        AS source_contract_no,
    NULL::bigint         AS source_binding_id,
    d.id                 AS source_definition_id,
    g.tenant_id          AS source_tenant_id,
    g.id                 AS source_grant_id,
    1::bigint            AS source_version,
    d.name               AS definition_name,
    NULLIF(g.ticket_type_id, 0) AS ticket_type_id,
    jsonb_build_object(
      'response_time', d.response_time,
      'resolution_time', d.resolution_time,
      'business_hours', d.business_hours,
      'conditions', d.conditions,
      'priority', d.priority,
      'service_type', d.service_type
    ) AS terms,
    COALESCE(g.created_at, NOW()) AS effective_from,
    g.valid_until AS effective_until,
    'active' AS status
  FROM sla_definition_grants g
  JOIN sla_definitions d
    ON d.id = g.sla_definition_id AND d.tenant_id = g.tenant_id
  WHERE g.is_active = TRUE
)
INSERT INTO sla_definition_snapshots (
  tenant_id, source_kind, source_contract_id, source_contract_no,
  source_binding_id, source_definition_id, source_tenant_id, source_grant_id,
  source_version, definition_name, ticket_type_id, terms,
  effective_from, effective_until, status, payload_hash
)
SELECT
  tenant_id, source_kind, source_contract_id, source_contract_no,
  source_binding_id, source_definition_id, source_tenant_id, source_grant_id,
  source_version, definition_name, ticket_type_id, terms,
  effective_from, effective_until, status,
  encode(sha256(convert_to(jsonb_build_object(
    'source_definition_id', source_definition_id,
    'source_grant_id', source_grant_id,
    'definition_name', definition_name,
    'ticket_type_id', ticket_type_id,
    'terms', terms,
    'effective_from', effective_from,
    'effective_until', effective_until,
    'status', status
  )::text, 'UTF8')), 'hex')
FROM src
ON CONFLICT DO NOTHING;
```

> `sla_definition_grants` 没有 `valid_from`，回填暂以 `created_at` 作为生效起点；若业务存在更早的合同生效日，必须在 `020` 前补数据，不能默认用回填时间。

> 实际 `payload_hash` 由服务层按 `source_kind`、合同/grant 溯源字段、条款、有效期与状态统一计算；上例仅为 SQL 回填示意。

**切换与收回顺序**

> 全局迁移编号：`M21` = `020`，`M22` = `021`，`M23` = 可选的 `022`（删除 `sla_grant_customer_read`）。

1. `020` 建表并回填；provider 在 grant/definition 发布、变更时写入新 `source_version`，服务层计算 `payload_hash`；撤销时把该 grant 的 active 快照置为 `revoked`，不再生成新版本；
2. L1、provided 列表已改读 `sla_definition_snapshots`；AI/搜索/导出若涉及下发定义，必须走同一快照查询，禁止回读 provider 原行（当前无直接依赖）；旧 `sla_definition_customer_read` 仅保留为回退；
3. 影子对比 L1 命中率、工单 SLA 覆盖率与旧链路一致，观察一个发布周期；
4. `021_sla_definition_drop_customer_read.sql` 删除 `sla_definition_customer_read`；
5. `sla_grant_customer_read` 继续作为授权元数据 allowlist；确认无 customer 直接查询后，再由 `022` 评估删除；
6. 回滚 `021` 需恢复 `sla_definition_customer_read` 并回退 L1 到旧读路径；`020` 的快照表保留，不回删。

> 客户侧 `sla_definitions` 查询必须返回 0 行；`sla_definition_snapshots` 的 `source_*` 仅溯源，不参与 RLS。
