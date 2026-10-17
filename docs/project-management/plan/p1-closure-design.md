# P1 结项清单与 CSAT 详细设计（Closure Checklist & CSAT）

> 状态：**v1.0（2026-10-21 PE-15 落地稿）**
> 范围：结项清单（任务/单据/结项门/复盘 KB/资产登记/成员回收）+ CSAT 触发；`complete` 门禁接入
> 权威前置：[01 模块蓝图](../01-module-blueprint.md) §3「结项门」、[p0-integration-contracts.md](./p0-integration-contracts.md) §4、[p1-gate-design.md](./p1-gate-design.md)
> 对应工作项：PE-15（[06 实施路线](../06-implementation-roadmap.md) §P1）

---

## 1. 目标与范围

**目标**：结项从"状态流转"升级为"清单治理"——项目 `→ completed` 前必须走完**结项清单**（任务清零、关联单据终态、结项门通过、复盘 KB 草稿、资产/许可登记确认、成员回收），结项成功时**自动触发 CSAT 调研**；复盘 KB 只生成草稿（不自动发布），CSAT 缺失不阻塞结项。

**范围内**
- 结项清单读（自动项实时派生 + 人工/产物项落库）；
- 动作：生成复盘 KB 草稿（幂等）/ 资产登记确认或豁免 / 成员回收 / CSAT 触发（幂等）；
- `POST /projects/:id/status {action: complete}` 接入清单门禁（409 + blockers 明细）；
- 结项成功副作用：CSAT 尽力触发（失败不阻塞，可手动重试）；
- 前端工作台「结项清单」Tab 与动作入口。

**范围外**（后续工作项）
- 资产/许可/合同/供应商**台账链接**（PE-26，P2：`project_documents`/资产链接表落地后替代 P1 的人工确认留痕）；
- CSAT 结果回写项目复盘视图、满意度趋势报表（P2 报表批次）；
- 结项通知/邀请投递（`project.survey.invited` 通知事件与偏好词表，随通知批次；P1 先落调研与清单留痕）。

## 2. 决策（新增 D-18 ~ D-20）

| # | 决策点 | 结论 | 依据 |
|---|---|---|---|
| D-18 | 清单落库形态 | **一项目一行 `project_closures`**（同 D-15 阶段门"一项目一记录"先例）；自动项（任务/单据/结项门）**实时派生不入库**（避免与事实源双写漂移），人工/产物项（复盘草稿/资产登记/成员回收/CSAT）落库 | 双写一致性 + 复用 CAS 乐观锁 |
| D-19 | 成员回收范围 | 回收 `project_role=member` 的活跃成员；**保留 `pm` 与 `stakeholder`**（PM 为 `manager_id` 权威且受移除保护；干系人为复盘/CSAT 接收方）；provider 投影成员同样回收，但**不动其 membership 本体**（跨租户 allocation 撤销属 MSP 域，单租户 P1 不越界） | 契约 §3.7 + 04 §2（投影关系在 membership） |
| D-20 | CSAT 是否阻塞结项 | **不阻塞**（以契约 §4「满意度缺失不阻塞结项，登记为待办」为准，覆盖 01 §3 表格中的"CSAT 完成"表述）；结项成功时尽力触发（失败仅日志留痕），并提供幂等手动重试端点 | 契约优先 + 可用性（外部调研依赖不可靠） |

> 与 P0 契约的偏差（按契约 §9 要求回改）：P1 复盘草稿引用落 `project_closures.retro_kb_article_id`；`project_documents.kb_article_id` 属 P2 可选表，落地后由该表/资产链接引用本字段（不重复建设）。

## 3. 数据模型

### 3.1 `project_closures`（P1 新表，一项目一行）

| 列 | 类型 | 说明 |
|---|---|---|
| `tenant_id` / `project_id` | BIGINT NOT NULL | 同租户；唯一 `(tenant_id, project_id)` |
| `retro_kb_article_id` | BIGINT DEFAULT 0 | 复盘 KB 草稿文章ID（`knowledge_articles`，未生成为 0；草稿态不发布） |
| `retro_generated_by` / `retro_generated_at` | BIGINT / TIMESTAMPTZ | 草稿生成留痕 |
| `assets_confirmed_by` / `assets_confirmed_at` | BIGINT / TIMESTAMPTZ | 资产/许可登记确认留痕 |
| `assets_note` | VARCHAR(1024) | 登记备注（如台账编号/清单说明） |
| `assets_waived_by` / `assets_waived_at` | BIGINT / TIMESTAMPTZ | 豁免留痕（确认与豁免互斥，后写清除先写） |
| `assets_waive_reason` | VARCHAR(512) | 豁免原因（必填） |
| `members_recycled_by` / `members_recycled_at` | BIGINT / TIMESTAMPTZ | 成员回收留痕 |
| `members_recycled_count` | INT DEFAULT 0 | 最近一次回收数量（可重复执行以回收新加入成员） |
| `csat_survey_id` | BIGINT DEFAULT 0 | CSAT 调研ID（`surveys`，未触发为 0） |
| `csat_triggered_at` | TIMESTAMPTZ | 触发时间 |
| `version` / `created_by` / `created_at` / `updated_at` | | 乐观锁与审计列 |

迁移兜底：`migrations/20261021_project_management_pe15.sql`（+`_down`）；RLS：`015_project_management_policies.sql` 覆盖清单新增 `project_closures`，`cmd/rls-apply` 受管清单同步。

### 3.2 `survey_responses.project_id`（补齐 P0 DDL §3）

P0 DDL 约定 `survey_response` 加可空 `project_id`（契约 §1-5），P0 批次遗漏，**PE-15 补齐**：ent schema 增列 + 部分索引 `idx_survey_responses_project (tenant_id, project_id) WHERE project_id IS NOT NULL` + 迁移兜底；`dto.SubmitSurveyRequest` 增 `projectId`（与 `ticketId` 并列，均非必填），提交答卷时经 `SetNillableProjectID` 关联。

## 4. 清单项与门禁规则

| # | 清单项 | 类别 | 满足条件 | 阻塞结项 |
|---|---|---|---|---|
| 1 | `tasks` 任务清零 | 自动派生 | 无 `todo/in_progress/blocked` 任务（`done/cancelled` 视为清零） | ✅ |
| 2 | `records` 关联单据终态 | 自动派生 | 五域终态集合（复用 `NonTerminalRecords`：change 终态 `{closed,cancelled,rejected,rolled_back}`，其余域按各自终态） | ✅ |
| 3 | `gate` 结项阶段门 | 自动派生 | `project_gates(closure).status=passed`（PE-12） | ✅ |
| 4 | `retro` 复盘 KB 草稿 | 产物（**结项门提交自动生成** + 手动重试） | `retro_kb_article_id > 0` | ✅ |
| 5 | `assets` 资产/许可登记 | 人工（动作） | 已确认（`assets_confirmed_at`）或已豁免（`assets_waived_at` + 原因） | ✅ |
| 6 | `members` 成员回收 | 动作 | `members_recycled_at` 非空 | ✅ |
| 7 | `csat` CSAT 调研 | 动作（非阻塞） | `csat_survey_id > 0` | ❌（D-20） |

**`complete` 判定顺序**（`Service.Transition`）：
1. `closureBlockers`：未终结任务（`task`）+ 未终结关联单据（逐条 `{type,id,number,status}`）→ 409；
2. `requireGatePassed(closure)`：结项门未通过 → 409 `gate_not_passed`；
3. `requireClosureChecklist`：复盘/资产登记/成员回收未齐备 → 409（`ClosureBlockedError`，`type=retro|assets|members`）；
4. 全部通过 → 状态 CAS `active|suspended → completed`，回填 `actual_end`/`closed_at`；
5. 结项成功后 `autoTriggerCSAT`（尽力而为：清单行不存在则创建；端口缺失/失败仅日志，不影响结项）。

**自动生成点**：`SubmitGate(closure)` 提交成功后调用 `autoGenerateRetroDraft`（尽力而为：端口缺失/失败仅日志，不阻塞门提交；已生成则跳过），因此常规链路里复盘草稿在**结项门提交时**即自动落库（"结项自动生成复盘 KB 草稿"），清单手动按钮用于失败重试/提前生成。

> 清单读（`GET /closure`）会一次性返回**全部**未通过项（含自动项）与摘要 `blockers`（>10 条截断 + `more` 汇总），便于前端一次展示缺口。

## 5. API

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/projects/:id/closure` | `project:read` | 清单（可见性谓词；无记录返回零态视图，不产生写副作用） |
| POST | `/api/v1/projects/:id/closure/retro` | `project:write` | 生成复盘 KB 草稿（幂等：已生成直接回显；常规链路已在结项门提交时自动生成，此处用于重试/提前生成；终态 `cancelled` 拒绝；草稿不发布） |
| POST | `/api/v1/projects/:id/closure/assets` | `project:write` | `{action: confirm\|waive, note?, reason?}`；`waive` 必填原因；终态 `cancelled` 拒绝 |
| POST | `/api/v1/projects/:id/closure/members/recycle` | `project:write` | 成员回收（可重复执行；返回最近一次回收数量） |
| POST | `/api/v1/projects/:id/closure/csat` | `project:write` | CSAT 触发（幂等：已触发回显；结项自动触发失败后手动重试入口） |

**错误语义**：不可见/跨租户 404（不泄露存在性）；非法动作/缺原因 400；`cancelled` 项目动作 400；端口未注入（bootstrap 未装配）503；清单门禁 409（`common.ConflictCode` + `{blockers, total}`）。

**审计**（`auditlog`，`resource=project`）：`closure_retro_generate`、`closure_assets_confirm` / `closure_assets_waive`、`closure_members_recycle`、`closure_csat_trigger`、`closure_blocked`（complete 被清单/业务门禁拒绝时记录阻塞摘要，最多 5 条）。

**端口与装配**（`handlers/project/closure_ports.go`）：
- `ClosureKnowledgePort.CreateRetroDraft` → 知识库域服务 `handlers/knowledge.Service`（分类校验 + 消毒 + **新建即草稿**）；分类 `项目复盘`（加入 `service.IsWhitelistedKnowledgeCategory` 白名单）；
- `ClosureSurveyPort.CreateCSATSurvey` → `service.SurveyService.CreateSurvey`（`survey_type=CSAT`，1-5 分评分 + 开放式意见，`is_active=true`，30 天窗口）；
- bootstrap：`projectService.SetClosurePorts(NewClosureKnowledgeAdapter(knowledgeServiceDomain), NewClosureSurveyAdapter(surveyService))` + `SetLogger`。

## 6. 复盘草稿内容（自动生成，人工补充后发布）

Markdown 草稿（`content_type=markdown`），标题 `项目复盘：<code> <name>`，标签 `[项目复盘, <code>]`，正文结构：

```
# 项目复盘：<code> <name>
> 本草稿由结项清单自动生成（PE-15），请补充「复盘要点」后人工发布；系统不会自动发布。

## 项目概述（存在描述时）
## 执行事实
- 项目类型 / 计划周期 / 实际起止
- 任务：N 个（完成 x / 取消 y / 未完成 z）
- 里程碑：N 个（达成 m）
- 关联单据：N 个
- 阶段门：立项=passed / 计划=passed / 验收=passed / 结项=passed

## 复盘要点（待人工补充）
- 交付结果 / 偏差与原因（对照计划基线）/ 改进项 / 可沉淀的知识与模板
```

## 7. 前端（项目工作台「结项清单」Tab）

- Tab 标题计数：`结项清单 (阻塞项已通过/阻塞项总数)`；
- 工具栏：刷新 + 清单状态标签（`pending|ready|completed|cancelled`）+ 说明（阻塞项全通过后用页头「结项」完成；复盘不自动发布、CSAT 不阻塞）；
- 阻塞项 `Alert`：一次列出全部未通过项（含单据编号/任务数等）；
- 清单表：清单项（标题 + 阻塞/非阻塞标签 + 提示）→ 状态 → 说明 → 操作（生成复盘草稿 / 确认登记 / 豁免 / 执行成员回收 / 手动触发 CSAT）；只读角色与终态项目仅展示；
- 资产登记弹窗：确认（备注可选）/ 豁免（原因必填，危险按钮）；
- 成员回收二次确认弹窗；结项后自动刷新清单（CSAT 触发结果可见）。

## 8. 实现映射（文件清单）

| 层 | 文件 |
|---|---|
| 数据层 | `ent/schema/project_closure.go`、`ent/schema/survey_response.go`（+`project_id`）、`migrations/20261021_project_management_pe15.sql`（+`_down`）、RLS 015、`cmd/rls-apply/main.go` |
| 域/服务 | `handlers/project/closure_entity.go`、`closure_repository_impl.go`、`closure_service.go`、`closure_ports.go`、`closure_handler.go`、`service.go`（清单门禁 + 结项 CSAT 副作用）、`gate_service.go`（结项门提交自动生成复盘草稿）、`handler.go`（错误映射 + 结项拒绝审计） |
| 路由/装配 | `router/common_system_routes.go`（5 端点）、`internal/bootstrap/app.go`（端口注入）、`middleware/rbac_precheck_gen.go`（重生成）、`docs/acl-manifest.yaml`（重生成，919 路由） |
| 集成 | `dto/survey_dto.go`（`projectId`）、`service/survey_service.go`（`SetNillableProjectID`）、`service/knowledge_service.go`（分类白名单 `项目复盘`） |
| 前端 | `lib/services/project-service.ts`（5 方法 + 类型/顺序常量）、`pages/(main)/projects/detail.tsx`（结项清单 Tab + 动作/弹窗） |
| 测试 | `handlers/project/pe15_test.go`（4 用例）、`handler_test.go`（端口注入 + 路由）、`service_test.go`/`pe12_test.go`（结项前置清单适配）、`lib/services/__tests__/project-service.test.ts`（+5） |

## 9. 验证证据（2026-10-21）

| 验证项 | 命令 | 结果 |
|---|---|---|
| 结项清单全链路（派生/结项门提交自动生成复盘草稿/动作/门禁/CSAT 自动触发/幂等） | `go test ./handlers/project/ -run Closure -v -count=1` | ✅ `TestClosureChecklistFlow` PASS（断言门提交后 `retro.status=done` 且端口仅调用 1 次） |
| 隔离与守卫（跨租户/不可见 404、`cancelled` 400、端口未注入 503、豁免必填原因） | 同上 | ✅ `TestClosureIsolationAndGuards` PASS |
| 端口故障韧性（复盘失败保持 pending；CSAT 失败不阻塞结项、可重试） | 同上 | ✅ `TestClosurePortFailuresDoNotBlockCompletion` PASS |
| 端点级（信封/清单项/审计留痕/409/400/404、终态只读） | 同上 | ✅ `TestHandlerClosureEndpoints` PASS |
| 全包回归（含既有用例适配） | `go test ./handlers/project/ -count=1` | ✅ ok |
| 路由权限预检（5 端点） | `go test ./middleware/ -run Precheck -count=1` | ✅ ok（重生成后） |
| 路由守卫全量 | `go test ./router/ -count=1` | ✅ ok |
| 迁移/种子/服务回归（survey/knowledge/bpmn/模板） | `go test ./migration/ ./pkg/seeder/ ./service/ -run "Survey\|Knowledge\|Builtin\|Binding\|Trigger\|Template\|Migrat" -count=1` | ✅ 三包 ok |
| 定向构建 | `go build -p 2 ./handlers/project/ ./handlers/knowledge/ ./service/ ./router/ ./internal/bootstrap/` + `-tags rlsapply ./cmd/rls-apply/` | ✅ exit 0 |
| 授权生成与清单 | `go run ./cmd/authz-gen` + `node scripts/generate-acl-manifest.js` | ✅ **919 路由**（+5） |
| 前端类型/静态 | `npx tsc --noEmit`、改动文件 `eslint` | ✅ 0 error / 0 warning |
| 前端单测 | `npx jest src/lib/services/__tests__/project-service.test.ts` | ✅ **31/31**（+5 结项用例） |
| 前端 API 路径核对 | `node scripts/check-api-paths.js` | ✅ **1052/1052**（+5） |
| 前端生产构建 | `npm run build` | ✅ exit 0 |
| 覆盖率守卫映射 | `node scripts/test-coverage-guard.js --base origin/main`（Windows cmd 引号问题经 `.dev/tmp-guard.js` 包装执行） | ⚠️ 44 条缺失均为**存量**（ai_telemetry/msp_*/tool_*/lib/api 等）；本次改动文件零命中 |

> 备注：`npm run lint:antd` 为仓库级存量违规检查（42 处，均在既有文件），本批改动文件零新增。

## 10. 后续（P1 收口与 P2）

- **P1 收口**：真实库迁移（`20261021_*` + RLS 015 重跑）+ `rls-apply -verify` + 结项清单 HTTP E2E + 浏览器 UI 冒烟（「结项清单」Tab 六动作）；
- **P2/PE-26**：资产/许可/合同/供应商台账链接（`project_documents`/链接表），把「资产登记确认」从人工留痕升级为结构化链接；
- **P2 报表**：CSAT 结果进项目复盘视图（`survey_responses.project_id` 已就绪）与组合度量（按时交付率/里程碑达成率/阶段门一次通过率）。