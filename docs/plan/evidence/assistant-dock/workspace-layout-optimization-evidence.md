# `/ai/chat` 工作区布局优化证据（2026-10-20）

- 提交：`77d9c3c7`（composer 复用 + 头部降噪）、`6811f488`（空态宽度对齐）、`1eec3f3d`（空态禁用清空）
- 部署：`1.8.0-dev.1eec3f3d` @ `172.18.3.238:8089`（frontend healthy，`/api/v1/version` 对齐）
- 产物指纹：`/usr/share/nginx/html/static/index-DoLQZHeo.js`

## 1. 体检结论（改前真机 tbJ1/tbJ2）

用户提出「工作区布局还有没有优化空间」后，先用 Playwright 量化，发现 4 处具体问题：

| # | 问题 | 实测证据 |
|---|---|---|
| 1 | 附件选择器独占输入框上方一整行 | `attachGapBelowInput = -81`（y 坐标在输入框上方 81px）；且左边缘比输入框偏 15px |
| 2 | 头部工具条在 1024px 视口溢出 | `headerOverflow = 17`（scrollWidth 超出 clientWidth） |
| 3 | 消息列与输入坞宽度不同源 | 消息列 720px、输入坞 820px，**相差 100px** |
| 4 | 空态与输入坞宽度不一致 | 空态容器硬编码 720，输入坞 820 |

问题根源：
- 工作区仍用旧版 `ChatAttachmentPicker`（`<Space direction="vertical">` 自包含形态，按钮 + 芯片 + 计数各占一行）。
- 头部把「保存成文章」「清空对话」两个**文字按钮**常驻，整段会话级低频操作占了 ~180px。
- `maxWidth: 720` 与 `CONTENT_MAX_WIDTH = 820` 两处硬编码并存。

## 2. 改造

### 2.1 composer 复用（消除 Dock / 工作区分叉）

`AssistantComposer` 新增形态参数，保持 Dock 默认行为不变：

| 参数 | 默认（Dock） | 工作区取值 |
|---|---|---|
| `testId` | `assistant-composer` | `assistant-workspace-composer` |
| `inputTestId` | `assistant-panel-input` | `assistant-chat-input` |
| `inputFontSize` | 13 | 14 |
| `maxRows` | 由 `widePanel` 推导（6/10） | 8 |
| `onStartAsync` | 必传 | **不传 → 不渲染后台精算按钮** |
| `extraActions` | — | 预留（本次未用） |

工作区输入区因此变成与 Dock 一致的单一容器：文本域 + 24px 工具条（附件 / 发送）。

### 2.2 头部降噪

- 「保存成文章」「清空对话」收进 `更多操作` Dropdown（`data-testid="ai-chat-more-actions"`）。
- 头部右侧从「Bot 选择器 + Provider 选择器 + 保存成文章 + 诊断 + 清空对话」缩为「Bot/Provider + 诊断图标 + 更多图标」。
- 空态下「清空对话」禁用（`streaming || isEmpty`）。

### 2.3 宽度同源

- 空态容器 `maxWidth: 720` → `CONTENT_MAX_WIDTH`（820）。

## 3. 验证

### 3.1 自动化

| 门禁 | 结果 |
|---|---|
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（AIChat / AssistantComposer） | exit 0 |
| `jest src/components/ai --maxWorkers=2` | **42 suites / 295 tests 全过** |
| `jest src/components/ai/dock/__tests__/AssistantComposer.test.tsx` | 8 tests 全过（含新增工作区形态 1 例） |

> 注：默认 `--maxWorkers` 在本机偶发 jest worker 被 SIGTERM（测试用例全绿但套件报失败），
> 用 `--maxWorkers=2` 稳定复跑为 42/42。

### 3.2 真机（Playwright）

改前 → 改后：

| 指标 | 改前（tbJ1/tbJ2） | 改后（tbJ5） |
|---|---|---|
| 头部按钮数 | 3 文字 + 1 图标 | **2 图标**（诊断 / 更多） |
| 头部溢出（1024px） | **17px** | **0** |
| 附件位置 | 输入框**上方** −81px（独占一行） | 输入框**下方** +2px（同容器） |
| 附件左偏 | 15px | 6px |
| 附件与发送同排 | — | ✅ `true` |
| 消息列 / 输入坞宽度 | 720 / 820（差 100） | **820 / 819（差 1）** |
| 空态宽度 | 720 | **820** |
| 空态「清空对话」 | 可点击（无意义） | **禁用** |
| 页面横向溢出 | 0 | 0 |
| console errors | 0 | 0 |

```json
{
  "version": "1.8.0-dev.1eec3f3d",
  "headerButtons": 2,
  "headerLabels": ["会话诊断", "更多操作"],
  "headerOverflow": 0,
  "composerButtons": 2,
  "composerLabels": ["附件", "发送"],
  "attachGapBelowInput": 2,
  "emptyStateWidth": 820,
  "inputWidth": 819,
  "alignmentDelta": -1,
  "menuItems": ["保存成文章", "清空对话"],
  "clearDisabledOnEmpty": true,
  "consoleErrors": []
}
```

## 4. 已知非本次问题

- `POST /ai/chat/stream` 与 `POST /ai/telemetry/events` 在访客态返回 403：访客未登录，属既有权限行为；
  登录后正常（Dock 侧此前已用登录态验证过真实流式）。
- 工作区未接入 `AssistantMemoryChips`（Dock 专属），故 composer 工具条只有 2 个按钮；
  如需与 Dock 完全一致，可后续按 `tools` 参数注入。

## 5. 未纳入本次改动

- 侧栏（268px）在 1024px 视口下与主区争宽，仍可进一步做成抽屉式；本次未动，避免扩大改动面。
- 头部 Bot 选择器与 Provider 选择器在小视口下仍占约 300px，后续可合并为一个「模型/能力」弹层。
