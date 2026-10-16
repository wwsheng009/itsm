---
name: itsm-knowledge-operations
description: 通过 ITSM REST API 操作知识库文章（创建/更新/发布/下架/复核/版本对比与恢复/搜索/分类可见性/统计）。Use when the agent needs to 创建知识库文章、发布或下架文章、检索知识、管理受限分类、查看或恢复历史版本、读取知识库统计；covers /api/v1/knowledge/articles 及 publish/unpublish/review/versions/search/categories/stats 端点，含权限码（knowledge:read/write/delete）、固定分类白名单、发布=版本唯一入口、L0/L1 可见性规则与评论 stub 缺口。Do NOT use for 工单操作（itsm-ticket-operations）或部署（itsm-deploy）。
---

# ITSM 知识库操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证与工单域一致：PAT（`Authorization: Bearer itsm_pat_…`）或登录换 JWT（token 仅经 httpOnly cookie 下发）；租户绑定令牌无需 `X-Tenant-Code`。

完整端点表、字段契约、错误码与 GAPS：先读 [references/knowledge-api.md](./references/knowledge-api.md)。

## 脚本工具（scripts/itsm_kb.py）

零依赖 Python 3 CLI（与工单域同款封装：cookie 取 JWT、Bearer、multipart、错误 JSON 化）：

```bash
B=http://172.18.3.238:8089/api/v1
python scripts/itsm_kb.py login --base $B --username demo.mfg.admin --password 'Demo@2026!' --save ./itsm.token
python scripts/itsm_kb.py --base $B --token-file ./itsm.token categories
python scripts/itsm_kb.py --base $B --token-file ./itsm.token create --title '打印机卡纸处理' --content '...' --category troubleshooting --tags 打印,硬件
python scripts/itsm_kb.py --base $B --token-file ./itsm.token publish 12 --change-log '首版'
python scripts/itsm_kb.py --base $B --token-file ./itsm.token search --query 卡纸
```

`--help` 列出 22 个子命令（login/me/categories/list/get/create/update/publish/unpublish/review/versions/versions-compare/restore/search/stats/recent/recommendations/restrict-list/restrict-set/delete/attachment-list/attachment-upload）。

## 核心流程：创建并发布一篇可检索的文章

1. **分类联合校验（服务端强制，创建与更新同策略，2026-10-16 起）**：允许集合 = 白名单（12 值：`accountManagement/troubleshooting/network/processGuide/systemConfig` + `账号管理/故障排除/网络连接/流程指南/系统配置/用户指南/技术文档`）∪ `GET /knowledge/categories` 返回值（8 个默认分类 ∪ 租户已用分类）。其余值 400「无效的知识库分类」；界面下拉值因此全部可创建。
2. **创建**：`POST /knowledge/articles`（`knowledge:write`）——`title/content/category` 必填；可选 `tags`、`contentType`（text/markdown/html/rich_text）、时效（validFrom/validUntil/reviewIntervalDays 0–3650）、权威性 `authorityLevel`（0/10/20/30）。
3. **发布**：`POST /knowledge/articles/:id/publish`（body 可空 `{changeLog≤200}`）——**发布是版本产生的唯一入口**；内容与最近发布版本一致时幂等，不产生新版本。
4. **检索**：`POST /knowledge/search {query, category?, limit≤50}`——默认只返回已发布且时效合格（L1 citable）；受限分类按访问者身份过滤（L0，fail-closed）。
5. **核验**：`GET /knowledge/articles/:id`，或 `GET /knowledge/articles?status=published&search=…`。

## 决策要点（硬规则）

- **发布态不是编辑字段**：PUT 更新忽略 `status`；变更发布态只能走 publish / unpublish。
- **PUT 更新=一次编辑**：写回后文章一律回到草稿（已发布会被下架），需重新 publish 才生效；更新路径与创建同做分类联合校验。
- `unpublish` 幂等且不产生版本；`restore` 是"编辑"：内容回滚后文章**回到草稿（已发布会被下架）**，需重新 publish 才对外生效。
- `POST /:id/review` 刷新 lastReviewedAt（解除"逾期未复核"过滤）；PUT 不允许写该字段。
- 评论端点 `GET/POST /:id/comments` 当前是 **stub**（恒返回假数据），勿依赖；点赞/协作会话（sessions/participants）服务存在但**未挂路由**。
- 权限：读 `knowledge:read`（end_user 默认有）；写 `knowledge:write`（agent/工程师/管理员类角色）；删除 `knowledge:delete`（管理角色）。
- 文章附件走域内别名 `/knowledge/articles/:id/attachments`（权限码仍用 knowledge 家族）；multipart 字段名 `file`。

## 排障与验证

排障按序查 [references/knowledge-api.md](./references/knowledge-api.md) §8/§9。验证脚本/工具：对 demo 环境跑 创建→发布→搜索→版本→下架→重发 链路并保留请求/响应证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。