# 个人访问令牌（PAT）验收标准

> **Status: draft** ｜ 日期：2026-10-16 ｜ 定位：**验收标准（DoD + 用例矩阵 + 端到端执行步骤）**
> 关联：[方案文档](./personal-access-token-plan-2026-10-16.md) ｜ [实施文档](./personal-access-token-implementation-2026-10-16.md) ｜ [演示环境说明](../multi-tenant/demo-environment.md)
>
> 按文档治理规范，验收证据必须包含：**日期、Git SHA、镜像 digest、部署模式、数据库、执行命令、未验证层级**；缺失任一项只能作为参考，不构成放行依据。

---

## 1. 验收原则与判定口径

| 原则 | 说明 |
|---|---|
| **等价性优先** | 每条安全约束都先验证「同一请求用 JWT 会话」的结果，再用 PAT 验证结果**一致**；不一致即缺陷（除非方案文档 §4 明确登记为决策差异） |
| **证据化** | 每条用例记录：命令原文 + 响应原文（截取关键字段）+ 时间戳 + 执行机；截图仅作辅助 |
| **fail-closed 判定** | 任何「校验失败仍放行」的观察直接判不通过（如吊销后偶发成功、租户不匹配放行） |
| **零泄漏判定** | 在数据库、日志、审计正文、API 响应四个位置检索 `itsm_pat_` 后的完整明文串，任何命中即**阻断级**缺陷 |
| **不追溯历史数据** | `source` 历史语义污染（普通 JWT 请求记为 `login`）不属本批缺陷；只验收新写入数据的正确性 |

缺陷分级：**B1 阻断**（安全泄漏、越权、吊销失效、等价性破坏）；**B2 严重**（核心功能不可用、401/403 误判）；**B3 一般**（展示/文案/体验）；**B4 建议**。

---

## 2. 功能验收用例矩阵

> 前置：后端已部署含 PAT 功能的版本且 `PAT_ENABLED=true`；使用具备真实 membership 的演示账号（见 [demo-environment §2](../multi-tenant/demo-environment.md)）。

### A. 令牌生命周期（G1/G3）

| # | 场景 | 步骤概要 | 预期 | 分级 |
|---|---|---|---|---|
| AC-01 | 创建令牌 | 个人中心创建 `name=验收-令牌1`，有效期 90 天 | 返回 `{code:0}`，`plaintextToken` 以 `itsm_pat_` 开头，**仅此一次**；列表出现该令牌（`tokenPrefix` = 明文前 17 字符） | B2 |
| AC-02 | 一次性明文 | 刷新列表 / 重新打开详情 | 任何后续响应**均不含明文**，只有 `maskedToken`（含 `****`） | B1 |
| AC-03 | 有效期必填 | 创建时不传 `expiresInDays`/`expiresAt` | 400 参数错误 | B3 |
| AC-04 | 有效期上限 | 传 `expiresInDays=400`（>365） | 400；错误信息说明上限 | B3 |
| AC-05 | 自定义过期时间 | 传 `expiresAt`（未来 30 天） | 成功；列表 `expiresAt` 与请求一致 | B3 |
| AC-06 | 数量上限 | 连续创建至超过 `PAT_MAX_ACTIVE_PER_USER`（默认 20） | 第 21 个返回 HTTP 422 / `code=4220`（`common.UnprocessableEntityCode`），提示先吊销 | B3 |
| AC-07 | 轮换（立即） | `POST .../:id/rotate` `{gracePeriodHours:0}` | 返回新明文（一次性）；旧令牌列表状态 = 已吊销（`revokedReason=rotated`）；旧令牌立刻 401（AC-19 验证） | B2 |
| AC-08 | 轮换（宽限） | `gracePeriodHours=24` | 新旧令牌在 24h 内**均可用**；旧令牌 `expiresAt` 被收紧为 `now+24h`；`rotatedToTokenId` 指向新令牌 | B2 |
| AC-09 | 单令牌吊销 | `POST .../:id/revoke` | 状态=已吊销；重复调用幂等（不报错，状态不变） | B2 |
| AC-10 | 吊销全部 | `POST .../revoke-all` | `revokedCount` 正确；全部令牌状态=已吊销；全部令牌立刻 401 | B2 |
| AC-11 | 过期失效（演示库） | 直接 `UPDATE personal_access_tokens SET expires_at=now()-interval '1 minute' WHERE id=<id>` 后带令牌调用 | 401「token无效」 | B2 |
| AC-12 | 状态派生 | 创建一个已过 `expiresAt` 的令牌查看列表 | 状态显示「已过期」（无需后台任务） | B3 |

### B. 用令牌访问 API（G2 —— 核心硬指标）

| # | 场景 | 步骤概要 | 预期 | 分级 |
|---|---|---|---|---|
| AC-13 | **用 PAT 创建工单** | `POST /api/v1/tickets` + `Authorization: Bearer itsm_pat_xxx`，body：`{"title":"PAT 验收工单","description":"由个人访问令牌创建","priority":"medium","type":"incident"}` | `{code:0}`，返回 `ticketNumber`；前端/DB 可见该工单，`requesterId` = 令牌所属用户 | **B1（核心）** |
| AC-14 | PAT 查询列表 | `GET /api/v1/tickets?page=1&pageSize=5`（PAT） | `{code:0}`；可见范围与同用户 JWT 会话一致 | B2 |
| AC-15 | 免 CSRF | AC-13 请求**不带** `X-CSRF-Token` | 成功（`middleware/csrf.go:154-159` 对 Bearer 放行） | B3 |
| AC-16 | CLI 零改动 | `itsm-cli` 以 PAT 作为凭据执行一次查询 | 成功（`Authorization: Bearer` 通道复用） | B3 |
| AC-17 | PAT 不可用于切换租户 | 带 `X-Tenant-Code: demo-med`（非令牌租户）调用 | 401 + `reasonCode=TENANT_MISMATCH_REJECTED`，并产生 `tenant.probe_denied` 审计行 | B1 |
| AC-18 | 租户数据隔离 | 用 demo-mfg 令牌 `GET /api/v1/tickets` 全量扫描 | 返回结果不含 demo-med / demo-edu 工单 | B1 |

### C. 权限与安全约束等价性（G4 —— 逐条对照）

| # | 场景 | JWT 会话结果 | PAT 结果 | 判定 |
|---|---|---|---|---|
| AC-19 | 吊销后的令牌 | — | 401「token无效」 | **B1** |
| AC-20 | 无权限资源（如 `GET /api/v1/audit-logs`，需 `audit:read`） | 403「权限不足」 | **403**，错误码与文案一致 | **B1（等价性）** |
| AC-21 | 停用用户 | 先停用该用户，再带 PAT 调用 | 401「token无效」（双重防线：停用触发 R3 批量吊销 → 吊销态 401；未及吊销时认证期 Active 校验兜底 401）。等价性成立：JWT 会话在停用时同样于**认证层**被拒（401「账号权限已变更」，`InvalidateUserAccessTokens`），均不进入 RBAC | **B1** |
| AC-22 | 角色变更实时生效 | 角色降级后用 PAT 调用原可访问资源 | 权限按**新角色**判定（不吊销令牌但权限收紧，D-PAT-6/R4） | B2 |
| AC-23 | 密码重置吊销 | 重置该用户密码后带 PAT 调用 | 401（`revokedReason=security_event`，列表可见原因） | **B1** |
| AC-24 | 离职编排 | 执行 offboard 后带 PAT 调用 | 401；**且**管理端审计（`resource=auth`）存在 `auth.token_revoked_all` 行、`request_body.reason=security_event`（管理员可从审计页追溯为何令牌全部失效） | B2 |
| AC-25 | 会话互不干扰 | JWT 登录 / 登出 / 刷新 | 与改造前行为一致（回归） | B2 |
| AC-26 | 未启用开关 | `PAT_ENABLED=false` + 重启 | PAT 认证 401；管理面端点 404 | B2 |
| AC-27 | 认证错误防枚举 | 分别用「不存在的令牌 / 已吊销 / 已过期」调用 | 三者响应**完全一致**（401 同文案），无法区分 | B3 |

### D. 使用历史与审计（G5/G6）

| # | 场景 | 步骤概要 | 预期 | 分级 |
|---|---|---|---|---|
| AC-28 | 使用历史接口 | `GET /api/v1/auth/personal-access-tokens/:id/usages` | 返回该令牌的使用行（含 `auth.token_used` 与被审计的写操作），含 `summary.useCount/lastUsedAt`；分页正确 | B2 |
| AC-29 | 使用节流 | 60s 内重复调用同一 GET 端点多次 | `last_used_at` 与 `use_count` 的**落库更新**均最多一次（60s 节流）；精确的逐次归因以审计行为准 | B3 |
| AC-30 | 本人安全事件 | `GET /api/v1/auth/security-events` | 合并展示本人登录（`auth.login`）、令牌创建/轮换/吊销/使用事件；**无法看到他人事件**（构造他人 tokenId → 空集） | B2 |
| AC-31 | 审计来源标记 | 管理端 `GET /api/v1/audit-logs?source=pat` | PAT 相关行 `source=pat` 且 `tokenId` 非空；登录行仍为 `source=login` | B2 |
| AC-32 | 审计页前端 | 审计日志页查看 | 出现「来源」列与过滤，PAT 行显示「令牌」 | B3 |
| AC-33 | 管理动作审计 | 创建/轮换/吊销各一次后查 `resource=auth` | 存在 `auth.token_created` / `auth.token_rotated` / `auth.token_revoked` 行 | B2 |
| AC-34 | 明文零泄漏 | 在 DB（`SELECT ... FROM personal_access_tokens`）、后端日志、`audit_logs.request_body`、所有 API 响应中检索明文 | **全部无命中**（库中只有 `token_hash` 与 `token_prefix`） | **B1（阻断）** |
| AC-35 | 日志不含明文 | 复现一次认证失败 + 一次成功，检索 zap 输出 | 无完整明文（最多出现 17 字符前缀） | **B1** |

### E. RLS / 多租户（架构基线）

| # | 场景 | 步骤概要 | 预期 | 分级 |
|---|---|---|---|---|
| AC-36 | RLS enforce 下 PAT 认证可用 | enforce 档位环境重复 AC-13 | 成功（认证期 `SystemContext` 点查生效） | **B1** |
| AC-37 | RLS 隔离 | enforce 档位：租户会话查询 `personal_access_tokens` | 只见本租户行；无租户上下文不可见 | B1 |
| AC-38 | RLS 策略存在性 | `SELECT * FROM pg_policies WHERE tablename='personal_access_tokens'` | 策略存在（ENABLE，USING/WITH CHECK 均含 `tenant_id = get_current_tenant_id()`） | B2 |

### F. 防滥用与前端卫生（审查增补）

| # | 场景 | 步骤概要 | 预期 | 分级 |
|---|---|---|---|---|
| AC-39 | **防自我繁殖** | 用 PAT 调 4 个管理面写路由（create/rotate/revoke/revoke-all） | 一律 403「请使用浏览器会话管理访问令牌」，**且零副作用**（令牌列表/数量/审计均无变化）；读路由（list/usages/security-events）正常可用 | **B1（阻断）** |
| AC-40 | 平台角色禁令 | 以 super_admin 会话调 `POST /personal-access-tokens` | 403（D-PAT-14：平台角色不予发放长期旁路凭据） | **B1** |
| AC-41 | 前端明文生命周期 | 创建令牌 → 明文展示 → 关闭弹窗 → 检索 React Query 缓存、localStorage、sessionStorage | 三处均无明文（Jest 断言 + 手工 DevTools 复核） | **B1** |
| AC-42 | logout 无害性 | 带 PAT 调 `POST /auth/logout` | 401「token无效」；令牌状态不变（无吊销副作用） | B3 |
| AC-43 | 令牌不入 URL | 把 PAT 放入 query（`?token=itsm_pat_...`）或 body 调用 | 认证一律失败（仅 Authorization 头生效）；api-reference 有「禁止放入 URL（会进访问日志）」警示 | B2 |
| AC-44 | 单令牌限流 | 1 分钟内用同一 PAT 高频调用（> `PAT_RATE_LIMIT_PER_MINUTE`） | 429；其余用户/令牌不受影响 | B3 |

---

## 3. 自动化验收（提交前必跑）

| 层 | 命令 | 通过标准 |
|---|---|---|
| 后端编译 | `cd itsm-backend && go build ./...` | exit 0 |
| 认证/领域/服务 | `go test ./middleware/... ./handlers/pat/... ./service/... -count=1` | 全绿，含「落库无明文」「60s 去重」「等价上下文」断言 |
| 路由与守卫 | `go test ./router/... -count=1` | 全绿（写路由守卫 / 预检新鲜度 / 权限码目录） |
| 端到端（进程内） | `go test ./router/ -run TestPAT -count=1`（`router/pat_routes_test.go`） | 会话创建 → PAT 建工单 → 吊销 401 → 跨租户 401 全链路通过 |
| 前端 | `cd itsm-frontend && npm run type-check && npm run lint:check` | exit 0 |
| 前端单测 | `npm run test:unit -- --testPathPattern 'personal-access-token|AccessTokenPanel|auditlog'` | 全绿（列表/一次性明文/吊销/复制） |
| 文档门禁 | `make docs-gate` | 无阻断（advisory 项需逐条说明或修复） |

---

## 4. 238 演示环境端到端验收步骤（Runbook）

> **前置条件（P0）**
> 1. 已按 [itsm-deploy SKILL](../../.agents/skills/itsm-deploy/SKILL.md) 将含本功能的镜像部署到 demo 栈，版本 `1.8.0-dev.<sha>`；
> 2. **执行机具备到 172.18.3.238 的网络通道**（2026-10-16 实测：本工作站直连 `:8089`/`:22` 超时，经 SOCKS `127.0.0.1:10808` 亦 502 → 需在 238 本机或具备内网通道的机器执行）；
> 3. 演示账号见 [demo-environment §2](../multi-tenant/demo-environment.md)（口令为公开演示口令，仅演示环境使用；本 Runbook 一律以 `$DEMO_PASSWORD` 变量引用，不落明文）。

```bash
BASE=http://172.18.3.238:8089/api/v1

# ---- 0. 健康与版本 ----
curl -fsS $BASE/health                                   # {"status":"ok",...}
curl -fsS $BASE/version                                  # data.version == 本次部署 tag

# ---- 1. 会话登录（Cookie）----
curl -sS -c /tmp/pat_c.txt -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo.mfg.admin","password":"'"$DEMO_PASSWORD"'"}'
# → code:0（demo 未开启 CSRF；若开启，则先 GET $BASE/csrf-token 取 data.csrf_token，
#   后续 Cookie 会话写请求统一加 -H "X-CSRF-Token: $CSRF"）

# ---- 2. 创建 PAT（AC-01）----
curl -sS -b /tmp/pat_c.txt -X POST $BASE/auth/personal-access-tokens \
  -H 'Content-Type: application/json' \
  -d '{"name":"acceptance-token","expiresInDays":90}'
# 保存 data.plaintextToken 为 $PAT；保存 data.token.id 为 $TID

# ---- 3. 列表与掩码（AC-02）----
curl -sS -b /tmp/pat_c.txt $BASE/auth/personal-access-tokens
# 断言：响应含 tokenPrefix / maskedToken；grep "$PAT" 无命中

# ---- 4. 【核心】用 PAT 创建工单（AC-13/15）----
curl -sS -X POST $BASE/tickets \
  -H "Authorization: Bearer $PAT" -H 'Content-Type: application/json' \
  -d '{"title":"PAT 验收工单","description":"由个人访问令牌创建","priority":"medium","type":"incident"}'
# 断言 code:0 + ticketNumber；记下 $TICKET_ID

# ---- 5. 数据隔离（AC-18）：断言返回工单的 tenantId 全部等于令牌所属租户 ----
TENANT_ID=$(curl -sS -b /tmp/pat_c.txt $BASE/auth/me | grep -o '"tenantId":[0-9]*' | head -1 | cut -d: -f2)
curl -sS -H "Authorization: Bearer $PAT" "$BASE/tickets?page=1&pageSize=100" \
  | grep -o '"tenantId":[0-9]*' | sort -u
# 预期：仅输出一行，且等于 $TENANT_ID（出现其它 tenantId 即 FAIL）

# ---- 6. 权限等价（AC-20：audit-logs 需 audit:read）----
curl -sS -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $PAT" $BASE/audit-logs   # 403

# ---- 7. 租户越权探测（AC-17）----
curl -sS -H "Authorization: Bearer $PAT" -H 'X-Tenant-Code: demo-med' $BASE/tickets
# 断言 401 + reasonCode=TENANT_MISMATCH_REJECTED

# ---- 8. 使用历史与安全事件（AC-28/30/31）----
curl -sS -b /tmp/pat_c.txt "$BASE/auth/personal-access-tokens/$TID/usages?page=1&pageSize=10"
curl -sS -b /tmp/pat_c.txt "$BASE/auth/security-events?kind=all&page=1&pageSize=10"
curl -sS -b /tmp/pat_c.txt "$BASE/audit-logs?source=pat&page=1&pageSize=5"    # 管理账号有 audit:read

# ---- 9. 过期与吊销（AC-11/19；演示库直连 DB 仅用于验收）----
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo \
  -c "UPDATE personal_access_tokens SET expires_at = now() - interval '1 minute' WHERE id = $TID"
curl -sS -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $PAT" $BASE/tickets        # 401

# ---- 10. 清理（AC-10）----
curl -sS -b /tmp/pat_c.txt -X POST $BASE/auth/personal-access-tokens/revoke-all -H 'Content-Type: application/json' -d '{}'
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -c "DELETE FROM tickets WHERE id = $TICKET_ID"
```

**补充用例（演示库）**：AC-07/08 轮换、AC-21/23 停用与改密吊销（用演示库非关键账号）、AC-34/35 明文检索（DB + `docker logs itsm-backend-demo`）、AC-36/37 RLS（若 demo 未开 enforce，在本地 Postgres 或临时环境验证并记录「未验证层级」）。

---

## 5. 证据记录模板

```markdown
## 验收记录：PAT（<日期>）
- Git SHA：<short-sha>　版本 tag：1.8.0-dev.<sha>
- 镜像 digest：backend <sha256:...>　frontend <sha256:...>
- 部署模式：docker-compose.demo（itsm-demo，8089）　数据库：itsm_demo
- 执行机：<host>（到 238 的通道：<直连/代理/238 本机>）
- 结果：AC-01..AC-38 逐条 PASS/FAIL/SKIP（SKIP 必须写明未验证层级）

| 用例 | 命令/操作 | 关键响应 | 判定 |
|---|---|---|---|
| AC-13 | `curl -X POST .../tickets -H "Authorization: Bearer itsm_pat_****"` | `{"code":0,...,"ticketNumber":"INC-xxxx"}` | PASS |
...
- 缺陷清单：<编号 / 级别 / 描述 / 复现>
- 结论：<放行 / 有条件放行（列出 B3 清单）/ 不放行>
```

证据文档落位：`.dev/deploy/ACCEPTANCE-PAT-<date>-<sha>.md`，随部署提交入库。

---

## 6. 放行判定

| 判定 | 条件 |
|---|---|
| **放行** | 全部 B1/B2 用例 PASS（**含 AC-34/35 零泄漏、AC-39 防自我繁殖、AC-40 平台角色禁令、AC-41 明文前端卫生**）；B3 ≤ 3 条且有修复计划；自动化验收全绿；api-reference PAT 章节已交付 |
| **有条件放行** | 仅 B3 未清零；B1/B2 全 PASS |
| **不放行** | 任一 B1/B2 FAIL；或明文在任一位置泄漏；或等价性用例（AC-17/18/20/21/23）FAIL |

---

## 7. 验收清单（汇总勾选）

- [ ] AC-01..AC-12 生命周期
- [ ] **AC-13 用 PAT 创建工单（核心）**
- [ ] AC-14..AC-18 API 访问与租户隔离
- [ ] AC-19..AC-27 权限/安全等价性
- [ ] AC-28..AC-35 使用历史、审计与零泄漏
- [ ] AC-36..AC-38 RLS
- [ ] **AC-39 防自我繁殖 / AC-40 平台角色禁令 / AC-41 明文前端卫生**
- [ ] AC-42..AC-44 logout 无害 / 令牌不入 URL / 单令牌限流
- [ ] §3 自动化验收全绿
- [ ] §4 演示环境 Runbook 执行完毕且证据入库
- [ ] 文档状态由 `draft` → `current`（方案/实施/验收三件套 + CHANGELOG + api-reference）
