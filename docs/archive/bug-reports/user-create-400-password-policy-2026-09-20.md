# 用户管理「新建用户」400 错误 —— 排查与处理记录

> Status: historical。2026-09-20 的排查与修复记录，只适用于当时的 v1.6.9 测试环境与对应镜像；第 10 节为仅本地、未部署的后续改动。

- 环境：http://172.18.3.238:8088 （ITSM v1.6.9 测试环境，docker compose prod 栈）
- 时间：2026-09-20（CST）
- 排查人：AI 助手（本地 aicli）
- 结论：**不是部署故障，而是前后端密码策略不一致导致的输入校验问题**；后端另有一处错误码映射缺陷（策略类错误返回 500）。
- 处置：**已于 2026-09-20 15:40–16:05 (CST) 完成修复并部署到 172.18.3.238，回归通过**（详见第 9 节）。
- 影响：管理员在「系统管理 → 用户管理 → 新建用户」时，若密码不满足后端策略，页面只提示 `HTTP error! status: 400`（或 500），没有可读的规则提示，用户无法自助纠正。

---

## 1. 复现过程与证据

### 1.1 工具说明
用户要求使用 `list_pages` 检查页面。本机 MCP 服务 `local-e2e` 不可用（`MCP 客户端不存在: local-e2e`），
故改用本机 **browser-skill（bsk CLI + Edge 扩展）** 操作用户已登录的浏览器，并辅以 HTTP API 直连复现，两条路径互相印证。

### 1.2 页面复现（bsk，Edge 已登录 admin）
| 步骤 | 结果 |
|:---|:---|
| 打开 http://172.18.3.238:8088/admin/users | 页面正常，列表显示 1 个用户（admin），「新建用户」按钮可点 |
| 打开「新建用户」弹窗 | 表单字段：用户名*、姓名*、邮箱*、电话、部门、密码*、主角色（默认"最终用户"）、RBAC 角色 |
| 填写：testuser02 / 测试用户02 / testuser02@example.com / 密码 `Pass1234`（8 位）→ 提交 | `POST /api/v1/users` → **HTTP 400**；页面 toast 原文：`HTTP error! status: 400` |
| 密码改为 `Passw0rd1234`（12 位，无特殊字符）→ 提交 | `POST /api/v1/users` → **HTTP 500** |
| 密码改为 `Passw0rd1234!`（13 位，含大小写/数字/特殊字符）→ 提交 | `POST /api/v1/users` → **HTTP 200**，用户创建成功（列表出现 testuser03） |

### 1.3 后端日志证据（itsm-backend-prod）
```text
# 8 位密码（400）
2026-09-20T07:30:05.515Z error gin@v1.12.0/context.go:192 参数绑定失败:
  Key: 'CreateUserRequest.Password' Error:Field validation for 'Password' failed on the 'min' tag
[GIN] 2026/09/20 - 07:30:05 | 400 | POST "/api/v1/users"

# 12 位无特殊字符（500）
2026-09-20T07:29:13.865Z error gin@v1.12.0/context.go:192 创建用户失败:
  密码必须同时包含大写字母、小写字母、数字和特殊字符
[GIN] 2026/09/20 - 07:29:13 | 500 | POST "/api/v1/users"

# 合规密码（200）
[GIN] 2026/09/20 - 07:30:15 | 200 | POST "/api/v1/users"
```

### 1.4 前端网络抓包（bsk network）
```
POST /api/v1/users → 403（CSRF token 过期，前端自动刷新后重试）
POST /api/v1/users → 400 / 500（重试结果）
POST /api/v1/users → 200（合规密码）
```
> 403 是前端 CSRF 令牌轮换机制的正常重试，与本问题无关。

---

## 2. 根因分析

### 2.1 直接原因：前后端密码策略不一致
| 位置 | 规则 | 代码位置 |
|:---|:---|:---|
| 前端表单 | 仅校验「必填 + 最少 6 位」 | `itsm-frontend/src/app/(main)/admin/users/page.tsx:585-588`（`{ min: 6 }`） |
| 后端 DTO 绑定 | 密码 12–128 位 | `itsm-backend/dto/user_dto.go:14`（`binding:"required,min=12,max=128"`） |
| 后端业务校验 | 必须含大写、小写、数字、特殊字符 | `itsm-backend/service/user_service.go:605-626` `validatePassword()` |

前端提示「至少 6 位」，后端要求「至少 12 位 + 四类字符」。管理员按界面提示填写 6–11 位密码时，
请求被后端 400 拒绝；填 12 位但缺特殊字符时，被业务层拒绝。

### 2.2 次生问题：错误信息不可读 + 错误码映射错误
1. **前端**：新建用户失败时 toast 直接显示 `HTTP error! status: 400`（未把后端 `message` 展示出来/未做字段级提示），
   用户无法知道到底哪里不合规。表单也没有任何密码规则说明文字。
2. **后端**：`handlers/user/handler.go` 的 `CreateUser` 只把「已存在」（用户名/邮箱重复）映射为 400，
   其余业务错误一律走 `common.FailWithErr(...)` → **500**。密码策略属于客户端输入错误，应返回 400 而非 500
   （`handlers/user/handler.go:81-91`）。`ResetPassword`（重置密码）存在同样问题（`:375-384`）。
3. 绑定校验失败时返回统一的 `{"code":1001,"message":"请求参数错误"}`，不携带具体字段/规则。

### 2.3 为什么"部署"没问题
合规密码下同一接口、同一 UI 流程返回 200 且数据成功落库，说明 nginx → frontend → backend → postgres 全链路正常。

---

## 3. 服务器部署检查（172.18.3.238，ssh n9e + sudo）

| 检查项 | 结果 |
|:---|:---|
| 容器 | 7 个 ITSM 容器全部 `Up 47 hours (healthy)`（nginx/frontend/backend/worker/ai-service/postgres/redis）；另有历史容器 zen_tesla 正常 |
| 镜像 | `itsm-frontend:1.6.9`、`itsm-backend:1.6.9`、`itsm-ai-service:1.6.9`（本机构建，与部署文档一致） |
| 后端健康 | `/api/v1/health` → `{"status":"ok"}`；`/api/v1/readyz` → `{"ready":true,"schemaVersion":"021_add_workflow_template_catalog",...}` |
| 数据库 | `schema_migrations` 已应用 **46** 条；`users` 表仅 admin（+排查期间创建的临时用户，已删除） |
| 配置 | `RLS_MODE=shadow`、`LOG_LEVEL=error`；compose 文件与 `.env.prod` 位于 `/home/n9e/itsm/repo` |
| 磁盘 | `/` 88%（13G 可用，偏紧）、`/data` 15%（170G 可用） |

> 说明：`/` 分区使用率 88% 建议关注（Docker 数据目录已迁移至 `/data`，短期无阻塞）。

---

## 4. 已实施的代码修复（本地源码 E:\projects\itsm\itsm）

> 源码取自 `itsm-main.tar.gz`（v1.6.9，与线上镜像 commit b89182b 基本一致；后端行号可对上日志）。

### 4.1 后端：策略类错误返回 400 并透出规则（`itsm-backend/handlers/user/handler.go`）
```go
// CreateUser / ResetPassword 中新增分支
if strings.HasPrefix(err.Error(), "密码") {
    common.ParamError(c, err.Error())   // 400 + 具体规则，例如"密码长度必须为12到128位"
    return
}
```

### 4.2 前端：表单规则与后端对齐 + 提示文案（`page.tsx` / `translations.ts`）
```tsx
rules={[
  { required: true, message: t('users.form.requiredPassword') },
  { min: 12, message: t('users.form.minPassword') },
  {
    pattern: /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d)(?=.*[^A-Za-z0-9]).{12,128}$/,
    message: t('users.form.passwordPolicy'),
  },
]}
extra={t('users.form.passwordHint')}   // 常驻提示：12-128 位，含大小写/数字/特殊字符
```
新增/更新 i18n（中英）：`minPassword` 改为 12 位，新增 `passwordPolicy`、`passwordHint`。

### 4.3 验证
- 后端：`go build ./handlers/user/ ./service/` → **BUILD_OK**（Go 1.25.13）。
- 前端：本地无 `node_modules`，未执行 `tsc`；改动为表单 rules + i18n 文案，需在服务器构建镜像时验证。

---

## 5. 修复上线步骤（如需部署，在 172.18.3.238 执行）

```bash
# 1) 同步修改后的文件到 /home/n9e/itsm/repo（backend handler + frontend page.tsx + translations.ts）
# 2) 重建镜像（frontend 走 node 构建，backend 走 go 构建）
cd /home/n9e/itsm/repo
sudo docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml build itsm-backend itsm-frontend
# 3) 滚动重启
sudo docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml up -d itsm-backend itsm-worker itsm-frontend
# 4) 回归验证
#    - 新建用户填 8 位密码 → 前端应直接提示"密码至少12个字符"，不再发请求
#    - 填 12 位无特殊字符 → 后端应返回 400 且 message 为具体规则（不再 500）
#    - 填合规密码 → 200 创建成功
```
> 回滚：镜像 tag 不变时可用 `docker tag` 备份当前镜像后重建；或恢复 `.orig` 源码重建。

---

## 6. 临时规避方法（线上当前版本，无需改代码）

创建用户时密码需满足：**长度 12–128 位，且同时包含大写字母、小写字母、数字和特殊字符**，
例如 `Passw0rd1234!`（已实测通过）。

---

## 7. 排查期间产生的临时数据

- 通过 UI/API 创建了验证用户 `testuser03`（id=2）：先经 `DELETE /api/v1/users/2` 软删除（`active=false`），
  随后清理 `user_roles` 边并物理删除 `users` 行；复核 `users` 表仅剩 `admin`，环境已还原。
- 新增本地排查脚本：`tools/api_check.py`（API 登录/调用）、`tools/deploy_check.sh`（部署只读巡检）、
  `tools/payload_create_user.json`、`tools/check_user_fk.sh`、`tools/cleanup_test_user.sh`。
- 本地源码解压目录：`E:\projects\itsm\itsm`（由 `itsm-main.tar.gz` 解压，含中文名文件解压告警，不影响后端源码）。

---

## 8. 结论与建议

1. **结论**：400 的直接原因是前端密码规则（≥6 位）与后端策略（≥12 位 + 四类字符）不一致，
   且后端对策略错误返回 500、前端只显示原始状态码，导致问题难以自查。部署本身健康。
2. **建议**：
   - 上线第 4 节修复（前端校验 + 后端 400 映射 + 可读文案）。
   - 密码策略建议在后端以结构化错误码（如 `ErrWeakPassword` + 详细字段）返回，前端做字段级提示，避免文案耦合。
   - 关注 `/` 分区 88% 使用率。
   - 修复上线后补充回归用例：新建用户/重置密码的密码边界值（11/12/128/129 位、缺各类字符）。

---

## 9. 修复部署记录（2026-09-20 实施）

### 9.1 时间线（CST）
| 时间 | 动作 | 结果 |
|:---|:---|:---|
| 15:40 | 备份服务器源码（`.bak-20260920` × 3） | 成功，原文件 14630 / 24710 / 372919 字节 |
| 15:41 | 上传修复文件到 `/home/n9e/itsm/repo` | 3 个文件 SHA256 与本地逐一比对一致 |
| 15:44 | 重建后端镜像 `bash scripts/build-images.sh 1.6.9 "" backend` | 成功，`go build` 通过，新镜像 `sha256:0aa7c633…` |
| 15:46–16:01 | 重建前端镜像 `bash scripts/build-images.sh 1.6.9 "" frontend` | 成功，`✓ Compiled successfully in 4.6min`，**TS 类型检查通过**（仅既有 ESLint 警告），143 个静态页生成 |
| 16:02 | `docker compose up -d itsm-backend itsm-worker itsm-frontend` | 三个容器滚动重建，全部 `healthy` |
| 16:03 | 健康检查 | `health: {"status":"ok"}`，`readyz: {"ready":true, schemaVersion 021…}` |
| 16:05 | 回归验证（API + UI） | 全部通过（见 9.3） |

### 9.2 变更与产物
- 部署方式：`docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml`（project=itsm-prod），镜像 tag 仍为 `1.6.9`。
- 新镜像 ID：backend `sha256:0aa7c633a0abe62f77b4af45342dec86ad94fa8a70f9646dcb497549c81e02a5`；
  frontend `sha256:349d99d194a2953ab93cadd988faa8760ed2e213acfc20f1017fe702cab9e5bf`。
- 容器状态：`itsm-backend-prod` / `itsm-worker-prod` / `itsm-frontend-prod` 均 `Up (healthy)`；postgres / redis / nginx / ai-service 未重建，保持 `Up 2 days (healthy)`。
- 备份文件（回滚用）：
  - `/home/n9e/itsm/repo/itsm-backend/handlers/user/handler.go.bak-20260920`
  - `/home/n9e/itsm/repo/itsm-frontend/src/app/(main)/admin/users/page.tsx.bak-20260920`
  - `/home/n9e/itsm/repo/itsm-frontend/src/lib/i18n/translations.ts.bak-20260920`

### 9.3 回归验证结果（部署后实测）
| 用例 | 修复前 | 修复后 | 判定 |
|:---|:---|:---|:---|
| 8 位密码 `Pass1234` → API | 400「请求参数错误」 | 400「请求参数错误」 | 不变（绑定层拦截） |
| 12 位无特殊字符 `Passw0rd1234` → API | **500** | **400 + `密码必须同时包含大写字母、小写字母、数字和特殊字符`** | ✅ 缺陷修复 |
| 13 位合规 `Passw0rd1234!` → API | 200 | 200（创建成功） | ✅ 功能未回归 |
| 重置密码 `PUT /users/{id}/reset-password` + 12 位无特殊字符 | **500** | **400 + 同一规则文案** | ✅ 同一缺陷修复 |
| UI：密码框下方提示 | 无 | 「密码长度 12-128 位，需包含大写字母、小写字母、数字和特殊字符」 | ✅ 前端镜像生效 |
| UI：填 8 位密码提交 | 发请求 → toast `HTTP error! status: 400` | **前端直接拦截**，字段提示「密码至少12个字符」，`bsk network` 确认未发出 `POST /api/v1/users` | ✅ 体验修复 |

- 验证用测试用户 `verifyokuser`（id=3）、`verifyrpuser`（id=4）均已清理：先删 `user_roles` 边再删 `users` 行，复核 `users` 表仅剩 `admin`（id=1）。
- 回归脚本：`tools/api_check.py` + `tools/payload_weak8.json` / `payload_nospecial.json` / `payload_valid.json`；
  注意 DTO 字段名为 `name` / `role` / `roleIds`（不是 `display_name` / `role_ids`），字段写错会先触发 `Name required` 绑定错误。

### 9.4 回滚方式
```bash
cd /home/n9e/itsm/repo
# 1) 还原源码
cp itsm-backend/handlers/user/handler.go.bak-20260920 itsm-backend/handlers/user/handler.go
cp 'itsm-frontend/src/app/(main)/admin/users/page.tsx.bak-20260920' 'itsm-frontend/src/app/(main)/admin/users/page.tsx'
cp itsm-frontend/src/lib/i18n/translations.ts.bak-20260920 itsm-frontend/src/lib/i18n/translations.ts
# 2) 重建并重启
bash scripts/build-images.sh 1.6.9 "" backend
bash scripts/build-images.sh 1.6.9 "" frontend
sudo docker compose --env-file .env.prod -f docker-compose.prod.yml -f docker-compose.test.yml up -d itsm-backend itsm-worker itsm-frontend
```

### 9.5 遗留事项
- `/` 分区使用率 88%（13G 可用）仍建议关注；本次构建产生的旧镜像层可用 `docker image prune` 清理。
- 前端镜像未打 `latest` 标签（构建脚本按 `VERSION=1.6.9` 打 tag），与现状一致。
- 建议后续把密码策略改为后端返回结构化错误码（如 `ErrWeakPassword`）并由前端做字段级映射，避免前后端文案/规则再次漂移。

---

## 10. 第二阶段修复：让配置页的密码策略真正生效（2026-09-20，仅本地代码，未部署）

### 10.1 问题与根因

**现象**：`/admin/system-config` 安全设置里 `passwordMinLength` 显示 8，但「新建用户」「注册」「重置密码」等表单仍提示并校验「密码至少 12 个字符」。

**根因**：`system_configs` 里的密码键长期是「死配置」——展示用得上，校验时不读：

| 层 | 修复前 | 后果 |
|:---|:---|:---|
| 后端服务 | `service/user_service.go` 硬编码 12–128 + 四类字符 | 配置成 8 也按 12 拦截 |
| 后端绑定 | DTO `binding:"min=12,max=128"` | 8 位密码在绑定层就被拒（400 请求参数错误） |
| 后端策略 | `passwordPolicyConfigKeys` 缺 `passwordMaxLength` | 配置页的最大长度字段从未被策略读取 |
| 前端 | 表单规则硬编码 `min: 12` | 提示与拦截都停留在 12 位 |

### 10.2 后端改动（`itsm-backend`）

- `service/password_policy.go`（策略唯一事实来源）：`passwordPolicyConfigKeys` 补入 `passwordMaxLength`；新增 `effectiveBounds()`（下限 6 / 上限 128 夹紧）与 `normalized()`；`GetPasswordPolicy` 在写缓存前归一化，保证对外暴露的 min/max 与 `Validate` 判定完全一致；`applyPasswordPolicyConfig` 处理最大长度。
- DTO 绑定放宽为兜底 `min=6,max=128`（`dto/auth_dto.go`、`dto/user_dto.go`），真实强度统一交给 `PasswordPolicy.Validate` 按租户配置校验；`handlers/auth/service.go` 注册路径同样走策略校验（不再绕过）。
- 新增公开只读端点 `GET /api/v1/auth/password-policy`（`router/router.go`），按 `X-Tenant-ID` / `X-Tenant-Code` 返回当前租户生效策略，未登录页面也能取到。
- 测试：`service/password_policy_test.go` 新增 `TestPasswordPolicy_MaxLengthConfig`（配 16 生效、配 9999 夹紧为 128）；修正 `handlers/auth/service_test.go` 中不满足默认策略的旧口令 `brand-new-pass` → `Brand-New-Pass1`。

### 10.3 前端改动（`itsm-frontend`）

- 新增 `src/lib/api/password-policy-api.ts`：拉取 / 归一化 / 60s 缓存 + 并发去重 / `clearPasswordPolicyCache()` / `buildPasswordRules()` / `buildPasswordHint()`。
- 新增 `src/lib/hooks/usePasswordPolicy.ts`：默认策略起步，取到租户策略后覆盖；接口异常时回退默认 12-128 四类（不放行弱密码）。
- 接入页面：新建用户、管理员重置密码、个人中心改密、注册页 —— 规则与提示均来自上述策略。
- `/admin/system-config` 保存成功后调用 `clearPasswordPolicyCache()`，策略立即生效，无需重启或等 TTL。
- i18n 新增 `passwordPolicy.*`（zh/en，含 `{min}`/`{max}`/`{classes}` 占位符）。
- 测试：`src/lib/api/__tests__/password-policy-api.test.ts`、`src/lib/hooks/__tests__/usePasswordPolicy.test.ts`，共 17 例。

### 10.4 本地验证（全部通过）

| 检查 | 命令 | 结果 |
|:---|:---|:---|
| 后端策略单测 | `go test ./service/ -run 'TestPasswordPolicy' -count=1` | ok |
| 后端服务 / 认证 / 路由 | `go test ./service/ ./handlers/auth/ ./router/ -count=1 -p 1` | ok |
| 前端类型检查 | `npx tsc --noEmit` | 0 error |
| 前端单测 | `npx jest src/lib/api/__tests__/password-policy-api.test.ts src/lib/hooks/__tests__/usePasswordPolicy.test.ts --coverage=false` | 17 passed |
| 前端 ESLint | `npx eslint <改动文件>` | 通过 |

> 环境备注：`go build ./...` 在本机会因 `cmd/cmdb` 内存不足失败，故按包范围测试并加 `-p 1`；`npm ci` 因 lock 与 package.json 不同步失败，安装依赖用 `npm install --no-package-lock`。

### 10.5 部署状态与上线步骤

- **本阶段未部署**：以上改动只在本地源码并通过本地测试，`172.18.3.238` 线上仍是第 9 节所述的「12 位硬编码」版本。
- 上线时：同步 backend + frontend 改动 → 重建两个镜像 → 滚动重启 → 在配置页把 `passwordMinLength` 设为 8，然后验证「8 位合规密码可提交成功、表单提示同步变为 8-128」。
- 风险提示：策略下限为 6、上限为 128；把 min 调低即放宽强度要求，如需收紧请同时保持四类字符要求开启。
