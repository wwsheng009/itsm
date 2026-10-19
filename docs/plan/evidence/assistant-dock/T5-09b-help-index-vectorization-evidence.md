# T5-09b help-index 语义向量化证据

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-09b**（T5-09 拆分；写面/审阅见 T5-09a）
- **范围**：`docs/help` 内嵌索引的向量化、按租户懒构建、语义检索与词法回退
- **不含**：构建期离线向量文件、跨租户共享向量、切换默认嵌入模型
- **提交**：随本文件同一提交（`feat(ai): T5-09b help-index 语义向量化`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| help-index 可向量化 | ✅ 全部内嵌帮助文档写入 connector + legacy 向量库，`object_type=help_doc` | `service/rag_help_index.go`；`TestHelpDocsSemanticIndexAndSearch` |
| 语义检索 | ✅ query embedding → TopK → 按 `source` 路径回查内嵌文档 | 同上 |
| 词法回退 | ✅ embedding/vector 未配置、索引失败或检索失败时仍返回词法结果 | `TestHelpSearch_SemanticMerge`；handler 降级路径 |
| 混合排序 | ✅ 保留词法排序，按路径去重后用语义补齐，limit 20 上限 | `TestMergeHelpSearchResults_DedupAndLimit` |
| pageKey 过滤 | ✅ 语义命中在映射回文档后按 pageKey 过滤 | `TestHelpDocsSemanticIndexAndSearch` |
| 租户隔离 | ✅ 向量按 tenantID 写入/查询；connector filter + legacy 查询双收敛 | `EnsureHelpDocsIndexed` / `SearchHelpDocs` |
| 重复索引控制 | ✅ 进程内按租户成功后不再重复；失败不标记，下一请求重试 | 单测 |
| 工具 + REST 双入口 | ✅ `help_search` 与 `GET /ai/help/search` 均合并语义结果 | `tool_registry_help.go`；`handlers/ai/help.go` |
| 零配置兼容 | ✅ 未配置向量/embedding 时行为与 H0-03 完全一致 | 既有 help 测试全绿 |

---

## 2. 实现

### 2.1 索引口径

| 项 | 口径 |
|---|---|
| 数据源 | `helpindex.Load().Docs()`（`docs/help/**` 构建期内嵌副本） |
| 对象类型 | `help_doc` |
| object_id | 文档路径 FNV-1a 31-bit（正数；路径仍存 `source`） |
| connector ID | `tenant:{tenantID}:help_doc:{objectID}` |
| content | 文档正文（Markdown 明文，frontmatter 已剥离） |
| metadata/source | `path` 相对路径、`title`、`tenantID`、`objectType` |
| 写入 | connector `Insert` + legacy `Upsert` 双写 |
| 触发 | 首次 `help_search` / `/ai/help/search` 时按租户懒索引 |
| 失败 | 不标记成功；调用方回退词法，不阻塞帮助检索 |

### 2.2 检索口径

1. 先执行既有词法检索（保持 exact keyword / CJK 2-gram 行为）；
2. 若向量与 embedding 可用：
   - 对 query 生成 embedding；
   - connector store 过滤 `tenantID + objectType=help_doc`；
   - legacy store 使用 `SearchTopKByTypeResults(..., "help_doc", ...)`；
   - 用 `source` 路径回查 `helpindex.ReadDoc`，未知路径丢弃；
   - 按 pageKey 过滤；
3. 合并：词法在前、语义补齐、路径去重、limit 钳制；
4. 语义失败只记录降级，不改变词法结果。

### 2.3 代码清单

| 文件 | 改动 |
|---|---|
| `internal/helpindex/index.go` | 新增 `Docs()`；检索结果增加 `source: lexical` |
| `service/rag_help_index.go` | 新增帮助文档索引/语义检索/结果合并 |
| `service/rag_service.go` | 新增按租户索引状态与互斥锁 |
| `service/tool_registry.go` | 注入 `HelpSemanticSearcher` |
| `service/tool_registry_help.go` | `help_search` 合并语义结果 |
| `handlers/ai/help.go` | `/ai/help/search` 合并语义结果 |
| `internal/bootstrap/app.go` | 将 RAGService 注入 ToolRegistry |

---

## 3. 验证

### 3.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `gofmt -w <改动 Go 文件>` | ✅ 无格式差异 |
| `go test -count=1 -p 1 ./service` | ✅ `99.321s` |
| `go test -count=1 -p 1 ./handlers/ai` | ✅ `12.932s` |
| `go test -count=1 -p 1 ./internal/helpindex` | ✅ `0.589s` |
| `go test -count=1 -p 1 ./router` | ✅ `6.898s` |
| `go test -count=1 -p 1 ./middleware -run 'TestPrecheckMapIsFresh\|TestRoutePrecheckAlignment'` | ✅ `1.865s` |
| `go test -count=1 -p 1 ./internal/bootstrap` | ✅ `1.248s` |

关键断言：

- `TestHelpDocsSemanticIndexAndSearch`：首次索引写入全部文档；第二调用不重复写；语义结果标记 `semantic`；pageKey 过滤只返回该页文档；
- `TestMergeHelpSearchResults_DedupAndLimit`：同路径以词法排序优先，语义补齐，limit 生效；
- `TestHelpSearch_SemanticMerge`：工具入口合并语义；语义失败后词法仍可用；
- `TestHelpDocsSemantic_UnavailableWhenEmbedderMissing`：embedding 缺失时返回稳定不可用错误，不伪装成功；
- 既有 help 检索/页面帮助/审阅队列测试全绿。

### 3.2 前端

| 命令 | 结果 |
|---|---|
| `npm run type-check` | ✅ exit 0 |
| `npx eslint src/lib/api/ai-api.ts` | ✅ exit 0 |

`HelpSearchResult` 增加可选 `source: lexical | semantic`，旧后端缺省不影响渲染；既有帮助面板/API 套件不受影响。

---

## 4. 局限与后续

1. **运行时懒索引**：首次帮助检索会触发 21 篇文档的 embedding；成功后的进程生命周期内不重复。生产可按租户预热（后台任务）以避免首查延迟。
2. **按租户重复存储**：产品帮助文档内容相同，但现有向量表/检索均按 tenant 收敛，因此每租户各存一份；后续可评估全局帮助命名空间。
3. **对象 ID 为路径哈希**：极端哈希碰撞可能覆盖；当前文档规模风险可忽略，路径仍保存在 `source` 可诊断。
4. **未接构建期向量文件**：避免 CI 持有 embedding 凭据；构建期只产出文本索引。
5. **语义结果不提供分数**：混合合并以词法优先 + 语义补齐，未做调参；后续可用点击/解决率调权。
6. **未跑真实 pgvector/connector E2E**：本批使用 fake connector 单测；真实后端沿用既有 RAG smoke/条件项验证。

---

## 5. 变更文件

**新增**

- `itsm-backend/service/rag_help_index.go`
- `itsm-backend/service/rag_help_index_test.go`
- `docs/plan/evidence/assistant-dock/T5-09b-help-index-vectorization-evidence.md`

**修改**

- `itsm-backend/internal/helpindex/index.go`
- `itsm-backend/service/rag_service.go`
- `itsm-backend/service/tool_registry.go`
- `itsm-backend/service/tool_registry_help.go`
- `itsm-backend/service/tool_registry_help_test.go`
- `itsm-backend/handlers/ai/help.go`
- `itsm-backend/internal/bootstrap/app.go`
- `itsm-frontend/src/lib/api/ai-api.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
