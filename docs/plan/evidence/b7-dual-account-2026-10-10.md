# B7 浏览器双账号实测留痕（2026-10-10）

> 承接《剩余缺口治理方案》B7 与《前端权限门禁全站清单》§7 模板第 5 项。
> 方式：本地 Playwright（`playwright-core` + ms-playwright Chromium）脚本化实测，
> 两个隔离浏览器上下文分别登录正/反例账号，逐项采集 URL / 主内容区文案 / 按钮集 + 截图。
> 脚本：`.tmp/web-check/b7-dual-account.cjs`、`.tmp/web-check/b7-supplement.cjs`；
> 原始证据：`.tmp/web-check/b7/b7-result.json`、`b7-supplement.json` + 24 张截图（均为未跟踪临时产物）。

## 账号

| 角色 | 账号 | 租户 | 关键权限画像 |
|---|---|---|---|
| 正例 | `admin_demo_customer` | 11（demo_customer） | admin：`ai:write`/`knowledge:write`/`notification:write`/`workflow:write`/`bpmn:write`/`mcp:admin`/`sla:write`/`user:write` 等 |
| 反例 | `b7_probe_tech`（探针，用后即删） | 11 | technician：仅 `ticket:create/read/update/write`、`incident:read/write`、`knowledge:read`、`notification:read`、`ai:read`、`service_request:write`、`task:update`；**无** 各治理写码 |

## 结果矩阵

| # | 项 | 正例（admin_demo_customer） | 反例（b7_probe_tech） | 判定 |
|---|---|---|---|---|
| 1 | B6 admin 区 · `/admin/sla-definitions` | 页面正常，「新建SLA」可见 | **403 页**（返回首页），侧栏无「管理功能」 | ✅ |
| 2 | B6 admin 区 · `/admin/bots` | 页面正常（含 `bot.enabled=false` 未启用提示） | **403 页** | ✅ |
| 3 | 工作流版本 · `/workflow/versions` | 页面正常（导出版本入口）；侧栏「版本管理」可达 | 页面可读，无写入口；侧栏无管理区 | ✅（0 版本数据，行级激活/回滚未实测） |
| 4 | 知识审核 · `/knowledge/reviews` | 页面正常（审核列表/刷新） | **直达 → `/403`**（路由级 `knowledge:write`） | ✅ |
| 5 | 通知中心 · `/notifications` | 全部标记已读/清空通知/标记已读/删除 + 偏好「重置为默认/保存设置」 | 仅「刷新」；偏好无保存/重置 | ✅（见观察③） |
| 6 | AI 审批 · `/ai/approval` | 「工具治理」可见；队列空 | 无「工具治理」；队列空 | ⚠️ 部分（见观察①） |
| 7 | AIChat · `/ai/chat` | 「保存成文章」+「清空对话」 | 仅「清空对话」（无 `knowledge:write`） | ✅ |
| 8 | 问题关联 · `/problems/13` | 编辑 / 启动 RCA / 开始处理 / 添加工单 / 添加事件 / 添加变更 | 「未找到问题」（无 `problem:read`）+ 无任何写入口 | ✅ |
| 9 | 审批面板 · `/tickets/224` | 分配/编辑/抄送/删除；「您是此工单的申请人，无法进行审批操作」 | 仅「编辑」（`ticket:update`）；分配/抄送/删除隐藏 | ✅（见观察②） |
| 10 | profile · `/profile` | 「保存修改」可用；部门只读（管理员维护） | **「保存修改」可用**（B3 自助面；低权用户不再被 `user:write` 拦截） | ✅ |
| 11 | Header 通知抽屉 | 「全部已读」+ 5 条未读列表 | 「暂无通知」，无「全部已读」 | ✅ |

## 观察项（非权限缺口，登记备查）

1. **AI 审批队列无 pending 数据**：行级「通过/驳回」按钮未在本次实测中出现；该门禁为 `ai:write`（`pages/(main)/ai/approval/index.tsx:156`），沿用批次 5 的 pending 单实测证据（admin 显示通过/驳回、低权显示「无审批权限」）。
2. **工单 224 正例为申请人身份**：「批准/拒绝」因申请人规则隐藏（页面明示），非权限问题；`workflow:write` 门禁沿用批次 3 实测证据（admin 可见、user01 隐藏）。
3. **无 `notification:write` 用户的偏好开关可点但不可持久化**：`启用/停用` 为本地状态，页面不渲染「保存设置」入口，无 API 写入面；建议后续将开关按 `notification:write` 置 disabled（体验项，非安全缺口）。

## 清理

- 探针问题 `#13`（PRB-20261010-0013）已删除（`problems_total=0` 复核）；
- 探针用户 `b7_probe_tech`（id=86）已删除（登录复核 `code=2001` 凭据失效）；
- 临时 Cookie/截图仅存于 `.tmp`（未跟踪）。
