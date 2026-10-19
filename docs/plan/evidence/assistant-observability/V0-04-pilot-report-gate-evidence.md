# V0-04 试点对照报告 + 放量门槛 · 交付证据

- 日期：2026-10-18
- 任务：V0-04（方案 §11.5 放量门槛 / §13.6 试点对照报告自动产出｜验收：§11 对照报告自动产出；达标才推进 Dock 放量）
- 状态：`integration_verified`（报告生成 + 门槛判定 + 看板/导出闭环；真实 PG 冒烟与分群归后续）
- 交付物：
  - 后端：`service/ai_pilot_report.go`（本期 vs 基线双窗口；单条 PG FILTER 聚合 `AggregatePilotCounters`；
    三段试点对照 + 全局负向指标 + §11.5 门槛判定）+ `handlers/ai/pilot.go`
    （`GET /api/v1/ai/observability/pilot-report?currentDays=&baselineDays=`，ai:read）+ `authz-gen` 预检再生
  - 前端：`aiGetPilotReport()` + `components/ai/observability/PilotReportCard.tsx`
    （门槛标签 / 三段试点标签 / 指标对照表 / 负向指标表 / 阈值与结论 / 空数据说明）+
    `lib/ai/pilot-report-markdown.ts`（Markdown 报告构建，复用 I2-02 `downloadTextFile` 导出）+
    `/ai/observability` 挂载（跟随窗口 `days` 联动）

## 1. 对照口径（实现即契约）

| 试点 | 主指标（改善 ≥30% 达标） | 次指标 | 负向 |
|---|---|---|---|
| V0-01 工单 Inline | `inline_adoption_rate` = inline_applied / inline_preview（up） | `inline_undo_rate`（down） | — |
| V0-02 自助&建单 | `self_service_resolve_rate` = resolved / answered（up） | `ai_create_latency_p95_ms`（down） | `ai_create_field_correction_rate`（误分类率代理，down） |
| V0-03 变更影响 | `plan_adoption_rate` = inline_applied / inline_preview（toolName=analyze_ci_impact_plan，up） | `plan_latency_p95_ms`（down） | — |
| 全局负向 | — | — | 误分类率代理 / `suggestion_ignore_rate` / `proactive_dismiss_rate` / `task_failure_rate` |

- **窗口**：本期 = 最近 `currentDays` 天；基线 = 其前 `baselineDays` 天（默认 7/7，上限 30）——
  与 §7「基线用同一埋点、试点前一周采集、不可回溯补采」一致；
- **达标**：主指标相对基线改善 ≥30%（§11.5「降幅 ≥30% 或等值业务指标」）；样本不足/基线为 0 → `不可比`（不误判达标）；
- **门槛**：≥2 项试点达标 **且** 负向指标未恶化（恶化判定含 **+10% 小样本容差**，实现口径已在报告阈值中回显）；
- 报告在无样本时输出显式说明（提示先完成基线采集）。

## 2. 验证证据（可复跑）

```powershell
cd itsm-backend
gofmt -l service/ai_pilot_report.go service/ai_pilot_report_test.go handlers/ai/pilot.go handlers/ai/pilot_handler_test.go
go test ./service/ -run 'TestBuildPilotReport' -count=1            # ok（纯函数口径 + sqlmock 双窗口接线）
go test ./handlers/ai/ -run 'TestGetPilotReport' -count=1          # ok（200/窗口 clamp/401/503）
go test ./service/ ./handlers/ai/ ./router/ ./middleware/ ./service/bot/ -count=1   # 5 包全绿
go run ./cmd/authz-gen   # 再生：/api/v1/ai/observability/pilot-report → ai:read

cd ../itsm-frontend
npx jest "src/components/ai/observability/__tests__/PilotReportCard.test.tsx" src/lib/ai/__tests__/pilot-report-markdown.test.ts --runInBand
# → 2 suites / 6 tests
npx jest --testPathPattern "ai/observability" --runInBand           # 2 suites / 16 tests（页面 12 + 卡片 4）
npx jest src/components/ai src/lib/ai src/lib/store src/lib/utils --runInBand   # 54 suites / 467 tests
npx tsc --noEmit ; npx eslint src/components/ai/observability "src/pages/*/ai/observability/*" …
```

关键断言：
- 口径：采纳率 0.5→0.8（+60%）达标；时延 30000→12000ms（+60%）达标；样本不足 → `不可比/样本不足` 且不判达标；
  负向从 10%→40% → 门槛拦截并列出恶化项；+10% 容差边界不判恶化；
- 门槛：三项达标 → `pass`；无样本 → 不放行 + 说明「基线不可回溯补采」；≥2 达标但负向恶化 → 拦截（负向优先）；
- 端点：200 + 窗口 clamp（999→30）+ 门槛字段齐全；缺租户 401；埋点未装配 503；
- 前端：门槛标签「达标（可放量）2/2」、三段试点标签（含未达标）、指标表（50.0%→80.0%，+60.0%）、
  负向表、阈值/结论/说明；**导出** 下载 `ai-pilot-report-7d.md`（内容含「# 试点对照报告」）+ `handoff(source=pilot_report_export)`；
  503 → 未启用提示且导出禁用；窗口 `days` 变化按新窗口重取。

## 3. 条件项（不阻塞）

- 报告聚合 SQL 使用 PG 专有 `FILTER` / `percentile_cont` / `jsonb->>`：本批经 sqlmock 断言接线，
  **真实 PG 冒烟（执行计划与 jsonb 索引）归联调**（迁移/索引沿用 O0-02 表）；
- `§13.6 分群对比`（租户/角色/页面维度）与 `建议排序权重`（suggestion_shown/clicked/ignored 反馈调权）
  属 O2-01，将在同一数据面上扩展；
- 窗口按试点起止日自动对齐（而非滚动天数）与运营侧定时出报告（周报/邮件）归运营迭代；
- 误分类率真值（人工标注）仍是已知缺口：当前为字段纠正代理（V0-02 证据已登记）。