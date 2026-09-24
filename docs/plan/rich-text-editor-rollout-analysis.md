# 富文本编辑器可切换单据分析（Rollout Analysis）

> 文档类型：技术调研 + 分批实施建议（Proposed）
> 适用范围：`itsm-frontend`、`itsm-backend`
> 编制日期：2026-09-22
> 版本：v1.0
> 目标读者：前端、后端、测试、产品
> 关联文档：`docs/plan/generic-attachment-richtext-control-plan.md`（附件/富文本能力底座方案）、`docs/architecture/ticket-create-page-rich-input-optimization.md`
> 说明：本文为**只读调研结论**，不含任何代码改动；所有代码坐标为编制时点的工作区快照。

---

## 0. 结论先行（TL;DR）

1. **可切换性由三件事共同决定**：① 前端公共编辑器是否可用；② 后端是否已把该单据注册为附件宿主（`defaultAttachmentHosts()`）；③ 前端是否配置了该域的域内 URL 映射（漏配会导致普通用户 403）。三者缺一，切换即踩坑。
2. **基础设施已通、可立即切换的只有一类**：**服务请求申请理由 `reason`**（后端宿主已注册且以 `reason` 为富文本引用源、域内路由已存在），属"零后端改造"档；知识库文章与工单已完成切换。
3. **需补后端宿主 + 前端 URL 映射的第二梯队**：事件、问题、已知错误、变更、变更 PIR、发布、CMDB CI。权限码已存在并在方案中标注"预留"，后端列类型均为 `field.Text`，工作量集中在"注册宿主 + 别名路由 + 前端映射"三处样板代码。
4. **存在一档"只上文本富文本、不接图片"的安全降级**：编辑器不注入 `onUploadImage` 时图片能力整体禁用（fail-fast 而非 404），保留加粗/标题/列表/引用/代码块/链接能力，适合暂不做后端接线的域先行落地。
5. **有一批字段不建议切换**：机器消费的 JSON/清单字段、短留痕文本（审批意见、升级/回滚原因）、后台配置表单；富文本会污染搜索、通知、审计与列表摘要。
6. **关键技术约束**：切换后必须同步改造渲染侧（详情 HTML 渲染 / 列表与通知纯文本截断），并为新宿主实现 `ReferencesFunc`，否则内嵌图片会被附件回收任务误删。

---

## 1. 判断标尺：一个单据"能不能切"取决于三件前置条件

| 前置条件 | 现状 | 证据坐标 |
|:---|:---|:---|
| ① 前端公共编辑器可用 | 已有 `components/common/rich-text/RichTextEditor`（TipTap → HTML，白名单净化）；**不传 `onUploadImage` 时图片能力整体禁用**（fail-fast，不会产生 404 断链） | `itsm-frontend/src/components/common/rich-text/RichTextEditor.tsx:55-81` |
| ② 后端附件宿主已注册 | `defaultAttachmentHosts()` 只注册 4 个 `biz_type`：`ticket` / `ticket_comment` / `knowledge_article` / `service_request`；`incident` / `problem` / `change` / `release` / `cmdb_ci` 为**预留位**（权限码已存在、尚未接线） | `itsm-backend/service/attachment_service.go:49-53,923-1001`；`docs/plan/generic-attachment-richtext-control-plan.md:341-347` |
| ③ 前端域内 URL 映射 | `DOMAIN_CONTENT_URLS` / `DOMAIN_PREVIEW_URLS` / `DOMAIN_LIST_PATHS` 同样只覆盖 ticket / knowledge_article / service_request 三域 | `itsm-frontend/src/lib/api/attachment-api.ts:41-43,82-92` |

> ⚠️ 条件③是最容易被忽略的隐性门槛：附件内容/预览接口有"兜底权限码"设计，兜底码只授予 admin/sysadmin；普通用户请求未配置域内 URL 的域，会直接 403（与历史问题 BE-10 同类）。`attachment-api.ts:75-92` 的注释已就此做出说明。

### 1.1 附件的两个写入方与一条保护链

- 富文本内嵌图：由编辑器 `onUploadImage` 上传 → 返回 `attachment_id` → HTML 中以 `<img src data-attachment-id>` 引用。
- 单据普通附件：`AttachmentField` 直传，挂在宿主单据下。
- 保护链：宿主注册的 `ReferencesFunc` 决定"该附件是否仍被引用"；若新域只注册了 `ExistsFunc` 而漏了引用检查，附件清理任务会误删仍在使用中的图片。当前 `attachment.cleanup_enabled=false`，问题暂未暴露。

---

## 2. 两种存储范式（切换前必须先选定）

| 范式 | 结构 | 适用判定 | 现状案例 | 坐标 |
|:---|:---|:---|:---|:---|
| **双字段（工单范式）** | 正文纯文本列 + `_html` 列 + `_format` 标记 | 该字段已被**搜索 / 通知 / AI / 导出**消费，不能把 HTML 灌进下游 | 工单 `description` + `description_html` + `description_format` | `itsm-backend/ent/schema/ticket.go:23-29` |
| **单字段 HTML（知识库 / 服务请求范式）** | 仅一个 Text 列，直接存 HTML | 无强下游消费，或下游可自行净化 | `knowledge_article.content`；`service_request.reason` | `ent/schema/knowledgearticle.go:16`；`ent/schema/servicerequest.go:22` |

前端统一用 `isHtmlContent()` 做"HTML / 历史纯文本"双读，判定是保守的（只认块级标签），因此老数据不会被误判为 HTML。坐标：`itsm-frontend/src/lib/rich-text/content-format.ts:16-26`。

> 决策规则：**只要该字段出现在搜索条件、通知模板、AI 摘要或导出中，一律走双字段**；否则优先走单字段 HTML 以最小化改造。

---

## 3. 分档结论

### 3.1 A 档 — 基础设施已通，可直接切换

| 单据 | 字段与表单现状 | 后端就绪度 | 坐标 |
|:---|:---|:---|:---|
| **服务请求申请理由** | `reason`：`Input.TextArea rows=4 maxLength=2000`，待替换 | 宿主已注册，且后端注释明确"服务请求的富文本正文落在 `reason`"；域内路由已存在 | `pages/(main)/service-catalog/request/$id/index.tsx:203-209`；`itsm-backend/service/attachment_service.go:989-998`；`src/lib/api/attachment-api.ts:42,65-73` |
| **知识库文章** | 已完成切换（新建 / 编辑 / 详情渲染） | 已注册 | `pages/(main)/knowledge/articles/new|$id/edit`、`components/knowledge/ArticleDetail.tsx` |
| **工单** | 已完成切换（创建页、详情编辑弹层、评论附件） | 已注册（`ticket` + `ticket_comment`） | `pages/(main)/tickets/create/index.tsx`、`components/ticket/TicketDetail.tsx` |

**服务请求是当前性价比最高的下一站**：无需后端改动，仅需前端换控件 + 详情渲染改造 + 列表截断。

> 现状提示：工作区已有未提交改动涉及 `pages/(main)/service-requests/new/index.tsx`（服务选择页）与 `ServiceRequestDetail.tsx`，方向与本档一致；切换 `reason` 时必须同步修改这两处渲染侧，避免"存 HTML、显示源码"。

### 3.2 B 档 — 需补后端宿主 + 前端 URL 映射

**统一改造清单**（每个域三件事）：

1. 在 `defaultAttachmentHosts()` 注册 `ExistsFunc` + `ReferencesFunc`（后者不可省，见 §1.1）；
2. 新增域内别名路由 `/api/v1/<domain>/:id/attachments...`；
3. 前端补 `DOMAIN_CONTENT_URLS` / `DOMAIN_PREVIEW_URLS` / `DOMAIN_LIST_PATHS` 三项映射。

后端目标列类型均为 `field.Text`，可直接承载 HTML。

| 单据 | 候选字段（前端现状） | 后端列 | 备注 |
|:---|:---|:---|:---|
| **事件 incident** | `description`（create:270-276、edit:223-224）；`rootCause`（create:584-590） | `description` Text（`ent/schema/incident.go:24`）；**`root_cause` 未在 incident schema 中确认，切换前需核对落列** | 其余多行输入为"危机沟通计划 / 升级原因"等应急流程短文本，不建议切 |
| **问题 problem** | `description` / `rootCause` / `impact`（new:112-158、edit:170-187） | 均 Text（`ent/schema/problem.go:26,38,44,47`） | 前端已有 10–5000 字校验；切换后需按"净化后纯文本长度"重新对齐校验口径 |
| **已知错误 known error** | `description / symptoms / rootCause / workaround / resolution`（known-errors:458-472） | 均 Text（`ent/schema/known_error.go:23-35`） | 表单多为 `rows=2` 的短输入，建议只切 `resolution` / `workaround` |
| **变更 change** | `description / justification / implementationPlan / rollbackPlan`（new:166-295、edit:235-359） | 均 Text（`ent/schema/change.go:26,29,68,71`） | **实施步骤 / 回滚步骤是富文本最佳场景**，建议列为 B 档首批 |
| **变更 PIR** | `successSummary` / `lessonsLearned`（pir:269-289） | 均 Text（`ent/schema/change_pir.go:30-39`） | `issuesEncountered` / `improvementRecommendations` 的 `maxLength=300` 过短，**不要切** |
| **发布 release** | `description / releaseNotes / rollbackProcedure / validationCriteria`（`components/release/ReleaseForm.tsx:213-283`） | 均 Text（`ent/schema/release.go:24,65,68,71`） | `deploymentSteps` / `affectedSystems` / `affectedComponents` 是"每行一个"清单语义 → 保留 TextArea |
| **CMDB CI** | `description`（`CIEditorForm.tsx:206-207`、`CIRelationshipManager.tsx:399-400`） | — | `extensionAttributes` 与 `attribute_schema` 均为 JSON → 必须保留纯文本 |

### 3.3 C 档 — 只上"文本富文本"，暂不接图片

- **做法**：`RichTextEditor` 不注入 `onUploadImage`。编辑器会禁用图片按钮并对粘贴/拖拽图片做拦截告警（安全降级，不会出现断链 404）。
- **可用能力**：加粗、斜体、标题、有序/无序列表、引用、行内代码、代码块、链接。
- **适用**：希望先获得排版能力、但暂不承担后端宿主接线的域（例如变更实施步骤可先行试点）。
- **代价**：后续补图片能力时，仍需走一遍 B 档的"注册宿主 + 别名路由 + 前端映射"流程。

### 3.4 D 档 — 不建议切换（保持 TextArea）

| 类别 | 典型字段 | 坐标 | 原因 |
|:---|:---|:---|:---|
| 机器消费字段 | JSON 模板（`cmdb/cloud-services` 的 `attribute_schema`）、许可证密钥、邮箱地址/域名清单、受影响系统/组件清单、CI 扩展属性 | `pages/(main)/cmdb/cloud-services/*`、`components/release/ReleaseForm.tsx` | 结构化语义，HTML 会造成解析失败 |
| 短留痕文本 | 审批意见 | `pages/(main)/approvals/index.tsx:698-700`、`pages/(main)/my-requests/$requestId:288-290` | 会污染通知、审计与列表摘要 |
| 短留痕文本 | 升级原因 | `components/incident/IncidentDetail.tsx:857-861` | 同上 |
| 短留痕文本 | 拒绝/回滚原因 | `components/release/ReleaseDetail.tsx:91-94` | 同上 |
| 后台配置表单 | `admin/**`、规则/菜单/角色/字典/系统配置 | `pages/(main)/admin/**` | 非单据正文，属系统配置 |

> **待确认归属**：`improvements/new` 前端存在 `description` TextArea，但其 API 落在 `lib/api/change-api.ts`，后端未见独立的 improvement 服务/实体。若"改进"不是独立单据，应随变更域一并处理；若是独立单据，需先补后端实体再评估。

---

## 4. 统一改造模式（建议照此执行）

1. **判定档位**：先对照 §3 确认目标字段属于 A/B/C/D 哪一档；B 档必须先做后端接线，C 档要先确认业务方接受"无图片"。
2. **选定存储范式**：按 §2 的决策规则二选一，禁止"先单字段存 HTML、后期又要求搜索"的返工路径。
3. **渲染侧同批改造（易漏项）**：
   - 详情页：`sanitizeRichTextHtml` + 只读渲染；
   - 列表 / 卡片 / 通知 / AI 摘要：`htmlToPlainText` 截断（工单已有现成实现可复用，参考 `components/ticket/TicketDetail.tsx` 的列表摘要处理）；
   - 导出与打印模板：确认是否直接输出该字段，必要时同批加上净化。
4. **附件引用保护**：新宿主必须实现 `ReferencesFunc`，否则清理任务（`attachment.cleanup_enabled` 打开后）会误删在用图片。
5. **权限回归**：上传走 `<domain>:write`、内容/预览走 `<domain>:read`；漏配域内 URL 会复现"普通用户 403"，需按 §1 条件③ 逐域核对。
6. **开关与门禁**：沿用 `VITE_RICH_TEXT` 开关做降级回退；每个新域补充 `RichTextEditor` / `attachment-uploader` 定向单测 + 一次多角色冒烟。
7. **长度校验对齐**：后端 DTO 若按字符串长度校验，HTML 标签会挤占额度；建议改为"净化后纯文本长度"或按域放宽阈值。

---

## 5. 排期建议（WBS）

| 波次 | 内容 | 后端改动 | 前端改动 | 建议理由 |
|:---|:---|:---|:---|:---|
| **第一波** | 服务请求 `reason` | 无 | 表单换控件 + 详情渲染 + 列表截断（含工作区在改的 `service-requests/new`、`ServiceRequestDetail`） | 基础设施全通，投入产出比最高 |
| **第二波** | 变更（`description` / `implementationPlan` / `rollbackPlan`）+ 事件/问题正文 | 注册宿主 + 别名路由 | 表单 + 渲染 + 域内 URL 映射 | 权限码已就位，富文本价值最直观 |
| **第三波** | 发布、CMDB CI `description`、已知错误 `workaround/resolution` | 同上（逐域） | 同上（逐域） | 按业务诉求排优先级，短字段不切 |
| **每一波** | 回归项 | 长度校验口径、附件引用保护、权限与 403 冒烟、存量数据双读 | 同左 | 防止"能编辑但显示/搜索异常" |

### 5.1 实施进度（滚动更新）

| 波次 | 状态 | 证据 / 说明 |
|:---|:---|:---|
| **第一波**：服务请求 `reason` | ✅ 已完成并提交 | commit `320d4bb5`（9 files, +273/-12）；`npx tsc --noEmit` exit 0、`jest src/components/common/rich-text` 4 suites/23 tests 全通过、`go build ./...` exit 0；后端 `reason` 校验放宽至 `max=20000` |
| **第二波**：变更 `description`/`implementationPlan`/`rollbackPlan` + 事件/问题正文 | ✅ 已完成并提交（`1a85e8d8`） | 后端：`change`/`incident`/`problem` 宿主注册（含 `ReferencesFunc`，`service/attachment_service.go`）+ 三域别名路由（各 6 条，权限复用 `<domain>:read/write/delete`）+ DTO 放宽（incident/problem `description` → `max=20000`；change 三字段原本无 `max=`）+ 新增覆盖守卫测试 `service/attachment_hosts_wave2_test.go`；前端：`attachment-api.ts` 域内 URL 映射补齐三域，变更三字段与事件/问题 `description` 接入富文本（新建两段式 / 编辑即时上传）+ 详情 `RichTextContent` 双读 + 列表 `htmlToPlainText(…, 160)` 截断 + 附件路由单测补 1 例。验证：`go build ./...` exit 0、`go test ./handlers/attachment/... ./router/... ./service/...` 各包 ok、`npx tsc --noEmit` exit 0、`jest src/lib/api/__tests__/attachment-api.test.ts src/components/common/rich-text` 5 suites/43 tests 全通过。**本轮事件/问题范围仅 `description`**，`root_cause`/`workaround`/`resolution` 留待第三波 |
| **第三波**：发布 / CMDB CI `description` / 已知错误 | ⏳ 未开始 | — |

---

## 6. 风险与前置约束

| 风险 | 说明 | 缓解 |
|:---|:---|:---|
| 普通用户 403 | 未配置域内 URL 映射时，内容/预览接口回落到仅 admin/sysadmin 的兜底码 | 每域切换前核对 `DOMAIN_CONTENT_URLS` / `DOMAIN_PREVIEW_URLS` / `DOMAIN_LIST_PATHS` |
| HTML 污染下游 | 搜索 LIKE、通知模板、AI 摘要把标签当正文 | 已在下游消费的字段一律用双字段范式，或列表侧统一 `htmlToPlainText` |
| 附件误删 | 新宿主漏实现 `ReferencesFunc` | 宿主注册时强制实现引用检查；当前 `cleanup_enabled=false` 仅为暂态 |
| 存量数据兼容 | 历史记录是纯文本 / Markdown | `isHtmlContent()` 保守判定 + 双读；上线前用真实老数据抽样验证 |
| 长度校验失真 | 按 HTML 长度校验会误伤长正文 | 改为净化后纯文本长度，或按域放宽 |
| 灰度缺失 | 当前无灰度环境 | 依赖 `VITE_RICH_TEXT` 开关回退 + 按域逐个放量 |

---

## 7. 待确认事项

1. ~~服务请求线是否已有并行改动~~（**已闭环**）：第一波已随 commit `320d4bb5` 提交，`service-requests/new` 与 `ServiceRequestDetail` 的改动已纳入，无遗留冲突。
2. **事件 `rootCause` 的后端落列**：`ent/schema/incident.go` 未见 `root_cause` 字段，需确认前端提交后的持久化路径。
3. **改进 improvement 的归属**：是否为独立单据（决定是否需要新增后端实体与宿主）。
4. **非 ticket 域的上线策略**：当前只有开发环境，需明确是否允许直接上线、是否需要开关分级。
5. **预览能力范围**：域内 preview 路由是否对所有新域都要求实现，还是允许"先只支持 content"。
6. **HTML 落库后的检索口径（第二波新暴露）**：变更 / 事件 / 问题的关键字检索仍走后端列级 LIKE，HTML 落库后可能命中标签与属性文本（前端看板内过滤已按纯文本）；建议后端检索时剥离标签，或增派生纯文本列。
7. **change 三字段无长度上限**：`dto/change_dto.go` 的 `description` / `implementationPlan` / `rollbackPlan` 本无 `max=` 约束，仅靠前端把门；后续补约束时对齐 20000 口径。
8. **标准变更模板弹窗未切富文本**：`standard-changes` 页内模板弹窗的同类字段仍为 `TextArea`，属模板实体另一 surface，未纳入第二波。
9. **编辑态移除图片不解除附件绑定**：两波均未接 `extractAttachmentImageIds` + `AttachmentApi.remove`（`TicketDetail.tsx` 有现成用法），可作为统一收敛项。

---

## 附录 A：证据坐标速查

| 主题 | 坐标 |
|:---|:---|
| 公共富文本编辑器 | `itsm-frontend/src/components/common/rich-text/RichTextEditor.tsx:55-81` |
| 附件宿主注册 | `itsm-backend/service/attachment_service.go:49-53,923-1001` |
| 服务请求以 `reason` 为引用源 | `itsm-backend/service/attachment_service.go:989-998` |
| 预留宿主与权限码方案表 | `docs/plan/generic-attachment-richtext-control-plan.md:341-347` |
| 前端域内 URL 映射（含 `change`/`incident`/`problem`） | `itsm-frontend/src/lib/api/attachment-api.ts`（`DOMAIN_LIST_PATHS` / `DOMAIN_CONTENT_URLS` / `DOMAIN_PREVIEW_URLS`） |
| 双字段范式（工单） | `itsm-backend/ent/schema/ticket.go:23-29` |
| 单字段 HTML 范式 | `itsm-backend/ent/schema/knowledgearticle.go:16`、`itsm-backend/ent/schema/servicerequest.go:22` |
| 前端内容格式双读判定 | `itsm-frontend/src/lib/rich-text/content-format.ts:16-26` |

## 附录 B：修订记录

| 版本 | 日期 | 说明 |
|:---|:---|:---|
| v1.2 | 2026-09-22 | 第二波实施完成并提交（`1a85e8d8`，27 files, +1181/-115）：后端三域宿主/别名路由/DTO 放宽 + 覆盖守卫测试，前端三域表单与渲染改造；补 §7 遗留项 6-9 |
| v1.1 | 2026-09-22 | 补 §5.1 实施进度：第一波已提交（`320d4bb5`）；第二波开工（后端三域宿主 + 别名路由 + DTO 放宽，前端域内 URL 映射与变更/事件/问题正文接入） |
| v1.0 | 2026-09-22 | 首次编制：三前置条件标尺、A/B/C/D 分档、统一改造模式、三波次排期与风险清单 |
