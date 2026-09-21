# 工单创建/编辑页富输入与布局优化方案

> Status: draft
> Created: 2026-09-20
> Scope: `itsm-frontend` 工单创建页 `/tickets/create`、工单详情/编辑页 `/tickets/[ticketId]`
> 基线代码: `itsm-frontend/src/app/(main)/tickets/create/page.tsx`（671 行，实测）

本文件是**方案与实施计划（draft）**，不是已完成功能证明。文中标注的行号为基线快照行号，改动后会漂移，实施时以符号名（组件名、函数名）为准。

---

## 1. 背景与现状

### 1.1 需求来源

当前 `/tickets/create` 页面存在以下缺口：

| # | 缺口 | 用户影响 |
| --- | --- | --- |
| R1 | 工单描述缺少富文本编辑能力（无加粗/列表/代码块/标题等结构化表达） | 长文本排障信息可读性差，工程师难以结构化描述问题 |
| R2 | 缺少附件上传入口 | 截图、日志、配置文件只能靠外链或文字描述传递 |
| R3 | 不支持粘贴图片 | 截图需先存盘再上传，操作链路长 |
| R4 | 页面布局冗长，类型选择区常驻占用首屏 | 用户进入页面后需先滚动才能看到表单主体 |
| R5 | 「选择工单类型」以内联区块形式直接占用页面空间 | 首屏信息密度低，类型多时形成大面积空白/长列表 |

### 1.2 现状代码事实（基线）

`itsm-frontend/src/app/(main)/tickets/create/page.tsx`：

- 第 3 行：`import React, { useState, useEffect, useRef } from 'react';`
- antd 导入：`Card, Form, Input, Button, Space, Typography`（已含 `Typography`，因此 `<Text type="secondary">` 可用）
- 第 376 行附近：`<Text type="secondary">选择工单类型，填写详细信息后提交</Text>` —— 页面副标题文案
- 第 387 行附近：`<span>选择工单类型</span>` —— **当前内联类型选择区的标题，即 R5 需要改造的锚点**
- 第 504 行附近：`{field.type === 'textarea' ? (` —— 自定义字段按类型动态渲染，`textarea` 是现有的一种字段类型（富文本接入需在此扩展分支）
- 第 126 行附近 `useEffect`：调用 `TicketTypeApi.list({ status: 'active', page: 1, pageSize: 100 })`，再 `setTicketTypes(result.types.map(...))` —— 工单类型数据来源与已加载状态

### 1.3 现有可复用资产（避免重复造轮子）

| 资产 | 路径 | 复用点 |
| --- | --- | --- |
| 附件区块组件 | `itsm-frontend/src/components/business/TicketAttachmentSection.tsx` | 附件列表/上传 UI 已有实现，优先复用而非新建 |
| 类型表单弹窗 | `itsm-frontend/src/components/business/TicketTypeFormModal.tsx` | 弹窗组件的既有写法与交互范式可对齐 |
| 附件 API 客户端 | `itsm-frontend/src/lib/api/ticket-attachment-api.ts` | 上传/删除/查询附件的调用入口 |
| 工单类型 API 客户端 | `itsm-frontend/src/lib/api/ticketTypeApi.ts` | `list()` 已在使用，弹层数据源不变 |

### 1.4 依赖现状

`itsm-frontend/package.json` 现有相关依赖包含：`antd ^6.2.2`、`@tanstack/react-query`、`axios`、`@types/dompurify`、`clsx`、`date-fns`。

**未发现成熟的富文本编辑器依赖**（tiptap / slate / quill / lexical 等均未见）。因此 R1 需要**新增依赖**，这是本方案的主要引入成本与风险点。实施前请复核 `package.json`（历史查看存在省略区间，以实际文件为准）。

`@types/dompurify` 的存在说明项目已有 HTML 净化意图，富文本回显必须走同一策略。

---

## 2. 目标与非目标

### 2.1 目标

- **G1**：工单描述字段支持富文本编辑（基础排版能力），并保证存储与回显的安全性与向后兼容。
- **G2**：支持附件上传（复用现有附件能力），在创建阶段即可挂载附件。
- **G3**：支持在富文本编辑器中**直接粘贴图片**，自动上传并内联插入。
- **G4**：重构创建页布局，压缩首屏高度，表单主体上移。
- **G5**：「选择工单类型」由内联区块改为**弹层面板**，选中后以紧凑形式回显。

### 2.2 非目标（本次不做）

- 不做工单详情页的信息架构重构（仅做富文本/附件的**回显一致性**适配）。
- 不做富文本的协同编辑、版本历史、评论批注。
- 不改动后端工单主表 schema 的语义（见 4.5 的兼容策略）。
- 不替换 antd 版本或引入新的 UI 框架。
- 不做移动端专项适配（保持响应式可用即可）。

---

## 3. 技术选型

### 3.1 富文本编辑器：Tiptap 2（`@tiptap/react` + `@tiptap/starter-kit`）

| 候选 | 结论 | 理由 |
| --- | --- | --- |
| Tiptap 2 | **采用** | Headless、React 一等公民、按需装载扩展；输出 HTML 可直接复用现有 `dompurify` 清洗链路；包体积可控 |
| Quill 2 | 备选 | 自带主题样式，与 antd 主题融合成本较高 |
| Slate | 不采用 | 需自行实现 schema/序列化，维护成本高 |
| Lexical | 不采用 | 生态在 React 侧相对分散，团队无存量经验 |
| `contenteditable` 手写 | 不采用 | 粘贴清洗、撤销栈、选型稳定性全靠自研，风险不可控 |

**扩展清单（按需引入，避免一次性全量）**

- `StarterKit`：段落、标题、加粗/斜体/删除线、行内代码、引用、有序/无序列表、分隔线
- `@tiptap/extension-link`：超链接插入与编辑
- `@tiptap/extension-image`：图片节点（用于粘贴图片内联展示）
- `@tiptap/extension-placeholder`：空态提示（对齐现有「填写详细信息后提交」文案）
- `@tiptap/extension-underline`、`@tiptap/extension-text-align`：格式对齐

**不为 v1 引入**：表格、代码高亮、Mermaid、协作（Yjs）。

### 3.2 附件上传：复用现有上传能力 + 自研拖拽/粘贴壳层

优先复用后端既有附件接口与前端既有上传封装（`TicketAttachment` 相关 API 客户端与 `TicketAttachmentSection` 组件）。若既有封装不支持「暂存后随工单一次性提交」，则采用**两段式**：

1. 选择/粘贴即上传到附件暂存接口，拿到附件 ID 与 URL；
2. 提交工单时携带附件 ID 列表，由后端做归属绑定。

> 待确认：暂存接口是否已存在（见 §10 待确认事项 4）。现状核实结论：**不存在**（详见 §4.5 与 §5.2），故本期默认走「创建成功后在原页补传」的两段式路径。

### 3.3 弹层：antd `Modal` + `Drawer` 组合

- 工单类型选择：`Modal`（居中，类型卡片网格）——满足「不直接占用页面空间」的核心诉求。
- 附件面板：`Drawer`（右侧滑出），避免与类型弹层抢占视觉焦点。

### 3.4 安全与清洗

富文本入参一律视为不可信 HTML：

- 提交前：`DOMPurify.sanitize(html, { ALLOWED_TAGS, ALLOWED_ATTR })` 白名单清洗（现有依赖 `dompurify` + `@types/dompurify` 已具备）。
- 展示时：同样二次清洗后再 `dangerouslySetInnerHTML`，不信任已存库内容。
- 禁止 `script`、`iframe`、`style`、`on*` 事件属性；`a` 标签强制 `rel="noopener noreferrer"`、`target="_blank"`。
- 图片仅允许 `http(s):` 与站内相对路径，拒绝 `data:`（粘贴图片必须先落盘成 URL，见 4.4）。

---
## 4. 详细设计

### 4.1 页面信息架构（布局优化）

现状：单列长表单，类型选择器、自定义字段、基础字段全部堆叠在首屏，视觉噪音大。

目标布局（`Row`/`Col` 栅格，`lg` 断点单列回退）：

| 区域 | 内容 | 说明 |
| --- | --- | --- |
| A 头部条 | 标题、工单类型徽标、`更换类型` 按钮 | 类型不再占块，只显示已选结果 |
| B 主区（左，`span=16`） | 标题、描述（富文本）、自定义字段 | 输入主战场 |
| C 侧区（右，`span=8`，`sticky`） | 附件面板入口、优先级、影响范围、指派、SLA | 属性与附件，滚动时吸附 |
| D 底部操作条 | 存草稿、提交、取消 | `affix` 到底部，长表单不必滚回顶部 |

移动端：B/C 折叠为单列，侧区改为底部 `Collapse` 面板，底部操作条保持吸附。

---
### 4.2 工单类型选择弹层（替换行内选择器）

现状：`create/page.tsx` 第 387 行 `<span>选择工单类型</span>` 之后直接铺开类型列表，占用首屏主要空间。

目标：改为「已选类型摘要 + 触发按钮」，点击后弹出面板。

- 触发区（常驻首屏，高度 ≤ 56px）：
  - 未选：`<Button type="primary" size="large" icon={<AppstoreOutlined />}>选择工单类型</Button>` + `<Text type="secondary">选择工单类型，填写详细信息后提交</Text>` 提示语保留在此处。
  - 已选：显示类型图标 + 名称 + `<Tag>已选</Tag>` + `更换类型` 链接按钮。
- 弹层：`<Modal title="选择工单类型" width={960} footer={null} open={typePickerOpen}>`，内部为类型卡片网格（`Row gutter={[16,16]}` + `Col xs={24} sm={12} lg={8}`），每张 `Card`：
  - 图标 / 名称 / 一句话描述 / 是否需要审批、是否有必填自定义字段等 `Tag`。
  - `hoverable` + `onClick={handleSelectType}`。
- 搜索与分组：类型数量 > 8 时，顶部加 `Input.Search` 与按业务域分组的 `Segmented`（如 全部 / 服务请求 / 故障 / 变更）。纯前端过滤，不新增接口。
- 选择行为：
  - `handleSelectType` 设置 `ticketTypeId` 并关闭弹层（`setTypePickerOpen(false)`）。
  - **已有输入时的二次确认**：若用户已填标题/描述/自定义字段再更换类型，弹出 `Modal.confirm`（"更换类型会清空当前已填写的自定义字段，是否继续？"）。
  - 自定义字段按新类型的 schema 重新渲染，旧字段值按 `name` 交集保留（避免误清空通用字段）。
- 无可用类型时：弹层内展示 `Empty`，文案引导跳转 `/tickets/types` 创建类型。

`Drawer` 备选：类型卡片信息量大、需要长滚动时用 `Drawer placement="right" width={480}`；默认仍用 `Modal`（居中对齐更符合"选择"语义）。

---
### 4.3 富文本编辑（描述 / 详情字段）

选型：**TipTap 2.x**（`@tiptap/react` + `@tiptap/starter-kit` + 扩展包），理由：

| 方案 | 体积 | 粘贴图片 | 与 antd/React19 兼容 | 结论 |
| --- | --- | --- | --- | --- |
| TipTap | ~120KB gzip（按需扩展） | 原生 `handlePaste` 可控 | 官方支持 | **采用** |
| Quill 2 | ~90KB | 需魔改 clipboard | 一般 | 备选 |
| Slate | ~80KB | 需自写 | 需自维护 | 备选 |

依赖清单（新增，`itsm-frontend/package.json`）：
`@tiptap/react`、`@tiptap/pm`、`@tiptap/starter-kit`、`@tiptap/extension-link`、`@tiptap/extension-image`、`@tiptap/extension-placeholder`、`@tiptap/extension-underline`、`@tiptap/extension-text-align`（可选）、`@tiptap/extension-table*`（可选，二期）。

工具栏（`ToolbarBubble` + 顶部固定条双形态）：加粗、斜体、下划线、删除线、无序/有序列表、引用、行内代码、代码块、链接（弹 `Modal` 输入 URL，校验 `https?://`）、清除格式、撤销/重做。

**安全**：编辑态输出 HTML → 提交前用 `DOMPurify.sanitize(html, { ALLOWED_TAGS: [...], ALLOWED_ATTR: [...] })` 白名单清洗（项目已有 `dompurify` + `@types/dompurify`）；渲染态（详情页、列表摘要）同样二次清洗，禁止 `script`/`iframe`/`on*`/`javascript:`。后端 `service` 层补一条 HTML 白名单校验作为纵深防御（可选，见 §5）。

**纯文本派生**：提交时同时计算 `descriptionText`（`html.replace(/<[^>]+>/g,'')` 折叠空白，截断 500 字）写入工单 `description`/摘要字段，保证列表页、搜索、通知不需要改渲染逻辑。

**降级开关**：`NEXT_PUBLIC_RICH_TEXT=off` 时回退为现有 `Input.TextArea`，便于灰度与快速回滚。

---
### 4.4 粘贴图片 / 拖拽图片

流程（复用现有附件通道，不新增图片专用接口）：

1. TipTap 注册 `editorProps.handlePaste` 与 `handleDrop`：从 `clipboardData.files` / `dataTransfer.files` 中筛出 `image/*`。
2. 立即用 `URL.createObjectURL(file)` 插入占位 `image` 节点（带 `data-uploading="true"`），并叠一层 `Spin`/半透明遮罩，保证"粘贴即可见"的即时反馈。
3. 并发调用现有附件上传接口（同 4.5 的 `TicketAttachment` 通道），拿到 `fileId`/URL 后把占位节点的 `src` 换成服务端地址、移除 `data-uploading`。
4. 失败：占位节点替换为红色错误块（`上传失败，点击重试`），点击重新上传；同时 `message.error` 提示。
5. 单图限制 10MB、仅 `image/png|jpeg|gif|webp`；超限直接 `message.warning` 拒绝，不插入占位。

要点：
- **必须复用附件接口**，让粘贴的图片与"附件"在同一实体里，避免出现"正文图片不随工单走"的孤儿文件。
- 富文本内图片统一走 `ATTACHMENT_BASE_URL`（或后端返回的受控代理地址），不走外链，防止 SSRF 与防盗链失效。
- `handlePaste` 中若同时含 `text/html` 与 `image/*`，优先插入图片、丢弃 HTML（防止 Word/网页带样式污染）。
- 拖拽图片到编辑区与粘贴走同一函数，避免两套逻辑。

---

### 4.5 附件上传

复用已存在的 `TicketAttachmentSection`（前端）与 `TicketAttachment` 后端服务，仅做体验补强：

- 上传方式：`Upload.Dragger`（拖拽区）+ 粘贴 + 点击选择三通道合一。
- 上传时机：**两段式（本期方案）**。现状核实：后端附件路由全部挂在工单维度（`itsm-backend/router/ticket_routes.go:170-175`，`GET/POST/DELETE /tickets/:id/attachments`），**不存在"未建单先上传"的入口**；前端 `TicketAttachmentApi.upload` 也硬编码 `POST /api/v1/tickets/${ticketId}/attachments`（`itsm-frontend/src/lib/api/ticket-attachment-api.ts:60-64`，且注释明确必须走 `httpClient` 以携带 CSRF/租户头）。因此本期流程为：提交工单 → 拿到 `ticketId` → 在创建页的提交后步骤内继续上传附件（失败可重试）→ 再跳转详情页。若产品坚持"先传后提交"，需先落地 §5.2 的草稿上传接口（后端新增，P1）。
- 进度与状态：每个文件一行（名称 / 大小 / `Progress` / 删除 / 重试），失败不阻断其他文件。
- 类型与大小：白名单（文档/表格/图片/压缩包）+ 单文件 ≤ 50MB，`beforeUpload` 校验；`multiple` + `maxCount`。
- 已上传但未提交的附件：仅存在于"提交后步骤"的内存态（`ticketId` 已存在），不做跨会话草稿回显；离开页面（`beforeunload` 或路由变化）时若仍有未完成上传，提示"仍有附件未上传完成"。
- 编辑页：展示既有附件并允许增删，删除需即时调用后端解绑接口。

---
### 4.6 自定义字段渲染升级

现状：`create/page.tsx` 按 `field.type` 分支渲染（约 504 行 `field.type === 'textarea'`）。升级为独立组件 `DynamicFieldRenderer`，按类型映射：

| `field.type` | 渲染 | 说明 |
| --- | --- | --- |
| `text` / `textarea` | `Input` / `Input.TextArea`（`autoSize`） | 保持 |
| `richtext`（新增） | `RichTextEditor` | 后端字段 `type` 枚举需同步 |
| `select` / `multiselect` | `Select`（多值 `mode="multiple"`） | 支持选项搜索 |
| `date` / `datetime` | `DatePicker` / `DatePicker showTime` | `dayjs`（antd v6 内置日期库） |
| `number` | `InputNumber` | 支持 min/max |
| `user` | `UserSelect`（复用现有用户选择组件） | 与指派字段同源 |
| `attachment` | `AttachmentField` | 复用 4.5 上传壳层 |

要点：字段值统一收敛到 `form.setFieldValue(['customFields', field.key], value)`，避免散落的 `setState`；`required` 字段在弹层/步骤提交时统一校验（`form.validateFields`），错误定位到字段并滚动到可视区（`scrollToField`）。字段布局走 4.1 的响应式栅格（`Col` 12/24）。

### 4.7 编辑页（`/tickets/[ticketId]`）复用与差异

复用同一套 `RichTextEditor` + `AttachmentField`，差异点：

- 初始化：从工单详情 `descriptionHtml` 反序列化到编辑器（`editor.commands.setContent(html, false)`，`false` 表示不触发 `onUpdate`，防止脏标记误报）；无 `descriptionHtml` 时回退把纯文本 `description` 转成段落。
- 附件：加载既有附件列表，编辑态允许增删；新增走同一上传接口，删除调用解绑接口（幂等）。
- 部分权限：无编辑权限时整个表单 `disabled`（编辑器 `editable: false`），工具栏隐藏。
- 脏数据保护：`isDirty` 时路由离开弹 `Modal.confirm`；保存成功后重置基线快照。
- 保存语义：仅提交变更字段（`diff`），沿用现有更新接口，避免全量覆盖并发覆盖。

---
## 5. 后端与数据模型配合

### 5.1 存储字段

现状：工单描述为纯文本 `description`（TEXT）。富文本需要额外保存 HTML 与图片引用。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `description` | TEXT | 保留，存编辑器导出的**纯文本**（用于列表摘要、搜索、通知短信/邮件） |
| `description_html` | TEXT / JSONB | 新增，存清洗后的 HTML |
| `description_format` | VARCHAR(16) | 新增，`plain` / `html`，用于灰度回退与兼容旧数据 |

要点：
- 双写策略：新建/更新时同时提交 `description`（富文本转纯文本）与 `description_html`，避免列表页/搜索因改 HTML 而失效。
- 向后兼容：`description_html` 为空时，前端按纯文本渲染（`white-space: pre-wrap`），不做破坏性迁移。
- 迁移脚本：新增列 + 索引（如需全文检索）走 `itsm-backend` 现有 migration 机制（`migration-lint` 校验）。

### 5.2 图片与附件的存储归属

粘贴图片与附件统一走**附件服务**，不引入第二套存储：

1. 粘贴/拖拽 → 前端压缩 → 调现有 `POST /api/v1/tickets/:id/attachments`（工单已创建）→ 返回 `{ id, url, filename, size }`。**本期不新增接口**；如需"未建单先上传"，见本节末尾的接口提案。
2. 富文本中插入 `<img src="..." data-attachment-id="...">`，`src` 用附件下载地址而非 base64。
3. 附件与工单同生命周期：上传即绑定 `ticket_id`，**不产生孤儿附件**，因此本期无需定时清理任务。
4. 编辑器内删除图片时，前端调用现有 `DELETE /api/v1/tickets/:id/attachments/:attachmentId` 解绑并删除；编辑期图片已属该工单，不存在"待绑定"中间态。

**（P1，本期不实施）如需支持"先传后提交"**：现状没有"待绑定附件"上传接口（核实见 §4.5）。粘贴图片/拖拽文件要支持"先传后提交"，需后端新增草稿上传接口，建议 `POST /api/v1/tickets/attachments/draft`（返回 `{ id, url, filename, size }`）+ `POST /api/v1/tickets/:id/attachments/bind`（回填 `ticket_id`），复用现有 `TicketAttachmentHandler` 的存储与权限校验，并配套孤儿附件定时清理。

好处：权限、病毒扫描、配额、审计全部复用附件服务既有能力。

### 5.3 清洗与转义（服务端兜底）

前端 DOMPurify 只做体验层防护，**服务端必须二次清洗**（不可信任客户端）：

- Go 侧使用白名单清洗（如 `bluemonday` 的 UGC/自定义策略），仅允许 `p, br, strong, em, u, s, code, pre, blockquote, ul, ol, li, h1-h6, a[href], img[src|alt|data-attachment-id], table` 等。
- `a` 的 `href` 仅允许 `http/https/mailto`；`img` 的 `src` 仅允许本站附件域，禁止外链与 `data:`（`data:` 可保留但需限制体积，默认拒绝）。
- 清洗后若结果为空，回退为纯文本段落。
- 在工单历史/评论/通知的**所有渲染出口**统一走同一清洗策略，避免遗漏（渲染层再加一层前端 DOMPurify 双保险）。

### 5.4 输入边界与安全加固

- 体积与数量上限在接口层显式声明：`description_html` 单次请求上限（建议 512 KB）、附件单文件/总数量上限沿用附件服务配置；超限返回 413/422 并给出可读提示，避免写入超大 payload。
- URL 协议白名单统一在清洗前判定：拒绝 `javascript:`、`vbscript:`、`file:`、协议相对 `//`，`data:` 默认拒绝。
- 粘贴的 base64 图片一律前端转附件上传（`data:` 不落库）：上传失败则丢弃该图并提示重试，从源头杜绝内联大图。
- 渲染出口 CSP 基线：`img-src 'self' <附件域名>`、`object-src 'none'`，`script-src` 不允许 `unsafe-inline`；基线由网关统一注入并在本文档登记。
- 幂等与一致性：附件绑定/删除按 `attachmentId` 幂等（重复绑定返回既定语义而非 500）；保存时 `description` 与 `description_html` 在同一事务内提交，不出现「HTML 已更新、纯文本未更新」的部分成功。
- 可观测：清洗拒绝次数、附件上传失败率、粘贴转附件成功率、编辑页保存失败率暴露为指标，供灰度期观察。

---

## 6. 实施计划

### 6.1 阶段与里程碑

| 阶段 | 内容 | 主要产出 | 预估 |
| --- | --- | --- | --- |
| P0 组件基建 | 封装富文本编辑器、抽取附件字段壳层、新建类型选择弹层 | `RichTextEditor`、`AttachmentField`、`TicketTypePickerModal` 三个独立组件（可单独评审/复用） | 0.5 人日 |
| P1 创建页改造 | 接入三件套、栅格化布局重构、类型改为「已选卡片 + 更换」 | `tickets/create/page.tsx` 新版本 | 1 人日 |
| P2 后端与数据 | 富文本字段落库、上传接口复用/校验、服务端清洗 | 迁移脚本、DTO 变更、`sanitize` 工具 | 1 人日 |
| P3 编辑页复用 | 详情/编辑页接入同一套组件，初始化与脏数据保护 | `tickets/[ticketId]/page.tsx` | 0.5 人日 |
| P4 验证与回归 | 用例执行、浏览器兼容、粘贴/上传边界验证 | 测试记录与验收结论 | 0.5 人日 |

合计约 **3.5 人日**（前端 2.5，后端 1）。P0 与 P2 可并行。

### 6.2 变更清单（文件级）

| 文件 | 类型 | 变更要点 |
| --- | --- | --- |
| `itsm-frontend/src/components/business/RichTextEditor.tsx` | 新增 | Tiptap 封装；暴露 `value` / `onChange` / `placeholder` / `minHeight`；内置粘贴图片与工具栏 |
| `itsm-frontend/src/components/business/AttachmentField.tsx` | 新增（自 `TicketAttachmentSection` 抽取） | 保留原上传/下载/删除能力，改为受控值 `value` + `onChange`，可嵌入表单 |
| `itsm-frontend/src/components/business/TicketTypePickerModal.tsx` | 新增 | `open` / `onCancel` / `value` / `onChange`；分组与键鼠可访问 |
| `itsm-frontend/src/app/(main)/tickets/create/page.tsx` | 修改 | 移除常驻类型列表；接入弹层、富文本、附件；栅格布局 |
| `itsm-frontend/src/app/(main)/tickets/[ticketId]/page.tsx` | 修改 | 复用同一套输入组件；HTML 反序列化与脏数据保护 |
| `itsm-frontend/src/components/business/TicketAttachmentSection.tsx` | 修改或降级为包装 | 保持既有唯一引用点不破，内部改为委托 `AttachmentField` |
| `itsm-frontend/package.json` | 修改 | 新增 `@tiptap/react`、`@tiptap/starter-kit`、所需扩展 |
| 后端迁移 + 工单 DTO | 修改 | 新增 `description_html`；请求/响应双写兼容 |

### 6.3 提交拆分建议

按「组件 → 创建页 → 后端 → 编辑页」切分为 4 个可独立回滚的提交；前端组件阶段不改变现有页面行为，保证任一提交回滚都不产生半成品页面。

### 6.4 测试与质量门禁

| 层级 | 覆盖点 | 门禁 |
| --- | --- | --- |
| 后端单测 | 清洗白名单（`script` / `on*` / 外链 `img` / `javascript:` URL）、纯文本回退、双写一致性 | 迁移与清洗用例随 PR 必跑；`migration-lint` 作为 CI gate，迁移不合规即阻断合并 |
| 前端单测 | 粘贴转附件、上传失败重试、类型弹层键鼠交互与焦点管理 | `RichTextEditor` / `AttachmentField` / `TicketTypePickerModal` 三组件均有组件级用例 |
| E2E | 创建页粘贴上传→提交→详情页回显；编辑页加载→改单字段→保存请求体仅含变更；1440/1280 布局无横向滚动 | 关键路径 smoke，回归期每次发版执行 |
| 安全用例 | 构造恶意 payload 直连接口；`data:` 与超大 HTML 边界 | 用例入库，作为 AC-7 的自动化版本 |

**完成定义（Definition of Done）**——本次落地的 PR 需同时满足：

1. 上述各层级用例全绿，`migration-lint` 通过；
2. §7 的 AC-1 ~ AC-10 与 NF-1 ~ NF-4 均有对应验证记录（用例或走查截图）；
3. §6.6 观测指标在看板可查，且开关默认处于「仅写入 HTML、渲染保持纯文本」的初始档位；
4. §9 登记入口（文档索引、前端 README 组件清单、安全与隐私检查项）已同步更新；
5. 本文件 `Status: draft` 转 `current`，§10 决策表结论已回写。

### 6.5 灰度与回滚

- 以 `description_format` 作为开关：先仅写入 `description_html`、详情页继续渲染纯文本（观察期无回归），再按灰度切换详情页渲染 HTML。
- 出问题时的最快回滚是前端渲染层切回纯文本 + 动态导入的编辑器组件直接下线（`next/dynamic` 的 `ssr: false` 组件可整体摘除），无需回滚数据库。
- 每个阶段的回滚判定与责任人写入提交说明，避免「功能已合并但无人敢回滚」。

---

### 6.6 可观测性与灰度观测指标

| 指标 | 采集点 | 用途 / 阈值 |
| --- | --- | --- |
| `ticket_attachment_upload_total{result}` | 附件上传接口 | 成功率 < 98%（5 分钟窗口）告警 |
| `ticket_attachment_upload_bytes` | 同上 | 观察粘贴图片体积分布，校准前端压缩阈值 |
| `ticket_description_sanitize_rejected_total{reason}` | 服务端清洗 | 非零即人工核查（潜在 XSS 探测） |
| `ticket_description_format_total{format}` | 工单创建/更新 | 观察 `plain` / `html` 占比，作为灰度推进依据 |
| 前端 `editor.upload.failed` / `editor.paste.rejected` | 编辑器组件埋点 | 失败率与重试成功率，用于回归判断 |

- 灰度按租户/用户维度放量（`description_format` 仅对新数据生效）；异常时优先回退到「仅落库不展示 HTML」而非回滚代码（见 §8）。
- 前端错误上报只携带错误码与文件元信息（体积/类型），**不上传文件内容与 HTML 正文**。

## 7. 验收标准

| 编号 | 验收点 | 验证方式 |
| --- | --- | --- |
| AC-1 | 描述区支持加粗/斜体/列表/标题/引用/代码块/链接，且回显与提交一致 | 手工用例：编辑各样式 → 提交 → 详情页对比 |
| AC-2 | 截图后 `Ctrl+V` 直接粘贴，图片上传成功并插入编辑器对应位置 | 手工用例：剪贴板粘贴 PNG；断网时给出失败提示且不产生脏字符 |
| AC-3 | 支持拖拽与点击上传附件，可删除、可下载，数量/体积超限被拦截 | 手工用例 + 边界值（超限文件、重名文件） |
| AC-4 | 「选择工单类型」不再常驻占位；点击按钮弹出面板，选中后页面仅显示已选类型摘要 | 视觉检查 + 首次进入页面时的首屏高度对比 |
| AC-5 | 1440 / 1920 / 1280 及以上宽度下表单单列或双列自适应，无横向滚动 | 浏览器缩放与分辨率切换检查 |
| AC-6 | 编辑页可加载既有富文本与附件，保存仅提交变更 | 手工用例：加载 → 改动单字段 → 抓包确认请求体 |
| AC-7 | 提交的 HTML 经服务端清洗，`script` / `on*` / 外链 `img` 被剥离 | 接口层用构造的恶意 payload 直连验证 |
| AC-8 | 富文本为空时可正常提交纯文本回退，历史工单渲染不受影响 | 回归用例：打开 3 条历史工单详情 |
| AC-9 | 回滚/降级开关生效：关闭渲染开关后详情页回退纯文本渲染，创建页不加载编辑器（`next/dynamic` 不请求 chunk）且仍可正常提单 | 灰度走查：切换开关 → 验证创建 / 详情 / 编辑三条路径 |
| AC-10 | 超限或非法内容被拒时给出可读错误提示，且不清空用户已输入内容 | 边界用例：512 KB 上限上下各一例 + 含 `javascript:` 链接的 payload |

### 7.1 非功能验收

| 编号 | 验收点 | 验证方式 |
| --- | --- | --- |
| NF-1 | 工具栏、类型弹层、附件区可全键盘操作；焦点可见、不逃逸（打开入弹层、关闭回到触发器） | 键盘走查 + 组件级用例 |
| NF-2 | 编辑器仅在工单创建/编辑路由加载，首屏 JS 增量不超过约定预算 | 构建产物对比（Next.js 构建报告） |
| NF-3 | 清洗拒绝、附件上传失败、粘贴转附件成功率、保存失败率均有指标可查 | 指标看板抽查 |
| NF-4 | 提交体量受控：单次 `description_html` 超限被拒并返回可读错误 | 边界用例（限额上下各一例） |

---

## 8. 风险与回滚

| 风险 | 影响 | 缓解 | 回滚 |
| --- | --- | --- | --- |
| 新增编辑器依赖体积偏大 | 首屏包体增长 | 编辑器组件动态导入（`next/dynamic`，`ssr: false`），仅工单创建/编辑路由加载 | 移除依赖与组件，恢复 `Input.TextArea` |
| 图片粘贴产生大体积 base64 | 请求体过大、接口超限 | 前端压缩与体积上限，超限转附件上传并插入引用链接 | 关闭粘贴功能开关 |
| 存储字段变更影响历史数据 | 详情渲染异常 | 保留 `description` 纯文本，`description_html` 可空；渲染层「有 HTML 用 HTML，否则纯文本」 | 停用 HTML 渲染分支，仅落库不展示 |
| 类型弹层改动导致校验遗漏 | 未选类型即提交 | 提交前统一 `validateFields`，错误定位到弹层触发器 | 恢复常驻选择器布局（组件仍在） |

---

## 9. 关联文档与登记

- 文档规范：`itsm/docs/documentation-governance.md`（本文件 `Status: draft`，落地后转 `current`）。
- 同类「诊断与设计」文档：`itsm/docs/product/workflow-console-diagnosis-and-design.md`。
- 决策记录（ADR）：选型与兼容策略如需长期留存，按 `itsm/docs/architecture/adr-*.md` 体例补一份 ADR（下一个可用编号；候选主题「富文本存储格式与 `description_html` 兼容期退出策略」）。本期先记录于 §5.1、§5.3 与 §10，不阻断实施。
- 落地时需同步登记的入口：
  - `itsm/docs/README.md`「产品与架构」索引（新增本文件条目）；
  - 前端 README 的组件清单（`RichTextEditor`、`AttachmentField`、`TicketTypePickerModal`）；
  - 安全与隐私检查项：清洗白名单、CSP 基线、上传体积与类型上限（对应 §5.3、§5.4）。
- 兼容与废弃（双写退出条件）：`description`（纯文本）与 `description_html` 属过渡期双写。当详情页、列表摘要、通知模板、导出、检索等全部下游消费方均按「有 HTML 用 HTML，否则纯文本」渲染，且灰度租户占比达 100%、观察 1 个迭代无 HTML 渲染事故后，由前端负责人发起评审决定是否收敛为单一字段；未达成前保持双写。回退动作见 §8。

---

## 10. 待确认事项

以下事项均已给出**默认取值**：未在「结论时限」前提出异议即按默认推进，保证实施不被阻塞；结论落地后回写本节，并同步 §5、§6.4。

| # | 决策项 | 默认取值（未反馈即执行） | 影响面 | Owner | 结论时限 |
| --- | --- | --- | --- | --- | --- |
| D-1 | 富文本是否支持表格与图片缩放 | 不支持表格；图片保留原始尺寸（`width` / `height` 属性透传，不写入内联样式） | 扩展选型（保持最小依赖集）、清洗白名单 | 前端负责人 | P0 组件基建启动前 |
| D-2 | 编辑态是否允许删除既有附件 | 允许删除，走解绑接口（幂等）；写工单历史，记录操作人与时间 | 权限模型、审计留痕、解绑接口 | 后端负责人 | P1 编辑页复用前 |
| D-3 | 历史工单详情页是否同步切 HTML 渲染 | 同步切换（「有 HTML 用 HTML，否则纯文本」）；历史数据无 HTML 时行为不变 | 详情页渲染分支、灰度开关、回退路径 | 前端负责人 | P2 详情页接入前 |
| D-4 | 附件「先传后提交」（草稿上传 + 提交时绑定） | 本期不做，采用两段式（先建单再上传）。若产品要求，需新增 `POST /api/v1/tickets/attachments/draft` 与 `.../bind`（见 §5.2），并评估后端排期与孤儿附件清理 | 后端接口、附件生命周期、清理任务 | 产品 + 后端负责人 | P2 结束前评审 |
