---
name: itsm-msp-operations
description: 通过 ITSM REST API 操作 MSP 托管服务商跨租户能力（分配管理、客户管理与建号、模板/工单类型受控下发、跨客户工作台：工单/任务/变更/问题/服务请求、批量操作、视图、报表与审计聚合）。Use when the agent needs 以 MSP 员工身份查看客户资产、跨租户处理工单、下发工单类型或工作流模板、跑客户绩效报表或审计面板；covers /api/v1/msp 全族（msp_allocation/msp_customer/msp_ticket/msp_report 权限码，MSPMiddleware 门禁）。Do NOT use for 单租户业务操作（itsm-ticket/change/problem-operations，用客户身份）、SLA 合同下发（itsm-sla-operations 的 MSP 子面）。
---

# ITSM MSP 跨租户操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent，以 **MSP 员工身份**）。全族挂 `MSPMiddleware`，非 MSP 用户 403。权限码：`msp:read` / `msp_allocation:read|write` / `msp_customer:read|write` / `msp_ticket:read|write` / `msp_report:read`。**授权链双层**：端点 RBAC + 条目级 allocation 服务端二次校验（mspguard）。

## 脚本工具（scripts/itsm_msp.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`。**首次配置用 `setup`**（一次性写入部署地址与凭据到 `~/.itsm/config.json` / `~/.itsm/itsm.token`，权限收紧、掩码回显），之后命令自动读取、无需 `--base/--token-file`；**localhost 只是脚本兜底，不是部署默认**（解析优先级：`--base` > `ITSM_BASE_URL` > 配置 > 兜底）。**47 个子命令**：

```bash
BASE=https://itsm.example.com/api/v1     # ← 换成部署地址（含 /api/v1；内部 demo http://172.18.3.238:8089/api/v1）
# A) PAT（推荐，个人中心→安全设置创建）——管道传入，不进 argv/shell 历史：
cat pat.txt | python scripts/itsm_msp.py setup --base $BASE --token-stdin
# B) 或账号换 JWT（15 分钟过期；口令走环境变量或交互式隐藏输入，勿明文写命令行）：
ITSM_USERNAME=<MSP_USER> ITSM_PASSWORD='***' python scripts/itsm_msp.py setup --base $BASE --from-env

python scripts/itsm_msp.py config     # 自检：生效地址/凭据来源/连通性/身份（不回显明文）

# 之后所有命令自动读取配置（无需 --base/--token-file）：
python scripts/itsm_msp.py status
python scripts/itsm_msp.py customers
python scripts/itsm_msp.py workbench-summary
python scripts/itsm_msp.py workbench-tickets --status open --customer-id 3
python scripts/itsm_msp.py ticket-reply 63 --content 'MSP 侧处理说明'
python scripts/itsm_msp.py reports-performance
python scripts/itsm_msp.py audit-summary
```

Windows PowerShell 等价：`$B = 'https://itsm.example.com/api/v1'; Get-Content pat.txt | python scripts/itsm_msp.py setup --base $B --token-stdin`；token 落盘 `$HOME\.itsm\itsm.token`（自动读取）。

子命令分组：setup/config ｜ status/context ｜ allocations（list/history/create/deallocate）｜ customers（list/tickets/roles/建号-未封装 curl 口径）｜ workflow-templates+distribute ｜ ticket-types 下发链（distributions/distribute/preview/publish/unbind，D-TT-1）｜ workbench（tickets/summary/ticket-get/comments/assign/reply/status/batch≤100/views+default）｜ tasks（list/claim/complete）｜ changes/problems/service-requests（list/get/status）｜ reports-customers/reports-performance/audit-summary。

## 核心流程：工单类型受控下发（D-TT-1）

1. `tt-distribute <ctid> <typeId>`（copy-in 生成草稿副本 + 来源追踪 + 受控锁）→ 2. `tt-preview`（下发预览）→ 3. `tt-publish`（发布同步到客户）→ 4. 需解绑时 `tt-unbind`（转客户 native 类型）。

## 决策要点（硬规则）

- **审批类动作不在 MSP 面**：变更 approve/reject、服务请求 approval 保留在客户治理面——MSP 侧只能做**处理类**状态迁移（D8 白名单）。
- **每租户限流 + 白名单**：`workbench/batch` ≤100 条、仅低危动作、逐条授权与审计；高危动作被拒。
- **XFD8 隐私边界**：客户内部备注（isInternal=true）不下发 MSP（comments 只读面可见公开评论）。
- **任务写回在客户租户内执行**（claim/complete），逐条审计 `source=workbench`。
- 工单类型下发是**受控锁**链路：copy-in → 受控锁 → 发布同步 → 解绑转 native；客户侧不可改受控字段。
- `workbench/views` 依赖灰度开关 `WORKBENCH_VIEWS_ENABLED`（默认关），未开启时不可用。
- 建号与角色候选（IP-P0-5 / IP-P2-7）：`POST /customers/:ctid/users`（脚本未封装，curl 口径）+ `GET /customers/:ctid/roles`。

## 排障与验证

排障按序查 [references/msp-api.md](./references/msp-api.md) §2；403 先确认 MSP 身份（`status`），再确认端点权限码与条目级 allocation。验证口径：对 demo 环境（MSP 账号）跑 status → customers → workbench-summary → workbench-tickets → audit-summary 链路并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
