# heidsoft/itsm 上游能力扫描与「审查 → 调整 → 移植」方案

> **状态**：扫描结论（2026-11-06），**未做任何代码合并**；试合并已在隔离 worktree 中 `--abort`
> **上游**：`https://github.com/heidsoft/itsm.git`（238 部署机使用的 fork），`main = 63b9feb5`
> **本地**：`feat/msp-p0-hardening = 9fefbc24`（`wwsheng009/itsm`）
> **一句话结论**：**不能合并，也不能替换**；上游价值集中在 6 个「新增能力」簇，需以能力为单位重写移植，
> 其中 1 个簇（角色模型重构）必须**硬排除**，因为与本地已收口的 D7 / G11 / TUM-7 权限边界直接冲突。

---

## 1. 分叉事实（全部为命令实测）

| 项 | 值 |
|---|---|
| merge-base | `e7fd79e9` `fix(ci): 使用 pgvector/pgvector:pg17 镜像修复 DR workflow`（**2026-09-20**） |
| 本地独有提交 | **636**（2026-09-20 → 11-06），触及 2998 文件 |
| 上游独有提交 | **537**（2026-09-20 → 11-05，作者 530 heidsoft / 4 codex / 2 TRAE / 1 ITSM Codex），触及 2903 文件 |
| 双方都改过的文件 | **575** |
| patch-id 等价提交 | **0 / 537**（`git cherry` 全部为 `+`）→ 上游没有任何提交是本地补丁的再基线，**无法靠 cherry-pick 去重** |
| 上游删除文件 | **574**；本地删除 114 |
| 上游构成 | 304 fix / 70 feat / 63 docs / 51 chore / 21 test / 17 refactor / 6 style / 2 ci |

## 2. 为什么不能直接合并

试合并（`integration/upstream-heidsoft-scan` + 独立 worktree，已 abort）：

| 指标 | 值 |
|---|---|
| 冲突文件 | **439** |
| 自动合并成功 | 2883 |
| 冲突类型 | content **308**、modify/delete **110**、rename/delete 6、rename/rename 5 |
| 「上游删除 + 本地修改」 | **94** 个，其中 **84 个在 `itsm-frontend/src`** |

### 2.1 根因：前端已**架构级**分叉（不是文件级差异）

| | 上游 `heidsoft/main` | 本地 |
|---|---|---|
| 构建 | `NEXT_DISABLE_TURBOPACK=1 next build && prepare-standalone.mjs` | `tsc -b && vite build` |
| 依赖 | `next` | `vite` + `@vitejs/plugin-react` |
| `src/` 顶层 | `app/ (auth) (main) api auth components…` | `App.tsx main.tsx pages routes layouts generated hooks lib…` |

→ **同一页面组件在两棵树里路径与生命周期不同**。任何「把上游前端文件搬过来」都会得到
可编译的空壳或半迁移状态。**上游前端只能作为行为参照（规格），不可作为文件来源。**

### 2.2 生成物必须重建，不能三方合并

冲突最密集文件（`<<<<<<<` 计数）：

```
432  docs/acl-manifest.yaml            ← 生成物：只能重跑 scripts/generate-acl-manifest.js
 37  itsm-frontend/package-lock.json    ← 依赖树：各自 npm install 重生
 26  itsm-backend/internal/bootstrap/app.go   ← 真·人工裁决热点（装配面）
 23  itsm-backend/docs/swagger.json / docs.go  ← swag 生成物（见 G16：TUM 端点零注解）
 22  itsm-backend/ent/client.go  21 ent/mutation.go  14 ent/sladefinition_update.go  ← ent 生成物
 16  itsm-backend/handlers/common/service.go   ← 权限解析单源，冲突＝语义冲突
 13  TicketDetail.tsx / handlers/ai/service.go
```

## 3. 硬排除项（不做移植评审）

| 项 | 上游提交 | 排除理由 |
|---|---|---|
| **角色模型重构** | `f66f5c824` `feat(role)!:` 角色重设为权限包、**退役 17 个遗留角色并迁移存量数据**（59 文件） | BREAKING。与本地已收口的三处冲突：① 2026-10-10 治理域收口 D7（`tenant:*` 平台专属、`internal/authz/roles.go:247-249,521-523`）② G11 组合角色 `provider_tenant_admin` 四处同源 ③ `middleware.RoleRank` / `isElevatedMenuRole` / `pkg/seeder` 词表。移植＝同时改写权限单源，风险高于收益 |
| Next.js 侧前端文件 | 各 `src/app/**` 提交 | 本地是 Vite SPA，结构不兼容（§2.1） |
| 生成物 | `acl-manifest.yaml`、`docs/{docs.go,swagger.*}`、`ent/**`、`rbac_precheck_gen.go`、`package-lock.json` | 一律「本地重生成」，不取上游内容 |
| 叙事类文档 | `CHANGELOG.md`（上游改 242 次）、`UPGRADE.md`（57 次）、`docs/api-reference.md`（67 次） | 手工摘要合并，禁止整文件替换 |

---

## 4. 功能簇可移植性分级

「重叠%」= 该簇触及文件中，本地也改过的比例（冲突成本代理）。是否已有能力＝本地源码实测，不是猜测。

| 簇 | 提交 | 时间跨度 | 触及文件 | 重叠% | 本地现状（证据） | 分级 |
|---|---|---|---|---|---|---|
| **值班排班 on-call** | 1 | 10-21 | 14 | 36% | **无 on_call 域**：`handlers/` 下无该目录，仅 `rbac_precheck_gen.go`/docs 命中 | **A 新增能力，优先** |
| **SSO / OIDC** | 4 | 10-16→10-29 | 71 | 28% | 无实现：`oidc` 仅出现在 `connector/connector.go` 与 capability 测试 | **A 新增能力**（安全面需专项评审） |
| **CLI / MCP 面** | 9 | 10-23→11-04 | 158 | **6%** | 已有 `itsm-cli/` 与 `mcp/{client,transport}`；上游新增零依赖 MCP Server（`ff97aa0ad`，56 文件）+ 11 命令 + 构建陈旧守卫 | **A 增量最小**（多为新增文件） |
| **云发现 / 多云适配器** | 8 | 10-23→11-01 | 282 | 29% | 已有 `handlers/cloud`（仅 3 文件）与 `cloud_provider` 4 处；上游补齐 AWS/腾讯云/火山引擎适配器（`a1989b199`，20 文件）+ 云发现 Job 闭环（`af1125a4d`，21 文件） | **A/B 差集移植** |
| **验证命令 verify** | 2 | 11-03 | 14 | 29% | 无对应 `cmd/`（阿里云 OSS/交付链路零费用验证） | **A 新增工具** |
| **运维三件套 / 死信** | 9 | 09-25→10-24 | 39 | 31% | 已有 `handlers/operations`；死信人工重放产品面需核对 | **B 差集** |
| **报表 / 服务质量视图** | 10 | 10-02→10-31 | 106 | 39% | 已有 `handlers/analytics`、`report:*` 权限码；上游为单请求聚合 + `metricVersion` 口径版本 | **B 差集** |
| **应用市场 Marketplace** | 10 | 09-25→11-04 | 75 | 25% | 已有 `handlers/marketplace`；上游补商品目录管理面（平台级权限码 + 10 端点 + 版本发布/回滚）与依赖闸门 | **B 差集**（涉及平台权限码，需 D7 校验） |
| **AI 记忆 / 配额 / 评测** | 8 | 10-07→11-01 | 50 | 38% | 已有 `handlers/ai`、`internal/llm`、`ai_llm_calls`、providers；上游为情景/会话记忆层、token 配额与成本告警、ai-eval 语料 | **B 差集**（高价值） |
| **通知域 GA** | 2 | 10-03→10-21 | 46 | 43% | 已有 `handlers/notification` 与投递链路 | **B** |
| **服务请求交付链** | 1 | 11-03 | 54 | 20% | 已有 `handlers/service_request` 三级审批 + provision/execute | **B 需先证伪重复** |
| **变更回滚 / 留痕** | 2 | 10-22 | 45 | 58% | 已有 `handlers/change` 完整状态机 + `/fail` 类入口需核对；技能面 `itsm-change-operations` 已建模 | **B/C** |
| **问题 → 知识沉淀闭环** | 3 | 10-15→10-22 | 75 | 59% | 已有 `handlers/problem`、`knowledge`、`known_error`(KEDB) | **B/C** |
| **BPMN 模板版本升级** | 1 | 11-03 | 33 | 24% | 已有 `handlers/bpmn`、`service/bpmn` | **B** |
| **连接器凭据轮换** | 1 | 10-22 | 10 | 60% | 已有 `handlers/connector`；上游称「实现了却没有路由」 | **B 小切口，可先验证** |
| **能力矩阵 / Pilot / GA** | 6 | 10-21→11-03 | 74 | **66%** | 已有 `handlers/capability`（21 处） | **C 暂缓**（与本地能力词表耦合最高） |
| **角色模型重构** | 1 | 10-09 | 59 | 41% | 见 §3 | **D 拒绝** |
| 其它/未归类（fix/docs/chore 混合） | 45 | 09-20→11-05 | 2528 | 22% | 需按域二次细分后才可评估 | **待细分** |

## 5. 建议移植顺序（先低风险增量，后高耦合）

1. **CLI / MCP 面**（重叠 6%，多为新增文件）→ 与本地 `itsm-cli/` 对齐命令面与 MCP server 形态；
2. **验证命令 verify** + **连接器凭据轮换路由**（小切口，10–14 文件，可快速验证协议是否可行）；
3. **值班排班 on-call**（本地空白域，14 文件；新增领域入口 + 迁移 + 权限码）；
4. **SSO / OIDC**（新增能力但属认证面，须安全评审 + 双人复核，参照 `plan §10` 冻结范式）；
5. **云发现 / 多云适配器**（差集：本地 `handlers/cloud` 3 文件 → 上游适配器 + Job 闭环）；
6. **AI 记忆 / 配额 / 评测**、**Marketplace**、**报表**（差集，逐簇先证伪重复）；
7. **能力矩阵 / Pilot / GA**、**角色重构** 保持不动，直到权限词表与治理边界双方对齐。

## 6. 审查 → 调整 → 移植协议（每簇一轮，禁止整簇 merge）

| 步 | 动作 | 退出判据 |
|---|---|---|
| S1 差集 | 列「上游有 / 本地有 / 语义差」三列；先证明不是重复实现 | 差集清单落文档，含本地证据 `file:line` |
| S2 规格化 | 把上游行为写成能力规格（端点、错误码、审计事件、权限码、迁移），**不搬文件** | 契约表（请求/响应/错误码）齐备 |
| S3 边界校验 | 权限码归属（D7：平台家族码不得落入租户角色）、G11 四处同源、RLS `tenant_id`、能力词表 | 无越权面扩张；`internal/authz` parity 通过 |
| S4 本地重写 | 后端按本地分层（handler/service/dto/router/ent）实现；前端**按 Vite 结构重写**，只取行为 | `go build`/`go vet` + `tsc --noEmit` |
| S5 迁移对齐 | 上游 migration 文件不得直取；本地重编号 + checksum 账本核对（见 `multi-llm-provider-plan` §4.4 账本漂移教训） | `cmd/mig-verify -ro` 无阻塞 |
| S6 测试先行 | 补契约用例（错误码同源、护栏、审计）+ 生成物重生成（swagger/acl-manifest/ent） | `go test ./service ./handlers/...` + 目标 jest 全绿 |
| S7 live 验证 | 238 演示栈部署 + API/浏览器双验证（沿用本轮方法：`activeAdminCount` 前后对照法） | 残项表逐项闭环，回滚步骤成文 |

**硬约束**：① 每簇一个 PR，禁止跨簇混合；② `CHANGELOG/UPGRADE/api-reference` 手工摘要；
③ 生成物一律本地重生成；④ 上游若删文件，本地先确认没有依赖再删（本次 94 个「上游删+本地改」即为反面教材）。

## 7. 复现命令（评审者可自跑，不动工作区）

```bash
# 1) 只读 fetch 上游（不改本地分支）
git remote add heidsoft https://github.com/heidsoft/itsm.git 2>/dev/null
git fetch --no-tags heidsoft main

# 2) 分叉事实与补丁等价性
git merge-base HEAD heidsoft/main                       # e7fd79e9
git rev-list --left-right --count HEAD...heidsoft/main   # 636   537
git cherry HEAD heidsoft/main | awk '{print substr($0,1,1)}' | sort | uniq -c   # 全为 '+'

# 3) 冲突面（不落工作区）
git merge-tree --write-tree --name-only HEAD heidsoft/main | sed -n '2,/^$/p' | wc -l   # 439

# 4) 簇级重叠扫描（需 python）
python -X utf8 output/upstream-cluster-scan.py

# 5) 试合并（务必在独立 worktree，且看完就 abort）
git branch -f integration/upstream-heidsoft-scan HEAD
git worktree add output/upstream-scan integration/upstream-heidsoft-scan
cd output/upstream-scan && git merge --no-commit --no-ff heidsoft/main
git diff --name-only --diff-filter=U | wc -l ; git merge --abort
```

## 8. 本次留下的工作区（可随时清理，不影响主工作树）

| 对象 | 说明 |
|---|---|
| 远端 `heidsoft` | 只读 fetch 配置；删除：`git remote remove heidsoft`（**注意**：238 部署机的 origin 就是它） |
| 分支 `integration/upstream-heidsoft-scan` | 停在 `9fefbc24`，试合并已 abort，无合并提交 |
| worktree `output/upstream-scan/` | `git worktree remove output/upstream-scan` 清理 |
| `output/upstream-cluster-scan.py` | 簇级重叠评分脚本（`output/` 已 gitignore） |

## 9. 文件信息

| 项 | 值 |
|---|---|
| 创建日期 | 2026-11-06 |
| 扫描对象 | `heidsoft/main = 63b9feb5`；本地 `9fefbc24` |
| 方法 | `merge-tree` 无副作用试合并 + 独立 worktree 试合并（已 abort）+ `git cherry` 补丁等价 + 簇级文件交集 + 本地源码能力核对 |
| 未执行 | 任何合并提交、任何 cherry-pick、任何代码改动；主工作区未切分支、未受影响 |
| 后续动作 | 按 §5 顺序逐簇开 S1 差集评审；建议首簇选「CLI/MCP 面」验证协议，次选「on-call」补空白域 |
