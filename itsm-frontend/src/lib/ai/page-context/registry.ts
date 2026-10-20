/**
 * D0-03 页面助手注册表（L1 页面声明，静态注册；P0 零延迟基线）。
 *
 * 口径（方案 §4.1/§3.3/§3.4；I0-02 分组模型）：
 * - 顺序即匹配优先级：**具体静态路由必须先于同前缀的动态路由**（如 `/tickets/create` 先于 `/tickets/:id`）；
 * - **分组**：`tasks[]`（任务卡，零输入可执行，**必须只读/plan**——写动作一律走确认闭环，不进任务卡）
 *   与 `questions[]`（问题卡，了解型，点击即问）；**每页至少 1 个任务卡**（I0-02 验收）；
 * - 页面内动作总数 ≤3（任务卡 1–2 + 问题卡 0–2，保持建议区紧凑）；
 * - `entrypoint` 对齐后端已知入口枚举，授权与工具门禁仍按服务端语义走；
 * - D1-01：动作可声明 `tools`（依赖工具名）——服务端 `/ai/assistant/context` 按
 *   「场景 Bot 授权 ∩ 调用方 RBAC ∩ 风险上限」过滤；仅声明可从提示词直接推断依赖的动作。
 */
import type { PageAssistantDeclaration } from './types';

export const PAGE_ASSISTANT_REGISTRY: readonly PageAssistantDeclaration[] = [
  // ===== 工单 =====
  {
    pageKey: 'ticket.create',
    pattern: '/tickets/create',
    title: '创建工单',
    scene: 's1-ticket-assistant',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'draft_fields',
        kind: 'task',
        label: '帮我起标题与描述',
        prompt: '我要创建工单：请先问我关键信息，再给出一个可编辑的标题与描述草稿。',
        readOnly: true,
        tools: ['draft_ticket_fields'],
      },
    ],
    questions: [
      {
        id: 'how_to_fill',
        kind: 'question',
        label: '这些字段该怎么填？',
        prompt: '创建工单时，分类、优先级、影响范围这些字段应该怎么判断？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'ticket.aiCreate',
    pattern: '/tickets/ai-create',
    title: 'AI 创建工单',
    scene: 's1-ticket-assistant',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'ai_create_draft',
        kind: 'task',
        label: '把描述整理成工单字段（只读草稿）',
        prompt: '我准备用自然语言创建工单：请给出标题、描述、分类与优先级草稿（只读，不提交）。',
        readOnly: true,
        tools: ['draft_ticket_fields'],
      },
    ],
    questions: [
      {
        id: 'describe_need',
        kind: 'question',
        label: '怎么描述才能生成准确工单？',
        prompt: '用自然语言创建工单时，怎样描述问题才能让 AI 生成的字段更准确？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'ticket.detail',
    pattern: '/tickets/:id',
    title: '工单详情',
    scene: 's1-ticket-assistant',
    entrypoint: 'ticket_detail',
    tasks: [
      {
        id: 'triage_plan',
        kind: 'task',
        label: '分诊建议（分类与优先级）',
        prompt: '请给出这张工单的分类与优先级建议，并说明判断依据。',
        readOnly: true,
        tools: ['list_tickets'],
      },
      {
        id: 'reply_draft',
        kind: 'task',
        label: '生成答复草稿',
        prompt: '请基于这张工单生成一段可编辑的答复草稿（不含最终提交动作）。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'kb_lookup',
        kind: 'question',
        label: '知识库有没有相关方案？',
        prompt: '知识库里有和这张工单相关的解决方案吗？请列出引用来源。',
        readOnly: true,
        tools: ['list_kb'],
      },
    ],
  },
  {
    pageKey: 'ticket.list',
    pattern: '/tickets',
    title: '工单列表',
    scene: 's1-ticket-assistant',
    entrypoint: 'ticket_list',
    tasks: [
      {
        id: 'pending_overview',
        kind: 'task',
        label: '汇总我待处理的工单',
        prompt: '汇总我当前待处理的工单（数量、优先级分布、临近 SLA 的项），只做只读汇总。',
        readOnly: true,
        tools: ['list_tickets'],
      },
    ],
    questions: [
      {
        id: 'priority_view',
        kind: 'question',
        label: '如何优先处理这些工单？',
        prompt: '根据优先级与 SLA 风险，我应该先处理哪些工单？',
        readOnly: true,
      },
    ],
  },

  // ===== 事件 =====
  {
    pageKey: 'incident.create',
    pattern: '/incidents/create',
    title: '创建事件',
    scene: 's2-incident-oncall',
    entrypoint: 'incident_create',
    tasks: [
      {
        id: 'complete_description',
        kind: 'task',
        label: '帮我完善事件描述',
        prompt: '我要创建事件：请引导我补齐影响范围、影响面与时间线，并给出可编辑的描述草稿。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'severity_help',
        kind: 'question',
        label: '级别该怎么定？',
        prompt: '事件级别（P1/P2/…）的判断标准是什么？当前情况建议定几级？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'incident.detail',
    pattern: '/incidents/:id',
    title: '事件详情',
    scene: 's2-incident-oncall',
    entrypoint: 'incident_detail',
    tasks: [
      {
        id: 'impact_plan',
        kind: 'task',
        label: '影响面分析计划',
        prompt: '请为这个事件生成影响面分析计划（受影响 CI、上下游、关联工单统计），只产出计划不修改数据。',
        readOnly: true,
        tools: ['analyze_ci_impact_plan'],
      },
    ],
    questions: [
      {
        id: 'similar_incidents',
        kind: 'question',
        label: '历史上出现过类似事件吗？',
        prompt: '历史上有没有类似事件？当时的处置方式是什么？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'incident.list',
    pattern: '/incidents',
    title: '事件列表',
    scene: 's2-incident-oncall',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'active_overview',
        kind: 'task',
        label: '未解决事件概览',
        prompt: '按影响面汇总当前未解决事件（只读），并标出需要值班关注的高风险项。',
        readOnly: true,
        tools: ['get_incident_stats'],
      },
    ],
    questions: [],
  },

  // ===== 变更 =====
  {
    pageKey: 'change.create',
    pattern: '/changes/new',
    title: '新建变更',
    scene: 's4-change-impact-analyst',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'risk_draft',
        kind: 'task',
        label: '梳理风险与回滚方案',
        prompt: '我要提交变更：请引导我补齐影响范围、风险点与回滚方案，只给出计划草稿。',
        readOnly: true,
      },
    ],
    questions: [],
  },
  {
    pageKey: 'change.detail',
    pattern: '/changes/:id',
    title: '变更详情',
    scene: 's4-change-impact-analyst',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'impact_plan',
        kind: 'task',
        label: '生成影响分析计划',
        prompt: '请为这个变更生成影响分析计划（涉及 CI、上下游依赖、关联工单与历史事件），只做只读分析。',
        readOnly: true,
        tools: ['analyze_ci_impact_plan'],
      },
    ],
    questions: [
      {
        id: 'cab_evidence',
        kind: 'question',
        label: 'CAB 需要哪些证据？',
        prompt: '提交 CAB 评审通常需要哪些证据材料？当前变更还缺什么？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'change.list',
    pattern: '/changes',
    title: '变更列表',
    scene: 's4-change-impact-analyst',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'week_overview',
        kind: 'task',
        label: '本周变更概览',
        prompt: '汇总本周待批准与进行中的变更（只读），并标出高风险项。',
        readOnly: true,
      },
    ],
    questions: [],
  },

  // ===== 问题 =====
  {
    pageKey: 'problem.list',
    pattern: '/problems',
    title: '问题管理',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'problem_overview',
        kind: 'task',
        label: '汇总未关闭问题（只读）',
        prompt: '汇总当前未关闭的问题（数量、优先级、关联的已知错误），只做只读汇总。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'known_error_hint',
        kind: 'question',
        label: '已知错误的根因线索怎么找？',
        prompt: '如何从已知错误与历史工单中定位问题根因？给出排查路径。',
        readOnly: true,
      },
    ],
  },

  // ===== CMDB =====
  {
    pageKey: 'ci.create',
    pattern: '/cmdb/cis/create',
    title: '新建配置项',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'ci_draft',
        kind: 'task',
        label: '整理配置项填写清单（只读）',
        prompt: '我要新建配置项：请给出类型/属性/关系填写清单与常见错误提醒（只读，不写入）。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'ci_fields_help',
        kind: 'question',
        label: 'CI 属性该怎么填？',
        prompt: '创建配置项时，类型、属性、关系应该怎么填？有哪些常见错误？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'ci.detail',
    pattern: '/cmdb/cis/:id',
    title: '配置项详情',
    scene: 's4-change-impact-analyst',
    entrypoint: 'ci_detail',
    tasks: [
      {
        id: 'impact_plan',
        kind: 'task',
        label: '生成影响分析计划',
        prompt: '请为这个配置项生成影响分析计划（上下游关系、关联工单与历史事件），只做只读分析。',
        readOnly: true,
        tools: ['analyze_ci_impact_plan'],
      },
    ],
    questions: [
      {
        id: 'related_tickets',
        kind: 'question',
        label: '它关联了哪些工单？',
        prompt: '这个配置项关联了哪些未关闭工单？请列出并说明影响。',
        readOnly: true,
        tools: ['get_ci_tickets'],
      },
    ],
  },
  {
    pageKey: 'ci.list',
    pattern: '/cmdb/cis',
    title: '配置项列表',
    scene: 's4-change-impact-analyst',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'type_stats',
        kind: 'task',
        label: '按类型统计 CI',
        prompt: '按类型统计当前配置项数量，并列出最近变更最多的类型（只读）。',
        readOnly: true,
        tools: ['list_cis'],
      },
    ],
    questions: [],
  },

  // ===== 知识 =====
  {
    pageKey: 'knowledge.new',
    pattern: '/knowledge/articles/new',
    title: '新建知识文章',
    scene: 's3-knowledge-assistant',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'draft_from_context',
        kind: 'task',
        label: '帮我把要点整理成文章',
        prompt: '请把我提供的要点整理成一篇知识文章草稿（含标题、适用场景与步骤），不自动发布。',
        readOnly: true,
        tools: ['draft_kb_article'],
      },
    ],
    questions: [],
  },
  {
    pageKey: 'knowledge.list',
    pattern: '/knowledge',
    title: '知识库',
    scene: 's3-knowledge-assistant',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'kb_digest',
        kind: 'task',
        label: '整理知识检索清单（只读）',
        prompt: '请根据我要解决的问题，给出建议的知识检索关键词与候选方案清单（只读，不发布内容）。',
        readOnly: true,
        tools: ['list_kb'],
      },
    ],
    questions: [
      {
        id: 'search_help',
        kind: 'question',
        label: '怎么快速找到相关方案？',
        prompt: '我想在知识库中查找某个主题的解决方案，应该怎么描述检索条件？',
        readOnly: true,
        tools: ['list_kb'],
      },
      {
        id: 'self_service',
        kind: 'question',
        label: '自助解决问题的入口在哪里？',
        prompt: '普通用户遇到常见 IT 问题（网络/账号/软件），自助解决的推荐路径是什么？',
        readOnly: true,
      },
    ],
  },

  // ===== SLA / 服务目录 / 仪表盘 =====
  {
    pageKey: 'sla.monitor',
    pattern: '/sla-monitor',
    title: 'SLA 监控',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'risk_scan',
        kind: 'task',
        label: '找出临近违约的工单',
        prompt: '列出当前临近 SLA 违约的工单（按剩余时间排序，只读），并给出优先处理建议。',
        readOnly: true,
        tools: ['list_tickets'],
      },
    ],
    questions: [
      {
        id: 'explain_risk',
        kind: 'question',
        label: '为什么这张单有违约风险？',
        prompt: '如何判断一张工单是否存在 SLA 违约风险？关键时间点有哪些？',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'service.catalog',
    pattern: '/service-catalog',
    title: '服务目录',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'catalog_recommend',
        kind: 'task',
        label: '推荐应选的服务条目（只读）',
        prompt: '根据我的问题描述，推荐应选择的服务目录条目并说明理由（只读，不提交申请）。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'which_service',
        kind: 'question',
        label: '该选哪个服务？',
        prompt: '我遇到一个问题，不确定该走哪个服务目录条目，请帮我判断并说明理由。',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'service.requests',
    pattern: '/service-requests',
    title: '服务请求',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'request_progress',
        kind: 'task',
        label: '查我的请求进度',
        prompt:
          '查询我最近提交的服务请求进度，按状态列出，并指出下一步需要我做什么（只读，不直接提交任何变更）。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'open_account',
        kind: 'question',
        label: '我要开通账号',
        prompt:
          '我要开通 VPN、邮箱或业务账号，请判断该走哪类服务请求，并列出需要准备的信息（先确认类型，不要直接提交）。',
        readOnly: true,
      },
      {
        id: 'device_broken',
        kind: 'question',
        label: '我的设备坏了',
        prompt:
          '我的设备坏了，请判断应报事件单还是服务请求，并说明需要补充的设备信息（只读）。',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'dashboard',
    pattern: '/dashboard',
    title: '仪表盘',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'attention_today',
        kind: 'task',
        label: '今天最需要关注什么？',
        prompt: '基于当前数据，汇总今天最需要关注的事项（SLA 风险、未解决事件、待批变更），只做只读汇总。',
        readOnly: true,
        tools: ['list_tickets', 'get_incident_stats'],
      },
    ],
    questions: [
      {
        id: 'sla_summary',
        kind: 'question',
        label: 'SLA 风险摘要怎么看？',
        prompt: '仪表盘上的 SLA 指标分别代表什么？出现什么信号时需要介入？',
        readOnly: true,
      },
    ],
  },

  // ===== AI 工作区与观测 =====
  {
    pageKey: 'ai.chat',
    pattern: '/ai/chat',
    title: 'AI 助手',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'summarize_tickets',
        kind: 'task',
        label: '汇总我最近处理过的工单',
        prompt: '总结我最近处理过的工单，并按状态与优先级分类。',
        readOnly: true,
        tools: ['list_tickets'],
      },
    ],
    questions: [
      {
        id: 'standard_process',
        kind: 'question',
        label: '这个问题的标准处置流程？',
        prompt: '这个问题的标准处置流程是什么？请给出步骤与责任人。',
        readOnly: true,
      },
    ],
  },
  {
    pageKey: 'ai.observability',
    pattern: '/ai/observability',
    title: 'AI 观测看板',
    entrypoint: 'chat',
    tasks: [
      {
        id: 'obs_digest',
        kind: 'task',
        label: '汇总近期错误与降级（只读）',
        prompt: '汇总近期 AI 助手错误与降级事件（按页面与错误码），只做只读分析。',
        readOnly: true,
      },
    ],
    questions: [
      {
        id: 'metric_meaning',
        kind: 'question',
        label: '四层指标口径怎么理解？',
        prompt: '观测看板的采用/效果/质量/成本与时延四层指标分别代表什么？如何据此判断体验问题？',
        readOnly: true,
      },
    ],
  },
];