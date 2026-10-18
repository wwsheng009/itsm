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

- **F1：`/ai/triage` 恒退化关键词分类（sidecar 配置缺口）** — `itsm-ai-service-demo` 日志：`Guidance triage LLM unavailable, using keyword fallback: LLM_API_KEY not configured`（`itsm-ai-service/api/guidance.py:213`）。后端 `TriageService.SuggestForTenant` 优先走 Guidance sidecar（`guidanceClient != nil`），sidecar 的 `LLM_API_KEY` 为空即恒回退（容器 env 为静态 `LLM_PROVIDER=openai` / `LLM_MODEL=gpt-*`，与后端租户 provider `deepseek-v4.1-flash` 不一致）；本次后端网关修复不覆盖该端点。建议：为 sidecar 配置可用 key，或按 `TriageService` 既有 gateway 分支调整优先级（单独跟进）。
- **F2：`get_ci_impact` 工具 5001** — 与 LLM 无关（15.8ms、无 `ai_llm_calls` 记录）；`service/tool_registry.go:1309` 存在 `cmdb relationship service not initialized` 分支（bootstrap 依赖 `CIRelationshipService`），需单独定位注入/开关问题。

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
