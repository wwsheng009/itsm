# O1-02 条件项闭环：定时巡检 sweeper + 订阅偏好映射（2026-10-18）

- 任务：方案 `docs/plan/ai-assistant-dock-plan-2026-10-18.md` §13.5「实时健康与告警」的
  「告警通道：通知中心（复用现有通知）+ **平台级系统配置开关**」落地；
  同时闭环 O1-02 进度块登记的条件项「定时巡检 sweeper 与订阅偏好映射归后续
  （当前显式触发，避免默认打扰）」。
- 状态：`integration_verified`（单测 + 编译 + 既有包回归全绿；真实 PG/RLS 与浏览器 E2E 待环境）。

## 1. 交付物

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| 巡检服务 | `itsm-backend/service/ai_health_sweeper.go`（新增） | 全租户定时巡检：Snapshot + SLO + DispatchAlerts（复用 30 分钟去重） |
| 偏好映射 | `itsm-backend/service/ai_health.go` | `AIHealthAlertEventType` + `filterRecipientsByPreference` + `skippedByPreference` |
| 事件字典 | `itsm-backend/dto/notification_preference_dto.go` | 新增 `ai_health_alert`（AI 健康告警） |
| 默认偏好 | `itsm-backend/service/notification_preference_service.go` | `InitializeDefaultPreferences` 纳入 `ai_health_alert`（站内默认开启） |
| 配置 | `itsm-backend/config/config.go` + `config.yaml.example` | `ai_health` 块（`sweeper_enabled` 默认 false / 间隔 / 窗口）+ `ai_telemetry` 块示例；`LoadConfig` viper 回写补齐两个新块 |
| 接线 | `itsm-backend/internal/bootstrap/app.go` | `Application.AIHealthSweeper` + `startBackgroundTasks` 按开关启动 |
| 前端 | `src/pages/(main)/notifications/index.tsx`、`src/lib/i18n/translations.ts`（zh/en）、`src/lib/api/ai-api.ts` | 偏好页新增「AI 健康告警」条目 + 派发结果新增 `skippedByPreference?` 类型 |
| 测试 | `service/ai_health_sweeper_test.go`、`service/ai_health_preference_test.go`、`config/config_ai_observability_test.go` | 7 + 3 + 4 例 |

## 2. 契约（实现口径）

- **默认不打扰**：`ai_health.sweeper_enabled` 缺省 false —— 不配置即维持「仅显式触发端点」
  （`POST /ai/health/alerts/dispatch` 行为/返回向后兼容，仅新增字段）；
  显式开启后 `startBackgroundTasks`（worker/all 模式）启动，启动即跑一轮 + 按间隔巡检。
- **巡检口径**：每轮枚举租户（系统旁路 `tenantctx.SystemContext`，单轮上限 200 租户，
  命中上限打 warn 并留待下一轮）；逐租户 `tenantctx.WithTenantID` 收窄后调用既有
  `DispatchAlerts`（同一 Snapshot/SLO/去重实现，无第二套口径）。
- **租户级隔离**：单租户错误或 panic 只计入 `Errors` 并继续其余租户；`RunOnce` 整体不因单租户失败报错。
- **订阅偏好映射**：收件人在「活跃管理员→回退活跃用户」候选之上，按
  `notification_preferences(event_type=ai_health_alert).in_app_enabled` 过滤：
  显式 false → 排除（计入 `skippedByPreference`）；无偏好行 → 放行（与
  `channelAllowedForEvent` 的平台语义一致）；偏好查询失败 → 本轮该租户不投递（fail visible，
  避免偏好不可读时误发）。
- **默认值**：间隔 15 分钟、窗口 7 天（上限 30）、单轮 200 租户；均为服务构造器兜底（<=0 回落）。

## 3. 复跑命令与实测结果

```powershell
# 后端 itsm-backend
gofmt -l -w service/ai_health.go service/ai_health_sweeper.go service/ai_health_sweeper_test.go service/ai_health_preference_test.go service/notification_preference_service.go dto/notification_preference_dto.go config/config.go config/config_ai_observability_test.go internal/bootstrap/app.go
go build ./service/ ./internal/bootstrap/ ./config/ ./dto/        # exit 0
go test ./service/ -run 'TestAIHealth' -count=1 -v                # 9/9 PASS（含既有 2 例回归）
go test ./service/ ./config/ ./dto/ ./handlers/notification/ ./handlers/ai/ -count=1
# ok service 56.5s / config 0.6s / dto 0.6s / handlers/notification 1.2s / handlers/ai 6.1s
go test ./internal/bootstrap/ -count=1                            # ok（首次因磁盘空间不足失败，go clean -cache 释放 23GB 后通过）

# 前端 itsm-frontend
npx tsc --noEmit                                                  # exit 0
npx eslint 'src/pages/(main)/notifications/index.tsx' src/lib/i18n/translations.ts src/lib/api/ai-api.ts
npx jest --runTestsByPath 'src/pages/(main)/notifications/__tests__/index.test.tsx' 'src/pages/(main)/ai/observability/__tests__/index.test.tsx' --coverage=false --watchAll=false --runInBand --silent
# 2 suites / 16 tests 全绿（含「实时健康：ai:write 下派发告警并展示结果」回归）
```

单测清单：

- `service/ai_health_sweeper_test.go`（4 例）：缺省值与窗口上限；未装配（nil 接收者不 panic）；
  两租户巡检（A 违规 2 条/2 管理员 → 4 通知、B 样本不足 0 告警；第二轮 30 分钟去重 0 新建/2 skip）；
  租户级错误隔离（坏 client → `Errors=1` 且 `RunOnce` 不报错）。
- `service/ai_health_preference_test.go`（3 例）：退订者被排除且只投递给放行者；
  全部退订 → 评估照常但 0 投递；无偏好行 → 默认放行（回归既有语义）。
- `config/config_ai_observability_test.go`（4 例）：`ai_telemetry` / `ai_health` 的
  `${VAR:default}` 环境变量覆盖解析；缺省零值（= 缺省行为由服务兜底，巡检关闭）。

## 4. 未覆盖 / 诚实边界

- **真实 PG/RLS 未在本轮验证**（本机 Docker 未运行）：租户枚举在 enforce 模式下的
  管理池/BYPASSRLS 行为、通知写入的 RLS 路径待真实库冒烟；已在方案文档保留条件项。
- **浏览器 E2E 未做**：偏好页新增条目的交互走查依赖真实前端 + 后端联调。
- 前端仅登记偏好条目与类型；派发结果面板文案未展示 `skippedByPreference`（留待 UI 走查一并处理）。
- 巡检单轮 200 租户上限为保护性设计；超过时剩余租户在下一轮处理（已有 warn 日志），
  不做单轮全量（避免长事务）。
- 去重仍为「同租户同标题 30 分钟」；跨轮次告警风暴治理维持既有实现，未新增升级/静默策略。---

## 附录：O1-02 条件项闭环 · 派发面板展示退订过滤人数（2026-10-18，`integration_verified`）

- 条件项原文：「派发面板文案暂未展示 `skippedByPreference`」。
- 交付：`/ai/observability` 派发结果区在 `skippedByPreference > 0` 时渲染
  `health-dispatch-preference-filtered` 文案：「另有 N 名候选收件人因订阅偏好（站内通知关闭）被排除」；
  - 与既有「去重跳过 {skipped}」显式区分（一个是 30 分钟去重，一个是用户退订）；
  - **0/缺省不渲染**（旧后端不回传该字段时不臆造数字，诚实缺省）。
- i18n：`aiObservability.healthDispatchPreferenceFiltered`（zh/en，占位符 `{muted}`）。
- 可复跑：
  - `npx jest --runTestsByPath 'src/pages/(main)/ai/observability/__tests__/index.test.tsx' --runInBand`
    → 1 suite / 17 tests 通过（含：>0 显示断言、=0 不渲染断言）；
  - `npx tsc --noEmit`、`npx eslint <3 文件>` → exit 0。
- 边界：真实 PG/RLS 冒烟与浏览器走查仍待环境（Docker 未运行）。