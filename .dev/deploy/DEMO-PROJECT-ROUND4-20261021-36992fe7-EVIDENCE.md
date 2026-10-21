# DEMO 部署证据：第四轮回归修复（36992fe7，2026-10-21）

- 版本：`1.8.0-dev.36992fe7`
- 部署分支：`deploy-demo-project-round4-36992fe7`
- 前置版本：`1.8.0-dev.50a1831b`
- 环境：`172.18.3.238`（`itsm-demo`，`itsm_demo`，8089）
- 迁移：无（ledger 105 → 105）

## 1. 部署结果

| 步骤 | 结果 |
|---|---|
| A. fetch/checkout | `HEAD=36992fe7`；`VERSION=1.8.0-dev.36992fe7` |
| A. 落树核对 | `overflow: hidden` ×8；`B1（第四轮根因）` 注释在位；`onHeaderCell` ×2；`归属与计划` ×2；守卫测试在位；**工时控件 `max=` 计数 0** |
| B. 构建 | `BUILD_EXIT=0` |
| C. up -d + init | init `exit=0`；ledger 105 → 105 |
| 容器 | backend/worker/frontend 全 `Up (healthy)`，tag=`36992fe7` |
| 版本 | `1.8.0-dev.36992fe7` |

A/B 指纹：

| 指纹 | 旧 `50a1831b` | 新 `36992fe7` |
|---|---|---|
| frontend `归属与计划` | 0 | **1** |

前端产物内：`不得超过` 命中 3 个文件（校验文案已进包）。

## 2. 运行时验证

| # | 检查 | 结果 |
|---|---|---|
| V1 | 后端拒绝 `999999999` | **400** `estimateHours must be within 0..10000` |
| V1b | 后端拒绝 `10001` | **400** |
| V2 | 汇总护栏 | `invalidHoursTasks=1`、`invalidEstimateHours=1000000`、`maxTaskHours=10000` |
| V3 | 页面可达 | `/projects`、`/projects/9`、`/projects/templates` 全 200 |
| V4 | 前端产物含新标记 | `归属与计划` = 1；`不得超过` = 3 |
| V5 | 日志 | 无 panic / fatal / error |

## 3. 测试数据清理

第四轮报告 §7 声明写入了两条 10000h 测试任务，实测确认：

```
10|越界工时拒绝保存验证|est=10000
11|工时钳制观测（不提交）|est=10000
```

清理前汇总 `20009 = 9 + 10000 + 10000`（**非护栏失效**）。

已通过 API 软删 task 10 / 11（均 200），清理后：

```json
{
  "taskCount": 3,
  "estimatedTaskCount": 1,
  "estimateHours": 9,
  "invalidHoursTasks": 1,
  "invalidEstimateHours": 1000000,
  "maxTaskHours": 10000
}
```

> 仍剩 task 8（`1000000h` 历史脏数据）被排除并告警；需业务侧修正工时或软删。

## 4. 未覆盖（需浏览器人工复验）

1. 任务弹窗输入 `999999999` → **未点保存即出现红字**（去掉 `max` 后校验器首次能看到真实值）；
2. 项目列表：首屏同时可见 状态/健康度/进度/归属与计划/操作，**无浮层遮挡**；
3. 阶段门「门禁阻塞项」列头可见（≥260px）。

> 这三项是纯视觉/交互，API 与源码断言都无法替代浏览器验证。
> 已用 `task-hours-limit.test.ts` 锁死"工时控件不得有 max="，防止第 1 项回退。

## 5. 回滚

```bash
cd /home/n9e/itsm/repo
git checkout -f deploy-demo-project-round3-50a1831b
sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.50a1831b/' .env.demo
echo 'passw0rd' | sudo -S -p '' env VERSION=1.8.0-dev.50a1831b \
  docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-backend-demo itsm-worker-demo itsm-frontend-demo
```
