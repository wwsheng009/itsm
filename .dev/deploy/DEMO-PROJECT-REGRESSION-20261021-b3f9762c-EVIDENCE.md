# DEMO 部署证据：回归验证遗留项修复（b3f9762c，2026-10-21）

- 版本：`1.8.0-dev.b3f9762c`
- 部署分支：`deploy-demo-project-regression-b3f9762c`
- 前置版本：`1.8.0-dev.0aa276da`
- 环境：`172.18.3.238`（`itsm-demo` 栈，`itsm_demo` 库，端口 8089）
- 迁移：无（ledger 105 → 105）

## 1. 提交链

| 提交 | 说明 |
|---|---|
| `cb6142f1` | 回归遗留修复（归口候选端点 / 报表部门维度 / 操作列固定 / 数字控件 / 选项可寻址） |
| `b3f9762c` | **修复 `cb6142f1` 引入的构建失败**：`MenuItems.tsx` 仍引用已删除的 `../Sidebar.module.css`，改为 `./Sidebar.module.css`（同时修复收起态 flyout 文字） |

> 首次构建 `1.8.0-dev.cb6142f1` 失败于：
> `Could not resolve "../Sidebar.module.css" from "src/components/layout/sidebar/MenuItems.tsx"`。
> 本地 `tsc --noEmit` 不解析 CSS，未能暴露；生产 `vite build` 会解析，故以 `b3f9762c` 重新构建成功。

## 2. 部署结果

| 步骤 | 结果 |
|---|---|
| A. fetch/checkout | `HEAD=b3f9762c`；`.env.demo VERSION=1.8.0-dev.b3f9762c` |
| B. 构建 | `BUILD_EXIT=0`；backend/frontend 镜像构建成功 |
| C. up -d + init | init `exit=0`；ledger 105 → 105（无迁移） |
| 容器 | backend/worker/frontend 均 `Up (healthy)`，tag=`b3f9762c` |
| 版本 | `GET /api/v1/version` → `1.8.0-dev.b3f9762c` |

A/B 指纹：

| 指纹 | 旧 `0aa276da` | 新 `b3f9762c` |
|---|---|---|
| backend `owner-team-candidates` | 0 | **2** |
| frontend `归口团队` | 2 | **3** |
| frontend `所属部门` | — | **7** |
| frontend `fixed` | — | **21** |

## 3. 运行时验证

| # | 检查 | 结果 |
|---|---|---|
| V1 | 版本 / 登录 / CSRF | `1.8.0-dev.b3f9762c`；csrf_len=24 |
| V2 | **A2 归口候选** | `GET /projects/owner-team-candidates` → 200，返回 **10** 个团队（云平台运维 / 变更委员会 / 安全运营 / 应用支持 / 数据库运维 / 服务台-L1 …） |
| V3 | **A2 写入链路** | `PUT /projects/9 {ownerTeamId:7}` → **200**；响应 `ownerTeamId=7`、`ownerTeamName=云平台运维`、`departmentName=IT基础架构`；列表回显一致 |
| V3b | **A2 报表分组** | 出现 `云平台运维 / IT基础架构 / 1`；其余为 `未归口 + 部门` 分列（信息技术部 2、IT安全 1、未设置 3）——不再全部堆在「未归口」 |
| V3c | 测试数据还原 | `PUT {ownerTeamId:0}` → 200（未留残留） |
| V4 | B12 一次通过率 | `passed=5 / firstPass=5 / autoApproved=6 / rate=1`（自动放行仍单列） |
| V5 | 页面可达 | `/projects`、`/projects/templates`、`/projects/reports`、`/projects/portfolio` 全 200 |
| V6 | 日志 | backend 近 10 分钟无 panic / fatal / error |

> 说明：V3 首次失败是验证脚本的 shell 引号问题（`sudo` 命令串被当作 PID），非产品缺陷；
> 改为独立脚本后 200 通过。

## 4. 未覆盖（需浏览器人工复验）

以下为纯视觉/交互项，API 层无法断言：

1. 任务页首屏是否直接可见操作列（`fixed:'right'`）；
2. 阶段门「门禁阻塞项」表头是否横排；
3. 下拉选项读屏可寻址性（`virtual={false}` 后应进入 DOM）。

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
git checkout -f deploy-demo-project-review-0aa276da
sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.0aa276da/' .env.demo
echo 'passw0rd' | sudo -S -p '' env VERSION=1.8.0-dev.0aa276da \
  docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-backend-demo itsm-worker-demo itsm-frontend-demo
```
