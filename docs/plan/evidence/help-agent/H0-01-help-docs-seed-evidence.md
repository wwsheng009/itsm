# H0-01 帮助文档骨架与种子文档 · 交付证据

- 日期：2026-10-18
- 任务：H0-01（方案 §5.1 内容架构 / §7 P0）
- 状态：`unit_verified`（内容运营流程与 Help Agent 工具归 H0-03/H0-04）
- 交付物：`docs/help/**`（17 篇：索引 + 16 篇页面文档）+ frontmatter 规范

## 1. 结构与规范

- **单一真源**：产品帮助文档位于 `docs/help/**`（docs-as-code，随版本发布，mkdocs 站点同源）；
- **frontmatter 规范**（`docs/help/index.md` 内说明并示例）：
  `page_keys`（关联页面注册表键，必填）、`audience`、`version`、`last_reviewed`（新鲜度）、`owner`；
- **页面文档清单**（覆盖全部 **21 个注册表 pageKey**）：

| 文件 | page_keys |
|---|---|
| `index.md` | `[]`（通用：规范 + 索引） |
| `tickets/list.md` | `ticket.list` |
| `tickets/create.md` | `ticket.create`, `ticket.aiCreate` |
| `tickets/detail.md` | `ticket.detail` |
| `incidents/list.md` | `incident.list` |
| `incidents/create.md` | `incident.create` |
| `incidents/detail.md` | `incident.detail` |
| `changes/list.md` | `change.list` |
| `changes/detail.md` | `change.create`, `change.detail` |
| `problems/list.md` | `problem.list` |
| `configuration-items.md` | `ci.list`, `ci.create`, `ci.detail` |
| `knowledge/list.md` | `knowledge.list`, `knowledge.new` |
| `sla/monitor.md` | `sla.monitor` |
| `service-catalog.md` | `service.catalog` |
| `dashboard.md` | `dashboard` |
| `ai/chat.md` | `ai.chat` |
| `ai/observability.md` | `ai.observability` |

## 2. 构建与门禁验证（可复跑）

```powershell
# 依赖（本机首次）：按仓库既有清单安装 MkDocs
python -m pip install -r requirements-docs.txt
# 站点构建
python -m mkdocs build --site-dir .tmp/mkdocs-site          # exit 0；help/ 零警告；17 个页面产物
# 文档断链门禁（仓库既有 C.3）
bash scripts/docs-gate/check-broken-links.sh                # docs/help 断链 0（全仓 88 条为存量 advisory）
```

**踩坑与修复**：
- 初版 `index.md` 链接 `changes/list.md` 不存在 → 补齐该文档；
- `docs/help/cmdb/cis.md` 被 mkdocs `exclude_docs` 的 `cmdb/` 规则排除（全局目录名匹配）→
  迁移为 `docs/help/configuration-items.md`（文件头注明原因），构建恢复正常。

## 3. 边界与遗留

- 内容为种子级（≤40 行/页，含"AI 助手可用动作"小节与注册表建议卡对齐）；
- 截图/富媒体、FAQ 深化、租户自定义帮助（KB 承载）归后续批次；
- `last_reviewed` 为人工字段；git 元数据增强的新鲜度判定归后续（当前由 H0-02 ledger 按 90 天规则判定）。