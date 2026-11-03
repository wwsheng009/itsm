---
name: itsm-service-request-operations
description: 通过 ITSM REST API 操作服务请求与服务目录（请求 CRUD、多级审批链 approve/reject、开通任务 provision/execute、目录 CRUD/搜索/统计）。Use when the agent needs 提交或审批服务请求、触发资源开通、查询待审批、维护服务目录条目；covers /api/v1/service-requests + /service-catalogs + /provisioning-tasks，含 service_request:read/write/approve 权限分离与多级审批状态机（submitted→manager→it→security→provisioning→delivered）。Do NOT use for 变更审批（itsm-change-operations）、MSP 客户请求（itsm-msp-operations）。
---

# ITSM 服务请求 + 服务目录操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。权限：读 `service_request:read` / 写 `service_request:write` / **审批独立 `service_request:approve`**；目录 `service_catalog:read/write/delete`。

## 脚本工具（scripts/itsm_service_request.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`。**首次配置用 `setup`**（一次性写入部署地址与凭据到 `~/.itsm/config.json` / `~/.itsm/itsm.token`，权限收紧、掩码回显），之后命令自动读取、无需 `--base/--token-file`；**localhost 只是脚本兜底，不是部署默认**（解析优先级：`--base` > `ITSM_BASE_URL` > 配置 > 兜底）。**23 个子命令**：

```bash
BASE=https://itsm.example.com/api/v1     # ← 换成部署地址（含 /api/v1；内部 demo http://172.18.3.238:8089/api/v1）
# A) PAT（推荐，个人中心→安全设置创建）——管道传入，不进 argv/shell 历史：
cat pat.txt | python scripts/itsm_service_request.py setup --base $BASE --token-stdin
# B) 或账号换 JWT（15 分钟过期；口令走环境变量或交互式隐藏输入，勿明文写命令行）：
ITSM_USERNAME=you ITSM_PASSWORD='***' python scripts/itsm_service_request.py setup --base $BASE --from-env

python scripts/itsm_service_request.py config     # 自检：生效地址/凭据来源/连通性/身份（不回显明文）

# 之后所有命令自动读取配置（无需 --base/--token-file）：
python scripts/itsm_service_request.py catalog-list
python scripts/itsm_service_request.py create --set catalogId=1 --set 'reason=申请测试虚拟机' --set dataClassification=internal
python scripts/itsm_service_request.py pending
python scripts/itsm_service_request.py approval 9 --action approve --comment '同意'
python scripts/itsm_service_request.py provision 9
python scripts/itsm_service_request.py provisioning-tasks 9
```

Windows PowerShell 等价：`$B = 'https://itsm.example.com/api/v1'; Get-Content pat.txt | python scripts/itsm_service_request.py setup --base $B --token-stdin`；token 落盘 `$HOME\.itsm\itsm.token`（自动读取）。

子命令清单：setup / config / login / list / pending / get / create / update / delete / status / approvals / approval / provision / provisioning-tasks / task-execute ｜ catalog-list / catalog-get / catalog-search / catalog-stats / catalog-create / catalog-update / catalog-delete / catalog-services。

## 核心流程：目录下单 → 多级审批 → 开通

1. `catalog-list` 选条目 → 2. `create --set catalogId=N --set reason=...`（submitted）→ 3. `pending` 看待审 → 4. `approval --action approve`（服务端按当前态推进到下一级：manager→it→security）→ 5. `provision`（provisioning）→ 6. `provisioning-tasks` 跟踪、`task-execute` 执行 → delivered。

## 决策要点（硬规则）

- **审批链与运营状态是两条正交链**：审批走 `POST /:id/approval {action:approve|reject}`（服务端决定下一级，无需指定目标状态）；运营白名单走 `PUT /:id/status`（submitted/manager_approved/it_approved 仅可 cancelled；security_approved→provisioning；failed→provisioning 重试）。把 approved 当 status 传会被拒。
- 状态常量（domain/servicerequest）：`submitted / manager_approved / it_approved / security_approved / rejected / provisioning / delivered / failed / cancelled`；rejected/delivered/cancelled 终态。
- 创建两形态：**目录下单**（catalogId 必传，title 可省）或**自由请求**（title 必传）；`dataClassification` oneof public/internal/confidential/restricted。
- 列表分页参数是 **`size`**（≤100）——与 tickets 的 pageSize 不同。
- provisioning 执行是独立路径 `/provisioning-tasks/:id/execute`。
- 目录条目：`name/category` 必填；`serviceType` oneof vm/rds/oss/network/storage/security/custom。

## 排障与验证

排障按序查 [references/service-request-api.md](./references/service-request-api.md) §4；审批 403 核对 `service_request:approve`；状态 409 核对运营白名单。验证口径：对 demo 环境跑 catalog-list → create → pending → approval → provision → provisioning-tasks 链路并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
