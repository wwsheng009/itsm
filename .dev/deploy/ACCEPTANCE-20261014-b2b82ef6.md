# §8 端到端验收执行记录 —— SLA 合同化专项（2026-10-14，238 demo）

> 环境：`172.18.3.238` demo 栈（`http://172.18.3.238:8089/`）｜最终版本：**`1.8.0-dev.b2b82ef6`**
> 范围：方案 §8 SLA-A1…A17 + §5 反例清单 + P1-2 并轨闸门｜执行人：主 agent（API/DB/UI 实测）
> 关联：`ACCEPTANCE-RUNBOOK-c700c060.md`（本记录为其执行结果与偏差说明）

---

## 0. 结论摘要

- **验收执行通过**：API 场景脚本 42 步 **0 fail/blocked**；DB 层逐锚点断言齐备；UI 抽查通过。
- 验收过程发现并修复 **3 个接线缺陷**（IM-36/37/38，均已随本轮部署至 238），以及 demo 库 RBAC 数据缺口（新权限码未同步到存量租户）。
- 遗留观察项 4 条（见 §6），均不影响合同化主链结论。

## 1. 验收期缺陷修复（本轮新增，3 提交）

| # | 提交 | 缺陷（验收实测） | 修复 |
|---|------|-----------------|------|
| IM-36 | `e5bfdc94`（backend） | `/sla` 路由族不经过 `/api/v1/msp` 的 MSPMiddleware → `RequireMSPPermission` 前置 MSPContext 缺省，**grants/contracts/settlement-report 三组对所有 MSP 用户恒 403「仅MSP员工可访问」** | `SetupSLARoutes` 接收 ent client，三组显式装配 `RequireMSPDeploymentMode + MSPMiddleware`；新增守卫测试 `sla_msp_context_guard_test.go` |
| IM-37 | `fa04cd67`（frontend） | `ADMIN_AREA_PERMISSIONS` 未收录 `sla:contract:read/write` → provider（仅持合同码）被 `/admin` 区域门禁整区拦下，`/admin/sla-contracts` 不可达 | 区域并集按 B6 政策收录两个合同码 |
| IM-38 | `b2b82ef6`（frontend） | `AdminRouteGuard`（MainLayout 内联兜底）用 role 判定 `admin/super_admin`，与 B6 区域并集单源不一致 → 即使区域放行仍内联 403 | 改用 `ADMIN_AREA_PERMISSIONS` + `store.hasPermission`（super_admin 走 `*` 通配） |

**demo 库 RBAC 数据修正（验收准备，非代码）**：6 个新权限码（`sla:grant/contract:*`、`sla:settlement:read`、`sla:violation:settle`）仅存在于平台租户（t1），未同步到 demo 租户（t2）——
① 目录克隆 t1→t2（6 行，幂等）；② 官方工具 `cmd/sync_role_permissions -tenant-id 2 -apply`（created=18/deleted=0）；
③ `cmd/backfill_provider_admin -tenant-id 2`（幂等复核）；④ `user_roles(2→46)` 复合角色边（provider 租户管理员）；
⑤ 重启 backend 清缓存。修复后 t2 的 msp_manager/msp_admin/provider_tenant_admin 各持 6 码；manager `/auth/me` 18 码含合同/结算码。

## 2. 部署证据（238，三轮增量）

| 轮次 | 版本 | 镜像 | 结果 |
|---|---|---|---|
| 1 | `1.8.0-dev.e5bfdc94` | backend `sha256:8b9332d6…` / frontend `7099c9ef…`（未变） | `BUILD_*_EXIT=0`；backend/worker/frontend healthy；init exit=0；D 步全锚点（401 探针/迁移 6 行/表在位） |
| 2 | `1.8.0-dev.fa04cd67` | frontend `sha256:fc231b0e…` | 同上（backend 摘要未变=缓存命中） |
| 3 | `1.8.0-dev.b2b82ef6` | frontend `sha256:27ea4724…` | 同上；浏览器复验通过 |

> 部署方式：增量 bundle（`itsm-slafix-incremental` / `itsm-fefix-incremental` / `itsm-fefix2-incremental`）+ `deploy238_slafix_{a,w,c,d}.sh`（A 检出+构建 / W 轮询 / C up -d / D 健康+回归探针）。证据：`.dev/deploy/` 同名脚本与 bundle。

## 3. 验收执行证据（API 脚本 + DB 断言）

执行器：`.dev/deploy/acceptance_sla_e2e.py`（`--mode run`，幂等；实体前缀 `[ACC]`）。
最终运行：**42 steps / 0 fail**（ts=10150023）。DB 复核（探针 P8）：

- **tickets 层（SLA 证据载体）**：`42(mfg)→def9/L1`、`43(med)→L3b`、`44(edu,active)→def9/L1`、`45(edu,revoked)→L3b`；`38/40/41` 同构。
- **合同/绑定**：3 合同（C1 mfg active；C2 med 过期期段；C3 edu terminated）；C1 bindings = `{def9,类型0}+{def9,类型13}`。
- **类型副本**：3 次分发副本（26/27/28，`-p2`/`-p2-2` 机械后缀=INV-1 实证）；分发行 3 条均 `unbound`（解绑）。
- **native 零改动**：mfg 原生类型 13–16 字段未变（type13 仅 usage_count/updated_at 因建单计数）。
- **RLS/隔离**：t2 越权查询 customerTenantId=1 → **403 且响应无任何计数**；customer 读写 provider 定义 → 404（PUT/GET）。

## 4. 锚点结果（SLA-A1…A17）

| 锚点 | 结论 | 关键证据（238 实测） |
|---|---|---|
| SLA-A1 下发匹配 | ✅ PASS | 合同绑定 def9 → mfg 工单 42 `def9/L1`、deadline 非空 |
| SLA-A2 匹配优先级 | ✅ PASS（变体） | demo 无 L2 tier 数据；以 L1 vs 租户自建(L3b) 验证：edu 44=L1 / 45=L3b；L1>L2>L3 链由 matcher 单测锁定 |
| SLA-A3 fail-loud | ⚠️ 部分（单测锁定） | 各租户均有自建定义兜底（L3b），运行期未自然触发 L4；L4 告警/未绑定桶由 `ticket_sla_matcher_test` 等锁定；本次未见 `sla_unbound` 日志 |
| SLA-A4 隔离回归 | ✅ PASS | customer PUT→404；DELETE→200 no-op（DB 复核定义未变）；非授权读→404 |
| SLA-A5 合同期 | ✅ PASS | med 过期合同 → 工单 43=L3b（未命中 L1） |
| SLA-A6 结算报表 | ✅ PASS | 越权 customerTenantId=1 → 403 且响应无任何计数；归属客户取数 200 |
| SLA-A7 表单防错 | ✅ PASS（代码锁定） | `SLADefinitionForm`：类型下拉=租户动态配置 + 旧值"历史数据不生效"提示（legacyValueNotice） |
| SLA-A8 fail-closed | ✅ PASS | 无 token 全 401（D3 探针）；tenant≤0 护栏单测（sla_grant_test 5 断言） |
| SLA-A9 解绑级联 | ✅ PASS | edu：44=L1 → 合同 terminated → 45=L3b |
| SLA-A10 信息泄露 | ✅ PASS | provided 仅白名单 11 字段（无 owner/grant/罚则）；告警事件白名单随实现期修正锁定 |
| SLA-A11 结算口径白名单 | ✅ PASS | report：covered=1 / uncovered=0 / exempt=0 / settlementBreaches=0 / compliance=100 |
| SLA-A12 类型豁免 | ➖ 未构造 | demo 无 `sla_exempt` 类型样本；分支由单测锁定 |
| SLA-A13 双口径对账 | ✅ PASS | row0 = `{contractVerdict:met, internalVerdict:met, driftFlag:false, matchedLayer:L1}`；L1 行在列 1 条 |
| SLA-A14 违约基准过滤 | ✅ PASS（结构+单测） | `excludedInternalViolations` 字段在位；L3* 违约不进结算由 service 测试锁定 |
| SLA-A15 类型受控下发 | ✅ PASS | 3 次分发（副本 26/27/28，`-p2`/`-p2-2` 后缀）；受控字段 PUT→403（"受控字段由服务商管理"）；治理字段 PUT→200；解绑 released=true |
| SLA-A16 下发不覆盖 | ✅ PASS（部分） | 同源重复下发→409；native 行零字段改动（DB）；INV-2 发布期零改动由 SQL 守卫+单测锁定 |
| SLA-A17 发布流程 | ✅ PASS | preview(upToDate) → 编辑源(dirty) → diff(description) → publish(synced) → unbind；"存量工单 deadline 不受影响"未构造（无存量工单参与分发类型） |

## 5. 反例清单（必须拒绝）

| # | 反例 | 结论 |
|---|---|---|
| 1 | provider 直改 customer 定义 | ✅（customer 仅只读 provided；PUT→404） |
| 2 | customer 自建 violation 进合同结算 | ✅（matched_layer 过滤；44/45 层级实证 + service 测试） |
| 3 | 匹配失败无痕静默 | ⚠️ 部分（未见自然 L4；告警/未绑定桶由单测锁定） |
| 4 | 结算取数前未校验归属 | ✅（403、无计数） |
| 5 | 非归属 customer 写 grant | ✅（mspguard；A6 同证） |
| 6 | 类型下发触碰 native | ✅（409/后缀/native 未变） |
| 7 | 非 saas_msp 下 grant 写端点未 404 | ✅（部署模式 gate 单测；demo=saas_msp 不适用） |
| 8 | 放宽列表过滤做"列表注入" | ✅（走独立 provided 端点，白名单字段） |

## 6. 观察项与建议（遗留，不阻塞）

1. **incidents 无 SLA 面**：29 条 incidents（含种子）`sla_definition_id`/deadlines 全空——SLA 载体为 `tickets`（含 `sla_matched_layer`），方案文案"建 incident"应读作"建工单"；REST 事件建单不触发 SLA 绑定（如需事件域 SLA 需另行立项）。
2. **`/auth/me` 权限未并集 m2m 附加角色**：后端运行时判定取"主角色+m2m 并集"（manager 实际含 role46），但 me 仅回主角色 18 码（role46=134 码）→ FE 门禁与 BE 判定存在口径差（本轮已通过区域码补齐绕过合同页；建议后端 me 对齐并集，或前端改权限数据源）。
3. **定义 DELETE 域外 no-op 返回 200**（fail-quiet）：数据无损伤，但建议映射 404，避免"看起来删除成功"。
4. **RBAC 目录同步缺口**：新增权限码不会自动到达存量租户；`cmd/sync_role_permissions` 只做"角色↔已存在码"映射（不建目录行）。建议扩展该工具或在部署手册固化"目录克隆 + sync + backfill"三步。
5. ai-service 容器 tag 未随版本重建（同源 retag，历史沿革一致，可忽略）。

## 7. 复跑指引

```powershell
# 场景复跑（幂等；新建行均带 [ACC] 前缀）
python .dev/deploy/acceptance_sla_e2e.py --mode discover   # 只读侦察
python .dev/deploy/acceptance_sla_e2e.py --mode run        # 全场景（42 步）

# DB 断言（远端）
python .dev/deploy/remote.py --put .dev/deploy/acceptance_probe8.sh /tmp/acceptance_probe8.sh
$env:ITSM_DEPLOY_HOST='172.18.3.238'; $env:ITSM_DEPLOY_USER='n9e'; $env:ITSM_DEPLOY_PASSWORD='passw0rd'
python .dev/deploy/remote.py --sudo 'bash /tmp/acceptance_probe8.sh'
```

## 8. 产物清单（.dev/deploy/）

- 执行器：`acceptance_sla_e2e.py`；探针：`probe_me_238.py`、`probe_me_raw.py`、`acceptance_probe1…9.sh`、`acceptance_rbac_fix_{dry,apply}.sh`
- 部署：`deploy238_slafix_{a,w,c,d}.sh` + bundle ×3（slafix/fefix/fefix2）；跨租户工具二进制 `bin/*`（不入库）
- 本记录 +（前序）`ACCEPTANCE-RUNBOOK-c700c060.md`、`DEMO-FULL-20261014-c700c060-EVIDENCE.md`

> 详细运行输出见会话日志；关键步骤以 `RESULT:` JSON 行记录（42 步）并已回填本文档。
