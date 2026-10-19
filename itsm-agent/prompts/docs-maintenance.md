# Docs Maintenance Prompt Template（H1-02/H1-03 PR 通道）

> 供 `itsm-agent`（watcher → orchestrator → generator → validator → pr）在**文档维护任务**中使用。
> 触发源：`/ai/help/todos` 待办项（reason: doc_missing / doc_stale / page_unregistered / user_help_gaps）、
> ledger 缺口（H0-02 `_ledger.json`）或 **H1-03 夜批 stale 提议**
>（`docs/help/_stale-proposals.json` / 「[docs] 夜间文档更新提议」Issue；action: create_doc / review_update / remove_reference）。

## Task Input

- **Trigger:** {{trigger}} <!-- ledger_gap | help_gap_cluster | code_change | manual -->
- **Page Key:** {{page_key}}
- **Reason:** {{reason}}
- **Context JSON:** {{context_json}} <!-- 待办项元数据（pageKey/docStatus/gapCount/lastSeen）；禁止包含用户问题原文 -->
- **Stale Proposal（H1-03，可空）:** {{stale_proposal_json}} <!-- {pageKey, action, docs[], reasons[]}；原因如 manifest_changed:pattern,suggestions -->
- **Owner（可空）:** {{owner}}

## 目标

按 docs-as-code 单一真源维护 `docs/help/**.md`：补齐缺失文档或修订过时文档。
分级（方案 §5.7）：L1 机械修正（链接/错别字/frontmatter 补全）、L2 内容更新（字段/流程变化后的修订）、
L3 新增页面文档（首次必须人工过目 + 指定 owner）。

## Grounding（必须遵守，禁止编造）

1. **页面事实**：`itsm-frontend/src/lib/ai/page-context/registry.ts`、`src/routes/route-paths.ts`、
   `itsm-frontend/src/generated/page-manifest.json`（pattern/entrypoint/scene/文档覆盖状态）；
2. **结构参考**：`docs/help/index.md` 与同域既有文档；frontmatter 规范：
   `page_keys`（必填，必须与注册表一致）、`audience`（internal|customer|admin）、`version`、`last_reviewed`、`owner`；
3. **业务事实**：既有 `docs/help/**`、KB 相关文档（如可用）、工具目录（`GET /agent/tools/catalog`）；
4. 不确定的路由/字段/权限码**不得编造**：写「待补充」并标注需要确认的 owner。

## 交付物

- 变更文件：`docs/help/**.md`（新增或修订；一个 PR 聚焦一个页面域，可含同域多篇）；
- 修订时：内容有实质变化才更新 `last_reviewed`（纯格式调整不更新）；
- 正文结构建议：适用场景 / 关键概念 / 常用操作 / 常见问题 / AI 助手可用动作。

## 必须通过的校验（缺一不可）

```bash
cd itsm-frontend
npm run docs:validate        # H1-02 校验器（frontmatter/结构/page_keys/链接/敏感信息）
npm run docs:ledger:check    # H0-02 ratchet（missing/stale/orphan 不回退 + 内嵌助手注册表漂移检查）
```

校验失败必须修复后重跑；**禁止**用 `--write-baseline` 放宽门禁（确需放宽 → 先开评审 issue，由维护者更新）。

## L1 自动合并策略（P2，§5.7；默认关，先观察）

- 机械修正执行器：`itsm-frontend/scripts/apply-doc-fixes.mjs`（核心纯函数 `scripts/lib/doc-merge-core.mjs`）。
  - **默认 dry-run**：`npm run docs:merge`（列出 L1 候选并产出 `docs/help/_automerge-report.json`，不写文件）；
  - **自动合并**：`DOCS_AUTO_MERGE=1 npm run docs:merge:apply`——需**校验全绿**才应用，应用后复跑校验，
    失败则整体回滚；每文件数上限 20；
  - **L1 白名单**：frontmatter 可推导补全（`version`，代理口径为前端包 major.minor）、相对链接唯一命中修正、
    **仅 EOF 换行补齐**（逐行去尾空白会破坏 Markdown 硬换行语义，已明确禁用）；
  - **L2/L3 与未知类型一律跳过**（内容修订走一键审阅、首篇必须人工过目）；
  - **审计**：实际写入追加 `docs/help/_automerge-audit.jsonl`（时间/触发者/模式/校验前后/明细/回滚），
    夜批（`docs-stale-nightly.yml`）只跑 dry-run 观察候选，候选随 artifact 上传。
- Agent 侧规则：`pr` 通道仍按 L2/L3 人工分级执行；**不要**在 PR 中夹带 L1 机械批量重排，
  机械修正统一由执行器（或维护者本地命令）完成并留审计，避免 PR 噪音与不可追溯的批量改动。

## PR 规则

- 分支：`docs/{{page_key}}-{{date}}`；标题：`[docs] <页面标题>：<动作>`（新增/修订/补链）；
- 描述必须包含：触发源、页面键、校验命令与结果、变更摘要（L2/L3 附依据出处）、仍待人工确认的点；
- 不修改 `docs/help/_coverage-baseline.json`；不触碰生成物（`src/generated/**`、`internal/helpindex|assistantcontext/*.json`——
  它们由 `npm run docs:manifest` 生成，若页面注册表变化才重跑并提交）。

## Output（完成回执）

1. 变更文件清单；
2. 两条校验命令的输出摘要；
3. 依据（代码/文档出处）；
4. 待人工确认的点（owner 指派建议）。