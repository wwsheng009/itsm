# P2 · L1 自动合并策略（§5.7） · 交付证据

- 日期：2026-10-18
- 任务：P2「智能化与运营」第 2 项：**L1 自动合并策略**（§5.7「校验全绿可自动合并/自动发布（策略开关，默认关，先观察）」）
- 状态：`integration_verified`（核心纯函数 + 执行器 + 门禁/回滚/审计 + 夜批 dry-run + Agent 规则；自动发布与错别字修正归后续）
- 交付物：
  - 核心纯函数：`itsm-frontend/scripts/lib/doc-merge-core.mjs`
    （L1 分类 `classifyDocFix`、候选选择 `selectAutoMergeCandidates`、frontmatter 补全 `addFrontmatterVersion`、
    EOF 换行 `normalizeEofNewline`、相对链接唯一命中修正 `rewriteRelativeLinks`、审计构造/序列化/摘要）
  - 执行器：`itsm-frontend/scripts/apply-doc-fixes.mjs`（默认 dry-run；`--apply` + `DOCS_AUTO_MERGE=1` 才写；
    校验全绿前置门禁；应用后复校验，失败**整体回滚**；`--max-files` 上限；审计 JSONL；`--report` 机器可读报告）
  - npm 脚本：`docs:merge`（dry-run + report）、`docs:merge:apply`、`docs:merge:test`
  - 夜批：`.github/workflows/docs-stale-nightly.yml` 新增 L1 dry-run 步骤（默认关，仅观察）+ report artifact +
    Issue 头提示；测试步骤同时跑 `docs:merge:test`
  - Agent 规则：`itsm-agent/prompts/docs-maintenance.md` 新增「L1 自动合并策略」小节（禁止 PR 夹带机械批量重排）

## 1. 口径（实现即契约）

| 项 | 口径 |
|---|---|
| L1 白名单 | `frontmatter_version_add`（推导值=前端包 `major.minor`，代理口径）/ `link_normalize`（**仅 basename 在 docs/help 内唯一命中**时改写，保留锚点/标题/图片前缀）/ `eof_newline`（**仅补文件结尾换行**） |
| 明确不做 | 错别字修正（无词典/无事实源）；**逐行去尾空白**（Markdown 行尾两空格 = 硬换行，会改渲染语义——本地 E2E 发现后收窄为 EOF 换行，并加负向测试）|
| 策略开关 | 默认 **关**：`--apply` 还需 `DOCS_AUTO_MERGE=1`，否则自动降级 dry-run（exit 0） |
| 校验门禁 | 应用前 `docs:validate` 必须 exit 0（否则 fail-closed：不写任何文件、exit 1、不落审计）；应用后复跑校验，失败 → 回滚全部变更 + exit 1 |
| 风险上限 | 单批文件数默认 20（`--max-files`）；候选按文件稳定排序 |
| 审计 | 实际写入追加 `docs/help/_automerge-audit.jsonl`（时间/触发者（CI actor#run）/模式/校验前后/明细/回滚/备注） |
| L2/L3/未知 | 一律跳过并记 `skipped`（`level_l2|l3_requires_human`；内容修订走一键审阅、首篇必须人工过目） |

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npm run docs:merge:test      # node:test：7 pass（分类/选择/补全/EOF/链接/审计/摘要）
npm run docs:stale:test      # 回归：7 pass
npm run docs:validate        # OK：17 篇文档，0 错误
npm run docs:ledger:check    # OK（ratchet 未回退 + 内嵌助手注册表 in sync）
npm run docs:merge           # dry-run：L1 候选 0（文件 0），校验=green（干净树）
```

**真实端到端（本地实测，含门禁/应用/回滚路径）**：
1. **门禁 fail-closed**：放入临时脏文档（`page_keys: ticket.list` 但缺 `version`）→ `npm run docs:validate` FAIL(1)
   → `DOCS_AUTO_MERGE=1 node scripts/apply-doc-fixes.mjs --apply` 输出
   「校验未通过，按 §5.7 不执行自动合并（未写入任何文件）」+ `exit 1` + **无审计文件** + 文档未被改写（`version:` 仍缺失）；
2. **应用路径**：补齐 `version` 后校验 OK(18 篇) → 同上命令 → 应用候选、应用后复校验 green、审计 JSONL 落盘
   （`mode=apply, validationBefore=green, validationAfter=green`）；
3. **规则收窄（E2E 发现）**：首版把「逐行去尾空白」列为 L1，E2E 过程中对 18 篇种子文档执行了该清理；
   复核认为 Markdown 行尾两空格是硬换行语法 → 规则收窄为**仅 EOF 换行**并新增负向测试
   （`标题  \n` 逐字节保留）；复核现状：`docs/help/**` 行尾硬换行用法 0 处（`rg '  $'` 计数 0）、
   文档校验/账本全绿，未影响可见内容与渲染；临时文档/审计/报告已清理（审计为本地测试产物，非提交物）；
4. YAML 语法校验：`python -c "import yaml; yaml.safe_load(...)"` → `workflow yaml ok`。

## 3. 条件项（不阻塞）

- **自动发布**（mkdocs 站点）不在批内：夜批只跑 dry-run 观察，`--apply` 由维护者/后续 CI 在开关下执行；
- **错别字/语法修正**需词典或 LLM 起草 → 走 L2 一键审阅，不进入 L1 白名单（避免"看似机械、实则语义"的改动）；
- `frontmatter_version_add` 的推导值为代理口径（前端包 major.minor）；如需精确文档版本号，后续接产品版本源；
- L1 容量/频率阈值（`maxFiles`、每日上限）可依观察结果调整，策略仍保持默认关。