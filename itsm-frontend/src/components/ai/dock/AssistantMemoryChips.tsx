/**
 * V2-01 个性化记忆：Dock 输入框上方的记忆条（常用语 / 个人模板 / 记忆管理）。
 *
 * 交互契约（方案 §7 V2-01）：
 * - 点击 chip = **只预填输入框**（`onInsert`），绝不自动发送/提交；
 * - 「存为常用语 / 存为模板」= 显式保存当前输入（用户主动提供，可随时在管理面删除）；
 * - 管理面展示「最近处理方式」（纯元数据：动作键 + 时间），并提供用户级关闭与清空；
 * - 租户策略关闭 / 埋点 off → 本组件不渲染（零请求由 store 保证）；
 *   用户显式关闭 → 渲染「重新开启」入口（区别于租户策略，不误导）。
 */
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { App, Button, Input, Modal, Popover, Tag, Tooltip, Typography, theme } from 'antd';
import { BookmarkPlus, FileText, History, Trash2 } from 'lucide-react';

import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { recordAssistantMemoryAction } from '@/lib/ai/memory/assistant-memory-store';
import { useAssistantMemory } from '@/lib/ai/memory/use-assistant-memory';
import {
  MEMORY_MAX_TEMPLATE_BODY_CHARS,
  MEMORY_MAX_TEMPLATE_NAME_CHARS,
} from '@/lib/ai/memory/types';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text } = Typography;

/** recent 动作键 → 翻译键（未登记键的兜底规则与 lib/ai/memory/types.recentActionLabel 同口径）。 */
const RECENT_ACTION_KEYS: Record<string, string> = {
  'inline_apply:triage_detail': 'assistant.memory.recentTriageDetail',
  'inline_apply:triage_create': 'assistant.memory.recentTriageCreate',
  'memory.phrase_used': 'assistant.memory.recentPhraseUsed',
  'memory.template_used': 'assistant.memory.recentTemplateUsed',
};

export const ASSISTANT_MEMORY_CHIPS_TESTID = 'assistant-memory-chips';
export const ASSISTANT_MEMORY_CHIP_TESTID = 'assistant-memory-chip';
export const ASSISTANT_MEMORY_MANAGE_TESTID = 'assistant-memory-manage';
export const ASSISTANT_MEMORY_SAVE_PHRASE_TESTID = 'assistant-memory-save-phrase';
export const ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID = 'assistant-memory-save-template';
export const ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID = 'assistant-memory-template-modal';
export const ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID = 'assistant-memory-template-name';
export const ASSISTANT_MEMORY_TEMPLATE_BODY_TESTID = 'assistant-memory-template-body';
export const ASSISTANT_MEMORY_REENABLE_TESTID = 'assistant-memory-reenable';
export const ASSISTANT_MEMORY_CLEAR_TESTID = 'assistant-memory-clear';
export const ASSISTANT_MEMORY_DISABLE_TESTID = 'assistant-memory-disable';

interface MemoryChip {
  kind: 'phrase' | 'template';
  id: string;
  label: string;
  insertText: string;
}

export interface AssistantMemoryChipsProps {
  /** 当前输入（存为常用语/模板的素材；空文本时保存按钮禁用）。 */
  query: string;
  /** 点击 chip：只预填输入框（调用方负责 setQuery + 聚焦），绝不自动发送。 */
  onInsert: (text: string) => void;
}

const AssistantMemoryChips: React.FC<AssistantMemoryChipsProps> = ({ query, onInsert }) => {
  const { token } = theme.useToken();
  const { message, modal } = App.useApp();
  const { t } = useI18n();
  const {
    mode,
    status,
    memory,
    error,
    ready,
    userDisabled,
    savePhrase,
    saveTemplate,
    removePhrase,
    removeTemplate,
    clearAll,
    setUserEnabled,
  } = useAssistantMemory();

  const [templateOpen, setTemplateOpen] = useState(false);
  const [templateName, setTemplateName] = useState('');
  const [templateBody, setTemplateBody] = useState('');
  const shownRef = useRef(false);

  const chips = useMemo<MemoryChip[]>(
    () => [
      ...memory.phrases.map(phrase => ({
        kind: 'phrase' as const,
        id: `phrase:${phrase}`,
        label: phrase,
        insertText: phrase,
      })),
      ...memory.templates.map(tpl => ({
        kind: 'template' as const,
        id: `template:${tpl.name}`,
        label: tpl.name,
        insertText: tpl.body || tpl.name,
      })),
    ],
    [memory]
  );

  // 展示埋点：仅当确有内容可展示时上报一次（suggestion_shown 复用，cardId=memory）。
  useEffect(() => {
    if (!ready || shownRef.current) return;
    if (chips.length === 0 && memory.recent.length === 0) return;
    shownRef.current = true;
    trackAssistantEvent('suggestion_shown', {
      cardId: 'memory',
      extra: {
        phrases: memory.phrases.length,
        templates: memory.templates.length,
        recent: memory.recent.length,
      },
    });
  }, [ready, chips.length, memory]);

  if (status === 'disabled') {
    if (!userDisabled) return null; // 租户策略 / 埋点 off：策略面处理，不渲染
    return (
      <div style={{ marginBottom: 6, textAlign: 'right' }}>
        <Text type="secondary" style={{ fontSize: 11 }}>
          {t('assistant.memory.disabled')}
        </Text>
        <Button
          size="small"
          type="link"
          data-testid={ASSISTANT_MEMORY_REENABLE_TESTID}
          onClick={() => void setUserEnabled(true)}
        >
          {t('assistant.memory.reenable')}
        </Button>
      </div>
    );
  }

  if (status !== 'ready') return null;

  const handleChip = (chip: MemoryChip) => {
    onInsert(chip.insertText);
    recordAssistantMemoryAction(chip.kind === 'phrase' ? 'memory.phrase_used' : 'memory.template_used');
    trackAssistantEvent('suggestion_clicked', { cardId: 'memory', extra: { kind: chip.kind } });
  };

  const handleSavePhrase = async () => {
    const text = query.trim();
    if (!text) return;
    await savePhrase(text);
    message.success(t('assistant.memory.savedPhrase'));
  };

  const openTemplateModal = () => {
    setTemplateName('');
    setTemplateBody(query.trim());
    setTemplateOpen(true);
  };

  const handleSaveTemplate = async () => {
    if (!templateName.trim()) return;
    await saveTemplate(templateName, templateBody);
    setTemplateOpen(false);
    message.success(t('assistant.memory.savedTemplate'));
  };

  const handleDisable = () => {
    modal.confirm({
      title: t('assistant.memory.disableTitle'),
      content: t('assistant.memory.disableContent'),
      okText: t('assistant.memory.disableOk'),
      cancelText: t('assistant.panel.cancel'),
      onOk: () => void setUserEnabled(false),
    });
  };

  const handleClear = () => {
    modal.confirm({
      title: t('assistant.memory.clearTitle'),
      content: t('assistant.memory.clearContent'),
      okText: t('assistant.memory.clearOk'),
      okButtonProps: { danger: true },
      cancelText: t('assistant.panel.cancel'),
      onOk: () => void clearAll(),
    });
  };

  const formatTime = (at: string) => at.slice(0, 16).replace('T', ' ');

  const recentActionText = (key: string) =>
    RECENT_ACTION_KEYS[key]
      ? t(RECENT_ACTION_KEYS[key])
      : key.startsWith('inline_apply:')
        ? t('assistant.memory.recentInlineApply', { scope: key.slice('inline_apply:'.length) })
        : key;

  const manageContent = (
    <div style={{ width: 272 }} data-testid="assistant-memory-manage-panel">
      <Text strong style={{ fontSize: 12 }}>
        {t('assistant.memory.phrasesTitle', { count: memory.phrases.length })}
      </Text>
      {memory.phrases.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            {t('assistant.memory.phrasesEmpty')}
          </Text>
        </div>
      ) : (
        memory.phrases.map(phrase => (
          <div key={phrase} style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 2 }}>
            <Text ellipsis style={{ fontSize: 12, flex: 1 }} title={phrase}>
              {phrase}
            </Text>
            <Button
              size="small"
              type="text"
              danger
              icon={<Trash2 size={12} />}
              aria-label={t('assistant.memory.removePhraseAria', { phrase })}
              onClick={() => void removePhrase(phrase)}
            />
          </div>
        ))
      )}

      <div style={{ marginTop: 10 }}>
        <Text strong style={{ fontSize: 12 }}>
          {t('assistant.memory.templatesTitle', { count: memory.templates.length })}
        </Text>
      </div>
      {memory.templates.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            {t('assistant.memory.templatesEmpty')}
          </Text>
        </div>
      ) : (
        memory.templates.map(tpl => (
          <div key={tpl.name} style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 2 }}>
            <Text ellipsis style={{ fontSize: 12, flex: 1 }} title={tpl.name}>
              {tpl.name}
            </Text>
            <Button
              size="small"
              type="text"
              danger
              icon={<Trash2 size={12} />}
              aria-label={t('assistant.memory.removeTemplateAria', { name: tpl.name })}
              onClick={() => void removeTemplate(tpl.name)}
            />
          </div>
        ))
      )}

      <div style={{ marginTop: 10 }}>
        <Text strong style={{ fontSize: 12 }}>
          {t('assistant.memory.recentTitle')}
        </Text>
      </div>
      {memory.recent.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            {t('assistant.memory.recentEmpty')}
          </Text>
        </div>
      ) : (
        memory.recent.slice(0, 5).map(item => (
          <div key={item.key}>
            <Text type="secondary" style={{ fontSize: 11 }}>
              · {recentActionText(item.key)}（{formatTime(item.at)}）
            </Text>
          </div>
        ))
      )}

      {mode === 'local' ? (
        <div style={{ marginTop: 8 }}>
          <Text type="secondary" style={{ fontSize: 11 }}>
            {t('assistant.memory.localNote')}
          </Text>
        </div>
      ) : null}

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 8,
          marginTop: 10,
          paddingTop: 8,
          borderTop: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <Button size="small" data-testid={ASSISTANT_MEMORY_DISABLE_TESTID} onClick={handleDisable}>
          {t('assistant.memory.disableButton')}
        </Button>
        <Button size="small" danger data-testid={ASSISTANT_MEMORY_CLEAR_TESTID} onClick={handleClear}>
          {t('assistant.memory.clearButton')}
        </Button>
      </div>
    </div>
  );

  return (
    <div
      data-testid={ASSISTANT_MEMORY_CHIPS_TESTID}
      style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 6, marginBottom: 6 }}
    >
      {chips.map(chip => (
        <Tooltip key={chip.id} title={chip.kind === 'template' ? t('assistant.memory.chipTemplate', { name: chip.label }) : t('assistant.memory.chipPhrase')}>
          <Tag
            data-testid={ASSISTANT_MEMORY_CHIP_TESTID}
            data-kind={chip.kind}
            style={{ cursor: 'pointer', margin: 0, maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            onClick={() => handleChip(chip)}
          >
            {chip.label}
          </Tag>
        </Tooltip>
      ))}

      <Popover trigger="click" placement="topRight" content={manageContent}>
        <Button
          size="small"
          type="text"
          icon={<History size={13} />}
          data-testid={ASSISTANT_MEMORY_MANAGE_TESTID}
          aria-label={t('assistant.memory.manageAria')}
        >
          {t('assistant.memory.manageButton')}
        </Button>
      </Popover>

      <Tooltip title={t('assistant.memory.savePhraseTooltip')}>
        <Button
          size="small"
          type="text"
          icon={<BookmarkPlus size={13} />}
          data-testid={ASSISTANT_MEMORY_SAVE_PHRASE_TESTID}
          aria-label={t('assistant.memory.savePhraseAria')}
          disabled={!query.trim()}
          onClick={() => void handleSavePhrase()}
        />
      </Tooltip>
      <Tooltip title={t('assistant.memory.saveTemplateTooltip')}>
        <Button
          size="small"
          type="text"
          icon={<FileText size={13} />}
          data-testid={ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID}
          aria-label={t('assistant.memory.saveTemplateAria')}
          disabled={!query.trim()}
          onClick={openTemplateModal}
        />
      </Tooltip>

      {error === 'save_failed' ? (
        <Text type="danger" style={{ fontSize: 11 }}>
          {t('assistant.memory.saveFailed')}
        </Text>
      ) : null}
      {error === 'sync_failed' ? (
        <Text type="secondary" style={{ fontSize: 11 }}>
          {t('assistant.memory.syncFailed')}
        </Text>
      ) : null}

      <Modal
        title={t('assistant.memory.templateModalTitle')}
        open={templateOpen}
        onCancel={() => setTemplateOpen(false)}
        onOk={() => void handleSaveTemplate()}
        okText={t('assistant.memory.save')}
        cancelText={t('assistant.panel.cancel')}
        okButtonProps={{ disabled: !templateName.trim() }}
      >
        <div data-testid={ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID}>
          <Input
            placeholder={t('assistant.memory.templateNamePlaceholder', { max: MEMORY_MAX_TEMPLATE_NAME_CHARS })}
            value={templateName}
            maxLength={MEMORY_MAX_TEMPLATE_NAME_CHARS}
            onChange={e => setTemplateName(e.target.value)}
            data-testid={ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID}
            style={{ marginBottom: 8 }}
          />
          <Input.TextArea
            placeholder={t('assistant.memory.templateBodyPlaceholder', { max: MEMORY_MAX_TEMPLATE_BODY_CHARS })}
            value={templateBody}
            maxLength={MEMORY_MAX_TEMPLATE_BODY_CHARS}
            onChange={e => setTemplateBody(e.target.value)}
            autoSize={{ minRows: 3, maxRows: 8 }}
            data-testid={ASSISTANT_MEMORY_TEMPLATE_BODY_TESTID}
          />
        </div>
      </Modal>

    </div>
  );
};

export default AssistantMemoryChips;
