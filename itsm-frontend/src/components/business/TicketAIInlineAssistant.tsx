/**
 * V0-01 工单详情 Inline 三件套（答复草稿 + 知识推荐；分诊建议由 AISuggestionPanel 承载）。
 *
 * 口径（方案 §7 V0-01 "Inline 优先"）：
 * - 草稿由 `POST /ai/tickets/:id/reply-draft` 只读生成（后端带 KB grounding）→ 本组件可编辑；
 * - 「填入回复」把草稿写入当前页评论框（CommentPanel externalDraft），**绝不自动提交**；
 * - 知识推荐复用 `POST /ai/rag/search(kb)`；「引用到草稿」把来源标题追加到草稿尾部；
 * - 基线采集（与 O0-01 同一埋点）：inline_preview / inline_applied / task_started / task_completed / task_failed / error_shown。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Empty, Input, Space, Tag, Typography, message } from 'antd';
import { BookOpen, ClipboardCopy, RefreshCw, Sparkles, Wand2 } from 'lucide-react';

import {
  aiCreateReplyDraft,
  aiSearchKB,
  type RagAnswer,
  type ReplyDraftCitation,
} from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

export interface TicketAIInlineAssistantProps {
  ticketId: number;
  title: string;
  description?: string;
  /** 提供时渲染「填入回复」：把草稿写入页面评论框（只填不提交）。 */
  onApplyReply?: (text: string) => void;
}

export const TicketAIInlineAssistant: React.FC<TicketAIInlineAssistantProps> = ({
  ticketId,
  title,
  description,
  onApplyReply,
}) => {
  const [draft, setDraft] = useState('');
  const [draftLoading, setDraftLoading] = useState(false);
  const [draftError, setDraftError] = useState<string | null>(null);
  const [citations, setCitations] = useState<ReplyDraftCitation[]>([]);

  const [kbItems, setKbItems] = useState<RagAnswer[]>([]);
  const [kbLoading, setKbLoading] = useState(false);
  const [kbError, setKbError] = useState<string | null>(null);

  // 知识推荐：进入详情页即检索（工单标题 + 描述）；失败降级为提示，不阻塞。
  const loadKb = useCallback(async () => {
    const query = `${title} ${description ?? ''}`.trim();
    if (!ticketId || !query) return;
    setKbLoading(true);
    setKbError(null);
    try {
      const result = await aiSearchKB(query, 3);
      setKbItems(result.answers ?? []);
      trackAssistantEvent('inline_preview', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        extra: { panel: 'kb', count: (result.answers ?? []).length },
      });
    } catch {
      setKbError('知识检索暂不可用');
      setKbItems([]);
    } finally {
      setKbLoading(false);
    }
  }, [ticketId, title, description]);

  useEffect(() => {
    void loadKb();
  }, [loadKb]);

  const handleGenerate = useCallback(async () => {
    trackAssistantEvent('task_started', {
      pageKey: 'ticket.detail',
      targetType: 'ticket',
      targetId: ticketId,
      taskCardId: 'reply_draft',
    });
    setDraftLoading(true);
    setDraftError(null);
    const startedAt = Date.now();
    try {
      const result = await aiCreateReplyDraft(ticketId);
      setDraft(result.draft ?? '');
      setCitations(result.citations ?? []);
      trackAssistantEvent('task_completed', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        taskCardId: 'reply_draft',
        durationMs: Date.now() - startedAt,
        outcomeType: 'draft',
        extra: { task: 'reply_draft', citations: (result.citations ?? []).length },
      });
      trackAssistantEvent('inline_preview', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        extra: { panel: 'reply_draft', ref: 'ai.tickets.reply-draft' },
      });
    } catch (err) {
      const msg = (err as Error)?.message || 'AI 草稿生成失败，请稍后重试';
      setDraftError(msg);
      trackAssistantEvent('task_failed', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        taskCardId: 'reply_draft',
        extra: { task: 'reply_draft' },
      });
      trackAssistantEvent('error_shown', {
        pageKey: 'ticket.detail',
        errorCode: 'reply_draft_failed',
      });
    } finally {
      setDraftLoading(false);
    }
  }, [ticketId]);

  const handleApply = useCallback(() => {
    if (!onApplyReply || !draft.trim()) return;
    onApplyReply(draft.trim());
    trackAssistantEvent('inline_applied', {
      pageKey: 'ticket.detail',
      targetType: 'ticket',
      targetId: ticketId,
      extra: { panel: 'reply', action: 'fill', chars: draft.trim().length },
    });
  }, [draft, onApplyReply, ticketId]);

  const handleCopy = useCallback(async () => {
    if (!draft.trim()) return;
    try {
      await navigator.clipboard?.writeText(draft.trim());
      message.success('已复制到剪贴板');
      trackAssistantEvent('inline_applied', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        extra: { panel: 'reply', action: 'copy', chars: draft.trim().length },
      });
    } catch {
      message.warning('复制失败，请手动选择文本');
    }
  }, [draft, ticketId]);

  const handleQuote = useCallback(
    (citation: ReplyDraftCitation) => {
      const fragment = `（引用：${citation.title} ${citation.source}）`;
      setDraft(prev => (prev.trim() ? `${prev.trim()}\n${fragment}` : fragment));
      trackAssistantEvent('inline_applied', {
        pageKey: 'ticket.detail',
        targetType: 'ticket',
        targetId: ticketId,
        extra: { panel: 'kb', action: 'quote', source: citation.source },
      });
    },
    [ticketId]
  );

  return (
    <Card
      size="small"
      data-testid="ticket-ai-inline"
      className="mb-4 border-indigo-100"
      title={
        <Space>
          <Wand2 className="w-4 h-4 text-indigo-600" />
          <span className="font-medium text-indigo-700">AI 工单助手（Inline）</span>
          <Text type="secondary" style={{ fontSize: 12, fontWeight: 400 }}>
            草稿/引用仅填入，提交仍由你来完成
          </Text>
        </Space>
      }
    >
      <div data-testid="inline-reply-section" style={{ marginBottom: 12 }}>
        <Space style={{ marginBottom: 8 }} wrap>
          <Button
            type="primary"
            size="small"
            ghost
            icon={<Sparkles className="w-3 h-3" />}
            loading={draftLoading}
            onClick={() => void handleGenerate()}
            data-testid="inline-generate-draft"
          >
            生成答复草稿
          </Button>
          {draft ? (
            <Button
              size="small"
              icon={<RefreshCw className="w-3 h-3" />}
              loading={draftLoading}
              onClick={() => void handleGenerate()}
              data-testid="inline-refresh-draft"
            >
              重新生成
            </Button>
          ) : null}
          {onApplyReply ? (
            <Button
              size="small"
              type="primary"
              icon={<Wand2 className="w-3 h-3" />}
              disabled={!draft.trim()}
              onClick={handleApply}
              data-testid="inline-apply-reply"
            >
              填入回复框
            </Button>
          ) : null}
          <Button
            size="small"
            icon={<ClipboardCopy className="w-3 h-3" />}
            disabled={!draft.trim()}
            onClick={() => void handleCopy()}
            data-testid="inline-copy-draft"
          >
            复制
          </Button>
        </Space>

        {draftError ? (
          <Alert
            type="warning"
            showIcon
            message={draftError}
            data-testid="inline-draft-error"
            style={{ marginBottom: 8 }}
          />
        ) : null}

        <TextArea
          data-testid="inline-draft-input"
          value={draft}
          onChange={event => setDraft(event.target.value)}
          placeholder="点击「生成答复草稿」，草稿可在此编辑（不会自动提交）。"
          autoSize={{ minRows: 4, maxRows: 10 }}
        />

        {citations.length > 0 ? (
          <div style={{ marginTop: 8 }} data-testid="inline-citations">
            <Text type="secondary" style={{ fontSize: 12 }}>
              引用来源：
            </Text>
            <Space wrap size={4}>
              {citations.map((citation, index) => (
                <Tag key={`${citation.source}-${index}`} color="geekblue">
                  {`[${index + 1}] ${citation.title}`}
                </Tag>
              ))}
            </Space>
          </div>
        ) : null}
      </div>

      <div data-testid="inline-kb-section">
        <Space style={{ marginBottom: 6 }}>
          <BookOpen className="w-4 h-4 text-indigo-600" />
          <Text strong style={{ fontSize: 13 }}>
            知识推荐
          </Text>
          <Button
            type="link"
            size="small"
            loading={kbLoading}
            onClick={() => void loadKb()}
            data-testid="inline-refresh-kb"
          >
            刷新
          </Button>
        </Space>

        {kbError ? (
          <Alert type="warning" showIcon message={kbError} data-testid="inline-kb-error" />
        ) : null}

        {!kbError && !kbLoading && kbItems.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无相关知识" />
        ) : null}

        {kbItems.map(item => (
          <div
            key={`${item.objectType}-${item.id}`}
            data-testid={`inline-kb-item-${item.id}`}
            style={{
              padding: '6px 0',
              borderBottom: '1px dashed #f0f0f0',
            }}
          >
            <Space size={6} wrap>
              <Text strong style={{ fontSize: 12 }}>
                {item.title || `知识 #${item.id}`}
              </Text>
              {typeof item.score === 'number' ? <Tag>匹配 {(item.score * 100).toFixed(0)}%</Tag> : null}
              <Button
                type="link"
                size="small"
                onClick={() =>
                  handleQuote({
                    title: item.title || `知识 #${item.id}`,
                    source: `kb#${item.id}`,
                    snippet: item.snippet,
                    score: item.score,
                  })
                }
                data-testid={`inline-quote-${item.id}`}
              >
                引用到草稿
              </Button>
            </Space>
            <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
              {item.snippet}
            </Paragraph>
          </div>
        ))}
      </div>
    </Card>
  );
};

export default TicketAIInlineAssistant;