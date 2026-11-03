---
name: itsm-sla-operations
description: 通过 ITSM REST API 操作 SLA（定义 CRUD、监控大屏数据、指标/违约记录与处置、绩效维度聚合、合规检查/报告、告警规则与历史、MSP 下发授权/合同/结算报表）。Use when the agent needs 查询或创建 SLA 定义、查看 SLA 监控与违约、处置违约（争议/裁决）、按维度聚合绩效、跑合规报告；covers /api/v1/sla 全族，含 sla:read/write/delete/violation:settle 权限分离与 MSP 专属子面。Do NOT use for 工单 SLA 暂停恢复（itsm-ticket-operations 的 /tickets/:id/sla/*）、部署（itsm-deploy）。
---

# ITSM SLA 操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。权限：读 `sla:read` / 写 `sla:write` / 删除 `sla:delete` / **违约裁决 `sla:violation:settle`**（独立码）。

## 脚本工具（scripts/itsm_sla.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`。**首次配置用 `setup`**（一次性写入部署地址与凭据到 `~/.itsm/config.json` / `~/.itsm/itsm.token`，权限收紧、掩码回显），之后命令自动读取、无需 `--base/--token-file`；**localhost 只是脚本兜底，不是部署默认**（解析优先级：`--base` > `ITSM_BASE_URL` > 配置 > 兜底）。**27 个子命令**：

```bash
BASE=https://itsm.example.com/api/v1     # ← 换成部署地址（含 /api/v1；内部 demo http://172.18.3.238:8089/api/v1）
# A) PAT（推荐，个人中心→安全设置创建）——管道传入，不进 argv/shell 历史：
cat pat.txt | python scripts/itsm_sla.py setup --base $BASE --token-stdin
# B) 或账号换 JWT（15 分钟过期；口令走环境变量或交互式隐藏输入，勿明文写命令行）：
ITSM_USERNAME=you ITSM_PASSWORD='***' python scripts/itsm_sla.py setup --base $BASE --from-env

python scripts/itsm_sla.py config     # 自检：生效地址/凭据来源/连通性/身份（不回显明文）

# 之后所有命令自动读取配置（无需 --base/--token-file）：
python scripts/itsm_sla.py stats
python scripts/itsm_sla.py monitoring --start 2026-10-01T00:00:00Z --end 2026-10-31T23:59:59Z
python scripts/itsm_sla.py violations --resolved false
python scripts/itsm_sla.py performance --dimension priority
python scripts/itsm_sla.py check-compliance 63
python scripts/itsm_sla.py definition-create --set name=紧急SLA --set responseTime=15 --set resolutionTime=120 --set priority=critical
```

Windows PowerShell 等价：`$B = 'https://itsm.example.com/api/v1'; Get-Content pat.txt | python scripts/itsm_sla.py setup --base $B --token-stdin`；token 落盘 `$HOME\.itsm\itsm.token`（自动读取）。

子命令清单：setup / config / login / stats / definitions / definition-get / definition-create / definition-update / definition-delete / provided / alert-rules / alert-rule-get / alert-rule-create / alert-rule-update / alert-rule-delete / metrics / violations / violation-update / violation-settle / monitoring / performance / check-compliance / alert-history / compliance-report ＋ MSP 直通 grants / contracts / settlement。

## 核心流程：违约处置（权限分离）

1. `violations --resolved false` 找未决违约 → 2. 有争议：`violation-update <id> --state disputed --dispute-reason ...` → 3. 归责终态：`violation-settle <id> --decision accepted|waived --reason ...`（需 `sla:violation:settle`）。

## 决策要点（硬规则）

- **monitoring 时间必须 RFC3339 或留空**（历史 "30d"/"now" 伪值已废弃，传了也不生效）。
- **违约处置端点分离**：`disputed` 走 `PUT violations/:id`；`accepted/waived` 走 `PUT violations/:id/settle`——混用 400。
- `check-compliance` 直接 POST 到 `/sla/check-compliance/:ticketId`，返回含 `actual_response_minutes` 的合规结果。
- `performance` 的 `dimension` 仅 `serviceType|priority`；serviceType/priority 参数是统计种群预过滤（非分页后筛选）。
- 定义必填：`name` + `responseTime` + `resolutionTime`（分钟，≥1）；`businessHours/escalationRules/conditions` 为自由 map（服务端不校验 schema）。
- **MSP 子面**（grants/contracts/settlement-report）非 MSP 部署一律 404；合同写入口唯一化（grant 写端点已 410）；结算快照锁定后该周期违约不可变更。
- 列表响应键：metrics=`{metrics,count}`、violations=`{items,total,page,pageSize}`——两套键并存。

## 排障与验证

排障按序查 [references/sla-api.md](./references/sla-api.md) §2。验证口径：对 demo 环境跑 stats → monitoring（窗口）→ violations（过滤）→ performance（双维度）→ check-compliance 链路并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
