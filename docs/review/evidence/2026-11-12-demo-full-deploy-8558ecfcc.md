# 2026-11-12 238 全量部署证据：membership 身份前端展示

> 版本：`1.8.0-dev.8558ecfcc`
> 提交：`8558ecfcc41cbb6d3b6b40509800b522ebea548d`
> 分支：`deploy/full-8558ecfcc`
> 部署时间：2026-11-12T04:07:37Z
> 关联：[Phase 4 第二批验证报告](../2026-11-12-membership-business-actors.md)

## 1. 运行态

```text
URL              http://172.18.3.238:8089
version          1.8.0-dev.8558ecfcc
runtime commit   8558ecfcc41cbb6d3b6b40509800b522ebea548d
readyz           {"ready":true,"schemaVersion":"20261112_membership_constraints_validate"}
demo containers  6/6 healthy
```

前序版本：`1.8.0-dev.cb55a4e69`。
本版无新增迁移；21/21 membership 复合外键保持 VALIDATED，6 类业务引用
missing=0、跨租户错配=0（problems 209、changes 14、service_requests 135）。

## 2. 备份与回滚

```text
/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.8558ecfcc-20261112T035807Z
├── .env.demo.before
├── itsm_demo.dump
├── itsm_demo.dump.sha256
├── rollback.txt
├── release.txt
└── smoke-attachment.json
```

## 3. 本版变更

### 前端

- 新增共享 `MembershipBadge`（`src/components/common/MembershipBadge.tsx`）：
  展示「租户身份 #N」，tooltip 说明 C 方案 membership 语义；
- 评论作者：`CommentItemBody` / `CommentPanel` / MSP 工作台只读评论；
- 附件上传人：`AttachmentPanel` + `ticket-attachment-adapter`；
- 审批人：`ApprovalTimeline` + `ApprovalWorkflowPanel`；
- 通知接收人：`NotificationDrawer` + 通知中心列表；
- 新增 i18n：`detailTabs.membershipIdentity` / `membershipIdentityTooltip`（zh/en）。

### 后端

- `ApprovalRecordResponse` 新增 `approverMembershipId`，审批记录响应补齐 membership 字段。

## 4. UI 实际验证

新增 `tests/e2e/demo/07-membership-identity.spec.ts`，在真实浏览器中验证：

```text
✓ 工单评论展示作者 membership 身份（ticket 71，membership 4）
✓ provider 客户租户评论展示客户租户 membership（新建工单 + provider 评论，membership 22）
✓ 附件上传人展示 membership 身份（ticket 61，membership 4）
✓ 通知接收人展示 membership 身份（mfgAdmin 通知列表）
4 passed
```

provider 评论用例为真实业务操作：

1. `demo.mfg.admin` 在客户租户创建工单；
2. `demo.msp.manager` 深度切换到客户租户并发表评论；
3. 客户管理员打开工单详情，看到 `membership-badge-22`（provider 在客户租户内的身份）；
4. 清理工单并返回 provider 家租户。

## 5. 全量 E2E

```text
00-smoke                    6 passed
01-permissions              8 passed
02-associations             8 passed
03-workflows                8 passed
04-ui-ux                   11 passed
05-multi-tenant-business    4 passed
06-attachment-access        1 passed
07-membership-identity      4 passed
合计                       50/50
```

## 6. 检查与门禁

```text
npx tsc --noEmit                          PASS
npm run build                             PASS
npx eslint（改动文件）                     PASS
go build ./dto ./service                  PASS
go test ./service -run ApprovalService     PASS
Jest：CommentItemBody / ApprovalTimeline   PASS
```

## 7. 发布后观察

- 发布后 backend 日志无 `level=error` / panic / fatal；
- 无 5xx 标记；
- 45 分钟完整观察窗待补记。

## 8. 仍未完成

- 变更审批（`change_approvals`）与服务请求审批（`service_request_approvals`）尚未增加
  membership 列，审批身份展示仅覆盖工单审批记录；
- 全量 17 表行为矩阵、BPMN `ccType=group/role`、home tenant 无 membership 兼容窗口收口。
