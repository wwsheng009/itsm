# V2-01 个性化记忆（常用语 / 个人模板 / 上次处理方式）· 证据

- 批次：V2-01（方案 §7「V 系列」/ §11.4-3）
- 日期：2026-10-18
- 状态：`integration_verified`（前后端一体；含租户策略开关与三档合规）
- 原则：**元数据优先、不采集对话正文**（§13.1）；常用语/模板 = 用户**显式保存**（可随时删除）；
  合规三档 `on/anonymous/off`；写边界不变（记忆只填充输入/建议，**绝不自动发送或提交**）。

## 1. 交付摘要

### 1.1 数据面（后端）

| 面 | 内容 |
|---|---|
| 个人记忆存储 | `users.preferences.assistantMemory`（白名单扩展，复用 IP-P1-6c 端点 `GET/PUT /api/v1/users/me/preferences`，仅本人可读写） |
| 结构护栏 | `validateAssistantMemory`：字段白名单（version/phrases/templates/recent）+ 条数/长度上限 + `recent.key` 结构化字符集（`^[a-z0-9_.:-]+$`）+ `at` 必须合法时间；**非法整体拒绝 400**（不静默裁剪）；上限：phrases ≤20×200 字、templates ≤10（名 ≤40 / 体 ≤1000）、recent ≤20×120 |
| 租户策略开关 | M2 能力源新增 `assistant.memory_enabled`（缺行 = 跟随静态默认**开启**；管理台覆盖行即时生效）；`GET/PUT /system-configs/ai-capabilities` 增加 `assistantMemoryEnabled`（生效值/默认值/键清单 4 键）；管理台「系统配置 → AI 配置」新增第 4 行开关 |
| 员工侧策略下发 | `GET /api/v1/capabilities` 新增 `assistant.memoryEnabled`（bootstrap 注入同一 `capability.ConfigSource`；未注入/读取失败 → **默认开启** fail-safe，不阻断） |

### 1.2 前端

- `src/lib/ai/memory/types.ts`：模型 + 纯函数（`normalizeAssistantMemory` 与后端同口径裁剪；`recordRecentAction` 非法键静默忽略）。
- `src/lib/ai/memory/assistant-memory-store.ts`：模式矩阵 `resolveAssistantMemoryMode`（`on→server` / `anonymous→local` / `off`）
  + 数据面 IO：server = 偏好端点读写（本机镜像兜底）；local = 仅 localStorage；off = **零读写**；
  乐观保存 + 失败如实置错（`sync_failed` / `save_failed`）。
- `src/lib/ai/memory/use-assistant-memory.ts`：懒加载 hook（首次挂载解析；off 不渲染决策交给组件）。
- `src/components/ai/dock/AssistantMemoryChips.tsx`：Dock 输入框上方记忆条——
  chips（常用语/模板）**点击只预填输入框**；「存为常用语 / 存为模板」显式保存；管理面含**最近处理方式（纯元数据）**、用户级关闭（可重新开启）、清空记忆；匿名档明示「仅存本机，不上传」。
- `AssistantChatPanel.tsx` 接线（与 pendingPrompt 相同的聚焦路径，只填不发送）。
- `use-inline-apply.ts`：应用成功记录 `inline_apply:<scope>` 元数据动作键（**仅在记忆已就绪时写入**，未加载不隐式请求）。
- 管理台：`AICapabilitiesCard` 第 4 行「个性化记忆（租户策略）」；i18n zh/en 键 `mcp.capabilities.assistantMemoryEnabled(Hint)`。
- 埋点：新增 `memory_updated`（前端字典 + 后端白名单 + 方案 §13.2 + 契约测试四处同步）；
  chips 展示/点击复用 `suggestion_shown/clicked`（`cardId=memory`，零字典变更的部分照旧优先复用）。

## 2. 复跑命令

```powershell
# 前端（itsm-frontend/）
npx jest src/lib/ai/memory src/components/ai/dock/__tests__/AssistantMemoryChips.test.tsx --coverage=false --watchAll=false --runInBand --silent
npx jest --testPathPattern "system-config" --coverage=false --watchAll=false --runInBand --silent
npx jest src/components/ai src/lib/ai src/lib/api --coverage=false --watchAll=false --runInBand --silent
npx tsc --noEmit
npx eslint <16 个改动文件>   # 详见落地时的命令清单（0 error）

# 后端（itsm-backend/）
gofmt -l -w capability/capability.go capability/runtime.go handlers/capability/handler.go handlers/systemconfig/ai_capabilities.go handlers/common/user_preferences.go internal/bootstrap/app.go handlers/ai/telemetry.go
go test ./capability/ ./handlers/capability/ ./handlers/systemconfig/ ./handlers/common/ ./handlers/ai/ -count=1
```

> 无新增路由/权限码 → 免 `authz-gen`（precheck 地图不受影响）。

## 3. 实测结果（2026-10-18）

| 门禁 | 结果 |
|---|---|
| 记忆 lib + chips（3 suites） | **24 tests** 全绿（types 9 / store 9 / chips 6） |
| system-config 域（卡片/区块/API） | **3 suites / 19 tests** 全绿 |
| AI 表面回归（`components/ai` + `lib/ai` + `lib/api`） | **125 suites / 1641 tests** 全绿（字典契约测试同步后） |
| 后端 5 包（capability / handlers/capability / handlers/systemconfig / handlers/common / handlers/ai） | 全绿（含新增用例：记忆覆盖与恢复默认、记忆结构护栏、能力面下发、systemconfig 四键契约） |
| `npx tsc --noEmit` | exit 0 |
| `npx eslint`（16 个改动文件） | exit 0 |

## 4. 关键断言（负向与边界）

- **零请求**：埋点 off 或用户级禁用 → 能力面（`getCapabilities`）与偏好面（`getUserPreferences`）都不触达（单测负向断言）。
- **租户策略关闭**：能力面一次判定后 → 偏好面零读写、记忆条不渲染。
- **anonymous 不上传**：仅在 localStorage 读写，`updateUserPreferences` **绝不被调用**。
- **未加载不隐式请求**：`recordAction` 在 store idle/未就绪时静默跳过（ticket 页的应用动作不会意外触发加载）。
- **正文永不入记忆**：`recent.key` 非法字符集（含中文自由文本）本地与服务端双重拒绝；未知子字段服务端整体拒绝。
- **失败如实**：服务端读失败 → 本机缓存 + `sync_failed` 提示；写失败 → 本机兜底 + `save_failed` 提示（不静默丢数据）。
- **只填不发送**：chips 点击只调 `onInsert`（预填 + 聚焦），无任何发送/提交调用。

## 5. 边界与条件项（诚实标注）

1. **真库/真租户冒烟归联调**（本机 Docker daemon 未运行）：`system_configs` 覆盖行写入 / RLS / 多实例缓存收敛沿用 M2 既有条件项。
2. **「上次处理方式」当前采集点** = Inline Apply（工单分诊应用成功）；批量分诊应用、助手快捷动作等更多动作点随后续批次接入（`recent` 结构已就绪）。
3. **UI 文案未 i18n**：记忆条位于 Dock 面板（该面板现有文案为中文硬编码），与面板现状一致；管理台开关已 zh/en。
4. **未纳入 axe 扫描面**（I2-02 扫描面仍为面板/诊断/建议条）；记忆条为纯 Tag/Button 结构，后续专项补扫。
5. **多端并发**：服务端偏好为「最后写入覆盖」（无冲突合并；上限/结构校验兜底）；写入方含 `recent` 追加属低频。
6. **策略读取失败 fail-safe**：按产品默认（开启）处理，绝不因此阻断对话链路。
7. **默认值**：`assistant.memory_enabled` 静态默认开启（`bootstrap` 显式 `true`）；管理台可覆盖关闭，`reset` 恢复默认。

## 6. 关联

- 方案：§7 V2-01 行、§11.4-3、§13.1（元数据优先/合规）、§13.2（`memory_updated`）
- 复用：M2 能力开关（`capability.ConfigSource`）、用户偏好端点（IP-P1-6c）、O0-01 埋点 SDK、I1-01 Inline Apply
- 前序证据：`V1-01-batch-ai-evidence.md`、`V1-02-employee-entry-coverage-evidence.md`、`V1-03-intent-interpretation-evidence.md`