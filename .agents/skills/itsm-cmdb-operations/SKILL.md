---
name: itsm-cmdb-operations
description: 通过 ITSM REST API 查询 CMDB 配置项（CI 列表/搜索/统计/历史/拓扑/影响分析、CI 类型与属性定义、关系与关系类型、保存视图、本体/能力自描述、发现健康、对账）。Use when the agent needs 查询配置项/资产、分析 CI 关系与影响面、查 CI 类型结构、环境检查前盘点资源；covers /api/v1/cmdb/*（cis/ci-types/attributes/relationships/views/ontology/discovery/reconciliation），含 cmdb:read/write/delete 权限族。Do NOT use for 工单操作（itsm-ticket-operations）、知识库（itsm-knowledge-operations）、部署（itsm-deploy）；写面（CI/关系 CRUD、批量、导入导出、云资源、发现任务）暂未封装，见 SKILL.md 待办。
---

# ITSM CMDB 操作（API 调用手册）

面向**外部调用方**（自动化脚本 / CI / Agent）。认证与工单/知识库域一致：PAT 或登录 JWT，`Authorization: Bearer`；租户绑定令牌无需 `X-Tenant-Code`。权限码族：`cmdb:read`（全部读面）/ `cmdb:write` / `cmdb:delete`。

## 脚本工具（scripts/itsm_cmdb.py）

零依赖 Python 3 CLI，公共层复用 `../_lib/itsm_common.py`（认证/输出/错误/token 落盘，与工单、知识库同款卫生：`--save` 默认 `~/.itsm/itsm.token`、掩码回显）。**56 个子命令：读面全量 + 写面核心 + 导入导出/云三件套/发现**：

```bash
B=http://172.18.3.238:8089/api/v1
TOKEN="$HOME/.itsm/itsm.token"
python scripts/itsm_cmdb.py login --base $B --username demo.mfg.admin --password 'Demo@2026!' --save "$TOKEN"
python scripts/itsm_cmdb.py --base $B --token-file "$TOKEN" cis-stats                     # 全局统计（状态/类型/环境/关键度分组）
python scripts/itsm_cmdb.py --base $B --token-file "$TOKEN" cis-list --environment production --criticality P1
python scripts/itsm_cmdb.py --base $B --token-file "$TOKEN" cis-get 12
python scripts/itsm_cmdb.py --base $B --token-file "$TOKEN" cis-impact 12                 # 变更/故障影响面分析
python scripts/itsm_cmdb.py --base $B --token-file "$TOKEN" ontology                      # 类型×属性×关系词表自描述
```

子命令清单：login / cis-list / cis-get / cis-search / cis-stats / cis-history / cis-topology / cis-impact / cis-relationships / types-list / type-get / type-attributes / ontology / capabilities / rel-types / rel-list / rel-get / views-list / discovery-health / reconciliation ＋ 写面 ci-create / ci-update / ci-delete / batch-create / batch-update / batch-delete / lifecycle-update / lifecycle-history / batch-lifecycle / revert / rel-create / rel-update / rel-delete ＋ 长尾 import-create / import-status / import-list / export-create / export-status / export-list / cloud-accounts / cloud-account-get / cloud-account-create / cloud-account-update / cloud-account-delete / cloud-services / cloud-service-get / cloud-resources / cloud-resource-get / cloud-resource-create / cloud-resource-update / cloud-resource-delete / discovery-sources / discovery-source-create / discovery-job-create / discovery-results。

## 核心查询速查（curl 等价）

```bash
PAT="Authorization: Bearer itsm_pat_xxx"
curl -H "$PAT" "$B/cmdb/cis?page=1&size=20&status=production&environment=prod&search=mysql"   # 宽语义模糊搜索
curl -H "$PAT" -X POST "$B/cmdb/cis/search" -H 'Content-Type: application/json' -d '{"filters":{"ciType":"server"},"page":1,"pageSize":20}'
curl -H "$PAT" "$B/cmdb/cis/12/topology";  curl -H "$PAT" "$B/cmdb/cis/12/impact-analysis"
curl -H "$PAT" "$B/cmdb/ci-types";  curl -H "$PAT" "$B/cmdb/ci-types/5/attributes"
```

## 决策要点（硬规则）

- **列表过滤为 form 绑定**（`GET /cmdb/cis`）：`page/size(≤200)/ciTypeId/ciType/status/environment/criticality/search/ciNumber/sortBy/sortOrder/withRelations`；`search` 是宽语义模糊（名称/序列号/型号/厂商/云资源ID/位置等）；`ciNumber` 是 AI Agent 定位实体的**稳定自然键**（精确匹配）。
- **高级搜索走 POST body**（`POST /cmdb/cis/search`）：`{filters, page, pageSize≤100, sortBy, sortOrder}`；与列表过滤字段同源。
- **路由面两套前缀**：规范前缀 `/cmdb/*`；`/configuration-items/*` 是**弃用别名**（勿新增端点，脚本只用规范前缀）。
- **统计路由在 `/:id` 之前注册**（`GET /cmdb/cis/stats`）；`/cmdb/cis/relationships` 是兼容旧路径（规范在 `/cmdb/relationships`）。
- **CI 附件**走域内别名 `/cmdb/cis/:id/attachments`（宿主权限码 cmdb 族，multipart `file`，≤10MB）。
- 写面契约（CI CRUD/批量/生命周期/revert、导入导出任务、云账号/服务/资源、发现 sources/jobs/results）暂未封装脚本；需要时直接 curl 并回填本文件。

## 待办（下一步）

- [x] 写面封装（2026-10-17）：`ci-create/ci-update/ci-delete`、`batch-create/batch-update/batch-delete`、`lifecycle-update/lifecycle-history/batch-lifecycle`、`revert`、`rel-create/rel-update/rel-delete`——238 实测 create→update(乐观锁)→lifecycle→rel 三步链 + 自清理全通过
- [x] 长尾封装（2026-10-17）：`import-*/export-*`（任务制轮询）、`cloud-accounts/services/resources` CRUD、`discovery-sources/jobs/results`——238 实测 export 任务创建+轮询、云面/发现面列表（云发现能力未配置 → 返回 null，见 references GAPS-6）
- [ ] GAPS 持续登记：见 references §4（search 未知键静默忽略、lifecycle 响应不回显状态等）

## 排障与验证

排障按序查本目录 `references/cmdb-api.md`（P0 调研稿，随写面封装补全）。验证口径：对 demo 环境跑 cis-stats → cis-list（过滤）→ cis-get → cis-impact 链路并保留证据（仿 `.dev/deploy/DEMO-*EVIDENCE.md` 模式）。
