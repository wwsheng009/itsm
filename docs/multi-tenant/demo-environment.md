# ITSM 多租户演示环境（独立演示栈）

> 状态：当前（2026-11-11 更新）｜定位：面向**学习 / 演示 / 培训**的全量多租户数据集与独立环境；与主站（`8088`）**完全隔离**，可随时重建重置。

## 1. 入口与形态

| 项 | 值 |
|---|---|
| 演示入口 | `http://172.18.3.238:8089`（独立栈 `itsm-demo`） |
| 数据库 | `itsm_demo`（复用宿主 `itsm-postgres-prod` 实例，独立库） |
| 版本 | 以 `.env.demo` 中 `VERSION` 为准（2026-11-11 全量重部署后 `1.8.0-dev.bf131540`） |
| 部署模式 | `saas_msp`（MSP 跨租户能力开启） |
| 容器组 | `itsm-{backend,worker,frontend,nginx,redis,ai-service}-demo`（6 个常驻，均 healthy）+ `itsm-init-demo` 一次性迁移任务 |
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

- 服务商经理可跨 3 客户查看 **34 张 `[DEMO]` 工单**（含状态、优先级、负责人、SLA 截止）；
  当前演示库另有 31 张历史验收/E2E 残留，工作台实际 65 张；演示时建议用 `[DEMO]` 关键字筛选。
- 「SLA 风险」看板支持 24h 临近窗口；**当前演示库 `slaDueSoon=0`**，未关闭样本均已超期
  （种子基准时间较早），不再是“含临近样本”；如需临近样本请重跑 SLA 回填/调整种子。
- 「客户用量」看板按客户展示成员/未关闭/新增；
- 工作台行内可对 MSP 托管单执行 **回复 / 改状态 / 指派**（演示数据中已产生真实协同记录）。
- `review-test`（tenant id=6）为 2026-10-28 评审遗留测试租户：`provider_admin` 管理面可见，
  但无 MSP 分配、不进入工作台汇总，不属于 demo 种子数据。

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

**2026-11-10 E2E 验证结果（2026-11-11 已从当前工作区 HEAD 全量发布 `1.8.0-dev.bf131540`）**

| 项 | 原 238 现象 | 当前状态 |
|---|---|---|
| 变更受影响 CI | `affectedCis` 填 CI 编号时 CMDB 影响分析为 0（数字 ID 正常） | 已修复；编号与数字 ID 均正常，E2E 通过 |
| 服务请求审批 | BPMN 停在“请求受理/任务分配”时，L2/L3 间歇 500「同步流程审批任务失败」 | 已修复；审批桥接仅匹配审批任务，L1/L2/L3 通过 |
| 跨租户删除 | `DELETE /tickets/:id` 跨租户返回 500 | 已修复；返回 404，不泄露资源存在性 |
| 跨租户问题关联 | 关联其他租户工单/事件/变更返回 500 | 已修复；返回 400 校验错误 |
| 事件转问题 | 事件详情不返回 `problemId`，“查看问题”入口不显示 | 已修复；详情返回 `problemId`，入口可用 |
| 服务目录顶部统计 | 固定桶与标签页计数矛盾 | 已修复；按真实分类动态聚合 |
| 平板 768 布局 | Dashboard body 横向溢出 | 已修复；768–991 默认收起侧栏 |
| 终端用户 Dashboard | 无 `dashboard:read` 却请求 overview，产生 403/console error | 已修复；按权限门控并显示受限提示 |
| MSP 经理角色契约 | 登录 `role=msp_manager`，刷新后 `/auth/me` 返回 `role=end_user` | 已修复；`/auth/me` 与 JWT effective role 对齐 |
| 工单详情 AI 权限 | 无 `ai:read` 仍渲染 AI 面板并请求 `/ai/rag/search`、`/ai/triage` | 已修复；入口级隐藏，E2E 断言无 AI 请求 |
| 变更详情 AI 权限 | 无 `ai:read` 仍展示 AI 影响分析入口 | 已修复；变更 AI 计划卡按 `ai:read` 门控 |
| 发布单跨租户引用 | 创建发布单可写入其他租户的 `changeId`/`ownerId`/`departmentId`/`customerId` | 已修复；非法引用返回 400 |
| 发布↔变更 UI 关联 | 表单无“关联变更”入口，详情不展示 `changeId` | 已修复；新增 `ChangeSelect` 与详情跳转 |
| 服务请求删除权限 | 租户管理员无 `service_request:delete`，无法清理 | 已修复；租户管理员可删除，存量租户迁移已应用 |
| 服务请求删除语义 | 软删后 GET 返回 HTTP 200，仅包体 `code=404` | 已修复；返回 HTTP 404 + 业务码 4004 |
| 多租户跳转返回入口 | provider 进入客户租户后 Header 不显示“返回工作台” | 已修复；`DeepTenantReturn` 独立渲染，切回 provider 后打开工作台；E2E 通过 |
| 多租户连续建单 | 租户序列与全局唯一 `ticket_number` 冲突，重试后 500 | 已修复；Redis/DB 发号共用全局探针，跳过他租户占用号；E2E 通过 |
| 工单附件列表 | `end_user`/`it_admin`/`security_admin`/provider 深度态打开 ticket 74 显示「获取附件列表失败」 | 已修复；授权跟随 `ticket:read` + active 用户；45/45 E2E 通过 |

> 同轮完成硬编码角色白名单专项审计及穷尽复核补漏：修复 Header AI `isAdmin()` 旁路、
> AI 通知/审批 fallback 无效角色码、MSP 写权限重复白名单、provider `allowedActions`
> 行级误判；内部备注 staff 限制复核保留，CC 管理与旧前端角色守卫登记为待产品决策。
> 详见 [`docs/review/2026-11-11-role-whitelist-audit.md`](../review/2026-11-11-role-whitelist-audit.md)。
>
> 同轮落地 C 方案第一阶段：membership 作为租户内身份，工单 requester/creator membership
> 回填，provider 代客建单要求显式客户 `requesterId`。详见
> [`membership-tenant-identity-c-plan.md`](./plan/membership-tenant-identity-c-plan.md)。

> 本轮执行/修复详情：`docs/review/2026-11-10-msp-demo-e2e-validation.md`；
> 测试套件：`itsm-frontend/tests/e2e/demo/`。
>
> **待产品决策缺口**：provider 深度切换到客户作用域后直接建单仍返回 404「申请人不存在或不可用」；
> 当前已验证主路径为客户侧建单、provider 深度切换后读取/评论/改状态及工作台指派/回复。

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

## 8. 发布、回滚与上传冒烟（2026-11-06）

演示栈发布统一使用 `scripts/deploy-demo.sh`，禁止再手工拼「build → sed → up」步骤。
该脚本把 238 曾出现的 `uploads` bind-mount 权限问题、初始化未成功即切主服务、
以及「服务 healthy 但附件上传 400」纳入发布门禁。

### 8.1 发布

```bash
cd /home/n9e/itsm/repo

# 1) 只读预检（工具 / env / compose / DB / 当前上传目录可写性）
bash scripts/deploy-demo.sh preflight

# 2) 完整发布：备份 → 构建 → 修 uploads ACL → init exit=0 → up → 冒烟
bash scripts/deploy-demo.sh deploy --version "1.8.0-dev.$(git rev-parse --short=8 HEAD)"

# 等价的 Makefile 入口
make demo-deploy DEMO_ARGS='--skip-build'
```

若执行用户不在 docker 组，脚本会自动对 Docker 命令使用 `sudo`（交互式环境只需认证一次，
也可直接 `sudo bash scripts/deploy-demo.sh ...`）；证据目录会归回 `SUDO_USER`，避免 root
独占备份文件。

AI 源码未变更时避免重复构建大镜像，可显式复用已验证镜像并重打版本 tag：

```bash
bash scripts/deploy-demo.sh deploy \
  --version "1.8.0-dev.$(git rev-parse --short=8 HEAD)" \
  --reuse-ai itsm-ai-service:1.8.0-dev.5b070145
```

常用选项：

| 选项 | 用途 |
|---|---|
| `--skip-build` | 复用已存在的目标版本镜像 |
| `--skip-backup` | 跳过备份（仅限已确认可丢弃的临时环境） |
| `--skip-attachment-smoke` | 跳过认证附件上传冒烟（不推荐） |
| `--smoke-url` | 覆盖冒烟入口（默认 `http://127.0.0.1:${DEMO_HTTP_PORT:-8089}`） |
| `--smoke-ticket-id` | 指定附件冒烟工单（默认取当前管理员可见第一张） |
| `--allow-dirty` | 允许 tracked 工作树有未提交变更（默认拒绝） |

发布证据固定写入：

```text
backups/demo-release-<version>-<UTC时间>/
├── .env.demo.before       # 发布前环境文件（600）
├── .env.demo.deployed     # 发布后环境文件（600）
├── itsm_demo.dump         # pg_dump custom format
├── itsm_demo.dump.sha256
├── rollback.txt           # 前序版本/分支/commit/镜像
├── smoke-attachment.json  # 附件上传/预览/删除结果
└── release.txt            # 发布后容器、镜像与版本证据
```

### 8.2 uploads 权限规则

后端镜像以 `app` 用户运行（当前 UID/GID `100:101`），而宿主 bind mount 由
`n9e:1000` 创建，直接 `up -d` 会在新工单/租户首次上传时失败：

```text
attachment storage failure: create dir:
mkdir uploads/tickets/1: permission denied
```

`deploy-demo.sh` 会在切换服务前自动对 `deploy/demo/uploads` 施加 ACL：

- 递归授予容器 UID/GID `rwx`；
- 对全部目录设置 default ACL，保证后续新建的 `tickets/<tenant>/<domain>/<ticket>` 可继承；
- 使用目标 backend 镜像以 `${APP_UID}:${APP_GID}` 执行临时容器写入探针。

手工修复与自检：

```bash
make demo-permissions DEMO_ARGS='--version 1.8.0-dev.<sha>'
getfacl -p deploy/demo/uploads/tickets
docker exec itsm-backend-demo sh -c \
  'touch /app/uploads/tickets/.probe && rm -f /app/uploads/tickets/.probe'
```

若宿主文件系统不支持 ACL，脚本回退 `chown`，并提示宿主读取需 sudo。

### 8.3 冒烟与回滚

```bash
# 健康 + readyz + /api/v1/version + 真实评论附件上传/预览/删除
make demo-smoke DEMO_ARGS='--version 1.8.0-dev.<sha>'

# 容器与线上版本
make demo-status
```

附件冒烟使用工单域端点 `POST /api/v1/tickets/:id/attachments` 且
`usage=comment_attachment`，与评论编辑器真实调用一致；测试附件为 1x1 PNG，
校验 preview SHA256 后立即删除。若环境无可见工单则明确输出 `skipped`。

回滚时从证据目录恢复旧环境并切回旧镜像：

```bash
BACKUP=backups/demo-release-<version>-<UTC时间>
cp -p "$BACKUP/.env.demo.before" .env.demo
docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --force-recreate itsm-backend-demo itsm-worker-demo itsm-frontend-demo itsm-ai-service-demo
```

数据库仅在确认新版本造成数据不可逆变更时恢复：

```bash
docker exec -i itsm-postgres-prod pg_restore -U itsm -d itsm_demo --clean --if-exists \
  < "$BACKUP/itsm_demo.dump"
```
