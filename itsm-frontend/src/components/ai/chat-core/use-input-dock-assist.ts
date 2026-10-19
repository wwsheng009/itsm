/**
 * T5-02 输入坞智能辅助（交互层）：把 `/` 快捷指令、`@` 当前页对象引用接入任意输入坞。
 *
 * 契约：
 * - 菜单开启时接管 ↑/↓（移动）/ Enter（选择）/ Esc（关闭）/ Tab（选择），**绝不发送**；
 * - 菜单关闭时原样委托会话内核的 `onKeyDown`（Enter 发送等行为不变）；
 * - 选中一律「只填不发」：替换触发 token 为插入文案/引用标签（引用带尾随空格）；
 * - IME 组合输入（`isComposing`）期间不参与导航/选择，避免与中文输入法抢键；
 * - Esc 关闭只对「当时那段输入」生效，继续编辑后自动恢复触发判定。
 */
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import type { TextAreaRef } from 'antd/es/input/TextArea';

import { useAssistantPageData, type AssistantPageData } from '@/lib/store/assistant-context-store';
import {
  applyReference,
  applySlashCommand,
  filterSlashCommands,
  matchesReferenceKeyword,
  parseReferenceTrigger,
  parseSlashTrigger,
  referenceDisplayText,
  type SlashCommand,
} from './input-dock-commands';
import type { InputDockAssistMenuItem, InputDockAssistMode } from './InputDockAssistMenu';

export interface UseInputDockAssistOptions {
  query: string;
  setQuery: (value: string) => void;
  /** 菜单未开启时原样委托（会话内核的 Enter 发送等）。 */
  onSessionKeyDown?: (event: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  /** 选中后聚焦回输入框（可选的 TextArea ref）。 */
  inputRef?: React.RefObject<TextAreaRef | null>;
}

export interface InputDockAssistState {
  mode: InputDockAssistMode;
  items: InputDockAssistMenuItem[];
  hint: string;
  activeIndex: number;
}

export interface UseInputDockAssistResult {
  /** 当前应展示的菜单状态；null=不展示。 */
  assist: InputDockAssistState | null;
  handleKeyDown: (event: React.KeyboardEvent<HTMLTextAreaElement>) => void;
  selectItem: (index: number) => void;
  highlightItem: (index: number) => void;
}

interface AssistDerived {
  mode: InputDockAssistMode;
  items: InputDockAssistMenuItem[];
  hint: string;
  commands: SlashCommand[];
  pages: AssistantPageData[];
}

export function useInputDockAssist({
  query,
  setQuery,
  onSessionKeyDown,
  inputRef,
}: UseInputDockAssistOptions): UseInputDockAssistResult {
  const pageData = useAssistantPageData();
  const [activeIndex, setActiveIndex] = useState(0);
  const [dismissedFor, setDismissedFor] = useState<string | null>(null);

  const derived = useMemo<AssistDerived | null>(() => {
    if (dismissedFor === query) return null;

    const slashKeyword = parseSlashTrigger(query);
    if (slashKeyword !== null) {
      const commands = filterSlashCommands(slashKeyword);
      return {
        mode: 'slash',
        items: commands.map(command => ({
          key: command.name,
          label: command.name,
          description: command.description,
        })),
        hint: '没有匹配的指令',
        commands,
        pages: [],
      };
    }

    const reference = parseReferenceTrigger(query);
    if (reference) {
      const pages =
        pageData && matchesReferenceKeyword(pageData, reference.keyword) ? [pageData] : [];
      return {
        mode: 'reference',
        items: pages.map(page => ({
          key: `reference:${page.targetType}:${page.targetId}`,
          label: referenceDisplayText(page),
          description: page.summary,
        })),
        hint: pageData ? '没有匹配的当前页对象' : '当前页面暂无可引用的对象',
        commands: [],
        pages,
      };
    }

    return null;
  }, [dismissedFor, pageData, query]);

  const itemCount = derived?.items.length ?? 0;

  // 输入变化或菜单类型切换后，高亮回到首项（列表可能已重新过滤）。
  useEffect(() => {
    setActiveIndex(0);
  }, [derived?.mode, itemCount, query]);

  const focusInput = useCallback(() => {
    const ref = inputRef?.current as
      | { focus?: () => void; resizableTextArea?: { textArea?: HTMLTextAreaElement } }
      | null
      | undefined;
    const textarea = ref?.resizableTextArea?.textArea;
    if (textarea) {
      textarea.focus();
      const end = textarea.value.length;
      textarea.setSelectionRange(end, end);
      return;
    }
    ref?.focus?.();
  }, [inputRef]);

  const selectItem = useCallback(
    (index: number) => {
      if (!derived) return;
      if (derived.mode === 'slash') {
        const command = derived.commands[index];
        if (!command) return;
        setQuery(applySlashCommand(query, command));
      } else {
        const page = derived.pages[index];
        if (!page) return;
        setQuery(applyReference(query, page));
      }
      setDismissedFor(null);
      // 等输入框 value 更新后再聚焦并落光标到末尾。
      window.setTimeout(focusInput, 0);
    },
    [derived, focusInput, query, setQuery]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (!derived) {
        onSessionKeyDown?.(event);
        return;
      }
      // IME 组合输入期间不参与导航/选择（避免与中文输入法抢键）。
      if ((event.nativeEvent as KeyboardEvent | undefined)?.isComposing) return;

      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setActiveIndex(prev => (itemCount > 0 ? (prev + 1) % itemCount : 0));
        return;
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault();
        setActiveIndex(prev => (itemCount > 0 ? (prev - 1 + itemCount) % itemCount : 0));
        return;
      }
      if (event.key === 'Escape') {
        event.preventDefault();
        setDismissedFor(query);
        return;
      }
      if (event.key === 'Enter' || event.key === 'Tab') {
        if (event.key === 'Enter' && event.shiftKey) {
          // Shift+Enter（换行）不拦截，交给默认行为。
          onSessionKeyDown?.(event);
          return;
        }
        // 菜单开启时只做选择，绝不发送；无候选项时按键被吞掉（防止误发）。
        event.preventDefault();
        if (itemCount > 0) selectItem(activeIndex);
        return;
      }
      onSessionKeyDown?.(event);
    },
    [activeIndex, derived, itemCount, onSessionKeyDown, query, selectItem]
  );

  return {
    assist: derived
      ? {
          mode: derived.mode,
          items: derived.items,
          hint: derived.hint,
          activeIndex,
        }
      : null,
    handleKeyDown,
    selectItem,
    highlightItem: setActiveIndex,
  };
}
