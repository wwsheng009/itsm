# 多租户文档一致性审计（架构割裂排查）

> 状态：**Draft v0.1（待评审）**｜日期：2026-09-29｜基准：仓库 HEAD `06c4b263`｜基准口径：`msp-concept-model-and-architecture-canon.md` v0.7
> 范围：`docs/multi-tenant/` 全部 18 份文档 + 上位 `docs/architecture/adr-004-*.md`
> 方法：三路并行只读审计（① 编号文档 01–07；② 作用域模型与用户生命周期；③ 交互/登录/工作台/前端）+ 核心文档自审 + 代码交叉核对（`middleware/msp_gate.go` 等）
> 关联：[概念总纲](./msp-concept-model-and-architecture-canon.md)（附录 C 为本文注册表的权威载体）｜[目标架构](./msp-target-architecture.md)｜[工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)

---

## 0. 结论（TL;DR）

**存在系统性"架构割裂"，共 3 类根因**（不是零散笔误）：

| # | 根因 | 表现 |
|---|---|---|
| **根因 1** | **修订未回填**：2026-09-29 的两轮修订（工作台"条目级写无需切换"、登录细化"登录落 provider 家/无候选列表"）只落在新文档与修订记录，旧文档正文未同步 | 同一问题两套答案：target-architecture §4.2、scope-model、lifecycle、user-flows F-06、login §4.3/P0、frontend P0、05-usage-guide 仍写"连续操作走切换/顶栏切换器/登录候选选择" |
| **根因 2** | **编号重号**：A#/G#/R#/F#/D#/B# 在多个文档各表其意 | 引用即歧义（如 `A8` 可指 canon 验收、ADR-004 缓存行动项、frontend 验收；`G4` 可指 07 缺审批组、canon 建号受限、工作台规则） |
| **根因 3** | **同一概念多套词表** | MSP 角色（代码 `msp_viewer/tech/specialist/manager/admin` vs Q7 模板 `msp_observer/tech/manager/full`）；membership 表名（`memberships` vs `user_tenant_memberships`）；字母 A/B/C 多义（部署选项/实现路线/拆分方案） |

**本次已修复**：canon 部署门控事实错误（`saas`/未知模式）、G#→K# 改名、membership 表名对齐、新增附录 C 注册表与权威层级、README 口径与状态、工作台编号（R/G）、user-flows F-06、login 顶栏口径、target-architecture/scope-model/lifecycle 修订指针、01–07 as-is 横幅。

**仍待决策**：MSP 角色词表统一（D10）；`account_kind` 客户单作用域强约束 vs canon B5/A4/D4；scope-model/lifecycle 正文回填排期。

---

## 1. 权威层级与生效规则（防割裂的"宪法"）

| 层级 | 文档 | 权威范围 |
|---|---|---|
| L0 选型 | `docs/architecture/adr-004-*.md`（Proposed） | 租户模型选型结论、行动项（**编号 `ADR-004:A#`**） |
| L1 概念 | **canon（本目录总纲）** | 概念定义、边界规则 B1–B6、不变量 I1–I13、术语收敛、迁移路线、开放决策 D/E |
| L2 决策修订源 | [工作台方案](./msp-cross-customer-workbench-and-filter-plan.md)、[登录与切换细化](./msp-login-and-switching-refinement-plan.md) | 2026-09-29 起对"跨客户操作/登录落地"的**最新修订**（对被修订文档有约束力） |
| L3 目标详细设计 | [目标架构](./msp-target-architecture.md) | membership 模型、上下文解析、权限矩阵、建号/邀请、数据模型与迁移（**须接受 L2 修订**） |
| L4 方案 | user-lifecycle、scope-model、user-flows、frontend | 各自领域实现计划（编号为文档局部编号，引用须加前缀） |
| L5 现状/运营 | 01–07（as-is） | 实测现状、部署与运营手册（**不得作为目标口径**） |
| L6 演练 | three-persona-operation-simulation | 操作剧本（现状/目标并列，不定义架构） |

**生效规则**：

1. **概念/边界冲突 → canon 胜**；实现/交互冲突 → **最新修订源（L2）胜**，但**必须回填被修订文档并留指针**（否则视为割裂）；
2. **现状描述冲突 → 以 01–07 + 代码为准**；canon 若与代码不符，必须区分"现状 vs 目标"两列表述（本次已修 1 处）；
3. **任何文档引用编号必须带前缀**（见 §2）；新增编号必须先在 canon 附录 C 登记；
4. 状态行必须与修订记录一致（本次修正 canon 头部 `Draft v0.1`→`v0.7`）。

---

## 2. 术语与编号注册表（摘要；权威版 = canon 附录 C）

| 编号 | 归属 | 含义 | 引用写法 |
|---|---|---|---|
| `R1–R12` | canon | 架构/安全风险 | `R9` |
| `K1–K5` | canon §7.3 | provider 能力供给缺口（**原 G1–G5 改名**） | `K1` |
| `G1–G10` | 07-known-gaps | 生产实测缺口（唯一 G 空间） | `07:G4` |
| `F1–F15` | user-lifecycle（主方案） | 生命周期/切换发现项 | `F7` |
| `UF-01–UF-10` | user-flows | 交互流程号（**原 F-01…F-10 改名**） | `UF-05` |
| `WB1–WB6` | 工作台方案 | 工作台规则（**原 G1–G6 改名**） | `WB3` |
| `REV-1–REV-5` | 工作台方案 | 对既有文档的修订项（**原 R7–R11 改名**） | `REV-2` |
| `WB-R1–WB-R6` / `WB-A1–WB-A6` | 工作台方案 | 工作台风险 / 验收（**原 R1–R6 / A1–A6 改名**） | `WB-R2`、`WB-A1` |
| `D1–D10` | canon §10 | 开放决策 | `D10` |
| `E1–E6` | canon §7.2 | 工单流转决策 | `E2` |
| `I1–I13` | canon §6 | 不变量 | `I9` |
| `A1–A12` | canon §9 | 验收标准（canon 内裸 `A#` 仅此含义） | `A12` |
| `ADR-004:A1–A11` | ADR-004 | 行动项（**必须带前缀**） | `ADR-004:A8` |
| `LOGIN-A#` / `FE-A#` | login / frontend | 文档局部验收（**必须带前缀**） | `FE-A1` |
| `LOGIN-R#` / `LOGIN-D#` / `LOGIN-B#` | login | 局部修订/问题/边界（**必须带前缀**） | `LOGIN-R3` |
| 路线 A/B（lifecycle、target-architecture） | L2/L3 | **实现路线**：A=P0 最小闭环；B=P1 membership 转正 | `路线 A` |
| 选项 A/B（canon §7） | canon | **部署拓扑**：A=单 provider 预设；B=多 provider 模型 | `选项 B` |
| 方案 A/B（canon §2.2） | canon | **平台-服务商拆分**：A=同体保持；B=拆分平台租户 | `§2.2 方案 B` |

> **A/B/C 字母多义是本次审计确认的割裂源之一**：三处均保留但必须带限定词（"实现路线/部署选项/拆分方案"），禁止裸用。

---

## 3. 冲突清单（按严重度）

| ID | 位置 | 冲突内容 | 严重度 | 处置 |
|---|---|---|---|---|
| C1 | canon §7 表格 vs `middleware/msp_gate.go:21-33`、01/02/README | canon 写 `saas`→404，代码实际 `saas`/空/未知值→**开启** MSP | **高** | ✅ 已修：现状/目标分列 + 新增 R12 |
| C2 | target-architecture §4.2/§4.4、scope-model:261/266/285、lifecycle:247/435、05-usage-guide:47/80 | "连续操作走切换/头通道+切换为主路径" | **高** | ✅ 修订指针 + 05 横幅；正文回填排期 |
| C3 | scope-model:264/251-256、lifecycle:388/479/608 | 登录 409+候选列表、`last_active` 自动落地 | **高** | ✅ 修订指针（登录细化 + I8） |
| C4 | lifecycle:447-459、scope-model:470-477、target-architecture §5.3 vs canon §7.3 | MSP 角色词表两套（`msp_observer/tech/manager/full` vs `msp_viewer/tech/specialist/manager/admin`）；`specialist→msp_specialist` 悬空 | **中高** | 🟡 登记 D10（待产品/架构拍板） |
| C5 | canon C7/§4/P1 vs target-architecture §3.2 | membership 表名不一致 | 中 | ✅ canon 统一为 `user_tenant_memberships` |
| C6 | user-flows:268 | F-06 残留"写操作必须走切换"（与本文件 v0.4 修订及工作台 REV-1 矛盾） | **高** | ✅ 已修 |
| C7 | login §4.3/§TL;DR/P0（:118-125,155） | 顶栏主控件=切换器（工作台 REV-2 已修订为过滤器） | **高** | ✅ 已修（过滤器为主 + 切换器=深度入口） |
| C8 | 工作台:56-61/191-195 | 工作台用 `G1–G6`/`R7–R11`，与 07:G#、canon R# 重号 | 中高 | ✅ 改名 `WB1–WB6`/`REV-1–REV-5` |
| C9 | login:142-147,173-176,186-194,201-212 | `R1–R6`/`D1–D4`/`B1–B10`/`F1–F12`/`A1–A5` 与 canon 全局编号同号异义 | 中高 | ✅ 注册表前缀规范（`LOGIN-*`） |
| C10 | frontend:208-212 | 验收 `A1–A8` 与 canon A# 重号；P0 仍以"切换器"为主 | 中 | ✅ 前缀 `FE-A#` + 与 C7 同步 |
| C11 | 01:53/55/69/78/105、02:12/83/134、03:27/37/58、05:47 | 现状文档与目标口径冲突（类型枚举/双字段/admin 旁路/未知模式默认开启/连续操作切换） | 中 | ✅ as-is 横幅 + 审计登记；02 的 `/tmp` 指引按 G10 方向待改 |
| C12 | scope-model:142-144/174、lifecycle:261/544-546 vs canon B5/A4/D4 | `account_kind` 客户单作用域 **DB 级强约束** 与"账号 N:N 经 membership"存在张力 | 中 | 🟡 待决策（并入 D4/D10 复核） |
| C13 | scope-model:212-239、lifecycle:513-547、target-architecture §7.2 | membership DDL 缺组织归属/`expires_at`；allocation 去重索引缺 `provider_tenant_id` | 中 | 🟡 登记（P1 设计冻结时统一） |
| C14 | scope-model:290-294、lifecycle:432/468 vs canon I11 | 审计字段/事件 source 不统一（缺 membership_id/target_tenant） | 中低 | 🟡 登记（P1） |
| C15 | 06:79-83、01:102、02:136 | 引用 ADR-004 行动项裸用 `A8` 等 | 中低 | ✅ 注册表要求前缀 `ADR-004:A8` |
| C16 | 审计基线自身 | 曾假设"切换不重签 token"；经核对各文档一致为"深度切换重签 JWT + 撤销旧 refresh" | — | ✅ **撤销该条（假阳性）** |
| C17 | workbench:70,76 | 工作台可见性含 **platform** 且开放 `/msp/workbench`，与 canon B2/§2.1（平台管理员无 MSP 身份、无 admin 旁路、仅 `/msp/status` 管理员模式）冲突 | 中高 | 🟡 待决策：平台治理通道是否允许只读跨客户视图（并入 D5/D10 复核） |
| C18 | user-flows:50 vs :63 | 同一文档"三种方式"（总览/头通道/切换）与"四种方式"（工作台/过滤器/头通道/深度切换）并存 | 中 | 🟡 待回填（口径以工作台 `REV-4` 为准） |
| C19 | workbench:224-229/211-216 | 工作台残余局部编号 `R1–R6`（风险）/`A1–A6`（验收）与 canon 重号 | 中 | ✅ 改名 `WB-R1–WB-R6`/`WB-A1–WB-A6`（注册表登记） |

---

## 4. 本次已执行的修复（与提交对应）

1. canon：§7 门控现状/目标分列 + 新增 **R12**（未知模式静默开启 MSP）；P0 ④ 引用；
2. canon：§7.3 `G1–G5` → **`K1–K5`**（消除与 07:G# 重号）；§8 P0 ⑩、修订记录同步；
3. canon：membership 物理表名统一为 **`user_tenant_memberships`**（C7/§4/P1）；
4. canon：新增 **附录 C**（权威层级 + 编号注册表 + A/B 消歧 + 前缀规范）；附录 A 增列 ADR-004 上位关系；头部版本号 `v0.1`→`v0.7`；
5. canon：新增 **D10**（MSP 角色词表统一）；
6. README：核心模型一句话修正（1:1 归属、工作台/过滤器主路径、切换仅深度操作）；01–07 标注 as-is；canon/three-persona 行更新；新增本审计行；维护约定增补编号登记要求；
7. 工作台方案：`R7–R11`→`REV-1–REV-5`、`G1–G6`→`WB1–WB6`、`R1–R6`→`WB-R1–WB-R6`、`A1–A6`→`WB-A1–WB-A6`；
8. user-flows：F-06 更新为"条目级写无需切换"；流程号登记为 `UF-01–UF-10`；
9. login：顶栏口径修正（过滤器为主 + 切换器=深度入口）；局部编号前缀声明；
10. target-architecture / scope-model / lifecycle：新增"2026-09-29 修订指针"横幅（指向工作台 REV-1–REV-5 与登录细化），标注待回填段落；
11. 01–07：新增 as-is 横幅（目标口径以 canon 为准，差异见本审计 §3）。

---

## 5. 待办（需决策或后续排期）

| # | 事项 | 责任/批次 |
|---|---|---|
| T1 | **D10 角色词表统一**：保留代码词表并将 Q7 模板映射为 `observer→msp_viewer`、`full→msp_admin`？或重命名代码词表 | 架构决策（P0 前） |
| T2 | scope-model / lifecycle / target-architecture 正文回填（C2/C3/C13/C14） | P1 设计冻结同批 |
| T3 | `account_kind` 单作用域强约束与 canon A4/D4 的取舍（C12） | 并入 D4 评审 |
| T4 | 02 文档 `/tmp` 指引改为 `$HOME/itsm-artifacts`（G10 方向） | 文档小改（可随时） |
| T5 | 01–07 逐份"现状 vs 目标"差异标注（本轮仅加横幅） | 后续迭代 |
| T6 | 建立文档门禁：新增/修改文档检查（编号登记、状态行与修订记录一致、被修订文档回填） | 工具化建议 |

---

## 6. 防复发（维护约定增补，已并入 README）

1. **头部四件套**：状态（含版本）、日期、权威层级定位、上位/关联文档；
2. **编号三查**：写入前查 canon 附录 C；跨文档引用带前缀；被修订文档必须回填并留指针；
3. **双列表述**：现状与目标必须分列（禁止把目标当现状写，反之亦然）；
4. **单一权威**：概念冲突以 canon 为准；交互/实现冲突以最新修订源为准——两者冲突时先改 canon，再回填。

---

## 附录：审计证据摘要

- 三路并行审计（只读）：① 01–07（7 份）：均 `状态：当前`（2026-09-28），**无一引用 canon**；发现高风险 3 处（saas 门控、未知模式默认开启、default 语义）等；② scope-model + lifecycle（2 份，Draft v0.3）：登录/切换口径与 I8/I9 冲突，角色模板偏离词表，DDL 缺项；③ user-flows + login + workbench + frontend（4 份）：修订未回填（F-06/§4.3/P0）、编号重号密集（R/D/B/F/A/G）。
- 代码核对点：`middleware/msp_gate.go:21-33`（private 关；saas/saas_msp/空/未知 开）；`ent/schema/tenant.go:36-41`（双单值归属字段）；`service/msp_allocation_service.go:70-83`（去重但不校验 provider 归属）。
- 假阳性 1 条（C16）已在 §3 撤销。
