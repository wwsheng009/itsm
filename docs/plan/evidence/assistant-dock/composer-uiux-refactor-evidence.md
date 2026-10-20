# Dock composer UI/UX 重构证据（2026-10-20）

- 提交：`56d9c771`（composer 重构）、`e0e55200`（Esc 分层修复）
- 部署：`1.8.0-dev.e0e55200` @ `172.18.3.238:8089`（frontend healthy）
- 产物指纹：`/usr/share/nginx/html/static/index-CXAxwL7C.js` 命中 `assistant-composer`

## 1. 问题（用户实测反馈）

1. **按钮过载**：静止态输入区共 9 个按钮——页签行 3（历史/工作区/诊断）+ 记忆条 3（管理/存常用语/存模板）+ 附件 1 + 输入坞 2（精算/发送）。
2. **附件独占一行**：`ChatAttachmentPicker` 自带 `<Space direction="vertical">`，即使零附件也占据一整行垂直空间，把输入框往下推。
3. **主次不分**：低频动作（存为常用语/模板）与高频动作（发送）同为常驻按钮，视觉重心落在按钮排而非输入内容。

## 2. 改造

| 项 | 改前 | 改后 |
|---|---|---|
| 输入区结构 | 附件行 + 输入条 + 记忆条（3 块并置） | 单一 composer 容器（文本域 + 24px 工具条） |
| composer 内按钮 | 3（附件/精算/发送） | 3~4（附件/记忆/精算/发送），全部图标化 |
| 记忆常驻按钮 | 3 | 1（chip、保存动作进单层弹层） |
| 附件位置 | 独立整行 | 工具条内，与输入框同容器（真机间距 2px） |
| 附件芯片 | 与"附件"文字按钮挤在一行 | 文本域上方只读 chip，可单个移除，零附件时不渲染 |
| 焦点反馈 | 无 | 聚焦时主色描边 + 三级阴影；静止态次色细描边 |
| 流式中附件 | 未禁用（依赖调用方传 disabled，易漏） | 组件内统一由 `streaming` 锁定 |
| Esc | 浮层打开时误关整个面板 | 分层：先收浮层，无浮层才收面板 |

## 3. 代码落点

- 新增 `itsm-frontend/src/components/ai/dock/AssistantComposer.tsx`：单容器 composer。
- 新增 `itsm-frontend/src/components/ai/chat-core/use-chat-attachments.ts`：上传链路 hook（Dock/工作区共用）。
- `ChatAttachmentPicker.tsx`：改为该 hook 的自包含视图，对外 props 契约不变（工作区零影响）。
- `AssistantMemoryChips.tsx`：新增 `layout='toolbar'`；弹层内 chip/保存/管理单层切换，选中 chip 自动收起。
- `AssistantChatPanel.tsx`：输入区替换为 composer；移除独立记忆条与附件行。
- `AssistantDock.tsx`：Esc 分层判断 Ant 浮层。
- `translations-assistant.ts`：新增 `assistant.attachment.remove`（zh/en）。

## 4. 验证

### 4.1 自动化

| 门禁 | 结果 |
|---|---|
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（5 个改动文件） | exit 0 |
| `jest src/components/ai/dock src/components/ai/chat-core` | 12 suites / 127 tests 全过 |
| `jest src/components/ai/dock + chat-core + a11y-surfaces` | 14 suites / 141 tests 全过 |
| `jest src/components/ai src/lib/ai` | **60 suites / 426 tests 全过** |

新增测试：
- `AssistantComposer.test.tsx` 7 例：单容器、静止态按钮数、芯片移除、附件禁用、流式中停止、错误就地、键盘 Enter。
- `AssistantDock.test.tsx` +1 例：Esc 分层（浮层打开时保持面板）。

### 4.2 真机（Playwright / Tabbit，`tbI3`）

```json
{
  "composerButtons": 3,
  "buttonLabels": ["附件", "后台精算", "发送"],
  "attachGapBelowInput": 2,
  "attachVsSendSameRow": true,
  "emptyChipContainer": 0,
  "popoverOpen": true,
  "popoverClosedAfterEsc": true,
  "panelAliveAfterEsc": true,
  "inputAliveAfterEsc": true,
  "sendDisabledEmpty": true,
  "sendEnabledTyped": true,
  "asyncEnabledTyped": true,
  "stopVisibleStreaming": 1,
  "attachDisabledStreaming": true,
  "bannerState": "streaming",
  "sendBackAfterDone": 1,
  "panelAliveAtEnd": true
}
```

说明：本轮租户个性化记忆处于关闭态，故 composer 只渲染 3 个按钮（记忆入口按设计隐藏）；
`tbI1` 在记忆开启态实测为 4 个按钮（附件/记忆管理/后台精算/发送）。

### 4.3 已知非本次问题

- `POST /ai/telemetry/events` 返回 403：该租户埋点未开通，属既有权限状态，不影响聊天与 composer 功能。
- `GET /auth/me` 401：访客态首次探测，登录后消失。

## 5. 未纳入本次改动

- 面板头部仍为 3 个按钮（历史/工作区/诊断）。诊断入口已有独立抽屉与既有测试契约，
  收进历史弹层会改变其可发现性，留作后续单独评估。
- 工作区 `/ai/chat` 的输入坞未改（仍用原 `ChatAttachmentPicker` 自包含形态），
  避免同一批次改动两个表面；composer 组件已按可复用方式抽取，后续可平移。
