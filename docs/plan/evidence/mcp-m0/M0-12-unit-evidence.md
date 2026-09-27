# M0-12 证据（MCP 管理页：服务器 / 基础工具治理 / 健康摘要）

> 验收项：A0-12（对应任务 M0-12）｜目标 DoD：`unit_verified` + 人工冒烟
> 实际状态：**`unit_verified`**（组件测试 12 用例 + API 客户端 11 用例全绿；`tsc --noEmit` 与 `eslint` 干净）。
> **人工浏览器冒烟为待办**：需真实后端会话（登录 + `mcp.enabled=true` + 可连 MCP 服务器），
> 安排到 M0-14 集成验收一并执行并归档截图（届时可复用 M0-13 的 mock MCP 服务器）。
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，Node 22 + Vite + Jest 29（jsdom），基线 `c971edb0`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 说明 |
| --- | --- |
| `itsm-frontend/src/lib/api/mcp-api.ts`（新增，~380 行） | 管理面 API 客户端：13 个端点（读 5 / 写 8）、DTO 类型（ServerView/ToolView/ConnectionTestResult/Event…）、错误码映射（15 个 §5.5 字符串码 → 中文提示）、三维降级判定（`isMCPFeatureDisabled` 404 无错误码 = 开关关闭；`isMCPServiceUnavailable` 503/`unavailable` = 服务未就绪） |
| `itsm-frontend/src/pages/(main)/admin/mcp-servers/index.tsx`（新增，~1.3k 行） | 三段式管理页：服务器（汇总卡 + 列表 + 三步向导 + 测试连接 + 启停/重载/删除 + 事件抽屉）、工具治理（服务器选择 + 行内分类标注 + 单工具启停 + 批量启停 + 二次确认）、健康摘要（计数 + 状态/最近错误 + 事件） |
| `itsm-frontend/src/pages/(main)/admin/mcp-servers/mcp-helpers.ts`（新增） | 纯逻辑：三态口径（管理位/运行态/生效态）、工具标签优先级、掩码展示、标识/地址校验（与后端 `validation.go` 同口径）、超时/并发/重试范围常量、轮询判定 |
| `itsm-frontend/src/pages/(main)/admin/mcp-servers/__tests__/{index,mcp-helpers}.test.tsx/ts`（新增） | 组件 + 纯逻辑用例（见下） |
| `itsm-frontend/src/lib/api/__tests__/mcp-api.test.ts`（新增） | API 客户端用例（URL/方法/URL 编码/兜底/错误码/降级判定） |
| `docs/plan/_data/vite-route-map.csv`、`itsm-frontend/src/routes/{index.tsx,route-paths.ts}` | 新路由 `/admin/mcp-servers`（与生成器输出逐行一致） |
| `itsm-frontend/src/components/layout/sidebar/menu-config.ts` | capability 规则：`/admin/mcp-servers → ai`（AI 能力关闭时一并隐藏入口） |
| `itsm-backend/pkg/seeder/seeder.go` | 菜单种子：`MCP 外部工具`（ParentPath `/admin`，PermissionCode `mcp:admin`，SortOrder 286） |
| `itsm-frontend/src/lib/i18n/translations.ts` | `mcp.*` 文案（zh-CN 与 en-US 各 ~120 键） |

## 关键实现点（对齐任务卡"要点"）

1. **三步向导**：基本信息（标识/显示名/传输/地址）→ 认证（凭据类型 + 键值对 + 自定义请求头）→ 高级（超时/并发/重试/信任级别）；
   `下一步` 仅校验当前步骤字段（`WIZARD_STEP_FIELDS`），校验失败停留本步并展示错误。
2. **凭据只写不读回**：编辑态只显示掩码**键名**（`credential_masked`/`headers_masked`），键值以密码框录入且**不回填**任何值（含掩码串）；
   提交时空键值数组 → `undefined`（= 不修改），与后端"空凭据 = 不修改"语义一致。
3. **测试连接**：成功后展示协议版本、服务端信息、耗时、工具预览列表；失败展示错误码可读提示（`ssrf_blocked`/`protocol_mismatch`/`auth_required`…）。
4. **异步启停**：`enable/disable/reload` 均按 D8 语义（202 + 2s 轮询 `GET /:id`），过渡态（connecting/reconnecting/configuring）继续轮询，
   超过 30s 降级提示"仍在处理"；停用/删除/批量停用/全部启停均二次确认。
5. **三态分离**：列表同时展示**管理位**（Switch）、**运行态**（connected/error/…）、**生效工具数**与**隔离计数**；
   工具表区分 `configured_enabled`（治理位）与 `enabled`（生效 = 治理位 ∧ healthy ∧ ¬quarantined），标签优先级 隔离 > 停用 > 不可用 > 生效。
6. **空态引导**：无服务器时 `Empty` + CTA；功能开关关闭/服务未就绪时整页降级为引导卡片（不渲染空表）。

## 执行记录

| # | 命令（workdir=`itsm-frontend`） | 结果 |
| --- | --- | --- |
| 1 | `npx tsc --noEmit` | exit 0（全仓类型检查通过） |
| 2 | `npx eslint src/lib/api/mcp-api.ts src/lib/api/__tests__/mcp-api.test.ts "src/pages/(main)/admin/mcp-servers" src/routes/index.tsx src/components/layout/sidebar/menu-config.ts` | exit 0 |
| 3 | `npx jest src/lib/api/__tests__/mcp-api.test.ts --coverage=false` | 11 passed |
| 4 | `npx jest mcp-servers --coverage=false --runInBand --silent` | 12 passed（页面 3 + 纯逻辑 9） |
| 5 | `go vet ./pkg/seeder/`（workdir=`itsm-backend`，菜单种子改动） | exit 0 |

### 断言清单（对应任务卡"测试与证据"）

| 要求 | 覆盖用例 |
| --- | --- |
| 三态展示 | `index.test.tsx`：`connected` 行展示运行态 + 协议版本；`enabled=false` 行展示"未启用"并保留最近错误（`auth_required`）；隔离计数 `1 个已隔离` 与工具级状态分离 |
| 批量操作 + 确认弹窗 | 同上：`批量停用` → 确认弹窗（文案含"该服务器全部工具"）→ 确认前不发请求 → 确认后 `bulkSetTools(1,{enabled:false})` |
| 掩码回传 | 同上：编辑弹窗展示"已保存凭据（掩码）：token"、`queryByDisplayValue('****')` 为空、标识字段 disabled（不可变） |
| 表单校验 | 同上：向导第一步空标识 → "请填写标识"；`Bad Name` → 格式错误文案；两次均未调用 `createServer` |
| 降级路径 | 同上：404（无 errorCode）→ "MCP 外部工具功能未启用"；503/`unavailable` → "MCP 管理服务未就绪" |
| 状态/校验口径（快速回归） | `mcp-helpers.test.ts`：运行态分色、工具生效与标签优先级、健康告警、掩码键名、标识/地址校验、范围常量、轮询判定 |
| API 契约 | `mcp-api.test.ts`：列表/健康/测试/启停/重载/工具清单/单工具启停/批量/分类标注（`encodeURIComponent`）/凭据轮换/事件/删除；错误码映射与降级判定 |

## 偏差与说明（相对任务卡）

1. **类型文件位置**：任务卡建议新建 `lib/types/mcp.ts`；仓库无 `lib/types/` 目录（既有范式是 `lib/api/llm-provider-api.ts` 内联类型），
   故类型与客户端同文件，避免引入新目录约定。
2. **路由生成物处理**：`route-paths.ts` 头部标注"do not edit by hand"，唯一真源是 `docs/plan/_data/vite-route-map.csv`（生成器 `gen_routes.py`）。
   本次：① 在 CSV 追加 `/admin/mcp-servers` 行；② 用生成器产出比对后，**手工**把三处改动（lazy 声明、路由项、ROUTE_PATHS 条目）落到 CRLF 的仓库文件中
   （生成器输出为 LF，直接落盘会造成 774 行全文件 diff）。三处内容与生成器输出逐行一致。
3. **组件测试组织**：本机 antd+jsdom 渲染极慢（单次 render 数十秒，页面套件约 290s），故页面用例按"一次渲染覆盖多条行为"组织（3 个），
   纯口径/校验下沉到 `mcp-helpers.test.ts`（9 个，毫秒级）；断言覆盖面以任务卡清单为准而非"一行为一用例"。
4. **隔离解除**：后端没有"解除隔离"管理端点（`quarantined` 由工具发现流程写入，成因修复后重载服务器重新发现才会清除），
   页面只展示隔离标签 + 原因 + 提示"修复后重载"，不提供无后端支撑的假操作按钮。
5. **人工冒烟待办**：见文首说明（M0-14 执行；需真实后端会话与可连服务器）。

## 未覆盖 / 待办

- **M0-13**：mock MCP 服务器（供 M0-14/E2E 使用）——子代理实施因运行时排队失败未产出，改由主会话本地实施。
- **M0-14**：真实环境端到端（新增 → 测试 → 启用 → 发现 → 治理 → 对话只读调用 → 审计可查）+ 管理页截图 + `-race`。
- **M2-05**：完整 E2E（Playwright）与治理/schema 隔离场景。
- 工具分类标注当前为**逐行保存**；批量标注（多选后一次提交）未做（后端仅单工具端点）。
