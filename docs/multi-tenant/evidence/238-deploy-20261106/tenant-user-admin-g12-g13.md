# 238 部署记录 · 租户用户护栏与错误码修复（G12/G13）

> 状态：**已上线（2026-11-06）**，目标栈 `itsm-demo`（`http://172.18.3.238:8089`）
> 部署对象：`fix(tenant-user-admin): 最后管理员护栏取服务端租户级计数 + membership 错误码补齐`
> 关联：[`review` v0.2](../../../review/2026-11-06-tenant-user-role-adjustment-review.md)、[`07-known-gaps.md` G12–G16](../../07-known-gaps.md)

## 1. 来源与 lineage（复现前必读）

| 项 | 值 |
|---|---|
| 开发仓 | `git@github.com/wwsheng009/itsm.git`，分支 `feat/msp-p0-hardening`（领先 `origin/main` 615 commits，未推送） |
| 238 仓 | `/home/n9e/itsm/repo`，`origin = https://github.com/heidsoft/itsm.git`（**不同 fork**） |
| 后果 | 两仓 hash 空间不通用：本地 `456394ab` 与 238 `6cf692f45` 是**同信息不同 hash**（cherry-pick 产物）。**不能**用 push→pull 部署，只能走 patch |
| 238 未随本批部署的本地改动 | 前端 i18n 链（`37f5d9fe`…`b0506cd5`）、`fix(compile)`、`feat(skills)`、sidebar 参数、租户到期 null 修复等（属预期差异，非遗漏） |

部署方式：`git diff 1b017498^ 1b017498` → `qoder-deploy-tum.patch` → pscp 上传 → `git apply --index` + 在 238 本地提交（沿用该 host「镜像 tag = 本机 sha」约定）。

## 2. 结果

| 项 | 值 |
|---|---|
| 238 HEAD | `6cf692f45` → **`057b38132`**（patch commit；部署后 tracked 树干净） |
| 镜像 | `itsm-backend:1.8.0-dev.057b3813`、`itsm-frontend:1.8.0-dev.057b3813` |
| `.env.demo` | `VERSION=1.8.0-dev.057b3813`；备份 `.env.demo.bak-pre-tum-20261106-104832Z` |
| 容器 | `itsm-backend-demo` / `itsm-worker-demo` / `itsm-frontend-demo` 全部 `Up (healthy)` |
| prod 栈 | **未触碰**：8088 `/api/v1/version` 仍 `1.8.0-dev.586bb07a` |
| 8089 `/api/v1/version` | `1.8.0-dev.057b3813` |
| readyz / healthz / 前端首页 | 200 / 200 / 200；后端日志 `panic|fatal|level=error|success:false` 命中 **0** |

## 3. 验证证据（G13 契约与数据源）

平台管理员 `admin` 登录 8089（cookie 会话），对 `demo-med`（tenant **4**，6 用户，含 1 个活跃 admin）调用 `GET /api/v1/tenants/4/users`：

| 查询 | `total` | 当页活跃 admin | `activeAdminCount` |
|---|---|---|---|
| `page=1&pageSize=1` | 6 | **0** | **1** |
| `page=2&pageSize=1` | 6 | 0 | **1** |
| `page=1&pageSize=1&role=end_user` | 1 | 0 | **1** |
| `page=1&pageSize=20` | 6 | 1 | **1** |

结论：计数**不随筛选与分页收敛**，并与当页可见 admin 数解耦 —— 这正是修复前会误判的组合（旧前端在第一行会算出 0）。部署前同一端点响应中 `activeAdminCount` 出现次数为 **0**（字段不存在），构成前后对照。

前端：`docker exec itsm-frontend-demo grep -rl activeAdminCount /usr/share/nginx/html` → `static/index-DtaaMdLQ.js`（docroot 为 `/usr/share/nginx/html/static`，非 `assets`）。

审计面：后端日志确认治理读通道仍按 `target_tenant_id=4` 落审计（`user_id=1`、`username=admin`、`status_code=200`）。

## 4. 新发现：关闭态写通道行为（补 review 残项）

对 `demo.med.admin`（user **11**）发起 `PUT /tenants/4/users/11/status {"active":false}`：

1. 不带 CSRF → `{"code":4031,"message":"CSRF token missing"}`；
2. 带 CSRF → `{"code":"TENANT_USER_ADMIN_DISABLED","message":"平台侧用户治理未启用"}`，随后复核账号仍 `active=true`（**零副作用**）。

`TENANT_USER_ADMIN_ENABLED` 在 `.env.demo` 与 `docker-compose.demo.yml` 中**均未出现** → 取默认关（`itsm-backend/service/tenant_user_admin.go:120`），与 TUM-D3 / plan v0.12「保持生产默认关」一致。

含义：

- review 残项「未验证 `TENANT_USER_ADMIN_ENABLED` 关闭态下抽屉的写操作表现」**就此关闭**：关闭态返回稳定错误码 + 中文文案，前端映射存在（`TENANT_USER_ADMIN_DISABLED`）。
- 因写通道关闭，`409 LAST_ADMIN_PROTECTED` **无法在本实例实测**；要实测须显式开启开关，属配置变更，未擅自执行（见 §6 残项）。

## 5. 部署踩坑（复现须知）

| 坑 | 现象 | 正解 |
|---|---|---|
| demo compose **无 `build:` 段** | `docker compose -f docker-compose.demo.yml build` 输出 `No services to build` 且 **rc=0（假装成功）**；随后 `up -d` 去 registry 拉不存在的 tag 而 registry timeout | 镜像由 **prod compose 的 build 配方**产出（backend `itsm-backend/Dockerfile.prod`、frontend `Dockerfile` target=`production`），demo 栈只消费 `${VERSION}` 标签 |
| service 名带后缀 | `no such service: itsm-backend` | demo 栈 service 名为 `itsm-backend-demo`/`itsm-frontend-demo`/`itsm-worker-demo`；prod 才是 `itsm-backend`/`itsm-frontend` |
| snap docker 看不到宿主 `/tmp` | `couldn't find env file: /tmp/env.tum.build` | 即 [`07:G10`](../../07-known-gaps.md)，实测再次命中；暂存文件须放 `$HOME` 非隐藏目录 |
| registry 抖动 | 单层 57 MB 拉取约 8 min；首轮 `up` 直接 timeout | 重试可过；`docker info` 已配 daocloud/1panel mirror，但 build 阶段仍可能回落 docker.io |
| 构建变量透传 | 用 `sudo env ... docker compose` 而非依赖宿主 env | `--env-file` 副本仅替换 `VERSION`，不改写 `.env.prod` |

## 6. 残项闭环结果（2026-11-06 执行测试）

| # | 残项 | 状态 | 证据 |
|---|---|---|---|
| R1 | `go test ./handlers/tenant/ ./router/` | ✅ **已闭环（容器化 Linux）** | 本机三次尝试均 link 期 `cannot allocate memory`；改为在 238 用 `golang:1.25.13-alpine` bind mount 源码 + CGO 跑：`ok itsm-backend/handlers/tenant 0.145s`、`ok itsm-backend/router 0.271s`、`ok itsm-backend/service 4.814s`，`RUNNER_RC=0` |
| R2 | `409 LAST_ADMIN_PROTECTED` 线上实测 | ✅ **已闭环（临时开写通道实测，测毕复原）** | `PUT /tenants/4/users/11/status {"active":false}` → `{"code":"LAST_ADMIN_PROTECTED","message":"该用户是租户最后一个可用管理员，不能停用"}`，复核 `active=true`（零副作用）；启停正路径对 `demo.med.user`(14) 关闭→再启用均 `code:0`，终态 `active=true`；`activeAdminCount` 全程稳定为 1 |
| R2b | membership 撤销面错误码（G12/G14 关联） | ✅ **已实测** | 先 DB 只读预检（user 11 `home_tenant=4`、membership `tenant=4,status=active`）确认必触发 home 不可撤，再 `POST .../membership/revoke` → **HTTP 409** `HOME_MEMBERSHIP_IMMUTABLE`「home 映射不可撤销（账号级语义，请使用账号停用）」；DB 复核 `tenant_id=4` 撤销行数 **0** → 本批新映射的前端文案对应的正是服务端真实行为 |
| R3 | 浏览器 UI 点测 | ✅ **已闭环（ego-browser）** | 抽屉「共 6 个用户」，6 行数据、6 个开关，**仅 `demo.med.admin`（周敏·医疗-管理员）`disabled=true`**，其余 5 个 enabled；应用自身请求 `GET /api/v1/tenants/4/users?page=1&pageSize=20` → **200 / total 6**；产物 `static/index-DtaaMdLQ.js` 内 `仅剩这一个活跃管理员` ×1、`activeAdminCount` ×2、`HOME_MEMBERSHIP_IMMUTABLE` ×1 |
| R4 | 多管理员租户的假性禁用现场复现 | ⏸ **环境不可得，保持开放** | 六个租户 `activeAdminCount` 实测分布 `1:0 / 2:0 / 3:1 / 4:1 / 5:1 / 6:1` —— **无任一租户 ≥2 活跃 admin**，旧缺陷形态（≥2 且跨页）在演示数据里不可自然出现。不为验证而往演示库新建管理员（无删除通道，TUM-D8）；该形态由单元用例覆盖（后端「筛选/分页不收敛计数」+ 前端「`activeAdminCount=2` 仍可停用」）|

### R3 过程中的两次误判（留此以免重复踩）

1. 首轮把「抽屉 `共 0 个用户`、0 行」读成上线回归 —— 实为**等待条件错误**：antd v5 表格恒含
   `tr.ant-table-measure-row`，`querySelectorAll('table tbody tr').length > 0` 在数据到达前即为真。
   排除 measure-row 后同一入口稳定为 6 行 / HTTP 200。**应用无此问题。**
2. 次轮 Tooltip 断言失败亦属脚本问题：抽屉表格有横向滚动（`scroll.x`），`getBoundingClientRect()` 得到
   `x=2036` 超出视口宽 1480，hover 落空。该文案改由产物 bundle 字符串核验（见上表 R3）。

### 复原确认

测试后写通道已恢复关闭：容器内 `TENANT_USER_ADMIN_ENABLED=[unset]`、`.env.demo` 无该变量、override 文件已删除；
再次 `PUT status` 复现 `{"code":"TENANT_USER_ADMIN_DISABLED"}`，读通道仍正常（`total=6 / activeAdminCount=1`）。
238 仓库目录内临时 `itsm-backend/Dockerfile.qodertest` 已删除，host tracked 树干净。

## 7. 回滚

```bash
# 238 上执行（回到 patch 前镜像与 env）
cd ~/itsm/repo
git revert --no-edit 057b38132          # 或 git reset --hard 6cf692f45（host 无他人后续 commit 时）
cp .env.demo.bak-pre-tum-20261106-104832Z .env.demo
# 重建旧 tag（或直接复用仍存在的旧镜像，无需重 build）
echo <PW> | sudo -S docker compose -f docker-compose.demo.yml --env-file .env.demo \
  up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo
curl -s http://127.0.0.1:8089/api/v1/version   # 期望 1.8.0-dev.6cf692f45
```

旧镜像 `itsm-backend:1.8.0-dev.6cf692f45` / `itsm-frontend:1.8.0-dev.10635e36` 仍在本地镜像库，回滚**无需**重新构建（秒级）。

## 8. 治理影响

- `07:G12`、`07:G13`：代码已合并（本地 `1b017498`）并在 238 演示栈生效，可判定**线上生效**；`07:G16`（Swagger 注解缺失）不受本次部署影响，仍待办。
- `07:G14`、`07:G15`：本次未涉及，维持原状态（待决策 / 维持不做）。
- 新增事实建议进文档（尚未做）：**demo 栈镜像由 prod compose 构建**这一隐含依赖在 `02-deployment-and-configuration.md` 与 `docker-compose.demo.yml` 注释中均未记载，是 §5 首个坑的根因。
