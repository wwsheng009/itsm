---
name: itsm-change-operations
description: 通过 ITSM REST API 操作变更管理（变更单 CRUD/提交审批/按类型状态机流转 approve-reject-schedule-start-complete-close-rollback-cancel/风险评估/PIR/变更日历/CAB 成员/CMDB 影响面）。Use when the agent needs 创建变更单、提交或审批变更、推进变更状态、评估变更风险、实施后评审 PIR、查询变更日历或影响面；covers /api/v1/changes + /cab，含 change:read/write/approve/rollback 权限分离与 BPMN 桥接语义。Do NOT use for 工单事件（itsm-ticket-operations）、部署操作（itsm-deploy）、CMDB 查询（itsm-cmdb-operations）。
---

# ITSM 变更管理操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。**权限四分离**（H-15）：读 `change:read` / 写 `change:write` / **审批 `change:approve`** / **回滚 `change:rollback`**——write 不能替代审批与回滚。

## 脚本工具（scripts/itsm_change.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`。**首次配置用 `setup`**（一次性写入部署地址与凭据到 `~/.itsm/config.json` / `~/.itsm/itsm.token`，权限收紧、掩码回显），之后命令自动读取、无需 `--base/--token-file`；**localhost 只是脚本兜底，不是部署默认**（解析优先级：`--base` > `ITSM_BASE_URL` > 配置 > 兜底）。**35 个子命令**：

```bash
BASE=https://itsm.example.com/api/v1     # ← 换成部署地址（含 /api/v1；内部 demo http://172.18.3.238:8089/api/v1）
# A) PAT（推荐，个人中心→安全设置创建）——管道传入，不进 argv/shell 历史：
cat pat.txt | python scripts/itsm_change.py setup --base $BASE --token-stdin
# B) 或账号换 JWT（15 分钟过期；口令走环境变量或交互式隐藏输入，勿明文写命令行）：
ITSM_USERNAME=you ITSM_PASSWORD='***' python scripts/itsm_change.py setup --base $BASE --from-env

python scripts/itsm_change.py config     # 自检：生效地址/凭据来源/连通性/身份（不回显明文）

# 之后所有命令自动读取配置（无需 --base/--token-file）：
python scripts/itsm_change.py stats
python scripts/itsm_change.py list --status pending
python scripts/itsm_change.py create --set title='升级 MES 数据库补丁' --set type=normal --set priority=high --set riskLevel=medium --set plannedStartDate=2026-10-20T02:00:00Z
python scripts/itsm_change.py submit 7 --approver-ids 3,4
python scripts/itsm_change.py approve 7 --comment '同意，窗口内实施'
python scripts/itsm_change.py schedule 7 --comment '排期 10-20 凌晨'
python scripts/itsm_change.py start 7
python scripts/itsm_change.py complete 7
python scripts/itsm_change.py pir-create 7 --set overallResult=successful --set objectivesAchieved=true
```

Windows PowerShell 等价：`$B = 'https://itsm.example.com/api/v1'; Get-Content pat.txt | python scripts/itsm_change.py setup --base $B --token-stdin`；token 落盘 `$HOME\.itsm\itsm.token`（自动读取）。

子命令清单：setup / config / login / list / get / stats / create / update / delete / submit / assign / **approve / reject / schedule / start / complete / close / rollback / cancel**（8 个流转）/ approvals / approval-submit / approval-summary / risk-get / risk-update / cmdb-impact / calendar / pirs / pir-get / pir-create / pir-update / pir-delete / cab-members / cab-add / cab-update / cab-remove。

## 核心流程：一张变更单的生命周期

1. **create**（draft）→ 2. **submit**（可指定 approverIds）→ 3. **approve / reject**（审批人；非审批人 403，已被他人处理 409）→ 4. **schedule** → 5. **start**（in_progress）→ 6. **complete / rollback / cancel**（failed 可重回 scheduled）→ 7. **close**（completed/rolled_back 归档）→ 8. **pir-create**（实施后评审）。

## 决策要点（硬规则）

- **状态机按类型分表**（单一事实源 `service.IsValidChangeStatusTransition`）：normal 严格 ITIL（approved→只能 scheduled）；standard 预授权可跳步（draft→approved/scheduled/in_progress）；emergency 可跳过排期（approved→in_progress）。非法转换 409。
- `pending` ≡ `submitted`（历史别名，服务端归一）。
- **流转目标状态由 URL 末段决定**（`POST /:id/approve` 等 8 端点同 handler）；body 仅 `{comment?}` 或 `{reason?}`。
- **审批双校验**：路由权限 `change:approve` + 链上未决审批人行级校验；approve/reject 会桥接 BPMN（change_normal_flow / change_emergency_flow），**桥接失败阻断业务流转（fail-closed）**。
- 行级写权限：非审批/回滚动作仅创建人/被指派人/管理员。
- 列表响应键为 `changes`（非 items）；`affectedCis/relatedTickets` 是**字符串数组**（填编号）。
- 生命周期：风险评估（risk-update）应在 submit 前完成；PIR 在 close 后做。

## 排障与验证

排障按序查 [references/change-api.md](./references/change-api.md) §5；403 先核对权限码（approve/rollback 分离）再核对是否链上审批人；409 看是非法转换还是并发已决。验证口径：对 demo 环境跑 create→submit→approve→schedule→start→complete→close→pir 全链并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
