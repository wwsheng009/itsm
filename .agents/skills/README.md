# ITSM Skills 目录与治理

> 本目录收纳面向外部调用方（自动化脚本 / CI / Agent）的 ITSM 业务操作技能。本文是**唯一治理入口**：现状、候选路线图、准入门槛、收敛规则。新增 / 停用 skill 前先过本文。路由数据基线：2026-10-17（itsm-backend/router）。

## 1. 现有技能

| Skill | 覆盖域 | 脚本 | 状态 |
|---|---|---|---|
| itsm-deploy | 238/189 构建发布链路（git bundle + .dev/deploy 脚本族） | 部署脚本 | 维护中 |
| itsm-ticket-operations | 工单 CRUD / 状态机 / 评论 / 附件 / SLA 查询 | itsm_ticket.py（20 子命令） | 维护中（2026-10-17 加固 token 落盘/掩码、types 去重、create 补参+--dry-run） |
| itsm-knowledge-operations | 知识库 CRUD / 发布 / 版本 / 检索 / 分类可见性 | itsm_kb.py（22 子命令） | 维护中（2026-10-17 同款加固 + GAPS 9/10 登记） |

## 2. 候选路线图（按路由面 + 复用价值）

| 优先级 | 候选 Skill | 路由面（router/*.go 实测） | 理由 / 备注 |
|---|---|---|---|
| **P0**（✅ 已建 2026-10-17） | itsm-cmdb-operations | cmdb_routes 106 | 读面全量 20 + 写面核心 13 子命令；GAPS 见其 references §4 |
| **P0**（✅ 已建 2026-10-17） | itsm-change-operations | change 35 + cab 4 + approval_chain 6 | 33 子命令；分类型状态机 + 审批双校验，全链 238 实测通过 |
| P1 | itsm-sla-operations | sla 39 | 定义/监控/超时；与 ticket stats 组合可做报表自动化 |
| P1 | itsm-msp-operations | msp 45 | MSP 多租户资源范围/客户管理，平台核心场景 |
| P1 | itsm-problem-operations | problem 24 | 调查+知识沉淀链路与 knowledge skill 衔接 |
| P3 | itsm-incident-operations | incident 47 | 见 §7 边界结论：监控驱动场景，复用低频；先不做 |
| P2 | itsm-service-request-operations | service_request 20 + service_catalog 10 | 目录下单/审批；优先级低于流程域能力 |
| P2 | itsm-release-operations | release 16 | **须先与 itsm-deploy 划界**（业务发布 vs 部署操作） |
| P2 | itsm-cloud-operations | cloud 15 | 云资源同步/盘点，偏集成 |
| P2（产品功能） | **skills 分发与安装**（资源中心页） | —（非 skill） | 见 §6：Phase 1 仓库打包 → Phase 2 ITSM 内建资源页（API+前端）→ Phase 3 签名/自更新 |
| 不做 | — | bot 9 / connector 8 / survey 7 / audit 2 / alert 1 / vendor 4 | 长尾低频：按需并入相近域或临时 curl，不占 skill 名额 |

**明确不做**：PAT 管理面（AC-39 防自我繁殖：PAT 身份无权管理 PAT，仅浏览器会话）；登录/auth 公共底座（已内嵌各脚本）。

## 3. 准入门槛（全部满足才建新 skill）

1. 有独立领域词汇与可识别触发场景；
2. 有非平凡契约（独立状态机 / 专属权限码 / 已知 GAPS），值得一份 `references/`；
3. 预计复用 ≥3 次，或承载跨会话自动化；
4. 与现有 skill 职责不重叠（重叠先合并或划界，再议新 skill）。

## 4. 治理规则（上下文预算与一致性）

1. **描述预算**：description ≤3 行——触发词 + 覆盖端点 + 「Do NOT use for」互斥链接；正文全部下沉 `references/`（渐进披露，按需才读）。
2. **域聚合**：一个业务域一个 skill；不按路由文件切分；长尾域并入相近域。
3. **共享底座**：脚本公共层（HTTP Client / 认证 / token 主目录落盘 / 错误封装）收敛到共享模块 **`_lib/itsm_common.py`**（与本目录各 skill 平级，脚本经 `parents[2]/_lib` 引用）；认证与 token 规范维护**一份** reference，各 SKILL.md 链接引用，禁止复制漂移（ticket/kb 曾发生一次同款漂移，2026-10-17 已收敛消除）。
4. **触发词互斥**：相近域在 description 显式互斥（ticket vs incident；change vs release vs deploy）。
5. **组合任务**：跨域流程（如 环境检查→建单→知识沉淀）写 runbook 文档引用多个 skill，**不为流程单开 skill**。
6. **数量上限**：常驻 skill ≤8；超出先合并。
7. **定期收敛**：每季度按使用频次复盘；连续两个周期未使用且 `usageCount=0` 的 skill 归档（移出本目录，git 历史保留）。

## 5. 近期待办（2026-10-17）

- [x] 抽取共享底座 `_lib/itsm_common.py`，迁移 ticket/kb 两脚本（2026-10-17 完成：净删 319 行复制代码，238 实测 stats/login/上传/401 全链路回归通过）
- [x] incident(47) 与 ticket 域边界调研——结论回填 §7（P3 暂缓）
- [x] **P0**：itsm-cmdb-operations——读面+写面核心完成（33 子命令，238 实测）
- [x] **P0**：itsm-change-operations——SKILL.md + references + 33 子命令脚本，238 全生命周期实测
- [ ] CMDB 长尾：导入导出任务 / 云资源三件套 / 发现任务封装
- [ ] P1：itsm-sla-operations / itsm-msp-operations / itsm-problem-operations

## 6. 分发与安装（skills 打包下发）

目标：让 Claude Code / Codex / 其他 code-x agent 通过**一个 link** 下载并安装本目录技能（含安装指导），无需手工拷贝目录。

### 6.1 分发约束（已确认的实现事实）

1. **共享底座布局不可拆**：各域脚本经 `parents[2]/_lib` 引用 `_lib/itsm_common.py`（§4.3）。单 skill zip 必须内嵌 `_lib/`，保持「技能根/skill/scripts」布局，否则 import 断链。
2. **宿主技能根目录映射**（安装器负责落到正确位置）：
   - Claude Code：`~/.claude/skills/<name>/`
   - Codex / aicli：`~/.aicli/.agents/skills/<name>/`（外部技能根）
   - 通用/项目级：`<project>/.agents/skills/<name>/`
   - 无 `SKILL.md` 的 `_lib/` 目录会被宿主忽略，无害。
3. **脱敏要求**：SKILL.md 示例含 demo 环境口令，公开分发前统一替换为占位符（打包脚本负责）。

### 6.2 三阶段路线

| Phase | 内容 | 产物 |
|---|---|---|
| **P1 仓库即分发**（零基建） | 打包脚本产出 `dist/skills/`：每 skill 一个 zip（内嵌 `_lib`）+ 全量 bundle + `manifest.json`（name/version/description/sha256/updatedAt/sourceCommit）+ 零依赖安装器 `itsm-skills-install.py`（校验 sha256 → 解压到宿主技能根 → 登记 `.skills-installed.json`） | `scripts/package-skills.ps1` + `dist/skills/*` |
| **P2 ITSM 内建资源中心页** | 后端：`GET /api/v1/skills/registry`（清单）、`GET /api/v1/skills/:name/download`（go:embed 镜像或挂载 dist）、`GET /api/v1/skills/installer`；前端：资源页 skill 卡片（版本/大小/sha256/更新时间）+ 按**宿主 Tab**（Claude Code / Codex / 通用）展示一键复制安装命令 + 安装指导文档链接 | 后端 skills 路由 + 前端「资源中心」页 |
| **P3 可选增强** | 包签名（minisign/cosign）、安装器 `--check` 自更新、版本锁定与回滚 | 签名公钥分发 + CLI 参数 |

### 6.3 安装体验（P2 目标形态）

资源页每个 skill 卡片给出按宿主的复制即用命令，形如：

```bash
# Claude Code / Codex 通用（P1 形态：直连仓库 raw / P2 形态：ITSM 资源页 base）
curl -fsSL <BASE>/skills/manifest.json -o manifest.json
python itsm-skills-install.py --manifest manifest.json --name itsm-ticket-operations --host codex   # host: claude|codex|project
```

安装器职责：sha256 校验 → 按宿主映射解压 → 幂等（重装覆盖 + 版本登记）→ 输出后续验证命令（如 `itsm_ticket.py --help`）。

> 注：本节是**产品功能规划**，按 §4.5 不新增 skill；Phase 1 属 skill 体系工具链（仓库内脚本），Phase 2 走产品需求（后端路由 + 前端页面）。

## 7. incident 与 ticket 域边界结论（2026-10-17 调研）

| 维度 | /tickets（itsm-ticket-operations 已覆盖） | /incidents（独立域） |
|---|---|---|
| 数据模型 | 工单（含 type=incident 等枚举）+ 平台工单类型/表单/SLA | 独立 incident 实体（ent，无 ticket 外键关联） |
| 状态机 | `common.IsValidTicketStatusTransition`（new→…→closed，7 态） | `common.IsValidIncidentStatusTransition`（10 态：new/acknowledged/assigned/in_progress/triaged/escalated/on_hold/resolved/closed/cancelled） |
| 能力重心 | 业务单据流（表单/审批/自动化/评分/视图） | **监控告警驱动**：告警关联（/alerts 子面 5 条）、事件时间线、影响分析、根因分析、SLA 暂停/恢复、major-incident、convert-to-problem、metrics/monitoring |
| 权限码 | ticket 族 | incident 族（含 incident:admin/force-update） |

**结论**：两域**数据不互通、状态机各自独立**（incident 实体无 ticket 关联字段；ticket 的 type=incident 只是工单分类枚举）。incident 是面向监控告警集成的重运维域（alerts/events/metrics/monitoring 占 47 条中约 1/3）。**按 §3 门槛评估：当前无跨会话自动化诉求，P3 暂缓**；待接入监控源（Prometheus/Zabbix webhook 类场景）时再按 P1 启动，脚本骨架可直接复用 `_lib` 底座。
