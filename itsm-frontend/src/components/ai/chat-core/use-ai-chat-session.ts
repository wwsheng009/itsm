/**
 * D0-01 chat-core：AI 会话内核（会话列表 / 流式 SSE / 工具事件 / 确认闭环 / Provider 与 Bot 选择）。
 *
 * 由 AIChat.tsx 逐字搬运（行为等价重构），页面与后续 Dock 共用：
 * - 工作区页面（AIChat.tsx）保留整页布局锁与外壳 JSX；
 * - 观测埋点（message_sent / first_token / tool_call / stream_end / error_shown）随内核搬运。
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { message as antdMessage, theme } from 'antd';

import {
  AIApi,
  aiChatStream,
  deleteConversation,
  exportConversation,
  getConversationMessages,
  listConversations,
  type AgentBotsCapabilities,
  type AIPageContextHint,
  type AIRunStepEvent,
  type AIToolStreamEvent,
  type BotOption,
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
import {
  buildArticlePrefillState,
  buildConversationArticlePrefillState,
} from '@/lib/knowledge/ai-article-prefill';
import {
  buildAskAIRequestScope,
  readAskAIScope,
  type AskAIScope,
} from '@/lib/ai/ask-ai-scope';
import { createChatTelemetry } from '@/lib/ai/chat-telemetry';
import type { AssistantOutcome } from '@/lib/ai/outcome';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import { useAssistantPageData } from '@/lib/store/assistant-context-store';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { buildChatCss } from './chat-css';
import { downloadTextFile } from '@/lib/utils/download-text-file';
import { AUTO_SCROLL_THRESHOLD, nextId } from './constants';
import type { ChatMessage } from './types';

/**
 * @param options.consumeWorkspaceHandoff 是否消费 Dock → 工作区的会话交接。
 *   只有 `/ai/chat` 工作区实例应传 true；Dock 面板必须保持 false，
 *   否则用户在别的页面打开 Dock 时会把本该交给工作区的会话"截胡"。
 */
export function useAIChatSession(options?: { consumeWorkspaceHandoff?: boolean }) {
  const consumeWorkspaceHandoff = options?.consumeWorkspaceHandoff === true;
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = theme.useToken();
  const { hasPermission } = usePermissions();
  // 「设为我的默认 / 清除默认」写的是 PUT /ai/user-preference，仍是 system:write（P1 只放开读端点）
  // → 条件渲染，避免普通用户点击后 403 报错。
  const canManageProviderPreference = hasPermission('system', 'write');
  // 「补充为知识文章 / 保存成文章」落点为 /knowledge/articles/new（路由级 knowledge:write 已覆盖），
  // 入口级同步收敛，避免无权限用户点击后被 403 拦截。
  const canPromoteKnowledge = hasPermission('knowledge', 'write');
  // 删除会话 → DELETE /ai/conversations/:id（router/ai_routes.go:19，ai:write）。
  const canDeleteConversation = hasPermission('ai', 'write');

  // <768px 默认收起侧栏（不做 overlay，仅初始值）。
  const [sidebarOpen, setSidebarOpen] = useState(() =>
    typeof window === 'undefined' ? true : window.innerWidth >= 768
  );
  const [query, setQuery] = useState('');
  // B3-02：页面 launcher 携带的入口上下文（路由 state，一次性；可手动清除）。
  // 契约与校验见 lib/ai/ask-ai-scope（非法 state 一律按「无上下文」处理）。
  const [scopeDismissed, setScopeDismissed] = useState(false);
  const entryScope = useMemo<AskAIScope | null>(() => readAskAIScope(location.state), [location.state]);
  const activeScope = scopeDismissed ? null : entryScope;
  // D0-04：页面上下文提示（未注册路由不落字段；服务端有独立白名单归一化，仅提示/审计）。
  const { pageKey: assistantPageKey, route: assistantRoute, declaration: assistantDeclaration } =
    usePageAssistantContext();
  // D1-02：L2 运行时数据（详情页显式注册）→ 无 launcher 上下文时的请求入口上下文；
  // 服务端仍以对象预检为事实源（对象不存在/跨租户/无权 → 404 fail-closed）。
  const pageData = useAssistantPageData();
  const pageScope = useMemo<AskAIScope | null>(() => {
    if (!assistantDeclaration) return null;
    const entrypoint = assistantDeclaration.entrypoint ?? 'chat';
    const hasTarget = Boolean(pageData?.targetType && pageData?.targetId);
    if (entrypoint === 'chat' && !hasTarget) return null;
    const scope: AskAIScope = { entrypoint };
    if (pageData?.targetType && pageData.targetId) {
      scope.targetType = pageData.targetType;
      scope.targetId = pageData.targetId;
    }
    if (pageData?.summary) scope.summary = pageData.summary;
    return scope;
  }, [assistantDeclaration, pageData]);
  const effectiveScope = activeScope ?? pageScope;
  const pageContextHint = useMemo<AIPageContextHint | undefined>(
    () =>
      assistantPageKey && assistantPageKey !== 'unknown'
        ? { pageKey: assistantPageKey, route: assistantRoute }
        : undefined,
    [assistantPageKey, assistantRoute]
  );
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
  // 展示用能力块：与候选列表同一次请求下发（不新增请求），用于 bot.enabled=false 的禁用态。
  const [botCapabilities, setBotCapabilities] = useState<AgentBotsCapabilities | null>(null);
  const [selectedBotId, setSelectedBotId] = useState<number | null>(null);
  useEffect(() => {
    let alive = true;
    AIApi.listVisibleBotsWithCapabilities()
      .then(result => {
        if (!alive) return;
        setBots(result.items);
        setBotCapabilities(result.capabilities);
        // 能力已关闭：清空本地选择，避免继续携带失效的 botId（后端策略同时会降级）。
        if (result.capabilities?.botEnabled === false) setSelectedBotId(null);
      })
      .catch(() => {
        // listVisibleBotsWithCapabilities 内部已兜底为空；此分支仅防未预期异常。
        if (alive) {
          setBots([]);
          setBotCapabilities(null);
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  const chatCss = useMemo(() => buildChatCss(token), [token]);


  const providerLabel = useCallback(
    (key?: string) => feature.providers.find(p => p.key === key)?.displayName || key || '',
    [feature.providers]
  );

  // 「补充为知识文章」：把回答 Markdown 原文（含标题推导）带进新建页。
  // 走 /knowledge/articles/new 而不是历史别名 /knowledge/articles/create —— 后者的重定向
  // 只保留 query、会丢弃 location.state（routes/legacy-redirects.tsx）。
  const handlePromoteToArticle = useCallback(
    (target: ChatMessage) => {
      // P2 起草采纳率（H1-04 条件项）：AI 起草动作信号（终点 doc_draft_published 由新建页
      // 在创建成功后上报，同页漏斗；这里只报「生成了预填草稿」这一动作）。
      trackAssistantEvent('doc_draft_created', {
        pageKey: assistantPageKey,
        outcomeType: 'draft',
        source: 'chat_promote_answer',
        extra: { kind: 'answer' },
      });
      navigate('/knowledge/articles/new', { state: buildArticlePrefillState(target.content) });
    },
    [assistantPageKey, navigate]
  );

  // 标题栏「保存成文章」：整段会话。state 只带会话 ID —— 会话正文动辄上万字，
  // 塞进 history.state 会拖慢导航且逼近浏览器体积上限；拉取与组装都推迟到新建页
  // （GET /ai/conversations/:id → Markdown，见 lib/knowledge/conversation-article）。
  const handleSaveConversationAsArticle = useCallback(() => {
    if (!convId || streaming || messages.length === 0) return;
    trackAssistantEvent('doc_draft_created', {
      pageKey: assistantPageKey,
      outcomeType: 'draft',
      source: 'chat_promote_conversation',
      conversationId: convId,
      extra: { kind: 'conversation' },
    });
    navigate('/knowledge/articles/new', {
      state: buildConversationArticlePrefillState(convId),
    });
  }, [assistantPageKey, convId, messages.length, navigate, streaming]);

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

  /**
   * P3：消费 Dock → 工作区的会话交接。
   *
   * 只在**首次挂载**消费一次（ref 防 StrictMode 双调用重复拉取），且不依赖 `streaming`
   * （挂载时必然非流式）。加载失败只提示、不阻断页面——用户仍可正常发起新会话。
   *
   * 注意：这里刻意不做「卸载后丢弃结果」的 alive 守卫——StrictMode 会执行
   * effect → cleanup → effect，若第一次请求被 cleanup 取消、第二次又被 ref 拦住，
   * 交接会静默丢失。React 18 对卸载后的 setState 是安全的 no-op。
   */
  const handoffConsumedRef = useRef(false);
  useEffect(() => {
    if (!consumeWorkspaceHandoff) return;
    if (handoffConsumedRef.current) return;
    handoffConsumedRef.current = true;
    const handoffId = useAssistantStore.getState().consumeHandoffConversationId();
    if (!handoffId) return;
    void getConversationMessages(handoffId)
      .then(msgs => {
        const mapped: ChatMessage[] = msgs.map(m => ({
          id: `saved-${m.id}`,
          role: m.role === 'user' ? 'user' : 'assistant',
          content: parseMessageContent(m.role, m.content),
          createdAt: m.createdAt,
        }));
        autoScrollRef.current = true;
        setMessages(mapped);
        setConvId(handoffId);
      })
      .catch(() => {
        antdMessage.error('加载会话失败');
      });
  }, [consumeWorkspaceHandoff, parseMessageContent]);

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

  // I2-02 合规：导出会话（Markdown 快照 → 本地下载；失败仅提示，不影响会话状态）
  const handleExportConversation = useCallback(
    async (e: React.MouseEvent, convIdToExport: number) => {
      e.stopPropagation();
      try {
        const snapshot = await exportConversation(convIdToExport);
        downloadTextFile(`ai-conversation-${convIdToExport}.md`, snapshot.markdown);
        antdMessage.success('会话已导出（Markdown）');
      } catch {
        antdMessage.error('导出失败');
      }
    },
    []
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

      // D0-05：本回合埋点（失败静默；只报长度与元数据，不采集问题原文）。
      const telemetry = createChatTelemetry({
        pageKey: 'ai.chat',
        route: location.pathname,
        entrypoint: effectiveScope?.entrypoint,
        botId: selectedBotId ?? undefined,
        conversationId: convId ?? undefined,
      });
      telemetry.onSend(userMsg.content.length, convId);

      try {
        const finalConvId = await aiChatStream(
          {
            query: userMsg.content,
            conversationId: convId,
            limit: 5,
            provider: selectedProvider,
            botId: selectedBotId ?? undefined,
            // B3-02/D1-02：入口上下文（launcher 优先；否则由页面 L2 数据派生）；两者皆无 → 空对象。
            ...buildAskAIRequestScope(effectiveScope),
            // D0-04：页面上下文提示（缺省不落字段，请求体与现状逐字节一致）。
            ...(pageContextHint ? { pageContext: pageContextHint } : {}),
            signal: controller.signal,
          },
          {
            onSources: sources => {
              updateAssistant(assistantId, { sources });
            },
            onDelta: delta => {
              appendAssistantContent(assistantId, delta);
              telemetry.onFirstToken();
            },
            onToolEvent: event => {
              appendToolEvent(assistantId, event);
              telemetry.onToolEvent({
                tool: event.tool,
                status: event.status,
                durationMs: event.durationMs,
                errorCode: event.errorCode,
              });
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
              telemetry.onDone({
                conversationId: newConvId,
                provider: info?.provider,
                providerSource: info?.providerSource,
                // O2-02/§13.4：usage 缺席时保持 undefined（SDK 不落字段，旧后端零变化）。
                tokenIn: info?.usage?.tokenIn,
                tokenOut: info?.usage?.tokenOut,
                // B4-02：口径标记透传（true=估算 / false=provider 实测）。
                tokenEstimated: info?.usage?.estimated,
              });
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
                        // O2-02/§13.4：回合用量估算（诊断抽屉数据源；旧后端缺省）。
                        usage: info?.usage,
                        run: m.run ? { ...m.run, status: 'done' } : undefined,
                      }
                    : m
                )
              );
            },
            onError: msg => {
              telemetry.onError(msg);
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
        // 网络/流异常（error 帧之外）：记录一次失败；用户主动停止不计。
        telemetry.onError((err as Error)?.message);
        try {
          const res = await AIApi.chat({
            query: userMsg.content,
            conversationId: convId,
            limit: 5,
            provider: selectedProvider,
            botId: selectedBotId ?? undefined,
            ...buildAskAIRequestScope(effectiveScope),
            ...(pageContextHint ? { pageContext: pageContextHint } : {}),
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
          telemetry.onError(fallbackMsg);
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
      activeScope,
      pageScope,
      appendAssistantContent,
      convId,
      loadConversations,
      location.pathname,
      pageContextHint,
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

  // I0-04：结果落点回执的「打开」——SPA 导航 + 观测（用户选择打开落点）。
  const openOutcome = useCallback(
    (outcome: AssistantOutcome) => {
      if (!outcome.route) return;
      trackAssistantEvent('handoff', {
        pageKey: 'ai.chat',
        extra: { from: 'outcome_receipt', kind: outcome.kind },
      });
      navigate(outcome.route);
    },
    [navigate]
  );

  return {
    messages,
    convId,
    streaming,
    query,
    setQuery,
    providerNotice,
    setProviderNotice,
    conversations,
    loadingConvs,
    sidebarOpen,
    setSidebarOpen,
    activeScope,
    /** P3：当前生效的入口上下文（launcher scope 优先，否则由页面 L2 数据派生）。 */
    effectiveScope,
    setScopeDismissed,
    openOutcome,
    bots,
    botCapabilities,
    selectedBotId,
    setSelectedBotId,
    feature,
    selectedProvider,
    setSelectedProvider,
    canSwitchProvider,
    canManageProviderPreference,
    providerLabel,
    handleSetPersonalDefault,
    handleClearPersonalDefault,
    canPromoteKnowledge,
    canDeleteConversation,
    scrollRef,
    isEmpty,
    currentConvTitle,
    chatCss,
    formatTime,
    handleSend,
    handleStop,
    handleClear,
    handleKeyDown,
    handleScroll,
    switchToConversation,
    startNewConversation,
    handleDeleteConversation,
    handleExportConversation,
    handlePromoteToArticle,
    handleSaveConversationAsArticle,
    handleOpenApproval,
    handleRequestConfirm,
  };
}
