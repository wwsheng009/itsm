import { useNavigate } from 'react-router';

/**
 * AI 智能助手 — 流式回答 + 引用来源 + 会话历史
 *
 * 交互特性：
 *  - 左侧边栏显示会话历史列表（按时间倒序），支持切换/新建/删除
 *  - SSE 推送 token，助手气泡实时增长；期间用户可"停止生成"取消流。
 *  - 每条助手消息挂一组引用来源（RagAnswer），点击展开可查看片段与得分。
 *  - 流失败时自动降级为一次性 chat 调用，保证有可读回答。
 */

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Avatar,
  Button,
  Card,
  Divider,
  Dropdown,
  Empty,
  Input,
  List,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
  message as antdMessage,
} from 'antd';
import {
  Bot,
  ChevronRight,
  Clock,
  Eraser,
  FileText,
  LoaderCircle,
  MessageSquare,
  MoreHorizontal,
  Plus,
  Send,
  Star,
  StopCircle,
  Trash2,
  User,
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

const { Text, Paragraph } = Typography;

/** done 事件 providerSource → 可读来源（BE-7）。 */
const PROVIDER_SOURCE_LABELS: Record<string, string> = {
  request: '会话指定',
  user: '个人默认',
  tenant: '租户默认',
  static: '静态配置',
};

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

const AIChat: React.FC = () => {
  const navigate = useNavigate();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [query, setQuery] = useState('');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [convId, setConvId] = useState<number | undefined>(undefined);
  const [streaming, setStreaming] = useState(false);
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [loadingConvs, setLoadingConvs] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  // 多 Provider 会话选择器（FE-4）：仅系统管理员 + 开关开启 + ≥2 个可用实例时渲染。
  const feature = useLLMProviderFeature();
  const { refresh: refreshFeature } = feature;
  const canSwitchProvider = feature.enabled && feature.providers.length > 1;
  const [selectedProvider, setSelectedProvider] = useState<string | undefined>(undefined);
  const [providerNotice, setProviderNotice] = useState<string | null>(null);

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
        setMessages(mapped);
        setConvId(conv.id);
      } catch {
        antdMessage.error('加载会话失败');
      }
    },
    [streaming]
  );

  // 开始新会话
  const startNewConversation = useCallback(() => {
    if (streaming) return;
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

  // 解析存入的消息内容（assistant 的 content 可能是 JSON {answer, sources}）
  const parseMessageContent = (role: string, content: string): string => {
    if (role !== 'assistant') return content;
    try {
      const parsed = JSON.parse(content);
      if (parsed.answer) return parsed.answer;
    } catch {
      // plain text
    }
    return content;
  };

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, []);

  useEffect(() => {
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

  const handleSend = useCallback(() => {
    const trimmed = query.trim();
    if (!trimmed || streaming) return;

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
  }, [query, streaming, runStreaming]);

  const handleStop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const handleClear = useCallback(() => {
    if (streaming) return;
    setMessages([]);
    setConvId(undefined);
  }, [streaming]);

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

  // 抽取第一条用户消息作为会话标题
  const firstUserMessage = messages.find(m => m.role === 'user');

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 150px)', gap: 8 }}>
      {/* 左侧会话历史列表 */}
      <div
        style={{
          width: sidebarOpen ? 260 : 0,
          overflow: 'hidden',
          transition: 'width 0.2s',
          display: 'flex',
          flexDirection: 'column',
          background: '#fafafa',
          borderRadius: 8,
          border: '1px solid #f0f0f0',
          flexShrink: 0,
        }}
      >
        <div style={{ padding: '12px 12px 8px', display: 'flex', alignItems: 'center', gap: 8 }}>
          <MessageSquare size={15} style={{ color: '#667' }} />
          <Text strong style={{ fontSize: 13, flex: 1 }}>会话历史</Text>
          <Button
            type="text"
            size="small"
            icon={<Plus size={14} />}
            onClick={startNewConversation}
            title="新建会话"
          />
          <Button
            type="text"
            size="small"
            icon={<ChevronRight size={14} />}
            onClick={() => setSidebarOpen(false)}
            title="收起"
          />
        </div>

        <div style={{ flex: 1, overflowY: 'auto' }}>
          {loadingConvs ? (
            <div style={{ textAlign: 'center', padding: 24 }}>
              <Spin size="small" />
            </div>
          ) : conversations.length === 0 ? (
            <div style={{ padding: 16, textAlign: 'center' }}>
              <Text type="secondary" style={{ fontSize: 12 }}>暂无会话记录</Text>
            </div>
          ) : (
            <List
              size="small"
              dataSource={conversations}
              locale={{ emptyText: null }}
              renderItem={conv => (
                <List.Item
                  key={conv.id}
                  style={{
                    padding: '8px 12px',
                    cursor: 'pointer',
                    background: conv.id === convId ? '#e6f4ff' : 'transparent',
                    borderLeft: conv.id === convId ? '2px solid #3b82f6' : '2px solid transparent',
                  }}
                  onClick={() => switchToConversation(conv)}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <Text
                      style={{ fontSize: 13, display: 'block' }}
                      ellipsis
                    >
                      {conv.title || `会话 #${conv.id}`}
                    </Text>
                    <Space size={4}>
                      <Clock size={10} style={{ color: '#aaa' }} />
                      <Text type="secondary" style={{ fontSize: 11 }}>{formatTime(conv.createdAt)}</Text>
                    </Space>
                  </div>
                  <Popconfirm
                    title="删除此会话？"
                    onConfirm={e => handleDeleteConversation(e!, conv.id)}
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
                </List.Item>
              )}
            />
          )}
        </div>
      </div>

      {/* 主聊天区 */}
      <Card
        title={
          <Space size={8}>
            {!sidebarOpen && (
              <Button
                type="text"
                size="small"
                icon={<MessageSquare size={14} />}
                onClick={() => setSidebarOpen(true)}
                title="展开会话历史"
              />
            )}
            <Bot size={18} />
            <span>AI 助手</span>
            {currentConvTitle ? (
              <Tag style={{ maxWidth: 200 }}>
                <Text style={{ fontSize: 11 }} ellipsis>{currentConvTitle}</Text>
              </Tag>
            ) : null}
            {canSwitchProvider ? (
              <>
                <Select
                  size="small"
                  style={{ minWidth: 170, maxWidth: 260 }}
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
                {selectedProvider && feature.preference?.providerKey !== selectedProvider ? (
                  <Tooltip title="设为我的默认（仅影响你自己的新会话）">
                    <Button
                      size="small"
                      type="text"
                      icon={<Star size={13} />}
                      onClick={handleSetPersonalDefault}
                    />
                  </Tooltip>
                ) : null}
                {feature.preference?.providerKey ? (
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
          </Space>
        }
        extra={
          <Space>
            {streaming ? (
              <Button size="small" danger icon={<StopCircle size={14} />} onClick={handleStop}>
                停止生成
              </Button>
            ) : null}
            <Button size="small" icon={<Eraser size={14} />} onClick={handleClear} disabled={streaming}>
              清空对话
            </Button>
          </Space>
        }
        style={{ flex: 1, display: 'flex', flexDirection: 'column' }}
        styles={{
          body: {
            flex: 1,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            padding: 0,
          },
        }}
      >
        <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: '16px' }}>
          {isEmpty ? (
            <Empty
              description="发起提问，AI 将结合知识库为你回答，并给出引用来源。选择左侧会话可继续历史对话。"
              style={{ marginTop: 96 }}
            />
          ) : (
            <List
              itemLayout="horizontal"
              dataSource={messages}
              renderItem={item => (
                <List.Item style={{ border: 'none', padding: '12px 0', alignItems: 'flex-start' }}>
                  <List.Item.Meta
                    avatar={
                      <Avatar
                        icon={item.role === 'user' ? <User size={16} /> : <Bot size={16} />}
                        style={{
                          backgroundColor: item.role === 'user' ? '#1890ff' : '#52c41a',
                        }}
                      />
                    }
                    title={
                      <Space size={6}>
                        <Text strong>{item.role === 'user' ? '你' : 'AI 助手'}</Text>
                        {item.streaming ? <Tag color="processing">流式</Tag> : null}
                        {item.error ? <Tag color="error">失败</Tag> : null}
                        {item.providerInfo?.provider ? (
                          <Tooltip
                            title={`生效来源：${
                              PROVIDER_SOURCE_LABELS[item.providerInfo.providerSource ?? ''] ||
                              item.providerInfo.providerSource ||
                              '未知'
                            }`}
                          >
                            <Tag color="geekblue">
                              由 {providerLabel(item.providerInfo.provider)} 回答
                            </Tag>
                          </Tooltip>
                        ) : null}
                      </Space>
                    }
                    description={
                      <div
                        style={{
                          backgroundColor: item.role === 'user' ? '#f0f2f5' : '#f6ffed',
                          padding: '12px 14px',
                          borderRadius: 10,
                          color: '#000',
                          border: '1px solid #e6f4ff',
                        }}
                      >
                        {item.content ? (
                          <Paragraph
                            style={{ marginBottom: item.sources?.length ? 12 : 0, whiteSpace: 'pre-wrap' }}
                          >
                            {item.content}
                            {item.streaming ? <span className="ai-caret">▍</span> : null}
                          </Paragraph>
                        ) : item.streaming ? (
                          <Space size={6} align="center">
                            <Spin size="small" />
                            <Text type="secondary">检索知识库并生成回答…</Text>
                          </Space>
                        ) : null}
                        {item.error ? (
                          <Text type="danger" style={{ display: 'block', marginTop: 8 }}>
                            {item.error}
                          </Text>
                        ) : null}
                        {item.sources && item.sources.length > 0 ? (
                          <SourceList sources={item.sources} />
                        ) : null}
                        {item.role === 'assistant' &&
                        !item.streaming &&
                        !item.error &&
                        item.content &&
                        (!item.sources || item.sources.length === 0) ? (
                          <div
                            style={{
                              marginTop: 10,
                              paddingTop: 10,
                              borderTop: '1px dashed #d9d9d9',
                              display: 'flex',
                              alignItems: 'center',
                              gap: 8,
                              flexWrap: 'wrap',
                            }}
                          >
                            <Text type="secondary" style={{ fontSize: 12 }}>
                              未引用知识库文章 · 若本回答有价值，可沉淀为知识
                            </Text>
                            <Button
                              size="small"
                              type="link"
                              icon={<FileText size={12} />}
                              style={{ padding: 0, height: 'auto' }}
                              onClick={() => navigate('/knowledge/articles/create')}
                            >
                              补充为知识文章
                            </Button>
                          </div>
                        ) : null}
                      </div>
                    }
                  />
                </List.Item>
              )}
            />
          )}
        </div>

        {providerNotice ? (
          <Alert
            type="warning"
            showIcon
            closable
            message={providerNotice}
            onClose={() => setProviderNotice(null)}
            style={{ margin: '0 16px 8px' }}
          />
        ) : null}

        <Divider style={{ margin: 0 }} />

        <div style={{ padding: '16px' }}>
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder="请输入你的问题，Enter 发送"
              value={query}
              onChange={e => setQuery(e.target.value)}
              onPressEnter={handleSend}
              disabled={streaming}
              allowClear
            />
            <Button
              type="primary"
              icon={<Send size={14} />}
              onClick={handleSend}
              loading={streaming}
              disabled={streaming || !query.trim()}
            >
              发送
            </Button>
          </Space.Compact>
        </div>
      </Card>
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

const SourceList: React.FC<{ sources: RagAnswer[] }> = ({ sources }) => {
  return (
    <div style={{ marginTop: 6 }}>
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
              <Space size={6} wrap>
                <Tag color={objectTypeTagColor(s.objectType)}>{s.objectType || 'source'}</Tag>
                <Text strong style={{ fontSize: 13 }}>
                  {title}
                </Text>
                {scoreLabel ? <Tag color="green">相关度 {scoreLabel}</Tag> : null}
              </Space>
              {s.snippet ? (
                <Paragraph
                  type="secondary"
                  style={{ marginTop: 4, marginBottom: 0, fontSize: 12 }}
                  ellipsis={{ rows: 2, expandable: true, symbol: '展开' }}
                >
                  {s.snippet}
                </Paragraph>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
};

export default AIChat;
