# M0-05 单元级证据（SSRF 与出站安全）

> 验收项：A0-05（对应任务 M0-05）｜目标级别：`unit_verified`（负向用例全过，满足 M0-08 联调前置门槛）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，`go1.25.13 windows/amd64`，基线 `7442fad5`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 变更文件

- 新增 `itsm-backend/mcp/transport/ssrf.go`：
  - `SSRFGuard`（实现 `Guard` 接口）+ `SSRFConfig`（`AllowHTTP`/`AllowPrivate` 平台开关、`AllowedHosts`、`AllowedPorts`、可注入 `Resolver`、`Audit` 钩子）；
  - 校验链：协议（仅 https，http 需平台开关）→ 禁止 userinfo → 域名 allowlist → 端口 allowlist（默认仅 443/80）→ DNS 解析 → 受限地址段判定 → **rebinding 钉住比对**（首次解析结果钉住，后续必须是其子集）；
  - 受限地址：环回 / 未指定 / 组播 / 链路本地 / ULA / RFC1918 + 保留段表（CGNAT、TEST-NET、6to4、NAT64、基准测试段、`240/4` 等），IPv4-mapped IPv6 先解包再判定；
  - 失败统一 `ssrf_blocked`；错误文本不含 IP/主机名，解析到的 IP 仅进入 `Audit` 事件（供 M0-08 接审计设施）。
- 新增 `itsm-backend/mcp/transport/ssrf_test.go`：表驱动负向 24 例 + rebinding / 审计 / 集成 / 每请求校验 / 重定向禁跟随。
- 复用（M0-04 已交付）：`transport.New` 构造期校验；`headerRoundTripper` 每请求校验；`CheckRedirect=ErrUseLastResponse`。

## 执行记录

| # | 命令（itsm-backend 目录） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -l mcp\transport` | 无输出 |
| 2 | `go test ./mcp/transport/ -run TestSSRF -v` | 全部子用例通过（24 表驱动 + 5 个专项） |
| 3 | `go test ./mcp/... -count=1` | 全绿：`client 1.933s`、`registry 0.627s`、`transport 0.332s` |
| 4 | `go vet ./mcp/...` | `vet-exit=0` |
| 5 | `go build ./...` | `build-exit=0`（258s） |

## 覆盖说明（对照 M0-05 要点）

- **仅 https**：http 默认拒绝；`AllowHTTP` 平台开关放行（正/负用例各一）。
- **私网与保留段**：环回（IPv4/IPv6）、RFC1918（10/172.16/192.168）、链路本地与云元数据 `169.254.169.254`、ULA、IPv4-mapped 私网、CGNAT、基准测试段、未指定地址 —— 全部拒绝。
- **域名解析**：解析到私网拒绝；多记录中任一私网即拒绝；allowlist 命中子域放行、非命中拒绝。
- **端口 allowlist**：默认仅 scheme 端口（https 443 / http 80）；`https://...:8443` 默认拒绝、显式放行后通过。
- **DNS rebinding（核心）**：首次校验钉住解析结果 → 换成私网地址被拒（命中受限段）→ 换成**未钉住的公网地址同样被拒**（错误含 `rebinding`）→ 回到钉住集合子集放行（兼容多 A 记录轮换）。
- **每请求校验**：先钉住公网结果，再把解析器切换为私网；`RoundTripper` 在**发起网络请求前**返回 `ssrf_blocked`。
- **禁跟随重定向**：302 响应原样返回（`StatusFound`），重定向目标从未被请求（计数器断言）。
- **审计与不泄露**：`Audit` 钩子收到允许/拒绝事件（含 IP、目标已去 userinfo）；对外错误文本经断言不含 `10.0.0.5` 与主机名。
- **fail-closed 集成**：`transport.New` 缺 `Guard` → `invalid_transport`（M0-04 用例）；`SSRFGuard` 拒绝 → `ssrf_blocked` 且不产生任何请求。

## 未覆盖 / 待办（进入 M0-08 前必须闭环）

- **管理面强制调用**：M0-08 必须在「新增 / 编辑 / 测试连接」三条路径显式构造并传入 `SSRFGuard`（含平台开关与 allowlist 的配置来源）。
- **审计接线**：`Audit` 钩子尚未接入 ITSM 审计设施（`tool_invocations` / 审计日志），M0-08 一并落地。
- **解析器超时与自定义 DNS**：当前使用 `net.DefaultResolver`（受 ctx 约束）；自定义 DNS/DoH 未设计。
- **平台开关配置项**：`AllowHTTP`/`AllowPrivate`/`AllowedHosts`/`AllowedPorts` 的配置键与装配点未定【未核实】，须在 M0-08 与 `config` 打通后再视情况回写本文件。
