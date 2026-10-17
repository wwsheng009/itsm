---
name: itsm-problem-operations
description: 通过 ITSM REST API 操作问题管理（问题单 CRUD、调查/根因/解决方案、关闭、关联 ticket/incident/change、评论、统计趋势热点、问题 SLA）。Use when the agent needs 创建或跟进问题单、记录根因与解决方案、关联相关工单/事件/变更、查看问题统计趋势热点；covers /api/v1/problems 全族，含 open→investigating→identified→resolved→closed 状态机（409 守卫）。Do NOT use for 事件监控告警域（/incidents，见 README §7）、工单操作（itsm-ticket-operations）。
---

# ITSM 问题管理操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。权限：读 `problem:read` / 写 `problem:write` / 删除 `problem:delete`；行级写权限=创建人/受理人/管理员。

## 脚本工具（scripts/itsm_problem.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`（19 子命令）：

```bash
B=http://172.18.3.238:8089/api/v1
TOKEN="$HOME/.itsm/itsm.token"
python scripts/itsm_problem.py login --base $B --username demo.mfg.admin --password 'YOUR_PASSWORD' --save "$TOKEN"
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" stats
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" list --status open
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" create --set 'title=数据库连接池耗尽' --set 'description=高峰期 MES 无法连接数据库，需根因分析' --set priority=high
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" investigate 5
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" root-cause 5 --root-cause '连接池配置过小且未回收泄漏连接'
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" solution 5 --set solutionDescription='扩容连接池并修复泄漏'
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" update 5 --set status=resolved
python scripts/itsm_problem.py --base $B --token-file "$TOKEN" close 5 --resolution '已扩容并修复，观察稳定'
```

子命令清单：login / list / get / create / update / delete / stats / trend / hotspots / investigate / root-cause / solution / close / sla / comments / comment-add / associations / association-add / association-remove。

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
