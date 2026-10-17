# P1 详细设计：项目模板（PE-11）

> 状态：**已实现 v1.0（2026-10-19）**
> 范围：模板 CRUD + 一键生成（阶段/任务/检查单/里程碑，幂等）
> 依据：[01 模块蓝图 §P1](../01-module-blueprint.md)｜[06 实施路线 PE-11](../06-implementation-roadmap.md)｜[07 §2-11 检查单=任务子项](../07-completeness-review.md)｜[plan/p0-api-and-pages.md §4](../plan/p0-api-and-pages.md)

---

## 1. 定位与决策

**一句话**：模板是"方法论可复制"的载体——交付标准（阶段/任务/检查单/里程碑）沉淀一次，任何项目一键生成计划，重复执行不产生重复数据。

| # | 决策点 | 结论 | 依据 |
|---|---|---|---|
| T-1 | 目标形态 | **应用到已有项目**（不做"从模板创建项目"：项目主数据仍走既有创建流程） | 复用 P0 创建/权限/可见性，模板只负责计划面 |
| T-2 | 幂等语义 | **一项目一模板**：`projects.template_id` 非空即已应用；重复调用 409 `TEMPLATE_ALREADY_APPLIED`（**不产生任何重复数据**） | CAS 认领 + 唯一闸门，天然抗并发；避免多模板叠加导致计划失控 |
| T-3 | 生成映射 | 阶段 → L1 分组任务；任务 → L2；检查单 → L3 检查项子任务（不建新实体） | WBS ≤3 层既有约束 + 07 §2-11「检查单=任务子项」 |
| T-4 | 日期语义 | payload 只存**相对偏移**（`offsetDays`/`startOffsetDays`/`durationDays`），应用时按锚点换算；锚点 = 项目 `start_date`（缺失时 = 应用当日，UTC 自然日） | 模板可跨项目复用；与基线/偏差的日历口径一致（D-12 自然日） |
| T-5 | 生命周期 | `draft`（可编不可用）→ `active`（可应用）→ `archived`（只读归档）；**仅 active 可应用**（409） | 对齐工单类型 preset 的"启用"模式；防半成品模板误用 |
| T-6 | 事务边界 | 认领 + 全部生成在同一事务；任何一步失败整体回滚（无半成品项目） | 幂等闸门与生成必须原子 |
| T-7 | 删除语义 | 软删；**被项目应用时拒绝删除（409，引导归档）**，防止 `template_id` 悬空 | 与项目 code 删除门禁同构 |
| T-8 | MSP 分发 | `project_template_distributions`（copy-in 下发）属 P2+ 的 [05 §4](../05-msp-delivery.md)，本设计不引入分发字段 | 分发是复制定义，不是共享记录（05 原则） |

## 2. 数据层

### 2.1 新表 `project_templates`（ent：`project_template.go`）

| 列 | 类型 | 说明 |
|---|---|---|
| `tenant_id` | BIGINT NOT NULL | 租户隔离（RLS 015 覆盖清单已含本表） |
| `name` | VARCHAR(128) NOT NULL | 模板名称（租户内唯一，软删不释放） |
| `description` | TEXT NULL | 描述 |
| `type` | VARCHAR(16) DEFAULT 'internal' | 适用项目类型 `internal\|improvement\|delivery` |
| `status` | VARCHAR(16) DEFAULT 'draft' | `draft\|active\|archived` |
| `payload` | JSONB | 模板定义（§2.3） |
| `version` | INTEGER DEFAULT 1 | 乐观锁 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | | 审计与软删 |

索引：`uq_project_templates_tenant_name (tenant_id, name)` 唯一；`idx_project_templates_tenant_status (tenant_id, status)`。

### 2.2 `projects` 新增列（模板应用面）

| 列 | 类型 | 说明 |
|---|---|---|
| `template_id` | BIGINT NULL | 已应用模板 ID；**非空 = 已应用（幂等闸门）** |
| `template_applied_at` | TIMESTAMPTZ NULL | 应用时间 |
| `template_applied_by` | BIGINT NULL | 应用人 |

迁移兜底：`itsm-backend/migrations/20261019_project_management_pe11.sql`（+`_down`，幂等 `ADD COLUMN IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`）；RLS 015 覆盖清单与 `rls-apply` 受管清单同步。

### 2.3 payload 结构（JSONB）

```json
{
  "milestones": [{ "name": "验收通过", "offsetDays": 30 }],
  "tasks": [
    {
      "stage": "实施",
      "name": "环境部署",
      "description": "可选说明",
      "priority": "high",
      "startOffsetDays": 5,
      "durationDays": 3,
      "estimateHours": 8,
      "checklist": ["备份完成", "回滚演练"]
    }
  ]
}
```

限额（超限 400）：里程碑 ≤50、任务 ≤200、检查项/任务 ≤20；名称 ≤200 字、阶段名 ≤100 字、描述 ≤2000 字；偏移/工期 ≤3650 天；payload ≤64KB。

## 3. 生成规则

| 模板元素 | 生成物 | 说明 |
|---|---|---|
| `stage` 分组（非空） | L1 分组任务（名称=阶段名） | 阶段按任务**出现顺序**去重排序；`sort_order` 记录次序 |
| 无 `stage` 的任务 | 直接挂项目根（L1） | 混合模板允许 |
| 任务 | L2 子任务 | `plan_start = 锚点 + startOffsetDays`；`plan_end = plan_start + durationDays`（仅给 duration 时以锚点起算）；`priority` 缺省 `medium` |
| `checklist[]` | L3 检查项子任务 | 名称=检查项文本；`status=todo`；不设计划日期 |
| `milestones[]` | 里程碑 | `due_date = 锚点 + offsetDays`；`status=planned` |

生成的任务 `created_by = 应用人`；全部字段经服务层复校验（payload 在创建/更新/应用三处均校验，防 DB 直改脏数据）。

## 4. API（6 端点，权限沿用 `project:read/write`）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/projects/templates?status=&type=&q=&page=&pageSize=` | `project:read` | 列表（信封 `items` + 别名 `projectTemplates`；**省略 payload**，仅给计数） |
| POST | `/api/v1/projects/templates` | `project:write` | 创建（`status` 缺省 `draft`） |
| GET | `/api/v1/projects/templates/:templateId` | `project:read` | 详情（含 payload） |
| PUT | `/api/v1/projects/templates/:templateId` | `project:write` | 更新（nil 不改；`version` 可选乐观锁；`status` 显式切换） |
| DELETE | `/api/v1/projects/templates/:templateId` | `project:write` | 软删（被应用 → 409 `hint=archive_instead`） |
| POST | `/api/v1/projects/:id/templates/apply` | `project:write` | 一键生成（body `{"templateId": 1}`） |

错误语义（`common.AppError` 家族）：

| 场景 | HTTP | 说明 |
|---|---|---|
| 模板不存在 / 他租户 | 404 | 不泄露存在性 |
| 项目不可见（非成员且非管理） | 404 | 可见性谓词（契约 §1-11） |
| 名称重复 | 409 | `ErrTemplateNameExists` |
| 模板未激活（draft/archived） | 409 | 提示先激活 |
| 项目已应用（含并发竞态） | 409 | data：`templateId/templateName/appliedAt/hint=apply_once_per_project` |
| 模板被应用后删除 | 409 | data：`hint=archive_instead` |
| 乐观锁冲突 | 409 | data：`currentVersion` |
| 终态项目 / payload 非法 | 400 | 与里程碑/基线同口径 |
| payload 空 / 无任务与里程碑 | 400 | `ErrValidation` |

## 5. 并发与幂等（实现要点）

1. 服务层前置检查（项目可见 → 非终态 → 未应用 → 模板 active → payload 校验）；
2. 仓储 `ApplyTemplateTx`：`UPDATE projects SET template_id, template_applied_at, template_applied_by WHERE id=? AND tenant_id=? AND deleted_at IS NULL AND template_id IS NULL` —— **CAS 认领**，影响行数 0 → `ErrTemplateAlreadyApplied`（竞态由 DB 收敛，不依赖应用层检查）；
3. 同一事务内生成阶段/任务/检查项/里程碑；失败整体回滚（认领一并回滚，项目保持未应用）；
4. 测试证据：`handlers/project/pe11_test.go`（`TestTemplateCRUDAndApply` / `TestHandlerTemplateEndpoints`）覆盖幂等（重复应用后任务数不变）、跨租户模板 404、可见性 404、终态 400、CAS 409、删除门禁 409、列表过滤与软删。

## 6. 前端（已落地，2026-10-19）

| 页面 | 路由 | 已实现要点 |
|---|---|---|
| 模板管理 | `/projects/templates` | 列表（状态/类型/关键字 + 服务端分页 + 计数列）；新建/编辑弹窗（里程碑 + 任务 Form.List 行编辑；任务含阶段/优先级/开始偏移/工期/估算/检查项逗号分隔；保存转 payload）；draft→激活 / active→归档 / archived→重新激活 / 删除（被应用提示改为归档） |
| 项目列表 | `/projects` | 头部「模板管理」入口 |
| 项目工作台 | `/projects/:id` | 头部「应用模板」（仅 `templateId` 为空且项目非终态时可见；弹窗仅列 active 模板并显示计数，成功后回显生成计数并刷新项目/任务/里程碑）；概览「已应用模板」显示模板名 + 应用日期（模板已删时降级 `#ID`） |

登记点（对齐 plan/p0-api-and-pages.md §4）：`route-paths.ts`、`routes/index.tsx`、`route-permissions.ts`（只读面 `project:read`；写操作页内 `project:write` 门禁）。
> 实现说明：模板管理页路由以只读数据面 `project:read` 登记（与后端 GET 权限一致），页内写操作由 `useCan(['project:write'])` 门禁；菜单入口复用项目列表页头部按钮（未新增菜单项，避免一级菜单膨胀）。

## 7. 验证记录（2026-10-19）

| 项 | 结果 |
|---|---|
| `go test ./handlers/project/`（含 PE-11 用例） | ✅ |
| `go test ./middleware/ -run Precheck` | ✅ |
| `go test ./router/ -run "Permission\|Precheck\|PlatformFace"` | ✅ |
| `go build ./handlers/project/ ./router/ ./common/ ./middleware/` | ✅ |
| `cmd/authz-gen` 预检重生成 | ✅ |
| `node scripts/generate-acl-manifest.js` | ✅ 911 路由（+6）；模板 6 端点全部挂权限（`--check` 报的 59 条缺失均为 bots/mcp/auth/llm 存量路由，与本批无关） |
| 前端 `npm run type-check` | ✅ |
| 前端切变文件 `eslint` | ✅ 0 error / 0 warning |
| `project-service` 单测 | ✅ 22/22（新增模板 6 用例） |
| `route-permissions` 守卫 | ✅ 9/9（新路由映射命中真实路由） |
| `node scripts/check-api-paths.js` | ✅ 1044/1044 |
| 前端 `npm run build` | ✅（`lint:antd` 存量违规均在既有文件，本批零命中） |

## 8. 非目标（沿用 01 §1.3 / 07）

- 不做"从模板创建项目"（T-1）；不做模板版本回滚/差异对比（P2 评估）；
- 不做领取（checkout）/审批分发（MSP 场景见 05 §4）；不做甘特/关键路径联动（依赖 v1 仅 FS）；
- 检查单不建独立实体（07 §2-11），检查项即 L3 任务子项。