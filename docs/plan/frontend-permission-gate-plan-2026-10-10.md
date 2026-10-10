# 系统性权限门禁：前端 fail-closed 路由/入口收敛（不靠"填完才知道没权限"）

## 1. 问题与根因

**现象（报告案例）**：technician（user01，无 `knowledge:write`）打开 `http://localhost:3000/knowledge/articles/new`，可以完整填写文章，点「保存」后才拿到 `403 {"code":2003,"message":"权限不足"}`——体验上就是"填完才说没权限"。

**实测确认**（本轮已核对）：
- 后端 `router/knowledge_routes.go:23` `POST /knowledge/articles` 挂 `RequirePermission("knowledge","write")`；curl 实测 user01 → `403 2003 权限不足`。
- 前端 `routes/index.tsx`（gen_routes.py 自动生成）里 21 条 `new/create/edit` 写入口路由**全部无权限声明**；`RequireCapability` 只用在 3 个区域级分组（admin / tenant-admin / MSP）。
- 入口按钮也基本不判权限（如知识库列表页「新建文章」`knowledge/index.tsx:370` 无条件渲染）——同类问题已在 `/service-catalog`「新建服务」死按钮上出现过（本会话已修）。

**根因**：前端缺少「路由/入口 ↔ 权限码」的单一事实源与 fail-closed 门禁；权限只体现为"后端拒绝后的错误提示"。

**对照**：后端已经系统化——每个写路由显式 `middleware.RequirePermission`，并有 `router/route_permission_guard_test.go`（AST 扫描 + 白名单，新写路由漏挂权限直接测试红）。前端缺等价物。

## 2. 目标

1. 无权限用户**到达表单前**即被拦截（路由级 fail-closed，直达 URL 也不放行）；
2. 入口按钮/链接按同一权限源隐藏，不再出现"点了没反应/填完才失败"；
3. 防漂移守卫测试：新增写入口忘记声明权限 → 测试红；
4. 后端保持唯一权威（前端只是 UX 门禁，不替代服务端校验，纵深防御）；
5. 不改自动生成的 `routes/index.tsx`（避免与 gen_routes.py 再生成冲突）。

## 3. 方案（四层）

### L1 路由级：声明式映射 + 单点接入
- 新增 `itsm-frontend/src/routes/route-permissions.ts`：
  - `interface RoutePermissionRule { pattern: string; anyOf: string[]; reason: string }`（pattern 用 react-router `matchPath` 语法，如 `/knowledge/articles/new`）；
  - `ROUTE_PERMISSIONS: RoutePermissionRule[]`：覆盖全部写入口路由（§4 清单，权限码以后端 `router/*.go` 的 `RequirePermission` 为准逐条核对）；
  - `resolveRoutePermission(pathname, hasPermission, isAdmin)`：命中规则 → 判定 `allow | deny | none`（`none` 表示该路径无需额外权限）。
- 接入点：`src/routes/guards.tsx` 的 `RequireAuth`（`(main)` 全组唯一布局守卫）——认证/强制改密检查之后追加路由权限检查，`deny` → `<Navigate to="/403" replace />`。
  - 选型理由：① 不动自动生成的路由表（对比：逐条包 `RequireCapability` 要改 21 行且会被再生成覆盖）；② 单点覆盖 `(main)` 全部子路由，含直达 URL 与 SPA 内部导航；③ `/403` 页已存在（`routes/index.tsx:244`）。
  - 口径与现有 `RequireCapability` 一致：`isAdmin() || anyOf.some(p => hasPermission(p))`。

### L2 入口级：PermissionGate 组件
- 新增 `src/components/auth/PermissionGate.tsx`：
  - `<PermissionGate anyOf={['knowledge:write']} fallback={null}>`：无权限**不渲染**（默认 fail-closed，与仓库 `canManageXxx && <Button/>` 惯例同口径）；
  - 支持 `mode="disable"`：渲染禁用按钮 + Tooltip「无权限」（适合入口不宜消失的场景）。
- 本次落地（报告案例）：知识库列表页「新建文章」按钮、文章详情页编辑/发布/下架等写操作入口；其余域的入口按钮按同一组件在后续批次 sweep（§5 清单）。

### L3 防漂移：守卫测试（对标后端 route_permission_guard_test.go）
- 新增 `itsm-frontend/src/routes/__tests__/route-permissions.test.tsx`：
  1. 遍历导出的 `routes: RouteObject[]` 树，收集全部 path（含嵌套）；
  2. 规则 A（fail-closed）：任何 `new|create|edit` 路径必须命中 `ROUTE_PERMISSIONS`，或列入显式白名单且写明理由——新增未声明的写入口直接测试红；
  3. 规则 B：表中每条 pattern 必须匹配到至少一条真实路由（防僵尸规则/路径漂移）；
  4. 规则 C：引用的权限码必须已在 `auth-store.PERMISSIONS` 目录登记（防拼写漂移）。
- 新增 `PermissionGate` 组件测试（有权限渲染 / 无权限隐藏 / admin 旁路 / disable 模式）。

### L4 错误路径兜底（纵深，不替代 L1）
- 新增 `isForbiddenError`（HTTP 403 或业务码 2003）→ 统一文案「没有权限执行此操作，请联系管理员」；
- 知识库新建页 `onFinish` catch 接入（服务端权限被收回等竞态场景仍有清晰提示，而不是裸「创建失败：权限不足」）。

## 4. 写入口路由 → 权限码映射（21 条；实现时逐条以后端 router/*.go 核对）

| 前端路由 | 后端权限码（核对点） |
|---|---|
| `/tickets/create`、`/tickets/ai-create` | `ticket:create`（ticket_routes.go） |
| `/incidents/create`、`/incidents/:id/edit` | `incident:write` |
| `/changes/new`、`/changes/:id/edit` | `change:write` |
| `/problems/new`、`/problems/:id/edit` | `problem:write` |
| `/releases/new`、`/releases/:id/edit` | `release:write` |
| `/improvements/new` | `improvement:write`（核对注册文件） |
| `/knowledge/articles/new`、`/knowledge/articles/:id/edit` | `knowledge:write` |
| `/assets/new`、`/assets/:id/edit` | `asset:write`（核对注册文件） |
| `/licenses/new`、`/licenses/:id/edit` | `license:write`（核对注册文件） |
| `/cmdb/cis/create`、`/cmdb/cis/:id/edit` | `cmdb:write` |
| `/service-catalog/edit/:id` | `service_catalog:write` |
| `/service-requests/new` | `service_request:create` 或 `:write`（核对） |

## 5. 不做 / 后续

- 不改后端；不重构自动生成的 `routes/index.tsx`。
- 其余域入口按钮 sweep（约 15 个页面）与 `/403` 页体验优化（如「返回上一页」）列为后续批次——路由级门禁已先兜住全部 21 条写入口。
- 菜单可见性维持现状（后端 menus 已按权限过滤）。

## 6. 验证

- jest：route-permissions 守卫测试 + PermissionGate 测试 + 知识库入口门禁测试全绿；`tsc --noEmit`、eslint 0；
- 浏览器（隔离会话）：
  - user01 直达 `/knowledge/articles/new` → 立即落到 `/403`（看不到表单）；知识库列表页无「新建文章」按钮；
  - admin_demo_customer（含 `knowledge:write`）→ 正常进入表单并创建成功；
  - `/service-catalog` 回归：user01 无创建按钮（既有修复不变）、admin 正常弹表单；
- CHANGELOG 登记。
