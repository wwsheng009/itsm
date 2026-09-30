# BP8 实施证据（预算与护栏参数落配置）

> 文档类型：实施证据（工程前置 BP8）
> Status: draft
> 编制日期：2026-09-27
> 前置项：BP8（方案 §2.2；B1-02 开工前）
> 关联文档：`docs/plan/ai-bot-capability-landing-implementation-plan-2026-09-27.md`（§2.2 BP8、§4.2 B1-02）
> 核查方式：真实 `config.LoadConfig`（隔离临时目录 + 全局 viper Reset）+ 表驱动 UT

## 1. 结论

| 项 | 判定 | 说明 |
| --- | --- | --- |
| BP8 | **完成（`unit_verified`）** | 六项参数全部落配置（YAML + 环境变量双通道），非正值/非法值一律 fail-safe 回落默认；`BotToolTimeout()` 统一秒→Duration 换算 |
| 阻塞 | 无 | B1-02 已在实现中直接消费本结构（`bootstrap` 侧映射到 `bot.Budget`） |

## 2. 配置面（新增）

```yaml
bot:
  enabled: false              # BP5 全局开关（既有）
  redaction_profile: default  # default | strict（未知值 → default）
  budget:
    max_steps: 24             # 每 run 步骤上限
    max_tokens: 100000        # 每 run token 上限（无 provider 回报时不参与判定）
    max_tool_calls: 12        # 每 run 工具调用次数上限
    tool_timeout_seconds: 30  # 单工具执行超时（BP8 明确默认 30s）
    max_output_bytes: 65536   # 单次工具输出字节上限
```

环境变量兜底（裸键或 `ITSM_` 前缀均可）：`BOT_REDACTION_PROFILE`、`BOT_BUDGET_MAX_STEPS`、`BOT_BUDGET_MAX_TOKENS`、`BOT_BUDGET_MAX_TOOL_CALLS`、`BOT_BUDGET_TOOL_TIMEOUT_SECONDS`、`BOT_BUDGET_MAX_OUTPUT_BYTES`。

## 3. 交付物

| # | 文件 | 说明 |
| --- | --- | --- |
| 1 | `itsm-backend/config/config.go`（修改） | `BotConfig` 扩展 `redaction_profile` + `budget`；`BotBudgetConfig` 五字段；`applyBotDefaults` 归一化（未知档位 → `default`；非正预算 → 硬默认）；`BotToolTimeout()` 秒→Duration；新增 `getEnvIntWithDefault`（非法/非正值忽略，保留配置值） |
| 2 | `itsm-backend/config/config_bot_test.go`（修改） | 新增 4 个用例：默认值、YAML+环境变量覆盖、非法值 fail-safe、`getEnvIntWithDefault` 表驱动 8 例（裸键/前缀/零负值/非数字/空白） |

## 4. 运行记录（本机，pwsh）

```text
go test ./config/ -count=1 -run 'TestLoadConfig_Bot' -v
→ PASS TestLoadConfig_BotSwitches / BotDefaults / BotBudgetDefaults
→ PASS BotBudgetFromYAMLAndEnv / BotBudgetInvalidFallsBack
go test ./config/ -count=1 -run 'TestGetEnvIntWithDefault' -v
→ PASS（8 个子用例）
go test ./config/ ./service/bot/ ./handlers/ai/ ./tests/botintegration/ -count=1 -timeout 25m
→ ok config 1.337s | ok service/bot 8.340s | ok handlers/ai 9.622s | ok tests/botintegration 5.107s   （exit 0）
go vet ./internal/bootstrap/ ./config/ → exit 0；gofumpt -l → 无输出
```

## 5. 语义边界（避免误用）

| 项 | 口径 |
| --- | --- |
| `max_tokens` | 仅在 provider 回报 token 用量时参与判定；当前主链路未接入回报（见 B1-02 证据 §5 遗留②），配置已就位 |
| 非法 YAML 值 | `${VAR:default}` 占位把非数字注入 YAML 会导致 `LoadConfig` **显式失败**（启动即暴露），不静默降级——这是既有配置层的 fail-loud 语义，本任务未改变 |
| 非法环境变量 | 数字型非法值被忽略（保留 YAML/默认值），不会把预算静默清零 |
| 上限方向 | 所有参数只收紧不放大：`<=0` 一律取默认，避免「0 = 无限制」的误读 |

## 6. 变更记录

| 日期 | 变更 | 说明 |
| --- | --- | --- |
| 2026-09-27 | 初稿 | BP8 交付：六项参数落配置 + 环境变量兜底 + fail-safe 归一化 + 9 个用例 |
