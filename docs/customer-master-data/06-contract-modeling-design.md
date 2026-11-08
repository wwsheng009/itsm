# 06 · 合同建模与业务数据逻辑优化

> 状态：Draft v1.1 · 2026-11-08
> 前置：所有数据不跨租户（[04](./04-multi-provider-scenario.md)）；合同 ↔ 项目闭环见 [05](./05-customer-contract-project-linkage.md)
> 代码基线：`ent/schema/contract.go`、`support_contract.go`、`msp_contract.go`、`msp_contract_sla_binding.go`

---

## 1. 现状：三套合同实体并存

这是本项目合同建模的**根本事实**，任何优化都必须先承认它，而不是再加第四套。

| 维度 | `contracts`（vendor 采购） | `support_contracts`（客户支持） | `msp_contracts`（MSP SLA） |
|---|---|---|---|
| 语义 | 我方向**上游供应商**采购 | 我方对**客户**的服务承诺 | provider 对 customer 租户的**SLA 结算** |
| 归属 | `tenant_id`（无约束） | `tenant_id`（RLS） | `tenant_id = provider_tenant_id`（RLS） |
| 对方主体 | `vendor_id`（**Optional**） | `customer_id`（Required）+ `branch_id` | `customer_tenant_id`（provider 内部业务字段，**不构成授信**；见 [07 §3.2](./07-tenant-data-boundary.md)） |
| 合同号字段 | `contract_number` | `contract_number` + `normalized_contract_number` | `contract_no` |
| 唯一性 | **全局 `.Unique()`** ⚠️ | `(tenant_id, normalized_contract_number)` | `(tenant_id, contract_no)` |
| 金额 | `value` **Float** ⚠️ | **无** | **无**（仅 `currency` + `penalties`） |
| 日期字段 | `start_date` / `end_date` | `start_at` / `end_at` | `effective_from` / `effective_until` |
| 状态 | 自由字符串，默认 `active` | 自由字符串 | `draft\|active\|expired\|terminated`（有注释无约束） |
| 条款明细 | 无 | 无 | `penalties` JSON + `sla_bindings` 关联表 |
| 索引 | 仅合同号全局唯一 | `(tenant_id, customer_id, status)` | `(tenant_id, customer_tenant_id, status)` |

> `msp_contract.go` 源码注释已明确写道：「与既有 contract.go（vendor 合同）/ support_contract.go（支持合同）为**不同域实体**」。方向是对的，但**三个域各自长出、没有统一契约**，于是产生了下面 6 个真实缺陷。

---

## 2. 六个可验证的建模缺陷

### D-1（Bug）合同号全局唯一，多租户下必然冲突

```go
// contract.go:17
field.String("contract_number").NotEmpty().Unique(),
```

`Unique()` 在 ent 中是**表级全局唯一**，没有 `tenant_id` 前缀。后果：

- 租户 A 录入合同号 `HT-2026-001` 后，租户 B 录同一个号会**直接失败**；
- 这在多租户 + 多客户场景下几乎必然发生（合同号是各企业自己的编号体系，天然重复）。

对照 `support_contracts` / `msp_contracts` 都用 `(tenant_id, contract_no)` —— **同一项目内三套实体，唯一性口径不一致**。

### D-2（Bug）金额用 Float

```go
field.Float("value").Default(0),   // contract.go:20
```

`0.1 + 0.2 != 0.3` 的经典问题。合同金额用于额度分配与结算比对，必须 `NUMERIC(18,2)`。而 `support_contracts` **完全没有金额字段**，`msp_contracts` 只有 `penalties` 与 `currency` —— 意味着**没有任何一处能回答"这份合同值多少钱"**。

### D-3 日期字段三种命名，无法统一报表

`start_date/end_date`、`start_at/end_at`、`effective_from/effective_until` 三套并存，跨域做「本季度到期的合同」这类查询必须写三段 UNION，且极易漏掉一套。

### D-4 `vendor_id` 可为空，采购合同可以没有供应商

```go
field.Int("vendor_id").Optional(),   // contract.go:25
```

采购合同没有供应商在业务上不成立。同时 `Contracts` 表**除合同号外没有任何索引**，按 vendor 或到期时间查询都是全表扫描。

### D-5 合同只是「一张封面」，无法履约

三套实体都只有「编号 + 期限 + 状态」，**没有条款、没有服务项明细、没有计量单位、没有额度**。后果：

- 无法回答「这份合同包含哪些服务、各多少量」；
- 无法核算「合同额度被消耗了多少」（[05](./05-customer-contract-project-linkage.md) 的 `billing` 分配行只挂在项目侧，合同侧没有服务项额度与实耗台账）；
- 服务目录（`service_catalog`）与合同完全脱钩，卖什么、卖多少没有记录。

### D-6 状态语义重叠，「生效」与「未到期」混为一谈

`contracts.status` 注释写「生效, 到期, 终止」，默认值却是 `active`；`expired` 是**时间到了自动发生**的事实，不是人操作的状态。把时间事实与操作状态塞进同一个字段，会导致：

- 定时任务漏跑 → 状态与事实不符；
- 「已到期但仍显示 active」→ 续约提醒失效。

---

## 3. 定位优化：不合并实体，但统一契约

### 3.1 为什么**不**合并成一张表

| 合并方案的代价 | 说明 |
|---|---|
| 采购与销售方向相反 | `contracts` 是我付钱给别人，`support_contracts` 是别人付钱给我，字段语义（对方主体、金额方向、违约方向）根本不同 |
| 跨租户约束冲突 | `msp_contracts` 现有 RLS 存在**客户租户只读放宽**（`msp_contract_customer_read`），本身违反 BD-1「业务数据不跨租户」，须先收回（见 [07 §4](./07-tenant-data-boundary.md)）；而 `support_contracts` 是强隔离。合并后无法同时满足两种安全语义 |
| 已有代码依赖 | `msp_contract.go` 已被 D-SLA-4 采纳并绑定 `msp_contract_sla_bindings`，合并等于推翻既有决策 |

**结论：保留三套实体，划分清晰职责边界 + 施加统一契约（Conventions）。**

### 3.2 三层合同域职责边界

```
┌─ 采购域 PROCUREMENT ──────────────────────────────┐
│  contracts（我方 ← 供应商）                        │
│  约束：我方要付什么钱、拿什么服务                    │
│  对方：vendors                                     │
└───────────────────────────────────────────────────┘

┌─ 交付域 DELIVERY（本次优化重点）────────────────────┐
│  support_contracts（我方 → 客户）                  │
│  约束：我方承诺给客户什么服务、什么标准、多少量        │
│  对方：service_customers / customer_branches       │
│  下游：contract_project_links → projects（05）     │
└───────────────────────────────────────────────────┘

┌─ 结算域 SETTLEMENT ───────────────────────────────┐
│  msp_contracts（provider ↔ customer 租户）          │
│  约束：SLA 条款 + 罚则 + 结算周期（D-SLA-4/8/9）     │
│  对方：customer_tenant_id（内部字段，不授信）        │
└───────────────────────────────────────────────────┘
```

### 3.3 判定规则：新需求该落到哪一层

| 问题 | 落到 |
|---|---|
| 我方要向供应商采购设备/服务 | `contracts` |
| 客户买了我们的运维/支持服务，约定 SLA 与响应时限（**同租户内商务约定**） | `support_contracts` |
| provider 要给 customer 租户定 SLA 罚则并出结算快照 | `msp_contracts` |
| 合同包含哪些服务项、各多少量、单价多少 | `support_contracts` + `contract_service_items` |
| 合同支撑了哪些交付项目 | `contract_project_links`（05） |
| 合同金额/额度被消耗多少 | `contract_service_items` 额度 + `billing` 分配行 + `contract_usage_ledger` 实耗台账 |

### 3.4 统一契约（Contract Conventions，CC-1 ~ CC-8）

> 契约按域**分阶段适用**：CC-1 / CC-2 / CC-8 立即适用于三个域；CC-3~CC-7 先在交付域（`support_contracts`）强制执行，采购域与结算域按矩阵逐步收口。未收口项必须在路线图中显式登记，不能假装已统一。

| 编号 | 契约 | 落地要求 |
|---|---|---|
| **CC-1** | 编号一律**租户内唯一** | `UNIQUE (tenant_id, normalized_no)`，禁止全局 `Unique()` |
| **CC-2** | 金额一律 `NUMERIC(18,2)` | 禁止 `Float`；币种必填或继承租户默认 |
| **CC-3** | 有效期字段统一 `effective_from` / `effective_until` | 签订时间另设 `signed_at`；禁止 `start_date/end_date/start_at/end_at` 新写法 |
| **CC-4** | 状态统一枚举 | `draft / pending_sign / active / expired / terminated / renewed`；`expiring` 为派生值不入库 |
| **CC-5** | 软删统一 `deleted_at` | 唯一键用部分索引排除软删行 |
| **CC-6** | 乐观锁 `version` | 对齐 `projects.version` |
| **CC-7** | 操作留痕 `created_by` / `updated_by` | — |
| **CC-8** | 跨租户引用一律弱引用 | 不建跨租户外键（对齐 IP-P0-4 与 04） |

| 契约 | `contracts`（采购） | `support_contracts`（交付） | `msp_contracts`（结算） |
|---|---|---|---|
| CC-1 租户内唯一 | 本次修复（§7.1） | ✅ 现有 | ✅ 现有 |
| CC-2 NUMERIC 金额 | 本次 `value → amount` | ✅ 本次新增 | n/a（无合同金额，只存 `currency`） |
| CC-3 `effective_*` | 🚧 采购域后续迁移 | ✅ 本次 | ✅ 现有 |
| CC-4 状态枚举 | 🚧 采购域后续迁移 | ✅ 本次 | 🚧 后续补 `renewed` 映射 |
| CC-5 软删 | 🚧 采购域后续迁移 | ✅ 本次 | 🚧 后续迁移 |
| CC-6 乐观锁 | 🚧 采购域后续迁移 | ✅ 本次 | 🚧 后续迁移 |
| CC-7 操作留痕 | 🚧 采购域后续迁移 | ✅ 本次 | 🚧 后续迁移 |
| CC-8 跨租户弱引用 | ✅ | ✅ | ✅（G17 收回后） |

**迁移时对旧字段的处理**：`start_date/end_date/start_at/end_at` 保留为**兼容只读列**，新代码只读写 `effective_from/effective_until`，双写一个版本后废弃。

---

## 4. `support_contracts` 重构：从「一条记录」到「合同聚合根」

### 4.1 编号与层级体系

现状只有 `contract_number` 一个平面编号，无法表达「框架合同 + 子合同」「主合同 + 补充协议」。

```
主合同           HT-2026-001            parent_contract_id = NULL
├─ 子合同        HT-2026-001-01         parent_contract_id = 主合同
├─ 子合同        HT-2026-001-02
└─ 补充协议 v2   HT-2026-001  rev=2     amendment（改金额/展期，不新建合同号）
续签合同         HT-2027-001            renewal_of_contract_id = HT-2026-001
```

**归一化规则**（`normalized_contract_number`）

| 规则 | 示例 |
|---|---|
| 全角转半角 | `ＨＴ－2026－001` → `HT-2026-001` |
| 去除所有空白 | `HT 2026 001` → `HT2026001` |
| 字母大写 | `ht-2026-001` → `HT-2026-001` |
| 统一分隔符（`—` `_` `.` → `-`） | `HT_2026_001` → `HT-2026-001` |

> 唯一键建在 `normalized_contract_number` 上，允许原始 `contract_number` 保留客户习惯写法（对齐现有 `support_contracts` 设计）。

### 4.2 状态机重构（时间事实 vs 操作状态分离）

```
        ┌──────────────── 时间驱动（派生，不入库）────────────────┐
        │                                                        │
draft ──提交──> pending_sign ──签订──> active ──effective_until 临近──> (expiring)
                │                       │                        │
                └──撤回──> draft        │                        │
                                        ├──到期未续──> expired ◀──┘
                                        ├──人工终止──> terminated
                                        └──签新合同──> renewed
```

**核心改动：`expiring` / `expired` 都是时间派生状态，列表展示不得依赖定时任务。**

```sql
-- 列表/看板实时计算，避免定时任务漏跑导致状态漂移
CASE
  WHEN status = 'active' AND effective_until IS NOT NULL
       AND effective_until <= now()
  THEN 'expired'
  WHEN status = 'active' AND effective_until IS NOT NULL
       AND effective_until <= now() + (续约提前期)::interval
  THEN 'expiring'
  ELSE status
END AS display_status
```

续约提前期按客户等级：S 级 90 天、A 级 60 天、B/C 级 30 天（与 [01 §4.3](./01-customer-master-data-design.md) 分级影响面对齐）。

| 状态 | 入库 | 谁触发 | 允许的操作 |
|---|---|---|---|
| `draft` | ✅ | 人 | 改全部字段、删除 |
| `pending_sign` | ✅ | 人 | 仅附件与备注；可撤回 |
| `active` | ✅ | 人（签订） | 建服务项、挂项目、建变更 |
| `expiring` | ❌ 派生 | 系统（按时间） | 同 `active` + 续约引导 |
| `expired` | ✅ | 系统（到期任务）或人 | 只读，可续签 |
| `terminated` | ✅ | 人（双人确认） | 只读 |
| `renewed` | ✅ | 系统（续签生成） | 只读，指向新合同 |

> **状态别名禁止**：`ended`、`suspended`、`deleted` 不属于本状态机。历史或外部文档中的 `ended` 按 `expired`（自然到期）或 `renewed`（已续签）映射；提前终止一律 `terminated`。`expired` 即使落库任务故障，`display_status` 仍按时间正确显示。

### 4.3 服务项明细：`contract_service_items`（让合同可履约）

这是"合同从封面变成可履约对象"的关键表——现有三套实体全都没有。

```sql
CREATE TABLE IF NOT EXISTS contract_service_items (
  id                BIGSERIAL PRIMARY KEY,
  tenant_id         BIGINT NOT NULL,
  contract_id       BIGINT NOT NULL REFERENCES support_contracts(id),
  catalog_item_id   BIGINT,              -- 弱引用 service_catalog 项（同租户）
  item_code         VARCHAR(64),         -- 冗余编码，便于无目录时手录
  item_name         VARCHAR(255) NOT NULL,
  spec              VARCHAR(500),        -- 服务规格/范围描述
  billing_unit      VARCHAR(32) NOT NULL DEFAULT 'month',
      -- month|quarter|year|hour|ticket|device|project
  quantity          NUMERIC(18,4) NOT NULL DEFAULT 1,
  period_count      NUMERIC(18,4) NOT NULL DEFAULT 1,
      -- 计费周期数：month/quarter/year 用；hour/ticket/device/project 固定 1
  unit_price        NUMERIC(18,2) NOT NULL DEFAULT 0,
  amount            NUMERIC(18,2) NOT NULL DEFAULT 0,
      -- 合同期内总金额（TCV）：quantity × unit_price × period_count；允许议价覆盖
  quota_total       NUMERIC(18,4),       -- 额度总量（工时/工单数/设备数），空=不限额
  quota_used        NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 已消耗（由 contract_usage_ledger 同事务回写）
  note              VARCHAR(500),        -- 议价/覆盖说明
  sla_definition_id BIGINT,              -- 服务项级 SLA（优先级最高）
  response_target_min  INT,              -- 覆盖性时限（空=用 SLA 定义）
  resolve_target_min   INT,
  sort_order        INT NOT NULL DEFAULT 0,
  status            VARCHAR(16) NOT NULL DEFAULT 'active',   -- active|inactive
  deleted_at        TIMESTAMPTZ,
  created_by        BIGINT,
  updated_by        BIGINT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_csi_qty   CHECK (quantity >= 0),
  CONSTRAINT ck_csi_period CHECK (period_count > 0),
  CONSTRAINT ck_csi_price CHECK (unit_price >= 0),
  CONSTRAINT ck_csi_used  CHECK (quota_used >= 0),
  CONSTRAINT fk_csi_contract_tenant
    FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_csi_tenant_id
  ON contract_service_items (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_csi_contract_code
  ON contract_service_items (tenant_id, contract_id, item_code)
  WHERE deleted_at IS NULL AND item_code IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_csi_tenant_contract
  ON contract_service_items (tenant_id, contract_id, status);
```

**金额与额度口径**
- `amount` 是**合同期内总金额（TCV）**：`quantity × unit_price × period_count`；
- 对 `month/quarter/year`，`period_count` 按合同期折算；对 `hour/ticket/device/project`，固定为 1；
- 允许“整体议价”覆盖乘积（例如打包优惠），但覆盖必须写 `note`，并在审计日志记录原值；
- `quota_used` 只由计量台账回写；`quota_reserved` 不落库，由 `contract_project_links` 的 `billing` 行实时汇总。

### 4.4 变更单：`contract_amendments`

补充协议不应该新建合同号（否则续签链与服务项历史断裂）。

```sql
CREATE TABLE IF NOT EXISTS contract_amendments (
  id             BIGSERIAL PRIMARY KEY,
  tenant_id      BIGINT NOT NULL,
  contract_id    BIGINT NOT NULL REFERENCES support_contracts(id),
  amendment_no   VARCHAR(64) NOT NULL,          -- 合同内序号：A1、A2...
  amend_type     VARCHAR(32) NOT NULL,
      -- amount（增减金额）|term（展期）|scope（范围变更）|id_only（仅主体信息）
  effective_from DATE NOT NULL,
  amount_delta   NUMERIC(18,2) NOT NULL DEFAULT 0,   -- 增减额（可负）
  term_delta_days INT,                               -- 展期天数（可负）
  diff           JSONB DEFAULT '{}',                 -- 变更前后快照 {before:{},after:{}}
  reason         VARCHAR(500),
  status         VARCHAR(16) NOT NULL DEFAULT 'draft',  -- draft|effective|cancelled
  signed_at      TIMESTAMPTZ,
  created_by     BIGINT,
  updated_by     BIGINT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT fk_ca_contract_tenant
    FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_amend_no
  ON contract_amendments (tenant_id, contract_id, amendment_no);
```

---

### 4.5 用量台账：`contract_usage_ledger`（保证额度扣减幂等）

`quota_used` 是聚合值，不能只靠“原子自增”保证正确；计量任务重试、消息重复投递都会造成重复扣减。每次实耗必须先写台账，再在同一事务内更新服务项聚合。

```sql
CREATE TABLE IF NOT EXISTS contract_usage_ledger (
  id                BIGSERIAL PRIMARY KEY,
  tenant_id         BIGINT NOT NULL,
  contract_id       BIGINT NOT NULL,
  service_item_id   BIGINT NOT NULL,
  source_type       VARCHAR(32) NOT NULL,
      -- timesheet|ticket|device_snapshot|project_close|manual_adjustment
  source_ref        VARCHAR(160) NOT NULL,   -- 源单号/事件 ID，幂等键
  quantity_delta    NUMERIC(18,4) NOT NULL DEFAULT 0,
  amount_delta      NUMERIC(18,2) NOT NULL DEFAULT 0,
  occurred_at       TIMESTAMPTZ NOT NULL,
  note              VARCHAR(500),
  created_by        BIGINT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_usage_source UNIQUE (tenant_id, service_item_id, source_type, source_ref),
  CONSTRAINT ck_usage_delta CHECK (quantity_delta <> 0 OR amount_delta <> 0),
  CONSTRAINT fk_usage_contract_tenant
    FOREIGN KEY (tenant_id, contract_id) REFERENCES support_contracts (tenant_id, id),
  CONSTRAINT fk_usage_item_tenant
    FOREIGN KEY (tenant_id, service_item_id) REFERENCES contract_service_items (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_contract
  ON contract_usage_ledger (tenant_id, contract_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_usage_tenant_item
  ON contract_usage_ledger (tenant_id, service_item_id, occurred_at);
```

**写入顺序（必须同事务）**

1. `INSERT ... ON CONFLICT (tenant_id, service_item_id, source_type, source_ref) DO NOTHING`；
2. 若插入成功，`UPDATE contract_service_items SET quota_used = quota_used + quantity_delta WHERE ... AND (quota_total IS NULL OR quota_used + quantity_delta <= quota_total)`；
3. 影响行数为 0 → 回滚并返回 `CONTRACT_QUOTA_EXCEEDED`；
4. 重复 source 命中时比对已有 `quantity_delta/amount_delta`：完全相同则幂等返回，不同则返回 `CONTRACT_USAGE_SOURCE_CONFLICT`，不得覆盖历史台账。

---

## 5. 业务数据逻辑（本次核心）

### 5.1 金额三层与一致性规则

```
① 合同有效总额 effective_amount
     = support_contracts.amount + 生效变更累计
     ≥ SUM(contract_service_items.amount)
         │
② 服务项额度 contract_service_items.quota_total / quota_used
         │
③ 项目分配 contract_project_links.allocated_amount / allocated_hours（仅 billing 行，05）
```

**五条校验规则**

| 规则 | 判定 | 违反时 |
|---|---|---|
| **M-1** | `SUM(service_items.amount)` ≤ `effective_amount`（容差 0.01） | 警告（打包优惠合法），可配为阻断 |
| **M-2** | `SUM(billing_links.allocated_amount WHERE status='active')` ≤ `effective_amount` | 阻断（超额承诺） |
| **M-3** | `amount` 变更必须经 `contract_amendments`，不得直接改 | 阻断 |
| **M-4** | `quota_used` 只能由 **`contract_usage_ledger` 同事务回写**，禁止人工编辑 | 阻断 |
| **M-5** | 只有 `relation_type='billing'` 的 `contract_project_links` 行可携带额度 | 阻断 |

> M-2 的数量维度按服务项执行：`SUM(active billing allocated_hours) ≤ quota_total - quota_used`；金额维度按合同执行：`SUM(active billing allocated_amount) ≤ effective_amount`。两者不能互相替代。

**M-3 的实现方式**：`support_contracts.amount` 在 `status='active'` 后变为**只读派生列**：

```sql
-- 有效合同额 = 原始额 + 已生效变更累计
SELECT c.amount + COALESCE(SUM(a.amount_delta), 0) AS effective_amount
FROM support_contracts c
LEFT JOIN contract_amendments a
  ON a.contract_id = c.id AND a.tenant_id = c.tenant_id
 AND a.status = 'effective'
WHERE c.id = $1 AND c.tenant_id = $2
GROUP BY c.id;
```

> 存储的 `amount` 保持“签约原始额”，`effective_amount` 实时计算。M-1/M-2 的合同上限一律使用 `effective_amount`，不得再直接比较 `contract.amount`。这样变更历史永远可追溯，不会出现“改了金额但说不清为什么”。

### 5.2 额度扣减：预占 ≠ 实耗（最容易做错的地方）

| 概念 | 字段 | 语义 | 谁写入 |
|---|---|---|---|
| **总额度** | `quota_total` | 合同买了多少（1000 工时） | 合同录入 |
| **预占（数量）** | `SUM(contract_project_links.allocated_hours)`（仅 `relation_type='billing'` 且 `status='active'`） | 已承诺给项目，但还没用 | 项目挂接时 |
| **预占（金额）** | `SUM(contract_project_links.allocated_amount)`（仅 `billing` 且 `active`） | 已承诺给项目的合同金额 | 项目挂接时 |
| **实耗** | `quota_used` / `contract_usage_ledger` | 真实消耗（工时已提交） | 台账同事务回写 |
| **剩余可消耗** | `quota_total - quota_used` | 还能消耗多少 | 派生 |
| **可再分配（数量）** | `quota_total - quota_used - SUM(active billing allocated_hours)` | 还能承诺给新项目多少 | 派生 |
| **可再分配（金额）** | `effective_amount - SUM(active billing allocated_amount)` | 还能承诺给新项目的合同金额 | 派生 |

**关键语义**：报表必须**分开呈现预占与实耗**。

```
1000 工时合同
  ├─ 预占 700（分配给 3 个项目）
  │    ├─ 实耗 450
  │    └─ 未耗 250（项目还在跑）
  └─ 未分配 300
```

上例中：剩余可消耗 = 1000 - 450 = 550；可再分配 = 1000 - 450 - 700 = 300。两个值不能混用。

若把预占当实耗，会导致「额度已用完」误报，销售无法再开项目；若把实耗当预占，会导致超额承诺才发现。金额与数量必须分别核算，不能拿 `allocated_amount` 去比较 `quota_total`。

**并发安全与幂等**：先写 `contract_usage_ledger`（唯一 source 幂等），再在同一事务内原子更新 `quota_used` 并做上限守卫：

```sql
-- 1) 台账唯一键吸收重复事件
INSERT INTO contract_usage_ledger
  (tenant_id, contract_id, service_item_id, source_type, source_ref,
   quantity_delta, occurred_at)
VALUES ($tenantId, $contractId, $itemId, 'timesheet', $sourceRef, $delta, NOW())
ON CONFLICT (tenant_id, service_item_id, source_type, source_ref) DO NOTHING;

-- 2) 仅当台账插入成功（影响行数 1）时更新聚合
UPDATE contract_service_items
SET quota_used = quota_used + $delta, updated_at = NOW()
WHERE id = $itemId AND tenant_id = $tenantId
  AND (quota_total IS NULL OR quota_used + $delta <= quota_total)
RETURNING quota_used;
-- 影响行数 0 → 回滚并返回 409 CONTRACT_QUOTA_EXCEEDED
```

### 5.3 状态派生计算（不存储时间事实）

| 展示状态 | 计算方式 |
|---|---|
| `expiring` | `status='active' AND effective_until > now() AND effective_until <= now() + 续约提前期` |
| `expired` | `status='active' AND effective_until <= now()`（展示层实时；落库任务只是补偿） |
| 剩余天数 | `effective_until - now()`，`NULL` 表示长期有效 |

> 列表页一次 SQL 算出，避免 N+1；`display_status` 必须同时覆盖 `expired` 与 `expiring`。`expired` 落库由任务负责，即便任务故障，展示仍按时间正确。

### 5.4 日期口径（必须写死，否则对账必吵架）

| 规则 | 约定 |
|---|---|
| 时区 | 统一存 `timestamptz`；展示按租户时区（`tenants.timezone`，缺省 `Asia/Shanghai`） |
| `effective_from` | **含**当日 00:00:00 |
| `effective_until` | **不含**，存“结束日次日 00:00:00”；例如“到 12 月 31 日”存 `次年 1 月 1 日 00:00:00` |
| 工单归属判定 | `created_at ∈ [effective_from, effective_until)`（对齐 D-SLA-9 白名单④） |
| 长期有效 | `effective_until IS NULL`，不写 `9999-12-31` |

### 5.5 金额精度与舍入

| 规则 | 约定 |
|---|---|
| 存储 | `NUMERIC(18,2)`（2 位小数） |
| 计算中间值 | 保留 4 位，最终结果四舍五入到 2 位（`ROUND(x, 2)`） |
| 累计求和 | 先求和再舍入，**禁止逐行舍入后相加**（避免分位漂移） |
| 币种 | 同一合同内服务项必须同币种；跨币种需拆合同 |
| 禁止 | `float` / `double` / JSON 数字存储金额 |

### 5.6 计量单位与工时换算

| `billing_unit` | 计量来源 | 备注 |
|---|---|---|
| `hour` | 工时记录（timesheet） | 需定义标准工时（8h/人日） |
| `ticket` | 工单计数 | 按合同覆盖类型过滤（D-SLA-9） |
| `device` | CMDB 资产数 | 快照式，按月末取值 |
| `month`/`quarter`/`year` | 时间跨度 | 自动按合同期折算 |
| `project` | 项目数 | 对齐 `contract_project_links` |

> 换算规则集中在一处配置表，禁止散落在业务代码里。

---

## 6. 与 SLA 域、客户域、项目域的对接

### 6.1 与 `msp_contracts` 的分工（不重复造）

| 关注点 | `support_contracts` | `msp_contracts` |
|---|---|---|
| 商务承诺（买了什么服务、多少钱） | ✅ 主责 | ❌ |
| SLA 条款与罚则 | 引用 `sla_definition_id` | ✅ 主责（`penalties` + `sla_bindings`） |
| 结算周期与快照 | ❌ | ✅ 主责（D-SLA-8/9） |
| 跨租户可见性 | ❌ 禁止 | ❌ **禁止**（现状 RLS 012 的 `customer_read` 待收回）；客户侧经**结算快照下发** `settlement_statements` 查看（[07 §4.2](./07-tenant-data-boundary.md)） |
| 服务项明细 | ✅ 主责 | ❌ |

**一句话**：`support_contracts` 是**商务承诺容器**，`msp_contracts` 是**SLA 结算载体**。前者可以有 `sla_definition_id` 做默认值，但罚则与结算永远在后者。

### 6.2 SLA 优先级链（修订版，替换 01 §5.5）

```
服务项级 sla_definition_id（contract_service_items）
  > 合同级 sla_definition_id（support_contracts）
    > 分支级 SLA（customer_branches）
      > 客户级 sla_definition_id（service_customers）
        > 租户默认 SLA
```

> 与 D-SLA-9 的 L1/L2/L3 判定衔接：`msp_contracts.sla_bindings` 提供结算基准，`support_contracts` 提供服务项级细化。

> **Q-09 裁决 B 的读取口径**：合同个案 SLA 读 `contract_sla_snapshots.sla_terms`；非合同/grant 下发的 SLA 定义读 `sla_definition_snapshots.terms`。两者都在 customer 租户内，禁止回读 provider 的 `sla_definitions`。

### 6.3 与项目域（05）

| 环节 | 联动 |
|---|---|
| 建项目客户链接 | 不强制存在合同；无合同/无 `basis` 链时只标记“未关联合同”风险（对齐 05 T4） |
| 建合同项目链接 | `basis`/`coverage` 只做语义关联；`billing` 行才校验 `allocated_amount/allocated_hours ≤ 服务项可用额度` |
| 项目结项 | 释放项目未耗的预占（`quota_used` 不回退，`billing` 行的 `allocated_*` 按实耗结算后置零/关闭） |
| 合同终止 | 关联项目强制挂起，需人工处置 |

### 6.4 与客户域（01）

| 环节 | 联动 |
|---|---|
| 客户 `churned` | 合同批量转 `terminated`（需人工确认，不自动） |
| 客户合并 | 合同 `customer_id` 可随之迁移，但这是 L-04 的**唯一例外**：需 `customer.merge` 权限、同事务审计，且先迁移项目客户链接再迁移合同 |
| 客户等级变化 | 续约提前期随之调整（派生计算，无需改数据） |

---

## 7. DDL 汇总

### 7.1 修复 `contracts`（vendor 采购，D-1/D-2/D-4）

```sql
-- D-1：全局唯一 → 租户内唯一（必须先清洗跨租户重复，见 §9.2）
ALTER TABLE contracts DROP CONSTRAINT IF EXISTS contracts_contract_number_key;
DROP INDEX IF EXISTS contracts_contract_number_key;
ALTER TABLE contracts ADD COLUMN IF NOT EXISTS normalized_contract_number VARCHAR(160);
-- 存量由统一 normalizer 回填（全角转半角、去空白、统一分隔符、字母大写），与新代码同源
CREATE UNIQUE INDEX IF NOT EXISTS uq_contracts_tenant_no
  ON contracts (tenant_id, normalized_contract_number)
  WHERE normalized_contract_number IS NOT NULL;

-- D-2：value 迁移到 NUMERIC
ALTER TABLE contracts ADD COLUMN IF NOT EXISTS amount NUMERIC(18,2);
UPDATE contracts SET amount = ROUND(value::numeric, 2) WHERE amount IS NULL;
ALTER TABLE contracts
  ADD CONSTRAINT ck_contracts_amount_nonneg CHECK (amount IS NULL OR amount >= 0);
-- value 保留只读一版后废弃

-- D-4 + 缺失索引
CREATE INDEX IF NOT EXISTS idx_contracts_tenant_vendor ON contracts (tenant_id, vendor_id);
CREATE INDEX IF NOT EXISTS idx_contracts_tenant_end    ON contracts (tenant_id, end_date)
  WHERE status = 'active';
```

> `vendor_id` 改为 NOT NULL 需先回填/清理孤儿合同（`vendor_id IS NULL` 的存量），见 §9.2。

### 7.2 `support_contracts` 完整增强（替换 02 §12.1，作为最终版）

```sql
ALTER TABLE support_contracts
  -- CC-3 统一有效期字段（start_at/end_at 保留兼容只读）
  ADD COLUMN IF NOT EXISTS effective_from   TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS effective_until  TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS signed_at        TIMESTAMPTZ,
  -- CC-2 金额
  ADD COLUMN IF NOT EXISTS amount           NUMERIC(18,2),   -- 合同授权总额（TCV），非应收/开票基础
  ADD COLUMN IF NOT EXISTS currency         VARCHAR(8),
  ADD COLUMN IF NOT EXISTS contract_type    VARCHAR(32) NOT NULL DEFAULT 'support',
  ADD COLUMN IF NOT EXISTS contract_name    VARCHAR(255),
  -- 层级与续签
  ADD COLUMN IF NOT EXISTS parent_contract_id     BIGINT,
  ADD COLUMN IF NOT EXISTS renewal_of_contract_id BIGINT,
  ADD COLUMN IF NOT EXISTS amendment_count        INT NOT NULL DEFAULT 0,
  -- SLA 与结算引用
  ADD COLUMN IF NOT EXISTS sla_definition_id      BIGINT,
  ADD COLUMN IF NOT EXISTS auto_renew             BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS terminated_at          TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS termination_reason     VARCHAR(255),
  -- CC-5/6/7
  ADD COLUMN IF NOT EXISTS deleted_at       TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS version          BIGINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS created_by       BIGINT,
  ADD COLUMN IF NOT EXISTS updated_by       BIGINT;

-- 有效期回填
UPDATE support_contracts
SET effective_from = COALESCE(effective_from, start_at),
    effective_until = COALESCE(
      effective_until,
      CASE WHEN end_at IS NULL THEN NULL
           ELSE date_trunc('day', end_at) + interval '1 day' END
    )
WHERE effective_from IS NULL OR effective_until IS NULL;

CREATE INDEX IF NOT EXISTS idx_sc_tenant_effective
  ON support_contracts (tenant_id, effective_until)
  WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sc_tenant_type
  ON support_contracts (tenant_id, contract_type, status);

ALTER TABLE support_contracts
  ADD CONSTRAINT ck_sc_amount_nonneg CHECK (amount IS NULL OR amount >= 0);
```

> 存量 `end_at` 若原本表示“结束日当天有效”，回填为次日 00:00（半开区间）；若原本是精确失效时刻，则需先确认口径再迁移，不能直接套用 `date_trunc`。

> `amount` 的定位是**合同授权额度**，用于项目分配上限（M-2）与服务项汇总（M-1），不是财务应收。存量 `amount IS NULL` 的合同在签订前必须补齐金额与币种；`active` 合同建议增加服务层强校验，禁止空金额进入分配。

### 7.3 新表

`contract_service_items`（§4.3）、`contract_amendments`（§4.4）、`contract_usage_ledger`（§4.5）、`contract_project_links`（[02 §12.3](./02-ddl-and-migration.md)）。

建表完成后追加 `contract_project_links.service_item_id` 的复合外键：

```sql
ALTER TABLE contract_project_links
  ADD CONSTRAINT fk_cpl_service_item_tenant
  FOREIGN KEY (tenant_id, service_item_id)
  REFERENCES contract_service_items (tenant_id, id);
```

> 约束创建前先查 `pg_constraint` 判重，避免重复执行报错。

### 7.4 RLS 登记（新增）

`contract_service_items`、`contract_amendments`、`contract_usage_ledger` 必须登记到 `cmd/rls-apply` 与 `internal/schema/tenant_guard.go`，与 `support_contracts` 同作用域。

---

## 8. API 调整

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/contracts` | 统一列表（`domain=procurement\|support\|msp` 切换域） |
| POST | `/api/v1/support-contracts` | 创建（`status='draft'`，校验编号归一化重复） |
| PUT | `/api/v1/support-contracts/:id` | 更新（`active` 后 `amount`/`effective_*` 只读，走 amendments） |
| POST | `/api/v1/support-contracts/:id/sign` | 签订：`draft/pending_sign → active`（校验服务项非空） |
| POST | `/api/v1/support-contracts/:id/terminate` | 终止（双人确认 + 原因必填） |
| GET | `/api/v1/support-contracts/:id/items` | 服务项列表 |
| POST/PUT/DELETE | `/api/v1/support-contracts/:id/items[/:itemId]` | 服务项 CRUD（`active` 后仅允许新增，不改历史） |
| POST | `/api/v1/support-contracts/:id/amendments` | 创建变更单 |
| POST | `/api/v1/support-contracts/:id/amendments/:aid/effect` | 变更生效（重算 `effective_amount`） |
| GET | `/api/v1/support-contracts/:id/balance` | 额度视图（total/reserved/used/remaining/available_to_allocate/available_amount） |
| GET | `/api/v1/support-contracts/:id/projects` | 关联项目（05 正查） |
| POST | `/api/v1/support-contracts/:id/renew` | 续签（生成新合同 + 可选结转项目） |

> **路由兼容**：现有 email-intake 支持合同接口为 `/api/v1/email-intake/support-contracts`，MSP 结算合同为 `/api/v1/sla/contracts`。新 `/api/v1/support-contracts` 与 `/api/v1/contracts` 是新增统一面；上线期必须保留旧路径为兼容别名，并明确权限码：交付域 `support_contract:read|write`，结算域 `sla:contract:read|write`，不得混用。

**新增错误码**

| 码 | HTTP | 场景 |
|---|---|---|
| `CONTRACT_NO_DUPLICATE` | 409 | 归一化合同号租户内重复 |
| `CONTRACT_AMOUNT_IMMUTABLE` | 422 | 已生效合同直接改金额（须走变更单） |
| `CONTRACT_SERVICE_ITEM_REQUIRED` | 422 | 签订时无服务项 |
| `CONTRACT_QUOTA_EXCEEDED` | 409 | 额度扣减超限 |
| `CONTRACT_ALLOCATION_EXCEEDED` | 409 | 项目分配超合同额 |
| `CONTRACT_DOMAIN_MISMATCH` | 422 | 跨域误用（如给内部项目挂外部合同） |
| `CPL_SERVICE_ITEM_MISMATCH` | 422 | billing 行的服务项不属于该合同，或额度单位不匹配 |
| `CONTRACT_USAGE_SOURCE_CONFLICT` | 409 | 同一计量 source 重复提交且载荷不一致 |

---

## 9. 迁移与兼容

### 9.1 迁移顺序（接 [02 §8](./02-ddl-and-migration.md) 的 M9–M13）

| 步骤 | 动作 | 风险 |
|---|---|---|
| M14 | `contract_service_items` / `contract_amendments` / `contract_usage_ledger` 建表 + `service_item_id` 复合外键 | 低 |
| M15 | 存量合同**生成一条汇总服务项**（`item_name='历史合同（未拆分）'`，`amount=COALESCE(support_contracts.amount,0)`），保证签订校验不失败 | 中（需确认口径） |
| M16 | `contracts` 全局唯一 → 租户内唯一 + `normalized_contract_number` 回填（§9.2 先清洗） | **高** |
| M17 | `contracts.value` → `amount NUMERIC` 双写一版后废弃 | 中 |
| M18 | 新表 RLS 登记 | 需人工 |
| M19 | `018`：`contract_sla_snapshots` / `settlement_statements` 建表、RLS、双写 | 中 |
| M20 | `019`：C1–C4 全绿后 DROP 012 两条 customer_read 策略 | **高** |

### 9.2 高风险迁移的两个前置检查

**检查 1：`contracts` 同租户合同号重复**

```sql
SELECT tenant_id,
       upper(regexp_replace(contract_number,'\s+','','g')) AS norm_no,
       COUNT(*) AS cnt
FROM contracts
GROUP BY tenant_id, 2 HAVING COUNT(*) > 1;
```

> 上例是简化版；全角/分隔符归一化必须与代码中的统一 normalizer 一致，建议迁移脚本直接调用同一函数或由应用层预检。

处置：同租户重复 → 人工合并或改号；**跨租户重复是必然的**（D-1），改索引后自然允许，但迁移前必须先确认没有同租户内重复。原查询按 `norm_no` 不分租户会在多租户下返回大量预期内重复，不能作为阻断条件。

**检查 2：`contracts` 孤儿合同（无 vendor）**

```sql
SELECT id, contract_number, tenant_id FROM contracts WHERE vendor_id IS NULL;
```

处置：补 vendor 或标记为「内部合同」后归档，再考虑加 NOT NULL。

### 9.3 兼容策略

- `start_at/end_at`、`value`、自由字符串 `status` 全部**保留只读一版**；
- 服务层双读：优先新字段，缺失回退旧字段；
- 开关 `CONTRACT_MODEL_V2_ENABLED`（按租户灰度）；
- 老 API 响应字段值由新字段映射，前端无需同步改造。

---

## 10. 前端调整

### 10.1 合同列表（`/email-intake/contracts`）

现状列：`ID / 合同号 / 客户 / 分支 / 状态 / 有效期 / 操作`（实测页面为空表）。

**建议列**：`合同号 / 合同名称 / 类型 / 客户 / 等级 / 状态(含 expiring) / 有效期 / 剩余天数 / 合同额 / 已分配 / 已消耗 / 关联项目数 / 操作`

**筛选**：域切换（采购/支持/MSP）、类型、状态、客户等级、到期区间（30/60/90 天）、是否自动续约。

### 10.2 合同详情（三栏）

```
[头部] 合同号 + 类型徽标 + 状态 + 有效合同额 + 额度进度条
[左]  基本信息 / 客户与分支 / 有效期与签订 / SLA / 续签链
[右]  服务项明细（可编辑表格：名称/计量单位/数量/单价/金额/额度/已用）
[下]  Tabs：关联项目 | 变更历史 | 附件 | 操作日志
```

### 10.3 关键交互

1. `active` 后金额字段变为**只读 + 「申请变更」按钮**（引导建变更单）；
2. 保存服务项时实时校验 `SUM(amount) ≤ effective_amount`，超限黄条提示；
3. 额度进度条分三段色：已耗（深）/ 预占（浅）/ 未分配（灰）；
4. 进入客户等级对应续约提前期时，状态列显示 `expiring` 橙色徽标 + 「续签」快捷入口；
5. 续签向导：选择结转项目 → 预览额度继承 → 确认生成新合同。

---

## 11. 反模式清单（评审必查）

| 反模式 | 后果 | 正确做法 |
|---|---|---|
| 再加第四套合同表 | 四套并存，报表永远对不齐 | 先在 §3.2 判定落到哪个域 |
| 合同号用全局唯一 | 多租户必然冲突（D-1） | CC-1 租户内唯一 |
| 金额用 float/JSON 数字 | 分位漂移，对账吵架 | CC-2 `NUMERIC(18,2)` |
| 直接改已生效合同的金额 | 变更无痕，审计失败 | M-3 走 `contract_amendments` |
| 把预占当实耗 | 额度误报用尽 | §5.2 分列展示 |
| 人工改 `quota_used` | 数据不可信 | M-4 仅 `contract_usage_ledger` 同事务回写 |
| 在 `basis`/`coverage` 行上挂 `allocated_*` | 同一项目重复记账 | 只有 `billing` 行携带额度（M-5） |
| 计量 source 重试直接加 `quota_used` | 重复扣减 | 台账唯一源 + 事务回写 |
| 用 `expired` 表示"快到期" | 与事实混淆 | `expiring` 为派生值 |
| 补充协议新建合同号 | 服务项与续签链断裂 | 用 `contract_amendments` |
| 合同期写 `9999-12-31` | 排序/计算异常 | `effective_until IS NULL` |
| 逐行舍入后相加 | 累计分位误差 | §5.5 先加后舍 |

---

## 12. 验收清单

1. 同一合同号可在两个不同租户各自创建成功（D-1 修复）；
2. 金额精度：`0.1 + 0.2 = 0.30`，无浮点尾差（D-2 修复）；
3. `support_contracts` 全部走 `effective_from/effective_until`，旧字段仅只读；
4. 已生效合同改金额被拒绝，走变更单后 `effective_amount` 正确重算；
5. 签订合同时若服务项为空则拒绝；
6. 服务项 `SUM(amount) > effective_amount` 时按配置告警或阻断；
7. 额度视图互洽：数量 `remaining = total - used`、`available_to_allocate = total - used - reserved`；金额 `available_amount = effective_amount - allocated_amount`；`reserved`/`allocated_amount` 只统计 active `billing` 行；
8. 并发扣减额度 10 次后 `quota_used` 精确等于增量总和；同一 source 重复投递不重复扣减；
9. `expiring` 在各等级客户的提前期分别正确（S 90 / A 60 / B·C 30 天）；
10. `effective_until` 之前（不含该时刻）的工单仍计入合同覆盖（§5.4 半开区间）；
11. 续签生成新合同后旧合同 `status='renewed'`，服务项历史保留；
12. `contracts` 同租户重复合同号仍被拒绝，跨租户同号可创建；
13. 采购域/交付域/结算域三套实体互不串用（`CONTRACT_DOMAIN_MISMATCH` 生效）；
14. 跨租户 `contract_id` / `service_item_id` 写入被复合外键拒绝；
15. `basis`/`coverage` 行携带 `allocated_*` 被拒绝，`billing` 行缺少 `service_item_id` 被拒绝。

---

## 13. 对既有文档的变更

| 文档 | 变更 |
|---|---|
| [02 §12.1](./02-ddl-and-migration.md) | 过渡 DDL 已撤销，以本文 §7.2 为唯一权威；`contract_project_links` 的 billing/服务项约束见 02 §12.3 |
| [01 §5.5](./01-customer-master-data-design.md) | SLA 优先级链更新为服务项级最高（§6.2）；合同字段表指向本文 |
| [05](./05-customer-contract-project-linkage.md) | 状态/日期统一到本文 CC-3/CC-4；执行率改按 `effective_amount`；额度只统计 `billing` 行 |
| [03](./03-open-questions.md)、[README](./README.md) | 合同域阶段、迁移 M14–M20 与 G17 的 018/019 顺序纳入路线图 |
| `mkdocs.yml` | 导航新增「合同建模与数据逻辑」 |
