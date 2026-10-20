# `/ai/chat` 窄屏响应式布局证据（2026-10-20）

- 提交：`0e2ba65f`
- 部署：`1.8.0-dev.0e2ba65f` @ `172.18.3.238:8089`（frontend healthy，`/api/v1/version` 对齐）
- 产物指纹：`/usr/share/nginx/html/static/index-70nqmNpj.js`

## 1. 改前问题（真机 tbK1）

| 视口 | 侧栏 | 消息区宽度 | 头部溢出 |
|---|---|---|---|
| 1440 | 并排 256px | 871px | 0 |
| 1180 | 并排 256px | 611px | 0 |
| 1024 | 并排 256px | **455px** | 0 |
| 900 | 并排 256px | **331px** | 0 |
| 768 | 并排 256px | **199px** | **94px（选择器被裁切）** |

结论：侧栏在窄屏下仍占据 flex 宽度，把消息区压到不可用；头部两个选择器（Bot 140px + Provider ≥160px）在 768px 下溢出。

## 2. 改造

### 2.1 侧栏抽屉化（<1100px）

| 状态 | 宽屏（≥1100px） | 窄屏（<1100px） |
|---|---|---|
| 侧栏定位 | `static`（并排 flex） | `absolute`（浮层，`z-index: 30`） |
| 展开时主区宽度 | 被挤压 | **不变**（浮层覆盖） |
| 默认状态 | 展开 | 收起 |
| 关闭方式 | 点「收起侧栏」 | 点收起 / 点外部 / Esc |

实现要点：
- 仅在跨越断点时同步一次开合，用户手动切换后不被 `useMediaQuery` 反复覆写。
- 外部点击监听用**捕获阶段**（`mousedown`），避免被面板内组件 `stopPropagation` 吃掉。
- Esc 监听同样只在「窄屏 + 抽屉展开」时挂载，不影响宽屏。

### 2.2 头部折叠（<900px）

- Bot 选择器 + Provider 选择器收进 `模型与能力` Popover（`data-testid="ai-chat-model-settings-open"`）。
- 宽屏（≥900px）行为完全不变：两个选择器直接可见。
- 选择器 JSX 抽成 `botSelectorNode` / `providerSelectorNode` 变量复用，避免两套渲染漂移。

## 3. 验证

### 3.1 自动化

| 门禁 | 结果 |
|---|---|
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（AIChat / 新测试） | exit 0 |
| `jest src/components/ai --maxWorkers=2` | **43 suites / 302 tests 全过** |

新增 `AIChat.responsive.test.tsx` 7 例：
1. 窄屏挂载后自动收起侧栏；
2. 宽屏不强制收起；
3. 窄屏展开时侧栏 `position: absolute` + `z-index: 30`；
4. 点击浮层外部收起；
5. Esc 收起；
6. 窄屏（<900px）Bot/Provider 进弹层、头部不直接渲染选择器；
7. 宽屏 Bot 选择器直接可见、无折叠入口。

### 3.2 真机（tbK4）

| 视口 | 侧栏 | 消息区宽度 | 头部溢出 | 折叠入口 |
|---|---|---|---|---|
| 1440 | `static` 268px 可见 | 871px | 0 | 无（直接可见） |
| 1180 | `static` 268px 可见 | 611px | 0 | 无（直接可见） |
| 1024 | `absolute` 收起 | **735px**（改前 455） | 0 | — |
| 900 | `absolute` 收起 | **611px**（改前 331） | 0 | — |
| 768 | `absolute` 收起 | **479px**（改前 199） | **0**（改前 94） | ✅ 模型与能力 |

交互验证（1024px）：

```json
{
  "initialDrawerVisible": false,
  "initialMessageWidth": 735,
  "afterExpandVisible": true,
  "afterExpandWidth": 268,
  "afterExpandPosition": "absolute",
  "afterExpandZ": "30",
  "afterExpandMessageWidth": 735,
  "messageWidthUnchanged": true,
  "closedByOutsideClick": true,
  "reopened": true,
  "closedByEsc": true
}
```

768px 模型与能力弹层：

```json
{
  "modelBtn768": 1,
  "modelPopoverVisible": true,
  "botInPopover": 1,
  "headerOverflow768": 0
}
```

宽屏回归：`widePosition = "static"`、`wideVisible = true` —— 并排布局未被破坏。

## 4. 收益汇总

| 视口 | 消息区改前 | 消息区改后 | 提升 |
|---|---|---|---|
| 1024 | 455px | 735px | **+61%** |
| 900 | 331px | 611px | **+85%** |
| 768 | 199px | 479px | **+141%** |

## 5. 已知非本次问题

- `POST /ai/telemetry/events` 访客态 403：既有权限行为。
- 窄屏抽屉未加遮罩层（scrim）：当前靠阴影 + 外部点击关闭，视觉层级已可辨；
  如需更强的模态感可后续补半透明遮罩。

## 6. 未纳入

- 侧栏宽度在宽屏仍固定 268px，未做拖拽调宽（Dock 已有离散三档先例，可作为后续一致性项）。
- 768px 以下`AI 助手`标题与上下文 Tag 未进一步收纳（实测未溢出，暂不需要）。
