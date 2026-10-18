# P3 · 写入完整性校验与口径归一（PE-28）

> 状态：**设计 v1.0（2026-10-18）**；实现与验证证据同日落地，见 §6。
> 上游：2026-10-18 模块评审 P0-2「健康度计算规则 + 字段校验（负责人、归口、日期、币种）」；健康度部分已由 [p3-health-design.md](./p3-health-design.md)（PE-27）落地，本设计收敛**字段校验**部分。
> 定位：写侧守卫——把"可被引用 / 可被信任"的字段口径定死（fail-closed）；零新表、零新端点、零迁移。

---

## 1. 评审缺口核验（P0 全表）

| 评审缺口（P0） | 核验结果 | 处置 |
|---|---|---|
| 任务 / WBS / 里程碑 / 工时填报 | 已覆盖（PE-4 / PE-14 / PE-22） | 无需开发 |
| 健康度计算规则 | PE-27 已落地（9 条规则 + `healthReasons`） | ✅ 2026-10-18 |
| 字段校验 · **负责人** | **未校验**：可写入不存在 / 跨租户 / 已失效用户 | **D-60** |
| 字段校验 · **归口** | 已覆盖（PE-17：同租户 / 未软删 / active） | 无需开发 |
| 字段校验 · **日期** | 项目级空档（任务级已有 `planEnd ≥ planStart`） | **D-62** |
| 字段校验 · **币种** | 仅 trim + 长度；无字符集 / 大小写口径 | **D-63** |
| 字段校验 · 部门（核验新发现） | **未校验**：可写入不存在 / 跨租户 / 已删部门 | **D-61** |
| 状态机扩充（进行中 / 暂停 / 终止 / 归档）+ 验收把关 | 已覆盖，见 §5 核验说明 | 记录结论 |
| 「计划结束日期缺失仍可完成」 | 计划日期按设计可空；PE-24 已按 `noPlanDate` 单独归口 | 评估不采纳，见 §5 |

## 2. 决策记录（D-60~D-63）

| 编号 | 决策 | 依据 |
|---|---|---|
| D-60 | **负责人**：`managerId > 0` 时必须为**同租户活跃 membership**（未软删 + status=active + 未过期），否则 400 `ErrManagerUnavailable`；≤0 = 未设置（创建）/ 不修改（更新，沿用 `optionalInt` 语义）。**不要求先加入项目成员**（立项门负责任命把关） | 复用 `ActiveMembership`（与「加入成员」前置校验同源）；防悬空引用污染立项门 / 通知候选 / 组合分组 |
| D-61 | **部门**：`departmentId > 0` 时必须同租户且未软删，否则 400 `ErrDepartmentUnavailable`；≤0 = 未设置（创建）/ 不修改（更新） | 与归口（PE-17 同构校验）对齐；防组合「按部门」维度出现不可解析引用 |
| D-62 | **日期**：`startDate` / `endDate` 两端齐备（更新路径按**合并存量后**的有效值）时要求 `endDate ≥ startDate`，否则 400；计划日期保持可空（单端可存在，不引入必填） | 与任务级 `planEnd ≥ planStart` 口径对齐；快照 / 偏差 / 健康度以日期为输入，逆序区间使口径不可信 |
| D-63 | **币种**：trim + **大写归一**（存储与展示口径统一）；非空时必须匹配 `^[A-Z]{2,8}$`，否则 400；空串 = 租户默认（D-33 成对清空语义不变，不做币种主数据） | 展示 / 基线预算比对（PE-27 `budget_deviation`）需要稳定的币种口径；保持"轻量、非财务级" |

补充口径：

- 上述校验均只约束**写入**；存量数据不迁移、不回填。
- 更新接口 `nil` = 不修改；负责人 / 部门 `≤0` 沿用 `optionalInt` 语义 = 不修改（本轮不新增清空能力）；两者均不触发引用校验。
- 日期校验在并发窗口内以"校验时点"为准（与 code 唯一性同为软校验，非 DB 约束）。

## 3. 契约影响

| 面 | 变更 |
|---|---|
| `POST /projects`、`PUT /projects/:id` | 新增 4 类 400（param error）：负责人不可用 / 部门不可用 / 日期区间非法 / 币种非法 |
| 其余端点、权限、RLS、迁移、Swagger 结构 | 零变更 |
| 前端 | 新增共享 `formatBudget` 展示（列表 / 详情同源）；提交时币种 trim + 大写（与 D-63 一致） |

## 4. 错误映射

| 场景 | 服务层 | HTTP |
|---|---|---|
| managerId 非本租户活跃成员 | `ErrManagerUnavailable` | 400「项目经理不可用（须为本租户活跃成员）」 |
| departmentId 不存在 / 跨租户 / 软删 | `ErrDepartmentUnavailable` | 400「所属部门不可用（不存在/已删除）」 |
| endDate < startDate（创建或更新合并后） | `ErrValidation` + `endDate must not be before startDate` | 400 |
| 币种非 `^[A-Z]{2,8}$` | `ErrValidation` + `budget currency must be 2-8 letters (e.g. CNY)` | 400 |

## 5. 核验说明（状态机 / 验收把关 / 完成门槛）

- 状态词表 `planning|active|suspended|completed|cancelled` + 动作 `activate/suspend/resume/complete/cancel` 已覆盖评审所列「进行中 / 暂停 / 终止」；「归档」语义由软删承担（列表默认不可见 + 删除门禁防误删）。
- 验收把关：四道阶段门（立项 / 执行 / 验收 / 结项）已含验收门；`activate`（立项门）与 `complete`（结项门）为强制点，其余门记 warning 不阻断。
- 「已完成须有计划结束日期」**评估不采纳**：计划日期系统内可空（快速立项 / 模板场景），PE-24 按时交付率已按 `noPlanDate` 单独归口；若未来强制，应作为可配置门禁另立需求，避免破坏既有数据与演示链路。

## 6. 测试矩阵与验证证据

| 层 | 用例 |
|---|---|
| 负责人 | 无 membership → 400；软删 / 过期 → 400；跨租户 → 400；合法成员 → 通过；更新清空（0）不校验 |
| 部门 | 不存在 / 软删 / 跨租户 → 400；合法 → 通过 |
| 日期 | 创建逆序 → 400（同日通过）；更新单端合并存量：start > 存量 end → 400、end < 存量 start → 400；合法通过 |
| 币种 | 小写 → 大写存储；`cn`（<2）/ `CNY$` → 400；空串与金额成对清空（D-33 回归）；`" USD "` → `"USD"` 回归 |
| 端点 | 四类 400 的 HTTP 映射与消息 |
| 回归 | `go test ./handlers/project/ -count=1 -p 1`、`go vet ./handlers/project/` |

验证证据（2026-10-18，本机实跑）：

| 项 | 结果 |
|---|---|
| `go test ./handlers/project/ -run 'TestManagerWriteValidation|TestDepartmentWriteValidation|TestProjectDateRangeValidation|TestBudgetCurrencyNormalization|TestWriteValidationEndpoints' -count=1` | ✅ ok（5 用例：负责人 membership 矩阵 / 部门引用 / 日期合并校验 / 币种归一与字符集 / 端点 400 映射与合法写入） |
| `go test ./handlers/project/ -count=1 -p 1`（全包回归） | ✅ ok 4.452s |
| `go vet ./handlers/project/` | ✅ exit 0 |
| `gofmt -l`（6 个改动 Go 文件） | ✅ 无输出（全部 gofmt-clean） |
| 前端 `npm run type-check` / 改动文件 `eslint` / `budget`+`project-service` 单测 **65/65**（2 suites） | ✅ |
| `check-api-paths`（仓库根） | ✅ **1075/1075**（Matched 1075 / Missing 0） |
| `npm run build` | ✅ built in 49.43s |
| 迁移 / RLS / ACL / Swagger | 无变更（零新表、零新端点） |

## 7. 后续候选（评审核验后，未排期）

列表运营面（排序 / 列配置 / 批量 / 导出 / 变更历史）、CMDB CI 关联、组合优先级排序、AI 计划生成 / 工期预估。

## 8. 修订记录

| 日期 | 变更 |
|---|---|
| 2026-10-18 | 初版 v1.0：D-60~D-63 + P0 评审核验表 + 错误映射 + 测试矩阵 |
