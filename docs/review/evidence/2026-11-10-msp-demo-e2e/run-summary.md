# MSP Demo E2E Run Summary · 2026-11-10

## 环境

| 项 | 值 |
|---|---|
| Base URL | `http://172.18.3.238:8089` |
| Version | `1.8.0-dev.f7dd2974`（部署 worktree commit `f7dd29743`） |
| Deployment mode | `saas_msp` |
| Browser | Chrome（Playwright `channel: chrome`） |
| Worker | `1`（规避登录限流） |

## 运行命令

```bash
cd itsm-frontend
PLAYWRIGHT_BASE_URL=http://172.18.3.238:8089 \
PLAYWRIGHT_DEMO_CHANNEL=chrome \
npx playwright test -c playwright.demo.config.ts tests/e2e/demo --workers=1
```

## 结果

```text
Running 39 tests using 1 worker
39 passed (2.8m)
```

> 0 个 expected failure；D1–D15 的回归断言均为普通用例。

> 已移除旧的 known-defect 容忍分支：跨租户删除/问题关联不再接受 500，事件转换后必须返回
> `problemId`，L1/L2/L3 审批必须 200，D10 删除后 GET 必须 404，Dashboard/AI 不再允许 403 白名单。

## 发布与部署后验证

238 已发布 `1.8.0-dev.f7dd2974`：

- 6 个常驻容器（backend/worker/frontend/nginx/redis/ai-service）healthy；`itsm-init-demo` exit=0。
- `/healthz`、`/readyz` ready；`/api/v1/version` 返回 `f7dd2974`。
- `/home/n9e/itsm/repo-e2e-6745c998` 部署 commit 已固定为 branch `deploy/e2e-20261111-f7dd2974`。
- 全量 demo E2E `39/39 passed`；发布前的预跑记录已由本结果取代，不再作为当前状态引用。

> 发布前曾用本地生产构建 + 238 后端验证前端门控与布局修复；最终验证据以部署后的 238 全量结果为准。

## 轮次与覆盖

| 轮次 | 覆盖 | 结果 |
|---|---|---|
| Round 0 | 环境/版本/ready、平台/服务商/客户代表登录、客户与 MSP 页面 smoke、登录与 `/auth/me` 角色一致性 | pass |
| Round 1 | end_user / 客户管理员 / MSP manager / MSP agents 权限矩阵、跨租户 GET/LIST/PUT/DELETE、未认证 | pass |
| Round 2 | 工单↔工单、问题↔工单/事件/变更、事件→问题、变更↔CMDB、变更关联工单、工单↔CI、发布↔变更 | pass |
| Round 3 | 事件→问题→变更全生命周期、服务请求 L1/L2/L3、工单协作、MSP 指派/回复/改状态、租户 readiness | pass |
| Round 4 | 客户核心页面、工单详情 BPMN/i18n、MSP 工作台、readiness 向导、360/768/1440 响应式、服务目录统计、无 ai:read 的工单/变更 AI 门控、发布↔变更 UI | pass |

## 发现与处理

| 编号 | 现象 | 证据 | 处理 |
|---|---|---|---|
| D1 | 变更 `affectedCis` 填 ciNumber 时 CMDB 影响分析为 0；数字 ID 正常 | `02-associations` A4/A4-D1；`handlers/change` | 已修复并发布：`resolveAffectedCIIDs` 同时解析 ID/ciNumber；单测 `affected_ci_resolution_test.go`；E2E 通过 |
| D2 | BPMN 实例停在“请求受理/任务分配”时，服务请求 L2/L3 间歇 500「同步流程审批任务失败」 | `03-workflows` B2/B2-D2；`service/bpmn_approval_bridge_service.go` | 已修复并发布：审批桥接仅匹配 `taskPurpose=approval`；单测 `TestServiceRequestApproval_SkipsNonApprovalTask`；E2E 通过 |
| D3 | 跨租户 `DELETE /tickets/:id` 返回 500 | `01-permissions`；`handlers/ticket` | 已修复并发布：`ent.NotFound` → 404；单测 `TestDeleteTicket_NotFoundReturns404`；E2E 通过 |
| D4 | 跨租户问题关联返回 500 | `02-associations` A2；`handlers/problem` | 已修复并发布：返回 typed `ValidationError(400)` + handler `RespondError`；单测通过 |
| D5 | 事件转问题后事件详情不返回 `problemId`，前端“查看问题”入口不出现 | `02-associations` A3/A3-D2；`handlers/incident` | 已修复并发布：加载 `incident.problems` 反向边并回填 DTO；单测 `problem_link_test.go`；E2E 通过 |
| D6 | 768 平板 Dashboard body 横向溢出 | `04-ui-ux`；`layouts/MainLayout.tsx` | 已修复并发布：768–991 默认收起侧栏；E2E 通过 |
| D7 | 服务目录顶部固定桶与标签页计数矛盾 | `04-ui-ux`；`service-catalog/` | 已修复并发布：顶部展示真实动态分类；Jest `ServiceCatalogStats.test.tsx`；E2E 通过 |
| D8 | end_user Dashboard 仍请求 `dashboard:read` 专属 overview → 403 + console error | `00-smoke`、`01-permissions` | 已修复并发布：按 `dashboard:read` 门控 query，显示受限提示；E2E 通过 |
| D9 | `demo.msp.manager` 登录 `role=msp_manager`，但 `/auth/me` 返回 `role=end_user` | `00-smoke` D9；`handlers/common` | 已修复并发布：`GetMe` 使用 JWT effective role 并重算能力标记；单测 + E2E 通过 |
| D10 | 租户管理员无 `service_request:delete`，服务请求无法清理 | 权限矩阵；`internal/authz`、`middleware/rbac.go` | 已修复并发布：授予租户管理员删除；新增存量租户幂等迁移与 `TestAdminCanDeleteServiceRequest`；E2E DELETE 200 |
| D11 | 无 `ai:read` 的客户用户打开工单详情仍渲染 AI Inline/分诊面板并发请求 | `04-ui-ux` D11；`mfgUser` | 已修复并发布：`TicketDetail` 入口级隐藏 AI 面板；知识库 AI 搜索框同样门控；E2E 通过 |
| D12 | 无 `ai:read` 的客户 lead 在变更详情仍看到 AI 影响分析入口 | `04-ui-ux` D12；`mfgLead` | 已修复并发布：`ChangeDetail` 按 `ai:read` 隐藏 AI 计划卡；E2E 通过 |
| D13 | 发布单创建/更新接受其他租户引用 | `02-associations` A6；`service/release_service.go` | 已修复并发布：写入前校验四类引用；非法引用返回 400；单测 + E2E 通过 |
| D14 | 发布表单无“关联变更”入口，详情不展示 `changeId` | `04-ui-ux` D14；`ReleaseForm`/`ReleaseDetail` | 已修复并发布：新增 `ChangeSelect`；详情展示变更标题并可跳转；E2E 通过 |
| D15 | 服务请求软删后 GET 返回 HTTP 200（包体 code=404） | `03-workflows` D10；`handlers/service_request/handler.go` | 已修复并发布：`common.NotFound` 返回 HTTP 404 + 业务码 4004；handler 单测 + E2E 通过 |

## 数据清理

- 本轮 2026-11-10/11 产生的 `[E2E-*]` 数据 API 可见残留为 0（工单/事件/问题/变更/发布/服务请求）。
- 产品删除为软删，数据库保留审计行（最终审计：tickets=150、problems=77、incidents=58、service_requests=48）；
  这些记录 `deleted_at` 非空、API/工作台不可见，不属于演示残留，未做物理删除。
- D13/D14/D12 的发布与变更测试数据已清理；发布列表、制造/医疗租户变更列表复查 `[E2E-RELEASE-XT]`、`[E2E-RELEASE-UI]`、`[E2E-CHANGE-AI]` 均为 0。
- 未触碰 `[DEMO]` 种子账号与种子单据。
- 已知 demo 残留：`review-test`（tenant id=6）、31 张历史验收/2026-10-28/29 E2E 工单、SLA 全部超期；属于既有演示库状态，未在本轮清理。

## 产物位置

- 测试报告：`.dev/playwright-demo-report/`（本地，gitignore）
- 失败 trace/screenshot：`.dev/playwright-demo-results/`（本地，gitignore）
- 套件：`itsm-frontend/tests/e2e/demo/`
- 运行配置：`itsm-frontend/playwright.demo.config.ts`
- 运行手册：`itsm-frontend/tests/e2e/demo/README.md`

## 限制

- 238 已发布本轮修复；全量 39/39 通过，无 expected failure。
- 本轮不覆盖性能/并发容量、真实 SMTP/邀请投递、云发现、AI 质量评估。
- 历史评审文档保持 `Status: historical`，未改写；当前事实已回填 `docs/multi-tenant/demo-environment.md` 与新报告。