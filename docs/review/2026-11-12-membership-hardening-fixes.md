# 多租户成员生命周期修复记录（2026-11-12）

> Status: current — 本轮修复与本地定向验证完成，未部署；非发布放行报告。

## 范围与环境

- 审查基线：`feat/msp-p0-hardening` / `16c5c453c`。修复期间并行工作将 HEAD 推进到
  `2d444c6b3`（E2E 导航稳定性），已保留；本记录对应其上的未提交工作树修复。
- 环境：Windows / Go，单包串行验证，`GOMAXPROCS=2 GOMEMLIMIT=2GiB`、`go test -p 1`。
  router 最终重跑与全包构建额外设置 `GOGC=50`，控制本机链接阶段内存压力。
- 数据库：本地 Ent/SQLite 回归；PostgreSQL 迁移验证结果单独记录，不以 SQLite 替代。
- 部署模式/镜像 digest：不适用；本轮未构建发布镜像、未部署、未调用生产 API。
- 保留进入修复前已有的 CHANGELOG、审查/计划文档及前端 E2E 改动，不覆盖并行工作。

## 不变量

1. 当前租户成员资格先于权限码和历史 owner/assignee 判断，失效关系不被 JWT、缓存或 home tenant 回退重新授权。
2. 账号主角色、目标租户附加角色、membership 角色同步提交；事务失败全部回滚。
3. 业务参与人 `user_id` 与 `membership_id` 指向同一租户内同一账号；改派/清空必须同步双列。
4. 新分配/投递要求全局账号启用及当前成员资格；历史失效身份仍可追溯，不阻断其他合法用户维护单据。
5. 回填不恢复撤权；已执行 SQL 的原始 checksum 不修改；不能无依据删除疑似被旧迁移恢复的关系。

## 已实施：请求期身份与用户降权

生产入口：`router/router.go` 的 AuthMiddleware → RBACMiddleware → 各路由权限守卫；
`router/common_system_routes.go` 的 `PUT /users/:id` → `handlers/user.UpdateUser` → `service.UserService.UpdateUser`。

- RBAC 每次请求校验 active、未删除、未到期 membership；异常 fail closed。
- 当前 membership 角色替代 JWT 内旧主角色，附加角色仍按启用且同租户过滤。
- membership-less 例外仅保留数据库核验的平台根身份/显式平台切换，以及显式兼容开关下从未建立成员关系的 home 账号。
- 权限缓存命中前重查身份有效性，并绑定账号、membership 和角色的身份版本。
- 用户更新事务包含 users、目标租户 user_roles 和现存 membership.role_id；变更后清理权限缓存并吊销 access token。
- 主角色单独变更时替换旧主角色镜像边；`roleIds=[]` 仅清目标租户边；不创建/恢复 membership，不改变状态或到期时间。

## 已实施：安全迁移与历史 actor 补齐

生产入口：bootstrap 的 filesystem discovery → `migration.Migrator.RunMigrations` →
`reconcileSupersededMigrations` → 正常版本排序与 checksum 校验。

- 未执行的两份不安全历史版本被 runner 收养，不先执行再删除疑似恢复关系。
- `20261112_membership_backfill_safe.sql` 检查完整成员历史；已撤销、已暂停、已存在的
  grant 均不修改。角色采用当前 provider/首管词表，缺失同租户角色时不替换为 admin。
- 新 safe 版本包含被替代版本的 DDL，排序早于 reconcile / constraints_validate。
  `20261112z_membership_actor_references_reconcile.sql` 在旧迁移流之后补齐 actor 空引用。
- 只补同租户 NULL 引用；非 NULL 历史引用不覆盖。对存量可疑关系输出人工核验信号，
  不据 `source=migration` 自动撤权；原 SQL 字节及已记录 checksum 保留。
- 两份新迁移均 forward-only。**真实 PostgreSQL 四种升级/回滚场景尚未执行**；
  静态排序与校验和回归通过，不等同于实际 DDL/DML 验收。

## 已实施：工单身份一致性

生产入口：`SetupTicketRoutes` → `handlers/ticket` / `handlers/ticket_workflow` →
现有 service / ticket repository；子工单复用普通创建事务及 outbox，不另建创建规则。

- 分配、PATCH 改派、接单、转发接管更新 `assignee_id` 与对应 membership；
  repository 明确清空时同步清双列，不沿用上一处理人的身份。
- 工单及子工单 HTTP 更新补齐可选字段语义：`assigneeId` 未传/`null` 保留、`0` 清空、
  正整数校验后分配、负数拒绝；同样覆盖现有 TicketService/CoreService 入口。
  AI 直接更新与审批后执行共用参数映射，不再把“未传”变成清空。
  前端 API 契约回归锁定零值透传和未传保留，旧版本冲突不得重写已清空身份。
- 全局停用账号、撤销/暂停/到期关系不可重新分配；legacy home 回退只允许从未建立
  该租户 membership 的账号，不能通过软删历史重新获得资格。
- 子工单校验父工单租户及创建人、申请人、处理人身份，保留 provider 代客户创建语义；
  outbox 失败回滚子工单。补齐响应的 `parentTicketId` 映射。
- CC 列表/删除先核验当前身份，再判断历史 owner/assignee 或自我移除资格。
  `/tickets/cc/my` 与其余 CC 读面统一使用 `ticket:read`，生成预检已重新生成。

## 已实施：通知与历史处理人

生产通知入口：`internal/bootstrap/app.go` 注册 `CommandDeliverNotification` →
`commandbus.Worker` → `NotificationDeliveryCommandHandler.Handle`。
问题/变更入口保持现有 Router → 领域 Handler/Service → 领域 repository。

- 通知消费/重试验证 command 租户、全局账号启用状态及目标租户有效 membership，
  不再把 `users.tenant_id`（home）当成唯一投递范围；允许 provider 的有效客户身份。
- 所有读取/写入绑定 command 租户，不使用 system bypass；忽略 payload 自报 membership/
  tenant。数据库查询失败不回落放行；已成功 delivery 的重放仍无副作用。
- 覆盖站内与连接器渠道：撤销、暂停、到期、全局停用及跨租户接收人拒绝；
  首次外部发送失败后撤权，Worker 重试不再调用 connector，最终进入 dead-letter。
- 问题/变更仅在处理人发生变化时重新解析资格。其他维护保留原历史双列，避免撤权后
  无法维护或把历史 membership 换成今天的新身份；清空时同步清双列。
- repository 写入同时校验原处理人双列，避免其读写期间并发换人被静默覆盖；
  **这不是问题/变更全流程的客户端 version 乐观锁保证**。

## 验证记录

1. `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./middleware -run 'Test(RBAC|ResolvePermissions|Membership)' -count=1 -timeout=180s`
   - 第一批回归通过；使用修复前 RBAC overlay 可复现旧 token 在撤销/暂停/到期后仍被接受。
   - 后续完整 `go test -p 1 ./middleware -count=1 -timeout=180s` 通过（2.318s）。
     初跑发现 `/tickets/cc/my` 生成预检漂移，执行 `go run -p 1 ./cmd/authz-gen` 后复跑通过。
2. `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./service -run 'TestUser(Membership|Service_(UpdateUser|SyncUserRoles))' -count=1 -timeout=180s`
   - 最终用户降权与回滚定向回归通过（1.565s）。
3. `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./migration -count=1 -timeout=120s`
   - 通过（1.664s），包含 discovery 顺序、旧 SQL 不变、replacement 前提及 checksum 拒绝篡改。
4. `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -tags integration_migration -p 1 ./migration -run '^TestMembershipBackfill' -count=1 -v -timeout=120s`
   - 测试编译及静态用例通过；真实 PostgreSQL 的 direct upgrade、旧 identity 已执行、
     整个旧流已执行、失败回滚/重试四个场景 **SKIP**，缺少
     `ITSM_MEMBERSHIP_MIGRATION_TEST_DSN`。本机 Docker daemon 不可用且无 PostgreSQL 服务端工具。
5. `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./handlers/common/membershipref ./repository/ticket ./handlers/ticket ./handlers/ticket_workflow -count=1 -timeout=180s`
   - membershipref、ticket repository、ticket_workflow 全包通过。
   - 首跑 ticket Handler 的两个子工单正常场景失败：响应漏 `parentTicketId`。
     补齐 DTO 映射后，单独重跑 `./handlers/ticket` 全包通过（1.254s），未放宽断言。
6. 前端最终 `npm run type-check` 退出码 0；契约与 API 行为命令
   `npm run test:unit -- --runInBand --coverage=false --runTestsByPath src/lib/__tests__/api-contract.test.ts src/lib/api/__tests__/ticket-api.test.ts`
   通过，**2 suites / 65 tests**，路由 mismatch 为 0。包含处理人未传保留、
   显式 `0` 透传及子工单清空请求。`npm run lint:antd` **未通过**：全库既有
   `Space direction` 用法（如 `ChatAttachmentPicker.tsx`、`TenantUsersDrawer.tsx`）；
   本轮前端仅修改 API 测试文件，没有修改这些 UI 文件或新增该旧 API。
7. 文档门禁：
   - `make docs-gate` 无法启动：本机无 make。
   - `bash scripts/docs-gate/run-all.sh` 在 C.3 失效链接扫描超过 300 秒被终止，已输出
     CHANGELOG 中既有安装/API/部署文档旧链接，不宣称整体通过。
   - 独立 `bash scripts/docs-gate/check-doc-sync.sh --strict` 最终复跑退出码 0；
     日期新鲜度使用 BSD `date -j`，在本机 GNU date 下未执行该部分，不能据退出码
     声称完整日期校验通过。
8. 最终 DTO/业务包：
   `GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./dto ./handlers/common/membershipref ./repository/ticket ./handlers/ticket ./handlers/ticket_workflow ./handlers/problem ./handlers/change -count=1 -timeout=180s`
   - **7 包通过**。覆盖 HTTP/仓储改派与清空、跨租户/停用拒绝、子工单 outbox 回滚、
     问题/变更历史身份保持及写入期间身份并发变化。
9. 最终 service 定向回归：

   ```bash
   GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 ./service \
     -run 'Test(TicketService|TicketWorkflow|TicketCC|TicketUpdateAssigneePresence|ResolveCreateMemberships|CCTicket|UserMembership|UserService_(UpdateUser|SyncUserRoles)|UpdateTicketRequestFromArgs|ErrorCodeOf|TicketNotification|NotificationDelivery|NormalizeNotificationEventType|NotifyTicket|NotifySLA)' \
     -count=1 -timeout=180s
   ```

   - **通过（24.614s）**；包含生产通知 Worker 重试撤权、站内/连接器资格矩阵、
     查询故障 fail-closed、幂等及死信，TicketService/CoreService 的可选处理人契约，
     用户降权与角色同步事务回滚。没有执行完整 `./service` 全量套件。
10. Router 与其他身份业务入口：

    ```bash
    GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -p 1 \
      ./router ./handlers/auth ./handlers/user ./handlers/service_request ./handlers/msp \
      -count=1 -timeout=180s
    ```

    - auth（2.753s）、user（0.729s）、service_request（8.418s）、msp（2.316s）
      **4 包通过**；同次 router 链接因 Windows `VirtualAlloc errno=1455` 内存不足失败，
      整条命令退出码 1，不计作通过。
    - 此前 router 全包暴露 4 个旧夹具失败，已修正三个测试文件：
      `msp_a11_a12_e2e_test.go` 为 provider 建立 home membership，并通过生产
      `MembershipSyncService.SyncAllocationMembership` 同步客户分配身份；
      `router_association_routes_test.go` 为 end_user 建立只有 `ticket:read` 的真实成员；
      `tenant_user_admin_e2e_test.go` 为客户管理员负向 actor 建立有效客户身份，
      使断言到达平台权限边界，而不是提前被成员门禁拒绝。未放宽授权或拒绝断言。
    - 上条命令结束后，单独重跑：

      ```bash
      GOMAXPROCS=2 GOMEMLIMIT=2GiB GOGC=50 go test -p 1 ./router -count=1 -timeout=180s
      ```

      **router 全包通过（8.681s）**。只降低 GC 触发阈值控制内存，未跳过测试或 vet。
11. 后端全包构建：

    ```bash
    GOMAXPROCS=2 GOMEMLIMIT=2GiB GOGC=50 go build -p 1 ./...
    ```

    **退出码 0**。在 router 回归结束后单独运行；这是全包构建，不是全后端测试通过的证明。
12. 最终差异与格式检查：
    - `git diff --check` 退出码 0；40 个变更 Go 文件（含未跟踪的新测试文件）
      `gofmt -l` 无输出。
    - 已审查业务实现、生成权限预检、迁移及文档 diff；新增行命名扫描命中为内部上下文、
      既有角色/渠道词表、日志键和测试入参，没有新增 HTTP DTO snake_case 字段。
    - 最终核对 HEAD 仍为 `2d444c6b3`。保留其他未跟踪文件及并行修改，未提交、未推送。

## 结论与剩余发布门禁

- 本轮代码修复、相关包与 service 定向回归、前端 65 项契约/API 测试、类型检查及
  后端全包构建已完成。真实 Router/Handler/Worker 调用链见各节；测试不只覆盖孤立 helper。
- **升级兼容要求**：原先以 `assigneeId: 0` 表示“不更新”的调用方必须改为省略字段；
  `0` 现在明确表示取消分配。详见根目录 `UPGRADE.md`。
- **发布前仍须执行**：真实 PostgreSQL 四种升级/回滚重试场景、存量可疑成员关系人工核验、
  RLS enforce 下完整业务旅程及新版本生产环境 E2E。缺少测试数据库导致的 SKIP 不计为通过。
- 完整 `./service` / 全后端测试套件未执行；`lint:antd` 和完整文档门禁未通过或未完成，
  原因见验证记录。上述限制不被单项检查退出码 0 覆盖。
- 本轮未执行生产迁移、部署或发布镜像构建。不沿用历史 41/41 或 51/51 E2E 结果作为
  本轮版本证据，不能据本记录直接放行发布。
