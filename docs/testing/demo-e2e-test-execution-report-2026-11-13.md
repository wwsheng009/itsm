# ITSM 演示环境 E2E 业务测试执行总结报告（2026-11-13）

> **runId**：`20261113A`
> **被测环境**：`http://172.18.3.238:8089`（独立演示栈 `itsm-demo`，部署模式 `saas_msp`）
> **测试窗口**：2026-11-13 09:37 – 10:12
> **测试类型**：黑盒 E2E 业务验收（多角色 × 多租户）
> **配套文档**：
> - 测试计划：[`demo-multi-tenant-e2e-business-test-plan-2026-11-13.md`](./demo-multi-tenant-e2e-business-test-plan-2026-11-13.md)
> - 缺陷台账：[`demo-e2e-defect-tracking-2026-11-13.md`](./demo-e2e-defect-tracking-2026-11-13.md)

---

## 一、执行摘要

本轮共执行 **11 个场景组**，覆盖 **9 个账号 / 3 个租户 + 平台超管**，
验证 **7 条业务主链路**中的 6 条，采集 **约 30 项正向通过项**，识别 **32 条缺陷**
（去重归并为 **7 类根因**）。

### 1.1 结论一句话

> **跨租户隔离与核心单据 CRUD 做得扎实（安全组全绿），但两条「演示叙事主线」断裂：
> ① AI 全链路因未配千帆而 100% 超时；② 服务请求三级审批链在 L2 处不可见、且不进统一审批中心。**

### 1.2 缺陷分布

| 级别 | 数量 | 编号 |
|---|---|---|
| **P0 阻断** | 2 | D1（AI 全链路不可用）、D19（审批链 L2 断裂） |
| **P1 严重** | 4 | D2（SLA 不展示）、D12（providerSource 恒 static）、D13（上游失败误返 403）、D20（department 范围失效） |
| **P2 一般** | 21 | D3, D7, D8, D9, D10, D14, D15, D16, D17, D21, D22, D23, D24, D25, D27, D29, D30, D31, D32, D33, D34 |
| **P3 建议** | 5 | D5, D11, D26, D28, D35 |
| **合计** | **32** | 7 类根因族 |

---

## 二、执行范围与完成度

| 场景组 | 计划用例 | 已执行 | 通过 | 失败/受阻 | 完成度 |
|---|---|---|---|---|---|
| A 平台治理（PLT） | 8 | 2 | 2 | 0 | 25% |
| B 服务商 MSP（MSP） | 10 | 3 | 3 | 0 | 30% |
| C 工单生命周期（TKT） | 15 | 5 | 5 | 0 | 33% |
| D 事件与问题（INC/PRB） | 8 | 2 | 2 | 0 | 25% |
| E 变更管理（CHG） | 5 | 3 | 2 | 1 未执行完整流转 | 60% |
| F 服务请求审批（SR） | 10 | 5 | 3 | **2 失败（L2/L3 断链）** | 50% |
| G 知识库（KB） | 7 | 4 | 4 | 0 | 57% |
| H 项目管理（PRJ） | 5 | 2 | 1 | 1 无法验证 | 40% |
| I AI 能力（AI） | 7 | 3 | 0 | **3 失败** | 43% |
| J 隔离与安全（SEC） | 7 | 5 | **5 全绿** | 0 | 71% |
| K UI/UX（UX） | 5 | 5 | 0 | 5 均有问题 | 100%（发现率） |

> **说明**：完成度按"已执行用例数 / 计划用例数"计。本轮策略是**纵向穿透优先**
> （先把每条主链路走通再横向铺开），因此单组完成度不高但每条链路都拿到了深度结论。

### 2.1 已执行账号清单（9 个）

| 账号 | 角色 | 租户 | 落地路由 | 结论 |
|---|---|---|---|---|
| `admin` | `super_admin` | 平台 | `/dashboard` | ✅ 符合 |
| `demo.msp.manager` | `provider_admin` | 云维科技 | `/msp/workbench` | ✅ 符合 |
| `demo.mfg.user` | `end_user` | 华辰制造（3） | `/dashboard` | ✅ 符合 |
| `demo.mfg.agent` | `agent` | 华辰制造（3） | `/dashboard` | ✅ 符合 |
| `demo.mfg.lead` | `manager` | 华辰制造（3） | `/dashboard` | ✅ 符合 |
| `demo.mfg.itadmin` | `it_admin` | 华辰制造（3） | `/dashboard` | ✅ 符合 |
| `demo.mfg.admin` | `admin` | 华辰制造（3） | `/dashboard` | ✅ 符合 |
| `demo.med.admin` / `demo.med.secadmin` | `admin` / `security_admin` | 仁和医疗（4） | 接口登录验证 | ✅ tenantId=4 |
| `demo.edu.admin` / `demo.edu.secadmin` | `admin` / `security_admin` | 云帆教育（5） | 接口登录验证 | ✅ tenantId=5 |

---

## 三、核心发现（按业务价值排序）

### 3.1 【P0-1】AI 全链路不可用 —— 配置未指向千帆

**三重证据交叉印证**：

| 层 | 证据 |
|---|---|
| UI | AI 助手点击任务卡后返回红色错误：`LLM stream failed: OpenAI stream error: Post "https://api.openai.com/v1/chat/completions": context deadline exceeded` |
| 接口 | `GET /api/v1/ai/llm-calls` 132 条记录全部 `provider=openai_chat_completions`、`providerSource=static`、`model=gpt-4o-mini`、`success=false`、`errorClass=timeout` |
| 配置 | `docker-compose.demo.yml` `LLM_PROVIDER=${LLM_PROVIDER:-openai}`；sidecar `LLM_BASE_URL=${LLM_ENDPOINT:-${OPENAI_BASE_URL:-https://api.openai.com/v1}}` —— 未注入时**默认回落 OpenAI** |

**定性**：演示栈的 `.env.demo` **未注入用户提供的千帆配置**，运行时使用代码静态默认值打到 `api.openai.com`。境内网络访问 OpenAI 必然超时，因此 AI 对话、AI 分诊、AI 摘要、AI 建单**四个入口全部失效**。

**旁证（重要）**：变更详情页的「AI 影响分析」**可以正常出结果**（产出 4 条实施要点）。
说明系统并非 AI 模块整体坏掉，而是**恰好只有依赖外部 LLM 网关的那条链路断了**——
这进一步确认修复方向是**改配置**，不需要改代码逻辑。

**修复成本**：极低（改环境变量 + 重建容器）。
**修复动作**：
```dotenv
LLM_PROVIDER=openai
LLM_MODEL=<千帆侧模型名>
LLM_API_KEY=<REDACTED_LLM_API_KEY>
LLM_ENDPOINT=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions
OPENAI_BASE_URL=https://qianfan.baidubce.com/v2/tokenplan/personal
OPENAI_API_KEY=<同上>
LLM_TIMEOUT=60
```

---

### 3.2 【P0-2】服务请求三级审批链在 L2 断裂

**取证链（决定性）**：

| 步骤 | 操作 | 结果 |
|---|---|---|
| ① | `demo.mfg.lead`（部门经理）审批 SR-1 | ✅ 成功，状态 `已提交 → 经理已审批` |
| ② | `demo.mfg.itadmin`（IT 主管）→ `/service-requests` | ❌ 「待审批 **(0)**」，两个内部标签页均空 |
| ③ | `GET /api/v1/service-requests`（IT 主管身份） | ❌ `total: 0` |
| ④ | `GET /api/v1/service-requests/1`（同一身份直取单据） | ✅ 数据完整：`status=manager_approved`、`currentLevel=2`、L2 `status=pending` |
| ⑤ | `/approvals` 审批中心（17 项流程待办） | ❌ **0 项是服务请求**，全为 `ticket_general_flow` / `change_normal_flow` |

**根因（已定位到行）**：
`itsm-backend/handlers/service_request/repository_impl.go:352-377` 的行级数据权限只包含两种角色：

```go
query = query.Where(servicerequest.Or(
    servicerequest.RequesterIDEQ(filters.CurrentUserID),   // 我提交的
    servicerequest.ProcessorIDEQ(filters.CurrentUserID),   // 我处理的
))
```

**遗漏了第三种角色 —— 「待我审批的人」**。IT 主管既不是申请人（是 #8 刘洋），
也不是处理人（为空），因此被过滤为 0。

**架构层暴露的更深问题**：
服务请求走的是**自有 3 级审批表**（`service_request_approvals`），
而工单/变更走的是**企业 BPMN 流程实例**（出现在「审批中心 → 流程待办」）。
**两套审批机制互不相通** —— 服务请求的审批在企业统一审批中心里完全不存在。

**影响**：三级审批（部门经理 → IT 主管 → 安全管理员）**无法走完**，服务请求业务闭环断裂。
演示「多级审批」这一核心卖点时，走到 L2 就卡住。

**修复方案**：
1. **短期（最小改动）**：在 `List` 的非租户全量分支中，把「待本人审批」并入可见条件——
   关联 `service_request_approvals` 中 `status='pending'` 且节点匹配当前用户的记录。
2. **中期（推荐）**：拆出独立的 `ListPendingApprovals` 查询，以待办审批表为驱动表，
   不套用申请人的数据范围；并让服务请求接入统一审批中心。

---

### 3.3 【P1】SLA 详情完全不展示 —— 前后端字段契约不一致

**关键澄清**：此前怀疑"SLA 定义缺失"或"新建单未绑定 SLA"，**两个假设均被推翻**。

| 证据 | 内容 |
|---|---|
| SLA 管理页 | 生效 SLA **7** 条、累计违规 38、合规率 45% → **定义存在** |
| `GET /tickets/629/sla` | `slaDefinitionId=9`、`slaResponseDeadline`、`slaResolutionDeadline`、`slaStatus=active` → **绑定正常、时限已算** |
| `GET /tickets/3/sla`（种子单） | `slaDefinitionId=18`、`resolutionDeadlineBreached=true` → **同上** |
| 页面显示 | 「未配置 SLA 定义，请联系管理员」→ **展示失败** |

**根因**：字段名不匹配。

| 语义 | 后端返回 | 前端读取 | 结果 |
|---|---|---|---|
| SLA 名称 | ❌ 无 `slaName` | `slaInfo.slaName` | 落入"未配置"分支 |
| 响应截止 | `slaResponseDeadline` | `responseDeadline` | 无法展示 |
| 解决截止 | `slaResolutionDeadline` | `resolutionDeadline` | 无法展示 |
| 是否违约 | `resolutionDeadlineBreached` | `isBreached` | 违约高亮失效 |

**影响**：**每一张工单**的 SLA 卡片都是空的，而 SLA 页却宣称"生效 7 条 / 违规 38 / 合规率 45%"
—— 同一系统内自相矛盾，是演示时最容易被现场质问的点。

**修复**：后端补 `slaName`（按 `slaDefinitionId` 查定义名）+ 输出 camelCase 兼容字段；
前端做字段归一化容错。建议两侧都改。

---

### 3.4 【P1】`department` 数据范围被实现为全量

代码注释**自认**：

> 「`all` 与 `department` 均为目标租户内全量；**department 不再额外过滤部门**」

即部门级数据隔离**形同虚设**。这同时解释了为什么「所属部门」为空也不影响可见性，
也意味着「跨部门不可见」这条安全断言在当前实现下**无法成立**。

---

### 3.5 【隔离与安全：全绿】这是本系统最扎实的部分

| 用例 | 断言 | 结果 |
|---|---|---|
| SEC-01 | 客户管理员访问 `/msp/workbench` | ✅ 跳 `/403`，无 MSP 菜单 |
| SEC-02 | 跨租户读医疗工单 #33~#37 | ✅ 全部 **404**，零字段泄露 |
| SEC-03 | 跨租户改状态 | ✅ **404** |
| SEC-04 | 匿名访问 `/tickets` | ✅ **401** |
| SEC-07 | 跨租户评论（写） | ✅ **403** |

**返回码语义也设计得当**：读用 404（防资源枚举）、写用 403（明确拒绝）。
这部分不需要改动。

---

## 四、7 类根因族与修复路线图

| # | 根因族 | 成员 | 修复点 | 成本 | 建议顺序 |
|---|---|---|---|---|---|
| 1 | **AI 链路** | D1, D12, D13, D28 | `.env.demo` 注入千帆；错误码改 502/503；错误话术包装（不暴露内部端点） | **极低** | **① 立即** |
| 2 | **审批可见性** | D19, D14, D32, D34 | `repository_impl.go` 行级过滤补「待本人审批」；按钮按节点状态禁用；申请人 join 名称 | 中 | **② 立即** |
| 3 | **SLA 契约** | D2, D8, D10, D11 | 后端补 `slaName` + camelCase；统一三处统计口径；数值格式化 | 低-中 | ③ |
| 4 | **组织/客户字段空** | D3, D17, D23 | 单据实体 `department_id`/`customer_id` 落库 + DTO 映射名称 | 中 | ④ |
| 5 | **统计卡分项≠总数** | D4, D15, D24 | 统计 SQL 补齐状态枚举；加断言「分项合计 == 总数」 | 低 | ⑤ |
| 6 | **i18n + 裸标识符** | D30, D31, D6/D18, D21, D35 | 状态/优先级走 i18n；流程名映射中文；Badge-chevron 加 margin | 低 | ⑥ |
| 7 | **空态/格式/计数** | D5, D9, D25, D26, D27, D29, D33 | 各点独立小修 | 低 | ⑦ |

---

## 五、测试数据与环境状态

### 5.1 本轮产生的数据

| 类型 | 标识 | 租户 | 状态 |
|---|---|---|---|
| 工单 | `#629` / `TKT-202611-000505`「[E2E-20261113A] 车间打印终端无法连接共享打印机」 | 制造（3） | 已分派给吴迪，状态「待处理」 |
| 评论 | 工单 `#629` 时间线 2 条 | 制造（3） | 保留（审计用） |
| 知识文章 | `#45`「[E2E-20261113A] 共享打印机无法连接的排查与处置指引」 | 制造（3） | **已发布**（建议轮末下架） |
| 服务请求 | `SR-202610-000001` | 制造（3） | 被 L1 审批为「经理已审批」，**卡在 L2** |

### 5.2 未触碰

`[DEMO]` 种子单据、`demo.*` 种子账号、`review-test` 遗留租户 —— 全部保持原状。

### 5.3 需人工清理

- 知识文章 `#45`：建议下架（`/knowledge/articles/45` → 取消发布），避免与演示用文章混在一起。
- 工单 `#629`：建议置 `cancelled`，或保留作为"现场提单"演示素材。

---

## 六、阻塞与未完成项（诚实披露）

### 6.1 【阻塞】238 服务器 SSH 认证失败 —— 修复与部署无法执行

| 项 | 内容 |
|---|---|
| 目标 | `root@172.18.3.238:22` |
| 提供凭据 | `<REDACTED_PASSWORD>`（任务说明） |
| 实测 | `Password authentication failed` / `Access denied` / `Configured password was not accepted` |
| 端口 | 22 **开放**（`Test-NetConnection` 通过） |
| 服务端认证 | 接受 password 方式，但该口令被拒 |
| 结论 | **任务说明中的 root 口令与服务器实际口令不一致**，或 root 登录已禁用/改密 |

**因此未能完成的动作**：
- ❌ 未修改 238 上的 `.env.demo`（D1 修复必须此步）
- ❌ 未构建镜像
- ❌ 未重建 `itsm-demo` 栈
- ❌ 未做修复后回归

**已完成的部分**：
- ✅ 所有缺陷已定位到**具体文件 + 行号**
- ✅ 修复方案已写成**可直接执行的配置/代码补丁思路**（见各缺陷条目）
- ✅ 代码可在本地工作区 `E:\projects\itsm\itsm` 直接修改提交，**只差部署动作**

**需要用户提供**：238 的实际 root 口令，或可用 SSH 私钥 / 具备 docker 权限的账号。

### 6.2 未执行的用例（非阻塞，属轮次预算）

| 用例 | 原因 |
|---|---|
| CHG-02 变更全链路流转 | 待审批单为 `[DEMO]` 种子数据，按测试规范不改种子单据 |
| PRJ-02 进度派生 | `GET /projects/{id}` 响应不含 tasks 字段，需前端详情页或任务接口才能核对（已登记 D25） |
| SR-05（L3 安全审批） | 被 D19 阻断，L2 都走不到 |
| KB-07 工单转知识 | 轮次时间不足 |
| 部分 MED/EDU 租户真实业务操作 | 仅做了登录与租户绑定的接口验证，未做完整 UI 业务操作 |

---

## 七、给演示团队的三条建议

### 7.1 演示前必须修的两件事（30 分钟内可完成）

1. **注入千帆配置并重建 ai-service** —— 这是唯一能让"AI-Native"这个产品名站得住的动作。
   修完后 `POST /api/v1/ai/triage` 应返回 200，`/ai/chat` 应返回自然语言答复。
2. **演示服务请求审批时避开 L2 断点**，或先修 `repository_impl.go` 的行级过滤。
   若要现场演示三级审批，当前实现会卡在 IT 主管看不到待办。

### 7.2 演示解说口径建议

- **SLA**：不要主动打开工单详情的 SLA 卡片（是空的）；改从 SLA 管理页讲"7 条策略 / 合规率趋势"。
- **审批中心**：只演示工单/变更的流程待办（那边是通的），不要提服务请求也走审批。
- **跨租户隔离**：这是**最强的部分**，建议主动演示——用制造管理员去访问医疗工单，会干净地返回 404。

### 7.3 需要规避的问法

如果客户问「同一个工单，为什么 SLA 页说有 38 个违规，但详情页说未配置 SLA？」——
这是 D2+D10 的直接暴露点，建议提前准备口径或先修复。

---

## 八、产出物清单

| 文件 | 内容 |
|---|---|
| [`demo-multi-tenant-e2e-business-test-plan-2026-11-13.md`](./demo-multi-tenant-e2e-business-test-plan-2026-11-13.md) | 测试计划与场景手册：账号权限矩阵、11 组场景（约 90 条用例）、缺陷分级与闭环流程、9 轮排期 |
| [`demo-e2e-defect-tracking-2026-11-13.md`](./demo-e2e-defect-tracking-2026-11-13.md) | 缺陷台账：32 条缺陷的逐条取证、代码定位、修复方案；含"撤销的误判"诚实记录 |
| 本文件 | 执行总结：完成度、核心发现、修复路线图、阻塞披露、演示建议 |

---

## 九、附：诚实性说明

本轮严格区分「已观测事实」与「未验证假设」，并在过程中**主动撤销了 3 处误判**：

| 误判 | 撤销依据 |
|---|---|
| D2 原判「新建单未绑定 SLA」 | 接口实测证明绑定正常，真实问题在字段契约 |
| D3 原判「新建单不落部门字段」 | 种子单同样为空，问题范围更大 |
| 「通过按钮图标与文字重叠」 | 放大核对后确认为正常按钮布局 |

此外 **D7（end_user 的「问 AI」按钮置灰）当前仍为 `investigating`**：
同一按钮在 `agent` 视角可用、在 `end_user` 视角置灰，**可能是刻意的权限设计**，
尚未取得「应当可用」的证据，因此不擅自定级为缺陷。
