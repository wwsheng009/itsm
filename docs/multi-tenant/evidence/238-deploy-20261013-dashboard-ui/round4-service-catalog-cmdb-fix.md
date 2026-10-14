# 238 修复证据：/service-catalog「获取失败」消除（2026-10-13 第四轮）

> 状态：已完成（238 复验通过）｜定位：修复证据（2026-10-13 第四轮）｜版本：`1.8.0-dev.ff0a88da`（demo 8089）｜提交：`ff0a88da`

## 1. 现象与根因

- 现象：`demo.mfg.user`（end_user，demo-mfg）访问 `/service-catalog` 弹「获取失败」；目录主列表本身正常。
- 复现（API）：`GET /api/v1/service-catalogs?...status=enabled` → 200（3 条 [DEMO] 服务）；
  `GET /api/v1/cmdb/ci-types`、`GET /api/v1/cmdb/cloud-services` → **403 权限不足**。
- 根因：页面 `loadOptions` 无条件加载 CMDB 选项（CI 类型/云服务，端点需 `cmdb:read`），
  `end_user` 无该码 → catch 弹 `message.error('获取失败')`；该数据仅用于**可选筛选项**。
- 数据佐证（demo-mfg 角色）：`cmdb:read` = 系统管理员/IT管理员/服务台坐席 ✅，最终用户 ❌。

## 2. 修复（`ff0a88da`）

| 文件 | 变更 |
|---|---|
| `service-catalog/index.tsx` | `hasPermission('cmdb','read')` 为假时**跳过选项请求**（不发请求、不弹错）；`canReadCmdb` 传入筛选组件 |
| `ServiceCatalogFilters.tsx` | 新增 `canReadCmdb`（默认 true）：无权限不渲染 CI类型/云服务 筛选项（避免空下拉） |
| 组件测试 | +2 例：默认渲染两项筛选；`canReadCmdb=false` 隐藏且刷新按钮保留 |

验证：jest 4/4、`tsc --noEmit`、eslint 全绿。

## 3. 部署与线上复验

- 部署：远端 `deploy-demo-b9` @ `ff0a88da`（含并发会话已上线的 `c7ac0abc` 后端内容）；
  仅重建 `itsm-frontend`，`itsm-backend`/`itsm-ai-service` 由 `c7ac0abc`/`de337963` retag；
  `.env.demo` VERSION → `1.8.0-dev.ff0a88da`（备份 `.env.demo.bak-20261013-165647`）；6/6 healthy。
- 浏览器（demo.mfg.user）：`/service-catalog` **无「获取失败」toast**；CI类型/云服务 筛选项隐藏；
  3 条演示服务正常渲染；网络面板仅 `GET /api/v1/service-catalogs?...` [200]，**无任何 /api/v1/cmdb 请求**。
- 回归面：`cmdb:read` 持有者（admin/IT管理员/坐席）不受影响（组件默认渲染，请求照常成功）。
- 重建基线复核：demo-mfg 全部角色跨租户 MSP 数据面授权 = **0**（第三轮重建成果保持）。

## 4. 备注

- 与并发会话部署共存：其分支 `deploy-latest8b` 未改动；如对方后续再发布，按其流程重打
  `.env.demo` VERSION 即可（本次仅新增一个前端修复标签）。
- 如产品侧希望 end_user 也能按 CI 类型/云服务筛选，应改为授予 `cmdb:read`（权限面决策），
  而不是恢复无条件请求。
