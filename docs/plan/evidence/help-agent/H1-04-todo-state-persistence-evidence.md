# 待办认领/忽略状态持久化与看板消费（H1-01/H1-03/H1-04 条件项闭环）证据

- 日期：2026-10-18
- 状态：`integration_verified`
- 对应方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md`
  （H1-01「待办认领/忽略状态持久化与看板消费归 H1-04」、H1-03「提议认领状态归 H1-04 批」、
  H1-04「待办认领/忽略持久化单独立项」三处条件项）

## 1. 交付范围

把 H1-01 文档待办队列从「只读派生视图」升级为「可运营队列」：租户内可**认领 / 忽略 / 恢复**待办，
状态持久化并在覆盖率看板消费（状态列 + 行内操作 + 状态筛选 + 降级口径）。

## 2. 数据面

### 2.1 主迁移 `itsm-backend/migrations/20261018n_ai_doc_todo_states.sql`

| 列 | 说明 |
|---|---|
| tenant_id | 租户（RLS + 查询条件双约束） |
| page_key | 待办页面键（VARCHAR(128)，与待办队列 pageKey 同口径） |
| state | `claimed` / `ignored`（CHECK；`open` 不落行 = 缺省态） |
| updated_by / updated_at | 最近操作人（用户 id）/ 时间 |
| UNIQUE (tenant_id, page_key) | 每租户每页至多一行；索引 (tenant_id, state) |

口径注：**「恢复待办」= 删除行**回到缺省 `open`，不把空语义写进表（避免噪声与歧义）；
`upsert` 幂等（重复认领原地刷新操作人/时间）。

### 2.2 RLS `itsm-backend/database/rls/migrations/017_ai_doc_todo_states_policies.sql`（+ `_rollback.sql`）

- `ENABLE ROW LEVEL SECURITY` + `tenant_isolation_ai_doc_todo_states`
  （`tenant_id = get_current_tenant_id()`，USING + WITH CHECK，fail-closed；与 003–016 同范式，只 ENABLE 不 FORCE）；
- 幂等：`to_regclass` 守卫 + `DROP POLICY IF EXISTS`；无表时 NOTICE 跳过；
- 应用：`go run -tags rlsapply ./cmd/rls-apply -files 017_ai_doc_todo_states_policies.sql`（本机无 PG，归联调）。

## 3. 服务层 `itsm-backend/service/help_doc_todo_states.go`

- `NormalizeHelpDocTodoState`（claimed/ignored/open，忽略大小写与空白）/ `ValidateHelpDocTodoPageKey`（非空 + 列宽）；
- `ListStates(ctx, tenantID)`：全量读（pageKey → 状态；空结果返回非 nil map）；`updated_at` 输出 RFC3339 UTC；
- `SetState(ctx, tenantID, userID, pageKey, state)`：
  - claimed / ignored → `INSERT ... ON CONFLICT (tenant_id, page_key) DO UPDATE ... RETURNING`（幂等，返回落库状态）；
  - open → `DELETE`（幂等，返回 `{pageKey, state:"open"}` 无时间）；
  - 非法租户/用户/页面键/状态 → `ErrHelpDocTodoStateInvalid`（**零库访问**）；未装配 → `ErrHelpDocTodoStateUnavailable`。

## 4. 端点面

| 端点 | 权限 | 语义 |
|---|---|---|
| `GET /api/v1/ai/help/todos` | `ai:read` | 原契约 + `items[].state/stateUpdatedBy/stateUpdatedAt`（缺省 open）+ `totals.claimed/ignored` + `states.available`；状态服务未装配/失败 → 200 + `states.available=false`（按 open 渲染，不 5xx）；遥测降级语义不变 |
| `PUT /api/v1/ai/help/todos/state` | `ai:write` | body `{pageKey, state}`；claimed/ignored/open；非法输入 400、未装配 503、内部错误 500；`authz-gen` 已再生并通过预检新鲜度/对齐测试 |

装配：`internal/bootstrap/app.go` 以 `database.GetRawDB()` 注入 `service.NewHelpDocTodoStateService`
（与 MSP 帮助空间同库模式）；未注入时行为与注入前一致（GET 新增字段缺省、写路径 503）。

## 5. 看板消费 `itsm-frontend/src/pages/(main)/ai/help-coverage/index.tsx`

- 待办表新增列：**状态**（待处理/已认领/已忽略 Tag）+ **操作**（认领/忽略/恢复待办，`ai:write` 后端门控）；
- 新增状态筛选 Segmented（全部/待处理/已认领/已忽略，客户端过滤，含条数）；
- 写成功 → 仅刷新待办（账本主数据不动）；**写失败 → 错误提示且不乐观改写状态**（诚实口径）；
- 状态服务降级（`states.available=false`）→ 提示条 + 按钮禁用；
- `ai-api.ts`：`HelpTodoState`/`HelpTodoItem.state*`/`HelpTodosResult.totals.claimed|ignored|states` +
  `aiSetHelpTodoState(pageKey, state)`（PUT 状态写面）。

## 6. 复跑命令与实测结果（2026-10-18）

后端（workdir `itsm-backend`）：

```pwsh
gofmt -l -w service/help_doc_todo_states.go service/help_doc_todo_states_test.go `
  handlers/ai/help_todos.go handlers/ai/help_todos_test.go handlers/ai/service.go `
  router/ai_routes.go internal/bootstrap/app.go
go run ./cmd/authz-gen                                                   # 已生成 rbac_precheck_gen.go
go test ./service/ -run 'HelpDocTodo' -count=1                           # ok 1.8s
go test ./handlers/ai/ -run 'HelpTodo' -count=1                          # ok 1.9s
go test ./service/ ./handlers/ai/ -count=1                               # ok（service 56.3s / handlers/ai 8.0s）
go build ./router/ ./internal/bootstrap/ ./middleware/                   # ok
go test ./router/ -count=1                                               # ok 5.3s
go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1  # ok 1.9s
go test ./migration/ -count=1                                            # ok 1.2s（目录自发现迁移解析）
```

前端（workdir `itsm-frontend`）：

```pwsh
npx jest --runTestsByPath "src/pages/(main)/ai/help-coverage/__tests__/index.test.tsx" `
  --coverage=false --watchAll=false --runInBand      # 10 tests（+4 新增）全绿
npx jest src/lib/api/__tests__/ai-api.test.ts --coverage=false --watchAll=false --runInBand  # 64 tests（+2）
npx jest src/lib/ai src/components/ai src/lib/api --runInBand --silent --coverage=false      # 129 suites/1682 tests
npx jest --runTestsByPath <页面域 6 套> --coverage=false --watchAll=false --runInBand          # 6 suites/44 tests
npx tsc --noEmit                                                                              # exit 0
npx eslint <本批 4 个前端文件>                                                                  # exit 0
```

## 7. 关键断言（测试锁定）

- 服务层：归一化（` Claimed `→claimed、非法态拒绝）；页面键列宽边界（128 合法 / 129 拒绝）；
  `ListStates` 扫描两行 + RFC3339 UTC + 空结果非 nil map + 未装配/非法租户错误；
  `SetState` claimed 幂等 UPSERT（参数 tenant/page/state/user 与 RETURNING 解析）；open → DELETE；
  四类非法输入 `ErrHelpDocTodoStateInvalid` 且 **sqlmock 零预期**（不触达数据库）；
- 端点层：GET 合并（states.available=true、items[].state/stateUpdatedBy/stateUpdatedAt、totals.claimed=1）；
  未装配状态服务 → `available=false`（200，不 5xx）；
  PUT claimed → 200 + 落库状态回显；PUT open → 200 + `state=open` 且无 updatedAt；
  未装配 → 503；身份缺失 → 401；未知状态/缺 pageKey/畸形 JSON → 400 且零库访问；
- 看板：认领点击 → `aiSetHelpTodoState('dashboard','claimed')` + 刷新后渲染「已认领」/「恢复待办」；
  状态筛选「已忽略」仅显示 ignored 项；降级提示 + 按钮禁用；写失败错误提示且状态保持待处理（不乐观改写）。

## 8. 边界与条件项

- **真实 PG 冒烟归联调**：RLS 017 应用（`rls-apply -files` + `-verify`）与 upsert 冲突路径（ON CONFLICT）
  需真实 PG；本批以 sqlmock 锁定 SQL 形状与参数、以 migration/router/middleware 包测试锁定编译与预检一致性；
- **页面 i18n** 沿用 H1-04 既有条件项（本批新增文案与页面现状一致为中文硬编码）；
- 夜批 stale 提议（H1-03）的 **Issue 侧认领**仍归平台侧；本表提供队列侧（账本缺口 ∪ 遥测缺口）认领/忽略的
  通用持久化，H1-03 提议一旦进入待办队列即同享该状态面；
- 状态为**运营意图**而非流程门禁：ignored 仅改变看板展示/筛选语义，不阻断任何写路径或后续批次重新认领。