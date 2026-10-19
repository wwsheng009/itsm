# V1-03 口语↔术语确认 · 交付证据

- 日期：2026-10-18
- 方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` §7「V 系列 · 用户价值补齐」V1-03
  ——「我理解成：网络故障（高）→ 确认 / 修改」（§11.4 用户视角补充需求第 5 项）
- 状态：`integration_verified`（前端 121 suites / 1613 tests + 后端 3 包 + `authz-gen` 全绿）
- 边界不变式：**AI 只给理解建议，确认/修改后才进入表单生成；表单提交前仍可修改，绝不自动提交。**

## 1. 交付物

| 层 | 文件 | 要点 |
|---|---|---|
| 后端端点 | `itsm-backend/handlers/ai/interpret.go` | `POST /api/v1/ai/tickets/interpret`：口语 → 术语（分类/优先级）+ 证据片段 + 置信度；**纯确定性词典**（无 IO / 不调 LLM，无 Provider 也可用）；不生成表单、不写业务字段 |
| 路由 | `itsm-backend/router/ai_routes.go` | `ai:read` ∩ `ticket:read`（与 reply-draft / batch-* 同门控）；`go run ./cmd/authz-gen` 已再生，`TestPrecheckMapIsFresh` 绿 |
| 后端测试 | `itsm-backend/handlers/ai/interpret_test.go` | 6 组契约：口语映射+证据、否定表达、紧急词、未识别降级、输入边界（空/缺字段/501 字 400、500 字 200 + 标题 30 字截断） |
| 前端 API | `itsm-frontend/src/lib/api/ai-api.ts` | `aiInterpretTicketIntent(utterance)` + `TicketIntentInterpretation` / `TicketIntentTerm` / `TicketIntentFields` |
| 确认卡 | `itsm-frontend/src/components/a2ui/IntentUnderstandingCard.tsx` | 「我理解成：**网络故障（高）**」+ 证据 chips（「连不上网」→ 网络故障）+ `确认 / 修改`；修改态为分类/优先级下拉（`继续 / 返回`）；未识别时文案降级为「没识别到明确术语，先按默认理解：」 |
| 流程接线 | `itsm-frontend/src/components/a2ui/A2UIFormRenderer.tsx` | 两阶段：发送 → **先理解**（确认/修改）→ 才生成 A2UI 表单；用户确认值作为表单初值（`/ticket/*` 覆盖 + `form.setFieldValue`）；理解失败**降级**为原直接生成路径（不阻塞建单） |
| 前端测试 | `src/components/a2ui/__tests__/IntentUnderstandingCard.test.tsx`（5 例）、`src/components/a2ui/__tests__/A2UIFormRenderer.test.tsx`（4 例）、`src/lib/api/__tests__/ai-api.test.ts`（+2 例） | 3 suites / 68 tests 全绿 |

## 2. 关键口径与断言

1. **术语与证据**：`terms[]` 每项带 `surface`（命中的口语片段）、`term`（术语标签）、`value`（规范值）、`confidence`。
   例：`"我的电脑连不上网了，很急"` → `summary="网络故障（高）"`、`type.network`（surface=连不上网, 0.93）、`priority.high`（surface=很急, 0.90）。
2. **否定优先（防误升级）**：`exclusive` 规则（不着急/不急/不紧急/低优先级…）命中即锁定，避免「不着急」被「急」覆盖；
   覆盖用例：`打印机坏了，不着急`→低、`网络有点慢，这件事不紧急`→低、`软件装不上，不急`→低。
3. **紧急词分级**：`非常紧急/救火/宕机/线上挂了/生产环境…`→`urgent`（高于「急」类，置信度也更高）；
   `production 环境非常紧急` 场景 summary=`网络故障（紧急）`。
4. **未识别不猜测**：无命中 → `matched=false`、`confidence=0.35`、`type=other/priority=medium`、`terms=[]`、`summary="其他（中）"`；前端据此弱化文案（不假装识别成功）。
5. **输入边界 fail-visible**：空/纯空白 utterance → 400（code 4000）；501 字 → 400；500 字 → 200 且 title 截断 ≤30 字（与 A2UI 建单标题口径一致）。
6. **两阶段写边界**：确认卡出现在表单生成之前；未点击「确认/继续」时 `generateTicketForm` **零调用**（测试断言）。
7. **修正值生效**：修改后 `applied = {...fields, ...changes}` 覆盖 `/ticket/type|priority|title|description`，表单初值即用户认可的理解。
8. **纠正不重复计数**：确认卡上的字段修正立即上报 `ai_create_field_corrected`（taskCardId=`intent_interpretation`）；生成时 tracker 基线改用 applied 值 → 提交时的纠正面只统计「表单阶段」的再修改，端到端断言全程仅 1 条纠正事件。
9. **埋点零字典变更（刻意决策）**：复用既有事件 `confirmation_shown / confirmation_accepted / confirmation_rejected(reasonCode=modify) / ai_create_field_corrected`（方案 §13.2 三处契约零改动，无新增事件）；`taskCardId` 区分卡片归属。
10. **理解失败降级**：interpret 抛错 → 记录 console + 走原 `generateForm`（不弹错、不阻塞建单），测试覆盖。

## 3. 可复跑命令与结果

```powershell
# 后端
cd itsm-backend
gofmt -l -w handlers/ai/interpret.go handlers/ai/interpret_test.go router/ai_routes.go
go test ./handlers/ai/ -run 'TestInterpret' -count=1        # ok（1.6s）
go test -p 2 ./router/ ./middleware/ -count=1               # ok（含 TestPrecheckMapIsFresh）
go run ./cmd/authz-gen                                      # 已生成 middleware/rbac_precheck_gen.go

# 前端
cd ../itsm-frontend
npx jest src/components/a2ui src/lib/api/__tests__/ai-api.test.ts --coverage=false --watchAll=false --runInBand --silent
# → 3 suites / 68 tests passed
npx jest --testPathPattern "tickets/ai-create" --coverage=false --watchAll=false --runInBand
# → 1 suite / 1 test passed（V0-02 页回归）
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
# → 121 suites / 1613 tests passed
npx tsc --noEmit        # exit 0
npx eslint src/components/a2ui/A2UIFormRenderer.tsx src/components/a2ui/IntentUnderstandingCard.tsx \
  src/components/a2ui/__tests__/IntentUnderstandingCard.test.tsx src/components/a2ui/__tests__/A2UIFormRenderer.test.tsx \
  src/lib/api/ai-api.ts src/lib/api/__tests__/ai-api.test.ts     # 0 error
```

## 4. 条件项（诚实标注，未做即未做）

- 词典为**平台级规则词典**（不入库、不可租户化）；租户自定义术语/分类别名、LLM 辅助归一化 → 后续迭代；
- 确认卡当前接入点为员工侧 **`/tickets/ai-create`**；Dock 对话、外部门户、邮件场景的口语入口归 V1-02/V2；
- 理解结果不落审计/不持久化（只读解释，无业务写入，无敏感数据）；
- 新卡未纳入 `a11y-surfaces` axe 扫描（I2-02 覆盖面遗留条件项的一部分）；
- 真实 Provider / 真库 E2E 未跑（本机 Docker daemon 未运行，沿用既有条件项）；
- 观察项（非产品缺陷）：`A2UIFormRenderer.test.tsx` 既有 fixture 的 `dataModelUpdate.path` 用 `ticket`（后端真实值为 `/ticket`），
  导致测试内 model 未吃到 AI 建议值（表单由 `form.setFieldValue` 呈现）；V1-03 覆盖路径按真实口径 `/ticket/*` 写入。