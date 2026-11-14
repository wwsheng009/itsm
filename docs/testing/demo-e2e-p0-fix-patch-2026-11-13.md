# ITSM 演示环境 —— P0 缺陷修复补丁说明（2026-11-13）

> **用途**：本轮 E2E 测试发现 2 个 P0 阻断缺陷。因 **238 服务器 SSH 认证失败**（`<REDACTED_PASSWORD>` 被拒），
> 未能完成「改配置 → 构建 → 部署 → 回归」闭环。本文件提供**可直接执行**的修复步骤，
> 待取得有效凭据后按此施工。
>
> **配套**：[测试计划](./demo-multi-tenant-e2e-business-test-plan-2026-11-13.md) ·
> [缺陷台账](./demo-e2e-defect-tracking-2026-11-13.md) ·
> [执行总结](./demo-e2e-test-execution-report-2026-11-13.md)

---

## 补丁 1 · D1（P0）：让 AI 链路指向千帆

### 1.1 性质

**纯配置修复，无需改代码。** 已确认系统 AI 代码逻辑正常（变更页「AI 影响分析」可用），
仅外部 LLM 网关地址错误（默认落到 `api.openai.com`，132 次调用全部 `timeout`）。

### 1.2 施工步骤

**Step 1 — 确认当前实际配置（只读）**

```bash
ssh root@172.18.3.238
cd /opt/itsm            # 或实际部署目录，用 `find / -name 'docker-compose.demo.yml'` 定位
grep -nE 'LLM_|OPENAI_|AI_' .env.demo
```

预期当前**缺少** `LLM_ENDPOINT` / `LLM_API_KEY`，或值为空/指向 openai。

**Step 2 — 备份**

```bash
cp .env.demo .env.demo.bak.$(date +%Y%m%d%H%M%S)
```

**Step 3 — 注入千帆配置**

在 `.env.demo` 中追加/覆盖（注意：若已有同名键，需替换而非追加，避免后者不生效）：

```dotenv
# ---- 千帆（百度智能云）LLM 网关 ----
LLM_PROVIDER=openai
LLM_MODEL=ernie-4.5-turbo-128k
LLM_API_KEY=<REDACTED_LLM_API_KEY>
LLM_ENDPOINT=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions
OPENAI_BASE_URL=https://qianfan.baidubce.com/v2/tokenplan/personal
OPENAI_API_KEY=<REDACTED_LLM_API_KEY>
LLM_TIMEOUT=60
```

> **`LLM_MODEL` 取值需与千帆侧实际开通的模型一致。**
> 若 `ernie-4.5-turbo-128k` 不可用，请先在千帆控制台确认模型名，或改用 `ernie-4.0-8k` 等。
> 端点 `/v2/tokenplan/personal/chat/completions` 为 OpenAI Chat Completions 兼容协议，
> 因此 `LLM_PROVIDER=openai` 是正确的——这里指的是**协议类型**，不是厂商。

**Step 4 — 重建受影响容器**

`ai-service` 与本配置相关；`backend` 若也读 `LLM_*` 亦需重建。按 `docker-compose.demo.yml`
的 env 引用关系，建议一并重建避免不一致：

```bash
cd /opt/itsm
docker compose -f docker-compose.demo.yml --env-file .env.demo up -d --force-recreate ai-service backend
docker compose -f docker-compose.demo.yml --env-file .env.demo ps
```

**Step 5 — 等健康**

```bash
until [ "$(docker inspect -f '{{.State.Health.Status}}' itsm-demo-ai-service 2>/dev/null)" = "healthy" ]; do sleep 3; done
echo ai-service healthy
```

（容器名以 `docker ps` 实际输出为准，可能带 `itsm-demo-` 前缀。）

### 1.3 验收标准（三条全过才算修好）

| # | 验证 | 命令 / 操作 | 期望 |
|---|---|---|---|
| 1 | provider 已切换 | `GET /api/v1/ai/llm-calls?pageSize=1`（带登录 Cookie） | `providerSource` **不再是 `static`**；`success=true` |
| 2 | 分诊可用 | `POST /api/v1/ai/triage` body `{"title":"打印机故障","description":"车间打印机无法连接"}` | **HTTP 200**（当前为 403），返回含分类 + 置信度 |
| 3 | 对话可用 | 浏览器 `/ai/chat` 点「汇总我最近处理过的工单」 | 返回**自然语言摘要**，不是红色 `LLM stream failed` 气泡 |

### 1.4 回滚

```bash
cp .env.demo.bak.<timestamp> .env.demo
docker compose -f docker-compose.demo.yml --env-file .env.demo up -d --force-recreate ai-service backend
```

---

## 补丁 2 · D19（P0）：修复服务请求审批链 L2 不可见

### 2.1 性质

**需改代码 + 重建 backend。** 纯 SQL 条件遗漏，改动量小但需回归。

### 2.2 根因摘要

`itsm-backend/handlers/service_request/repository_impl.go` 的 `List` 方法中，
行级数据权限只放行「我提交的」与「我处理的」：

```go
if !datascope.IsTenantWide(filters.DataScope) {
    query = query.Where(servicerequest.Or(
        servicerequest.RequesterIDEQ(filters.CurrentUserID),
        servicerequest.ProcessorIDEQ(filters.CurrentUserID),
    ))
}
```

**遗漏「待我审批」。** 导致 L2 审批人（`it_admin`）在自己的待办里看不到待审单据。

### 2.3 修改方案

**方案 A（最小改动，推荐先做）**：并入「存在待本人审批的节点」条件。

前置确认（施工前必须做，决定用哪个字段 join）：

```bash
# 确认审批表的关联字段与状态枚举
grep -nE 'approver|status|level|step' itsm-backend/ent/schema/servicerequestapproval.go
grep -rnE 'service_request_approvals' itsm-backend/migrations/*.sql | head -20
```

按实际 schema 选择下列之一：

```go
// 变体 1：审批表有 approver_membership_id / approver_id 字段
pendingMine := func(q *ent.ServiceRequestQuery) {
    q.Where(servicerequest.HasApprovalsWith(
        servicerequestapproval.StatusEQ("pending"),
        servicerequestapproval.ApproverID(filters.CurrentUserID),
    ))
}

// 变体 2：审批表按 level + step 标识节点（无具体审批人，靠角色匹配）
// 则需先由当前用户角色推导可审批的 step 集合，再用 HasApprovalsWith 匹配
//   step IN (用户可审批的 step) AND level = current_level AND status = 'pending'

query = query.Where(servicerequest.Or(
    servicerequest.RequesterIDEQ(filters.CurrentUserID),
    servicerequest.ProcessorIDEQ(filters.CurrentUserID),
    pendingMine, // ← 新增
))
```

**务必保留 `CurrentUserID <= 0` 的兜底**（返回空集，避免越权全量）：

```go
if filters.CurrentUserID <= 0 {
    query = query.Where(servicerequest.IDEQ(-1))
} else {
    // 上述 Or(...) 逻辑
}
```

**方案 B（中期重构，更正确）**：拆出独立方法 `ListPendingApprovals`，
以 `service_request_approvals` 为**驱动表**查询（`WHERE approver = me AND status = 'pending'`），
再 join 主表。审批待办本质是"我的任务"，不应套用申请人的数据范围语义。

### 2.4 施工步骤

```bash
# 1. 改代码
cd /path/to/repo && git checkout -b fix/sr-pending-approval-visibility

# 2. 本地验证（必须）
cd itsm-backend
go build ./...
go test ./handlers/service_request/... -run 'Approval|Membership' -v

# 3. 构建并部署 demo 栈
cd ..
./scripts/deploy-demo.sh deploy --version 1.8.0-dev.$(git rev-parse --short HEAD)
```

> 使用仓库自带 `scripts/deploy-demo.sh`（它会自动备份 `.env.demo` 与 `itsm_demo` 库、
> 修 uploads ACL、跑迁移后再切服务，比手工 `docker compose up` 安全）。

### 2.5 验收标准

| # | 验证 | 期望 |
|---|---|---|
| 1 | `demo.mfg.itadmin` 登录 → `/service-requests` | 「待审批」显示 **≥1**（`SR-202610-000001`） |
| 2 | `GET /api/v1/service-requests`（itadmin 身份） | `total ≥ 1`，含 `SR-202610-000001` |
| 3 | IT 主管审批通过 | 状态 → `it_approved`，`currentLevel` → 3，L3 `pending` |
| 4 | `demo.mfg.secadmin` 登录 | 能在待办看到该单，完成 L3 审批 → 终态 |
| 5 | **回归：越权未被打穿** | `demo.mfg.user` 的 `/service-requests` 仍**只**见自己提交的单；跨租户仍 404 |

> **⚠️ 第 5 条是关键回归项**：本次改动放宽了可见范围，必须确认没有把
> 「他人单据」或「跨租户单据」一并放进来。

### 2.6 回滚

```bash
./scripts/deploy-demo.sh deploy --skip-build --version <上一个已知良好版本>
```

---

## 补丁 3 · D2（P1）：SLA 详情不展示

### 3.1 根因

前后端字段名不一致，且后端**未返回 `slaName`**：

| 语义 | 后端 | 前端读取 |
|---|---|---|
| SLA 名称 | ❌ 无 `slaName` | `slaInfo.slaName` |
| 响应截止 | `slaResponseDeadline` | `responseDeadline` |
| 解决截止 | `slaResolutionDeadline` | `resolutionDeadline` |
| 违约标志 | `resolutionDeadlineBreached` | `isBreached` |

因 `slaName` 缺失，`TicketDetail.tsx:1232` 的
`slaInfo.slaName ? <正常渲染> : <Text>{t('ticketDetail.slaNotConfigured')}</Text>`
永远走 else 分支，渲染出「未配置 SLA 定义」。

### 3.2 修改（建议两侧都改）

**后端**：组装响应时补名称并输出兼容字段

```go
// 在 SLA 响应组装处（handlers/ticket/service.go 附近）
if def != nil {
    resp.SLAName = def.Name   // 新增：按 slaDefinitionId 查定义名
}
// 兼容字段（camelCase，供前端直接读取）
resp.ResponseDeadline   = resp.SlaResponseDeadline
resp.ResolutionDeadline = resp.SlaResolutionDeadline
resp.IsBreached         = resp.ResolutionDeadlineBreached
```

**前端**：`TicketApi.getTicketSLA` 后做归一化，防止后端字段再变

```ts
const normalizeSLA = (raw: any) => ({
  slaName:            raw.slaName ?? (raw.slaDefinitionId ? `SLA 定义 #${raw.slaDefinitionId}` : ''),
  responseDeadline:   raw.responseDeadline   ?? raw.slaResponseDeadline,
  resolutionDeadline: raw.resolutionDeadline ?? raw.slaResolutionDeadline,
  isBreached:         raw.isBreached         ?? raw.resolutionDeadlineBreached ?? false,
  status:             raw.slaStatus ?? raw.status,
})
```

### 3.3 验收

打开工单 `#629` 与种子单 `#3`，SLA 卡片应显示：SLA 名称、响应截止、解决截止、倒计时；
`#3` 因 `resolutionDeadlineBreached=true` 应显示**违约**样式（红色）。

---

## 修复优先级与预估工时

| 顺序 | 补丁 | 级别 | 改动面 | 预估 | 风险 |
|---|---|---|---|---|---|
| ① | 补丁 1（千帆配置） | P0 | 仅 `.env.demo` + 重建 2 容器 | **15 分钟** | 极低（可秒级回滚） |
| ② | 补丁 2（审批可见性） | P0 | 1 个 Go 文件 + 回归 | 2–3 小时（含回归） | 中（需确认越权未打穿） |
| ③ | 补丁 3（SLA 契约） | P1 | 后端 1 处 + 前端 1 处 | 2 小时 | 低 |

---

## 施工前置条件（当前缺失）

- [ ] **238 服务器有效 root 凭据**（当前 `<REDACTED_PASSWORD>` 被拒）
- [ ] 确认 `.env.demo` 的实际路径与 compose 项目名
- [ ] 确认千帆侧可用模型名（`LLM_MODEL` 取值）
- [ ] 确认 `service_request_approvals` 表的实际字段（决定补丁 2 用哪个变体）
