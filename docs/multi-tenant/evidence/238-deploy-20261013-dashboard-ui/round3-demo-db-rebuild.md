# 238 演示库整库重建证据（2026-10-13 第三轮）

> 状态：**已完成**｜触发：按 `docs/multi-tenant/demo-environment.md §4 整库重建` 重置演示环境
> 代码：远端 `deploy-demo-b7` @ `9e9a9c47`（含跨租户分类收口 `1e78d08e`）
> 镜像：`1.8.0-dev.de337963`（backend/frontend 重建版，ai retag）

## 1. 执行步骤（文档 §4）

1. 远端仓库同步：增量 bundle → `git fetch` → `deploy-demo-b7` checkout；
2. `docker compose --env-file .env.demo -f docker-compose.demo.yml down -v`（含 redis 卷）；
3. 终止 `itsm_demo` 残留连接 → `DROP DATABASE` → `CREATE DATABASE itsm_demo OWNER itsm` → `CREATE EXTENSION vector`；
4. `up -d` → `itsm-init-demo` **exit 0**（迁移 + 产品基线，tenants=1）；
5. 种子器（本机，`python -X utf8 scripts/demo/seed_demo_dataset.py --base-url http://172.18.3.238:8089 --admin-password 'Demo@2026!Admin'`，13 阶段）：
   **新建 163｜已存在 25｜失败 0｜164s**；
6. 文档 §4 补充脚本：`sla_backdate.sql`（每客户租户 2 张未关闭单回填超期）、`fix_user_departments.sql`（21 行部门对齐）、`fix_sr_duplicates.sql`。

> 注意：Windows 控制台 GBK 编码会让种子器在打印 `↔` 时抛 `UnicodeEncodeError`——须用 `python -X utf8`（或 `PYTHONIOENCODING=utf-8`）运行；脚本幂等，中断后重跑自动续跑。

## 2. 重建后核对（SQL）

| 指标 | 结果 |
|---|---|
| 租户 / 用户 / MSP 分配 | 5 / 22 / 7 |
| **客户租户跨租户 MSP 授权（4 资源族）** | **0**（新供给路径端到端验证：不再"先克隆后剥离"） |
| 客户角色 msp 码残留 | 仅 `msp:read`（+部分 `msp:write` 区域码） |
| SLA 超期 / 临近（未关闭） | demo-mfg 2/3、demo-med 2/1、demo-edu 2/1（合计 6/5） |
| MSP 托管单 | mfg 2、med 1、edu 1 |
| provider 租户 msp 授权 | default/demo-provider 各 60（完整） |
| 数据集清点（种子器报告） | 每客户租户：工单 11~12（7 状态）、事件 6~7、问题 2~3、变更 3、服务请求 3、知识 5、CI 5~6、工单类型 4、已知错误 2 |

## 3. 重建后浏览器冒烟（`1.8.0-dev.de337963`）

| 场景 | 结果 |
|---|---|
| 服务商 `demo.msp.manager` | 落 `/msp/workbench`；SLA 看板 超期 6 / 临近 5（与 SQL 一致）；客户用量 3 客户；`/dashboard` 跨租户分区 3/22/6/5/0 |
| 客户 `demo.mfg.admin` | 落 `/dashboard`；**无跨租户分区**；侧栏无 MSP 菜单；手动访问 `/msp/workbench` → `/403` |

> 重建会失效旧会话（cookie 指向的会话记录随库重建清空），登录页按预期回跳，属正常现象。

## 4. 说明

- 本次重建后，演示环境即为「分类收口后」的全新基线：客户租户从建库起就不含跨租户 MSP 数据面码（此前靠存量对账剥离）；
- 如需再次重置，直接重复 `demo-environment.md §4`（幂等；种子器重复执行 `created=0`）。
