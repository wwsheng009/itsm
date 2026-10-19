/**
 * D0-05/D1-01/D1-02/I0-02 页面级建议卡（静态基线 + 服务端过滤 + 任务/问题分组）。
 *
 * - 数据源：优先 `GET /ai/assistant/context`（D1-01 服务端按「可见 Bot ∩ 工具授权 ∩ RBAC」过滤）；
 *   请求失败/未知页面回退 D0-03 静态注册表；未注册路由回退通用建议（constants.SUGGESTIONS）；
 * - I0-02：建议分区「任务卡（零输入可执行；只读）」「问题卡（了解）」两组渲染（顺序 = 任务在前）；
 * - 埋点：`suggestion_shown` / `suggestion_clicked` / `suggestion_ignored`（含 cardId/kind/rank 与 extra.feed）；
 *   `ignored` = 会话级「展示后未点击」结算（卡片消失或面板关闭时上报；点击过的卡豁免）；
 * - 零输入约束：任务卡全部只读（注册表契约测试锁定），点击后仍走正常对话链路（写动作走确认闭环）。
 */
import React, { useCallback, useEffect, useMemo, useRef } from 'react';
import { useNavigate } from 'react-router';
import { Button, theme } from 'antd';

import { executeAssistantAction } from '@/lib/ai/assistant-actions';
import { flattenPageActions } from '@/lib/ai/page-context/types';
import { usePageAssistantContext } from '@/lib/ai/page-context/use-page-assistant-context';
import { useAssistantSuggestions } from '@/lib/ai/page-context/use-assistant-suggestions';
import { useDynamicSuggestions } from '@/lib/ai/page-context/use-dynamic-suggestions';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { useAssistantPageData } from '@/lib/store/assistant-context-store';
import { useI18n } from '@/lib/i18n/useI18n';
import { SUGGESTIONS } from './constants';

interface SuggestionCardItem {
  id: string;
  kind: 'task' | 'question';
  label: string;
  prompt: string;
  /** 动作声明的回退路由（D1-02 动作桥；未注册 handler 时跳转）。 */
  route?: string;
  /** 数据源标注（P2：dynamic=LLM 生成 → 埋点 feed=llm）。 */
  feed?: 'server' | 'static' | 'llm';
}

/** 已展示建议（忽略结算的最小台账：按 pageKey+cardId 区分，避免跨页 id 复用串扰）。 */
type ShownSuggestion = {
  id: string;
  pageKey: string;
  kind: 'task' | 'question';
  rank: number;
  feed: 'server' | 'static' | 'llm';
};

const suggestionKey = (pageKey: string, cardId: string) => `${pageKey}::${cardId}`;

export interface SuggestionCardsProps {
  onSend: (prompt: string) => void;
  disabled?: boolean;
  /** 紧凑形态（Dock 面板：单列 + small）。 */
  compact?: boolean;
}

export const SuggestionCards: React.FC<SuggestionCardsProps> = ({ onSend, disabled, compact }) => {
  const { token } = theme.useToken();
  const { t } = useI18n();
  const { declaration, pageKey, route } = usePageAssistantContext();
  const feed = useAssistantSuggestions(declaration, pageKey, route);
  // P2 LLM 动态建议：失败/未启用 → 空数组（保持静态/服务端基线，零行为差异）。
  const dynamic = useDynamicSuggestions(declaration, pageKey, route);
  const navigate = useNavigate();
  const pageData = useAssistantPageData();

  // 服务端过滤结果不含 route 声明（route 属于前端注册表契约），按 actionId 回填。
  const routeById = useMemo(() => {
    const map = new Map<string, string>();
    if (declaration) {
      flattenPageActions(declaration).forEach(action => {
        if (action.route) map.set(action.id, action.route);
      });
    }
    return map;
  }, [declaration]);

  const cards = useMemo<SuggestionCardItem[]>(() => {
    if (!declaration) {
      return SUGGESTIONS.map((text, index) => ({
        id: `generic-${index}`,
        kind: 'question' as const,
        label: text,
        prompt: text,
        feed: 'static' as const,
      }));
    }
    const base = feed.items.map<SuggestionCardItem>(item => ({
      id: item.id,
      kind: item.kind,
      label: item.label,
      prompt: item.prompt,
      route: routeById.get(item.id),
      feed: feed.source,
    }));
    if (dynamic.items.length === 0) return base;
    // P2：动态建议置于各组首位（知识/LLM 更贴近当前上下文），静态建议保持原顺序兜底。
    const dynTasks = dynamic.items.filter(item => item.kind === 'task').map<SuggestionCardItem>(item => ({ ...item, feed: 'llm' }));
    const dynQuestions = dynamic.items.filter(item => item.kind === 'question').map<SuggestionCardItem>(item => ({ ...item, feed: 'llm' }));
    const baseTasks = base.filter(item => item.kind === 'task');
    const baseQuestions = base.filter(item => item.kind === 'question');
    return [...dynTasks, ...baseTasks, ...dynQuestions, ...baseQuestions];
  }, [declaration, feed.items, feed.source, dynamic.items, routeById]);

  // I0-02：任务卡/问题卡分组（rank 保持全局序号，便于服务端聚类与漏斗对齐）。
  const taskCards = useMemo(() => cards.filter(card => card.kind === 'task'), [cards]);
  const questionCards = useMemo(() => cards.filter(card => card.kind === 'question'), [cards]);

  // 会话级忽略采集（D1-02 / O2-01）：展示后未点击的卡，在"机会结束"时结算一次。
  //
  // 机会窗口 = 一轮展示集：结算时机为卡片消失（页面切换/服务端替换/会话由空变有）
  // 或组件卸载（面板关闭）；点击过的卡豁免。卸载结算延迟一个宏任务，
  // 避免 React StrictMode 开发态「挂载→清理→重挂」产生虚假忽略（同一提交内会被下一轮 effect 取消）。
  const shownRef = useRef(new Map<string, ShownSuggestion>());
  const clickedRef = useRef(new Set<string>());
  const ignoreTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (ignoreTimerRef.current !== null) {
      clearTimeout(ignoreTimerRef.current);
      ignoreTimerRef.current = null;
    }

    const emitIgnored = (key: string, shown: ShownSuggestion) => {
      if (clickedRef.current.has(key)) return;
      trackAssistantEvent('suggestion_ignored', {
        pageKey: shown.pageKey,
        cardId: shown.id,
        cardKind: shown.kind,
        rank: shown.rank,
        extra: { feed: shown.feed },
      });
    };

    const next = new Map<string, ShownSuggestion>();
    cards.forEach((card, index) => {
      const shown: ShownSuggestion = {
        id: card.id,
        pageKey,
        kind: card.kind,
        rank: index,
        feed: card.feed ?? feed.source,
      };
      next.set(suggestionKey(pageKey, card.id), shown);
      trackAssistantEvent('suggestion_shown', {
        pageKey,
        cardId: card.id,
        cardKind: card.kind,
        rank: index,
        extra: { feed: shown.feed },
      });
    });

    // 上一轮已消失且未点击 → 忽略；仍在展示的卡继续等待，不重复计数。
    shownRef.current.forEach((shown, key) => {
      if (!next.has(key)) emitIgnored(key, shown);
    });
    // 清理已消失卡的点击台账（避免同 id 复用时沿用旧点击）。
    for (const key of Array.from(clickedRef.current)) {
      if (!next.has(key)) clickedRef.current.delete(key);
    }
    shownRef.current = next;

    return () => {
      ignoreTimerRef.current = setTimeout(() => {
        ignoreTimerRef.current = null;
        shownRef.current.forEach((shown, key) => emitIgnored(key, shown));
        shownRef.current.clear();
      }, 0);
    };
  }, [cards, pageKey, feed.source]);

  const handlePick = useCallback(
    (card: SuggestionCardItem, index: number) => {
      clickedRef.current.add(suggestionKey(pageKey, card.id));
      // D1-02 动作桥：handler → route 跳转 → 提示词（默认，与 D0-05 一致）。
      const dispatch = executeAssistantAction(
        { actionId: card.id, prompt: card.prompt, route: card.route },
        { pageData, navigate, sendPrompt: onSend }
      );
      trackAssistantEvent('suggestion_clicked', {
        pageKey,
        cardId: card.id,
        cardKind: card.kind,
        rank: index,
        extra: { feed: card.feed ?? feed.source, dispatch },
      });
    },
    [onSend, pageKey, feed.source, pageData, navigate]
  );

  const renderCard = (card: SuggestionCardItem, rank: number) => (
    <Button
      key={card.id}
      data-testid={`suggestion-card-${card.id}`}
      className="ai-chat-suggest"
      size={compact ? 'small' : 'middle'}
      onClick={() => handlePick(card, rank)}
      disabled={disabled}
      title={card.kind === 'task' ? t('assistant.suggestions.taskCardTooltip') : undefined}
      style={
        card.kind === 'question'
          ? { color: token.colorTextSecondary, borderColor: token.colorBorderSecondary }
          : undefined
      }
    >
      {card.label}
    </Button>
  );

  const groupGridStyle: React.CSSProperties = {
    display: 'grid',
    gap: compact ? 8 : 12,
    gridTemplateColumns: compact ? '1fr' : 'repeat(2, minmax(0, 1fr))',
  };
  const groupHeaderStyle: React.CSSProperties = {
    fontSize: 11,
    color: token.colorTextSecondary,
    margin: '2px 0 6px',
  };

  return (
    <div data-testid="suggestion-cards" style={{ display: 'grid', gap: compact ? 10 : 14 }}>
      {taskCards.length > 0 ? (
        <section data-testid="suggestion-group-tasks">
          <div style={groupHeaderStyle}>{t('assistant.suggestions.groupTasks')}</div>
          <div style={groupGridStyle}>{taskCards.map((card, index) => renderCard(card, index))}</div>
        </section>
      ) : null}
      {questionCards.length > 0 ? (
        <section data-testid="suggestion-group-questions">
          <div style={groupHeaderStyle}>{t('assistant.suggestions.groupQuestions')}</div>
          <div style={groupGridStyle}>
            {questionCards.map((card, index) => renderCard(card, taskCards.length + index))}
          </div>
        </section>
      ) : null}
    </div>
  );
};

export default SuggestionCards;