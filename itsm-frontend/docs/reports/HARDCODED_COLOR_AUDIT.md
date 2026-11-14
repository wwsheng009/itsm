# 前端硬编码颜色审计

> 扫描日期：2026-11-13  
> 范围：`itsm-frontend/src/**/*.{ts,tsx,css}`，排除 `__tests__` / `*.test.*` / `*.spec.*`  
> 工具：`itsm-frontend/scripts/audit-hardcoded-colors.mjs`  
> 命令：`npm run audit:colors`（`--json` 输出机器可读结果）

## 1. 结论

**存在大量硬编码颜色，但主要不是页面级背景问题。** 页面级背景已在 `DARK_THEME_COLOR_AUDIT.md` §19 统一为 `--color-background-primary`；当前剩余硬编码集中在：

1. 组件内联语义色（状态色、统计卡、图标色）
2. 全局 CSS / CSS Module 的历史覆盖与 fallback
3. 图表 palette、常量与数据 hook
4. Tailwind 调色板工具类（`bg-blue-50`、`text-slate-900` 等）

没有发现 `bg-[#...]` / `text-[#...]` 这类 Tailwind 任意值 hex 类；这一点是好的。

## 2. 总量

| 指标 | 数量 |
|---|---:|
| 扫描文件 | 995 |
| 原生颜色字面量总数 | **2037 次 / 1831 行** |
| 其中 token 定义文件（允许） | 429 次 / 393 行（7 个文件） |
| 非 token 原生颜色字面量 | **1608 次** |
| 组件内联样式中的原生颜色 | **309 行 / 88 文件** |
| CSS 文件中的原生颜色 | **589 行 / 9 文件** |
| 其它 TS/JS 常量、图表、配置 | **540 行 / 118 文件**（不含已归入内联样式的行） |
| Tailwind 调色板类 | **1942 次 / 235 文件** |
| Tailwind 任意 hex 类 | **0** |

> Token 定义文件：`styles/theme-variables.css`、`lib/design-system/colors.ts`、`lib/design-system/chart-theme.ts`、`lib/design-system/theme.tsx`、`lib/design-system/spacing.ts`、`design-system/tokens/index.ts`、`lib/color-contrast.ts`。这些文件中的具体值属于主题真源，不应按“组件硬编码”处理。

## 3. 最常见原生颜色

| 次数 | 颜色 | 典型用途 |
|---:|---|---|
| 114 | `#52c41a` | AntD 成功绿 / 统计值 / 图标 |
| 90 | `#1890ff` | AntD 主蓝 / 统计值 / 图标 |
| 82 | `#ff4d4f` | AntD 错误红 / 图标 |
| 65 | `#faad14` | AntD 警告黄 / 图标 |
| 50 | `#3b82f6` | 设计系统主蓝 / 图表 |
| 41 | `#fa8c16` | AntD 橙 / 统计值 |
| 35 | `#ffffff` | 彩色按钮字色 / 卡片 / fallback |
| 32 | `#1e293b` | 卡片 surface |
| 29 | `#2563eb` | 主色深阶 |
| 24 | `#0f172a` | 页面底 / 深色文字 |
| 20 | `#1677ff` | AntD 蓝 |
| 20 | `#d9d9d9` | 灰色 / 占位 |
| 21 | `#8c8c8c` | 灰色 / 占位 |
| 17 | `#f59e0b` | 警告橙 |
| 14 | `#722ed1` | 紫色状态 |

这些值中，`#52c41a`、`#1890ff`、`#ff4d4f`、`#faad14`、`#fa8c16`、`#1677ff` 属于 AntD 旧调色板，应该逐步映射到本项目的 `--color-*-text` / `--color-*` / `--color-*-bg` 语义令牌。

## 4. 高风险：组件内联样式

309 行组件内联样式仍包含原生颜色。重点文件：

| 行数 | 文件 |
|---:|---|
| 31 | `pages/(main)/incidents/components/IncidentList.tsx` |
| 13 | `pages/(main)/admin/components/QuickActions.tsx` |
| 12 | `pages/(main)/dashboard/hooks/useDashboardData.ts` |
| 11 | `pages/(main)/dashboard/charts.tsx` |
| 10 | `pages/(main)/changes/index.tsx` |
| 8 | `pages/(main)/problems/index.tsx` |
| 7 | `components/ticket/TicketKanban.tsx` |
| 7 | `pages/(main)/msp/dashboard/index.tsx` |
| 6 | `components/business/BatchOperationConfirm.tsx` |
| 6 | `components/problem/EnhancedProblemDetail.tsx` |
| 6 | `pages/(main)/problems/trends/index.tsx` |
| 6 | `pages/(main)/workflow/dashboard/index.tsx` |

迁移原则：

- `color: '#...'` / `valueStyle: { color: '#...' }` → 优先 `var(--color-*-text)`，保证暗色对比度。
- `background: '#...'` / `backgroundColor: '#...'` → 优先 `var(--color-*-bg)`（软底）或 `var(--color-background-*/surface-*)`。
- 图标 `style={{ color: '#...' }}` → `var(--color-primary|success|warning|error)` 或对应 `*-text` 令牌。
- `borderColor` → `var(--color-border-primary|secondary)`。
- **不要**把图表 `fill`/`stroke`、颜色选择器初始值、需要 `${color}14` 字符串拼接的值机械替换成 CSS 变量。这几点已在上一轮审计中确认过风险。

## 5. 中风险：CSS 文件

| 行数 | 文件 | 说明 |
|---:|---|---|
| 469 | `styles/globals.css` | 包含 token 定义、AntD 覆盖、历史组件样式；需按区块人工治理，不能整文件机械替换 |
| 46 | `pages/agent-ops-demo/prototype.module.css` | demo 页面独立视觉稿，优先级低 |
| 39 | `components/layout/header/Header.module.css` | 阴影、白色 badge 字色、少量 fallback |
| 9 | `components/ui/typography.css` | 排版色 |
| 9 | `styles/antd-layout-overrides.css` | AntD 布局覆盖 |
| 8 | `components/ui/animations.css` | 动画关键帧中的颜色 |
| 5 | `components/layout/sidebar/Sidebar.module.css` | 侧栏局部色/阴影 |
| 3 | `lib/error-handler.module.css` | 错误页局部色 |
| 1 | `components/common/BusinessStatsGrid.module.css` | 仅有注释中的 hex |

建议优先级：`Header.module.css`、`typography.css`、`animations.css`、`Sidebar.module.css` 先治理；`globals.css` 按 token / AntD 覆盖 / 业务样式分段处理。

## 6. 图表与常量：不能盲改

其它 TS/JS 常量中的 540 行原生颜色，重点文件：

| 行数 | 文件 |
|---:|---|
| 61 | `components/common/BusinessStatsGrid.tsx` |
| 33 | `constants/taxonomy.ts` |
| 30 | `components/cmdb/TopologyGraph.tsx` |
| 29 | `components/business/SLAMonitorDashboard.tsx` |
| 19 | `lib/services/analytics-service.ts` |
| 14 | `components/common/rich-text/RichTextEditor.tsx` |
| 13 | `pages/(main)/admin/components/SystemOverview.tsx` |
| 11 | `pages/(main)/admin/cmdb-types/index.tsx` |
| 11 | `pages/(main)/admin/components/QuickActions.tsx` |
| 10 | `components/errors/GlobalError.tsx` |
| 10 | `lib/config/unified-config.ts` |

处理建议：

- **图表**：统一由 `useChartTheme()` 输出亮/暗 palette；不要在 chart 组件里散落 hex。
- **状态/优先级**：优先使用 `--status-*`、`--priority-*`、`--sla-*`；需要软底时用 `--color-*-bg`。
- **设计对象**（`QuickActions`、`SystemOverview`、MSP dashboard 等）：如果 hex 参与字符串拼接，先改成明确的语义 token 组合，不能直接替换成 `var()`。
- **富文本编辑器**：`#141414` 等编辑器内部主题色应由编辑器主题对象集中管理，并在暗色/亮色切换时更新。
- **品牌图标**：第三方登录图标色（微信/QQ/钉钉）属于品牌色，允许保留独立常量。

## 7. Tailwind 调色板

`bg-blue-50`、`text-slate-900`、`border-gray-200` 这类类名共 **1942 次 / 235 文件**。它们不是 hex，但仍然是固定调色板：

- 全局 `globals.css` 已对部分 `50/100` 软底和 `600~800` 文字做暗色兜底；
- **不是所有调色板都覆盖**，例如实色 `bg-blue-500`、部分边框色、部分 `text-slate-*`；
- 迁移时优先改成项目语义类/令牌，而不是继续扩展全局 `.dark .bg-*` 兜底规则。

Top 文件：`styles/globals.css`、`auth/AuthLayout.tsx`、`marketplace/$id`、`msp/dashboard`、`skills-install`、`ResponsiveLayout.tsx`、`AIMetrics.tsx`、`installations`、`approvals`、`dashboard` 等。

## 8. 建议治理顺序

1. **P1：组件内联语义色**
   - 先处理 246 行含 `#52c41a/#1890ff/#ff4d4f/#faad14/#fa8c16/#1677ff` 的行；
   - 使用 `--color-*-text` / `--color-*` / `--color-*-bg`；
   - 排除图表 SVG、颜色选择器和字符串拼接场景。
2. **P1：CSS Module**
   - `Header.module.css`、`Sidebar.module.css`、`typography.css`、`animations.css`；
   - 阴影统一改用 `--shadow-*`，颜色改用 `--color-*`。
3. **P2：状态/优先级/图表**
   - 状态常量迁移到 `--status-*` / `--priority-*` / `--sla-*`；
   - 图表统一由 `useChartTheme()` 出 palette。
4. **P3：Tailwind 调色板**
   - 按模块逐步替换为语义类；
   - 不再新增任何 `.dark .bg-<palette>` 兜底规则，除非有明确测试保护。

## 9. 工具与防回归

```bash
cd itsm-frontend
npm run audit:colors
node scripts/audit-hardcoded-colors.mjs --json > /tmp/color-audit.json
```

当前工具是**审计**而非构建门禁，不会让现有代码失败。后续可在治理到可接受阈值后增加：

- CI 预算检查（`nontoken > baseline` 即失败）；
- ESLint 自定义规则禁止组件内联 raw hex；
- 图表色/状态色的统一 token 测试。

## 10. 明确不计入问题的项

- `var(--color-*, #fallback)` 的 fallback 值：用于样式表加载失败时兜底，扫描会统计为 raw literals，但不应简单删除。
- AntD `<Tag color="green">` 的 `green/blue/red` 等：语义名，由 AntD 主题算法处理，不是 raw hex。
- `rgba(0, 0, 0, ...)` 阴影：建议逐步迁移到 `--shadow-*`，但不影响暗色背景一致性。
- 第三方品牌色（微信/QQ/钉钉等）：允许保留。---

## 11. 第一轮定向优化（2026-11-14 08:06 CST）

### 11.1 原则

不做按颜色字符串全量替换。先区分四类场景：

1. **UI 文字/图标色** → `--color-*-text` / `--color-*`
2. **软底/状态 pill** → `--color-*-bg` / `--color-primary-soft`
3. **共享卡片表面/边框** → `--color-surface-*` / `--color-border-*`
4. **图表 SVG、颜色选择器、会参与 `${color}14` 拼接的配置** → 保留原值，后续按组件重构

### 11.2 已迁移

- `BusinessStatsGrid.tsx` + `.module.css`
  - 删除 6 组浅色 + 6 组暗色硬编码 palette（约 100 行）
  - 改为 6 个 tone class + `color-mix()` 从 `--color-primary/success/warning/error/info/purple-text` 派生
  - 浅色/暗色共用同一套语义 token
- `IncidentList.tsx`
  - 事件状态、优先级、图标底色、删除警告全部改为 `--color-*-text` / `--color-*-bg`
- `TicketKanban.tsx`
  - 状态列颜色改为语义 token；截止时间错误色改为 `--color-error-text`
- `BatchOperationConfirm.tsx`
  - 批量操作图标/提示色改为语义 text token
- `DashboardOverview.tsx`
  - 统计数字颜色改为语义 text token
- `EnhancedProblemDetail.tsx`
  - 关联事件/变更/知识头像、Badge、统计值改为语义 token
- `problems/trends`、`workflow/automation`、`workflow/dashboard`、`workflow/sla`
  - 统计值和 SLA 状态颜色改为语义 text token
- `sla-monitor`、`service-requests`、`tickets/dashboard`
  - 统计值颜色改为语义 text token
- `changes`、`problems`、`incidents`
  - 看板列颜色改为语义 token（CSS var 在 `backgroundColor/borderLeft` 中已验证可用）
- `Header.module.css` / `Sidebar.module.css`
  - 激活态、在线状态、徽标、阴影改为语义色/`--shadow-*`
- `RichTextEditor.tsx`
  - 图片手柄、菜单、引用、代码块、表格边框改为 `--color-*` / `--shadow-lg`
- `typography.css` / `animations.css`
  - 渐变文字、打字机光标、高亮、焦点动画改为语义 token/阴影
- `error-handler.module.css`
  - 错误标题、重试按钮改为语义 token

### 11.3 指标变化

| 指标 | 优化前 | 优化后 | 变化 |
|---|---:|---:|---:|
| 原生颜色字面量 | 2037 | **1787** | -250 |
| 非 token 字面量 | 1608 | **1358** | -250 |
| 组件内联行 | 309 | **202** | -107 |
| CSS 行 | 589 | **557** | -32 |
| 其它 TS/JS 常量行 | 540 | **452** | -88 |
| Tailwind palette 类 | 1942 | **1938** | -4 |

### 11.4 刻意未迁移

- `QuickActions.tsx`、`SystemOverview.tsx`、MSP dashboard 中参与 `${color}14` / `${color}30` 拼接的颜色字段
- Recharts / AntD Progress 的 SVG `strokeColor`、图表 palette、SVG marker
- 颜色选择器初始值
- `lib/config/unified-config.ts` 的重复主题/日志色（应先收敛配置源）
- `constants/taxonomy.ts` 的 AntD 旧调色板（需先决定标签体系是否改成 token）
- 第三方品牌色（微信/QQ/钉钉）
- `globals.css` 内的 token 定义、AntD 兼容覆盖和 fallback

### 11.5 验证与部署

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- 全量 Jest：**381 suites / 4846 passed / 13 skipped**
- 浏览器复测：
  - 页面底仍为 `rgb(15,23,42)`
  - `/workflow/dashboard` 统计色解析为 `rgb(96,165,250)` / `rgb(74,222,128)` / `rgb(251,191,36)` / `rgb(248,113,113)`
  - `/changes` 看板 CSS var 解析为 primary/success/warning/error/info/purple 的实际 RGB
  - `/tickets/create` 富文本编辑器正常渲染，无 console error
- 发布：
  - commit `de6b6831`
  - image `itsm-frontend:1.8.0-dev.de6b6831`
  - container `itsm-frontend-demo` healthy；`/` = 200，`/health` = 200
  - env 备份 `.env.demo.bak-color-optimize-20261114-080619`
  - 回滚分支 `deploy-color-optimize-de6b6831`

### 11.6 回滚

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-color-optimize-20261114-080619 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```---

## 12. 第二轮定向优化（2026-11-14 09:15 CST）

### 12.1 本轮新增迁移

- `BusinessPageTemplate.tsx`
  - `StatsCard` 默认色从 `#3b82f6` 改为 `var(--color-primary)`
  - 图标软底从 `${color}14` 字符串拼接改为 `color-mix(in srgb, ${color} 8%, transparent)`
- `useIncidentStats.tsx`、`problems/index.tsx`、`changes/index.tsx`、`assets/index.tsx`
  - `PageStats.color` 全部迁移为 `var(--color-primary/success/warning/error)`
- `constants/taxonomy.ts`
  - `TicketStatusConfig` / `TicketPriorityConfig` 的 `bgColor/textColor` 改为语义 token
  - 主类型颜色映射改为 token
- `admin/service-catalogs`、`admin/roles`、`admin/permissions`、`admin/menus`、`admin/workflows`、`admin/tenants`、`admin/system-config`、`admin/sla-definitions`、`admin/approval-chains`、`admin/cmdb-types`、`admin/escalation-rules`
  - 统计数字/图标色改为语义 text token
- `knowledge`、`problems/known-errors`、`tickets/templates`、`tickets/dashboard`、`TicketDeepAnalytics`、`ChangeDetail`、`SLA monitor`
  - 统计/状态/图标色改为语义 token
- `license/LicenseList`、`release/ReleaseList`、`ReportMetrics`、`AdvancedAnalytics`
  - 统计与评分颜色改为语义 token
- `changes/$id/pir`
  - 实施结果图标改为 success/warning/error text token

### 12.2 指标变化

| 指标 | 第一轮后 | 第二轮后 | 变化 |
|---|---:|---:|---:|
| 原生颜色字面量 | 1787 | **1653** | -134 |
| 非 token 字面量 | 1358 | **1224** | -134 |
| 组件内联行 | 202 | **119** | -83 |
| CSS 行 | 557 | **557** | 0 |
| 其它 TS/JS 常量行 | 452 | **405** | -47 |
| Tailwind palette 类 | 1938 | **1938** | 0 |

从审计初始状态相比：

- 原生颜色字面量：2037 → 1653（-384）
- 非 token 字面量：1608 → 1224（-384）
- 组件内联行：309 → 119（-190）

### 12.3 浏览器验证

- 所有代表路由的 `body` / `main` 仍为 `rgb(15,23,42)`
- `/incidents`、`/changes`、`/problems`、`/assets` 的统计图标软底 `color-mix` 已解析为：
  - 主蓝 `color(srgb 0.231373 0.509804 0.964706 / 0.08)`
  - 成功绿、警告橙、错误红同源
- `/knowledge`、`/admin/roles`、`/admin/service-catalogs`、`/tickets/templates`、`/workflow/dashboard`、`/sla-monitor` 统计值颜色均为语义 RGB
- 无 console error / page error

### 12.4 验证与部署

- `npx tsc --noEmit`：通过
- `npm run build`：通过
- 全量 Jest：**381 suites / 4846 passed / 13 skipped**
- 发布：
  - commit `7e62253f`
  - image `itsm-frontend:1.8.0-dev.7e62253f`
  - container `itsm-frontend-demo` healthy；`/` = 200，`/health` = 200
  - env 备份 `.env.demo.bak-color-optimize2-20261114-091025`
  - 回滚分支 `deploy-color-optimize-7e62253f`

### 12.5 仍未迁移

下一阶段从“可安全 token 化”进入“需要组件级重构”的区域：

- `QuickActions`、`SystemOverview`、MSP dashboard：多处 `/${color}08`、`${color}14`、`${color}30` 拼接，需改成 `--action-color` + `color-mix`
- Recharts / AntD Charts / SVG marker：需要统一由 `useChartTheme()` 输出 palette
- `cmdb/topology` 与 `TopologyGraph`：SVG marker 颜色需要确认 CSS 变量在属性中的兼容性
- `constants/taxonomy.ts` 中保留的 AntD Tag 预设色名（`blue/gold/red` 等）属于主题语义名，不按 raw hex 处理
- `globals.css` 中的 AntD 兼容覆盖与 token 定义
- Tailwind palette 类（当前 1938 次；应逐步改成语义类，而不是继续扩大全局 `.dark .bg-*` 兜底）

### 12.6 回滚

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-color-optimize2-20261114-091025 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```---

## 13. 项目资源负荷热图主题适配（2026-11-14 09:45 CST）

### 13.1 问题

页面：`http://172.18.3.238:8089/projects/5` →「资源负荷」Tab。

浏览器实测（暗色）：

- 表格容器：`#1e293b`
- 周单元格内层背景：`#fafafa`
- 单元格文字：`#f8fafc`
- 对比度：**1.00:1**（几乎不可读）
- 6 个周列单元格均受影响

源码根因：

```ts
// ProjectResourceHeatmap.tsx（修复前）
heatmapCellColor() => '#fafafa' | '#d9f7be' | '#fff1b8' | '#ffe7ba' | '#ffccc7'
```

固定亮色 pastel 没有跟随主题；单元格主文字又继承表格暗色文字 token。

### 13.2 修复

- `heatmapCellColor` 改为返回语义 `color-mix()`：
  - 0：`var(--color-background-tertiary)`
  - ≤50%：success 18% + surface
  - ≤80%：warning 18% + surface
  - ≤100%：warning 32% + surface
  - >100%：error 22% + surface
- 单元格主文字显式使用 `var(--color-text-primary)`
- 次行文字使用 `var(--color-text-secondary)`
- 同步更新 `ProjectResourceHeatmap.test.ts`

### 13.3 实测（238 `/projects/5`）

| 主题 | 单元格状态 | 背景 | 文字 | 对比度 |
|---|---|---|---|---:|
| 暗色 | 空 | `rgb(51,65,85)` | `rgb(248,250,252)` | 9.90 |
| 暗色 | ≤50% | 深绿 tint | `rgb(248,250,252)` | 10.10 |
| 暗色 | ≤80% | 深黄 tint | `rgb(248,250,252)` | 10.10 |
| 暗色 | ≤100% | 深橙 tint | `rgb(248,250,252)` | 7.53 |
| 暗色 | >100% | 深红 tint | `rgb(248,250,252)` | 11.28 |
| 亮色 | 空 | `rgb(241,245,249)` | `rgb(15,23,42)` | 高对比 |
| 亮色 | 各负荷档 | 浅 pastel tint | `rgb(15,23,42)` | 高对比 |

页面级背景保持：

- 暗色 `body/main`：`rgb(15,23,42)`
- 表格：`rgb(30,41,59)`
- 无 console error / page error

### 13.4 验证与部署

- `npx tsc --noEmit`：通过
- `ProjectResourceHeatmap` Jest：6 tests passed
- `npm run build`：通过（首次 esbuild service stopped，重试成功）
- 发布：
  - commit `537ad008`
  - image `itsm-frontend:1.8.0-dev.537ad008`
  - container `itsm-frontend-demo` healthy
  - env 备份 `.env.demo.bak-resource-heatmap-20261114-094512`
  - 回滚分支 `deploy-resource-heatmap-537ad008`

### 13.5 回滚

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-resource-heatmap-20261114-094512 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```---

## 14. 富文本编辑器（RichTextEditor）主题适配（2026-11-14 10:23 CST）

### 14.1 范围确认

- 项目模块的项目/任务描述使用 AntD `Input.TextArea`（`src/pages/(main)/projects/detail.tsx` 第 2598、2705 行等），不是 HTML/富文本编辑器，因此不需要富文本暗色改造。
- 全站真正的富文本编辑器是 `src/components/common/rich-text/RichTextEditor.tsx`（Tiptap），用于工单创建/回复、动态字段、CMDB CI 表单等场景。
- 编辑态类名为 `.rich-text-editor` / `.ticket-rich-editor`，详情回显为 `.ticket-rich-text`，两者共用 `--rte-*` 色板。

### 14.2 修复前实测（238 `/tickets/create`）

| 元素 | 暗色值 | 问题 |
|---|---|---|
| 正文背景 | `#141414` | 中性黑岛，与全站 `#0f172a` 页面底和 `#1e293b` 卡片脱节 |
| 工具栏背景 | `#1f1f1f` | 同上，且与正文层级不清 |
| 占位符 | `#6b6b6b` | 暗色对比度约 **3.4:1**；亮色 `#bfbfbf` 约 **1.9:1**，均低于 4.5:1 |
| 边框/引用/代码 | `#303030` / `#1d1d1d` / `rgba(255,255,255,.12)` | 固定 AntD v5 暗色值，不随设计 token 演进 |

### 14.3 修复方式

`globals.css` 中 `:root` 与 `.dark` 的 `--rte-*` 全部改为语义 token：

| 变量 | 亮色 | 暗色 |
|---|---|---|
| `--rte-border` | `--color-border-primary` `#e2e8f0` | `--color-border-primary` `#334155` |
| `--rte-toolbar-bg` | `--color-surface-secondary` `#f8fafc` | `--color-surface-primary` `#1e293b` |
| `--rte-body-bg` | `--color-surface-primary` `#ffffff` | `--color-background-primary` `#0f172a` |
| `--rte-body-bg-disabled` | `--color-background-tertiary` `#f1f5f9` | `surface-primary 82% + background-primary` |
| `--rte-placeholder` | `--color-text-tertiary` `#64748b` | `--color-text-tertiary` `#94a3b8` |
| `--rte-quote-border` | `--color-border-secondary` | `--color-border-secondary` |
| `--rte-quote-bg` | `--color-background-secondary` | `--color-background-secondary` |
| `--rte-quote-text` | `--color-text-secondary` | `--color-text-secondary` |
| `--rte-code-bg` | `text-primary 6%` | `text-primary 12%` |
| `--rte-pre-bg` | `--color-background-tertiary` | `--color-background-tertiary` |
| `--rte-rule` | `--color-border-primary` | `--color-border-primary` |
| `--rte-link` | `--color-primary-text` `#2563eb` | `--color-primary-text` `#60a5fa` |

同时清理 `RichTextEditor.tsx` 内联样式回退值：焦点描边 `#1677ff` → `var(--color-primary)`，图片菜单/缩放手柄 `#fff`/`#1f1f1f` → surface token，浮层阴影 → `var(--shadow-lg)`，引用/代码/表格回退值 → `--rte-*` / `--color-*`。组件源码已无 hex/rgba 颜色字面量。

新增 `src/components/common/rich-text/__tests__/rte-theme-consistency.test.ts`：

- 断言 26 条 `--rte-*` 声明不得包含 hex/rgba，必须由 `var(--color-*)` 或 `color-mix()` 派生；
- 断言亮/暗两套 placeholder 都使用 `var(--color-text-tertiary)`。

### 14.4 修复后实测（238，部署 `bae83019` 后）

| 主题 | 元素 | 实际颜色 | 对比度 |
|---|---|---|---:|
| 暗色 | 页面/正文底 | `rgb(15,23,42)` | — |
| 暗色 | 工具栏 | `rgb(30,41,59)` | — |
| 暗色 | 正文文字 | `rgb(248,250,252)` on `rgb(15,23,42)` | **17.06:1** |
| 暗色 | 工具栏按钮 | `rgb(248,250,252)` on `rgb(30,41,59)` | **13.98:1** |
| 暗色 | 占位符 | `rgb(148,163,184)` on `rgb(15,23,42)` | **6.96:1** |
| 亮色 | 正文底 | `rgb(255,255,255)` | — |
| 亮色 | 工具栏 | `rgb(248,250,252)` | — |
| 亮色 | 正文文字 | `rgb(15,23,42)` on `rgb(255,255,255)` | **17.85:1** |
| 亮色 | 工具栏按钮 | `rgb(100,116,139)` on `rgb(248,250,252)` | **4.55:1** |
| 亮色 | 占位符 | `rgb(100,116,139)` on `rgb(255,255,255)` | **4.76:1** |

格式化内容与回显态实测：

- 暗色：引用文字 `#cbd5e1` / 底 `#1e293b` / 左边框 `#475569`；行内代码 `#f8fafc` + 12% 白 tint；代码块 `#f8fafc` on `#334155`；链接 `#60a5fa`；表格/分隔线边框 `#334155`。
- 亮色：引用 `#475569` on `#f8fafc`；行内代码 `#475569` + 6% 深色 tint；代码块 `#0f172a` on `#f1f5f9`；链接 `#2563eb`；表格/分隔线边框 `#e2e8f0`。
- 编辑器焦点描边为 2px `--color-primary` `#3b82f6`，在亮/暗底上均约 3.68:1，满足非文本 3:1。
- 工具栏激活态使用主按钮渐变 + 白字；暗色下边框为 `#5fa5fa`，状态可辨识。
- 浏览器 console error / page error：**0**。

### 14.5 验证与审计

- `npx tsc --noEmit`：通过
- `npx jest --runTestsByPath "src/components/common/rich-text/__tests__/rte-theme-consistency.test.ts"`：**2 tests passed**
- `npm run build`：通过
- `npm run audit:colors`：
  - 原生颜色字面量：**1653 → 1622**
  - 非 token 字面量：**1224 → 1193**
  - 组件内联样式：**119 行不变**（本轮 RTE 组件清理的是内联 fallback，主要收益在 `globals.css` token 化）
- 本次共替换 31 处固定色值（13 个 `--rte-*` × 2 主题 + 5 处组件回退色）。

### 14.6 部署与回滚

| 项 | 值 |
|---|---|
| 前端发布提交 | `bae83019` |
| 前端镜像 | `itsm-frontend:1.8.0-dev.bae83019` |
| 容器 | `itsm-frontend-demo`：healthy |
| 健康检查 | `/` = 200，`/health` = 200 |
| `.env.demo` 备份 | `.env.demo.bak-rte-theme-20261114-102314` |
| 远程回滚分支 | `deploy-rte-theme-bae83019` |

```bash
cd /home/n9e/itsm/repo
cp -a .env.demo.bak-rte-theme-20261114-102314 .env.demo
sudo docker compose --env-file .env.demo -f docker-compose.demo.yml \
  up -d --no-deps itsm-frontend-demo
```
