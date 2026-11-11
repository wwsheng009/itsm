# MSP Demo E2E Run Summary · 2026-11-10

## 环境

| 项 | 值 |
|---|---|
| Base URL | `http://172.18.3.238:8089` |
| Version | `1.8.0-dev.bf131540`（部署 worktree commit `bf1315408`） |
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
Running 45 tests using 1 worker
45 passed
```

> 0 个 expected failure；D1–D18、多租户跳转/业务操作与附件角色访问的回归断言均为普通用例。
> `a0d4240a` 起复核改为按 spec 分组独立进程执行，最终 `e1599ffd` 复核累计 45/45 通过；单进程全量在本机 Windows 上曾因 Chrome 内存不足 OOM，非产品失败。

> 已移除旧的 known-defect 容忍分支：跨租户删除/问题关联不再接受 500，事件转换后必须返回
> `problemId`，L1/L2/L3 审批必须 200，D10 删除后 GET 必须 404，Dashboard/AI 不再允许 403 白名单。

## 发布与部署后验证

238 已从当前工作区 HEAD `bf1315408` 全量发布 `1.8.0-dev.bf131540`：

- 6 个常驻容器（backend/worker/frontend/nginx/redis/ai-service）healthy；`itsm-init-demo` exit=0。
- `/api/v1/healthz` ok、`/api/v1/readyz` ready；`/api/v1/version` 返回 `bf131540`。
- 部署 worktree：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-bf131540`。
- 备份/回滚证据：`backups/demo-release-1.8.0-dev.bf131540-20261111T162032Z`。
- 镜像：backend `sha256:b4e7e3a5a77f9d91d1b7f2d3b91a19898f36ff31e0ba2ff312c9bbb4e1351a4b`；frontend `sha256:dc4b4798c3cdc27c664d4c8a6e42633649e81c895be300591183efecb120aee9`；ai-service `sha256:f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301`。
- demo E2E 分组累计 `45/45 passed`；此前 `f7dd2974`、`44faf311`、`5699b5a8`、`af3cba9c`、`a0d4240a`、`91eb6e82`、`49a2ad11`、`ad878805`、`e1599ffd` 全量部署均已被本结果取代。

> 发布前曾用本地生产构建 + 238 后端验证前端门控与布局修复；最终验证据以部署后的 238 结果为准。此前 `f7dd2974` 是独立部署分支的裁剪版本，未包含当前分支的 DeepTenantReturn 与完整当前代码；本次已纠正。

## 轮次与覆盖

| 轮次 | 覆盖 | 结果 |
|---|---|---|
| Round 0 | 环境/版本/ready、平台/服务商/客户代表登录、客户与 MSP 页面 smoke、登录与 `/auth/me` 角色一致性 | pass |
| Round 1 | end_user / 客户管理员 / MSP manager / MSP agents 权限矩阵、跨租户 GET/LIST/PUT/DELETE、未认证 | pass |
| Round 2 | 工单↔工单、问题↔工单/事件/变更、事件→问题、变更↔CMDB、变更关联工单、工单↔CI、发布↔变更 | pass |
| Round 3 | 事件→问题→变更全生命周期、服务请求 L1/L2/L3、工单协作（含 assignee membership 断言）、MSP 指派/回复/改状态、租户 readiness | pass |
| Round 4 | 客户核心页面、工单详情 BPMN/i18n、MSP 工作台、readiness 向导、360/768/1440 响应式、服务目录统计、无 ai:read 的工单/变更 AI 门控、发布↔变更 UI、provider→customer→返回工作台 | pass |
| Round 5 | provider 深度切换三客户处理既有工单、客户作用域评论/改状态、跨租户隔离、MSP 工作台指派/回复、provider 员工分配边界、provider 读取客户问题且医疗不可见、provider 代客建单（显式客户 requesterId） | pass |
| Round 6 | 工单附件上传后 end_user / it_admin / security_admin / provider 深度态列出附件；ticket 74 类场景 | pass |

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
| D16 | provider 进入客户租户后 Header 丢失“返回工作台”入口 | `04-ui-ux` 多租户跳转；`DeepTenantReturn`/`Header` | 已修复并全量发布：返回入口从 `CustomerFilter` 解耦，客户作用域独立渲染；切回 provider 后再导航工作台；E2E 通过 |
| D17 | 多租户连续建单时 tenant-local 序列撞全局唯一 `ticket_number`，3 次重试后 500 | `repository/ticket`；Round 5 MT-1；后端日志 | 已修复并全量发布：Redis/DB 发号共用 `nextFreeTicketNumber` 全局探针，跳过他租户占用号；单测 + 44/44 E2E 通过 |
| D18 | 工单附件列表对 end_user / it_admin / security_admin / provider 深度态返回 500 | `/tickets/74`；Round 6；`ticket_attachment` | 已修复并全量发布：授权跟随 `ticket:read` + active 用户；403/404 精确映射；单测 + 44/44 E2E 通过 |

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

- 238 已发布本轮修复；E2E 分组累计 45/45 通过，无 expected failure。
- C 方案 membership 租户内身份：membership 上下文、工单 membership 回填、provider 代客建单、`/auth/me` membership 字段；详见 [`../../../multi-tenant/plan/membership-tenant-identity-c-plan.md`](../../../multi-tenant/plan/membership-tenant-identity-c-plan.md)。
- C 方案 Phase 3：评论/附件/CC/审批/通知/审计/BPMN 决策 membership 引用；238 迁移台账已应用、15 个复合外键存在、租户错配 0；运行态评论 9/9、附件 2/2、CC 1/1、工单通知 78/78 有 membership；运行态 E2E 含评论/CC/附件 membership 断言，最终 45/45。
- 工单分配 membership 同步：单张/批量/智能/重新分配均写 `assignee_membership_id`；238 active 有处理人工单 62/62 有 membership，缺失 0。
- 硬编码角色白名单审计及穷尽复核补漏：Header AI `isAdmin()` 旁路、AI 通知/审批 fallback 无效角色码、MSP 写权限重复白名单、provider `allowedActions` 行级误判；详见 [`../../2026-11-11-role-whitelist-audit.md`](../../2026-11-11-role-whitelist-audit.md)。
- provider 深度态代客建单已修复：必须显式提交客户 `requesterId`，服务端解析目标租户 membership；
  缺失 `requesterId` 返回 400（不再落入 404 申请人校验）。
- 本轮不覆盖性能/并发容量、真实 SMTP/邀请投递、云发现、AI 质量评估。
- 历史评审文档保持 `Status: historical`，未改写；当前事实已回填 `docs/multi-tenant/demo-environment.md` 与新报告。