# M0-06 单元级证据（凭据加密与掩码）

> 验收项：A0-06（对应任务 M0-06）｜目标级别：`unit_verified`
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- 新增 `itsm-backend/mcp/admin/credential.go`：
  - `SecretValues`：敏感键值集合（不导出内部 map；实现 `String`/`GoString` 阻断日志/审计/事件误打印；`Masked()` 为管理 API 唯一读形态）；
  - `CredentialService`：`Encrypt`（JSON → AES-GCM → base64；空集合 → 空串）、`Decrypt`（仅供连接装配层）、`Masked`、`ApplyPatch`（**空值 = 不修改**，轮换入口）；
  - `ResolveEncryptionKey`：`MCP_ENCRYPTION_KEY` 优先；生产缺失返回错误（调用方启动期 Fatal）；非生产派生 `mcp-key-<jwt>`（`derived=true`，调用方 Warn）；
  - `MaskSecret`：长度 ≥12 保留前 4 后 2，否则 `****`。
- 新增 `itsm-backend/mcp/admin/credential_test.go`（11 个用例，含 enttest 落库断言）。
- 依赖复用：`middleware.EncryptionService`（AES-GCM，与 connector / LLM provider 同源）；语义对齐 `connector.NewPersistentConfigStore`（≥16 字符密钥、空凭据不覆盖）。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp\admin` | 无输出 |
| 2 | `go test ./mcp/admin/ -count=1 -v` | 11 个用例全过（`ok itsm-backend/mcp/admin 2.084s`） |
| 3 | `go test ./mcp/... -count=1` | 全绿：`admin 2.038s`、`client 2.523s`、`registry 0.506s`、`transport 0.537s` |
| 4 | `go vet ./mcp/...` | `vet-exit=0` |
| 5 | `go build ./...` | `build-exit=0`（本轮） |

## 覆盖说明（对照 M0-06 测试与证据要求）

- **加密落库断言（DB 中无明文）**：`enttest` 起 sqlite，写 `mcp_servers.credential_encrypted` 与 `headers_encrypted`；用原生 SQL 读回断言不含完整 token、不含明文子串；解密回环得到原值。
- **掩码 API 断言**：`Masked()` 投影（`Bearer super-secret-token-123` → `Bear****23`；短值 → `****`）；掩码结果不含完整密钥。
- **轮换后旧凭据失效**：`ApplyPatch` 以新值覆盖后重新加密——密文变化（随机 nonce）、新值可读、旧值不再被引用；换主密钥或篡改密文解密直接失败（GCM 完整性）。
- **生产模式缺密钥启动失败**：`ResolveEncryptionKey(jwt, true)` 在 env 为空时返回错误（错误文本含 `MCP_ENCRYPTION_KEY`）；非生产回退派生密钥并标 `derived=true`。
- **空值 = 不修改**：patch 空白值保留原值、新增键正常合并、原集合未提及的键保持不变（与 connector 更新语义一致）。
- **防误打印**：`fmt.Sprintf("%v"/"%s"/"%#v", values)` 均不含明文且含 `redacted` 标记。
- **密钥长度约束**：`NewCredentialService` 拒绝 <16 字符（含纯空白），与 connector 一致。

## 未覆盖 / 待办（M0-08 接线项）

- **启动装配**：`ResolveEncryptionKey` → 生产 `Fatal` / 非生产 Warn、服务实例注册与路由注入属 M0-08（本卡仅提供可测单元）。
- **双栏策略**：`headers_encrypted`（自定义头）与 `credential_encrypted`（`credential_type` 对应凭据）在 CRUD 中的写入/清空/删除语义由 M0-08 定义；本卡提供加密原语与合并规则。
- **主密钥轮换（换钥匙 + 存量重加密）**：未实现【未核实，建议二期】；当前仅支持凭据值轮换。
- **连接层解密消费**：M0-07 manager 通过 `HeaderProvider` 注入时调用 `Decrypt`（明文不得进入日志/事件）。
