/**
 * I1-02 划词问 AI：任意页面选中文本 → 悬浮「问 AI」→ 助手面板预填（只填不发送）。
 *
 * 口径（方案 §3.5 降低使用成本的输入通道）：
 * - 选区长度 2–500 字符（超界忽略，避免误触/巨文本）；
 * - 忽略输入框/可编辑元素与助手面板内部的选区（防递归与干扰输入）；
 * - 埋点 `handoff(from=selection)`；预填通道复用 assistant-store.pendingPrompt（I0-03）。
 *
 * 挂载：MainLayout（`AssistantDock` 旁）；未选中文本时零渲染。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { Button } from 'antd';
import { Sparkles } from 'lucide-react';

import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

const DEFAULT_MIN_LENGTH = 2;
const DEFAULT_MAX_LENGTH = 500;

export const SELECTION_ASK_AI_TESTID = 'selection-ask-ai';

/** 选区提示词（截断上限与 maxLength 一致；不自动发送）。 */
export const buildSelectionPrompt = (text: string): string =>
  `请结合当前页面解释以下内容，并给出可执行的下一步：\n"""${text.trim().slice(0, DEFAULT_MAX_LENGTH)}"""`;

export interface SelectionAskAIProps {
  minLength?: number;
  maxLength?: number;
}

interface SelectionState {
  text: string;
  x: number;
  y: number;
}

const isIgnoredSelection = (anchorNode: Node | null): boolean => {
  if (!anchorNode) return false;
  const element = anchorNode instanceof Element ? anchorNode : anchorNode.parentElement;
  if (!element || typeof element.closest !== 'function') return false;
  return Boolean(
    element.closest(
      'input, textarea, select, [contenteditable="true"], [data-testid^="assistant-"]'
    )
  );
};

export const SelectionAskAI: React.FC<SelectionAskAIProps> = ({
  minLength = DEFAULT_MIN_LENGTH,
  maxLength = DEFAULT_MAX_LENGTH,
}) => {
  const [state, setState] = useState<SelectionState | null>(null);
  const setPendingPrompt = useAssistantStore(store => store.setPendingPrompt);
  const openPanel = useAssistantStore(store => store.openPanel);

  const handleSelection = useCallback(() => {
    const selection = typeof window.getSelection === 'function' ? window.getSelection() : null;
    const text = selection?.toString().trim() ?? '';
    if (!selection || text.length < minLength || text.length > maxLength) {
      setState(null);
      return;
    }
    if (isIgnoredSelection(selection.anchorNode)) {
      setState(null);
      return;
    }
    let rect: DOMRect | null = null;
    try {
      rect = selection.getRangeAt(0).getBoundingClientRect();
    } catch {
      rect = null;
    }
    setState({
      text,
      x: Math.max(8, Math.min(window.innerWidth - 96, (rect?.left ?? 8) + (rect?.width ?? 0) / 2 - 36)),
      y: Math.max(8, (rect?.top ?? 8) - 40),
    });
  }, [minLength, maxLength]);

  useEffect(() => {
    const handleMouseUp = () => handleSelection();
    const hide = () => setState(null);
    const handleMouseDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (target?.closest?.(`[data-testid="${SELECTION_ASK_AI_TESTID}"]`)) return;
      hide();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') hide();
    };

    document.addEventListener('mouseup', handleMouseUp);
    document.addEventListener('mousedown', handleMouseDown, true);
    window.addEventListener('scroll', hide, true);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mouseup', handleMouseUp);
      document.removeEventListener('mousedown', handleMouseDown, true);
      window.removeEventListener('scroll', hide, true);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [handleSelection]);

  const handleAsk = useCallback(() => {
    if (!state) return;
    setPendingPrompt(buildSelectionPrompt(state.text));
    openPanel();
    trackAssistantEvent('handoff', { extra: { from: 'selection' } });
    setState(null);
  }, [openPanel, setPendingPrompt, state]);

  if (!state) return null;

  return (
    <Button
      data-testid={SELECTION_ASK_AI_TESTID}
      type="primary"
      size="small"
      icon={<Sparkles size={13} />}
      onMouseDown={event => {
        // 防止 document 级 mousedown 先隐藏按钮导致 click 丢失。
        event.preventDefault();
        event.stopPropagation();
      }}
      onClick={handleAsk}
      style={{
        position: 'fixed',
        left: state.x,
        top: state.y,
        zIndex: 990,
        boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
      }}
    >
      问 AI
    </Button>
  );
};

export default SelectionAskAI;