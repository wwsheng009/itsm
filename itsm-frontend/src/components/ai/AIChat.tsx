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
 *
 * 渲染层：主区 = 顶部工具条 + 消息滚动区（消息列 max-width 820 居中）+ 底部输入坞。
 */

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
  aiChatStream,
  deleteConversation,
  getConversationMessages,
  listConversations,
  type ConversationSummary,
  type RagAnswer,
} from '@/lib/api/ai-api';
import {
  LLM_PROVIDER_DISABLED,
  describeLLMProviderError,
  llmProviderErrorCode,
} from '@/lib/api/llm-provider-api';
import { useLLMProviderFeature } from '@/lib/hooks/use-llm-provider-feature';
import { usePermissions } from '@/lib/hooks/use-permissions';
import MarkdownMessage from './MarkdownMessage';

const { Text } = Typography;

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
`;

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
  onCreateArticle: () => void;
}

/** 单条消息：用户 = 右对齐气泡；助手 = 头像 + Markdown 正文 + 操作区。 */
const ChatMessageItem: React.FC<ChatMessageItemProps> = ({
  message,
  providerLabel,
  onCreateArticle,
}) => {
  const { token } = theme.useToken();
  const [copied, setCopied] = useState(false);
  const copyTimerRef = useRef<number | null>(null);

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
                  onClick={onCreateArticle}
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

  const chatCss = useMemo(() => buildChatCss(token), [token]);

  const providerLabel = useCallback(
    (key?: string) => feature.providers.find(p => p.key === key)?.displayName || key || '',
    [feature.providers]
  );

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
            signal: controller.signal,
          },
          {
            onSources: sources => {
              updateAssistant(assistantId, { sources });
            },
            onDelta: delta => {
              appendAssistantContent(assistantId, delta);
            },
            onDone: (newConvId, info) => {
              if (newConvId) {
                setConvId(newConvId);
                void loadConversations(); // 刷新侧边栏
              }
              updateAssistant(assistantId, { streaming: false, providerInfo: info });
            },
            onError: msg => {
              updateAssistant(assistantId, { streaming: false, error: msg });
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
    [appendAssistantContent, convId, loadConversations, refreshFeature, selectedProvider, updateAssistant]
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
    <div style={{ display: 'flex', gap: 12, height: 'calc(100vh - 150px)', minHeight: 480 }}>
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
                  onCreateArticle={() => navigate('/knowledge/articles/create')}
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
