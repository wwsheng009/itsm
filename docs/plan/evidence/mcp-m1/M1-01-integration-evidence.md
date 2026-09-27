# M1-01 证据（工具分类标注全链路：read_only / risk / category）

> 验收项：A1-01（对应任务 M1-01）｜DoD：`integration_verified`
> 实际状态：**`integration_verified`**（真实 ent + SQLite + M0-13 mock MCP 的端到端装配：默认值/工具面切换/Gate2 映射/审计差异/写面入审）
> 执行人 / 环境：AI 辅助执行；Windows + pwsh，go1.25.13，基线 `621de6af`（分支 `feat/bot-mcp-integration`）
> 时间：2026-09-27

## 交付物

| 文件 | 说明 |
| --- | --- |
| `itsm-backend/tests/mcpintegration/m1_annotation_test.go`（新增，~320 行） | M1-01 集成用例 4 个：默认值 + 工具面切换 / Gate2 read↔write / 标注审计 before-after / 参数校验 |
| `itsm-backend/middleware/rbac.go`（**修复**） | `checkPermissionMatch` 增加 MCP 例外：`mcp:admin` 不蕴含 `mcp:read`/`mcp:write` |
| `itsm-backend/middleware/mcp_permission_matrix_test.go`（**语义更新**） | M0-10 锁定的「admin 有效可及 write」期望改为 `false`，并补齐 admin 不蕴含 read/write 的双向断言 |

## 真实缺陷（本轮修复，风险等级：高）

**现象**：`checkPermissionMatch` 的通用规则「资源内 `admin` 动作是超集」（rbac.go:1209-1211）使 `mcp:admin` 蕴含 `mcp:write`。
**影响**：租户管理员（`admin` 角色持 `mcp:read` + `mcp:admin`，按 Q2 拍板**不**持 `mcp:write`）在 Gate2 静默获得写工具执行权：
授权面（`internal/authz/mcp_roles_test.go` 断言 admin 不持 mcp:write）与判定面矛盾，D7「默认拒绝」与「使用/治理分离」被绕过。
**发现路径**：M1-01 集成用例断言 `role=admin` 调写工具应被拒（`lacks mcp:write`），实测通过 → 反向定位到匹配器语义。
**修复**：`resource != "mcp"` 例外（仅 MCP；其余资源语义不变，有 `TestResourceAdminImplicationUnchanged` 同源断言）。
**回归**：M0-10 矩阵测试同步更正期望（原期望记录了当时的实际行为，非拍板意图），并新增 admin 不蕴含 read/write 断言。

## 用例与断言（`TestM1Annotation_*`）

| 用例 | 断言 |
| --- | --- |
| `DefaultsAndFaceSwitching` | ① D7 默认值：`read_only=false`、`risk=high`、`category=""`、`enabled=false`、未隔离；② 未标注只读的写工具**不在**只读工具面（`GetToolForTenant=nil`）；③ 标注只读但未启用 → 仍不在面（两道闸）；④ 启用后**立即**进面且 `Resource=mcp`/`Action=read`（无会话冻结）；⑤ 改回写标注 → **立即**离面，且 `ExecuteTool` 返回 `ErrUnknownTool` |
| `Gate2ReadVsWrite` | ① 只读工具：`agent`（无 mcp:read）拒绝且错误文本为 `lacks mcp:read`；`admin` 通过，`pendingID=0`（免审批直通）；② 写面（`IncludeWriteTools=true`）：写工具解析为 `Action=write`、`ReadOnly=false`；③ `admin` 调写工具被拒（`lacks mcp:write`，修复后行为）+ 拒绝留痕 + **不产生 pending**；④ `sysadmin` 调写工具 → `pendingID>0`，落库 `status=pending`/`needs_approval=true`/`approval_state=pending`/`role_snapshot=sysadmin` |
| `ClassificationAuditBeforeAfter` | 两次标注（写→只读→写）→ `audit_logs(resource=mcp, action=set_tool_classification)` 恰好 2 条且按序：`object_type=mcp_tool`、`object_id=callable`、`result=success`；`before/after` 分别体现 `read_only=false→true`（含 `risk=act_high`、`category=change`）与 `true→false`；before≠after（无差异不落审计） |
| `InvalidRiskRejected` | 非法 risk（`super-danger`）与超长 category（33 字符）→ 400 `validation_failed`；**零审计**；工具行未被部分修改（`risk` 仍 `high`、`category` 仍空） |

## 执行记录

| # | 命令（workdir=`itsm-backend`） | 结果 |
| --- | --- | --- |
| 1 | `gofmt -w ./tests/mcpintegration ./middleware` | 无 diff |
| 2 | `go test ./tests/mcpintegration/ -run TestM1Annotation -count=1` | 4/4 通过 |
| 3 | `go test ./middleware/ -run TestMCP -count=1` | 4/4 通过（含更正后的矩阵与分离断言） |
| 4 | `go test ./middleware/ ./handlers/ai/ ./tests/mcpintegration/ -count=1 -timeout 600s` | 三包全绿（8.2s / 18.2s / 13.5s） |
| 5 | `go vet ./tests/mcpintegration/` / `go build ./...` | exit 0 |

## 设计与口径说明

1. **两道闸（默认拒绝）**：进只读工具面 = `read_only=true` **且** `enabled=true` **且** `healthy` **且** 未隔离。仅标注只读不启用仍不可执行（用例③）。
2. **Action 由标注派生**：`provider.actionFor(readOnly)` → `read|write`；Gate2 用 `Resource/Action` 查权限 → MCP 码空间 `mcp:read`/`mcp:write`。
3. **无会话冻结**：标注变更后**下一轮解析**即生效（ITSM 结构性优势，分析报告 §6.3）；写入路径的审批冻结属 M1-02。
4. **risk 一期仅展示/统计**，不参与审批决策（二期接入 Bot 风险上限 G2）；本任务锁定的默认值 `high` 是 D7 的保守落点。
5. **MCP 例外仅限匹配器**：`authz` 授予面、`middleware` 兜底表、DB 角色表三处的 `mcp:*` 码集合不变；仅「admin 蕴含」这一条通用捷径被 MCP 排除。

## 遗留 / 待办

- 写路径的**审批 → 执行 → 回填**链路属 M1-02（本任务仅断言「进入 pending」与「权限拒绝不产生 pending」）。
- 审批详情携带来源三元组 + risk（A1-02 的「信息缺失下决策」问题）同样在 M1-02 落地。
