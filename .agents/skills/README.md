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
| **P0** | itsm-cmdb-operations | cmdb_routes 106 | 资产/关系/同步是「环境检查」类工单的底座；读多写少、契约稳定 |
| **P0** | itsm-change-operations | change 35 + cab 4 + approval_chain 6 | 独立状态机 + 审批/CAB 语义，与 ticket 流转互补 |
| P1 | itsm-sla-operations | sla 39 | 定义/监控/超时；与 ticket stats 组合可做报表自动化 |
| P1 | itsm-msp-operations | msp 45 | MSP 多租户资源范围/客户管理，平台核心场景 |
| P1 | itsm-problem-operations | problem 24 | 调查+知识沉淀链路与 knowledge skill 衔接 |
| P2 | itsm-service-request-operations | service_request 20 + service_catalog 10 | 目录下单/审批；优先级低于流程域能力 |
| P2 | itsm-release-operations | release 16 | **须先与 itsm-deploy 划界**（业务发布 vs 部署操作） |
| P2 | itsm-cloud-operations | cloud 15 | 云资源同步/盘点，偏集成 |
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
3. **共享底座**：脚本公共层（HTTP Client / 认证 / token 主目录落盘 / 错误封装）收敛到共享模块（规划 `scripts/_itsm_common.py`）；认证与 token 规范维护**一份** reference，各 SKILL.md 链接引用，禁止复制漂移（ticket/kb 已发生一次同款漂移，见 §5 待办）。
4. **触发词互斥**：相近域在 description 显式互斥（ticket vs incident；change vs release vs deploy）。
5. **组合任务**：跨域流程（如 环境检查→建单→知识沉淀）写 runbook 文档引用多个 skill，**不为流程单开 skill**。
6. **数量上限**：常驻 skill ≤8；超出先合并。
7. **定期收敛**：每季度按使用频次复盘；连续两个周期未使用且 `usageCount=0` 的 skill 归档（移出本目录，git 历史保留）。

## 5. 近期待办（2026-10-17）

- [ ] 抽取共享底座 `_itsm_common.py`，迁移 ticket/kb 两脚本（消除已发生的复制漂移）
- [ ] incident(47) 与 ticket 域边界调研（两套路由并存），结论回填本文 §2
- [ ] **P0 启动**：itsm-cmdb-operations——先产出 API 契约调研（references 初稿），再落脚本骨架
- [ ] P0：itsm-change-operations——先调研状态机与审批链契约
