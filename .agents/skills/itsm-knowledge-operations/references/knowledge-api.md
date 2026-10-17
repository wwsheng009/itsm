# 知识库 REST API 完整契约（references/knowledge-api.md）

> 基线：2026-10-16 工作树（`d01b916e` + 知识库技能新增）。行号漂移，以符号定位。
> 全局：Base `/api/v1`；信封 `{code,message,data}`；成功 `code=0`。**字段命名混合**：文章 DTO 为 camelCase；`/knowledge/search` 返回 snake_case（历史契约，见 §4）。
> 错误码映射与工单域一致：1001/1002/4000→400，2001/2002→401，2003/2004→403，4004→404，4220→422，5001→500。
> 脚本封装：`scripts/itsm_kb.py`（22 子命令，覆盖本文全部操作）。

## 0. 认证 / 租户 / 权限

- 认证：PAT 或 JWT，`Authorization: Bearer`；租户绑定令牌无需 `X-Tenant-Code`（不一致 → 401 `TENANT_MISMATCH_REJECTED`）。
- 路由权限码：

| 操作 | 权限码 |
|---|---|
| 列表/详情/版本读/版本对比/搜索/推荐/最近/统计/分类读/附件读 | `knowledge:read` |
| 创建/更新/发布/下架/复核/恢复版本/受限分类读+写/附件上传 | `knowledge:write` |
| 删除文章 / 附件删除 | `knowledge:delete` |

- 默认角色（authz/roles.go）：`end_user` 仅 `knowledge:read`；`agent`/`l2_support`/`ops_engineer`/`dba` 等 read+write；admin 家族另含 `knowledge:delete`（租户管理员还持 `knowledge:admin`）。
- 可见性两层：
  - **L0 受限分类**：被 `/knowledge/categories/restricted` 纳管的分类仅授权角色可读；检索/详情按访问者身份过滤，无身份时 fail-closed。
  - **L1 时效（citable）**：未生效/已过期/逾期未复核的文章不进入检索结果；详情仍可读，带 `freshness.verdict` 提示。

## 1. 文章 CRUD

### 创建 `POST /knowledge/articles`（`knowledge:write`）

`dto.CreateKnowledgeArticleRequest`：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| title | string | ✅ | 服务端 strict 消毒 |
| content | string | ✅ | 1–200000；UGC 消毒（保留常见富文本，剥离 script/on*/javascript:） |
| category | string | ✅ | **必须命中 10 值白名单**（见 §5），否则 400 |
| tags | []string | ❌ | 响应回数组 |
| contentType | string | ❌ | text/markdown/html/rich_text；缺省由服务端按内容判定 |
| validFrom / validUntil | RFC3339 | ❌ | 时效窗口 |
| reviewIntervalDays | int | ❌ | 0–3650；>0 且逾期未复核会被 L1 过滤（**创建即 overdue**：发布后需补 review 才可检索，见 §9-9） |
| authorityLevel | int | ❌ | 0 普通 / 10 部门推荐 / 20 官方标准 / 30 唯一真相源 |

响应：`KnowledgeArticleResponse`（`status=draft`）。
> 分类注意（2026-10-16 起）：创建/更新均做**联合校验**——允许集合 = 静态白名单(12) ∪ `GET /knowledge/categories` 返回值；两套集合已统一为一条策略，见 §5。

### 列表 `GET /knowledge/articles`（`knowledge:read`）

查询参数：`page`(默认1)、`pageSize`(默认10，上限100)、`category`、`status`（`published`/`draft`；其他值=不过滤）、`search`（title/content/tags 模糊）。
响应：`{items, articles(别名，等价 items), total, page, pageSize}`。

### 详情 `GET /knowledge/articles/:id`（`knowledge:read`）

响应：`KnowledgeArticleResponse` + `freshness{verdict: ok|not_yet_effective|expired|review_overdue, citable, reviewDueAt?}`。
> 已知缺口：响应 `author` / `views` 目前未回填（恒为空串/0），勿依赖；`contentType` 是解析后的生效值。

### 更新 `PUT /knowledge/articles/:id`（`knowledge:write`，部分更新）

字段：title/content/category/tags/contentType/validFrom/validUntil/reviewIntervalDays/authorityLevel（不传=不改，指针语义）。
- `status` 字段**被刻意忽略**——发布态只能由 publish/unpublish 变更。
- `lastReviewedAt` 不可写（防篡改时效）；复核走 `POST /:id/review`。
- 已设时效当前不支持改回"永久"。
- **更新=一次编辑**：写回内容一律回到草稿态（已发布文章会被下架），需重新 publish 才生效；更新路径与创建同做分类联合校验（非法分类 → 400）。

### 删除 `DELETE /knowledge/articles/:id`（`knowledge:delete`）

软删除；文章有活跃协作会话（active/idle）时拒绝。

## 2. 发布 / 下架 / 复核

- `POST /knowledge/articles/:id/publish`（write），body 可整体省略；`{changeLog≤200}` 缺省时服务端生成（首次发布 / 发布更新）：
  - 首次发布或内容相对**最近发布版本**有变化 → 生成新版本；
  - 已发布、或下架后原样重发（内容未变）→ 幂等，**不产生版本**；
  - 响应为文章 DTO（`status=published`）。
- `POST /knowledge/articles/:id/unpublish`（write）：只切可见性，不产生版本，重复调用幂等。
- `POST /knowledge/articles/:id/review`（write）：刷新 `lastReviewedAt=now`，并重算 `reviewDueAt=now+interval`（解除 L1 review_overdue）。**注**：创建时带 `reviewIntervalDays>0` 的文章发布后即为 overdue（`citable=false`）、不进检索，需补一次 review（见 §9 GAPS-9）。

## 3. 版本

| 端点 | 说明 |
|---|---|
| `GET /knowledge/articles/:id/versions` | **data 直接是版本数组**（无分页包络）；字段：id/version/title/content/contentType/category/tags/createdBy/createdByName/changeLog/createdAt |
| `GET /knowledge/articles/:id/versions/compare?from=&to=` | `from≠to`；返回 `{fromVersion,toVersion,diff,changes[{type:added\|removed,content}]}` |
| `POST /knowledge/articles/:id/versions/:version/restore` | 版本号在**路径**；恢复=写入历史内容并**回到草稿**（已发布会下架），需重新 publish 才生效；restore 前会尝试留一份「恢复前版本」快照（失败仅告警） |

## 4. 搜索 / 推荐 / 最近 / 统计

- `POST /knowledge/search`（read）：`{query(必填), category?, limit?}`，limit 默认 20、上限 50。
  - 装配 RAG 时走向量+关键词检索；未装配则关键词回退（title/content/tags，**只查 published**，再强制 L1 citable 过滤）。
  - 响应 `{items[], total}`；**item 为 snake_case**：`id/title/category/snippet/tags/is_published/relevance_score/created_at/updated_at`。
- `GET /knowledge/recommendations`、`GET /knowledge/recent`（read）：热门/最近文章列表。
- `GET /knowledge/stats`（read）：`{total, published, draft, views, rating, categories[{name,count}]}`。

## 5. 分类与可见性

- `GET /knowledge/categories`（read）：返回 **8 个默认分类 ∪ 文章表中已用分类**（去重、排序）。8 个默认值为：
  `故障排查 / 操作指南 / 最佳实践 / 变更发布 / 安全合规 / 常见问题 / 工单模板 / 流程说明`。
- **分类联合校验（2026-10-16 起，创建与更新同策略）**：允许集合 = 上述分类接口返回值 ∪ 静态白名单（12 值）：
  `accountManagement`、`troubleshooting`、`network`、`processGuide`、`systemConfig`、`账号管理`、`故障排除`、`网络连接`、`流程指南`、`系统配置`、`用户指南`、`技术文档`。
  其余值 → 400「无效的知识库分类: X」。即：**界面下拉值全部可创建**，自造分类被拒绝。
- `GET /knowledge/categories/restricted`（write）：`{items[], total}` 已纳管（仅授权角色可读）分类。
- `POST /knowledge/categories/restricted`（write）：`{category(必填), restricted(缺省 true)}` 纳管 / 解除。

## 6. 附件（文章域别名）

`/knowledge/articles/:id/attachments`（宿主权限码仍用 knowledge 家族，multipart 字段 `file`，≤10MB 白名单+内容嗅探）：

| 端点 | 权限 | 说明 |
|---|---|---|
| `GET` | read | `{attachments, total}` |
| `POST` | write | 上传 |
| `GET /:ref`、`/:ref/download`、`/:ref/preview` | read | 下载/内联预览（`ref` 接受数字 ID 或存储文件名） |
| `DELETE /:ref` | delete | 删除（被正文引用时可能被拒） |

## 7. 兼容路由

`/knowledge-articles`（legacy）：GET 列表、POST 创建、GET `/categories`、GET `/:id`，行为同上。

## 8. 错误速查

统一信封与错误码（2001/2002/2003/4004/1001…）。本域特有：分类不在白名单 → 400（message「无效的知识库分类: X」）；删除时有活跃协作会话 → 500 兜底文案（勿依赖细节）。

## 9. GAPS（调用方注意）

1. **评论端点是 stub**：`GET/POST /knowledge/articles/:id/comments` 恒返回假数据（未接库），勿依赖。
2. **点赞 / 协作会话未挂路由**：service 存在 LikeArticle/CreateSession/Heartbeat/ListParticipants，但路由层未暴露。
3. 响应 `author` / `views` 未回填（2026-10-16 实测确认恒为 `""`/`0`）。
4. `/knowledge/search` item 字段 snake_case 与全站 camelCase 规范不一致（历史契约）。
5. 版本列表无分页包络；其分页请求参数未生效。
6. 无批量删除 / 导出端点；文章只有 published/draft 两态（无归档态）。
7. （已修复 2026-10-16）分类两套集合不一致：原 create 链路**完全跳过校验**（12 值白名单是仅单测引用的死代码），`GET /categories` 下拉值与白名单互斥。现 create/update 统一为「白名单 ∪ 分类接口返回值」联合校验，非法值 400。
8. （已修复 2026-10-16）`PUT` 原不校验分类（实测可改任意值）：现与创建同策略，非法分类 400。
9. **创建即复核逾期（2026-10-17 实测）**：创建时设置 `reviewIntervalDays>0` 且未复核的文章，创建（乃至发布）后 `freshness.verdict` 立即为 `review_overdue`（`citable=false`），**不会进入 search 结果**；需调用一次 `POST /:id/review` 后才可检索（实测 review 后 `reviewDueAt=now+interval`，且该时间在后续 update+republish 中保留）。`reviewIntervalDays=0` 不受影响。调用方如需「发布即可检索」，应在 publish 后补 review。
10. （已修复 2026-10-17）**UGC 消毒把直引号实体化**：原实现不区分 `content_type`，一律走 HTML 净化，直引号 `"`/`'` 与 `&`/`<`/`>` 落库时被转成 `&#34;`/`&#39;`/`&amp;`/`&lt;`/`&gt;`；代码块内实体在渲染端不解码，示例代码字面显示为实体。现按类型分流：`html`/`rich_text` 仍走白名单净化，`markdown`/`text` 原样落库（XSS 由渲染端 `react-markdown + rehype-sanitize` 兜底）。**尚未重新部署的旧实例仍会实体化**，对旧实例发布 Markdown 时仍建议避开直引号并回读 `content` 核对。
