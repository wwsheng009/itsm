# 新技能交付与实测 —— itsm-knowledge-operations（2026-10-16 / 基线 d01b916e）

## 交付物

| 文件 | 内容 |
|---|---|
| `.agents/skills/itsm-knowledge-operations/SKILL.md` | 技能主文件：脚本入口、创建→发布→检索核心流程、硬规则（分类/发布/编辑即下架/stub） |
| `.agents/skills/itsm-knowledge-operations/references/knowledge-api.md` | 完整契约 9 节：认证权限/CRUD/发布复核/版本/搜索统计/分类/附件/错误/GAPS |
| `.agents/skills/itsm-knowledge-operations/scripts/itsm_kb.py` | 零依赖 Python CLI，22 子命令（含 multipart 附件上传与分类白名单前置校验） |
| `.agents/skills/itsm-knowledge-operations/agents/openai.yaml` | UI 元数据（display_name/short_description/default_prompt） |

## 实测（238 demo，`demo.mfg.admin`；全部经 `scripts/itsm_kb.py`）

| 步骤 | 结果 |
|---|---|
| login / me | role=admin，tenant=3 |
| categories | n=11 = 8 个默认分类 ∪ 已用分类（去重合并） |
| stats | total=5，published=5，draft=0 |
| create 白名单外分类 | 客户端前置拒绝 + 提示 10 值白名单 |
| create（troubleshooting，markdown） | id=17，status=draft；`author=''`、`views=0`（确认字段未回填缺口） |
| get | freshness=ok，citable=true |
| publish --change-log 首版 | status=published；versions=1（v1，changeLog=首版） |
| update（改正文） | **status=draft**——编辑即下架（更新=一次编辑） |
| publish（缺省 changeLog） | published；versions=2（changeLog=发布更新） |
| versions-compare 1↔2 | changes=2，diff 命中新增文本 |
| search "卡纸" | total=1，score=0.9（RAG 路径生效），返回 snake_case 字段 |
| unpublish → publish（内容未变） | 幂等：status=published 且 versions 仍=2 |
| review | lastReviewedAt 刷新成功 |
| attachment-upload / list | 上传 id=9；列表 total=1 |
| restore v1 | status=draft、正文回退到 v1；重发后 versions=3 |
| `demo.mfg.user` create | HTTP 403 / code 2003（end_user 仅 knowledge:read） |
| delete | ok；随后 GET → 404（软删） |
| PUT 白名单外分类（raw curl，另建 id=18） | **成功改为「最佳实践」**——更新路径不校验分类（测试后已清理） |

## 与代码对照发现的缺口（已写入 references §9 GAPS）

1. 评论端点 `GET/POST /:id/comments` 是 stub（恒返回假数据）。
2. 点赞 / 协作会话 service 存在但未挂路由。
3. 响应 `author`/`views` 恒为 `''`/`0`（实测确认）。
4. `POST /knowledge/search` item 为 snake_case（与全站 camelCase 不一致）。
5. **两套分类不一致**：`GET /knowledge/categories` = 8 默认 ∪ 已用（`最佳实践/常见问题` 等）；创建白名单却是另一套 10 值（5 EN + 5 CN）——界面下拉值拿去创建会 400。
6. `PUT` 更新不校验分类白名单，且**更新即下架**（已发布文章会回草稿）。
7. 版本列表无分页包络（分页请求参数未生效）。

## 校验

- `python -m py_compile` 通过；`skill-creator/quick_validate.py` → `Skill is valid!`
- CLI 全局参数（`--base/--token/--token-file`）前/后/混合顺序均可（沿用 Python 3.13 argparse SUPPRESS 修复经验）。
- 演示数据：实测文章 id=17/18 已软删；未新增持久演示文章。---

## 附录 4：分类策略修复与二次发布（2026-10-16，tag `1.8.0-dev.8af8782b`）

### 根因

- handler 链路 `CreateArticle→repo.Create` **完全没有分类校验**（直连 ent）；`service.KnowledgeService` 的 12 值白名单只被单测引用（生产未接线）——实测「随便分类X」可直接创建；
- 同时 `GET /knowledge/categories`（8 默认 ∪ 已用）与白名单是两套集合 → 界面下拉值与创建校验互斥（本文件附录 §GAP-7/8 的实测背景）。

### 修复（commit 8af8782b）

- `handlers/knowledge/service.go` 新增 `ValidateCategory`：允许集合 = 静态白名单（12 值：5 EN + 账号管理/故障排除/网络连接/流程指南/系统配置/用户指南/技术文档）∪ `GET /knowledge/categories` 返回值（8 默认 ∪ 租户已用，按租户隔离）；
- 创建（CreateArticle）与更新（UpdateArticle）handler 均接入，非法分类 → **400/1001**「无效的知识库分类」；
- `service/knowledge_service.go` 导出 `IsWhitelistedKnowledgeCategory`，白名单保持单一来源；
- 回归测试 `TestValidateCategory_UnionPolicy`（默认/白名单/已用可创建；自造值、空值、跨租户拒绝）；
- skill 同步：CLI 移除本地白名单拦截（避免与服务端漂移），文档改述联合校验策略（12 值）。

### 发布与线上复测（238 demo）

| 步骤 | 结果 |
|---|---|
| bundle `d01b916e..8af8782b` + B 构建 | BUILD_EXIT=0；镜像 sha256:2c85e8697e69… |
| C 上线（仅 backend/worker，未 down -v） | `itsm-backend:1.8.0-dev.8af8782b` healthy；health=200 / root=200 |
| create 默认值「最佳实践」 | 200（修复前界面下拉值必 400） |
| create 白名单「用户指南」 | 200 |
| create 租户已用「服务指南」 | 200 |
| create 自造「随便分类X」 | **400/1001**「无效的知识库分类…」 |
| update → 「随便分类X」 | **400/1001** |
| update → 「服务指南」 | 200（status=draft，编辑即下架语义不变） |
| 清理 3 篇测试文章 | 全部删除，检索剩余 0 |

备注：同窗口远端历史含第三方提交 `3c8e2f90`（仅 ticket skill 与 .gitignore，不涉后端），随 bundle 同步；对本次构建无影响。