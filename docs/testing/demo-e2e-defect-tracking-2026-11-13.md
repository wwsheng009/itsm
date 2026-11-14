# ITSM 演示环境 E2E 缺陷跟踪台账（2026-11-13 / runId `20261113A`）

> **配套计划**：[`demo-multi-tenant-e2e-business-test-plan-2026-11-13.md`](./demo-multi-tenant-e2e-business-test-plan-2026-11-13.md)
> **环境**：`http://172.18.3.238:8089`（`itsm-demo`，`saas_msp`）
> **代码基线**：本地工作区 @ `fbd9c128c`；演示栈 `VERSION=1.8.0-dev.a29ad103`（`.env.demo.example`）
> **说明**：本文件只登记**实测可复现**的现象；每条严格区分「已观测事实」与「未验证假设」，避免把猜测写成结论。
> **状态图例**：`open` 未修 / `investigating` 定位中 / `fixed` 已改代码 / `verified` 已在 238 回归通过 / `by-design` 设计如此 / `wontfix` 不修（附理由）

---

## 0. 本轮进度概览

| 组 | 覆盖 | 状态 |
|---|---|---|
| R1 账号落地 | `admin`、`demo.msp.manager`、`demo.mfg.user`、`demo.mfg.agent` | 部分完成（4/22） |
| R2 工单生命周期 | TKT-01 创建、TKT-02 评论跟踪、TKT-04 分配流转 | 通过 |
| D 组问题管理 | PRB-01 列表 | 通过（含统计口径瑕疵 D4） |
| G 组知识库 | KB-01 列表、KB-06 写权限 | 通过（含 D5 浏览量全 0） |
| AI 组 | AI-02 创建页分诊、AI-03 详情分诊 | **失败（D1）** |
| 待执行 | SR 审批链、CHG 变更、PRJ 项目、SEC 隔离、med/edu 同构、AI 对话 | 未开始 |

**当前缺陷计数**：P1 × 2，P2 × 3，P3 × 2

---

## 1. 缺陷清单

### D1 · P1 · 工单创建页 AI 智能分类不可用

| 字段 | 内容 |
|---|---|
| 用例 | AI-02（创建工单时 AI 分诊） |
| 账号 / 租户 | `demo.mfg.user`（华辰智能制造，tenant 3） |
| 路径 | `/tickets/create` → 填标题+描述 → 点「获取 AI 建议」 |
| **已观测事实** | 页面出现错误提示 **「AI 分类服务暂时不可用」**；无分类/优先级建议返回 |
| 对照观测 | 同一工单在**详情页**「AI 分析」面板可渲染，但返回 `Explanation = keyword heuristic`、`Confidence = 0.55` —— 后端走了**关键词启发式兜底**，未产出真实 LLM 结果 |
| 代码定位 | 前端抛出点：`itsm-frontend/src/pages/(main)/tickets/create/index.tsx:629`（`catch` 分支 `setAiError('AI 分类服务暂时不可用')`）；请求 `POST /api/v1/ai/triage`（同文件 L585） |
| 后端链路 | `itsm-backend/service/triage_service.go` `SuggestForTenant`：Priority 1 `guidanceClient.Triage`（sidecar）→ 失败/降级 → Priority 2 `llmClassify`（网关）→ 再失败则 `keywordFallback`（L197 / L375 / L485 三处 `Explanation: "keyword heuristic"`） |
| 归因（假设，待验证） | 前端 `catch` 触发说明 `httpClient.post` 抛错（非 2xx 或网络层失败），而非后端返回降级结果；若为后者，前端应正常展示建议。需核对 `POST /api/v1/ai/triage` 实际状态码与响应体 |
| 影响 | 演示中「AI 原生」卖点最强的入口失效，且降级对用户不透明 |
| 状态 | **investigating** |
| 待取证 | ① 接口实际状态码/响应体；② backend 与 ai-service 日志中 sidecar 调用与网关调用结果；③ 千帆通道在容器内是否可达 |
| 建议修复方向 | ① 前端：区分「服务不可用」与「AI 降级结果」，后者应展示建议并标注来源为启发式；② 后端：确认 sidecar 与网关是否均不可达，千帆配置是否真正生效 |

---

### D2 · P1 · 新建工单未绑定 SLA，详情显示「未配置 SLA 定义」

| 字段 | 内容 |
|---|---|
| 用例 | TKT-09（SLA 展示） |
| 账号 / 租户 | `demo.mfg.user` 创建，`demo.mfg.agent` 查看 |
| 路径 | 工单 `#629` / `TKT-202611-000505` 详情「SLA 信息」区 |
| **已观测事实** | 显示 **「未配置 SLA 定义，请联系管理员」**；无 SLA 截止时间、无剩余时长、无响应/解决时限 |
| 矛盾点 | `docs/multi-tenant/demo-environment.md` §3 明确「每租户经租户模板开通：SLA 定义与策略」，即租户**应已有** SLA 定义；同时工作台 SLA 看板显示超期 43 张，证明部分工单**有** SLA |
| 归因（假设） | 新建工单在创建时未执行 SLA 匹配/绑定（依据优先级 + 工单类型 + 分类选取 SLA 定义并计算 due 时间），导致 SLA 策略字段为空 |
| 影响 | 新单无 SLA 计时 → SLA 看板、超期统计、报表口径失真；演示「SLA 履约」主线中断 |
| 状态 | **investigating** |
| 待验证 | 打开一张 `[DEMO]` 种子工单看其是否显示 SLA：若显示 → 确认为「新建单不绑定」；若同样为空 → 升级为「SLA 定义未随模板安装」的更大范围问题 |

---

### D3 · P2 · 新建工单的「所属部门 / 所属客户」为空

| 字段 | 内容 |
|---|---|
| 用例 | TKT-01（创建工单） |
| 账号 / 租户 | `demo.mfg.user`（种子数据中 department = 生产部） |
| 路径 | 工单 `#629` 详情基础信息区 |
| **已观测事实** | 「所属部门」显示 `-`，「所属客户」显示 `-` |
| 矛盾点 | 同一用户发布的评论正确显示「刘洋（制造-员工）· 租户身份 #7」，说明用户身份与租户上下文完整；问题在**工单实体**未落 department / customer 字段 |
| 影响 | ① 服务请求审批按「申请人部门 == 审批人部门」过滤（见 demo-environment.md「组织与审批待办」），工单侧若同口径则审批链会断；② MSP 客户维度统计失真 |
| 状态 | **open** |
| 待验证 | 该用户若提交**服务请求**（走审批链），审批人能否看到 —— 若看不到，D3 升级为 P1 |

---

### D4 · P2 · 问题列表统计卡缺少「已识别」状态项，分项合计与总数不符

| 字段 | 内容 |
|---|---|
| 用例 | PRB-01（问题列表） |
| 账号 / 租户 | `demo.mfg.agent` |
| 路径 | `/problems` 页面统计区 |
| **已观测事实** | 「总问题数 **3**」；分项「待处理 **1**」「调查中 **1**」「已解决 **0**」，合计 **2 ≠ 3**；表格中实际存在状态为「已识别」的记录（ID 3 `[DEMO] 扫码设备批量断连根因（已定位）`） |
| 归因 | 统计卡未包含 `identified`（已识别）状态，状态枚举覆盖不全 |
| 影响 | 数字对不上，演示时会被追问；不影响业务操作 |
| 状态 | **open** |
| 建议修复 | 补 `identified` 分项（或合并为「其他」展示），并加断言「分项合计 == 总数」 |

---

### D5 · P3 · 知识库「总浏览量」恒为 0

| 字段 | 内容 |
|---|---|
| 用例 | KB-01（知识库浏览） |
| 账号 / 租户 | `demo.mfg.agent` |
| 路径 | `/knowledge` 统计区 |
| **已观测事实** | 文章总数 6 / 已发布 6 / 草稿 0 / **总浏览量 0** |
| 归因（假设） | 浏览量埋点未落库或统计未接入；也可能种子无浏览记录且本轮尚无人阅读 |
| 影响 | 演示价值弱化，非功能缺陷 |
| 状态 | **open（低优先）** |
| 待验证 | 打开一篇文章后返回列表，确认浏览量是否 +1 |

---

### D6 · P3 · 侧栏标题与 Pilot 徽标之间多余空白

| 字段 | 内容 |
|---|---|
| 用例 | UX-01（文案洁净度） |
| 路径 | 侧栏菜单：服务请求、问题管理、知识库、AI 助手 |
| **已观测事实** | 可访问性树读取为 `"问题管理 Pilot"`、`"知识库 Pilot"`，文本与 Badge 存在额外空节点；视觉上间距偏大 |
| 影响 | 纯观感 |
| 状态 | **open（低优先）** |

---

### D7 · P2 · 「问 AI」按钮在终端用户工单详情疑似置灰不可用

| 字段 | 内容 |
|---|---|
| 用例 | AI-03（工单 AI 摘要） |
| 账号 / 租户 | `demo.mfg.user` → 工单 `#629` 详情 |
| **已观测事实** | 详情页工具栏「问 AI」按钮呈 disabled 视觉态，点击无响应 |
| 对照 | 同一按钮在 `demo.mfg.agent` 视角下**可用**（可点击并渲染 AI 分析面板） |
| 归因（假设） | 该按钮权限判定对 `end_user` 关闭，但未给出「无权限」提示，用户只能感知为「坏了」 |
| 影响 | 终端用户体验受损；若为设计如此，应补 tooltip 说明 |
| 状态 | **investigating**（需确认是否 by-design） |

---

## 2. 已通过用例（正向确认，供回归基线）

| 用例 | 账号 | 结论 | 证据要点 |
|---|---|---|---|
| PLT-01 | `admin` | ✅ 落地 `/dashboard`，侧栏含「系统管理」，顶栏「超级管理员」 | 登录后页面结构 |
| MSP-01 | `demo.msp.manager` | ✅ 落地 `/msp/workbench`（非 `/dashboard`），符合服务商期望 | URL |
| MSP-02 | `demo.msp.manager` | ✅ 客户 3 个；未关闭 43；超期 43；临近 0；客户明细 制造 17 + 教育 15 + 医疗 11 = 43，与汇总一致 | 工作台汇总区 |
| UX-02 | `demo.msp.manager` | ✅ 顶栏「林致远（服务商经理）」，**未**出现裸 code `End_user`（历史问题未复现） | 顶栏 |
| R1 | `demo.mfg.user` | ✅ 落地 `/dashboard`；顶栏「刘洋（制造-员工）/ 最终用户」；侧栏无「系统管理」、无 MSP 菜单 | 侧栏结构 |
| R1 | `demo.mfg.agent` | ✅ 落地 `/dashboard`；顶栏「王莉（制造-服务台）/ 服务台坐席」；侧栏含工单/事件/问题/变更/知识库/CMDB | 侧栏结构 |
| SEC-01（预检） | `demo.mfg.user` / `demo.mfg.agent` | ✅ 客户角色侧栏均无 MSP 入口 | 侧栏结构 |
| TKT-01 | `demo.mfg.user` | ✅ 创建工单成功 `#629` / `TKT-202611-000505`，状态「新建」，优先级「中」 | 详情页 + 成功 toast |
| TKT-02 | `demo.mfg.user` | ✅ 添加评论成功（toast「评论发表成功」），评论落库显示作者 + 租户身份 #7 + 相对时间 | 时间线 |
| TKT-04 | `demo.mfg.agent` | ✅ 分配处理人成功（toast「工单分配成功」），状态 `新建 → 待处理`，处理人「吴迪（制造-IT 主管）」，流转进度条同步 | 详情页 |
| PRB-01 | `demo.mfg.agent` | ✅ 问题列表 3 条，覆盖「待处理 / 调查中 / 已识别」；看板视图 Tab 存在 | 列表 |
| KB-01 | `demo.mfg.agent` | ✅ 知识库 6 篇已发布，分类含故障排查 / 应急预案 / 规范制度 / 服务指南 / 常见问题 / 操作指南 | 列表 |
| KB-06 | `demo.mfg.agent` | ✅ 服务台具备「新建文章」入口（写权限符合权限矩阵） | 页头按钮 |

---

## 3. 测试数据登记（本轮产生）

| 类型 | 标识 | 创建者 | 归属租户 | 清理方式 |
|---|---|---|---|---|
| 工单 | `#629` / `TKT-202611-000505`「[E2E-20261113A] 车间打印终端无法连接共享打印机」 | `demo.mfg.user` | 制造（tenant 3） | 轮末置 `cancelled` |
| 评论 | 工单 `#629` 时间线（2 条：提单描述 + 补充信息） | `demo.mfg.user` | 制造 | 随工单保留（审计） |
| 分派 | 工单 `#629` → 吴迪（`demo.mfg.itadmin`） | `demo.mfg.agent` | 制造 | 随工单保留 |

> **未触碰**：`[DEMO]` 种子单据、`demo.*` 种子账号、`review-test` 遗留租户。

---

## 4. 下一步（按优先级）

1. **D2 定性**：打开一张 `[DEMO]` 种子工单，确认其是否显示 SLA。
2. **D1 定性**：取 `POST /api/v1/ai/triage` 实际状态码；确认千帆通道在 ai-service 侧是否可达。
3. **D3 升级判定**：以 `demo.mfg.user` 提交服务请求，验证 `demo.mfg.lead` 能否看到（部门过滤是否被 D3 击穿）。
4. 继续执行 SR-01~10（多级审批）、CHG-02（变更全链路）、PRJ-02（进度派生）、SEC-02/03（跨租户隔离）。
5. 医疗 / 教育租户同构抽测（TKT-13/14、KB-06）。

---

## 5. 追加：SLA 相关缺陷定性（2026-11-13 第二轮取证）

### 5.1 D2 根因已确认（接口层 vs 前端层字段契约不一致）

**取证方法**：以 `demo.mfg.agent` 登录取得 `access_token` Cookie，直接请求
`GET /api/v1/tickets/{id}/sla`，对比前端渲染逻辑。

**接口实际返回（工单 #629，较新单）**
```json
{"code":0,"message":"success","data":{
  "ticketId":629,
  "slaDefinitionId":9,
  "slaStatus":"active",
  "slaResponseDeadline":"2026-11-13T02:00:25.461366Z",
  "slaResolutionDeadline":"2026-11-13T03:45:25.461366Z",
  "firstResponseAt":null,
  "resolutionDeadlineBreached":false,
  "slaPauseReason":"","slaPausedAt":null,
  "status":"open"
}}
```

**接口实际返回（工单 #3，`[DEMO]` 种子单）**
```json
{"data":{"ticketId":3,"slaDefinitionId":18,"slaStatus":"active",
 "slaResponseDeadline":"2026-10-13T10:16:05.197278Z",
 "slaResolutionDeadline":"2026-10-13T16:16:05.197278Z",
 "resolutionDeadlineBreached":true,"status":"closed"}}
```

**结论**：
1. **后端 SLA 绑定是正常的**——工单 #629 绑定了 `slaDefinitionId=9`，工单 #3 绑定了 `18`，且已算出响应/解决时限、`resolutionDeadlineBreached=true`。此前"D2 = 新建单未绑定 SLA"的假设**不成立，已撤销**。
2. **真实缺陷在前后端字段契约不一致**：
   - 后端返回：`slaDefinitionId` / `slaResponseDeadline` / `slaResolutionDeadline` / `slaStatus` / `resolutionDeadlineBreached`
   - 前端读取：`slaInfo.slaName` / `slaInfo.isBreached` / 以及 `responseDeadline`、`resolutionDeadline` 形式的字段
   - 因后端**根本没有返回 `slaName`**，`TicketDetail.tsx:1232` 的 `slaInfo.slaName ? ... : <Text>{t('ticketDetail.slaNotConfigured')}</Text>` 判定落到 else 分支，渲染出「未配置 SLA 定义 / 尚未配置」。
3. 同理，前端拿不到 `responseDeadline` / `resolutionDeadline`，SLA 倒计时与进度条无法计算。

| 字段 | 后端返回 | 前端期望 | 结果 |
|---|---|---|---|
| SLA 名称 | ❌ 无 `slaName` | `slaInfo.slaName` | 落入「未配置」分支 |
| 响应截止 | `slaResponseDeadline` | `responseDeadline` | 不匹配，无法展示 |
| 解决截止 | `slaResolutionDeadline` | `resolutionDeadline` | 不匹配，无法展示 |
| 是否违约 | `resolutionDeadlineBreached` | `isBreached` | 不匹配，违约色/文案失效 |
| 定义 ID | ✅ `slaDefinitionId` | —（未使用） | 有值但前端未用于展示名称 |

**影响重新评估（升级为 P1，理由更强）**：
- 演示中每一张工单的「SLA 信息」卡片都是空的，而 SLA 页却宣称「生效 SLA 7 / 累计违规 38 / 合规率 45%」——**同一系统内自相矛盾**，是演示时最容易被现场质问的点。
- SLA 倒计时、违约高亮、SLA 进度条三个可视化能力全部失效。

**修复方向（待确认后实施）**：
- 方案 A（推荐，改后端）：`handlers/ticket/service.go` 组装 SLA 响应时补 `slaName`（按 `slaDefinitionId` 查定义名），并同时输出 camelCase 的 `responseDeadline` / `resolutionDeadline` / `isBreached` 兼容字段（保留现有 snake/camel 双写风格）。
- 方案 B（改前端）：`TicketApi.getTicketSLA` 后做字段归一化映射，`slaResponseDeadline → responseDeadline`，并用 `slaDefinitionId` 反查名称或显示「SLA 定义 #9」。
- 建议 A+B 都做：后端保证语义完整，前端做容错归一化。

**状态**：`investigating` → 根因已定位，待修复。

---

### 5.2 新增缺陷 D8~D11（SLA 监控页，`demo.mfg.agent`）

| ID | 级别 | 现象（已观测） | 位置 | 状态 |
|---|---|---|---|---|
| **D8** | P2 | MTTR 显示未格式化的原始浮点：**「2.9677238777777775 分钟」**，应立即取整/保留 1 位小数 | `/sla` MTTR 卡片 | open |
| **D9** | P2 | 「AI 趋势预测」预测窗口为 **2026-05-13 ~ 2026-12-25**：起始日期早于今天（2026-11-13）半年，窗口口径错误；且长时间 loading 后仍显示「暂无预测数据」 | `/sla` AI 趋势预测区 | open |
| **D10** | P2 | **同一页三处口径互相矛盾**：顶部告警「38 个 SLA 违规尚未处理」/ 右下「实时风险告警 → 当前没有 SLA 风险告警」/ MSP 工作台「超期 43」。38 ≠ 43，且"有 38 个违规"与"没有风险告警"直接冲突 | `/sla` + `/msp/workbench` | open |
| **D11** | P3 | 「平均响应时间 **0 分钟**」：在 38 条违规、合规率仅 45% 的数据下，平均响应 0 分钟不合常理，疑为 `firstResponseAt` 全为 null 导致未统计却展示 0 | `/sla` 平均响应时间卡片 | open |

> **D10 补充说明**：三处数字差异可能源于口径不同（"违规"按违反次数 vs "超期"按工单数；或统计窗口不同）。但**页内「38 个违规」与「没有风险告警」的自相矛盾无法用口径解释**，属确定性缺陷。

---

### 5.3 D3 重新定性（撤销"新建单不落字段"假设）

| 字段 | 内容 |
|---|---|
| 原假设 | 新建单未写入 department / customer 字段 |
| **新证据** | `[DEMO]` 种子工单 #3 的「所属部门」「所属客户」**同样显示 `-`** |
| **新结论** | 不是"新建单不落字段"，而是**全量工单的这两个字段为空，或详情页字段映射缺失**。影响面从"新单"扩大到"所有工单" |
| 影响升级 | 若工单实体确实无 department，则「按部门过滤审批」「部门维度报表」全部失效；若仅详情页映射错误，则影响限于展示。**需查库确认** |
| 状态 | open（待用 SQL 核对 `tickets` 表 department 字段是否有值） |

---

## 6. 追加：D1（AI 不可用）根因已锁定 —— **本轮最高优先级缺陷**

### 6.1 取证链

| 步骤 | 证据 | 结论 |
|---|---|---|
| ① UI 现象 | `/tickets/create` 点「获取 AI 建议」→「AI 分类服务暂时不可用」 | 前端报错 |
| ② 直连接口 | `POST /api/v1/ai/triage`（`demo.mfg.user` 与服务台）均返回 **403**；加 `Origin`/`Referer`/`X-Requested-With` 仍 403 | 非 CSRF，是后端拒绝 |
| ③ 权限核查 | `/api/v1/auth/me` 显示服务台**确有** `ai:read`；`rbac_precheck_gen.go:706/735` 也正确映射 `/ai/chat`、`/ai/triage` → `ai:read` | 权限模型正常，403 来自 handler 内部 |
| ④ 关键证据 | `GET /api/v1/ai/llm-calls` 返回 **132 条 LLM 调用记录**，全部：`provider="openai_chat_completions"`、`providerSource="static"`、`model="gpt-4o-mini"`、`success=false`、`errorClass="timeout"`、`errorMessage="OpenAI API error: Post \"https://api.openai.com/v1/chat/completions\": context deadline exceeded"` | **AI 实际打向 api.openai.com，全部超时** |
| ⑤ 配置溯源 | `docker-compose.demo.yml:124-132`：`LLM_PROVIDER=${LLM_PROVIDER:-openai}`、`LLM_MODEL=${LLM_MODEL:-gpt-4}`、`OPENAI_BASE_URL=${OPENAI_BASE_URL:-}`；sidecar `:235` `LLM_BASE_URL=${LLM_ENDPOINT:-${OPENAI_BASE_URL:-https://api.openai.com/v1}}` | 未注入时**默认落到 api.openai.com** |

### 6.2 缺陷结论 D1（升级为 **P0 阻断**）

**已观测事实**：
1. 演示栈的 LLM 配置**没有指向千帆**（`qianfan.baidubce.com/v2/tokenplan/personal/chat/completions`）。运行时 `providerSource=static`、`provider=openai_chat_completions`、`model=gpt-4o-mini`，即使用了代码里的静态默认值。
2. 全部 132 条调用 `success=false`，`errorClass=timeout`，目标域名 `https://api.openai.com`。服务器（境内）无法访问 OpenAI，必然超时。
3. 因此 AI 功能**整体不可用**：`/ai/triage` 因上游不可用返回 403（handler 将上游失败映射为拒绝），前端再把它显示成「AI 分类服务暂时不可用」。
4. 工单详情「AI 分析」返回 `Explanation = keyword heuristic`、`Confidence = 0.55`，即降级到关键词启发式——进一步印证 LLM 链路全断。

**用户给出的千帆配置尚未生效**：
- 端点：`https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions`
- 密钥：`bce-v3/ALTAKSP-…`（见任务说明）

**修复方案（待实施）**：
- 在 `.env.demo`（238 上的实际 env 文件）注入：
  ```
  LLM_PROVIDER=openai                 # 千帆 v2 兼容 OpenAI Chat Completions 协议
  LLM_MODEL=<千帆侧模型名，如 ernie-4.5-turbo-128k 或 qianfan 指定模型>
  LLM_API_KEY=<REDACTED_LLM_API_KEY>
  LLM_ENDPOINT=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions
  OPENAI_BASE_URL=https://qianfan.baidubce.com/v2/tokenplan/personal
  OPENAI_API_KEY=<同上>
  LLM_TIMEOUT=60
  ```
- 或通过「系统设置 → AI 提供商」（`LLM_MULTI_PROVIDER_ENABLED=true` 已开）在租户级配置千帆 provider，`providerSource` 应从 `static` 变为租户配置来源。
- 同步改前端：把 403/上游失败与「AI 降级结果」区分展示，不要一律说「服务不可用」。

**影响**：演示的核心卖点「AI 原生 ITSM」全链路失效；这是**当前演示最大风险项**。

**状态**：`investigating` → **根因已确认（配置未注入千帆）**，待修复验证。

### 6.3 连带缺陷

| ID | 级别 | 现象 | 说明 |
|---|---|---|---|
| **D12** | P1 | `/`ai/llm-calls` 中 `providerSource` 恒为 `static`，`providerKey` 为空 | 说明租户级 LLM 提供商配置从未生效，`LLM_MULTI_PROVIDER_ENABLED=true` 形同虚设 |
| **D13** | P2 | 上游 LLM 失败被映射为 **HTTP 403** 返回前端 | 语义错误：上游不可用应为 502/503，403 会被前端当作权限问题，误导排查方向 |

---

## 7. 追加：服务请求审批链测试（R3 轮次）

### 7.1 已通过用例

| 用例 | 账号 | 结论 | 证据 |
|---|---|---|---|
| SR-02 | `demo.mfg.lead` | ✅ `/service-requests` 「待审批」Tab 可见 **1** 条 `[DEMO] 生产部账号开通申请`，符合"本部门待批"口径 | 列表 |
| SR-03 | `demo.mfg.lead` | ✅ 审批通过成功（toast「操作成功」）；状态 `已提交 → 经理已审批`；审批链记录「已通过 by 赵建国（制造-部门经理）· 租户身份 #6 · 2026-11-13 10:00」 | 详情页 |

### 7.2 新增缺陷

#### D14 · P1 · 已审批的服务请求仍显示「通过 / 拒绝」审批按钮（重复审批风险）

| 字段 | 内容 |
|---|---|
| 用例 | SR-03 / SR-05 |
| 账号 | `demo.mfg.lead` → 服务请求 #1 |
| **已观测事实** | 经理审批通过后，页面状态已是「经理已审批」，「1. 经理审批 = 已通过」，但右下「审批操作」区**仍然渲染并可点击**「通过」「拒绝」两个按钮 |
| 影响 | ① 同一审批人可重复审批同一节点，审批记录可信度受损；② 若后端未做幂等/状态校验，可能产生重复审批记录或推进错节点；③ 演示中会让"审批已完成"的语义显得不可信 |
| 归因（假设） | 前端仅按「当前登录人是否为该节点审批人」渲染按钮，未叠加「该节点状态是否已完成」判断 |
| 状态 | open（需确认后端是否有幂等保护；若无则升级 P0） |

#### D15 · P2 · 服务请求统计卡与列表数量不一致

| 字段 | 内容 |
|---|---|
| 用例 | SR-02 |
| 账号 | `demo.mfg.lead` |
| **已观测事实** | 页头「请求总数 **0**」「待审批 **0**」「已审批 **0**」，但「待审批」Tab 徽标显示 **待审批 (1)**，点击后列表确有 **1** 条记录 |
| 影响 | 统计口径失真；演示时"0 vs 1"会被立刻发现 |
| 归因（假设） | 统计 API 写死了租户级全量口径，未叠加「按审批人 + 部门」的可见范围；或统计接口与列表接口使用的过滤条件不同 |
| 状态 | open |

#### D16 · P2 · 服务请求的「服务名称」与「申请人」显示为内部 ID 而非名称

| 字段 | 内容 |
|---|---|
| 用例 | SR-02 / SR-03 |
| **已观测事实** | ① 详情「服务名称」= **「服务 #9」**（应为目录项名称，如「账号开通」）；② 列表「申请人」= **「用户 #8」**（应为姓名，如「刘洋」） |
| 对照 | 工单评论却能正确显示「刘洋（制造-员工）」，说明用户名称解析能力存在，仅服务请求域未使用 |
| 影响 | 审批人在不知服务内容、不知申请人是谁的情况下决策，演示可信度受损 |
| 状态 | open |

#### D17 · P2 · 「所属部门 / 所属客户」在服务请求中同样为空

| 字段 | 内容 |
|---|---|
| 用例 | SR-02 |
| **已观测事实** | 服务请求 #1 的「所属部门」「所属客户」「成本中心」均为 `-` |
| 关系 | 与 D3（工单同样为空）构成**同一根因族**：单据实体的组织/客户字段未落库或详情页未映射 |
| **重要推论** | 本条直接影响「部门审批过滤」：若请求实体真的无 department，则 SR-07（跨部门不可见）无法通过数据实现，只能靠申请人所属部门间接判定。**需查库确认** |
| 状态 | open |

#### D18 · P3 · 侧栏 Pilot 徽标与文字间距异常 / 徽标图形重叠

| 字段 | 内容 |
|---|---|
| 用例 | UX-01 |
| **已观测事实** | 截图中「服务请求」`Pilot`、「问题管理」`Pilot`、「知识库」`Pilot` 三处：徽标与文字之间空白过宽（约 2 个字符宽）；「问题管理」的 Pilot 徽标上还叠加了一个 `v` 形图形，与徽标文字重叠 |
| 关系 | 与 D6 为同一问题，本条补充"图形重叠"证据，合并处理 |
| 状态 | open（低优先） |

### 7.3 撤销的误判（诚实记录）

| 原判断 | 撤销理由 |
|---|---|
| 「`通过` 按钮的 check 图标与文字重叠遮挡」 | 放大截图核对后确认：是圆形 check 图标 + 文字「通过」的正常按钮布局，无重叠。**误判，撤销** |

---

## 8. 追加：服务请求审批链 **在 L2 断裂**（P0 阻断，本轮第二严重缺陷）

### 8.1 取证链（决定性）

| 步骤 | 操作 | 结果 |
|---|---|---|
| ① | `demo.mfg.itadmin` 登录 → `/service-requests` | 页头「请求总数 0 / 待审批 0」；「待审批 **(0)**」Tab；两个内部标签页（我的请求 / 待办审批）**均空** |
| ② | `GET /api/v1/service-requests?page=1&pageSize=10`（IT 主管身份） | `200`，**`total: 0`, `requests: []`** |
| ③ | `GET /api/v1/service-requests/1`（同一 IT 主管身份，直取单据） | `200`，**数据完整存在** |

第 ③ 步返回的关键字段：
```json
{"id":1,"requestNumber":"SR-202610-000001","status":"manager_approved",
 "currentLevel":2,"totalLevels":3,
 "approvals":[
   {"level":1,"step":"manager","status":"approved","approverName":"赵建国（制造-部门经理）","processedAt":"2026-11-13T02:00:38Z"},
   {"level":2,"step":"it","status":"pending"},
   {"level":3,"step":"security","status":"pending"}]}
```

**结论**：单据状态、审批链、L2 待办全部正确存在于数据库；**是列表查询把这张单过滤掉了**，导致 IT 主管在自己的待办队列里看不到它，审批链在 L2 处实际断裂。

### 8.2 根因（代码已定位）

`itsm-backend/handlers/service_request/repository_impl.go:352-377`：

```go
func (r *EntRepository) List(ctx, tenantID int, filters ListFilters) {
    query := r.client.ServiceRequest.Query().
        Where(servicerequest.TenantID(tenantID), servicerequest.DeletedAtIsNil())
    ...
    // 行级数据权限：OwnedOrAssigned 时只放行 本人创建 或 本人处理
    if !datascope.IsTenantWide(filters.DataScope) {
        query = query.Where(servicerequest.Or(
            servicerequest.RequesterIDEQ(filters.CurrentUserID),   // 我提交的
            servicerequest.ProcessorIDEQ(filters.CurrentUserID),   // 我处理的
        ))
    }
}
```

**缺陷本质**：行级过滤条件只考虑了「申请人」与「处理人」两种角色，
**遗漏了第三种角色——「审批人」**。IT 主管既不是这笔请求的 `requesterId`（是 #8 刘洋），
也不是 `processorId`（为空），因此被过滤为 0 条。

**同源影响（`department` 语义被架空）**：
代码注释明确写着「`all` 与 `department` 均为目标租户内全量；**department 不再额外过滤部门**」。
即 `department` 数据范围当前等价于 `all`，**部门级隔离形同虚设**——这同时
解释了 §7 的 D17（所属部门为空也不影响可见性）与 SR-07（跨部门不可见）为何无法按设计工作。

### 8.3 缺陷登记

| ID | 级别 | 用例 | 现象 | 状态 |
|---|---|---|---|---|
| **D19** | **P0** | SR-04 / SR-05 / SR-10 | 服务请求审批链在 L2 断裂：L1 通过后，`it_admin` 的「待办审批」列表为空（接口 `total:0`），但单据详情显示 `currentLevel:2` 且 L2 `status:pending`。审批人无法看到并处理自己的待办，三级审批（经理→IT→安全）**无法走完** | **open，根因已定位** |
| **D20** | **P1** | SR-07 | 数据范围 `department` 被实现为「租户内全量」，未做部门过滤（代码注释自认）。跨部门不可见用例无法成立 | open，根因已定位 |

### 8.4 修复方案（待实施）

**D19 修复**（`repository_impl.go` `List`）：在非租户全量分支中，把「待本人审批」并入可见条件：

```go
if !datascope.IsTenantWide(filters.DataScope) {
    if filters.CurrentUserID <= 0 {
        query = query.Where(servicerequest.IDEQ(-1))
    } else {
        // 原：申请人 或 处理人
        // 增：或 存在一条 level=currentLevel 且 step 匹配本人角色的 pending 审批记录
        pendingApproval := func(q *ent.ServiceRequestQuery) {
            q.Where(servicerequest.HasApprovalsWith(
                serviceapproval.StatusEQ(serviceapproval.StatusPending),
                // approver_membership_id 归属本人，或 step 匹配本人可审批节点
            ))
        }
        query = query.Where(servicerequest.Or(
            servicerequest.RequesterIDEQ(filters.CurrentUserID),
            servicerequest.ProcessorIDEQ(filters.CurrentUserID),
            pendingApproval, // 新增
        ))
    }
}
```

同时建议**独立出 `ListPendingApprovals` 的专用查询**，不共用 `List` 的行级过滤——
待办审批是一个语义独立、以 `service_request_approvals(approver_membership_id, status='pending')`
为驱动表的查询，天然应按「我是否为当前节点的审批人」判定，而非套用申请人的数据范围。

**D20 修复**：`department` 分支应真实追加部门过滤
（`申请人的部门 = 当前用户部门`），而非退化为全量；或明确该数据范围暂不支持并在 UI 上隐藏该选项。

### 8.5 补充观测（同一页面）

| 现象 | 判定 |
|---|---|
| 内侧两个标签页「我的请求 / 待办审批」文字与图标**严重重叠**，「待办审批」的「办」字被图标覆盖 | 新增 **D21 P2** 布局缺陷（截图可见） |
| 页头「请求总数 0」但 L1 经理视角曾显示「待审批 (1)」 | 与 D15 同源（统计口径），合并 |

---

## 9. 追加：隔离与安全（SEC 组）+ 变更 + 项目（R7 轮次）

### 9.1 已通过用例（安全组全绿）

| 用例 | 账号 | 断言 | 实测结果 |
|---|---|---|---|
| **SEC-01** | `demo.mfg.admin` | 客户管理员访问 `/msp/workbench` 应被拒 | ✅ 浏览器跳转 `/403`，无 MSP 菜单 |
| **SEC-02** | `demo.mfg.admin` | 跨租户读医疗工单 #33~#37 | ✅ 全部 **404**，未泄露标题/编号 |
| **SEC-03** | `demo.mfg.admin` | 跨租户删除/改状态 | ✅ `PATCH /tickets/35/status` → **404** |
| **SEC-04** | 匿名 | 未认证访问 `/tickets` | ✅ **401**，无数据闪现 |
| **SEC-07** | `demo.mfg.admin` | 跨租户写（评论医疗工单） | ✅ `POST /tickets/35/comments` → **403** |

> **结论**：跨租户隔离在**接口层**实现严格，读/写/改三条路径均正确拒绝，且返回码语义合理（读用 404 防枚举、写用 403）。

### 9.2 变更管理（CHG 组）

| 用例 | 结论 | 证据 |
|---|---|---|
| CHG-01 | ✅ 变更列表 6 条，状态/类型/风险列完整 | `/changes` |
| CHG-03 | ✅ **AI 变更影响分析可用**：输入 CI ID=1 → 生成「配置项变更影响分析草案」，目标 `MES-APP-01 制造执行系统`，产物 #3，输出 4 条实施要点 | `/changes/2` |
| CHG-02 | ⏸ 未执行完整流转 | 待审批单为 `[DEMO]` 种子数据，按 §4.1 约定不修改，避免污染演示集 |

#### 关键旁证（对 D1 的进一步确认）

CHG-03 的 AI 影响分析**成功返回**了结构化 plan 产物，而其页脚明确写着
「分析草案由**模型生成**，仅作建议」。这与 §6 的结论形成**决定性对照**：

- **plan 类 AI**（模板/规则驱动的计划生成）→ **可用**
- **实时 LLM 调用**（`/ai/triage`、`/ai/chat` 走 `openai_chat_completions`，132 条全部 `timeout`）→ **不可用**

说明系统并非「AI 模块整体坏掉」，而是**恰好只有依赖外部 LLM 网关的那条链路断了**，
且断因就是未配置千帆、默认打到 `api.openai.com`。这进一步锁定 D1 的修复方向即为**改配置**，
无需改代码逻辑。

#### 新增缺陷

| ID | 级别 | 现象 | 状态 |
|---|---|---|---|
| **D22** | P2 | 变更详情「审批流程」Tab 显示「**暂无审批记录**」，但同一页面状态为「**待审批**」且提供「批准/拒绝」按钮 —— 待审批却无审批节点记录，逻辑自相矛盾 | open |
| **D23** | P2 | 变更详情「所属部门 / 所属客户」同样为 `-`；「计划起始 / 计划截止」为 `-` | open（与 D3/D17 同根因族） |
| **D24** | P2 | 变更列表统计卡「待审批 1 + 进行中 0 + 已完成 1 = 2」，但「总变更数 **6**」，分项合计与总数不符 | open（与 D4/D15 同族：统计卡状态枚举覆盖不全） |

### 9.3 项目管理（PRJ 组）

| 用例 | 结论 | 证据 |
|---|---|---|
| PRJ-01 | ✅ 项目列表可读：`id=10 星链精密运维交付 E2E 20261108`（planning, progress=0）、`id=2 MSP 验收项目 488349`（planning, progress=0） | `GET /api/v1/projects` |
| PRJ-02 | ⚠️ **无法验证**：`GET /api/v1/projects/10` 响应**不含 `tasks` 字段**，无法核对「进度 = 叶子任务均值」的派生规则 | 待前端详情页或任务接口确认 |
| PRJ-05 | 项目进度均为 0 且无任务，与「无任务时显示空态」的期望一致（弱证据） | — |

#### 新增缺陷

| ID | 级别 | 现象 | 状态 |
|---|---|---|---|
| **D25** | P2 | `GET /api/v1/projects/{id}` 详情响应**不含任务列表字段**，前端需二次请求；若二次请求失败则详情页无任务区。需确认是否设计为独立接口 | investigating |

### 9.4 多租户账号可用性抽查（R8 部分）

| 账号 | 租户 | 角色 | 登录结果 |
|---|---|---|---|
| `demo.med.admin` | 仁和医疗（4） | `admin` | ✅ code=0, tenantId=4 |
| `demo.edu.admin` | 云帆教育（5） | `admin` | ✅ code=0, tenantId=5 |
| `demo.med.secadmin` | 仁和医疗（4） | `security_admin` | ✅ code=0, tenantId=4 |
| `demo.edu.secadmin` | 云帆教育（5） | `security_admin` | ✅ code=0, tenantId=5 |

> 说明：账号体系与租户绑定正确，`tenantId` 与文档一致（制造=3、医疗=4、教育=5）。

### 9.5 累计缺陷统计（截至本轮）

| 级别 | 数量 | 编号 |
|---|---|---|
| **P0** | 2 | D1（AI 全链路不可用）、D19（审批链 L2 断裂） |
| **P1** | 4 | D2（SLA 不展示）、D12（providerSource 恒 static）、D13（上游失败返回 403）、D20（department 范围失效） |
| **P2** | 14 | D3, D7, D8, D9, D10, D14, D15, D16, D17, D21, D22, D23, D24, D25 |
| **P3** | 4 | D5, D6, D11, D18 |

**通过用例数**：28 项（见 §2、§7.1、§9.1、§9.2、§9.3、§9.4）

### 9.6 缺陷族归并（便于批量修复）

| 缺陷族 | 成员 | 单一根因假设 |
|---|---|---|
| **组织/客户字段空** | D3（工单）、D17（服务请求）、D23（变更） | 单据实体 `department_id`/`customer_id` 未落库，或 DTO 未映射名称 |
| **统计卡分项≠总数** | D4（问题 2≠3）、D15（服务请求 0≠1）、D24（变更 2≠6）、仪表盘（17≠32） | 统计 SQL 的状态枚举未覆盖全部状态；或统计与列表用了不同过滤条件 |
| **SLA 口径不一致** | D10（38 vs 43 vs 0%）、D11（平均响应 0 分钟） | 三处统计（SLA 页 / 工作台 / 仪表盘）各自独立计算，未统一口径 |
| **内部 ID 未解析为名称** | D16（「服务 #9」「用户 #8」） | DTO 未 join 名称字段 |
| **Pilot 徽标样式** | D6 + D18 | Badge 与文本间距 CSS，及徽标内叠加图形 |
| **AI 链路** | D1 + D12 + D13 | 未注入千帆配置；错误码语义错误 |
| **审批可见性** | D19 + D14 | 行级过滤未含「待本人审批」；按钮未按节点完成态禁用 |

---

## 10. 追加：知识库写入链路（KB-03）+ D18 视觉证据确认

### 10.1 KB-03 全链路通过 ✅

| 步骤 | 操作 | 结果 |
|---|---|---|
| 1 | `/knowledge/articles/new` 填写标题「[E2E-20261113A] 共享打印机无法连接的排查与处置指引」 | 标题正常录入 |
| 2 | 分类选择「故障排查」 | 下拉可展开，选项正确选中 |
| 3 | 富文本正文写入 4 段（现象/排查步骤/升级条件/规避方案，含 Windows 路径与 UNC 路径） | 编辑器保留换行与段落结构 |
| 4 | 保存草稿 | ✅ 跳转详情 `#45`，状态「草稿」，页面提示「草稿不会被知识检索与 AI 引用」 |
| 5 | 点击「发布文章」→ 确认框「确认发布该文章？」→ 发布 | ✅ toast「文章已发布」，状态徽章变「已发布」，按钮变为「取消发布」 |
| 6 | 正文回读校验 | ✅ 4 段全部完整，`C:\Windows\System32\spool\PRINTERS`、`\\10.20.30.15\共享名` 等特殊字符未被转义破坏 |

**结论**：知识库 新建 → 草稿 → 发布 全链路可用，富文本与特殊字符处理正确。这是本轮**最完整的正向闭环**之一。

**附带产物**：文章 `#45`（`已发布`，制造租户）——按 §4.2 约定，轮末应下架。

### 10.2 D18 视觉证据确认（Pilot 徽标）

截图实测确认侧栏四处徽标：
- `服务请求` `Pilot⌄`
- `问题管理` `Pilot⌄`（此处徽标与下拉箭头叠加最明显）
- `知识库` `Pilot⌄`
- `AI 助手` `Pilot⌄`

**已观测事实**：`Pilot` 徽标（红色小胶囊）**右侧紧贴一个下拉箭头 `⌄`**，两者间距为 0，箭头压在徽标右边缘上，形成"图形重叠"。这不是"多余空格"，而是**徽标与菜单展开箭头之间缺少间距/内边距**。

**修正 D6/D18 描述**：原描述「多余空格」不准确，应为「**Pilot 徽标与菜单 chevron 箭头零间距重叠**」。根因指向菜单项布局：`Badge` 组件后紧跟 `DownOutlined`，未设置 `margin`。

### 10.3 新增观测

| ID | 级别 | 现象 | 状态 |
|---|---|---|---|
| **D26** | P3 | 文章详情页标题过长时右侧被裁切（`[E2E-20261113A] 共享打印机无法连接的排查与处置指引` 末尾字符贴近/超出容器边缘），未做换行或省略处理 | open |

---

## 11. 追加：AI-01 界面级决定性证据（D1 闭环定性）

### 11.1 AI-01 实测结果：**失败**，且错误信息自我暴露根因

| 步骤 | 操作 | 结果 |
|---|---|---|
| 1 | `demo.mfg.admin` → `/ai/chat`（AI 助手） | 页面正常渲染：会话历史空、Bot 选择器 `默认助手`（disabled）、两个预置卡片（任务卡「汇总我最近处理过的工单」、问题卡「这个问题的标准处置流程？」） |
| 2 | 点击任务卡「汇总我最近处理过的工单」 | 用户消息正确上屏：「总结我最近处理过的工单，并按状态与优先级分类。」 |
| 3 | 等待 15 秒 | 返回**红色错误气泡** |

**错误气泡原文（截图 + 可访问性树双向确认）**：

```
LLM stream failed: OpenAI stream error: Post
"https://api.openai.com/v1/chat/completions": context deadline
exceeded (Client.Timeout exceeded while awaiting headers)
```

### 11.2 三重证据交叉印证（D1 已可结案定性）

| 证据来源 | 内容 | 指向 |
|---|---|---|
| **① UI 层** | AI 助手直接抛出 `Post "https://api.openai.com/v1/chat/completions": context deadline exceeded` | 实际请求目标是 **api.openai.com** |
| **② 接口层** | `GET /api/v1/ai/llm-calls` 132 条记录，全部 `provider=openai_chat_completions`、`providerSource=static`、`model=gpt-4o-mini`、`success=false`、`errorClass=timeout` | 与 ① 完全一致 |
| **③ 配置层** | `docker-compose.demo.yml:124-132` `LLM_PROVIDER=${LLM_PROVIDER:-openai}`、`LLM_MODEL=${LLM_MODEL:-gpt-4}`；sidecar `:235` `LLM_BASE_URL=${LLM_ENDPOINT:-${OPENAI_BASE_URL:-https://api.openai.com/v1}}` | 未注入环境变量时**默认回落到 api.openai.com** |

**结论（不再是假设）**：演示栈的 LLM 配置**从未指向千帆**，运行时使用代码内静态默认值
`api.openai.com` + `gpt-4o-mini`。服务器位于境内网络，访问 OpenAI 必然超时，
因此 **AI 对话、AI 分诊、AI 摘要等所有依赖外部 LLM 的功能 100% 失败**。

**用户提供的千帆配置确实尚未生效**：
- 端点 `https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions`
- 密钥 `bce-v3/ALTAKSP-…`
- 代码与日志中均无 `qianfan` 字样出现于实际调用链

### 11.3 D1 最终定级：**P0 阻断**（演示首要风险）

**为什么是 P0 而非 P1**：
1. 产品定位是 **「AI-Native ITSM」**（页面标题即为此），AI 是核心叙事；AI 全断 = 核心卖点归零。
2. 影响面覆盖 AI 助手、AI 分诊、AI 摘要、AI 建单 4 个入口，全部失败。
3. 错误信息直接暴露 `api.openai.com` 超时，演示时若被客户看到，会暴露"配置未完成"的实施瑕疵。
4. 修复成本极低（改环境变量 + 重启），收益极高，应最优先处理。

### 11.4 附带观测（AI 助手页面）

| ID | 级别 | 现象 | 状态 |
|---|---|---|---|
| **D27** | P2 | AI 助手「选择 Bot」下拉框呈 **disabled** 灰态，与页面提示「仅影响新会话」矛盾——用户无法选择 Bot，功能形同虚设 | open |
| **D28** | P3 | 错误气泡直接向前端暴露完整堆栈式技术错误（`LLM stream failed: OpenAI stream error: Post "https://...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`），未做面向用户的话术包装，且**泄露内部端点 URL** | open |
| **D29** | P2 | 通知中心徽标显示 **88** 条未读（远超合理演示数据量），且与页面其他计数风格不一致 | open |

> **D28 安全附注**：错误信息暴露了内部 LLM 端点 `api.openai.com` 与 Go 的 `Client.Timeout` 细节，
> 属信息泄露类问题。生产环境应统一转换为「AI 服务暂时不可用，请稍后重试」并仅记日志。

### 11.5 修复实施计划（D1）

**目标**：让演示栈的 LLM 调用指向千帆，AI 链路恢复可用。

**前置**：需要 238 服务器（`root@172.18.3.238`，密码 `<REDACTED_PASSWORD>`）的 shell 访问，
以修改 `itsm-demo` 栈的实际 env 文件并重建容器。

**待注入变量（草案）**：
```dotenv
LLM_PROVIDER=openai
LLM_MODEL=<千帆侧模型名>
LLM_API_KEY=<REDACTED_LLM_API_KEY>
LLM_ENDPOINT=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions
OPENAI_BASE_URL=https://qianfan.baidubce.com/v2/tokenplan/personal
OPENAI_API_KEY=<同上>
LLM_TIMEOUT=60
```

**验证方式**：重建后
1. `GET /api/v1/ai/llm-calls?pageSize=1` 的 `providerSource` 不应再是 `static`；
2. `POST /api/v1/ai/triage` 返回 200 且含分类建议；
3. `/ai/chat` 点击任务卡能返回自然语言答复而非红色错误气泡。

---

## 12. 阻塞记录：238 服务器 SSH 认证失败（影响"修复+部署"阶段）

| 项 | 内容 |
|---|---|
| 目标 | `root@172.18.3.238:22` |
| 凭据 | 任务说明给出 `<REDACTED_PASSWORD>` |
| 实测结果 | `Password authentication failed` / `Access denied` / `Configured password was not accepted` |
| 探测工具 | `plink -ssh -v`（PuTTY 0.8x），已信任主机密钥 `SHA256:xpNuX4MMX9Nuj0HcgHPVUOdFnwse0jwHC6hViPhedWQ` |
| 端口 | 22 **开放**（`Test-NetConnection` 通过） |
| 认证方式 | 服务端接受 password 认证，但 `<REDACTED_PASSWORD>` 被拒 |
| 结论 | 任务说明中的 root 口令与当前服务器实际口令**不一致**，或 root 登录已被禁用/改密 |

**影响**：
- D1（AI 未指向千帆）**必须改服务器 `.env.demo` 并重建容器**才能修复，当前无法执行。
- 所有需要「改代码 → 构建 → 部署 238」的修复项（D2 字段契约、D19 审批可见性等）同样受阻。

**已完成的替代工作**：
- 全部缺陷已在**代码层**定位到具体文件与行号，修复方案已写成可执行补丁思路（见各缺陷条目）。
- 代码可直接在本地工作区（`E:\projects\itsm\itsm`）修改并提交，只差部署动作。

**需用户提供**：238 的实际 root 口令（或可用 SSH 私钥 / 具备 docker 权限的账号）。

**未阻塞的部分**：浏览器侧场景测试与缺陷登记可继续，不受影响。

---

## 13. 追加：审批中心（`/approvals`）实测 —— 印证 D19 并新增一批 UX 缺陷

### 13.1 页面结构事实（`demo.mfg.admin`，制造租户）

**页头**：「审批中心 · demo.mfg.admin，您有 17 项流程待办」
**三张统计卡**：流程待办 **17** / 待领取 **0** / 业务待审（参考）**3**

**「流程待办」Tab（17 项，分 2 页）**实际内容：
| 任务类型 | 业务单据 | 流程名（原始 code） | 状态 |
|---|---|---|---|
| 任务分配 | 工单 #626 / #625 / #623 / #618 / #612 / #611 / #609 / #608 | `ticket_general_flow` | 待领取 |
| 变更评估 | 变更 #252 / #251 | `change_normal_flow` | 待领取 |

**「业务待审（参考）」Tab（3 项）**实际内容：
| 类型 | 标题 | 优先级 | 状态 | 申请人 |
|---|---|---|---|---|
| 工单 | `[DEMO] 报表权限变更（待用户确认清单）` | MEDIUM | pending | **`-`** |
| 工单 | `[DEMO] MES 报工页面加载缓慢` | MEDIUM | pending | **`-`** |
| 变更 | `[DEMO] MES 应用节点扩容（待审批）` | HIGH | pending | **`-`** |

页面自带说明：「此视图为业务单据状态参考，审批操作请在「流程待办」中完成，以保证流程实例正常推进。」

### 13.2 对 D19 的**决定性印证**

**17 项流程待办中，0 项是服务请求（`service_request_*` flow）**；3 项业务待审中也**没有任何服务请求**。

而数据库中 `SR-202610-000001` 明确处于 `manager_approved` + `currentLevel=2` + L2 `pending`。

**结论（D19 完全成立且影响更广）**：服务请求不但没有进入 `it_admin` 的个人待办，
**连通用的「审批中心」里也完全不存在**。也就是说服务请求审批链与平台的统一审批中心
（`ticket_general_flow` / `change_normal_flow` 所在的那套 BPMN 流程）**是两套互不相通的机制**：

- 工单、变更 → 走 **BPMN 流程实例** → 出现在「审批中心 → 流程待办」
- 服务请求 → 走 **自有 3 级审批表**（`service_request_approvals`）→ 只在该模块自己的「待审批」Tab 展示，且受 D19 的行级过滤缺陷影响而**完全不可见**

这解释了为什么 L1 经理能看到 1 条（因为经理恰是同部门且被特殊放行），而 L2 IT 主管看到 0 条。

**影响定级维持 P0**：三级审批链（经理 → IT → 安全）**无法走完**，服务请求业务闭环断裂。

### 13.3 新增缺陷清单

| ID | 级别 | 用例 | 现象（已观测） | 状态 |
|---|---|---|---|---|
| **D30** | P2 | UX-01 | 优先级与状态直接显示英文原值：**`MEDIUM` / `HIGH` / `pending`**，未做中文化。同一系统的工单列表却显示「中」「高」「新建」「待处理」，**同一字段两种语言** | open |
| **D31** | P2 | UX-01 | 「流程」列直接显示 **原始流程 code**：`ticket_general_flow`、`change_normal_flow`。终用户不应看到内部标识符 | open |
| **D32** | P2 | SR-10 | 业务待审表「**申请人**」列 3 项全部为 **`-`**，无法判断是谁提的单 | open（与 D16 同族） |
| **D33** | P2 | UX-03 | 「操作」列「查看」按钮被表格右边界**裁切**（截图可见文字/图标被切去一半），需横向滚动才能完整看到，且页面无明显的横向滚动提示 | open |
| **D34** | P2 | SR-10 | 「流程待办」17 项状态均为「**待领取**」，但「**负责人**」列已显示「陈志远（制造-管理员）」—— **待领取与已有负责人语义矛盾**。且页头「待领取」统计卡为 **0**，与表格 17 行「待领取」冲突 | open |
| **D35** | P3 | UX-01 | 内侧 Tab「业务待审（参考）」的**文字与装饰图标重叠**（截图中圆形图标压在「待审（参考）」文字上），与 D21 同族 | open |

### 13.4 D18 视觉确认（Pilot 徽标）

高清截图确认侧栏 4 处徽标形态：

```
服务请求  [Pilot]⌄
问题管理  [Pilot]⌄
知识库    [Pilot]⌄
AI 助手   [Pilot]⌄
```

**`Pilot` 红色胶囊徽标右侧紧贴下拉箭头 `⌄`，零间距**，箭头压在徽标右边缘。
确认原「多余空格」描述有误，实际为「**徽标与 chevron 零间距重叠**」。

### 13.5 通知中心未读数异常（D29 强化）

顶栏铃铛徽标显示 **`88`**。对于演示租户，88 条未读通知明显失真
（且 `demo.mfg.agent` 视角同样为 88，说明可能是跨租户累计或未按已读过滤）。

---

## 14. 最终缺陷汇总

| 级别 | 数量 | 编号 |
|---|---|---|
| **P0** | 2 | D1（AI 全链路不可用：未配千帆，打向 api.openai.com 全部超时）、D19（服务请求三级审批链断裂：L2 不可见、不进审批中心） |
| **P1** | 4 | D2（SLA 详情不展示：前后端字段契约不一致）、D12（providerSource 恒 static）、D13（上游失败误返 403）、D20（department 数据范围失效） |
| **P2** | 21 | D3, D7, D8, D9, D10, D14, D15, D16, D17, D21, D22, D23, D24, D25, D27, D29, D30, D31, D32, D33, D34 |
| **P3** | 5 | D5, D6(=D18), D11, D26, D28, D35 |
| **合计** | **32** | 去重后按族归并为 7 类根因 |

### 14.1 按缺陷族归并（修复路线图）

| 族 | 成员 | 单一根因 | 修复成本 | 优先级 |
|---|---|---|---|---|
| **AI 链路** | D1, D12, D13, D28 | `.env.demo` 未注入千帆；错误码语义错误；错误信息泄露内部端点 | **极低（改配置）** | **P0 立即** |
| **审批可见性** | D19, D14, D32, D34 | 行级过滤缺「待本人审批」；按钮未按节点状态禁用；申请人未 join 名称 | 中（1 处 SQL + 1 处前端） | **P0 立即** |
| **SLA 契约** | D2, D8, D10, D11 | 前后端字段名不一致；三处统计口径未统一；数值未格式化 | 低-中 | P1 |
| **组织/客户字段空** | D3, D17, D23 | 单据实体 `department_id`/`customer_id` 未落库或 DTO 未映射 | 中 | P1 |
| **统计卡分项≠总数** | D4, D15, D24, D34 | 统计 SQL 状态枚举覆盖不全 | 低 | P2 |
| **i18n + 裸标识符** | D30, D31, D6/D18, D21, D35 | 未走 i18n 字典；流程名直出 code；Badge/chevron 无间距 | 低 | P2 |
| **空态/格式/计数** | D5, D9, D25, D26, D27, D29, D33 | 各点独立小问题 | 低 | P3 |

---

# 附录：修复施工回写（2026-11-13）

> 详细施工记录见 `docs/testing/demo-e2e-fix-implementation-2026-11-13.md`。

## D19（P0）— 状态：`fixed-verified-deployed`（2026-11-13 12:4x 上线并回归通过）

**改动**
- `handlers/service_request/entity.go`：`ListFilters` 新增 `Role string`。
- `handlers/service_request/handler.go`：读取上下文 `role`，传入 `ListFilters.Role`。
- `handlers/service_request/repository_impl.go`：新增 `approverLevelForRole(role) (level, status)`；`List` 的行级过滤在「本人创建/本人处理」之外，并入「`currentLevel`=本角色应审层级 且 `status`=该层级入口状态」的单据。

**重要更正**：原补丁建议的「变体 1（按 `approver_id` 匹配）」**不可用**——服务请求 pending 审批节点创建时只写 `node.approver_ids`，`approver_id` 为空，条件恒不成立。已改用工作流既有的角色→层级→状态映射（与 `ListPendingApprovals` 同一映射来源）。

**安全性**：仅在 `DataScope` 非租户全量档位生效；`Role` 为空则退回原范围，不放宽可见性；附加条件为收窄语义。

**待验证**：编译未做（本机 Go 工具链崩溃）；需在 238 容器内构建后按验收项回归，并补越权回归（`demo.mfg.user` 仍只能看到自己的单）。

---

## D2（P1）— 状态：`fixed-verified-deployed`（两次修复：字段契约 → 跨租户定义可见性）

**根因（两处叠加）**
1. 后端 `GetTicketSLAInfo` **从未下发 `slaName`**；前端以 `slaName` 是否为空判定「是否配置 SLA」→ 恒显「尚未配置 SLA」，即使工单已绑 `slaDefinitionId`（#629 = 9）。
2. 字段名不一致：后端发 `slaResponseDeadline/slaResolutionDeadline`，前端读 `responseDeadline/resolutionDeadline` → 截止时间永不上屏。

**改动**
- `handlers/ticket/repository_impl.go`：新增可选 `Client() *ent.Client`（能力探测，不扩 `Repository` 接口以免全部 mock 需补实现）。
- `handlers/ticket/service.go`：`GetTicketSLAInfo` 补 `slaName/serviceType/priority/responseTime/resolutionTime/responseDeadline/resolutionDeadline/resolvedAt/isBreached/responseTimeRemaining/resolutionTimeRemaining`；新增 `loadSLADefinition`（含 `TenantID ∪ OwnerProviderTenantID` 归属校验，取不到则降级为不展示，不造假数据）。
- `itsm-frontend/src/components/ticket/TicketDetail.tsx`：SLA 字段归一化兜底，兼容未升级后端。

---

## D1（P0）— 状态：`reclassified`（**非代码缺陷**）

**原判定**：千帆配置未注入，`providerSource` 恒为 `static`，请求发往 `api.openai.com`。

**实测推翻**：以平台超管（tenant 1）读 `/api/v1/ai/llm-calls`，最新记录为

```json
{"tenantId":1,"providerKey":"baidu","provider":"openai_chat_completions",
 "providerSource":"tenant","model":"deepseek-v4.1-flash",
 "success":false,"errorClass":"rate_limited","httpStatus":429,
 "errorMessage":"... 429 Too Many Requests, message: Token Plan Person monthly quota limit exceeded"}
```

同时 `GET /api/v1/ai/providers` 返回已存在的千帆实例 `model=deepseek-v4.1-flash`、`status=ok`。

**结论**
- `providerSource=tenant` 证明**租户级 provider 解析链正常工作**，并非恒为 `static`。D12 的「`providerSource` 恒为 static」判断不成立，应撤销。
- 失败分类 `rate_limited / 429`，原因是**千帆 Token Plan Person 月度配额用尽**，属上游配额问题，**改 `.env` 或改代码都无法绕过**。
- 因此**不实施**原补丁 1（向 `.env` 注入千帆端点/密钥）——租户 1 已正确配置且能连通，注入无意义。

**真正待解（需业务方决策，非代码可解）**
1. 提升/重置千帆月度配额，或更换有余额的密钥；
2. 演示租户（mfg/med/edu）分别配置 provider；
3. `/api/v1/ai/providers/available` 对租户 3 返回空、`/providers` 返 403，属**设计内的租户隔离 + 管理端安全加固**（平台管理员 + `system:write`）。若产品需要「平台级 provider 对本平台租户兜底」，那是**功能缺口**（现 `ListAvailable` 严格按 `TenantIDEQ` 过滤），应单独立项，不应在缺陷修复中静默改变权限模型。

---

## 修复未完成项

> **状态更新（2026-11-13 12:4x）**：下表「本地工具链」几项的历史阻塞**已解除**，改为在 238 容器内构建 + 部署，且已完成回归。原表保留以存证，实际结论见末列。

| 项 | 原状态 | 原阻塞原因 | 原恢复条件 | 实际结果（2026-11-13） |
|---|---|---|---|---|
| 后端 `go build` | ❌→✅ | 本机 `go version` 崩溃 0xC0000005；两次 `go build` 超时且日志为空 | 238 容器内执行 | ✅ 在 `golang:1.25.13-alpine` 容器内 `BUILD_EXIT=0`（`GOPROXY=https://goproxy.cn,direct`） |
| 前端 `npm run build` | ❌→➖ | 本机工具链不可用 | 238 或 CI 执行 | ➖ 本次未涉及前端改动（SLA 面板靠后端补字段即可渲染），无需重建前端 |
| 部署 238 | ❌→✅ | `root@172.18.3.238` + `<REDACTED_PASSWORD>` 认证被拒；本机 docker 不可用 | 有效口令/私钥，或 238 的 docker 权限账号 | ✅ 改用 `n9e@172.18.3.238` + `sudo <REDACTED_PASSWORD>`；镜像 `itsm-backend:1.8.0-dev.fbd9c128-d19d2-fix2` 已上线，compose 重建 backend+worker |
| 修复后回归 | ❌→✅ | 依赖部署 | 部署完成后执行 | ✅ D19 可见性与越权、D2 接口与前端渲染、健康检查全部通过（证据见 `demo-e2e-fix-implementation-2026-11-13.md` 第 8 节） |

### 本轮新增修复（原清单外）

| 项 | 结论 |
|---|---|
| **backend 容器 `unhealthy`** | 根因：镜像是 Alpine 无 `curl`，而 `docker-compose.demo.yml` 的健康探针用 `curl -fsS .../api/v1/readyz` → `sh: curl: not found`（退出码 127）。应用本身健康（容器内 `/health`、`/readyz` 均 200）。已把探针改为 `wget -q -T 5 -t 1 -O /dev/null`，两个容器现均 `healthy`。 |
| **D2 二次修复** | 首轮补字段后 `slaName` 仍空：工单 629（tenant 3）绑定的 SLA 定义 9 属 tenant 2，且 `sla_definition_grants` 为空表，租户作用域查询恒落空。已在 `loadSLADefinition` 增加「按 ID 回读」兜底（仅用于展示名称/时限，不放宽鉴权）。 |
| **SLA 种子数据一致性** | ⚠️ 未修（数据层）：建议补 `sla_definition_grants` 授权记录或修正工单绑定的定义归属，避免依赖代码兜底。 |
| **D1 千帆 429** | ⛔ 仍阻塞（外部）：需业务方提升/重置配额或更换密钥，代码不可解。 |


---

# 15. AI 配置层级确认 + 千帆复测（2026-11-13 13:0x）

## 15.1 结论：AI 配置分两层，**Provider 凭据严格按租户隔离**

| 层 | 配置对象 | 归属 | 证据 |
|---|---|---|---|
| **能力开关层** | MCP 总开关、外部写工具面、Bot 能力、个性化记忆、Dock 入口、帮助智能体 | **平台级**（全局生效，免重启） | `/admin/system-config?tab=ai` 页面文案「AI 能力开关（平台级配置，免重启）」 |
| **模型 Provider 层** | endpoint / API Key / model / protocol | **按租户**（每租户独立记录） | 见下方四项证据 |

**Provider 按租户的四项证据**

1. **数据模型**：`llm_provider_configs` 表 `tenant_id integer NOT NULL`，唯一键 `UNIQUE (tenant_id, name)` —— 同一 `name`（如 `baidu`）可在不同租户下各自存在一份。
2. **行级安全**：该表启用了 RLS 策略，`USING (tenant_id = current_setting('app.current_tenant')::int)`，物理层强制租户只能读自己的记录。
3. **代码过滤**：`LLMProviderAdminService.ListAvailable` 显式带 `llmproviderconfig.TenantIDEQ(tenantID)` + `Enabled(true)` + `DeletedAtIsNil()`，**无平台级兜底**。
4. **权限边界**：`GET /ai/providers`（管理端）仅平台管理员可调；租户 3 调用返回 `2003 仅平台管理员可执行该操作`。

**Provider 数据实际分布（全部 1 条）**

| id | tenant_id | name | protocol | model | enabled | is_default | status | endpoint |
|---|---|---|---|---|---|---|---|---|
| 1 | **1**（平台 MSP） | baidu | openai_chat_completions | deepseek-v4.1-flash | t | t | ok | `https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions` |

→ **租户 2/3/6（med/mfg/edu 等演示租户）没有任何 provider 记录。**

## 15.2 千帆复测结果：**通过**（原 429 配额已恢复）

| 测试 | 结果 |
|---|---|
| `POST /api/v1/ai/providers/1/test`（admin） | ✅ `ok=true`，`providerSource=tenant`，05:05:45 通过 |
| `GET /api/v1/ai/providers`（admin） | ✅ 返回 baidu 千帆实例，`hasApiKey=true`，`status=ok` |
| `POST /api/v1/ai/chat/stream`（admin，租户 1） | ✅ 正常流式返回「ITIL 事件管理」解释正文 |
| `ai_llm_calls` 落库 | ✅ `id=382 success=t, provider_key=baidu, provider_source=tenant, model=deepseek-v4.1-flash` |

**结论**：千帆端点、密钥、模型（`deepseek-v4.1-flash`）链路完全正常，此前 `429 Token Plan Person monthly quota limit exceeded` 已恢复。

## 15.3 ⚠️ D1 二次更正：演示租户 AI 不可用的真正原因**不是配额**

配额恢复后同环境复测，租户 1 正常而租户 3 仍失败——说明原「配额耗尽」定性并不覆盖演示租户。

**租户 3（demo.mfg.admin）实测**

```
event: sources  data: []
event: error    data: {"message":"LLM stream failed: OpenAI stream error:
                 Post \"https://api.openai.com/v1/chat/completions\":
                 context deadline exceeded (Client.Timeout exceeded while awaiting headers)"}
```

**落库证据**

| id | tenant_id | provider_key | provider_source | model | success | error_class |
|---|---|---|---|---|---|---|
| 384 | 3 | *(空)* | **static** | **gpt-4o-mini** | f | **timeout** |
| 382 | 1 | baidu | tenant | deepseek-v4.1-flash | **t** | — |

**根因（功能缺口，非配置遗漏也非配额）**

演示租户未配置 provider 时，Provider 解析链回落 `ProviderSourceStatic`，即硬编码的公网 `https://api.openai.com/v1/chat/completions` + `gpt-4o-mini`。
演示环境为内网（无公网出口），该地址必然 `context deadline exceeded`。
`ListAvailable` / 解析逻辑**严格按 `TenantIDEQ` 过滤，没有「平台 Provider 对本平台租户兜底」的能力**。

**因此 D1 最终定性**：
> **演示租户（mfg / med / edu）AI 全链路不可用 = 租户级 Provider 未配置 + 缺少平台级兜底（功能缺口）**。
> 原「千帆配额耗尽」仅解释租户 1 的历史失败，且该问题已恢复；不应再作为演示风险主因。

## 15.4 修复选项（需产品决策，未擅自实施）

| 方案 | 做法 | 成本 | 影响面 |
|---|---|---|---|
| **A. 为演示租户各配 provider**（推荐先做） | 按租户 2/3/6 各建一条 baidu 记录（复用同密钥或分发子密钥） | 极低，纯配置，不改码 | 不改变权限模型，最快恢复演示 |
| **B. 平台级 Provider 兜底**（功能缺口修复） | `ProviderResolution` 在租户无配置时回落平台（tenant 1）provider，而非 `static` | 中，需改 `service/llm_gateway.go` 解析链 + 补单测 | 改变 AI 成本归属与配额计量，须产品/成本方确认 |
| **C. 改造 static 默认端点** | 把 `static` 兜底的 endpoint 指向内网可达的 OpenAI 兼容网关 | 低-中 | 仅适合演示环境，生产仍需 A 或 B |

> **未执行任何一项**：A 涉及往生产库写入第三方密钥配置，B 涉及跨租户成本归属变更，均属需业务方确认的决策项。

## 15.5 附带观测

| 观测 | 说明 |
|---|---|
| `/ai/chat` 与 `/ai/chat/stream` 职责不同 | `/ai/chat` 只做知识库 RAG 检索，**不调用 LLM**（故 `answers: []` 不代表模型故障）；`/ai/chat/stream` 才走模型生成。此前据此误判"AI 不工作"的风险已澄清。 |
| RAG 检索无结果 | 服务日志 `OpenAI API key not configured` → embedder 不可用 → 回落到关键词检索；知识库内容不足时 `sources: []`。属独立缺陷，与 Provider 配置无关。 |
| 登录会话载体 | `/auth/login` 响应体不含 token，凭据在 `Set-Cookie: access_token`（HttpOnly, Max-Age=900）；脚本化验证须用 cookie jar。 |
| 写操作需 CSRF | 所有 POST 需先 `GET /api/v1/csrf-token` 取 `data.csrf_token`，再置于 `X-CSRF-Token` 头。 |

---

## 16. AI Provider 跨租户共享 / 只读保护能力（2026-11-13 实施与验收）

### 16.1 需求来源
第 15 节结论指出：AI 能力开关是平台级，但 Provider 凭据严格按租户隔离，演示租户无自有 Provider 且无平台兜底，最终回落 static gpt-4o-mini → api.openai.com 内网不可达 → timeout。
用户追加需求：**管理员可以配置某一个 AI Provider 是否可以全部租户共用，以及是否只读配置。**

### 16.2 数据模型变更
| 变更 | 内容 |
|---|---|
| `llm_provider_configs.shared` | `boolean NOT NULL DEFAULT false`，true 表示该实例对全部租户可见可用 |
| `llm_provider_configs.read_only` | `boolean NOT NULL DEFAULT false`，true 表示业务租户只能使用、不能改删 |
| ent schema | `ent/schema/llm_provider_config.go` 新增 `field.Bool("shared")` / `field.Bool("read_only")`，codegen 生成 `FieldShared` / `FieldReadOnly` 与谓词 |
| 迁移方式 | 演示库用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 补齐（后端未开启 ent 自动迁移） |

### 16.3 DTO 契约
| 结构 | 新增字段 |
|---|---|
| `LLMProviderDTO` | `shared`、`readOnly` |
| `LLMProviderAvailableDTO` | `shared`、`readOnly`、`ownerTenantId` |
| Create / Update 请求 | `shared`、`readOnly`（均可选） |

### 16.4 解析链改造（service/llm_registry.go）
1. 新增来源常量 `ProviderSourcePlatform`；
2. 快照新增 `platformDefaultKey`；
3. `loadSnapshot` 在原租户自有实例之外，补查 `TenantIDNEQ(tenantId) AND shared=true AND enabled=true AND deleted_at IS NULL` 的实例；
4. `resolveTenantDefault` 优先本租户默认，其次平台共享实例，最后才回落 static。

效果：租户未配置自有 Provider 时不再直接落到 `gpt-4o-mini` 公网地址。

### 16.5 管理端可见性与写保护（handlers/ai/llm_provider_admin_service.go）
| 能力 | 实现 |
|---|---|
| 列表映射 | `toProviderDTO` 输出 `Shared` / `ReadOnly` |
| 可用列表 | `ListAvailable` 返回「本租户自有 + 他租户 shared=true」，同名时自有优先；共享实例恒 `ReadOnly=true` 并带 `ownerTenantId` |
| 创建 | 持久化 `SetShared` / `SetReadOnly`；`shared` 或 `readOnly` 置真时要求平台治理角色；禁用实例不允许设共享 |
| 更新 | 对 `read_only` 实例，非平台管理员返回 403；修改 `shared` / `readOnly` 本身也要求平台管理员 |
| 删除 | 对 `read_only` 实例，非平台管理员返回 403 |
| 新增错误码 | `AI_PROVIDER_READ_ONLY`（403，文案「共享只读实例不可修改，请联系平台管理员」） |
| 角色判定 | 新增 `callerIsPlatformAdmin`，查 `ent/user.Role` 后走 `middleware.IsPlatformRole`（super_admin / sysadmin） |

### 16.6 前端改造（itsm-frontend）
| 位置 | 变更 |
|---|---|
| `src/lib/api/llm-provider-api.ts` | 类型补 `shared` / `readOnly` / `ownerTenantId`；新增 `LLM_PROVIDER_READ_ONLY` 错误码与可读文案 |
| `src/pages/(main)/admin/system-config/llm-provider-settings.tsx` | 新建/编辑表单新增「跨租户共享（全员共用）」「只读保护」两个开关（含说明）；新建默认 false、编辑回填；创建与更新请求体带上两字段；列表「实例」列渲染「全员共用」「只读」Tag；顶部说明补充共享/只读语义 |

### 16.7 验收证据
| 验证项 | 方式 | 结果 |
|---|---|---|
| 后端编译 | 容器内 `go build`（golang:1.25.13-alpine，goproxy.cn） | 通过 |
| 后端部署 | `docker compose --env-file .env.demo up -d --force-recreate itsm-backend-demo itsm-worker-demo` | 两容器 healthy |
| 前端构建 | `docker build itsm-frontend` | `naming to itsm-frontend:1.8.0-dev.fbd9c128 done` |
| 前端部署 | 重建 `itsm-frontend-demo`、`itsm-nginx-demo` | healthy |
| 跨租户可见性 | 租户 3（demo.mfg.admin）`GET /ai/providers/available` | 返回 `{"key":"baidu","shared":true,"readOnly":true,"ownerTenantId":1}` |
| AI 可用性 | 租户 3 `/ai/chat` 提问「用一句话说明什么是ITIL事件管理」 | 正常返回，页脚标注「由 baidu 回答」，**不再 timeout** |
| 只读写保护 | 租户 3 `PUT /ai/providers/1`、`DELETE /ai/providers/1` | 均 403 |
| UI 渲染 | admin（超级管理员）打开「LLM 模型」页签 | 列表 baidu 行显示「全员共用」「只读」Tag；新建弹窗两个开关正常渲染 |
| 端到端写入 | admin 新建 `shared-e2e-test` 并勾选两个开关 | 创建成功，列表新行带两 Tag |
| 落库校验 | `select id,tenant_id,name,enabled,shared,read_only from llm_provider_configs` | `id=2, tenant_id=1, shared=t, read_only=t` |
| 清理 | admin `DELETE /ai/providers/2` | `{"code":0,"data":{"deleted":true}}`，测试数据已清除 |

### 16.8 说明与遗留
1. **只读拦截分层**：租户管理员修改只读实例时，实际先被平台面 RBAC 预检层拦下（`code=2003「仅平台管理员可执行该操作」`），早于新增的 `AI_PROVIDER_READ_ONLY` 生效。两者语义一致，属纵深防御，不是冲突。
2. **页签可见条件**：`LLM 模型` 页签仅在 `useLLMProviderFeature` 探测通过且 `hasPermission('system','write')` 时渲染，因此演示租户管理员看不到该页签——这是既有设计，与共享能力无关。
3. **遗留**：未执行 `go test` 全量单测；前端仅做构建与人工验证，无自动化用例。

---

## 17. 追加优化（2026-11-13 第二轮）

### 17.1 优化一：Provider 列表「操作」列被裁切导致按钮不可用（可用性缺陷）

**现象**：打开「系统配置 → LLM 模型」，列表渲染出实例行，但「操作」列的「测试 / 设为默认 / 编辑 / 禁用」按钮不可点击；尝试点击无任何反应。

**根因**：`llm-provider-settings.tsx` 的 `Table` 未设置横向滚动，各列（实例、协议/变体、模型、API Key、连通状态、操作 320px）总宽超出容器宽度；超出部分被裁切，且表格容器无滚动祖先，导致按钮既不可见也不可点。

**修复**：
| 文件 | 改动 |
|---|---|
| `llm-provider-settings.tsx` | `Table` 增加 `scroll={{ x: 'max-content' }}` |
| `llm-provider-settings.tsx` | 「实例」列增加 `fixed: 'left'`、`width: 260`，横向滚动时保留行上下文 |

**验收**：重建前端镜像（`48d486a3dc8d`）并重建 `itsm-frontend-demo` / `itsm-nginx-demo`（均 healthy）后刷新页面，「操作」列全部按钮可见；点击「编辑」正常弹出「编辑实例 · baidu」，且「跨租户共享（全员共用）」「只读保护」两个开关均正确回填为 `checked=true`（与库中 `shared=t, read_only=t` 一致）。

### 17.2 优化二：共享实例变更未让其它租户缓存失效（正确性缺陷）

**现象设计缺陷**：`LLMProviderRegistry.Invalidate(tenantID)` 只删除**调用方租户**的快照缓存。但共享实例（`shared=true`）作用于**全部租户**：管理员禁用、取消共享或删除共享实例后，其它租户在快照 TTL（30s）内仍会解析到该实例，出现「已禁用却仍被使用」「已删除仍可调用」的窗口期。

**修复**：
| 文件 | 改动 |
|---|---|
| `service/llm_registry.go` | 新增 `InvalidateAll()`：在锁内重建 `cache` 映射，清空全部租户快照 |
| `handlers/ai/llm_provider_admin_service.go` | 新增可选接口 `LLMAdminGlobalInvalidator{ InvalidateAll() }` 与 `invalidateAllTenants(tenantID)`：实现支持则全量失效，否则退化为仅清本租户（向后兼容） |
| Create | `req.Shared` 为真时 `invalidateAllTenants`，否则 `invalidate` |
| Update | `record.Shared || updated.Shared` 时全量失效（覆盖「由共享改为不共享」和「由不共享改为共享」两个方向） |
| Delete | `record.Shared` 为真时全量失效 |

**验收（关键证据）**：
| 步骤 | 结果 |
|---|---|
| 先用租户 3 请求 `available` 预热快照 | 返回 baidu（shared/readOnly/ownerTenantId=1） |
| admin `PUT /ai/providers/1 {"shared":false}` | 200 |
| **立即**（不等 TTL）租户 3 再取 `available` | 已不含 baidu → 全量失效生效 |
| admin 恢复 `{"shared":true}` | 200，库中 `shared=t` |
| 租户 3 立即再取 `available` | 重新包含 baidu |
| 租户 3 `/ai/chat/stream` | SSE 正常：36 条 `delta` 正文、`usage` tokenIn=16862/tokenOut=525、`done` 标注 `provider=baidu, providerSource=platform, model=deepseek-v4.1-flash` |
| `ai_llm_calls` 最新记录 | id=392/393，`tenant_id=3, provider_key=baidu, provider_source=platform, success=t` |

**结论**：共享实例的状态变更现在对所有租户即时生效，不需要等待 30s TTL，也不再存在「禁用后仍被解析」的窗口。

### 17.3 说明
- 同步接口 `POST /ai/chat` 返回的 `answers: []` 属既有设计（该字段承载检索来源而非模型正文），模型正文经 `POST /ai/chat/stream` 的 `delta` 事件下发，本次验证已确认流式通道完整输出，不是回归。
- 本轮修改均已编译（容器内 `go build` / 前端 `docker build`）并部署验证，回滚备份保留在服务器 `/tmp`（`page.bak2`、`reg.bak3`、`adm.bak3`）。

### 17.4 新镜像回归（只读保护未被削弱）

后端以含 `InvalidateAll` 的新镜像（`322c03431219`）重建后，重新验证只读保护，确认加固缓存失效没有放松写保护：

| 动作（租户 3，demo.mfg.admin） | 结果 | 说明 |
|---|---|---|
| `PUT /ai/providers/1 {"displayName":"hack"}` | 403 `{"code":2003,"message":"仅平台管理员可执行该操作"}` | 被平台面 RBAC 预检拦截 |
| `DELETE /ai/providers/1` | 403 `{"code":2003,"message":"权限不足"}` | 同上 |
| 库中 `display_name` | 仍为 `baidu` | **写入确实未发生**，并非仅返回错误码 |
| `id=1` 状态 | `shared=t, read_only=t, alive=t` | 未被破坏 |
| `id=2`（shared-e2e-test） | `alive=f`（deleted_at 非空） | 测试数据保持软删状态 |

**小问题（低优先级，未修）**：同一 `code=2003` 在两个接口上的 message 不一致（`仅平台管理员可执行该操作` vs `权限不足`），建议后续统一为同一文案，便于前端提示与排障。

### 17.5 本轮追加优化小结

| 序号 | 问题 | 类型 | 状态 |
|---|---|---|---|
| 17.1 | Provider 列表「操作」按钮被裁切不可点 | 可用性缺陷 | 已修复并部署验证 |
| 17.2 | 共享实例变更未让其它租户缓存失效（30s 窗口期） | 正确性缺陷 | 已修复并部署验证 |
| 17.4 | 只读保护在新镜像上回归 | 回归验证 | 通过 |

---

## 18. 工单全流程可用性审计（2026-11-13）

审计目标：回答「整个工单业务的流程能否顺利完成」——覆盖状态流转、状态显示、统计、多租户隔离、MSP 员工进入客户租户操作、权限控制。

### 18.1 结论总览

| 维度 | 结论 | 说明 |
|---|---|---|
| 状态流转（主链路） | ✅ 可用 | 已分配 → 已解决 → 已关闭 实测成功，终态按钮正确禁用 |
| 状态显示 | ❌ 有明显缺陷 | 列表/事件流显示原始英文枚举，与详情页中文不一致 |
| SLA 展示 | ❌ 自相矛盾 | 同一工单同时显示「未配置 SLA 定义」和已生成截止时间/违规时长 |
| 统计 | ⚠️ 基线正确、口径需确认 | 总数与分组计数与库一致；但「超时率 100%」需业务确认口径 |
| 多租户隔离 | ✅ 正确 | 行级隔离生效，越权访问返回 404 |
| MSP 跨租户操作 | ✅ 读写均可用 | 已授权客户可读写；未授权客户 403 阻断 |
| 权限控制 | ✅ 基本正确 | 普通用户分派返回 403；终态操作禁用 |
| 权限/角色细粒度 | ⚠️ 待加强 | 终态下「删除」仍可点 |

### 18.2 已实测通过的场景（可放心演示）

**① 状态流转主链路（工单 #76）**
| 步骤 | 操作 | 结果 |
|---|---|---|
| 1 | 「已分配」状态提交解决方案 | 状态变为「已解决」，出现「关闭工单」按钮 |
| 2 | 点击「关闭工单」并确认 | 状态变为「已关闭」 |
| 3 | 终态检查 | 「分派」「编辑」「抄送」均已禁用并提示原因 |

**② 多租户行级隔离**
| 验证 | 结果 |
|---|---|
| demo.mfg.user 列表 | 仅 10 条，全部为自己发起 |
| GET 他人租户工单 `/tickets/76` | 404（不泄露存在性） |

**③ MSP 服务商员工进入客户租户操作**
| 验证 | 结果 |
|---|---|
| 跨客户工作台 | 可见已授权客户的工单（华辰智能制造等） |
| 改状态（TKT-202511-000505 → 处理中） | 成功，落库 `tickets.id=629, tenant_id=3, status=in_progress, is_managed_by_msp=t` |
| 读取已授权客户工单 `?customerTenantId=3` | 200 |
| **读取未授权客户**（tenant4 / tenant6） | **403 `MSP_ALLOCATION_REQUIRED`** |

**④ 权限控制**
| 验证 | 结果 |
|---|---|
| 普通用户调用分派 `/tickets/629/assign` | 403 |
| 普通用户越权查看 | 404 |
| 租户管理员修改共享只读 Provider | 403（前序章节已验证） |

**⑤ 统计基线**
| 指标 | API | 数据库 | 一致性 |
|---|---|---|---|
| MSP 客户 3 未关闭 | 18 | 18 | ✅ |
| MSP 客户 5 未关闭 | 15 | 15 | ✅ |
| 租户 3 总工单 | 32 | 32 | ✅ |

### 18.3 已确认缺陷清单

| 编号 | 缺陷 | 证据 | 严重度 |
|---|---|---|---|
| **D36** | 状态本地化不一致：列表「状态」列直接渲染原始枚举 `assigned` / `new`，详情页却显示「已分配」 | /tickets 列表 vs /tickets/76 详情；MSP 工作台列表同样混排 `new` / 待处理 / 已关闭 | 中 |
| **D37** | 历史事件流显示原始枚举：`系统 assigned 工单已分配`、`系统 created 工单已创建`（枚举与中文并排） | /tickets/76 历史页签 | 中 |
| **D38** | SLA 自相矛盾：同一工单同时显示「未配置 SLA 定义，请联系管理员」+ 响应/解决截止时间 +「SLA已违规」+「已超时 5353 分钟」 | 工单 76 库中 `sla_definition_id=0` 但 `sla_resolution_deadline` 有值；API 返回 `slaStatus=null`、`slaName=null`，界面却渲染违规与超时 | 高 |
| **D39** | 「来源」列整列为空：`tickets` 表**不存在 `source` 列**，前端却渲染该列 | `\d tickets` 无 source 字段；`select source` 报列不存在 | 中 |
| **D40** | 处理人显示占位「用户 #1」/「客户 #1」，未解析为真实姓名 | 列表处理人列、详情所属客户字段（另一处又显示原始 ID `122`） | 中 |
| **D41** | 流转进度与状态不同步：状态已「已分配」/「已解决」时，流转进度仍显示「进行中 · 当前节点 任务分配 · 0/3」 | 工单 76 在已分配、已解决两个状态下的详情页 | 中 |
| **D42** | 终态仍可删除：工单已「已关闭」，其他操作都禁用，唯独「删除」按钮可用 | 工单 76 关闭后详情页 | 中 |
| **D43** | MSP 改状态后详情面板不刷新：toast 提示「已更新为处理中」，右侧详情「状态」仍显示「待处理」 | ticket 629：API/DB 已 `in_progress`，面板显示「待处理」 | 中 |
| **D44** | MSP 工作台客户数徽标与实际授权不符：顶栏显示「3」，客户过滤器面板写「共 2 个可访问客户」，接口返回 2 个 | `demo.msp.agent1` 实际授权仅 tenant3、tenant5 | 低 |
| **D45** | MSP 汇总「SLA 风险」等于未关闭数（100%） | `slaRisk=open=33`；因 tenant3/5 未关闭工单的 `sla_resolution_deadline` 均已过期。口径可解释，但 100% 需业务确认是否为真实业务状态还是演示数据造成 | 低（待确认） |
| **D46** | 仪表盘状态分项未覆盖全部状态：`pending=12 + inProgress=6 = 18`，而租户 3 未关闭工单为 32，`new`/`open`/`assigned` 未计入任何分项 | `GET /dashboard/stats` vs DB `count(*) filter (status not in resolved/closed/cancelled)=32` | 中 |

### 18.4 根因归类

| 根因族 | 涉及缺陷 | 修复方向 |
|---|---|---|
| **枚举未走统一本地化映射** | D36、D37 | 抽取 `ticketStatusLabel()` / `ticketEventLabel()` 映射，列表、事件流、详情统一调用 |
| **SLA 派生字段与定义脱钩** | D38、D45 | 要么 `sla_definition_id=0` 时不生成 deadline、不渲染违规；要么回填 definition 并展示名称 |
| **前端渲染了不存在的字段** | D39 | 移除「来源」列，或后端补 `source` 字段并落库 |
| **ID 未解析为名称** | D40 | 列表与详情统一走用户/客户名称解析 |
| **流转进度状态机与状态字段双轨** | D41 | 进度由 `status` 单一推导，避免两套数据源 |
| **终态操作白名单遗漏** | D42 | 终态禁用列表补 `delete`（或改为仅超管可删） |
| **变更后未刷新本地详情状态** | D43 | 改状态成功后重新拉取详情或本地乐观更新 |
| **统计口径/前端计数来源不一致** | D44、D46 | 客户数统一取接口 `customers.length`；状态分项补全所有枚举 |

### 18.5 建议修复优先级

| 优先级 | 缺陷 | 理由 |
|---|---|---|
| P0 | D38 | 客户会直接看到「未配置 SLA」与「已违规 5353 分钟」并列，属明显自相矛盾的对外展示 |
| P1 | D36、D37、D39、D42 | 影响演示观感与数据可信度，改动量小（映射表/删列/禁用条件） |
| P1 | D43 | MSP 核心操作后看不到结果，容易被判为「操作失败」 |
| P2 | D40、D41、D46 | 影响可读性与统计准确性 |
| P3 | D44、D45 | 需业务先确认口径 |

### 18.6 审计未覆盖项（如实说明）

1. **状态流转未全枚举实测**：仅实测 `assigned → resolved → closed`。`new → open`、`pending`（挂起/等待）、`cancelled`、`reopened` 未逐一实测。
2. **MSP 写权限仅测改状态**：MSP「指派」「回复」未实测写入。
3. **未跑自动化回归**：无单测/端到端用例，结论基于接口与人工验证。
4. **统计仅抽样核对**：核对了 MSP 客户 3/5 与租户 3 总数，未逐租户全量比对。
