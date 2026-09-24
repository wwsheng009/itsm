# 通用附件 / 富文本控件下沉：改造方案与任务拆解

> 文档类型：技术方案 + 实施计划（Proposed）
> 适用范围：`itsm-frontend`、`itsm-backend`
> 编制日期：2026-09-22
> 版本：v1.0（已冻结，2026-09-22；四项 P0 决策落定：单文件 10MB 维持 / 评论附件纳入 / 错误码按最佳实践 / 兜底权限码启用；修订记录见 §11.4）
> 目标读者：前端、后端、测试、SRE
> 关联文档：`docs/architecture/ticket-create-page-rich-input-optimization.md`、`docs/api-reference.md`、`docs/acl-manifest.yaml`、`ROADMAP.md`（迁移与升级安全）

---

## 0. 范围与非目标

**范围**

- 单据附件能力：上传、列表、下载、预览、删除、审计。
- 富文本能力：编辑器壳、内嵌图片上传、图片查看/缩放/对齐、HTML 白名单净化。
- 覆盖域：工单（ticket，含**评论附件**）、知识库（knowledge）、服务请求（service_request），并为事件/问题/变更/发布预留 `biz_type` 扩展位。
- 前端控件下沉（`components/business/*` → `components/common/*`）与后端通用附件服务化。

**非目标**

- 富文本协同编辑（多人实时）、版本对比。
- 自研病毒扫描引擎；**复用**现网既有 `AttachmentVirusScanner` 接入点（`service/ticket_attachment_service.go:31-61`）与开关，新通用服务必须保留该接口（默认 noop），接入外部引擎另行立项。
- 对象存储（S3/OSS）的部署与运维；**不迁移**任何已有文件。现状不存在存储抽象（本地磁盘直写），本次仅在本地实现之上抽出 `StorageProvider` 接口（见 §5.3）。

**结论先行（TL;DR）**

1. **"封装了一半，但没有下沉"**：`RichTextEditor` 与 `AttachmentField` 已经是"能力可注入"的受控组件，但它们停在 `components/business/`（业务层目录），跨域复用的方式是"知道内部路径再 import"，不是稳定的公共契约。
2. **上传/下载链路没有通用后端**：全仓库检索确认，Go 后端不存在 `/api/v1/attachments/upload` 路由注册；`knowledge-base-api.ts#uploadImage()` 指向的 `POST /api/v1/knowledge/articles/upload/image` 同样未注册。也就是说：
   - `RichTextEditor` 的 `defaultUploadImage`（`POST /api/v1/attachments/upload`）是**断链的默认实现**，只有业务方显式注入 `onUploadImage` 时才可用（工单详情编辑弹层就是这样绕过的）；
   - 知识库侧 `uploadImage()` 目前是**没有后端落点**的调用，属未闭环能力。
   - 修正（v0.9）：可用链路实际有两处——工单详情编辑弹层（直传，`TicketDetail.tsx:521-536`）与工单创建页（先 `blob:` 占位、创建成功后上传回写，`create/page.tsx:241-259` + `:433-462`）。评论附件：`collaboration-api.ts:354-382` 的两个方法是**无 UI 调用的死代码**；后端评论附件的校验/持久化已部分存在（`ticket_comment_service.go:65-77`），缺口在前端接入（详见 §1.1，v1.0 纳入范围）。
3. 附件数据模型与鉴权只服务工单：`ticket_attachments` 表（含 `ticket_id` 外键列）+ `/tickets/:id/attachments*` 路由 + `ticket:read/create/delete` 权限。
4. 改造主线：**单一契约 + 单一存储服务 + 按宿主资源鉴权**。前端下沉为 `components/common/{rich-text,attachment}`，重复上传实现（`ticket-attachment-api` / `ticket-api`，外加 `collaboration-api` 死代码）收敛为一份，业务侧只保留"注入宿主上下文"的薄适配层；后端新增多态 `attachments` 资源，旧工单端点保持行为不变并可平滑重定向。

---

## 1. 现状诊断

### 1.1 前端资产盘点（代码坐标）

| 资产 | 路径 | 现有能力 | 复用情况 | 主要问题 |
|:---|:---|:---|:---|:---|
| 富文本编辑器 | `itsm-frontend/src/components/business/RichTextEditor.tsx` | TipTap v2；受控 `value/onChange`；粘贴/拖拽上传；`sanitizeRichTextHtml` 白名单净化；输出 `<img src data-attachment-id>`；`maxImageSizeMB`/`maxImageCount`/`onUploadingChange` | 工单详情编辑弹层（`dynamic(() => import(...))`）、工单创建页（`staged-images` 两阶段）、`DynamicFieldRenderer` | 位于 `business/`；`defaultUploadImage` 指向未注册端点；未注入上传函数时能力静默失效 |
| 附件字段壳 | `components/business/AttachmentField.tsx` | 受控 `value/onChange`；暂存模式（创建页先存 `File`，拿到宿主 ID 后统一上传）与即时模式（注入 `uploader`）；进度/失败重试；扩展名白名单 | 工单创建页、`DynamicFieldRenderer` | 同上；无统一的"列表/预览"展示契约 |
| 富文本配套 | `components/business/RichTextEditorResizableImage.ts`、`RichTextEditorImageMenu.tsx`、`RichTextImageViewer.tsx` | 图片缩放/对齐/还原/删除；详情页多图查看、键盘与遮罩退出 | 工单详情 | 与编辑器一起才能下沉，需同批迁移 |
| 工单附件 UI | `components/business/TicketAttachmentSection.tsx`、`components/business/detail-tabs/AttachmentPanel.tsx` | 附件列表/下载/删除 | 仅工单 | 其它域无对应展示层 |
| 工单附件 API | `lib/api/ticket-attachment-api.ts`、`lib/api/ticket-api.ts`（`uploadTicketAttachment`）、`lib/api/collaboration-api.ts:354-382`（评论附件，**无调用方**） | `/api/v1/tickets/:id/attachments` 系列调用；`collaboration-api` 另调 `DELETE /api/v1/attachments/:id` | 仅工单 | 同一能力存在三份实现（其中 `collaboration-api` 为死代码），字段命名与错误处理有漂移风险；其上传携带 `commentId` 但后端未解析、删除调用无后端路由 |
| 知识库上传 API | `lib/api/knowledge-base-api.ts`（`uploadImage`，字段名 `image`） | `POST ${ARTICLES_PREFIX}/upload/image` | 知识库 | 后端无该路由；字段名与通用契约（`file`）不一致 |
| 代理层 | `itsm-frontend/src/app/api/[...path]/route.ts` | 反向代理到 `ITSM_BACKEND_URL`（默认 `http://localhost:8090`），带 JWT 校验、路径黑名单 | 全站 | 代理层不做能力兜底，后端缺路由即 404 |
| HTTP 客户端 | `lib/api/http-client.ts` | `get/post/put/patch/delete/postFormDataWithProgress/uploadFile/getPaginated/batchOperation`；CSRF 轮换重试、租户头注入、进度回调 | 全站 | 已具备通用附件 API 所需全部原语 |

调用点事实：

- `TicketDetail.tsx:521-536` 的 `handleEditImageUpload` 直连 `TicketAttachmentApi.uploadAttachment(ticketId, file)`，返回 `preview` 地址后插入编辑器——工单详情编辑弹层的可用链路。
- 工单创建页走"两阶段"链路：`create/page.tsx:241-259` 的 `handleEditorImageUpload` 先返回 `blob:` 占位地址（工单尚不存在），创建成功后统一上传附件并把 `descriptionHtml` 中的占位替换为正式 `preview` 地址（`create/page.tsx:433-462`，辅助函数见 `lib/rich-text/staged-images.ts`）。
- `DynamicFieldRenderer.tsx` 已按字段类型分派 `richtext` / `attachment`，说明"字段级复用"的入口已经存在，缺的是稳定公共契约与通用后端。
- `components/knowledge/` 下没有 `RichTextEditor` 引用；知识库的富文本接入尚未真正铺开（这也降低了本次下沉的迁移面）。
- `collaboration-api.ts:354-382` 的 `uploadAttachment`/`deleteAttachment` 是**无任何 UI 调用的死代码**（全仓检索仅自测引用 `deleteAttachment`）：上传表单携带 `commentId`，但后端 `handlers/ticket_attachment` 未解析该字段，删除调用的 `DELETE /api/v1/attachments/:id` 无后端路由。评论附件的真实契约在 `TicketCommentApi`：创建/更新请求携带 `attachments?: number[]`（`ticket-comment-api.ts:15,33`）；后端创建时校验并持久化（`service/ticket_comment_service.go:65-77`，落 `ent/schema/ticket_comment.go:34-36` 的 `attachments` JSON 列；DTO 见 `dto/ticket_comment_dto.go:14,32`），但更新路径不处理附件（`:236` 传 nil）、校验未限制用途、前端 UI（`TicketCommentSection`）从未上传/传参/展示。**v1.0 决策：评论附件纳入本次范围**（见 §3.2 注、BE-9/FE-9）。

### 1.2 后端资产盘点

| 资产 | 路径 | 说明 |
|:---|:---|:---|
| 附件处理器 | `itsm-backend/handlers/ticket_attachment/handler.go` | `ListTicketAttachments` / `UploadAttachment` / `DownloadAttachment` / `PreviewAttachment` / `DeleteAttachment`（2026-09-02 自 `controller/` 迁移，处理器只做参数解析与响应封装） |
| 附件路由 | `router/ticket_routes.go:170-180` | 5 个端点；鉴权分别为 `ticket:read`（列表/下载/预览）、`ticket:create`（上传）、`ticket:delete`（删除）；下载/预览兼容"数字 ID"与历史存储文件名（`handler.attachmentRef`） |
| 业务服务 | `service/ticket_attachment_service.go` | 上传/列表/删除等业务逻辑与存储写入 |
| 数据模型 | `ent/schema/ticket_attachment.go` | `ticket_id`、`file_name`、`file_path`、`file_url`、`file_size`、`file_type`、`mime_type`、`uploaded_by`、`tenant_id`、`created_at` |
| 响应 DTO | `dto/ticket_attachment_dto.go` | `TicketAttachmentResponse`（camelCase：`id/ticketId/fileName/filePath/fileUrl/fileSize/fileType/mimeType/uploadedBy/uploader/createdAt`） |
| 权限权威源 | `internal/authz/catalog.go` | 权限码唯一源 `Definitions()`；新增码必须满足"独立资源面 + 至少一个路由 `RequirePermission` 引用 + 至少一个角色绑定"，并有守卫测试把关 |
| 鉴权中间件 | `middleware/rbac.go:975` | `RequirePermission(resource, action)`；路由声明经 `cmd/authz-gen` 单向 codegen 为预检映射 `ResourceActionMap` |
| 迁移机制 | `itsm-backend/migrations/` + `migration/` + `cmd/migration-lint` | "目录即真相"：`YYYYMMDD_<snake_case>.sql`，可选 `_down.sql`；已有 `*_expand.sql` / `*_contract.sql` 分阶段先例；ledger 调和与 lint 门禁已就绪 |
| 部署迁移开关 | `internal/bootstrap/app.go` | `ITSM_AUTO_MIGRATE` 仅允许 bootstrap 一次性任务开启，Web 长驻进程禁止 |

**缺口（实测证据）**

- 全量检索 `itsm-backend` Go 文件：不存在 `"/attachments"` 路由组，也不存在 `attachments/upload`、`upload/image` 字面量注册。
- 前端代码引用但后端无路由的端点共三个：`POST /api/v1/attachments/upload`（`RichTextEditor` 默认实现）、`POST /api/v1/knowledge/articles/upload/image`（知识库）、`DELETE /api/v1/attachments/:id`（仅存于 `collaboration-api` 死代码与其自测）。
- `router/` 目录下无任何 `upload|image` 路由声明（知识库路由文件只声明了文章 CRUD/评论/分类/搜索等）。
- 结论：**富文本图片上传在无宿主注入时无后端落点；知识库上传未实现；评论附件后端已部分可用、前端未接入**。这是本次改造必须消灭的第一类缺陷。

### 1.3 能力 × 域 差距矩阵

| 能力 | 工单 | 知识库 | 服务请求 | 事件/问题/变更/发布 | 说明 |
|:---|:---:|:---:|:---:|:---:|:---|
| 附件上传 | ✅ | ❌ | ❌ | ❌ | 仅 `/tickets/:id/attachments` |
| 附件列表/下载 | ✅ | ❌ | ❌ | ❌ | 权限与租户校验仅工单域 |
| 内嵌图片上传（富文本） | ⚠️ 部分 | ❌ | ❌ | ❌ | 工单详情编辑弹层（直传）与创建页（先占位后上传）可用；知识库/其它域断链；评论附件后端已部分具备（校验+持久化），前端 UI 未接入（v1.0 纳入，BE-9/FE-9） |
| 图片预览（inline） | ✅ | ❌ | ❌ | ❌ | `/tickets/:id/attachments/:ref/preview` |
| 图片查看器/缩放 | ✅ | ❌ | ❌ | ❌ | 组件在 `business/`，未下沉 |
| 附件删除 | ✅ | ❌ | ❌ | ❌ | 无软删与引用保护（富文本引用会裂图） |
| 上传配额/类型策略 | ⚠️ 硬编码 | ⚠️ 硬编码 | ⚠️ 硬编码 | ⚠️ 硬编码 | 前端常量白名单；后端策略不统一（`system_config_service.go` 另有 `maxFileSize`/`allowedFileTypes` 配置项未打通） |
| 审计 | ⚠️ 间接 | ❌ | ❌ | ❌ | 无统一附件审计事件 |

### 1.4 根因归纳

1. **数据模型按宿主建模**：`ticket_attachments.ticket_id` 决定了一张表一个域，无法多态复用。
2. **前端只做了"注入口"没有做"公共契约"**：`onUploadImage` / `uploader` 是临时口子，缺 Provider、缺默认实现、缺目录级下沉；同一能力已衍生出三份实现（含创建页两阶段逻辑；其中 `collaboration-api` 版本无调用方），于是每个域要么重复实现、要么断链。
3. **鉴权与路由绑定宿主**：`RequirePermission("ticket", ...)` 静态声明，没有"宿主资源解析器"，导致新域接入必须先复制路由与权限判断。
4. **能力登记缺失**：富文本默认上传与知识库上传两个端点从未登记进路由/权限表，长期以"待实现"状态存在且无 guard 拦截。

---

## 2. 目标架构

### 2.1 分层设计

```text
业务单据页（ticket / knowledge / service_request / ...）
  └── features/<domain>            # 宿主上下文适配：bizType + bizId + 宿主权限
        ├── components/common/rich-text     # RichTextEditor / Viewer / 图片扩展（无业务 API 依赖）
        └── components/common/attachment    # AttachmentField / List / Preview / Provider
              └── lib/upload               # AttachmentUploader 抽象 + useAttachmentUploader
                    └── lib/api/http-client → /api/v1/attachments*（或域内别名路由）

后端
  router/attachment_routes.go  +  router/<domain>_routes.go（域内别名，静态权限）
        └── handlers/attachment/handler.go        # 参数解析/响应封装
              └── service/attachment_service.go   # 存储抽象、配额、审计、引用保护
                    └── ent/schema/attachment.go  # 多态：biz_type / biz_id / usage
```

### 2.2 组件清单与职责（目标态）

| 目标路径 | 职责 | 来源 |
|:---|:---|:---|
| `components/common/rich-text/RichTextEditor.tsx` | 富文本受控编辑；粘贴/拖拽上传；净化输出 | 自 `business/RichTextEditor.tsx` 下沉 |
| `components/common/rich-text/RichTextEditorResizableImage.ts` | 图片缩放/对齐属性扩展 | 同批下沉 |
| `components/common/rich-text/RichTextEditorImageMenu.tsx` | 选中图片浮动菜单 | 同批下沉 |
| `components/common/rich-text/RichTextImageViewer.tsx` | 详情页图片查看器 | 同批下沉 |
| `components/common/attachment/AttachmentField.tsx` | 附件字段壳（暂存/即时双模式） | 自 `business/AttachmentField.tsx` 下沉 |
| `components/common/attachment/AttachmentList.tsx` | 附件列表展示（新增，抽自 `TicketAttachmentSection`/`AttachmentPanel` 公共部分） | 新增 |
| `components/common/attachment/AttachmentProvider.tsx` | 应用级上传器/限额/错误提示注入（新增） | 新增 |
| `lib/upload/attachment-uploader.ts` | `AttachmentUploader` 默认实现 + 类型守卫（新增） | 新增 |
| `lib/api/attachment-api.ts` | 通用附件 API 客户端（单份，替换工单双客户端） | 新增 |

**兼容策略**：迁移期在旧路径保留 `export * from '@/components/common/...'` 的 re-export 一个发布周期，随后删除；`business/` 下最终只留 `TicketAttachmentSection` 等宿主专属 UI。

### 2.3 关键设计决策

| 编号 | 决策 | 选项 | 结论与理由 |
|:---|:---|:---|:---|
| D1 | 数据模型 | 每域一张表 / 多态单表 | **多态单表 `attachments`**：`biz_type + biz_id + usage`，避免 N 份存储/鉴权/审计逻辑 |
| D2 | 内嵌图片归属 | 独立图片表 / 复用附件表 | **复用附件表 + `usage=inline_image`**；与 `data-attachment-id` 现有约定一致 |
| D3 | 鉴权方式 | 通用 `attachment:*` / 宿主资源映射 | **宿主资源映射为主**（工单行为完全不变）；**v1.0 决策：启用兜底 `attachment:read/write/delete`**，按 `catalog.go` 三条件登记（§4.3，P0-3） |
| D4 | 路由形态 | 通用路由动态解析权限 / 域内别名静态声明 | **域内别名为主**（`/knowledge/articles/:id/attachments*`），保持 `RequirePermission` 静态声明与 codegen 预检一致；通用路由仅服务已绑定宿主的 `inline_image` |
| D5 | 历史 URL | 直接切换 / 重定向兼容 | **重定向兼容**：历史富文本 HTML 中的 `/tickets/:id/attachments/:ref/preview` 永不失效（至少覆盖一个 LTS 周期） |
| D6 | 编辑器默认上传 | 保留指向未注册端点的默认实现 / 显式注入 | **删除断链默认实现**：未配置上传能力时禁用图片按钮并告警（fail-fast），不给用户 404 |
| D7 | 迁移方式 | 一次性切换 / expand-contract | **expand-contract**：建表与回填（expand）→ 双写 → 读切换 → 停写旧表 → 清理（contract），复用仓库既有先例 |

---

## 3. 接口契约

### 3.1 前端公共契约（TypeScript）

```ts
// lib/upload/types.ts（公共类型，业务域不得私自定义副本）
export type AttachmentUsage = 'attachment' | 'inline_image' | 'comment_attachment';

export interface AttachmentHostContext {
  bizType: string;      // 'ticket' | 'knowledge_article' | 'service_request' | ...
  bizId: number;        // 宿主单据 ID
  usage?: AttachmentUsage; // 默认 'attachment'
}

export interface AttachmentRef {
  id: number;
  bizType: string;
  bizId: number;
  usage: AttachmentUsage;
  fileName: string;
  fileSize: number;
  mimeType: string;
  fileUrl?: string;
  previewUrl?: string;   // inline 预览地址（图片用）
  sha256?: string;
  uploadedBy?: number;
  createdAt?: string;
}

/** 唯一上传入口类型；所有组件/域共用 */
export type AttachmentUploader = (
  file: File,
  host: AttachmentHostContext,
  onProgress?: (percent: number) => void
) => Promise<AttachmentRef>;

export type AttachmentDeleter = (attachment: AttachmentRef) => Promise<void>;
```

```ts
// components/common/rich-text/RichTextEditor.tsx
export interface RichTextEditorProps {
  value?: string;
  onChange?: (html: string) => void;        // 已净化
  onBlur?: () => void;
  placeholder?: string;
  disabled?: boolean;
  minHeight?: number;                        // 默认 220
  onUploadingChange?: (uploading: boolean) => void;
  /** 富文本图片上传；由 AttachmentProvider 或属性显式注入 */
  uploadImage?: (file: File) => Promise<UploadedImage>; // { url, id?, name? }
  /** @deprecated 兼容别名，物化一个发布周期后移除 */
  onUploadImage?: (file: File) => Promise<UploadedImage>;
  maxImageSizeMB?: number;                   // 默认 10
  maxImageCount?: number;                    // 默认 9
  hideToolbar?: boolean;
  dataTestId?: string;
}
```

```ts
// components/common/attachment/AttachmentField.tsx
export interface AttachmentFieldProps {
  value?: AttachmentFieldItem[];             // 受控
  onChange?: (items: AttachmentFieldItem[]) => void;
  disabled?: boolean;
  maxCount?: number;                         // 默认 10
  maxSizeMB?: number;                        // 默认 10（v1.0 统一上限）
  accept?: string;                           // 默认 ACCEPTED_ATTACHMENT_EXTENSIONS
  host?: AttachmentHostContext;              // 暂存模式回传宿主信息
  uploader?: AttachmentUploader;             // 注入即"即时模式"
  onDeleteUploaded?: AttachmentDeleter;
  title?: React.ReactNode;
  showTitle?: boolean;
  hint?: string;
  dataTestId?: string;
}
```

契约要点：

1. **受控优先**：`value` / `onChange` 是唯一状态通道，组件内部不缓存宿主数据。
2. **宿主上下文显式**：组件不读路由、不读全局 store；`bizType/bizId` 由业务页注入。
3. **单一上传入口**：`AttachmentUploader` 只允许一份默认实现（`lib/upload/attachment-uploader.ts`），业务域不得再直连 `httpClient` 上传。
4. **断链规避**：未提供 `uploadImage`/`uploader` 时，图片按钮与拖拽上传入口禁用，并在开发环境 `console.warn`；不再发起必然 404 的请求。

### 3.2 通用附件 REST 契约

统一前缀 `/api/v1`，沿用现有响应包装（`code` / `message` / `data`）与 `httpClient` 解包约定；`code` 为 **int**，成功一律 `HTTP 200 + code=0`（`common.Success` 固定 200，不使用 201/204）；错误码与 HTTP 映射见 §3.4；时间字段 ISO-8601；ID 为整数。

| # | 方法与路径 | 用途 | 权限（宿主映射） | 请求 | 成功响应 |
|:--|:---|:---|:---|:---|:---|
| A1 | `POST /attachments` | 上传并绑定宿主 | 宿主上传动作，见 §4 | multipart：`file`(必填)、`bizType`(必填)、`bizId`(必填)、`usage`(可选，默认 `attachment`)、`clientToken`(可选幂等键，≤64 字符) | `200 { code:0, data: AttachmentRef }`；同一 `(tenant_id, biz_type, biz_id, client_token)` 重复提交返回既有记录 |
| A2 | `GET /attachments` | 列表/元数据 | 宿主读动作 | query：`bizType`、`bizId`、`usage`、`page`、`pageSize` | `200 { data: { attachments: AttachmentRef[], total: number } }` |
| A3 | `GET /attachments/:id` | 单条元数据 | 宿主读动作 | path：`id` | `200 { data: AttachmentRef }` |
| A4 | `GET /attachments/:id/content` | 下载/预览 | 宿主读动作 | query：`disposition=inline\|attachment`（默认 `attachment`）；支持 `Range` | `200` 文件流；`Content-Disposition` / `Content-Type` / `Accept-Ranges` 完整 |
| A5 | `DELETE /attachments/:id` | 软删（不提供"仅解绑"降级） | 宿主删除动作 | path：`id` | `200 { code:0, data: { id, status: 'deleted' } }`；被 `usage=inline_image`（宿主正文）或 `usage=comment_attachment`（评论 `attachments` 数组）引用时返回 409 `6105`，且不改变任何状态（语义与状态机见 §5.3） |
| A6 | `POST /attachments/batch-query` | 批量元数据回填 | 宿主读动作 | `{ ids: number[] }`（≤200） | `200 { code:0, data: AttachmentRef[] }`；鉴权按 `(biz_type, biz_id)` **去重后批量校验一次**，禁止逐条查宿主（防 N+1） |
| A7 | ~~`POST /attachments/presign`~~ | **已取消（v1.0）** | — | — | 单文件上限维持 10MB，限额内 multipart 直传足够；未来若放开限额再评估 |

**域内别名（兼容 + 静态权限声明）**

| 别名 | 目标 | 说明 |
|:---|:---|:---|
| `GET /tickets/:id/attachments` | A2 | 保持现有响应字段完全不变 |
| `POST /tickets/:id/attachments` | A1 | 权限维持 `ticket:create` |
| `GET /tickets/:id/attachments/:ref`、`/download` | A4 | `ref` 兼容数字 ID 与历史存储文件名 |
| `GET /tickets/:id/attachments/:ref/preview` | A4（`disposition=inline`） | **历史富文本 URL，永久保留** |
| `DELETE /tickets/:id/attachments/:ref` | A5 | 权限维持 `ticket:delete` |
| 新增：`/knowledge/articles/:id/attachments*` | A1/A2/A4/A5 | 权限用 `knowledge:read/write/delete` |
| 新增：`/service-requests/:id/attachments*` | A1/A2/A4/A5 | 权限用 `service_request:read/write/delete` |

> 评论附件（v1.0 已纳入）：采用**先上传、后绑定**两阶段（与创建页图片暂存同构）——附件先以 `biz_type='ticket'`、`usage='comment_attachment'` 经 A1 上传（宿主映射到 `ticket:create`），评论创建/更新时携带 `attachments:[id...]`（`ticket-comment-api.ts:15,33` 既有契约）。服务端校验：同租户、`biz_type='ticket'` 且 `biz_id=ticketId`、`usage='comment_attachment'`、`status='active'`（在 `service/ticket_comment_service.go:286-291` 现有校验上收紧用途与状态）；更新路径补齐附件增删（现状 `:236` 传 nil）。评论删除或更新移除引用后，附件成为无主 `comment_attachment`，由 BE-8 清理任务在保留期后回收。`collaboration-api.ts:354-382` 死代码与重复 `CommentAttachment` 类型随 FE-9 删除。响应侧（BE-11）：评论响应在 `attachments` 之外另下发 `attachmentRefs` 展示元数据（同口径过滤 + 域内下载 / 预览地址），解决普通用户不持有兜底码 `attachment:read`、无法经通用 A3/A6 反查元数据的问题。

### 3.3 富文本内嵌图片时序

```text
用户粘贴图片
  → RichTextEditor 校验（maxImageSizeMB / maxImageCount / 类型）
  → uploadImage(file)（Provider 注入：POST /attachments 或域内别名）
      → 后端校验：大小、MIME 白名单（magic bytes 嗅探，不信任 Content-Type）、宿主存在性与状态、宿主写权限
      → 存储写入（本地/S3 抽象）→ 病毒扫描接入点（AttachmentVirusScanner，默认 noop，失败即删文件）→ 写后大小二次复核
      → attachments 落库（usage=inline_image, biz_type, biz_id）
      → 返回 AttachmentRef{ id, fileUrl, previewUrl }
  → 编辑器插入 <img src="{previewUrl}" data-attachment-id="{id}">
  → 提交时后端按 biz_type/biz_id 校验 data-attachment-id 归属（防跨域引用；工单/知识库更新路径均需接入，见 §7 BE-7）
  → 知识库正文净化需先对齐属性白名单（否则 data-attachment-id 被剥离，见 §3.5-3）
```

### 3.4 响应落地约定与错误码

**成功响应**：一律走 `common.Success` → HTTP `200` + `{"code":0,...}`；**不使用** `201`/`204`（`common/response.go:47-53` 固定 200，`httpClient` 按 `code` 解包）。

**兼容性决策（v1.0，已定，按最佳实践）**：新附件端点**不引入 `201`/`204` 兼容分支**，成功与失败统一 `HTTP 200` + 包络内 `code`（与 `common.Success`、`httpClient` 解包约定一致，避免同一语义出现两套状态码）；错误码按本节 61xx 登记并扩展 `Fail`/`FailWithData` 双 switch，**禁止未登记码静默返回 200**。旧工单端点逐字段兼容（BE-6）。

**错误码落地要求**（`common/response.go`）：

1. `Fail` 与 `FailWithData` 各有一个 HTTP 映射 switch（`:56-78`、`:88-107`），**未登记的码会静默返回 HTTP 200**（现网 `router.go:271` 的裸 `429` 即属此类）。新增附件错误码必须同时扩展这两个 switch。
2. 新增码占用 `61xx` 段（已核对现网已用 0/1001/1002/2001-2005/4000/4004/4090/4220/5001/5003，无冲突）：

| 场景 | HTTP | code | 常量名（新增至 `common/response.go`） | 前端提示 |
|:---|:---:|:---:|:---|:---|
| 参数缺失/类型错误 | 400 | 1001/1002 | 现有 `ParamErrorCode`/`ValidationError` | 定位到具体字段 |
| 未登录/租户缺失 | 401 | 2001/2002 | 现有 `AuthFailedCode`/`UnauthorizedCode` | `httpClient` 统一刷新/登出 |
| 无宿主持有权限 | 403 | 2003 | 现有 `ForbiddenCode` | i18n `attachment.error.forbidden` |
| 宿主不存在/跨租户 | 404 | 6101 | `AttachmentHostNotFoundCode` | 不泄漏宿主存在性；`attachment.error.hostNotFound` |
| 文件名非法（路径穿越/超长） | 400 | 6102 | `AttachmentInvalidFilenameCode` | `attachment.error.invalidFilename` |
| 文件超限 | 413 | 6103 | `AttachmentTooLargeCode` | 展示服务端限额；`attachment.error.tooLarge` |
| MIME/扩展名不允许 | 415 | 6104 | `AttachmentTypeNotAllowedCode` | 展示允许清单；`attachment.error.typeNotAllowed` |
| 附件被富文本引用仍要删 | 409 | 6105 | `AttachmentInUseCode` | `attachment.error.inUse`（引用位置提示第二阶段） |
| 配额超限 | 422 | 6106 | `AttachmentQuotaExceededCode` | `attachment.error.quotaExceeded` |
| 频率超限 | 429 | 6107 | `AttachmentRateLimitedCode` | 退避重试；`attachment.error.rateLimited` |

3. 前端：`http-client` 保持通用解包；附件错误文案集中映射（`lib/upload/error-messages.ts`），中英文案进 `lib/i18n/translations.ts`（见 §7 FE-7）。

**边界值**（默认值；可由 `system_config_service` 的 `upload` 分组覆盖，但前后端必须**同源下发**）：单文件 **10MB**（含图片；v1.0 决策维持，不放开到 50MB）、单宿主 100 个附件（评论附件单评论 ≤10 个）、`inline_image` 单文档 50 张、文件名 ≤255 字符、`batch-query` ≤200 条。

> **限额决策（v1.0，已定）**：单文件上限**维持 10MB**（现网工单服务硬编码值即为 10MB，`ticket_attachment_service.go:51`；不放开到 50MB）。前端 `AttachmentField` 现默认 50MB（`AttachmentField.tsx:101,218`）必须对齐为 10MB，并与 `system_config_service` 的 `upload` 分组（`maxFileSize`）同源下发；`RichTextEditor.maxImageSizeMB` 已为 10MB，保持一致。禁止"前端允许、后端 413"。

### 3.5 安全要求

1. **鉴权**：所有附件端点必须置于 `tenant` 分组内，禁止任何"仅登录即可下载"的豁免路径；下载前校验宿主读权限与宿主归属（`biz_type + biz_id + tenant_id` 三元一致）。
2. **CSRF**：保留 `httpClient` 现有 CSRF 机制；代理层已放行 `csrf-token` 引导端点。
3. **XSS 与内嵌图完整性**：编辑器输出统一走前端 `sanitizeRichTextHtml`；后端落库前二次白名单净化（`script`/`on*`/`javascript:`/外链 `img` 一律剥离）。注意现网存在**两套后端策略**：工单走 `internal/sanitize.SanitizeRichTextHTML`（显式放行 `data-attachment-id`/`data-align`/`width`/`height`，`richtext.go:66-70`），知识库走 `common.SanitizeHTML`（bluemonday `UGCPolicy`，未放行这些属性，`sanitizer.go:19-43`）。知识库接入富文本附件前必须二选一对齐（默认：扩展 `UGCPolicy()` 放行 `img` 的 `width/height/data-attachment-id/data-align`，改动面最小、不改变既有标签集；备选：切到 `SanitizeRichTextHTML`），否则 `data-attachment-id` 会被静默剥离，R8 的归属校验与引用保护全部失效。由 BE-7 落地并补单测。
4. **路径穿越**：`file_name` 仅作展示；存储 key 由服务端生成，新规则 `{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}`（相对 `StorageProvider` 根目录；现状根目录为 `uploads/tickets`，`ticket_attachment_service.go:42`）。历史值形如 `uploads/tickets/{ticketID}_{nano}_{安全文件名}`，必须**按原样读取**（双规则寻址）；下载按 `attachment.id` 反查 `file_path`，不接受客户端传入路径。
5. **隔离**：`ticket_id` 式外键由 `biz_type + biz_id` 取代后，查询必须带 `tenant_id`；租户上下文缺失统一 401（沿用 ROADMAP 既有防线）。
6. **审计**：**复用**租户分组已挂载的 `middleware.AuditMiddleware`（`router/router.go:456`，自动记录租户/操作者/方法/路径/状态码，落 `audit_logs`）；附件特有维度（附件 ID、宿主、usage、耗时）由服务层记结构化日志，必要时补 `audit_logs` 关联记录，**不新建审计表**；上传/下载/删除与越权尝试必须可追溯（查询入口 `service/auditlog_service.go`）。
7. **文件名**：`Content-Disposition` 使用 `filename*=UTF-8''<percent-encoded>`，避免头部注入。
8. **恶意文件与加固继承**：通用服务必须平移现网工单附件的加固能力——magic bytes MIME 嗅探（不信任 `Content-Type`，`ticket_attachment_service.go:111-135`）、`AttachmentVirusScanner` 扫描接入点（保存后扫描、失败即删文件，`:149-152`）、写后大小二次复核、文件名清洗与扩展名白名单；BE-3 验收包含上述单测。
9. **限流**：复用现有全局限流（内存/Redis 双实现，`router/router.go:256-289`）；如需附件专属阈值，在租户分组内追加 `middleware.RateLimitMiddleware`（`middleware/security.go:133-134`），默认不新增，仅登记 6107 错误码。

---

## 4. 权限映射表

### 4.1 权限模型现状与约束

- 权限码唯一权威源：`itsm-backend/internal/authz/catalog.go` 的 `Definitions()`；格式 `{resource}:{action}`。
- 校验入口：`middleware/rbac.go#RequirePermission(resource, action)`；路由声明经 `cmd/authz-gen` codegen 为预检映射。
- 新增权限码三条件：**独立资源面 + 至少一个路由引用 + 至少一个角色绑定**；守卫测试为 `pkg/seeder/role_permission_guard_test.go` 与 `router/permission_code_catalog_guard_test.go`。
- 结论：**默认不为每个业务域新增附件权限码**，而是把附件动作映射到宿主已有权限码（工单现网行为零变化）；**v1.0 决策：启用兜底码登记**——按三条件登记 `attachment:read/write/delete`（§4.3），宿主映射仍为主路径，兜底码仅用于无宿主域/系统级附件。

### 4.2 biz_type → 宿主资源/动作 映射（权威表）

| biz_type | 宿主资源 | 列表/元数据 | 下载/预览 | 上传/绑定 | 删除/解绑 | 与现网差异 |
|:---|:---|:---|:---|:---|:---|:---|
| `ticket` | `ticket` | `ticket:read` | `ticket:read` | `ticket:create` | `ticket:delete` | **完全一致**（`router/ticket_routes.go:170-180`） |
| `knowledge_article` | `knowledge` | `knowledge:read` | `knowledge:read` | `knowledge:write` | `knowledge:delete` | 新增路由引用，权限码已存在且已绑定角色（`roles.go:362,419`）；删除动作与 ticket/service_request 行保持一致 |
| `service_request` | `service_request` | `service_request:read` | `service_request:read` | `service_request:write` | `service_request:delete` | 新增路由引用，权限码已存在 |
| `incident` | `incident` | `incident:read` | `incident:read` | `incident:write` | `incident:delete` | 已接线（第二波，`1a85e8d8`） |
| `problem` | `problem` | `problem:read` | `problem:read` | `problem:write` | `problem:delete` | 已接线（第二波，`1a85e8d8`） |
| `known_error` | `problem` | `problem:read` | `problem:read` | `problem:write` | `problem:delete` | 新增行（第三波）：KEDB 属问题管理域，沿用 `handlers/known_error/handler.go` 既有约定复用 `problem:*` 词表，不引入新权限码 |
| `change` | `change` | `change:read` | `change:read` | `change:write` | `change:delete` | 已接线（第二波，`1a85e8d8`） |
| `release` | `release` | `release:read` | `release:read` | `release:write` | `release:delete` | 已接线（第三波，`0b453306`；别名路由 6 条在 `router/release_routes.go`） |
| `cmdb_ci` | `cmdb` | `cmdb:read` | `cmdb:read` | `cmdb:write` | `cmdb:delete` | 已接线（第三波，`0b453306`；别名路由挂 `/cmdb` 组而非 `cis` 子组，规避读门禁叠加，见 `router/cmdb_routes.go`） |
| `(无宿主/系统级)` | `attachment` | `attachment:read` | `attachment:read` | `attachment:write` | `attachment:delete` | **v1.0 启用登记**（随 §4.3 执行，P0-3） |

> 说明：`ticket` 行的"上传"沿用 `ticket:create` 是**刻意保持现网行为**，避免一次改造同时改变鉴权语义；后续如需收紧为 `ticket:write`，单独走权限变更评审。

> 评论附件（v1.0）不新增 `biz_type`：附件以 `biz_type='ticket'` + `usage='comment_attachment'` 上传，评论创建/更新时按 ID 绑定（见 §3.2 注），权限随 `ticket:*`。

### 4.3 兜底权限码登记清单（v1.0 已启用，P0-3 执行）

1. 在 `internal/authz/catalog.go#Definitions()` 增加 `attachment:read/write/delete`（独立资源面成立）。
2. 确保至少一个路由声明引用（通用 `/attachments` 路由组由 codegen 生成预检映射）。
3. 在 `internal/authz/roles.go#BuiltinRolePermissionCodes()`（权威源；该文件**没有** `builtinRoleBindings` 标识符）中为系统管理员/租户管理员绑定，确保三条件齐备。
4. 执行仓库既有 codegen：`go generate ./middleware/...`（指令在 `middleware/rbac.go:746`，生成 `ResourceActionMap` 预检映射）与 `go generate ./ent/...`（指令在 `ent/generate.go`）；守卫测试：`go test ./tests/parity/ ./internal/authz/ ./pkg/seeder/... ./router/... -run 'Guard|Catalog|Parity'`（`roles.go:12-20` 明确要求 parity 与 authz 两套必须跑）。
5. 同步 ACL 清单：`docs/acl-manifest.yaml` 是**生成物**（含 `generated_at`/`permission_coverage`），按仓库脚本**重新生成**并同批提交，禁止手工增删条目。
6. **落地记录（2026-09-22，P0-3 完成）**：
   - 已执行：三码登记（`catalog.go`）+ admin/sysadmin 绑定（`roles.go`，`allExcept` 角色随之继承）+ `go generate ./middleware/...`（`rbac_precheck_gen.go` 同批刷新，含 ticket 附件 download/preview 预检）+ `go generate ./ent/...`（零 diff）+ `node scripts/generate-acl-manifest.js`（595 路由 / 99.83% 覆盖）。
   - 定向守卫（全绿）：`go test ./tests/parity/ ./internal/authz/ ./pkg/seeder/ ./router/ -run 'TestBuiltinRolePermissionCodes_Guard|TestAuthzSeederCodeSetParity|TestRoleBindingsSubsetOfCodes|TestDefinitionsParityWithSeeder|TestRoutePermissionCodesAreDefined|TestWriteRoutesRequirePermission' -count=1` 与 `go test ./middleware/ -run 'TestPrecheckMapIsFresh|TestRoutePrecheckAlignment' -count=1`。
   - **验收口径修订**：上文第 5 条「`attachment:*` 在 ACL 清单可见」在 P0-3 阶段不可达——`docs/acl-manifest.yaml` 是**路由派生**生成物（脚本仅解析 `*_routes.go` 的 `RequirePermission` 声明），通用路由属 P1/BE-4。故 P0-3 完成定义改为「三码登记 + 角色绑定 + 预检/种子链路一致（守卫测试实证）」，清单可见性与三条件中的「路由引用」在 BE-4 复核（已列为 BE-4 验收追加项）。
   - 既有问题（与本批无因果，本次未修）：① `node scripts/generate-acl-manifest.js --check` 因 `GET /api/v1/auth/password-policy`（`router.go:314`）未声明权限而退出 1；② `pkg/seeder` 用例 `TestProductionInitializersRepairMissingServiceCatalogWithoutOverwritingTenantCustomization` 因 workflow-core 流程定义 `incident_emergency_flow` 缺失而失败（用例名含 `Catalog`，会被 `-run 'Guard|Catalog|Parity'` 误命中）。

7. **清单生成器修复（2026-09-22，BE-4 复核时发现并修复）**：`scripts/generate-acl-manifest.js` 两处漏采/错采导致 ACL 清单长期少报路由——
   ① `extractRouteCall` 路径正则用 `+`（至少 1 字符），使 `group.GET("", h)` 形式的**分组根路径**（列表/创建等最核心端点）被静默丢弃；
   ② 子路由文件以 `*gin.RouterGroup` 入参接收挂载点，脚本无法自证前缀：新 `attachment_routes.go` 的 `POST/GET ""` 两条直接被丢、其余 4 条丢 `/api/v1` 前缀（父分组前缀硬编码 `parent === "r" → ""`）。
   修复：路径正则 `+`→`*`；新增调用点实参→形参**按位置**对齐的挂载前缀解析（`collectSetupMounts`/`splitTopLevel`/`matchingParen`）；父分组前缀统一走 `resolveVar`。重新生成：**599 → 696 路由**（恢复 97 条此前不可见的根路径端点），覆盖 99.86%；`--check` 未保护清单收敛为 1 条既有项（`GET /api/v1/auth/password-policy`，与本批无因果）。

### 4.4 前端可见性映射

| UI 元素 | 判定依据 | 数据来源 |
|:---|:---|:---|
| 「附件」Tab / 区块 | 宿主读权限 | 现有权限 hook（沿用各域既有判定，不新增机制） |
| 上传/删除按钮 | 宿主上传/删除动作 | 同上 |
| 图片粘贴/拖拽入口 | 编辑器已注入 `uploadImage` 且宿主可写 | `AttachmentProvider` + 宿主上下文 |

---

## 5. 数据模型与存储设计

### 5.1 新表 `attachments`（DDL 草案）

```sql
CREATE TABLE IF NOT EXISTS attachments (
  id          BIGSERIAL PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  biz_type    VARCHAR(64)  NOT NULL,
  biz_id      BIGINT       NOT NULL,
  usage       VARCHAR(32)  NOT NULL DEFAULT 'attachment',  -- attachment | inline_image | comment_attachment
  file_name   VARCHAR(255) NOT NULL,
  file_path   VARCHAR(512) NOT NULL,                        -- 存储 key（服务端生成）
  file_url    VARCHAR(1024),
  file_size   BIGINT       NOT NULL,
  file_type   VARCHAR(128) NOT NULL,                        -- 历史语义：与 mime_type 同值（现网 service 同写 MIME，ticket_attachment_service.go:160-161）；分类请用 usage/biz_type
  mime_type   VARCHAR(128),
  sha256      CHAR(64),
  client_token VARCHAR(64),                                 -- 幂等键（可选，A1）
  uploaded_by BIGINT       NOT NULL,
  status      VARCHAR(16)  NOT NULL DEFAULT 'active',       -- active | deleted
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
  deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_attachments_biz
  ON attachments (tenant_id, biz_type, biz_id, usage)
  WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS uq_attachments_path
  ON attachments (file_path);

-- 幂等去重（client_token 为空的历史/普通上传不受约束）
CREATE UNIQUE INDEX IF NOT EXISTS uq_attachments_client_token
  ON attachments (tenant_id, biz_type, biz_id, client_token)
  WHERE client_token IS NOT NULL;
```

与 `ticket_attachments` 的字段对应：`ticket_id → biz_id`（`biz_type='ticket'`）、其余列同名同义（含 `file_type` 的"MIME 同值"历史语义），保证回填可直接 `SELECT`；`client_token` 为新增列，历史数据回填为 `NULL`。

**落地说明（2026-09-22，BE-1 完成）**：`migrations/20260922_create_attachments_expand.sql` 按上表落地，并补三处仓库一致性细化——① `BEGIN/COMMIT` 事务包裹；② 增加 RLS 策略 `tenant_isolation_attachments`（`ENABLE ROW LEVEL SECURITY` + `app.current_tenant` 变量，与 `20260828_create_alerts.sql`/`20260831_ai_analysis_result_expand.sql` 同模式），保证 `RLS_MODE=enforce` 下该表可查；③ 配套 `migrations/20260922_create_attachments_down.sql`（drop 表与策略）。

**列类型口径（2026-09-22 取证，BE-2 对齐）**：以 ent v0.14.6 方言源码为准——`field.Int` 映射 **BIGINT**（`dialect/sql/schema/postgres.go:122`），自增列由方言追加 `Identity` 属性（`postgres.go:180-196`），故 `id` 用 `BIGINT GENERATED BY DEFAULT AS IDENTITY`（与 `internal/bootstrap/workflow_templates_identity_migration.go` 记载的「ent diff 期望 IDENTITY」一致，规避 SERIAL/IDENTITY 属性差异）；`tenant_id/biz_id/file_size/uploaded_by` 用 `field.Int`，字符串长度用 `MaxLen` 与 DDL 同源（`sha256` 顺带由 `CHAR(64)` 改 `VARCHAR(64)` 以对齐 ent）；`file_size` 用 `NonNegative()`（错误码表未登记"空文件"，不用 `Positive()` 额外拦截 0 字节）。另记两处仓库既有文档陈旧点（**不影响本次交付**）：`20260831_ai_analysis_result_expand.sql` 注释所称"field.Int → INTEGER"与 ent 源码不符；`cmd/migration-lint` 的 NOT NULL 门禁只覆盖 `ALTER TABLE ... ADD COLUMN`（`migration/pii/validator.go:42`），`CREATE TABLE` 不在其检查面，故本次输出的 `OK` 不代表建表列有 DEFAULT 兜底。

**门禁**：`go run ./cmd/migration-lint -schema-dir ./ent/schema -migration-sql ./migrations/20260922_create_attachments_expand.sql` 通过；`go test ./migration/...`（目录自发现与配对）通过。脱敏副本演练待环境（缺本地 Postgres/Docker）。

### 5.2 回填 SQL（expand 阶段，幂等）

```sql
INSERT INTO attachments
  (id, tenant_id, biz_type, biz_id, usage, file_name, file_path, file_url,
   file_size, file_type, mime_type, uploaded_by, status, created_at)
SELECT id, tenant_id, 'ticket', ticket_id, 'attachment', file_name, file_path, file_url,
       file_size, file_type, mime_type, uploaded_by, 'active', created_at
FROM ticket_attachments
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('attachments', 'id'),
              GREATEST((SELECT COALESCE(MAX(id), 1) FROM attachments), 1));
```

**保 ID 平移**是硬要求：历史富文本 HTML 中的 `data-attachment-id` 与 `/tickets/:id/attachments/:ref` URL 都引用旧 ID，改号即裂图。

### 5.3 存储与生命周期

| 主题 | 设计 |
|:---|:---|
| 存储 key | 新规则 `{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}`（相对 `StorageProvider` 根目录，根目录沿用现网 `uploads/tickets`，配置化后迁移）；`file_path` 即 key，`file_url` 仅作展示缓存；**双规则寻址**：历史值（`uploads/tickets/{ticketID}_{nano}_{安全文件名}`）按原样读取，不做批量改名 |
| 存储抽象 | 现状**无**抽象（本地磁盘直写 `os.Create` / `os.Open`，`ticket_attachment_service.go:142,374`）；本次新增 `StorageProvider` 接口（`Save/Open/Delete/Stat`）+ 本地实现，对象存储适配留待后续（非目标），接口用于隔离路径生成与 IO |
| 删除语义 | 仅软删（`status=deleted`）；物理文件在无引用且超过保留期后由清理任务回收（保留期与任务见 §7 BE-8）；**不提供"仅解绑"降级**——被 `inline_image`/`comment_attachment` 引用时返回 409 `6105` 且不改变任何状态；确需解绑须先移除正文/评论引用 |
| 富文本引用保护 | 阶段二：引用判定 = 宿主正文 HTML 中是否存在 `data-attachment-id=<id>`（`usage=inline_image`）；删除前校验，命中返回 409；`data-attachment-id` 的正确落库依赖 §3.5-3 的净化对齐与 BE-7 的归属校验 |
| 评论引用保护 | 引用判定 = `ticket_comments.attachments` 数组是否含该 ID（`usage=comment_attachment`）；评论删除/更新移除引用后附件转为无主，由 BE-8 保留期后回收；A5 删除被引用附件返回 409（§3.2 注、BE-9） |
| 幂等 | `client_token` 去重，唯一索引 `uq_attachments_client_token`；重试命中唯一冲突时返回既有记录（不报错、不重复落库） |
| 大小/类型策略 | 服务端为准；复用 `system_config_service` 的 `upload` 分组（`maxFileSize`、`allowedFileTypes`）并补 `imageMaxFileSize`/`maxAttachmentCount`；单文件上限**统一为 10MB**（v1.0 决策）：前端默认值对齐（`AttachmentField.maxSizeMB` 50→10）并与该分组同源下发（§3.4） |

---

## 6. 迁移步骤（expand → migrate → contract）

### 阶段总览

| 阶段 | 目标 | 工期 | 出口门禁 | 回滚点 |
|:---|:---|:---:|:---|:---|
| P0 契约冻结与权限登记 | 契约、错误码、开关、权限映射定稿（含兜底码登记） | 3.5 人日 | 本文档 v1.0 冻结（2026-09-22 已完成）；四项决策落定；`catalog.go` 变更通过守卫测试 | 无数据变更，直接回退代码 |
| P1 后端通用能力（expand） | 新表 + 通用服务/路由 + 旧端点薄适配 + 引用完整性/生命周期 + 评论附件后端 | 12.5 人日 | 新旧端点双跑对照；`migration-lint` 通过；权限矩阵用例全绿 | 关开关，旧链路不受影响（新表可保留） |
| P2 数据回填与双写 | 历史数据平移 + 写路径双写 | 3 人日 | 行数/ID/字节数对账一致；抽样 200 条下载校验 | 停双写；旧表仍是唯一事实源 |
| P3 前端控件下沉 | `common/` 目录 + re-export + 全量调用点替换 + 评论附件接入 + i18n | 8 人日 | 组件测试全绿；工单创建/详情回归通过 | 恢复旧 import 路径（re-export 保留） |
| P4 读路径切换 + 灰度 | 通用读上线，按租户灰度 | 3 人日 | 灰度租户 0 错误；404/403 监控无新增 | 读开关回退到旧表 |
| P5 停写旧表 + 清理（contract） | 单写新表，旧表归档 | 3 人日 | 观察期 ≥2 周无回滚；旧表只读校验通过 | 恢复双写（保留 1 个发布周期窗口） |

### P0 契约冻结与权限登记（3.5 人日；2026-09-22 已完成冻结）

1. 冻结 §3（接口契约）、§4（权限映射）、§5.1（DDL）。**四项决策已定（v1.0）**：
   - 单文件限额**维持 10MB**（不放开到 50MB；前端默认值对齐，§3.4）；
   - 评论附件**纳入本次范围**（先上传后绑定；§3.2 注、BE-9/FE-9）；
   - 错误码/响应按**最佳实践**：新端点一律 `200 + code`，不引入 `201`/`204` 兼容分支，61xx 双 switch 登记（§3.4）；
   - **启用** D3 兜底权限码 `attachment:read/write/delete`（§4.3、P0-3）。
2. 缺陷修复先行项：确认 `/api/v1/attachments/upload` 与 `/knowledge/articles/upload/image` 的现状处置——**先让 `defaultUploadImage` fail-fast**（D6），消除静默 404；知识库上传随 P1 一并落地。
3. 按 §4.3 完成兜底码登记、角色绑定与 codegen，守卫测试全绿。
4. 输出：契约冻结记录（本文档 v1.0）、开关清单（见 §6.1）。

### P1 后端通用能力（12.5 人日）

1. 迁移文件（expand，沿用仓库 expand/contract 先例）：
   - `itsm-backend/migrations/20260922_create_attachments_expand.sql`（建表 + 索引，含 `client_token` 与 `uq_attachments_client_token`）
   - `itsm-backend/migrations/20260922_create_attachments_contract.sql`（P5 使用：旧表归档/改名）
   - 命名遵循 `YYYYMMDD_<snake_case>.sql`；`_down.sql` 是否强制以 `go run ./cmd/migration-lint` 实际规则为准（仓库 Makefile **没有** migration-lint 目标，直接执行该命令）。
2. 落地 `ent/schema/attachment.go` + `go generate ./ent/...`；服务层 `service/attachment_service.go`（`StorageProvider` 抽象、租户校验、宿主存在性校验、幂等、审计，并**继承既有加固**：magic bytes 嗅探、`AttachmentVirusScanner`、写后大小复核、文件名清洗）；评论附件绑定校验按 §3.2 注收紧（BE-9）。
3. 处理器 `handlers/attachment/handler.go`（复用 `ticket_attachment` 迁移经验：只做参数解析与响应封装）；路由 `router/attachment_routes.go` + 各域别名路由。
4. 旧端点改薄适配：`handlers/ticket_attachment/*` 内部转发通用服务，**响应字段与状态码保持完全不变**（字段级对照测试固化）。
5. 门禁：`go test ./handlers/... ./service/... ./router/...`；`go run ./cmd/migration-lint`（已核对 Makefile 无对应目标）；迁移在脱敏副本演练（对齐 ROADMAP「生产数据升级门禁」）。

### P2 数据回填与双写（3 人日）

1. 执行 §5.2 回填 SQL（幂等，可重跑）。
2. 对账脚本：数量、`MAX(id)`、字节数合计、抽样 200 条 `file_path` 存在性。
3. 双写：新上传同时写 `attachments` 与 `ticket_attachments`（或反向），读取仍走旧表；差异对账任务按小时跑。
4. 回滚：关闭双写开关即可；已回填的新表数据不影响旧链路。

### P3 前端控件下沉（8 人日）

1. 目录迁移（`git mv`，保留 blame）：`business/RichText*` → `common/rich-text/`；`business/AttachmentField.tsx` → `common/attachment/`。
2. 旧路径保留 re-export（一个发布周期），并在文件头标注 `@deprecated`。
3. 新增 `lib/upload/attachment-uploader.ts`、`lib/api/attachment-api.ts`、`lib/upload/error-messages.ts`；替换**全部调用点**：`TicketDetail.tsx:521-536`、`create/page.tsx:241-259/:433-462`（创建页两阶段保留，上传改走通用客户端）、`TicketAttachmentSection.tsx`、`detail-tabs/adapters/ticket-attachment-adapter.ts`、`DynamicFieldRenderer.tsx`；`collaboration-api.ts:354-382` 死代码删除（FE-9）。
4. 合并 `ticket-api.ts#uploadTicketAttachment` 与 `ticket-attachment-api.ts` 的重复实现（保留一个薄封装指向 `attachment-api`）；删除 `collaboration-api.ts` 的两个死代码方法与重复 `CommentAttachment` 类型（FE-9）。
5. 知识库接入：文章编辑器注入 `AttachmentProvider`（`bizType='knowledge_article'`），并完成 §3.5-3 的净化对齐与 BE-7 的归属校验联调。
6. i18n：6101-6107 错误文案与新 UI 文案进 `lib/i18n/translations.ts`（zh-CN/en）。
7. 评论附件 UI（FE-9）：**实现在真实渲染路径 `detail-tabs/CommentPanel.tsx`**（原方案写 `TicketCommentSection`，落地时全仓检索该文件除自身外零引用 = 死代码，已随 FE-9 删除）——新增 `common/attachment/CommentAttachmentField.tsx` 附件入口（复用 `AttachmentField` + `attachment-api`），评论创建/编辑携带 `attachments`，展示消费 BE-11 `attachmentRefs` 并支持下载/预览。
8. 门禁：定向执行相关 jest 用例（`RichTextImageViewer`、`RichTextEditorResizableImage`、`staged-images` 等），并按需补充 `AttachmentField` 契约用例。

### P4 读路径切换 + 灰度（3 人日）

1. 灰度开关（P0-4 定稿为下划线扁平形式，见 §6.1 注）：
   - `attachment.generic_read_enabled`（按租户）
   - `attachment.generic_write_enabled`
   - `attachment.dual_write_enabled`
2. 先切"列表/下载"读路径（低风险），观察 1 周；再切富文本图片上传（含 preview 兼容重定向）。
3. 监控：附件 API 4xx/5xx、P95 耗时、404 新增（防历史 URL 失效）、403 新增（防权限回归）。

### P5 停写旧表 + 清理（3 人日）

1. 写路径单写 `attachments`；旧表置只读（`REVOKE INSERT/UPDATE` 或应用层开关）。
2. 观察期 ≥2 周后执行 contract：旧表改名归档（保留只读副本至下一个 LTS 版本）；删除 re-export 与重复 API 客户端。
3. 更新文档：`docs/api-reference.md`、`docs/acl-manifest.yaml`（**重新生成**）、`docs/documentation-style-guide.md`（把 `docs/plan/` 登记进目录分层表；`mkdocs.yml` 已 exclude `plan/`）、本方案的"已落地"状态。
4. 遗留：`/tickets/:id/attachments/:ref/preview` 等兼容端点保留（D5），旧表归档不影响 URL 可用性。

### 6.1 开关与回滚矩阵

| 开关 | 默认 | 作用 | 关闭后行为 |
|:---|:---|:---|:---|
| `attachment.generic_read_enabled` | false → 灰度 true | 通用读路径（A2/A3/A4/A6） | 回退旧表读取，URL 不变 |
| `attachment.generic_write_enabled` | false | 通用写入（A1/A5） | 回退旧工单写入路径 |
| `attachment.dual_write_enabled` | false → P2 true → P5 false | 双写对账 | 停止双写，旧表为事实源 |
| `attachment.inline_image_enabled` | false | 富文本内嵌图片走通用链路 | 编辑器图片入口禁用（fail-fast） |

> **P0-4 配置键定稿（2026-09-22，已落地）**：为对齐 `mapstructure` / `system_config` 既有命名约定，上表键名由「建议命名」的点分形式扁平化为下划线形式（`attachment.generic.read.enabled` → `attachment.generic_read_enabled`，其余同理），部署级与租户级同名。落地位置：部署级 `itsm-backend/config/config.go#AttachmentConfig`（样例 `config.yaml.example`、`deploy/config.yaml`，另支持 `ATTACHMENT_*` 环境变量覆盖）；租户级 `service/system_config_service.go#InitDefaultConfigs`（`category=attachment`，随租户初始化写入）；部署说明 `docs/install.md` §3.1。

---

### 6.2 开发环境收口记录（2026-09-24）

> 决策来源：用户明确「当前只有开发环境，不考虑切换 / 灰度分流；数据库如需调整，可登录开发服务器
> （`ssh n9e@172.18.3.238`）直接处理」。因此 **P4（按租户灰度切流）与 P5（停写旧表 + contract）本期不执行**，
> 其余任务按开发环境一次性收口；旧表 `ticket_attachments` 继续作为兼容事实源保留。

| 项目 | 状态 | 证据 |
|:---|:---|:---|
| 数据迁移（expand + backfill） | ✅ 已应用 | 开发库 `schema_migrations` 新增 `20260922_create_attachments_expand`（checksum `a017c304…42d32`）与 `20260923_attachments_backfill`（checksum `54f08d3a…cbf0a`），库内可重跑幂等 |
| 回填对账 | ✅ 全绿 | `attachments` 15 行 = `ticket_attachments` 15 行；`missing_in_attachments=0`、`field_mismatch=0`、`id_collision_risk=OK`、`attachments_id_seq.last_value=21`（与旧表 `MAX(id)` 对齐，历史 `data-attachment-id` 与域内 URL 不裂图） |
| 开关（开发环境直开，不做灰度） | ✅ | `config.yaml#attachment`：`generic_read_enabled=true`、`generic_write_enabled=true`、`inline_image_enabled=true`、`dual_write_enabled=false`；`cleanup_enabled/purge=false`（`.env` 同名 `ATTACHMENT_*` 覆盖，`.env` 未入库） |
| 后端装配 | ✅ | `go build ./...` = 0；`main.exe` 重建重启后 `/api/v1/attachments` 返回 401（路由已挂载、需鉴权），不再是 404 |
| 后端定向测试 | ✅ | `go test ./handlers/ticket_attachment/...`、`go test -count=1 -run 'Attachment\|Comment' ./service/` 全绿 |
| 前端门禁 | ✅ | `npx tsc --noEmit` = 0；定向 jest 12 suites / 137 tests 全绿（附件域 `AttachmentField` / `CommentAttachmentField` / `TicketAttachmentSection` / detail-tabs 适配器 / `attachment-api` / `rich-text`） |
| P4 灰度 / P5 停写旧表 | ⏭️ 本期不执行 | 开发环境无灰度与停写需求；正式发布窗口再按 §6 阶段总览执行（contract 脚本 `20260922_create_attachments_contract.sql` 已就位） |

端到端冒烟（`http://127.0.0.1:8090`，2026-09-24；凭据用本地 `.env` 的 `JWT_SECRET` 现签 admin access token，**未改动任何账号密码**）：

| 场景 | 结果 |
|:---|:---|
| `GET /api/v1/tickets/4/attachments`（域内别名列表） | 200 / `code=0`，5 条（回填行 id 2-6，`fileUrl` 仍为原域内下载地址） |
| `GET /api/v1/attachments?bizType=ticket&bizId=4`（通用列表） | 200 / `code=0`，与域内列表同 5 条 |
| `POST /api/v1/attachments`（通用上传，ticket 4 / usage=attachment） | 200 / `code=0`，新增 id=22、24；落库 `file_path=1/ticket/4/<uuid>.txt`（新 key 规则），`sha256` 与本地一致 |
| `POST /api/v1/tickets/4/attachments`（域内别名上传，BE-6 旧端点） | 200 / `code=0`，新增 id=23 |
| `GET /api/v1/attachments/:id` + `/:id/content` | 200；下载字节 SHA256 与源文件逐字节一致 |
| `POST /api/v1/attachments/batch-query`（ids=[1,2,999999]） | 200 / `code=0`，命中项按 A6 返回、不存在项静默省略（`data` 为数组形态） |
| `DELETE /api/v1/attachments/:id` 与域内 `DELETE` | 200 / `code=0`；软删（`status=deleted` + `deleted_at`），列表回落到 5 条，`GET /:id` 转 404 `4004` |
| `POST /api/v1/attachments`（`bizId=999999`） | 404 `6101 宿主不存在或无权访问`（宿主存在性/归属校验生效） |
| 冒烟数据清理 | 本次 3 条上传全部软删，`attachments` 活跃行仍为 15，未污染业务数据 |

> 环境排障记录（复现要点）：SSH 隧道 `15433`（PostgreSQL）/ `16380`（Redis）中 Redis 段僵死时，
> `AuthMiddleware` 的吊销检查 fail-closed，所有受保护请求返回 401「token状态验证失败」（登录亦不可用）。
> 处置：重建隧道（`-L 15433:127.0.0.1:5433 -L 16380:127.0.0.1:6380`）并重启后端即可恢复；与附件功能本身无关。

---

## 7. 任务拆解（WBS）

> 估时单位：人日；「验收」为可验证的完成定义，不以"代码已写"代替。

### P0 契约与准备（3.5 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| P0-1 | 契约评审与冻结 | 本文档 v1.0（评审记录） | — | 1 | 前端/后端/测试三方签字；字段与错误码无歧义 |
| P0-2 | 现状断链处置：`defaultUploadImage` fail-fast | `RichTextEditor.tsx` 变更 + 用例 | P0-1 | 0.5 | 未注入上传实现时按钮禁用、无 404 请求（单测断言） |
| P0-3 | 权限映射与兜底码登记（v1.0 启用） | `internal/authz/catalog.go`+`roles.go` 变更 + codegen + 守卫测试通过 | P0-1 | 1.5 | ✅ 2026-09-22 落地：三码登记 + admin/sysadmin 绑定 + 双 codegen + 定向守卫全绿（证据与验收口径修订见 §4.3-6）；`attachment:*` 清单可见性随 BE-4 路由落地 |
| P0-4 | 开关与配置项登记 | 配置样例 + 部署说明 | P0-1 | 0.5 | ✅ 2026-09-22 落地：`config.go#AttachmentConfig` + `config.yaml.example`/`deploy/config.yaml` 样例 + `system_config_service` attachment 分组（租户级）+ `docs/install.md` §3.1；灰度开关可在目标环境按租户生效/回退 |

### P1 后端通用能力（12.5 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| BE-1 | expand 迁移：建 `attachments` 表与索引 | `migrations/20260922_create_attachments_expand.sql` | P0-1 | 1 | ✅ 2026-09-22 落地：expand + `_down.sql`（含 RLS 策略，见 §5.1 落地说明）；`migration-lint` 零违规、`go test ./migration/...` 自发现/配对通过；脱敏副本演练待环境 |
| BE-2 | ent schema 与生成物 | `ent/schema/attachment.go` + `go generate` | BE-1 | 1 | ✅ 2026-09-22 落地：`go generate ./ent` 完成（新增 `ent/attachment{,_create,_update,_delete,_query}.go` + `ent/attachment/`，`ent/{client,ent,hook,mutate,predicate,runtime,tx}.go`、`ent/migrate/schema.go` 同步）；`go build ./ent/...` 通过；`ent/migrate/schema.go:331-356` 的 `AttachmentsColumns` 与 DDL 逐列一致（17 列同序、`id` Increment→IDENTITY、无 ForeignKeys，切合"不建外键"设计）。全仓 `go build ./...` 见下方门禁记录 |
| BE-3 | 通用服务层（存储/租户/宿主校验/幂等/审计 + 加固继承） | `service/attachment_service.go` | BE-2 | 2.5 | ✅ 2026-09-22 落地：`service/attachment_service.go` + `attachment_service_test.go`；`StorageProvider` 接口 + `LocalStorageProvider`（双规则寻址：新 key `{tenant_id}/{biz_type}/{biz_id}/{uuid}.{ext}` 相对 `uploads/tickets`，历史 `uploads/tickets/...` 原样读取）；宿主注册表内置 ticket/ticket_comment/knowledge_article（`SetHost` 可扩展）；幂等 clientToken（并发唯一冲突回查赢家 + 清理多余文件）；审计直写 `ent.AuditLog`（不回滚业务）；A5 软删 + 引用保护（inline_image 查宿主正文 `data-attachment-id`、comment_attachment 查 `ticket_comments.attachments`，命中 `ErrAttachmentInUse`→409/6105，重复删除幂等）。加固四件套完整继承（magic bytes 嗅探 / 病毒扫描失败即删 / 写后大小复核 / 文件名清洗+扩展名白名单）。验收：`go test ./service/ -run "TestAttachment\|TestHTMLReferencesAttachment\|TestLocalStorageProviderKeyRules"` 全绿（14 项，覆盖跨租户 404、宿主不存在、幂等重试、超限/空文件、伪装内容、大小不符、扫描失败清理、存储抽象注入）；`uq_attachments_client_token` 的唯一冲突路径由 Postgres 集成测试覆盖（ent schema 不声明索引）。实现说明：`resolveMIMEType`/`isAllowedType` 在通用服务内以包级函数重写（表数据复用，BE-6 统一前保持双实现）；`attachment_service.go` 不暴露 `file_path`，下载统一走 A4 |
| BE-4 | 通用处理器与路由（A1-A6 + 错误码登记） | `handlers/attachment/handler.go`、`router/attachment_routes.go`、`common/response.go` 双 switch 扩展 | BE-3 | 2 | ✅ 2026-09-22 落地：A1-A6 六端点（`POST/GET /api/v1/attachments`、`POST /batch-query`、`GET /:id`、`GET /:id/content`（支持 Range）、`DELETE /:id`），静态码 `attachment:write/read/read/read/read/delete`，宿主维度按 §4.2 动态复核（未映射 biz_type 由兜底码承担）。错误码：6101-6107 在 `Fail`/`FailWithData` **双 switch** 全部登记（HTTP 200 + code=61xx 口径），另登记通用 `TooManyRequestsCode=429`，修复 `http.StatusTooManyRequests` 被当业务码导致的「未登记码静默 200」。验收：`go test ./handlers/attachment/ -count=1` 全绿（端点级覆盖 200/403/404/413/415/422/429/500 与 6101-6107 映射、多租户隔离、幂等重试、软删引用保护）；守卫 `go test ./router/ ./internal/authz/` 与 `go test ./middleware/`（预检新鲜度）全绿。**ACL 清单可见性闭环**：`node scripts/generate-acl-manifest.js` 重新生成后 6 条路由全部在册（`attachment.write/read/delete`），三条件「路由引用」成立——为此修复清单脚本两处漏采缺陷（见 §4.3-7） |
| BE-5 | 域内别名路由（knowledge / service_request） | 各域路由文件 + 权限声明 | BE-4 | 1 | ✅ 2026:09: 22 落地：`handlers/attachment/handler.go` 新增 4 个别名入口（`AliasUpload`/`AliasList`/`AliasDownload`/`AliasDelete`，宿主 = bizType 常量 + 路径 `:id`，附件引用读 `:ref`），复用 A1/A2/A4/A5 主体并新增**归属复核** `hostScope.matches`（附件必须属于路径宿主，跨宿主读取/删除一律 404 且先于文件流输出）；`service/attachment_service.go` 注册 `service_request` 宿主（`AttachmentBizTypeServiceRequest` + `ExistsFunc`/`ReferencesFunc`，引用保护读 `reason` 正文）。路由 12 条：`router/knowledge_routes.go` 6 条（`GET|POST /knowledge/articles/:id/attachments`、`GET /:ref`、`/:ref/download`、`/:ref/preview`、`DELETE /:ref`，静态码 `knowledge:read`/`write`/`delete`）与 `router/service_request_routes.go` 6 条（同形，静态码 `service_request:read`/`write`/`delete`）；`SetupKnowledgeRoutes`/`SetupServiceRequestRoutes` 新增 `attachmentHandler` 形参，`router/router.go` 传入 `config.AttachmentHandler`（nil 时别名路由整组不注册）。**零新增权限码**。同批：`go run ./cmd/authz-gen`（预检 +12 条，`:id` 归一为 `*`，守卫 `TestPrecheckMapIsFresh` 通过）、`node scripts/generate-acl-manifest.js`（696→708 路由、99.86% 覆盖；`--check` 未保护项仍仅既有 password-policy 一条）。验收：`go test ./handlers/attachment/ -count=1` 全绿（新增知识库别名闭环与服务请求跨宿主隔离 2 项）、`go test ./router/ ./internal/authz/ -count=1` 与 `go test ./middleware/ -run 'TestPrecheckMapIsFresh\|TestRoutePrecheckAlignment' -count=1` 全绿 |
| BE-6 | 旧工单端点薄适配 + 字段级兼容测试 | `handlers/ticket_attachment/*` 改造 + 对照测试 | BE-4 | 1.5 | ✅ 2026-09-22 落地：`service/ticket_attachment_service.go` 增通用后端 seam（`SetGenericBackend` + `GenericBackendFlags`，租户级读/写开关；读：`GetAttachmentFile` 命中通用表即返回，仅 `ErrAttachmentNotFound` 回退旧表；写：`DeleteAttachment` 仅「存活且宿主归属一致」的通用记录走通用删除，已软删/跨宿主一律回退旧表）；通用 `AttachmentView` 补 `StorageKey` 承接旧 `file_path` 语义。契约测试 `handlers/ticket_attachment/handler_contract_test.go` 四组：①**开关全关冻结**——上传/列表/下载/预览/历史纯文件名引用/删除的 JSON 键集合按字典序快照，`fileUrl` 形态正则 `^/api/v1/tickets/(\d+)/attachments/(\d+)/preview$`，取值白名单仅 `id`/`filePath`/`fileUrl`/`createdAt`；②**同库同用户逐字段对照**（开关开 vs 关）——含 `uploader` 嵌套 6 字段、`attachments` 表 1 行、`ticket_attachments` 表 1 行、通用记录软删后回退删旧表且列表 `total=0`；③**错误码对照 6 场景**——宿主不存在 / 附件不存在（下载·预览·删除）/ 类型拒绝 / 认证缺失，两模式 `HTTP + code + message` 三一致；④**读路径宿主归属**（`GenericReadHostScoped`）——两表主键同号时工单 A 的数字引用必须命中旧表属于自己的记录、工单 B 自身读取正常、无同号旧记录时跨工单引用一律 404。为修复 ④ 暴露的串读，同批新增 `AttachmentService.GetFileForHost`（A4 的宿主定向变体：租户 + `biz_type`/`biz_id` 三重过滤，归属不符按未命中 → 回退旧表），旧端点改用它取流——改造前旧链路按 `ticket_id` 过滤，不存在该行为；该用例经负向验证（临时改回 `GetFile` 时用例失败并打印串读到的他人内容）。验收：`go test ./handlers/ticket_attachment/... ./service/... -run 'TestLegacyTicketAttachmentContract\|TestAttachment\|TestUploadAttachment\|TestResolveMIMEType\|TestParseAttachmentRef\|TestNormalizeMIME\|TestAllowedAttachmentMIMEs' -count=1` 全绿（**修复后复跑 2026-09-22**：`handlers/ticket_attachment 5.355s` — 4 项契约用例 PASS〔Frozen 1.14s / FieldParity 0.98s / HostScoped 0.89s / ErrorCodeParity 0.89s〕；`service 11.043s` — 22 项 PASS；门禁 `go build ./...` = 0、`go vet ./service/ ./handlers/ticket_attachment/ ./config/` = 0） |
| BE-7 | 内嵌图片引用完整性（净化对齐 + 归属校验） | `common/sanitizer.go` 或知识库调用点变更 + 工单/知识库更新路径校验 | BE-4、BE-5 | 1.5 | ✅ 2026-09-22 落地：①**净化对齐**——工单正文走 `internal/sanitize`（P0 起即放行 `data-attachment-id`，既有用例覆盖）；知识库正文走 `common.SanitizeHTML`（bluemonday UGCPolicy 默认剥掉全部 `data-*`），本次在 `common/sanitizer.go` 显式放行 `data-attachment-id`（取值 `^\d+$`）与 `data-align`（`left|center|right`），修复「知识库内的附件回链在落库时被静默抹掉」——不放行则删除引用保护与宿主校验都失去锚点。②**归属校验**——新增 `service/attachment_refs.go`：`ValidateRichTextInlineRefs(ctx, client, tenantID, bizType, bizID, html)`（+ `TicketService.ValidateRichTextInlineRefs` 供 handler 层复用），解析 `<img>` 的 `src` / `data-attachment-id`（兼容单双引号与无引号），单次批量查询通用表后按四档规则裁决：A4 规范地址（`/api/v1/attachments/{id}[/content|/preview|/download]`）必须命中「同租户 + 同宿主 + 存活」且与 `data-attachment-id` 一致，否则按 `ref_mismatch`/`not_found`/`inactive`/`not_hosted` 剥离；旧工单域内地址（`/api/v1/tickets/{tid}/attachments/...`）在 `tid != bizID` 时按 `legacy_cross_host` 剥离（`bizID=0` 的新建场景同样剥离）；其它地址（外链 / 旧存储文件名）只在「能解析到通用记录且宿主不符 / 已软删」的确定性证据下剥离——**不对「查不到」一概而论**，避免灰度期误伤旧表引用（D5）。违规以 `Warnw` 逐条告警（`attachment_id`/`reason`/`src`）。接线：`service/ticket_service.go`（`CreateTicket` bizID=0、`UpdateTicket` bizID=工单 ID）、`handlers/ticket/service.go`（`PUT /api/v1/tickets/:id` 主路径，经 `productionSvc` 复用）、`handlers/knowledge/service.go`（`CreateArticle`/`UpdateArticle`，`SetEntClient` 未注入时跳过）。失败策略：查询出错仅告警并按已清洗内容落库，不阻塞业务写入；`client` 缺失整体跳过（与改造前一致）。验收：`go test ./service/ -run TestValidateRichTextInlineRefs -count=1` 全绿（21 个子用例：同宿主保留 / 跨工单 / 不存在 / ref 不一致 / 已软删 / 跨租户 / 旧地址跨工单与同工单 / 外链 / 非规范地址两类 / 新建两类 / 知识库两类 / 混合正文只剥违规项 / 跳过路径 4 类）、`go test ./common/ -run TestSanitizeHTML_KeepsAttachmentRefAttrs -count=1` 全绿。**BE-7 全量回归（2026-09-23）**：`go test ./service/ ./common/ ./handlers/knowledge/ ./handlers/ticket/ -count=1` 通过（`ok itsm-backend/service 383.281s`）；同批另一次高负载全量运行中，与附件域无关的既有 BPMN 用例 `TestBiz_MultipleInstancesIndependent` 偶发失败 1 次（隔离运行与复跑均 PASS），对应生产就绪计划 W2 范围「fix flaky handlers and service tests」（`docs/delivery/production-readiness-program.md`），不计入 BE-7 缺陷 |
| BE-8 | 生命周期与级联清理 | 物理文件清理任务 + 宿主删除级联策略 + 保留期配置 | BE-3 | 1 | ✅ 2026-09-22 落地：①**清理任务**——`service/attachment_cleanup.go` 新增 `CleanupExpired`（按租户，支持 dry-run）：只扫「`status=deleted` 且 `deleted_at <= now-retention`」的候选，逐条引用复核 → 先删物理文件（`StorageProvider.Delete`，文件缺失视为成功）→ 再删元数据行 → 写 `purge` 审计；单条失败仅告警并计入 `Failed` 留待下轮，引用复核失败或命中引用一律跳过（宁可漏回收，不可误删）。配置 `attachment.cleanup_enabled` / `cleanup_purge_enabled` / `retention_days` / `cleanup_interval_minutes` / `cleanup_batch_size`（默认 false / false / 30 天 / 360 分钟 / 200 条，批大小硬上限 1000；布尔开关保持「零值即关闭」），仅部署级生效，`cleanup_purge_enabled=false` 即演练模式（只输出清单与 `summary`，不删任何数据）。②**级联策略**——`CascadeHostDeletion` 把宿主下 active 附件软删、仍被引用项跳过并保持 active；接线 `TicketService.DeleteTicket`（`SetAttachmentLifecycle` 注入）与知识库 `DeleteArticle`（先删宿主、成功后级联），级联失败仅 `Warnw` 且不回滚业务；未注入时行为与改造前完全一致（开关关闭 = 零行为变化）。③**后台任务**——`internal/bootstrap/app.go` 新增 `safeGo("attachment-cleanup", …)`：按租户循环、启动即跑一轮、`ctx.Done()` 退出，日志 `attachment cleanup task started/completed`（含 `summary`）。④**BE-6 遗留观察收口**——`AttachmentService.Get` 补 `status='active'` 过滤（A3 软删元数据 404，与 A2/A4/A6 口径一致）；知识库引用复核补 `knowledgearticle.DeletedAtIsNil()`（该实体未纳入全局软删拦截器，否则文章删除后内嵌图片永不可回收）。验收：`go build ./...` = 0；`go test ./config/ -run TestLoadConfig_Attachment -count=1` 通过；`go test ./service/ -run 'TestAttachmentCleanup\|TestAttachmentCascade\|TestAttachmentGetHidesSoftDeletedMetadata' -count=1 -v` 10 项全绿（`ok itsm-backend/service 7.294s`）。演练记录：`docs/testing/attachment-cleanup-drill-2026-09-22.md`（本地自动化演练已执行；目标环境 dry-run 步骤、SQL 对账与判定口径已固化，待环境窗口补留痕） |
| BE-9 | 评论附件后端（先上传后绑定） | `service/ticket_comment_service.go` 校验收紧 + 更新路径附件增删 + 用例 | BE-3、BE-4 | 1 | ✅ 2026-09-22 落地：①**绑定校验收紧**——`service/ticket_comment_service.go` 从旧表实体（`ticketattachment`）切到通用 `attachment`，创建 / 更新统一经绑定解析：仅接受「同租户 + `biz_type='ticket'` + `biz_id`=工单 ID + `usage='comment_attachment'` + `status='active'`」的通用附件（宿主权限沿用 A1 的 `ticket:create`），去重保序、单评论上限 10（`maxCommentAttachmentsPerComment`），任一不合法即整体拒绝、**无部分写入**；旧 `ticket_attachments` ID 不再被接受。②**更新路径补齐**——`UpdateTicketCommentRequest.Attachments *[]int` 三态语义：`nil` = 不修改、`[]` = 清空引用、非空 = 全量替换；创建请求 `attachments` 绑定 `omitempty,max=10`（`dto/ticket_comment_dto.go`）。③**无主回收与引用保护**——评论删除或更新移除引用后附件转无主，交 BE-8 保留期任务回收；引用存续期间 `DELETE /api/v1/attachments/:id` 返回 409/6105。④**同批修复 BE-8 回归**——落地中发现 `deleteAttachment` 复用 A3 `Get` 做宿主鉴权，而 BE-8 已把 `Get` 收紧为「软删即 404」，导致 A5 契约「重复删除幂等（第二次仍 200）」退化；新增 `AttachmentService.LookupForAuthorization`（按租户取任意状态元数据，仅供鉴权 / 幂等判定）修复，A3/A4 保持软删 404。验收：`go build ./...` = 0、`go vet ./service/ ./handlers/attachment/ ./handlers/ticket_comment/ ./dto/` = 0；`go test ./handlers/attachment/ -count=1` 通过（12.698s，含新增 `TestDeleteEndpointCommentReferenceReturns409` 与幂等回归 `TestDeleteEndpointInUseReturns409ThenIdempotent`）；`go test ./service/ -run 'TestCommentAttachment\|TestAttachment' -count=1` 通过（15.476s；`service/ticket_comment_attachment_test.go` 覆盖绑定正例与去重、反例矩阵〔旧表 ID / `usage=inline_image` / `usage=attachment` / 已软删 / 跨工单 / 跨租户 / 不存在 / 非法 ID〕、上限 10、更新三态与删评论释放引用）。**注：v1.0 决策已含此项（§11.4-2），本行为 2026-09-22 补登，原 WBS 遗漏** |
| BE-10 | 域内端点 usage 透传（权限回归修复） | `service/ticket_attachment_service.go` + `handlers/ticket_attachment/handler.go` + 用例 | BE-6、BE-9 | 1 | ✅ 2026-09-23 落地：**问题**——FE-3/FE-9 接入后发现权限回归：通用 `/api/v1/attachments*` 的静态权限码是兜底码 `attachment:write/read/delete`（§4.3 仅 admin/sysadmin 持有），而 `inline_image` / `comment_attachment` 又必须落通用表（旧 `ticket_attachments` 无 usage 列），导致 technician/agent 上传评论附件或正文内嵌图片必被 403。**修复（后端）**——① `UploadAttachment` 新增 `rawUsage` 形参，经 `normalizeTicketAttachmentUsage` 校验（空/空白 → `attachment`；接受三值；其余 `ErrAttachmentUsageInvalid` → 参数错误）；② 转发通用服务的条件由「写开关」改为 `usage != attachment || genericWriteEnabled`——非默认用途无条件落通用表，写开关只管旧 `attachment` 用途的双跑；`generic == nil` 时返回 `ErrAttachmentUsageBackendMissing`（500，不静默降级）；③ `DeleteAttachment` 短路条件由写开关改为 `generic != nil`（先试通用软删、未命中回退旧表硬删），开关关闭时仍可删除已落通用表的记录。**修复（前端）**——④ `AttachmentApi` 工单分支改为无条件域内前缀；新增 `ticketAttachmentContentUrl` / `ticketAttachmentPreviewUrl`，`TicketAttachmentSection` / `ticket-attachment-adapter` / `TicketDetail` / 工单创建页的下载与预览回退全部改用域内地址（不再回落通用 A4 兜底码）。验收：`go test ./handlers/ticket_attachment/ -count=1 -run TestLegacyTicketAttachmentContract` 与 `go test ./service/ -count=1 -run 'TestNormalizeTicketAttachmentUsage|TestUploadAttachmentRejects|TestUploadAttachmentUsage|TestCommentAttachment'` 全绿（新增 `handlers/ticket_attachment/handler_usage_test.go` 钉住「inline_image 落通用表且不写旧表 / 缺省用途仍写旧表 / 非法用途参数错误且不落库 / 写开关关闭仍可经域内端点软删」）；`npx jest`（`attachment-api` / `ticket-attachment-api` / `TicketAttachmentSection` / `ticket-attachment-adapter` 四套 44 项）全绿、`npx tsc --noEmit -p tsconfig.json` = 0 |
| BE-11 | 评论附件元数据下发（普通用户可渲染） | `dto/ticket_comment_dto.go` + `service/ticket_comment_service.go` + 用例 | BE-9 | 0.5 | ✅ 2026-09-23 落地：**问题**——BE-9 冻结的 `TicketCommentResponse.attachments: number[]` 只回 ID；通用 A3/A6 的静态码是兜底码 `attachment:read`（§4.3 仅 admin/sysadmin 持有），普通用户无法反查元数据，评论附件在 UI 上只剩裸 ID；域内列表 `GET /tickets/:id/attachments` 的 `genericListAll` 只按 biz_type/biz_id 过滤，`comment_attachment` 是否透出语义不定。**方案**——不改冻结字段，评论响应新增 `attachmentRefs`（`TicketCommentAttachmentRef{id,fileName,fileSize,mimeType,downloadUrl,previewUrl?}`；`fileSize` 与域内附件 DTO 一致取 `int`；`previewUrl` 仅 `image/*` 下发且 `omitempty`）。服务层新增 `commentAttachmentRefs` 批量补齐：`IDIn` 去重 + 同租户 + `biz_type='ticket'` + `biz_id`=工单 + `usage='comment_attachment'` + `status='active'`，与 BE-9 绑定校验同口径——跨宿主 / 其它用途 / 已软删的 ID 一律不下发，避免评论接口成为越权读附件的旁路；best-effort（查询失败仅 `Warnw`，不影响正文与 ID 列表）；创建 / 列表 / 更新三处接线。**契约**——`attachments` 语义冻结不变（软删记录仍保留 ID，由 BE-8 回收，前端据此渲染失效占位）。验收：`go build ./...` = 0、`go vet ./dto/ ./service/ ./handlers/ticket_comment/` = 0；`go test ./service/ -run 'TestCommentAttachment' -count=1`（`ok 4.419s`）与 `-run 'TestComment'`（`ok 4.823s`）全绿，新增 `TestCommentAttachmentMetadataRefs` 覆盖创建路径元数据与域内下载 / 预览地址、非图片不发 `previewUrl`、列表顺序与绑定一致、跨宿主与 `inline_image` 脏数据不泄漏、软删后元数据消失而 `attachments` 不变。**环境注**：`go test ./handlers/...` 全量本轮未跑通（`github.com/stretchr/objx` 经 proxy.golang.org 拉取超时，网络受限，与本次变更无关），同包 `go vet` 已通过。详见 `docs/api-reference.md`「评论附件（先上传后绑定）」 |

> **遗留观察（BE-6 期间发现，已在 BE-8 收口）**：通用服务 `AttachmentService.Get`（A3 `GET /api/v1/attachments/:id`）未过滤 `status='active'`，软删记录的**元数据**仍可读取（`GetFile`/`List`/`BatchGet` 均已过滤，故内容不可读；旧工单链路走 `GetFile`，不受影响）。现 `Get` 已补 `status='active'` 过滤（软删一律 404），回归用例 `TestAttachmentGetHidesSoftDeletedMetadata` 钉住该口径。

### P2 回填与双写（3 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| DO-1 | 回填脚本与对账工具 | `migrations/*_backfill*.sql` + 对账 SQL/脚本 | BE-2 | 1.5 | 行数/ID/字节数一致；抽样 200 条可下载 |
| DO-2 | 双写与差异监控 | 服务层双写开关 + 对账任务 | BE-3 | 1 | 双写开关可独立回退；差异告警在测试环境触发过 |
| DO-3 | 迁移演练（脱敏副本）与耗时记录 | 演练报告 | DO-1 | 0.5 | 记录锁表时长/耗时，明确窗口与回滚步骤 |

### P3 前端下沉（8 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| FE-1 | 类型与上传器抽象（含限额默认值对齐） | `lib/upload/types.ts`、`attachment-uploader.ts` | P0-1 | 1 | ✅ 2026-09-22 落地：①**公共契约**——新增 `src/lib/upload/types.ts`，按 §3.1 落 `AttachmentUsage`/`AttachmentHostContext`/`AttachmentRef`（字段与后端 `handlers/attachment/handler.go#AttachmentRef` 同名同义）/`AttachmentUploader`/`AttachmentDeleter`，并集中 `DEFAULT_ATTACHMENT_MAX_SIZE_MB = 10`、`DEFAULT_ATTACHMENT_MAX_COUNT = 10`、`DEFAULT_RICHTEXT_IMAGE_MAX_COUNT = 9`、`ATTACHMENT_USAGES`，业务域不得再自定义副本。②**单一上传入口**——新增 `src/lib/upload/attachment-uploader.ts`：`AttachmentUploadTransport`/`AttachmentDeleteTransport` 端口 + `createAttachmentUploader()`/`createAttachmentDeleter()` 工厂（宿主归一化：bizType 裁剪非空、bizId 正整数、usage 缺省 `attachment`，非法输入直接拒绝且不触达传输层；返回值经 `isAttachmentRef` 契约守卫，缺字段立即暴露）+ 守卫 `isAttachmentUsage`/`isAttachmentHostContext`/`isAttachmentRef`；**零业务 API 直连**（只 import `./types`），HTTP 与端点选择由 FE-4 `lib/api/attachment-api.ts` 注入。③**限额对齐（v1.0）**——`AttachmentField` 默认值 `maxSizeMB` 50 → 10、`validateAttachmentFile` 缺省同步引用常量，工单创建页显式传入的 `maxSizeMB={50}` 改为常量引用，消除「前端允许 50MB、后端 10MB 413」的错配。验收：`npx tsc --noEmit -p tsconfig.json` = 0（引用方 `AttachmentField`/`create/page.tsx` 编译通过）；`npx jest src/lib/upload --coverage=false` 全绿（10 项：上限断言〔10MB 通过 / 11MB 拒绝〕、三类守卫矩阵、宿主归一化、上传工厂正例与两条失败路径、删除工厂正反例） |
| FE-2 | 目录下沉 + re-export | `components/common/rich-text/*`、`common/attachment/*` | FE-1 | 1.5 | ✅ 2026-09-22 落地：①**迁移（`git mv` 保留 blame）**——`business/RichTextEditor.tsx` / `RichTextEditorImageMenu.tsx` / `RichTextEditorResizableImage.ts` / `RichTextImageViewer.tsx` → `components/common/rich-text/`，`business/AttachmentField.tsx` → `components/common/attachment/`；3 个行为用例（`RichTextEditor` / `RichTextEditorResizableImage` / `RichTextImageViewer`）随实现迁至 `common/rich-text/__tests__/`，组件间仅同目录相对引用（`RichTextEditor` → `./RichTextEditorResizableImage`、`./RichTextEditorImageMenu`），无需改写 import。②**兼容层**——旧 5 个路径保留 re-export 薄文件（文件头 `@deprecated` + 指向新路径 + 「一个发布周期后删除」），`export *` 覆盖全部具名 / 类型导出、`export { default }` 覆盖默认导出，`TicketDetail.tsx`、`create/page.tsx`、`DynamicFieldRenderer.tsx`（相对导入）、`lib/upload` 用例等既有调用点零改动可用。③**守卫用例**——新增 `business/__tests__/legacy-reexports.test.ts`：4 个组件族新旧默认导出 `toBe` 同一实现、`validateAttachmentFile` / `formatAttachmentSize` 函数同源、旧路径校验结果与新路径一致，另有 4 组新旧类型「双向可赋值」编译期探针（漏导出类型 / 定义漂移会让 `tsc` 直接失败）。验收：`npx tsc --noEmit -p tsconfig.json` = 0；`npx jest src/components/common/rich-text src/components/business/__tests__/legacy-reexports.test.ts --coverage=false` = 4 suites / 22 tests 全绿；`npx jest src/lib/upload --coverage=false` = 10/10 全绿（经旧路径 shim 解析）；`git log --follow` 可追溯到迁移前提交 |
| FE-3 | 调用点替换（工单全量） | `TicketDetail.tsx`、`create/page.tsx`、`TicketAttachmentSection.tsx`、`detail-tabs/adapters/ticket-attachment-adapter.ts`、`DynamicFieldRenderer.tsx` | FE-2 | 2 | ✅ 2026-09-23 落地：①**调用点全量迁移**——`TicketDetail.tsx`（编辑弹层内嵌图片上传 / 删除）、`app/(main)/tickets/create/page.tsx`（两阶段：先暂存 `File`，工单创建后经 `uploadAttachmentItems` 回填 `attachmentId`）、`TicketAttachmentSection.tsx`（列表 / 上传 / 删除统一走 `AttachmentApi.list` / `uploader()` / `removeById`）、`detail-tabs/adapters/ticket-attachment-adapter.ts`（详情 Tab 读写复用同一适配器）、`DynamicFieldRenderer.tsx`（附件字段宿主上下文透传）五处全部改走 `lib/api/attachment-api.ts` 唯一 HTTP 实现；旧 `TicketAttachmentApi` 只剩 FE-4 薄封装（由 `ticket-api.ts` 委托），无业务调用点直连。②**端点口径（含 BE-10 修复）**——工单域一律域内 URL（含 `usage != attachment` 的评论附件 / 内嵌图片，避免兜底码 `attachment:*` 触发普通用户 403）；下载 / 预览回退改用 `ticketAttachmentContentUrl` / `ticketAttachmentPreviewUrl`，且下载恒走域内 `GET /:id`（旧 `fileUrl` 的 `/preview` 形态不得作为下载地址，用例钉住）。③**验收**——`npx jest src/components/business/__tests__/TicketAttachmentSection.test.tsx --coverage=false` = 7/7（宿主上下文拉取与 `AttachmentRef` 渲染、uploader 昵称优先、删除后重取并回调、预览回退域内地址、下载域内端点 + 文件名、上传进度透传、10MB 拦截）；`npx jest src/components/business/detail-tabs/adapters/__tests__/ticket-attachment-adapter.test.ts src/lib/api/__tests__/attachment-api.test.ts --coverage=false` = 2 suites / 25 tests 全绿；`npx tsc --noEmit -p tsconfig.json` = 0；全仓检索旧上传调用无业务残留（仅 `lib/services/ticket-service-v2.ts`、`lib/api/collaboration-api.ts` 两个待 FE-8 / FE-9 收口的遗留客户端类）。**修复记录**：新用例首轮暴露两处类型缺陷（`Partial<AttachmentRef>` 展开使必填字段被推断为 optional → `tsc` 报 TS2322），已改为 `REF_DEFAULTS: AttachmentRef` + `Object.assign` 合并并复跑 tsc / jest 全绿 |
| FE-4 | API 客户端去重 | `lib/api/attachment-api.ts` + 旧客户端薄封装 | FE-1 | 1 | ✅ 2026-09-22 落地：①**唯一 HTTP 实现**——新增 `itsm-frontend/src/lib/api/attachment-api.ts`（A1 上传 / A2 列表 / A3 明细 / A4 内容地址 / A5 软删 / A6 批量回填 + `uploader()` / `deleter()` / `transport()` 端口）。端点解析：工单 + 默认用途保留旧域内 URL（D5 + 静态权限走 `ticket:*`，普通用户 token 不含 `attachment:*`，改走通用路由会被 403 拦下）；知识库 / 服务请求走 BE-5 别名路由（`usage` 表单字段透传，已对照 `handlers/attachment/handler.go#uploadToHost`）；其余宿主走通用 A1-A6。【2026-09-23 修订（见 BE-10）】**工单一律走域内端点**（含 `usage != attachment` 的评论附件 / 内嵌图片）：通用 `/attachments*` 的静态码是兜底码 `attachment:*`（§4.3 仅绑定 admin/sysadmin），普通用户走通用路由必 403；用途改由域内端点 `usage` 表单字段经 BE-10 透传通用表，既不丢语义也不再触发权限回归。②**契约护栏**——旧 `TicketAttachment` 响应在 `toAttachmentRef` 补齐 `bizType/bizId/usage`（BE-6 打开写开关后若透出通用字段则原样采信），通用响应经 `isAttachmentRef` 守卫，非法结构抛错。③**去重**——`ticket-attachment-api.ts`、`ticket-api.ts` 的附件方法改为委托薄封装（不再自建 FormData / 直连 httpClient），方法签名、URL 与返回字段（`filePath`/`fileType`/`uploader`）保持不变。④**自查修正**——`transport().remove` 原用 `bizId:0` 占位会在宿主归一化处抛错，改为无宿主上下文时恒走通用 A5；A6 请求体确认为 `{ ids }` 且后端返回裸数组（`handler.go#BatchQuery`）。验收：`npx tsc --noEmit -p tsconfig.json` = 0；`npx jest src/lib/api/__tests__/attachment-api.test.ts src/lib/api/__tests__/ticket-attachment-api.test.ts src/lib/api/__tests__/ticket-api.test.ts --coverage=false` = 3 suites / 92 tests 全绿 |
| FE-5 | 知识库接入 | 文章编辑处注入 Provider | FE-2、BE-5、BE-7 | 1 | 知识库图片上传/回显通过（此前为断链）；正文落库后 `data-attachment-id` 仍在 |
| FE-6 | 组件测试补位 | `__tests__` 用例（field 契约/失败重试/禁用态） | FE-2 | 0.5 | 定向 jest 全绿；上传失败可重试且不阻断其它文件 |
| FE-7 | i18n 文案与错误提示映射 | `lib/upload/error-messages.ts` + `lib/i18n/translations.ts` 增量 | FE-4 | 0.5 | ✅ 2026-09-22 落地：①**错误码单一映射**——新增 `src/lib/upload/error-messages.ts`：`ATTACHMENT_ERROR_CODES`（2003 + 6101-6107，与 `common/response.go` 同值）、`ATTACHMENT_ERROR_I18N_KEYS`（code → `attachment.error.*`）、`extractAttachmentErrorCode`（兼容 `Error.code` / axios 风格 `response.data.code` / 字符串码）与 `formatAttachmentError`（登记码 → 后端 message → 兜底文案逐级回落，超限带 `{maxSizeMB}`）。②**双语文案**——`lib/i18n/translations.ts` 的 zh-CN / en-US 各新增 `attachment.error.*`（10 条：forbidden/hostNotFound/invalidFilename/tooLarge/typeNotAllowed/inUse/quotaExceeded/rateLimited/uploadFailed/deleteFailed）与 `attachment.field.*`（22 条：状态标签 / 校验原因 / 上传删除失败 / 重试与移除 aria / 汇总提示）。③**错误码透出（通用解包、无附件特判）**——`http-client` 抛出的 Error 附带 `code`/`httpStatus`/`requestId`，并修复非 2xx 响应体被空 `catch` 吞掉、后端 message 无法透出的旧缺陷（附件 61xx 经 `Fail()` 返回真实 4xx，此前前端只能看到 `HTTP error! status: 4xx`）。④**组件去硬编码**——`AttachmentField` 状态标签 / 校验原因 / 上传与删除失败 / 重试与移除提示全量走 `t(FIELD_KEYS.*)`；`validateAttachmentFile` 返回值新增 `code` 并接受可选 `t`（不传 `t` 时保持原中文 reason 契约，FE-1/FE-2 用例零改动）。验收：`npx tsc --noEmit -p tsconfig.json` = 0；`npx jest src/lib/upload src/lib/api/__tests__/http-client.test.ts src/components/business/__tests__/legacy-reexports.test.ts --coverage=false` = 4 suites / 55 tests 全绿（含「翻译表 key 在两种语言齐备」「组件源码不再出现历史中文文案」两组回归；`http-client.test.ts` 403 用例改为断言 message + code=2003 + httpStatus=403 透出） |

### P4 灰度与切换（3 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| DO-4 | 灰度配置与看板 | 开关配置 + 监控面板 | BE-4、FE-3 | 1.5 | 4xx/5xx、P95、404/403 增量看板可查 |
| DO-5 | 读路径切换（列表/下载先行） | 切流记录 | DO-2、DO-4 | 1 | 灰度租户 0 新增错误；可一键回退 |
| DO-6 | 富文本内嵌图片切流 | 切流记录 | DO-5、FE-3 | 0.5 | 历史 URL 无 404；新图片走通用链路 |

### P5 收敛（3 人日）

| ID | 任务 | 产出 | 依赖 | 估时 | 验收 |
|:---|:---|:---|:---|:---:|:---|
| DO-7 | 停写旧表 + contract 迁移 | `*_contract.sql` 执行记录 | DO-5、DO-6 | 1 | 旧表只读；新写入 100% 落在新表 |
| FE-8 | 清理 re-export 与重复客户端 | 删除记录 + 引用全量检索为空 | FE-4、DO-7 | 1 | 全仓检索无残留引用；构建通过 |
| FE-9 | 评论附件前端接入与死代码清理 | `CommentPanel.tsx` 附件入口 + `common/attachment/CommentAttachmentField.tsx` + `collaboration-api` 死代码/重复类型删除 | FE-3、BE-9 | 0.5 | ✅ 2026-09-23 落地：①**UI 接入真实在用的 `CommentPanel`**——全仓检索 `TicketCommentSection.tsx` 除自身定义外**零引用**（死代码，原 WBS 按「在用组件」记录），故 FE-9 改在真实渲染路径 `detail-tabs/CommentPanel.tsx` 接入，并同批删除该死代码文件，避免两份评论 UI 长期漂移（`CommentPanel` 由 `TicketDetail.tsx` 与事件详情页经适配器驱动）。②**先上传后绑定**——新增 `components/common/attachment/CommentAttachmentField.tsx`：`CommentAttachmentField` 复用 `AttachmentField` 即时模式（体积 / 扩展名校验、进度、失败重试、10MB 与 10 条上限、i18n 全继承），只注入 `uploader` / `onDeleteUploaded`；`CommentAttachmentList` 消费 BE-11 `attachmentRefs`（域内下载 / 预览地址），`attachments` 多于元数据的 ID 渲染 `attachments.expired` 失效占位而非静默丢弃。`CommentAdapter` 新增可选 `uploadAttachment` / `removeAttachment`（未实现的域不渲染附件入口，事件评论零影响），`ticket-comment-adapter` 固定 `{bizType:'ticket', bizId, usage:'comment_attachment'}` 走 `AttachmentApi.upload` / `removeById`（BE-10 域内端点，普通用户不触发兜底码 403）。③**创建 / 编辑提交**——创建携带 `attachments: number[]`（为空则省略，`omitempty` 语义一致）；编辑按 BE-9 三态语义：未触碰附件省略该字段（保持原引用）、触碰后全量替换；编辑态删除只改本地列表、保存时经引用替换真正解绑（规避「附件仍被引用 → 409/6105」），创建态删除仍即时调 `removeAttachment`；上传中 / 失败条目未落定前拒绝提交（新增 `attachments.pendingTip`，zh/en 双语）。④**类型收敛与死代码清理**——`CommentAttachment` 唯一来源为 `types/comment.ts`（BE-11 契约 `id/fileName/fileSize/mimeType/downloadUrl/previewUrl?`，`types/index.ts` 原样转出），删除 `types/collaboration.ts` 的重复接口（string id 旧契约）、`CollaborationApi.uploadAttachment` / `deleteAttachment`（连同其自测断言；`uploadAttachment` 的 `commentId` 字段后端从未解析、删除调用的路由不存在）与 `business/TicketCommentSection.tsx`。验收：`npx tsc --noEmit -p tsconfig.json` = 0；`npx jest src/components/common/attachment/__tests__/CommentAttachmentField.test.tsx src/components/business/detail-tabs/adapters/__tests__/ticket-comment-adapter.test.ts src/lib/api/__tests__/collaboration-api.test.ts --coverage=false` = 3 suites / 49 tests 全绿（新增用例覆盖：default 10 条上限与即时上传注入、解绑按 attachmentId 透传、未注入 remove 不改删除实现、BE-11 元数据渲染 / 预览仅图片、失效占位、无附件不渲染；适配器宿主上下文与 `previewUrl ?? fileUrl` 回落）。全仓检索 `CollaborationApi.uploadAttachment` / `deleteAttachment` / `TicketCommentSection` 无残留引用 |
| DO-8 | 文档与清单更新 | `docs/api-reference.md`、`docs/acl-manifest.yaml`（重新生成）、`docs/documentation-style-guide.md`（登记 `docs/plan/`）、本方案状态 | DO-7 | 1 | 文档评审通过；`docs/plan/` 已登记；本方案标记「已落地」并附证据链接 |

**合计：33 人日**（v1.0 增量：P0-3 兜底码登记 +0.5、BE-9 评论附件后端 +1、FE-9 评论前端接入 +0.5；v0.9 已修正原 P1 明细与概览不符问题并新增 BE-7/BE-8/FE-7。不含评审与观察期；建议排期 5~6 周，观察期与其它迭代并行）。

---

## 8. 测试与门禁

| 层级 | 覆盖点 | 门禁 |
|:---|:---|:---|
| 后端单测 | 宿主校验（跨租户/不存在/已删除宿主）、幂等、限额、文件名清洗、软删与引用保护（含评论引用）、评论附件绑定校验、加固继承（magic bytes/病毒扫描/写后大小复核） | 随 PR 必跑：`go test ./handlers/attachment/... ./service/... ./router/...` |
| 错误码与响应 | 6101-6107 在 `common/response.go` 登记，`Fail`/`FailWithData` 双 switch 映射正确（409/413/415/422/429）；新端点无 `201`/`204` 兼容分支（一律 `200 + code`），无未登记码静默 200 路径 | 表驱动单测：每个码 → HTTP 状态断言；兼容性断言（AC-16） |
| 权限矩阵测试 | §4.2 每一行 × 角色（管理员/租户管理员/普通用户/无权用户）的正反用例；兜底码 `attachment:*` 的登记与角色绑定（v1.0 启用） | `go test ./tests/parity/ ./internal/authz/ ./pkg/seeder/... ./router/... -run 'Guard|Catalog|Parity'`；新增 biz_type 必须补行 |
| 迁移门禁 | `migrations/` 语法、命名、ledger 调和、expand/contract 配对 | `go run ./cmd/migration-lint`（已核对仓库 Makefile 无该目标，直接执行）；脱敏副本演练报告 |
| 前端组件测试 | `AttachmentField`（暂存/即时/失败重试/禁用态）、`RichTextEditor`（上传注入/上限/净化）、Viewer | 定向 jest（按用例路径执行，避免全量超时）；迁移目录后同步更新 import；6101-6107 文案 zh-CN/en 齐备 |
| 契约测试 | 旧工单端点逐字段 JSON 对照（BE-6）；新旧读路径同数据一致性 | CI 必跑，字段增减即红灯 |
| E2E | ① 工单创建页粘贴图片→提交→详情回显；② 工单详情编辑→缩放/对齐→保存→刷新回显；③ 知识库文章图片上传；④ 越权用户访问附件 API 全部 403/404；⑤ 评论附件：上传→评论提交→回显/下载→删除评论后清理（v1.0 纳入） | 发布前 smoke；截图归档 |
| 性能 | 10MB 上限文件上传（边界）、20 并发上传、1GB 文件 Range 下载、`batch-query` 200 条（含鉴权是否退化为 N+1） | 记录 P95；回归超过基线 20% 阻断 |
| 兼容 | 历史富文本 HTML（含 `/tickets/:id/attachments/:ref/preview` 与 `data-attachment-id`）在切换后全部可渲染 | 使用生产脱敏样本抽样 ≥100 条 |

---

## 9. 风险与缓解

| # | 风险 | 影响 | 缓解 |
|:--|:---|:---|:---|
| R1 | 历史富文本 URL/ID 失效导致裂图 | 用户可见回归 | 保 ID 平移 + 兼容端点永久保留 + 抽样回归（§8） |
| R2 | 删除附件致历史文档裂图/评论缺件 | 数据不可逆 | 软删 + 引用保护（409，不提供"仅解绑"降级；覆盖 `inline_image`/`comment_attachment`）；物理清理延后（BE-8） |
| R3 | 权限语义漂移（上传从 `create` 改 `write`） | 越权/误拒 | 工单保持 `ticket:create`；矩阵测试固化；变更走单独评审 |
| R4 | 多态表查询缺租户条件造成越权 | 安全事件 | 查询强制 `tenant_id`；守卫测试 + 代码评审清单 |
| R5 | 双写窗口数据不一致 | 数据修复成本 | 幂等回填 + 小时级对账 + 差异告警；窗口内可停写回退 |
| R6 | 迁移锁表影响在线业务 | 可用性 | expand/contract + 脱敏副本演练 + 低峰执行 + 预估耗时报告 |
| R7 | 前端下沉引发大范围 import 回归 | 构建失败/页面白屏 | 目录级 `git mv` + re-export 过渡 + 定向组件测试 + 分域替换 |
| R8 | 知识库/工单 `data-attachment-id` 跨域引用 | 越权读取他人附件 | 提交时按 `biz_type+biz_id` 校验归属；跨域引用直接剥离并提示 |
| R9 | 存储策略不统一（本地/对象存储差异） | 环境事故 | 新增 `StorageProvider` 抽象（现状无抽象）+ 历史路径双规则寻址 + 集成测试；对象存储不在本次范围 |
| R10 | 知识库净化策略剥离 `data-attachment-id`，引用保护与归属校验失效 | 越权引用/裂图 | §3.5-3 净化对齐（BE-7）；知识库正文属性保真纳入 AC-13 |
| R11 | 前端默认 50MB 与后端现网 10MB 不一致导致 413 | 用户可见失败 | 已拍板维持 10MB：前端默认值对齐 10MB 并与 `upload` 分组**同源下发**（§3.4）；E2E 覆盖 10MB 边界与超限用例 |
| R12 | 调用点漏改（创建页/附件区块/adapter/评论附件）导致回归 | 功能缺失 | FE-3 明列 5 处调用点 + FE-9 评论附件；验收要求全仓检索无残留旧上传调用 |

---

## 10. 验收标准（AC）与 Definition of Done

| 编号 | 验收标准 |
|:---|:---|
| AC-1 | 通用附件 6 个端点（A1-A6）在契约、实现、文档三处一致；错误码表逐条有对应用例 |
| AC-2 | 工单附件端点改造后响应字段与状态码与改造前**逐字段一致**（合同测试固化） |
| AC-3 | 历史富文本 HTML（含 `data-attachment-id` 与 preview URL）抽样 ≥100 条全部可正常渲染与下载 |
| AC-4 | 权限矩阵测试覆盖 §4.2 每一行 × 4 类角色，正反用例全绿；无新增越权路径 |
| AC-5 | `attachments` 表回填对账通过（行数/ID/字节数/抽样下载），且保 ID |
| AC-6 | 双写窗口内差异告警为零；关开关可无损回退 |
| AC-7 | 前端公共控件 `components/common/{rich-text,attachment}` 无业务 API 直连（静态检查/评审确认） |
| AC-8 | 富文本内嵌图片在"未注入上传能力"时不再发起任何请求（fail-fast），注入后全链路可用 |
| AC-9 | 知识库图片上传从"断链"变为端到端可用 |
| AC-10 | `migration-lint`、后端单测、前端定向组件测试、E2E smoke 全绿；文档与清单同步更新 |
| AC-11 | 6101-6107 错误码在 `common/response.go` 登记，HTTP 映射与前端 i18n 文案逐条有断言 |
| AC-12 | 通用服务继承现网加固（magic bytes 嗅探、`AttachmentVirusScanner`、写后大小复核、文件名清洗），单测行为与现网一致 |
| AC-13 | 知识库正文落库后 `data-attachment-id`/`data-align` 保留；跨域或不存在引用被剥离并留痕 |
| AC-14 | 前端 5 处调用点全部替换（TicketDetail/创建页/TicketAttachmentSection/detail-tabs adapter/DynamicFieldRenderer），`collaboration-api` 死代码删除（v1.0 决策，FE-9）；全部文案 i18n 齐备 |
| AC-15 | 评论附件端到端可用：上传（`usage='comment_attachment'`）→ 评论创建/编辑携带 `attachments` → 列表回显/下载；归属/用途/状态校验有正反用例；评论删除后无主附件由清理任务回收 |
| AC-16 | 新附件端点全部 `HTTP 200 + code`（无 `201`/`204` 兼容分支）；61xx 在 `Fail`/`FailWithData` 双 switch 有映射，无"未登记码静默 200"路径（表驱动断言） |
| AC-17 | 单文件 10MB 上限前后端一致（前端默认值与 `upload` 分组同源下发）：10MB 边界文件通过、超限返回 413/6103 |
| AC-18 | 兜底码 `attachment:read/write/delete` 完成登记、角色绑定与 codegen（D3 启用）；守卫测试与 ACL 清单核对通过 |

**DoD（发布门禁）**

1. §8 各层级门禁全绿，且证据（报告/截图/日志）归档到 PR 或发布记录。
2. 灰度租户观察 ≥1 周（读）、≥2 周（停写旧表）无回滚触发。
3. 开关默认值回到安全档位，回滚手册（§6.1）在发布说明中可查。

---

## 11. 附录

### 11.1 代码坐标索引

| 主题 | 坐标 |
|:---|:---|
| 富文本编辑器 | `itsm-frontend/src/components/business/RichTextEditor.tsx`（`defaultUploadImage` 见 L84-138） |
| 附件字段 | `itsm-frontend/src/components/business/AttachmentField.tsx` |
| 工单图片上传调用点 | 详情：`itsm-frontend/src/components/ticket/TicketDetail.tsx:521-536`；创建页两阶段：`src/app/(main)/tickets/create/page.tsx:241-259`、`:433-462`，辅助 `src/lib/rich-text/staged-images.ts` |
| 其它附件调用点 | `src/components/business/TicketAttachmentSection.tsx`、`src/components/business/detail-tabs/adapters/ticket-attachment-adapter.ts`、`src/lib/api/collaboration-api.ts:354-382`（评论附件死代码，v1.0 纳入：FE-9 删除并改走通用客户端） |
| 评论附件契约 | 前端 `src/lib/api/ticket-comment-api.ts:15,33`（`attachments?: number[]`）；后端 `dto/ticket_comment_dto.go:14,32`、`service/ticket_comment_service.go:65-77,236,286-291`、`ent/schema/ticket_comment.go:34-36`；UI `src/components/business/TicketCommentSection.tsx` |
| 字段分派 | `itsm-frontend/src/components/business/DynamicFieldRenderer.tsx`（richtext/attachment 分支） |
| 知识库上传 API | `itsm-frontend/src/lib/api/knowledge-base-api.ts:254-260` |
| 前端代理 | `itsm-frontend/src/app/api/[...path]/route.ts` |
| 工单附件路由 | `itsm-backend/router/ticket_routes.go:170-180` |
| 工单附件处理器 | `itsm-backend/handlers/ticket_attachment/handler.go` |
| 工单附件服务 | `itsm-backend/service/ticket_attachment_service.go` |
| 工单附件模型/DTO | `itsm-backend/ent/schema/ticket_attachment.go`、`itsm-backend/dto/ticket_attachment_dto.go` |
| 权限权威源 | `itsm-backend/internal/authz/catalog.go`、`itsm-backend/internal/authz/roles.go` |
| 鉴权中间件 | `itsm-backend/middleware/rbac.go:975`（`RequirePermission`） |
| 迁移与门禁 | `itsm-backend/migrations/`、`itsm-backend/migration/`、`itsm-backend/cmd/migration-lint/main.go` |
| 部署迁移开关 | `itsm-backend/internal/bootstrap/app.go`（`ITSM_AUTO_MIGRATE` 仅 bootstrap 任务） |
| 净化策略 | 工单：`itsm-backend/internal/sanitize/richtext.go:66-70`；知识库：`itsm-backend/common/sanitizer.go:19-43`（调用点 `service/knowledge_service.go:56,172`、`handlers/knowledge/service.go:132,155`） |
| 附件安全加固参考实现 | `itsm-backend/service/ticket_attachment_service.go:31-61`（病毒扫描接口）、`:111-135`（magic bytes）、`:149-152`（扫描失败删文件）、`:160-161`（`file_type` 同写 MIME） |
| 审计与限流 | `itsm-backend/router/router.go:456`（`AuditMiddleware`）、`:256-289`（限流装配）、`middleware/security.go:133-134`（`RateLimitMiddleware`）、`service/auditlog_service.go` |
| 响应与错误码 | `itsm-backend/common/response.go:22-44`（码定义）、`:56-107`（`Fail`/`FailWithData` 双 switch） |
| codegen 指令 | `itsm-backend/middleware/rbac.go:746`（authz 预检映射）、`itsm-backend/ent/generate.go`（ent 生成） |

### 11.2 关联文档

- `docs/architecture/ticket-create-page-rich-input-optimization.md`（富输入优化设计，本方案的上游背景）
- `docs/api-reference.md`（API 文档，P5 同步更新）
- `docs/acl-manifest.yaml`（权限清单，生成物；P5 由仓库脚本重新生成）
- `ROADMAP.md`（迁移与升级安全、权限治理现状）
- `docs/documentation-style-guide.md`（文档规范）

### 11.3 术语

| 术语 | 含义 |
|:---|:---|
| 宿主（host） | 附件的归属业务单据，由 `biz_type + biz_id` 标识 |
| usage | 附件用途：`attachment`（普通附件）/ `inline_image`（富文本内嵌图片）/ `comment_attachment`（评论附件，先上传后绑定） |
| expand / contract | 迁移的两个阶段：先加新结构并回填，后停用旧结构并清理 |
| fail-fast | 未配置上传能力时直接禁用入口并告警，而不是发出必然失败的请求 |
| 双规则寻址 | 历史 `file_path` 按原值读取、新文件按新 key 规则写入，二者并存直到历史文件自然淘汰 |
| 加固继承 | 通用服务必须平移现网工单附件的安全能力（magic bytes、病毒扫描、写后大小复核、文件名清洗），不允许能力回退 |
| 先上传后绑定 | 评论附件两阶段：附件先上传获取 ID，再随评论创建/更新提交 `attachments` 完成绑定（与创建页图片暂存同构） |

### 11.4 修订记录

| 版本 | 日期 | 说明 |
|:---|:---|:---|
| v0.9（历史） | 2026-09-22 | 完整性审查修订版（4 项 P0 + 9 项 P1 闭环，6 项 P2 精度修订） |
| v1.0（本版，已冻结） | 2026-09-22 | P0 契约冻结评审通过；四项决策落定并落文：10MB 维持 / 评论附件纳入 / 错误码按最佳实践 / D3 兜底码启用；增量见下 |
| v1.0.1（P0 落地修订） | 2026-09-22 | P0-2 落地（`RichTextEditor` 无上传能力时 fail-fast + 单测）；P0-4 落地（配置键定稿为下划线扁平形式并登记：`config.go#AttachmentConfig`、`config.yaml.example`/`deploy/config.yaml` 样例、`system_config_service` attachment 分组、`docs/install.md` §3.1） |
| v1.0.2（P0 完成） | 2026-09-22 | P0-3 落地（`attachment:read/write/delete` 登记 + admin/sysadmin 绑定 + `go generate ./middleware/...` + ACL 清单重新生成；定向守卫全绿）。验收口径修订：清单为路由派生生成物，`attachment.*` 可见性与「路由引用」条件转 BE-4 复核（§4.3-6）；另记录两项既有失败（`--check` 的 password-policy 路由、seeder workflow-core 用例）为非本批因果 |
| v1.0.3（P1 启动） | 2026-09-22 | BE-1 落地：`migrations/20260922_create_attachments_expand.sql`（含 RLS 策略）与 `_down.sql` 配套；`migration-lint` 与迁移自发现测试通过（详见 §5.1 落地说明）。顺带修正 §7 小节标题人日（P1 11.5→12.5、P3 7.5→8），与 v1.0「31→33」及阶段总览对齐 |
| v1.0.4（P1 续） | 2026-09-22 | BE-2 落地：`ent/schema/attachment.go` + `go generate ./ent`（17 列与 DDL 逐列一致、无 ForeignKeys）；`go build ./...` 与 `go build ./ent/...` 均通过。BE-3 落地：`service/attachment_service.go` + 14 项单测全绿（详见 §7 BE-3 行）。取证要点：① 61xx 错误码与 A1-A6 路由登记属 BE-4（当前代码零登记，`common/response.go` 尚无 413/415/429 分支）；② `uq_attachments_client_token` 由迁移 DDL 建立，`client.Schema.Create(ctx)` 默认不删多余索引，故生产可存活；③ 存储抽象在本仓库无既有实现（`ticket_attachment_service.go` 为本地直写），本期新增接口 + 本地实现，对象存储适配留待后续。 |
| v1.0.5（P1 续） | 2026-09-22 | BE-4 落地：`handlers/attachment/handler.go` + `router/attachment_routes.go`（A1-A6）+ 6101-6107 双 switch 登记 + 通用 `TooManyRequestsCode=429`；端到端测试全绿，路由/authz/预检守卫全绿（详见 §7 BE-4 行）。**ACL 清单脚本修复**（§4.3-7）：路径正则 `+`→`*` 找回全部分组根路径、按位置解析 Setup 形参挂载前缀；清单 599→696 路由、覆盖 99.86%，6 条 `attachment:*` 路由全部在册（三条件「路由引用」闭环），`--check` 未保护项收敛为既有 1 条（password-policy）。 |
| v1.0.6（P1 续） | 2026-09-22 | BE-5 落地：附件域内别名 12 条路由接入知识库与服务请求（静态权限复用宿主码，零新增权限码）；`service_request` 宿主注册进通用附件服务；别名入口复用 A1/A2/A4/A5 主体并新增**归属复核**（附件不属于路径宿主 → 404，非法宿主/附件引用同样 404）。同批 `cmd/authz-gen` 与 ACL 清单再生成（预检 +12；清单 696→708、99.86%，`--check` 仍仅既有 password-policy 一条，`--check` 退出码 1 属既有问题）。测试：别名闭环 + 跨宿主隔离 2 项新增用例全绿，路由/authz/预检守卫全绿（详见 §7 BE-5 行）。 |
| v1.0.7（P1 后端能力完成） | 2026-09-23 | BE-6（旧工单端点薄适配 + 字段级/错误码/宿主归属契约对照）、BE-7（内嵌图片引用完整性：知识库 sanitizer 放行 `data-attachment-id`/`data-align` + `ValidateRichTextInlineRefs` 四档裁决）、BE-8（保留期物理清理 + 宿主删除级联 + 演练记录）、BE-9（评论附件后端：绑定校验收紧 / 更新三态 / 无主回收与引用保护；同批修复 A5 幂等回归）陆续落地，验收证据见 §7 各行。 |
| v1.0.8（BE-10 路由修订） | 2026-09-23 | BE-10 落地：通用 `/attachments*` 兜底码仅 admin/sysadmin 持有，工单域非默认用途（评论附件 / 内嵌图片）改由**域内端点透传 `usage`** 到通用表，消除普通用户 403 权限回归；FE-4 端点解析规则同步修订（工单一律域内），前端下载 / 预览回退改为域内地址。详见 §7 BE-10 行。 |
| v1.0.9（BE-11 评论附件元数据） | 2026-09-23 | BE-11 落地：评论响应新增 `attachmentRefs`（`id/fileName/fileSize/mimeType/downloadUrl/previewUrl?`），服务层按 BE-9 同口径（同租户 + 同工单 + `usage=comment_attachment` + 存活）批量补齐，跨宿主 / 其它用途 / 已软删一律不下发；`attachments` ID 契约保持不变，`docs/api-reference.md` 同步补响应示例。详见 §7 BE-11 行。 |
| v1.0.10（FE-9 评论附件前端） | 2026-09-23 | FE-9 落地：评论附件 UI 接入真实渲染路径 `detail-tabs/CommentPanel.tsx`（新增 `common/attachment/CommentAttachmentField.tsx`，`CommentAdapter` 增可选 `uploadAttachment`/`removeAttachment`，`ticket-comment-adapter` 固定 `usage='comment_attachment'` 走 `AttachmentApi`），创建携带 `attachments`、编辑按 BE-9 三态语义提交、展示消费 BE-11 `attachmentRefs` 并含失效占位；同批删除 `TicketCommentSection.tsx`（零引用死代码）、`CollaborationApi.uploadAttachment`/`deleteAttachment` 与 `types/collaboration.ts` 的重复 `CommentAttachment`，类型唯一来源收敛到 `types/comment.ts`。`npx tsc --noEmit` = 0，定向 jest 3 suites / 49 tests 全绿。详见 §7 FE-9 行。 |
| v1.0.11（开发环境收口完成） | 2026-09-24 | 数据侧：`20260922_create_attachments_expand` 与 `20260923_attachments_backfill` 在开发库落地并计入 `schema_migrations`，回填对账全绿（15/15 行、ID 与序列对齐、零字段差异）；配置侧：`config.yaml#attachment` 四个开关按开发环境直开（读/写/内嵌图片 true，双写 false，cleanup false）；验证侧：`go build ./...` = 0、后端定向测试全绿、`npx tsc --noEmit` = 0、定向 jest 12 suites / 137 tests 全绿，并完成 A1-A6 + 域内别名的 HTTP 端到端冒烟（上传/详情/下载字节比对/批量回填/双路径删除/宿主越权 404）。按用户「开发环境不考虑切换 / 灰度」指令，P4 灰度与 P5 停写旧表本期不执行，详见 §6.2。 |

**v0.9 修订项（对照完整性审查结论：4 项 P0 + 9 项 P1 全部闭环，6 项 P2 精度问题一并修订）**：

1. 响应码对齐仓库统一包装：A1/A5 改为 HTTP 200 + `code=0`；新增 §3.4 错误码落地要求与 6101-6107 映射（含 `Fail`/`FailWithData` 双 switch 扩展）。
2. DDL 补 `client_token` 列与 `uq_attachments_client_token` 唯一索引，兑现 A1 幂等承诺。
3. 明确加固继承：magic bytes 嗅探、`AttachmentVirusScanner`、写后大小复核、文件名清洗纳入 BE-3 验收（§3.5-8、§5.3）。
4. 补全前端调用点清单：创建页两阶段、`TicketAttachmentSection`、detail-tabs adapter、`collaboration-api` 评论附件；FE-3 扩面、FE-4 合并三份实现。
5. 修正事实错误：创建页内嵌图片链路可用（非"断链"），§1.1/§1.3/TL;DR 已同步；补第三处无落点调用 `DELETE /attachments/:id`。
6. 知识库净化对齐（`data-attachment-id` 保真）写入 §3.5-3、§3.3 时序与 BE-7，并新增 AC-13/R10。
7. `file_type` 语义更正为"与 `mime_type` 同值（历史语义）"；存储抽象更正为"现状无抽象，新建 `StorageProvider`"并明确双规则寻址（§5.3/R9）。
8. 删除语义去歧义：仅软删 + 被引用 409，不提供"仅解绑"降级（§3.2 A5、§5.3、R2）。
9. `knowledge_article` 删除动作权限改 `knowledge:delete`（§4.2）。
10. `roles.go` 标识符更正为 `BuiltinRolePermissionCodes()`，codegen/守卫命令按仓库实际指令写实（§4.3、§8）。
11. 审计与限流改为复用既有 `AuditMiddleware`/`RateLimitMiddleware`（§3.5-6/-9）；batch-query 明确按宿主去重批量鉴权（§3.2 A6）。
12. WBS 补齐 4 项缺口：BE-7（引用完整性）、BE-8（生命周期与级联）、FE-7（i18n）、DO-8 扩展（style guide 登记 + acl-manifest 重新生成）；修正人日算术（26→31）。

**v1.0 决策与增量（2026-09-22，已冻结）**：

1. **单文件限额维持 10MB**（不放开到 50MB）：§3.4 边界值与"限额决策"块、§3.1 `AttachmentField.maxSizeMB` 默认 50→10、§5.3 大小/类型策略、§8 性能用例改为 10MB 边界、R11 缓解改为"默认值对齐 + `upload` 分组同源下发"；A7 presign 取消（限额内 multipart 直传足够）。
2. **评论附件纳入本次范围**：修正 §1.1/§1.2/§1.3 表述（`collaboration-api` 两方法为**无调用方死代码**；真实契约在 `TicketCommentApi` + `ticket_comment_service` + `ent/schema/ticket_comment.go`）；新增 §3.2 注（先上传后绑定、归属/用途/状态校验、无主回收）、§5.3 评论引用保护、BE-9、FE-9、AC-15；术语表补"先上传后绑定"。
3. **错误码与响应按最佳实践**：新增 §3.4 兼容性决策（不引入 `201`/`204` 兼容分支、统一 `200 + code`、禁止未登记码静默 200）与 AC-16；§8 错误码门禁增补兼容性断言。
4. **D3 兜底权限码启用**：§2.3 D3、§4.1、§4.2、§4.3 去掉"仅按需登记"条件；P0-3 估时 1→1.5 并明确 codegen 与守卫命令；新增 AC-18；§8 权限矩阵门禁补"兜底码登记与角色绑定"。
5. **WBS 人日 31→33**：P0 +0.5（兜底码登记）、P1 +1（BE-9 评论附件后端）、P3 +0.5（FE-9 评论前端接入）；阶段总览、各节小计与 §7 合计同步；四项决策写入 P0 冻结记录（§6）。
