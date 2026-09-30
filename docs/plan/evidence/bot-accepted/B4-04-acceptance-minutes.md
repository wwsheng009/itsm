# B4 验收纪要（`accepted` 评审输入，待人工签署）

> 文档类型：验收纪要（进入 `accepted` 评审的输入）
> Status: **draft（AI 辅助编制，`accepted` 需产品 + 测试签署后方可生效）**
> 编制日期：2026-09-27
> 任务：B4-04（B4 `accepted` 评审）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.5 B4-04、§3.1 五级口径、§5.2 AB4-01～AB4-04）
> 声明：本文由 AI 辅助执行整理证据与判定建议，**不代表已通过 `accepted`**；签署栏空白即视为未验收。

## 1. 判定建议汇总

| 验收项 | 判据 | 建议判定 | 说明 |
| --- | --- | --- | --- |
| AB4-01 | `e2e/` api + browser 双通道可复跑；run-summary 模板产出；mock 全量 + real smoke | ✅ **api 通道通过**；browser 通道**待 CI 首轮** | `evidence/bot-b4/B4-01-e2e-charter-evidence.md`；`run-summary-20260929-203102.md`（6/6，提交 `ab7eaa65`）；browser spec 已就位并过 `tsc`/`eslint`，CI `e2e-bot.yml` 处 `continue-on-error` 校准位 |
| AB4-02 | 指标可查：run 成功率/确认率/verify 失败率/成本维度 | ✅ | `evidence/bot-b4/B4-02-metrics-evidence.md`；`TestMetricsService_*` 12 断言组 + HTTP 契约 2 例 + 看板 3 例全绿；token 未接线以代理口径显式暴露 |
| AB4-03 | 五级状态回写完成（方案、阶段一报告、ROADMAP/CHANGELOG 回写 diff） | ✅ | `evidence/bot-accepted/B4-03-status-writeback.md` + `B4-03-writeback.diff`（12,018 字节） |
| AB4-04 | `accepted` 签署；残余风险登记；S4/S5 结论明确 | ⏳ **待签署** | 本文 + `B4-04-s4-s5-evaluation.md`（S4 暂缓 / S5 进入下一期 P1） |

**B4 里程碑建议判定**：**条件达标**——AB4-01（api 通道）/AB4-02/AB4-03 已满足，AB4-01（browser 通道首绿）与 AB4-04（人工签署）为剩余条件。**不得**在签署前标记为 `accepted`（五级口径不可跳跃，方案 §1.3）。

## 2. 残余风险登记（逐条，待签署确认）

| # | 风险 | 影响 | 现有缓解 | 处置建议 | 责任人（待填） |
| --- | --- | --- | --- | --- | --- |
| R-1 | **browser E2E 未经真实栈验证**（本机 Windows 无 Postgres/Docker） | 工作区/确认抽屉/证据面板的浏览器级回归不可信 | spec 含 self-skip 守卫（不假红）；CI `e2e-bot.yml` 就位 | 首轮 CI 绿后摘 `continue-on-error` 转阻断门 | 测试 |
| R-2 | **token 计量未接线**（B1-02 遗留） | 成本看板仅代理口径（步数/工具调用/时延） | `tokensRecorded=false` + 页面/`notes` 双提示 | B5-05 接线后补真实成本列 | 后端 |
| R-3 | **S3 `create_kb_draft` 未交付** | S3 只到「产物草稿」，未创建知识草稿实体 | 负向断言锁定「不自动发布」；产物归属/隔离已测 | B5 或下一期交付（含写工具审批链 + 草稿租户隔离评审） | 产品 + 后端 |
| R-4 | **产物读取端点缺失**（`bot_artifacts` 只写不读） | 用户看不到自己的产物列表；证据面板无数据源 | 存储与隔离已测 | B5 增补 `GET /ai/artifacts`（含分页与归属校验） | 后端 |
| R-5 | **场景种子未接入租户开通流程** | 新租户默认看不到 S1/S2/S3（需显式调用 `SeedScenarioBots`） | 幂等、不覆盖管理员改动 | 产品决策后接入 provisioning（或保持显式装配） | 产品 |
| R-6 | **指标行数上限（50000）在大租户可能触顶** | 指标按已读取部分计算（`notes` 明示） | 窗口上限 90 天 + 按 Bot/入口过滤 | 压测后评估采样/预聚合 | 后端 |
| R-7 | **既有 flake 与失败用例（O-3/O-4/O-5，D-1/D-2 已修）** | 回归噪音 | 以 A/B 或隔离复跑证明与本系列无关 | 既有治理继续跟踪 | 测试 |
| R-8 | **`bot.enabled=false` 兼容态覆盖** | 开关关闭时的零行为变化是发布前提 | 逐任务均有「未注入即零变化」口径与用例（B0–B4） | 发布前跑开关关闭全链路门禁（复用 B0-07 口径） | 测试 |

## 3. 证据索引（可复核清单）

| 阶段 | 证据目录/文件 |
| --- | --- |
| BP5/B0 | `docs/plan/evidence/bot-b0/`（BP5、B0-01～B0-07） |
| B1 | `docs/plan/evidence/bot-b1/`（BP8、B1-01～B1-10） |
| B2 | `docs/plan/evidence/bot-b2/`（B2-01～B2-06） |
| B3 | `docs/plan/evidence/bot-b3/`（B3-01、B3-02、B3-06、B3-07、场景验收单） |
| B4 | `docs/plan/evidence/bot-b4/`（B4-01 章程与 run-summary、B4-02 指标） |
| 治理/验收 | `docs/plan/evidence/bot-accepted/`（B4-03 回写与 diff、B4-04 本文与 S4/S5 评估） |

复跑命令（任选）：`pwsh e2e/run-bot-e2e.ps1 -Channel api`；逐包：`go test ./service/bot/ ./handlers/ai/ ./tests/botintegration/ ./ent/schema/ -count=1`；前端：`npx tsc --noEmit` + `npx jest src/components/ai src/pages/(main)/ai --coverage=false`。

## 4. 未竟项与下一期输入

| # | 项 | 归属 |
| --- | --- | --- |
| 1 | browser E2E 首轮 CI 绿 + 摘 `continue-on-error` | B4 收尾（CI） |
| 2 | S5 场景（服务目录/服务请求工具 + pilot） | B5-01～B5-03 |
| 3 | S4 复评（变更域工具 spike + CAB 语义评审） | B5-04 |
| 4 | token 计量与真实成本列 | B5-05 |
| 5 | `create_kb_draft`、`GET /ai/artifacts`、场景种子 provisioning | B5 或产品决策后 |

## 5. 签署栏（人工，签署后本文件方可作为 `accepted` 依据）

| 角色 | 姓名 | 结论（同意 / 有条件同意 / 不同意） | 条件与备注 | 日期 |
| --- | --- | --- | --- | --- |
| 产品负责人 |  |  |  |  |
| 测试负责人 |  |  |  |  |
| 安全/合规（如涉及权限与凭据面） |  |  |  |  |
| 技术负责人 |  |  |  |  |

> 签署说明：任一条「有条件同意」须把条件补入 §2 残余风险登记并指定责任人；「不同意」须写明阻塞项与复评时间。

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B4-04 验收输入成文：AB4-01～AB4-04 判定建议（B4 条件达标）、8 条残余风险登记、证据索引与复跑命令；**签署栏空白，`accepted` 待人工签署** |
