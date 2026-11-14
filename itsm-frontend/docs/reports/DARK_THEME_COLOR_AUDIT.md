# 暗色主题配色干涉审计与修复方案

> 审计对象：`itsm-frontend`（AI-Native ITSM 前端）
> 审计页面：`http://172.18.3.238:8089/tickets/76`（工单详情，暗色模式）
> 分支：`feat/msp-p0-hardening`
> 日期：2026-11-13
> 结论：**存在 8 处背景色 × 主色调 × 控件色互相干涉的缺陷，其中 1 处为可读性阻断级（蓝字落蓝底 1.40:1）**
>
> **复核更新（2026-11-13 晚）**：首轮 8 处修复已全部落地并通过测试；第二轮全量扫描又发现
> **7 类跨页面暗色干涉**（Antd 容器层级、Layout 硬编码、全局排版、浮层反色、Tailwind pastel 工具类等），
> 已在 §10 补充修复与验证。
>
> **第三轮收尾（2026-11-13 深夜）**：§10.7 遗留项已处理完毕——亮色主色阶单一真源、暗色前景主色、
> SLA 危险色、暗色浮层令牌化、内联灰色文字、未引用死代码，详见 §11。
>
> **第四轮（2026-11-13 深夜）**：新增语义前景色令牌，迁移暗色下对比度不足的深紫/深红/深绿/灰色内联文字，
> 深底对比度提升到 5.3~8.8:1，详见 §12。
>
> **第五轮（2026-11-13 深夜）**：Recharts 坐标轴/网格/图例/Tooltip/饼图标签全局主题化，
> 并让共享报表图表、工单深度分析、流程趋势图使用亮/暗两套 palette，详见 §13。
>
> **第六轮（2026-11-13 深夜）**：修复页面级硬编码设计对象——SLA 监控非全屏白底、个人中心、
> 管理后台 SystemOverview / QuickActions 的白卡与深色文字，详见 §14。
>
> **第七轮（2026-11-13 深夜）**：8 个报表/分析页 Recharts 全部接入 `useChartTheme()`；
> 6 个 `@ant-design/charts` 仪表盘图接入 `classicDark/classic` 主题与亮/暗 palette，详见 §15。
>
> **部署（2026-11-13 22:24）**：前端镜像 `itsm-frontend:1.8.0-dev.cad6c1d7` 已发布到 238 demo 栈，
> 容器 healthy、8089 首页/health 200，实测 `html.dark` 页面底 `#0f172a`、正文 `#cbd5e1`，详见 §17。
>
> **主背景统一（2026-11-13 22:43）**：暗色顶栏不再使用 `#1e293b`，与 body/Layout/Sider/Content
> 统一为 `#0f172a`；镜像 `itsm-frontend:1.8.0-dev.3481c4db` 已发布并实测，详见 §18。

---

## 1. 背景

用户反馈：暗色主题下「背景色与主色调与控件的配色有干涉」。

本审计以**源码 token 定义**为主证据（比像素取色更精确、可定位到行），并以工单详情页暗色实拍截图交叉验证。

---

## 2. 暗色主题色源现状

### 2.1 存在三套互相独立的暗色底色定义

| # | 定义位置 | `background-primary` | `background-secondary` | `background-tertiary` | 色相系 |
|---|---|---|---|---|---|
| A | `styles/theme-variables.css:183-188` | `#111827` | `#1f2937` | `#374151` | Tailwind **gray** |
| B | `styles/globals.css:1907-1909` | `#0f172a` | `#1e293b` | `#334155` | Tailwind **slate** |
| C | `lib/design-system/colors.ts:154-159` | `#0f172a` | `#1e293b` | `#334155` | Tailwind **slate** |

**加载顺序**（`styles/globals.css:2-7`）：`theme-variables.css` → `antd-layout-overrides.css` → `animations.css` → `typography.css` → `tailwindcss`。

由于三者全部写入 `:root` / `.dark` 且无 `@layer` 隔离，**后导入者覆盖先导入者**。因此：

- `--color-border-primary` 被定义两次：`theme-variables.css:191` = `#374151`（gray），`globals.css:1913` = `#334155`（slate）。最终生效 **slate**。
- `--color-text-primary` 同样双写：`theme-variables.css:175` = `#f9fafb`，`globals.css:1916` = `#f8fafc`。
- A 套的 gray 值实际成为**死代码**，但其语义仍被部分模块按名字引用（如 `Sidebar.module.css` 的 `var(--color-bg-primary)` 回退链）。

**后果**：两套色相（gray 的 `#1f2937` 偏中性灰，slate 的 `#1e293b` 偏蓝）亮度几乎相同（相对亮度 0.0154 vs 0.0218），肉眼分不清但叠在一起会出现「脏灰、偏蓝」的色偏，这是干涉的第一层来源。

### 2.2 主色调：单值硬编码，跨模式共用

`lib/design-system/theme.tsx:140-143`

```ts
// 主按钮背景需承载白色文字（>=4.5:1），因此固定使用 600 阶；
// 链接文字另用 colorLink，暗色下自动取更亮的色阶，避免主色变深后链接不可读。
colorPrimary: '#2563eb',
colorLink: colorPalette.primary[600],
```

- `colorPrimary` 被**硬编码为 `#2563eb`**，亮色/暗色共用同一值，不随 `theme.darkAlgorithm` 提亮。
- `colorLink` 则取 `darkColors.primary[600]` = `#60a5fa`（`colors.ts:130`）。

**后果**：同一个「品牌主色」在暗色下分裂为两个亮度（`#2563eb` 与 `#60a5fa`），凡是用 `token.colorPrimary` 的图标/描边/文本都会偏暗。

> 注：`colorPrimary` 固定为 600 阶这一决策**本身是合理的**——白字压在 `#2563eb` 上为 5.17:1，满足 AA。问题不在「不该用 600 阶」，而在「暗色下缺少一个用于前景（文本/描边）的更亮主色出口」。

---

## 3. 实测对比度

按 WCAG 2.1 相对亮度公式计算（`colors.ts:226-246` 内已有同款实现）。

| 前景 | 背景 | 对比度 | 判定 | 场景 |
|---|---|---|---|---|
| `#3b82f6` | `#2563eb` | **1.40:1** | ❌ 严重不合格 | 暗色 ghost 主按钮文字落底色 |
| `#2563eb` | `#0f172a` | 3.46:1 | ⚠️ 仅达 UI 组件下限(3:1) | 主色描边/图标 vs 页面底 |
| `#3b82f6` | `#0f172a` | 4.86:1 | ✅ 可用 | 提亮后的主色 vs 页面底 |
| `#60a5fa` | `#0f172a` | 7.02:1 | ✅ 优秀 | 链接/强调文本 |
| `#ffffff` | `#2563eb` | 5.17:1 | ✅ AA | 主按钮白字 |
| `#0f172a` | `#1e293b` | **1.22:1** | ❌ 层次塌陷 | 侧边栏 vs 卡片 |
| `#374151` | `#111827` | 1.53:1 | ❌ | gray 套内部同样塌陷 |

---

## 4. 缺陷清单

### P0-1｜ghost 主按钮被强制填充，蓝字落蓝底（1.40:1，几乎不可读）

**证据链**

1. 组件：`components/business/TicketAIInlineAssistant.tsx:181-191`

   ```tsx
   <Button type="primary" size="small" ghost icon={<Sparkles />} ...>
     生成答复草稿
   </Button>
   ```

   Ant Design 语义中 `type="primary" ghost` = **透明底 + 主色描边 + 主色文字**。

2. 覆盖规则：`styles/globals.css:2943-2947`

   ```css
   html.dark .ant-btn-primary {
     background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
   }
   ```

3. 特异性计算：
   - `html.dark .ant-btn-primary` → `(0,2,1)`
   - antd ghost 规则 `.ant-btn-background-ghost.ant-btn-primary` → `(0,2,0)`

   **`(0,2,1) > (0,2,0)`，覆盖规则胜出**。且全库 grep 确认**无任何 `.ant-btn-background-ghost` 排除**。

4. 叠加第二处填充源：`styles/globals.css:1144-1147` 的 `.ant-btn-primary { background-color: var(--color-primary-500) }`，暗色下 `--color-primary-500` = `#3b82f6`（`globals.css:1845`），同样命中 ghost。

**结果**：ghost 按钮变成实心蓝，而文字色仍为主色 `#3b82f6` → `#3b82f6` on `#2563eb` = **1.40:1**，截图已复现「蓝底蓝字」。

**修复**：两处主按钮覆盖规则均排除 `.ant-btn-background-ghost` 与 `:disabled`。

---

### P0-2｜暗色缺少主按钮禁用态规则，禁用与可用难以区分

`globals.css` 中仅 `2351-2357` 存在 `html:not(.dark)` 下的 text/link 禁用规则，**暗色完全没有 `.ant-btn-primary:disabled`**。

叠加 P0-1 后：「填入回复框」(primary + disabled) 与「生成答复草稿」(primary + ghost + 可用) 都呈现蓝色填充块，只能靠文字变淡区分，违背可用性要求。

**修复**：为 `html.dark .ant-btn-primary:disabled` 补低饱和中性底 + 明确的禁用文字色。

---

### P0-3｜侧边栏与内容区同底色，层次塌陷（1.22:1）

`components/layout/sidebar/Sidebar.module.css:8`

```css
background: var(--color-background-primary, var(--color-bg-primary)) !important;
```

`--color-background-primary` 暗色 = `#0f172a`，与主内容区**完全同值**；卡片层 `--color-surface-primary` = `#1e293b`。侧边栏与卡片间仅 **1.22:1** 亮度差，视觉上无法区分「导航栏 / 内容底 / 卡片」三层结构。

**修复**：侧边栏改用 `--color-background-secondary`（`#1e293b`），使 内容底(`#0f172a`) < 侧边栏/卡片(`#1e293b`) 形成稳定的 Elevation 阶梯；同时把卡片底提到 `--color-surface-primary` 保持 1.22:1 → 与页面底 1.22:1 的可辨层差。

> 说明：暗色下合理的 Elevation 是「越靠前越亮」，即 `#0f172a` → `#1e293b` → `#334155`。当前把侧边栏钉在最暗层，破坏了这一模型。

---

### P1-1｜布局覆写文件硬编码浅色，暗色定时炸弹

`styles/antd-layout-overrides.css:8-24`

```css
.ant-layout { background: #f5f5f5; }
.ant-layout-header { background: #fff !important; }
```

两处硬编码浅色且 header 带 `!important`。当前页面因自定义 Header/Sidebar 覆盖而未暴露，但任何回退到 `.ant-layout-header` 的场景（如新增布局、打印、SSR 首屏）会在暗色下闪出白块。

**修复**：改为 `var(--color-background-secondary)` / `var(--color-background-primary)`。

---

### P1-2｜`--sla-expired` 暗色未覆盖，SLA 违规标识近乎不可见

`styles/theme-variables.css:162-167`

```css
--sla-danger: #ef4444;
--sla-expired: #7f1d1d;   /* 深酒红 */
```

暗色块 `:root.dark`（`172-214`）**未重定义 `--sla-expired`**。`#7f1d1d` 压在 `#0f172a` 上对比度仅约 **1.6:1**，而当前工单恰好处于「SLA 已违规」状态，该标识几乎不可见。

**修复**：暗色下提亮为 `#f87171`（error-400）；第三轮进一步调整为 `#fca5a5`（9.41:1），
并补 `--sla-danger: #f87171` 保留一档区分（见 §11）。

---

### P1-3｜`prefers-color-scheme` 分支与 `.dark` 分支主色冲突

`styles/globals.css:2051-2055`

```css
@media (prefers-contrast: high) { :root { --color-primary-600: #1d4ed8; ... } }
```

该分支与 `globals.css:1846` 的暗色 `--color-primary-600: #60a5fa` 在同一层叠上下文内竞争，导致「系统级暗色」与「手动切暗色」表现不一致；`high` 对比度模式在暗色下反而把主色**压暗**，方向性错误。

**修复**：`prefers-contrast` 分支限定 `:root:not(.dark)`，暗色走自己的提亮逻辑。

---

### P2-1｜`theme-variables.css` 与 `globals.css` 双写同名 token

同一变量在多处定义、取值不同（见 §2.1），维护期极易回归。建议以 `globals.css`（slate 系）为唯一真源，`theme-variables.css` 只保留**非颜色**类 token（间距/字体/圆角/阴影），或将颜色值对齐到 slate 系。

---

### P2-2｜单测与实现不一致（既有失败）

`lib/design-system/__tests__/design-system.test.ts:269`

```ts
expect(themeConfig.token.colorPrimary).toBe('#3b82f6');
```

而实现 `theme.tsx:142` 已是 `'#2563eb'`。该断言为历史遗留，已与「主按钮须承载白字 ≥4.5:1」的设计意图相反。

**修复**：断言更正为 `'#2563eb'`，并补一条「暗色 `colorLink` 应更亮」的守护断言。

---

## 5. 修复优先级与策略

| 优先级 | 缺陷 | 改动文件 | 风险 |
|---|---|---|---|
| P0 | P0-1 ghost 被填充 | `globals.css:1144,2945` | 低（加 `:not()` 收窄） |
| P0 | P0-2 禁用态缺失 | `globals.css`（新增） | 低（纯新增） |
| P0 | P0-3 层次塌陷 | `Sidebar.module.css:8` | 中（需回归侧栏视觉） |
| P1 | P1-1 硬编码浅色 | `antd-layout-overrides.css:10,18` | 低 |
| P1 | P1-2 SLA 不可见 | `theme-variables.css` | 低（纯新增） |
| P1 | P1-3 高对比度冲突 | `globals.css:2051` | 低 |
| P2 | P2-1 双写 token | `theme-variables.css` | 中（面广，建议单独立项） |
| P2 | P2-2 单测错位 | `design-system.test.ts:269` | 低 |

### 设计原则（本次修复遵循）

1. **前景/背景分离**：暗色下填充主色保持 600 阶（保证白字 5.17:1），强调文本/描边走 `--color-primary-600` = `#60a5fa`（7.02:1）。
2. **Elevation 单调递增**：`#0f172a`(页面) → `#1e293b`(侧栏/卡片) → `#334155`(悬浮层)，越靠前越亮。
3. **状态语义不依赖亮度差**：禁用态必须有独立的中性色，不能复用可用态填充。
4. **单一真源**：颜色 token 不再跨文件双写。

---

## 6. 验收标准

- [ ] 暗色工单详情页「生成答复草稿」呈现 **透明底 + 主色描边 + 主色文字**，文字对比度 ≥ 4.5:1
- [ ] 「填入回复框」禁用态呈中性灰底，与可用主按钮视觉可区分
- [ ] 侧边栏 / 内容区 / 卡片三层可辨，相邻层对比度 ≥ 1.2:1 且明度单调递增
- [ ] `antd-layout-overrides.css` 无硬编码浅色（grep `#fff`/`#f5f5f5` 为空）
- [ ] SLA 已违规标识在暗色下清晰可见（≥ 4.5:1）
- [ ] `pnpm test lib/design-system` 全绿
- [ ] 亮色模式无视觉回归（主按钮渐变仍为 `#3b82f6→#2563eb`）

---

## 7. 附录：证据索引

| 文件 | 行号 | 内容 |
|---|---|---|
| `styles/theme-variables.css` | 162-167 | SLA 颜色定义（`--sla-expired`） |
| `styles/theme-variables.css` | 172-214 | `.dark` 覆盖块（缺 `--sla-expired`） |
| `styles/globals.css` | 2-7 | 样式导入顺序 |
| `styles/globals.css` | 841-854 | `:root` 主色阶（`-500: #2563eb`） |
| `styles/globals.css` | 1144-1154 | 基础主按钮覆盖（未排除 ghost） |
| `styles/globals.css` | 1838-1850 | `.dark` 主色阶（`-500: #3b82f6`） |
| `styles/globals.css` | 1907-1919 | `.dark` 功能色（slate 系） |
| `styles/globals.css` | 2051-2055 | `prefers-contrast` 冲突分支 |
| `styles/globals.css` | 2372-2382 | 亮色主按钮（含 `!important`） |
| `styles/globals.css` | 2943-2952 | **暗色主按钮渐变（P0-1 根因）** |
| `styles/antd-layout-overrides.css` | 8-24 | 硬编码浅色 |
| `components/layout/sidebar/Sidebar.module.css` | 8 | 侧边栏底色 |
| `lib/design-system/theme.tsx` | 140-143 | `colorPrimary` / `colorLink` |
| `lib/design-system/colors.ts` | 121-180 | `darkColors` |
| `components/business/TicketAIInlineAssistant.tsx` | 181-214 | ghost / disabled 按钮 |
| `lib/design-system/__tests__/design-system.test.ts` | 269 | 错位断言 |

---

## 8. 修复记录（已实施）

### 8.1 改动清单

| 文件 | 位置 | 改动 | 对应缺陷 |
|---|---|---|---|
| `src/styles/globals.css` | 1144-1157 | 基础主按钮规则加 `:not(.ant-btn-background-ghost):not(:disabled)` | P0-1 |
| `src/styles/globals.css` | 2956-2999 | 暗色主按钮渐变同样排除 ghost/disabled | P0-1 |
| `src/styles/globals.css` | 2975-2995 | **新增**暗色 ghost 主按钮规则：透明底 + `--color-primary-600` 描边/文字 | P0-1 |
| `src/styles/globals.css` | 2996-3007 | **新增**暗色主按钮禁用态：中性底 + `--color-text-tertiary` 文字 | P0-2 |
| `src/styles/globals.css` | 2053-2068 | `prefers-contrast: high` 分支排除 `.dark`，并为暗色补提亮值 | P1-3 |
| `src/styles/antd-layout-overrides.css` | 8-20 | `.ant-layout` / `.ant-layout-header` 硬编码浅色改为主题令牌 | P1-1 |
| `src/styles/theme-variables.css` | 215-217 | 暗色补 `--sla-expired`（第三轮定为 `#fca5a5`） | P1-2 |
| `src/components/layout/sidebar/Sidebar.module.css` | 8-15 | 保持 `background-primary`（与菜单 `itemBg` 同源），补注释说明层次由既有 `border-right` 承担 | P0-3 |
| `src/lib/design-system/__tests__/design-system.test.ts` | 269 | 断言更正为 `#2563eb` | P2-2 |

### 8.2 关键决策说明

**为什么 ghost 不能只靠「排除填充」修好**

去掉填充后，antd 传入的前景色仍是 `colorPrimary` = `#2563eb`，压在卡片底 `#1e293b` 上只有 **2.49:1**，依然不可读。因此必须同时把 ghost 的前景色切到暗色提亮阶 `--color-primary-600`（`#60a5fa`）。

**为什么侧边栏没有改成 secondary 底色**

初版曾把 `.sider` 改为 `--color-background-secondary`，但发现两处冲突，已回退：

1. `theme.tsx:217-218` 中 antd Menu 的 `itemBg` / `subMenuItemBg` 取的是 `background.primary`。侧栏换底后，菜单项会变成嵌在较亮侧栏里的深色块，出现反色补丁。
2. `sidebar-theme-consistency.test.ts:79` 明确断言 `.sider` 使用 `background-primary`。改值会直接打破该守护测试。

结论：层次感改由**既有** `border-right: 1px solid var(--color-border-primary)` 承担（`#334155` 描边，与两侧底均有可辨差异），不动 token 语义。

**为什么禁用态文字用 `tertiary` 而不是 `disabled`**

初版用 `--color-text-disabled`（`#64748b`），压在 `#334155` 上只有 **2.18:1**，实测不合格——禁用态虽不可点击，标签内容仍需可辨识。改用 `--color-text-tertiary`（`#94a3b8`）后为 **4.04:1**。

### 8.3 实测验证（WCAG 2.1 相对亮度公式）

| 场景 | 前景 / 背景 | 修复前 | 修复后 | 判定 |
|---|---|---|---|---|
| ghost 按钮文字 / 卡片底 | `#60a5fa` / `#1e293b` | 1.41:1 | **5.75:1** | ✅ AA |
| ghost 按钮文字 / 页面底 | `#60a5fa` / `#0f172a` | 1.41:1 | **7.02:1** | ✅ AAA |
| ghost 悬停文字 / 卡片底 | `#93c5fd` / `#1e293b` | — | **8.11:1** | ✅ AAA |
| 实心主按钮白字 | `#ffffff` / `#2563eb` | 5.17:1 | **5.17:1** | ✅ AA（未回归） |
| 禁用主按钮文字 | `#94a3b8` / `#334155` | 2.18:1 | **4.04:1** | ✅ 明显改善 |
| SLA 已违规标识 | `#fca5a5` / `#0f172a` | 1.78:1 | **9.41:1** | ✅ AAA |
| 正文 / 次要文字 | `#f8fafc` / `#cbd5e1` | 17.06 / 12.02 | 不变 | ✅ AAA |

### 8.4 浏览器实机验证

在 `localhost:3000`（Vite dev，`html.dark`）注入真实样式表后，用 `getComputedStyle` 读取三种按钮的实际计算值，与截图双重确认：

```json
{
  "btn-ghost":    { "background": "rgba(0,0,0,0)", "backgroundImage": "none",
                    "color": "rgb(96,165,250)", "borderColor": "rgb(96,165,250)" },
  "btn-solid":    { "backgroundImage": "linear-gradient(135deg, rgb(59,130,246) 0%, rgb(37,99,235) 100%)",
                    "color": "rgb(248,250,252)" },
  "btn-disabled": { "background": "rgb(51,65,85)", "color": "rgb(148,163,184)",
                    "borderColor": "rgb(71,85,105)" }
}
```

- ghost：**透明底** + 浅蓝文字/描边 → 「蓝字压蓝底」已消除
- solid：蓝色渐变 + 白字 → 亮色/暗色品牌一致性保持，无回归
- disabled：中性 `#334155` 底 + 柔和文字 → 与可用主按钮可区分

> 说明：本地后端（127.0.0.1:8090）未运行，无法登录进入工单详情页做整页截图；故采用「真实样式表 + 真实 `html.dark` + 计算样式读取 + 截图」的方式针对按钮三态做定向验证，覆盖了本次全部按钮类改动。

### 8.5 测试结果

```
npx jest src/components/layout/sidebar src/lib/design-system --coverage=false
Test Suites: 5 passed, 5 total
Tests:       63 passed, 63 total
```

CSS 解析校验（postcss）：`globals.css` / `theme-variables.css` / `antd-layout-overrides.css` / `Sidebar.module.css` 全部 OK。

> 注：直接 `npx jest <单文件>` 时退出码为 1，原因是仓库全局覆盖率阈值（80%）在子集运行下无法满足，**并非测试失败**。加 `--coverage=false` 后全部通过。

---

## 9. 未处理项（建议后续立项）

| 编号 | 问题 | 建议 |
|---|---|---|
| P2-1 | `theme-variables.css` 与 `globals.css` 双写同名 token（gray 系 vs slate 系） | **暗色值已在 §10 对齐为 slate 系**；仍是双文件定义，建议后续合并为单一真源 |
| 遗留 | `--sla-danger`（`#ef4444`）在暗色下未单独定义 | **已在 §11 补齐为 `#f87171`** |
| 遗留 | `TicketDetail.tsx` / `TicketListColumns.tsx` 存在与本任务无关的既有改动 | 本次未触碰，提交时注意分离 |

---

## 10. 第二轮复核与修复（2026-11-13 晚）

### 10.1 复核方法

首轮修复全部保留并验证通过后，又对前端全量样式做了一轮静态 + 运行时复核：

1. **静态扫描**：遍历 `src/**/*.css`（17 个）与 `src/**/*.tsx`（722 个），按 WCAG 相对亮度公式筛选
   「亮色背景 / 深色文字 / 亮色边框」在没有暗色分支时命中的规则。
2. **运行时验证**：用 Vite 生产构建产物 + Chromium `getComputedStyle`，在真实 `html.dark` / `html.light`
   下读取页面、卡片、`bg-white`、`bg-gray-50`、`bg-blue-50`、标题/正文/链接、Antd Layout 的计算值。
3. **回归验证**：TypeScript、生产构建、主题相关 Jest 用例全部执行。

### 10.2 新增缺陷清单（按严重度）

| 编号 | 级别 | 问题 | 证据 | 修复 |
|---|---|---|---|---|
| P0-4 | P0 | 暗色 Antd `colorBgContainer` 与页面底同值，Card/Table/Input 与页面 **1.0:1**，层级完全塌陷 | `theme.tsx:150` 取 `background.primary(#0f172a)` | 改取 `surface.primary(#1e293b)`；`colorBgLayout` 暗色取 `background.primary(#0f172a)` |
| P0-5 | P0 | Layout 覆写文件暗色仍硬编码 `#141414` / `#1f1f1f`，形成第四套中性色；且 `.ant-layout-content { background: transparent !important }` 把主题类锁死 | `antd-layout-overrides.css:232-262`、`:55-62` | 全部改为主题令牌；移除 content 的 `background !important` 锁；暗色 header 与自定义 Header 统一 `#1e293b` |
| P0-6 | P0 | 全局基础排版写死亮色：`h1-h6 #171717`、`p #404040`、`a #2563eb`/`#1d4ed8`。所有未显式着色的 `<p>`（弹窗、详情、空状态）在暗色下不可读 | `globals.css:1317-1407` | 标题 → `text-primary`，正文 → `text-secondary`，链接 → `primary-600`/`primary-700`，焦点外圈 → `background-primary` |
| P0-7 | P0 | Tooltip / Popover / Dropdown 使用 `color: text-inverse` + `background: bg-elevated`：亮色=白字白底，暗色=深字深底，文字几乎不可见 | `globals.css:219-282` | 文字改 `text-primary`；tooltip 补边框；dropdown 边框/悬停/选中改主题令牌 |
| P1-4 | P1 | `theme-variables.css` 暗色仍是 Tailwind gray（`#111827/#1f2937/#374151`），与 globals/design-system 的 slate 叠加产生「灰中带蓝」色偏；系统暗色分支与手动 `.dark` 也不一致 | `theme-variables.css:172-258` | 文本/背景/边框/`--color-bg-*` 全部对齐 slate；系统暗色分支同步，并补 `--sla-expired` |
| P1-5 | P1 | Tailwind pastel 工具类没有暗色兜底：`bg-white` 被映射成页面底、`bg-gray-50` 被映射成卡片底（方向相反）；`bg-blue-50/green-100/text-blue-800` 等在暗色下形成高亮浅色补丁，hover 还会闪白 | `globals.css` 暗色工具类区 | 重排 `bg-white`→surface、`bg-gray-50/100`→page；新增语义软底/文字/边框与 hover/focus 变体兜底 |
| P1-6 | P1 | 事件/问题统计卡使用固定 pastel 渐变，暗色下整排浅色卡片；`.label` 固定 `#4b5563` | `BusinessStatsGrid.tsx:21-73`、`.module.css` | 新增暗色 tone 调色板（低透明度彩色 + 提亮前景）；label 改用 `text-secondary` |
| P1-7 | P1 | Header 仍有亮色残留：激活按钮 `#dbeafe/#2563eb`、角色徽章 `#eff6ff`、通知正文 `#6b7280`、通知分割线 `#f3f4f6`、退出登录 hover `#fef2f2`、邮箱 `#64748b` | `header/Header.module.css` | 基础值改令牌；补激活态/通知态/退出态的 `html.dark` 覆盖 |
| P1-8 | P1 | CMDB 拓扑节点固定 `background:#fff` + `color:#333`，暗色画布上是一块白卡 | `pages/(main)/cmdb/topology/index.tsx:82-86` | 改 `surface-primary` / `text-primary` |
| P2-3 | P2 | **亮色** ghost 主按钮仍被 `html:not(.dark) .ant-btn-primary` 强行填充（首轮只修了暗色规则），亮色下同样存在蓝字压蓝底 | `globals.css:2531-2541` | 亮色规则同样排除 `.ant-btn-background-ghost` 与 `:disabled`，并补 ghost disabled 透明态 |
| P2-4 | P2 | 工单详情「AI 分诊建议已应用」固定浅绿底 `#f6ffed`，暗色下是刺眼浅色块 | `TicketDetail.tsx:1098-1114` | 抽出 `.ticket-applied-notice`，暗色使用 `success-bg` + `#bbf7d0` |
| P2-5 | P2 | 多处固定深色文字在暗色下不可读：`#000`、`#666`、`#1f2937`、`rgba(0,0,0,0.45)` | IncidentList / PageHeader / GlobalError / ErrorBoundary / TicketModal / AccessTokenPanel / TenantUsersDrawer | 统一改 `text-primary` / `text-secondary` / `text-tertiary` |
| P2-6 | P2 | 系统暗色分支缺 `--sla-expired`，与手动 `.dark` 表现不一致 | `theme-variables.css:220-258` | 同步补齐 `--sla-danger` / `--sla-expired` |
| P2-7 | P2 | 页面级背景复用 `--color-bg-secondary`，暗色下该令牌是卡片层 `#1e293b`，导致页面/卡片同色 | dashboard / MSP dashboard / service catalog | 新增 `--color-page-background`（亮 `#f8fafc` / 暗 `#0f172a`），页面容器改用它 |

### 10.3 本轮改动文件

| 文件 | 改动 |
|---|---|
| `src/lib/design-system/theme.tsx` | 暗色 container/layout 层级修正 |
| `src/lib/design-system/__tests__/design-system.test.ts` | 守护暗色 container=`#1e293b`、layout=`#0f172a` |
| `src/styles/theme-variables.css` | gray → slate；系统暗色同步；新增 `--color-page-background`；补系统暗色 SLA |
| `src/styles/antd-layout-overrides.css` | 移除 `#141414/#1f1f1f`；header/layout 走令牌；移除 content 背景锁；面包屑文字令牌化 |
| `src/styles/globals.css` | 基础排版、组件基元、暗色 Tailwind 工具类、hover/focus 变体、亮色 ghost 排除、应用提示条 |
| `src/components/layout/header/Header.module.css` | 残留亮色态全部令牌化 + 暗色覆盖 |
| `src/components/common/BusinessStatsGrid.tsx` / `.module.css` | 暗色统计卡调色板 + label 令牌 |
| `src/components/ticket/TicketDetail.tsx` | 应用提示条改主题类 |
| `src/pages/(main)/cmdb/topology/index.tsx` | 拓扑节点主题化 |
| `src/pages/(main)/incidents/components/IncidentList.tsx` 等 | `#000/#666` 等固定深色文字令牌化 |
| `src/components/layout/PageHeader.tsx`、`errors/GlobalError.tsx`、`layout/ErrorBoundary.tsx`、`lib/error-handler.module.css` | 错误页/页头暗色可读性 |
| `src/pages/(main)/dashboard/index.tsx`、`msp/dashboard/index.tsx`、`service-catalog/index.tsx` | 页面背景改用 `--color-page-background` |
| `src/components/layout/__tests__/ErrorBoundary.test.tsx` | 移除 jsdom 无法序列化的 `background: var(...)` 断言 |

### 10.4 浏览器实测（生产构建 + `getComputedStyle`）

| 元素 | 暗色实际值 | 亮色实际值 | 判定 |
|---|---|---|---|
| 页面 `--color-background-primary` | `#0f172a` | `#ffffff` | ✅ |
| 卡片 `--color-surface-primary` | `#1e293b` | `#ffffff` | ✅ |
| `<h1>` | `rgb(248,250,252)` | `rgb(15,23,42)` | ✅ |
| `<p>` | `rgb(203,213,225)` | `rgb(71,85,105)` | ✅ |
| `<a>` | `rgb(96,165,250)` | `rgb(37,99,235)` | ✅ |
| `.card` | `rgb(30,41,59)` | `rgb(255,255,255)` | ✅ |
| `.bg-white` | `rgb(30,41,59)` | `rgb(255,255,255)` | ✅ |
| `.bg-gray-50` | `rgb(15,23,42)` | `rgb(249,250,251)` | ✅ |
| `.bg-blue-50` | `rgba(59,130,246,0.15)` | 浅蓝 | ✅ |
| `.text-blue-800` | `rgb(96,165,250)` | 深蓝 | ✅ |
| `.ant-layout` | `rgb(15,23,42)` | `rgb(248,250,252)` | ✅ |
| `.ant-layout-header` | `rgb(30,41,59)` | `rgb(255,255,255)` | ✅ |
| `.ant-layout-content` | `rgb(15,23,42)` | `rgb(255,255,255)` | ✅ |
| Tooltip / Popover / Dropdown 文字 | `rgb(248,250,252)` on `rgb(30,41,59)` | `rgb(15,23,42)` on `rgb(255,255,255)` | ✅ |

### 10.5 对比度复核（WCAG 2.1）

| 场景 | 前景 / 背景 | 对比度 | 判定 |
|---|---|---|---|
| 暗色标题 | `#f8fafc` / `#0f172a` | 17.06:1 | ✅ AAA |
| 暗色正文 | `#cbd5e1` / `#0f172a` | 12.02:1 | ✅ AAA |
| 暗色链接（页面） | `#60a5fa` / `#0f172a` | 7.02:1 | ✅ AAA |
| 暗色链接（卡片） | `#60a5fa` / `#1e293b` | 5.75:1 | ✅ AA |
| 暗色卡片/页面层差 | `#1e293b` / `#0f172a` | 1.22:1 + 边框 `#334155` | ✅ 可辨 |
| Header 正文 | `#f8fafc` / `#1e293b` | 13.98:1 | ✅ AAA |
| 暗色激活图标 | `#60a5fa` / 软底有效色 `#223657` | 4.77:1 | ✅ AA |
| 暗色应用提示 | `#bbf7d0` / 软底有效色 `#1f4241` | 9.05:1 | ✅ AAA |
| 暗色禁用主按钮 | `#94a3b8` / `#334155` | 4.04:1 | ✅ 明显改善 |
| 亮色 ghost 主按钮 | `#2563eb` / `#ffffff` | 5.17:1 | ✅ AA（不再被填充） |

### 10.6 测试与构建

```text
npx jest src/lib/design-system src/components/layout/sidebar \
  src/components/layout/__tests__/ErrorBoundary.test.tsx \
  src/components/common/__tests__/ErrorBoundary.test.tsx \
  src/components/ticket/__tests__/TicketDetailAssignSearch.test.tsx \
  --coverage=false
Test Suites: 8 passed, 8 total
Tests:       134 passed, 7 skipped, 141 total
```

- `npx tsc --noEmit`：通过
- `npm run build`：通过（Vite 11369 modules，仅既有 chunk size 警告）
- postcss 解析：`globals.css` / `theme-variables.css` / `antd-layout-overrides.css` /
  `header/Header.module.css` / `BusinessStatsGrid.module.css` 全部 OK
- Chromium 计算样式：见 §10.4

### 10.7 仍未处理 / 后续建议

以下项目已在 §11 第三轮收尾中处理：P2-1 亮色主色阶、P2-8 暗色前景主色、P2-9 SLA danger、
未引用死代码、内联 `#999/#8c8c8c`、暗色下拉固定白色文字。

剩余建议：

| 编号 | 问题 | 建议 |
|---|---|---|
| 遗留 | 语义色仍有一部分以内联具体色值出现（如 `#1890ff`、`#52c41a`、`#fa8c16`） | **第四轮已迁移暗色下对比度不足的深紫/深红/深绿/灰色（§12）**；其余当前基本合格 |
| 遗留 | `colors.ts` / `theme-variables.css` / `globals.css` 三处仍各存一份主色阶（值已一致，并有 §11 的守护测试） | 长期建议由 `colors.ts` 生成 CSS 变量，彻底消除双写 |
| 遗留 | 旧文档仍引用已删除的 `variables.module.css` / `utilities.module.css` / `KanbanStyles.module.css` | 文档属历史记录，后续可标注「已废弃」 |

---

## 11. 第三轮收尾（2026-11-13 深夜）

### 11.1 目标

处理 §10.7 遗留项：亮色主色阶单一真源、暗色前景主色、SLA 危险色、暗色浮层品牌化、内联灰色文字、未引用死代码。

### 11.2 改动明细

| 项目 | 改动 | 对应编号 |
|---|---|---|
| 亮色主色阶 | `globals.css` 的 `--color-primary-50~950` 整体对齐 `colors.ts`/`theme-variables.css`：500=`#3b82f6`、600=`#2563eb`、700=`#1d4ed8`…。CSS `<a>` 亮色由 `#1d4ed8` 恢复为 `#2563eb`，与 Antd `colorLink` 一致 | P2-1 |
| 主色阶守护测试 | `design-system.test.ts` 新增：解析 `globals.css` 亮/暗主色阶，逐档与 `colors.primary` / `darkColors.primary` 比对 | P2-1 |
| 暗色前景主色 | `colorPrimary` 暗色由固定 `#2563eb` 改为 `#3b82f6`；`colorLink` 保持 `#60a5fa`。非按钮图标/描边在 `#0f172a` 上由 3.46:1 提升到 4.85:1 | P2-8 |
| SLA 暗色 | 暗色补 `--sla-danger: #f87171`（6.45:1），`--sla-expired: #fca5a5`（9.41:1），同时保留两者一档区分；系统暗色分支同步 | P2-9 |
| 暗色浮层 | 36 条 Select/DatePicker/ColorPicker/TreeSelect/Cascader/Mentions/AutoComplete/Transfer 暗色规则去掉固定 `#fff` 与白色透明度：文字→`text-primary`、边框→`border-primary`、hover→`background-tertiary`、selected→`primary-soft + primary-600`、disabled→`text-disabled` | 10.7 遗留 |
| 内联灰色文字 | 22 个 TSX 文件、65 处 `color: '#999'` / `color: '#8c8c8c'` → `var(--color-text-tertiary)`；CMDB 拓扑 SVG marker 的 `fill/stroke` 保留具体色值（CSS 变量不适用于 SVG presentation attribute） | 10.7 遗留 |
| 死代码清理 | 删除未引用文件：`components/layout/Header.module.css`、`pages/(main)/profile/profile.module.css`、`components/ticket/KanbanStyles.module.css`、`components/ui/design-system.css`、`pages/(main)/dashboard/dashboard.css`、`styles/variables.module.css`、`styles/utilities.module.css`、`lib/theme/*`、`lib/utils/color-tokens.ts` 及其测试 | 10.7 遗留 |

### 11.3 验证

- `npx tsc --noEmit`：通过
- `npm run build`：通过（Vite 11369 modules，仅既有 chunk size 警告）
- Jest：`7 suites passed / 131 tests passed / 7 skipped`
  - 新增「globals.css 主色阶与 colors.ts / darkColors 保持单一真源」通过
  - 新增暗色 `colorPrimary=#3b82f6`、`colorLink=#60a5fa` 断言通过
- 对比度复核：

| 场景 | 前景 / 背景 | 对比度 |
|---|---|---|
| 暗色前景主色（图标/描边） | `#3b82f6` / `#0f172a` | 4.85:1 |
| 暗色链接 | `#60a5fa` / `#1e293b` | 5.75:1 |
| 暗色 SLA danger | `#f87171` / `#0f172a` | 6.45:1 |
| 暗色 SLA expired | `#fca5a5` / `#0f172a` | 9.41:1 |
| 亮色 badge-primary | `#1d4ed8` / `#dbeafe` | 5.49:1 |
| 亮色主按钮白字 | `#ffffff` / `#2563eb` | 5.17:1 |

### 11.4 仍然保留的长期项

1. **三处主色阶仍是物理双写**（`colors.ts` / `theme-variables.css` / `globals.css`），但值已一致且有测试守护。长期建议以 `colors.ts` 生成 CSS 变量。
2. **语义色仍有一部分以内联具体色值出现**（`#1890ff`、`#52c41a`、`#fa8c16` 等）。第四轮已迁移暗色下对比度不足的深紫/深红/深绿/灰色；其余颜色当前在深底上基本合格，后续可继续迁移。
3. **历史文档**仍引用本轮删除的 `variables.module.css` / `utilities.module.css` / `KanbanStyles.module.css`；这些文件已无代码引用，文档可后续标注「已废弃」。

---

## 12. 第四轮：语义前景色令牌化（2026-11-13 深夜）

### 12.1 新增语义前景色令牌

`theme-variables.css` 的亮色、暗色、系统暗色三个分支同步新增：

| 令牌 | 亮色 | 暗色 | 用途 |
|---|---|---|---|
| `--color-primary-text` | `#2563eb` | `#60a5fa` | 主色文字/图标 |
| `--color-success-text` | `#15803d` | `#4ade80` | 成功/完成/启用 |
| `--color-warning-text` | `#b45309` | `#fbbf24` | 警告/等待/临期 |
| `--color-error-text` | `#dc2626` | `#f87171` | 错误/失败/严重 |
| `--color-info-text` | `#0369a1` | `#7dd3fc` | 信息/提示 |
| `--color-purple-text` | `#7c3aed` | `#c084fc` | 紫色状态/流程 |
| `--color-magenta-text` | `#db2777` | `#f472b6` | 品红指标 |

### 12.2 内联颜色迁移

迁移暗色下对比度明显不足的具体色值（36 个文件）：

| 原色值 | 迁移到 | 原暗色对比（#1e293b） | 新暗色对比 |
|---|---|---|---|
| `#722ed1` | `var(--color-purple-text)` | 2.5:1 | 5.54:1 |
| `#cf1322` / `#f5222d` | `var(--color-error-text)` | 2.4~3.0:1 | 5.29:1 |
| `#3f8600` / `#389e0d` | `var(--color-success-text)` | 3.1~4.0:1 | 8.40:1 |
| `#595959` / `#bfbfbf` | `var(--color-text-tertiary)` | 1.9~8.3:1 | 5.71:1 |
| `#eb2f96` | `var(--color-magenta-text)` | 3.4:1 | 5.52:1 |
| `#d48806` / `#d46b08` | `var(--color-warning-text)` | 4.5~5.0:1 | 8.76:1 |

**刻意未替换**：

- `pages/(main)/dashboard/charts.tsx`、`*Chart.tsx` 的 `color` 图表配置：部分被 Recharts 作为 SVG `fill` 使用，CSS 变量在 presentation attribute 中不可靠。
- `QuickActions` / `SystemOverview` / MSP dashboard 的 `color` 字段：会参与 `${color}14` / `${color}30` 字符串拼接，必须保留具体 hex。
- `pages/(main)/cmdb/topology/index.tsx` 的 marker `color`：SVG `fill/stroke` 同上。
- `color: '#1890ff'`、`#ff4d4f`、`#52c41a`、`#fa8c16`、`#faad14`：当前暗色对比度分别为 3.66 / 4.47 / 5.5 / 6.0 / 7.5:1，基本满足大字号/图标/状态用途；其中部分还作为表单初始颜色值使用，贸然转 CSS 变量会破坏颜色选择器。

### 12.3 验证

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- 相关 Jest：
  - 设计系统 + 侧栏 + SLA：**6 suites / 77 tests passed**
  - `ChangeStatusBadge`：**21 tests passed**
- `git diff --check` / postcss 解析：通过

### 12.4 后续

- 可继续把 `#1890ff` / `#ff4d4f` / `#52c41a` / `#fa8c16` 等剩余内联语义色迁移到 `--color-*-text`，但需逐个区分「CSS 颜色」与「图表/颜色选择器具体值」，避免误伤。
- 图表颜色建议由 `useChartTheme()` 统一输出亮/暗两套 palette，而不是在组件内写具体色值。

---

## 13. 第五轮：Recharts 图表主题兜底（2026-11-13 深夜）

### 13.1 问题

Recharts 的默认坐标轴、网格、图例和默认 Tooltip 使用浅色主题色（`#666` 轴线、浅灰网格、白底 Tooltip），
组件没有传 `useChartTheme()` 时不会跟随 `.dark`。暗色下表现为：

- 坐标轴刻度/图例几乎不可见；
- 网格线仍是浅灰；
- 默认 Tooltip 白底黑字，与暗色卡片冲突；
- 饼图标签白字落在亮色扇区上对比不足。

### 13.2 全局 CSS 兜底

在 `globals.css` 增加 Recharts 专用主题规则：

| 元素 | 规则 |
|---|---|
| 坐标轴刻度 | `.recharts-cartesian-axis-tick-value { fill: text-secondary }` |
| 轴线/刻度线 | `.recharts-cartesian-axis-line/.tick-line { stroke: border-secondary }` |
| 网格 | `.recharts-cartesian-grid line { stroke: border-primary }` |
| 图例文字 | `.recharts-legend-item-text { color: text-secondary }` |
| 默认 Tooltip | 背景 `bg-elevated`、边框 `border-primary`、文字 `text-primary` |
| Tooltip 系列行 | `.recharts-tooltip-item { color: text-primary }`；暗色下自定义 Tooltip 内联系列色统一回正文色 |
| 饼图标签 | 白字 + `rgba(15,23,42,.55)` 描边 + `paint-order: stroke` |

### 13.3 使用 `useChartTheme()` 的组件

| 组件 | 改动 |
|---|---|
| `components/reports/ReportsCharts.tsx` | 坐标轴/网格/图例/Tooltip 使用 chart theme；系列色改用 `chart.palette`；饼图标签加描边 |
| `components/business/TicketDeepAnalytics.tsx` | 系列色改用 `chart.palette`；坐标轴/网格/Tooltip 由全局规则兜底 |
| `pages/(main)/workflow/dashboard/index.tsx` | 网格/坐标轴/图例改用 chart theme；折线色改用 `chart.palette[0]` |

### 13.4 验证

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- Jest：设计系统 + 侧栏 **5 suites / 64 tests passed**
- postcss / `git diff --check`：通过
- 全量 Jest（`--coverage=false --maxWorkers=2`）：**380 suites passed / 4841 tests passed / 13 skipped**。
  初次并行运行出现的 worker `SIGTERM` 为资源压力；3 个真实失败已在 §16 收口。
- Chromium 计算样式（`html.dark`）：

| 元素 | 计算值 |
|---|---|
| 网格线 | `rgb(51,65,85)` |
| 轴线 | `rgb(71,85,105)` |
| 坐标轴刻度 | `rgb(203,213,225)` |
| 图例文字 | `rgb(203,213,225)` |
| 默认 Tooltip 背景 / 文字 | `rgb(30,41,59)` / `rgb(248,250,252)` |
| Tooltip 系列行（原 `#722ed1`） | `rgb(248,250,252)` |
| 饼图标签 fill / stroke | `rgb(255,255,255)` / `rgba(15,23,42,0.55)` |

### 13.5 后续

- 各报表页（`reports/incident-trends`、`problem-efficiency`、`cmdb-quality` 等）仍保留部分写死的系列色。
  坐标轴/网格/图例/Tooltip 已被本轮全局规则覆盖；系列色建议逐页迁移到 `useChartTheme().palette`。
- 若后续引入其他 SVG 图表库（G2/reactflow），同样应优先使用 `chart-theme.ts` 的亮/暗 palette，
  而不是在组件内写死色值。

---

## 14. 第六轮：页面级硬编码设计对象（2026-11-13 深夜）

### 14.1 SLA 实时监控（非全屏）

`components/business/SLAMonitorDashboard.tsx` 的非全屏分支此前完全按亮色写死：

| 位置 | 旧值 | 问题 |
|---|---|---|
| 页面容器 | `#f0f2f5` | 暗色下整页浅灰 |
| Card | `#fff` | 暗色下白色卡片 |
| 标题 | `#000` | 暗色下黑字压深底 |
| 空态图标 | `#d9d9d9` | 暗色下过亮 |

**修复**：

- 页面容器 → `var(--color-page-background)`
- Card 非全屏 → `undefined`（交回 Antd `colorBgContainer`，暗色 `#1e293b`）
- 标题 → `var(--color-text-primary)`
- 空态图标 → `var(--color-text-tertiary)`
- 全屏大屏保留原有深色玻璃风格

### 14.2 个人中心

`pages/(main)/profile/index.tsx` 的 `DESIGN.colors` 被大量内联样式引用：

```ts
surface: '#ffffff',
border: '#e2e8f0',
text: '#1e293b',
textMuted: '#64748b',
bgSubtle: '#f8fafc',
```

暗色下会导致整页白底、深色文字、浅色边框。

**修复**：全部改为主题令牌：

```ts
surface: 'var(--color-surface-primary)',
border: 'var(--color-border-primary)',
text: 'var(--color-text-primary)',
textMuted: 'var(--color-text-tertiary)',
bgSubtle: 'var(--color-background-secondary)',
```

### 14.3 管理后台 SystemOverview / QuickActions

两个组件的 `DESIGN_SYSTEM` / `DESIGN` 同样硬编码白卡、深色文字、浅色边框。

**修复**：

- `surface` / `surfaceAlt` / `surfaceSubtle` → `--color-surface-primary` / `--color-background-secondary`
- `border` → `--color-border-primary`
- `text` / `textPrimary` → `--color-text-primary`
- `textMuted` / `textSecondary` → `--color-text-tertiary`
- `SystemOverview` 卡片渐变 → `linear-gradient(135deg, var(--color-background-primary) 0%, var(--color-background-secondary) 100%)`
- 一处 `${textSecondary}15` 的 hex-alpha 拼接改为 `color-mix(in srgb, var(--color-text-tertiary) 8%, transparent)`

### 14.4 验证

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- Jest：设计系统 + 侧栏 **5 suites / 64 tests passed**
- postcss / `git diff --check`：通过

### 14.5 后续

- 报表页系列色仍可继续迁移到 `useChartTheme().palette`。
- `agent-ops-demo/prototype.module.css` 是独立原型页，仍使用白色画布；如需在主应用暗色下展示，建议单独加 `.dark` 覆盖或明确标注为亮色原型。

---

## 15. 第七轮：全量图表主题接入（2026-11-13 深夜）

### 15.1 Recharts 全量覆盖

此前只有共享 `ReportsCharts`、工单深度分析、流程趋势图接入 `useChartTheme()`。本轮补齐剩余 8 个报表/分析页：

| 文件 | 改动 |
|---|---|
| `reports/tickets/index.tsx` | Pie/Bar 系列色改用 `chart.palette`；统计值改用语义前景令牌 |
| `reports/sla-performance/index.tsx` | 达标/违规/预警色改用 `chart.palette` |
| `reports/service-catalog-usage/index.tsx` | 服务类型/状态分布改用 palette；状态色按 chart.isDark 选择中性色 |
| `reports/change-success/index.tsx` | 状态/类型分布改用 palette；统计值令牌化 |
| `reports/problem-efficiency/index.tsx` | 状态/优先级系列色改用 palette；统计值/进度条令牌化 |
| `reports/incident-trends/index.tsx` | `colors` 对象改用 palette；优先级 Bar 使用 palette |
| `reports/cmdb-quality/index.tsx` | 完整度/准确度/一致度改用 palette |
| `tickets/analytics/index.tsx` | 趋势线/状态/优先级改用 palette；超期统计令牌化 |

验证：仓库中所有 `from 'recharts'` 文件现在都包含 `useChartTheme`。

### 15.2 @ant-design/charts 仪表盘

6 个仪表盘图接入 G2 暗色主题与亮/暗 palette：

| 文件 | 改动 |
|---|---|
| `dashboard/components/PeakHoursChart.tsx` | `theme: classicDark/classic`；柱色 palette |
| `dashboard/components/UserSatisfactionChart.tsx` | 主题/gradient/label 色改用 palette |
| `dashboard/components/TeamWorkloadChart.tsx` | 主题/系列色/label 色改用 palette + axisText |
| `dashboard/components/ResponseTimeChart.tsx` | 主题/柱色/label 色改用 palette |
| `dashboard/components/IncidentDistributionChart.tsx` | 主题/扇区色/statistic 标题与数值色改用 palette/axisText |
| `dashboard/components/TicketTrendChart.tsx` | 主题/四条系列色改用 palette |

G2 的 `theme` 入参支持字符串，经 `inferTheme` 转成 builtin theme：

```ts
theme: chart.isDark ? 'classicDark' : 'classic'
```

### 15.3 页面级设计对象补漏

| 文件 | 改动 |
|---|---|
| `components/business/SLAMonitorDashboard.tsx` | 非全屏页面底/Card/标题/空态图标改令牌；全屏大屏保留深色玻璃 |
| `pages/(main)/profile/index.tsx` | `DESIGN.colors.surface/border/text/textMuted/bgSubtle` 改令牌 |
| `pages/(main)/admin/components/SystemOverview.tsx` | 卡片渐变/边框/文字改令牌；hex-alpha 拼接改 `color-mix` |
| `pages/(main)/admin/components/QuickActions.tsx` | surface/border/text/textMuted 改令牌 |

### 15.4 验证

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- Jest：设计系统 + 侧栏 **5 suites / 64 tests passed**
- postcss / `git diff --check`：通过

### 15.5 后续

- `@ant-design/charts` 的 `classicDark` 主题由 G2 内置，轴/图例/Tooltip 颜色与设计系统 slate 色阶仍有细微差异；如需完全一致，可后续传入 G2 theme token 覆盖。
- 图表系列色已全部走 `chart.palette`，后续只需维护 `lib/design-system/chart-theme.ts` 一处。

---

## 16. 测试契约收口（2026-11-13 深夜）

全量测试初次运行时出现 3 个真实失败，均为**既有契约漂移**，与暗色主题改动无直接关系。本轮一并收口：

| 测试 | 原因 | 修复 |
|---|---|---|
| `src/lib/api/__tests__/ai-api.test.ts` | SSE `error` 事件无 `errorCode` 时，实现仍以 `onError(message, undefined)` 调用，旧契约断言只接受一个参数 | `ai-api.ts` 改为：有 `errorCode` 才传第二参数，否则只传 message |
| `src/components/user/__tests__/ProviderMembersCard.test.tsx` | 角色词表已把 `provider_agent` 映射为「服务商坐席」，测试仍断言裸 code 出现两次 | 断言改为 `getAllByText('服务商坐席').length === 2` |
| `src/lib/ai/page-context/__tests__/registry.test.ts` | 注册表新增 `/skills`、`/skills-install`，但自动生成的 `route-paths.ts` 未同步 | 补入 `ROUTE_PATHS` |

### 验证

```text
npx jest --coverage=false --silent --maxWorkers=2
Test Suites: 380 passed, 380 total
Tests:       13 skipped, 4841 passed, 4854 total
```

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- 之前并行运行的 worker `SIGTERM` 在 `--maxWorkers=2` 下未再出现，属于资源压力而非代码失败。

---

## 17. 238 演示栈部署记录（2026-11-13 22:24 CST）

### 17.1 发布物

| 项 | 值 |
|---|---|
| 源基线 | `fbd9c128c`（与 238 当前后端/前端镜像同源） |
| 前端发布提交 | `cad6c1d74`（仅 `itsm-frontend/src` 的暗色主题/图表主题变更） |
| 前端镜像 | `itsm-frontend:1.8.0-dev.cad6c1d7` |
| 后端/worker 镜像 | 保持 `itsm-backend:1.8.0-dev.fbd9c128`（内容未变） |
| AI 服务镜像 | 保持 `itsm-ai-service:1.8.0-dev.fbd9c128`（内容未变） |
| 版本别名 | 已把 backend/ai retag 为 `1.8.0-dev.cad6c1d7`，保证 `.env.demo` 版本一致时 compose 不尝试拉远端镜像 |
| 远程分支 | `deploy-dark-cad6c1d7`（保留用于复现/回滚） |

### 17.2 部署方式

为避免污染 238 主工作树（`deploy-demo-customer-621ff444` 有未提交改动）：

1. 本地用临时 index 只提交 `itsm-frontend/src`，生成 thin bundle；
2. 在 238 创建独立 clean worktree `/home/n9e/itsm/repo-dark-deploy`；
3. 从该 worktree 用生产 Dockerfile `--target production` 构建前端镜像；
4. 备份 `.env.demo` 为 `.env.demo.bak-dark-20261113-222426`，将 `VERSION` 改为 `1.8.0-dev.cad6c1d7`；
5. `docker compose ... up -d --no-deps itsm-frontend-demo`；
6. 验证后删除临时 worktree 与 bundle。

### 17.3 验证结果

```text
itsm-frontend-demo|itsm-frontend:1.8.0-dev.cad6c1d7|Up (healthy)
index=200
health=200
```

- 静态产物包含暗色主题标记 `recharts-pie-label-text`（`/static/index-BcFuQ7fV.css`）。
- `curl http://127.0.0.1:8089/api/v1/version` 仍返回后端 `1.8.0-dev.fbd9c128`（本次未改后端）。
- 浏览器实测（`http://172.18.3.238:8089/`，`localStorage['itsm-theme']='dark'`）：

| 元素 | 实测值 |
|---|---|
| `html.class` | `dark` |
| `body` 背景 / 文字 | `rgb(15,23,42)` / `rgb(248,250,252)` |
| `h1` | `rgb(248,250,252)` |
| `p` | `rgb(203,213,225)` |

### 17.4 回滚

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-dark-20261113-222426 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml up -d --no-deps itsm-frontend-demo
```

回滚后前端将使用备份中的旧 `VERSION` 镜像。
---

## 18. 暗色主背景统一（2026-11-13 22:43 CST）

### 18.1 差异点定位

浏览器实测（238 `/dashboard`，`html.dark`）：

| 区域 | 修复前背景 | 来源 |
|---|---|---|
| body / `--color-background-primary` | `#0f172a` | `theme-variables.css` / `globals.css` |
| `.ant-layout` | `#0f172a` | `antd-layout-overrides.css` 暗色分支 |
| `.ant-layout-sider` | `#0f172a` | `Sidebar.module.css` |
| `.ant-layout-content` | `#0f172a` | `MainLayout` 主题类 |
| **`.ant-layout-header`** | **`#1e293b`** | 两处：`header/Header.module.css` 与 `antd-layout-overrides.css` 暗色分支都取 `--color-bg-secondary` |

因此视觉上出现两种「主背景色」：页面/侧栏/内容为 `#0f172a`，顶栏为 `#1e293b`。

### 18.2 修复

统一顶栏到页面主背景：

- `src/components/layout/header/Header.module.css` 暗色 `.header`
  - `var(--color-bg-secondary)` → `var(--color-background-primary)`
- `src/styles/antd-layout-overrides.css` 暗色 `.ant-layout-header`
  - `var(--color-bg-secondary)` → `var(--color-background-primary)`
  - 系统暗色分支同步

卡片/输入/弹层继续使用 `--color-surface-primary`（暗色 `#1e293b`），作为内容表面而非页面背景。

### 18.3 实测（238 `/dashboard`）

| 元素 | 计算背景 |
|---|---|
| body | `rgb(15,23,42)` |
| `.ant-layout` | `rgb(15,23,42)` |
| `.ant-layout-header` | **`rgb(15,23,42)`** |
| `.ant-layout-sider` | `rgb(15,23,42)` |
| `.ant-layout-content` | `rgb(15,23,42)` |
| `.ant-card`（surface） | `rgb(30,41,59)` |

结论：暗色主背景已统一为 `#0f172a`；`#1e293b` 仅保留在卡片/浮层等内容表面。

### 18.4 部署

| 项 | 值 |
|---|---|
| 前端提交 | `3481c4db8` |
| 前端镜像 | `itsm-frontend:1.8.0-dev.3481c4db` |
| 容器 | `itsm-frontend-demo`：healthy |
| 入口 | http://172.18.3.238:8089 |
| `.env.demo` 备份 | `.env.demo.bak-dark-unify-20261113-224344` |

回滚：

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-dark-unify-20261113-224344 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```---

## 19. 全站业务页面级背景统一（2026-11-13 23:25 CST）

### 19.1 全量调查

使用 Playwright 在 238 暗色登录态下分两批检查 **75 个主/子路由**，并按“页面根容器 / 全宽页头 / 全宽筛选栏 / 卡片 / 表格 / 弹层”分类取样。

结论：全局 token 没有错，问题是部分历史模板把 **内容 surface** 用在了 **页面级 chrome** 上。

| 来源 | 影响路由 | 修复前实测 |
|---|---|---|
| `BusinessPageTemplate.tsx:305` 页头 | `/incidents`、`/problems`、`/changes`、`/assets` | 全宽 `1312×233`，`#1e293b` |
| `BusinessPageTemplate.tsx:374` 搜索/筛选栏 | 同上 | 全宽 `1312×65`，`#1e293b` |
| `TicketsLayout.tsx:24` 面包屑栏 | 所有 `/tickets/*` | 全宽 `1312×70`，`#1e293b` |
| `pages/(main)/tickets/index.tsx:171` 页头 | `/tickets` | 全宽 `1312×205`，`#1e293b` |
| `pages/(main)/profile/index.tsx:369` 页根 | `/profile` | 整页 `1312×1000`，`#1e293b`（视口 76.1%） |
| `WorkflowToolbar.tsx:126` 设计器工具栏 | `/workflow/designer` | 全宽 `1312×65`，`#1e293b` |

`TicketsLayout`、`WorkflowToolbar` 的根因是 `bg-white` 被全局暗色规则映射为 `surface-primary`；`profile` 则直接使用 `--color-background-secondary` 作为整页背景。

其余大面积 `#1e293b` 均为内容表面，不是页面背景：`.ant-card`、`.ant-table`、表格 fixed 单元格、富文本编辑器 `#141414`、AntD 信息提示 `#111f28`、图表渐变等。

### 19.2 统一约束

新增共享页面级 chrome 类（`globals.css`）：

```css
.page-chrome {
  background-color: var(--color-background-primary);
  border-color: var(--color-border-primary);
}
```

约束：

- 页面级 chrome（页头、面包屑、搜索/筛选栏、工具栏、页脚）→ `.page-chrome`
- 内容表面（卡片、表格、弹层、表单控件）→ `--color-surface-primary`
- 一级页面根容器 → `--color-page-background` / `--color-background-primary`

### 19.3 修复文件

- `src/components/layout/BusinessPageTemplate.tsx`：页头 + 搜索/筛选栏改为 `page-chrome`（2 处）
- `src/layouts/TicketsLayout.tsx`：面包屑栏改为 `page-chrome`
- `src/pages/(main)/tickets/index.tsx`：票单页头改为 `page-chrome`；loading skeleton 与页根对齐
- `src/pages/(main)/profile/index.tsx`：`bgSubtle` 改为 `var(--color-page-background)`
- `src/components/workflow/designer/WorkflowToolbar.tsx`：工具栏改为 `page-chrome`
- `src/components/errors/GlobalError.tsx`：根错误页使用 `var(--color-page-background)`
- `src/components/layout/ResponsiveLayout.tsx`：页头/页脚改为 `page-chrome`
- `src/components/layout/PageLayout.tsx`：页头/侧栏/标题区/页脚背景改为 `var(--color-background-primary)`
- `src/styles/globals.css`：新增 `.page-chrome`
- `src/components/layout/__tests__/page-background-consistency.test.ts`：新增静态回归测试，禁止页面级模板重新使用 `bg-white`/`surface-primary`

### 19.4 验证

```text
npx tsc --noEmit                                  PASS
npm run build                                     PASS (51s)
npx jest --maxWorkers=2 --coverage=false          381 suites passed / 4846 tests passed / 13 skipped
```

浏览器终检（238，`html.dark`）：

- 75 个主/子路由：未再发现页面根容器或全宽 chrome 使用 `surface(#1e293b)`
- 16 个代表路由：`body`、`main#main-content`、全部 `.page-chrome` 均为 `rgb(15,23,42)`
- `.page-chrome` 下边框：`rgb(51,65,85)`（`#334155`）
- 卡片/表格仍保持 `rgb(30,41,59)`；表头/悬停行 `rgb(39,50,67)`；富文本编辑器 `rgb(20,20,20)` 均只出现在内容卡片内

### 19.5 部署

| 项 | 值 |
|---|---|
| 前端发布提交 | `43ff5576` |
| 前端镜像 | `itsm-frontend:1.8.0-dev.43ff5576` |
| 后端/AI 版本别名 | `1.8.0-dev.fbd9c128` → `1.8.0-dev.43ff5576`（仅 retag） |
| 容器 | `itsm-frontend-demo`：healthy |
| 入口 | http://172.18.3.238:8089 |
| `.env.demo` 备份 | `.env.demo.bak-dark-page-chrome-20261113-232017` |
| 远程分支 | `deploy-page-chrome-43ff5576` |

验证：`/` = 200，`/health` = 200；浏览器暗色路由矩阵全部通过。

### 19.6 回滚

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-dark-page-chrome-20261113-232017 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```---

## 20. 硬编码颜色审计与第一轮优化（2026-11-14 08:06 CST）

完整报告见 [`HARDCODED_COLOR_AUDIT.md`](./HARDCODED_COLOR_AUDIT.md)。

- 新增审计工具：`itsm-frontend/scripts/audit-hardcoded-colors.mjs`，入口 `npm run audit:colors`
- 初始扫描：2037 个原生颜色字面量；非 token 1608 个；组件内联 309 行 / 88 文件
- 第一轮定向优化后：1787 个；非 token 1358 个；组件内联 202 行 / 74 文件
- 未做机械替换：图表 SVG、颜色选择器、`${color}14` 拼接、品牌色、全局 AntD 兼容覆盖均保留
- 发布：`itsm-frontend:1.8.0-dev.de6b6831`（commit `de6b6831`），container healthy
- `.env.demo` 备份：`.env.demo.bak-color-optimize-20261114-080619`
- 回滚分支：`deploy-color-optimize-de6b6831`

后续仍需治理：Tailwind palette 类（1938 次）、`QuickActions`/`SystemOverview`/MSP dashboard 的颜色拼接对象、图表 palette、SVG marker、`constants/taxonomy.ts` 旧 AntD 调色板、`globals.css` 内的历史硬编码覆盖。---

## 21. 第二轮语义色优化（2026-11-14 09:15 CST）

完整记录见 [`HARDCODED_COLOR_AUDIT.md`](./HARDCODED_COLOR_AUDIT.md) §12。

- `PageStats` 的 `${color}14` 拼接改为 `color-mix(in srgb, var(--color-*) 8%, transparent)`
- `TicketStatusConfig` / `TicketPriorityConfig` 的 `bgColor/textColor` 全部 token 化
- 管理台、知识库、许可证、发布、报表、SLA、工单模板等统计色改为 `--color-*-text`
- 原生颜色字面量：**1787 → 1653**
- 非 token 字面量：**1358 → 1224**
- 组件内联行：**202 → 119**
- 发布：`itsm-frontend:1.8.0-dev.7e62253f`（commit `7e62253f`），container healthy
- `.env.demo` 备份：`.env.demo.bak-color-optimize2-20261114-091025`
- 回滚分支：`deploy-color-optimize-7e62253f`

当前剩余硬编码主要集中在 `QuickActions` / `SystemOverview` / MSP dashboard 的动态颜色拼接、Recharts/SVG palette、`cmdb/topology` marker、`globals.css` 的 AntD 兼容层和 Tailwind palette 类。下一步需要做组件级重构，而不是字符串替换。---

## 22. 项目资源负荷热图暗色适配（2026-11-14 09:45 CST）

详细记录见 [`HARDCODED_COLOR_AUDIT.md`](./HARDCODED_COLOR_AUDIT.md) §13。

- 页面：`/projects/5` →「资源负荷」
- 修复前：单元格 `#fafafa` 背景 + `#f8fafc` 文字，对比度 **1.00:1**
- 修复后：单元格改为 `color-mix` 语义 tint，主文字 `--color-text-primary`，次文字 `--color-text-secondary`
- 暗色实测对比度：
  - 空：9.90
  - ≤50%：10.10
  - ≤80%：10.10
  - ≤100%：7.53
  - \>100%：11.28
- 亮色同时保持浅底深字
- 发布：`itsm-frontend:1.8.0-dev.537ad008`（commit `537ad008`），container healthy
- `.env.demo` 备份：`.env.demo.bak-resource-heatmap-20261114-094512`
- 回滚分支：`deploy-resource-heatmap-537ad008`---

## 23. 富文本编辑器暗色适配（2026-11-14 10:23 CST）

详细记录见 [`HARDCODED_COLOR_AUDIT.md`](./HARDCODED_COLOR_AUDIT.md) §14。

### 23.1 结论

- 项目模块的项目/任务描述是 `Input.TextArea`，项目页没有 HTML/富文本编辑器。
- 真正需要适配的是全局 `RichTextEditor`（Tiptap），用于工单创建/回复、动态字段和 CMDB 表单。
- 修复前暗色正文/工具栏为 `#141414` / `#1f1f1f`，与全站背景 `#0f172a`、卡片 `#1e293b` 不一致；占位符亮色仅约 **1.9:1**、暗色约 **3.4:1**。
- 修复后 `--rte-*` 全部由设计 token 派生，编辑器在亮/暗主题下均达到 WCAG AA 文本对比度。

### 23.2 暗色实测（238 `/tickets/create`）

| 元素 | 实际颜色 | 对比度 |
|---|---|---:|
| 页面/正文底 | `rgb(15,23,42)` | — |
| 工具栏 | `rgb(30,41,59)` | — |
| 正文文字 | `rgb(248,250,252)` on `rgb(15,23,42)` | **17.06:1** |
| 工具栏按钮 | `rgb(248,250,252)` on `rgb(30,41,59)` | **13.98:1** |
| 占位符 | `rgb(148,163,184)` on `rgb(15,23,42)` | **6.96:1** |
| 引用 | `rgb(203,213,225)` on `rgb(30,41,59)`，左边框 `rgb(71,85,105)` | 高对比 |
| 行内代码 | `rgb(248,250,252)` + 12% 白 tint | 高对比 |
| 代码块 | `rgb(248,250,252)` on `rgb(51,65,85)` | 高对比 |
| 链接 | `rgb(96,165,250)` | 约 7:1 |
| 表格/分隔线 | 边框 `rgb(51,65,85)` | — |

亮色同步回归：

| 元素 | 实际颜色 | 对比度 |
|---|---|---:|
| 正文底 | `rgb(255,255,255)` | — |
| 正文文字 | `rgb(15,23,42)` on `rgb(255,255,255)` | **17.85:1** |
| 工具栏按钮 | `rgb(100,116,139)` on `rgb(248,250,252)` | **4.55:1** |
| 占位符 | `rgb(100,116,139)` on `rgb(255,255,255)` | **4.76:1** |

浏览器 console error / page error：**0**。

### 23.3 测试与部署

- `npx tsc --noEmit`：通过
- `rte-theme-consistency.test.ts`：2 tests passed
- `npm run build`：通过
- 发布：`itsm-frontend:1.8.0-dev.bae83019`（commit `bae83019`），container `itsm-frontend-demo` healthy
- `/` = 200，`/health` = 200
- `.env.demo` 备份：`.env.demo.bak-rte-theme-20261114-102314`
- 回滚分支：`deploy-rte-theme-bae83019`

回滚命令见 [`HARDCODED_COLOR_AUDIT.md`](./HARDCODED_COLOR_AUDIT.md) §14.6。
