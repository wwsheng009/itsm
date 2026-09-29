# B1-09 实施证据（审批页增强）

> 文档类型：实施证据（任务 B1-09）
> Status: draft
> 编制日期：2026-09-27
> 任务：B1-09（审批页补 risk 徽标、目标对象跳转、dry-run 快照、过期倒计时；依赖 B1-05）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§4.2 B1-09）、`docs/plan/itsm-mcp-external-tool-integration-implementation-plan-2026-09-27.md`（M1-06 审批页基线）
> 核查方式：`tsc --noEmit` + Jest（B1-09 4 例 + AI 组件/审批页回归 56 例）

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| B1-09 | **`unit_verified`** | `/ai/approval` 新增：**目标对象列**（已核实路由可跳转 + `support_ref` 依据标签）、**确认时限列**（`expires_at` 1s 倒计时；过期置红）、**过期禁用**（通过/驳回置灰 + 原因提示）、**dry-run 预览快照**（展开行按需拉取详情展示） |
| 复用 | ✅ | 与对话内抽屉同源：`formatRemaining`（倒计时文案）、`targetUrl`（已核实路由）直接复用，避免两处口径漂移 |
| 遗留 | 3 项 | ①dry-run 快照需展开行触发（列表不携带 `result`，属后端既定边界）；②「四眼原则」未实现（B1-05 同源遗留）；③手工 E2E 截图归 B1-10 |

## 2. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-frontend/src/pages/(main)/ai/approval/index.tsx`（修改） | 目标对象列（`targetType#targetId`，仅已核实路由生成链接；`supportRef` 以「依据 …」标签展示）；确认时限列（倒计时/已过期 + dry-run 预览标签）；操作列过期时禁用并 Tooltip 说明；展开行追加 dry-run 快照块；仅当存在「待审批 + expiresAt」行时才起 1s 定时器 |
| 2 | `itsm-frontend/src/components/ai/dry-run-snapshot.tsx`（新建） | 按需拉取 `GET /agent/tools/:id`，展示预览结果与脱敏参数；loading/错误态明确；卸载即取消（不轮询、不泄漏） |
| 3 | `itsm-frontend/src/lib/api/ai-api.ts`（修改） | `ToolApproval` 补 `dryRun/verifyState/verifyNote/attemptCount/lastErrorCode`（后端 `handlers/ai/entity.go:69-85` 已有同名字段） |
| 4 | `itsm-frontend/src/pages/(main)/ai/approval/__tests__/b1-09.test.tsx`（新建） | 4 例：目标列跳转与依据、未核实路由不生成链接、倒计时/过期禁用、dry-run 展开快照（含脱敏断言） |

## 3. 行为口径

```text
确认时限列：
  non-pending            → dry-run 标签（若有）或「-」
  pending + 无 expiresAt → 「未设置」
  pending + 有 expiresAt → 剩余 …（1s 刷新）；now ≥ expiresAt → 「已过期」（红）
操作列：
  pending 且未过期 → 通过 / 驳回（原有）
  pending 且已过期 → 双按钮 disabled + Tooltip「不会被执行，也不会被补批准；请重新发起」
  其他            → 「已处理」（原有）
```

**不做的事**：不为未核实路由生成链接（`unknown_entity#X-9` 只展示文本）；不在列表接口缺 `result` 时臆造快照（展开行按需拉详情）。

## 4. 运行记录（本机，pwsh）

```text
npx tsc --noEmit → exit 0
npx jest --testPathPattern "b1-09" --coverage=false → PASS；4 passed
npx jest --testPathPattern "(ai/approval|components/ai)" --coverage=false
  → 7 suites / 56 passed（含 M1-06 审批页回归、B1-07 抽屉、B1-08 面板）
```

## 5. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | B1-09 交付：目标对象列/确认时限倒计时/过期禁用/dry-run 快照；4 例新增 + 56 例回归全绿，tsc 干净；判定 `unit_verified` |
