# V1-02 员工侧入口覆盖（移动端 / 门户 / 邮件场景）· 证据

- 批次：V1-02（方案 §7「V 系列 · 用户价值补齐」）
- 日期：2026-10-18
- 状态：`integration_verified`（前端批次；后端零改动——复用 V1-03 `POST /ai/tickets/interpret`）
- 原则：助手只给建议/预填，**绝不自动提交**；无权限不渲染（fail-closed）。

## 1. 交付摘要（三形态）

| 入口 | 形态 | 落点 |
|---|---|---|
| 移动端 | Dock 底部全宽抽屉（<768px）：圆角顶 + `env(safe-area-inset-bottom)` 安全区；FAB 避让手势条；`data-mobile` 形态标记 | `components/ai/dock/AssistantDock.tsx`（168 路由全局生效） |
| 门户 | `PortalAssistantEntry`：门户面「不知道选哪个 / 不会填」→ 打开常驻 Dock 并**预填提示词**（不发消息、不提交申请） | 服务目录 `/service-catalog`（`pageKey=service.catalog`）、服务申请 `/service-requests`（`pageKey=service.requests`） |
| 邮件 | `EmailIntentSuggest`：邮件 intake 复核抽屉「AI 理解邮件」→「我理解成：网络故障（高）」+ 证据 + 置信度 →「采纳」只回填**空白**字段，仍需人工「保存并重新核验」 | 邮件复核 `/email-intake` 人工修正卡（`pageKey=email.intake`） |

### 1.1 变更清单

- `itsm-frontend/src/components/ai/dock/AssistantDock.tsx`（移动端抽屉/安全区/形态标记）
- `itsm-frontend/src/components/ai/dock/__tests__/AssistantDock.test.tsx`（+1 例）
- `itsm-frontend/src/components/ai/PortalAssistantEntry.tsx`（新增，含 `PORTAL_ASSISTANT_ENTRY_TESTID`）
- `itsm-frontend/src/components/ai/__tests__/PortalAssistantEntry.test.tsx`（新增，3 例）
- `itsm-frontend/src/pages/(main)/service-catalog/index.tsx`（挂门户入口）
- `itsm-frontend/src/pages/(main)/service-requests/index.tsx`（挂门户入口，自定义提示词）
- `itsm-frontend/src/pages/(main)/email-intake/components/EmailIntentSuggest.tsx`（新增）
- `itsm-frontend/src/pages/(main)/email-intake/components/__tests__/EmailIntentSuggest.test.tsx`（新增，5 例）
- `itsm-frontend/src/pages/(main)/email-intake/index.tsx`（复核抽屉挂载 + 采纳回填收敛）

## 2. 复跑命令（在 `itsm-frontend/` 下）

```powershell
# 本批三套（移动端 / 门户 / 邮件）
npx jest src/components/ai/dock/__tests__/AssistantDock.test.tsx --coverage=false --watchAll=false --runInBand --silent
npx jest src/components/ai/__tests__/PortalAssistantEntry.test.tsx --coverage=false --watchAll=false --runInBand --silent
npx jest --testPathPattern "EmailIntentSuggest" --coverage=false --watchAll=false --runInBand --silent

# AI 表面回归 + 相关页面域
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
npx jest --testPathPattern "email-intake|service-catalog" --coverage=false --watchAll=false --runInBand --silent

# 静态门禁
npx tsc --noEmit
npx eslint src/components/ai/PortalAssistantEntry.tsx src/components/ai/__tests__/PortalAssistantEntry.test.tsx src/components/ai/dock/AssistantDock.tsx src/components/ai/dock/__tests__/AssistantDock.test.tsx "src/pages/(main)/service-catalog/index.tsx" "src/pages/(main)/service-requests/index.tsx" "src/pages/(main)/email-intake/index.tsx" "src/pages/(main)/email-intake/components/EmailIntentSuggest.tsx" "src/pages/(main)/email-intake/components/__tests__/EmailIntentSuggest.test.tsx"
```

> 后端零改动：无新增路由/权限码，**无需**重跑 `go run ./cmd/authz-gen`；邮件理解复用 V1-03 interpret 路由（`ai:read ∩ ticket:read`）。

## 3. 实测结果（2026-10-18）

| 门禁 | 结果 |
|---|---|
| AssistantDock（含新增移动端例） | 1 suite / **5 tests** 全绿 |
| PortalAssistantEntry | 1 suite / **3 tests** 全绿 |
| EmailIntentSuggest | 1 suite / **5 tests** 全绿 |
| AI 表面回归（`components/ai` + `lib/ai` + `lib/api`） | **122 suites / 1617 tests** 全绿（本批 +1 suite / +4 tests） |
| email-intake / service-catalog 域 | **4 suites / 44 tests** 全绿 |
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（9 个改动文件） | exit 0（0 error / 0 warning） |

## 4. 关键断言（防回归口径）

**移动端（AssistantDock）**

- 375px 渲染 → 打开面板：`data-mobile="true"`、宽度 `100%`（底部全宽抽屉）；
- 触发 `resize` 到 1280px → `data-mobile="false"` 且不再全宽（桌面右侧面板，宽度取 store）；
- FAB 位置在移动端切至 `right:16 / bottom: calc(16px + env(safe-area-inset-bottom, 0px))`。

**门户（PortalAssistantEntry）**

- 无 `ai:read` → 不渲染（入口级门禁）；
- 点击 → `openPanel()` 且 `pendingPrompt` 含「服务目录条目」；埋点复用 `assistant_opened`（`source=launcher`，`extra.entry=portal_entry`）；
- 负向：点击只预填 + 开面板，**不发消息、不提交**（`pendingPrompt` 由面板消费）；自定义提示词透传（service.requests 面复用同组件）。

**邮件（EmailIntentSuggest）**

- 门禁 `ai:read ∩ ticket:read`（与后端 interpret 路由同码）；无权限不渲染且零请求；
- 送理解文本先截断 ≤500 字（后端超长会 400）；
- 命中：渲染「我理解成：网络故障（高）」+ 证据 chips（`连不上网 → 网络故障`）+ 置信度；`suggestion_shown` 带 `card_id=email_intent, matched=true`；
- 采纳：只调用 `onAdopt`（页面侧仅回填**空白**字段 + 提示人工保存），`suggestion_clicked` 埋点；**无任何保存/开单调用**；
- 未命中（`matched=false`）：如实提示「未识别到明确术语：已按其他/中兜底」；
- 失败：不展示建议、`error_shown(interpret_failed)`、按钮保留可重试、不阻断人工复核；
- 空白文本 → 不渲染（边界）。

## 5. 边界与条件项（诚实标注）

1. **「外部门户」口径**：本仓库暂无独立外部顾客门户页面（`PortalConfig` 仅类型 + `ServiceCatalogApi` 占位；`PortalHomepage` 在 `components/service-catalog/index.ts` 中被注释）。本批以**员工自助门户面**（服务目录 / 服务申请）落点，组件 `PortalAssistantEntry` 与页面解耦（仅需 `pageKey` + 可选 `prompt`），真实门户上线时零改动挂载即可。
2. **邮件场景落点**：当前为 intake 复核抽屉（处理邮件→工单的作业面）；员工侧「邮件自动回复文案由助手起草」归后续批次（V2/联调）。
3. **移动端形态**：响应式 Web（无独立 App）；真机触控/视觉与浏览器 E2E 归联调。
4. **a11y**：新组件（门户入口/邮件建议卡）未纳入 axe 扫描面（I2-02 覆盖面板/诊断/建议条）；文案未 i18n（与工单模块现状一致）。
5. **事件字典零变更**：复用 `assistant_opened / suggestion_shown / suggestion_clicked / error_shown`；埋点失败静默（SDK 契约）。
6. **后端复用**：邮件理解走 V1-03 的确定性词典（平台级规则、不可租户化、无 LLM 归一化）——如实呈现，不冒充语义模型。

## 6. 关联

- 方案：`docs/plan/ai-assistant-dock-plan-2026-10-18.md` §7 V 系列补齐表（V1-02 行）+ §11.4（员工侧全入口）
- 复用证据：`V1-03-intent-interpretation-evidence.md`（interpret 契约）、`D0-02-assistant-dock-evidence.md`（Dock 容器）、`V0-02-self-service-ai-create-evidence.md`（员工侧自助）