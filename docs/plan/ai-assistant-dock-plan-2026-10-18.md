# 常驻 AI 助手（Copilot Dock）与自维护帮助文档 · 深化规划

- 日期：2026-10-18
- 状态：规划定稿（待评审；未开工）
- 定位：B 系列（Bot 运行时 B0–B4）之后的**体验层 B5**，并新增 **H 系列（帮助文档智能维护）**
- 修订：v2（2026-10-18）——按"AI 驱动的业务系统"目标补强交互完整性：新增 §3.2–3.7 交互规范、三种 AI 形态与 I 系列任务
- 修订：v3（2026-10-18）——补"最终用户价值验证"（§11）与 V 系列任务；路线调整为**试点价值先行**，Dock 放量以试点数据为门槛
- 修订：v4（2026-10-18）——新增 §13 观测性与分析（O 系列）：事件字典、四层看板、全链路轨迹、告警与试点对照报告
- 前置阅读：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（B0–B4 已交付）、`docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`

---

## 1. 目标与非目标

### 目标
1. AI 助手常驻前端：悬浮球 ↔ 侧栏面板，任意页面可唤起，可收纳为边缘图标；
2. 助手感知当前路由与业务场景，给出**可执行**的页面级建议（创建单据、搜索、分析等）；
3. 面向页面的帮助文档：缺失可检测、AI 可起草、AI 可更新，人工介入最小化；
4. 全部能力建立在现有 Bot 治理面之上（模板/授权/风险/入口/确认/审计/指标），不新增平行写路径；
5. **以最终用户的任务完成为中心**：每个能力都要能回答"它替用户省了哪几步"，并用对照数据验证（§11）。

### 非目标
- 不做"模拟点击页面 DOM"的浏览器代理；
- 不把 168 条路由塞进后端 entrypoint 枚举（授权矩阵会爆炸）；
- 不新造文档存储系统（产品文档走 docs-as-code，租户文档走知识库）。

---

## 2. 决策定稿（按最佳实践确认）

| # | 决策点 | 结论 | 理由 |
|---|---|---|---|
| D1 | 面板形态 | 右侧**停靠侧栏**（≥1024px）+ 移动端 Bottom Sheet；可收起为边缘 FAB（浮动球） | 业界 Copilot 主流；不遮挡主内容；与既有通知抽屉/全局搜索的层级关系清晰 |
| D2 | 建议生成 | **两段式**：P0 前端静态注册表（基线、零延迟）；P1 上移服务端并按「可见 Bot ∩ 工具授权 ∩ RBAC」过滤 | 首屏即时；治理在服务端兜底；杜绝"点了就被拒"的建议 |
| D3 | 页面与 Bot 绑定 | **统一助手 + 场景自动切换**：页面注册表声明 `botSlug`/场景，未声明则用默认助手 | 用户心智单一；避免"每页一个 Bot"的配置爆炸（现有 7 个场景 Bot 直接复用） |
| D4 | 零输入发起 | 允许，但**仅限只读/plan 动作**（如"生成影响分析计划"）；写动作一律走确认闭环 | 与 B1 Gate3 / confirmation 口径一致 |
| D5 | 帮助文档源 | **docs-as-code 为产品文档单一真源**（`docs/help/**`）+ 知识库承载租户文档；AI 起草走「PR 通道 / KB 草稿通道」双通道；覆盖率用 **ratchet 门禁**（参考根目录 `test-coverage-guard.json` 先例） | 版本化、可 diff、可审计；复用 KB 的版本/复核/时效/RAG 准入与 mkdocs 站点；覆盖率只升不降 |

---

## 3. 体验设计（Dock）与交互规范

### 3.1 基础形态

1. **入口**：右下角 FAB（含运行中状态点）；点击展开侧栏；可拖拽收纳为边缘竖条图标；`Ctrl/Cmd+I` 唤起/收起（并入 `GlobalShortcutProvider`）。
2. **权限**：无 `ai:read` 不渲染（沿用 Header 现有判断）。
3. **面板结构**：上下文条 → 建议区（3–5 条页面级提示）→ 会话区（复用 chat-core）→ 输入坞（`/` 快捷指令、`@` 引用当前页对象）→ 帮助视图（Tab 切换）。
4. **状态持久化**：展开态/宽度/上次会话/静音建议，按用户维度存 zustand persist。
5. **路由切换**：会话不中断；上下文条切换并提示"上下文已更新"；若会话绑定旧目标，输入坞默认清除目标（避免误操作）。
6. **移动端**：Bottom Sheet 全屏展开，避免与通知抽屉叠加。

### 3.2 交互状态机（面板）

| 状态 | 触发 | 界面与可用动作 |
|---|---|---|
| closed(FAB) | 默认 | 悬浮球；运行中任务/未读回执显示状态点 |
| opening | 点击/快捷键 | 骨架屏；可 ESC 取消 |
| ready | 上下文与建议就绪 | 任务卡 + 问题卡 + 帮助状态 + 输入坞 |
| streaming | 发送后 | 实时消息；可"停止生成"；页面可继续操作 |
| tool_running | 工具调用 | 时间线增量；可折叠；FAB 状态点同步 |
| awaiting_confirmation | 写动作前 | 确认卡（确认/拒绝/查看详情）；超时沿用 confirmation sweeper |
| awaiting_approval | Gate3 人工审批 | 进度说明 + "去审批中心"入口 |
| degraded / error | Provider 未配置、预算超限、流失败、离线 | 原因码 → 可读文案 → 恢复动作（去配置/重试/降级一次性回答） |
| done | 任务收口 | 回执卡（结果落点/证据/审计链接）+ 下一步建议 |
| handoff | AI 不适用/用户选择 | 转人工：打开对应页面/预填表单/指派 |

- 路由切换：会话不中断（状态与页面解耦）；上下文条更新并提示。
- 状态、错误文案、恢复动作由**错误字典**统一维护（原因码 → 文案 → 动作），组件测试逐状态覆盖。

### 3.3 任务优先（Task-first）交互

对话只是形态之一；**业务任务**才是主模型：

```
意图（任务卡/自由输入）→ 计划预览（dry-run，复用 dry-run-snapshot）
  → 确认（写动作）→ 执行（run/step 时间线）→ 验证（verify 回读）
  → 回执（结果落点）→ 撤销/补救（undo）
```

- **任务卡**：每个页面注册 1–3 个可零输入执行的"任务"（如工单详情："分诊建议""生成答复草稿""关联知识"）；建议区 = 任务卡（可执行）+ 问题卡（了解）。
- **结果落点契约（Outcome Contract）**：可写任务必须声明 `outcome: {type: entity|artifact|draft|navigation, ref, applyTarget}`；回执卡据此渲染"打开对象 / 应用到表单 / 追加到当前单据"。
- **一键应用（Inline Apply）**：字段级建议（分诊类别/优先级等，后端已产结构）在页面内直接"应用"；应用前展示 diff 预览，应用后可撤销（本地回滚 + 审计留痕）；**绝不自动提交**。

### 3.4 三种 AI 形态（形态与场景匹配）

| 形态 | 交互 | 典型场景 | 治理 |
|---|---|---|---|
| Copilot（对话） | 面板聊天 | 咨询、分析、起草 | 现有 Bot 治理 |
| Inline（页内） | 字段建议/一键应用/解释 | 分诊应用、表单预填、错误解释 | 只改草稿，提交仍走页面 |
| Autopilot（代理） | 后台任务 + 监督面 | 定时巡检、批量草稿、升级建议 | 只读/plan 自动；写动作进审批中心 |

- Autopilot 的输出统一落到监督面（复用 `/ai/approval` 与通知中心），支持暂停/回滚，不做"无感自动写"。

### 3.5 降低使用成本的输入通道

- **零输入**：任务卡直达（3.3）；
- **划词问 AI**：任意页面选中文本 → 悬浮"问 AI"（带入面板并预填）；
- **报错解释**：表单/接口错误旁"解释这个错误"（错误信息 + 页面上下文）；
- **命令面板整合**：现有 `Ctrl+K` 全局搜索（`GlobalSearch`）增加"问 AI"结果区与"在助手中继续"；助手面板内 `Ctrl+K` 可跳搜索；
- **附件/截图**：复用附件与多模态（若模型不支持视觉则降级为文本描述）；
- **语音输入**（P2，可选，Web Speech API）。

### 3.6 主动协助（不打扰原则）

- 默认全静默；允许的主动形态仅三种：
  1. FAB 状态点/徽标（任务完成、审批结果、SLA 临期等注册表声明的高价值事件）；
  2. 页面内联建议条（可关闭、"不再提示"记忆）；
  3. 通知中心投递（复用现有通知系统，用户可订阅/退订）。
- **禁止**：自动弹窗、打断输入、自动提交；频率上限与静默期由用户与租户策略共同决定。

### 3.7 交叉切面（工程与合规）

- **无障碍（WCAG 2.1 AA）**：完整键盘路径（Ctrl+I/ESC/Tab 焦点陷阱）、流式内容 `aria-live`、减少动效适配；
- **国际化**：建议/任务卡文案走 `useI18n` 翻译键；回复语言跟随界面语言；
- **合规**：会话保留/导出/删除策略；上屏前经 Redactor 脱敏；审计可检索（复用 `ai_audit`）；
- **性能**：chat-core 懒加载（首屏不加载对话 bundle）；建议区骨架屏；离线只读降级；
- **成本**：模型分级路由（任务卡/轻问答用轻模型，复杂任务用强模型）；预算与限流状态在面板可见；
- **灰度与回滚**：`assistant.dock.enabled` 按租户/角色/页面白名单推进；指标达标才扩大；保留一键关闭；
- **双入口一致性**：`/ai/chat` 工作区与 Dock 共用 chat-core；支持"在侧栏继续 / 全屏打开"，上下文经 store 传递（不再依赖 history state）。

---

## 4. 上下文模型与协议

### 4.1 三层上下文

| 层 | 内容 | 来源 | 边界 |
|---|---|---|---|
| L0 全局 | 路由、页面标题、面包屑、角色、租户 | `useLocation` + 菜单/面包屑元数据 | 无隐私风险 |
| L1 页面声明 | `pageKey`、场景、`botSlug`、建议问题、快捷动作、帮助文档引用 | 前端页面注册表（静态声明，随 UI 版本） | 可单测；随构建产出 Page Manifest |
| L2 运行时数据 | 详情对象 id/摘要、列表筛选 | 页面**显式**注册 context provider | 禁止自动采集；用户可见（上下文条）；服务端仍以对象预检为事实源 |

### 4.2 协议扩展（向后兼容）

- 前端请求新增可选字段：`pageContext: { pageKey, route }`；
- **缺省请求体逐字节不变**（快照测试锁定，与 B3-02 同口径）；
- 后端：`page_key` 仅作**提示与审计**，不参与授权；未知 `page_key` 降级为忽略（不 400，避免前端版本先行的硬失败）；
- 页面 → entrypoint 映射：页面注册表声明"最近语义入口"（如 `/tickets/:id` → `ticket_detail`），授权与工具门禁仍按 entrypoint 走（单一事实源不变）。

---

## 5. 帮助文档智能维护子系统（H 系列）

### 5.1 内容架构（单真源、双投影）

```
产品帮助文档真源：docs/help/**/*.md（docs-as-code）
        │  构建期
        ├─→ help-index.json（全文/语义索引 → 向量库导入，复用 /admin/vector-store 能力）
        ├─→ mkdocs 站点（website/，随版本发布）
        └─→ Doc Ledger（覆盖率账本，CI ratchet 门禁）

租户文档：知识库「使用手册」分类（KB 已有：版本、复核时效、权威等级、RAG 准入）
        └─→ 面板运行时与产品文档**合并展示**，显式标注来源
```

- 文档 frontmatter 规范（新增，校验器强制）：
  ```yaml
  ---
  page_keys: [ticket.detail, ticket.list]   # 必填，关联页面注册表
  audience: internal                        # internal | customer | admin
  version: 1.8                               # 适用产品版本
  last_reviewed: 2026-10-18
  owner: service-desk                        # 兜底责任人（仅用于分派，不阻塞）
  ---
  ```
- **单一真源规则**：页面级帮助内容只允许存在于 `docs/help/**` 或 KB（`source: help|tenant` 标签区分），禁止双写。

### 5.2 Page Manifest（AI 起草的"事实底稿"）

- 构建脚本 `scripts/gen-page-manifest.mjs` 从页面注册表 + 路由表 + 后端工具目录生成 `page-manifest.json`：
  ```json
  { "pageKey": "ticket.detail", "route": "/tickets/:id", "title": "工单详情",
    "scene": "s1", "entrypoint": "ticket_detail",
    "actions": ["ticket.reply", "ticket.assign"],
    "botTools": ["list_tickets", "create_ticket", "draft_ticket_fields"],
    "docs": ["help/tickets/detail.md"], "status": "ok" }
  ```
- 意义：AI 起草文档时有**结构化事实**可引用（字段、动作、可用工具、权限），而不是靠猜；这也是"尽量让 AI 完成"的前提。

### 5.3 Doc Ledger 与覆盖率检测

- `docs/help/_ledger.json`（生成物，CI 校验）：
  ```json
  { "generated_at": "...", "coverage": 0.72,
    "pages": { "ticket.detail": { "status": "ok", "docs": ["..."], "stale": false } } }
  ```
- 判定：
  - `missing`：注册表存在页面但无文档；
  - `stale`：文档 mtime/`last_reviewed` 落后于页面/路由/动作的变更（git 元数据）；
  - `orphan`：文档 `page_keys` 指向已不存在的页面。
- 门禁（ratchet）：`missing/stale` 数量不得增加（对齐 `test-coverage-guard.json` 的只升不降思路）；
- 运行时：`GET /ai/help/coverage?pageKey=` 返回当前页状态，面板帮助视图据此提示。

### 5.4 Help Agent 工具集（复用 Bot 工具治理）

| 工具 | 风险 | 确认 | 输出 |
|---|---|---|---|
| `help_search` | read | 无 | 文档/KB 片段 + 引用 |
| `help_get_page` | read | 无 | 当前页帮助与覆盖状态 |
| `help_read_doc` | read | 无 | 文档原文（含 frontmatter） |
| `help_list_gaps` | read | 无 | 缺失/过时清单（供批量任务） |
| `help_create_draft` | plan | 无（只落草稿） | KB「使用手册」草稿 / 文档分支 diff |
| `help_update_draft` | plan | 无 | 新版本草稿（依赖 `help_*_draft` 引用） |
| `help_submit_review` | act_low | **有** | 提交审阅（KB 复核流 或 PR） |
| `help_publish`（仅管理员/可按租户关闭） | act_medium | **有** | 发布 KB 文章 / 合并 PR |

- 与场景 Bot 的绑定：S3（知识自助助手）授予 read+plan 面；`help_submit_review` 属 act_low，按角色/入口授权；`help_publish` 默认仅平台管理员。
- 负向断言：Help Agent 与"删除/下线文档"零交集；知识实体写入只发生在草稿面。

### 5.5 AI 起草闭环（人最少）

```
检测（缺失/过时/提问零命中）
   → AI 起草（grounding：Page Manifest + 相关文档 + KB + 工具目录；PR 通道下还可读仓库代码）
   → 自动校验（结构 / page_keys / 链接 / 引用事实存在于 manifest / 敏感信息 / mkdocs 构建）
   → 提交（KB 草稿 或 Git PR）
   → 分级人工（见 5.7）→ 发布 → 索引 → 度量
```

- 页面内触发：面板帮助视图显示「该页面暂无帮助文档 → 让 AI 生成初稿」；生成后展示 diff 预览，一键提交审阅。
- 批量触发：管理页「帮助覆盖率看板」（对标现有 `bot-metrics`/`ai/audit` 页）支持勾选 missing/stale 页面批量生成草稿（后台任务队列，产物进审阅队列）。

### 5.6 触发源（四类）

1. **运行时页面检测**：打开面板即查询当前页覆盖状态（缺失/过时）；
2. **用户显式请求**："帮我给这个页面写/更新帮助文档"（走 5.4 工具链）；
3. **遥测缺口**：同一页面反复提问且文档/KB 零命中 → 记录 `help_gap` 事件 → 聚类为文档待办（复用 `/ai/feedback`、`ai_audit` 埋点面）；
4. **代码变更**：CI 对比 Page Manifest/路由 diff → 标记相关文档 `stale` → 夜间批任务生成更新提议（PR 通道可直接开 PR）。

### 5.7 最少人工分级

| 级别 | 场景 | 人工动作 |
|---|---|---|
| L1 机械修正 | 链接、错别字、frontmatter 补全 | 校验全绿可**自动合并/自动发布**（策略开关，默认关，先观察） |
| L2 内容更新 | 字段/流程变化后的文档修订 | AI 起草 + **一键审阅**（diff 视图） |
| L3 新增页面文档 | 新页面首次补文档 | AI 起草 + **首次必须人工过目**（可指派 owner） |

原则："AI 完成 95%，人只做点击与例外裁决"；所有 AI 写入留审计（模型、prompt_version、diff、触发者、审阅人），复用 `ai_audit` 与 KB 版本表。

### 5.8 与现有资产映射

| 需求 | 复用 |
|---|---|
| 文档版本/复核/时效 | `knowledge_article_version`、`FreshnessGuard`、`/knowledge/reviews` |
| 检索与引用 | `/ai/rag/search`、向量库、`RagAnswer` 引用渲染 |
| 工具治理 | Bot 模板/授权/风险/确认/blacklist/审计（`service/bot/*`） |
| docs-as-code 写入 | `itsm-agent`（watcher→orchestrator→generator→validator→pr），新增 `prompts/docs-maintenance.md` |
| 覆盖率门禁先例 | 根目录 `test-coverage-guard.json` |
| 页面语义初值 | `breadcrumb-utils.ts` 菜单标签、`ROUTE_PATHS`、`vite-route-map.csv` |

---

## 6. 技术方案（文件级）

### 前端
- `src/components/ai/dock/{AssistantDock.tsx,DockHandle.tsx}`
- `src/components/ai/chat-core/{use-ai-chat-session.ts,MessageList.tsx,Composer.tsx,ContextBar.tsx}`（从 `AIChat.tsx` 抽离）
- `src/lib/ai/page-context/{types.ts,registry.ts,use-page-assistant-context.ts}`
- `src/lib/ai/assistant-actions.ts`（动作桥：`actionId → handler`，未注册回退路由跳转）
- `src/lib/store/assistant-store.ts`；`src/lib/api/help-api.ts`
- `scripts/gen-page-manifest.mjs` → `src/generated/page-manifest.json`
- `MainLayout.tsx` 挂载 Dock；`Header.tsx` 的 AI 按钮改为打开 Dock

### 后端（Go）
- `service/bot/scope.go` + `handlers/ai/scope.go`：`page_key` 白名单 + 审计（提示性，不参与授权）
- `handlers/ai/`：`/ai/assistant/context`、`/ai/help/{search,coverage}`
- `service/help/`（新）：Doc Ledger 读取、帮助索引、草稿编排
- Bot 工具注册：5.4 工具集（走既有风险/授权/确认框架）
- 平台开关：`assistant.dock.enabled`、`help.agent.enabled`（沿用系统配置 AI 页签的免重启模式）

### CI / Agent
- `docs/help/**` + frontmatter 规范；`itsm-agent/prompts/docs-maintenance.md`
- CI Job：help-coverage ratchet + 校验器（结构/链接/page_keys/事实引用）

---

## 7. 分期与任务

> 顺序约定（v3）：**V 系列试点与 P0 并行启动**；Dock 的全面放量以 §11.5 门槛为准。
> P0 交付物均为支撑工程，验收时必须附带至少一个 V 系列试点的用户可见收益，禁止"只交壳"。

### V 系列 · 用户价值试点（价值先行，与 P0 并行；2–4 周）
> 原则：先证明"省步数"，再建承载壳。试点全部复用现有资产，不依赖 Dock 完成。
> 基线采集：试点上线前先用同一埋点（O0-01）采集现状流程 1 周基线——对照数据不可回溯补采。

| 任务 | 试点 | 复用资产 | 验收（对照现状基线） |
|---|---|---|---|
| V0-01 | 工单详情三件套：分诊建议 + 答复草稿 + 知识推荐（**Inline 优先**） | 分诊结构、RAG、AIChat 工具链 | 处理一张单：点击次数/输入字符数/耗时 下降 ≥30% |
| V0-02 | 一句话建单 + 自助问答（员工侧） | `/tickets/ai-create`、A2UI、RAG | 自助解决率、建单时长、误分类率 |
| V0-03 | 变更影响分析任务卡（plan 产物） | plan 工具、artifact、dry-run | 计划采纳率、分析耗时 |
| V0-04 | 试点对照报告 + 放量门槛 | O0-03 看板 / bot-metrics / AI 评估 | §11 对照报告自动产出；达标才推进 Dock 放量 |

### V 系列 · 用户价值补齐（试点结论后按优先级投产）
| 任务 | 内容 |
|---|---|
| V1-01 | 列表页批量 AI：选中 N 行 → 建议分类 / 批量草稿 / 批量分派（坐席的真实工作单元是"一批单"） |
| V1-02 | 员工侧入口覆盖：移动端 / 外部门户 / 邮件场景的助手形态 |
| V1-03 | 口语↔术语确认："我理解成：网络故障（高）→ 确认 / 修改" |
| V2-01 | 个性化记忆：常用语、上次处理方式、个人模板（租户策略可关闭） |
| V2-02 | 异步任务体验：先给可用答案，后台精算完成后通知 |

> 进度：**V1-01 列表页批量 AI 已交付**（2026-10-18，`integration_verified`）——选中 N 行 →「AI 建议分类 / 批量草稿 / 按建议分派」。
> 后端 `POST /ai/tickets/batch-triage`（≤20：分类/优先级/建议处理人/置信度）与
> `POST /ai/tickets/batch-reply-drafts`（≤10：草稿 + KB 引用）均**只读生成**；
> 采纳/分派由面板逐行或全部确认后走既有 `PUT /tickets/:id`、`POST /tickets/:id/assign`，绝不自动提交；
> 租户隔离逐项 `not_found`、有界并发（3）保序、单条失败只降级该条（`degraded=true` 仍 200）、
> 审计 `ticket_batch_triage`/`ticket_batch_reply_draft`；埋点复用既有事件（**事件字典零变更**）。
> 前端 `TicketBatchAIPanel` 挂 `TicketList`（`ai:read` 门控；采纳按 `ticket:update`、分派按 `ticket:assign` 收敛），选中即出现。
> 门禁：后端 3 包全绿 + `authz-gen` 再生；前端 2 suites/63 tests（面板 6 例）+ AI 表面 57 suites/489 tests + tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/V1-01-batch-ai-evidence.md`。
> 条件项：面板文案未 i18n（同工单模块现状）；草稿仅复制不自动填入评论框；「按建议分派」为 triage 建议处理人口径；真实 Provider E2E/移动端入口归联调与 V1-02。

> 进度：**V1-03 口语↔术语确认已交付**（2026-10-18，`integration_verified`）——「我理解成：网络故障（高）→ 确认 / 修改」。
> 后端 `POST /ai/tickets/interpret`（只读理解；纯确定性词典、不调 LLM）：口语 → 术语（分类/优先级）+ 证据片段
> （「连不上网」→ 网络故障）+ 置信度；否定表达优先（「不着急」不被「急」误升级）、未识别降级 other/medium（`matched=false`）、
> 空/超长 400 fail-visible；路由 `ai:read` ∩ `ticket:read`。
> 前端 `IntentUnderstandingCard` 挂 `A2UIFormRenderer` 两阶段流程：发送 → **先确认理解** → 才生成表单
> （确认/修正值作为表单初值；理解失败降级为原直接生成，不阻塞建单）；写边界不变（绝不自动提交）。
> 埋点复用既有事件（`confirmation_*` + `ai_create_field_corrected`，**事件字典零变更**）；
> 修正只计一次（确认卡即时上报，生成基线改用确认值，不重复计数）。
> 门禁：后端 interpret 6 组 + router/middleware（含 precheck 新鲜度）全绿 + `authz-gen` 再生；
> 前端 3 suites/68 tests（本批）+ 121 suites/1613 tests（AI 表面回归）+ tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/V1-03-intent-interpretation-evidence.md`。
> - **V1-03 条件项闭环 · 确认卡纳入 axe 扫描面**（2026-10-18，`integration_verified`）：
> `IntentUnderstandingCard` 纳入 I2-02 axe 扫描（归一化术语 + 口语证据段落 + 确认/修改控件），
> 0 violations；扫描面 5 → 6。证据 `docs/plan/evidence/assistant-dock/O2-02-workspace-entry-a11y-evidence.md`。
> 条件项：词典为平台级规则（不可租户化、无 LLM 归一化）；接入点仅员工侧 `ai-create`（Dock 对话/门户/邮件归 V1-02/V2）；
> 真实 Provider/真库 E2E 沿用既有条件项。

> 进度：**V1-02 员工侧入口覆盖已交付**（2026-10-18，`integration_verified`）——移动端 / 门户 / 邮件三形态；助手只给建议/预填，**绝不自动提交**。
> 移动端：Dock <768px 底部全宽抽屉（圆角顶 + `env(safe-area-inset-bottom)` 安全区、FAB 避让手势条、`data-mobile` 形态标记），
> resize 回宽屏复位为桌面右侧面板。
> 门户：新增可复用 `PortalAssistantEntry`——「不知道选哪个 / 不会填」→ 打开常驻 Dock 并预填提示词（不发消息、不提交），
> 已挂服务目录（`service.catalog`）与服务申请（`service.requests`）两面；埋点复用 `assistant_opened`（source=launcher）。
> 邮件：复核抽屉新增 `EmailIntentSuggest`——复用 V1-03 interpret（只读、≤500 字截断）→「我理解成：网络故障（高）」+ 证据 + 置信度；
> 「采纳」仅回填空白字段（不覆盖人工已填），仍需人工「保存并重新核验」；未命中如实标注兜底、理解失败不阻断（可重试）。
> 门禁：AI 表面回归 122 suites/1617 tests + email/service-catalog 域 4 suites/44 tests + tsc/eslint 全绿；后端零改动（无新路由/权限码，免 authz-gen）。
> 证据 `docs/plan/evidence/assistant-dock/V1-02-employee-entry-coverage-evidence.md`。
> 条件项：本仓库暂无独立外部顾客门户（`PortalConfig` 为占位、`PortalHomepage` 未实现）→ 以员工自助门户面落点、组件可复用；
> 邮件落点为 intake 复核面（员工侧邮件自动回复起草归 V2/联调）；真机触控/视觉 E2E、新组件 axe 扫描面、门户文案 i18n 归联调/后续。

> 进度：**V2-01 个性化记忆已交付**（2026-10-18，`integration_verified`）——常用语 / 个人模板 / 上次处理方式（租户策略可关闭）。
> 数据面：`users.preferences.assistantMemory`（白名单扩展 + `validateAssistantMemory` 结构护栏：字段白名单/条数/长度/
> 结构化动作键，非法整体拒绝 400）；租户开关 `assistant.memory_enabled` 接入 M2 能力源（缺行=默认开启，
> 管理台「AI 配置」第 4 行可覆盖关闭），员工侧经 `GET /api/v1/capabilities` 的 `assistant.memoryEnabled` 读取（fail-safe 默认开启）。
> 前端：`src/lib/ai/memory/**` 三档模式矩阵（on→服务端 / anonymous→仅本机不上传 / off→零读写）+ 记忆条 `AssistantMemoryChips`
> （chips 点击**只预填不发送**；显式保存常用语/模板；管理面含最近处理元数据、用户关闭、清空）；
> `use-inline-apply` 应用成功记录元数据动作键（未加载不隐式请求）。
> 合规：元数据优先、不采集对话正文（recent 仅动作键+时间，本地/服务端双重拒绝自由文本）；埋点新增 `memory_updated`（前后端字典 + §13.2 + 契约测试四处同步）。
> 门禁：记忆面 3 suites/24 tests + system-config 域 3 suites/19 tests + AI 表面 125 suites/1641 tests + tsc/eslint 全绿；后端 5 包全绿（免 authz-gen：无新路由）。
> 证据 `docs/plan/evidence/assistant-dock/V2-01-personalized-memory-evidence.md`。
> 条件项：真库/真租户冒烟（system_configs 覆盖行/RLS）归联调；「上次处理方式」采集点当前为 Inline Apply（更多动作点后续接入）；
> 记忆条文案未 i18n、未入 axe 扫描面；多端并发最后写入覆盖。

> 进度：**V2-02 异步任务体验已交付**（2026-10-18，`integration_verified`）——「先给可用答案，后台精算完成后通知」。
> 后端 `POST /ai/async/answers`（prompt 1..1000 字）**同步返回快速参考**（内嵌帮助索引命中，零 LLM；无命中如实 `none`），
> 后台精算 goroutine（有界并发 4 / 超时 60s）→ `GET /ai/async/answers/:id` 轮询；任务表 TTL 30m / 容量 200 / 租户+用户双隔离
> （越权/未知/过期统一 404）；精算成功经通知中心投递（`type=ai_async_answer`，best-effort，投递失败不阻断任务结果）；
> 未装配 Provider → 任务立即 `degraded`（`generator_unavailable`，仅快速参考，**不假装精算**）；失败/超时如实记 reason。
> 前端 `src/lib/ai/async/**` 任务表（未读语义：首次收口未读、已读后保持、移除为终态不复活）+ Dock 回执卡
> （running 先给快速参考、done 展示精算回答、degraded/failed 如实说明；面板打开即已读）+ 输入坞「后台精算」入口
> （只发起只读任务，**绝不发送对话/提交写动作**）+ FAB 状态点（运行中呼吸点；未读回执并入徽标，§3.2）。
> 埋点复用既有 `task_started/completed/failed`（**事件字典零变更**）；路由 ai:read，`authz-gen` 已再生。
> 门禁：后端 handlers/ai + router + middleware 全绿；前端异步面 2 suites/12 tests + AI 表面回归 127 suites/1655 tests + tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/V2-02-async-task-experience-evidence.md`。
> 条件项：精算为服务端进程内任务（重启丢失，持久队列归专项）；精算产物暂以回执卡独立展示（回填会话线程归 chat-core 改造）；
> 真实 Provider/真库 E2E（含通知中心落库与推送）归联调；回执卡/入口文案未 i18n、未入 axe 扫描面；通知点击落点暂为 `/ai/chat`。

### P0 · Dock 底座 + 帮助骨架（约 2 周）
| 任务 | 内容 | 验收 |
|---|---|---|
| D0-01 | chat-core 抽离（会话/SSE/工具事件/审批） | `/ai/chat` 既有 117 例回归全绿 |
| D0-02 | AssistantDock 容器 + store + 快捷键 + 移动端 | 168 路由可开合；无 `ai:read` 不渲染 |
| D0-03 | 页面注册表 + `usePageAssistantContext` + 与 `ROUTE_PATHS` 一致性单测 | 注册项全部命中路由表；Top 20 页面可用 |
| D0-04 | `pageContext` 请求扩展（前后端） | 缺省请求体快照与现状逐字节一致 |
| D0-05 | 静态建议 + 上下文条 + 埋点 | 建议点击率可观测 |
| H0-01 | `docs/help/**` 目录与 frontmatter 规范 + 10 篇种子文档 | mkdocs 构建通过 |
| H0-02 | Page Manifest 生成 + Doc Ledger + 覆盖率脚本 | CI 产出报告；ratchet 生效 |
| H0-03 | Help Agent 只读工具（search/get_page/read_doc/list_gaps） | 面板帮助视图可问答当前页 |
| H0-04 | 面板帮助视图："缺失提示 + 生成初稿"（KB 草稿通道） | 无文档页面一键产出草稿 |

> 进度：**D0-05 子集已交付**（2026-10-18，`unit_verified`）——聊天埋点映射器 + AIChat 接线
> （`message_sent`/`first_token`/`tool_call`/`stream_end`/`error_shown` + 确认单三事件；
> 隐私：只报长度与元数据，错误详情由 SDK 哈希）；21 例 lib 单测 + 58 例 AI 组件回归 + tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/D0-05-chat-telemetry-wiring-evidence.md`。
> 说明：D0-05 的"静态建议卡 + 上下文条"仍待办；本子集先满足 V 系列"基线采集"前置（§7 V 系列注）。
>
> 进度：**D0-01 已交付**（2026-10-18，`unit_verified`）——chat-core 抽离（锚点校验的逐字搬运，
> AIChat.tsx 1563 → 589 行）：`chat-core/use-ai-chat-session.ts`（会话/SSE/工具事件/确认闭环/
> Provider 与 Bot 选择 + 埋点）、`chat-core/ChatMessageItem.tsx`、constants/types/chat-css；
> 组件套件 58 → 62 例全绿（+4 内核冒烟）、lib/ai 32 例、源码契约 9 例、tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/D0-01-chat-core-extraction-evidence.md`。
> 条件项：Dock 容器消费内核归 D0-02；侧栏抽离按需（Dock 不展示历史栏）。
>
> 进度：**D0-02 已交付**（2026-10-18，`unit_verified`）——常驻容器（`components/ai/dock/`）：
> FAB ↔ 右侧面板（移动端全宽）、`Ctrl/Cmd+I` 切换、`ESC` 收起、无 `ai:read` 不渲染、
> **面板打开才挂载聊天视图（懒挂载，未展开零请求）**、zustand persist（open/width/lastRoute）；
> 挂载于 MainLayout；dock 套件 6 例 + 组件套件 68 例 + tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/D0-02-assistant-dock-evidence.md`。
> 条件项：页面上下文/智能提示归 D0-03/D0-04；拖拽调宽与边缘收纳、移动端动效待后续批次。
>
> 进度：**D0-03 已交付**（2026-10-18，`unit_verified`）——页面注册表（21 页，覆盖 Top 20）+
> `usePageAssistantContext` 路由联动（写入 `store.lastRoute`，容器 `data-page-key` 即时反映）；
> 契约门禁：注册项命中 ROUTE_PATHS、pageKey/动作唯一、entrypoint 白名单、**零输入任务卡全只读（D4）**；
> page-context 10 例 + dock 6 例 + AI 表面 15 suites/110 tests + tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/D0-03-page-context-registry-evidence.md`。
> 条件项：建议卡渲染归 D0-05；`pageContext` 协议回传归 D0-04。
>
> 进度：**D0-04 已交付**（2026-10-18，`unit_verified`）——pageContext 提示层协议：
> 前端 `pageContext?: {pageKey, route}`（缺省不落字段，**字节一致**门禁保持）+ 会话内核接线
> （未注册路由不携带）；后端归一化白名单（非法/未知**静默忽略不 400**、不参与授权）、
> handler 日志留痕 + `run_started` 事件条件携带 `pageKey`；
> 前端 ai-api 53 例 + chat-core 6 例、后端 page_context 5 例 + 端点级 hint-only 契约 +
> `handlers/ai` 全包回归、tsc/eslint 全绿；证据 `docs/plan/evidence/assistant-dock/D0-04-page-context-protocol-evidence.md`。
> 条件项：SSE 载荷正向断言（真实 run 工装）、`bot_runs.page_key` 持久化、提示注入 LLM（归 D1）。

> 进度：**D0-05 完整交付**（2026-10-18，`unit_verified`）——页面级静态建议卡 + 上下文条：
> 新增 `chat-core/SuggestionCards`（注册表驱动，任务卡/问题卡；未注册路由回退通用建议；
> `suggestion_shown/clicked` 埋点，满足"建议点击率可观测"）；工作区与 Dock 面板共用（compact 形态）；
> Dock 头部新增页面上下文条（`assistant-context-chip`）；
> dock+chat-core 13 例、AI 表面 15 suites/113 tests、prefill 契约 9 例、tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/D0-05-suggestions-context-bar-evidence.md`。
> 闭环（D1-02，2026-10-18）：`suggestion_ignored` 采集、富上下文条（target/summary）。
> 条件项：建议文案 i18n 归文案批次。

> 进度：**H0-01 + H0-02 已交付**（2026-10-18，`unit_verified`）——帮助文档骨架：
> `docs/help/**`（frontmatter 规范 + 17 篇种子文档，覆盖全部 21 个页面键；规避 mkdocs `cmdb/` 排除冲突）；
> Page Manifest + Doc Ledger 生成器（`itsm-frontend/scripts/gen-page-manifest.mjs`）与 ratchet 门禁
> （`_coverage-baseline.json`；实测 missing 0→1 触发 FAIL、恢复 PASS）+ CI `docs-ledger.yml`；
> mkdocs 构建通过且 help 零警告、docs-gate 对 docs/help 零断链、ledger coverage=100%；
> 证据 `docs/plan/evidence/help-agent/H0-01-help-docs-seed-evidence.md`、
> `docs/plan/evidence/help-agent/H0-02-page-manifest-doc-ledger-evidence.md`。
> 条件项：git 元数据 freshness（H1-03）、Help Agent 工具（H0-03）与面板帮助视图（H0-04）待后续。

> 进度：**H0-03 已交付**（2026-10-18，`unit_verified`）——Help Agent 只读数据面与工具：
> 生成器追加后端内嵌副本（`internal/helpindex/*.json`，go:embed）；新包 `internal/helpindex`
> （检索/页面状态/gaps/覆盖率，CJK 2-gram 回退且 ASCII 不回退防误命中）；
> REST `GET /ai/help/search`、`/ai/help/page`（未知 pageKey → status=unknown 降级不 404）；
> 4 个只读工具 `help_search/help_get_page/help_read_doc/help_list_gaps`（内置工具 17 → 21，
> 风险矩阵守卫同步；S3 场景授权）；定向用例全绿（helpindex 6 例、help 端点 5 例、工具 2 例、
> service/bot 与 handlers/ai 全包回归、RBAC 新鲜度）；
> 证据 `docs/plan/evidence/help-agent/H0-03-help-agent-tools-evidence.md`。
> 观察（与本批无关）：`go test ./service/` 全量时 `TestBiz_*` 有既有 SQLite 测试隔离冲突，已记录建议单独立项。
> 条件项：面板帮助视图（H0-04）待后续；对话内真实 provider E2E 归联调。

> 进度：**H0-04 已交付**（2026-10-18，`unit_verified`）——面板帮助视图与草稿通道（**P0 收口**）：
> 面板「对话 / 帮助」页签；帮助视图消费 `/ai/help/page` + `/ai/help/search`（状态/覆盖率/正文/检索）；
> 缺失或过时 → 「让 AI 生成初稿」（切回对话并发送结构化 prompt）；
> 后端新增 `help_create_draft`（plan：只落 bot_artifacts 草稿产物、零业务写入、不发布；内置工具 21 → 22）；
> dock 13 例、AI 表面 16 suites/119 tests、后端定向+service/bot+handlers/ai 全绿、tsc/eslint 全绿；
> 证据 `docs/plan/evidence/help-agent/H0-04-panel-help-view-evidence.md`。
> **P0 全部交付**（D0-01～05、H0-01～04）；条件项：KB 实体草稿/审阅发布与 PR 通道归 H1、帮助视图 i18n 同批。

### P1 · 场景化 + 帮助闭环（约 4 周）
| 任务 | 内容 |
|---|---|
| D1-01 | `/ai/assistant/context`：按可见 Bot/授权过滤的建议与快捷动作 |
| D1-02 | 动作桥 + 工单/事件/变更/CI 详情页 context provider（"创建/搜索/分析"建议落地） |
| H1-01 | 遥测缺口聚类 → 文档待办队列 |
| H1-02 | 校验器 + CI ratchet 门禁；PR 通道接入 `itsm-agent` |
| H1-03 | 夜间批任务：代码变更 → stale 标记 → 更新提议 |
| H1-04 | 帮助覆盖率看板（管理页） |

> 进度：**D1-01 已交付**（2026-10-18，`integration_verified`）——`/ai/assistant/context` 服务端建议过滤：
> 前端注册表单一真源 → 生成器产出 `itsm-backend/internal/assistantcontext/assistant-registry.json`（go:embed，
> `--check` 新增漂移门禁）；服务端按「未知页面降级 / 场景 Bot 可见性 fail-closed（缺失·draft·audience·entrypoint）/
> 建议声明 tools 的 授权∩RBAC∩风险 门禁（复用 bot.Decide）/ 策略未装配兼容态」过滤；
> 面板与工作区建议卡**静态基线先行、服务端结果替换**（失败回退 + `context_loaded/context_degraded` 埋点 + `extra.feed`）；
> 后端 7 例契约测试 + internal/assistantcontext 与 service/bot 用例全绿、authz-gen 与路由预检新鲜度通过、
> 前端 AI 表面 17 suites/124 tests 全绿、tsc/eslint 全绿；证据 `docs/plan/evidence/assistant-dock/D1-01-assistant-context-evidence.md`。
> 条件项：route 仅回显（模式匹配仍在注册表）；场景无关页面的 tools 声明待 D1-02 生效；浏览器 E2E 归联调。

> 进度：**D1-02 已交付**（2026-10-18，`integration_verified`）——动作桥 + 详情页 L2 上下文：
> 动作桥 `assistant-actions.ts`（handler → route 跳转 → 提示词回退；写动作不入桥）+ 注册 hook；
> L2 运行时数据 store（非持久化、显式注册、owner 语义）接入工单/事件/变更/CI 四类详情页；
> 会话内核按「launcher 优先，其次页面 L2 派生」发送 entrypoint/targetType/targetId/summary（服务端对象预检 fail-closed）；
> Dock 上下文条展示具体对象（用户可见）；后端新增 `TargetTypeChange` 目标预检（本租户存在 + change:read）；
> 前端 19 suites/138 tests、tsc/eslint 全绿；后端 handlers/ai + service/bot 全绿（含 change 隔离用例）。
> 证据 `docs/plan/evidence/assistant-dock/D1-02-actions-and-page-context-evidence.md`。
> 补全（2026-10-18）：**建议卡会话级忽略结算**——展示台账（cardId/kind/rank/feed）+ 点击豁免 +
> 卡消失即结算 + 卸载延迟一拍结算（StrictMode 双挂载不误报）；`suggestion_ignored` 采集缺口闭环；
> chat-core 13 例（新增 4 例）、AI 表面全绿、tsc/eslint 全绿。
> 条件项：页面级 handler 首个使用点归 I1-01 Inline Apply；列表页 L2 筛选数据待后续；浏览器 E2E 归联调。

> 进度：**H1-01 已交付**（2026-10-18，`integration_verified`）——遥测缺口聚类 → 文档待办队列：
> 帮助面板 missing/stale 上报 `help_gap`（coverageStatus/route）；后端新增 `GET /ai/help/todos`
> （账本缺口 ∪ `help_gap`/`doc_draft_created` 按 pageKey 聚类；优先级 missing>stale>unknown>用户缺口；
> 埋点未装配降级为账本缺口，200 + telemetry.available=false）；默认 30 天/50 条、上限 90 天/200 条；
> 前端 19 suites/140 tests、tsc/eslint 全绿；后端 service 定向 + handlers/ai 全绿；authz-gen 与预检新鲜度通过。
> 证据 `docs/plan/evidence/help-agent/H1-01-doc-todo-queue-evidence.md`。
> 条件项：浏览器 E2E 归联调。
> 闭环：待办认领/忽略持久化与看板消费已于 2026-10-18「待办状态持久化」批次落地（见下方进度块）。
> 闭环：`doc_draft_created` 前端上报点已于 2026-10-18「文档起草采纳率」批次落地（会话页两入口 + 新建页成文信号），见下方进度块。

> 进度：**H1-02 已交付**（2026-10-18，`unit_verified`）——校验器 + CI 门禁 + PR 通道：
> `itsm-frontend/scripts/validate-help-docs.mjs`（frontmatter/结构/page_keys 事实引用/链接/敏感信息六类校验，
> 正向 17 篇 0 错误、负向样例 6 类错误全部命中并 exit 1）；`docs:validate` 接入 `docs-ledger.yml`
> （含 mkdocs 构建健全性步骤与触发路径）；`itsm-agent/prompts/docs-maintenance.md` PR 通道模板
> （grounding/强制校验命令/分级人工/PR 规则）。证据 `docs/plan/evidence/help-agent/H1-02-docs-validator-ci-evidence.md`。
> 条件项：Actions 与 agent 运行时执行归平台侧（等价命令已本地逐条验证）；mkdocs 非 strict（其他域既有告警）。

> 进度：**H1-03 已交付**（2026-10-18，`integration_verified`）——夜间批任务（stale 检测 → 更新提议）：
> 共享工具 `scripts/lib/help-docs.mjs` + 纯函数核心 `doc-stale-core.mjs`（字段级 diff / 三类动作 / 优先级）+
> CLI `detect-doc-stale.mjs`（--base / --base-manifest / --head-manifest / --out / --fail；JSON+MD+Step Summary）；
> 9 例 node:test（5 纯函数 + 2 端到端 + 冒烟）全过；夜批工作流 `docs-stale-nightly.yml`
>（cron 次日 02:00 CST → 测试 → 检测 → artifact → 「[docs] 夜间文档更新提议」Issue 幂等刷新）YAML 校验通过；
> PR 通道 prompt 增加 `{{stale_proposal_json}}` 输入；产物 JSON/MD 已 gitignore。
> 证据 `docs/plan/evidence/help-agent/H1-03-nightly-stale-detection-evidence.md`。
> 条件项：Actions 调度/Issue 写入归平台侧；源码文件级映射（超出清单信号）待后续。
> 闭环：队列侧待办认领/忽略持久化已落地（H1-04 批，见下方进度块）；夜批提议的 Issue 侧认领仍归平台侧。

> 进度：**H1-04 已交付**（2026-10-18，`integration_verified`）——帮助覆盖率看板（管理页 `/ai/help-coverage`）：
> 后端 `GET /ai/help/coverage`（账本覆盖率/总计/页面清单/orphan）+ helpindex `Pages()`/`OrphanDocs()` 访问器；
> 看板消费覆盖率统计、页面清单（状态筛选/搜索）、H1-01 待办队列（缺口/出稿/最近）；
> 降级：待办失败与 `telemetry.available=false` 均提示且账本照常渲染；菜单迁移 `20261018_add_ai_help_coverage_menu.sql`；
> 前端看板 6 例 + observability 回归 + routes 30 例 + ai-api 53 例全绿、tsc/eslint 全绿；
> 后端 helpindex/handlers/ai 全绿、authz-gen 与路由预检新鲜度通过。
> 证据 `docs/plan/evidence/help-agent/H1-04-help-coverage-dashboard-evidence.md`。
> 条件项：菜单真实 PG 冒烟归联调（沿用 O0-03 已验模式）；页面 i18n 同批。
> 闭环：待办认领/忽略持久化与看板消费已于 2026-10-18「待办状态持久化」批次落地（见下方进度块）。

> 进度：**待办认领/忽略状态持久化与看板消费（H1-01/H1-03/H1-04 条件项闭环）已交付**（2026-10-18，`integration_verified`）：
> 数据面：`migrations/20261018n_ai_doc_todo_states.sql`（ai_doc_todo_states：租户×pageKey 唯一、
> state ∈ {claimed, ignored}、updated_by/updated_at；「恢复待办」= 删行回缺省 open，不存空语义）
> + RLS `database/rls/migrations/017_ai_doc_todo_states_policies.sql`（tenant 隔离 fail-closed，附回滚）；
> 服务 `service/help_doc_todo_states.go`（归一化/校验/ListStates/SetState 幂等 UPSERT|DELETE）；
> 端点：`GET /ai/help/todos` 合并状态（items[].state/stateUpdatedBy/stateUpdatedAt + totals.claimed/ignored +
> states.available；未装配 → 200 + available=false 按 open 渲染，不 5xx）与
> `PUT /ai/help/todos/state`（ai:write；claimed|ignored|open；非法输入 400、未装配 503；authz-gen 再生）；
> bootstrap 注入同库实例（未注入零行为变化）；
> 看板消费：`/ai/help-coverage` 待办表新增状态列（待处理/已认领/已忽略）+ 行内操作（认领/忽略/恢复待办）+
> 状态筛选（全部/待处理/已认领/已忽略）+ 写失败错误提示（不乐观改写状态）；状态服务降级 → 提示且按钮禁用；
> 门禁：后端 service 定向（归一化/校验/扫描/幂等写/非法输入零库访问）与全包（service 56.3s / handlers/ai 8.0s）、
> router/middleware/migration 全绿、authz-gen 与预检新鲜度通过；
> 前端 ai-api 64 tests（+2）、看板 10 tests（+4）、AI 表面 129 suites/1682 tests、页面域 6 suites/44 tests、
> tsc/eslint 全绿。
> 边界：真实 PG 冒烟（RLS 017 应用与 upsert 冲突路径）归联调；页面 i18n 沿用既有条件项；
> 夜批 stale 提议的 Issue 侧认领归平台侧，本表提供队列侧认领/忽略的通用持久化。
> 证据 `docs/plan/evidence/help-agent/H1-04-todo-state-persistence-evidence.md`。

> 进度：**V0-01 已交付**（2026-10-18，`integration_verified`）——工单详情三件套（**Inline 优先**）：
> 分诊建议复用既有 `AISuggestionPanel`（采纳写回 + 审计）；新增答复草稿
> （`POST /ai/tickets/:id/reply-draft`：只读生成 + KB Top3 grounding + 审计；`ai:read`+`ticket:read`）与
> 知识推荐（复用 `/ai/rag/search(kb)`）；`TicketAIInlineAssistant` 面板支持编辑/复制/**填入回复框**（CommentPanel
> `externalDraft`，**只填不提交**）与引用到草稿；
> 基线采集接线（O0-01 同一 SDK）：`inline_preview`/`inline_applied(fill·copy·quote)`/`task_started·completed(durationMs)`/`task_failed`；
> 后端 reply-draft 4 例 + handlers/ai 全包、authz-gen 与路由预检通过；前端 21 suites/198 tests、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/V0-01-ticket-inline-trio-evidence.md`。
> 条件项：≥30% 对照门槛需生产 1 周基线（事件与维度已就绪，自动报告归 O2-01）；真实 Provider E2E 归联调；
> 既有测试问题登记：`TicketDetailAssignSearch`（auth-store mock 缺 getState）经对照实验确认非本批回归。

> 进度：**I0-02 已交付**（2026-10-18，`integration_verified`）——任务卡模型（`tasks[]` 分组）：
> 注册表 21 页拆分为 `tasks[]`/`questions[]`（每页 ≥1 任务卡；任务 1–2、问题 0–2、总数 ≤3；D4 全只读），
> 6 个仅问题卡的页面补齐零输入任务卡；`flattenPageActions()` 统一扁平（任务在前）供 D1-01 服务端注册表与建议区消费；
> `SuggestionCards` 按任务卡/问题卡两组渲染（组头 + 全局 rank 保持）；生成器与内嵌注册表同源再生、漂移门禁通过；
> 前端 19 suites/142 tests、tsc/eslint 全绿；后端 assistantcontext + handlers/ai 全绿。
> 证据 `docs/plan/evidence/assistant-dock/I0-02-task-card-model-evidence.md`。
> 条件项：新增文案 i18n 同批；6 页定义变化会触发 H1-03 夜批 review_update（预期）；分组视觉走查归 I 系列审查。

> 进度：**V0-03 已交付**（2026-10-18，`integration_verified`）——变更影响分析任务卡（plan 产物，Inline 优先）：
> `aiExecuteTool()`（`POST /agent/tools/execute`）+ `ChangeAIInlinePlanCard`（受影响 CI 一键选择 / 手动输入 →
> 直达执行 `analyze_ci_impact_plan` → 产物（目标/检查清单/artifactId）面板内预览 → 采纳复制）；
> 变更详情页注册 D1-02 动作桥 handler：点击「生成影响分析计划」建议卡**页内直达**（不再发送提示词）；
> plan 工具只读不进审批，产物归属调用者（HTTP e2e 断言 tenant/owner/kind/checklist）；
> 度量：task_completed.durationMs（分析耗时）、inline_preview/inline_applied(adopt)（计划采纳率）、task_failed/error_shown；
> 后端 handlers/ai 全包全绿；前端面板 5 例 + ai-api 2 例 + AI 套件 20 suites/147 tests、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/V0-03-change-impact-plan-card-evidence.md`。
> 条件项：采纳回写变更字段归 I1 Inline Apply；dryRun 预览仅写工具需要；浏览器 E2E 归联调。

> 进度：**V0-02 已交付**（2026-10-18，`integration_verified`）——一句话建单 + 自助问答（员工侧）：
> 事件字典 +4（`self_service_answered/resolved/ticket_created`、`ai_create_field_corrected`；前端 SDK + 后端白名单 + §13.2 同步）；
> `AICreateFlowTracker`（建单时长=generate→提交成功，`latencyMs` 同口径入时延桶；误分类率代理=字段纠正率；任务漏斗）；
> `SelfServiceAssistant`（知识库 Top3 + 匹配度 → 已解决/转建单；未命中直接建单 CTA；不做写操作）；
> `A2UIFormRenderer` 受控 intent + 埋点接线；`/tickets/ai-create` 组合「先自助 → 未解决带入意图（不自动生成/提交）」；
> `/ai/observability` 自助服务卡（自助解决率/转建单/字段纠正 + 口径文案）；
> 前端 53 suites/463 tests、tsc/eslint 全绿；后端 5 包全绿。
> 证据 `docs/plan/evidence/assistant-dock/V0-02-self-service-ai-create-evidence.md`。
> 条件项：误分类率真值需标注；命中率运营报表归 O2-01；移动端入口与真实 RAG E2E 归联调。

> 进度：**V0-04 已交付**（2026-10-18，`integration_verified`）——试点对照报告 + 放量门槛（§11.5）：
> 后端 `service/ai_pilot_report.go`（本期 vs 基线双窗口；单条 PG FILTER 聚合；三段试点对照 + 全局负向指标 +
> ≥2 项达标且负向不恶化（+10% 容差）门槛判定）+ 端点 `GET /ai/observability/pilot-report`（ai:read）+ authz-gen 再生；
> 前端 `/ai/observability`「试点对照报告」卡（门槛标签/三段标签/指标对照表/负向表/阈值与结论/无样本说明）+
> Markdown 报告导出（复用 I2-02 下载工具，`handoff(source=pilot_report_export)`）；
> 后端 5 包全绿；前端 54 suites/467 tests + 看板 16 例、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-observability/V0-04-pilot-report-gate-evidence.md`。
> 条件项：真实 PG 冒烟、§13.6 分群对比与建议排序权重归 O2-01；窗口按试点起止日对齐归运营迭代。

> 进度：**I0-03 已交付**（2026-10-18，`integration_verified`）——快捷键与命令面板整合：
> Ctrl+K 真正接通（layout-store 请求通道 → Header 打开搜索；enableOnFormTags 使助手面板输入框内仍可达）；
> 全局搜索新增「问 AI」区（关键词非空常驻首项；点击或 Ctrl+Enter → 预填并打开助手面板 + handoff 埋点；
> Enter 仍为搜索，行为不冲突）；assistant-store 新增 pendingPrompt/consumePendingPrompt（不持久化，
> partialize 仅 UI 偏好）；面板消费预填（只填不发送）并聚焦输入框；
> 前端 25 suites/196 tests、tsc/eslint 全绿；证据 `docs/plan/evidence/assistant-dock/I0-03-shortcuts-and-command-palette-evidence.md`。
> 条件项：划词问 AI（I1-02）复用同一预填通道；搜索内深度问答维持现状；真实键盘 E2E 归联调。
> 既有测试问题登记（非本批回归）：TicketDetailAssignSearch（auth-store mock 缺 getState）、tickets/types 页权限门控断言（文件未改动）。

> 进度：**I1-01 已交付**（2026-10-18，`integration_verified`）——Inline Apply（diff + undo）：
> `use-inline-apply` 状态机（预览→确认→应用→撤销；**未确认不调用 onApply**；失败保留待确认）+ `InlineApplyDiff`
> 弹窗（before→after/空值占位/「不自动提交」提示）；工单**详情**从直接写库改为 diff 预览→确认写回→撤销写回旧值；
> 工单**创建**从自动回填改为 diff 预览→确认仅写表单（不提交）→撤销恢复原值；
> 埋点 inline_preview/inline_applied/inline_undone（scope+fields，可聚合采纳/撤销率）；AI 审计沿用既有链路。
> 前端 21 suites/151 tests（hook 5 例 + 弹窗 3 例）、tsc/eslint 全绿；
> 证据 `docs/plan/evidence/assistant-dock/I1-01-inline-apply-evidence.md`。
> 条件项：页面级完整交互（点击→diff→确认→撤销）归联调 E2E；创建页字段草案接入复用同一状态机，归后续。

> 进度：**I1-02 已交付**（2026-10-18，`integration_verified`）——划词问 AI + 错误解释入口：
> `SelectionAskAI`（选区 2–500 字符 → 悬浮「问 AI」→ 预填并打开面板；忽略输入框/助手面板内选区；
> mousedown/滚动/ESC 收起）挂载 MainLayout；`ExplainErrorButton`（Error/Response 形状/字符串归一 + ≤500 截断）
> 接入 `ErrorBoundary` 与 `RouteError`（路由 404/500/异常）；埋点 handoff(from=selection|error_explain)；
> 复用 I0-03 预填通道（只填不发送）；前端 27 suites/224 tests（新增 8 例）、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/I1-02-selection-and-error-explain-evidence.md`。
> 条件项：移动端长按形态与浏览器选区坐标 E2E 归联调；更多错误面可按需复用组件接线。

> 进度：**I0-04 已交付**（2026-10-18，`integration_verified`）——回执与结果落点（Outcome Contract）：
> 后端事件契约扩展 `ToolStreamEvent.artifactId`（产物类工具 done 事件回带；可选字段，旧客户端忽略）+
> `toolResultArtifactID` 兼容多数值形态；前端 `deriveAssistantOutcomes`（artifact/draft/navigation，去重+上限 4）+
> `OutcomeReceipt` 回执卡（引用可复制、可跳转，`inline_applied(open|copy)` 埋点）接入 ChatMessageItem，
> 并由 `openOutcome` 注入 SPA 导航（工作区与面板双入口）；
> 草稿落点直达 `/tickets/create`、`/knowledge/articles/new`、`/ai/help-coverage`；写路径 pending → `/ai/approval`；
> 后端 handlers/ai 全包 + 前端 26 suites/170 tests、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/I0-04-outcome-contract-evidence.md`。
> 条件项：entity 落点（业务对象 id 回带）与产物查看页深链归后续；浏览器 E2E 归联调。

> 进度：**I1-03 已交付**（2026-10-18，`integration_verified`）——主动协助（§3.6 不打扰原则）：
> ① FAB 徽标 = 待审批（`/agent/tools/invocations?state=pending`）+ 高价值未读通知（`sla_*/approval_required/ai_*/bot_*` 或深链 `/ai/**`）求和上球；
> ② 页面内联建议条 = 当前页 L1 首个任务卡，`silent` 默认开启 → 不出现；关闭静默后出现，可「关闭」（会话）与「不再提示」（按 scopeKey 持久化记忆），
>    「让 AI 看」只预填 + 开面板（不自动发送）；③ 通知订阅复用现有通知系统（WS 单例只读订阅 + 未读列表；订阅/退订入口 → `/profile` 通知偏好）。
> 新增事件 `proactive_shown/clicked/dismissed`（§13.2 已同步）；总开关关闭 → 无徽标/无条/**零请求**（负向断言）。
> 前端 32 suites / 230 tests（新增 12 例）、tsc/eslint 全绿；证据 `docs/plan/evidence/assistant-dock/I1-03-proactive-assist-evidence.md`。
> 条件项：待审批计数未按审批人过滤（后端参数扩展候选）；浏览器/移动端视觉 E2E 归联调。

> 进度：**I2-01 已交付**（2026-10-18，`integration_verified`）——Autopilot 监督面（暂停/回滚/审批联动）：
> 后端 `RunStore.CancelRun`（仅 running、幂等）+ `TraceService.ListRuns`（运行列表 + 步骤/工具/待审批/产物计数）+
> `RollbackAutopilotRun`（终止运行 + 冻结该运行全部待审批为 `cancelled`（永不执行）+ 原因回填会话 +
> 通知中心投递 `type=ai_autopilot` 深链 `/ai/approval`）；端点 `GET /ai/runs`、`POST /ai/runs/:id/cancel|rollback`
> 与 `GET /agent/tools/invocations?runId=` 审批联动 + authz-gen 预检再生；
> 前端 `/ai/approval`「Autopilot 运行监督」卡（状态过滤/暂停/回滚/轨迹深链/通知中心入口/按运行查看待审批 + 过滤 chip）；
> 后端 5 包全绿；前端 37 suites/302 tests + 审批页 11 例、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/I2-01-autopilot-supervision-evidence.md`。
> 条件项：调度触发面与流级中断归后续（保持"不做无感自动写"边界）；附带修复既有 b1-09 权限 mock 失效（非本批回归，已登记）。

> 进度：**I2-02 已交付**（2026-10-18，`integration_verified`）——合规与无障碍专项：
> 会话导出（`GET /ai/conversations/:id/export`（ai:read；Markdown 快照，结构化回答抽取可读文本；跨租户=404）+
> 前端 `exportConversation` + Blob 下载工具（受限环境静默降级）+ 工作区会话列表「导出会话」入口；删除沿用既有 ai:write + Popconfirm）+
> axe 扫描（引入 jest-axe/axe-core；面板/诊断抽屉（含轨迹视图）/内联建议条三界面 **0 violations**；仅禁用 jsdom 不可靠的 color-contrast）+
> authz-gen 预检再生；后端 5 包全绿；前端 51 suites/505 tests、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/I2-02-compliance-a11y-evidence.md`。
> - **I2-02 扫描面扩展**（2026-10-18，`integration_verified`）：axe 扫描对象 3 → 6，新增
> `AssistantMemoryChips`（V2-01 记忆条）、`AssistantAsyncAnswerCard`（V2-02 异步回执卡）与
> `IntentUnderstandingCard`（V1-03 确认卡）三例（仅禁 jsdom 不可靠的 color-contrast，其余规则全开）；
> 前两例随 O2-02 工作区入口复用交付，确认卡闭环 V1-03 条件项。
> 证据 `docs/plan/evidence/assistant-dock/O2-02-workspace-entry-a11y-evidence.md`。
> 条件项：更多界面纳入 axe 批量与真实浏览器走查归后续。

### P2 · 智能化与运营
- LLM 动态建议（缓存 + 审计）；L1 自动合并策略；文档命中率/解决率运营报表；MSP 多租户帮助空间与分发授权。

> 进度：**浏览器级运行时走查 + 写路径审批闭环缺陷修复已交付**（2026-10-18，`integration_verified`，238 demo）：
> 在 `1.8.0-dev.d51ed172` 上用 tabbit + Playwright 完成最终用户视角走查：Dock FAB/热键/ESC/建议卡/上下文条（D0-02/D0-03/D0-05）、
> 真实 LLM 流式对话与「停止生成」（tbB）、工具调用 `list_tickets` 实调（tbC）、帮助视图检索/未注册页回退/反馈/异步精算（tbD/tbDF/H0-04/V2-02）、
> `/ai/chat` Bot 选择器与会话持久化（tbE/D1-01）、详情页「问 AI」→ scope chip（tbE2/D1-02）均通过。
> **发现并修复 2 处写路径缺陷**：
> ① `create_ticket.ArgsSchema` 仅声明 `ci_id`，执行层却读取 `title/description/priority/type` 等 →
>    LLM 无从得知 `title` 必填，审批通过后必然 `create_ticket: title is required`；已补齐 schema 与 `required:["title"]`；
> ② `ToolApprovalCard` 将内联 `onLoaded` 闭包纳入 `useCallback` 依赖，父组件 `setState` → 新闭包 → 无限重拉
>    （实测同秒 20+ 次 `GET /agent/tools/:id` → 429），且 `catch` 一律显示「已过期/不可用」；
>    已改为 `useRef` 持有回调 + 新增 `unavailable` 态（仅 404 归为过期），瞬时失败可重试。
> 修复后端到端复验（`1.8.0-dev.2926ddc8`）：`#34 approval_state=approved / status=done`，工单 `TKT-202610-000065` 真实落库，
> 卡片收敛「已处理」，卡片挂载到批准前仅 1 次详情请求；对照修复前 `#33 status=failed`。
> 门禁：tool-approval-card 11 例 + AI 表面 48 suites/316 tests + tsc/eslint、`go vet` + service 定向用例全绿；
> 双镜像构建 `BUILD_EXIT=0`、三容器 healthy、版本对齐、A/B 指纹 0→1。
> 证据 `docs/plan/evidence/assistant-dock/approval-write-path-runtime-verification-evidence.md`。
> 追加（`1.8.0-dev.abd8705a`）：回归走查又发现**帮助面板加载中谎报「未注册页面」**（已注册页面在
> `/ai/help/page` 返回前因 `data=null` 落入 unknown）——已改为加载中「查询中」，仅 pageKey 缺失时显示未注册；
> AssistantHelpPanel 15 例（新增首帧断言）+ AI 表面 48 suites/322 tests + tsc/eslint 全绿；
> 浏览器专项（延迟请求 4s）实测 `查询中`→`已覆盖`、`unknownDuringLoad=0`；Dock/审批页/工作区回归 0 console errors。
> 条件项：本机 Windows 全包 `go build`/`go test ./service/` 受 MinGW 链接器内存限制（非代码问题），已用远端镜像构建替代；
> 验证产生的 `TKT-202610-000065` 为测试数据，可清理。

> **P2 全部交付**（4/4，2026-10-18）：LLM 动态建议、L1 自动合并策略、文档命中率/解决率运营报表、MSP 多租户帮助空间与分发授权。
> 证据：`docs/plan/evidence/assistant-dock/P2-dynamic-suggestions-evidence.md`、`docs/plan/evidence/help-agent/P2-l1-auto-merge-evidence.md`、
> `docs/plan/evidence/help-agent/P2-doc-metrics-report-evidence.md`、`docs/plan/evidence/help-agent/P2-msp-help-space-evidence.md`。

> 进度：**P2 · LLM 动态建议（缓存 + 审计）已交付**（2026-10-18，`integration_verified`）：
> `DynamicSuggestionService`（TTL 5m 缓存 + 同键单飞 + 失败不缓存 fail-safe）+ 严格 JSON 提示词生成器
> （经 LLMGateway §3.3 解析链）+ 审计落 `ai_feedbacks(ai_audit, scenario=assistant_dynamic_suggestion)`；
> 解析层强制只读（§D4）与最多 3 条；端点 `GET /ai/assistant/suggestions/dynamic`（ai:read；未装配 503、
> 未知页 known=false、失败 generated=false+reason）+ authz-gen 再生；
> 前端增强层 `useDynamicSuggestions`（会话级缓存/失败静默）+ `SuggestionCards` 动态卡置组首（埋点 `feed=llm`）+
> 降级链「动态 → D1-01 服务端过滤 → 静态注册表基线」；后端 5 包全绿；前端 56 suites/477 tests、tsc/eslint 全绿。
> 证据 `docs/plan/evidence/assistant-dock/P2-dynamic-suggestions-evidence.md`。
> 条件项：真实 LLM 质量 E2E 与显式租户开关归联调/运营；审计信心分为代理口径。

> 进度：**P2 · L1 自动合并策略已交付**（2026-10-18，`integration_verified`）：
> `scripts/lib/doc-merge-core.mjs`（L1 分类/候选选择/frontmatter 补全/EOF 换行/相对链接唯一命中修正/审计）+
> `scripts/apply-doc-fixes.mjs`（默认 dry-run；`DOCS_AUTO_MERGE=1` 才写；**校验全绿前置**；
> 应用后复校验失败**整体回滚**；单批 ≤20 文件；审计 `_automerge-audit.jsonl`；`--report` 报告）+
> npm `docs:merge / docs:merge:apply / docs:merge:test`；夜批 `docs-stale-nightly.yml` 增 L1 dry-run + artifact +
> Issue 提示（默认关，仅观察）；Agent 规则（`prompts/docs-maintenance.md`）禁止 PR 夹带机械批量重排。
> 本地 E2E 实测：校验失败 → fail-closed（不写文件/exit 1/无审计）；校验通过 → 应用 + 复校验 + 审计落盘；
> 并**发现并收窄**「逐行去尾空白会破坏 Markdown 硬换行」的规则风险（现仅 EOF 换行，附负向测试）。
> 验证：`docs:merge:test` 7 pass、`docs:stale:test` 7 pass、`docs:validate` OK(17 篇)、`docs:ledger:check` OK。
> 证据 `docs/plan/evidence/help-agent/P2-l1-auto-merge-evidence.md`。
> 条件项：自动发布与错别字修正（走 L2）归后续；`version` 推导为代理口径。

> 进度：**P2 · 文档命中率/解决率运营报表已交付**（2026-10-18，`integration_verified`）：
> 后端 `service/ai_doc_metrics.go`（会话级命中率 = 1 − 有 help_gap 的会话/有 help_opened 的会话；
> 解决率 = 1 − 帮助会话中升级人工的会话占比；反馈解决率 = doc_feedback(helpful) / 全部反馈；
> 按页 Top20 + 按天缺口收敛趋势；零分母输出 null 不虚报 0%）+ 端点 `GET /ai/observability/doc-metrics`
> （ai:read；`handlers/ai/doc_metrics.go` 合并 helpindex 账本覆盖率；埋点未装配 → 200 + telemetry.available=false）+
> authz-gen 再生；
> 事件面：新增 `doc_feedback`（§13.2 字典同步增补：前端字典 + 后端白名单）并**首次接线 `help_opened`**
> （此前仅存在于字典，报表分母恒 0）——`AssistantHelpPanel` 打开去重上报 + 「已解决/未解决」反馈（每条文档视图一次）；
> 前端消费：`aiGetDocMetrics` + `DocMetricsCard`（挂 `/ai/observability`）+ Markdown 导出（`doc-metrics-markdown.ts`）+
> i18n zh/en；后端 5 包全绿（service 59.3s）；前端 57 suites/486 tests、tsc/eslint 全绿；
> 文档校验 OK(17 篇)/账本 ratchet OK。
> 证据 `docs/plan/evidence/help-agent/P2-doc-metrics-report-evidence.md`。
> 条件项：真实 PG 冒烟（jsonb/date_trunc/子查询）归联调；
> 匿名/关闭模式无会话 id → 会话级比率显示「—」。

> 进度：**文档起草采纳率（H1-01 / P2 条件项闭环）已交付**（2026-10-18，`integration_verified`）——
> 起草链路信号接线 + 运营报表采纳率：
> 信号：会话页两入口（「补充为知识文章」单条回答 / 「保存成文章」整段会话）上报 `doc_draft_created`
> （source=chat_promote_answer|chat_promote_conversation，extra.kind，outcomeType=draft）；
> 新建页仅在 AI 预填驱动且**创建成功**后上报 `doc_draft_published`
> （source=article_new_prefill；普通空白新建不上报，保证分子与起草动作同一漏斗）；
> 事件字典/后端白名单同步增补 `doc_draft_published`（§13.2 行更新；两端字典冻结单测同步）。
> 报表：`service/ai_doc_metrics.go` 三处聚合（总量/按页/按天）新增 `published` 计数与
> `rates.draftPublishRate = published/drafts`（零分母 null 不虚报 0%）；
> 前端 `DocMetricsCard` 新增「起草落地率（草稿→成文）」+「起草成文」统计与按页「成文」列、
> Markdown 导出同步（含「窗口级漏斗，非逐篇归因」口径注记）、i18n zh/en。
> 测试：后端 service 定向（sqlmock 三查询列 + 比率断言）+ handlers/ai 全绿（service 52.9s / handlers 7.4s）；
> 前端 4 套定向 43 tests + `src/lib/ai`+`src/components/ai` 48 suites/307 tests + `src/lib/api` 81 suites/1373 tests
> 全绿（exit 0），tsc --noEmit / eslint 全绿。
> 边界：口径为**窗口级漏斗**（窗口内成文数 / 起草动作数），不构成逐篇归因；草稿在窗口末尾产生、成文落入
> 下一窗口属已知近似（报表内显式注记）；真实 PG 冒烟（jsonb → 计数）仍归联调。
> 证据 `docs/plan/evidence/help-agent/H1-04-doc-draft-adoption-evidence.md`。

> 进度：**P2 · MSP 多租户帮助空间与分发授权已交付**（2026-10-18，`integration_verified`）：
> 数据面 `migrations/20261018m_msp_help_space.sql`（provider 帮助库 `msp_help_docs` + 客户 copy-in 追踪
> `msp_help_distributions`，active 行部分唯一索引）+ RLS `016_msp_help_policies.sql`（两表 tenant 隔离，
> fail-closed，与 003–015 同范式）；
> 服务 `service/msp_help_space.go`：provider 帮助库（draft→publish，仅已发布可分发，编辑已发布回 draft 推进版本）+
> 分发授权（copy-in 快照 + 来源追踪；升级提示"提示不强制"→ 显式 upgrade 原位刷新；unbind 转客户原生副本；
> 授权唯一入口 mspguard + 客户租户 ctx 重绑定，零跨租户读）+ 审计 `msp_help_doc.{distribute,upgrade,unbind}`；
> 端点 9 个（`/msp/help/docs*` = msp read/write；`/msp/customers/:id/help/**` = msp_customer read/write ∩ mspguard）；
> 客户侧消费：`/ai/help/page` 增 `distributedDocs`、`/ai/help/search` 增 `distributedResults`（来源标注 + 失败降级不阻塞）；
> 前端帮助面板「服务商分发」标注 / unbound 标签 / 分发检索分区 / 反馈与 docCount 口径含分发数；
> 后端 7 包全绿（含 bootstrap 装配）；前端 57 suites/489 tests、tsc/eslint 全绿；authz-gen 再生。
> 证据 `docs/plan/evidence/help-agent/P2-msp-help-space-evidence.md`。
> 条件项：真实 PG 冒烟（本机 Docker daemon 未运行）与 provider 管理 UI（对齐 P2-①/D-TT-1 先例：先 API+授权链）归联调/迭代；
> 事件来源维度未分离（help_opened/doc_feedback 暂不区分内嵌/分发）。

### I 系列 · 交互补强（审查新增，并入 P0/P1 节奏）
| 任务 | 内容 | 验收 |
|---|---|---|
| I0-01 | 交互状态机 + 错误字典（§3.2） | 组件按状态机实现；8 类状态组件测试覆盖 |
| I0-02 | 任务卡模型：注册表新增 `tasks[]`，建议区分组（任务卡/问题卡） | Top 20 页面每页 ≥1 个零输入任务卡 |
| I0-03 | 快捷键与命令面板整合（Ctrl+I；Ctrl+K 增"问 AI"区） | 键盘全流程可达；不与现有快捷键冲突 |
| I0-04 | 回执与结果落点（Outcome Contract） | 创建/草稿任务回执可跳转、可验证 |
| I1-01 | Inline Apply：分诊/字段建议一键应用（diff + undo） | 工单创建/详情接入；负向断言"不自动提交" |
| I1-02 | 划词问 AI + 错误解释入口 | 选区→面板预填；错误旁解释可用 |
| I1-03 | 主动协助（FAB 徽标 + 内联建议条 + 通知订阅） | 默认静默开关生效；"不再提示"记忆可用 |
| I2-01 | Autopilot 监督面（暂停/回滚/审批联动） | 与 `/ai/approval`、通知中心联动 |
| I2-02 | 合规与无障碍专项 | 会话导出/删除；axe 扫描通过 |

### O 系列 · 观测性与分析（并入 P0/P1）
| 任务 | 内容 | 验收 |
|---|---|---|
| O0-01 | 前端埋点 SDK + 事件字典（§13.2） | 契约单测；采样/批量/失败不阻塞交互；脱敏断言 |
| O0-02 | 后端事件接收与聚合 `POST /ai/telemetry/events` + 扩展 `/ai/bot-metrics` | 租户隔离；聚合口径与看板一致 |
| O0-03 | 观测看板 MVP：漏斗 + 错误 + 成本/时延（`/ai/observability`） | 四层指标可查；按页面/角色/租户过滤 |
| O1-01 | 单任务全链路轨迹（run_id 回放） | 会话→run→工具→确认→产物→业务对象可回放 |
| O1-02 | 实时健康与告警（Provider/错误率/预算/队列） | 告警进通知中心；SLO 定义 |
| O2-01 | 分析增强：建议排序权重、试点对照报告、分群对比 | §11 报告自动产出；建议采纳率提升可验证 |
| O2-02 | 面板内诊断抽屉（运行轨迹/耗时/token 可见） | 报障可导出轨迹 ID |

> 进度：
> - **O0-01 已交付**（2026-10-18，`unit_verified`）——前端埋点 SDK + 28 项事件字典契约
>   （`itsm-frontend/src/lib/ai/telemetry.ts`，15 例单测 / tsc / eslint 全绿）；
>   证据 `docs/plan/evidence/assistant-observability/O0-01-telemetry-sdk-evidence.md`。
> - **O0-02 已交付**（2026-10-18，`integration_verified`）——后端接入与聚合：
>   `POST /ai/telemetry/events`（白名单/批量上限/载荷护栏）、`GET /ai/telemetry/summary`（口径已冻结）、
>   迁移 `ai_telemetry_events`（RLS 实测：跨租户写 42501 / 跨租户读 0 行 / 同租户放行）；
>   服务+接口 13 例、middleware 新鲜度与对齐、router/migration/bootstrap 回归全绿；
>   证据 `docs/plan/evidence/assistant-observability/O0-02-telemetry-ingest-evidence.md`。
> - **O0-02 条件项闭环**（2026-10-18，`integration_verified`）——埋点保留期清理
>   （§13.1.4 合规 / §13.3 数据策略）：`service/ai_telemetry_housekeeping.go`
>   （跨租户系统任务；保留期默认 90 天与聚合回溯一致；单批 5000 行循环扫空 + 单轮批次硬上限；
>   中途失败返回已删进度；Start 失败仅告警不阻断进程）+ bootstrap 接线
>   （worker/all 模式随 startBackgroundTasks 起停，config `ai_telemetry.*` 可覆盖）+
>   幂等支撑索引迁移 `20261018_ai_telemetry_events_retention.sql`（清理命中 occurred_at 索引范围扫描）；
>   service 10 例 + migration 自发现回归 + service/config/migration/bootstrap 包全绿；
>   证据 `docs/plan/evidence/assistant-observability/O0-02-retention-cleanup-evidence.md`。
> - **O0-03 已交付**（2026-10-18，`integration_verified`）——观测看板 MVP：`/ai/observability`
>   （采用/效果/质量/成本与时延四层 + 关键转化漏斗 + 错误/时延 + days/pageKey/role 过滤）、
>   后端 role 维度与过滤（迁移 `20261018_ai_telemetry_events_filters.sql`，真实 PG 实测过滤口径 3/2/1/1/1）、
>   菜单迁移（真实 PG 冒烟幂等；发现并规避 menus 无 (tenant_id,path) 唯一约束的既有写法问题）；
>   页面 5 例 + 路由 30 例 + tsc/eslint 全绿；证据 `docs/plan/evidence/assistant-observability/O0-03-observability-dashboard-evidence.md`。
>   条件项：业务页面埋点接线归 D0-05/V 系列（AIChat 基础事件已随 D0-05 子集交付）；
>   保留期清理已交付（见 O0-02 条件项闭环）；真实 PG 删除语义回归与全栈 E2E 待补。
> - **O1-01 已交付**（2026-10-18，`integration_verified`）——单任务全链路轨迹（run_id 回放）：
>   后端 `TraceService.GetRunTrace`（租户前置；会话→run→步骤/事件→工具/确认→产物→业务对象一次聚合；
>   原始参数不回显、跨租户=不存在同口径）+ 端点 `GET /ai/runs/:id/trace`（ai:read；503/404 边界）+
>   `authz-gen` 预检再生；前端 `aiGetRunTrace` + `RunTraceView`（链路六段 + 复制轨迹 ID）+
>   `/ai/observability` 轨迹回放卡（`?runId=` 深链）；
>   后端 4 包（handlers/ai、service/bot、router、middleware）全绿；前端 35 suites/293 tests、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-observability/O1-01-run-trace-evidence.md`。
>   - **O1-01 条件项闭环**（2026-10-18，`integration_verified`）——纯问答轨迹（conversation 维度）+
>   审计页行内跳转：后端 `TraceService.GetConversationTrace`（会话档案/消息回合/运行/工具/产物一次聚合；
>   跨租户=不存在同口径；单条 2000 跑数、窗口 200 条显式截断；原始参数不回显；与 run 轨迹共用映射口径）+
>   端点 `GET /ai/conversations/:id/trace`（ai:read；400/404/503 边界；authz-gen 预检再生）；
>   前端 `aiGetConversationTrace` + `ConversationTraceView` + 诊断抽屉纯问答回退（无 ID 才降级）+
>   `/ai/observability?conversationId=` 深链（与 runId 互斥）+ 审计页「轨迹」行内跳转
>   （有 run 优先/无 run 回退会话/两者皆无不渲染）+ a11y 扫描面 6→7；
>   后端 service/bot+handlers/ai/router/service/botintegration 全绿、前端 components/ai+lib/ai 48 suites/305 tests、
>   observability+audit 2 suites/19 tests、tsc/eslint 全绿；
>   证据 `docs/plan/evidence/assistant-observability/O1-01-conversation-trace-evidence.md`。
>   条件项：真实 PG/RLS 冒烟与浏览器 E2E 待环境；会话轨迹不含遥测事件（观测面独立）；运行列表窗口 50。
> - **O1-02 已交付**（2026-10-18，`integration_verified`）——实时健康与告警：
>   `service/ai_health.go` 快照（运行/工具/确认/队列/Provider/质量）+ 纯函数 SLO 评估
>   （任务成功率/工具错误率/首 token P95/错误恢复率/LLM 成功率；样本不足不判定）+ 告警派发
>   （通知中心：活跃管理员→回退活跃用户、同标题 30 分钟去重、`type=ai_health` 深链 `/ai/observability`）；
>   端点 `GET /ai/health`（ai:read）与 `POST /ai/health/alerts/dispatch`（ai:write）+ authz-gen 预检再生；
>   `/ai/observability` 健康卡（SLO 标签 + 派发按钮按 ai:write 门控）；
>   后端 5 包全绿（含去重/租户隔离/回退收件人）、前端 35 suites/293 tests + 看板 10 例、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-observability/O1-02-health-alerts-evidence.md`。
> - **O1-02 条件项闭环**（2026-10-18，`integration_verified`）——定时巡检 sweeper + 订阅偏好映射：
>   `service/ai_health_sweeper.go`（部署级开关 `ai_health.sweeper_enabled` 默认 false＝不默认打扰；
>   单轮上限 200 租户、租户级错误/panic 隔离、启动即跑一轮 + 定时循环）+ 收件人订阅过滤
>   （`notification_preferences` 的 `ai_health_alert`：显式退订排除、无行放行、查询失败 fail visible；
>   派发结果新增 `skippedByPreference`）+ 事件字典/默认偏好/偏好页（前端）同源落位；
>   service 7 例 + config 4 例、service/config/dto/handlers 回归与 bootstrap 全绿、
>   前端 2 suites/16 tests + tsc/eslint 全绿；
>   证据 `docs/plan/evidence/assistant-observability/O1-02-sweeper-subscription-evidence.md`。
>   - **O1-02 条件项闭环 · 派发面板展示退订过滤人数**（2026-10-18，`integration_verified`）：
>   `/ai/observability` 派发结果在 `skippedByPreference > 0` 时新增「另有 N 名候选收件人因订阅偏好
>   （站内通知关闭）被排除」文案（与「去重跳过」口径显式区分；0/缺省不渲染，诚实缺省）；
>   zh/en i18n 同批；observability 17 tests（新增 2 断言/1 例）+ tsc/eslint 全绿。
>   条件项：真实 PG/RLS 冒烟与浏览器走查待环境。
> - **O2-02 已交付**（2026-10-18，`integration_verified`）——面板内诊断抽屉：
>   `DiagnosticsDrawer`（会话/消息数/生成状态 + 最近运行本地快照（v2 事件，零请求）+ 步骤耗时 +
>   按需加载 O1-01 轨迹并复用 `RunTraceView`）+ 报障导出（「复制轨迹 ID」/「复制会话 ID」，
>   埋点 `handoff(source=diagnostics_copy)`；纯问答回退会话 ID）；
>   入口在 `AssistantChatPanel` 页签行；token 显式诚实降级（B4-02 未接线）；
>   前端 37 suites/302 tests（新增 model 4 + drawer 4 + 面板入口 1）、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-dock/O2-02-diagnostics-drawer-evidence.md`。
>   - **O2-02 补全 · token 段已接线**（2026-10-18，`integration_verified`）——§13.4「轨迹/耗时/**token** 可见」
>   最后一段落地：后端把网关限流用的**字符/4 估算**经请求级 sink（`service/llm_usage.go`，
>   `WithUsageSink`/`reportUsageEstimate`，未注入零行为变化）在 6 个门禁点统一上报，
>   `chatStreamInner` 汇总为回合用量（TokenIn 累加工具循环；TokenOut = 已流式正文估算），
>   新增 v2 `usage` 事件（`{v:2, tokenIn, tokenOut, estimated:true, scope:"turn"}`，**恒在 done 前**、
>   零值也发；v1 载荷字节不变）；前端解析（畸形载荷丢弃不虚构）→ `stream_end` 埋点
>   （`tokenIn/tokenOut` + `extra.tokenEstimated`）→ 诊断抽屉 `diagnostics-usage` 段
>   （「用量（估算）」+ 输入/输出/合计 ≈ + 非账单口径说明；纯问答同样可见）。
>   门禁：后端 `service`/`handlers/ai`/`router`/`middleware` + `tests/botintegration`（真实 handler→RAG
>   端到端帧断言，含 usage<done 顺序契约）全绿；前端 127 suites/1663 tests、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-dock/O2-02-token-usage-evidence.md`。
>   条件项：失败回合/非 SSE 回退路径不带 usage。
> - **B4-02 已交付**（2026-10-18，`integration_verified`）——真实 usage 解析（provider 协议回包）：
>   四协议适配器解析（`openai_chat` usage 末帧 / `anthropic_messages` 分帧累加 / `google_gemini`
>   usageMetadata / `openai_responses` 终态快照；容错缺失→nil）+ 请求级上报通道
>   （`WithProviderUsageSink`，未注入零行为变化）+ `ResolveTurnUsage` 口径收口
>   （provider 覆盖全部 LLM 调用才置 `estimated=false`，部分覆盖保持估算）；
>   SDK 路径（Chat/ChatStream/ChatStreamWithTools/Streaming）与协议路径均接线；
>   `usage` v2 事件字段不变、`estimated` 透出实测标记；诊断抽屉「用量（provider 实测）」+
>   埋点 `extra.tokenEstimated` 透传；
>   后端 protocol/service/handlers 全绿（新增 12 例含端到端 sink 断言与请求体 `stream_options`）、
>   前端 128 suites/1672 tests + tsc/eslint 全绿；
>   证据 `docs/plan/evidence/assistant-observability/B4-02-real-usage-evidence.md`。
>   条件项：真实 provider 联调待环境；不做账单映射（缓存/推理细分为不入本事件）。
>   - **O2-02 补全 · 工作区入口复用 + 新增组件 axe 覆盖面**（2026-10-18，`integration_verified`）：
>   抽出共享入口 `AssistantDiagnosticsEntry`（按钮 + 懒加载抽屉，唯一实现；关闭态零请求），
>   Dock 面板重构复用（入口行为与 testid 不变），`/ai/chat` 工作区工具条接入
>   （`ai-chat-diagnostics-open` 独立 testid 免同页歧义、`pageKey="ai.chat"`、透传 messages/convId/streaming）；
>   I2-02 axe 扫描面 3 → 5：新增 `AssistantMemoryChips`（V2-01）与 `AssistantAsyncAnswerCard`（V2-02）两例
>   （V1-03 确认卡随后并入 → 6 面）；
>   前端 128 suites/1667 tests、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-dock/O2-02-workspace-entry-a11y-evidence.md`。
> - **O2-01 已交付**（2026-10-18，`integration_verified`）——分析增强（§13.6）：
>   ① 建议排序权重（先规则）：`suggestion_shown/clicked/ignored` 按 cardId 聚合，拉普拉斯平滑打分
>   （k=10、全局 CTR 先验），**仅组内重排**保持 I0-02 分组契约；D1-01 响应新增 `ranking/score/feedback`
>   （埋点未装配 → `registry` 兼容态）；
>   ② 分群对比：`GET /ai/observability/segments?dimension=role|pageKey`（建议 CTR/任务完成率/Inline 采纳率/错误率；
>   维度白名单防注入、非法维度 400）+ 看板「分群对比」卡（维度切换/窗口联动）；authz-gen 预检再生；
>   后端 5 包全绿；前端 56 suites/526 tests、tsc/eslint 全绿。
>   证据 `docs/plan/evidence/assistant-observability/O2-01-analysis-enhancements-evidence.md`。
>   - **O2-01 补全 · 忽略样本衰减 + 负样本参与打分**（2026-10-18，`integration_verified`）：
>   反馈按「卡 × 事件 × 天龄」分桶 → **半衰期 14 天**指数衰减（`decayedShown/decayedClicked/decayedIgnored`，
>   `0.5^(age/14)`，桶内 age 取 `ageDays+0.5` 均值修正）；打分补 λ=0.5 忽略惩罚
>   `(clicked' + k·prior) / (shown' + λ·ignored' + k)`（k=10）；与 D1-02 `suggestion_ignored` 采集接线后，
>   忽略样本即时进入排序；后端 service/handlers/ai 全绿（新增半衰期/忽略惩罚/衰减排序用例）、
>   前端 chat-core 13 例、AI 表面 48 suites/311 tests、tsc/eslint 全绿。
>   条件项：模型化排序（"后模型"）仍归 P2；真实 PG 冒烟与平台侧租户分群归后续。

---

## 8. 风险与缓解

| 风险 | 缓解 |
|---|---|
| entrypoint 膨胀 / 400 | `page_key` 提示层 + 页面映射最近语义入口；未知 key 忽略不报错 |
| 上下文隐私 | L2 显式注入 + 用户可见 + 服务端对象预检 fail-closed |
| 建议不可执行 | 服务端按授权交集过滤；负向断言入测试 |
| AI 文档幻觉 | 强制 grounding（Page Manifest/代码/既有 KB）+ 校验器 + 分级人工 |
| 文档双写漂移 | 单真源规则 + `source` 标签 + orphan 检测 |
| 面板打扰/成本 | 默认收起、不自动弹窗；复用会话与建议懒加载；沿用预算闸门 |
| chat-core 抽离回归 | 先抽离后接面板；以既有 AI 组件测试为回归基线 |
| 交互复杂度过载（状态多、用户迷路） | 状态机 + 错误字典统一文案；面板只暴露"当前该做的下一步"；可用性走查 |
| Inline 误应用（字段被错误覆盖） | diff 预览 + undo + 审计；写操作仍由页面提交 |
| 主动提示变成打扰 | 默认静默、仅三种允许形态、频率上限与"不再提示"；干扰率入指标 |
| 双入口行为不一致 | 共用 chat-core 与 store；一致性用例纳入回归 |

---

## 9. 度量

- Dock：展开率、建议点击率、零输入发起占比、P95 首 token、会话完成率；
- 写动作：确认成功率、回读通过率（沿用 `bot-metrics`）；
- 帮助：覆盖率（ratchet）、AI 起草采纳率、一键审阅通过率、帮助命中率（未升级人工率）、`help_gap` 收敛趋势。
- 任务闭环（目标验证）：任务卡发起占比、任务完成率、每任务输入字符数、inline 采纳率与 undo 率、转人工率；
- 体验健康：建议忽略率、主动提示关闭率（干扰率）、错误态恢复率。
- **试点对照（§11）**：单任务点击/输入字符/耗时降幅、自助解决率、误分类率、计划采纳率——按"新流程 vs 现状"同任务对照采集。
- 采集、看板、告警与轨迹回放的实现见 §13；试点对照报告由 §13.6 自动产出。

---

## 10. 证据与惯例

- 任务完成状态沿用 B 系列五级口径（`unit/ integration/ flow_verified` 等），证据落 `docs/plan/evidence/assistant-dock/`、`docs/plan/evidence/help-agent/`、`docs/plan/evidence/assistant-observability/`；
- 每个里程碑附复跑清单与负向断言；
- 本规划实施前需评审确认，P0 结束后复核 D2/D5 的落地口径。

---

## 11. 最终用户价值验证（按角色）

### 11.1 判断原则

1. **帮助程度与用户熟练度成反比**：越不熟系统的用户收益越大；对高频熟练坐席，价值必须来自"省步数"（Inline / 批量 / 草稿），聊天面板本身可能是负资产；
2. **价值必须可对照测量**：每个能力上线前声明它替代了什么动作（点击 / 输入 / 跳转 / 等待），上线后与基线对照；
3. **先承担最小承载**：试点不依赖 Dock 完成，用既有 `/ai/chat` + 页内组件即可跑通。

### 11.2 角色价值矩阵

| 角色 | 现状痛点 | 方案帮助点 | 帮助程度 | 前提 |
|---|---|---|---|---|
| 一线坐席（高频熟练） | 分类定级靠经验、找知识慢、答复手打 | Inline 一键应用、答复草稿、知识推荐、划词问 AI | 高（来自 Inline，非聊天） | diff + undo；规模化靠 V1-01 |
| 普通员工（低频不熟） | 不知提什么单、字段怎么填、进度哪里看 | 自助问答、一句话建单、帮助闭环 | 最高 | 员工侧入口（V1-02） |
| 二线 / 变更经理 | 影响分析、复盘靠人肉查 | 任务卡（影响分析 / 复盘 plan） | 中高 | 产物落点（Outcome Contract） |
| IT 管理员 | 配置问题查文档 | 帮助问答、文档维护闭环 | 中 | 团队层面收益 |

### 11.3 用户无感的支撑工程（诚实标注）

Dock 容器、chat-core 抽离、交互状态机、帮助覆盖率 ratchet、Autopilot 巡检——用户不会因此感知"好用"，它们是承载与治理；**不得作为唯一交付物**，必须与 V 系列同批验收。

### 11.4 用户视角补充需求（已纳入 V 系列）

1. 列表页批量 AI（坐席的真实工作单元是"一批单"）；
2. 员工侧全入口（移动端 / 门户 / 邮件）；
3. 个性化记忆（常用语 / 上次处理方式，可关闭）；
4. 时效体验（先答后精 + 完成通知）；
5. 口语↔术语确认（"我理解成 …"）。

### 11.5 放量门槛

- V0-01～V0-03 三项试点中 ≥2 项达到对照目标（降幅 ≥30% 或等值业务指标）；
- 负向指标不恶化：误分类率、建议忽略率、干扰率、转人工率；
- 达标后按 §7 顺序放量 Dock；未达标先迭代试点，不扩面。

---

## 12. 开放问题（不阻塞开工，评审时收敛）

| # | 问题 | 建议口径 | 收敛时点 |
|---|---|---|---|
| Q1 | 页面接入标准（哪些页面必须有任务卡） | 核心 40 个业务页面 100% 有任务卡；其余页面统一降级为"通用助手 + 帮助状态" | P0 结束 |
| Q2 | MSP 工作台的助手客户上下文 | 与 P2 多租户帮助空间一起设计（助手需感知 `CustomerFilter` 当前客户） | P1 评审 |
| Q3 | 旧 launcher（页面"问 AI"按钮）兼容周期 | 保留 1 个版本周期的 `/ai/chat` 深链兼容，随后统一改为打开 Dock | P0 结束 |
| Q4 | 帮助文档 owner 的运营职责与 SLA | frontmatter `owner` 仅兜底；平台侧按季度复核覆盖率与 stale 清单 | H1 评审 |
| Q5 | 模型分级路由策略与成本上限 | P2 细化：任务卡/轻问答 → 轻模型；复杂任务 → 强模型；租户预算闸门 | P2 启动前 |

---

## 13. 观测性与分析组件（O 系列）

> 目的：让"AI 是否降低了使用成本、任务是否被完成、哪里在失败"成为**可观测、可分析、可告警**的事实，而不是主观感受。
> 复用：`bot_runs/bot_steps/bot_events`、`tool_invocations`、`ai_audit`、`/ai/bot-metrics`、`/ai/evaluation`、通知中心。

### 13.1 原则

1. **元数据优先，正文不重复落**：只采集行为/性能/结果元数据；对话正文留在既有会话表，轨迹以 `conversation_id/run_id` 关联；
2. **四层口径**：采用（adoption）→ 效果（outcome）→ 质量（quality）→ 成本与时延（cost/latency），全部可按租户/页面/角色/场景切片；
3. **埋点不阻塞交互**：批量、采样、失败静默；埋点异常不得影响对话与任务；
4. **合规**：租户隔离 + Redactor 脱敏 + 保留期；用户可关闭个性化埋点（保留匿名聚合）。

### 13.2 事件字典（前端 + 后端统一契约）

| 事件 | 关键字段 | 用途 |
|---|---|---|
| `assistant_opened/closed` | page_key、route、entrypoint、source（fab/快捷键/launcher） | 采用率、入口分析 |
| `context_loaded/degraded` | page_key、有无 target、耗时 | 上下文质量 |
| `suggestion_shown/clicked/ignored` | card_id、kind（task/question）、rank | 建议排序与价值 |
| `task_started/completed/failed` | task_card_id、outcome.type、耗时、error_code | 任务闭环漏斗 |
| `message_sent / first_token / stream_end` | latency_ms、model、token_in/out、provider_source | 性能与成本 |
| `tool_call / confirmation_shown/accepted/rejected / approval_pending` | tool、risk、reason_code | 治理与信任 |
| `inline_preview/applied/undone` | field、source（triage/ai）、undo 原因 | Inline 采纳与风险 |
| `help_opened / help_gap / doc_draft_created / doc_draft_published / doc_feedback` | page_key、query_hash、coverage_status、extra.helpful、extra.kind | 帮助闭环与缺口、起草落地率（草稿→成文）、反馈解决率（P2 运营报表） |
| `error_shown / recovered / handoff` | error_code、recovery_action | 异常体验 |
| `proactive_shown/clicked/dismissed` | scope（page/approval/notification）、dismiss 模式（session/forever） | 主动协助触达与干扰率、静默开关生效性 |
| `self_service_answered/resolved/ticket_created / ai_create_field_corrected` | page_key、results/field、task_card_id、duration_ms | V0-02 自助解决率、误分类率代理（字段纠正率）、一句话建单时长 |
| `memory_updated` | extra.op（phrase/template saved/deleted、action_recorded、cleared、disabled/enabled）、条数 | V2-01 个性化记忆采纳与合规开关审计（仅元数据，无正文） |
| `telemetry_disabled` | user_pref | 合规审计 |

### 13.3 四层看板（管理页 `/ai/observability`）

| 层 | 指标 |
|---|---|
| 采用 | 打开率、活跃用户、建议点击率、任务卡发起率、零输入占比、入口分布 |
| 效果 | 任务完成率、单任务点击/输入字符/耗时（对照 §11）、inline 采纳率与 undo 率、转人工率、自助解决率 |
| 质量 | 引用覆盖率、无引用回答率、点踩率、verify 通过率、错误率（按 error_code）、重试成功率 |
| 成本/性能 | P50/P95 首 token、端到端时延、token 成本/任务、模型分布、预算超限率 |

- 运营层：帮助覆盖率/stale、文档起草采纳率、`help_gap` 收敛；
- 支持按 页面/角色/租户/场景 Bot 过滤；预置"试点对照报告"（§11 可直接使用）。

### 13.4 单任务全链路轨迹（Trace）

- 主键：`run_id`（纯问答以 `conversation_id + message_id` 聚合）；
- 链路：会话 → run → step → tool_invocation → confirmation/approval → artifact → 业务对象（Outcome Contract 引用）；
- 使用：管理侧回放排查；用户侧"诊断抽屉"展示当前会话的运行轨迹/耗时/token，一键复制轨迹 ID 用于报障。

### 13.5 实时健康与告警

- 监控项：Provider 可用性与错误率、SSE 中断率、工具失败率、确认超时率、队列积压、预算消耗、watchdog 状态；
- 告警通道：通知中心（复用现有通知）+ 平台级系统配置开关；
- SLO 建议（阈值评审定）：首 token P95 ≤ 3s、任务成功率 ≥ 95%、错误态恢复率 ≥ 90%。

### 13.6 分析增强

- 建议排序：按 `suggestion_clicked/ignored` 反馈调整权重（先规则，后模型）；
- 试点对照报告：自动输出 §11 的"新流程 vs 基线"对比；
- 分群对比：租户/角色/页面维度找"哪里对用户真正有用"；
- 失败聚类：error_code + 工具失败 + 零命中问题 → 文档待办与产品改进清单。

### 13.7 落地组件

- 前端：`src/lib/ai/telemetry.ts`（统一埋点 SDK：批量/采样/脱敏/失败静默）、`src/components/ai/diagnostics/*`（面板诊断抽屉）；
- 后端：`service/observability/`（事件接收 `POST /ai/telemetry/events`、聚合查询）、扩展现有 `/ai/bot-metrics` 与 `/ai/evaluation`；
- 数据：`ai_telemetry_event` 表（租户前置索引 + 时间分区 + 保留期策略）；
- 管理页：`pages/(main)/ai/observability/index.tsx`（看板/轨迹检索），菜单挂在 AI 分组。
