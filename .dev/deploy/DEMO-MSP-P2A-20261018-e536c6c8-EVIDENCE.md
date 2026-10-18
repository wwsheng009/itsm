# 238 demo 部署证据（第三轮）：MSP 跨客户工作台项目只读投影（2026-10-18，1.8.0-dev.e536c6c8）

## 变更内容

- 提交：`e536c6c8` feat(msp): 跨客户工作台项目只读投影（P2+ 客户交付 P2-A）
  - 后端：`GET /api/v1/msp/workbench/projects`、`GET /api/v1/msp/projects/:id?customerTenantId=`；
    `recordType=project` 投影（allocation 允许集合 + 逐租户查询 + updated_at 合并 + 游标分页 + status/q 过滤），
    `allowedActions` 仅 `view`（无写端点）。
  - 角色：C2 客户角色基线补 `project:read`（tech）/`project:write`（manager，P2-B 预留）。
  - 前端：MSP 工作台「跨租户单据」新增「项目」类型（slug=projects，只读）。
- 交付镜像：`itsm-backend:1.8.0-dev.e536c6c8`（sha256:c3bfe928…）、`itsm-frontend:1.8.0-dev.e536c6c8`（sha256:5f92d31b…）；ai-service retag 对齐。
- 演示数据：以客户租户管理员（`demo.mfg.admin`）创建项目 `PRJ-20261018-6947`（id=2，tenant 3 / 华辰智能制造，status=planning）。

## 部署路径

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-pm-p2c-full.bundle`（基线=远端 5a1f1b03） | fetch/checkout OK（HEAD=e536c6c8，分支 deploy-demo-msp-p2a） |
| A | checkout + `.env.demo VERSION`（保留 LLM_TIMEOUT=5） | `A-STATUS: DONE` |
| A2 | 清理 + retag ai + 后台顺序构建 | `A2-STATUS: DONE` |
| W | backend → frontend | `BUILD_BACKEND_EXIT=0`、`BUILD_FRONTEND_EXIT=0`、`BUILD_ALL_DONE` |
| C | `up -d backend/worker/frontend` | 三容器 healthy；version=e536c6c8；`/assets`、`/assets/new` 均 200；`LLM_TIMEOUT=5` 保持 |

## 验证证据

### API（MSP 账号，`demo.msp.manager` / 公开演示口令）

| 检查 | 结果 |
|---|---|
| 跨客户项目列表 `GET /api/v1/msp/workbench/projects?customerTenantIds=all` | 200，`total=1`，条目 `recordType=project` / `PRJ-20261018-6947` / `华辰智能制造` / `planning` |
| `allowedActions` | **仅 `view`**（`[{action:"view",allowed:true}]`） |
| 详情 `GET /api/v1/msp/projects/2?customerTenantId=3` | 200，字段回显（编号/标题/状态/描述） |
| 未知 id | 404 |
| 声明租户不匹配（customerTenantId=4） | 400 |

（`node .tmp/demo-msp-probe.js` → SUMMARY total=7 failed=0）

### 浏览器（Tabbit，MSP 视角）

- 登录 `demo.msp.manager` → 落地 `/msp/workbench`；
- 「跨租户单据」board 的类型选择渲染 **变更 / 问题 / 服务请求 / 项目**；
- 选择「项目」→ 列表出现 `PRJ-20261018-6947 · MSP 验收项目 488349 · 华辰智能制造 · 规划中`；
- 点击行 → 详情抽屉：编号/标题/客户/状态/创建时间/更新时间/描述（`P2-A 跨客户项目只读面板验收`）；
- 无「推进状态」动作（只读面契约生效）；截图 2 张（artifact 任务 `task-e47e93f3-…`）。

## 观察项

1. demo 的 MSP 演示口令为 `Demo@2026!`（`scripts/demo/README.md` 的 `--user-password` 默认值），非 `DEMO_ADMIN_PASSWORD`。
2. 工作台「跨租户单据」为 Segmented 切换（非 Tabs），记录类型为分组按钮（非 Select）——UI 自动化需按此选择器定位。
3. `allowedActions` 仅 view 是**有意契约**：项目审批/状态推进保留客户治理面（P2-B 另立写面）。

## 复跑方式

```bash
NEW=1.8.0-dev.e536c6c8 bash /tmp/deploy238_pm_p2c_a.sh        # n9e
NEW=1.8.0-dev.e536c6c8 bash /tmp/deploy238_pm_p2c_a2.sh       # sudo
bash /tmp/deploy238_pm_p2c_w.sh                                # sudo（轮询）
NEW=1.8.0-dev.e536c6c8 bash /tmp/deploy238_pm_p2b_c.sh        # sudo（复用 C 脚本）
node .tmp/demo-msp-probe.js                                    # API 验证
```