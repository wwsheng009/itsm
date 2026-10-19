# T5-05 灰度开关证据（assistant.dock.enabled / help.agent.enabled）

- **状态**：已完成（2026-10-19）
- **任务**：方案 §14.2 **T5-05**
- **范围**：租户级运行时灰度开关（平台配置 + 免重启 + 前端消费 + 帮助数据面门禁）
- **关联文档**：方案 §3.7 L118/L120、§6 L270、§14.2；交付台账 T5 段
- **提交**：随本文件同一提交（`feat(ai): T5-05 助手灰度开关`）

---

## 1. 验收对照

| 验收要点 | 结果 | 证据 |
|---|---|---|
| `assistant.dock.enabled` 平台配置 | ✅ 能力键加入 `capability` 单一来源；GET/PUT `/system-configs/ai-capabilities` 可读写/恢复默认 | `capability.go`；`ai_capabilities.go`；配置卡 |
| `help.agent.enabled` 平台配置 | ✅ 同上；系统配置 → AI 配置新增两个开关行 | `AICapabilitiesCard.tsx` |
| 免重启生效 | ✅ 写入后 `Invalidate(tenant)`，同实例下一次读取立即生效；多实例 ≤30s TTL 收敛 | `capability.Update`；`capability_test.go` |
| 普通用户可感知 | ✅ 公共能力面 `GET /api/v1/capabilities` 下发 `assistant.dockEnabled/helpEnabled`（无需 `system_config:read`） | `handlers/capability/handler.go`；hook |
| Dock 关闭生效 | ✅ 租户关闭后 `AssistantDock` 不渲染（含 FAB），主动协助信号聚合同步关闭 | `AssistantDock.tsx`；Dock 测试 |
| Dock 关闭后的预填入口 | ✅ 划词/报错解释/全局搜索等预填入口自动降级到 `/ai/chat`，工作区消费 `pendingPrompt`（只填不发） | `AssistantDock.tsx`；`AIChat.tsx`；Dock 测试 |
| 帮助关闭生效 | ✅ 面板隐藏帮助页签并回到对话；`/ai/help/search`、`/ai/help/page` 返回 403 | `AssistantChatPanel.tsx`；`help.go`；后端测试 |
| fail-safe | ✅ 能力源未注入/读取失败/字段缺省 → 默认开启，升级零行为变化 | `parseAssistantCapabilityPolicy`；hook 测试；后端测试 |

---

## 2. 实现清单

### 2.1 后端

| 文件 | 改动 |
|---|---|
| `capability/capability.go` | 新增 `KeyAssistantDockEnabled` / `KeyHelpAgentEnabled`；`Defaults` / `Snapshot` / `Patch` 扩展；load/update/clear/known keys/description 全链路 |
| `internal/bootstrap/app.go` | 静态默认均为 `true`（缺行跟随默认，升级零行为变化） |
| `handlers/capability/handler.go` | 公共能力面 `assistant` 新增 `dockEnabled` / `helpEnabled`；未注入源时默认 `true` |
| `handlers/systemconfig/ai_capabilities.go` | 管理端点请求/响应/defaults 扩展；PUT 支持两键三态与 reset |
| `handlers/ai/help.go` | `help.agent.enabled=false` 时 `SearchHelp` / `GetPageHelp` 返回 403；未注入源 fail-safe 开启 |

### 2.2 前端

| 文件 | 改动 |
|---|---|
| `lib/api/capability-api.ts` | `assistant` 类型新增 `dockEnabled?` / `helpEnabled?` |
| `lib/ai/use-assistant-capability-policy.ts` | 新增策略 hook：读取公共能力面、字段缺省/失败 fail-safe、`enabled=false` 零请求 |
| `components/ai/dock/AssistantDock.tsx` | 消费 `dockEnabled`：关闭时不渲染入口；`helpEnabled` 传给面板；关闭时主动信号零轮询 |
| `components/ai/dock/AssistantChatPanel.tsx` | 新增 `helpEnabled` prop；关闭时隐藏帮助页签并收回帮助视图 |
| `components/ai/AIChat.tsx` | Dock 关闭时消费 `pendingPrompt`，承接预填入口降级（只填不发） |
| `lib/api/system-config-api.ts` | 能力键/默认值/补丁/快照类型扩展 |
| `pages/(main)/admin/system-config/AICapabilitiesCard.tsx` | 新增两个开关行，支持脏字段保存与恢复默认 |
| `pages/(main)/admin/system-config/AICapabilitiesSection.tsx` | 页面说明补充 Dock / 帮助智能体灰度 |
| `lib/i18n/translations.ts` | `mcp.capabilities.*` zh/en 各新增 4 键（标签 + 说明） |

---

## 3. 行为口径

| 场景 | 行为 |
|---|---|
| 缺行 / 读取失败 / 未注入能力源 | 开启（保持既有行为，不阻断主链路） |
| `assistant.dock.enabled=false` | 不渲染常驻 Dock（含 FAB、面板、主动协助轮询）；预填入口自动跳转 `/ai/chat` 并由工作区填词；工作区与既有深链不受影响 |
| `help.agent.enabled=false` | Dock 面板只保留对话页签；帮助数据面返回 403；覆盖/待办等运营端点不在此开关范围 |
| 管理台保存 | 当前实例立即失效缓存；其他实例 ≤30s TTL 收敛；前端下一次能力读取/刷新后应用 |
| 恢复默认 | 删除覆盖行，回到静态默认 `true` |

> 本期为**租户级**开关；方案 §3.7 提到的角色/页面白名单未在本任务实现，仍为后续增强。

---

## 4. 验证

### 4.1 后端（workdir `itsm-backend`）

| 命令 | 结果 |
|---|---|
| `go test -count=1 ./capability ./handlers/capability ./handlers/systemconfig ./handlers/ai` | ✅ 4 包全部通过（3.387s / 1.772s / 2.734s / 7.870s） |
| `gofmt -w <改动 Go 文件>` | ✅ 无格式差异 |

关键断言：

- `TestConfigSource_GrayscaleSwitchesOverrideAndClear`：两键覆盖立即生效、Clear 恢复默认。
- `TestKnownKeys_StableOrder`：键清单 6 项且顺序稳定。
- `TestHandlerExposesAssistantPolicies`：公共能力面缺省/关闭/开启三态正确。
- `TestAICapabilities_GetExposesEffectiveAndDefaults`：GET 下发两键生效值、默认值、键清单。
- `TestAICapabilities_PutAppliesPatchAndReset`：PUT 两键三态写入并返回最新快照。
- `TestHelpDataPlaneHonorsHelpAgentSwitch`：关闭时 `/help/search` 与 `/help/page` 均 403。

### 4.2 前端（workdir `itsm-frontend`）

| 命令 | 结果 |
|---|---|
| `npm test -- --runTestsByPath <7 个定向套件> --coverage=false --runInBand --silent` | ✅ 7 suites / 46 tests passed |
| `npm test -- --runTestsByPath src/lib/api/__tests__/system-config-api.test.ts --coverage=false --runInBand --silent` | ✅ 1 suite / 11 tests passed |
| `npm run type-check` | ✅ exit 0 |
| `npx eslint <改动文件>` | ✅ exit 0 |

定向套件：

- `use-assistant-capability-policy.test.ts`
- `AssistantDock.test.tsx`
- `AssistantChatPanel.test.tsx`
- `AssistantProactiveAssist.test.tsx`
- `AIChat.diagnostics.test.tsx`（工作区回归）
- `AICapabilitiesCard.test.tsx`
- `AICapabilitiesSection.test.tsx`
- `system-config-api.test.ts`

关键断言：

- Dock 关闭 → 不渲染入口/FAB；未注入/失败 → 默认开启。
- Dock 关闭 + 预填入口 → 自动跳转 `/ai/chat`，提示词保留待工作区消费。
- 帮助关闭 → 面板无帮助页签、无帮助视图。
- 配置卡保存仅提交脏字段；两键默认开启、可关闭、可按覆盖键恢复默认。

---

## 5. 局限与后续

1. **仅租户级**：角色/页面白名单未实现；如需更细粒度灰度，应扩展为结构化配置（当前 `system_configs` 布尔值不足）。
2. **前端非推送**：已打开页面不会实时消失；下一次能力读取/刷新后应用。后端写入后同实例立即生效，多实例 ≤30s 收敛。
3. **帮助门禁范围**：仅用户侧 `/help/search`、`/help/page`；覆盖率/待办等运营端点保持可用（管理职责）。
4. **Dock 开关不影响工作区**：`/ai/chat` 深链与 launcher 行为保持现状，符合 §3.7 / §14.4-① 收口。
5. **未跑真实浏览器/PG E2E**：本轮为单元 + 组件 + 端点测试；真实部署验证归联调条件项。

---

## 6. 变更文件

**新增**

- `itsm-backend/handlers/ai/help_switch_test.go`
- `itsm-frontend/src/lib/ai/use-assistant-capability-policy.ts`
- `itsm-frontend/src/lib/ai/__tests__/use-assistant-capability-policy.test.ts`
- `docs/plan/evidence/assistant-dock/T5-05-gray-switches-evidence.md`

**修改**

- `itsm-backend/capability/capability.go`
- `itsm-backend/capability/capability_test.go`
- `itsm-backend/internal/bootstrap/app.go`
- `itsm-backend/handlers/capability/handler.go`
- `itsm-backend/handlers/capability/handler_test.go`
- `itsm-backend/handlers/systemconfig/ai_capabilities.go`
- `itsm-backend/handlers/systemconfig/ai_capabilities_test.go`
- `itsm-backend/handlers/ai/help.go`
- `itsm-frontend/src/lib/api/capability-api.ts`
- `itsm-frontend/src/lib/api/system-config-api.ts`
- `itsm-frontend/src/lib/api/__tests__/system-config-api.test.ts`
- `itsm-frontend/src/components/ai/dock/AssistantDock.tsx`
- `itsm-frontend/src/components/ai/dock/AssistantChatPanel.tsx`
- `itsm-frontend/src/components/ai/AIChat.tsx`
- `itsm-frontend/src/components/ai/dock/__tests__/AssistantDock.test.tsx`
- `itsm-frontend/src/components/ai/dock/__tests__/AssistantChatPanel.test.tsx`
- `itsm-frontend/src/components/ai/dock/__tests__/AssistantProactiveAssist.test.tsx`
- `itsm-frontend/src/pages/(main)/admin/system-config/AICapabilitiesCard.tsx`
- `itsm-frontend/src/pages/(main)/admin/system-config/AICapabilitiesSection.tsx`
- `itsm-frontend/src/pages/(main)/admin/system-config/__tests__/AICapabilitiesCard.test.tsx`
- `itsm-frontend/src/pages/(main)/admin/system-config/__tests__/AICapabilitiesSection.test.tsx`
- `itsm-frontend/src/lib/i18n/translations.ts`
- `docs/plan/ai-assistant-dock-plan-2026-10-18.md`
- `docs/plan/ai-assistant-dock-plan-delivery-ledger-2026-10-19.md`
