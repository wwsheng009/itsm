# DEMO 部署证据：LLM 网关未绑定调用方走租户 provider 解析链（BE-8）

- 目标环境：`172.18.3.238` demo（`docker-compose.demo.yml`，端口 8089）
- 部署版本：`1.8.0-dev.00aaa26e`
- 远端 HEAD：`00aaa26eb3492174478da991bbd75fa3f8a2d31c`（`fix(llm): 未绑定调用方也走租户 provider 解析链（BE-8）`）
- 上一版本：`1.8.0-dev.fbb8fb0e`
- 执行时间：2026-10-18 21:2x-21:4x（本地）
- 部署脚本：`/tmp/deploy238_pe5_a.sh` / `_b.sh` / `_c.sh`（env 覆盖 `NEW`/`OLD_BE`/`OLD_FE`）；验证脚本：`.dev/deploy/deploy238_be8_verify.sh`

## 1. 部署步骤与结果

| 步骤 | 结果 |
|---|---|
| A：bundle `fbb8fb0e..00aaa26e` 上传并 checkout（分支 `deploy-demo-be8-00aaa26e`） | ✅ `.env.demo` `VERSION=1.8.0-dev.00aaa26e` |
| B：`docker compose build`（backend + frontend） | ✅ `/tmp/build-pe5-1.8.0-dev.00aaa26e.log` 尾部 `Image itsm-backend:1.8.0-dev.00aaa26e Built`、`Image itsm-frontend:...00aaa26e Built`、`BUILD_EXIT=0` |
| C：`up -d itsm-backend-demo itsm-worker-demo itsm-frontend-demo` + 健康检查 | ✅ 事后核验：三容器 `Up ... (healthy)`、镜像均为 `1.8.0-dev.00aaa26e`、`itsm-init-demo` `Exited (0)`；`GET /api/v1/version` → `1.8.0-dev.00aaa26e` |

备注：C 步执行窗口内本地到 demo 机的 SSH/HTTP 出现约 7 分钟瞬断（`Error reading SSH protocol banner` / `curl exit 52`），恢复后核验 C 步已成功落盘（远端 `uptime 546 days`，主机未重启，属本地网络侧抖动）。

## 2. 验证结果（脚本 `deploy238_be8_verify.sh`，`BE8-VERIFY-DONE` 正常收尾）

| # | 检查项 | 结果 |
|---|---|---|
| 0 | `GET /api/v1/version` | `1.8.0-dev.00aaa26e`（与部署一致） |
| 1 | `GET /ai/tickets/65/summary`（未绑定调用方 → 工单总结） | `code=0`、`method=llm`、返回真实中文摘要；`ai_llm_calls` 新增 **#49 13:38:50 `provider_key=baidu model=deepseek-v4.1-flash success=t latency=2310ms`**（修复前该路径落到静态 provider） |
| 2 | `POST /ai/triage` | 返回 keyword 启发式结果（`reasoning=keyword-based classification (LLM unavailable)`）→ sidecar 路径，属发现 **F1**（范围外，见 §3） |
| 3 | `POST /projects/1/ai/summary`（上一轮修复回归） | `generatedBy=ai`、`degraded=false`、`model=deepseek-v4.1-flash`、`chars=457`；`ai_llm_calls` **#50 13:38:52 baidu t 1717ms** |
| 4 | `POST /agent/tools/execute {"name":"get_ci_impact"}` | `code=5001`（15.8ms、无 LLM 调用记录）→ 属发现 **F2**（范围外，见 §3） |
| 5 | `ai_llm_calls` 尾部 | **#48 13:13:16 `openai/<static> success=f latency=15906ms`**（修复前同类静态 provider 失败样本）→ 部署后 #49/#50 均为 `baidu/deepseek-v4.1-flash success=t`，静态失败不再新增 |
| 6 | 降级日志（`--since 10m`） | 无 `degraded` / `falling back` / `LLM failed` 命中 |

## 3. 范围外发现（Follow-up）

- **F1：`/ai/triage` 恒退化关键词分类（Guidance sidecar 配置缺口）** — 链路：`handlers/ai/service.go:1862`（`TriageTicket`）→ `TriageService.Suggest` → `SuggestForTenant` 优先级 1：`guidanceClient != nil` 时调 `itsm-ai-service-demo`，仅当 sidecar **报错**才落到优先级 2（后端网关/租户 provider）。sidecar `api/guidance.py:133-135`：`api_key = os.getenv("LLM_API_KEY") or cfg.llm.api_key`，为空即 `RuntimeError("LLM_API_KEY not configured")`，被 :213 捕获后静默降级为 `_keyword_classify`（HTTP 200、`method="guidance-fallback"`、`confidence=0.7`、`explanation="keyword-based classification (LLM unavailable)"`）——**200 而非错误**，故后端永远不会触发网关回退。
  容器实测：`LLM_API_KEY=len(0)`（空）、`LLM_MODEL=gpt-4o-mini`、`LLM_PROVIDER=openai`、`LLM_BASE_URL=…(len 25)`；`docker-compose.demo.yml` 对 sidecar 用 `${LLM_API_KEY:-}` / `${LLM_ENDPOINT:-…}`，`.env.demo` 未提供该 key。另注：demo `.env.demo` 的 `LLM_MODEL=gpt-4o-mini` 若配上 key 直连，会重演「静态模型覆盖」类问题（与 BE-8 同源），需指向有效端点/模型。
  连带缺陷：`TriageTicket` 调的是 `Suggest(ctx,…)`，其 `SuggestForTenant(…, 1)` 固定 tenant 1（`service/triage_service.go:136-138`）——多租户下 sidecar 永远拿到租户 1。
  建议：① 为 sidecar 配可用 `LLM_API_KEY`/端点（注意与租户 provider 一致性）；② 或后端分诊改为「网关优先 / 识别 `method=guidance-fallback` 后回退网关」；③ 修 `Suggest` 硬编码租户（改传上下文租户）。
- **F2：`get_ci_impact` 工具 5001（更正：非注入问题，是跨租户 `ci_id` + not-found 泛化错误）** — 20:xx 复核实验（`.dev/deploy/deploy238_f2_probe.sh`）：
  - `get_ci_relationships ci_id=1` → `code=0` 空关系；`get_ci ci_id=1` → `code=0 {"found":false}` ⇒ `t.cmdb`/`t.ciRelationship` **均已注入**（`bootstrap/app.go:630` + `:796` 无条件注入），原「`cmdb relationship service not initialized` 嫌疑」不成立；
  - 真实根因：CI id=1 属 **tenant 3**（`CI-202610-000002`），会话租户为 1，而 demo 的 CI 只存在于 tenant 3/4/5（tenant 1 无任何 CI）。`get_ci_impact` 走 `ci_id` 分支**不做预检**，直接 `GetCIImpactAnalysis(ctx, 1, tenantID=1, 3)` → `ConfigurationItem.Query().Where(IDEQ(1), TenantIDEQ(1)).First()` → ent not found → `CI not found` → handler `common.FailWithErr(..., "操作失败")` → **5001「操作失败」**（15.8ms、无 `ai_llm_calls` 记录，与 LLM/BE-8 无关）；
  - 行为不一致：同一工具 `ci_number` 分支对未知/跨租户编号返回 **200 `{"found":false}`**（实测 `ci_number=CI-202610-000002` 亦然），`ci_id` 分支却 5001。
  建议（独立跟进）：`ci_id` 分支对齐 `ci_number` 的 graceful 语义（not-found → `{"found":false,"ci_id":…}`）或映射 404 语义码；补契约测试「跨租户/不存在 ci_id 不得 5001」。正路径（有效租户 CI + 关系数据）未验证，需对应租户会话复测。

## 4. 复现方式

```bash
# 远端：拉脚本并执行
bash /tmp/deploy238_be8_verify.sh
# 本地重新上传执行（若脚本更新）
python .dev/deploy/remote.py --sudo --put .dev/deploy/deploy238_be8_verify.sh /tmp/deploy238_be8_verify.sh \
  "sed -i 's/\r$//' /tmp/deploy238_be8_verify.sh && bash /tmp/deploy238_be8_verify.sh"
```

## 5. 代码侧回归（本地）

- `go build ./...` ✅
- `go test -p 1 -count=1 ./service/ -run "TestLLMGateway|TestImpactExplanation"` ✅（含 4 个 BE-8 用例：未绑定走解析链 / 无租户回落静态 / 流式路径 / 无解析器保持静态快速路径）
- `go test ./handlers/email_intake/ ./handlers/project/ ./handlers/ai/` ✅（含 `TestEmailIntakeExtractorDelegatesModelToGateway`）
