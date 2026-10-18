# 238 demo 修复证据：项目 AI 助手降级（2026-10-18，1.8.0-dev.d80cf753）

## 现象

`/projects/1` 的「AI 助手」三端点均返回：
`AI 调用失败，已降级为确定性输出；可稍后重试`（`generatedBy=deterministic`，`degraded=true`）。

## 根因

- 租户 DB provider（tenant 1，default）：`baidu` / `openai_chat_completions` / 模型 `deepseek-v4.1-flash` / 千帆 Token Plan Personal；连通性测试 `status=ok`。
- 但 `ProjectAIAdapter.chat` 把**静态 env `LLM_MODEL`（gpt-4o-mini）**传给 `LLMGateway.Chat`；`OpenAIProvider.Chat` 在 model 非空时覆盖自身模型 → 请求实际使用 `gpt-4o-mini`。
- 千帆返回：`403 The current model does not support Token Plan Person`。
- 对比：`bpmn_ai_generator_service.go` 早已采用「传空模型，委派 provider 选择」的修复；项目 AI 漏掉了同一处理。

## 修复

1. `ProjectAIAdapter.chat`：改传空模型，使用 `gateway.ChatWithRequest` 按租户/用户解析链选择 provider；不再覆盖其配置模型。
2. `ProviderResolution` 新增 `Model`（来自 `ProviderSlot.Model`）；适配器记录最近一次实际生效模型，响应 `model` 字段展示真实值（静态回退仍回落到 `LLM_MODEL`）。
3. `ai_llm_calls` 遥测：空模型委派时记录解析后的实际模型（非流式 + 流式路径）。
4. 新增回归单测 `TestProjectAIAdapterDelegatesModelToProvider`：断言适配器传空模型且静态回退显示构造参数。

提交：`7b383714`（主修复）、`d80cf753`（遥测模型）。

## 部署

| 项 | 结果 |
|---|---|
| 版本 | `1.8.0-dev.d80cf753` |
| backend/worker/frontend | 均 healthy；init ExitCode=0 |
| `/api/v1/version` | 对齐 |
| 健康检查 | `/` 200、`/api/v1/health` 200 |

## 验证（真实 API，admin cookie + CSRF）

| 端点 | 结果 |
|---|---|
| `POST /projects/1/ai/summary` | `generatedBy=ai`、`degraded=false`、`model=deepseek-v4.1-flash`、360+ 字 |
| `POST /projects/1/ai/risk-warnings` | `generatedBy=ai`、`degraded=false`、`model=deepseek-v4.1-flash`、warnings=6 |
| `POST /projects/1/ai/minutes/tasks` | `generatedBy=ai`、`degraded=false`、`model=deepseek-v4.1-flash`、proposals=2 |

`ai_llm_calls` 最近成功行（修复后）：

```
provider=openai_chat_completions provider_key=baidu model=deepseek-v4.1-flash success=t latency=1206/9796/5733ms
```

修复前失败行：`model=gpt-4o-mini success=f`（403）。修复后 10 分钟内无 `project ai degraded` 日志。

### 浏览器验证（Tabbit，`/projects/1` → 「AI 助手」）

| 项 | 结果 |
|---|---|
| 点击「生成摘要」 | 返回 LLM 摘要（项目周报结构，约 360 字） |
| `generatedBy` 标签 | `AI`（绿色） |
| 模型标签 | `模型：deepseek-v4.1-flash` |
| 降级提示 | 未出现（无「已降级为确定性输出」） |

## 检查

- `go test -p 1 -count=1 ./handlers/project/` 全绿（含新增适配器测试）
- `go test -p 1 -count=1 -run "TestLLMGateway|TestGateway" ./service/` 全绿
- `./service/`、`./internal/bootstrap/` 编译通过
