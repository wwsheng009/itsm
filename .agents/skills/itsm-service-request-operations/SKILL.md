---
name: itsm-service-request-operations
description: 通过 ITSM REST API 操作服务请求与服务目录（请求 CRUD、多级审批链 approve/reject、开通任务 provision/execute、目录 CRUD/搜索/统计）。Use when the agent needs 提交或审批服务请求、触发资源开通、查询待审批、维护服务目录条目；covers /api/v1/service-requests + /service-catalogs + /provisioning-tasks，含 service_request:read/write/approve 权限分离与多级审批状态机（submitted→manager→it→security→provisioning→delivered）。Do NOT use for 变更审批（itsm-change-operations）、MSP 客户请求（itsm-msp-operations）。
---

# ITSM 服务请求 + 服务目录操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证同全站（PAT/JWT Bearer）。权限：读 `service_request:read` / 写 `service_request:write` / **审批独立 `service_request:approve`**；目录 `service_catalog:read/write/delete`。

## 脚本工具（scripts/itsm_service_request.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`（21 子命令）：

```bash
B=http://172.18.3.238:8089/api/v1
TOKEN="$HOME/.itsm/itsm.token"
python scripts/itsm_service_request.py login --base $B --username demo.mfg.admin --password 'YOUR_PASSWORD' --save "$TOKEN"
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" catalog-list
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" create --set catalogId=1 --set 'reason=申请测试虚拟机' --set dataClassification=internal
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" pending
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" approval 9 --action approve --comment '同意'
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" provision 9
python scripts/itsm_service_request.py --base $B --token-file "$TOKEN" provisioning-tasks 9
```

子命令清单：login / list / pending / get / create / update / delete / status / approvals / approval / provision / provisioning-tasks / task-execute ｜ catalog-list / catalog-get / catalog-search / catalog-stats / catalog-create / catalog-update / catalog-delete / catalog-services。

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
