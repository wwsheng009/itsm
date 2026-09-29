import { useNavigate } from 'react-router';

/**
 * AI 智能助手 — 流式回答 + 引用来源 + 会话历史（ChatGPT / DeepSeek 风格会话界面）
 *
 * 交互特性（数据流与既有功能保持不变）：
 *  - 左侧边栏显示会话历史列表（按时间倒序），支持切换/新建/删除
 *  - SSE 推送 token，助手消息实时增长（Markdown 渲染）；期间用户可"停止生成"取消流。
 *  - 每条助手消息挂一组引用来源（RagAnswer），点击展开可查看片段与得分。
 *  - 流失败时自动降级为一次性 chat 调用，保证有可读回答。
 *  - Provider 切换器（P1 读端点已降为 ai:read，全员可见）；「设为我的默认 / 清除默认」写按钮
 *    仍要求 system:write（PUT /ai/user-preference 未放开）。
 *  - 助手的「补充为知识文章」把回答 Markdown 原文经路由 state 带入新建页，
 *    不再只做一次不带数据的裸跳转（契约见 lib/knowledge/ai-article-prefill）。
 *  - 标题栏「保存成文章」把**整段会话**整理成文章：state 只带会话 ID，正文由新建页
 *    经接口拉取后组装（会话动辄上万字，不塞 history.state；组装逻辑见 lib/knowledge/conversation-article）。
 *
 * 渲染层：主区 = 顶部工具条 + 消息滚动区（消息列 max-width 820 居中）+ 底部输入坞。
 *        整页高度由 lockChatPageLayout 锁到视口内（只滚动消息区，外层无滚动条）。
 */

import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Avatar,
  Button,
  Input,
  Popconfirm,
  Select,
  Spin,
  Tag,
  Tooltip,
  Typography,
  message as antdMessage,
  theme,
} from 'antd';
import {
  Bot,
  Check,
  ChevronLeft,
  Clock,
  Copy,
  Eraser,
  FileText,
  LoaderCircle,
  MessageSquare,
  Plus,
  Send,
  Star,
  StopCircle,
  Trash2,
} from 'lucide-react';

import {
  AIApi,
  aiApproveTool,
  aiChatStream,
  deleteConversation,
  getConversationMessages,
  listConversations,
  type ConversationSummary,
  type RagAnswer,
  type AIToolStreamEvent,
  type AIRunStepEvent,
  type ToolInvocationDetail,
  type BotOption,
} from '@/lib/api/ai-api';
import {
  LLM_PROVIDER_DISABLED,
  describeLLMProviderError,
  llmProviderErrorCode,
} from '@/lib/api/llm-provider-api';
import { useLLMProviderFeature } from '@/lib/hooks/use-llm-provider-feature';
import { usePermissions } from '@/lib/hooks/use-permissions';
import {
  buildArticlePrefillState,
  buildConversationArticlePrefillState,
} from '@/lib/knowledge/ai-article-prefill';
import MarkdownMessage from './MarkdownMessage';
import ToolCallTimeline, { mergeToolEvents } from './tool-call-timeline';
import ToolApprovalCard from './tool-approval-card';
import ConfirmationDrawer from './confirmation-drawer';
import EvidencePanel, { type EvidenceTargetMeta } from './evidence-panel';
import RunStatusBar, { type RunSnapshot } from './run-status-bar';
import BotSelector from './BotSelector';

const { Text } = Typography;

/**
 * 时间线中不再重复展示的状态（由待审批卡片单独承载）。
 * 常量置于模块级，避免每次渲染新建数组导致 ToolCallTimeline 的重算。
 */
const PENDING_HIDDEN_STATUSES: readonly string[] = ['pending'];

/** done 事件 providerSource → 可读来源（BE-7）。 */
const PROVIDER_SOURCE_LABELS: Record<string, string> = {
  request: '会话指定',
  user: '个人默认',
  tenant: '租户默认',
  static: '静态配置',
};

/** 空状态建议卡片（点击即发送）。 */
const SUGGESTIONS = [
  '总结我最近处理过的工单',
  '这个问题的标准处置流程是什么？',
  '知识库里有没有相关方案？',
  '帮我起草一份变更方案',
];

/** 消息列宽度（与输入坞同宽居中）。 */
const CONTENT_MAX_WIDTH = 820;

/** 距底部小于该阈值时恢复"自动滚到底"。 */
const AUTO_SCROLL_THRESHOLD = 120;

type AntdToken = ReturnType<typeof theme.useToken>['token'];

interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  createdAt: string;
  streaming?: boolean;
  sources?: RagAnswer[];
  /**
   * 工具调用过程事件（M1-04）：仅来自 SSE（M1-03），不额外轮询、不读审计表。
   * 事件缺失（旧后端/事件丢失）时组件不渲染时间线，内容仍由 `content` 承载。
   */
  toolEvents?: AIToolStreamEvent[];
  /**
   * 写工具的待审批 invocation（M1-05）：由 approval_pending 事件派生，渲染待审批卡片。
   * 状态刷新由卡片自身的「刷新状态」按钮触发（不轮询）。
   */
  pendingApprovals?: number[];
  /**
   * B1-08：v2 运行快照（`run_started`/`step`/`done`/`error` 增量维护）。
   * 旧后端不产生 v2 事件 → 保持 undefined，状态条与证据面板均不渲染。
   */
  run?: RunSnapshot;
  /** B1-08：v2 步骤（按 stepIndex 去重、上限 100 条兜底内存）。 */
  steps?: AIRunStepEvent[];
  /** 生效实例（done 事件回带；开关关闭时缺省）。 */
  providerInfo?: { provider?: string; providerSource?: string };
  error?: string;
}

const nextId = () => `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

/** 会话列表 / 消息操作区的少量 hover 反馈（作用域卡在本组件内，不动全局样式）。 */
const buildChatCss = (token: AntdToken): string => `
.ai-chat-conv { border-radius: 8px; transition: background-color .15s ease; }
.ai-chat-conv:hover { background: ${token.colorFillTertiary}; }
.ai-chat-conv[data-selected='true'] { background: ${token.colorPrimaryBg}; }
.ai-chat-actions { opacity: .5; transition: opacity .15s ease; }
.ai-chat-item:hover .ai-chat-actions, .ai-chat-actions:focus-within { opacity: 1; }
.ai-chat-suggest { text-align: left; height: auto; white-space: normal; padding: 12px 14px; }
.ai-chat-suggest:hover { border-color: ${token.colorPrimary}; color: ${token.colorPrimary}; }

/* 全屏锁生效期间（见 lockChatPageLayout）：内容区不留底部内边距、隐藏全局页脚。
   两者合计 64px，会让聊天卡片与视口下边缘之间残留一条空白带。
   这里用作用域 CSS 而不是内联样式：MainLayout 会在断点切换时重写 .main-content 的
   padding 简写，内联覆盖会被 React 的重渲染清掉；CSS 规则不会。 */
html[data-ai-chat] #main-content > .main-content { padding-bottom: 0 !important; }
html[data-ai-chat] #main-content ~ footer { display: none !important; }
`;

/**
 * 聊天页整页布局锁：把 Header / Content 外壳临时改造成 100vh 纵向 flex 链，
 * 使聊天卡片精确占满剩余高度、输入坞贴底，消息溢出只滚动消息区，页面本身不再出现滚动条。
 *
 * 同时在 <html> 上打 data-ai-chat 标记，启用 buildChatCss 中「隐藏全局页脚 + 去掉内容区
 * 底部内边距」的作用域规则，让聊天卡片直接铺到视口下边缘。
 *
 * 祖先链刻意用内联样式逐层覆盖，而不是写作用域 CSS（如 html.xxx #main-content .page-transition）：
 * 跨层后代选择器在挂载瞬间命中不稳定，会让外壳先按旧高度渲染一帧再跳变；内联样式配合
 * transition:none 可保证首帧即为最终布局（实测首帧卡片高度已等于视口减去 Header 与顶部内边距）。
 * 返回的还原函数在组件卸载时把祖先链恢复原样，因此不会影响其它页面。
 */
const lockChatPageLayout = (): (() => void) => {
  const restores: Array<() => void> = [];
  const patch = (el: Element | null, styles: Record<string, string>) => {
    if (!(el instanceof HTMLElement)) return;
    const saved = Object.keys(styles).map(
      prop => [prop, el.style.getPropertyValue(prop)] as const
    );
    for (const [prop, value] of Object.entries(styles)) el.style.setProperty(prop, value);
    restores.push(() => {
      for (const [prop, prev] of saved) {
        if (prev) el.style.setProperty(prop, prev);
        else el.style.removeProperty(prop);
      }
    });
  };

  const sider = document.querySelector('#root .ant-layout-has-sider');
  const main = document.getElementById('main-content');
  const fillHeight = { height: '100%', 'min-height': '0' };
  const column = { display: 'flex', 'flex-direction': 'column' };
  // 外壳自身带 transition（ant-layout-content 200ms / 降级动画 0.01ms），会让尺寸变化延迟一帧才生效。
  const instant = { transition: 'none' };

  // 外壳锁成 100vh：文档本身不滚动，滚动条只出现在消息区。
  patch(document.documentElement, { height: '100vh', overflow: 'hidden' });
  patch(document.body, { height: '100%', overflow: 'hidden' });
  for (const el of document.querySelectorAll('#root, #root .ant-app, #root .app-root')) {
    patch(el, { height: '100%' });
  }
  patch(sider, fillHeight);
  patch(sider?.querySelector(':scope > .ant-layout') ?? null, fillHeight);

  // Content 与路由过渡层改成纵向 flex 容器，卡片用 flex:1 吃掉 Header 之外的全部高度。
  patch(main, { ...column, ...instant, 'min-height': '0', overflow: 'hidden' });
  patch(main?.querySelector(':scope > .main-content') ?? null, {
    ...column,
    ...instant,
    ...fillHeight,
    flex: '1 1 auto',
  });
  for (const el of main?.querySelectorAll('.page-transition') ?? []) {
    patch(el, { ...column, ...instant, ...fillHeight, flex: '1 1 auto' });
  }

  // 底部留白（16px 内边距 + 48px 页脚）交给作用域 CSS：内联写法会被 MainLayout 在断点
  // 切换时的重渲染清掉，而这里的两处目标都受 React 管理（.main-content 的 padding 简写）。
  document.documentElement.setAttribute('data-ai-chat', '');
  restores.push(() => document.documentElement.removeAttribute('data-ai-chat'));

  return () => {
    for (const restore of restores.reverse()) restore();
  };
};

/** 助手的「补充为知识文章」入口依赖的裁剪条件（与迁移前一致）。 */
const canPromoteToArticle = (m: ChatMessage): boolean =>
  m.role === 'assistant' &&
  !m.streaming &&
  !m.error &&
  Boolean(m.content) &&
  (!m.sources || m.sources.length === 0);

interface ChatMessageItemProps {
  message: ChatMessage;
  providerLabel: (key?: string) => string;
  onCreateArticle: (message: ChatMessage) => void;
  /** M1-05：跳转外置审批页（一期边界 = 外置审批闭环，卡片只提示与跳转）。 */
  onOpenApproval: (invocationId: number) => void;
  /**
   * B1-07：对话内确认。缺省时保持一期行为（卡片只提示 + 跳转）。
   * 注入后由卡片提供「确认 / 拒绝」入口，抽屉负责倒计时与原因必填。
   */
  onRequestConfirm?: (invocationId: number) => void;
  /** B1-07：确认后的刷新回调（状态变化后让外层刷新会话/审计视图）。 */
  onConfirmed?: () => void;
}

/** 单条消息：用户 = 右对齐气泡；助手 = 头像 + Markdown 正文 + 操作区。 */
const ChatMessageItem: React.FC<ChatMessageItemProps> = ({
  message,
  providerLabel,
  onCreateArticle,
  onOpenApproval,
  onRequestConfirm,
  onConfirmed,
}) => {
  const { token } = theme.useToken();
  const [copied, setCopied] = useState(false);
  const copyTimerRef = useRef<number | null>(null);
  // B1-07：对话内确认抽屉（仅在 BusinessBot 注入 onRequestConfirm 时启用）。
  const [confirmTarget, setConfirmTarget] = useState<ToolInvocationDetail | null>(null);
  const [confirmError, setConfirmError] = useState<string | undefined>(undefined);
  // B1-08：invocation id → 目标对象元信息（卡片拉取详情时顺带汇总，零额外请求）。
  const [detailsById, setDetailsById] = useState<Record<number, EvidenceTargetMeta>>({});

  useEffect(
    () => () => {
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    },
    []
  );

  const handleCopyAnswer = useCallback(async () => {
    try {
      if (!navigator.clipboard?.writeText) return;
      await navigator.clipboard.writeText(message.content);
      setCopied(true);
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
      copyTimerRef.current = window.setTimeout(() => {
        setCopied(false);
        copyTimerRef.current = null;
      }, 1600);
    } catch {
      // 剪贴板不可用：静默降级，不打断阅读。
    }
  }, [message.content]);

  /**
   * 待审批条目（M1-05）：由 SSE 事件配对得到，只取「已提交审批且有 invocationId」的写调用。
   * 事件缺失时不产生卡片（降级为纯文本回答），与时间线同一套配对规则。
   */
  const pendingEntries = useMemo(
    () =>
      mergeToolEvents(message.toolEvents).filter(
        entry => entry.status === 'pending' && typeof entry.invocationId === 'number' && entry.invocationId > 0
      ),
    [message.toolEvents]
  );

  if (message.role === 'user') {
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <div
          style={{
            maxWidth: '80%',
            padding: '10px 14px',
            borderRadius: 16,
            background: token.colorFillTertiary,
            color: token.colorText,
            fontSize: 14,
            lineHeight: 1.7,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {message.content}
        </div>
      </div>
    );
  }

  const providerSourceLabel =
    PROVIDER_SOURCE_LABELS[message.providerInfo?.providerSource ?? ''] ||
    message.providerInfo?.providerSource ||
    '未知';

  return (
    <div className="ai-chat-item" style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
      <Avatar
        size={28}
        icon={<Bot size={16} />}
        style={{
          flexShrink: 0,
          background: token.colorPrimaryBg,
          color: token.colorPrimary,
        }}
      />
      <div style={{ flex: 1, minWidth: 0 }}>
        {message.content ? (
          <MarkdownMessage content={message.content} streaming={message.streaming} />
        ) : message.streaming ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              color: token.colorTextSecondary,
              fontSize: 13,
              padding: '2px 0',
            }}
          >
            <Spin size="small" />
            <span>检索知识库并生成回答…</span>
          </div>
        ) : null}

        {message.error ? (
          <Alert
            type="error"
            showIcon
            message={message.error}
            style={{ marginTop: message.content ? 8 : 0 }}
          />
        ) : null}

        {/* 待审批卡片（M1-05）：写工具提交审批后可见；状态由卡片按需拉取（不轮询）。 */}
        {pendingEntries.map(entry => (
          <ToolApprovalCard
            key={`approval-${entry.invocationId}`}
            invocationId={entry.invocationId as number}
            tool={entry.tool}
            provider={entry.provider}
            server={entry.server}
            onOpenApproval={onOpenApproval}
            onLoaded={detail => {
              setDetailsById(prev => ({
                ...prev,
                [detail.id]: {
                  targetType: detail.targetType,
                  targetId: detail.targetId,
                  supportRef: detail.supportRef,
                },
              }));
            }}
            onRequestConfirm={
              onRequestConfirm
                ? detail => {
                    setConfirmError(undefined);
                    setConfirmTarget(detail);
                  }
                : undefined
            }
          />
        ))}

        {/* B1-08 运行状态条：仅在收到 v2 `run_started` 时渲染（旧后端不产生 → 无变化）。 */}
        <RunStatusBar
          info={
            message.run
              ? {
                  ...message.run,
                  providerLabel: message.providerInfo?.provider
                    ? providerLabel(message.providerInfo.provider)
                    : undefined,
                }
              : undefined
          }
        />

        {/* 过程证据（B1-08）：有 v2 步骤时用证据面板（含目标/依据），否则回退 M1-04 时间线。
            pending 条目由上方卡片承载，两者都不重复展示。 */}
        {message.steps && message.steps.length > 0 ? (
          <EvidencePanel
            steps={message.steps}
            toolEvents={message.toolEvents}
            detailsByInvocation={detailsById}
          />
        ) : (
          <ToolCallTimeline events={message.toolEvents} hideStatuses={PENDING_HIDDEN_STATUSES} />
        )}

        {/* B1-07：对话内确认抽屉（仅注入 onRequestConfirm 时可用；决策走 B1-05 状态机）。 */}
        {onRequestConfirm ? (
          <ConfirmationDrawer
            open={confirmTarget !== null}
            detail={confirmTarget}
            errorMessage={confirmError}
            onClose={() => {
              setConfirmTarget(null);
              setConfirmError(undefined);
            }}
            onDecision={async (approve, reason) => {
              if (!confirmTarget) return;
              try {
                await aiApproveTool(confirmTarget.id, { approve, reason: reason || undefined });
                setConfirmTarget(null);
                setConfirmError(undefined);
                onConfirmed?.();
              } catch (err) {
                // 过期/冲突/队列不可用等：错误语义由后端给出，抽屉内可见地失败并保留上下文。
                setConfirmError((err as Error)?.message || '确认操作失败，请稍后重试');
              }
            }}
          />
        ) : null}

        {message.sources && message.sources.length > 0 ? (
          <SourceList sources={message.sources} />
        ) : null}

        {!message.streaming && message.content ? (
          <div
            className="ai-chat-actions"
            style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 8, flexWrap: 'wrap' }}
          >
            <Button
              type="text"
              size="small"
              icon={copied ? <Check size={12} /> : <Copy size={12} />}
              onClick={handleCopyAnswer}
              style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
            >
              {copied ? '已复制' : '复制回答'}
            </Button>
            {message.providerInfo?.provider ? (
              <Tooltip title={`生效来源：${providerSourceLabel}`}>
                <span style={{ fontSize: 12, color: token.colorTextSecondary }}>
                  由 {providerLabel(message.providerInfo.provider)} 回答
                </span>
              </Tooltip>
            ) : null}
            {canPromoteToArticle(message) ? (
              <Tooltip title="未引用知识库文章，可沉淀为知识">
                <Button
                  type="text"
                  size="small"
                  icon={<FileText size={12} />}
                  onClick={() => onCreateArticle(message)}
                  style={{ height: 'auto', padding: 0, fontSize: 12, color: token.colorTextSecondary }}
                >
                  补充为知识文章
                </Button>
              </Tooltip>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
};

// objectType → Tag 颜色映射（业务实体类型着色，未识别类型回退蓝色）
const OBJECT_TYPE_TAG_COLORS: Record<string, string> = {
  ticket: 'orange',
  incident: 'red',
  problem: 'purple',
  change: 'geekblue',
  release: 'cyan',
  ci: 'magenta',
  kb: 'blue',
  knowledge: 'blue',
};

const objectTypeTagColor = (objectType?: string): string =>
  (objectType && OBJECT_TYPE_TAG_COLORS[objectType.toLowerCase()]) || 'blue';

/** 助手消息内的引用来源（objectType Tag + 标题 + 相关度 + 可展开片段）。 */
const SourceList: React.FC<{ sources: RagAnswer[] }> = ({ sources }) => {
  const { token } = theme.useToken();
  return (
    <div
      style={{
        marginTop: 10,
        padding: '8px 12px',
        borderRadius: 10,
        background: token.colorFillQuaternary,
        border: `1px solid ${token.colorBorderSecondary}`,
      }}
    >
      <Text type="secondary" style={{ fontSize: 12 }}>
        引用来源 · {sources.length}
      </Text>
      <ul style={{ paddingLeft: 18, marginTop: 6, marginBottom: 0 }}>
        {sources.map((s, idx) => {
          const title = s.title || s.snippet?.slice(0, 30) || `来源 ${idx + 1}`;
          const scoreLabel =
            typeof s.score === 'number' ? `${(s.score * 100).toFixed(0)}%` : undefined;
          return (
            <li key={`${s.objectType}-${s.id}-${idx}`} style={{ marginBottom: 6 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <Tag color={objectTypeTagColor(s.objectType)} style={{ marginInlineEnd: 0 }}>
                  {s.objectType || 'source'}
                </Tag>
                <Text strong style={{ fontSize: 13 }}>
                  {title}
                </Text>
                {scoreLabel ? (
                  <Tag color="green" style={{ marginInlineEnd: 0 }}>
                    相关度 {scoreLabel}
                  </Tag>
                ) : null}
              </div>
              {s.snippet ? (
                <Typography.Paragraph
                  type="secondary"
                  style={{ marginTop: 4, marginBottom: 0, fontSize: 12 }}
                  ellipsis={{ rows: 2, expandable: true, symbol: '展开' }}
                >
                  {s.snippet}
                </Typography.Paragraph>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
};

const AIChat: React.FC = () => {
  const navigate = useNavigate();
  const { token } = theme.useToken();
  const { hasPermission } = usePermissions();
  // 「设为我的默认 / 清除默认」写的是 PUT /ai/user-preference，仍是 system:write（P1 只放开读端点）
  // → 条件渲染，避免普通用户点击后 403 报错。
  const canManageProviderPreference = hasPermission('system', 'write');

  // <768px 默认收起侧栏（不做 overlay，仅初始值）。
  const [sidebarOpen, setSidebarOpen] = useState(() =>
    typeof window === 'undefined' ? true : window.innerWidth >= 768
  );
  const [query, setQuery] = useState('');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [convId, setConvId] = useState<number | undefined>(undefined);
  const [streaming, setStreaming] = useState(false);
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [loadingConvs, setLoadingConvs] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  /** 自动滚动开关：用户主动上滚且距底 > 120px 时置 false，回到近底部恢复。 */
  const autoScrollRef = useRef(true);

  // 多 Provider 会话选择器（FE-4 + P1）：开关开启 + ≥2 个可用实例时渲染（读端点已降为 ai:read）。
  const feature = useLLMProviderFeature();
  const { refresh: refreshFeature } = feature;
  const canSwitchProvider = feature.enabled && feature.providers.length > 1;
  const [selectedProvider, setSelectedProvider] = useState<string | undefined>(undefined);
  const [providerNotice, setProviderNotice] = useState<string | null>(null);

  // B2-04 工作区 Bot 选择器：候选列表按当前角色 audience 过滤（端点未开启 → 空数组 →
  // 选择器不渲染，行为与引入该能力前一致）。选择仅对**新会话**生效。
  const [bots, setBots] = useState<BotOption[]>([]);
  const [selectedBotId, setSelectedBotId] = useState<number | null>(null);
  useEffect(() => {
    let alive = true;
    AIApi.listVisibleBots()
      .then(list => {
        if (alive) setBots(list);
      })
      .catch(() => {
        // listVisibleBots 内部已兜底为空数组；此分支仅防未预期异常。
        if (alive) setBots([]);
      });
    return () => {
      alive = false;
    };
  }, []);

  const chatCss = useMemo(() => buildChatCss(token), [token]);

  // 整页锁定：挂载时把外壳改造成 100vh 纵向 flex 链，卸载时原样还原（布局提交前同步执行，无跳变）。
  useLayoutEffect(() => {
    return lockChatPageLayout();
  }, []);

  const providerLabel = useCallback(
    (key?: string) => feature.providers.find(p => p.key === key)?.displayName || key || '',
    [feature.providers]
  );

  // 「补充为知识文章」：把回答 Markdown 原文（含标题推导）带进新建页。
  // 走 /knowledge/articles/new 而不是历史别名 /knowledge/articles/create —— 后者的重定向
  // 只保留 query、会丢弃 location.state（routes/legacy-redirects.tsx）。
  const handlePromoteToArticle = useCallback(
    (target: ChatMessage) => {
      navigate('/knowledge/articles/new', { state: buildArticlePrefillState(target.content) });
    },
    [navigate]
  );

  // 标题栏「保存成文章」：整段会话。state 只带会话 ID —— 会话正文动辄上万字，
  // 塞进 history.state 会拖慢导航且逼近浏览器体积上限；拉取与组装都推迟到新建页
  // （GET /ai/conversations/:id → Markdown，见 lib/knowledge/conversation-article）。
  const handleSaveConversationAsArticle = useCallback(() => {
    if (!convId || streaming || messages.length === 0) return;
    navigate('/knowledge/articles/new', {
      state: buildConversationArticlePrefillState(convId),
    });
  }, [convId, messages.length, navigate, streaming]);

  /**
   * 跳转外置审批页（M1-05）。一期不提供对话内联确认：卡片只提示状态并把人送到审批页
   * （内联确认依赖阶段一 B1 的确认状态机，属二期）。
   */
  const handleOpenApproval = useCallback(
    (_invocationId: number) => {
      navigate('/ai/approval');
    },
    [navigate]
  );

  /**
   * B1-07：启用对话内确认入口。
   *
   * 抽屉由消息项本地托管（确认单详情在卡片拉取后直接传入，避免二次请求）；
   * 这里只保留调用点用于埋点/扩展，不改变渲染路径。
   */
  const handleRequestConfirm = useCallback((_invocationId: number) => undefined, []);

  // 所选实例在可用列表中消失（被禁用/删除）→ 清除选择并提示，回退默认（§6.2 场景 5）。
  useEffect(() => {
    if (!selectedProvider || !feature.ready || !feature.enabled) return;
    if (!feature.providers.some(p => p.key === selectedProvider)) {
      setSelectedProvider(undefined);
      setProviderNotice('所选 Provider 已不可用，已回退到默认实例');
    }
  }, [feature.enabled, feature.providers, feature.ready, selectedProvider]);

  const handleSetPersonalDefault = useCallback(async () => {
    if (!selectedProvider) return;
    try {
      await feature.setPreference(selectedProvider);
      antdMessage.success(`已将「${providerLabel(selectedProvider)}」设为我的默认`);
    } catch (err) {
      antdMessage.error(describeLLMProviderError(err, '设置个人默认失败'));
    }
  }, [feature, providerLabel, selectedProvider]);

  const handleClearPersonalDefault = useCallback(async () => {
    try {
      await feature.setPreference(null);
      antdMessage.success('已清除个人默认，跟随租户默认');
    } catch (err) {
      antdMessage.error(describeLLMProviderError(err, '清除个人默认失败'));
    }
  }, [feature]);

  // 加载会话列表
  const loadConversations = useCallback(async () => {
    setLoadingConvs(true);
    try {
      const list = await listConversations();
      setConversations(list);
    } catch {
      // silent
    } finally {
      setLoadingConvs(false);
    }
  }, []);

  useEffect(() => {
    void loadConversations();
  }, [loadConversations]);

  // 解析存入的消息内容（assistant 的 content 可能是 JSON {answer, sources}）
  const parseMessageContent = useCallback((role: string, content: string): string => {
    if (role !== 'assistant') return content;
    try {
      const parsed = JSON.parse(content);
      if (parsed.answer) return parsed.answer;
    } catch {
      // plain text
    }
    return content;
  }, []);

  // 切换到指定会话，加载历史消息
  const switchToConversation = useCallback(
    async (conv: ConversationSummary) => {
      if (streaming) return;
      try {
        const msgs = await getConversationMessages(conv.id);
        const mapped: ChatMessage[] = msgs.map(m => ({
          id: `saved-${m.id}`,
          role: m.role === 'user' ? 'user' : 'assistant',
          content: parseMessageContent(m.role, m.content),
          createdAt: m.createdAt,
        }));
        autoScrollRef.current = true;
        setMessages(mapped);
        setConvId(conv.id);
      } catch {
        antdMessage.error('加载会话失败');
      }
    },
    [parseMessageContent, streaming]
  );

  // 开始新会话
  const startNewConversation = useCallback(() => {
    if (streaming) return;
    autoScrollRef.current = true;
    setMessages([]);
    setConvId(undefined);
  }, [streaming]);

  // 删除会话
  const handleDeleteConversation = useCallback(
    async (e: React.MouseEvent, convIdToDelete: number) => {
      e.stopPropagation();
      try {
        await deleteConversation(convIdToDelete);
        setConversations(prev => prev.filter(c => c.id !== convIdToDelete));
        if (convId === convIdToDelete) {
          setMessages([]);
          setConvId(undefined);
        }
        antdMessage.success('会话已删除');
      } catch {
        antdMessage.error('删除失败');
      }
    },
    [convId]
  );

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, []);

  // 用户主动上滚 → 暂停自动滚动；回到近底部 → 恢复。
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    autoScrollRef.current = distanceToBottom <= AUTO_SCROLL_THRESHOLD;
  }, []);

  useEffect(() => {
    if (!autoScrollRef.current) return;
    scrollToBottom();
  }, [messages, scrollToBottom]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const updateAssistant = useCallback((assistantId: string, patch: Partial<ChatMessage>) => {
    setMessages(prev => prev.map(m => (m.id === assistantId ? { ...m, ...patch } : m)));
  }, []);

  const appendAssistantContent = useCallback((assistantId: string, delta: string) => {
    setMessages(prev =>
      prev.map(m => (m.id === assistantId ? { ...m, content: m.content + delta } : m))
    );
  }, []);

  /**
   * 追加工具事件（M1-04）：按到达顺序保存，由 ToolCallTimeline 做 started/终态配对。
   * 上限 50 条仅用于兜底内存（超长会话/异常服务端），超出丢弃最早的事件——
   * 时间线是过程视图，丢失早期条目不改变最终回答。
   */
  const appendToolEvent = useCallback((assistantId: string, event: AIToolStreamEvent) => {
    setMessages(prev =>
      prev.map(m => {
        if (m.id !== assistantId) return m;
        const toolEvents = [...(m.toolEvents ?? []), event];
        // pendingApprovals：写工具提交审批后生成卡片；同一 invocation 只保留一次。
        const pendingApprovals =
          event.status === 'pending' && typeof event.id === 'number' && event.id > 0
            ? [...(m.pendingApprovals ?? []), event.id].filter((id, idx, arr) => arr.indexOf(id) === idx)
            : m.pendingApprovals;
        return { ...m, toolEvents: toolEvents.slice(-50), pendingApprovals };
      })
    );
  }, []);

  /**
   * 追加 v2 步骤事件（B1-08）：按 `stepIndex` 去重（B1-03 兼容层可能重放同一帧），
   * 上限 100 条兜底内存；运行快照同步更新步骤数与最近步骤耗时/类型。
   */
  const appendRunStep = useCallback((assistantId: string, step: AIRunStepEvent) => {
    setMessages(prev =>
      prev.map(m => {
        if (m.id !== assistantId) return m;
        const steps = [...(m.steps ?? []).filter(s => s.stepIndex !== step.stepIndex), step]
          .sort((a, b) => a.stepIndex - b.stepIndex)
          .slice(-100);
        return {
          ...m,
          steps,
          run: {
            ...(m.run ?? { status: 'running' as const }),
            stepCount: steps.length,
            lastStepType: step.type,
            lastDurationMs: step.durationMs,
          },
        };
      })
    );
  }, []);

  const runStreaming = useCallback(
    async (userMsg: ChatMessage, assistantId: string) => {
      const controller = new AbortController();
      abortRef.current = controller;
      setStreaming(true);

      try {
        const finalConvId = await aiChatStream(
          {
            query: userMsg.content,
            conversationId: convId,
            limit: 5,
            provider: selectedProvider,
            botId: selectedBotId ?? undefined,
            signal: controller.signal,
          },
          {
            onSources: sources => {
              updateAssistant(assistantId, { sources });
            },
            onDelta: delta => {
              appendAssistantContent(assistantId, delta);
            },
            onToolEvent: event => {
              appendToolEvent(assistantId, event);
            },
            // B1-08：v2 运行事件（旧后端不发送 → 不产生状态条/证据面板，渲染零影响）。
            onRunStarted: run => {
              updateAssistant(assistantId, {
                run: { runId: run.runId, status: 'running', startedAt: Date.now() },
              });
            },
            onStep: step => {
              appendRunStep(assistantId, step);
            },
            onDone: (newConvId, info) => {
              if (newConvId) {
                setConvId(newConvId);
                void loadConversations(); // 刷新侧边栏
              }
              setMessages(prev =>
                prev.map(m =>
                  m.id === assistantId
                    ? {
                        ...m,
                        streaming: false,
                        providerInfo: info,
                        run: m.run ? { ...m.run, status: 'done' } : undefined,
                      }
                    : m
                )
              );
            },
            onError: msg => {
              setMessages(prev =>
                prev.map(m =>
                  m.id === assistantId
                    ? {
                        ...m,
                        streaming: false,
                        error: msg,
                        run: m.run ? { ...m.run, status: 'failed' } : undefined,
                      }
                    : m
                )
              );
            },
          }
        );
        if (finalConvId) {
          setConvId(finalConvId);
          void loadConversations();
        }
      } catch (err) {
        const aborted = (err as Error)?.name === 'AbortError';
        if (aborted) {
          updateAssistant(assistantId, { streaming: false, error: '已停止生成' });
          return;
        }
        try {
          const res = await AIApi.chat({
            query: userMsg.content,
            conversationId: convId,
            limit: 5,
            provider: selectedProvider,
            botId: selectedBotId ?? undefined,
          });
          const answers: unknown[] = Array.isArray(res?.answers) ? res.answers : [];
          const fallbackText = answers
            .map(a => (typeof a === 'string' ? a : JSON.stringify(a)))
            .join('\n\n');
          updateAssistant(assistantId, {
            streaming: false,
            content: fallbackText || '抱歉，我没有找到相关的答案。',
            sources: answers.filter((a): a is RagAnswer => typeof a === 'object' && a !== null),
          });
          if (res?.conversationId) {
            setConvId(res.conversationId);
            void loadConversations();
          }
        } catch (fallbackErr) {
          const fallbackMsg = describeLLMProviderError(fallbackErr, '流式请求与降级请求均失败');
          updateAssistant(assistantId, { streaming: false, error: fallbackMsg });
          if (llmProviderErrorCode(fallbackErr) === LLM_PROVIDER_DISABLED) {
            // 实例被禁用：清掉会话选择，回退租户默认，并刷新可用列表。
            setSelectedProvider(undefined);
            setProviderNotice('所选 Provider 已禁用，已回退到默认实例，请重新发送');
            void refreshFeature();
          }
          antdMessage.error(fallbackMsg);
        }
      } finally {
        abortRef.current = null;
        setStreaming(false);
      }
    },
    [
      appendAssistantContent,
      convId,
      loadConversations,
      refreshFeature,
      selectedBotId,
      selectedProvider,
      updateAssistant,
    ]
  );

  // 支持带参调用：空状态建议卡片直接以该文案发送。
  const handleSend = useCallback(
    (raw?: string) => {
      const trimmed = (typeof raw === 'string' ? raw : query).trim();
      if (!trimmed || streaming) return;

      autoScrollRef.current = true;
      const userMsg: ChatMessage = {
        id: nextId(),
        role: 'user',
        content: trimmed,
        createdAt: new Date().toISOString(),
      };
      const assistantMsg: ChatMessage = {
        id: nextId(),
        role: 'assistant',
        content: '',
        createdAt: new Date().toISOString(),
        streaming: true,
      };
      setMessages(prev => [...prev, userMsg, assistantMsg]);
      setQuery('');
      void runStreaming(userMsg, assistantMsg.id);
    },
    [query, streaming, runStreaming]
  );

  const handleStop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const handleClear = useCallback(() => {
    if (streaming) return;
    autoScrollRef.current = true;
    setMessages([]);
    setConvId(undefined);
  }, [streaming]);

  // Enter 发送 / Shift+Enter 换行；中文输入法合成期（isComposing）不触发发送。
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key !== 'Enter' || e.shiftKey) return;
      if (e.nativeEvent.isComposing) return;
      e.preventDefault();
      handleSend();
    },
    [handleSend]
  );

  const isEmpty = useMemo(() => messages.length === 0, [messages]);

  const currentConvTitle = useMemo(() => {
    if (!convId) return null;
    return conversations.find(c => c.id === convId)?.title || `会话 #${convId}`;
  }, [convId, conversations]);

  // 格式化时间
  const formatTime = (iso: string) => {
    try {
      const d = new Date(iso);
      const now = new Date();
      const diff = now.getTime() - d.getTime();
      if (diff < 60_000) return '刚刚';
      if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
      if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
      return d.toLocaleDateString('zh-CN');
    } catch {
      return '';
    }
  };

  return (
    <div data-ai-chat-page style={{ display: 'flex', gap: 12, flex: '1 1 auto', minHeight: 0 }}>
      {/* 局部 hover 反馈（不新增全局样式文件） */}
      <style>{chatCss}</style>

      {/* 左侧会话历史栏 */}
      <aside
        style={{
          width: sidebarOpen ? 268 : 0,
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          visibility: sidebarOpen ? 'visible' : 'hidden',
          transition: 'width 0.2s ease',
          background: token.colorFillQuaternary,
          borderRadius: 12,
          border: sidebarOpen ? `1px solid ${token.colorBorderSecondary}` : 'none',
        }}
      >
        <div style={{ width: 268, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '10px 10px 6px', display: 'flex', alignItems: 'center', gap: 6 }}>
            <MessageSquare size={15} color={token.colorTextSecondary} />
            <Text strong style={{ fontSize: 13, flex: 1 }}>
              会话历史
            </Text>
            <Tooltip title="新建会话">
              <Button
                type="text"
                size="small"
                icon={<Plus size={14} />}
                onClick={startNewConversation}
                disabled={streaming}
              />
            </Tooltip>
            <Tooltip title="收起侧栏">
              <Button
                type="text"
                size="small"
                icon={<ChevronLeft size={14} />}
                onClick={() => setSidebarOpen(false)}
              />
            </Tooltip>
          </div>

          <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingBottom: 8 }}>
            {loadingConvs ? (
              <div style={{ textAlign: 'center', padding: 24 }}>
                <Spin size="small" />
              </div>
            ) : conversations.length === 0 ? (
              <div style={{ padding: 16, textAlign: 'center' }}>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  暂无会话记录
                </Text>
              </div>
            ) : (
              conversations.map(conv => (
                <div
                  key={conv.id}
                  className="ai-chat-conv"
                  data-selected={conv.id === convId}
                  onClick={() => switchToConversation(conv)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '8px 10px',
                    margin: '0 6px 2px',
                    cursor: 'pointer',
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <Text style={{ fontSize: 13, display: 'block' }} ellipsis>
                      {conv.title || `会话 #${conv.id}`}
                    </Text>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                      <Clock size={10} color={token.colorTextTertiary} />
                      <Text type="secondary" style={{ fontSize: 11 }}>
                        {formatTime(conv.createdAt)}
                      </Text>
                    </div>
                  </div>
                  <Popconfirm
                    title="删除此会话？"
                    onConfirm={e => handleDeleteConversation(e as React.MouseEvent, conv.id)}
                    okText="删除"
                    cancelText="取消"
                    okButtonProps={{ danger: true, size: 'small' }}
                  >
                    <Button
                      type="text"
                      size="small"
                      danger
                      icon={<Trash2 size={12} />}
                      onClick={e => e.stopPropagation()}
                      style={{ opacity: 0.6 }}
                    />
                  </Popconfirm>
                </div>
              ))
            )}
          </div>
        </div>
      </aside>

      {/* 主聊天区：工具条 / 消息滚动区 / 输入坞 */}
      <section
        style={{
          flex: 1,
          minWidth: 0,
          display: 'flex',
          flexDirection: 'column',
          background: token.colorBgContainer,
          borderRadius: 12,
          border: `1px solid ${token.colorBorderSecondary}`,
          overflow: 'hidden',
        }}
      >
        {/* a. 顶部工具条（约 48px，底部 1px 分隔线） */}
        <header
          style={{
            height: 48,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '0 12px',
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          {!sidebarOpen ? (
            <Tooltip title="展开会话历史">
              <Button
                type="text"
                size="small"
                icon={<MessageSquare size={15} />}
                onClick={() => setSidebarOpen(true)}
              />
            </Tooltip>
          ) : null}
          <Bot size={18} color={token.colorPrimary} />
          <Text strong style={{ fontSize: 14, whiteSpace: 'nowrap' }}>
            AI 助手
          </Text>
          {currentConvTitle ? (
            <Tag
              style={{
                maxWidth: 200,
                marginInlineEnd: 0,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {currentConvTitle}
            </Tag>
          ) : null}

          <div
            style={{
              marginLeft: 'auto',
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              flexShrink: 0,
            }}
          >
            {/* B2-04 Bot 选择器：已有会话时锁定（切换仅影响新会话，不回溯改写历史归属） */}
            <BotSelector
              bots={bots}
              value={selectedBotId}
              onChange={setSelectedBotId}
              locked={Boolean(convId)}
            />
            {/* Provider 选择器：feature.enabled && providers.length > 1（P1 起普通用户同样可见） */}
            {canSwitchProvider ? (
              <>
                <Select
                  size="small"
                  style={{ minWidth: 160, maxWidth: 240 }}
                  value={selectedProvider}
                  allowClear
                  placeholder={
                    feature.preference?.effectiveProviderKey
                      ? `跟随默认 · ${providerLabel(feature.preference.effectiveProviderKey)}`
                      : '跟随默认实例'
                  }
                  options={feature.providers.map(p => ({
                    value: p.key,
                    label: p.implemented ? p.displayName : `${p.displayName}（未接入）`,
                    disabled: !p.implemented,
                  }))}
                  onChange={value => {
                    setSelectedProvider(value);
                    setProviderNotice(null);
                  }}
                />
                {/* 写端点（PUT /ai/user-preference）仍需 system:write */}
                {canManageProviderPreference &&
                selectedProvider &&
                feature.preference?.providerKey !== selectedProvider ? (
                  <Tooltip title="设为我的默认（仅影响你自己的新会话）">
                    <Button
                      size="small"
                      type="text"
                      icon={<Star size={13} />}
                      onClick={handleSetPersonalDefault}
                    />
                  </Tooltip>
                ) : null}
                {canManageProviderPreference && feature.preference?.providerKey ? (
                  <Tooltip
                    title={`清除我的默认（当前：${providerLabel(feature.preference.providerKey)}）`}
                  >
                    <Button size="small" type="text" onClick={handleClearPersonalDefault}>
                      清除默认
                    </Button>
                  </Tooltip>
                ) : null}
              </>
            ) : null}

            {streaming ? (
              <Tag color="processing" icon={<LoaderCircle size={12} className="animate-spin" />}>
                生成中
              </Tag>
            ) : null}
            {streaming ? (
              <Button size="small" danger icon={<StopCircle size={14} />} onClick={handleStop}>
                停止生成
              </Button>
            ) : null}
            <Tooltip
              title={
                convId
                  ? '把整段会话整理成一篇知识文章草稿'
                  : '完成一轮问答后可将整段会话保存为文章'
              }
            >
              <Button
                size="small"
                icon={<FileText size={14} />}
                onClick={handleSaveConversationAsArticle}
                disabled={!convId || streaming || isEmpty}
              >
                保存成文章
              </Button>
            </Tooltip>
            <Tooltip title="清空当前对话">
              <Button size="small" icon={<Eraser size={14} />} onClick={handleClear} disabled={streaming}>
                清空对话
              </Button>
            </Tooltip>
          </div>
        </header>

        {/* b. 消息滚动区（ref 绑在真正的滚动容器上） */}
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '20px 20px 8px' }}
        >
          {isEmpty ? (
            <div style={{ maxWidth: 720, margin: '0 auto', paddingTop: 32, textAlign: 'center' }}>
              <div
                style={{
                  width: 56,
                  height: 56,
                  margin: '0 auto',
                  borderRadius: '50%',
                  background: token.colorPrimaryBg,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <Bot size={28} color={token.colorPrimary} />
              </div>
              <div style={{ fontSize: 18, fontWeight: 600, color: token.colorTextHeading, marginTop: 12 }}>
                AI 助手
              </div>
              <div style={{ fontSize: 13, color: token.colorTextSecondary, marginTop: 4 }}>
                结合知识库为你回答，并给出引用来源。选择左侧会话可继续历史对话。
              </div>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
                  gap: 12,
                  marginTop: 28,
                }}
              >
                {SUGGESTIONS.map(text => (
                  <Button
                    key={text}
                    className="ai-chat-suggest"
                    onClick={() => handleSend(text)}
                    disabled={streaming}
                  >
                    {text}
                  </Button>
                ))}
              </div>
            </div>
          ) : (
            <div
              style={{
                maxWidth: CONTENT_MAX_WIDTH,
                margin: '0 auto',
                display: 'flex',
                flexDirection: 'column',
                gap: 22,
              }}
            >
              {messages.map(item => (
                <ChatMessageItem
                  key={item.id}
                  message={item}
                  providerLabel={providerLabel}
                  onCreateArticle={handlePromoteToArticle}
                  onOpenApproval={handleOpenApproval}
                  // B1-07：对话内确认（确认抽屉）。入口只在确认单仍 pending 且未过期时出现。
                  onRequestConfirm={handleRequestConfirm}
                />
              ))}
            </div>
          )}
        </div>

        {/* c. 输入坞（与消息列同宽居中） */}
        <div
          style={{
            flexShrink: 0,
            padding: '10px 16px 8px',
            borderTop: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          {providerNotice ? (
            <Alert
              type="warning"
              showIcon
              closable
              message={providerNotice}
              onClose={() => setProviderNotice(null)}
              style={{ maxWidth: CONTENT_MAX_WIDTH, margin: '0 auto 8px' }}
            />
          ) : null}

          <div style={{ maxWidth: CONTENT_MAX_WIDTH, margin: '0 auto' }}>
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-end',
                gap: 8,
                padding: '6px 6px 6px 14px',
                borderRadius: 22,
                border: `1px solid ${token.colorBorderSecondary}`,
                background: token.colorBgContainer,
                boxShadow: token.boxShadowTertiary,
              }}
            >
              <Input.TextArea
                value={query}
                onChange={e => setQuery(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="请输入你的问题…"
                autoSize={{ minRows: 1, maxRows: 8 }}
                variant="borderless"
                style={{ padding: '6px 0', resize: 'none', fontSize: 14, lineHeight: 1.6 }}
              />
              {streaming ? (
                <Tooltip title="停止生成">
                  <Button
                    shape="circle"
                    danger
                    icon={<StopCircle size={16} />}
                    onClick={handleStop}
                    aria-label="停止生成"
                  />
                </Tooltip>
              ) : (
                <Button
                  shape="circle"
                  type="primary"
                  icon={<Send size={16} />}
                  onClick={() => handleSend()}
                  disabled={!query.trim()}
                  aria-label="发送"
                />
              )}
            </div>
            <div
              style={{
                fontSize: 12,
                color: token.colorTextSecondary,
                textAlign: 'center',
                marginTop: 6,
              }}
            >
              Enter 发送，Shift+Enter 换行 · 回答由 AI 生成，请自行甄别
            </div>
          </div>
        </div>
      </section>
    </div>
  );
};

export default AIChat;
