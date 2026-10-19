# H0-02 Page Manifest + Doc Ledger · 交付证据

- 日期：2026-10-18
- 任务：H0-02（方案 §5.2 / §5.3 / §7 P0）
- 状态：`unit_verified`（AI 起草工具链与面板帮助视图归 H0-03/H0-04）
- 交付物：
  - `itsm-frontend/scripts/gen-page-manifest.mjs`（生成器：Page Manifest + Doc Ledger + ratchet 门禁）
  - `itsm-frontend/package.json`（`docs:manifest` / `docs:ledger:check`）
  - `.github/workflows/docs-ledger.yml`（CI 门禁 + 产物归档）
  - 生成物：`itsm-frontend/src/generated/page-manifest.json`、`docs/help/_ledger.json`、`docs/help/_coverage-baseline.json`

## 1. 实现要点

- **TS 数据源零预编译**：用 esbuild 现场把 `page-context/registry.ts` + `routes/route-paths.ts`
  打包为 CJS 后加载（避免 ts-node 配置分叉）；
- **Page Manifest**：每页输出 `pageKey/pattern/title/entrypoint/scene/docs/status/suggestions`
  （供文档起草 grounding 与后续服务端消费）；
- **Doc Ledger**：`coverage`、`totals{ok,missing,stale,orphan}`、逐页状态与 `orphanDocs`（未知 page_key
  的漂移检测）；`stale` 规则 = 文档 `last_reviewed` 缺失/超 90 天；
- **ratchet 门禁**：`--check` 对比 `_coverage-baseline.json`，`missing/stale/orphan` 任一回退即 exit 1；
  放宽需显式 `--write-baseline`（评审动作）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
node scripts/gen-page-manifest.mjs --write-baseline
node scripts/gen-page-manifest.mjs --check        # exit 0：pages=21 ok=21 missing=0 stale=0 orphan=0 coverage=100%
# ratchet 实证（模拟文档缺失）：
Rename-Item ..\docs\help\sla\monitor.md monitor.md.bak
node scripts/gen-page-manifest.mjs --check        # exit 1：missing 0 → 1（明确修复指引）
Rename-Item ..\docs\help\sla\monitor.md.bak monitor.md
node scripts/gen-page-manifest.mjs --check        # exit 0：恢复
```

实测输出（摘要）：

```
[docs-ledger] pages=21 ok=21 missing=0 stale=0 orphan=0 coverage=100.0%
[docs-ledger] OK（ratchet 未回退）                       # check
[docs-ledger] FAIL: 文档覆盖率回退（ratchet）：missing 0 → 1   # 模拟缺失
```

## 3. CI 接线（docs-ledger.yml）

- **触发面**：`page-context/**`、`route-paths.ts`、生成脚本、Manifest 产物、`docs/help/**`、本工作流；
- **步骤**：checkout → Node 22（npm cache）→ `npm ci --legacy-peer-deps` → `npm run docs:ledger:check`
  → 归档 `_ledger.json` + `page-manifest.json`；
- 回退即 FAIL；放宽基线需评审后提交 `--write-baseline` 产物。

## 4. 边界与遗留（条件项）

- `stale` 目前基于 `last_reviewed`（90 天）；与页面代码变更联动的 git 元数据判定归 H1-03 夜间任务；
- Manifest 尚未被服务端消费（Help Agent 工具 H0-03 与视觉 grounding 使用）；
- `docs-gate` 的存量 88 条断链与本批无关（C.3 为 advisory；docs/help 零断链）。