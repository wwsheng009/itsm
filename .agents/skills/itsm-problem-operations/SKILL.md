---
name: itsm-problem-operations
description: 通过 ITSM REST API 操作问题管理（问题单 CRUD、调查/根因/解决方案、关闭、关联 ticket/incident/change、评论、统计趋势热点、问题 SLA）。Use when the agent needs 创建或跟进问题单、记录根因与解决方案、关联相关工单/事件/变更、查看问题统计趋势热点；covers /api/v1/problems 全族，含 open→investigating→identified→resolved→closed 状态机（409 守卫）。Do NOT use for 事件监控告警域（/incidents，见 README §7）、工单操作（itsm-ticket-operations）。
---

# ITSM 问题管理操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。权限：读 `problem:read` / 写 `problem:write` / 删除 `problem:delete`；行级写权限=创建人/受理人/管理员。

## 脚本工具（scripts/itsm_problem.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`。**首次配置用 `setup`**（一次性写入部署地址与凭据到 `~/.itsm/config.json` / `~/.itsm/itsm.token`，权限收紧、掩码回显），之后命令自动读取、无需 `--base/--token-file`；**localhost 只是脚本兜底，不是部署默认**（解析优先级：`--base` > `ITSM_BASE_URL` > 配置 > 兜底）。**21 个子命令**：

```bash
BASE=https://itsm.example.com/api/v1     # ← 换成部署地址（含 /api/v1；内部 demo http://172.18.3.238:8089/api/v1）
# A) PAT（推荐，个人中心→安全设置创建）——管道传入，不进 argv/shell 历史：
cat pat.txt | python scripts/itsm_problem.py setup --base $BASE --token-stdin
# B) 或账号换 JWT（15 分钟过期；口令走环境变量或交互式隐藏输入，勿明文写命令行）：
ITSM_USERNAME=you ITSM_PASSWORD='***' python scripts/itsm_problem.py setup --base $BASE --from-env

python scripts/itsm_problem.py config     # 自检：生效地址/凭据来源/连通性/身份（不回显明文）

# 之后所有命令自动读取配置（无需 --base/--token-file）：
python scripts/itsm_problem.py stats
python scripts/itsm_problem.py list --status open
python scripts/itsm_problem.py create --set 'title=数据库连接池耗尽' --set 'description=高峰期 MES 无法连接数据库，需根因分析' --set priority=high
python scripts/itsm_problem.py investigate 5
python scripts/itsm_problem.py root-cause 5 --root-cause '连接池配置过小且未回收泄漏连接'
python scripts/itsm_problem.py solution 5 --set solutionDescription='扩容连接池并修复泄漏'
python scripts/itsm_problem.py update 5 --set status=resolved
python scripts/itsm_problem.py close 5 --resolution '已扩容并修复，观察稳定'
```

Windows PowerShell 等价：`$B = 'https://itsm.example.com/api/v1'; Get-Content pat.txt | python scripts/itsm_problem.py setup --base $B --token-stdin`；token 落盘 `$HOME\.itsm\itsm.token`（自动读取）。

子命令清单：setup / config / login / list / get / create / update / delete / stats / trend / hotspots / investigate / root-cause / solution / close / sla / comments / comment-add / associations / association-add / association-remove。

## 核心流程：问题生命周期

1. `create`（open）→ 2. `investigate`（investigating）→ 3. `root-cause` → 4. `solution` → 5. `update --set status=resolved` → 6. `close --resolution ...`。

## 决策要点（硬规则）

- **状态机**：`open → investigating/identified/resolved`；`resolved → investigating(重开)/closed`；**closed 终态**；违规 409（`当前问题状态不允许此操作`），跳过分析直接 close 必被拒。
- **状态流转走 `PUT /problems/:id` 的 `status` 字段**（无专用 resolve/close 端点）；`investigate`/`close` 是语义化快捷端点（investigate 无 body）。
- 创建必填：`title`(2-200)、`description`(10-20000)、`priority`(low/medium/high/critical)；**更新时 description 同样 ≥10 字符**。
- 关联管理：`relatedType` 仅 `ticket|incident|change`；add 传 `relatedIds[]`，remove 传单个 `relatedId` 且 **DELETE 带 JSON body**。
- 状态时间戳自动维护（resolvedAt/closedAt），无需手动传。
- hotspots/trend/stats 为图表自由结构，脚本直通输出。

## 排障与验证

排障按序查 [references/problem-api.md](./references/problem-api.md) §3；409 先核对当前状态与目标状态是否在转换表内。验证口径：对 demo 环境跑 stats → create → investigate → root-cause → solution → resolved → close 链路并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
