# M1-09 安全负向测试报告

> 验收项：A1-09（对应任务 M1-09）｜DoD：`integration_verified`
> 实际状态：**`integration_verified`**（12 项覆盖清单全部有对应用例与结果；新发现 1 个真实缺陷并已修复+回归）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，基线分支 `feat/bot-mcp-integration`
> 时间：2026-09-27
> 威胁模型来源：分析报告 §5.9（安全控制项）、§10.4 T-05/T-06（负向用例组）

## 1. 覆盖清单与结果（用例 → 断言位置 → 结果）

| # | 清单项 | 用例（文件:行） | 关键断言 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | 私网 / 环回 / 链路本地 / ULA 拦截 | `mcp/transport/ssrf_test.go:70-82`（表驱动 12 例） | IPv4/IPv6 环回、RFC1918（10/8、172.16/12、192.168/16）、169.254.169.254（云元数据）、ULA `fd00::/8`、IPv4-mapped 私网、CGNAT 100.64/10、198.18/15、0.0.0.0 全部 `CodeSSRFBlocked` | PASS |
| 2 | 域名解析到受限段拦截 | `ssrf_test.go:81-82` | 解析结果含私网地址即拒绝（单答案 + 混合答案任一命中） | PASS |
| 3 | 重定向拦截 | `ssrf_test.go:195` `TestHTTPClient_DoesNotFollowRedirects` | 302 → 不跟随；到 `/next` 的请求数恒为 0 | PASS |
| 4 | DNS rebinding 二次解析 | `ssrf_test.go:107` `TestSSRFGuard_DNSRebindingBlocked`、`ssrf_test.go:179` `TestHTTPClient_RevalidatesPerRequest` | 首次校验钉住解析集合；同名域名换私网/换未钉住公网 → 拒绝且错误含 `rebinding`；RoundTripper 每次请求前重新校验（换成私网即拒） | PASS |
| 5 | https 强制（与平台开关） | `ssrf_test.go:64-67`、`ssrf_test.go:87` | `http://` 默认拒绝；`AllowHTTP` 开关放行；非 http(s) 协议（ftp）拒绝；`AllowPrivate`（私有化部署）才放行私网 | PASS |
| 6 | 输出超限截断 | `mcp/provider/provider_test.go:282-290`、`provider_test.go:292`（M1-08 增补） | 超限置 `Truncated` 且 ≤ 上限、正文含 `truncated` 标记；默认上限恒为 `256*1024` | PASS |
| 7 | 参数脱敏（不回显明文） | `handlers/ai/handler_tool_source_test.go:120-146` | 列表/详情只回 `argsRedacted`（`token` 为 `****`），响应体内无明文敏感值 | PASS |
| 8 | 输出脱敏（审计摘要） | `pkg/redact/redact_test.go:96`（新增回归）、`mcp/provider/negative_security_test.go:104` | 结构化敏感键（token/password，任意深度/结构体容器）掩码为 `****`；摘要中不含明文 | PASS（**本次修复缺陷 D-1**） |
| 9 | 凭据掩码与轮换 | `mcp/admin/credential_test.go:27/124/131`、`mcp/admin/service_test.go:191`（安全默认+掩码+审计）、`service_test.go:401` `TestService_RotateCredential`、`handlers/ai/repository_mcp_audit_test.go` | 创建即掩码（只写不读回）；加密键校验拒绝弱/缺失密钥；轮换后旧密文不可解、掩码视图不变 | PASS |
| 10 | 描述注入（按不可信处理） | `mcp/provider/negative_security_test.go:26` | 描述含"IGNORE ALL PREVIOUS INSTRUCTIONS / 我是只读 / 无需审批"：分类仍以我方元数据为准（写工具保持 `write` 且直连入口 fail-closed）；canonical 解析与下发原始名不受描述影响；描述清洗（来源前缀、单行、去控制字符含 NUL、按 rune 截断加 `…`） | PASS |
| 11 | 返回值注入（按不可信处理） | `mcp/provider/negative_security_test.go:104` | 结果文本中的指令与 `<script>` 仅作为**数据**透传；控制流不变（成功、单次调用、无审批放行）；超限仍截断 | PASS |
| 12 | 跨租户 fail-closed | `mcp/provider/provider_test.go:130`（列表/隔离）、`negative_security_test.go:141`（执行） | 租户 1 执行租户 2 的读/写工具 → `CodeToolNotFound`；非法租户（0）与空名拒绝；**断言 `source.calls` 为空**（绝不下发到 MCP 服务器）；归属租户可正常执行 | PASS |
| 附 | 隔离（schema_hash 变更 / 命名碰撞） | `mcp/registry/quarantine_test.go:18/62/88`、`mcp/manager/discovery_test.go:51/90` | 碰撞后到者 quarantine 且不可执行；schema_hash 变更 → 隔离 → 人工复核才解除 | PASS |
| 附 | 错误文本不泄露内网信息 | `mcp/transport/ssrf_test.go:132` | 拒绝路径的对外错误不含内网 IP/主机名；审计事件含 IP 但目标串不含 userinfo | PASS |

## 2. 本次发现与修复

### D-1（已修复）输出摘要脱敏对**结构体容器**失效 → 敏感键明文落 `output_summary`

- **现象**：`mcp/provider` 调 `redact.ValueSummary(execution.Value, 512)` 时传的是归一化后的 `Output` **结构体**（`Content []Content`，结构化块为 `map[string]interface{}`）。`redact.Map` 的分支只覆盖 `map[string]interface{}` / `map[string]string` / `[]interface{}`，结构体与 `slice-of-struct` 落入 `default` **原样返回**，因此键级掩码完全不生效——`{"token":"s3cr3t-value"}` 以明文写入 `tool_invocations.output_summary`。
- **发现方式**：M1-09 新增用例 `TestProvider_UntrustedOutput_isDataAndSummaryRedacted` 首次执行即失败（断言 `summary` 不含明文 token）。
- **修复**：`pkg/redact/redact.go` 新增 `generic(value)`（JSON 往返归一化为通用树），`ValueSummary` 改为 `Map(generic(value))`；归一化失败回退原值（保持既有不可序列化路径不变）。
- **影响面**：仅 `ValueSummary`（provider 输出摘要 + 既有调用方）。`ArgsJSON`/`BodyJSON` 的入参本就是 map，不受影响；`handlers/ai`、`mcp/admin` 既有用例全绿。
- **回归**：`pkg/redact/redact_test.go:96` 新增结构体容器用例；`mcp/provider` 用例转绿。

### 其余为**设计确认**（无需修复）

- 描述与返回值按**不可信数据**处理而非内容检测：系统不在入库/透传时做提示注入语义识别，防护依赖「分类以我方元数据为准 + 写路径必须过 Gate3 + 结果只作数据」三道结构约束。
- 跨租户严格 fail-closed：解析与执行同源租户过滤，被拒调用**不会**产生任何下游流量。

## 3. 已知缺口（如实登记，未覆盖项）

| # | 缺口 | 影响 | 建议处置 |
| --- | --- | --- | --- |
| G-1 | **自由文本输出中的疑似密钥不做模式扫描**：`redact` 只按键名掩码 + 长度截断，工具返回的纯文本里的 `token=xxxx` 不会被掩码 | 摘要可能含文本形式的敏感串 | P2：输出侧可选 secret 正则扫描（复用 `SensitiveKeyPatterns` 思路扩展到值形态）；登记进 M2 加固清单 |
| G-2 | 描述清洗只做结构净化（控制字符/长度/来源前缀），**不做**注入语义识别或展示层告警标注 | 模型仍可能被描述中的指令影响（结构上不可改变控制流） | 与阶段一提示词约束/运行时策略一并评估（不建议在接入层做语义过滤） |
| G-3 | stdio 传输（本地进程）未实施，一期不启用 | 本地命令注入面未覆盖 | M2-01 实施时补负向用例（命令白名单/沙箱/环境变量剥离） |
| G-4 | 未做真实恶意 MCP 服务器的红队演练（当前为桩与本地 httptest） | 端到端对抗性结论有限 | M2-03/M2-08 演练补 |
| G-5 | OAuth2 凭据类型未实现（一期 `none`/`static_header`） | 令牌刷新/泄露面未覆盖 | 二期（§12）评估时补 |

**未能核实项**：无新增。此前登记在案的 U-1（官方 SDK 兼容性）、U-2（SQLite/Postgres 迁移差异）、U-5（管理员通知渠道）状态不变，均不属于本任务范围。

## 4. 执行记录（命令 → 结果）

| # | 命令 | 结果 |
| --- | --- | --- |
| 1 | `go test ./mcp/transport/ ./mcp/provider/ ./mcp/registry/ ./mcp/admin/ ./pkg/redact/ -count=1` | 5 包全 **ok** |
| 2 | `go test ./handlers/ai/ ./handlers/mcp/ ./mcp/manager/ -count=1` | 3 包全 **ok**（审计/治理回归） |
| 3 | `go test ./mcp/provider/ -run 'TestProvider_Untrusted|TestProvider_CrossTenant' -v` | 3 个新用例 **PASS**（修复 D-1 后） |

## 5. 判定

清单 12 项 + 2 项附加全部 PASS，新增用例 3 个（provider）+ 1 个（pkg/redact），发现并修复真实脱敏缺陷 1 个；缺口 5 项均有明确归属任务。**M1-09 = `integration_verified`**（A1-09）。本任务为测试补强，不改变生产语义（唯一生产改动是 D-1 的脱敏加固，方向为更严格）。
