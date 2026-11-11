# 238 演示环境 E2E（ITSM MSP Demo）

> 目标：针对已部署的演示栈 `http://172.18.3.238:8089` 执行多用户、多租户、多单据关联的浏览器/API E2E。
> 与 `tests/e2e/` 下历史本地 seeder 用例隔离；本目录不依赖 `localhost:3000` 或本地 `webServer`。

## 1. 运行

```bash
cd itsm-frontend

# 默认跑全部 demo 用例（Chrome 系统通道；1 worker 规避登录限流）
PLAYWRIGHT_BASE_URL=http://172.18.3.238:8089 \
PLAYWRIGHT_DEMO_CHANNEL=chrome \
npx playwright test -c playwright.demo.config.ts --workers=1

# 分组
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/01-permissions.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/02-associations.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/03-workflows.spec.ts --workers=1
npx playwright test -c playwright.demo.config.ts tests/e2e/demo/04-ui-ux.spec.ts --workers=1
```

- 本机需有 Chrome；没有时可用 `PLAYWRIGHT_DEMO_CHANNEL=chromium` 并安装 Playwright Chromium。
- 登录限流（10/min）自动退避：最多 5 次尝试，按服务端 `retryAfterSeconds` 等待（上限 60s）。
- 报告输出：`../.dev/playwright-demo-report`；失败 trace/screenshot：`../.dev/playwright-demo-results`。
- `results`/`report` 在 `.dev/` 下，已被 `.gitignore` 忽略。

## 2. 账号

与 `scripts/demo/seed_demo_dataset.py` / `docs/multi-tenant/demo-environment.md` 对齐：

- 平台：`admin / Demo@2026!Admin`
- 租户：`Demo@2026!`
- 服务商：`demo.msp.manager`、`demo.msp.agent1`、`demo.msp.agent2`
- 客户：`demo.{mfg,med,edu}.{admin,agent,lead,user,itadmin,secadmin}`

可用环境变量覆盖：

```bash
DEMO_ADMIN_PASSWORD='...' DEMO_USER_PASSWORD='...' ...
```

认证走 `POST /api/v1/auth/login` 的 httpOnly cookie；测试夹具从 cookie jar 提取 `access_token`，不依赖响应体 token。

## 3. 数据与清理

- 新建数据标题统一 `[E2E-<runId>]`。
- 工单/变更/问题/事件/服务请求/关联在用例内 `finally` 发起删除；相关资源为软删，API/工作台不可见但审计行保留。
- 租户管理员自 `1.8.0-dev.5699b5a8` 起具备 `service_request:delete`，服务请求通过删除接口清理。
- **不要**删除或改写 demo 种子数据（`[DEMO]` 前缀记录）。

## 4. 已修复项（238 `1.8.0-dev.5699b5a8` 已验证）

| 编号 | 现象 | 处理 |
|---|---|---|
| D1 | 变更 `affectedCis` 用 `ciNumber` 时 CMDB 影响分析为 0 | 已修复并发布；`02-associations` 普通用例通过 |
| D2 | 服务请求审批遇 BPMN 非审批任务时 L2/L3 间歇 500 | 已修复并发布（审批桥接仅匹配 `taskPurpose=approval`）；`03-workflows` 通过 |
| D3 | 跨租户 `DELETE /tickets/:id` 返回 500 | 已修复并发布，返回 404；`01-permissions` 通过 |
| D4 | 跨租户问题关联返回 500 | 已修复并发布，返回 400；`02-associations` 通过 |
| D5 | 事件转问题后详情不返回 `problemId` | 已修复并发布（incident.problems 反向边映射）；`02-associations` 通过 |
| D6 | 平板 768 Dashboard body 横向溢出 | 已修复并发布（768–991 默认收起侧栏）；`04-ui-ux` 通过 |
| D7 | 服务目录顶部固定桶与标签页计数矛盾 | 已修复并发布（顶部展示真实动态分类）；`04-ui-ux` 通过 |
| D8 | end_user 无 `dashboard:read` 却请求 overview | 已修复并发布（权限门控 query + 受限提示）；`01-permissions` 通过 |
| D9 | MSP 经理 `/auth/me` 返回 `role=end_user` | 已修复并发布（`/auth/me` 返回 JWT effective role）；`00-smoke` 通过 |
| D10 | 租户管理员无 `service_request:delete` | 已修复并发布：授权租户管理员删除，幂等迁移覆盖存量租户；`03-workflows` 通过 |
| D11 | 无 `ai:read` 仍渲染工单 AI 面板并发请求 | 已修复并发布（入口级门控）；`04-ui-ux` 通过 |
| D12 | 无 `ai:read` 的客户 lead 仍看到变更 AI 影响分析 | 已修复并发布（变更 AI 计划卡门控）；`04-ui-ux` 通过 |
| D13 | 发布单接受其他租户引用 | 已修复并发布（写入前校验四类引用）；`02-associations` A6 通过 |
| D14 | 发布表单无法选择变更，详情不展示变更 | 已修复并发布（`ChangeSelect` + 详情跳转）；`04-ui-ux` 通过 |
| D15 | 服务请求软删后 GET 返回 HTTP 200（包体 code=404） | 已修复并发布（HTTP 404 + 业务码 4004）；`03-workflows` D10 通过 |
| D16 | provider 进入客户租户后 Header 丢失“返回工作台” | 已修复并全量发布（`DeepTenantReturn` 独立渲染）；`04-ui-ux` 多租户跳转通过 |
| D17 | 多租户连续建单时租户序列撞全局唯一工单号，重试后 500 | 已修复并全量发布（全局 `nextFreeTicketNumber` 探针）；`05-multi-tenant-business` 通过 |

> 当前套件无预期失败用例；上述回归均为普通断言。
>
> **待产品决策**：provider 深度切到客户作用域后直接建单仍返回 404「申请人不存在或不可用」；
> 当前用例验证的是客户建单、provider 深度处理的主路径。

## 5. 目录

```text
tests/e2e/demo/
├── fixtures/demo-accounts.ts   # 21+demo 账号
├── fixtures/demo-auth.ts       # cookie 登录/session 缓存/多角色 context
├── support/api.ts              # envelope 断言/Bearer/租户头
├── support/assertions.ts       # console/请求失败、裸 key/角色码/BPMN id、横向溢出
├── 00-smoke.spec.ts
├── 01-permissions.spec.ts
├── 02-associations.spec.ts
├── 03-workflows.spec.ts
├── 04-ui-ux.spec.ts
└── 05-multi-tenant-business.spec.ts
```