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
| 客户·华辰智能制造 | `demo.mfg.itadmin` | it_admin（IT 主管，L2 审批） |
| 客户·华辰智能制造 | `demo.mfg.secadmin` | security_admin（安全管理员，L3 审批） |
| 客户·仁和医疗集团 | `demo.med.{admin,agent,lead,user,itadmin,secadmin}` | 同上（信息科/服务台/科室主任/医生/IT 主管/安全） |
| 客户·云帆教育科技 | `demo.edu.{admin,agent,lead,user,itadmin,secadmin}` | 同上（信息中心/支持/教务主任/教师/IT 主管/安全） |

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
- **领域单据（事件/问题/变更）**：
  - 事件：每租户 6~7 张，覆盖 `new`/`acknowledged`/`in_progress`/`on_hold`/`escalated`/
    `resolved`/`closed`（含监控来源与升级样本，按事件状态机自动推导合法流转路径）；
  - 问题：每租户 2~3 张，`open`/`investigating`/`identified`；
  - 变更：每租户 3 张，`draft`/`pending`（待审批）/`completed`（走 submit→approve→schedule→
    start→complete 全链路）；其中 draft/pending 指派给服务台角色（变更列表对非管理角色按
    「本人创建/被指派」行级收敛，指派后服务台可见，属预期权限语义）。

**主数据与知识**

- 每租户 5 篇已发布知识文章（按行业定制：制造/医疗/教育，覆盖故障排查/应急预案/规范/服务指南）；
- 每租户 5~6 个 CMDB 配置项（应用/数据库/网络/存储等，含属性与环境/关键度）；
- **CI 关系**：每租户 2~3 条（depends_on / connects_to / runs_on），支撑拓扑图与影响分析演示；
- **工单类型**：每租户安装 4 个产品预设（通用事件 / PACS 事件 / 数据库变更 / 账号权限申请），
  支撑「工单类型」页与新建工单的类型选择；
- **已知错误（KEDB）**：每租户 2 条（含症状/根因/规避/解决），支撑「问题管理 → 已知错误」页；
- 服务目录、SLA、CI 类型、审批链等来自租户模板（开箱可用）。

**组织与审批待办（部门字段）**

- 演示用户 `department` 为**真实部门**（岗位在账号名后缀中体现）：如制造租户
  `admin/agent/itadmin/secadmin` 属「信息技术部」，`lead/user` 属「生产部」；
- 服务请求「待审批」按 `申请人部门 == 审批人部门`（manager 层级）过滤，
  故部门经理能看到本部门员工提交的待批服务请求（演示数据：每位 lead 有 2 条）。

**页面浏览权限速查（租户模板 + 演示级补授）**

| 页面 | 权限码 | 模板已授权角色 | 演示说明 |
|---|---|---|---|
| 工单类型 `/tickets/types` | `ticket_type:read` | admin / manager / agent / it_admin（+多数管理角色） | 租户模板原本不安装工单类型数据，已由种子器按预设安装 |
| 服务目录 `/service-catalog` | `service_catalog:read` | admin / end_user / it_director | **manager / it_admin / agent 由演示级补授**（模板未授，已登记待产品决策） |
| 服务请求 `/service-requests` | `service_request:read` | admin / manager / agent / it_admin / end_user | 列表范围＝「我的请求」＋「我的待审批（按角色与部门）」；lead 默认无自有请求，待审批页有数据 |
| 已知错误 `/problems/known-errors` | `problem:read`（KEDB 复用 problem 词表） | admin / manager / agent / it_admin / dba / l2_support 等 | end_user 无问题域读权限（设计内） |

**跨租户协同（工作台）**

- 服务商经理可跨 3 客户查看 34 张工单（含状态、优先级、负责人、SLA 截止）；
- 「SLA 风险」看板含「临近」样本（窗口 24h）；
- 「客户用量」看板按客户展示成员/未关闭/新增；
- 工作台行内可对 MSP 托管单执行 **回复 / 改状态 / 指派**（演示数据中已产生真实协同记录）。

**服务请求与审批（每客户租户）**

- 服务目录：每租户 3 个 [DEMO] 目录项（账号开通 / 网络与访问 / 终端支持等，需审批）；
- 服务请求：每租户 3 张，覆盖审批链路不同阶段：
  - 「待审批」（`submitted`，L1 经理待批）；
  - 「经理/科室主任/教务已批」（`manager_approved`）；
  - 「两级已批」（`it_approved`，教育租户样本）。
- **审批中心待办**：以 `demo.<tenant>.lead`（manager）登录 → 「审批中心」可见 3 条待领取的
  工单流程任务（ticket_general_flow，含批准/拒绝入口），演示「我的待办」体验。
- 角色配套：每租户新增 `itadmin`（it_admin，L2）与 `secadmin`（security_admin，L3）账号，
  可按层级演示审批权限差异。

**SLA 超时样本（可选，仅演示库）**

- 工单创建即写入 SLA 响应/解决截止时间（实测 46/46 覆盖）；
- 「SLA 监控 / 超时工单」需要逾期样本，正常 API 无法回填历史时间 → 提供一次性脚本
  `scripts/demo/sla_backdate.sql`（每租户 2 张未关闭 [DEMO] 工单回填为已超时，幂等）。

## 4. 重新生成 / 重置

**幂等重跑（补齐缺口，不产生重复数据）**：

```bash
python scripts/demo/seed_demo_dataset.py \
  --base-url http://172.18.3.238:8089 \
  --admin-password 'Demo@2026!Admin'
# 分阶段：--stages tenants,users,allocations,masterdata,ticket_types,rbac_grants,topology,records,domain,service_requests,kedb,cross,verify
```

> 实测：全阶段（13 个阶段）重复执行 `created=0 / existing=188 / failed=0`；
> 幂等策略＝按租户 code、用户名去重；业务单据按标题匹配，
> 状态与规格不符时自动删除重建（自愈），跨租户回复按标记去重。

**可选补充（SLA 超时 / 部门对齐 / 重复清理，仅演示库执行一次）**：

```bash
docker exec -i itsm-postgres-prod psql -U itsm -d itsm_demo < scripts/demo/sla_backdate.sql
docker exec -i itsm-postgres-prod psql -U itsm -d itsm_demo < scripts/demo/fix_user_departments.sql
docker exec -i itsm-postgres-prod psql -U itsm -d itsm_demo < scripts/demo/fix_sr_duplicates.sql
```

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
- **服务请求层级审批的 BPMN 桥接限制（已登记）**：部分服务请求在推进到 IT/安全层级时
  返回 500「同步流程审批任务失败」（业务侧角色满足，但流程任务操作人鉴权不匹配）；
  同一实例重试与管理员代批均失败。演示数据因此保留在可推进到的最深层级
  （L1 已批 / L2 已批样本可用），标题后缀与真实状态对齐（种子器按「标题词干」匹配，
  已审批实例不可改名时保留现状）；
- **服务目录读权限模板差异（已登记）**：租户模板 `service_catalog:read` 授给了
  admin/end_user/it_director，未授 manager/it_admin/agent；演示环境由种子器
  `rbac_grants` 阶段补授（仅追加）。是否进产品模板待产品决策；
- SLA「已超时」样本依赖一次性回填脚本（见上），重建演示库后需重新执行；
- 演示账号口令统一且公开，仅用于演示环境，**请勿在真实环境复用**；
- AI 能力取决于 `.env.demo` 是否配置 LLM 凭据，未配置时自动降级，不影响主链路演示；
- 每次重建若使用最新镜像，请确保镜像已包含
  `fix(seeder): 序列校正在事务提交后执行`（提交 `5e4eab85`，修复全新库首次开通租户 500）。

## 6. 租户级邮件与系统配置（P0，2026-10-13）

演示库三个租户已配置**各自的 SMTP**（演示栈内 mock sink），用于演示"不同租户邮件互不影响"：

| 租户 | system_configs（tenant_id） | smtpHost / 端口 | 发件人 | 说明 |
|---|---|---|---|---|
| demo-mfg（3） | `smtpHost=itsm-smtp-mock-a` | `itsm-smtp-mock-a:2525`（无 SSL） | `ops-a@tenant-a.example` | 邮件落入容器 `/tmp/mock_a.log` |
| demo-med（4） | `smtpHost=itsm-smtp-mock-b` | `itsm-smtp-mock-b:2525`（无 SSL） | `ops-b@tenant-b.example` | 邮件落入容器 `/tmp/mock_b.log` |
| demo-edu（5） | 留空（未配置） | — | — | 演示"未配置 → 部署级回退；无回退则告警跳过" |

- mock sink 容器（**不属于 compose 编排**，需手动维护；镜像 `python:3.12-slim-bookworm` 已在 238 本地）：
  `docker run -d --name itsm-smtp-mock-a --network itsm-demo-network -v /home/n9e/itsm/repo/scripts/demo/mock_smtp.py:/mock_smtp.py:ro -e MOCK_LOG=/tmp/mock_a.log python:3.12-slim-bookworm python /mock_smtp.py 2525`
  （mock-b 同理，`MOCK_LOG=/tmp/mock_b.log`）
- 查看收到邮件：`docker exec itsm-smtp-mock-a cat /tmp/mock_a.log`
- 端到端验收（幂等，可重跑）：`python scripts/demo/verify_scd_p0.py` → **23/23 PASS**
  （A/B 租户隔离 / 未配置告警 / 保存即生效 / 密文+掩码 / 掩码回写不覆盖 / Redis 失效订阅）
- 凭据：`smtpPassword` 在库中为 AES-GCM 密文（`v1:` 前缀，密钥 = 演示栈 `.env.demo` 的 `DEMO_CONFIG_SECRET_KEY`）；
  页面/接口只回显 `******`，提交 `******` 表示保持原值。
- 注意：`DEMO_CONFIG_SECRET_KEY` 轮换后旧密文无法解密（需重新保存邮件密码）；
  演示栈缺失该键时（`SERVER_ENV=production`）保存敏感配置会被拒绝（fail-closed）。

## 7. 相关提交

| 提交 | 内容 |
|---|---|
| `577a40eb` | 演示栈（`docker-compose.demo.yml` + 演示 Nginx + 环境变量模板） |
| `5e4eab85` | 序列校正时机修复（全新库首次开通租户 500） |
| `118c2322` + `c7ac0abc` | OB-18 P0：租户配置解析器（Redis 失效）+ 按租户邮件解析 + 凭据 AES-GCM/掩码/审计 + 前端文案；238 v8 实测 23/23（含占位 SMTP 回退缺陷修复） |
| 本批 | 演示数据集种子器 `scripts/demo/seed_demo_dataset.py` + 本指南 |
