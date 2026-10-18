# DEMO 验证证据：应用内 LLM 配置请求级下发 guidance sidecar（F2，2026-10-18）

- 版本：`1.8.0-dev.c05cc49e`；实现提交 `c05cc49ecfdab2364c183a53c47c6dcf5f1cdfdf`（`feat(ai): 应用内 LLM 配置请求级下发 guidance sidecar（F2）`）
- 演示环境：172.18.3.238，容器 `itsm-backend-demo` / `itsm-worker-demo` / `itsm-frontend-demo` / `itsm-ai-service-demo`（sidecar **本次已按新代码重建**，与 F1 不同）
- 前置：F1（`1.8.0-dev.eb214870`）已识别 sidecar 静默降级并回落网关；见 [DEMO-F1 证据](DEMO-F1-20261018-eb214870-EVIDENCE.md)
- 范围：纯后端（`service/` Go 侧解析与下发）+ sidecar（`itsm-ai-service/api/guidance.py` 新增 `llm` 覆盖分支）；前端无改动（C 步以旧 FE 镜像打新 tag）

## 1. 目标与方案

问题：sidecar（itsm-ai-service）是单进程单配置服务，只读自身 env/config.yaml；而应用内配置（`llm_provider_configs` 表，逐租户 + AES-GCM）只存在于 Go 注册表，二者完全隔离 → 应用内配置永远到不了 sidecar（F1 只能兜底回落网关）。

方案（请求级下发，A 方案）：`TriageService` 调 sidecar 前解析**本租户默认实例**，把 `{protocol,variant,url,model,apiKey}` 随 `POST /triage` 的 `llm` 字段下发；sidecar 收到 `llm` 时不再读自身配置。**不回落** sidecar 自身 env（应用内配置为唯一事实源）；无可用 DB 目标时跳过 sidecar 直接走网关。

| # | 落点 | 说明 |
|---|---|---|
| 1 | `service/guidance_llm_spec.go`（新） | `GuidanceLLMSpec` + `GuidanceSpecResolver` 窄接口 + `buildGuidanceSpec`（协议/变体能力门）+ `(*LLMProviderRegistry).ResolveGuidanceSpec` |
| 2 | `service/llm_registry.go` | entry 预计算 `guidanceSpec`（URL 用 `resolveProtocolURL` + `adapter.GetAPIPath()` + `protocol.DefaultEndpoint`，与网关逐字节同口径） |
| 3 | `service/guidance_client.go` | `GuidanceTriageRequest` 增 `LLM *GuidanceLLMSpec json:"llm,omitempty"`；`Triage(...)` 增 spec 参数 |
| 4 | `service/triage_service.go` | `SetGuidanceSpecResolver` + `resolveGuidanceSpec`；未接线 = 旧行为（不下发、继续调 sidecar，前向兼容）；`ok=false`/error → 跳过 sidecar |
| 5 | `internal/bootstrap/app.go` | `triageService.SetGuidanceSpecResolver(providerRegistry)`（多 Provider 装配处） |
| 6 | `itsm-ai-service/api/guidance.py` | `GuidanceLLMTarget` + `llm` 字段；`_call_llm_override`；`/triage` 有 `llm` 走覆盖（**apiKey 禁入日志**，仅记 tenant/protocol/variant/url/model） |

能力门（sidecar 仅实现两种调用形态，其余返回 `ok=false` → 跳过 sidecar）：`openai_chat_completions`（default/ollama）、`anthropic_messages`（default/minimax）。

## 2. 本地验证（2026-10-18 复跑）

| 检查 | 结果 |
|---|---|
| `go test ./service/ -run 'Guidance' -count=1 -v`（GOMAXPROCS=2） | ✅ 26 项 PASS（含 `TestResolveGuidanceSpec_*` ×5、`TestTriage_Suggest_GuidanceSpecResolver_*` ×4、F1 回归 ×3）；`ok itsm-backend/service 2.756s`，EXIT=0 |
| `go test ./handlers/ai/ -run 'Triage' -count=1` | ✅ `ok itsm-backend/handlers/ai 1.680s`，EXIT=0 |
| `python -m py_compile itsm-ai-service/api/guidance.py` | ✅ EXIT=0 |
| 并发提交隔离 | ✅ `git diff c05cc49e HEAD` 对 F2 五个源文件改动数 = 0（`e82b9b94` PE-27/28 未触碰 F2 面） |

## 3. 部署（238 demo）

三步脚本 + 验证：`deploy238_f2_a.sh`（bundle → `deploy-demo-f2-c05cc49e`、`.env.demo` VERSION）→ `_b.sh`（**构建 backend + itsm-ai-service 双镜像**）→ `_c.sh`（up -d + 健康检查 + A/B 指纹）→ `_verify.sh`（运行验证）。

| 检查 | 结果 |
|---|---|
| 远端 HEAD | `c05cc49ecfdab2364c183a53c47c6dcf5f1cdfdf`（bundle 前置 `eb214870` 满足） |
| `.env.demo` | `VERSION=1.8.0-dev.c05cc49e` |
| 构建 | `itsm-ai-service:1.8.0-dev.c05cc49e` / `itsm-backend:1.8.0-dev.c05cc49e` 均 Built，`BUILD_EXIT=0`（ai-service 层缓存全命中） |
| 容器 | backend/worker/frontend/ai-service `Up (healthy)`，镜像均为 `*:1.8.0-dev.c05cc49e`；`itsm-init-demo` Exit 0 |
| `GET /api/v1/version` | `1.8.0-dev.c05cc49e` |
| A/B 指纹（镜像内 grep） | backend `sidecar LLM target`：旧 `eb214870` = **0** → 新 = **1**；ai-service `_call_llm_override`：旧（悬空镜像 `7f88650065d3`）= **0** → 新 = **2** |

## 4. 运行时验证（tenant 1 = `admin`，存在唯一 provider 行）

事实源（`llm_provider_configs`，tenant 1 默认实例）：

```
 id | tenant | name  | protocol                | variant | model               | endpoint_full_path | enabled | is_default | status
  1 |      1 | baidu | openai_chat_completions |         | deepseek-v4.1-flash | t                  | t       | t          | ok
      endpoint=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions
```

sidecar 自身 env（关键：**LLM_API_KEY 仍为空**，结果不可能来自 sidecar 自身配置）：

```
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=***   （空；已掩码）
LLM_PROVIDER=openai
LLM_MODEL=gpt-4o-mini
```

(1) `POST /api/v1/ai/triage` #1（VPN 无法连接）：`category=network, confidence=0.92, priority=high`，
`reasoning="VPN 连接超时属于网络接入类故障，影响华东区 30 人办公…"`（真实 LLM 输出）。
(2) `POST /api/v1/ai/triage` #2（Outlook 报错 0x8004010F）：`category=application, confidence=0.87, priority=high`，
`reasoning="Outlook 报错 0x8004010F 通常为脱机通讯簿(OAB)或 OST 配置文件损坏…"`。

sidecar 日志（两次调用各 1 行；URL/模型与 DB 行**逐字一致**，apiKey 未出现）：

```
2026-10-18 15:35:07 INFO Guidance triage using request-scoped LLM target tenant=1
  protocol=openai_chat_completions variant=None url=https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions model=deepseek-v4.1-flash
2026-10-18 15:35:10 INFO （同上）
override_log_lines=2  sidecar_llm_unavailable_lines=0  sidecar_post_triage_lines=2
```

backend 日志（侧车成功路径，非降级/回落）：

```
2026-10-18T15:35:10.216Z info TriageService: Guidance-constrained classification {"method": "guidance", "category": "network", "priority": "high", "confidence": 0.92, "latency_ms": 2857.82}
2026-10-18T15:35:11.668Z info TriageService: Guidance-constrained classification {"method": "application", "category": "application", "priority": "high", "confidence": 0.87, "latency_ms": 1246.94}
guidance_success_lines=2  degraded_lines=0
```

遥测（**F2 与 F1 的行为差异**）：`ai_llm_calls` 在两次分诊后 **new_calls_total=0**（F1 下每次分诊因 sidecar 降级都新增网关行；F2 下 sidecar 直连上游成功，网关不再被调用）。

结论：应用内（DB）配置成为 sidecar 的唯一事实源——同一 sidecar 进程（自身未配密钥）在两次演示分诊中都用**租户 1 的 DB 配置**直连 `qianfan.baidubce.com` 完成真实 LLM 分诊，且不产生网关遥测。

## 5. 负向对照：跨租户不下发（首次误用 `demo.mfg.admin` = tenant 3）

| 项 | 结果 |
|---|---|
| `/ai/triage` ×2（tenant 3） | `reasoning="keyword heuristic"`（HTTP 200，本地兜底） |
| sidecar 日志 | `override_log_lines=0`、无 `POST /triage` → **sidecar 未被调用**（无租户目标 → 跳过） |
| `ai_llm_calls` | 新增 2 行：`provider_key=<static>、model=gpt-4o-mini、success=f`（`api.openai.com ... connection refused`，静态 provider 无网/无密钥） |

说明：tenant 3 无任何 `llm_provider_configs` 行 → `ResolveGuidanceSpec` 返回 `ok=false` → 按设计跳过 sidecar；网关也解析不到租户实例、静态 provider 失败 → 落到本地关键词兜底。即：**租户 1 的配置没有被下发给其他租户**，且无 DB 目标的租户终态与 F1 一致（少一次无效 sidecar 往返）。

## 6. 遗留与跟进

1. **cosmetic**：sidecar 日志中 `variant=None`（空变体经 JSON 省略后为 `None`）；仅日志外观，无功能影响，未为此改动已通过验证的产物。
2. **可观测性**：`ok=false` 路径后端记 Debug（demo `LOG_LEVEL=info` 下不可见）→ 建议后续加指标/计数或提级为 Info（承接 F1「静默」教训的同类问题）。
3. **范围外**：sidecar 其它端点（`/api/v1/rca/*`、`/api/v1/risk/*`）仍读自身 env/config.yaml；本次仅 `/triage` 支持请求级下发。
4. 未做浏览器级 UI 走查（承接 F1 遗留）。
5. 回滚：`.env.demo` 改回 `VERSION=1.8.0-dev.eb214870` + `up -d`（旧镜像仍在；注意 sidecar 亦回滚为旧悬空镜像）。