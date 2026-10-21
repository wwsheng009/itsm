# DEMO 部署证据：第三轮回归遗留修复（50a1831b，2026-10-21）

- 版本：`1.8.0-dev.50a1831b`
- 部署分支：`deploy-demo-project-round3-50a1831b`
- 前置版本：`1.8.0-dev.b3f9762c`
- 环境：`172.18.3.238`（`itsm-demo`，`itsm_demo`，8089）
- 迁移：无（ledger 105 → 105）

## 1. 部署结果

| 步骤 | 结果 |
|---|---|
| A. fetch/checkout | `HEAD=50a1831b`；`VERSION=1.8.0-dev.50a1831b`；落树核对 `MaxTaskHours = 10000`、`TASK_HOURS_MAX`、`minWidth: 260`、`fixed: 'right'` 全部命中 |
| B. 构建 | `BUILD_EXIT=0`（前端 `tsc -b && vite build` 213s） |
| C. up -d + init | init `exit=0`；ledger 105 → 105 |
| 容器 | backend/worker/frontend 全 `Up (healthy)`，tag=`50a1831b` |
| 版本 | `1.8.0-dev.50a1831b` |

A/B 指纹：

| 指纹 | 旧 `b3f9762c` | 新 `50a1831b` |
|---|---|---|
| backend `invalidHoursTasks` | 0 | **1** |
| frontend `已排除` | 1 | **2** |

## 2. 运行时验证（B1 三层）

| # | 检查 | 结果 |
|---|---|---|
| V1 | **写入拒绝**：`estimateHours=999999999` | **400** `estimateHours must be within 0..10000` |
| V2a | 边界 `10000` | **200**（创建成功） |
| V2b | 边界 `10001` | **400**（同一错误语义） |
| V3 | **汇总护栏**（含历史 1000000h） | `estimateHours=9`（**不再是 1000009**）；`invalidHoursTasks=1`；`invalidEstimateHours=1000000`；`maxTaskHours=10000` |
| V4 | 清理边界任务 | DELETE → 200；复核 `taskCount=3`、`estimatedTaskCount=1` |
| V5 | 页面可达 | `/projects`、`/projects/9`、`/projects/templates` 全 200 |
| V6 | 日志 | 近 10 分钟无 panic / fatal / error |

### 关键对照（项目 9）

```
修复前：estimateHours = 1000009   （9 + 1000000，错误值当作事实）
修复后：estimateHours = 9
        invalidHoursTasks = 1
        invalidEstimateHours = 1000000
        maxTaskHours = 10000
```

> 历史越界任务（task 8, 1000000h）**未被删除**，只是不再计入汇总并显式回报；
> 需业务侧修正该任务工时后，告警才会消失。

## 3. 未覆盖（需浏览器人工复验）

1. 任务弹窗输入 `999999999`：应在**不点保存前**就出现红字（表单校验），而非失焦静默钳制；
2. 项目列表/模板列表：首屏直接可见操作列，文字横排；
3. 阶段门页：「门禁阻塞项」列头可见（≥260px）。

## 4. 回滚

```bash
cd /home/n9e/itsm/repo
git checkout -f deploy-demo-project-regression-b3f9762c
sed -i 's/^VERSION=.*/VERSION=1.8.0-dev.b3f9762c/' .env.demo
echo 'passw0rd' | sudo -S -p '' env VERSION=1.8.0-dev.b3f9762c \
  docker compose --env-file .env.demo -f docker-compose.demo.yml up -d \
  itsm-backend-demo itsm-worker-demo itsm-frontend-demo
```
