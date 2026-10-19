---
page_keys: []
audience: internal
version: 1.8
last_reviewed: 2026-10-18
owner: platform
---

# ITSM 使用帮助（帮助中心）

本目录是**产品帮助文档的单一真源**（docs-as-code），与 AI 助手的面板帮助/自助问答共用：

- 每个页面文档通过 `page_keys` 关联前端**页面注册表**（`itsm-frontend/src/lib/ai/page-context/registry.ts`）；
- `last_reviewed` 用于新鲜度判定（超过 90 天未复核 = `stale`）；
- 覆盖率由 `itsm-frontend/scripts/gen-page-manifest.mjs` 生成 `_ledger.json` 并在 CI 上以 ratchet 门禁守护（缺失只减不增）。

## 文档结构规范（frontmatter）

```yaml
---
page_keys: [ticket.detail]   # 必填：关联的页面键（可为空数组 = 通用文档）
audience: internal           # internal | customer | admin
version: 1.8                 # 适用产品版本
last_reviewed: 2026-10-18    # 最近复核日期（新鲜度判定）
owner: service-desk          # 兜底责任人
---
```

## 页面文档索引

| 页面 | 文档 |
|---|---|
| 工单列表 / 工单详情 / 创建工单 | [tickets/list.md](tickets/list.md)、[tickets/detail.md](tickets/detail.md)、[tickets/create.md](tickets/create.md) |
| 事件（列表/创建/详情） | [incidents/list.md](incidents/list.md)、[incidents/create.md](incidents/create.md)、[incidents/detail.md](incidents/detail.md) |
| 变更（列表/新建/详情） | [changes/list.md](changes/list.md)、[changes/detail.md](changes/detail.md) |
| 问题管理 | [problems/list.md](problems/list.md) |
| CMDB 配置项 | [configuration-items.md](configuration-items.md) |
| 知识库 | [knowledge/list.md](knowledge/list.md) |
| SLA 监控 | [sla/monitor.md](sla/monitor.md) |
| 服务目录 | [service-catalog.md](service-catalog.md) |
| 仪表盘 | [dashboard.md](dashboard.md) |
| AI 助手 / AI 观测看板 | [ai/chat.md](ai/chat.md)、[ai/observability.md](ai/observability.md) |

## 与 AI 助手的关系

页面级建议卡（任务卡/问题卡）来自页面注册表；帮助检索与"让 AI 生成初稿"以本目录文档为知识源。
发现文档缺失或过时：在对应页面打开 AI 助手，说"帮我更新这个页面的帮助文档"（后续批次接入起草工具链）。
