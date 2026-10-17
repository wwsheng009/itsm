# P2 详细设计 · PE-21 风险/问题台账（`project_risks`）

> 状态：**Draft v1.0（实施中）**
> 更新日期：2026-10-24
> 上游：[02 §3.5](../02-data-model.md)、[03 §2.2](../03-integration.md)、[06 §2/§4.3](../06-implementation-roadmap.md)、[07 §4.2](../07-completeness-review.md)
> 范围：风险/问题台账 + **转问题单/变更链路**（P2 验收第 1 条：「风险台账可转问题单/变更，链路审计完整」）

---

## 1. 目标

1. 项目内维护 `kind=risk|issue` 台账：标题/描述、等级、概率、影响、负责人（项目活跃成员）、状态机、结论（resolution）；
2. 风险/问题可**升级为 ITSM 问题单**或**变更单**（跨模块写，链路可追溯、可审计、幂等防护）；
3. 与既有机制一致：可见性谓词（管理角色 ∨ 项目成员，不可见 404）、终态项目拒绝写、软删、乐观锁、审计（`resource=project`）。

## 2. 决策（本设计新增）

| # | 决策点 | 结论 | 依据 |
|---|---|---|---|
| **D-26** | 表形态 | **单表 `project_risks`**（`kind` 区分 risk/issue），不拆两表 | [02 §3.5](../02-data-model.md) 权威定义；两类字段与状态机同构 |
| **D-27** | 状态机与关闭语义 | `open →(mitigate) mitigating`、`mitigating →(close) closed`、`open →(close) closed`、`closed →(reopen) open`；**`close` 必填 `resolution`** | 风险治理实践：关闭=有结论；复盘需要可重开（审计留痕） |
| **D-28** | 转换链路 | `POST /projects/:id/risks/:riskId/convert {target: problem\|change}`；**端口 + 适配器**（bootstrap 注入，未装配 503）；成功后回写 `related_problem_id` / `related_change_id`；**同目标二次转换 409 + `existingId`** | [03 §2.2/§2.3](../03-integration.md)；PE-5 任务→工单同先例（跨模块写走服务层校验 + 审计） |
| **D-29** | 转换字段映射 | 问题：`title`/`description`（附来源：项目 `<code>` 风险 `#<id>`）+ `priority` 由 `level` 映射（low→low / medium→medium / high→high）；变更：`title`/`description` + `riskLevel` 同映射 + `justification` 附来源 | 保持最小复制、可审计；不复制字段族 |
| **D-30** | 终态项目 | `completed`/`cancelled` **拒绝写**（400「project is closed」）；读放行（只读回看） | 与里程碑/任务既有语义一致 |
| **D-31** | 负责人约束 | `owner_id` 必须为项目活跃成员（`ensureAssignee` 同口径）；`0/缺省` = 未指派 | 复用 PE-14 成员有效性联动 |
| **D-32** | 工单关联 | `related_ticket_id` 为**弱引用**（同租户、不校验终态）；由 `PUT` 维护 | 02 §3.5 原义；不做工单反向约束 |

## 3. 数据模型 `project_risks`（P2）

```
tenant_id          int 必填
project_id         int 必填
kind               string  risk|issue（默认 risk）
title              string 必填 ≤255
description        text 可空
level              string  low|medium|high（默认 medium）
probability        string  low|medium|high 可空
impact             string  low|medium|high 可空
owner_id           int 可空（项目活跃成员）
status             string  open|mitigating|closed（默认 open）
resolution         text 可空（close 时必填）
related_ticket_id  int 可空（同租户弱引用）
related_problem_id int 可空（转换产物）
related_change_id  int 可空（转换产物）
created_by         int
version            int 默认 1（乐观锁）
deleted_at         time 可空（软删）
created_at / updated_at
索引：(tenant_id, project_id, status) / (tenant_id, project_id, level)
```

- 迁移兜底：`itsm-backend/migrations/20261024_project_management_pe21.sql`（+`_down`）；
- RLS：`015_project_management_policies.sql` 覆盖清单 + `cmd/rls-apply` 受管清单追加 `project_risks`；
- 分页别名：`projectRiskResponse → projectRisks`。

## 4. API（8 条，全部 `project` 权限码）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/projects/:id/risks` | 列表（`status`/`kind`/`level` 过滤；可见性谓词） |
| POST | `/projects/:id/risks` | 创建（终态项目 400；owner 校验） |
| GET | `/projects/:id/risks/:riskId` | 详情（可见性谓词） |
| PUT | `/projects/:id/risks/:riskId` | 更新（不含状态；`version` 乐观锁 409） |
| DELETE | `/projects/:id/risks/:riskId` | 软删（已转换记录仍可删，关联实体不受影响） |
| POST | `/projects/:id/risks/:riskId/status` | 状态流转（`mitigate\|close\|reopen`；close 必填 reason/resolution） |
| POST | `/projects/:id/risks/:riskId/convert` | 转问题单/变更（`target=problem\|change`；幂等 409） |

审计（`resource=project`）：`risk_create` / `risk_update` / `risk_delete` / `risk_status` / `risk_convert`，metadata 含 `riskId`/`projectId`/`kind`/`level`/`target`。

## 5. 转换契约（端口 + 适配器）

```go
// 域内端口（risk_ports.go），bootstrap 装配；未装配 → 503。
type RiskProblemPort interface {
    CreateProblemFromRisk(ctx context.Context, tenantID, actorID int, in RiskConvertInput) (int, error)
}
type RiskChangePort interface {
    CreateChangeFromRisk(ctx context.Context, tenantID, actorID int, in RiskConvertInput) (int, error)
}
```

- **时序**：校验（可见性/终态/已转换）→ 调端口创建 → CAS 回写 `related_*_id` → 审计。
- **回写失败**（极端并发）：返回 500 并提示「单据已创建（#id），请人工关联」；下一次 convert 前先查询 `related_problem_id`/`related_change_id` 防重复。
- **适配器**：`problem.Service.Create` / `change.Service.CreateChange`（生产链路；适配器内做复用字段映射，见 D-29）。

## 6. 前端（项目工作台）

- 新增「风险」Tab：列表（等级/状态 Tag、kind、负责人、关联问题/变更跳转）、新建/编辑弹窗、状态动作（缓解/关闭/重开；关闭弹窗必填结论）、转换对话框（选问题/变更 → 成功后展示产物链接）；
- `project-service.ts`：新增 6 方法 + 类型（`ProjectRisk` 等）；
- 路由/守卫无新增页面（工作台内 Tab）。

## 7. 验证清单（实施后回填证据）

| 项 | 命令/方式 | 结果 |
|---|---|---|
| ent 生成 | `go generate ./ent` | ✅ exit 0（`project_risks` 生成物） |
| 单测 | `go test ./handlers/project/ -run "Risk" -count=1` | ✅ 3 用例（工作流/转换/端点） |
| 回归 | `go test ./handlers/project/ ./middleware/ -count=1`、`go test ./router/ -run "Permission\|Precheck\|PlatformFace\|Menu"` | ✅ 全绿 |
| 生成物 | `go run ./cmd/authz-gen` + `node scripts/generate-acl-manifest.js` | ✅ **934 路由**（+7 风险端点） |
| 前端 | `npx tsc --noEmit` / 改动文件 `eslint` / `project-service` 单测 / `check-api-paths` / `npm run build` | ✅ tsc 0 错、eslint 0、单测 44/44、路径 1060/0、build ✅ |
| 真实库 | 迁移 + RLS + 探针（P2 收口批次统一执行） | ⏳ P2 收口 |

> 实施记录（2026-10-24）：后端 `handlers/project/risk_{entity,service,ports,handler}.go` + `ent/schema/project_risk.go` + 迁移 `20261024_project_management_pe21.sql`（+down）+ RLS 015/rls-apply 清单 + 分页别名 + 路由 7 条 + bootstrap 端口装配；前端 `ProjectRiskPanel.tsx`（工作台「风险/问题」Tab）+ `project-service` 7 方法 + 单测 6 例。
