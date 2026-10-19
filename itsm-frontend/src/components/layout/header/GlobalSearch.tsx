import { useNavigate } from 'react-router';

import React, { useState, useEffect, useCallback, useRef } from 'react';
import { Input, Modal, List, Spin } from 'antd';
import type { InputRef } from 'antd';
import { Search, Sparkles, Ticket } from 'lucide-react';
import {
  globalSearch,
  type GlobalSearchResponse,
  type SearchResult,
} from '@/lib/api/global-search-api';
import { DESIGN } from '@/design-system/tokens';
import { useAssistantStore } from '@/lib/store/assistant-store';
import { trackAssistantEvent } from '@/lib/ai/telemetry';
import styles from './Header.module.css';

interface GlobalSearchProps {
  open: boolean;
  onClose: () => void;
  initialKeyword?: string;
  initialResults?: GlobalSearchResponse | null;
}

export const GlobalSearch: React.FC<GlobalSearchProps> = ({
  open,
  onClose,
  initialKeyword = '',
  initialResults = null,
}) => {
  const navigate = useNavigate();
  const searchInputRef = useRef<InputRef>(null);
  const [searchValue, setSearchValue] = useState('');
  const [searchResults, setSearchResults] = useState<GlobalSearchResponse | null>(initialResults);
  const [isSearching, setIsSearching] = useState(false);
  // I0-03：搜索「问 AI」——把关键词转交常驻助手面板（预填，不自动发送）。
  const openPanel = useAssistantStore(state => state.openPanel);
  const setPendingPrompt = useAssistantStore(state => state.setPendingPrompt);

  const handleSearch = useCallback(async (value: string) => {
    const keyword = value.trim();
    if (!keyword) {
      setSearchResults(null);
      return;
    }

    setIsSearching(true);
    try {
      const results = await globalSearch(keyword);
      setSearchResults(results);
    } catch (error) {
      console.error('Search failed:', error);
      setSearchResults({ results: [], total: 0 });
    } finally {
      setIsSearching(false);
    }
  }, []);

  useEffect(() => {
    if (!open) {
      setSearchValue('');
      setSearchResults(null);
      return;
    }

    setSearchValue(initialKeyword);
    setSearchResults(initialResults);

    if (initialKeyword.trim() && !initialResults) {
      void handleSearch(initialKeyword);
    }
  }, [handleSearch, initialKeyword, initialResults, open]);

  useEffect(() => {
    if (!open) return;

    const focusTimer = window.setTimeout(() => {
      searchInputRef.current?.focus();
    }, 0);

    return () => window.clearTimeout(focusTimer);
  }, [open]);

  // Ctrl+K 快捷键
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const handleItemClick = (item: SearchResult) => {
    const pathMap: Record<string, string> = {
      ticket: 'tickets',
      incident: 'incidents',
      problem: 'problems',
      change: 'changes',
      knowledge: 'knowledge',
    };
    // PE-16：项目/项目任务跳转——任务结果进入其所属项目工作台。
    if (item.type === 'project') {
      navigate(`/projects/${item.id}`);
    } else if (item.type === 'project_task') {
      navigate(item.projectId ? `/projects/${item.projectId}` : '/projects');
    } else {
      const basePath = pathMap[item.type] || item.type;
      navigate(`/${basePath}/${item.id}`);
    }
    onClose();
  };

  const handleAskAI = useCallback(
    (keyword: string) => {
      const q = keyword.trim();
      if (!q) return;
      setPendingPrompt(q);
      openPanel();
      trackAssistantEvent('handoff', { extra: { from: 'global_search' } });
      onClose();
    },
    [onClose, openPanel, setPendingPrompt]
  );

  return (
    <Modal
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <Search size={18} style={{ color: DESIGN.colors.accent }} />
          <span>全局搜索</span>
        </div>
      }
      open={open}
      onCancel={onClose}
      footer={null}
      width={560}
      centered
      destroyOnHidden
      focusable={{ focusTriggerAfterClose: false }}
    >
      <div style={{ padding: '0 20px' }}>
        <Input
          ref={searchInputRef}
          className={styles.globalSearchModalInput}
          placeholder='搜索工单、事件、项目、任务...'
          prefix={<Search size={16} style={{ color: DESIGN.colors.textMuted }} />}
          value={searchValue}
          onChange={e => setSearchValue(e.target.value)}
          onKeyDown={e => {
            if (e.key !== 'Enter') return;
            // I0-03：Enter 仍是搜索；Ctrl/Cmd+Enter = 问 AI（键盘全流程可达，行为不冲突）。
            if (e.ctrlKey || e.metaKey) {
              e.preventDefault();
              handleAskAI(searchValue);
              return;
            }
            void handleSearch(searchValue);
          }}
          suffix={isSearching ? <Spin size='small' /> : null}
          style={{
            width: '100%',
            height: 40,
            borderRadius: DESIGN.radius.md,
            marginBottom: 16,
          }}
          autoFocus
        />
      </div>

      {/* I0-03：问 AI 区（关键词非空时常驻首项；点击或 Ctrl+Enter 转交助手面板） */}
      {searchValue.trim() ? (
        <div style={{ padding: '0 20px', marginBottom: 12 }}>
          <div
            data-testid="global-search-ask-ai"
            role="button"
            tabIndex={0}
            onClick={() => handleAskAI(searchValue)}
            onKeyDown={e => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                handleAskAI(searchValue);
              }
            }}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '10px 12px',
              borderRadius: DESIGN.radius.md,
              border: `1px solid ${DESIGN.colors.border}`,
              background: DESIGN.colors.bgSubtle,
              cursor: 'pointer',
            }}
          >
            <Sparkles size={16} style={{ color: DESIGN.colors.accent }} />
            <div style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                问 AI：{searchValue.trim()}
              </div>
              <div style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
                在 AI 助手中继续（Ctrl+Enter）
              </div>
            </div>
          </div>
        </div>
      ) : null}

      {searchResults && searchResults.results.length > 0 ? (
        <div style={{ padding: '0 20px' }}>
          <List
            header={<strong>搜索结果</strong>}
            dataSource={searchResults.results}
            renderItem={(item: SearchResult) => (
              <List.Item>
                <List.Item.Meta
                  avatar={<Ticket size={16} style={{ color: DESIGN.colors.accent }} />}
                  title={<a onClick={() => handleItemClick(item)}>{item.title}</a>}
                  description={[item.status, item.projectName, item.code].filter(Boolean).join(' · ')}
                />
              </List.Item>
            )}
          />
        </div>
      ) : searchResults && !isSearching ? (
        <div style={{ padding: 40, textAlign: 'center', color: DESIGN.colors.textMuted }}>
          未找到结果
        </div>
      ) : (
        <div style={{ padding: 40, textAlign: 'center', color: DESIGN.colors.textMuted }}>
          输入关键词搜索...
        </div>
      )}
    </Modal>
  );
};

// 独立的搜索框组件（用于 Header 右侧）
interface SearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSearch: (value: string) => void;
  onOpen?: () => void;
}

export const SearchInput: React.FC<SearchInputProps> = ({ value, onChange, onSearch, onOpen }) => {
  return (
    <Input
      className={styles.headerSearchInput}
      placeholder='搜索...'
      prefix={<Search size={14} style={{ color: DESIGN.colors.textMuted }} />}
      suffix={
        <kbd
          style={{
            fontSize: 11,
            padding: '1px 5px',
            borderRadius: 3,
            background: DESIGN.colors.bgSubtle,
            border: `1px solid ${DESIGN.colors.border}`,
            color: DESIGN.colors.textMuted,
            lineHeight: 1.4,
          }}
        >
          Ctrl K
        </kbd>
      }
      value={value}
      onChange={e => onChange(e.target.value)}
      onPressEnter={() => onSearch(value)}
      onClick={onOpen}
      style={{
        width: 180,
        height: 32,
        minHeight: 32,
        padding: '0 10px',
        display: 'inline-flex',
        alignItems: 'center',
        borderRadius: DESIGN.radius.md,
        border: `1px solid ${DESIGN.colors.border}`,
        background: DESIGN.colors.bgSubtle,
        fontSize: 13,
      }}
    />
  );
};
