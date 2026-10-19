# H1-02 帮助文档校验器 + CI 门禁 + PR 通道 · 交付证据

- 日期：2026-10-18
- 任务：H1-02（方案 §5.5 自动校验 / §6 CI Job / §5.8 itsm-agent PR 通道）
- 状态：`unit_verified`（校验器 + CI 接线 + agent prompt；Actions/agent 运行时执行归平台侧）
- 交付物：
  - 校验器：`itsm-frontend/scripts/validate-help-docs.mjs`（+ `package.json` → `docs:validate`）
  - CI：`.github/workflows/docs-ledger.yml` 新增 `Help docs validator` 步骤与 `mkdocs build` 站点健全性步骤，
    触发路径补充脚本/mkdocs 配置/requirements
  - PR 通道：`itsm-agent/prompts/docs-maintenance.md`（docs-as-code 维护任务模板：grounding/校验/分级/PR 规则）

## 1. 校验项（错误级 → exit 1；警告级不失败）

| 类别 | 规则 |
|---|---|
| frontmatter | 必填键：`page_keys`/`audience`/`version`/`last_reviewed`/`owner`；`page_keys` 允许空数组（通用文档，如帮助中心索引），页面级覆盖缺口由 ratchet 兜底 |
| 取值域 | `audience ∈ {internal, customer, admin, all, end_user}`；`version` 形如 `1.8`/`1.8.2`；`last_reviewed=YYYY-MM-DD` 且非未来 |
| 结构 | 正文必须含一级标题（`# …`）、正文非空；过短（<200 字符）为警告 |
| 事实引用 | `page_keys` 必须存在于前端页面注册表（esbuild 现场加载 registry.ts，与 Page Manifest 同源）；未知键 = orphan 错误 |
| 链接 | 相对 Markdown 链接/图片必须解析到仓库内真实文件（含对 docs 外路径与 `/` 绝对路径的解析） |
| 敏感信息 | 私钥块 / AWS Access Key / `sk-` 令牌 / 明文口令赋值 |

未知 frontmatter 键 → 警告（保持规范可演进）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-frontend
npm run docs:validate         # 正向：OK 17 篇文档，0 错误
npm run docs:ledger:check     # ratchet + 内嵌助手注册表漂移检查：OK
npx eslint scripts/validate-help-docs.mjs

# 负向验证（临时文件，六类错误全部命中，退出码 1；验证后已删除）：
#   docs/help/zzz-validate-violation.md → owner 空 / audience 非法 / version 非法 /
#   last_reviewed 未来日期 / page_keys orphan / 断链
npm run docs:validate ; # EXIT=1，输出 6 个错误 + 1 条正文过短警告

cd ..
python -m mkdocs build --site-dir .tmp/mkdocs-site    # 站点构建通过（exit 0；告警均属 help 之外的既有域）
```

## 3. CI 接线（docs-ledger.yml）

```
Docs ledger ratchet check（H0-02）
Help docs validator    （H1-02）
Setup Python 3.12 → pip install -r requirements-docs.txt → mkdocs build（站点健全性）
Upload ledger artifacts
```

- 触发面：页面注册表/路由/生成器/**校验器**/页面清单/`docs/help/**`/`mkdocs.yml`/`requirements-docs.txt`/工作流自身；
- 说明：Actions 的真实运行需 push/PR 触发；其执行的 4 条命令均已在本地逐条验证通过。

## 4. PR 通道（itsm-agent）

`itsm-agent/prompts/docs-maintenance.md` 定义了文档维护任务模板：

- 输入：待办项（`/ai/help/todos` 的 pageKey/reason 或 ledger 缺口；禁止携带用户问题原文）；
- Grounding：页面注册表 / route-paths / Page Manifest / 同域既有文档 / KB（可用时）/ 工具目录；**禁止编造**；
- 交付：`docs/help/**.md` 新增或修订（一个 PR 聚焦一个页面域）；实质内容变化才更新 `last_reviewed`；
- 门禁：PR 必须附 `npm run docs:validate` 与 `npm run docs:ledger:check` 输出；**禁止** `--write-baseline` 放宽；
- 分级人工（§5.7）：L1 机械修正 / L2 内容更新 / L3 新增页面（首次人工过目 + owner）；
- PR 规则：分支 `docs/<page_key>-<date>`、标题 `[docs] …`、描述含触发源/依据/待确认点；不触碰生成物与基线文件。

## 5. 边界与条件项（不阻塞）

- mkdocs 步骤为非 strict（仓库其他文档域存在既有告警：`testing/*` 链接指向 excluded 页面；help 域零告警）；
- PR 创建/审阅的运行时（branch/commit/push/PR）由 `itsm-agent` 平台执行；本批交付的是"可被 agent 消费的
  标准提示 + 强制校验命令 + 分级规则"；
- 敏感信息规则为启发式（正则）；仓库级深度扫描继续由 `scripts/docs-gate/check-hardcoded-passwords.sh` 覆盖。