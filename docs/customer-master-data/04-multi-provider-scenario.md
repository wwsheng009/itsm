# 04 · 多服务商场景下的客户主数据用法

> 状态：Draft v1.0 · 2026-11-06
> 前置决策（已由用户拍板）：**客户不跨租户共享，所有数据都不跨租户。**
> 本文回答一个问题：既然数据绝不跨租户，那在**多服务商（Multi-MSP Provider）**场景下，客户主数据到底怎么用、能不能用、边界在哪。

---

## 1. 决策前提与四条硬推导

### 1.1 前提

| 编号 | 决策 | 影响 |
|---|---|---|
| **D-A** | 客户主档**不跨租户共享**，所有客户主数据都在 `tenant_id` 内闭环 | 关闭 Q-01「全局客户池 + 租户订阅」方案 |
| **D-B** | 不引入 `customer_global_id` / 全局客户注册中心 | 表结构不新增全局唯一客户标识 |
| **D-C** | 客户主档仍按 `tenant_id` 归属，RLS 全覆盖 | 沿用主设计 P-01 原则不变 |
| **D-D** | 本原则适用于**全部业务数据**，不只是客户主数据 | 铁律 BD-1~BD-4 见 [07-tenant-data-boundary.md](./07-tenant-data-boundary.md)；含 `pg_policy` 审计 SQL 与违规处置 |
| **D-E**<br>（2026-11-06） | 现有 `msp_contracts` 的两条 `customer_read` 策略（RLS 012）**判定违规，裁决收回** | 保留现状 = 保留整行、无生命周期、不可审计的永久后门；收回须与 L1 改道同批（推荐客户租户内快照 `contract_sla_snapshots`）。**禁止先 DROP**，否则 L1 静默降级到 L4 默认 60/480 分钟。完整因果与两步过渡见 [07 §3.2 / §4.5](./07-tenant-data-boundary.md) |

### 1.2 四条硬推导（必须写进代码纪律）

| 编号 | 推导 | 工程含义 |
|---|---|---|
| **C-01** | 同一真实客户被 N 个服务商服务时，系统内存在 **N 条互不可见的独立主档** | 禁止任何"跨 provider 客户去重"逻辑；`uq_customer_tenant_taxid` 的 `tenant_id` 前缀是**故意**的，不是缺陷 |
| **C-02** | 客户只能归属**一个**服务商（`tenants.msp_provider_id` 单值，对齐 IP-P0-4） | 同一客户不会同时拥有 A、B 两家 provider 的 `msp_customer` 租户；"多服务商"不等于"多归属" |
| **C-03** | 服务商之间**不做数据同步、不做实时对账、不做联合 360** | 不建跨 provider 消息通道；`customer_external_ids` 只存"我认识的对方标识"，不拉数据 |
| **C-04** | 跨 provider 的语义对齐只能靠**客户中性标识**（`tax_id` / `email_domains`）+ **客户授权的导出导入** | 对齐是"逻辑判定"，不是"数据共享" |

> 一句话：**多服务商场景下，主数据是"每个服务商各自的客户视图"，不是"共享的客户真相"。**

---

## 2. 三种拓扑：先分清哪一种是"多服务商"

### 2.1 拓扑 1 · 单客户单服务商（基线，占绝大多数）

```
provider 租户 A (id=3, type=msp_provider)
   └── service_customers(id=100, tenant_id=3, customer_type='msp_endcustomer', tax_id=T)
         └── customer_tenant_links → 客户租户 (id=10, type=msp_customer, msp_provider_id=3)
```

用法：SLA 绑定、客户经理、联系人、合同、项目关联全部正常生效。

### 2.2 拓扑 2 · 客户同时使用多个**互相独立**的服务商

```
provider A 租户 (id=3)
   └── customer 100: tax_id=T, name=客户X, code=CUS-0100   ← A 自己维护的视图

provider B 租户 (id=7)
   └── customer 200: tax_id=T, name=客户X, code=BE-0042    ← B 自己维护的视图（另一条记录）

客户租户 (id=10)：只能挂 msp_provider_id = 3 或 7 之一
```

**关键事实**
- A 看不到 200，B 看不到 100；两边数据**物理隔离**（RLS）。
- 两条档案"逻辑上是同一法人"，但系统**不认为**它们有关系——这是设计选择，不是缺陷。
- 客户租户只能归属一方；另一方以"非租户形态"服务（如仅提供项目制交付、或客户自持账号）。

**必须明确的边界**

| 事项 | 结论 |
|---|---|
| 跨 provider 工单联动 | ❌ 不做 |
| 跨 provider 客户 360 | ❌ 不做（各自只有自己的一段） |
| 跨 provider 联系人共享 | ❌ 不做（各自维护自己认识的对接人，符合现实） |
| 同一个 `tax_id` 在 A/B 两处并存 | ✅ 允许，且是**常态** |

### 2.3 拓扑 3 · 服务商切换 / 交接（Handover，多服务商里最需要设计的流程）

```
阶段 0  A 服务中：A 租户有完整主档 + 合同 + 工单历史
阶段 1  客户决定迁往 B
阶段 2  客户授权 → 生成「客户主数据交接包」（客户签字/邮件确认）
阶段 3  B 在 B 租户内导入 → 生成 B 视角新主档（新 id，记录来源标识）
阶段 4  A 侧：customer → lifecycle_stage='churned'；客户租户 suspended → archived
阶段 5  归属切换：tenants.msp_provider_id 3 → 7（高危，双人审批）
```

**核心原则：交接是「导出—导入」，不是「共享—同步」。** 数据在切换瞬间通过"客户授权的离线包"完成搬家，而不是两个租户之间建立管道。

### 2.4 易混淆对照：多客户单服务商 ≠ 一客户多服务商

| 场景 | 实际形态 | 用哪套机制 |
|---|---|---|
| **多客户单服务商**（最常见） | 1 个 `msp_provider` 下挂 N 个 `msp_customer` 租户 | `MSPAllocation` 分派工程师 + provider 租户内 N 条客户主档 + `customer_tenant_links` 指向各客户租户 |
| **一客户多服务商** | N 个 `msp_provider` 各自有该客户的独立档案 | 本文拓扑 2：各自独立，仅标识对齐 |
| **集团型服务商**（父子公司） | `msp_provider` 的父子租户链 | 沿用 `tenants.parent_tenant_id`，客户主档不跨层共享 |

> 这三种经常被口头混称"多服务商"，落地前必须先归类，否则会写出违规的跨租户 JOIN。

---

## 3. 不跨租户的前提下，主数据在多服务商场景的 5 个真实用法

### 3.1 用法一：客户身份对齐（对齐但不共享）

**目标**：让 B 知道"这个客户在 A 那边有历史"，但**读不到** A 的任何数据。

实现：`customer_external_ids` 记录"对方租户里的标识"

```sql
-- B 租户内，客户 200 上记录：该客户在 A 服务商处的编码
INSERT INTO customer_external_ids (tenant_id, customer_id, system_code, external_id, external_url)
VALUES (7, 200, 'msp:A-TENANT-CODE', 'CUS-0100', NULL);
```

| 字段 | 用法 |
|---|---|
| `system_code` = `msp:<对方租户 code>` | 声明"这是某个外部服务商租户" |
| `external_id` = 对方的客户 code | 存一个字符串引用，**不带任何数据** |
| `external_url` | 可选，指向**客户授权后**可访问的凭据页面（不是本系统内跳转） |

对齐判定（只在本租户内计算，不查对方）：

| 条件 | 判定 |
|---|---|
| `normalized_tax_id` 相同 | 高度疑似同一法人 → 提示"该客户可能已有其他服务商记录，是否登记外部标识" |
| `email_domains` 交集非空 | 疑似同集团 → 弱提示 |
| 名称归一后相近 | 弱提示 |

> 注意：上述提示**只在同一租户内**与其他客户记录比对（如 A 租户内有两个代理商录入同一客户），**从不跨租户查询**。

### 3.2 用法二：服务边界声明（多服务商最实际的诉求）

多服务商真正的痛点往往不是"看不到对方的客户数据"，而是**责任边界扯不清**（谁管网络、谁管应用、故障时互相推诿）。

新增主档字段 `coverage_scope`（JSONB），在本租户内声明"我负责到什么程度"：

```json
{
  "inScope": ["network", "firewall", "server-hardware"],
  "outOfScope": ["application", "database", "end-user-device"],
  "boundaryNote": "应用层由另一家服务商负责，跨域故障需联合作战室",
  "contactForBoundary": "customer_contacts.id=3021"
}
```

配套：
- 与 `support_contracts.contract_type` 联合展示（合同范围 vs 主档范围）；
- 工单派单时提示"该资产/系统可能超出服务范围"（软提示，不阻断）；
- 交接包中必须包含 `coverage_scope`，避免新服务商接手后边界真空。

### 3.3 用法三：联系人各自维护（天然适配）

| 特点 | 说明 |
|---|---|
| 每个 provider 维护**自己认识的**对接人 | A 的技术对接人 ≠ B 的技术对接人，完全正常 |
| `customer_contacts.linked_user_id` 为空即可 | 无需为客户建系统用户 |
| 通知只按本租户联系人发 | 不跨租户发送、不共享收件人 |
| `opt_in` 独立控制 | 合规上互不影响 |

> 这是"不跨租户"反而更贴合现实的一处：客户对不同服务商的联系窗口本来就不同。

### 3.4 用法四：SLA 与合同完全独立

| 对象 | 归属 | 多服务商下的含义 |
|---|---|---|
| `sla_definition_id` | 各租户自己的 `sladefinitions` | A、B 各自定义，互不干扰 |
| `support_contracts` | 各租户自己的合同 | 合同号在各自租户内唯一，**跨 provider 不保证唯一** |
| SLA 达成率 | 只能算"本 provider 段" | 不存在"客户总 SLA" |

**结论**：对外汇报时，只能声称"我方负责范围内的达成率"，不能合成客户整体可用性。这是"不跨租户"必然带来的表述约束，需要在报表文案上明确。

### 3.5 用法五：项目关联不受影响（同租户内闭环）

`project_customer_links` 全程在**项目所属租户**内，不因多服务商改变：

```
provider A 租户：project(交付项目) ── link ── customer 100（A 侧主档）  ✅
provider B 租户：project(另一个项目) ── link ── customer 200（B 侧主档） ✅
跨租户：project(A) ──✗── customer 200                                ❌ 禁止
```

---

## 4. 为多服务商场景补充的表与字段

### 4.1 主档新增：服务边界

```sql
ALTER TABLE service_customers
  ADD COLUMN IF NOT EXISTS coverage_scope JSONB DEFAULT '{}';
-- {inScope:[], outOfScope:[], boundaryNote:"", contactForBoundary:null}
```

### 4.2 新增：客户主数据交接包（Handover Package）

> 仅记录"交接这件事发生过、给了谁、授权人是谁、导出了什么范围的快照哈希"，**不存客户数据本体**（数据本体走客户授权的离线导出文件）。

```sql
CREATE TABLE IF NOT EXISTS customer_handover_packages (
  id                   BIGSERIAL PRIMARY KEY,
  tenant_id            BIGINT NOT NULL,           -- 发起方（原服务商）租户
  customer_id          BIGINT NOT NULL REFERENCES service_customers(id),
  direction            VARCHAR(16) NOT NULL,      -- export|import
  counterpart_ref      VARCHAR(160),              -- 对方服务商标识（租户 code 或名称），弱引用
  scope                JSONB DEFAULT '{}',        -- 导出范围：{branch,contacts,contracts,projects,assets}
  package_hash         VARCHAR(64),               -- 导出文件 SHA-256，用于自证未被篡改
  item_counts          JSONB DEFAULT '{}',        -- {branches:3, contacts:8, contracts:2, ...}
  status               VARCHAR(24) NOT NULL DEFAULT 'draft',
      -- draft|authorized|exported|imported|revoked
  authorized_by        VARCHAR(255),              -- 客户授权人（姓名 + 邮箱，来自客户侧确认）
  authorized_at        TIMESTAMPTZ,
  authorization_evidence VARCHAR(500),            -- 邮件主题/工单号/签字件引用
  created_by           BIGINT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_handover_tenant_customer
  ON customer_handover_packages (tenant_id, customer_id, status);
```

**状态机**

```
draft ──提交授权──> authorized ──导出完成──> exported ──对方确认导入──> imported
  │                     │                       │
  └────撤销─────────────┴───────撤销────────────┘──> revoked
```

**硬规则**
1. 无 `authorized_at` + `authorization_evidence` 时**禁止** `status` 前进到 `exported`；
2. 导出必须记录 `package_hash` 与 `item_counts`，便于事后证明"导出了什么"；
3. `direction='import'` 由接收方在**自己的租户**内登记，与发起方记录互不可见（各自留痕，不共享一条记录）；
4. 交接包**不含**工单正文历史（仅计数与摘要），正文按合同/合规要求另行处理。

### 4.3 归属切换（provider handover 的租户侧动作）

`tenants.msp_provider_id` 变更是**高危操作**，必须走审批而非普通接口：

| 项 | 要求 |
|---|---|
| 权限 | 平台级 `tenant.provider.reassign`（不授予 provider 自身） |
| 审批 | 双人复核（申请人 ≠ 审批人） |
| 前置校验 | 原 provider 侧客户 `lifecycle_stage='churned'`；交接包 `status='imported'`；目标 provider 活跃 |
| 审计 | `auditlog` 记录 before/after provider + 交接包 ID + 审批单号 |
| 数据留存 | 原租户**不删除**，转 `archived`；保留期 ≥ 合同约定（默认 12 个月） |
| 不可逆窗口 | 24 小时内可回滚（仅限误操作，需平台审批） |

---

## 5. 多服务商场景下的操作流程（端到端）

### 5.1 客户接入第二个服务商（拓扑 2）

```
1. B 服务商在自己的租户内新建客户主档
   - 录入 tax_id / email_domains（用于本租户内去重提示）
   - customer_type='msp_endcustomer'
2. B 填写 coverage_scope，声明"我只负责哪一块"
3. B 在 customer_external_ids 登记「客户在 A 处的标识」（可选，仅字符串引用）
4. B 维护自己的联系人、SLA、合同
5. 【不做】B 不查询 A 的主档，不申请跨租户读取，不建共享通道
```

### 5.2 客户从 A 迁到 B（拓扑 3）

```
1. A 侧发起：创建 customer_handover_packages(direction='export', status='draft')
2. 客户授权：客户方授权人邮件/签字确认 → 回填 authorized_by/at/evidence
3. A 侧导出：按 scope 生成离线包（含主档、分支、联系人、合同摘要、项目关联、coverage_scope）
   → 记录 package_hash + item_counts → status='exported'
4. B 侧导入：B 在自己的租户内新建主档并导入 → 生成新 id
   → B 侧登记 customer_handover_packages(direction='import', counterpart_ref='A', status='imported')
5. B 侧在新主档上记录 customer_external_ids(system_code='msp:<A code>', external_id='<A 的客户 code>')
6. A 侧收尾：customer → lifecycle_stage='churned'；churn_at=now()；churn_reason='服务商变更为 B'
7. 平台侧：审批 tenants.msp_provider_id 变更；原租户转 archived
```

**时间线约束（硬性）**
- 步骤 3 与步骤 4 之间，A 侧主档**保持可读但不接受新业务写入**（避免导出后又变更）；
- 步骤 7 必须在步骤 4 完成之后，否则客户会陷入"无归属"窗口。

### 5.3 撤单 / 交接失败

| 情况 | 处理 |
|---|---|
| 客户撤回授权 | `status='revoked'`；A 侧主档恢复正常，`lifecycle_stage` 回退 |
| 导入失败 | B 侧保留 `direction='import'` 的失败记录；A 侧不回退（仍 `exported`） |
| 切换后回退 | 24 小时内平台审批回退 `msp_provider_id`；交接包记录保留作为审计证据 |

---

## 6. 明确不做的反模式（写进评审清单）

| 反模式 | 为什么禁止 | 正确替代 |
|---|---|---|
| 建全局客户池，各租户"订阅"同一客户 | 直接破坏"不跨租户"与四层隔离 | 各租户独立主档 + `customer_external_ids` 引用 |
| 用 `tax_id` 做全局唯一索引 | 会让 B 无法录入 A 已录入的客户 | 唯一键保持 `(tenant_id, normalized_tax_id)` |
| provider 之间建 RPC/消息同步客户数据 | 变相跨租户 | 客户授权的离线交接包 |
| 跨租户 JOIN 拼"客户 360" | 违规且性能不可控 | 各自 360，范围内陈述 |
| 用 `MSPAllocation` 去读对方客户主档 | 分配授权只覆盖**被分配的目标租户**，不是对方 provider 的档案 | 不要设计这种路径；如确需协作，走客户主导的联合工单 |
| 客户租户同时挂两个 `msp_provider_id` | 归属唯一写通道（IP-P0-4） | 切换而非并存；并存需求回到 Q-01 另立项 |
| 合成"客户整体可用性 SLA" | 数据不足，且跨越 provider 边界 | 只报"本 provider 责任范围内" |

---

## 7. 对既有设计文档的变更清单

| 文档 | 变更 |
|---|---|
| `README.md` | 文档结构新增本文；取舍表更新"客户是否跨租户共享 = 不共享（已裁决）" |
| `03-open-questions.md` | Q-01 关闭（选 A，严格租户内）；Q-03 收敛为"客户只能归属一个 provider" |
| `01-customer-master-data-design.md` | §6 多租户场景新增「场景 E：多服务商（拓扑 2）」，加交叉引用；§4.5 服务域新增 `coverage_scope` |
| `02-ddl-and-migration.md` | 新增 `coverage_scope` 字段、`customer_handover_packages` 表、RLS 登记项 |

---

## 8. 验收清单（多服务商专项）

1. A 租户用户携带全部权限，也无法通过任何接口读到 B 租户的客户 100/200 任一字段（含列表、详情、导出、搜索建议）；
2. 同一 `tax_id` 在 A、B 两个租户可各自创建成功，均不报唯一冲突；
3. `customer_external_ids` 只存字符串引用，接口返回中不含任何对方租户的业务字段；
4. 交接包在 `status='draft'` 时导出接口返回 403/422，提示"缺少客户授权"；
5. 交接包 `exported` 后记录 `package_hash` 与 `item_counts`，可在审计页查看；
6. `tenants.msp_provider_id` 变更接口在无审批单号时拒绝执行；
7. 原 provider 客户 `lifecycle_stage='churned'` 后，A 侧新工单不能再关联该客户（或仅允许只读归档）；
8. `coverage_scope` 在客户详情、工单派单、交接包三处一致展示；
9. 任一"跨 provider 客户 360"或"全局客户搜索"需求，在评审中被本文 §6 明确驳回。

---

## 9. 一句话总结

> 既然所有数据都不跨租户，多服务商场景下客户主数据的定位就是：
> **「每个服务商各自的客户经营底座」——用同一套模型、各自的档案、靠中性标识对齐、靠客户授权交接。**
>
> 主数据提供的是**内部一致性**（分层、SLA、联系人、项目、结算口径统一），
> 而不是**跨组织的客户真相**。想清楚这一点，多服务商就不会把架构拖进跨租户泥潭。
