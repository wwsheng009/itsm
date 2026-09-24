# 前端迁移到 Vite.js：方案与实施计划

> 文档类型：技术方案 + 实施计划（Proposed）
> 适用范围：`itsm-frontend`（前端工程、构建与部署链路）；不改动 `itsm-backend` / `itsm-ai-service` 业务代码
> 编制日期：2026-09-22
> 版本：v1.0（待评审；待决策项见 §3 与 §11）
> 目标读者：前端、测试、SRE/运维、架构
> 关联文档：`docs/architecture/architecture.md`、`docs/delivery/production-readiness-program.md`、`docs/dev-commands-reference.md`、`docs/plan/generic-attachment-richtext-control-plan.md`
> 证据基线：本文数字均为 2026-09-22 在本机实测（VMware VM / Xeon E5-2686 v4 8 vCPU @2.3GHz / Node v24.15.0 / Next 15.5.24 / 15 分钟 token 有效期），命令与原始输出见 §12

---

## 0. 结论先行（TL;DR）

1. **不能"只换打包器"**：Next.js 15 的 dev/构建只走 Turbopack 或 webpack，Vite 与 `next dev` 互斥。要上 Vite，本质上是一次**框架迁移**（路由/导航/构建/部署四层），不是 `vite.config` 配置切换。
2. **但这次迁移的代码风险出奇地低**：实测 `itsm-frontend/src` 中 **0 处 `next/headers`、0 处 Server Action（`'use server'`）、0 个 `export default async function` 页面、0 个并行路由（`@*` 目录）**；166 个 `page.tsx` 中 **156 个（94%）是 `'use client'`**，数据全部由浏览器侧请求 `/api/v1/*`（react-query 38 个文件，SWR 0）。**这个前端实质上已经是 SPA，只是套着 Next 的路由壳**。
3. **收益是数量级的**：现在每次重启 dev server，全部路由退回"未编译"，单条首次访问 130~200s（`/login` 198.1s、`/tickets/create` 134.7s；webpack 模式 `/login` 345.4s），用户点菜单就是在等服务端编译。Vite 的 dev 是"依赖预打包 + 按需 ESM"，冷启动秒级、路由切换不再有服务端编译等待。
4. **迁移代价集中在三处，都不在业务组件里**：
   - ① `src/middleware.ts`（受保护路由 + 历史路径 307 重定向，框架能力）
   - ② `src/app/api/[...path]/route.ts`（BFF：JWT 校验 + 敏感路径黑名单 + X-Forwarded-For 透传）
   - ③ `next.config.ts` + 容器部署形态（`rewrites` / `images` / `output: 'standalone'` / nginx 反代）
5. **推荐路径**：**一次性迁移到 Vite（React SPA）+ 保留 Next 分支回退**，不做长期双轨。理由与替代方案见 §6。
6. **不建议为了"dev 变快"而立即全量迁移**：若只需解决"点菜单要等"，Next 15→16 升级（Turbopack 文件系统缓存默认开启）是成本低得多的替代方案；本方案的定位是**把 dev 体验与构建链路一次性拉到现代基线**的中期重构。两条路线的对比见 §3.1。

**范围**

- 前端工程从 Next.js App Router 迁移到 Vite + React SPA：路由体系、导航 API、守卫与重定向、BFF/代理、构建产物与容器部署。
- 覆盖全部 166 条路由、`src/components`（约 450 个组件文件）与 `src/lib`（API 客户端、hooks、i18n、design-system）的**不改业务语义**的搬运。
- 测试链路适配：Playwright E2E 保持可用；Jest 可选迁移到 Vitest。

**非目标**

- 不做 SSR / RSC / SEO（现状本就 0 使用，`robots`/OG 元数据见 §5.7 仅做等价降级）。
- 不重写组件、不改样式体系（antd 6 + Tailwind 4 原样保留）、不改后端接口与数据契约。
- 不引入微前端、不做"部分路由 Next + 部分路由 Vite"的长期混布（§6.3 说明原因）。
- 不借机调整业务逻辑（筛选持久化、CSRF 轮换、WS 通知等行为必须保持逐字节等价）。

**成功指标（验收阈值）**

| 指标 | 现状（实测） | 迁移后目标 | 测量方式 |
|:---|:---|:---|:---|
| dev 冷启动到可访问 | 数分钟级（首路由编译 130~200s） | **≤ 5s** | 起 server 到 `/login` 返回 200 |
| 菜单点击 → 内容渲染 | 未预热路由 30~345s；预热后 4.6~5.0s | **≤ 1.5s**（dev，含 API） | Playwright 计时（同 §12 方法） |
| dev 重启后再访问同一路由 | 重新付一遍冷编译 | **≤ 3s** | 杀进程重启 → curl 首字节 |
| HMR（保存文件 → 浏览器更新） | 秒级~数十秒 | **≤ 500ms** | Vite HMR 日志时间戳 |
| 单个 `_rsc`/导航请求数 | 1 条（已修复双击问题） | 保持 1 条 | DevTools Network 计数 |
| 生产构建耗时 | webpack（`NEXT_DISABLE_TURBOPACK=1`） | **较现状缩短 ≥ 50%** | `npm run build` 墙钟 |
| 前端生产镜像 | Node standalone（`node server.js`） | **静态资源 + nginx**（无 Node 运行时） | 镜像大小 / 启动耗时 |
| Playwright E2E | 基线（`npm run test:smoke` 等） | **全部用例通过，0 新增失败** | CI |

---

## 1. 现状诊断

### 1.1 代码结构实测（`itsm-frontend/src`）

| 维度 | 实测值 | 说明 |
|:---|:---|:---|
| `.ts` / `.tsx` 文件总数 | **1013** | 迁移的搬运面 |
| `'use client'` 文件 | **435**（占 43%） | 客户端组件为主 |
| `page.tsx` 总数 | **166** | 路由数 |
| 其中 `'use client'` 页面 | **156**（94%） | 页面级无服务端逻辑 |
| `export default async function` 页面 | **0** | 无服务端取数 |
| `next/headers` / `'use server'` / `cookies()` / `headers()` | **0 / 0 / 0 / 0** | 无 RSC、无 Server Action、无服务端 Cookie 读取 |
| 并行路由目录（`@*`） | **0** | 无 `default.js` 迁移负担 |
| `layout.tsx` / `route.ts` | **4 / 2** | 布局 4 个；路由处理器仅 `api/health` 与 `api/[...path]` |
| 引用 antd 的文件 | **459** | UI 体系完整保留，Vite 侧无需改造 |
| 引用 `@tanstack/react-query` 的文件 | **38** | 数据层不变 |
| 引用 `axios` / `fetch(` 的文件 | **2 / 19** | 数据全部浏览器侧发起 |

### 1.2 Next.js 专有面清单（迁移的全部机械替换点）

| Next API | 命中文件数 | 迁移目标 | 说明 |
|:---|:---|:---|:---|
| `next/navigation` | **130** | react-router v7 hooks | 绝大多数是 `useRouter().push()` |
| 其中 `useSearchParams` / `usePathname` | 11 / 9 | react-router 同名 hooks | 返回类型不同，需逐处核对（§5.2） |
| `next/link` | **16** | react-router `Link` | `href` → `to` |
| `next/dynamic` | **4** | `React.lazy` + `<Suspense>` | 或保留 `@loadable/component`（不推荐） |
| `next/image` | **2**（`components/ui/AppImage.tsx`、`components/ui/OptimizedImage.tsx`）+ 6 处 `quality={...}` | 原生 `<img>` 或 `vite-imagetools` | 见 §5.4 |
| `next/script` | **1**（`src/app/layout.tsx:2`） | `index.html` 内联或 `<script>` 组件 | 见 §5.4 |
| `next/font` | **1**（已注释，实际使用系统字体） | 删除残留 | `src/app/layout.tsx:4,17-18` 是空实现 |
| `next/server`（middleware/route handler） | **3** | 见 §1.3 | 框架能力，需重写 |
| `next.config.ts` 能力 | 1 | `vite.config.ts` + nginx | `rewrites` / `images` / `output: 'standalone'` |
| `NEXT_PUBLIC_*` 环境变量 | ~30 个文件（含测试/文档/Dockerfile） | `import.meta.env.VITE_*` | 业务集中在 `src/lib/env.ts`、`src/lib/api/api-config.ts` |

> 结论：**除 §1.3 的三处框架能力外，其余 100% 是机械替换**，可用脚本 + codemod 完成，不需要理解业务。

### 1.3 服务端职责清单（Vite 没有对应物，必须重写或转移）

| # | 位置 | 现有职责 | 生产实际是否生效 | 迁移落点 |
|:---|:---|:---|:---|:---|
| 1 | `src/middleware.ts:6-48` | 受保护路由未登录 307 → `/login?redirect=`；`LEGACY_MENU_REDIRECTS`（历史菜单路径 → 正确路由，含 `/list` 后缀等） | 生效（Next 服务端） | 客户端守卫 `RequireAuth` + 路由级 `redirects` 表（§5.5） |
| 2 | `src/app/api/[...path]/route.ts:1-160` | 反向代理至 `ITSM_BACKEND_URL`；`PUBLIC_PATHS` 白名单（`:15-30`）、`BLOCKED_PATHS` 敏感路径黑名单（`:8-12`）、JWT 校验（`:32-82`）、真实 IP 透传（`:111-140`，审计日志依赖） | **被 nginx 绕过**：`nginx/conf.d/default.conf:52-54` 将 `/api/` 直连 `itsm-backend:8090`；仅当直连前端容器时生效 | dev：`vite server.proxy`（+ 可选移植校验逻辑，§5.6）；生产：保持 nginx 直连（现网已是此路径） |
| 3 | `src/app/api/health/route.ts` | 健康检查 | 生效（compose healthcheck 打 `http://127.0.0.1:3000`） | 静态站由 nginx 承接；或改指后端 `/health`（`nginx/conf.d/default.conf:80-82` 已有） |

> 关键发现：**生产链路里前端 BFF 代理本就被 nginx 短路**（浏览器 → nginx:80 → 后端:8090）。因此迁移到静态资源 + nginx 不但不丢能力，反而删掉一层 Node 运行时；需要单独确认的只有"直连前端容器"的旁路场景与 dev 环境的防线一致性问题（见 §7 风险 R2）。

### 1.4 dev 性能基线（本次迁移的动因）

| 场景 | 实测 | 证据 |
|:---|:---|:---|
| Turbopack 冷编译单路由 | `/login` **198.1s**、`/tickets/create` **134.7s** | 15.5.24 dev server 日志 |
| webpack 冷编译 | `/login` **345.4s**（3855 modules） | `npm run dev:webpack` |
| dev 重启后 | 全部失效重编译（`.next/cache` 仅有 `swc`+`webpack`，无 turbopack 目录） | 目录实测；15.5 stable 的 `experimental.turbopackPersistentCaching` 抛 `CanaryOnlyError` |
| 预热命中已编译路由 | `/dashboard` 4.9s、`/tickets` 5.0s（200） | `npm run dev:warm -- /dashboard /tickets` |
| 单次 `_rsc` 服务端耗时（空闲） | 458ms ~ 4.8s（未编译时 19.9s） | DevTools + server 日志 |
| 每请求 SSR CPU | 1.2~2.6s（antd v6 cssinjs 每请求内联 30~320KB 样式 + React dev 栈采集） | `next.config.ts:23-24` 备忘与实测一致 |
| 路由总数 | **166**（预热脚本一次最多覆盖十余条） | `rg --files -g 'page.tsx'` |

### 1.5 为什么"可行性"没问题、"直接换"不行

- **可行性**：SPA 所需的一切（客户端路由、Provider 树、浏览器侧鉴权、HTTP 客户端、状态管理、UI 库）在本项目都已存在且被广泛使用；Next 只是一层未被使用的服务端外壳。
- **不可行的是"只换 bundler"**：`next dev` 自带路由/中间件/RSC 运行时，Vite 不提供；`next/navigation`、`next/link`、`next.config` 的 `rewrites`/`images` 在 Vite 下都没有等价物（`rewrites` 有 dev proxy 近似，生产靠 nginx）。

---

## 2. 目标架构

### 2.1 技术选型

| 层 | 现状 | 目标 | 理由 |
|:---|:---|:---|:---|
| 构建/dev | Next 15.5.24（Turbopack） | **Vite 7** | dev 秒级启动 + 原生 ESM HMR；`@tailwindcss/postcss` 换 `@tailwindcss/vite` |
| 框架 | Next App Router | **React 19.2 + react-router v7（Data Router / SPA）** | react-router 的 `useNavigate/useSearchParams/useParams/Link` 与 `next/navigation` 语义 1:1，130 个文件的替换最省 |
| 路由定义 | 文件系统路由（`src/app/**/page.tsx`） | **`src/routes.ts` 代码路由表**（由脚本从现有目录树生成初稿） | 166 条路由 + 路由组 + 动态段 + 历史重定向，代码表更可控、可 review |
| 数据层 | react-query 5 + 自研 `http-client` | **不变** | 迁移不碰数据契约 |
| UI | antd 6 + Tailwind 4 + lucide | **不变**（移除 `@ant-design/nextjs-registry`） | 纯客户端渲染后 cssinjs 更简单 |
| 测试 | Jest（单测）+ Playwright（E2E） | Playwright 不变；Jest **可选**迁 Vitest（§3.4） | Vite 生态下 Vitest 复用同一 transform 管线 |
| 静态资源 | `next/image` 优化器 | 原生 `<img>` + nginx 缓存（可选 `vite-imagetools`） | 现网仅 2 个封装文件 + 6 处 `quality` |
| 部署 | Node standalone（`node server.js`，端口 3000） | **静态产物 + nginx**（现网 nginx 已在前） | 见 §1.3；镜像更小、启动更快、无 Node 常驻 |

### 2.2 目录与路由映射规则

| 现状（Next） | 目标（Vite） | 说明 |
|:---|:---|:---|
| `src/app/layout.tsx` | `index.html` + `src/main.tsx` + `src/App.tsx`（Provider 树） | Provider 顺序必须与现存一致（§5.1） |
| `src/app/(main)/layout.tsx` | `src/layouts/MainLayout.tsx`（路由 `element`） | 路由组 `(main)` 在目标里不产生 URL 段 |
| `src/app/(main)/tickets/page.tsx` | `src/pages/tickets/index.tsx` | 记录 `path: '/tickets'` |
| `src/app/(main)/tickets/[ticketId]/page.tsx` | `src/pages/tickets/$ticketId.tsx` | `[x]` → `:x` |
| `src/app/(main)/knowledge/[[...slug]]/page.tsx`（若存在可选段） | 展开为显式路由 | 需逐条核对 |
| `src/app/api/[...path]/route.ts` | 删除；dev 用 `server.proxy`，生产用 nginx | 逻辑等价物见 §5.6 |
| `src/app/api/health/route.ts` | 删除；nginx 提供 `/health` | compose healthcheck 改为打 nginx 或后端 |
| `src/middleware.ts` | `src/routes/guards.tsx` + `src/routes/legacy-redirects.ts` | 两张表原样搬迁（§5.5） |
| `src/app/globals.css` | `src/styles/globals.css`（`main.tsx` 引入） | 不变 |

### 2.3 运行时数据流（迁移后）

```
浏览器
 ├─ 静态资源（HTML/JS/CSS） ── nginx（location /）── 静态目录
 ├─ /api/v1/**  ──────────── nginx（location /api/）── itsm-backend:8090（JWT + 租户校验，现状不变）
 └─ /ws/**（若启用）───────── nginx ── 后端（保持现有配置）

dev:
 vite dev(3000) ── server.proxy /api → 127.0.0.1:8090（或 ITSM_BACKEND_URL）
```

*说明：现状 prod 即为上图；本次改造只把 `location /` 的上游从 `itsm-frontend:3000`（Node）换成静态文件（nginx 内部 `root`/`try_files`），`/api/` 与 WS 链路零改动。*

---

## 3. 关键决策点（评审待确认）

### 3.1 决策 D1：现在就迁 Vite，还是先升 Next 16？

| 维度 | 方案 A：迁移 Vite（本方案） | 方案 B：升级 Next 16.3.x | 方案 C：维持现状 + 预热 |
|:---|:---|:---|:---|
| dev 冷启动 | **秒级** | 首次仍要编译；**重启复用**（Turbopack FS 缓存 16.1 起默认开启） | 无改善（重启即重付） |
| 单路由首次编译 | **不存在**（无服务端编译） | 仍存在（130~200s 量级，本机） | 仍存在 |
| 生产构建 | Vite（快） | Turbopack（官方 2~5×） | 现状 webpack |
| 改动面 | 框架迁移（§4，约 150 文件机械替换 + 3 处能力重写） | `proxy.ts` 改名、lint 移出构建、镜像参数、构建链验证 | 0 |
| 风险 | 中（部署形态 + 旁路场景） | 低 | 0 |
| 收益上限 | **高**（开发体验数量级改善 + 部署简化） | 中（主要是重启后不再重编译） | 低 |
| 结论 | 满足"把 dev 体验一次性拉到基线"的目标时选 A | 只想止血时选 B（成本 0.5~1 天） | 仅作为过渡期兜底 |

> 建议：**A 与 B 不冲突**。若短期内不想动框架，可先执行 B 止血；本方案的窗口期建议与下一次前端重构（如 `docs/plan/generic-attachment-richtext-control-plan.md` 落地后）合并，避免重复回归。

### 3.2 决策 D2：生产 BFF 是否保留？

- 事实：`nginx/conf.d/default.conf:52-54` 已把 `/api/` 直连后端，前端 BFF 仅在"直连前端容器"时生效；`docker-compose.prod.yml:388-395` 明确"3000 端口不对宿主发布"。
- **推荐 D2-a（不保留）**：dev 用 `vite server.proxy`，prod 全量走 nginx → 运行时最少、与现网一致。
- 备选 D2-b（保留）：若存在"前端容器直连"的旁路部署（如单容器演示环境、内网直连 3000），需保留一个轻量 Node/nginx 中间层，把 `PUBLIC_PATHS`/`BLOCKED_PATHS`/JWT 校验/`X-Forwarded-For` 透传逻辑（`src/app/api/[...path]/route.ts:8-140`）移植为独立服务。
- **需确认**：是否存在直连前端容器 3000 的部署或文档/脚本（迁移前用 `rg "3000" docs/ docker-compose*.yml nginx/` 全量核对）。

### 3.3 决策 D3：路由定义方式

- **推荐**：`src/routes.ts` 手写路由表（脚本生成初稿），配合 `React.lazy` 按路由分包。
- 备选：`@react-router/fs-routes` 文件路由（目录结构镜像现状，改动更少，但 166 条路由的 review 与重定向表仍是手工）。
- 不推荐：TanStack Router（类型更安全，但 `useSearchParams/useNavigate` 迁移成本高于 react-router，且团队无存量经验）。

### 3.4 决策 D4：单测框架是否同步迁移 Jest → Vitest

- 现状：`jest.config.js` + `jest.setup.js` + `jest-environment-safe-nwsapi.js`，`npm run test:unit` 等 6 个脚本。
- 影响：Jest 在 Vite 项目里需额外 transform 配置（babel-jest 保留即可运行），**可先不迁**。
- **推荐**：分两步——迁移期保持 Jest（减少变量），迁移完成后单独立项评估 Vitest（预计 1~2 人日，收益是配置统一与速度）。

### 3.5 决策 D5：是否同步启用 monorepo 共享类型

- 现状：前后端类型各自维护，`src/lib/api/*` 手写 DTO。
- 本方案**不做**（非目标）；仅在迁移中保持类型定义原样复制，避免把两类变更混在一个 PR。

---

## 4. 工作分解（WBS）

> 约定：任务号 `FE-V1-xx`，估算单位为人日（1 人日 = 6h 有效编码）。每个 PR 只做一件事；**禁止把业务改动、样式重构混入迁移 PR**（否则回归无法定位）。

### 4.1 阶段总览

| 阶段 | 任务 | 交付物 | 估算 | 依赖 |
|:---|:---|:---|:---|:---|
| **P0 准备与基线** | FE-V1-01 | 迁移分支 + 基线快照（E2E 全绿记录、§1.4 性能复测、166 路由清单） | 1.5 | — |
| | FE-V1-02 | 路由清单与映射表（自动生成 + 人工标注动态段/重定向） | 1 | FE-V1-01 |
| | FE-V1-03 | D2 旁路部署核查报告（直连 3000 的场景清点） | 0.5 | — |
| **P1 骨架** | FE-V1-10 | `vite.config.ts` / `index.html` / `main.tsx` / Provider 树 / Tailwind 4 接入 | 2 | P0 |
| | FE-V1-11 | TS/ESLint/路径别名（`@/*`）在新工程可用；`npm run type-check` 全绿 | 1 | FE-V1-10 |
| **P2 路由与导航** | FE-V1-20 | `src/routes.ts`（166 条，`React.lazy` 分包）+ 嵌套布局 | 3 | FE-V1-10 |
| | FE-V1-21 | 导航 API codemod：`next/navigation` 130 文件 + `next/link` 16 文件 | 4 | FE-V1-20 |
| | FE-V1-22 | `useSearchParams`/`usePathname` 逐处核对（11/9 文件），`next/dynamic` 4 文件 | 2 | FE-V1-21 |
| | FE-V1-23 | 页面元数据 / 标题策略（`index.html` + 路由级 `useTitle`） | 0.5 | FE-V1-20 |
| **P3 服务端能力** | FE-V1-30 | 客户端守卫 `RequireAuth` + `LegacyRedirects` 表搬迁（48+ 行原样） | 1.5 | FE-V1-20 |
| | FE-V1-31 | dev `server.proxy`（含 Cookie/WS/超时）+ 与 BFF 行为差异清单 | 1.5 | FE-V1-10 |
| | FE-V1-32 | `/api/health` 承接方确定与替换（nginx 或后端 `/health`） | 0.5 | FE-V1-31 |
| **P4 资源** | FE-V1-40 | `next/image`（2 文件 + 6 处 `quality`）→ 原生 `<img>` | 1 | P1 |
| | FE-V1-41 | `next/script`（`layout.tsx:2`）与 `next/font` 残留清理；`public/` 资源与 `uploads/` 路径核对 | 0.5 | P1 |
| **P5 构建与部署** | FE-V1-50 | 生产构建（`vite build`）+ `Dockerfile` 多阶段（静态产物 + nginx/或纯静态镜像） | 2 | P2、P3 |
| | FE-V1-51 | `nginx` 配置：`location /` 静态 + `try_files` SPA 回退 + 缓存头 + `/api/` 保持 | 1 | FE-V1-50 |
| | FE-V1-52 | `docker-compose.prod.yml` 前端服务改写 + healthcheck + 构建参数（`VITE_API_URL` 等） | 1 | FE-V1-51 |
| | FE-V1-53 | Playwright `webServer` 改指 Vite；CI 构建缓存更新 | 2 | FE-V1-50 |
| **P6 验证** | FE-V1-60 | 全量 E2E + 关键路径手工回归（登录/工单/知识库/审批/报表/通知/附件） | 3 | P5 |
| | FE-V1-61 | 性能验收（§0 指标逐项实测） | 1 | FE-V1-60 |
| | FE-V1-62 | 回滚演练（切回 Next 镜像分支） | 0.5 | FE-V1-52 |
| **P7 切换** | FE-V1-70 | 灰度/切换 + 观察期（1 周）+ 分支归档 | 1.5 | FE-V1-61、FE-V1-62 |

**合计：约 24~31 人日**；按 2 人（1 前端主 + 1 前端/测试协同，SRE 0.3 参与）排期约 **4~5 周**（含 1 周观察期，观察期不占人日），与 §10 的周表对应。

### 4.2 FE-V1-01 基线采集（必须先做）

```powershell
# 1) E2E 基线（现行 Next dev 或 prod 镜像，记录全绿清单）
cd itsm-frontend; npm run test:smoke
# 2) 性能基线复测（与 §1.4 对齐；重启 dev server 后测首路由）
npm run dev:warm -- /login /dashboard
# 3) 路由清单固化
$src='E:\projects\itsm\itsm-frontend\src'
rg --files -g 'page.tsx' $src | ForEach-Object { $_ -replace [regex]::Escape($src+'\app'),'' } | Sort-Object | Set-Content ..\docs\plan\_data\vite-routes-before.txt
```

**验收**：`_data/vite-routes-before.txt` 行数 = 166；E2E 结果存档（失败项若存在，需在迁移前后一致，不得新增）。

### 4.3 FE-V1-02 路由映射表

输出 `docs/plan/_data/vite-route-map.csv`，列：`next_path,next_file,vite_path,vite_component,lazy_chunk,dynamic_segments,redirects,notes`。

- 路由组 `(main)` / `(auth)` 等不产生 URL 段，但决定布局归属（`MainLayout` / `AuthLayout`）。
- 动态段：`[ticketId]` → `:ticketId`；多段与可选段逐条标注。
- 与 `src/middleware.ts:65` 的 `LEGACY_MENU_REDIRECTS` 求并集，标注"仅重定向、无页面"的条目数。

### 4.4 各阶段完成定义（DoD）

| 阶段 | DoD |
|:---|:---|
| P1 | `npm run dev` 秒级启动；`/login` 可渲染；`npm run type-check` 0 错误（允许 `@ts-expect-error` 仅出现在已登记清单） |
| P2 | 166 条路由全部可直达（无 404）；无 `next/*` 引用残留（`rg "from 'next/" src` 为 0，白名单见 §5.8） |
| P3 | 未登录访问任一受保护路由 → 跳 `/login?redirect=`；登录后回跳正确；历史路径重定向与现状逐条比对一致 |
| P5 | 本地 `docker compose -f docker-compose.prod.yml` 起栈，Playwright 全绿；镜像体积与启动耗时记录 |
| P6 | §0 指标表逐项达标；回归缺陷数 = 0（P0/P1 级）、允许的 P2 级缺陷有登记与排期 |

---

## 5. 关键实现细则

### 5.1 工程骨架

**`vite.config.ts`（草案）**

```ts
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const backend = env.ITSM_BACKEND_URL || 'http://127.0.0.1:8090';
  return {
    plugins: [react(), tailwindcss()],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    base: '/',
    define: {
      __APP_VERSION__: JSON.stringify(env.VITE_APP_VERSION ?? 'dev'),
      __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
    },
    server: {
      host: '0.0.0.0',
      port: 3000,
      strictPort: true,
      proxy: {
        '/api': { target: backend, changeOrigin: false, cookieDomainRewrite: '' },
      },
    },
    optimizeDeps: {
      // antd / 图表 / 编辑器等大依赖预打包，避免首次进入页面时浏览器侧二次发现
      include: ['react', 'react-dom', 'react-router', 'antd', '@ant-design/icons',
                '@tanstack/react-query', 'axios', 'dayjs'],
    },
    build: {
      outDir: 'dist',
      sourcemap: mode !== 'production',
      rollupOptions: {
        output: {
          manualChunks: {
            react: ['react', 'react-dom', 'react-router'],
            antd: ['antd', '@ant-design/icons'],
            charts: ['echarts', '@ant-design/charts'],
          },
        },
      },
    },
  };
});
```

**`index.html`**：把 `src/app/layout.tsx:20-60` 的 metadata 等价搬入（`<title>`、`<meta name="description">`、`viewport`、`theme-color`、`icons`、`manifest`），并保留 `lang="zh-CN"` 与 `suppressHydrationWarning` 对应的挂载点约定（`<div id="root">`）。

**`src/main.tsx`**

```tsx
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider, createBrowserRouter } from 'react-router';
import { routes } from '@/routes';
import '@/styles/globals.css';

const router = createBrowserRouter(routes);
createRoot(document.getElementById('root')!).render(
  <StrictMode><RouterProvider router={router} /></StrictMode>
);
```

**Provider 树**：现状 `src/app/layout.tsx` 内的 Provider 嵌套（Query / 主题 / i18n / antd ConfigProvider / 全局错误边界 / 通知等）**按原顺序逐层照搬**到 `src/App.tsx` 或路由根 `element`；`@ant-design/nextjs-registry`（SSR 样式提取用）直接删除——纯客户端渲染下无需服务端提取，仅保留 antd `ConfigProvider` 主题配置。

**要点**

- Tailwind 4：PostCSS 插件换成 `@tailwindcss/vite`；`globals.css` 顶部保持 `@import "tailwindcss";`，现有 `@theme`/自定义 token 原样保留（`tailwind.config` 若仍被 4.x 兼容模式引用，需核对 `@config` 指令）。
- TS：`tsconfig.json` 去掉 `"plugins": [{ "name": "next" }]`、`next-env.d.ts`；保留 `paths: { "@/*": ["./src/*"] }`（与 Vite alias 对齐）。
- ESLint：flat config 去掉 `eslint-config-next`，改 `typescript-eslint` + `eslint-plugin-react-hooks` + `eslint-plugin-react-refresh`；**规则集变化需一次性评估**（可能暴露存量告警，建议迁移期关闭 `react-refresh/only-export-components` 以免 450+ 组件文件产生噪声）。
- 环境变量：`next dev/build` 的 `NEXT_PUBLIC_*` → Vite 的 `VITE_*`（仅 `VITE_` 前缀暴露给客户端），集中改造点见 §5.7。

### 5.2 路由与导航 codemod

**`src/routes.ts`（片段）**

```tsx
import { lazy } from 'react';
import { Navigate, type RouteObject } from 'react-router';
import { RequireAuth } from '@/routes/guards';
import MainLayout from '@/layouts/MainLayout';

const Login = lazy(() => import('@/pages/login'));
const Tickets = lazy(() => import('@/pages/tickets'));
const TicketDetail = lazy(() => import('@/pages/tickets/$ticketId'));

export const routes: RouteObject[] = [
  { path: '/login', element: <Login /> },
  {
    element: <RequireAuth><MainLayout /></RequireAuth>,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'dashboard', element: <Dashboard /> },
      { path: 'tickets', element: <Tickets /> },
      { path: 'tickets/:ticketId', element: <TicketDetail /> },
      // ... 其余 160+ 条由 FE-V1-02 生成的映射表逐条落表
    ],
  },
  { path: '*', element: <NotFound /> },
];
```

**导航替换对照表（FE-V1-21/22 的机械化依据）**

| Next | react-router | 注意 |
|:---|:---|:---|
| `useRouter().push(path)` | `useNavigate()(path)` | 130 个文件中绝大多数属此类 |
| `router.replace(path)` | `navigate(path, { replace: true })` | — |
| `router.back()` | `navigate(-1)` | — |
| `router.refresh()` | `queryClient.invalidateQueries()`（或 `refetch()`） | **语义不同**，需逐处确认（refresh 是 RSC 重取） |
| `usePathname()` | `useLocation().pathname` | 9 文件 |
| `useParams()` | `useParams()` | 同名，返回类型一致 |
| `useSearchParams()` | `useSearchParams()` | **返回类型不同**：Next 只读（`ReadonlyURLSearchParams`），react-router 返回 `[URLSearchParams, setSearchParams]`。消费端 `.get()` 兼容，但"复制/拼接 query"的写法要改为 `setSearchParams`（11 文件逐处核对） |
| `<Link href={x}>` | `<Link to={x}>` | 16 文件；`prefetch` prop 删除（SPA 无意义） |
| `next/dynamic(() => import(...), { ssr:false })` | `React.lazy` + `<Suspense fallback>` | 4 文件；`ssr:false` 语义自动满足 |
| `redirect()`（服务端） | `<Navigate to replace />` 或路由 `loader` | 检查是否存在（本次实测未见服务端 `redirect`） |

**codemod 优先**：先用 `ast-grep`/`jscodeshift` 或正则替换跑一遍，再人工核对 `useSearchParams` 的 11 个文件与 `router.refresh` 的命中；每批替换后立即 `npm run type-check`，把类型错误当作替换遗漏的告警器。

### 5.3 布局（嵌套路由）

- 现状 4 个 `layout.tsx` → 目标：`RootProviders`（全局）、`MainLayout`（含侧边栏/顶栏/通知，替换 `src/app/(main)/layout.tsx`）、`AuthLayout`（登录/注册类页面）。用 `<Outlet />` 承接子路由。
- 布局内的"当前菜单高亮"逻辑若依赖 `usePathname()`，直接替换即可（react-router `useLocation().pathname` 语义一致）。
- 路由级 `<Suspense>`：在 `MainLayout` 的内容区包一层，`fallback` 复用现有页面骨架屏组件，保证 lazy chunk 加载时体验与现状一致。

### 5.4 资源与静态文件

| 项 | 处理 |
|:---|:---|
| `next/image`（`components/ui/AppImage.tsx`、`components/ui/OptimizedImage.tsx`） | 改为原生 `<img>` + `loading="lazy"` + `decoding="async"`；保留统一的尺寸/圆角/回退逻辑（这两个封装本身就是为了抽象，改动只在其内部） |
| `quality={...}`（6 处） | 删除（原生 img 无此概念）；若后续需要真正压缩，另行评估 `vite-imagetools` |
| `src/app/layout.tsx:2` 的 `next/script` | 移入 `index.html`（`<script>` 标签）或用等价组件；注意 `strategy` 语义（`beforeInteractive` → head 内 defer/同步） |
| `next/font` | `layout.tsx:4,17-18` 为注释残留，直接清理 |
| `public/` | 原样搬到项目根的 `public/`（Vite 默认行为一致），引用路径 `/xxx.png` 不变 |
| `uploads/`（compose 挂载 `./uploads:/app/public/uploads:ro`） | **必须**在 nginx 或静态产物中保持同一 URL 前缀（`/uploads/**`），否则附件预览失效；迁移后由 nginx `location /uploads/` 指向挂载卷（§5.9） |
| favicon / manifest / icons | 从 `layout.tsx` metadata 搬到 `index.html` + `public/` |

### 5.5 守卫与历史重定向（FE-V1-30）

```tsx
// src/routes/guards.tsx
export function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isBootstrapping } = useAuthStore();
  const location = useLocation();
  if (isBootstrapping) return <FullPageSpinner />;
  if (!isAuthenticated) {
    const redirect = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirect}`} replace />;
  }
  return <>{children}</>;
}
```

- `protectedRoutes`（`src/middleware.ts:6-48`）逐条转为路由树上的守卫包裹；**匹配语义**（前缀匹配 vs 精确匹配）必须与原实现一致，建议把原表整表保留为数据文件并在守卫里做同一套匹配，降低语义漂移。
- `LEGACY_MENU_REDIRECTS`（`src/middleware.ts:65`）→ `src/routes/legacy-redirects.ts`，在路由表顶层以 `<Route path="old" element={<Navigate to="/new" replace />} />` 展开；**必须保持 307 语义**（`replace`），否则浏览器历史会被污染。
- 守卫是纯客户端行为：首次加载会出现"短暂空白/加载态"（因 JWT 来自客户端存储）。缓解：`isBootstrapping` 阶段渲染与现状等价的骨架屏，避免闪烁；**不得**为了消除闪烁把鉴权前置到服务端（那会重新引入 BFF）。

### 5.6 dev 代理与 BFF 差异（FE-V1-31）

| 能力（现状 `src/app/api/[...path]/route.ts`） | dev（`server.proxy`） | 说明与处置 |
|:---|:---|:---|
| 转发 `/api/:path*` → 后端 | 等价 | `target = ITSM_BACKEND_URL`（默认 `127.0.0.1:8090`） |
| `PUBLIC_PATHS` 免鉴权白名单（`:15-30`） | 不需要 | 白名单仅是"不做本地 JWT 校验"，后端本身对该类路径放行 |
| `BLOCKED_PATHS` 黑名单（`:8-12`） | **需确认** | dev 下失去这道拦截；须核对后端对这些路径已有鉴权（§7 R2） |
| 本地 JWT 校验（`:32-82`） | 不做 | 后端 `middleware` 已有 JWT + 租户校验；dev 下未登录请求将由后端返回 401，前端 `http-client` 的 401 处理链路需实测确认（§7 R2） |
| `X-Forwarded-For`/`X-Real-IP` 透传（`:111-140`，审计日志用） | 不需要 | 仅影响"浏览器直连前端容器"的旁路场景；生产由 nginx（`nginx.conf` 已设置真实 IP 头）承担 |
| Set-Cookie 透传 | `cookieDomainRewrite: ''` | 后端若下发 `Domain=` 需改写，否则本地 `localhost` 收不到 Cookie |

**若 D2-b（保留 BFF）**：单独实现一个 ~120 行的 Node/nginx 中间层，逻辑照搬 `route.ts`，并纳入健康检查；但这会抵消"去掉 Node 运行时"的收益，**仅在有旁路部署需求时采用**。

### 5.7 环境变量改造（FE-V1-50 前置）

| 现状 | 目标 | 使用点 |
|:---|:---|:---|
| `NEXT_PUBLIC_API_URL` | `VITE_API_URL` | `src/lib/api/api-config.ts`、`src/lib/env.ts` |
| `NEXT_PUBLIC_API_BASE_URL` | `VITE_API_BASE_URL` | 同上（若仍在用需实测确认） |
| `NEXT_PUBLIC_API_VERSION` | `VITE_API_VERSION` | `api-config` |
| `NEXT_PUBLIC_API_TIMEOUT` / `NEXT_PUBLIC_API_RETRY_COUNT` | `VITE_API_TIMEOUT` / `VITE_API_RETRY_COUNT` | `http-client` |
| `NEXT_PUBLIC_APP_VERSION` / `NEXT_PUBLIC_BUILD_TIME` | `VITE_APP_VERSION` / `VITE_BUILD_TIME`（或 `define` 注入） | 页脚/关于弹窗 |
| `NEXT_PUBLIC_ENABLE_AI` | `VITE_ENABLE_AI` | 功能开关 |
| `ITSM_BACKEND_URL`（仅服务端） | 保留为 `.env.local` 变量，供 `vite.config.ts` 的 proxy 读取 | `loadEnv(mode, cwd, '')` |

**关键差异**：Next 的 `NEXT_PUBLIC_*` 在**构建时**烘入产物；Vite 的 `import.meta.env.VITE_*` 同样是**构建时静态替换**。因此"同一镜像换环境变量"依旧不可行——这一点与现状完全一致（compose `docker-compose.prod.yml:378-380` 的注释已明确构建期烘入），迁移不改变部署约束，也就不需要新的运维流程。

**必须保留的兼容读取**：改造 `src/lib/env.ts` 时提供一次性回退（`import.meta.env.VITE_X ?? import.meta.env.NEXT_PUBLIC_X`）不必要；直接一次性改名更干净，但要在 §5.8 的残留扫描里确认无遗漏（含 `.env.example`、`docs/`、`.github/workflows`、`Dockerfile`）。

### 5.8 残留检查（PR 门禁）

```powershell
$src='E:\projects\itsm\itsm-frontend\src'
rg -n "from 'next/|require\('next/" $src          # 期望：0 命中
rg -n "NEXT_PUBLIC_" $src                         # 期望：0 命中
rg -n "useSearchParams" $src                      # 期望：命中数与 §1.2 一致（11）且已核对
rg -n "router\.refresh" $src                      # 期望：0 命中或已逐一改写
rg --files -g 'page.tsx' -g 'layout.tsx' $src     # 期望：0（旧目录已删）
```

### 5.9 部署产物（FE-V1-50/51/52）

**Dockerfile（新）**：`node:24-alpine` 构建 → `vite build` → 产物 `dist/` 交给 nginx 镜像（或在 `itsm-nginx` 中用共享卷）。两种落地方式：

- **方案 i（推荐，改动最小）**：`itsm-frontend` 镜像变为 `nginx:alpine` + `dist/` + 一份只服务静态资源的 `default.conf`；compose 中 `expose: 3000` 改 80，或保留 3000（`nginx` 监听 3000）。前端容器不再需要 Node、healthcheck 从 `wget http://127.0.0.1:3000` 改为 `wget http://127.0.0.1:3000/health` 或 index。
- **方案 ii（更彻底）**：删掉 `itsm-frontend` 服务，把 `dist/` 直接挂载/复制进 `nginx` 容器，由统一入口 nginx 服务静态资源与 `/api/` 反代。收益是少一个容器，代价是改动面大、与现有 `depends_on/healthcheck` 编排耦合（`docker-compose.prod.yml:413-428`）。

**nginx（在现有 `nginx/conf.d/default.conf` 基础上增补）**

```nginx
# location / 的上游由 itsm-frontend:3000 改为静态产物（方案 i 中在前端容器内，方案 ii 中在本容器内）
root /usr/share/nginx/html;
index index.html;
location / {
  try_files $uri $uri/ /index.html;          # SPA 回退：166 条前端路由必须能直达（刷新不 404）
}
location = /health { return 200 'ok'; add_header Content-Type text/plain; }
location /assets/ {                            # Vite 产物带 hash，可长缓存
  expires 1y; add_header Cache-Control "public, immutable";
}
location /uploads/ { alias /usr/share/nginx/uploads/; }   # 保持与现状同一 URL 前缀
location /api/ { proxy_pass http://itsm-backend:8090; ... } # 现状不变（default.conf:52-54）
```

**compose 变更点**

- 构建参数：`NEXT_PUBLIC_*` → `VITE_*`（`docker-compose.prod.yml:371-380`）；`ITSM_BACKEND_URL` 不再需要传给前端（BFF 已删，除非 D2-b）。
- `environment: ITSM_BACKEND_URL` / `no_proxy` 可移除；`NODE_ENV` 无意义。
- healthcheck（`:403-407`）改写（见上）。
- `volumes: ./uploads:/app/public/uploads:ro`（`:397`）需改为新静态根下的 `uploads` 路径（否则附件 URL 断链）——**这是最容易漏的一处**。

---

## 6. 迁移策略

### 6.1 策略选择

| 策略 | 可行？ | 说明 |
|:---|:---|:---|
| **A. 一次性迁移（推荐）** | ✅ | 在 `feat/vite-migration` 分支完成全部搬运，通过完整回归后一次性切换；`main` 全程保持 Next 可发布 |
| B. 渐进式（Next 与 Vite 并存，页面级双轨） | ❌ 不推荐 | 两套 bundler/两套路由/两份 React 运行时无法在同一产物内共存；实现方式只能是"两个独立前端 + 路径分流"，成本高于一次性迁移，且样式与状态会分裂 |
| C. 只替换构建工具（`vite build` 出 Next 产物） | ❌ | 不支持。Next 15 的构建只支持 Turbopack/webpack |
| D. 迁移到 Nuxt/其他框架 | ❌ | 后端 Go、前端 React + antd 生态，无收益 |

### 6.2 长分支协作规则（4~5 周窗口）

1. `main` 上的日常业务开发照常（小步 PR）；迁移分支**每周五 rebase 一次** `main`，不允许落后超过 1 周。
2. 冲突热点预判与约定：
   - `src/routes.ts` / 路由映射表：新增页面的开发者在 `main` 上不动路由定义文件时，rebase 冲突最小；**约定迁移期内新增页面必须在 `docs/plan/_data/vite-route-map.csv` 追加一行**（迁移分支合并时以此为准）。
   - `package.json`：迁移分支只增删依赖，避免顺带升级其他依赖。
   - `src/lib/**`（API/鉴权）：迁移分支尽量不改语义，仅做 `NEXT_PUBLIC_*` → `VITE_*` 改名。
3. **每个阶段结束前 `main` rebase 一次并跑完整 E2E**，避免"最后一刻才合并"导致的雪崩。

### 6.3 切换与回退

- 切换前保留：`git tag next-final-<date>`、生产镜像 `itsm-frontend:<VERSION>-next`（可立即回滚）。
- 切换方式：compose 升级 `VERSION` → 观察 1 周（无 P1 缺陷）后归档 Next 分支。
- 回滚成本：镜像回退 + nginx 配置回退，**预计 ≤ 15 分钟**（演练见 FE-V1-62）。

---

## 7. 风险与对策

| ID | 风险 | 概率 | 影响 | 对策 | 责任 |
|:---|:---|:---:|:---:|:---|:---|
| R1 | 166 条路由有遗漏/映射错误，出现 404 或空白页 | 中 | 高 | ① FE-V1-02 生成清单并做**自动比对**（迁移前后路由集合差集必须为空）；② 新增"全路由可达性冒烟"脚本：遍历 `routes.ts`，未登录期望 302→`/login`、已登录期望非 404/500；③ E2E 覆盖核心 30 条路径 | 前端 |
| R2 | 鉴权行为漂移：dev 下失去 `BLOCKED_PATHS` 拦截、未登录请求的 401 处理链路变化（原 BFF 会提前 401，现由后端返回） | 中 | 中 | ① 迁移前核对后端对 `route.ts:8-12` 黑名单路径的鉴权/审计覆盖；② E2E 增加"未登录访问受保护页 + 直接调用受保护 API"用例；③ 若存在直连前端容器的旁路部署，走 D2-b 保留 BFF；④ `http-client` 的 401 跳转逻辑做单点验证 | 前端 + 后端 |
| R3 | SPA 首次加载体积变大（Next 现状每页需下载运行时 + 页面 chunk，未必更小；但 SPA 一次性下主 chunk） | 中 | 中 | ① 路由级 `React.lazy` + `manualChunks`；② 构建后对比 `dist` 总量与首屏传输量（gzip 后），目标：首屏 JS ≤ 现状同页传输量；③ 需要时对 antd/图表做按需引入复核；④ 生产启用 brotli/gzip | 前端 |
| R4 | 深链接刷新 404（SPA history fallback 未配置） | 中 | 高 | nginx `try_files $uri $uri/ /index.html`（§5.9）纳入验收用例：直接 `goto('/tickets/:id')` 刷新后仍 200 | SRE |
| R5 | 环境变量改名遗漏（CI、Dockerfile、`.env.example`、文档、脚本） | 中 | 中 | §5.8 门禁 + 全仓 `rg -n "NEXT_PUBLIC_"` 清零；构建参数在 compose 中显式声明 | 前端 + SRE |
| R6 | antd v6 + React 19 在纯 CSR 下的 cssinjs 首屏样式闪烁（FOUC） | 低 | 低 | 保留 `ConfigProvider` + 主题 token；关键样式与现状截图逐页比对；必要时先渲染骨架屏 | 前端 |
| R7 | 测试基础设施改造引入假失败/假通过（Playwright `webServer`、登录态 setup、Jest 仍在跑） | 中 | 中 | FE-V1-53 单独 PR；迁移期 Jest 配置不动；Playwright 用同一批用例跑"Next 基线 vs Vite"对比，差异逐条解释 | 测试 |
| R8 | 长分支合并冲突/进度蔓延（3~4 周窗口） | 中 | 中 | §6.2 协作规则；每阶段末 rebase + 全量 E2E；冲突超阈值时暂停并缩短窗口 | 前端 Lead |
| R9 | 静态资源缓存策略错误导致发布后用户拿到旧 `index.html` | 低 | 中 | `index.html` 短缓存（no-cache/5min），`/assets/*` 长缓存 + immutable（Vite 文件名带 hash）；发布后验证 | SRE |
| R10 | 依赖生态耦合 Next | 低 | 低 | 实测仅 `next`、`@ant-design/nextjs-registry` 两个包绑定 Next，均删除；顺带清掉未使用的 `swr`（全仓 0 引用）；新增 `react-router@^7`、`vite`、`@vitejs/plugin-react`、`@tailwindcss/vite` | 前端 |
| R11 | 迁移期间 Next 侧发生安全补丁（如 Next 15.x CVE），需在两条线同步 | 低 | 中 | `main` 的 hotfix 必须 cherry-pick 到迁移分支；迁移分支不升级 Next 版本（保持 15.5.24 直至切换） | 前端 Lead |

---

## 8. 测试与验收计划

### 8.1 自动化测试

| 层级 | 工具 | 迁移动作 | 验收 |
|:---|:---|:---|:---|
| 单元测试 | Jest 29（保持不动） | 无（D4 决策后再评估 Vitest） | `npm run test:unit` 与迁移前同结果 |
| 类型 | `tsc --noEmit` | 去掉 Next 插件与 `next-env.d.ts` | 0 错误 |
| E2E | Playwright | `webServer` 由 `next dev` 改 `vite dev`；`baseURL` 不变 | 现有用例全绿（0 新增失败） |
| **新增：全路由可达性冒烟** | Playwright + `routes.ts` 遍历 | 见下 | 166 条逐一断言 |

```ts
// e2e/route-reachability.spec.ts（新增）
import { routes } from '../src/routes';
const paths = flattenPaths(routes);         // 展开嵌套 children，替换 :param 为真实示例值
for (const p of paths) {
  test(`route reachable: ${p}`, async ({ page }) => {
    const resp = await page.goto(p, { waitUntil: 'domcontentloaded' });
    expect(resp!.status()).toBeLessThan(400);            // 无 404 / 5xx
    await expect(page.locator('#root')).not.toBeEmpty(); // 已渲染
  });
}
```

> 未登录场景另跑一遍：断言重定向到 `/login?redirect=<原路径>`（对应 `middleware.ts` 的现状语义）。

### 8.2 手工回归清单（模块级，按现状功能域）

| # | 场景 | 关键点 |
|:---|:---|:---|
| 1 | 登录 / 登出 / 记住我 / 会话过期 | `redirect` 回跳、401 处理、Token 刷新 |
| 2 | 工单：列表（筛选/分页/排序/导出）→ 详情 → 新建 → 编辑 → 流转/审批 | 深链接刷新、面包屑、返回态保持 |
| 3 | 知识库：目录树、搜索、富文本/附件渲染、历史版本 | 附件 URL（`/uploads/**`） |
| 4 | 审批中心 / 流程（BPMN 相关页面） | 懒加载 chunk 首次进入 |
| 5 | 报表 / 仪表盘（图表库） | `manualChunks` 后图表页加载与 resize |
| 6 | 通知（WS/轮询）、全局消息、错误边界 | 断线重连、跨页通知 |
| 7 | 设置 / 用户管理 / 权限差异（不同角色登录） | 菜单可见性、路由守卫 |
| 8 | 历史菜单路径（`LEGACY_MENU_REDIRECTS` 全表） | 逐条 307 到新地址（脚本化比对） |
| 9 | 主题切换 / 暗色 / 语言（若有） | 与现状截图一致 |
| 10 | 浏览器前进/后退、刷新、直接粘贴深链接 | SPA 路由与 nginx fallback |

### 8.3 性能验收（对照 §0）

```powershell
# 迁移后 dev 指标
cd itsm-frontend
npm run dev            # 记 t0
# t1: 首次 GET /login 200 的时间（Playwright/curl 计时）→ 要求 ≤ 5s
# 重启后重复：要求 ≤ 3s
# 菜单点击 → 渲染：Playwright 计时 → 要求 ≤ 1.5s
# 生产构建墙钟对比 webpack 基线
npm run build
```

**判定规则**：任一指标未达标需给出原因与补救（例如 antd 预打包不足 → 补 `optimizeDeps.include`），不允许"部分达标即验收"。

---

## 9. 部署与回滚

### 9.1 构建与发布

| 步骤 | 命令/产出 | 备注 |
|:---|:---|:---|
| 1 | CI: `npm ci && npm run type-check && npm run build` | `VITE_*` 构建参数注入（`.env.production` 或 CI 变量） |
| 2 | 产出 `dist/`（含 `index.html` + `assets/*` 带 hash） | 镜像 `itsm-frontend:${VERSION}` |
| 3 | `docker compose -f docker-compose.prod.yml up -d` | `VERSION` 升级触发替换 |
| 4 | 校验：`/health`、首页、`/api/v1/*` 代理、深链接刷新、附件 URL | 见 §8.1/§8.2 |

### 9.2 回滚（演练项 FE-V1-62）

1. `export VERSION=<上一个 Next 版本号>` → `docker compose up -d`（前端镜像回退）。
2. 若 nginx 配置有变，一并回退 `nginx/conf.d/default.conf`（建议按版本打 tag）。
3. 目标 RTO ≤ 15 分钟；回滚后跑 `npm run test:smoke` 确认。

---

## 10. 排期与人力（建议）

| 周 | 里程碑 | 内容 |
|:---|:---|:---|
| W1 | **M1 骨架可跑** | P0 全部 + FE-V1-10/11；`vite dev` 秒级启动，`/login` 渲染成功；路由映射表评审通过 |
| W2 | **M2 路由与导航切换完成** | FE-V1-20/21/22/23；166 条路由全部可达（未登录重定向正确） |
| W3 | **M3 服务端能力与资源收口** | FE-V1-30/31/32/40/41；`rg "from 'next/"` 清零；类型全绿 |
| W4 | **M4 生产形态跑通** | FE-V1-50/51/52/53；本地 compose 全栈 + Playwright 全绿 |
| W5 | **M5 验收与切换** | FE-V1-60/61/62/70；性能指标达标、回滚演练、灰度切换、进入观察期 |

**人力**：前端 1.5 人（主 + 兼）+ 测试 0.5 人 + SRE 0.3 人；总计约 24~31 人日。

---

## 11. 待确认清单（评审会上逐条闭环）

| # | 问题 | 影响 | 建议默认值 |
|:---|:---|:---|:---|
| Q1 | 是否存在**直连前端容器 3000 端口**的部署/脚本/文档（非经 nginx）？ | 决定 D2 是否保留 BFF | 否 → 走 D2-a |
| Q2 | `middleware.ts` 的 `BLOCKED_PATHS` 对应后端是否已有同等鉴权？ | R2 防线 | 需后端确认 |
| Q3 | 迁移窗口期可否锁定 4~5 周（期间有大版本需求需评估冲突）？ | 排期 | W1 起算 |
| Q4 | 是否有外部系统依赖前端域名的 OG/SEO/`robots.txt` 行为？ | R3/元数据 | 内部系统，无 |
| Q5 | 浏览器支持基线（是否需兼容旧版 Edge/Chrome）？ | 构建 target | 与现状一致（最近 2 个大版本） |
| Q6 | 单测框架是否同批迁 Vitest？ | D4 | 否，单独立项 |
| Q7 | `uploads/` 静态前缀与新镜像路径的对齐责任人（SRE/前端）？ | 附件可用性 | SRE |

**决策记录（评审后填写）**

| 决策 | 结论 | 日期 | 决策人 |
|:---|:---|:---|:---|
| D1 迁移 vs 升 Next 16 | 待定 | | |
| D2 生产 BFF 去留 | 待定（建议 D2-a） | | |
| D3 路由定义方式 | 待定（建议 `routes.ts` 代码表） | | |
| D4 单测框架 | 待定（建议暂不迁） | | |

---

## 12. 附录

### 12.1 证据与复现命令（2026-09-22 本机）

| 证据 | 命令 | 结果摘要 |
|:---|:---|:---|
| 路由数 | `rg --files -g 'page.tsx' itsm-frontend/src` | 166（其中 156 含 `'use client'`） |
| Next 专有 API 命中 | `rg -c "next/navigation"` 等 | navigation 130 / link 16 / dynamic 4 / image 2 / script 1 / headers 0 / use server 0 |
| 无服务端取数 | `rg "export default async function" src/app -g 'page.tsx'` | 0 |
| dev 编译耗时 | dev server 日志 | `/login` 198.1s、`/tickets/create` 134.7s（Turbopack）；webpack `/login` 345.4s |
| 预热命中 | `npm run dev:warm -- /dashboard /tickets` | 4.9s / 5.0s（200） |
| 无 Turbopack 持久缓存 | 检查 `.next/cache/` | 仅 `swc` + `webpack`；15.5 启用持久缓存抛 `CanaryOnlyError` |
| 生产 API 直连 | `nginx/conf.d/default.conf:52-54`、`docker-compose.prod.yml:388-395` | `/api/` → `itsm-backend:8090`；3000 不对宿主发布 |
| BFF 实现 | `src/app/api/[...path]/route.ts:8-140` | `PUBLIC_PATHS`、`BLOCKED_PATHS`、JWT 校验、`X-Forwarded-For` |
| 守卫/重定向 | `src/middleware.ts:6-48,65` | 受保护路由表 + `LEGACY_MENU_REDIRECTS` |
| Next 配置耦合 | `next.config.ts:29-44,47-60,61,66-71` | `rewrites` / `images` / `output: 'standalone'` / TS-ESLint 忽略 |
| 依赖耦合 | `package.json:51,69` | `next`、`@ant-design/nextjs-registry`（`swr` 0 引用） |

### 12.2 npm 脚本对照

| 现状 | 迁移后 | 说明 |
|:---|:---|:---|
| `npm run dev`（`next dev --turbopack`） | `npm run dev`（`vite`） | 端口保持 3000 |
| `npm run dev:webpack` | 删除 | 无对应需求 |
| `npm run build`（`NEXT_DISABLE_TURBOPACK=1 next build`） | `npm run build`（`tsc -b && vite build`） | 产物 `dist/` |
| `npm run start` | `npm run preview`（`vite preview`） | 仅本地预览；生产由 nginx 服务 |
| `npm run dev:warm` | 删除 | 无服务端编译，预热脚本失去意义 |
| `npm run type-check` / `test:unit` / `test:e2e` | 保持（E2E 的 `webServer` 改 Vite） | 见 §8.1 |

### 12.3 关键文件索引

| 文件 | 迁移处置 |
|:---|:---|
| `src/app/layout.tsx` | 拆为 `index.html` + `main.tsx` + Provider 树（`next/script`、`next/font` 残留清理） |
| `src/app/(main)/layout.tsx` 等 3 个 layout | `src/layouts/*.tsx` + `<Outlet />` |
| `src/app/**/page.tsx`（166） | `src/pages/**` + `routes.ts` 登记（脚本生成） |
| `src/middleware.ts` | `src/routes/guards.tsx` + `src/routes/legacy-redirects.ts` |
| `src/app/api/[...path]/route.ts` | 删除（dev → `server.proxy`；prod → nginx 现状已直连） |
| `src/app/api/health/route.ts` | 删除（nginx `/health` 或后端 `/health`） |
| `next.config.ts` / `next-env.d.ts` | 删除，能力拆到 `vite.config.ts` + nginx |
| `Dockerfile`（生产阶段 `node server.js`） | 静态产物 + nginx（§5.9） |
| `docker-compose.prod.yml:364-407` | 前端服务改写（构建参数/健康检查/卷路径） |
| `nginx/conf.d/default.conf:37-54` | `location /` 静态 + `try_files`；`/api/`、`/uploads/`、`/health` 收口 |
| `e2e/**`、`package.json` scripts | E2E `webServer` 改写；脚本按 §12.2 调整 |

### 12.4 参考

- Vite 官方文档（`server.proxy`、`optimizeDeps`、`build.rollupOptions`）
- react-router v7 数据路由（`createBrowserRouter`、`RouterProvider`、嵌套路由）
- Tailwind CSS 4 的 Vite 插件接入方式
- Next.js 15→16 升级说明（备用止血方案 B 的依据，见 §3.1）

---

## 13. 变更记录

| 版本 | 日期 | 变更 | 作者 |
|:---|:---|:---|:---|
| v1.0 | 2026-09-22 | 初稿：现状诊断、目标架构、WBS、实现细则、风险与验收 | — |
