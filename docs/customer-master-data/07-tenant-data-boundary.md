# 07 · 租户数据边界：业务数据不跨租户（硬约束）

> 状态：Draft v1.0 · 2026-11-06
> 决策来源：用户两次明确拍板 —— **「客户不跨租户共享，所有数据都不跨租户」「业务数据不会跨租户」**
> 本文用途：把这条原则落成**可审计、可验收、可回滚**的硬边界，并清除既有实现中的违规构造。

---

## 1. 边界的精确定义

### 1.1 什么算「业务数据」（不得跨租户）

| 类别 | 举例 |
|---|---|
| 客户主数据 | `service_customers`、`customer_branches`、`customer_contacts`、`customer_relationships`、`customer_tenant_links` |
| 合同与商务 | `support_contracts`、`contract_service_items`、`contract_amendments`、`contract_project_links`、`contracts`（采购）、**`msp_contracts`**、`msp_contract_sla_bindings` |
| 服务记录 | `tickets`、`incidents`、`problems`、`changes`、`service_requests`、`email_conversations` |
| 配置与资产 | `configurationitems`、`assets`、`applications` |
| 项目与交付 | `projects`、`project_members`、`project_customer_links`、工时、里程碑 |
| SLA 结果 | `sladefinitions`、SLA 计量、违约记录、结算快照 |
| 组织与人员 | `users`、`groups`、`roles`、`user_tenant_memberships`（各自租户内的部分） |
| 附件与文档 | `documents`、合同扫描件、报障邮件正文 |

### 1.2 什么不算业务数据（允许的平台级信息）

| 类别 | 举例 | 为什么允许 |
|---|---|---|
| 租户拓扑元数据 | `tenants.id`、`type`、`parent_tenant_id`、`msp_provider_id`、`status`、`quota`、`expires_at` | 描述"租户是什么"，不含业务内容；平台运营必需 |
| 访问授权记录 | `msp_allocations`（provider 用户 → customer 租户的授权） | **授权**不是**数据**；它只决定"能否进入目标租户"，进入后读的仍是目标租户内数据 |
| 跨租户弱引用 | `customer_tenant_links.linked_tenant_id`、`customer_external_ids.external_id`、`contract_project_links.project_id` | 只存 ID / 字符串，不携带对方任何业务字段 |
| 平台共享字典 | 标签云、市场模板、AI Prompt 模板 | 平台级配置，非某租户业务产出 |

> **判定口诀**：如果一条记录的内容是"某个租户做了什么业务"，就是业务数据；如果只是"租户这个容器本身的属性"，就不是。

---

## 2. 四条硬边界铁律（BD-1 ~ BD-4）

| 编号 | 铁律 | 检查方式 |
|---|---|---|
| **BD-1** | 任何业务数据表的 RLS 策略，**谓词只能是 `tenant_id = 当前租户`** | 全表扫描 `pg_policy`，出现 `customer_tenant_id = ctx` 或任何非 `tenant_id` 谓词的 `SELECT` 放宽即违规 |
| **BD-2** | **禁止**通过 RLS 放宽实现"跨租户可见" | 同上；代码评审禁止新增含两个不同租户列的 policy |
| **BD-3** | 跨租户需要"看到对方结论"时，用**下发/快照**，不用**跨租户读** | 结算、报表类需求必须落"本租户内的一份副本" |
| **BD-4** | 跨租户引用一律弱引用 + 服务层授权，**不建跨租户外键** | `information_schema` 无跨租户 FK；对齐 IP-P0-4 |

### 2.1 BD-1 的可执行审计 SQL

```sql
-- 列出所有含"非 tenant_id 单列谓词"的 SELECT 策略（潜在违规）
SELECT n.nspname AS schema,
       c.relname  AS table_name,
       p.polname  AS policy_name,
       pg_get_expr(p.polqual, p.polrelid) AS using_expr,
       pg_get_expr(p.polwithcheck, p.polrelid) AS check_expr
FROM pg_policy p
JOIN pg_class c ON c.oid = p.polrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND p.polcmd IN ('r', '*')          -- SELECT / ALL
  AND (
        pg_get_expr(p.polqual, p.polrelid) NOT LIKE '%tenant_id = get_current_tenant_id()%'
     OR pg_get_expr(p.polqual, p.polrelid) LIKE '%customer_tenant_id%'
     OR pg_get_expr(p.polqual, p.polrelid) LIKE '%provider_tenant_id%'
      )
ORDER BY 2, 3;
```

> **预期结果：0 行。** 非 0 即说明存在业务数据跨租户可见，按 §4 处置。

---

## 3. 现有构造逐项审计

| # | 构造 | 判定 | 依据 |
|---|---|---|---|
| 1 | `tenants.msp_provider_id` / `parent_tenant_id` | ✅ 合规 | 租户拓扑元数据（§1.2），非业务数据 |
| 2 | `msp_allocations`（provider_user_id + customer_tenant_id） | ✅ 合规 | 访问授权记录；进入后读目标租户内数据，不做跨租户 JOIN |
| 3 | `customer_tenant_links.linked_tenant_id` | ✅ 合规 | 弱引用，只存租户 ID，不读对方字段 |
| 4 | `customer_external_ids.external_id` | ✅ 合规 | 字符串引用，不携带对方业务内容 |
| 5 | `contract_project_links.project_id` | ✅ 合规 | 同租户弱引用 |
| 6 | `msp_contracts.customer_tenant_id` 字段本身 | ⚠️ 存疑 | 存 ID 可接受（弱引用），但需确认业务用途；见 §4.1 |
| 7 | **`msp_contracts` RLS `msp_contract_customer_read`** | ❌ **违规** | `FOR SELECT USING (customer_tenant_id = get_current_tenant_id())` → 客户租户可直读 provider 的合同业务行 |
| 8 | **`msp_contract_sla_bindings` RLS `msp_contract_binding_customer_read`** | ❌ **违规** | 同上，通过 EXISTS 子查询继承放宽 |

### 3.1 违规证据（源码实测）

```sql
-- itsm-backend/database/rls/migrations/012_msp_contract_policies.sql:54-58
p := 'msp_contract_customer_read';
CREATE POLICY %I ON %I FOR SELECT
  USING (customer_tenant_id = get_current_tenant_id())
```

```sql
-- 同文件:81-88
p := 'msp_contract_binding_customer_read';
CREATE POLICY %I ON %I FOR SELECT USING (EXISTS (
  SELECT 1 FROM msp_contracts c
  WHERE c.id = %I.contract_id
    AND c.customer_tenant_id = get_current_tenant_id()))
```

文件头注释亦明写：「customer 端只读授信（结算报表/下发视图的读面），写路径 provider-only」（第 4–5 行）。

**这直接违反 BD-1 / BD-2**：客户租户读到的是 provider 租户里的合同行（含 `name`、`penalties`、`settlement_cycle`、`currency`、金额相关条款），属于**跨租户读取业务数据**，而非"进入目标租户后再读"。

### 3.2 设计意图溯源（为什么当初会这么写）

> 结论先行：**当初的意图并不是"故意跨租户"，而是把 `customer_tenant_id` 字段当成了"显式授权凭据"**，沿用 D-SLA-1 的「跨租户只读授信通道」心智（canon I7/B1：跨租户仅经显式授权通道）。但凭据选错了、粒度放大了，于是**从"授权通道"退化成"跨租户读放宽"**。

#### 3.2.1 方案文本中声明的三个用途

| # | 声明的用途 | 出处 | 实测是否成立 |
|---|---|---|---|
| 1 | **L1 SLA 匹配**——客户租户建单时，读自己那份生效合同，取合同绑定的 SLA 定义集 | `service/ticket_sla_service.go:682-724`（`matchContractDefinition`，注释明写「customer ctx 下经 RLS 012 授信策略读取（`msp_contract_customer_read`）」） | ✅ **真实依赖**（唯一） |
| 2 | **下发视图 / provided 读面**——客户在 SLA 列表里看到"服务商下发了哪些条款" | `012` 头注释 L4-5；方案 IM-15「customer 读面（provided 视图、P2 结算报表）需要合同链可见」 | ⚠️ **尚未实现**（预留）。`router/sla_routes.go:83-92` 的 `/sla/contracts` 四端点全部挂在 `MSPMiddleware` 下属 provider 面；前端 `itsm-frontend/src` 检索 `msp-contracts` / `mspContract` **零命中**，仅 `sla-api.test.ts` 有 provider 侧 API 用例 |
| 3 | **结算报表读面** | `012` 头注释 L4-5 | ❌ **不成立**。`GET /sla/settlement-report` 同样挂在 `MSPMiddleware` + `RequireMSPPermission("sla","settlement:read")` 下，执行上下文是 **provider 租户**，`msp_contract_provider_all`（`tenant_id = ctx`）已完全覆盖，**不需要** `customer_read` |

> 也就是说：**三条理由里只有第 1 条是真的，而第 1 条恰好是最容易被合规方式替代的一条**（见 §4.2 结算快照 + L1 改读快照/工单内快照字段）。

#### 3.2.2 机制上为什么会退化成"放宽"

| 退化环节 | 方案原本的模式（D-SLA-1，v1.9） | `msp_contracts` 的实际实现 |
|---|---|---|
| 授权凭据 | **独立的授权行** `sla_definition_grants`（`is_active`、`valid_from/valid_until`、`ticket_type_id`） | **合同行上的一个字段** `customer_tenant_id` |
| 触发条件 | 先校验 provider 归属 + 写权限（授权三件套），且授权可**单独撤销** | 只要 `customer_tenant_id` 等于你，就**永久可读** |
| 可见范围 | 仅 `sla_definitions` 一行的**条款内容**（客户本来就该知道自己的 SLA） | 合同**整行**：`name`、`penalties`（违约率/上限）、`settlement_cycle`、`currency`、`contract_no`、`effective_*` |
| 生命周期 | `is_active` / 有效期失效即不可见 | **合同 `status=terminated` 或客户与 provider 解约后，历史行仍可读**（策略里没有任何状态或 relations 校验） |
| 写入保护 | `WITH CHECK (tenant_id = 当前租户)` 显式锁写 | 无 `WITH CHECK`（`FOR SELECT` 策略本身不涉及写，这一点是对的） |

**四条退化原因的因果链**：

1. **省一次实现**：`matchContractDefinition` 是 ent 直接查询、跑在 customer ctx 里。若不放宽 RLS，就必须改走系统特权上下文（`common/tenantctx`）或独立 provided 端点——工程量更大。选择"直接在 RLS 上加一条 SELECT 策略"是**最小改动路径**。
2. **误把"外键字段"当"授权凭据"**：`customer_tenant_id` 的业务语义是"这份合同服务哪个客户"（provider 的内部业务字段），被顺手复用成了访问控制谓词。**数据字段与授权凭据被混同**，这是根因。
3. **沿用"授权通道"叙事，但省掉了凭据独立性**：方案对 SLA 定义要求"grant 行即授权凭据、可独立撤销、有有效期"；对合同却只留了"行上有个我的 ID"。**同一方案内两处通道模式不一致**。
4. **注释自证了用途错配**：`012` 头注释同时写了"结算报表"（provider 面）和"下发视图"（未实现），说明写迁移时**没有回到实际调用方核对**，理由是先写后找的。

#### 3.2.3 为什么这条"设计目的"不能作为保留理由

即便承认动机是"给客户读自己合同的 SLA 条款"，**手段与目的之间也不成立**：

- **目的可被满足得更窄**：客户需要的只是「我的工单适用哪套时限/罚则」。这已经在工单行内以 `sla_definition_id` + deadline 快照落库（`sla-contract-multitenant-plan` §21-222：「deadline 快照在工单行内，判违约不回读 provider 行」），**判违约根本不需要回读 `msp_contracts`**。
- **剩余需求是"展示"而非"计算"**：展示走 `settlement_statements` 快照或 provided 端点即可，无需行级放宽。
- **粒度不可收敛**：`FOR SELECT USING (customer_tenant_id = ctx)` 是**整行、永久、无状态校验**的可见性，无法表达"只看条款不看罚则""解约后不可见"这类必然要求，只能靠应用层过滤——而应用层过滤在 RLS 放宽的前提下**不再有兜底**。

**因此 §4 方案 B 的判断不变**：这条策略应收回，用途 1 改由结算快照 / 工单内快照承载，用途 2 若要做则建独立 provided 端点（不复用行级策略）。

---

## 4. 处置方案

### 4.1 `msp_contracts` 的三条路线

| 方案 | 做法 | 评价 |
|---|---|---|
| A. 维持现状 | 保留 customer_read 策略 | ❌ 与用户裁决直接冲突，否决 |
| **B. 收回跨租户读 + 结算快照下发（推荐）** | 删除 customer_read 策略；provider 在结算时生成**结算单快照**，通过"下发"动作在**客户租户内落一份副本**；客户读自己租户的副本 | ✅ 满足 BD-1/BD-3；保留客户可见性 |
| C. 客户侧自建合同副本 | 客户租户独立录入一份"我方承诺"合同 | ⚠️ 双份维护易漂移；仅在客户需要自主管理时采用 |

### 4.2 方案 B 的落地结构

```
provider 租户 (tenant_id = P)
├── msp_contracts            ← 仅 provider 可读写（BD-1 合规）
├── msp_contract_sla_bindings← 仅 provider 可读写
└── 结算动作（provider 发起）
        │  生成不可变快照（含期间、指标、罚则、金额）
        ▼
customer 租户 (tenant_id = C)
└── settlement_statements    ← 客户只读自己租户内的这一份（BD-1 合规）
     └── settlement_statement_lines
```

**关键点**：跨租户那一步是**一次显式的「写入对方租户」动作**（provider 有写权限时才可下发），落库后客户读的是**自己租户的行**，RLS 谓词仍是 `tenant_id = ctx`，完全合规。

```sql
-- 客户侧结算单（客户租户内）
CREATE TABLE IF NOT EXISTS settlement_statements (
  id                  BIGSERIAL PRIMARY KEY,
  tenant_id           BIGINT NOT NULL,          -- 客户租户自身（BD-1 合规）
  source_provider_ref VARCHAR(160),             -- 服务商标识（字符串，弱引用）
  source_contract_ref VARCHAR(160),             -- 对方合同号（字符串，弱引用）
  statement_no        VARCHAR(64) NOT NULL,
  period_from         DATE NOT NULL,
  period_until        DATE NOT NULL,
  currency            VARCHAR(8),
  amount              NUMERIC(18,2),
  metrics_snapshot    JSONB DEFAULT '{}',       -- 期内 SLA 指标快照
  penalty_snapshot    JSONB DEFAULT '{}',       -- 期内罚则计算快照
  payload_hash        VARCHAR(64),              -- 快照内容 SHA-256（可自证未被篡改）
  issued_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  issued_by_ref       VARCHAR(160),             -- 下发方标识（字符串）
  status              VARCHAR(16) NOT NULL DEFAULT 'issued', -- issued|disputed|accepted
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, statement_no)
);
```

> 注意：`source_provider_ref` / `source_contract_ref` 是**字符串**而非 `provider_tenant_id` 外键——这样连"跨租户 ID 引用"都最小化，且客户无法据此反查 provider 内部数据。

### 4.3 `msp_contracts.customer_tenant_id` 字段怎么处理

| 用途 | 处置 |
|---|---|
| 标记"这份合同服务哪个客户租户" | ✅ 保留（provider 内部业务字段，不构成跨租户读） |
| 作为 RLS 放宽依据 | ❌ 删除该 policy |
| 作为客户侧反查入口 | ❌ 不提供；客户侧只认 `settlement_statements` |

### 4.4 迁移脚本（替代 012 的 customer_read 部分）

```sql
-- 013_revoke_msp_contract_customer_read.sql
-- 目标：移除跨租户读放宽，落实 BD-1/BD-2

DO $$
BEGIN
  IF to_regclass('public.msp_contracts') IS NOT NULL THEN
    DROP POLICY IF EXISTS msp_contract_customer_read ON msp_contracts;
    RAISE NOTICE 'rls 013: dropped msp_contract_customer_read';
  END IF;

  IF to_regclass('public.msp_contract_sla_bindings') IS NOT NULL THEN
    DROP POLICY IF EXISTS msp_contract_binding_customer_read ON msp_contract_sla_bindings;
    RAISE NOTICE 'rls 013: dropped msp_contract_binding_customer_read';
  END IF;
END $$;

-- provider 侧策略保持不变（tenant_id = ctx）
-- 回滚见 013_revoke_msp_contract_customer_read_rollback.sql（重建原策略，仅应急）
```

**上线前置检查**（必须全绿）

| 检查 | SQL / 动作 |
|---|---|
| 无客户租户仍在调用 msp_contracts 查询接口 | 查访问日志 / 接口埋点，确认 `customer` 角色无 `GET /msp-contracts*` 调用 |
| 结算页面已切到 `settlement_statements` | 前端路由与接口联调通过 |
| 存量已下发结算单 | 数据迁移完成，条数核对一致 |
| 回滚脚本可用 | 演练一次（仅重建策略，不改数据） |

### 4.5 裁决：收回（附「必须同批改造 L1」的硬前置）

> **结论：收回。但不是"只跑一条 DROP POLICY"，而是「收回 + L1 读取改道」必须同一提交。**

#### 4.5.1 为什么不能保留现状

| 保留的代价 | 具体内容 |
|---|---|
| **与用户裁决直接冲突** | 「所有数据都不跨租户」是硬约束，不是可权衡项；保留即等于把违规写进基线 |
| **策略无法收窄** | PostgreSQL 无列级 RLS。`FOR SELECT USING (customer_tenant_id = ctx)` 天生是**整行可见**，`penalties`（违约率/上限）、`settlement_cycle`、`currency` 一并暴露，无法只放条款不放罚则 |
| **无生命周期** | 策略不含 `status` / 有效期谓词。合同 `terminated`、双方解约、甚至争议中的合同，历史行**永久可读** |
| **应用层失去兜底** | RLS 一旦放宽，后续任何新增的 customer 侧查询（导出、搜索建议、AI 检索）都自动继承这份可见性，只能靠每个调用方自觉加谓词；而 RLS 存在的意义正是"不依赖调用方自觉" |
| **不可审计** | 客户读 provider 行这件事在数据库侧无痕迹，无法回答"谁在什么时候读过对方的罚则条款" |

#### 4.5.2 为什么也不能"只收回"——L1 会静默降级

这是本次决策最容易踩的坑，必须先讲清楚：

```
matchContractDefinition（ticket_sla_service.go:688-724）
  → client.MSPContract.Query()  ← 跑在 customer ctx
  → RLS driver（database/rls/driver.go:151）在 enforce 模式下
    把非 system-bypass 的租户作用域语句路由到低权池 itsm_app
  → 若删除 customer_read 策略 → 返回 0 行
  → L1 未命中 → 落到 L2 → L3b → L4
  → L4 是 fail-loud，但返回值是默认 60 / 480 分钟
  → 结果：客户工单不再按合同 SLA 计时，且不报错
```

**这是"合规改造"变成"业务事故"的典型路径**：合同 SLA 悄悄失效，覆盖率虚高、漏报违约，结算基准全部失真。所以 `013` 不能单独上线。

#### 4.5.3 L1 改道的三个方案

| 方案 | 做法 | 评价 |
|---|---|---|
| **L1-A 系统特权上下文** | `matchContractDefinition` 内改用 `tenantctx.SystemContext(ctx, "sla:l1-contract-resolve", ...)` 走 admin 池（BYPASSRLS），查询里已有显式 `CustomerTenantIDEQ(tenantID)` 谓词兜底 | ⚠️ **可做但不推荐为终态**。技术上可行（仓库既有 `SystemContext` 惯例，`common/tenantctx/system.go:39` 带审计日志），但它把"请求路径能读跨租户业务行"变成常态，与本文「受控访问 ≠ 跨租户读表」自相矛盾；且 BYPASSRLS 权限漏进请求路径后，一次谓词写错就是全量泄露 |
| **L1-B 合同 SLA 快照下发（推荐）** | provider 在合同 `active` 时，向 customer 租户**物化一份最小快照** `contract_sla_snapshots`（`tenant_id = C`，仅含 `contract_ref`、期限、`sla_definition_id` 引用集、状态），L1 读本租户副本 | ✅ **唯一同时满足 BD-1 与 L1 可用的方案**。与 `settlement_statements`（§4.2）同构，复用同一套"计算 → 快照 → 显式下发 → 读自身副本"模式；`penalties` / `currency` **不进快照**，天然收窄 |
| **L1-C 保持现状** | 不动 | ❌ 与用户裁决冲突，否决 |

**推荐 L1-B 的关键理由**：客户真正需要的是「我的工单适用哪套时限」，而不是「服务商那份合同的全部商务条款」。快照是**按需裁剪后的最小事实集**，既让 L1 可算，又让客户读的是自己租户的行——RLS 谓词回到 `tenant_id = ctx`，合规与功能同时成立。

#### 4.5.4 若暂不具备 L1-B 条件，允许的过渡

若必须分两步走，**过渡顺序唯一可行**是「先建快照通道，再收回策略」：

```
Step 1（新增，不删任何东西）
  ├─ 建 contract_sla_snapshots + RLS(tenant_id = ctx)
  ├─ provider 侧合同 active/变更/终止 时同步下发快照（写 customer 租户）
  ├─ matchContractDefinition 改为读快照（双写期两路都读，取快照优先）
  └─ 观察一个结算周期，确认 L1 命中率与改前一致

Step 2（收回）
  ├─ 删除快照回退逻辑（不再 fallback 到 msp_contracts）
  ├─ 执行 013 DROP POLICY
  └─ 验收 §7 全部 10 条 + L1 命中率无下降
```

**禁止的过渡顺序**是「先 DROP 再想办法」——那会直接造成 §4.5.2 的静默降级，且降级不可观测（只有 warn 日志）。

#### 4.5.5 一句话裁决

> **保留现状 = 保留一个无法收窄、无生命周期、不可审计的永久后门，且与「业务数据不跨租户」硬约束正面冲突；收回 = 需要一个同批的 L1 改造（推荐 L1-B 快照下发），成本一次性、收益是把同类问题在框架层封死。**
> 因此：**收回，且按 4.5.4 的两步顺序执行，不得先 DROP。**

---

## 5. 结算/报表类需求的正确做法（通用模式）

> 凡是"客户要看服务商算出来的结果"，一律走本模式，不要新开 RLS 放宽。

```
① 数据产生方（provider）在自己的租户内计算
② 生成不可变快照（含 payload_hash）
③ 下发动作：以显式写权限写入对方租户（或由对方主动拉取后写入自身租户）
④ 消费方（customer）读自己租户内的快照副本
⑤ 争议：customer 在副本上置 status='disputed'，provider 侧收到通知（消息不含业务数据，只含 statement_no）
⑥ 更正：provider 下发新版快照（新 statement_no / 版本号），旧版保留
```

| 反模式 | 为什么违规 |
|---|---|
| 给 customer 开 `msp_contracts` 的 SELECT 策略 | 直读对方业务行（当前 012 就是） |
| 建跨租户 VIEW 让客户查 | VIEW 底层仍读对方表，且 RLS 可能被绕过 |
| 提供一个"跨租户聚合 API" 在服务层拼数据 | 服务层能拼，说明数据已跨租户取出，违规 |
| 用 provider 的超管权限代查后返回 | 越权代理，审计上无法区分"谁看的" |

---

## 6. 对既有设计文档的修订要求

| 文档位置 | 问题 | 修订 |
|---|---|---|
| [06 §3.1](./06-contract-modeling-design.md) 合并代价表 | 写成「`msp_contracts` 必须引用 `customer_tenant_id`（跨租户）」→ 把跨租户当作既定前提 | 改为「`msp_contracts` 现有 RLS 存在客户租户只读放宽，本身违反 BD-1，需先收回（见 07 §4）」 |
| [06 §6.1](./06-contract-modeling-design.md) 分工表 | 「跨租户 ✅ `customer_tenant_id` 授信」→ 把违规当特性 | 改为「跨租户可见性：❌ 禁止；客户侧经结算快照（07 §4.2）」 |
| [04](./04-multi-provider-scenario.md) | 已有"不跨租户"原则，但未覆盖 RLS 放宽这一具体形态 | 补一条指向本文 BD-1 |
| [01](./01-customer-master-data-design.md) §6.7 隔离矩阵 | 只讲"跨租户读取唯一合法路径 = MSPAllocation 受控访问" | 补「受控访问 ≠ 跨租户读表」的辨析 |
| [02](./02-ddl-and-migration.md) §12.5 | 只登记了 `contract_project_links` 的 RLS | 补 §12.6：`contract_sla_snapshots` / `settlement_statements` 受管表登记、最小字段集、013 四项前置检查与回滚 |
| [multi-tenant/07-known-gaps.md](../multi-tenant/07-known-gaps.md) | 缺口清单未收录本违规 | 已登记为 **G17**（§18 详情），编号空间由 G1–G16 扩至 G1–G17 |
| [multi-tenant/plan/sla-contract-multitenant-plan-2026-10-14.md](../multi-tenant/plan/sla-contract-multitenant-plan-2026-10-14.md) §B.8 IM-15 | 原文把 customer 只读授信当作既定设计 | 已补「IM-15 补正」行：判定违规、裁决收回、唯一真实依赖为 L1 |

---

## 7. 验收清单（硬边界专项）

1. `pg_policy` 审计 SQL（§2.1）返回 **0 行**；
2. `msp_contracts` / `msp_contract_sla_bindings` 上**不存在** `customer_*_read` 策略；
3. 以客户租户上下文（`app.current_tenant = C`）执行 `SELECT * FROM msp_contracts` 返回 **0 行**；
4. 以 provider 上下文执行同一查询返回其自有行（隔离未被误伤）；
5. 客户可在自己租户内查到 `settlement_statements`，字段含 `payload_hash`；
6. `settlement_statements` 中**无任何 provider 侧内部 ID 外键**（仅字符串 `source_*_ref`）；
7. 客户租户上下文无法通过任何接口（含导出、搜索建议、AI 检索）取得 provider 合同正文；
8. 全库无跨租户外键（`pg_constraint` 检查）；
9. 新增业务表一律只登记 `tenant_id = ctx` 单谓词策略，评审清单含 BD-1 检查项；
10. 回滚脚本 `013_..._rollback.sql` 可用且已演练。

---

## 8. 与「不跨租户」原则的关系：一张图收束

```
                    平台层（非业务数据，可跨）
                 tenants 拓扑 / msp_allocations 授权
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
  provider 租户 P        customer 租户 C        内部租户 I
  ┌──────────────┐      ┌──────────────┐      ┌──────────┐
  │ support_     │      │ settlement_  │      │ projects │
  │ contracts    │      │ statements   │      │ tickets  │
  │ msp_contracts│      │ tickets      │      │ ...      │
  │ projects     │      │ users        │      └──────────┘
  │ tickets      │      │ ...          │
  └──────────────┘      └──────────────┘
        │                      ▲
        │  ③ 显式下发（写对方租户，非读对方）
        └──────────────────────┘
        ①② 计算 → 快照(payload_hash)
        
  ❌ 禁止：P 的 RLS 给 C 开 SELECT
  ❌ 禁止：跨租户 JOIN / VIEW / 聚合 API
  ✅ 允许：弱引用 ID、授权进入、快照下发
```

> **一句话**：数据只能在**一个租户里存在并计算**；跨租户流动的唯一形式是"**显式下发的一份副本**"，且落库后归接收方租户所有。
