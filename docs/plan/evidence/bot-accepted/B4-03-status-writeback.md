# B4-03 实施证据（五级状态回写与文档治理）

> 文档类型：实施证据（任务 B4-03，治理项）
> Status: draft
> 编制日期：2026-09-27
> 任务：B4-03（按五级状态回写：本方案 §3.3/§5.2、阶段一报告、`ROADMAP.md` / `CHANGELOG.md`）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-03、§5.2 AB4-03）
> 核查方式：回写 diff 留档（`B4-03-writeback.diff`）+ 证据路径逐条可解析校验

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| AB4-03 | ✅ | 四处回写完成：方案（§3.3 速览 + §5.2 判定 + 任务卡状态 + 变更记录）、阶段一报告（新增 §9 落地状态回写）、`ROADMAP.md`（当前收敛项 + Last synced）、`CHANGELOG.md`（[Unreleased] → Added） |
| B4-03 | ✅（治理项，不单独定级） | 回写 diff 已归档（12,018 字节）；状态口径严格按五级（未用 checkbox / "设计完成" 冒充交付） |

## 2. 回写清单

| # | 载体 | 回写内容 | 状态口径 |
| --- | --- | --- | --- |
| 1 | `docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md` | §3.3.1 状态速览逐任务行（BP5–B4-02）+ §5.2 判定列 + 每个任务卡「状态」行 + 变更记录追加 | 五级口径逐项：`unit_verified` / `integration_verified` / `flow_verified`；条件项与未竟项显式登记 |
| 2 | `docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md` | 新增 **§9 落地状态回写（2026-09-27）**：BP5–B4 六行交付状态 + 三处与初稿设计的偏差 + 变更记录追加 | 报告本体保持「设计态」标注，交付状态集中在 §9，避免两处口径混用 |
| 3 | `ROADMAP.md` | 「当前收敛项」新增 `[ ] 用户侧 Bot 能力落地（B0–B4）`（已完成 / 剩余两行）；`Last synced` 更新为 2026-09-27 | 功能分支交付**不置 `[x]`**（未合并、未发布）；剩余项逐条列出 |
| 4 | `CHANGELOG.md` | `## [Unreleased]` → `### Added` 新增首条：Bot 能力落地（B0–B4 摘要 + 证据目录 + 方案链接） | 置于 Unreleased（未发版），标注功能分支名 |

## 3. 回写 diff（留档）

- 归档文件：`docs/plan/evidence/bot-accepted/B4-03-writeback.diff`（`git diff -- ROADMAP.md CHANGELOG.md docs/plan/ai-bot-capability-landing-analysis-2026-09-27.md`，12,018 字节；方案本体 diff 随同批提交，见提交记录）。
- diff 统计：`CHANGELOG.md +1`、`ROADMAP.md +5/-1`、`analysis +22`、方案文件（同批，§3.3/§5.2/任务卡/变更记录多处）。

## 4. 治理检查（防「以设计冒充交付」）

| # | 检查 | 结果 |
| --- | --- | --- |
| 1 | 是否存在未跑的判定：标 `integration_verified` 及以上者必须有测试/命令证据 | ✅ 逐项有证据文件与命令（引用 `evidence/bot-b*/` 与复跑清单） |
| 2 | 条件项是否显式（不隐藏） | ✅ 每行标注「条件项」/「部分交付」/「剩余」 |
| 3 | 未核实项是否保留【未核实】标记 | ✅ 方案 §10 未核实清单保持；阶段一报告 §9 明确「本报告初稿未运行测试」 |
| 4 | 是否使用 checkbox 冒充交付 | ✅ 未使用；ROADMAP 项保持 `[ ]` |
| 5 | 交付状态与证据路径可解析 | ✅ 抽检 8 条路径全部存在（bot-b0/b1/b2/b3/b4 证据目录） |

## 5. 遗留

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | B4-04 `accepted` 评审纪要（产品 + 测试签署，残余风险逐条登记） | **B4-04** |
| 2 | 方案 §5.2 判定列中「条件项闭环」的最终收敛（browser E2E 首轮 CI 绿、token 接线） | B4-04 评审输入 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B4-03 交付：四处回写（方案 / 阶段一报告 §9 / ROADMAP / CHANGELOG）+ 回写 diff 归档 + 5 项治理检查通过 |
