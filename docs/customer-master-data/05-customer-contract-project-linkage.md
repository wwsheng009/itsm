# 05 · 客户 → 合同 → 项目的业务闭环

> 状态：Draft v1.1 · 2026-11-08
> 前置：所有数据不跨租户（见 [04-multi-provider-scenario.md](./04-multi-provider-scenario.md)）
> 本文回答：客户、合同、项目三者的**业务联系**如何建模成闭环，而不是三张孤立的表。

---

## 1. 结论先行：三段链路，一段已通、一段半通、一段断

| 链路 | 现状 | 判定 |
|---|---|---|
| **客户 → 合同** | `support_contracts.customer_id` 为必填边（`edge.From("customer").Ref("contracts").Required()`），已通 | ⚠️ **字段级已通，业务级半通**：没有合同类型、金额、SLA 绑定、续签链；`branch_id` 与 `customer_id` 的同租户/同客户约束本次用复合外键修复 |
| **客户 → 项目** | `projects` 实体**完全没有客户字段**，仅 `department` / `applications` / `tags` 三条边；D-01 明确不设 `customer_tenant_id` | ✅ 已在 [02 §6](./02-ddl-and-migration.md) 用 `project_customer_links` 补齐（同租户闭环） |
| **合同 → 项目** | **完全缺失**。旧草案的单个 `project_customer_links.contract_id` 只能「从项目反查一份合同」，无法回答「这份合同下有哪些项目」「合同额度被哪些项目消耗了」；本修订已将该列移除 | ❌ **断链，本轮补齐** |

> 一句话：**客户是"谁"，合同是"承诺什么"，项目是"怎么兑现"。** 承诺与兑现之间必须有一条可正查、可计量、可对账的链路，否则合同到期时无法回答「履约完了吗」，项目结项时也无法回答「哪份合同可以关闭」。

---

## 2. 业务语义：三角关系的三问三答

### 2.1 为什么不能只靠一张链接表

即使旧草案在 `project_customer_links` 上保留单个 `contract_id`，它也只能回答「**项目**关联了哪个客户、依据哪一份合同」。业务上还缺三个方向：

| 业务问题 | 只有 `project_customer_links` 能否回答 |
|---|---|
| 这份框架合同下开了几个子项目？累计消耗多少额度？ | ❌ 需全表扫描 + 聚合，且无法表达「一份合同多项目、一个项目多合同」 |
| 这个项目同时由框架合同 + 变更单追加供给，如何记账？ | ❌ 单 `contract_id` 无法表达 M:N |
| 合同续签后，在途项目是否自动挂到新合同？ | ❌ 无续签链与项目结转语义 |

### 2.2 三种真实业务形态

| 形态 | 合同 | 项目 | 说明 |
|---|---|---|---|
| **F1 单一支撑** | 1 份交付合同 | 1 个交付项目 | 最常见，一对一 |
| **F2 框架+子项** | 1 份框架合同 | N 个项目（实施/优化/扩容） | 合同是额度池，项目分批消耗 |
| **F3 多合同支撑** | N 份合同（运维+实施+变更） | 1 个项目 | 一个项目横跨多份合同范围 |

→ M:N 是必然，必须独立链接表：**`contract_project_links`**。

### 2.3 四层语义（决定字段设计）

```
客户 (谁被服务)
  └─ 合同 (承诺边界：范围 / 期限 / SLA / 金额)
        └─ 项目 (兑现动作：交付 / 实施 / 优化)
              └─ 工时 / 里程碑 / 交付物 (实际消耗)
```

- **合同是项目的"授权依据"**：无合同（或框架授权）的交付项目属于**风险项**，应在项目列表提示"未关联合同"。
- **项目是合同的"履约凭证"**：合同履约率 = 已结项项目数 / 合同内计划项目数（或金额口径）。
- **客户是二者的"归属锚点"**：合同与项目上的客户必须一致，这是本设计的**第一条铁律**。

---

## 3. 核心表：`contract_project_links`

> **DDL 以 [02 §12.3](./02-ddl-and-migration.md) 为唯一权威**。本节只保留业务语义，避免两份建表语句漂移。

| 字段 | 语义 |
|---|---|
| `relation_type='basis'` | 合同是本项目的立项依据（主关系，每项目最多一条） |
| `relation_type='coverage'` | 合同覆盖本项目的运维/支持范围 |
| `relation_type='billing'` | **额度分配行**，本项目消耗该合同的金额/数量；只有该行可携带 `allocated_*` |
| `change` | 变更单追加（合同变更驱动项目范围变化） |
| `service_item_id` | `billing` 行必填，指向合同内具体服务项 |
| `allocated_amount` / `allocated_hours` | 金额额度 / 数量额度（工时、工单数、设备数、项目数，按服务项 `billing_unit` 解释） |
| `status` | `active / ended / cancelled`；`ended` 表示链路已结束，`cancelled` 表示撤销 |

**为什么要 `billing` 行**
不是做财务核算（那是 ERP 的职责），而是为了回答运营问题：
- 框架合同 100 万，三个项目分别分配 40/35/15 万 → 剩余 10 万可再开项（合同执行率可视）；
- 项目结项时反查“这份合同已被消耗多少”，避免超额承诺；
- 金额和数量分开统计，`billing` 行只承载一种或两种额度，不会与 `basis/coverage` 的语义关系重复记账。

> 明确非目标：不做应收、不做开票、不做税率。金额仅为**授权额度的分配视图**。

---

## 4. 五条归属一致性铁律

这是本轮最重要的一组约束——现有模型存在**真实完整性漏洞**：`support_contracts` 的 `customer_id` 与 `branch_id` 是两条独立边，数据库层完全可能存出「合同属于客户 A，但引用客户 B 的分支」。

### 铁律 L-01：合同的分支必须属于该合同的客户

```sql
-- 服务层校验（写入 support_contracts.branch_id 前必须执行）
-- 若 branch_id 非空，则 customer_branches.customer_id 必须等于 support_contracts.customer_id
SELECT 1 FROM customer_branches
WHERE id = $branchId AND tenant_id = $tenantId
  AND customer_id = $customerId AND deleted_at IS NULL;
```

**现状风险**：迁移 SQL 中 `branch_id` 仅 `REFERENCES customer_branches(id)`，无复合约束。修复方案（P1）：

```sql
-- 方案 A（推荐）：复合外键，数据库层强制
ALTER TABLE support_contracts
  ADD CONSTRAINT fk_contract_branch_tenant_customer
  FOREIGN KEY (tenant_id, customer_id, branch_id)
  REFERENCES customer_branches (tenant_id, customer_id, id);
```

> 父端唯一索引与合同客户复合外键见 [02 §7.2](./02-ddl-and-migration.md)。执行前必须先清洗存量不一致数据（见 §8.2 排查 SQL）。复合外键在 `branch_id` 为 NULL 时不生效（MATCH SIMPLE 语义），正好符合“客户级合同”的要求。

### 铁律 L-02：项目的客户必须与合同的客户一致

`contract_project_links` 两端各自指向一份合同和一个项目，二者背后的客户必须相同：

```sql
-- 校验（写入前）
SELECT
  (SELECT customer_id FROM support_contracts WHERE id = $contractId AND tenant_id = $tenantId) AS contract_customer,
  (SELECT customer_id FROM project_customer_links
     WHERE project_id = $projectId AND tenant_id = $tenantId AND is_primary AND deleted_at IS NULL) AS project_customer;
-- 两者必须相等，且均非空；否则拒绝并返回 CPL_CUSTOMER_MISMATCH
```

> 若项目尚未建立客户链接，**先建项目客户链接，再建合同项目链接**（顺序不可颠倒），否则出现"合同挂在一个没有客户的项目上"。

### 铁律 L-03：项目类型必须是 delivery 才能挂客户与合同

`projects.type` 现有取值 `internal | improvement | delivery`：

| 项目类型 | 可否关联客户/合同 | 说明 |
|---|---|---|
| `delivery` | ✅ 必须关联客户 | 对外交付项目，无客户属数据缺陷 |
| `improvement` | ⚠️ 可选 | 内部优化，若为某客户专项则可关联 |
| `internal` | ❌ 禁止 | 纯内部项目，关联客户会污染客户视角指标 |

服务层拒绝 `internal` 项目建 `project_customer_links`，返回 `PCL_PROJECT_TYPE_INVALID`。

### 铁律 L-04：合同客户归属不可变更

一旦合同下已挂项目，`support_contracts.customer_id` **不可修改**。改客户在业务上等于“换了一份合同”，应新建合同并把旧合同置为 `terminated`（人工）或 `expired`（自然到期）。

```sql
-- 服务层：更新 customer_id 前检查
SELECT COUNT(*) FROM contract_project_links
WHERE contract_id = $id AND tenant_id = $tenantId AND deleted_at IS NULL;
-- > 0 则拒绝，返回 CONTRACT_CUSTOMER_IMMUTABLE
```

> **唯一例外：客户合并**。合并事务需要 `customer.merge` 权限与完整审计，并按“项目客户链接 → 合同客户 → 其他关联”的顺序执行；除此之外，任何直接改合同客户都按 L-04 拒绝。

### 铁律 L-05：billing 行必须指向同一合同的服务项

`contract_project_links.relation_type='billing'` 时，`service_item_id` 必填，且必须满足：

1. 服务项属于同一 `tenant_id` 与同一 `contract_id`；
2. `allocated_hours` 的计量单位与 `contract_service_items.billing_unit` 一致；
3. 同一合同+项目+服务项最多一条 `active` 的 `billing` 行。

违反时返回 `CPL_SERVICE_ITEM_MISMATCH`。

---

## 5. 合同类型 × 项目类型 的合法组合矩阵

| 合同类型 | 可支撑项目类型 | 是否可多项目 | 典型场景 |
|---|---|---|---|
| `framework`（框架） | `delivery` / `improvement` | ✅ 可多 | 年度框架，按需开实施/优化项目 |
| `project`（项目合同） | `delivery` | 通常 1 : 1 | 一次性交付 |
| `support`（运维支持） | 一般不挂项目，`relation_type='coverage'` | ✅ 可多 | 项目结项后转运维，合同覆盖多个已交付系统 |
| `maintenance`（维保） | `coverage` | ✅ 可多 | 存量系统维保 |
| `subscription`（订阅） | 通常不挂项目 | — | SaaS 订阅，走租户而非项目 |

> 落地为服务层软校验（允许例外 + 告警日志），不做数据库硬约束——业务上确实存在非常规组合。

---

## 6. 三条正查路径（这是断链修复的核心价值）

### 6.1 客户 → 全部（客户 360 的骨架）

```sql
-- 某客户在当前租户下的合同与项目全景
SELECT 'contract' AS kind, c.id, c.contract_number AS code, c.status,
       c.effective_from, c.effective_until, NULL::int AS project_id
FROM support_contracts c
WHERE c.tenant_id = $1 AND c.customer_id = $2 AND c.deleted_at IS NULL
UNION ALL
SELECT 'project', p.id, p.code, p.status, p.start_date, p.end_date, p.id
FROM project_customer_links l
JOIN projects p ON p.id = l.project_id AND p.tenant_id = l.tenant_id AND p.deleted_at IS NULL
WHERE l.tenant_id = $1 AND l.customer_id = $2 AND l.deleted_at IS NULL;
```

### 6.2 合同 → 项目（本轮新增，原来无法回答）

```sql
SELECT p.id, p.code, p.name, p.status, p.progress, p.health,
       l.relation_type, l.service_item_id, l.allocated_amount,
       l.allocated_hours, l.is_primary
FROM contract_project_links l
JOIN projects p ON p.id = l.project_id AND p.tenant_id = l.tenant_id AND p.deleted_at IS NULL
WHERE l.tenant_id = $1 AND l.contract_id = $2 AND l.deleted_at IS NULL
  AND l.status = 'active'
ORDER BY l.is_primary DESC, p.start_date;
```

### 6.3 合同执行率（额度视角）

```sql
WITH eff AS (
  SELECT c.id, c.contract_number,
         c.amount + COALESCE(SUM(a.amount_delta), 0) AS effective_amount
  FROM support_contracts c
  LEFT JOIN contract_amendments a
    ON a.contract_id = c.id AND a.tenant_id = c.tenant_id
   AND a.status = 'effective'
  WHERE c.tenant_id = $1 AND c.id = $2
  GROUP BY c.id, c.contract_number, c.amount
)
SELECT c.id, c.contract_number, c.effective_amount,
       COALESCE(SUM(l.allocated_amount), 0) AS allocated_amount,
       c.effective_amount - COALESCE(SUM(l.allocated_amount), 0) AS remaining_amount,
       ROUND(COALESCE(SUM(l.allocated_amount), 0) / NULLIF(c.effective_amount, 0) * 100, 1)
         AS allocated_pct
FROM eff c
LEFT JOIN contract_project_links l
  ON l.contract_id = c.id AND l.tenant_id = $1
 AND l.relation_type = 'billing' AND l.status = 'active' AND l.deleted_at IS NULL
GROUP BY c.id, c.contract_number, c.effective_amount;
```

> 展示口径：`allocated_pct > 100` 标红（超额承诺），`remaining_amount < 0` 需审批例外。**执行率不是实耗率**；实耗看 `contract_usage_ledger` / `quota_used`。

---

## 7. 时序联动：续签、到期、在途项目

### 7.1 三个时间锚点

| 锚点 | 来源 | 用途 |
|---|---|---|
| 合同有效期 | `support_contracts.effective_from` / `effective_until`（半开区间） | 驱动续约提醒 |
| 项目周期 | `projects.start_date` / `end_date` / `actual_start` / `actual_end` | 驱动交付看板 |
| 链路有效期 | `contract_project_links.effective_from` / `effective_to` | 支持"合同续签后项目结转" |

### 7.2 五类时序冲突与处置

| 冲突 | 判定 | 处置 |
|---|---|---|
| **T1 项目超出合同期** | `project.end_date >= contract.effective_until` | 立项时告警，需填 `note` 说明；结项前必须续签或缩范围 |
| **T2 合同到期但项目在途** | `contract.effective_until <= now()` 且项目 `status IN (planning,active)` | 生成续约待办（S 级客户提前 90 天，A 级 60 天，其余 30 天）；合同到期后项目仍可继续但标"合同已过期"红标 |
| **T3 续签后项目未结转** | 旧合同 `status='renewed'`，但其项目仍挂旧合同 | 续签向导中提供"结转在途项目到新合同"一键操作（改 `contract_id` + 写 `effective_from`） |
| **T4 项目先于合同立项** | 项目存在但无 `basis` 关系 | 项目列表标记"未关联合同"，并在合同创建时提示可选在途项目 |
| **T5 项目已结项但合同未消耗完** | 项目 `status='completed'`，合同 `available_to_allocate > 0` | 提示“合同额度未用尽”，供续作/退款决策（不做自动退款） |

### 7.3 续签链（`renewal_of_contract_id`）

```
合同 C1 (2025-01-01 ~ 2025-12-31, renewed)
  └─ C2 (2026-01-01 ~ 2026-12-31, active, renewal_of_contract_id = C1)
       └─ C3 (...)
```

**规则**
1. 续签必须指向同客户、同租户的上一份合同（软校验）；
2. 续签时 `contract_project_links` **不自动迁移**（避免账期混乱），由用户在向导中显式选择结转；
3. 结转时旧链路 `status='ended'`、`effective_to=旧合同 effective_until 的结束日`，新建链路指向新合同（保留历史可追溯）。

---

## 8. 存量数据排查与回填

### 8.1 回填顺序（接 [02 §8](./02-ddl-and-migration.md)）

| 步骤 | 动作 | 幂等 |
|---|---|---|
| M9 | 补 `support_contracts` 增强列（**以 [06 §7.2](./06-contract-modeling-design.md) 为最终版**） | 幂等 |
| M10 | 存量合同 `contract_type` 回填：有 `effective_until` 且跨度 ≥ 330 天 → `framework`，否则 `support` | 幂等 |
| M11 | 清理 L-01 违规数据（分支不属于客户） | 需人工确认 |
| M12 | 建复合外键 `fk_contract_branch_tenant_customer` | 幂等（先查 `pg_constraint`） |
| M13 | `contract_project_links` 建表 + 存量猜测回填（**不自动建链**，仅生成建议清单） | 幂等 |
| M14–M20 | 服务项/变更单/用量台账/`contracts` 修复/快照与 G17 收回 | 见 [06 §9.1](./06-contract-modeling-design.md) |

> **重要**：存量合同与项目之间**不做自动推断关联**。业务上猜测关联会导致财务口径错乱，只生成"建议关联清单"供人工确认。

### 8.2 L-01 违规数据排查 SQL

```sql
-- 找出「合同的分支不属于该合同客户」的所有存量异常
SELECT c.id AS contract_id, c.contract_number, c.customer_id,
       c.tenant_id, c.branch_id, b.tenant_id AS branch_tenant_id,
       b.customer_id AS branch_owner_customer_id
FROM support_contracts c
JOIN customer_branches b ON b.id = c.branch_id
WHERE c.branch_id IS NOT NULL
  AND (b.tenant_id <> c.tenant_id OR b.customer_id <> c.customer_id);
```

处置：逐条人工确认是「合同客户填错」还是「分支挂错客户」，修正后再建外键。

---

## 9. 状态机联动

### 9.1 合同状态与项目状态

| 合同 `status` | 允许的项目操作 | 说明 |
|---|---|---|
| `draft` / `pending_sign` | ❌ 不允许建 `basis` / `billing` 链路 | 合同尚未生效 |
| `active` | 全部 | 正常 |
| `expiring`（派生） | 同 `active` + 续约引导 | 未入库 |
| `expired` | 只读 + 可结项，**禁止新建 basis 链路** | 需先续签 |
| `renewed` | 只读 + 可结项 | 指向新合同，历史保留 |
| `terminated`（提前终止） | 关联项目强制进入 `suspended`，需人工处置 | 高危，双人确认 |

### 9.2 项目状态对合同的反向影响

| 项目事件 | 对合同的影响 |
|---|---|
| 项目结项（`completed`） | 合同详情页履约率 +1；若全部 basis 项目结项，提示"合同可评估关闭" |
| 项目取消（`cancelled`） | 该链路 `status='cancelled'`，释放 `billing` 行的 `allocated_amount/allocated_hours` |
| 项目健康度 `red` | 合同详情页标记"履约风险"，进入客户健康度计算 |

### 9.3 与客户生命周期的联动

| 客户 `lifecycle_stage` | 合同 | 项目 |
|---|---|---|
| `renewal`（续约中） | 生成续约任务，锁定 `renewal_of_contract_id` | 在途项目正常，新建需审批 |
| `churned`（流失） | 全部转 `terminated`（人工确认） | 全部项目转 `suspended` 或 `cancelled` |
| `archived`（归档） | 只读 | 只读 |

---

## 10. API 设计

### 10.1 合同侧

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/customers/:id/contracts` | `customer.read` | 客户合同列表（含执行率） |
| POST | `/api/v1/customers/:id/contracts` | `support_contract.write` | 新建合同（自动带 `customer_id`） |
| GET | `/api/v1/support-contracts/:id` | `support_contract.read` | 合同详情（含项目数、执行率） |
| PUT | `/api/v1/support-contracts/:id` | `support_contract.write` | 更新（`customer_id` 变更受 L-04 限制） |
| GET | `/api/v1/support-contracts/:id/projects` | `support_contract.read` | **合同 → 项目正查**（新增） |
| POST | `/api/v1/support-contracts/:id/projects` | `support_contract.write` + `project.read` | 挂项目（校验 L-02/L-05） |
| POST | `/api/v1/support-contracts/:id/renew` | `support_contract.write` | 续签（生成新合同 + 可选结转） |
| GET | `/api/v1/support-contracts/:id/execution` | `support_contract.read` | 执行率（额度/项目数双口径） |

### 10.2 项目侧

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/projects/:id/customers` | 项目关联客户 |
| GET | `/api/v1/projects/:id/contracts` | 项目关联合同（含分配额） |
| POST | `/api/v1/projects/:id/contract-links` | 建合同链接（校验 L-02 / L-03） |

### 10.3 请求示例

```json
POST /api/v1/support-contracts/88/projects
{
  "projectId": 301,
  "relationType": "billing",
  "serviceItemId": 42,
  "allocatedAmount": 400000,
  "effectiveFrom": "2026-01-01",
  "note": "一期实施"
}
```

> `basis` 关系单独建一条（`isPrimary=true`），不携带 `allocatedAmount`；额度统一落在 `billing` 行。

### 10.4 新增错误码

| 码 | HTTP | 场景 |
|---|---|---|
| `CONTRACT_BRANCH_CUSTOMER_MISMATCH` | 422 | L-01：分支不属于合同客户 |
| `CPL_CUSTOMER_MISMATCH` | 422 | L-02：项目客户与合同客户不一致 |
| `PCL_PROJECT_TYPE_INVALID` | 422 | L-03：internal 项目禁止关联客户 |
| `CONTRACT_CUSTOMER_IMMUTABLE` | 422 | L-04：已挂项目的合同禁止改客户 |
| `CONTRACT_ALLOCATION_EXCEEDED` | 409 | 分配额超出合同有效金额（硬阻断） |
| `CONTRACT_EXPIRED_FOR_NEW_LINK` | 422 | 合同已过期/终止仍尝试建 basis 链路 |
| `CPL_SERVICE_ITEM_MISMATCH` | 422 | L-05：billing 服务项不属于合同或单位不匹配 |

---

## 11. 前端联动设计

### 11.1 客户详情页（`/master-data/customers` 的详情抽屉）

```
[Tabs] 概览 | 合同 | 项目 | 工单 | 资产 | 变更历史

【合同 Tab】
  合同号 | 类型 | 状态 | 有效期 | 金额 | 已分配 | 剩余 | 关联项目数 | 操作
  - 「关联项目数」可点击 → 弹出该项目清单（正查）
  - 行展开显示该合同下的项目列表 + 分配额

【项目 Tab】
  项目编码 | 名称 | 类型 | 状态 | 进度 | 健康度 | 关联合同 | 分配额
  - 「关联合同」为空时显示黄色警示「未关联合同」
```

### 11.2 合同详情页

```
[头部] 合同号 + 类型徽标 + 状态 + 分配率进度条（allocated_pct）
[左] 基本信息 / 客户信息（可跳客户）/ 分支范围 / SLA
[右] 关联项目列表（正查）+ 额度分配概览
[操作] 续签 / 挂项目 / 终止 / 导出
```

### 11.3 项目详情页（项目管模块内）

```
[客户信息卡] 客户名称 + 等级 + 客户经理（来自 project_customer_links）
[合同信息卡] 合同号 + 类型 + 有效期 + 本项目分配额 + 剩余可分配额度
[冲突提示] 项目结束日期 > 合同到期日 → 红条「项目周期超出合同期限」
```

### 11.4 交互规则

1. 客户详情 → 合同 → 项目，三级可下钻，面包屑可回溯；
2. 建合同项目链接时，客户自动带入且**不可改**（只能改项目）；
3. 分配额合计超出 `effective_amount` 时**硬阻断**，不得只弹确认；需先走变更单增额；
4. 项目周期与合同期冲突时，保存前提示但允许保存（记录 `note`）。

---

## 12. 反模式清单（评审必查）

| 反模式 | 后果 | 正确做法 |
|---|---|---|
| 在 `projects` 加 `customer_id` / `contract_id` 列 | 一项目多合同无法表达；跨租户风险 | 用 `project_customer_links` + `contract_project_links` |
| 用 `projects.name` 或 `code` 里塞客户名 | 无法统计，改名即断 | 走正式链接表 |
| 合同客户与项目客户不一致却不校验 | 客户视角数据串台 | 铁律 L-02 |
| 合同改客户以"复用"历史项目 | 履约历史污染 | 铁律 L-04，新建合同 |
| 自动推断存量合同↔项目关联 | 财务口径错乱 | 只生成建议清单，人工确认 |
| 续签时自动把所有项目迁到新合同 | 账期混乱、审计不可追溯 | 显式结转 + 保留旧链路历史 |
| 把 `allocated_amount` 当应收/开票依据 | 与 ERP 双账 | 明确定位为"授权额度分配视图" |
| 允许 `internal` 项目关联客户 | 客户指标被内部项目污染 | 铁律 L-03 |

---

## 13. 验收清单

1. 合同可正查项目清单（含分配额），接口返回与手工核算一致；
2. 客户详情「合同」「项目」两个 Tab 可三级下钻互跳；
3. 建「分支不属于该客户」的合同被拒绝（L-01，含复合外键生效验证）；
4. 建「项目客户 ≠ 合同客户」的链接被拒绝（L-02）；
5. `internal` 项目无法关联客户（L-03）；
6. 已挂项目的合同修改客户被拒绝（L-04）；
7. 合同分配率 `allocated_pct` 按 `effective_amount` 计算正确，超额承诺标红；
8. 合同续签后可选结转在途项目，旧链路保留 `ended` 历史；
9. 项目结束日期晚于合同到期日时有明确冲突提示；
10. 客户 `churned` 后其合同与项目进入只读/挂起流程；
11. 存量 L-01 违规数据排查 SQL 返回 0 条后方可上生产外键；
12. `billing` 行缺少服务项或服务项跨合同/跨单位被拒绝（L-05）。

---

## 14. 对既有文档的变更

| 文档 | 变更 |
|---|---|
| [01 §5.4](./01-customer-master-data-design.md) | 项目关联补「合同正查」交叉引用 |
| [01 §5.5](./01-customer-master-data-design.md) | 合同增强补 DDL 位置与三方闭环引用 |
| [02 §12](./02-ddl-and-migration.md) | `support_contracts` DDL 以 06 §7.2 为权威；`contract_project_links` 增加 `service_item_id`、billing 约束与复合外键 |
| [02 §8](./02-ddl-and-migration.md) | 回填表新增 M9–M13；合同扩展的 M14–M20 见 06 §9.1 |
| [06](./06-contract-modeling-design.md) | 状态/日期/金额/额度/台账口径的唯一权威 |
| [README](./README.md) | 文档结构新增本文 |
| `mkdocs.yml` | 导航新增「合同与项目闭环」 |
