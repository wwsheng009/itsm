# H1-03 夜间批任务：代码变更 → stale 标记 → 更新提议 · 交付证据

- 日期：2026-10-18
- 任务：H1-03（方案 §5.6 触发源 4 / §7 P1；对比 Page Manifest/路由 diff → 标记相关文档 stale → 生成更新提议）
- 状态：`integration_verified`（检测核心/CLI/测试/夜批工作流/PR 通道输入；Actions 运行时触发归平台侧）
- 交付物：
  - 共享工具：`itsm-frontend/scripts/lib/help-docs.mjs`（frontmatter 解析 / 文档遍历 / 注册表加载 / 清单解析）
  - 检测核心：`itsm-frontend/scripts/lib/doc-stale-core.mjs`（页面签名 / 字段级 diff / 提议构建 / Markdown 摘要，
    纯函数）
  - CLI：`itsm-frontend/scripts/detect-doc-stale.mjs`
    （`--base` / `--base-manifest` / `--head-manifest` / `--out` / `--fail`；产出 JSON + MD；写 `GITHUB_STEP_SUMMARY`）
  - 测试：`scripts/__tests__/doc-stale.test.mjs`（5 例纯函数）+ `doc-stale-detect.test.mjs`（2 例端到端）
  - npm：`docs:stale` / `docs:stale:test`
  - 夜批：`.github/workflows/docs-stale-nightly.yml`（cron `0 18 * * *` = 次日 02:00 CST；
    测试 → 检测 → artifact → 「[docs] 夜间文档更新提议」Issue 幂等创建/刷新）
  - PR 通道：`itsm-agent/prompts/docs-maintenance.md` 增加 `{{stale_proposal_json}}` 输入与触发源说明
  - 生成物忽略：`.gitignore` 新增 `docs/help/_stale-proposals.{json,md}`（CI 以 artifact 留存）

## 1. 检测口径（实现即契约）

| 项 | 规则 |
|---|---|
| 变更信号 | 页面清单（`src/generated/page-manifest.json`，H0-02 生成物）基线 vs 当前的**页面定义**差异：`pattern`/`title`/`entrypoint`/`scene`/`suggestions(id,kind,readOnly)`；路由/注册表变更经漂移门禁（`docs:ledger:check`）强制重生成后再由本检测承接 |
| 动作 | 变更页有文档 → `review_update`（原因 `manifest_changed:<字段>`）；无文档 → `create_doc`；新增页无文档 → `create_doc`（`new_page`）；删除页仍有文档引用 → `remove_reference`（`page_removed`） |
| 优先级 | `create_doc` > `remove_reference` > `review_update`；同级按 pageKey |
| 基线降级 | `git show` 取不到基线清单（首次引入生成物）→ 按空基线处理并在报告标注 `baseManifestMissing`（不报错） |
| 失败语义 | 夜批默认退出码 0（不因提议失败）；`--fail` 且有提议 → 2；参数/环境错误 → 1 |

产物（`_stale-proposals.json`）结构：`{generatedAt, base, headManifest, baseManifestMissing, headCount, diff:{added,removed,changed}, proposals:[{pageKey,action,docs[],reasons[]}], summary:{actions}}`。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npm run docs:stale:test        # node:test：7 例全过（5 纯函数 + 2 端到端）
npm run docs:stale -- --base HEAD --out ../.tmp/stale-proposals.json
# → base=git:HEAD pages=21 changed=0 added=21 removed=0 proposals=21（review_update:21）；退出码 0（基线无清单的降级路径）
npx eslint scripts/detect-doc-stale.mjs scripts/lib/*.mjs scripts/__tests__/doc-stale*.mjs   # 0 问题

cd ..
python -c "import yaml; d=yaml.safe_load(open('.github/workflows/docs-stale-nightly.yml', encoding='utf-8')); ..."
# → YAML OK: docs-stale-nightly | cron: 0 18 * * * | steps: 6
git check-ignore -v docs/help/_stale-proposals.json docs/help/_stale-proposals.md   # 已忽略（.gitignore:355/356）
```

端到端用例断言（合成 base/head + 真实 docs 映射）：
- `dashboard`（标题变更 + 有文档）→ `review_update` + `manifest_changed:title` + 关联文档；
- `brand.new.page`（新增 + 无文档）→ `create_doc`；
- `sla.monitor`（head 删除 + 有文档）→ `remove_reference`；
- 同一清单 → 0 提议且 `--fail` 仍退出 0；MD 摘要同步产出。

## 3. 夜批工作流（H1-03）

```
schedule cron 0 18 * * *（次日 02:00 CST）/ workflow_dispatch
└─ docs-stale-nightly job
   1) node --test（检测核心与端到端用例，零 npm ci 依赖）
   2) node scripts/detect-doc-stale.mjs --base origin/main --out ../docs/help/_stale-proposals.json
      （Step Summary 汇总）
   3) upload-artifact：_stale-proposals.{json,md}（保留 14 天）
   4) github-script：有提议时创建/刷新「[docs] 夜间文档更新提议」Issue（标题幂等；
      正文含处置指引 → itsm-agent/prompts/docs-maintenance.md + 两条强制校验命令）
```

## 4. 边界与条件项（不阻塞）

- 页面**源码文件**改动但页面定义（清单字段）未变 → 不触发（保守设计：避免噪音；如需扩到源码级映射，
  需引入「文件 → pageKey」映射表，建议与 H1-04 同批评审）；
- Actions 的实际调度/Issue 写入需平台触发（`issues: write`）；本地已验证其执行的等价命令与产物；
- `remove_reference` 仅在仍有文档引用时产出（无引用的删除页视为自然 orphan，不产生噪音提议）；
- 提议的认领/忽略状态与看板消费归 H1-04；`--fail` 门禁保留给未来"PR 内 stale check"按需启用。