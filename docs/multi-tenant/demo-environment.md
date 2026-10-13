# ITSM 多租户演示环境（独立演示栈）

> 定位：面向**学习 / 演示 / 培训**的全量多租户数据集与独立环境；与主站（`8088`）**完全隔离**，可随时重建重置。

## 1. 入口与形态

| 项 | 值 |
|---|---|
| 演示入口 | `http://172.18.3.238:8089`（独立栈 `itsm-demo`） |
| 数据库 | `itsm_demo`（复用宿主 `itsm-postgres-prod` 实例，独立库） |
| 版本 | 以 `.env.demo` 中 `VERSION` 为准（当前 `1.8.0-dev.5e4eab85`） |
| 部署模式 | `saas_msp`（MSP 跨租户能力开启） |
| 容器组 | `itsm-{backend,worker,frontend,nginx,redis,ai-service,init}-demo`（7 个） |
| 部署文件 | `docker-compose.demo.yml`、`nginx/demo/conf.d/default.conf`、`.env.demo.example` |

与主站的关系：仅共享宿主 PostgreSQL 实例与镜像（`*-demo` 容器、独立 Redis、独立端口），
业务数据、会话、队列互不影响；主站 `8088` 不受演示数据变更影响。

## 2. 演示账号（统一口令，公开使用）

**口令**：租户账号 `Demo@2026!`；平台管理员 `Demo@2026!Admin`。

| 租户 / 用途 | 用户名 | 角色 / 岗位 |
|---|---|---|
| 平台（超级管理员） | `admin` | super_admin |
| 服务商·云维科技 | `demo.msp.manager` | provider_admin（MSP 服务经理） |
| 服务商·云维科技 | `demo.msp.agent1` | provider_agent（一线工程师，负责制造/教育） |
| 服务商·云维科技 | `demo.msp.agent2` | provider_agent（二线工程师，负责医疗） |
| 客户·华辰智能制造 | `demo.mfg.admin` | admin（信息化管理员） |
| 客户·华辰智能制造 | `demo.mfg.agent` | agent（IT 服务台） |
| 客户·华辰智能制造 | `demo.mfg.lead` | manager（生产部经理 / 审批人） |
| 客户·华辰智能制造 | `demo.mfg.user` | end_user（车间主管） |
| 客户·仁和医疗集团 | `demo.med.admin` / `demo.med.agent` / `demo.med.lead` / `demo.med.user` | 同上（信息科/服务台/科室主任/医生） |
| 客户·云帆教育科技 | `demo.edu.admin` / `demo.edu.agent` / `demo.edu.lead` / `demo.edu.user` | 同上（信息中心/支持/教务主任/教师） |

## 3. 数据集清单

**租户与授权**

- 1 个服务商租户（`demo-provider`）+ 3 个客户租户（`demo-mfg` 制造业 / `demo-med` 医疗 / `demo-edu` 教育）；
- 7 项 MSP 分配（经理 3 个客户 primary；agent1 覆盖制造+教育；agent2 覆盖医疗）；
- 每租户经「租户模板开通」安装：角色/权限/菜单、SLA 定义与策略、服务目录、CI 类型、
  审批组、BPMN 流程模板、标准变更等产品基线。

**业务单据（每客户租户）**

- 11~12 张 [DEMO] 工单，**覆盖全部 7 种状态**：
  `new` / `open` / `pending`（×2）/ `in_progress` / `resolved` / `closed` / `cancelled`；
- 类型覆盖：事件（incident）/ 服务请求（service_request）/ 变更（change）；
- 优先级覆盖：低 / 中 / 高 / 紧急；
- 每张含处理时间线与评论（客户侧 + 服务商侧）；
- **MSP 托管单**（assignee=服务商技术员）：写入 `managed_by_user_id`/MSP 快照，
  可从服务商「跨客户工作台」回复、改状态、指派（跨租户交互演示）。

**主数据与知识**

- 每租户 2 篇已发布知识文章（按行业定制：制造/医疗/教育）；
- 每租户 3~4 个 CMDB 配置项（应用/数据库/网络/存储等，含属性与环境/关键度）；
- 服务目录、SLA、CI 类型、审批链等来自租户模板（开箱可用）。

**跨租户协同（工作台）**

- 服务商经理可跨 3 客户查看 34 张工单（含状态、优先级、负责人、SLA 截止）；
- 「SLA 风险」看板含「临近」样本（窗口 24h）；
- 「客户用量」看板按客户展示成员/未关闭/新增；
- 工作台行内可对 MSP 托管单执行 **回复 / 改状态 / 指派**（演示数据中已产生真实协同记录）。

## 4. 重新生成 / 重置

**幂等重跑（补齐缺口，不产生重复数据）**：

```bash
python scripts/demo/seed_demo_dataset.py \
  --base-url http://172.18.3.238:8089 \
  --admin-password 'Demo@2026!Admin'
# 分阶段：--stages tenants,users,allocations,masterdata,records,cross,verify
```

> 实测：全阶段重复执行 `created=0 / existing=80 / failed=0`；
> 幂等策略＝按租户 code、用户名去重；业务单据按标题匹配，
> 状态与规格不符时自动删除重建（自愈），跨租户回复按标记去重。

**整库重建（演示环境重置）**：

```bash
cd /home/n9e/itsm/repo
docker compose --env-file .env.demo -f docker-compose.demo.yml down -v
docker exec itsm-postgres-prod psql -U itsm -d postgres -c "DROP DATABASE IF EXISTS itsm_demo"
docker exec itsm-postgres-prod psql -U itsm -d postgres -c "CREATE DATABASE itsm_demo OWNER itsm"
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -c "CREATE EXTENSION IF NOT EXISTS vector"
docker compose --env-file .env.demo -f docker-compose.demo.yml up -d
python scripts/demo/seed_demo_dataset.py --base-url http://172.18.3.238:8089 --admin-password 'Demo@2026!Admin'
```

## 5. 已知说明

- 平台侧用户治理接口在本环境返回 `TENANT_USER_ADMIN_DISABLED`（功能未启用），
  演示账号的停用请通过「口令轮换」或重建演示库处理；
- 演示账号口令统一且公开，仅用于演示环境，**请勿在真实环境复用**；
- AI 能力取决于 `.env.demo` 是否配置 LLM 凭据，未配置时自动降级，不影响主链路演示；
- 每次重建若使用最新镜像，请确保镜像已包含
  `fix(seeder): 序列校正在事务提交后执行`（提交 `5e4eab85`，修复全新库首次开通租户 500）。

## 6. 相关提交

| 提交 | 内容 |
|---|---|
| `577a40eb` | 演示栈（`docker-compose.demo.yml` + 演示 Nginx + 环境变量模板） |
| `5e4eab85` | 序列校正时机修复（全新库首次开通租户 500） |
| 本批 | 演示数据集种子器 `scripts/demo/seed_demo_dataset.py` + 本指南 |
