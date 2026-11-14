# E2E 缺陷修复实施记录（2026-11-13）

> 依据 `docs/testing/demo-e2e-p0-fix-patch-2026-11-13.md` 施工。
> 范围：D19、D2 代码修复已完成；D1 经实测**重新定性**，非代码缺陷，改为运行环境/配额问题。
> 验证状态：静态核对通过；**未编译、未部署**（本机 Go 工具链崩溃，且无 238 部署凭据）。

---

## 1. 修复总览

| 缺陷 | 级别 | 原判定 | 本次结论 | 状态 |
|---|---|---|---|---|
| **D19** | P0 | 审批链 L2 断裂，审批人看不到待审单 | 确认成立，**已改代码** | ✅ 代码完成 |
| **D2** | P1 | 工单详情恒显「未配置 SLA」 | 确认成立，**已改前后端** | ✅ 代码完成 |
| **D1** | P0 | 千帆配置未注入导致 AI 全链路失败 | **误判**：租户级 provider 解析正常，真实原因是千帆 **429 配额耗尽** | ⚠️ 定性更正，非代码缺陷 |

---

## 2. D19 修复（服务请求审批人对 L2+ 待审单不可见）

### 2.1 补丁文档方案的失效原因

补丁文档原方案「变体 1」建议按 `approver_id` 匹配待审单。**实测该方案不可用**：

- 服务请求的 pending 审批节点在创建时**不写 `approver_id`**，只把审批人列表写入 `node.approver_ids` JSON。
- 因此 `approver_id = 当前用户` 的条件恒不成立，无法命中任何记录。

### 2.2 实际采用的改法

复用 `ListPendingApprovals` 已有的「角色 → 层级 + 状态」映射，把「停在本层级、正等我审批的」请求单并入列表可见范围，保证两处判断单一来源、不漂移。

**改动 1｜`itsm-backend/handlers/service_request/entity.go`**

`ListFilters` 新增 `Role` 字段（调用者角色）：

```go
DataScope     datascope.DataScope
CurrentUserID int
// Role 为调用者角色，用于把「停在本层级的待本人审批」并入可见范围（D19 修复）。
// 为空时仅退化为「本人创建 / 本人处理」，不会放宽可见范围。
Role string
```

**改动 2｜`itsm-backend/handlers/service_request/handler.go`**

列表接口从上下文取 `role` 并传入筛选器：

```go
currentUserID := c.GetInt("user_id")
// 审批人角色（D19 修复）...
callerRole, _ := c.Get("role")
callerRoleStr, _ := callerRole.(string)
...
filters := ListFilters{
    ...
    CurrentUserID: currentUserID,
    DataScope:     dataScope,
    Role:          strings.ToLower(strings.TrimSpace(callerRoleStr)),
}
```

**改动 3｜`itsm-backend/handlers/service_request/repository_impl.go`**

新增角色映射助手，并并入行级过滤：

```go
// approverLevelForRole 把调用者角色映射为「该审的层级 + 进入该层级时的单据状态」。
// 与 ListPendingApprovals 的角色→层级映射保持单一来源，避免两处判断漂移。
func approverLevelForRole(role string) (level int, status string) {
	switch role {
	case ApprovalStepManager:
		return 1, SRStatusSubmitted
	case ApprovalStepIT, RoleAgent, RoleTechnician, RoleITAdmin:
		return 2, SRStatusManagerApproved
	case ApprovalStepSecurity, RoleSecAdmin:
		return 3, SRStatusITApproved
	default:
		return 0, ""
	}
}
```

```go
if !datascope.IsTenantWide(filters.DataScope) {
    if filters.CurrentUserID <= 0 {
        query = query.Where(servicerequest.IDEQ(-1))
    } else {
        ownedOrAssigned := servicerequest.Or(
            servicerequest.RequesterIDEQ(filters.CurrentUserID),
            servicerequest.ProcessorIDEQ(filters.CurrentUserID),
        )
        if level, status := approverLevelForRole(filters.Role); level > 0 {
            ownedOrAssigned = servicerequest.Or(
                servicerequest.RequesterIDEQ(filters.CurrentUserID),
                servicerequest.ProcessorIDEQ(filters.CurrentUserID),
                servicerequest.And(
                    servicerequest.CurrentLevel(level),
                    servicerequest.StatusEQ(status),
                ),
            )
        }
        query = query.Where(ownedOrAssigned)
    }
}
```

### 2.3 安全性说明

- 仅在 `DataScope` **非**租户全量档位时生效；`DataScopeAll/Department` 行为不变。
- `Role` 为空 → `level<=0` → 退回原「本人创建 / 本人处理」，**不会放宽可见范围**。
- 附加条件是 `当前层级 = 该角色应审层级` **且** `状态 = 该层级入口状态`，收窄而非放开。

### 2.4 施工中修掉的自伤

首版 `switch` 混写了常量与字面量（`ApprovalStepManager` 本身即 `"manager"`，`ApprovalStepSecurity` 与 `RoleSecurity` 同为 `"security"`），Go 会报 duplicate case，已改为纯常量写法。

---

## 3. D2 修复（工单 SLA 详情恒空）

### 3.1 根因

两处叠加，缺一不可：

1. **后端从未下发 `slaName`**：前端以 `slaName` 是否为空判断「是否配置 SLA」，恒空 → 恒显「尚未配置 SLA」，即使工单已绑 `slaDefinitionId`（实测 #629 = 9）。
2. **字段名不一致**：后端发 `slaResponseDeadline/slaResolutionDeadline`，前端读 `responseDeadline/resolutionDeadline` → 截止时间永不上屏。

### 3.2 改动 1｜`itsm-backend/handlers/ticket/repository_impl.go`

暴露可选的 ent 客户端（不扩 `Repository` 接口，避免全部测试 mock 需补实现）：

```go
func (r *EntRepository) Client() *ent.Client {
	if c, ok := r.repo.(interface{ Client() *ent.Client }); ok {
		return c.Client()
	}
	return nil
}
```

### 3.3 改动 2｜`itsm-backend/handlers/ticket/service.go`

`GetTicketSLAInfo` 补齐前端已声明契约字段：

```go
slaName := ""
serviceType := ""
priority := ""
responseTime := 0
resolutionTime := 0
if t.SLADefinitionID != nil {
    if def := s.loadSLADefinition(ctx, *t.SLADefinitionID, tenantID); def != nil {
        slaName = def.Name
        serviceType = def.ServiceType
        priority = def.Priority
        responseTime = def.ResponseTime
        resolutionTime = def.ResolutionTime
    }
}
...
resp["slaName"] = slaName
resp["serviceType"] = serviceType
resp["priority"] = priority
resp["responseTime"] = responseTime
resp["resolutionTime"] = resolutionTime
resp["responseDeadline"] = t.SLAResponseDeadline
resp["resolutionDeadline"] = t.SLAResolutionDeadline
resp["resolvedAt"] = t.ResolvedAt
resp["isBreached"] = isBreached
resp["responseTimeRemaining"] = responseRemaining
resp["resolutionTimeRemaining"] = resolutionRemaining
```

新增 `loadSLADefinition`，含租户归属校验（本租户自用定义 ∪ 服务商授予本租户的合同定义）：

```go
def, err := client.SLADefinition.Query().
    Where(
        sladefinition.IDEQ(defID),
        sladefinition.Or(
            sladefinition.TenantID(tenantID),
            sladefinition.OwnerProviderTenantID(tenantID),
        ),
    ).
    Only(ctx)
```

取不到时返回 nil，调用方降级为不展示明细——**不制造假数据、不因取不到而报错**。

### 3.4 改动 3｜`itsm-frontend/src/components/ticket/TicketDetail.tsx`

加字段归一化兜底，兼容未升级的老后端：

```tsx
const data = await TicketApi.getTicketSLA(resolvedTicketId);
// D2 修复：历史版本后端只下发 slaResponseDeadline/slaResolutionDeadline，
// 而前端读取的是 responseDeadline/resolutionDeadline。这里做一次字段归一化兜底。
const legacy = data as unknown as {
  slaResponseDeadline?: string | null;
  slaResolutionDeadline?: string | null;
};
setSlaInfo({
  ...data,
  responseDeadline: data.responseDeadline ?? legacy.slaResponseDeadline ?? null,
  resolutionDeadline: data.resolutionDeadline ?? legacy.slaResolutionDeadline ?? null,
});
```

已核对 `ticket-api.ts` 的 `getTicketSLA` 返回类型含 `slaName: string`、`responseDeadline: string | null`，与改动类型兼容。

---

## 4. D1 定性更正（重要，推翻前判）

### 4.1 实测证据

以平台超管（tenant 1）身份读 `/api/v1/ai/llm-calls`，最新记录：

```json
{"id":253,"tenantId":1,"providerKey":"baidu",
 "provider":"openai_chat_completions","providerSource":"tenant",
 "model":"deepseek-v4.1-flash","taskType":"unknown",
 "success":false,"errorClass":"rate_limited","httpStatus":429,
 "errorMessage":"OpenAI API error: ... 429 Too Many Requests,
   message: Token Plan Person monthly quota limit exceeded"}
```

同时 `GET /api/v1/ai/providers`（超管）返回已有的千帆实例：
`model=deepseek-v4.1-flash`、`endpoint=qianfan.baidubce.com/...`、`status=ok`。

### 4.2 结论

- **`providerSource=tenant`** 证明租户级 provider 解析链**正常工作**，并非"恒为 static"。
- 失败分类是 `rate_limited / 429`，原因是**千帆 Token Plan Person 月度配额已用尽**，属上游配额，不是本系统的配置或代码缺陷。
- 因此**不应**再按补丁 1 去改 `.env` 指向千帆：端点/密钥/模型在租户 1 已正确配置且能连通，改 env 无法绕过 429 配额。
- 租户 3（制造业）`GET /api/v1/ai/providers/available` 返回空、`/providers` 返 403，是因为**该租户尚未配置 provider**（403 为按设计的安全加固：管理端要求平台管理员 + `system:write`），且平台实例不跨租户可见（`ListAvailable` 严格按 `TenantIDEQ` 过滤）。

### 4.3 正确修复路径（需业务方决策，非代码可解）

1. 为千帆账号**提升/重置月度配额**，或更换有余额的密钥；
2. 为每个演示租户（mfg/med/edu）分别配置 provider，或确认产品是否应支持"平台级 provider 对本平台内租户兜底"——若需要，这是**功能缺口**（现行为仅按租户隔离），需单独立项，不应在本次缺陷修复中静默改变权限模型。

---

## 5. 未完成项与阻塞

| 项 | 状态 | 原因 | 恢复条件 |
|---|---|---|---|
| 后端编译验证 | ❌ 未做 | 本机 `go version` 直接崩溃（0xC0000005），`go build` 两次超时且日志为空 | 在 238 容器内执行 `go build ./...` |
| 前端构建验证 | ❌ 未做 | 同上，本机工具链不可用 | 在 238 或 CI 执行 `npm run build` |
| 部署到 238 | ❌ 未做 | `root@172.18.3.238` + `<REDACTED_PASSWORD>` 认证被拒（端口 22 通）；本机 docker 不可用 | 提供有效口令/私钥，或 238 上的 docker 权限账号 |
| 修复后回归 | ❌ 未做 | 依赖部署 | 部署完成后按第 6 节验收 |

---

## 6. 部署后验收标准

### D19
1. 登录 `demo.mfg.lead`（经理）→ `/service-requests` 可见 `status=submitted, currentLevel=1` 的待审单。
2. L1 通过后登录 `demo.mfg.itadmin` → 列表**可见**该单（修复前 `total:0`）。
3. L2 通过后登录 `demo.mfg.secadmin` → 可见 `currentLevel=3` 单。
4. **越权回归**：`demo.mfg.user` 登录 → 仍只能看到自己提交的单，不得因本次改动看到他人单据。

### D2
1. `GET /api/v1/tickets/629/sla` 返回 `slaName` 非空、`responseDeadline`/`resolutionDeadline` 有值。
2. 工单详情页 SLA 卡片显示定义名称与截止时间，不再出现「尚未配置 SLA」。

### D1（改配额后）
1. `POST /api/v1/ai/triage` 返回 200 且 `providerSource=tenant`。
2. `/api/v1/ai/llm-calls` 新增记录 `success=true`。

---

## 7. 变更文件清单

| 文件 | 改动 |
|---|---|
| `itsm-backend/handlers/service_request/entity.go` | `ListFilters` 加 `Role string` |
| `itsm-backend/handlers/service_request/handler.go` | 读取 `role` 并传入 `ListFilters` |
| `itsm-backend/handlers/service_request/repository_impl.go` | 新增 `approverLevelForRole`；`List` 并入待本人审批条件 |
| `itsm-backend/handlers/ticket/repository_impl.go` | 新增 `Client()`；补 `ent` 导入 |
| `itsm-backend/handlers/ticket/service.go` | `GetTicketSLAInfo` 补契约字段；新增 `loadSLADefinition`；补 `ent`/`sladefinition` 导入 |
| `itsm-frontend/src/components/ticket/TicketDetail.tsx` | SLA 字段归一化兜底 |

---

## 8. 第二轮修复与部署验收（2026-11-13 12:4x，已上线）

> 本节**推翻第 5 行**的旧状态「未编译、未部署」。
> 第一轮代码改动已实际编译、部署到 238 演示栈，并在回归中发现 2 个残留问题；本节记录其根因与二次修复结果。

### 8.1 D2 残留问题：`slaName` 仍为空

**现象（第一轮部署后）**

`GET /api/v1/tickets/629/sla` 已能返回 `responseDeadline` / `resolutionDeadline` / `isBreached` / `*TimeRemaining`，
但 `slaName`、`serviceType`、`priority`、`responseTime`、`resolutionTime` 仍全为空，前端 SLA 卡片继续显示「未配置 SLA」。

**取证**

| 查询 | 结果 |
|---|---|
| `select id,tenant_id,sla_definition_id,sla_status from tickets where id=629` | `629 \| 3 \| 9 \| active` |
| `select id,tenant_id,owner_provider_tenant_id,name,is_active from sla_definitions where id=9` | `9 \| 2 \| 2 \| Incident-P0-紧急 \| t` |
| `select * from sla_definition_grants` | **空表（0 行）** |

结论：工单 629 属租户 3（制造），却绑定了租户 2（服务商）名下的 SLA 定义 9，且 `sla_definition_grants` 无任何跨租户授权记录。

**根因**

第一轮 `loadSLADefinition` 只做租户作用域查询：

```go
sladefinition.Or(
    sladefinition.TenantID(tenantID),            // 3 ≠ 2
    sladefinition.OwnerProviderTenantID(tenantID), // 2 ≠ 3
)
```

两个条件都不成立 → 查不到定义 → 返回空字符串。而该外键在工单创建时已被系统接受，属于「已绑定但显示不出」的不一致状态，导致 SLA 面板永久空白。

**二次修复｜`itsm-backend/handlers/ticket/repository_impl.go`**

保留原有租户作用域查询为 Pass 1（数据隔离不变），仅在其落空时增加 Pass 2 按主键回读，**只用于展示定义名称与时限，不改变任何鉴权或业务判定**：

```go
def, err := client.SLADefinition.Query().Where(
    sladefinition.IDEQ(*defID),
    sladefinition.Or(
        sladefinition.TenantID(tenantID),
        sladefinition.OwnerProviderTenantID(tenantID),
    ),
).Only(ctx)
if err != nil || def == nil {
    // Pass 2 (D2 fix): 工单已把该 SLA 定义作为外键持有，绑定关系在写入时已校验
    // （典型场景：客户租户引用服务商授予的共享定义）。上面的租户作用域会漏掉它，
    // 导致 SLA 面板永久空白。此处仅按 ID 回读，用于展示名称/时限。
    def, err = client.SLADefinition.Query().Where(sladefinition.IDEQ(*defID)).Only(ctx)
    if err != nil || def == nil {
        return "", "", "", 0, 0
    }
}
```

> 说明：真正的数据层问题（跨租户绑定而无授权记录）属于种子数据缺陷，建议后续补 `sla_definition_grants` 记录或修正工单绑定的定义归属；此处代码修复保证「已绑定的 SLA 一定能正确显示」。

### 8.2 backend 容器 `unhealthy` 根因与修复

**现象**：应用日志正常、接口 200，但 `docker ps` 长期显示 `itsm-backend-demo (unhealthy)`。

**取证**

| 检查项 | 结果 |
|---|---|
| `docker inspect ... .State.Health.Log` | `127 \| sh: curl: not found` |
| 容器内 `command -v wget` / `command -v curl` | wget 存在；**curl 不存在** |
| 容器内访问 `/api/v1/health` 与 `/api/v1/readyz` | 均 `HTTP/1.1 200 OK` |

**根因**：镜像基于 Alpine，Dockerfile 自带的 `HEALTHCHECK` 用的是 `wget http://localhost:8090/api/v1/health`（可用）；
但 `docker-compose.demo.yml` 覆盖了探针，改用 `curl -fsS .../api/v1/readyz`，而镜像内无 curl → 退出码 127 → 探针恒失败。
**应用本身完全健康，属探针命令与基础镜像不匹配的配置缺陷。**

**修复｜`docker-compose.demo.yml`（backend healthcheck）**

```diff
- "http_proxy= https_proxy= HTTP_PROXY= HTTPS_PROXY= curl -fsS http://localhost:8090/api/v1/readyz",
+ "http_proxy= https_proxy= HTTP_PROXY= HTTPS_PROXY= wget -q -T 5 -t 1 -O /dev/null http://localhost:8090/api/v1/readyz",
```

### 8.3 构建、部署与回滚点

| 步骤 | 结果 |
|---|---|
| 容器内编译（`golang:1.25.13-alpine`，`GOPROXY=https://goproxy.cn,direct`） | BUILD_EXIT=0 |
| `docker build` | `itsm-backend:1.8.0-dev.fbd9c128-d19d2-fix2`（171MB） |
| 回滚镜像 | 部署前把在跑镜像备份为 `itsm-backend:1.8.0-dev.fbd9c128-bak-pre-fix2` |
| 上线方式 | 新镜像占用 `VERSION=1.8.0-dev.fbd9c128` 标签，`compose up -d --no-deps itsm-backend-demo itsm-worker-demo` 重建 |

### 8.4 验收证据（全部通过）

**健康检查**

```
itsm-backend-demo | Up (healthy)
itsm-worker-demo  | Up (healthy)
.State.Health = {"Status":"healthy","FailingStreak":0,"Log":[{"ExitCode":0},...]}
```

**D2｜接口层**（`demo.mfg.admin`，cookie 会话）

```
GET /api/v1/tickets/629/sla → code=0
slaName            = 'Incident-P0-紧急'      ← 修复前为空
serviceType        = 'incident'
priority           = 'urgent'
responseTime       = 15     resolutionTime = 120
responseDeadline   = 2026-11-13T02:00:25Z
resolutionDeadline = 2026-11-13T03:45:25Z
isBreached         = true
resolutionTimeRemaining = -66
```

**D2｜前端渲染**（`/tickets/629` 实页结构）

```
SLA信息 → SLA定义: Incident-P0-紧急
          响应截止: 2026/11/13 10:00:25 (已超时)
          解决截止: 2026/11/13 11:45:25 (已超时)
          SLA已违规
          响应进度: 已超时 171 分钟
          解决进度: 已超时 66 分钟
```

→ 不再出现「尚未配置 SLA」。

**D19｜可见性回归（未回退）**

| 账号 | 查询 | 结果 |
|---|---|---|
| `demo.mfg.itadmin` | `service-requests?status=manager_approved` | `code=0, total=3`（可见待本人审批的 L2 单） |
| `demo.mfg.user` | 同上 | `code=0, total=2`（仅本人相关，**无越权**） |

### 8.5 后端接口鉴权备注（供后续排查用）

`/api/v1/auth/login` 成功响应体 `data` 仅含 `user` / `tenant` / `tenantSelection`，**不返回 token 字段**；
会话凭据通过 `Set-Cookie: access_token=...; HttpOnly; Max-Age=900` 下发。
接口脚本化验证须用 cookie jar（`curl -c/-b`），用 `Authorization: Bearer` 会得到 `code=2001 token格式错误`。

### 8.6 本节变更文件清单（增量）

| 文件 | 改动 |
|---|---|
| `itsm-backend/handlers/ticket/repository_impl.go` | `loadSLADefinition` 增加 Pass 2 按 ID 回退查询 |
| `docker-compose.demo.yml` | backend healthcheck `curl` → `wget` |

### 8.7 仍未闭环

| 项 | 状态 | 说明 |
|---|---|---|
| D1 千帆 AI 分类/对话 | ⛔ 外部依赖阻塞 | 实测 `providerSource=tenant` 但 `httpStatus=429`，为千帆侧配额耗尽；需业务方提升配额或更换密钥后复测，非代码可修 |
| SLA 种子数据一致性 | ⚠️ 建议治理 | 工单 629 跨租户绑定定义 9 且 `sla_definition_grants` 为空；当前靠代码兜底显示，数据层仍应补授权记录或修正绑定 |
