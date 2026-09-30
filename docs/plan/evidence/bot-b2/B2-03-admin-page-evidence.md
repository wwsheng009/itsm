# B2-03 实施证据（Bot 模板与工具授权管理页）

> 文档类型：实施证据（任务 B2-03）
> Status: draft
> 编制日期：2026-09-27
> 任务：B2-03（管理页模板/授权 CRUD + 影响面预览；依赖 B2-01）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.3 B2-03、§5.2 AB2-03）
> 核查方式：前端组件测试（antd + jsdom）+ API 纯函数测试 + `tsc --noEmit` + eslint + 后端 seeder 包 A/B 对照

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B2-03 | **`unit_verified`** | 模板 CRUD、授权 CRUD、影响面预览、表单校验、读写权限分离（403 只读提示）全部落地并测过；条件项：手工全流程（新增→授权→生效→回收）留待 B2-06/BT-09 浏览器级执行 |
| 兼容默认 | ✅ | `bot.enabled=false`（整组路由 404）→ 页面降级"功能未启用"引导，不渲染列表、不影响既有系统 |
| 只读可用 | ✅ | 列表/详情/授权读取走 `ai:read`；写动作 403 → 前端只读提示且不阻塞浏览 |
| 测试 | ✅ | 页面 7 例 + API 纯函数 12 例全绿；`tsc` 与 eslint 干净 |

## 2. 交付物

| # | 层 | 文件 | 说明 |
| --- | --- | --- | --- |
| 1 | 前端 API | `itsm-frontend/src/lib/api/bot-api.ts`（新建） | 8 个端点客户端（`/api/v1/admin/bots`…）+ 纯函数（`parseEntrypoints`/`serializeEntrypoints`/`buildImpactPreview`/`grantRiskExceeds`/`isBotFeatureDisabled`/`isBotPermissionDenied`） |
| 2 | 前端页面 | `itsm-frontend/src/pages/(main)/admin/bots/index.tsx`（新建） | 模板列表 → 新建/编辑（含**影响面预览**）→ 授权抽屉（工具 × 风险上限 × 参数策略） |
| 3 | 路由 | `itsm-frontend/src/routes/index.tsx`、`route-paths.ts`（修改） | `/admin/bots` 登记（懒加载 + 静态路径集合） |
| 4 | 菜单 | `itsm-backend/pkg/seeder/seeder.go`（修改） | `/admin` 下新增「Bot 管理与授权」（`ai:read`，SortOrder 287，Icon `Bot`） |
| 5 | 路由源数据 | `docs/plan/_data/vite-route-map.csv`（修改） | 新增 `/admin/bots` 行（生成器的单一真源） |
| 6 | i18n | `itsm-frontend/src/lib/i18n/translations.ts`（修改） | `botsAdmin.*` 中英双语（含状态/受众/风险枚举标签、影响面告警文案） |
| 7 | 测试 | `bots/__tests__/index.test.tsx`（7 例）、`src/lib/api/__tests__/bot-api.test.ts`（12 例） | 见 §4 |

## 3. 关键设计（与 B2-01/B2-02 契约对齐）

| 点 | 实现 | 依据 |
| --- | --- | --- |
| 枚举字面量 | status `draft/pilot/ga`；risk `read/plan/act_low/act_medium/act_high`；audience `internal/end_user/all` | `service/bot/admin.go` 常量 |
| 风险上限约束 | 授权风险上限不得超过模板上限（前端预检 + 后端强校验双保险） | `service/bot/admin.go` riskRank |
| slug 口径 | **不硬校验**：后端仅要求非空 + 租户唯一；前端只在格式非常见时给**软提示**，绝不阻塞提交 | `CreateTemplate` 实现（无正则/长度约束） |
| 入口（entrypoints） | 自由录入（tags）+ 提示"仅 `chat` 为已冻结常量，其余待 B3-01 枚举"；未配置入口 → 影响面预览显式告警"fail-closed 等同禁用" | `EntrypointChat = "chat"`；`entrypointAllowed` fail-closed |
| 影响面预览 | 纯函数返回机器码（`no_entrypoints`/`draft`/`no_grants`）+ 受众集合，文案由 i18n 渲染 | 阶段一报告 §5.7 要求"保存前展示影响面" |
| slug 不可变 | 编辑态禁用 + 提示；后端对非空 slug 直接 400 | `UpdateBotTemplate` 实现 |
| 兼容默认 | 空授权/未配置 Bot = 兼容默认（只读 ∪ 遗留写白名单），页面在授权抽屉与影响面预览两处显式说明 | B2-02 冻结口径 |
| 读写分离 | 读 `ai:read`、写 `ai:write`；403 不视为"未启用" | `router/bot_routes.go` 门禁 |

## 4. 运行记录（本机，pwsh）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `npx jest --testPathPattern "admin/bots"` | **7 例全绿**（列表/入口空态/新建校验与提交/授权越界拦截与增删/删除模板/功能未启用降级/403 只读） |
| 2 | `npx jest src/lib/api/__tests__/bot-api.test.ts` | **12 例全绿** |
| 3 | `npx tsc --noEmit` | exit 0（干净） |
| 4 | `npx eslint src/pages/(main)/admin/bots src/lib/api/bot-api.ts src/components/ai/BotSelector.tsx` | exit 0 |
| 5 | `go test ./pkg/seeder/ -count=1` | 5 例既有失败（见 O-4）；**A/B 对照已证与本次改动无关**（stash 掉 seeder.go 两行改动后逐字复现同一失败） |

## 5. 遗留与发现

| # | 项 | 归属/处置 |
| --- | --- | --- |
| 1 | G-B2-03-1：`python .dev/vite-migration/gen_routes.py` 全量重生成会产生 **1000+ 行无关 diff**（route-paths.ts 与 index.tsx 整文件重写，疑为行尾/生成器版本漂移）。本次改用手工最小登记（1 行 import + 1 行路由 + 1 行路径） | 登记为工程债：建议单开一次"生成器产物对齐"小任务，避免每次加路由都触发全文件 diff |
| 2 | G-B2-03-2：手工全流程（新增 → 授权 → 生效 → 回收）与浏览器截图 | **B2-06 / BT-09**（需要真实后端 + 登录会话） |
| 3 | O-4（既有失败，非本任务引入）：`pkg/seeder` 5 例失败（`incident_emergency_flow: process_definition not found` 系列） | A/B 对照确认 pre-existing；建议独立小任务处置（属工作流种子数据问题，与 Bot 菜单行无关） |
| 4 | 入口枚举待 B3-01 落地后收敛 UI 建议值 | B3-01 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B2-03 交付：`/admin/bots` 管理页（模板 CRUD + 授权抽屉 + 影响面预览 + 读写分离）+ 菜单/路由/i18n 登记；页面 7 例与纯函数 12 例全绿，`tsc`/eslint 干净；判定 `unit_verified`（手工全流程归 B2-06；登记 G-B2-03-1 生成器漂移、O-4 既有失败） |
